package vfs

import (
	"context"
	"fmt"
	"io/fs"
	"net/url"
	"os"
	"path"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"golang.org/x/net/webdav"

	"anthology-dav/config"
	"anthology-dav/providers"
)

const (
	FolderMovies     = "Filmler"
	FolderSeries     = "Diziler"
	FolderAnime      = "Animeler"
	FolderSourcePick = "_Kaynak_Sec_"
	FolderLegacyAll  = "_Tum_Saglayicilar"
	FolderCatalog    = "Populer_ve_Vitrin"
	FolderSearch     = "Arama_Listesi"
	FolderWatchlist  = "Watchlist"
)

// VFSDiscoveredSource represents a verified working source returned by the stream resolver.
type VFSDiscoveredSource struct {
	ProviderID   string
	ProviderName string
	Quality      string
}

type contextKey string

const RequestHostKey contextKey = "anthology_request_host"

func (v *AnthologyVFS) baseURL(ctx context.Context) string {
	if ctx != nil {
		if h, ok := ctx.Value(RequestHostKey).(string); ok && h != "" {
			return "http://" + h
		}
	}
	return v.cfg.BaseURL
}

// SourceDiscoveryFunc is a callback used by VFS to discover working sources on demand without circular imports.
type SourceDiscoveryFunc func(ctx context.Context, mediaType string, tmdbID int, title, origTitle, year string, season, episode int) []VFSDiscoveredSource

// AnthologyVFS implements golang.org/x/net/webdav.FileSystem with on-demand lazy loading.
type AnthologyVFS struct {
	cfg        *config.Config
	wl         *config.WatchlistManager
	lib        *config.LibraryManager
	cache      *MemoryCache
	discoverFn SourceDiscoveryFunc
	bootTime   time.Time
	mu         sync.RWMutex
	itemIndex  map[string]indexedMedia
}

type indexedMedia struct {
	ProviderID string
	Item       providers.MediaItem
}

func NewAnthologyVFS(cfg *config.Config, cache *MemoryCache) *AnthologyVFS {
	return &AnthologyVFS{
		cfg:       cfg,
		wl:        config.Watchlist(),
		lib:       config.Library(),
		cache:     cache,
		bootTime:  time.Now(),
		itemIndex: make(map[string]indexedMedia),
	}
}

// SetSourceDiscoverer wires the MultiResolver discovery callback into the VFS.
func (v *AnthologyVFS) SetSourceDiscoverer(fn SourceDiscoveryFunc) {
	v.discoverFn = fn
}

func (v *AnthologyVFS) cleanSegments(name string) []string {
	cleaned := path.Clean("/" + strings.TrimSpace(name))
	cleaned = strings.TrimPrefix(cleaned, "/dav")
	cleaned = strings.Trim(cleaned, "/")
	if cleaned == "" || cleaned == "." {
		return nil
	}
	parts := strings.Split(cleaned, "/")
	var out []string
	for _, p := range parts {
		if p != "" && p != "." {
			out = append(out, p)
		}
	}
	return out
}

func isIgnoredMacFile(seg string) bool {
	return strings.HasPrefix(seg, "._") || seg == ".DS_Store" || seg == ".Spotlight-V100" || seg == ".Trashes" || seg == "Thumbs.db"
}

// Mkdir supports legacy MKCOL search or directory creation.
func (v *AnthologyVFS) Mkdir(ctx context.Context, name string, perm os.FileMode) error {
	segs := v.cleanSegments(name)
	if len(segs) == 2 && segs[0] == FolderWatchlist {
		query := strings.TrimSpace(segs[1])
		if query == "" || isIgnoredMacFile(query) {
			return os.ErrPermission
		}
		_ = v.wl.AddGlobalQuery(query)
		v.cache.DeletePrefix("watchlist:" + query)
		return nil
	}
	if len(segs) == 3 && segs[1] == FolderSearch {
		prov, ok := providers.GetByName(segs[0])
		if !ok {
			return os.ErrNotExist
		}
		query := strings.TrimSpace(segs[2])
		if query == "" || isIgnoredMacFile(query) {
			return os.ErrPermission
		}
		_ = v.wl.AddProviderQuery(prov.ID(), query)
		v.cache.DeletePrefix("search:" + prov.ID() + ":" + query)
		return nil
	}
	return os.ErrPermission
}

// RemoveAll removes a movie, series, or anime from the library when deleted via WebDAV (Finder, Explorer, media players).
func (v *AnthologyVFS) RemoveAll(ctx context.Context, name string) error {
	segs := v.cleanSegments(name)
	if len(segs) == 2 {
		rootCat := segs[0]
		folderName := segs[1]
		if rootCat == FolderMovies || rootCat == FolderSeries || rootCat == FolderAnime {
			mType := "movie"
			if rootCat == FolderSeries {
				mType = "series"
			} else if rootCat == FolderAnime {
				mType = "anime"
			}
			items := v.lib.GetByType(mType)
			for _, it := range items {
				if formatItemFolderName(it.Title, it.Year) == folderName || SanitizeFileName(it.Title) == folderName {
					_ = v.lib.Remove(it.ID)
					return nil
				}
			}
		}
		if rootCat == FolderWatchlist {
			_ = v.wl.RemoveGlobalQuery(folderName)
			v.cache.DeletePrefix("watchlist:" + folderName)
			return nil
		}
	}
	if len(segs) == 3 && segs[1] == FolderSearch {
		prov, ok := providers.GetByName(segs[0])
		if !ok {
			return os.ErrNotExist
		}
		_ = v.wl.RemoveProviderQuery(prov.ID(), segs[2])
		v.cache.DeletePrefix("search:" + prov.ID() + ":" + segs[2])
		return nil
	}
	return os.ErrPermission
}

func (v *AnthologyVFS) Rename(ctx context.Context, oldName, newName string) error {
	return os.ErrPermission
}

func (v *AnthologyVFS) Stat(ctx context.Context, name string) (os.FileInfo, error) {
	f, err := v.OpenFile(ctx, name, os.O_RDONLY, 0)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	return f.Stat()
}

func (v *AnthologyVFS) OpenFile(ctx context.Context, name string, flag int, perm os.FileMode) (webdav.File, error) {
	segs := v.cleanSegments(name)
	for _, s := range segs {
		if isIgnoredMacFile(s) {
			return nil, os.ErrNotExist
		}
	}

	// 1. Root directory: /dav/
	if len(segs) == 0 {
		children := []fs.FileInfo{
			NewDirFileInfo(FolderMovies, v.bootTime),
			NewDirFileInfo(FolderSeries, v.bootTime),
			NewDirFileInfo(FolderAnime, v.bootTime),
			NewDirFileInfo(FolderSourcePick, v.bootTime),
			NewDirFileInfo(FolderWatchlist, v.bootTime),
			NewDirFileInfo(FolderLegacyAll, v.bootTime),
		}
		return NewVirtualDir("/", children, v.bootTime), nil
	}

	root := segs[0]
	switch root {
	case FolderMovies:
		return v.handleLibraryMovies(ctx, segs[1:])
	case FolderSeries:
		return v.handleLibrarySeriesOrAnime(ctx, "series", FolderSeries, segs[1:])
	case FolderAnime:
		return v.handleLibrarySeriesOrAnime(ctx, "anime", FolderAnime, segs[1:])
	case FolderSourcePick:
		return v.handleSourcePicker(ctx, segs[1:])
	case FolderWatchlist:
		return v.handleWatchlist(ctx, segs[1:])
	case FolderLegacyAll:
		return v.handleLegacyAll(ctx, segs[1:])
	}

	// Fallback check for direct provider access
	if prov, ok := providers.GetByName(root); ok {
		return v.handleProviderRoot(ctx, prov, segs[1:])
	}

	return nil, os.ErrNotExist
}

// ── Library Movies Handler (/dav/Filmler/...) ──────────────────────────────────

func (v *AnthologyVFS) handleLibraryMovies(ctx context.Context, rest []string) (webdav.File, error) {
	movies := v.lib.GetByType("movie")

	if len(rest) == 0 {
		var children []fs.FileInfo
		for _, m := range movies {
			fName := formatItemFolderName(m.Title, m.Year)
			children = append(children, NewDirFileInfo(fName, v.bootTime))
		}
		return NewVirtualDir(FolderMovies, children, v.bootTime), nil
	}

	targetFolder := rest[0]
	var matchedMovie *config.LibraryItem
	for _, m := range movies {
		if formatItemFolderName(m.Title, m.Year) == targetFolder || SanitizeFileName(m.Title) == targetFolder {
			matchedMovie = &m
			break
		}
	}
	if matchedMovie == nil {
		return nil, os.ErrNotExist
	}

	baseName := formatItemFolderName(matchedMovie.Title, matchedMovie.Year)
	strmName := baseName + ".strm"
	strmContent := fmt.Sprintf("%s/stream/resolve?type=movie&tmdb_id=%d&title=%s&year=%s\n",
		v.baseURL(ctx), matchedMovie.TMDBID, url.QueryEscape(matchedMovie.Title), url.QueryEscape(matchedMovie.Year))
	strmFile := NewVirtualFile(strmName, []byte(strmContent), v.bootTime)

	if len(rest) == 1 {
		st, _ := strmFile.Stat()
		return NewVirtualDir(targetFolder, []fs.FileInfo{st}, v.bootTime), nil
	}

	if len(rest) == 2 && rest[1] == strmName {
		return strmFile, nil
	}

	return nil, os.ErrNotExist
}

// ── Library Series & Anime Handler (/dav/Diziler/... & /dav/Animeler/...) ─────

func (v *AnthologyVFS) ensureItemSeasons(ctx context.Context, item *config.LibraryItem) []config.LibrarySeason {
	if len(item.Seasons) > 0 {
		return item.Seasons
	}
	providers.PrewarmLibraryItemMetadata(ctx, item)
	if len(item.Seasons) > 0 {
		return item.Seasons
	}
	return []config.LibrarySeason{{SeasonNumber: 1, EpisodeCount: 12}}
}

func (v *AnthologyVFS) handleLibrarySeriesOrAnime(ctx context.Context, mediaType, rootFolder string, rest []string) (webdav.File, error) {
	items := v.lib.GetByType(mediaType)

	if len(rest) == 0 {
		var children []fs.FileInfo
		for _, it := range items {
			fName := formatItemFolderName(it.Title, it.Year)
			children = append(children, NewDirFileInfo(fName, v.bootTime))
		}
		return NewVirtualDir(rootFolder, children, v.bootTime), nil
	}

	targetFolder := rest[0]
	var matchedItem *config.LibraryItem
	for _, it := range items {
		if formatItemFolderName(it.Title, it.Year) == targetFolder || SanitizeFileName(it.Title) == targetFolder {
			matchedItem = &it
			break
		}
	}
	if matchedItem == nil {
		return nil, os.ErrNotExist
	}

	// Zero-network-latency season lookup from pre-cached library metadata
	seasons := v.ensureItemSeasons(ctx, matchedItem)

	if len(rest) == 1 {
		var children []fs.FileInfo
		for _, s := range seasons {
			if s.SeasonNumber <= 0 {
				continue
			}
			sFolder := fmt.Sprintf("Season %02d", s.SeasonNumber)
			children = append(children, NewDirFileInfo(sFolder, v.bootTime))
		}
		if len(children) == 0 {
			children = append(children, NewDirFileInfo("Season 01", v.bootTime))
		}
		return NewVirtualDir(targetFolder, children, v.bootTime), nil
	}

	seasonFolder := rest[1]
	seasonNum := parseSeasonFolderNumber(seasonFolder)
	if seasonNum <= 0 {
		return nil, os.ErrNotExist
	}

	// Zero-network-latency episode count lookup from pre-cached season metadata
	epCount := 0
	for _, s := range seasons {
		if s.SeasonNumber == seasonNum && s.EpisodeCount > 0 {
			epCount = s.EpisodeCount
			break
		}
	}
	if epCount <= 0 {
		epCount = 12
	}

	folderTitle := formatItemFolderName(matchedItem.Title, matchedItem.Year)
	plainTitle := SanitizeFileName(matchedItem.Title)

	filesMap := make(map[string]*VirtualFile, epCount)
	legacyMap := make(map[string]*VirtualFile, epCount)
	var orderedNames []string

	for epNum := 1; epNum <= epCount; epNum++ {
		strmContent := fmt.Sprintf("%s/stream/resolve?type=%s&tmdb_id=%d&title=%s&year=%s&season=%d&episode=%d\n",
			v.baseURL(ctx), mediaType, matchedItem.TMDBID, url.QueryEscape(matchedItem.Title), url.QueryEscape(matchedItem.Year), seasonNum, epNum)

		primaryName := SanitizeFileName(fmt.Sprintf("%s - S%02dE%02d", folderTitle, seasonNum, epNum)) + ".strm"
		vf := NewVirtualFile(primaryName, []byte(strmContent), v.bootTime)
		filesMap[primaryName] = vf
		orderedNames = append(orderedNames, primaryName)

		legacyName := SanitizeFileName(fmt.Sprintf("%s - S%02dE%02d", plainTitle, seasonNum, epNum)) + ".strm"
		if legacyName != primaryName {
			legacyMap[legacyName] = NewVirtualFile(legacyName, []byte(strmContent), v.bootTime)
		}
	}

	if len(rest) == 2 {
		children := make([]fs.FileInfo, 0, len(orderedNames))
		for _, fName := range orderedNames {
			st, _ := filesMap[fName].Stat()
			children = append(children, st)
		}
		return NewVirtualDir(seasonFolder, children, v.bootTime), nil
	}

	if len(rest) == 3 {
		if vf, ok := filesMap[rest[2]]; ok {
			return vf, nil
		}
		if vf, ok := legacyMap[rest[2]]; ok {
			return vf, nil
		}
		return nil, os.ErrNotExist
	}

	return nil, os.ErrNotExist
}

// ── On-Demand Working Source Picker (/dav/_Kaynak_Sec_/...) ───────────────────

func (v *AnthologyVFS) handleSourcePicker(ctx context.Context, rest []string) (webdav.File, error) {
	if len(rest) == 0 {
		children := []fs.FileInfo{
			NewDirFileInfo(FolderMovies, v.bootTime),
			NewDirFileInfo(FolderSeries, v.bootTime),
			NewDirFileInfo(FolderAnime, v.bootTime),
		}
		return NewVirtualDir(FolderSourcePick, children, v.bootTime), nil
	}

	cat := rest[0]
	var mediaType string
	switch cat {
	case FolderMovies:
		mediaType = "movie"
	case FolderSeries:
		mediaType = "series"
	case FolderAnime:
		mediaType = "anime"
	default:
		return nil, os.ErrNotExist
	}

	items := v.lib.GetByType(mediaType)
	if len(rest) == 1 {
		var children []fs.FileInfo
		for _, it := range items {
			children = append(children, NewDirFileInfo(formatItemFolderName(it.Title, it.Year), v.bootTime))
		}
		return NewVirtualDir(cat, children, v.bootTime), nil
	}

	targetFolder := rest[1]
	var matchedItem *config.LibraryItem
	for _, it := range items {
		if formatItemFolderName(it.Title, it.Year) == targetFolder || SanitizeFileName(it.Title) == targetFolder {
			matchedItem = &it
			break
		}
	}
	if matchedItem == nil {
		return nil, os.ErrNotExist
	}

	if mediaType == "movie" {
		filesMap, ordered := v.buildWorkingSourceFiles(ctx, matchedItem, "movie", 1, 1)
		if len(rest) == 2 {
			var children []fs.FileInfo
			for _, name := range ordered {
				st, _ := filesMap[name].Stat()
				children = append(children, st)
			}
			return NewVirtualDir(targetFolder, children, v.bootTime), nil
		}
		if len(rest) == 3 {
			if vf, ok := filesMap[rest[2]]; ok {
				return vf, nil
			}
		}
		return nil, os.ErrNotExist
	}

	// Series or Anime inside _Kaynak_Sec_
	seasons := v.ensureItemSeasons(ctx, matchedItem)
	if len(rest) == 2 {
		var children []fs.FileInfo
		for _, s := range seasons {
			if s.SeasonNumber <= 0 {
				continue
			}
			children = append(children, NewDirFileInfo(fmt.Sprintf("Season %02d", s.SeasonNumber), v.bootTime))
		}
		return NewVirtualDir(targetFolder, children, v.bootTime), nil
	}

	seasonFolder := rest[2]
	seasonNum := parseSeasonFolderNumber(seasonFolder)
	if seasonNum <= 0 {
		return nil, os.ErrNotExist
	}

	epCount := 12
	for _, s := range seasons {
		if s.SeasonNumber == seasonNum && s.EpisodeCount > 0 {
			epCount = s.EpisodeCount
			break
		}
	}

	if len(rest) == 3 {
		var children []fs.FileInfo
		for epNum := 1; epNum <= epCount; epNum++ {
			children = append(children, NewDirFileInfo(fmt.Sprintf("S%02dE%02d", seasonNum, epNum), v.bootTime))
		}
		return NewVirtualDir(seasonFolder, children, v.bootTime), nil
	}

	epFolder := rest[3]
	epNum := parseEpisodeFolderNumber(epFolder)
	if epNum <= 0 {
		return nil, os.ErrNotExist
	}

	filesMap, ordered := v.buildWorkingSourceFiles(ctx, matchedItem, mediaType, seasonNum, epNum)
	if len(rest) == 4 {
		var children []fs.FileInfo
		for _, name := range ordered {
			st, _ := filesMap[name].Stat()
			children = append(children, st)
		}
		return NewVirtualDir(epFolder, children, v.bootTime), nil
	}

	if len(rest) == 5 {
		if vf, ok := filesMap[rest[4]]; ok {
			return vf, nil
		}
	}

	return nil, os.ErrNotExist
}

func (v *AnthologyVFS) buildWorkingSourceFiles(ctx context.Context, item *config.LibraryItem, mediaType string, season, episode int) (map[string]*VirtualFile, []string) {
	filesMap := make(map[string]*VirtualFile)
	var ordered []string

	var sources []VFSDiscoveredSource
	if v.discoverFn != nil {
		sources = v.discoverFn(ctx, mediaType, item.TMDBID, item.Title, item.OriginalTitle, item.Year, season, episode)
	}

	folderTitle := formatItemFolderName(item.Title, item.Year)
	suffix := folderTitle
	if mediaType != "movie" {
		suffix = fmt.Sprintf("%s - S%02dE%02d", folderTitle, season, episode)
	}

	// 1. Auto Best option (if at least one working source exists)
	bestQual := "HD"
	if len(sources) > 0 && sources[0].Quality != "" {
		bestQual = sources[0].Quality
	}
	autoName := SanitizeFileName(fmt.Sprintf("1 - Otomatik En Iyi (%s) - %s", bestQual, suffix)) + ".strm"
	autoURL := fmt.Sprintf("%s/stream/resolve?type=%s&tmdb_id=%d&title=%s&year=%s&season=%d&episode=%d&provider=auto\n",
		v.baseURL(ctx), mediaType, item.TMDBID, url.QueryEscape(item.Title), url.QueryEscape(item.Year), season, episode)
	filesMap[autoName] = NewVirtualFile(autoName, []byte(autoURL), v.bootTime)
	ordered = append(ordered, autoName)

	// 2. Each verified working provider
	for i, src := range sources {
		fName := SanitizeFileName(fmt.Sprintf("%d - %s (%s) - %s", i+2, src.ProviderName, src.Quality, suffix)) + ".strm"
		sURL := fmt.Sprintf("%s/stream/resolve?type=%s&tmdb_id=%d&title=%s&year=%s&season=%d&episode=%d&provider=%s\n",
			v.baseURL(ctx), mediaType, item.TMDBID, url.QueryEscape(item.Title), url.QueryEscape(item.Year), season, episode, url.QueryEscape(src.ProviderID))
		filesMap[fName] = NewVirtualFile(fName, []byte(sURL), v.bootTime)
		ordered = append(ordered, fName)
	}

	return filesMap, ordered
}

func parseEpisodeFolderNumber(name string) int {
	upper := strings.ToUpper(strings.TrimSpace(name))
	if idx := strings.Index(upper, "E"); idx != -1 {
		num, _ := strconv.Atoi(strings.TrimSpace(upper[idx+1:]))
		if num > 0 {
			return num
		}
	}
	num, _ := strconv.Atoi(upper)
	return num
}

// ── Legacy Provider Explorer (/dav/_Tum_Saglayicilar/...) ─────────────────────

func (v *AnthologyVFS) handleLegacyAll(ctx context.Context, rest []string) (webdav.File, error) {
	if len(rest) == 0 {
		var children []fs.FileInfo
		for _, prov := range providers.All() {
			children = append(children, NewDirFileInfo(SanitizeFileName(prov.Name()), v.bootTime))
		}
		return NewVirtualDir(FolderLegacyAll, children, v.bootTime), nil
	}

	prov, ok := providers.GetByName(rest[0])
	if !ok {
		return nil, os.ErrNotExist
	}
	return v.handleProviderRoot(ctx, prov, rest[1:])
}

func (v *AnthologyVFS) handleProviderRoot(ctx context.Context, prov providers.Provider, rest []string) (webdav.File, error) {
	if len(rest) == 0 {
		children := []fs.FileInfo{
			NewDirFileInfo(FolderCatalog, v.bootTime),
			NewDirFileInfo(FolderSearch, v.bootTime),
		}
		return NewVirtualDir(prov.Name(), children, v.bootTime), nil
	}

	sub := rest[0]
	switch sub {
	case FolderCatalog:
		return v.handleProviderCatalog(ctx, prov, rest[1:])
	case FolderSearch:
		return v.handleProviderSearch(ctx, prov, rest[1:])
	default:
		return nil, os.ErrNotExist
	}
}

func (v *AnthologyVFS) handleProviderCatalog(ctx context.Context, prov providers.Provider, rest []string) (webdav.File, error) {
	items, err := v.getCatalogCached(ctx, prov)
	if err != nil && len(items) == 0 {
		items = nil
	}
	folderMap := v.indexMediaItems(prov.ID(), items, false)

	if len(rest) == 0 {
		var children []fs.FileInfo
		var names []string
		for folderName := range folderMap {
			names = append(names, folderName)
		}
		sort.Strings(names)
		for _, n := range names {
			children = append(children, NewDirFileInfo(n, v.bootTime))
		}
		return NewVirtualDir(FolderCatalog, children, v.bootTime), nil
	}

	itemFolder := rest[0]
	media, ok := folderMap[itemFolder]
	if !ok {
		if indexed, found := v.lookupIndexedMedia(prov.ID(), itemFolder); found {
			media = indexed.Item
			ok = true
		}
	}
	if !ok {
		return nil, os.ErrNotExist
	}

	return v.handleMediaItem(ctx, prov, itemFolder, media, rest[1:])
}

func (v *AnthologyVFS) handleProviderSearch(ctx context.Context, prov providers.Provider, rest []string) (webdav.File, error) {
	if len(rest) == 0 {
		queries := v.wl.GetProviderQueries(prov.ID())
		var children []fs.FileInfo
		for _, q := range queries {
			children = append(children, NewDirFileInfo(SanitizeFileName(q), v.bootTime))
		}
		return NewVirtualDir(FolderSearch, children, v.bootTime), nil
	}

	queryFolder := rest[0]
	query := v.resolveProviderSearchQuery(prov.ID(), queryFolder)
	items, _ := v.getSearchCached(ctx, prov, query)
	folderMap := v.indexMediaItems(prov.ID(), items, false)

	if len(rest) == 1 {
		var children []fs.FileInfo
		var names []string
		for folderName := range folderMap {
			names = append(names, folderName)
		}
		sort.Strings(names)
		for _, n := range names {
			children = append(children, NewDirFileInfo(n, v.bootTime))
		}
		return NewVirtualDir(queryFolder, children, v.bootTime), nil
	}

	itemFolder := rest[1]
	media, ok := folderMap[itemFolder]
	if !ok {
		if indexed, found := v.lookupIndexedMedia(prov.ID(), itemFolder); found {
			media = indexed.Item
			ok = true
		}
	}
	if !ok {
		return nil, os.ErrNotExist
	}

	return v.handleMediaItem(ctx, prov, itemFolder, media, rest[2:])
}

func (v *AnthologyVFS) handleWatchlist(ctx context.Context, rest []string) (webdav.File, error) {
	if len(rest) == 0 {
		queries := v.wl.GetGlobalQueries()
		var children []fs.FileInfo
		for _, q := range queries {
			children = append(children, NewDirFileInfo(SanitizeFileName(q), v.bootTime))
		}
		return NewVirtualDir(FolderWatchlist, children, v.bootTime), nil
	}

	queryFolder := rest[0]
	query := v.resolveGlobalSearchQuery(queryFolder)
	results := v.getGlobalWatchlistCached(ctx, query)

	if len(rest) == 1 {
		var children []fs.FileInfo
		var names []string
		for folderName := range results {
			names = append(names, folderName)
		}
		sort.Strings(names)
		for _, n := range names {
			children = append(children, NewDirFileInfo(n, v.bootTime))
		}
		return NewVirtualDir(queryFolder, children, v.bootTime), nil
	}

	itemFolder := rest[1]
	entry, ok := results[itemFolder]
	if !ok {
		if indexed, found := v.lookupIndexedMedia("global_watchlist", itemFolder); found {
			entry = indexed
			ok = true
		}
	}
	if !ok {
		return nil, os.ErrNotExist
	}

	prov, ok := providers.GetByID(entry.ProviderID)
	if !ok {
		return nil, os.ErrNotExist
	}
	return v.handleMediaItem(ctx, prov, itemFolder, entry.Item, rest[2:])
}

func (v *AnthologyVFS) handleMediaItem(ctx context.Context, prov providers.Provider, itemFolder string, item providers.MediaItem, rest []string) (webdav.File, error) {
	detail, err := v.getDetailsCached(ctx, prov, item)
	if err != nil || detail == nil {
		detail = &providers.MediaDetail{
			MediaItem: item,
			StreamID:  item.ID,
		}
	}

	cleanSeriesTitle := SanitizeFileName(detail.Title)
	if cleanSeriesTitle == "" || cleanSeriesTitle == "Untitled" {
		cleanSeriesTitle = SanitizeFileName(item.Title)
	}

	if len(detail.Seasons) > 0 || detail.Type == providers.MediaTypeSeries || detail.Type == providers.MediaTypeAnime {
		seasons := detail.Seasons
		if len(seasons) == 0 {
			seasons = []providers.Season{{Number: 1, Name: "Season 01"}}
		}

		if len(rest) == 0 {
			var children []fs.FileInfo
			for _, s := range seasons {
				sFolder := fmt.Sprintf("Season %02d", s.Number)
				children = append(children, NewDirFileInfo(sFolder, v.bootTime))
			}
			return NewVirtualDir(itemFolder, children, v.bootTime), nil
		}

		seasonFolder := rest[0]
		sNum := parseSeasonFolderNumber(seasonFolder)
		if sNum <= 0 {
			return nil, os.ErrNotExist
		}

		eps, err := v.getEpisodesCached(ctx, prov, detail, sNum)
		if err != nil {
			return nil, os.ErrNotExist
		}

		filesMap := v.buildEpisodeFiles(ctx, prov, cleanSeriesTitle, sNum, eps)

		if len(rest) == 1 {
			var children []fs.FileInfo
			var names []string
			for fName := range filesMap {
				names = append(names, fName)
			}
			sort.Strings(names)
			for _, fName := range names {
				vf := filesMap[fName]
				st, _ := vf.Stat()
				children = append(children, st)
			}
			return NewVirtualDir(seasonFolder, children, v.bootTime), nil
		}

		if len(rest) == 2 {
			targetFile, ok := filesMap[rest[1]]
			if !ok {
				return nil, os.ErrNotExist
			}
			return targetFile, nil
		}

		return nil, os.ErrNotExist
	}

	filesMap := v.buildMovieFiles(ctx, prov, detail)
	if len(rest) == 0 {
		var children []fs.FileInfo
		var names []string
		for fName := range filesMap {
			names = append(names, fName)
		}
		sort.Strings(names)
		for _, fName := range names {
			vf := filesMap[fName]
			st, _ := vf.Stat()
			children = append(children, st)
		}
		return NewVirtualDir(itemFolder, children, v.bootTime), nil
	}

	if len(rest) == 1 {
		targetFile, ok := filesMap[rest[0]]
		if !ok {
			return nil, os.ErrNotExist
		}
		return targetFile, nil
	}

	return nil, os.ErrNotExist
}

func (v *AnthologyVFS) buildEpisodeFiles(ctx context.Context, prov providers.Provider, seriesTitle string, seasonNum int, eps []providers.Episode) map[string]*VirtualFile {
	out := make(map[string]*VirtualFile)
	for idx, ep := range eps {
		epNum := ep.Number
		if epNum <= 0 {
			epNum = idx + 1
		}
		streamID := ep.StreamID
		if streamID == "" {
			streamID = ep.ID
		}
		baseName := SanitizeFileName(fmt.Sprintf("%s - S%02dE%02d", seriesTitle, seasonNum, epNum))
		strmName := baseName + ".strm"
		strmContent := []byte(fmt.Sprintf("%s/stream/%s?id=%s\n", v.baseURL(ctx), prov.ID(), url.QueryEscape(streamID)))
		out[strmName] = NewVirtualFile(strmName, strmContent, v.bootTime)
	}
	return out
}

func (v *AnthologyVFS) buildMovieFiles(ctx context.Context, prov providers.Provider, detail *providers.MediaDetail) map[string]*VirtualFile {
	out := make(map[string]*VirtualFile)
	baseTitle := SanitizeFileName(detail.Title)
	if detail.Year != "" && !strings.Contains(baseTitle, fmt.Sprintf("(%s)", detail.Year)) {
		baseTitle = SanitizeFileName(fmt.Sprintf("%s (%s)", detail.Title, detail.Year))
	}
	streamID := detail.StreamID
	if streamID == "" {
		streamID = detail.ID
	}
	strmName := baseTitle + ".strm"
	strmContent := []byte(fmt.Sprintf("%s/stream/%s?id=%s\n", v.baseURL(ctx), prov.ID(), url.QueryEscape(streamID)))
	out[strmName] = NewVirtualFile(strmName, strmContent, v.bootTime)
	return out
}

func formatItemFolderName(title, year string) string {
	cleanTitle := SanitizeFileName(title)
	if year != "" && !strings.Contains(cleanTitle, fmt.Sprintf("(%s)", year)) {
		return SanitizeFileName(fmt.Sprintf("%s (%s)", title, year))
	}
	return cleanTitle
}

func parseSeasonFolderNumber(folder string) int {
	clean := strings.ToLower(strings.TrimSpace(folder))
	clean = strings.TrimPrefix(clean, "season")
	clean = strings.TrimPrefix(clean, "sezon")
	clean = strings.TrimSpace(clean)
	n, err := strconv.Atoi(clean)
	if err != nil {
		return -1
	}
	return n
}

func (v *AnthologyVFS) getCatalogCached(ctx context.Context, prov providers.Provider) ([]providers.MediaItem, error) {
	key := "catalog:" + prov.ID()
	if val, ok := v.cache.Get(key); ok {
		if items, ok := val.([]providers.MediaItem); ok {
			return items, nil
		}
	}
	cctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	items, err := prov.GetCatalog(cctx, 1)
	if err != nil {
		return nil, err
	}
	v.cache.Set(key, items, v.cfg.CatalogTTL)
	return items, nil
}

func (v *AnthologyVFS) getSearchCached(ctx context.Context, prov providers.Provider, query string) ([]providers.MediaItem, error) {
	key := fmt.Sprintf("search:%s:%s", prov.ID(), strings.ToLower(strings.TrimSpace(query)))
	if val, ok := v.cache.Get(key); ok {
		if items, ok := val.([]providers.MediaItem); ok {
			return items, nil
		}
	}
	cctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	items, err := prov.Search(cctx, query)
	if err != nil {
		return nil, err
	}
	v.cache.Set(key, items, v.cfg.CatalogTTL)
	return items, nil
}

func (v *AnthologyVFS) getDetailsCached(ctx context.Context, prov providers.Provider, item providers.MediaItem) (*providers.MediaDetail, error) {
	key := fmt.Sprintf("details:%s:%s", prov.ID(), item.ID)
	if val, ok := v.cache.Get(key); ok {
		if det, ok := val.(*providers.MediaDetail); ok {
			return det, nil
		}
	}
	cctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	det, err := prov.GetDetails(cctx, item.ID)
	if err != nil {
		return nil, err
	}
	v.cache.Set(key, det, v.cfg.DetailsTTL)
	return det, nil
}

func (v *AnthologyVFS) getEpisodesCached(ctx context.Context, prov providers.Provider, detail *providers.MediaDetail, season int) ([]providers.Episode, error) {
	key := fmt.Sprintf("episodes:%s:%s:%d", prov.ID(), detail.ID, season)
	if val, ok := v.cache.Get(key); ok {
		if eps, ok := val.([]providers.Episode); ok {
			return eps, nil
		}
	}
	for _, s := range detail.Seasons {
		if s.Number == season && len(s.Episodes) > 0 {
			v.cache.Set(key, s.Episodes, v.cfg.DetailsTTL)
			return s.Episodes, nil
		}
	}
	cctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	eps, err := prov.GetEpisodes(cctx, detail.ID, season)
	if err != nil {
		return nil, err
	}
	v.cache.Set(key, eps, v.cfg.DetailsTTL)
	return eps, nil
}

func (v *AnthologyVFS) getGlobalWatchlistCached(ctx context.Context, query string) map[string]indexedMedia {
	cleanQuery := strings.ToLower(strings.TrimSpace(query))
	key := "watchlist:" + cleanQuery
	if val, ok := v.cache.Get(key); ok {
		if m, ok := val.(map[string]indexedMedia); ok {
			return m
		}
	}
	results := make(map[string]indexedMedia)
	var mu sync.Mutex
	var wg sync.WaitGroup

	allProvs := providers.All()
	for _, prov := range allProvs {
		wg.Add(1)
		go func(p providers.Provider) {
			defer wg.Done()
			cctx, cancel := context.WithTimeout(ctx, 12*time.Second)
			defer cancel()
			items, err := p.Search(cctx, query)
			if err != nil || len(items) == 0 {
				return
			}
			folderMap := v.indexMediaItems(p.ID(), items, true)
			mu.Lock()
			for fName, it := range folderMap {
				results[fName] = indexedMedia{
					ProviderID: p.ID(),
					Item:       it,
				}
			}
			mu.Unlock()
		}(prov)
	}
	wg.Wait()
	v.cache.Set(key, results, 30*time.Minute)
	return results
}

func (v *AnthologyVFS) indexMediaItems(providerID string, items []providers.MediaItem, prefixProvider bool) map[string]providers.MediaItem {
	folderMap := make(map[string]providers.MediaItem)
	for _, it := range items {
		title := SanitizeFileName(it.Title)
		if title == "" || title == "Untitled" {
			continue
		}
		var folderName string
		if it.Year != "" && !strings.Contains(title, fmt.Sprintf("(%s)", it.Year)) {
			folderName = fmt.Sprintf("%s (%s)", title, it.Year)
		} else {
			folderName = title
		}
		if prefixProvider {
			p, ok := providers.GetByID(providerID)
			pName := providerID
			if ok {
				pName = p.Name()
			}
			folderName = fmt.Sprintf("[%s] %s", pName, folderName)
		}
		folderName = SanitizeFileName(folderName)
		folderMap[folderName] = it

		v.mu.Lock()
		v.itemIndex[folderKey(providerID, folderName)] = indexedMedia{
			ProviderID: providerID,
			Item:       it,
		}
		v.mu.Unlock()
	}
	return folderMap
}

func (v *AnthologyVFS) lookupIndexedMedia(providerID, folderName string) (indexedMedia, bool) {
	v.mu.RLock()
	defer v.mu.RUnlock()
	it, ok := v.itemIndex[folderKey(providerID, folderName)]
	return it, ok
}

func folderKey(providerID, folderName string) string {
	return strings.ToLower(providerID) + "::" + strings.ToLower(folderName)
}

func (v *AnthologyVFS) resolveProviderSearchQuery(providerID, folderName string) string {
	queries := v.wl.GetProviderQueries(providerID)
	for _, q := range queries {
		if SanitizeFileName(q) == folderName {
			return q
		}
	}
	return folderName
}

func (v *AnthologyVFS) resolveGlobalSearchQuery(folderName string) string {
	queries := v.wl.GetGlobalQueries()
	for _, q := range queries {
		if SanitizeFileName(q) == folderName {
			return q
		}
	}
	return folderName
}
