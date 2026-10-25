package providers

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"regexp"
	"strings"

	"anthology-dav/config"
)

type KultFilmlerProvider struct{}

func init() {
	Register(&KultFilmlerProvider{})
}

func (p *KultFilmlerProvider) ID() string   { return "kultfilmler" }
func (p *KultFilmlerProvider) Name() string { return "KültFilmler" }

func (p *KultFilmlerProvider) baseURL() string {
	return config.RemoteString("urls.movies.kultfilmler", "https://kultfilmler.net")
}

func (p *KultFilmlerProvider) GetCatalog(ctx context.Context, page int) ([]MediaItem, error) {
	u := fmt.Sprintf("%s/category/aksiyon-filmleri-izle/", p.baseURL())
	if page > 1 {
		u = fmt.Sprintf("%s/category/aksiyon-filmleri-izle/page/%d/", p.baseURL(), page)
	}
	body, _, err := FetchString(ctx, u, DefaultHeaders(p.baseURL()+"/"))
	if err != nil {
		return nil, err
	}
	return p.parseCards(body), nil
}

func (p *KultFilmlerProvider) Search(ctx context.Context, query string) ([]MediaItem, error) {
	u := fmt.Sprintf("%s/?s=%s", p.baseURL(), url.QueryEscape(query))
	body, _, err := FetchString(ctx, u, DefaultHeaders(p.baseURL()+"/"))
	if err != nil {
		return nil, err
	}
	return p.parseCards(body), nil
}

func (p *KultFilmlerProvider) parseCards(html string) []MediaItem {
	var items []MediaItem
	seen := make(map[string]bool)
	re := regexp.MustCompile(`(?is)<a[^>]+class="[^"]*\bmcard\b[^"]*"[^>]+href="([^"]+)"[^>]*>([\s\S]*?)</a>`)
	reH3 := regexp.MustCompile(`(?is)<h3[^>]*>([^<]+)</h3>`)
	reAlt := regexp.MustCompile(`(?is)alt="([^"]+)"`)

	for _, m := range re.FindAllStringSubmatch(html, 60) {
		href := ResolveRelativeURL(m[1], p.baseURL())
		inner := m[2]
		if seen[href] {
			continue
		}
		rawT := ""
		if tm := reH3.FindStringSubmatch(inner); len(tm) > 1 {
			rawT = tm[1]
		} else if am := reAlt.FindStringSubmatch(inner); len(am) > 1 {
			rawT = am[1]
		}
		title := CleanMediaTitle(rawT)
		if title == "" {
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

func (p *KultFilmlerProvider) GetDetails(ctx context.Context, mediaID string) (*MediaDetail, error) {
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

	reEps := regexp.MustCompile(`(?is)<a[^>]+class="[^"]*\bep\b[^"]*"[^>]+href="([^"]+)"`)
	epMatches := reEps.FindAllStringSubmatch(body, -1)
	if len(epMatches) > 0 {
		var eps []Episode
		for idx, m := range epMatches {
			epURL := ResolveRelativeURL(m[1], p.baseURL())
			eps = append(eps, Episode{
				ID:       epURL,
				StreamID: epURL,
				Title:    fmt.Sprintf("%d. Bölüm", idx+1),
				Season:   1,
				Number:   idx + 1,
			})
		}
		return &MediaDetail{
			MediaItem: MediaItem{ID: u, Title: title, Year: ExtractYear(rawTitle), Type: MediaTypeSeries},
			Seasons:   []Season{{Number: 1, Name: "Season 01", Episodes: eps}},
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

func (p *KultFilmlerProvider) GetEpisodes(ctx context.Context, mediaID string, season int) ([]Episode, error) {
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

func (p *KultFilmlerProvider) ResolveStream(ctx context.Context, streamID string) (*StreamResult, error) {
	u := ResolveRelativeURL(streamID, p.baseURL())
	body, _, err := FetchString(ctx, u, DefaultHeaders(p.baseURL()+"/"))
	if err != nil {
		return nil, err
	}
	var sources []StreamSource
	var iframeURLs []string

	reIframe := regexp.MustCompile(`(?i)<iframe[^>]+src=["']([^"']+)["']`)
	for _, m := range reIframe.FindAllStringSubmatch(body, -1) {
		iframeURLs = append(iframeURLs, strings.ReplaceAll(m[1], `\/`, `/`))
	}
	reSrcData := regexp.MustCompile(`(?i)<script[^>]+id="kf-srcdata"[^>]*>([\s\S]*?)</script>`)
	if sm := reSrcData.FindStringSubmatch(body); len(sm) > 1 {
		reSrc := regexp.MustCompile(`src=\\?["']([^"'\\]+)`)
		for _, m := range reSrc.FindAllStringSubmatch(sm[1], -1) {
			raw := strings.ReplaceAll(m[1], `\/`, `/`)
			if strings.HasPrefix(raw, "//") {
				raw = "https:" + raw
			}
			if strings.HasPrefix(raw, "http") {
				iframeURLs = append(iframeURLs, raw)
			}
		}
	}

	for _, ifr := range iframeURLs {
		if strings.Contains(ifr, "youtube.com") || strings.Contains(ifr, "youtu.be") {
			continue
		}
		if strings.Contains(ifr, "vidmoly") {
			sources = append(sources, ExtractVidMoly(ctx, ifr, u)...)
		} else if strings.Contains(ifr, "vidpapi.xyz") || strings.Contains(ifr, "vidpapi.com") {
			parts := strings.Split(strings.Trim(ifr, "/"), "/")
			if len(parts) > 0 {
				videoID := parts[len(parts)-1]
				_, _, headers, _, err := FetchBytes(ctx, http.MethodGet, ifr, map[string]string{
					"User-Agent": config.DefaultUserAgent,
					"Referer":    u,
				}, nil)
				cookie := ""
				if err == nil && headers != nil {
					if sc := headers.Get("Set-Cookie"); sc != "" {
						for _, c := range strings.Split(sc, ";") {
							if strings.Contains(c, "fireplayer_player=") {
								cookie = strings.TrimSpace(c)
								break
							}
						}
					}
				}
				form := url.Values{
					"data": {videoID},
					"do":   {"getVideo"},
				}
				apiURL := fmt.Sprintf("https://vidpapi.xyz/player/index.php?data=%s&do=getVideo", url.QueryEscape(videoID))
				reqHdrs := map[string]string{
					"User-Agent":       config.DefaultUserAgent,
					"Referer":          ifr,
					"X-Requested-With": "XMLHttpRequest",
					"Content-Type":     "application/x-www-form-urlencoded; charset=UTF-8",
				}
				if cookie != "" {
					reqHdrs["Cookie"] = cookie
				}
				apiResp, status, err := PostForm(ctx, apiURL, form, reqHdrs)
				if err == nil && status == http.StatusOK {
					var vpRes struct {
						SecuredLink string `json:"securedLink"`
					}
					if json.Unmarshal([]byte(apiResp), &vpRes) == nil && vpRes.SecuredLink != "" {
						streamURL := strings.ReplaceAll(vpRes.SecuredLink, `\/`, `/`)
						sources = append(sources, StreamSource{
							Name:    "KültFilmler Vidpapi",
							Title:   "Vidpapi • 1080p",
							URL:     streamURL,
							Quality: "1080p",
							Format:  "hls",
							IsHLS:   true,
							Headers: map[string]string{
								"Referer":    "https://vidpapi.xyz/",
								"User-Agent": config.DefaultUserAgent,
							},
						})
					}
				}
			}
		} else {
			sources = append(sources, ExtractAnyIframe(ctx, ifr, u)...)
		}
		if len(sources) > 0 {
			break
		}
	}

	if len(sources) == 0 {
		sources = append(sources, ExtractAnyIframe(ctx, u, p.baseURL()+"/")...)
	}
	return SelectBestStream(sources)
}
