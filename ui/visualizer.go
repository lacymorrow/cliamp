package ui

import (
	"math"
	"math/cmplx"
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/madelynnblue/go-dsp/fft"
)

const (
	numBands = 10
	fftSize  = 2048
	barWidth = 6 // character width of each spectrum bar
)

// Unicode block elements for bar height (9 levels including space)
var barBlocks = []string{" ", "▁", "▂", "▃", "▄", "▅", "▆", "▇", "█"}

// Frequency edges for 10 spectrum bands (Hz)
var bandEdges = [11]float64{20, 100, 200, 400, 800, 1600, 3200, 6400, 12800, 16000, 20000}

// Pre-built styles for spectrum bar colors to avoid per-frame allocation.
var (
	specLowStyle  = lipgloss.NewStyle().Foreground(spectrumLow)
	specMidStyle  = lipgloss.NewStyle().Foreground(spectrumMid)
	specHighStyle = lipgloss.NewStyle().Foreground(spectrumHigh)
)

// Visualizer performs FFT analysis and renders spectrum bars.
type Visualizer struct {
	prev [numBands]float64 // previous frame for temporal smoothing
	sr   float64
	buf  []float64 // reusable FFT buffer to avoid per-frame allocation
}

// NewVisualizer creates a Visualizer for the given sample rate.
func NewVisualizer(sampleRate float64) *Visualizer {
	return &Visualizer{
		sr:  sampleRate,
		buf: make([]float64, fftSize),
	}
}

// Analyze runs FFT on raw audio samples and returns 10 normalized band levels (0-1).
func (v *Visualizer) Analyze(samples []float64) [numBands]float64 {
	var bands [numBands]float64
	if len(samples) == 0 {
		// Decay previous values when no audio data
		for b := range numBands {
			bands[b] = v.prev[b] * 0.8
			v.prev[b] = bands[b]
		}
		return bands
	}

	// Zero-fill and copy into reusable buffer
	clear(v.buf)
	copy(v.buf, samples)

	// Apply Hann window to reduce spectral leakage
	for i := range fftSize {
		w := 0.5 * (1 - math.Cos(2*math.Pi*float64(i)/float64(fftSize-1)))
		v.buf[i] *= w
	}

	// Compute FFT
	spectrum := fft.FFTReal(v.buf)

	binHz := v.sr / float64(fftSize)

	// Sum magnitudes per frequency band
	for b := range numBands {
		loIdx := int(bandEdges[b] / binHz)
		hiIdx := int(bandEdges[b+1] / binHz)
		if loIdx < 1 {
			loIdx = 1
		}
		halfLen := len(spectrum) / 2
		if hiIdx >= halfLen {
			hiIdx = halfLen - 1
		}

		var sum float64
		count := 0
		for i := loIdx; i <= hiIdx; i++ {
			sum += cmplx.Abs(spectrum[i])
			count++
		}
		if count > 0 {
			sum /= float64(count)
		}

		// Convert to dB-like scale and normalize to 0-1
		if sum > 0 {
			bands[b] = (20*math.Log10(sum) + 10) / 50
		}
		bands[b] = max(0, min(1, bands[b]))

		// Temporal smoothing: fast attack, slow decay
		if bands[b] > v.prev[b] {
			bands[b] = bands[b]*0.6 + v.prev[b]*0.4
		} else {
			bands[b] = bands[b]*0.25 + v.prev[b]*0.75
		}
		v.prev[b] = bands[b]
	}

	return bands
}

// barHeight is the number of character rows for the spectrum bars.
const barHeight = 5

// Render converts band levels into a multi-row colored spectrum bar string.
func (v *Visualizer) Render(bands [numBands]float64) string {
	lines := make([]string, barHeight)

	for row := range barHeight {
		var sb strings.Builder
		// threshold: top row = highest level, bottom row = lowest
		rowBottom := float64(barHeight-1-row) / float64(barHeight)
		rowTop := float64(barHeight-row) / float64(barHeight)

		for i, level := range bands {
			var block string
			if level >= rowTop {
				// Fully filled row
				block = "█"
			} else if level > rowBottom {
				// Partially filled — pick a fractional block
				frac := (level - rowBottom) / (rowTop - rowBottom)
				idx := int(frac * float64(len(barBlocks)-1))
				idx = max(0, min(idx, len(barBlocks)-1))
				block = barBlocks[idx]
			} else {
				block = " "
			}

			// Color gradient based on row height
			var style lipgloss.Style
			switch {
			case rowBottom >= 0.6:
				style = specHighStyle
			case rowBottom >= 0.3:
				style = specMidStyle
			default:
				style = specLowStyle
			}

			sb.WriteString(style.Render(strings.Repeat(block, barWidth)))
			if i < numBands-1 {
				sb.WriteString(" ")
			}
		}
		lines[row] = sb.String()
	}

	return strings.Join(lines, "\n")
}
