package providers

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"html"
	"io"
	"math/rand"
	"net/http"
	"net/url"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"anthology-dav/config"
)

const DefaultTimeout = 15 * time.Second

var sharedHTTPClient = &http.Client{
	Timeout: 18 * time.Second,
	Transport: &http.Transport{
		TLSClientConfig:     &tls.Config{InsecureSkipVerify: true},
		ForceAttemptHTTP2:   true,
		MaxIdleConns:        100,
		MaxIdleConnsPerHost: 20,
		IdleConnTimeout:     90 * time.Second,
	},
}

var http11Client = &http.Client{
	Timeout: 18 * time.Second,
	Transport: &http.Transport{
		TLSClientConfig:     &tls.Config{InsecureSkipVerify: true},
		ForceAttemptHTTP2:   false,
		TLSNextProto:        make(map[string]func(authority string, c *tls.Conn) http.RoundTripper),
		MaxIdleConns:        100,
		MaxIdleConnsPerHost: 20,
		IdleConnTimeout:     90 * time.Second,
	},
}

var noRedirectHTTPClient = &http.Client{
	Timeout: 12 * time.Second,
	Transport: &http.Transport{
		TLSClientConfig:   &tls.Config{InsecureSkipVerify: true},
		ForceAttemptHTTP2: true,
	},
	CheckRedirect: func(req *http.Request, via []*http.Request) error {
		return http.ErrUseLastResponse
	},
}

// DefaultHeaders returns standard desktop browser headers.
func DefaultHeaders(referer string) map[string]string {
	h := map[string]string{
		"User-Agent":      config.DefaultUserAgent,
		"Accept":          "text/html,application/xhtml+xml,application/xml;q=0.9,*/*;q=0.8",
		"Accept-Language": "tr-TR,tr;q=0.9,en-US;q=0.8,en;q=0.7",
	}
	if referer != "" {
		h["Referer"] = referer
	}
	return h
}

// FetchBytes performs an HTTP request with custom headers and timeout.
func FetchBytes(ctx context.Context, method, targetURL string, headers map[string]string, body io.Reader) ([]byte, int, http.Header, string, error) {
	req, err := http.NewRequestWithContext(ctx, method, targetURL, body)
	if err != nil {
		return nil, 0, nil, "", err
	}
	req.Header.Set("User-Agent", config.DefaultUserAgent)
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	resp, err := sharedHTTPClient.Do(req)
	if err != nil {
		return nil, 0, nil, "", err
	}
	defer resp.Body.Close()

	data, err := io.ReadAll(io.LimitReader(resp.Body, 15*1024*1024))
	finalURL := targetURL
	if resp.Request != nil && resp.Request.URL != nil {
		finalURL = resp.Request.URL.String()
	}
	return data, resp.StatusCode, resp.Header, finalURL, err
}

// FetchString performs a GET request and returns body as string.
func FetchString(ctx context.Context, targetURL string, headers map[string]string) (string, int, error) {
	b, code, _, _, err := FetchBytes(ctx, http.MethodGet, targetURL, headers, nil)
	return string(b), code, err
}

// FetchJSON performs a GET request and decodes JSON into dest.
func FetchJSON(ctx context.Context, targetURL string, headers map[string]string, dest any) error {
	b, code, _, _, err := FetchBytes(ctx, http.MethodGet, targetURL, headers, nil)
	if err != nil {
		return err
	}
	if code < 200 || code >= 400 {
		return fmt.Errorf("HTTP %d from %s", code, targetURL)
	}
	return json.Unmarshal(b, dest)
}

// FetchJSONWithBody performs an HTTP request with body and decodes JSON response into dest.
func FetchJSONWithBody(ctx context.Context, method, targetURL string, headers map[string]string, body []byte, dest any) error {
	var bodyReader io.Reader
	if body != nil {
		bodyReader = bytes.NewReader(body)
	}
	b, code, _, _, err := FetchBytes(ctx, method, targetURL, headers, bodyReader)
	if err != nil {
		return err
	}
	if code < 200 || code >= 400 {
		return fmt.Errorf("HTTP %d from %s", code, targetURL)
	}
	return json.Unmarshal(b, dest)
}

// FetchBytesHTTP11 performs an HTTP request forcing HTTP/1.1 (bypasses Cloudflare HTTP/2 fingerprinting).
func FetchBytesHTTP11(ctx context.Context, method, targetURL string, headers map[string]string, body io.Reader) ([]byte, int, http.Header, string, error) {
	req, err := http.NewRequestWithContext(ctx, method, targetURL, body)
	if err != nil {
		return nil, 0, nil, "", err
	}
	req.Header.Set("User-Agent", config.DefaultUserAgent)
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	resp, err := http11Client.Do(req)
	if err != nil {
		return nil, 0, nil, "", err
	}
	defer resp.Body.Close()

	data, err := io.ReadAll(io.LimitReader(resp.Body, 15*1024*1024))
	finalURL := targetURL
	if resp.Request != nil && resp.Request.URL != nil {
		finalURL = resp.Request.URL.String()
	}
	return data, resp.StatusCode, resp.Header, finalURL, err
}

// FetchStringHTTP11 performs a GET request forcing HTTP/1.1 and returns body as string.
func FetchStringHTTP11(ctx context.Context, targetURL string, headers map[string]string) (string, int, error) {
	b, code, _, _, err := FetchBytesHTTP11(ctx, http.MethodGet, targetURL, headers, nil)
	return string(b), code, err
}

// FetchJSONHTTP11 performs a GET request forcing HTTP/1.1 and decodes JSON into dest.
func FetchJSONHTTP11(ctx context.Context, targetURL string, headers map[string]string, dest any) error {
	b, code, _, _, err := FetchBytesHTTP11(ctx, http.MethodGet, targetURL, headers, nil)
	if err != nil {
		return err
	}
	if code < 200 || code >= 400 {
		return fmt.Errorf("HTTP %d from %s", code, targetURL)
	}
	return json.Unmarshal(b, dest)
}

// FetchJSONWithBodyHTTP11 performs an HTTP request forcing HTTP/1.1 with body and decodes JSON response into dest.
func FetchJSONWithBodyHTTP11(ctx context.Context, method, targetURL string, headers map[string]string, body []byte, dest any) error {
	var bodyReader io.Reader
	if body != nil {
		bodyReader = bytes.NewReader(body)
	}
	b, code, _, _, err := FetchBytesHTTP11(ctx, method, targetURL, headers, bodyReader)
	if err != nil {
		return err
	}
	if code < 200 || code >= 400 {
		return fmt.Errorf("HTTP %d from %s", code, targetURL)
	}
	return json.Unmarshal(b, dest)
}

// PostForm performs an application/x-www-form-urlencoded POST request.
func PostForm(ctx context.Context, targetURL string, form url.Values, headers map[string]string) (string, int, error) {
	hdrs := make(map[string]string)
	for k, v := range headers {
		hdrs[k] = v
	}
	if _, ok := hdrs["Content-Type"]; !ok {
		hdrs["Content-Type"] = "application/x-www-form-urlencoded; charset=UTF-8"
	}
	b, code, _, _, err := FetchBytes(ctx, http.MethodPost, targetURL, hdrs, strings.NewReader(form.Encode()))
	return string(b), code, err
}

// PostJSON performs an application/json POST request.
func PostJSON(ctx context.Context, targetURL string, payload any, headers map[string]string) ([]byte, int, error) {
	raw, err := json.Marshal(payload)
	if err != nil {
		return nil, 0, err
	}
	hdrs := make(map[string]string)
	for k, v := range headers {
		hdrs[k] = v
	}
	hdrs["Content-Type"] = "application/json"
	b, code, _, _, err := FetchBytes(ctx, http.MethodPost, targetURL, hdrs, bytes.NewReader(raw))
	return b, code, err
}

// UltraClean normalizes Turkish & Latin characters into lowercase alphanumeric only.
func UltraClean(s string) string {
	s = FoldTurkish(s)
	var b strings.Builder
	for i := 0; i < len(s); i++ {
		ch := s[i]
		if (ch >= 'a' && ch <= 'z') || (ch >= '0' && ch <= '9') {
			b.WriteByte(ch)
		}
	}
	return b.String()
}

// FoldTurkish converts Turkish characters to ASCII lowercase equivalents.
func FoldTurkish(s string) string {
	replacer := strings.NewReplacer(
		"Ç", "c", "ç", "c",
		"Ğ", "g", "ğ", "g",
		"İ", "i", "I", "i", "ı", "i",
		"Ö", "o", "ö", "o",
		"Ş", "s", "ş", "s",
		"Ü", "u", "ü", "u",
		"Â", "a", "â", "a",
		"Î", "i", "î", "i",
		"Û", "u", "û", "u",
	)
	return strings.ToLower(replacer.Replace(s))
}

var nonSlugRe = regexp.MustCompile(`[^a-z0-9]+`)

// Slugify turns any title into a URL-safe hyphenated slug.
func Slugify(s string) string {
	folded := FoldTurkish(s)
	slug := nonSlugRe.ReplaceAllString(folded, "-")
	return strings.Trim(slug, "-")
}

var htmlTagRe = regexp.MustCompile(`<[^>]+>`)
var multiSpaceRe = regexp.MustCompile(`\s+`)

// CleanHTMLText strips HTML tags, unescapes HTML entities, and collapses whitespace.
func CleanHTMLText(s string) string {
	noTags := htmlTagRe.ReplaceAllString(s, " ")
	unescaped := html.UnescapeString(noTags)
	return strings.TrimSpace(multiSpaceRe.ReplaceAllString(unescaped, " "))
}

// ExtractYear extracts a 4-digit release year (1900-2035) from a title or date string.
var yearRe = regexp.MustCompile(`\b(19\d\d|20[0-3]\d)\b`)

func ExtractYear(s string) string {
	m := yearRe.FindStringSubmatch(s)
	if len(m) > 1 {
		return m[1]
	}
	return ""
}

// StripYearSuffix removes trailing "(2024)" or "izle" noise from titles.
var stripNoiseRe = regexp.MustCompile(`(?i)\s*(?:\((?:19|20)\d\d\)|full\s*hd\s*izle|film\s*izle|dizi\s*izle|hd\s*izle|türkçe\s*dublaj|altyazılı|izle)\s*$`)

func CleanMediaTitle(s string) string {
	s = CleanHTMLText(s)
	for {
		cleaned := stripNoiseRe.ReplaceAllString(s, "")
		if cleaned == s {
			break
		}
		s = strings.TrimSpace(cleaned)
	}
	return strings.TrimSpace(s)
}

// ResolveRelativeURL resolves a relative href against a base URL.
func ResolveRelativeURL(href, baseURL string) string {
	href = strings.TrimSpace(href)
	if href == "" {
		return ""
	}
	if strings.HasPrefix(href, "http://") || strings.HasPrefix(href, "https://") {
		return href
	}
	if strings.HasPrefix(href, "//") {
		return "https:" + href
	}
	base, err := url.Parse(baseURL)
	if err != nil {
		return strings.TrimRight(baseURL, "/") + "/" + strings.TrimLeft(href, "/")
	}
	rel, err := url.Parse(href)
	if err != nil {
		return strings.TrimRight(baseURL, "/") + "/" + strings.TrimLeft(href, "/")
	}
	return base.ResolveReference(rel).String()
}

// ── Quality Scoring & Sorting (Ported from src/shared/quality.js) ──────────

var (
	q4KRe   = regexp.MustCompile(`(?i)\b(4k|2160p?|uhd)\b`)
	q2KRe   = regexp.MustCompile(`(?i)\b(2k|1440p?|qhd)\b`)
	q1080Re = regexp.MustCompile(`(?i)\b(1080p?|fhd|full[\s-]?hd)\b`)
	q720Re  = regexp.MustCompile(`(?i)\b(720p?|hd)\b`)
	q540Re  = regexp.MustCompile(`(?i)\b(540p?)\b`)
	q480Re  = regexp.MustCompile(`(?i)\b(480p?|sd)\b`)
	q360Re  = regexp.MustCompile(`(?i)\b(360p?)\b`)
	q240Re  = regexp.MustCompile(`(?i)\b(240p?)\b`)
)

// GetQualityScore computes the numeric quality score for a StreamSource.
func GetQualityScore(s StreamSource) int {
	q := strings.ToLower(s.Quality + " " + s.Title + " " + s.Name + " " + s.URL)
	score := 0
	switch {
	case q4KRe.MatchString(q):
		score = 2160
	case q2KRe.MatchString(q):
		score = 1440
	case q1080Re.MatchString(q):
		score = 1080
	case q720Re.MatchString(q):
		score = 720
	case q540Re.MatchString(q):
		score = 540
	case q480Re.MatchString(q):
		score = 480
	case q360Re.MatchString(q):
		score = 360
	case q240Re.MatchString(q):
		score = 240
	default:
		score = 500
	}

	isDirectMP4 := s.Format == "mp4" || (!s.IsHLS && (strings.HasSuffix(strings.ToLower(s.URL), ".mp4") || strings.Contains(strings.ToLower(s.URL), ".mp4?")))
	if isDirectMP4 && score > 0 {
		score++
	}

	// Turkish language priority within same resolution:
	// Turkish Dubbed (+20) > Turkish Subtitled / Japanese (+15) > English / Other (0)
	lowTitle := strings.ToLower(s.Title)
	if strings.Contains(lowTitle, "türkçe dublaj") || strings.Contains(lowTitle, "tr dublaj") || strings.Contains(lowTitle, "tr dub") {
		score += 20
	} else if strings.Contains(lowTitle, "altyazı") || strings.Contains(lowTitle, "altyazi") || strings.Contains(lowTitle, "japonca") {
		score += 15
	}

	return score
}

// SortStreamsByQuality sorts a slice of StreamSource in descending order of quality.
func SortStreamsByQuality(streams []StreamSource) {
	sort.SliceStable(streams, func(i, j int) bool {
		return GetQualityScore(streams[i]) > GetQualityScore(streams[j])
	})
}

// ── Dean Edwards Packed JS Unpacker (Ported from src/shared/unpacker.js) ───

var packedJSRe = regexp.MustCompile(`(?s)\}\(\s*['"](.*?)['"]\s*,\s*(\d+)\s*,\s*(\d+)\s*,\s*['"](.*?)['"]\.split\(['"]\|['"]\)`)
var wordTokenRe = regexp.MustCompile(`\b\w+\b`)

const base62Alphabet = "0123456789abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ"

func encodeBaseN(n, radix int) string {
	if radix < 2 || radix > 62 {
		radix = 10
	}
	var prefix string
	if n >= radix {
		prefix = encodeBaseN(n/radix, radix)
	}
	rem := n % radix
	if rem > 35 {
		return prefix + string(rune(rem+29))
	}
	if rem < len(base62Alphabet) {
		return prefix + string(base62Alphabet[rem])
	}
	return prefix + strconv.Itoa(rem)
}

// UnpackJS unpacks Dean Edwards eval(function(p,a,c,k,e,d)...) blocks.
// If multiple blocks exist in the given string, it unpacks each and joins them.
func UnpackJS(packed string) string {
	matches := packedJSRe.FindAllStringSubmatch(packed, -1)
	if len(matches) == 0 {
		return ""
	}
	var results []string
	for _, m := range matches {
		if len(m) < 5 {
			continue
		}
		p := m[1]
		a, err1 := strconv.Atoi(m[2])
		c, err2 := strconv.Atoi(m[3])
		if err1 != nil || err2 != nil || a <= 0 {
			continue
		}
		k := strings.Split(m[4], "|")

		dict := make(map[string]string, c)
		for i := c - 1; i >= 0; i-- {
			key := encodeBaseN(i, a)
			val := key
			if i < len(k) && k[i] != "" {
				val = k[i]
			}
			dict[key] = val
		}

		unpacked := wordTokenRe.ReplaceAllStringFunc(p, func(tok string) string {
			if rep, ok := dict[tok]; ok {
				return rep
			}
			return tok
		})
		results = append(results, unpacked)
	}
	return strings.Join(results, "\n")
}

// ── Common Embed Extractors (Ported from src/shared/helpers.js & providers) ─

// ExtractTauVideo resolves https://tau-video.xyz/embed/{id} -> direct MP4 streams.
func ExtractTauVideo(ctx context.Context, embedURL string) []StreamSource {
	parts := strings.Split(strings.TrimRight(embedURL, "/"), "/")
	videoID := parts[len(parts)-1]
	if idx := strings.IndexByte(videoID, '?'); idx != -1 {
		videoID = videoID[:idx]
	}
	if videoID == "" {
		return nil
	}
	apiURL := "https://tau-video.xyz/api/video/" + videoID
	var data struct {
		URLs []struct {
			URL   string `json:"url"`
			Label string `json:"label"`
		} `json:"urls"`
	}
	if err := FetchJSON(ctx, apiURL, map[string]string{"Referer": "https://animecix.tv/"}, &data); err != nil {
		return nil
	}
	var out []StreamSource
	for _, u := range data.URLs {
		if u.URL == "" {
			continue
		}
		q := u.Label
		if q == "" {
			q = "1080p"
		}
		out = append(out, StreamSource{
			Name:    "TauVideo",
			Title:   "TauVideo • " + q,
			URL:     u.URL,
			Quality: q,
			Format:  "mp4",
			IsHLS:   false,
			Headers: map[string]string{
				"Referer":    "https://tau-video.xyz/",
				"User-Agent": config.DefaultUserAgent,
			},
		})
	}
	return out
}

var sibnetSrcRe = regexp.MustCompile(`player\.src\(\[\{src:\s*["']?([^"'\s>]+)`)

// ExtractSibnet resolves https://video.sibnet.ru/shell.php?videoid=... -> direct MP4 stream.
func ExtractSibnet(ctx context.Context, embedURL, referer string) []StreamSource {
	if referer == "" {
		referer = "https://video.sibnet.ru/"
	}
	htmlStr, _, err := FetchString(ctx, embedURL, DefaultHeaders(referer))
	if err != nil {
		return nil
	}
	m := sibnetSrcRe.FindStringSubmatch(htmlStr)
	if len(m) < 2 {
		return nil
	}
	streamURL := m[1]
	if strings.HasPrefix(streamURL, "/") {
		streamURL = "https://video.sibnet.ru" + streamURL
	}
	return []StreamSource{
		{
			Name:    "Sibnet",
			Title:   "Sibnet • 720p MP4",
			URL:     streamURL,
			Quality: "720p",
			Format:  "mp4",
			IsHLS:   false,
			Headers: map[string]string{
				"Referer":    "https://video.sibnet.ru/",
				"User-Agent": config.DefaultUserAgent,
			},
		},
	}
}

var okRuOptionsRe = regexp.MustCompile(`data-options="([^"]+)"`)

// ExtractOkRu resolves https://ok.ru/videoembed/... or odnoklassniki.ru/videoembed/...
func ExtractOkRu(ctx context.Context, embedURL string) []StreamSource {
	if strings.HasPrefix(embedURL, "//") {
		embedURL = "https:" + embedURL
	}
	htmlStr, _, err := FetchString(ctx, embedURL, DefaultHeaders("https://ok.ru/"))
	if err != nil {
		return nil
	}
	m := okRuOptionsRe.FindStringSubmatch(htmlStr)
	if len(m) < 2 {
		return nil
	}
	rawJSON := html.UnescapeString(m[1])
	var opts struct {
		Flashvars struct {
			Metadata any `json:"metadata"`
		} `json:"flashvars"`
	}
	if err := json.Unmarshal([]byte(rawJSON), &opts); err != nil {
		return nil
	}
	var meta struct {
		Videos []struct {
			Name string `json:"name"`
			URL  string `json:"url"`
		} `json:"videos"`
		HLSManifestURL string `json:"hlsManifestUrl"`
	}
	switch v := opts.Flashvars.Metadata.(type) {
	case string:
		_ = json.Unmarshal([]byte(v), &meta)
	default:
		b, _ := json.Marshal(v)
		_ = json.Unmarshal(b, &meta)
	}

	qMap := map[string]string{
		"ultra":  "2160p",
		"quad":   "1440p",
		"full":   "1080p",
		"hd":     "720p",
		"sd":     "480p",
		"low":    "360p",
		"lowest": "240p",
		"mobile": "240p",
	}
	var out []StreamSource
	for _, vid := range meta.Videos {
		if vid.URL == "" {
			continue
		}
		q := qMap[strings.ToLower(vid.Name)]
		if q == "" {
			q = "720p"
		}
		out = append(out, StreamSource{
			Name:    "OkRu",
			Title:   "OkRu • " + q,
			URL:     vid.URL,
			Quality: q,
			Format:  "mp4",
			IsHLS:   false,
			Headers: map[string]string{
				"Referer":    "https://ok.ru/",
				"User-Agent": config.DefaultUserAgent,
			},
		})
	}
	if len(out) == 0 && meta.HLSManifestURL != "" {
		out = append(out, StreamSource{
			Name:    "OkRu",
			Title:   "OkRu • HLS",
			URL:     meta.HLSManifestURL,
			Quality: "1080p",
			Format:  "hls",
			IsHLS:   true,
			Headers: map[string]string{
				"Referer":    "https://ok.ru/",
				"User-Agent": config.DefaultUserAgent,
			},
		})
	}
	return out
}

var vidMolyIDRe = regexp.MustCompile(`(?:embed-|/e/|/w/|/dl/)([a-zA-Z0-9]+)`)
var vidMolyFileRe = regexp.MustCompile(`file\s*:\s*["'](https?://[^"'\s<>]+\.m3u8[^"'\s<>]*)["']`)

// ExtractVidMoly resolves vidmoly.to / vidmoly.me / vidmoly.biz / vidmoly.net embeds.
func ExtractVidMoly(ctx context.Context, embedURL, referer string) []StreamSource {
	if strings.HasPrefix(embedURL, "//") {
		embedURL = "https:" + embedURL
	}
	normURL := embedURL
	if m := vidMolyIDRe.FindStringSubmatch(embedURL); len(m) > 1 {
		normURL = "https://vidmoly.biz/embed-" + m[1] + ".html"
	} else {
		normURL = strings.NewReplacer("vidmoly.to", "vidmoly.biz", "vidmoly.me", "vidmoly.biz", "vidmoly.net", "vidmoly.biz").Replace(embedURL)
	}
	if referer == "" {
		referer = "https://vidmoly.biz/"
	}
	htmlStr, _, err := FetchString(ctx, normURL, map[string]string{
		"User-Agent":     config.DefaultUserAgent,
		"Referer":        referer,
		"Sec-Fetch-Dest": "iframe",
	})
	if err != nil {
		return nil
	}
	m := vidMolyFileRe.FindStringSubmatch(htmlStr)
	if len(m) < 2 {
		return nil
	}
	subs := ExtractVTTSubtitles(htmlStr)
	return []StreamSource{
		{
			Name:    "VidMoly",
			Title:   "VidMoly • 1080p HLS",
			URL:     m[1],
			Quality: "1080p",
			Format:  "hls",
			IsHLS:   true,
			Headers: map[string]string{
				"Referer":    "https://vidmoly.biz/",
				"User-Agent": config.DefaultUserAgent,
			},
			Subtitles: subs,
		},
	}
}

var hdpHashRe = regexp.MustCompile(`(?:/video/|data=)([a-f0-9]{16,64})`)
var securedLinkRe = regexp.MustCompile(`"securedLink"\s*:\s*"([^"]+)"`)

// ExtractHDPlayerSystem resolves hdplayersystem.com / anizmplayer.com / vidpapi.xyz / filmizle.in FirePlayer embeds.
func ExtractHDPlayerSystem(ctx context.Context, embedURL, referer string) []StreamSource {
	if strings.HasPrefix(embedURL, "//") {
		embedURL = "https:" + embedURL
	}
	parsed, err := url.Parse(embedURL)
	if err != nil {
		return nil
	}
	origin := parsed.Scheme + "://" + parsed.Host

	hash := ""
	if m := hdpHashRe.FindStringSubmatch(embedURL); len(m) > 1 {
		hash = m[1]
	} else {
		parts := strings.Split(strings.TrimRight(parsed.Path, "/"), "/")
		hash = parts[len(parts)-1]
	}
	if hash == "" {
		return nil
	}

	// Optional initial GET to grab fireplayer_player cookie and subtitles
	var setCookie string
	var subs []Subtitle
	bodyBytes, _, respHdr, _, err := FetchBytes(ctx, http.MethodGet, embedURL, DefaultHeaders(referer), nil)
	if err == nil {
		subs = ExtractVTTSubtitles(string(bodyBytes))
		for _, c := range respHdr.Values("Set-Cookie") {
			if strings.Contains(c, "fireplayer_player=") {
				setCookie = strings.Split(c, ";")[0]
				break
			}
		}
	}

	apiURL := fmt.Sprintf("%s/player/index.php?data=%s&do=getVideo", origin, url.QueryEscape(hash))
	form := url.Values{}
	form.Set("hash", hash)
	form.Set("data", hash)
	form.Set("do", "getVideo")
	if referer != "" {
		form.Set("r", referer)
	} else {
		form.Set("r", origin+"/")
	}

	postHeaders := map[string]string{
		"User-Agent":       config.DefaultUserAgent,
		"Referer":          embedURL,
		"Origin":           origin,
		"X-Requested-With": "XMLHttpRequest",
		"Content-Type":     "application/x-www-form-urlencoded; charset=UTF-8",
	}
	if setCookie != "" {
		postHeaders["Cookie"] = setCookie
	}

	respText, _, err := PostForm(ctx, apiURL, form, postHeaders)
	if err != nil {
		return nil
	}
	m := securedLinkRe.FindStringSubmatch(respText)
	if len(m) < 2 {
		return nil
	}
	streamURL := strings.ReplaceAll(m[1], `\/`, `/`)
	return []StreamSource{
		{
			Name:    "HDPlayerSystem",
			Title:   "HDPlayerSystem • 1080p HLS",
			URL:     streamURL,
			Quality: "1080p",
			Format:  "hls",
			IsHLS:   true,
			Headers: map[string]string{
				"Referer":    origin + "/",
				"User-Agent": config.DefaultUserAgent,
			},
			Subtitles: subs,
		},
	}
}

var mailRuIDRe = regexp.MustCompile(`video/embed/([^"'\s<>?]+)`)

// ExtractMailRu resolves https://my.mail.ru/video/embed/{id}.
func ExtractMailRu(ctx context.Context, embedURL string) []StreamSource {
	m := mailRuIDRe.FindStringSubmatch(embedURL)
	if len(m) < 2 {
		return nil
	}
	vid := m[1]
	metaURL := "https://my.mail.ru/+/video/meta/" + vid
	var data struct {
		Videos []struct {
			URL string `json:"url"`
			Key string `json:"key"`
		} `json:"videos"`
	}
	if err := FetchJSON(ctx, metaURL, map[string]string{"Referer": "https://my.mail.ru/video/embed/" + vid}, &data); err != nil {
		return nil
	}
	var out []StreamSource
	for _, v := range data.Videos {
		u := v.URL
		if strings.HasPrefix(u, "//") {
			u = "https:" + u
		}
		if u == "" {
			continue
		}
		q := v.Key
		if q == "" {
			q = "720p"
		}
		out = append(out, StreamSource{
			Name:    "MailRu",
			Title:   "MailRu • " + q,
			URL:     u,
			Quality: q,
			Format:  "mp4",
			IsHLS:   false,
			Headers: map[string]string{
				"Referer":    "https://my.mail.ru/",
				"User-Agent": config.DefaultUserAgent,
			},
		})
	}
	return out
}

var rumbleIDRe = regexp.MustCompile(`rumble\.com/embed/([^/?#"'<>]+)`)

// ExtractRumble resolves https://rumble.com/embed/{id}.
func ExtractRumble(ctx context.Context, embedURL string) []StreamSource {
	m := rumbleIDRe.FindStringSubmatch(embedURL)
	if len(m) < 2 {
		return nil
	}
	apiURL := "https://rumble.com/embedJS/u3/?request=video&ver=2&v=" + m[1]
	var data struct {
		U struct {
			HLS struct {
				URL string `json:"url"`
			} `json:"hls"`
		} `json:"u"`
		UA struct {
			HLS struct {
				Auto struct {
					URL string `json:"url"`
				} `json:"auto"`
			} `json:"hls"`
		} `json:"ua"`
	}
	if err := FetchJSON(ctx, apiURL, map[string]string{"Referer": embedURL}, &data); err != nil {
		return nil
	}
	hlsURL := data.U.HLS.URL
	if hlsURL == "" {
		hlsURL = data.UA.HLS.Auto.URL
	}
	if hlsURL == "" {
		return nil
	}
	return []StreamSource{
		{
			Name:    "Rumble",
			Title:   "Rumble • 1080p HLS",
			URL:     hlsURL,
			Quality: "1080p",
			Format:  "hls",
			IsHLS:   true,
			Headers: map[string]string{
				"Referer":    "https://rumble.com/",
				"User-Agent": config.DefaultUserAgent,
			},
		},
	}
}

var mp4UploadSrcRe = regexp.MustCompile(`src:\s*["'](https?://[^"'\s<>]+\.mp4[^"'\s<>]*)["']`)

// ExtractMP4Upload resolves https://www.mp4upload.com/embed-{id}.html.
func ExtractMP4Upload(ctx context.Context, embedURL string) []StreamSource {
	htmlStr, _, err := FetchString(ctx, embedURL, DefaultHeaders("https://www.mp4upload.com/"))
	if err != nil {
		return nil
	}
	m := mp4UploadSrcRe.FindStringSubmatch(htmlStr)
	if len(m) < 2 {
		return nil
	}
	return []StreamSource{
		{
			Name:    "MP4Upload",
			Title:   "MP4Upload • 1080p MP4",
			URL:     m[1],
			Quality: "1080p",
			Format:  "mp4",
			IsHLS:   false,
			Headers: map[string]string{
				"Referer":    "https://www.mp4upload.com/",
				"User-Agent": config.DefaultUserAgent,
			},
		},
	}
}

var yourUploadIDRe = regexp.MustCompile(`yourupload\.com/embed/([a-zA-Z0-9]+)`)

// ExtractYourUpload resolves yourupload.com/embed/{id}.
func ExtractYourUpload(embedURL string) []StreamSource {
	m := yourUploadIDRe.FindStringSubmatch(embedURL)
	if len(m) < 2 {
		return nil
	}
	return []StreamSource{
		{
			Name:    "YourUpload",
			Title:   "YourUpload • 720p MP4",
			URL:     fmt.Sprintf("http://www.vidcache.net:8161/%s.mp4", m[1]),
			Quality: "720p",
			Format:  "mp4",
			IsHLS:   false,
			Headers: map[string]string{
				"Referer":    "https://www.yourupload.com/",
				"User-Agent": config.DefaultUserAgent,
			},
		},
	}
}

var dailymotionIDRe = regexp.MustCompile(`(?:dailymotion\.com/(?:embed/)?video/|dai\.ly/|daily\.php\?k=)([a-zA-Z0-9]+)`)

// ExtractDailymotion resolves Dailymotion video IDs or embed URLs to HLS.
func ExtractDailymotion(ctx context.Context, videoIDOrURL string) []StreamSource {
	vid := videoIDOrURL
	if m := dailymotionIDRe.FindStringSubmatch(videoIDOrURL); len(m) > 1 {
		vid = m[1]
	}
	if vid == "" {
		return nil
	}
	metaURL := fmt.Sprintf("https://www.dailymotion.com/player/metadata/video/%s", vid)
	var meta struct {
		Qualities map[string][]struct {
			Type string `json:"type"`
			URL  string `json:"url"`
		} `json:"qualities"`
	}
	if err := FetchJSON(ctx, metaURL, DefaultHeaders("https://www.dailymotion.com/"), &meta); err == nil {
		for _, qList := range meta.Qualities {
			for _, item := range qList {
				if item.URL != "" && (strings.Contains(item.Type, "mpegURL") || strings.Contains(item.URL, ".m3u8")) {
					return []StreamSource{
						{
							Name:    "Dailymotion",
							Title:   "Dailymotion • 1080p HLS",
							URL:     item.URL,
							Quality: "1080p",
							Format:  "hls",
							IsHLS:   true,
							Headers: map[string]string{
								"Referer":    "https://www.dailymotion.com/",
								"User-Agent": config.DefaultUserAgent,
							},
						},
					}
				}
			}
		}
	}
	return nil
}

var vttURLRe = regexp.MustCompile(`(?i)https?://[^\s"'\\<>]+\.vtt`)

// ExtractVTTSubtitles extracts all .vtt subtitle URLs from HTML or JS text.
func ExtractVTTSubtitles(content string) []Subtitle {
	matches := vttURLRe.FindAllString(content, -1)
	seen := make(map[string]bool)
	var subs []Subtitle
	for i, u := range matches {
		u = strings.ReplaceAll(u, `\/`, `/`)
		lower := strings.ToLower(u)
		if seen[u] || strings.Contains(lower, "thumb") || strings.Contains(lower, "sprite") || strings.Contains(lower, "preview") {
			continue
		}
		seen[u] = true
		lang := "tr"
		label := "Türkçe"
		if strings.Contains(lower, "eng") || strings.Contains(lower, "_en.") || strings.Contains(lower, "/en.") {
			lang = "en"
			label = "English"
		}
		subs = append(subs, Subtitle{
			ID:       fmt.Sprintf("sub_%d", i),
			Language: lang,
			Label:    label,
			Format:   "vtt",
			URL:      u,
		})
	}
	return subs
}

var (
	vttTimeRe      = regexp.MustCompile(`(\d{2}:\d{2}:\d{2})\.(\d{3})\s*-->\s*(\d{2}:\d{2}:\d{2})\.(\d{3})(?:[^\r\n]*)`)
	vttShortTimeRe = regexp.MustCompile(`(\d{2}:\d{2})\.(\d{3})\s*-->\s*(\d{2}:\d{2})\.(\d{3})(?:[^\r\n]*)`)
)

// VTTToSRT converts WebVTT subtitle text bytes into SubRip (.srt) subtitle bytes.
func VTTToSRT(vtt []byte) []byte {
	if len(vtt) == 0 {
		return nil
	}
	raw := string(vtt)
	raw = strings.ReplaceAll(raw, "\r\n", "\n")
	raw = strings.ReplaceAll(raw, "\r", "\n")
	lines := strings.Split(raw, "\n")

	var sb strings.Builder
	idx := 1
	inHeader := true
	var currentBlock []string

	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if inHeader {
			if strings.HasPrefix(trimmed, "WEBVTT") || strings.HasPrefix(trimmed, "NOTE") || strings.HasPrefix(trimmed, "STYLE") || strings.HasPrefix(trimmed, "REGION") || trimmed == "" {
				continue
			}
			inHeader = false
		}

		if trimmed == "" {
			if len(currentBlock) > 0 {
				sb.WriteString(strconv.Itoa(idx))
				sb.WriteString("\n")
				idx++
				for _, bLine := range currentBlock {
					sb.WriteString(bLine)
					sb.WriteString("\n")
				}
				sb.WriteString("\n")
				currentBlock = nil
			}
			continue
		}

		if vttTimeRe.MatchString(trimmed) {
			inHeader = false
			converted := vttTimeRe.ReplaceAllString(trimmed, "${1},${2} --> ${3},${4}")
			currentBlock = append(currentBlock, converted)
		} else if vttShortTimeRe.MatchString(trimmed) {
			inHeader = false
			converted := vttShortTimeRe.ReplaceAllString(trimmed, "00:${1},${2} --> 00:${3},${4}")
			currentBlock = append(currentBlock, converted)
		} else {
			if len(currentBlock) == 0 {
				if _, err := strconv.Atoi(trimmed); err == nil {
					continue
				}
			}
			currentBlock = append(currentBlock, trimmed)
		}
	}

	if len(currentBlock) > 0 {
		sb.WriteString(strconv.Itoa(idx))
		sb.WriteString("\n")
		for _, bLine := range currentBlock {
			sb.WriteString(bLine)
			sb.WriteString("\n")
		}
		sb.WriteString("\n")
	}

	return []byte(strings.TrimSpace(sb.String()) + "\n")
}

var genericM3U8Re = regexp.MustCompile(`(?i)https?://[^\s"'\\<>]+\.m3u8[^\s"'\\<>]*`)
var genericMP4Re = regexp.MustCompile(`(?i)(?:file|src)\s*:\s*["'](https?://[^\s"'\\<>]+\.mp4[^\s"'\\<>]*)["']`)

// ExtractDiziPodPlayer extracts HLS streams from player.dizipod.com/embed/...
func ExtractDiziPodPlayer(ctx context.Context, embedURL, referer string) []StreamSource {
	if strings.HasPrefix(embedURL, "//") {
		embedURL = "https:" + embedURL
	}
	htmlStr, _, err := FetchString(ctx, embedURL, map[string]string{
		"User-Agent": config.DefaultUserAgent,
		"Referer":    "https://dizipod.com/",
	})
	if err != nil || htmlStr == "" {
		return nil
	}
	unpacked := UnpackJS(htmlStr)
	combined := strings.ReplaceAll(htmlStr+"\n"+unpacked, `\`, "")

	m := regexp.MustCompile(`(?i)(https?://[a-zA-Z0-9.-]+\.tyuopix\.com[^\s"']+\.m3u8)`).FindStringSubmatch(combined)
	if len(m) > 1 {
		return []StreamSource{
			{
				Name:    "DiziPod",
				Title:   "DiziPod • 1080p HLS",
				URL:     m[1],
				Quality: "1080p",
				Format:  "hls",
				IsHLS:   true,
				Headers: map[string]string{
					"User-Agent": config.DefaultUserAgent,
					"Referer":    "https://player.dizipod.com/",
				},
			},
		}
	}
	for _, gm := range genericM3U8Re.FindAllString(combined, -1) {
		return []StreamSource{
			{
				Name:    "DiziPod",
				Title:   "DiziPod • 1080p HLS",
				URL:     gm,
				Quality: "1080p",
				Format:  "hls",
				IsHLS:   true,
				Headers: map[string]string{
					"User-Agent": config.DefaultUserAgent,
					"Referer":    "https://player.dizipod.com/",
				},
			},
		}
	}
	return nil
}

// ExtractAnyIframe inspects an embed URL and dispatches to the appropriate extractor or unpacks Dean Edwards JS.
func ExtractAnyIframe(ctx context.Context, iframeURL, referer string) []StreamSource {
	iframeURL = strings.TrimSpace(strings.ReplaceAll(iframeURL, `\/`, `/`))
	if strings.HasPrefix(iframeURL, "//") {
		iframeURL = "https:" + iframeURL
	}
	lower := strings.ToLower(iframeURL)
	switch {
	case strings.Contains(lower, "tau-video"):
		return ExtractTauVideo(ctx, iframeURL)
	case strings.Contains(lower, "sibnet.ru"):
		return ExtractSibnet(ctx, iframeURL, referer)
	case strings.Contains(lower, "ok.ru") || strings.Contains(lower, "odnoklassniki.ru"):
		return ExtractOkRu(ctx, iframeURL)
	case strings.Contains(lower, "vidmoly"):
		return ExtractVidMoly(ctx, iframeURL, referer)
	case strings.Contains(lower, "hdplayersystem") || strings.Contains(lower, "anizmplayer") || strings.Contains(lower, "vidpapi") || strings.Contains(lower, "filmizle.in") || strings.Contains(lower, "hdstreamable"):
		return ExtractHDPlayerSystem(ctx, iframeURL, referer)
	case strings.Contains(lower, "mail.ru"):
		return ExtractMailRu(ctx, iframeURL)
	case strings.Contains(lower, "rumble.com"):
		return ExtractRumble(ctx, iframeURL)
	case strings.Contains(lower, "mp4upload.com"):
		return ExtractMP4Upload(ctx, iframeURL)
	case strings.Contains(lower, "yourupload.com"):
		return ExtractYourUpload(iframeURL)
	case strings.Contains(lower, "dailymotion.com") || strings.Contains(lower, "dai.ly"):
		return ExtractDailymotion(ctx, iframeURL)
	case strings.Contains(lower, "player.dizipod.com") || strings.Contains(lower, "dizipod.com/embed") ||
		strings.Contains(lower, "player.dizibak.net") || strings.Contains(lower, "dizibak.net/embed"):
		return ExtractDiziPodPlayer(ctx, iframeURL, referer)
	case strings.Contains(lower, "git.php?id=") || strings.Contains(lower, "streambox1"):
		if ym := regexp.MustCompile(`(?:git\.php\?id=|v=|youtu\.be/|/embed/)([a-zA-Z0-9_-]{11})`).FindStringSubmatch(iframeURL); len(ym) > 1 {
			if s := ExtractYouTubeInnertube(ctx, ym[1]); s != nil {
				return []StreamSource{*s}
			}
		}
	case strings.Contains(lower, "youtube.com") || strings.Contains(lower, "youtu.be"):
		if s := ExtractYouTubeInnertube(ctx, iframeURL); s != nil {
			return []StreamSource{*s}
		}
		return nil
	}

	// Generic fetch + Dean Edwards unpack + regex scan
	htmlStr, _, err := FetchString(ctx, iframeURL, DefaultHeaders(referer))
	if err != nil || htmlStr == "" {
		return nil
	}
	unpacked := UnpackJS(htmlStr)
	combined := strings.ReplaceAll(htmlStr+"\n"+unpacked, `\/`, `/`)
	subs := ExtractVTTSubtitles(combined)

	parsed, _ := url.Parse(iframeURL)
	iframeOrigin := referer
	if parsed != nil && parsed.Host != "" {
		iframeOrigin = parsed.Scheme + "://" + parsed.Host + "/"
	}

	var out []StreamSource
	seen := make(map[string]bool)
	for _, m := range genericM3U8Re.FindAllString(combined, -1) {
		u := strings.ReplaceAll(m, `\/`, `/`)
		if seen[u] || strings.Contains(strings.ToLower(u), "preview") || strings.Contains(strings.ToLower(u), "thumb") {
			continue
		}
		seen[u] = true
		out = append(out, StreamSource{
			Name:    "HLS Player",
			Title:   "HLS • 1080p",
			URL:     u,
			Quality: "1080p",
			Format:  "hls",
			IsHLS:   true,
			Headers: map[string]string{
				"Referer":    iframeOrigin,
				"User-Agent": config.DefaultUserAgent,
			},
			Subtitles: subs,
		})
	}
	for _, m := range genericMP4Re.FindAllStringSubmatch(combined, -1) {
		if len(m) > 1 {
			u := strings.ReplaceAll(m[1], `\/`, `/`)
			if !seen[u] {
				seen[u] = true
				out = append(out, StreamSource{
					Name:    "MP4 Player",
					Title:   "MP4 • 1080p",
					URL:     u,
					Quality: "1080p",
					Format:  "mp4",
					IsHLS:   false,
					Headers: map[string]string{
						"Referer":    iframeOrigin,
						"User-Agent": config.DefaultUserAgent,
					},
					Subtitles: subs,
				})
			}
		}
	}
	if len(out) == 0 {
		for _, ifr := range regexp.MustCompile(`(?i)<iframe[^>]+src=["']([^"']+)["']`).FindAllStringSubmatch(htmlStr, 3) {
			nestedURL := ResolveRelativeURL(ifr[1], iframeURL)
			if nestedURL != iframeURL && !strings.Contains(nestedURL, "about:blank") {
				if sub := ExtractAnyIframe(ctx, nestedURL, iframeURL); len(sub) > 0 {
					out = append(out, sub...)
					break
				}
			}
		}
	}
	return out
}

// ── YouTube Innertube Resolver & Search (Ported from src/shared/ytmp4.js) ──

var ytIDExtractRe = regexp.MustCompile(`(?:youtube\.com/(?:embed/|watch\?v=|shorts/)|youtu\.be/|^)([a-zA-Z0-9_-]{11})(?:$|[^a-zA-Z0-9_-])`)

// ExtractYouTubeID extracts an 11-char YouTube video ID from a URL or ID string.
func ExtractYouTubeID(input string) string {
	input = strings.TrimSpace(strings.TrimPrefix(input, "youtube:video:"))
	if len(input) == 11 {
		return input
	}
	m := ytIDExtractRe.FindStringSubmatch(input)
	if len(m) > 1 {
		return m[1]
	}
	return ""
}

// ExtractYouTubeInnertube resolves a YouTube video ID to a direct HLS or MP4 stream via Android Innertube API.
func ExtractYouTubeInnertube(ctx context.Context, videoIDOrURL string) *StreamSource {
	videoID := ExtractYouTubeID(videoIDOrURL)
	if videoID == "" {
		return nil
	}
	innertubeKey := config.RemoteString("urls.youtube.innertube_key", "AIzaSyAO_FJ2SlqU8Q4STEHLGCilw_Y9_11qcW8")
	androidUA := "com.google.android.youtube/20.10.38 (Linux; U; Android 11) gzip"

	payload := map[string]any{
		"context": map[string]any{
			"client": map[string]any{
				"clientName":        "ANDROID",
				"clientVersion":     "20.10.38",
				"androidSdkVersion": 30,
				"userAgent":         androidUA,
				"hl":                "tr",
				"gl":                "TR",
			},
		},
		"videoId":        videoID,
		"contentCheckOk": true,
		"racyCheckOk":    true,
	}

	b, code, err := PostJSON(ctx, "https://www.youtube.com/youtubei/v1/player?key="+innertubeKey, payload, map[string]string{
		"User-Agent": androidUA,
	})
	if err != nil || code != http.StatusOK {
		return nil
	}

	var data struct {
		StreamingData struct {
			HLSManifestURL string `json:"hlsManifestUrl"`
			Formats        []struct {
				Itag         int    `json:"itag"`
				URL          string `json:"url"`
				QualityLabel string `json:"qualityLabel"`
				Height       int    `json:"height"`
			} `json:"formats"`
		} `json:"streamingData"`
		VideoDetails struct {
			Title  string `json:"title"`
			Author string `json:"author"`
		} `json:"videoDetails"`
	}
	if err := json.Unmarshal(b, &data); err != nil {
		return nil
	}

	if data.StreamingData.HLSManifestURL != "" {
		return &StreamSource{
			Name:    "YouTube",
			Title:   data.VideoDetails.Title + " • 1080p HLS",
			URL:     data.StreamingData.HLSManifestURL,
			Quality: "1080p",
			Format:  "hls",
			IsHLS:   true,
			Headers: map[string]string{
				"User-Agent": androidUA,
			},
		}
	}

	for _, f := range data.StreamingData.Formats {
		if f.URL != "" {
			q := f.QualityLabel
			if q == "" {
				q = "720p"
			}
			return &StreamSource{
				Name:    "YouTube",
				Title:   data.VideoDetails.Title + " • " + q,
				URL:     f.URL,
				Quality: q,
				Format:  "mp4",
				IsHLS:   false,
				Headers: map[string]string{
					"User-Agent": androidUA,
				},
			}
		}
	}
	return nil
}

// YouTubeSearchResult represents a single result from Innertube search.
type YouTubeSearchResult struct {
	VideoID     string
	Title       string
	Channel     string
	Duration    string
	DurationSec int
}

func parseDurationSeconds(s string) int {
	parts := strings.Split(strings.TrimSpace(s), ":")
	total := 0
	for _, p := range parts {
		n, err := strconv.Atoi(p)
		if err != nil {
			return 0
		}
		total = total*60 + n
	}
	return total
}

// SearchYouTubeInnertube searches YouTube via Innertube API and returns matching videos.
func SearchYouTubeInnertube(ctx context.Context, query string) []YouTubeSearchResult {
	innertubeKey := config.RemoteString("urls.youtube.innertube_key", "AIzaSyAO_FJ2SlqU8Q4STEHLGCilw_Y9_11qcW8")
	payload := map[string]any{
		"query": query,
		"context": map[string]any{
			"client": map[string]any{
				"clientName":    "WEB",
				"clientVersion": "2.20240313.00.00",
				"hl":            "tr",
				"gl":            "TR",
			},
		},
	}
	b, code, err := PostJSON(ctx, "https://www.youtube.com/youtubei/v1/search?key="+innertubeKey, payload, nil)
	if err != nil || code != http.StatusOK {
		return nil
	}

	// Extract videoRenderer blocks via regex or JSON walk for speed & resilience
	var root map[string]any
	if err := json.Unmarshal(b, &root); err != nil {
		return nil
	}

	var results []YouTubeSearchResult
	seen := make(map[string]bool)
	var walk func(node any)
	walk = func(node any) {
		switch v := node.(type) {
		case map[string]any:
			if vr, ok := v["videoRenderer"].(map[string]any); ok {
				vid, _ := vr["videoId"].(string)
				if vid != "" && !seen[vid] {
					seen[vid] = true
					title := extractRunsText(vr["title"])
					channel := extractRunsText(vr["ownerText"])
					dur := extractRunsText(vr["lengthText"])
					results = append(results, YouTubeSearchResult{
						VideoID:     vid,
						Title:       title,
						Channel:     channel,
						Duration:    dur,
						DurationSec: parseDurationSeconds(dur),
					})
				}
				return
			}
			for _, child := range v {
				walk(child)
			}
		case []any:
			for _, child := range v {
				walk(child)
			}
		}
	}
	walk(root)
	return results
}

func extractRunsText(v any) string {
	m, ok := v.(map[string]any)
	if !ok {
		return ""
	}
	if st, ok := m["simpleText"].(string); ok {
		return st
	}
	if runs, ok := m["runs"].([]any); ok && len(runs) > 0 {
		if r0, ok := runs[0].(map[string]any); ok {
			if txt, ok := r0["text"].(string); ok {
				return txt
			}
		}
	}
	return ""
}

// ── DYG Video API (Used by PuhuTV & Star TV) ───────────────────────────────

// ExtractDYGVideo resolves streams from Dogus Digital (dygvideo.dygdigital.com).
func ExtractDYGVideo(ctx context.Context, publisherID int, referenceID, referer string) []StreamSource {
	apiURL := fmt.Sprintf("https://dygvideo.dygdigital.com/api/video_info?akamai=true&PublisherId=%d&ReferenceId=%s&SecretKey=NtvApiSecret2014*", publisherID, url.QueryEscape(referenceID))
	var res struct {
		Data struct {
			Title   string         `json:"title"`
			HLSLink string         `json:"hls_link"`
			Flavors map[string]any `json:"flavors"`
			Tracks  []struct {
				File  string `json:"file"`
				URL   string `json:"url"`
				Label string `json:"label"`
				Lang  string `json:"lang"`
			} `json:"tracks"`
		} `json:"data"`
	}
	if err := FetchJSON(ctx, apiURL, DefaultHeaders(referer), &res); err != nil {
		return nil
	}

	var subs []Subtitle
	for i, tr := range res.Data.Tracks {
		file := tr.File
		if file == "" {
			file = tr.URL
		}
		if file != "" {
			lang := tr.Lang
			if lang == "" {
				lang = "tr"
			}
			lbl := tr.Label
			if lbl == "" {
				lbl = "Türkçe"
			}
			subs = append(subs, Subtitle{
				ID:       fmt.Sprintf("dyg_sub_%d", i),
				Language: lang,
				Label:    lbl,
				Format:   "vtt",
				URL:      file,
			})
		}
	}

	var out []StreamSource
	hlsURL := ""
	if h, ok := res.Data.Flavors["hls"].(string); ok && h != "" {
		hlsURL = h
	} else if res.Data.HLSLink != "" {
		hlsURL = res.Data.HLSLink
	}

	if hlsURL != "" {
		out = append(out, StreamSource{
			Name:      "PuhuTV / DYG",
			Title:     res.Data.Title + " • 1080p HLS",
			URL:       hlsURL,
			Quality:   "1080p",
			Format:    "hls",
			IsHLS:     true,
			Headers:   DefaultHeaders(referer),
			Subtitles: subs,
		})
	}

	// Also scan any other video links in flavors
	for _, val := range res.Data.Flavors {
		if s, ok := val.(string); ok && s != "" && s != hlsURL {
			if strings.Contains(s, ".mp4") {
				out = append(out, StreamSource{
					Name:      "PuhuTV / DYG",
					Title:     res.Data.Title + " • 1080p MP4",
					URL:       s,
					Quality:   "1080p",
					Format:    "mp4",
					IsHLS:     false,
					Headers:   DefaultHeaders(referer),
					Subtitles: subs,
				})
			} else if strings.Contains(s, ".m3u8") && hlsURL == "" {
				out = append(out, StreamSource{
					Name:      "PuhuTV / DYG",
					Title:     res.Data.Title + " • HLS",
					URL:       s,
					Quality:   "1080p",
					Format:    "hls",
					IsHLS:     true,
					Headers:   DefaultHeaders(referer),
					Subtitles: subs,
				})
			}
		}
	}
	return out
}

// ── TMDB Metadata & Catalog Helper ─────────────────────────────────────────

// TMDBInfo holds resolved title, original title, year, IMDb ID, and numeric TMDB ID.
type TMDBInfo struct {
	ID            string
	IMDbID        string
	TitleTR       string
	TitleOriginal string
	Year          string
	MediaType     MediaType
	Seasons       []Season
}

// ResolveTMDBInfo fetches Turkish and Original titles, release year, and season/episode counts from TMDB.
func ResolveTMDBInfo(ctx context.Context, rawID string, mediaType MediaType) (*TMDBInfo, error) {
	cleanID := strings.TrimSpace(rawID)
	cleanID = strings.TrimPrefix(cleanID, "tmdb:")
	if idx := strings.IndexByte(cleanID, ':'); idx != -1 {
		cleanID = cleanID[:idx]
	}
	if cleanID == "" {
		return nil, fmt.Errorf("empty TMDB ID")
	}

	apiKey := config.DefaultTMDBAPIKey
	isSeries := mediaType == MediaTypeSeries || mediaType == MediaTypeAnime

	if strings.HasPrefix(cleanID, "tt") {
		findURL := fmt.Sprintf("https://api.themoviedb.org/3/find/%s?api_key=%s&external_source=imdb_id&language=tr-TR", cleanID, apiKey)
		var fd struct {
			MovieResults []struct {
				ID          int    `json:"id"`
				Title       string `json:"title"`
				OrigTitle   string `json:"original_title"`
				ReleaseDate string `json:"release_date"`
			} `json:"movie_results"`
			TVResults []struct {
				ID           int    `json:"id"`
				Name         string `json:"name"`
				OrigName     string `json:"original_name"`
				FirstAirDate string `json:"first_air_date"`
			} `json:"tv_results"`
		}
		if err := FetchJSON(ctx, findURL, nil, &fd); err == nil {
			if isSeries && len(fd.TVResults) > 0 {
				tv := fd.TVResults[0]
				return ResolveTMDBInfo(ctx, strconv.Itoa(tv.ID), MediaTypeSeries)
			}
			if len(fd.MovieResults) > 0 {
				mv := fd.MovieResults[0]
				return &TMDBInfo{
					ID:            strconv.Itoa(mv.ID),
					IMDbID:        cleanID,
					TitleTR:       mv.Title,
					TitleOriginal: mv.OrigTitle,
					Year:          ExtractYear(mv.ReleaseDate),
					MediaType:     MediaTypeMovie,
				}, nil
			}
			if len(fd.TVResults) > 0 {
				tv := fd.TVResults[0]
				return ResolveTMDBInfo(ctx, strconv.Itoa(tv.ID), MediaTypeSeries)
			}
		}
	}

	endpoint := "movie"
	if isSeries {
		endpoint = "tv"
	}
	detailURL := fmt.Sprintf("https://api.themoviedb.org/3/%s/%s?api_key=%s&language=tr-TR&append_to_response=external_ids", endpoint, cleanID, apiKey)
	var d struct {
		ID           int    `json:"id"`
		Title        string `json:"title"`
		OrigTitle    string `json:"original_title"`
		Name         string `json:"name"`
		OrigName     string `json:"original_name"`
		ReleaseDate  string `json:"release_date"`
		FirstAirDate string `json:"first_air_date"`
		IMDbID       string `json:"imdb_id"`
		ExternalIDs  struct {
			IMDbID string `json:"imdb_id"`
		} `json:"external_ids"`
		Seasons []struct {
			SeasonNumber int    `json:"season_number"`
			EpisodeCount int    `json:"episode_count"`
			Name         string `json:"name"`
		} `json:"seasons"`
	}
	if err := FetchJSON(ctx, detailURL, nil, &d); err != nil {
		// If movie failed, try tv or vice versa
		altEndpoint := "tv"
		if isSeries {
			altEndpoint = "movie"
		}
		altURL := fmt.Sprintf("https://api.themoviedb.org/3/%s/%s?api_key=%s&language=tr-TR&append_to_response=external_ids", altEndpoint, cleanID, apiKey)
		if err2 := FetchJSON(ctx, altURL, nil, &d); err2 != nil {
			return nil, err
		}
		isSeries = altEndpoint == "tv"
	}

	info := &TMDBInfo{
		ID:        strconv.Itoa(d.ID),
		IMDbID:    d.IMDbID,
		MediaType: MediaTypeMovie,
	}
	if info.IMDbID == "" {
		info.IMDbID = d.ExternalIDs.IMDbID
	}
	if isSeries {
		info.MediaType = MediaTypeSeries
		info.TitleTR = d.Name
		info.TitleOriginal = d.OrigName
		info.Year = ExtractYear(d.FirstAirDate)
		for _, s := range d.Seasons {
			if s.SeasonNumber <= 0 || s.EpisodeCount <= 0 {
				continue
			}
			var eps []Episode
			for ep := 1; ep <= s.EpisodeCount; ep++ {
				eps = append(eps, Episode{
					ID:       fmt.Sprintf("%s:%d:%d", info.ID, s.SeasonNumber, ep),
					StreamID: fmt.Sprintf("tv:%s:%d:%d", info.ID, s.SeasonNumber, ep),
					Title:    fmt.Sprintf("Season %d Episode %d", s.SeasonNumber, ep),
					Season:   s.SeasonNumber,
					Number:   ep,
				})
			}
			info.Seasons = append(info.Seasons, Season{
				Number:   s.SeasonNumber,
				Name:     fmt.Sprintf("Season %02d", s.SeasonNumber),
				Episodes: eps,
			})
		}
	} else {
		info.TitleTR = d.Title
		info.TitleOriginal = d.OrigTitle
		info.Year = ExtractYear(d.ReleaseDate)
	}
	return info, nil
}

// TMDBPopularCatalog fetches popular movies and/or TV series from TMDB (used by TMDB-backed resolvers).
func TMDBPopularCatalog(ctx context.Context, includeMovies, includeTV bool, page int) ([]MediaItem, error) {
	if page < 1 {
		page = 1
	}
	var items []MediaItem
	if includeMovies {
		u := fmt.Sprintf("https://api.themoviedb.org/3/movie/popular?api_key=%s&language=tr-TR&page=%d", config.DefaultTMDBAPIKey, page)
		var res struct {
			Results []struct {
				ID          int    `json:"id"`
				Title       string `json:"title"`
				OrigTitle   string `json:"original_title"`
				ReleaseDate string `json:"release_date"`
				PosterPath  string `json:"poster_path"`
				Overview    string `json:"overview"`
			} `json:"results"`
		}
		if err := FetchJSON(ctx, u, nil, &res); err == nil {
			for _, r := range res.Results {
				title := r.Title
				if title == "" {
					title = r.OrigTitle
				}
				items = append(items, MediaItem{
					ID:          fmt.Sprintf("movie:%d", r.ID),
					Title:       title,
					Year:        ExtractYear(r.ReleaseDate),
					Type:        MediaTypeMovie,
					PosterURL:   "https://image.tmdb.org/t/p/w500" + r.PosterPath,
					Description: r.Overview,
				})
			}
		}
	}
	if includeTV {
		u := fmt.Sprintf("https://api.themoviedb.org/3/tv/popular?api_key=%s&language=tr-TR&page=%d", config.DefaultTMDBAPIKey, page)
		var res struct {
			Results []struct {
				ID           int    `json:"id"`
				Name         string `json:"name"`
				OrigName     string `json:"original_name"`
				FirstAirDate string `json:"first_air_date"`
				PosterPath   string `json:"poster_path"`
				Overview     string `json:"overview"`
			} `json:"results"`
		}
		if err := FetchJSON(ctx, u, nil, &res); err == nil {
			for _, r := range res.Results {
				title := r.Name
				if title == "" {
					title = r.OrigName
				}
				items = append(items, MediaItem{
					ID:          fmt.Sprintf("tv:%d", r.ID),
					Title:       title,
					Year:        ExtractYear(r.FirstAirDate),
					Type:        MediaTypeSeries,
					PosterURL:   "https://image.tmdb.org/t/p/w500" + r.PosterPath,
					Description: r.Overview,
				})
			}
		}
	}
	return items, nil
}

// TMDBSearchCatalog searches TMDB for movies and/or TV series.
func TMDBSearchCatalog(ctx context.Context, query string, includeMovies, includeTV bool) ([]MediaItem, error) {
	var items []MediaItem
	if includeMovies {
		u := fmt.Sprintf("https://api.themoviedb.org/3/search/movie?api_key=%s&language=tr-TR&query=%s", config.DefaultTMDBAPIKey, url.QueryEscape(query))
		var res struct {
			Results []struct {
				ID          int    `json:"id"`
				Title       string `json:"title"`
				OrigTitle   string `json:"original_title"`
				ReleaseDate string `json:"release_date"`
				PosterPath  string `json:"poster_path"`
				Overview    string `json:"overview"`
			} `json:"results"`
		}
		if err := FetchJSON(ctx, u, nil, &res); err == nil {
			for _, r := range res.Results {
				title := r.Title
				if title == "" {
					title = r.OrigTitle
				}
				items = append(items, MediaItem{
					ID:          fmt.Sprintf("movie:%d", r.ID),
					Title:       title,
					Year:        ExtractYear(r.ReleaseDate),
					Type:        MediaTypeMovie,
					PosterURL:   "https://image.tmdb.org/t/p/w500" + r.PosterPath,
					Description: r.Overview,
				})
			}
		}
	}
	if includeTV {
		u := fmt.Sprintf("https://api.themoviedb.org/3/search/tv?api_key=%s&language=tr-TR&query=%s", config.DefaultTMDBAPIKey, url.QueryEscape(query))
		var res struct {
			Results []struct {
				ID           int    `json:"id"`
				Name         string `json:"name"`
				OrigName     string `json:"original_name"`
				FirstAirDate string `json:"first_air_date"`
				PosterPath   string `json:"poster_path"`
				Overview     string `json:"overview"`
			} `json:"results"`
		}
		if err := FetchJSON(ctx, u, nil, &res); err == nil {
			for _, r := range res.Results {
				title := r.Name
				if title == "" {
					title = r.OrigName
				}
				items = append(items, MediaItem{
					ID:          fmt.Sprintf("tv:%d", r.ID),
					Title:       title,
					Year:        ExtractYear(r.FirstAirDate),
					Type:        MediaTypeSeries,
					PosterURL:   "https://image.tmdb.org/t/p/w500" + r.PosterPath,
					Description: r.Overview,
				})
			}
		}
	}
	return items, nil
}

// ── Shared Decoders for VidMody, Vidrame, Vidmoxy, SetPlay/FastPlay, CloseLoad ─

// VidmodyDecrypt decrypts VidMody hex+reverse+base64+sub(key) blocks.
func VidmodyDecrypt(hexStr string, key int) string {
	rawHex, err := hex.DecodeString(hexStr)
	if err != nil {
		return ""
	}
	// Reverse bytes
	for i, j := 0, len(rawHex)-1; i < j; i, j = i+1, j-1 {
		rawHex[i], rawHex[j] = rawHex[j], rawHex[i]
	}
	decoded, err := base64.StdEncoding.DecodeString(string(rawHex))
	if err != nil {
		decoded, err = base64.RawStdEncoding.DecodeString(strings.TrimRight(string(rawHex), "="))
		if err != nil {
			return ""
		}
	}
	out := make([]byte, len(decoded))
	for i, b := range decoded {
		out[i] = byte((int(b) - key + 256) % 256)
	}
	res := string(out)
	hexEscRe := regexp.MustCompile(`\\x([0-9a-fA-F]{2})`)
	return hexEscRe.ReplaceAllStringFunc(res, func(m string) string {
		v, err := strconv.ParseUint(m[2:], 16, 8)
		if err != nil {
			return m
		}
		return string(byte(v))
	})
}

// SPGDecode decodes SPG.cerceve(n, o) XOR base64 strings used by SetFilmIzle and HDFilmIzle.
func SPGDecode(n, o string) string {
	cleanN := strings.ReplaceAll(strings.ReplaceAll(n, `\/`, `/`), `\`, "")
	cleanO := strings.ReplaceAll(strings.ReplaceAll(o, `\/`, `/`), `\`, "")
	a, err1 := base64.StdEncoding.DecodeString(cleanN)
	b, err2 := base64.StdEncoding.DecodeString(cleanO)
	if err1 != nil || err2 != nil || len(b) == 0 {
		return ""
	}
	out := make([]byte, len(a))
	for i := 0; i < len(a); i++ {
		out[i] = a[i] ^ b[i%len(b)]
	}
	parts := strings.Split(string(out), "|")
	return parts[0]
}

// DecodeVidrameXOR decodes Vidrame XOR arrays.
func DecodeVidrameXOR(d []int, k string) string {
	if len(d) == 0 || len(k) == 0 {
		return ""
	}
	out := make([]byte, len(d))
	for i, v := range d {
		out[i] = byte(v ^ int(k[i%len(k)]) ^ ((i*17 + 13) & 255))
	}
	return string(out)
}

// DecodeVidmoxy decodes Vidmoxy base64 + reverse + ROT13 strings.
func DecodeVidmoxy(encoded string) string {
	b, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil {
		return ""
	}
	for i, j := 0, len(b)-1; i < j; i, j = i+1, j-1 {
		b[i], b[j] = b[j], b[i]
	}
	for i, ch := range b {
		switch {
		case ch >= 'A' && ch <= 'Z':
			b[i] = ((ch - 'A' + 13) % 26) + 'A'
		case ch >= 'a' && ch <= 'z':
			b[i] = ((ch - 'a' + 13) % 26) + 'a'
		}
	}
	return string(b)
}

var (
	fastplayManRe  = regexp.MustCompile(`(?:src|stream)\s*:\s*"(/manifests/[^"]+)"`)
	fastplaySpRe   = regexp.MustCompile(`"sp"\s*:\s*"([^"]+)"`)
	fastplaySpTRe  = regexp.MustCompile(`"spT"\s*:\s*(\d+)`)
	fastplaySubsRe = regexp.MustCompile(`subtitles\s*:\s*(\[[\s\S]*?\])`)
)

func BuildFastPlayXSp(sp string, spT int64) string {
	n := spT
	r := strconv.FormatInt(rand.Int63n(2176782336), 36)
	toHash := fmt.Sprintf("%s|%d|%s", sp, n, r)
	var t uint32 = 2166136261
	for i := 0; i < len(toHash); i++ {
		t ^= uint32(toHash[i])
		t = t * 16777619
	}
	h := fmt.Sprintf("%x", t)
	return fmt.Sprintf("%d.%s.%s", n, r, h)
}

// ResolveFastPlay resolves fastplay.*/stfplay.php manifests and subtitles.
func ResolveFastPlay(ctx context.Context, fastplayURL, referer string) (string, []Subtitle, string, map[string]string) {
	bodyBytes, _, _, finalURL, err := FetchBytes(ctx, http.MethodGet, fastplayURL, DefaultHeaders(referer), nil)
	if err != nil {
		return "", nil, "", nil
	}
	fpHTML := string(bodyBytes)
	m := fastplayManRe.FindStringSubmatch(fpHTML)
	if len(m) < 2 {
		return "", nil, "", nil
	}
	parsed, err := url.Parse(fastplayURL)
	if err != nil {
		return "", nil, "", nil
	}
	origin := parsed.Scheme + "://" + parsed.Host
	manifestURL := origin + strings.ReplaceAll(m[1], "&amp;", "&")
	subs := ExtractVTTSubtitles(fpHTML)
	if finalURL == "" {
		finalURL = fastplayURL
	}

	headers := map[string]string{
		"Referer":    finalURL,
		"User-Agent": config.DefaultUserAgent,
	}

	spM := fastplaySpRe.FindStringSubmatch(fpHTML)
	sptM := fastplaySpTRe.FindStringSubmatch(fpHTML)
	if len(spM) > 1 && len(sptM) > 1 {
		sptVal, _ := strconv.ParseInt(sptM[1], 10, 64)
		if sptVal > 0 {
			headers["X-Sp"] = BuildFastPlayXSp(spM[1], sptVal)
		}
	}

	return manifestURL, subs, finalURL, headers
}

// BuildAniziumCfControl generates the XOR hex Cf-Control header required by api.anizium.co.
func BuildAniziumCfControl() string {
	weekdays := []string{"sunday", "monday", "tuesday", "wednesday", "thursday", "friday", "saturday"}
	loc, err := time.LoadLocation("Europe/Istanbul")
	if err != nil {
		loc = time.FixedZone("TRT", 3*3600)
	}
	now := time.Now().In(loc)
	dayName := weekdays[int(now.Weekday())]
	secretKey := "hlxjl1c2w281ax473rt1ofgrvhyjvi_" + dayName

	const chars = "abcdefghijklmnopqrstuvwxyz0123456789"
	var rk strings.Builder
	for i := 0; i < 6; i++ {
		rk.WriteByte(chars[rand.Intn(len(chars))])
	}
	payload := fmt.Sprintf(`{"%s":%d}`, rk.String(), now.UnixMilli())
	var hexOut strings.Builder
	for i := 0; i < len(payload); i++ {
		b := payload[i] ^ secretKey[i%len(secretKey)]
		fmt.Fprintf(&hexOut, "%02x", b)
	}
	return hexOut.String()
}
