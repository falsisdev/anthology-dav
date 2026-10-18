package providers

import (
	"context"
	"fmt"
	"strings"
)

type LiveM3UProvider struct {
	id           string
	name         string
	groupFilters []string
	includeSport bool
}

func newLiveM3UProvider(id, name string, groupFilters []string, includeSport bool) *LiveM3UProvider {
	return &LiveM3UProvider{
		id:           id,
		name:         name,
		groupFilters: groupFilters,
		includeSport: includeSport,
	}
}

func init() {
	Register(newLiveM3UProvider("m3u_list", "Canli_TV_Tum_Kanallar", nil, false))
}

func (p *LiveM3UProvider) ID() string   { return p.id }
func (p *LiveM3UProvider) Name() string { return p.name }

func (p *LiveM3UProvider) loadChannels(ctx context.Context) ([]LiveChannelEntry, error) {
	m3u, err := FetchCanliM3U(ctx)
	if err != nil {
		return nil, err
	}
	var filterFn func(string) bool
	if len(p.groupFilters) > 0 {
		filterFn = func(group string) bool {
			upper := strings.ToUpper(group)
			for _, gf := range p.groupFilters {
				if strings.Contains(upper, strings.ToUpper(gf)) {
					return true
				}
			}
			return false
		}
	}
	return ParseCanliM3UChannels(m3u, filterFn), nil
}

func (p *LiveM3UProvider) GetCatalog(ctx context.Context, page int) ([]MediaItem, error) {
	channels, err := p.loadChannels(ctx)
	if err != nil {
		return nil, err
	}
	var items []MediaItem
	for _, ch := range channels {
		items = append(items, MediaItem{
			ID:        fmt.Sprintf("live:%s|||%s", ch.Name, ch.URL),
			Title:     ch.Name,
			Type:      MediaTypeLive,
			PosterURL: ch.Logo,
		})
	}
	return items, nil
}

func (p *LiveM3UProvider) Search(ctx context.Context, query string) ([]MediaItem, error) {
	channels, err := p.loadChannels(ctx)
	if err != nil {
		return nil, err
	}
	qLower := strings.ToLower(strings.TrimSpace(query))
	var items []MediaItem
	for _, ch := range channels {
		if strings.Contains(strings.ToLower(ch.Name), qLower) || strings.Contains(strings.ToLower(ch.Group), qLower) {
			items = append(items, MediaItem{
				ID:        fmt.Sprintf("live:%s|||%s", ch.Name, ch.URL),
				Title:     ch.Name,
				Type:      MediaTypeLive,
				PosterURL: ch.Logo,
			})
		}
	}
	return items, nil
}

func (p *LiveM3UProvider) GetDetails(ctx context.Context, mediaID string) (*MediaDetail, error) {
	trimmed := strings.TrimPrefix(mediaID, "live:")
	parts := strings.SplitN(trimmed, "|||", 2)
	title := parts[0]
	streamURL := trimmed
	if len(parts) == 2 {
		streamURL = parts[1]
	}
	return &MediaDetail{
		MediaItem: MediaItem{
			ID:    mediaID,
			Title: title,
			Type:  MediaTypeLive,
		},
		StreamID: fmt.Sprintf("%s|||%s", title, streamURL),
	}, nil
}

func (p *LiveM3UProvider) GetEpisodes(ctx context.Context, mediaID string, season int) ([]Episode, error) {
	return nil, nil
}

func (p *LiveM3UProvider) ResolveStream(ctx context.Context, streamID string) (*StreamResult, error) {
	trimmed := strings.TrimPrefix(streamID, "live:")
	parts := strings.SplitN(trimmed, "|||", 2)
	title := parts[0]
	u := trimmed
	if len(parts) == 2 {
		u = parts[1]
	}

	var sources []StreamSource

	// 1. Direct URL from streamID (if provided)
	if strings.HasPrefix(u, "http") {
		sources = append(sources, StreamSource{
			Name:    p.name,
			Title:   p.name + " • " + title,
			URL:     u,
			Quality: "1080p",
			Format:  "hls",
			IsHLS:   true,
			IsLive:  true,
		})
	}

	// 2. Lookup channel in canli.m3u for backup streams
	if channels, err := p.loadChannels(ctx); err == nil {
		for _, ch := range channels {
			if strings.EqualFold(ch.Name, title) || strings.EqualFold(ch.ID, title) || (u != "" && ch.URL == u) {
				if ch.URL != "" && ch.URL != u {
					sources = append(sources, StreamSource{
						Name:    p.name,
						Title:   ch.Name,
						URL:     ch.URL,
						Quality: "1080p",
						Format:  "hls",
						IsHLS:   true,
						IsLive:  true,
					})
				}
				for _, b := range ch.Backups {
					if b != "" && b != u && b != ch.URL {
						sources = append(sources, StreamSource{
							Name:    p.name + " (Yedek)",
							Title:   ch.Name + " [Yedek]",
							URL:     b,
							Quality: "720p",
							Format:  "hls",
							IsHLS:   true,
							IsLive:  true,
						})
					}
				}
				break
			}
		}
	}

	// 3. Multi-platform Sport engine resolver
	if p.includeSport || strings.Contains(strings.ToUpper(title), "SPOR") || strings.Contains(strings.ToUpper(title), "BEIN") {
		sportStreams := ResolveMultiSportStreams(ctx, title, title)
		sources = append(sources, sportStreams...)
	}

	return SelectBestStream(sources)
}
