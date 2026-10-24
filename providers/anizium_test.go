package providers

import (
	"context"
	"testing"
	"time"
)

func TestAniziumLiveFull(t *testing.T) {
	p := &AniziumProvider{}
	ctx, cancel := context.WithTimeout(context.Background(), 25*time.Second)
	defer cancel()

	// 1. Test GetCatalog
	catalog, err := p.GetCatalog(ctx, 1)
	if err != nil {
		t.Fatalf("GetCatalog failed: %v", err)
	}
	t.Logf("Catalog items count: %d", len(catalog))
	if len(catalog) == 0 {
		t.Fatalf("GetCatalog returned 0 items")
	}
	t.Logf("Catalog item 0: ID=%s Title=%s Type=%s Poster=%s", catalog[0].ID, catalog[0].Title, catalog[0].Type, catalog[0].PosterURL)

	// 2. Test Search
	searchQueries := []string{"Demon Slayer", "Solo Leveling", "Black Clover"}
	for _, query := range searchQueries {
		results, err := p.Search(ctx, query)
		if err != nil {
			t.Errorf("Search failed for '%s': %v", query, err)
			continue
		}
		t.Logf("Search '%s' found %d results", query, len(results))
		if len(results) == 0 {
			t.Errorf("Expected search results for '%s', got 0", query)
		} else {
			t.Logf("  Found: ID=%s Title='%s' Year=%s Type=%s", results[0].ID, results[0].Title, results[0].Year, results[0].Type)
		}
	}

	// 3. Test GetDetails
	det, err := p.GetDetails(ctx, "200709895")
	if err != nil {
		t.Fatalf("GetDetails failed: %v", err)
	}
	t.Logf("Details: Title='%s' Type=%s Seasons=%d", det.Title, det.Type, len(det.Seasons))
	if len(det.Seasons) == 0 {
		t.Fatalf("GetDetails returned 0 seasons")
	}
	t.Logf("Season 1 episodes: %d", len(det.Seasons[0].Episodes))

	// 4. Test GetEpisodes
	eps, err := p.GetEpisodes(ctx, "200709895", 1)
	if err != nil {
		t.Fatalf("GetEpisodes failed: %v", err)
	}
	t.Logf("GetEpisodes Season 1 count: %d", len(eps))
	if len(eps) == 0 {
		t.Fatalf("GetEpisodes returned 0 episodes")
	}
	t.Logf("Episode 1: ID=%s Title='%s' StreamID=%s", eps[0].ID, eps[0].Title, eps[0].StreamID)

	// 5. Test ResolveStream (Series with multiple dubs and subtitles)
	res, err := p.ResolveStream(ctx, "200709895:1:1")
	if err != nil {
		t.Fatalf("ResolveStream series failed: %v", err)
	}
	t.Logf("ResolveStream Primary: Title='%s' Quality=%s URL=%s", res.Primary.Title, res.Primary.Quality, res.Primary.URL)
	t.Logf("ResolveStream Alternates count: %d", len(res.Alternates))
	for i, alt := range res.Alternates {
		t.Logf("  Alt [%d]: Title='%s' Quality=%s URL=%s", i, alt.Title, alt.Quality, alt.URL)
	}
	t.Logf("ResolveStream Subtitles count: %d", len(res.Subtitles))
	for i, sub := range res.Subtitles {
		t.Logf("  Sub [%d]: Lang=%s Label='%s' URL=%s", i, sub.Language, sub.Label, sub.URL)
	}

	if len(res.Subtitles) == 0 {
		t.Errorf("Expected subtitles from Anizium, got 0")
	}
	if len(res.Alternates) == 0 {
		t.Errorf("Expected multiple qualities/dubs in Alternates, got 0")
	}

	// 6. Test ResolveStream (Movie with Turkish dub and multi-subs)
	movieRes, err := p.ResolveStream(ctx, "3138199710558108")
	if err != nil {
		t.Fatalf("ResolveStream movie failed: %v", err)
	}
	t.Logf("Movie ResolveStream Primary: Title='%s' Quality=%s URL=%s", movieRes.Primary.Title, movieRes.Primary.Quality, movieRes.Primary.URL)
	t.Logf("Movie Subtitles count: %d", len(movieRes.Subtitles))
	if len(movieRes.Subtitles) == 0 {
		t.Errorf("Expected subtitles for movie, got 0")
	}
}
