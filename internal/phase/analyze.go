package phase

import (
	"fmt"
	"sort"
	"strings"
)

type LineGap struct {
	Deeper string  `json:"deeper"` // e.g. "savunma hattı"
	Higher string  `json:"higher"`
	Depth  float64 `json:"depth"` // m of empty grass between them
}

type Finding struct {
	Kind    string   `json:"kind"` // free_between_lines, free_player, between_lines_marked, overload, marking_lost, line_gap
	Text    string   `json:"text"`
	Players []string `json:"players,omitempty"`
}

type Analysis struct {
	Params       Params         `json:"params"`
	Findings     []Finding      `json:"findings"`
	Passes       []PassOption   `json:"passes"`
	BetweenLines []BetweenLines `json:"between_lines"`
	Lines        []Line         `json:"lines"`
	LineGaps     []LineGap      `json:"line_gaps"`
	Overloads    []Overload     `json:"overloads"`
	Zones        []Zone         `json:"zones"`
	Control      Control        `json:"control"`
}

// Analyze reads a scenario. It is a pure function of its input.
func Analyze(s Scenario) (Analysis, error) {
	if err := s.Validate(); err != nil {
		return Analysis{}, err
	}
	prm := s.params()
	a := Analysis{
		Params:       prm,
		Passes:       passOptions(s, prm),
		BetweenLines: betweenLines(s),
		Lines:        defensiveLines(s),
		Overloads:    overloads(s),
		Zones:        zones(s),
		Control:      pitchControl(s, prm),
	}
	zoneControl(a.Zones, a.Control)
	a.LineGaps = lineGaps(a.Lines)
	a.Findings = findings(s, a)
	return a, nil
}

func lineName(i, n int) string {
	switch {
	case i == 0:
		return "savunma hattı"
	case i == n-1:
		return "ön hat"
	case n == 4 && i == 2:
		return "ileri orta saha hattı"
	default:
		return "orta saha hattı"
	}
}

func lineGaps(lines []Line) []LineGap {
	var out []LineGap
	for i := 0; i+1 < len(lines); i++ {
		depth := lines[i].MinX - lines[i+1].MaxX
		if depth < 0 {
			depth = 0
		}
		out = append(out, LineGap{lineName(i, len(lines)), lineName(i+1, len(lines)), round2(depth)})
	}
	return out
}

const (
	markingLostAt = 5.0  // m between a man-marker and his target
	bigLineGap    = 12.0 // m of depth between two lines worth pointing out
)

func findings(s Scenario, a Analysis) []Finding {
	team := s.DefendingTeam
	if team == "" {
		team = "Rakip"
	}
	carrier, _ := find(s.Attackers, s.BallCarrier)
	passTo := map[string]PassOption{}
	for _, p := range a.Passes {
		passTo[p.To] = p
	}
	between := map[string]BetweenLines{}
	for _, b := range a.BetweenLines {
		between[b.Player] = b
	}

	var out []Finding
	// Attackers in input order keeps the output deterministic.
	for _, p := range s.Attackers {
		if p.ID == carrier.ID {
			continue
		}
		pass, b := passTo[p.ID], between[p.ID]
		_, isBetween := between[p.ID]
		// A free pass back to the keeper or a centre-back is true but not
		// news; findings only report options that keep or gain ground.
		forward := p.X >= carrier.X-2
		switch {
		case isBetween && pass.Free:
			out = append(out, Finding{"free_between_lines", fmt.Sprintf(
				"%s hatlar arasında boşta. Önünde %s geçildi, arkasında %s var. "+
					"Topu aldığında en yakın rakip (%s) %s sn sonra gelebilir; %s pas yolu açık.",
				p.Name, list(b.Bypassed, 3), list(b.Behind, 2), pass.Presser, num(pass.TimeOnBall), abl(carrier.Name)),
				[]string{p.ID}})
		case pass.Free && forward:
			out = append(out, Finding{"free_player", fmt.Sprintf(
				"%s boşta: %s pas yolu açık, en yakın rakip (%s) topun gelişinden %s sn sonra gelebilir.",
				p.Name, abl(carrier.Name), pass.Presser, num(pass.TimeOnBall)), []string{p.ID}})
		case isBetween:
			var why string
			switch {
			case pass.TimeOnBall < -0.2:
				why = fmt.Sprintf("%s toptan önce gelir", pass.Presser)
			case pass.TimeOnBall < 0:
				why = fmt.Sprintf("%s topla aynı anda gelir", pass.Presser)
			default:
				why = fmt.Sprintf("%s %s sn içinde gelir", pass.Presser, num(pass.TimeOnBall))
			}
			if !pass.LaneOpen {
				why = fmt.Sprintf("%s pas yolunu kapatıyor", pass.Interceptor)
			}
			out = append(out, Finding{"between_lines_marked", fmt.Sprintf(
				"%s hatlar arasında (%s geçildi) ama kapalı: %s.", p.Name, list(b.Bypassed, 3), why), []string{p.ID}})
		}
	}

	for _, o := range a.Overloads {
		out = append(out, Finding{"overload", fmt.Sprintf(
			"%s bölgesinde %s %d sayısal üstünlük: %s karşısında %s.",
			o.Name, dat(len(o.Attackers)), len(o.Defenders), list(o.Attackers, 5), orNobody(list(o.Defenders, 5))), nil})
	}

	for _, m := range s.Markings {
		if m.Mode != "man" {
			continue
		}
		d, _ := find(s.Defenders, m.DefenderID)
		t, _ := find(s.Attackers, m.TargetID)
		if gap := dist(d.X, d.Y, t.X, t.Y); gap > markingLostAt {
			out = append(out, Finding{"marking_lost", fmt.Sprintf(
				"%s, adam adama markaj yaptığı %s %s m uzakta.", d.Name, abl(t.Name), num(gap)), []string{t.ID, d.ID}})
		}
	}

	if len(a.LineGaps) > 0 {
		g := a.LineGaps[0]
		for _, x := range a.LineGaps[1:] {
			if x.Depth > g.Depth {
				g = x
			}
		}
		if g.Depth >= bigLineGap {
			out = append(out, Finding{"line_gap", fmt.Sprintf(
				"%s %s ile %s arasında %s m derinlik var.", team, g.Higher, g.Deeper, num(g.Depth)), nil})
		}
	}

	rank := map[string]int{"free_between_lines": 0, "free_player": 1, "between_lines_marked": 2, "overload": 3, "marking_lost": 4, "line_gap": 5}
	sort.SliceStable(out, func(i, j int) bool { return rank[out[i].Kind] < rank[out[j].Kind] })
	return out
}

// abl adds the Turkish ablative suffix ("-dan/-den/-tan/-ten") by vowel
// harmony on the last vowel and voicing of the last letter: Torreira'dan,
// Osimhen'den, Kanté'den, Batrakov'dan, Abdülkerim'den. Good enough for the
// foreign names football is full of.
func abl(name string) string {
	runes := []rune(strings.ToLower(name))
	front := false
	for i := len(runes) - 1; i >= 0; i-- {
		if strings.ContainsRune("aıou", runes[i]) {
			break
		}
		if strings.ContainsRune("eiöüéè", runes[i]) {
			front = true
			break
		}
	}
	d := "d"
	if len(runes) > 0 && strings.ContainsRune("çfhkpsşt", runes[len(runes)-1]) {
		d = "t"
	}
	if front {
		return name + "'" + d + "en"
	}
	return name + "'" + d + "an"
}

// dat adds the dative to a small count as analysts say it: 3'e 2, 2'ye 1.
func dat(n int) string {
	suffix := map[int]string{1: "e", 2: "ye", 3: "e", 4: "e", 5: "e", 6: "ya", 7: "ye", 8: "e", 9: "a", 10: "a", 11: "e"}[n]
	if suffix == "" {
		suffix = "e"
	}
	return fmt.Sprintf("%d'%s", n, suffix)
}

// num formats with one decimal and a Turkish decimal comma.
func num(v float64) string { return strings.Replace(fmt.Sprintf("%.1f", v), ".", ",", 1) }

func list(names []string, max int) string {
	if len(names) > max {
		names = names[:max]
	}
	switch len(names) {
	case 0:
		return ""
	case 1:
		return names[0]
	default:
		return strings.Join(names[:len(names)-1], ", ") + " ve " + names[len(names)-1]
	}
}

func orNobody(s string) string {
	if s == "" {
		return "kimse yok"
	}
	return s
}
