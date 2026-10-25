package providers_test

import (
	"context"
	"testing"
	"time"

	"anthology-dav/providers"
)

func TestHDFilmDelisiLive(t *testing.T) {
	p, ok := providers.GetByID("hdfilmdelisi")
	if !ok || p == nil {
		t.Fatal("provider hdfilmdelisi not found")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 25*time.Second)
	defer cancel()

	// 1. Search
	items, err := p.Search(ctx, "avatar")
	if err != nil {
		t.Fatalf("Search error: %v", err)
	}
	t.Logf("Search found %d items", len(items))
	if len(items) == 0 {
		t.Fatal("expected at least 1 search result")
	}
	for i, item := range items {
		if i >= 3 {
			break
		}
		t.Logf("  - [%s] %s (%s)", item.ID, item.Title, item.Year)
	}

	// 2. ResolveStream
	res, err := p.ResolveStream(ctx, "avatar")
	if err != nil {
		t.Fatalf("ResolveStream error: %v", err)
	}
	if res == nil || res.Primary.URL == "" {
		t.Fatal("expected stream result, got nil or empty URL")
	}
	t.Logf("SUCCESS! Stream found: Name=%s, Quality=%s, URL=%s", res.Primary.Name, res.Primary.Quality, res.Primary.URL)
}
