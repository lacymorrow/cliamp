# CLIAMP

A retro terminal music player inspired by Winamp 2.x. MP3 playback with a 10-band spectrum visualizer, 10-band parametric EQ, and playlist management.

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
go run . track.mp3
```

## Build

```sh
go build -o cliamp .
./cliamp *.mp3
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
| `r` | Cycle repeat (Off / All / One) |
| `z` | Toggle shuffle |
| `q` | Quit |
