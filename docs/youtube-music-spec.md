# YouTube Music Provider — Implementation Spec

## Overview

Add a YouTube Music library browser provider so the user's YT Music playlists
appear in the `N` provider menu alongside Spotify, Navidrome, and Radio.

**Key insight:** cliamp already plays YouTube Music URLs via yt-dlp. The only
missing piece is a *library browser* — metadata/playlist enumeration. Audio
playback requires zero changes.

## Architecture

```
┌──────────────────────────────────────────────────────────────────┐
│                        main.go                                   │
│  ytmusicProv := ytmusic.New(nil, cfg.YouTubeMusic.ClientID)     │
│  providers = append(providers, ProviderEntry{                    │
│      Key: "ytmusic", Name: "YouTube Music", Provider: ytmusicProv│
│  })                                                              │
└──────────┬───────────────────────────────────────────────────────┘
           │
           ▼
┌──────────────────────────────────────────────────────────────────┐
│               external/ytmusic/provider.go                       │
│                                                                  │
│  Implements playlist.Provider + playlist.Authenticator            │
│  • Name() → "YouTube Music"                                      │
│  • Playlists() → YouTube Data API: playlists.list?mine=true      │
│  • Tracks(id) → YouTube Data API: playlistItems.list             │
│  • Authenticate() → OAuth2 Desktop flow (browser popup)          │
│                                                                  │
│  Tracks returned with Path: "https://music.youtube.com/watch?v=" │
│  → existing yt-dlp pipe chain handles playback                   │
└──────────┬───────────────────────────────────────────────────────┘
           │
           ▼
┌──────────────────────────────────────────────────────────────────┐
│               external/ytmusic/session.go                        │
│                                                                  │
│  OAuth2 Desktop flow (mirrors Spotify session.go):               │
│  • localhost callback server on port 19873                       │
│  • PKCE code challenge                                           │
│  • Token caching at ~/.config/cliamp/ytmusic_credentials.json    │
│  • Silent refresh via refresh token on subsequent launches       │
│  • Reconnect() for expired sessions                              │
│                                                                  │
│  Uses golang.org/x/oauth2 + google.golang.org/api/youtube/v3    │
└──────────────────────────────────────────────────────────────────┘
```

## Files to Create

### 1. `external/ytmusic/session.go`

OAuth2 session management — mirrors `external/spotify/session.go`.

```go
package ytmusic

// Key differences from Spotify session:
// - No go-librespot / audio streaming (yt-dlp handles that)
// - Google OAuth2 endpoint instead of Spotify
// - Scopes: youtube.readonly
// - Callback port: 19873 (Spotify uses 19872)
// - Credentials file: ytmusic_credentials.json
```

**OAuth2 Config:**
- Endpoint: `google.golang.org/x/oauth2/google` → `google.Endpoint`
- Scopes: `"https://www.googleapis.com/auth/youtube.readonly"`
- Redirect URI: `http://127.0.0.1:19873/callback`
- PKCE: Yes (S256 challenge, same as Spotify)

**Stored Credentials:**
```go
type storedCreds struct {
    RefreshToken string `json:"refresh_token"`
}
```

Much simpler than Spotify — no username/device ID/stored data needed.
Just the refresh token for silent re-auth.

**Session struct:**
```go
type Session struct {
    mu          sync.Mutex
    clientID    string
    service     *youtube.Service  // google.golang.org/api/youtube/v3
    tokenSource oauth2.TokenSource
}
```

**Key functions:**
- `NewSession(ctx, clientID)` → load stored creds, try silent refresh, fall back to interactive
- `NewSessionSilent(ctx, clientID)` → stored creds only, return error if interactive needed
- `doOAuth(clientID)` → start localhost server, open browser, exchange code, return token
- `loadCreds() / saveCreds()` → `~/.config/cliamp/ytmusic_credentials.json`

**Auth flow (mirrors Spotify exactly):**
1. Start HTTP server on `127.0.0.1:19873`
2. Build auth URL with PKCE verifier
3. `openBrowser(authURL)` (reuse existing helper)
4. Wait for callback with auth code
5. Exchange code for token
6. Save refresh token to disk
7. Serve "✅ Authenticated! You can close this tab." HTML (same as Spotify)
8. Create `youtube.Service` with the token

### 2. `external/ytmusic/provider.go`

Playlist provider — mirrors `external/spotify/provider.go`.

```go
package ytmusic

type YouTubeMusicProvider struct {
    session    *Session
    clientID   string
    mu         sync.Mutex
    trackCache map[string][]playlist.Track // playlist ID → cached tracks
}
```

**`Playlists() ([]playlist.PlaylistInfo, error)`**

Uses YouTube Data API `playlists.list`:
```go
call := session.service.Playlists.List([]string{"snippet", "contentDetails"}).
    Mine(true).
    MaxResults(50)
```

Paginate with `PageToken`. Map to `playlist.PlaylistInfo`:
```go
PlaylistInfo{
    ID:         item.Id,
    Name:       item.Snippet.Title,
    TrackCount: int(item.ContentDetails.ItemCount),
}
```

Also include a synthetic "Liked Music" entry:
```go
PlaylistInfo{
    ID:         "LL",   // YouTube's special "Liked Videos" playlist ID
    Name:       "Liked",
    TrackCount: -1,     // unknown until fetched
}
```

**`Tracks(playlistID string) ([]playlist.Track, error)`**

Uses YouTube Data API `playlistItems.list`:
```go
call := session.service.PlaylistItems.List([]string{"snippet", "contentDetails"}).
    PlaylistId(playlistID).
    MaxResults(50)
```

Paginate with `PageToken`. For duration, batch-fetch via `videos.list`:
```go
call := session.service.Videos.List([]string{"contentDetails", "snippet"}).
    Id(videoIDs...) // up to 50 per request
```

Map to `playlist.Track`:
```go
Track{
    Path:         "https://music.youtube.com/watch?v=" + videoID,
    Title:        item.Snippet.Title,
    Artist:       item.Snippet.VideoOwnerChannelTitle, // or parse from title
    Album:        "",  // not available from YouTube Data API
    Stream:       false,
    DurationSecs: parseDuration(video.ContentDetails.Duration), // ISO 8601
}
```

**`Authenticate() error`** — triggers interactive OAuth2 flow.

**`Name() string`** — returns `"YouTube Music"`.

**Caching:**
- Cache tracks by playlist ID (no snapshot_id equivalent, but fine for session lifetime)
- Clear cache when provider is re-selected or on explicit refresh

### 3. Config additions (`config/config.go`)

```go
type YouTubeMusicConfig struct {
    Disabled bool   // true when user sets enabled = false
    ClientID string // Google Cloud OAuth2 client ID
}

func (y YouTubeMusicConfig) IsSet() bool {
    return !y.Disabled && y.ClientID != ""
}
```

Config file section:
```toml
[ytmusic]
client_id = "your_google_cloud_client_id_here"
```

Parse in `Load()` under `case "ytmusic":` section handler.

### 4. Wiring in `main.go`

```go
import "cliamp/external/ytmusic"

// After Spotify provider block:
var ytmusicProv *ytmusic.YouTubeMusicProvider
if cfg.YouTubeMusic.IsSet() {
    ytmusicProv = ytmusic.New(nil, cfg.YouTubeMusic.ClientID)
    providers = append(providers, ui.ProviderEntry{
        Key: "ytmusic", Name: "YouTube Music", Provider: ytmusicProv,
    })
}

// Update help text:
// --provider <name>  Default provider: radio, navidrome, spotify, ytmusic
```

### 5. Documentation (`docs/youtube-music.md`)

Mirror the structure of `docs/spotify.md`:

1. **Setup** — Creating a Google Cloud project
   - Go to console.cloud.google.com
   - Create project
   - Enable "YouTube Data API v3"
   - Create OAuth 2.0 Client ID (Desktop application type)
   - Add redirect URI: `http://127.0.0.1:19873/callback`
   - Copy Client ID
2. **Configuring cliamp** — Add `[ytmusic]` section to config.toml
3. **Usage** — Select YouTube Music provider, sign in on first use
4. **Playlists** — All playlists in your YT Music library are shown
5. **Troubleshooting** — Common auth issues
6. **Requirements** — yt-dlp (for playback), Google Cloud project

## Dependencies

**New Go module dependencies:**
- `google.golang.org/api/youtube/v3` — YouTube Data API v3 client
- `golang.org/x/oauth2/google` — Google OAuth2 endpoint (likely already transitive)

**No new system dependencies** — yt-dlp is already required for YouTube playback.

## API Quota Budget

YouTube Data API v3 free tier: **10,000 units/day** per Google Cloud project.

| Operation | Cost | Typical Usage |
|---|---|---|
| `playlists.list` (list user playlists) | 1 unit | 1-2 calls per session |
| `playlistItems.list` (50 items/page) | 1 unit | 1-10 calls per playlist |
| `videos.list` (batch duration lookup) | 1 unit | 1-10 calls per playlist |
| `search.list` | 100 units | Not used (yt-dlp search instead) |

**Typical session: 5-30 units.** User could browse playlists 300+ times/day.

## Auth Flow Comparison

| | Spotify | YouTube Music |
|---|---|---|
| Dashboard | developer.spotify.com | console.cloud.google.com |
| Credential | Client ID | Client ID |
| OAuth endpoint | accounts.spotify.com | accounts.google.com |
| Callback port | 19872 | 19873 |
| Scopes | streaming, playlist-read-* | youtube.readonly |
| Token storage | spotify_credentials.json | ytmusic_credentials.json |
| Silent refresh | ✅ refresh token | ✅ refresh token |
| PKCE | ✅ S256 | ✅ S256 |
| Auto-close tab | ✅ | ✅ |

## Track Path Format

Tracks use standard YouTube Music URLs:
```
https://music.youtube.com/watch?v=dQw4w9WgXcQ
```

The player's existing URL detection (`IsURL()` in `playlist/playlist.go`)
recognizes these as HTTP URLs and routes them through `buildYTDLPipeline()`,
which spawns the `yt-dlp | ffmpeg` pipe chain. **Zero audio pipeline changes.**

## Edge Cases

1. **"Liked Videos" playlist** — YouTube's special `LL` playlist ID contains
   liked videos. Filter to music-only by checking `snippet.categoryId == "10"`
   (Music category) on the video details, or accept all liked videos.

2. **Private/unlisted videos** — `playlistItems.list` may return items where
   the video is private or deleted. Skip these (check for empty `videoId` or
   `snippet.title == "Private video"`).

3. **Music vs regular YouTube** — The YouTube Data API doesn't distinguish
   YT Music playlists from regular YouTube playlists. All user playlists show
   up. This is actually fine — if someone has a "Coding Music" YouTube
   playlist, they probably want it in cliamp too.

4. **No album metadata** — YouTube Data API doesn't provide album info for
   music tracks. The `Artist` field uses `snippet.videoOwnerChannelTitle`
   (the uploader's channel name), which for official music is usually the
   artist name. Could be improved later with yt-dlp metadata extraction.

5. **Duration** — `playlistItems.list` doesn't include duration. Requires a
   secondary `videos.list` call with batched video IDs (up to 50 per request).
   Parse ISO 8601 duration (e.g. `PT4M13S` → 253 seconds).

## Implementation Order

1. `external/ytmusic/session.go` — OAuth2 flow + credential storage
2. `config/config.go` — Add `YouTubeMusicConfig` + parsing
3. `external/ytmusic/provider.go` — `Playlists()` + `Tracks()` with API calls
4. `main.go` — Wire up provider
5. `docs/youtube-music.md` — User-facing setup guide
6. Test end-to-end: config → auth → browse playlists → play track
