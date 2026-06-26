package main

import (
	"database/sql"
	"fmt"

	_ "modernc.org/sqlite"
)

var navidromeDB *sql.DB

func initBackfill(dbPath string) error {
	var err error
	navidromeDB, err = sql.Open("sqlite", "file:"+dbPath+"?mode=ro")
	if err != nil {
		return fmt.Errorf("open: %w", err)
	}
	if err = navidromeDB.Ping(); err != nil {
		return fmt.Errorf("connect: %w", err)
	}
	return nil
}

func runBackfill() (imported, skipped int) {
	if navidromeDB == nil {
		fmt.Println("backfill: navidrome DB not initialized")
		return
	}

	rows, err := navidromeDB.Query(`
		SELECT
			id,
			title,
			artist,
			COALESCE(album, ''),
			CAST(duration AS INTEGER),
			play_count,
			COALESCE(mbz_recording_id, '')
		FROM media_file
		WHERE play_count > 0
		ORDER BY artist, album, title
	`)
	if err != nil {
		fmt.Printf("backfill: query failed: %v\n", err)
		return
	}
	defer rows.Close()

	for rows.Next() {
		var (
			navidromeID string
			title       string
			artist      string
			album       string
			duration    int
			playCount   int
			mbid        string
		)

		if err := rows.Scan(&navidromeID, &title, &artist, &album, &duration, &playCount, &mbid); err != nil {
			fmt.Printf("backfill: scan error: %v\n", err)
			continue
		}

		if backfillTrack(navidromeID, title, artist, album, duration, playCount, mbid) {
			imported++
		} else {
			skipped++
		}
	}

	fmt.Printf("backfill: done — imported: %d skipped: %d\n", imported, skipped)
	return
}

func backfillTrack(navidromeID, title, artist, album string, duration, playCount int, mbid string) bool {
	coverArtID, navAlbumID, navArtistID := navidromeGetSong(navidromeID)
	pic, picSource := fetchArtistImage(navArtistID, artist)

	dbMu.Lock()
	defer dbMu.Unlock()

	artistID, ok := upsertArtist(artist, pic, picSource)
	if !ok {
		return false
	}

	albumID, trackCover, ok := upsertAlbum(artistID, album, coverArtID, navAlbumID)
	if !ok {
		return false
	}

	_, ok = upsertTrackWithCount(artistID, albumID, title, duration, playCount, trackCover, navidromeID, mbid)
	return ok
}

func upsertTrackWithCount(artistID int64, albumID sql.NullInt64, name string, dur, playCount int, trackCover, navidromeID, mbid string) (int64, bool) {
	if _, err := db.Exec(`
		INSERT INTO tracks (artist_id, album_id, navidrome_id, mbid, name, duration, cover_url, play_count)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(artist_id, name) DO UPDATE SET
		duration     = CASE WHEN ? > 0 AND duration = 0 THEN ? ELSE duration END,
		album_id     = CASE WHEN album_id IS NULL AND ? IS NOT NULL THEN ? ELSE album_id END,
		navidrome_id = CASE WHEN ? != '' THEN ? ELSE navidrome_id END,
		mbid         = CASE WHEN ? != '' THEN ? ELSE mbid END,
		cover_url    = CASE WHEN ? != '' THEN ? ELSE cover_url END,
		play_count   = play_count + ?`,
		artistID, albumID, navidromeID, mbid, name, dur, trackCover, playCount,
		dur, dur,
		albumID, albumID,
		navidromeID, navidromeID,
		mbid, mbid,
		trackCover, trackCover,
		playCount); err != nil {
		fmt.Printf("upsertTrackWithCount: %q: %v\n", name, err)
		return 0, false
	}
	var id int64
	if err := db.QueryRow(`SELECT rowid FROM tracks WHERE artist_id = ? AND name = ?`, artistID, name).Scan(&id); err != nil {
		fmt.Printf("upsertTrackWithCount lookup: %q: %v\n", name, err)
		return 0, false
	}
	return id, true
}
