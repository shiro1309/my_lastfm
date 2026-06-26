package main

import (
	"encoding/json"
	"fmt"

	"fm-scraper/internal/schema"

	pdk "github.com/extism/go-pdk"
)

type TrackInfo struct {
	ID             string  `json:"id"`
	Title          string  `json:"title"`
	Artist         string  `json:"artist"`
	Album          string  `json:"album"`
	AlbumArtist    string  `json:"albumArtist"`
	Duration       float32 `json:"duration"`
	TrackNumber    int32   `json:"trackNumber"`
	DiscNumber     int32   `json:"discNumber"`
	MbzRecordingID string  `json:"mbzRecordingId"`
	MbzAlbumID     string  `json:"mbzAlbumId"`
	MbzArtistID    string  `json:"mbzArtistId"`
}

type ScrobbleRequest struct {
	Username  string    `json:"username"`
	Track     TrackInfo `json:"track"`
	Timestamp int64     `json:"timestamp"`
}

//go:wasmexport nd_scrobbler_is_authorized
func isAuthorized() int32 {
	apiURL, ok := pdk.GetConfig("api_url")
	if !ok || apiURL == "" {
		pdk.SetError(fmt.Errorf("scrobbler(not_authorized)"))
		return 1
	}
	pdk.OutputString("true")
	return 0
}

//go:wasmexport nd_scrobbler_now_playing
func nowPlaying() int32 {
	return 0
}

//go:wasmexport nd_scrobbler_scrobble
func scrobble() int32 {
	var req ScrobbleRequest
	if err := pdk.InputJSON(&req); err != nil {
		pdk.SetError(fmt.Errorf("scrobbler(unrecoverable)"))
		return 1
	}

	payload := schema.ScrobblePayload{
		Title:       req.Track.Title,
		Artist:      req.Track.Artist,
		Album:       req.Track.Album,
		Duration:    req.Track.Duration,
		TrackNumber: req.Track.TrackNumber,
		DiscNumber:  req.Track.DiscNumber,
		Timestamp:   req.Timestamp,
		NavidromeID: req.Track.ID,
		MBID:        req.Track.MbzRecordingID,
	}

	apiURL, _ := pdk.GetConfig("api_url")
	apiKey, _ := pdk.GetConfig("api_key")

	if err := sendPayload(payload, apiURL, apiKey); err != nil {
		pdk.SetError(fmt.Errorf("scrobbler(retry_later)"))
		return 1
	}
	return 0
}

func sendPayload(payload schema.ScrobblePayload, apiURL, apiKey string) error {
	body, err := json.Marshal(payload)
	if err != nil {
		return err
	}

	req := pdk.NewHTTPRequest(pdk.MethodPost, apiURL)
	req.SetHeader("Content-Type", "application/json")
	if apiKey != "" {
		req.SetHeader("Authorization", "Bearer "+apiKey)
	}
	req.SetBody(body)

	res := req.Send()
	if res.Status() < 200 || res.Status() >= 300 {
		return fmt.Errorf("http %d", res.Status())
	}
	return nil
}

func main() {}
