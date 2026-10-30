package apifootball

import (
	"fmt"
	"strings"
	"time"
)

// MatchDetail is one finished match flattened into what the database stores.
// Team names are still API spellings; the store maps them to canonical teams.
type MatchDetail struct {
	FixtureID  int
	SeasonCode string
	Kickoff    time.Time
	Referee    string
	Venue      string
	Home, Away TeamDetail
	Events     []EventDetail
}

type TeamDetail struct {
	// ID is what ties the sections of a response together: the API spells
	// the same team differently across them ("Fenerbahçe" at the top,
	// "Fenerbahce" in the lineup), but the id is stable.
	ID        int
	Name      string
	Formation string
	CoachID   *int
	CoachName string
	Stats     TeamStats
	Players   []PlayerDetail
}

type TeamStats struct {
	Shots, ShotsOnTarget, ShotsInsideBox, Corners, Fouls, Yellow, Red *int
	Passes, PassesAccurate, Saves                                     *int
	Possession, XG, GoalsPrevented                                    *float64
}

// PlayerDetail is one squad member: a starter, a substitute who came on, or an
// unused substitute (Minutes nil). Keeping the bench lets later analysis see
// who was rested rather than absent.
type PlayerDetail struct {
	ID        int
	Name      string
	Started   bool
	Number    *int
	Pos       string // G, D, M, F
	Grid      string // "row:column" for starters
	Captain   bool
	MinuteOn  *int
	MinuteOff *int
	Minutes   *int
	Stats     *PlayerStats
}

type EventDetail struct {
	Minute    int
	Extra     *int
	TeamID    int
	PlayerID  *int
	RelatedID *int   // assister for goals, the player coming on for subs
	Type      string // goal, own_goal, penalty_goal, missed_penalty, yellow, second_yellow, red, sub, var
	Detail    string
}

// Normalize flattens a single-id /fixtures response.
func Normalize(f Fixture) (MatchDetail, error) {
	if len(f.Lineups) != 2 {
		return MatchDetail{}, fmt.Errorf("fixture %d: %d lineups, want 2", f.Fixture.ID, len(f.Lineups))
	}
	d := MatchDetail{
		FixtureID:  f.Fixture.ID,
		SeasonCode: SeasonCode(f.League.Season),
		Kickoff:    f.Fixture.Date,
		Home:       TeamDetail{ID: f.Teams.Home.ID, Name: f.Teams.Home.Name},
		Away:       TeamDetail{ID: f.Teams.Away.ID, Name: f.Teams.Away.Name},
	}
	if f.Fixture.Referee != nil {
		// Sometimes "Name, Country".
		d.Referee = strings.TrimSpace(strings.Split(*f.Fixture.Referee, ",")[0])
	}
	if f.Fixture.Venue.Name != nil {
		d.Venue = *f.Fixture.Venue.Name
	}

	for _, e := range f.Events {
		typ, ok := eventType(e)
		if !ok {
			continue
		}
		d.Events = append(d.Events, EventDetail{
			Minute: e.Time.Elapsed, Extra: e.Time.Extra, TeamID: e.Team.ID,
			PlayerID: e.Player.ID, RelatedID: e.Assist.ID, Type: typ, Detail: e.Detail,
		})
	}

	for _, side := range []*TeamDetail{&d.Home, &d.Away} {
		if err := fillTeam(side, f, d.Events); err != nil {
			return MatchDetail{}, fmt.Errorf("fixture %d: %w", f.Fixture.ID, err)
		}
	}
	return d, nil
}

func eventType(e Event) (string, bool) {
	detail := strings.ToLower(e.Detail)
	switch e.Type {
	case "Goal":
		switch detail {
		case "own goal":
			return "own_goal", true
		case "penalty":
			return "penalty_goal", true
		case "missed penalty":
			return "missed_penalty", true
		default:
			return "goal", true
		}
	case "Card":
		switch {
		case strings.Contains(detail, "second"):
			return "second_yellow", true
		case strings.Contains(detail, "red"):
			return "red", true
		default:
			return "yellow", true
		}
	case "subst":
		return "sub", true
	case "Var":
		return "var", true
	}
	return "", false
}

func fillTeam(t *TeamDetail, f Fixture, events []EventDetail) error {
	var lu *Lineup
	for i := range f.Lineups {
		if f.Lineups[i].Team.ID == t.ID {
			lu = &f.Lineups[i]
		}
	}
	if lu == nil {
		return fmt.Errorf("no lineup for %s", t.Name)
	}
	t.Formation = lu.Formation
	t.CoachID = lu.Coach.ID
	if lu.Coach.Name != nil {
		t.CoachName = *lu.Coach.Name
	}

	for _, s := range f.Statistics {
		if s.Team.ID != t.ID {
			continue
		}
		t.Stats = TeamStats{
			Shots: s.intStat("Total Shots"), ShotsOnTarget: s.intStat("Shots on Goal"),
			ShotsInsideBox: s.intStat("Shots insidebox"), Corners: s.intStat("Corner Kicks"),
			Fouls: s.intStat("Fouls"), Yellow: s.intStat("Yellow Cards"), Red: s.intStat("Red Cards"),
			Passes: s.intStat("Total passes"), PassesAccurate: s.intStat("Passes accurate"),
			Saves: s.intStat("Goalkeeper Saves"), Possession: s.Stat("Ball Possession"),
			XG: s.Stat("expected_goals"), GoalsPrevented: s.Stat("goals_prevented"),
		}
	}

	stats := map[int]*PlayerStats{}
	names := map[int]string{}
	for _, tp := range f.Players {
		if tp.Team.ID != t.ID {
			continue
		}
		for _, p := range tp.Players {
			if len(p.Statistics) > 0 {
				ps := p.Statistics[0]
				stats[p.Player.ID] = &ps
			}
			names[p.Player.ID] = p.Player.Name // full name; lineups abbreviate
		}
	}

	// Minutes on and off come from the substitution and red card events.
	on, off := map[int]int{}, map[int]int{}
	for _, e := range events {
		if e.TeamID != t.ID {
			continue
		}
		switch e.Type {
		case "sub":
			if e.PlayerID != nil {
				off[*e.PlayerID] = e.Minute
			}
			if e.RelatedID != nil {
				on[*e.RelatedID] = e.Minute
			}
		case "red", "second_yellow":
			if e.PlayerID != nil {
				off[*e.PlayerID] = e.Minute
			}
		}
	}

	add := func(entries []LineupEntry, started bool) {
		for _, en := range entries {
			if en.Player.ID == nil {
				continue // the API occasionally lists an unidentified player
			}
			id := *en.Player.ID
			p := PlayerDetail{ID: id, Name: en.Player.Name, Started: started, Number: en.Player.Number, Stats: stats[id]}
			if full := names[id]; full != "" {
				p.Name = full
			}
			if en.Player.Pos != nil {
				p.Pos = *en.Player.Pos
			}
			if en.Player.Grid != nil {
				p.Grid = *en.Player.Grid
			}
			if started {
				zero := 0
				p.MinuteOn = &zero
			} else if m, ok := on[id]; ok {
				p.MinuteOn = &m
			}
			if m, ok := off[id]; ok {
				p.MinuteOff = &m
			}
			if p.Stats != nil {
				p.Captain = p.Stats.Games.Captain
				p.Minutes = p.Stats.Games.Minutes
			}
			t.Players = append(t.Players, p)
		}
	}
	add(lu.StartXI, true)
	add(lu.Substitutes, false)
	return nil
}

func (t TeamStatistics) intStat(name string) *int {
	v := t.Stat(name)
	if v == nil {
		return nil
	}
	return FlexNum{V: v}.Int()
}
