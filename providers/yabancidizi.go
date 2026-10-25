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

type YabanciDiziProvider struct{}

func init() {
	Register(&YabanciDiziProvider{})
}

func (p *YabanciDiziProvider) ID() string   { return "yabancidizi" }
func (p *YabanciDiziProvider) Name() string { return "YabanciDizi" }

func (p *YabanciDiziProvider) baseURL() string {
	return config.RemoteString("urls.series.yabancidizi.base", config.RemoteString("urls.series_foreign.yabancidizi.base", "https://yabancidizi.news"))
}

var (
	yabanciDiziCardRe = regexp.MustCompile(`(?is)<a[^>]+href="(?:https?://[^"]*yabancidizi\.[^/]+)?/?dizi/([^"/?#]+)"[^>]*>(.*?)</a>`)
	yabanciDiziEpRe   = regexp.MustCompile(`(?i)(?:/?dizi/[^"'/]+/)?sezon-(\d+)/bolum-(\d+)`)
)

func (p *YabanciDiziProvider) parseCards(htmlStr string) []MediaItem {
	var items []MediaItem
	seen := make(map[string]bool)
	for _, m := range yabanciDiziCardRe.FindAllStringSubmatch(htmlStr, -1) {
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

func (p *YabanciDiziProvider) GetCatalog(ctx context.Context, page int) ([]MediaItem, error) {
	base := p.baseURL()
	htmlStr, _, err := FetchStringHTTP11(ctx, base+"/", DefaultHeaders(base+"/"))
	if err != nil {
		return nil, err
	}
	return p.parseCards(htmlStr), nil
}

func (p *YabanciDiziProvider) Search(ctx context.Context, query string) ([]MediaItem, error) {
	base := p.baseURL()
	u := fmt.Sprintf("%s/search?qr=%s", base, url.QueryEscape(strings.TrimSpace(query)))
	var data struct {
		Data struct {
			Result []struct {
				Title string `json:"s_name"`
				Link  string `json:"s_link"`
				Image string `json:"s_image"`
				Year  string `json:"s_year"`
			} `json:"result"`
		} `json:"data"`
	}
	if err := FetchJSONWithBodyHTTP11(ctx, "POST", u, map[string]string{
		"User-Agent":       config.DefaultUserAgent,
		"Referer":          base + "/",
		"X-Requested-With": "XMLHttpRequest",
		"Accept":           "application/json, text/javascript, */*; q=0.01",
	}, nil, &data); err == nil && len(data.Data.Result) > 0 {
		var items []MediaItem
		for _, r := range data.Data.Result {
			slug := strings.Trim(strings.TrimPrefix(r.Link, "/dizi/"), "/")
			if slug != "" && r.Title != "" {
				items = append(items, MediaItem{
					ID:    slug,
					Title: r.Title,
					Year:  r.Year,
					Type:  MediaTypeSeries,
				})
			}
		}
		return items, nil
	}
	return p.GetCatalog(ctx, 1)
}

func (p *YabanciDiziProvider) GetDetails(ctx context.Context, mediaID string) (*MediaDetail, error) {
	slug := strings.Trim(strings.TrimPrefix(mediaID, "yabancidizi:"), "/")
	base := p.baseURL()
	seriesURL := fmt.Sprintf("%s/dizi/%s", base, slug)
	htmlStr, _, err := FetchStringHTTP11(ctx, seriesURL, DefaultHeaders(base+"/"))
	if err != nil {
		return nil, err
	}
	title := slug
	if m := regexp.MustCompile(`(?is)<h1[^>]*>(.*?)</h1>`).FindStringSubmatch(htmlStr); len(m) > 1 {
		title = CleanMediaTitle(m[1])
	}

	seasonMap := make(map[int][]Episode)
	seen := make(map[string]bool)
	for _, m := range yabanciDiziEpRe.FindAllStringSubmatch(htmlStr, -1) {
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

func (p *YabanciDiziProvider) GetEpisodes(ctx context.Context, mediaID string, season int) ([]Episode, error) {
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

func (p *YabanciDiziProvider) ResolveStream(ctx context.Context, streamID string) (*StreamResult, error) {
	parts := strings.Split(strings.TrimPrefix(streamID, "yabancidizi:"), ":")
	slug := parts[0]
	season := 1
	episode := 1
	if len(parts) >= 3 {
		season, _ = strconv.Atoi(parts[1])
		episode, _ = strconv.Atoi(parts[2])
	}
	base := p.baseURL()
	epURL := fmt.Sprintf("%s/dizi/%s/sezon-%d/bolum-%d", base, slug, season, episode)
	htmlStr, _, err := FetchStringHTTP11(ctx, epURL, DefaultHeaders(base+"/"))
	if err != nil {
		return nil, err
	}

	var sources []StreamSource
	for _, vm := range regexp.MustCompile(`vidmoly\.[a-z]+/(?:dl/|embed-)([a-zA-Z0-9]+)`).FindAllStringSubmatch(htmlStr, -1) {
		sources = append(sources, ExtractVidMoly(ctx, "https://vidmoly.biz/embed-"+vm[1]+".html", epURL)...)
	}
	for _, im := range regexp.MustCompile(`(?i)<iframe[^>]+src="([^"]+)"`).FindAllStringSubmatch(htmlStr, 4) {
		sources = append(sources, ExtractAnyIframe(ctx, im[1], epURL)...)
	}
	return SelectBestStream(sources)
}
