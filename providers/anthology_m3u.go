package providers

import (
	"context"
	"fmt"
	"regexp"
	"strings"
	"unicode"
)

const (
	nuvioBaseMovieURL  = "https://raw.githubusercontent.com/mooncrown04/m3ubirlestir/main/nuvio_parcalari"
	nuvioBaseSeriesURL = "https://raw.githubusercontent.com/mooncrown04/m3ubirlestir/main/nuvio_dizi_parcalari"
)

type AnthologyM3UProvider struct{}

func init() {
	Register(&AnthologyM3UProvider{})
}

func (p *AnthologyM3UProvider) ID() string   { return "anthology_m3u" }
func (p *AnthologyM3UProvider) Name() string { return "Anthology_M3U_Arsiv" }

func getTurkishLetterGroup(title string) string {
	norm := FoldTurkish(strings.TrimSpace(title))
	if norm == "" {
		return "diger"
	}
	r := []rune(norm)[0]
	if r >= 'a' && r <= 'z' {
		return string(r)
	}
	if unicode.IsDigit(r) {
		return "0_9_rakam"
	}
	return "diger"
}

func (p *AnthologyM3UProvider) GetCatalog(ctx context.Context, page int) ([]MediaItem, error) {
	if page < 1 {
		page = 1
	}
	groups := []string{"a", "b", "c", "d", "k", "m", "s", "t"}
	grp := groups[(page-1)%len(groups)]
	u := fmt.Sprintf("%s/nuvio_%s.m3u", nuvioBaseMovieURL, grp)
	body, _, err := FetchString(ctx, u, nil)
	if err != nil {
		return nil, err
	}
	return p.parseM3UEntries(body, 60), nil
}

func (p *AnthologyM3UProvider) Search(ctx context.Context, query string) ([]MediaItem, error) {
	grp := getTurkishLetterGroup(query)
	var items []MediaItem
	qLower := strings.ToLower(strings.TrimSpace(query))

	movieURL := fmt.Sprintf("%s/nuvio_%s.m3u", nuvioBaseMovieURL, grp)
	if body, _, err := FetchString(ctx, movieURL, nil); err == nil {
		for _, item := range p.parseM3UEntries(body, 2000) {
			if strings.Contains(strings.ToLower(item.Title), qLower) {
				items = append(items, item)
				if len(items) >= 40 {
					break
				}
			}
		}
	}

	seriesURL := fmt.Sprintf("%s/dizi_%s_s1.m3u", nuvioBaseSeriesURL, grp)
	if body, _, err := FetchString(ctx, seriesURL, nil); err == nil {
		for _, item := range p.parseM3UEntries(body, 2000) {
			if strings.Contains(strings.ToLower(item.Title), qLower) {
				items = append(items, item)
				if len(items) >= 60 {
					break
				}
			}
		}
	}
	return items, nil
}

func (p *AnthologyM3UProvider) parseM3UEntries(m3u string, limit int) []MediaItem {
	var items []MediaItem
	seen := make(map[string]bool)
	lines := strings.Split(m3u, "\n")
	reLogo := regexp.MustCompile(`tvg-logo="([^"]*)"`)

	var currentTitle, currentLogo string
	for _, rawLine := range lines {
		line := strings.TrimSpace(rawLine)
		if strings.HasPrefix(line, "#EXTINF:") {
			if idx := strings.LastIndex(line, ","); idx != -1 {
				currentTitle = CleanMediaTitle(line[idx+1:])
			}
			if m := reLogo.FindStringSubmatch(line); len(m) > 1 {
				currentLogo = m[1]
			}
		} else if strings.HasPrefix(line, "http") && currentTitle != "" {
			if !seen[line] {
				seen[line] = true
				items = append(items, MediaItem{
					ID:        fmt.Sprintf("m3u:%s|||%s", currentTitle, line),
					Title:     currentTitle,
					Year:      ExtractYear(currentTitle),
					Type:      MediaTypeMovie,
					PosterURL: currentLogo,
				})
				if len(items) >= limit {
					break
				}
			}
			currentTitle = ""
			currentLogo = ""
		}
	}
	return items
}

func (p *AnthologyM3UProvider) GetDetails(ctx context.Context, mediaID string) (*MediaDetail, error) {
	trimmed := strings.TrimPrefix(mediaID, "m3u:")
	parts := strings.SplitN(trimmed, "|||", 2)
	title := parts[0]
	streamURL := trimmed
	if len(parts) == 2 {
		streamURL = parts[1]
	}
	return &MediaDetail{
		MediaItem: MediaItem{
			ID:    mediaID,
			Title: CleanMediaTitle(title),
			Year:  ExtractYear(title),
			Type:  MediaTypeMovie,
		},
		StreamID: streamURL,
	}, nil
}

func (p *AnthologyM3UProvider) GetEpisodes(ctx context.Context, mediaID string, season int) ([]Episode, error) {
	return nil, nil
}

func (p *AnthologyM3UProvider) ResolveStream(ctx context.Context, streamID string) (*StreamResult, error) {
	u := streamID
	if strings.HasPrefix(u, "m3u:") {
		parts := strings.SplitN(strings.TrimPrefix(u, "m3u:"), "|||", 2)
		if len(parts) == 2 {
			u = parts[1]
		}
	}
	if !strings.HasPrefix(u, "http") {
		return nil, fmt.Errorf("anthology_m3u: invalid stream URL %s", streamID)
	}
	isHLS := strings.Contains(u, ".m3u8")
	format := "mp4"
	if isHLS {
		format = "hls"
	}
	return SelectBestStream([]StreamSource{
		{
			Name:    "Anthology M3U Arşiv",
			Title:   "M3U Arşiv • 1080p",
			URL:     u,
			Quality: "1080p",
			Format:  format,
			IsHLS:   isHLS,
		},
	})
}
