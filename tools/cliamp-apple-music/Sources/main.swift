import Foundation
import MusicKit

// MARK: - JSON Helpers

func emit(_ dict: [String: Any]) {
    guard let data = try? JSONSerialization.data(withJSONObject: dict, options: []),
          let str = String(data: data, encoding: .utf8)
    else { return }
    print(str)
    fflush(stdout)
}

func emitError(_ message: String, code: String = "error") {
    emit(["type": "error", "message": message, "code": code])
}

// MARK: - Track Serialization

func trackDict(_ song: Song) -> [String: Any] {
    var d: [String: Any] = [
        "id": song.id.rawValue,
        "title": song.title,
        "artist": song.artistName,
        "duration": Int(song.duration ?? 0),
    ]
    if let album = song.albumTitle { d["album"] = album }
    if let tn = song.trackNumber { d["trackNumber"] = tn }
    if let genre = song.genreNames.first { d["genre"] = genre }
    if let year = song.releaseDate {
        let cal = Calendar.current
        d["year"] = cal.component(.year, from: year)
    }
    if let artwork = song.artwork {
        d["artworkUrl"] = artwork.url(width: 300, height: 300)?.absoluteString ?? ""
    }
    return d
}

func trackDictFromTrack(_ track: Track) -> [String: Any] {
    var d: [String: Any] = [
        "id": track.id.rawValue,
        "title": track.title,
        "artist": track.artistName,
    ]
    if let duration = track.duration { d["duration"] = Int(duration) }
    d["album"] = track.albumTitle ?? ""
    if let tn = track.trackNumber { d["trackNumber"] = tn }
    if let artwork = track.artwork {
        d["artworkUrl"] = artwork.url(width: 300, height: 300)?.absoluteString ?? ""
    }
    return d
}

// MARK: - Player Manager

actor PlayerManager {
    let player = ApplicationMusicPlayer.shared
    private var stateTask: Task<Void, Never>?

    func startStateReporter() {
        stateTask = Task {
            var lastState: String = ""
            while !Task.isCancelled {
                try? await Task.sleep(for: .seconds(1))
                let state = buildStateDict()
                let stateStr = (try? String(data: JSONSerialization.data(withJSONObject: state), encoding: .utf8)) ?? ""
                if stateStr != lastState {
                    emit(state)
                    lastState = stateStr
                }
            }
        }
    }

    func buildStateDict() -> [String: Any] {
        var d: [String: Any] = [
            "type": "state",
            "playing": player.state.playbackStatus == .playing,
            "position": player.playbackTime,
            // Volume is system-level, not exposed by MusicPlayer.State
            "shuffleMode": player.state.shuffleMode == .songs ? "songs" : "off",
        ]

        switch player.state.repeatMode {
        case .one: d["repeatMode"] = "one"
        case .all: d["repeatMode"] = "all"
        default: d["repeatMode"] = "off"
        }

        if let entry = player.queue.currentEntry {
            switch entry.item {
            case .song(let song):
                d["trackId"] = song.id.rawValue
                d["title"] = song.title
                d["artist"] = song.artistName
                d["album"] = song.albumTitle ?? ""
                d["duration"] = song.duration ?? 0
                if let artwork = song.artwork {
                    d["artworkUrl"] = artwork.url(width: 300, height: 300)?.absoluteString ?? ""
                }
            default:
                d["title"] = entry.title
            }
        }

        return d
    }

    // MARK: - Playback Commands

    func play(trackId: String) async {
        do {
            let request = MusicCatalogResourceRequest<Song>(matching: \.id, equalTo: MusicItemID(trackId))
            let response = try await request.response()
            guard let song = response.items.first else {
                emitError("Track not found: \(trackId)", code: "not_found")
                return
            }
            player.queue = [song]
            try await player.prepareToPlay()
            try await player.play()
        } catch {
            emitError("Play failed: \(error.localizedDescription)", code: "play_error")
        }
    }

    func playPlaylist(playlistId: String, startIndex: Int) async {
        do {
            let request = MusicCatalogResourceRequest<Playlist>(matching: \.id, equalTo: MusicItemID(playlistId))
            let response = try await request.response()
            guard var playlist = response.items.first else {
                // Try library playlist
                await playLibraryPlaylist(playlistId: playlistId, startIndex: startIndex)
                return
            }
            playlist = try await playlist.with([.tracks])
            guard let tracks = playlist.tracks else {
                emitError("Playlist has no tracks", code: "empty_playlist")
                return
            }
            player.queue = ApplicationMusicPlayer.Queue(for: tracks)
            try await player.prepareToPlay()
            if startIndex > 0 && startIndex < tracks.count {
                // Skip to the desired start index
                for _ in 0..<startIndex {
                    try await player.skipToNextEntry()
                }
            }
            try await player.play()
        } catch {
            emitError("Play playlist failed: \(error.localizedDescription)", code: "play_error")
        }
    }

    private func playLibraryPlaylist(playlistId: String, startIndex: Int) async {
        do {
            var request = MusicLibraryRequest<Playlist>()
            request.filter(matching: \.id, equalTo: MusicItemID(playlistId))
            let response = try await request.response()
            guard var playlist = response.items.first else {
                emitError("Playlist not found: \(playlistId)", code: "not_found")
                return
            }
            playlist = try await playlist.with([.tracks])
            guard let tracks = playlist.tracks else {
                emitError("Playlist has no tracks", code: "empty_playlist")
                return
            }
            player.queue = ApplicationMusicPlayer.Queue(for: tracks)
            try await player.prepareToPlay()
            if startIndex > 0 && startIndex < tracks.count {
                for _ in 0..<startIndex {
                    try await player.skipToNextEntry()
                }
            }
            try await player.play()
        } catch {
            emitError("Play library playlist failed: \(error.localizedDescription)", code: "play_error")
        }
    }

    func queueAdd(trackId: String, position: String) async {
        do {
            let request = MusicCatalogResourceRequest<Song>(matching: \.id, equalTo: MusicItemID(trackId))
            let response = try await request.response()
            guard let song = response.items.first else {
                emitError("Track not found: \(trackId)", code: "not_found")
                return
            }
            let insertPosition: ApplicationMusicPlayer.Queue.EntryInsertionPosition =
                position == "tail" ? .tail : .afterCurrentEntry
            if player.queue.entries.isEmpty {
                player.queue = [song]
                try await player.prepareToPlay()
            } else {
                try await player.queue.insert(MusicItemCollection([song]), position: insertPosition)
            }
            emit(["type": "queue_added", "trackId": trackId])
        } catch {
            emitError("Queue add failed: \(error.localizedDescription)", code: "queue_error")
        }
    }

    func pause() {
        player.pause()
    }

    func resume() async {
        do {
            try await player.play()
        } catch {
            emitError("Resume failed: \(error.localizedDescription)", code: "play_error")
        }
    }

    func next() async {
        do {
            try await player.skipToNextEntry()
        } catch {
            emitError("Next failed: \(error.localizedDescription)", code: "skip_error")
        }
    }

    func previous() async {
        do {
            try await player.skipToPreviousEntry()
        } catch {
            emitError("Previous failed: \(error.localizedDescription)", code: "skip_error")
        }
    }

    func stop() {
        player.stop()
    }

    func seek(seconds: Double) {
        player.playbackTime = seconds
    }

    func seekRelative(delta: Double) {
        player.playbackTime = max(0, player.playbackTime + delta)
    }

    func setVolume(_ volume: Float) {
        // ApplicationMusicPlayer doesn't expose volume control directly;
        // volume is system-level. Report this limitation.
        emitError("Volume control not available for Apple Music (system volume only)", code: "unsupported")
    }

    func setShuffle(enabled: Bool) {
        player.state.shuffleMode = enabled ? .songs : .off
        emit(["type": "shuffle_changed", "enabled": enabled])
    }

    func setRepeat(mode: String) {
        switch mode {
        case "one": player.state.repeatMode = .one
        case "all": player.state.repeatMode = .all
        default: player.state.repeatMode = MusicPlayer.RepeatMode.none
        }
        emit(["type": "repeat_changed", "mode": mode])
    }

    func getQueue() {
        var entries: [[String: Any]] = []
        for entry in player.queue.entries {
            var d: [String: Any] = [:]
            switch entry.item {
            case .song(let song):
                d = trackDict(song)
            default:
                d["title"] = entry.title
            }
            entries.append(d)
        }
        emit(["type": "queue", "entries": entries])
    }
}

// MARK: - Library & Search Commands

func fetchPlaylists() async {
    do {
        var request = MusicLibraryRequest<Playlist>()
        request.limit = 100
        let response = try await request.response()
        var items: [[String: Any]] = []
        for playlist in response.items {
            var d: [String: Any] = [
                "id": playlist.id.rawValue,
                "name": playlist.name,
            ]
            // Track count requires fetching with .tracks, skip for list performance
            if let lastModified = playlist.lastModifiedDate {
                d["lastModified"] = ISO8601DateFormatter().string(from: lastModified)
            }
            items.append(d)
        }
        emit(["type": "playlists", "items": items])
    } catch {
        emitError("Fetch playlists failed: \(error.localizedDescription)", code: "fetch_error")
    }
}

func fetchTracks(playlistId: String) async {
    do {
        // Try library playlist first
        var request = MusicLibraryRequest<Playlist>()
        request.filter(matching: \.id, equalTo: MusicItemID(playlistId))
        let response = try await request.response()

        if var playlist = response.items.first {
            playlist = try await playlist.with([.tracks])
            let tracks = playlist.tracks ?? []
            let items = tracks.map { trackDictFromTrack($0) }
            emit(["type": "tracks", "playlistId": playlistId, "items": items])
            return
        }

        // Try catalog playlist
        let catRequest = MusicCatalogResourceRequest<Playlist>(matching: \.id, equalTo: MusicItemID(playlistId))
        let catResponse = try await catRequest.response()
        if var playlist = catResponse.items.first {
            playlist = try await playlist.with([.tracks])
            let tracks = playlist.tracks ?? []
            let items = tracks.map { trackDictFromTrack($0) }
            emit(["type": "tracks", "playlistId": playlistId, "items": items])
            return
        }

        emitError("Playlist not found: \(playlistId)", code: "not_found")
    } catch {
        emitError("Fetch tracks failed: \(error.localizedDescription)", code: "fetch_error")
    }
}

func searchCatalog(query: String, type: String, limit: Int) async {
    do {
        switch type {
        case "songs":
            var request = MusicCatalogSearchRequest(term: query, types: [Song.self])
            request.limit = limit
            let response = try await request.response()
            let items = response.songs.map { trackDict($0) }
            emit(["type": "search", "query": query, "searchType": "songs", "items": items])

        case "albums":
            var request = MusicCatalogSearchRequest(term: query, types: [Album.self])
            request.limit = limit
            let response = try await request.response()
            var items: [[String: Any]] = []
            for album in response.albums {
                items.append([
                    "id": album.id.rawValue,
                    "title": album.title,
                    "artist": album.artistName,
                    "trackCount": album.trackCount,
                ])
            }
            emit(["type": "search", "query": query, "searchType": "albums", "items": items])

        case "playlists":
            var request = MusicCatalogSearchRequest(term: query, types: [Playlist.self])
            request.limit = limit
            let response = try await request.response()
            var items: [[String: Any]] = []
            for playlist in response.playlists {
                items.append([
                    "id": playlist.id.rawValue,
                    "name": playlist.name,
                    "curator": playlist.curatorName ?? "",
                ])
            }
            emit(["type": "search", "query": query, "searchType": "playlists", "items": items])

        default:
            emitError("Unknown search type: \(type)", code: "invalid_type")
        }
    } catch {
        emitError("Search failed: \(error.localizedDescription)", code: "search_error")
    }
}

func searchLibrary(query: String, type: String, limit: Int) async {
    do {
        switch type {
        case "songs":
            var request = MusicLibraryRequest<Song>()
            request.filter(text: query)
            request.limit = limit
            let response = try await request.response()
            let items = response.items.map { trackDict($0) }
            emit(["type": "library_search", "query": query, "searchType": "songs", "items": items])

        case "albums":
            var request = MusicLibraryRequest<Album>()
            request.filter(text: query)
            request.limit = limit
            let response = try await request.response()
            var items: [[String: Any]] = []
            for album in response.items {
                items.append([
                    "id": album.id.rawValue,
                    "title": album.title,
                    "artist": album.artistName,
                    "trackCount": album.trackCount,
                ])
            }
            emit(["type": "library_search", "query": query, "searchType": "albums", "items": items])

        default:
            emitError("Unknown search type: \(type)", code: "invalid_type")
        }
    } catch {
        emitError("Library search failed: \(error.localizedDescription)", code: "search_error")
    }
}

func fetchRecentlyPlayed(limit: Int) async {
    do {
        var request = MusicRecentlyPlayedRequest<Song>()
        request.limit = limit
        let response = try await request.response()
        let items = response.items.map { trackDict($0) }
        emit(["type": "recently_played", "items": items])
    } catch {
        emitError("Recently played failed: \(error.localizedDescription)", code: "fetch_error")
    }
}

func fetchRecommendations(limit: Int) async {
    do {
        var request = MusicPersonalRecommendationsRequest()
        request.limit = limit
        let response = try await request.response()
        var items: [[String: Any]] = []
        for rec in response.recommendations {
            var d: [String: Any] = [
                "title": rec.title ?? "Recommendation",
            ]
            // Extract playlists from recommendation
            let playlists = rec.playlists
            if !playlists.isEmpty {
                d["playlists"] = playlists.map { pl in
                    ["id": pl.id.rawValue, "name": pl.name] as [String: Any]
                }
            }
            let albums = rec.albums
            if !albums.isEmpty {
                d["albums"] = albums.map { al in
                    ["id": al.id.rawValue, "title": al.title, "artist": al.artistName] as [String: Any]
                }
            }
            items.append(d)
        }
        emit(["type": "recommendations", "items": items])
    } catch {
        emitError("Recommendations failed: \(error.localizedDescription)", code: "fetch_error")
    }
}

// MARK: - Main

@main
struct AppleMusicHelper {
    static func main() async {
        // Authorize
        let status = await MusicAuthorization.request()
        guard status == .authorized else {
            emitError("Apple Music not authorized. Status: \(status)", code: "auth_required")
            // Don't exit — let the user see the error and retry
            return
        }
        emit(["type": "auth", "status": "authorized"])

        let manager = PlayerManager()
        await manager.startStateReporter()

        // Read commands from stdin
        while let line = readLine() {
            let trimmed = line.trimmingCharacters(in: .whitespacesAndNewlines)
            guard !trimmed.isEmpty else { continue }
            guard let data = trimmed.data(using: .utf8),
                  let cmd = try? JSONSerialization.jsonObject(with: data) as? [String: Any],
                  let action = cmd["cmd"] as? String
            else {
                emitError("Invalid JSON command", code: "parse_error")
                continue
            }

            switch action {
            case "playlists":
                await fetchPlaylists()

            case "tracks":
                guard let playlistId = cmd["playlistId"] as? String else {
                    emitError("Missing playlistId", code: "missing_param")
                    continue
                }
                await fetchTracks(playlistId: playlistId)

            case "search":
                let query = cmd["query"] as? String ?? ""
                let type = cmd["type"] as? String ?? "songs"
                let limit = cmd["limit"] as? Int ?? 25
                await searchCatalog(query: query, type: type, limit: limit)

            case "library_search":
                let query = cmd["query"] as? String ?? ""
                let type = cmd["type"] as? String ?? "songs"
                let limit = cmd["limit"] as? Int ?? 25
                await searchLibrary(query: query, type: type, limit: limit)

            case "play":
                guard let trackId = cmd["trackId"] as? String else {
                    emitError("Missing trackId", code: "missing_param")
                    continue
                }
                await manager.play(trackId: trackId)

            case "play_playlist":
                guard let playlistId = cmd["playlistId"] as? String else {
                    emitError("Missing playlistId", code: "missing_param")
                    continue
                }
                let startIndex = cmd["startIndex"] as? Int ?? 0
                await manager.playPlaylist(playlistId: playlistId, startIndex: startIndex)

            case "queue_add":
                guard let trackId = cmd["trackId"] as? String else {
                    emitError("Missing trackId", code: "missing_param")
                    continue
                }
                let position = cmd["position"] as? String ?? "next"
                await manager.queueAdd(trackId: trackId, position: position)

            case "pause":
                await manager.pause()

            case "resume":
                await manager.resume()

            case "next":
                await manager.next()

            case "previous":
                await manager.previous()

            case "stop":
                await manager.stop()

            case "seek":
                guard let seconds = cmd["seconds"] as? Double else {
                    emitError("Missing seconds", code: "missing_param")
                    continue
                }
                await manager.seek(seconds: seconds)

            case "seek_relative":
                guard let delta = cmd["delta"] as? Double else {
                    emitError("Missing delta", code: "missing_param")
                    continue
                }
                await manager.seekRelative(delta: delta)

            case "set_volume":
                guard let volume = cmd["volume"] as? Double else {
                    emitError("Missing volume", code: "missing_param")
                    continue
                }
                await manager.setVolume(Float(volume))

            case "set_shuffle":
                let enabled = cmd["enabled"] as? Bool ?? false
                await manager.setShuffle(enabled: enabled)

            case "set_repeat":
                let mode = cmd["mode"] as? String ?? "off"
                await manager.setRepeat(mode: mode)

            case "queue":
                await manager.getQueue()

            case "now_playing":
                // Force emit current state immediately
                emit(await manager.buildStateDict())

            case "recently_played":
                let limit = cmd["limit"] as? Int ?? 25
                await fetchRecentlyPlayed(limit: limit)

            case "recommendations":
                let limit = cmd["limit"] as? Int ?? 10
                await fetchRecommendations(limit: limit)

            case "ping":
                emit(["type": "pong"])

            default:
                emitError("Unknown command: \(action)", code: "unknown_command")
            }
        }
    }
}
