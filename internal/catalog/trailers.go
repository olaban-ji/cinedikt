package catalog

// The trailer a film plays in place.
//
// Kept in meta.trailers. TrailerJob does every lookup;
// GET /api/trailers/{tconst} only reads what it has kept, so no reader
// ever waits on TMDb or YouTube. A film opened before the job has
// reached it is marked wanted, which wakes the job and puts the film at
// the front of its queue, and the reader is told the answer is on its
// way. Behind the wanted films the job sweeps every film in the catalog,
// most voted first, and then asks again about the answers that have
// come due.

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

// TrailerSweepMinVotes is how well known a film has to be for the sweep
// to reach it. Zero is every film the id matcher would match: the sweep
// fills the whole catalog, and a film below a higher floor is still
// looked up the moment a reader opens it.
const TrailerSweepMinVotes = 0

// TrailerNullRetry is how old a "no trailer" answer has to be before it
// is asked again, for a film released in the last twelve months.
// Trailers are often added after release.
const TrailerNullRetry = 7 * 24 * time.Hour

// trailerNullRetryDays is TrailerNullRetry in the database's terms.
const trailerNullRetryDays = 7

// TrailerBatch is how many titles the job claims per round. A reader's
// mark is heard between titles, not between rounds, so this is not how
// long a reader waits.
const TrailerBatch = 50

// TrailerRest is how long the job waits after catching up, and how often
// it looks for films to queue. A reader opening a film with no answer,
// or a new generation, wakes it sooner.
const TrailerRest = 30 * time.Minute

// ReaskTrailer reports whether a stored answer should be asked again
// because the film is recent: a "no trailer" older than
// TrailerNullRetry, for a film that came out in the last twelve months.
// A "no trailer" for a film not out yet is asked again once the film is
// out and the answer is more than a week old.
//
// released is the release day, or the first of January of the film's
// year when the day is not known; zero when neither is. recentNull says
// the same thing in SQL, and the two must agree.
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
}

// Due reports whether a stored answer should be asked again: a recent
// film's week-old "no trailer", which ReaskTrailer describes, or any
// answer old enough that TMDb's terms want it asked again. The endpoint
// still serves a due answer, and marks the film so the job asks again.
// trailerDue says the same thing in SQL, and the two must agree.
func (r TrailerRow) Due(now time.Time) bool {
	if !r.Asked {
		return false
	}
	return ReaskTrailer(r.Key, r.AskedAt, r.Released, now) || now.Sub(r.AskedAt) > TMDbRefreshAfter
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
		       coalesce(p.released, make_date(t.start_year, 1, 1))
		FROM (SELECT $1::text AS tconst) q
		LEFT JOIN `+Live+`.titles t ON t.tconst = q.tconst
		LEFT JOIN meta.posters p ON p.tconst = q.tconst
		LEFT JOIN meta.trailers tr ON tr.tconst = q.tconst`, tconst).
		Scan(&row.Title, &row.Asked, &key, &asked, &released)
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
// none. Either way the title leaves the job's queue, wanted or not.
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

// keepTMDbFind records what a lookup by IMDb id turned up: TMDb's id for
// the title, or that it has none, and its overview where OMDb has given
// no plot. The same two writes the id matcher makes, so neither it nor
// anything else asks TMDb about the title again until the match comes
// due.
func (s *Store) keepTMDbFind(ctx context.Context, tconst string, got tmdb.Found) error {
	if err := s.rememberTMDB(ctx, tconst, got.ID); err != nil {
		return err
	}
	return s.keepTMDbOverview(ctx, tconst, got.Overview)
}

// TrailerLookup is what the trailer job needs from TMDb: the id of a
// title the id matcher has not reached yet, and a movie's clips.
type TrailerLookup interface {
	PosterFinder
	trailer.Lister
}

// TrailerJob looks up the trailer of every film a reader can open.
type TrailerJob struct {
	Store *Store
	// TMDb is the runner's client, and with it the process's one
	// limiter.
	TMDb TrailerLookup
	// Check is YouTube's oEmbed, with a limiter of its own.
	Check  trailer.Checker
	Logger *slog.Logger
	// MinVotes is the sweep's floor. A film a reader has opened is
	// looked up whatever its votes.
	MinVotes int
	// Batch is how many are claimed per round; zero takes TrailerBatch.
	Batch int
	// Wanted is the wake a reader's mark sends. A pass that hears it
	// stops the round it is in at the next title and goes back for the
	// wanted films, so a reader waits on the lookup in hand rather than
	// on the rest of a round of the sweep. Nil is never interrupted.
	Wanted <-chan struct{}
	// Notify hears when a pass starts, how far it has got, and how it
	// ends. Nil leaves that in the log.
	Notify notify.Sink

	// refilled is when the queue was last refilled from the catalog.
	// That scans every film, and a reader's marks wake the job every few
	// seconds while the sweep has not reached what they open; the marks
	// queue their own titles, so only the rest interval and a new
	// generation bring a refill. The zero value refills on the next
	// pass.
	refilled time.Time
}

// trailerTitle is one title the job is about to ask about.
type trailerTitle struct {
	tconst string
	// matched is whether meta.tmdb has an answer for the title, and
	// tmdbID is that answer: zero for "TMDb has no movie for it".
	matched bool
	tmdbID  int
	// wantedAt is when a reader last marked the title, for a title taken
	// as a wanted film; zero for the sweep and the re-asks.
	wantedAt time.Time
}

// Run asks what it can before ctx is done, in four kinds, each only once
// the ones before it have nothing left to give: films a reader has
// opened, newest first; every film never asked, most voted first; "no
// trailer" answers for recent films, which ReaskTrailer describes; and
// answers old enough that TMDb's terms want them asked again. The
// wanted films are looked at again before every round, and whenever a
// reader's mark arrives.
func (j *TrailerJob) Run(ctx context.Context) error {
	batch := j.Batch
	if batch <= 0 {
		batch = TrailerBatch
	}
	if time.Since(j.refilled) >= TrailerRest {
		if err := j.Store.refillTrailerQueue(ctx, j.MinVotes); err != nil {
			if stopping(err) {
				return nil
			}
			return err
		}
		j.refilled = time.Now()
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
	// asked counts the answers that cost a request. A film TMDb has no
	// movie for is answered without one, and must not make a pass in
	// which every request failed look like one that got through.
	var asked int64
	var last error
	// A failed lookup stores nothing, so it stays at the head of its
	// kind; this pass must not ask it again. A kind whose head is
	// nothing but this pass's failures gives way to the next, and the
	// pass ends once every kind's head is, so a TMDb or a YouTube that
	// is down costs a round of failed lookups per kind rather than the
	// whole queue, and one film that keeps failing does not hold up the
	// rest. A try is kept with the mark it served, zero when it served
	// none, and holds back only that mark: a film that failed in the
	// sweep, or failed for a reader, is asked about again as soon as a
	// reader marks it, rather than once a pass that can run for days is
	// over.
	tried := make(map[string]time.Time)
	for {
		if ctx.Err() != nil {
			j.Logger.Info("trailers paused", "found", found, "none", none, "failed", failed)
			return nil
		}
		titles, err := j.Store.trailersWanted(ctx, batch, j.MinVotes, tried)
		if stopping(err) {
			return nil
		}
		if err != nil {
			return err
		}
		if len(titles) == 0 {
			if found+none+failed > 0 {
				track.done(found + none + failed)
				j.Logger.Info("trailers caught up", "found", found, "none", none, "failed", failed)
			}
			if failed >= failedLookups && asked == 0 {
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
		for _, title := range titles {
			if ctx.Err() != nil {
				return nil
			}
			if j.heard() {
				// A reader has opened a film with no answer. The rest of
				// this round waits behind it.
				break
			}
			tried[title.tconst] = title.wantedAt
			free := title.matched && title.tmdbID <= 0
			key, match, err := j.lookup(ctx, title)
			// A match is kept even when the trailer lookup after it
			// failed, so the next try asks TMDb one thing, not two.
			if match != nil {
				if err := j.Store.keepTMDbFind(ctx, title.tconst, *match); err != nil {
					if stopping(err) {
						return nil
					}
					return err
				}
			}
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
			if !free {
				asked++
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

// heard reports whether a reader's mark has arrived, taking the wake if
// so: the pass that hears it is the one that serves it.
func (j *TrailerJob) heard() bool {
	select {
	case <-j.Wanted:
		return true
	default:
		return false
	}
}

// lookup asks for one title's trailer: a YouTube key, or "" for none.
//
// A title with a TMDb id is asked about by that id. A title TMDb has no
// movie for has no trailer either, and costs no request. A title the id
// matcher has not reached yet is matched here first, so a reader never
// waits on the matcher: match is what TMDb said, for the caller to keep,
// and is set whenever that question was answered, whatever became of
// the trailer lookup after it.
func (j *TrailerJob) lookup(ctx context.Context, title trailerTitle) (key string, match *tmdb.Found, err error) {
	id := title.tmdbID
	if !title.matched {
		got, err := j.TMDb.FindByIMDb(ctx, title.tconst)
		switch {
		case errors.Is(err, tmdb.ErrNotFound):
			got = tmdb.Found{}
		case err != nil:
			return "", nil, err
		}
		match = &got
		id = got.ID
	}
	if id <= 0 {
		return "", match, nil
	}
	key, err = trailer.Pick(ctx, j.TMDb, j.Check, id)
	return key, match, err
}

// refillTrailerQueue clears what has been answered since it was queued,
// or has left the catalog, and queues every film at or above the floor
// that has no answer, whatever TMDb has said about it so far. Only a
// title new to the queue, or one whose votes have moved, is written. A
// wanted title whose answer came due before the want is kept: that is
// the answer it is waiting to have replaced.
func (s *Store) refillTrailerQueue(ctx context.Context, minVotes int) error {
	if _, err := s.pool.Exec(ctx, `
		DELETE FROM meta.trailer_queue q
		WHERE EXISTS (
		      SELECT 1 FROM meta.trailers tr
		      WHERE tr.tconst = q.tconst
		        AND (q.wanted_at IS NULL OR tr.asked_at >= q.wanted_at))
		   OR NOT EXISTS (SELECT 1 FROM `+Live+`.titles t WHERE t.tconst = q.tconst)`); err != nil {
		return fmt.Errorf("catalog: clear answered trailer queue: %w", err)
	}
	err := s.fillQueue(ctx, `
		INSERT INTO meta.trailer_queue (tconst, votes)
		SELECT t.tconst, coalesce(r.num_votes, 0)
		FROM `+Live+`.titles t
		LEFT JOIN `+Live+`.ratings r USING (tconst)
		WHERE `+tmdbIDFilm+`
		  AND coalesce(r.num_votes, 0) >= $1
		  AND NOT EXISTS (SELECT 1 FROM meta.trailers tr WHERE tr.tconst = t.tconst)
		  AND NOT EXISTS (
		      SELECT 1 FROM meta.trailer_queue q
		      WHERE q.tconst = t.tconst AND q.votes = coalesce(r.num_votes, 0))
		ON CONFLICT (tconst) DO UPDATE SET votes = EXCLUDED.votes`, minVotes)
	if err != nil {
		return fmt.Errorf("catalog: fill trailer queue: %w", err)
	}
	return nil
}

// markTrailersWanted puts the titles a reader opened at the front of the
// trailer job's queue, whatever their votes and whatever kind of title
// they are: somebody opened it, which is reason enough.
//
// Only a title that still needs asking is marked: one with no answer, or
// one whose answer has come due. By the time a mark is written the job
// may already have answered it, and marking that would ask again for an
// answer a few seconds old.
func (s *Store) markTrailersWanted(ctx context.Context, ids []string) error {
	_, err := s.pool.Exec(ctx, `
		INSERT INTO meta.trailer_queue (tconst, votes, wanted_at)
		SELECT t.tconst, coalesce(r.num_votes, 0), now()
		FROM `+Live+`.titles t
		LEFT JOIN `+Live+`.ratings r USING (tconst)
		LEFT JOIN meta.posters p ON p.tconst = t.tconst
		LEFT JOIN meta.trailers tr ON tr.tconst = t.tconst
		WHERE t.tconst = ANY($1)
		  AND (tr.tconst IS NULL OR `+trailerDue("$2", "$3")+`)
		ON CONFLICT (tconst) DO UPDATE SET wanted_at = EXCLUDED.wanted_at`,
		ids, trailerNullRetryDays, tmdbRefreshDays)
	return err
}

// recentNull is ReaskTrailer in SQL, for the answer tr of the title t
// whose poster row is p: "no trailer" for a film that came out in the
// last twelve months, asked more than days ago.
func recentNull(days string) string {
	return `tr.youtube_key IS NULL
		  AND tr.asked_at < now() - make_interval(days => ` + days + `)
		  AND coalesce(p.released, make_date(t.start_year, 1, 1)) > now() - interval '12 months'
		  AND coalesce(p.released, make_date(t.start_year, 1, 1)) <= now()`
}

// trailerDue is TrailerRow.Due in SQL, with the same names recentNull
// uses: a recent film's week-old "no trailer", or any answer older than
// refreshDays.
func trailerDue(nullDays, refreshDays string) string {
	return `((` + recentNull(nullDays) + `)
		  OR tr.asked_at < now() - make_interval(days => ` + refreshDays + `))`
}

// The four kinds of work, as the FROM and WHERE that trailersWanted and
// trailersOutstanding share. Each names the title t and its TMDb answer
// m, and takes the placeholders for its settings, so every query names
// only the parameters it is given.

// trailerWantedFrom is the films a reader has opened that are still
// waiting: never asked, or asked before the reader wanted them, which
// the mark allows only for an answer that has come due.
const trailerWantedFrom = `
		FROM meta.trailer_queue q
		JOIN ` + Live + `.titles t ON t.tconst = q.tconst
		LEFT JOIN meta.tmdb m ON m.tconst = q.tconst
		WHERE q.wanted_at IS NOT NULL
		  AND NOT EXISTS (
		      SELECT 1 FROM meta.trailers tr
		      WHERE tr.tconst = q.tconst AND tr.asked_at >= q.wanted_at)`

// trailerQueued is the sweep: the films never asked, as far as the queue
// holds them at the floor, matched to TMDb or not.
func trailerQueued(votes string) string {
	return `
		FROM meta.trailer_queue q
		JOIN ` + Live + `.titles t ON t.tconst = q.tconst
		LEFT JOIN meta.tmdb m ON m.tconst = q.tconst
		WHERE q.votes >= ` + votes + `
		  AND NOT EXISTS (SELECT 1 FROM meta.trailers tr WHERE tr.tconst = q.tconst)`
}

// recentNullTrailers is "no trailer" for a film that came out in the
// last twelve months, asked more than a week ago, for a film TMDb has a
// movie for. One it has none for is certain to stay "no trailer".
func recentNullTrailers(days string) string {
	return `
		FROM meta.trailers tr
		JOIN meta.tmdb m ON m.tconst = tr.tconst AND m.tmdb_id IS NOT NULL
		JOIN ` + Live + `.titles t ON t.tconst = tr.tconst
		LEFT JOIN meta.posters p ON p.tconst = tr.tconst
		WHERE ` + recentNull(days)
}

// staleTrailers is any answer old enough that TMDb's terms want it asked
// again. One whose title has left the catalog, whose TMDb answer was
// "no movie", or whose re-ask keeps failing, is deleted by
// forgetDueTMDbData instead, and the sweep asks about it afresh.
func staleTrailers(days string) string {
	return `
		FROM meta.trailers tr
		JOIN meta.tmdb m ON m.tconst = tr.tconst AND m.tmdb_id IS NOT NULL
		JOIN ` + Live + `.titles t ON t.tconst = tr.tconst
		WHERE tr.asked_at < now() - make_interval(days => ` + days + `)`
}

// trailersOutstanding counts the four kinds of work, once, at the start
// of a pass. A title that is in two kinds is counted twice; the figure
// is for a progress line, not for bookkeeping.
func (s *Store) trailersOutstanding(ctx context.Context, minVotes int) (int64, error) {
	var n int64
	err := s.pool.QueryRow(ctx, `
		SELECT (SELECT count(*) `+trailerWantedFrom+`)
		     + (SELECT count(*) `+trailerQueued("$1")+`)
		     + (SELECT count(*) `+recentNullTrailers("$2")+`)
		     + (SELECT count(*) `+staleTrailers("$3")+`)`,
		minVotes, trailerNullRetryDays, tmdbRefreshDays).Scan(&n)
	if err != nil {
		return 0, fmt.Errorf("catalog: count titles wanting a trailer: %w", err)
	}
	return n, nil
}

// trailersWanted is the next round: the head of the first of the four
// kinds that has anything this pass has not already tried. A lookup
// that failed stores nothing, so it is still at the head of its kind,
// and is left out here until a reader marks the title after the mark
// that try served; tried holds that mark, zero for one that served none.
func (s *Store) trailersWanted(ctx context.Context, limit, minVotes int, tried map[string]time.Time) ([]trailerTitle, error) {
	for _, kind := range []struct {
		from  string
		args  []any
		want  string
		order string
	}{
		{trailerWantedFrom, nil, "q.wanted_at", "q.wanted_at DESC, q.tconst"},
		{trailerQueued("$2"), []any{minVotes}, "NULL::timestamptz", "q.votes DESC, q.tconst"},
		{recentNullTrailers("$2"), []any{trailerNullRetryDays}, "NULL::timestamptz", "tr.asked_at, tr.tconst"},
		{staleTrailers("$2"), []any{tmdbRefreshDays}, "NULL::timestamptz", "tr.asked_at, tr.tconst"},
	} {
		rows, err := s.pool.Query(ctx, `
			SELECT t.tconst, m.tconst IS NOT NULL, coalesce(m.tmdb_id, 0), `+kind.want+` `+kind.from+`
			ORDER BY `+kind.order+`
			LIMIT $1`, append([]any{limit}, kind.args...)...)
		if err != nil {
			return nil, fmt.Errorf("catalog: titles wanting a trailer: %w", err)
		}
		var fresh []trailerTitle
		for rows.Next() {
			var t trailerTitle
			var wantedAt *time.Time
			if err := rows.Scan(&t.tconst, &t.matched, &t.tmdbID, &wantedAt); err != nil {
				rows.Close()
				return nil, err
			}
			if wantedAt != nil {
				t.wantedAt = *wantedAt
			}
			if at, ok := tried[t.tconst]; ok && !t.wantedAt.After(at) {
				continue
			}
			fresh = append(fresh, t)
		}
		rows.Close()
		if err := rows.Err(); err != nil {
			return nil, err
		}
		if len(fresh) > 0 {
			return fresh, nil
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
		// A reader opening a film with no answer is waiting on this job,
		// and should not wait out its rest to be heard. A new generation
		// brings films to queue, so the next pass refills; the rest
		// interval covers a wake sent while nobody was listening, and
		// brings a refill of its own.
		woke, published := waitForTrailers(ctx, wakes, wait)
		if !woke {
			return
		}
		if published {
			job.refilled = time.Time{}
		}
	}
}

// waitForTrailers is waitFor for the trailer job, which has two wakes: a
// reader's mark, and a new generation. published says it was the second.
func waitForTrailers(ctx context.Context, wakes *Wakes, backstop time.Duration) (woke, published bool) {
	timer := time.NewTimer(backstop)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return false, false
	case <-wakes.Trailers:
		return true, true
	case <-wakes.TrailersWanted:
		return true, false
	case <-timer.C:
		return true, false
	}
}
