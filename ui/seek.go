package ui

import (
	"time"

	tea "github.com/charmbracelet/bubbletea"
)

// seekDebounceTicks is how many ticks to wait after the last seek keypress
// before actually executing the yt-dlp seek (restart). During this window,
// additional seek presses accumulate.
const seekDebounceTicks = 3 // ~300ms at 100ms tick interval

// seekTickMsg fires when the debounce timer expires.
type seekTickMsg struct{}

// doSeek accumulates a seek delta. For yt-dlp streams it debounces;
// for local files it seeks immediately.
func (m *Model) doSeek(d time.Duration) tea.Cmd {
	if m.player.IsYTDLSeek() {
		if m.pendingSeek == 0 {
			// First press: snapshot the current position as the base.
			m.seekBasePos = m.player.Position()
		}
		m.pendingSeek += d
		m.seekTimer = seekDebounceTicks
		return nil // tick loop will fire the actual seek
	}
	// Local/HTTP seek: immediate.
	m.player.Seek(d)
	if m.mpris != nil {
		m.mpris.EmitSeeked(m.player.Position().Microseconds())
	}
	return nil
}

// displayPosition returns the position to show in the UI.
// When a yt-dlp seek is pending, shows the target position immediately
// so the user can see where they're seeking to.
func (m *Model) displayPosition() time.Duration {
	if m.pendingSeek != 0 {
		target := m.seekBasePos + m.pendingSeek
		if target < 0 {
			return 0
		}
		dur := m.player.Duration()
		if dur > 0 && target >= dur {
			return dur - time.Second
		}
		return target
	}
	return m.player.Position()
}

// tickSeek is called from the main tick loop. Decrements the debounce timer
// and fires the seek when it reaches zero.
func (m *Model) tickSeek() tea.Cmd {
	if m.seekTimer <= 0 || m.pendingSeek == 0 {
		return nil
	}
	m.seekTimer--
	if m.seekTimer > 0 {
		return nil
	}

	// Timer expired — fire the accumulated seek.
	d := m.pendingSeek
	m.pendingSeek = 0

	// Run seek in background to avoid blocking the UI.
	// Use SeekYTDL directly — it doesn't hold the speaker lock during spawn.
	return func() tea.Msg {
		m.player.SeekYTDL(d)
		return seekTickMsg{}
	}
}
