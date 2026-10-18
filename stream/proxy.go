package stream

import (
	"bufio"
	"context"
	"crypto/tls"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"strings"
	"time"

	"anthology-dav/config"
	"anthology-dav/providers"
	"anthology-dav/vfs"
)

// ProxyHandler serves /stream/{provider_id}, /sub/{provider_id}, and /proxy/media endpoints.
type ProxyHandler struct {
	cfg        *config.Config
	cache      *vfs.MemoryCache
	httpClient *http.Client
}

func NewProxyHandler(cfg *config.Config, cache *vfs.MemoryCache) *ProxyHandler {
	tr := &http.Transport{
		TLSClientConfig:       &tls.Config{InsecureSkipVerify: true},
		MaxIdleConns:          200,
		MaxIdleConnsPerHost:   32,
		IdleConnTimeout:       90 * time.Second,
		ResponseHeaderTimeout: 30 * time.Second,
	}
	return &ProxyHandler{
		cfg:        cfg,
		cache:      cache,
		httpClient: &http.Client{Transport: tr},
	}
}

// HandleStream handles /stream/{provider_id}?id={stream_id}
func (h *ProxyHandler) HandleStream(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodOptions {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Methods", "GET, HEAD, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "*")
		w.WriteHeader(http.StatusOK)
		return
	}

	providerID := strings.TrimPrefix(r.URL.Path, "/stream/")
	providerID = strings.Trim(providerID, "/")
	streamID := r.URL.Query().Get("id")
	if providerID == "" || streamID == "" {
		http.Error(w, "missing provider_id or id parameter", http.StatusBadRequest)
		return
	}

	prov, ok := providers.GetByID(providerID)
	if !ok {
		http.Error(w, "unknown provider: "+providerID, http.StatusNotFound)
		return
	}

	res, err := h.resolveCached(r.Context(), prov, streamID)
	if err != nil || res == nil || res.Primary.URL == "" {
		msg := "no stream sources resolved"
		if err != nil {
			msg = err.Error()
		}
		http.Error(w, msg, http.StatusBadGateway)
		return
	}

	if err := h.ServeStreamResult(w, r, res); err != nil {
		http.Error(w, err.Error(), http.StatusBadGateway)
	}
}

// ServeStreamResult delivers a StreamResult to media players or HTTP clients via HLS rewriter, Range Proxy, or 302 Redirect.
// All HLS playlists are proxied with CORS and segment rewriting to guarantee cross-device and browser playback.
func (h *ProxyHandler) ServeStreamResult(w http.ResponseWriter, r *http.Request, res *providers.StreamResult) error {
	if res == nil || res.Primary.URL == "" {
		return fmt.Errorf("empty stream result")
	}

	candidates := orderByHealth(append([]providers.StreamSource{res.Primary}, res.Alternates...))
	var lastErr error

	for i, candidate := range candidates {
		isLast := i == len(candidates)-1
		isHLS := candidate.IsHLS || strings.Contains(strings.ToLower(candidate.URL), ".m3u8")
		if isHLS {
			err := h.serveHLSManifest(w, r, candidate.URL, candidate.Headers)
			if err == nil {
				return nil
			}
			lastErr = err
			markHostBad(candidate.URL)
			log.Printf("[STREAM] HLS candidate %d failed (%s %s): %v", i, candidate.Quality, hostOf(candidate.URL), err)
			continue
		}

		if len(candidate.Headers) == 0 {
			if isHostBad(candidate.URL) && !isLast {
				log.Printf("[STREAM] skipping bad direct host (%s): %s", candidate.Quality, hostOf(candidate.URL))
				continue
			}
			http.Redirect(w, r, candidate.URL, http.StatusFound)
			return nil
		}

		resp, err := h.openUpstream(r, candidate.URL, candidate.Headers)
		if err != nil || resp.StatusCode >= 400 {
			if err == nil {
				err = fmt.Errorf("upstream returned %d", resp.StatusCode)
				if isBadUpstreamStatus(resp.StatusCode) {
					markHostBad(candidate.URL)
				}
				resp.Body.Close()
			} else if r.Context().Err() == nil {
				markHostBad(candidate.URL)
			}
			lastErr = err
			log.Printf("[STREAM] proxied candidate %d failed (%s): %v", i, candidate.Quality, err)
			continue
		}
		h.writeUpstream(w, r, resp)
		return nil
	}

	if lastErr != nil {
		return lastErr
	}
	return fmt.Errorf("stream playback failed across all candidates")
}

// probeUpstream performs a tiny ranged GET to verify a direct URL is playable.
func (h *ProxyHandler) probeUpstream(ctx context.Context, targetURL string, customHeaders map[string]string) (int, error) {
	pctx, cancel := context.WithTimeout(ctx, 6*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(pctx, http.MethodGet, targetURL, nil)
	if err != nil {
		return 0, err
	}
	req.Header.Set("User-Agent", h.cfg.UserAgent)
	for k, v := range customHeaders {
		if v != "" {
			req.Header.Set(k, v)
		}
	}
	req.Header.Set("Range", "bytes=0-0")
	resp, err := h.httpClient.Do(req)
	if err != nil {
		return 0, err
	}
	resp.Body.Close()
	if resp.StatusCode >= 400 {
		return resp.StatusCode, fmt.Errorf("upstream returned %d", resp.StatusCode)
	}
	return resp.StatusCode, nil
}

// HandleSub handles /sub/{provider_id}?id={stream_id}&url={direct_url}
func (h *ProxyHandler) HandleSub(w http.ResponseWriter, r *http.Request) {
	providerID := strings.TrimPrefix(r.URL.Path, "/sub/")
	providerID = strings.Trim(providerID, "/")
	streamID := r.URL.Query().Get("id")
	directURL := r.URL.Query().Get("url")

	var subHeaders map[string]string
	if directURL == "" {
		prov, ok := providers.GetByID(providerID)
		if !ok {
			http.Error(w, "unknown provider: "+providerID, http.StatusNotFound)
			return
		}
		res, err := h.resolveCached(r.Context(), prov, streamID)
		if err != nil || res == nil || len(res.Subtitles) == 0 {
			http.Error(w, "no subtitles found", http.StatusNotFound)
			return
		}
		sub := res.Subtitles[0]
		directURL = sub.URL
		subHeaders = sub.Headers
	}

	data, _, _, _, err := providers.FetchBytes(r.Context(), "GET", directURL, subHeaders, nil)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadGateway)
		return
	}

	w.Header().Set("Access-Control-Allow-Origin", "*")
	if strings.HasSuffix(strings.ToLower(directURL), ".srt") {
		w.Header().Set("Content-Type", "application/x-subrip; charset=utf-8")
	} else {
		w.Header().Set("Content-Type", "text/vtt; charset=utf-8")
	}
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(data)
}

// HandleProxyMedia handles rewritten HLS segments/sub-manifests and direct proxied media URLs:
// /proxy/media?u={base64_url}&h={base64_headers_json}
func (h *ProxyHandler) HandleProxyMedia(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodOptions {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Methods", "GET, HEAD, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "*")
		w.WriteHeader(http.StatusOK)
		return
	}

	uB64 := r.URL.Query().Get("u")
	hB64 := r.URL.Query().Get("h")
	if uB64 == "" {
		http.Error(w, "missing u parameter", http.StatusBadRequest)
		return
	}
	rawURLBytes, err := base64.RawURLEncoding.DecodeString(uB64)
	if err != nil {
		http.Error(w, "invalid u parameter", http.StatusBadRequest)
		return
	}
	targetURL := string(rawURLBytes)

	var headers map[string]string
	if hB64 != "" {
		if hBytes, err := base64.RawURLEncoding.DecodeString(hB64); err == nil {
			_ = json.Unmarshal(hBytes, &headers)
		}
	}

	lowerURL := strings.ToLower(targetURL)
	if strings.Contains(lowerURL, ".m3u8") {
		if err := h.serveHLSManifest(w, r, targetURL, headers); err != nil {
			http.Error(w, err.Error(), http.StatusBadGateway)
		}
		return
	}
	h.serveRangeStream(w, r, targetURL, headers)
}

func (h *ProxyHandler) resolveCached(ctx context.Context, prov providers.Provider, streamID string) (*providers.StreamResult, error) {
	key := fmt.Sprintf("stream:%s:%s", prov.ID(), streamID)
	if val, ok := h.cache.Get(key); ok {
		if res, ok := val.(*providers.StreamResult); ok {
			return res, nil
		}
	}
	rctx, cancel := context.WithTimeout(ctx, 25*time.Second)
	defer cancel()
	res, err := prov.ResolveStream(rctx, streamID)
	if err != nil {
		return nil, err
	}
	if res != nil && res.Primary.URL != "" {
		h.cache.Set(key, res, h.cfg.StreamTTL)
	}
	return res, nil
}

func (h *ProxyHandler) serveRangeStream(w http.ResponseWriter, r *http.Request, targetURL string, customHeaders map[string]string) {
	resp, err := h.openUpstream(r, targetURL, customHeaders)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadGateway)
		return
	}
	h.writeUpstream(w, r, resp)
}

// openUpstream issues the (ranged) upstream request without writing anything to the client.
func (h *ProxyHandler) openUpstream(r *http.Request, targetURL string, customHeaders map[string]string) (*http.Response, error) {
	method := r.Method
	if method != http.MethodGet && method != http.MethodHead {
		method = http.MethodGet
	}

	req, err := http.NewRequestWithContext(r.Context(), method, targetURL, nil)
	if err != nil {
		return nil, err
	}

	req.Header.Set("User-Agent", h.cfg.UserAgent)
	for k, v := range customHeaders {
		if v != "" {
			req.Header.Set(k, v)
		}
	}
	if rangeHdr := r.Header.Get("Range"); rangeHdr != "" {
		req.Header.Set("Range", rangeHdr)
	}
	if ifRange := r.Header.Get("If-Range"); ifRange != "" {
		req.Header.Set("If-Range", ifRange)
	}

	return h.httpClient.Do(req)
}

// writeUpstream streams an upstream response (headers + body) to the client and closes it.
func (h *ProxyHandler) writeUpstream(w http.ResponseWriter, r *http.Request, resp *http.Response) {
	defer resp.Body.Close()

	passthroughHeaders := []string{
		"Content-Type",
		"Content-Length",
		"Content-Range",
		"Accept-Ranges",
		"ETag",
		"Last-Modified",
		"Content-Disposition",
	}
	for _, hdr := range passthroughHeaders {
		if val := resp.Header.Get(hdr); val != "" {
			w.Header().Set(hdr, val)
		}
	}
	if w.Header().Get("Accept-Ranges") == "" {
		w.Header().Set("Accept-Ranges", "bytes")
	}
	w.Header().Set("Access-Control-Allow-Origin", "*")

	w.WriteHeader(resp.StatusCode)
	if r.Method == http.MethodHead {
		return
	}
	buf := make([]byte, 64*1024)
	_, _ = io.CopyBuffer(w, resp.Body, buf)
}

func (h *ProxyHandler) serveHLSManifest(w http.ResponseWriter, r *http.Request, manifestURL string, customHeaders map[string]string) error {
	req, err := http.NewRequestWithContext(r.Context(), http.MethodGet, manifestURL, nil)
	if err != nil {
		return err
	}
	req.Header.Set("User-Agent", h.cfg.UserAgent)
	for k, v := range customHeaders {
		if v != "" {
			req.Header.Set(k, v)
		}
	}

	resp, err := h.httpClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		return fmt.Errorf("upstream HLS returned %d", resp.StatusCode)
	}

	targetManifestURL := manifestURL
	if resp.Request != nil && resp.Request.URL != nil {
		targetManifestURL = resp.Request.URL.String()
	}

	baseURL, err := url.Parse(targetManifestURL)
	if err != nil {
		return err
	}

	baseURLPrefix := h.cfg.BaseURL
	if r.Host != "" {
		scheme := "http"
		if r.TLS != nil || r.Header.Get("X-Forwarded-Proto") == "https" {
			scheme = "https"
		}
		baseURLPrefix = fmt.Sprintf("%s://%s", scheme, r.Host)
	}

	headersJSON, _ := json.Marshal(customHeaders)
	hB64 := base64.RawURLEncoding.EncodeToString(headersJSON)

	rewriteURL := func(rawRef string) string {
		rawRef = strings.TrimSpace(rawRef)
		if rawRef == "" {
			return rawRef
		}
		refURL, err := url.Parse(rawRef)
		if err != nil {
			return rawRef
		}
		resolved := baseURL.ResolveReference(refURL).String()
		uB64 := base64.RawURLEncoding.EncodeToString([]byte(resolved))
		return fmt.Sprintf("%s/proxy/media?u=%s&h=%s", baseURLPrefix, uB64, hB64)
	}

	var out strings.Builder
	scanner := bufio.NewScanner(resp.Body)
	scanner.Buffer(make([]byte, 1024*1024), 4*1024*1024)
	for scanner.Scan() {
		line := scanner.Text()
		trimmed := strings.TrimSpace(line)
		if trimmed == "" {
			out.WriteString("\n")
			continue
		}
		if strings.HasPrefix(trimmed, "#") {
			if strings.Contains(trimmed, `URI="`) {
				line = rewriteTagURI(trimmed, rewriteURL)
			}
			out.WriteString(line + "\n")
			continue
		}
		out.WriteString(rewriteURL(trimmed) + "\n")
	}

	content := out.String()
	w.Header().Set("Content-Type", "application/vnd.apple.mpegurl; charset=utf-8")
	w.Header().Set("Content-Length", fmt.Sprintf("%d", len(content)))
	w.Header().Set("Access-Control-Allow-Origin", "*")
	w.Header().Set("Access-Control-Allow-Methods", "GET, HEAD, OPTIONS")
	w.Header().Set("Access-Control-Allow-Headers", "*")
	w.Header().Set("Cache-Control", "no-cache")
	w.WriteHeader(http.StatusOK)
	if r.Method != http.MethodHead {
		_, _ = io.WriteString(w, content)
	}
	return nil
}

func rewriteTagURI(tagLine string, rewriteFn func(string) string) string {
	idx := strings.Index(tagLine, `URI="`)
	if idx == -1 {
		return tagLine
	}
	start := idx + len(`URI="`)
	end := strings.Index(tagLine[start:], `"`)
	if end == -1 {
		return tagLine
	}
	origURI := tagLine[start : start+end]
	newURI := rewriteFn(origURI)
	return tagLine[:start] + newURI + tagLine[start+end:]
}
