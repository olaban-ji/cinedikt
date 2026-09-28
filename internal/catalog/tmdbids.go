package catalog

// The TMDb id of every film a search can offer.
//
// Search results come back keyed by TMDb's own id. Learning which IMDb
// title that is at search time is one request per hit, and a couple of
// readers settling a new query in the same second is enough to step
// over TMDb's ceiling. So the id is learned here, ahead of time, and a
// search only has to look it up.
//
// A match for a film search can still offer is asked again once it is
// tmdbRefreshDays old, which TMDb's terms ask of anything cached from
// it, after every title never asked. Any other match waits for the
// backstop in tmdbterms.go, which deletes whatever is still unrenewed at
// tmdbForgetDays, and the queue's next refill takes its title back in
// if search can offer it again.

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

// tmdbIDRefillEvery is how long a pass of the id matcher runs before it
// fills its queue again: the rest interval, which is how long a publish's
// titles wait between passes too. Tests shorten it.
var tmdbIDRefillEvery = TMDbRest

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

// Run matches what it can before ctx is done: every title the queue
// holds, best known first, and then the matches that have come due,
// oldest first. The queue is looked at again before every batch.
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
	// The queue is filled at the start of a pass. A pass that runs past
	// tmdbIDRefillEvery, as the re-asks do when a whole sweep's matches
	// come due together, fills it again as it goes, so the titles a
	// publish brings wait that long at most rather than behind every
	// re-ask.
	refilled := time.Now()
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
		if time.Since(refilled) >= tmdbIDRefillEvery {
			if err := j.Store.refillTMDBQueue(ctx); err != nil {
				if stopping(err) {
					return nil
				}
				return err
			}
			refilled = time.Now()
		}
		ids, due, err := j.Store.tmdbIDBatch(ctx, batch, tried)
		if stopping(err) {
			return nil
		}
		if err != nil {
			return err
		}
		if len(ids) == 0 {
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
		for _, id := range ids {
			if ctx.Err() != nil {
				return nil
			}
			// A queued title may have stopped being one search can offer
			// since it was queued. A due match was chosen by that same
			// test a moment ago, so it needs no second look.
			if !due {
				wanted, err := j.Store.tmdbIDWanted(ctx, id)
				if stopping(err) {
					return nil
				}
				if err != nil {
					return err
				}
				if !wanted {
					if err := j.Store.dropTMDBQueue(ctx, id); err != nil && !stopping(err) {
						j.Logger.Warn("tmdb id: queue", "tconst", id, "err", err)
					}
					continue
				}
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
			// The overview came on the same answer. It is kept only
			// where OMDb has given no plot.
			if err := j.Store.keepTMDbOverview(ctx, id, got.Overview); err != nil && !stopping(err) {
				j.Logger.Warn("tmdb id: overview", "tconst", id, "err", err)
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

// tmdbIDsOutstanding is how many titles this pass still has to ask
// about: the ones never asked, and the matches that have come due.
func (s *Store) tmdbIDsOutstanding(ctx context.Context) (int64, error) {
	var n int64
	err := s.pool.QueryRow(ctx, `
		SELECT
		    (SELECT count(*)
		     FROM `+Live+`.titles t
		     WHERE `+tmdbIDFilm+`
		       AND NOT EXISTS (SELECT 1 FROM meta.tmdb m WHERE m.tconst = t.tconst))
		  + (SELECT count(*) `+tmdbIDsDue+`)`, tmdbRefreshDays).Scan(&n)
	if err != nil {
		return 0, fmt.Errorf("catalog: count titles wanting a tmdb id: %w", err)
	}
	return n, nil
}

// tmdbIDsDue is the matches old enough that TMDb's terms want them asked
// again, as the FROM and WHERE that tmdbIDBatch and tmdbIDsOutstanding
// share; $1 is tmdbRefreshDays. Only for a film search can still offer:
// one that has left the catalog, or stopped being such a film, is left
// to the backstop, and asked about afresh should it come back.
const tmdbIDsDue = `
		FROM meta.tmdb m
		JOIN ` + Live + `.titles t ON t.tconst = m.tconst
		WHERE m.asked_at < now() - make_interval(days => $1)
		  AND ` + tmdbIDFilm

// refillTMDBQueue puts every unmatched title into the queue the job
// walks, best known first. Votes are copied onto the row: ordering the
// live catalog on every page is the query the poster queue exists to
// avoid, and this one would be larger.
//
// It runs at the start of every pass, not only after a publish, though
// a publish is the one thing that brings new titles or moves votes.
// The wake that says a publish happened is kept nowhere: one sent
// while nobody was listening is lost, and the backstop pass is what
// catches its titles. A pass with nothing new only reads.
//
// A match the backstop has deleted leaves its title unmatched, so it is
// queued again here like a title new to the catalog.
func (s *Store) refillTMDBQueue(ctx context.Context) error {
	if _, err := s.pool.Exec(ctx, `
		DELETE FROM meta.tmdb_queue q
		WHERE EXISTS (SELECT 1 FROM meta.tmdb m WHERE m.tconst = q.tconst)`); err != nil {
		return fmt.Errorf("catalog: clear matched tmdb queue: %w", err)
	}
	// Only a title new to the queue, or one whose votes have moved since
	// it was queued, gets past the last NOT EXISTS. Letting every waiting
	// title reach ON CONFLICT would rewrite all of them each pass — the
	// better part of half a million dead rows every ten minutes, for the
	// same numbers. A WHERE on the DO UPDATE is not enough on its own:
	// Postgres still locks every row it declines to update, and a lock
	// is a write to the row's page like any other.
	err := s.fillQueue(ctx, `
		INSERT INTO meta.tmdb_queue (tconst, votes)
		SELECT t.tconst, coalesce(r.num_votes, 0)
		FROM `+Live+`.titles t
		LEFT JOIN `+Live+`.ratings r USING (tconst)
		WHERE `+tmdbIDFilm+`
		  AND NOT EXISTS (SELECT 1 FROM meta.tmdb m WHERE m.tconst = t.tconst)
		  AND NOT EXISTS (
		      SELECT 1 FROM meta.tmdb_queue q
		      WHERE q.tconst = t.tconst AND q.votes = coalesce(r.num_votes, 0))
		ON CONFLICT (tconst) DO UPDATE SET votes = EXCLUDED.votes`)
	if err != nil {
		return fmt.Errorf("catalog: fill tmdb queue: %w", err)
	}
	return nil
}

// fillQueue runs a queue refill's INSERT with nested loops turned off.
//
// Every refill leaves out what its queue already holds with a NOT EXISTS
// on that same queue. Planned against a queue that is empty, as one is
// before its first fill or once its job has caught up, that becomes a
// nested loop reading the whole queue again for every candidate, the
// rows the statement has just written included: hours for a sweep of
// the whole catalog, where a hash or merge join takes seconds. The
// setting and the INSERT share one transaction: as two statements on
// the pool they could land on different connections, and SET LOCAL ends
// with the transaction, so the setting reaches no other statement.
func (s *Store) fillQueue(ctx context.Context, sql string, args ...any) error {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(context.WithoutCancel(ctx))
	if _, err := tx.Exec(ctx, `SET LOCAL enable_nestloop = off`); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, sql, args...); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

// tmdbIDBatch is the next titles to ask about, leaving out what this
// pass has already tried: the queue's, best known first, and only once
// the queue has nothing else to give, the matches that have come due,
// oldest first. due says which of the two it is.
func (s *Store) tmdbIDBatch(ctx context.Context, limit int, tried map[string]bool) (ids []string, due bool, err error) {
	queued, err := s.tconsts(ctx, `
		SELECT tconst
		FROM meta.tmdb_queue
		ORDER BY votes DESC, tconst
		LIMIT $1`, limit)
	if err != nil {
		return nil, false, fmt.Errorf("catalog: tmdb id queue: %w", err)
	}
	if fresh := untried(queued, tried); len(fresh) > 0 {
		return fresh, false, nil
	}
	stale, err := s.tconsts(ctx, `
		SELECT m.tconst `+tmdbIDsDue+`
		ORDER BY m.asked_at, m.tconst
		LIMIT $2`, tmdbRefreshDays, limit)
	if err != nil {
		return nil, false, fmt.Errorf("catalog: tmdb ids due: %w", err)
	}
	return untried(stale, tried), true, nil
}

// untried is ids without the ones this pass has already asked about.
func untried(ids []string, tried map[string]bool) []string {
	var fresh []string
	for _, id := range ids {
		if !tried[id] {
			fresh = append(fresh, id)
		}
	}
	return fresh
}

// tmdbIDWanted reports whether this title is still worth a request: a
// film search could offer that TMDb has not been asked about yet.
//
// The refill only ever adds to the queue, so a title a publish took
// out of the catalog, or made a documentary, stays queued. Judging each
// one here, as it comes up, costs a few index lookups per request;
// clearing them from the whole queue on every pass would mean checking
// the credits of every title in it, for the handful a publish retires.
func (s *Store) tmdbIDWanted(ctx context.Context, tconst string) (bool, error) {
	var wanted bool
	err := s.pool.QueryRow(ctx, `
		SELECT EXISTS (
		    SELECT 1
		    FROM `+Live+`.titles t
		    WHERE t.tconst = $1
		      AND `+tmdbIDFilm+`
		      AND NOT EXISTS (SELECT 1 FROM meta.tmdb m WHERE m.tconst = t.tconst))`, tconst).Scan(&wanted)
	if err != nil {
		return false, fmt.Errorf("catalog: tmdb id wanted %s: %w", tconst, err)
	}
	return wanted, nil
}

// dropTMDBQueue takes a title out of the queue without recording an
// answer. The poster job may already have recorded one, or the title
// is no longer one a search could offer — and recording nothing for
// that one leaves it free to be queued again should it come back.
func (s *Store) dropTMDBQueue(ctx context.Context, tconst string) error {
	_, err := s.pool.Exec(ctx, `DELETE FROM meta.tmdb_queue WHERE tconst = $1`, tconst)
	return err
}

// rememberTMDB records TMDb's id for a title, or that TMDb has none.
// A zero id is that second answer. Either way the title leaves the queue.
//
// An answer for a title already matched replaces its row in place, and
// is stamped afresh: that is how a match that has come due is renewed,
// and search and the trailer job, which read the id, never find the row
// missing while it is.
func (s *Store) rememberTMDB(ctx context.Context, tconst string, tmdbID int) error {
	var id any
	if tmdbID > 0 {
		id = tmdbID
	}
	const upsert = `
		INSERT INTO meta.tmdb (tconst, tmdb_id, asked_at)
		VALUES ($1, $2, now())
		ON CONFLICT (tconst) DO UPDATE
		SET tmdb_id  = EXCLUDED.tmdb_id,
		    asked_at = EXCLUDED.asked_at`
	_, err := s.pool.Exec(ctx, upsert, tconst, id)
	if err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == "23505" && tmdbID > 0 {
			// Another IMDb title already owns this TMDb id. This one was
			// still asked, and has to be recorded as such or it comes back.
			_, err = s.pool.Exec(ctx, upsert, tconst, nil)
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
// does. It shares the runner's TMDb client, and with it the process's
// one limiter, so it and every other TMDb caller stay inside one budget
// instead of each spending a full one.
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
