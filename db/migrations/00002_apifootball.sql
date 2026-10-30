-- +goose Up

-- Forfeits (e.g. Gaziantep and Hatayspor withdrew after the February 2023
-- earthquake and their remaining matches were awarded 3-0). They were never
-- played, so models must not learn from them.
ALTER TABLE matches ADD COLUMN awarded BOOLEAN NOT NULL DEFAULT false;

-- grid is API-Football's "row:column" slot in the announced shape (row 1 is
-- the goalkeeper): the starting position the Phase Lab will reason from.
ALTER TABLE appearances
    ADD COLUMN grid    TEXT,
    ADD COLUMN captain BOOLEAN NOT NULL DEFAULT false;

-- API-Football reports duels as a total; the aerial/ground split stays NULL
-- until a source that has it (SofaScore) is added.
ALTER TABLE player_match_stats
    ADD COLUMN duels_total     SMALLINT,
    ADD COLUMN duels_won       SMALLINT,
    ADD COLUMN fouls_committed SMALLINT,
    ADD COLUMN fouls_drawn     SMALLINT,
    ADD COLUMN saves           SMALLINT,
    ADD COLUMN goals_conceded  SMALLINT,
    ADD COLUMN yellow_cards    SMALLINT,
    ADD COLUMN red_cards       SMALLINT;

ALTER TABLE team_match_stats
    ADD COLUMN shots_inside_box SMALLINT,
    ADD COLUMN passes           SMALLINT,
    ADD COLUMN passes_accurate  SMALLINT,
    ADD COLUMN saves            SMALLINT,
    ADD COLUMN goals_prevented  NUMERIC(5, 2);

-- One coach per team per match, straight from the lineup. Spells in
-- team_coach_spells are derived from these.
CREATE TABLE match_coaches (
    match_id INT NOT NULL REFERENCES matches (id),
    team_id  INT NOT NULL REFERENCES teams (id),
    coach_id INT NOT NULL REFERENCES coaches (id),
    PRIMARY KEY (match_id, team_id)
);

CREATE TABLE coach_aliases (
    source      TEXT NOT NULL,
    external_id TEXT NOT NULL,
    coach_id    INT  NOT NULL REFERENCES coaches (id),
    PRIMARY KEY (source, external_id)
);

-- Events are replaced per match and source on re-ingest.
CREATE INDEX match_events_match_source_idx ON match_events (match_id, source);

-- +goose Down
DROP INDEX match_events_match_source_idx;
DROP TABLE coach_aliases;
DROP TABLE match_coaches;
ALTER TABLE team_match_stats
    DROP COLUMN shots_inside_box, DROP COLUMN passes, DROP COLUMN passes_accurate,
    DROP COLUMN saves, DROP COLUMN goals_prevented;
ALTER TABLE player_match_stats
    DROP COLUMN duels_total, DROP COLUMN duels_won, DROP COLUMN fouls_committed,
    DROP COLUMN fouls_drawn, DROP COLUMN saves, DROP COLUMN goals_conceded,
    DROP COLUMN yellow_cards, DROP COLUMN red_cards;
ALTER TABLE appearances DROP COLUMN grid, DROP COLUMN captain;
ALTER TABLE matches DROP COLUMN awarded;
