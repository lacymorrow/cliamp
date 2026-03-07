# Apple Music Provider — Implementation Spec

## Research Summary

### Open Source Landscape

| Project | Language | Approach | Streaming? | Audio Pipeline? |
|---------|----------|----------|------------|-----------------|
| **[Yatoro](https://github.com/jayadamsmorgan/Yatoro)** | Swift | MusicKit `ApplicationMusicPlayer` | ✅ Full catalog | ❌ System audio only |
| **[Cider v1](https://github.com/ciderapp/Cider)** | Electron/Vue | MusicKit JS + Widevine CDM | ✅ Full catalog (lossy only) | ✅ Web Audio API (EQ, visualizer) |
| **[Cider v2](https://github.com/ciderapp/Cider-2)** | Closed source | Same as v1 | ✅ | ✅ |
| **[ryanccn/am](https://github.com/ryanccn/am)** | Rust | AppleScript → Music.app | ❌ Control only | ❌ |
| **[Apple-Music-CLI-Player](https://github.com/mcthomas/Apple-Music-CLI-Player)** | Shell | AppleScript → Music.app | ❌ Control only | ❌ |
| **[Musish](https://github.com/Musish/Musish)** | React | MusicKit JS (browser) | ✅ In-browser only | ❌ |
| **[go-apple-music](https://github.com/minchao/go-apple-music)** | Go | Apple Music REST API | ❌ Metadata only | ❌ |

### Approach Analysis

#### ❌ AppleScript / Music.app Control
- Can list playlists and track metadata
- **Cannot play cloud/streaming tracks** — `play` command fails for Apple Music tracks without local files
- Confirmed on this machine: all 139 library tracks are `shared track` with `cloud` location; `play` results in `stopped` state
- Only useful as a fallback for locally-downloaded tracks

#### ❌ MusicKit JS (Cider approach)
- Requires Electron/Chromium with Widevine CDM for DRM decryption
- Cider explicitly says: "Lossless playback not supported — decryption of lossless audio is not available in MusicKit JS"
- Way too heavy for a CLI app (ships an entire browser)
- Widevine CDM expires and causes issues (Cider-2 issue #1072)

#### ❌ Apple Music REST API (go-apple-music)
- Metadata only: catalog search, library browsing, playlist management
- **No stream URLs** — API does not expose audio content
- Useful for supplementary features (search, recommendations) but cannot play music

#### ✅ MusicKit Swift Framework (`ApplicationMusicPlayer`)
- **This is how Yatoro does it.** The only working approach for a non-browser app.
- `ApplicationMusicPlayer.shared` handles everything: auth, DRM, streaming, queue management
- Full Apple Music catalog access (search, library, playlists, stations, recommendations)
- Requires macOS 14+ (Sonoma)
- Requires embedded `Info.plist` with `NSAppleMusicUsageDescription` and a `CFBundleIdentifier`
- Audio plays through macOS system audio (MusicKit controls the output device)
- **No raw audio buffer access** — you can't tap into the decrypted PCM stream
- Yatoro proves this works in a CLI: Swift Package Manager binary with `-sectcreate __TEXT __info_plist` linker flags

### Key Insight: Separate Audio Paths

Apple Music tracks **cannot** go through cliamp's Beep audio pipeline (decode → resample → EQ → volume → FFT → speaker). The DRM-protected audio is decoded and played by a system process — `ApplicationMusicPlayer` is a remote control, not an audio source.

This means:
- When playing Apple Music: system audio out (no cliamp EQ, no FFT-based visualizer)
- When playing local/Spotify/Navidrome: normal Beep pipeline (full EQ + visualizer)

This is the same tradeoff Cider v2 hits: "Crossfade: we are limited on audio managing and modifying in MusicKit"

---

## Architecture: Swift Helper Binary + JSON IPC

### Why a helper binary?

cliamp is written in Go. MusicKit is a Swift-only framework. We can't use MusicKit from Go directly (no ObjC bridge exists for MusicKit — it's Swift-only with concurrency requirements). The cleanest path is:

```
cliamp (Go TUI) ←── stdin/stdout JSON lines ──→ cliamp-apple-music (Swift helper)
```

### Helper Binary: `cliamp-apple-music`

A small Swift Package Manager executable that:
1. Requests MusicKit authorization on first run
2. Listens for JSON commands on stdin
3. Sends JSON events/responses on stdout
4. Uses `ApplicationMusicPlayer.shared` for all playback
5. Uses MusicKit API for search, library, playlists

Embedded Info.plist (required for MusicKit entitlement):
```xml
<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "...">
<plist version="1.0">
<dict>
    <key>CFBundleName</key>
    <string>cliamp-apple-music</string>
    <key>CFBundleIdentifier</key>
    <string>com.cliamp.apple-music</string>
    <key>NSAppleMusicUsageDescription</key>
    <string>cliamp needs access to Apple Music for playback.</string>
</dict>
</plist>
```

### IPC Protocol (JSON Lines over stdin/stdout)

**Commands (cliamp → helper):**

```jsonl
{"cmd":"auth"}
{"cmd":"playlists"}
{"cmd":"tracks","playlistId":"p.AbCdEf123"}
{"cmd":"search","query":"bohemian rhapsody","type":"songs","limit":25}
{"cmd":"library_search","query":"athlete","type":"songs","limit":25}
{"cmd":"play","trackId":"1440123456"}
{"cmd":"play_playlist","playlistId":"p.AbCdEf123","startIndex":0}
{"cmd":"queue_add","trackId":"1440123456","position":"next"}
{"cmd":"pause"}
{"cmd":"resume"}
{"cmd":"next"}
{"cmd":"previous"}
{"cmd":"seek","seconds":30.5}
{"cmd":"seek_relative","delta":-10}
{"cmd":"set_volume","volume":0.8}
{"cmd":"set_shuffle","enabled":true}
{"cmd":"set_repeat","mode":"all"}
{"cmd":"now_playing"}
{"cmd":"queue"}
{"cmd":"recommendations","limit":10}
{"cmd":"recently_played","limit":25}
{"cmd":"station_from_song","trackId":"1440123456"}
```

**Responses (helper → cliamp):**

```jsonl
{"type":"auth","status":"authorized"}
{"type":"playlists","items":[{"id":"p.AbCd","name":"My Playlist","trackCount":42},...]}
{"type":"tracks","items":[{"id":"1440123456","title":"Wires","artist":"Athlete","album":"Tourist","duration":260,"trackNumber":1,"artworkUrl":"https://..."},...]}
{"type":"search","items":[...]}
{"type":"error","message":"Not authorized","code":"auth_required"}
```

**Events (helper → cliamp, unsolicited):**

```jsonl
{"type":"state","playing":true,"trackId":"1440123456","title":"Wires","artist":"Athlete","album":"Tourist","position":45.2,"duration":260.0,"volume":0.8,"shuffle":true,"repeat":"off"}
{"type":"queue_changed","entries":[{"id":"...","title":"...","artist":"..."},...]}
```

The helper emits `state` events at ~1Hz (or on state change) so cliamp can update the now-playing display.

---

## Go Provider Implementation

### File Structure

```
external/applemusic/
├── provider.go          # playlist.Provider implementation
├── helper.go            # Helper process lifecycle + IPC
├── helper_darwin.go     # macOS: spawn + manage Swift helper
├── helper_stub.go       # !darwin: return nil
├── protocol.go          # JSON command/response types
└── streamer.go          # Fake streamer (signals "playing via Apple Music")
```

```
tools/cliamp-apple-music/  # Swift Package Manager project
├── Package.swift
├── Resources/
│   └── Info.plist
└── Sources/
    └── main.swift         # ~300-500 lines: auth, IPC loop, MusicKit calls
```

### provider.go

```go
package applemusic

import "cliamp/playlist"

type AppleMusicProvider struct {
    helper *Helper
}

func New() *AppleMusicProvider {
    h, err := NewHelper()
    if err != nil {
        return nil  // not on macOS or helper binary not found
    }
    return &AppleMusicProvider{helper: h}
}

func (p *AppleMusicProvider) Name() string { return "Apple Music" }

func (p *AppleMusicProvider) Playlists() ([]playlist.PlaylistInfo, error) {
    resp, err := p.helper.Command("playlists", nil)
    // parse JSON response into []PlaylistInfo
    ...
}

func (p *AppleMusicProvider) Tracks(playlistID string) ([]playlist.Track, error) {
    resp, err := p.helper.Command("tracks", map[string]any{"playlistId": playlistID})
    // parse into []Track with Path = "applemusic:track:<id>"
    // Stream = true (not a file path)
    ...
}
```

### Playback Integration

When a track with `applemusic:track:<id>` URI is selected:
1. cliamp does NOT create a Beep streamer
2. Instead, sends `play` command to the helper
3. Helper's `ApplicationMusicPlayer` starts streaming
4. cliamp enters "remote playback" mode:
   - Polls helper for position/duration (or reads event stream)
   - Displays now-playing with progress bar
   - Routes play/pause/next/prev keypresses to helper commands
   - **Disables EQ controls** (grayed out, shows "N/A for Apple Music")
   - **Visualizer**: either disabled or shows a simple position-based animation

This requires a small refactor to cliamp's player to support a "remote" mode where it's not driving the audio pipeline itself. The `player.Model` already has state for position/duration — it just needs to accept external updates.

### StreamerFactory

Register a factory for `applemusic:` URIs that returns a special sentinel:

```go
// RegisterAppleMusicStreamer tells the player that applemusic: URIs
// are handled externally. Instead of decoding audio, the player
// delegates to the Apple Music helper process.
func RegisterAppleMusicStreamer(p *AppleMusicProvider) playlist.StreamerFactory {
    return func(uri string) (beep.StreamSeekCloser, beep.Format, time.Duration, error) {
        id := strings.TrimPrefix(uri, "applemusic:track:")
        p.helper.Command("play", map[string]any{"trackId": id})
        // Return a silent/no-op streamer — audio comes from system
        return &silentStreamer{}, beep.Format{SampleRate: 44100, NumChannels: 2, Precision: 2}, 0, nil
    }
}
```

Or better: detect `applemusic:` prefix in the player and skip the streamer pipeline entirely, entering remote-playback mode.

---

## Swift Helper Source (Sketch)

```swift
// tools/cliamp-apple-music/Sources/main.swift
import Foundation
import MusicKit

@main
struct AppleMusicHelper {
    static func main() async {
        // 1. Request authorization
        let status = await MusicAuthorization.request()
        guard status == .authorized else {
            emit(["type": "error", "message": "Not authorized", "code": "auth_required"])
            exit(1)
        }
        emit(["type": "auth", "status": "authorized"])

        let player = ApplicationMusicPlayer.shared

        // 2. Start state reporter (1Hz)
        Task {
            while true {
                try? await Task.sleep(for: .seconds(1))
                guard let entry = player.queue.currentEntry else { continue }
                let state: [String: Any] = [
                    "type": "state",
                    "playing": player.state.playbackStatus == .playing,
                    "position": player.playbackTime,
                    "title": (entry.item as? Song)?.title ?? "",
                    "artist": (entry.item as? Song)?.artistName ?? "",
                    // ... etc
                ]
                emit(state)
            }
        }

        // 3. Read commands from stdin
        while let line = readLine() {
            guard let data = line.data(using: .utf8),
                  let cmd = try? JSONSerialization.jsonObject(with: data) as? [String: Any],
                  let action = cmd["cmd"] as? String
            else { continue }

            switch action {
            case "playlists":
                // MusicLibraryRequest<Playlist>
            case "tracks":
                // Fetch tracks for a playlist
            case "search":
                // MusicCatalogSearchRequest
            case "play":
                // Set queue and play
            case "pause":
                player.pause()
            case "resume":
                try? await player.play()
            case "next":
                try? await player.skipToNextEntry()
            case "previous":
                try? await player.skipToPreviousEntry()
            case "seek":
                if let secs = cmd["seconds"] as? Double {
                    player.playbackTime = secs
                }
            // ... etc
            default:
                emit(["type": "error", "message": "Unknown command: \(action)"])
            }
        }
    }

    static func emit(_ dict: [String: Any]) {
        guard let data = try? JSONSerialization.data(withJSONObject: dict),
              let str = String(data: data, encoding: .utf8)
        else { return }
        print(str)
        fflush(stdout)
    }
}
```

---

## Build & Distribution

### Building the helper

```bash
cd tools/cliamp-apple-music
swift build -c release
# Output: .build/release/cliamp-apple-music
```

The binary gets:
- Info.plist embedded via `-sectcreate __TEXT __info_plist` linker flags (in Package.swift)
- Signed ad-hoc (`codesign -s -`) for MusicKit entitlement

### GoReleaser integration

For macOS builds, the release pipeline:
1. Builds the Swift helper for both `arm64` and `x86_64`
2. Bundles it alongside the cliamp binary
3. cliamp looks for `cliamp-apple-music` in:
   - Same directory as the cliamp binary
   - `~/.config/cliamp/bin/`
   - `$PATH`

For Linux/Windows builds: helper is not built/bundled. The Apple Music provider returns `nil` from `New()`.

### Homebrew

```ruby
# In the cask/formula:
# The helper binary is included in the release archive
# No additional setup needed on macOS 14+
```

---

## Config

```toml
[applemusic]
enabled = true
# No API keys needed — MusicKit handles auth via system dialog
# User must grant Media & Apple Music permission in System Settings
```

---

## User Experience

### First Run
1. User starts cliamp with `[applemusic] enabled = true`
2. cliamp spawns the helper binary
3. macOS shows "cliamp-apple-music wants to access Apple Music" permission dialog
4. User clicks Allow
5. Apple Music playlists appear in the provider browser alongside Spotify/Navidrome/Local

### Playback
- Apple Music tracks play through the system audio output
- cliamp shows now-playing info, progress bar, album art (via artwork URL)
- Play/pause, next/previous, seek all work via helper commands
- EQ and spectrum visualizer are disabled for Apple Music tracks (show "System Audio" badge)
- When switching to a local/Spotify track, normal Beep pipeline resumes

### Search
- Press `N` (net search) → select "Apple Music" → search the full catalog
- Results can be queued, played, or added to a playlist

---

## Implementation Plan

| Phase | What | Effort |
|-------|------|--------|
| **1** | Swift helper binary (auth, playlists, tracks, play/pause/next/prev/seek, state events) | 2-3 days |
| **2** | Go provider (spawn helper, IPC, playlist.Provider impl, remote playback mode) | 2-3 days |
| **3** | TUI integration (remote playback display, disable EQ for AM tracks, search) | 1-2 days |
| **4** | Build/release pipeline (GoReleaser, Homebrew, README) | 1 day |
| **5** | Search, recommendations, stations, queue management | 1-2 days |

Total: ~7-11 days

### Phase 1 — Start Here

Build and test the Swift helper standalone:
```bash
cd tools/cliamp-apple-music
swift build
echo '{"cmd":"auth"}' | .build/debug/cliamp-apple-music
echo '{"cmd":"playlists"}' | .build/debug/cliamp-apple-music
echo '{"cmd":"search","query":"wires athlete","type":"songs","limit":5}' | .build/debug/cliamp-apple-music
```

Once the helper works in isolation, wire it into cliamp's provider system.

---

## Limitations

- **macOS 14+ only** — MusicKit Swift framework requirement
- **No lossless** — ApplicationMusicPlayer plays AAC 256kbps (same limitation as MusicKit JS / Cider)
- **No EQ/visualizer** — Audio goes through system audio, not Beep pipeline
- **No crossfade** — MusicKit doesn't expose audio buffers for mixing
- **Requires Apple Music subscription** — Free tier only gets previews
- **System audio output** — Cannot select audio output device from cliamp (uses system default)

## Future Possibilities

- **Audio Tap (macOS 15+)**: `ScreenCaptureKit` or Core Audio process taps could capture ApplicationMusicPlayer's output PCM for visualization. Experimental, may violate DRM restrictions.
- **Hybrid mode**: For tracks that exist both in Apple Music and as local files, prefer local playback (full pipeline) and fall back to Apple Music streaming.
- **Linux/Windows**: Not possible without MusicKit. Could explore MusicKit JS in headless Chromium as a future avenue, but the complexity is not worth it.
