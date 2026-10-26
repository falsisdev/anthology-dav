package providers

import (
	"testing"
)

func TestDetectAudioType(t *testing.T) {
	tests := []struct {
		input    string
		expected AudioType
	}{
		{"Inception (2010) Türkçe Dublaj 1080p", AudioTypeDub},
		{"TR Dublaj FastPlay", AudioTypeDub},
		{"Breaking Bad - S01E01 [TR Dub]", AudioTypeDub},
		{"Interstellar Türkçe Altyazılı 1080p", AudioTypeSub},
		{"Attack on Titan - S01E01 Japonca [TR Altyazı]", AudioTypeOriginal},
		{"Naruto Shippuden S01E01 English Dubbed", AudioTypeEnDub},
		{"Anizium • İngilizce Dublaj • 1080p", AudioTypeEnDub},
		{"Standard Video Stream", AudioTypeUnknown},
	}

	for _, tt := range tests {
		got := DetectAudioType(tt.input)
		if got != tt.expected {
			t.Errorf("DetectAudioType(%q) = %v; want %v", tt.input, got, tt.expected)
		}
	}

	// Verify AudioTypeLabel does not specify Japanese for original audio
	if got := AudioTypeLabel(AudioTypeOriginal); got != "🎬 Orijinal" {
		t.Errorf("AudioTypeLabel(AudioTypeOriginal) = %q; want %q", got, "🎬 Orijinal")
	}
}

func TestGetQualityScoreUserAudioPriority(t *testing.T) {
	// User rule verified in /grill-me:
	// Original/Sub (+25) > Dubbed (+15) > English Dubbed (+5) at the same resolution.
	sSub := StreamSource{
		Quality:   "1080p",
		AudioType: AudioTypeSub,
		Title:     "Film • [TR Altyazı] • 1080p",
	}
	sOriginal := StreamSource{
		Quality:   "1080p",
		AudioType: AudioTypeOriginal,
		Title:     "Anime • Japonca [TR Altyazı] • 1080p",
	}
	sDub := StreamSource{
		Quality:   "1080p",
		AudioType: AudioTypeDub,
		Title:     "Film • [TR Dublaj] • 1080p",
	}
	sEnDub := StreamSource{
		Quality:   "1080p",
		AudioType: AudioTypeEnDub,
		Title:     "Anime • İngilizce Dublaj • 1080p",
	}

	scoreSub := GetQualityScore(sSub)
	scoreOrig := GetQualityScore(sOriginal)
	scoreDub := GetQualityScore(sDub)
	scoreEnDub := GetQualityScore(sEnDub)

	if scoreSub != scoreOrig {
		t.Errorf("Expected sub and original to have same priority bonus, got %d vs %d", scoreSub, scoreOrig)
	}
	if scoreSub <= scoreDub {
		t.Errorf("Expected Sub (%d) > Dub (%d) according to user preference", scoreSub, scoreDub)
	}
	if scoreDub <= scoreEnDub {
		t.Errorf("Expected Dub (%d) > EnDub (%d)", scoreDub, scoreEnDub)
	}
}

func TestExtractSubtitles(t *testing.T) {
	rawHTML := `
		<!DOCTYPE html>
		<html>
		<head>
			<script>
				window.playerConfig = {
					file: "https://cdn.example.com/stream.m3u8",
					tracks: [
						{ file: "https://sub.example.com/turkish.vtt", kind: "captions", label: "Türkçe" },
						{ file: "https://sub.example.com/english.srt", kind: "captions", label: "English" },
						{ file: "/subs/relative_tr.vtt", label: "TR" }
					]
				};
			</script>
		</head>
		<body>
			<video>
				<track src="//cdn.example.com/proto_rel.vtt" label="Türkçe Altyazı" kind="subtitles">
				<track src="/subs/escaped\/url\/sub.vtt" label="Fransızca" kind="subtitles">
			</video>
		</body>
		</html>
	`

	subs := ExtractSubtitles(rawHTML, "https://example.com")
	if len(subs) < 4 {
		t.Fatalf("Expected at least 4 extracted subtitles, got %d", len(subs))
	}

	hasTurkish := false
	hasEnglish := false
	hasSRT := false
	for _, s := range subs {
		t.Logf("Found Subtitle: Lang=%s Label='%s' Format=%s URL=%s", s.Language, s.Label, s.Format, s.URL)
		if s.Language == "tr" {
			hasTurkish = true
		}
		if s.Language == "en" {
			hasEnglish = true
		}
		if s.Format == "srt" {
			hasSRT = true
		}
	}

	if !hasTurkish {
		t.Errorf("Expected Turkish subtitle to be extracted")
	}
	if !hasEnglish {
		t.Errorf("Expected English subtitle to be extracted")
	}
	if !hasSRT {
		t.Errorf("Expected SRT format to be recognized")
	}
}
