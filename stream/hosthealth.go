package stream

import (
	"net/url"
	"strings"
	"sync"
	"time"

	"anthology-dav/providers"
)

// hostCooldown is how long a CDN host is skipped after returning 429/403/5xx.
const hostCooldown = 10 * time.Minute

var (
	badHostsMu sync.RWMutex
	badHosts   = map[string]time.Time{}
)

func hostOf(rawURL string) string {
	u, err := url.Parse(rawURL)
	if err != nil {
		return ""
	}
	return strings.ToLower(u.Hostname())
}

// markHostBad puts the host of rawURL on cooldown.
func markHostBad(rawURL string) {
	h := hostOf(rawURL)
	if h == "" {
		return
	}
	badHostsMu.Lock()
	badHosts[h] = time.Now().Add(hostCooldown)
	badHostsMu.Unlock()
}

// isHostBad reports whether rawURL's host is currently on cooldown.
func isHostBad(rawURL string) bool {
	h := hostOf(rawURL)
	if h == "" {
		return false
	}
	badHostsMu.RLock()
	until, ok := badHosts[h]
	badHostsMu.RUnlock()
	return ok && time.Now().Before(until)
}

// isBadUpstreamStatus reports statuses that indicate a blocked / rate-limited / broken CDN.
func isBadUpstreamStatus(code int) bool {
	return code == 429 || code == 403 || code == 401 || code == 410 || code >= 500
}

// hasHealthySource reports whether a stream result has at least one source on a healthy host.
func hasHealthySource(res *providers.StreamResult) bool {
	if res == nil {
		return false
	}
	if res.Primary.URL != "" && !isHostBad(res.Primary.URL) {
		return true
	}
	for _, a := range res.Alternates {
		if a.URL != "" && !isHostBad(a.URL) {
			return true
		}
	}
	return false
}

// orderByHealth moves sources on cooled-down hosts to the end (keeping them as last resort).
func orderByHealth(sources []providers.StreamSource) []providers.StreamSource {
	good := make([]providers.StreamSource, 0, len(sources))
	var bad []providers.StreamSource
	for _, s := range sources {
		if isHostBad(s.URL) {
			bad = append(bad, s)
		} else {
			good = append(good, s)
		}
	}
	return append(good, bad...)
}
