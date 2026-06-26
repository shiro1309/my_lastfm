package main

import (
	"bufio"
	"database/sql"
	"encoding/json"
	"fm-scraper/internal/schema"
	"fmt"
	"net/http"
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
func commitScrobble(
	artist, album, track string,
	dur int,
	pic, cover, trackCover string,
	navidromeID, mbid string,
	ts int64,
	source string,
) bool {
	if db == nil {
		return false
	}

	a := strings.TrimSpace(artist)
	al := strings.TrimSpace(album)
	tr := strings.TrimSpace(track)

	if source == "" {
		source = "live"
	}

	dbMu.Lock()
	defer dbMu.Unlock()

	// 1. Upsert artist
	if _, err := db.Exec(`
		INSERT INTO artists (name, picture_url, total_plays) VALUES (?, ?, 0)
		ON CONFLICT(name) DO UPDATE SET
		picture_url = CASE WHEN ? != '' THEN ? ELSE picture_url END`,
		a, pic, pic, pic); err != nil {
		fmt.Printf("commitScrobble: artist insert failed for %q: %v\n", a, err)
		return false
	}
	var artistID int64
	if err := db.QueryRow(`SELECT rowid FROM artists WHERE name = ?`, a).Scan(&artistID); err != nil {
		fmt.Printf("commitScrobble: artist lookup failed for %q: %v\n", a, err)
		return false
	}

	// 2. Upsert album (nil for singles)
	var albumID sql.NullInt64
	if al != "" {
		if _, err := db.Exec(`
			INSERT INTO albums (artist_id, name, cover_url, total_plays) VALUES (?, ?, ?, 0)
			ON CONFLICT(artist_id, name) DO UPDATE SET
			cover_url = CASE WHEN ? != '' THEN ? ELSE cover_url END`,
			artistID, al, cover, cover, cover); err != nil {
			fmt.Printf("commitScrobble: album insert failed for %q/%q: %v\n", a, al, err)
			return false
		}
		var id int64
		if err := db.QueryRow(`SELECT rowid FROM albums WHERE artist_id = ? AND name = ?`, artistID, al).Scan(&id); err != nil {
			fmt.Printf("commitScrobble: album lookup failed for %q/%q: %v\n", a, al, err)
			return false
		}
		albumID = sql.NullInt64{Int64: id, Valid: true}
	}

	// 3. Upsert track
	if _, err := db.Exec(`
		INSERT INTO tracks (artist_id, album_id, navidrome_id, mbid, name, duration, cover_url)
		VALUES (?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(artist_id, name) DO UPDATE SET
		duration     = CASE WHEN ? > 0 AND duration = 0 THEN ? ELSE duration END,
		album_id     = CASE WHEN album_id IS NULL AND ? IS NOT NULL THEN ? ELSE album_id END,
		navidrome_id = CASE WHEN ? != '' THEN ? ELSE navidrome_id END,
		mbid         = CASE WHEN ? != '' THEN ? ELSE mbid END,
		cover_url    = CASE WHEN ? != '' THEN ? ELSE cover_url END`,
		artistID, albumID, navidromeID, mbid, tr, dur, trackCover,
		dur, dur,
		albumID, albumID,
		navidromeID, navidromeID,
		mbid, mbid,
		trackCover, trackCover); err != nil {
		fmt.Printf("commitScrobble: track insert failed for %q/%q: %v\n", a, tr, err)
		return false
	}
	var trackID int64
	if err := db.QueryRow(`SELECT rowid FROM tracks WHERE artist_id = ? AND name = ?`, artistID, tr).Scan(&trackID); err != nil {
		fmt.Printf("commitScrobble: track lookup failed for %q/%q: %v\n", a, tr, err)
		return false
	}

	// 4. Insert scrobble
	res, err := db.Exec(`
		INSERT OR IGNORE INTO scrobbles (track_id, played_at, source) VALUES (?, ?, ?)`,
		trackID, ts, source)
	if err != nil {
		fmt.Printf("commitScrobble: scrobble insert failed for track_id=%d ts=%d: %v\n", trackID, ts, err)
		return false
	}

	// 5. Update metrics only if scrobble was new
	if rows, _ := res.RowsAffected(); rows > 0 {
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
		return true
	}
	return false
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
