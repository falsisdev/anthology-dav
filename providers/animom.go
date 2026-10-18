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

type AniMomProvider struct{}

func init() {
	Register(&AniMomProvider{})
}

func (p *AniMomProvider) ID() string   { return "animom" }
func (p *AniMomProvider) Name() string { return "AniMom" }

func (p *AniMomProvider) baseURL() string {
	return config.RemoteString("urls.anime.animom.base", "https://animom.org")
}

var (
	aniMomAnimeHrefRe = regexp.MustCompile(`(?is)<a[^>]+href="([^"]*/anime/([^"/?#]+))"[^>]*>(.*?)</a>`)
	aniMomImgRe       = regexp.MustCompile(`(?i)<img[^>]+(?:data-src|src)="([^"]+)"`)
	aniMomEpHrefRe    = regexp.MustCompile(`(?i)href="([^"]*/anime/[^/]+/sezon-(\d+)/bolum-(\d+))"`)
	aniMomFrameRe     = regexp.MustCompile(`(?i)(?:data-frame|src)="([^"]+)"`)
)

func (p *AniMomProvider) parseCards(htmlStr, base string) []MediaItem {
	var items []MediaItem
	seen := make(map[string]bool)
	for _, m := range aniMomAnimeHrefRe.FindAllStringSubmatch(htmlStr, -1) {
		slug := m[2]
		if slug == "" || seen[slug] {
			continue
		}
		inner := m[3]
		title := CleanHTMLText(inner)
		if title == "" {
			continue
		}
		seen[slug] = true
		poster := ""
		if im := aniMomImgRe.FindStringSubmatch(inner); len(im) > 1 {
			poster = ResolveRelativeURL(im[1], base)
		}
		items = append(items, MediaItem{
			ID:        slug,
			Title:     title,
			Type:      MediaTypeAnime,
			PosterURL: poster,
		})
	}
	return items
}

func (p *AniMomProvider) GetCatalog(ctx context.Context, page int) ([]MediaItem, error) {
	base := p.baseURL()
	htmlStr, _, err := FetchString(ctx, base+"/home", DefaultHeaders(base+"/"))
	if err != nil {
		return nil, err
	}
	return p.parseCards(htmlStr, base), nil
}

func (p *AniMomProvider) Search(ctx context.Context, query string) ([]MediaItem, error) {
	base := p.baseURL()
	form := url.Values{}
	form.Set("query", strings.TrimSpace(query))
	respStr, _, err := PostForm(ctx, base+"/search", form, map[string]string{
		"User-Agent":       config.DefaultUserAgent,
		"Referer":          base + "/home",
		"Origin":           base,
		"X-Requested-With": "XMLHttpRequest",
	})
	if err != nil {
		return nil, err
	}
	var data struct {
		Theme string `json:"theme"`
	}
	if json.Unmarshal([]byte(respStr), &data) == nil && data.Theme != "" {
		return p.parseCards(data.Theme, base), nil
	}
	return p.parseCards(respStr, base), nil
}

func (p *AniMomProvider) GetDetails(ctx context.Context, mediaID string) (*MediaDetail, error) {
	slug := strings.TrimPrefix(mediaID, "animom:")
	base := p.baseURL()
	htmlStr, _, err := FetchString(ctx, fmt.Sprintf("%s/anime/%s", base, slug), DefaultHeaders(base+"/"))
	if err != nil {
		return nil, err
	}
	title := slug
	if m := regexp.MustCompile(`(?is)<h1[^>]*>(.*?)</h1>`).FindStringSubmatch(htmlStr); len(m) > 1 {
		title = CleanHTMLText(m[1])
	}

	seasonMap := make(map[int][]Episode)
	seen := make(map[string]bool)
	for _, m := range aniMomEpHrefRe.FindAllStringSubmatch(htmlStr, -1) {
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
			Type:  MediaTypeAnime,
		},
		Seasons: seasons,
	}, nil
}

func (p *AniMomProvider) GetEpisodes(ctx context.Context, mediaID string, season int) ([]Episode, error) {
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

func (p *AniMomProvider) ResolveStream(ctx context.Context, streamID string) (*StreamResult, error) {
	parts := strings.Split(strings.TrimPrefix(streamID, "animom:"), ":")
	slug := parts[0]
	season := 1
	episode := 1
	if len(parts) >= 3 {
		season, _ = strconv.Atoi(parts[1])
		episode, _ = strconv.Atoi(parts[2])
	}
	base := p.baseURL()
	epURL := fmt.Sprintf("%s/anime/%s/sezon-%d/bolum-%d", base, slug, season, episode)
	htmlStr, _, err := FetchString(ctx, epURL, DefaultHeaders(base+"/"))
	if err != nil {
		return nil, err
	}

	var sources []StreamSource
	seen := make(map[string]bool)
	for _, m := range aniMomFrameRe.FindAllStringSubmatch(htmlStr, -1) {
		u := strings.TrimSpace(m[1])
		if u == "" || seen[u] || strings.HasSuffix(u, ".js") || strings.HasSuffix(u, ".css") || strings.HasSuffix(u, ".png") || strings.HasSuffix(u, ".jpg") {
			continue
		}
		if strings.Contains(u, "sibnet") || strings.Contains(u, "hdplayersystem") || strings.Contains(u, "yourupload") || strings.Contains(u, "ok.ru") || strings.Contains(u, "vidmoly") {
			seen[u] = true
			sources = append(sources, ExtractAnyIframe(ctx, u, epURL)...)
		}
	}
	return SelectBestStream(sources)
}
