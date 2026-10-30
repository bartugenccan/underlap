package apifootball

import (
	"encoding/json"
	"os"
	"strings"
	"testing"
	"time"
)

// testdata/fixture_gs_hatay_2024.json is a real response trimmed down:
// Galatasaray 2-1 Hatayspor, 9 August 2024, the 2024-25 opener.
func loadFixture(t *testing.T) Fixture {
	t.Helper()
	body, err := os.ReadFile("testdata/fixture_gs_hatay_2024.json")
	if err != nil {
		t.Fatal(err)
	}
	if err := checkEnvelope(body); err != nil {
		t.Fatal(err)
	}
	fs, err := decodeResponse[Fixture](body)
	if err != nil || len(fs) != 1 {
		t.Fatalf("decode: %v (%d fixtures)", err, len(fs))
	}
	return fs[0]
}

func player(t *testing.T, team TeamDetail, nameContains string) PlayerDetail {
	t.Helper()
	for _, p := range team.Players {
		if strings.Contains(p.Name, nameContains) {
			return p
		}
	}
	t.Fatalf("%s: no player matching %q", team.Name, nameContains)
	return PlayerDetail{}
}

func TestNormalizeGalatasarayHatayspor(t *testing.T) {
	d, err := Normalize(loadFixture(t))
	if err != nil {
		t.Fatal(err)
	}
	if d.SeasonCode != "2425" || !d.Kickoff.Equal(time.Date(2024, 8, 9, 18, 0, 0, 0, time.UTC)) {
		t.Fatalf("season/kickoff = %s %v", d.SeasonCode, d.Kickoff)
	}
	gs, hatay := d.Home, d.Away
	if gs.Name != "Galatasaray" || gs.Formation != "4-2-3-1" || gs.CoachName != "O. Buruk" {
		t.Fatalf("home = %s %s %s", gs.Name, gs.Formation, gs.CoachName)
	}
	if hatay.Formation != "4-1-4-1" {
		t.Fatalf("away formation = %s", hatay.Formation)
	}
	if gs.Stats.XG == nil || *gs.Stats.XG != 3.58 || *hatay.Stats.XG != 0.57 {
		t.Fatalf("xG = %v / %v", gs.Stats.XG, hatay.Stats.XG)
	}
	if gs.Stats.Possession == nil || *gs.Stats.Possession <= 50 {
		t.Fatalf("possession %% not parsed: %v", gs.Stats.Possession)
	}

	starters, withGrid := 0, 0
	for _, p := range gs.Players {
		if p.Started {
			starters++
			if p.Grid != "" {
				withGrid++
			}
		}
	}
	if starters != 11 || withGrid != 11 {
		t.Fatalf("starters %d, with grid %d; want 11 and 11", starters, withGrid)
	}

	// Mertens went off for Aktürkoğlu on 70'.
	mertens, kerem := player(t, gs, "Mertens"), player(t, gs, "Aktürkoğlu")
	if !mertens.Started || mertens.MinuteOff == nil || *mertens.MinuteOff != 70 {
		t.Fatalf("Mertens: %+v", mertens)
	}
	if kerem.Started || kerem.MinuteOn == nil || *kerem.MinuteOn != 70 {
		t.Fatalf("Aktürkoğlu: %+v", kerem)
	}

	var unused int
	for _, p := range append(gs.Players, hatay.Players...) {
		if !p.Started && p.MinuteOn == nil {
			unused++
			if p.Minutes != nil {
				t.Fatalf("unused sub %s has minutes %d", p.Name, *p.Minutes)
			}
		}
	}
	if unused == 0 {
		t.Fatal("unused substitutes should be kept")
	}

	var penalty, stoppage bool
	for _, e := range d.Events {
		if e.Type == "penalty_goal" && e.Minute == 80 && e.TeamID == gs.ID {
			penalty = true
		}
		if e.Type == "yellow" && e.Minute == 90 && e.Extra != nil && *e.Extra == 3 {
			stoppage = true
		}
	}
	if !penalty || !stoppage {
		t.Fatalf("penalty goal 80' found=%v, yellow 90+3' found=%v", penalty, stoppage)
	}
}

// The API spells a team differently across sections of the same response
// (e.g. "Fenerbahçe" at the top, "Fenerbahce" in the lineup). Sections must be
// tied together by team id, never by name.
func TestNormalizeMatchesSectionsByTeamID(t *testing.T) {
	f := loadFixture(t)
	for i := range f.Lineups {
		if f.Lineups[i].Team.ID == f.Teams.Away.ID {
			f.Lineups[i].Team.Name = "Hatay Spor Kulübü"
		}
	}
	for i := range f.Players {
		if f.Players[i].Team.ID == f.Teams.Away.ID {
			f.Players[i].Team.Name = "Hatay Spor Kulübü"
		}
	}
	d, err := Normalize(f)
	if err != nil {
		t.Fatal(err)
	}
	if d.Away.Formation != "4-1-4-1" || len(d.Away.Players) == 0 {
		t.Fatalf("away side lost its lineup: %+v", d.Away)
	}
}

func TestCheckEnvelope(t *testing.T) {
	if err := checkEnvelope([]byte(`{"errors":[],"results":0,"response":[]}`)); err != nil {
		t.Fatalf("empty errors array rejected: %v", err)
	}
	err := checkEnvelope([]byte(`{"errors":{"plan":"Free plans do not have access to this season, try from 2022 to 2024."},"results":0,"response":[]}`))
	if err == nil || !strings.Contains(err.Error(), "Free plans") {
		t.Fatalf("plan error not surfaced: %v", err)
	}
}

func TestFlexNum(t *testing.T) {
	for in, want := range map[string]float64{`7`: 7, `1.88`: 1.88, `"1.88"`: 1.88, `"56%"`: 56, `"25"`: 25} {
		var f FlexNum
		if err := json.Unmarshal([]byte(in), &f); err != nil || f.V == nil || *f.V != want {
			t.Errorf("%s -> %v, %v; want %v", in, f.V, err, want)
		}
	}
	var f FlexNum
	if err := json.Unmarshal([]byte(`null`), &f); err != nil || f.V != nil {
		t.Errorf("null -> %v, %v", f.V, err)
	}
}

func TestSeasonCode(t *testing.T) {
	if got := SeasonCode(2024); got != "2425" {
		t.Fatalf("SeasonCode(2024) = %s", got)
	}
	if got := SeasonCode(2099); got != "9900" {
		t.Fatalf("SeasonCode(2099) = %s", got)
	}
}
