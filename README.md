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
| API-Football ingest (lineups, formations, minute-level events, player stats) | next |
| Realism references and team profiles (15-minute goal windows) | next |

Roadmap: Phase 1 is the **Phase Lab**, a deterministic engine that reasons about
shapes, marking and triggers. Phase 2 is a tactics-board UI, Phase 3 a stochastic
match engine on top of it.

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
internal/store        Postgres access: upserts and model queries
internal/baseline     Elo, Poisson, backtest and scoring
```
