package baseline

import (
	"math"
	"time"
)

// Poisson is the classic Maher / Dixon-Coles team-strength model without the
// low-score correction:
//
//	home goals ~ Poisson(c * att[home] * def[away] * homeAdv)
//	away goals ~ Poisson(c * att[away] * def[home])
//
// att > 1 means a team scores more than average, def > 1 means it concedes
// more than average. Older matches count less: a match HalfLifeDays old has
// half the weight of one played today. Parameters are refitted whenever the
// history or the forecast date changes.
type Poisson struct {
	HalfLifeDays float64
	MaxGoals     int
	// Shrink pulls every team toward average by this many pseudo-matches, so a
	// handful of results cannot produce extreme ratings.
	Shrink float64
	// NewcomerAtt/NewcomerDef rate a team with no matches in the window
	// (usually promoted): weaker than average on both ends.
	NewcomerAtt, NewcomerDef float64

	history []Match

	fittedLen int
	fittedDay string
	c, home   float64
	att, def  map[string]float64
	lastIters int // iterations the last fit needed; a fit that hits maxIter did not converge
}

func NewPoisson() *Poisson {
	return &Poisson{
		HalfLifeDays: 180,
		MaxGoals:     10,
		Shrink:       2,
		NewcomerAtt:  0.85,
		NewcomerDef:  1.15,
	}
}

func (*Poisson) Name() string { return "poisson" }

func (p *Poisson) Observe(m Match) { p.history = append(p.history, m) }

// Rates returns the expected goals for both sides. Exported for analysis and
// for later engines that want the baseline's view of a fixture.
func (p *Poisson) Rates(home, away string, at time.Time) (lambdaHome, lambdaAway float64) {
	p.fit(at)
	ah, dh := p.strength(home)
	aa, da := p.strength(away)
	return p.c * ah * da * p.home, p.c * aa * dh
}

func (p *Poisson) Predict(home, away string, at time.Time) Probs {
	lh, la := p.Rates(home, away, at)
	var out Probs
	for i := 0; i <= p.MaxGoals; i++ {
		pi := poissonPMF(i, lh)
		for j := 0; j <= p.MaxGoals; j++ {
			pij := pi * poissonPMF(j, la)
			switch {
			case i > j:
				out.H += pij
			case i == j:
				out.D += pij
			default:
				out.A += pij
			}
		}
	}
	return out.normalized()
}

func (p *Poisson) strength(team string) (att, def float64) {
	if a, ok := p.att[team]; ok {
		return a, p.def[team]
	}
	return p.NewcomerAtt, p.NewcomerDef
}

type weighted struct {
	m Match
	w float64
}

func (p *Poisson) fit(at time.Time) {
	day := at.UTC().Format("2006-01-02")
	if p.fittedLen == len(p.history) && p.fittedDay == day && p.att != nil {
		return
	}
	p.fittedLen, p.fittedDay = len(p.history), day

	var data []weighted
	for _, m := range p.history {
		ageDays := at.Sub(m.Kickoff).Hours() / 24
		if ageDays < 0 {
			continue
		}
		w := math.Pow(0.5, ageDays/p.HalfLifeDays)
		if w < 0.01 {
			continue
		}
		data = append(data, weighted{m, w})
	}

	// Warm start from the previous fit: one new matchday barely moves the
	// optimum, so this converges in a few rounds instead of dozens.
	oldAtt, oldDef := p.att, p.def
	p.att, p.def = map[string]float64{}, map[string]float64{}
	if oldAtt == nil {
		p.c, p.home = 1.35, 1.25
	}
	if len(data) == 0 {
		return
	}
	for _, d := range data {
		for _, t := range []string{d.m.Home, d.m.Away} {
			if a, ok := oldAtt[t]; ok {
				p.att[t], p.def[t] = a, oldDef[t]
			} else {
				p.att[t], p.def[t] = 1, 1
			}
		}
	}

	// Weighted maximum likelihood by fixed-point iteration, with Shrink
	// pseudo-matches per team at the average rate (strength 1) built into the
	// likelihood. The pseudo-matches keep the optimum finite for a team that
	// has not scored or conceded yet, and stop three results from making a
	// team look like the best or worst side in the league.
	//
	// The objective is concave in the log-parameters and every update below is
	// the exact maximiser for its block with the others fixed, so the
	// iteration cannot cycle.
	//
	// One direction is nearly flat: multiplying every att and def by s and
	// dividing c by s² leaves every rate, and so the likelihood, unchanged;
	// only the pseudo-matches care. Block updates crawl along such a ridge, so
	// each round also jumps straight to the best s, which in closed form is
	// s = 2N / (Σatt + Σdef).
	prior := p.Shrink * 1.35 // pseudo-goals at a typical per-team scoring rate
	for iter := 0; iter < p.maxIter(); iter++ {
		p.lastIters = iter + 1
		maxChange := 0.0
		set := func(m map[string]float64, t string, v float64) {
			maxChange = math.Max(maxChange, math.Abs(v-m[t]))
			m[t] = v
		}

		attNum, attDen := map[string]float64{}, map[string]float64{}
		for _, d := range data {
			h, a := d.m.Home, d.m.Away
			attNum[h] += d.w * float64(d.m.HomeGoals)
			attDen[h] += d.w * p.c * p.def[a] * p.home
			attNum[a] += d.w * float64(d.m.AwayGoals)
			attDen[a] += d.w * p.c * p.def[h]
		}
		for t := range p.att {
			set(p.att, t, (attNum[t]+prior)/(attDen[t]+prior))
		}

		defNum, defDen := map[string]float64{}, map[string]float64{}
		for _, d := range data {
			h, a := d.m.Home, d.m.Away
			defNum[a] += d.w * float64(d.m.HomeGoals)
			defDen[a] += d.w * p.c * p.att[h] * p.home
			defNum[h] += d.w * float64(d.m.AwayGoals)
			defDen[h] += d.w * p.c * p.att[a]
		}
		for t := range p.def {
			set(p.def, t, (defNum[t]+prior)/(defDen[t]+prior))
		}

		var sum float64
		for t := range p.att {
			sum += p.att[t] + p.def[t]
		}
		s := 2 * float64(len(p.att)) / sum
		for t := range p.att {
			p.att[t] *= s
			p.def[t] *= s
		}
		p.c /= s * s

		var homeNum, homeDen, awayNum, awayDen float64
		for _, d := range data {
			h, a := d.m.Home, d.m.Away
			homeNum += d.w * float64(d.m.HomeGoals)
			homeDen += d.w * p.c * p.att[h] * p.def[a]
			awayNum += d.w * float64(d.m.AwayGoals)
			awayDen += d.w * p.att[a] * p.def[h]
		}
		// c is fitted on away goals (no home factor), home advantage on the
		// home goals given c. Together they reproduce both goal totals.
		newC := awayNum / awayDen
		newHome := homeNum / homeDen * p.c / newC
		maxChange = math.Max(maxChange, math.Max(math.Abs(newHome-p.home), math.Abs(newC-p.c)))
		p.home, p.c = newHome, newC

		if maxChange < 1e-6 {
			break
		}
	}
}

func (p *Poisson) maxIter() int { return 500 }
