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
)

const baseURL = "https://www.football-data.co.uk/mmz4281"

// FileName is the on-disk name for a season file inside a raw data directory.
func FileName(season string) string { return "T1_" + season + ".csv" }

// Fetch downloads one season into dir. It refuses to write anything that is not
// a football-data CSV (see LooksLikeCSV).
func Fetch(ctx context.Context, client *http.Client, season, dir string) error {
	url := fmt.Sprintf("%s/%s/T1.csv", baseURL, season)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return err
	}
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("fetch %s: %w", url, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("fetch %s: status %s", url, resp.Status)
	}
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("fetch %s: %w", url, err)
	}
	if !LooksLikeCSV(body) {
		return fmt.Errorf("fetch %s: response is not a football-data CSV (blocked network or changed URL?)", url)
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(dir, FileName(season)), body, 0o644)
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
