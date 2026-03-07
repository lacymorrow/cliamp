# YouTube Music Integration

Cliamp can browse your [YouTube Music](https://music.youtube.com/) playlists and play tracks through its audio pipeline — EQ, visualizer, and all effects apply. Playback uses yt-dlp, which must be installed.

## Setup

### Creating your client ID

1. Go to [console.cloud.google.com](https://console.cloud.google.com/) and log in
2. Create a new project (or select an existing one)
3. Navigate to **APIs & Services > Library**
4. Search for **YouTube Data API v3** and click **Enable**
5. Go to **APIs & Services > Credentials**
6. Click **Create Credentials > OAuth client ID**
7. If prompted, configure the OAuth consent screen first:
   - User Type: **External**
   - Fill in app name (e.g. "cliamp") and your email
   - Add scope: `https://www.googleapis.com/auth/youtube.readonly`
   - Add yourself as a test user (required while app is in "Testing" status)
8. For the OAuth client ID:
   - Application type: **Desktop app**
   - Name: anything (e.g. "cliamp")
9. Copy the **Client ID** and **Client Secret**

### Configuring cliamp

Add your client ID and client secret to `~/.config/cliamp/config.toml`:

```toml
[ytmusic]
client_id = "your_client_id_here"
client_secret = "your_client_secret_here"
```

Run `cliamp`, select YouTube Music as a provider, and press Enter to sign in. Credentials are cached at `~/.config/cliamp/ytmusic_credentials.json` — subsequent launches refresh silently.

## Usage

Once authenticated, YouTube Music appears as a provider alongside Spotify, Navidrome, and Radio. Press `Esc`/`b` to open the provider browser and select YouTube Music.

Your playlists are listed in the provider panel, with "Liked Music" at the top. Navigate with the arrow keys and press `Enter` to load one. Tracks are played through cliamp's yt-dlp pipeline, so EQ, visualizer, mono, and all other effects work exactly as with local files.

## Controls

When focused on the provider panel:

| Key | Action |
|---|---|
| `Up` `Down` / `j` `k` | Navigate playlists |
| `Enter` | Load the selected playlist |
| `Tab` | Switch between provider and playlist focus |
| `Esc` / `b` | Open provider browser |

After loading a playlist you return to the standard playlist view with all the usual controls (seek, volume, EQ, shuffle, repeat, queue, search, lyrics).

## Playlists

All playlists in your YouTube Music library are shown, including:
- **Liked Music** — your liked videos (YouTube's special `LL` playlist)
- Playlists you've created
- Playlists you've saved

YouTube doesn't distinguish between YouTube Music playlists and regular YouTube playlists — all appear in the list. This is intentional: if you have a "Coding Music" YouTube playlist, you probably want it in cliamp too.

## Troubleshooting

- **"OAuth failed"** — Make sure your Google Cloud project has YouTube Data API v3 enabled and your OAuth client type is "Desktop app".
- **"Access blocked"** — While your app is in "Testing" status, only test users you've added can sign in. Add your Google account as a test user in the OAuth consent screen settings.
- **Playlist not showing** — Only playlists in your library are listed. Save/follow a playlist in YouTube Music for it to appear.
- **Re-authenticate** — Delete `~/.config/cliamp/ytmusic_credentials.json` and restart cliamp to trigger a fresh login.
- **Private/deleted videos** — These are automatically skipped when loading a playlist.

## Requirements

- [yt-dlp](https://github.com/yt-dlp/yt-dlp) installed and on your PATH (for audio playback)
- A Google Cloud project with YouTube Data API v3 enabled
- No Spotify Premium or other paid subscription required — YouTube Music free tier works
