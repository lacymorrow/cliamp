package main

import (
	"fmt"
	"os"
	"path/filepath"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/gopxl/beep/v2"

	"winamp-cli/player"
	"winamp-cli/playlist"
	"winamp-cli/ui"
)

func main() {
	if len(os.Args) < 2 {
		fmt.Fprintln(os.Stderr, "Usage: cliamp <file.mp3> [file2.mp3 ...]")
		os.Exit(1)
	}

	// Expand shell globs that may not have been expanded by the shell
	var files []string
	for _, arg := range os.Args[1:] {
		matches, err := filepath.Glob(arg)
		if err != nil || len(matches) == 0 {
			files = append(files, arg)
		} else {
			files = append(files, matches...)
		}
	}

	// Build playlist from file arguments
	pl := playlist.New()
	for _, f := range files {
		pl.Add(playlist.TrackFromPath(f))
	}

	// Initialize audio engine at CD-quality sample rate
	sr := beep.SampleRate(44100)
	p := player.New(sr)
	defer p.Close()

	// Launch the TUI
	m := ui.NewModel(p, pl)
	prog := tea.NewProgram(m, tea.WithAltScreen())
	if _, err := prog.Run(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
