package providers

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"regexp"

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
	})
	if err != nil {
		return nil, err
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

	reID1 := regexp.MustCompile(`data-id="(\d+)"`)
	reID2 := regexp.MustCompile(`filmid\s*[=:]\s*['"]?(\d+)`)
	filmID := ""
	if m := reID1.FindStringSubmatch(body); len(m) > 1 {
		filmID = m[1]
	} else if m := reID2.FindStringSubmatch(body); len(m) > 1 {
		filmID = m[1]
	}

	var sources []StreamSource
	reVidmoly := regexp.MustCompile(`vidmoly\s*\(\s*['"]([^'"]+)['"]`)
	reOkru := regexp.MustCompile(`okru\s*\(\s*['"]([^'"]+)['"]`)
	reSibnet := regexp.MustCompile(`sibnet\s*\(\s*['"]([^'"]+)['"]`)
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
			})
			if err != nil {
				continue
			}

			var altObj struct {
				Data []struct {
					ID     int    `json:"id"`
					Baslik string `json:"baslik"`
				} `json:"data"`
			}
			if json.Unmarshal([]byte(altStr), &altObj) == nil {
				for _, d := range altObj.Data {
					embedForm := url.Values{}
					embedForm.Set("id", fmt.Sprintf("%d", d.ID))
					embStr, _, err := PostForm(ctx, p.baseURL()+"/ajax/dataEmbed.asp", embedForm, map[string]string{
						"Referer":          u,
						"X-Requested-With": "XMLHttpRequest",
					})
					if err != nil {
						continue
					}
					if m := reVidmoly.FindStringSubmatch(embStr); len(m) > 1 {
						sources = append(sources, ExtractVidMoly(ctx, "https://vidmoly.net/embed-"+m[1]+".html", u)...)
					}
					if m := reOkru.FindStringSubmatch(embStr); len(m) > 1 {
						sources = append(sources, ExtractOkRu(ctx, "https://odnoklassniki.ru/videoembed/"+m[1])...)
					}
					if m := reSibnet.FindStringSubmatch(embStr); len(m) > 1 {
						sources = append(sources, ExtractSibnet(ctx, "https://video.sibnet.ru/shell.php?videoid="+m[1], u)...)
					}
					if m := reIframe.FindStringSubmatch(embStr); len(m) > 1 {
						sources = append(sources, ExtractAnyIframe(ctx, m[1], u)...)
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
