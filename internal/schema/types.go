package schema

// ScrobblePayload is what the Navidrome plugin POSTs to your API.
// Also used by the API server to receive and parse it.
type ScrobblePayload struct {
	Title       string  `json:"title"`
	Artist      string  `json:"artist"`
	Album       string  `json:"album"`
	Duration    float32 `json:"duration"`
	TrackNumber int32   `json:"trackNumber"`
	DiscNumber  int32   `json:"discNumber"`
	Timestamp   int64   `json:"timestamp"`
	NavidromeID string  `json:"navidromeId"`
	MBID        string  `json:"mbid,omitempty"`
}
