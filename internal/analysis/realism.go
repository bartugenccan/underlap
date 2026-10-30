package analysis

import (
	"context"
	"fmt"
	"io"
	"sort"

	"github.com/jackc/pgx/v5/pgxpool"
)

// Realism holds league-wide reference numbers. A simulated season has to
// reproduce these before the engine can be trusted.
type Realism struct {
	Matches int `json:"matches"`
	// TotalGoals[i] is the share of matches with i goals; the last bucket is "7 or more".
	TotalGoals    [8]float64           `json:"total_goals_share"`
	MeanHomeGoals float64              `json:"mean_home_goals"`
	MeanAwayGoals float64              `json:"mean_away_goals"`
	HomeWin       float64              `json:"home_win"`
	Draw          float64              `json:"draw"`
	AwayWin       float64              `json:"away_win"`
	TopScorelines []Scoreline          `json:"top_scorelines"`
	PerTeamMatch  map[string]float64   `json:"per_team_match"` // shots, shots on target, corners, yellows
	BySeason      map[string]SeasonRow `json:"by_season"`
}

type Scoreline struct {
	Score string  `json:"score"`
	Share float64 `json:"share"`
}

type SeasonRow struct {
	Matches   int     `json:"matches"`
	GoalsPerM float64 `json:"goals_per_match"`
	HomeWin   float64 `json:"home_win"`
	Draw      float64 `json:"draw"`
	AwayWin   float64 `json:"away_win"`
}

// LeagueRealism computes the references from every played, non-awarded match.
func LeagueRealism(ctx context.Context, db *pgxpool.Pool) (*Realism, error) {
	rows, err := db.Query(ctx, `
		SELECT s.code, m.home_goals, m.away_goals FROM matches m JOIN seasons s ON s.id = m.season_id
		WHERE m.home_goals IS NOT NULL AND NOT m.awarded`)
	if err != nil {
		return nil, err
	}
	r := &Realism{PerTeamMatch: map[string]float64{}, BySeason: map[string]SeasonRow{}}
	scores := map[string]int{}
	type acc struct{ n, goals, h, d, a int }
	seasons := map[string]*acc{}
	var hg, ag, h, d, a int
	for rows.Next() {
		var code string
		var home, away int
		if err := rows.Scan(&code, &home, &away); err != nil {
			return nil, err
		}
		r.Matches++
		hg += home
		ag += away
		r.TotalGoals[min(home+away, 7)]++
		scores[fmt.Sprintf("%d-%d", home, away)]++
		s := seasons[code]
		if s == nil {
			s = &acc{}
			seasons[code] = s
		}
		s.n++
		s.goals += home + away
		switch {
		case home > away:
			h++
			s.h++
		case home == away:
			d++
			s.d++
		default:
			a++
			s.a++
		}
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if r.Matches == 0 {
		return nil, fmt.Errorf("no played matches; run `underlap ingest footballdata` first")
	}

	n := float64(r.Matches)
	for i := range r.TotalGoals {
		r.TotalGoals[i] /= n
	}
	r.MeanHomeGoals, r.MeanAwayGoals = float64(hg)/n, float64(ag)/n
	r.HomeWin, r.Draw, r.AwayWin = float64(h)/n, float64(d)/n, float64(a)/n
	for code, s := range seasons {
		sn := float64(s.n)
		r.BySeason[code] = SeasonRow{s.n, float64(s.goals) / sn, float64(s.h) / sn, float64(s.d) / sn, float64(s.a) / sn}
	}
	for score, c := range scores {
		r.TopScorelines = append(r.TopScorelines, Scoreline{score, float64(c) / n})
	}
	sort.Slice(r.TopScorelines, func(i, j int) bool {
		if r.TopScorelines[i].Share != r.TopScorelines[j].Share {
			return r.TopScorelines[i].Share > r.TopScorelines[j].Share
		}
		return r.TopScorelines[i].Score < r.TopScorelines[j].Score
	})
	r.TopScorelines = r.TopScorelines[:min(10, len(r.TopScorelines))]

	var shots, sot, corners, yellows *float64
	err = db.QueryRow(ctx, `
		SELECT avg(s.shots), avg(s.shots_on_target), avg(s.corners), avg(s.yellow_cards)
		FROM team_match_stats s JOIN matches m ON m.id = s.match_id
		WHERE s.source = 'footballdata' AND NOT m.awarded`).Scan(&shots, &sot, &corners, &yellows)
	if err != nil {
		return nil, err
	}
	for k, v := range map[string]*float64{"shots": shots, "shots_on_target": sot, "corners": corners, "yellow_cards": yellows} {
		if v != nil {
			r.PerTeamMatch[k] = *v
		}
	}
	return r, nil
}

func WriteRealism(w io.Writer, r *Realism) {
	fmt.Fprintf(w, "%d matches\n", r.Matches)
	fmt.Fprintf(w, "home win %.1f%%  draw %.1f%%  away win %.1f%%\n", 100*r.HomeWin, 100*r.Draw, 100*r.AwayWin)
	fmt.Fprintf(w, "goals per match %.2f (home %.2f, away %.2f)\n", r.MeanHomeGoals+r.MeanAwayGoals, r.MeanHomeGoals, r.MeanAwayGoals)
	fmt.Fprint(w, "total goals:")
	for i, s := range r.TotalGoals {
		label := fmt.Sprint(i)
		if i == 7 {
			label = "7+"
		}
		fmt.Fprintf(w, "  %s:%.1f%%", label, 100*s)
	}
	fmt.Fprint(w, "\ntop scorelines:")
	for _, s := range r.TopScorelines {
		fmt.Fprintf(w, "  %s %.1f%%", s.Score, 100*s.Share)
	}
	fmt.Fprintf(w, "\nper team per match: shots %.1f, on target %.1f, corners %.1f, yellow cards %.2f\n",
		r.PerTeamMatch["shots"], r.PerTeamMatch["shots_on_target"], r.PerTeamMatch["corners"], r.PerTeamMatch["yellow_cards"])
	var codes []string
	for c := range r.BySeason {
		codes = append(codes, c)
	}
	sort.Strings(codes)
	for _, c := range codes {
		s := r.BySeason[c]
		fmt.Fprintf(w, "  %s  %3d matches  %.2f goals  H %.1f%%  D %.1f%%  A %.1f%%\n",
			c, s.Matches, s.GoalsPerM, 100*s.HomeWin, 100*s.Draw, 100*s.AwayWin)
	}
}
