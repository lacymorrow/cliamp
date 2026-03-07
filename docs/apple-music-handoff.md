# Apple Music Provider — Handoff

## Status: Phase 1 Complete, Blocked on Apple Developer Portal

Branch: `feat/apple-music-provider`
Commit: `1b85962`

---

## What's Built

### Swift Helper Binary (`tools/cliamp-apple-music/`)
A standalone Swift Package Manager executable that bridges MusicKit to cliamp via JSON-line IPC.

- **Auth**: `MusicAuthorization.request()` — works, macOS shows permission dialog
- **Library**: playlists, library search — works (returns data, but this machine has 0 library playlists and 139 streaming-only tracks)
- **Playback**: `ApplicationMusicPlayer.shared` — play/pause/next/prev/seek/shuffle/repeat/queue
- **State reporting**: 1Hz JSON events with track info, position, duration, play state
- **Catalog search**: playlists, songs, albums from Apple Music catalog
- **Recommendations & recently played**

Build:
```bash
cd tools/cliamp-apple-music
swift build           # debug
swift build -c release  # release (~45s)
```

Test:
```bash
echo '{"cmd":"ping"}' | .build/debug/cliamp-apple-music
# Output: {"status":"authorized","type":"auth"}
# Output: {"type":"pong"}
```

### Go Provider (`external/applemusic/`)

| File | Purpose |
|------|---------|
| `protocol.go` | JSON command/response types, lazy parsing for items |
| `helper.go` | Spawns Swift binary, manages stdin/stdout IPC, routes state events |
| `provider.go` | `playlist.Provider` impl, play/pause/seek, URI scheme `applemusic:track:<id>` |

### Integration (`main.go`, `config/config.go`)

- Config: `[applemusic]` section with `enabled = true`
- Wired into `NewComposite()` alongside Spotify/Navidrome/Local
- Helper binary auto-discovered from: same dir as cliamp, `~/.config/cliamp/bin/`, `tools/.build/`, `$PATH`
- `defer appleMusicProv.Close()` for cleanup

### Spec (`docs/apple-music-spec.md`)
Full research doc covering every open-source AM client, why MusicKit Swift is the only viable approach, IPC protocol reference, and implementation plan.

---

## What's Broken: "Failed to request developer token"

Catalog operations (`search`, `recently_played`, `recommendations`) fail with:
```json
{"type":"error","code":"search_error","message":"Search failed: Failed to request developer token"}
```

**Why:** MusicKit's automatic developer token generation requires the app's bundle identifier to be registered in Apple's developer portal with MusicKit capability. Without this, the framework can't fetch the token needed for Apple Music API calls.

Library-only operations (`playlists`, `library_search`, `auth`, `ping`) work fine without a developer token.

---

## What To Do When You Have Apple Developer Access

### 1. Register the App ID (5 min)

1. Go to [Certificates, Identifiers & Profiles](https://developer.apple.com/account/resources/identifiers/list)
2. Click **+** to register a new identifier
3. Select **App IDs** → **App**
4. Fill in:
   - Description: `cliamp Apple Music Helper`
   - Bundle ID: `com.cliamp.apple-music` (must match `Resources/Info.plist`)
5. Under **Capabilities**, check **MusicKit**
6. Click **Continue** → **Register**

### 2. Test Catalog Search

```bash
cd tools/cliamp-apple-music
swift build
echo '{"cmd":"search","query":"bohemian rhapsody","type":"songs","limit":3}' | .build/debug/cliamp-apple-music
```

If it returns results instead of the developer token error, you're good.

### 3. Test Playback

```bash
# Pipe multiple commands (newline-separated)
printf '{"cmd":"search","query":"bohemian rhapsody","type":"songs","limit":1}\n' | .build/debug/cliamp-apple-music
# Note the track ID from the response, then:
printf '{"cmd":"play","trackId":"<ID_FROM_SEARCH>"}\n{"cmd":"ping"}\n' | .build/debug/cliamp-apple-music
# You should hear music through your speakers
```

### 4. Remaining Work (Phase 2-3)

**Remote playback mode in the TUI:**
- When an `applemusic:track:<id>` URI is selected, skip Beep pipeline
- Send play command to helper instead
- Display now-playing from helper's state events
- Disable EQ controls (show "System Audio" badge)
- Route play/pause/next/prev keypresses to helper

**Search integration:**
- Press `N` (net search) → add "Apple Music" as a source
- Display search results, allow queue/play

**Build pipeline:**
- GoReleaser: build Swift helper for arm64/x86_64, bundle with release
- Homebrew: include helper in the formula/cask

---

## Architecture Reference

```
┌──────────────────┐     stdin (JSON commands)     ┌─────────────────────┐
│                  │ ──────────────────────────────→│                     │
│   cliamp (Go)    │                                │  cliamp-apple-music │
│   TUI + Player   │     stdout (JSON events)       │  (Swift, MusicKit)  │
│                  │ ←──────────────────────────────│                     │
└──────────────────┘                                └─────────────────────┘
                                                           │
                                                    ApplicationMusicPlayer
                                                           │
                                                    macOS System Audio
```

Audio goes through macOS system audio (ApplicationMusicPlayer handles DRM). cliamp's Beep pipeline (decode/resample/EQ/FFT/speaker) is bypassed for Apple Music tracks.

---

## Key Decisions Made

1. **Swift helper over AppleScript**: AppleScript can't play streaming tracks (confirmed: `play` command → `stopped` state for all 139 cloud-only tracks on this machine)
2. **Swift helper over MusicKit JS**: MusicKit JS requires Electron/Chromium + Widevine CDM. Too heavy for CLI.
3. **IPC over CGo**: MusicKit is Swift-only with async/await. No ObjC bridge exists. Subprocess IPC is cleaner than trying to bridge Swift concurrency through CGo.
4. **System audio, no EQ**: `ApplicationMusicPlayer` doesn't expose raw PCM. Audio is DRM-protected and decoded by a system process. This is the same limitation Cider v2 hits.

## Files Changed

```
.gitignore                           # added tools/cliamp-apple-music/.build/
config/config.go                     # AppleMusicConfig struct + parser
main.go                              # Apple Music provider init + composite
external/applemusic/protocol.go      # IPC types
external/applemusic/helper.go        # Swift helper lifecycle
external/applemusic/provider.go      # playlist.Provider impl
tools/cliamp-apple-music/Package.swift
tools/cliamp-apple-music/Resources/Info.plist
tools/cliamp-apple-music/Sources/main.swift
docs/apple-music-spec.md             # full research + spec
docs/apple-music-handoff.md          # this file
```
