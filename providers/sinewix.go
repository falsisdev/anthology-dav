package providers

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"regexp"
	"strings"

	"anthology-dav/config"
)

const (
	sinewixKey       = "9iQNC5HQwPlaFuJDkhncJ5XTJ8feGXOJatAA"
	sinewixHash256   = "711bff4afeb47f07ab08a0b07e85d3835e739295e8a6361db77eebd93d96306b"
	sinewixSignature = "3082058830820370a00302010202145bbfbba9791db758ad12295636e094ab4b07dc24300d06092a864886f70d01010b05003074310b3009060355040613025553311330110603550408130a43616c69666f726e6961311630140603550407130d4d6f756e7461696e205669657731143012060355040a130b476f6f676c6520496e632e3110300e060355040b1307416e64726f69643110300e06035504031307416e64726f69643020170d3231313231353232303433335a180f32303531313231353232303433335a3074310b3009060355040613025553311330110603550408130a43616c69666f726e6961311630140603550407130d4d6f756e7461696e205669657731143012060355040a130b476f6f676c6520496e632e3110300e060355040b1307416e64726f69643110300e06035504031307416e64726f696430820222300d06092a864886f70d01010105000382020f003082020a0282020100a5106a24bb3f9c0aaf3a2b228f794b5eaf1757ba758b19736a39d1bdc73fc983a7237b8d5ca5156cfa999c1dab3418bbc2be0920e0ee001c8aa4812d1dae75d080f09e91e0abda83ff9a76e8384a4429f4849248069a59505b12ac2c14ba2e4d1a13afcdaf54e508697ff928a9f738e6f4a6fc27409c55329eb149b5ff89c5a2d7c06bf9e62086f955cad17d7be2623ee9d5ec56068eadc23cb0965a13ff97d49fe10ef41afc6eeca36b4ace9582097faff89f590bc831cdb3a69eec5d15b67c3f2cad49e37ed053733e3d2d400c47755b932bdbe15d749fd6ad1dce30ba5e66094dfb6ee6f64cafb807e11b19a990c5d078c6d6701cda0bdeb21e99404ff166074f4c89b04c418f4e7940db5c78647c475bcfb85d4c4e836ee7d7c1d53e9e736b5d96d4b4d8b98209064b729ac6a682d55a6a930e518d849898bb28329ca0aaa133b5e5270a9d5940cac6af4802a57fd971efda91abb602882dd6aa6ce2b236b57b52ee2481498f0cacbcc2c36c238bc84becad7eaaf1125b9a1ca9ded6c79f3f283a52050377809b2a9995d66e1636b0ed426fdd8685c47cb18e82077f4aefcc07887e1dc58b4d64be1632f0e7b4625da6f40c65a8512a6454a4b96963e7f876136e6c0069a519a79ad632078ed965aa12482458060c030ed50db706d854f88cb004630b49285d8af8b471ff8f6070687826412287b50049bcb7d1b6b62ef90203010001a310300e300c0603551d13040530030101ff300d06092a864886f70d01010b0500038202010051c0b7bd793181dc29ca777d3773f928a366c8469ecf2fa3cfb076e8831970d19bb2b96e44e8ccc647cf0696bb824ac61c23d958525d283cab26037b04d58aa79bf92192db843adf5c26a980f081d2f0e14f759fc5ff4c5bb3dce0860299bfe7b349a8155a2efaf731ba25ce796a80c1442c7bf80f8c1a7912ff0b6f6592264315337251a846460194fa594f81f38f9e5233a63201e931ad9cab5bf119f24025613f307194eaa6eb39a83f3c05a49ba34455b1aff7c6839bbb657d9392ffdf397432af6e56ba9534a8b07d7060fe09691c6cf07cb5324f67b3cc0871a8c621d81fe71d71085c55206a4f57e25f774fd4b979b299e8bb076b50fca42fa57da2d519fd35a4a7c0137babaed4345f8031b63b6a71f5e8268f709d658ccd7c2a58849379d25bfa598c3f4a2c3d9b7d89285fefeb7f0ec65137d38b08ce432a15688b624a179e6a4a505ebc3bcdfbc4d4330508ee2d8d0f016924dcec21a6838ef7d834c6f43bde4a5201ed0b3bb4e9bd377b470e36bcf5bc3d56169dbd8e39567aa7dce4d1a8a8a54a5e1aa6fb1a8aab0062669a966f96e15ccce6fe12ea5e6a8b8c8823bdc94988ca39759fd1cc8fd8ae5c3d74db50b174cf7d77655016c075c91d439ed01cc0a9f695c99fad3b5495fb6cb1e01a5fa020cc6022a85c07ec55f9eba89719f86e49d34ab5bd208c5f70cced2b7b7963c014f8404432979b506de29e"
	sinewixUA        = "EasyPlex (Android 14; SM-A546B; Samsung Galaxy A54 5G; tr)"
)

type SineWixProvider struct{}

func init() {
	Register(&SineWixProvider{})
}

func (p *SineWixProvider) ID() string   { return "sinewix" }
func (p *SineWixProvider) Name() string { return "SineWix" }

func (p *SineWixProvider) baseURL() string {
	return config.RemoteString("urls.movies.sinewix.api_base", config.RemoteString("urls.movies.sinewix", "https://ydfvfdizipanel.ru/public/api"))
}

func (p *SineWixProvider) headers() map[string]string {
	return map[string]string{
		"User-Agent": sinewixUA,
		"Accept":     "application/json",
		"hash256":    sinewixHash256,
		"signature":  sinewixSignature,
	}
}

func (p *SineWixProvider) GetCatalog(ctx context.Context, page int) ([]MediaItem, error) {
	var items []MediaItem
	seriesURL := fmt.Sprintf("%s/series/popular/%s", p.baseURL(), sinewixKey)
	if body, _, err := FetchString(ctx, seriesURL, p.headers()); err == nil {
		items = append(items, p.parseItems(body, MediaTypeSeries)...)
	}
	moviesURL := fmt.Sprintf("%s/genres/movies/all/%s", p.baseURL(), sinewixKey)
	if body, _, err := FetchString(ctx, moviesURL, p.headers()); err == nil {
		items = append(items, p.parseItems(body, MediaTypeMovie)...)
	}
	return items, nil
}

func (p *SineWixProvider) Search(ctx context.Context, query string) ([]MediaItem, error) {
	u := fmt.Sprintf("%s/search/%s/%s", p.baseURL(), url.PathEscape(query), sinewixKey)
	body, _, err := FetchString(ctx, u, p.headers())
	if err != nil {
		return nil, err
	}
	return p.parseItems(body, MediaTypeMovie), nil
}

func (p *SineWixProvider) parseItems(jsonStr string, defaultType MediaType) []MediaItem {
	var items []MediaItem
	var wrapper struct {
		Data   []map[string]any `json:"data"`
		Search []map[string]any `json:"search"`
	}
	var list []map[string]any
	if json.Unmarshal([]byte(jsonStr), &wrapper) == nil {
		list = append(wrapper.Data, wrapper.Search...)
	} else {
		_ = json.Unmarshal([]byte(jsonStr), &list)
	}
	for _, raw := range list {
		idStr := fmt.Sprintf("%v", raw["id"])
		title, _ := raw["title"].(string)
		if title == "" {
			title, _ = raw["name"].(string)
		}
		if idStr == "" || title == "" {
			continue
		}
		mType := defaultType
		if tStr, ok := raw["type"].(string); ok {
			if strings.Contains(strings.ToLower(tStr), "serie") || tStr == "1" {
				mType = MediaTypeSeries
			} else if strings.Contains(strings.ToLower(tStr), "anime") {
				mType = MediaTypeAnime
			}
		}
		if _, hasSeasons := raw["seasons"]; hasSeasons {
			mType = MediaTypeSeries
		}
		prefix := "movie"
		if mType == MediaTypeSeries || mType == MediaTypeAnime {
			prefix = "series"
		}
		poster, _ := raw["poster_path"].(string)
		releaseDate, _ := raw["release_date"].(string)
		if releaseDate == "" {
			releaseDate, _ = raw["first_air_date"].(string)
		}
		items = append(items, MediaItem{
			ID:        fmt.Sprintf("%s:%s", prefix, idStr),
			Title:     CleanMediaTitle(title),
			Year:      ExtractYear(releaseDate),
			Type:      mType,
			PosterURL: poster,
		})
	}
	return items
}

func (p *SineWixProvider) GetDetails(ctx context.Context, mediaID string) (*MediaDetail, error) {
	parts := strings.SplitN(mediaID, ":", 2)
	kind := "movie"
	rawID := mediaID
	if len(parts) == 2 {
		kind = parts[0]
		rawID = parts[1]
	}

	if kind == "series" {
		u := fmt.Sprintf("%s/series/show/%s/%s", p.baseURL(), rawID, sinewixKey)
		body, _, err := FetchString(ctx, u, p.headers())
		if err != nil {
			return nil, err
		}
		var data struct {
			ID           int    `json:"id"`
			Name         string `json:"name"`
			Title        string `json:"title"`
			PosterPath   string `json:"poster_path"`
			Overview     string `json:"overview"`
			FirstAirDate string `json:"first_air_date"`
			Seasons      []struct {
				SeasonNumber int `json:"season_number"`
				Episodes     []struct {
					ID            int    `json:"id"`
					EpisodeNumber int    `json:"episode_number"`
					Name          string `json:"name"`
					Videos        []struct {
						Link   string `json:"link"`
						Server string `json:"server"`
						Lang   string `json:"lang"`
					} `json:"videos"`
				} `json:"episodes"`
			} `json:"seasons"`
		}
		if err := json.Unmarshal([]byte(body), &data); err != nil {
			return nil, err
		}
		title := data.Name
		if title == "" {
			title = data.Title
		}
		var seasons []Season
		for _, s := range data.Seasons {
			sNum := s.SeasonNumber
			if sNum <= 0 {
				sNum = 1
			}
			var eps []Episode
			for _, ep := range s.Episodes {
				eNum := ep.EpisodeNumber
				if eNum <= 0 {
					eNum = len(eps) + 1
				}
				linksJSON, _ := json.Marshal(ep.Videos)
				streamToken := fmt.Sprintf("videos:%s", string(linksJSON))
				eps = append(eps, Episode{
					ID:       fmt.Sprintf("%d", ep.ID),
					StreamID: streamToken,
					Title:    ep.Name,
					Season:   sNum,
					Number:   eNum,
				})
			}
			if len(eps) > 0 {
				seasons = append(seasons, Season{
					Number:   sNum,
					Name:     fmt.Sprintf("Season %02d", sNum),
					Episodes: eps,
				})
			}
		}
		return &MediaDetail{
			MediaItem: MediaItem{
				ID:          mediaID,
				Title:       CleanMediaTitle(title),
				Year:        ExtractYear(data.FirstAirDate),
				Type:        MediaTypeSeries,
				PosterURL:   data.PosterPath,
				Description: data.Overview,
			},
			Seasons: seasons,
		}, nil
	}

	u := fmt.Sprintf("%s/media/detail/%s/%s", p.baseURL(), rawID, sinewixKey)
	body, _, err := FetchString(ctx, u, p.headers())
	if err != nil {
		return nil, err
	}
	var data struct {
		ID          int    `json:"id"`
		Title       string `json:"title"`
		PosterPath  string `json:"poster_path"`
		Overview    string `json:"overview"`
		ReleaseDate string `json:"release_date"`
		Videos      []struct {
			Link   string `json:"link"`
			Server string `json:"server"`
			Lang   string `json:"lang"`
		} `json:"videos"`
	}
	if err := json.Unmarshal([]byte(body), &data); err != nil {
		return nil, err
	}
	linksJSON, _ := json.Marshal(data.Videos)
	return &MediaDetail{
		MediaItem: MediaItem{
			ID:          mediaID,
			Title:       CleanMediaTitle(data.Title),
			Year:        ExtractYear(data.ReleaseDate),
			Type:        MediaTypeMovie,
			PosterURL:   data.PosterPath,
			Description: data.Overview,
		},
		StreamID: fmt.Sprintf("videos:%s", string(linksJSON)),
	}, nil
}

func (p *SineWixProvider) GetEpisodes(ctx context.Context, mediaID string, season int) ([]Episode, error) {
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

func (p *SineWixProvider) ResolveStream(ctx context.Context, streamID string) (*StreamResult, error) {
	if !strings.HasPrefix(streamID, "videos:") {
		detail, err := p.GetDetails(ctx, streamID)
		if err != nil {
			return nil, err
		}
		streamID = detail.StreamID
	}
	jsonPart := strings.TrimPrefix(streamID, "videos:")
	var videos []struct {
		Link   string `json:"link"`
		Server string `json:"server"`
		Lang   string `json:"lang"`
	}
	if err := json.Unmarshal([]byte(jsonPart), &videos); err != nil {
		return nil, err
	}

	var sources []StreamSource
	reMF := regexp.MustCompile(`href="(https?://download\d+\.mediafire\.com/[^"]+)"`)
	for _, v := range videos {
		link := strings.TrimSpace(v.Link)
		if link == "" {
			continue
		}
		if strings.Contains(link, "mediafire.com") {
			mfHTML, _, err := FetchString(ctx, link, nil)
			if err == nil {
				if m := reMF.FindStringSubmatch(mfHTML); len(m) > 1 {
					link = m[1]
				}
			}
		}
		isHLS := strings.Contains(link, ".m3u8")
		format := "mp4"
		if isHLS {
			format = "hls"
		}
		sources = append(sources, StreamSource{
			Name:    "SineWix",
			Title:   fmt.Sprintf("SineWix %s (%s)", v.Server, v.Lang),
			URL:     link,
			Quality: v.Server,
			Format:  format,
			IsHLS:   isHLS,
			Headers: map[string]string{
				"Referer":    "https://ydfvfdizipanel.ru/",
				"User-Agent": sinewixUA,
			},
		})
	}
	return SelectBestStream(sources)
}
