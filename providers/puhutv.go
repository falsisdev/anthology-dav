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
			slug, _ := v["slug_path"].(string)
			name, _ := v["name"].(string)
			if name == "" {
				name, _ = v["display_name"].(string)
			}
			if slug != "" && name != "" && strings.HasSuffix(slug, "-detay") && !seen[slug] {
				seen[slug] = true
				mType := MediaTypeSeries
				if mt, _ := v["meta_type"].(string); strings.Contains(strings.ToLower(mt), "movie") || strings.Contains(strings.ToLower(mt), "film") {
					mType = MediaTypeMovie
				}
				items = append(items, MediaItem{
					ID:    strings.TrimSuffix(slug, "-detay"),
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
	htmlStr, _, err := FetchString(ctx, fmt.Sprintf("%s/%s-detay", base, slug), DefaultHeaders(base+"/"))
	if err != nil {
		return nil, err
	}

	title := slug
	if m := regexp.MustCompile(`(?is)<h1[^>]*>(.*?)</h1>`).FindStringSubmatch(htmlStr); len(m) > 1 {
		title = CleanMediaTitle(m[1])
	}

	seasonMap := make(map[int][]Episode)
	if m := puhuNextDataRe.FindStringSubmatch(htmlStr); len(m) > 1 {
		var root map[string]any
		if json.Unmarshal([]byte(m[1]), &root) == nil {
			seenVid := make(map[string]bool)
			var walk func(node any)
			walk = func(node any) {
				switch v := node.(type) {
				case map[string]any:
					vid, _ := v["video_id"].(string)
					epName, _ := v["name"].(string)
					if vid != "" && !seenVid[vid] {
						seenVid[vid] = true
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

	if len(seasonMap) == 0 {
		return &MediaDetail{
			MediaItem: MediaItem{
				ID:    slug,
				Title: title,
				Type:  MediaTypeMovie,
			},
			StreamID: slug,
		}, nil
	}

	return &MediaDetail{
		MediaItem: MediaItem{
			ID:    slug,
			Title: title,
			Type:  MediaTypeSeries,
		},
		Seasons: []Season{
			{
				Number:   1,
				Name:     "Season 01",
				Episodes: seasonMap[1],
			},
		},
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
