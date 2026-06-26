package main

import (
	"database/sql"
	"fmt"
	"os"

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

	navUser := os.Getenv("NAVIDROME_USER")
	if navUser == "" {
		fmt.Println("backfill: NAVIDROME_USER not set")
		return
	}

	rows, err := navidromeDB.Query(`
	    SELECT
	        mf.id,
	        mf.title,
	        mf.artist,
	        COALESCE(mf.album, ''),
	        CAST(mf.duration AS INTEGER),
	        COALESCE(mf.mbz_recording_id, ''),
	        s.submission_time
	    FROM scrobbles s
	    JOIN media_file mf ON mf.id = s.media_file_id
	    JOIN user u ON u.id = s.user_id
	    WHERE u.user_name = ?
	    ORDER BY s.submission_time ASC
	`, navUser)
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
			mbid        string
			playTime    int64
		)

		if err := rows.Scan(&navidromeID, &title, &artist, &album, &duration, &mbid, &playTime); err != nil {
			fmt.Printf("backfill: scan error: %v\n", err)
			continue
		}

		if backfillTrack(navidromeID, title, artist, album, duration, mbid, playTime) {
			imported++
		} else {
			skipped++
		}
	}

	fmt.Printf("backfill: done — imported: %d skipped: %d\n", imported, skipped)
	return
}

func backfillTrack(navidromeID, title, artist, album string, duration int, mbid string, playTime int64) bool {
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

	trackID, ok := upsertTrack(artistID, albumID, title, duration, trackCover, navidromeID, mbid)
	if !ok {
		return false
	}

	if !insertScrobble(trackID, playTime, "backfill") {
		return false
	}

	updateMetrics(artistID, albumID, duration, playTime)
	return true
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
