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
