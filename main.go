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
	portFlag := flag.Int("port", config.DefaultPort, "Bind port number")
	watchlistFlag := flag.String("watchlist", "", "Path to watchlist.json file")
	libraryFlag := flag.String("library", "", "Path to library.json file")
	flag.Parse()

	cfg := config.Get()
	cfg.SetBind(*hostFlag, *portFlag)
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
		davHandler.ServeHTTP(w, r)
	})

	// Stream and resolver endpoints
	mux.HandleFunc("/stream/resolve", multiResolver.HandleResolve)
	mux.HandleFunc("/stream/", proxyHandler.HandleStream)
	mux.HandleFunc("/sub/", proxyHandler.HandleSub)
	mux.HandleFunc("/proxy/media", proxyHandler.HandleProxyMedia)

	// API endpoints for Web UI
	mux.HandleFunc("/api/", webHandler.HandleAPI)

	// Health and provider status endpoint
	mux.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		all := providers.All()
		libCount := len(config.Library().All())
		var provList []map[string]string
		for _, p := range all {
			provList = append(provList, map[string]string{
				"id":   p.ID(),
				"name": p.Name(),
				"dav":  fmt.Sprintf("%s/dav/%s/", cfg.BaseURL, vfs.SanitizeFileName(p.Name())),
			})
		}
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"status":         "ok",
			"provider_count": len(all),
			"library_count":  libCount,
			"webdav_url":     cfg.BaseURL + "/dav/",
			"web_ui_url":     cfg.BaseURL + "/",
			"providers":      provList,
		})
	})

	// Root, Static Assets and Web UI handler
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/" || r.URL.Path == "/logo.png" || r.URL.Path == "/favicon.ico" || strings.HasPrefix(r.URL.Path, "/assets/") {
			webHandler.ServeHTTP(w, r)
			return
		}
		if strings.HasPrefix(r.URL.Path, "/dav") {
			davHandler.ServeHTTP(w, r)
			return
		}
		if strings.HasPrefix(r.URL.Path, "/api") {
			webHandler.HandleAPI(w, r)
			return
		}
		http.NotFound(w, r)
	})

	srv := &http.Server{
		Addr:              cfg.Addr(),
		Handler:           mux,
		ReadHeaderTimeout: 15 * time.Second,
	}

	go func() {
		log.Printf("================================================================")
		log.Printf("Anthology WebDAV started on %s", cfg.BaseURL)
		log.Printf("Web UI:               %s/", cfg.BaseURL)
		log.Printf("WebDAV Root (Infuse): %s/dav/", cfg.BaseURL)
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
