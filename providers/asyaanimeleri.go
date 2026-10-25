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
	asyaCardRe   = regexp.MustCompile(`(?is)<a\s+href="(https?://[^"]*asyaanimeleri\.[^/]+/series/[^"]+)"[^>]*title="([^"]+)"`)
	asyaCardArea = regexp.MustCompile(`(?is)<div class="listupd">(.*?)<div class="pagination">`)
	asyaEpLiRe   = regexp.MustCompile(`(?is)<li[^>]*>\s*<a\s+href="([^"]+)"[^>]*>(.*?)</a>\s*</li>`)
	asyaEpNumRe  = regexp.MustCompile(`(?is)<div class="epl-num">([^<]+)</div>`)
	asyaOptRe    = regexp.MustCompile(`(?i)<option\s+value="([A-Za-z0-9+/=]{16,})"`)
	asyaIframeRe = regexp.MustCompile(`(?i)<iframe[^>]+src=["']([^"']+)["']`)
)

func (p *AsyaAnimeleriProvider) GetCatalog(ctx context.Context, page int) ([]MediaItem, error) {
	base := p.baseURL()
	u := base + "/"
	if page > 1 {
		u = fmt.Sprintf("%s/series/page/%d/", base, page)
	}
	htmlStr, _, err := FetchString(ctx, u, DefaultHeaders(base+"/"))
	if err != nil {
		return nil, err
	}
	return p.parseCards(htmlStr), nil
}

func (p *AsyaAnimeleriProvider) parseCards(htmlStr string) []MediaItem {
	var items []MediaItem
	seen := make(map[string]bool)
	target := htmlStr
	if m := asyaCardArea.FindStringSubmatch(htmlStr); len(m) > 1 {
		target = m[1]
	}
	for _, m := range asyaCardRe.FindAllStringSubmatch(target, -1) {
		rawURL := m[1]
		title := CleanHTMLText(m[2])
		slug := rawURL
		if parts := strings.Split(strings.TrimRight(rawURL, "/"), "/"); len(parts) > 0 {
			slug = parts[len(parts)-1]
		}
		if slug == "" || seen[slug] {
			continue
		}
		seen[slug] = true
		items = append(items, MediaItem{
			ID:    slug,
			Title: title,
			Year:  ExtractYear(title),
			Type:  MediaTypeAnime,
		})
	}
	return items
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
	slug := strings.TrimPrefix(mediaID, "asyaanimeleri:")
	base := p.baseURL()
	pageURL := fmt.Sprintf("%s/series/%s/", base, strings.Trim(slug, "/"))
	htmlStr, _, err := FetchString(ctx, pageURL, DefaultHeaders(base+"/"))
	if err != nil {
		return nil, err
	}

	title := slug
	if m := regexp.MustCompile(`(?is)<h1[^>]*class="entry-title"[^>]*>(.*?)</h1>`).FindStringSubmatch(htmlStr); len(m) > 1 {
		title = CleanHTMLText(m[1])
	}

	var eps []Episode
	seen := make(map[int]bool)

	sub := htmlStr
	if idx := strings.Index(htmlStr, `class="eplister"`); idx != -1 {
		sub = htmlStr[idx:]
		if endIdx := strings.Index(sub, `</ul>`); endIdx != -1 {
			sub = sub[:endIdx]
		}
	}

	for _, m := range asyaEpLiRe.FindAllStringSubmatch(sub, -1) {
		epURL := m[1]
		inner := m[2]
		numStr := ""
		if nm := asyaEpNumRe.FindStringSubmatch(inner); len(nm) > 1 {
			numStr = strings.TrimSpace(nm[1])
		}
		eNum := 0
		if numStr != "" {
			if strings.Contains(numStr, "-") {
				parts := strings.Split(numStr, "-")
				eNum, _ = strconv.Atoi(strings.TrimSpace(parts[0]))
			} else {
				eNum, _ = strconv.Atoi(numStr)
			}
		}
		if eNum <= 0 {
			if mm := regexp.MustCompile(`-(\d+)-bolum`).FindStringSubmatch(epURL); len(mm) > 1 {
				eNum, _ = strconv.Atoi(mm[1])
			}
		}
		if eNum <= 0 {
			continue
		}
		if seen[eNum] {
			continue
		}
		seen[eNum] = true
		eps = append(eps, Episode{
			ID:       epURL,
			StreamID: epURL,
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
	base := p.baseURL()
	htmlStr, _, err := FetchString(ctx, epURL, DefaultHeaders(base+"/"))
	if err != nil {
		return nil, err
	}

	var iframeURLs []string
	seen := make(map[string]bool)

	// 1. Decode base64 <option value="..."> mirrors
	for _, m := range asyaOptRe.FindAllStringSubmatch(htmlStr, -1) {
		decoded, err := base64.StdEncoding.DecodeString(m[1])
		if err != nil {
			continue
		}
		if im := asyaIframeRe.FindStringSubmatch(string(decoded)); len(im) > 1 {
			u := strings.TrimSpace(im[1])
			if strings.HasPrefix(u, "//") {
				u = "https:" + u
			}
			if u != "" && !seen[u] {
				seen[u] = true
				iframeURLs = append(iframeURLs, u)
			}
		}
	}

	// 2. Direct #pembed iframe
	for _, im := range asyaIframeRe.FindAllStringSubmatch(htmlStr, -1) {
		u := strings.TrimSpace(im[1])
		if strings.HasPrefix(u, "//") {
			u = "https:" + u
		}
		if u != "" && !seen[u] {
			seen[u] = true
			iframeURLs = append(iframeURLs, u)
		}
	}

	var sources []StreamSource
	ref := base + "/"

	for _, u := range iframeURLs {
		lower := strings.ToLower(u)
		switch {
		case strings.Contains(lower, "ok.ru"):
			sources = append(sources, ExtractOkRu(ctx, u)...)
		case strings.Contains(lower, "sibnet.ru"):
			sources = append(sources, ExtractSibnet(ctx, u, ref)...)
		case strings.Contains(lower, "vidmoly"):
			sources = append(sources, ExtractVidMoly(ctx, u, ref)...)
		case strings.Contains(lower, "mail.ru"):
			sources = append(sources, ExtractMailRu(ctx, u)...)
		case strings.Contains(lower, "rumble.com"):
			sources = append(sources, ExtractRumble(ctx, u)...)
		default:
			sources = append(sources, ExtractAnyIframe(ctx, u, epURL)...)
		}
		if len(sources) >= 5 {
			break
		}
	}

	if len(sources) == 0 {
		return nil, fmt.Errorf("asyaanimeleri: no stream found for '%s'", epURL)
	}

	return SelectBestStream(sources)
}
