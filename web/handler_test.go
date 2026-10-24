package web

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"golang.org/x/net/webdav"

	"anthology-dav/auth"
	"anthology-dav/config"
	"anthology-dav/stream"
	"anthology-dav/vfs"
)

func setupTestServer(t *testing.T, multiUser bool) (*config.Config, *auth.Database, *Handler, http.Handler) {
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "test.db")

	cfg := &config.Config{
		Host:              "127.0.0.1",
		Port:              8085,
		BaseURL:           "http://127.0.0.1:8085",
		CatalogTTL:        60,
		DetailsTTL:        60,
		StreamTTL:         60,
		WatchlistPath:     filepath.Join(tmpDir, "watchlist.json"),
		LibraryPath:       filepath.Join(tmpDir, "library.json"),
		MultiUser:         multiUser,
		DBPath:            dbPath,
		AllowRegistration: true,
	}

	var db *auth.Database
	if multiUser {
		var err error
		db, err = auth.InitDB(dbPath)
		if err != nil {
			t.Fatalf("InitDB error: %v", err)
		}
	}

	cache := vfs.NewMemoryCache()
	virtualFS := vfs.NewAnthologyVFS(cfg, cache)
	proxyHandler := stream.NewProxyHandler(cfg, cache)
	multiResolver := stream.NewMultiResolver(proxyHandler)
	h := NewHandler(cfg, cache, multiResolver)

	davHandler := &webdav.Handler{
		Prefix:     "/dav",
		FileSystem: virtualFS,
		LockSystem: webdav.NewMemLS(),
	}

	var authedDav http.Handler = davHandler
	if multiUser {
		authedDav = auth.WebDAVAuthMiddleware(cfg, davHandler)
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/dav/", func(w http.ResponseWriter, r *http.Request) {
		lowPath := strings.ToLower(strings.TrimSuffix(r.URL.Path, "/"))
		isMediaFile := strings.HasSuffix(lowPath, ".strm") || strings.HasSuffix(lowPath, ".vtt") || strings.HasSuffix(lowPath, ".srt") || strings.HasSuffix(lowPath, ".m3u")
		if (r.Method == http.MethodGet || r.Method == http.MethodHead) && !isMediaFile {
			h.HandlePlaylistM3U(w, r)
			return
		}
		ctx := context.WithValue(r.Context(), vfs.RequestHostKey, r.Host)
		authedDav.ServeHTTP(w, r.WithContext(ctx))
	})
	mux.HandleFunc("/playlist.m3u", h.HandlePlaylistM3U)
	mux.HandleFunc("/api/", h.HandleAPI)

	return cfg, db, h, mux
}

func TestSingleUserModeWebDAV(t *testing.T) {
	_, _, _, mux := setupTestServer(t, false)

	// In single user mode, PROPFIND /dav/ should NOT require any password
	req := httptest.NewRequest("PROPFIND", "/dav/", nil)
	req.Header.Set("Depth", "1")
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusMultiStatus {
		t.Fatalf("Expected 207 Multi-Status, got %d", w.Code)
	}
}

func TestMultiUserModeWebDAVAuthAndIsolation(t *testing.T) {
	_, db, _, mux := setupTestServer(t, true)

	// 1. Unauthenticated request to /dav/ must return 401
	reqUnauth := httptest.NewRequest("PROPFIND", "/dav/", nil)
	reqUnauth.Header.Set("Depth", "1")
	wUnauth := httptest.NewRecorder()
	mux.ServeHTTP(wUnauth, reqUnauth)

	if wUnauth.Code != http.StatusUnauthorized {
		t.Fatalf("Expected 401 Unauthorized for unauthed /dav/, got %d", wUnauth.Code)
	}
	if wUnauth.Header().Get("WWW-Authenticate") == "" {
		t.Fatalf("Expected WWW-Authenticate header")
	}

	// 2. Register two test users
	userAlice, err := db.Register("alice", "pass123")
	if err != nil {
		t.Fatalf("Register alice failed: %v", err)
	}
	userBob, err := db.Register("bob", "pass456")
	if err != nil {
		t.Fatalf("Register bob failed: %v", err)
	}

	// Add a movie to Alice's library
	_ = db.AddUserLibraryItem(userAlice.ID, config.LibraryItem{
		ID:     "movie:101",
		TMDBID: 101,
		Title:  "Alice In Wonderland",
		Year:   "2010",
		Type:   "movie",
	})

	// Add an anime to Bob's library
	_ = db.AddUserLibraryItem(userBob.ID, config.LibraryItem{
		ID:     "anime:202",
		TMDBID: 202,
		Title:  "Bob Anime Adventure",
		Year:   "2023",
		Type:   "anime",
	})

	// 3. Alice accesses /dav/ with HTTP Basic Auth
	reqAlice := httptest.NewRequest("PROPFIND", "/dav/Filmler/", nil)
	reqAlice.Header.Set("Depth", "1")
	reqAlice.SetBasicAuth("alice", "pass123")
	wAlice := httptest.NewRecorder()
	mux.ServeHTTP(wAlice, reqAlice)

	if wAlice.Code != http.StatusMultiStatus {
		t.Fatalf("Expected 207 Multi-Status for alice, got %d", wAlice.Code)
	}
	aliceBody := wAlice.Body.String()
	if !strings.Contains(aliceBody, "Alice In Wonderland") {
		t.Fatalf("Expected Alice's movie in response, got: %s", aliceBody)
	}
	if strings.Contains(aliceBody, "Bob Anime Adventure") {
		t.Fatalf("Bob's media leaked into Alice's library!")
	}

	// 4. Bob accesses /dav/Animeler/ with HTTP Basic Auth
	reqBob := httptest.NewRequest("PROPFIND", "/dav/Animeler/", nil)
	reqBob.Header.Set("Depth", "1")
	reqBob.SetBasicAuth("bob", "pass456")
	wBob := httptest.NewRecorder()
	mux.ServeHTTP(wBob, reqBob)

	if wBob.Code != http.StatusMultiStatus {
		t.Fatalf("Expected 207 Multi-Status for bob, got %d", wBob.Code)
	}
	bobBody := wBob.Body.String()
	if !strings.Contains(bobBody, "Bob Anime Adventure") {
		t.Fatalf("Expected Bob's anime in response, got: %s", bobBody)
	}
	if strings.Contains(bobBody, "Alice In Wonderland") {
		t.Fatalf("Alice's movie leaked into Bob's library!")
	}

	// 5. VLC Playlist (/playlist.m3u) auth check
	reqVlcUnauth := httptest.NewRequest("GET", "/playlist.m3u", nil)
	wVlcUnauth := httptest.NewRecorder()
	mux.ServeHTTP(wVlcUnauth, reqVlcUnauth)
	if wVlcUnauth.Code != http.StatusUnauthorized {
		t.Fatalf("Expected 401 on unauthed /playlist.m3u, got %d", wVlcUnauth.Code)
	}

	reqVlcAlice := httptest.NewRequest("GET", "/playlist.m3u", nil)
	reqVlcAlice.SetBasicAuth("alice", "pass123")
	wVlcAlice := httptest.NewRecorder()
	mux.ServeHTTP(wVlcAlice, reqVlcAlice)
	if wVlcAlice.Code != http.StatusOK {
		t.Fatalf("Expected 200 OK for Alice's playlist, got %d", wVlcAlice.Code)
	}
	vlcContent := wVlcAlice.Body.String()
	if !strings.Contains(vlcContent, "Alice In Wonderland") {
		t.Fatalf("Expected Alice In Wonderland in M3U, got: %s", vlcContent)
	}
	if strings.Contains(vlcContent, "Bob Anime Adventure") {
		t.Fatalf("Bob's media leaked into Alice's M3U playlist!")
	}
}

func TestAuthAPIEndpoints(t *testing.T) {
	_, _, _, mux := setupTestServer(t, true)

	// Register via API
	regBody := `{"username":"tester","password":"superpassword"}`
	reqReg := httptest.NewRequest("POST", "/api/auth/register", strings.NewReader(regBody))
	reqReg.Header.Set("Content-Type", "application/json")
	wReg := httptest.NewRecorder()
	mux.ServeHTTP(wReg, reqReg)

	if wReg.Code != http.StatusOK {
		t.Fatalf("Register API failed with code %d: %s", wReg.Code, wReg.Body.String())
	}

	var regResp struct {
		Status string `json:"status"`
		Token  string `json:"token"`
		User   struct {
			Username string `json:"username"`
		} `json:"user"`
	}
	_ = json.Unmarshal(wReg.Body.Bytes(), &regResp)
	if regResp.Token == "" || regResp.User.Username != "tester" {
		t.Fatalf("Unexpected register response: %+v", regResp)
	}

	// Check /api/auth/me with Bearer token
	reqMe := httptest.NewRequest("GET", "/api/auth/me", nil)
	reqMe.Header.Set("Authorization", "Bearer "+regResp.Token)
	wMe := httptest.NewRecorder()
	mux.ServeHTTP(wMe, reqMe)

	if wMe.Code != http.StatusOK {
		t.Fatalf("Expected 200 OK from /api/auth/me, got %d", wMe.Code)
	}
	if !strings.Contains(wMe.Body.String(), `"username":"tester"`) {
		t.Fatalf("Expected tester in /api/auth/me response, got %s", wMe.Body.String())
	}
}
