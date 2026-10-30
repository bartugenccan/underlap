-- +goose Up

-- Seasons use football-data.co.uk style codes: '2627' = 2026-27.
CREATE TABLE seasons (
    id         SERIAL PRIMARY KEY,
    code       TEXT NOT NULL UNIQUE,
    name       TEXT NOT NULL,
    start_year INT  NOT NULL
);

CREATE TABLE teams (
    id         SERIAL PRIMARY KEY,
    name       TEXT NOT NULL UNIQUE,
    short_name TEXT
);

-- Every source spells team names differently ("Fenerbahce", "Fenerbahçe", api ids...).
-- Ingesters resolve names through this table instead of trusting raw strings.
CREATE TABLE team_aliases (
    source  TEXT NOT NULL,
    alias   TEXT NOT NULL,
    team_id INT  NOT NULL REFERENCES teams (id),
    PRIMARY KEY (source, alias)
);

CREATE TABLE coaches (
    id          SERIAL PRIMARY KEY,
    name        TEXT NOT NULL,
    nationality TEXT,
    birth_date  DATE
);

CREATE TABLE team_coach_spells (
    team_id    INT  NOT NULL REFERENCES teams (id),
    coach_id   INT  NOT NULL REFERENCES coaches (id),
    start_date DATE NOT NULL,
    end_date   DATE,
    PRIMARY KEY (team_id, coach_id, start_date)
);

CREATE TABLE players (
    id                  SERIAL PRIMARY KEY,
    name                TEXT     NOT NULL,
    birth_date          DATE,
    nationality         TEXT,
    preferred_foot      TEXT CHECK (preferred_foot IN ('left', 'right', 'both')),
    primary_position    TEXT,
    secondary_positions TEXT[]   NOT NULL DEFAULT '{}',
    height_cm           SMALLINT,
    weight_kg           SMALLINT
);

CREATE TABLE player_aliases (
    source      TEXT NOT NULL,
    external_id TEXT NOT NULL,
    player_id   INT  NOT NULL REFERENCES players (id),
    PRIMARY KEY (source, external_id)
);

CREATE TABLE referees (
    id   SERIAL PRIMARY KEY,
    name TEXT NOT NULL UNIQUE
);

-- A league season is a double round robin, so (season, home, away) identifies a match.
-- Goals stay NULL until the match is played.
CREATE TABLE matches (
    id            SERIAL PRIMARY KEY,
    season_id     INT         NOT NULL REFERENCES seasons (id),
    round         TEXT,
    kickoff_at    TIMESTAMPTZ NOT NULL,
    home_team_id  INT         NOT NULL REFERENCES teams (id),
    away_team_id  INT         NOT NULL REFERENCES teams (id),
    home_goals    SMALLINT,
    away_goals    SMALLINT,
    ht_home_goals SMALLINT,
    ht_away_goals SMALLINT,
    referee_id    INT REFERENCES referees (id),
    venue         TEXT,
    UNIQUE (season_id, home_team_id, away_team_id),
    CHECK (home_team_id <> away_team_id)
);

CREATE INDEX matches_kickoff_idx ON matches (kickoff_at);

CREATE TABLE match_sources (
    source      TEXT NOT NULL,
    external_id TEXT NOT NULL,
    match_id    INT  NOT NULL REFERENCES matches (id),
    PRIMARY KEY (source, external_id)
);

CREATE TABLE team_match_stats (
    match_id        INT  NOT NULL REFERENCES matches (id),
    team_id         INT  NOT NULL REFERENCES teams (id),
    source          TEXT NOT NULL,
    shots           SMALLINT,
    shots_on_target SMALLINT,
    corners         SMALLINT,
    fouls           SMALLINT,
    yellow_cards    SMALLINT,
    red_cards       SMALLINT,
    possession      NUMERIC(4, 1),
    xg              NUMERIC(5, 2),
    PRIMARY KEY (match_id, team_id, source)
);

-- Shape per phase. 'nominal' is the announced formation; the in/out of possession
-- shapes are what the Phase Lab (Faz 1) reasons about.
CREATE TABLE team_match_shapes (
    match_id  INT  NOT NULL REFERENCES matches (id),
    team_id   INT  NOT NULL REFERENCES teams (id),
    phase     TEXT NOT NULL CHECK (phase IN ('nominal', 'in_possession', 'out_of_possession')),
    formation TEXT NOT NULL,
    source    TEXT NOT NULL,
    PRIMARY KEY (match_id, team_id, phase, source)
);

CREATE TABLE appearances (
    match_id       INT     NOT NULL REFERENCES matches (id),
    player_id      INT     NOT NULL REFERENCES players (id),
    team_id        INT     NOT NULL REFERENCES teams (id),
    started        BOOLEAN NOT NULL,
    position       TEXT,
    role           TEXT,
    shirt_number   SMALLINT,
    minute_on      SMALLINT,
    minute_off     SMALLINT,
    minutes_played SMALLINT,
    PRIMARY KEY (match_id, player_id)
);

CREATE TABLE player_match_stats (
    match_id            INT  NOT NULL REFERENCES matches (id),
    player_id           INT  NOT NULL REFERENCES players (id),
    source              TEXT NOT NULL,
    touches             SMALLINT,
    aerial_won          SMALLINT,
    aerial_lost         SMALLINT,
    ground_won          SMALLINT,
    ground_lost         SMALLINT,
    tackles             SMALLINT,
    interceptions       SMALLINT,
    passes              SMALLINT,
    passes_accurate     SMALLINT,
    long_balls          SMALLINT,
    long_balls_accurate SMALLINT,
    key_passes          SMALLINT,
    progressive_carries SMALLINT,
    dribbles_attempted  SMALLINT,
    dribbles_won        SMALLINT,
    possession_lost     SMALLINT,
    shots               SMALLINT,
    shots_on_target     SMALLINT,
    goals               SMALLINT,
    assists             SMALLINT,
    xg                  NUMERIC(5, 2),
    xa                  NUMERIC(5, 2),
    avg_x               NUMERIC(5, 2),
    avg_y               NUMERIC(5, 2),
    rating              NUMERIC(4, 2),
    PRIMARY KEY (match_id, player_id, source)
);

CREATE TABLE match_events (
    id                BIGSERIAL PRIMARY KEY,
    match_id          INT      NOT NULL REFERENCES matches (id),
    minute            SMALLINT NOT NULL,
    extra_minute      SMALLINT,
    team_id           INT      NOT NULL REFERENCES teams (id),
    player_id         INT REFERENCES players (id),
    related_player_id INT REFERENCES players (id),
    type              TEXT     NOT NULL CHECK (type IN (
        'goal', 'own_goal', 'penalty_goal', 'missed_penalty',
        'yellow', 'second_yellow', 'red', 'sub', 'var')),
    detail            TEXT,
    source            TEXT     NOT NULL
);

CREATE INDEX match_events_match_idx ON match_events (match_id);

-- Decimal odds. 'close' is the last price before kick-off; the baseline model is
-- judged against the margin-free probabilities implied by these.
CREATE TABLE odds (
    match_id  INT          NOT NULL REFERENCES matches (id),
    bookmaker TEXT         NOT NULL,
    kind      TEXT         NOT NULL CHECK (kind IN ('open', 'close')),
    market    TEXT         NOT NULL DEFAULT '1x2',
    home      NUMERIC(7, 3) NOT NULL,
    draw      NUMERIC(7, 3) NOT NULL,
    away      NUMERIC(7, 3) NOT NULL,
    source    TEXT         NOT NULL,
    PRIMARY KEY (match_id, bookmaker, kind, market)
);

-- Traits come from two places: derived from data, or tagged by an analyst with
-- evidence (a match and a video timestamp). Analyst tags must name their author.
CREATE TABLE player_traits (
    id                  BIGSERIAL PRIMARY KEY,
    player_id           INT         NOT NULL REFERENCES players (id),
    trait               TEXT        NOT NULL,
    value               NUMERIC,
    note                TEXT,
    source              TEXT        NOT NULL CHECK (source IN ('data', 'analyst')),
    evidence_match_id   INT REFERENCES matches (id),
    evidence_url        TEXT,
    evidence_ts_seconds INT,
    author              TEXT,
    created_at          TIMESTAMPTZ NOT NULL DEFAULT now(),
    CHECK (source = 'data' OR author IS NOT NULL)
);

CREATE INDEX player_traits_player_idx ON player_traits (player_id);

-- +goose Down
DROP TABLE player_traits;
DROP TABLE odds;
DROP TABLE match_events;
DROP TABLE player_match_stats;
DROP TABLE appearances;
DROP TABLE team_match_shapes;
DROP TABLE team_match_stats;
DROP TABLE match_sources;
DROP TABLE matches;
DROP TABLE referees;
DROP TABLE player_aliases;
DROP TABLE players;
DROP TABLE team_coach_spells;
DROP TABLE coaches;
DROP TABLE team_aliases;
DROP TABLE teams;
DROP TABLE seasons;
