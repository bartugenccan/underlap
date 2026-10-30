package phase

import (
	"math"
	"sort"
)

// Line is one horizontal band of the defending team (e.g. the back four),
// found by clustering outfield defenders on x.
type Line struct {
	Members []string `json:"members"`
	MinX    float64  `json:"min_x"`
	MaxX    float64  `json:"max_x"`
	MeanX   float64  `json:"mean_x"`
}

// linePenalty is the cost of opening one more line, in squared metres. It
// keeps a slightly staggered back four as one line while still splitting a
// midfield that sits 10+ metres in front of it.
const linePenalty = 60

// defensiveLines clusters outfield defenders into 1-4 lines with exact 1D
// k-means (dynamic programming over the sorted x values), picking the number
// of lines that minimises spread plus linePenalty per line. Lines come back
// deepest first (closest to the goal the defenders protect, i.e. highest x).
func defensiveLines(s Scenario) []Line {
	var ps []Player
	for _, p := range s.Defenders {
		if !p.IsGK() {
			ps = append(ps, p)
		}
	}
	if len(ps) == 0 {
		return nil
	}
	sort.SliceStable(ps, func(i, j int) bool { return ps[i].X < ps[j].X })
	n := len(ps)

	// cost(i, j) = sum of squared deviations of ps[i..j] from their mean.
	prefix, prefix2 := make([]float64, n+1), make([]float64, n+1)
	for i, p := range ps {
		prefix[i+1] = prefix[i] + p.X
		prefix2[i+1] = prefix2[i] + p.X*p.X
	}
	cost := func(i, j int) float64 {
		cnt := float64(j - i + 1)
		sum := prefix[j+1] - prefix[i]
		return (prefix2[j+1] - prefix2[i]) - sum*sum/cnt
	}

	maxK := min(4, n)
	bestScore, bestCuts := math.Inf(1), []int(nil)
	for k := 1; k <= maxK; k++ {
		// dp[c][j]: best cost splitting ps[0..j] into c+1 clusters.
		dp := make([][]float64, k)
		cut := make([][]int, k)
		for c := range dp {
			dp[c] = make([]float64, n)
			cut[c] = make([]int, n)
			for j := range dp[c] {
				dp[c][j] = math.Inf(1)
			}
		}
		for j := 0; j < n; j++ {
			dp[0][j] = cost(0, j)
		}
		for c := 1; c < k; c++ {
			for j := c; j < n; j++ {
				for i := c; i <= j; i++ {
					if v := dp[c-1][i-1] + cost(i, j); v < dp[c][j] {
						dp[c][j], cut[c][j] = v, i
					}
				}
			}
		}
		score := dp[k-1][n-1] + linePenalty*float64(k)
		if score < bestScore-1e-9 {
			bestScore = score
			cuts := make([]int, 0, k)
			j := n - 1
			for c := k - 1; c > 0; c-- {
				i := cut[c][j]
				cuts = append([]int{i}, cuts...)
				j = i - 1
			}
			bestCuts = cuts
		}
	}

	var lines []Line
	start := 0
	for _, end := range append(bestCuts, n) {
		l := Line{MinX: ps[start].X, MaxX: ps[end-1].X}
		var sum float64
		for _, p := range ps[start:end] {
			l.Members = append(l.Members, p.Name)
			sum += p.X
		}
		l.MeanX = round2(sum / float64(end-start))
		lines = append(lines, l)
		start = end
	}
	// Deepest first.
	sort.SliceStable(lines, func(i, j int) bool { return lines[i].MeanX > lines[j].MeanX })
	return lines
}

// BetweenLines is an attacker who has gone past part of the defending team
// and still has a block behind him: the "blok arası" / "hatlar arası" receiver.
type BetweenLines struct {
	Player   string   `json:"player"`
	Name     string   `json:"name"`
	Bypassed []string `json:"bypassed"` // defenders now behind the play, nearest first
	Behind   []string `json:"behind"`   // defenders still between him and goal, nearest first
	// Space is the distance to the nearest defender, in metres.
	Space          float64 `json:"space"`
	NearestDefName string  `json:"nearest_defender"`
}

const sideMargin = 2.0 // m: how far inside the block's width a player must stand

// betweenLines does not rely on the clustered lines, which a stretched
// midfield can blur. An attacker is between the lines when at least two
// outfield defenders are already goal-side of the ball behind him (bypassed),
// at least three are still in front of goal, and he stands inside the width
// of that remaining block rather than out on the touchline.
func betweenLines(s Scenario) []BetweenLines {
	var out []BetweenLines
	for _, a := range s.Attackers {
		var bypassed, behind []Player
		for _, d := range s.Defenders {
			if d.IsGK() {
				continue
			}
			switch {
			case d.X < a.X-1:
				bypassed = append(bypassed, d)
			case d.X > a.X+1:
				behind = append(behind, d)
			}
		}
		if len(bypassed) < 2 || len(behind) < 3 {
			continue
		}
		minY, maxY := math.Inf(1), math.Inf(-1)
		for _, d := range behind {
			minY, maxY = math.Min(minY, d.Y), math.Max(maxY, d.Y)
		}
		if a.Y < minY+sideMargin || a.Y > maxY-sideMargin {
			continue
		}
		byDist := func(ps []Player) []string {
			sort.SliceStable(ps, func(i, j int) bool {
				return dist(ps[i].X, ps[i].Y, a.X, a.Y) < dist(ps[j].X, ps[j].Y, a.X, a.Y)
			})
			names := make([]string, len(ps))
			for i, p := range ps {
				names[i] = p.Name
			}
			return names
		}
		near, d := nearestDefender(s, a.X, a.Y)
		out = append(out, BetweenLines{
			Player: a.ID, Name: a.Name, Bypassed: byDist(bypassed), Behind: byDist(behind),
			Space: round2(d), NearestDefName: near.Name,
		})
	}
	return out
}
