package phase

import (
	"fmt"
	"strconv"
	"strings"
)

// LineupPlayer is a starter as the lineup source gives it.
type LineupPlayer struct {
	ID     string `json:"id"`
	Name   string `json:"name"`
	Number int    `json:"number,omitempty"`
	Pos    string `json:"pos,omitempty"` // G, D, M, F
	Grid   string `json:"grid"`          // "row:column"; row 1 is the keeper, column 1 the team's left
}

// Seed turns a starting XI into nominal pitch positions: each grid row becomes
// a band from the team's own goal towards halfway, each player in a row is
// spread evenly across the width. It is the announced shape, not where the
// players will actually stand in possession or out of it; the analyst moves
// them from there.
//
// attacking selects the side of the frame: the attacking team defends x=0 and
// its column 1 is y=0; the defending team defends x=105 and, facing the other
// way, has its column 1 at y=68.
func Seed(lineup []LineupPlayer, attacking bool) ([]Player, error) {
	type slot struct{ row, col int }
	slots := make([]slot, len(lineup))
	rows, perRow := 0, map[int]int{}
	for i, p := range lineup {
		r, c, err := parseGrid(p.Grid)
		if err != nil {
			return nil, fmt.Errorf("%s: %w", p.Name, err)
		}
		slots[i] = slot{r, c}
		rows = max(rows, r)
		perRow[r] = max(perRow[r], c)
	}

	const (
		keeperX = 5.0  // m from the own goal line
		frontX  = 48.0 // front row just inside the own half
	)
	out := make([]Player, len(lineup))
	for i, p := range lineup {
		s := slots[i]
		x := keeperX
		if rows > 1 {
			x = keeperX + float64(s.row-1)*(frontX-keeperX)/float64(rows-1)
		}
		y := (float64(s.col) - 0.5) * PitchWidth / float64(perRow[s.row])
		if !attacking {
			x, y = PitchLength-x, PitchWidth-y
		}
		role := ""
		if p.Pos == "G" || s.row == 1 {
			role = "GK"
		}
		out[i] = Player{ID: p.ID, Name: p.Name, X: round2(x), Y: round2(y), Role: role}
	}
	return out, nil
}

func parseGrid(g string) (row, col int, err error) {
	r, c, ok := strings.Cut(g, ":")
	if !ok {
		return 0, 0, fmt.Errorf("grid %q: want row:column", g)
	}
	if row, err = strconv.Atoi(r); err != nil || row < 1 {
		return 0, 0, fmt.Errorf("grid %q: bad row", g)
	}
	if col, err = strconv.Atoi(c); err != nil || col < 1 {
		return 0, 0, fmt.Errorf("grid %q: bad column", g)
	}
	return row, col, nil
}
