-- Built after the COPY, never before: an index maintained during a bulk
-- load costs more than one built once over finished data.

-- A map is "this film's people, then everything those people made", so
-- both directions of the credit are looked up by these two.
CREATE INDEX ON {{schema}}.principals (nconst, category);
CREATE INDEX ON {{schema}}.principals (tconst, category);
CREATE INDEX ON {{schema}}.directors (nconst);
CREATE INDEX ON {{schema}}.directors (tconst);

-- Search. Trigram over both titles, and only over the rows search can
-- return, so the index is not carrying adult titles it will never show.
CREATE INDEX ON {{schema}}.titles
    USING gin (lower(primary_title) gin_trgm_ops) WHERE NOT is_adult;
CREATE INDEX ON {{schema}}.titles
    USING gin (lower(original_title) gin_trgm_ops) WHERE NOT is_adult;

-- The grid's film test: released, not a documentary, not adult. A
-- partial index over exactly the rows a map can hold.
CREATE INDEX ON {{schema}}.titles (start_year)
    WHERE NOT is_adult AND start_year IS NOT NULL AND NOT (genres @> ARRAY['Documentary']);
