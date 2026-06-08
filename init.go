package main

import (
	"bufio"
	"database/sql"
	"fmt"
	"os"
	"strings"
)

func initEnv() {
	f, err := os.Open(".env")
	if err != nil {
		fmt.Fprintf(os.Stderr, "[ERROR] Failed to open env: %v\n", err)
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

func initDB() (bool, error) {
	var err error
	db, err = sql.Open("sqlite", "data/stats.db")
	if err != nil {
		return false, fmt.Errorf("open: %w", err)
	}

	// Close db if any subsequent initialisation step fails.
	success := false
	defer func() {
		if !success {
			db.Close()
			db = nil
		}
	}()

	db.SetMaxOpenConns(1)

	if err = db.Ping(); err != nil {
		return false, fmt.Errorf("connect: %w", err)
	}

	// WAL mode returns the active mode name; verify it actually switched.
	var walMode string
	if err = db.QueryRow(`PRAGMA journal_mode=WAL`).Scan(&walMode); err != nil {
		return false, fmt.Errorf("pragma journal_mode: %w", err)
	}
	if walMode != "wal" {
		return false, fmt.Errorf("pragma journal_mode: expected wal, got %q (network filesystem?)", walMode)
	}

	pragmas := []struct {
		label string
		sql   string
	}{
		{"synchronous", `PRAGMA synchronous=NORMAL`},
		{"foreign_keys", `PRAGMA foreign_keys=ON`},
	}

	for _, p := range pragmas {
		if _, err = db.Exec(p.sql); err != nil {
			return false, fmt.Errorf("pragma %s: %w", p.label, err)
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
			id          INTEGER PRIMARY KEY AUTOINCREMENT,
			artist_id   INTEGER NOT NULL REFERENCES artists(id),
			name        TEXT NOT NULL COLLATE NOCASE,
			total_plays INTEGER DEFAULT 0,
			UNIQUE(artist_id, name)
		)`,
		`CREATE TABLE IF NOT EXISTS tracks (
			id        INTEGER PRIMARY KEY AUTOINCREMENT,
			artist_id INTEGER NOT NULL REFERENCES artists(id),
			album_id  INTEGER REFERENCES albums(id),
			name      TEXT NOT NULL COLLATE NOCASE,
			duration  INTEGER DEFAULT 0,
			UNIQUE(artist_id, name)
		)`,
		`CREATE TABLE IF NOT EXISTS scrobbles (
			id        INTEGER PRIMARY KEY AUTOINCREMENT,
			track_id  INTEGER NOT NULL REFERENCES tracks(id),
			played_at INTEGER NOT NULL,
			UNIQUE(track_id, played_at)
		)`,
		`CREATE TABLE IF NOT EXISTS global_metrics (
			id                     INTEGER PRIMARY KEY CHECK(id=1),
			total_scrobbles        INTEGER DEFAULT 0,
			total_duration_seconds INTEGER DEFAULT 0
		)`,
		`CREATE TABLE IF NOT EXISTS daily_metrics (
			date           TEXT PRIMARY KEY,
			scrobble_count INTEGER DEFAULT 0
		)`,
		`INSERT OR IGNORE INTO global_metrics VALUES (1, 0, 0)`,
		`CREATE INDEX IF NOT EXISTS idx_scrobbles_played_at ON scrobbles(played_at)`,
		`CREATE INDEX IF NOT EXISTS idx_tracks_artist_id    ON tracks(artist_id)`,
		`CREATE INDEX IF NOT EXISTS idx_albums_artist_id    ON albums(artist_id)`,
	}

	for _, s := range stmts {
		if _, err = db.Exec(s); err != nil {
			return false, fmt.Errorf("schema: %w\nstatement: %s", err, s)
		}
	}

	var count int
	if err = db.QueryRow("SELECT COUNT(*) FROM scrobbles").Scan(&count); err != nil {
		return false, fmt.Errorf("checking existing data: %w", err)
	}

	success = true

	return count == 0, nil
}
