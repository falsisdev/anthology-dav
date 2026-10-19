package web

import (
	_ "embed"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"

	"anthology-dav/config"
	"anthology-dav/providers"
	"anthology-dav/stream"
	"anthology-dav/vfs"
)

//go:embed assets/logo.png
var anthologyLogoPNG []byte

//go:embed assets/favicon.ico
var anthologyFaviconICO []byte

// Handler serves the SwiftUI Glassmorphism Web UI and its JSON APIs.
type Handler struct {
	cfg      *config.Config
	cache    *vfs.MemoryCache
	resolver *stream.MultiResolver
}

// NewHandler creates a new Web UI Handler.
func NewHandler(cfg *config.Config, cache *vfs.MemoryCache, resolver *stream.MultiResolver) *Handler {
	return &Handler{
		cfg:      cfg,
		cache:    cache,
		resolver: resolver,
	}
}

// ServeHTTP handles root "/" requests, static embedded assets or routes to APIs.
func (h *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path == "/" {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(glassmorphismHTML))
		return
	}
	if r.URL.Path == "/logo.png" || r.URL.Path == "/assets/logo.png" {
		w.Header().Set("Content-Type", "image/png")
		w.Header().Set("Cache-Control", "public, max-age=86400")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(anthologyLogoPNG)
		return
	}
	if r.URL.Path == "/favicon.ico" || r.URL.Path == "/assets/favicon.ico" {
		w.Header().Set("Content-Type", "image/x-icon")
		w.Header().Set("Cache-Control", "public, max-age=86400")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(anthologyFaviconICO)
		return
	}
	if strings.HasPrefix(r.URL.Path, "/api/") {
		h.HandleAPI(w, r)
		return
	}
	http.NotFound(w, r)
}

// HandleAPI routes /api/... endpoints.
func (h *Handler) HandleAPI(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Access-Control-Allow-Origin", "*")
	w.Header().Set("Access-Control-Allow-Methods", "GET, POST, OPTIONS, DELETE")
	w.Header().Set("Access-Control-Allow-Headers", "Content-Type")

	if r.Method == http.MethodOptions {
		w.WriteHeader(http.StatusOK)
		return
	}

	path := strings.TrimPrefix(r.URL.Path, "/api")
	switch {
	case path == "/search":
		h.handleSearch(w, r)
	case path == "/sources":
		h.handleDiscoverSources(w, r)
	case path == "/library" && r.Method == http.MethodGet:
		h.handleLibraryList(w, r)
	case path == "/library/add" && r.Method == http.MethodPost:
		h.handleLibraryAdd(w, r)
	case (path == "/library/remove" && r.Method == http.MethodPost) || (path == "/library" && r.Method == http.MethodDelete):
		h.handleLibraryRemove(w, r)
	case path == "/library/provider" && r.Method == http.MethodPost:
		h.handleLibraryProvider(w, r)
	case path == "/providers":
		h.handleProviders(w, r)
	case path == "/status":
		h.handleStatus(w, r)
	default:
		http.Error(w, `{"error":"endpoint not found"}`, http.StatusNotFound)
	}
}

func (h *Handler) handleSearch(w http.ResponseWriter, r *http.Request) {
	query := strings.TrimSpace(r.URL.Query().Get("q"))
	if query == "" {
		_ = json.NewEncoder(w).Encode([]providers.TMDBSearchResult{})
		return
	}

	results, err := providers.SearchTMDBUnified(r.Context(), query)
	if err != nil {
		http.Error(w, fmt.Sprintf(`{"error":"%s"}`, err.Error()), http.StatusInternalServerError)
		return
	}
	_ = json.NewEncoder(w).Encode(results)
}

func (h *Handler) handleDiscoverSources(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	mediaType := strings.ToLower(strings.TrimSpace(q.Get("type")))
	tmdbID, _ := strconv.Atoi(q.Get("tmdb_id"))
	title := strings.TrimSpace(q.Get("title"))
	origTitle := strings.TrimSpace(q.Get("original_title"))
	year := strings.TrimSpace(q.Get("year"))
	season, _ := strconv.Atoi(strings.TrimSpace(q.Get("season")))
	episode, _ := strconv.Atoi(strings.TrimSpace(q.Get("episode")))

	if season <= 0 {
		season = 1
	}
	if episode <= 0 {
		episode = 1
	}

	sources := h.resolver.DiscoverSources(r.Context(), mediaType, tmdbID, title, origTitle, year, season, episode)
	_ = json.NewEncoder(w).Encode(map[string]any{
		"status":  "ok",
		"sources": sources,
	})
}

func (h *Handler) handleLibraryList(w http.ResponseWriter, r *http.Request) {
	items := config.Library().All()
	_ = json.NewEncoder(w).Encode(items)
}

func (h *Handler) handleLibraryAdd(w http.ResponseWriter, r *http.Request) {
	var item config.LibraryItem
	if err := json.NewDecoder(r.Body).Decode(&item); err != nil {
		http.Error(w, `{"error":"invalid json body"}`, http.StatusBadRequest)
		return
	}
	if item.Title == "" || item.TMDBID <= 0 {
		http.Error(w, `{"error":"missing title or tmdb_id"}`, http.StatusBadRequest)
		return
	}
	if item.Type == "" {
		item.Type = "movie"
	}

	if err := config.Library().Add(item); err != nil {
		http.Error(w, fmt.Sprintf(`{"error":"%s"}`, err.Error()), http.StatusInternalServerError)
		return
	}

	if saved, ok := config.Library().GetByTMDB(item.Type, item.TMDBID); ok {
		providers.PrewarmLibraryItemMetadata(r.Context(), &saved)
		item = saved
	}

	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(map[string]any{"status": "ok", "item": item})
}

func (h *Handler) handleLibraryRemove(w http.ResponseWriter, r *http.Request) {
	var body struct {
		ID string `json:"id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.ID == "" {
		// Try query param
		body.ID = r.URL.Query().Get("id")
	}
	if body.ID == "" {
		http.Error(w, `{"error":"missing id"}`, http.StatusBadRequest)
		return
	}

	if err := config.Library().Remove(body.ID); err != nil {
		http.Error(w, fmt.Sprintf(`{"error":"%s"}`, err.Error()), http.StatusInternalServerError)
		return
	}

	_ = json.NewEncoder(w).Encode(map[string]string{"status": "ok", "removed": body.ID})
}

func (h *Handler) handleLibraryProvider(w http.ResponseWriter, r *http.Request) {
	var body struct {
		ID       string `json:"id"`
		Provider string `json:"provider"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.ID == "" {
		http.Error(w, `{"error":"missing id or provider"}`, http.StatusBadRequest)
		return
	}

	if err := config.Library().UpdateProvider(body.ID, body.Provider); err != nil {
		http.Error(w, fmt.Sprintf(`{"error":"%s"}`, err.Error()), http.StatusInternalServerError)
		return
	}

	_ = json.NewEncoder(w).Encode(map[string]string{"status": "ok", "provider": body.Provider})
}

func (h *Handler) handleProviders(w http.ResponseWriter, r *http.Request) {
	all := providers.All()
	type ProvInfo struct {
		ID       string `json:"id"`
		Name     string `json:"name"`
		Title    string `json:"title"`
		Category string `json:"category"`
		Icon     string `json:"icon"`
		Domain   string `json:"domain"`
	}
	var list []ProvInfo
	for _, p := range all {
		id := strings.ToLower(p.ID())
		cat := "Genel"
		cleanTitle := p.Name()
		icon := "⚡"
		domain := ""

		switch id {
		// Anime
		case "animecix":
			cat, cleanTitle, icon, domain = "Anime", "AnimeciX", "⛩️", "animecix.tv"
		case "anizium":
			cat, cleanTitle, icon, domain = "Anime", "Anizium", "⛩️", "anizium.co"
		case "sonanime":
			cat, cleanTitle, icon, domain = "Anime", "SonAnime", "⛩️", "sonanime.com"
		case "animom":
			cat, cleanTitle, icon, domain = "Anime", "AniMom", "⛩️", "animom.org"
		case "anizm":
			cat, cleanTitle, icon, domain = "Anime", "Anizm", "⛩️", "anizm.net"
		case "seicode":
			cat, cleanTitle, icon, domain = "Anime", "SeiCode", "⛩️", "seicode.net"
		case "asyaanimeleri":
			cat, cleanTitle, icon, domain = "Anime", "AsyaAnimeleri", "⛩️", "asyaanimeleri.top"
		case "animeprax":
			cat, cleanTitle, icon, domain = "Anime", "AnimePrax", "⛩️", "animeprax.com"

		// Diziler
		case "diziwatch":
			cat, cleanTitle, icon, domain = "Dizi", "DiziWatch", "📺", "diziwatch.ac"
		case "diziboxizle":
			cat, cleanTitle, icon, domain = "Dizi", "DiziBox", "📺", "diziboxizle.com"
		case "diziyou":
			cat, cleanTitle, icon, domain = "Dizi", "DiziYou", "📺", "diziyou.one"
		case "dizimom":
			cat, cleanTitle, icon, domain = "Dizi", "DiziMom", "📺", "dizimom.diy"
		case "dizipod":
			cat, cleanTitle, icon, domain = "Dizi", "DiziPod", "📺", "dizipod.com"
		case "dizibak":
			cat, cleanTitle, icon, domain = "Dizi", "DiziBak", "📺", "dizibak.net"
		case "yabancidizi":
			cat, cleanTitle, icon, domain = "Dizi", "Yabancı Dizi", "📺", "yabancidizi.news"
		case "ddizi":
			cat, cleanTitle, icon, domain = "Dizi", "DDizi", "📺", "ddizi.im"
		case "puhutv":
			cat, cleanTitle, icon, domain = "Dizi", "PuhuTV", "📺", "puhutv.com"
		case "trdiziizle":
			cat, cleanTitle, icon, domain = "Dizi", "TrDiziİzle", "📺", "trdiziizle.tv"
		case "tvdiziler":
			cat, cleanTitle, icon, domain = "Dizi", "TvDiziler", "📺", "tvdiziler.tv"
		case "anthology_diziler":
			cat, cleanTitle, icon, domain = "Dizi", "Anthology Dizi Arşivi", "📁", "github.com/falsisdev/anthology"

		// Filmler
		case "filmmodu":
			cat, cleanTitle, icon, domain = "Film", "FilmModu", "🎬", "filmmodu.one"
		case "hdfilmcehennemi":
			cat, cleanTitle, icon, domain = "Film", "HDFilmCehennemi", "🎬", "hdfilmcehennemi.nl"
		case "hdfilmizle":
			cat, cleanTitle, icon, domain = "Film", "HDFilmİzle", "🎬", "hdfilmizle.vip"
		case "hdfilmdelisi":
			cat, cleanTitle, icon, domain = "Film", "HDFilmDelisi", "🎬", "hdfilmdelisi.one"
		case "kultfilmler":
			cat, cleanTitle, icon, domain = "Film", "Kült Filmler", "🎬", "kultfilmler.net"
		case "liderfilmizle":
			cat, cleanTitle, icon, domain = "Film", "LiderFilmİzle", "🎬", "liderfilmizle.vip"
		case "setfilmizle":
			cat, cleanTitle, icon, domain = "Film", "SetFilmİzle", "🎬", "setfilmizle.ltd"
		case "sinemacx":
			cat, cleanTitle, icon, domain = "Film", "SinemaCX", "🎬", "sinema.gg"
		case "sinewix":
			cat, cleanTitle, icon, domain = "Film", "SineWix", "🎬", "sinewix.app"
		case "vidlink":
			cat, cleanTitle, icon, domain = "Film", "VidLink Cloud", "☁️", "vidlink.pro"
		case "vidmody":
			cat, cleanTitle, icon, domain = "Film", "Vidmody", "🎬", "vidmody.com"
		case "webteizle":
			cat, cleanTitle, icon, domain = "Film", "Webteİzle", "🎬", "webteizle.info"
		case "youtube_dizifilm":
			cat, cleanTitle, icon, domain = "Film", "YouTube Dizi & Film", "▶️", "youtube.com"
		case "anthology_m3u":
			cat, cleanTitle, icon, domain = "Film", "Anthology M3U Arşivi", "📁", "github.com/falsisdev/anthology"
		default:
			cat, cleanTitle, icon, domain = "Diğer", p.Name(), "⚡", ""
		}

		list = append(list, ProvInfo{
			ID:       p.ID(),
			Name:     p.Name(),
			Title:    cleanTitle,
			Category: cat,
			Icon:     icon,
			Domain:   domain,
		})
	}
	_ = json.NewEncoder(w).Encode(list)
}

func (h *Handler) handleStatus(w http.ResponseWriter, r *http.Request) {
	libCount := len(config.Library().All())
	_ = json.NewEncoder(w).Encode(map[string]any{
		"status":          "ok",
		"version":         "2.0-glass",
		"apple_silicon":   true,
		"providers_count": providers.Count(),
		"library_count":   libCount,
		"webdav_root":     h.cfg.BaseURL + "/dav/",
	})
}

// ── SwiftUI Glassmorphism Single Page App (Embedded HTML/CSS/JS) ─────

const glassmorphismHTML = `<!DOCTYPE html>
<html lang="tr">
<head>
  <meta charset="UTF-8">
  <meta name="viewport" content="width=device-width, initial-scale=1.0, viewport-fit=cover">
  <title>Anthology WebDAV</title>
  <link rel="icon" type="image/x-icon" href="/favicon.ico">
  <link rel="preconnect" href="https://fonts.googleapis.com">
  <link rel="preconnect" href="https://fonts.gstatic.com" crossorigin>
  <link href="https://fonts.googleapis.com/css2?family=SF+Pro+Display:wght@300;400;500;600;700&family=SF+Pro+Text:wght@400;500;600&display=swap" rel="stylesheet">
  <style>
    :root {
      --font-display: -apple-system, BlinkMacSystemFont, "SF Pro Display", "SF Pro Text", system-ui, sans-serif;
      --bg-acrylic: rgba(18, 22, 31, 0.76);
      --glass-panel: rgba(255, 255, 255, 0.05);
      --glass-panel-hover: rgba(255, 255, 255, 0.09);
      --glass-border: rgba(255, 255, 255, 0.12);
      --glass-border-subtle: rgba(255, 255, 255, 0.07);
      --accent-blue: #0A84FF;
      --accent-purple: #BF5AF2;
      --accent-pink: #FF375F;
      --accent-orange: #FF9F0A;
      --accent-green: #30D158;
      --accent-cyan: #64D2FF;
      --text-primary: #FFFFFF;
      --text-secondary: rgba(255, 255, 255, 0.65);
      --text-tertiary: rgba(255, 255, 255, 0.42);
      --spring-curve: cubic-bezier(0.16, 1, 0.3, 1);
    }

    * {
      box-sizing: border-box;
      margin: 0;
      padding: 0;
      -webkit-font-smoothing: antialiased;
    }

    body {
      font-family: var(--font-display);
      background: #090B10;
      color: var(--text-primary);
      min-height: 100vh;
      overflow-x: hidden;
      display: flex;
      flex-direction: column;
      position: relative;
    }

    /* Modern SwiftUI App Root Container */
    .app-container {
      position: relative;
      width: 100%;
      min-height: 100vh;
      display: flex;
      flex-direction: column;
      z-index: 1;
    }

    /* SwiftUI Translucent Glass Navigation Bar */
    .app-header {
      position: sticky;
      top: 0;
      z-index: 100;
      background: rgba(14, 17, 24, 0.75);
      backdrop-filter: blur(35px) saturate(190%);
      -webkit-backdrop-filter: blur(35px) saturate(190%);
      border-bottom: 1px solid var(--glass-border-subtle);
      padding: 0 32px;
      height: 68px;
      display: flex;
      align-items: center;
      justify-content: space-between;
      gap: 20px;
    }

    .app-brand {
      display: flex;
      align-items: center;
      gap: 12px;
      user-select: none;
    }

    .app-brand-logo {
      width: 36px;
      height: 36px;
      object-fit: contain;
      filter: drop-shadow(0 2px 8px rgba(10, 132, 255, 0.4));
    }

    .app-brand-text {
      font-size: 16px;
      font-weight: 700;
      letter-spacing: -0.3px;
      color: #FFFFFF;
    }

    .status-badge {
      display: inline-flex;
      align-items: center;
      gap: 6px;
      padding: 4px 10px;
      background: rgba(48, 209, 88, 0.14);
      border: 1px solid rgba(48, 209, 88, 0.3);
      border-radius: 20px;
      font-size: 11px;
      font-weight: 500;
      color: #30D158;
    }

    .status-dot {
      width: 6px;
      height: 6px;
      border-radius: 50%;
      background: #30D158;
      box-shadow: 0 0 8px #30D158;
      animation: pulse 2s infinite;
    }

    @keyframes pulse {
      0%, 100% { opacity: 1; transform: scale(1); }
      50% { opacity: 0.5; transform: scale(0.85); }
    }

    .webdav-pill {
      font-size: 12px;
      color: var(--text-secondary);
      background: rgba(255, 255, 255, 0.06);
      padding: 6px 14px;
      border-radius: 12px;
      border: 1px solid var(--glass-border-subtle);
      display: flex;
      align-items: center;
      gap: 8px;
    }

    /* SwiftUI Segmented Navigation Bar */
    .segmented-control {
      display: flex;
      background: rgba(0, 0, 0, 0.4);
      border: 1px solid var(--glass-border-subtle);
      border-radius: 14px;
      padding: 4px;
      gap: 4px;
      box-shadow: inset 0 1px 3px rgba(0,0,0,0.5);
    }

    .segment-btn {
      background: transparent;
      border: none;
      color: var(--text-secondary);
      padding: 7px 18px;
      border-radius: 10px;
      font-size: 13px;
      font-weight: 500;
      font-family: inherit;
      cursor: pointer;
      display: flex;
      align-items: center;
      gap: 8px;
      transition: all 0.2s var(--spring-curve);
    }

    .segment-btn:hover {
      color: var(--text-primary);
    }

    .segment-btn.active {
      background: rgba(255, 255, 255, 0.14);
      color: #FFFFFF;
      box-shadow: 0 4px 12px rgba(0, 0, 0, 0.35), inset 0 1px 1px rgba(255, 255, 255, 0.25);
      font-weight: 600;
    }

    /* Main Content Area */
    .content-area {
      flex: 1;
      width: 100%;
      max-width: 1400px;
      margin: 0 auto;
      padding: 28px 32px 64px;
    }

    /* Spotlight Search Bar */
    .search-wrapper {
      position: relative;
      max-width: 680px;
      margin: 10px auto 32px;
    }

    .search-input {
      width: 100%;
      height: 56px;
      padding: 0 54px 0 54px;
      background: rgba(255, 255, 255, 0.06);
      border: 1px solid var(--glass-border);
      border-radius: 18px;
      font-size: 17px;
      font-family: inherit;
      color: #FFFFFF;
      outline: none;
      backdrop-filter: blur(20px);
      box-shadow: 0 10px 30px -5px rgba(0,0,0,0.5), inset 0 1px 1px rgba(255,255,255,0.15);
      transition: all 0.25s var(--spring-curve);
    }

    .search-input:focus {
      background: rgba(255, 255, 255, 0.09);
      border-color: var(--accent-blue);
      box-shadow: 0 12px 40px -5px rgba(10, 132, 255, 0.3), inset 0 1px 1px rgba(255,255,255,0.25);
    }

    .search-icon {
      position: absolute;
      left: 20px;
      top: 50%;
      transform: translateY(-50%);
      color: var(--text-tertiary);
      pointer-events: none;
    }

    .search-shortcut {
      position: absolute;
      right: 18px;
      top: 50%;
      transform: translateY(-50%);
      font-size: 11px;
      background: rgba(255, 255, 255, 0.1);
      padding: 4px 8px;
      border-radius: 6px;
      color: var(--text-tertiary);
      border: 1px solid var(--glass-border-subtle);
    }

    /* Section Headers */
    .section-header {
      display: flex;
      align-items: center;
      justify-content: space-between;
      margin-bottom: 20px;
    }

    .section-title {
      font-size: 20px;
      font-weight: 700;
      letter-spacing: -0.4px;
    }

    .filter-pills {
      display: flex;
      gap: 8px;
    }

    .filter-pill {
      background: rgba(255, 255, 255, 0.05);
      border: 1px solid var(--glass-border-subtle);
      color: var(--text-secondary);
      padding: 6px 14px;
      border-radius: 12px;
      font-size: 12px;
      font-family: inherit;
      cursor: pointer;
      transition: all 0.2s var(--spring-curve);
    }

    .filter-pill.active {
      background: var(--accent-blue);
      color: white;
      border-color: transparent;
      box-shadow: 0 4px 14px rgba(10, 132, 255, 0.4);
    }

    /* Media Cards Grid */
    .media-grid {
      display: grid;
      grid-template-columns: repeat(auto-fill, minmax(210px, 1fr));
      gap: 22px;
    }

    .media-card {
      background: var(--glass-panel);
      border: 1px solid var(--glass-border-subtle);
      border-radius: 18px;
      overflow: hidden;
      display: flex;
      flex-direction: column;
      position: relative;
      transition: all 0.3s var(--spring-curve);
      box-shadow: 0 10px 24px -5px rgba(0, 0, 0, 0.4);
    }

    .media-card:hover {
      transform: translateY(-6px) scale(1.02);
      background: var(--glass-panel-hover);
      border-color: var(--glass-border);
      box-shadow: 0 20px 40px -10px rgba(0, 0, 0, 0.75);
    }

    .poster-box {
      width: 100%;
      height: 295px;
      background: #14171E;
      position: relative;
      overflow: hidden;
    }

    .poster-img {
      width: 100%;
      height: 100%;
      object-fit: cover;
      transition: transform 0.4s var(--spring-curve);
    }

    .media-card:hover .poster-img {
      transform: scale(1.05);
    }

    .badge-type {
      position: absolute;
      top: 10px;
      left: 10px;
      padding: 4px 9px;
      border-radius: 8px;
      font-size: 10px;
      font-weight: 700;
      text-transform: uppercase;
      letter-spacing: 0.5px;
      backdrop-filter: blur(12px);
    }

    .badge-movie { background: rgba(10, 132, 255, 0.75); color: #FFF; }
    .badge-series { background: rgba(191, 90, 242, 0.75); color: #FFF; }
    .badge-anime { background: rgba(255, 55, 95, 0.75); color: #FFF; }

    .rating-badge {
      position: absolute;
      top: 10px;
      right: 10px;
      background: rgba(0, 0, 0, 0.65);
      border: 1px solid rgba(255, 255, 255, 0.15);
      backdrop-filter: blur(12px);
      padding: 3px 8px;
      border-radius: 8px;
      font-size: 11px;
      font-weight: 600;
      color: #FFD60A;
      display: flex;
      align-items: center;
      gap: 3px;
    }

    .card-body {
      padding: 14px;
      display: flex;
      flex-direction: column;
      flex: 1;
      justify-content: space-between;
    }

    .media-title {
      font-size: 14px;
      font-weight: 600;
      color: #FFFFFF;
      white-space: nowrap;
      overflow: hidden;
      text-overflow: ellipsis;
      margin-bottom: 4px;
    }

    .media-meta {
      font-size: 12px;
      color: var(--text-secondary);
      margin-bottom: 12px;
      display: flex;
      align-items: center;
      gap: 6px;
    }

    /* Glass Buttons */
    .btn-action {
      width: 100%;
      height: 36px;
      background: rgba(255, 255, 255, 0.08);
      border: 1px solid var(--glass-border);
      border-radius: 10px;
      color: #FFFFFF;
      font-size: 12px;
      font-weight: 600;
      font-family: inherit;
      cursor: pointer;
      display: flex;
      align-items: center;
      justify-content: center;
      gap: 6px;
      transition: all 0.2s var(--spring-curve);
    }

    .btn-action:hover {
      background: var(--accent-blue);
      border-color: transparent;
      box-shadow: 0 4px 16px rgba(10, 132, 255, 0.4);
    }

    .btn-added {
      background: rgba(48, 209, 88, 0.18);
      border-color: rgba(48, 209, 88, 0.35);
      color: #30D158;
      cursor: default;
    }

    .btn-delete {
      background: rgba(255, 69, 58, 0.15);
      border-color: rgba(255, 69, 58, 0.3);
      color: #FF453A;
      margin-top: 8px;
    }
    .btn-delete:hover {
      background: #FF453A;
      color: white;
      box-shadow: 0 4px 16px rgba(255, 69, 58, 0.4);
    }

    .provider-select {
      width: 100%;
      height: 32px;
      background: rgba(0, 0, 0, 0.4);
      border: 1px solid var(--glass-border-subtle);
      border-radius: 8px;
      color: var(--text-secondary);
      font-size: 11px;
      font-family: inherit;
      padding: 0 8px;
      margin-top: 6px;
      outline: none;
    }

    /* Toast Alert */
    .toast {
      position: fixed;
      top: 24px;
      left: 50%;
      transform: translateX(-50%) translateY(-100px);
      background: rgba(30, 35, 45, 0.9);
      backdrop-filter: blur(25px);
      border: 1px solid var(--glass-border);
      padding: 12px 24px;
      border-radius: 16px;
      color: #FFFFFF;
      font-size: 13px;
      font-weight: 500;
      box-shadow: 0 15px 40px rgba(0,0,0,0.6);
      display: flex;
      align-items: center;
      gap: 10px;
      z-index: 999;
      transition: transform 0.4s var(--spring-curve);
    }

    .toast.show {
      transform: translateX(-50%) translateY(0);
    }

    /* Providers Grid */
    .providers-grid {
      display: grid;
      grid-template-columns: repeat(auto-fill, minmax(290px, 1fr));
      gap: 14px;
    }

    .provider-card {
      background: var(--glass-panel);
      border: 1px solid var(--glass-border-subtle);
      border-radius: 16px;
      padding: 14px 16px;
      display: flex;
      align-items: center;
      gap: 14px;
      min-width: 0;
      position: relative;
      overflow: hidden;
      transition: all 0.25s var(--spring-curve);
    }

    .provider-card:hover {
      background: var(--glass-panel-hover);
      border-color: var(--glass-border);
      transform: translateY(-2px);
      box-shadow: 0 10px 24px -6px rgba(0, 0, 0, 0.4);
    }

    .provider-logo-box {
      width: 46px;
      height: 46px;
      border-radius: 12px;
      background: rgba(255, 255, 255, 0.05);
      border: 1px solid var(--glass-border);
      display: flex;
      align-items: center;
      justify-content: center;
      flex-shrink: 0;
      overflow: hidden;
      box-shadow: inset 0 1px 1px rgba(255, 255, 255, 0.15);
    }

    .provider-logo-img {
      width: 28px;
      height: 28px;
      object-fit: contain;
      border-radius: 6px;
      display: block;
    }

    .provider-fallback-icon {
      font-size: 22px;
      line-height: 1;
      display: flex;
      align-items: center;
      justify-content: center;
    }

    .provider-info {
      flex: 1;
      min-width: 0;
      display: flex;
      flex-direction: column;
      gap: 3px;
    }

    .provider-title {
      font-size: 13.5px;
      font-weight: 600;
      color: #FFFFFF;
      white-space: nowrap;
      overflow: hidden;
      text-overflow: ellipsis;
      letter-spacing: -0.2px;
    }

    .provider-meta {
      font-size: 11.5px;
      color: var(--text-secondary);
      white-space: nowrap;
      overflow: hidden;
      text-overflow: ellipsis;
      display: flex;
      align-items: center;
      gap: 6px;
    }

    .provider-cat-tag {
      font-weight: 500;
      color: var(--accent-cyan);
    }

    .provider-status {
      flex-shrink: 0;
      margin-left: auto;
    }

    /* Empty State */
    .empty-state {
      text-align: center;
      padding: 80px 20px;
      color: var(--text-tertiary);
    }

    .empty-icon {
      font-size: 48px;
      margin-bottom: 14px;
      opacity: 0.6;
    }

    /* Source Discovery Button & Glass Modal */
    .btn-discover {
      background: rgba(100, 210, 255, 0.14);
      border-color: rgba(100, 210, 255, 0.35);
      color: var(--accent-cyan);
    }
    .btn-discover:hover {
      background: rgba(100, 210, 255, 0.25);
      border-color: var(--accent-cyan);
      color: #fff;
    }

    .modal-backdrop {
      position: fixed;
      inset: 0;
      background: rgba(6, 9, 15, 0.72);
      backdrop-filter: blur(18px);
      -webkit-backdrop-filter: blur(18px);
      z-index: 9000;
      display: none;
      align-items: center;
      justify-content: center;
      padding: 20px;
    }
    .modal-backdrop.open {
      display: flex;
    }

    .modal-glass {
      width: 100%;
      max-width: 620px;
      max-height: 85vh;
      background: rgba(22, 27, 38, 0.88);
      backdrop-filter: blur(40px) saturate(190%);
      -webkit-backdrop-filter: blur(40px) saturate(190%);
      border: 1px solid rgba(255, 255, 255, 0.16);
      border-radius: 22px;
      box-shadow: 0 28px 70px rgba(0, 0, 0, 0.75);
      display: flex;
      flex-direction: column;
      overflow: hidden;
    }

    .modal-header {
      padding: 18px 22px;
      border-bottom: 1px solid rgba(255, 255, 255, 0.08);
      display: flex;
      align-items: center;
      justify-content: space-between;
      gap: 12px;
    }
    .modal-title {
      font-size: 16px;
      font-weight: 700;
      color: #fff;
      letter-spacing: -0.3px;
    }
    .modal-close {
      background: rgba(255, 255, 255, 0.08);
      border: 1px solid rgba(255, 255, 255, 0.12);
      color: var(--text-secondary);
      width: 30px;
      height: 30px;
      border-radius: 50%;
      cursor: pointer;
      font-size: 14px;
      display: flex;
      align-items: center;
      justify-content: center;
    }
    .modal-close:hover {
      background: rgba(255, 255, 255, 0.16);
      color: #fff;
    }

    .modal-controls {
      padding: 12px 22px;
      background: rgba(0, 0, 0, 0.25);
      border-bottom: 1px solid rgba(255, 255, 255, 0.06);
      display: flex;
      align-items: center;
      gap: 10px;
      flex-wrap: wrap;
    }
    .modal-controls label {
      font-size: 12px;
      color: var(--text-secondary);
      font-weight: 500;
    }
    .modal-input {
      width: 68px;
      padding: 6px 10px;
      border-radius: 8px;
      background: rgba(255, 255, 255, 0.08);
      border: 1px solid rgba(255, 255, 255, 0.14);
      color: #fff;
      font-size: 12.5px;
      outline: none;
    }

    .modal-body {
      padding: 18px 22px;
      overflow-y: auto;
      display: flex;
      flex-direction: column;
      gap: 10px;
    }

    .source-row {
      display: flex;
      align-items: center;
      justify-content: space-between;
      gap: 12px;
      padding: 12px 15px;
      border-radius: 14px;
      background: rgba(255, 255, 255, 0.05);
      border: 1px solid rgba(255, 255, 255, 0.09);
      transition: all 0.18s ease;
    }
    .source-row:hover {
      background: rgba(255, 255, 255, 0.09);
      border-color: rgba(255, 255, 255, 0.2);
    }
    .source-row-info {
      display: flex;
      flex-direction: column;
      gap: 3px;
      min-width: 0;
    }
    .source-row-name {
      font-size: 13.5px;
      font-weight: 600;
      color: #fff;
      display: flex;
      align-items: center;
      gap: 8px;
    }
    .source-row-sub {
      font-size: 11.5px;
      color: var(--text-secondary);
      white-space: nowrap;
      overflow: hidden;
      text-overflow: ellipsis;
    }
    .source-row-actions {
      display: flex;
      align-items: center;
      gap: 8px;
      flex-shrink: 0;
    }
    .btn-mini {
      padding: 6px 11px;
      border-radius: 9px;
      font-size: 11.5px;
      font-weight: 600;
      cursor: pointer;
      border: 1px solid rgba(255, 255, 255, 0.14);
      background: rgba(255, 255, 255, 0.08);
      color: #fff;
      transition: all 0.15s ease;
    }
    .btn-mini.primary {
      background: var(--accent-blue);
      border-color: rgba(255, 255, 255, 0.25);
    }
    .btn-mini:hover {
      transform: translateY(-1px);
      filter: brightness(1.12);
    }
  </style>
</head>
<body>
  <div class="toast" id="toast">
    <span id="toastIcon">✓</span>
    <span id="toastMsg">İşlem başarıyla tamamlandı.</span>
  </div>

  <!-- Working Source Discovery Glass Modal -->
  <div class="modal-backdrop" id="sourceModal" onclick="if(event.target===this) closeSourceModal()">
    <div class="modal-glass">
      <div class="modal-header">
        <div>
          <div class="modal-title" id="modalItemTitle">Çalışan Kaynakları Bul</div>
          <div style="font-size:11.5px; color:var(--text-secondary); margin-top:2px;">Yalnızca bu içerik/bölüm için doğrulanmış çalışan kaynaklar listelenir</div>
        </div>
        <button class="modal-close" onclick="closeSourceModal()">✕</button>
      </div>
      <div class="modal-controls" id="modalEpisodeBar">
        <label>Sezon:</label>
        <input type="number" id="modalSeason" class="modal-input" value="1" min="1">
        <label>Bölüm:</label>
        <input type="number" id="modalEpisode" class="modal-input" value="1" min="1">
        <button class="btn-mini primary" onclick="scanSourcesForModal()">🔍 Şimdi Tara</button>
      </div>
      <div class="modal-body" id="modalSourceList"></div>
    </div>
  </div>

  <div class="app-container">
    <!-- SwiftUI App Navigation Header -->
    <header class="app-header">
      <div class="app-brand">
        <img class="app-brand-logo" src="/logo.png" alt="Anthology Logo">
        <span class="app-brand-text">Anthology WebDAV</span>
      </div>

      <!-- Centered Segmented Navigation -->
      <nav class="segmented-control">
        <button class="segment-btn active" onclick="switchTab('search')">
          <svg width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.5"><circle cx="11" cy="11" r="8"/><path d="m21 21-4.3-4.3"/></svg>
          Keşfet & Ara
        </button>
        <button class="segment-btn" onclick="switchTab('library')">
          <svg width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.5"><path d="m19 21-7-4-7 4V5a2 2 0 0 1 2-2h10a2 2 0 0 1 2 2v16z"/></svg>
          Kütüphanem (<span id="libCountBadge">0</span>)
        </button>
        <button class="segment-btn" onclick="switchTab('providers')">
          <svg width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.5"><circle cx="12" cy="12" r="3"/><path d="M19.4 15a1.65 1.65 0 0 0 .33 1.82l.06.06a2 2 0 1 1-2.83 2.83l-.06-.06a1.65 1.65 0 0 0-1.82-.33 1.65 1.65 0 0 0-1 1.51V21a2 2 0 1 1-4 0v-.09A1.65 1.65 0 0 0 9 19.4a1.65 1.65 0 0 0-1.82.33l-.06.06a2 2 0 1 1-2.83-2.83l.06-.06a1.65 1.65 0 0 0 .33-1.82 1.65 1.65 0 0 0-1.51-1H3a2 2 0 1 1 0-4h.09A1.65 1.65 0 0 0 4.6 9a1.65 1.65 0 0 0-.33-1.82l-.06-.06a2 2 0 1 1 2.83-2.83l.06.06a1.65 1.65 0 0 0 1.82.33H9a1.65 1.65 0 0 0 1-1.51V3a2 2 0 1 1 4 0v.09a1.65 1.65 0 0 0 1 1.51 1.65 1.65 0 0 0 1.82-.33l.06-.06a2 2 0 1 1 2.83 2.83l-.06.06a1.65 1.65 0 0 0-.33 1.82V9a1.65 1.65 0 0 0 1.51 1H21a2 2 0 1 1 0 4h-.09a1.65 1.65 0 0 0-1.51 1z"/></svg>
          Sağlayıcılar
        </button>
      </nav>

      <!-- Right Endpoint Pill -->
      <div class="webdav-pill">
        <span class="status-dot"></span>
        <span>WebDAV: <strong>/dav/</strong></span>
      </div>
    </header>

    <!-- Main Content Tab Containers -->
    <section class="content-area">
      <!-- TAB 1: SEARCH & DISCOVER -->
      <div id="tab-search">
        <div class="search-wrapper">
          <svg class="search-icon" width="20" height="20" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.5"><circle cx="11" cy="11" r="8"/><path d="m21 21-4.3-4.3"/></svg>
          <input type="text" id="searchInput" class="search-input" placeholder="Film, dizi veya anime ara (örn: Solo Leveling, Inception, Breaking Bad)..." autocomplete="off" autofocus>
          <span class="search-shortcut">⌘K</span>
        </div>

        <div class="section-header">
          <h2 class="section-title" id="searchHeader">Arama Sonuçları</h2>
          <span id="searchResultCount" style="color:var(--text-tertiary); font-size:13px;"></span>
        </div>

        <div id="searchResults" class="media-grid"></div>
      </div>

      <!-- TAB 2: MY LIBRARY -->
      <div id="tab-library" style="display:none;">
        <div class="section-header">
          <h2 class="section-title">Kütüphanemdeki İçerikler</h2>
          <div class="filter-pills">
            <button class="filter-pill active" onclick="filterLibrary('all', this)">Tümü</button>
            <button class="filter-pill" onclick="filterLibrary('movie', this)">🎬 Filmler</button>
            <button class="filter-pill" onclick="filterLibrary('series', this)">📺 Diziler</button>
            <button class="filter-pill" onclick="filterLibrary('anime', this)">⛩️ Animeler</button>
          </div>
        </div>
        <div id="libraryGrid" class="media-grid"></div>
      </div>

      <!-- TAB 3: PROVIDERS -->
      <div id="tab-providers" style="display:none;">
        <div class="section-header">
          <h2 class="section-title">Kayıtlı 34 Sağlayıcı Motoru</h2>
          <span style="color:var(--text-tertiary); font-size:13px;">Anthology Akıllı Çözücü Aktif</span>
        </div>
        <div id="providersGrid" class="providers-grid"></div>
      </div>
    </section>
  </div>

  <script>
    let currentTab = 'search';
    let libraryItems = [];
    let providerList = [];
    let searchTimeout = null;
    let activeFilter = 'all';
    let activeModalItem = null;

    // Hotkey: Command+K to focus search
    window.addEventListener('keydown', (e) => {
      if ((e.metaKey || e.ctrlKey) && e.key === 'k') {
        e.preventDefault();
        switchTab('search');
        document.getElementById('searchInput').focus();
      }
      if (e.key === 'Escape') {
        closeSourceModal();
      }
    });

    document.getElementById('searchInput').addEventListener('input', (e) => {
      clearTimeout(searchTimeout);
      const q = e.target.value.trim();
      if (!q) {
        document.getElementById('searchResults').innerHTML = '';
        document.getElementById('searchResultCount').textContent = '';
        return;
      }
      searchTimeout = setTimeout(() => doSearch(q), 300);
    });

    function showToast(msg, icon = '✓') {
      const toast = document.getElementById('toast');
      document.getElementById('toastMsg').textContent = msg;
      document.getElementById('toastIcon').textContent = icon;
      toast.classList.add('show');
      setTimeout(() => toast.classList.remove('show'), 2600);
    }

    function switchTab(tab) {
      currentTab = tab;
      document.querySelectorAll('.segment-btn').forEach((btn, idx) => {
        btn.classList.toggle('active', 
          (tab === 'search' && idx === 0) ||
          (tab === 'library' && idx === 1) ||
          (tab === 'providers' && idx === 2)
        );
      });
      document.getElementById('tab-search').style.display = tab === 'search' ? 'block' : 'none';
      document.getElementById('tab-library').style.display = tab === 'library' ? 'block' : 'none';
      document.getElementById('tab-providers').style.display = tab === 'providers' ? 'block' : 'none';

      if (tab === 'library') loadLibrary();
      if (tab === 'providers') loadProviders();
    }

    async function loadLibrary() {
      try {
        const res = await fetch('/api/library');
        libraryItems = await res.json() || [];
        document.getElementById('libCountBadge').textContent = libraryItems.length;
        renderLibrary();
      } catch (e) {
        console.error(e);
      }
    }

    function filterLibrary(type, btn) {
      activeFilter = type;
      document.querySelectorAll('.filter-pill').forEach(p => p.classList.remove('active'));
      btn.classList.add('active');
      renderLibrary();
    }

    function renderLibrary() {
      const grid = document.getElementById('libraryGrid');
      let filtered = libraryItems;
      if (activeFilter !== 'all') {
        filtered = libraryItems.filter(it => it.type === activeFilter);
      }

      if (filtered.length === 0) {
        grid.innerHTML = '<div class="empty-state" style="grid-column: 1/-1;"><div class="empty-icon">📭</div><h3>Kütüphanenizde henüz içerik yok.</h3><p style="margin-top:6px;">"Keşfet & Ara" sekmesinden izlemek istediğiniz içerikleri ekleyebilirsiniz.</p></div>';
        return;
      }

      grid.innerHTML = filtered.map(it => {
        const badgeClass = it.type === 'anime' ? 'badge-anime' : (it.type === 'series' ? 'badge-series' : 'badge-movie');
        const badgeLabel = it.type === 'anime' ? '⛩️ ANIME' : (it.type === 'series' ? '📺 DIZI' : '🎬 FILM');
        const poster = it.poster_url || 'https://via.placeholder.com/300x450/111/444?text=Poster';
        const workingSet = new Set(it.working_providers || []);

        // Include all 34 providers without separating movie, series, and anime sources
        let relevantProvs = [...providerList];
        if (workingSet.size > 0) {
          relevantProvs.sort((a, b) => (workingSet.has(b.id) ? 1 : 0) - (workingSet.has(a.id) ? 1 : 0));
        }

        let provOptions = '<option value="auto">⚡ Otomatik (En İyi Çalışan Kaynak)</option>';
        relevantProvs.forEach(p => {
          const selected = (it.selected_provider === p.id) ? 'selected' : '';
          const prefix = workingSet.has(p.id) ? '✓ ' : '';
          const suffix = workingSet.has(p.id) ? ' (Çalışıyor)' : '';
          provOptions += '<option value="' + p.id + '" ' + selected + '>' + prefix + (p.title || p.name) + suffix + '</option>';
        });

        const workInfo = workingSet.size > 0 ? (workingSet.size + ' çalışan kaynak') : 'Yayına Hazır';

        return '<div class="media-card">' +
          '<div class="poster-box">' +
            '<img class="poster-img" src="' + poster + '" loading="lazy" alt="' + it.title + '">' +
            '<span class="badge-type ' + badgeClass + '">' + badgeLabel + '</span>' +
          '</div>' +
          '<div class="card-body">' +
            '<div class="media-title" title="' + it.title + '">' + it.title + '</div>' +
            '<div class="media-meta"><span>' + (it.year || 'N/A') + '</span> • <span style="color:var(--accent-green);">' + workInfo + '</span></div>' +
            '<label style="font-size:10px; color:var(--text-tertiary);">Oynatma Kaynağı:</label>' +
            '<select class="provider-select" onchange="changeProvider(\'' + it.id + '\', this.value)">' +
              provOptions +
            '</select>' +
            '<button class="btn-action btn-discover" onclick="openSourceFinder(\'' + it.id + '\')">' +
              '🔍 Çalışan Kaynakları Bul' +
            '</button>' +
            '<button class="btn-action btn-delete" onclick="removeFromLibrary(\'' + it.id + '\', \'' + it.title.replace(/'/g, "\\'") + '\')">' +
              '<svg width="12" height="12" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.5"><polyline points="3 6 5 6 21 6"/><path d="M19 6v14a2 2 0 0 1-2 2H7a2 2 0 0 1-2-2V6m3 0V4a2 2 0 0 1 2-2h4a2 2 0 0 1 2 2v2"/></svg>' +
              'Kütüphaneden Çıkar' +
            '</button>' +
          '</div>' +
        '</div>';
      }).join('');
    }

    function openSourceFinder(itemId) {
      const it = libraryItems.find(x => x.id === itemId);
      if (!it) return;
      activeModalItem = it;
      document.getElementById('modalItemTitle').textContent = it.title + (it.year ? ' (' + it.year + ')' : '');
      const epBar = document.getElementById('modalEpisodeBar');
      epBar.style.display = (it.type === 'movie') ? 'none' : 'flex';
      document.getElementById('modalSeason').value = 1;
      document.getElementById('modalEpisode').value = 1;
      document.getElementById('sourceModal').classList.add('open');
      scanSourcesForModal();
    }

    function closeSourceModal() {
      document.getElementById('sourceModal').classList.remove('open');
      activeModalItem = null;
    }

    async function scanSourcesForModal() {
      if (!activeModalItem) return;
      const it = activeModalItem;
      const s = parseInt(document.getElementById('modalSeason').value || '1', 10);
      const e = parseInt(document.getElementById('modalEpisode').value || '1', 10);
      const listEl = document.getElementById('modalSourceList');
      listEl.innerHTML = '<div style="text-align:center; padding:32px; color:var(--text-secondary);">⏳ Sağlayıcılar eşzamanlı taranıyor ve doğrulanıyor...</div>';

      try {
        const u = '/api/sources?type=' + encodeURIComponent(it.type) +
          '&tmdb_id=' + encodeURIComponent(it.tmdb_id) +
          '&title=' + encodeURIComponent(it.title) +
          '&original_title=' + encodeURIComponent(it.original_title || '') +
          '&year=' + encodeURIComponent(it.year || '') +
          '&season=' + s + '&episode=' + e;

        const res = await fetch(u);
        const data = await res.json();
        const sources = (data && data.sources) ? data.sources : [];

        if (sources.length === 0) {
          listEl.innerHTML = '<div style="text-align:center; padding:30px; color:var(--accent-pink);">Bu bölüm/içerik için şu an aktif bir yayın kaynağı bulunamadı.</div>';
          return;
        }

        // Update local library state so dropdown reflects working providers immediately
        await loadLibrary();

        const bestQual = sources[0].quality || '1080p';
        let html = '<div class="source-row" style="border-color: rgba(48, 209, 88, 0.35); background: rgba(48, 209, 88, 0.08);">' +
          '<div class="source-row-info">' +
            '<div class="source-row-name">⚡ Otomatik (En İyi Çalışan Kaynak) <span class="status-badge">' + bestQual + '</span></div>' +
            '<div class="source-row-sub">Doğrulanmış ' + sources.length + ' kaynak arasından en yüksek kaliteyi otomatik oynatır</div>' +
          '</div>' +
          '<div class="source-row-actions">' +
            '<button class="btn-mini primary" onclick="selectWorkingProvider(\'' + it.id + '\', \'auto\')">Varsayılan Yap</button>' +
            '<button class="btn-mini" onclick="copyWorkingStreamURL(\'auto\', ' + s + ', ' + e + ')">Linki Kopyala</button>' +
          '</div>' +
        '</div>';

        sources.forEach((src, idx) => {
          const isCurrent = (it.selected_provider === src.provider_id);
          html += '<div class="source-row">' +
            '<div class="source-row-info">' +
              '<div class="source-row-name">' + (idx + 1) + '. ' + src.provider_name + ' <span class="status-badge">' + src.quality + ' • ' + src.format + '</span></div>' +
              '<div class="source-row-sub">' + (src.title || src.provider_name) + '</div>' +
            '</div>' +
            '<div class="source-row-actions">' +
              '<button class="btn-mini ' + (isCurrent ? '' : 'primary') + '" onclick="selectWorkingProvider(\'' + it.id + '\', \'' + src.provider_id + '\')">' +
                (isCurrent ? '✓ Seçili' : 'Bu Kaynağı Seç') +
              '</button>' +
              '<button class="btn-mini" onclick="copyWorkingStreamURL(\'' + src.provider_id + '\', ' + s + ', ' + e + ')">Linki Kopyala</button>' +
            '</div>' +
          '</div>';
        });

        listEl.innerHTML = html;
      } catch (err) {
        listEl.innerHTML = '<div style="text-align:center; padding:30px; color:var(--accent-pink);">Tarama sırasında hata oluştu.</div>';
      }
    }

    async function selectWorkingProvider(itemId, provId) {
      await changeProvider(itemId, provId);
      await loadLibrary();
      closeSourceModal();
    }

    function copyWorkingStreamURL(provId, s, e) {
      if (!activeModalItem) return;
      const it = activeModalItem;
      const url = window.location.origin + '/stream/resolve?type=' + encodeURIComponent(it.type) +
        '&tmdb_id=' + encodeURIComponent(it.tmdb_id) +
        '&title=' + encodeURIComponent(it.title) +
        '&year=' + encodeURIComponent(it.year || '') +
        '&season=' + s + '&episode=' + e +
        '&provider=' + encodeURIComponent(provId);
      navigator.clipboard.writeText(url).then(() => {
        showToast('Doğrulanmış yayın linki kopyalandı! (VLC / IINA / Infuse ile açabilirsiniz)', '▶');
      });
    }

    async function doSearch(query) {
      const resultsContainer = document.getElementById('searchResults');
      resultsContainer.innerHTML = '<div style="grid-column: 1/-1; text-align:center; padding: 40px; color: var(--text-secondary);">TMDB taranıyor...</div>';
      try {
        const res = await fetch('/api/search?q=' + encodeURIComponent(query));
        const list = await res.json() || [];
        document.getElementById('searchResultCount').textContent = list.length + ' sonuç bulundu';

        if (list.length === 0) {
          resultsContainer.innerHTML = '<div class="empty-state" style="grid-column: 1/-1;"><div class="empty-icon">🔍</div><h3>Sonuç bulunamadı</h3></div>';
          return;
        }

        resultsContainer.innerHTML = list.map(it => {
          const isAdded = libraryItems.some(lib => lib.tmdb_id === it.id);
          const badgeClass = it.type === 'anime' ? 'badge-anime' : (it.type === 'series' ? 'badge-series' : 'badge-movie');
          const badgeLabel = it.type === 'anime' ? '⛩️ ANIME' : (it.type === 'series' ? '📺 DIZI' : '🎬 FILM');
          const poster = it.poster_url || 'https://via.placeholder.com/300x450/111/444?text=Poster';
          const rating = it.rating ? '★ ' + it.rating.toFixed(1) : '';

          const actionBtn = isAdded ?
            '<button class="btn-action btn-added">✓ Kütüphanede</button>' :
            '<button class="btn-action" onclick="addToLibrary(' + JSON.stringify(it).replace(/"/g, '&quot;') + ')">+ Kütüphaneme Ekle</button>';

          return '<div class="media-card">' +
            '<div class="poster-box">' +
              '<img class="poster-img" src="' + poster + '" loading="lazy" alt="' + it.title + '">' +
              '<span class="badge-type ' + badgeClass + '">' + badgeLabel + '</span>' +
              (rating ? '<span class="rating-badge">' + rating + '</span>' : '') +
            '</div>' +
            '<div class="card-body">' +
              '<div class="media-title" title="' + it.title + '">' + it.title + '</div>' +
              '<div class="media-meta"><span>' + (it.year || 'N/A') + '</span></div>' +
              actionBtn +
            '</div>' +
          '</div>';
        }).join('');
      } catch (e) {
        resultsContainer.innerHTML = '<div style="color:var(--accent-pink); padding:20px;">Arama sırasında hata oluştu.</div>';
      }
    }

    async function addToLibrary(it) {
      try {
        const payload = {
          tmdb_id: it.id,
          title: it.title,
          original_title: it.original_title,
          year: it.year,
          type: it.type,
          poster_url: it.poster_url,
          backdrop_url: it.backdrop_url,
          overview: it.overview,
          selected_provider: 'auto'
        };
        const res = await fetch('/api/library/add', {
          method: 'POST',
          headers: {'Content-Type': 'application/json'},
          body: JSON.stringify(payload)
        });
        if (res.ok) {
          showToast(it.title + ' kütüphanenize eklendi! Medya oynatıcınızdan hemen izleyebilirsiniz.');
          await loadLibrary();
          const q = document.getElementById('searchInput').value.trim();
          if (q) doSearch(q);
        }
      } catch (e) {
        showToast('Ekleme başarısız!', '✕');
      }
    }

    async function removeFromLibrary(id, title) {
      try {
        const res = await fetch('/api/library/remove', {
          method: 'POST',
          headers: {'Content-Type': 'application/json'},
          body: JSON.stringify({id: id})
        });
        if (res.ok) {
          showToast(title + ' kütüphaneden çıkarıldı.');
          await loadLibrary();
        }
      } catch (e) {
        showToast('Silme başarısız!', '✕');
      }
    }

    async function changeProvider(id, prov) {
      try {
        await fetch('/api/library/provider', {
          method: 'POST',
          headers: {'Content-Type': 'application/json'},
          body: JSON.stringify({id: id, provider: prov})
        });
        showToast('Varsayılan oynatma kaynağı güncellendi: ' + prov);
      } catch (e) {
        console.error(e);
      }
    }

    async function loadProviders() {
      const grid = document.getElementById('providersGrid');
      grid.innerHTML = '<div style="grid-column: 1/-1; text-align:center; padding: 30px;">Sağlayıcılar taranıyor...</div>';
      try {
        const res = await fetch('/api/providers');
        providerList = await res.json() || [];
        grid.innerHTML = providerList.map(p => {
          const title = p.title || p.name;
          const icon = p.icon || '⚡';
          const domain = p.domain || '';
          const hasFavicon = domain && !domain.includes('github.com');
          const faviconUrl = hasFavicon ? 'https://www.google.com/s2/favicons?domain=' + encodeURIComponent(domain) + '&sz=64' : '';

          let logoHtml = '';
          if (hasFavicon) {
            logoHtml = '<div class="provider-logo-box">' +
              '<img class="provider-logo-img" src="' + faviconUrl + '" alt="' + title + '" ' +
                'onload="this.style.display=\'block\'; if(this.nextElementSibling) this.nextElementSibling.style.display=\'none\';" ' +
                'onerror="this.style.display=\'none\'; if(this.nextElementSibling) this.nextElementSibling.style.display=\'flex\';">' +
              '<div class="provider-fallback-icon" style="display:none;">' + icon + '</div>' +
            '</div>';
          } else {
            logoHtml = '<div class="provider-logo-box"><div class="provider-fallback-icon">' + icon + '</div></div>';
          }

          const domainText = domain ? domain : p.id;

          return '<div class="provider-card">' +
            logoHtml +
            '<div class="provider-info">' +
              '<div class="provider-title" title="' + title + '">' + title + '</div>' +
              '<div class="provider-meta">' +
                '<span class="provider-cat-tag">' + p.category + '</span>' +
                '<span>•</span>' +
                '<span title="' + domainText + '">' + domainText + '</span>' +
              '</div>' +
            '</div>' +
            '<span class="status-badge provider-status">AKTİF</span>' +
          '</div>';
        }).join('');
      } catch (e) {
        grid.innerHTML = '<div style="color:var(--accent-pink);">Sağlayıcılar yüklenemedi.</div>';
      }
    }

    // Initial load
    (async () => {
      await loadLibrary();
      try {
        const res = await fetch('/api/providers');
        providerList = await res.json() || [];
      } catch (e) {}
    })();
  </script>
</body>
</html>`
