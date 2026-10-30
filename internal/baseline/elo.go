package baseline

import (
	"math"
	"time"
)

// Elo is the World Football Elo variant: home advantage in rating points and a
// bigger update for bigger wins.
//
// Elo only gives an expected score E = P(home) + P(draw)/2, so the draw share
// has to come from somewhere. Draws are most likely between even teams, so the
// model uses pD = c * 2*min(E, 1-E) and learns c from the results it has seen,
// which keeps the average draw rate honest without a hand-tuned constant.
type Elo struct {
	K       float64 // update size
	HomeAdv float64 // rating points added to the home side
	Initial float64 // rating of teams in the first season seen
	// NewcomerGap is how far below the league mean a team that appears after
	// the first season starts. Those are almost always promoted sides.
	NewcomerGap float64

	ratings   map[string]float64
	games     int
	draws     float64
	drawShape float64 // running sum of 2*min(E, 1-E)
}

func NewElo() *Elo {
	return &Elo{K: 20, HomeAdv: 60, Initial: 1500, NewcomerGap: 80, ratings: map[string]float64{}}
}

func (*Elo) Name() string { return "elo" }

func (e *Elo) rating(team string) float64 {
	if r, ok := e.ratings[team]; ok {
		return r
	}
	if e.games < 200 || len(e.ratings) == 0 {
		return e.Initial
	}
	var sum float64
	for _, r := range e.ratings {
		sum += r
	}
	return sum/float64(len(e.ratings)) - e.NewcomerGap
}

func (e *Elo) expected(home, away string) float64 {
	diff := e.rating(home) + e.HomeAdv - e.rating(away)
	return 1 / (1 + math.Pow(10, -diff/400))
}

func (e *Elo) drawScale() float64 {
	// Prior: ~27% draws in an even-matches league, worth 50 games.
	c := (e.draws + 0.27*50) / (e.drawShape + 50)
	return math.Min(c, 1)
}

func (e *Elo) Predict(home, away string, _ time.Time) Probs {
	exp := e.expected(home, away)
	d := e.drawScale() * 2 * math.Min(exp, 1-exp)
	h := exp - d/2
	return Probs{h, d, 1 - h - d}
}

func (e *Elo) Observe(m Match) {
	exp := e.expected(m.Home, m.Away)
	e.games++
	e.drawShape += 2 * math.Min(exp, 1-exp)

	var score float64
	switch m.Outcome() {
	case Home:
		score = 1
	case Draw:
		score = 0.5
		e.draws++
	}

	gd := m.HomeGoals - m.AwayGoals
	if gd < 0 {
		gd = -gd
	}
	mult := 1.0
	switch {
	case gd == 2:
		mult = 1.5
	case gd >= 3:
		mult = (11 + float64(gd)) / 8
	}

	delta := e.K * mult * (score - exp)
	rh, ra := e.rating(m.Home), e.rating(m.Away)
	e.ratings[m.Home] = rh + delta
	e.ratings[m.Away] = ra - delta
}
