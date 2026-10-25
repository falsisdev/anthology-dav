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

type TvDizilerProvider struct{}

func init() {
	Register(&TvDizilerProvider{})
}

func (p *TvDizilerProvider) ID() string   { return "tvdiziler" }
func (p *TvDizilerProvider) Name() string { return "TvDiziler" }

func (p *TvDizilerProvider) baseURL() string {
	return config.RemoteString("urls.series_local.tvdiziler.base", "https://tvdiziler.tv")
}

var (
	tvDizilerSeriesRe = regexp.MustCompile(`(?is)<a[^>]+href=["'](?:https?://[^"'/]+)?/?dizi/([^"'/?#]+)["'][^>]*>(.*?)</a>`)
	tvDizilerEpRe     = regexp.MustCompile(`(?is)<a[^>]+href=["'](?:https?://[^"'/]+)?/?([a-z0-9-]+-(\d+)-bolum[^"'/?#]*)["'][^>]*>(.*?)</a>`)
	tvDizilerHhsRe    = regexp.MustCompile(`(?i)data-hhs=["']([^"']+)["']`)
)

func (p *TvDizilerProvider) parseSeries(htmlStr string) []MediaItem {
	cleanHTML := strings.ReplaceAll(htmlStr, `\/`, `/`)
	var items []MediaItem
	seen := make(map[string]bool)
	for _, m := range tvDizilerSeriesRe.FindAllStringSubmatch(cleanHTML, -1) {
		slug := strings.Trim(m[1], "/")
		if slug == "" || seen[slug] {
			continue
		}
		title := ""
		if tm := regexp.MustCompile(`(?i)<h[23][^>]*>(.*?)</h[23]>`).FindStringSubmatch(m[2]); len(tm) > 1 {
			title = CleanMediaTitle(tm[1])
		}
		if title == "" {
			title = CleanMediaTitle(m[2])
		}
		if title == "" {
			continue
		}
		seen[slug] = true
		items = append(items, MediaItem{
			ID:    slug,
			Title: title,
			Type:  MediaTypeSeries,
		})
	}
	return items
}

func (p *TvDizilerProvider) GetCatalog(ctx context.Context, page int) ([]MediaItem, error) {
	base := p.baseURL()
	htmlStr, _, err := FetchString(ctx, base+"/dizi-izle", DefaultHeaders(base+"/"))
	if err != nil {
		return nil, err
	}
	return p.parseSeries(htmlStr), nil
}

func (p *TvDizilerProvider) Search(ctx context.Context, query string) ([]MediaItem, error) {
	base := p.baseURL()
	u := fmt.Sprintf("%s/search?qr=%s", base, url.QueryEscape(strings.TrimSpace(query)))
	headers := map[string]string{
		"User-Agent":       config.DefaultUserAgent,
		"Referer":          base + "/",
		"X-Requested-With": "XMLHttpRequest",
		"Accept":           "application/json, text/javascript, */*; q=0.01",
	}

	bodyBytes, _, _, _, err := FetchBytes(ctx, "POST", u, headers, nil)
	if err == nil && len(bodyBytes) > 0 {
		var res struct {
			Data string `json:"data"`
		}
		if json.Unmarshal(bodyBytes, &res) == nil && res.Data != "" {
			return p.parseSeries(res.Data), nil
		}
	}
	return p.GetCatalog(ctx, 1)
}

func (p *TvDizilerProvider) GetDetails(ctx context.Context, mediaID string) (*MediaDetail, error) {
	slug := strings.Trim(strings.TrimPrefix(mediaID, "tvdiziler:"), "/")
	slug = strings.TrimPrefix(slug, "dizi/")
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

	var eps []Episode
	seen := make(map[string]bool)
	for _, m := range tvDizilerEpRe.FindAllStringSubmatch(htmlStr, -1) {
		epSlug := strings.Trim(m[1], "/")
		if seen[epSlug] {
			continue
		}
		seen[epSlug] = true
		eNum, _ := strconv.Atoi(m[2])
		if eNum <= 0 {
			eNum = len(eps) + 1
		}
		eps = append(eps, Episode{
			ID:       epSlug,
			StreamID: epSlug,
			Title:    fmt.Sprintf("Bölüm %d", eNum),
			Season:   1,
			Number:   eNum,
		})
	}
	if len(eps) == 0 {
		eps = append(eps, Episode{
			ID:       slug + "-1-bolum-izle",
			StreamID: slug + "-1-bolum-izle",
			Title:    "Bölüm 1",
			Season:   1,
			Number:   1,
		})
	}
	return &MediaDetail{
		MediaItem: MediaItem{
			ID:    slug,
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

func (p *TvDizilerProvider) GetEpisodes(ctx context.Context, mediaID string, season int) ([]Episode, error) {
	det, err := p.GetDetails(ctx, mediaID)
	if err != nil {
		return nil, err
	}
	if len(det.Seasons) > 0 {
		return det.Seasons[0].Episodes, nil
	}
	return nil, nil
}

func (p *TvDizilerProvider) ResolveStream(ctx context.Context, streamID string) (*StreamResult, error) {
	epSlug := strings.Trim(strings.TrimPrefix(streamID, "tvdiziler:"), "/")
	base := p.baseURL()
	epURL := fmt.Sprintf("%s/%s", base, epSlug)
	htmlStr, _, err := FetchString(ctx, epURL, DefaultHeaders(base+"/"))
	if err != nil {
		return nil, err
	}

	var sources []StreamSource
	for _, m := range tvDizilerHhsRe.FindAllStringSubmatch(htmlStr, 10) {
		for _, part := range strings.Split(m[1], ",") {
			part = strings.TrimSpace(part)
			if part == "" {
				continue
			}
			plyURL := ResolveRelativeURL(part, base)
			sources = append(sources, ExtractAnyIframe(ctx, plyURL, epURL)...)
		}
	}
	for _, im := range regexp.MustCompile(`(?i)<iframe[^>]+src="([^"]+)"`).FindAllStringSubmatch(htmlStr, 4) {
		sources = append(sources, ExtractAnyIframe(ctx, ResolveRelativeURL(im[1], base), epURL)...)
	}
	return SelectBestStream(sources)
}
