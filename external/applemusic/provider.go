package applemusic

import (
	"fmt"

	"cliamp/playlist"
)

const trackURIPrefix = "applemusic:track:"

// AppleMusicProvider implements playlist.Provider using the cliamp-apple-music
// Swift helper for Apple Music library browsing and streaming playback.
type AppleMusicProvider struct {
	helper *Helper
}

// New creates an AppleMusicProvider. Returns nil if the helper binary is
// not available (e.g., not on macOS or binary not installed).
func New() *AppleMusicProvider {
	h, err := NewHelper()
	if err != nil {
		fmt.Printf("Apple Music: %v\n", err)
		return nil
	}
	return &AppleMusicProvider{helper: h}
}

func (p *AppleMusicProvider) Name() string { return "Apple Music" }

// Playlists returns the user's Apple Music library playlists.
func (p *AppleMusicProvider) Playlists() ([]playlist.PlaylistInfo, error) {
	resp, err := p.helper.Send(Command{Cmd: "playlists"})
	if err != nil {
		return nil, fmt.Errorf("apple music playlists: %w", err)
	}
	if resp.Type == "error" {
		return nil, fmt.Errorf("apple music playlists: %s", resp.Message)
	}

	var playlists []playlist.PlaylistInfo
	for _, item := range resp.Playlists() {
		playlists = append(playlists, playlist.PlaylistInfo{
			ID:         item.ID,
			Name:       item.Name,
			TrackCount: item.TrackCount,
		})
	}
	return playlists, nil
}

// Tracks returns tracks for a given playlist ID.
func (p *AppleMusicProvider) Tracks(playlistID string) ([]playlist.Track, error) {
	resp, err := p.helper.Send(Command{Cmd: "tracks", PlaylistID: playlistID})
	if err != nil {
		return nil, fmt.Errorf("apple music tracks: %w", err)
	}
	if resp.Type == "error" {
		return nil, fmt.Errorf("apple music tracks: %s", resp.Message)
	}

	return tracksFromItems(resp.Tracks()), nil
}

// Search searches the Apple Music catalog. Returns tracks matching the query.
func (p *AppleMusicProvider) Search(query string, limit int) ([]playlist.Track, error) {
	resp, err := p.helper.Send(Command{
		Cmd:   "search",
		Query: query,
		Type:  "songs",
		Limit: limit,
	})
	if err != nil {
		return nil, fmt.Errorf("apple music search: %w", err)
	}
	if resp.Type == "error" {
		return nil, fmt.Errorf("apple music search: %s", resp.Message)
	}

	return tracksFromItems(resp.Tracks()), nil
}

// Play tells the helper to start playing a track by Apple Music ID.
func (p *AppleMusicProvider) Play(trackID string) error {
	return p.helper.SendAsync(Command{Cmd: "play", TrackID: trackID})
}

// Pause pauses playback.
func (p *AppleMusicProvider) Pause() error {
	return p.helper.SendAsync(Command{Cmd: "pause"})
}

// Resume resumes playback.
func (p *AppleMusicProvider) Resume() error {
	return p.helper.SendAsync(Command{Cmd: "resume"})
}

// Next skips to the next track.
func (p *AppleMusicProvider) Next() error {
	return p.helper.SendAsync(Command{Cmd: "next"})
}

// Previous goes to the previous track.
func (p *AppleMusicProvider) Previous() error {
	return p.helper.SendAsync(Command{Cmd: "previous"})
}

// Seek seeks to an absolute position in seconds.
func (p *AppleMusicProvider) Seek(seconds float64) error {
	return p.helper.SendAsync(Command{Cmd: "seek", Seconds: seconds})
}

// State returns the current playback state.
func (p *AppleMusicProvider) State() Response {
	return p.helper.State()
}

// OnStateChange registers a callback for playback state changes.
func (p *AppleMusicProvider) OnStateChange(cb func(Response)) {
	p.helper.OnStateChange(cb)
}

// Helper returns the underlying helper for direct IPC if needed.
func (p *AppleMusicProvider) Helper() *Helper {
	return p.helper
}

// Close shuts down the helper process.
func (p *AppleMusicProvider) Close() error {
	return p.helper.Close()
}

// IsAppleMusicURI returns true if the given URI is an Apple Music track.
func IsAppleMusicURI(uri string) bool {
	return len(uri) > len(trackURIPrefix) && uri[:len(trackURIPrefix)] == trackURIPrefix
}

// TrackIDFromURI extracts the Apple Music track ID from an applemusic: URI.
func TrackIDFromURI(uri string) string {
	if !IsAppleMusicURI(uri) {
		return ""
	}
	return uri[len(trackURIPrefix):]
}

// tracksFromItems converts TrackItems to playlist.Tracks.
func tracksFromItems(items []TrackItem) []playlist.Track {
	var tracks []playlist.Track
	for _, item := range items {
		tracks = append(tracks, playlist.Track{
			Path:         trackURIPrefix + item.ID,
			Title:        item.Title,
			Artist:       item.Artist,
			Album:        item.Album,
			Genre:        item.Genre,
			Year:         item.Year,
			TrackNumber:  item.TrackNumber,
			DurationSecs: item.Duration,
			Stream:       true,
		})
	}
	return tracks
}
