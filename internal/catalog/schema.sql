-- The catalog's tables, created in whichever schema is being loaded.
-- Every column comes from the IMDb datasets; nothing here is derived
-- from an API. {{schema}} is replaced with catalog_next during a load.

CREATE SCHEMA IF NOT EXISTS {{schema}};

-- Unlogged for the load: a COPY into an unlogged table skips the WAL,
-- which is most of the cost of writing forty million rows. They are set
-- logged once the data is in, before anything is published.
CREATE UNLOGGED TABLE {{schema}}.titles (
    tconst          text PRIMARY KEY,
    primary_title   text NOT NULL,
    original_title  text NOT NULL,
    is_adult        boolean NOT NULL,
    start_year      int,
    genres          text[] NOT NULL DEFAULT '{}',
    -- IMDb's runtime in minutes, null where it has none: the Daily's
    -- Length fact, and nothing else reads it.
    runtime_minutes int
);

CREATE UNLOGGED TABLE {{schema}}.names (
    nconst       text PRIMARY KEY,
    primary_name text NOT NULL
);

CREATE UNLOGGED TABLE {{schema}}.principals (
    tconst    text NOT NULL,
    ordering  int  NOT NULL,
    nconst    text NOT NULL,
    category  text NOT NULL,
    character text,
    PRIMARY KEY (tconst, ordering)
);

CREATE UNLOGGED TABLE {{schema}}.directors (
    tconst text NOT NULL,
    nconst text NOT NULL,
    -- The order IMDb lists them in, which is the order the chips show.
    ordering int NOT NULL,
    PRIMARY KEY (tconst, nconst)
);

CREATE UNLOGGED TABLE {{schema}}.ratings (
    tconst         text PRIMARY KEY,
    average_rating numeric(3,1) NOT NULL,
    num_votes      int NOT NULL
);

-- The cold screen's pool: the best known movies of each era, ranked once
-- here rather than on every request.
--
-- Ranking them live means sorting a quarter of a million rows to choose
-- eight, and no index can help — the year is on `titles` and the votes
-- are on `ratings`, so neither ordering survives the join. It only
-- changes when a generation does, so it is settled at import.
--
-- Posters are deliberately not part of it. They arrive on their own
-- schedule, long after the import, so the pool holds candidates and the
-- read picks from whichever of them have a picture by then.
CREATE UNLOGGED TABLE {{schema}}.first_run (
    era       int  NOT NULL,
    tconst    text NOT NULL,
    num_votes int  NOT NULL,
    PRIMARY KEY (era, tconst)
);
