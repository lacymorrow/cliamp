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

func run(overrides config.Overrides, positional []string) error {
	cfg, err := config.Load()
	if err != nil {
		return fmt.Errorf("config: %w", err)
	}
	overrides.Apply(&cfg)

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

	resolved, err := resolve.Args(positional)
	if err != nil {
		return err
	}

	if len(resolved.Tracks) == 0 && len(resolved.Pending) == 0 && provider == nil {
		return errors.New(`usage: cliamp <file|folder|url> [...]

  Local files     cliamp track.mp3 song.flac ~/Music
  Local M3U/PLS   cliamp ~/radio-stations.m3u stations.pls
  HTTP stream     cliamp https://example.com/song.mp3
  Radio / M3U     cliamp http://radio.example.com/stream.m3u
  Radio / PLS     cliamp http://radio.example.com/listen.pls
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

	p, err := player.New(player.Quality{
		SampleRate:      cfg.SampleRate,
		BufferMs:        cfg.BufferMs,
		ResampleQuality: cfg.ResampleQuality,
	})
	if err != nil {
		return fmt.Errorf("player: %w", err)
	}
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
	if overrides.Play != nil && *overrides.Play {
		m.SetAutoPlay(true)
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

Playback:
  --volume <dB>           Volume in dB, range [-30, +6] (e.g. --volume -5)
  --shuffle
  --repeat <off|all|one>
  --mono / --no-mono
  --auto-play             Start playback immediately

Audio engine:
  --sample-rate <Hz>      Output sample rate (22050, 44100, 48000, 96000, 192000)
  --buffer-ms <ms>        Speaker buffer in milliseconds (50–500)
  --resample-quality <n>  Resample quality factor (1–4)

Appearance:
  --theme <name>          UI theme name
  --eq-preset <name>      EQ preset name (e.g. "Bass Boost")

General:
  -h, --help              Show this help message
  -v, --version           Show the current version
  --upgrade               Upgrade cliamp to the latest release

Examples:
  cliamp track.mp3 song.flac ~/Music
  cliamp --shuffle --volume -5 track.mp3
  cliamp track.mp3 --repeat all --mono
  cliamp --auto-play --shuffle ~/Music
  cliamp --eq-preset "Bass Boost" ~/Music
  cliamp https://example.com/song.mp3
  cliamp http://radio.example.com/stream.m3u
  cliamp https://soundcloud.com/user/sets/playlist
  cliamp https://www.youtube.com/watch?v=...

Environment:
  NAVIDROME_URL, NAVIDROME_USER, NAVIDROME_PASS   Navidrome server

Playlists: ~/.config/cliamp/playlists/*.toml
Formats:   mp3, wav, flac, ogg, m4a, aac, opus, wma (aac/opus/wma need ffmpeg)
SoundCloud/YouTube/Bandcamp require yt-dlp`

func main() {
	action, overrides, positional, err := config.ParseFlags(os.Args[1:])
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}

	switch action {
	case "help":
		fmt.Println(helpText)
		return
	case "version":
		if version == "" {
			fmt.Println("cliamp (dev build)")
		} else {
			fmt.Printf("cliamp %s\n", version)
		}
		return
	case "upgrade":
		if err := upgrade.Run(version); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		return
	}

	if err := run(overrides, positional); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
