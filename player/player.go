package player

import (
	"fmt"
	"math"
	"os"
	"sync"
	"sync/atomic"
	"time"

	"github.com/gopxl/beep/v2"
	"github.com/gopxl/beep/v2/mp3"
	"github.com/gopxl/beep/v2/speaker"
)

// EQFreqs are the center frequencies for the 10-band parametric equalizer.
var EQFreqs = [10]float64{70, 180, 320, 600, 1000, 3000, 6000, 12000, 14000, 16000}

// Player is the audio engine managing the playback pipeline:
// [MP3 Decode] -> [Resample] -> [10x Biquad EQ] -> [Volume] -> [Tap] -> [Ctrl] -> [Speaker]
type Player struct {
	mu        sync.Mutex
	sr        beep.SampleRate
	streamer  beep.StreamSeekCloser
	format    beep.Format
	ctrl      *beep.Ctrl
	volume    float64 // dB, range [-30, +6]
	eqBands   [10]float64
	tap       *Tap
	trackDone atomic.Bool
	playing   bool
	paused    bool
	file      *os.File
}

// New creates a Player and initializes the speaker at the given sample rate.
func New(sr beep.SampleRate) *Player {
	speaker.Init(sr, sr.N(time.Second/10))
	return &Player{sr: sr}
}

// Play opens and starts playing an MP3 file, building the full audio pipeline.
func (p *Player) Play(path string) error {
	p.Stop()

	f, err := os.Open(path)
	if err != nil {
		return fmt.Errorf("open: %w", err)
	}

	streamer, format, err := mp3.Decode(f)
	if err != nil {
		f.Close()
		return fmt.Errorf("decode: %w", err)
	}

	p.mu.Lock()
	p.file = f
	p.streamer = streamer
	p.format = format
	p.trackDone.Store(false)

	var s beep.Streamer = streamer

	// Resample to target sample rate if needed
	if format.SampleRate != p.sr {
		s = beep.Resample(4, format.SampleRate, p.sr, s)
	}

	// Chain 10 biquad peaking EQ filters; each reads its gain from p.eqBands[i]
	for i := 0; i < 10; i++ {
		s = newBiquad(s, EQFreqs[i], 1.4, &p.eqBands[i], float64(p.sr))
	}

	// Volume control
	s = &volumeStreamer{s: s, vol: &p.volume, mu: &p.mu}

	// Tap for FFT visualization
	p.tap = NewTap(s, 4096)

	// Pause/resume control
	p.ctrl = &beep.Ctrl{Streamer: p.tap}

	p.playing = true
	p.paused = false
	p.mu.Unlock()

	// Play with end-of-track callback
	speaker.Play(beep.Seq(p.ctrl, beep.Callback(func() {
		p.trackDone.Store(true)
	})))

	return nil
}

// TogglePause toggles between paused and playing states.
func (p *Player) TogglePause() {
	speaker.Lock()
	defer speaker.Unlock()
	if p.ctrl != nil {
		p.ctrl.Paused = !p.ctrl.Paused
		p.paused = p.ctrl.Paused
	}
}

// Stop halts playback and releases resources.
func (p *Player) Stop() {
	speaker.Clear()
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.streamer != nil {
		p.streamer.Close()
		p.streamer = nil
	}
	if p.file != nil {
		p.file.Close()
		p.file = nil
	}
	p.ctrl = nil
	p.tap = nil
	p.playing = false
	p.paused = false
	p.trackDone.Store(false)
}

// Seek moves the playback position by the given duration (positive or negative).
func (p *Player) Seek(d time.Duration) error {
	speaker.Lock()
	defer speaker.Unlock()
	if p.streamer == nil {
		return nil
	}
	curSample := p.streamer.Position()
	curDur := p.format.SampleRate.D(curSample)
	newSample := p.format.SampleRate.N(curDur + d)
	if newSample < 0 {
		newSample = 0
	}
	if newSample >= p.streamer.Len() {
		newSample = p.streamer.Len() - 1
	}
	return p.streamer.Seek(newSample)
}

// Position returns the current playback position.
func (p *Player) Position() time.Duration {
	speaker.Lock()
	defer speaker.Unlock()
	if p.streamer == nil {
		return 0
	}
	return p.format.SampleRate.D(p.streamer.Position())
}

// Duration returns the total duration of the current track.
func (p *Player) Duration() time.Duration {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.streamer == nil {
		return 0
	}
	return p.format.SampleRate.D(p.streamer.Len())
}

// SetVolume sets the volume in dB, clamped to [-30, +6].
func (p *Player) SetVolume(db float64) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.volume = max(min(db, 6), -30)
}

// Volume returns the current volume in dB.
func (p *Player) Volume() float64 {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.volume
}

// SetEQBand sets a single EQ band's gain in dB, clamped to [-12, +12].
func (p *Player) SetEQBand(band int, dB float64) {
	if band < 0 || band >= 10 {
		return
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	p.eqBands[band] = max(min(dB, 12), -12)
}

// EQBands returns a copy of all 10 EQ band gains.
func (p *Player) EQBands() [10]float64 {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.eqBands
}

// IsPlaying returns true if a track is loaded and playing (possibly paused).
func (p *Player) IsPlaying() bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.playing
}

// IsPaused returns true if playback is paused.
func (p *Player) IsPaused() bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.paused
}

// TrackDone returns true if the current track has finished playing.
func (p *Player) TrackDone() bool {
	return p.trackDone.Load()
}

// Samples returns the latest audio samples from the tap for FFT analysis.
func (p *Player) Samples() []float64 {
	p.mu.Lock()
	tap := p.tap
	p.mu.Unlock()
	if tap == nil {
		return nil
	}
	return tap.Samples(2048)
}

// Close stops playback and cleans up.
func (p *Player) Close() {
	p.Stop()
}

// --- Volume Streamer ---

type volumeStreamer struct {
	s   beep.Streamer
	vol *float64
	mu  *sync.Mutex
}

func (v *volumeStreamer) Stream(samples [][2]float64) (int, bool) {
	n, ok := v.s.Stream(samples)
	v.mu.Lock()
	gain := math.Pow(10, *v.vol/20)
	v.mu.Unlock()
	for i := 0; i < n; i++ {
		samples[i][0] *= gain
		samples[i][1] *= gain
	}
	return n, ok
}

func (v *volumeStreamer) Err() error { return v.s.Err() }

// --- Biquad Peaking EQ Filter ---
// Implements a second-order IIR peaking equalizer per the Audio EQ Cookbook.
// Each filter reads its gain from a shared pointer, so EQ changes take
// effect on the next Stream() call without rebuilding the pipeline.

type biquad struct {
	s    beep.Streamer
	freq float64
	q    float64
	gain *float64 // points to Player.eqBands[i]
	sr   float64
	// Per-channel filter state
	x1, x2 [2]float64
	y1, y2  [2]float64
	// Cached coefficients
	lastGain            float64
	b0, b1, b2, a1, a2 float64
	inited              bool
}

func newBiquad(s beep.Streamer, freq, q float64, gain *float64, sr float64) *biquad {
	return &biquad{s: s, freq: freq, q: q, gain: gain, sr: sr}
}

func (b *biquad) calcCoeffs(dB float64) {
	if b.inited && dB == b.lastGain {
		return
	}
	b.lastGain = dB
	b.inited = true

	A := math.Pow(10, dB/40)
	w0 := 2 * math.Pi * b.freq / b.sr
	sinW0 := math.Sin(w0)
	cosW0 := math.Cos(w0)
	alpha := sinW0 / (2 * b.q)

	b0 := 1 + alpha*A
	b1 := -2 * cosW0
	b2 := 1 - alpha*A
	a0 := 1 + alpha/A
	a1 := -2 * cosW0
	a2 := 1 - alpha/A

	b.b0 = b0 / a0
	b.b1 = b1 / a0
	b.b2 = b2 / a0
	b.a1 = a1 / a0
	b.a2 = a2 / a0
}

func (b *biquad) Stream(samples [][2]float64) (int, bool) {
	n, ok := b.s.Stream(samples)
	dB := *b.gain

	// Skip processing when gain is effectively zero
	if dB > -0.1 && dB < 0.1 {
		return n, ok
	}

	b.calcCoeffs(dB)

	for i := 0; i < n; i++ {
		for ch := 0; ch < 2; ch++ {
			x := samples[i][ch]
			y := b.b0*x + b.b1*b.x1[ch] + b.b2*b.x2[ch] - b.a1*b.y1[ch] - b.a2*b.y2[ch]
			b.x2[ch] = b.x1[ch]
			b.x1[ch] = x
			b.y2[ch] = b.y1[ch]
			b.y1[ch] = y
			samples[i][ch] = y
		}
	}
	return n, ok
}

func (b *biquad) Err() error { return b.s.Err() }
