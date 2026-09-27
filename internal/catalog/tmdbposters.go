package catalog

// The second-chance poster fetch.
//
// OMDb answers for about 59% of this catalog. Of the rest, a few have
// an address that has since stopped answering, and most are titles OMDb
// simply has no picture for. TMDb often does.
//
// What it deliberately does not do is fetch all of them. Three hundred
// thousand titles have no poster and fewer than fifteen hundred have as
// many as a hundred votes: the tail is films nobody will ever open, and
// a service with a rate limit should not spend a day on them. So this
// job takes two kinds of work, in this order:
//
//   - what a reader has already tried to look at (meta.posters.wanted_at,
//     written by the read paths), and
//   - a sweep of the well-known titles, so the common case is repaired
//     before anybody meets it.
//
// Each title is asked about once, ever: tmdb_at is stamped whatever the
// answer, including "TMDb has nothing either". A second service having
// nothing is still an answer.

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"cinedikt/internal/notify"
	"cinedikt/internal/tmdb"
)

// PosterFinder is what this needs from TMDb. An interface so a test
// never reaches the network.
type PosterFinder interface {
	FindByIMDb(ctx context.Context, imdbID string) (tmdb.Found, error)
}

// TMDbSweepMinVotes is how well known a title has to be for the sweep
// to fetch a picture nobody has asked for yet.
//
// A hundred votes takes the sweep from 310,000 titles to about 1,300 —
// half a minute of work instead of a couple of hours — and everything
// below it is still repaired the moment a reader opens it. The floor is
// a statement about what is worth pre-fetching, not about what is worth
// having.
//
// Zero sweeps the whole catalog, which is a defensible thing to want:
// it is a few hours of a rate-limited API once, and after it every
// title TMDb has a picture for has one. Nothing else changes — a title
// TMDb has nothing for is stamped either way and never asked again.
const TMDbSweepMinVotes = 100

// TMDbBatch is how many titles are claimed per round.
//
// Small, now that the queue has an index that returns a page in its
// own order: fifty at forty requests a second is about a second, so a
// title a reader asks for waits about that long rather than behind a
// thousand nobody asked for. The page used to be a thousand only to
// amortise a sort of the entire remaining queue, which is the thing
// posters_tmdb_queue removed.
const TMDbBatch = 50

// TMDbJob fills in pictures OMDb could not.
type TMDbJob struct {
	Store  *Store
	Client PosterFinder
	Logger *slog.Logger
	// MinVotes is the sweep's floor. Zero means every title, which is
	// what it is set to when somebody wants the whole catalog filled.
	// The demand queue ignores it either way: a reader looking at a
	// film is a better reason than its vote count.
	MinVotes int
	// Batch is how many are claimed per round; zero takes TMDbBatch.
	Batch int
	// Notify hears when a pass starts, how far it has got, and how it
	// ends. Nil leaves that in the log.
	Notify notify.Sink
}

// Run repairs what it can before ctx is done.
//
// It takes no schema. Everything it needs is on meta.posters, which
// outlives every generation — so unlike the other jobs it does not
// care which catalog is live, only that a row is waiting.
//
// One title at a time on purpose. The client's own limiter is the pace,
// and there is no burst worth chasing here: the whole queue after the
// first sweep is a handful of titles a reader has just met.
func (j *TMDbJob) Run(ctx context.Context) error {
	batch := j.Batch
	if batch <= 0 {
		batch = TMDbBatch
	}
	floor := j.MinVotes
	// A whole-catalog sweep is hours of work. Without this it is hours
	// of silence, and silence and a wedged job look exactly alike —
	// which is what the OMDb backfill has newProgress for.
	outstanding, err := j.Store.tmdbOutstanding(ctx, floor)
	if stopping(err) {
		return nil
	}
	if err != nil {
		return err
	}
	track := newProgress(j.Logger, "filling in posters from tmdb", outstanding)
	run := pass{sink: j.Notify, job: notify.JobTMDbPosters}
	if outstanding > 0 {
		track.watch(j.Notify, notify.Event{Job: notify.JobTMDbPosters})
	}

	var found, blank, failed int64
	var last error
	// A fault leaves the row unstamped on purpose, so the next pass
	// tries it again — which means this pass must not, or a title TMDb
	// keeps refusing would be handed back by every query and asked
	// about until the process ended. The same rule the OMDb backfill
	// gets from its start-of-pass timestamp.
	tried := make(map[string]bool)
	for {
		if ctx.Err() != nil {
			j.Logger.Info("tmdb posters paused", "found", found, "none", blank, "failed", failed)
			return nil
		}
		ids, err := j.Store.tmdbWanted(ctx, batch, floor)
		if stopping(err) {
			return nil
		}
		if err != nil {
			return err
		}
		fresh := ids[:0:0]
		for _, id := range ids {
			if !tried[id] {
				fresh = append(fresh, id)
			}
		}
		if len(fresh) == 0 {
			if found+blank+failed > 0 {
				track.done(found + blank + failed)
				j.Logger.Info("tmdb posters caught up",
					"found", found, "none", blank, "failed", failed)
			}
			// Every lookup failing, with not one answer among them, is
			// TMDb, or the way there, rather than a bad batch.
			if failed >= failedLookups && found+blank == 0 {
				return &LookupsFailedError{Provider: "TMDb", Count: failed, Last: last}
			}
			run.finish(found, blank, failed)
			return nil
		}
		run.start(outstanding)
		for _, id := range fresh {
			tried[id] = true
			if ctx.Err() != nil {
				return nil
			}
			got, err := j.Client.FindByIMDb(ctx, id)
			var none bool
			switch {
			case stopping(err):
				return nil
			case errors.Is(err, tmdb.ErrNotFound):
				// TMDb does not have it either. That is an answer, and
				// storing it is what stops the title coming round again.
				got = tmdb.Found{}
				none = true
			case refused(err):
				// The key itself. Every title after this one would be
				// told the same, so the pass ends here.
				return &KeyError{Provider: "TMDb", Err: err}
			case err != nil:
				// A fault rather than an answer. Left unstamped, so the
				// next pass tries it again — but not this one.
				failed++
				last = err
				j.Logger.Warn("tmdb poster", "tconst", id, "err", err)
				continue
			}
			if err := j.Store.saveTMDbPoster(ctx, id, got); err != nil {
				if stopping(err) {
					return nil
				}
				j.Logger.Warn("tmdb poster: save", "tconst", id, "err", err)
				continue
			}
			// The same answer is the id a search resolves. Recording it
			// here means the id job does not ask TMDb about this title
			// a second time.
			if got.ID > 0 || none {
				if err := j.Store.rememberTMDB(ctx, id, got.ID); err != nil {
					if stopping(err) {
						return nil
					}
					j.Logger.Warn("tmdb id", "tconst", id, "err", err)
				}
			}
			if got.Poster != "" {
				found++
			} else {
				blank++
			}
			track.step(found + blank + failed)
		}
	}
}

// refused reports whether TMDb turned the credentials down.
func refused(err error) bool {
	var se *tmdb.StatusError
	return errors.As(err, &se) && (se.Status == 401 || se.Status == 403)
}

// tmdbOutstanding is how many titles this pass has to ask about. It is
// the same set tmdbWanted hands out, counted once at the start so the
// log can say how far through it the pass is.
func (s *Store) tmdbOutstanding(ctx context.Context, minVotes int) (int64, error) {
	var n int64
	err := s.pool.QueryRow(ctx, `
		SELECT count(*)
		FROM meta.posters
		WHERE tmdb_at IS NULL
		  AND (status = 'dead' OR poster_url IS NULL OR btrim(poster_url) = '')
		  AND (wanted_at IS NOT NULL OR coalesce(votes, 0) >= $1)`, minVotes).Scan(&n)
	if err != nil {
		return 0, fmt.Errorf("catalog: count titles wanting a tmdb poster: %w", err)
	}
	return n, nil
}

// tmdbWanted is the next titles to ask TMDb about: the ones a reader
// wanted, then the best known of the rest.
//
// It reads one table. Ordering used to join the live catalog for a
// vote count, which meant every page re-sorted the whole remaining
// queue — three hundred thousand rows, four hundred milliseconds, over
// and over. The count is snapshotted onto the row now, so this is an
// index scan that stops at the limit.
//
// Adult titles need no exclusion here. A poster row is only ever
// created by the OMDb backfill, which selects `NOT t.is_adult`, so one
// cannot enter this queue in the first place.
//
// Never anything already asked about. tmdb_at is the whole of the
// bookkeeping, which is why this query needs no start-of-pass guard the
// way the OMDb backfill does: there is nothing here that can be handed
// out twice.
func (s *Store) tmdbWanted(ctx context.Context, limit, minVotes int) ([]string, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT tconst
		FROM meta.posters
		WHERE tmdb_at IS NULL
		  AND (status = 'dead' OR poster_url IS NULL OR btrim(poster_url) = '')
		  AND (wanted_at IS NOT NULL OR coalesce(votes, 0) >= $2)
		ORDER BY wanted_at DESC NULLS LAST, votes DESC, tconst
		LIMIT $1`, limit, minVotes)
	if err != nil {
		return nil, err
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

// KeepTMDbPoster records what TMDb had for a title whose picture just
// failed to load.
//
// A picture replaces the address that failed. Nothing is still an
// answer: the title is not asked again, and the address it already has
// stays, so the card can try that one once more.
func (s *Store) KeepTMDbPoster(ctx context.Context, tconst string, got tmdb.Found) error {
	if err := s.saveTMDbPoster(ctx, tconst, got); err != nil {
		return err
	}
	if got.ID > 0 || got.Poster == "" {
		if err := s.rememberTMDB(ctx, tconst, got.ID); err != nil {
			return err
		}
	}
	return nil
}

// saveTMDbPoster writes what TMDb had, or the fact that it had nothing.
//
// The stamp goes on either way. Without it a title TMDb cannot help
// with comes back on every pass, which is the loop this whole job
// exists to get out of.
//
// A found picture clears the demand mark and takes the row out of
// `dead`: it has an address that answers again.
func (s *Store) saveTMDbPoster(ctx context.Context, tconst string, got tmdb.Found) error {
	if got.Poster == "" {
		_, err := s.pool.Exec(ctx, `
			UPDATE meta.posters
			SET tmdb_at = now(), wanted_at = NULL
			WHERE tconst = $1`, tconst)
		return err
	}
	var released any
	if !got.Released.IsZero() {
		released = got.Released
	}
	_, err := s.pool.Exec(ctx, `
		UPDATE meta.posters
		SET poster_url = $2,
		    released   = coalesce(released, $3::date),
		    status     = 'ok',
		    source     = 'tmdb',
		    tmdb_at    = now(),
		    wanted_at  = NULL,
		    fetched_at = now()
		WHERE tconst = $1`, tconst, got.Poster, released)
	if err == nil {
		s.notify(ctx, NotifyReady)
	}
	return err
}

// TMDbRest is how long the fallback waits after catching up. Longer
// than the OMDb backfill's: what it is waiting for is a reader to meet
// a film with no picture, which is not a thing that happens in bursts.
const TMDbRest = 10 * time.Minute

// fillFromTMDb keeps the fallback running for as long as the process
// does, beside the OMDb backfill rather than inside it: the two answer
// to different rate limits, and chaining them would drop the faster one
// to the pace of the slower.
func fillFromTMDb(ctx context.Context, job *TMDbJob, logger *slog.Logger, wakes *Wakes) {
	waited := false
	for {
		ready, err := job.Store.LiveReady(ctx)
		wait := TMDbRest
		switch {
		case err != nil || !ready:
			if !waited {
				logger.Info("tmdb posters waiting for a catalog")
				waited = true
			}
			wait = PosterWaitForCatalog
		default:
			waited = false
			err := job.Run(ctx)
			if err != nil && ctx.Err() == nil {
				logger.Warn("tmdb posters", "err", err)
			}
			reportRun(ctx, job.Notify, notify.JobTMDbPosters, err, time.Now().Add(wait))
		}
		// A reader who opens a film with no picture is the best reason
		// there is to ask TMDb about it, and they should not have to
		// wait out a ten minute sleep for the asking.
		if !waitFor(ctx, wakes.Wanted, wait) {
			return
		}
	}
}
