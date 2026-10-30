package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"os"
	"strconv"

	"github.com/bartugenccan/underlap/internal/apifootball"
)

const defaultAPICache = "data/cache/apifootball"

func fetchAPIFootball(ctx context.Context, args []string) error {
	fs := flag.NewFlagSet("fetch apifootball", flag.ContinueOnError)
	seasons := fs.String("seasons", "2024,2023,2022", "API season years, newest first (free plan: 2022-2024)")
	budget := fs.Int("budget", 90, "most requests this run may make (free plan: 100 a day)")
	priority := fs.String("priority-team", "Galatasaray", "fetch this team's matches first (API team name)")
	cache := fs.String("cache", defaultAPICache, "directory for cached raw responses")
	if err := fs.Parse(args); err != nil {
		return err
	}
	key := os.Getenv("API_FOOTBALL_KEY")
	if key == "" {
		return errors.New("API_FOOTBALL_KEY is not set (environment or .env)")
	}
	var years []int
	for _, s := range seasonList(*seasons) {
		y, err := strconv.Atoi(s)
		if err != nil {
			return fmt.Errorf("season %q: want a year like 2024", s)
		}
		years = append(years, y)
	}

	b := &apifootball.Backfill{
		Client:       apifootball.NewClient(key),
		Cache:        apifootball.Cache{Dir: *cache},
		Budget:       *budget,
		PriorityTeam: *priority,
		Log:          os.Stdout,
	}
	res, err := b.Run(ctx, years)
	fmt.Printf("requests %d, matches cached %d, still pending %d\n", res.Requests, res.Fetched, res.Pending)
	return err
}
