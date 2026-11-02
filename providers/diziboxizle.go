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

type DiziBoxProvider struct{}

func init() {
	Register(&DiziBoxProvider{})
}

func (p *DiziBoxProvider) ID() string   { return "diziboxizle" }
func (p *DiziBoxProvider) Name() string { return "DiziBox" }

func (p *DiziBoxProvider) baseURL() string {
	return config.RemoteString("urls.series.diziboxizle.base", config.RemoteString("urls.series_foreign.diziboxizle.base", "https://diziboxizle.com"))
}

var (
	diziBoxCardRe    = regexp.MustCompile(`(?is)<a[^>]+href="(https?://[^"]*diziboxizle\.com/([^"/?#]+)/?)"[^>]*title="([^"]+)"`)
	diziBoxCatLinkRe = regexp.MustCompile(`(?is)<div\s+class="categorytitle"[^>]*>\s*<a\s+href="https?://[^"]*diziboxizle\.com/([a-z0-9-]+)/?"[^>]*>([^<]+)</a>`)
	diziBoxEpRe      = regexp.MustCompile(`(?i)href="(https?://[^"]*diziboxizle\.com/([a-z0-9-]+-(?:(\d+)-sezon-(\d+)-bolum|(\d+)-bolum)[^"/?#]*)/?)"`)
)

func (p *DiziBoxProvider) parseCards(htmlStr string) []MediaItem {
	var items []MediaItem
	seen := make(map[string]bool)
	for _, m := range diziBoxCatLinkRe.FindAllStringSubmatch(htmlStr, -1) {
		slug := m[1]
		if slug == "" || seen[slug] || strings.Contains(slug, "-sezon-") || strings.Contains(slug, "-bolum") || slug == "dizi-arsivi" || slug == "iletisim" {
			continue
		}
		seen[slug] = true
		items = append(items, MediaItem{
			ID:    slug,
			Title: CleanMediaTitle(m[2]),
			Year:  ExtractYear(m[2]),
			Type:  MediaTypeSeries,
		})
	}
	for _, m := range diziBoxCardRe.FindAllStringSubmatch(htmlStr, -1) {
		slug := m[2]
		if slug == "" || seen[slug] || strings.Contains(slug, "-sezon-") || strings.Contains(slug, "-bolum") || slug == "dizi-arsivi" || slug == "iletisim" {
			continue
		}
		seen[slug] = true
		items = append(items, MediaItem{
			ID:    slug,
			Title: CleanMediaTitle(m[3]),
			Year:  ExtractYear(m[3]),
			Type:  MediaTypeSeries,
		})
	}
	return items
}

func (p *DiziBoxProvider) GetCatalog(ctx context.Context, page int) ([]MediaItem, error) {
	if page < 1 {
		page = 1
	}
	base := p.baseURL()
	if page == 1 {
		if htmlStr, _, err := FetchString(ctx, base+"/", DefaultHeaders(base+"/")); err == nil {
			if items := p.parseCards(htmlStr); len(items) > 0 {
				return items, nil
			}
		}
	}
	htmlStr, _, err := FetchString(ctx, fmt.Sprintf("%s/dizi-arsivi/page/%d/", base, page), DefaultHeaders(base+"/"))
	if err != nil {
		return nil, err
	}
	return p.parseCards(htmlStr), nil
}

func (p *DiziBoxProvider) Search(ctx context.Context, query string) ([]MediaItem, error) {
	base := p.baseURL()
	var items []MediaItem
	seen := make(map[string]bool)

	if htmlStr, _, err := FetchString(ctx, fmt.Sprintf("%s/?s=%s", base, url.QueryEscape(strings.TrimSpace(query))), DefaultHeaders(base+"/")); err == nil {
		for _, m := range diziBoxCatLinkRe.FindAllStringSubmatch(htmlStr, -1) {
			slug := m[1]
			if slug == "" || seen[slug] || strings.Contains(slug, "-sezon-") || strings.Contains(slug, "-bolum") {
				continue
			}
			if det, err := p.GetDetails(ctx, slug); err == nil && det != nil && len(det.Seasons) > 0 {
				seen[slug] = true
				items = append(items, MediaItem{
					ID:    slug,
					Title: CleanMediaTitle(m[2]),
					Year:  ExtractYear(m[2]),
					Type:  MediaTypeSeries,
				})
			}
		}
	}

	u := fmt.Sprintf("%s/wp-json/wp/v2/categories?search=%s&per_page=10", base, url.QueryEscape(strings.TrimSpace(query)))
	var cats []struct {
		Slug  string `json:"slug"`
		Name  string `json:"name"`
		Count int    `json:"count"`
	}
	if err := FetchJSON(ctx, u, DefaultHeaders(base+"/"), &cats); err == nil && len(cats) > 0 {
		for _, c := range cats {
			if c.Slug != "" && c.Name != "" && c.Count > 0 && !seen[c.Slug] {
				if det, err := p.GetDetails(ctx, c.Slug); err == nil && det != nil && len(det.Seasons) > 0 {
					seen[c.Slug] = true
					items = append(items, MediaItem{
						ID:    c.Slug,
						Title: CleanMediaTitle(c.Name),
						Type:  MediaTypeSeries,
					})
				}
			}
		}
	}
	return items, nil
}

func (p *DiziBoxProvider) GetDetails(ctx context.Context, mediaID string) (*MediaDetail, error) {
	slug := strings.Trim(strings.TrimPrefix(mediaID, "diziboxizle:"), "/")
	base := p.baseURL()
	seriesURL := fmt.Sprintf("%s/%s/", base, slug)
	htmlStr, _, err := FetchString(ctx, seriesURL, DefaultHeaders(base+"/"))
	if err != nil {
		return nil, err
	}
	title := slug
	if m := regexp.MustCompile(`(?is)<h1[^>]*>(.*?)</h1>`).FindStringSubmatch(htmlStr); len(m) > 1 {
		title = CleanMediaTitle(m[1])
	}

	seasonMap := make(map[int][]Episode)
	seen := make(map[string]bool)
	for _, m := range diziBoxEpRe.FindAllStringSubmatch(htmlStr, -1) {
		epURL := m[1]
		sNum := 1
		eNum := 0
		if m[3] != "" && m[4] != "" {
			sNum, _ = strconv.Atoi(m[3])
			eNum, _ = strconv.Atoi(m[4])
		} else if m[5] != "" {
			eNum, _ = strconv.Atoi(m[5])
		}
		if sNum <= 0 {
			sNum = 1
		}
		if eNum <= 0 {
			continue
		}
		key := fmt.Sprintf("%d_%d", sNum, eNum)
		if seen[key] {
			continue
		}
		seen[key] = true
		seasonMap[sNum] = append(seasonMap[sNum], Episode{
			ID:       epURL,
			StreamID: epURL,
			Title:    fmt.Sprintf("%d. Sezon %d. Bölüm", sNum, eNum),
			Season:   sNum,
			Number:   eNum,
		})
	}
	if len(seasonMap) == 0 {
		if strings.Contains(slug, "-bolum") {
			seasonMap[1] = []Episode{
				{
					ID:       fmt.Sprintf("%s/%s/", base, slug),
					StreamID: fmt.Sprintf("%s/%s/", base, slug),
					Title:    "1. Sezon 1. Bölüm",
					Season:   1,
					Number:   1,
				},
			}
		} else {
			return nil, fmt.Errorf("diziboxizle: no episodes found for %s", slug)
		}
	}

	var seasons []Season
	for sNum, eps := range seasonMap {
		seasons = append(seasons, Season{
			Number:   sNum,
			Name:     fmt.Sprintf("Season %02d", sNum),
			Episodes: eps,
		})
	}
	return &MediaDetail{
		MediaItem: MediaItem{
			ID:    slug,
			Title: title,
			Type:  MediaTypeSeries,
		},
		Seasons: seasons,
	}, nil
}

func (p *DiziBoxProvider) GetEpisodes(ctx context.Context, mediaID string, season int) ([]Episode, error) {
	det, err := p.GetDetails(ctx, mediaID)
	if err != nil {
		return nil, err
	}
	for _, s := range det.Seasons {
		if s.Number == season {
			return s.Episodes, nil
		}
	}
	return nil, nil
}

func (p *DiziBoxProvider) ResolveStream(ctx context.Context, streamID string) (*StreamResult, error) {
	epURL := strings.TrimPrefix(streamID, "diziboxizle:")
	base := p.baseURL()
	htmlStr, _, err := FetchString(ctx, epURL, DefaultHeaders(base+"/"))
	if err != nil {
		return nil, err
	}
	pageSubs := ExtractSubtitles(htmlStr, base)
	aType := DetectAudioType(htmlStr)
	if aType == AudioTypeUnknown {
		aType = AudioTypeSub
	}
	dubTag := "[TR Altyazı]"
	if aType == AudioTypeDub {
		dubTag = "[TR Dublaj]"
	}

	var sources []StreamSource
	seenIframes := make(map[string]bool)
	extractFromHTML := func(pageHTML, refURL string) {
		for _, im := range regexp.MustCompile(`(?i)<iframe[^>]+src="([^"]+)"`).FindAllStringSubmatch(pageHTML, 5) {
			u := strings.TrimSpace(im[1])
			if u == "" || seenIframes[u] || strings.Contains(u, "youtube.com") || strings.Contains(u, "facebook.com") {
				continue
			}
			seenIframes[u] = true
			extracted := ExtractAnyIframe(ctx, u, refURL)
			for i := range extracted {
				if len(extracted[i].Subtitles) == 0 && len(pageSubs) > 0 {
					extracted[i].Subtitles = pageSubs
				}
				if extracted[i].AudioType == AudioTypeUnknown {
					extracted[i].AudioType = aType
				}
				if !strings.Contains(extracted[i].Title, "[TR") {
					extracted[i].Title = fmt.Sprintf("DiziBox • %s • %s", dubTag, extracted[i].Quality)
				}
				sources = append(sources, extracted[i])
			}
		}
	}

	extractFromHTML(htmlStr, epURL)

	// Also follow alternate player part links (e.g., /2/, /3/ with class="post-page-numbers")
	seenParts := map[string]bool{epURL: true}
	for _, pm := range regexp.MustCompile(`(?i)<a[^>]+href="(https?://[^"]*diziboxizle\.com/[^"]+/\d+/)"[^>]*class="[^"]*post-page-numbers[^"]*"`).FindAllStringSubmatch(htmlStr, 4) {
		partURL := pm[1]
		if seenParts[partURL] {
			continue
		}
		seenParts[partURL] = true
		if partHTML, _, err := FetchString(ctx, partURL, DefaultHeaders(epURL)); err == nil && partHTML != "" {
			extractFromHTML(partHTML, partURL)
		}
	}

	return SelectBestStream(sources)
}
