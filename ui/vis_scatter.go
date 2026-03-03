package ui

import "strings"

// renderScatter draws a twinkling particle field using Braille dots.
// Dot density per band is proportional to the squared energy level, with a
// gravity bias that makes particles denser near the bottom.
func (v *Visualizer) renderScatter(bands [numBands]float64) string {
	height := v.Rows
	dotRows := height * 4

	lines := make([]string, height)

	for row := range height {
		var sb strings.Builder

		for b := range numBands {
			charsPerBand := visBandWidth(b)
			for c := range charsPerBand {
				var braille rune = '\u2800'

				for dr := range 4 {
					for dc := range 2 {
						dotRow := row*4 + dr
						dotCol := c*2 + dc

						h := scatterHash(b, dotRow, dotCol, v.frame)

						// Gravity bias: more particles settle near the bottom.
						heightFactor := 0.5 + 0.5*float64(dotRow)/float64(dotRows-1)
						threshold := bands[b] * bands[b] * heightFactor

						if h < threshold {
							braille |= brailleBit[dr][dc]
						}
					}
				}

				rowNorm := float64(height-1-row) / float64(height)
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
