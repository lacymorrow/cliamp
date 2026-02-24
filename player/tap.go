package player

import (
	"sync"

	"github.com/gopxl/beep/v2"
)

// Tap is a streamer wrapper that copies samples into a ring buffer
// for real-time FFT visualization. It sits in the audio pipeline
// between the volume control and the speaker controller.
type Tap struct {
	s    beep.Streamer
	mu   sync.Mutex
	buf  []float64
	pos  int
	size int
}

// NewTap wraps a streamer with a ring buffer of the given size.
func NewTap(s beep.Streamer, bufSize int) *Tap {
	return &Tap{
		s:    s,
		buf:  make([]float64, bufSize),
		size: bufSize,
	}
}

// Stream passes audio through while capturing a mono mix into the ring buffer.
func (t *Tap) Stream(samples [][2]float64) (int, bool) {
	n, ok := t.s.Stream(samples)
	t.mu.Lock()
	for i := 0; i < n; i++ {
		t.buf[t.pos] = (samples[i][0] + samples[i][1]) / 2
		t.pos = (t.pos + 1) % t.size
	}
	t.mu.Unlock()
	return n, ok
}

// Err returns the underlying streamer's error.
func (t *Tap) Err() error {
	return t.s.Err()
}

// Samples returns the last n samples from the ring buffer in chronological order.
func (t *Tap) Samples(n int) []float64 {
	if n > t.size {
		n = t.size
	}
	out := make([]float64, n)
	t.mu.Lock()
	start := (t.pos - n + t.size) % t.size
	for i := 0; i < n; i++ {
		out[i] = t.buf[(start+i)%t.size]
	}
	t.mu.Unlock()
	return out
}
