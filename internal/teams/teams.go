// Package teams maps every source's spelling of a club to one canonical name.
//
// The mapping is curated by hand in aliases.csv. Ingest refuses names that are
// not listed instead of guessing, because a guessed new team silently splits
// one club's history into two.
package teams

import (
	_ "embed"
	"encoding/csv"
	"fmt"
	"strings"
)

//go:embed aliases.csv
var aliasesCSV string

var canonical = mustParse(aliasesCSV)

func mustParse(s string) map[[2]string]string {
	rows, err := csv.NewReader(strings.NewReader(s)).ReadAll()
	if err != nil {
		panic(fmt.Sprintf("teams/aliases.csv: %v", err))
	}
	m := make(map[[2]string]string, len(rows))
	for i, r := range rows {
		if i == 0 {
			continue // header
		}
		if len(r) != 3 || r[0] == "" || r[1] == "" || r[2] == "" {
			panic(fmt.Sprintf("teams/aliases.csv line %d: want canonical,source,alias", i+1))
		}
		key := [2]string{r[1], r[2]}
		if prev, dup := m[key]; dup && prev != r[0] {
			panic(fmt.Sprintf("teams/aliases.csv line %d: %s/%s maps to both %q and %q", i+1, r[1], r[2], prev, r[0]))
		}
		m[key] = r[0]
	}
	return m
}

// Canonical returns the canonical club name for a source's alias.
func Canonical(source, alias string) (string, error) {
	if c, ok := canonical[[2]string{source, alias}]; ok {
		return c, nil
	}
	return "", fmt.Errorf("unknown team %q from %s: add it to internal/teams/aliases.csv", alias, source)
}
