//go:build !linux

// Package mpris provides a stub implementation for non-Linux platforms
// where D-Bus is not available.
package mpris

// Message types (must match the Linux implementation).
type (
	PlayPauseMsg struct{}
	NextMsg      struct{}
	PrevMsg      struct{}
	StopMsg      struct{}
	QuitMsg      struct{}
	InitMsg      struct{ Svc *Service }
)

// TrackInfo carries metadata for the currently playing track.
type TrackInfo struct {
	Title  string
	Artist string
	Length int64 // microseconds
}

// Service is a no-op stub on non-Linux platforms.
type Service struct{}

// New returns nil on non-Linux platforms (no D-Bus available).
func New(send func(interface{})) (*Service, error) {
	return nil, nil
}

// Update is a no-op on non-Linux platforms.
func (s *Service) Update(status string, track TrackInfo, volumeDB float64) {}

// Close is a no-op on non-Linux platforms.
func (s *Service) Close() {}
