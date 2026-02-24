package ui

import (
	"time"
	"unicode/utf8"

	tea "github.com/charmbracelet/bubbletea"
)

// handleKey processes a single key press and returns an optional command.
func (m *Model) handleKey(msg tea.KeyMsg) tea.Cmd {
	if m.searching {
		return m.handleSearchKey(msg)
	}

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

	case "/":
		m.searching = true
		m.searchQuery = ""
		m.searchResults = nil
		m.searchCursor = 0
		m.prevFocus = m.focus
		m.focus = focusSearch
	}

	return nil
}

// handleSearchKey processes key presses while in search mode.
func (m *Model) handleSearchKey(msg tea.KeyMsg) tea.Cmd {
	switch msg.Type {
	case tea.KeyEscape:
		m.searching = false
		m.focus = m.prevFocus

	case tea.KeyEnter:
		if len(m.searchResults) > 0 {
			idx := m.searchResults[m.searchCursor]
			m.playlist.SetIndex(idx)
			m.plCursor = idx
			m.adjustScroll()
			m.playCurrentTrack()
		}
		m.searching = false
		m.focus = focusPlaylist

	case tea.KeyUp:
		if m.searchCursor > 0 {
			m.searchCursor--
		}

	case tea.KeyDown:
		if m.searchCursor < len(m.searchResults)-1 {
			m.searchCursor++
		}

	case tea.KeyBackspace:
		if len(m.searchQuery) > 0 {
			_, size := utf8.DecodeLastRuneInString(m.searchQuery)
			m.searchQuery = m.searchQuery[:len(m.searchQuery)-size]
			m.updateSearch()
		}

	default:
		if msg.Type == tea.KeyRunes {
			m.searchQuery += string(msg.Runes)
			m.updateSearch()
		}
	}

	return nil
}
