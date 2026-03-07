package ui

import (
	"time"

	tea "github.com/charmbracelet/bubbletea"
)

// seekDebounceTicks is how many ticks to wait after the last seek keypress
// before actually executing the yt-dlp seek (restart). During this window,
// additional seek presses accumulate.
const seekDebounceTicks = 8 // ~800ms at 100ms tick interval

// seekTickMsg fires when the debounce timer expires.
type seekTickMsg struct{}

// doSeek accumulates a seek delta. For yt-dlp streams it debounces;
// for local files it seeks immediately.
func (m *Model) doSeek(d time.Duration) tea.Cmd {
	if m.player.IsYTDLSeek() {
		if m.pendingSeek == 0 && !m.seekInFlight {
			// First press (no pending, no in-flight): snapshot current position.
			m.seekBasePos = m.player.Position()
		} else if m.pendingSeek == 0 && m.seekInFlight {
			// Pressing during an in-flight seek: start from the in-flight target.
			m.seekBasePos = m.seekTargetPos
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
// When a yt-dlp seek is pending or in-flight, shows the target position
// so the user can see where they're seeking to without bouncing.
func (m *Model) displayPosition() time.Duration {
	// Accumulating keypresses: show projected target.
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
	// Seek fired but yt-dlp hasn't restarted yet: hold the target.
	if m.seekInFlight {
		return m.seekTargetPos
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
	// Compute and store the target position for display until seek completes.
	target := m.seekBasePos + d
	if target < 0 {
		target = 0
	}
	dur := m.player.Duration()
	if dur > 0 && target >= dur {
		target = dur - time.Second
	}
	m.seekTargetPos = target
	m.seekInFlight = true
	m.pendingSeek = 0

	// Capture player pointer before returning the Cmd — don't close over m.
	p := m.player
	return func() tea.Msg {
		p.SeekYTDL(d)
		return seekTickMsg{}
	}
}
