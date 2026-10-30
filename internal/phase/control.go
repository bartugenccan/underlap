package phase

import "math"

// Control is a simplified pitch-control map: for every cell, how many seconds
// earlier the attacking team can get there than the defending team. Positive
// means the attackers arrive first.
type Control struct {
	CellSize float64     `json:"cell_size"`
	Cols     int         `json:"cols"`
	Rows     int         `json:"rows"`
	Margin   [][]float64 `json:"margin"` // [row][col], row along y, col along x
}

func pitchControl(s Scenario, prm Params) Control {
	cols := int(math.Ceil(PitchLength / prm.CellSize))
	rows := int(math.Ceil(PitchWidth / prm.CellSize))
	c := Control{CellSize: prm.CellSize, Cols: cols, Rows: rows, Margin: make([][]float64, rows)}
	for r := 0; r < rows; r++ {
		c.Margin[r] = make([]float64, cols)
		y := math.Min((float64(r)+0.5)*prm.CellSize, PitchWidth)
		for col := 0; col < cols; col++ {
			x := math.Min((float64(col)+0.5)*prm.CellSize, PitchLength)
			ta, _ := firstArrival(s.Attackers, x, y, prm)
			td, _ := firstArrival(s.Defenders, x, y, prm)
			c.Margin[r][col] = round2(td - ta)
		}
	}
	return c
}

// zoneControl fills each zone's share of cells the attackers win.
func zoneControl(zs []Zone, c Control) {
	won := make([]int, len(zs))
	total := make([]int, len(zs))
	for r := 0; r < c.Rows; r++ {
		y := math.Min((float64(r)+0.5)*c.CellSize, PitchWidth-1e-9)
		for col := 0; col < c.Cols; col++ {
			x := math.Min((float64(col)+0.5)*c.CellSize, PitchLength-1e-9)
			lane, band := zoneOf(x, y)
			i := band*5 + lane
			total[i]++
			if c.Margin[r][col] > 0 {
				won[i]++
			}
		}
	}
	for i := range zs {
		if total[i] > 0 {
			zs[i].Control = round2(float64(won[i]) / float64(total[i]))
		}
	}
}
