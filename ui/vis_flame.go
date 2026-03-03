package ui

import (
	"math"
	"strings"
)

// renderFlame draws rising flame tendrils using Braille dots. Each band produces
// a column of flickering fire that rises proportionally to energy, with lateral
// wobble driven by a sine-based displacement for an organic, dancing look.
func (v *Visualizer) renderFlame(bands [numBands]float64) string {
	height := v.Rows
	dotRows := height * 4

	lines := make([]string, height)

	for row := range height {
		var sb strings.Builder

		for b := range numBands {
			charsPerBand := visBandWidth(b)
			bandDotCols := charsPerBand * 2
			for c := range charsPerBand {
				var braille rune = '\u2800'

				for dr := range 4 {
					for dc := range 2 {
						dotRow := row*4 + dr
						dotCol := c*2 + dc

						// Invert: flames rise from bottom, so row 0 = top of flame.
						flameY := float64(dotRows-1-dotRow) / float64(dotRows-1)

						// Flame reaches up to flameY proportional to band level.
						if flameY > bands[b] {
							continue
						}

						// Lateral wobble: sine wave displaced by height and time.
						t := float64(v.frame) * 0.3
						wobble := math.Sin(t+flameY*6.0+float64(b)*2.1) * 1.5
						centerCol := float64(bandDotCols) / 2.0

						// Flame narrows toward the tip.
						tipNarrow := 1.0 - flameY/max(bands[b], 0.01)
						flameWidth := (0.3 + 0.7*tipNarrow) * centerCol

						dist := math.Abs(float64(dotCol) - centerCol + 0.5 - wobble) // distance from flame center
						if dist < flameWidth {
							// Add flicker at the edges using hash.
							edge := dist / flameWidth
							if edge < 0.7 || scatterHash(b, dotRow, dotCol, v.frame) < 0.6 {
								braille |= brailleBit[dr][dc]
							}
						}
					}
				}

				// Color: bottom rows (base) are red/hot, upper rows (tips) are green/cool.
				// This inverts the normal spectrum coloring for a fire gradient effect.
				rowNorm := float64(row) / float64(height)
				style := specStyle(rowNorm)
				sb.WriteString(style.Render(string(braille)))
			}
			if b < numBands-1 {
				sb.WriteString(" ")
			}
		}
		lines[row] = sb.String()
	}

	return strings.Join(lines, "\n")
}
