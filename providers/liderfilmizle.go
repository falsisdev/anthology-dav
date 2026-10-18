package providers

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/url"
	"regexp"
	"strings"

	"anthology-dav/config"
)

type LiderFilmIzleProvider struct{}

func init() {
	Register(&LiderFilmIzleProvider{})
}

func (p *LiderFilmIzleProvider) ID() string   { return "liderfilmizle" }
func (p *LiderFilmIzleProvider) Name() string { return "LiderFilmİzle" }

func (p *LiderFilmIzleProvider) baseURL() string {
	return config.RemoteString("urls.movies.liderfilmizle", "https://liderfilmizle.vip")
}

func (p *LiderFilmIzleProvider) GetCatalog(ctx context.Context, page int) ([]MediaItem, error) {
	if page < 1 {
		page = 1
	}
	u := fmt.Sprintf("%s/kesfet/filmler/?sayfa=%d", p.baseURL(), page)
	body, _, err := FetchString(ctx, u, DefaultHeaders(p.baseURL()+"/"))
	if err != nil {
		return nil, err
	}
	return p.parseCards(body), nil
}

func (p *LiderFilmIzleProvider) Search(ctx context.Context, query string) ([]MediaItem, error) {
	apiURL := fmt.Sprintf("%s/api/search.php?q=%s", p.baseURL(), url.QueryEscape(query))
	body, _, err := FetchString(ctx, apiURL, map[string]string{
		"Referer":          p.baseURL() + "/",
		"X-Requested-With": "XMLHttpRequest",
	})
	if err == nil {
		var res []struct {
			Title  string `json:"title"`
			URL    string `json:"url"`
			Slug   string `json:"slug"`
			Type   string `json:"type"`
			Poster string `json:"poster"`
		}
		if json.Unmarshal([]byte(body), &res) == nil && len(res) > 0 {
			var items []MediaItem
			for _, r := range res {
				href := r.URL
				if href == "" && r.Slug != "" {
					href = p.baseURL() + "/" + strings.TrimPrefix(r.Slug, "/")
				}
				href = ResolveRelativeURL(href, p.baseURL())
				mType := MediaTypeMovie
				if strings.Contains(r.Type, "dizi") || strings.Contains(href, "/dizi/") {
					mType = MediaTypeSeries
				}
				items = append(items, MediaItem{
					ID:        href,
					Title:     CleanMediaTitle(r.Title),
					Year:      ExtractYear(r.Title),
					Type:      mType,
					PosterURL: r.Poster,
				})
			}
			return items, nil
		}
	}

	u := fmt.Sprintf("%s/?s=%s", p.baseURL(), url.QueryEscape(query))
	htmlBody, _, err := FetchString(ctx, u, DefaultHeaders(p.baseURL()+"/"))
	if err != nil {
		return nil, err
	}
	return p.parseCards(htmlBody), nil
}

func (p *LiderFilmIzleProvider) parseCards(html string) []MediaItem {
	var items []MediaItem
	seen := make(map[string]bool)
	re := regexp.MustCompile(`(?is)<a[^>]+href="(https?://[^"]*liderfilmizle[^"]+/[^"]+)"[^>]*>.*?(?:<h[234][^>]*>([^<]+)</h[234]>|title="([^"]+)")`)
	for _, m := range re.FindAllStringSubmatch(html, 60) {
		href := m[1]
		rawT := m[2]
		if rawT == "" {
			rawT = m[3]
		}
		title := CleanMediaTitle(rawT)
		if title == "" || seen[href] || strings.Contains(href, "/kesfet/") || strings.Contains(href, "/kategori/") {
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

func (p *LiderFilmIzleProvider) GetDetails(ctx context.Context, mediaID string) (*MediaDetail, error) {
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

	reEps := regexp.MustCompile(`(?is)<a[^>]+href="([^"]*/sezon-\d+/bolum-\d+[^"]*)"`)
	epMatches := reEps.FindAllStringSubmatch(body, -1)
	if len(epMatches) > 0 {
		seasonMap := make(map[int][]Episode)
		seenEp := make(map[string]bool)
		reSE := regexp.MustCompile(`sezon-(\d+)/bolum-(\d+)`)
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

func (p *LiderFilmIzleProvider) GetEpisodes(ctx context.Context, mediaID string, season int) ([]Episode, error) {
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

func (p *LiderFilmIzleProvider) ResolveStream(ctx context.Context, streamID string) (*StreamResult, error) {
	u := ResolveRelativeURL(streamID, p.baseURL())
	body, _, err := FetchString(ctx, u, DefaultHeaders(p.baseURL()+"/"))
	if err != nil {
		return nil, err
	}

	var sources []StreamSource

	reVS := regexp.MustCompile(`window\._vs\s*=\s*["']([A-Za-z0-9+/=]+)["']`)
	reFile := regexp.MustCompile(`file\s*:\s*["']([^"']+)["']`)
	reDL := regexp.MustCompile(`fetch\(['"](/dl\?[^'"]+)['"]`)

	if vm := reVS.FindStringSubmatch(body); len(vm) > 1 {
		if dec, err := base64.StdEncoding.DecodeString(vm[1]); err == nil {
			var vsList []struct {
				ID  string `json:"id"`
				URL string `json:"url"`
			}
			if json.Unmarshal(dec, &vsList) == nil {
				for _, item := range vsList {
					embedURL := ResolveRelativeURL(item.URL, p.baseURL())
					pBody, _, err := FetchString(ctx, embedURL, DefaultHeaders(u))
					if err != nil {
						continue
					}
					subs := ExtractVTTSubtitles(pBody)
					if fm := reFile.FindStringSubmatch(pBody); len(fm) > 1 {
						sources = append(sources, StreamSource{
							Name:      "LiderFilm VOD",
							Title:     "LiderFilm VOD • 1080p",
							URL:       ResolveRelativeURL(fm[1], embedURL),
							Quality:   "1080p",
							Format:    "hls",
							IsHLS:     true,
							Headers:   map[string]string{"Referer": embedURL},
							Subtitles: subs,
						})
					}
					if dm := reDL.FindStringSubmatch(pBody); len(dm) > 1 {
						dlURL := ResolveRelativeURL(dm[1], embedURL)
						dlResp, _, err := FetchString(ctx, dlURL, DefaultHeaders(embedURL))
						if err == nil {
							var dlObj struct {
								URL string `json:"url"`
							}
							if json.Unmarshal([]byte(dlResp), &dlObj) == nil && dlObj.URL != "" {
								sources = append(sources, StreamSource{
									Name:      "LiderFilm Embed",
									Title:     "LiderFilm Embed • 1080p",
									URL:       dlObj.URL,
									Quality:   "1080p",
									Format:    "hls",
									IsHLS:     true,
									Headers:   map[string]string{"Referer": embedURL},
									Subtitles: subs,
								})
							}
						}
					}
				}
			}
		}
	}

	if len(sources) == 0 {
		sources = append(sources, ExtractAnyIframe(ctx, u, p.baseURL()+"/")...)
	}
	return SelectBestStream(sources)
}
