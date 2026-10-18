package providers

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"strings"
)

type VidLinkProvider struct{}

func init() {
	Register(&VidLinkProvider{})
}

func (p *VidLinkProvider) ID() string   { return "vidlink" }
func (p *VidLinkProvider) Name() string { return "VidLink" }

func (p *VidLinkProvider) GetCatalog(ctx context.Context, page int) ([]MediaItem, error) {
	return TMDBPopularCatalog(ctx, true, true, page)
}

func (p *VidLinkProvider) Search(ctx context.Context, query string) ([]MediaItem, error) {
	return TMDBSearchCatalog(ctx, query, true, true)
}

func (p *VidLinkProvider) GetDetails(ctx context.Context, mediaID string) (*MediaDetail, error) {
	mType := MediaTypeMovie
	rawID := mediaID
	if strings.HasPrefix(mediaID, "tv:") || strings.HasPrefix(mediaID, "series:") {
		mType = MediaTypeSeries
		parts := strings.SplitN(mediaID, ":", 2)
		if len(parts) == 2 {
			rawID = parts[1]
		}
	} else if strings.HasPrefix(mediaID, "movie:") {
		parts := strings.SplitN(mediaID, ":", 2)
		if len(parts) == 2 {
			rawID = parts[1]
		}
	}

	info, err := ResolveTMDBInfo(ctx, rawID, mType)
	if err != nil {
		return nil, err
	}
	title := info.TitleTR
	if title == "" {
		title = info.TitleOriginal
	}

	if info.MediaType == MediaTypeSeries {
		return &MediaDetail{
			MediaItem: MediaItem{
				ID:    mediaID,
				Title: title,
				Year:  info.Year,
				Type:  MediaTypeSeries,
			},
			Seasons: info.Seasons,
		}, nil
	}

	return &MediaDetail{
		MediaItem: MediaItem{
			ID:    mediaID,
			Title: title,
			Year:  info.Year,
			Type:  MediaTypeMovie,
		},
		StreamID: fmt.Sprintf("movie:%s", info.ID),
	}, nil
}

func (p *VidLinkProvider) GetEpisodes(ctx context.Context, mediaID string, season int) ([]Episode, error) {
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

func (p *VidLinkProvider) ResolveStream(ctx context.Context, streamID string) (*StreamResult, error) {
	parts := strings.Split(streamID, ":")
	kind := "movie"
	tmdbID := streamID
	season, episode := "1", "1"
	if len(parts) >= 2 {
		kind = parts[0]
		tmdbID = parts[1]
		if len(parts) >= 4 {
			season = parts[2]
			episode = parts[3]
		}
	}

	encEndpoint := fmt.Sprintf("https://enc-dec.app/api/enc-vidlink?text=%s", url.QueryEscape(tmdbID))
	var encRes struct {
		Result string `json:"result"`
	}
	if err := FetchJSON(ctx, encEndpoint, nil, &encRes); err != nil || encRes.Result == "" {
		return nil, fmt.Errorf("vidlink: failed to encrypt tmdbId %s", tmdbID)
	}

	apiURL := fmt.Sprintf("https://vidlink.pro/api/b/movie/%s", encRes.Result)
	if kind == "tv" {
		apiURL = fmt.Sprintf("https://vidlink.pro/api/b/tv/%s/%s/%s", encRes.Result, season, episode)
	}

	respBody, _, err := FetchString(ctx, apiURL, map[string]string{
		"Referer": "https://vidlink.pro/",
		"Origin":  "https://vidlink.pro",
	})
	if err != nil {
		return nil, err
	}

	var parsed struct {
		Stream struct {
			Playlist  string `json:"playlist"`
			Qualities map[string]struct {
				URL string `json:"url"`
			} `json:"qualities"`
			Captions []struct {
				URL      string `json:"url"`
				Language string `json:"language"`
			} `json:"captions"`
		} `json:"stream"`
	}
	if err := json.Unmarshal([]byte(respBody), &parsed); err != nil {
		return nil, err
	}

	var subs []Subtitle
	for i, c := range parsed.Stream.Captions {
		if c.URL != "" {
			subs = append(subs, Subtitle{
				ID:       fmt.Sprintf("vl_sub_%d", i),
				Language: c.Language,
				Label:    c.Language,
				URL:      c.URL,
				Format:   "vtt",
			})
		}
	}

	var sources []StreamSource
	if parsed.Stream.Playlist != "" {
		sources = append(sources, StreamSource{
			Name:    "VidLink Auto HLS",
			Title:   "VidLink • 1080p HLS",
			URL:     parsed.Stream.Playlist,
			Quality: "1080p",
			Format:  "hls",
			IsHLS:   true,
			Headers: map[string]string{
				"Referer": "https://vidlink.pro/",
				"Origin":  "https://vidlink.pro",
			},
			Subtitles: subs,
		})
	}
	for q, qObj := range parsed.Stream.Qualities {
		if qObj.URL != "" {
			sources = append(sources, StreamSource{
				Name:      "VidLink " + q,
				Title:     "VidLink • " + q,
				URL:       qObj.URL,
				Quality:   q,
				Format:    "mp4",
				IsHLS:     false,
				Headers:   nil,
				Subtitles: subs,
			})
		}
	}

	return SelectBestStream(sources)
}
