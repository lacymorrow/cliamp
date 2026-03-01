// Package main is the entry point for the CLIAMP terminal music player.
package main

import (
	"errors"
	"fmt"
	"os"

	tea "github.com/charmbracelet/bubbletea"

	"cliamp/config"
	"cliamp/external/local"
	"cliamp/external/navidrome"
	"cliamp/mpris"
	"cliamp/player"
	"cliamp/playlist"
	"cliamp/resolve"
	"cliamp/theme"
	"cliamp/ui"
	"cliamp/upgrade"
)

// version is set at build time via -ldflags "-X main.version=vX.Y.Z".
var version string

func run() error {
	cfg, err := config.Load()
	if err != nil {
		return fmt.Errorf("config: %w", err)
	}

	var navProv playlist.Provider
	if c := navidrome.NewFromEnv(); c != nil {
		navProv = c
	}
	localProv := local.New()
	var localAsProvider playlist.Provider
	if localProv != nil {
		if pls, _ := localProv.Playlists(); len(pls) > 0 {
			localAsProvider = localProv
		}
	}
	var provider playlist.Provider
	if cp := playlist.NewComposite(navProv, localAsProvider); cp != nil {
		provider = cp
	}

	defer resolve.CleanupYTDL()

	resolved, err := resolve.Args(os.Args[1:])
	if err != nil {
		return err
	}

	if len(resolved.Tracks) == 0 && len(resolved.Pending) == 0 && provider == nil {
		return errors.New(`usage: cliamp <file|folder|url> [...]

  Local files     cliamp track.mp3 song.flac ~/Music
  Local M3U       cliamp ~/radio-stations.m3u
  HTTP stream     cliamp https://example.com/song.mp3
  Radio / M3U     cliamp http://radio.example.com/stream.m3u
  Podcast feed    cliamp https://example.com/podcast/feed.xml
  SoundCloud      cliamp https://soundcloud.com/user/sets/playlist
  YouTube         cliamp https://www.youtube.com/watch?v=...
  Bandcamp        cliamp https://artist.bandcamp.com/album/...

  Navidrome       Set NAVIDROME_URL, NAVIDROME_USER, NAVIDROME_PASS
  Playlists       ~/.config/cliamp/playlists/*.toml

Formats: mp3, wav, flac, ogg, m4a, aac, opus, wma (aac/opus/wma need ffmpeg)
SoundCloud/YouTube/Bandcamp require yt-dlp (brew install yt-dlp)`)
	}

	pl := playlist.New()
	pl.Add(resolved.Tracks...)

	p := player.New(player.Quality{
		SampleRate:      cfg.SampleRate,
		BufferMs:        cfg.BufferMs,
		ResampleQuality: cfg.ResampleQuality,
	})
	defer p.Close()

	cfg.ApplyPlayer(p)
	cfg.ApplyPlaylist(pl)

	themes := theme.LoadAll()

	m := ui.NewModel(p, pl, provider, localProv, themes)
	m.SetPendingURLs(resolved.Pending)
	if len(resolved.Tracks) == 0 && len(resolved.Pending) == 0 {
		m.StartInProvider()
	}
	if cfg.EQPreset != "" && cfg.EQPreset != "Custom" {
		m.SetEQPreset(cfg.EQPreset)
	}
	if cfg.Theme != "" {
		m.SetTheme(cfg.Theme)
	}

	prog := tea.NewProgram(m, tea.WithAltScreen())

	if svc, err := mpris.New(func(msg interface{}) { prog.Send(msg) }); err == nil && svc != nil {
		defer svc.Close()
		go prog.Send(mpris.InitMsg{Svc: svc})
	}

	finalModel, err := prog.Run()
	if err != nil {
		return err
	}

	// Persist theme selection across restarts.
	if fm, ok := finalModel.(ui.Model); ok {
		themeName := fm.ThemeName()
		if themeName == theme.DefaultName {
			themeName = ""
		}
		_ = config.Save("theme", fmt.Sprintf("%q", themeName))
	}

	return nil
}

const helpText = `cliamp — retro terminal music player

Usage: cliamp [flags] <file|folder|url> [...]

Flags:
  --help       Show this help message
  --upgrade    Upgrade cliamp to the latest release
  --version    Show the current version

Examples:
  cliamp track.mp3 song.flac ~/Music
  cliamp ~/radio-stations.m3u
  cliamp https://example.com/song.mp3
  cliamp http://radio.example.com/stream.m3u
  cliamp https://example.com/podcast/feed.xml
  cliamp https://soundcloud.com/user/sets/playlist
  cliamp https://www.youtube.com/watch?v=...
  cliamp https://artist.bandcamp.com/album/...

Environment:
  NAVIDROME_URL, NAVIDROME_USER, NAVIDROME_PASS   Navidrome server

Playlists: ~/.config/cliamp/playlists/*.toml
Formats:   mp3, wav, flac, ogg, m4a, aac, opus, wma (aac/opus/wma need ffmpeg)
SoundCloud/YouTube/Bandcamp require yt-dlp`

func main() {
	if len(os.Args) > 1 {
		switch os.Args[1] {
		case "--help", "-h":
			fmt.Println(helpText)
			return
		case "--version", "-v":
			if version == "" {
				fmt.Println("cliamp (dev build)")
			} else {
				fmt.Printf("cliamp %s\n", version)
			}
			return
		case "--upgrade":
			if err := upgrade.Run(version); err != nil {
				fmt.Fprintln(os.Stderr, err)
				os.Exit(1)
			}
			return
		}
	}

	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
