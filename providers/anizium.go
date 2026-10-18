package providers

import (
	"context"
	"fmt"
	"net/url"
	"strconv"
	"strings"

	"anthology-dav/config"
)

type AniziumProvider struct{}

func init() {
	Register(&AniziumProvider{})
}

func (p *AniziumProvider) ID() string   { return "anizium" }
func (p *AniziumProvider) Name() string { return "Anizium" }

func (p *AniziumProvider) apiURL() string {
	return config.RemoteString("urls.anime.anizium.api", "https://api.anizium.co")
}

func (p *AniziumProvider) siteURL() string {
	return config.RemoteString("urls.anime.anizium.base", "https://anizium.co")
}

func (p *AniziumProvider) headers() map[string]string {
	site := p.siteURL()
	return map[string]string{
		"User-Agent": config.DefaultUserAgent,
		"Accept":     "application/json, text/plain, */*",
		"Origin":     site,
		"Referer":    site + "/",
		"Cf-Control": BuildAniziumCfControl(),
		"device":     "browser",
		"language":   "tr",
		"site":       "main",
	}
}

func (p *AniziumProvider) GetCatalog(ctx context.Context, page int) ([]MediaItem, error) {
	u := p.apiURL() + "/page/home"
	var data map[string]any
	if err := FetchJSON(ctx, u, p.headers(), &data); err != nil {
		return nil, err
	}

	var items []MediaItem
	seen := make(map[string]bool)
	sections := []string{"trends", "slider", "latest_animes", "recommended"}
	for _, sec := range sections {
		arr, ok := data[sec].([]any)
		if !ok {
			continue
		}
		for _, elem := range arr {
			m, ok := elem.(map[string]any)
			if !ok {
				continue
			}
			idStr := fmt.Sprintf("%v", m["id"])
			if idStr == "" || idStr == "<nil>" || seen[idStr] {
				continue
			}
			title, _ := m["title"].(string)
			if title == "" {
				title, _ = m["title_tr"].(string)
			}
			if title == "" {
				continue
			}
			seen[idStr] = true
			poster, _ := m["poster"].(string)
			if poster != "" && !strings.HasPrefix(poster, "http") {
				poster = "https://x.anizium.co/assets/poster/" + poster
			}
			year := ""
			if y, ok := m["release_year"]; ok && y != nil {
				year = fmt.Sprintf("%v", y)
			}
			desc, _ := m["synopsis_tr"].(string)
			items = append(items, MediaItem{
				ID:          idStr,
				Title:       title,
				Year:        ExtractYear(year),
				Type:        MediaTypeAnime,
				PosterURL:   poster,
				Description: desc,
			})
		}
	}
	return items, nil
}

func (p *AniziumProvider) Search(ctx context.Context, query string) ([]MediaItem, error) {
	u := fmt.Sprintf("%s/page/search?value=%s&page=1", p.apiURL(), url.QueryEscape(strings.TrimSpace(query)))
	var data struct {
		Data []struct {
			ID          any    `json:"id"`
			Title       string `json:"title"`
			TitleTR     string `json:"title_tr"`
			Poster      string `json:"poster"`
			SynopsisTR  string `json:"synopsis_tr"`
			ReleaseYear any    `json:"release_year"`
		} `json:"data"`
	}
	if err := FetchJSON(ctx, u, p.headers(), &data); err != nil {
		return nil, err
	}
	var items []MediaItem
	for _, it := range data.Data {
		idStr := fmt.Sprintf("%v", it.ID)
		title := it.Title
		if title == "" {
			title = it.TitleTR
		}
		if idStr == "" || title == "" {
			continue
		}
		poster := it.Poster
		if poster != "" && !strings.HasPrefix(poster, "http") {
			poster = "https://x.anizium.co/assets/poster/" + poster
		}
		items = append(items, MediaItem{
			ID:          idStr,
			Title:       title,
			Year:        ExtractYear(fmt.Sprintf("%v", it.ReleaseYear)),
			Type:        MediaTypeAnime,
			PosterURL:   poster,
			Description: it.SynopsisTR,
		})
	}
	return items, nil
}

func (p *AniziumProvider) GetDetails(ctx context.Context, mediaID string) (*MediaDetail, error) {
	cleanID := strings.TrimPrefix(mediaID, "anizium:")
	u := fmt.Sprintf("%s/anime/get?id=%s", p.apiURL(), url.QueryEscape(cleanID))
	var data struct {
		Anime struct {
			ID          any    `json:"id"`
			Title       string `json:"title"`
			TitleTR     string `json:"title_tr"`
			Poster      string `json:"poster"`
			SynopsisTR  string `json:"synopsis_tr"`
			ReleaseYear any    `json:"release_year"`
		} `json:"anime"`
		Episodes []struct {
			SeasonNumber  int    `json:"season_number"`
			EpisodeNumber int    `json:"episode_number"`
			Title         string `json:"title"`
		} `json:"episodes"`
	}
	if err := FetchJSON(ctx, u, p.headers(), &data); err != nil {
		return nil, err
	}
	title := data.Anime.Title
	if title == "" {
		title = data.Anime.TitleTR
	}
	if title == "" {
		title = "Anizium #" + cleanID
	}
	poster := data.Anime.Poster
	if poster != "" && !strings.HasPrefix(poster, "http") {
		poster = "https://x.anizium.co/assets/poster/" + poster
	}

	seasonMap := make(map[int][]Episode)
	for _, ep := range data.Episodes {
		sNum := ep.SeasonNumber
		if sNum <= 0 {
			sNum = 1
		}
		eNum := ep.EpisodeNumber
		if eNum <= 0 {
			eNum = 1
		}
		epTitle := ep.Title
		if epTitle == "" {
			epTitle = fmt.Sprintf("%d. Sezon %d. Bölüm", sNum, eNum)
		}
		seasonMap[sNum] = append(seasonMap[sNum], Episode{
			ID:       fmt.Sprintf("%s:%d:%d", cleanID, sNum, eNum),
			StreamID: fmt.Sprintf("%s:%d:%d", cleanID, sNum, eNum),
			Title:    epTitle,
			Season:   sNum,
			Number:   eNum,
		})
	}
	if len(seasonMap) == 0 {
		seasonMap[1] = []Episode{
			{
				ID:       fmt.Sprintf("%s:1:1", cleanID),
				StreamID: fmt.Sprintf("%s:1:1", cleanID),
				Title:    "1. Sezon 1. Bölüm",
				Season:   1,
				Number:   1,
			},
		}
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
		MediaItem: MediaItem{
			ID:          cleanID,
			Title:       title,
			Year:        ExtractYear(fmt.Sprintf("%v", data.Anime.ReleaseYear)),
			Type:        MediaTypeAnime,
			PosterURL:   poster,
			Description: data.Anime.SynopsisTR,
		},
		Seasons: seasons,
	}, nil
}

func (p *AniziumProvider) GetEpisodes(ctx context.Context, mediaID string, season int) ([]Episode, error) {
	det, err := p.GetDetails(ctx, mediaID)
	if err != nil {
		return nil, err
	}
	for _, s := range det.Seasons {
		if s.Number == season {
			return s.Episodes, nil
		}
	}
	return nil, nil
}

func (p *AniziumProvider) ResolveStream(ctx context.Context, streamID string) (*StreamResult, error) {
	parts := strings.Split(strings.TrimPrefix(streamID, "anizium:"), ":")
	animeID := parts[0]
	season := 1
	episode := 1
	if len(parts) >= 3 {
		season, _ = strconv.Atoi(parts[1])
		episode, _ = strconv.Atoi(parts[2])
	}
	if season <= 0 {
		season = 1
	}
	if episode <= 0 {
		episode = 1
	}

	u := fmt.Sprintf("%s/anime/source?id=%s&site=main&server=1&season=%d&episode=%d", p.apiURL(), url.QueryEscape(animeID), season, episode)
	var srcData struct {
		Groups map[string]map[string]struct {
			URL       string `json:"url"`
			Subtitles []struct {
				Language string `json:"language"`
				Label    string `json:"label"`
				File     string `json:"file"`
			} `json:"subtitles"`
		} `json:"groups"`
	}
	if err := FetchJSON(ctx, u, p.headers(), &srcData); err != nil {
		return nil, err
	}

	var sources []StreamSource
	site := p.siteURL()
	streamHeaders := map[string]string{
		"User-Agent": config.DefaultUserAgent,
		"Referer":    site + "/",
		"Origin":     site,
	}

	for groupType, qMap := range srcData.Groups {
		langLabel := "TR Altyazı"
		if groupType == "trdub" {
			langLabel = "TR Dublaj"
		} else if groupType == "endub" {
			langLabel = "EN Dublaj"
		}
		for qKey, qObj := range qMap {
			if qObj.URL == "" {
				continue
			}
			var subs []Subtitle
			for i, sub := range qObj.Subtitles {
				if sub.File == "" {
					continue
				}
				lang := "tr"
				if strings.EqualFold(sub.Language, "en") || strings.EqualFold(sub.Language, "eng") {
					lang = "en"
				}
				subs = append(subs, Subtitle{
					ID:       fmt.Sprintf("anizium_%s_%d", lang, i),
					Language: lang,
					Label:    sub.Label,
					Format:   "vtt",
					URL:      sub.File,
					Headers:  streamHeaders,
				})
			}
			qLabel := qKey + "p"
			if qKey == "2160" {
				qLabel = "2160p"
			}
			sources = append(sources, StreamSource{
				Name:      "Anizium",
				Title:     fmt.Sprintf("Anizium • %s • %s", langLabel, qLabel),
				URL:       qObj.URL,
				Quality:   qLabel,
				Format:    "mp4",
				IsHLS:     false,
				Headers:   streamHeaders,
				Subtitles: subs,
			})
		}
	}
	return SelectBestStream(sources)
}
