package apifootball

import (
	"context"
	"fmt"
	"io"
	"net/url"
	"strconv"
	"strings"
)

// Backfill fills the cache within a daily request budget and picks up where
// the previous run stopped, so running it once a day completes the history.
type Backfill struct {
	Client *Client
	Cache  Cache
	// Budget is the most requests this run may make.
	Budget int
	// PriorityTeam's matches are fetched first across all seasons (matched
	// case-insensitively against the API team name).
	PriorityTeam string
	Log          io.Writer
}

type BackfillResult struct {
	Requests int
	Fetched  int
	Pending  int // finished matches still missing from the cache
}

type queued struct{ season, id int }

func (b *Backfill) Run(ctx context.Context, seasons []int) (BackfillResult, error) {
	var res BackfillResult
	var first, rest []queued

	for _, season := range seasons {
		list, ok, err := b.Cache.List(season)
		if err != nil {
			return res, err
		}
		if !ok {
			if res.Requests >= b.Budget {
				return res, fmt.Errorf("budget spent before the %d fixture list could be fetched", season)
			}
			body, err := b.Client.Get(ctx, "/fixtures", url.Values{
				"league": {strconv.Itoa(SuperLig)}, "season": {strconv.Itoa(season)}})
			res.Requests++
			if err != nil {
				return res, err
			}
			if err := b.Cache.SaveList(season, body); err != nil {
				return res, err
			}
			if list, err = decodeResponse[Fixture](body); err != nil {
				return res, err
			}
		}
		for _, f := range list {
			if !f.Finished() || b.Cache.HasFixture(season, f.Fixture.ID) {
				continue
			}
			q := queued{season, f.Fixture.ID}
			if b.isPriority(f) {
				first = append(first, q)
			} else {
				rest = append(rest, q)
			}
		}
	}

	queue := append(first, rest...)
	res.Pending = len(queue)
	for _, q := range queue {
		if res.Requests >= b.Budget {
			break
		}
		if b.Client.Remaining >= 0 && b.Client.Remaining <= 1 {
			fmt.Fprintln(b.Log, "daily quota exhausted; stopping")
			break
		}
		body, err := b.Client.Get(ctx, "/fixtures", url.Values{"id": {strconv.Itoa(q.id)}})
		res.Requests++
		if err != nil {
			return res, err
		}
		if err := b.Cache.SaveFixture(q.season, q.id, body); err != nil {
			return res, err
		}
		res.Fetched++
		res.Pending--
		fmt.Fprintf(b.Log, "cached %d/%d (quota left %d, %d pending)\n", q.season, q.id, b.Client.Remaining, res.Pending)
	}
	return res, nil
}

func (b *Backfill) isPriority(f Fixture) bool {
	if b.PriorityTeam == "" {
		return false
	}
	return strings.EqualFold(f.Teams.Home.Name, b.PriorityTeam) || strings.EqualFold(f.Teams.Away.Name, b.PriorityTeam)
}
