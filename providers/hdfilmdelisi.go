package providers

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"regexp"
	"strconv"

	"anthology-dav/config"
)

type HDFilmDelisiProvider struct{}

func init() {
	Register(&HDFilmDelisiProvider{})
}

func (p *HDFilmDelisiProvider) ID() string   { return "hdfilmdelisi" }
func (p *HDFilmDelisiProvider) Name() string { return "HDFilmDelisi" }

func (p *HDFilmDelisiProvider) baseURL() string {
	return config.RemoteString("urls.movies.hdfilmdelisi", "https://hdfilmdelisi.one")
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
	htmlBody, _, err := FetchString(ctx, p.baseURL(), DefaultHeaders(p.baseURL()+"/"))
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
			Slug   string `json:"slug"`
			Title  string `json:"title"`
			Year   int    `json:"year"`
			Poster string `json:"poster"`
		} `json:"films"`
		Results []struct {
			Slug   string `json:"slug"`
			Title  string `json:"title"`
			Year   int    `json:"year"`
			Poster string `json:"poster"`
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
			u := fmt.Sprintf("%s/film/%s", p.baseURL(), f.Slug)
			yr := ExtractYear(f.Title)
			if f.Year > 1900 {
				yr = strconv.Itoa(f.Year)
			}
			items = append(items, MediaItem{
				ID:        u,
				Title:     CleanMediaTitle(f.Title),
				Year:      yr,
				Type:      MediaTypeMovie,
				PosterURL: f.Poster,
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
		items = append(items, MediaItem{
			ID:    href,
			Title: title,
			Year:  ExtractYear(rawT),
			Type:  MediaTypeMovie,
		})
	}
	return items
}

func (p *HDFilmDelisiProvider) GetDetails(ctx context.Context, mediaID string) (*MediaDetail, error) {
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

func (p *HDFilmDelisiProvider) GetEpisodes(ctx context.Context, mediaID string, season int) ([]Episode, error) {
	return nil, nil
}

func (p *HDFilmDelisiProvider) ResolveStream(ctx context.Context, streamID string) (*StreamResult, error) {
	u := ResolveRelativeURL(streamID, p.baseURL())
	body, _, err := FetchString(ctx, u, DefaultHeaders(p.baseURL()+"/"))
	if err != nil {
		return nil, err
	}

	var sources []StreamSource

	reIframe := regexp.MustCompile(`(?i)src=["'](https?://[^"']*vidmody\.com[^"']+)["']`)
	reDec := regexp.MustCompile(`decrypt\(\s*["']([0-9a-fA-F]+)["']\s*,\s*(\d+)\s*\)`)
	reFile := regexp.MustCompile(`file\s*:\s*["'](https?://[^"']+)["']`)

	for _, m := range reIframe.FindAllStringSubmatch(body, 5) {
		iframeURL := m[1]
		pBody, _, err := FetchString(ctx, iframeURL, DefaultHeaders(p.baseURL()+"/"))
		if err != nil {
			continue
		}
		subs := ExtractVTTSubtitles(pBody)
		if dm := reDec.FindStringSubmatch(pBody); len(dm) > 2 {
			key, _ := strconv.Atoi(dm[2])
			decStr := VidmodyDecrypt(dm[1], key)
			if fm := reFile.FindStringSubmatch(decStr); len(fm) > 1 {
				sources = append(sources, StreamSource{
					Name:      "HDFilmDelisi Vidmody",
					Title:     "Vidmody • 1080p",
					URL:       fm[1],
					Quality:   "1080p",
					Format:    "hls",
					IsHLS:     true,
					Headers:   map[string]string{"Referer": "https://vidmody.com/"},
					Subtitles: subs,
				})
			}
		}
	}

	if len(sources) == 0 {
		sources = append(sources, ExtractAnyIframe(ctx, u, p.baseURL()+"/")...)
	}
	return SelectBestStream(sources)
}
