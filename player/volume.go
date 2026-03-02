package player

import (
	"math"
	"sync"

	"github.com/gopxl/beep/v2"
)

// volumeStreamer applies dB gain and optional mono downmix to an audio stream.
type volumeStreamer struct {
	s    beep.Streamer
	vol  *float64
	mono *bool
	mu   *sync.Mutex
}

func (v *volumeStreamer) Stream(samples [][2]float64) (int, bool) {
	n, ok := v.s.Stream(samples)
	if n == 0 {
		return 0, ok
	}
	v.mu.Lock()
	gain := math.Pow(10, *v.vol/20)
	mono := *v.mono
	v.mu.Unlock()
	for i := range n {
		samples[i][0] *= gain
		samples[i][1] *= gain
		if mono {
			mid := (samples[i][0] + samples[i][1]) / 2
			samples[i][0] = mid
			samples[i][1] = mid
		}
	}
	return n, ok
}

func (v *volumeStreamer) Err() error { return v.s.Err() }
