package vfs

import (
	"context"
	"io"
	"strings"
	"testing"

	"anthology-dav/config"
	"anthology-dav/providers"
)

func TestVFSMultiQualityListing(t *testing.T) {
	cfg := config.Get()
	cache := NewMemoryCache()
	vfsInstance := NewAnthologyVFS(cfg, cache)

	dummyMovie := config.LibraryItem{
		ID:     "test_movie_qual",
		TMDBID: 888881,
		Title:  "Interstellar",
		Year:   "2014",
		Type:   "movie",
	}

	_ = config.Library().Add(dummyMovie)
	defer func() {
		_ = config.Library().Remove(dummyMovie.ID)
	}()

	// Wire source scanner to return mock discovered source with multiple qualities
	vfsInstance.SetSourceDiscoverer(func(ctx context.Context, mediaType string, tmdbID int, title, origTitle, year string, season, episode int) []VFSDiscoveredSource {
		return []VFSDiscoveredSource{
			{
				ProviderID:         "anizium",
				ProviderName:       "Anizium",
				Quality:            "4K UHD",
				AudioType:          string(providers.AudioTypeSub),
				AudioLabel:         "💬 Türkçe Altyazı",
				AvailableQualities: []string{"4K UHD", "1080p", "720p", "480p"},
			},
		}
	})

	ctx := context.Background()

	// 1. Open _Kaynak_Sec_ directory for Interstellar
	srcDir, err := vfsInstance.OpenFile(ctx, "/_Kaynak_Sec_/Filmler/Interstellar (2014)", 0, 0)
	if err != nil {
		t.Fatalf("Failed to open _Kaynak_Sec_: %v", err)
	}
	defer srcDir.Close()

	entries, err := srcDir.Readdir(-1)
	if err != nil {
		t.Fatalf("Failed to readdir: %v", err)
	}

	var found4k, found1080, found720, found480 bool
	for _, fi := range entries {
		t.Logf("Entry: %s (size: %d)", fi.Name(), fi.Size())
		if strings.Contains(fi.Name(), "[4K UHD]") {
			found4k = true
		}
		if strings.Contains(fi.Name(), "[1080p]") {
			found1080 = true
		}
		if strings.Contains(fi.Name(), "[720p]") {
			found720 = true
		}
		if strings.Contains(fi.Name(), "[480p]") {
			found480 = true
		}
	}

	if !found4k || !found1080 || !found720 || !found480 {
		t.Fatalf("Expected all resolution files in _Kaynak_Sec_: 4k=%v, 1080=%v, 720=%v, 480=%v",
			found4k, found1080, found720, found480)
	}

	// 2. Read content of 720p strm file and verify quality parameter
	strm720File, err := vfsInstance.OpenFile(ctx, "/_Kaynak_Sec_/Filmler/Interstellar (2014)/5 - Anizium [💬 Türkçe Altyazı] [720p] - Interstellar (2014).strm", 0, 0)
	if err != nil {
		t.Fatalf("Failed to open 720p strm file: %v", err)
	}
	defer strm720File.Close()

	data, err := io.ReadAll(strm720File)
	if err != nil {
		t.Fatalf("Failed to read 720p strm: %v", err)
	}
	content := string(data)
	t.Logf("720p STRM content: %s", content)

	if !strings.Contains(content, "quality=720p") {
		t.Errorf("Expected 'quality=720p' in strm content, got: %s", content)
	}
}
