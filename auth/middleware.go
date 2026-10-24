package auth

import (
	"net/http"
	"strings"

	"anthology-dav/config"
)

// WebDAVAuthMiddleware enforces HTTP Basic Auth on /dav/ in multi-user mode.
func WebDAVAuthMiddleware(cfg *config.Config, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !cfg.MultiUser {
			// Standalone single-user mode: no auth required
			next.ServeHTTP(w, r)
			return
		}

		// 1. Try standard HTTP Basic Auth
		username, password, ok := r.BasicAuth()
		if ok && username != "" && password != "" {
			u, err := DB().Authenticate(username, password)
			if err == nil && u != nil {
				ctx := WithUser(r.Context(), u)
				next.ServeHTTP(w, r.WithContext(ctx))
				return
			}
		}

		// 2. Try query parameter credentials fallback (?u=username&p=password or ?token=sessionToken)
		q := r.URL.Query()
		qUser := q.Get("u")
		qPass := q.Get("p")
		if qUser != "" && qPass != "" {
			u, err := DB().Authenticate(qUser, qPass)
			if err == nil && u != nil {
				ctx := WithUser(r.Context(), u)
				next.ServeHTTP(w, r.WithContext(ctx))
				return
			}
		}

		qToken := q.Get("token")
		if qToken != "" {
			u, err := DB().ValidateSession(qToken)
			if err == nil && u != nil {
				ctx := WithUser(r.Context(), u)
				next.ServeHTTP(w, r.WithContext(ctx))
				return
			}
		}

		// 3. Try session cookie (e.g. when accessing /dav/ from browser)
		if cookie, err := r.Cookie("anthology_session"); err == nil && cookie.Value != "" {
			u, err := DB().ValidateSession(cookie.Value)
			if err == nil && u != nil {
				ctx := WithUser(r.Context(), u)
				next.ServeHTTP(w, r.WithContext(ctx))
				return
			}
		}

		// Denied: request HTTP Basic Auth prompt in VidHub / Infuse / Kodi / browser
		w.Header().Set("WWW-Authenticate", `Basic realm="Anthology WebDAV"`)
		http.Error(w, "401 Unauthorized - Lütfen geçerli kullanıcı adı ve şifrenizi girin.", http.StatusUnauthorized)
	})
}

// ExtractWebUser extracts the current user from session cookie, Basic Auth or Bearer token.
func ExtractWebUser(r *http.Request) *User {
	// 1. Check anthology_session cookie
	if cookie, err := r.Cookie("anthology_session"); err == nil && cookie.Value != "" {
		if u, err := DB().ValidateSession(cookie.Value); err == nil && u != nil {
			return u
		}
	}

	// 2. Check Bearer token
	authHeader := r.Header.Get("Authorization")
	if strings.HasPrefix(strings.ToLower(authHeader), "bearer ") {
		token := strings.TrimSpace(authHeader[7:])
		if u, err := DB().ValidateSession(token); err == nil && u != nil {
			return u
		}
	}

	// 3. Check Basic Auth
	if username, password, ok := r.BasicAuth(); ok && username != "" {
		if u, err := DB().Authenticate(username, password); err == nil && u != nil {
			return u
		}
	}

	// 4. Check query token
	if token := r.URL.Query().Get("token"); token != "" {
		if u, err := DB().ValidateSession(token); err == nil && u != nil {
			return u
		}
	}

	return nil
}
