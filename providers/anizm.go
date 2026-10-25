package providers

import (
	"context"
	"fmt"
	"io"
	"net/http"
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

func (p *AnizmProvider) playerAPI() string {
	return config.RemoteString("urls.anime.anizm.player_api", "https://anizmplayer.com")
}

var (
	anizmEpHrefRe      = regexp.MustCompile(`(?i)href="(https?://anizm\.net/([a-z0-9-]+-(\d+)-bolum(?:-izle)?))"`)
	anizmDataSourcesRe = regexp.MustCompile(`(?i)data-sources="(https?://anizm\.net/bolum/\d+/cevirmen/\d+)"`)
	anizmCsrfRe        = regexp.MustCompile(`(?i)<meta\s+name="csrf-token"\s+content="([^"]+)"`)
	anizmFirePlayerRe  = regexp.MustCompile(`(?i)(?:FirePlayer\s*\(\s*["']([a-f0-9]+)["']|anizmplayer\.com/video/([a-f0-9]+))`)
)

func (p *AnizmProvider) ajaxHeaders(referer, csrf string) map[string]string {
	hdrs := map[string]string{
		"User-Agent":       "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/122.0.0.0 Safari/537.36",
		"Referer":          referer,
		"X-Requested-With": "XMLHttpRequest",
		"Accept":           "application/json, text/plain, */*",
	}
	if csrf != "" {
		hdrs["X-CSRF-TOKEN"] = csrf
	}
	return hdrs
}

func (p *AnizmProvider) GetCatalog(ctx context.Context, page int) ([]MediaItem, error) {
	return p.Search(ctx, "a")
}

func (p *AnizmProvider) Search(ctx context.Context, query string) ([]MediaItem, error) {
	base := p.baseURL()
	// 1. Primary: Instant v3 autocomplete / suggestion API (never 500s)
	suggestURL := fmt.Sprintf("%s/arama-onerileri?q=%s", base, url.QueryEscape(strings.TrimSpace(query)))
	var sugResp struct {
		Results []struct {
			Title string `json:"title"`
			Year  string `json:"year"`
			URL   string `json:"url"`
		} `json:"results"`
	}
	hdrs := p.ajaxHeaders(base+"/", "")
	if err := FetchJSON(ctx, suggestURL, hdrs, &sugResp); err == nil && len(sugResp.Results) > 0 {
		var items []MediaItem
		for _, r := range sugResp.Results {
			slug := r.URL
			if parts := strings.Split(strings.TrimRight(r.URL, "/"), "/"); len(parts) > 0 {
				slug = parts[len(parts)-1]
			}
			if slug == "" {
				continue
			}
			items = append(items, MediaItem{
				ID:    slug,
				Title: CleanHTMLText(r.Title),
				Year:  ExtractYear(r.Year),
				Type:  MediaTypeAnime,
			})
		}
		if len(items) > 0 {
			return items, nil
		}
	}

	// 2. Secondary: Legacy searchAnime API
	u := fmt.Sprintf("%s/searchAnime?query=%s&page=1", base, url.QueryEscape(strings.TrimSpace(query)))
	var resp struct {
		Data []struct {
			ID       int    `json:"info_id"`
			Slug     string `json:"info_slug"`
			Title    string `json:"info_title"`
			TitleEn  string `json:"info_titleenglish"`
			TitleOrg string `json:"info_titleoriginal"`
			Poster   string `json:"info_poster"`
			Year     string `json:"info_year"`
		} `json:"data"`
	}
	if err := FetchJSON(ctx, u, hdrs, &resp); err == nil && len(resp.Data) > 0 {
		var items []MediaItem
		for _, d := range resp.Data {
			if d.Slug == "" {
				continue
			}
			title := d.Title
			if title == "" {
				title = d.TitleEn
			}
			if title == "" {
				title = d.TitleOrg
			}
			if title == "" {
				title = d.Slug
			}
			poster := d.Poster
			if poster != "" && !strings.HasPrefix(poster, "http") {
				poster = fmt.Sprintf("%s/storage/pcovers/%s", base, poster)
			}
			items = append(items, MediaItem{
				ID:        d.Slug,
				Title:     CleanHTMLText(title),
				Year:      ExtractYear(d.Year),
				Type:      MediaTypeAnime,
				PosterURL: poster,
			})
		}
		return items, nil
	}

	return nil, fmt.Errorf("anizm: no search results for '%s'", query)
}

func (p *AnizmProvider) GetDetails(ctx context.Context, mediaID string) (*MediaDetail, error) {
	slug := strings.TrimPrefix(mediaID, "anizm:")
	base := p.baseURL()
	pageURL := fmt.Sprintf("%s/%s", base, strings.Trim(slug, "/"))
	htmlStr, _, err := FetchString(ctx, pageURL, DefaultHeaders(base+"/"))
	if err != nil {
		return nil, err
	}

	title := slug
	if m := regexp.MustCompile(`(?is)<h1[^>]*>(.*?)</h1>`).FindStringSubmatch(htmlStr); len(m) > 1 {
		title = CleanHTMLText(m[1])
	}

	var eps []Episode
	seen := make(map[string]bool)

	// 1. Check v3-anime-episode-grid
	if gridIdx := strings.Index(htmlStr, `v3-anime-episode-grid`); gridIdx != -1 {
		gridSub := htmlStr[gridIdx:]
		if endIdx := strings.Index(gridSub, `</section>`); endIdx != -1 {
			gridSub = gridSub[:endIdx]
		}
		reGridA := regexp.MustCompile(`(?is)<a\s+href="(https?://anizm\.net/[^"]+)"[^>]*>.*?<strong>([^<]+)</strong>`)
		for _, m := range reGridA.FindAllStringSubmatch(gridSub, -1) {
			epURL := m[1]
			label := strings.TrimSpace(m[2])
			if seen[epURL] {
				continue
			}
			seen[epURL] = true
			eNum := 1
			if nm := regexp.MustCompile(`(\d+)`).FindStringSubmatch(label); len(nm) > 1 {
				eNum, _ = strconv.Atoi(nm[1])
			}
			eps = append(eps, Episode{
				ID:       epURL,
				StreamID: epURL,
				Title:    label,
				Season:   1,
				Number:   eNum,
			})
		}
	}

	// 2. Legacy regex matching
	if len(eps) == 0 {
		for _, m := range anizmEpHrefRe.FindAllStringSubmatch(htmlStr, -1) {
			epURL := m[1]
			eNum, _ := strconv.Atoi(m[3])
			if eNum <= 0 {
				eNum = 1
			}
			if seen[epURL] {
				continue
			}
			seen[epURL] = true
			eps = append(eps, Episode{
				ID:       epURL,
				StreamID: epURL,
				Title:    fmt.Sprintf("Bölüm %d", eNum),
				Season:   1,
				Number:   eNum,
			})
		}
	}

	// 3. Check v3-anime-detail-action (e.g. for movies / standalone OVAs)
	if len(eps) == 0 {
		if m := regexp.MustCompile(`(?is)<a[^>]+class="[^"]*v3-anime-detail-action[^"]*"[^>]+href="(https?://anizm\.net/[^"]+)"`).FindStringSubmatch(htmlStr); len(m) > 1 {
			eps = append(eps, Episode{
				ID:       m[1],
				StreamID: m[1],
				Title:    "Bölüm 1",
				Season:   1,
				Number:   1,
			})
		}
	}

	if len(eps) == 0 {
		epFallback := fmt.Sprintf("%s/%s-1-bolum-izle", base, slug)
		eps = append(eps, Episode{
			ID:       epFallback,
			StreamID: epFallback,
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
	epURL := strings.TrimPrefix(streamID, "anizm:")
	base := p.baseURL()
	if !strings.HasPrefix(epURL, "http") {
		epURL = fmt.Sprintf("%s/%s", base, strings.TrimLeft(epURL, "/"))
	}

	// 1. Fetch Episode HTML, retain CSRF token and Set-Cookie session
	req, err := http.NewRequestWithContext(ctx, "GET", epURL, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/122.0.0.0 Safari/537.36")
	req.Header.Set("Referer", base+"/")

	client := &http.Client{Timeout: DefaultTimeout}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	bodyBytes, err := io.ReadAll(io.LimitReader(resp.Body, 1024*1024))
	if err != nil {
		return nil, err
	}
	htmlStr := string(bodyBytes)

	// Build cookie header string from cookies returned by server
	var cookies []string
	for _, c := range resp.Cookies() {
		cookies = append(cookies, fmt.Sprintf("%s=%s", c.Name, c.Value))
	}
	cookieStr := strings.Join(cookies, "; ")

	// Extract CSRF Token
	csrfToken := ""
	if cm := anizmCsrfRe.FindStringSubmatch(htmlStr); len(cm) > 1 {
		csrfToken = cm[1]
	}

	// Extract data-sources URL
	sourcesURL := ""
	if sm := anizmDataSourcesRe.FindStringSubmatch(htmlStr); len(sm) > 1 {
		sourcesURL = sm[1]
	}
	if sourcesURL == "" {
		if sm := regexp.MustCompile(`(?i)data-sources="([^"]+)"`).FindStringSubmatch(htmlStr); len(sm) > 1 {
			sourcesURL = sm[1]
		}
	}
	if sourcesURL == "" {
		return nil, fmt.Errorf("anizm: data-sources not found on '%s'", epURL)
	}

	// 2. Fetch translator videos list
	hdrs := p.ajaxHeaders(epURL, csrfToken)
	if cookieStr != "" {
		hdrs["Cookie"] = cookieStr
	}

	var trResp struct {
		Success bool `json:"success"`
		Videos  []struct {
			ID   int    `json:"id"`
			Name string `json:"name"`
		} `json:"videos"`
	}
	if err := FetchJSON(ctx, sourcesURL, hdrs, &trResp); err != nil || len(trResp.Videos) == 0 {
		return nil, fmt.Errorf("anizm: failed to fetch translator videos from '%s': %v", sourcesURL, err)
	}

	var sources []StreamSource

	// 3. Iterate candidate videos (prioritize AnizmPlayer / Aincrad, Odnoklassniki, Sibnet)
	reCeveirmen := regexp.MustCompile(`/cevirmen/\d+$`)
	for _, v := range trResp.Videos {
		// Try direct player URL or video endpoint
		playerURL := fmt.Sprintf("%s/player/%d", base, v.ID)
		playerHdrs := map[string]string{
			"User-Agent": hdrs["User-Agent"],
			"Referer":    epURL,
		}
		if cookieStr != "" {
			playerHdrs["Cookie"] = cookieStr
		}

		playerHTML, _, err := FetchString(ctx, playerURL, playerHdrs)
		if err != nil || strings.Contains(playerHTML, "Just a moment...") || strings.Contains(playerHTML, "cf-mitigated") {
			// Fallback: try fetching video AJAX endpoint
			videoURL := reCeveirmen.ReplaceAllString(sourcesURL, fmt.Sprintf("/video/%d", v.ID))
			var vResp struct {
				Success bool   `json:"success"`
				URL     string `json:"url"`
				Source  string `json:"source"`
			}
			if err := FetchJSON(ctx, videoURL, hdrs, &vResp); err == nil && vResp.Success && vResp.URL != "" {
				playerURL = vResp.URL
				if strings.HasPrefix(playerURL, "//") {
					playerURL = "https:" + playerURL
				}
				playerHTML, _, _ = FetchString(ctx, playerURL, playerHdrs)
			}
		}

		if playerHTML == "" || strings.Contains(playerHTML, "Just a moment...") || strings.Contains(playerHTML, "cf-mitigated") {
			continue
		}

		// Check for packed JS: FirePlayer("...")
		unpacked := UnpackJS(playerHTML)
		combined := playerHTML + "\n" + unpacked

		if fm := anizmFirePlayerRe.FindStringSubmatch(combined); len(fm) > 1 {
			hash := fm[1]
			if hash == "" && len(fm) > 2 {
				hash = fm[2]
			}
			postURL := fmt.Sprintf("%s/player/index.php?data=%s&do=getVideo", p.playerAPI(), hash)
			postBody := fmt.Sprintf("hash=%s&r=%s", url.QueryEscape(hash), url.QueryEscape(base+"/"))

			var apiResp struct {
				SecuredLink string `json:"securedLink"`
				HLS         bool   `json:"hls"`
			}
			postHdrs := map[string]string{
				"User-Agent":       hdrs["User-Agent"],
				"Content-Type":     "application/x-www-form-urlencoded; charset=UTF-8",
				"X-Requested-With": "XMLHttpRequest",
				"Referer":          p.playerAPI() + "/",
				"Origin":           p.playerAPI(),
			}

			if err := FetchJSONWithBody(ctx, "POST", postURL, postHdrs, []byte(postBody), &apiResp); err == nil && apiResp.SecuredLink != "" {
				sources = append(sources, StreamSource{
					Name:    "Anizm",
					Title:   fmt.Sprintf("Anizm (%s) • 1080p HLS", v.Name),
					URL:     apiResp.SecuredLink,
					Quality: "1080p",
					Format:  "hls",
					IsHLS:   true,
					Headers: map[string]string{
						"User-Agent": hdrs["User-Agent"],
						"Referer":    p.playerAPI() + "/",
					},
				})
			}
		}

		// Fallback check for OkRu or Sibnet iframes
		if strings.Contains(strings.ToLower(v.Name), "odnoklassniki") || strings.Contains(combined, "ok.ru") {
			for _, m := range regexp.MustCompile(`(?i)(?:src|href)=["'](https?://[^"']*ok\.ru/videoembed/[^"']+)["']`).FindAllStringSubmatch(combined, -1) {
				sources = append(sources, ExtractOkRu(ctx, m[1])...)
			}
		}
		if strings.Contains(strings.ToLower(v.Name), "sibnet") || strings.Contains(combined, "sibnet.ru") {
			for _, m := range regexp.MustCompile(`(?i)(?:src|href)=["'](https?://[^"']*video\.sibnet\.ru/[^"']+)["']`).FindAllStringSubmatch(combined, -1) {
				sources = append(sources, ExtractSibnet(ctx, m[1], base+"/")...)
			}
		}

		if len(sources) > 0 {
			break
		}
	}

	if len(sources) == 0 {
		return nil, fmt.Errorf("anizm: no streams found for '%s'", epURL)
	}

	return SelectBestStream(sources)
}
