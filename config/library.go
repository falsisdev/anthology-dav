package config

import (
	"encoding/json"
	"fmt"
	"os"
	"sort"
	"strings"
	"sync"
	"time"
)

// LibrarySeason caches season number and episode count so VFS listings require 0 network calls.
type LibrarySeason struct {
	SeasonNumber int `json:"season_number"`
	EpisodeCount int `json:"episode_count"`
}

// LibraryItem represents a user-saved movie, TV series, or anime in the personal library.
type LibraryItem struct {
	ID               string          `json:"id"`                          // e.g. "movie:550", "tv:1396"
	TMDBID           int             `json:"tmdb_id"`                     // TMDB numerical ID
	Title            string          `json:"title"`                       // Primary title
	OriginalTitle    string          `json:"original_title,omitempty"`    // Original title
	EnglishTitle     string          `json:"english_title,omitempty"`     // English/international title
	AltTitles        []string        `json:"alt_titles,omitempty"`        // Alternative/Romaji titles
	Year             string          `json:"year"`                        // Release year (e.g. "2008")
	Type             string          `json:"type"`                        // "movie", "series", "anime"
	PosterURL        string          `json:"poster_url,omitempty"`        // Poster image URL
	BackdropURL      string          `json:"backdrop_url,omitempty"`      // Backdrop banner image URL
	Overview         string          `json:"overview,omitempty"`          // Synopsis/description
	SelectedProvider string          `json:"selected_provider,omitempty"` // "auto" or specific provider ID
	WorkingProviders []string        `json:"working_providers,omitempty"` // Verified working provider IDs
	Seasons          []LibrarySeason `json:"seasons,omitempty"`           // Cached seasons & episode counts
	AddedAt          time.Time       `json:"added_at"`                    // Time added to library
}

// LibraryData is the JSON structure persisted to library.json.
type LibraryData struct {
	Items []LibraryItem `json:"items"`
}

// LibraryManager manages thread-safe CRUD operations for user library.
type LibraryManager struct {
	mu   sync.RWMutex
	path string
	data LibraryData
}

var (
	libManager *LibraryManager
	libOnce    sync.Once
)

// Library returns the global LibraryManager singleton.
func Library() *LibraryManager {
	libOnce.Do(func() {
		cfg := Get()
		libManager = &LibraryManager{
			path: cfg.LibraryPath,
			data: LibraryData{
				Items: []LibraryItem{},
			},
		}
		_ = libManager.Load()
	})
	return libManager
}

// Load reads library.json from disk.
func (lm *LibraryManager) Load() error {
	lm.mu.Lock()
	defer lm.mu.Unlock()

	raw, err := os.ReadFile(lm.path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	var loaded LibraryData
	if err := json.Unmarshal(raw, &loaded); err != nil {
		return err
	}
	if loaded.Items == nil {
		loaded.Items = []LibraryItem{}
	}
	lm.data = loaded
	return nil
}

func (lm *LibraryManager) saveLocked() error {
	raw, err := json.MarshalIndent(lm.data, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(lm.path, raw, 0644)
}

// All returns a slice copy of all library items, sorted newest to oldest.
func (lm *LibraryManager) All() []LibraryItem {
	lm.mu.RLock()
	defer lm.mu.RUnlock()

	out := make([]LibraryItem, len(lm.data.Items))
	copy(out, lm.data.Items)
	sort.Slice(out, func(i, j int) bool {
		return out[i].AddedAt.After(out[j].AddedAt)
	})
	return out
}

// Get finds an item by its ID.
func (lm *LibraryManager) Get(id string) (LibraryItem, bool) {
	lm.mu.RLock()
	defer lm.mu.RUnlock()

	cleanID := strings.TrimSpace(id)
	for _, it := range lm.data.Items {
		if strings.EqualFold(it.ID, cleanID) {
			return it, true
		}
	}
	return LibraryItem{}, false
}

// GetByTMDB finds an item by mediaType ("movie", "series", "anime") and TMDB ID.
func (lm *LibraryManager) GetByTMDB(mediaType string, tmdbID int) (LibraryItem, bool) {
	lm.mu.RLock()
	defer lm.mu.RUnlock()

	for _, it := range lm.data.Items {
		if it.TMDBID == tmdbID {
			if mediaType == "" || strings.EqualFold(it.Type, mediaType) ||
				(mediaType == "series" && it.Type == "anime") ||
				(mediaType == "anime" && it.Type == "series") {
				return it, true
			}
		}
	}
	return LibraryItem{}, false
}

// GetByType returns all items of a specific type ("movie", "series", "anime").
func (lm *LibraryManager) GetByType(mediaType string) []LibraryItem {
	lm.mu.RLock()
	defer lm.mu.RUnlock()

	var out []LibraryItem
	cleanType := strings.ToLower(strings.TrimSpace(mediaType))
	for _, it := range lm.data.Items {
		if strings.ToLower(it.Type) == cleanType {
			out = append(out, it)
		}
	}
	sort.Slice(out, func(i, j int) bool {
		return strings.ToLower(out[i].Title) < strings.ToLower(out[j].Title)
	})
	return out
}

// Add adds or updates an item in the library.
func (lm *LibraryManager) Add(item LibraryItem) error {
	lm.mu.Lock()
	defer lm.mu.Unlock()

	if item.ID == "" {
		if item.Type == "movie" {
			item.ID = fmt.Sprintf("movie:%d", item.TMDBID)
		} else {
			item.ID = fmt.Sprintf("tv:%d", item.TMDBID)
		}
	}
	if item.AddedAt.IsZero() {
		item.AddedAt = time.Now()
	}
	if item.SelectedProvider == "" {
		item.SelectedProvider = "auto"
	}

	found := false
	for i, it := range lm.data.Items {
		if strings.EqualFold(it.ID, item.ID) {
			lm.data.Items[i] = item
			found = true
			break
		}
	}
	if !found {
		lm.data.Items = append(lm.data.Items, item)
	}

	return lm.saveLocked()
}

// Remove removes an item from the library by ID or TMDB ID.
func (lm *LibraryManager) Remove(id string) error {
	lm.mu.Lock()
	defer lm.mu.Unlock()

	cleanID := strings.TrimSpace(id)
	newItems := make([]LibraryItem, 0, len(lm.data.Items))
	removed := false
	for _, it := range lm.data.Items {
		if strings.EqualFold(it.ID, cleanID) || strings.EqualFold(fmt.Sprintf("%d", it.TMDBID), cleanID) {
			removed = true
			continue
		}
		newItems = append(newItems, it)
	}
	if removed {
		lm.data.Items = newItems
		return lm.saveLocked()
	}
	return nil
}

// UpdateProvider sets the preferred stream provider for a library item.
func (lm *LibraryManager) UpdateProvider(id, providerID string) error {
	lm.mu.Lock()
	defer lm.mu.Unlock()

	cleanID := strings.TrimSpace(id)
	cleanProv := strings.TrimSpace(providerID)
	if cleanProv == "" {
		cleanProv = "auto"
	}

	for i, it := range lm.data.Items {
		if strings.EqualFold(it.ID, cleanID) {
			lm.data.Items[i].SelectedProvider = cleanProv
			return lm.saveLocked()
		}
	}
	return fmt.Errorf("library: item %s not found", id)
}

// UpdateMetadata updates cached seasons and alternative titles for a library item.
func (lm *LibraryManager) UpdateMetadata(id string, englishTitle string, altTitles []string, seasons []LibrarySeason) error {
	lm.mu.Lock()
	defer lm.mu.Unlock()

	cleanID := strings.TrimSpace(id)
	for i, it := range lm.data.Items {
		if strings.EqualFold(it.ID, cleanID) {
			if englishTitle != "" {
				lm.data.Items[i].EnglishTitle = englishTitle
			}
			if len(altTitles) > 0 {
				lm.data.Items[i].AltTitles = altTitles
			}
			if len(seasons) > 0 {
				lm.data.Items[i].Seasons = seasons
			}
			return lm.saveLocked()
		}
	}
	return nil
}

// UpdateWorkingProviders records verified working provider IDs for a library item.
func (lm *LibraryManager) UpdateWorkingProviders(id string, working []string) error {
	lm.mu.Lock()
	defer lm.mu.Unlock()

	cleanID := strings.TrimSpace(id)
	for i, it := range lm.data.Items {
		if strings.EqualFold(it.ID, cleanID) {
			lm.data.Items[i].WorkingProviders = working
			return lm.saveLocked()
		}
	}
	return nil
}
