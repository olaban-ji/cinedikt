package catalog

// The trailer a film plays in place.
//
// Asked once per title and kept in meta.trailers. TrailerJob fills it
// ahead of readers for the best-known films that have a TMDb id, so most
// opens are answered from here; GET /api/trailers/{tconst} looks up the
// long tail the first time somebody opens one. Both choose through
// trailer.Pick, so either gives the same answer.

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/jackc/pgx/v5"

	"cinedikt/internal/notify"
	"cinedikt/internal/tmdb"
	"cinedikt/internal/trailer"
)

// TrailerSweepMinVotes is how well known a film has to be for the job to
// look up its trailer before anybody opens it.
const TrailerSweepMinVotes = 10000

// TrailerNullRetry is how old a "no trailer" answer has to be before it
// is asked again, for a film released in the last twelve months.
// Trailers are often added after release.
const TrailerNullRetry = 7 * 24 * time.Hour

// trailerNullRetryDays is TrailerNullRetry in the database's terms.
const trailerNullRetryDays = 7

// TrailerBatch is how many titles the job claims per round.
const TrailerBatch = 50

// TrailerRest is how long the job waits after catching up.
const TrailerRest = 30 * time.Minute

// ReaskTrailer reports whether a stored answer should be asked again on
// demand: a "no trailer" older than TrailerNullRetry, for a film that
// came out in the last twelve months. Any other stored answer stands.
// That includes a "no trailer" for a film not out yet, which is asked
// again once the film is out and the answer is more than a week old.
//
// released is the release day, or the first of January of the film's
// year when the day is not known; zero when neither is. The job's own
// query for these rows, in recentNullTrailers, says the same thing in
// SQL, and the two must agree.
func ReaskTrailer(key string, askedAt, released, now time.Time) bool {
	if key != "" || now.Sub(askedAt) <= TrailerNullRetry || released.IsZero() {
		return false
	}
	return released.After(now.AddDate(-1, 0, 0)) && !released.After(now)
}

// TrailerRow is everything the endpoint needs to answer for one title,
// read in one query.
type TrailerRow struct {
	// Title is whether the catalog holds the title at all.
	Title bool
	// Asked is whether meta.trailers has an answer; Key is that answer,
	// empty for "no trailer".
	Asked   bool
	Key     string
	AskedAt time.Time
	// Released is the release day, or the first of January of the
	// film's year. Zero when neither is known.
	Released time.Time
	// TMDbAsked is whether meta.tmdb has an answer; TMDbID is that
	// answer, zero for "TMDb has no movie for this title".
	TMDbAsked bool
	TMDbID    int
}

// Trailer reads what is stored about a title's trailer, and the facts
// that decide whether to ask again.
func (s *Store) Trailer(ctx context.Context, tconst string) (TrailerRow, error) {
	var row TrailerRow
	var key *string
	var asked, released *time.Time
	err := s.pool.QueryRow(ctx, `
		SELECT t.tconst IS NOT NULL,
		       tr.tconst IS NOT NULL, tr.youtube_key, tr.asked_at,
		       coalesce(p.released, make_date(t.start_year, 1, 1)),
		       m.tconst IS NOT NULL, coalesce(m.tmdb_id, 0)
		FROM (SELECT $1::text AS tconst) q
		LEFT JOIN `+Live+`.titles t ON t.tconst = q.tconst
		LEFT JOIN meta.posters p ON p.tconst = q.tconst
		LEFT JOIN meta.trailers tr ON tr.tconst = q.tconst
		LEFT JOIN meta.tmdb m ON m.tconst = q.tconst`, tconst).
		Scan(&row.Title, &row.Asked, &key, &asked, &released, &row.TMDbAsked, &row.TMDbID)
	if errors.Is(err, pgx.ErrNoRows) {
		return TrailerRow{}, nil
	}
	if err != nil {
		return TrailerRow{}, fmt.Errorf("catalog: trailer %s: %w", tconst, err)
	}
	if key != nil {
		row.Key = *key
	}
	if asked != nil {
		row.AskedAt = *asked
	}
	if released != nil {
		row.Released = *released
	}
	return row, nil
}

// KeepTrailer stores the answer for a title: a YouTube key, or "" for
// none. Either way the title leaves the job's queue.
func (s *Store) KeepTrailer(ctx context.Context, tconst, key string) error {
	_, err := s.pool.Exec(ctx, `
		INSERT INTO meta.trailers (tconst, youtube_key, asked_at)
		VALUES ($1, $2, now())
		ON CONFLICT (tconst) DO UPDATE
		SET youtube_key = EXCLUDED.youtube_key,
		    asked_at    = EXCLUDED.asked_at`, tconst, textOrNull(key))
	if err != nil {
		return fmt.Errorf("catalog: keep trailer %s: %w", tconst, err)
	}
	if _, err := s.pool.Exec(ctx, `DELETE FROM meta.trailer_queue WHERE tconst = $1`, tconst); err != nil {
		return fmt.Errorf("catalog: trailer queue %s: %w", tconst, err)
	}
	return nil
}

// KeepTMDbFind records what a lookup by IMDb id turned up, for a caller
// that had to make one: TMDb's id for the title, or that it has none,
// and its overview where OMDb has given no plot.
func (s *Store) KeepTMDbFind(ctx context.Context, tconst string, got tmdb.Found) error {
	if err := s.rememberTMDB(ctx, tconst, got.ID); err != nil {
		return err
	}
	return s.keepTMDbOverview(ctx, tconst, got.Overview)
}

// TrailerJob looks up trailers ahead of readers.
type TrailerJob struct {
	Store *Store
	// Videos is TMDb, through the runner's client and the process's one
	// limiter.
	Videos trailer.Lister
	// Check is YouTube's oEmbed, the same checker the endpoint uses, so
	// the two together keep to its budget.
	Check  trailer.Checker
	Logger *slog.Logger
	// MinVotes is the sweep's floor.
	MinVotes int
	// Batch is how many are claimed per round; zero takes TrailerBatch.
	Batch int
	// Notify hears when a pass starts, how far it has got, and how it
	// ends. Nil leaves that in the log.
	Notify notify.Sink
}

// trailerTitle is one title the job is about to ask about.
type trailerTitle struct {
	tconst string
	tmdbID int
}

// Run asks what it can before ctx is done, in three kinds, each only
// once the one before it is empty: well-known films never asked, most
// voted first; "no trailer" answers for recent films, which ReaskTrailer
// describes; and answers old enough that TMDb's terms want them asked
// again.
func (j *TrailerJob) Run(ctx context.Context) error {
	batch := j.Batch
	if batch <= 0 {
		batch = TrailerBatch
	}
	if err := j.Store.refillTrailerQueue(ctx, j.MinVotes); err != nil {
		if stopping(err) {
			return nil
		}
		return err
	}
	outstanding, err := j.Store.trailersOutstanding(ctx, j.MinVotes)
	if stopping(err) {
		return nil
	}
	if err != nil {
		return err
	}
	if outstanding == 0 {
		return nil
	}
	track := newProgress(j.Logger, "finding trailers", outstanding)
	track.watch(j.Notify, notify.Event{Job: notify.JobTrailers})
	run := pass{sink: j.Notify, job: notify.JobTrailers}
	var found, none, failed int64
	var last error
	// A failed lookup stores nothing, so it stays at the head of its
	// kind; this pass must not ask it again. A head made of nothing but
	// this pass's failures ends the pass, so a TMDb or a YouTube that is
	// down costs one batch of failed lookups rather than the whole queue.
	tried := make(map[string]bool)
	for {
		if ctx.Err() != nil {
			j.Logger.Info("trailers paused", "found", found, "none", none, "failed", failed)
			return nil
		}
		titles, err := j.Store.trailersWanted(ctx, batch, j.MinVotes)
		if stopping(err) {
			return nil
		}
		if err != nil {
			return err
		}
		var fresh []trailerTitle
		for _, title := range titles {
			if !tried[title.tconst] {
				fresh = append(fresh, title)
			}
		}
		if len(fresh) == 0 {
			if found+none+failed > 0 {
				track.done(found + none + failed)
				j.Logger.Info("trailers caught up", "found", found, "none", none, "failed", failed)
			}
			if failed >= failedLookups && found+none == 0 {
				// Either service can be the one that is down; say which.
				provider := "TMDb"
				var check *trailer.CheckError
				if errors.As(last, &check) {
					provider = "YouTube"
				}
				return &LookupsFailedError{Provider: provider, Count: failed, Last: last}
			}
			run.finish(found, none, failed)
			return nil
		}
		run.start(outstanding)
		for _, title := range fresh {
			if ctx.Err() != nil {
				return nil
			}
			tried[title.tconst] = true
			key, err := trailer.Pick(ctx, j.Videos, j.Check, title.tmdbID)
			switch {
			case stopping(err):
				return nil
			case refused(err):
				return &KeyError{Provider: "TMDb", Err: err}
			case err != nil:
				failed++
				last = err
				j.Logger.Warn("trailer lookup failed", "tconst", title.tconst, "err", err)
				track.step(found + none + failed)
				continue
			}
			if err := j.Store.KeepTrailer(ctx, title.tconst, key); err != nil {
				if stopping(err) {
					return nil
				}
				return err
			}
			if key != "" {
				found++
			} else {
				none++
			}
			track.step(found + none + failed)
		}
	}
}

// refillTrailerQueue clears what has been answered and queues every
// well-known title with a TMDb id and no answer. Only a title new to the
// queue, or one whose votes have moved, is written.
func (s *Store) refillTrailerQueue(ctx context.Context, minVotes int) error {
	if _, err := s.pool.Exec(ctx, `
		DELETE FROM meta.trailer_queue q
		WHERE EXISTS (SELECT 1 FROM meta.trailers tr WHERE tr.tconst = q.tconst)`); err != nil {
		return fmt.Errorf("catalog: clear answered trailer queue: %w", err)
	}
	_, err := s.pool.Exec(ctx, `
		INSERT INTO meta.trailer_queue (tconst, votes)
		SELECT m.tconst, coalesce(r.num_votes, 0)
		FROM meta.tmdb m
		JOIN `+Live+`.titles t ON t.tconst = m.tconst
		LEFT JOIN `+Live+`.ratings r ON r.tconst = m.tconst
		WHERE m.tmdb_id IS NOT NULL
		  AND coalesce(r.num_votes, 0) >= $1
		  AND NOT EXISTS (SELECT 1 FROM meta.trailers tr WHERE tr.tconst = m.tconst)
		  AND NOT EXISTS (
		      SELECT 1 FROM meta.trailer_queue q
		      WHERE q.tconst = m.tconst AND q.votes = coalesce(r.num_votes, 0))
		ON CONFLICT (tconst) DO UPDATE SET votes = EXCLUDED.votes`, minVotes)
	if err != nil {
		return fmt.Errorf("catalog: fill trailer queue: %w", err)
	}
	return nil
}

// The three kinds of work, as the FROM and WHERE that trailersWanted and
// trailersOutstanding share. Each joins the TMDb id it will ask with, and
// takes the placeholder for its one setting, so every query names only
// the parameters it is given.

// trailerQueued is the titles never asked, as far as the queue still
// holds them at the floor.
func trailerQueued(votes string) string {
	return `
		FROM meta.trailer_queue q
		JOIN meta.tmdb m ON m.tconst = q.tconst AND m.tmdb_id IS NOT NULL
		JOIN ` + Live + `.titles t ON t.tconst = q.tconst
		WHERE q.votes >= ` + votes + `
		  AND NOT EXISTS (SELECT 1 FROM meta.trailers tr WHERE tr.tconst = q.tconst)`
}

// recentNullTrailers is "no trailer" for a film that came out in the
// last twelve months, asked more than a week ago. It is ReaskTrailer in
// SQL.
func recentNullTrailers(days string) string {
	return `
		FROM meta.trailers tr
		JOIN meta.tmdb m ON m.tconst = tr.tconst AND m.tmdb_id IS NOT NULL
		JOIN ` + Live + `.titles t ON t.tconst = tr.tconst
		LEFT JOIN meta.posters p ON p.tconst = tr.tconst
		WHERE tr.youtube_key IS NULL
		  AND tr.asked_at < now() - make_interval(days => ` + days + `)
		  AND coalesce(p.released, make_date(t.start_year, 1, 1)) > now() - interval '12 months'
		  AND coalesce(p.released, make_date(t.start_year, 1, 1)) <= now()`
}

// staleTrailers is any answer old enough that TMDb's terms want it asked
// again. One whose title has left the catalog, or whose re-ask keeps
// failing, is deleted by forgetDueTMDbData instead.
func staleTrailers(days string) string {
	return `
		FROM meta.trailers tr
		JOIN meta.tmdb m ON m.tconst = tr.tconst AND m.tmdb_id IS NOT NULL
		JOIN ` + Live + `.titles t ON t.tconst = tr.tconst
		WHERE tr.asked_at < now() - make_interval(days => ` + days + `)`
}

// trailersOutstanding counts the three kinds of work, once, at the start
// of a pass. A row that is both recent and stale is counted twice; the
// figure is for a progress line, not for bookkeeping.
func (s *Store) trailersOutstanding(ctx context.Context, minVotes int) (int64, error) {
	var n int64
	err := s.pool.QueryRow(ctx, `
		SELECT (SELECT count(*) `+trailerQueued("$1")+`)
		     + (SELECT count(*) `+recentNullTrailers("$2")+`)
		     + (SELECT count(*) `+staleTrailers("$3")+`)`,
		minVotes, trailerNullRetryDays, tmdbRefreshDays).Scan(&n)
	if err != nil {
		return 0, fmt.Errorf("catalog: count titles wanting a trailer: %w", err)
	}
	return n, nil
}

// trailersWanted is the head of the first of the three kinds that has
// any. A lookup that failed stores nothing, so it is still here: the job
// leaves out what it has already tried this pass.
func (s *Store) trailersWanted(ctx context.Context, limit, minVotes int) ([]trailerTitle, error) {
	for _, kind := range []struct {
		from  string
		arg   int
		order string
	}{
		{trailerQueued("$2"), minVotes, "q.votes DESC, q.tconst"},
		{recentNullTrailers("$2"), trailerNullRetryDays, "tr.asked_at, tr.tconst"},
		{staleTrailers("$2"), tmdbRefreshDays, "tr.asked_at, tr.tconst"},
	} {
		rows, err := s.pool.Query(ctx, `
			SELECT m.tconst, m.tmdb_id `+kind.from+`
			ORDER BY `+kind.order+`
			LIMIT $1`, limit, kind.arg)
		if err != nil {
			return nil, fmt.Errorf("catalog: titles wanting a trailer: %w", err)
		}
		var out []trailerTitle
		for rows.Next() {
			var t trailerTitle
			if err := rows.Scan(&t.tconst, &t.tmdbID); err != nil {
				rows.Close()
				return nil, err
			}
			out = append(out, t)
		}
		rows.Close()
		if err := rows.Err(); err != nil {
			return nil, err
		}
		if len(out) > 0 {
			return out, nil
		}
	}
	return nil, nil
}

// fillTrailers keeps the trailer job running for as long as the process
// does. It shares the runner's TMDb client, and with it the process's
// one limiter, so its requests and the other jobs' add up to one budget.
func fillTrailers(ctx context.Context, job *TrailerJob, logger *slog.Logger, wakes *Wakes) {
	waited := false
	for {
		ready, err := job.Store.LiveReady(ctx)
		wait := TrailerRest
		switch {
		case err != nil || !ready:
			if !waited {
				logger.Info("trailers waiting for a catalog")
				waited = true
			}
			wait = PosterWaitForCatalog
		default:
			waited = false
			err := job.Run(ctx)
			if err != nil && ctx.Err() == nil {
				logger.Warn("trailers", "err", err)
			}
			reportRun(ctx, job.Notify, notify.JobTrailers, err, time.Now().Add(wait))
		}
		if !waitFor(ctx, wakes.Trailers, wait) {
			return
		}
	}
}
