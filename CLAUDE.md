# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Project

CLIAMP is a retro terminal music player inspired by Winamp 2.x, written in Go. It plays local files (MP3, FLAC, WAV, OGG, M4A, AAC, Opus, WMA), HTTP streams, YouTube/SoundCloud/Bandcamp (via yt-dlp), Spotify (via go-librespot), and Navidrome/Subsonic servers. Features include spectrum visualizers, 10-band EQ, gapless playback, MPRIS integration, and lyrics display.

Forked from [bjarneo/cliamp](https://github.com/bjarneo/cliamp).

## Commands

```bash
# Build
go build -ldflags="-s -w" -o cliamp .

# Build with version
go build -ldflags="-s -w -X main.version=v1.2.3" -o cliamp .

# Run
./cliamp ~/Music
./cliamp --volume -5 --shuffle --repeat all ~/Music

# Test
go test ./...
go test -v ./ui        # single package
go test -run TestParseJumpTarget ./ui  # single test

# GoReleaser snapshot (test build, no publish)
goreleaser build --single-target --snapshot --clean
```

## Architecture

### Startup Flow (main.go)
CLI flags → config load (`~/.config/cliamp/config.toml`) → flag overrides → init providers → resolve args → create player → create TUI → optional MPRIS → run Bubbletea program.

### Key Packages

| Package | Purpose |
|---------|---------|
| `config/` | TOML config + CLI flag parsing. Custom line-by-line parser (no external TOML library). |
| `player/` | Audio engine built on Beep. Manages decode → resample → EQ → volume → tap → speaker pipeline. |
| `playlist/` | Track list, shuffle/repeat, Provider interface, composite multiplexer. |
| `resolve/` | Resolves CLI args to tracks (files, dirs, URLs, M3U, PLS, RSS, yt-dlp). |
| `external/` | Provider implementations: `navidrome/`, `spotify/`, `local/`. |
| `ui/` | Bubbletea TUI. Single `Model` with focus-area state machine (playlist, EQ, search, provider, net search). Overlays for info, queue, lyrics, themes, file browser. |
| `theme/` | Color themes loaded from `theme/themes/*.toml`. |
| `mpris/` | MPRIS D-Bus integration (Linux only; stub on other platforms). |
| `lyrics/` | Lyrics fetching and display. |
| `upgrade/` | Self-upgrade: downloads latest release binary. |
| `telemetry/` | Anonymous monthly usage ping. |

### Audio Pipeline
```
Gapless Switcher → Decode → Resample → 10-band EQ (biquad) → Volume (dB) → Tap (FFT sidechain) → Speaker
```
Next track is preloaded concurrently for gapless transitions (3s lead for Navidrome, 15s for yt-dlp).

### Provider Interface
```go
type Provider interface {
    Name() string
    Playlists() ([]PlaylistInfo, error)
    Tracks(playlistID string) ([]Track, error)
}
```
`CompositeProvider` multiplexes providers using `"providerIndex:playlistID"` routing.

### Platform-Specific Code
- `player/device_darwin.go` — CoreAudio sample rate detection (macOS)
- `player/device_other.go` — stub for Linux/Windows
- `mpris/mpris.go` vs `mpris/mpris_stub.go` — Linux-only D-Bus

## Key Patterns

- **Atomic types for lock-free updates**: `atomic.Value` for stream title, `atomic.Uint64` for EQ bands (float64 stored as bits).
- **Version injection**: Build-time `-X main.version={{.Version}}` via ldflags/GoReleaser.
- **CGO required**: Native audio I/O (CoreAudio on macOS, ALSA on Linux) requires CGO_ENABLED=1.
- **FFmpeg subprocess**: M4A/AAC/ALAC/Opus/WMA decoded via `ffmpeg` pipe process (`player/ffmpeg.go`).
- **Focus areas over separate models**: Single UI `Model` manages all modes via focus state enum, reducing coupling.
- **Config persistence**: `config.Save(key, value)` updates single keys (e.g., theme at shutdown).

## Release Process

Automated via GitHub Actions on tag push (`v*`):
1. Matrix build: 4 native compiles (linux/amd64, linux/arm64, darwin/amd64, darwin/arm64)
2. GoReleaser: archives, checksums, GitHub Release
3. Homebrew tap auto-update
4. Optional AUR publish (requires secrets)

Config: `.goreleaser.yml`, `.github/workflows/release.yml`, `.github/workflows/aur.yml`.
