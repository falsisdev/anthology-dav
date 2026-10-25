package providers

import (
	"context"
	"encoding/json"
	"fmt"
	"regexp"
	"strconv"
	"strings"

	"anthology-dav/config"
)

type PuhuTVProvider struct{}

func init() {
	Register(&PuhuTVProvider{})
}

func (p *PuhuTVProvider) ID() string   { return "puhutv" }
func (p *PuhuTVProvider) Name() string { return "PuhuTV" }

func (p *PuhuTVProvider) baseURL() string {
	return config.RemoteString("urls.series_local.puhutv.base", "https://puhutv.com")
}

var puhuNextDataRe = regexp.MustCompile(`(?is)<script[^>]+id="__NEXT_DATA__"[^>]*>(.*?)</script>`)

func (p *PuhuTVProvider) parseNextCatalog(htmlStr string) []MediaItem {
	m := puhuNextDataRe.FindStringSubmatch(htmlStr)
	if len(m) < 2 {
		return nil
	}
	var root map[string]any
	if err := json.Unmarshal([]byte(m[1]), &root); err != nil {
		return nil
	}

	var items []MediaItem
	seen := make(map[string]bool)
	var walk func(node any)
	walk = func(node any) {
		switch v := node.(type) {
		case map[string]any:
			name, _ := v["name"].(string)
			if name == "" {
				name, _ = v["display_name"].(string)
			}
			slug, _ := v["slug_path"].(string)
			if slug == "" {
				if meta, ok := v["meta"].(map[string]any); ok {
					slug, _ = meta["slug"].(string)
				}
			}
			if slug == "" {
				slug, _ = v["to_watch_asset_slug"].(string)
			}
			if slug == "" {
				slug, _ = v["slug"].(string)
			}
			cleanSlug := strings.TrimSuffix(slug, "-detay")
			if cleanSlug != "" && name != "" && !seen[cleanSlug] {
				seen[cleanSlug] = true
				mType := MediaTypeSeries
				if mt, _ := v["type"].(string); strings.Contains(strings.ToLower(mt), "movie") {
					mType = MediaTypeMovie
				}
				items = append(items, MediaItem{
					ID:    cleanSlug,
					Title: name,
					Type:  mType,
				})
			}
			for _, child := range v {
				walk(child)
			}
		case []any:
			for _, child := range v {
				walk(child)
			}
		}
	}
	walk(root)
	return items
}

func (p *PuhuTVProvider) GetCatalog(ctx context.Context, page int) ([]MediaItem, error) {
	base := p.baseURL()
	htmlStr, _, err := FetchString(ctx, base+"/yerli-diziler", DefaultHeaders(base+"/"))
	if err != nil {
		return nil, err
	}
	items := p.parseNextCatalog(htmlStr)
	if htmlMovies, _, err2 := FetchString(ctx, base+"/filmler", DefaultHeaders(base+"/")); err2 == nil {
		items = append(items, p.parseNextCatalog(htmlMovies)...)
	}
	return items, nil
}

func (p *PuhuTVProvider) Search(ctx context.Context, query string) ([]MediaItem, error) {
	cat, err := p.GetCatalog(ctx, 1)
	if err != nil {
		return nil, err
	}
	q := UltraClean(query)
	var matched []MediaItem
	for _, it := range cat {
		if strings.Contains(UltraClean(it.Title), q) || strings.Contains(UltraClean(it.ID), q) {
			matched = append(matched, it)
		}
	}
	if len(matched) == 0 {
		slug := Slugify(query)
		if slug != "" {
			matched = append(matched, MediaItem{
				ID:    slug,
				Title: query,
				Type:  MediaTypeSeries,
			})
		}
	}
	return matched, nil
}

func (p *PuhuTVProvider) GetDetails(ctx context.Context, mediaID string) (*MediaDetail, error) {
	slug := strings.TrimSuffix(strings.TrimPrefix(mediaID, "puhutv:"), "-detay")
	base := p.baseURL()
	candidates := []string{
		fmt.Sprintf("%s/%s-detay", base, slug),
		fmt.Sprintf("%s/%s", base, slug),
		fmt.Sprintf("%s/%s-1-sezon-bolumleri", base, slug),
	}

	var htmlStr string
	for _, cand := range candidates {
		s, _, err := FetchString(ctx, cand, DefaultHeaders(base+"/"))
		if err == nil && strings.Contains(s, "__NEXT_DATA__") {
			htmlStr = s
			break
		}
	}
	if htmlStr == "" {
		return nil, fmt.Errorf("puhutv details page not found for %s", slug)
	}

	title := slug
	if m := regexp.MustCompile(`(?is)<h1[^>]*>(.*?)</h1>`).FindStringSubmatch(htmlStr); len(m) > 1 {
		title = CleanMediaTitle(m[1])
	}

	seasonMap := make(map[int][]Episode)
	singleVid := ""

	if m := puhuNextDataRe.FindStringSubmatch(htmlStr); len(m) > 1 {
		var root map[string]any
		if json.Unmarshal([]byte(m[1]), &root) == nil {
			if props, ok := root["props"].(map[string]any); ok {
				if pp, ok := props["pageProps"].(map[string]any); ok {
					// Check details for show title
					if d, ok := pp["details"].(map[string]any); ok {
						if dData, ok := d["data"].(map[string]any); ok {
							if dName, ok := dData["display_name"].(string); ok && dName != "" {
								title = dName
							} else if dName, ok := dData["name"].(string); ok && dName != "" {
								title = dName
							}
							if v, ok := dData["video_id"].(string); ok && v != "" {
								singleVid = v
							}
						}
					}
					// Parse episodeData
					if epData, ok := pp["episodeData"].(map[string]any); ok {
						parseSeasonList := func(sList []any) {
							for sIdx, sNode := range sList {
								if sMap, ok := sNode.(map[string]any); ok {
									sNum := sIdx + 1
									if sn, ok := sMap["name"].(string); ok {
										if sm := regexp.MustCompile(`(\d+)\.\s*Sezon`).FindStringSubmatch(sn); len(sm) > 1 {
											sNum, _ = strconv.Atoi(sm[1])
										}
									}
									if epArr, ok := sMap["episodes"].([]any); ok {
										for _, eNode := range epArr {
											if eMap, ok := eNode.(map[string]any); ok {
												vid, _ := eMap["video_id"].(string)
												epName, _ := eMap["name"].(string)
												if vid != "" {
													pos := len(seasonMap[sNum]) + 1
													if meta, ok := eMap["meta"].(map[string]any); ok {
														if p, ok := meta["position"].(float64); ok && p > 0 {
															pos = int(p)
														}
													}
													seasonMap[sNum] = append(seasonMap[sNum], Episode{
														ID:       vid,
														StreamID: vid,
														Title:    epName,
														Season:   sNum,
														Number:   pos,
													})
												}
											}
										}
									}
								}
							}
						}

						if dList, ok := epData["data"].([]any); ok {
							parseSeasonList(dList)
						} else if dMap, ok := epData["data"].(map[string]any); ok {
							parseSeasonList([]any{dMap})
						}
					}
				}
			}

			// If still empty, walk recursively for video_id
			if len(seasonMap) == 0 && singleVid == "" {
				seenVid := make(map[string]bool)
				var walk func(node any)
				walk = func(node any) {
					switch v := node.(type) {
					case map[string]any:
						vid, _ := v["video_id"].(string)
						epName, _ := v["name"].(string)
						if vid != "" && !seenVid[vid] {
							seenVid[vid] = true
							if singleVid == "" {
								singleVid = vid
							}
							eNum := len(seasonMap[1]) + 1
							if em := regexp.MustCompile(`(\d+)\.\s*B[öo]l[üu]m`).FindStringSubmatch(epName); len(em) > 1 {
								eNum, _ = strconv.Atoi(em[1])
							}
							seasonMap[1] = append(seasonMap[1], Episode{
								ID:       vid,
								StreamID: vid,
								Title:    epName,
								Season:   1,
								Number:   eNum,
							})
						}
						for _, child := range v {
							walk(child)
						}
					case []any:
						for _, child := range v {
							walk(child)
						}
					}
				}
				walk(root)
			}
		}
	}

	if len(seasonMap) == 0 {
		return &MediaDetail{
			MediaItem: MediaItem{
				ID:    slug,
				Title: title,
				Type:  MediaTypeMovie,
			},
			StreamID: singleVid,
		}, nil
	}

	var seasons []Season
	for sNum, eps := range seasonMap {
		seasons = append(seasons, Season{
			Number:   sNum,
			Name:     fmt.Sprintf("Season %02d", sNum),
			Episodes: eps,
		})
	}

	return &MediaDetail{
		MediaItem: MediaItem{
			ID:    slug,
			Title: title,
			Type:  MediaTypeSeries,
		},
		Seasons: seasons,
	}, nil
}

func (p *PuhuTVProvider) GetEpisodes(ctx context.Context, mediaID string, season int) ([]Episode, error) {
	det, err := p.GetDetails(ctx, mediaID)
	if err != nil {
		return nil, err
	}
	for _, s := range det.Seasons {
		if s.Number == season {
			return s.Episodes, nil
		}
	}
	return nil, nil
}

func (p *PuhuTVProvider) ResolveStream(ctx context.Context, streamID string) (*StreamResult, error) {
	vid := strings.TrimPrefix(streamID, "puhutv:")
	sources := ExtractDYGVideo(ctx, 29, vid, p.baseURL()+"/")
	return SelectBestStream(sources)
}
