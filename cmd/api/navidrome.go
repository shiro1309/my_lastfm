package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"os"
)

// --- NAVIDROME ---
func navCreds() (base, user, pass string) {
	return os.Getenv("NAVIDROME_URL"),
		os.Getenv("NAVIDROME_USER"),
		url.QueryEscape(os.Getenv("NAVIDROME_PASS"))
}

func navGet(endpoint string, v any) error {
	base, user, pass := navCreds()
	if base == "" {
		return fmt.Errorf("NAVIDROME_URL not set")
	}
	u := fmt.Sprintf("%s/rest/%s&u=%s&p=%s&v=1.16.1&c=my_lastfm&f=json",
		base, endpoint, user, pass)
	resp, err := http.Get(u)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	return json.NewDecoder(resp.Body).Decode(v)
}

type songResponse struct {
	SubsonicResponse struct {
		Song struct {
			CoverArt string `json:"coverArt"`
			AlbumID  string `json:"albumId"`
			ArtistID string `json:"artistId"`
		} `json:"song"`
	} `json:"subsonic-response"`
}

type albumResponse struct {
	SubsonicResponse struct {
		Album struct {
			SongCount int    `json:"songCount"`
			CoverArt  string `json:"coverArt"`
		} `json:"album"`
	} `json:"subsonic-response"`
}

type artistInfoResponse struct {
	SubsonicResponse struct {
		ArtistInfo2 struct {
			LargeImageUrl string `json:"largeImageUrl"`
		} `json:"artistInfo2"`
	} `json:"subsonic-response"`
}

func navidromeGetSong(id string) (coverArtID, albumID, artistID string) {
	var r songResponse
	if err := navGet("getSong?id="+url.QueryEscape(id), &r); err != nil {
		return "", "", ""
	}
	s := r.SubsonicResponse.Song
	return s.CoverArt, s.AlbumID, s.ArtistID
}

func navidromeGetAlbum(id string) (songCount int, coverArtID string) {
	var r albumResponse
	if err := navGet("getAlbum?id="+url.QueryEscape(id), &r); err != nil {
		return 0, ""
	}
	a := r.SubsonicResponse.Album
	return a.SongCount, a.CoverArt
}

func navidromeGetArtistImage(id string) string {
	var r artistInfoResponse
	if err := navGet("getArtistInfo2?id="+url.QueryEscape(id), &r); err != nil {
		return ""
	}
	return r.SubsonicResponse.ArtistInfo2.LargeImageUrl
}
