package baseline

import (
	"fmt"
	"math"
	"math/rand/v2"
	"sort"
	"testing"
	"time"
)

// simLeague plays double round robins between teams with known strengths, so
// tests can check that models recover what generated the data.
type simLeague struct {
	att, def map[string]float64
	c, home  float64
	rng      *rand.Rand
}

func newSimLeague(seed uint64, teams int) *simLeague {
	l := &simLeague{att: map[string]float64{}, def: map[string]float64{}, c: 1.3, home: 1.3, rng: rand.New(rand.NewPCG(seed, seed))}
	for i := 0; i < teams; i++ {
		name := fmt.Sprintf("T%02d", i)
		// Team 0 is the strongest, the last team the weakest.
		q := 1 - 2*float64(i)/float64(teams-1) // +1 .. -1
		l.att[name] = math.Exp(0.35 * q)
		l.def[name] = math.Exp(-0.3 * q)
	}
	return l
}

func (l *simLeague) poisson(lambda float64) int {
	// Knuth's method; fine for small lambdas.
	limit, k, p := math.Exp(-lambda), 0, 1.0
	for {
		p *= l.rng.Float64()
		if p <= limit {
			return k
		}
		k++
	}
}

func (l *simLeague) names() []string {
	var names []string
	for n := range l.att {
		names = append(names, n)
	}
	sort.Strings(names) // map order is random; the fixture list must not be
	return names
}

func (l *simLeague) season(code string, start time.Time) []Match {
	names := l.names()
	var out []Match
	day := start
	for _, h := range names {
		for _, a := range names {
			if h == a {
				continue
			}
			lh := l.c * l.att[h] * l.def[a] * l.home
			la := l.c * l.att[a] * l.def[h]
			p := Probs{}
			for i := 0; i <= 10; i++ {
				for j := 0; j <= 10; j++ {
					x := poissonPMF(i, lh) * poissonPMF(j, la)
					switch {
					case i > j:
						p.H += x
					case i == j:
						p.D += x
					default:
						p.A += x
					}
				}
			}
			p = p.normalized()
			out = append(out, Match{Season: code, Kickoff: day, Home: h, Away: a,
				HomeGoals: l.poisson(lh), AwayGoals: l.poisson(la), Market: &p})
			day = day.Add(13 * time.Hour)
		}
	}
	return out
}

func TestPoissonRecoversStrengths(t *testing.T) {
	l := newSimLeague(1, 18)
	start := time.Date(2024, 8, 1, 18, 0, 0, 0, time.UTC)
	matches := append(l.season("2425", start), l.season("2526", start.AddDate(1, 0, 0))...)

	p := NewPoisson()
	p.HalfLifeDays = 10000 // effectively unweighted: the generator does not drift
	for _, m := range matches {
		p.Observe(m)
	}
	at := matches[len(matches)-1].Kickoff.Add(24 * time.Hour)
	p.fit(at)

	// Two seasons are a small sample, so single rates are noisy; the ordering
	// of the teams is what has to come back.
	names := l.names()
	var trueAtt, fitAtt, trueDef, fitDef []float64
	for _, n := range names {
		trueAtt, fitAtt = append(trueAtt, l.att[n]), append(fitAtt, p.att[n])
		trueDef, fitDef = append(trueDef, l.def[n]), append(fitDef, p.def[n])
	}
	if r := spearman(trueAtt, fitAtt); r < 0.85 {
		t.Errorf("attack ranking correlation = %.2f, want >= 0.85", r)
	}
	if r := spearman(trueDef, fitDef); r < 0.85 {
		t.Errorf("defence ranking correlation = %.2f, want >= 0.85", r)
	}
	if math.Abs(p.home-l.home) > 0.15 {
		t.Fatalf("home advantage = %.3f, want about %.2f", p.home, l.home)
	}

	pr := p.Predict("T00", "T17", at)
	if s := pr.H + pr.D + pr.A; math.Abs(s-1) > 1e-9 {
		t.Fatalf("probabilities sum to %v", s)
	}
	if pr.H < 0.6 {
		t.Fatalf("strongest at home vs weakest: P(home) = %.2f, want a clear favourite", pr.H)
	}
}

// spearman is the rank correlation of two equally long samples without ties.
func spearman(x, y []float64) float64 {
	rank := func(v []float64) []float64 {
		idx := make([]int, len(v))
		for i := range idx {
			idx[i] = i
		}
		sort.Slice(idx, func(a, b int) bool { return v[idx[a]] < v[idx[b]] })
		r := make([]float64, len(v))
		for pos, i := range idx {
			r[i] = float64(pos)
		}
		return r
	}
	rx, ry := rank(x), rank(y)
	var d2 float64
	for i := range rx {
		d := rx[i] - ry[i]
		d2 += d * d
	}
	n := float64(len(x))
	return 1 - 6*d2/(n*(n*n-1))
}

func TestPoissonFitConverges(t *testing.T) {
	p := NewPoisson()
	for i, m := range newSimLeague(4, 18).season("2526", time.Date(2025, 8, 1, 18, 0, 0, 0, time.UTC)) {
		p.Observe(m)
		if i == 0 || i%40 != 39 {
			continue
		}
		p.fit(m.Kickoff.Add(time.Hour))
		if p.lastIters >= p.maxIter() {
			t.Fatalf("after %d matches the fit hit the %d iteration cap", i+1, p.maxIter())
		}
		for team, a := range p.att {
			if math.IsNaN(a) || math.IsNaN(p.def[team]) {
				t.Fatalf("NaN strength for %s", team)
			}
		}
	}
}

func TestPoissonNewcomerIsWeakerThanAverage(t *testing.T) {
	p := NewPoisson()
	for _, m := range newSimLeague(2, 10).season("2526", time.Date(2025, 8, 1, 0, 0, 0, 0, time.UTC)) {
		p.Observe(m)
	}
	at := time.Date(2026, 8, 1, 0, 0, 0, 0, time.UTC)
	newVsAvg := p.Predict("Promoted", "T05", at)
	avgVsAvg := p.Predict("T04", "T05", at)
	if newVsAvg.H >= avgVsAvg.H {
		t.Fatalf("newcomer P(home win) %.3f should be below an average side's %.3f", newVsAvg.H, avgVsAvg.H)
	}
}

func TestEloZeroSumAndValidProbs(t *testing.T) {
	e := NewElo()
	e.Observe(Match{Home: "A", Away: "B", HomeGoals: 3, AwayGoals: 0})
	if e.ratings["A"] <= 1500 || e.ratings["B"] >= 1500 {
		t.Fatalf("winner must gain, loser must lose: %v", e.ratings)
	}
	if s := e.ratings["A"] + e.ratings["B"]; math.Abs(s-3000) > 1e-9 {
		t.Fatalf("ratings not zero-sum: total %v", s)
	}
	for _, gap := range []float64{-800, -200, 0, 200, 800} {
		e.ratings["X"], e.ratings["Y"] = 1500+gap, 1500
		p := e.Predict("X", "Y", time.Time{})
		if p.H < 0 || p.D < 0 || p.A < 0 || math.Abs(p.H+p.D+p.A-1) > 1e-9 {
			t.Fatalf("gap %v: invalid probs %+v", gap, p)
		}
	}
}

func TestBacktestModelsBeatUniform(t *testing.T) {
	l := newSimLeague(3, 18)
	start := time.Date(2023, 8, 1, 18, 0, 0, 0, time.UTC)
	var matches []Match
	for i, code := range []string{"2324", "2425", "2526"} {
		matches = append(matches, l.season(code, start.AddDate(i, 0, 0))...)
	}
	matches[len(matches)-1].Market = nil // unscored but still observed

	reports := Backtest(matches, []Model{Uniform{}, NewBaseRates(), NewElo(), NewPoisson()}, "2425")
	all := reports[len(reports)-1]
	if all.Season != "all" {
		t.Fatalf("last report = %q, want all", all.Season)
	}
	if want := 2*306 - 1; all.Scores["poisson"].N != want {
		t.Fatalf("scored %d matches, want %d (first season trains only, no-market match skipped)", all.Scores["poisson"].N, want)
	}
	u := all.Scores["uniform"].LogLoss()
	for _, name := range []string{"elo", "poisson", MarketName} {
		if got := all.Scores[name].LogLoss(); got >= u {
			t.Errorf("%s log loss %.4f should beat uniform %.4f", name, got, u)
		}
	}
	// The market here is the true generating model, so nothing should beat it by much.
	if all.Scores["poisson"].LogLoss() < all.Scores[MarketName].LogLoss()-0.01 {
		t.Errorf("poisson beat the true model: suspicious leakage")
	}
}

func TestImpliedProbsRemoveMargin(t *testing.T) {
	p := ImpliedProbs(2.0, 3.4, 3.6) // ~5% margin
	if math.Abs(p.H+p.D+p.A-1) > 1e-12 || p.H <= p.A {
		t.Fatalf("got %+v", p)
	}
}
