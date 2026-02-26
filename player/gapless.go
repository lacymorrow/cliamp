package player

import (
	"sync"
	"sync/atomic"

	"github.com/gopxl/beep/v2"
)

// gaplessStreamer is a beep.Streamer that sequences tracks with zero-gap
// transitions. It sits at the bottom of the audio pipeline and manages
// track sources while the EQ/volume/tap/ctrl chain above it lives forever.
//
// It always returns (len(samples), true) — it never stops the speaker.
// When no audio is available, it fills silence.
type gaplessStreamer struct {
	mu      sync.Mutex
	current beep.Streamer // active track (decoded + resampled)
	next    beep.Streamer // preloaded next track
	drained atomic.Bool   // true when current exhausts with no next
	onSwap  func()        // called (in goroutine) on gapless transition
}

// Stream reads samples from the current track. On exhaustion, it seamlessly
// fills remaining samples from the next track (like beep.Seq). If no next
// track is available, it sets drained=true and fills silence.
func (g *gaplessStreamer) Stream(samples [][2]float64) (int, bool) {
	g.mu.Lock()
	cur := g.current
	g.mu.Unlock()

	if cur == nil {
		// No active track — fill silence
		clear(samples)
		return len(samples), true
	}

	n, ok := cur.Stream(samples)

	if !ok || n < len(samples) {
		// Current track exhausted — try to seamlessly continue with next
		g.mu.Lock()
		next := g.next
		g.next = nil
		g.current = next
		swapFn := g.onSwap
		g.mu.Unlock()

		if next != nil {
			// Fill remaining buffer from next track — zero gap
			if n < len(samples) {
				filled, _ := next.Stream(samples[n:])
				n += filled
			}
			// Notify about the transition (non-blocking)
			if swapFn != nil {
				go swapFn()
			}
			g.drained.Store(false)
		} else {
			// No next track — we've drained
			g.drained.Store(true)
		}
	}

	// Fill any remaining with silence
	for i := n; i < len(samples); i++ {
		samples[i] = [2]float64{}
	}
	return len(samples), true
}

// Err always returns nil — errors are handled per-track at decode time.
func (g *gaplessStreamer) Err() error { return nil }

// SetNext preloads the next track's resampled streamer for gapless transition.
func (g *gaplessStreamer) SetNext(s beep.Streamer) {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.next = s
}

// Replace interrupts the current track and starts a new one immediately.
// Used for manual skip/prev/select operations.
func (g *gaplessStreamer) Replace(s beep.Streamer) {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.current = s
	g.next = nil
	g.drained.Store(false)
}

// Clear removes both current and next tracks. The streamer will output
// silence until Replace or SetNext is called.
func (g *gaplessStreamer) Clear() {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.current = nil
	g.next = nil
	g.drained.Store(false)
}

// Drained reports whether the current track ended with no next track queued.
func (g *gaplessStreamer) Drained() bool {
	return g.drained.Load()
}
