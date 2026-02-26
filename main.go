// Package main is the entry point for the CLIAMP terminal music player.
package main

import (
	"errors"
	"fmt"
	"os"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/gopxl/beep/v2"

	"cliamp/config"
	"cliamp/external/navidrome"
	"cliamp/mpris"
	"cliamp/player"
	"cliamp/playlist"
	"cliamp/resolve"
	"cliamp/ui"
)

func run() error {
	cfg, err := config.Load()
	if err != nil {
		return fmt.Errorf("config: %w", err)
	}

	var provider playlist.Provider
	if c := navidrome.NewFromEnv(); c != nil {
		provider = c
	}

	resolved, err := resolve.Args(os.Args[1:])
	if err != nil {
		return err
	}

	if len(resolved.Tracks) == 0 && len(resolved.Pending) == 0 && provider == nil {
		return errors.New("usage: cliamp <file|folder> [...] or configure a provider via ENV\n\n - Navidrome: NAVIDROME_URL, NAVIDROME_USER, NAVIDROME_PASS\n")
	}

	pl := playlist.New()
	pl.Add(resolved.Tracks...)

	p := player.New(beep.SampleRate(player.DefaultSampleRate))
	defer p.Close()

	cfg.ApplyPlayer(p)
	cfg.ApplyPlaylist(pl)

	m := ui.NewModel(p, pl, provider)
	m.SetPendingURLs(resolved.Pending)
	if cfg.EQPreset != "" && cfg.EQPreset != "Custom" {
		m.SetEQPreset(cfg.EQPreset)
	}

	prog := tea.NewProgram(m, tea.WithAltScreen())

	if svc, err := mpris.New(func(msg interface{}) { prog.Send(msg) }); err == nil && svc != nil {
		defer svc.Close()
		go prog.Send(mpris.InitMsg{Svc: svc})
	}

	_, err = prog.Run()
	return err
}

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
