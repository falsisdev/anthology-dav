package providers

import (
	"context"
	"testing"
	"time"
)

func TestSetFilmIzleLive(t *testing.T) {
	p := &SetFilmIzleProvider{}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()

	items, err := p.Search(ctx, "Inception")
	if err != nil {
		t.Fatalf("SetFilmIzle Search failed: %v", err)
	}
	if len(items) == 0 {
		t.Fatalf("SetFilmIzle Search returned 0 items")
	}
	t.Logf("SetFilmIzle found %d items. First: ID=%s Title='%s'", len(items), items[0].ID, items[0].Title)

	res, err := p.ResolveStream(ctx, items[0].ID)
	if err != nil {
		t.Fatalf("SetFilmIzle ResolveStream failed: %v", err)
	}
	if res == nil || res.Primary.URL == "" {
		t.Fatalf("SetFilmIzle returned empty stream")
	}
	t.Logf("SetFilmIzle SUCCESS: StreamURL=%s Quality=%s Headers=%v Subs=%d", res.Primary.URL, res.Primary.Quality, res.Primary.Headers, len(res.Subtitles))
}

func TestHDFilmIzleLive(t *testing.T) {
	p := &HDFilmIzleProvider{}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()

	items, err := p.Search(ctx, "Inception")
	if err != nil {
		t.Fatalf("HDFilmIzle Search failed: %v", err)
	}
	if len(items) == 0 {
		t.Fatalf("HDFilmIzle Search returned 0 items")
	}
	t.Logf("HDFilmIzle found %d items. First: ID=%s Title='%s'", len(items), items[0].ID, items[0].Title)

	res, err := p.ResolveStream(ctx, items[0].ID)
	if err != nil {
		t.Fatalf("HDFilmIzle ResolveStream failed: %v", err)
	}
	if res == nil || res.Primary.URL == "" {
		t.Fatalf("HDFilmIzle returned empty stream")
	}
	t.Logf("HDFilmIzle SUCCESS: StreamURL=%s Quality=%s Headers=%v Subs=%d", res.Primary.URL, res.Primary.Quality, res.Primary.Headers, len(res.Subtitles))
}

func TestKultFilmlerLive(t *testing.T) {
	p := &KultFilmlerProvider{}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()

	items, err := p.Search(ctx, "Inception")
	if err != nil {
		t.Fatalf("KultFilmler Search failed: %v", err)
	}
	if len(items) == 0 {
		t.Fatalf("KultFilmler Search returned 0 items")
	}
	t.Logf("KultFilmler found %d items. First: ID=%s Title='%s'", len(items), items[0].ID, items[0].Title)

	res, err := p.ResolveStream(ctx, items[0].ID)
	if err != nil {
		t.Fatalf("KultFilmler ResolveStream failed: %v", err)
	}
	if res == nil || res.Primary.URL == "" {
		t.Fatalf("KultFilmler returned empty stream")
	}
	t.Logf("KultFilmler SUCCESS: StreamURL=%s Quality=%s Headers=%v Subs=%d", res.Primary.URL, res.Primary.Quality, res.Primary.Headers, len(res.Subtitles))
}

func TestLiderFilmIzleLive(t *testing.T) {
	p := &LiderFilmIzleProvider{}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()

	items, err := p.Search(ctx, "Inception")
	if err != nil {
		t.Fatalf("LiderFilmIzle Search failed: %v", err)
	}
	if len(items) == 0 {
		t.Fatalf("LiderFilmIzle Search returned 0 items")
	}
	t.Logf("LiderFilmIzle found %d items. First: ID=%s Title='%s'", len(items), items[0].ID, items[0].Title)

	res, err := p.ResolveStream(ctx, items[0].ID)
	if err != nil {
		t.Fatalf("LiderFilmIzle ResolveStream failed: %v", err)
	}
	if res == nil || res.Primary.URL == "" {
		t.Fatalf("LiderFilmIzle returned empty stream")
	}
	t.Logf("LiderFilmIzle SUCCESS: StreamURL=%s Quality=%s Headers=%v Subs=%d", res.Primary.URL, res.Primary.Quality, res.Primary.Headers, len(res.Subtitles))
}

func TestSinemaCXLive(t *testing.T) {
	p := &SinemaCXProvider{}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()

	items, err := p.Search(ctx, "Inception")
	if err != nil {
		t.Fatalf("SinemaCX Search failed: %v", err)
	}
	if len(items) == 0 {
		t.Fatalf("SinemaCX Search returned 0 items")
	}
	t.Logf("SinemaCX found %d items. First: ID=%s Title='%s'", len(items), items[0].ID, items[0].Title)

	res, err := p.ResolveStream(ctx, items[0].ID)
	if err != nil {
		t.Fatalf("SinemaCX ResolveStream failed: %v", err)
	}
	if res == nil || res.Primary.URL == "" {
		t.Fatalf("SinemaCX returned empty stream")
	}
	t.Logf("SinemaCX SUCCESS: StreamURL=%s Quality=%s Headers=%v Subs=%d", res.Primary.URL, res.Primary.Quality, res.Primary.Headers, len(res.Subtitles))
}

func TestWebteIzleLive(t *testing.T) {
	p := &WebteIzleProvider{}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()

	items, err := p.Search(ctx, "Inception")
	if err != nil {
		t.Fatalf("WebteIzle Search failed: %v", err)
	}
	if len(items) == 0 {
		t.Fatalf("WebteIzle Search returned 0 items")
	}
	t.Logf("WebteIzle found %d items. First: ID=%s Title='%s'", len(items), items[0].ID, items[0].Title)

	res, err := p.ResolveStream(ctx, items[0].ID)
	if err != nil {
		t.Fatalf("WebteIzle ResolveStream failed: %v", err)
	}
	if res == nil || res.Primary.URL == "" {
		t.Fatalf("WebteIzle returned empty stream")
	}
	t.Logf("WebteIzle SUCCESS: StreamURL=%s Quality=%s Headers=%v Subs=%d", res.Primary.URL, res.Primary.Quality, res.Primary.Headers, len(res.Subtitles))
}

func TestYouTubeDiziFilmLive(t *testing.T) {
	p := &YouTubeDiziFilmProvider{}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()

	items, err := p.Search(ctx, "G.O.R.A.")
	if err != nil {
		t.Fatalf("YouTubeDiziFilm Search failed: %v", err)
	}
	if len(items) == 0 {
		t.Fatalf("YouTubeDiziFilm Search returned 0 items")
	}
	t.Logf("YouTubeDiziFilm found %d items. First: ID=%s Title='%s'", len(items), items[0].ID, items[0].Title)

	res, err := p.ResolveStream(ctx, items[0].ID)
	if err != nil {
		t.Fatalf("YouTubeDiziFilm ResolveStream failed: %v", err)
	}
	if res == nil || res.Primary.URL == "" {
		t.Fatalf("YouTubeDiziFilm returned empty stream")
	}
	t.Logf("YouTubeDiziFilm SUCCESS: StreamURL=%s Quality=%s Headers=%v Subs=%d", res.Primary.URL, res.Primary.Quality, res.Primary.Headers, len(res.Subtitles))
}
