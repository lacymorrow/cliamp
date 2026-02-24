// Package main is the entry point for the CLIAMP terminal music player.
package main

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/gopxl/beep/v2"

	"cliamp/config"
	"cliamp/player"
	"cliamp/playlist"
	"cliamp/ui"
)

// audioExts is the set of file extensions the player can decode.
var audioExts = map[string]bool{
	".mp3":  true,
	".wav":  true,
	".flac": true,
	".ogg":  true,
}

func run() error {
	if len(os.Args) < 2 {
		return errors.New("usage: cliamp <file|folder> [...]")
	}

	// Expand shell globs and resolve directories into audio files
	var files []string
	for _, arg := range os.Args[1:] {
		matches, err := filepath.Glob(arg)
		if err != nil || len(matches) == 0 {
			matches = []string{arg}
		}
		for _, path := range matches {
			resolved, err := collectAudioFiles(path)
			if err != nil {
				return fmt.Errorf("scanning %s: %w", path, err)
			}
			files = append(files, resolved...)
		}
	}

	if len(files) == 0 {
		return errors.New("no playable files found (supported: mp3, wav, flac, ogg)")
	}

	// Build playlist from file arguments
	pl := playlist.New()
	for _, f := range files {
		pl.Add(playlist.TrackFromPath(f))
	}

	// Load user config
	cfg, err := config.Load()
	if err != nil {
		return fmt.Errorf("config: %w", err)
	}

	// Initialize audio engine at CD-quality sample rate
	sr := beep.SampleRate(44100)
	p := player.New(sr)
	defer p.Close()

	// Apply config
	p.SetVolume(cfg.Volume)
	if cfg.EQPreset == "" || cfg.EQPreset == "Custom" {
		for i, gain := range cfg.EQ {
			p.SetEQBand(i, gain)
		}
	}
	switch cfg.Repeat {
	case "all":
		pl.CycleRepeat() // off -> all
	case "one":
		pl.CycleRepeat() // off -> all
		pl.CycleRepeat() // all -> one
	}
	if cfg.Shuffle {
		pl.ToggleShuffle()
	}

	// Launch the TUI
	m := ui.NewModel(p, pl)
	if cfg.EQPreset != "" && cfg.EQPreset != "Custom" {
		m.SetEQPreset(cfg.EQPreset)
	}
	prog := tea.NewProgram(m, tea.WithAltScreen())
	finalModel, err := prog.Run()
	if err != nil {
		return fmt.Errorf("tui: %w", err)
	}

	// Save current state for next session
	fm := finalModel.(ui.Model)
	cfg.Volume = p.Volume()
	cfg.EQ = p.EQBands()
	cfg.EQPreset = fm.EQPresetName()
	cfg.Repeat = strings.ToLower(pl.Repeat().String())
	cfg.Shuffle = pl.Shuffled()
	if err := config.Save(cfg); err != nil {
		return fmt.Errorf("saving config: %w", err)
	}

	return nil
}

// collectAudioFiles returns audio file paths for the given argument.
// If path is a directory, it walks it recursively collecting supported files.
// If path is a file with a supported extension, it returns it directly.
func collectAudioFiles(path string) ([]string, error) {
	info, err := os.Stat(path)
	if err != nil {
		return nil, err
	}

	if !info.IsDir() {
		if audioExts[strings.ToLower(filepath.Ext(path))] {
			return []string{path}, nil
		}
		return nil, nil
	}

	var files []string
	err = filepath.WalkDir(path, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !d.IsDir() && audioExts[strings.ToLower(filepath.Ext(p))] {
			files = append(files, p)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}

	sort.Strings(files)
	return files, nil
}

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
