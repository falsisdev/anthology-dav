package providers

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"regexp"
	"strconv"
	"strings"

	"anthology-dav/config"
)

type DiziMomProvider struct{}

func init() {
	Register(&DiziMomProvider{})
}

func (p *DiziMomProvider) ID() string   { return "dizimom" }
func (p *DiziMomProvider) Name() string { return "DiziMom" }

func (p *DiziMomProvider) baseURL() string {
	u := config.RemoteString("urls.series.dizimom.base", "https://ddizimom.net")
	if u == "" || strings.Contains(u, "dizidad.com") {
		return "https://ddizimom.net"
	}
	return strings.TrimRight(u, "/")
}

var (
	diziMomCardRe   = regexp.MustCompile(`(?is)<a[^>]+href=["'](?:https?://[^"'/]+)?/dizi/([^"'/?#]+)["'][^>]*>(.*?)</a>`)
	diziMomTitleRe  = regexp.MustCompile(`(?is)<span[^>]+class=["'][^"']*title[^"']*["'][^>]*>(.*?)</span>`)
	diziMomEpRe     = regexp.MustCompile(`(?i)href=["']((?:https?://[^"'/]+)?/dizi/[^"'/?#]+/sezon-(\d+)/bolum-(\d+))["']`)
	diziMomDataPvRe = regexp.MustCompile(`(?is)<button[^>]+class=["'][^"']*source-btn[^"']*["'][^>]*data-pv=["']([^"']+)["'][^>]*>(.*?)</button>`)
	diziMomPvAttrRe = regexp.MustCompile(`(?i)data-pv=["']([^"']+)["']`)
	diziMomPlayerRe = regexp.MustCompile(`(?s)window\.__PLAYER__\s*=\s*(\{.*?\});`)
)

func (p *DiziMomProvider) parseCards(htmlStr, base string) []MediaItem {
	var items []MediaItem
	seen := make(map[string]bool)
	for _, m := range diziMomCardRe.FindAllStringSubmatch(htmlStr, -1) {
		slug := strings.Trim(m[1], "/")
		if slug == "" || seen[slug] || strings.Contains(slug, "/sezon-") {
			continue
		}
		inner := m[2]
		title := ""
		if tm := diziMomTitleRe.FindStringSubmatch(inner); len(tm) > 1 {
			title = CleanMediaTitle(tm[1])
		}
		if title == "" {
			title = CleanMediaTitle(inner)
		}
		if title == "" || strings.EqualFold(title, "Dizi izle") {
			continue
		}
		seen[slug] = true
		poster := ""
		if im := regexp.MustCompile(`(?i)<img[^>]+(?:data-src|src)=["']([^"']+)["']`).FindStringSubmatch(inner); len(im) > 1 {
			poster = ResolveRelativeURL(im[1], base)
		}
		items = append(items, MediaItem{
			ID:        slug,
			Title:     title,
			Year:      ExtractYear(title),
			Type:      MediaTypeSeries,
			PosterURL: poster,
		})
	}
	return items
}

func (p *DiziMomProvider) GetCatalog(ctx context.Context, page int) ([]MediaItem, error) {
	base := p.baseURL()
	u := base + "/diziler"
	if page > 1 {
		u = fmt.Sprintf("%s/diziler?s_page=%d", base, page)
	}
	htmlStr, _, err := FetchString(ctx, u, DefaultHeaders(base+"/"))
	if err != nil {
		return nil, err
	}
	return p.parseCards(htmlStr, base), nil
}

func (p *DiziMomProvider) Search(ctx context.Context, query string) ([]MediaItem, error) {
	base := p.baseURL()
	htmlStr, _, err := FetchString(ctx, fmt.Sprintf("%s/ara?q=%s", base, url.QueryEscape(strings.TrimSpace(query))), DefaultHeaders(base+"/"))
	if err != nil {
		return nil, err
	}
	return p.parseCards(htmlStr, base), nil
}

func (p *DiziMomProvider) GetDetails(ctx context.Context, mediaID string) (*MediaDetail, error) {
	slug := strings.Trim(strings.TrimPrefix(mediaID, "dizimom:"), "/")
	slug = strings.TrimPrefix(slug, "dizi/")
	base := p.baseURL()
	seriesURL := fmt.Sprintf("%s/dizi/%s", base, slug)
	htmlStr, _, err := FetchString(ctx, seriesURL, DefaultHeaders(base+"/"))
	if err != nil {
		return nil, err
	}
	title := slug
	if m := regexp.MustCompile(`(?is)<h1[^>]*>(.*?)</h1>`).FindStringSubmatch(htmlStr); len(m) > 1 {
		title = CleanMediaTitle(m[1])
	}

	seasonMap := make(map[int][]Episode)
	seen := make(map[string]bool)
	for _, m := range diziMomEpRe.FindAllStringSubmatch(htmlStr, -1) {
		epURL := ResolveRelativeURL(m[1], base)
		sNum, _ := strconv.Atoi(m[2])
		eNum, _ := strconv.Atoi(m[3])
		if sNum <= 0 {
			sNum = 1
		}
		if eNum <= 0 {
			continue
		}
		key := fmt.Sprintf("%d_%d", sNum, eNum)
		if seen[key] {
			continue
		}
		seen[key] = true
		seasonMap[sNum] = append(seasonMap[sNum], Episode{
			ID:       epURL,
			StreamID: epURL,
			Title:    fmt.Sprintf("%d. Sezon %d. Bölüm", sNum, eNum),
			Season:   sNum,
			Number:   eNum,
		})
	}
	if len(seasonMap) == 0 {
		epURL := fmt.Sprintf("%s/dizi/%s/sezon-1/bolum-1", base, slug)
		seasonMap[1] = []Episode{
			{
				ID:       epURL,
				StreamID: epURL,
				Title:    "1. Sezon 1. Bölüm",
				Season:   1,
				Number:   1,
			},
		}
	}

	var seasons []Season
	for sNum, eps := range seasonMap {
		seasons = append(seasons, Season{
			Number:   sNum,
			Name:     fmt.Sprintf("Season %02d", sNum),
			Episodes: eps,
		})
	}
	return &MediaDetail{
		MediaItem: MediaItem{
			ID:    slug,
			Title: title,
			Type:  MediaTypeSeries,
		},
		Seasons: seasons,
	}, nil
}

func (p *DiziMomProvider) GetEpisodes(ctx context.Context, mediaID string, season int) ([]Episode, error) {
	det, err := p.GetDetails(ctx, mediaID)
	if err != nil {
		return nil, err
	}
	for _, s := range det.Seasons {
		if s.Number == season {
			return s.Episodes, nil
		}
	}
	return nil, nil
}

func (p *DiziMomProvider) resolvePilavyerPlay(ctx context.Context, pv, label string, aType AudioType, pageSubs []Subtitle) []StreamSource {
	playHost := "https://pilavyerplay.top"
	embedURL := fmt.Sprintf("%s/assets/js/s.php?s=%s", playHost, url.QueryEscape(pv))
	htmlStr, _, err := FetchString(ctx, embedURL, map[string]string{
		"User-Agent": config.DefaultUserAgent,
		"Referer":    playHost + "/",
	})
	if err != nil || htmlStr == "" {
		return nil
	}
	m := diziMomPlayerRe.FindStringSubmatch(htmlStr)
	if len(m) < 2 {
		return nil
	}
	var cfg struct {
		Stream string `json:"stream"`
		Subs   []struct {
			File  string `json:"file"`
			Label string `json:"label"`
			Kind  string `json:"kind"`
		} `json:"subs"`
	}
	if err := json.Unmarshal([]byte(m[1]), &cfg); err != nil || cfg.Stream == "" {
		return nil
	}
	streamURL := ResolveRelativeURL(cfg.Stream, playHost)
	var subs []Subtitle
	for _, sub := range cfg.Subs {
		if sub.File == "" {
			continue
		}
		lang := "tr"
		lblLower := strings.ToLower(sub.Label)
		if strings.Contains(lblLower, "ingilizce") || strings.Contains(lblLower, "english") {
			lang = "en"
		}
		subs = append(subs, Subtitle{
			Language: lang,
			Label:    sub.Label,
			Format:   "vtt",
			URL:      ResolveRelativeURL(sub.File, playHost),
		})
	}
	if len(subs) == 0 {
		subs = pageSubs
	}
	dubTag := "[TR Altyazı]"
	if aType == AudioTypeDub {
		dubTag = "[TR Dublaj]"
	}
	if label == "" {
		label = "PilavyerPlay"
	}
	return []StreamSource{
		{
			Name:      "DiziMom - " + label,
			Title:     fmt.Sprintf("DiziMom • %s • %s • 1080p", label, dubTag),
			URL:       streamURL,
			Quality:   "1080p",
			Format:    "hls",
			IsHLS:     true,
			AudioType: aType,
			Headers: map[string]string{
				"User-Agent": config.DefaultUserAgent,
				"Referer":    playHost + "/",
				"Origin":     playHost,
			},
			Subtitles: subs,
		},
	}
}

func (p *DiziMomProvider) ResolveStream(ctx context.Context, streamID string) (*StreamResult, error) {
	epURL := strings.TrimPrefix(streamID, "dizimom:")
	base := p.baseURL()
	if !strings.HasPrefix(epURL, "http") {
		epURL = fmt.Sprintf("%s/dizi/%s", base, strings.TrimPrefix(epURL, "/"))
	}
	htmlStr, _, err := FetchString(ctx, epURL, DefaultHeaders(base+"/"))
	if err != nil {
		return nil, err
	}

	var sources []StreamSource
	seenPV := make(map[string]bool)

	pageSubs := ExtractSubtitles(htmlStr, base)
	aType := DetectAudioType(htmlStr)
	if aType == AudioTypeUnknown {
		aType = AudioTypeSub
	}

	for _, m := range diziMomDataPvRe.FindAllStringSubmatch(htmlStr, -1) {
		pv := strings.TrimSpace(m[1])
		if pv == "" || seenPV[pv] {
			continue
		}
		seenPV[pv] = true
		label := CleanHTMLText(m[2])
		sources = append(sources, p.resolvePilavyerPlay(ctx, pv, label, aType, pageSubs)...)
	}
	for _, m := range diziMomPvAttrRe.FindAllStringSubmatch(htmlStr, -1) {
		pv := strings.TrimSpace(m[1])
		if pv == "" || seenPV[pv] {
			continue
		}
		seenPV[pv] = true
		sources = append(sources, p.resolvePilavyerPlay(ctx, pv, "Player", aType, pageSubs)...)
	}

	// Fallback to any iframes if present
	seenURLs := make(map[string]bool)
	for _, im := range regexp.MustCompile(`(?i)<iframe[^>]+src="([^"]+)"`).FindAllStringSubmatch(htmlStr, 5) {
		u := strings.TrimSpace(im[1])
		if u == "" || u == "about:blank" || seenURLs[u] || strings.Contains(u, "youtube.com") {
			continue
		}
		seenURLs[u] = true
		sources = append(sources, ExtractAnyIframe(ctx, ResolveRelativeURL(u, base), epURL)...)
	}

	return SelectBestStream(sources)
}
