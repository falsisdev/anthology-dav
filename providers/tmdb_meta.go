package providers

import (
	"context"
	"fmt"
	"net/url"
	"strings"
	"sync"
	"time"

	"anthology-dav/config"
)

// TMDBShowDetails represents extended metadata for a TV show or anime from TMDB.
type TMDBShowDetails struct {
	ID            int          `json:"id"`
	Name          string       `json:"name"`
	OriginalName  string       `json:"original_name"`
	EnglishName   string       `json:"english_name"`
	AltTitles     []string     `json:"alt_titles"`
	FirstAirDate  string       `json:"first_air_date"`
	Overview      string       `json:"overview"`
	PosterPath    string       `json:"poster_path"`
	BackdropPath  string       `json:"backdrop_path"`
	VoteAverage   float64      `json:"vote_average"`
	OriginCountry []string     `json:"origin_country"`
	Genres        []TMDBGenre  `json:"genres"`
	Seasons       []TMDBSeason `json:"seasons"`
	IsAnime       bool         `json:"is_anime"`

	AlternativeTitles struct {
		Results []struct {
			ISO3166_1 string `json:"iso_3166_1"`
			Title     string `json:"title"`
		} `json:"results"`
	} `json:"alternative_titles"`

	Translations struct {
		Translations []struct {
			ISO639_1 string `json:"iso_639_1"`
			Data     struct {
				Name string `json:"name"`
			} `json:"data"`
		} `json:"translations"`
	} `json:"translations"`
}

type TMDBGenre struct {
	ID   int    `json:"id"`
	Name string `json:"name"`
}

// TMDBSeason represents a season of a TV show or anime.
type TMDBSeason struct {
	ID           int    `json:"id"`
	SeasonNumber int    `json:"season_number"`
	Name         string `json:"name"`
	EpisodeCount int    `json:"episode_count"`
	AirDate      string `json:"air_date"`
	PosterPath   string `json:"poster_path"`
	Overview     string `json:"overview"`
}

// TMDBEpisode represents a single episode of a TV show or anime.
type TMDBEpisode struct {
	ID            int     `json:"id"`
	EpisodeNumber int     `json:"episode_number"`
	SeasonNumber  int     `json:"season_number"`
	Name          string  `json:"name"`
	Overview      string  `json:"overview"`
	AirDate       string  `json:"air_date"`
	StillPath     string  `json:"still_path"`
	VoteAverage   float64 `json:"vote_average"`
}

// TMDBSearchResult represents a unified search item for the Web UI.
type TMDBSearchResult struct {
	ID            int      `json:"id"`
	Title         string   `json:"title"`
	OriginalTitle string   `json:"original_title"`
	Year          string   `json:"year"`
	Type          string   `json:"type"` // "movie", "series", "anime"
	PosterURL     string   `json:"poster_url"`
	BackdropURL   string   `json:"backdrop_url"`
	Overview      string   `json:"overview"`
	Rating        float64  `json:"rating"`
	Genres        []string `json:"genres"`
}

var (
	tmdbCacheMu      sync.RWMutex
	tmdbShowCache    = make(map[int]cachedTMDBShow)
	tmdbEpisodeCache = make(map[string]cachedTMDBEpisodes)
)

type cachedTMDBShow struct {
	data      *TMDBShowDetails
	expiresAt time.Time
}

type cachedTMDBEpisodes struct {
	data      []TMDBEpisode
	expiresAt time.Time
}

func hasLatinChars(s string) bool {
	for _, r := range s {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') {
			return true
		}
	}
	return false
}

// GetTMDBShowDetails retrieves show info including seasons, genres, alternative titles, and anime detection.
func GetTMDBShowDetails(ctx context.Context, tmdbID int) (*TMDBShowDetails, error) {
	if tmdbID <= 0 {
		return nil, fmt.Errorf("invalid tmdbID %d", tmdbID)
	}

	tmdbCacheMu.RLock()
	if c, ok := tmdbShowCache[tmdbID]; ok && time.Now().Before(c.expiresAt) {
		tmdbCacheMu.RUnlock()
		return c.data, nil
	}
	tmdbCacheMu.RUnlock()

	u := fmt.Sprintf("https://api.themoviedb.org/3/tv/%d?api_key=%s&language=tr-TR&append_to_response=alternative_titles,translations", tmdbID, config.DefaultTMDBAPIKey)
	var data TMDBShowDetails
	if err := FetchJSON(ctx, u, nil, &data); err != nil {
		return nil, err
	}

	// Extract English title and Latin alternative titles
	for _, tr := range data.Translations.Translations {
		if strings.EqualFold(tr.ISO639_1, "en") && strings.TrimSpace(tr.Data.Name) != "" {
			data.EnglishName = strings.TrimSpace(tr.Data.Name)
			break
		}
	}
	if data.EnglishName == "" && hasLatinChars(data.OriginalName) {
		data.EnglishName = strings.TrimSpace(data.OriginalName)
	}

	seenAlt := make(map[string]bool)
	addAlt := func(t string) {
		t = strings.TrimSpace(t)
		if t == "" || !hasLatinChars(t) {
			return
		}
		lower := strings.ToLower(t)
		if seenAlt[lower] {
			return
		}
		seenAlt[lower] = true
		data.AltTitles = append(data.AltTitles, t)
	}
	addAlt(data.Name)
	addAlt(data.EnglishName)
	addAlt(data.OriginalName)
	for _, alt := range data.AlternativeTitles.Results {
		if alt.ISO3166_1 == "US" || alt.ISO3166_1 == "GB" || alt.ISO3166_1 == "TR" || alt.ISO3166_1 == "JP" || alt.ISO3166_1 == "ROM" {
			addAlt(alt.Title)
		}
	}

	// Detect if anime: OriginCountry contains "JP" and genre 16 (Animation)
	isJP := false
	for _, c := range data.OriginCountry {
		if strings.EqualFold(c, "JP") {
			isJP = true
			break
		}
	}
	isAnim := false
	for _, g := range data.Genres {
		if g.ID == 16 || strings.Contains(strings.ToLower(g.Name), "anim") {
			isAnim = true
			break
		}
	}
	data.IsAnime = isJP && isAnim

	// Cache for 6 hours
	tmdbCacheMu.Lock()
	tmdbShowCache[tmdbID] = cachedTMDBShow{
		data:      &data,
		expiresAt: time.Now().Add(6 * time.Hour),
	}
	tmdbCacheMu.Unlock()

	return &data, nil
}

// PrewarmLibraryItemMetadata fetches seasons, episode counts, English title, and alt titles from TMDB
// and persists them into library.json so VFS directory listings take 0ms.
func PrewarmLibraryItemMetadata(ctx context.Context, item *config.LibraryItem) {
	if item == nil || item.TMDBID <= 0 {
		return
	}
	if item.Type == "movie" {
		if item.EnglishTitle != "" && len(item.AltTitles) > 0 {
			return
		}
		u := fmt.Sprintf("https://api.themoviedb.org/3/movie/%d?api_key=%s&language=tr-TR&append_to_response=alternative_titles,translations", item.TMDBID, config.DefaultTMDBAPIKey)
		var mData struct {
			Title        string `json:"title"`
			OriginalTitle string `json:"original_title"`
			AlternativeTitles struct {
				Titles []struct {
					ISO3166_1 string `json:"iso_3166_1"`
					Title     string `json:"title"`
				} `json:"titles"`
			} `json:"alternative_titles"`
			Translations struct {
				Translations []struct {
					ISO639_1 string `json:"iso_639_1"`
					Data     struct {
						Title string `json:"title"`
					} `json:"data"`
				} `json:"translations"`
			} `json:"translations"`
		}
		if err := FetchJSON(ctx, u, nil, &mData); err == nil {
			engTitle := ""
			for _, tr := range mData.Translations.Translations {
				if strings.EqualFold(tr.ISO639_1, "en") && strings.TrimSpace(tr.Data.Title) != "" {
					engTitle = strings.TrimSpace(tr.Data.Title)
					break
				}
			}
			if engTitle == "" && hasLatinChars(mData.OriginalTitle) {
				engTitle = strings.TrimSpace(mData.OriginalTitle)
			}
			seen := make(map[string]bool)
			var alts []string
			add := func(t string) {
				t = strings.TrimSpace(t)
				if t == "" || !hasLatinChars(t) {
					return
				}
				l := strings.ToLower(t)
				if !seen[l] {
					seen[l] = true
					alts = append(alts, t)
				}
			}
			add(item.Title)
			add(engTitle)
			add(item.OriginalTitle)
			for _, a := range mData.AlternativeTitles.Titles {
				if a.ISO3166_1 == "US" || a.ISO3166_1 == "GB" || a.ISO3166_1 == "TR" || a.ISO3166_1 == "JP" || a.ISO3166_1 == "ROM" {
					add(a.Title)
				}
			}
			config.Library().UpdateMetadata(item.ID, engTitle, alts, nil)
			item.EnglishTitle = engTitle
			item.AltTitles = alts
		}
		return
	}

	// Series or Anime
	det, err := GetTMDBShowDetails(ctx, item.TMDBID)
	if err != nil || det == nil {
		return
	}
	var libSeasons []config.LibrarySeason
	for _, s := range det.Seasons {
		if s.SeasonNumber <= 0 || s.EpisodeCount <= 0 {
			continue
		}
		libSeasons = append(libSeasons, config.LibrarySeason{
			SeasonNumber: s.SeasonNumber,
			EpisodeCount: s.EpisodeCount,
		})
	}
	config.Library().UpdateMetadata(item.ID, det.EnglishName, det.AltTitles, libSeasons)
	item.EnglishTitle = det.EnglishName
	item.AltTitles = det.AltTitles
	item.Seasons = libSeasons
}

// GetTMDBSeasonEpisodes retrieves all episodes for a specific season.
func GetTMDBSeasonEpisodes(ctx context.Context, tmdbID int, seasonNumber int) ([]TMDBEpisode, error) {
	if tmdbID <= 0 || seasonNumber < 0 {
		return nil, fmt.Errorf("invalid tmdbID or season: %d, S%d", tmdbID, seasonNumber)
	}

	cacheKey := fmt.Sprintf("%d:%d", tmdbID, seasonNumber)
	tmdbCacheMu.RLock()
	if c, ok := tmdbEpisodeCache[cacheKey]; ok && time.Now().Before(c.expiresAt) {
		tmdbCacheMu.RUnlock()
		return c.data, nil
	}
	tmdbCacheMu.RUnlock()

	u := fmt.Sprintf("https://api.themoviedb.org/3/tv/%d/season/%d?api_key=%s&language=tr-TR", tmdbID, seasonNumber, config.DefaultTMDBAPIKey)
	var res struct {
		Episodes []TMDBEpisode `json:"episodes"`
	}
	if err := FetchJSON(ctx, u, nil, &res); err != nil {
		return nil, err
	}

	tmdbCacheMu.Lock()
	tmdbEpisodeCache[cacheKey] = cachedTMDBEpisodes{
		data:      res.Episodes,
		expiresAt: time.Now().Add(6 * time.Hour),
	}
	tmdbCacheMu.Unlock()

	return res.Episodes, nil
}

// SearchTMDBUnified searches TMDB for movies and series, classifying anime automatically.
func SearchTMDBUnified(ctx context.Context, query string) ([]TMDBSearchResult, error) {
	cleanQuery := strings.TrimSpace(query)
	if cleanQuery == "" {
		return nil, nil
	}

	var results []TMDBSearchResult
	var mu sync.Mutex
	var wg sync.WaitGroup

	// Search Movies
	wg.Add(1)
	go func() {
		defer wg.Done()
		u := fmt.Sprintf("https://api.themoviedb.org/3/search/movie?api_key=%s&language=tr-TR&query=%s", config.DefaultTMDBAPIKey, url.QueryEscape(cleanQuery))
		var res struct {
			Results []struct {
				ID          int     `json:"id"`
				Title       string  `json:"title"`
				OrigTitle   string  `json:"original_title"`
				ReleaseDate string  `json:"release_date"`
				PosterPath  string  `json:"poster_path"`
				BackdropPath string `json:"backdrop_path"`
				Overview    string  `json:"overview"`
				VoteAverage float64 `json:"vote_average"`
				GenreIDs    []int   `json:"genre_ids"`
			} `json:"results"`
		}
		if err := FetchJSON(ctx, u, nil, &res); err == nil {
			var list []TMDBSearchResult
			for _, r := range res.Results {
				title := r.Title
				if title == "" {
					title = r.OrigTitle
				}
				if title == "" {
					continue
				}
				poster := ""
				if r.PosterPath != "" {
					poster = "https://image.tmdb.org/t/p/w500" + r.PosterPath
				}
				backdrop := ""
				if r.BackdropPath != "" {
					backdrop = "https://image.tmdb.org/t/p/w1280" + r.BackdropPath
				}
				list = append(list, TMDBSearchResult{
					ID:            r.ID,
					Title:         title,
					OriginalTitle: r.OrigTitle,
					Year:          ExtractYear(r.ReleaseDate),
					Type:          "movie",
					PosterURL:     poster,
					BackdropURL:   backdrop,
					Overview:      r.Overview,
					Rating:        r.VoteAverage,
				})
			}
			mu.Lock()
			results = append(results, list...)
			mu.Unlock()
		}
	}()

	// Search TV Shows & Anime
	wg.Add(1)
	go func() {
		defer wg.Done()
		u := fmt.Sprintf("https://api.themoviedb.org/3/search/tv?api_key=%s&language=tr-TR&query=%s", config.DefaultTMDBAPIKey, url.QueryEscape(cleanQuery))
		var res struct {
			Results []struct {
				ID            int      `json:"id"`
				Name          string   `json:"name"`
				OrigName      string   `json:"original_name"`
				FirstAirDate  string   `json:"first_air_date"`
				PosterPath    string   `json:"poster_path"`
				BackdropPath  string   `json:"backdrop_path"`
				Overview      string   `json:"overview"`
				VoteAverage   float64  `json:"vote_average"`
				OriginCountry []string `json:"origin_country"`
				GenreIDs      []int    `json:"genre_ids"`
			} `json:"results"`
		}
		if err := FetchJSON(ctx, u, nil, &res); err == nil {
			var list []TMDBSearchResult
			for _, r := range res.Results {
				title := r.Name
				if title == "" {
					title = r.OrigName
				}
				if title == "" {
					continue
				}
				poster := ""
				if r.PosterPath != "" {
					poster = "https://image.tmdb.org/t/p/w500" + r.PosterPath
				}
				backdrop := ""
				if r.BackdropPath != "" {
					backdrop = "https://image.tmdb.org/t/p/w1280" + r.BackdropPath
				}

				// Check if Anime
				isJP := false
				for _, c := range r.OriginCountry {
					if strings.EqualFold(c, "JP") {
						isJP = true
						break
					}
				}
				isAnim := false
				for _, gid := range r.GenreIDs {
					if gid == 16 {
						isAnim = true
						break
					}
				}
				mType := "series"
				if isJP && isAnim {
					mType = "anime"
				}

				list = append(list, TMDBSearchResult{
					ID:            r.ID,
					Title:         title,
					OriginalTitle: r.OrigName,
					Year:          ExtractYear(r.FirstAirDate),
					Type:          mType,
					PosterURL:     poster,
					BackdropURL:   backdrop,
					Overview:      r.Overview,
					Rating:        r.VoteAverage,
				})
			}
			mu.Lock()
			results = append(results, list...)
			mu.Unlock()
		}
	}()

	wg.Wait()
	return results, nil
}
