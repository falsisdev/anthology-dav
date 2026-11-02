package stream

import (
	"bufio"
	"bytes"
	"context"
	"crypto/tls"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"strconv"
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

// ServeStreamResult delivers a StreamResult to media players (Infuse, Kodi, etc.) or HTTP clients
// via local Range Proxy or HLS rewriter. It proxies all streams locally with proper headers and CORS
// to guarantee that players never fail due to CDN hotlink/CORS/User-Agent restrictions.
func (h *ProxyHandler) ServeStreamResult(w http.ResponseWriter, r *http.Request, res *providers.StreamResult) error {
	if res == nil || res.Primary.URL == "" {
		return fmt.Errorf("empty stream result")
	}

	requestedQuality := strings.ToLower(strings.TrimSpace(r.URL.Query().Get("quality")))

	allCandidates := append([]providers.StreamSource{res.Primary}, res.Alternates...)
	var candidates []providers.StreamSource
	if requestedQuality != "" && requestedQuality != "auto" {
		var matched, rest []providers.StreamSource
		for _, c := range allCandidates {
			norm := strings.ToLower(providers.NormalizeQuality(c.Quality))
			if norm == requestedQuality || strings.Contains(strings.ToLower(c.Quality), requestedQuality) {
				matched = append(matched, c)
			} else {
				rest = append(rest, c)
			}
		}
		candidates = append(matched, rest...)
	} else {
		candidates = allCandidates
	}

	var lastErr error

	for i, candidate := range candidates {
		subs := candidate.Subtitles
		if len(subs) == 0 {
			subs = res.Subtitles
		}
		isHLS := candidate.IsHLS || strings.Contains(strings.ToLower(candidate.URL), ".m3u8")
		if isHLS {
			err := h.serveHLSManifestWithSubs(w, r, candidate.URL, candidate.Headers, subs)
			if err == nil {
				return nil
			}
			lastErr = err
			log.Printf("[STREAM] HLS candidate %d failed (%s): %v", i, candidate.Quality, err)
			continue
		}

		resp, err := h.openUpstream(r, candidate.URL, candidate.Headers)
		if err != nil || resp.StatusCode >= 400 {
			if err == nil {
				err = fmt.Errorf("upstream returned %d", resp.StatusCode)
				resp.Body.Close()
			}
			lastErr = err
			log.Printf("[STREAM] Range proxy candidate %d failed (%s): %v", i, candidate.Quality, err)
			continue
		}

		// Peek at initial bytes in case an unmarked stream URL is actually an HLS playlist (#EXTM3U)
		if r.Method == http.MethodGet && r.Header.Get("Range") == "" {
			br := bufio.NewReaderSize(resp.Body, 64*1024)
			peek, _ := br.Peek(128)
			trimmedPeek := bytes.TrimSpace(bytes.TrimPrefix(peek, []byte("\xef\xbb\xbf")))
			if bytes.HasPrefix(trimmedPeek, []byte("#EXTM3U")) {
				err := h.rewriteAndWriteHLS(w, r, br, resp, candidate.URL, candidate.Headers, subs)
				resp.Body.Close()
				if err == nil {
					return nil
				}
				lastErr = err
				continue
			}
			h.writeUpstreamReader(w, r, resp, br, peek)
			return nil
		}

		h.writeUpstream(w, r, resp)
		return nil
	}

	if lastErr != nil {
		return lastErr
	}
	return fmt.Errorf("stream playback failed across all candidates")
}

// BuildDynamicMasterPlaylist generates an RFC 8216 compliant HLS Master Playlist
// combining multiple resolution variants and subtitle tracks.
func (h *ProxyHandler) BuildDynamicMasterPlaylist(r *http.Request, hlsCandidates []providers.StreamSource, subs []providers.Subtitle) string {
	var sb strings.Builder
	sb.WriteString("#EXTM3U\n")
	sb.WriteString("#EXT-X-VERSION:3\n")
	sb.WriteString("#EXT-X-INDEPENDENT-SEGMENTS\n")

	baseURLPrefix := h.cfg.BaseURL
	if r.Host != "" {
		scheme := "http"
		if r.TLS != nil || r.Header.Get("X-Forwarded-Proto") == "https" {
			scheme = "https"
		}
		baseURLPrefix = fmt.Sprintf("%s://%s", scheme, r.Host)
	}

	// 1. Add Subtitle tracks if present
	hasSubs := len(subs) > 0
	subGroupID := "anthology_subs"
	if hasSubs {
		for i, sub := range subs {
			isDefault := "NO"
			if i == 0 {
				isDefault = "YES"
			}
			label := sub.Label
			if label == "" {
				label = "Türkçe"
			}
			lang := sub.Language
			if lang == "" {
				lang = "tr"
			}
			subURL := fmt.Sprintf("%s/sub/direct?url=%s", baseURLPrefix, url.QueryEscape(sub.URL))
			sb.WriteString(fmt.Sprintf("#EXT-X-MEDIA:TYPE=SUBTITLES,GROUP-ID=%q,NAME=%q,DEFAULT=%s,AUTOSELECT=YES,LANGUAGE=%q,URI=%q\n",
				subGroupID, label, isDefault, lang, subURL))
		}
	}

	// 2. Add Video Stream Variants sorted by quality score descending
	for _, cand := range hlsCandidates {
		w, height, bw, normLabel := providers.ParseResolutionDetails(cand.Quality)
		headersJSON, _ := json.Marshal(cand.Headers)
		hB64 := base64.RawURLEncoding.EncodeToString(headersJSON)
		uB64 := base64.RawURLEncoding.EncodeToString([]byte(cand.URL))

		variantURL := fmt.Sprintf("%s/proxy/media?u=%s&h=%s", baseURLPrefix, uB64, hB64)

		if hasSubs {
			sb.WriteString(fmt.Sprintf("#EXT-X-STREAM-INF:BANDWIDTH=%d,RESOLUTION=%dx%d,NAME=%q,SUBTITLES=%q\n",
				bw, w, height, normLabel, subGroupID))
		} else {
			sb.WriteString(fmt.Sprintf("#EXT-X-STREAM-INF:BANDWIDTH=%d,RESOLUTION=%dx%d,NAME=%q\n",
				bw, w, height, normLabel))
		}
		sb.WriteString(variantURL + "\n")
	}

	return sb.String()
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
	if resp.StatusCode < 400 && r.Method == http.MethodGet {
		br := bufio.NewReaderSize(resp.Body, 64*1024)
		peek, _ := br.Peek(128)
		trimmedPeek := bytes.TrimSpace(bytes.TrimPrefix(peek, []byte("\xef\xbb\xbf")))
		if bytes.HasPrefix(trimmedPeek, []byte("#EXTM3U")) {
			defer resp.Body.Close()
			if err := h.rewriteAndWriteHLS(w, r, br, resp, targetURL, customHeaders, nil); err != nil {
				http.Error(w, err.Error(), http.StatusBadGateway)
			}
			return
		}
		h.writeUpstreamReader(w, r, resp, br, peek)
		return
	}
	h.writeUpstream(w, r, resp)
}

func (h *ProxyHandler) applyUpstreamHeaders(req *http.Request, customHeaders map[string]string) {
	req.Header.Set("User-Agent", h.cfg.UserAgent)
	isFastplay := strings.Contains(strings.ToLower(req.URL.Host), "fastplay") || strings.Contains(strings.ToLower(req.URL.Path), "/manifests/")
	for k, v := range customHeaders {
		if strings.EqualFold(k, "X-Fastplay-Sp") || strings.EqualFold(k, "X-Fastplay-Spt") {
			continue
		}
		if strings.EqualFold(k, "X-Sp") && !isFastplay {
			continue
		}
		if v != "" {
			req.Header.Set(k, v)
		}
	}
	if isFastplay {
		if sp := customHeaders["X-Fastplay-Sp"]; sp != "" {
			spt, _ := strconv.ParseInt(customHeaders["X-Fastplay-Spt"], 10, 64)
			if spt <= 0 {
				spt = time.Now().Unix()
			}
			req.Header.Set("X-Sp", providers.BuildFastPlayXSp(sp, spt))
		}
	}
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

	h.applyUpstreamHeaders(req, customHeaders)
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
	h.writeUpstreamReader(w, r, resp, resp.Body, nil)
}

func (h *ProxyHandler) writeUpstreamReader(w http.ResponseWriter, r *http.Request, resp *http.Response, bodyReader io.Reader, peek []byte) {
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
	// If upstream disguises an MPEG-TS segment (sync byte 0x47) as .jpg/.png/.html, normalize Content-Type for strict players.
	if len(peek) >= 4 && peek[0] == 0x47 {
		ct := strings.ToLower(w.Header().Get("Content-Type"))
		if strings.Contains(ct, "image/") || strings.Contains(ct, "text/html") || strings.Contains(ct, "text/plain") {
			w.Header().Set("Content-Type", "video/mp2t")
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
	_, _ = io.CopyBuffer(w, bodyReader, buf)
}

func (h *ProxyHandler) serveHLSManifest(w http.ResponseWriter, r *http.Request, manifestURL string, customHeaders map[string]string) error {
	return h.serveHLSManifestWithSubs(w, r, manifestURL, customHeaders, nil)
}

func (h *ProxyHandler) serveHLSManifestWithSubs(w http.ResponseWriter, r *http.Request, manifestURL string, customHeaders map[string]string, subs []providers.Subtitle) error {
	req, err := http.NewRequestWithContext(r.Context(), http.MethodGet, manifestURL, nil)
	if err != nil {
		return err
	}
	h.applyUpstreamHeaders(req, customHeaders)

	resp, err := h.httpClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		return fmt.Errorf("upstream HLS returned %d", resp.StatusCode)
	}

	return h.rewriteAndWriteHLS(w, r, resp.Body, resp, manifestURL, customHeaders, subs)
}

func (h *ProxyHandler) rewriteAndWriteHLS(w http.ResponseWriter, r *http.Request, bodyReader io.Reader, resp *http.Response, manifestURL string, customHeaders map[string]string, subs []providers.Subtitle) error {
	targetManifestURL := manifestURL
	if resp != nil && resp.Request != nil && resp.Request.URL != nil {
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

	rawBytes, err := io.ReadAll(bodyReader)
	if err != nil {
		return err
	}
	rawManifest := strings.TrimSpace(strings.TrimPrefix(string(rawBytes), "\xef\xbb\xbf"))
	if !strings.HasPrefix(rawManifest, "#EXTM3U") && !strings.Contains(rawManifest, "#EXT-X-") {
		return fmt.Errorf("upstream response is not a valid HLS manifest")
	}

	isMaster := strings.Contains(rawManifest, "#EXT-X-STREAM-INF")
	hasExistingSubs := strings.Contains(rawManifest, "TYPE=SUBTITLES")
	injectSubs := isMaster && !hasExistingSubs && len(subs) > 0
	subGroupID := "anthology_subs"

	var out strings.Builder
	subsInjected := false
	scanner := bufio.NewScanner(strings.NewReader(rawManifest))
	scanner.Buffer(make([]byte, 1024*1024), 4*1024*1024)
	for scanner.Scan() {
		line := scanner.Text()
		trimmed := strings.TrimSpace(line)
		if trimmed == "" {
			out.WriteString("\n")
			continue
		}
		if strings.HasPrefix(trimmed, "#") {
			if strings.HasPrefix(trimmed, "#EXTM3U") && injectSubs && !subsInjected {
				out.WriteString(line + "\n")
				subsInjected = true
				for i, sub := range subs {
					isDefault := "NO"
					if i == 0 {
						isDefault = "YES"
					}
					label := sub.Label
					if label == "" {
						label = "Türkçe"
					}
					lang := sub.Language
					if lang == "" {
						lang = "tr"
					}
					subURL := fmt.Sprintf("%s/sub/direct?url=%s", baseURLPrefix, url.QueryEscape(sub.URL))
					out.WriteString(fmt.Sprintf("#EXT-X-MEDIA:TYPE=SUBTITLES,GROUP-ID=%q,NAME=%q,DEFAULT=%s,AUTOSELECT=YES,LANGUAGE=%q,URI=%q\n",
						subGroupID, label, isDefault, lang, subURL))
				}
				continue
			}
			if strings.HasPrefix(trimmed, "#EXT-X-STREAM-INF:") && injectSubs && !strings.Contains(trimmed, "SUBTITLES=") {
				line = fmt.Sprintf(`%s,SUBTITLES="%s"`, trimmed, subGroupID)
				trimmed = line
			}
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
	var out strings.Builder
	rem := tagLine
	for {
		idx := strings.Index(rem, `URI="`)
		if idx == -1 {
			out.WriteString(rem)
			break
		}
		start := idx + len(`URI="`)
		end := strings.Index(rem[start:], `"`)
		if end == -1 {
			out.WriteString(rem)
			break
		}
		out.WriteString(rem[:start])
		origURI := rem[start : start+end]
		out.WriteString(rewriteFn(origURI))
		rem = rem[start+end:]
	}
	return out.String()
}
