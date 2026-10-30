// Package footballdata reads the free results + odds CSVs published by
// football-data.co.uk. Süper Lig is division "T1".
//
// The column set varies between seasons (older files use Bb* market columns,
// newer ones Avg*/Max* and add closing prices), so parsing is header driven and
// every optional field is a pointer.
package footballdata

import (
	"bytes"
	"encoding/csv"
	"errors"
	"fmt"
	"io"
	"strconv"
	"strings"
	"time"
	_ "time/tzdata" // kick-off times are UK local; Windows has no zoneinfo by default
)

const Source = "footballdata"

type TeamStats struct {
	Shots, ShotsOnTarget, Corners, Fouls, Yellow, Red *int
}

type Odds struct {
	Bookmaker string // bet365, pinnacle, market_avg, market_max
	Kind      string // open, close
	Home      float64
	Draw      float64
	Away      float64
}

type Match struct {
	Season    string
	Kickoff   time.Time
	Home      string
	Away      string
	HomeGoals *int
	AwayGoals *int
	HTHome    *int
	HTAway    *int
	Referee   string
	HomeStats TeamStats
	AwayStats TeamStats
	Odds      []Odds
}

func (m Match) Played() bool { return m.HomeGoals != nil && m.AwayGoals != nil }

// oddsColumns maps a column prefix to (bookmaker, kind). The prefix is followed
// by H, D or A in the CSV header, e.g. PSCH = Pinnacle closing home price.
var oddsColumns = []struct{ prefix, bookmaker, kind string }{
	{"B365", "bet365", "open"},
	{"PS", "pinnacle", "open"},
	{"Avg", "market_avg", "open"},
	{"Max", "market_max", "open"},
	{"BbAv", "market_avg", "open"}, // pre-2019 files
	{"BbMx", "market_max", "open"},
	{"B365C", "bet365", "close"},
	{"PSC", "pinnacle", "close"},
	{"AvgC", "market_avg", "close"},
	{"MaxC", "market_max", "close"},
}

var london = mustLoad("Europe/London")

// bom is the UTF-8 byte order mark some season files start with.
var bom = string(rune(0xFEFF))

func mustLoad(name string) *time.Location {
	loc, err := time.LoadLocation(name)
	if err != nil {
		panic(err)
	}
	return loc
}

// LooksLikeCSV reports whether b starts with a football-data header. Network
// blocks and captive portals answer with HTML pages and a 200 status, so the
// fetcher checks this before saving anything.
func LooksLikeCSV(b []byte) bool {
	b = bytes.TrimPrefix(b, []byte(bom))
	return bytes.HasPrefix(b, []byte("Div,"))
}

// Parse reads one season file. season is the football-data code, e.g. "2627".
func Parse(r io.Reader, season string) ([]Match, error) {
	cr := csv.NewReader(r)
	cr.FieldsPerRecord = -1
	cr.LazyQuotes = true

	header, err := cr.Read()
	if err != nil {
		return nil, fmt.Errorf("read header: %w", err)
	}
	col := make(map[string]int, len(header))
	for i, h := range header {
		col[strings.TrimPrefix(strings.TrimSpace(h), bom)] = i
	}
	for _, required := range []string{"Date", "HomeTeam", "AwayTeam"} {
		if _, ok := col[required]; !ok {
			return nil, fmt.Errorf("missing column %q", required)
		}
	}

	var matches []Match
	for line := 2; ; line++ {
		rec, err := cr.Read()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("line %d: %w", line, err)
		}
		row := record{rec, col}
		if row.str("HomeTeam") == "" {
			continue // files often end with empty ",,,," rows
		}
		m, err := row.match(season)
		if err != nil {
			return nil, fmt.Errorf("line %d: %w", line, err)
		}
		matches = append(matches, m)
	}
	return matches, nil
}

type record struct {
	fields []string
	col    map[string]int
}

func (r record) str(name string) string {
	i, ok := r.col[name]
	if !ok || i >= len(r.fields) {
		return ""
	}
	return strings.TrimSpace(r.fields[i])
}

func (r record) int(name string) *int {
	v, err := strconv.Atoi(r.str(name))
	if err != nil {
		return nil
	}
	return &v
}

func (r record) price(name string) (float64, bool) {
	v, err := strconv.ParseFloat(r.str(name), 64)
	return v, err == nil && v > 1
}

func (r record) match(season string) (Match, error) {
	kickoff, err := parseKickoff(r.str("Date"), r.str("Time"))
	if err != nil {
		return Match{}, err
	}
	m := Match{
		Season:    season,
		Kickoff:   kickoff,
		Home:      r.str("HomeTeam"),
		Away:      r.str("AwayTeam"),
		HomeGoals: r.int("FTHG"),
		AwayGoals: r.int("FTAG"),
		HTHome:    r.int("HTHG"),
		HTAway:    r.int("HTAG"),
		Referee:   r.str("Referee"),
		HomeStats: TeamStats{r.int("HS"), r.int("HST"), r.int("HC"), r.int("HF"), r.int("HY"), r.int("HR")},
		AwayStats: TeamStats{r.int("AS"), r.int("AST"), r.int("AC"), r.int("AF"), r.int("AY"), r.int("AR")},
	}
	for _, oc := range oddsColumns {
		h, okH := r.price(oc.prefix + "H")
		d, okD := r.price(oc.prefix + "D")
		a, okA := r.price(oc.prefix + "A")
		if okH && okD && okA {
			m.Odds = append(m.Odds, Odds{oc.bookmaker, oc.kind, h, d, a})
		}
	}
	return m, nil
}

func parseKickoff(date, clock string) (time.Time, error) {
	if clock == "" {
		clock = "00:00"
	}
	for _, layout := range []string{"02/01/2006 15:04", "02/01/06 15:04"} {
		if t, err := time.ParseInLocation(layout, date+" "+clock, london); err == nil {
			return t, nil
		}
	}
	return time.Time{}, fmt.Errorf("bad date/time %q %q", date, clock)
}

// SeasonName turns "2627" into "2026-27".
func SeasonName(code string) (name string, startYear int, err error) {
	if len(code) != 4 {
		return "", 0, fmt.Errorf("season code %q: want 4 digits like 2627", code)
	}
	yy, err := strconv.Atoi(code[:2])
	if err != nil {
		return "", 0, fmt.Errorf("season code %q: %w", code, err)
	}
	startYear = 2000 + yy
	return fmt.Sprintf("%d-%s", startYear, code[2:]), startYear, nil
}
