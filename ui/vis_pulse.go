package ui

import (
	"math"
	"strings"
)

// renderPulse draws a pulsating ellipse using Braille dots that spreads across
// the full display width. The radius at each angle is driven by the
// corresponding frequency band, so the shape deforms and beats hard in all
// directions. A secondary shockwave ring bursts outward on strong transients.
func (v *Visualizer) renderPulse(bands [numBands]float64) string {
	height := v.Rows
	dotRows := height * 4
	dotCols := panelWidth * 2

	centerX := float64(dotCols) / 2.0
	centerY := float64(dotRows) / 2.0

	// Scale factor to squash x-axis so the shape fills the wide display.
	// In the scaled coordinate space the shape is circular; on screen it
	// becomes a wide ellipse spanning the full panel width.
	xScale := centerY / centerX
	maxR := centerY - 1

	// Overall energy drives the shockwave.
	var totalEnergy float64
	for _, e := range bands {
		totalEnergy += e
	}
	avgEnergy := totalEnergy / float64(numBands)

	// Shockwave: an expanding ring triggered by energy spikes.
	shockPhase := math.Mod(float64(v.frame)*0.12, 1.0)
	shockR := maxR * shockPhase
	shockStrength := avgEnergy * (1.0 - shockPhase)

	lines := make([]string, height)

	for row := range height {
		var sb strings.Builder

		for c := range panelWidth {
			var braille rune = '\u2800'
			var peakIntensity float64

			for dr := range 4 {
				for dc := range 2 {
					dotX := float64(c*2 + dc)
					dotY := float64(row*4 + dr)

					dx := (dotX - centerX) * xScale
					dy := dotY - centerY
					dist := math.Sqrt(dx*dx + dy*dy)

					// Bright center dot.
					if dist < 1.0 {
						braille |= brailleBit[dr][dc]
						if peakIntensity < 0.2 {
							peakIntensity = 0.2
						}
						continue
					}

					angle := math.Atan2(dy, dx)
					if angle < 0 {
						angle += 2 * math.Pi
					}

					// Rotation speeds up with energy.
					rotSpeed := 0.02 + avgEnergy*0.06
					rotAngle := angle + float64(v.frame)*rotSpeed
					rotAngle -= math.Floor(rotAngle/(2*math.Pi)) * 2 * math.Pi

					// Map angle to band with cosine interpolation.
					bandPos := rotAngle / (2 * math.Pi) * float64(numBands)
					bandIdx := int(bandPos) % numBands
					nextBand := (bandIdx + 1) % numBands
					frac := bandPos - math.Floor(bandPos)
					t := (1 - math.Cos(frac*math.Pi)) / 2
					energy := bands[bandIdx]*(1-t) + bands[nextBand]*t

					// Aggressive radius: tiny at rest, slams outward with energy.
					punch := energy * energy
					r := maxR * (0.1 + 0.9*punch)
					if r < 1 {
						continue
					}

					drawn := false

					// --- Main body ---
					if dist <= r {
						proximity := dist / r
						if proximity > peakIntensity {
							peakIntensity = proximity
						}

						if proximity > 0.45 {
							braille |= brailleBit[dr][dc]
							drawn = true
						} else {
							density := 0.3 + proximity*0.7
							if scatterHash(bandIdx, row*4+dr, c*2+dc, v.frame) < density {
								braille |= brailleBit[dr][dc]
								drawn = true
							}
						}
					}

					// --- Outer glow ---
					if !drawn && dist < r+4.0 && energy > 0.15 {
						overflow := (dist - r) / 4.0
						glowChance := energy * (1.0 - overflow) * 0.4
						if scatterHash(bandIdx, row*4+dr, c*2+dc, v.frame) < glowChance {
							braille |= brailleBit[dr][dc]
							if peakIntensity < 0.85 {
								peakIntensity = 0.85
							}
						}
					}

					// --- Shockwave ring ---
					if shockStrength > 0.1 {
						shockDist := math.Abs(dist - shockR)
						shockThick := 1.0 + shockStrength*2.0
						if shockDist < shockThick {
							fade := 1.0 - shockDist/shockThick
							if scatterHash(bandIdx+7, row*4+dr, c*2+dc, v.frame) < fade*shockStrength {
								braille |= brailleBit[dr][dc]
								if peakIntensity < 0.7 {
									peakIntensity = 0.7
								}
							}
						}
					}
				}
			}

			style := specStyle(peakIntensity)
			sb.WriteString(style.Render(string(braille)))
		}

		lines[row] = sb.String()
	}

	return strings.Join(lines, "\n")
}
