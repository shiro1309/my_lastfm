package main

import (
	"encoding/json"
	"strconv"
	"strings"
)

type intString int

func (i *intString) UnmarshalJSON(b []byte) error {
	s := strings.Trim(string(b), `"`)
	n, err := strconv.Atoi(s)
	if err != nil {
		return err
	}
	*i = intString(n)
	return nil
}

// Last.fm types
type LastFmArtistText struct {
	Text string `json:"#text"`
}

type LastFmDate struct {
	Uts string `json:"uts"`
}

type LastFmNowPlaying struct {
	NowPlaying string `json:"nowplaying"`
}

type LastFmTrack struct {
	Artist LastFmArtistText  `json:"artist"`
	Name   string            `json:"name"`
	Album  LastFmArtistText  `json:"album"`
	Date   *LastFmDate       `json:"date,omitempty"`
	Attr   *LastFmNowPlaying `json:"@attr,omitempty"`
}

type LastFmRecentTracks struct {
	Track LastFmTrackList `json:"track"`
	Attr  struct {
		TotalPages intString `json:"totalPages"`
		Total      intString `json:"total"`
	} `json:"@attr"`
}

// LastFmTrackList handles both a single object and an array
type LastFmTrackList []LastFmTrack

func (t *LastFmTrackList) UnmarshalJSON(b []byte) error {
	// try array first
	var arr []LastFmTrack
	if err := json.Unmarshal(b, &arr); err == nil {
		*t = arr
		return nil
	}
	// fall back to single object
	var single LastFmTrack
	if err := json.Unmarshal(b, &single); err != nil {
		return err
	}
	*t = []LastFmTrack{single}
	return nil
}

type LastFmResponse struct {
	RecentTracks LastFmRecentTracks `json:"recenttracks"`
}

// Deezer types
type DeezerAlbum struct {
	ID       int    `json:"id"`
	Title    string `json:"title"`
	CoverXL  string `json:"cover_xl"`
	NbTracks int    `json:"nb_tracks"`
}

type DeezerArtist struct {
	Name      string `json:"name"`
	PictureXL string `json:"picture_xl"`
}

type DeezerTrack struct {
	Title    string       `json:"title"`
	Duration int          `json:"duration"`
	Artist   DeezerArtist `json:"artist"`
	Album    DeezerAlbum  `json:"album"`
}

// processBatch type
type trackResult struct {
	track      LastFmTrack
	uts        int64
	album      string // empty string if single
	dur        int
	pic        string
	cover      string
	trackCover string // cover for standalone singles
}
