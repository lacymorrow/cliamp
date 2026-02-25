// Package main is the entry point for the CLIAMP terminal music player.
package main

import (
	"bufio"
	"errors"
	"fmt"
	"io/fs"
	"net/http"
	"os"
	"path/filepath"
	"slices"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/gopxl/beep/v2"

	"cliamp/config"
	"cliamp/external/navidrome"
	"cliamp/mpris"
	"cliamp/player"
	"cliamp/playlist"
	"cliamp/ui"
)

func run() error {
	var provider playlist.Provider

	navURL := os.Getenv("NAVIDROME_URL")
	navUser := os.Getenv("NAVIDROME_USER")
	navPass := os.Getenv("NAVIDROME_PASS")

	if navURL != "" && navUser != "" && navPass != "" {
		provider = &navidrome.NavidromeClient{URL: navURL, User: navUser, Password: navPass}
	}

	if len(os.Args) < 2 && provider == nil {
		return errors.New("usage: cliamp <file|folder> [...] or configure a provider via ENV\n\n - Navidrome: NAVIDROME_URL, NAVIDROME_USER, NAVIDROME_PASS\n")
	}

	// Expand shell globs and resolve directories into audio files
	var files []string
	for _, arg := range os.Args[1:] {
		// URLs bypass glob expansion and filesystem checks
		if playlist.IsURL(arg) {
			if playlist.IsM3U(arg) {
				streams, err := resolveM3U(arg)
				if err != nil {
					return fmt.Errorf("resolving m3u %s: %w", arg, err)
				}
				files = append(files, streams...)
			} else {
				files = append(files, arg)
			}
			continue
		}
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

	if len(files) == 0 && provider == nil {
		return errors.New("no playable files found")
	}

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
	m := ui.NewModel(p, pl, provider)
	if cfg.EQPreset != "" && cfg.EQPreset != "Custom" {
		m.SetEQPreset(cfg.EQPreset)
	}
	prog := tea.NewProgram(m, tea.WithAltScreen())

	// Start MPRIS D-Bus service for media key / playerctl support.
	mprisSvc, err := mpris.New(func(msg interface{}) { prog.Send(msg) })
	if err == nil && mprisSvc != nil {
		defer mprisSvc.Close()
		go prog.Send(mpris.InitMsg{Svc: mprisSvc})
	}

	if _, err := prog.Run(); err != nil {
		return fmt.Errorf("tui: %w", err)
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
		if player.SupportedExts[strings.ToLower(filepath.Ext(path))] {
			return []string{path}, nil
		}
		return nil, nil
	}

	var files []string
	err = filepath.WalkDir(path, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !d.IsDir() && player.SupportedExts[strings.ToLower(filepath.Ext(p))] {
			files = append(files, p)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}

	slices.Sort(files)
	return files, nil
}

// resolveM3U fetches an M3U playlist URL and returns the stream URLs it contains.
func resolveM3U(m3uURL string) ([]string, error) {
	resp, err := http.Get(m3uURL)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	var urls []string
	scanner := bufio.NewScanner(resp.Body)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		urls = append(urls, line)
	}
	return urls, scanner.Err()
}

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
