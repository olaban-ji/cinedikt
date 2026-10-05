-- meta is never renamed by the daily swap. It holds what must outlive a
-- generation: the stamps that gate the next import, and the poster
-- addresses, release dates, synopses, trailers, people's photos and
-- where-to-watch answers that cost an API call to learn.

CREATE SCHEMA IF NOT EXISTS meta;

CREATE TABLE IF NOT EXISTS meta.generation (
    id            int PRIMARY KEY DEFAULT 1 CHECK (id = 1),
    -- file -> {last_modified, etag, length}
    files         jsonb NOT NULL,
    row_counts    jsonb NOT NULL,
    imported_at   timestamptz NOT NULL
);

-- Nothing reads these. Dropping what is already gone costs nothing, so
-- this runs on every start like the rest of the file.
DROP TABLE IF EXISTS meta.last_check;
DROP INDEX IF EXISTS meta.posters_missing;

CREATE TABLE IF NOT EXISTS meta.posters (
    tconst     text PRIMARY KEY,
    poster_url text,
    released   date,
    -- Whether that date is TMDb's, filled in where OMDb had none. It is
    -- part of TMDb's answer, so it goes when the rest of that answer
    -- does: at a re-ask, or at the six-month backstop. OMDb's date stays.
    released_tmdb boolean NOT NULL DEFAULT false,
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

-- The TMDb fallback's queue, in the order the job reads it: what a
-- reader asked for first, then the best known of the rest.
--
-- Partial, so it holds only the rows that are actually waiting rather
-- than all three-quarters of a million. Both sort keys are on this
-- table, so a page is read in index order rather than re-sorting the
-- remaining queue by a vote count joined from a schema that is renamed
-- daily.
CREATE INDEX IF NOT EXISTS posters_tmdb_queue
    ON meta.posters (wanted_at DESC NULLS LAST, votes DESC, tconst)
    WHERE tmdb_at IS NULL
      AND (status = 'dead' OR poster_url IS NULL OR btrim(poster_url) = '');

-- TMDb's answers in the order they come due: the fallback's re-asks read
-- it oldest first, and the backstop clears what is past its time. Partial,
-- so it holds only the rows TMDb has been asked about.
CREATE INDEX IF NOT EXISTS posters_tmdb_asked
    ON meta.posters (tmdb_at) WHERE tmdb_at IS NOT NULL;

-- Trigram search, installed into meta rather than wherever the search
-- path happens to point. An unqualified CREATE EXTENSION needs a valid
-- schema to land in, and a database whose public schema has been
-- dropped has none — which is a real state, not a hypothetical.
--
-- IF NOT EXISTS leaves it where it already is on a database that has
-- it, so the search path below covers both.
CREATE EXTENSION IF NOT EXISTS pg_trgm WITH SCHEMA meta;

-- TMDb's id for an IMDb title. Search speaks TMDb's ids; the catalog
-- speaks tconsts. This is the join, learned ahead of time and kept
-- across generations, the same way a poster is. Like everything cached
-- from TMDb it is asked again after 150 days if search can still offer
-- its film, replacing the row in place, and one still unrenewed at 175
-- days is deleted.
--
-- A row with a null tmdb_id is still an answer: TMDb was asked and has
-- no movie for that title, and it is not asked again until that answer
-- comes due.
CREATE TABLE IF NOT EXISTS meta.tmdb (
    tconst   text PRIMARY KEY,
    tmdb_id  int,
    asked_at timestamptz NOT NULL
);

CREATE UNIQUE INDEX IF NOT EXISTS tmdb_id_key
    ON meta.tmdb (tmdb_id) WHERE tmdb_id IS NOT NULL;

-- The matcher's re-asks, oldest first, and the backstop's deletes.
CREATE INDEX IF NOT EXISTS tmdb_asked ON meta.tmdb (asked_at);

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

-- What a film is about, for the preview and the film panel. OMDb is the
-- source: the poster pass stores the plot with every answer it saves, and
-- the synopsis job asks for the titles that pass reached before it kept
-- plots. TMDb's overview arrives free on answers other jobs already save,
-- and fills in only where OMDb has nothing.
--
-- A null overview is an answer: nobody has a synopsis for that title. A
-- 'tmdb' row is refreshed or dropped after 150 days, which TMDb's terms
-- ask of anything cached from it; an 'omdb' row is kept.
--
-- omdb_at is when OMDb last answered for the title, whichever text is
-- shown; null until it has been asked. It is what the synopsis job goes
-- by, so an overview of TMDb's that arrived first never stops OMDb, the
-- better source, from being asked.
CREATE TABLE IF NOT EXISTS meta.synopses (
    tconst     text PRIMARY KEY,
    overview   text,
    source     text NOT NULL CHECK (source IN ('omdb', 'tmdb')),
    fetched_at timestamptz NOT NULL,
    omdb_at    timestamptz
);

-- The 'tmdb' rows in the order they come due.
CREATE INDEX IF NOT EXISTS synopses_tmdb_age
    ON meta.synopses (fetched_at) WHERE source = 'tmdb';

-- The titles the synopsis job has still to ask OMDb about: the ones a
-- reader has met (wanted_at), then the best known. Votes are copied here
-- for the same reason tmdb_queue copies them.
CREATE TABLE IF NOT EXISTS meta.synopsis_queue (
    tconst    text PRIMARY KEY,
    votes     int NOT NULL,
    wanted_at timestamptz
);

CREATE INDEX IF NOT EXISTS synopsis_queue_order
    ON meta.synopsis_queue (wanted_at DESC NULLS LAST, votes DESC, tconst);

-- The YouTube trailer a film plays in place, learned from TMDb's list of
-- clips and checked against YouTube's oEmbed, and asked again after 150
-- days. The trailer job is the only thing that asks: it fills this for
-- every film, a film a reader has opened first, and the endpoint only
-- reads it.
CREATE TABLE IF NOT EXISTS meta.trailers (
    tconst      text PRIMARY KEY,
    youtube_key text,          -- null: TMDb has no trailer that can be embedded
    asked_at    timestamptz NOT NULL
);

-- The trailer job's re-asks, oldest first.
CREATE INDEX IF NOT EXISTS trailers_asked ON meta.trailers (asked_at);

-- The titles the trailer job has still to ask about: the ones a reader
-- has opened (wanted_at), newest first, then every film never asked,
-- most voted first. Votes are copied here for the same reason
-- tmdb_queue copies them. A wanted title may already have an answer,
-- one that has come due; it stays wanted until it is asked again.
CREATE TABLE IF NOT EXISTS meta.trailer_queue (
    tconst    text PRIMARY KEY,
    votes     int NOT NULL,
    wanted_at timestamptz
);

-- The sweep's order.
CREATE INDEX IF NOT EXISTS trailer_queue_order
    ON meta.trailer_queue (votes DESC, tconst);

-- And what readers are waiting on, which the job reads before every
-- batch. Partial, so it holds only the handful of rows that are wanted
-- rather than every film the sweep has still to reach.
CREATE INDEX IF NOT EXISTS trailer_queue_wanted
    ON meta.trailer_queue (wanted_at DESC, tconst) WHERE wanted_at IS NOT NULL;

-- A person's photo, learned from TMDb's find by IMDb name id. Only the
-- path on TMDb's image host is kept; the page loads the picture from
-- there, the way it loads a backup poster. The people job is the only
-- thing that asks: it fills this for everyone a map can show, the
-- people on a map a reader has opened first, and a read only ever
-- looks it up. Like everything cached from TMDb it is asked again after
-- 150 days, replacing the row in place, and one still unrenewed at 175
-- days is deleted.
--
-- A null profile_path is an answer: TMDb has no photo for the person,
-- or no person for the id (tmdb_id null too), and it is not asked
-- again until that answer comes due.
CREATE TABLE IF NOT EXISTS meta.people (
    nconst       text PRIMARY KEY,
    tmdb_id      int,
    profile_path text,
    asked_at     timestamptz NOT NULL
);

-- The people job's re-asks, oldest first, and the backstop's deletes.
CREATE INDEX IF NOT EXISTS people_asked ON meta.people (asked_at);

-- The people the job has still to ask about: the ones on a map a reader
-- has opened (wanted_at), newest first, then everyone never asked, by
-- the votes of the best known film of theirs a map can show, most first.
-- Votes are copied here for the same reason tmdb_queue copies them. A
-- wanted person may already have an answer, one that has come due; they
-- stay wanted until they are asked again.
CREATE TABLE IF NOT EXISTS meta.people_queue (
    nconst    text PRIMARY KEY,
    votes     int NOT NULL DEFAULT 0,
    wanted_at timestamptz
);

-- The sweep's order.
CREATE INDEX IF NOT EXISTS people_queue_order
    ON meta.people_queue (votes DESC, nconst);

-- And what readers are waiting on, read before every batch. Partial, so
-- it holds only the people who are wanted rather than everyone the
-- sweep has still to reach.
CREATE INDEX IF NOT EXISTS people_queue_wanted
    ON meta.people_queue (wanted_at DESC, nconst) WHERE wanted_at IS NOT NULL;

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

-- Where a movie can be watched in one country: the Streaming
-- Availability API's answer, shaped the way the page shows it, into
-- stream, free, rent and buy lists. A movie on nothing is kept the same
-- way, as an answer; a failed ask is never kept. Only the movies readers
-- open are ever asked about, so this grows with what people look at,
-- never with the catalog.
--
-- An answer is served as it stands, and kept until something changes
-- it. The daily changes job rewrites the answers the API's feed of
-- changes says have changed (meta.streaming_sync). expires_at is when the
-- first of its options leaves, when one is leaving; a River job is
-- scheduled for that moment, so an answer does not go on offering a
-- service the movie has left. One older than 30 days is still served,
-- and a River job asks again behind it, for a change the feed missed.
CREATE TABLE IF NOT EXISTS meta.where_to_watch (
    tconst     text NOT NULL,
    country    text NOT NULL,
    answer     jsonb NOT NULL,
    fetched_at timestamptz NOT NULL,
    expires_at timestamptz,
    PRIMARY KEY (tconst, country)
);

-- The countries the Streaming Availability API covers, by lowercased ISO
-- code, with the name the page's sentence uses. A reader anywhere else
-- is told there is no coverage without the API being asked. Refreshed
-- once it is a week old.
CREATE TABLE IF NOT EXISTS meta.streaming_countries (
    code       text PRIMARY KEY,
    name       text NOT NULL,
    fetched_at timestamptz NOT NULL
);

-- How far the daily changes job has read the Streaming Availability
-- API's feed of changes, per country: synced_to is where the next run
-- starts, since every change before it has been handled, and updated_at
-- is when a run last recorded it, which is what makes a country due
-- again a day later. A country appears once somebody has an answer kept
-- for it; one without is never read.
CREATE TABLE IF NOT EXISTS meta.streaming_sync (
    country    text PRIMARY KEY,
    synced_to  timestamptz NOT NULL,
    updated_at timestamptz NOT NULL
);

-- GeoLite2 Country itself, the build MaxMind last published, which
-- places a reader's address in a country. Kept here so a restart, or a
-- second container during a deploy, reads it instead of downloading it
-- again. last_modified is MaxMind's stamp for the build: a check whose
-- HEAD sees the same one has nothing to download.
CREATE TABLE IF NOT EXISTS meta.geoip (
    id            int PRIMARY KEY DEFAULT 1 CHECK (id = 1),
    last_modified text NOT NULL,
    mmdb          bytea NOT NULL,
    fetched_at    timestamptz NOT NULL
);
