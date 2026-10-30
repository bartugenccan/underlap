package store

import (
	"context"
	"fmt"

	"github.com/bartugenccan/underlap/internal/footballdata"
)

// IngestStats says what an ingest run touched.
type IngestStats struct {
	Matches, Played, OddsRows int
}

// UpsertFootballData writes parsed football-data matches in one transaction.
// Re-running it is safe: every insert is an upsert keyed on natural keys, so a
// refreshed CSV only fills in new results and prices.
func (s *Store) UpsertFootballData(ctx context.Context, matches []footballdata.Match) (IngestStats, error) {
	var st IngestStats
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return st, err
	}
	defer tx.Rollback(ctx)

	r := newResolver(tx, footballdata.Source)
	seasons := map[string]int{}

	for _, m := range matches {
		seasonID, ok := seasons[m.Season]
		if !ok {
			name, year, err := footballdata.SeasonName(m.Season)
			if err != nil {
				return st, err
			}
			err = tx.QueryRow(ctx,
				`INSERT INTO seasons (code, name, start_year) VALUES ($1, $2, $3)
				 ON CONFLICT (code) DO UPDATE SET name = EXCLUDED.name
				 RETURNING id`, m.Season, name, year).Scan(&seasonID)
			if err != nil {
				return st, fmt.Errorf("season %s: %w", m.Season, err)
			}
			seasons[m.Season] = seasonID
		}

		home, err := r.team(ctx, m.Home)
		if err != nil {
			return st, err
		}
		away, err := r.team(ctx, m.Away)
		if err != nil {
			return st, err
		}
		referee, err := r.referee(ctx, m.Referee)
		if err != nil {
			return st, err
		}

		var matchID int
		err = tx.QueryRow(ctx, `
			INSERT INTO matches (season_id, kickoff_at, home_team_id, away_team_id,
			                     home_goals, away_goals, ht_home_goals, ht_away_goals, referee_id)
			VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)
			ON CONFLICT (season_id, home_team_id, away_team_id) DO UPDATE SET
				kickoff_at    = EXCLUDED.kickoff_at,
				home_goals    = EXCLUDED.home_goals,
				away_goals    = EXCLUDED.away_goals,
				ht_home_goals = EXCLUDED.ht_home_goals,
				ht_away_goals = EXCLUDED.ht_away_goals,
				referee_id    = COALESCE(EXCLUDED.referee_id, matches.referee_id)
			RETURNING id`,
			seasonID, m.Kickoff, home, away, m.HomeGoals, m.AwayGoals, m.HTHome, m.HTAway, referee).Scan(&matchID)
		if err != nil {
			return st, fmt.Errorf("match %s %s-%s: %w", m.Season, m.Home, m.Away, err)
		}
		st.Matches++
		if m.Played() {
			st.Played++
		}

		_, err = tx.Exec(ctx,
			`INSERT INTO match_sources (source, external_id, match_id) VALUES ($1, $2, $3)
			 ON CONFLICT DO NOTHING`,
			footballdata.Source, m.Season+":"+m.Home+":"+m.Away, matchID)
		if err != nil {
			return st, err
		}

		for _, side := range []struct {
			team  int
			stats footballdata.TeamStats
		}{{home, m.HomeStats}, {away, m.AwayStats}} {
			ts := side.stats
			if ts.Shots == nil && ts.Corners == nil && ts.Yellow == nil {
				continue // this season's file has no match stats
			}
			_, err = tx.Exec(ctx, `
				INSERT INTO team_match_stats (match_id, team_id, source, shots, shots_on_target,
				                              corners, fouls, yellow_cards, red_cards)
				VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)
				ON CONFLICT (match_id, team_id, source) DO UPDATE SET
					shots = EXCLUDED.shots, shots_on_target = EXCLUDED.shots_on_target,
					corners = EXCLUDED.corners, fouls = EXCLUDED.fouls,
					yellow_cards = EXCLUDED.yellow_cards, red_cards = EXCLUDED.red_cards`,
				matchID, side.team, footballdata.Source,
				ts.Shots, ts.ShotsOnTarget, ts.Corners, ts.Fouls, ts.Yellow, ts.Red)
			if err != nil {
				return st, err
			}
		}

		for _, o := range m.Odds {
			_, err = tx.Exec(ctx, `
				INSERT INTO odds (match_id, bookmaker, kind, home, draw, away, source)
				VALUES ($1, $2, $3, $4, $5, $6, $7)
				ON CONFLICT (match_id, bookmaker, kind, market) DO UPDATE SET
					home = EXCLUDED.home, draw = EXCLUDED.draw, away = EXCLUDED.away`,
				matchID, o.Bookmaker, o.Kind, o.Home, o.Draw, o.Away, footballdata.Source)
			if err != nil {
				return st, err
			}
			st.OddsRows++
		}
	}
	return st, tx.Commit(ctx)
}
