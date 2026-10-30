// Command underlap is the single entry point for ingestion and model runs.
//
//	underlap migrate
//	underlap fetch footballdata  [--seasons 1718,...,2627] [--out data/raw/footballdata]
//	underlap ingest footballdata [--seasons ...] [--dir data/raw/footballdata]
//	underlap baseline backtest   [--eval-from 2223] [--csv-dir DIR]
//	underlap fetch apifootball   [--seasons 2024,2023,2022] [--budget 90] [--priority-team Galatasaray]
//	underlap ingest apifootball  [--seasons 2024,2023,2022]
//	underlap analysis realism    [--out data/reference/realism.json]
//	underlap analysis team-profile --team Galatasaray --season 2425
//
// DATABASE_URL overrides the default local docker-compose database.
// API_FOOTBALL_KEY is read from the environment or a .env file in the working
// directory.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"time"

	"github.com/bartugenccan/underlap/internal/baseline"
	"github.com/bartugenccan/underlap/internal/footballdata"
	"github.com/bartugenccan/underlap/internal/store"
	"github.com/bartugenccan/underlap/internal/teams"
)

const (
	defaultDSN     = "postgres://underlap:underlap@localhost:54320/underlap?sslmode=disable"
	defaultRawDir  = "data/raw/footballdata"
	defaultSeasons = "1718,1819,1920,2021,2122,2223,2324,2425,2526,2627"
)

const usage = `usage:
  underlap migrate
  underlap fetch footballdata  [--seasons LIST] [--out DIR]
  underlap ingest footballdata [--seasons LIST] [--dir DIR]
  underlap baseline backtest   [--eval-from SEASON] [--csv-dir DIR]
  underlap fetch apifootball   [--seasons LIST] [--budget N] [--priority-team NAME] [--cache DIR]
  underlap ingest apifootball  [--seasons LIST] [--cache DIR]
  underlap phase analyze       --file SCENARIO.json [--json]
  underlap serve               [--addr localhost:8080] [--origin http://localhost:3000]
  underlap analysis realism    [--out FILE]
  underlap analysis team-profile [--team NAME] [--season CODE] [--json FILE]`

var errUsage = errors.New(usage)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()

	if err := loadDotEnv(".env"); err != nil {
		fmt.Fprintln(os.Stderr, "read .env:", err)
		os.Exit(1)
	}
	if err := run(ctx, os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, err)
		if errors.Is(err, errUsage) {
			os.Exit(2)
		}
		os.Exit(1)
	}
}

func run(ctx context.Context, args []string) error {
	cmd := strings.Join(head(args, 2), " ")
	switch {
	case len(args) >= 1 && args[0] == "migrate":
		return store.Migrate(ctx, dsn())
	case cmd == "fetch footballdata":
		return fetchFootballData(ctx, args[2:])
	case cmd == "ingest footballdata":
		return ingestFootballData(ctx, args[2:])
	case cmd == "baseline backtest":
		return backtest(ctx, args[2:])
	case len(args) >= 1 && args[0] == "serve":
		return serve(ctx, args[1:])
	case cmd == "phase analyze":
		return phaseAnalyze(ctx, args[2:])
	case cmd == "analysis realism":
		return analysisRealism(ctx, args[2:])
	case cmd == "analysis team-profile":
		return analysisTeamProfile(ctx, args[2:])
	case cmd == "ingest apifootball":
		return ingestAPIFootball(ctx, args[2:])
	case cmd == "fetch apifootball":
		return fetchAPIFootball(ctx, args[2:])
	default:
		return errUsage
	}
}

func head(s []string, n int) []string {
	if len(s) < n {
		return s
	}
	return s[:n]
}

func dsn() string {
	if v := os.Getenv("DATABASE_URL"); v != "" {
		return v
	}
	return defaultDSN
}

func seasonList(s string) []string {
	var out []string
	for _, p := range strings.Split(s, ",") {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, p)
		}
	}
	return out
}

func fetchFootballData(ctx context.Context, args []string) error {
	fs := flag.NewFlagSet("fetch footballdata", flag.ContinueOnError)
	seasons := fs.String("seasons", defaultSeasons, "comma-separated season codes")
	out := fs.String("out", defaultRawDir, "directory to write T1_<season>.csv files into")
	if err := fs.Parse(args); err != nil {
		return err
	}
	// Keep going past a failed season so one bad file does not cost the rest;
	// still exit non-zero so the failure is visible.
	client := &http.Client{Timeout: 30 * time.Second}
	var failed []string
	for _, s := range seasonList(*seasons) {
		if err := footballdata.Fetch(ctx, client, s, *out); err != nil {
			fmt.Fprintln(os.Stderr, err)
			failed = append(failed, s)
			continue
		}
		fmt.Printf("fetched %s\n", footballdata.FileName(s))
	}
	if len(failed) > 0 {
		return fmt.Errorf("failed seasons: %s", strings.Join(failed, ","))
	}
	return nil
}

func ingestFootballData(ctx context.Context, args []string) error {
	fs := flag.NewFlagSet("ingest footballdata", flag.ContinueOnError)
	seasons := fs.String("seasons", "", "comma-separated season codes (default: every file in --dir)")
	dir := fs.String("dir", defaultRawDir, "directory holding T1_<season>.csv files")
	if err := fs.Parse(args); err != nil {
		return err
	}
	matches, err := footballdata.LoadDir(*dir, seasonList(*seasons))
	if err != nil {
		return err
	}
	st, err := store.Open(ctx, dsn())
	if err != nil {
		return err
	}
	defer st.Close()

	stats, err := st.UpsertFootballData(ctx, matches)
	if err != nil {
		return err
	}
	fmt.Printf("upserted %d matches (%d played, %d awarded/forfeit), %d odds rows\n",
		stats.Matches, stats.Played, stats.Awarded, stats.OddsRows)
	return nil
}

func backtest(ctx context.Context, args []string) error {
	fs := flag.NewFlagSet("baseline backtest", flag.ContinueOnError)
	evalFrom := fs.String("eval-from", "2223", "first season to score; earlier seasons only train")
	csvDir := fs.String("csv-dir", "", "read football-data CSVs from this directory instead of the database")
	if err := fs.Parse(args); err != nil {
		return err
	}

	var matches []baseline.Match
	if *csvDir != "" {
		raw, err := footballdata.LoadDir(*csvDir, nil)
		if err != nil {
			return err
		}
		if matches, err = toBaseline(raw); err != nil {
			return err
		}
	} else {
		st, err := store.Open(ctx, dsn())
		if err != nil {
			return err
		}
		defer st.Close()
		if matches, err = st.BaselineMatches(ctx); err != nil {
			return err
		}
	}
	if len(matches) == 0 {
		return errors.New("no played matches found; run `underlap ingest footballdata` first")
	}

	models := []baseline.Model{baseline.Uniform{}, baseline.NewBaseRates(), baseline.NewElo(), baseline.NewPoisson()}
	reports := baseline.Backtest(matches, models, *evalFrom)
	if len(reports) == 0 {
		return fmt.Errorf("nothing to score from season %s on (no matches with odds?)", *evalFrom)
	}
	return baseline.WriteReports(os.Stdout, reports)
}

// toBaseline converts parsed CSV rows with the same canonical team names,
// forfeit filter and odds preference as the database path, so both backtest
// paths score identically.
func toBaseline(raw []footballdata.Match) ([]baseline.Match, error) {
	rank := map[string]int{}
	for i, k := range baseline.OddsPreference {
		rank[k] = i + 1
	}
	var out []baseline.Match
	for _, m := range raw {
		if !m.Played() || m.Awarded() {
			continue
		}
		home, err := teams.Canonical(footballdata.Source, m.Home)
		if err != nil {
			return nil, err
		}
		away, err := teams.Canonical(footballdata.Source, m.Away)
		if err != nil {
			return nil, err
		}
		bm := baseline.Match{Season: m.Season, Kickoff: m.Kickoff, Home: home, Away: away,
			HomeGoals: *m.HomeGoals, AwayGoals: *m.AwayGoals}
		best := 0
		for _, o := range m.Odds {
			if r := rank[o.Bookmaker+"/"+o.Kind]; r > 0 && (best == 0 || r < best) {
				best = r
				p := baseline.ImpliedProbs(o.Home, o.Draw, o.Away)
				bm.Market = &p
			}
		}
		out = append(out, bm)
	}
	return out, nil
}
