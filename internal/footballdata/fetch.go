package footballdata

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

const (
	baseURL   = "https://www.football-data.co.uk/mmz4281"
	userAgent = "underlap-data-fetch/1.0 (+https://github.com/bartugenccan/underlap)"
	attempts  = 4
)

// FileName is the on-disk name for a season file inside a raw data directory.
func FileName(season string) string { return "T1_" + season + ".csv" }

// Fetch downloads one season into dir. It retries server errors with backoff
// and refuses to write anything that is not a football-data CSV (see
// LooksLikeCSV).
func Fetch(ctx context.Context, client *http.Client, season, dir string) error {
	url := fmt.Sprintf("%s/%s/T1.csv", baseURL, season)
	var body []byte
	var err error
	for i := 0; i < attempts; i++ {
		if i > 0 {
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(time.Duration(1<<i) * 5 * time.Second): // 10s, 20s, 40s
			}
		}
		var retry bool
		body, retry, err = get(ctx, client, url)
		if err == nil || !retry {
			break
		}
	}
	if err != nil {
		return err
	}
	if !LooksLikeCSV(body) {
		return fmt.Errorf("fetch %s: response is not a football-data CSV (blocked network or changed URL?)", url)
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(dir, FileName(season)), body, 0o644)
}

// get performs one request. retry reports whether the failure is worth another
// attempt: network errors, 429 and 5xx are; anything else is not.
func get(ctx context.Context, client *http.Client, url string) (body []byte, retry bool, err error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, false, err
	}
	req.Header.Set("User-Agent", userAgent)
	resp, err := client.Do(req)
	if err != nil {
		return nil, true, fmt.Errorf("fetch %s: %w", url, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		retry = resp.StatusCode == http.StatusTooManyRequests || resp.StatusCode >= 500
		return nil, retry, fmt.Errorf("fetch %s: status %s", url, resp.Status)
	}
	body, err = io.ReadAll(resp.Body)
	if err != nil {
		return nil, true, fmt.Errorf("fetch %s: %w", url, err)
	}
	return body, false, nil
}

// LoadDir parses every T1_<season>.csv in dir, or only the given seasons when
// the list is non-empty. Matches come back in kick-off order.
func LoadDir(dir string, seasons []string) ([]Match, error) {
	if len(seasons) == 0 {
		paths, err := filepath.Glob(filepath.Join(dir, "T1_*.csv"))
		if err != nil {
			return nil, err
		}
		for _, p := range paths {
			seasons = append(seasons, strings.TrimSuffix(strings.TrimPrefix(filepath.Base(p), "T1_"), ".csv"))
		}
	}
	if len(seasons) == 0 {
		return nil, fmt.Errorf("no T1_*.csv files in %s", dir)
	}

	var all []Match
	for _, s := range seasons {
		f, err := os.Open(filepath.Join(dir, FileName(s)))
		if err != nil {
			return nil, err
		}
		ms, err := Parse(f, s)
		f.Close()
		if err != nil {
			return nil, fmt.Errorf("season %s: %w", s, err)
		}
		all = append(all, ms...)
	}
	sort.SliceStable(all, func(i, j int) bool { return all[i].Kickoff.Before(all[j].Kickoff) })
	return all, nil
}
