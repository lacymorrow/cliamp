//go:build linux

// Package mpris exposes an MPRIS2 D-Bus service so that Linux desktop
// environments, hardware media keys, and tools like playerctl can
// control Cliamp.
package mpris

import (
	"fmt"
	"math"
	"sync"

	"github.com/godbus/dbus/v5"
	"github.com/godbus/dbus/v5/introspect"
	"github.com/godbus/dbus/v5/prop"
)

// Message types injected into the Bubbletea event loop.
type (
	PlayPauseMsg struct{}
	NextMsg      struct{}
	PrevMsg      struct{}
	StopMsg      struct{}
	QuitMsg      struct{}
	SeekMsg      struct{ Offset int64 } // microseconds (relative)
	InitMsg      struct{ Svc *Service }
)

// TrackInfo carries metadata for the currently playing track.
type TrackInfo struct {
	Title  string
	Artist string
	Length int64 // microseconds
}

// Service manages the MPRIS2 D-Bus presence.
type Service struct {
	conn  *dbus.Conn
	props *prop.Properties
	send  func(interface{})
	mu    sync.Mutex
}

// introspection XML for the two MPRIS interfaces.
const introspectXML = `
<node>
  <interface name="org.mpris.MediaPlayer2">
    <method name="Raise"/>
    <method name="Quit"/>
  </interface>
  <interface name="org.mpris.MediaPlayer2.Player">
    <method name="Next"/>
    <method name="Previous"/>
    <method name="Pause"/>
    <method name="PlayPause"/>
    <method name="Stop"/>
    <method name="Play"/>
    <method name="Seek"><arg direction="in" type="x"/></method>
    <signal name="Seeked"><arg type="x"/></signal>
  </interface>
` + introspect.IntrospectDataString + `</node>`

// root implements org.mpris.MediaPlayer2 methods.
type root struct{ svc *Service }

func (r root) Raise() *dbus.Error { return nil }
func (r root) Quit() *dbus.Error {
	r.svc.send(QuitMsg{})
	return nil
}

// playerIface implements org.mpris.MediaPlayer2.Player methods.
type playerIface struct{ svc *Service }

func (p playerIface) Next() *dbus.Error {
	p.svc.send(NextMsg{})
	return nil
}

func (p playerIface) Previous() *dbus.Error {
	p.svc.send(PrevMsg{})
	return nil
}

func (p playerIface) Pause() *dbus.Error {
	p.svc.send(PlayPauseMsg{})
	return nil
}

func (p playerIface) PlayPause() *dbus.Error {
	p.svc.send(PlayPauseMsg{})
	return nil
}

func (p playerIface) Stop() *dbus.Error {
	p.svc.send(StopMsg{})
	return nil
}

func (p playerIface) Play() *dbus.Error {
	p.svc.send(PlayPauseMsg{})
	return nil
}

func (p playerIface) Seek(offset int64) *dbus.Error {
	p.svc.send(SeekMsg{Offset: offset})
	return nil
}

// New connects to the session D-Bus, claims the MPRIS bus name, and
// exports the two required interfaces. send is used to inject messages
// into the Bubbletea event loop (typically prog.Send).
func New(send func(interface{})) (*Service, error) {
	conn, err := dbus.ConnectSessionBus()
	if err != nil {
		return nil, fmt.Errorf("mpris: session bus: %w", err)
	}

	reply, err := conn.RequestName("org.mpris.MediaPlayer2.cliamp",
		dbus.NameFlagDoNotQueue)
	if err != nil {
		conn.Close()
		return nil, fmt.Errorf("mpris: request name: %w", err)
	}
	if reply != dbus.RequestNameReplyPrimaryOwner {
		conn.Close()
		return nil, fmt.Errorf("mpris: name already taken")
	}

	svc := &Service{conn: conn, send: send}
	path := dbus.ObjectPath("/org/mpris/MediaPlayer2")

	// Export method handlers.
	conn.Export(root{svc}, path, "org.mpris.MediaPlayer2")
	conn.Export(playerIface{svc}, path, "org.mpris.MediaPlayer2.Player")
	conn.Export(introspect.Introspectable(introspectXML), path,
		"org.freedesktop.DBus.Introspectable")

	// Export properties for both interfaces.
	propsSpec := map[string]map[string]*prop.Prop{
		"org.mpris.MediaPlayer2": {
			"Identity":     {Value: "Cliamp", Writable: false, Emit: prop.EmitTrue},
			"CanQuit":      {Value: true, Writable: false, Emit: prop.EmitTrue},
			"CanRaise":     {Value: false, Writable: false, Emit: prop.EmitTrue},
			"HasTrackList": {Value: false, Writable: false, Emit: prop.EmitTrue},
		},
		"org.mpris.MediaPlayer2.Player": {
			"PlaybackStatus": {Value: "Stopped", Writable: false, Emit: prop.EmitTrue},
			"Metadata":       {Value: makeMetadata(TrackInfo{}), Writable: false, Emit: prop.EmitTrue},
			"Volume":         {Value: 1.0, Writable: false, Emit: prop.EmitTrue},
			"Position":       {Value: int64(0), Writable: false, Emit: prop.EmitFalse},
			"Rate":           {Value: 1.0, Writable: false, Emit: prop.EmitTrue},
			"MinimumRate":    {Value: 1.0, Writable: false, Emit: prop.EmitTrue},
			"MaximumRate":    {Value: 1.0, Writable: false, Emit: prop.EmitTrue},
			"CanControl":     {Value: true, Writable: false, Emit: prop.EmitTrue},
			"CanPlay":        {Value: true, Writable: false, Emit: prop.EmitTrue},
			"CanPause":       {Value: true, Writable: false, Emit: prop.EmitTrue},
			"CanGoNext":      {Value: true, Writable: false, Emit: prop.EmitTrue},
			"CanGoPrevious":  {Value: true, Writable: false, Emit: prop.EmitTrue},
			"CanSeek":        {Value: true, Writable: false, Emit: prop.EmitTrue},
		},
	}

	props, err := prop.Export(conn, path, propsSpec)
	if err != nil {
		conn.Close()
		return nil, fmt.Errorf("mpris: export props: %w", err)
	}
	svc.props = props

	return svc, nil
}

// Update refreshes MPRIS properties when playback state changes.
// status is "Playing", "Paused", or "Stopped". volumeDB is the
// current volume in decibels (range [-30, +6]). positionUs is
// the current playback position in microseconds. canSeek indicates
// whether the current track supports seeking.
func (s *Service) Update(status string, track TrackInfo, volumeDB float64, positionUs int64, canSeek bool) {
	if s == nil {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.props == nil {
		return
	}

	iface := "org.mpris.MediaPlayer2.Player"

	changed := make(map[string]dbus.Variant)

	if cur, err := s.props.Get(iface, "PlaybackStatus"); err == nil {
		if cur.Value() != status {
			s.props.Set(iface, "PlaybackStatus", dbus.MakeVariant(status))
			changed["PlaybackStatus"] = dbus.MakeVariant(status)
		}
	}

	meta := makeMetadata(track)
	s.props.Set(iface, "Metadata", dbus.MakeVariant(meta))
	changed["Metadata"] = dbus.MakeVariant(meta)

	vol := dbToLinear(volumeDB)
	s.props.Set(iface, "Volume", dbus.MakeVariant(vol))
	changed["Volume"] = dbus.MakeVariant(vol)

	// Position uses EmitFalse — update silently (clients poll or use Seeked signal).
	s.props.Set(iface, "Position", dbus.MakeVariant(positionUs))

	// Only emit CanSeek change if the value actually changed.
	if cur, err := s.props.Get(iface, "CanSeek"); err == nil {
		if cur.Value() != canSeek {
			s.props.Set(iface, "CanSeek", dbus.MakeVariant(canSeek))
			changed["CanSeek"] = dbus.MakeVariant(canSeek)
		}
	}

	if len(changed) > 0 {
		s.emitPropertiesChanged(iface, changed)
	}
}

// EmitSeeked sends the org.mpris.MediaPlayer2.Player.Seeked signal
// with the absolute position in microseconds. Call after any seek
// operation (D-Bus or keyboard) so desktop widgets can snap to the
// new position.
func (s *Service) EmitSeeked(positionUs int64) {
	if s == nil {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.conn == nil {
		return
	}
	s.conn.Emit(
		dbus.ObjectPath("/org/mpris/MediaPlayer2"),
		"org.mpris.MediaPlayer2.Player.Seeked",
		positionUs,
	)
}

// Close releases the D-Bus connection.
func (s *Service) Close() {
	if s == nil {
		return
	}
	if s.conn != nil {
		s.conn.Close()
	}
}

// emitPropertiesChanged sends the standard D-Bus PropertiesChanged signal.
func (s *Service) emitPropertiesChanged(iface string, changed map[string]dbus.Variant) {
	s.conn.Emit(
		dbus.ObjectPath("/org/mpris/MediaPlayer2"),
		"org.freedesktop.DBus.Properties.PropertiesChanged",
		iface,
		changed,
		[]string{},
	)
}

// makeMetadata builds an MPRIS metadata map from TrackInfo.
func makeMetadata(t TrackInfo) map[string]dbus.Variant {
	m := map[string]dbus.Variant{
		"mpris:trackid": dbus.MakeVariant(dbus.ObjectPath("/org/mpris/MediaPlayer2/Track/1")),
	}
	if t.Title != "" {
		m["xesam:title"] = dbus.MakeVariant(t.Title)
	}
	if t.Artist != "" {
		m["xesam:artist"] = dbus.MakeVariant([]string{t.Artist})
	}
	if t.Length > 0 {
		m["mpris:length"] = dbus.MakeVariant(t.Length)
	}
	return m
}

// dbToLinear converts a dB volume (range [-30, +6]) to a 0.0–1.0 linear scale.
func dbToLinear(db float64) float64 {
	// Map -30 dB → 0.0, 0 dB → ~0.83, +6 dB → 1.0
	if db <= -30 {
		return 0.0
	}
	if db >= 6 {
		return 1.0
	}
	return math.Pow(10, db/20) / math.Pow(10, 6.0/20)
}
