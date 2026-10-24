package auth

import (
	"os"
	"path/filepath"
	"testing"

	"anthology-dav/config"
)

func TestAuthAndLibraryIsolation(t *testing.T) {
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "test_anthology.db")

	db, err := InitDB(dbPath)
	if err != nil {
		t.Fatalf("InitDB failed: %v", err)
	}
	defer db.Close()

	// 1. Test Register
	u1, err := db.Register("alice", "password123")
	if err != nil {
		t.Fatalf("Register alice failed: %v", err)
	}
	if u1.Username != "alice" || u1.ID <= 0 {
		t.Fatalf("Unexpected user: %+v", u1)
	}

	u2, err := db.Register("bob", "secret456")
	if err != nil {
		t.Fatalf("Register bob failed: %v", err)
	}

	// Duplicate registration should fail
	if _, err := db.Register("alice", "anotherpass"); err != ErrUserAlreadyExists {
		t.Fatalf("Expected ErrUserAlreadyExists, got %v", err)
	}

	// Short password should fail
	if _, err := db.Register("charlie", "12"); err != ErrInvalidPassLength {
		t.Fatalf("Expected ErrInvalidPassLength, got %v", err)
	}

	// 2. Test Authenticate
	authed, err := db.Authenticate("alice", "password123")
	if err != nil {
		t.Fatalf("Authenticate alice failed: %v", err)
	}
	if authed.ID != u1.ID {
		t.Fatalf("Expected alice ID %d, got %d", u1.ID, authed.ID)
	}

	if _, err := db.Authenticate("alice", "wrongpassword"); err != ErrInvalidPassword {
		t.Fatalf("Expected ErrInvalidPassword, got %v", err)
	}

	// 3. Test Sessions
	sess, err := db.CreateSession(u1.ID)
	if err != nil {
		t.Fatalf("CreateSession failed: %v", err)
	}
	if sess.Token == "" {
		t.Fatalf("Session token empty")
	}

	validatedUser, err := db.ValidateSession(sess.Token)
	if err != nil {
		t.Fatalf("ValidateSession failed: %v", err)
	}
	if validatedUser.ID != u1.ID {
		t.Fatalf("Expected user %d, got %d", u1.ID, validatedUser.ID)
	}

	// Delete session (logout)
	_ = db.DeleteSession(sess.Token)
	if _, err := db.ValidateSession(sess.Token); err != ErrSessionExpired {
		t.Fatalf("Expected ErrSessionExpired after logout, got %v", err)
	}

	// 4. Test Library Isolation
	// Alice adds Inception (movie)
	itemAlice := config.LibraryItem{
		ID:        "movie:27205",
		TMDBID:    27205,
		Title:     "Inception",
		Year:      "2010",
		Type:      "movie",
		PosterURL: "https://image.tmdb.org/t/p/w500/inception.jpg",
	}
	if err := db.AddUserLibraryItem(u1.ID, itemAlice); err != nil {
		t.Fatalf("Alice AddUserLibraryItem failed: %v", err)
	}

	// Bob adds Solo Leveling (anime)
	itemBob := config.LibraryItem{
		ID:        "anime:127532",
		TMDBID:    127532,
		Title:     "Solo Leveling",
		Year:      "2024",
		Type:      "anime",
		PosterURL: "https://image.tmdb.org/t/p/w500/sololeveling.jpg",
	}
	if err := db.AddUserLibraryItem(u2.ID, itemBob); err != nil {
		t.Fatalf("Bob AddUserLibraryItem failed: %v", err)
	}

	// Verify Alice's library contains only Inception
	aliceLib, err := db.GetUserLibrary(u1.ID)
	if err != nil {
		t.Fatalf("GetUserLibrary alice failed: %v", err)
	}
	if len(aliceLib) != 1 || aliceLib[0].Title != "Inception" {
		t.Fatalf("Expected Alice to have 1 item (Inception), got: %+v", aliceLib)
	}

	// Verify Bob's library contains only Solo Leveling
	bobLib, err := db.GetUserLibrary(u2.ID)
	if err != nil {
		t.Fatalf("GetUserLibrary bob failed: %v", err)
	}
	if len(bobLib) != 1 || bobLib[0].Title != "Solo Leveling" {
		t.Fatalf("Expected Bob to have 1 item (Solo Leveling), got: %+v", bobLib)
	}

	// Test GetUserLibraryByType
	aliceMovies, _ := db.GetUserLibraryByType(u1.ID, "movie")
	if len(aliceMovies) != 1 {
		t.Fatalf("Expected Alice to have 1 movie, got %d", len(aliceMovies))
	}
	aliceAnime, _ := db.GetUserLibraryByType(u1.ID, "anime")
	if len(aliceAnime) != 0 {
		t.Fatalf("Expected Alice to have 0 animes, got %d", len(aliceAnime))
	}

	// Bob removes item
	if err := db.RemoveUserLibraryItem(u2.ID, "anime:127532"); err != nil {
		t.Fatalf("Bob remove failed: %v", err)
	}
	bobLibAfter, _ := db.GetUserLibrary(u2.ID)
	if len(bobLibAfter) != 0 {
		t.Fatalf("Expected Bob to have 0 items after remove, got %d", len(bobLibAfter))
	}

	// Alice's library should still remain intact
	aliceLibAfter, _ := db.GetUserLibrary(u1.ID)
	if len(aliceLibAfter) != 1 {
		t.Fatalf("Alice library should be unaffected, got %d items", len(aliceLibAfter))
	}
}

func init() {
	// Clean env for tests
	_ = os.Setenv("AUTH_MODE", "")
}
