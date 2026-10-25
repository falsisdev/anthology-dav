package providers_test

import (
	"context"
	"testing"
	"time"

	"anthology-dav/providers"
)

func TestVidmodyLive(t *testing.T) {
	p, ok := providers.GetByID("vidmody")
	if !ok || p == nil {
		t.Fatal("provider vidmody not found")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 25*time.Second)
	defer cancel()

	// Test 1: Movie (Avatar tt0499549)
	res, err := p.ResolveStream(ctx, "movie:tt0499549")
	if err != nil {
		t.Fatalf("ResolveStream movie error: %v", err)
	}
	if res == nil || res.Primary.URL == "" {
		t.Fatal("expected movie stream result, got nil or empty URL")
	}
	t.Logf("SUCCESS (Movie)! Stream found: Name=%s, Quality=%s, URL=%s", res.Primary.Name, res.Primary.Quality, res.Primary.URL)

	// Test 2: Series (Breaking Bad tt0903747 S01E01)
	resSeries, err := p.ResolveStream(ctx, "tv:tt0903747:1:1")
	if err != nil {
		t.Fatalf("ResolveStream series error: %v", err)
	}
	if resSeries == nil || resSeries.Primary.URL == "" {
		t.Fatal("expected series stream result, got nil or empty URL")
	}
	t.Logf("SUCCESS (Series)! Stream found: Name=%s, Quality=%s, URL=%s", resSeries.Primary.Name, resSeries.Primary.Quality, resSeries.Primary.URL)
}
