package ui

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"cliamp/player"
	"cliamp/playlist"
	"cliamp/resolve"
)

// fbEntry is a single item in the file browser listing.
type fbEntry struct {
	name     string
	path     string
	isDir    bool
	isAudio  bool
	isParent bool
}

// fbTracksResolvedMsg carries tracks resolved from file browser selections.
type fbTracksResolvedMsg struct {
	tracks  []playlist.Track
	replace bool
}

// openFileBrowser initialises and shows the file browser overlay.
func (m *Model) openFileBrowser() {
	home, err := os.UserHomeDir()
	if err != nil {
		home = "/"
	}
	m.fbDir = home
	m.fbCursor = 0
	m.fbSelected = make(map[string]bool)
	m.fbErr = ""
	m.loadFBDir()
	m.showFileBrowser = true
}

// loadFBDir reads the current directory and populates fbEntries.
func (m *Model) loadFBDir() {
	m.fbErr = ""
	m.fbEntries = nil

	// Always provide a parent entry for navigating up.
	m.fbEntries = append(m.fbEntries, fbEntry{
		name:     "..",
		path:     filepath.Dir(m.fbDir),
		isDir:    true,
		isParent: true,
	})

	entries, err := os.ReadDir(m.fbDir)
	if err != nil {
		m.fbErr = err.Error()
		m.fbCursor = 0
		return
	}

	// Separate dirs and files, skip dotfiles.
	var dirs, files []fbEntry
	for _, e := range entries {
		name := e.Name()
		if strings.HasPrefix(name, ".") {
			continue
		}
		full := filepath.Join(m.fbDir, name)
		if e.IsDir() {
			dirs = append(dirs, fbEntry{
				name:  name + "/",
				path:  full,
				isDir: true,
			})
		} else {
			ext := strings.ToLower(filepath.Ext(name))
			files = append(files, fbEntry{
				name:    name,
				path:    full,
				isAudio: player.SupportedExts[ext],
			})
		}
	}

	m.fbEntries = append(m.fbEntries, dirs...)
	m.fbEntries = append(m.fbEntries, files...)
	m.fbCursor = 0
}

// handleFileBrowserKey processes key presses while the file browser is open.
func (m *Model) handleFileBrowserKey(msg tea.KeyMsg) tea.Cmd {
	switch msg.String() {
	case "ctrl+c":
		m.showFileBrowser = false
		m.player.Close()
		m.quitting = true
		return tea.Quit

	case "esc", "o":
		m.showFileBrowser = false
		return nil

	case "up", "k":
		if m.fbCursor > 0 {
			m.fbCursor--
		}

	case "down", "j":
		if m.fbCursor < len(m.fbEntries)-1 {
			m.fbCursor++
		}

	case "enter", "l", "right":
		if len(m.fbSelected) > 0 {
			return m.fbConfirm(false)
		}
		// No selections — descend into directory.
		if m.fbCursor < len(m.fbEntries) {
			e := m.fbEntries[m.fbCursor]
			if e.isDir {
				m.fbDir = e.path
				m.loadFBDir()
			}
		}

	case "backspace", "h", "left":
		m.fbDir = filepath.Dir(m.fbDir)
		m.loadFBDir()

	case " ":
		if m.fbCursor < len(m.fbEntries) {
			e := m.fbEntries[m.fbCursor]
			if !e.isParent && (e.isAudio || e.isDir) {
				if m.fbSelected[e.path] {
					delete(m.fbSelected, e.path)
				} else {
					m.fbSelected[e.path] = true
				}
			}
		}

	case "a":
		// Toggle select all audio files in current view.
		allSelected := true
		for _, e := range m.fbEntries {
			if e.isAudio && !m.fbSelected[e.path] {
				allSelected = false
				break
			}
		}
		for _, e := range m.fbEntries {
			if e.isAudio {
				if allSelected {
					delete(m.fbSelected, e.path)
				} else {
					m.fbSelected[e.path] = true
				}
			}
		}

	case "g":
		m.fbCursor = 0

	case "G":
		if len(m.fbEntries) > 0 {
			m.fbCursor = len(m.fbEntries) - 1
		}

	case "R":
		if len(m.fbSelected) > 0 {
			return m.fbConfirm(true)
		}
	}

	return nil
}

// fbConfirm collects selected paths, closes the overlay, and returns an async
// command that resolves the paths into tracks.
func (m *Model) fbConfirm(replace bool) tea.Cmd {
	var paths []string
	for p := range m.fbSelected {
		paths = append(paths, p)
	}
	m.showFileBrowser = false

	return func() tea.Msg {
		r, err := resolve.Args(paths)
		if err != nil {
			return err
		}
		return fbTracksResolvedMsg{tracks: r.Tracks, replace: replace}
	}
}

// renderFileBrowser renders the file browser overlay.
func (m Model) renderFileBrowser() string {
	lines := []string{
		titleStyle.Render("O P E N  F I L E S"),
		dimStyle.Render("  " + m.fbDir),
		"",
	}

	if m.fbErr != "" {
		lines = append(lines, errorStyle.Render("  "+m.fbErr))
	}

	maxVisible := 12
	rendered := 0

	if len(m.fbEntries) == 0 {
		lines = append(lines, dimStyle.Render("  (empty)"))
		rendered = 1
	} else {
		scroll := 0
		if m.fbCursor >= maxVisible {
			scroll = m.fbCursor - maxVisible + 1
		}

		for i := scroll; i < len(m.fbEntries) && i < scroll+maxVisible; i++ {
			e := m.fbEntries[i]

			// Selection check mark.
			check := "  "
			if m.fbSelected[e.path] {
				check = "✓ "
			}

			// Type indicator suffix.
			suffix := ""
			if e.isAudio {
				suffix = " ♫"
			}

			label := check + e.name + suffix

			// Truncate long names.
			maxW := panelWidth - 4
			labelRunes := []rune(label)
			if len(labelRunes) > maxW {
				label = string(labelRunes[:maxW-1]) + "…"
			}

			if i == m.fbCursor {
				lines = append(lines, playlistSelectedStyle.Render("> "+label))
			} else if e.isDir {
				lines = append(lines, trackStyle.Render("  "+label))
			} else if e.isAudio {
				lines = append(lines, playlistItemStyle.Render("  "+label))
			} else {
				lines = append(lines, dimStyle.Render("  "+label))
			}
			rendered++
		}
	}

	// Pad to fixed height.
	for range maxVisible - rendered {
		lines = append(lines, "")
	}

	// Selection count.
	if len(m.fbSelected) > 0 {
		lines = append(lines, "", statusStyle.Render(fmt.Sprintf("  %d selected", len(m.fbSelected))))
	} else {
		lines = append(lines, "")
	}

	lines = append(lines, "", helpKey("↑↓", "Navigate ")+helpKey("Enter", "Open ")+helpKey("Spc", "Select ")+helpKey("a", "All ")+helpKey("←", "Back ")+helpKey("Esc", "Close"))

	return m.centerOverlay(strings.Join(lines, "\n"))
}
