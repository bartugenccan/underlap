package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/bartugenccan/underlap/internal/phase"
	"github.com/bartugenccan/underlap/internal/store"
)

const origin = "http://localhost:3000"

func TestAnalyzeEndpoint(t *testing.T) {
	body, err := os.ReadFile("../phase/testdata/gsfb_guendouzi_after.json")
	if err != nil {
		t.Fatal(err)
	}
	h := New(nil, origin)

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/api/phase/analyze", strings.NewReader(string(body))))
	if rec.Code != http.StatusOK {
		t.Fatalf("status %d: %s", rec.Code, rec.Body)
	}
	var a phase.Analysis
	if err := json.Unmarshal(rec.Body.Bytes(), &a); err != nil {
		t.Fatal(err)
	}
	if len(a.Findings) == 0 || a.Findings[0].Kind != "free_between_lines" {
		t.Fatalf("first finding should be Batrakov free between the lines, got %+v", a.Findings)
	}

	for name, tc := range map[string]struct {
		body string
		want int
	}{
		"not json":      {"{", http.StatusBadRequest},
		"unknown field": {`{"attackers": [], "colour": "red"}`, http.StatusBadRequest},
		"invalid":       {`{"attackers": [], "defenders": [], "ball_carrier": "x"}`, http.StatusUnprocessableEntity},
	} {
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/api/phase/analyze", strings.NewReader(tc.body)))
		if rec.Code != tc.want {
			t.Errorf("%s: status %d, want %d (%s)", name, rec.Code, tc.want, rec.Body)
		}
	}
}

func TestCORS(t *testing.T) {
	h := New(nil, origin)
	req := httptest.NewRequest(http.MethodOptions, "/api/phase/analyze", nil)
	req.Header.Set("Origin", origin)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusNoContent || rec.Header().Get("Access-Control-Allow-Origin") != origin {
		t.Fatalf("preflight: %d %v", rec.Code, rec.Header())
	}
	req.Header.Set("Origin", "https://evil.example")
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Header().Get("Access-Control-Allow-Origin") != "" {
		t.Fatal("foreign origin allowed")
	}
}

type fakeStore struct{ ml *store.MatchLineups }

func (f fakeStore) Matches(context.Context, string, string) ([]store.MatchSummary, error) {
	return []store.MatchSummary{f.ml.Match}, nil
}

func (f fakeStore) Lineups(_ context.Context, id int) (*store.MatchLineups, error) {
	if id != f.ml.Match.ID {
		return nil, store.ErrNotFound
	}
	return f.ml, nil
}

func xi(prefix string) []phase.LineupPlayer {
	grids := []string{"1:1", "2:1", "2:2", "2:3", "2:4", "3:1", "3:2", "4:1", "4:2", "4:3", "5:1"}
	out := make([]phase.LineupPlayer, len(grids))
	for i, g := range grids {
		out[i] = phase.LineupPlayer{ID: prefix + g, Name: prefix + g, Grid: g}
	}
	out[0].Pos = "G"
	return out
}

func TestLineupsSeedsAnAnalyzableScenario(t *testing.T) {
	st := fakeStore{&store.MatchLineups{
		Match: store.MatchSummary{ID: 7, Home: "Galatasaray", Away: "Hatayspor"},
		Home:  store.TeamLineup{Team: "Galatasaray", Formation: "4-2-3-1", Starters: xi("gs")},
		Away:  store.TeamLineup{Team: "Hatayspor", Formation: "4-1-4-1", Starters: xi("ht")},
	}}
	h := New(st, origin)

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/matches/7/lineups?attacking=away", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("status %d: %s", rec.Code, rec.Body)
	}
	var resp LineupsResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatal(err)
	}
	if resp.Scenario.AttackingTeam != "Hatayspor" || resp.Scenario.BallCarrier != "ht1:1" {
		t.Fatalf("away side should attack with its keeper on the ball: %+v", resp.Scenario)
	}
	if _, err := phase.Analyze(resp.Scenario); err != nil {
		t.Fatalf("seeded scenario does not analyze: %v", err)
	}

	for path, want := range map[string]int{
		"/api/matches/8/lineups":                    http.StatusNotFound,
		"/api/matches/x/lineups":                    http.StatusBadRequest,
		"/api/matches/7/lineups?attacking=both":     http.StatusBadRequest,
		"/api/matches?team=Galatasaray":             http.StatusBadRequest,
		"/api/matches?team=Galatasaray&season=2425": http.StatusOK,
	} {
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, nil))
		if rec.Code != want {
			t.Errorf("%s: status %d, want %d", path, rec.Code, want)
		}
	}
}
