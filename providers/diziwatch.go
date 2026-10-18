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

type DiziWatchProvider struct{}

func init() {
	Register(&DiziWatchProvider{})
}

func (p *DiziWatchProvider) ID() string   { return "diziwatch" }
func (p *DiziWatchProvider) Name() string { return "DiziWatch" }

func (p *DiziWatchProvider) baseURL() string {
	return config.RemoteString("urls.series_foreign.diziwatch.base", "https://diziwatch.ac")
}

var (
	diziWatchCardRe = regexp.MustCompile(`(?is)<a[^>]+href="(?:https?://[^"]*diziwatch\.[^/]+)?/dizi/([^"/?#]+)"[^>]*>(.*?)</a>`)
	diziWatchEpRe   = regexp.MustCompile(`(?i)href="(?:https?://[^"]*diziwatch\.[^/]+)?/dizi/[^/]+/sezon-(\d+)/bolum-(\d+)"`)
)

func (p *DiziWatchProvider) parseCards(htmlStr string) []MediaItem {
	var items []MediaItem
	seen := make(map[string]bool)
	for _, m := range diziWatchCardRe.FindAllStringSubmatch(htmlStr, -1) {
		slug := m[1]
		if slug == "" || seen[slug] {
			continue
		}
		title := CleanMediaTitle(m[2])
		if title == "" {
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

func (p *DiziWatchProvider) GetCatalog(ctx context.Context, page int) ([]MediaItem, error) {
	base := p.baseURL()
	htmlStr, _, err := FetchString(ctx, base+"/episodes", DefaultHeaders(base+"/"))
	if err != nil {
		return nil, err
	}
	if items := p.parseCards(htmlStr); len(items) > 0 {
		return items, nil
	}
	htmlStr2, _, err := FetchString(ctx, base+"/", DefaultHeaders(base+"/"))
	if err != nil {
		return nil, err
	}
	return p.parseCards(htmlStr2), nil
}

func (p *DiziWatchProvider) Search(ctx context.Context, query string) ([]MediaItem, error) {
	base := p.baseURL()
	form := url.Values{}
	form.Set("searchterm", strings.TrimSpace(query))
	respStr, _, err := PostForm(ctx, base+"/bg/searchcontent", form, map[string]string{
		"User-Agent":       config.DefaultUserAgent,
		"Referer":          base + "/",
		"Origin":           base,
		"X-Requested-With": "XMLHttpRequest",
	})
	if err != nil {
		return nil, err
	}
	var data struct {
		HTML string `json:"html"`
	}
	if items := p.parseCards(respStr); len(items) > 0 {
		return items, nil
	}
	_ = data
	return p.parseCards(respStr), nil
}

func (p *DiziWatchProvider) GetDetails(ctx context.Context, mediaID string) (*MediaDetail, error) {
	slug := strings.Trim(strings.TrimPrefix(mediaID, "diziwatch:"), "/")
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
	for _, m := range diziWatchEpRe.FindAllStringSubmatch(htmlStr, -1) {
		sNum, _ := strconv.Atoi(m[1])
		eNum, _ := strconv.Atoi(m[2])
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
			ID:       fmt.Sprintf("%s:%d:%d", slug, sNum, eNum),
			StreamID: fmt.Sprintf("%s:%d:%d", slug, sNum, eNum),
			Title:    fmt.Sprintf("%d. Sezon %d. Bölüm", sNum, eNum),
			Season:   sNum,
			Number:   eNum,
		})
	}
	if len(seasonMap) == 0 {
		seasonMap[1] = []Episode{
			{
				ID:       fmt.Sprintf("%s:1:1", slug),
				StreamID: fmt.Sprintf("%s:1:1", slug),
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

func (p *DiziWatchProvider) GetEpisodes(ctx context.Context, mediaID string, season int) ([]Episode, error) {
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

func (p *DiziWatchProvider) ResolveStream(ctx context.Context, streamID string) (*StreamResult, error) {
	parts := strings.Split(strings.TrimPrefix(streamID, "diziwatch:"), ":")
	slug := parts[0]
	season := 1
	episode := 1
	if len(parts) >= 3 {
		season, _ = strconv.Atoi(parts[1])
		episode, _ = strconv.Atoi(parts[2])
	}
	base := p.baseURL()
	epURL := fmt.Sprintf("%s/dizi/%s/sezon-%d/bolum-%d", base, slug, season, episode)
	htmlStr, _, err := FetchString(ctx, epURL, DefaultHeaders(base+"/"))
	if err != nil {
		return nil, err
	}

	var sources []StreamSource
	for _, im := range regexp.MustCompile(`(?i)<iframe[^>]+src="([^"]+)"`).FindAllStringSubmatch(htmlStr, 4) {
		sources = append(sources, ExtractAnyIframe(ctx, im[1], base+"/")...)
	}
	return SelectBestStream(sources)
}
