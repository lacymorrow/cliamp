# Navidrome Integration

Cliamp can connect to a [Navidrome](https://www.navidrome.org/) server and stream music directly from your library. Navidrome is a self-hosted music server compatible with the Subsonic API.

## Setup

Set three environment variables before launching Cliamp:

```sh
export NAVIDROME_URL="http://your-server:4533"
export NAVIDROME_USER="your-username"
export NAVIDROME_PASS="your-password"
```

Then run Cliamp without any file arguments:

```sh
cliamp
```

You can also combine local files with a Navidrome session:

```sh
NAVIDROME_URL=http://localhost:4533 NAVIDROME_USER=admin NAVIDROME_PASS=secret cliamp ~/Music/extra.mp3
```

## How It Works

When the environment variables are set, Cliamp authenticates with your Navidrome server using the Subsonic API. On launch it fetches your playlists and presents them in the TUI.

Browse your playlists with the arrow keys and press Enter to load one. The tracks are added to the local playlist and playback starts immediately. Audio is streamed as MP3 from the server.

## Controls

When focused on the provider panel:

| Key | Action |
|---|---|
| `Up` `Down` / `j` `k` | Navigate playlists |
| `Enter` | Load the selected playlist |
| `Tab` | Switch between provider and playlist focus |

After loading a playlist you return to the standard playlist view with all the usual controls (seek, volume, EQ, shuffle, repeat, queue, search).

## Architecture

The integration is built around a `Provider` interface defined in the `playlist` package:

```go
type Provider interface {
    Name() string
    Playlists() ([]PlaylistInfo, error)
    Tracks(playlistID string) ([]Track, error)
}
```

The Navidrome client (`external/navidrome/client.go`) implements this interface. It builds authenticated Subsonic API requests using MD5 token auth (password + random salt) and parses the JSON responses into playlist and track structs.

Playlist and track fetching runs asynchronously through Bubbletea commands so the UI stays responsive while the server responds.

Adding support for another Subsonic-compatible server (Airsonic, Gonic, etc.) would mean implementing the same `Provider` interface against that server's API.

## Requirements

No additional dependencies are needed beyond a running Navidrome instance. The client uses Go's standard `net/http` and `crypto/md5` packages. Your Navidrome server must have the Subsonic API enabled, which is the default.
