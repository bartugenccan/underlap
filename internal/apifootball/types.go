package apifootball

import (
	"encoding/json"
	"strconv"
	"strings"
	"time"
)

// Fixture is one element of /fixtures. The list endpoint fills the top-level
// fields; a single-id request also fills Events, Lineups, Statistics, Players.
type Fixture struct {
	Fixture struct {
		ID      int       `json:"id"`
		Referee *string   `json:"referee"`
		Date    time.Time `json:"date"`
		Venue   struct {
			Name *string `json:"name"`
		} `json:"venue"`
		Status struct {
			Short string `json:"short"`
		} `json:"status"`
	} `json:"fixture"`
	League struct {
		Season int    `json:"season"`
		Round  string `json:"round"`
	} `json:"league"`
	Teams struct {
		Home TeamRef `json:"home"`
		Away TeamRef `json:"away"`
	} `json:"teams"`
	Goals struct {
		Home *int `json:"home"`
		Away *int `json:"away"`
	} `json:"goals"`
	Score struct {
		Halftime struct {
			Home *int `json:"home"`
			Away *int `json:"away"`
		} `json:"halftime"`
	} `json:"score"`
	Events     []Event          `json:"events"`
	Lineups    []Lineup         `json:"lineups"`
	Statistics []TeamStatistics `json:"statistics"`
	Players    []TeamPlayers    `json:"players"`
}

// Finished reports whether the match is over (full time, extra time or penalties).
func (f Fixture) Finished() bool {
	switch f.Fixture.Status.Short {
	case "FT", "AET", "PEN":
		return true
	}
	return false
}

type TeamRef struct {
	ID   int    `json:"id"`
	Name string `json:"name"`
}

type PlayerRef struct {
	ID   *int    `json:"id"`
	Name *string `json:"name"`
}

type Event struct {
	Time struct {
		Elapsed int  `json:"elapsed"`
		Extra   *int `json:"extra"`
	} `json:"time"`
	Team     TeamRef   `json:"team"`
	Player   PlayerRef `json:"player"`
	Assist   PlayerRef `json:"assist"` // for substitutions: the player coming on
	Type     string    `json:"type"`   // Goal, Card, subst, Var
	Detail   string    `json:"detail"` // Normal Goal, Own Goal, Penalty, Missed Penalty, Yellow Card, ...
	Comments *string   `json:"comments"`
}

type Lineup struct {
	Team      TeamRef `json:"team"`
	Formation string  `json:"formation"`
	Coach     struct {
		ID   *int    `json:"id"`
		Name *string `json:"name"`
	} `json:"coach"`
	StartXI     []LineupEntry `json:"startXI"`
	Substitutes []LineupEntry `json:"substitutes"`
}

type LineupEntry struct {
	Player struct {
		ID     *int    `json:"id"`
		Name   string  `json:"name"`
		Number *int    `json:"number"`
		Pos    *string `json:"pos"`  // G, D, M, F
		Grid   *string `json:"grid"` // "row:column", row 1 is the goalkeeper
	} `json:"player"`
}

type TeamStatistics struct {
	Team       TeamRef `json:"team"`
	Statistics []struct {
		Type  string  `json:"type"`
		Value FlexNum `json:"value"`
	} `json:"statistics"`
}

// Stat returns the value of a statistic by its API name, e.g. "expected_goals".
func (t TeamStatistics) Stat(name string) *float64 {
	for _, s := range t.Statistics {
		if s.Type == name {
			return s.Value.V
		}
	}
	return nil
}

type TeamPlayers struct {
	Team    TeamRef `json:"team"`
	Players []struct {
		Player struct {
			ID   int    `json:"id"`
			Name string `json:"name"`
		} `json:"player"`
		Statistics []PlayerStats `json:"statistics"`
	} `json:"players"`
}

type PlayerStats struct {
	Games struct {
		Minutes    *int    `json:"minutes"`
		Number     *int    `json:"number"`
		Position   *string `json:"position"`
		Rating     FlexNum `json:"rating"`
		Captain    bool    `json:"captain"`
		Substitute bool    `json:"substitute"`
	} `json:"games"`
	Offsides *int `json:"offsides"`
	Shots    struct {
		Total *int `json:"total"`
		On    *int `json:"on"`
	} `json:"shots"`
	Goals struct {
		Total    *int `json:"total"`
		Conceded *int `json:"conceded"`
		Assists  *int `json:"assists"`
		Saves    *int `json:"saves"`
	} `json:"goals"`
	Passes struct {
		Total *int `json:"total"`
		Key   *int `json:"key"`
		// Accurate passes as a count, despite the name; sent as a string.
		Accuracy FlexNum `json:"accuracy"`
	} `json:"passes"`
	Tackles struct {
		Total         *int `json:"total"`
		Blocks        *int `json:"blocks"`
		Interceptions *int `json:"interceptions"`
	} `json:"tackles"`
	Duels struct {
		Total *int `json:"total"`
		Won   *int `json:"won"`
	} `json:"duels"`
	Dribbles struct {
		Attempts *int `json:"attempts"`
		Success  *int `json:"success"`
		Past     *int `json:"past"`
	} `json:"dribbles"`
	Fouls struct {
		Drawn     *int `json:"drawn"`
		Committed *int `json:"committed"`
	} `json:"fouls"`
	Cards struct {
		Yellow *int `json:"yellow"`
		Red    *int `json:"red"`
	} `json:"cards"`
}

// FlexNum accepts the many shapes the API uses for numbers: 7, 1.88, "1.88",
// "56%", "25" and null.
type FlexNum struct{ V *float64 }

func (f *FlexNum) UnmarshalJSON(b []byte) error {
	s := strings.TrimSpace(string(b))
	if s == "null" || s == `""` {
		f.V = nil
		return nil
	}
	var str string
	if err := json.Unmarshal(b, &str); err == nil {
		s = strings.TrimSuffix(strings.TrimSpace(str), "%")
	}
	v, err := strconv.ParseFloat(s, 64)
	if err != nil {
		f.V = nil // unexpected text: treat as missing rather than failing the match
		return nil
	}
	f.V = &v
	return nil
}

// Int returns the value rounded to an int, or nil.
func (f FlexNum) Int() *int {
	if f.V == nil {
		return nil
	}
	v := int(*f.V + 0.5)
	return &v
}
