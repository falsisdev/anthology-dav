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

type AnizmProvider struct{}

func init() {
	Register(&AnizmProvider{})
}

func (p *AnizmProvider) ID() string   { return "anizm" }
func (p *AnizmProvider) Name() string { return "Anizm" }

func (p *AnizmProvider) baseURL() string {
	return config.RemoteString("urls.anime.anizm.base", "https://anizm.net")
}

var (
	anizmCardRe       = regexp.MustCompile(`(?is)<a[^>]+href="(?:https?://anizm\.net)?/([a-z0-9-]+)"[^>]*>.*?<img[^>]+src="([^"]+)"[^>]*alt="([^"]+)"`)
	anizmEpRe         = regexp.MustCompile(`(?i)href="(?:https?://anizm\.net)?/([a-z0-9-]+-(\d+)-bolum-izle)"`)
	anizmTranslatorRe = regexp.MustCompile(`(?i)translator="([^"]+)"`)
	anizmVideoAttrRe  = regexp.MustCompile(`(?i)video="([^"]+)"`)
	anizmFirePlayerRe = regexp.MustCompile(`FirePlayer\s*\(\s*["']([a-f0-9]+)["']`)
)

func (p *AnizmProvider) GetCatalog(ctx context.Context, page int) ([]MediaItem, error) {
	base := p.baseURL()
	htmlStr, _, err := FetchString(ctx, base+"/", DefaultHeaders(base+"/"))
	if err != nil {
		return nil, err
	}
	var items []MediaItem
	seen := make(map[string]bool)
	for _, m := range anizmCardRe.FindAllStringSubmatch(htmlStr, -1) {
		slug := m[1]
		if seen[slug] || strings.HasSuffix(slug, "-bolum-izle") || slug == "anime-izle" || slug == "takvim" {
			continue
		}
		seen[slug] = true
		items = append(items, MediaItem{
			ID:        slug,
			Title:     CleanHTMLText(m[3]),
			Type:      MediaTypeAnime,
			PosterURL: ResolveRelativeURL(m[2], base),
		})
	}
	return items, nil
}

func (p *AnizmProvider) Search(ctx context.Context, query string) ([]MediaItem, error) {
	base := p.baseURL()
	u := fmt.Sprintf("%s/searchAnime?query=%s&page=1", base, url.QueryEscape(strings.TrimSpace(query)))
	var data struct {
		Data []struct {
			Slug  string `json:"info_slug"`
			Title string `json:"info_title"`
			Thumb string `json:"info_poster"`
		} `json:"data"`
	}
	if err := FetchJSON(ctx, u, map[string]string{
		"User-Agent":       config.DefaultUserAgent,
		"Referer":          base + "/",
		"X-Requested-With": "XMLHttpRequest",
	}, &data); err == nil && len(data.Data) > 0 {
		var items []MediaItem
		for _, d := range data.Data {
			if d.Slug == "" || d.Title == "" {
				continue
			}
			items = append(items, MediaItem{
				ID:        d.Slug,
				Title:     d.Title,
				Type:      MediaTypeAnime,
				PosterURL: ResolveRelativeURL(d.Thumb, base),
			})
		}
		return items, nil
	}
	return p.GetCatalog(ctx, 1)
}

func (p *AnizmProvider) GetDetails(ctx context.Context, mediaID string) (*MediaDetail, error) {
	slug := strings.TrimPrefix(mediaID, "anizm:")
	base := p.baseURL()
	htmlStr, _, err := FetchString(ctx, fmt.Sprintf("%s/%s", base, slug), DefaultHeaders(base+"/"))
	if err != nil {
		return nil, err
	}
	title := slug
	if m := regexp.MustCompile(`(?is)<h1[^>]*>(.*?)</h1>`).FindStringSubmatch(htmlStr); len(m) > 1 {
		title = CleanHTMLText(m[1])
	}

	var eps []Episode
	seen := make(map[int]bool)
	for _, m := range anizmEpRe.FindAllStringSubmatch(htmlStr, -1) {
		epSlug := m[1]
		eNum, _ := strconv.Atoi(m[2])
		if eNum <= 0 || seen[eNum] {
			continue
		}
		seen[eNum] = true
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

func (p *AnizmProvider) GetEpisodes(ctx context.Context, mediaID string, season int) ([]Episode, error) {
	det, err := p.GetDetails(ctx, mediaID)
	if err != nil {
		return nil, err
	}
	if len(det.Seasons) > 0 {
		return det.Seasons[0].Episodes, nil
	}
	return nil, nil
}

func (p *AnizmProvider) ResolveStream(ctx context.Context, streamID string) (*StreamResult, error) {
	epSlug := strings.TrimPrefix(streamID, "anizm:")
	base := p.baseURL()
	epURL := fmt.Sprintf("%s/%s", base, strings.TrimLeft(epSlug, "/"))
	htmlStr, _, err := FetchString(ctx, epURL, DefaultHeaders(base+"/"))
	if err != nil {
		return nil, err
	}

	hdrs := map[string]string{
		"User-Agent":       config.DefaultUserAgent,
		"Referer":          epURL,
		"X-Requested-With": "XMLHttpRequest",
	}

	var sources []StreamSource
	for _, tm := range anizmTranslatorRe.FindAllStringSubmatch(htmlStr, 3) {
		trURL := ResolveRelativeURL(tm[1], base)
		var trResp struct {
			Data string `json:"data"`
		}
		if err := FetchJSON(ctx, trURL, hdrs, &trResp); err != nil {
			continue
		}
		for _, vm := range anizmVideoAttrRe.FindAllStringSubmatch(trResp.Data, 4) {
			vURL := ResolveRelativeURL(vm[1], base)
			var vResp struct {
				Player string `json:"player"`
			}
			if err := FetchJSON(ctx, vURL, hdrs, &vResp); err != nil {
				continue
			}
			iframeMatch := regexp.MustCompile(`(?i)src="([^"]+)"`).FindStringSubmatch(vResp.Player)
			if len(iframeMatch) < 2 {
				continue
			}
			playerURL := ResolveRelativeURL(iframeMatch[1], base)
			pHTML, _, err := FetchString(ctx, playerURL, DefaultHeaders(epURL))
			if err != nil {
				continue
			}
			unpacked := UnpackJS(pHTML)
			combined := pHTML + "\n" + unpacked
			if fm := anizmFirePlayerRe.FindStringSubmatch(combined); len(fm) > 1 {
				sources = append(sources, ExtractHDPlayerSystem(ctx, "https://anizmplayer.com/player/index.php?data="+fm[1], playerURL)...)
			} else {
				sources = append(sources, ExtractAnyIframe(ctx, playerURL, epURL)...)
			}
		}
		if len(sources) > 0 {
			break
		}
	}
	return SelectBestStream(sources)
}
