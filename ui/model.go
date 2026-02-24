// Package ui implements the Bubbletea TUI for the CLIAMP terminal music player.
package ui

import (
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"winamp-cli/player"
	"winamp-cli/playlist"
)

type focusArea int

const (
	focusPlaylist focusArea = iota
	focusEQ
)

type tickMsg time.Time

// Model is the Bubbletea model for the CLIAMP TUI.
type Model struct {
	player    *player.Player
	playlist  *playlist.Playlist
	vis       *Visualizer
	focus     focusArea
	eqCursor  int // selected EQ band (0-9)
	plCursor  int // selected playlist item
	plScroll  int // scroll offset for playlist view
	plVisible int // max visible playlist items
	titleOff  int // scroll offset for long track titles
	err       error
	quitting  bool
	width     int
	height    int
}

// NewModel creates a Model wired to the given player and playlist.
func NewModel(p *player.Player, pl *playlist.Playlist) Model {
	return Model{
		player:    p,
		playlist:  pl,
		vis:       NewVisualizer(44100),
		plVisible: 8,
	}
}

// Init starts the tick timer and requests the terminal size.
func (m Model) Init() tea.Cmd {
	return tea.Batch(tickCmd(), tea.WindowSize())
}

func tickCmd() tea.Cmd {
	return tea.Tick(time.Millisecond*50, func(t time.Time) tea.Msg {
		return tickMsg(t)
	})
}

// Update handles messages: key presses, ticks, and window resizes.
func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		cmd := m.handleKey(msg)
		if m.quitting {
			return m, tea.Quit
		}
		return m, cmd

	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height

	case tickMsg:
		// Check if the current track finished naturally
		if m.player.IsPlaying() && !m.player.IsPaused() && m.player.TrackDone() {
			m.nextTrack()
		}
		m.titleOff++
		return m, tickCmd()
	}

	return m, nil
}

// nextTrack advances to the next playlist track and starts playing it.
func (m *Model) nextTrack() {
	track, ok := m.playlist.Next()
	if !ok {
		m.player.Stop()
		return
	}
	m.plCursor = m.playlist.Index()
	m.adjustScroll()
	if err := m.player.Play(track.Path); err != nil {
		m.err = err
	}
}

// prevTrack goes to the previous track, or restarts if >3s into the current one.
func (m *Model) prevTrack() {
	if m.player.Position() > 3*time.Second {
		m.player.Seek(-m.player.Position())
		return
	}
	track, ok := m.playlist.Prev()
	if !ok {
		return
	}
	m.plCursor = m.playlist.Index()
	m.adjustScroll()
	if err := m.player.Play(track.Path); err != nil {
		m.err = err
	}
}

// playCurrentTrack starts playing whatever track the playlist cursor points to.
func (m *Model) playCurrentTrack() {
	track, idx := m.playlist.Current()
	if idx < 0 {
		return
	}
	m.titleOff = 0
	if err := m.player.Play(track.Path); err != nil {
		m.err = err
	}
}

// adjustScroll ensures plCursor is visible in the playlist view.
func (m *Model) adjustScroll() {
	if m.plCursor < m.plScroll {
		m.plScroll = m.plCursor
	}
	if m.plCursor >= m.plScroll+m.plVisible {
		m.plScroll = m.plCursor - m.plVisible + 1
	}
}
