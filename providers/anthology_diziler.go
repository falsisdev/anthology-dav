package providers

import (
	"context"
	"fmt"
	"regexp"
	"strconv"
	"strings"

	"anthology-dav/config"
)

type AnthologyDizilerProvider struct{}

func init() {
	Register(&AnthologyDizilerProvider{})
}

func (p *AnthologyDizilerProvider) ID() string   { return "anthology_diziler" }
func (p *AnthologyDizilerProvider) Name() string { return "Anthology_Diziler" }

type officialTurkishShow struct {
	ID      string
	Title   string
	Year    string
	Network string
	Slug    string
}

var curatedTurkishShows = []officialTurkishShow{
	{ID: "showtv:kizilcik-serbeti", Title: "Kızılcık Şerbeti", Year: "2022", Network: "Show TV", Slug: "kizilcik-serbeti"},
	{ID: "showtv:bahar", Title: "Bahar", Year: "2024", Network: "Show TV", Slug: "bahar"},
	{ID: "showtv:deha", Title: "Deha", Year: "2024", Network: "Show TV", Slug: "deha"},
	{ID: "showtv:siyah-kalp", Title: "Siyah Kalp", Year: "2024", Network: "Show TV", Slug: "siyah-kalp"},
	{ID: "showtv:cukur", Title: "Çukur", Year: "2017", Network: "Show TV", Slug: "cukur"},
	{ID: "nowtv:hudutsuz-sevda", Title: "Hudutsuz Sevda", Year: "2023", Network: "NOW TV", Slug: "Hudutsuz-Sevda"},
	{ID: "nowtv:kizil-goncalar", Title: "Kızıl Goncalar", Year: "2023", Network: "NOW TV", Slug: "Kizil-Goncalar"},
	{ID: "nowtv:leyla-hayat-ask-adalet", Title: "Leyla: Hayat... Aşk... Adalet...", Year: "2024", Network: "NOW TV", Slug: "Leyla-Hayat-Ask-Adalet"},
	{ID: "kanald:uzak-sehir", Title: "Uzak Şehir", Year: "2024", Network: "Kanal D", Slug: "uzak-sehir"},
	{ID: "kanald:inci-taneleri", Title: "İnci Taneleri", Year: "2024", Network: "Kanal D", Slug: "inci-taneleri"},
	{ID: "kanald:arka-sokaklar", Title: "Arka Sokaklar", Year: "2006", Network: "Kanal D", Slug: "arka-sokaklar"},
	{ID: "startv:sahipsizler", Title: "Sahipsizler", Year: "2024", Network: "Star TV", Slug: "sahipsizler"},
	{ID: "startv:yali-capkini", Title: "Yalı Çapkını", Year: "2022", Network: "Star TV", Slug: "yali-capkini"},
	{ID: "atv:kurulus-osman", Title: "Kuruluş Osman", Year: "2019", Network: "ATV", Slug: "kurulus-osman"},
	{ID: "atv:bir-gece-masali", Title: "Bir Gece Masalı", Year: "2024", Network: "ATV", Slug: "bir-gece-masali"},
	{ID: "trt1:gonul-dagi", Title: "Gönül Dağı", Year: "2020", Network: "TRT 1", Slug: "gonul-dagi"},
	{ID: "trt1:teskilat", Title: "Teşkilat", Year: "2021", Network: "TRT 1", Slug: "teskilat"},
	{ID: "trt1:mehmed-fetihler-sultani", Title: "Mehmed: Fetihler Sultanı", Year: "2024", Network: "TRT 1", Slug: "mehmed-fetihler-sultani"},
}

func (p *AnthologyDizilerProvider) GetCatalog(ctx context.Context, page int) ([]MediaItem, error) {
	var items []MediaItem
	for _, s := range curatedTurkishShows {
		items = append(items, MediaItem{
			ID:          s.ID,
			Title:       s.Title,
			Year:        s.Year,
			Type:        MediaTypeSeries,
			Description: fmt.Sprintf("Resmî Yayıncı: %s", s.Network),
		})
	}
	return items, nil
}

func (p *AnthologyDizilerProvider) Search(ctx context.Context, query string) ([]MediaItem, error) {
	q := UltraClean(query)
	var items []MediaItem
	for _, s := range curatedTurkishShows {
		if strings.Contains(UltraClean(s.Title), q) || strings.Contains(UltraClean(s.Slug), q) {
			items = append(items, MediaItem{
				ID:          s.ID,
				Title:       s.Title,
				Year:        s.Year,
				Type:        MediaTypeSeries,
				Description: fmt.Sprintf("Resmî Yayıncı: %s", s.Network),
			})
		}
	}
	if len(items) == 0 {
		items = append(items, MediaItem{
			ID:    "yt:" + strings.TrimSpace(query),
			Title: strings.TrimSpace(query),
			Type:  MediaTypeSeries,
		})
	}
	return items, nil
}

func (p *AnthologyDizilerProvider) GetDetails(ctx context.Context, mediaID string) (*MediaDetail, error) {
	title := mediaID
	year := ""
	for _, s := range curatedTurkishShows {
		if s.ID == mediaID {
			title = s.Title
			year = s.Year
			break
		}
	}
	if strings.HasPrefix(title, "yt:") {
		title = strings.TrimPrefix(title, "yt:")
	}

	var eps []Episode
	for ep := 1; ep <= 30; ep++ {
		eps = append(eps, Episode{
			ID:       fmt.Sprintf("%s:1:%d", mediaID, ep),
			StreamID: fmt.Sprintf("%s|%s|1|%d", mediaID, title, ep),
			Title:    fmt.Sprintf("%d. Bölüm", ep),
			Season:   1,
			Number:   ep,
		})
	}
	return &MediaDetail{
		MediaItem: MediaItem{
			ID:    mediaID,
			Title: title,
			Year:  year,
			Type:  MediaTypeSeries,
		},
		Seasons: []Season{
			{
				Number:   1,
				Name:     "Season 01",
				Episodes: eps,
			},
		},
	}, nil
}

func (p *AnthologyDizilerProvider) GetEpisodes(ctx context.Context, mediaID string, season int) ([]Episode, error) {
	det, err := p.GetDetails(ctx, mediaID)
	if err != nil {
		return nil, err
	}
	if len(det.Seasons) > 0 {
		return det.Seasons[0].Episodes, nil
	}
	return nil, nil
}

var showTVHopeVideoRe = regexp.MustCompile(`(?is)data-hope-video=['"]([^'"]+)['"]`)

func (p *AnthologyDizilerProvider) ResolveStream(ctx context.Context, streamID string) (*StreamResult, error) {
	parts := strings.Split(streamID, "|")
	mediaID := parts[0]
	title := mediaID
	epNum := 1
	if len(parts) >= 4 {
		title = parts[1]
		epNum, _ = strconv.Atoi(parts[3])
	}
	if epNum <= 0 {
		epNum = 1
	}

	var sources []StreamSource

	// 1. Try Show TV direct CDN if Show TV series
	if strings.HasPrefix(mediaID, "showtv:") {
		slug := strings.TrimPrefix(mediaID, "showtv:")
		epPage := fmt.Sprintf("https://www.showtv.com.tr/dizi/tum_bolumler/%s", slug)
		if htmlStr, _, err := FetchString(ctx, epPage, DefaultHeaders("https://www.showtv.com.tr/")); err == nil {
			epLinkRe := regexp.MustCompile(fmt.Sprintf(`(?i)href="(/dizi/tanitim/%s/[^"]*bolum-%d-[^"]*|/dizi/tum_bolumler/%s[^"]*%d[^"]*)"`, regexp.QuoteMeta(slug), epNum, regexp.QuoteMeta(slug), epNum))
			if m := epLinkRe.FindStringSubmatch(htmlStr); len(m) > 1 {
				if epHTML, _, err2 := FetchString(ctx, "https://www.showtv.com.tr"+m[1], DefaultHeaders(epPage)); err2 == nil {
					if hm := showTVHopeVideoRe.FindStringSubmatch(epHTML); len(hm) > 1 {
						for _, u := range genericM3U8Re.FindAllString(strings.ReplaceAll(hm[1], `\/`, `/`), -1) {
							sources = append(sources, StreamSource{
								Name:    "Show TV",
								Title:   fmt.Sprintf("%s %d. Bölüm • Show TV 1080p", title, epNum),
								URL:     u,
								Quality: "1080p",
								Format:  "hls",
								IsHLS:   true,
								Headers: map[string]string{
									"User-Agent": config.DefaultUserAgent,
									"Referer":    "https://www.showtv.com.tr/",
								},
							})
						}
					}
				}
			}
		}
	}

	// 2. Official YouTube full episode resolution via Innertube
	searchQuery := fmt.Sprintf("%s %d. Bölüm Full İzle", title, epNum)
	ytResults := SearchYouTubeInnertube(ctx, searchQuery)
	for _, v := range ytResults {
		if v.DurationSec > 0 && v.DurationSec < 900 {
			continue
		}
		if s := ExtractYouTubeInnertube(ctx, v.VideoID); s != nil {
			s.Name = "Anthology Diziler"
			s.Title = fmt.Sprintf("%s %d. Bölüm • %s (%s)", title, epNum, v.Channel, s.Quality)
			sources = append(sources, *s)
			if len(sources) >= 2 {
				break
			}
		}
	}

	return SelectBestStream(sources)
}
