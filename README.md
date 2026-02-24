# CLIAMP

A retro terminal music player inspired by Winamp 2.x. Plays MP3, WAV, FLAC, OGG, AAC, ALAC, Opus, and WMA with a 10-band spectrum visualizer, 10-band parametric EQ, and playlist management.

Built with [Bubbletea](https://github.com/charmbracelet/bubbletea), [Lip Gloss](https://github.com/charmbracelet/lipgloss), and [Beep](https://github.com/gopxl/beep).


https://github.com/user-attachments/assets/270ee066-95d2-4a3b-90bc-68a67ae9b92f


## Ascii
```
╭────────────────────────────────────────────────────────────────╮
│                                                                │
│  C L I A M P                                                   │
│  ♫ Artist - Song Title                                         │
│  01:23 / 04:56                                    ▶ Playing    │
│                                                                │
│  █████ ▇▇▇▇▇ ▅▅▅▅▅ █████ ▃▃▃▃▃ ▅▅▅▅▅ ▇▇▇▇▇ ▃▃▃▃▃ ▂▂▂▂▂ ▁▁▁▁▁   │
│  ━━━━━━━━━━━━━━━━━━━━━━━━━━●━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━  │
│                                                                │
│  VOL ██████████████████░░░░  +0.0dB                            │
│  EQ  70 180 320 600 1k 3k 6k 12k 14k 16k                       │
│                                                                │
│  ── Playlist ── [Shuffle] [Repeat: Off] ──                     │
│  ▶ 1. Song One                                                 │
│    2. Song Two                                                 │
│                                                                │
│  [Spc]⏯  [<>]Trk [←→]Seek [+-]Vol [Tab]Focus [Q]Quit           │
│                                                                │
╰────────────────────────────────────────────────────────────────╯
```

## Run in dev

```sh
go run . track.mp3 song.flac
go run . ~/Music/album
```

## Build

```sh
go build -o cliamp .
./cliamp *.mp3 *.flac *.wav *.ogg
./cliamp ~/Music                   # recursively finds all audio files
./cliamp ~/Music/jazz ~/Music/rock # multiple folders
./cliamp ~/Music song.mp3          # mix folders and files
```

### ffmpeg (optional)

AAC, ALAC (`.m4a`), Opus, and WMA playback requires [ffmpeg](https://ffmpeg.org/) installed:

```sh
# Arch
sudo pacman -S ffmpeg
# Debian/Ubuntu
sudo apt install ffmpeg
# macOS
brew install ffmpeg
```

MP3, WAV, FLAC, and OGG work without ffmpeg.

## Configuration

Copy the example config to get started:

```sh
mkdir -p ~/.config/cliamp
cp config.toml.example ~/.config/cliamp/config.toml
```

```toml
# Default volume in dB (range: -30 to 6)
volume = 0

# Repeat mode: "off", "all", or "one"
repeat = "off"

# Start with shuffle enabled
shuffle = false

# EQ preset: "Flat", "Rock", "Pop", "Jazz", "Classical",
#             "Bass Boost", "Treble Boost", "Vocal", "Electronic", "Acoustic"
# Leave empty or "Custom" to use manual eq values below
eq_preset = "Flat"

# 10-band EQ gains in dB (range: -12 to 12)
# Bands: 70Hz, 180Hz, 320Hz, 600Hz, 1kHz, 3kHz, 6kHz, 12kHz, 14kHz, 16kHz
eq = [0, 0, 0, 0, 0, 0, 0, 0, 0, 0]
```

## Keys

| Key | Action |
|---|---|
| `Space` | Play / Pause |
| `s` | Stop |
| `>` `.` | Next track |
| `<` `,` | Previous track |
| `Left` `Right` | Seek -/+5s |
| `+` `-` | Volume up/down |
| `Tab` | Toggle focus (Playlist / EQ) |
| `j` `k` / `Up` `Down` | Playlist scroll / EQ band adjust |
| `h` `l` | EQ cursor left/right |
| `Enter` | Play selected track |
| `e` | Cycle EQ preset |
| `/` | Search playlist |
| `a` | Toggle queue (play next) |
| `r` | Cycle repeat (Off / All / One) |
| `z` | Toggle shuffle |
| `q` | Quit |

## Author

[x.com/iamdothash](https://x.com/iamdothash)
