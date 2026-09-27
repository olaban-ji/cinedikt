-- meta is never renamed by the daily swap. It holds what must outlive a
-- generation: the stamps that gate the next import, and the poster
-- addresses and release dates that cost an API call to learn.

CREATE SCHEMA IF NOT EXISTS meta;

CREATE TABLE IF NOT EXISTS meta.generation (
    id            int PRIMARY KEY DEFAULT 1 CHECK (id = 1),
    -- file -> {last_modified, etag, length}
    files         jsonb NOT NULL,
    row_counts    jsonb NOT NULL,
    imported_at   timestamptz NOT NULL
);

-- What the last HEAD saw, whether or not it led to an import. The
-- generation row only moves when something publishes, so without this
-- there is no way to tell "checked an hour ago, the files had not moved"
-- from "nothing has run for a day" except by reading logs.
CREATE TABLE IF NOT EXISTS meta.last_check (
    id         int PRIMARY KEY DEFAULT 1 CHECK (id = 1),
    checked_at timestamptz NOT NULL,
    -- file -> {last_modified, etag, length}, as the host reported them.
    files      jsonb,
    -- Why the check did or did not start an import.
    outcome    text NOT NULL
);

CREATE TABLE IF NOT EXISTS meta.posters (
    tconst     text PRIMARY KEY,
    poster_url text,
    released   date,
    -- ok once OMDb has answered, even when it had nothing to give;
    -- missing only when the lookup itself failed and is worth retrying;
    -- dead once the address it gave has been seen to 404.
    status     text NOT NULL CHECK (status IN ('ok', 'missing', 'dead')),
    fetched_at timestamptz NOT NULL,
    -- Which service the address came from, so a TMDb picture is not
    -- mistaken for one OMDb is holding back.
    source     text,
    -- When TMDb was last asked about this title, and when a reader last
    -- wanted a picture for it that was not there. The second is the
    -- queue: it is written by whatever noticed, and read by the job
    -- that repairs it.
    tmdb_at    timestamptz,
    wanted_at  timestamptz,
    -- What the poster averages to, as "#rrggbb". The opening screen
    -- fills a frame with it while the picture is still arriving, so a
    -- film shows its own colour before it shows itself.
    colour     char(7),
    -- How well known the title was when this row last mattered to the
    -- TMDb queue.
    --
    -- A snapshot, not a join. The real count lives in the live
    -- catalog's ratings table, and that whole schema is renamed on
    -- every publish — so no index on it can survive to order this
    -- queue, and ordering three hundred thousand rows by a joined
    -- column is the query this column exists to delete.
    votes      int
);

-- Databases that predate the columns above. Each is a no-op on a fresh
-- one, so both paths end at the same shape.
ALTER TABLE meta.posters ADD COLUMN IF NOT EXISTS source text;
ALTER TABLE meta.posters ADD COLUMN IF NOT EXISTS tmdb_at timestamptz;
ALTER TABLE meta.posters ADD COLUMN IF NOT EXISTS wanted_at timestamptz;
ALTER TABLE meta.posters ADD COLUMN IF NOT EXISTS colour char(7);
ALTER TABLE meta.posters ADD COLUMN IF NOT EXISTS votes int;
-- Widening the status check, once. Guarded because this file runs on
-- every process start, and ADD CONSTRAINT is not free: it validates
-- every row and holds ACCESS EXCLUSIVE while it does. On this table
-- that is a third of a second of blocked writes at each boot, against
-- a backfill that may be mid-pass in the container being replaced.
DO $$
BEGIN
    IF NOT EXISTS (
        SELECT 1 FROM pg_constraint
        WHERE conrelid = 'meta.posters'::regclass
          AND conname  = 'posters_status_check'
          AND pg_get_constraintdef(oid) LIKE '%dead%'
    ) THEN
        ALTER TABLE meta.posters DROP CONSTRAINT IF EXISTS posters_status_check;
        ALTER TABLE meta.posters ADD CONSTRAINT posters_status_check
            CHECK (status IN ('ok', 'missing', 'dead'));
    END IF;
END $$;

-- The backfill asks for these first.
CREATE INDEX IF NOT EXISTS posters_missing ON meta.posters (status) WHERE status = 'missing';

-- The TMDb fallback's queue, in the order the job reads it: what a
-- reader asked for first, then the best known of the rest.
--
-- Partial, so it holds only the rows that are actually waiting rather
-- than all three-quarters of a million. Both sort keys are on this
-- table, which is the point — the old index could only order by
-- wanted_at, so every page re-sorted the whole remaining queue by a
-- vote count joined from a schema that is renamed daily.
CREATE INDEX IF NOT EXISTS posters_tmdb_queue
    ON meta.posters (wanted_at DESC NULLS LAST, votes DESC, tconst)
    WHERE tmdb_at IS NULL
      AND (status = 'dead' OR poster_url IS NULL OR btrim(poster_url) = '');

DROP INDEX IF EXISTS meta.posters_want_tmdb;

-- Fill the snapshot in for rows written before the column existed.
--
-- Guarded twice over, because this file runs on every process start:
-- once on whether there is any row to fill, which an index makes
-- almost free, and once on whether there is a live catalog to read
-- votes from at all. A first start has neither.
DO $$
BEGIN
    IF to_regclass('catalog.ratings') IS NOT NULL
       AND EXISTS (
           SELECT 1 FROM meta.posters
           WHERE votes IS NULL AND tmdb_at IS NULL
           LIMIT 1
       )
    THEN
        UPDATE meta.posters p
        SET votes = coalesce(r.num_votes, 0)
        FROM catalog.titles t
        LEFT JOIN catalog.ratings r USING (tconst)
        WHERE p.tconst = t.tconst
          AND p.votes IS NULL
          AND p.tmdb_at IS NULL;
    END IF;
END $$;

-- Trigram search, installed into meta rather than wherever the search
-- path happens to point. An unqualified CREATE EXTENSION needs a valid
-- schema to land in, and a database whose public schema has been
-- dropped has none — which is a real state, not a hypothetical.
--
-- IF NOT EXISTS leaves it where it already is on a database that has
-- it, so the search path below covers both.
CREATE EXTENSION IF NOT EXISTS pg_trgm WITH SCHEMA meta;

-- TMDb's id for an IMDb title. Search speaks TMDb's ids; the catalog
-- speaks tconsts. This is the join, learned once per title and kept
-- across generations, the same way a poster is.
--
-- A row with a null tmdb_id is still an answer: TMDb was asked and has
-- no movie for that title, and asking again will not change it.
CREATE TABLE IF NOT EXISTS meta.tmdb (
    tconst   text PRIMARY KEY,
    tmdb_id  int,
    asked_at timestamptz NOT NULL
);

CREATE UNIQUE INDEX IF NOT EXISTS tmdb_id_key
    ON meta.tmdb (tmdb_id) WHERE tmdb_id IS NOT NULL;

-- The order the matcher walks titles it has not asked about yet.
-- Votes live here rather than in a join: the live catalog is renamed
-- out from under any index on it, which is the same reason posters
-- keeps its own copy.
CREATE TABLE IF NOT EXISTS meta.tmdb_queue (
    tconst text PRIMARY KEY,
    votes  int NOT NULL
);

CREATE INDEX IF NOT EXISTS tmdb_queue_order
    ON meta.tmdb_queue (votes DESC, tconst);

-- The share card for one movie, rendered once and kept. Rendering is
-- fonts, a poster fetch and a scale; a link pasted into a busy channel
-- is fetched by every reader's client at once, and none of them should
-- pay for that.
--
-- Keyed by version as well as movie, so a new poster or a new template
-- is a new row rather than an overwrite: an unfurler still holding the
-- old address gets the picture it cached, and the new address gets the
-- new one.
CREATE TABLE IF NOT EXISTS meta.og_images (
    tconst  text NOT NULL,
    v       text NOT NULL,
    png     bytea NOT NULL,
    made_at timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (tconst, v)
);

-- What the Telegram notifier has already said: which alerts went out,
-- how loudly the live catalog has been called old, and which message
-- is the pinned board. One row, so a deploy picks up where the last
-- process stopped instead of saying it all again.
CREATE TABLE IF NOT EXISTS meta.notify (
    id         int PRIMARY KEY DEFAULT 1 CHECK (id = 1),
    state      jsonb NOT NULL,
    updated_at timestamptz NOT NULL DEFAULT now()
);
