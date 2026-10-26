package stream

import (
	"context"
	"testing"
	"time"

	"anthology-dav/config"
	"anthology-dav/providers"
	"anthology-dav/vfs"
)

func TestAniziumViaMultiResolver(t *testing.T) {
	cfg := config.Get()
	cache := vfs.NewMemoryCache()
	proxy := NewProxyHandler(cfg, cache)
	mr := NewMultiResolver(proxy)

	ctx, cancel := context.WithTimeout(context.Background(), 25*time.Second)
	defer cancel()

	// 1. Test direct resolveFromProvider for Anizium
	prov, ok := providers.GetByID("anizium")
	if !ok {
		t.Fatalf("Anizium provider not found in registry")
	}

	res, err := mr.resolveFromProvider(ctx, prov, "anime", 0, "Demon Slayer", "", "2019", []string{"Kimetsu no Yaiba"}, 1, 1)
	if err != nil {
		t.Fatalf("resolveFromProvider for Demon Slayer failed: %v", err)
	}
	t.Logf("Demon Slayer S01E01 resolved from Anizium:")
	t.Logf("  Primary Title: %s", res.Primary.Title)
	t.Logf("  Primary Quality: %s", res.Primary.Quality)
	t.Logf("  Primary URL: %s", res.Primary.URL)
	t.Logf("  Alternates count: %d", len(res.Alternates))
	t.Logf("  Subtitles count: %d", len(res.Subtitles))

	if len(res.Subtitles) == 0 {
		t.Errorf("Expected subtitles in Demon Slayer stream result")
	}

	// 2. Test DiscoverSources for Black Clover
	sources := mr.DiscoverSources(ctx, "anime", 0, "Black Clover", "", "2017", 1, 1)
	t.Logf("DiscoverSources found: %d sources", len(sources))
	var foundAnizium bool
	for _, s := range sources {
		t.Logf("  Source: Prov=%s Quality=%s Title=%s Format=%s", s.ProviderID, s.Quality, s.Title, s.Format)
		if s.ProviderID == "anizium" {
			foundAnizium = true
		}
	}
	if !foundAnizium {
		t.Errorf("Expected anizium in discovered sources for Black Clover S01E01")
	}
}

func TestRevivedProvidersViaMultiResolver(t *testing.T) {
	cfg := config.Get()
	cache := vfs.NewMemoryCache()
	proxy := NewProxyHandler(cfg, cache)
	mr := NewMultiResolver(proxy)

	ctx, cancel := context.WithTimeout(context.Background(), 35*time.Second)
	defer cancel()

	// 1. Test YabancıDizi via MultiResolver
	provYD, ok := providers.GetByID("yabancidizi")
	if !ok {
		t.Fatalf("yabancidizi provider not found")
	}
	resYD, err := mr.resolveFromProvider(ctx, provYD, "series", 0, "Treasure Island", "", "2026", nil, 1, 3)
	if err != nil {
		t.Logf("resolveFromProvider for Treasure Island yabancidizi: %v", err)
	} else if resYD != nil && resYD.Primary.URL != "" {
		t.Logf("SUCCESS YabancıDizi via MultiResolver: URL=%s", resYD.Primary.URL)
	}

	// 2. Test HDFilmDelisi via MultiResolver
	provHDFD, ok := providers.GetByID("hdfilmdelisi")
	if !ok {
		t.Fatalf("hdfilmdelisi provider not found")
	}
	resHDFD, err := mr.resolveFromProvider(ctx, provHDFD, "movie", 0, "Avatar", "", "2009", nil, 1, 1)
	if err != nil {
		t.Logf("resolveFromProvider for Avatar hdfilmdelisi: %v", err)
	} else if resHDFD != nil && resHDFD.Primary.URL != "" {
		t.Logf("SUCCESS HDFilmDelisi via MultiResolver: URL=%s", resHDFD.Primary.URL)
	}

	// 3. Test Vidmody via MultiResolver
	provVM, ok := providers.GetByID("vidmody")
	if !ok {
		t.Fatalf("vidmody provider not found")
	}
	resVM, err := mr.resolveFromProvider(ctx, provVM, "movie", 19995, "Avatar", "", "2009", nil, 1, 1) // TMDB 19995 = Avatar
	if err != nil {
		t.Logf("resolveFromProvider for Avatar vidmody: %v", err)
	} else if resVM != nil && resVM.Primary.URL != "" {
		t.Logf("SUCCESS Vidmody via MultiResolver: URL=%s", resVM.Primary.URL)
	}
}

func TestFilterAndSortStreamsByAudio(t *testing.T) {
	sourceSub := providers.StreamSource{
		URL:       "https://example.com/sub_1080p.m3u8",
		Quality:   "1080p",
		AudioType: providers.AudioTypeSub,
		Title:     "Film • [TR Altyazı] • 1080p",
	}
	sourceDub := providers.StreamSource{
		URL:       "https://example.com/dub_1080p.m3u8",
		Quality:   "1080p",
		AudioType: providers.AudioTypeDub,
		Title:     "Film • [TR Dublaj] • 1080p",
	}
	sourceEnDub := providers.StreamSource{
		URL:       "https://example.com/endub_1080p.m3u8",
		Quality:   "1080p",
		AudioType: providers.AudioTypeEnDub,
		Title:     "Anime • İngilizce Dublaj • 1080p",
	}

	allSources := []providers.StreamSource{sourceEnDub, sourceDub, sourceSub}

	// 1. "sub" preference
	resSub := FilterAndSortStreamsByAudio(allSources, "sub")
	if resSub == nil || resSub.Primary.AudioType != providers.AudioTypeSub {
		t.Fatalf("Expected AudioTypeSub when audio=sub, got %+v", resSub)
	}

	// 2. "dub" preference
	resDub := FilterAndSortStreamsByAudio(allSources, "dub")
	if resDub == nil || resDub.Primary.AudioType != providers.AudioTypeDub {
		t.Fatalf("Expected AudioTypeDub when audio=dub, got %+v", resDub)
	}

	// 3. "en_dub" preference
	resEnDub := FilterAndSortStreamsByAudio(allSources, "en_dub")
	if resEnDub == nil || resEnDub.Primary.AudioType != providers.AudioTypeEnDub {
		t.Fatalf("Expected AudioTypeEnDub when audio=en_dub, got %+v", resEnDub)
	}

	// 4. "auto" or empty preference (should prioritize Sub / Original over Dub per user rules)
	resAuto := FilterAndSortStreamsByAudio(allSources, "auto")
	if resAuto == nil || resAuto.Primary.AudioType != providers.AudioTypeSub {
		t.Fatalf("Expected AudioTypeSub to be prioritized on 'auto', got %+v", resAuto)
	}

	// 5. Fallback test: Requesting "en_dub" when only Dub & Sub exist
	dubAndSubOnly := []providers.StreamSource{sourceDub, sourceSub}
	resFallback := FilterAndSortStreamsByAudio(dubAndSubOnly, "en_dub")
	if resFallback == nil || resFallback.Primary.URL == "" {
		t.Fatalf("Expected fallback stream when requested audio is missing, got %+v", resFallback)
	}
}
