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
// job takes three kinds of work, in this order:
//
//   - what a reader has already tried to look at (meta.posters.wanted_at,
//     written by the read paths),
//   - a sweep of the well-known titles, so the common case is repaired
//     before anybody meets it, and
//   - the answers that have come due.
//
// tmdb_at is stamped whatever the answer, including "TMDb has nothing
// either": a second service having nothing is still an answer. TMDb's
// terms ask that anything cached from it be refreshed within six
// months, so once an answer is tmdbRefreshDays old it is asked again,
// after everything else: a picture of TMDb's, and TMDb's "nothing" for
// a title that still has no picture. One still there at tmdbForgetDays
// is cleared by the backstop in tmdbterms.go, which puts the title back
// in the queue.

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
// it is a few hours of a rate-limited API, and after it every title
// TMDb has a picture for has one. Nothing else changes — a title TMDb
// has nothing for is stamped either way, and asked again only when
// that answer comes due, like every other.
const TMDbSweepMinVotes = 100

// TMDbBatch is how many titles are claimed per round.
//
// Small, now that the queue has an index that returns a page in its
// own order: fifty at the default twenty requests a second is a few
// seconds, so a title a reader asks for waits about that long rather
// than behind a thousand nobody asked for. The page used to be a
// thousand only to amortise a sort of the entire remaining queue, which
// is the thing posters_tmdb_queue removed.
const TMDbBatch = 50

// TMDbJob fills in pictures OMDb could not.
type TMDbJob struct {
	Store  *Store
	Client PosterFinder
	Logger *slog.Logger
	// MinVotes is the sweep's floor. Zero means every title, which is
	// what it is set to when somebody wants the whole catalog filled.
	// The demand queue ignores it either way: a reader looking at a
	// film is a better reason than its vote count. So do the re-asks:
	// an answer was worth having once, and is worth keeping.
	MinVotes int
	// Batch is how many are claimed per round; zero takes TMDbBatch.
	Batch int
	// Notify hears when a pass starts, how far it has got, and how it
	// ends. Nil leaves that in the log.
	Notify notify.Sink
}

// Run repairs what it can before ctx is done. The queue is looked at
// again before every batch, so a title a reader wants never waits
// behind the sweep or the re-asks for longer than a batch.
//
// It takes no schema. Everything it needs is on meta.posters, which
// outlives every generation — so unlike the other jobs it does not
// care which catalog is live, only that a row is waiting.
//
// One title at a time on purpose. The client's own limiter is the pace,
// and there is no burst worth chasing here: the whole queue after the
// first sweep is a handful of titles a reader has just met, and the
// answers that come due a day at a time.
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
		fresh := untried(ids, tried)
		if len(fresh) == 0 {
			// Nothing wanted and nothing left to sweep, bar this pass's
			// own failures. The answers that have come due are last.
			ids, err = j.Store.tmdbPostersDue(ctx, batch)
			if stopping(err) {
				return nil
			}
			if err != nil {
				return err
			}
			fresh = untried(ids, tried)
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
				// storing it is what stops the title coming round again
				// until the answer comes due.
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
			// And the overview, where OMDb has given no plot.
			if err := j.Store.keepTMDbOverview(ctx, id, got.Overview); err != nil && !stopping(err) {
				j.Logger.Warn("tmdb overview", "tconst", id, "err", err)
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
// the same sets tmdbWanted and tmdbPostersDue hand out, counted once at
// the start so the log can say how far through them the pass is.
func (s *Store) tmdbOutstanding(ctx context.Context, minVotes int) (int64, error) {
	var n int64
	err := s.pool.QueryRow(ctx, `
		SELECT
		    (SELECT count(*)
		     FROM meta.posters
		     WHERE tmdb_at IS NULL
		       AND (status = 'dead' OR poster_url IS NULL OR btrim(poster_url) = '')
		       AND (wanted_at IS NOT NULL OR coalesce(votes, 0) >= $1))
		  + (SELECT count(*) FROM meta.posters WHERE `+tmdbPosterDue("$2")+`)`,
		minVotes, tmdbRefreshDays).Scan(&n)
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
// out twice. An answer that has come due is tmdbPostersDue's.
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

// tmdbPosterDue is true of a poster row whose TMDb answer is older than
// days and still matters: a picture of TMDb's that is in use, or TMDb's
// "nothing" for a title that still has no picture. TMDb's "nothing" for
// a title whose own picture works is not asked again: that title is not
// waiting on TMDb, and the backstop clears the stamp in time.
func tmdbPosterDue(days string) string {
	return `tmdb_at < now() - make_interval(days => ` + days + `)
		  AND (source = 'tmdb' OR status = 'dead' OR poster_url IS NULL OR btrim(poster_url) = '')`
}

// tmdbPostersDue is the next answers to ask TMDb for again, oldest first.
//
// Asking again replaces the row in place: a new picture overwrites the
// old address in the same statement, so a reader never meets the title
// without one while it is refreshed. Only TMDb saying it has nothing
// takes a picture of its own away.
func (s *Store) tmdbPostersDue(ctx context.Context, limit int) ([]string, error) {
	ids, err := s.tconsts(ctx, `
		SELECT tconst
		FROM meta.posters
		WHERE `+tmdbPosterDue("$2")+`
		ORDER BY tmdb_at, tconst
		LIMIT $1`, limit, tmdbRefreshDays)
	if err != nil {
		return nil, fmt.Errorf("catalog: tmdb posters due: %w", err)
	}
	return ids, nil
}

// KeepTMDbPoster records what TMDb had for a title whose picture just
// failed to load.
//
// A picture replaces the address that failed. Nothing is still an
// answer: the title is not asked again until that answer comes due, and
// an address of OMDb's it already has stays, so the card can try that
// one once more. The overview on the same answer is kept where OMDb has
// given no plot.
func (s *Store) KeepTMDbPoster(ctx context.Context, tconst string, got tmdb.Found) error {
	if err := s.saveTMDbPoster(ctx, tconst, got); err != nil {
		return err
	}
	if got.ID > 0 || got.Poster == "" {
		if err := s.rememberTMDB(ctx, tconst, got.ID); err != nil {
			return err
		}
	}
	return s.keepTMDbOverview(ctx, tconst, got.Overview)
}

// saveTMDbPoster writes what TMDb had, or the fact that it had nothing.
//
// The stamp goes on either way. Without it a title TMDb cannot help
// with comes back on every pass, which is the loop this whole job
// exists to get out of.
//
// A found picture clears the demand mark and takes the row out of
// `dead`: it has an address that answers again.
//
// TMDb answering that it has nothing takes away a picture of its own,
// the one a re-ask was asking about: TMDb no longer stands behind it,
// and its terms do not allow keeping it past its six months. An address
// of OMDb's is kept.
//
// The opening screen's colour goes with a picture that changes or goes:
// it was the average of the old one, and a frame the wrong colour is
// worse than none. The colour job works the new one out.
//
// A release date is taken from TMDb only where OMDb gave none, and is
// marked as TMDb's, so that a later answer replaces it and "nothing"
// takes it away, as it does TMDb's picture. OMDb's date is never
// touched.
func (s *Store) saveTMDbPoster(ctx context.Context, tconst string, got tmdb.Found) error {
	if got.Poster == "" {
		_, err := s.pool.Exec(ctx, `
			UPDATE meta.posters
			SET tmdb_at       = now(),
			    wanted_at     = NULL,
			    poster_url    = CASE WHEN source = 'tmdb' THEN NULL ELSE poster_url END,
			    colour        = CASE WHEN source = 'tmdb' THEN NULL ELSE colour END,
			    source        = CASE WHEN source = 'tmdb' THEN NULL ELSE source END,
			    released      = CASE WHEN released_tmdb THEN NULL ELSE released END,
			    released_tmdb = false
			WHERE tconst = $1`, tconst)
		return err
	}
	var released any
	if !got.Released.IsZero() {
		released = got.Released
	}
	_, err := s.pool.Exec(ctx, `
		UPDATE meta.posters
		SET poster_url    = $2,
		    colour        = CASE WHEN poster_url IS DISTINCT FROM $2 THEN NULL ELSE colour END,
		    released      = CASE WHEN released IS NULL OR released_tmdb THEN $3::date ELSE released END,
		    released_tmdb = CASE WHEN released IS NULL OR released_tmdb THEN $3::date IS NOT NULL ELSE false END,
		    status        = 'ok',
		    source        = 'tmdb',
		    tmdb_at       = now(),
		    wanted_at     = NULL,
		    fetched_at    = now()
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
