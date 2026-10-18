package providers

import (
	"context"
	"fmt"
	"strings"
)

type YouTubeDiziFilmProvider struct{}

func init() {
	Register(&YouTubeDiziFilmProvider{})
}

func (p *YouTubeDiziFilmProvider) ID() string   { return "youtube_dizifilm" }
func (p *YouTubeDiziFilmProvider) Name() string { return "YouTube_Dizi_Film" }

var youtubeCuratedCatalog = []MediaItem{
	{ID: "yt_series:Kurtlar Vadisi", Title: "Kurtlar Vadisi", Year: "2003", Type: MediaTypeSeries},
	{ID: "yt_series:Ezel", Title: "Ezel", Year: "2009", Type: MediaTypeSeries},
	{ID: "yt_series:Aşk-ı Memnu", Title: "Aşk-ı Memnu", Year: "2008", Type: MediaTypeSeries},
	{ID: "yt_series:Avrupa Yakası", Title: "Avrupa Yakası", Year: "2004", Type: MediaTypeSeries},
	{ID: "yt_series:Behzat Ç.", Title: "Behzat Ç.", Year: "2010", Type: MediaTypeSeries},
	{ID: "yt_series:Poyraz Karayel", Title: "Poyraz Karayel", Year: "2015", Type: MediaTypeSeries},
	{ID: "yt_series:Suskunlar", Title: "Suskunlar", Year: "2012", Type: MediaTypeSeries},
	{ID: "yt_series:İçerde", Title: "İçerde", Year: "2016", Type: MediaTypeSeries},
	{ID: "yt_series:Çukur", Title: "Çukur", Year: "2017", Type: MediaTypeSeries},
	{ID: "yt_series:Kuzey Güney", Title: "Kuzey Güney", Year: "2011", Type: MediaTypeSeries},
	{ID: "yt_series:Yargı", Title: "Yargı", Year: "2021", Type: MediaTypeSeries},
	{ID: "yt_series:Teşkilat", Title: "Teşkilat", Year: "2021", Type: MediaTypeSeries},
	{ID: "yt_series:Gönül Dağı", Title: "Gönül Dağı", Year: "2020", Type: MediaTypeSeries},
	{ID: "yt_movie:Hababam Sınıfı", Title: "Hababam Sınıfı", Year: "1975", Type: MediaTypeMovie},
	{ID: "yt_movie:Süt Kardeşler", Title: "Süt Kardeşler", Year: "1976", Type: MediaTypeMovie},
	{ID: "yt_movie:Tosun Paşa", Title: "Tosun Paşa", Year: "1976", Type: MediaTypeMovie},
	{ID: "yt_movie:Şaban Oğlu Şaban", Title: "Şaban Oğlu Şaban", Year: "1977", Type: MediaTypeMovie},
	{ID: "yt_movie:Kibar Feyzo", Title: "Kibar Feyzo", Year: "1978", Type: MediaTypeMovie},
	{ID: "yt_movie:Çöpçüler Kralı", Title: "Çöpçüler Kralı", Year: "1977", Type: MediaTypeMovie},
	{ID: "yt_movie:Züğürt Ağa", Title: "Züğürt Ağa", Year: "1985", Type: MediaTypeMovie},
	{ID: "yt_movie:Eşkıya", Title: "Eşkıya", Year: "1996", Type: MediaTypeMovie},
	{ID: "yt_movie:G.O.R.A.", Title: "G.O.R.A.", Year: "2004", Type: MediaTypeMovie},
	{ID: "yt_movie:A.R.O.G", Title: "A.R.O.G", Year: "2008", Type: MediaTypeMovie},
	{ID: "yt_movie:Vizontele", Title: "Vizontele", Year: "2001", Type: MediaTypeMovie},
}

func (p *YouTubeDiziFilmProvider) GetCatalog(ctx context.Context, page int) ([]MediaItem, error) {
	return youtubeCuratedCatalog, nil
}

func (p *YouTubeDiziFilmProvider) Search(ctx context.Context, query string) ([]MediaItem, error) {
	results := SearchYouTubeInnertube(ctx, query+" Full Tek Parça")
	var items []MediaItem
	for _, r := range results {
		title := CleanMediaTitle(r.Title)
		if title == "" {
			title = r.VideoID
		}
		items = append(items, MediaItem{
			ID:    "yt_vid:" + r.VideoID,
			Title: title,
			Year:  ExtractYear(r.Title),
			Type:  MediaTypeMovie,
		})
	}
	return items, nil
}

func (p *YouTubeDiziFilmProvider) GetDetails(ctx context.Context, mediaID string) (*MediaDetail, error) {
	if strings.HasPrefix(mediaID, "yt_series:") {
		title := strings.TrimPrefix(mediaID, "yt_series:")
		var eps []Episode
		for e := 1; e <= 30; e++ {
			token := fmt.Sprintf("yt_q:%s %d. Bölüm Full", title, e)
			eps = append(eps, Episode{
				ID:       token,
				StreamID: token,
				Title:    fmt.Sprintf("%d. Bölüm", e),
				Season:   1,
				Number:   e,
			})
		}
		return &MediaDetail{
			MediaItem: MediaItem{
				ID:    mediaID,
				Title: title,
				Type:  MediaTypeSeries,
			},
			Seasons: []Season{
				{Number: 1, Name: "Season 01", Episodes: eps},
			},
		}, nil
	}

	if strings.HasPrefix(mediaID, "yt_movie:") {
		title := strings.TrimPrefix(mediaID, "yt_movie:")
		return &MediaDetail{
			MediaItem: MediaItem{
				ID:    mediaID,
				Title: title,
				Type:  MediaTypeMovie,
			},
			StreamID: fmt.Sprintf("yt_q:%s Full Film Tek Parça", title),
		}, nil
	}

	return &MediaDetail{
		MediaItem: MediaItem{
			ID:    mediaID,
			Title: mediaID,
			Type:  MediaTypeMovie,
		},
		StreamID: mediaID,
	}, nil
}

func (p *YouTubeDiziFilmProvider) GetEpisodes(ctx context.Context, mediaID string, season int) ([]Episode, error) {
	detail, err := p.GetDetails(ctx, mediaID)
	if err != nil {
		return nil, err
	}
	for _, s := range detail.Seasons {
		if s.Number == season {
			return s.Episodes, nil
		}
	}
	return nil, nil
}

func (p *YouTubeDiziFilmProvider) ResolveStream(ctx context.Context, streamID string) (*StreamResult, error) {
	videoID := ""
	if strings.HasPrefix(streamID, "yt_vid:") {
		videoID = strings.TrimPrefix(streamID, "yt_vid:")
	} else {
		q := strings.TrimPrefix(streamID, "yt_q:")
		results := SearchYouTubeInnertube(ctx, q)
		if len(results) == 0 {
			return nil, fmt.Errorf("youtube_dizifilm: no videos found for %s", q)
		}
		videoID = results[0].VideoID
	}

	src := ExtractYouTubeInnertube(ctx, videoID)
	if src == nil {
		return nil, fmt.Errorf("youtube_dizifilm: failed to resolve video %s", videoID)
	}
	return SelectBestStream([]StreamSource{*src})
}
