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
)

// Unicode block elements for bar height (8 levels)
var barBlocks = []string{" ", "▁", "▂", "▃", "▄", "▅", "▆", "▇", "█"}

// Frequency edges for 10 spectrum bands (Hz)
var bandEdges = [11]float64{20, 100, 200, 400, 800, 1600, 3200, 6400, 12800, 16000, 20000}

// Visualizer performs FFT analysis and renders spectrum bars.
type Visualizer struct {
	prev [numBands]float64 // previous frame for temporal smoothing
	sr   float64
}

// NewVisualizer creates a Visualizer for the given sample rate.
func NewVisualizer(sampleRate float64) *Visualizer {
	return &Visualizer{sr: sampleRate}
}

// Analyze runs FFT on raw audio samples and returns 10 normalized band levels (0-1).
func (v *Visualizer) Analyze(samples []float64) [numBands]float64 {
	var bands [numBands]float64
	if len(samples) == 0 {
		// Decay previous values
		for b := 0; b < numBands; b++ {
			bands[b] = v.prev[b] * 0.8
			v.prev[b] = bands[b]
		}
		return bands
	}

	// Zero-pad or truncate to fftSize
	buf := make([]float64, fftSize)
	copy(buf, samples)

	// Apply Hann window to reduce spectral leakage
	for i := 0; i < fftSize; i++ {
		w := 0.5 * (1 - math.Cos(2*math.Pi*float64(i)/float64(fftSize-1)))
		buf[i] *= w
	}

	// Compute FFT
	spectrum := fft.FFTReal(buf)

	binHz := v.sr / float64(fftSize)

	// Sum magnitudes per frequency band
	for b := 0; b < numBands; b++ {
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

// Render converts band levels into a colored spectrum bar string.
func (v *Visualizer) Render(bands [numBands]float64) string {
	var sb strings.Builder

	for i, level := range bands {
		idx := int(level * float64(len(barBlocks)-1))
		if idx < 0 {
			idx = 0
		}
		if idx >= len(barBlocks) {
			idx = len(barBlocks) - 1
		}

		block := barBlocks[idx]

		// Color gradient: green -> yellow -> red based on level
		var style lipgloss.Style
		switch {
		case level > 0.75:
			style = lipgloss.NewStyle().Foreground(spectrumHigh)
		case level > 0.45:
			style = lipgloss.NewStyle().Foreground(spectrumMid)
		default:
			style = lipgloss.NewStyle().Foreground(spectrumLow)
		}

		sb.WriteString(style.Render(strings.Repeat(block, 5)))
		if i < numBands-1 {
			sb.WriteString(" ")
		}
	}

	return sb.String()
}
