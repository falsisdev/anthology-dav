package providers

import (
	"context"
	"encoding/base64"
	"fmt"
	"net/url"
	"regexp"
	"strings"

	"anthology-dav/config"
)

type HDFilmCehennemiProvider struct{}

func init() {
	Register(&HDFilmCehennemiProvider{})
}

func (p *HDFilmCehennemiProvider) ID() string   { return "hdfilmcehennemi" }
func (p *HDFilmCehennemiProvider) Name() string { return "HDFilmCehennemi" }

func (p *HDFilmCehennemiProvider) baseURL() string {
	return config.RemoteString("urls.movies.hdfilmcehennemi", "https://www.hdfilmcehennemi.nl")
}

func (p *HDFilmCehennemiProvider) GetCatalog(ctx context.Context, page int) ([]MediaItem, error) {
	u := p.baseURL()
	if page > 1 {
		u = fmt.Sprintf("%s/imdb-7-puan-uzeri-filmler-2/page/%d/", p.baseURL(), page)
	}
	body, _, err := FetchString(ctx, u, DefaultHeaders(p.baseURL()+"/"))
	if err != nil {
		return nil, err
	}
	return p.parseCards(body), nil
}

func (p *HDFilmCehennemiProvider) Search(ctx context.Context, query string) ([]MediaItem, error) {
	u := fmt.Sprintf("%s/search/?q=%s", p.baseURL(), url.QueryEscape(query))
	body, _, err := FetchString(ctx, u, map[string]string{
		"Referer":          p.baseURL() + "/",
		"X-Requested-With": "XMLHttpRequest",
	})
	if err == nil {
		items := p.parseCards(body)
		if len(items) > 0 {
			return items, nil
		}
	}
	slug := Slugify(query)
	slugURL := fmt.Sprintf("%s/%s/", p.baseURL(), slug)
	if detail, err := p.GetDetails(ctx, slugURL); err == nil && detail != nil {
		return []MediaItem{detail.MediaItem}, nil
	}
	return nil, nil
}

func (p *HDFilmCehennemiProvider) parseCards(html string) []MediaItem {
	var items []MediaItem
	seen := make(map[string]bool)
	re := regexp.MustCompile(`(?is)<a[^>]+href="(https?://[^"]*hdfilmcehennemi[^"]+/[^"/]+/?)"[^>]*>.*?(?:<strong[^>]*>([^<]+)</strong>|<h[234][^>]*>([^<]+)</h[234]>|title="([^"]+)")`)
	for _, m := range re.FindAllStringSubmatch(html, 60) {
		href := m[1]
		rawT := m[2]
		if rawT == "" {
			rawT = m[3]
		}
		if rawT == "" {
			rawT = m[4]
		}
		title := CleanMediaTitle(rawT)
		if title == "" || seen[href] || strings.Contains(href, "/tur/") || strings.Contains(href, "/kategori/") || strings.Contains(href, "/yil/") || strings.Contains(href, "/ulkeler/") {
			continue
		}
		seen[href] = true
		mediaType := MediaTypeMovie
		if strings.Contains(href, "/dizi/") {
			mediaType = MediaTypeSeries
		}
		items = append(items, MediaItem{
			ID:    href,
			Title: title,
			Year:  ExtractYear(rawT),
			Type:  mediaType,
		})
	}
	return items
}

func (p *HDFilmCehennemiProvider) GetDetails(ctx context.Context, mediaID string) (*MediaDetail, error) {
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

	reEps := regexp.MustCompile(`(?is)<a[^>]+href="(https?://[^"]+/\d+-sezon-\d+-bolum[^"]*)"[^>]*>`)
	epMatches := reEps.FindAllStringSubmatch(body, -1)
	if len(epMatches) > 0 {
		seasonMap := make(map[int][]Episode)
		seenEp := make(map[string]bool)
		reSE := regexp.MustCompile(`(\d+)-sezon-(\d+)-bolum`)
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

func (p *HDFilmCehennemiProvider) GetEpisodes(ctx context.Context, mediaID string, season int) ([]Episode, error) {
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

func (p *HDFilmCehennemiProvider) ResolveStream(ctx context.Context, streamID string) (*StreamResult, error) {
	u := ResolveRelativeURL(streamID, p.baseURL())
	body, _, err := FetchString(ctx, u, DefaultHeaders(p.baseURL()+"/"))
	if err != nil {
		return nil, err
	}

	var sources []StreamSource

	reIframe := regexp.MustCompile(`(?i)(?:data-src|src)=["']([^"']*video/embed[^"']+|https?://[^"']*hdfilmcehennemi\.mobi[^"']+)["']`)
	matches := reIframe.FindAllStringSubmatch(body, 10)
	for _, m := range matches {
		embedURL := ResolveRelativeURL(m[1], p.baseURL())
		embedHTML, _, err := FetchString(ctx, embedURL, DefaultHeaders(p.baseURL()+"/"))
		if err != nil {
			continue
		}

		subs := ExtractVTTSubtitles(embedHTML)
		unpacked := UnpackJS(embedHTML)
		combined := embedHTML + "\n" + unpacked
		reB64Parts := regexp.MustCompile(`\[((?:"[A-Za-z0-9+/=]+"(?:\s*,\s*)?)+)\]`)
		for _, arrM := range reB64Parts.FindAllStringSubmatch(combined, 5) {
			reItem := regexp.MustCompile(`"([A-Za-z0-9+/=]+)"`)
			var parts []string
			for _, im := range reItem.FindAllStringSubmatch(arrM[1], -1) {
				parts = append(parts, im[1])
			}
			joined := strings.Join(parts, "")
			if dec, err := base64.StdEncoding.DecodeString(joined); err == nil {
				decStr := string(dec)
				if strings.HasPrefix(decStr, "http") {
					sources = append(sources, StreamSource{
						Name:      "HDFilmCehennemi CloseLoad",
						Title:     "CloseLoad • 1080p",
						URL:       decStr,
						Quality:   "1080p",
						Format:    "hls",
						IsHLS:     true,
						Headers:   map[string]string{"Referer": "https://hdfilmcehennemi.mobi/"},
						Subtitles: subs,
					})
				}
			}
		}

		reM3U8 := regexp.MustCompile(`(?:file_link|file)\s*[=:]\s*["'](https?://[^"']+\.m3u8[^"']*)["']`)
		if mm := reM3U8.FindStringSubmatch(combined); len(mm) > 1 {
			sources = append(sources, StreamSource{
				Name:      "HDFilmCehennemi Direct",
				Title:     "Direct • 1080p",
				URL:       strings.ReplaceAll(mm[1], `\/`, `/`),
				Quality:   "1080p",
				Format:    "hls",
				IsHLS:     true,
				Headers:   map[string]string{"Referer": embedURL},
				Subtitles: subs,
			})
		}
	}

	if len(sources) == 0 {
		sources = append(sources, ExtractAnyIframe(ctx, u, p.baseURL()+"/")...)
	}
	return SelectBestStream(sources)
}
