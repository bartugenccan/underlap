package phase

import "math"

// PassOption is one possible pass from the ball carrier.
type PassOption struct {
	To       string  `json:"to"`
	ToName   string  `json:"to_name"`
	Distance float64 `json:"distance"`
	// LaneOpen: no defender can reach any point of the pass line before the ball.
	LaneOpen bool `json:"lane_open"`
	// Interceptor is the defender who closes the lane, when it is closed.
	Interceptor string `json:"interceptor,omitempty"`
	// TimeOnBall is how long the receiver has between the ball arriving and
	// the first defender arriving. Negative means a defender gets there first.
	TimeOnBall float64 `json:"time_on_ball"`
	Presser    string  `json:"nearest_defender"`
	Free       bool    `json:"free"`
}

const (
	laneSkipStart = 1.5 // m the passer's own body covers
	laneSkipEnd   = 2.0 // m around the receiver, which TimeOnBall already judges
	laneStep      = 0.5 // m between checked points
)

func passOptions(s Scenario, prm Params) []PassOption {
	carrier, _ := find(s.Attackers, s.BallCarrier)
	var out []PassOption
	for _, r := range s.Attackers {
		if r.ID == carrier.ID {
			continue
		}
		d := dist(carrier.X, carrier.Y, r.X, r.Y)
		o := PassOption{To: r.ID, ToName: r.Name, Distance: round2(d), LaneOpen: true}

		ux, uy := (r.X-carrier.X)/d, (r.Y-carrier.Y)/d
		for t := laneSkipStart; t <= d-laneSkipEnd; t += laneStep {
			x, y := carrier.X+ux*t, carrier.Y+uy*t
			ball := t / prm.PassSpeed
			if td, who := firstArrival(s.Defenders, x, y, prm); td < ball {
				o.LaneOpen, o.Interceptor = false, who.Name
				break
			}
		}

		ballArrival := d / prm.PassSpeed
		td, presser := firstArrival(s.Defenders, r.X, r.Y, prm)
		o.TimeOnBall = round2(td - ballArrival)
		o.Presser = presser.Name
		o.Free = o.LaneOpen && o.TimeOnBall >= prm.FreeTime
		out = append(out, o)
	}
	return out
}

// nearestDefender returns the closest defender to a point and the distance.
func nearestDefender(s Scenario, x, y float64) (Player, float64) {
	best, who := math.Inf(1), Player{}
	for _, d := range s.Defenders {
		if v := dist(d.X, d.Y, x, y); v < best {
			best, who = v, d
		}
	}
	return who, best
}
