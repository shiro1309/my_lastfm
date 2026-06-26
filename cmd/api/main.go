package main

import (
	"bufio"
	"database/sql"
	"encoding/json"
	"fm-scraper/internal/schema"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"strings"
	"sync"
	"time"

	_ "modernc.org/sqlite"
)

var (
	db   *sql.DB
	dbMu sync.Mutex
)

func main() {
	initEnv()

	if err := initDB(); err != nil {
		fmt.Printf("DB init failed: %v\n", err)
		os.Exit(1)
	}

	var totalScrobbles, totalSecs int64
	db.QueryRow("SELECT total_scrobbles, total_duration_seconds FROM global_metrics WHERE id=1").
		Scan(&totalScrobbles, &totalSecs)

	fmt.Printf("my_lastfm started\n")
	fmt.Printf("scrobbles     : %d\n", totalScrobbles)
	fmt.Printf("time listened : %dh %dm\n", totalSecs/3600, (totalSecs%3600)/60)
	fmt.Println()

	port := os.Getenv("API_PORT")
	if port == "" {
		port = "8088"
	}

	mux := http.NewServeMux()
	mux.HandleFunc("POST /api/scrobble", handleScrobble)

	fmt.Printf("listening on :%s\n", port)
	fmt.Printf("  POST /api/scrobble\n")

	if err := http.ListenAndServe(":"+port, mux); err != nil {
		fmt.Printf("server failed: %v\n", err)
		os.Exit(1)
	}
}

// --- Init ---
func initDB() error {
	var err error
	dbPath := os.Getenv("DB_PATH")
	if dbPath == "" {
		dbPath = "data/stats.db"
	}
	fmt.Printf("opening DB at: %s\n", dbPath)

	db, err = sql.Open("sqlite", dbPath)
	if err != nil {
		return fmt.Errorf("open: %w", err)
	}

	success := false
	defer func() {
		if !success {
			db.Close()
			db = nil
		}
	}()

	db.SetMaxOpenConns(1)

	if err = db.Ping(); err != nil {
		return fmt.Errorf("connect: %w", err)
	}

	var walMode string
	if err = db.QueryRow(`PRAGMA journal_mode=WAL`).Scan(&walMode); err != nil {
		return fmt.Errorf("pragma journal_mode: %w", err)
	}
	if walMode != "wal" {
		return fmt.Errorf("pragma journal_mode: expected wal, got %q", walMode)
	}

	for _, p := range []struct{ label, sql string }{
		{"synchronous", `PRAGMA synchronous=NORMAL`},
		{"foreign_keys", `PRAGMA foreign_keys=ON`},
	} {
		if _, err = db.Exec(p.sql); err != nil {
			return fmt.Errorf("pragma %s: %w", p.label, err)
		}
	}

	stmts := []string{
		`CREATE TABLE IF NOT EXISTS artists (
			id          INTEGER PRIMARY KEY AUTOINCREMENT,
			name        TEXT NOT NULL UNIQUE COLLATE NOCASE,
			picture_url TEXT DEFAULT '',
			total_plays INTEGER DEFAULT 0
		)`,
		`CREATE TABLE IF NOT EXISTS albums (
			id           INTEGER PRIMARY KEY AUTOINCREMENT,
			artist_id    INTEGER NOT NULL REFERENCES artists(id),
			navidrome_id TEXT DEFAULT '',
			name         TEXT NOT NULL COLLATE NOCASE,
			cover_url    TEXT DEFAULT '',
			total_plays  INTEGER DEFAULT 0,
			UNIQUE(artist_id, name)
		)`,
		`CREATE TABLE IF NOT EXISTS tracks (
			id           INTEGER PRIMARY KEY AUTOINCREMENT,
			artist_id    INTEGER NOT NULL REFERENCES artists(id),
			album_id     INTEGER REFERENCES albums(id),
			navidrome_id TEXT DEFAULT '',
			mbid         TEXT DEFAULT '',
			name         TEXT NOT NULL COLLATE NOCASE,
			duration     INTEGER DEFAULT 0,
			cover_url    TEXT DEFAULT '',
			UNIQUE(artist_id, name)
		)`,
		`CREATE TABLE IF NOT EXISTS scrobbles (
			id        INTEGER PRIMARY KEY AUTOINCREMENT,
			track_id  INTEGER NOT NULL REFERENCES tracks(id),
			played_at INTEGER NOT NULL,
			source    TEXT NOT NULL DEFAULT 'live'
				CHECK(source IN ('live', 'backfill', 'lastfm_import')),
			UNIQUE(track_id, played_at)
		)`,
		`CREATE TABLE IF NOT EXISTS global_metrics (
			id                     INTEGER PRIMARY KEY CHECK(id=1),
			total_scrobbles        INTEGER DEFAULT 0,
			total_duration_seconds INTEGER DEFAULT 0
		)`,
		`CREATE TABLE IF NOT EXISTS daily_metrics (
			date             TEXT PRIMARY KEY,
			scrobble_count   INTEGER DEFAULT 0,
			duration_seconds INTEGER DEFAULT 0
		)`,
		`INSERT OR IGNORE INTO global_metrics VALUES (1, 0, 0)`,
		`CREATE INDEX IF NOT EXISTS idx_scrobbles_played_at ON scrobbles(played_at)`,
		`CREATE INDEX IF NOT EXISTS idx_scrobbles_track_id  ON scrobbles(track_id)`,
		`CREATE INDEX IF NOT EXISTS idx_scrobbles_source    ON scrobbles(source)`,
		`CREATE INDEX IF NOT EXISTS idx_tracks_artist_id    ON tracks(artist_id)`,
		`CREATE INDEX IF NOT EXISTS idx_tracks_navidrome_id ON tracks(navidrome_id)`,
		`CREATE INDEX IF NOT EXISTS idx_albums_artist_id    ON albums(artist_id)`,
		`CREATE INDEX IF NOT EXISTS idx_albums_navidrome_id ON albums(navidrome_id)`,
	}

	for _, s := range stmts {
		if _, err = db.Exec(s); err != nil {
			return fmt.Errorf("schema: %w\nstatement: %s", err, s)
		}
	}

	success = true
	return nil
}

func initEnv() {
	f, err := os.Open(".env")
	if err != nil {
		fmt.Errorf("No env: %w", err)
		return
	}
	defer f.Close()
	s := bufio.NewScanner(f)
	for s.Scan() {
		line := strings.TrimSpace(s.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		if k, v, ok := strings.Cut(line, "="); ok {
			os.Setenv(strings.TrimSpace(k), strings.Trim(strings.TrimSpace(v), `"'`))
		}
	}
}

func logoOutRebel() {
	fmt.Println(`
	 ██████   ██████                      ████                     █████       ██████                 
	▒▒██████ ██████                      ▒▒███                    ▒▒███       ███▒▒███                
	 ▒███▒█████▒███  █████ ████           ▒███   ██████    █████  ███████    ▒███ ▒▒▒  █████████████  
	 ▒███▒▒███ ▒███ ▒▒███ ▒███            ▒███  ▒▒▒▒▒███  ███▒▒  ▒▒▒███▒    ███████   ▒▒███▒▒███▒▒███ 
	 ▒███ ▒▒▒  ▒███  ▒███ ▒███            ▒███   ███████ ▒▒█████   ▒███    ▒▒▒███▒     ▒███ ▒███ ▒███ 
	 ▒███      ▒███  ▒███ ▒███            ▒███  ███▒▒███  ▒▒▒▒███  ▒███ ███  ▒███      ▒███ ▒███ ▒███ 
	 █████     █████ ▒▒███████  █████████ █████▒▒████████ ██████   ▒▒█████   █████     █████▒███ █████
	▒▒▒▒▒     ▒▒▒▒▒   ▒▒▒▒▒███ ▒▒▒▒▒▒▒▒▒ ▒▒▒▒▒  ▒▒▒▒▒▒▒▒ ▒▒▒▒▒▒     ▒▒▒▒▒   ▒▒▒▒▒     ▒▒▒▒▒ ▒▒▒ ▒▒▒▒▒ 
	                  ███ ▒███                                                         Version: 0.0.1
	                 ▒▒██████                                                                         
	                  ▒▒▒▒▒▒                                                                          
	`)
}

// --- DB ---
func commitScrobble(artist, album, track string, dur int, pic, cover, trackCover string, navidromeID, mbid string, ts int64, source string) bool {
	if db == nil {
		return false
	}

	if source == "" {
		source = "live"
	}

	dbMu.Lock()
	defer dbMu.Unlock()

	artistID, ok := upsertArtist(strings.TrimSpace(artist), pic)
	if !ok {
		return false
	}

	albumID, ok := upsertAlbum(artistID, strings.TrimSpace(album), cover, navidromeID)
	if !ok {
		return false
	}

	trackID, ok := upsertTrack(artistID, albumID, strings.TrimSpace(track), dur, trackCover, navidromeID, mbid)
	if !ok {
		return false
	}

	if !insertScrobble(trackID, ts, source) {
		return false
	}

	updateMetrics(artistID, albumID, dur, ts)
	return true
}

func upsertArtist(name, pic string) (int64, bool) {
	if _, err := db.Exec(`
		INSERT INTO artists (name, picture_url, total_plays) VALUES (?, ?, 0)
		ON CONFLICT(name) DO UPDATE SET
		picture_url = CASE WHEN ? != '' THEN ? ELSE picture_url END`,
		name, pic, pic, pic); err != nil {
		fmt.Printf("upsertArtist: %q: %v\n", name, err)
		return 0, false
	}
	var id int64
	if err := db.QueryRow(`SELECT rowid FROM artists WHERE name = ?`, name).Scan(&id); err != nil {
		fmt.Printf("upsertArtist lookup: %q: %v\n", name, err)
		return 0, false
	}
	return id, true
}

func upsertAlbum(artistID int64, name, cover, trackNavidromeID string) (sql.NullInt64, bool) {
	if name == "" {
		return sql.NullInt64{}, true
	}

	// new album — fetch cover from Navidrome
	navAlbumID := ""
	var existingID int64
	if err := db.QueryRow(`SELECT rowid FROM albums WHERE artist_id = ? AND name = ? COLLATE NOCASE`, artistID, name).Scan(&existingID); err == sql.ErrNoRows {
		coverArtID, _ := navidromeGetCoverArt(trackNavidromeID)
		if coverArtID != "" {
			cover = navidromeCoverURL(coverArtID)
			navAlbumID = coverArtID
		}
	}

	if _, err := db.Exec(`
		INSERT INTO albums (artist_id, navidrome_id, name, cover_url, total_plays) VALUES (?, ?, ?, ?, 0)
		ON CONFLICT(artist_id, name) DO UPDATE SET
		cover_url    = CASE WHEN ? != '' THEN ? ELSE cover_url END,
		navidrome_id = CASE WHEN ? != '' THEN ? ELSE navidrome_id END`,
		artistID, navAlbumID, name, cover,
		cover, cover,
		navAlbumID, navAlbumID); err != nil {
		fmt.Printf("upsertAlbum: %q: %v\n", name, err)
		return sql.NullInt64{}, false
	}

	var id int64
	if err := db.QueryRow(`SELECT rowid FROM albums WHERE artist_id = ? AND name = ?`, artistID, name).Scan(&id); err != nil {
		fmt.Printf("upsertAlbum lookup: %q: %v\n", name, err)
		return sql.NullInt64{}, false
	}
	return sql.NullInt64{Int64: id, Valid: true}, true
}

func upsertTrack(artistID int64, albumID sql.NullInt64, name string, dur int, trackCover, navidromeID, mbid string) (int64, bool) {
	if _, err := db.Exec(`
		INSERT INTO tracks (artist_id, album_id, navidrome_id, mbid, name, duration, cover_url)
		VALUES (?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(artist_id, name) DO UPDATE SET
		duration     = CASE WHEN ? > 0 AND duration = 0 THEN ? ELSE duration END,
		album_id     = CASE WHEN album_id IS NULL AND ? IS NOT NULL THEN ? ELSE album_id END,
		navidrome_id = CASE WHEN ? != '' THEN ? ELSE navidrome_id END,
		mbid         = CASE WHEN ? != '' THEN ? ELSE mbid END,
		cover_url    = CASE WHEN ? != '' THEN ? ELSE cover_url END`,
		artistID, albumID, navidromeID, mbid, name, dur, trackCover,
		dur, dur,
		albumID, albumID,
		navidromeID, navidromeID,
		mbid, mbid,
		trackCover, trackCover); err != nil {
		fmt.Printf("upsertTrack: %q: %v\n", name, err)
		return 0, false
	}
	var id int64
	if err := db.QueryRow(`SELECT rowid FROM tracks WHERE artist_id = ? AND name = ?`, artistID, name).Scan(&id); err != nil {
		fmt.Printf("upsertTrack lookup: %q: %v\n", name, err)
		return 0, false
	}
	return id, true
}

func insertScrobble(trackID, ts int64, source string) bool {
	res, err := db.Exec(`
		INSERT OR IGNORE INTO scrobbles (track_id, played_at, source) VALUES (?, ?, ?)`,
		trackID, ts, source)
	if err != nil {
		fmt.Printf("insertScrobble: track_id=%d ts=%d: %v\n", trackID, ts, err)
		return false
	}
	rows, _ := res.RowsAffected()
	return rows > 0
}

func updateMetrics(artistID int64, albumID sql.NullInt64, dur int, ts int64) {
	date := time.Unix(ts, 0).Format("2006-01-02")
	db.Exec(`UPDATE artists SET total_plays = total_plays + 1 WHERE id = ?`, artistID)
	if albumID.Valid {
		db.Exec(`UPDATE albums SET total_plays = total_plays + 1 WHERE id = ?`, albumID.Int64)
	}
	db.Exec(`
		UPDATE global_metrics
		SET total_scrobbles        = total_scrobbles + 1,
		    total_duration_seconds = total_duration_seconds + ?
		WHERE id = 1`, dur)
	db.Exec(`
		INSERT INTO daily_metrics (date, scrobble_count, duration_seconds) VALUES (?, 1, ?)
		ON CONFLICT(date) DO UPDATE SET
		scrobble_count   = scrobble_count + 1,
		duration_seconds = duration_seconds + ?`,
		date, dur, dur)
}

// --- scrobble ---
func handleScrobble(w http.ResponseWriter, r *http.Request) {
	var payload schema.ScrobblePayload
	if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
		jsonError(w, "invalid payload", http.StatusBadRequest, err)
		return
	}
	defer r.Body.Close()

	ok := commitScrobble(
		payload.Artist,
		payload.Album,
		payload.Title,
		int(payload.Duration),
		"", "", "",
		payload.NavidromeID,
		payload.MBID,
		payload.Timestamp,
		"live",
	)

	if ok {
		fmt.Printf("[%s] scrobbled: %s - %s (%ds)\n",
			time.Now().Format("15:04:05"),
			payload.Artist, payload.Title, int(payload.Duration))
		jsonOK(w, map[string]string{"status": "ok"})
	} else {
		jsonError(w, "failed to save scrobble", http.StatusInternalServerError, nil)
	}
}

// --- utils ---
func jsonOK(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(v)
}

func jsonError(w http.ResponseWriter, msg string, code int, err error) {
	if err != nil {
		fmt.Printf("[ERROR] %s: %v\n", msg, err)
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	json.NewEncoder(w).Encode(map[string]string{"error": msg})
}

type ScrobblePayload struct {
	Title       string  `json:"title"`
	Artist      string  `json:"artist"`
	Album       string  `json:"album"`
	Duration    float32 `json:"duration"`
	TrackNumber int32   `json:"trackNumber"`
	DiscNumber  int32   `json:"discNumber"`
	Timestamp   int64   `json:"timestamp"`
	NavidromeID string  `json:"navidromeId"`
	MBID        string  `json:"mbid,omitempty"`
}

type NavidromeTrackResponse struct {
	SubsonicResponse struct {
		Song struct {
			CoverArt string `json:"coverArt"`
			AlbumID  string `json:"albumId"`
		} `json:"song"`
	} `json:"subsonic-response"`
}

func navidromeGetCoverArt(trackNavidromeID string) (coverArtID string, albumID string) {
	base := os.Getenv("NAVIDROME_URL")
	user := os.Getenv("NAVIDROME_USER")
	pass := os.Getenv("NAVIDROME_PASS")

	if base == "" || user == "" || pass == "" {
		return "", ""
	}

	u := fmt.Sprintf("%s/rest/getSong?v=1.16.1&c=my_lastfm&f=json&u=%s&p=%s&id=%s",
		base,
		url.QueryEscape(user),
		url.QueryEscape(pass),
		url.QueryEscape(trackNavidromeID),
	)

	resp, err := http.Get(u)
	if err != nil || resp.StatusCode != 200 {
		fmt.Printf("[navidrome] getSong failed for %s: %v\n", trackNavidromeID, err)
		return "", ""
	}
	defer resp.Body.Close()

	var result NavidromeTrackResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		fmt.Printf("[navidrome] decode failed: %v\n", err)
		return "", ""
	}

	return result.SubsonicResponse.Song.CoverArt, result.SubsonicResponse.Song.AlbumID
}

func navidromeCoverURL(coverArtID string) string {
	base := os.Getenv("NAVIDROME_URL")
	user := os.Getenv("NAVIDROME_USER")
	pass := os.Getenv("NAVIDROME_PASS")

	if base == "" || coverArtID == "" {
		return ""
	}

	return fmt.Sprintf("%s/rest/getCoverArt?v=1.16.1&c=my_lastfm&f=json&u=%s&p=%s&id=%s&size=300",
		base,
		url.QueryEscape(user),
		url.QueryEscape(pass),
		url.QueryEscape(coverArtID),
	)
}
