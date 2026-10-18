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

type DiziMomProvider struct{}

func init() {
	Register(&DiziMomProvider{})
}

func (p *DiziMomProvider) ID() string   { return "dizimom" }
func (p *DiziMomProvider) Name() string { return "DiziMom" }

func (p *DiziMomProvider) baseURL() string {
	return config.RemoteString("urls.series_foreign.dizimom.base", "https://www.dizimom.diy")
}

var (
	diziMomCardRe = regexp.MustCompile(`(?is)<a[^>]+href="(https?://[^"]*dizimom\.[^/]+/diziler/([^"/?#]+)/?)"[^>]*title="([^"]+)"`)
	diziMomEpRe   = regexp.MustCompile(`(?i)href="(https?://[^"]*dizimom\.[^/]+/([a-z0-9-]+-(\d+)-sezon-(\d+)-bolum[^"/?#]*)/?)"`)
)

func (p *DiziMomProvider) parseCards(htmlStr string) []MediaItem {
	var items []MediaItem
	seen := make(map[string]bool)
	for _, m := range diziMomCardRe.FindAllStringSubmatch(htmlStr, -1) {
		slug := m[2]
		if slug == "" || seen[slug] {
			continue
		}
		seen[slug] = true
		items = append(items, MediaItem{
			ID:    slug,
			Title: CleanMediaTitle(m[3]),
			Year:  ExtractYear(m[3]),
			Type:  MediaTypeSeries,
		})
	}
	return items
}

func (p *DiziMomProvider) GetCatalog(ctx context.Context, page int) ([]MediaItem, error) {
	base := p.baseURL()
	htmlStr, _, err := FetchString(ctx, base+"/yabanci-dizi-izle/", DefaultHeaders(base+"/"))
	if err != nil {
		return nil, err
	}
	return p.parseCards(htmlStr), nil
}

func (p *DiziMomProvider) Search(ctx context.Context, query string) ([]MediaItem, error) {
	base := p.baseURL()
	htmlStr, _, err := FetchString(ctx, fmt.Sprintf("%s/?s=%s", base, url.QueryEscape(strings.TrimSpace(query))), DefaultHeaders(base+"/"))
	if err != nil {
		return nil, err
	}
	return p.parseCards(htmlStr), nil
}

func (p *DiziMomProvider) GetDetails(ctx context.Context, mediaID string) (*MediaDetail, error) {
	slug := strings.Trim(strings.TrimPrefix(mediaID, "dizimom:"), "/")
	base := p.baseURL()
	seriesURL := fmt.Sprintf("%s/diziler/%s/", base, slug)
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
		epURL := m[1]
		sNum, _ := strconv.Atoi(m[3])
		eNum, _ := strconv.Atoi(m[4])
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
		seasonMap[1] = []Episode{
			{
				ID:       fmt.Sprintf("%s/%s-1-sezon-1-bolum-izle/", base, slug),
				StreamID: fmt.Sprintf("%s/%s-1-sezon-1-bolum-izle/", base, slug),
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

func (p *DiziMomProvider) ResolveStream(ctx context.Context, streamID string) (*StreamResult, error) {
	epURL := strings.TrimPrefix(streamID, "dizimom:")
	base := p.baseURL()
	htmlStr, _, err := FetchString(ctx, epURL, DefaultHeaders(base+"/"))
	if err != nil {
		return nil, err
	}
	var sources []StreamSource
	for _, im := range regexp.MustCompile(`(?i)<iframe[^>]+src="([^"]+)"`).FindAllStringSubmatch(htmlStr, 5) {
		u := im[1]
		if strings.Contains(u, "youtube.com") {
			continue
		}
		sources = append(sources, ExtractAnyIframe(ctx, u, epURL)...)
	}
	return SelectBestStream(sources)
}
