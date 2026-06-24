package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"fm-scraper/internal/schema"
)

func main() {
	http.HandleFunc("/api/scrobble", handleScrobble)
	http.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		fmt.Fprintf(w, `{"status":"ok"}`)
	})

	fmt.Println("MVP scrobble listener on :8080")
	http.ListenAndServe(":8080", nil)
}

func handleScrobble(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "POST only", http.StatusMethodNotAllowed)
		return
	}

	var payload schema.ScrobblePayload
	if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
		http.Error(w, fmt.Sprintf("Bad payload: %v", err), http.StatusBadRequest)
		return
	}

	// Just print it
	fmt.Printf("\n[SCROBBLE] %s - %s - %s (%ds)\n",
		payload.Artist, payload.Album, payload.Title, int(payload.Duration))
	fmt.Printf("  Timestamp: %d (%s)\n", payload.Timestamp,
		time.Unix(payload.Timestamp, 0).Format("2006-01-02 15:04:05"))
	fmt.Printf("  Track: %d, Disc: %d\n", payload.TrackNumber, payload.DiscNumber)
	fmt.Printf("  NavidromeID: %s\n", payload.NavidromeID)
	fmt.Printf("  MBID: %s\n\n", payload.MBID)

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"status": "received"})
}
