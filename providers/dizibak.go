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

type DiziBakProvider struct{}

func init() {
	Register(&DiziBakProvider{})
}

func (p *DiziBakProvider) ID() string   { return "dizibak" }
func (p *DiziBakProvider) Name() string { return "DiziBak" }

func (p *DiziBakProvider) baseURL() string {
	return config.RemoteString("urls.series_foreign.dizibak.base", "https://dizibak.net")
}

var (
	diziBakSeriesRe = regexp.MustCompile(`(?is)<a[^>]+href="(https?://[^"]*dizibak\.[^/]+/diziler/([^"/?#]+)/?)"[^>]*>(.*?)</a>`)
	diziBakEpRe     = regexp.MustCompile(`(?i)href="(https?://[^"]*dizibak\.[^/]+/[a-z0-9-]+-(\d+)-sezon-(\d+)-bolum/?)"`)
	diziBakPostIDRe = regexp.MustCompile(`(?i)data-post-id="(\d+)"`)
	diziBakNonceRe  = regexp.MustCompile(`(?i)data-nonce="([a-f0-9]+)"`)
)

func (p *DiziBakProvider) parseCards(htmlStr string) []MediaItem {
	var items []MediaItem
	seen := make(map[string]bool)
	for _, m := range diziBakSeriesRe.FindAllStringSubmatch(htmlStr, -1) {
		slug := m[2]
		if slug == "" || seen[slug] {
			continue
		}
		title := CleanMediaTitle(m[3])
		if title == "" {
			title = slug
		}
		seen[slug] = true
		items = append(items, MediaItem{
			ID:    slug,
			Title: title,
			Year:  ExtractYear(m[3]),
			Type:  MediaTypeSeries,
		})
	}
	return items
}

func (p *DiziBakProvider) GetCatalog(ctx context.Context, page int) ([]MediaItem, error) {
	base := p.baseURL()
	htmlStr, _, err := FetchString(ctx, base+"/", DefaultHeaders(base+"/"))
	if err != nil {
		return nil, err
	}
	return p.parseCards(htmlStr), nil
}

func (p *DiziBakProvider) Search(ctx context.Context, query string) ([]MediaItem, error) {
	base := p.baseURL()
	homeHTML, _, _ := FetchString(ctx, base+"/", DefaultHeaders(base+"/"))
	nonceMatch := regexp.MustCompile(`live_search_obj[\s\S]*?nonce\s*:\s*["']([a-f0-9]+)["']`).FindStringSubmatch(homeHTML)
	if len(nonceMatch) > 1 {
		form := url.Values{}
		form.Set("action", "live_search")
		form.Set("keyword", strings.TrimSpace(query))
		form.Set("nonce", nonceMatch[1])
		respStr, _, err := PostForm(ctx, base+"/wp-admin/admin-ajax.php", form, map[string]string{
			"User-Agent":       config.DefaultUserAgent,
			"Referer":          base + "/",
			"X-Requested-With": "XMLHttpRequest",
		})
		if err == nil && len(respStr) > 0 {
			var res struct {
				Success bool `json:"success"`
				Data    struct {
					Diziler []struct {
						Title string `json:"title"`
						Link  string `json:"link"`
					} `json:"diziler"`
					Filmler []struct {
						Title string `json:"title"`
						Link  string `json:"link"`
					} `json:"filmler"`
				} `json:"data"`
			}
			if json.Unmarshal([]byte(respStr), &res) == nil && res.Success {
				var items []MediaItem
				for _, d := range res.Data.Diziler {
					slug := strings.Trim(strings.TrimPrefix(d.Link, base+"/diziler/"), "/")
					if slug == "" {
						parts := strings.Split(strings.Trim(d.Link, "/"), "/")
						slug = parts[len(parts)-1]
					}
					items = append(items, MediaItem{
						ID:    slug,
						Title: CleanMediaTitle(d.Title),
						Type:  MediaTypeSeries,
					})
				}
				for _, f := range res.Data.Filmler {
					parts := strings.Split(strings.Trim(f.Link, "/"), "/")
					slug := parts[len(parts)-1]
					items = append(items, MediaItem{
						ID:    slug,
						Title: CleanMediaTitle(f.Title),
						Type:  MediaTypeMovie,
					})
				}
				if len(items) > 0 {
					return items, nil
				}
			}
			if items := p.parseCards(respStr); len(items) > 0 {
				return items, nil
			}
		}
	}
	htmlStr, _, err := FetchString(ctx, fmt.Sprintf("%s/?s=%s", base, url.QueryEscape(query)), DefaultHeaders(base+"/"))
	if err != nil {
		return nil, err
	}
	return p.parseCards(htmlStr), nil
}

func (p *DiziBakProvider) GetDetails(ctx context.Context, mediaID string) (*MediaDetail, error) {
	slug := strings.TrimPrefix(mediaID, "dizibak:")
	base := p.baseURL()
	seriesURL := fmt.Sprintf("%s/diziler/%s/", base, strings.Trim(slug, "/"))
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
	for _, m := range diziBakEpRe.FindAllStringSubmatch(htmlStr, -1) {
		epURL := m[1]
		sNum, _ := strconv.Atoi(m[2])
		eNum, _ := strconv.Atoi(m[3])
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
		seasonMap[1] = []Episode{
			{
				ID:       fmt.Sprintf("%s/%s-1-sezon-1-bolum/", base, slug),
				StreamID: fmt.Sprintf("%s/%s-1-sezon-1-bolum/", base, slug),
				Title:    "1. Sezon 1. Bölüm",
				Season:   1,
				Number:   1,
			},
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

func (p *DiziBakProvider) GetEpisodes(ctx context.Context, mediaID string, season int) ([]Episode, error) {
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

func (p *DiziBakProvider) ResolveStream(ctx context.Context, streamID string) (*StreamResult, error) {
	epURL := strings.TrimPrefix(streamID, "dizibak:")
	base := p.baseURL()
	htmlStr, _, err := FetchString(ctx, epURL, DefaultHeaders(base+"/"))
	if err != nil {
		return nil, err
	}

	var sources []StreamSource
	pm := diziBakPostIDRe.FindStringSubmatch(htmlStr)
	nm := diziBakNonceRe.FindStringSubmatch(htmlStr)
	if len(pm) > 1 && len(nm) > 1 {
		for _, sourceType := range []string{"altyazi", "dublaj"} {
			curAType := AudioTypeSub
			dubTag := "[TR Altyazı]"
			if sourceType == "dublaj" {
				curAType = AudioTypeDub
				dubTag = "[TR Dublaj]"
			}

			form := url.Values{}
			form.Set("action", "load_player_content")
			form.Set("post_id", pm[1])
			form.Set("nonce", nm[1])
			form.Set("source", sourceType)
			respStr, _, err := PostForm(ctx, base+"/wp-admin/admin-ajax.php", form, map[string]string{
				"User-Agent":       config.DefaultUserAgent,
				"Referer":          epURL,
				"X-Requested-With": "XMLHttpRequest",
			})
			if err == nil && len(respStr) > 0 {
				cleanResp := strings.ReplaceAll(respStr, `\/`, `/`)
				cleanResp = strings.ReplaceAll(cleanResp, `\`, "")
				if im := regexp.MustCompile(`(?i)src=["']([^"']+)["']`).FindStringSubmatch(cleanResp); len(im) > 1 {
					embedURL := im[1]
					if strings.HasPrefix(embedURL, "//") {
						embedURL = "https:" + embedURL
					}
					extracted := ExtractAnyIframe(ctx, embedURL, epURL)
					for i := range extracted {
						extracted[i].AudioType = curAType
						if !strings.Contains(extracted[i].Title, dubTag) {
							extracted[i].Title = fmt.Sprintf("DiziBak • %s • %s", dubTag, extracted[i].Quality)
						}
						sources = append(sources, extracted[i])
					}
				}
			}
		}
	}
	if len(sources) == 0 {
		for _, im := range regexp.MustCompile(`(?i)<iframe[^>]+src="([^"]+)"`).FindAllStringSubmatch(htmlStr, 3) {
			sources = append(sources, ExtractAnyIframe(ctx, im[1], epURL)...)
		}
	}
	return SelectBestStream(sources)
}
