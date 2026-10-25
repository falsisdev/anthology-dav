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

type DiziPodProvider struct{}

func init() {
	Register(&DiziPodProvider{})
}

func (p *DiziPodProvider) ID() string   { return "dizipod" }
func (p *DiziPodProvider) Name() string { return "DiziPod" }

func (p *DiziPodProvider) baseURL() string {
	return config.RemoteString("urls.series_foreign.dizipod.base", "https://dizipod.com")
}

var (
	diziPodCardRe   = regexp.MustCompile(`(?is)<a[^>]+href="(https?://[^"]*dizipod\.com/(?:diziler|film)/([^"/?#]+)/?)"[^>]*>(.*?)</a>`)
	diziPodEpLinkRe = regexp.MustCompile(`(?i)href="(https?://[^"]*dizipod\.com/([a-z0-9-]+-(\d+)-sezon-(\d+)-bolum)/?)"`)
	diziPodPostIDRe = regexp.MustCompile(`(?i)data-post-id="(\d+)"`)
)

func (p *DiziPodProvider) parseCards(htmlStr string) []MediaItem {
	var items []MediaItem
	seen := make(map[string]bool)
	for _, m := range diziPodCardRe.FindAllStringSubmatch(htmlStr, -1) {
		fullURL := m[1]
		slug := m[2]
		if slug == "" || seen[slug] {
			continue
		}
		inner := m[3]
		title := CleanMediaTitle(inner)
		if title == "" {
			title = slug
		}
		seen[slug] = true
		mType := MediaTypeSeries
		if strings.Contains(fullURL, "/film/") {
			mType = MediaTypeMovie
		}
		items = append(items, MediaItem{
			ID:    fullURL,
			Title: title,
			Year:  ExtractYear(inner),
			Type:  mType,
		})
	}
	return items
}

func (p *DiziPodProvider) GetCatalog(ctx context.Context, page int) ([]MediaItem, error) {
	base := p.baseURL()
	htmlStr, _, err := FetchString(ctx, base+"/trendler/", DefaultHeaders(base+"/"))
	if err != nil {
		return nil, err
	}
	return p.parseCards(htmlStr), nil
}

func (p *DiziPodProvider) Search(ctx context.Context, query string) ([]MediaItem, error) {
	base := p.baseURL()
	form := url.Values{}
	form.Set("action", "dp_live_search")
	form.Set("q", strings.TrimSpace(query))
	respStr, _, err := PostForm(ctx, base+"/wp/wp-admin/admin-ajax.php", form, map[string]string{
		"User-Agent":       config.DefaultUserAgent,
		"Referer":          base + "/",
		"X-Requested-With": "XMLHttpRequest",
		"Content-Type":     "application/x-www-form-urlencoded",
	})
	if err == nil && len(respStr) > 0 {
		var ajaxRes struct {
			Success bool `json:"success"`
			Data    struct {
				Films []struct {
					ID    int    `json:"id"`
					Title string `json:"title"`
					URL   string `json:"url"`
					IMDb  string `json:"imdb"`
				} `json:"films"`
				Series []struct {
					ID    int    `json:"id"`
					Title string `json:"title"`
					URL   string `json:"url"`
					IMDb  string `json:"imdb"`
				} `json:"series"`
			} `json:"data"`
		}
		if err := json.Unmarshal([]byte(respStr), &ajaxRes); err == nil && ajaxRes.Success {
			var items []MediaItem
			for _, s := range ajaxRes.Data.Series {
				items = append(items, MediaItem{
					ID:    s.URL,
					Title: CleanMediaTitle(s.Title),
					Type:  MediaTypeSeries,
				})
			}
			for _, f := range ajaxRes.Data.Films {
				items = append(items, MediaItem{
					ID:    f.URL,
					Title: CleanMediaTitle(f.Title),
					Type:  MediaTypeMovie,
				})
			}
			if len(items) > 0 {
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

func (p *DiziPodProvider) GetDetails(ctx context.Context, mediaID string) (*MediaDetail, error) {
	pageURL := strings.TrimPrefix(mediaID, "dizipod:")
	if !strings.HasPrefix(pageURL, "http") {
		pageURL = p.baseURL() + "/diziler/" + strings.Trim(pageURL, "/") + "/"
	}
	htmlStr, _, err := FetchString(ctx, pageURL, DefaultHeaders(p.baseURL()+"/"))
	if err != nil {
		return nil, err
	}
	title := "DiziPod"
	if m := regexp.MustCompile(`(?is)<h1[^>]*>(.*?)</h1>`).FindStringSubmatch(htmlStr); len(m) > 1 {
		title = CleanMediaTitle(m[1])
	}

	if strings.Contains(pageURL, "/film/") {
		return &MediaDetail{
			MediaItem: MediaItem{
				ID:    pageURL,
				Title: title,
				Type:  MediaTypeMovie,
			},
			StreamID: pageURL,
		}, nil
	}

	seasonMap := make(map[int][]Episode)
	seen := make(map[string]bool)
	for _, m := range diziPodEpLinkRe.FindAllStringSubmatch(htmlStr, -1) {
		epURL := m[1]
		sNum, _ := strconv.Atoi(m[3])
		eNum, _ := strconv.Atoi(m[4])
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
				ID:       pageURL,
				StreamID: pageURL,
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
			ID:    pageURL,
			Title: title,
			Type:  MediaTypeSeries,
		},
		Seasons: seasons,
	}, nil
}

func (p *DiziPodProvider) GetEpisodes(ctx context.Context, mediaID string, season int) ([]Episode, error) {
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

func (p *DiziPodProvider) ResolveStream(ctx context.Context, streamID string) (*StreamResult, error) {
	epURL := strings.TrimPrefix(streamID, "dizipod:")
	base := p.baseURL()
	htmlStr, _, err := FetchString(ctx, epURL, DefaultHeaders(base+"/"))
	if err != nil {
		return nil, err
	}

	var sources []StreamSource
	for _, m := range diziPodPostIDRe.FindAllStringSubmatch(htmlStr, 4) {
		postID := m[1]
		ajaxURL := fmt.Sprintf("%s/wp/wp-admin/admin-ajax.php?action=get_episode_player&post_id=%s", base, postID)
		respStr, _, err := FetchString(ctx, ajaxURL, map[string]string{
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
				} else if strings.HasPrefix(embedURL, "/") {
					embedURL = "https://player.dizipod.com" + embedURL
				}
				sources = append(sources, ExtractAnyIframe(ctx, embedURL, epURL)...)
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
