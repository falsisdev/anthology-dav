package providers

import (
	"context"
	"fmt"
	"net/url"
	"strconv"
	"strings"

	"anthology-dav/config"
)

type SeicodeProvider struct{}

func init() {
	Register(&SeicodeProvider{})
}

func (p *SeicodeProvider) ID() string   { return "seicode" }
func (p *SeicodeProvider) Name() string { return "Seicode" }

func (p *SeicodeProvider) apiBase() string {
	return config.RemoteString("urls.anime.seicode.api", "https://next.seicode.net")
}

func (p *SeicodeProvider) headers() map[string]string {
	return map[string]string{
		"User-Agent": config.DefaultUserAgent,
		"Accept":     "application/json",
		"Referer":    "https://seicode.net/",
		"Origin":     "https://seicode.net",
	}
}

func (p *SeicodeProvider) GetCatalog(ctx context.Context, page int) ([]MediaItem, error) {
	if page < 1 {
		page = 1
	}
	u := fmt.Sprintf("%s/anime?page=%d", p.apiBase(), page)
	var data struct {
		Animes []struct {
			Slug        string `json:"slug"`
			Title       string `json:"title"`
			Description string `json:"description"`
			PosterURL   string `json:"poster_url"`
		} `json:"animes"`
	}
	if err := FetchJSON(ctx, u, p.headers(), &data); err != nil {
		return nil, err
	}
	var items []MediaItem
	for _, a := range data.Animes {
		if a.Slug == "" || a.Title == "" {
			continue
		}
		items = append(items, MediaItem{
			ID:          a.Slug,
			Title:       a.Title,
			Type:        MediaTypeAnime,
			PosterURL:   a.PosterURL,
			Description: a.Description,
		})
	}
	return items, nil
}

func (p *SeicodeProvider) Search(ctx context.Context, query string) ([]MediaItem, error) {
	u := fmt.Sprintf("%s/anime/search?q=%s", p.apiBase(), url.QueryEscape(strings.TrimSpace(query)))
	var results []struct {
		Slug        string `json:"slug"`
		Title       string `json:"title"`
		Description string `json:"description"`
		PosterURL   string `json:"poster_url"`
	}
	if err := FetchJSON(ctx, u, p.headers(), &results); err != nil {
		return nil, err
	}
	var items []MediaItem
	for _, a := range results {
		if a.Slug == "" || a.Title == "" {
			continue
		}
		items = append(items, MediaItem{
			ID:          a.Slug,
			Title:       a.Title,
			Type:        MediaTypeAnime,
			PosterURL:   a.PosterURL,
			Description: a.Description,
		})
	}
	return items, nil
}

func (p *SeicodeProvider) GetDetails(ctx context.Context, mediaID string) (*MediaDetail, error) {
	slug := strings.TrimPrefix(mediaID, "seicode:")
	u := fmt.Sprintf("%s/anime/%s", p.apiBase(), url.PathEscape(slug))
	var data struct {
		Title       string `json:"title"`
		Description string `json:"description"`
		PosterURL   string `json:"poster_url"`
		Seasons     []struct {
			SeasonNumber int `json:"season_number"`
			Episodes     []struct {
				EpisodeNumber int    `json:"episode_number"`
				Title         string `json:"title"`
			} `json:"episodes"`
		} `json:"seasons"`
	}
	if err := FetchJSON(ctx, u, p.headers(), &data); err != nil {
		return nil, err
	}
	title := data.Title
	if title == "" {
		title = slug
	}

	var seasons []Season
	for _, s := range data.Seasons {
		sNum := s.SeasonNumber
		if sNum <= 0 {
			sNum = 1
		}
		var eps []Episode
		for _, ep := range s.Episodes {
			eNum := ep.EpisodeNumber
			if eNum <= 0 {
				eNum = 1
			}
			epTitle := ep.Title
			if epTitle == "" {
				epTitle = fmt.Sprintf("Bölüm %d", eNum)
			}
			eps = append(eps, Episode{
				ID:       fmt.Sprintf("%s:%d:%d", slug, sNum, eNum),
				StreamID: fmt.Sprintf("%s:%d:%d", slug, sNum, eNum),
				Title:    epTitle,
				Season:   sNum,
				Number:   eNum,
			})
		}
		if len(eps) > 0 {
			seasons = append(seasons, Season{
				Number:   sNum,
				Name:     fmt.Sprintf("Season %02d", sNum),
				Episodes: eps,
			})
		}
	}
	if len(seasons) == 0 {
		seasons = append(seasons, Season{
			Number: 1,
			Name:   "Season 01",
			Episodes: []Episode{
				{
					ID:       fmt.Sprintf("%s:1:1", slug),
					StreamID: fmt.Sprintf("%s:1:1", slug),
					Title:    "Bölüm 1",
					Season:   1,
					Number:   1,
				},
			},
		})
	}
	return &MediaDetail{
		MediaItem: MediaItem{
			ID:          slug,
			Title:       title,
			Type:        MediaTypeAnime,
			PosterURL:   data.PosterURL,
			Description: data.Description,
		},
		Seasons: seasons,
	}, nil
}

func (p *SeicodeProvider) GetEpisodes(ctx context.Context, mediaID string, season int) ([]Episode, error) {
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

func (p *SeicodeProvider) ResolveStream(ctx context.Context, streamID string) (*StreamResult, error) {
	parts := strings.Split(strings.TrimPrefix(streamID, "seicode:"), ":")
	slug := parts[0]
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

	u := fmt.Sprintf("%s/anime/%s", p.apiBase(), url.PathEscape(slug))
	var data struct {
		Seasons []struct {
			SeasonNumber int `json:"season_number"`
			Episodes     []struct {
				EpisodeNumber int `json:"episode_number"`
				VideoLinks    []struct {
					Label string `json:"label"`
					URL   string `json:"url"`
				} `json:"video_links"`
			} `json:"episodes"`
		} `json:"seasons"`
	}
	if err := FetchJSON(ctx, u, p.headers(), &data); err != nil {
		return nil, err
	}

	var sources []StreamSource
	for _, s := range data.Seasons {
		sNum := s.SeasonNumber
		if sNum <= 0 {
			sNum = 1
		}
		if sNum != season && len(data.Seasons) > 1 {
			continue
		}
		for _, ep := range s.Episodes {
			if ep.EpisodeNumber == episode {
				for _, vl := range ep.VideoLinks {
					if vl.URL == "" {
						continue
					}
					extracted := ExtractAnyIframe(ctx, vl.URL, "https://seicode.net/")
					sources = append(sources, extracted...)
				}
			}
		}
	}
	return SelectBestStream(sources)
}
