package stream

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"anthology-dav/config"
	"anthology-dav/providers"
	"anthology-dav/vfs"
)

func TestBuildDynamicMasterPlaylist(t *testing.T) {
	cfg := config.Get()
	cache := vfs.NewMemoryCache()
	proxy := NewProxyHandler(cfg, cache)

	req := httptest.NewRequest("GET", "http://127.0.0.1:8085/stream/resolve?type=movie&title=Inception", nil)

	hlsCandidates := []providers.StreamSource{
		{
			URL:       "https://cdn.example.com/master_1080p.m3u8",
			Quality:   "1080p",
			Format:    "hls",
			IsHLS:     true,
			AudioType: providers.AudioTypeSub,
		},
		{
			URL:       "https://cdn.example.com/master_720p.m3u8",
			Quality:   "720p",
			Format:    "hls",
			IsHLS:     true,
			AudioType: providers.AudioTypeSub,
		},
	}

	subs := []providers.Subtitle{
		{
			URL:      "https://sub.example.com/tr.vtt",
			Language: "tr",
			Label:    "Türkçe",
			Format:   "vtt",
		},
	}

	manifest := proxy.BuildDynamicMasterPlaylist(req, hlsCandidates, subs)
	t.Logf("Generated Master Manifest:\n%s", manifest)

	if !strings.HasPrefix(manifest, "#EXTM3U") {
		t.Fatalf("Expected manifest to start with #EXTM3U, got: %s", manifest)
	}
	if !strings.Contains(manifest, "#EXT-X-STREAM-INF:BANDWIDTH=4500000,RESOLUTION=1920x1080,NAME=\"1080p\"") {
		t.Errorf("Expected 1080p stream-inf in manifest")
	}
	if !strings.Contains(manifest, "#EXT-X-STREAM-INF:BANDWIDTH=2400000,RESOLUTION=1280x720,NAME=\"720p\"") {
		t.Errorf("Expected 720p stream-inf in manifest")
	}
	if !strings.Contains(manifest, "#EXT-X-MEDIA:TYPE=SUBTITLES") || !strings.Contains(manifest, "anthology_subs") {
		t.Errorf("Expected subtitle media tag in master manifest")
	}
}

func TestFilterAndSortStreamsByAudioAndQuality(t *testing.T) {
	source1080Sub := providers.StreamSource{
		URL:       "https://example.com/sub_1080p.m3u8",
		Quality:   "1080p",
		AudioType: providers.AudioTypeSub,
		Title:     "Film • [TR Altyazı] • 1080p",
	}
	source720Sub := providers.StreamSource{
		URL:       "https://example.com/sub_720p.m3u8",
		Quality:   "720p",
		AudioType: providers.AudioTypeSub,
		Title:     "Film • [TR Altyazı] • 720p",
	}
	source1080Dub := providers.StreamSource{
		URL:       "https://example.com/dub_1080p.m3u8",
		Quality:   "1080p",
		AudioType: providers.AudioTypeDub,
		Title:     "Film • [TR Dublaj] • 1080p",
	}

	allSources := []providers.StreamSource{source1080Dub, source720Sub, source1080Sub}

	// 1. Specific quality "720p" with "sub"
	res720 := FilterAndSortStreamsByAudioAndQuality(allSources, "sub", "720p")
	if res720 == nil || res720.Primary.Quality != "720p" {
		t.Fatalf("Expected 720p stream when quality=720p requested, got: %+v", res720)
	}

	// 2. "auto" quality with "sub" (should prioritize highest: 1080p)
	resAuto := FilterAndSortStreamsByAudioAndQuality(allSources, "sub", "auto")
	if resAuto == nil || resAuto.Primary.Quality != "1080p" {
		t.Fatalf("Expected 1080p stream on auto, got: %+v", resAuto)
	}

	// 3. Fallback when requested quality doesn't exist (e.g. 4k)
	resFallback := FilterAndSortStreamsByAudioAndQuality(allSources, "sub", "4k")
	if resFallback == nil || resFallback.Primary.URL == "" {
		t.Fatalf("Expected fallback stream when requested 4k is missing, got: %+v", resFallback)
	}
}

func TestServeStreamResultDynamicMasterPlaylist(t *testing.T) {
	cfg := config.Get()
	cache := vfs.NewMemoryCache()
	proxy := NewProxyHandler(cfg, cache)

	streamRes := &providers.StreamResult{
		Primary: providers.StreamSource{
			URL:       "https://cdn.example.com/1080p.m3u8",
			Quality:   "1080p",
			IsHLS:     true,
			AudioType: providers.AudioTypeSub,
		},
		Alternates: []providers.StreamSource{
			{
				URL:       "https://cdn.example.com/720p.m3u8",
				Quality:   "720p",
				IsHLS:     true,
				AudioType: providers.AudioTypeSub,
			},
		},
	}

	// Request with quality=auto (default)
	req := httptest.NewRequest("GET", "http://127.0.0.1:8085/stream/resolve?type=movie&title=Inception&audio=sub&quality=auto", nil)
	rr := httptest.NewRecorder()

	err := proxy.ServeStreamResult(rr, req, streamRes)
	if err != nil {
		t.Fatalf("ServeStreamResult error: %v", err)
	}

	if rr.Code != http.StatusOK {
		t.Fatalf("Expected HTTP 200, got: %d", rr.Code)
	}
	if !strings.Contains(rr.Header().Get("Content-Type"), "mpegurl") {
		t.Fatalf("Expected mpegurl content-type, got: %s", rr.Header().Get("Content-Type"))
	}
	body := rr.Body.String()
	if !strings.Contains(body, "#EXT-X-STREAM-INF") || !strings.Contains(body, "1080p") || !strings.Contains(body, "720p") {
		t.Fatalf("Expected dynamic master playlist in response body, got:\n%s", body)
	}
}
