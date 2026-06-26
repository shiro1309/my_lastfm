package main

import (
	"database/sql"
	"fmt"
	"strings"
	"time"
)

// --- SCROBBLE PIPELINE ---

func commitScrobble(artist, album, track string, dur int, navidromeID, mbid string, ts int64, source string) bool {
	if db == nil {
		return false
	}
	if source == "" {
		source = "live"
	}

	coverArtID, navAlbumID, navArtistID := navidromeGetSong(navidromeID)
	pic, picSource := fetchArtistImage(navArtistID, artist)

	dbMu.Lock()
	defer dbMu.Unlock()

	artistID, ok := upsertArtist(strings.TrimSpace(artist), pic, picSource)
	if !ok {
		return false
	}

	albumID, trackCover, ok := upsertAlbum(artistID, strings.TrimSpace(album), coverArtID, navAlbumID)
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

func upsertArtist(name, pic, picSource string) (int64, bool) {
	if _, err := db.Exec(`
        INSERT INTO artists (name, picture_url, picture_source, total_plays) VALUES (?, ?, ?, 0)
        ON CONFLICT(name) DO UPDATE SET
        picture_url    = CASE WHEN ? != '' THEN ? ELSE picture_url END,
        picture_source = CASE WHEN ? != '' THEN ? ELSE picture_source END`,
		name, pic, picSource,
		pic, pic,
		picSource, picSource); err != nil {
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

func upsertAlbum(artistID int64, name, coverArtID, navAlbumID string) (sql.NullInt64, string, bool) {
	if name == "" {
		return sql.NullInt64{}, "", true
	}

	var existingID int64
	err := db.QueryRow(`SELECT rowid FROM albums WHERE artist_id = ? AND name = ? COLLATE NOCASE`, artistID, name).Scan(&existingID)

	if err == sql.ErrNoRows {
		songCount, _ := navidromeGetAlbum(navAlbumID)
		if songCount > 0 && songCount < 3 {
			fmt.Printf("[upsertAlbum] %q has %d tracks — treating as single\n", name, songCount)
			return sql.NullInt64{}, coverArtID, true
		}

		if _, err := db.Exec(`
			INSERT INTO albums (artist_id, navidrome_id, name, cover_url, total_plays) VALUES (?, ?, ?, ?, 0)
			ON CONFLICT(artist_id, name) DO UPDATE SET
			cover_url    = CASE WHEN ? != '' THEN ? ELSE cover_url END,
			navidrome_id = CASE WHEN ? != '' THEN ? ELSE navidrome_id END`,
			artistID, navAlbumID, name, coverArtID,
			coverArtID, coverArtID,
			navAlbumID, navAlbumID); err != nil {
			fmt.Printf("upsertAlbum: %q: %v\n", name, err)
			return sql.NullInt64{}, "", false
		}
	}

	var id int64
	if err := db.QueryRow(`SELECT rowid FROM albums WHERE artist_id = ? AND name = ?`, artistID, name).Scan(&id); err != nil {
		fmt.Printf("upsertAlbum lookup: %q: %v\n", name, err)
		return sql.NullInt64{}, "", false
	}
	return sql.NullInt64{Int64: id, Valid: true}, "", true
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
