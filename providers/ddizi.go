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

type DDiziProvider struct{}

func init() {
	Register(&DDiziProvider{})
}

func (p *DDiziProvider) ID() string   { return "ddizi" }
func (p *DDiziProvider) Name() string { return "DDizi" }

func (p *DDiziProvider) baseURL() string {
	b := config.RemoteString("urls.series.ddizi.base", "https://www.ddizi.tel")
	if strings.Contains(b, "ddizi.im") || strings.Contains(b, "ddizi.pw") {
		return "https://www.ddizi.tel"
	}
	return b
}

var (
	ddiziSeriesRe = regexp.MustCompile(`(?is)<a[^>]+href="(?:https?://[^"]*)?/diziler/([^"/?#]+(?:/[^"/?#]+)?)"[^>]*>(.*?)</a>`)
	ddiziEpRe     = regexp.MustCompile(`(?is)<a[^>]+href="(?:https?://[^"]*)?/izle/([^"/?#]+(?:/[^"/?#]+)?)"[^>]*>(.*?)</a>`)
)

func (p *DDiziProvider) parseSeries(htmlStr string) []MediaItem {
	var items []MediaItem
	seen := make(map[string]bool)
	for _, m := range ddiziSeriesRe.FindAllStringSubmatch(htmlStr, -1) {
		slug := strings.Trim(m[1], "/")
		if slug == "" || seen[slug] || strings.Contains(slug, "sayfa-") {
			continue
		}
		title := CleanMediaTitle(m[2])
		if title == "" || len(title) < 2 {
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

func (p *DDiziProvider) GetCatalog(ctx context.Context, page int) ([]MediaItem, error) {
	base := p.baseURL()
	htmlStr, _, err := FetchString(ctx, base+"/", DefaultHeaders(base+"/"))
	if err != nil {
		return nil, err
	}
	return p.parseSeries(htmlStr), nil
}

func (p *DDiziProvider) Search(ctx context.Context, query string) ([]MediaItem, error) {
	base := p.baseURL()
	form := url.Values{}
	form.Set("arama", strings.TrimSpace(query))
	htmlStr, _, err := PostForm(ctx, base+"/arama/", form, DefaultHeaders(base+"/"))
	if err != nil {
		return nil, err
	}
	return p.parseSeries(htmlStr), nil
}

func (p *DDiziProvider) GetDetails(ctx context.Context, mediaID string) (*MediaDetail, error) {
	slug := strings.Trim(strings.TrimPrefix(mediaID, "ddizi:"), "/")
	slug = strings.TrimPrefix(slug, "diziler/")
	base := p.baseURL()
	seriesURL := fmt.Sprintf("%s/diziler/%s", base, slug)
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
	for _, m := range ddiziEpRe.FindAllStringSubmatch(htmlStr, -1) {
		epSlug := strings.Trim(m[1], "/")
		if seen[epSlug] {
			continue
		}
		seen[epSlug] = true
		epTxt := CleanHTMLText(m[2])
		eNum := len(eps) + 1
		if em := regexp.MustCompile(`(\d+)\.\s*B[öo]l[üu]m`).FindStringSubmatch(epTxt); len(em) > 1 {
			eNum, _ = strconv.Atoi(em[1])
		}
		eps = append(eps, Episode{
			ID:       epSlug,
			StreamID: epSlug,
			Title:    epTxt,
			Season:   1,
			Number:   eNum,
		})
	}
	if len(eps) == 0 {
		eps = append(eps, Episode{
			ID:       slug,
			StreamID: slug,
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

func (p *DDiziProvider) GetEpisodes(ctx context.Context, mediaID string, season int) ([]Episode, error) {
	det, err := p.GetDetails(ctx, mediaID)
	if err != nil {
		return nil, err
	}
	if len(det.Seasons) > 0 {
		return det.Seasons[0].Episodes, nil
	}
	return nil, nil
}

func (p *DDiziProvider) ResolveStream(ctx context.Context, streamID string) (*StreamResult, error) {
	epSlug := strings.Trim(strings.TrimPrefix(streamID, "ddizi:"), "/")
	epSlug = strings.TrimPrefix(epSlug, "izle/")
	base := p.baseURL()
	var epURL string
	if strings.HasPrefix(epSlug, "http") {
		epURL = epSlug
	} else {
		epURL = fmt.Sprintf("%s/izle/%s", base, epSlug)
	}
	htmlStr, _, err := FetchString(ctx, epURL, DefaultHeaders(base+"/"))
	if err != nil {
		return nil, err
	}

	var sources []StreamSource
	iframeMatches := regexp.MustCompile(`(?i)<iframe[^>]+src=["']([^"']+)["']`).FindAllStringSubmatch(htmlStr, 10)
	for _, im := range iframeMatches {
		iframeURL := ResolveRelativeURL(im[1], base)
		if strings.Contains(iframeURL, "daily.php") {
			sources = append(sources, ExtractDailymotion(ctx, iframeURL)...)
		} else if strings.Contains(iframeURL, "youtube.php") {
			if ym := regexp.MustCompile(`[?&]v=([a-zA-Z0-9_-]{11})`).FindStringSubmatch(iframeURL); len(ym) > 1 {
				if s := ExtractYouTubeInnertube(ctx, ym[1]); s != nil {
					sources = append(sources, *s)
				}
			}
		} else {
			sources = append(sources, ExtractAnyIframe(ctx, iframeURL, epURL)...)
		}
	}
	return SelectBestStream(sources)
}
