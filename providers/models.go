package providers

// MediaType represents the type of media item.
type MediaType string

const (
	MediaTypeMovie  MediaType = "movie"
	MediaTypeSeries MediaType = "series"
	MediaTypeAnime  MediaType = "anime"
)

// MediaItem represents a catalog or search result item.
type MediaItem struct {
	ID          string    `json:"id"`
	Title       string    `json:"title"`
	Year        string    `json:"year,omitempty"`
	Type        MediaType `json:"type"`
	PosterURL   string    `json:"poster_url,omitempty"`
	Description string    `json:"description,omitempty"`
}

// Season represents a season of a TV series or anime.
type Season struct {
	Number   int       `json:"number"`
	Name     string    `json:"name,omitempty"`
	Episodes []Episode `json:"episodes,omitempty"`
}

// Episode represents a single episode or movie playable entry.
type Episode struct {
	ID        string     `json:"id"`
	StreamID  string     `json:"stream_id"`
	Title     string     `json:"title"`
	Season    int        `json:"season"`
	Number    int        `json:"number"`
	Subtitles []Subtitle `json:"subtitles,omitempty"`
}

// MediaDetail represents full metadata and season breakdown for a MediaItem.
type MediaDetail struct {
	MediaItem
	Seasons   []Season   `json:"seasons,omitempty"`
	StreamID  string     `json:"stream_id,omitempty"`
	Subtitles []Subtitle `json:"subtitles,omitempty"`
}

// Subtitle represents an external subtitle track (.vtt or .srt).
type Subtitle struct {
	ID       string            `json:"id"`
	Language string            `json:"language"` // e.g., "tr", "en"
	Label    string            `json:"label"`    // e.g., "Türkçe", "English"
	Format   string            `json:"format"`   // "vtt" or "srt"
	URL      string            `json:"url"`
	Headers  map[string]string `json:"headers,omitempty"`
}

// StreamSource represents a resolved video stream candidate.
type StreamSource struct {
	Name      string            `json:"name"`
	Title     string            `json:"title"`
	URL       string            `json:"url"`
	Quality   string            `json:"quality"` // e.g. "2160p", "1080p", "720p", "480p", "Auto"
	Format    string            `json:"format"`  // "hls", "mp4", "mkv"
	IsHLS     bool              `json:"is_hls"`
	Headers   map[string]string `json:"headers,omitempty"`
	Subtitles []Subtitle        `json:"subtitles,omitempty"`
}

// StreamResult holds the best selected stream along with all available sources and subtitles.
type StreamResult struct {
	Primary    StreamSource   `json:"primary"`
	Alternates []StreamSource `json:"alternates,omitempty"`
	Subtitles  []Subtitle     `json:"subtitles,omitempty"`
}
