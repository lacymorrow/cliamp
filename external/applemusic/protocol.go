package applemusic

import "encoding/json"

// Command is a JSON command sent to the Swift helper via stdin.
type Command struct {
	Cmd        string  `json:"cmd"`
	PlaylistID string  `json:"playlistId,omitempty"`
	TrackID    string  `json:"trackId,omitempty"`
	Query      string  `json:"query,omitempty"`
	Type       string  `json:"type,omitempty"`
	Limit      int     `json:"limit,omitempty"`
	StartIndex int     `json:"startIndex,omitempty"`
	Position   string  `json:"position,omitempty"`
	Seconds    float64 `json:"seconds,omitempty"`
	Delta      float64 `json:"delta,omitempty"`
	Enabled    *bool   `json:"enabled,omitempty"`
	Mode       string  `json:"mode,omitempty"`
	Volume     float64 `json:"volume,omitempty"`
}

// Response is a generic JSON response from the helper via stdout.
// Different response types populate different fields.
type Response struct {
	Type    string `json:"type"`
	Status  string `json:"status,omitempty"`
	Message string `json:"message,omitempty"`
	Code    string `json:"code,omitempty"`

	// Raw items — parsed into the correct type based on Type
	RawItems json.RawMessage `json:"items,omitempty"`
	Entries  json.RawMessage `json:"entries,omitempty"`

	// Metadata
	PlaylistID string `json:"playlistId,omitempty"`
	Query      string `json:"query,omitempty"`
	SearchType string `json:"searchType,omitempty"`
	TrackID    string `json:"trackId,omitempty"`

	// State fields (from "state" events)
	Playing     bool    `json:"playing,omitempty"`
	PositionS   float64 `json:"position,omitempty"`
	Duration    float64 `json:"duration,omitempty"`
	Title       string  `json:"title,omitempty"`
	Artist      string  `json:"artist,omitempty"`
	Album       string  `json:"album,omitempty"`
	ArtworkURL  string  `json:"artworkUrl,omitempty"`
	ShuffleMode string  `json:"shuffleMode,omitempty"`
	RepeatMode  string  `json:"repeatMode,omitempty"`
}

// Playlists parses the items as playlist entries.
func (r *Response) Playlists() []PlaylistItem {
	if r.RawItems == nil {
		return nil
	}
	var items []PlaylistItem
	json.Unmarshal(r.RawItems, &items)
	return items
}

// Tracks parses the items as track entries.
func (r *Response) Tracks() []TrackItem {
	if r.RawItems == nil {
		return nil
	}
	var items []TrackItem
	json.Unmarshal(r.RawItems, &items)
	return items
}

// QueueEntries parses the entries as track entries.
func (r *Response) QueueEntries() []TrackItem {
	if r.Entries == nil {
		return nil
	}
	var items []TrackItem
	json.Unmarshal(r.Entries, &items)
	return items
}

// PlaylistItem represents a playlist from the helper.
type PlaylistItem struct {
	ID           string `json:"id"`
	Name         string `json:"name"`
	TrackCount   int    `json:"trackCount,omitempty"`
	LastModified string `json:"lastModified,omitempty"`
}

// TrackItem represents a track from the helper.
type TrackItem struct {
	ID          string `json:"id"`
	Title       string `json:"title"`
	Artist      string `json:"artist"`
	Album       string `json:"album,omitempty"`
	Genre       string `json:"genre,omitempty"`
	Year        int    `json:"year,omitempty"`
	TrackNumber int    `json:"trackNumber,omitempty"`
	Duration    int    `json:"duration,omitempty"`
	ArtworkURL  string `json:"artworkUrl,omitempty"`
}
