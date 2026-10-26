package providers

import (
	"context"
	"fmt"
	"regexp"
	"strconv"
	"strings"

	"anthology-dav/config"
)

type VidmodyProvider struct{}

func init() {
	Register(&VidmodyProvider{})
}

func (p *VidmodyProvider) ID() string   { return "vidmody" }
func (p *VidmodyProvider) Name() string { return "Vidmody" }

func (p *VidmodyProvider) GetCatalog(ctx context.Context, page int) ([]MediaItem, error) {
	return TMDBPopularCatalog(ctx, true, true, page)
}

func (p *VidmodyProvider) Search(ctx context.Context, query string) ([]MediaItem, error) {
	return TMDBSearchCatalog(ctx, query, true, true)
}

func (p *VidmodyProvider) GetDetails(ctx context.Context, mediaID string) (*MediaDetail, error) {
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
	imdbID := info.IMDbID
	if imdbID == "" {
		imdbID = "tt" + info.ID
	}
	title := info.TitleTR
	if title == "" {
		title = info.TitleOriginal
	}

	if info.MediaType == MediaTypeSeries {
		var seasons []Season
		for _, s := range info.Seasons {
			var eps []Episode
			for _, ep := range s.Episodes {
				token := fmt.Sprintf("tv:%s:%d:%d", imdbID, s.Number, ep.Number)
				eps = append(eps, Episode{
					ID:       token,
					StreamID: token,
					Title:    ep.Title,
					Season:   s.Number,
					Number:   ep.Number,
				})
			}
			seasons = append(seasons, Season{
				Number:   s.Number,
				Name:     s.Name,
				Episodes: eps,
			})
		}
		return &MediaDetail{
			MediaItem: MediaItem{
				ID:    mediaID,
				Title: title,
				Year:  info.Year,
				Type:  MediaTypeSeries,
			},
			Seasons: seasons,
		}, nil
	}

	return &MediaDetail{
		MediaItem: MediaItem{
			ID:    mediaID,
			Title: title,
			Year:  info.Year,
			Type:  MediaTypeMovie,
		},
		StreamID: fmt.Sprintf("movie:%s", imdbID),
	}, nil
}

func (p *VidmodyProvider) GetEpisodes(ctx context.Context, mediaID string, season int) ([]Episode, error) {
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

func (p *VidmodyProvider) ResolveStream(ctx context.Context, streamID string) (*StreamResult, error) {
	parts := strings.Split(streamID, ":")
	idPart := streamID
	isMovie := true
	seasonNum := 1
	episodeNum := 1

	if len(parts) >= 2 {
		idPart = parts[1]
		if parts[0] == "tv" || parts[0] == "series" {
			isMovie = false
			if len(parts) >= 4 {
				seasonNum, _ = strconv.Atoi(parts[2])
				episodeNum, _ = strconv.Atoi(parts[3])
			}
		}
	}

	if !strings.HasPrefix(idPart, "tt") {
		mType := MediaTypeMovie
		if !isMovie {
			mType = MediaTypeSeries
		}
		if info, err := ResolveTMDBInfo(ctx, idPart, mType); err == nil && info != nil && info.IMDbID != "" {
			idPart = info.IMDbID
		}
	}

	var candidateURLs []string
	if isMovie {
		candidateURLs = []string{
			fmt.Sprintf("https://ha.vixolity.com/vs/%s", idPart),
			fmt.Sprintf("https://vidmody.com/vs/%s", idPart),
		}
	} else {
		candidateURLs = []string{
			fmt.Sprintf("https://ha.vixolity.com/vs/%s/s%d/e%02d", idPart, seasonNum, episodeNum),
			fmt.Sprintf("https://vidmody.com/vs/%s/s%d/e%02d", idPart, seasonNum, episodeNum),
		}
	}

	headers := map[string]string{
		"User-Agent": config.DefaultUserAgent,
		"Referer":    "https://pl.vixolity.com/",
		"Origin":     "https://pl.vixolity.com",
		"Accept":     "*/*",
	}

	var sources []StreamSource

	for _, targetURL := range candidateURLs {
		body, code, err := FetchString(ctx, targetURL, headers)
		if err != nil || code != 200 {
			continue
		}

		var subs []Subtitle
		// 1. Check if the response is directly an HLS playlist (#EXTM3U)
		if strings.Contains(body, "#EXTM3U") {
			subRe := regexp.MustCompile(`(?i)#EXT-X-MEDIA:TYPE=SUBTITLES.*?NAME="([^"]+)".*?URI="([^"]+)"`)
			for _, sm := range subRe.FindAllStringSubmatch(body, -1) {
				subs = append(subs, Subtitle{
					Language: strings.ToLower(sm[1]),
					Label:    sm[1],
					URL:      ResolveRelativeURL(sm[2], targetURL),
					Format:   "vtt",
					Headers:  headers,
				})
			}
			sources = append(sources, StreamSource{
				Name:      "Vidmody (Vixolity)",
				Title:     "Vidmody • 1080p HLS",
				URL:       targetURL,
				Quality:   "1080p",
				Format:    "hls",
				IsHLS:     true,
				Headers:   headers,
				Subtitles: subs,
			})
			break
		}

		// 2. Encrypted HTML block (VidmodyDecrypt)
		subs = append(subs, ExtractSubtitles(body, targetURL)...)
		aType := AudioTypeOriginal
		tag := "[Orijinal Ses]"
		if len(subs) > 0 {
			aType = AudioTypeSub
			tag = "[TR Altyazı]"
		}
		reDec := regexp.MustCompile(`(?i)(?:decrypt|\))\s*\(\s*["']([0-9a-fA-F]+)["']\s*,\s*(\d+)\s*\)`)
		reFile := regexp.MustCompile(`file\s*:\s*["'](https?://[^"']+)["']`)
		if dm := reDec.FindStringSubmatch(body); len(dm) > 2 {
			key, _ := strconv.Atoi(dm[2])
			decStr := VidmodyDecrypt(dm[1], key)
			if fm := reFile.FindStringSubmatch(decStr); len(fm) > 1 {
				sources = append(sources, StreamSource{
					Name:      "Vidmody HLS",
					Title:     "Vidmody • " + tag + " • 1080p",
					URL:       fm[1],
					Quality:   "1080p",
					Format:    "hls",
					IsHLS:     true,
					AudioType: aType,
					Headers:   headers,
					Subtitles: subs,
				})
				break
			}
		}

		if m3u8 := genericM3U8Re.FindString(body); m3u8 != "" {
			sources = append(sources, StreamSource{
				Name:      "Vidmody Direct",
				Title:     "Vidmody Direct • " + tag + " • 1080p",
				URL:       strings.ReplaceAll(m3u8, `\/`, `/`),
				Quality:   "1080p",
				Format:    "hls",
				IsHLS:     true,
				AudioType: aType,
				Headers:   headers,
				Subtitles: subs,
			})
			break
		}
	}

	return SelectBestStream(sources)
}
