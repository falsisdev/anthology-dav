package stream

import (
	"bytes"
	"context"
	"net/http/httptest"
	"testing"
	"time"

	"anthology-dav/config"
	"anthology-dav/providers"
	"anthology-dav/vfs"
)

func TestVTTToSRTConversion(t *testing.T) {
	vttInput := []byte("WEBVTT\n\n00:00:06.000 --> 00:00:09.000 line:20%\nBu çeviri Anizium için hazırlanmıştır.\n\n00:00:15.811 --> 00:00:19.611\nÜCRA BİR KÖY, HAGE\n")
	srtOutput := providers.VTTToSRT(vttInput)
	srtStr := string(srtOutput)
	t.Logf("SRT output:\n%s", srtStr)

	if !bytes.Contains(srtOutput, []byte("00:00:06,000 --> 00:00:09,000")) {
		t.Errorf("Expected SRT comma timestamps, got:\n%s", srtStr)
	}
	if !bytes.Contains(srtOutput, []byte("Bu çeviri Anizium için hazırlanmıştır.")) {
		t.Errorf("Expected subtitle text preserved")
	}
	if !bytes.Contains(srtOutput, []byte("1\n")) || !bytes.Contains(srtOutput, []byte("2\n")) {
		t.Errorf("Expected sequential numbering")
	}
}

func TestResolveSubtitleEndpoint(t *testing.T) {
	cfg := config.Get()
	cache := vfs.NewMemoryCache()
	proxy := NewProxyHandler(cfg, cache)
	mr := NewMultiResolver(proxy)

	ctx, cancel := context.WithTimeout(context.Background(), 25*time.Second)
	defer cancel()

	// 1. Resolve VTT subtitle for Black Clover S01E01 from Anizium
	vttData, err := mr.ResolveSubtitle(ctx, "anime", 0, "Black Clover", "", "2017", 1, 1, "anizium", "tr", "vtt")
	if err != nil {
		t.Fatalf("ResolveSubtitle VTT failed: %v", err)
	}
	t.Logf("Resolved VTT length: %d bytes", len(vttData))
	if !bytes.Contains(vttData, []byte("WEBVTT")) {
		t.Errorf("Expected WEBVTT header in resolved VTT, got: %s", string(vttData[:min(len(vttData), 100)]))
	}

	// 2. Resolve SRT subtitle for Black Clover S01E01 from Anizium
	srtData, err := mr.ResolveSubtitle(ctx, "anime", 0, "Black Clover", "", "2017", 1, 1, "anizium", "tr", "srt")
	if err != nil {
		t.Fatalf("ResolveSubtitle SRT failed: %v", err)
	}
	t.Logf("Resolved SRT length: %d bytes", len(srtData))
	if !bytes.Contains(srtData, []byte("-->")) || bytes.Contains(srtData, []byte("WEBVTT")) {
		t.Errorf("Expected valid SRT without WEBVTT header")
	}

	// 3. Test HandleSubResolve HTTP handler
	req := httptest.NewRequest("GET", "/sub/resolve?type=anime&title=Black+Clover&year=2017&season=1&episode=1&provider=anizium&lang=tr&format=vtt", nil)
	w := httptest.NewRecorder()
	mr.HandleSubResolve(w, req)
	resp := w.Result()
	if resp.StatusCode != 200 {
		t.Fatalf("HandleSubResolve returned status %d", resp.StatusCode)
	}
	if ct := resp.Header.Get("Content-Type"); ct != "text/vtt; charset=utf-8" {
		t.Errorf("Expected text/vtt Content-Type, got %s", ct)
	}
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
