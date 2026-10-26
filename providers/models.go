package providers

import (
	"strings"
)

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

// AudioType represents whether the audio is Dubbed, Subtitled (Original), English Dubbed, etc.
type AudioType string

const (
	AudioTypeUnknown  AudioType = ""
	AudioTypeSub      AudioType = "sub"      // Orijinal ses + Türkçe Altyazı
	AudioTypeDub      AudioType = "dub"      // Türkçe Dublaj
	AudioTypeOriginal AudioType = "original" // Orijinal Ses
	AudioTypeEnDub    AudioType = "en_dub"   // İngilizce Dublaj
)

// StreamSource represents a resolved video stream candidate.
type StreamSource struct {
	Name      string            `json:"name"`
	Title     string            `json:"title"`
	URL       string            `json:"url"`
	Quality   string            `json:"quality"` // e.g. "2160p", "1080p", "720p", "480p", "Auto"
	Format    string            `json:"format"`  // "hls", "mp4", "mkv"
	IsHLS     bool              `json:"is_hls"`
	AudioType AudioType         `json:"audio_type,omitempty"`
	AudioLang string            `json:"audio_lang,omitempty"` // e.g., "tr", "ja", "en"
	Headers   map[string]string `json:"headers,omitempty"`
	Subtitles []Subtitle        `json:"subtitles,omitempty"`
}

// StreamResult holds the best selected stream along with all available sources and subtitles.
type StreamResult struct {
	Primary    StreamSource   `json:"primary"`
	Alternates []StreamSource `json:"alternates,omitempty"`
	Subtitles  []Subtitle     `json:"subtitles,omitempty"`
}

// NormalizeQuality converts various quality representations into standard labels.
func NormalizeQuality(quality string) string {
	ql := strings.ToLower(strings.TrimSpace(quality))
	switch {
	case strings.Contains(ql, "4k") || strings.Contains(ql, "2160") || strings.Contains(ql, "uhd"):
		return "4K UHD"
	case strings.Contains(ql, "1440") || strings.Contains(ql, "2k") || strings.Contains(ql, "qhd"):
		return "2K QHD"
	case strings.Contains(ql, "1080") || strings.Contains(ql, "fhd"):
		return "1080p"
	case strings.Contains(ql, "720") || strings.Contains(ql, "hd"):
		return "720p"
	case strings.Contains(ql, "540"):
		return "540p"
	case strings.Contains(ql, "480") || strings.Contains(ql, "sd"):
		return "480p"
	case strings.Contains(ql, "360"):
		return "360p"
	case strings.Contains(ql, "240"):
		return "240p"
	default:
		if quality != "" {
			return quality
		}
		return "Auto"
	}
}

// ParseResolutionDetails returns width, height, estimated bandwidth (bps) and normalized label.
func ParseResolutionDetails(quality string) (width, height, bandwidth int, label string) {
	norm := NormalizeQuality(quality)
	switch norm {
	case "4K UHD":
		return 3840, 2160, 14000000, "4K UHD"
	case "2K QHD":
		return 2560, 1440, 8000000, "2K QHD"
	case "1080p":
		return 1920, 1080, 4500000, "1080p"
	case "720p":
		return 1280, 720, 2400000, "720p"
	case "540p":
		return 960, 540, 1500000, "540p"
	case "480p":
		return 854, 480, 1000000, "480p"
	case "360p":
		return 640, 360, 600000, "360p"
	case "240p":
		return 426, 240, 350000, "240p"
	default:
		return 1920, 1080, 3000000, "Auto"
	}
}
