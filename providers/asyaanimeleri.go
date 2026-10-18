package providers

import (
	"context"
	"encoding/base64"
	"fmt"
	"net/url"
	"regexp"
	"strconv"
	"strings"

	"anthology-dav/config"
)

type AsyaAnimeleriProvider struct{}

func init() {
	Register(&AsyaAnimeleriProvider{})
}

func (p *AsyaAnimeleriProvider) ID() string   { return "asyaanimeleri" }
func (p *AsyaAnimeleriProvider) Name() string { return "AsyaAnimeleri" }

func (p *AsyaAnimeleriProvider) baseURL() string {
	return config.RemoteString("urls.anime.asyaanimeleri.base", "https://asyaanimeleri.top")
}

var (
	asyaCardRe   = regexp.MustCompile(`(?is)<article[^>]*class="[^"]*bs[^"]*"[^>]*>.*?<a[^>]+href="([^"]+)"[^>]*title="([^"]+)"[^>]*>.*?<img[^>]+src="([^"]+)"`)
	asyaEpListRe = regexp.MustCompile(`(?is)<div[^>]*class="eplister"[^>]*>(.*?)</div>`)
	asyaEpItemRe = regexp.MustCompile(`(?is)<a[^>]+href="([^"]+)"[^>]*>.*?<div class="epl-num">([^<]+)</div>`)
	asyaOptRe    = regexp.MustCompile(`(?i)<option[^>]+value="([A-Za-z0-9+/=]{20,})"`)
	asyaIframeRe = regexp.MustCompile(`(?i)<iframe[^>]+src="([^"]+)"`)
)

func (p *AsyaAnimeleriProvider) parseCards(htmlStr string) []MediaItem {
	var items []MediaItem
	seen := make(map[string]bool)
	for _, m := range asyaCardRe.FindAllStringSubmatch(htmlStr, -1) {
		href := strings.TrimSpace(m[1])
		if href == "" || seen[href] {
			continue
		}
		seen[href] = true
		items = append(items, MediaItem{
			ID:        href,
			Title:     CleanHTMLText(m[2]),
			Type:      MediaTypeAnime,
			PosterURL: m[3],
		})
	}
	return items
}

func (p *AsyaAnimeleriProvider) GetCatalog(ctx context.Context, page int) ([]MediaItem, error) {
	base := p.baseURL()
	htmlStr, _, err := FetchString(ctx, base+"/series/?order=popular", DefaultHeaders(base+"/"))
	if err != nil {
		return nil, err
	}
	return p.parseCards(htmlStr), nil
}

func (p *AsyaAnimeleriProvider) Search(ctx context.Context, query string) ([]MediaItem, error) {
	base := p.baseURL()
	u := fmt.Sprintf("%s/?s=%s", base, url.QueryEscape(strings.TrimSpace(query)))
	htmlStr, _, err := FetchString(ctx, u, DefaultHeaders(base+"/"))
	if err != nil {
		return nil, err
	}
	return p.parseCards(htmlStr), nil
}

func (p *AsyaAnimeleriProvider) GetDetails(ctx context.Context, mediaID string) (*MediaDetail, error) {
	pageURL := strings.TrimPrefix(mediaID, "asyaanimeleri:")
	if !strings.HasPrefix(pageURL, "http") {
		pageURL = p.baseURL() + "/series/" + strings.Trim(pageURL, "/") + "/"
	}
	htmlStr, _, err := FetchString(ctx, pageURL, DefaultHeaders(p.baseURL()+"/"))
	if err != nil {
		return nil, err
	}
	title := "AsyaAnimeleri"
	if m := regexp.MustCompile(`(?is)<h1[^>]*class="entry-title"[^>]*>(.*?)</h1>`).FindStringSubmatch(htmlStr); len(m) > 1 {
		title = CleanHTMLText(m[1])
	}

	var eps []Episode
	seen := make(map[int]bool)
	epBlock := htmlStr
	if m := asyaEpListRe.FindStringSubmatch(htmlStr); len(m) > 1 {
		epBlock = m[1]
	}
	for _, m := range asyaEpItemRe.FindAllStringSubmatch(epBlock, -1) {
		epHref := strings.TrimSpace(m[1])
		numStr := strings.TrimSpace(m[2])
		eNum, _ := strconv.Atoi(regexp.MustCompile(`\d+`).FindString(numStr))
		if eNum <= 0 || seen[eNum] {
			continue
		}
		seen[eNum] = true
		eps = append(eps, Episode{
			ID:       epHref,
			StreamID: epHref,
			Title:    fmt.Sprintf("Bölüm %d", eNum),
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
			Type:  MediaTypeAnime,
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

func (p *AsyaAnimeleriProvider) GetEpisodes(ctx context.Context, mediaID string, season int) ([]Episode, error) {
	det, err := p.GetDetails(ctx, mediaID)
	if err != nil {
		return nil, err
	}
	if len(det.Seasons) > 0 {
		return det.Seasons[0].Episodes, nil
	}
	return nil, nil
}

func (p *AsyaAnimeleriProvider) ResolveStream(ctx context.Context, streamID string) (*StreamResult, error) {
	epURL := strings.TrimPrefix(streamID, "asyaanimeleri:")
	if !strings.HasPrefix(epURL, "http") {
		epURL = p.baseURL() + "/" + strings.TrimLeft(epURL, "/")
	}
	htmlStr, _, err := FetchString(ctx, epURL, DefaultHeaders(p.baseURL()+"/"))
	if err != nil {
		return nil, err
	}

	var iframeURLs []string
	seen := make(map[string]bool)
	for _, m := range asyaOptRe.FindAllStringSubmatch(htmlStr, -1) {
		decoded, err := base64.StdEncoding.DecodeString(m[1])
		if err != nil {
			continue
		}
		if im := asyaIframeRe.FindStringSubmatch(string(decoded)); len(im) > 1 {
			u := im[1]
			if strings.HasPrefix(u, "//") {
				u = "https:" + u
			}
			if !seen[u] {
				seen[u] = true
				iframeURLs = append(iframeURLs, u)
			}
		}
	}
	for _, im := range asyaIframeRe.FindAllStringSubmatch(htmlStr, -1) {
		u := im[1]
		if strings.HasPrefix(u, "//") {
			u = "https:" + u
		}
		if !seen[u] {
			seen[u] = true
			iframeURLs = append(iframeURLs, u)
		}
	}

	var sources []StreamSource
	for _, u := range iframeURLs {
		sources = append(sources, ExtractAnyIframe(ctx, u, epURL)...)
		if len(sources) >= 3 {
			break
		}
	}
	return SelectBestStream(sources)
}
