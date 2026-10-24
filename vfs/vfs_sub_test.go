package vfs

import (
	"context"
	"io"
	"strings"
	"testing"

	"anthology-dav/config"
)

func TestVFSSubtitleListingAndReading(t *testing.T) {
	cfg := config.Get()
	cache := NewMemoryCache()
	vfsInstance := NewAnthologyVFS(cfg, cache)

	// Add dummy anime item to library
	dummyItem := config.LibraryItem{
		ID:            "test_anime_1",
		TMDBID:        200709895,
		Title:         "Black Clover",
		Year:          "2017",
		Type:          "anime",
		Seasons:       []config.LibrarySeason{{SeasonNumber: 1, EpisodeCount: 2}},
	}
	_ = config.Library().Add(dummyItem)
	defer func() {
		_ = config.Library().Remove(dummyItem.ID)
	}()

	// Wire subtitle resolver
	vfsInstance.SetSubtitleResolver(func(ctx context.Context, mediaType string, tmdbID int, title, origTitle, year string, season, episode int, lang, format string) ([]byte, error) {
		if format == "srt" {
			return []byte("1\n00:00:01,000 --> 00:00:04,000\nTest Altyazi\n"), nil
		}
		return []byte("WEBVTT\n\n00:00:01.000 --> 00:00:04.000\nTest Altyazi\n"), nil
	})

	ctx := context.Background()

	// 1. Browse Season 01 directory
	seasonDir, err := vfsInstance.OpenFile(ctx, "/Animeler/Black Clover (2017)/Season 01", 0, 0)
	if err != nil {
		t.Fatalf("Failed to open Season 01 directory: %v", err)
	}
	defer seasonDir.Close()

	entries, err := seasonDir.Readdir(-1)
	if err != nil {
		t.Fatalf("Failed to readdir: %v", err)
	}

	t.Logf("Season 01 file count: %d", len(entries))
	var hasStrm, hasTrVtt, hasTrSrt bool
	for _, fi := range entries {
		t.Logf("  File: %s (size: %d)", fi.Name(), fi.Size())
		if strings.HasSuffix(fi.Name(), ".strm") {
			hasStrm = true
		}
		if strings.HasSuffix(fi.Name(), ".tr.vtt") {
			hasTrVtt = true
		}
		if strings.HasSuffix(fi.Name(), ".tr.srt") {
			hasTrSrt = true
		}
	}

	if !hasStrm {
		t.Errorf("Expected .strm file in Season 01 directory")
	}
	if !hasTrVtt {
		t.Errorf("Expected .tr.vtt file in Season 01 directory")
	}
	if !hasTrSrt {
		t.Errorf("Expected .tr.srt file in Season 01 directory")
	}

	// 2. Open and read .tr.vtt subtitle file
	vttFile, err := vfsInstance.OpenFile(ctx, "/Animeler/Black Clover (2017)/Season 01/Black Clover (2017) - S01E01.tr.vtt", 0, 0)
	if err != nil {
		t.Fatalf("Failed to open .tr.vtt file: %v", err)
	}
	defer vttFile.Close()

	data, err := io.ReadAll(vttFile)
	if err != nil {
		t.Fatalf("Failed to read .tr.vtt: %v", err)
	}
	t.Logf("Read .tr.vtt content:\n%s", string(data))
	if !strings.Contains(string(data), "WEBVTT") || !strings.Contains(string(data), "Test Altyazi") {
		t.Errorf("Unexpected .tr.vtt content")
	}

	// 3. Open and read .tr.srt subtitle file
	srtFile, err := vfsInstance.OpenFile(ctx, "/Animeler/Black Clover (2017)/Season 01/Black Clover (2017) - S01E01.tr.srt", 0, 0)
	if err != nil {
		t.Fatalf("Failed to open .tr.srt file: %v", err)
	}
	defer srtFile.Close()

	srtData, err := io.ReadAll(srtFile)
	if err != nil {
		t.Fatalf("Failed to read .tr.srt: %v", err)
	}
	t.Logf("Read .tr.srt content:\n%s", string(srtData))
	if !strings.Contains(string(srtData), "00:00:01,000 --> 00:00:04,000") || !strings.Contains(string(srtData), "Test Altyazi") {
		t.Errorf("Unexpected .tr.srt content")
	}
}
