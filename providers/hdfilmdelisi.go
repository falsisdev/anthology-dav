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

type HDFilmDelisiProvider struct{}

func init() {
	Register(&HDFilmDelisiProvider{})
}

func (p *HDFilmDelisiProvider) ID() string   { return "hdfilmdelisi" }
func (p *HDFilmDelisiProvider) Name() string { return "HDFilmDelisi" }

func (p *HDFilmDelisiProvider) baseURL() string {
	return config.RemoteString("urls.movies.hdfilmdelisi.base", config.RemoteString("urls.movies.hdfilmdelisi", "https://hdfilmdelisi.one"))
}

func (p *HDFilmDelisiProvider) GetCatalog(ctx context.Context, page int) ([]MediaItem, error) {
	if page < 1 {
		page = 1
	}
	apiURL := fmt.Sprintf("%s/api/films?page=%d&sort=newest&limit=20", p.baseURL(), page)
	body, _, err := FetchString(ctx, apiURL, DefaultHeaders(p.baseURL()+"/"))
	if err == nil {
		items := p.parseJSONFilms(body)
		if len(items) > 0 {
			return items, nil
		}
	}
	htmlBody, _, err := FetchString(ctx, p.baseURL()+"/", DefaultHeaders(p.baseURL()+"/"))
	if err != nil {
		return nil, err
	}
	return p.parseHTMLFilms(htmlBody), nil
}

func (p *HDFilmDelisiProvider) Search(ctx context.Context, query string) ([]MediaItem, error) {
	apiURL := fmt.Sprintf("%s/api/search?q=%s", p.baseURL(), url.QueryEscape(query))
	body, _, err := FetchString(ctx, apiURL, DefaultHeaders(p.baseURL()+"/"))
	if err == nil {
		items := p.parseJSONFilms(body)
		if len(items) > 0 {
			return items, nil
		}
	}
	htmlBody, _, err := FetchString(ctx, fmt.Sprintf("%s/?s=%s", p.baseURL(), url.QueryEscape(query)), DefaultHeaders(p.baseURL()+"/"))
	if err != nil {
		return nil, err
	}
	return p.parseHTMLFilms(htmlBody), nil
}

func (p *HDFilmDelisiProvider) parseJSONFilms(jsonStr string) []MediaItem {
	var items []MediaItem
	var wrapper struct {
		Films []struct {
			Slug           string `json:"slug"`
			Baslik         string `json:"baslik"`
			OrijinalBaslik string `json:"orijinalBaslik"`
			Title          string `json:"title"`
			YayinYili      int    `json:"yayinYili"`
			Year           int    `json:"year"`
			Afis           string `json:"afis"`
			Poster         string `json:"poster"`
			IMDbID         string `json:"imdbId"`
		} `json:"films"`
		Results []struct {
			Slug           string `json:"slug"`
			Baslik         string `json:"baslik"`
			OrijinalBaslik string `json:"orijinalBaslik"`
			Title          string `json:"title"`
			YayinYili      int    `json:"yayinYili"`
			Year           int    `json:"year"`
			Afis           string `json:"afis"`
			Poster         string `json:"poster"`
			IMDbID         string `json:"imdbId"`
		} `json:"results"`
	}
	if err := json.Unmarshal([]byte(jsonStr), &wrapper); err == nil {
		list := wrapper.Films
		if len(list) == 0 {
			list = wrapper.Results
		}
		for _, f := range list {
			if f.Slug == "" {
				continue
			}
			rawT := f.Baslik
			if rawT == "" {
				rawT = f.Title
			}
			if rawT == "" {
				rawT = f.OrijinalBaslik
			}
			yrInt := f.YayinYili
			if yrInt == 0 {
				yrInt = f.Year
			}
			yr := ""
			if yrInt > 1900 {
				yr = strconv.Itoa(yrInt)
			} else {
				yr = ExtractYear(rawT)
			}
			poster := f.Afis
			if poster == "" {
				poster = f.Poster
			}
			items = append(items, MediaItem{
				ID:        f.Slug,
				Title:     CleanMediaTitle(rawT),
				Year:      yr,
				Type:      MediaTypeMovie,
				PosterURL: poster,
			})
		}
	}
	return items
}

func (p *HDFilmDelisiProvider) parseHTMLFilms(html string) []MediaItem {
	var items []MediaItem
	seen := make(map[string]bool)
	re := regexp.MustCompile(`(?is)<a[^>]+href="([^"]*/film/[^"]+)"[^>]*>.*?(?:<h[234][^>]*>([^<]+)</h[234]>|title="([^"]+)")`)
	for _, m := range re.FindAllStringSubmatch(html, 50) {
		href := ResolveRelativeURL(m[1], p.baseURL())
		rawT := m[2]
		if rawT == "" {
			rawT = m[3]
		}
		title := CleanMediaTitle(rawT)
		if title == "" || seen[href] {
			continue
		}
		seen[href] = true
		slug := strings.Trim(strings.TrimPrefix(href, p.baseURL()+"/film/"), "/")
		items = append(items, MediaItem{
			ID:    slug,
			Title: title,
			Year:  ExtractYear(rawT),
			Type:  MediaTypeMovie,
		})
	}
	return items
}

func (p *HDFilmDelisiProvider) GetDetails(ctx context.Context, mediaID string) (*MediaDetail, error) {
	slug := strings.Trim(strings.TrimPrefix(mediaID, "hdfilmdelisi:movie:"), "/")
	slug = strings.Trim(strings.TrimPrefix(slug, p.baseURL()+"/film/"), "/")
	slug = strings.Trim(strings.TrimPrefix(slug, "/film/"), "/")

	apiURL := fmt.Sprintf("%s/api/films/%s", p.baseURL(), slug)
	body, code, err := FetchString(ctx, apiURL, DefaultHeaders(p.baseURL()+"/"))
	if err == nil && code == 200 {
		var filmDet struct {
			Baslik         string `json:"baslik"`
			OrijinalBaslik string `json:"orijinalBaslik"`
			Title          string `json:"title"`
			Slug           string `json:"slug"`
			YayinYili      int    `json:"yayinYili"`
			Afis           string `json:"afis"`
			IMDbID         string `json:"imdbId"`
			VideoURL       string `json:"videoUrl"`
		}
		if err := json.Unmarshal([]byte(body), &filmDet); err == nil && (filmDet.Baslik != "" || filmDet.Title != "") {
			title := filmDet.Baslik
			if title == "" {
				title = filmDet.Title
			}
			yr := ""
			if filmDet.YayinYili > 1900 {
				yr = strconv.Itoa(filmDet.YayinYili)
			}
			return &MediaDetail{
				MediaItem: MediaItem{
					ID:        slug,
					Title:     CleanMediaTitle(title),
					Year:      yr,
					Type:      MediaTypeMovie,
					PosterURL: filmDet.Afis,
				},
				StreamID: slug,
			}, nil
		}
	}

	u := fmt.Sprintf("%s/film/%s", p.baseURL(), slug)
	htmlBody, _, err := FetchString(ctx, u, DefaultHeaders(p.baseURL()+"/"))
	if err != nil {
		return nil, err
	}
	reH1 := regexp.MustCompile(`(?is)<h1[^>]*>(.*?)</h1>`)
	rawTitle := slug
	if m := reH1.FindStringSubmatch(htmlBody); len(m) > 1 {
		rawTitle = m[1]
	}
	title := CleanMediaTitle(rawTitle)
	if title == "" {
		title = slug
	}
	return &MediaDetail{
		MediaItem: MediaItem{
			ID:    slug,
			Title: title,
			Year:  ExtractYear(rawTitle),
			Type:  MediaTypeMovie,
		},
		StreamID: slug,
	}, nil
}

func (p *HDFilmDelisiProvider) GetEpisodes(ctx context.Context, mediaID string, season int) ([]Episode, error) {
	return nil, nil
}

func (p *HDFilmDelisiProvider) ResolveStream(ctx context.Context, streamID string) (*StreamResult, error) {
	slug := strings.Trim(strings.TrimPrefix(streamID, "hdfilmdelisi:movie:"), "/")
	slug = strings.Trim(strings.TrimPrefix(slug, p.baseURL()+"/film/"), "/")
	slug = strings.Trim(strings.TrimPrefix(slug, "/film/"), "/")

	var sources []StreamSource

	// 1. Query JSON API: /api/films/{slug}
	apiURL := fmt.Sprintf("%s/api/films/%s", p.baseURL(), slug)
	if body, code, err := FetchString(ctx, apiURL, DefaultHeaders(p.baseURL()+"/")); err == nil && code == 200 {
		var filmDet struct {
			Baslik   string `json:"baslik"`
			IMDbID   string `json:"imdbId"`
			VideoURL string `json:"videoUrl"`
		}
		if json.Unmarshal([]byte(body), &filmDet) == nil {
			vixHeaders := map[string]string{
				"User-Agent": config.DefaultUserAgent,
				"Referer":    "https://pl.vixolity.com/",
				"Origin":     "https://pl.vixolity.com",
				"Accept":     "*/*",
			}

			// A. If IMDb ID is present, ha.vixolity.com/vs/{imdbId} is the direct 1080p master HLS
			if filmDet.IMDbID != "" && strings.HasPrefix(filmDet.IMDbID, "tt") {
				directURL := fmt.Sprintf("https://ha.vixolity.com/vs/%s", filmDet.IMDbID)
				sources = append(sources, StreamSource{
					Name:    "HDFilmDelisi (Vixolity)",
					Title:   "HDFilmDelisi • 1080p HLS",
					URL:     directURL,
					Quality: "1080p",
					Format:  "hls",
					IsHLS:   true,
					Headers: vixHeaders,
				})
			}

			// B. If videoUrl is provided (e.g., https://pl.vixolity.com/{id}), fetch player and decrypt
			if filmDet.VideoURL != "" {
				pBody, pCode, pErr := FetchString(ctx, filmDet.VideoURL, map[string]string{
					"User-Agent": config.DefaultUserAgent,
					"Referer":    p.baseURL() + "/",
				})
				if pErr == nil && pCode == 200 {
					reDec := regexp.MustCompile(`(?i)(?:decrypt|\))\s*\(\s*["']([0-9a-fA-F]{50,})["']\s*,\s*(\d+)\)`)
					reHex := regexp.MustCompile(`['"]([0-9a-fA-F]{100,})['"],\s*(\d+)\)`)
					reFile := regexp.MustCompile(`file:\s*['"](https?://[^'"]+)['"]`)

					var decBlocks []string
					for _, dm := range reDec.FindAllStringSubmatch(pBody, -1) {
						k, _ := strconv.Atoi(dm[2])
						decBlocks = append(decBlocks, VidmodyDecrypt(dm[1], k))
					}
					for _, dm := range reHex.FindAllStringSubmatch(pBody, -1) {
						k, _ := strconv.Atoi(dm[2])
						decBlocks = append(decBlocks, VidmodyDecrypt(dm[1], k))
					}

					for _, block := range decBlocks {
						if fm := reFile.FindStringSubmatch(block); len(fm) > 1 {
							sources = append(sources, StreamSource{
								Name:    "HDFilmDelisi Player",
								Title:   "HDFilmDelisi • 1080p",
								URL:     fm[1],
								Quality: "1080p",
								Format:  "hls",
								IsHLS:   true,
								Headers: vixHeaders,
							})
							break
						}
					}
				}
			}
		}
	}

	// 2. Legacy fallback: parse HTML page for iframes or embeds
	if len(sources) == 0 {
		pageURL := fmt.Sprintf("%s/film/%s", p.baseURL(), slug)
		body, _, err := FetchString(ctx, pageURL, DefaultHeaders(p.baseURL()+"/"))
		if err == nil {
			sources = append(sources, ExtractAnyIframe(ctx, pageURL, p.baseURL()+"/")...)
			if m3u8 := genericM3U8Re.FindString(body); m3u8 != "" {
				sources = append(sources, StreamSource{
					Name:    "HDFilmDelisi Direct",
					Title:   "HDFilmDelisi • 1080p",
					URL:     strings.ReplaceAll(m3u8, `\/`, `/`),
					Quality: "1080p",
					Format:  "hls",
					IsHLS:   true,
					Headers: map[string]string{"Referer": p.baseURL() + "/"},
				})
			}
		}
	}

	return SelectBestStream(sources)
}
