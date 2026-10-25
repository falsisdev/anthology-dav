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

type WebteIzleProvider struct{}

func init() {
	Register(&WebteIzleProvider{})
}

func (p *WebteIzleProvider) ID() string   { return "webteizle" }
func (p *WebteIzleProvider) Name() string { return "Webteİzle" }

func (p *WebteIzleProvider) baseURL() string {
	return config.RemoteString("urls.movies.webteizle", "https://webteizle.info")
}

func (p *WebteIzleProvider) GetCatalog(ctx context.Context, page int) ([]MediaItem, error) {
	u := p.baseURL()
	if page > 1 {
		u = fmt.Sprintf("%s/filmler/%d", p.baseURL(), page)
	}
	body, _, err := FetchString(ctx, u, DefaultHeaders(p.baseURL()+"/"))
	if err != nil {
		return nil, err
	}
	return p.parseCards(body), nil
}

func (p *WebteIzleProvider) Search(ctx context.Context, query string) ([]MediaItem, error) {
	form := url.Values{}
	form.Set("q", query)
	respStr, _, err := PostForm(ctx, p.baseURL()+"/ajax/arama.asp", form, map[string]string{
		"Referer":          p.baseURL() + "/",
		"X-Requested-With": "XMLHttpRequest",
		"User-Agent":       config.DefaultUserAgent,
	})
	if err != nil {
		return nil, err
	}

	var searchRes struct {
		Status  string `json:"status"`
		Results struct {
			Filmler struct {
				Results []struct {
					Title string `json:"title"`
					URL   string `json:"url"`
					Image string `json:"image"`
				} `json:"results"`
			} `json:"filmler"`
			Diziler struct {
				Results []struct {
					Title string `json:"title"`
					URL   string `json:"url"`
					Image string `json:"image"`
				} `json:"results"`
			} `json:"diziler"`
		} `json:"results"`
	}

	var items []MediaItem
	if json.Unmarshal([]byte(respStr), &searchRes) == nil && searchRes.Status == "success" {
		for _, f := range searchRes.Results.Filmler.Results {
			href := ResolveRelativeURL(f.URL, p.baseURL())
			items = append(items, MediaItem{
				ID:        href,
				Title:     CleanMediaTitle(f.Title),
				Year:      ExtractYear(f.Title),
				Type:      MediaTypeMovie,
				PosterURL: ResolveRelativeURL(f.Image, p.baseURL()),
			})
		}
		for _, d := range searchRes.Results.Diziler.Results {
			href := ResolveRelativeURL(d.URL, p.baseURL())
			items = append(items, MediaItem{
				ID:        href,
				Title:     CleanMediaTitle(d.Title),
				Year:      ExtractYear(d.Title),
				Type:      MediaTypeSeries,
				PosterURL: ResolveRelativeURL(d.Image, p.baseURL()),
			})
		}
		if len(items) > 0 {
			return items, nil
		}
	}
	return p.parseCards(respStr), nil
}

func (p *WebteIzleProvider) parseCards(html string) []MediaItem {
	var items []MediaItem
	seen := make(map[string]bool)
	re := regexp.MustCompile(`(?is)<a[^>]+href="([^"]*(?:/izle/|/film/|/hakkinda/)[^"]+)"[^>]*>.*?(?:<div[^>]*class="[^"]*film-ismi[^"]*"[^>]*>([^<]+)</div>|title="([^"]+)"|<span[^>]*>([^<]+)</span>)`)
	for _, m := range re.FindAllStringSubmatch(html, 60) {
		href := ResolveRelativeURL(m[1], p.baseURL())
		rawT := m[2]
		if rawT == "" {
			rawT = m[3]
		}
		if rawT == "" {
			rawT = m[4]
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

func (p *WebteIzleProvider) GetDetails(ctx context.Context, mediaID string) (*MediaDetail, error) {
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

func (p *WebteIzleProvider) GetEpisodes(ctx context.Context, mediaID string, season int) ([]Episode, error) {
	return nil, nil
}

func (p *WebteIzleProvider) ResolveStream(ctx context.Context, streamID string) (*StreamResult, error) {
	u := ResolveRelativeURL(streamID, p.baseURL())
	body, _, err := FetchString(ctx, u, DefaultHeaders(p.baseURL()+"/"))
	if err != nil {
		return nil, err
	}

	if strings.Contains(u, "/hakkinda/") {
		reIzle := regexp.MustCompile(`(?i)href=["'](/izle/(?:dublaj|altyazi)/[^"']+)["']`)
		if im := reIzle.FindStringSubmatch(body); len(im) > 1 {
			u = ResolveRelativeURL(im[1], p.baseURL())
			body, _, _ = FetchString(ctx, u, DefaultHeaders(p.baseURL()+"/"))
		}
	}

	reID1 := regexp.MustCompile(`data-id="(\d+)"`)
	reID2 := regexp.MustCompile(`filmid\s*[=:]\s*['"]?(\d+)`)
	filmID := ""
	if m := reID1.FindStringSubmatch(body); len(m) > 1 {
		filmID = m[1]
	} else if m := reID2.FindStringSubmatch(body); len(m) > 1 {
		filmID = m[1]
	}

	var sources []StreamSource
	reVidmoly := regexp.MustCompile(`(?i)vidmoly\s*\(\s*['"]([^'"]+)['"]`)
	reOkru := regexp.MustCompile(`(?i)okru\s*\(\s*['"]([^'"]+)['"]`)
	reFilemoon := regexp.MustCompile(`(?i)filemoon\s*\(\s*['"]([^'"]+)['"]`)
	reIframe := regexp.MustCompile(`(?i)<iframe[^>]+src=["']([^"']+)["']`)

	if filmID != "" {
		for _, dil := range []string{"0", "1"} {
			form := url.Values{}
			form.Set("filmid", filmID)
			form.Set("dil", dil)
			form.Set("s", "")
			form.Set("b", "")
			form.Set("bot", "0")

			altStr, _, err := PostForm(ctx, p.baseURL()+"/ajax/dataAlternatif3.asp", form, map[string]string{
				"Referer":          u,
				"X-Requested-With": "XMLHttpRequest",
				"Origin":           p.baseURL(),
			})
			if err != nil {
				continue
			}

			var altObj struct {
				Status string `json:"status"`
				Data   []struct {
					ID     int    `json:"id"`
					Baslik string `json:"baslik"`
				} `json:"data"`
			}
			if json.Unmarshal([]byte(altStr), &altObj) == nil && altObj.Status == "success" {
				for _, alt := range altObj.Data {
					embedForm := url.Values{}
					embedForm.Set("id", fmt.Sprintf("%d", alt.ID))
					embStr, _, err := PostForm(ctx, p.baseURL()+"/ajax/dataEmbed.asp", embedForm, map[string]string{
						"Referer":          u,
						"X-Requested-With": "XMLHttpRequest",
						"Origin":           p.baseURL(),
					})
					if err != nil {
						continue
					}

					embedURL := ""
					if ifm := reIframe.FindStringSubmatch(embStr); len(ifm) > 1 {
						embedURL = ifm[1]
					} else if vm := reVidmoly.FindStringSubmatch(embStr); len(vm) > 1 {
						embedURL = fmt.Sprintf("https://vidmoly.biz/embed-%s.html", vm[1])
					} else if om := reOkru.FindStringSubmatch(embStr); len(om) > 1 {
						embedURL = fmt.Sprintf("https://odnoklassniki.ru/videoembed/%s", om[1])
					} else if fm := reFilemoon.FindStringSubmatch(embStr); len(fm) > 1 {
						embedURL = fmt.Sprintf("https://filemoon.sx/e/%s", fm[1])
					}

					if embedURL != "" {
						if strings.Contains(embedURL, "vidmoly") {
							sources = append(sources, ExtractVidMoly(ctx, embedURL, u)...)
						} else {
							sources = append(sources, ExtractAnyIframe(ctx, embedURL, u)...)
						}
					}
					if len(sources) > 0 {
						break
					}
				}
			}
			if len(sources) > 0 {
				break
			}
		}
	}

	if len(sources) == 0 {
		sources = append(sources, ExtractAnyIframe(ctx, u, p.baseURL()+"/")...)
	}
	return SelectBestStream(sources)
}
