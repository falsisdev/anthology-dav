package providers

import (
	"context"
	"fmt"
	"net/url"
	"regexp"
	"strings"

	"anthology-dav/config"
)

type KultFilmlerProvider struct{}

func init() {
	Register(&KultFilmlerProvider{})
}

func (p *KultFilmlerProvider) ID() string   { return "kultfilmler" }
func (p *KultFilmlerProvider) Name() string { return "KültFilmler" }

func (p *KultFilmlerProvider) baseURL() string {
	return config.RemoteString("urls.movies.kultfilmler", "https://kultfilmler.net")
}

func (p *KultFilmlerProvider) GetCatalog(ctx context.Context, page int) ([]MediaItem, error) {
	u := fmt.Sprintf("%s/category/aksiyon-filmleri-izle/", p.baseURL())
	if page > 1 {
		u = fmt.Sprintf("%s/category/aksiyon-filmleri-izle/page/%d/", p.baseURL(), page)
	}
	body, _, err := FetchString(ctx, u, DefaultHeaders(p.baseURL()+"/"))
	if err != nil {
		return nil, err
	}
	return p.parseCards(body), nil
}

func (p *KultFilmlerProvider) Search(ctx context.Context, query string) ([]MediaItem, error) {
	u := fmt.Sprintf("%s/?s=%s", p.baseURL(), url.QueryEscape(query))
	body, _, err := FetchString(ctx, u, DefaultHeaders(p.baseURL()+"/"))
	if err != nil {
		return nil, err
	}
	return p.parseCards(body), nil
}

func (p *KultFilmlerProvider) parseCards(html string) []MediaItem {
	var items []MediaItem
	seen := make(map[string]bool)
	re := regexp.MustCompile(`(?is)<a[^>]+href="(https?://[^"]*kultfilmler[^"]+/[^"/]+/?)"[^>]*title="([^"]+)"`)
	for _, m := range re.FindAllStringSubmatch(html, 60) {
		href := m[1]
		title := CleanMediaTitle(m[2])
		if title == "" || seen[href] || strings.Contains(href, "/category/") || strings.Contains(href, "/tag/") {
			continue
		}
		seen[href] = true
		mType := MediaTypeMovie
		if strings.Contains(href, "/dizi/") {
			mType = MediaTypeSeries
		}
		items = append(items, MediaItem{
			ID:    href,
			Title: title,
			Year:  ExtractYear(m[2]),
			Type:  mType,
		})
	}
	return items
}

func (p *KultFilmlerProvider) GetDetails(ctx context.Context, mediaID string) (*MediaDetail, error) {
	u := ResolveRelativeURL(mediaID, p.baseURL())
	body, _, err := FetchString(ctx, u, DefaultHeaders(p.baseURL()+"/"))
	if err != nil {
		return nil, err
	}
	reH1 := regexp.MustCompile(`(?is)<h1[^>]*>(.*?)</h1>`)
	rawTitle := u
	if m := reH1.FindStringSubmatch(body); len(m) > 1 {
		rawTitle = m[1]
	}
	title := CleanMediaTitle(rawTitle)
	if title == "" {
		title = u
	}

	reEps := regexp.MustCompile(`(?is)<a[^>]+class="[^"]*\bep\b[^"]*"[^>]+href="([^"]+)"`)
	epMatches := reEps.FindAllStringSubmatch(body, -1)
	if len(epMatches) > 0 {
		var eps []Episode
		for idx, m := range epMatches {
			epURL := ResolveRelativeURL(m[1], p.baseURL())
			eps = append(eps, Episode{
				ID:       epURL,
				StreamID: epURL,
				Title:    fmt.Sprintf("%d. Bölüm", idx+1),
				Season:   1,
				Number:   idx + 1,
			})
		}
		return &MediaDetail{
			MediaItem: MediaItem{ID: u, Title: title, Year: ExtractYear(rawTitle), Type: MediaTypeSeries},
			Seasons:   []Season{{Number: 1, Name: "Season 01", Episodes: eps}},
		}, nil
	}

	return &MediaDetail{
		MediaItem: MediaItem{
			ID:    u,
			Title: title,
			Year:  ExtractYear(rawTitle),
			Type:  MediaTypeMovie,
		},
		StreamID: u,
	}, nil
}

func (p *KultFilmlerProvider) GetEpisodes(ctx context.Context, mediaID string, season int) ([]Episode, error) {
	detail, err := p.GetDetails(ctx, mediaID)
	if err != nil {
		return nil, err
	}
	for _, s := range detail.Seasons {
		if s.Number == season {
			return s.Episodes, nil
		}
	}
	return nil, nil
}

func (p *KultFilmlerProvider) ResolveStream(ctx context.Context, streamID string) (*StreamResult, error) {
	u := ResolveRelativeURL(streamID, p.baseURL())
	body, _, err := FetchString(ctx, u, DefaultHeaders(p.baseURL()+"/"))
	if err != nil {
		return nil, err
	}
	var sources []StreamSource
	reIframe := regexp.MustCompile(`(?i)<iframe[^>]+src=["']([^"']+)["']`)
	for _, m := range reIframe.FindAllStringSubmatch(body, 6) {
		sources = append(sources, ExtractAnyIframe(ctx, m[1], u)...)
	}
	if len(sources) == 0 {
		sources = append(sources, ExtractAnyIframe(ctx, u, p.baseURL()+"/")...)
	}
	return SelectBestStream(sources)
}
