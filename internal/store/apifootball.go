package store

import (
	"context"
	"errors"
	"fmt"
	"strconv"

	"github.com/jackc/pgx/v5"

	"github.com/bartugenccan/underlap/internal/apifootball"
)

// APIIngestResult reports what UpsertAPIFootball noticed about a match.
type APIIngestResult struct {
	// Awarded is set when the goal events disagree with an official 3-0: the
	// match was abandoned and awarded (e.g. a team walked off), so its score
	// says nothing about the football played.
	Awarded bool
	// Mismatch describes any other disagreement between events and score.
	Mismatch string
}

// UpsertAPIFootball writes one match's details in its own transaction. The
// match itself must already exist (football-data covers every season the free
// API plan does); this only attaches lineups, events and statistics to it.
// Re-running is safe: rows are upserted and events replaced.
func (s *Store) UpsertAPIFootball(ctx context.Context, d apifootball.MatchDetail) (APIIngestResult, error) {
	var res APIIngestResult
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return res, err
	}
	defer tx.Rollback(ctx)

	r := newResolver(tx, apifootball.Source)
	homeID, err := r.team(ctx, d.Home.Name)
	if err != nil {
		return res, err
	}
	awayID, err := r.team(ctx, d.Away.Name)
	if err != nil {
		return res, err
	}

	var matchID int
	var homeGoals, awayGoals *int
	err = tx.QueryRow(ctx, `
		SELECT m.id, m.home_goals, m.away_goals FROM matches m JOIN seasons s ON s.id = m.season_id
		WHERE s.code = $1 AND m.home_team_id = $2 AND m.away_team_id = $3`,
		d.SeasonCode, homeID, awayID).Scan(&matchID, &homeGoals, &awayGoals)
	if errors.Is(err, pgx.ErrNoRows) {
		return res, fmt.Errorf("fixture %d (%s %s-%s): no such match; ingest football-data first",
			d.FixtureID, d.SeasonCode, d.Home.Name, d.Away.Name)
	}
	if err != nil {
		return res, err
	}

	// The API files own goals under the team that benefits, so counting goal
	// events per team gives the score directly.
	evHome, evAway := 0, 0
	for _, e := range d.Events {
		if e.Type != "goal" && e.Type != "penalty_goal" && e.Type != "own_goal" {
			continue
		}
		if e.TeamID == d.Home.ID {
			evHome++
		} else {
			evAway++
		}
	}
	if homeGoals != nil && awayGoals != nil && (evHome != *homeGoals || evAway != *awayGoals) {
		h, a := *homeGoals, *awayGoals
		if (h == 3 && a == 0) || (h == 0 && a == 3) {
			res.Awarded = true
			if _, err = tx.Exec(ctx, `UPDATE matches SET awarded = true WHERE id = $1`, matchID); err != nil {
				return res, err
			}
		} else {
			res.Mismatch = fmt.Sprintf("fixture %d %s %d-%d %s: goal events add up to %d-%d",
				d.FixtureID, d.Home.Name, h, a, d.Away.Name, evHome, evAway)
		}
	}

	if _, err = tx.Exec(ctx,
		`INSERT INTO match_sources (source, external_id, match_id) VALUES ($1, $2, $3)
		 ON CONFLICT DO NOTHING`, apifootball.Source, strconv.Itoa(d.FixtureID), matchID); err != nil {
		return res, err
	}
	referee, err := r.referee(ctx, d.Referee)
	if err != nil {
		return res, err
	}
	// football-data has no referee column for Süper Lig, so fill the gaps.
	if _, err = tx.Exec(ctx,
		`UPDATE matches SET referee_id = COALESCE(referee_id, $2), venue = COALESCE(venue, NULLIF($3, ''))
		 WHERE id = $1`, matchID, referee, d.Venue); err != nil {
		return res, err
	}

	playerIDs := map[int]int{} // API player id -> players.id
	for _, side := range []struct {
		team   int
		detail apifootball.TeamDetail
	}{{homeID, d.Home}, {awayID, d.Away}} {
		if err := upsertTeamDetail(ctx, tx, matchID, side.team, side.detail, playerIDs); err != nil {
			return res, fmt.Errorf("fixture %d %s: %w", d.FixtureID, side.detail.Name, err)
		}
	}

	if _, err = tx.Exec(ctx, `DELETE FROM match_events WHERE match_id = $1 AND source = $2`,
		matchID, apifootball.Source); err != nil {
		return res, err
	}
	teamIDs := map[int]int{d.Home.ID: homeID, d.Away.ID: awayID}
	for _, e := range d.Events {
		teamID, ok := teamIDs[e.TeamID]
		if !ok {
			return res, fmt.Errorf("fixture %d: event for unknown team id %d", d.FixtureID, e.TeamID)
		}
		_, err = tx.Exec(ctx, `
			INSERT INTO match_events (match_id, minute, extra_minute, team_id, player_id,
			                          related_player_id, type, detail, source)
			VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)`,
			matchID, e.Minute, e.Extra, teamID, lookup(playerIDs, e.PlayerID), lookup(playerIDs, e.RelatedID),
			e.Type, e.Detail, apifootball.Source)
		if err != nil {
			return res, err
		}
	}
	return res, tx.Commit(ctx)
}

// lookup maps an optional API player id to our id; players not in the squad
// lists (rare) stay NULL rather than inventing a row.
func lookup(ids map[int]int, apiID *int) *int {
	if apiID == nil {
		return nil
	}
	if id, ok := ids[*apiID]; ok {
		return &id
	}
	return nil
}

func upsertTeamDetail(ctx context.Context, tx pgx.Tx, matchID, teamID int, t apifootball.TeamDetail, playerIDs map[int]int) error {
	if t.Formation != "" {
		if _, err := tx.Exec(ctx, `
			INSERT INTO team_match_shapes (match_id, team_id, phase, formation, source)
			VALUES ($1, $2, 'nominal', $3, $4)
			ON CONFLICT (match_id, team_id, phase, source) DO UPDATE SET formation = EXCLUDED.formation`,
			matchID, teamID, t.Formation, apifootball.Source); err != nil {
			return err
		}
	}

	if t.CoachID != nil {
		coachID, err := externalID(ctx, tx, "coach_aliases", "coaches", "coach_id", *t.CoachID, t.CoachName)
		if err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `
			INSERT INTO match_coaches (match_id, team_id, coach_id) VALUES ($1, $2, $3)
			ON CONFLICT (match_id, team_id) DO UPDATE SET coach_id = EXCLUDED.coach_id`,
			matchID, teamID, coachID); err != nil {
			return err
		}
	}

	st := t.Stats
	if _, err := tx.Exec(ctx, `
		INSERT INTO team_match_stats (match_id, team_id, source, shots, shots_on_target, shots_inside_box,
		    corners, fouls, yellow_cards, red_cards, possession, xg, passes, passes_accurate, saves, goals_prevented)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $16)
		ON CONFLICT (match_id, team_id, source) DO UPDATE SET
		    shots = EXCLUDED.shots, shots_on_target = EXCLUDED.shots_on_target,
		    shots_inside_box = EXCLUDED.shots_inside_box, corners = EXCLUDED.corners,
		    fouls = EXCLUDED.fouls, yellow_cards = EXCLUDED.yellow_cards, red_cards = EXCLUDED.red_cards,
		    possession = EXCLUDED.possession, xg = EXCLUDED.xg, passes = EXCLUDED.passes,
		    passes_accurate = EXCLUDED.passes_accurate, saves = EXCLUDED.saves,
		    goals_prevented = EXCLUDED.goals_prevented`,
		matchID, teamID, apifootball.Source, st.Shots, st.ShotsOnTarget, st.ShotsInsideBox,
		st.Corners, st.Fouls, st.Yellow, st.Red, st.Possession, st.XG, st.Passes, st.PassesAccurate,
		st.Saves, st.GoalsPrevented); err != nil {
		return err
	}

	for _, p := range t.Players {
		pid, err := externalID(ctx, tx, "player_aliases", "players", "player_id", p.ID, p.Name)
		if err != nil {
			return err
		}
		playerIDs[p.ID] = pid

		minutes := p.Minutes
		if minutes == nil && !p.Started && p.MinuteOn == nil {
			zero := 0 // unused substitute: in the squad, did not play
			minutes = &zero
		}
		if _, err := tx.Exec(ctx, `
			INSERT INTO appearances (match_id, player_id, team_id, started, position, shirt_number,
			                         minute_on, minute_off, minutes_played, grid, captain)
			VALUES ($1, $2, $3, $4, NULLIF($5, ''), $6, $7, $8, $9, NULLIF($10, ''), $11)
			ON CONFLICT (match_id, player_id) DO UPDATE SET
			    team_id = EXCLUDED.team_id, started = EXCLUDED.started, position = EXCLUDED.position,
			    shirt_number = EXCLUDED.shirt_number, minute_on = EXCLUDED.minute_on,
			    minute_off = EXCLUDED.minute_off, minutes_played = EXCLUDED.minutes_played,
			    grid = EXCLUDED.grid, captain = EXCLUDED.captain`,
			matchID, pid, teamID, p.Started, p.Pos, p.Number, p.MinuteOn, p.MinuteOff, minutes, p.Grid, p.Captain); err != nil {
			return err
		}

		if p.Stats == nil || p.Minutes == nil {
			continue // no stats for players who did not play
		}
		s := p.Stats
		if _, err := tx.Exec(ctx, `
			INSERT INTO player_match_stats (match_id, player_id, source, tackles, interceptions,
			    passes, passes_accurate, key_passes, dribbles_attempted, dribbles_won,
			    shots, shots_on_target, goals, assists, rating, duels_total, duels_won,
			    fouls_committed, fouls_drawn, saves, goals_conceded, yellow_cards, red_cards)
			VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $16, $17, $18, $19, $20, $21, $22, $23)
			ON CONFLICT (match_id, player_id, source) DO UPDATE SET
			    tackles = EXCLUDED.tackles, interceptions = EXCLUDED.interceptions,
			    passes = EXCLUDED.passes, passes_accurate = EXCLUDED.passes_accurate,
			    key_passes = EXCLUDED.key_passes, dribbles_attempted = EXCLUDED.dribbles_attempted,
			    dribbles_won = EXCLUDED.dribbles_won, shots = EXCLUDED.shots,
			    shots_on_target = EXCLUDED.shots_on_target, goals = EXCLUDED.goals,
			    assists = EXCLUDED.assists, rating = EXCLUDED.rating,
			    duels_total = EXCLUDED.duels_total, duels_won = EXCLUDED.duels_won,
			    fouls_committed = EXCLUDED.fouls_committed, fouls_drawn = EXCLUDED.fouls_drawn,
			    saves = EXCLUDED.saves, goals_conceded = EXCLUDED.goals_conceded,
			    yellow_cards = EXCLUDED.yellow_cards, red_cards = EXCLUDED.red_cards`,
			matchID, pid, apifootball.Source, s.Tackles.Total, s.Tackles.Interceptions,
			s.Passes.Total, s.Passes.Accuracy.Int(), s.Passes.Key, s.Dribbles.Attempts, s.Dribbles.Success,
			s.Shots.Total, s.Shots.On, s.Goals.Total, s.Goals.Assists, s.Games.Rating.V,
			s.Duels.Total, s.Duels.Won, s.Fouls.Committed, s.Fouls.Drawn, s.Goals.Saves,
			s.Goals.Conceded, s.Cards.Yellow, s.Cards.Red); err != nil {
			return err
		}
	}
	return nil
}

// externalID resolves a source's numeric id for a person (player or coach) to
// our row, creating the row and the alias on first sight.
func externalID(ctx context.Context, tx pgx.Tx, aliasTable, table, idCol string, apiID int, name string) (int, error) {
	ext := strconv.Itoa(apiID)
	var id int
	err := tx.QueryRow(ctx,
		fmt.Sprintf(`SELECT %s FROM %s WHERE source = $1 AND external_id = $2`, idCol, aliasTable),
		apifootball.Source, ext).Scan(&id)
	if err == nil {
		return id, nil
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return 0, err
	}
	if err := tx.QueryRow(ctx, fmt.Sprintf(`INSERT INTO %s (name) VALUES ($1) RETURNING id`, table), name).Scan(&id); err != nil {
		return 0, err
	}
	_, err = tx.Exec(ctx,
		fmt.Sprintf(`INSERT INTO %s (source, external_id, %s) VALUES ($1, $2, $3)`, aliasTable, idCol),
		apifootball.Source, ext, id)
	return id, err
}
