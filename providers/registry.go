package providers

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"sync"
)

// Provider is the standard interface implemented by all 41 Anthology providers.
type Provider interface {
	ID() string
	Name() string
	GetCatalog(ctx context.Context, page int) ([]MediaItem, error)
	Search(ctx context.Context, query string) ([]MediaItem, error)
	GetDetails(ctx context.Context, mediaID string) (*MediaDetail, error)
	GetEpisodes(ctx context.Context, mediaID string, season int) ([]Episode, error)
	ResolveStream(ctx context.Context, streamID string) (*StreamResult, error)
}

var (
	registryMu     sync.RWMutex
	byID           = make(map[string]Provider)
	byName         = make(map[string]Provider)
	orderedIDs     []string
)

// Register adds a Provider to the central registry.
func Register(p Provider) {
	if p == nil {
		return
	}
	registryMu.Lock()
	defer registryMu.Unlock()

	id := strings.TrimSpace(p.ID())
	name := strings.TrimSpace(p.Name())
	if _, exists := byID[id]; !exists {
		orderedIDs = append(orderedIDs, id)
	}
	byID[id] = p
	byName[name] = p
	byName[strings.ToLower(name)] = p
}

// GetByID retrieves a registered provider by its ID.
func GetByID(id string) (Provider, bool) {
	registryMu.RLock()
	defer registryMu.RUnlock()
	p, ok := byID[strings.TrimSpace(id)]
	return p, ok
}

// GetByName retrieves a registered provider by its WebDAV folder Name (or ID).
func GetByName(name string) (Provider, bool) {
	registryMu.RLock()
	defer registryMu.RUnlock()
	clean := strings.TrimSpace(name)
	if p, ok := byName[clean]; ok {
		return p, true
	}
	if p, ok := byName[strings.ToLower(clean)]; ok {
		return p, true
	}
	if p, ok := byID[strings.ToLower(clean)]; ok {
		return p, true
	}
	return nil, false
}

// All returns all registered providers sorted alphabetically by their display Name.
func All() []Provider {
	registryMu.RLock()
	defer registryMu.RUnlock()
	list := make([]Provider, 0, len(orderedIDs))
	for _, id := range orderedIDs {
		if p, ok := byID[id]; ok {
			list = append(list, p)
		}
	}
	sort.Slice(list, func(i, j int) bool {
		return strings.ToLower(list[i].Name()) < strings.ToLower(list[j].Name())
	})
	return list
}

// Count returns the total number of registered providers.
func Count() int {
	registryMu.RLock()
	defer registryMu.RUnlock()
	return len(byID)
}

// SelectBestStream sorts streams by quality (4K > 1440p > 1080p > 720p > 480p > 360p) and builds a StreamResult.
func SelectBestStream(sources []StreamSource) (*StreamResult, error) {
	valid := make([]StreamSource, 0, len(sources))
	seen := make(map[string]bool)
	for _, s := range sources {
		u := strings.TrimSpace(s.URL)
		if u == "" || seen[u] {
			continue
		}
		seen[u] = true
		if s.Format == "" {
			if s.IsHLS || strings.Contains(strings.ToLower(u), ".m3u8") {
				s.Format = "hls"
				s.IsHLS = true
			} else if strings.Contains(strings.ToLower(u), ".mkv") {
				s.Format = "mkv"
			} else {
				s.Format = "mp4"
			}
		}
		valid = append(valid, s)
	}
	if len(valid) == 0 {
		return nil, fmt.Errorf("no playable stream sources found")
	}

	SortStreamsByQuality(valid)

	// Merge all unique subtitles across sources
	var allSubs []Subtitle
	seenSub := make(map[string]bool)
	for _, src := range valid {
		for _, sub := range src.Subtitles {
			if sub.URL != "" && !seenSub[sub.URL] {
				seenSub[sub.URL] = true
				if sub.Format == "" {
					if strings.HasSuffix(strings.ToLower(sub.URL), ".srt") {
						sub.Format = "srt"
					} else {
						sub.Format = "vtt"
					}
				}
				if sub.Language == "" {
					sub.Language = "tr"
				}
				allSubs = append(allSubs, sub)
			}
		}
	}
	if len(valid[0].Subtitles) == 0 && len(allSubs) > 0 {
		valid[0].Subtitles = allSubs
	}

	res := &StreamResult{
		Primary:   valid[0],
		Subtitles: allSubs,
	}
	if len(valid) > 1 {
		res.Alternates = valid[1:]
	}
	return res, nil
}
