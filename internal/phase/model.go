// Package phase is the Phase Lab: a deterministic engine that reads one frozen
// moment of a match (where everyone stands, who marks whom, who has the ball)
// and works out what an analyst would point at on a tactics board: who is
// free, where a side has more players, who sits between the lines, which pass
// is on. Same input, same output: there is no randomness anywhere.
//
// Frame: the pitch is 105 x 68 metres. The team in possession attacks left to
// right, from x=0 towards x=105. y=0 is the attacking team's left touchline.
// Both teams are given in this one frame.
package phase

import (
	"fmt"
	"math"
)

const (
	PitchLength = 105.0
	PitchWidth  = 68.0
)

type Player struct {
	ID   string  `json:"id"`
	Name string  `json:"name"`
	X    float64 `json:"x"`
	Y    float64 `json:"y"`
	// Role is free text except "GK", which marks the goalkeeper.
	Role string `json:"role,omitempty"`
	// Speed in m/s; 0 means Params.DefaultSpeed.
	Speed float64 `json:"speed,omitempty"`
}

func (p Player) IsGK() bool { return p.Role == "GK" }

type Marking struct {
	DefenderID string `json:"defender"`
	Mode       string `json:"mode"` // "zone" or "man"
	TargetID   string `json:"target,omitempty"`
}

// Params are the physical assumptions. They are deliberately simple and
// visible, so an analyst can see why the engine calls a player free.
type Params struct {
	PassSpeed    float64 `json:"pass_speed"`    // m/s along the ground
	Reaction     float64 `json:"reaction"`      // s before a player starts moving
	DefaultSpeed float64 `json:"default_speed"` // m/s
	Reach        float64 `json:"reach"`         // m a player can stretch to the ball
	FreeTime     float64 `json:"free_time"`     // s on the ball before a defender arrives that counts as free
	CellSize     float64 `json:"cell_size"`     // m, pitch control grid
}

func DefaultParams() Params {
	return Params{PassSpeed: 15, Reaction: 0.3, DefaultSpeed: 7, Reach: 1, FreeTime: 1, CellSize: 2.5}
}

type Scenario struct {
	Name          string    `json:"name,omitempty"`
	AttackingTeam string    `json:"attacking_team,omitempty"`
	DefendingTeam string    `json:"defending_team,omitempty"`
	Attackers     []Player  `json:"attackers"`
	Defenders     []Player  `json:"defenders"`
	BallCarrier   string    `json:"ball_carrier"`
	Markings      []Marking `json:"markings,omitempty"`
	Params        *Params   `json:"params,omitempty"`
}

func (s Scenario) params() Params {
	if s.Params == nil {
		return DefaultParams()
	}
	p := *s.Params
	d := DefaultParams()
	for _, f := range []struct {
		v   *float64
		def float64
	}{{&p.PassSpeed, d.PassSpeed}, {&p.Reaction, d.Reaction}, {&p.DefaultSpeed, d.DefaultSpeed},
		{&p.Reach, d.Reach}, {&p.FreeTime, d.FreeTime}, {&p.CellSize, d.CellSize}} {
		if *f.v <= 0 {
			*f.v = f.def
		}
	}
	return p
}

// Validate rejects scenarios the engine cannot reason about.
func (s Scenario) Validate() error {
	if len(s.Attackers) == 0 || len(s.Attackers) > 11 || len(s.Defenders) == 0 || len(s.Defenders) > 11 {
		return fmt.Errorf("want 1-11 players per side, got %d attackers and %d defenders", len(s.Attackers), len(s.Defenders))
	}
	ids := map[string]bool{}
	for _, p := range append(append([]Player(nil), s.Attackers...), s.Defenders...) {
		if p.ID == "" {
			return fmt.Errorf("player %q has no id", p.Name)
		}
		if ids[p.ID] {
			return fmt.Errorf("duplicate player id %q", p.ID)
		}
		ids[p.ID] = true
		if p.X < 0 || p.X > PitchLength || p.Y < 0 || p.Y > PitchWidth {
			return fmt.Errorf("%s at (%.1f, %.1f) is off the %gx%g pitch", p.ID, p.X, p.Y, PitchLength, PitchWidth)
		}
	}
	if _, ok := find(s.Attackers, s.BallCarrier); !ok {
		return fmt.Errorf("ball carrier %q is not an attacker", s.BallCarrier)
	}
	for _, m := range s.Markings {
		if _, ok := find(s.Defenders, m.DefenderID); !ok {
			return fmt.Errorf("marking: %q is not a defender", m.DefenderID)
		}
		switch m.Mode {
		case "zone":
		case "man":
			if _, ok := find(s.Attackers, m.TargetID); !ok {
				return fmt.Errorf("marking: %q marks %q, who is not an attacker", m.DefenderID, m.TargetID)
			}
		default:
			return fmt.Errorf("marking: mode %q, want zone or man", m.Mode)
		}
	}
	return nil
}

func find(ps []Player, id string) (Player, bool) {
	for _, p := range ps {
		if p.ID == id {
			return p, true
		}
	}
	return Player{}, false
}

func dist(ax, ay, bx, by float64) float64 { return math.Hypot(ax-bx, ay-by) }

// arrival is when a player can be at (x, y): reaction time plus running, with
// the last Reach metres covered by stretching.
func arrival(p Player, x, y float64, prm Params) float64 {
	speed := p.Speed
	if speed <= 0 {
		speed = prm.DefaultSpeed
	}
	d := math.Max(0, dist(p.X, p.Y, x, y)-prm.Reach)
	return prm.Reaction + d/speed
}

// firstArrival is the earliest any of ps can be at (x, y), and who that is.
func firstArrival(ps []Player, x, y float64, prm Params) (float64, Player) {
	best, who := math.Inf(1), Player{}
	for _, p := range ps {
		if t := arrival(p, x, y, prm); t < best {
			best, who = t, p
		}
	}
	return best, who
}

func round2(v float64) float64 { return math.Round(v*100) / 100 }
