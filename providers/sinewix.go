package providers

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"regexp"
	"strings"

	"anthology-dav/config"
)

const (
	sinewixKey       = "9iQNC5HQwPlaFuJDkhncJ5XTJ8feGXOJatAA"
	sinewixHash256   = "459173e7d53c1a25b78c9c1a3e2d1f4b8a7c6e5d4f3a2b1c0d9e8f7a6b5c4d3e"
	sinewixSignature = "EasyPlex-Signature-V2"
	sinewixUA        = "EasyPlex (Android 14; SM-A546B; Samsung Galaxy A54 5G; tr)"
)

type SineWixProvider struct{}

func init() {
	Register(&SineWixProvider{})
}

func (p *SineWixProvider) ID() string   { return "sinewix" }
func (p *SineWixProvider) Name() string { return "SineWix" }

func (p *SineWixProvider) baseURL() string {
	return config.RemoteString("urls.movies.sinewix", "https://ydfvfdizipanel.ru/public/api")
}

func (p *SineWixProvider) headers() map[string]string {
	return map[string]string{
		"User-Agent": sinewixUA,
		"Accept":     "application/json",
		"hash256":    sinewixHash256,
		"signature":  sinewixSignature,
	}
}

func (p *SineWixProvider) GetCatalog(ctx context.Context, page int) ([]MediaItem, error) {
	var items []MediaItem
	seriesURL := fmt.Sprintf("%s/series/popular/%s", p.baseURL(), sinewixKey)
	if body, _, err := FetchString(ctx, seriesURL, p.headers()); err == nil {
		items = append(items, p.parseItems(body, MediaTypeSeries)...)
	}
	moviesURL := fmt.Sprintf("%s/genres/movies/all/%s", p.baseURL(), sinewixKey)
	if body, _, err := FetchString(ctx, moviesURL, p.headers()); err == nil {
		items = append(items, p.parseItems(body, MediaTypeMovie)...)
	}
	return items, nil
}

func (p *SineWixProvider) Search(ctx context.Context, query string) ([]MediaItem, error) {
	u := fmt.Sprintf("%s/search/%s/%s", p.baseURL(), url.PathEscape(query), sinewixKey)
	body, _, err := FetchString(ctx, u, p.headers())
	if err != nil {
		return nil, err
	}
	return p.parseItems(body, MediaTypeMovie), nil
}

func (p *SineWixProvider) parseItems(jsonStr string, defaultType MediaType) []MediaItem {
	var items []MediaItem
	var wrapper struct {
		Data   []map[string]any `json:"data"`
		Search []map[string]any `json:"search"`
	}
	var list []map[string]any
	if json.Unmarshal([]byte(jsonStr), &wrapper) == nil {
		list = append(wrapper.Data, wrapper.Search...)
	} else {
		_ = json.Unmarshal([]byte(jsonStr), &list)
	}
	for _, raw := range list {
		idStr := fmt.Sprintf("%v", raw["id"])
		title, _ := raw["title"].(string)
		if title == "" {
			title, _ = raw["name"].(string)
		}
		if idStr == "" || title == "" {
			continue
		}
		mType := defaultType
		if tStr, ok := raw["type"].(string); ok {
			if strings.Contains(strings.ToLower(tStr), "serie") || tStr == "1" {
				mType = MediaTypeSeries
			} else if strings.Contains(strings.ToLower(tStr), "anime") {
				mType = MediaTypeAnime
			}
		}
		if _, hasSeasons := raw["seasons"]; hasSeasons {
			mType = MediaTypeSeries
		}
		prefix := "movie"
		if mType == MediaTypeSeries || mType == MediaTypeAnime {
			prefix = "series"
		}
		poster, _ := raw["poster_path"].(string)
		releaseDate, _ := raw["release_date"].(string)
		if releaseDate == "" {
			releaseDate, _ = raw["first_air_date"].(string)
		}
		items = append(items, MediaItem{
			ID:        fmt.Sprintf("%s:%s", prefix, idStr),
			Title:     CleanMediaTitle(title),
			Year:      ExtractYear(releaseDate),
			Type:      mType,
			PosterURL: poster,
		})
	}
	return items
}

func (p *SineWixProvider) GetDetails(ctx context.Context, mediaID string) (*MediaDetail, error) {
	parts := strings.SplitN(mediaID, ":", 2)
	kind := "movie"
	rawID := mediaID
	if len(parts) == 2 {
		kind = parts[0]
		rawID = parts[1]
	}

	if kind == "series" {
		u := fmt.Sprintf("%s/series/show/%s/%s", p.baseURL(), rawID, sinewixKey)
		body, _, err := FetchString(ctx, u, p.headers())
		if err != nil {
			return nil, err
		}
		var data struct {
			ID           int    `json:"id"`
			Name         string `json:"name"`
			Title        string `json:"title"`
			PosterPath   string `json:"poster_path"`
			Overview     string `json:"overview"`
			FirstAirDate string `json:"first_air_date"`
			Seasons      []struct {
				SeasonNumber int `json:"season_number"`
				Episodes     []struct {
					ID            int    `json:"id"`
					EpisodeNumber int    `json:"episode_number"`
					Name          string `json:"name"`
					Videos        []struct {
						Link   string `json:"link"`
						Server string `json:"server"`
						Lang   string `json:"lang"`
					} `json:"videos"`
				} `json:"episodes"`
			} `json:"seasons"`
		}
		if err := json.Unmarshal([]byte(body), &data); err != nil {
			return nil, err
		}
		title := data.Name
		if title == "" {
			title = data.Title
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
					eNum = len(eps) + 1
				}
				linksJSON, _ := json.Marshal(ep.Videos)
				streamToken := fmt.Sprintf("videos:%s", string(linksJSON))
				eps = append(eps, Episode{
					ID:       fmt.Sprintf("%d", ep.ID),
					StreamID: streamToken,
					Title:    ep.Name,
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
		return &MediaDetail{
			MediaItem: MediaItem{
				ID:          mediaID,
				Title:       CleanMediaTitle(title),
				Year:        ExtractYear(data.FirstAirDate),
				Type:        MediaTypeSeries,
				PosterURL:   data.PosterPath,
				Description: data.Overview,
			},
			Seasons: seasons,
		}, nil
	}

	u := fmt.Sprintf("%s/media/detail/%s/%s", p.baseURL(), rawID, sinewixKey)
	body, _, err := FetchString(ctx, u, p.headers())
	if err != nil {
		return nil, err
	}
	var data struct {
		ID          int    `json:"id"`
		Title       string `json:"title"`
		PosterPath  string `json:"poster_path"`
		Overview    string `json:"overview"`
		ReleaseDate string `json:"release_date"`
		Videos      []struct {
			Link   string `json:"link"`
			Server string `json:"server"`
			Lang   string `json:"lang"`
		} `json:"videos"`
	}
	if err := json.Unmarshal([]byte(body), &data); err != nil {
		return nil, err
	}
	linksJSON, _ := json.Marshal(data.Videos)
	return &MediaDetail{
		MediaItem: MediaItem{
			ID:          mediaID,
			Title:       CleanMediaTitle(data.Title),
			Year:        ExtractYear(data.ReleaseDate),
			Type:        MediaTypeMovie,
			PosterURL:   data.PosterPath,
			Description: data.Overview,
		},
		StreamID: fmt.Sprintf("videos:%s", string(linksJSON)),
	}, nil
}

func (p *SineWixProvider) GetEpisodes(ctx context.Context, mediaID string, season int) ([]Episode, error) {
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

func (p *SineWixProvider) ResolveStream(ctx context.Context, streamID string) (*StreamResult, error) {
	if !strings.HasPrefix(streamID, "videos:") {
		detail, err := p.GetDetails(ctx, streamID)
		if err != nil {
			return nil, err
		}
		streamID = detail.StreamID
	}
	jsonPart := strings.TrimPrefix(streamID, "videos:")
	var videos []struct {
		Link   string `json:"link"`
		Server string `json:"server"`
		Lang   string `json:"lang"`
	}
	if err := json.Unmarshal([]byte(jsonPart), &videos); err != nil {
		return nil, err
	}

	var sources []StreamSource
	reMF := regexp.MustCompile(`href="(https?://download\d+\.mediafire\.com/[^"]+)"`)
	for _, v := range videos {
		link := strings.TrimSpace(v.Link)
		if link == "" {
			continue
		}
		if strings.Contains(link, "mediafire.com") {
			mfHTML, _, err := FetchString(ctx, link, nil)
			if err == nil {
				if m := reMF.FindStringSubmatch(mfHTML); len(m) > 1 {
					link = m[1]
				}
			}
		}
		isHLS := strings.Contains(link, ".m3u8")
		format := "mp4"
		if isHLS {
			format = "hls"
		}
		sources = append(sources, StreamSource{
			Name:    "SineWix",
			Title:   fmt.Sprintf("SineWix %s (%s)", v.Server, v.Lang),
			URL:     link,
			Quality: v.Server,
			Format:  format,
			IsHLS:   isHLS,
			Headers: map[string]string{
				"Referer":    "https://ydfvfdizipanel.ru/",
				"User-Agent": sinewixUA,
			},
		})
	}
	return SelectBestStream(sources)
}
