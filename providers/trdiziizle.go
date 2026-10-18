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

type TrDiziIzleProvider struct{}

func init() {
	Register(&TrDiziIzleProvider{})
}

func (p *TrDiziIzleProvider) ID() string   { return "trdiziizle" }
func (p *TrDiziIzleProvider) Name() string { return "TrDiziIzle" }

func (p *TrDiziIzleProvider) baseURL() string {
	return config.RemoteString("urls.series_local.trdiziizle.base", "https://www.trdiziizle.tv/tr2")
}

var trDiziLinkRe = regexp.MustCompile(`(?is)<a[^>]+href="(https?://[^"]*trdiziizle\.[^"]+/([^"/?#]+)/?)"[^>]*>(.*?)</a>`)

func (p *TrDiziIzleProvider) parseLinks(htmlStr string) []MediaItem {
	var items []MediaItem
	seen := make(map[string]bool)
	for _, m := range trDiziLinkRe.FindAllStringSubmatch(htmlStr, -1) {
		fullURL := m[1]
		slug := m[2]
		if slug == "" || seen[slug] || slug == "tr2" || strings.Contains(slug, "iletisim") || strings.Contains(slug, "kategori") {
			continue
		}
		title := CleanMediaTitle(m[3])
		if len(title) < 2 {
			continue
		}
		seen[slug] = true
		items = append(items, MediaItem{
			ID:    fullURL,
			Title: title,
			Type:  MediaTypeSeries,
		})
	}
	return items
}

func (p *TrDiziIzleProvider) GetCatalog(ctx context.Context, page int) ([]MediaItem, error) {
	base := p.baseURL()
	htmlStr, _, err := FetchString(ctx, base+"/", DefaultHeaders(base+"/"))
	if err != nil {
		return nil, err
	}
	return p.parseLinks(htmlStr), nil
}

func (p *TrDiziIzleProvider) Search(ctx context.Context, query string) ([]MediaItem, error) {
	base := p.baseURL()
	u := fmt.Sprintf("%s/?s=%s", base, url.QueryEscape(strings.TrimSpace(query)))
	htmlStr, _, err := FetchString(ctx, u, DefaultHeaders(base+"/"))
	if err != nil {
		return nil, err
	}
	return p.parseLinks(htmlStr), nil
}

func (p *TrDiziIzleProvider) GetDetails(ctx context.Context, mediaID string) (*MediaDetail, error) {
	pageURL := strings.TrimPrefix(mediaID, "trdiziizle:")
	if !strings.HasPrefix(pageURL, "http") {
		pageURL = p.baseURL() + "/" + strings.Trim(pageURL, "/") + "/"
	}
	htmlStr, _, err := FetchString(ctx, pageURL, DefaultHeaders(p.baseURL()+"/"))
	if err != nil {
		return nil, err
	}
	title := "TrDiziIzle"
	if m := regexp.MustCompile(`(?is)<h1[^>]*>(.*?)</h1>`).FindStringSubmatch(htmlStr); len(m) > 1 {
		title = CleanMediaTitle(m[1])
	}

	var eps []Episode
	seen := make(map[string]bool)
	for _, m := range trDiziLinkRe.FindAllStringSubmatch(htmlStr, -1) {
		href := m[1]
		txt := CleanHTMLText(m[3])
		if seen[href] || !strings.Contains(strings.ToLower(txt), "bölüm") && !strings.Contains(strings.ToLower(href), "bolum") {
			continue
		}
		seen[href] = true
		eNum := len(eps) + 1
		if em := regexp.MustCompile(`(\d+)\.\s*B[öo]l[üu]m`).FindStringSubmatch(txt); len(em) > 1 {
			eNum, _ = strconv.Atoi(em[1])
		}
		eps = append(eps, Episode{
			ID:       href,
			StreamID: href,
			Title:    txt,
			Season:   1,
			Number:   eNum,
		})
	}
	if len(eps) == 0 {
		eps = append(eps, Episode{
			ID:       pageURL,
			StreamID: pageURL,
			Title:    "Bölüm 1",
			Season:   1,
			Number:   1,
		})
	}
	return &MediaDetail{
		MediaItem: MediaItem{
			ID:    pageURL,
			Title: title,
			Type:  MediaTypeSeries,
		},
		Seasons: []Season{
			{
				Number:   1,
				Name:     "Season 01",
				Episodes: eps,
			},
		},
	}, nil
}

func (p *TrDiziIzleProvider) GetEpisodes(ctx context.Context, mediaID string, season int) ([]Episode, error) {
	det, err := p.GetDetails(ctx, mediaID)
	if err != nil {
		return nil, err
	}
	if len(det.Seasons) > 0 {
		return det.Seasons[0].Episodes, nil
	}
	return nil, nil
}

func (p *TrDiziIzleProvider) ResolveStream(ctx context.Context, streamID string) (*StreamResult, error) {
	epURL := strings.TrimPrefix(streamID, "trdiziizle:")
	base := p.baseURL()
	htmlStr, _, err := FetchString(ctx, epURL, DefaultHeaders(base+"/"))
	if err != nil {
		return nil, err
	}
	var sources []StreamSource
	for _, im := range regexp.MustCompile(`(?i)<iframe[^>]+src="([^"]+)"`).FindAllStringSubmatch(htmlStr, 5) {
		sources = append(sources, ExtractAnyIframe(ctx, ResolveRelativeURL(im[1], base), epURL)...)
	}
	return SelectBestStream(sources)
}
