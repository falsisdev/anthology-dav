package web

import (
	_ "embed"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"anthology-dav/auth"
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
	case path == "/auth/register" && r.Method == http.MethodPost:
		h.handleAuthRegister(w, r)
	case path == "/auth/login" && r.Method == http.MethodPost:
		h.handleAuthLogin(w, r)
	case path == "/auth/logout" && r.Method == http.MethodPost:
		h.handleAuthLogout(w, r)
	case path == "/auth/me" && r.Method == http.MethodGet:
		h.handleAuthMe(w, r)
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

func (h *Handler) handleAuthRegister(w http.ResponseWriter, r *http.Request) {
	if !h.cfg.MultiUser {
		http.Error(w, `{"error":"çok kullanıcılı mod aktif değil"}`, http.StatusBadRequest)
		return
	}
	if !h.cfg.AllowRegistration {
		http.Error(w, `{"error":"yeni kullanıcı kaydı şu anda kapalıdır"}`, http.StatusForbidden)
		return
	}
	var body struct {
		Username string `json:"username"`
		Password string `json:"password"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		http.Error(w, `{"error":"geçersiz veri"}`, http.StatusBadRequest)
		return
	}
	u, err := auth.DB().Register(body.Username, body.Password)
	if err != nil {
		http.Error(w, fmt.Sprintf(`{"error":"%s"}`, err.Error()), http.StatusBadRequest)
		return
	}
	sess, err := auth.DB().CreateSession(u.ID)
	if err == nil {
		http.SetCookie(w, &http.Cookie{
			Name:     "anthology_session",
			Value:    sess.Token,
			Path:     "/",
			HttpOnly: true,
			SameSite: http.SameSiteLaxMode,
			MaxAge:   30 * 24 * 3600,
		})
	}
	_ = json.NewEncoder(w).Encode(map[string]any{
		"status": "ok",
		"user": map[string]any{
			"id":       u.ID,
			"username": u.Username,
		},
		"token": sess.Token,
	})
}

func (h *Handler) handleAuthLogin(w http.ResponseWriter, r *http.Request) {
	if !h.cfg.MultiUser {
		http.Error(w, `{"error":"çok kullanıcılı mod aktif değil"}`, http.StatusBadRequest)
		return
	}
	var body struct {
		Username string `json:"username"`
		Password string `json:"password"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		http.Error(w, `{"error":"geçersiz veri"}`, http.StatusBadRequest)
		return
	}
	u, err := auth.DB().Authenticate(body.Username, body.Password)
	if err != nil {
		http.Error(w, fmt.Sprintf(`{"error":"%s"}`, err.Error()), http.StatusUnauthorized)
		return
	}
	sess, err := auth.DB().CreateSession(u.ID)
	if err == nil {
		http.SetCookie(w, &http.Cookie{
			Name:     "anthology_session",
			Value:    sess.Token,
			Path:     "/",
			HttpOnly: true,
			SameSite: http.SameSiteLaxMode,
			MaxAge:   30 * 24 * 3600,
		})
	}
	_ = json.NewEncoder(w).Encode(map[string]any{
		"status": "ok",
		"user": map[string]any{
			"id":       u.ID,
			"username": u.Username,
		},
		"token": sess.Token,
	})
}

func (h *Handler) handleAuthLogout(w http.ResponseWriter, r *http.Request) {
	if c, err := r.Cookie("anthology_session"); err == nil && c.Value != "" {
		_ = auth.DB().DeleteSession(c.Value)
	}
	http.SetCookie(w, &http.Cookie{
		Name:     "anthology_session",
		Value:    "",
		Path:     "/",
		HttpOnly: true,
		MaxAge:   -1,
	})
	_ = json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
}

func (h *Handler) handleAuthMe(w http.ResponseWriter, r *http.Request) {
	if !h.cfg.MultiUser {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"multi_user": false,
			"user":       map[string]string{"username": "local"},
		})
		return
	}
	u := auth.ExtractWebUser(r)
	if u == nil {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"multi_user":     true,
			"user":           nil,
			"login_required": true,
		})
		return
	}
	_ = json.NewEncoder(w).Encode(map[string]any{
		"multi_user": true,
		"user": map[string]any{
			"id":       u.ID,
			"username": u.Username,
		},
	})
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
	if h.cfg.MultiUser {
		u := auth.ExtractWebUser(r)
		if u == nil {
			http.Error(w, `{"error":"unauthorized","login_required":true}`, http.StatusUnauthorized)
			return
		}
		items, err := auth.DB().GetUserLibrary(u.ID)
		if err != nil {
			http.Error(w, fmt.Sprintf(`{"error":"%s"}`, err.Error()), http.StatusInternalServerError)
			return
		}
		if items == nil {
			items = []config.LibraryItem{}
		}
		_ = json.NewEncoder(w).Encode(items)
		return
	}
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

	if h.cfg.MultiUser {
		u := auth.ExtractWebUser(r)
		if u == nil {
			http.Error(w, `{"error":"unauthorized","login_required":true}`, http.StatusUnauthorized)
			return
		}
		if err := auth.DB().AddUserLibraryItem(u.ID, item); err != nil {
			http.Error(w, fmt.Sprintf(`{"error":"%s"}`, err.Error()), http.StatusInternalServerError)
			return
		}
		if saved, ok := auth.DB().GetUserLibraryItemByTMDB(u.ID, item.Type, item.TMDBID); ok {
			providers.PrewarmLibraryItemMetadata(r.Context(), saved)
			if len(saved.Seasons) > 0 {
				_ = auth.DB().UpdateUserItemSeasons(u.ID, saved.ID, saved.Seasons, saved.AltTitles)
			}
			item = *saved
		}
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(map[string]any{"status": "ok", "item": item})
		return
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

	if h.cfg.MultiUser {
		u := auth.ExtractWebUser(r)
		if u == nil {
			http.Error(w, `{"error":"unauthorized","login_required":true}`, http.StatusUnauthorized)
			return
		}
		if err := auth.DB().RemoveUserLibraryItem(u.ID, body.ID); err != nil {
			http.Error(w, fmt.Sprintf(`{"error":"%s"}`, err.Error()), http.StatusInternalServerError)
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]string{"status": "ok", "removed": body.ID})
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

	if h.cfg.MultiUser {
		u := auth.ExtractWebUser(r)
		if u == nil {
			http.Error(w, `{"error":"unauthorized","login_required":true}`, http.StatusUnauthorized)
			return
		}
		if err := auth.DB().UpdateUserProvider(u.ID, body.ID, body.Provider); err != nil {
			http.Error(w, fmt.Sprintf(`{"error":"%s"}`, err.Error()), http.StatusInternalServerError)
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]string{"status": "ok", "provider": body.Provider})
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
	var curUser *auth.User
	if h.cfg.MultiUser {
		curUser = auth.ExtractWebUser(r)
		if curUser != nil {
			items, _ := auth.DB().GetUserLibrary(curUser.ID)
			libCount = len(items)
		} else {
			libCount = 0
		}
	}
	lan := config.DetectLANAddresses(h.cfg.Port)
	resp := map[string]any{
		"status":          "ok",
		"version":         "2.1-glass",
		"apple_silicon":   true,
		"providers_count": providers.Count(),
		"library_count":   libCount,
		"webdav_root":     lan.PrimaryURL + "/dav/",
		"playlist_url":    lan.PrimaryURL + "/playlist.m3u",
		"lan":             lan,
		"multi_user":      h.cfg.MultiUser,
	}
	if curUser != nil {
		resp["user"] = map[string]any{
			"id":       curUser.ID,
			"username": curUser.Username,
		}
	}
	_ = json.NewEncoder(w).Encode(resp)
}

// HandlePlaylistM3U dynamically generates an M3U8 playlist (/playlist.m3u & /playlist.m3u8)
// for VLC (Android, iOS, Desktop), IINA, PotPlayer, and IPTV players that lack a WebDAV browser.
func (h *Handler) HandlePlaylistM3U(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Access-Control-Allow-Origin", "*")
	w.Header().Set("Content-Type", "audio/x-mpegurl; charset=utf-8")
	if r.URL.Query().Get("download") == "1" {
		w.Header().Set("Content-Disposition", `attachment; filename="anthology-kutuphane.m3u"`)
	} else {
		w.Header().Set("Content-Disposition", `inline; filename="anthology-kutuphane.m3u"`)
	}

	// Determine client-reachable base URL
	baseURL := h.cfg.BaseURL
	reqHost := strings.TrimSpace(r.Host)
	if reqHost != "" {
		lowHost := strings.ToLower(reqHost)
		if strings.HasPrefix(lowHost, "127.0.0.1") || strings.HasPrefix(lowHost, "localhost") || strings.HasPrefix(lowHost, "[::1]") {
			lan := config.DetectLANAddresses(h.cfg.Port)
			if lan.PrimaryIP != "" && lan.PrimaryIP != "127.0.0.1" && (h.cfg.Host == "" || h.cfg.Host == "0.0.0.0" || h.cfg.Host == "::") {
				baseURL = lan.PrimaryURL
			} else {
				baseURL = "http://" + reqHost
			}
		} else {
			baseURL = "http://" + reqHost
		}
	}

	q := r.URL.Query()
	filterID := strings.TrimSpace(q.Get("id"))
	filterType := strings.ToLower(strings.TrimSpace(q.Get("type")))
	filterSeason, _ := strconv.Atoi(strings.TrimSpace(q.Get("season")))
	filterEpisode, _ := strconv.Atoi(strings.TrimSpace(q.Get("episode")))
	overrideProv := strings.TrimSpace(q.Get("provider"))
	overrideAudio := strings.TrimSpace(q.Get("audio"))
	if overrideAudio == "" {
		overrideAudio = "auto"
	}

	var items []config.LibraryItem
	if h.cfg.MultiUser {
		u := auth.ExtractWebUser(r)
		if u == nil {
			w.Header().Set("WWW-Authenticate", `Basic realm="Anthology Playlist"`)
			http.Error(w, "#EXTM3U\n#EXTINF:-1,Yetkisiz Erisim - Lutfen kullanici adi ve sifrenizi girin\n", http.StatusUnauthorized)
			return
		}
		var err error
		items, err = auth.DB().GetUserLibrary(u.ID)
		if err != nil {
			items = []config.LibraryItem{}
		}
	} else {
		items = config.Library().All()
	}

	var sb strings.Builder
	sb.WriteString("#EXTM3U\n")

	for _, it := range items {
		if filterID != "" && it.ID != filterID {
			continue
		}
		if filterType != "" && !strings.EqualFold(it.Type, filterType) {
			continue
		}

		prov := it.SelectedProvider
		if overrideProv != "" {
			prov = overrideProv
		}
		if prov == "" {
			prov = "auto"
		}

		displayTitle := it.Title
		if it.Year != "" {
			displayTitle = fmt.Sprintf("%s (%s)", it.Title, it.Year)
		}
		cleanTitle := strings.ReplaceAll(displayTitle, ",", " ")
		logo := it.PosterURL

		if it.Type == "movie" {
			subURL := fmt.Sprintf("%s/sub/resolve?type=movie&tmdb_id=%d&title=%s&year=%s&lang=tr&format=vtt",
				baseURL, it.TMDBID, url.QueryEscape(it.Title), url.QueryEscape(it.Year))
			streamURL := fmt.Sprintf("%s/stream/resolve?type=movie&tmdb_id=%d&title=%s&year=%s&provider=%s&audio=%s",
				baseURL, it.TMDBID, url.QueryEscape(it.Title), url.QueryEscape(it.Year), url.QueryEscape(prov), url.QueryEscape(overrideAudio))
			sb.WriteString(fmt.Sprintf("#EXTINF:-1 tvg-id=\"%d\" tvg-logo=\"%s\" group-title=\"Filmler\",%s\n#EXTVLCOPT:sub-file=%s\n%s\n",
				it.TMDBID, logo, cleanTitle, subURL, streamURL))
			continue
		}

		// Series or Anime
		itemCopy := it
		seasons := itemCopy.Seasons
		if len(seasons) == 0 {
			providers.PrewarmLibraryItemMetadata(r.Context(), &itemCopy)
			seasons = itemCopy.Seasons
		}
		if len(seasons) == 0 {
			seasons = []config.LibrarySeason{{SeasonNumber: 1, EpisodeCount: 12}}
		}

		catGroup := "Diziler"
		if it.Type == "anime" {
			catGroup = "Animeler"
		}
		groupTitle := fmt.Sprintf("%s - %s", catGroup, cleanTitle)

		for _, s := range seasons {
			if s.SeasonNumber <= 0 {
				continue
			}
			if filterSeason > 0 && s.SeasonNumber != filterSeason {
				continue
			}
			epCount := s.EpisodeCount
			if epCount <= 0 {
				epCount = 12
			}
			for ep := 1; ep <= epCount; ep++ {
				if filterEpisode > 0 && ep != filterEpisode {
					continue
				}
				epLabel := fmt.Sprintf("%s - S%02dE%02d", cleanTitle, s.SeasonNumber, ep)
				subURL := fmt.Sprintf("%s/sub/resolve?type=%s&tmdb_id=%d&title=%s&year=%s&season=%d&episode=%d&lang=tr&format=vtt",
					baseURL, it.Type, it.TMDBID, url.QueryEscape(it.Title), url.QueryEscape(it.Year), s.SeasonNumber, ep)
				streamURL := fmt.Sprintf("%s/stream/resolve?type=%s&tmdb_id=%d&title=%s&year=%s&season=%d&episode=%d&provider=%s&audio=%s",
					baseURL, it.Type, it.TMDBID, url.QueryEscape(it.Title), url.QueryEscape(it.Year), s.SeasonNumber, ep, url.QueryEscape(prov), url.QueryEscape(overrideAudio))
				sb.WriteString(fmt.Sprintf("#EXTINF:-1 tvg-id=\"%d_S%02dE%02d\" tvg-logo=\"%s\" group-title=\"%s\",%s\n#EXTVLCOPT:sub-file=%s\n%s\n",
					it.TMDBID, s.SeasonNumber, ep, logo, groupTitle, epLabel, subURL, streamURL))
			}
		}
	}

	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte(sb.String()))
}

// ── Material 3 (Material You) Native Design Web App (Embedded HTML/CSS/JS) ─────

const material3HTML = `<!DOCTYPE html>
<html lang="tr">
<head>
  <meta charset="UTF-8">
  <meta name="viewport" content="width=device-width, initial-scale=1.0, viewport-fit=cover">
  <title>Anthology WebDAV</title>
  <link rel="icon" type="image/x-icon" href="/favicon.ico">
  <link rel="preconnect" href="https://fonts.googleapis.com">
  <link rel="preconnect" href="https://fonts.gstatic.com" crossorigin>
  <link href="https://fonts.googleapis.com/css2?family=Roboto:wght@400;500;700&display=swap" rel="stylesheet">
  <style>
    :root {
      /* Google Material 3 - Dark Cinema Tonal Palette */
      --md-sys-color-primary: #A8C7FA;
      --md-sys-color-on-primary: #003062;
      --md-sys-color-primary-container: #084282;
      --md-sys-color-on-primary-container: #D3E4FF;

      --md-sys-color-secondary: #BEC6DC;
      --md-sys-color-on-secondary: #283141;
      --md-sys-color-secondary-container: #334460;
      --md-sys-color-on-secondary-container: #DBE2F9;

      --md-sys-color-tertiary: #FFB786;
      --md-sys-color-on-tertiary: #4D2600;
      --md-sys-color-tertiary-container: #6F3800;
      --md-sys-color-on-tertiary-container: #FFDCC3;

      --md-sys-color-error: #FFB4AB;
      --md-sys-color-on-error: #690005;
      --md-sys-color-error-container: #93000A;
      --md-sys-color-on-error-container: #FFDAD6;

      --md-sys-color-background: #0E1219;
      --md-sys-color-on-background: #E1E2E8;
      --md-sys-color-surface: #0E1219;
      --md-sys-color-on-surface: #E1E2E8;
      --md-sys-color-surface-dim: #090C11;
      --md-sys-color-surface-bright: #343A46;
      --md-sys-color-surface-container-lowest: #07090E;
      --md-sys-color-surface-container-low: #131720;
      --md-sys-color-surface-container: #181D27;
      --md-sys-color-surface-container-high: #212734;
      --md-sys-color-surface-container-highest: #2C3343;

      --md-sys-color-surface-variant: #424754;
      --md-sys-color-on-surface-variant: #C3C6CF;
      --md-sys-color-outline: #8C919D;
      --md-sys-color-outline-variant: #313746;
      --md-sys-color-scrim: #000000;

      --md-sys-color-inverse-surface: #E1E2E8;
      --md-sys-color-inverse-on-surface: #2D3139;
      --md-sys-color-inverse-primary: #1961B7;

      /* Media Category Badges */
      --badge-movie-bg: rgba(8, 66, 130, 0.85);
      --badge-movie-fg: #D3E4FF;
      --badge-series-bg: rgba(85, 39, 104, 0.85);
      --badge-series-fg: #F7D8FF;
      --badge-anime-bg: rgba(124, 43, 60, 0.85);
      --badge-anime-fg: #FFD9DF;

      /* Shape tokens */
      --md-sys-shape-corner-none: 0px;
      --md-sys-shape-corner-extra-small: 4px;
      --md-sys-shape-corner-small: 8px;
      --md-sys-shape-corner-medium: 12px;
      --md-sys-shape-corner-large: 16px;
      --md-sys-shape-corner-extra-large: 28px;
      --md-sys-shape-corner-full: 9999px;

      /* Elevation Shadows */
      --md-sys-elevation-0: none;
      --md-sys-elevation-1: 0 1px 3px 1px rgba(0,0,0,0.35), 0 1px 2px 0 rgba(0,0,0,0.3);
      --md-sys-elevation-2: 0 2px 6px 2px rgba(0,0,0,0.4), 0 1px 2px 0 rgba(0,0,0,0.3);
      --md-sys-elevation-3: 0 4px 12px 3px rgba(0,0,0,0.45), 0 1px 3px 0 rgba(0,0,0,0.35);
      --md-sys-elevation-4: 0 6px 16px 4px rgba(0,0,0,0.5), 0 2px 4px 0 rgba(0,0,0,0.4);

      /* Motion tokens */
      --md-sys-motion-easing-emphasized: cubic-bezier(0.2, 0, 0, 1);
      --md-sys-motion-easing-emphasized-decelerate: cubic-bezier(0.05, 0.7, 0.1, 1);
      --md-sys-motion-easing-emphasized-accelerate: cubic-bezier(0.3, 0, 0.8, 0.15);
      --md-sys-motion-duration-short: 150ms;
      --md-sys-motion-duration-medium: 300ms;
      --md-sys-motion-duration-long: 500ms;

      --font-family: Roboto, -apple-system, BlinkMacSystemFont, "Segoe UI", system-ui, sans-serif;
    }

    * {
      box-sizing: border-box;
      margin: 0;
      padding: 0;
      -webkit-font-smoothing: antialiased;
      -moz-osx-font-smoothing: grayscale;
    }

    body {
      font-family: var(--font-family);
      background-color: var(--md-sys-color-background);
      color: var(--md-sys-color-on-background);
      min-height: 100vh;
      overflow-x: hidden;
      display: flex;
    }

    /* Common M3 Icon styling */
    .m3-icon {
      width: 22px;
      height: 22px;
      fill: currentColor;
      flex-shrink: 0;
      display: inline-block;
      vertical-align: middle;
      transition: transform var(--md-sys-motion-duration-short) var(--md-sys-motion-easing-emphasized);
    }

    /* M3 Ripple Wave Animation */
    .m3-ripple-container {
      position: relative;
      overflow: hidden;
      user-select: none;
    }

    .m3-ripple-wave {
      position: absolute;
      border-radius: 50%;
      background-color: currentColor;
      opacity: 0.18;
      pointer-events: none;
      transform: scale(0);
      animation: m3-ripple-anim 550ms var(--md-sys-motion-easing-emphasized-decelerate) forwards;
    }

    @keyframes m3-ripple-anim {
      to {
        transform: scale(2.6);
        opacity: 0;
      }
    }

    /* App Root Layout */
    .m3-app-root {
      display: flex;
      width: 100%;
      min-height: 100vh;
      position: relative;
    }

    /* Desktop Navigation Rail (>= 840px) */
    .m3-nav-rail {
      width: 88px;
      height: 100vh;
      position: fixed;
      top: 0;
      left: 0;
      background-color: var(--md-sys-color-surface-container-low);
      border-right: 1px solid var(--md-sys-color-outline-variant);
      display: flex;
      flex-direction: column;
      align-items: center;
      padding: 20px 0;
      z-index: 90;
      gap: 20px;
    }

    .m3-rail-brand {
      display: flex;
      align-items: center;
      justify-content: center;
      width: 52px;
      height: 52px;
      border-radius: var(--md-sys-shape-corner-large);
      background-color: var(--md-sys-color-surface-container);
      box-shadow: var(--md-sys-elevation-1);
      margin-bottom: 8px;
    }

    .m3-brand-logo {
      width: 32px;
      height: 32px;
      object-fit: contain;
      filter: drop-shadow(0 2px 6px rgba(168, 199, 250, 0.4));
    }

    .m3-rail-nav {
      display: flex;
      flex-direction: column;
      align-items: center;
      gap: 16px;
      flex: 1;
      width: 100%;
    }

    .m3-rail-item {
      display: flex;
      flex-direction: column;
      align-items: center;
      gap: 6px;
      background: none;
      border: none;
      cursor: pointer;
      color: var(--md-sys-color-on-surface-variant);
      width: 100%;
      padding: 4px 0;
      transition: color var(--md-sys-motion-duration-short) var(--md-sys-motion-easing-emphasized);
      font-family: inherit;
    }

    .m3-rail-indicator {
      width: 56px;
      height: 32px;
      border-radius: var(--md-sys-shape-corner-full);
      display: flex;
      align-items: center;
      justify-content: center;
      position: relative;
      transition: all var(--md-sys-motion-duration-medium) var(--md-sys-motion-easing-emphasized);
    }

    .m3-rail-item:hover .m3-rail-indicator {
      background-color: rgba(255, 255, 255, 0.06);
      color: var(--md-sys-color-on-surface);
    }

    .m3-rail-item.active {
      color: var(--md-sys-color-on-surface);
    }

    .m3-rail-item.active .m3-rail-indicator {
      background-color: var(--md-sys-color-secondary-container);
      color: var(--md-sys-color-on-secondary-container);
      box-shadow: var(--md-sys-elevation-1);
    }

    .m3-rail-label {
      font-size: 11px;
      font-weight: 500;
      letter-spacing: 0.1px;
    }

    .m3-rail-badge {
      position: absolute;
      top: -3px;
      right: 6px;
      background-color: var(--md-sys-color-error);
      color: var(--md-sys-color-on-error);
      font-size: 10px;
      font-weight: 700;
      min-width: 18px;
      height: 18px;
      line-height: 18px;
      border-radius: 9px;
      text-align: center;
      padding: 0 4px;
    }

    .m3-rail-footer {
      display: flex;
      flex-direction: column;
      align-items: center;
      gap: 12px;
      width: 100%;
    }

    /* Main Shell */
    .m3-main-shell {
      flex: 1;
      margin-left: 88px;
      display: flex;
      flex-direction: column;
      min-height: 100vh;
      position: relative;
      background-color: var(--md-sys-color-background);
    }

    /* Top App Bar */
    .m3-top-app-bar {
      height: 64px;
      position: sticky;
      top: 0;
      z-index: 80;
      background-color: rgba(14, 18, 25, 0.85);
      backdrop-filter: blur(20px);
      -webkit-backdrop-filter: blur(20px);
      border-bottom: 1px solid var(--md-sys-color-outline-variant);
      padding: 0 32px;
      display: flex;
      align-items: center;
      justify-content: space-between;
      gap: 16px;
    }

    .m3-top-brand {
      display: flex;
      align-items: center;
      gap: 12px;
    }

    .m3-mobile-logo {
      display: none;
      width: 32px;
      height: 32px;
      object-fit: contain;
    }

    .m3-top-title {
      font-size: 19px;
      font-weight: 700;
      letter-spacing: -0.3px;
      color: var(--md-sys-color-on-surface);
    }

    .m3-status-chip {
      display: inline-flex;
      align-items: center;
      gap: 6px;
      padding: 4px 12px;
      background-color: rgba(166, 245, 181, 0.12);
      border: 1px solid rgba(166, 245, 181, 0.3);
      border-radius: var(--md-sys-shape-corner-full);
      font-size: 11px;
      font-weight: 600;
      color: #A6F5B5;
    }

    .m3-status-dot {
      width: 6px;
      height: 6px;
      border-radius: 50%;
      background-color: #30D158;
      box-shadow: 0 0 8px #30D158;
      animation: m3-pulse 2s infinite ease-in-out;
    }

    @keyframes m3-pulse {
      0%, 100% { transform: scale(1); opacity: 1; }
      50% { transform: scale(0.85); opacity: 0.5; }
    }

    .m3-top-actions {
      display: flex;
      align-items: center;
      gap: 10px;
    }

    /* M3 Pill Buttons */
    .m3-pill-btn {
      display: inline-flex;
      align-items: center;
      gap: 8px;
      height: 40px;
      padding: 0 18px;
      border-radius: var(--md-sys-shape-corner-full);
      font-family: inherit;
      font-size: 13px;
      font-weight: 500;
      cursor: pointer;
      border: 1px solid var(--md-sys-color-outline-variant);
      background-color: var(--md-sys-color-surface-container);
      color: var(--md-sys-color-on-surface);
      transition: all var(--md-sys-motion-duration-short) var(--md-sys-motion-easing-emphasized);
    }

    .m3-pill-btn:hover {
      background-color: var(--md-sys-color-surface-container-high);
      border-color: var(--md-sys-color-outline);
      box-shadow: var(--md-sys-elevation-1);
    }

    .m3-pill-btn.primary {
      background-color: var(--md-sys-color-primary);
      color: var(--md-sys-color-on-primary);
      border-color: transparent;
      font-weight: 600;
    }

    .m3-pill-btn.primary:hover {
      background-color: #BDD6FF;
      box-shadow: var(--md-sys-elevation-2);
    }

    .m3-pill-btn.vlc {
      background-color: var(--md-sys-color-tertiary-container);
      color: var(--md-sys-color-on-tertiary-container);
      border-color: rgba(255, 183, 134, 0.3);
      font-weight: 600;
    }

    .m3-pill-btn.vlc:hover {
      background-color: #8C4700;
      box-shadow: var(--md-sys-elevation-2);
    }

    .m3-icon-btn {
      width: 44px;
      height: 44px;
      border-radius: 50%;
      border: none;
      background: none;
      color: var(--md-sys-color-on-surface-variant);
      display: inline-flex;
      align-items: center;
      justify-content: center;
      cursor: pointer;
      transition: background-color var(--md-sys-motion-duration-short) var(--md-sys-motion-easing-emphasized), color var(--md-sys-motion-duration-short);
    }

    .m3-icon-btn:hover {
      background-color: rgba(255, 255, 255, 0.08);
      color: var(--md-sys-color-on-surface);
    }

    /* Content Area */
    .m3-content-area {
      flex: 1;
      width: 100%;
      max-width: 1400px;
      margin: 0 auto;
      padding: 32px 32px 80px;
    }

    /* Tab Transition Container */
    .m3-tab-pane {
      display: none;
      opacity: 0;
      transform: translateY(8px);
      transition: opacity var(--md-sys-motion-duration-medium) var(--md-sys-motion-easing-emphasized),
                  transform var(--md-sys-motion-duration-medium) var(--md-sys-motion-easing-emphasized);
    }

    .m3-tab-pane.active {
      display: block;
      opacity: 1;
      transform: translateY(0);
    }

    /* M3 Floating Search Bar */
    .m3-search-anchor {
      max-width: 720px;
      margin: 12px auto 28px;
      position: relative;
    }

    .m3-search-bar {
      display: flex;
      align-items: center;
      height: 56px;
      border-radius: var(--md-sys-shape-corner-full);
      background-color: var(--md-sys-color-surface-container-high);
      border: 1px solid var(--md-sys-color-outline-variant);
      box-shadow: var(--md-sys-elevation-2);
      padding: 0 16px 0 20px;
      gap: 14px;
      transition: all var(--md-sys-motion-duration-medium) var(--md-sys-motion-easing-emphasized);
    }

    .m3-search-bar:focus-within {
      background-color: var(--md-sys-color-surface-container-highest);
      border-color: var(--md-sys-color-primary);
      box-shadow: var(--md-sys-elevation-3);
    }

    .m3-search-input {
      flex: 1;
      height: 100%;
      background: none;
      border: none;
      outline: none;
      color: var(--md-sys-color-on-surface);
      font-family: inherit;
      font-size: 16px;
    }

    .m3-search-input::placeholder {
      color: var(--md-sys-color-on-surface-variant);
    }

    .m3-search-clear {
      display: none;
      width: 32px;
      height: 32px;
      border-radius: 50%;
      border: none;
      background: none;
      color: var(--md-sys-color-on-surface-variant);
      align-items: center;
      justify-content: center;
      cursor: pointer;
    }

    .m3-search-clear:hover {
      background-color: rgba(255, 255, 255, 0.1);
      color: var(--md-sys-color-on-surface);
    }

    .m3-search-shortcut {
      font-size: 11px;
      font-weight: 600;
      color: var(--md-sys-color-on-surface-variant);
      background-color: var(--md-sys-color-surface-container);
      border: 1px solid var(--md-sys-color-outline-variant);
      border-radius: var(--md-sys-shape-corner-small);
      padding: 3px 8px;
      user-select: none;
    }

    /* M3 Filter Chips Row */
    .m3-chips-row {
      display: flex;
      align-items: center;
      justify-content: center;
      gap: 10px;
      margin-top: 14px;
      flex-wrap: wrap;
    }

    .m3-filter-chip {
      display: inline-flex;
      align-items: center;
      gap: 8px;
      height: 36px;
      padding: 0 16px;
      border-radius: var(--md-sys-shape-corner-small);
      background-color: var(--md-sys-color-surface-container-low);
      border: 1px solid var(--md-sys-color-outline-variant);
      color: var(--md-sys-color-on-surface-variant);
      font-family: inherit;
      font-size: 13px;
      font-weight: 500;
      cursor: pointer;
      transition: all var(--md-sys-motion-duration-short) var(--md-sys-motion-easing-emphasized);
    }

    .m3-filter-chip:hover {
      background-color: var(--md-sys-color-surface-container-high);
      color: var(--md-sys-color-on-surface);
    }

    .m3-filter-chip.active {
      background-color: var(--md-sys-color-secondary-container);
      color: var(--md-sys-color-on-secondary-container);
      border-color: transparent;
      font-weight: 600;
      box-shadow: var(--md-sys-elevation-1);
    }

    .m3-chip-check {
      display: none;
      width: 16px;
      height: 16px;
    }

    .m3-filter-chip.active .m3-chip-check {
      display: inline-block;
    }

    /* Section Headers */
    .m3-section-header {
      display: flex;
      align-items: center;
      justify-content: space-between;
      margin-top: 24px;
      margin-bottom: 20px;
    }

    .m3-section-title {
      font-size: 22px;
      font-weight: 700;
      letter-spacing: -0.4px;
      color: var(--md-sys-color-on-surface);
    }

    .m3-section-subtitle {
      font-size: 13px;
      color: var(--md-sys-color-on-surface-variant);
    }

    /* M3 Banner Card (Library VLC info) */
    .m3-banner-card {
      background-color: var(--md-sys-color-surface-container);
      border: 1px solid var(--md-sys-color-outline-variant);
      border-radius: var(--md-sys-shape-corner-large);
      padding: 20px 24px;
      margin-bottom: 28px;
      display: flex;
      flex-direction: column;
      gap: 16px;
      box-shadow: var(--md-sys-elevation-1);
    }

    .m3-banner-header {
      display: flex;
      align-items: flex-start;
      justify-content: space-between;
      gap: 16px;
      flex-wrap: wrap;
    }

    .m3-banner-title {
      font-size: 15px;
      font-weight: 700;
      color: var(--md-sys-color-on-surface);
      display: flex;
      align-items: center;
      gap: 8px;
    }

    .m3-banner-desc {
      font-size: 13px;
      color: var(--md-sys-color-on-surface-variant);
      margin-top: 4px;
      line-height: 1.5;
    }

    .m3-banner-actions {
      display: flex;
      gap: 10px;
      flex-wrap: wrap;
    }

    /* Media Grid & Cards */
    .m3-media-grid {
      display: grid;
      grid-template-columns: repeat(auto-fill, minmax(210px, 1fr));
      gap: 22px;
    }

    .m3-media-card {
      background-color: var(--md-sys-color-surface-container-low);
      border: 1px solid var(--md-sys-color-outline-variant);
      border-radius: var(--md-sys-shape-corner-large);
      overflow: hidden;
      display: flex;
      flex-direction: column;
      position: relative;
      box-shadow: var(--md-sys-elevation-1);
      transition: transform var(--md-sys-motion-duration-medium) var(--md-sys-motion-easing-emphasized),
                  box-shadow var(--md-sys-motion-duration-medium) var(--md-sys-motion-easing-emphasized),
                  border-color var(--md-sys-motion-duration-short);
    }

    .m3-media-card:hover {
      transform: translateY(-4px);
      box-shadow: var(--md-sys-elevation-3);
      border-color: var(--md-sys-color-outline);
    }

    .m3-poster-box {
      width: 100%;
      height: 295px;
      background-color: var(--md-sys-color-surface-container-highest);
      position: relative;
      overflow: hidden;
    }

    .m3-poster-img {
      width: 100%;
      height: 100%;
      object-fit: cover;
      transition: transform var(--md-sys-motion-duration-long) var(--md-sys-motion-easing-emphasized);
    }

    .m3-media-card:hover .m3-poster-img {
      transform: scale(1.04);
    }

    .m3-poster-scrim {
      position: absolute;
      bottom: 0;
      left: 0;
      right: 0;
      height: 80px;
      background: linear-gradient(to top, rgba(14, 18, 25, 0.95), transparent);
      pointer-events: none;
    }

    .m3-badge-type {
      position: absolute;
      top: 10px;
      left: 10px;
      padding: 4px 10px;
      border-radius: var(--md-sys-shape-corner-small);
      font-size: 10.5px;
      font-weight: 700;
      letter-spacing: 0.5px;
      text-transform: uppercase;
      backdrop-filter: blur(10px);
      box-shadow: var(--md-sys-elevation-1);
    }

    .m3-badge-movie { background-color: var(--badge-movie-bg); color: var(--badge-movie-fg); }
    .m3-badge-series { background-color: var(--badge-series-bg); color: var(--badge-series-fg); }
    .m3-badge-anime { background-color: var(--badge-anime-bg); color: var(--badge-anime-fg); }

    .m3-rating-badge {
      position: absolute;
      top: 10px;
      right: 10px;
      background-color: rgba(0, 0, 0, 0.75);
      border: 1px solid rgba(255, 255, 255, 0.15);
      backdrop-filter: blur(10px);
      padding: 3px 8px;
      border-radius: var(--md-sys-shape-corner-small);
      font-size: 11px;
      font-weight: 600;
      color: #FFD60A;
      display: flex;
      align-items: center;
      gap: 4px;
      box-shadow: var(--md-sys-elevation-1);
    }

    .m3-card-body {
      padding: 16px;
      display: flex;
      flex-direction: column;
      flex: 1;
      justify-content: space-between;
      gap: 10px;
    }

    .m3-card-title {
      font-size: 14.5px;
      font-weight: 700;
      color: var(--md-sys-color-on-surface);
      white-space: nowrap;
      overflow: hidden;
      text-overflow: ellipsis;
      line-height: 1.3;
    }

    .m3-card-meta {
      font-size: 12px;
      color: var(--md-sys-color-on-surface-variant);
      display: flex;
      align-items: center;
      gap: 6px;
    }

    .m3-card-select {
      width: 100%;
      height: 38px;
      background-color: var(--md-sys-color-surface-container);
      border: 1px solid var(--md-sys-color-outline-variant);
      border-radius: var(--md-sys-shape-corner-small);
      color: var(--md-sys-color-on-surface);
      font-family: inherit;
      font-size: 12px;
      padding: 0 10px;
      outline: none;
      cursor: pointer;
    }

    .m3-card-select:focus {
      border-color: var(--md-sys-color-primary);
    }

    .m3-card-btn {
      width: 100%;
      height: 38px;
      border-radius: var(--md-sys-shape-corner-full);
      font-family: inherit;
      font-size: 12.5px;
      font-weight: 600;
      cursor: pointer;
      display: flex;
      align-items: center;
      justify-content: center;
      gap: 8px;
      border: 1px solid var(--md-sys-color-outline-variant);
      background-color: var(--md-sys-color-surface-container-high);
      color: var(--md-sys-color-on-surface);
      transition: all var(--md-sys-motion-duration-short) var(--md-sys-motion-easing-emphasized);
    }

    .m3-card-btn:hover {
      background-color: var(--md-sys-color-surface-container-highest);
      border-color: var(--md-sys-color-outline);
    }

    .m3-card-btn.add-btn {
      background-color: var(--md-sys-color-primary-container);
      color: var(--md-sys-color-on-primary-container);
      border-color: transparent;
    }

    .m3-card-btn.add-btn:hover {
      background-color: #1A5296;
      box-shadow: var(--md-sys-elevation-1);
    }

    .m3-card-btn.added {
      background-color: rgba(48, 209, 88, 0.15);
      border-color: rgba(48, 209, 88, 0.35);
      color: #30D158;
      cursor: default;
    }

    .m3-card-btn.vlc {
      background-color: var(--md-sys-color-tertiary-container);
      color: var(--md-sys-color-on-tertiary-container);
      border-color: transparent;
    }

    .m3-card-btn.vlc:hover {
      background-color: #8C4700;
      box-shadow: var(--md-sys-elevation-1);
    }

    .m3-card-btn.danger {
      background: none;
      border-color: transparent;
      color: var(--md-sys-color-error);
    }

    .m3-card-btn.danger:hover {
      background-color: rgba(255, 180, 171, 0.1);
    }

    /* Providers Grid & Cards */
    .m3-providers-grid {
      display: grid;
      grid-template-columns: repeat(auto-fill, minmax(290px, 1fr));
      gap: 16px;
    }

    .m3-provider-card {
      background-color: var(--md-sys-color-surface-container-low);
      border: 1px solid var(--md-sys-color-outline-variant);
      border-radius: var(--md-sys-shape-corner-large);
      padding: 16px;
      display: flex;
      align-items: center;
      gap: 14px;
      box-shadow: var(--md-sys-elevation-1);
      transition: all var(--md-sys-motion-duration-short) var(--md-sys-motion-easing-emphasized);
    }

    .m3-provider-card:hover {
      background-color: var(--md-sys-color-surface-container);
      border-color: var(--md-sys-color-outline);
      box-shadow: var(--md-sys-elevation-2);
      transform: translateY(-2px);
    }

    .m3-provider-logo-box {
      width: 44px;
      height: 44px;
      border-radius: var(--md-sys-shape-corner-medium);
      background-color: var(--md-sys-color-surface-container-high);
      display: flex;
      align-items: center;
      justify-content: center;
      flex-shrink: 0;
      overflow: hidden;
      border: 1px solid var(--md-sys-color-outline-variant);
    }

    .m3-provider-logo-img {
      width: 28px;
      height: 28px;
      object-fit: contain;
    }

    .m3-provider-info {
      flex: 1;
      overflow: hidden;
    }

    .m3-provider-name {
      font-size: 14px;
      font-weight: 700;
      color: var(--md-sys-color-on-surface);
      white-space: nowrap;
      overflow: hidden;
      text-overflow: ellipsis;
    }

    .m3-provider-meta {
      font-size: 11.5px;
      color: var(--md-sys-color-on-surface-variant);
      display: flex;
      align-items: center;
      gap: 6px;
      margin-top: 3px;
      white-space: nowrap;
      overflow: hidden;
      text-overflow: ellipsis;
    }

    .m3-provider-cat-tag {
      padding: 1px 6px;
      border-radius: var(--md-sys-shape-corner-extra-small);
      background-color: var(--md-sys-color-surface-container-highest);
      color: var(--md-sys-color-primary);
      font-size: 10px;
      font-weight: 600;
    }

    /* M3 Adaptive Modals & Bottom Sheets */
    .m3-scrim {
      position: fixed;
      top: 0;
      left: 0;
      right: 0;
      bottom: 0;
      background-color: rgba(0, 0, 0, 0.72);
      backdrop-filter: blur(8px);
      -webkit-backdrop-filter: blur(8px);
      z-index: 1000;
      display: flex;
      align-items: center;
      justify-content: center;
      opacity: 0;
      pointer-events: none;
      transition: opacity var(--md-sys-motion-duration-medium) var(--md-sys-motion-easing-emphasized);
      padding: 16px;
    }

    .m3-scrim.open {
      opacity: 1;
      pointer-events: auto;
    }

    .m3-dialog {
      background-color: var(--md-sys-color-surface-container-high);
      border: 1px solid var(--md-sys-color-outline-variant);
      border-radius: var(--md-sys-shape-corner-extra-large);
      box-shadow: var(--md-sys-elevation-4);
      width: 100%;
      max-width: 660px;
      max-height: 88vh;
      overflow-y: auto;
      display: flex;
      flex-direction: column;
      transform: scale(0.92);
      transition: transform var(--md-sys-motion-duration-medium) var(--md-sys-motion-easing-emphasized);
    }

    .m3-scrim.open .m3-dialog {
      transform: scale(1);
    }

    .m3-sheet-handle {
      display: none;
      width: 36px;
      height: 4px;
      border-radius: 2px;
      background-color: var(--md-sys-color-outline-variant);
      margin: 12px auto 4px auto;
    }

    .m3-dialog-header {
      padding: 24px 24px 16px;
      display: flex;
      align-items: center;
      justify-content: space-between;
      border-bottom: 1px solid var(--md-sys-color-outline-variant);
    }

    .m3-dialog-title {
      font-size: 20px;
      font-weight: 700;
      color: var(--md-sys-color-on-surface);
      display: flex;
      align-items: center;
      gap: 10px;
    }

    .m3-dialog-body {
      padding: 24px;
      display: flex;
      flex-direction: column;
      gap: 16px;
      overflow-y: auto;
    }

    .m3-source-row {
      display: flex;
      align-items: center;
      justify-content: space-between;
      padding: 14px 16px;
      border-radius: var(--md-sys-shape-corner-medium);
      background-color: var(--md-sys-color-surface-container);
      border: 1px solid var(--md-sys-color-outline-variant);
      gap: 12px;
      flex-wrap: wrap;
    }

    .m3-source-row-info {
      flex: 1;
      min-width: 200px;
    }

    .m3-source-row-name {
      font-size: 13.5px;
      font-weight: 700;
      color: var(--md-sys-color-on-surface);
      display: flex;
      align-items: center;
      gap: 8px;
    }

    .m3-source-row-sub {
      font-size: 11.5px;
      color: var(--md-sys-color-on-surface-variant);
      margin-top: 2px;
    }

    .m3-source-row-actions {
      display: flex;
      align-items: center;
      gap: 8px;
      flex-wrap: wrap;
    }

    /* M3 Outlined Text Field */
    .m3-text-field {
      display: flex;
      flex-direction: column;
      gap: 6px;
    }

    .m3-text-field-label {
      font-size: 12px;
      font-weight: 500;
      color: var(--md-sys-color-primary);
    }

    .m3-text-field-input {
      width: 100%;
      height: 48px;
      padding: 0 16px;
      border-radius: var(--md-sys-shape-corner-small);
      background-color: var(--md-sys-color-surface-container);
      border: 1px solid var(--md-sys-color-outline);
      color: var(--md-sys-color-on-surface);
      font-family: inherit;
      font-size: 14px;
      outline: none;
      transition: border-color var(--md-sys-motion-duration-short);
    }

    .m3-text-field-input:focus {
      border-color: var(--md-sys-color-primary);
      border-width: 2px;
    }

    /* M3 Code Snippet Container */
    .m3-code-snippet {
      background-color: var(--md-sys-color-surface-container-lowest);
      border: 1px solid var(--md-sys-color-outline-variant);
      border-radius: var(--md-sys-shape-corner-medium);
      padding: 14px 16px;
      display: flex;
      align-items: center;
      justify-content: space-between;
      gap: 12px;
      font-family: ui-monospace, SFMono-Regular, Menlo, Monaco, Consolas, monospace;
      font-size: 12.5px;
      color: #93C5FD;
      word-break: break-all;
    }

    /* M3 Snackbar (Toast) */
    .m3-snackbar {
      position: fixed;
      bottom: 28px;
      left: 50%;
      transform: translateX(-50%) translateY(40px);
      background-color: var(--md-sys-color-inverse-surface);
      color: var(--md-sys-color-inverse-on-surface);
      padding: 14px 22px;
      border-radius: var(--md-sys-shape-corner-extra-small);
      font-size: 13.5px;
      font-weight: 500;
      box-shadow: var(--md-sys-elevation-3);
      display: flex;
      align-items: center;
      gap: 12px;
      z-index: 2000;
      opacity: 0;
      pointer-events: none;
      transition: all var(--md-sys-motion-duration-medium) var(--md-sys-motion-easing-emphasized);
    }

    .m3-snackbar.show {
      transform: translateX(-50%) translateY(0);
      opacity: 1;
      pointer-events: auto;
    }

    /* Mobile Bottom Navigation Bar (< 840px) */
    .m3-bottom-nav {
      display: none;
      position: fixed;
      bottom: 0;
      left: 0;
      right: 0;
      height: 76px;
      background-color: var(--md-sys-color-surface-container);
      border-top: 1px solid var(--md-sys-color-outline-variant);
      z-index: 95;
      padding-bottom: env(safe-area-inset-bottom, 0px);
      align-items: center;
      justify-content: space-around;
    }

    .m3-bottom-item {
      display: flex;
      flex-direction: column;
      align-items: center;
      justify-content: center;
      flex: 1;
      height: 100%;
      background: none;
      border: none;
      color: var(--md-sys-color-on-surface-variant);
      cursor: pointer;
      gap: 4px;
      padding: 8px 0;
      transition: color var(--md-sys-motion-duration-short);
      font-family: inherit;
    }

    .m3-bottom-indicator {
      width: 64px;
      height: 32px;
      border-radius: var(--md-sys-shape-corner-full);
      display: flex;
      align-items: center;
      justify-content: center;
      position: relative;
      transition: all var(--md-sys-motion-duration-medium) var(--md-sys-motion-easing-emphasized);
    }

    .m3-bottom-item.active {
      color: var(--md-sys-color-on-surface);
    }

    .m3-bottom-item.active .m3-bottom-indicator {
      background-color: var(--md-sys-color-secondary-container);
      color: var(--md-sys-color-on-secondary-container);
      box-shadow: var(--md-sys-elevation-1);
    }

    .m3-bottom-label {
      font-size: 11.5px;
      font-weight: 500;
    }

    .m3-bottom-badge {
      position: absolute;
      top: -2px;
      right: 12px;
      background-color: var(--md-sys-color-error);
      color: var(--md-sys-color-on-error);
      font-size: 10px;
      font-weight: 700;
      min-width: 18px;
      height: 18px;
      line-height: 18px;
      border-radius: 9px;
      text-align: center;
      padding: 0 4px;
    }

    /* Empty States */
    .m3-empty-state {
      display: flex;
      flex-direction: column;
      align-items: center;
      justify-content: center;
      padding: 60px 20px;
      text-align: center;
      color: var(--md-sys-color-on-surface-variant);
      gap: 12px;
    }

    .m3-empty-icon {
      width: 56px;
      height: 56px;
      color: var(--md-sys-color-outline);
    }

    /* Responsive Rules */
    @media (max-width: 839px) {
      .m3-nav-rail {
        display: none;
      }
      .m3-main-shell {
        margin-left: 0;
        padding-bottom: 84px;
      }
      .m3-bottom-nav {
        display: flex;
      }
      .m3-mobile-logo {
        display: block;
      }
      .m3-top-app-bar {
        padding: 0 16px;
      }
      .m3-content-area {
        padding: 20px 16px 88px;
      }
      .m3-media-grid {
        grid-template-columns: repeat(auto-fill, minmax(160px, 1fr));
        gap: 14px;
      }
      .m3-poster-box {
        height: 230px;
      }
      .m3-search-shortcut {
        display: none;
      }
      .m3-snackbar {
        bottom: 96px;
      }

      /* M3 Modal Bottom Sheet in Mobile */
      .m3-scrim {
        align-items: flex-end;
        padding: 0;
      }
      .m3-dialog {
        border-radius: 28px 28px 0 0;
        max-height: 85vh;
        transform: translateY(100%);
        border-bottom: none;
      }
      .m3-scrim.open .m3-dialog {
        transform: translateY(0);
      }
      .m3-sheet-handle {
        display: block;
      }
    }
  </style>
</head>
<body>
  <div class="m3-app-root">
    <!-- Desktop Navigation Rail (>= 840px) -->
    <aside class="m3-nav-rail">
      <div class="m3-rail-brand" title="Anthology">
        <img src="/logo.png" class="m3-brand-logo" alt="Anthology Logo">
      </div>
      <nav class="m3-rail-nav">
        <button class="m3-rail-item active" onclick="switchTab('search')" id="railTabSearch" title="Keşfet ve Ara (⌘K)">
          <div class="m3-rail-indicator m3-ripple-container">
            <svg class="m3-icon" viewBox="0 0 24 24"><path d="M15.5 14h-.79l-.28-.27A6.471 6.471 0 0 0 16 9.5 6.5 6.5 0 1 0 9.5 16c1.61 0 3.09-.59 4.23-1.57l.27.28v.79l5 4.99L20.49 19l-4.99-5zm-6 0C7.01 14 5 11.99 5 9.5S7.01 5 9.5 5 14 7.01 14 9.5 11.99 14 9.5 14z"/></svg>
          </div>
          <span class="m3-rail-label">Keşfet</span>
        </button>

        <button class="m3-rail-item" onclick="switchTab('library')" id="railTabLibrary" title="Kütüphanem">
          <div class="m3-rail-indicator m3-ripple-container">
            <svg class="m3-icon" viewBox="0 0 24 24"><path d="M4 6H2v14c0 1.1.9 2 2 2h14v-2H4V6zm16-4H8c-1.1 0-2 .9-2 2v12c0 1.1.9 2 2 2h12c1.1 0 2-.9 2-2V4c0-1.1-.9-2-2-2zm-8 12.5v-9l6 4.5-6 4.5z"/></svg>
            <span class="m3-rail-badge" id="railLibCountBadge">0</span>
          </div>
          <span class="m3-rail-label">Kütüphane</span>
        </button>

        <button class="m3-rail-item" onclick="switchTab('providers')" id="railTabProviders" title="Sağlayıcılar">
          <div class="m3-rail-indicator m3-ripple-container">
            <svg class="m3-icon" viewBox="0 0 24 24"><path d="M20.5 11H19V7c0-1.1-.9-2-2-2h-4V3.5C13 2.12 11.88 1 10.5 1S8 2.12 8 3.5V5H4c-1.1 0-1.99.9-1.99 2v3.8H3.5c1.49 0 2.7 1.21 2.7 2.7s-1.21 2.7-2.7 2.7H2V20c0 1.1.9 2 2 2h3.8v-1.5c0-1.49 1.21-2.7 2.7-2.7 1.49 0 2.7 1.21 2.7 2.7V22H17c1.1 0 2-.9 2-2v-4h1.5c1.38 0 2.5-1.12 2.5-2.5s-1.12-2.5-2.5-2.5z"/></svg>
          </div>
          <span class="m3-rail-label">Sağlayıcılar</span>
        </button>
      </nav>

      <div class="m3-rail-footer">
        <button class="m3-icon-btn m3-ripple-container" onclick="openConnectModal()" title="VLC, TV ve Ağ Bağlantı Rehberi">
          <svg class="m3-icon" viewBox="0 0 24 24"><path d="M4 6h18V4H4c-1.1 0-2 .9-2 2v11H0v3h14v-3H4V6zm19 2h-6c-.55 0-1 .45-1 1v10c0 .55.45 1 1 1h6c.55 0 1-.45 1-1V9c0-.55-.45-1-1-1zm-1 9h-4v-7h4v7z"/></svg>
        </button>
        <div id="railUserHeaderBox"></div>
      </div>
    </aside>

    <!-- Main Shell -->
    <div class="m3-main-shell">
      <!-- Top App Bar -->
      <header class="m3-top-app-bar">
        <div class="m3-top-brand">
          <img src="/logo.png" class="m3-mobile-logo" alt="Anthology">
          <span class="m3-top-title">Anthology</span>
          <div class="m3-status-chip">
            <span class="m3-status-dot"></span>
            <span>WebDAV Çevrimiçi</span>
          </div>
        </div>

        <div class="m3-top-actions">
          <button class="m3-pill-btn m3-ripple-container" onclick="openConnectModal()" title="VLC, TV ve Ağ Bağlantısı">
            <svg class="m3-icon" style="width:18px;height:18px;" viewBox="0 0 24 24"><path d="M4 6h18V4H4c-1.1 0-2 .9-2 2v11H0v3h14v-3H4V6zm19 2h-6c-.55 0-1 .45-1 1v10c0 .55.45 1 1 1h6c.55 0 1-.45 1-1V9c0-.55-.45-1-1-1zm-1 9h-4v-7h4v7z"/></svg>
            <span id="headerLanBadge">VLC & Cihazlar</span>
          </button>
          <div id="topUserHeaderBox"></div>
        </div>
      </header>

      <!-- Content Area -->
      <main class="m3-content-area">
        <!-- TAB 1: SEARCH & DISCOVER -->
        <section id="tab-search" class="m3-tab-pane active">
          <!-- Floating M3 Search Bar -->
          <div class="m3-search-anchor">
            <div class="m3-search-bar">
              <svg class="m3-icon" style="color:var(--md-sys-color-on-surface-variant);" viewBox="0 0 24 24"><path d="M15.5 14h-.79l-.28-.27A6.471 6.471 0 0 0 16 9.5 6.5 6.5 0 1 0 9.5 16c1.61 0 3.09-.59 4.23-1.57l.27.28v.79l5 4.99L20.49 19l-4.99-5zm-6 0C7.01 14 5 11.99 5 9.5S7.01 5 9.5 5 14 7.01 14 9.5 11.99 14 9.5 14z"/></svg>
              <input type="text" id="searchInput" class="m3-search-input" placeholder="Film, dizi veya anime ara (örn: Solo Leveling, Inception, Breaking Bad)..." autocomplete="off" autofocus>
              <button class="m3-search-clear" id="searchClearBtn" onclick="clearSearch()" title="Temizle">
                <svg class="m3-icon" style="width:18px;height:18px;" viewBox="0 0 24 24"><path d="M19 6.41L17.59 5 12 10.59 6.41 5 5 6.41 10.59 12 5 17.59 6.41 19 12 13.41 17.59 19 19 17.59 13.41 12z"/></svg>
              </button>
              <span class="m3-search-shortcut">⌘K</span>
            </div>

            <!-- Category Filter Chips -->
            <div class="m3-chips-row">
              <button class="m3-filter-chip active m3-ripple-container" onclick="filterSearchResults('all', this)">
                <svg class="m3-icon m3-chip-check" viewBox="0 0 24 24"><path d="M9 16.17L4.83 12l-1.42 1.41L9 19 21 7l-1.41-1.41z"/></svg>
                <span>Tümü</span>
              </button>
              <button class="m3-filter-chip m3-ripple-container" onclick="filterSearchResults('movie', this)">
                <svg class="m3-icon m3-chip-check" viewBox="0 0 24 24"><path d="M9 16.17L4.83 12l-1.42 1.41L9 19 21 7l-1.41-1.41z"/></svg>
                <span>🎬 Filmler</span>
              </button>
              <button class="m3-filter-chip m3-ripple-container" onclick="filterSearchResults('series', this)">
                <svg class="m3-icon m3-chip-check" viewBox="0 0 24 24"><path d="M9 16.17L4.83 12l-1.42 1.41L9 19 21 7l-1.41-1.41z"/></svg>
                <span>📺 Diziler</span>
              </button>
              <button class="m3-filter-chip m3-ripple-container" onclick="filterSearchResults('anime', this)">
                <svg class="m3-icon m3-chip-check" viewBox="0 0 24 24"><path d="M9 16.17L4.83 12l-1.42 1.41L9 19 21 7l-1.41-1.41z"/></svg>
                <span>⛩️ Animeler</span>
              </button>
            </div>
          </div>

          <div class="m3-section-header">
            <h2 class="m3-section-title" id="searchHeader">Arama Sonuçları</h2>
            <span class="m3-section-subtitle" id="searchResultCount"></span>
          </div>

          <div id="searchResults" class="m3-media-grid"></div>
        </section>

        <!-- TAB 2: MY LIBRARY -->
        <section id="tab-library" class="m3-tab-pane">
          <!-- VLC & LAN Quick Connect Card -->
          <div class="m3-banner-card">
            <div class="m3-banner-header">
              <div>
                <div class="m3-banner-title">
                  <span style="color:#FFB786;">🟠</span> VLC, Tablet, Telefon & TV Kolay Erişim
                </div>
                <div class="m3-banner-desc">
                  Android VLC'de WebDAV tarayıcısı bulunmadığından içerikleri <strong>"▶ VLC'de Oynat"</strong> butonuyla doğrudan açabilir veya VLC <strong>Akışlar (Streams)</strong> sekmesine kütüphane M3U adresini ekleyebilirsiniz.
                </div>
              </div>
              <div class="m3-banner-actions">
                <button class="m3-pill-btn vlc m3-ripple-container" onclick="openFullPlaylistInVLC()">
                  <svg class="m3-icon" style="width:16px;height:16px;" viewBox="0 0 24 24"><path d="M8 5v14l11-7z"/></svg>
                  <span>Tümünü VLC'de Aç</span>
                </button>
                <button class="m3-pill-btn primary m3-ripple-container" onclick="copyEndpointPath('/playlist.m3u', 'VLC M3U adresi kopyalandı!')">
                  <svg class="m3-icon" style="width:16px;height:16px;" viewBox="0 0 24 24"><path d="M16 1H4c-1.1 0-2 .9-2 2v14h2V3h12V1zm3 4H8c-1.1 0-2 .9-2 2v14c0 1.1.9 2 2 2h11c1.1 0 2-.9 2-2V7c0-1.1-.9-2-2-2zm0 16H8V7h11v14z"/></svg>
                  <span>M3U Kopyala</span>
                </button>
                <button class="m3-pill-btn m3-ripple-container" onclick="openConnectModal()">
                  <svg class="m3-icon" style="width:16px;height:16px;" viewBox="0 0 24 24"><path d="M12 2C6.48 2 2 6.48 2 12s4.48 10 10 10 10-4.48 10-10S17.52 2 12 2zm1 15h-2v-6h2v6zm0-8h-2V7h2v2z"/></svg>
                  <span>Rehber</span>
                </button>
              </div>
            </div>
          </div>

          <div class="m3-section-header">
            <h2 class="m3-section-title">Kütüphanemdeki İçerikler</h2>
            <div class="m3-chips-row" style="margin-top:0;">
              <button class="m3-filter-chip active m3-ripple-container" onclick="filterLibrary('all', this)">
                <svg class="m3-icon m3-chip-check" viewBox="0 0 24 24"><path d="M9 16.17L4.83 12l-1.42 1.41L9 19 21 7l-1.41-1.41z"/></svg>
                <span>Tümü</span>
              </button>
              <button class="m3-filter-chip m3-ripple-container" onclick="filterLibrary('movie', this)">
                <svg class="m3-icon m3-chip-check" viewBox="0 0 24 24"><path d="M9 16.17L4.83 12l-1.42 1.41L9 19 21 7l-1.41-1.41z"/></svg>
                <span>🎬 Filmler</span>
              </button>
              <button class="m3-filter-chip m3-ripple-container" onclick="filterLibrary('series', this)">
                <svg class="m3-icon m3-chip-check" viewBox="0 0 24 24"><path d="M9 16.17L4.83 12l-1.42 1.41L9 19 21 7l-1.41-1.41z"/></svg>
                <span>📺 Diziler</span>
              </button>
              <button class="m3-filter-chip m3-ripple-container" onclick="filterLibrary('anime', this)">
                <svg class="m3-icon m3-chip-check" viewBox="0 0 24 24"><path d="M9 16.17L4.83 12l-1.42 1.41L9 19 21 7l-1.41-1.41z"/></svg>
                <span>⛩️ Animeler</span>
              </button>
            </div>
          </div>

          <div id="libraryGrid" class="m3-media-grid"></div>
        </section>

        <!-- TAB 3: PROVIDERS -->
        <section id="tab-providers" class="m3-tab-pane">
          <div class="m3-section-header">
            <h2 class="m3-section-title">Kayıtlı 34 Sağlayıcı Motoru</h2>
            <span class="m3-section-subtitle">Anthology Akıllı Çözücü Aktif</span>
          </div>
          <div id="providersGrid" class="m3-providers-grid"></div>
        </section>
      </main>
    </div>

    <!-- Mobile Bottom Navigation Bar (< 840px) -->
    <nav class="m3-bottom-nav">
      <button class="m3-bottom-item active" onclick="switchTab('search')" id="bottomTabSearch">
        <div class="m3-bottom-indicator m3-ripple-container">
          <svg class="m3-icon" viewBox="0 0 24 24"><path d="M15.5 14h-.79l-.28-.27A6.471 6.471 0 0 0 16 9.5 6.5 6.5 0 1 0 9.5 16c1.61 0 3.09-.59 4.23-1.57l.27.28v.79l5 4.99L20.49 19l-4.99-5zm-6 0C7.01 14 5 11.99 5 9.5S7.01 5 9.5 5 14 7.01 14 9.5 11.99 14 9.5 14z"/></svg>
        </div>
        <span class="m3-bottom-label">Keşfet</span>
      </button>

      <button class="m3-bottom-item" onclick="switchTab('library')" id="bottomTabLibrary">
        <div class="m3-bottom-indicator m3-ripple-container">
          <svg class="m3-icon" viewBox="0 0 24 24"><path d="M4 6H2v14c0 1.1.9 2 2 2h14v-2H4V6zm16-4H8c-1.1 0-2 .9-2 2v12c0 1.1.9 2 2 2h12c1.1 0 2-.9 2-2V4c0-1.1-.9-2-2-2zm-8 12.5v-9l6 4.5-6 4.5z"/></svg>
          <span class="m3-bottom-badge" id="bottomLibCountBadge">0</span>
        </div>
        <span class="m3-bottom-label">Kütüphanem</span>
      </button>

      <button class="m3-bottom-item" onclick="switchTab('providers')" id="bottomTabProviders">
        <div class="m3-bottom-indicator m3-ripple-container">
          <svg class="m3-icon" viewBox="0 0 24 24"><path d="M20.5 11H19V7c0-1.1-.9-2-2-2h-4V3.5C13 2.12 11.88 1 10.5 1S8 2.12 8 3.5V5H4c-1.1 0-1.99.9-1.99 2v3.8H3.5c1.49 0 2.7 1.21 2.7 2.7s-1.21 2.7-2.7 2.7H2V20c0 1.1.9 2 2 2h3.8v-1.5c0-1.49 1.21-2.7 2.7-2.7 1.49 0 2.7 1.21 2.7 2.7V22H17c1.1 0 2-.9 2-2v-4h1.5c1.38 0 2.5-1.12 2.5-2.5s-1.12-2.5-2.5-2.5z"/></svg>
        </div>
        <span class="m3-bottom-label">Sağlayıcılar</span>
      </button>
    </nav>
  </div>

  <!-- MODAL 1: AUTH DIALOG / BOTTOM SHEET -->
  <div class="m3-scrim" id="authModal" onclick="if(event.target===this && !authRequired) closeAuthModal()">
    <div class="m3-dialog" style="max-width: 440px;">
      <div class="m3-sheet-handle"></div>
      <div class="m3-dialog-header">
        <div class="m3-dialog-title" id="authModalTitle">Anthology Hesabı</div>
        <button class="m3-icon-btn" id="authModalClose" onclick="closeAuthModal()" title="Kapat">
          <svg class="m3-icon" viewBox="0 0 24 24"><path d="M19 6.41L17.59 5 12 10.59 6.41 5 5 6.41 10.59 12 5 17.59 6.41 19 12 13.41 17.59 19 19 17.59 13.41 12z"/></svg>
        </button>
      </div>
      <div class="m3-dialog-body">
        <div class="m3-chips-row" style="margin-top:0; justify-content:stretch;">
          <button class="m3-filter-chip active m3-ripple-container" id="authTabLogin" style="flex:1; justify-content:center;" onclick="switchAuthMode('login')">Giriş Yap</button>
          <button class="m3-filter-chip m3-ripple-container" id="authTabRegister" style="flex:1; justify-content:center;" onclick="switchAuthMode('register')">Yeni Kayıt</button>
        </div>
        <form onsubmit="handleAuthSubmit(event)" style="display:flex; flex-direction:column; gap:16px;">
          <div class="m3-text-field">
            <label class="m3-text-field-label" for="authUsername">Kullanıcı Adı</label>
            <input type="text" id="authUsername" class="m3-text-field-input" required autocomplete="username">
          </div>
          <div class="m3-text-field">
            <label class="m3-text-field-label" for="authPassword">Şifre</label>
            <input type="password" id="authPassword" class="m3-text-field-input" required autocomplete="current-password">
          </div>
          <div id="authErrorMsg" style="display:none; color:var(--md-sys-color-error); font-size:12.5px; background:rgba(255,180,171,0.12); padding:10px 14px; border-radius:8px;"></div>
          <button type="submit" class="m3-pill-btn primary m3-ripple-container" style="height:46px; justify-content:center; margin-top:8px;">
            <span id="authSubmitText">Giriş Yap</span>
          </button>
        </form>
      </div>
    </div>
  </div>

  <!-- MODAL 2: CONNECT / DEVICES / VLC GUIDE -->
  <div class="m3-scrim" id="connectModal" onclick="if(event.target===this) closeConnectModal()">
    <div class="m3-dialog">
      <div class="m3-sheet-handle"></div>
      <div class="m3-dialog-header">
        <div class="m3-dialog-title">
          <svg class="m3-icon" style="color:var(--md-sys-color-primary);" viewBox="0 0 24 24"><path d="M4 6h18V4H4c-1.1 0-2 .9-2 2v11H0v3h14v-3H4V6zm19 2h-6c-.55 0-1 .45-1 1v10c0 .55.45 1 1 1h6c.55 0 1-.45 1-1V9c0-.55-.45-1-1-1zm-1 9h-4v-7h4v7z"/></svg>
          <span>Cihaz & VLC Bağlantı Rehberi</span>
        </div>
        <button class="m3-icon-btn" onclick="closeConnectModal()" title="Kapat">
          <svg class="m3-icon" viewBox="0 0 24 24"><path d="M19 6.41L17.59 5 12 10.59 6.41 5 5 6.41 10.59 12 5 17.59 6.41 19 12 13.41 17.59 19 19 17.59 13.41 12z"/></svg>
        </button>
      </div>
      <div class="m3-dialog-body">
        <div>
          <div style="font-size:14px; font-weight:700; color:var(--md-sys-color-on-surface); margin-bottom:6px;">1. VLC Streams (Android, iOS, iPadOS, Smart TV, PC)</div>
          <div style="font-size:12.5px; color:var(--md-sys-color-on-surface-variant); margin-bottom:8px;">
            VLC uygulamasında <strong>Daha Fazla / Akışlar (Streams)</strong> sekmesine gidip aşağıdaki M3U bağlantısını yapıştırın:
          </div>
          <div class="m3-code-snippet">
            <span id="connectVlcCode">http://.../playlist.m3u</span>
            <button class="m3-pill-btn primary m3-ripple-container" style="height:32px; padding:0 12px; font-size:11.5px;" onclick="copyEndpointPath('/playlist.m3u', 'VLC M3U adresi kopyalandı!')">Kopyala</button>
          </div>
          <div id="btnCopyVlcToken" style="display:none; margin-top:8px;">
            <button class="m3-pill-btn vlc m3-ripple-container" style="height:34px; font-size:12px;" onclick="copyVlcTokenPlaylist()">
              🔑 Şifresiz Tokenli VLC Linkini Kopyala
            </button>
          </div>
        </div>

        <div>
          <div style="font-size:14px; font-weight:700; color:var(--md-sys-color-on-surface); margin-bottom:6px;">2. WebDAV Oynatıcılar (Infuse, Kodi, VidHub, Nova)</div>
          <div style="font-size:12.5px; color:var(--md-sys-color-on-surface-variant); margin-bottom:8px;">
            Infuse veya Kodi'de <strong>WebDAV</strong> kaynağı ekleyin:
          </div>
          <div class="m3-code-snippet">
            <span id="connectDavCode">http://.../dav/</span>
            <button class="m3-pill-btn primary m3-ripple-container" style="height:32px; padding:0 12px; font-size:11.5px;" onclick="copyEndpointPath('/dav/', 'WebDAV adresi kopyalandı!')">Kopyala</button>
          </div>
        </div>

        <div style="background:var(--md-sys-color-surface-container); border-radius:12px; padding:14px; border:1px solid var(--md-sys-color-outline-variant);">
          <div style="font-size:13px; font-weight:700; color:var(--md-sys-color-primary); margin-bottom:4px;">💡 İpucu: Tek Tıkla VLC</div>
          <div style="font-size:12px; color:var(--md-sys-color-on-surface-variant); line-height:1.5;">
            Android, iPhone veya Mac cihazınızda bir içeriği oynatmak için hiçbir şey kopyalamadan doğrudan web arayüzündeki <strong>"▶ VLC'de Oynat"</strong> butonuna basabilirsiniz. Cihazınızdaki VLC uygulaması otomatik olarak başlayacaktır.
          </div>
        </div>
      </div>
    </div>
  </div>

  <!-- MODAL 3: SOURCE FINDER & PLAY MODAL -->
  <div class="m3-scrim" id="sourceModal" onclick="if(event.target===this) closeSourceModal()">
    <div class="m3-dialog">
      <div class="m3-sheet-handle"></div>
      <div class="m3-dialog-header">
        <div class="m3-dialog-title" id="modalItemTitle">Bölüm Seç & Oynat</div>
        <button class="m3-icon-btn" onclick="closeSourceModal()" title="Kapat">
          <svg class="m3-icon" viewBox="0 0 24 24"><path d="M19 6.41L17.59 5 12 10.59 6.41 5 5 6.41 10.59 12 5 17.59 6.41 19 12 13.41 17.59 19 19 17.59 13.41 12z"/></svg>
        </button>
      </div>

      <!-- Episode bar for series & anime -->
      <div id="modalEpisodeBar" style="padding: 12px 24px; display:flex; align-items:center; gap:12px; background-color:var(--md-sys-color-surface-container); border-bottom:1px solid var(--md-sys-color-outline-variant); flex-wrap:wrap;">
        <span id="lblSeason" style="font-size:12.5px; font-weight:600; color:var(--md-sys-color-on-surface-variant);">Sezon:</span>
        <input type="number" id="modalSeason" class="m3-text-field-input" style="width:70px; height:36px; padding:0 8px; text-align:center;" value="1" min="1">
        <span id="lblEpisode" style="font-size:12.5px; font-weight:600; color:var(--md-sys-color-on-surface-variant);">Bölüm:</span>
        <input type="number" id="modalEpisode" class="m3-text-field-input" style="width:70px; height:36px; padding:0 8px; text-align:center;" value="1" min="1">
        <button class="m3-pill-btn primary m3-ripple-container" style="height:36px; padding:0 14px; font-size:12px;" onclick="scanSourcesForModal()">
          <svg class="m3-icon" style="width:16px;height:16px;" viewBox="0 0 24 24"><path d="M12 4V1L8 5l4 4V6c3.31 0 6 2.69 6 6 0 1.01-.25 1.97-.7 2.8l1.46 1.46A7.93 7.93 0 0 0 20 12c0-4.42-3.58-8-8-8zm0 14c-3.31 0-6-2.69-6-6 0-1.01.25-1.97.7-2.8L5.24 7.74A7.93 7.93 0 0 0 4 12c0 4.42 3.58 8 8 8v3l4-4-4-4v3z"/></svg>
          <span>Taramayı Yenile</span>
        </button>
        <button class="m3-pill-btn vlc m3-ripple-container" style="height:36px; padding:0 14px; font-size:12px;" onclick="playModalCurrentInVLC()">
          ▶ Hemen VLC'de Aç
        </button>
        <button class="m3-pill-btn m3-ripple-container" style="height:36px; padding:0 12px; font-size:12px;" onclick="copyItemM3UPlaylist()" title="Tüm sezon ve bölümlerin M3U listesini kopyala">
          📋 M3U Listesi
        </button>
      </div>

      <div class="m3-dialog-body" id="modalSourceList"></div>
    </div>
  </div>

  <!-- M3 SNACKBAR (TOAST) -->
  <div class="m3-snackbar" id="m3Snackbar">
    <span id="snackbarIcon">✓</span>
    <span id="snackbarMsg">İşlem tamamlandı.</span>
  </div>

  <script>
    let currentTab = 'search';
    let libraryItems = [];
    let providerList = [];
    let searchTimeout = null;
    let activeFilter = 'all';
    let activeSearchFilter = 'all';
    let lastSearchResults = [];
    let activeModalItem = null;
    let serverLanInfo = null;

    // Multi-User Authentication State
    let isMultiUser = false;
    let currentUser = null;
    let authRequired = false;
    let currentAuthMode = 'login';
    let authToken = localStorage.getItem('anthology_token') || '';

    function authHeaders() {
      const h = { 'Content-Type': 'application/json' };
      if (authToken) {
        h['Authorization'] = 'Bearer ' + authToken;
      }
      return h;
    }

    // Material 3 Ripple Effect Implementation
    document.addEventListener('pointerdown', (e) => {
      const target = e.target.closest('.m3-ripple-container, .m3-pill-btn, .m3-filter-chip, .m3-card-btn, .m3-rail-item, .m3-bottom-item, .m3-icon-btn, .m3-media-card');
      if (!target) return;
      
      const rect = target.getBoundingClientRect();
      const circle = document.createElement('span');
      const diameter = Math.max(rect.width, rect.height);
      const radius = diameter / 2;
      
      circle.style.width = circle.style.height = diameter + 'px';
      circle.style.left = (e.clientX - rect.left - radius) + 'px';
      circle.style.top = (e.clientY - rect.top - radius) + 'px';
      circle.classList.add('m3-ripple-wave');
      
      const existing = target.getElementsByClassName('m3-ripple-wave')[0];
      if (existing) existing.remove();
      
      target.appendChild(circle);
      setTimeout(() => circle.remove(), 580);
    });

    // Hotkey: Command+K / Ctrl+K to focus search
    window.addEventListener('keydown', (e) => {
      if ((e.metaKey || e.ctrlKey) && e.key === 'k') {
        e.preventDefault();
        switchTab('search');
        const input = document.getElementById('searchInput');
        input.focus();
        input.select();
      }
      if (e.key === 'Escape') {
        closeSourceModal();
        closeConnectModal();
        if (!authRequired) closeAuthModal();
      }
    });

    function escapeHtml(str) {
      if (!str) return '';
      return String(str).replace(/[&<>"']/g, s => ({'&':'&amp;','<':'&lt;','>':'&gt;','"':'&quot;',"'":'&#39;'})[s]);
    }

    function getReachableBaseOrigin() {
      const loc = window.location.origin;
      if ((loc.includes('127.0.0.1') || loc.includes('localhost')) && serverLanInfo && serverLanInfo.primary_url) {
        return serverLanInfo.primary_url;
      }
      return loc;
    }

    async function loadServerStatus() {
      try {
        const res = await fetch('/api/status', { headers: authHeaders() });
        const data = await res.json();
        if (data && data.lan) {
          serverLanInfo = data.lan;
        }
        if (data && data.multi_user !== undefined) {
          isMultiUser = !!data.multi_user;
        }
      } catch (e) {}
    }

    async function checkAuthMe() {
      try {
        const res = await fetch('/api/auth/me', { headers: authHeaders() });
        const data = await res.json();
        isMultiUser = !!data.multi_user;
        if (data.user && data.user.username && data.user.username !== 'local') {
          currentUser = data.user;
          authRequired = false;
        } else {
          currentUser = null;
        }
        updateUserHeader();
      } catch (e) {
        currentUser = null;
        updateUserHeader();
      }
    }

    function updateUserHeader() {
      const topBox = document.getElementById('topUserHeaderBox');
      const railBox = document.getElementById('railUserHeaderBox');
      
      const userHtml = (!isMultiUser)
        ? '<div class="m3-pill-btn" style="color:var(--md-sys-color-on-surface-variant); cursor:default; height:36px; padding:0 12px; font-size:11.5px;">Yerel Mod</div>'
        : (currentUser
            ? '<div style="display:flex; align-items:center; gap:8px;">' +
                '<span class="m3-pill-btn" style="height:36px; padding:0 12px; font-size:12px; border-color:var(--md-sys-color-primary);">' +
                  '👤 <strong>' + escapeHtml(currentUser.username) + '</strong>' +
                '</span>' +
                '<button class="m3-icon-btn" onclick="handleLogout()" title="Çıkış Yap">' +
                  '<svg class="m3-icon" viewBox="0 0 24 24"><path d="M17 7l-1.41 1.41L18.17 11H8v2h10.17l-2.58 2.58L17 17l5-5zM4 5h8V3H4c-1.1 0-2 .9-2 2v14c0 1.1.9 2 2 2h8v-2H4V5z"/></svg>' +
                '</button>' +
              '</div>'
            : '<button class="m3-pill-btn primary m3-ripple-container" onclick="openAuthModal(\'login\', false)" style="height:36px; padding:0 14px; font-size:12.5px;">Giriş Yap</button>'
          );

      if (topBox) topBox.innerHTML = userHtml;
      if (railBox) {
        if (!isMultiUser) {
          railBox.innerHTML = '';
        } else if (currentUser) {
          railBox.innerHTML = '<button class="m3-icon-btn m3-ripple-container" onclick="handleLogout()" title="' + escapeHtml(currentUser.username) + ' (Çıkış)">' +
            '<svg class="m3-icon" viewBox="0 0 24 24"><path d="M12 12c2.21 0 4-1.79 4-4s-1.79-4-4-4-4 1.79-4 4 1.79 4 4 4zm0 2c-2.67 0-8 1.34-8 4v2h16v-2c0-2.66-5.33-4-8-4z"/></svg>' +
          '</button>';
        } else {
          railBox.innerHTML = '<button class="m3-icon-btn m3-ripple-container" onclick="openAuthModal(\'login\', false)" title="Giriş Yap">' +
            '<svg class="m3-icon" viewBox="0 0 24 24"><path d="M12 12c2.21 0 4-1.79 4-4s-1.79-4-4-4-4 1.79-4 4 1.79 4 4 4zm0 2c-2.67 0-8 1.34-8 4v2h16v-2c0-2.66-5.33-4-8-4z"/></svg>' +
          '</button>';
        }
      }
    }

    function openAuthModal(mode = 'login', required = false) {
      authRequired = required;
      document.getElementById('authModalClose').style.display = required ? 'none' : 'flex';
      switchAuthMode(mode);
      document.getElementById('authErrorMsg').style.display = 'none';
      document.getElementById('authModal').classList.add('open');
      setTimeout(() => document.getElementById('authUsername').focus(), 120);
    }

    function closeAuthModal() {
      if (authRequired && !currentUser) return;
      document.getElementById('authModal').classList.remove('open');
    }

    function switchAuthMode(mode) {
      currentAuthMode = mode;
      const isLogin = mode === 'login';
      document.getElementById('authTabLogin').classList.toggle('active', isLogin);
      document.getElementById('authTabRegister').classList.toggle('active', !isLogin);
      document.getElementById('authModalTitle').textContent = isLogin ? 'Anthology Girişi' : 'Yeni Hesap Oluştur';
      document.getElementById('authSubmitText').textContent = isLogin ? 'Giriş Yap' : 'Hesap Oluştur ve Başla';
      document.getElementById('authErrorMsg').style.display = 'none';
    }

    async function handleAuthSubmit(e) {
      e.preventDefault();
      const u = document.getElementById('authUsername').value.trim();
      const p = document.getElementById('authPassword').value;
      const errEl = document.getElementById('authErrorMsg');
      errEl.style.display = 'none';

      const endpoint = currentAuthMode === 'login' ? '/api/auth/login' : '/api/auth/register';
      try {
        const res = await fetch(endpoint, {
          method: 'POST',
          headers: { 'Content-Type': 'application/json' },
          body: JSON.stringify({ username: u, password: p })
        });
        const data = await res.json();
        if (!res.ok || data.error) {
          errEl.textContent = data.error || 'İşlem başarısız oldu.';
          errEl.style.display = 'block';
          return;
        }

        if (data.token) {
          authToken = data.token;
          localStorage.setItem('anthology_token', authToken);
        }
        currentUser = data.user;
        authRequired = false;
        updateUserHeader();
        closeAuthModal();
        showToast('Hoş geldiniz, ' + currentUser.username + '!', '👤');
        await loadLibrary();
      } catch (err) {
        errEl.textContent = 'Sunucuya bağlanırken hata oluştu.';
        errEl.style.display = 'block';
      }
    }

    async function handleLogout() {
      try {
        await fetch('/api/auth/logout', { method: 'POST', headers: authHeaders() });
      } catch (e) {}
      authToken = '';
      localStorage.removeItem('anthology_token');
      currentUser = null;
      updateUserHeader();
      showToast('Oturum kapatıldı.', '✓');
      if (isMultiUser) {
        libraryItems = [];
        updateLibraryBadges(0);
        renderLibrary();
        openAuthModal('login', true);
      }
    }

    function openConnectModal() {
      const baseOrigin = getReachableBaseOrigin();
      const vlcCode = document.getElementById('connectVlcCode');
      const davCode = document.getElementById('connectDavCode');
      const vlcTokenBtn = document.getElementById('btnCopyVlcToken');

      if (isMultiUser && currentUser) {
        if (vlcCode) {
          const hostPort = baseOrigin.replace(/^https?:\/\//i, '');
          vlcCode.innerHTML = 'http://<strong style="color:var(--md-sys-color-primary);">' + escapeHtml(currentUser.username) + ':&lt;sifreniz&gt;</strong>@' + hostPort + '/playlist.m3u';
        }
        if (davCode) {
          davCode.innerHTML = baseOrigin + '/dav/<br><span style="font-size:11.5px; color:var(--md-sys-color-on-surface-variant);">' +
            'Kullanıcı: <strong style="color:var(--md-sys-color-primary);">' + escapeHtml(currentUser.username) + '</strong> | Şifre: <strong>(hesap şifreniz)</strong></span>';
        }
        if (vlcTokenBtn) {
          vlcTokenBtn.style.display = authToken ? 'inline-block' : 'none';
        }
      } else {
        if (vlcCode) {
          vlcCode.textContent = baseOrigin + '/playlist.m3u';
        }
        if (davCode) {
          davCode.innerHTML = baseOrigin + '/dav/<br><span style="font-size:11px; color:var(--md-sys-color-on-surface-variant);">(Yerel mod: Kullanıcı adı ve şifre gerekmez)</span>';
        }
        if (vlcTokenBtn) {
          vlcTokenBtn.style.display = 'none';
        }
      }
      document.getElementById('connectModal').classList.add('open');
    }

    function closeConnectModal() {
      document.getElementById('connectModal').classList.remove('open');
    }

    function copyVlcTokenPlaylist() {
      if (!authToken) return;
      const url = getReachableBaseOrigin() + '/playlist.m3u?token=' + encodeURIComponent(authToken);
      if (navigator.clipboard && navigator.clipboard.writeText) {
        navigator.clipboard.writeText(url).then(() => showToast('Tokenli şifresiz VLC linki kopyalandı!', '📋'));
      } else {
        prompt('VLC M3U Adresi:', url);
      }
    }

    function copyEndpointPath(pathSuffix, msg) {
      let txt = getReachableBaseOrigin() + pathSuffix;
      if (isMultiUser && currentUser && pathSuffix === '/playlist.m3u') {
        txt = 'http://' + encodeURIComponent(currentUser.username) + ':sifreniz@' + getReachableBaseOrigin().replace(/^https?:\/\//i, '') + pathSuffix;
      }
      if (navigator.clipboard && navigator.clipboard.writeText) {
        navigator.clipboard.writeText(txt).then(() => showToast(msg, '📋'));
      } else {
        prompt('Aşağıdaki adresi kopyalayın:', txt);
      }
    }

    function openInVLC(streamUrl, title) {
      const ua = navigator.userAgent || '';
      const isAndroid = /Android/i.test(ua);
      const isIOS = /iPad|iPhone|iPod/i.test(ua) || (navigator.platform === 'MacIntel' && navigator.maxTouchPoints > 1);

      showToast((title || 'Medya') + ' VLC uygulamasında açılıyor...', '▶');

      if (isAndroid) {
        const noScheme = streamUrl.replace(/^https?:\/\//i, '');
        const scheme = streamUrl.startsWith('https://') ? 'https' : 'http';
        const fallbackVlc = 'vlc://' + streamUrl;
        const intentUrl = 'intent://' + noScheme +
          '#Intent;scheme=' + scheme +
          ';type=video/*;package=org.videolan.vlc' +
          (title ? ';S.title=' + encodeURIComponent(title) : '') +
          ';S.browser_fallback_url=' + encodeURIComponent(fallbackVlc) +
          ';end';
        window.location.href = intentUrl;
        return;
      }

      if (isIOS) {
        window.location.href = 'vlc-x-callback://x-callback-url/stream?url=' + encodeURIComponent(streamUrl);
        return;
      }

      window.location.href = 'vlc://' + streamUrl;
    }

    function openFullPlaylistInVLC() {
      let playlistUrl = getReachableBaseOrigin() + '/playlist.m3u';
      if (isMultiUser && authToken) {
        playlistUrl += '?token=' + encodeURIComponent(authToken);
      }
      openInVLC(playlistUrl, 'Anthology Kütüphanesi');
    }

    function playMovieInVLC(itemId) {
      const it = libraryItems.find(x => x.id === itemId);
      if (!it) return;
      const prov = it.selected_provider || 'auto';
      let playUrl = getReachableBaseOrigin() + '/playlist.m3u?id=' + encodeURIComponent(it.id) +
        '&provider=' + encodeURIComponent(prov);
      if (isMultiUser && authToken) {
        playUrl += '&token=' + encodeURIComponent(authToken);
      }
      openInVLC(playUrl, it.title + (it.year ? ' (' + it.year + ')' : ''));
    }

    function playModalCurrentInVLC(provOverride, audioOverride) {
      if (!activeModalItem) return;
      const it = activeModalItem;
      const s = parseInt(document.getElementById('modalSeason').value || '1', 10);
      const e = parseInt(document.getElementById('modalEpisode').value || '1', 10);
      const prov = provOverride || it.selected_provider || 'auto';
      const audio = audioOverride || 'auto';
      let playUrl = getReachableBaseOrigin() + '/playlist.m3u?id=' + encodeURIComponent(it.id) +
        '&season=' + s + '&episode=' + e +
        '&provider=' + encodeURIComponent(prov) +
        '&audio=' + encodeURIComponent(audio);
      if (isMultiUser && authToken) {
        playUrl += '&token=' + encodeURIComponent(authToken);
      }
      const label = it.type === 'movie'
        ? it.title
        : (it.title + ' - S' + String(s).padStart(2, '0') + 'E' + String(e).padStart(2, '0'));
      openInVLC(playUrl, label);
    }

    function copyItemM3UPlaylist() {
      if (!activeModalItem) return;
      let url = getReachableBaseOrigin() + '/playlist.m3u?id=' + encodeURIComponent(activeModalItem.id);
      if (isMultiUser && authToken) {
        url += '&token=' + encodeURIComponent(authToken);
      }
      if (navigator.clipboard && navigator.clipboard.writeText) {
        navigator.clipboard.writeText(url).then(() => {
          showToast(activeModalItem.title + ' M3U listesi kopyalandı! VLC Akışlar kısmına yapıştırabilirsiniz.', '📋');
        });
      } else {
        prompt('M3U Oynatma Listesi Adresi:', url);
      }
    }

    function showToast(msg, icon = '✓') {
      const snackbar = document.getElementById('m3Snackbar');
      document.getElementById('snackbarMsg').textContent = msg;
      document.getElementById('snackbarIcon').textContent = icon;
      snackbar.classList.add('show');
      setTimeout(() => snackbar.classList.remove('show'), 2800);
    }

    function switchTab(tab) {
      currentTab = tab;
      
      // Update Rail navigation items
      document.getElementById('railTabSearch').classList.toggle('active', tab === 'search');
      document.getElementById('railTabLibrary').classList.toggle('active', tab === 'library');
      document.getElementById('railTabProviders').classList.toggle('active', tab === 'providers');

      // Update Bottom navigation items
      document.getElementById('bottomTabSearch').classList.toggle('active', tab === 'search');
      document.getElementById('bottomTabLibrary').classList.toggle('active', tab === 'library');
      document.getElementById('bottomTabProviders').classList.toggle('active', tab === 'providers');

      // Update Tab panes
      document.getElementById('tab-search').classList.toggle('active', tab === 'search');
      document.getElementById('tab-library').classList.toggle('active', tab === 'library');
      document.getElementById('tab-providers').classList.toggle('active', tab === 'providers');

      if (tab === 'library') loadLibrary();
      if (tab === 'providers') loadProviders();
    }

    function updateLibraryBadges(count) {
      const str = String(count || 0);
      const railBadge = document.getElementById('railLibCountBadge');
      const bottomBadge = document.getElementById('bottomLibCountBadge');
      if (railBadge) railBadge.textContent = str;
      if (bottomBadge) bottomBadge.textContent = str;
    }

    async function loadLibrary() {
      if (isMultiUser && !currentUser) {
        updateLibraryBadges(0);
        renderLibrary();
        return;
      }
      try {
        const res = await fetch('/api/library', { headers: authHeaders() });
        if (res.status === 401) {
          if (isMultiUser) openAuthModal('login', true);
          return;
        }
        libraryItems = await res.json() || [];
        updateLibraryBadges(libraryItems.length);
        renderLibrary();
      } catch (e) {
        console.error(e);
      }
    }

    function filterLibrary(type, btn) {
      activeFilter = type;
      btn.parentElement.querySelectorAll('.m3-filter-chip').forEach(p => p.classList.remove('active'));
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
        grid.innerHTML = '<div class="m3-empty-state" style="grid-column: 1/-1;">' +
          '<svg class="m3-empty-icon" viewBox="0 0 24 24"><path d="M4 6H2v14c0 1.1.9 2 2 2h14v-2H4V6zm16-4H8c-1.1 0-2 .9-2 2v12c0 1.1.9 2 2 2h12c1.1 0 2-.9 2-2V4c0-1.1-.9-2-2-2zm-8 12.5v-9l6 4.5-6 4.5z"/></svg>' +
          '<h3 style="font-size:17px; color:var(--md-sys-color-on-surface);">Kütüphanenizde henüz içerik yok.</h3>' +
          '<p style="font-size:13px;">"Keşfet" sekmesinden arama yaparak dilediğiniz film, dizi ve animeleri ekleyebilirsiniz.</p>' +
        '</div>';
        return;
      }

      grid.innerHTML = filtered.map(it => {
        const badgeClass = it.type === 'anime' ? 'm3-badge-anime' : (it.type === 'series' ? 'm3-badge-series' : 'm3-badge-movie');
        const badgeLabel = it.type === 'anime' ? '⛩️ Anime' : (it.type === 'series' ? '📺 Dizi' : '🎬 Film');
        const poster = it.poster_url || 'https://via.placeholder.com/300x450/131720/8c919d?text=Poster';
        const workingSet = new Set(it.working_providers || []);

        let relevantProvs = [...providerList];
        if (workingSet.size > 0) {
          relevantProvs.sort((a, b) => (workingSet.has(b.id) ? 1 : 0) - (workingSet.has(a.id) ? 1 : 0));
        }

        let provOptions = '<option value="auto">⚡ Otomatik (En Yüksek Kalite)</option>';
        relevantProvs.forEach(p => {
          const selected = (it.selected_provider === p.id) ? 'selected' : '';
          const prefix = workingSet.has(p.id) ? '✓ ' : '';
          const suffix = workingSet.has(p.id) ? ' (Çalışıyor)' : '';
          provOptions += '<option value="' + p.id + '" ' + selected + '>' + prefix + (p.title || p.name) + suffix + '</option>';
        });

        const workInfo = workingSet.size > 0 ? (workingSet.size + ' kaynak doğrulandı') : 'Yayına Hazır';
        const vlcBtn = (it.type === 'movie')
          ? '<button class="m3-card-btn vlc m3-ripple-container" onclick="playMovieInVLC(\'' + it.id + '\')"><svg class="m3-icon" style="width:16px;height:16px;" viewBox="0 0 24 24"><path d="M8 5v14l11-7z"/></svg><span>▶ VLC\'de Oynat</span></button>'
          : '<button class="m3-card-btn vlc m3-ripple-container" onclick="openSourceFinder(\'' + it.id + '\')"><svg class="m3-icon" style="width:16px;height:16px;" viewBox="0 0 24 24"><path d="M8 5v14l11-7z"/></svg><span>▶ Bölüm Seç & Oynat</span></button>';

        return '<div class="m3-media-card">' +
          '<div class="m3-poster-box">' +
            '<img class="m3-poster-img" src="' + poster + '" loading="lazy" alt="' + escapeHtml(it.title) + '">' +
            '<div class="m3-poster-scrim"></div>' +
            '<span class="m3-badge-type ' + badgeClass + '">' + badgeLabel + '</span>' +
          '</div>' +
          '<div class="m3-card-body">' +
            '<div>' +
              '<div class="m3-card-title" title="' + escapeHtml(it.title) + '">' + escapeHtml(it.title) + '</div>' +
              '<div class="m3-card-meta"><span>' + (it.year || 'N/A') + '</span> • <span style="color:#A6F5B5; font-weight:600;">' + workInfo + '</span></div>' +
            '</div>' +
            '<div>' +
              '<label style="font-size:10.5px; color:var(--md-sys-color-on-surface-variant); display:block; margin-bottom:4px;">Oynatma Kaynağı:</label>' +
              '<select class="m3-card-select" onchange="changeProvider(\'' + it.id + '\', this.value)">' +
                provOptions +
              '</select>' +
            '</div>' +
            vlcBtn +
            '<button class="m3-card-btn m3-ripple-container" onclick="openSourceFinder(\'' + it.id + '\')">' +
              '<svg class="m3-icon" style="width:16px;height:16px;" viewBox="0 0 24 24"><path d="M15.5 14h-.79l-.28-.27A6.471 6.471 0 0 0 16 9.5 6.5 6.5 0 1 0 9.5 16c1.61 0 3.09-.59 4.23-1.57l.27.28v.79l5 4.99L20.49 19l-4.99-5zm-6 0C7.01 14 5 11.99 5 9.5S7.01 5 9.5 5 14 7.01 14 9.5 11.99 14 9.5 14z"/></svg>' +
              '<span>Kaynakları Bul</span>' +
            '</button>' +
            '<button class="m3-card-btn danger m3-ripple-container" onclick="removeFromLibrary(\'' + it.id + '\', \'' + it.title.replace(/'/g, "\\'") + '\')">' +
              '<svg class="m3-icon" style="width:16px;height:16px;" viewBox="0 0 24 24"><path d="M6 19c0 1.1.9 2 2 2h8c1.1 0 2-.9 2-2V7H6v12zM19 4h-3.5l-1-1h-5l-1 1H5v2h14V4z"/></svg>' +
              '<span>Kütüphaneden Çıkar</span>' +
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
      const isMovie = (it.type === 'movie');
      document.getElementById('lblSeason').style.display = isMovie ? 'none' : 'inline';
      document.getElementById('modalSeason').style.display = isMovie ? 'none' : 'inline-block';
      document.getElementById('lblEpisode').style.display = isMovie ? 'none' : 'inline';
      document.getElementById('modalEpisode').style.display = isMovie ? 'none' : 'inline-block';
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
      listEl.innerHTML = '<div class="m3-empty-state"><div style="font-size:24px;">⏳</div><div>Sağlayıcılar eşzamanlı taranıyor ve doğrulanıyor...</div><div style="font-size:12px; color:var(--md-sys-color-on-surface-variant);">Beklemek istemiyorsanız üstteki "▶ Hemen VLC\'de Aç" butonuyla anında izleyebilirsiniz.</div></div>';

      try {
        const u = '/api/sources?type=' + encodeURIComponent(it.type) +
          '&tmdb_id=' + encodeURIComponent(it.tmdb_id) +
          '&title=' + encodeURIComponent(it.title) +
          '&original_title=' + encodeURIComponent(it.original_title || '') +
          '&year=' + encodeURIComponent(it.year || '') +
          '&season=' + s + '&episode=' + e;

        const res = await fetch(u, { headers: authHeaders() });
        const data = await res.json();
        const sources = (data && data.sources) ? data.sources : [];

        if (sources.length === 0) {
          listEl.innerHTML = '<div class="m3-empty-state"><div style="font-size:24px;">⚠️</div><div style="color:var(--md-sys-color-error);">Bu bölüm için şu an aktif bir yayın kaynağı bulunamadı.</div></div>';
          return;
        }

        await loadLibrary();

        const bestQual = sources[0].quality || '1080p';
        let html = '<div class="m3-source-row" style="border-color: rgba(48, 209, 88, 0.4); background: rgba(48, 209, 88, 0.08);">' +
          '<div class="m3-source-row-info">' +
            '<div class="m3-source-row-name">⚡ Otomatik En İyi (Akıllı Öncelik) <span class="m3-status-chip" style="font-size:10px; padding:2px 8px;">' + bestQual + '</span></div>' +
            '<div class="m3-source-row-sub">Orijinal ses ve Türkçe altyazı öncelikli olarak tüm kaynaklar arasından otomatik seçer</div>' +
          '</div>' +
          '<div class="m3-source-row-actions" style="display:flex; flex-wrap:wrap; gap:8px;">' +
            '<button class="m3-pill-btn vlc m3-ripple-container" style="height:32px; padding:0 12px; font-size:11.5px;" onclick="playModalCurrentInVLC(\'auto\', \'sub\')">▶ Altyazılı Aç</button>' +
            '<button class="m3-pill-btn vlc m3-ripple-container" style="height:32px; padding:0 12px; font-size:11.5px;" onclick="playModalCurrentInVLC(\'auto\', \'dub\')">▶ Dublaj Aç</button>' +
            '<button class="m3-pill-btn primary m3-ripple-container" style="height:32px; padding:0 12px; font-size:11.5px;" onclick="selectWorkingProvider(\'' + it.id + '\', \'auto\')">Varsayılan Yap</button>' +
          '</div>' +
        '</div>';

        const grouped = [];
        const groupMap = {};
        sources.forEach(src => {
          if (!groupMap[src.provider_id]) {
            groupMap[src.provider_id] = {
              provider_id: src.provider_id,
              provider_name: src.provider_name,
              variants: []
            };
            grouped.push(groupMap[src.provider_id]);
          }
          groupMap[src.provider_id].variants.push(src);
        });

        grouped.forEach((grp, idx) => {
          const isCurrent = (it.selected_provider === grp.provider_id);
          html += '<div class="m3-source-row" style="flex-direction:column; align-items:stretch; gap:10px;">' +
            '<div style="display:flex; align-items:center; justify-content:space-between; flex-wrap:wrap; gap:8px;">' +
              '<div class="m3-source-row-info">' +
                '<div class="m3-source-row-name">' + (idx + 1) + '. ' + escapeHtml(grp.provider_name) + '</div>' +
              '</div>' +
              '<div style="display:flex; align-items:center; gap:8px;">' +
                '<button class="m3-pill-btn ' + (isCurrent ? '' : 'primary') + ' m3-ripple-container" style="height:30px; padding:0 12px; font-size:11px;" onclick="selectWorkingProvider(\'' + it.id + '\', \'' + grp.provider_id + '\')">' +
                  (isCurrent ? '✓ Seçili Sağlayıcı' : 'Sağlayıcıyı Seç') +
                '</button>' +
              '</div>' +
            '</div>' +
            '<div style="display:flex; flex-direction:column; gap:6px; padding-left:12px; border-left:2px solid var(--md-sys-color-outline-variant);">' +
              grp.variants.map(v => {
                const aLbl = v.audio_label || (v.audio_type === 'dub' ? '🎙️ Türkçe Dublaj' : '💬 Türkçe Altyazı');
                return '<div style="display:flex; align-items:center; justify-content:space-between; flex-wrap:wrap; gap:6px; background:rgba(255,255,255,0.03); padding:6px 10px; border-radius:8px;">' +
                  '<div style="display:flex; align-items:center; gap:8px;">' +
                    '<span style="font-size:12.5px; font-weight:600; color:var(--md-sys-color-on-surface);">' + escapeHtml(aLbl) + '</span>' +
                    '<span class="m3-status-chip" style="font-size:10px; padding:2px 8px;">' + escapeHtml(v.quality) + ' • ' + escapeHtml(v.format) + '</span>' +
                  '</div>' +
                  '<div style="display:flex; align-items:center; gap:6px;">' +
                    '<button class="m3-pill-btn vlc m3-ripple-container" style="height:28px; padding:0 10px; font-size:11px;" onclick="playModalCurrentInVLC(\'' + grp.provider_id + '\', \'' + v.audio_type + '\')">▶ VLC\'de Aç</button>' +
                    '<button class="m3-pill-btn m3-ripple-container" style="height:28px; padding:0 10px; font-size:11px;" onclick="copyWorkingStreamURL(\'' + grp.provider_id + '\', ' + s + ', ' + e + ', \'' + v.audio_type + '\')">Linki Kopyala</button>' +
                  '</div>' +
                '</div>';
              }).join('') +
            '</div>' +
          '</div>';
        });

        listEl.innerHTML = html;
      } catch (err) {
        listEl.innerHTML = '<div class="m3-empty-state"><div style="color:var(--md-sys-color-error);">Tarama sırasında hata oluştu.</div></div>';
      }
    }

    async function selectWorkingProvider(itemId, provId) {
      await changeProvider(itemId, provId);
      await loadLibrary();
      closeSourceModal();
    }

    function copyWorkingStreamURL(provId, s, e, audioType) {
      if (!activeModalItem) return;
      const it = activeModalItem;
      const audio = audioType || 'auto';
      const url = getReachableBaseOrigin() + '/stream/resolve?type=' + encodeURIComponent(it.type) +
        '&tmdb_id=' + encodeURIComponent(it.tmdb_id) +
        '&title=' + encodeURIComponent(it.title) +
        '&year=' + encodeURIComponent(it.year || '') +
        '&season=' + s + '&episode=' + e +
        '&provider=' + encodeURIComponent(provId) +
        '&audio=' + encodeURIComponent(audio);
      if (navigator.clipboard && navigator.clipboard.writeText) {
        navigator.clipboard.writeText(url).then(() => {
          showToast('Doğrulanmış yayın linki kopyalandı! (VLC, IINA, Infuse ile açabilirsiniz)', '▶');
        });
      } else {
        prompt('Yayın Linki:', url);
      }
    }

    const searchInputEl = document.getElementById('searchInput');
    const searchClearBtn = document.getElementById('searchClearBtn');

    searchInputEl.addEventListener('input', (e) => {
      clearTimeout(searchTimeout);
      const q = e.target.value.trim();
      searchClearBtn.style.display = q ? 'flex' : 'none';
      if (!q) {
        lastSearchResults = [];
        document.getElementById('searchResults').innerHTML = '';
        document.getElementById('searchResultCount').textContent = '';
        return;
      }
      searchTimeout = setTimeout(() => doSearch(q), 280);
    });

    function clearSearch() {
      searchInputEl.value = '';
      searchClearBtn.style.display = 'none';
      lastSearchResults = [];
      document.getElementById('searchResults').innerHTML = '';
      document.getElementById('searchResultCount').textContent = '';
      searchInputEl.focus();
    }

    function filterSearchResults(type, btn) {
      activeSearchFilter = type;
      btn.parentElement.querySelectorAll('.m3-filter-chip').forEach(p => p.classList.remove('active'));
      btn.classList.add('active');
      renderSearchResults();
    }

    async function doSearch(query) {
      const resultsContainer = document.getElementById('searchResults');
      resultsContainer.innerHTML = '<div style="grid-column: 1/-1; text-align:center; padding: 48px; color: var(--md-sys-color-on-surface-variant);">' +
        '<div style="font-size:24px; margin-bottom:8px;">⏳</div><div>TMDB taranıyor...</div></div>';
      try {
        const res = await fetch('/api/search?q=' + encodeURIComponent(query));
        lastSearchResults = await res.json() || [];
        renderSearchResults();
      } catch (e) {
        resultsContainer.innerHTML = '<div style="color:var(--md-sys-color-error); padding:24px; text-align:center;">Arama sırasında hata oluştu.</div>';
      }
    }

    function renderSearchResults() {
      const resultsContainer = document.getElementById('searchResults');
      let filtered = lastSearchResults;
      if (activeSearchFilter !== 'all') {
        filtered = lastSearchResults.filter(it => it.type === activeSearchFilter);
      }

      document.getElementById('searchResultCount').textContent = filtered.length + ' sonuç bulundu';

      if (filtered.length === 0) {
        resultsContainer.innerHTML = '<div class="m3-empty-state" style="grid-column: 1/-1;">' +
          '<svg class="m3-empty-icon" viewBox="0 0 24 24"><path d="M15.5 14h-.79l-.28-.27A6.471 6.471 0 0 0 16 9.5 6.5 6.5 0 1 0 9.5 16c1.61 0 3.09-.59 4.23-1.57l.27.28v.79l5 4.99L20.49 19l-4.99-5zm-6 0C7.01 14 5 11.99 5 9.5S7.01 5 9.5 5 14 7.01 14 9.5 11.99 14 9.5 14z"/></svg>' +
          '<h3 style="font-size:16px; color:var(--md-sys-color-on-surface);">Sonuç bulunamadı</h3>' +
          '<p style="font-size:12.5px;">Arama terimini değiştirerek veya filtreyi "Tümü" yaparak tekrar deneyebilirsiniz.</p>' +
        '</div>';
        return;
      }

      resultsContainer.innerHTML = filtered.map(it => {
        const isAdded = libraryItems.some(lib => lib.tmdb_id === it.id);
        const badgeClass = it.type === 'anime' ? 'm3-badge-anime' : (it.type === 'series' ? 'm3-badge-series' : 'm3-badge-movie');
        const badgeLabel = it.type === 'anime' ? '⛩️ Anime' : (it.type === 'series' ? '📺 Dizi' : '🎬 Film');
        const poster = it.poster_url || 'https://via.placeholder.com/300x450/131720/8c919d?text=Poster';
        const rating = it.rating ? '★ ' + it.rating.toFixed(1) : '';

        const actionBtn = isAdded
          ? '<button class="m3-card-btn added">✓ Kütüphanede</button>'
          : '<button class="m3-card-btn add-btn m3-ripple-container" onclick="addToLibrary(' + JSON.stringify(it).replace(/"/g, '&quot;') + ')">' +
              '<svg class="m3-icon" style="width:16px;height:16px;" viewBox="0 0 24 24"><path d="M19 13h-6v6h-2v-6H5v-2h6V5h2v6h6v2z"/></svg>' +
              '<span>Kütüphaneme Ekle</span>' +
            '</button>';

        return '<div class="m3-media-card">' +
          '<div class="m3-poster-box">' +
            '<img class="m3-poster-img" src="' + poster + '" loading="lazy" alt="' + escapeHtml(it.title) + '">' +
            '<div class="m3-poster-scrim"></div>' +
            '<span class="m3-badge-type ' + badgeClass + '">' + badgeLabel + '</span>' +
            (rating ? '<span class="m3-rating-badge">' + rating + '</span>' : '') +
          '</div>' +
          '<div class="m3-card-body">' +
            '<div>' +
              '<div class="m3-card-title" title="' + escapeHtml(it.title) + '">' + escapeHtml(it.title) + '</div>' +
              '<div class="m3-card-meta"><span>' + (it.year || 'N/A') + '</span></div>' +
            '</div>' +
            actionBtn +
          '</div>' +
        '</div>';
      }).join('');
    }

    async function addToLibrary(it) {
      if (isMultiUser && !currentUser) {
        openAuthModal('login', true);
        return;
      }
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
          headers: authHeaders(),
          body: JSON.stringify(payload)
        });
        if (res.status === 401) {
          openAuthModal('login', true);
          return;
        }
        if (res.ok) {
          showToast(it.title + ' kütüphanenize eklendi!', '✓');
          await loadLibrary();
          renderSearchResults();
        }
      } catch (e) {
        showToast('Ekleme başarısız!', '✕');
      }
    }

    async function removeFromLibrary(id, title) {
      if (isMultiUser && !currentUser) {
        openAuthModal('login', true);
        return;
      }
      try {
        const res = await fetch('/api/library/remove', {
          method: 'POST',
          headers: authHeaders(),
          body: JSON.stringify({ id: id })
        });
        if (res.status === 401) {
          openAuthModal('login', true);
          return;
        }
        if (res.ok) {
          showToast(title + ' kütüphaneden çıkarıldı.', '✓');
          await loadLibrary();
        }
      } catch (e) {
        showToast('Silme başarısız!', '✕');
      }
    }

    async function changeProvider(id, prov) {
      if (isMultiUser && !currentUser) {
        openAuthModal('login', true);
        return;
      }
      try {
        const res = await fetch('/api/library/provider', {
          method: 'POST',
          headers: authHeaders(),
          body: JSON.stringify({ id: id, provider: prov })
        });
        if (res.status === 401) {
          openAuthModal('login', true);
          return;
        }
        showToast('Varsayılan oynatma kaynağı güncellendi: ' + prov, '⚡');
      } catch (e) {
        console.error(e);
      }
    }

    async function loadProviders() {
      const grid = document.getElementById('providersGrid');
      grid.innerHTML = '<div style="grid-column: 1/-1; text-align:center; padding: 48px; color:var(--md-sys-color-on-surface-variant);">' +
        '<div style="font-size:24px; margin-bottom:8px;">⏳</div><div>Sağlayıcılar taranıyor...</div></div>';
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
            logoHtml = '<div class="m3-provider-logo-box">' +
              '<img class="m3-provider-logo-img" src="' + faviconUrl + '" alt="' + escapeHtml(title) + '" ' +
                'onload="this.style.display=\'block\'; if(this.nextElementSibling) this.nextElementSibling.style.display=\'none\';" ' +
                'onerror="this.style.display=\'none\'; if(this.nextElementSibling) this.nextElementSibling.style.display=\'flex\';">' +
              '<div style="display:none; font-size:18px;">' + icon + '</div>' +
            '</div>';
          } else {
            logoHtml = '<div class="m3-provider-logo-box"><div style="font-size:18px;">' + icon + '</div></div>';
          }

          const domainText = domain ? domain : p.id;

          return '<div class="m3-provider-card">' +
            logoHtml +
            '<div class="m3-provider-info">' +
              '<div class="m3-provider-name" title="' + escapeHtml(title) + '">' + escapeHtml(title) + '</div>' +
              '<div class="m3-provider-meta">' +
                '<span class="m3-provider-cat-tag">' + p.category + '</span>' +
                '<span>•</span>' +
                '<span title="' + domainText + '">' + domainText + '</span>' +
              '</div>' +
            '</div>' +
            '<span class="m3-status-chip" style="font-size:10px; padding:2px 8px;">AKTİF</span>' +
          '</div>';
        }).join('');
      } catch (e) {
        grid.innerHTML = '<div style="color:var(--md-sys-color-error); padding:24px; text-align:center;">Sağlayıcılar yüklenemedi.</div>';
      }
    }

    // Initial load sequence
    (async () => {
      await loadServerStatus();
      await checkAuthMe();
      await loadLibrary();
      try {
        const res = await fetch('/api/providers');
        providerList = await res.json() || [];
      } catch (e) {}
    })();
  </script>
</body>
</html>`

const glassmorphismHTML = material3HTML
