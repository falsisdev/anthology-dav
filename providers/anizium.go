package providers

import (
	"context"
	"fmt"
	"net/url"
	"strconv"
	"strings"

	"anthology-dav/config"
)

type AniziumProvider struct{}

func init() {
	Register(&AniziumProvider{})
}

func (p *AniziumProvider) ID() string   { return "anizium" }
func (p *AniziumProvider) Name() string { return "Anizium" }

func (p *AniziumProvider) apiURL() string {
	if u := config.RemoteString("urls.anime.anizium.base", ""); u != "" {
		return strings.TrimRight(u, "/")
	}
	if u := config.RemoteString("urls.anime.anizium.api", ""); u != "" {
		return strings.TrimRight(u, "/")
	}
	return "https://api.anizium.co"
}

func (p *AniziumProvider) siteURL() string {
	if u := config.RemoteString("urls.anime.anizium.site", ""); u != "" {
		return strings.TrimRight(u, "/")
	}
	return "https://anizium.co"
}

func (p *AniziumProvider) headers() map[string]string {
	site := p.siteURL()
	return map[string]string{
		"User-Agent": config.DefaultUserAgent,
		"Accept":     "application/json, text/plain, */*",
		"Origin":     site,
		"Referer":    site + "/",
		"Cf-Control": BuildAniziumCfControl(),
		"device":     "browser",
		"language":   "tr",
		"site":       "main",
	}
}

type aniziumCatalogItem struct {
	ID            any    `json:"ID"`
	Name          string `json:"name"`
	Type          string `json:"type"`
	Poster        string `json:"poster"`
	Banner        string `json:"banner"`
	DetailsBanner string `json:"details_banner"`
	Overview      string `json:"overview"`
	ReleaseYear   any    `json:"release_year"`
}

type aniziumHomeResponse struct {
	Success          bool                 `json:"success"`
	SettlementTop    []aniziumCatalogItem `json:"settlement_top"`
	SettlementMiddle []aniziumCatalogItem `json:"settlement_middle"`
	SettlementLower  []aniziumCatalogItem `json:"settlement_lower"`
}

func (p *AniziumProvider) GetCatalog(ctx context.Context, page int) ([]MediaItem, error) {
	u := p.apiURL() + "/page/home"
	var resp aniziumHomeResponse
	if err := FetchJSON(ctx, u, p.headers(), &resp); err != nil {
		return nil, err
	}

	var rawItems []aniziumCatalogItem
	rawItems = append(rawItems, resp.SettlementTop...)
	rawItems = append(rawItems, resp.SettlementMiddle...)
	rawItems = append(rawItems, resp.SettlementLower...)

	var items []MediaItem
	seen := make(map[string]bool)

	for _, it := range rawItems {
		idStr := fmt.Sprintf("%v", it.ID)
		if idStr == "" || idStr == "<nil>" || seen[idStr] {
			continue
		}
		title := strings.TrimSpace(it.Name)
		if title == "" {
			continue
		}
		seen[idStr] = true

		poster := it.Poster
		if poster == "" {
			poster = it.DetailsBanner
		}
		if poster == "" {
			poster = it.Banner
		}
		if poster != "" && !strings.HasPrefix(poster, "http") {
			poster = "https://x.anizium.co/assets/anime-poster/" + poster
		}

		mType := MediaTypeAnime
		if strings.EqualFold(it.Type, "movie") {
			mType = MediaTypeMovie
		}

		items = append(items, MediaItem{
			ID:          idStr,
			Title:       title,
			Year:        ExtractYear(fmt.Sprintf("%v", it.ReleaseYear)),
			Type:        mType,
			PosterURL:   poster,
			Description: it.Overview,
		})
	}

	return items, nil
}

type aniziumSearchResponse struct {
	Success bool `json:"success"`
	Page    struct {
		Data []aniziumCatalogItem `json:"data"`
	} `json:"page"`
}

func cleanAniziumQuery(str string) string {
	s := strings.ToLower(str)
	replacer := strings.NewReplacer(
		"ı", "i", "İ", "i",
		"ü", "u", "Ü", "u",
		"ö", "o", "Ö", "o",
		"ş", "s", "Ş", "s",
		"ğ", "g", "Ğ", "g",
		"ç", "c", "Ç", "c",
		":", " ", "-", " ", "_", " ", "/", " ",
	)
	s = replacer.Replace(s)
	words := strings.Fields(s)
	return strings.Join(words, " ")
}

func (p *AniziumProvider) Search(ctx context.Context, query string) ([]MediaItem, error) {
	q := strings.TrimSpace(query)
	if q == "" {
		return nil, nil
	}

	searchOnce := func(term string) ([]MediaItem, error) {
		u := fmt.Sprintf("%s/page/search?value=%s&page=1", p.apiURL(), url.QueryEscape(term))
		var resp aniziumSearchResponse
		if err := FetchJSON(ctx, u, p.headers(), &resp); err != nil {
			return nil, err
		}
		var items []MediaItem
		for _, it := range resp.Page.Data {
			idStr := fmt.Sprintf("%v", it.ID)
			title := strings.TrimSpace(it.Name)
			if idStr == "" || idStr == "<nil>" || title == "" {
				continue
			}
			poster := it.Poster
			if poster == "" {
				poster = it.DetailsBanner
			}
			if poster == "" {
				poster = it.Banner
			}
			if poster != "" && !strings.HasPrefix(poster, "http") {
				poster = "https://x.anizium.co/assets/anime-poster/" + poster
			}

			mType := MediaTypeAnime
			if strings.EqualFold(it.Type, "movie") {
				mType = MediaTypeMovie
			}

			items = append(items, MediaItem{
				ID:          idStr,
				Title:       title,
				Year:        ExtractYear(fmt.Sprintf("%v", it.ReleaseYear)),
				Type:        mType,
				PosterURL:   poster,
				Description: it.Overview,
			})
		}
		return items, nil
	}

	// 1. Direct search
	items, err := searchOnce(q)
	if err == nil && len(items) > 0 {
		return items, nil
	}

	// 2. Cleaned query search (strip punctuation & normalize turkish chars)
	cleanQ := cleanAniziumQuery(q)
	if cleanQ != "" && !strings.EqualFold(cleanQ, q) {
		items, err = searchOnce(cleanQ)
		if err == nil && len(items) > 0 {
			return items, nil
		}
	}

	// 3. First word fallback for compound titles (e.g. "Naruto Shippuden" -> "Naruto")
	words := strings.Fields(cleanQ)
	if len(words) > 1 && len(words[0]) >= 4 {
		items, err = searchOnce(words[0])
		if err == nil && len(items) > 0 {
			return items, nil
		}
	}

	return nil, nil
}

type aniziumEpisodeItem struct {
	ID       any    `json:"ID"`
	Name     string `json:"name"`
	Number   int    `json:"number"`
	Overview string `json:"overview"`
}

type aniziumSeasonItem struct {
	ID       any                  `json:"ID"`
	Name     string               `json:"name"`
	Number   int                  `json:"number"`
	Episodes []aniziumEpisodeItem `json:"episodes"`
}

type aniziumDetailResponse struct {
	Success bool `json:"success"`
	Data    struct {
		ID            any                 `json:"ID"`
		Name          string              `json:"name"`
		NameTR        string              `json:"name_tr"`
		Type          string              `json:"type"`
		Poster        string              `json:"poster"`
		Banner        string              `json:"banner"`
		DetailsBanner string              `json:"details_banner"`
		Overview      string              `json:"overview"`
		ReleaseYear   any                 `json:"release_year"`
		Seasons       []aniziumSeasonItem `json:"seasons"`
	} `json:"data"`
}

func (p *AniziumProvider) GetDetails(ctx context.Context, mediaID string) (*MediaDetail, error) {
	cleanID := strings.TrimPrefix(mediaID, "anizium:")
	cleanID = strings.TrimPrefix(cleanID, "anime:")
	cleanID = strings.TrimPrefix(cleanID, "movie:")
	cleanID = strings.TrimPrefix(cleanID, "ep:")
	if parts := strings.Split(cleanID, ":"); len(parts) > 1 {
		cleanID = parts[0]
	}

	u := fmt.Sprintf("%s/anime/get?id=%s", p.apiURL(), url.QueryEscape(cleanID))
	var resp aniziumDetailResponse
	if err := FetchJSON(ctx, u, p.headers(), &resp); err != nil {
		return nil, err
	}
	d := resp.Data
	title := strings.TrimSpace(d.Name)
	if title == "" {
		title = strings.TrimSpace(d.NameTR)
	}
	if title == "" {
		title = "Anizium #" + cleanID
	}

	poster := d.Poster
	if poster == "" {
		poster = d.DetailsBanner
	}
	if poster == "" {
		poster = d.Banner
	}
	if poster != "" && !strings.HasPrefix(poster, "http") {
		poster = "https://x.anizium.co/assets/anime-poster/" + poster
	}

	isMovie := strings.EqualFold(d.Type, "movie")
	mType := MediaTypeAnime
	if isMovie {
		mType = MediaTypeMovie
	}

	var seasons []Season
	for _, s := range d.Seasons {
		sNum := s.Number
		if sNum <= 0 {
			sNum = 1
		}
		var eps []Episode
		for _, ep := range s.Episodes {
			eNum := ep.Number
			if eNum <= 0 {
				eNum = 1
			}
			epTitle := ep.Name
			if epTitle == "" {
				epTitle = fmt.Sprintf("%d. Sezon %d. Bölüm", sNum, eNum)
			} else {
				epTitle = fmt.Sprintf("%d. Sezon %d. Bölüm - %s", sNum, eNum, epTitle)
			}
			streamID := fmt.Sprintf("%s:%d:%d", cleanID, sNum, eNum)
			eps = append(eps, Episode{
				ID:       streamID,
				StreamID: streamID,
				Title:    epTitle,
				Season:   sNum,
				Number:   eNum,
			})
		}
		if len(eps) > 0 {
			sName := s.Name
			if sName == "" {
				sName = fmt.Sprintf("Season %02d", sNum)
			}
			seasons = append(seasons, Season{
				Number:   sNum,
				Name:     sName,
				Episodes: eps,
			})
		}
	}

	defaultStreamID := fmt.Sprintf("%s:1:1", cleanID)
	if len(seasons) == 0 {
		seasons = []Season{
			{
				Number: 1,
				Name:   "Season 01",
				Episodes: []Episode{
					{
						ID:       defaultStreamID,
						StreamID: defaultStreamID,
						Title:    "1. Sezon 1. Bölüm",
						Season:   1,
						Number:   1,
					},
				},
			},
		}
	}

	return &MediaDetail{
		MediaItem: MediaItem{
			ID:          cleanID,
			Title:       title,
			Year:        ExtractYear(fmt.Sprintf("%v", d.ReleaseYear)),
			Type:        mType,
			PosterURL:   poster,
			Description: d.Overview,
		},
		StreamID: defaultStreamID,
		Seasons:  seasons,
	}, nil
}

func (p *AniziumProvider) GetEpisodes(ctx context.Context, mediaID string, season int) ([]Episode, error) {
	det, err := p.GetDetails(ctx, mediaID)
	if err != nil {
		return nil, err
	}
	for _, s := range det.Seasons {
		if s.Number == season {
			return s.Episodes, nil
		}
	}
	if len(det.Seasons) > 0 && season == 1 {
		return det.Seasons[0].Episodes, nil
	}
	return nil, nil
}

type aniziumSubtitleItem struct {
	Group string `json:"group"`
	Name  string `json:"name"`
	Link  string `json:"link"`
	Row   int    `json:"row"`
}

type aniziumStreamQualityItem struct {
	Quality any    `json:"quality"`
	Link    string `json:"link"`
	Type    string `json:"type"`
}

type aniziumStreamGroup struct {
	Group    string                     `json:"group"`
	Name     string                     `json:"name"`
	Type     string                     `json:"type"`
	Platform string                     `json:"platform"`
	Items    []aniziumStreamQualityItem `json:"items"`
}

type aniziumSourceResponse struct {
	Success   bool                  `json:"success"`
	Subtitles []aniziumSubtitleItem `json:"subtitles"`
	Groups    []aniziumStreamGroup  `json:"groups"`
}

func mapAniziumLangCode(group, name string) (langCode, label string) {
	g := strings.ToLower(strings.TrimSpace(group))
	switch g {
	case "tr":
		langCode = "tr"
		label = "Türkçe"
	case "en":
		langCode = "en"
		label = "İngilizce"
	case "de":
		langCode = "de"
		label = "Almanca"
	case "fr":
		langCode = "fr"
		label = "Fransızca"
	case "es":
		langCode = "es"
		label = "İspanyolca"
	case "it":
		langCode = "it"
		label = "İtalyanca"
	case "ar":
		langCode = "ar"
		label = "Arapça"
	default:
		langCode = g
		if langCode == "" {
			langCode = "tr"
		}
		label = strings.ToUpper(g)
		if label == "" {
			label = "Altyazı"
		}
	}
	if name != "" {
		label = name
	}
	return langCode, label
}

func (p *AniziumProvider) ResolveStream(ctx context.Context, streamID string) (*StreamResult, error) {
	clean := strings.TrimPrefix(streamID, "anizium:")
	clean = strings.TrimPrefix(clean, "ep:")
	clean = strings.TrimPrefix(clean, "anime:")
	isMovie := strings.HasPrefix(clean, "movie:")
	clean = strings.TrimPrefix(clean, "movie:")

	parts := strings.Split(clean, ":")
	animeID := parts[0]
	season := 1
	episode := 1
	if len(parts) >= 3 {
		season, _ = strconv.Atoi(parts[1])
		episode, _ = strconv.Atoi(parts[2])
	}
	if season <= 0 {
		season = 1
	}
	if episode <= 0 {
		episode = 1
	}

	fetchSource := func(useEp bool) (*aniziumSourceResponse, error) {
		var u string
		if useEp {
			u = fmt.Sprintf("%s/anime/source?id=%s&site=main&server=1&season=%d&episode=%d", p.apiURL(), url.QueryEscape(animeID), season, episode)
		} else {
			u = fmt.Sprintf("%s/anime/source?id=%s&site=main&server=1", p.apiURL(), url.QueryEscape(animeID))
		}
		var srcResp aniziumSourceResponse
		if err := FetchJSON(ctx, u, p.headers(), &srcResp); err != nil {
			return nil, err
		}
		return &srcResp, nil
	}

	var srcResp *aniziumSourceResponse
	var err error
	if isMovie {
		srcResp, err = fetchSource(false)
		if (err != nil || srcResp == nil || !srcResp.Success || len(srcResp.Groups) == 0) && (season > 0 || episode > 0) {
			srcResp, err = fetchSource(true)
		}
	} else {
		srcResp, err = fetchSource(true)
		if (err != nil || srcResp == nil || !srcResp.Success || len(srcResp.Groups) == 0) {
			// Fallback without season/episode in case it is structured as a movie
			srcResp, err = fetchSource(false)
		}
	}

	if err != nil {
		return nil, fmt.Errorf("anizium: failed to fetch stream source: %w", err)
	}
	if srcResp == nil || !srcResp.Success || len(srcResp.Groups) == 0 {
		return nil, fmt.Errorf("anizium: no stream groups found for id=%s S%dE%d", animeID, season, episode)
	}

	site := p.siteURL()
	streamHeaders := map[string]string{
		"User-Agent": config.DefaultUserAgent,
		"Referer":    site + "/",
		"Origin":     site,
	}

	// 1. Format Subtitles (Turkish, English, German, French, Arabic, etc.)
	var subtitles []Subtitle
	seenSub := make(map[string]bool)
	for i, rawSub := range srcResp.Subtitles {
		subURL := strings.TrimSpace(rawSub.Link)
		if subURL == "" || seenSub[subURL] {
			continue
		}
		seenSub[subURL] = true
		langCode, label := mapAniziumLangCode(rawSub.Group, rawSub.Name)
		subtitles = append(subtitles, Subtitle{
			ID:       fmt.Sprintf("anizium_%s_%d", langCode, i+1),
			Language: langCode,
			Label:    label,
			Format:   "vtt",
			URL:      subURL,
			Headers:  streamHeaders,
		})
	}

	// 2. Format Streams for all dub groups (TR Dub, Original/TR Sub, EN Dub, etc.)
	var sources []StreamSource
	for _, grp := range srcResp.Groups {
		gKey := strings.ToLower(strings.TrimSpace(grp.Group))
		var groupTag string
		switch gKey {
		case "trdub":
			groupTag = "Türkçe Dublaj"
		case "original":
			groupTag = "Japonca [TR Altyazılı]"
		case "endub":
			groupTag = "İngilizce Dublaj"
		default:
			if grp.Name != "" {
				if strings.Contains(strings.ToLower(grp.Name), "dub") {
					groupTag = grp.Name
				} else {
					groupTag = grp.Name + " Dublaj"
				}
			} else {
				groupTag = "Japonca"
			}
		}

		for _, item := range grp.Items {
			link := strings.TrimSpace(item.Link)
			if link == "" {
				continue
			}

			qInt, _ := strconv.Atoi(fmt.Sprintf("%v", item.Quality))
			var qLabel string
			switch qInt {
			case 2160:
				qLabel = "4K UHD"
			case 1440:
				qLabel = "2K QHD"
			default:
				if qInt > 0 {
					qLabel = fmt.Sprintf("%dp", qInt)
				} else {
					qLabel = "1080p"
				}
			}

			fmtType := strings.ToLower(strings.TrimSpace(item.Type))
			if fmtType == "" {
				if grp.Type != "" {
					fmtType = strings.ToLower(strings.TrimSpace(grp.Type))
				} else {
					fmtType = "mp4"
				}
			}
			isHLS := fmtType == "hls" || strings.Contains(strings.ToLower(link), ".m3u8")

			streamTitle := fmt.Sprintf("Anizium • %s • %s", groupTag, qLabel)

			sources = append(sources, StreamSource{
				Name:      "Anizium",
				Title:     streamTitle,
				URL:       link,
				Quality:   qLabel,
				Format:    fmtType,
				IsHLS:     isHLS,
				Headers:   streamHeaders,
				Subtitles: subtitles,
			})
		}
	}

	if len(sources) == 0 {
		return nil, fmt.Errorf("anizium: no playable streams found for id=%s S%dE%d", animeID, season, episode)
	}

	return SelectBestStream(sources)
}
