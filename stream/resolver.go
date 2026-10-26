package stream

import (
	"bytes"
	"context"
	"fmt"
	"net/http"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"anthology-dav/config"
	"anthology-dav/providers"
)

// DiscoveredSource represents a verified working stream provider for a specific movie or episode.
type DiscoveredSource struct {
	ProviderID         string   `json:"provider_id"`
	ProviderName       string   `json:"provider_name"`
	Quality            string   `json:"quality"`
	Format             string   `json:"format"`
	Title              string   `json:"title"`
	AudioType          string   `json:"audio_type,omitempty"`
	AudioLabel         string   `json:"audio_label,omitempty"`
	HasSubtitles       bool     `json:"has_subtitles,omitempty"`
	AvailableQualities []string `json:"available_qualities,omitempty"`
}

// MultiResolver manages dynamic multi-provider scraping and stream resolution.
type MultiResolver struct {
	proxy *ProxyHandler
}

// NewMultiResolver creates a new MultiResolver instance.
func NewMultiResolver(proxy *ProxyHandler) *MultiResolver {
	return &MultiResolver{proxy: proxy}
}

// CandidateProvidersForType returns the ordered list of candidate provider IDs for a given media type.
// High-priority providers for the specific category come first to guarantee fast, native Turkish sub/dub sources.
func CandidateProvidersForType(mediaType string) []string {
	switch strings.ToLower(strings.TrimSpace(mediaType)) {
	case "anime":
		return []string{
			"animecix", "anizium", "sonanime", "animom",
			"seicode", "anizm", "asyaanimeleri", "animeprax", "vidlink",
		}
	case "series", "tv":
		return []string{
			"diziwatch", "diziboxizle", "diziyou", "dizimom",
			"dizipod", "dizibak", "yabancidizi", "vidlink", "vidmody",
			"ddizi", "puhutv", "trdiziizle", "tvdiziler", "anthology_diziler",
		}
	default: // "movie"
		return []string{
			"filmmodu", "hdfilmcehennemi", "hdfilmizle", "hdfilmdelisi",
			"vidlink", "vidmody", "webteizle", "setfilmizle",
			"kultfilmler", "liderfilmizle", "sinemacx", "sinewix",
		}
	}
}

// HandleResolve handles /stream/resolve?type={movie|series|anime}&tmdb_id={id}&title={title}&original_title={orig}&year={year}&season={s}&episode={e}&provider={prov}
func (mr *MultiResolver) HandleResolve(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	mediaType := strings.ToLower(strings.TrimSpace(q.Get("type")))
	tmdbID, _ := strconv.Atoi(strings.TrimSpace(q.Get("tmdb_id")))
	title := strings.TrimSpace(q.Get("title"))
	origTitle := strings.TrimSpace(q.Get("original_title"))
	year := strings.TrimSpace(q.Get("year"))
	// TrimSpace is critical: players forward the .strm trailing newline as "%0A" (e.g. "9\n"),
	// which made Atoi fail and silently fall back to episode 1.
	season, _ := strconv.Atoi(strings.TrimSpace(q.Get("season")))
	episode, _ := strconv.Atoi(strings.TrimSpace(q.Get("episode")))
	preferredProv := strings.TrimSpace(q.Get("provider"))
	audioPref := strings.ToLower(strings.TrimSpace(q.Get("audio")))
	if audioPref == "" {
		audioPref = "auto"
	}
	qualityPref := strings.ToLower(strings.TrimSpace(q.Get("quality")))
	if qualityPref == "" {
		qualityPref = "auto"
	}

	if season <= 0 {
		season = 1
	}
	if episode <= 0 {
		episode = 1
	}

	var altTitles []string
	var workingProvs []string

	if item, ok := config.Library().GetByTMDB(mediaType, tmdbID); ok {
		if (preferredProv == "" || preferredProv == "auto") && item.SelectedProvider != "" && item.SelectedProvider != "auto" {
			preferredProv = item.SelectedProvider
		}
		if title == "" {
			title = item.Title
		}
		if origTitle == "" {
			origTitle = item.OriginalTitle
		}
		if year == "" {
			year = item.Year
		}
		if len(item.AltTitles) == 0 && tmdbID > 0 {
			providers.PrewarmLibraryItemMetadata(r.Context(), &item)
		}
		altTitles = item.AltTitles
		if item.EnglishTitle != "" {
			altTitles = append([]string{item.EnglishTitle}, altTitles...)
		}
		workingProvs = item.WorkingProviders
	}

	if title == "" && tmdbID <= 0 {
		http.Error(w, "missing title or tmdb_id parameter", http.StatusBadRequest)
		return
	}

	// 1. Check Cache
	cacheKey := fmt.Sprintf("resolve:%s:%d:%s:%d:%d:%s:%s:%s", mediaType, tmdbID, title, season, episode, preferredProv, audioPref, qualityPref)
	if val, ok := mr.proxy.cache.Get(cacheKey); ok {
		if res, ok := val.(*providers.StreamResult); ok && res != nil && res.Primary.URL != "" {
			if err := mr.proxy.ServeStreamResult(w, r, res); err == nil {
				return
			}
			mr.proxy.cache.Delete(cacheKey)
		}
	}

	// NOTE: HEAD is resolved fully (no fast-path). Infuse sends HEAD before playback and aborts
	// if it receives an empty 200 without a real Content-Length.

	// 2. Determine Candidate Providers
	var candidates []string
	if preferredProv != "" && preferredProv != "auto" {
		candidates = []string{preferredProv}
	} else {
		baseCandidates := CandidateProvidersForType(mediaType)
		if len(workingProvs) > 0 {
			var regularWorking []string
			var fallbackWorking []string
			for _, wp := range workingProvs {
				if wp == "vidlink" || wp == "vidmody" {
					fallbackWorking = append(fallbackWorking, wp)
				} else {
					regularWorking = append(regularWorking, wp)
				}
			}
			seen := make(map[string]bool)
			for _, wp := range regularWorking {
				seen[wp] = true
				candidates = append(candidates, wp)
			}
			for _, bc := range baseCandidates {
				if !seen[bc] && bc != "vidlink" && bc != "vidmody" {
					seen[bc] = true
					candidates = append(candidates, bc)
				}
			}
			for _, wp := range fallbackWorking {
				if !seen[wp] {
					seen[wp] = true
					candidates = append(candidates, wp)
				}
			}
		} else {
			candidates = baseCandidates
		}
	}

	// 3. Concurrent Multi-Provider Scraping
	ctx, cancel := context.WithTimeout(r.Context(), 18*time.Second)
	defer cancel()

	type resolveResult struct {
		providerID string
		result     *providers.StreamResult
	}

	resultCh := make(chan resolveResult, len(candidates))
	var wg sync.WaitGroup

	for _, provID := range candidates {
		prov, ok := providers.GetByID(provID)
		if !ok {
			continue
		}

		wg.Add(1)
		go func(p providers.Provider) {
			defer wg.Done()
			pCtx, pCancel := context.WithTimeout(ctx, 9*time.Second)
			defer pCancel()

			res, err := mr.resolveFromProvider(pCtx, p, mediaType, tmdbID, title, origTitle, year, altTitles, season, episode)
			if err == nil && res != nil && res.Primary.URL != "" {
				// Cache individual provider result as well
				provKey := fmt.Sprintf("resolve:%s:%d:%s:%d:%d:%s", mediaType, tmdbID, title, season, episode, p.ID())
				mr.proxy.cache.Set(provKey, res, 45*time.Minute)
				select {
				case resultCh <- resolveResult{providerID: p.ID(), result: res}:
				default:
				}
			}
		}(prov)
	}

	go func() {
		wg.Wait()
		close(resultCh)
	}()

	var foundResults []resolveResult
	for res := range resultCh {
		foundResults = append(foundResults, res)
		// Only fast-break if we got a high-definition stream from a reliable native provider.
		// Cloud resolvers (vidlink/vidmody) return in ~100ms but often hit rate limits (429/428),
		// so do not abandon other concurrent providers on vidlink hits.
		if res.providerID != "vidlink" && res.providerID != "vidmody" {
			q := strings.ToLower(res.result.Primary.Quality)
			if strings.Contains(q, "1080") || strings.Contains(q, "4k") || strings.Contains(q, "2160") || strings.Contains(q, "1440") {
				break
			}
		}
	}

	if len(foundResults) == 0 {
		http.Error(w, fmt.Sprintf("stream resolver: no verified stream found for '%s' S%02dE%02d across %d providers", title, season, episode, len(candidates)), http.StatusNotFound)
		return
	}

	var allSources []providers.StreamSource
	var fallbackSources []providers.StreamSource
	for _, fr := range foundResults {
		if fr.result.Primary.URL != "" {
			if fr.providerID == "vidlink" || fr.providerID == "vidmody" {
				fallbackSources = append(fallbackSources, fr.result.Primary)
				fallbackSources = append(fallbackSources, fr.result.Alternates...)
			} else {
				allSources = append(allSources, fr.result.Primary)
				allSources = append(allSources, fr.result.Alternates...)
			}
		}
	}
	// Native providers strictly match title + season + episode; cloud resolvers (VidLink/Vidmody)
	// are only used when no native provider returned a stream (they often mis-map episodes).
	if len(allSources) == 0 {
		allSources = fallbackSources
	}
	sort.SliceStable(foundResults, func(i, j int) bool {
		return !isCloudResolver(foundResults[i].providerID) && isCloudResolver(foundResults[j].providerID)
	})

	bestResult := FilterAndSortStreamsByAudioAndQuality(allSources, audioPref, qualityPref)
	if bestResult == nil || bestResult.Primary.URL == "" {
		bestResult, _ = providers.SelectBestStream(allSources)
	}
	if bestResult == nil || bestResult.Primary.URL == "" {
		bestResult = foundResults[0].result
	}

	// Deliver stream to client: try bestResult first, then fall back to each individual provider result
	if err := mr.proxy.ServeStreamResult(w, r, bestResult); err == nil {
		mr.proxy.cache.Set(cacheKey, bestResult, 45*time.Minute)
		return
	}

	for _, fr := range foundResults {
		if fr.result == bestResult {
			continue
		}
		if err := mr.proxy.ServeStreamResult(w, r, fr.result); err == nil {
			mr.proxy.cache.Set(cacheKey, fr.result, 45*time.Minute)
			return
		}
	}

	mr.proxy.cache.Delete(cacheKey)
	http.Error(w, fmt.Sprintf("stream resolver: playback failed for '%s' S%02dE%02d across %d candidates", title, season, episode, len(allSources)), http.StatusBadGateway)
}

// ResolveSubtitle dynamically finds, fetches and formats (VTT or SRT) subtitles for a movie or episode.
func (mr *MultiResolver) ResolveSubtitle(ctx context.Context, mediaType string, tmdbID int, title, origTitle, year string, season, episode int, preferredProv, lang, format string) ([]byte, error) {
	if season <= 0 {
		season = 1
	}
	if episode <= 0 {
		episode = 1
	}
	reqLang := strings.ToLower(strings.TrimSpace(lang))
	if reqLang == "" {
		reqLang = "tr"
	}
	reqFormat := strings.ToLower(strings.TrimSpace(format))
	if reqFormat == "" {
		reqFormat = "vtt"
	}

	// 1. Check Subtitle Cache
	cacheKey := fmt.Sprintf("sub:data:%s:%d:%s:%d:%d:%s:%s:%s", mediaType, tmdbID, title, season, episode, preferredProv, reqLang, reqFormat)
	if val, ok := mr.proxy.cache.Get(cacheKey); ok {
		if b, ok := val.([]byte); ok && len(b) > 0 {
			return b, nil
		}
	}

	// 2. Look for already resolved stream result in cache
	var subURL string
	var subHeaders map[string]string

	findMatchingSub := func(subs []providers.Subtitle) bool {
		if len(subs) == 0 {
			return false
		}
		for _, s := range subs {
			if strings.EqualFold(s.Language, reqLang) && strings.TrimSpace(s.URL) != "" {
				subURL = s.URL
				subHeaders = s.Headers
				return true
			}
		}
		if reqLang == "tr" {
			for _, s := range subs {
				l := strings.ToLower(s.Label)
				if (strings.Contains(l, "türk") || strings.Contains(l, "turk")) && strings.TrimSpace(s.URL) != "" {
					subURL = s.URL
					subHeaders = s.Headers
					return true
				}
			}
		}
		for _, s := range subs {
			if strings.TrimSpace(s.URL) != "" {
				subURL = s.URL
				subHeaders = s.Headers
				return true
			}
		}
		return false
	}

	provKey := fmt.Sprintf("resolve:%s:%d:%s:%d:%d:%s", mediaType, tmdbID, title, season, episode, preferredProv)
	if val, ok := mr.proxy.cache.Get(provKey); ok {
		if res, ok := val.(*providers.StreamResult); ok && res != nil {
			findMatchingSub(res.Subtitles)
		}
	}

	if subURL == "" {
		autoKey := fmt.Sprintf("resolve:%s:%d:%s:%d:%d:auto", mediaType, tmdbID, title, season, episode)
		if val, ok := mr.proxy.cache.Get(autoKey); ok {
			if res, ok := val.(*providers.StreamResult); ok && res != nil {
				findMatchingSub(res.Subtitles)
			}
		}
	}

	// 3. If not yet in cache, run provider resolution
	if subURL == "" {
		var altTitles []string
		if item, ok := config.Library().GetByTMDB(mediaType, tmdbID); ok {
			altTitles = item.AltTitles
			if item.EnglishTitle != "" {
				altTitles = append([]string{item.EnglishTitle}, altTitles...)
			}
			if title == "" {
				title = item.Title
			}
			if origTitle == "" {
				origTitle = item.OriginalTitle
			}
			if year == "" {
				year = item.Year
			}
		}

		candidates := CandidateProvidersForType(mediaType)
		if preferredProv != "" && preferredProv != "auto" {
			candidates = append([]string{preferredProv}, candidates...)
		}

		for _, provID := range candidates {
			p, ok := providers.GetByID(provID)
			if !ok {
				continue
			}
			pCtx, pCancel := context.WithTimeout(ctx, 6*time.Second)
			res, err := mr.resolveFromProvider(pCtx, p, mediaType, tmdbID, title, origTitle, year, altTitles, season, episode)
			pCancel()
			if err == nil && res != nil && len(res.Subtitles) > 0 {
				if findMatchingSub(res.Subtitles) {
					break
				}
			}
		}
	}

	if subURL == "" {
		if reqFormat == "srt" {
			return []byte("\n"), nil
		}
		return []byte("WEBVTT\n\n"), nil
	}

	// 4. Fetch the subtitle content
	data, _, _, _, err := providers.FetchBytes(ctx, "GET", subURL, subHeaders, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch subtitle from %s: %w", subURL, err)
	}

	// 5. Convert format if SRT requested and content is WebVTT
	if reqFormat == "srt" && bytes.Contains(data, []byte("WEBVTT")) {
		data = providers.VTTToSRT(data)
	} else if reqFormat == "vtt" && !bytes.Contains(data, []byte("WEBVTT")) {
		data = append([]byte("WEBVTT\n\n"), data...)
	}

	mr.proxy.cache.Set(cacheKey, data, 1*time.Hour)
	return data, nil
}

// HandleSubResolve serves /sub/resolve?type={movie|series|anime}&tmdb_id={id}&title={title}&season={s}&episode={e}&lang={tr|en}&format={vtt|srt}&provider={prov}
func (mr *MultiResolver) HandleSubResolve(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	mediaType := strings.ToLower(strings.TrimSpace(q.Get("type")))
	tmdbID, _ := strconv.Atoi(strings.TrimSpace(q.Get("tmdb_id")))
	title := strings.TrimSpace(q.Get("title"))
	origTitle := strings.TrimSpace(q.Get("original_title"))
	year := strings.TrimSpace(q.Get("year"))
	season, _ := strconv.Atoi(strings.TrimSpace(q.Get("season")))
	episode, _ := strconv.Atoi(strings.TrimSpace(q.Get("episode")))
	preferredProv := strings.TrimSpace(q.Get("provider"))
	lang := strings.TrimSpace(q.Get("lang"))
	format := strings.TrimSpace(q.Get("format"))

	ctx, cancel := context.WithTimeout(r.Context(), 15*time.Second)
	defer cancel()

	data, err := mr.ResolveSubtitle(ctx, mediaType, tmdbID, title, origTitle, year, season, episode, preferredProv, lang, format)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadGateway)
		return
	}

	w.Header().Set("Access-Control-Allow-Origin", "*")
	if strings.EqualFold(format, "srt") {
		w.Header().Set("Content-Type", "application/x-subrip; charset=utf-8")
	} else {
		w.Header().Set("Content-Type", "text/vtt; charset=utf-8")
	}
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(data)
}

// DiscoverSources concurrently checks all candidate providers for a movie or episode,
// caches each working stream, updates WorkingProviders in library.json, and returns only verified working sources.
func (mr *MultiResolver) DiscoverSources(ctx context.Context, mediaType string, tmdbID int, title, origTitle, year string, season, episode int) []DiscoveredSource {
	if season <= 0 {
		season = 1
	}
	if episode <= 0 {
		episode = 1
	}

	var altTitles []string
	if item, ok := config.Library().GetByTMDB(mediaType, tmdbID); ok {
		if title == "" {
			title = item.Title
		}
		if origTitle == "" {
			origTitle = item.OriginalTitle
		}
		if year == "" {
			year = item.Year
		}
		if len(item.AltTitles) == 0 && tmdbID > 0 {
			providers.PrewarmLibraryItemMetadata(ctx, &item)
		}
		altTitles = item.AltTitles
		if item.EnglishTitle != "" {
			altTitles = append([]string{item.EnglishTitle}, altTitles...)
		}
	} else if tmdbID > 0 && (mediaType == "series" || mediaType == "anime") {
		if det, err := providers.GetTMDBShowDetails(ctx, tmdbID); err == nil && det != nil {
			altTitles = det.AltTitles
			if det.EnglishName != "" {
				altTitles = append([]string{det.EnglishName}, altTitles...)
			}
		}
	}

	discoverCacheKey := fmt.Sprintf("discover:%s:%d:%s:%d:%d", mediaType, tmdbID, title, season, episode)
	if val, ok := mr.proxy.cache.Get(discoverCacheKey); ok {
		if cachedList, ok := val.([]DiscoveredSource); ok && len(cachedList) > 0 {
			return cachedList
		}
	}

	candidates := CandidateProvidersForType(mediaType)
	dCtx, cancel := context.WithTimeout(ctx, 12*time.Second)
	defer cancel()

	var (
		mu         sync.Mutex
		discovered []DiscoveredSource
		allSources []providers.StreamSource
		wg         sync.WaitGroup
	)

	for _, provID := range candidates {
		prov, ok := providers.GetByID(provID)
		if !ok {
			continue
		}

		wg.Add(1)
		go func(p providers.Provider) {
			defer wg.Done()
			pCtx, pCancel := context.WithTimeout(dCtx, 9*time.Second)
			defer pCancel()

			// Check if already in cache
			provKey := fmt.Sprintf("resolve:%s:%d:%s:%d:%d:%s", mediaType, tmdbID, title, season, episode, p.ID())
			var res *providers.StreamResult
			if val, ok := mr.proxy.cache.Get(provKey); ok {
				res, _ = val.(*providers.StreamResult)
			}
			if res == nil || res.Primary.URL == "" {
				var err error
				res, err = mr.resolveFromProvider(pCtx, p, mediaType, tmdbID, title, origTitle, year, altTitles, season, episode)
				if err != nil || res == nil || res.Primary.URL == "" {
					return
				}
				mr.proxy.cache.Set(provKey, res, 45*time.Minute)
			}

			providerStreams := append([]providers.StreamSource{res.Primary}, res.Alternates...)

			// Group by AudioType (select best quality stream for each audio type)
			type groupInfo struct {
				best      providers.StreamSource
				qualities []string
			}
			grouped := make(map[providers.AudioType]*groupInfo)
			seenQualPerAudio := make(map[providers.AudioType]map[string]bool)

			for _, st := range providerStreams {
				at := st.AudioType
				if at == providers.AudioTypeUnknown {
					at = providers.DetectAudioType(st.Title + " " + st.Name)
				}
				if at == providers.AudioTypeUnknown && len(st.Subtitles) > 0 {
					at = providers.AudioTypeSub
				}

				if seenQualPerAudio[at] == nil {
					seenQualPerAudio[at] = make(map[string]bool)
				}
				normQual := providers.NormalizeQuality(st.Quality)
				if normQual == "" {
					normQual = "HD"
				}

				gi, exists := grouped[at]
				if !exists {
					gi = &groupInfo{best: st}
					grouped[at] = gi
				} else if providers.GetQualityScore(st) > providers.GetQualityScore(gi.best) {
					gi.best = st
				}

				if !seenQualPerAudio[at][normQual] {
					seenQualPerAudio[at][normQual] = true
					gi.qualities = append(gi.qualities, normQual)
				}
			}

			mu.Lock()
			for at, gi := range grouped {
				st := gi.best
				qual := strings.TrimSpace(st.Quality)
				if qual == "" {
					qual = "HD"
				}
				format := strings.ToUpper(strings.TrimSpace(st.Format))
				if format == "" {
					if st.IsHLS {
						format = "HLS"
					} else {
						format = "MP4"
					}
				}
				audioCode := string(at)
				if audioCode == "" {
					audioCode = "auto"
				}
				audioLabel := providers.AudioTypeLabel(at)
				hasSubs := len(st.Subtitles) > 0

				discovered = append(discovered, DiscoveredSource{
					ProviderID:         p.ID(),
					ProviderName:       p.Name(),
					Quality:            qual,
					Format:             format,
					Title:              st.Title,
					AudioType:          audioCode,
					AudioLabel:         audioLabel,
					HasSubtitles:       hasSubs,
					AvailableQualities: gi.qualities,
				})
			}
			allSources = append(allSources, providerStreams...)
			mu.Unlock()
		}(prov)
	}

	wg.Wait()

	// Sort discovered sources: native (strict title+episode matched) providers first, then audio preference, then quality.
	audioWeight := func(at string) int {
		switch at {
		case "sub", "original":
			return 3
		case "dub":
			return 2
		case "en_dub":
			return 1
		default:
			return 0
		}
	}

	sort.SliceStable(discovered, func(i, j int) bool {
		ci, cj := isCloudResolver(discovered[i].ProviderID), isCloudResolver(discovered[j].ProviderID)
		if ci != cj {
			return !ci
		}
		ai, aj := audioWeight(discovered[i].AudioType), audioWeight(discovered[j].AudioType)
		if ai != aj {
			return ai > aj
		}
		return qualityWeight(discovered[i].Quality) > qualityWeight(discovered[j].Quality)
	})

	if len(allSources) > 0 && len(discovered) > 0 {
		bestSources := allSources
		if !isCloudResolver(discovered[0].ProviderID) {
			if v, ok := mr.proxy.cache.Get(fmt.Sprintf("resolve:%s:%d:%s:%d:%d:%s", mediaType, tmdbID, title, season, episode, discovered[0].ProviderID)); ok {
				if r, ok := v.(*providers.StreamResult); ok && r != nil {
					bestSources = append([]providers.StreamSource{r.Primary}, r.Alternates...)
				}
			}
		}
		if bestResult, err := providers.SelectBestStream(bestSources); err == nil && bestResult != nil && bestResult.Primary.URL != "" {
			autoKey1 := fmt.Sprintf("resolve:%s:%d:%s:%d:%d:auto:auto", mediaType, tmdbID, title, season, episode)
			autoKey2 := fmt.Sprintf("resolve:%s:%d:%s:%d:%d:auto", mediaType, tmdbID, title, season, episode)
			autoKey3 := fmt.Sprintf("resolve:%s:%d:%s:%d:%d:", mediaType, tmdbID, title, season, episode)
			mr.proxy.cache.Set(autoKey1, bestResult, 45*time.Minute)
			mr.proxy.cache.Set(autoKey2, bestResult, 45*time.Minute)
			mr.proxy.cache.Set(autoKey3, bestResult, 45*time.Minute)
		}
	}

	if len(discovered) > 0 {
		mr.proxy.cache.Set(discoverCacheKey, discovered, 30*time.Minute)
		if tmdbID > 0 {
			var workingIDs []string
			seenID := make(map[string]bool)
			for _, d := range discovered {
				if !seenID[d.ProviderID] {
					seenID[d.ProviderID] = true
					workingIDs = append(workingIDs, d.ProviderID)
				}
			}
			if item, ok := config.Library().GetByTMDB(mediaType, tmdbID); ok {
				_ = config.Library().UpdateWorkingProviders(item.ID, workingIDs)
			}
		}
	}

	return discovered
}

// FilterAndSortStreamsByAudioAndQuality selects the best stream based on audio preference and requested resolution.
func FilterAndSortStreamsByAudioAndQuality(sources []providers.StreamSource, audioPref, qualityPref string) *providers.StreamResult {
	if len(sources) == 0 {
		return nil
	}
	audioP := strings.ToLower(strings.TrimSpace(audioPref))
	qualityP := strings.ToLower(strings.TrimSpace(qualityPref))

	// 1. Audio Filter
	var audioFiltered []providers.StreamSource
	switch audioP {
	case "dub":
		for _, s := range sources {
			if s.AudioType == providers.AudioTypeDub {
				audioFiltered = append(audioFiltered, s)
			} else {
				low := strings.ToLower(s.Title + " " + s.Name)
				if strings.Contains(low, "dublaj") || strings.Contains(low, "tr dub") {
					audioFiltered = append(audioFiltered, s)
				}
			}
		}
	case "sub", "original":
		for _, s := range sources {
			if s.AudioType == providers.AudioTypeSub || s.AudioType == providers.AudioTypeOriginal {
				audioFiltered = append(audioFiltered, s)
			} else if len(s.Subtitles) > 0 {
				audioFiltered = append(audioFiltered, s)
			} else {
				low := strings.ToLower(s.Title + " " + s.Name)
				if strings.Contains(low, "altyazı") || strings.Contains(low, "altyazi") || strings.Contains(low, "japonca") {
					audioFiltered = append(audioFiltered, s)
				}
			}
		}
	case "en_dub", "endub":
		for _, s := range sources {
			if s.AudioType == providers.AudioTypeEnDub {
				audioFiltered = append(audioFiltered, s)
			} else {
				low := strings.ToLower(s.Title + " " + s.Name)
				if strings.Contains(low, "ingilizce dublaj") || strings.Contains(low, "en dub") || strings.Contains(low, "endub") {
					audioFiltered = append(audioFiltered, s)
				}
			}
		}
	}

	if len(audioFiltered) == 0 {
		audioFiltered = sources
	}

	// 2. Quality Filter
	if qualityP != "" && qualityP != "auto" {
		normTarget := providers.NormalizeQuality(qualityP)
		var qualMatched []providers.StreamSource
		for _, s := range audioFiltered {
			normS := providers.NormalizeQuality(s.Quality)
			if strings.EqualFold(normS, normTarget) || strings.Contains(strings.ToLower(s.Quality), qualityP) {
				qualMatched = append(qualMatched, s)
			}
		}
		if len(qualMatched) > 0 {
			if res, err := providers.SelectBestStream(qualMatched); err == nil && res != nil && res.Primary.URL != "" {
				return res
			}
		}
	}

	res, _ := providers.SelectBestStream(audioFiltered)
	return res
}

// FilterAndSortStreamsByAudio is a backward-compatible wrapper for FilterAndSortStreamsByAudioAndQuality.
func FilterAndSortStreamsByAudio(sources []providers.StreamSource, audioPref string) *providers.StreamResult {
	return FilterAndSortStreamsByAudioAndQuality(sources, audioPref, "auto")
}

func qualityWeight(q string) int {
	ql := strings.ToLower(q)
	switch {
	case strings.Contains(ql, "4k") || strings.Contains(ql, "2160"):
		return 400
	case strings.Contains(ql, "1440") || strings.Contains(ql, "2k"):
		return 300
	case strings.Contains(ql, "1080"):
		return 200
	case strings.Contains(ql, "720"):
		return 150
	case strings.Contains(ql, "hd"):
		return 120
	case strings.Contains(ql, "480"):
		return 80
	default:
		return 50
	}
}

func (mr *MultiResolver) resolveFromProvider(ctx context.Context, prov providers.Provider, mediaType string, tmdbID int, title, origTitle, year string, altTitles []string, season, episode int) (*providers.StreamResult, error) {
	// 1. Direct TMDB ID providers (VidLink & Vidmody)
	if tmdbID > 0 && (prov.ID() == "vidlink" || prov.ID() == "vidmody") {
		if mediaType == "movie" {
			return prov.ResolveStream(ctx, fmt.Sprintf("movie:%d", tmdbID))
		}
		return prov.ResolveStream(ctx, fmt.Sprintf("tv:%d:%d:%d", tmdbID, season, episode))
	}

	// 2. Build ordered list of Latin search queries (Turkish, English, Romaji, Original)
	var queries []string
	seenQ := make(map[string]bool)
	addQuery := func(q string) {
		q = strings.TrimSpace(q)
		if q == "" || !hasLatinCharacters(q) {
			return
		}
		lq := strings.ToLower(q)
		if !seenQ[lq] {
			seenQ[lq] = true
			queries = append(queries, q)
		}
	}
	addQuery(title)
	for _, alt := range altTitles {
		addQuery(alt)
	}
	addQuery(origTitle)

	if len(queries) == 0 {
		return nil, fmt.Errorf("%s: no searchable Latin title for '%s'", prov.Name(), title)
	}

	// 3. Search and strictly verify title & year match
	var matchedItem *providers.MediaItem
	for _, q := range queries {
		items, err := prov.Search(ctx, q)
		if err != nil || len(items) == 0 {
			continue
		}
		for i := range items {
			it := &items[i]
			if isStrictTitleMatch(queries, year, it.Title, it.Year) {
				matchedItem = it
				break
			}
		}
		if matchedItem != nil {
			break
		}
	}

	// NEVER fall back to items[0] if no title matched!
	if matchedItem == nil {
		return nil, fmt.Errorf("%s: no strict title match found for '%s'", prov.Name(), title)
	}

	if mediaType == "movie" {
		streamID := matchedItem.ID
		var det *providers.MediaDetail
		if d, err := prov.GetDetails(ctx, matchedItem.ID); err == nil && d != nil {
			det = d
			if det.StreamID != "" {
				streamID = det.StreamID
			}
		}
		if res, err := prov.ResolveStream(ctx, streamID); err == nil && res != nil && res.Primary.URL != "" {
			return res, nil
		}
		// Fallback for anime/series providers that store movies as Season 1 Episode 1
		eps, _ := prov.GetEpisodes(ctx, matchedItem.ID, 1)
		if len(eps) == 0 && det != nil && len(det.Seasons) > 0 {
			eps = det.Seasons[0].Episodes
		}
		if len(eps) > 0 && eps[0].StreamID != "" {
			return prov.ResolveStream(ctx, eps[0].StreamID)
		}
		return nil, fmt.Errorf("%s: could not resolve movie stream for '%s'", prov.Name(), title)
	}

	// 4. Series / Anime strict season & episode resolution (with single-entry fallback for S01E01)
	var det *providers.MediaDetail
	episodes, err := prov.GetEpisodes(ctx, matchedItem.ID, season)
	if err != nil || len(episodes) == 0 {
		if d, err := prov.GetDetails(ctx, matchedItem.ID); err == nil && d != nil {
			det = d
			for _, s := range det.Seasons {
				if s.Number == season {
					episodes = s.Episodes
					break
				}
			}
			// Only allow single-season fallback when requesting Season 1
			if len(episodes) == 0 && season == 1 && len(det.Seasons) == 1 && det.Seasons[0].Number <= 1 {
				episodes = det.Seasons[0].Episodes
			}
		}
	}

	if len(episodes) == 0 {
		// If requesting S01E01 and the provider stores it as a single direct stream (e.g., movie/hybrid provider)
		if season == 1 && episode == 1 {
			streamID := matchedItem.ID
			if det == nil {
				det, _ = prov.GetDetails(ctx, matchedItem.ID)
			}
			if det != nil && det.StreamID != "" {
				streamID = det.StreamID
			}
			if res, err := prov.ResolveStream(ctx, streamID); err == nil && res != nil && res.Primary.URL != "" {
				return res, nil
			}
		}
		return nil, fmt.Errorf("%s: season %d not found", prov.Name(), season)
	}

	var targetEp *providers.Episode
	allZeroNumbers := true
	for i := range episodes {
		ep := &episodes[i]
		if ep.Number > 0 {
			allZeroNumbers = false
		}
		if ep.Number == episode {
			targetEp = ep
			break
		}
	}

	// Only use index-based lookup if the provider left all Episode.Number fields as 0
	if targetEp == nil && allZeroNumbers && episode > 0 && episode <= len(episodes) {
		targetEp = &episodes[episode-1]
	}

	// NEVER fall back to episodes[0] if the requested episode does not exist!
	if targetEp == nil {
		return nil, fmt.Errorf("%s: S%02dE%02d not found", prov.Name(), season, episode)
	}

	return prov.ResolveStream(ctx, targetEp.StreamID)
}

func hasLatinCharacters(s string) bool {
	for _, r := range s {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') {
			return true
		}
	}
	return false
}

var (
	yearParensRe = regexp.MustCompile(`\(\s*(19\d{2}|20\d{2})\s*\)`)
	trReplacer   = strings.NewReplacer(
		"ç", "c", "Ç", "c",
		"ğ", "g", "Ğ", "g",
		"ı", "i", "İ", "i", "I", "i",
		"ö", "o", "Ö", "o",
		"ş", "s", "Ş", "s",
		"ü", "u", "Ü", "u",
		"â", "a", "Â", "a",
		"î", "i", "Î", "i",
		"û", "u", "Û", "u",
	)
	noiseWords = map[string]bool{
		"izle": true, "full": true, "hd": true, "1080p": true, "720p": true, "4k": true,
		"turkce": true, "dublaj": true, "altyazili": true, "altyazi": true,
		"bolum": true, "sezon": true, "dizi": true, "film": true, "anime": true,
		"sansursuz": true, "tek": true, "parca": true, "online": true,
	}
)

func normalizeTitleTokens(raw string, stripNoise bool) []string {
	s := trReplacer.Replace(raw)
	s = yearParensRe.ReplaceAllString(s, " ")
	s = strings.ToLower(s)

	var sb strings.Builder
	for _, r := range s {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') {
			sb.WriteRune(r)
		} else {
			sb.WriteRune(' ')
		}
	}

	fields := strings.Fields(sb.String())
	if !stripNoise {
		return fields
	}
	var out []string
	for _, f := range fields {
		if !noiseWords[f] {
			out = append(out, f)
		}
	}
	if len(out) == 0 {
		return fields
	}
	return out
}

func isStrictTitleMatch(knownTitles []string, expectedYear, candidateTitle, candidateYear string) bool {
	// If candidate embeds a year like "(2001)" and we expect "2019", check year compatibility
	candYear := strings.TrimSpace(candidateYear)
	if candYear == "" {
		if m := yearParensRe.FindStringSubmatch(candidateTitle); len(m) > 1 {
			candYear = m[1]
		}
	}
	if expectedYear != "" && candYear != "" {
		y1, err1 := strconv.Atoi(expectedYear)
		y2, err2 := strconv.Atoi(candYear)
		if err1 == nil && err2 == nil && y1 > 1900 && y2 > 1900 {
			diff := y1 - y2
			if diff < -1 || diff > 1 {
				return false
			}
		}
	}

	candTokens := normalizeTitleTokens(candidateTitle, true)
	if len(candTokens) == 0 {
		return false
	}
	candJoined := strings.Join(candTokens, " ")

	for _, kt := range knownTitles {
		qTokens := normalizeTitleTokens(kt, true)
		if len(qTokens) == 0 {
			continue
		}
		qJoined := strings.Join(qTokens, " ")

		// 1. Exact normalized title match
		if qJoined == candJoined {
			return true
		}

		// 2. Prefix/Subset match where all query tokens match in order and candidate doesn't have unrelated extra words
		if len(candTokens) >= len(qTokens) && len(candTokens) <= len(qTokens)+2 {
			allMatch := true
			for i, qt := range qTokens {
				if candTokens[i] != qt {
					allMatch = false
					break
				}
			}
			if allMatch {
				return true
			}
		}
	}

	return false
}

// isCloudResolver reports whether a provider is a TMDB-ID based cloud resolver (VidLink/Vidmody).
// These are used only as last-resort fallbacks because their anime/series episode mapping is unreliable.
func isCloudResolver(id string) bool {
	return id == "vidlink" || id == "vidmody"
}
