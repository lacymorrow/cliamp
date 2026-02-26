# Gapless Playback

## Problem

Track transitions in audio players typically have an audible gap. This happens because the naive approach tears down the entire audio pipeline (decode → resample → EQ → volume → speaker) and rebuilds it for each new track, calling `speaker.Clear()` in between. The speaker thread stops, buffers are flushed, and the new pipeline takes time to spin up — producing a brief silence.

## Solution

The speaker runs continuously from the first track until the player is closed. A single long-lived audio pipeline is built once, and track transitions happen at the sample level inside a custom `gaplessStreamer` at the bottom of the chain.

## Architecture

### Pipeline layout

```
[gaplessStreamer] → [10× Biquad EQ] → [Volume/Mono] → [Tap] → [Ctrl] → speaker
       ↑
       ├─ current: [Decode A] → [Resample A]
       └─ next:    [Decode B] → [Resample B]  (preloaded)
```

- The **EQ → volume → tap → ctrl** chain is built on the first `Play()` call and reused for all subsequent tracks.
- `speaker.Play(ctrl)` is called exactly once. The speaker never stops between tracks.
- The `gaplessStreamer` is the only component that changes between tracks. It holds a reference to the current and (optionally) next decoded+resampled audio stream.

### Key types

**`gaplessStreamer`** (`player/gapless.go`) — A `beep.Streamer` that sequences tracks:

- `Stream(samples)` reads from the current track. When it exhausts, remaining samples are filled from the preloaded next track in the same call — zero gap. If no next track exists, it fills silence and sets `drained = true`. Always returns `(len(samples), true)` so the speaker never stops.
- `SetNext(s)` preloads the next track's stream.
- `Replace(s)` interrupts the current track immediately (for manual skip/select).
- `Clear()` removes all tracks (for stop). The speaker continues running, outputting silence.

**`trackPipeline`** (`player/player.go`) — Bundles a decoded track's resources (decoder, resampled stream, format, file handle). The `Player` keeps two of these: `current` (active) and `nextPipeline` (preloaded).

## How transitions work

### Gapless (automatic advance)

1. While track A plays, the UI calls `player.Preload(nextPath)` which decodes track B and calls `gapless.SetNext(B)`.
2. When track A's decoder returns 0 samples, `gaplessStreamer.Stream()` detects this and fills the remaining buffer from track B — no silence inserted.
3. The `onSwap` callback fires (in a goroutine), which swaps `current ← nextPipeline` on the Player, closes track A's resources, and sets the `gaplessAdvance` flag.
4. On the next UI tick, `player.GaplessAdvanced()` returns true (once). The UI advances the playlist cursor and preloads the next-next track.

### Manual skip (>, <, Enter)

Calls `player.Play(path)` which calls `gapless.Replace(newStream)`. The current track is interrupted immediately and the new track starts. Any preloaded next is discarded and a new preload is triggered.

### Stop (s)

Calls `player.Stop()` which calls `gapless.Clear()` and pauses the ctrl. The speaker keeps running but outputs silence. A subsequent `Play()` unpauses and replaces the source.

### Close (q)

Calls `player.Close()` → `Stop()` + `speaker.Clear()`. This is the only time the speaker thread is actually terminated.

## Preloading

The UI calls `preloadNext()` after every action that changes which track will play next:

- `playCurrentTrack()` — initial play
- `nextTrack()` — manual or fallback advance
- `prevTrack()` — go back
- Gapless transition handler
- Shuffle toggle (`z`) — invalidates then reloads
- Repeat toggle (`r`) — invalidates then reloads

`playlist.PeekNext()` determines which track comes next without advancing the playlist position. It returns `false` when the next track can't be predicted (e.g., shuffle+RepeatAll at end of list, which triggers a re-randomization). In that case, no preload happens and the transition falls back to non-gapless.

## Fallback behavior

Gapless is best-effort. The system falls back gracefully:

| Scenario | What happens |
|----------|-------------|
| Preload fails (bad file, network error) | Error silently ignored. Track drains, tick handler does non-gapless `nextTrack()`. |
| Shuffle + RepeatAll wrap | `PeekNext()` returns false (can't predict post-shuffle order). One non-gapless transition per shuffle cycle. |
| Very short track (<100ms) | Preload may not complete in time. Drains to fallback. |
| End of playlist (RepeatOff) | No next track. Gapless drains. Tick handler calls `nextTrack()` which calls `Stop()`. |
| HTTP streams | Works normally. Preload opens a new HTTP connection for the next track. |

## Thread safety

- `gaplessStreamer` uses its own `sync.Mutex` for the hot audio path (~440 calls/sec at 44.1kHz).
- The `onSwap` callback runs in a goroutine to avoid holding the gapless lock while acquiring `Player.mu`.
- `speaker.Lock()` is only used for seek/position queries, same as before.
- The `gaplessAdvance` flag uses `atomic.Bool` with `CompareAndSwap` for lock-free single-consumer signaling between the audio thread and UI tick.
