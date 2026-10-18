package providers

import (
	"context"
	"fmt"
	"net/url"
	"regexp"
	"strings"

	"anthology-dav/config"
)

type SinemaCXProvider struct{}

func init() {
	Register(&SinemaCXProvider{})
}

func (p *SinemaCXProvider) ID() string   { return "sinemacx" }
func (p *SinemaCXProvider) Name() string { return "SinemaCX" }

func (p *SinemaCXProvider) baseURL() string {
	return config.RemoteString("urls.movies.sinemacx", "https://www.sinema.gg")
}

func (p *SinemaCXProvider) GetCatalog(ctx context.Context, page int) ([]MediaItem, error) {
	u := p.baseURL()
	if page > 1 {
		u = fmt.Sprintf("%s/page/%d/", p.baseURL(), page)
	}
	body, _, err := FetchString(ctx, u, DefaultHeaders(p.baseURL()+"/"))
	if err != nil {
		return nil, err
	}
	return p.parseCards(body), nil
}

func (p *SinemaCXProvider) Search(ctx context.Context, query string) ([]MediaItem, error) {
	u := fmt.Sprintf("%s/?s=%s", p.baseURL(), url.QueryEscape(query))
	body, _, err := FetchString(ctx, u, DefaultHeaders(p.baseURL()+"/"))
	if err != nil {
		return nil, err
	}
	return p.parseCards(body), nil
}

func (p *SinemaCXProvider) parseCards(html string) []MediaItem {
	var items []MediaItem
	seen := make(map[string]bool)
	re := regexp.MustCompile(`(?is)<a[^>]+href="(https?://[^"]*sinema\.[^"]+/[^"/]+/?)"[^>]*title="([^"]+)"`)
	for _, m := range re.FindAllStringSubmatch(html, 60) {
		href := m[1]
		title := CleanMediaTitle(m[2])
		if title == "" || seen[href] || strings.Contains(href, "/tur/") || strings.Contains(href, "/kategori/") {
			continue
		}
		seen[href] = true
		items = append(items, MediaItem{
			ID:    href,
			Title: title,
			Year:  ExtractYear(m[2]),
			Type:  MediaTypeMovie,
		})
	}
	return items
}

func (p *SinemaCXProvider) GetDetails(ctx context.Context, mediaID string) (*MediaDetail, error) {
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

func (p *SinemaCXProvider) GetEpisodes(ctx context.Context, mediaID string, season int) ([]Episode, error) {
	return nil, nil
}

func (p *SinemaCXProvider) ResolveStream(ctx context.Context, streamID string) (*StreamResult, error) {
	u := ResolveRelativeURL(streamID, p.baseURL())
	body, _, err := FetchString(ctx, u, DefaultHeaders(p.baseURL()+"/"))
	if err != nil {
		return nil, err
	}

	var sources []StreamSource
	reIframe := regexp.MustCompile(`(?i)src=["'](https?://player\.filmizle\.in/player/index\.php\?data=([a-zA-Z0-9]+))["']`)
	for _, m := range reIframe.FindAllStringSubmatch(body, 5) {
		embedURL := m[1]
		sources = append(sources, ExtractHDPlayerSystem(ctx, embedURL, u)...)
	}
	if len(sources) == 0 {
		sources = append(sources, ExtractAnyIframe(ctx, u, p.baseURL()+"/")...)
	}
	return SelectBestStream(sources)
}
