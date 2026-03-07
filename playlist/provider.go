package playlist

import "errors"

// ErrNeedsAuth is returned by providers that require interactive sign-in
// before they can be used.
var ErrNeedsAuth = errors.New("sign-in required")

type PlaylistInfo struct {
	ID         string
	Name       string
	TrackCount int
}

type Provider interface {
	// Provider name
	Name() string

	Playlists() ([]PlaylistInfo, error)

	//Local file or URL
	Tracks(playlistID string) ([]Track, error)
}

// Authenticator is optionally implemented by providers that require sign-in.
type Authenticator interface {
	Authenticate() error
}
