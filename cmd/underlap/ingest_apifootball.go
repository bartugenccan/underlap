package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"strconv"

	"github.com/bartugenccan/underlap/internal/apifootball"
	"github.com/bartugenccan/underlap/internal/store"
)

// ingestAPIFootball loads every cached match into the database. It never
// touches the network, so it can be re-run freely.
func ingestAPIFootball(ctx context.Context, args []string) error {
	fs := flag.NewFlagSet("ingest apifootball", flag.ContinueOnError)
	seasons := fs.String("seasons", "2024,2023,2022", "API season years")
	cacheDir := fs.String("cache", defaultAPICache, "directory of cached raw responses")
	if err := fs.Parse(args); err != nil {
		return err
	}
	st, err := store.Open(ctx, dsn())
	if err != nil {
		return err
	}
	defer st.Close()

	cache := apifootball.Cache{Dir: *cacheDir}
	var ok, failed, awarded int
	for _, s := range seasonList(*seasons) {
		year, err := strconv.Atoi(s)
		if err != nil {
			return fmt.Errorf("season %q: want a year like 2024", s)
		}
		ids, err := cache.CachedFixtureIDs(year)
		if err != nil {
			return err
		}
		for _, id := range ids {
			f, err := cache.Fixture(year, id)
			var res store.APIIngestResult
			if err == nil {
				var d apifootball.MatchDetail
				if d, err = apifootball.Normalize(f); err == nil {
					res, err = st.UpsertAPIFootball(ctx, d)
				}
			}
			if err != nil {
				fmt.Fprintln(os.Stderr, err)
				failed++
				continue
			}
			if res.Awarded {
				awarded++
			}
			if res.Mismatch != "" {
				fmt.Fprintln(os.Stderr, "warning:", res.Mismatch)
			}
			ok++
		}
	}
	fmt.Printf("ingested %d matches (%d found to be awarded), %d failed\n", ok, awarded, failed)
	if failed > 0 {
		return fmt.Errorf("%d matches failed to ingest", failed)
	}
	return nil
}
