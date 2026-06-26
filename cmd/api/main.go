package main

import (
	"bufio"
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"strings"
	"sync"

	_ "modernc.org/sqlite"
)

var (
	db   *sql.DB
	dbMu sync.Mutex
)

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
	if err := http.ListenAndServe(":"+port, mux); err != nil {
		fmt.Printf("server failed: %v\n", err)
		os.Exit(1)
	}
}

// --- ENV ---
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

// --- DB ---

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

	for _, p := range []struct{ label, sql string }{
		{"foreign_keys", `PRAGMA foreign_keys=ON`},
		{"synchronous", `PRAGMA synchronous=NORMAL`},
	} {
		if _, err = db.Exec(p.sql); err != nil {
			return fmt.Errorf("pragma %s: %w", p.label, err)
		}
	}

	stmts := []string{
		`CREATE TABLE IF NOT EXISTS artists (
		    id             INTEGER PRIMARY KEY AUTOINCREMENT,
		    name           TEXT NOT NULL UNIQUE COLLATE NOCASE,
		    picture_url    TEXT DEFAULT '',
		    picture_source TEXT DEFAULT '',
		    total_plays    INTEGER DEFAULT 0
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

func fetchArtistImage(artistNavidromeID, artistName string) (url, source string) {
	// 1. try Navidrome
	if artistNavidromeID != "" {
		if img := navidromeGetArtistImage(artistNavidromeID); img != "" {
			return img, "navidrome"
		}
	}

	// 2. fall back to Deezer
	if img := deezerArtistPicture(artistName); img != "" {
		return img, "deezer"
	}

	return "", ""
}

func deezerArtistPicture(artist string) string {
	resp, err := http.Get("https://api.deezer.com/search/artist?q=" + url.QueryEscape(artist) + "&limit=1")
	if err != nil || resp.StatusCode != 200 {
		return ""
	}
	defer resp.Body.Close()

	var out struct {
		Data []struct {
			PictureXL string `json:"picture_xl"`
		} `json:"data"`
	}
	if json.NewDecoder(resp.Body).Decode(&out) != nil || len(out.Data) == 0 {
		return ""
	}
	return out.Data[0].PictureXL
}
