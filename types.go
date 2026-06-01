package main

import (
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

type LastFmResponse struct {
	RecentTracks struct {
		Track []LastFmTrack `json:"track"`
		Attr  struct {
			TotalPages intString `json:"totalPages"`
			Total      intString `json:"total"`
		} `json:"@attr"`
	} `json:"recenttracks"`
}

// Deezer types
type DeezerArtist struct {
	Name      string `json:"name"`
	PictureXL string `json:"picture_xl"`
}

type DeezerTrack struct {
	Title    string       `json:"title"`
	Duration int          `json:"duration"`
	Artist   DeezerArtist `json:"artist"`
}

// processBatch type
type trackResult struct {
	track LastFmTrack
	uts   int64
	album string
	dur   int
	pic   string
}
