package providers

import (
	"context"
	"encoding/base64"
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
	return config.RemoteString("urls.movies.sinemacx", "https://sinemacc.com")
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
	u := fmt.Sprintf("%s/arama/?s=%s", p.baseURL(), url.QueryEscape(query))
	body, _, err := FetchString(ctx, u, DefaultHeaders(p.baseURL()+"/"))
	if err != nil {
		return nil, err
	}
	return p.parseCards(body), nil
}

func (p *SinemaCXProvider) parseCards(html string) []MediaItem {
	var items []MediaItem
	seen := make(map[string]bool)
	re := regexp.MustCompile(`(?is)<a[^>]+href="(https?://[^"]*sinemacc\.com/film/[^"]+)"[^>]*>([\s\S]*?)</a>`)
	reH := regexp.MustCompile(`(?is)<h[234][^>]*>([^<]+)</h[234]>`)
	reAlt := regexp.MustCompile(`(?is)alt="([^"]+)"`)

	for _, m := range re.FindAllStringSubmatch(html, 60) {
		href := m[1]
		if seen[href] {
			continue
		}
		rawT := ""
		if hm := reH.FindStringSubmatch(m[2]); len(hm) > 1 {
			rawT = hm[1]
		} else if am := reAlt.FindStringSubmatch(m[2]); len(am) > 1 {
			rawT = am[1]
		} else {
			parts := strings.Split(strings.Trim(href, "/"), "/")
			rawT = parts[len(parts)-1]
		}
		title := CleanMediaTitle(rawT)
		if title == "" {
			continue
		}
		seen[href] = true
		items = append(items, MediaItem{
			ID:    href,
			Title: title,
			Year:  ExtractYear(rawT),
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

	// 1. Direct or base64-encoded player.filmizle.in / vidpapi embeds
	var embedURLs []string
	reEmbed := regexp.MustCompile(`(?i)(?:src|data-vsrc|data-src)=["']([^"']+)["']`)
	for _, m := range reEmbed.FindAllStringSubmatch(body, -1) {
		val := m[1]
		if strings.HasPrefix(val, "http") {
			embedURLs = append(embedURLs, val)
		} else if dec, err := base64.StdEncoding.DecodeString(val); err == nil && strings.HasPrefix(string(dec), "http") {
			embedURLs = append(embedURLs, string(dec))
		}
	}

	for _, embedURL := range embedURLs {
		if strings.Contains(embedURL, "filmizle.in") {
			sources = append(sources, ExtractHDPlayerSystem(ctx, embedURL, u)...)
		} else if strings.Contains(embedURL, "vidpapi") {
			sources = append(sources, ExtractAnyIframe(ctx, embedURL, u)...)
		}
		if len(sources) > 0 {
			break
		}
	}

	if len(sources) == 0 {
		sources = append(sources, ExtractAnyIframe(ctx, u, p.baseURL()+"/")...)
	}
	return SelectBestStream(sources)
}
