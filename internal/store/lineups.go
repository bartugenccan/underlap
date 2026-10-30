package store

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/bartugenccan/underlap/internal/phase"
)

type MatchSummary struct {
	ID        int       `json:"id"`
	Season    string    `json:"season"`
	Kickoff   time.Time `json:"kickoff"`
	Home      string    `json:"home"`
	Away      string    `json:"away"`
	HomeGoals *int      `json:"home_goals"`
	AwayGoals *int      `json:"away_goals"`
	Awarded   bool      `json:"awarded"`
}

// Matches lists a team's matches in a season that have line-ups (API-Football
// details), oldest first.
func (s *Store) Matches(ctx context.Context, team, season string) ([]MatchSummary, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT m.id, se.code, m.kickoff_at, h.name, a.name, m.home_goals, m.away_goals, m.awarded
		FROM matches m
		JOIN seasons se ON se.id = m.season_id
		JOIN teams h ON h.id = m.home_team_id
		JOIN teams a ON a.id = m.away_team_id
		WHERE se.code = $2 AND $1 IN (h.name, a.name)
		  AND EXISTS (SELECT 1 FROM match_sources ms WHERE ms.match_id = m.id AND ms.source = 'apifootball')
		ORDER BY m.kickoff_at`, team, season)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []MatchSummary{}
	for rows.Next() {
		var m MatchSummary
		if err := rows.Scan(&m.ID, &m.Season, &m.Kickoff, &m.Home, &m.Away, &m.HomeGoals, &m.AwayGoals, &m.Awarded); err != nil {
			return nil, err
		}
		out = append(out, m)
	}
	return out, rows.Err()
}

type TeamLineup struct {
	Team      string               `json:"team"`
	Formation string               `json:"formation"`
	Coach     string               `json:"coach"`
	Starters  []phase.LineupPlayer `json:"starters"`
}

type MatchLineups struct {
	Match MatchSummary `json:"match"`
	Home  TeamLineup   `json:"home"`
	Away  TeamLineup   `json:"away"`
}

// ErrNotFound is returned when a match id does not exist.
var ErrNotFound = errors.New("not found")

// Lineups returns both starting XIs of a match with their grid slots.
func (s *Store) Lineups(ctx context.Context, matchID int) (*MatchLineups, error) {
	var ml MatchLineups
	var homeID, awayID int
	err := s.pool.QueryRow(ctx, `
		SELECT m.id, se.code, m.kickoff_at, h.name, a.name, m.home_goals, m.away_goals, m.awarded, h.id, a.id
		FROM matches m
		JOIN seasons se ON se.id = m.season_id
		JOIN teams h ON h.id = m.home_team_id
		JOIN teams a ON a.id = m.away_team_id
		WHERE m.id = $1`, matchID).Scan(&ml.Match.ID, &ml.Match.Season, &ml.Match.Kickoff, &ml.Match.Home,
		&ml.Match.Away, &ml.Match.HomeGoals, &ml.Match.AwayGoals, &ml.Match.Awarded, &homeID, &awayID)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	for _, side := range []struct {
		lineup *TeamLineup
		name   string
		teamID int
	}{{&ml.Home, ml.Match.Home, homeID}, {&ml.Away, ml.Match.Away, awayID}} {
		if err := s.teamLineup(ctx, matchID, side.teamID, side.lineup); err != nil {
			return nil, err
		}
		side.lineup.Team = side.name
	}
	if len(ml.Home.Starters) == 0 || len(ml.Away.Starters) == 0 {
		return nil, fmt.Errorf("match %d has no line-ups yet: %w", matchID, ErrNotFound)
	}
	return &ml, nil
}

func (s *Store) teamLineup(ctx context.Context, matchID, teamID int, l *TeamLineup) error {
	err := s.pool.QueryRow(ctx, `
		SELECT COALESCE((SELECT formation FROM team_match_shapes
		                 WHERE match_id = $1 AND team_id = $2 AND phase = 'nominal' LIMIT 1), ''),
		       COALESCE((SELECT c.name FROM match_coaches mc JOIN coaches c ON c.id = mc.coach_id
		                 WHERE mc.match_id = $1 AND mc.team_id = $2), '')`,
		matchID, teamID).Scan(&l.Formation, &l.Coach)
	if err != nil {
		return err
	}
	rows, err := s.pool.Query(ctx, `
		SELECT p.id, p.name, COALESCE(a.shirt_number, 0), COALESCE(a.position, ''), a.grid
		FROM appearances a JOIN players p ON p.id = a.player_id
		WHERE a.match_id = $1 AND a.team_id = $2 AND a.started AND a.grid IS NOT NULL
		ORDER BY split_part(a.grid, ':', 1)::int, split_part(a.grid, ':', 2)::int`, matchID, teamID)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var id int
		var p phase.LineupPlayer
		if err := rows.Scan(&id, &p.Name, &p.Number, &p.Pos, &p.Grid); err != nil {
			return err
		}
		p.ID = "p" + strconv.Itoa(id)
		l.Starters = append(l.Starters, p)
	}
	return rows.Err()
}
