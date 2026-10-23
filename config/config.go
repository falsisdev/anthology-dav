package config

import (
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

const (
	DefaultHost       = "0.0.0.0"
	DefaultPort       = 8085
	DefaultUserAgent  = "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/124.0.0.0 Safari/537.36"
	DefaultTMDBAPIKey = "500330721680edb6d5f7f12ba7cd9023"
	VidlinkTMDBAPIKey = "68e094699525b18a70bab2f86b1fa706"
	RemoteConfigURL   = "https://raw.githubusercontent.com/falsisdev/anthology/main/config.json"
)

// GetAnthologyDir returns the path to a local anthology repo if present, checked via ANTHOLOGY_DIR env, adjacent dir, or home dir.
func GetAnthologyDir() string {
	if env := os.Getenv("ANTHOLOGY_DIR"); env != "" {
		return env
	}
	// Check adjacent directory ../anthology
	if wd, err := os.Getwd(); err == nil {
		adj := filepath.Join(filepath.Dir(wd), "anthology")
		if stat, err := os.Stat(adj); err == nil && stat.IsDir() {
			return adj
		}
	}
	// Check ~/Desktop/Projects/anthology
	if home, err := os.UserHomeDir(); err == nil {
		homePath := filepath.Join(home, "Desktop", "Projects", "anthology")
		if stat, err := os.Stat(homePath); err == nil && stat.IsDir() {
			return homePath
		}
	}
	return ""
}

// Config holds the runtime configuration for the anthology-dav server.
type Config struct {
	Host          string
	Port          int
	BaseURL       string
	UserAgent     string
	CatalogTTL    time.Duration
	DetailsTTL    time.Duration
	StreamTTL     time.Duration
	WatchlistPath string
	LibraryPath   string
}

var (
	globalCfg *Config
	cfgOnce   sync.Once
)

// Get returns the singleton server configuration.
func Get() *Config {
	cfgOnce.Do(func() {
		wd, err := os.Getwd()
		if err != nil {
			wd = "."
		}
		globalCfg = &Config{
			Host:          DefaultHost,
			Port:          DefaultPort,
			BaseURL:       fmt.Sprintf("http://127.0.0.1:%d", DefaultPort),
			UserAgent:     DefaultUserAgent,
			CatalogTTL:    1 * time.Hour,
			DetailsTTL:    6 * time.Hour,
			StreamTTL:     15 * time.Minute,
			WatchlistPath: filepath.Join(wd, "watchlist.json"),
			LibraryPath:   filepath.Join(wd, "library.json"),
		}
	})
	return globalCfg
}

// SetBind updates the host and port and recomputes BaseURL.
// Wildcard bind addresses (0.0.0.0, ::, empty) are not connectable, so the
// client-facing BaseURL (written into .strm files) falls back to 127.0.0.1.
func (c *Config) SetBind(host string, port int) {
	c.Host = host
	c.Port = port
	publicHost := host
	switch host {
	case "", "0.0.0.0", "::", "[::]":
		publicHost = "127.0.0.1"
	}
	c.BaseURL = fmt.Sprintf("http://%s:%d", publicHost, port)
}

// LANInfo holds detected local network / Hotspot addresses for connecting from tablets, phones, and TVs.
type LANInfo struct {
	PrimaryIP  string   `json:"primary_ip"`
	PrimaryURL string   `json:"primary_url"`
	AllIPs     []string `json:"all_ips"`
}

// DetectLANAddresses discovers the active physical Wi-Fi / LAN / Hotspot IPv4 address
// (ignoring virtual bridges like bridge0, bridge100, utun, vmnet, docker).
func DetectLANAddresses(port int) LANInfo {
	info := LANInfo{
		AllIPs: []string{},
	}
	seen := make(map[string]bool)

	// 1. Outbound route UDP probe (zero packets sent, reveals OS active route interface IP)
	if conn, err := net.DialTimeout("udp", "8.8.8.8:80", 300*time.Millisecond); err == nil {
		if udpAddr, ok := conn.LocalAddr().(*net.UDPAddr); ok && udpAddr.IP != nil {
			ip := udpAddr.IP.To4()
			if ip != nil && !ip.IsLoopback() {
				ipStr := ip.String()
				info.PrimaryIP = ipStr
				info.AllIPs = append(info.AllIPs, ipStr)
				seen[ipStr] = true
			}
		}
		_ = conn.Close()
	}

	// 2. Enumerate active physical interfaces (en0, eth0, wlan0, etc.) excluding virtual bridges/tunnels
	if ifaces, err := net.Interfaces(); err == nil {
		for _, iface := range ifaces {
			if iface.Flags&net.FlagUp == 0 || iface.Flags&net.FlagLoopback != 0 {
				continue
			}
			lowName := strings.ToLower(iface.Name)
			if strings.HasPrefix(lowName, "bridge") ||
				strings.HasPrefix(lowName, "utun") ||
				strings.HasPrefix(lowName, "awdl") ||
				strings.HasPrefix(lowName, "llw") ||
				strings.HasPrefix(lowName, "veth") ||
				strings.HasPrefix(lowName, "docker") ||
				strings.HasPrefix(lowName, "vmnet") ||
				strings.HasPrefix(lowName, "vboxnet") ||
				strings.HasPrefix(lowName, "anpi") ||
				strings.HasPrefix(lowName, "ap") {
				continue
			}
			addrs, err := iface.Addrs()
			if err != nil {
				continue
			}
			for _, a := range addrs {
				ipNet, ok := a.(*net.IPNet)
				if !ok || ipNet.IP == nil {
					continue
				}
				ip4 := ipNet.IP.To4()
				if ip4 == nil || ip4.IsLoopback() || ip4.IsLinkLocalUnicast() {
					continue
				}
				ipStr := ip4.String()
				if !seen[ipStr] {
					seen[ipStr] = true
					info.AllIPs = append(info.AllIPs, ipStr)
					if info.PrimaryIP == "" {
						info.PrimaryIP = ipStr
					}
				}
			}
		}
	}

	if info.PrimaryIP == "" {
		info.PrimaryIP = "127.0.0.1"
	}
	info.PrimaryURL = fmt.Sprintf("http://%s:%d", info.PrimaryIP, port)

	return info
}

// SetPublicURL overrides the client-facing BaseURL (e.g. http://192.168.1.10:8085
// so other devices on the LAN such as Apple TV can open .strm links).
func (c *Config) SetPublicURL(u string) {
	u = strings.TrimRight(strings.TrimSpace(u), "/")
	if u != "" {
		c.BaseURL = u
	}
}

// Addr returns the host:port string for net/http Server.
func (c *Config) Addr() string {
	return fmt.Sprintf("%s:%d", c.Host, c.Port)
}

// WatchlistData stores persisted MKCOL search queries for global Watchlist and per-provider Arama_Listesi.
type WatchlistData struct {
	GlobalQueries   []string            `json:"global_queries"`
	ProviderQueries map[string][]string `json:"provider_queries"`
}

// WatchlistManager manages thread-safe access and disk persistence for watchlist.json.
type WatchlistManager struct {
	mu   sync.RWMutex
	path string
	data WatchlistData
}

var (
	wlManager *WatchlistManager
	wlOnce    sync.Once
)

// Watchlist returns the global WatchlistManager instance.
func Watchlist() *WatchlistManager {
	wlOnce.Do(func() {
		cfg := Get()
		wlManager = &WatchlistManager{
			path: cfg.WatchlistPath,
			data: WatchlistData{
				GlobalQueries:   []string{},
				ProviderQueries: make(map[string][]string),
			},
		}
		_ = wlManager.Load()
	})
	return wlManager
}

// Load reads watchlist.json from disk if present.
func (w *WatchlistManager) Load() error {
	w.mu.Lock()
	defer w.mu.Unlock()

	raw, err := os.ReadFile(w.path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	var loaded WatchlistData
	if err := json.Unmarshal(raw, &loaded); err != nil {
		return err
	}
	if loaded.GlobalQueries == nil {
		loaded.GlobalQueries = []string{}
	}
	if loaded.ProviderQueries == nil {
		loaded.ProviderQueries = make(map[string][]string)
	}
	w.data = loaded
	return nil
}

func (w *WatchlistManager) saveLocked() error {
	raw, err := json.MarshalIndent(w.data, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(w.path, raw, 0644)
}

// AddGlobalQuery adds a search query to /dav/Watchlist/ and persists to watchlist.json.
func (w *WatchlistManager) AddGlobalQuery(query string) error {
	query = strings.TrimSpace(query)
	if query == "" {
		return nil
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	for _, q := range w.data.GlobalQueries {
		if strings.EqualFold(q, query) {
			return nil
		}
	}
	w.data.GlobalQueries = append(w.data.GlobalQueries, query)
	return w.saveLocked()
}

// RemoveGlobalQuery removes a search query from /dav/Watchlist/.
func (w *WatchlistManager) RemoveGlobalQuery(query string) error {
	query = strings.TrimSpace(query)
	w.mu.Lock()
	defer w.mu.Unlock()
	filtered := make([]string, 0, len(w.data.GlobalQueries))
	for _, q := range w.data.GlobalQueries {
		if !strings.EqualFold(q, query) {
			filtered = append(filtered, q)
		}
	}
	w.data.GlobalQueries = filtered
	return w.saveLocked()
}

// GetGlobalQueries returns all persisted queries in /dav/Watchlist/.
func (w *WatchlistManager) GetGlobalQueries() []string {
	w.mu.RLock()
	defer w.mu.RUnlock()
	out := make([]string, len(w.data.GlobalQueries))
	copy(out, w.data.GlobalQueries)
	return out
}

// AddProviderQuery adds a search query to /dav/{Provider}/Arama_Listesi/ and persists to watchlist.json.
func (w *WatchlistManager) AddProviderQuery(providerID, query string) error {
	query = strings.TrimSpace(query)
	if query == "" || providerID == "" {
		return nil
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	list := w.data.ProviderQueries[providerID]
	for _, q := range list {
		if strings.EqualFold(q, query) {
			return nil
		}
	}
	w.data.ProviderQueries[providerID] = append(list, query)
	return w.saveLocked()
}

// RemoveProviderQuery removes a search query from /dav/{Provider}/Arama_Listesi/.
func (w *WatchlistManager) RemoveProviderQuery(providerID, query string) error {
	query = strings.TrimSpace(query)
	w.mu.Lock()
	defer w.mu.Unlock()
	list := w.data.ProviderQueries[providerID]
	filtered := make([]string, 0, len(list))
	for _, q := range list {
		if !strings.EqualFold(q, query) {
			filtered = append(filtered, q)
		}
	}
	w.data.ProviderQueries[providerID] = filtered
	return w.saveLocked()
}

// GetProviderQueries returns all persisted queries for a specific provider.
func (w *WatchlistManager) GetProviderQueries(providerID string) []string {
	w.mu.RLock()
	defer w.mu.RUnlock()
	list := w.data.ProviderQueries[providerID]
	out := make([]string, len(list))
	copy(out, list)
	return out
}

// RemoteConfig holds dynamic domain/endpoint overrides from Anthology's config.json.
var (
	remoteCfgMu   sync.RWMutex
	remoteCfgData map[string]any
	remoteCfgTime time.Time
)

// LoadRemoteConfig loads Anthology's config.json from local repository or GitHub.
func LoadRemoteConfig() map[string]any {
	remoteCfgMu.RLock()
	if remoteCfgData != nil && time.Since(remoteCfgTime) < 10*time.Minute {
		defer remoteCfgMu.RUnlock()
		return remoteCfgData
	}
	remoteCfgMu.RUnlock()

	remoteCfgMu.Lock()
	defer remoteCfgMu.Unlock()

	if remoteCfgData != nil && time.Since(remoteCfgTime) < 10*time.Minute {
		return remoteCfgData
	}

	// 1. Try local Anthology config.json first for instant startup
	if anthDir := GetAnthologyDir(); anthDir != "" {
		localCfgPath := filepath.Join(anthDir, "config.json")
		if raw, err := os.ReadFile(localCfgPath); err == nil {
			var parsed map[string]any
			if json.Unmarshal(raw, &parsed) == nil {
				remoteCfgData = parsed
				remoteCfgTime = time.Now()
			}
		}
	}

	// 2. Try remote GitHub config.json with short timeout
	client := &http.Client{Timeout: 4 * time.Second}
	req, err := http.NewRequest(http.MethodGet, RemoteConfigURL, nil)
	if err == nil {
		req.Header.Set("User-Agent", DefaultUserAgent)
		if resp, err := client.Do(req); err == nil {
			defer resp.Body.Close()
			if resp.StatusCode == http.StatusOK {
				if body, err := io.ReadAll(resp.Body); err == nil {
					var parsed map[string]any
					if json.Unmarshal(body, &parsed) == nil {
						remoteCfgData = parsed
						remoteCfgTime = time.Now()
					}
				}
			}
		}
	}

	if remoteCfgData == nil {
		remoteCfgData = make(map[string]any)
	}
	return remoteCfgData
}

// RemoteString looks up a dot-separated path (e.g. "urls.anime.animecix.base") from Anthology config.json.
func RemoteString(dotPath, fallback string) string {
	data := LoadRemoteConfig()
	parts := strings.Split(dotPath, ".")
	var cur any = data
	for _, p := range parts {
		m, ok := cur.(map[string]any)
		if !ok {
			return fallback
		}
		val, exists := m[p]
		if !exists {
			return fallback
		}
		cur = val
	}
	if s, ok := cur.(string); ok && strings.TrimSpace(s) != "" {
		return strings.TrimRight(strings.TrimSpace(s), "/")
	}
	return fallback
}
