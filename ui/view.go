package ui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"
)

const panelWidth = 60 // usable inner width (66 frame - 2 border - 4 padding)

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
	return frameStyle.Render(content)
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
	for i := 0; i < maxW; i++ {
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
	if m.player.IsPlaying() {
		if m.player.IsPaused() {
			status = statusStyle.Render("⏸ Paused")
		} else {
			status = statusStyle.Render("▶ Playing")
		}
	} else {
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
	samples := m.player.Samples()
	var bands [numBands]float64
	if len(samples) > 0 {
		bands = m.vis.Analyze(samples)
	} else {
		bands = m.vis.Analyze(nil)
	}
	return m.vis.Render(bands)
}

func (m Model) renderSeekBar() string {
	pos := m.player.Position()
	dur := m.player.Duration()
	barWidth := panelWidth

	var progress float64
	if dur > 0 {
		progress = float64(pos) / float64(dur)
	}
	if progress > 1 {
		progress = 1
	}

	filled := int(progress * float64(barWidth-1))
	if filled < 0 {
		filled = 0
	}

	seekFill := lipgloss.NewStyle().Foreground(colorSeekBar)
	seekDim := lipgloss.NewStyle().Foreground(colorDim)

	return seekFill.Render(strings.Repeat("━", filled)) +
		seekFill.Render("●") +
		seekDim.Render(strings.Repeat("━", max(0, barWidth-filled-1)))
}

func (m Model) renderVolume() string {
	vol := m.player.Volume()
	frac := (vol + 30) / 36
	if frac < 0 {
		frac = 0
	}
	if frac > 1 {
		frac = 1
	}

	barWidth := 22
	filled := int(frac * float64(barWidth))

	volStyle := lipgloss.NewStyle().Foreground(colorVolume)

	bar := volStyle.Render(strings.Repeat("█", filled)) +
		dimStyle.Render(strings.Repeat("░", barWidth-filled))

	return labelStyle.Render("VOL ") + bar + dimStyle.Render(fmt.Sprintf(" %+.1fdB", vol))
}

func (m Model) renderEQ() string {
	bands := m.player.EQBands()
	labels := []string{"70", "180", "320", "600", "1k", "3k", "6k", "12k", "14k", "16k"}

	var parts []string
	for i, label := range labels {
		style := eqInactiveStyle
		if m.focus == focusEQ && i == m.eqCursor {
			style = eqActiveStyle
			label = fmt.Sprintf("%+.0f", bands[i])
		}
		parts = append(parts, style.Render(label))
	}

	return labelStyle.Render("EQ  ") + strings.Join(parts, " ")
}

func (m Model) renderPlaylistHeader() string {
	var shuffle string
	if m.playlist.Shuffled() {
		shuffle = lipgloss.NewStyle().Foreground(colorAccent).Bold(true).Render("[Shuffle]")
	} else {
		shuffle = dimStyle.Render("[Shuffle]")
	}

	repeatMode := m.playlist.Repeat()
	repeatStr := fmt.Sprintf("[Repeat: %s]", repeatMode)
	if repeatMode != 0 {
		repeatStr = lipgloss.NewStyle().Foreground(colorAccent).Bold(true).Render(repeatStr)
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
	visible := m.plVisible
	if visible > len(tracks) {
		visible = len(tracks)
	}

	scroll := m.plScroll
	if scroll+visible > len(tracks) {
		scroll = len(tracks) - visible
	}
	if scroll < 0 {
		scroll = 0
	}

	var lines []string
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

		line := style.Render(fmt.Sprintf("%s%d. %s", prefix, i+1, name))
		lines = append(lines, line)
	}

	return strings.Join(lines, "\n")
}

func (m Model) renderHelp() string {
	return helpStyle.Render("[Spc]⏯  [<>]Trk [←→]Seek [+-]Vol [Tab]Focus [Q]Quit")
}
