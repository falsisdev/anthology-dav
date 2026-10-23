package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"syscall"
	"time"

	"golang.org/x/net/webdav"

	"anthology-dav/config"
	"anthology-dav/providers"
	"anthology-dav/stream"
	"anthology-dav/vfs"
	"anthology-dav/web"
)

func main() {
	hostFlag := flag.String("host", config.DefaultHost, "Bind host address")
	defaultPort := config.DefaultPort
	if envPort := os.Getenv("PORT"); envPort != "" {
		if p, err := strconv.Atoi(envPort); err == nil && p > 0 {
			defaultPort = p
		}
	}
	portFlag := flag.Int("port", defaultPort, "Bind port number")
	watchlistFlag := flag.String("watchlist", "", "Path to watchlist.json file")
	libraryFlag := flag.String("library", "", "Path to library.json file")
	publicURLFlag := flag.String("public-url", os.Getenv("ANTHOLOGY_PUBLIC_URL"), "Client-facing base URL written into .strm files (e.g. http://192.168.1.10:8085)")
	flag.Parse()

	cfg := config.Get()
	cfg.SetBind(*hostFlag, *portFlag)
	cfg.SetPublicURL(*publicURLFlag)
	if *watchlistFlag != "" {
		cfg.WatchlistPath = *watchlistFlag
	}
	if *libraryFlag != "" {
		cfg.LibraryPath = *libraryFlag
	}

	// Trigger watchlist and library preload immediately; load remote config asynchronously
	_ = config.Watchlist().Load()
	_ = config.Library().Load()
	go func() { _ = config.LoadRemoteConfig() }()

	cache := vfs.NewMemoryCache()
	virtualFS := vfs.NewAnthologyVFS(cfg, cache)
	proxyHandler := stream.NewProxyHandler(cfg, cache)
	multiResolver := stream.NewMultiResolver(proxyHandler)
	virtualFS.SetSourceDiscoverer(func(ctx context.Context, mediaType string, tmdbID int, title, origTitle, year string, season, episode int) []vfs.VFSDiscoveredSource {
		found := multiResolver.DiscoverSources(ctx, mediaType, tmdbID, title, origTitle, year, season, episode)
		out := make([]vfs.VFSDiscoveredSource, 0, len(found))
		for _, f := range found {
			out = append(out, vfs.VFSDiscoveredSource{
				ProviderID:   f.ProviderID,
				ProviderName: f.ProviderName,
				Quality:      f.Quality,
			})
		}
		return out
	})
	webHandler := web.NewHandler(cfg, cache, multiResolver)

	// Background prewarm of TMDB seasons & alternative titles for existing library items
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		defer cancel()
		for _, it := range config.Library().All() {
			itemCopy := it
			if len(itemCopy.Seasons) == 0 || len(itemCopy.AltTitles) == 0 {
				providers.PrewarmLibraryItemMetadata(ctx, &itemCopy)
			}
		}
	}()

	davHandler := &webdav.Handler{
		Prefix:     "/dav",
		FileSystem: virtualFS,
		LockSystem: webdav.NewMemLS(),
		Logger: func(r *http.Request, err error) {
			if err != nil && !os.IsNotExist(err) {
				log.Printf("[WEBDAV] %s %s -> %v", r.Method, r.URL.Path, err)
			}
		},
	}

	mux := http.NewServeMux()

	// WebDAV endpoint (/dav and /dav/...)
	mux.HandleFunc("/dav", func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "/dav/", http.StatusMovedPermanently)
	})
	mux.HandleFunc("/dav/", func(w http.ResponseWriter, r *http.Request) {
		lowPath := strings.ToLower(strings.TrimSuffix(r.URL.Path, "/"))
		isMediaFile := strings.HasSuffix(lowPath, ".strm") || strings.HasSuffix(lowPath, ".vtt") || strings.HasSuffix(lowPath, ".srt") || strings.HasSuffix(lowPath, ".m3u")
		if (r.Method == http.MethodGet || r.Method == http.MethodHead) && !isMediaFile {
			// Standard WebDAV clients use PROPFIND on directories; x/net/webdav returns 405 on GET for directories.
			// Serving the M3U playlist on GET /dav/ allows VLC Network Stream users who enter /dav/ to load the library seamlessly.
			q := r.URL.Query()
			if strings.HasPrefix(lowPath, "/dav/filmler") {
				q.Set("type", "movie")
			} else if strings.HasPrefix(lowPath, "/dav/diziler") {
				q.Set("type", "series")
			} else if strings.HasPrefix(lowPath, "/dav/animeler") {
				q.Set("type", "anime")
			}
			r.URL.RawQuery = q.Encode()
			webHandler.HandlePlaylistM3U(w, r)
			return
		}
		ctx := context.WithValue(r.Context(), vfs.RequestHostKey, r.Host)
		davHandler.ServeHTTP(w, r.WithContext(ctx))
	})

	// Stream and resolver endpoints
	mux.HandleFunc("/stream/resolve", multiResolver.HandleResolve)
	mux.HandleFunc("/stream/", proxyHandler.HandleStream)
	mux.HandleFunc("/sub/", proxyHandler.HandleSub)
	mux.HandleFunc("/proxy/media", proxyHandler.HandleProxyMedia)

	// VLC / M3U Playlist endpoints (/playlist.m3u and /playlist.m3u8)
	mux.HandleFunc("/playlist.m3u", webHandler.HandlePlaylistM3U)
	mux.HandleFunc("/playlist.m3u8", webHandler.HandlePlaylistM3U)

	// API endpoints for Web UI
	mux.HandleFunc("/api/", webHandler.HandleAPI)

	// Health and provider status endpoint
	mux.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		all := providers.All()
		libCount := len(config.Library().All())
		lan := config.DetectLANAddresses(cfg.Port)
		var provList []map[string]string
		for _, p := range all {
			provList = append(provList, map[string]string{
				"id":   p.ID(),
				"name": p.Name(),
				"dav":  fmt.Sprintf("%s/dav/%s/", lan.PrimaryURL, vfs.SanitizeFileName(p.Name())),
			})
		}
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"status":         "ok",
			"provider_count": len(all),
			"library_count":  libCount,
			"webdav_url":     lan.PrimaryURL + "/dav/",
			"playlist_url":   lan.PrimaryURL + "/playlist.m3u",
			"web_ui_url":     lan.PrimaryURL + "/",
			"lan":            lan,
			"providers":      provList,
		})
	})

	// Root, Static Assets and Web UI handler
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/" || r.URL.Path == "/logo.png" || r.URL.Path == "/favicon.ico" || strings.HasPrefix(r.URL.Path, "/assets/") {
			webHandler.ServeHTTP(w, r)
			return
		}
		if r.URL.Path == "/playlist.m3u" || r.URL.Path == "/playlist.m3u8" {
			webHandler.HandlePlaylistM3U(w, r)
			return
		}
		if strings.HasPrefix(r.URL.Path, "/dav") {
			ctx := context.WithValue(r.Context(), vfs.RequestHostKey, r.Host)
			davHandler.ServeHTTP(w, r.WithContext(ctx))
			return
		}
		if strings.HasPrefix(r.URL.Path, "/api") {
			webHandler.HandleAPI(w, r)
			return
		}
		http.NotFound(w, r)
	})

	loggingHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		sw := &statusWriter{ResponseWriter: w, code: http.StatusOK}
		mux.ServeHTTP(sw, r)
		dur := time.Since(start)
		if strings.HasPrefix(r.URL.Path, "/stream") || strings.HasPrefix(r.URL.Path, "/proxy") || strings.HasPrefix(r.URL.Path, "/playlist") || (strings.HasPrefix(r.URL.Path, "/dav") && r.Method != "PROPFIND") {
			log.Printf("[REQ] %s %s -> %d (%v) Range=%q UA=%s", r.Method, r.URL.RequestURI(), sw.code, dur, r.Header.Get("Range"), r.UserAgent())
		}
	})

	srv := &http.Server{
		Addr:              cfg.Addr(),
		Handler:           loggingHandler,
		ReadHeaderTimeout: 15 * time.Second,
	}

	go func() {
		lan := config.DetectLANAddresses(cfg.Port)
		log.Printf("================================================================")
		log.Printf("Anthology WebDAV started on %s", cfg.BaseURL)
		log.Printf("Web UI (Local):       %s/", cfg.BaseURL)
		log.Printf("Web UI (LAN/Tablet):  %s/", lan.PrimaryURL)
		log.Printf("WebDAV Root (LAN):    %s/dav/", lan.PrimaryURL)
		log.Printf("VLC M3U Playlist:     %s/playlist.m3u", lan.PrimaryURL)
		log.Printf("Registered Providers: %d", providers.Count())
		log.Printf("Library Items Count:  %d", len(config.Library().All()))
		log.Printf("================================================================")
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("[FATAL] Server error: %v", err)
		}
	}()

	stop := make(chan os.Signal, 1)
	signal.Notify(stop, syscall.SIGINT, syscall.SIGTERM)
	<-stop

	log.Printf("[INFO] Shutting down anthology-dav...")
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := srv.Shutdown(ctx); err != nil {
		log.Printf("[WARN] Graceful shutdown error: %v", err)
	}
	log.Printf("[INFO] Server stopped cleanly.")
}

type statusWriter struct {
	http.ResponseWriter
	code int
}

func (w *statusWriter) WriteHeader(code int) {
	w.code = code
	w.ResponseWriter.WriteHeader(code)
}
