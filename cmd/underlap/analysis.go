package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"

	"github.com/bartugenccan/underlap/internal/analysis"
	"github.com/bartugenccan/underlap/internal/store"
)

func analysisRealism(ctx context.Context, args []string) error {
	fs := flag.NewFlagSet("analysis realism", flag.ContinueOnError)
	out := fs.String("out", "data/reference/realism.json", "where to write the reference JSON (empty: don't)")
	if err := fs.Parse(args); err != nil {
		return err
	}
	st, err := store.Open(ctx, dsn())
	if err != nil {
		return err
	}
	defer st.Close()
	r, err := analysis.LeagueRealism(ctx, st.Pool())
	if err != nil {
		return err
	}
	analysis.WriteRealism(os.Stdout, r)
	return writeJSON(*out, r)
}

func analysisTeamProfile(ctx context.Context, args []string) error {
	fs := flag.NewFlagSet("analysis team-profile", flag.ContinueOnError)
	team := fs.String("team", "Galatasaray", "canonical team name")
	season := fs.String("season", "2425", "season code")
	out := fs.String("json", "", "also write the profile as JSON to this path")
	if err := fs.Parse(args); err != nil {
		return err
	}
	st, err := store.Open(ctx, dsn())
	if err != nil {
		return err
	}
	defer st.Close()
	p, err := analysis.Profile(ctx, st.Pool(), *team, *season)
	if err != nil {
		return err
	}
	if err := analysis.WriteProfile(os.Stdout, p); err != nil {
		return err
	}
	return writeJSON(*out, p)
}

func writeJSON(path string, v any) error {
	if path == "" {
		return nil
	}
	b, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	if err := os.WriteFile(path, append(b, '\n'), 0o644); err != nil {
		return err
	}
	fmt.Println("wrote", path)
	return nil
}
