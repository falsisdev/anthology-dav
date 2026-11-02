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
	return config.RemoteString("urls.series.trdiziizle.base", config.RemoteString("urls.series_local.trdiziizle.base", "https://www.trdiziizle.tv/tr2"))
}

var (
	trDiziSeriesLinkRe = regexp.MustCompile(`(?is)<a[^>]+href="(https?://[^"]*trdiziizle\.[^"]+/diziler/([^"/?#]+)/?)"[^>]*>(.*?)</a>`)
	trDiziArticleRe    = regexp.MustCompile(`(?is)<h\d[^>]+class="[^"]*entry-title[^"]*"[^>]*>\s*<a[^>]+href="(https?://[^"]*trdiziizle\.[^"]+/([^"/?#]+)/?)"[^>]*>(.*?)</a>`)
	trDiziLinkRe       = regexp.MustCompile(`(?is)<a[^>]+href="(https?://[^"]*trdiziizle\.[^"]+/([^"/?#]+)/?)"[^>]*>(.*?)</a>`)
)

func isIgnoredTrDiziSlug(slug string) bool {
	s := strings.ToLower(strings.Trim(slug, "/"))
	if s == "" || s == "tr2" || s == "diziler" || s == "tum-bolumler" {
		return true
	}
	ignored := []string{"uye-ol", "giris", "iletisim", "kategori", "gizlilik", "hakkimizda", "yorum", "feed", "wp-", "kayit", "dmca", "etiket", "author", "page"}
	for _, ig := range ignored {
		if strings.Contains(s, ig) {
			return true
		}
	}
	return false
}

func (p *TrDiziIzleProvider) parseLinks(htmlStr string) []MediaItem {
	var items []MediaItem
	seen := make(map[string]bool)

	// 1. Prefer /diziler/{slug}/ links
	for _, m := range trDiziSeriesLinkRe.FindAllStringSubmatch(htmlStr, -1) {
		fullURL := m[1]
		slug := m[2]
		if isIgnoredTrDiziSlug(slug) || seen[slug] {
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

	// 2. Also include search <article> entry-title links
	for _, m := range trDiziArticleRe.FindAllStringSubmatch(htmlStr, -1) {
		fullURL := m[1]
		slug := m[2]
		if isIgnoredTrDiziSlug(slug) || seen[slug] {
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
	if strings.Contains(strings.ToLower(pageURL), "bolum") && !strings.Contains(strings.ToLower(pageURL), "/tum-bolumler") {
		seen[pageURL] = true
		eNum := 1
		if em := regexp.MustCompile(`-(\d+)-bolum`).FindStringSubmatch(strings.ToLower(pageURL)); len(em) > 1 {
			eNum, _ = strconv.Atoi(em[1])
		}
		eps = append(eps, Episode{
			ID:       pageURL,
			StreamID: pageURL,
			Title:    fmt.Sprintf("Bölüm %d", eNum),
			Season:   1,
			Number:   eNum,
		})
	}
	for _, m := range trDiziLinkRe.FindAllStringSubmatch(htmlStr, -1) {
		href := m[1]
		slug := m[2]
		txt := CleanHTMLText(m[3])
		hrefLower := strings.ToLower(href)
		if seen[href] || isIgnoredTrDiziSlug(slug) || strings.Contains(hrefLower, "/tum-bolumler") || strings.Contains(hrefLower, "/diziler/") {
			continue
		}
		if !strings.Contains(strings.ToLower(txt), "bölüm") && !strings.Contains(hrefLower, "bolum") {
			continue
		}
		seen[href] = true
		eNum := 0
		if em := regexp.MustCompile(`(\d+)\.\s*B[öo]l[üu]m`).FindStringSubmatch(txt); len(em) > 1 {
			eNum, _ = strconv.Atoi(em[1])
		} else if em := regexp.MustCompile(`-(\d+)-bolum`).FindStringSubmatch(hrefLower); len(em) > 1 {
			eNum, _ = strconv.Atoi(em[1])
		}
		if eNum <= 0 {
			eNum = len(eps) + 1
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
	for _, im := range regexp.MustCompile(`(?i)<iframe[^>]+(?:data-src|src)="([^"]+)"`).FindAllStringSubmatch(htmlStr, 8) {
		u := strings.TrimSpace(im[1])
		if u == "" || u == "about:blank" {
			continue
		}
		sources = append(sources, ExtractAnyIframe(ctx, ResolveRelativeURL(u, base), epURL)...)
	}
	if res, err := SelectBestStream(sources); err == nil && res != nil {
		return res, nil
	}
	// Official YouTube full-episode fallback if embedded YouTube video was removed
	if m := regexp.MustCompile(`(?is)<h1[^>]*>(.*?)</h1>`).FindStringSubmatch(htmlStr); len(m) > 1 {
		q := CleanMediaTitle(m[1])
		if q != "" {
			for _, h := range SearchYouTubeInnertube(ctx, q) {
				if h.DurationSec >= 900 {
					if yt := ExtractYouTubeInnertube(ctx, h.VideoID); yt != nil {
						sources = append(sources, *yt)
						break
					}
				}
			}
		}
	}
	return SelectBestStream(sources)
}
