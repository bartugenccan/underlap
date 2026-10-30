// Package api serves the Phase Lab and the match data the tactics board needs
// over HTTP/JSON.
package api

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strconv"

	"github.com/bartugenccan/underlap/internal/phase"
	"github.com/bartugenccan/underlap/internal/store"
)

// Store is the read side the API needs; a nil Store serves only the analyzer.
type Store interface {
	Matches(ctx context.Context, team, season string) ([]store.MatchSummary, error)
	Lineups(ctx context.Context, matchID int) (*store.MatchLineups, error)
}

const maxBody = 1 << 20 // a scenario is a few KB; anything near 1 MB is a mistake

// New returns the HTTP handler. allowOrigin is the one browser origin allowed
// to call the API (the local Next.js dev server).
func New(st Store, allowOrigin string) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("POST /api/phase/analyze", analyze)
	if st != nil {
		mux.HandleFunc("GET /api/matches", func(w http.ResponseWriter, r *http.Request) { matches(w, r, st) })
		mux.HandleFunc("GET /api/matches/{id}/lineups", func(w http.ResponseWriter, r *http.Request) { lineups(w, r, st) })
	}
	return cors(allowOrigin, mux)
}

func cors(origin string, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Origin") == origin {
			w.Header().Set("Access-Control-Allow-Origin", origin)
			w.Header().Set("Access-Control-Allow-Methods", "GET, POST, OPTIONS")
			w.Header().Set("Access-Control-Allow-Headers", "Content-Type")
			w.Header().Set("Vary", "Origin")
		}
		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		next.ServeHTTP(w, r)
	})
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func writeError(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]string{"error": msg})
}

func analyze(w http.ResponseWriter, r *http.Request) {
	var s phase.Scenario
	dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, maxBody))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&s); err != nil {
		writeError(w, http.StatusBadRequest, "invalid scenario JSON: "+err.Error())
		return
	}
	a, err := phase.Analyze(s)
	if err != nil {
		writeError(w, http.StatusUnprocessableEntity, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, a)
}

func matches(w http.ResponseWriter, r *http.Request, st Store) {
	team, season := r.URL.Query().Get("team"), r.URL.Query().Get("season")
	if team == "" || season == "" {
		writeError(w, http.StatusBadRequest, "team and season are required, e.g. ?team=Galatasaray&season=2425")
		return
	}
	ms, err := st.Matches(r.Context(), team, season)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, ms)
}

// LineupsResponse carries both XIs and a ready-to-analyze scenario seeded from
// them, with the chosen side in possession and its keeper on the ball.
type LineupsResponse struct {
	*store.MatchLineups
	Scenario phase.Scenario `json:"scenario"`
}

func lineups(w http.ResponseWriter, r *http.Request, st Store) {
	id, err := strconv.Atoi(r.PathValue("id"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "match id must be a number")
		return
	}
	attacking := r.URL.Query().Get("attacking")
	if attacking == "" {
		attacking = "home"
	}
	if attacking != "home" && attacking != "away" {
		writeError(w, http.StatusBadRequest, "attacking must be home or away")
		return
	}
	ml, err := st.Lineups(r.Context(), id)
	if errors.Is(err, store.ErrNotFound) {
		writeError(w, http.StatusNotFound, err.Error())
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	att, def := ml.Home, ml.Away
	if attacking == "away" {
		att, def = def, att
	}
	attackers, err := phase.Seed(att.Starters, true)
	if err == nil {
		var defenders []phase.Player
		if defenders, err = phase.Seed(def.Starters, false); err == nil {
			s := phase.Scenario{
				Name:          att.Team + " oyun kuruyor (" + att.Formation + ") - " + def.Team + " (" + def.Formation + ")",
				AttackingTeam: att.Team, DefendingTeam: def.Team,
				Attackers: attackers, Defenders: defenders,
			}
			for _, p := range attackers {
				if p.IsGK() {
					s.BallCarrier = p.ID
				}
			}
			if s.BallCarrier == "" && len(attackers) > 0 {
				s.BallCarrier = attackers[0].ID
			}
			writeJSON(w, http.StatusOK, LineupsResponse{ml, s})
			return
		}
	}
	writeError(w, http.StatusInternalServerError, "seed line-up: "+err.Error())
}
