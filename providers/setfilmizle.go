package providers

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"regexp"
	"strings"

	"anthology-dav/config"
)

type SetFilmIzleProvider struct{}

func init() {
	Register(&SetFilmIzleProvider{})
}

func (p *SetFilmIzleProvider) ID() string   { return "setfilmizle" }
func (p *SetFilmIzleProvider) Name() string { return "SetFilmİzle" }

func (p *SetFilmIzleProvider) baseURL() string {
	return config.RemoteString("urls.movies.setfilmizle", "https://www.setfilmizle.ltd")
}

func (p *SetFilmIzleProvider) GetCatalog(ctx context.Context, page int) ([]MediaItem, error) {
	u := fmt.Sprintf("%s/tur/aksiyon/", p.baseURL())
	if page > 1 {
		u = fmt.Sprintf("%s/tur/aksiyon/page/%d/", p.baseURL(), page)
	}
	body, _, err := FetchString(ctx, u, DefaultHeaders(p.baseURL()+"/"))
	if err != nil {
		return nil, err
	}
	return p.parseCards(body), nil
}

func (p *SetFilmIzleProvider) Search(ctx context.Context, query string) ([]MediaItem, error) {
	u := fmt.Sprintf("%s/?s=%s", p.baseURL(), url.QueryEscape(query))
	body, _, err := FetchString(ctx, u, DefaultHeaders(p.baseURL()+"/"))
	if err != nil {
		return nil, err
	}
	return p.parseCards(body), nil
}

func (p *SetFilmIzleProvider) parseCards(html string) []MediaItem {
	var items []MediaItem
	seen := make(map[string]bool)
	re := regexp.MustCompile(`(?is)<a[^>]+href="(https?://[^"]*setfilmizle[^"]+/[^"/]+/?)"[^>]*>.*?(?:<h[234][^>]*>([^<]+)</h[234]>|title="([^"]+)")`)
	for _, m := range re.FindAllStringSubmatch(html, 60) {
		href := m[1]
		rawT := m[2]
		if rawT == "" {
			rawT = m[3]
		}
		title := CleanMediaTitle(rawT)
		if title == "" || seen[href] || strings.Contains(href, "/tur/") || strings.Contains(href, "/kategori/") || strings.Contains(href, "/yil/") {
			continue
		}
		seen[href] = true
		mType := MediaTypeMovie
		if strings.Contains(href, "/dizi/") {
			mType = MediaTypeSeries
		}
		items = append(items, MediaItem{
			ID:    href,
			Title: title,
			Year:  ExtractYear(rawT),
			Type:  mType,
		})
	}
	return items
}

func (p *SetFilmIzleProvider) GetDetails(ctx context.Context, mediaID string) (*MediaDetail, error) {
	u := ResolveRelativeURL(mediaID, p.baseURL())
	body, _, err := FetchString(ctx, u, DefaultHeaders(p.baseURL()+"/"))
	if err != nil {
		return nil, err
	}
	reH1 := regexp.MustCompile(`(?is)<h1[^>]*>(.*?)</h1>`)
	rawTitle := u
	if m := reH1.FindStringSubmatch(body); len(m) > 1 {
		rawTitle = m[1]
	}
	title := CleanMediaTitle(rawTitle)
	if title == "" {
		title = u
	}

	reEps := regexp.MustCompile(`(?is)<a[^>]+class="[^"]*\bfep\b[^"]*"[^>]+href="([^"]+)"`)
	epMatches := reEps.FindAllStringSubmatch(body, -1)
	if len(epMatches) > 0 {
		seasonMap := make(map[int][]Episode)
		seenEp := make(map[string]bool)
		reSE := regexp.MustCompile(`(\d+)-sezon-(\d+)-bolum`)
		for _, m := range epMatches {
			epURL := ResolveRelativeURL(m[1], p.baseURL())
			if seenEp[epURL] {
				continue
			}
			seenEp[epURL] = true
			sNum, eNum := 1, len(seasonMap[1])+1
			if se := reSE.FindStringSubmatch(epURL); len(se) > 2 {
				fmt.Sscanf(se[1], "%d", &sNum)
				fmt.Sscanf(se[2], "%d", &eNum)
			}
			seasonMap[sNum] = append(seasonMap[sNum], Episode{
				ID:       epURL,
				StreamID: epURL,
				Title:    fmt.Sprintf("%d. Sezon %d. Bölüm", sNum, eNum),
				Season:   sNum,
				Number:   eNum,
			})
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
			MediaItem: MediaItem{ID: u, Title: title, Year: ExtractYear(rawTitle), Type: MediaTypeSeries},
			Seasons:   seasons,
		}, nil
	}

	return &MediaDetail{
		MediaItem: MediaItem{
			ID:    u,
			Title: title,
			Year:  ExtractYear(rawTitle),
			Type:  MediaTypeMovie,
		},
		StreamID: u,
	}, nil
}

func (p *SetFilmIzleProvider) GetEpisodes(ctx context.Context, mediaID string, season int) ([]Episode, error) {
	detail, err := p.GetDetails(ctx, mediaID)
	if err != nil {
		return nil, err
	}
	for _, s := range detail.Seasons {
		if s.Number == season {
			return s.Episodes, nil
		}
	}
	return nil, nil
}

func (p *SetFilmIzleProvider) ResolveStream(ctx context.Context, streamID string) (*StreamResult, error) {
	u := ResolveRelativeURL(streamID, p.baseURL())
	body, _, err := FetchString(ctx, u, DefaultHeaders(p.baseURL()+"/"))
	if err != nil {
		return nil, err
	}

	var sources []StreamSource

	reNonce := regexp.MustCompile(`"video"\s*:\s*"([a-zA-Z0-9]+)"`)
	nonce := ""
	if nm := reNonce.FindStringSubmatch(body); len(nm) > 1 {
		nonce = nm[1]
	}
	rePart := regexp.MustCompile(`(?is)data-post-id="(\d+)"[^>]+data-player-name="([^"]+)"[^>]+data-part-key="([^"]*)"`)
	reSPG := regexp.MustCompile(`SPG\.cerceve\(\s*["']([^"']+)["']\s*,\s*["']([^"']+)["']\s*\)`)

	for _, m := range rePart.FindAllStringSubmatch(body, 6) {
		postID, playerName, partKey := m[1], m[2], m[3]
		form := url.Values{}
		form.Set("action", "get_video_url")
		form.Set("nonce", nonce)
		form.Set("post_id", postID)
		form.Set("player_name", playerName)
		form.Set("part_key", partKey)

		ajaxURL := p.baseURL() + "/wp-admin/admin-ajax.php"
		respStr, _, err := PostForm(ctx, ajaxURL, form, map[string]string{
			"Referer":          u,
			"X-Requested-With": "XMLHttpRequest",
		})
		if err != nil {
			continue
		}
		var ajaxRes struct {
			Data struct {
				URL string `json:"url"`
			} `json:"data"`
		}
		if json.Unmarshal([]byte(respStr), &ajaxRes) == nil && ajaxRes.Data.URL != "" {
			embedURL := ajaxRes.Data.URL
			embedHTML, _, err := FetchString(ctx, embedURL, DefaultHeaders(u))
			if err == nil {
				targetURL := embedURL
				if sm := reSPG.FindStringSubmatch(embedHTML); len(sm) > 2 {
					if dec := SPGDecode(sm[1], sm[2]); strings.HasPrefix(dec, "http") {
						targetURL = dec
					}
				}
				manifestURL, fpSubs, finalRef := ResolveFastPlay(ctx, targetURL, embedURL)
				if manifestURL != "" {
					sources = append(sources, StreamSource{
						Name:      "SetFilmİzle FastPlay",
						Title:     "FastPlay • 1080p",
						URL:       manifestURL,
						Quality:   "1080p",
						Format:    "hls",
						IsHLS:     true,
						Headers:   map[string]string{"Referer": finalRef},
						Subtitles: fpSubs,
					})
				}
			}
		}
	}

	if len(sources) == 0 {
		sources = append(sources, ExtractAnyIframe(ctx, u, p.baseURL()+"/")...)
	}
	return SelectBestStream(sources)
}
