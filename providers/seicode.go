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
	return config.RemoteString("urls.anime.seicode.api_base", "https://next.seicode.net")
}

func (p *SeicodeProvider) headers() map[string]string {
	return map[string]string{
		"User-Agent": "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/122.0.0.0 Safari/537.36",
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
			ID       string `json:"id"`
			Slug     string `json:"slug"`
			English  string `json:"english"`
			Name     string `json:"name"`
			Title    string `json:"title"`
			Pictures struct {
				Avatar string `json:"avatar"`
				Banner string `json:"banner"`
			} `json:"pictures"`
			Summary string `json:"summary"`
		} `json:"animes"`
	}
	if err := FetchJSON(ctx, u, p.headers(), &data); err != nil {
		return nil, err
	}
	var items []MediaItem
	for _, a := range data.Animes {
		slug := a.Slug
		if slug == "" {
			slug = a.ID
		}
		if slug == "" {
			continue
		}
		title := a.English
		if title == "" {
			title = a.Name
		}
		if title == "" {
			title = a.Title
		}
		if title == "" {
			title = slug
		}
		poster := a.Pictures.Avatar
		if poster == "" {
			poster = a.Pictures.Banner
		}
		items = append(items, MediaItem{
			ID:          slug,
			Title:       CleanHTMLText(title),
			Type:        MediaTypeAnime,
			PosterURL:   poster,
			Description: a.Summary,
		})
	}
	return items, nil
}

func (p *SeicodeProvider) Search(ctx context.Context, query string) ([]MediaItem, error) {
	u := fmt.Sprintf("%s/anime/search?q=%s", p.apiBase(), url.QueryEscape(strings.TrimSpace(query)))
	var results []struct {
		ID       string `json:"id"`
		Slug     string `json:"slug"`
		English  string `json:"english"`
		Name     string `json:"name"`
		Title    string `json:"title"`
		Pictures struct {
			Avatar string `json:"avatar"`
			Banner string `json:"banner"`
		} `json:"pictures"`
		Summary string `json:"summary"`
	}
	if err := FetchJSON(ctx, u, p.headers(), &results); err != nil {
		return nil, err
	}
	var items []MediaItem
	for _, a := range results {
		slug := a.Slug
		if slug == "" {
			slug = a.ID
		}
		if slug == "" {
			continue
		}
		title := a.English
		if title == "" {
			title = a.Name
		}
		if title == "" {
			title = a.Title
		}
		if title == "" {
			title = slug
		}
		poster := a.Pictures.Avatar
		if poster == "" {
			poster = a.Pictures.Banner
		}
		items = append(items, MediaItem{
			ID:          slug,
			Title:       CleanHTMLText(title),
			Type:        MediaTypeAnime,
			PosterURL:   poster,
			Description: a.Summary,
		})
	}
	return items, nil
}

func (p *SeicodeProvider) GetDetails(ctx context.Context, mediaID string) (*MediaDetail, error) {
	slug := strings.TrimPrefix(mediaID, "seicode:")
	u := fmt.Sprintf("%s/anime/%s", p.apiBase(), url.PathEscape(slug))
	var data struct {
		English  string `json:"english"`
		Name     string `json:"name"`
		Title    string `json:"title"`
		Summary  string `json:"summary"`
		Pictures struct {
			Avatar string `json:"avatar"`
			Banner string `json:"banner"`
		} `json:"pictures"`
		Seasons []struct {
			SeasonNumber int `json:"season_number"`
			Episodes     []struct {
				EpisodeNumber int    `json:"episode_number"`
				Title         string `json:"title"`
				Name          string `json:"name"`
			} `json:"episodes"`
		} `json:"seasons"`
	}
	if err := FetchJSON(ctx, u, p.headers(), &data); err != nil {
		return nil, err
	}
	title := data.English
	if title == "" {
		title = data.Name
	}
	if title == "" {
		title = data.Title
	}
	if title == "" {
		title = slug
	}

	poster := data.Pictures.Avatar
	if poster == "" {
		poster = data.Pictures.Banner
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
				epTitle = ep.Name
			}
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
			Title:       CleanHTMLText(title),
			Type:        MediaTypeAnime,
			PosterURL:   poster,
			Description: data.Summary,
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
	if len(det.Seasons) > 0 {
		return det.Seasons[0].Episodes, nil
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
				EpisodeNumber int               `json:"episode_number"`
				VideoLinks    map[string]string `json:"video_links"`
			} `json:"episodes"`
		} `json:"seasons"`
	}
	if err := FetchJSON(ctx, u, p.headers(), &data); err != nil {
		return nil, err
	}

	var sources []StreamSource
	ref := "https://seicode.net/"

	for _, s := range data.Seasons {
		sNum := s.SeasonNumber
		if sNum <= 0 {
			sNum = 1
		}
		if sNum != season && len(data.Seasons) > 1 {
			continue
		}
		for _, ep := range s.Episodes {
			if ep.EpisodeNumber == episode || (len(s.Episodes) == 1 && episode == 1) {
				for hostKey, link := range ep.VideoLinks {
					link = strings.TrimSpace(link)
					if link == "" {
						continue
					}
					if strings.HasPrefix(link, "//") {
						link = "https:" + link
					}
					lowerKey := strings.ToLower(hostKey)
					lowerLink := strings.ToLower(link)

					switch {
					case strings.Contains(lowerKey, "tau-video") || strings.Contains(lowerLink, "tau-video.xyz"):
						sources = append(sources, ExtractTauVideo(ctx, link)...)
					case strings.Contains(lowerKey, "okru") || strings.Contains(lowerLink, "ok.ru"):
						sources = append(sources, ExtractOkRu(ctx, link)...)
					case strings.Contains(lowerKey, "sibnet") || strings.Contains(lowerLink, "sibnet.ru"):
						sources = append(sources, ExtractSibnet(ctx, link, ref)...)
					case strings.Contains(lowerKey, "vidmoly") || strings.Contains(lowerLink, "vidmoly"):
						sources = append(sources, ExtractVidMoly(ctx, link, ref)...)
					case strings.Contains(lowerKey, "mp4upload") || strings.Contains(lowerLink, "mp4upload"):
						sources = append(sources, ExtractMP4Upload(ctx, link)...)
					default:
						sources = append(sources, ExtractAnyIframe(ctx, link, ref)...)
					}
				}
			}
		}
	}

	if len(sources) == 0 {
		return nil, fmt.Errorf("seicode: no stream sources found for '%s' S%02dE%02d", slug, season, episode)
	}

	return SelectBestStream(sources)
}
