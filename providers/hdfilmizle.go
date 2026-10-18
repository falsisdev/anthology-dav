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

type HDFilmIzleProvider struct{}

func init() {
	Register(&HDFilmIzleProvider{})
}

func (p *HDFilmIzleProvider) ID() string   { return "hdfilmizle" }
func (p *HDFilmIzleProvider) Name() string { return "HDFilmİzle" }

func (p *HDFilmIzleProvider) baseURL() string {
	return config.RemoteString("urls.movies.hdfilmizle", "https://www.hdfilmizle.vip")
}

func (p *HDFilmIzleProvider) GetCatalog(ctx context.Context, page int) ([]MediaItem, error) {
	u := p.baseURL()
	if page > 1 {
		u = fmt.Sprintf("%s/page/%d/", p.baseURL(), page)
	}
	body, _, err := FetchString(ctx, u, DefaultHeaders(p.baseURL()+"/"))
	if err != nil {
		altBase := "https://www.hdfilmizle.ink"
		body, _, err = FetchString(ctx, altBase, DefaultHeaders(altBase+"/"))
		if err != nil {
			return nil, err
		}
	}
	return p.parseCards(body), nil
}

func (p *HDFilmIzleProvider) Search(ctx context.Context, query string) ([]MediaItem, error) {
	searchURL := p.baseURL() + "/search/"
	data := url.Values{}
	data.Set("query", query)
	respStr, _, err := PostForm(ctx, searchURL, data, map[string]string{
		"Referer":          p.baseURL() + "/",
		"X-Requested-With": "XMLHttpRequest",
	})
	if err == nil {
		var results []struct {
			Title string `json:"title"`
			Slug  string `json:"slug"`
			URL   string `json:"url"`
			Year  string `json:"year"`
			Type  string `json:"type"`
		}
		if json.Unmarshal([]byte(respStr), &results) == nil && len(results) > 0 {
			var items []MediaItem
			for _, r := range results {
				href := r.URL
				if href == "" && r.Slug != "" {
					href = p.baseURL() + "/" + strings.TrimPrefix(r.Slug, "/") + "/"
				}
				href = ResolveRelativeURL(href, p.baseURL())
				mType := MediaTypeMovie
				if strings.Contains(r.Type, "dizi") || strings.Contains(href, "/dizi/") {
					mType = MediaTypeSeries
				}
				yr := r.Year
				if yr == "" {
					yr = ExtractYear(r.Title)
				}
				items = append(items, MediaItem{
					ID:    href,
					Title: CleanMediaTitle(r.Title),
					Year:  yr,
					Type:  mType,
				})
			}
			return items, nil
		}
	}

	u := fmt.Sprintf("%s/?s=%s", p.baseURL(), url.QueryEscape(query))
	body, _, err := FetchString(ctx, u, DefaultHeaders(p.baseURL()+"/"))
	if err != nil {
		return nil, err
	}
	return p.parseCards(body), nil
}

func (p *HDFilmIzleProvider) parseCards(html string) []MediaItem {
	var items []MediaItem
	seen := make(map[string]bool)
	re := regexp.MustCompile(`(?is)<a[^>]+href="(https?://[^"]*hdfilmizle[^"]+/[^"/]+/?)"[^>]*title="([^"]+)"`)
	for _, m := range re.FindAllStringSubmatch(html, 60) {
		href := m[1]
		title := CleanMediaTitle(m[2])
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
			Year:  ExtractYear(m[2]),
			Type:  mType,
		})
	}
	return items
}

func (p *HDFilmIzleProvider) GetDetails(ctx context.Context, mediaID string) (*MediaDetail, error) {
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

	reEps := regexp.MustCompile(`(?is)<a[^>]+href="(https?://[^"]+/sezon-\d+/bolum-\d+[^"]*)"[^>]*>`)
	epMatches := reEps.FindAllStringSubmatch(body, -1)
	if len(epMatches) > 0 {
		seasonMap := make(map[int][]Episode)
		seenEp := make(map[string]bool)
		reSE := regexp.MustCompile(`sezon-(\d+)/bolum-(\d+)`)
		for _, m := range epMatches {
			epURL := m[1]
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

func (p *HDFilmIzleProvider) GetEpisodes(ctx context.Context, mediaID string, season int) ([]Episode, error) {
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

func (p *HDFilmIzleProvider) ResolveStream(ctx context.Context, streamID string) (*StreamResult, error) {
	u := ResolveRelativeURL(streamID, p.baseURL())
	body, _, err := FetchString(ctx, u, DefaultHeaders(p.baseURL()+"/"))
	if err != nil {
		return nil, err
	}

	var sources []StreamSource

	var iframeURLs []string
	reIframe := regexp.MustCompile(`(?i)(?:src|data-src)=["'](https?://[^"']+(?:vidrame|vidmoxy|setplay|fastplay)[^"']*)["']`)
	for _, m := range reIframe.FindAllStringSubmatch(body, 10) {
		iframeURLs = append(iframeURLs, strings.ReplaceAll(m[1], `\/`, `/`))
	}

	reVidrameArr := regexp.MustCompile(`var\s+d\s*=\s*\[([\d,\s]+)\].*?var\s+k\s*=\s*["']([^"']+)["']`)
	reEE := regexp.MustCompile(`ee\s*\(\s*["']([^"']+)["']\s*\)`)
	reSPG := regexp.MustCompile(`SPG\.cerceve\(\s*["']([^"']+)["']\s*,\s*["']([^"']+)["']\s*\)`)

	for _, iframeURL := range iframeURLs {
		embedHTML, _, err := FetchString(ctx, iframeURL, DefaultHeaders(u))
		if err != nil {
			continue
		}
		subs := ExtractVTTSubtitles(embedHTML)

		if strings.Contains(iframeURL, "vidrame") {
			if vm := reVidrameArr.FindStringSubmatch(embedHTML); len(vm) > 2 {
				var nums []int
				for _, ns := range strings.Split(vm[1], ",") {
					if n, err := strconv.Atoi(strings.TrimSpace(ns)); err == nil {
						nums = append(nums, n)
					}
				}
				if decoded := DecodeVidrameXOR(nums, vm[2]); strings.HasPrefix(decoded, "http") {
					sources = append(sources, StreamSource{
						Name:    "HDFilmİzle Vidrame",
						Title:   "Vidrame • 1080p",
						URL:     decoded,
						Quality: "1080p",
						Format:  "hls",
						IsHLS:   true,
						Headers: map[string]string{
							"Referer":    iframeURL,
							"User-Agent": "ExoPlayerLib/2.19.1",
						},
						Subtitles: subs,
					})
				}
			}
		} else if strings.Contains(iframeURL, "vidmoxy") {
			if em := reEE.FindStringSubmatch(embedHTML); len(em) > 1 {
				if decoded := DecodeVidmoxy(em[1]); strings.HasPrefix(decoded, "http") {
					sources = append(sources, StreamSource{
						Name:    "HDFilmİzle Vidmoxy",
						Title:   "Vidmoxy • 1080p",
						URL:     decoded,
						Quality: "1080p",
						Format:  "hls",
						IsHLS:   true,
						Headers: map[string]string{
							"Referer":    iframeURL,
							"User-Agent": "ExoPlayerLib/2.19.1",
						},
						Subtitles: subs,
					})
				}
			}
		} else if strings.Contains(iframeURL, "setplay") || strings.Contains(iframeURL, "fastplay") {
			targetURL := iframeURL
			if sm := reSPG.FindStringSubmatch(embedHTML); len(sm) > 2 {
				if dec := SPGDecode(sm[1], sm[2]); strings.HasPrefix(dec, "http") {
					targetURL = dec
				}
			}
			manifestURL, fpSubs, finalRef := ResolveFastPlay(ctx, targetURL, iframeURL)
			if manifestURL != "" {
				sources = append(sources, StreamSource{
					Name:    "HDFilmİzle FastPlay",
					Title:   "FastPlay • 1080p",
					URL:     manifestURL,
					Quality: "1080p",
					Format:  "hls",
					IsHLS:   true,
					Headers: map[string]string{
						"Referer":    finalRef,
						"User-Agent": "ExoPlayerLib/2.19.1",
					},
					Subtitles: append(subs, fpSubs...),
				})
			}
		}
	}

	if len(sources) == 0 {
		sources = append(sources, ExtractAnyIframe(ctx, u, p.baseURL()+"/")...)
	}
	return SelectBestStream(sources)
}
