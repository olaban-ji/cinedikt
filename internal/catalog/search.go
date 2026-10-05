package catalog

import (
	"context"
	"fmt"
	"math/rand"
	"strings"
	"sync"
	"time"
)

// Hit is one search result, already known to be a movie this catalog can
// build a map from.
type Hit struct {
	ID     string `json:"id"`
	Title  string `json:"title"`
	Year   int    `json:"year"`
	Poster string `json:"poster,omitempty"`
	// Colour is what the poster averages to, for the frame the opening
	// screen draws before the picture arrives. Left out when it has not
	// been worked out yet: the client has a colour of its own to fall
	// back on, and a wrong one is worse than none.
	Colour string `json:"c,omitempty"`
	Votes  int    `json:"-"`
}

// MinQuery is the shortest query worth asking about. Two characters
// matches half the catalog and tells the reader nothing.
const MinQuery = 2

// Search finds movies by title, out of the catalog itself.
//
// It only sees the primary and original titles. A name IMDb files
// elsewhere comes back empty; the API then asks TMDb, and keeps a hit
// only when this catalog has already matched that TMDb id to a film
// it can map.
//
// Ranking is what makes a search feel right, and it is the one thing a
// title match cannot do on its own: "matrix" has to put The Matrix
// above the thirty other films with the word in the title.
//
// Votes do almost all of that work — they are the best proxy the
// dataset has for "the one they meant". An exact title is worth a great
// deal on top, but as a multiplier rather than an absolute: there is a
// film called "Godfather", and it is not the one anybody means.
//
// A bonus for titles *starting* with the query was tried and removed:
// it put "Matrix Zone" above "The Matrix", because an article at the
// front is enough to lose a prefix match and no number of votes could
// win it back.
func (s *Store) Search(ctx context.Context, query string, limit int) ([]Hit, error) {
	query = strings.TrimSpace(query)
	if len(query) < MinQuery {
		return nil, nil
	}
	ctx, cancel := context.WithTimeout(ctx, ReadTimeout)
	defer cancel()

	lower := strings.ToLower(query)
	rows, err := s.pool.Query(ctx, `
		SELECT t.tconst, t.primary_title, coalesce(t.start_year, 0),
		       coalesce(p.poster_url, ''), coalesce(r.num_votes, 0)
		FROM `+Live+`.titles t
		LEFT JOIN `+Live+`.ratings r USING (tconst)
		LEFT JOIN meta.posters p USING (tconst)
		WHERE (lower(t.primary_title) LIKE $1 OR lower(t.original_title) LIKE $1)
		  AND `+gridFilm+`
		  AND EXISTS (SELECT 1 FROM `+Live+`.principals pr WHERE pr.tconst = t.tconst)
		ORDER BY
		    -- Votes decide, except that typing a title in full counts
		    -- for a great deal. A multiplier rather than a rank: an
		    -- obscure film actually called "Godfather" should not
		    -- outrank The Godfather, but a small film typed in full
		    -- should beat a blockbuster that merely contains the words.
		    coalesce(r.num_votes, 0)
		      * CASE WHEN lower(t.primary_title) = $2 THEN 50 ELSE 1 END DESC,
		    t.tconst
		LIMIT $3`,
		"%"+lower+"%", lower, limit)
	if err != nil {
		return nil, fmt.Errorf("catalog: search: %w", err)
	}
	defer rows.Close()

	out := make([]Hit, 0, limit)
	for rows.Next() {
		var h Hit
		if err := rows.Scan(&h.ID, &h.Title, &h.Year, &h.Poster, &h.Votes); err != nil {
			return nil, fmt.Errorf("catalog: scan hit: %w", err)
		}
		out = append(out, h)
	}
	return out, rows.Err()
}

// ByTMDB resolves TMDb movie ids to films a search may offer, in the
// order they were asked. The ids were matched ahead of time; a search
// does not ask TMDb which IMDb title one of them is. An id with no
// match, or a match this catalog cannot map, is left out.
func (s *Store) ByTMDB(ctx context.Context, ids []int) ([]Hit, error) {
	if len(ids) == 0 {
		return nil, nil
	}
	keys := make([]int32, len(ids))
	for i, id := range ids {
		keys[i] = int32(id)
	}
	ctx, cancel := context.WithTimeout(ctx, ReadTimeout)
	defer cancel()

	rows, err := s.pool.Query(ctx, `
		SELECT m.tmdb_id, t.tconst, t.primary_title, coalesce(t.start_year, 0),
		       coalesce(p.poster_url, ''), coalesce(r.num_votes, 0)
		FROM meta.tmdb m
		JOIN `+Live+`.titles t ON t.tconst = m.tconst
		LEFT JOIN `+Live+`.ratings r ON r.tconst = t.tconst
		LEFT JOIN meta.posters p ON p.tconst = t.tconst
		WHERE m.tmdb_id = ANY($1)
		  AND `+gridFilm+`
		  AND EXISTS (SELECT 1 FROM `+Live+`.principals pr WHERE pr.tconst = t.tconst)`,
		keys)
	if err != nil {
		return nil, fmt.Errorf("catalog: tmdb hits: %w", err)
	}
	defer rows.Close()

	byID := make(map[int]Hit, len(ids))
	for rows.Next() {
		var h Hit
		var tmdbID int32
		if err := rows.Scan(&tmdbID, &h.ID, &h.Title, &h.Year, &h.Poster, &h.Votes); err != nil {
			return nil, fmt.Errorf("catalog: scan hit: %w", err)
		}
		byID[int(tmdbID)] = h
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("catalog: tmdb hits: %w", err)
	}

	out := make([]Hit, 0, len(ids))
	seen := make(map[int]struct{}, len(ids))
	for _, id := range ids {
		if _, ok := seen[id]; ok {
			continue
		}
		seen[id] = struct{}{}
		if h, ok := byID[id]; ok {
			out = append(out, h)
		}
	}
	return out, nil
}

// firstRunDepth is how many candidates an era may offer before the cold
// screen gives up on it. The first one usually has a picture; the rest
// are there so a broken poster does not leave the era blank.
const firstRunDepth = 12

// firstRunProbe bounds the poster checks that follow the query. A slow
// image host should not hold the cold screen open.
const firstRunProbe = 4 * time.Second

// FirstRun picks movies to open a map from, a different set each visit.
//
// They come one per era from a pool ranked at import time, so the screen
// shows how far apart two movies can be and still be two movies apart —
// and so nobody arrives to the same eight twice.
//
// The ranking is not done here. Choosing the best known movie of an era
// from the whole catalog means sorting a quarter of a million rows to
// return one, and there is no index that helps: the year is on `titles`
// and the votes are on `ratings`. The pool settles that once per
// generation; this reads a couple of thousand rows and picks.
//
// A movie with no poster, or whose poster answers 404, is not one of
// them. The pool is built before the pictures arrive, so that choice is
// made here, and the next candidate in the era takes the place.
func (s *Store) FirstRun(ctx context.Context) ([]Hit, error) {
	qctx, cancel := context.WithTimeout(ctx, ReadTimeout)
	rows, err := s.pool.Query(qctx, `
		SELECT tconst, title, year, poster, colour, votes, era
		FROM (
			SELECT t.tconst, t.primary_title AS title,
			       coalesce(t.start_year, 0) AS year,
			       p.poster_url AS poster, p.colour, f.num_votes AS votes, f.era,
			       row_number() OVER (PARTITION BY f.era ORDER BY random()) AS n
			FROM `+Live+`.first_run f
			JOIN `+Live+`.titles t USING (tconst)
			JOIN meta.posters p USING (tconst)
			-- A tile with no picture is a grey box. An empty address is
			-- the same thing as none.
			WHERE p.poster_url IS NOT NULL
			  AND btrim(p.poster_url) <> ''
			  AND (p.released IS NULL OR p.released <= current_date)
		) candidates
		WHERE n <= $1
		ORDER BY era, n`, firstRunDepth)
	if err != nil {
		cancel()
		return nil, fmt.Errorf("catalog: first run: %w", err)
	}

	var groups [][]pick
	for rows.Next() {
		var p pick
		var colour *string
		if err := rows.Scan(&p.hit.ID, &p.hit.Title, &p.hit.Year, &p.hit.Poster, &colour, &p.hit.Votes, &p.era); err != nil {
			rows.Close()
			cancel()
			return nil, fmt.Errorf("catalog: scan first run: %w", err)
		}
		if colour != nil {
			p.hit.Colour = strings.TrimSpace(*colour)
		}
		if len(groups) == 0 || groups[len(groups)-1][0].era != p.era {
			groups = append(groups, nil)
		}
		groups[len(groups)-1] = append(groups[len(groups)-1], p)
	}
	err = rows.Err()
	rows.Close()
	cancel()
	if err != nil {
		return nil, fmt.Errorf("catalog: first run: %w", err)
	}

	pctx, cancel := context.WithTimeout(ctx, firstRunProbe)
	defer cancel()
	hits, gone := firstLive(pctx, groups)
	// A film here with no colour yet simply goes without one: the
	// client has a fallback, and downloading a poster to average it
	// would hold a reader for a frame they are about to stop looking
	// at. ColourJob fills the whole pool in the background, which is
	// the right unit — this screen draws eight of two thousand at
	// random, so colouring only the ones somebody happened to see
	// would leave the rest just as bare next time.
	// What the host said is gone is written down, so the next start
	// does not ask again and the TMDb fallback has something to repair.
	// Off the request: the screen is already on its way out.
	if len(gone) > 0 {
		go func() {
			ctx, cancel := context.WithTimeout(context.Background(), ReadTimeout)
			defer cancel()
			for _, id := range gone {
				_ = s.MarkPosterDead(ctx, id)
			}
		}()
	}
	return hits, nil
}

// pick is one candidate for the cold screen, with the era it stands for.
type pick struct {
	hit Hit
	era int
}

// firstLive keeps the first movie in each era whose poster still
// exists, and reports the ones whose address has stopped answering.
// Eras are asked together; within an era the next candidate is only
// asked when the one before it is gone.
func firstLive(ctx context.Context, groups [][]pick) ([]Hit, []string) {
	chosen := make([]pick, len(groups))
	dead := make([][]string, len(groups))
	var wg sync.WaitGroup
	for i, group := range groups {
		wg.Add(1)
		go func(i int, group []pick) {
			defer wg.Done()
			for _, p := range group {
				missing, gone := posterGone(ctx, p.hit.Poster)
				if gone {
					dead[i] = append(dead[i], p.hit.ID)
				}
				if missing {
					continue
				}
				chosen[i] = p
				return
			}
		}(i, group)
	}
	wg.Wait()

	out := make([]pick, 0, len(groups))
	for _, p := range chosen {
		if p.hit.ID != "" {
			out = append(out, p)
		}
	}
	// Shuffled, not sorted by era.
	//
	// In era order the screen is the same shape every visit — oldest
	// film top left, newest bottom right — and a reader learns the
	// positions rather than the films. It also decided what a smaller
	// screen showed: the client takes as many as fit from the front,
	// so a window with room for four got the four oldest eras every
	// time and the rest of the century was unreachable.
	//
	// One per era still, which is the part that matters: the eight span
	// the century. Where each one lands is not information.
	rand.Shuffle(len(out), func(i, j int) { out[i], out[j] = out[j], out[i] })
	hits := make([]Hit, len(out))
	for i, p := range out {
		hits[i] = p.hit
	}
	var gone []string
	for _, ids := range dead {
		gone = append(gone, ids...)
	}
	return hits, gone
}
