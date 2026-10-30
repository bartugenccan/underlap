package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"strings"

	"github.com/bartugenccan/underlap/internal/phase"
)

func phaseAnalyze(_ context.Context, args []string) error {
	fs := flag.NewFlagSet("phase analyze", flag.ContinueOnError)
	file := fs.String("file", "", "scenario JSON file")
	asJSON := fs.Bool("json", false, "print the full analysis as JSON")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *file == "" {
		return fmt.Errorf("--file is required")
	}
	b, err := os.ReadFile(*file)
	if err != nil {
		return err
	}
	var s phase.Scenario
	if err := json.Unmarshal(b, &s); err != nil {
		return fmt.Errorf("%s: %w", *file, err)
	}
	a, err := phase.Analyze(s)
	if err != nil {
		return err
	}
	if *asJSON {
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		return enc.Encode(a)
	}

	fmt.Println(s.Name)
	fmt.Println("\nbulgular:")
	for _, f := range a.Findings {
		fmt.Printf("  [%s] %s\n", f.Kind, f.Text)
	}
	fmt.Println("\npas seçenekleri:")
	for _, p := range a.Passes {
		lane := "açık"
		if !p.LaneOpen {
			lane = "kapalı (" + p.Interceptor + ")"
		}
		free := ""
		if p.Free {
			free = "  BOŞTA"
		}
		fmt.Printf("  %-12s %5.1f m  yol %-22s topla süre %+5.2f sn (%s)%s\n", p.ToName, p.Distance, lane, p.TimeOnBall, p.Presser, free)
	}
	fmt.Println("\nsavunma hatları (derinden öne):")
	for _, l := range a.Lines {
		fmt.Printf("  x %5.1f-%5.1f  %s\n", l.MinX, l.MaxX, strings.Join(l.Members, ", "))
	}
	return nil
}
