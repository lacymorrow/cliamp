package ui

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"
	"unicode/utf8"

	tea "github.com/charmbracelet/bubbletea"
)

// handleKey processes a single key press and returns an optional command.
func (m *Model) handleKey(msg tea.KeyMsg) tea.Cmd {
	if m.showKeymap {
		return m.handleKeymapKey(msg)
	}

	// Theme picker overlay — interactive navigation
	if m.showThemes {
		return m.handleThemeKey(msg)
	}

	// Playlist manager overlay (browse, add, remove, delete)
	if m.showPlManager {
		return m.handlePlaylistManagerKey(msg)
	}

	// Queue manager overlay
	if m.showQueue {
		return m.handleQueueKey(msg)
	}

	// Track info overlay
	if m.showInfo {
		switch msg.String() {
		case "ctrl+c":
			m.showInfo = false
			m.player.Close()
			m.quitting = true
			return tea.Quit
		case "esc", "i":
			m.showInfo = false
		}
		return nil
	}

	if m.searching {
		return m.handleSearchKey(msg)
	}

	if m.focus == focusProvider {
		switch msg.String() {
		case "q", "ctrl+c":
			m.player.Close()
			m.quitting = true
			return tea.Quit
		case "up", "k":
			if m.provCursor > 0 {
				m.provCursor--
			}
		case " ":
			return m.togglePlayPause()
		case "down", "j":
			if m.provCursor < len(m.providerLists)-1 {
				m.provCursor++
			}
		case "enter":
			if len(m.providerLists) > 0 && !m.provLoading {
				m.provLoading = true
				return fetchTracksCmd(m.provider, m.providerLists[m.provCursor].ID)
			}
		case "tab":
			if m.playlist.Len() > 0 {
				m.focus = focusPlaylist
			}
		}
		return nil
	}

	switch msg.String() {
	case "q", "ctrl+c":
		m.player.Close()
		m.quitting = true
		return tea.Quit
	case "esc", "backspace", "b":
		if m.focus == focusPlaylist && m.provider != nil {
			m.focus = focusProvider
		}

	case " ":
		cmd := m.togglePlayPause()
		m.notifyMPRIS()
		return cmd

	case "s":
		m.player.Stop()
		m.notifyMPRIS()

	case ">", ".":
		cmd := m.nextTrack()
		m.notifyMPRIS()
		return cmd

	case "<", ",":
		cmd := m.prevTrack()
		m.notifyMPRIS()
		return cmd

	case "left":
		if m.focus == focusEQ {
			if m.eqCursor > 0 {
				m.eqCursor--
			}
		} else {
			m.player.Seek(-5 * time.Second)
			if m.mpris != nil {
				m.mpris.EmitSeeked(m.player.Position().Microseconds())
			}
		}

	case "right":
		if m.focus == focusEQ {
			if m.eqCursor < 9 {
				m.eqCursor++
			}
		} else {
			m.player.Seek(5 * time.Second)
			if m.mpris != nil {
				m.mpris.EmitSeeked(m.player.Position().Microseconds())
			}
		}

	case "up", "k":
		if m.focus == focusEQ {
			bands := m.player.EQBands()
			m.player.SetEQBand(m.eqCursor, bands[m.eqCursor]+1)
			m.eqPresetIdx = -1 // manual tweak → custom
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
			m.eqPresetIdx = -1 // manual tweak → custom
		} else {
			if m.plCursor < m.playlist.Len()-1 {
				m.plCursor++
				m.adjustScroll()
			}
		}

	case "enter":
		if m.focus == focusPlaylist {
			m.playlist.SetIndex(m.plCursor)
			cmd := m.playCurrentTrack()
			m.notifyMPRIS()
			return cmd
		}

	case "+", "=":
		m.player.SetVolume(m.player.Volume() + 1)
		m.notifyMPRIS()

	case "-":
		m.player.SetVolume(m.player.Volume() - 1)
		m.notifyMPRIS()

	case "r":
		m.playlist.CycleRepeat()
		m.player.ClearPreload()
		return m.preloadNext()

	case "z":
		m.playlist.ToggleShuffle()
		m.player.ClearPreload()
		return m.preloadNext()

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

	case "e":
		m.eqPresetIdx++
		if m.eqPresetIdx >= len(eqPresets) {
			m.eqPresetIdx = 0
		}
		m.applyEQPreset()

	case "a":
		if m.focus == focusPlaylist {
			if !m.playlist.Dequeue(m.plCursor) {
				m.playlist.Queue(m.plCursor)
			}
		}

	case "A":
		if m.focus == focusPlaylist {
			m.showQueue = true
			m.queueCursor = 0
		}

	case "S":
		m.saveTrack()

	case "m":
		m.player.ToggleMono()

	case "/":
		m.searching = true
		m.searchQuery = ""
		m.searchResults = nil
		m.searchCursor = 0
		m.prevFocus = m.focus
		m.focus = focusSearch

	case "p":
		if m.localProvider != nil {
			m.openPlaylistManager()
		}

	case "t":
		m.openThemePicker()

	case "i":
		m.showInfo = true

	case "v":
		m.vis.CycleMode()

	case "x":
		if m.focus == focusPlaylist {
			if m.plVisible == 5 {
				m.plVisible = 20
			} else {
				m.plVisible = 5
			}
			m.adjustScroll()
		}

	case "ctrl+k":
		m.showKeymap = true
	}

	return nil
}

// saveTrack copies the current track to ~/Music/cliamp/ with a clean filename.
// Only works for downloaded yt-dlp tracks (temp files).
func (m *Model) saveTrack() {
	track, idx := m.playlist.Current()
	if idx < 0 {
		m.saveMsg = "Nothing to save"
		m.saveMsgTTL = 40 // ~2s at 50ms ticks
		return
	}

	// Only save local temp files (yt-dlp downloads), not streams or user's own files.
	if track.Stream || !strings.HasPrefix(track.Path, os.TempDir()) {
		m.saveMsg = "Only downloaded tracks can be saved"
		m.saveMsgTTL = 40
		return
	}

	home, err := os.UserHomeDir()
	if err != nil {
		m.saveMsg = fmt.Sprintf("Save failed: %s", err)
		m.saveMsgTTL = 40
		return
	}

	saveDir := filepath.Join(home, "Music", "cliamp")
	if err := os.MkdirAll(saveDir, 0o755); err != nil {
		m.saveMsg = fmt.Sprintf("Save failed: %s", err)
		m.saveMsgTTL = 40
		return
	}

	ext := filepath.Ext(track.Path)
	name := track.Title
	if track.Artist != "" {
		name = track.Artist + " - " + name
	}
	// Sanitize filename: remove path separators and other problematic chars.
	name = strings.Map(func(r rune) rune {
		if r == '/' || r == '\\' || r == ':' || r == '*' || r == '?' || r == '"' || r == '<' || r == '>' || r == '|' {
			return '_'
		}
		return r
	}, name)

	dest := filepath.Join(saveDir, name+ext)

	if err := copyFile(track.Path, dest); err != nil {
		m.saveMsg = fmt.Sprintf("Save failed: %s", err)
		m.saveMsgTTL = 40
		return
	}

	m.saveMsg = fmt.Sprintf("Saved to ~/Music/cliamp/%s", name+ext)
	m.saveMsgTTL = 60 // ~3s
}

func copyFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()

	out, err := os.Create(dst)
	if err != nil {
		return err
	}

	_, copyErr := io.Copy(out, in)
	closeErr := out.Close()
	if copyErr != nil {
		os.Remove(dst) // clean up partial file
		return copyErr
	}
	if closeErr != nil {
		os.Remove(dst)
		return closeErr
	}
	return nil
}

// handleSearchKey processes key presses while in search mode.
func (m *Model) handleSearchKey(msg tea.KeyMsg) tea.Cmd {
	// Allow opening overlays during search (ctrl combos don't conflict with text input).
	switch msg.String() {
	case "ctrl+k":
		m.showKeymap = true
		return nil
	}

	switch msg.Type {
	case tea.KeyEscape:
		m.searching = false
		m.focus = m.prevFocus

	case tea.KeyEnter:
		var cmd tea.Cmd
		if len(m.searchResults) > 0 {
			idx := m.searchResults[m.searchCursor]
			m.playlist.SetIndex(idx)
			m.plCursor = idx
			m.adjustScroll()
			cmd = m.playCurrentTrack()
			m.notifyMPRIS()
		}
		m.searching = false
		m.focus = focusPlaylist
		return cmd

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

// handlePlaylistManagerKey dispatches keys to the active manager screen.
func (m *Model) handlePlaylistManagerKey(msg tea.KeyMsg) tea.Cmd {
	switch m.plMgrScreen {
	case plMgrScreenList:
		return m.handlePlMgrListKey(msg)
	case plMgrScreenTracks:
		return m.handlePlMgrTracksKey(msg)
	case plMgrScreenNewName:
		return m.handlePlMgrNewNameKey(msg)
	}
	return nil
}

// handlePlMgrListKey handles keys on screen 0 (playlist list).
func (m *Model) handlePlMgrListKey(msg tea.KeyMsg) tea.Cmd {
	// If waiting for delete confirmation, only accept y/n.
	if m.plMgrConfirmDel {
		switch msg.String() {
		case "y", "Y":
			if m.plMgrCursor < len(m.plMgrPlaylists) {
				name := m.plMgrPlaylists[m.plMgrCursor].Name
				if err := m.localProvider.DeletePlaylist(name); err != nil {
					m.saveMsg = fmt.Sprintf("Delete failed: %s", err)
					m.saveMsgTTL = 60
				} else {
					m.saveMsg = fmt.Sprintf("Deleted \"%s\"", name)
					m.saveMsgTTL = 60
				}
				m.plMgrRefreshList()
			}
			m.plMgrConfirmDel = false
		default:
			m.plMgrConfirmDel = false
		}
		return nil
	}

	count := len(m.plMgrPlaylists) + 1 // +1 for "+ New Playlist..."
	switch msg.String() {
	case "ctrl+c":
		m.showPlManager = false
		m.player.Close()
		m.quitting = true
		return tea.Quit
	case "up", "k":
		if m.plMgrCursor > 0 {
			m.plMgrCursor--
		}
	case "down", "j":
		if m.plMgrCursor < count-1 {
			m.plMgrCursor++
		}
	case "enter", "l", "right":
		if m.plMgrCursor < len(m.plMgrPlaylists) {
			m.plMgrEnterTrackList(m.plMgrPlaylists[m.plMgrCursor].Name)
		} else {
			// "+ New Playlist..." selected
			m.plMgrScreen = plMgrScreenNewName
			m.plMgrNewName = ""
		}
	case "a":
		// Quick-add current track to the highlighted playlist.
		if m.plMgrCursor < len(m.plMgrPlaylists) {
			m.addToPlaylist(m.plMgrPlaylists[m.plMgrCursor].Name)
			m.plMgrRefreshList()
		}
	case "d":
		if m.plMgrCursor < len(m.plMgrPlaylists) {
			m.plMgrConfirmDel = true
		}
	case "esc", "p":
		m.showPlManager = false
	}
	return nil
}

// handlePlMgrTracksKey handles keys on screen 1 (track list inside a playlist).
func (m *Model) handlePlMgrTracksKey(msg tea.KeyMsg) tea.Cmd {
	switch msg.String() {
	case "ctrl+c":
		m.showPlManager = false
		m.player.Close()
		m.quitting = true
		return tea.Quit
	case "up", "k":
		if m.plMgrCursor > 0 {
			m.plMgrCursor--
		}
	case "down", "j":
		if m.plMgrCursor < len(m.plMgrTracks)-1 {
			m.plMgrCursor++
		}
	case "enter":
		// Replace playlist and start playback.
		if len(m.plMgrTracks) > 0 {
			m.player.Stop()
			m.player.ClearPreload()
			m.playlist.Replace(m.plMgrTracks)
			m.plCursor = 0
			m.playlist.SetIndex(0)
			m.adjustScroll()
			m.showPlManager = false
			m.focus = focusPlaylist
			cmd := m.playCurrentTrack()
			m.notifyMPRIS()
			return cmd
		}
	case "a":
		// Add current playing track to this playlist.
		m.addToPlaylist(m.plMgrSelPlaylist)
		// Refresh the track list to show the new track.
		tracks, _ := m.localProvider.Tracks(m.plMgrSelPlaylist)
		m.plMgrTracks = tracks
	case "d":
		// Remove highlighted track.
		if len(m.plMgrTracks) > 0 && m.plMgrCursor < len(m.plMgrTracks) {
			err := m.localProvider.RemoveTrack(m.plMgrSelPlaylist, m.plMgrCursor)
			if err != nil {
				m.saveMsg = fmt.Sprintf("Remove failed: %s", err)
				m.saveMsgTTL = 60
			} else {
				m.saveMsg = "Track removed"
				m.saveMsgTTL = 60
			}
			// Reload tracks (or go back if playlist was deleted).
			tracks, err := m.localProvider.Tracks(m.plMgrSelPlaylist)
			if err != nil || len(tracks) == 0 {
				// Playlist was auto-deleted (empty). Return to list.
				m.plMgrRefreshList()
				m.plMgrScreen = plMgrScreenList
				m.plMgrCursor = 0
				return nil
			}
			m.plMgrTracks = tracks
			if m.plMgrCursor >= len(m.plMgrTracks) {
				m.plMgrCursor = len(m.plMgrTracks) - 1
			}
		}
	case "esc", "backspace", "h", "left":
		// Go back to playlist list.
		m.plMgrRefreshList()
		m.plMgrScreen = plMgrScreenList
		// Try to position cursor on the playlist we just left.
		for i, pl := range m.plMgrPlaylists {
			if pl.Name == m.plMgrSelPlaylist {
				m.plMgrCursor = i
				break
			}
		}
		m.plMgrConfirmDel = false
	}
	return nil
}

// handlePlMgrNewNameKey handles keys on screen 2 (new playlist name input).
func (m *Model) handlePlMgrNewNameKey(msg tea.KeyMsg) tea.Cmd {
	switch msg.Type {
	case tea.KeyEscape:
		m.plMgrScreen = plMgrScreenList
	case tea.KeyEnter:
		name := strings.TrimSpace(m.plMgrNewName)
		if name != "" {
			m.addToPlaylist(name)
			m.plMgrRefreshList()
			m.plMgrScreen = plMgrScreenList
		}
	case tea.KeyBackspace:
		if len(m.plMgrNewName) > 0 {
			_, size := utf8.DecodeLastRuneInString(m.plMgrNewName)
			m.plMgrNewName = m.plMgrNewName[:len(m.plMgrNewName)-size]
		}
	default:
		if msg.Type == tea.KeyRunes {
			m.plMgrNewName += string(msg.Runes)
		}
	}
	return nil
}

// addToPlaylist appends the current track to a local playlist and shows a status message.
func (m *Model) addToPlaylist(name string) {
	track, idx := m.playlist.Current()
	if idx < 0 {
		m.saveMsg = "No track to add"
		m.saveMsgTTL = 40
		return
	}
	if err := m.localProvider.AddTrack(name, track); err != nil {
		m.saveMsg = fmt.Sprintf("Failed: %s", err)
		m.saveMsgTTL = 60
		return
	}
	m.saveMsg = fmt.Sprintf("Added to \"%s\"", name)
	m.saveMsgTTL = 60 // ~3s
}

// handleThemeKey processes key presses while the theme picker is open.
func (m *Model) handleThemeKey(msg tea.KeyMsg) tea.Cmd {
	count := len(m.themes) + 1 // +1 for Default
	switch msg.String() {
	case "ctrl+c":
		m.themePickerCancel()
		m.player.Close()
		m.quitting = true
		return tea.Quit
	case "up", "k":
		if m.themeCursor > 0 {
			m.themeCursor--
			m.themePickerApply() // live preview
		}
	case "down", "j":
		if m.themeCursor < count-1 {
			m.themeCursor++
			m.themePickerApply() // live preview
		}
	case "enter":
		m.themePickerSelect()
	case "esc", "q", "t":
		m.themePickerCancel()
	}
	return nil
}

// handleQueueKey processes key presses while the queue manager overlay is open.
func (m *Model) handleQueueKey(msg tea.KeyMsg) tea.Cmd {
	qLen := m.playlist.QueueLen()

	switch msg.String() {
	case "ctrl+c":
		m.showQueue = false
		m.player.Close()
		m.quitting = true
		return tea.Quit
	case "ctrl+k":
		m.showKeymap = true
	case "up", "k":
		if m.queueCursor > 0 {
			m.queueCursor--
		}
	case "down", "j":
		if m.queueCursor < qLen-1 {
			m.queueCursor++
		}
	case "d":
		if qLen > 0 {
			m.playlist.RemoveQueueAt(m.queueCursor)
			if m.queueCursor >= m.playlist.QueueLen() && m.queueCursor > 0 {
				m.queueCursor--
			}
		}
	case "c":
		m.playlist.ClearQueue()
		m.showQueue = false
	case "esc", "A":
		m.showQueue = false
	}
	return nil
}

// keymapEntry is a key-action pair for the keymap overlay.
type keymapEntry struct{ key, action string }

// keymapEntries is the full list of keybindings shown in the keymap overlay.
var keymapEntries = []keymapEntry{
	{"Space", "Play / Pause"},
	{"s", "Stop"},
	{"> .", "Next track"},
	{"< ,", "Previous track"},
	{"← →", "Seek ±5s"},
	{"+ -", "Volume up/down"},
	{"m", "Toggle mono"},
	{"e", "Cycle EQ preset"},
	{"t", "Choose theme"},
	{"v", "Cycle visualizer"},
	{"↑ ↓", "Playlist scroll / EQ adjust"},
	{"h l", "EQ cursor left/right"},
	{"Enter", "Play selected track"},
	{"a", "Toggle queue (play next)"},
	{"A", "Queue manager"},
	{"p", "Playlist manager"},
	{"i", "Track info / metadata"},
	{"S", "Save track to ~/Music"},
	{"r", "Cycle repeat"},
	{"z", "Toggle shuffle"},
	{"x", "Expand/collapse playlist"},
	{"/", "Search playlist"},
	{"Tab", "Toggle focus"},
	{"Esc", "Back to provider"},
	{"Ctrl+K", "This keymap"},
	{"q", "Quit"},
}

// handleKeymapKey processes key presses while the keymap overlay is open.
func (m *Model) handleKeymapKey(msg tea.KeyMsg) tea.Cmd {
	switch msg.Type {
	case tea.KeyEscape:
		m.showKeymap = false
		m.keymapSearch = ""
		m.keymapFiltered = nil
		m.keymapCursor = 0
	case tea.KeyUp:
		if m.keymapCursor > 0 {
			m.keymapCursor--
		}
	case tea.KeyDown:
		count := len(keymapEntries)
		if m.keymapSearch != "" {
			count = len(m.keymapFiltered)
		}
		if m.keymapCursor < count-1 {
			m.keymapCursor++
		}
	case tea.KeyBackspace:
		if len(m.keymapSearch) > 0 {
			_, size := utf8.DecodeLastRuneInString(m.keymapSearch)
			m.keymapSearch = m.keymapSearch[:len(m.keymapSearch)-size]
			m.updateKeymapFilter()
		}
	default:
		switch msg.String() {
		case "ctrl+c":
			m.showKeymap = false
			m.player.Close()
			m.quitting = true
			return tea.Quit
		default:
			if msg.Type == tea.KeyRunes {
				m.keymapSearch += string(msg.Runes)
				m.updateKeymapFilter()
			}
		}
	}
	return nil
}

// updateKeymapFilter rebuilds the filtered indices and clamps the cursor.
func (m *Model) updateKeymapFilter() {
	m.keymapFiltered = nil
	m.keymapCursor = 0
	if m.keymapSearch == "" {
		return
	}
	query := strings.ToLower(m.keymapSearch)
	for i, e := range keymapEntries {
		if strings.Contains(strings.ToLower(e.key), query) ||
			strings.Contains(strings.ToLower(e.action), query) {
			m.keymapFiltered = append(m.keymapFiltered, i)
		}
	}
}
