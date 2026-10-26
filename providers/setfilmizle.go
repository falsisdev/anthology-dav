package providers

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"regexp"
	"slices"
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
	re := regexp.MustCompile(`(?is)<a[^>]+class="[^"]*card-link[^"]*"[^>]+href="([^"]+)"[^>]*>([\s\S]*?)</a>`)
	reH := regexp.MustCompile(`(?is)<h[234][^>]*>([^<]+)</h[234]>`)
	reAlt := regexp.MustCompile(`(?is)alt="([^"]+)"`)

	for _, m := range re.FindAllStringSubmatch(html, 60) {
		href := ResolveRelativeURL(m[1], p.baseURL())
		inner := m[2]
		if seen[href] {
			continue
		}
		rawT := ""
		if tm := reH.FindStringSubmatch(inner); len(tm) > 1 {
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

	reNonce := regexp.MustCompile(`(?i)(?:video\s*:\s*|data-nonce=)["']([a-zA-Z0-9]+)["']`)
	var nonces []string
	for _, nm := range reNonce.FindAllStringSubmatch(body, -1) {
		if !slices.Contains(nonces, nm[1]) {
			nonces = append(nonces, nm[1])
		}
	}

	rePostID := regexp.MustCompile(`data-post-id=["'](\d+)["']`)
	var postIDs []string
	for _, pm := range rePostID.FindAllStringSubmatch(body, -1) {
		if !slices.Contains(postIDs, pm[1]) {
			postIDs = append(postIDs, pm[1])
		}
	}

	type playerBtn struct {
		name string
		key  string
	}
	reBtn := regexp.MustCompile(`(?is)data-player-name=["']([^"'$]+)["'](?:[^>]*data-part-key=["']([^"']*)["'])?`)
	var buttons []playerBtn
	for _, bm := range reBtn.FindAllStringSubmatch(body, -1) {
		key := ""
		if len(bm) > 2 {
			key = bm[2]
		}
		buttons = append(buttons, playerBtn{name: bm[1], key: key})
	}
	if len(buttons) == 0 {
		buttons = append(buttons, playerBtn{name: "SetPlay", key: ""})
	}

	reSPG := regexp.MustCompile(`SPG\.cerceve\(\s*(?:["'][^"']*["']\s*,\s*)?["']([^"']+)["']\s*,\s*["']([^"']+)["']\s*\)`)

	seenBtnTypes := make(map[AudioType]bool)
	for _, postID := range postIDs {
		if len(sources) >= 4 {
			break
		}
		for _, btn := range buttons {
			btnAType := DetectAudioType(btn.name)
			if seenBtnTypes[btnAType] && btnAType != AudioTypeUnknown {
				continue
			}

			dubTag := "1080p"
			if btnAType == AudioTypeDub {
				dubTag = "[TR Dublaj] • 1080p"
			} else if btnAType == AudioTypeSub || btnAType == AudioTypeOriginal {
				dubTag = "[TR Altyazı] • 1080p"
			}

			for _, nonce := range nonces {
				form := url.Values{}
				form.Set("action", "get_video_url")
				form.Set("nonce", nonce)
				form.Set("post_id", postID)
				form.Set("player_name", btn.name)
				form.Set("part_key", btn.key)

				ajaxURL := p.baseURL() + "/wp-admin/admin-ajax.php"
				respStr, _, err := PostForm(ctx, ajaxURL, form, map[string]string{
					"Referer":          u,
					"X-Requested-With": "XMLHttpRequest",
				})
				if err != nil {
					continue
				}
				var ajaxRes struct {
					Success bool `json:"success"`
					Data    struct {
						URL    string `json:"url"`
						Stream struct {
							URL string `json:"url"`
						} `json:"stream"`
					} `json:"data"`
				}
				if json.Unmarshal([]byte(respStr), &ajaxRes) == nil && (ajaxRes.Data.Stream.URL != "" || ajaxRes.Data.URL != "") {
					embedURL := ajaxRes.Data.Stream.URL
					if embedURL == "" {
						embedURL = ajaxRes.Data.URL
					}
					embedURL = strings.ReplaceAll(embedURL, `\/`, `/`)
					embedHTML, _, err := FetchString(ctx, embedURL, DefaultHeaders(u))
					if err == nil {
						targetURL := embedURL
						if sm := reSPG.FindStringSubmatch(embedHTML); len(sm) > 2 {
							if dec := SPGDecode(sm[1], sm[2]); strings.HasPrefix(dec, "http") {
								targetURL = dec
							}
						}
						manifestURL, fpSubs, finalRef, fpHeaders := ResolveFastPlay(ctx, targetURL, embedURL)
						if manifestURL != "" {
							if fpHeaders == nil {
								fpHeaders = map[string]string{"Referer": finalRef}
							}
							curAType := btnAType
							if curAType == AudioTypeUnknown && len(fpSubs) > 0 {
								curAType = AudioTypeSub
							}
							sources = append(sources, StreamSource{
								Name:      "SetFilmİzle " + btn.name,
								Title:     fmt.Sprintf("SetFilmİzle • %s • %s", btn.name, dubTag),
								URL:       manifestURL,
								Quality:   "1080p",
								Format:    "hls",
								IsHLS:     true,
								AudioType: curAType,
								Headers:   fpHeaders,
								Subtitles: fpSubs,
							})
							if curAType != AudioTypeUnknown {
								seenBtnTypes[curAType] = true
							}
							break // Found working stream for this player
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
