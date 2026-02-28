# MPRIS Integration

Cliamp exposes an [MPRIS2](https://specifications.freedesktop.org/mpris-spec/latest/) D-Bus service on Linux. This allows desktop environments, media key daemons, and command line tools like `playerctl` to control playback, read track metadata, and adjust volume without touching the TUI.

## Requirements

A running D-Bus session bus is the only requirement. Most Linux desktop environments and Wayland compositors provide one automatically. No extra packages are needed beyond a tool to talk to D-Bus such as `playerctl`.

## Bus Name

Cliamp registers itself as:

```
org.mpris.MediaPlayer2.cliamp
```

Only one instance can hold this name at a time. If a second Cliamp process tries to start, the MPRIS registration will fail silently and that instance will run without D-Bus integration.

## Supported Operations

### Playback Control

All standard transport commands are supported through the `org.mpris.MediaPlayer2.Player` interface:

| playerctl command | Effect |
|---|---|
| `playerctl play-pause` | Toggle play / pause |
| `playerctl play` | Resume playback |
| `playerctl pause` | Pause playback |
| `playerctl stop` | Stop playback |
| `playerctl next` | Skip to the next track |
| `playerctl previous` | Go to the previous track (or restart if more than 3 seconds in) |

### Seeking

Relative and absolute seeking are both supported:

```sh
playerctl position 30          # seek to 30 seconds
playerctl position 5+          # seek forward 5 seconds
playerctl position 5-          # seek backward 5 seconds
```

Desktop widgets that display a progress bar will receive `Seeked` signals and stay in sync.

### Volume

Volume is exposed as a linear value between 0.0 and 1.0. Internally Cliamp uses a decibel scale (from 30 dB to +6 dB), and the conversion happens automatically.

```sh
playerctl volume               # print current volume (0.0 to 1.0)
playerctl volume 0.5           # set volume to 50%
```

Setting volume through `playerctl` updates the player immediately. Changing volume with the `+` and `-` keys in the TUI is reflected back to D-Bus clients on the next tick.

### Metadata

Track metadata is published under the standard MPRIS keys:

| Key | Description |
|---|---|
| `mpris:trackid` | D-Bus object path identifying the current track |
| `xesam:title` | Track title |
| `xesam:artist` | Artist name (as a list with one entry) |
| `xesam:album` | Album name, when available |
| `xesam:url` | File path or stream URL |
| `mpris:length` | Duration in microseconds |

Query metadata with:

```sh
playerctl metadata              # all keys
playerctl metadata artist       # just the artist
playerctl metadata title        # just the title
```

For live radio streams that provide ICY metadata, the artist and title fields update dynamically as the station reports new track information.

### Status

```sh
playerctl status                # prints Playing, Paused, or Stopped
```

## Architecture

The implementation lives in the `mpris` package. On Linux (`mpris/mpris.go`), it connects to the session bus, claims the MPRIS bus name, and exports two D-Bus interfaces:

1. `org.mpris.MediaPlayer2` provides identity and quit support.
2. `org.mpris.MediaPlayer2.Player` provides playback methods and properties.

Properties are managed through the `godbus/dbus/v5/prop` package. The `Update` method on the service is called from the Bubbletea event loop whenever playback state changes. It uses `SetMust` rather than `Set` to bypass the property library's writable checks and callback triggers, which are intended for external D-Bus writes and would cause issues when called internally.

For writable properties like Volume, a callback is registered that injects a message into the Bubbletea event loop through `prog.Send`. Because the callback runs inside the property library's mutex and `prog.Send` writes to an unbuffered channel, the send is wrapped in a goroutine to avoid a deadlock between the D-Bus handler and the event loop.

On non-Linux platforms (`mpris/mpris_stub.go`), all types and function signatures are mirrored as no-ops so the rest of the codebase compiles without build tags.

## Limitations

Shuffle and loop status are not exposed as D-Bus properties. The `z` and `r` keys in the TUI control shuffle and repeat locally, but these states are not visible to or controllable from external tools.

The `HasTrackList` property is set to false. Cliamp does not implement the optional `org.mpris.MediaPlayer2.TrackList` interface.
