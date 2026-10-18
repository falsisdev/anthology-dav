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

type FilmModuProvider struct{}

func init() {
	Register(&FilmModuProvider{})
}

func (p *FilmModuProvider) ID() string   { return "filmmodu" }
func (p *FilmModuProvider) Name() string { return "FilmModu" }

func (p *FilmModuProvider) baseURL() string {
	return config.RemoteString("urls.movies.filmmodu", "https://www.filmmodu.one")
}

func (p *FilmModuProvider) GetCatalog(ctx context.Context, page int) ([]MediaItem, error) {
	u := p.baseURL()
	if page > 1 {
		u = fmt.Sprintf("%s/?page=%d", p.baseURL(), page)
	}
	body, _, err := FetchString(ctx, u, DefaultHeaders(p.baseURL()+"/"))
	if err != nil {
		return nil, err
	}
	return p.parseCards(body), nil
}

func (p *FilmModuProvider) Search(ctx context.Context, query string) ([]MediaItem, error) {
	u := fmt.Sprintf("%s/film-ara?term=%s", p.baseURL(), url.QueryEscape(query))
	body, _, err := FetchString(ctx, u, DefaultHeaders(p.baseURL()+"/"))
	if err == nil {
		items := p.parseCards(body)
		if len(items) > 0 {
			return items, nil
		}
	}

	liveBase := "https://filmmodu.live"
	liveBody, _, err := FetchString(ctx, fmt.Sprintf("%s/ara?q=%s", liveBase, url.QueryEscape(query)), DefaultHeaders(liveBase+"/"))
	if err != nil {
		return nil, err
	}
	return p.parseLiveCards(liveBody, liveBase), nil
}

func (p *FilmModuProvider) parseCards(html string) []MediaItem {
	var items []MediaItem
	seen := make(map[string]bool)
	re := regexp.MustCompile(`(?is)<div[^>]*class="[^"]*movie[^"]*"[^>]*>.*?<a[^>]+href="([^"]+)"[^>]*>.*?<img[^>]+src="([^"]*)"[^>]*alt="([^"]+)"`)
	for _, m := range re.FindAllStringSubmatch(html, 60) {
		href := ResolveRelativeURL(m[1], p.baseURL())
		title := CleanMediaTitle(m[3])
		if title == "" || seen[href] || strings.Contains(href, "/kategori/") || strings.Contains(href, "/tur/") {
			continue
		}
		seen[href] = true
		items = append(items, MediaItem{
			ID:        href,
			Title:     title,
			Year:      ExtractYear(m[3]),
			Type:      MediaTypeMovie,
			PosterURL: ResolveRelativeURL(m[2], p.baseURL()),
		})
	}
	if len(items) == 0 {
		reAlt := regexp.MustCompile(`(?is)<a[^>]+href="(https?://[^"/]*filmmodu[^"/]*/[^"/]+)"[^>]*title="([^"]+)"`)
		for _, m := range reAlt.FindAllStringSubmatch(html, 50) {
			href := m[1]
			title := CleanMediaTitle(m[2])
			if title == "" || seen[href] || strings.Contains(href, "/film-ara") {
				continue
			}
			seen[href] = true
			items = append(items, MediaItem{
				ID:    href,
				Title: title,
				Year:  ExtractYear(m[2]),
				Type:  MediaTypeMovie,
			})
		}
	}
	return items
}

func (p *FilmModuProvider) parseLiveCards(html, base string) []MediaItem {
	var items []MediaItem
	seen := make(map[string]bool)
	re := regexp.MustCompile(`(?is)<a[^>]+href="([^"]*/film/[^"]+)"[^>]*>.*?(?:<h[234][^>]*>([^<]+)</h[234]>|title="([^"]+)")`)
	for _, m := range re.FindAllStringSubmatch(html, 40) {
		href := ResolveRelativeURL(m[1], base)
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

func (p *FilmModuProvider) GetDetails(ctx context.Context, mediaID string) (*MediaDetail, error) {
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

func (p *FilmModuProvider) GetEpisodes(ctx context.Context, mediaID string, season int) ([]Episode, error) {
	return nil, nil
}

func (p *FilmModuProvider) ResolveStream(ctx context.Context, streamID string) (*StreamResult, error) {
	u := ResolveRelativeURL(streamID, p.baseURL())
	body, _, err := FetchString(ctx, u, DefaultHeaders(p.baseURL()+"/"))
	if err != nil {
		return nil, err
	}

	var sources []StreamSource
	var subs []Subtitle

	reAlt := regexp.MustCompile(`(?is)<div[^>]*class="[^"]*alternates[^"]*"[^>]*>(.*?)</div>`)
	var altURLs []string
	if m := reAlt.FindStringSubmatch(body); len(m) > 1 {
		reLink := regexp.MustCompile(`href="([^"]+)"`)
		for _, lm := range reLink.FindAllStringSubmatch(m[1], 10) {
			link := ResolveRelativeURL(lm[1], p.baseURL())
			if !strings.Contains(strings.ToLower(link), "fragman") {
				altURLs = append(altURLs, link)
			}
		}
	}
	if len(altURLs) == 0 {
		altURLs = append(altURLs, u)
	}

	reVidID := regexp.MustCompile(`var\s+videoId\s*=\s*['"]([^'"]+)['"]`)
	reVidType := regexp.MustCompile(`var\s+videoType\s*=\s*['"]([^'"]+)['"]`)

	for _, altURL := range altURLs {
		altHTML := body
		if altURL != u {
			altHTML, _, _ = FetchString(ctx, altURL, DefaultHeaders(u))
		}
		var videoID, videoType string
		if m := reVidID.FindStringSubmatch(altHTML); len(m) > 1 {
			videoID = m[1]
		}
		if m := reVidType.FindStringSubmatch(altHTML); len(m) > 1 {
			videoType = m[1]
		}
		if videoID != "" && videoType != "" {
			srcEndpoint := fmt.Sprintf("%s/get-source?movie_id=%s&type=%s", p.baseURL(), url.QueryEscape(videoID), url.QueryEscape(videoType))
			respStr, _, err := FetchString(ctx, srcEndpoint, map[string]string{
				"Referer":          altURL,
				"X-Requested-With": "XMLHttpRequest",
			})
			if err == nil {
				var parsed struct {
					Subtitle string `json:"subtitle"`
					Sources  []struct {
						Src   string `json:"src"`
						Label string `json:"label"`
					} `json:"sources"`
				}
				if json.Unmarshal([]byte(respStr), &parsed) == nil {
					if parsed.Subtitle != "" {
						subs = append(subs, Subtitle{
							ID:       "filmmodu_tr",
							Language: "tr",
							Label:    "Türkçe",
							URL:      ResolveRelativeURL(parsed.Subtitle, p.baseURL()),
							Format:   "vtt",
						})
					}
					for _, s := range parsed.Sources {
						if s.Src != "" {
							sources = append(sources, StreamSource{
								Name:      "FilmModu",
								Title:     "FilmModu • " + s.Label,
								URL:       ResolveRelativeURL(s.Src, p.baseURL()),
								Quality:   s.Label,
								Format:    "hls",
								IsHLS:     true,
								Headers:   map[string]string{"Referer": p.baseURL() + "/"},
								Subtitles: subs,
							})
						}
					}
				}
			}
		}
	}

	if len(sources) == 0 {
		rePV := regexp.MustCompile(`data-pv="([^"]+)"`)
		if m := rePV.FindStringSubmatch(body); len(m) > 1 {
			pvURL := fmt.Sprintf("https://play2.pilavyerplay.top/assets/js/s.php?s=%s", url.QueryEscape(m[1]))
			pvBody, _, err := FetchString(ctx, pvURL, DefaultHeaders("https://filmmodu.live/"))
			if err == nil {
				rePlayer := regexp.MustCompile(`window\.__PLAYER__\s*=\s*(\{.*?\});`)
				if pm := rePlayer.FindStringSubmatch(pvBody); len(pm) > 1 {
					var playerObj struct {
						Stream string `json:"stream"`
						Subs   []struct {
							File  string `json:"file"`
							Label string `json:"label"`
						} `json:"subs"`
					}
					if json.Unmarshal([]byte(pm[1]), &playerObj) == nil && playerObj.Stream != "" {
						for i, sb := range playerObj.Subs {
							if sb.File != "" {
								subs = append(subs, Subtitle{
									ID:       fmt.Sprintf("fm_sub_%d", i),
									Language: "tr",
									Label:    sb.Label,
									URL:      sb.File,
									Format:   "vtt",
								})
							}
						}
						sources = append(sources, StreamSource{
							Name:      "FilmModu Live",
							Title:     "FilmModu Live • 1080p",
							URL:       playerObj.Stream,
							Quality:   "1080p",
							Format:    "hls",
							IsHLS:     true,
							Headers:   map[string]string{"Referer": "https://play2.pilavyerplay.top/"},
							Subtitles: subs,
						})
					}
				}
			}
		}
	}

	return SelectBestStream(sources)
}
