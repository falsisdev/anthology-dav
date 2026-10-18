package providers

import (
	"context"
	"fmt"
	"net/url"
	"strconv"
	"strings"

	"anthology-dav/config"
)

type SonAnimeProvider struct{}

func init() {
	Register(&SonAnimeProvider{})
}

func (p *SonAnimeProvider) ID() string   { return "sonanime" }
func (p *SonAnimeProvider) Name() string { return "SonAnime" }

func (p *SonAnimeProvider) apiBase() string {
	return config.RemoteString("urls.anime.sonanime.api", "https://api.sonanime.com")
}

func (p *SonAnimeProvider) headers() map[string]string {
	return map[string]string{
		"User-Agent": config.DefaultUserAgent,
		"Accept":     "application/json",
		"Referer":    "https://sonanime.com/",
		"Origin":     "https://sonanime.com",
	}
}

func (p *SonAnimeProvider) GetCatalog(ctx context.Context, page int) ([]MediaItem, error) {
	u := p.apiBase() + "/api/anime/browse?type=series&sortBy=rating&sortDir=desc"
	var itemsRaw []struct {
		Slug        string `json:"slug"`
		Name        string `json:"anime_name"`
		Image       string `json:"anime_image"`
		Description string `json:"description"`
		ReleaseYear any    `json:"release_year"`
	}
	if err := FetchJSON(ctx, u, p.headers(), &itemsRaw); err != nil {
		return nil, err
	}
	var items []MediaItem
	for i, it := range itemsRaw {
		if i >= 60 || it.Slug == "" || it.Name == "" {
			continue
		}
		items = append(items, MediaItem{
			ID:          it.Slug,
			Title:       it.Name,
			Year:        ExtractYear(fmt.Sprintf("%v", it.ReleaseYear)),
			Type:        MediaTypeAnime,
			PosterURL:   it.Image,
			Description: it.Description,
		})
	}
	return items, nil
}

func (p *SonAnimeProvider) Search(ctx context.Context, query string) ([]MediaItem, error) {
	u := fmt.Sprintf("%s/api/anime/search?q=%s", p.apiBase(), url.QueryEscape(strings.TrimSpace(query)))
	var itemsRaw []struct {
		Slug        string `json:"slug"`
		Name        string `json:"anime_name"`
		Image       string `json:"anime_image"`
		Description string `json:"description"`
		ReleaseYear any    `json:"release_year"`
	}
	if err := FetchJSON(ctx, u, p.headers(), &itemsRaw); err != nil {
		return nil, err
	}
	var items []MediaItem
	for _, it := range itemsRaw {
		if it.Slug == "" || it.Name == "" {
			continue
		}
		items = append(items, MediaItem{
			ID:          it.Slug,
			Title:       it.Name,
			Year:        ExtractYear(fmt.Sprintf("%v", it.ReleaseYear)),
			Type:        MediaTypeAnime,
			PosterURL:   it.Image,
			Description: it.Description,
		})
	}
	return items, nil
}

func (p *SonAnimeProvider) GetDetails(ctx context.Context, mediaID string) (*MediaDetail, error) {
	slug := strings.TrimPrefix(mediaID, "sonanime:")
	u := fmt.Sprintf("%s/api/anime/link/%s", p.apiBase(), url.PathEscape(slug))
	var data struct {
		Name        string `json:"anime_name"`
		Image       string `json:"anime_image"`
		Description string `json:"description"`
		Episodes    []struct {
			SeasonNumber  int    `json:"season_number"`
			EpisodeNumber int    `json:"episode_number"`
			EpisodeName   string `json:"episode_name"`
		} `json:"episodes"`
	}
	if err := FetchJSON(ctx, u, p.headers(), &data); err != nil {
		return nil, err
	}
	title := data.Name
	if title == "" {
		title = slug
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
		epTitle := ep.EpisodeName
		if epTitle == "" {
			epTitle = fmt.Sprintf("Bölüm %d", eNum)
		}
		seasonMap[sNum] = append(seasonMap[sNum], Episode{
			ID:       fmt.Sprintf("%s:%d:%d", slug, sNum, eNum),
			StreamID: fmt.Sprintf("%s:%d:%d", slug, sNum, eNum),
			Title:    epTitle,
			Season:   sNum,
			Number:   eNum,
		})
	}
	if len(seasonMap) == 0 {
		seasonMap[1] = []Episode{
			{
				ID:       fmt.Sprintf("%s:1:1", slug),
				StreamID: fmt.Sprintf("%s:1:1", slug),
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
			ID:          slug,
			Title:       title,
			Type:        MediaTypeAnime,
			PosterURL:   data.Image,
			Description: data.Description,
		},
		Seasons: seasons,
	}, nil
}

func (p *SonAnimeProvider) GetEpisodes(ctx context.Context, mediaID string, season int) ([]Episode, error) {
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

func (p *SonAnimeProvider) ResolveStream(ctx context.Context, streamID string) (*StreamResult, error) {
	parts := strings.Split(strings.TrimPrefix(streamID, "sonanime:"), ":")
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

	u := fmt.Sprintf("%s/api/anime/link/%s", p.apiBase(), url.PathEscape(slug))
	var data struct {
		Episodes []struct {
			SeasonNumber  int    `json:"season_number"`
			EpisodeNumber int    `json:"episode_number"`
			Fansub        string `json:"fansub"`
			Link1080      string `json:"episode_link_1080"`
			Link720       string `json:"episode_link_720"`
			Link480       string `json:"episode_link_480"`
			Link360       string `json:"episode_link_360"`
		} `json:"episodes"`
	}
	if err := FetchJSON(ctx, u, p.headers(), &data); err != nil {
		return nil, err
	}

	var sources []StreamSource
	hdrs := map[string]string{
		"User-Agent": config.DefaultUserAgent,
		"Referer":    "https://sonanime.com/",
	}
	for _, ep := range data.Episodes {
		sNum := ep.SeasonNumber
		if sNum <= 0 {
			sNum = 1
		}
		if (sNum == season && ep.EpisodeNumber == episode) || (season > 1 && ep.EpisodeNumber == episode) {
			fansub := ep.Fansub
			if fansub == "" {
				fansub = "Varsayılan"
			}
			addLink := func(link, q string) {
				if link != "" {
					sources = append(sources, StreamSource{
						Name:    "SonAnime",
						Title:   fmt.Sprintf("SonAnime (%s) • %s", fansub, q),
						URL:     link,
						Quality: q,
						Format:  "mp4",
						IsHLS:   false,
						Headers: hdrs,
					})
				}
			}
			addLink(ep.Link1080, "1080p")
			addLink(ep.Link720, "720p")
			addLink(ep.Link480, "480p")
			addLink(ep.Link360, "360p")
		}
	}
	return SelectBestStream(sources)
}
