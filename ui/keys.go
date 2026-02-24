package ui

import (
	"time"

	tea "github.com/charmbracelet/bubbletea"
)

// handleKey processes a single key press and returns an optional command.
func (m *Model) handleKey(msg tea.KeyMsg) tea.Cmd {
	switch msg.String() {
	case "q", "ctrl+c":
		m.player.Close()
		m.quitting = true
		return tea.Quit

	case " ":
		if !m.player.IsPlaying() {
			m.playCurrentTrack()
		} else {
			m.player.TogglePause()
		}

	case "s":
		m.player.Stop()

	case ">", ".":
		m.nextTrack()

	case "<", ",":
		m.prevTrack()

	case "left":
		if m.focus == focusEQ {
			if m.eqCursor > 0 {
				m.eqCursor--
			}
		} else {
			m.player.Seek(-5 * time.Second)
		}

	case "right":
		if m.focus == focusEQ {
			if m.eqCursor < 9 {
				m.eqCursor++
			}
		} else {
			m.player.Seek(5 * time.Second)
		}

	case "up", "k":
		if m.focus == focusEQ {
			bands := m.player.EQBands()
			m.player.SetEQBand(m.eqCursor, bands[m.eqCursor]+1)
		} else {
			if m.plCursor > 0 {
				m.plCursor--
				m.adjustScroll()
			}
		}

	case "down", "j":
		if m.focus == focusEQ {
			bands := m.player.EQBands()
			m.player.SetEQBand(m.eqCursor, bands[m.eqCursor]-1)
		} else {
			if m.plCursor < m.playlist.Len()-1 {
				m.plCursor++
				m.adjustScroll()
			}
		}

	case "enter":
		if m.focus == focusPlaylist {
			m.playlist.SetIndex(m.plCursor)
			m.playCurrentTrack()
		}

	case "+", "=":
		m.player.SetVolume(m.player.Volume() + 1)

	case "-":
		m.player.SetVolume(m.player.Volume() - 1)

	case "r":
		m.playlist.CycleRepeat()

	case "z":
		m.playlist.ToggleShuffle()

	case "tab":
		if m.focus == focusPlaylist {
			m.focus = focusEQ
		} else {
			m.focus = focusPlaylist
		}

	case "h":
		if m.focus == focusEQ && m.eqCursor > 0 {
			m.eqCursor--
		}

	case "l":
		if m.focus == focusEQ && m.eqCursor < 9 {
			m.eqCursor++
		}
	}

	return nil
}
