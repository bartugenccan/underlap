package phase

import (
	"encoding/json"
	"os"
	"slices"
	"strings"
	"testing"
)

func load(t *testing.T, name string) Scenario {
	t.Helper()
	b, err := os.ReadFile("testdata/" + name + ".json")
	if err != nil {
		t.Fatal(err)
	}
	var s Scenario
	if err := json.Unmarshal(b, &s); err != nil {
		t.Fatal(err)
	}
	return s
}

func analyze(t *testing.T, s Scenario) Analysis {
	t.Helper()
	a, err := Analyze(s)
	if err != nil {
		t.Fatal(err)
	}
	return a
}

func pass(t *testing.T, a Analysis, to string) PassOption {
	t.Helper()
	for _, p := range a.Passes {
		if p.To == to {
			return p
		}
	}
	t.Fatalf("no pass option to %s", to)
	return PassOption{}
}

func hasFinding(a Analysis, kind, player string) bool {
	for _, f := range a.Findings {
		if f.Kind == kind && slices.Contains(f.Players, player) {
			return true
		}
	}
	return false
}

// The chain from Serdar Kelleci's GS-FB simulation: if Guendouzi steps out to
// press Torreira man to man, Kanté is left alone, and Batrakov gets the ball
// free between Fenerbahçe's lines. Coordinates are approximate, read off his
// tactics board; the test checks the qualitative outcome only.
func TestGuendouziPressFreesBatrakov(t *testing.T) {
	before := analyze(t, load(t, "gsfb_guendouzi_before"))
	if !hasFinding(before, "between_lines_marked", "batrakov") {
		t.Errorf("before: Batrakov should be between the lines but covered; findings: %v", before.Findings)
	}
	if p := pass(t, before, "batrakov"); p.Free || p.LaneOpen {
		t.Errorf("before: pass to Batrakov should be closed by Guendouzi, got %+v", p)
	}

	after := analyze(t, load(t, "gsfb_guendouzi_after"))
	if !hasFinding(after, "free_between_lines", "batrakov") {
		t.Errorf("after: Batrakov should be free between the lines; findings: %v", after.Findings)
	}
	if p := pass(t, after, "batrakov"); !p.Free {
		t.Errorf("after: pass to Batrakov should be free, got %+v", p)
	}
}

// Lille's trap from the Lille-GS simulation: Mbappé drops deep, Jakobs follows
// him up, and the third man runs into the space Jakobs left.
func TestJakobsFollowingMbappeOpensSpaceBehind(t *testing.T) {
	before := analyze(t, load(t, "lille_mbappe_before"))
	if p := pass(t, before, "haraldsson"); p.Free {
		t.Errorf("before: Jakobs is still home, Haraldsson should not be free: %+v", p)
	}

	after := analyze(t, load(t, "lille_mbappe_after"))
	if p := pass(t, after, "haraldsson"); !p.Free {
		t.Errorf("after: Haraldsson should be free behind Jakobs: %+v", p)
	}
	var found bool
	for _, o := range after.Overloads {
		if slices.Contains(o.Attackers, "Bentaleb") && slices.Contains(o.Attackers, "Santos") &&
			slices.Contains(o.Attackers, "Mbappé") && len(o.Attackers) > len(o.Defenders) {
			found = true
		}
	}
	if !found {
		t.Errorf("after: want the Bentaleb-Santos-Mbappé overload on Lille's right, got %+v", after.Overloads)
	}
}

func TestAnalyzeIsDeterministic(t *testing.T) {
	s := load(t, "gsfb_guendouzi_after")
	a1, _ := json.Marshal(analyze(t, s))
	a2, _ := json.Marshal(analyze(t, s))
	if string(a1) != string(a2) {
		t.Fatal("two runs on the same scenario differ")
	}
}

// Mirroring the scenario across the long axis must mirror control. The 2.5 m
// grid does not divide 68 m evenly, so grid cells never mirror onto each
// other; compare the underlying arrival times at mirrored points instead.
func TestPitchControlMirrors(t *testing.T) {
	s := load(t, "gsfb_guendouzi_after")
	ma, md := mirror(s.Attackers), mirror(s.Defenders)
	prm := DefaultParams()
	compared := 0
	for x := 0.0; x <= PitchLength; x += 3.5 {
		for y := 0.0; y <= PitchWidth; y += 3.4 {
			ta, _ := firstArrival(s.Attackers, x, y, prm)
			td, _ := firstArrival(s.Defenders, x, y, prm)
			mta, _ := firstArrival(ma, x, PitchWidth-y, prm)
			mtd, _ := firstArrival(md, x, PitchWidth-y, prm)
			if d := (td - ta) - (mtd - mta); d > 1e-9 || d < -1e-9 {
				t.Fatalf("control at (%.1f, %.1f) = %.3f, mirrored %.3f", x, y, td-ta, mtd-mta)
			}
			compared++
		}
	}
	if compared < 500 {
		t.Fatalf("only %d points compared", compared)
	}
}

func mirror(ps []Player) []Player {
	out := make([]Player, len(ps))
	for i, p := range ps {
		p.Y = PitchWidth - p.Y
		out[i] = p
	}
	return out
}

func TestDefenderOnTheLineClosesIt(t *testing.T) {
	s := Scenario{
		BallCarrier: "a1",
		Attackers:   []Player{{ID: "a1", Name: "A1", X: 30, Y: 34}, {ID: "a2", Name: "A2", X: 50, Y: 34}},
		Defenders:   []Player{{ID: "d1", Name: "D1", X: 40, Y: 34}},
	}
	if p := pass(t, analyze(t, s), "a2"); p.LaneOpen || p.Interceptor != "D1" {
		t.Fatalf("defender standing on the pass line must close it: %+v", p)
	}
	s.Defenders[0].Y = 60 // far off the line
	if p := pass(t, analyze(t, s), "a2"); !p.LaneOpen {
		t.Fatalf("defender 26 m away must not close it: %+v", p)
	}
}

func TestDefensiveLines(t *testing.T) {
	var ds []Player
	add := func(id string, x, y float64) { ds = append(ds, Player{ID: id, Name: id, X: x, Y: y}) }
	add("gk", 102, 34)
	ds[0].Role = "GK"
	add("rb", 79, 10)
	add("rcb", 81, 26)
	add("lcb", 80, 42)
	add("lb", 78, 58)
	add("dm1", 64, 28)
	add("dm2", 66, 40)
	add("am", 55, 34)
	add("st", 48, 30)
	s := Scenario{BallCarrier: "x", Attackers: []Player{{ID: "x", Name: "x", X: 40, Y: 34}}, Defenders: ds}
	lines := analyze(t, s).Lines
	if len(lines) != 3 {
		t.Fatalf("want 3 lines (back four, double pivot, front two), got %+v", lines)
	}
	if got := strings.Join(lines[0].Members, ","); got != "lb,rb,lcb,rcb" {
		t.Errorf("deepest line = %s", got)
	}
	if got := strings.Join(lines[2].Members, ","); got != "st,am" {
		t.Errorf("front line = %s", got)
	}
}

func TestZoneBoundaries(t *testing.T) {
	for _, tc := range []struct {
		x, y       float64
		lane, band int
	}{
		{0, 0, 0, 0}, {17.4, 13.83, 0, 0}, {17.6, 13.84, 1, 1}, {52.5, 34, 2, 3},
		{80, 54.16, 4, 4}, {105, 68, 4, 5},
	} {
		if l, b := zoneOf(tc.x, tc.y); l != tc.lane || b != tc.band {
			t.Errorf("zoneOf(%v, %v) = lane %d band %d, want %d %d", tc.x, tc.y, l, b, tc.lane, tc.band)
		}
	}
}

func TestValidate(t *testing.T) {
	ok := Scenario{BallCarrier: "a", Attackers: []Player{{ID: "a", X: 10, Y: 10}}, Defenders: []Player{{ID: "d", X: 20, Y: 20}}}
	if err := ok.Validate(); err != nil {
		t.Fatalf("valid scenario rejected: %v", err)
	}
	bad := map[string]func(Scenario) Scenario{
		"duplicate id":       func(s Scenario) Scenario { s.Defenders = []Player{{ID: "a", X: 20, Y: 20}}; return s },
		"carrier not ours":   func(s Scenario) Scenario { s.BallCarrier = "d"; return s },
		"off the pitch":      func(s Scenario) Scenario { s.Attackers = []Player{{ID: "a", X: 106, Y: 10}}; return s },
		"bad marking mode":   func(s Scenario) Scenario { s.Markings = []Marking{{DefenderID: "d", Mode: "zonal"}}; return s },
		"man without target": func(s Scenario) Scenario { s.Markings = []Marking{{DefenderID: "d", Mode: "man"}}; return s },
	}
	for name, mutate := range bad {
		if err := mutate(ok).Validate(); err == nil {
			t.Errorf("%s: accepted", name)
		}
	}
}

// Galatasaray's 4-2-3-1 from the 2024-25 opener, grids as API-Football gives them.
func TestSeedFollowsGridOrientation(t *testing.T) {
	xi := []LineupPlayer{
		{ID: "muslera", Name: "Muslera", Pos: "G", Grid: "1:1"},
		{ID: "kohn", Name: "Köhn", Pos: "D", Grid: "2:1"},
		{ID: "abdulkerim", Name: "Abdülkerim", Pos: "D", Grid: "2:2"},
		{ID: "davinson", Name: "Davinson", Pos: "D", Grid: "2:3"},
		{ID: "dubois", Name: "Dubois", Pos: "D", Grid: "2:4"},
		{ID: "demirbay", Name: "Demirbay", Pos: "M", Grid: "3:1"},
		{ID: "torreira", Name: "Torreira", Pos: "M", Grid: "3:2"},
		{ID: "baris", Name: "Barış", Pos: "M", Grid: "4:1"},
		{ID: "mertens", Name: "Mertens", Pos: "M", Grid: "4:2"},
		{ID: "ziyech", Name: "Ziyech", Pos: "M", Grid: "4:3"},
		{ID: "icardi", Name: "Icardi", Pos: "F", Grid: "5:1"},
	}
	at := func(ps []Player, id string) Player { p, _ := find(ps, id); return p }

	att, err := Seed(xi, true)
	if err != nil {
		t.Fatal(err)
	}
	if !at(att, "muslera").IsGK() || at(att, "muslera").X != 5 {
		t.Errorf("keeper: %+v", at(att, "muslera"))
	}
	// Attacking left to right, the left-back (column 1) is at low y.
	if at(att, "kohn").Y >= at(att, "dubois").Y {
		t.Errorf("attacking: Köhn %v should be left of Dubois %v", at(att, "kohn").Y, at(att, "dubois").Y)
	}
	if at(att, "icardi").X <= at(att, "mertens").X || at(att, "mertens").X <= at(att, "torreira").X {
		t.Errorf("rows should advance towards halfway")
	}

	def, _ := Seed(xi, false)
	// Defending, the same team faces the other way: left-back at high y, keeper at x=100.
	if at(def, "kohn").Y <= at(def, "dubois").Y || at(def, "muslera").X != 100 {
		t.Errorf("defending: Köhn %v / Dubois %v, keeper x %v", at(def, "kohn").Y, at(def, "dubois").Y, at(def, "muslera").X)
	}
	if _, err := Seed([]LineupPlayer{{Name: "x", Grid: "2"}}, true); err == nil {
		t.Error("malformed grid accepted")
	}
}

func TestTurkishSuffixes(t *testing.T) {
	for in, want := range map[string]string{
		"Torreira": "Torreira'dan", "Osimhen": "Osimhen'den", "Kanté": "Kanté'den",
		"Abdülkerim": "Abdülkerim'den", "Batrakov": "Batrakov'dan", "Mbappé": "Mbappé'den", "Brook": "Brook'tan",
	} {
		if got := abl(in); got != want {
			t.Errorf("abl(%q) = %q, want %q", in, got, want)
		}
	}
	if dat(2) != "2'ye" || dat(3) != "3'e" || dat(6) != "6'ya" {
		t.Errorf("dat: %s %s %s", dat(2), dat(3), dat(6))
	}
}
