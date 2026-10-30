package phase

import (
	"fmt"
	"sort"
)

// The positional-play grid analysts use: five lanes across the pitch, split
// where the penalty box and the six-yard box meet the touchline-parallel
// lines, and six 17.5 m bands along it. "Yarı alan" is a half-space lane.
var (
	laneEdges = []float64{0, 13.84, 24.84, 43.16, 54.16, PitchWidth}
	LaneNames = []string{"sol kanat", "sol yarı alan", "merkez", "sağ yarı alan", "sağ kanat"}
	bandSize  = PitchLength / 6
)

type Zone struct {
	Lane      int    `json:"lane"` // 0..4 from the attacking team's left
	Band      int    `json:"band"` // 0..5 from the attacking team's goal
	Name      string `json:"name"`
	Attackers int    `json:"attackers"`
	Defenders int    `json:"defenders"`
	// Control is the share of the zone's pitch-control cells the attacking
	// team reaches first.
	Control float64 `json:"attacking_control"`
}

func zoneOf(x, y float64) (lane, band int) {
	band = int(x / bandSize)
	if band > 5 {
		band = 5
	}
	lane = 4
	for i := 1; i < len(laneEdges); i++ {
		if y < laneEdges[i] {
			lane = i - 1
			break
		}
	}
	return lane, band
}

func zoneName(lane, band int) string {
	third := []string{"kendi yarı sahasının derini", "kendi yarı sahası", "orta saha (geri)", "orta saha (ileri)", "son üçte bir", "ceza sahası önü"}[band]
	return fmt.Sprintf("%s, %s", LaneNames[lane], third)
}

// zones counts players per zone. Goalkeepers count: a keeper joining the
// build-up is exactly the extra man analysts talk about.
func zones(s Scenario) []Zone {
	out := make([]Zone, 0, 30)
	idx := map[[2]int]int{}
	for band := 0; band < 6; band++ {
		for lane := 0; lane < 5; lane++ {
			idx[[2]int{lane, band}] = len(out)
			out = append(out, Zone{Lane: lane, Band: band, Name: zoneName(lane, band)})
		}
	}
	for _, p := range s.Attackers {
		l, b := zoneOf(p.X, p.Y)
		out[idx[[2]int{l, b}]].Attackers++
	}
	for _, p := range s.Defenders {
		l, b := zoneOf(p.X, p.Y)
		out[idx[[2]int{l, b}]].Defenders++
	}
	return out
}

// Overload is a local numerical superiority: a zone plus its orthogonal
// neighbours where the attacking side has more players.
type Overload struct {
	Lane      int      `json:"lane"`
	Band      int      `json:"band"`
	Name      string   `json:"name"`
	Attackers []string `json:"attackers"`
	Defenders []string `json:"defenders"`
}

func overloads(s Scenario) []Overload {
	var out []Overload
	for band := 0; band < 6; band++ {
		for lane := 0; lane < 5; lane++ {
			in := func(p Player) bool {
				l, b := zoneOf(p.X, p.Y)
				dl, db := l-lane, b-band
				return (dl == 0 && (db == 0 || db == 1 || db == -1)) || (db == 0 && (dl == 1 || dl == -1))
			}
			centre := func(p Player) bool { l, b := zoneOf(p.X, p.Y); return l == lane && b == band }
			var att, def []string
			attHere := 0
			for _, p := range s.Attackers {
				if in(p) {
					att = append(att, p.Name)
				}
				if centre(p) {
					attHere++
				}
			}
			for _, p := range s.Defenders {
				if in(p) && !p.IsGK() {
					def = append(def, p.Name)
				}
			}
			// Anchor each overload on a zone that actually holds attackers, and
			// require at least a 3v2 so two players chatting is not reported.
			if attHere > 0 && len(att) >= 3 && len(att)-len(def) >= 1 {
				out = append(out, Overload{lane, band, zoneName(lane, band), att, def})
			}
		}
	}
	return dedupeOverloads(out)
}

// dedupeOverloads keeps one entry per group: neighbouring anchor zones often
// see mostly the same players. Strongest superiority first; an overload that
// shares two or more attackers with one already kept is dropped. Ties keep
// zone order, so the output stays deterministic.
func dedupeOverloads(in []Overload) []Overload {
	sorted := append([]Overload(nil), in...)
	sort.SliceStable(sorted, func(i, j int) bool {
		return len(sorted[i].Attackers)-len(sorted[i].Defenders) > len(sorted[j].Attackers)-len(sorted[j].Defenders)
	})
	var kept []Overload
	for _, o := range sorted {
		dup := false
		for _, k := range kept {
			if shared(o.Attackers, k.Attackers) >= 2 {
				dup = true
				break
			}
		}
		if !dup {
			kept = append(kept, o)
		}
	}
	return kept
}

func shared(a, b []string) int {
	n := 0
	for _, x := range a {
		for _, y := range b {
			if x == y {
				n++
			}
		}
	}
	return n
}
