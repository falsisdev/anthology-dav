package auth

import (
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"

	"golang.org/x/crypto/bcrypt"
	_ "modernc.org/sqlite"

	"anthology-dav/config"
)

var (
	ErrUserNotFound      = errors.New("kullanıcı bulunamadı")
	ErrUserAlreadyExists = errors.New("bu kullanıcı adı zaten kullanılıyor")
	ErrInvalidPassword   = errors.New("şifre hatalı")
	ErrInvalidUsername   = errors.New("kullanıcı adı 3-30 karakter arasında olmalı ve sadece harf, rakam ve alt çizgi içermelidir")
	ErrInvalidPassLength = errors.New("şifre en az 4 karakter olmalıdır")
	ErrSessionExpired    = errors.New("oturum süresi doldu")
)

var usernameRegex = regexp.MustCompile(`^[a-zA-Z0-9_]{3,30}$`)

type Database struct {
	db *sql.DB
	mu sync.RWMutex
}

var (
	globalDB   *Database
	globalDBMu sync.Mutex
)

// DB returns the global Database instance.
func DB() *Database {
	return globalDB
}

// InitDB initializes SQLite and executes schema migrations.
func InitDB(dbPath string) (*Database, error) {
	globalDBMu.Lock()
	defer globalDBMu.Unlock()

	if globalDB != nil {
		return globalDB, nil
	}

	dir := filepath.Dir(dbPath)
	if dir != "" && dir != "." {
		if err := os.MkdirAll(dir, 0755); err != nil {
			return nil, fmt.Errorf("failed to create db directory: %w", err)
		}
	}

	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		return nil, fmt.Errorf("failed to open sqlite db: %w", err)
	}

	// Performance & durability settings
	pragmas := []string{
		"PRAGMA foreign_keys = ON;",
		"PRAGMA journal_mode = WAL;",
		"PRAGMA synchronous = NORMAL;",
		"PRAGMA busy_timeout = 5000;",
	}
	for _, p := range pragmas {
		if _, err := db.Exec(p); err != nil {
			_ = db.Close()
			return nil, fmt.Errorf("failed to set pragma %q: %w", p, err)
		}
	}

	d := &Database{db: db}
	if err := d.migrate(); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("migration failed: %w", err)
	}

	globalDB = d
	return d, nil
}

func (d *Database) migrate() error {
	queries := []string{
		`CREATE TABLE IF NOT EXISTS users (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			username TEXT UNIQUE NOT NULL COLLATE NOCASE,
			password_hash TEXT NOT NULL,
			created_at DATETIME DEFAULT CURRENT_TIMESTAMP
		);`,
		`CREATE TABLE IF NOT EXISTS user_libraries (
			user_id INTEGER NOT NULL,
			id TEXT NOT NULL,
			tmdb_id INTEGER NOT NULL,
			title TEXT NOT NULL,
			original_title TEXT,
			year TEXT,
			media_type TEXT NOT NULL,
			poster_url TEXT,
			backdrop_url TEXT,
			overview TEXT,
			selected_provider TEXT DEFAULT 'auto',
			alt_titles_json TEXT,
			working_providers_json TEXT,
			seasons_json TEXT,
			added_at DATETIME DEFAULT CURRENT_TIMESTAMP,
			PRIMARY KEY (user_id, id),
			FOREIGN KEY(user_id) REFERENCES users(id) ON DELETE CASCADE
		);`,
		`CREATE INDEX IF NOT EXISTS idx_user_lib_type ON user_libraries(user_id, media_type);`,
		`CREATE INDEX IF NOT EXISTS idx_user_lib_tmdb ON user_libraries(user_id, tmdb_id);`,
		`CREATE TABLE IF NOT EXISTS sessions (
			token TEXT PRIMARY KEY,
			user_id INTEGER NOT NULL,
			expires_at DATETIME NOT NULL,
			created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
			FOREIGN KEY(user_id) REFERENCES users(id) ON DELETE CASCADE
		);`,
		`CREATE INDEX IF NOT EXISTS idx_sessions_user ON sessions(user_id);`,
		`CREATE INDEX IF NOT EXISTS idx_sessions_expires ON sessions(expires_at);`,
	}

	for _, q := range queries {
		if _, err := d.db.Exec(q); err != nil {
			return err
		}
	}
	return nil
}

// Close closes the database connection.
func (d *Database) Close() error {
	if d.db != nil {
		return d.db.Close()
	}
	return nil
}

// ── User Management ──────────────────────────────────────────────────────────

// Register creates a new user account with a bcrypt-hashed password.
func (d *Database) Register(username, password string) (*User, error) {
	username = strings.TrimSpace(username)
	if !usernameRegex.MatchString(username) {
		return nil, ErrInvalidUsername
	}
	if len(password) < 4 {
		return nil, ErrInvalidPassLength
	}

	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return nil, fmt.Errorf("failed to hash password: %w", err)
	}

	now := time.Now().UTC()
	res, err := d.db.Exec(`INSERT INTO users (username, password_hash, created_at) VALUES (?, ?, ?)`,
		username, string(hash), now)
	if err != nil {
		if strings.Contains(err.Error(), "UNIQUE") || strings.Contains(err.Error(), "constraint") {
			return nil, ErrUserAlreadyExists
		}
		return nil, err
	}

	id, err := res.LastInsertId()
	if err != nil {
		return nil, err
	}

	return &User{
		ID:           id,
		Username:     username,
		PasswordHash: string(hash),
		CreatedAt:    now,
	}, nil
}

// Authenticate verifies username and password, returning the User on success.
func (d *Database) Authenticate(username, password string) (*User, error) {
	u, err := d.GetUserByUsername(username)
	if err != nil {
		return nil, err
	}

	if err := bcrypt.CompareHashAndPassword([]byte(u.PasswordHash), []byte(password)); err != nil {
		return nil, ErrInvalidPassword
	}

	return u, nil
}

// GetUserByUsername retrieves a user by case-insensitive username.
func (d *Database) GetUserByUsername(username string) (*User, error) {
	username = strings.TrimSpace(username)
	row := d.db.QueryRow(`SELECT id, username, password_hash, created_at FROM users WHERE username = ? COLLATE NOCASE`, username)
	var u User
	if err := row.Scan(&u.ID, &u.Username, &u.PasswordHash, &u.CreatedAt); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrUserNotFound
		}
		return nil, err
	}
	return &u, nil
}

// GetUserByID retrieves a user by ID.
func (d *Database) GetUserByID(id int64) (*User, error) {
	row := d.db.QueryRow(`SELECT id, username, password_hash, created_at FROM users WHERE id = ?`, id)
	var u User
	if err := row.Scan(&u.ID, &u.Username, &u.PasswordHash, &u.CreatedAt); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrUserNotFound
		}
		return nil, err
	}
	return &u, nil
}

// ── Session Management ───────────────────────────────────────────────────────

func generateToken() string {
	b := make([]byte, 24)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

// CreateSession generates a new 30-day login session for a user.
func (d *Database) CreateSession(userID int64) (*Session, error) {
	u, err := d.GetUserByID(userID)
	if err != nil {
		return nil, err
	}

	token := generateToken()
	now := time.Now().UTC()
	expires := now.Add(30 * 24 * time.Hour)

	_, err = d.db.Exec(`INSERT INTO sessions (token, user_id, expires_at, created_at) VALUES (?, ?, ?, ?)`,
		token, userID, expires, now)
	if err != nil {
		return nil, err
	}

	return &Session{
		Token:     token,
		UserID:    userID,
		Username:  u.Username,
		ExpiresAt: expires,
		CreatedAt: now,
	}, nil
}

// ValidateSession verifies a session token and returns the corresponding User.
func (d *Database) ValidateSession(token string) (*User, error) {
	token = strings.TrimSpace(token)
	if token == "" {
		return nil, ErrSessionExpired
	}

	var u User
	var expires time.Time
	row := d.db.QueryRow(`
		SELECT u.id, u.username, u.password_hash, u.created_at, s.expires_at
		FROM sessions s
		JOIN users u ON u.id = s.user_id
		WHERE s.token = ?`, token)
	if err := row.Scan(&u.ID, &u.Username, &u.PasswordHash, &u.CreatedAt, &expires); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrSessionExpired
		}
		return nil, err
	}

	if time.Now().UTC().After(expires) {
		_ = d.DeleteSession(token)
		return nil, ErrSessionExpired
	}

	return &u, nil
}

// DeleteSession invalidates a session token (logout).
func (d *Database) DeleteSession(token string) error {
	_, err := d.db.Exec(`DELETE FROM sessions WHERE token = ?`, token)
	return err
}

// ── User Library Management ──────────────────────────────────────────────────

// AddUserLibraryItem adds or updates a media item in the specified user's library.
func (d *Database) AddUserLibraryItem(userID int64, item config.LibraryItem) error {
	if item.ID == "" {
		item.ID = fmt.Sprintf("%s:%d", item.Type, item.TMDBID)
	}
	if item.Type == "" {
		item.Type = "movie"
	}
	if item.SelectedProvider == "" {
		item.SelectedProvider = "auto"
	}

	altTitlesRaw, _ := json.Marshal(item.AltTitles)
	workingRaw, _ := json.Marshal(item.WorkingProviders)
	seasonsRaw, _ := json.Marshal(item.Seasons)

	query := `INSERT INTO user_libraries 
		(user_id, id, tmdb_id, title, original_title, year, media_type, poster_url, backdrop_url, overview, selected_provider, alt_titles_json, working_providers_json, seasons_json, added_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(user_id, id) DO UPDATE SET
			title = excluded.title,
			original_title = excluded.original_title,
			year = excluded.year,
			media_type = excluded.media_type,
			poster_url = excluded.poster_url,
			backdrop_url = excluded.backdrop_url,
			overview = excluded.overview,
			selected_provider = excluded.selected_provider,
			alt_titles_json = excluded.alt_titles_json,
			working_providers_json = excluded.working_providers_json,
			seasons_json = excluded.seasons_json`

	now := time.Now().UTC()
	_, err := d.db.Exec(query,
		userID, item.ID, item.TMDBID, item.Title, item.OriginalTitle, item.Year, item.Type,
		item.PosterURL, item.BackdropURL, item.Overview, item.SelectedProvider,
		string(altTitlesRaw), string(workingRaw), string(seasonsRaw), now)
	return err
}

// RemoveUserLibraryItem removes a media item from the user's library.
func (d *Database) RemoveUserLibraryItem(userID int64, itemID string) error {
	_, err := d.db.Exec(`DELETE FROM user_libraries WHERE user_id = ? AND (id = ? OR id LIKE ?)`,
		userID, itemID, "%:"+itemID)
	return err
}

// GetUserLibrary returns all items saved by a user, newest to oldest.
func (d *Database) GetUserLibrary(userID int64) ([]config.LibraryItem, error) {
	rows, err := d.db.Query(`
		SELECT id, tmdb_id, title, original_title, year, media_type, poster_url, backdrop_url, overview, selected_provider, alt_titles_json, working_providers_json, seasons_json, added_at
		FROM user_libraries
		WHERE user_id = ?
		ORDER BY added_at DESC`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []config.LibraryItem
	for rows.Next() {
		var it config.LibraryItem
		var altRaw, workRaw, seasRaw sql.NullString
		err := rows.Scan(
			&it.ID, &it.TMDBID, &it.Title, &it.OriginalTitle, &it.Year, &it.Type,
			&it.PosterURL, &it.BackdropURL, &it.Overview, &it.SelectedProvider,
			&altRaw, &workRaw, &seasRaw, &it.AddedAt,
		)
		if err != nil {
			return nil, err
		}
		if altRaw.Valid && altRaw.String != "" {
			_ = json.Unmarshal([]byte(altRaw.String), &it.AltTitles)
		}
		if workRaw.Valid && workRaw.String != "" {
			_ = json.Unmarshal([]byte(workRaw.String), &it.WorkingProviders)
		}
		if seasRaw.Valid && seasRaw.String != "" {
			_ = json.Unmarshal([]byte(seasRaw.String), &it.Seasons)
		}
		out = append(out, it)
	}
	return out, rows.Err()
}

// GetUserLibraryByType returns items of a specific mediaType for a user.
func (d *Database) GetUserLibraryByType(userID int64, mediaType string) ([]config.LibraryItem, error) {
	all, err := d.GetUserLibrary(userID)
	if err != nil {
		return nil, err
	}
	var filtered []config.LibraryItem
	for _, it := range all {
		if strings.EqualFold(it.Type, mediaType) {
			filtered = append(filtered, it)
		}
	}
	return filtered, nil
}

// GetUserLibraryItemByTMDB finds a user's library item by TMDB ID.
func (d *Database) GetUserLibraryItemByTMDB(userID int64, mediaType string, tmdbID int) (*config.LibraryItem, bool) {
	all, err := d.GetUserLibrary(userID)
	if err != nil {
		return nil, false
	}
	for _, it := range all {
		if it.TMDBID == tmdbID {
			if mediaType == "" || strings.EqualFold(it.Type, mediaType) ||
				(mediaType == "series" && it.Type == "anime") ||
				(mediaType == "anime" && it.Type == "series") {
				return &it, true
			}
		}
	}
	return nil, false
}

// UpdateUserProvider updates the selected provider for an item in a user's library.
func (d *Database) UpdateUserProvider(userID int64, itemID, provider string) error {
	_, err := d.db.Exec(`UPDATE user_libraries SET selected_provider = ? WHERE user_id = ? AND id = ?`,
		provider, userID, itemID)
	return err
}

// UpdateUserItemSeasons caches TMDB season and episode metadata in the user's library item.
func (d *Database) UpdateUserItemSeasons(userID int64, itemID string, seasons []config.LibrarySeason, altTitles []string) error {
	seasonsRaw, _ := json.Marshal(seasons)
	altRaw, _ := json.Marshal(altTitles)
	_, err := d.db.Exec(`UPDATE user_libraries SET seasons_json = ?, alt_titles_json = ? WHERE user_id = ? AND id = ?`,
		string(seasonsRaw), string(altRaw), userID, itemID)
	return err
}
