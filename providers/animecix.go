package providers

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"

	"anthology-dav/config"
)

type AnimecixProvider struct{}

func init() {
	Register(&AnimecixProvider{})
}

func (p *AnimecixProvider) ID() string   { return "animecix" }
func (p *AnimecixProvider) Name() string { return "AnimeciX" }

func (p *AnimecixProvider) baseURL() string {
	return config.RemoteString("urls.anime.animecix.base", "https://animecix.tv")
}

func (p *AnimecixProvider) headers() map[string]string {
	base := p.baseURL()
	return map[string]string{
		"Accept":          "application/json, text/plain, */*",
		"User-Agent":      config.DefaultUserAgent,
		"x-e-h":           "7Y2ozlO+QysR5w9Q6Tupmtvl9jJp7ThFH8SB+Lo7NvZjgjqRSqOgcT2v4ISM9sP10LmnlYI8WQ==.xrlyOBFS5BHjQ2Lk",
		"Referer":         base + "/",
		"Origin":          base,
		"Accept-Language": "tr-TR,tr;q=0.9,en-US;q=0.8,en;q=0.7",
	}
}

func (p *AnimecixProvider) GetCatalog(ctx context.Context, page int) ([]MediaItem, error) {
	if page < 1 {
		page = 1
	}
	u := fmt.Sprintf("%s/secure/titles?limit=24&page=%d", p.baseURL(), page)
	var data struct {
		Pagination struct {
			Data []struct {
				ID          int    `json:"id"`
				Name        string `json:"name"`
				Title       string `json:"title"`
				Poster      string `json:"poster"`
				Description string `json:"description"`
				Year        int    `json:"year"`
				ReleaseDate string `json:"release_date"`
				IsSeries    bool   `json:"is_series"`
			} `json:"data"`
		} `json:"pagination"`
	}
	if err := FetchJSON(ctx, u, p.headers(), &data); err != nil {
		return nil, err
	}
	var items []MediaItem
	for _, it := range data.Pagination.Data {
		title := it.Name
		if title == "" {
			title = it.Title
		}
		if title == "" {
			continue
		}
		year := ""
		if it.Year > 0 {
			year = strconv.Itoa(it.Year)
		} else {
			year = ExtractYear(it.ReleaseDate)
		}
		mType := MediaTypeAnime
		if !it.IsSeries {
			mType = MediaTypeMovie
		}
		items = append(items, MediaItem{
			ID:          strconv.Itoa(it.ID),
			Title:       title,
			Year:        year,
			Type:        mType,
			PosterURL:   it.Poster,
			Description: it.Description,
		})
	}
	return items, nil
}

func (p *AnimecixProvider) Search(ctx context.Context, query string) ([]MediaItem, error) {
	u := fmt.Sprintf("%s/secure/search/%s?limit=20", p.baseURL(), url.PathEscape(strings.TrimSpace(query)))
	var data struct {
		Results []struct {
			ID          int    `json:"id"`
			Name        string `json:"name"`
			Title       string `json:"title"`
			Poster      string `json:"poster"`
			Description string `json:"description"`
			Year        int    `json:"year"`
			ReleaseDate string `json:"release_date"`
			IsSeries    bool   `json:"is_series"`
			Type        string `json:"type"`
		} `json:"results"`
	}
	if err := FetchJSON(ctx, u, p.headers(), &data); err != nil {
		return nil, err
	}
	var items []MediaItem
	for _, it := range data.Results {
		title := it.Name
		if title == "" {
			title = it.Title
		}
		if title == "" {
			continue
		}
		year := ""
		if it.Year > 0 {
			year = strconv.Itoa(it.Year)
		} else {
			year = ExtractYear(it.ReleaseDate)
		}
		mType := MediaTypeAnime
		if !it.IsSeries && strings.EqualFold(it.Type, "movie") {
			mType = MediaTypeMovie
		}
		items = append(items, MediaItem{
			ID:          strconv.Itoa(it.ID),
			Title:       title,
			Year:        year,
			Type:        mType,
			PosterURL:   it.Poster,
			Description: it.Description,
		})
	}
	return items, nil
}

func (p *AnimecixProvider) GetDetails(ctx context.Context, mediaID string) (*MediaDetail, error) {
	cleanID := strings.TrimPrefix(mediaID, "animecix:")
	u := fmt.Sprintf("%s/secure/titles/%s?titleId=%s", p.baseURL(), cleanID, cleanID)
	var data struct {
		Title struct {
			ID          int    `json:"id"`
			Name        string `json:"name"`
			Title       string `json:"title"`
			Poster      string `json:"poster"`
			Description string `json:"description"`
			Year        int    `json:"year"`
			ReleaseDate string `json:"release_date"`
			IsSeries    bool   `json:"is_series"`
			Seasons     []struct {
				Number       int `json:"number"`
				EpisodeCount int `json:"episode_count"`
			} `json:"seasons"`
			Videos []struct {
				Name       string `json:"name"`
				SeasonNum  int    `json:"season_num"`
				EpisodeNum int    `json:"episode_num"`
				URL        string `json:"url"`
			} `json:"videos"`
		} `json:"title"`
	}
	if err := FetchJSON(ctx, u, p.headers(), &data); err != nil {
		return nil, err
	}
	t := data.Title
	title := t.Name
	if title == "" {
		title = t.Title
	}
	year := ""
	if t.Year > 0 {
		year = strconv.Itoa(t.Year)
	} else {
		year = ExtractYear(t.ReleaseDate)
	}

	if !t.IsSeries && len(t.Seasons) == 0 {
		return &MediaDetail{
			MediaItem: MediaItem{
				ID:          cleanID,
				Title:       title,
				Year:        year,
				Type:        MediaTypeMovie,
				PosterURL:   t.Poster,
				Description: t.Description,
			},
			StreamID: fmt.Sprintf("%s:1:1", cleanID),
		}, nil
	}

	seasonMap := make(map[int][]Episode)
	for _, s := range t.Seasons {
		sNum := s.Number
		if sNum <= 0 {
			sNum = 1
		}
		cnt := s.EpisodeCount
		if cnt <= 0 {
			cnt = 12
		}
		for ep := 1; ep <= cnt; ep++ {
			seasonMap[sNum] = append(seasonMap[sNum], Episode{
				ID:       fmt.Sprintf("%s:%d:%d", cleanID, sNum, ep),
				StreamID: fmt.Sprintf("%s:%d:%d", cleanID, sNum, ep),
				Title:    fmt.Sprintf("%d. Sezon %d. Bölüm", sNum, ep),
				Season:   sNum,
				Number:   ep,
			})
		}
	}

	if len(seasonMap) == 0 {
		seenEp := make(map[string]bool)
		for _, v := range t.Videos {
			if v.EpisodeNum <= 0 || v.SeasonNum <= 0 {
				continue
			}
			sNum := v.SeasonNum
			eNum := v.EpisodeNum
			key := fmt.Sprintf("%d_%d", sNum, eNum)
			if seenEp[key] {
				continue
			}
			seenEp[key] = true
			epTitle := v.Name
			if epTitle == "" {
				epTitle = fmt.Sprintf("Bölüm %d", eNum)
			}
			seasonMap[sNum] = append(seasonMap[sNum], Episode{
				ID:       fmt.Sprintf("%s:%d:%d", cleanID, sNum, eNum),
				StreamID: fmt.Sprintf("%s:%d:%d", cleanID, sNum, eNum),
				Title:    epTitle,
				Season:   sNum,
				Number:   eNum,
			})
		}
	}

	if len(seasonMap) == 0 {
		seasonMap[1] = []Episode{
			{
				ID:       fmt.Sprintf("%s:1:1", cleanID),
				StreamID: fmt.Sprintf("%s:1:1", cleanID),
				Title:    "Bölüm 1",
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
			Year:        year,
			Type:        MediaTypeAnime,
			PosterURL:   t.Poster,
			Description: t.Description,
		},
		Seasons: seasons,
	}, nil
}

func (p *AnimecixProvider) GetEpisodes(ctx context.Context, mediaID string, season int) ([]Episode, error) {
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

var tauEmbedExtractRe = regexp.MustCompile(`https?://tau-video\.xyz/embed/[^"'\s<>]+`)

func (p *AnimecixProvider) ResolveStream(ctx context.Context, streamID string) (*StreamResult, error) {
	parts := strings.Split(strings.TrimPrefix(streamID, "animecix:"), ":")
	titleID := parts[0]
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

	bestURL := fmt.Sprintf("%s/secure/best-video?titleId=%s&episode=%d&season=%d", p.baseURL(), titleID, episode, season)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, bestURL, nil)
	if err != nil {
		return nil, err
	}
	for k, v := range p.headers() {
		req.Header.Set(k, v)
	}

	var embedURL string
	if resp, err := noRedirectHTTPClient.Do(req); err == nil {
		defer resp.Body.Close()
		if loc := resp.Header.Get("Location"); strings.Contains(loc, "tau-video") {
			embedURL = loc
		} else if body, err := io.ReadAll(resp.Body); err == nil {
			if m := tauEmbedExtractRe.FindString(string(body)); m != "" {
				embedURL = m
			}
		}
	}

	var sources []StreamSource
	if embedURL != "" {
		sources = append(sources, ExtractTauVideo(ctx, embedURL)...)
	}

	// Fallback: check title details videos array
	if len(sources) == 0 {
		u := fmt.Sprintf("%s/secure/titles/%s?titleId=%s", p.baseURL(), titleID, titleID)
		var data struct {
			Title struct {
				Videos []struct {
					SeasonNum  int    `json:"season_num"`
					EpisodeNum int    `json:"episode_num"`
					URL        string `json:"url"`
				} `json:"videos"`
			} `json:"title"`
		}
		if err := FetchJSON(ctx, u, p.headers(), &data); err == nil {
			for _, v := range data.Title.Videos {
				sNum := v.SeasonNum
				if sNum <= 0 {
					sNum = 1
				}
				eNum := v.EpisodeNum
				if eNum <= 0 {
					eNum = 1
				}
				if sNum == season && eNum == episode && v.URL != "" {
					sources = append(sources, ExtractAnyIframe(ctx, v.URL, p.baseURL()+"/")...)
				}
			}
		}
	}

	return SelectBestStream(sources)
}
