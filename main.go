package main

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	_ "modernc.org/sqlite"
)

// --- GLOBALS ---

var (
	db                 *sql.DB
	dbMu               sync.Mutex
	deezerCache        = make(map[string][]DeezerTrack)
	cacheMu            sync.Mutex
	deezerTrackCache   = make(map[string][]DeezerTrack)
	deezerPictureCache = make(map[string]string)
)

// --- MAIN ---

func main() {
	initEnv()
	isNew, err := initDB()
	if err != nil {
		fmt.Printf("DB init failed: %v\n", err)
		os.Exit(1)
	}

	apiKey := os.Getenv("LASTFM_API_KEY")
	user := os.Getenv("LASTFM_USER")

	if apiKey == "" || user == "" {
		fmt.Println("startup failed -- LASTFM_API_KEY and LASTFM_USER must be set in .env")
		os.Exit(1)
	}

	logoOutRebel()

	// Print startup stats from DB
	var totalScrobbles, totalSecs int64
	if err := db.QueryRow("SELECT total_scrobbles, total_duration_seconds FROM global_metrics WHERE id=1").
		Scan(&totalScrobbles, &totalSecs); err != nil {
		fmt.Printf("warning -- could not read global metrics: %v\n", err)
	}

	fmt.Printf("user          : %s\n", user)
	fmt.Printf("scrobbles     : %d\n", totalScrobbles)
	fmt.Printf("time listened : %dh %dm\n", totalSecs/3600, (totalSecs%3600)/60)

	apiPort := os.Getenv("API_PORT")
	if apiPort == "" {
		apiPort = "8080"
	}

	go startAPIServer(apiPort)
	fmt.Printf("API listening on http://localhost:%s/api\n", apiPort)

	fmt.Println()
	fmt.Println("starting sync...")
	fmt.Println()

	/* probe1, err := fetchScrobbles(user, apiKey, 5, 0, 2)
	fmt.Println(probe1) */

	if isNew {
		runFullBackfill(user, apiKey)
	} else {
		runIncrementalSync(user, apiKey)
	}

	for range time.NewTicker(4 * time.Minute).C {
		runIncrementalSync(user, apiKey)
	}
}

// --- SYNC ---

// runFullBackfill fetches entire Last.fm history oldest to newest.
// Only called once on first launch when the DB is empty.
func runFullBackfill(user, apiKey string) {
	fmt.Println("no local data found -- starting full historical backfill")
	fmt.Println()

	batch_size := 50

	probe, err := fetchScrobbles(user, apiKey, batch_size, 0, 1)
	if err != nil {
		fmt.Printf("backfill failed -- initial probe: %v\n", err)
		return
	}

	totalItems := int(probe.RecentTracks.Attr.Total)
	totalPages := int(probe.RecentTracks.Attr.TotalPages)

	if totalItems == 0 {
		fmt.Println("no scrobble history found on Last.fm -- monitoring active")
		return
	}

	fmt.Printf("found %d scrobbles across %d pages\n", totalItems, totalPages)
	fmt.Println()

	for p := 1; p <= totalPages; p++ {
		// it starts from the last page as in total % batch_size will be the first batch size
		fmt.Printf("[%d/%d] fetching page %d...", p, totalPages, p)

		page, err := fetchScrobbles(user, apiKey, batch_size, 0, p)
		if err != nil {
			fmt.Printf(" error: %v -- retrying once\n", err)
			time.Sleep(2 * time.Second)
			page, err = fetchScrobbles(user, apiKey, batch_size, 0, p)
			if err != nil {
				fmt.Printf(" retry failed -- skipping page %d\n", p)
				continue
			}
		}

		tracks := filterScrobbleTracks(page.RecentTracks.Track)
		logged, skipped := processBatch(tracks)
		fmt.Printf(" logged: %d  skipped: %d\n", logged, skipped)

		time.Sleep(200 * time.Millisecond)
	}
}

// runIncrementalSync fetches only scrobbles newer than the latest
// timestamp in the DB. Called on startup if data exists, then every 4 minutes.
func runIncrementalSync(user, apiKey string) {
	latest := latestTimestamp()
	if latest == 0 {
		return
	}

	probe, err := fetchScrobbles(user, apiKey, 1, latest+1, 1)
	if err != nil {
		fmt.Printf("incremental sync failed: %v\n", err)
		return
	}

	totalNew := int(probe.RecentTracks.Attr.Total)
	if totalNew == 0 {
		return
	}

	limit := totalNew + 10
	resp, err := fetchScrobbles(user, apiKey, limit, latest+1, 1)
	if err != nil {
		fmt.Printf("incremental sync fetch failed: %v\n", err)
		return
	}

	fmt.Printf("[%s] %d new scrobble(s)\n", time.Now().Format("15:04:05"), totalNew)

	logged, skipped := processBatch(filterScrobbleTracks(resp.RecentTracks.Track))
	fmt.Printf("  done -- logged: %d  already existed: %d\n", logged, skipped)
}

func filterScrobbleTracks(tracks []LastFmTrack) []LastFmTrack {
	var out []LastFmTrack
	for _, t := range tracks {
		if t.Attr != nil && t.Attr.NowPlaying == "true" {
			continue
		}
		if t.Date == nil || t.Date.Uts == "" {
			continue
		}
		out = append(out, t)
	}
	return out
}

func processBatch(tracks []LastFmTrack) (logged int, skipped int) {
	// if no tracks, skip
	totalTracks := len(tracks)
	if totalTracks == 0 {
		return
	}

	results := make([]*trackResult, totalTracks)
	var wg sync.WaitGroup
	sem := make(chan struct{}, 5)

	for i := range tracks {
		wg.Add(1)
		i := i
		go func() {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()

			t := tracks[i]
			uts, err := strconv.ParseInt(t.Date.Uts, 10, 64)
			if err != nil {
				return
			}

			album := t.Album.Text
			if album == "" {
				album = "[Unknown Album]"
			}

			dur, pic := executeLivePipeline(t.Artist.Text, t.Name)
			results[i] = &trackResult{t, uts, album, dur, pic}
		}()
	}
	wg.Wait()

	// Serial DB writes -- no lock contention
	for _, r := range results {
		if r == nil {
			continue
		}
		if commitScrobble(r.track.Artist.Text, r.album, r.track.Name, r.dur, r.pic, r.uts) {
			logged++
		} else {
			skipped++
		}
	}
	return
}

// --- METADATA PIPELINE ---

func executeLivePipeline(artist, track string) (int, string) {
	core := stripParens(track)
	key := strings.ToLower(strings.TrimSpace(artist))

	// 1. Last.fm for duration
	dur := lastfmDuration(artist, core)

	// 2. Deezer artist picture (dedicated artist search, cached separately)
	cacheMu.Lock()
	pic, picOk := deezerPictureCache[key]
	if !picOk {
		pic = deezerArtistPicture(artist)
		deezerPictureCache[key] = pic
	}

	// 3. Deezer track search for duration fallback (cached separately)
	tracks, trackOk := deezerTrackCache[key]
	if !trackOk {
		tracks = deezerSearch(fmt.Sprintf(`artist:"%s"`, artist))
		deezerTrackCache[key] = tracks
	}
	cacheMu.Unlock()

	if dur > 0 {
		return dur, pic
	}

	// 4. Local DB fallback for duration
	if dur = localDuration(artist, track); dur > 0 {
		return dur, pic
	}

	// 5. Match Deezer track for duration
	coreLower := strings.ToLower(core)
	for _, t := range tracks {
		if strings.Contains(strings.ToLower(t.Title), coreLower) {
			return t.Duration, pic
		}
	}

	// 6. Direct Deezer track search as last resort
	if dt := deezerSearch(artist + " " + core); len(dt) > 0 {
		return dt[0].Duration, pic
	}

	// 7. Try English portion from parentheses
	if eng := parensText(track); eng != "" {
		if dt := deezerSearch(artist + " " + eng); len(dt) > 0 {
			return dt[0].Duration, pic
		}
	}

	return 0, pic
}

// --- API HELPERS ---

func fetchScrobbles(user, apiKey string, limit int, from int64, page int) (LastFmResponse, error) {
	u := fmt.Sprintf("https://ws.audioscrobbler.com/2.0/?method=user.getrecenttracks&user=%s&api_key=%s&limit=%d&page=%d&format=json",
		url.QueryEscape(user), url.QueryEscape(apiKey), limit, page)
	if from > 0 {
		u += fmt.Sprintf("&from=%d", from)
	}
	var out LastFmResponse
	resp, err := http.Get(u)
	if err != nil {
		return out, err
	}
	defer resp.Body.Close()
	return out, json.NewDecoder(resp.Body).Decode(&out)
}

func lastfmDuration(artist, track string) int {
	u := fmt.Sprintf("https://ws.audioscrobbler.com/2.0/?method=track.getInfo&api_key=%s&artist=%s&track=%s&format=json&autocorrect=1",
		os.Getenv("LASTFM_API_KEY"), url.QueryEscape(artist), url.QueryEscape(track))
	resp, err := http.Get(u)
	if err != nil || resp.StatusCode != 200 {
		return 0
	}
	defer resp.Body.Close()

	var result map[string]map[string]any
	if json.NewDecoder(resp.Body).Decode(&result) != nil {
		return 0
	}
	ms := 0
	switch v := result["track"]["duration"].(type) {
	case string:
		ms, _ = strconv.Atoi(v)
	case float64:
		ms = int(v)
	}
	return ms / 1000
}

func deezerSearch(q string) []DeezerTrack {
	resp, err := http.Get("https://api.deezer.com/search?q=" + url.QueryEscape(q))
	if err != nil || resp.StatusCode != 200 {
		return nil
	}
	defer resp.Body.Close()
	var out struct {
		Data []DeezerTrack `json:"data"`
	}
	json.NewDecoder(resp.Body).Decode(&out)
	return out.Data
}

func deezerArtistPicture(artist string) string {
	resp, err := http.Get("https://api.deezer.com/search/artist?q=" + url.QueryEscape(artist) + "&limit=1")
	if err != nil || resp.StatusCode != 200 {
		return ""
	}
	defer resp.Body.Close()

	var out struct {
		Data []struct {
			Name      string `json:"name"`
			PictureXL string `json:"picture_xl"`
		} `json:"data"`
	}
	if json.NewDecoder(resp.Body).Decode(&out) != nil || len(out.Data) == 0 {
		return ""
	}
	return out.Data[0].PictureXL
}

func localDuration(artist, track string) int {
	if db == nil {
		return 0
	}
	var dur int
	db.QueryRow(`SELECT track_duration FROM scrobbles WHERE artist_name=? COLLATE NOCASE AND track=? COLLATE NOCASE AND track_duration>0 ORDER BY played_at DESC LIMIT 1`,
		artist, track).Scan(&dur)
	return dur
}

// --- DB WRITER ---
func commitScrobble(artist, album, track string, dur int, pic string, ts int64) bool {
	if db == nil {
		return false
	}

	a := strings.TrimSpace(artist)
	al := strings.TrimSpace(album)
	tr := strings.TrimSpace(track)

	dbMu.Lock()
	defer dbMu.Unlock()

	// 1. Upsert artist, get id
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

	// 2. Upsert album, get id
	if _, err := db.Exec(`
		INSERT INTO albums (artist_id, name, total_plays) VALUES (?, ?, 0)
		ON CONFLICT(artist_id, name) DO NOTHING`,
		artistID, al); err != nil {
		fmt.Printf("commitScrobble: album insert failed for %q/%q: %v\n", a, al, err)
		return false
	}
	var albumID int64
	if err := db.QueryRow(`SELECT rowid FROM albums WHERE artist_id = ? AND name = ?`, artistID, al).Scan(&albumID); err != nil {
		fmt.Printf("commitScrobble: album lookup failed for %q/%q: %v\n", a, al, err)
		return false
	}

	// 3. Upsert track, get id
	if _, err := db.Exec(`
		INSERT INTO tracks (artist_id, album_id, name, duration) VALUES (?, ?, ?, ?)
		ON CONFLICT(artist_id, name) DO UPDATE SET
		duration = CASE WHEN ? > 0 AND duration = 0 THEN ? ELSE duration END,
		album_id = CASE WHEN album_id IS NULL THEN ? ELSE album_id END`,
		artistID, albumID, tr, dur, dur, dur, albumID); err != nil {
		fmt.Printf("commitScrobble: track insert failed for %q/%q: %v\n", a, tr, err)
		return false
	}
	var trackID int64
	if err := db.QueryRow(`SELECT rowid FROM tracks WHERE artist_id = ? AND name = ?`, artistID, tr).Scan(&trackID); err != nil {
		fmt.Printf("commitScrobble: track lookup failed for %q/%q: %v\n", a, tr, err)
		return false
	}

	// 4. Insert scrobble
	res, err := db.Exec(`INSERT OR IGNORE INTO scrobbles (track_id, played_at) VALUES (?, ?)`, trackID, ts)
	if err != nil {
		fmt.Printf("commitScrobble: scrobble insert failed for track_id=%d ts=%d: %v\n", trackID, ts, err)
		return false
	}

	if rows, _ := res.RowsAffected(); rows > 0 {
		date := time.Unix(ts, 0).Format("2006-01-02")
		db.Exec(`UPDATE artists SET total_plays = total_plays + 1 WHERE id = ?`, artistID)
		db.Exec(`UPDATE albums SET total_plays = total_plays + 1 WHERE id = ?`, albumID)
		db.Exec(`UPDATE global_metrics SET total_scrobbles = total_scrobbles + 1, total_duration_seconds = total_duration_seconds + ? WHERE id = 1`, dur)
		db.Exec(`INSERT INTO daily_metrics VALUES (?, 1) ON CONFLICT(date) DO UPDATE SET scrobble_count = scrobble_count + 1`, date)
		return true
	}
	return false
}

// --- UTILS ---

func latestTimestamp() int64 {
	if db == nil {
		return 0
	}
	var ts sql.NullInt64
	db.QueryRow("SELECT MAX(played_at) FROM scrobbles").Scan(&ts)
	return ts.Int64
}

var reParens = regexp.MustCompile(`\s*[\(\[][^\)\]]*[\)\]]`)
var reParensCapture = regexp.MustCompile(`[\(\[][^\)\]]+[\)\]]`)

func stripParens(s string) string { return strings.TrimSpace(reParens.ReplaceAllString(s, "")) }
func parensText(s string) string {
	if m := reParensCapture.FindString(s); m != "" {
		return strings.Trim(m, "()[] ")
	}
	return ""
}

func pad(s string, w int) string {
	n := utf8.RuneCountInString(s)
	if n > w {
		return string([]rune(s)[:w-3]) + "..."
	}
	return s + strings.Repeat(" ", w-n)
}
