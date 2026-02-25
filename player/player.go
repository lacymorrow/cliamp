package player

import (
	"fmt"
	"io"
	"math"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/gopxl/beep/v2"
	"github.com/gopxl/beep/v2/flac"
	"github.com/gopxl/beep/v2/mp3"
	"github.com/gopxl/beep/v2/speaker"
	"github.com/gopxl/beep/v2/vorbis"
	"github.com/gopxl/beep/v2/wav"
)

// EQFreqs are the center frequencies for the 10-band parametric equalizer.
var EQFreqs = [10]float64{70, 180, 320, 600, 1000, 3000, 6000, 12000, 14000, 16000}

// SupportedExts is the set of file extensions the player can decode.
var SupportedExts = map[string]bool{
	".mp3":  true,
	".wav":  true,
	".flac": true,
	".ogg":  true,
	".m4a":  true,
	".aac":  true,
	".m4b":  true,
	".alac": true,
	".wma":  true,
	".opus": true,
}

// httpClient is used for all HTTP streaming. It sets a dial/TLS/header
// timeout but no overall timeout, so infinite live streams aren't killed.
var httpClient = &http.Client{
	Transport: &http.Transport{
		ResponseHeaderTimeout: 15 * time.Second,
	},
}

// isURL reports whether path is an HTTP or HTTPS URL.
func isURL(path string) bool {
	return strings.HasPrefix(path, "http://") || strings.HasPrefix(path, "https://")
}

// Player is the audio engine managing the playback pipeline:
//
//	[MP3 Decode] -> [Resample] -> [10x Biquad EQ] -> [Volume] -> [Tap] -> [Ctrl] -> [Speaker]
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
	seekable  bool
	mono      bool
	rc        io.ReadCloser
}

// New creates a Player and initializes the speaker at the given sample rate.
func New(sr beep.SampleRate) *Player {
	speaker.Init(sr, sr.N(time.Second/10))
	return &Player{sr: sr}
}

// Play opens and starts playing an audio file, building the full audio pipeline.
// Supported formats: MP3, WAV, FLAC, OGG Vorbis.
func (p *Player) Play(path string) error {
	p.Stop()

	rc, err := openSource(path)
	if err != nil {
		return err
	}

	streamer, format, err := decode(rc, path, p.sr)
	if err != nil {
		rc.Close()
		return fmt.Errorf("decode: %w", err)
	}

	p.mu.Lock()
	p.rc = rc
	p.streamer = streamer
	p.format = format
	p.trackDone.Store(false)

	// HTTP streams decoded natively read from a non-seekable http.Response.Body.
	// FFmpeg-decoded streams are fully buffered in memory and therefore seekable.
	_, isPCM := streamer.(*pcmStreamer)
	p.seekable = !isURL(path) || isPCM

	var s beep.Streamer = streamer

	// Resample to target sample rate if needed
	if format.SampleRate != p.sr {
		s = beep.Resample(4, format.SampleRate, p.sr, s)
	}

	// Chain 10 biquad peaking EQ filters; each reads its gain from p.eqBands[i]
	for i := range 10 {
		s = newBiquad(s, EQFreqs[i], 1.4, &p.eqBands[i], float64(p.sr))
	}

	// Volume control + mono downmix
	s = &volumeStreamer{s: s, vol: &p.volume, mono: &p.mono, mu: &p.mu}

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
	if p.rc != nil {
		p.rc.Close()
		p.rc = nil
	}
	p.ctrl = nil
	p.tap = nil
	p.playing = false
	p.paused = false
	p.seekable = false
	p.trackDone.Store(false)
}

// Seek moves the playback position by the given duration (positive or negative).
// Returns nil immediately for non-seekable streams (e.g., HTTP without ffmpeg).
func (p *Player) Seek(d time.Duration) error {
	speaker.Lock()
	defer speaker.Unlock()
	if p.streamer == nil || !p.seekable {
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

// ToggleMono switches between stereo and mono (L+R downmix) output.
func (p *Player) ToggleMono() {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.mono = !p.mono
}

// Mono returns true if mono output is enabled.
func (p *Player) Mono() bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.mono
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

// Seekable reports whether the current track supports seeking.
func (p *Player) Seekable() bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.seekable
}

// StreamErr returns the current streamer error, if any (e.g., connection drops).
func (p *Player) StreamErr() error {
	p.mu.Lock()
	s := p.streamer
	p.mu.Unlock()
	if s == nil {
		return nil
	}
	return s.Err()
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

// openSource returns a ReadCloser for the given path, handling both
// local files and HTTP URLs.
func openSource(path string) (io.ReadCloser, error) {
	if !isURL(path) {
		return os.Open(path)
	}
	resp, err := httpClient.Get(path)
	if err != nil {
		return nil, fmt.Errorf("http get: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		resp.Body.Close()
		return nil, fmt.Errorf("http status %s", resp.Status)
	}
	return resp.Body, nil
}

// formatExt returns the audio format extension for a path.
// For URLs, it parses the path component (ignoring query params),
// checks a "format" query param as fallback, and defaults to ".mp3".
func formatExt(path string) string {
	if !isURL(path) {
		return strings.ToLower(filepath.Ext(path))
	}
	u, err := url.Parse(path)
	if err != nil {
		return ".mp3"
	}
	ext := strings.ToLower(filepath.Ext(u.Path))
	if ext == "" || ext == ".view" {
		if f := u.Query().Get("format"); f != "" {
			return "." + strings.ToLower(f)
		}
		return ".mp3"
	}
	return ext
}

// needsFFmpeg reports whether the given extension requires ffmpeg to decode.
func needsFFmpeg(ext string) bool {
	switch ext {
	case ".m4a", ".aac", ".m4b", ".alac", ".wma", ".opus":
		return true
	}
	return false
}

// decode selects the appropriate decoder based on the file extension.
func decode(rc io.ReadCloser, path string, sr beep.SampleRate) (beep.StreamSeekCloser, beep.Format, error) {
	ext := formatExt(path)
	if needsFFmpeg(ext) {
		return decodeFFmpeg(path, sr)
	}
	switch ext {
	case ".wav":
		return wav.Decode(rc)
	case ".flac":
		return flac.Decode(rc)
	case ".ogg":
		return vorbis.Decode(rc)
	default:
		return mp3.Decode(rc)
	}
}

// volumeStreamer applies dB gain and optional mono downmix to an audio stream.
type volumeStreamer struct {
	s    beep.Streamer
	vol  *float64
	mono *bool
	mu   *sync.Mutex
}

func (v *volumeStreamer) Stream(samples [][2]float64) (int, bool) {
	n, ok := v.s.Stream(samples)
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

// biquad implements a second-order IIR peaking equalizer per the Audio EQ Cookbook.
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
	y1, y2 [2]float64
	// Cached coefficients
	lastGain           float64
	b0, b1, b2, a1, a2 float64
	inited             bool
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

	a := math.Pow(10, dB/40)
	w0 := 2 * math.Pi * b.freq / b.sr
	sinW0 := math.Sin(w0)
	cosW0 := math.Cos(w0)
	alpha := sinW0 / (2 * b.q)

	b0 := 1 + alpha*a
	b1 := -2 * cosW0
	b2 := 1 - alpha*a
	a0 := 1 + alpha/a
	a1 := -2 * cosW0
	a2 := 1 - alpha/a

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

	for i := range n {
		for ch := range 2 {
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
