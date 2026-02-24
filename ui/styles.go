package ui

import "github.com/charmbracelet/lipgloss"

// CLIAMP color palette using standard ANSI terminal colors (0-15).
// These adapt to the user's terminal theme for consistent appearance.
var (
	colorBorder  = lipgloss.ANSIColor(8)  // bright black (dark gray)
	colorTitle   = lipgloss.ANSIColor(10) // bright green
	colorText    = lipgloss.ANSIColor(15) // bright white
	colorDim     = lipgloss.ANSIColor(7)  // white (light gray)
	colorAccent  = lipgloss.ANSIColor(11) // bright yellow
	colorPlaying = lipgloss.ANSIColor(10) // bright green
	colorSeekBar = lipgloss.ANSIColor(11) // bright yellow
	colorVolume  = lipgloss.ANSIColor(2)  // green

	// Spectrum gradient: green -> yellow -> red
	spectrumLow  = lipgloss.ANSIColor(10) // bright green
	spectrumMid  = lipgloss.ANSIColor(11) // bright yellow
	spectrumHigh = lipgloss.ANSIColor(9)  // bright red
)

// Lip Gloss styles
var (
	frameStyle = lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(colorBorder).
			Padding(1, 2).
			Width(66)

	titleStyle = lipgloss.NewStyle().
			Foreground(colorTitle).
			Bold(true)

	trackStyle = lipgloss.NewStyle().
			Foreground(colorAccent)

	timeStyle = lipgloss.NewStyle().
			Foreground(colorText)

	statusStyle = lipgloss.NewStyle().
			Foreground(colorPlaying).
			Bold(true)

	dimStyle = lipgloss.NewStyle().
			Foreground(colorDim)

	labelStyle = lipgloss.NewStyle().
			Foreground(colorText).
			Bold(true)

	eqActiveStyle = lipgloss.NewStyle().
			Foreground(colorAccent).
			Bold(true)

	eqInactiveStyle = lipgloss.NewStyle().
			Foreground(colorDim)

	playlistActiveStyle = lipgloss.NewStyle().
				Foreground(colorPlaying).
				Bold(true)

	playlistItemStyle = lipgloss.NewStyle().
				Foreground(colorText)

	playlistSelectedStyle = lipgloss.NewStyle().
				Foreground(colorAccent).
				Bold(true)

	helpStyle = lipgloss.NewStyle().
			Foreground(colorDim)

	errorStyle = lipgloss.NewStyle().
			Foreground(lipgloss.ANSIColor(9)) // bright red
)
