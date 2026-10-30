package apifootball

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
)

// Cache stores raw API bodies on disk so no request is ever made twice:
//
//	<Dir>/<season>/fixtures.json         the season's fixture list
//	<Dir>/<season>/fixture_<id>.json     one match with all details
type Cache struct{ Dir string }

func (c Cache) listPath(season int) string {
	return filepath.Join(c.Dir, strconv.Itoa(season), "fixtures.json")
}

func (c Cache) fixturePath(season, id int) string {
	return filepath.Join(c.Dir, strconv.Itoa(season), fmt.Sprintf("fixture_%d.json", id))
}

func (c Cache) HasFixture(season, id int) bool {
	_, err := os.Stat(c.fixturePath(season, id))
	return err == nil
}

// save writes atomically, so an interrupted run never leaves half a file that
// a later run would mistake for a cached response.
func save(path string, body []byte) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, body, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

func (c Cache) SaveList(season int, body []byte) error { return save(c.listPath(season), body) }

func (c Cache) SaveFixture(season, id int, body []byte) error {
	return save(c.fixturePath(season, id), body)
}

// List returns the cached fixture list for a season, or ok=false if absent.
func (c Cache) List(season int) (fixtures []Fixture, ok bool, err error) {
	body, err := os.ReadFile(c.listPath(season))
	if os.IsNotExist(err) {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, err
	}
	fixtures, err = decodeResponse[Fixture](body)
	return fixtures, err == nil, err
}

// Fixture loads one cached match with all its details.
func (c Cache) Fixture(season, id int) (Fixture, error) {
	body, err := os.ReadFile(c.fixturePath(season, id))
	if err != nil {
		return Fixture{}, err
	}
	fs, err := decodeResponse[Fixture](body)
	if err != nil {
		return Fixture{}, fmt.Errorf("fixture %d: %w", id, err)
	}
	if len(fs) != 1 {
		return Fixture{}, fmt.Errorf("fixture %d: %d entries in response", id, len(fs))
	}
	return fs[0], nil
}

// CachedFixtureIDs lists the match ids cached for a season, ascending.
func (c Cache) CachedFixtureIDs(season int) ([]int, error) {
	paths, err := filepath.Glob(filepath.Join(c.Dir, strconv.Itoa(season), "fixture_*.json"))
	if err != nil {
		return nil, err
	}
	var ids []int
	for _, p := range paths {
		s := strings.TrimSuffix(strings.TrimPrefix(filepath.Base(p), "fixture_"), ".json")
		if id, err := strconv.Atoi(s); err == nil {
			ids = append(ids, id)
		}
	}
	sort.Ints(ids)
	return ids, nil
}
