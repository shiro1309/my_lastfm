package main

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"
)

// --- RESPONSE TYPES ---

type APIArtist struct {
	Rowid      int64  `json:"rowid"`
	Name       string `json:"name"`
	PictureURL string `json:"picture_url"`
	TotalPlays int    `json:"total_plays"`
}

type APIAlbum struct {
	Rowid       int64  `json:"rowid"`
	ArtistRowid int64  `json:"artist_rowid"`
	ArtistName  string `json:"artist_name"`
	Name        string `json:"name"`
	CoverURL    string `json:"cover_url"`
	TotalPlays  int    `json:"total_plays"`
}

type APITrack struct {
	Rowid       int64  `json:"rowid"`
	ArtistRowid int64  `json:"artist_rowid"`
	ArtistName  string `json:"artist_name"`
	AlbumRowid  int64  `json:"album_rowid"`
	AlbumName   string `json:"album_name"`
	Name        string `json:"name"`
	Duration    int    `json:"duration_seconds"`
	CoverURL    string `json:"cover_url"`
}

type APIScrobble struct {
	Rowid         int64  `json:"rowid"`
	TrackRowid    int64  `json:"track_rowid"`
	TrackName     string `json:"track"`
	ArtistName    string `json:"artist"`
	AlbumName     string `json:"album"`
	Duration      int    `json:"duration_seconds"`
	PlayedAt      int64  `json:"played_at"`
	PlayedAtHuman string `json:"played_at_human"`
	CoverURL      string `json:"cover_url"`
}

type APIGlobalMetrics struct {
	TotalScrobbles     int64  `json:"total_scrobbles"`
	TotalDurationSecs  int64  `json:"total_duration_seconds"`
	TotalDurationHuman string `json:"total_duration_human"`
}

type APIDailyMetric struct {
	Date          string `json:"date"`
	ScrobbleCount int    `json:"scrobble_count"`
}

type APIError struct {
	Error string `json:"error"`
}

// --- SERVER ---

func startAPIServer(port string) {
	mux := http.NewServeMux()

	mux.HandleFunc("GET /api/metrics", handleGlobalMetrics)
	mux.HandleFunc("GET /api/metrics/daily", handleDailyMetrics)

	mux.HandleFunc("GET /api/artists", handleArtists)
	mux.HandleFunc("GET /api/artists/{name}", handleArtist)

	mux.HandleFunc("GET /api/albums", handleAlbums)

	mux.HandleFunc("GET /api/tracks", handleTracks)
	mux.HandleFunc("GET /api/tracks/top", handleTopTracks)

	mux.HandleFunc("GET /api/scrobbles", handleScrobbles)
	mux.HandleFunc("GET /api/scrobbles/recent", handleRecentScrobbles)

	http.ListenAndServe(":"+port, mux)
}

// --- HANDLERS ---

func handleGlobalMetrics(w http.ResponseWriter, r *http.Request) {
	var m APIGlobalMetrics
	if err := db.QueryRow("SELECT total_scrobbles, total_duration_seconds FROM global_metrics WHERE id=1").
		Scan(&m.TotalScrobbles, &m.TotalDurationSecs); err != nil {
		jsonError(w, "db error", 500, err)
		return
	}
	m.TotalDurationHuman = formatDuration(m.TotalDurationSecs)
	jsonOK(w, m)
}

func handleDailyMetrics(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	limit := queryInt(q.Get("limit"), 30)
	from := q.Get("from")
	to := q.Get("to")

	query := "SELECT date, scrobble_count FROM daily_metrics"
	var args []any
	var clauses []string
	if from != "" {
		clauses = append(clauses, "date >= ?")
		args = append(args, from)
	}
	if to != "" {
		clauses = append(clauses, "date <= ?")
		args = append(args, to)
	}
	if len(clauses) > 0 {
		query += " WHERE " + strings.Join(clauses, " AND ")
	}

	order := "DESC"
	if q.Get("order") == "asc" {
		order = "ASC"
	}
	query += " ORDER BY date " + order + " LIMIT ?"
	args = append(args, limit)

	rows, err := db.Query(query, args...)
	if err != nil {
		jsonError(w, "db error", 500, err)
		return
	}
	defer rows.Close()

	var results []APIDailyMetric
	for rows.Next() {
		var d APIDailyMetric
		rows.Scan(&d.Date, &d.ScrobbleCount)
		results = append(results, d)
	}
	jsonOK(w, results)
}

func handleArtists(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	limit := queryInt(q.Get("limit"), 50)
	orderBy := "total_plays DESC"
	if q.Get("sort") == "name" {
		orderBy = "name ASC"
	}

	rows, err := db.Query(
		`SELECT rowid, name, COALESCE(picture_url,''), total_plays FROM artists ORDER BY `+orderBy+` LIMIT ?`,
		limit)
	if err != nil {
		jsonError(w, "db error", 500, err)
		return
	}
	defer rows.Close()

	var results []APIArtist
	for rows.Next() {
		var a APIArtist
		rows.Scan(&a.Rowid, &a.Name, &a.PictureURL, &a.TotalPlays)
		results = append(results, a)
	}
	jsonOK(w, results)
}

func handleArtist(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")

	type ArtistDetail struct {
		APIArtist
		Albums []APIAlbum `json:"albums"`
		Tracks []APITrack `json:"tracks"`
	}

	var a APIArtist
	if err := db.QueryRow(
		`SELECT rowid, name, COALESCE(picture_url,''), total_plays FROM artists WHERE name = ? COLLATE NOCASE`, name,
	).Scan(&a.Rowid, &a.Name, &a.PictureURL, &a.TotalPlays); err != nil {
		jsonError(w, "artist not found", 404, err)
		return
	}

	albumRows, err := db.Query(
		`SELECT rowid, artist_id, name, COALESCE(cover_url,''), total_plays FROM albums WHERE artist_id = ? ORDER BY total_plays DESC`, a.Rowid)
	if err != nil {
		jsonError(w, "db error", 500, err)
		return
	}
	defer albumRows.Close()
	var albums []APIAlbum
	for albumRows.Next() {
		var al APIAlbum
		al.ArtistName = a.Name
		albumRows.Scan(&al.Rowid, &al.ArtistRowid, &al.Name, &al.CoverURL, &al.TotalPlays)
		albums = append(albums, al)
	}

	trackRows, err := db.Query(
		`SELECT t.rowid, t.artist_id, t.album_id, t.name, t.duration, COALESCE(al.name,'')
		 FROM tracks t LEFT JOIN albums al ON al.rowid = t.album_id
		 WHERE t.artist_id = ? ORDER BY t.name ASC`, a.Rowid)
	if err != nil {
		jsonError(w, "db error", 500, err)
		return
	}
	defer trackRows.Close()
	var tracks []APITrack
	for trackRows.Next() {
		var t APITrack
		t.ArtistName = a.Name
		trackRows.Scan(&t.Rowid, &t.ArtistRowid, &t.AlbumRowid, &t.Name, &t.Duration, &t.AlbumName)
		tracks = append(tracks, t)
	}

	jsonOK(w, ArtistDetail{a, albums, tracks})
}

func handleAlbums(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	limit := queryInt(q.Get("limit"), 50)
	artist := q.Get("artist")

	query := `SELECT al.rowid, al.artist_id, ar.name, al.name, COALESCE(al.cover_url,''), al.total_plays
    	FROM albums al JOIN artists ar ON ar.rowid = al.artist_id`

	var args []any
	if artist != "" {
		query += " WHERE ar.name = ? COLLATE NOCASE"
		args = append(args, artist)
	}
	query += " ORDER BY al.total_plays DESC LIMIT ?"
	args = append(args, limit)

	rows, err := db.Query(query, args...)
	if err != nil {
		jsonError(w, "db error", 500, err)
		return
	}
	defer rows.Close()

	var results []APIAlbum
	for rows.Next() {
		var al APIAlbum
		rows.Scan(&al.Rowid, &al.ArtistRowid, &al.ArtistName, &al.Name, &al.CoverURL, &al.TotalPlays)
		results = append(results, al)
	}
	jsonOK(w, results)
}

func handleTracks(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	limit := queryInt(q.Get("limit"), 50)
	artist := q.Get("artist")

	query := `SELECT t.rowid, t.artist_id, ar.name, t.album_id, COALESCE(al.name,''), t.name, t.duration, COALESCE(NULLIF(al.cover_url,''), t.cover_url, '')
		FROM tracks t
		JOIN artists ar ON ar.rowid = t.artist_id
		LEFT JOIN albums al ON al.rowid = t.album_id`
	var args []any
	if artist != "" {
		query += " WHERE ar.name = ? COLLATE NOCASE"
		args = append(args, artist)
	}
	query += " ORDER BY t.name ASC LIMIT ?"
	args = append(args, limit)

	rows, err := db.Query(query, args...)
	if err != nil {
		jsonError(w, "db error", 500, err)
		return
	}
	defer rows.Close()

	var results []APITrack
	for rows.Next() {
		var t APITrack
		rows.Scan(&t.Rowid, &t.ArtistRowid, &t.ArtistName, &t.AlbumRowid, &t.AlbumName, &t.Name, &t.Duration, &t.CoverURL)
		results = append(results, t)
	}
	jsonOK(w, results)
}

func handleTopTracks(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	limit := queryInt(q.Get("limit"), 20)
	artist := q.Get("artist")

	type TopTrack struct {
		APITrack
		PlayCount int `json:"play_count"`
	}

	query := `SELECT t.rowid, t.artist_id, ar.name, t.album_id, COALESCE(al.name,''), t.name, t.duration, COALESCE(NULLIF(al.cover_url,''), t.cover_url, ''), COUNT(s.rowid) as plays
		FROM scrobbles s
		JOIN tracks t ON t.rowid = s.track_id
		JOIN artists ar ON ar.rowid = t.artist_id
		LEFT JOIN albums al ON al.rowid = t.album_id`
	var args []any
	if artist != "" {
		query += " WHERE ar.name = ? COLLATE NOCASE"
		args = append(args, artist)
	}
	query += " GROUP BY s.track_id ORDER BY plays DESC LIMIT ?"
	args = append(args, limit)

	rows, err := db.Query(query, args...)
	if err != nil {
		jsonError(w, "db error", 500, err)
		return
	}
	defer rows.Close()

	var results []TopTrack
	for rows.Next() {
		var t TopTrack
		rows.Scan(&t.Rowid, &t.ArtistRowid, &t.ArtistName, &t.AlbumRowid, &t.AlbumName, &t.Name, &t.Duration, &t.CoverURL, &t.PlayCount)
		results = append(results, t)
	}
	jsonOK(w, results)
}

func handleScrobbles(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	limit := queryInt(q.Get("limit"), 50)
	artist := q.Get("artist")
	before := q.Get("before")
	after := q.Get("after")

	query := `SELECT s.rowid, s.track_id, t.name, ar.name, COALESCE(al.name,''), t.duration, s.played_at, COALESCE(NULLIF(al.cover_url,''), t.cover_url, '')
		FROM scrobbles s
		JOIN tracks t ON t.rowid = s.track_id
		JOIN artists ar ON ar.rowid = t.artist_id
		LEFT JOIN albums al ON al.rowid = t.album_id`
	var args []any
	var clauses []string

	if artist != "" {
		clauses = append(clauses, "ar.name = ? COLLATE NOCASE")
		args = append(args, artist)
	}
	if before != "" {
		clauses = append(clauses, "s.played_at < ?")
		args = append(args, before)
	}
	if after != "" {
		clauses = append(clauses, "s.played_at > ?")
		args = append(args, after)
	}
	if len(clauses) > 0 {
		query += " WHERE " + strings.Join(clauses, " AND ")
	}
	query += " ORDER BY s.played_at DESC LIMIT ?"
	args = append(args, limit)

	rows, err := db.Query(query, args...)
	if err != nil {
		jsonError(w, "db error", 500, err)
		return
	}
	defer rows.Close()
	jsonOK(w, scanScrobbles(rows))
}

func handleRecentScrobbles(w http.ResponseWriter, r *http.Request) {
	limit := queryInt(r.URL.Query().Get("limit"), 20)
	rows, err := db.Query(
		`SELECT s.rowid, s.track_id, t.name, ar.name, COALESCE(al.name,''), t.duration, s.played_at, COALESCE(NULLIF(al.cover_url,''), t.cover_url, '')
		 FROM scrobbles s
		 JOIN tracks t ON t.rowid = s.track_id
		 JOIN artists ar ON ar.rowid = t.artist_id
		 LEFT JOIN albums al ON al.rowid = t.album_id
		 ORDER BY s.played_at DESC LIMIT ?`, limit)
	if err != nil {
		jsonError(w, "db error", 500, err)
		return
	}
	defer rows.Close()
	jsonOK(w, scanScrobbles(rows))
}

func scanScrobbles(rows *sql.Rows) []APIScrobble {
	var results []APIScrobble
	for rows.Next() {
		var s APIScrobble
		rows.Scan(&s.Rowid, &s.TrackRowid, &s.TrackName, &s.ArtistName, &s.AlbumName, &s.Duration, &s.PlayedAt, &s.CoverURL)
		s.PlayedAtHuman = time.Unix(s.PlayedAt, 0).Format("2006-01-02 15:04:05")
		results = append(results, s)
	}
	return results
}

func formatDuration(secs int64) string {
	h := secs / 3600
	m := (secs % 3600) / 60
	return strconv.FormatInt(h, 10) + "h " + strconv.FormatInt(m, 10) + "m"
}

func queryInt(s string, def int) int {
	if s == "" {
		return def
	}
	n, err := strconv.Atoi(s)
	if err != nil || n <= 0 {
		return def
	}
	return n
}

func jsonOK(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(v)
}

func jsonError(w http.ResponseWriter, msg string, code int, err error) {
	if err != nil {
		fmt.Printf("[API ERROR] %s: %v\n", msg, err)
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	json.NewEncoder(w).Encode(APIError{msg})
}
