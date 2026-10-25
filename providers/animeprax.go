package providers

import (
	"context"
	"fmt"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"

	"anthology-dav/config"
)

type AnimePraxProvider struct{}

func init() {
	Register(&AnimePraxProvider{})
}

func (p *AnimePraxProvider) ID() string   { return "animeprax" }
func (p *AnimePraxProvider) Name() string { return "AnimePrax" }

func (p *AnimePraxProvider) baseURL() string {
	return config.RemoteString("urls.anime.animeprax.base", "https://animeprax.com")
}

var (
	animePraxCardRe   = regexp.MustCompile(`(?is)<article[^>]*class="[^"]*anime-card[^"]*"[^>]*>(.*?)</article>`)
	animePraxHrefRe   = regexp.MustCompile(`(?i)href="([^"]*/anime/([^"/?#]+))"`)
	animePraxTitleRe  = regexp.MustCompile(`(?is)<h3[^>]*>(.*?)</h3>`)
	animePraxImgRe    = regexp.MustCompile(`(?i)<img[^>]+src="([^"]+)"`)
	animePraxEpLinkRe = regexp.MustCompile(`(?i)href="[^"]*/anime/[^/]+/season/(\d+)/episode/(\d+)"`)
	animePraxTeamIDRe = regexp.MustCompile(`(?i)data-team-id="(\d+)"`)
)

func (p *AnimePraxProvider) GetCatalog(ctx context.Context, page int) ([]MediaItem, error) {
	base := p.baseURL()
	htmlStr, _, err := FetchString(ctx, base+"/", DefaultHeaders(base+"/"))
	if err != nil {
		return nil, err
	}
	var items []MediaItem
	seen := make(map[string]bool)
	for _, m := range animePraxCardRe.FindAllStringSubmatch(htmlStr, -1) {
		card := m[1]
		hm := animePraxHrefRe.FindStringSubmatch(card)
		if len(hm) < 3 {
			continue
		}
		slug := hm[2]
		if seen[slug] {
			continue
		}
		seen[slug] = true
		title := slug
		if tm := animePraxTitleRe.FindStringSubmatch(card); len(tm) > 1 {
			title = CleanHTMLText(tm[1])
		}
		poster := ""
		if im := animePraxImgRe.FindStringSubmatch(card); len(im) > 1 {
			poster = ResolveRelativeURL(im[1], base)
		}
		items = append(items, MediaItem{
			ID:        slug,
			Title:     title,
			Type:      MediaTypeAnime,
			PosterURL: poster,
		})
	}
	return items, nil
}

func (p *AnimePraxProvider) Search(ctx context.Context, query string) ([]MediaItem, error) {
	base := p.baseURL()
	u := fmt.Sprintf("%s/api/search?q=%s", base, url.QueryEscape(strings.TrimSpace(query)))
	var results []struct {
		Slug          string `json:"slug"`
		Title         string `json:"title"`
		CoverImageURL string `json:"cover_image_url"`
	}
	if err := FetchJSON(ctx, u, map[string]string{
		"User-Agent": config.DefaultUserAgent,
		"Accept":     "application/json",
		"Referer":    base + "/",
	}, &results); err != nil {
		return nil, err
	}
	var items []MediaItem
	for _, r := range results {
		if r.Slug == "" {
			continue
		}
		title := r.Title
		if title == "" {
			title = r.Slug
		}
		poster := r.CoverImageURL
		if poster != "" && !strings.HasPrefix(poster, "http") {
			poster = ResolveRelativeURL(poster, base)
		}
		items = append(items, MediaItem{
			ID:        r.Slug,
			Title:     CleanHTMLText(title),
			Type:      MediaTypeAnime,
			PosterURL: poster,
		})
	}
	return items, nil
}

func (p *AnimePraxProvider) GetDetails(ctx context.Context, mediaID string) (*MediaDetail, error) {
	slug := strings.TrimPrefix(mediaID, "animeprax:")
	base := p.baseURL()
	htmlStr, _, err := FetchString(ctx, fmt.Sprintf("%s/anime/%s", base, slug), DefaultHeaders(base+"/"))
	if err != nil {
		return nil, err
	}
	title := slug
	if m := regexp.MustCompile(`(?is)<h1[^>]*>(.*?)</h1>`).FindStringSubmatch(htmlStr); len(m) > 1 {
		title = CleanHTMLText(m[1])
	}

	seasonMap := make(map[int][]Episode)
	seen := make(map[string]bool)
	for _, m := range animePraxEpLinkRe.FindAllStringSubmatch(htmlStr, -1) {
		sNum, _ := strconv.Atoi(m[1])
		eNum, _ := strconv.Atoi(m[2])
		if sNum <= 0 {
			sNum = 1
		}
		if eNum <= 0 {
			continue
		}
		key := fmt.Sprintf("%d_%d", sNum, eNum)
		if seen[key] {
			continue
		}
		seen[key] = true
		seasonMap[sNum] = append(seasonMap[sNum], Episode{
			ID:       fmt.Sprintf("%s:%d:%d", slug, sNum, eNum),
			StreamID: fmt.Sprintf("%s:%d:%d", slug, sNum, eNum),
			Title:    fmt.Sprintf("%d. Sezon %d. Bölüm", sNum, eNum),
			Season:   sNum,
			Number:   eNum,
		})
	}
	if len(seasonMap) == 0 {
		seasonMap[1] = []Episode{
			{
				ID:       fmt.Sprintf("%s:1:1", slug),
				StreamID: fmt.Sprintf("%s:1:1", slug),
				Title:    "1. Sezon 1. Bölüm",
				Season:   1,
				Number:   1,
			},
		}
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
			Type:  MediaTypeAnime,
		},
		Seasons: seasons,
	}, nil
}

func (p *AnimePraxProvider) GetEpisodes(ctx context.Context, mediaID string, season int) ([]Episode, error) {
	det, err := p.GetDetails(ctx, mediaID)
	if err != nil {
		return nil, err
	}
	for _, s := range det.Seasons {
		if s.Number == season {
			return s.Episodes, nil
		}
	}
	if len(det.Seasons) > 0 {
		return det.Seasons[0].Episodes, nil
	}
	return nil, nil
}

func (p *AnimePraxProvider) ResolveStream(ctx context.Context, streamID string) (*StreamResult, error) {
	parts := strings.Split(strings.TrimPrefix(streamID, "animeprax:"), ":")
	slug := parts[0]
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

	base := p.baseURL()
	epURL := fmt.Sprintf("%s/anime/%s/season/%d/episode/%d", base, slug, season, episode)
	htmlStr, _, err := FetchString(ctx, epURL, DefaultHeaders(base+"/"))
	if err != nil {
		return nil, err
	}

	var teamIDs []string
	seenTeam := make(map[string]bool)
	for _, m := range animePraxTeamIDRe.FindAllStringSubmatch(htmlStr, -1) {
		if !seenTeam[m[1]] {
			seenTeam[m[1]] = true
			teamIDs = append(teamIDs, m[1])
		}
	}
	if len(teamIDs) == 0 {
		teamIDs = []string{"0"}
	}

	var sources []StreamSource
	hdrs := map[string]string{
		"User-Agent":       config.DefaultUserAgent,
		"Referer":          epURL,
		"X-Requested-With": "XMLHttpRequest",
		"Accept":           "application/json, text/plain, */*",
	}

	for _, tid := range teamIDs {
		srcURL := fmt.Sprintf("%s/anime/%s/season/%d/episode/%d/sources?team=%s&t=%d", base, slug, season, episode, tid, time.Now().UnixMilli())
		var resp struct {
			Sources []struct {
				ProviderName string `json:"provider_name"`
				EmbedURL     string `json:"embed_url"`
			} `json:"sources"`
		}
		if err := FetchJSON(ctx, srcURL, hdrs, &resp); err == nil {
			for _, s := range resp.Sources {
				embed := strings.TrimSpace(s.EmbedURL)
				if embed == "" {
					continue
				}
				if strings.HasPrefix(embed, "//") {
					embed = "https:" + embed
				}
				pName := strings.ToLower(s.ProviderName)
				lowerEmbed := strings.ToLower(embed)

				switch {
				case strings.Contains(pName, "sibnet") || strings.Contains(lowerEmbed, "sibnet.ru"):
					sources = append(sources, ExtractSibnet(ctx, embed, base+"/")...)
				case strings.Contains(pName, "ok") || strings.Contains(lowerEmbed, "ok.ru"):
					sources = append(sources, ExtractOkRu(ctx, embed)...)
				case strings.Contains(pName, "dailymotion") || strings.Contains(lowerEmbed, "dailymotion.com"):
					sources = append(sources, ExtractDailymotion(ctx, embed)...)
				default:
					sources = append(sources, ExtractAnyIframe(ctx, embed, epURL)...)
				}
			}
		}
		if len(sources) > 0 {
			break
		}
	}

	if len(sources) == 0 {
		return nil, fmt.Errorf("animeprax: no working streams found for '%s' S%02dE%02d", slug, season, episode)
	}

	return SelectBestStream(sources)
}
