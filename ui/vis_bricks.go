package ui

import "strings"

// renderBricks draws solid block columns with visible gaps between rows and bands.
// Uses half-height blocks (▄) so each brick is half a terminal row, with blank
// gaps between them, keeping total height equal to the bars visualizer.
func (v *Visualizer) renderBricks(bands [numBands]float64) string {
	height := v.Rows

	lines := make([]string, height)

	for row := range height {
		var sb strings.Builder
		rowThreshold := float64(height-1-row) / float64(height)

		for i, level := range bands {
			bw := visBandWidth(i)
			style := specStyle(rowThreshold)
			if level > rowThreshold {
				sb.WriteString(style.Render(strings.Repeat("▄", bw)))
			} else {
				sb.WriteString(strings.Repeat(" ", bw))
			}
			if i < numBands-1 {
				sb.WriteString(" ")
			}
		}
		lines[row] = sb.String()
	}

	return strings.Join(lines, "\n")
}
