package providers

import (
	"context"
	"testing"
	"time"
)

func TestDiziBoxIzleLive(t *testing.T) {
	p := &DiziBoxProvider{}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	items, err := p.Search(ctx, "The Pirate Bay")
	if err != nil {
		t.Fatalf("DiziBox Search failed: %v", err)
	}
	if len(items) == 0 {
		t.Fatalf("DiziBox Search returned 0 items")
	}
	t.Logf("DiziBox found %d items. First: ID=%s Title='%s'", len(items), items[0].ID, items[0].Title)

	det, err := p.GetDetails(ctx, items[0].ID)
	if err != nil {
		t.Fatalf("DiziBox GetDetails failed: %v", err)
	}
	t.Logf("DiziBox Details: Title='%s' Seasons=%d", det.Title, len(det.Seasons))
	if len(det.Seasons) == 0 || len(det.Seasons[0].Episodes) == 0 {
		t.Fatalf("DiziBox Details has no episodes")
	}

	streamID := det.Seasons[0].Episodes[0].StreamID
	t.Logf("DiziBox resolving stream for: %s", streamID)
	res, err := p.ResolveStream(ctx, streamID)
	if err != nil {
		t.Fatalf("DiziBox ResolveStream failed: %v", err)
	}
	if res == nil || res.Primary.URL == "" {
		t.Fatalf("DiziBox ResolveStream returned empty stream")
	}
	t.Logf("✅ DiziBox Stream RESOLVED: Title='%s' Quality=%s URL=%s Format=%s", res.Primary.Title, res.Primary.Quality, res.Primary.URL, res.Primary.Format)
}

func TestDiziYouLive(t *testing.T) {
	p := &DiziYouProvider{}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	items, err := p.Search(ctx, "Breaking Bad")
	if err != nil {
		t.Fatalf("DiziYou Search failed: %v", err)
	}
	if len(items) == 0 {
		t.Fatalf("DiziYou Search returned 0 items")
	}
	t.Logf("DiziYou found %d items. First: ID=%s Title='%s'", len(items), items[0].ID, items[0].Title)

	det, err := p.GetDetails(ctx, items[0].ID)
	if err != nil {
		t.Fatalf("DiziYou GetDetails failed: %v", err)
	}
	t.Logf("DiziYou Details: Title='%s' Seasons=%d", det.Title, len(det.Seasons))
	if len(det.Seasons) == 0 || len(det.Seasons[0].Episodes) == 0 {
		t.Fatalf("DiziYou Details has no episodes")
	}

	streamID := det.Seasons[0].Episodes[0].StreamID
	t.Logf("DiziYou resolving stream for: %s", streamID)
	res, err := p.ResolveStream(ctx, streamID)
	if err != nil {
		t.Fatalf("DiziYou ResolveStream failed: %v", err)
	}
	if res == nil || res.Primary.URL == "" {
		t.Fatalf("DiziYou ResolveStream returned empty stream")
	}
	t.Logf("✅ DiziYou Stream RESOLVED: Title='%s' Quality=%s URL=%s Format=%s", res.Primary.Title, res.Primary.Quality, res.Primary.URL, res.Primary.Format)
}

func TestDiziMomLive(t *testing.T) {
	p := &DiziMomProvider{}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	items, err := p.Search(ctx, "Tulsa King")
	if err != nil {
		t.Fatalf("DiziMom Search failed: %v", err)
	}
	if len(items) == 0 {
		t.Fatalf("DiziMom Search returned 0 items")
	}
	t.Logf("DiziMom found %d items. First: ID=%s Title='%s'", len(items), items[0].ID, items[0].Title)

	det, err := p.GetDetails(ctx, items[0].ID)
	if err != nil {
		t.Fatalf("DiziMom GetDetails failed: %v", err)
	}
	t.Logf("DiziMom Details: Title='%s' Seasons=%d", det.Title, len(det.Seasons))
	if len(det.Seasons) == 0 || len(det.Seasons[0].Episodes) == 0 {
		t.Fatalf("DiziMom Details has no episodes")
	}

	streamID := det.Seasons[0].Episodes[0].StreamID
	t.Logf("DiziMom resolving stream for: %s", streamID)
	res, err := p.ResolveStream(ctx, streamID)
	if err != nil {
		t.Fatalf("DiziMom ResolveStream failed: %v", err)
	}
	if res == nil || res.Primary.URL == "" {
		t.Fatalf("DiziMom ResolveStream returned empty stream")
	}
	t.Logf("✅ DiziMom Stream RESOLVED: Title='%s' Quality=%s URL=%s Format=%s", res.Primary.Title, res.Primary.Quality, res.Primary.URL, res.Primary.Format)
}

func TestDiziPodLive(t *testing.T) {
	p := &DiziPodProvider{}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	items, err := p.Search(ctx, "Breaking Bad")
	if err != nil {
		t.Fatalf("DiziPod Search failed: %v", err)
	}
	if len(items) == 0 {
		t.Fatalf("DiziPod Search returned 0 items")
	}
	t.Logf("DiziPod found %d items. First: ID=%s Title='%s'", len(items), items[0].ID, items[0].Title)

	det, err := p.GetDetails(ctx, items[0].ID)
	if err != nil {
		t.Fatalf("DiziPod GetDetails failed: %v", err)
	}
	t.Logf("DiziPod Details: Title='%s' Seasons=%d", det.Title, len(det.Seasons))
	if len(det.Seasons) == 0 || len(det.Seasons[0].Episodes) == 0 {
		t.Fatalf("DiziPod Details has no episodes")
	}

	streamID := det.Seasons[0].Episodes[0].StreamID
	t.Logf("DiziPod resolving stream for: %s", streamID)
	res, err := p.ResolveStream(ctx, streamID)
	if err != nil {
		t.Fatalf("DiziPod ResolveStream failed: %v", err)
	}
	if res == nil || res.Primary.URL == "" {
		t.Fatalf("DiziPod ResolveStream returned empty stream")
	}
	t.Logf("✅ DiziPod Stream RESOLVED: Title='%s' Quality=%s URL=%s Format=%s", res.Primary.Title, res.Primary.Quality, res.Primary.URL, res.Primary.Format)
}

func TestDiziBakLive(t *testing.T) {
	p := &DiziBakProvider{}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	items, err := p.Search(ctx, "Breaking Bad")
	if err != nil {
		t.Fatalf("DiziBak Search failed: %v", err)
	}
	if len(items) == 0 {
		t.Fatalf("DiziBak Search returned 0 items")
	}
	t.Logf("DiziBak found %d items. First: ID=%s Title='%s'", len(items), items[0].ID, items[0].Title)

	det, err := p.GetDetails(ctx, items[0].ID)
	if err != nil {
		t.Fatalf("DiziBak GetDetails failed: %v", err)
	}
	t.Logf("DiziBak Details: Title='%s' Seasons=%d", det.Title, len(det.Seasons))
	if len(det.Seasons) == 0 || len(det.Seasons[0].Episodes) == 0 {
		t.Fatalf("DiziBak Details has no episodes")
	}

	streamID := det.Seasons[0].Episodes[0].StreamID
	t.Logf("DiziBak resolving stream for: %s", streamID)
	res, err := p.ResolveStream(ctx, streamID)
	if err != nil {
		t.Fatalf("DiziBak ResolveStream failed: %v", err)
	}
	if res == nil || res.Primary.URL == "" {
		t.Fatalf("DiziBak ResolveStream returned empty stream")
	}
	t.Logf("✅ DiziBak Stream RESOLVED: Title='%s' Quality=%s URL=%s Format=%s", res.Primary.Title, res.Primary.Quality, res.Primary.URL, res.Primary.Format)
}

func TestDDiziLive(t *testing.T) {
	p := &DDiziProvider{}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	items, err := p.Search(ctx, "Kızılcık Şerbeti")
	if err != nil {
		t.Fatalf("DDizi Search failed: %v", err)
	}
	if len(items) == 0 {
		t.Fatalf("DDizi Search returned 0 items")
	}
	t.Logf("DDizi found %d items. First: ID=%s Title='%s'", len(items), items[0].ID, items[0].Title)

	det, err := p.GetDetails(ctx, items[0].ID)
	if err != nil {
		t.Fatalf("DDizi GetDetails failed: %v", err)
	}
	t.Logf("DDizi Details: Title='%s' Seasons=%d", det.Title, len(det.Seasons))
	if len(det.Seasons) == 0 || len(det.Seasons[0].Episodes) == 0 {
		t.Fatalf("DDizi Details has no episodes")
	}

	streamID := det.Seasons[0].Episodes[0].StreamID
	t.Logf("DDizi resolving stream for: %s", streamID)
	res, err := p.ResolveStream(ctx, streamID)
	if err != nil {
		t.Fatalf("DDizi ResolveStream failed: %v", err)
	}
	if res == nil || res.Primary.URL == "" {
		t.Fatalf("DDizi ResolveStream returned empty stream")
	}
	t.Logf("✅ DDizi Stream RESOLVED: Title='%s' Quality=%s URL=%s Format=%s", res.Primary.Title, res.Primary.Quality, res.Primary.URL, res.Primary.Format)
}

func TestPuhuTVLive(t *testing.T) {
	p := &PuhuTVProvider{}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	items, err := p.Search(ctx, "Behzat Ç")
	if err != nil {
		t.Fatalf("PuhuTV Search failed: %v", err)
	}
	if len(items) == 0 {
		t.Fatalf("PuhuTV Search returned 0 items")
	}
	t.Logf("PuhuTV found %d items. First: ID=%s Title='%s'", len(items), items[0].ID, items[0].Title)

	det, err := p.GetDetails(ctx, items[0].ID)
	if err != nil {
		t.Fatalf("PuhuTV GetDetails failed: %v", err)
	}
	t.Logf("PuhuTV Details: Title='%s' Seasons=%d", det.Title, len(det.Seasons))
	if len(det.Seasons) == 0 || len(det.Seasons[0].Episodes) == 0 {
		t.Fatalf("PuhuTV Details has no episodes")
	}

	streamID := det.Seasons[0].Episodes[0].StreamID
	t.Logf("PuhuTV resolving stream for: %s", streamID)
	res, err := p.ResolveStream(ctx, streamID)
	if err != nil {
		t.Fatalf("PuhuTV ResolveStream failed: %v", err)
	}
	if res == nil || res.Primary.URL == "" {
		t.Fatalf("PuhuTV ResolveStream returned empty stream")
	}
	t.Logf("✅ PuhuTV Stream RESOLVED: Title='%s' Quality=%s URL=%s Format=%s", res.Primary.Title, res.Primary.Quality, res.Primary.URL, res.Primary.Format)
}

func TestTvDizilerLive(t *testing.T) {
	p := &TvDizilerProvider{}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	items, err := p.Search(ctx, "Adım Farah")
	if err != nil {
		t.Fatalf("TvDiziler Search failed: %v", err)
	}
	if len(items) == 0 {
		t.Fatalf("TvDiziler Search returned 0 items")
	}
	t.Logf("TvDiziler found %d items. First: ID=%s Title='%s'", len(items), items[0].ID, items[0].Title)

	det, err := p.GetDetails(ctx, items[0].ID)
	if err != nil {
		t.Fatalf("TvDiziler GetDetails failed: %v", err)
	}
	t.Logf("TvDiziler Details: Title='%s' Seasons=%d", det.Title, len(det.Seasons))
	if len(det.Seasons) == 0 || len(det.Seasons[0].Episodes) == 0 {
		t.Fatalf("TvDiziler Details has no episodes")
	}

	streamID := det.Seasons[0].Episodes[0].StreamID
	t.Logf("TvDiziler resolving stream for: %s", streamID)
	res, err := p.ResolveStream(ctx, streamID)
	if err != nil {
		t.Fatalf("TvDiziler ResolveStream failed: %v", err)
	}
	if res == nil || res.Primary.URL == "" {
		t.Fatalf("TvDiziler ResolveStream returned empty stream")
	}
	t.Logf("✅ TvDiziler Stream RESOLVED: Title='%s' Quality=%s URL=%s Format=%s", res.Primary.Title, res.Primary.Quality, res.Primary.URL, res.Primary.Format)
}



