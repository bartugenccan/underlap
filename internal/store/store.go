// Package store is the Postgres layer: migrations, ingestion upserts and the
// read queries the models need.
package store

import (
	"context"
	"database/sql"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	_ "github.com/jackc/pgx/v5/stdlib" // database/sql driver for goose
	"github.com/pressly/goose/v3"

	"github.com/bartugenccan/underlap/db"
	"github.com/bartugenccan/underlap/internal/baseline"
	"github.com/bartugenccan/underlap/internal/teams"
)

type Store struct{ pool *pgxpool.Pool }

func Open(ctx context.Context, dsn string) (*Store, error) {
	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		return nil, err
	}
	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		return nil, fmt.Errorf("connect to postgres (is `docker compose up -d` running?): %w", err)
	}
	return &Store{pool: pool}, nil
}

func (s *Store) Close() { s.pool.Close() }

// Pool exposes the connection pool to read-only analysis queries.
func (s *Store) Pool() *pgxpool.Pool { return s.pool }

// Migrate applies every pending migration embedded in the db package.
func Migrate(ctx context.Context, dsn string) error {
	sqlDB, err := sql.Open("pgx", dsn)
	if err != nil {
		return err
	}
	defer sqlDB.Close()
	goose.SetBaseFS(db.Migrations)
	if err := goose.SetDialect("postgres"); err != nil {
		return err
	}
	return goose.UpContext(ctx, sqlDB, "migrations")
}

// resolver turns source-specific names into ids, creating rows on first sight
// and caching them for the rest of the transaction.
type resolver struct {
	tx       pgx.Tx
	source   string
	teams    map[string]int
	referees map[string]int
}

func newResolver(tx pgx.Tx, source string) *resolver {
	return &resolver{tx: tx, source: source, teams: map[string]int{}, referees: map[string]int{}}
}

// team maps a source's spelling to the canonical team row via the curated
// list in internal/teams, so every source lands on the same row. Unknown
// names are an error, never a new team.
func (r *resolver) team(ctx context.Context, alias string) (int, error) {
	if id, ok := r.teams[alias]; ok {
		return id, nil
	}
	name, err := teams.Canonical(r.source, alias)
	if err != nil {
		return 0, err
	}
	var id int
	err = r.tx.QueryRow(ctx,
		`INSERT INTO teams (name) VALUES ($1)
		 ON CONFLICT (name) DO UPDATE SET name = EXCLUDED.name
		 RETURNING id`, name).Scan(&id)
	if err == nil {
		_, err = r.tx.Exec(ctx,
			`INSERT INTO team_aliases (source, alias, team_id) VALUES ($1, $2, $3)
			 ON CONFLICT (source, alias) DO NOTHING`, r.source, alias, id)
	}
	if err != nil {
		return 0, fmt.Errorf("resolve team %q: %w", alias, err)
	}
	r.teams[alias] = id
	return id, nil
}

// referee returns nil for an empty name so the column stays NULL.
func (r *resolver) referee(ctx context.Context, name string) (*int, error) {
	if name == "" {
		return nil, nil
	}
	if id, ok := r.referees[name]; ok {
		return &id, nil
	}
	var id int
	err := r.tx.QueryRow(ctx,
		`INSERT INTO referees (name) VALUES ($1)
		 ON CONFLICT (name) DO UPDATE SET name = EXCLUDED.name
		 RETURNING id`, name).Scan(&id)
	if err != nil {
		return nil, fmt.Errorf("resolve referee %q: %w", name, err)
	}
	r.referees[name] = id
	return &id, nil
}

// BaselineMatches returns every match actually played (forfeits excluded) with
// the preferred market probabilities (see baseline.OddsPreference), oldest
// first.
func (s *Store) BaselineMatches(ctx context.Context) ([]baseline.Match, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT s.code, m.kickoff_at, ht.name, at.name, m.home_goals, m.away_goals,
		       o.home, o.draw, o.away
		FROM matches m
		JOIN seasons s ON s.id = m.season_id
		JOIN teams ht ON ht.id = m.home_team_id
		JOIN teams at ON at.id = m.away_team_id
		LEFT JOIN LATERAL (
			SELECT home, draw, away
			FROM odds
			WHERE match_id = m.id AND market = '1x2'
			  AND array_position($1::text[], bookmaker || '/' || kind) IS NOT NULL
			ORDER BY array_position($1::text[], bookmaker || '/' || kind)
			LIMIT 1
		) o ON true
		WHERE m.home_goals IS NOT NULL AND m.away_goals IS NOT NULL AND NOT m.awarded
		ORDER BY m.kickoff_at`, baseline.OddsPreference)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []baseline.Match
	for rows.Next() {
		var m baseline.Match
		var oh, od, oa *float64
		if err := rows.Scan(&m.Season, &m.Kickoff, &m.Home, &m.Away, &m.HomeGoals, &m.AwayGoals, &oh, &od, &oa); err != nil {
			return nil, err
		}
		if oh != nil && od != nil && oa != nil {
			p := baseline.ImpliedProbs(*oh, *od, *oa)
			m.Market = &p
		}
		out = append(out, m)
	}
	return out, rows.Err()
}
