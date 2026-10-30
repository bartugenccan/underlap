package baseline

import (
	"fmt"
	"io"
	"math"
	"sort"
	"strings"
	"text/tabwriter"
)

// MarketName is the pseudo-model that forecasts with the market's own prices.
const MarketName = "market"

// Score accumulates forecast quality. Lower Brier and log loss are better.
//   - Brier: sum over H/D/A of (p - outcome)^2, from 0 (perfect) to 2.
//   - Log loss: -ln(p of what happened). Punishes confident misses hard.
//   - Accuracy: share of matches where the most likely outcome happened.
type Score struct {
	N       int
	brier   float64
	logLoss float64
	correct int
}

func (s *Score) Add(p Probs, o Outcome) {
	s.N++
	for _, k := range []Outcome{Home, Draw, Away} {
		hit := 0.0
		if k == o {
			hit = 1
		}
		d := p.of(k) - hit
		s.brier += d * d
	}
	s.logLoss -= math.Log(math.Max(p.of(o), 1e-12))
	if argmax(p) == o {
		s.correct++
	}
}

func (s *Score) Brier() float64    { return s.brier / float64(s.N) }
func (s *Score) LogLoss() float64  { return s.logLoss / float64(s.N) }
func (s *Score) Accuracy() float64 { return float64(s.correct) / float64(s.N) }

func argmax(p Probs) Outcome {
	switch {
	case p.H >= p.D && p.H >= p.A:
		return Home
	case p.D >= p.A:
		return Draw
	default:
		return Away
	}
}

// Report holds one score per model for one season (or "all").
type Report struct {
	Season string
	Scores map[string]*Score
}

// Backtest walks forward through matches day by day: every model forecasts all
// of a day's matches, then observes their results. Seasons before evalFrom
// only train the models. Only matches with market odds are scored, so every
// model, the market included, is judged on exactly the same matches.
func Backtest(matches []Match, models []Model, evalFrom string) []Report {
	sorted := append([]Match(nil), matches...)
	sort.SliceStable(sorted, func(i, j int) bool { return sorted[i].Kickoff.Before(sorted[j].Kickoff) })

	bySeason := map[string]*Report{}
	all := &Report{Season: "all", Scores: map[string]*Score{}}
	var order []string

	add := func(season, model string, p Probs, o Outcome) {
		r, ok := bySeason[season]
		if !ok {
			r = &Report{Season: season, Scores: map[string]*Score{}}
			bySeason[season] = r
			order = append(order, season)
		}
		for _, rep := range []*Report{r, all} {
			if rep.Scores[model] == nil {
				rep.Scores[model] = &Score{}
			}
			rep.Scores[model].Add(p, o)
		}
	}

	for start := 0; start < len(sorted); {
		day := sorted[start].Kickoff.UTC().Format("2006-01-02")
		end := start
		for end < len(sorted) && sorted[end].Kickoff.UTC().Format("2006-01-02") == day {
			end++
		}
		for _, m := range sorted[start:end] {
			if m.Season < evalFrom || m.Market == nil {
				continue
			}
			o := m.Outcome()
			for _, model := range models {
				add(m.Season, model.Name(), model.Predict(m.Home, m.Away, m.Kickoff), o)
			}
			add(m.Season, MarketName, *m.Market, o)
		}
		for _, m := range sorted[start:end] {
			for _, model := range models {
				model.Observe(m)
			}
		}
		start = end
	}

	reports := make([]Report, 0, len(order)+1)
	for _, s := range order {
		reports = append(reports, *bySeason[s])
	}
	if len(all.Scores) > 0 {
		reports = append(reports, *all)
	}
	return reports
}

// WriteReports prints one table per report, models sorted by log loss.
func WriteReports(w io.Writer, reports []Report) error {
	tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', tabwriter.AlignRight)
	for _, r := range reports {
		names := make([]string, 0, len(r.Scores))
		for n := range r.Scores {
			names = append(names, n)
		}
		sort.Slice(names, func(i, j int) bool { return r.Scores[names[i]].LogLoss() < r.Scores[names[j]].LogLoss() })

		n := r.Scores[names[0]].N
		fmt.Fprintf(tw, "season %s (%d matches)\t\t\t\t\n", r.Season, n)
		fmt.Fprintln(tw, "model\tlog loss\tbrier\taccuracy\t")
		for _, name := range names {
			s := r.Scores[name]
			fmt.Fprintf(tw, "%s\t%.4f\t%.4f\t%.1f%%\t\n", name, s.LogLoss(), s.Brier(), 100*s.Accuracy())
		}
		fmt.Fprintln(tw, strings.Repeat("\t", 4))
	}
	return tw.Flush()
}
