package footballdata

import (
	"os"
	"testing"
	"time"
)

func parseFile(t *testing.T, path, season string) []Match {
	t.Helper()
	f, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	ms, err := Parse(f, season)
	if err != nil {
		t.Fatal(err)
	}
	return ms
}

func TestParseNewFormat(t *testing.T) {
	ms := parseFile(t, "testdata/synthetic_new.csv", "2627")
	if len(ms) != 2 {
		t.Fatalf("got %d matches, want 2 (trailing empty row must be skipped)", len(ms))
	}

	played := ms[0]
	if played.Home != "Konyaspor" || played.Away != "Galatasaray" {
		t.Fatalf("teams = %q vs %q (BOM must not leak into the first header)", played.Home, played.Away)
	}
	if !played.Played() || *played.HomeGoals != 0 || *played.AwayGoals != 4 {
		t.Fatalf("score not parsed: %+v", played)
	}
	// UK clocks went back on 25 Oct 2026, so 18:00 London is 18:00 UTC.
	want := time.Date(2026, 10, 30, 18, 0, 0, 0, time.UTC)
	if !played.Kickoff.Equal(want) {
		t.Fatalf("kickoff = %v, want %v", played.Kickoff.UTC(), want)
	}
	if played.Referee != "Test Referee" || *played.AwayStats.ShotsOnTarget != 8 || *played.HomeStats.Corners != 4 {
		t.Fatalf("stats not parsed: %+v", played)
	}

	byKey := map[string]Odds{}
	for _, o := range played.Odds {
		byKey[o.Bookmaker+"/"+o.Kind] = o
	}
	if len(byKey) != 6 {
		t.Fatalf("got %d odds rows, want 6: %+v", len(byKey), played.Odds)
	}
	if o := byKey["pinnacle/close"]; o.Home != 7.20 || o.Draw != 5.10 || o.Away != 1.44 {
		t.Fatalf("pinnacle close = %+v", o)
	}

	fixture := ms[1]
	if fixture.Played() {
		t.Fatal("unplayed fixture reported as played")
	}
	if len(fixture.Odds) != 2 {
		t.Fatalf("fixture odds = %+v, want bet365 + market_avg open only", fixture.Odds)
	}
}

func TestParseOldFormat(t *testing.T) {
	ms := parseFile(t, "testdata/synthetic_old.csv", "1718")
	if len(ms) != 1 {
		t.Fatalf("got %d matches", len(ms))
	}
	m := ms[0]
	if y, mo, d := m.Kickoff.Date(); y != 2017 || mo != time.August || d != 12 {
		t.Fatalf("two-digit year date = %v", m.Kickoff)
	}
	if len(m.Odds) != 2 || m.Odds[1].Bookmaker != "market_avg" {
		t.Fatalf("old Bb* columns not mapped: %+v", m.Odds)
	}
}

func TestLooksLikeCSV(t *testing.T) {
	if !LooksLikeCSV([]byte(bom + "Div,Date,Time\n")) {
		t.Error("CSV with BOM rejected")
	}
	if LooksLikeCSV([]byte("<html><head><title>Erişim engellendi</title>")) {
		t.Error("HTML block page accepted")
	}
}

func TestSeasonName(t *testing.T) {
	name, year, err := SeasonName("2627")
	if err != nil || name != "2026-27" || year != 2026 {
		t.Fatalf("got %q %d %v", name, year, err)
	}
	if _, _, err := SeasonName("26-27"); err == nil {
		t.Fatal("bad code accepted")
	}
}
