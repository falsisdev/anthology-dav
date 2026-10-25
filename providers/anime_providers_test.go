package providers

import (
	"context"
	"strings"
	"testing"
	"time"
)

func TestSeicodeLive(t *testing.T) {
	p := &SeicodeProvider{}
	ctx, cancel := context.WithTimeout(context.Background(), 25*time.Second)
	defer cancel()

	// 1. Search
	items, err := p.Search(ctx, "Solo Leveling")
	if err != nil {
		t.Fatalf("Seicode Search failed: %v", err)
	}
	if len(items) == 0 {
		t.Fatalf("Seicode Search returned 0 items")
	}
	t.Logf("Seicode found %d items. First: ID=%s Title='%s' Poster=%s", len(items), items[0].ID, items[0].Title, items[0].PosterURL)

	// 2. Details
	det, err := p.GetDetails(ctx, items[0].ID)
	if err != nil {
		t.Fatalf("Seicode GetDetails failed: %v", err)
	}
	t.Logf("Seicode Details: Title='%s' Seasons=%d", det.Title, len(det.Seasons))
	if len(det.Seasons) == 0 || len(det.Seasons[0].Episodes) == 0 {
		t.Fatalf("Seicode Details has no episodes")
	}

	// 3. ResolveStream
	streamID := det.Seasons[0].Episodes[0].StreamID
	res, err := p.ResolveStream(ctx, streamID)
	if err != nil {
		t.Fatalf("Seicode ResolveStream failed: %v", err)
	}
	if res == nil || res.Primary.URL == "" {
		t.Fatalf("Seicode ResolveStream returned empty stream")
	}
	t.Logf("✅ Seicode Stream RESOLVED: Title='%s' Quality=%s URL=%s Format=%s", res.Primary.Title, res.Primary.Quality, res.Primary.URL, res.Primary.Format)
}

func TestSonAnimeLive(t *testing.T) {
	p := &SonAnimeProvider{}
	ctx, cancel := context.WithTimeout(context.Background(), 25*time.Second)
	defer cancel()

	// 1. Search
	items, err := p.Search(ctx, "Demon")
	if err != nil {
		t.Fatalf("SonAnime Search failed: %v", err)
	}
	if len(items) == 0 {
		t.Fatalf("SonAnime Search returned 0 items")
	}
	t.Logf("SonAnime found %d items. First: ID=%s Title='%s' Poster=%s", len(items), items[0].ID, items[0].Title, items[0].PosterURL)

	// 2. Details
	det, err := p.GetDetails(ctx, items[0].ID)
	if err != nil {
		t.Fatalf("SonAnime GetDetails failed: %v", err)
	}
	t.Logf("SonAnime Details: Title='%s' Seasons=%d", det.Title, len(det.Seasons))
	if len(det.Seasons) == 0 || len(det.Seasons[0].Episodes) == 0 {
		t.Fatalf("SonAnime Details has no episodes")
	}

	// 3. ResolveStream
	streamID := det.Seasons[0].Episodes[0].StreamID
	res, err := p.ResolveStream(ctx, streamID)
	if err != nil {
		t.Fatalf("SonAnime ResolveStream failed: %v", err)
	}
	if res == nil || res.Primary.URL == "" {
		t.Fatalf("SonAnime ResolveStream returned empty stream")
	}
	t.Logf("✅ SonAnime Stream RESOLVED: Title='%s' Quality=%s URL=%s Format=%s", res.Primary.Title, res.Primary.Quality, res.Primary.URL, res.Primary.Format)
}

func TestAsyaAnimeleriLive(t *testing.T) {
	p := &AsyaAnimeleriProvider{}
	ctx, cancel := context.WithTimeout(context.Background(), 25*time.Second)
	defer cancel()

	// 1. Search
	items, err := p.Search(ctx, "Demon")
	if err != nil {
		t.Fatalf("AsyaAnimeleri Search failed: %v", err)
	}
	if len(items) == 0 {
		t.Fatalf("AsyaAnimeleri Search returned 0 items")
	}
	t.Logf("AsyaAnimeleri found %d items. First: ID=%s Title='%s'", len(items), items[0].ID, items[0].Title)

	// 2. Details
	det, err := p.GetDetails(ctx, items[0].ID)
	if err != nil {
		t.Fatalf("AsyaAnimeleri GetDetails failed: %v", err)
	}
	t.Logf("AsyaAnimeleri Details: Title='%s' Seasons=%d", det.Title, len(det.Seasons))
	if len(det.Seasons) == 0 || len(det.Seasons[0].Episodes) == 0 {
		t.Fatalf("AsyaAnimeleri Details has no episodes")
	}

	// 3. ResolveStream
	streamID := det.Seasons[0].Episodes[0].StreamID
	res, err := p.ResolveStream(ctx, streamID)
	if err != nil {
		t.Fatalf("AsyaAnimeleri ResolveStream failed: %v", err)
	}
	if res == nil || res.Primary.URL == "" {
		t.Fatalf("AsyaAnimeleri ResolveStream returned empty stream")
	}
	t.Logf("✅ AsyaAnimeleri Stream RESOLVED: Title='%s' Quality=%s URL=%s Format=%s", res.Primary.Title, res.Primary.Quality, res.Primary.URL, res.Primary.Format)
}

func TestAnizmLive(t *testing.T) {
	p := &AnizmProvider{}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	// 1. Search
	items, err := p.Search(ctx, "Demon")
	if err != nil {
		t.Fatalf("Anizm Search failed: %v", err)
	}
	if len(items) == 0 {
		t.Fatalf("Anizm Search returned 0 items")
	}
	t.Logf("Anizm found %d items. First: ID=%s Title='%s'", len(items), items[0].ID, items[0].Title)

	// 2. Details
	det, err := p.GetDetails(ctx, items[0].ID)
	if err != nil {
		t.Fatalf("Anizm GetDetails failed: %v", err)
	}
	t.Logf("Anizm Details: Title='%s' Seasons=%d", det.Title, len(det.Seasons))
	if len(det.Seasons) == 0 || len(det.Seasons[0].Episodes) == 0 {
		t.Fatalf("Anizm Details has no episodes")
	}

	// 3. ResolveStream
	streamID := det.Seasons[0].Episodes[0].StreamID
	res, err := p.ResolveStream(ctx, streamID)
	if err != nil {
		if strings.Contains(err.Error(), "no streams found") || strings.Contains(err.Error(), "403") {
			t.Skipf("⚠️ Anizm streams blocked by Cloudflare Managed Challenge (Turnstile) on player/video endpoints: %v", err)
			return
		}
		t.Fatalf("Anizm ResolveStream failed: %v", err)
	}
	if res == nil || res.Primary.URL == "" {
		t.Fatalf("Anizm ResolveStream returned empty stream")
	}
	t.Logf("✅ Anizm Stream RESOLVED: Title='%s' Quality=%s URL=%s Format=%s", res.Primary.Title, res.Primary.Quality, res.Primary.URL, res.Primary.Format)
}

func TestAnimePraxLive(t *testing.T) {
	p := &AnimePraxProvider{}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	items, err := p.Search(ctx, "Demon")
	if err != nil {
		if strings.Contains(err.Error(), "522") || strings.Contains(err.Error(), "timeout") || strings.Contains(err.Error(), "deadline") || strings.Contains(err.Error(), "connection refused") {
			t.Skipf("AnimePrax upstream origin server is currently offline (Cloudflare 522/timeout): %v", err)
			return
		}
		t.Fatalf("AnimePrax Search failed: %v", err)
	}
	t.Logf("AnimePrax Search found %d items", len(items))
}
