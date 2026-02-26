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

// trackPipeline bundles a decoded track's resources.
type trackPipeline struct {
	decoder  beep.StreamSeekCloser // raw decoder (for Position/Duration/Seek)
	stream   beep.Streamer         // decoder + optional resample (fed to gapless)
	format   beep.Format
	seekable bool
	rc       io.ReadCloser // source file/HTTP body
}

// close releases the pipeline's resources.
func (tp *trackPipeline) close() {
	if tp.decoder != nil {
		tp.decoder.Close()
	}
	if tp.rc != nil {
		tp.rc.Close()
	}
}

// Player is the audio engine managing the playback pipeline:
//
//	[Gapless] -> [10x Biquad EQ] -> [Volume] -> [Tap] -> [Ctrl] -> speaker
//	     ↑
//	     ├─ current: [Decode A] → [Resample A]
//	     └─ next:    [Decode B] → [Resample B]  (preloaded)
type Player struct {
	mu           sync.Mutex
	sr           beep.SampleRate
	gapless      *gaplessStreamer
	current      *trackPipeline // active track's resources
	nextPipeline *trackPipeline // preloaded track's resources
	started      bool           // true after first speaker.Play()
	ctrl         *beep.Ctrl
	volume       float64 // dB, range [-30, +6]
	eqBands      [10]float64
	tap          *Tap
	playing      bool
	paused       bool
	mono         bool

	gaplessAdvance atomic.Bool // set when gapless transition fires
}

// New creates a Player and initializes the speaker at the given sample rate.
func New(sr beep.SampleRate) *Player {
	speaker.Init(sr, sr.N(time.Second/10))
	p := &Player{sr: sr}
	p.gapless = &gaplessStreamer{}
	p.gapless.onSwap = func() {
		// Called from audio thread (goroutine) when gapless transition occurs.
		// Swap current ← nextPipeline and close the old one.
		p.mu.Lock()
		old := p.current
		p.current = p.nextPipeline
		p.nextPipeline = nil
		p.mu.Unlock()
		if old != nil {
			old.close()
		}
		p.gaplessAdvance.Store(true)
	}
	return p
}

// buildPipeline opens and decodes a track, returning a ready-to-play pipeline.
func (p *Player) buildPipeline(path string) (*trackPipeline, error) {
	rc, err := openSource(path)
	if err != nil {
		return nil, err
	}

	decoder, format, err := decode(rc, path, p.sr)
	if err != nil {
		rc.Close()
		return nil, fmt.Errorf("decode: %w", err)
	}

	// HTTP streams decoded natively read from a non-seekable http.Response.Body.
	// FFmpeg-decoded streams are fully buffered in memory and therefore seekable.
	_, isPCM := decoder.(*pcmStreamer)
	seekable := !isURL(path) || isPCM

	var s beep.Streamer = decoder
	if format.SampleRate != p.sr {
		s = beep.Resample(4, format.SampleRate, p.sr, s)
	}

	return &trackPipeline{
		decoder:  decoder,
		stream:   s,
		format:   format,
		seekable: seekable,
		rc:       rc,
	}, nil
}

// Play opens and starts playing an audio file. On the first call it builds
// the long-lived EQ → volume → tap → ctrl chain and starts the speaker.
// Subsequent calls swap only the track source via the gapless streamer.
func (p *Player) Play(path string) error {
	tp, err := p.buildPipeline(path)
	if err != nil {
		return err
	}

	p.mu.Lock()

	// Close previous track resources
	if p.current != nil {
		p.current.close()
	}
	// Discard any preloaded next
	if p.nextPipeline != nil {
		p.nextPipeline.close()
		p.nextPipeline = nil
	}

	p.current = tp
	p.gapless.Replace(tp.stream)

	if !p.started {
		// Build the long-lived pipeline once
		var s beep.Streamer = p.gapless

		for i := range 10 {
			s = newBiquad(s, EQFreqs[i], 1.4, &p.eqBands[i], float64(p.sr))
		}
		s = &volumeStreamer{s: s, vol: &p.volume, mono: &p.mono, mu: &p.mu}
		p.tap = NewTap(s, 4096)
		p.ctrl = &beep.Ctrl{Streamer: p.tap}
		p.started = true
		p.playing = true
		p.paused = false
		p.mu.Unlock()

		speaker.Play(p.ctrl)
		return nil
	}

	// Unpause if paused
	p.ctrl.Paused = false
	p.playing = true
	p.paused = false
	p.mu.Unlock()

	return nil
}

// Preload builds a pipeline for the next track and queues it for gapless transition.
func (p *Player) Preload(path string) error {
	tp, err := p.buildPipeline(path)
	if err != nil {
		return err
	}

	p.mu.Lock()
	// Close previously preloaded pipeline if any
	if p.nextPipeline != nil {
		p.nextPipeline.close()
	}
	p.nextPipeline = tp
	p.mu.Unlock()

	p.gapless.SetNext(tp.stream)
	return nil
}

// ClearPreload discards the preloaded next track (e.g., when shuffle/repeat changes).
func (p *Player) ClearPreload() {
	p.gapless.SetNext(nil)
	p.mu.Lock()
	if p.nextPipeline != nil {
		p.nextPipeline.close()
		p.nextPipeline = nil
	}
	p.mu.Unlock()
}

// GaplessAdvanced returns true (once) when a gapless transition happened.
func (p *Player) GaplessAdvanced() bool {
	return p.gaplessAdvance.CompareAndSwap(true, false)
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

// Stop halts playback and releases resources. The speaker continues running
// (outputting silence via the gapless streamer) so it can be restarted without
// rebuilding the pipeline.
func (p *Player) Stop() {
	p.gapless.Clear()
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.current != nil {
		p.current.close()
		p.current = nil
	}
	if p.nextPipeline != nil {
		p.nextPipeline.close()
		p.nextPipeline = nil
	}
	if p.ctrl != nil {
		p.ctrl.Paused = true
	}
	p.playing = false
	p.paused = false
}

// Seek moves the playback position by the given duration (positive or negative).
// Returns nil immediately for non-seekable streams (e.g., HTTP without ffmpeg).
func (p *Player) Seek(d time.Duration) error {
	speaker.Lock()
	defer speaker.Unlock()
	p.mu.Lock()
	cur := p.current
	p.mu.Unlock()
	if cur == nil || !cur.seekable {
		return nil
	}
	curSample := cur.decoder.Position()
	curDur := cur.format.SampleRate.D(curSample)
	newSample := cur.format.SampleRate.N(curDur + d)
	if newSample < 0 {
		newSample = 0
	}
	if newSample >= cur.decoder.Len() {
		newSample = cur.decoder.Len() - 1
	}
	return cur.decoder.Seek(newSample)
}

// Position returns the current playback position.
func (p *Player) Position() time.Duration {
	speaker.Lock()
	defer speaker.Unlock()
	p.mu.Lock()
	cur := p.current
	p.mu.Unlock()
	if cur == nil {
		return 0
	}
	return cur.format.SampleRate.D(cur.decoder.Position())
}

// Duration returns the total duration of the current track.
func (p *Player) Duration() time.Duration {
	p.mu.Lock()
	cur := p.current
	p.mu.Unlock()
	if cur == nil {
		return 0
	}
	return cur.format.SampleRate.D(cur.decoder.Len())
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

// Drained returns true if the current track ended with no preloaded next track.
func (p *Player) Drained() bool {
	return p.gapless.Drained()
}

// Seekable reports whether the current track supports seeking.
func (p *Player) Seekable() bool {
	p.mu.Lock()
	cur := p.current
	p.mu.Unlock()
	return cur != nil && cur.seekable
}

// StreamErr returns the current streamer error, if any (e.g., connection drops).
func (p *Player) StreamErr() error {
	p.mu.Lock()
	cur := p.current
	p.mu.Unlock()
	if cur == nil {
		return nil
	}
	return cur.decoder.Err()
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

// Close fully stops the speaker and cleans up all resources.
func (p *Player) Close() {
	p.Stop()
	speaker.Clear()
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
