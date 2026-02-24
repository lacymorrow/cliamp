package ui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"
)

const panelWidth = 74 // usable inner width (80 frame - 6 padding)

// Pre-built styles for elements created per-render to avoid repeated allocation.
var (
	seekFillStyle = lipgloss.NewStyle().Foreground(colorSeekBar)
	seekDimStyle  = lipgloss.NewStyle().Foreground(colorDim)
	volBarStyle   = lipgloss.NewStyle().Foreground(colorVolume)
	activeToggle  = lipgloss.NewStyle().Foreground(colorAccent).Bold(true)
)

// View renders the full TUI frame.
func (m Model) View() string {
	if m.quitting {
		return ""
	}

	sections := []string{
		// Now playing
		m.renderTitle(),
		m.renderTrackInfo(),
		m.renderTimeStatus(),
		"",
		// Visualizer
		m.renderSpectrum(),
		m.renderSeekBar(),
		"",
		// Controls
		m.renderVolume(),
		m.renderEQ(),
		"",
		// Playlist
		m.renderPlaylistHeader(),
		m.renderPlaylist(),
		"",
		// Help
		m.renderHelp(),
	}

	if m.err != nil {
		sections = append(sections, errorStyle.Render(fmt.Sprintf("ERR: %s", m.err)))
	}

	content := strings.Join(sections, "\n")
	frame := frameStyle.Render(content)

	// Center horizontally and vertically within the terminal
	frameW := lipgloss.Width(frame)
	frameH := lipgloss.Height(frame)

	padLeft := max(0, (m.width-frameW)/2)
	padTop := max(0, (m.height-frameH)/2)

	return strings.Repeat("\n", padTop) +
		lipgloss.NewStyle().MarginLeft(padLeft).Render(frame)
}

func (m Model) renderTitle() string {
	return titleStyle.Render("C L I A M P")
}

func (m Model) renderTrackInfo() string {
	track, _ := m.playlist.Current()
	name := track.DisplayName()
	if name == "" {
		name = "No track loaded"
	}

	maxW := panelWidth - 4
	runes := []rune(name)

	if len(runes) <= maxW {
		return trackStyle.Render("♫ " + name)
	}

	// Cyclic scrolling for long titles
	sep := []rune("   ♫   ")
	padded := append(runes, sep...)
	total := len(padded)
	off := m.titleOff % total

	display := make([]rune, maxW)
	for i := range maxW {
		display[i] = padded[(off+i)%total]
	}
	return trackStyle.Render("♫ " + string(display))
}

func (m Model) renderTimeStatus() string {
	pos := m.player.Position()
	dur := m.player.Duration()

	posMin := int(pos.Minutes())
	posSec := int(pos.Seconds()) % 60
	durMin := int(dur.Minutes())
	durSec := int(dur.Seconds()) % 60

	timeStr := fmt.Sprintf("%02d:%02d / %02d:%02d", posMin, posSec, durMin, durSec)

	var status string
	switch {
	case m.player.IsPlaying() && m.player.IsPaused():
		status = statusStyle.Render("⏸ Paused")
	case m.player.IsPlaying():
		status = statusStyle.Render("▶ Playing")
	default:
		status = dimStyle.Render("■ Stopped")
	}

	left := timeStyle.Render(timeStr)
	gap := panelWidth - lipgloss.Width(left) - lipgloss.Width(status)
	if gap < 1 {
		gap = 1
	}

	return left + strings.Repeat(" ", gap) + status
}

func (m Model) renderSpectrum() string {
	bands := m.vis.Analyze(m.player.Samples())
	return m.vis.Render(bands)
}

func (m Model) renderSeekBar() string {
	pos := m.player.Position()
	dur := m.player.Duration()

	var progress float64
	if dur > 0 {
		progress = float64(pos) / float64(dur)
	}
	progress = max(0, min(1, progress))

	filled := int(progress * float64(panelWidth-1))

	return seekFillStyle.Render(strings.Repeat("━", filled)) +
		seekFillStyle.Render("●") +
		seekDimStyle.Render(strings.Repeat("━", max(0, panelWidth-filled-1)))
}

func (m Model) renderVolume() string {
	vol := m.player.Volume()
	frac := max(0, min(1, (vol+30)/36))

	barW := 30
	filled := int(frac * float64(barW))

	bar := volBarStyle.Render(strings.Repeat("█", filled)) +
		dimStyle.Render(strings.Repeat("░", barW-filled))

	return labelStyle.Render("VOL ") + bar + dimStyle.Render(fmt.Sprintf(" %+.1fdB", vol))
}

func (m Model) renderEQ() string {
	bands := m.player.EQBands()
	labels := [10]string{"70", "180", "320", "600", "1k", "3k", "6k", "12k", "14k", "16k"}

	parts := make([]string, len(labels))
	for i, label := range labels {
		style := eqInactiveStyle
		if m.focus == focusEQ && i == m.eqCursor {
			style = eqActiveStyle
			label = fmt.Sprintf("%+.0f", bands[i])
		}
		parts[i] = style.Render(label)
	}

	return labelStyle.Render("EQ  ") + strings.Join(parts, " ")
}

func (m Model) renderPlaylistHeader() string {
	var shuffle string
	if m.playlist.Shuffled() {
		shuffle = activeToggle.Render("[Shuffle]")
	} else {
		shuffle = dimStyle.Render("[Shuffle]")
	}

	repeatStr := fmt.Sprintf("[Repeat: %s]", m.playlist.Repeat())
	if m.playlist.Repeat() != 0 {
		repeatStr = activeToggle.Render(repeatStr)
	} else {
		repeatStr = dimStyle.Render(repeatStr)
	}

	return dimStyle.Render("── Playlist ── ") + shuffle + " " + repeatStr + " " + dimStyle.Render("──")
}

func (m Model) renderPlaylist() string {
	tracks := m.playlist.Tracks()
	if len(tracks) == 0 {
		return dimStyle.Render("  No tracks loaded")
	}

	currentIdx := m.playlist.Index()
	visible := min(m.plVisible, len(tracks))

	scroll := m.plScroll
	if scroll+visible > len(tracks) {
		scroll = len(tracks) - visible
	}
	scroll = max(0, scroll)

	lines := make([]string, 0, visible)
	for i := scroll; i < scroll+visible && i < len(tracks); i++ {
		prefix := "  "
		style := playlistItemStyle

		if i == currentIdx && m.player.IsPlaying() {
			prefix = "▶ "
			style = playlistActiveStyle
		}

		if m.focus == focusPlaylist && i == m.plCursor {
			style = playlistSelectedStyle
		}

		name := tracks[i].DisplayName()
		maxW := panelWidth - 6
		nameRunes := []rune(name)
		if len(nameRunes) > maxW {
			name = string(nameRunes[:maxW-1]) + "…"
		}

		lines = append(lines, style.Render(fmt.Sprintf("%s%d. %s", prefix, i+1, name)))
	}

	return strings.Join(lines, "\n")
}

func (m Model) renderHelp() string {
	return helpStyle.Render("[Spc]⏯  [<>]Trk [←→]Seek [+-]Vol [Tab]Focus [Q]Quit")
}
