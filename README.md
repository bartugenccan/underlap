# Underlap

An explainable match-scenario sandbox for the Turkish Süper Lig. Analysts set up
shapes, marking schemes and triggers ("what if X presses Y?") and see what opens
up: free players, overloads, gaps between the lines. It goes deep on one league
instead of shallow on fifty.

> An *overlap* is the run everyone expects. The *underlap* is the inside run
> nobody sees coming.

## Status: Phase 0 (data + baseline)

| Piece | State |
|---|---|
| Postgres schema (teams, matches, appearances, player stats, events, odds, analyst-tagged player traits) | done |
| football-data.co.uk ingest (results, match stats, opening/closing odds) | done |
| Baseline models: Elo, time-decayed Poisson, walk-forward backtest vs the market | done |
| Canonical team names across sources (`internal/teams/aliases.csv`), forfeits flagged and kept out of models | done |
| API-Football ingest (lineups with pitch grid, formations, coaches, minute-level events, team and player stats, xG) | done, 2022-2024 backfilling |
| Realism references and team profiles (15-minute goal windows, xG vs goals, formations, minutes) | done |

First real backtest (1,422 matches, 2022-23 to now, log loss): market 0.945,
Poisson 0.984, Elo 0.986, league base rates 1.065, uniform 1.099. The ~0.04 gap
to the market is what a results-only model cannot see: injuries, line-ups, form.

## Phase Lab (Phase 1, v0)

A deterministic engine that reads one frozen moment (positions, marking, who
has the ball) and reports what an analyst would point at on a tactics board:
who is free, who sits between the lines, which passes are on, where one side
has more players, how deep the gaps between the defending lines are. Every
finding comes with its reason (Turkish text), and the same input always gives
the same output.

```sh
go run ./cmd/underlap phase analyze --file internal/phase/testdata/gsfb_guendouzi_after.json
go run ./cmd/underlap serve    # POST /api/phase/analyze, GET /api/matches, GET /api/matches/{id}/lineups
```

The test scenarios are taken from a pundit's pre-match "simulation" streams:
when Guendouzi steps out to press Torreira, Batrakov ends up free between
Fenerbahçe's lines; when Jakobs follows a dropping Mbappé, Lille's third man
gets free behind him. The physical model (reaction time, running and pass
speed, reach) is deliberately simple and returned with every analysis.

Not in v0: automatic reactions (man-markers following, zonal cover shifting)
and a saved scenario tree. The analyst moves the pieces; the engine reads the
result. Phase 2 is the tactics-board UI on top of this API, Phase 3 a
stochastic match engine.

## Quick start

Requires Go and Docker.

```sh
docker compose up -d                       # Postgres on localhost:54320
go run ./cmd/underlap migrate
go run ./cmd/underlap ingest footballdata  # reads data/raw/footballdata/T1_*.csv
go run ./cmd/underlap baseline backtest    # Elo + Poisson vs the closing market
go test ./...
```

`baseline backtest --csv-dir data/raw/footballdata` runs straight from the CSVs,
without the database.

Match details come from [API-Football](https://www.api-football.com). Put
`API_FOOTBALL_KEY=...` in `.env` (gitignored), then:

```sh
go run ./cmd/underlap fetch apifootball      # one day's quota; resumes where it stopped
go run ./cmd/underlap ingest apifootball     # cache -> database, no network
go run ./cmd/underlap analysis realism       # writes data/reference/realism.json
go run ./cmd/underlap analysis team-profile --team Galatasaray --season 2425
```

The free plan covers seasons 2022-2024 only, has no batch requests and allows
100 requests a day, so the full history takes about eleven daily runs. Raw
responses are cached in `data/cache/apifootball/` (gitignored) and never
requested twice. Galatasaray's matches are fetched first (`--priority-team`).

### Getting the raw data

football-data.co.uk is blocked by Turkish ISPs because it publishes betting odds.
The `fetch-footballdata` GitHub Actions workflow downloads the CSVs from a GitHub
runner twice a week and commits them to `data/raw/footballdata/`; run it by hand
from the Actions tab the first time. Outside Turkey,
`go run ./cmd/underlap fetch footballdata` does the same locally. The fetcher
refuses to save anything that is not a football-data CSV, so a block page never
ends up in the data.

## Reading the backtest

Every model forecasts a day's matches before it sees their results. Seasons
before `--eval-from` only train. Only matches with market odds are scored, so all
models are judged on the same matches.

- **Log loss** (lower is better) punishes confident misses hard.
- **Brier** (lower is better) is the squared error over home/draw/away.
- **market** is the margin-free closing price (Pinnacle first). Beating it
  consistently is the hard part. The baseline only needs to beat `uniform` and
  `base_rates` clearly and get close to the market.

## Layout

```
cmd/underlap/         CLI entry point
db/migrations/        SQL migrations (embedded, applied by `migrate`)
internal/footballdata football-data.co.uk fetch + CSV parsing
internal/apifootball  API-Football client, cache, daily backfill, normalisation
internal/teams        canonical team names for every source
internal/store        Postgres access: upserts and model queries
internal/baseline     Elo, Poisson, backtest and scoring
internal/analysis     league realism references and team profiles
internal/phase        Phase Lab: pitch control, zones, passing lanes, lines, findings
internal/api          HTTP/JSON API for the tactics board
data/raw/             source files fetched by CI (committed)
data/reference/       derived reference numbers (committed)
```
