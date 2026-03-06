# Spotify Integration

Cliamp can stream your [Spotify](https://www.spotify.com/) library directly through its audio pipeline — EQ, visualizer, and all effects apply. Requires a [Spotify Premium](https://www.spotify.com/premium/) account.

## Setup

1. Create a Spotify app at [developer.spotify.com/dashboard](https://developer.spotify.com/dashboard)
2. Add redirect URI: `http://127.0.0.1:19872/login`
3. Add to your config:

```toml
# ~/.config/cliamp/config.toml
[spotify]
enabled = true
client_id = "your_client_id_here"
```

4. Run `cliamp` — the first launch opens OAuth2 in your browser
5. Credentials are cached at `~/.config/cliamp/spotify_credentials.json` — subsequent launches refresh silently (no browser popup)

## Usage

Once authenticated, Spotify appears as a provider alongside Navidrome and local playlists. Press `Esc`/`b` to open the provider browser and select Spotify.

Your Spotify playlists are listed in the provider panel. Navigate with the arrow keys and press `Enter` to load one. Tracks are streamed through cliamp's audio pipeline, so EQ, visualizer, mono, and all other effects work exactly as with local files.

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

Only playlists in your Spotify library are shown — this includes playlists you've created and playlists you've saved (followed). If a public playlist doesn't appear, open Spotify and click **Save** on it first. There's no need to copy tracks to a new playlist.

## Troubleshooting

- **"OAuth failed"** — Make sure your redirect URI is exactly `http://127.0.0.1:19872/login` in the Spotify dashboard (no trailing slash).
- **Playlist not showing** — You must save/follow the playlist in Spotify for it to appear. Only your library playlists are listed.
- **Playback issues** — Spotify integration requires a Premium account. Free accounts cannot stream.
- **Re-authenticate** — Delete `~/.config/cliamp/spotify_credentials.json` and restart cliamp to trigger a fresh login.

## Requirements

- Spotify Premium account
- A registered app at [developer.spotify.com/dashboard](https://developer.spotify.com/dashboard)
- No additional system dependencies beyond cliamp itself
