package main

import (
	"encoding/json"
	"fm-scraper/internal/schema"
	"fmt"
	"net/http"
	"time"
)

// --- HTTP ---

func handleScrobble(w http.ResponseWriter, r *http.Request) {
	var payload schema.ScrobblePayload
	if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
		jsonError(w, "invalid payload", http.StatusBadRequest, err)
		return
	}
	defer r.Body.Close()

	ok := commitScrobble(
		payload.Artist, payload.Album, payload.Title,
		int(payload.Duration),
		payload.NavidromeID, payload.MBID,
		payload.Timestamp, "live",
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
