package providers_test

import (
	"context"
	"testing"
	"time"

	"anthology-dav/providers"
)

func TestYabanciDiziStreamResolution(t *testing.T) {
	p, ok := providers.GetByID("yabancidizi")
	if !ok || p == nil {
		t.Fatal("provider yabancidizi not found")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 25*time.Second)
	defer cancel()

	res, err := p.ResolveStream(ctx, "the-mentalist-izle-1:1:1")
	if err != nil {
		t.Fatalf("ResolveStream error: %v", err)
	}
	if res == nil || res.Primary.URL == "" {
		t.Fatal("expected stream result, got nil or empty URL")
	}
	t.Logf("SUCCESS! Stream found: Name=%s, Quality=%s, URL=%s", res.Primary.Name, res.Primary.Quality, res.Primary.URL)
}

