package providers

import (
	"context"
	"fmt"
	"net/url"
	"regexp"
	"strconv"
	"strings"

	"anthology-dav/config"
)

type DiziYouProvider struct{}

func init() {
	Register(&DiziYouProvider{})
}

func (p *DiziYouProvider) ID() string   { return "diziyou" }
func (p *DiziYouProvider) Name() string { return "DiziYou" }

func (p *DiziYouProvider) baseURL() string {
	return config.RemoteString("urls.series_foreign.diziyou.base", "https://www.diziyou.one")
}

func (p *DiziYouProvider) storageURL() string {
	return config.RemoteString("urls.series_foreign.diziyou.storage", "https://storage.diziyou.one")
}

var (
	diziYouDataFetchRe = regexp.MustCompile(`(?is)<div id="searchelement">.*?<a[^>]+href="(?:https?://[^"]*diziyou\.[^/]+)?/([a-z0-9-]+)/?"[^>]*>.*?<a[^>]+href="[^"]*"[^>]*>([^<]+)</a>(?:.*?<div id="search-cat-year">(\d+)</div>)?`)
	diziYouGeneralRe   = regexp.MustCompile(`(?is)<a[^>]+href="(?:https?://[^"]*diziyou\.[^/]+)?/([a-z0-9-]+)/?"[^>]*>([^<]+)</a>`)
	diziYouEpRe        = regexp.MustCompile(`(?i)href="https?://[^"]*diziyou\.[^/]+/([a-z0-9-]+)-(\d+)-sezon-(\d+)-bolum/?"`)
	diziYouPlayerRe    = regexp.MustCompile(`(?i)<iframe[^>]+id="diziyouPlayer"[^>]+src="([^"]+)"`)
	diziYouItemIDRe    = regexp.MustCompile(`/(\d+)\.html`)
)

func (p *DiziYouProvider) parseSeriesLinks(htmlStr string) []MediaItem {
	var items []MediaItem
	seen := make(map[string]bool)

	// First try structured data_fetch results
	for _, m := range diziYouDataFetchRe.FindAllStringSubmatch(htmlStr, -1) {
		slug := strings.Trim(m[1], "/")
		if slug == "" || seen[slug] || strings.Contains(slug, "-sezon-") || slug == "kategori" {
			continue
		}
		seen[slug] = true
		title := CleanMediaTitle(m[2])
		year := ""
		if len(m) > 3 && m[3] != "" {
			year = m[3]
		}
		items = append(items, MediaItem{
			ID:    slug,
			Title: title,
			Year:  year,
			Type:  MediaTypeSeries,
		})
	}
	if len(items) > 0 {
		return items
	}

	// Fallback to general links
	for _, m := range diziYouGeneralRe.FindAllStringSubmatch(htmlStr, -1) {
		slug := strings.Trim(m[1], "/")
		if slug == "" || seen[slug] || strings.Contains(slug, "-sezon-") || slug == "kategori" || slug == "feed" || slug == "uye-ol" || slug == "wp-login" {
			continue
		}
		title := CleanMediaTitle(m[2])
		if title == "" || strings.EqualFold(title, "Diziyou") || strings.Contains(title, "İzle") && len(title) < 8 {
			continue
		}
		seen[slug] = true
		items = append(items, MediaItem{
			ID:    slug,
			Title: title,
			Year:  ExtractYear(m[2]),
			Type:  MediaTypeSeries,
		})
	}
	return items
}

func (p *DiziYouProvider) GetCatalog(ctx context.Context, page int) ([]MediaItem, error) {
	base := p.baseURL()
	htmlStr, _, err := FetchString(ctx, base+"/", DefaultHeaders(base+"/"))
	if err == nil {
		items := p.parseSeriesLinks(htmlStr)
		if len(items) > 0 {
			return items, nil
		}
	}
	return TMDBPopularCatalog(ctx, false, true, page)
}

func (p *DiziYouProvider) Search(ctx context.Context, query string) ([]MediaItem, error) {
	base := p.baseURL()

	// 1. Try admin-ajax.php data_fetch (fast and accurate)
	ajaxURL := fmt.Sprintf("%s/wp-admin/admin-ajax.php", base)
	form := url.Values{}
	form.Set("action", "data_fetch")
	form.Set("keyword", strings.TrimSpace(query))
	respStr, _, err := PostForm(ctx, ajaxURL, form, map[string]string{
		"User-Agent":       config.DefaultUserAgent,
		"Referer":          base + "/",
		"Origin":           base,
		"X-Requested-With": "XMLHttpRequest",
	})
	if err == nil && len(respStr) > 0 {
		if items := p.parseSeriesLinks(respStr); len(items) > 0 {
			return items, nil
		}
	}

	// 2. Fallback to /?s=
	u := fmt.Sprintf("%s/?s=%s", base, url.QueryEscape(strings.TrimSpace(query)))
	htmlStr, _, err := FetchString(ctx, u, DefaultHeaders(base+"/"))
	if err != nil {
		return nil, err
	}
	return p.parseSeriesLinks(htmlStr), nil
}

func (p *DiziYouProvider) GetDetails(ctx context.Context, mediaID string) (*MediaDetail, error) {
	cleanID := strings.TrimPrefix(mediaID, "diziyou:")
	if strings.HasPrefix(cleanID, "tv:") || strings.HasPrefix(cleanID, "movie:") {
		info, err := ResolveTMDBInfo(ctx, strings.Split(cleanID, ":")[1], MediaTypeSeries)
		if err != nil {
			return nil, err
		}
		results, _ := p.Search(ctx, info.TitleOriginal)
		if len(results) == 0 && info.TitleTR != "" {
			results, _ = p.Search(ctx, info.TitleTR)
		}
		if len(results) > 0 {
			cleanID = results[0].ID
		}
	}

	base := p.baseURL()
	seriesURL := fmt.Sprintf("%s/%s/", base, strings.Trim(cleanID, "/"))
	htmlStr, _, err := FetchString(ctx, seriesURL, DefaultHeaders(base+"/"))
	if err != nil {
		return nil, err
	}

	title := cleanID
	if m := regexp.MustCompile(`(?is)<h1[^>]*>(.*?)</h1>`).FindStringSubmatch(htmlStr); len(m) > 1 {
		title = CleanMediaTitle(m[1])
	}

	seasonMap := make(map[int][]Episode)
	seen := make(map[string]bool)
	for _, m := range diziYouEpRe.FindAllStringSubmatch(htmlStr, -1) {
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
			ID:       fmt.Sprintf("%s:%d:%d", cleanID, sNum, eNum),
			StreamID: fmt.Sprintf("%s:%d:%d", cleanID, sNum, eNum),
			Title:    fmt.Sprintf("%d. Sezon %d. Bölüm", sNum, eNum),
			Season:   sNum,
			Number:   eNum,
		})
	}
	if len(seasonMap) == 0 {
		for ep := 1; ep <= 10; ep++ {
			seasonMap[1] = append(seasonMap[1], Episode{
				ID:       fmt.Sprintf("%s:1:%d", cleanID, ep),
				StreamID: fmt.Sprintf("%s:1:%d", cleanID, ep),
				Title:    fmt.Sprintf("1. Sezon %d. Bölüm", ep),
				Season:   1,
				Number:   ep,
			})
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
			ID:    cleanID,
			Title: title,
			Type:  MediaTypeSeries,
		},
		Seasons: seasons,
	}, nil
}

func (p *DiziYouProvider) GetEpisodes(ctx context.Context, mediaID string, season int) ([]Episode, error) {
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

func (p *DiziYouProvider) ResolveStream(ctx context.Context, streamID string) (*StreamResult, error) {
	parts := strings.Split(strings.TrimPrefix(streamID, "diziyou:"), ":")
	slug := strings.Trim(parts[0], "/")
	season := 1
	episode := 1
	if len(parts) >= 3 {
		season, _ = strconv.Atoi(parts[1])
		episode, _ = strconv.Atoi(parts[2])
	}

	base := p.baseURL()
	storage := p.storageURL()
	epURL := fmt.Sprintf("%s/%s-%d-sezon-%d-bolum/", base, slug, season, episode)
	htmlStr, _, err := FetchString(ctx, epURL, DefaultHeaders(base+"/"))
	if err != nil {
		return nil, err
	}

	pm := diziYouPlayerRe.FindStringSubmatch(htmlStr)
	if len(pm) < 2 {
		return nil, fmt.Errorf("diziyouPlayer iframe not found")
	}
	idm := diziYouItemIDRe.FindStringSubmatch(pm[1])
	if len(idm) < 2 {
		return nil, fmt.Errorf("diziyou itemId not found")
	}
	itemID := idm[1]

	hdrs := map[string]string{
		"User-Agent": config.DefaultUserAgent,
		"Referer":    epURL,
		"Origin":     base,
	}
	subs := []Subtitle{
		{
			ID:       "diziyou_tr",
			Language: "tr",
			Label:    "Türkçe",
			Format:   "vtt",
			URL:      fmt.Sprintf("%s/subtitles/%s/tr.vtt", storage, itemID),
			Headers:  hdrs,
		},
	}

	var sources []StreamSource
	sources = append(sources, StreamSource{
		Name:      "DiziYou",
		Title:     "DiziYou • Orijinal (TR Altyazı) • 1080p HLS",
		URL:       fmt.Sprintf("%s/episodes/%s/play.m3u8", storage, itemID),
		Quality:   "1080p",
		Format:    "hls",
		IsHLS:     true,
		Headers:   hdrs,
		Subtitles: subs,
	})
	if strings.Contains(htmlStr, "turkceDublaj") {
		sources = append(sources, StreamSource{
			Name:      "DiziYou",
			Title:     "DiziYou • Türkçe Dublaj • 1080p HLS",
			URL:       fmt.Sprintf("%s/episodes/%s_tr/play.m3u8", storage, itemID),
			Quality:   "1080p",
			Format:    "hls",
			IsHLS:     true,
			Headers:   hdrs,
			Subtitles: subs,
		})
	}
	return SelectBestStream(sources)
}
