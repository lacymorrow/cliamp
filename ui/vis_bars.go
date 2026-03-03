package ui

import "strings"

// renderBars is the default smooth spectrum with fractional Unicode blocks.
func (v *Visualizer) renderBars(bands [numBands]float64) string {
	height := v.Rows

	lines := make([]string, height)

	for row := range height {
		var sb strings.Builder
		rowBottom := float64(height-1-row) / float64(height)
		rowTop := float64(height-row) / float64(height)

		for i, level := range bands {
			bw := visBandWidth(i)
			var block string
			if level >= rowTop {
				block = "█"
			} else if level > rowBottom {
				frac := (level - rowBottom) / (rowTop - rowBottom)
				idx := int(frac * float64(len(barBlocks)-1))
				idx = max(0, min(idx, len(barBlocks)-1))
				block = barBlocks[idx]
			} else {
				block = " "
			}

			style := specStyle(rowBottom)
			sb.WriteString(style.Render(strings.Repeat(block, bw)))
			if i < numBands-1 {
				sb.WriteString(" ")
			}
		}
		lines[row] = sb.String()
	}

	return strings.Join(lines, "\n")
}
