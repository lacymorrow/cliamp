package playlist

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
