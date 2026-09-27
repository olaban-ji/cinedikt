package catalog

// The TMDb id of every film a search can offer.
//
// Search results come back keyed by TMDb's own id. Learning which IMDb
// title that is at search time is one request per hit, and a couple of
// readers settling a new query in the same second is enough to step
// over TMDb's ceiling. The id does not change, so it is learned here,
// once, the way a poster is, and a search only has to look it up.

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/jackc/pgx/v5/pgconn"

	"cinedikt/internal/notify"
	"cinedikt/internal/tmdb"
)

// tmdbIDFilm is the film a search might have to resolve. Release date
// is deliberately not part of it: a film that is not out yet is still
// the same film, and the map should already know it on the day it is.
// What search itself refuses — a documentary, an adult title, a film
// with nobody billed — is left out, because matching it would spend a
// request on a hit that can never be offered.
const tmdbIDFilm = `
	NOT t.is_adult
	AND t.start_year IS NOT NULL
	AND NOT (t.genres @> ARRAY['Documentary'])
	AND EXISTS (SELECT 1 FROM ` + Live + `.principals pr WHERE pr.tconst = t.tconst)`

// TMDbIDJob matches IMDb titles to TMDb ids.
type TMDbIDJob struct {
	Store  *Store
	Client PosterFinder
	Logger *slog.Logger
	// Batch is how many are claimed per round; zero takes TMDbBatch.
	Batch int
	// Notify hears when a pass starts, how far it has got, and how it
	// ends. Nil leaves that in the log.
	Notify notify.Sink
}

// Run matches what it can before ctx is done.
func (j *TMDbIDJob) Run(ctx context.Context) error {
	n, err := j.Store.tmdbIDsOutstanding(ctx)
	if stopping(err) {
		return nil
	}
	if err != nil {
		return err
	}
	if n == 0 {
		return nil
	}
	if err := j.Store.refillTMDBQueue(ctx); err != nil {
		if stopping(err) {
			return nil
		}
		return err
	}
	batch := j.Batch
	if batch <= 0 {
		batch = TMDbBatch
	}
	track := newProgress(j.Logger, "matching tmdb ids", n)
	track.watch(j.Notify, notify.Event{Job: notify.JobTMDbIDs})
	run := pass{sink: j.Notify, job: notify.JobTMDbIDs}
	var matched, none, failed int64
	var last error
	// A fault leaves the title unstamped, so the next pass tries it
	// again — which means this pass must not, or the head of the queue
	// would be the same failure until the process ended.
	tried := make(map[string]bool)
	for {
		if ctx.Err() != nil {
			j.Logger.Info("tmdb ids paused", "matched", matched, "none", none, "failed", failed)
			return nil
		}
		ids, err := j.Store.tmdbIDBatch(ctx, batch)
		if stopping(err) {
			return nil
		}
		if err != nil {
			return err
		}
		var fresh []string
		for _, id := range ids {
			if !tried[id] {
				fresh = append(fresh, id)
			}
		}
		if len(fresh) == 0 {
			if matched+none+failed > 0 {
				track.done(matched + none + failed)
				j.Logger.Info("tmdb ids caught up", "matched", matched, "none", none, "failed", failed)
			}
			// Every lookup failing, with not one answer among them, is
			// TMDb, or the way there, rather than a bad batch.
			if failed >= failedLookups && matched+none == 0 {
				return &LookupsFailedError{Provider: "TMDb", Count: failed, Last: last}
			}
			run.finish(matched, none, failed)
			return nil
		}
		run.start(n)
		for _, id := range fresh {
			if ctx.Err() != nil {
				return nil
			}
			asked, err := j.Store.tmdbAsked(ctx, id)
			if stopping(err) {
				return nil
			}
			if err != nil {
				return err
			}
			if asked {
				if err := j.Store.dropTMDBQueue(ctx, id); err != nil && !stopping(err) {
					j.Logger.Warn("tmdb id: queue", "tconst", id, "err", err)
				}
				continue
			}
			tried[id] = true
			got, err := j.Client.FindByIMDb(ctx, id)
			switch {
			case stopping(err):
				return nil
			case errors.Is(err, tmdb.ErrNotFound):
				got = tmdb.Found{}
			case refused(err):
				return &KeyError{Provider: "TMDb", Err: err}
			case err != nil:
				failed++
				last = err
				j.Logger.Warn("tmdb id", "tconst", id, "err", err)
				track.step(matched + none + failed)
				continue
			}
			if err := j.Store.rememberTMDB(ctx, id, got.ID); err != nil {
				if stopping(err) {
					return nil
				}
				failed++
				j.Logger.Warn("tmdb id: save", "tconst", id, "err", err)
				track.step(matched + none + failed)
				continue
			}
			if got.ID > 0 {
				matched++
			} else {
				none++
			}
			track.step(matched + none + failed)
		}
	}
}

// tmdbIDsOutstanding is how many titles this pass still has to ask about.
func (s *Store) tmdbIDsOutstanding(ctx context.Context) (int64, error) {
	var n int64
	err := s.pool.QueryRow(ctx, `
		SELECT count(*)
		FROM `+Live+`.titles t
		WHERE `+tmdbIDFilm+`
		  AND NOT EXISTS (SELECT 1 FROM meta.tmdb m WHERE m.tconst = t.tconst)`).Scan(&n)
	if err != nil {
		return 0, fmt.Errorf("catalog: count titles wanting a tmdb id: %w", err)
	}
	return n, nil
}

// refillTMDBQueue puts every unmatched title into the queue the job
// walks, best known first. Votes are copied onto the row: ordering the
// live catalog on every page is the query the poster queue exists to
// avoid, and this one would be larger.
func (s *Store) refillTMDBQueue(ctx context.Context) error {
	if _, err := s.pool.Exec(ctx, `
		DELETE FROM meta.tmdb_queue q
		WHERE EXISTS (SELECT 1 FROM meta.tmdb m WHERE m.tconst = q.tconst)`); err != nil {
		return fmt.Errorf("catalog: clear matched tmdb queue: %w", err)
	}
	_, err := s.pool.Exec(ctx, `
		INSERT INTO meta.tmdb_queue (tconst, votes)
		SELECT t.tconst, coalesce(r.num_votes, 0)
		FROM `+Live+`.titles t
		LEFT JOIN `+Live+`.ratings r USING (tconst)
		WHERE `+tmdbIDFilm+`
		  AND NOT EXISTS (SELECT 1 FROM meta.tmdb m WHERE m.tconst = t.tconst)
		ON CONFLICT (tconst) DO UPDATE SET votes = EXCLUDED.votes`)
	if err != nil {
		return fmt.Errorf("catalog: fill tmdb queue: %w", err)
	}
	return nil
}

// tmdbIDBatch is the next titles to ask about, best known first.
func (s *Store) tmdbIDBatch(ctx context.Context, limit int) ([]string, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT tconst
		FROM meta.tmdb_queue
		ORDER BY votes DESC, tconst
		LIMIT $1`, limit)
	if err != nil {
		return nil, fmt.Errorf("catalog: tmdb id queue: %w", err)
	}
	defer rows.Close()
	var ids []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	return ids, rows.Err()
}

// tmdbAsked reports whether this title has already been put to TMDb.
func (s *Store) tmdbAsked(ctx context.Context, tconst string) (bool, error) {
	var asked bool
	err := s.pool.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM meta.tmdb WHERE tconst = $1)`, tconst).Scan(&asked)
	if err != nil {
		return false, fmt.Errorf("catalog: tmdb asked %s: %w", tconst, err)
	}
	return asked, nil
}

// dropTMDBQueue takes a title out of the queue without recording an
// answer. The poster job may already have recorded one.
func (s *Store) dropTMDBQueue(ctx context.Context, tconst string) error {
	_, err := s.pool.Exec(ctx, `DELETE FROM meta.tmdb_queue WHERE tconst = $1`, tconst)
	return err
}

// rememberTMDB records TMDb's id for a title, or that TMDb has none.
// A zero id is that second answer. Either way the title leaves the queue.
func (s *Store) rememberTMDB(ctx context.Context, tconst string, tmdbID int) error {
	var id any
	if tmdbID > 0 {
		id = tmdbID
	}
	_, err := s.pool.Exec(ctx, `
		INSERT INTO meta.tmdb (tconst, tmdb_id, asked_at)
		VALUES ($1, $2, now())
		ON CONFLICT (tconst) DO NOTHING`, tconst, id)
	if err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == "23505" && tmdbID > 0 {
			// Another IMDb title already owns this TMDb id. This one was
			// still asked, and has to be recorded as such or it comes back.
			_, err = s.pool.Exec(ctx, `
				INSERT INTO meta.tmdb (tconst, tmdb_id, asked_at)
				VALUES ($1, NULL, now())
				ON CONFLICT (tconst) DO NOTHING`, tconst)
		}
		if err != nil {
			return fmt.Errorf("catalog: remember tmdb id %s: %w", tconst, err)
		}
	}
	if err := s.dropTMDBQueue(ctx, tconst); err != nil {
		return fmt.Errorf("catalog: tmdb queue %s: %w", tconst, err)
	}
	return nil
}

// fillTMDbIDs keeps the matcher running for as long as the process
// does. It shares the poster job's client, so the two together stay
// inside one rate limit instead of each spending a full one.
func fillTMDbIDs(ctx context.Context, job *TMDbIDJob, logger *slog.Logger, wakes *Wakes) {
	waited := false
	for {
		ready, err := job.Store.LiveReady(ctx)
		wait := TMDbRest
		switch {
		case err != nil || !ready:
			if !waited {
				logger.Info("tmdb ids waiting for a catalog")
				waited = true
			}
			wait = PosterWaitForCatalog
		default:
			waited = false
			err := job.Run(ctx)
			if err != nil && ctx.Err() == nil {
				logger.Warn("tmdb ids", "err", err)
			}
			reportRun(ctx, job.Notify, notify.JobTMDbIDs, err, time.Now().Add(wait))
		}
		// A new generation is the only thing that brings new titles.
		// Its own channel, not the poster backfill's: sharing one
		// would give a publish to whichever of the two took it first.
		if !waitFor(ctx, wakes.PublishedIDs, wait) {
			return
		}
	}
}
