// Package baseline holds the simple statistical models every later engine has
// to beat: Elo, a time-decayed Poisson team-strength model, and trivial
// reference lines (uniform, league base rates). The betting market is the bar.
package baseline

import (
	"math"
	"time"
)

// Probs is a 1X2 forecast: home win, draw, away win.
type Probs struct{ H, D, A float64 }

func (p Probs) normalized() Probs {
	s := p.H + p.D + p.A
	return Probs{p.H / s, p.D / s, p.A / s}
}

func (p Probs) of(o Outcome) float64 {
	switch o {
	case Home:
		return p.H
	case Draw:
		return p.D
	default:
		return p.A
	}
}

// OddsPreference lists which price to judge against, best first, as
// "bookmaker/kind". Pinnacle's closing line is the sharpest public price;
// market_max is never used because a maximum across books is not a forecast.
var OddsPreference = []string{
	"pinnacle/close", "market_avg/close", "bet365/close",
	"pinnacle/open", "market_avg/open", "bet365/open",
}

// ImpliedProbs removes the bookmaker margin from decimal odds by scaling the
// inverse prices so they sum to one.
func ImpliedProbs(home, draw, away float64) Probs {
	return Probs{1 / home, 1 / draw, 1 / away}.normalized()
}

type Outcome int

const (
	Home Outcome = iota
	Draw
	Away
)

// Match is a played league match as the models see it.
type Match struct {
	Season    string
	Kickoff   time.Time
	Home      string
	Away      string
	HomeGoals int
	AwayGoals int
	Market    *Probs // margin-free market probabilities; nil when no odds are known
}

func (m Match) Outcome() Outcome {
	switch {
	case m.HomeGoals > m.AwayGoals:
		return Home
	case m.HomeGoals == m.AwayGoals:
		return Draw
	default:
		return Away
	}
}

// Model forecasts a match from what it has observed so far. Backtests call
// Predict for every match of a day before calling Observe on any of them, so a
// model never sees a result before forecasting it.
type Model interface {
	Name() string
	Predict(home, away string, at time.Time) Probs
	Observe(m Match)
}

// Uniform is the know-nothing line: 1/3 each.
type Uniform struct{}

func (Uniform) Name() string                            { return "uniform" }
func (Uniform) Predict(string, string, time.Time) Probs { return Probs{1.0 / 3, 1.0 / 3, 1.0 / 3} }
func (Uniform) Observe(Match)                           {}

// BaseRates predicts the league's running home/draw/away frequencies for every
// match, ignoring who plays. It starts from a weak prior so early forecasts
// are not degenerate.
type BaseRates struct{ counts [3]float64 }

func NewBaseRates() *BaseRates {
	return &BaseRates{counts: [3]float64{4.5, 2.7, 2.8}} // ~10 pseudo-matches of a typical league
}

func (*BaseRates) Name() string { return "base_rates" }

func (b *BaseRates) Predict(string, string, time.Time) Probs {
	return Probs{b.counts[Home], b.counts[Draw], b.counts[Away]}.normalized()
}

func (b *BaseRates) Observe(m Match) { b.counts[m.Outcome()]++ }

func poissonPMF(k int, lambda float64) float64 {
	lg, _ := math.Lgamma(float64(k) + 1)
	return math.Exp(float64(k)*math.Log(lambda) - lambda - lg)
}
