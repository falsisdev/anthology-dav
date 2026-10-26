package vfs

import (
	"context"
	"io"
	"strings"
	"testing"

	"anthology-dav/config"
	"anthology-dav/providers"
)

func TestVFSAudioVariantsListing(t *testing.T) {
	cfg := config.Get()
	cache := NewMemoryCache()
	vfsInstance := NewAnthologyVFS(cfg, cache)

	// Add dummy movie and anime
	dummyMovie := config.LibraryItem{
		ID:     "test_movie_var",
		TMDBID: 999991,
		Title:  "Inception",
		Year:   "2010",
		Type:   "movie",
	}
	dummyAnime := config.LibraryItem{
		ID:      "test_anime_var",
		TMDBID:  999992,
		Title:   "Attack on Titan",
		Year:    "2013",
		Type:    "anime",
		Seasons: []config.LibrarySeason{{SeasonNumber: 1, EpisodeCount: 1}},
	}

	_ = config.Library().Add(dummyMovie)
	_ = config.Library().Add(dummyAnime)
	defer func() {
		_ = config.Library().Remove(dummyMovie.ID)
		_ = config.Library().Remove(dummyAnime.ID)
	}()

	// Wire source scanner to return mock discovered sources with audio types
	vfsInstance.SetSourceDiscoverer(func(ctx context.Context, mediaType string, tmdbID int, title, origTitle, year string, season, episode int) []VFSDiscoveredSource {
		return []VFSDiscoveredSource{
			{
				ProviderID:   "filmmodu",
				ProviderName: "FilmModu",
				Quality:      "1080p",
				AudioType:    string(providers.AudioTypeSub),
				AudioLabel:   "💬 Türkçe Altyazı",
			},
			{
				ProviderID:   "filmmodu",
				ProviderName: "FilmModu",
				Quality:      "1080p",
				AudioType:    string(providers.AudioTypeDub),
				AudioLabel:   "🎙️ Türkçe Dublaj",
			},
		}
	})

	ctx := context.Background()

	// 1. Check Movie Directory
	movieDir, err := vfsInstance.OpenFile(ctx, "/Filmler/Inception (2010)", 0, 0)
	if err != nil {
		t.Fatalf("Failed to open movie dir: %v", err)
	}
	defer movieDir.Close()

	entries, err := movieDir.Readdir(-1)
	if err != nil {
		t.Fatalf("Failed to readdir movie: %v", err)
	}

	var foundAuto, foundSub, foundDub bool
	for _, fi := range entries {
		t.Logf("Movie entry: %s", fi.Name())
		if fi.Name() == "Inception (2010).strm" {
			foundAuto = true
		}
		if fi.Name() == "Inception (2010) [TR Altyazi].strm" {
			foundSub = true
		}
		if fi.Name() == "Inception (2010) [TR Dublaj].strm" {
			foundDub = true
		}
	}

	if !foundAuto || !foundSub || !foundDub {
		t.Fatalf("Expected auto, sub, and dub .strm files in movie folder: auto=%v, sub=%v, dub=%v", foundAuto, foundSub, foundDub)
	}

	// 2. Read Movie Sub STRM content and check audio query param
	subFile, err := vfsInstance.OpenFile(ctx, "/Filmler/Inception (2010)/Inception (2010) [TR Altyazi].strm", 0, 0)
	if err != nil {
		t.Fatalf("Failed to open sub .strm: %v", err)
	}
	defer subFile.Close()
	content, err := io.ReadAll(subFile)
	if err != nil {
		t.Fatalf("Failed to read sub .strm: %v", err)
	}
	if !strings.Contains(string(content), "audio=sub") {
		t.Errorf("Expected 'audio=sub' in strm content, got: %s", string(content))
	}

	// 3. Check Anime Season Directory (should have [EN Dublaj].strm as well)
	animeDir, err := vfsInstance.OpenFile(ctx, "/Animeler/Attack on Titan (2013)/Season 01", 0, 0)
	if err != nil {
		t.Fatalf("Failed to open anime season dir: %v", err)
	}
	defer animeDir.Close()

	animeEntries, err := animeDir.Readdir(-1)
	if err != nil {
		t.Fatalf("Failed to readdir anime season: %v", err)
	}

	var foundAnimeAuto, foundAnimeSub, foundAnimeDub, foundAnimeEnDub bool
	for _, fi := range animeEntries {
		t.Logf("Anime entry: %s", fi.Name())
		if fi.Name() == "Attack on Titan (2013) - S01E01.strm" {
			foundAnimeAuto = true
		}
		if fi.Name() == "Attack on Titan (2013) - S01E01 [TR Altyazi].strm" {
			foundAnimeSub = true
		}
		if fi.Name() == "Attack on Titan (2013) - S01E01 [TR Dublaj].strm" {
			foundAnimeDub = true
		}
		if fi.Name() == "Attack on Titan (2013) - S01E01 [EN Dublaj].strm" {
			foundAnimeEnDub = true
		}
	}

	if !foundAnimeAuto || !foundAnimeSub || !foundAnimeDub || !foundAnimeEnDub {
		t.Fatalf("Anime should have Auto, Sub, Dub, and EnDub: auto=%v, sub=%v, dub=%v, endub=%v", foundAnimeAuto, foundAnimeSub, foundAnimeDub, foundAnimeEnDub)
	}

	// 4. Check Movie _Kaynak_Sec_ Directory
	srcDir, err := vfsInstance.OpenFile(ctx, "/_Kaynak_Sec_/Filmler/Inception (2010)", 0, 0)
	if err != nil {
		t.Fatalf("Failed to open _Kaynak_Sec_: %v", err)
	}
	defer srcDir.Close()

	srcEntries, err := srcDir.Readdir(-1)
	if err != nil {
		t.Fatalf("Failed to readdir _Kaynak_Sec_: %v", err)
	}

	var foundBestSub, foundBestDub, foundFilmModuSub, foundFilmModuDub bool
	for _, fi := range srcEntries {
		t.Logf("_Kaynak_Sec_ entry: %s", fi.Name())
		if strings.Contains(fi.Name(), "En Iyi Altyazili") {
			foundBestSub = true
		}
		if strings.Contains(fi.Name(), "En Iyi Dublaj") {
			foundBestDub = true
		}
		if strings.Contains(fi.Name(), "FilmModu") && strings.Contains(fi.Name(), "Altyazı") {
			foundFilmModuSub = true
		}
		if strings.Contains(fi.Name(), "FilmModu") && strings.Contains(fi.Name(), "Dublaj") {
			foundFilmModuDub = true
		}
	}

	if !foundBestSub || !foundBestDub || !foundFilmModuSub || !foundFilmModuDub {
		t.Fatalf("Expected all _Kaynak_Sec_ audio variant items: bestSub=%v, bestDub=%v, fmSub=%v, fmDub=%v",
			foundBestSub, foundBestDub, foundFilmModuSub, foundFilmModuDub)
	}
}
