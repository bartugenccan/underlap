// Package analysis turns the database into the reference numbers the engines
// are tested against and the profiles analysts read.
package analysis

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strings"
	"text/tabwriter"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Windows are the 15-minute slices analysts talk about. Stoppage time stays
// with its half: 45+3' is in 31-45, 90+5' in 76-90.
var Windows = []string{"1-15", "16-30", "31-45", "46-60", "61-75", "76-90"}

type Record struct {
	Played, Won, Drawn, Lost, GoalsFor, GoalsAgainst int
}

type PlayerLine struct {
	Name      string   `json:"name"`
	Squad     int      `json:"in_squad"`
	Starts    int      `json:"starts"`
	Minutes   int      `json:"minutes"`
	Goals     int      `json:"goals"`
	Assists   int      `json:"assists"`
	AvgRating *float64 `json:"avg_rating"`
}

type TeamProfile struct {
	Team   string `json:"team"`
	Season string `json:"season"`
	// Overall comes from every played match; the rest only from matches with
	// API-Football details, counted in DetailMatches.
	Overall       Record         `json:"overall"`
	Home          Record         `json:"home"`
	Away          Record         `json:"away"`
	DetailMatches int            `json:"detail_matches"`
	GoalsFor      [6]int         `json:"goals_for_by_window"`
	GoalsAgainst  [6]int         `json:"goals_against_by_window"`
	Shots         int            `json:"shots"`
	ShotsOnTarget int            `json:"shots_on_target"`
	DetailGoals   int            `json:"detail_goals"`
	XGFor         float64        `json:"xg_for"`
	XGAgainst     float64        `json:"xg_against"`
	DetailGA      int            `json:"detail_goals_against"`
	Formations    map[string]int `json:"formations"`
	Coaches       map[string]int `json:"coaches"`
	Players       []PlayerLine   `json:"players"`
}

// Profile builds a team's season profile. team is the canonical name.
func Profile(ctx context.Context, db *pgxpool.Pool, team, season string) (*TeamProfile, error) {
	p := &TeamProfile{Team: team, Season: season, Formations: map[string]int{}, Coaches: map[string]int{}}

	var teamID, seasonID int
	err := db.QueryRow(ctx, `SELECT t.id, s.id FROM teams t, seasons s WHERE t.name = $1 AND s.code = $2`,
		team, season).Scan(&teamID, &seasonID)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, fmt.Errorf("no team %q or season %q (team names are canonical, e.g. Galatasaray, Fenerbahçe)", team, season)
	}
	if err != nil {
		return nil, err
	}

	// Results, forfeits excluded.
	rows, err := db.Query(ctx, `
		SELECT home_team_id = $1, home_goals, away_goals FROM matches
		WHERE season_id = $2 AND $1 IN (home_team_id, away_team_id)
		  AND home_goals IS NOT NULL AND NOT awarded`, teamID, seasonID)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var atHome bool
		var hg, ag int
		if err := rows.Scan(&atHome, &hg, &ag); err != nil {
			return nil, err
		}
		gf, ga := hg, ag
		side := &p.Home
		if !atHome {
			gf, ga, side = ag, hg, &p.Away
		}
		for _, r := range []*Record{&p.Overall, side} {
			r.Played++
			r.GoalsFor += gf
			r.GoalsAgainst += ga
			switch {
			case gf > ga:
				r.Won++
			case gf == ga:
				r.Drawn++
			default:
				r.Lost++
			}
		}
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	// The rest only uses matches with API details: detail = the team's matches
	// that have a source row from apifootball.
	const detail = `
		SELECT m.id FROM matches m
		JOIN match_sources ms ON ms.match_id = m.id AND ms.source = 'apifootball'
		WHERE m.season_id = $2 AND $1 IN (m.home_team_id, m.away_team_id) AND NOT m.awarded`

	if err := db.QueryRow(ctx, `SELECT count(*) FROM (`+detail+`) d`, teamID, seasonID).Scan(&p.DetailMatches); err != nil {
		return nil, err
	}

	// Goal events are filed under the team that scored (own goals included).
	rows, err = db.Query(ctx, `
		SELECT e.team_id = $1, e.minute FROM match_events e
		WHERE e.match_id IN (`+detail+`) AND e.source = 'apifootball'
		  AND e.type IN ('goal', 'penalty_goal', 'own_goal')`, teamID, seasonID)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var ours bool
		var minute int
		if err := rows.Scan(&ours, &minute); err != nil {
			return nil, err
		}
		w := window(minute)
		if ours {
			p.GoalsFor[w]++
			p.DetailGoals++
		} else {
			p.GoalsAgainst[w]++
			p.DetailGA++
		}
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	err = db.QueryRow(ctx, `
		SELECT COALESCE(sum(s.shots) FILTER (WHERE s.team_id = $1), 0),
		       COALESCE(sum(s.shots_on_target) FILTER (WHERE s.team_id = $1), 0),
		       COALESCE(sum(s.xg) FILTER (WHERE s.team_id = $1), 0),
		       COALESCE(sum(s.xg) FILTER (WHERE s.team_id <> $1), 0)
		FROM team_match_stats s
		WHERE s.source = 'apifootball' AND s.match_id IN (`+detail+`)`, teamID, seasonID).
		Scan(&p.Shots, &p.ShotsOnTarget, &p.XGFor, &p.XGAgainst)
	if err != nil {
		return nil, err
	}

	if err := countInto(ctx, db, p.Formations, `
		SELECT formation, count(*) FROM team_match_shapes
		WHERE team_id = $1 AND phase = 'nominal' AND match_id IN (`+detail+`)
		GROUP BY formation`, teamID, seasonID); err != nil {
		return nil, err
	}
	if err := countInto(ctx, db, p.Coaches, `
		SELECT c.name, count(*) FROM match_coaches mc JOIN coaches c ON c.id = mc.coach_id
		WHERE mc.team_id = $1 AND mc.match_id IN (`+detail+`)
		GROUP BY c.name`, teamID, seasonID); err != nil {
		return nil, err
	}

	rows, err = db.Query(ctx, `
		SELECT pl.name, count(*), count(*) FILTER (WHERE a.started),
		       COALESCE(sum(a.minutes_played), 0),
		       (SELECT count(*) FROM match_events e WHERE e.player_id = pl.id AND e.source = 'apifootball'
		          AND e.type IN ('goal', 'penalty_goal') AND e.match_id IN (`+detail+`)),
		       (SELECT count(*) FROM match_events e WHERE e.related_player_id = pl.id AND e.source = 'apifootball'
		          AND e.type IN ('goal', 'penalty_goal') AND e.match_id IN (`+detail+`)),
		       avg(ps.rating)
		FROM appearances a
		JOIN players pl ON pl.id = a.player_id
		LEFT JOIN player_match_stats ps ON ps.match_id = a.match_id AND ps.player_id = a.player_id
		     AND ps.source = 'apifootball'
		WHERE a.team_id = $1 AND a.match_id IN (`+detail+`)
		GROUP BY pl.id, pl.name
		HAVING sum(a.minutes_played) > 0
		ORDER BY sum(a.minutes_played) DESC`, teamID, seasonID)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var l PlayerLine
		if err := rows.Scan(&l.Name, &l.Squad, &l.Starts, &l.Minutes, &l.Goals, &l.Assists, &l.AvgRating); err != nil {
			return nil, err
		}
		p.Players = append(p.Players, l)
	}
	return p, rows.Err()
}

func window(minute int) int {
	w := (minute - 1) / 15
	if minute <= 45 && w > 2 {
		w = 2 // first-half stoppage time is reported as 45 + extra
	}
	if w > 5 {
		w = 5
	}
	if w < 0 {
		w = 0
	}
	return w
}

func countInto(ctx context.Context, db *pgxpool.Pool, m map[string]int, q string, args ...any) error {
	rows, err := db.Query(ctx, q, args...)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var k string
		var n int
		if err := rows.Scan(&k, &n); err != nil {
			return err
		}
		m[k] = n
	}
	return rows.Err()
}

// WriteProfile prints a profile as plain tables.
func WriteProfile(w io.Writer, p *TeamProfile) error {
	pct := func(a, b int) string {
		if b == 0 {
			return "-"
		}
		return fmt.Sprintf("%.1f%%", 100*float64(a)/float64(b))
	}
	rec := func(r Record) string {
		return fmt.Sprintf("%d played  %dW %dD %dL  goals %d-%d", r.Played, r.Won, r.Drawn, r.Lost, r.GoalsFor, r.GoalsAgainst)
	}
	fmt.Fprintf(w, "%s %s\n\n", p.Team, p.Season)
	fmt.Fprintf(w, "overall  %s\nhome     %s\naway     %s\n\n", rec(p.Overall), rec(p.Home), rec(p.Away))
	fmt.Fprintf(w, "details below from %d of %d matches (API-Football)\n\n", p.DetailMatches, p.Overall.Played)

	tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
	fmt.Fprintln(tw, "goals by minute\t"+strings.Join(Windows, "\t")+"\t")
	for _, row := range []struct {
		label string
		v     [6]int
	}{{"scored", p.GoalsFor}, {"conceded", p.GoalsAgainst}} {
		cells := make([]string, 6)
		for i, n := range row.v {
			cells[i] = fmt.Sprint(n)
		}
		fmt.Fprintln(tw, row.label+"\t"+strings.Join(cells, "\t")+"\t")
	}
	tw.Flush()

	fmt.Fprintf(w, "\nshots %d, on target %d (%s), goals per shot %s\n",
		p.Shots, p.ShotsOnTarget, pct(p.ShotsOnTarget, p.Shots), pct(p.DetailGoals, p.Shots))
	fmt.Fprintf(w, "xG for %.1f vs goals %d (%+.1f), xG against %.1f vs conceded %d (%+.1f)\n",
		p.XGFor, p.DetailGoals, float64(p.DetailGoals)-p.XGFor, p.XGAgainst, p.DetailGA, float64(p.DetailGA)-p.XGAgainst)
	fmt.Fprintf(w, "formations %s\ncoaches %s\n\n", sortedCounts(p.Formations), sortedCounts(p.Coaches))

	tw = tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
	fmt.Fprintln(tw, "player\tsquad\tstarts\tminutes\tgoals\tassists\trating\t")
	for _, l := range p.Players {
		rating := "-"
		if l.AvgRating != nil {
			rating = fmt.Sprintf("%.2f", *l.AvgRating)
		}
		fmt.Fprintf(tw, "%s\t%d\t%d\t%d\t%d\t%d\t%s\t\n", l.Name, l.Squad, l.Starts, l.Minutes, l.Goals, l.Assists, rating)
	}
	return tw.Flush()
}

func sortedCounts(m map[string]int) string {
	type kv struct {
		k string
		v int
	}
	var s []kv
	for k, v := range m {
		s = append(s, kv{k, v})
	}
	for i := range s { // tiny maps: insertion sort, most frequent first
		for j := i; j > 0 && (s[j].v > s[j-1].v || (s[j].v == s[j-1].v && s[j].k < s[j-1].k)); j-- {
			s[j], s[j-1] = s[j-1], s[j]
		}
	}
	parts := make([]string, len(s))
	for i, e := range s {
		parts[i] = fmt.Sprintf("%s (%d)", e.k, e.v)
	}
	return strings.Join(parts, ", ")
}
