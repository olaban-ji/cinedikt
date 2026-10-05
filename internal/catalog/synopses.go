package catalog

// What a film is about.
//
// OMDb is the source. The poster pass already asks it about every title
// and now keeps the plot from each answer, so a title reached from here
// on costs nothing extra. What that pass reached before it kept plots is
// asked for again by SynopsisJob, through the same client, so the two
// share one rate limit and stop together when the daily quota is spent.
//
// TMDb's overview comes free on the answers the TMDb jobs and the poster
// stand-in already save. It fills in only where OMDb has nothing, it
// never stops OMDb being asked, and a row of TMDb's is asked of OMDb
// again, or dropped, after 150 days.

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"sync/atomic"
	"time"

	"cinedikt/internal/notify"
	"cinedikt/internal/omdb"
)

// SynopsisBatch is how many titles are claimed per round. Small, so a
// title a reader has just met waits behind a handful rather than a page.
const SynopsisBatch = 50

// DefaultSynopsisWorkers is how many lookups the synopsis job has in
// flight. Modest on purpose: it shares the poster pass's client and its
// limit, and has no deadline to beat.
const DefaultSynopsisWorkers = 8

// SynopsisRest is how long the job waits after catching up, and how
// often it looks for well-known films to queue. A reader meeting a title
// OMDb has not answered for wakes it sooner.
const SynopsisRest = 20 * time.Minute

// synopsisFilm is the sweep's film test: the titles a map could draw a
// card for. Demand needs no test, because a card has already been drawn.
const synopsisFilm = `
	NOT t.is_adult
	AND t.start_year IS NOT NULL
	AND NOT (t.genres @> ARRAY['Documentary'])`

// omdbUnanswered is true of a title OMDb has not answered for, with the
// title's id in place of tconst. A row holding only TMDb's overview
// still counts as unanswered: OMDb is the better source, and an overview
// that happened to arrive first must not stop it being asked.
func omdbUnanswered(tconst string) string {
	return `NOT EXISTS (SELECT 1 FROM meta.synopses s WHERE s.tconst = ` + tconst + ` AND s.omdb_at IS NOT NULL)`
}

// posterPassAnswered is true of a title the OMDb poster pass has an
// answer for, with the title's id in place of tconst. The synopsis job
// asks only about those: a title the pass has not answered yet will be
// asked by the pass, which keeps the plot from that same answer, and
// asking it here too would spend two of the day's OMDb requests on one
// title.
func posterPassAnswered(tconst string) string {
	return `EXISTS (SELECT 1 FROM meta.posters p WHERE p.tconst = ` + tconst + ` AND p.status <> 'missing')`
}

// textOrNull is s as a nullable column: empty is no value at all.
func textOrNull(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}

// writeOMDbSynopses stores what OMDb answered for each title: its plot,
// or null when it had none. Either way omdb_at records that OMDb has
// answered, so the synopsis job does not ask again.
//
// A plot always replaces what is there. OMDb having none replaces only
// another OMDb answer, an empty row, or a TMDb overview old enough to
// be due: a fresh overview of TMDb's is a better thing to show than
// nothing, and it is dropped on schedule all the same.
func (s *Store) writeOMDbSynopses(ctx context.Context, ids []string, plots []*string) error {
	if len(ids) == 0 {
		return nil
	}
	const replace = `(EXCLUDED.overview IS NOT NULL
		   OR meta.synopses.source = 'omdb'
		   OR meta.synopses.overview IS NULL
		   OR meta.synopses.fetched_at < now() - make_interval(days => $3))`
	_, err := s.pool.Exec(ctx, `
		INSERT INTO meta.synopses (tconst, overview, source, fetched_at, omdb_at)
		SELECT u.tconst, u.overview, 'omdb', now(), now()
		FROM unnest($1::text[], $2::text[]) AS u(tconst, overview)
		ON CONFLICT (tconst) DO UPDATE
		SET omdb_at    = now(),
		    overview   = CASE WHEN `+replace+` THEN EXCLUDED.overview ELSE meta.synopses.overview END,
		    source     = CASE WHEN `+replace+` THEN 'omdb' ELSE meta.synopses.source END,
		    fetched_at = CASE WHEN `+replace+` THEN now() ELSE meta.synopses.fetched_at END`,
		ids, plots, tmdbRefreshDays)
	if err != nil {
		return fmt.Errorf("catalog: save %d synopses: %w", len(ids), err)
	}
	return nil
}

// keepTMDbOverview stores TMDb's overview where OMDb has given no plot.
//
// It never replaces an OMDb plot, and it leaves omdb_at alone, so the
// synopsis job still asks OMDb about a title that has only TMDb's text.
// An empty overview writes nothing: there is nothing to show, and a row
// of TMDb's saying "none" would only be one more to refresh.
func (s *Store) keepTMDbOverview(ctx context.Context, tconst, overview string) error {
	if overview == "" {
		return nil
	}
	_, err := s.pool.Exec(ctx, `
		INSERT INTO meta.synopses (tconst, overview, source, fetched_at)
		VALUES ($1, $2, 'tmdb', now())
		ON CONFLICT (tconst) DO UPDATE
		SET overview   = EXCLUDED.overview,
		    source     = EXCLUDED.source,
		    fetched_at = EXCLUDED.fetched_at
		WHERE meta.synopses.source = 'tmdb'
		   OR meta.synopses.overview IS NULL`, tconst, overview)
	if err != nil {
		return fmt.Errorf("catalog: keep tmdb overview %s: %w", tconst, err)
	}
	return nil
}

// SynopsisJob asks OMDb for the plots the poster pass did not keep.
type SynopsisJob struct {
	Store *Store
	// Client must be the poster pass's own client, the same value, so
	// the two share one rate limit and one daily-quota pause: when OMDb
	// says the day's requests are spent, both stop until it resets.
	Client PosterFiller
	Logger *slog.Logger
	// MinVotes is the sweep's floor; zero sweeps every film. Titles a
	// reader has met are asked about whatever their votes.
	MinVotes int
	// Batch is how many are claimed per round; zero takes SynopsisBatch.
	Batch int
	// Workers is how many lookups are in flight; zero takes
	// DefaultSynopsisWorkers.
	Workers int
	// Notify hears when a pass starts, how far it has got, and how it
	// ends. Nil leaves that in the log.
	Notify notify.Sink

	// refilled is when the queue was last refilled from the catalog.
	// That scans the whole live catalog, and a reader's marks wake the
	// job every few seconds while anybody is browsing; the marks put
	// their own titles in the queue, so only the rest interval brings
	// a refill. The zero value refills on the first pass.
	refilled time.Time
}

// errSkipped marks a title a worker did not get to ask about, because
// the pass was already stopping.
var errSkipped = errors.New("catalog: not asked")

type plotAnswer struct {
	id   string
	plot string
	err  error
}

// Run asks what it can before ctx is done. Titles a reader has met go
// first, then the best known, then TMDb overviews that have come due.
// A title OMDb has never answered for is asked about here only once the
// poster pass has answered it.
func (j *SynopsisJob) Run(ctx context.Context) error {
	// A spent daily quota is shared with the poster pass. Asking anyway
	// would start a pass only to pause it again.
	if _, paused := omdbPaused(j.Client); paused {
		return nil
	}
	batch := j.Batch
	if batch <= 0 {
		batch = SynopsisBatch
	}
	workers := j.Workers
	if workers <= 0 {
		workers = DefaultSynopsisWorkers
	}
	if time.Since(j.refilled) >= SynopsisRest {
		if err := j.Store.refillSynopsisQueue(ctx, j.MinVotes); err != nil {
			if stopping(err) {
				return nil
			}
			return err
		}
		j.refilled = time.Now()
	}
	outstanding, err := j.Store.synopsesOutstanding(ctx, j.MinVotes)
	if stopping(err) {
		return nil
	}
	if err != nil {
		return err
	}
	if outstanding == 0 {
		return nil
	}
	track := newProgress(j.Logger, "filling in synopses", outstanding)
	track.watch(j.Notify, notify.Event{Job: notify.JobSynopses})
	run := pass{sink: j.Notify, job: notify.JobSynopses}
	// found is plots saved; blank is titles OMDb answered for with none.
	var found, blank, failed int64
	var last error
	// A failed lookup is not written down, so it stays at the head of
	// the queue; this pass must not ask it again. A head made of nothing
	// but this pass's failures ends the pass, so an OMDb that is down
	// costs one batch of failed requests rather than the whole queue.
	// Only a title left unanswered is kept: an answer takes its title
	// out of the queue, and a sweep of every film would otherwise hold
	// every title it has asked about for as long as the pass runs.
	tried := make(map[string]bool)
	for {
		if ctx.Err() != nil {
			j.Logger.Info("synopses paused", "found", found, "none", blank, "failed", failed)
			return nil
		}
		ids, err := j.Store.synopsesWanted(ctx, batch, j.MinVotes)
		if stopping(err) {
			return nil
		}
		if err != nil {
			return err
		}
		fresh := untried(ids, tried)
		if len(fresh) == 0 {
			if found+blank+failed > 0 {
				track.done(found + blank + failed)
				j.Logger.Info("synopses caught up", "found", found, "none", blank, "failed", failed)
			}
			if failed >= failedLookups && found+blank == 0 {
				return &LookupsFailedError{Provider: "OMDb", Count: failed, Last: last}
			}
			run.finish(found, blank, failed)
			return nil
		}
		run.start(outstanding)

		var answered []string
		var plots []*string
		var spent bool
		var keyErr error
		for _, a := range j.ask(ctx, fresh, workers) {
			switch {
			case a.err == nil, errors.Is(a.err, omdb.ErrNotFound), errors.Is(a.err, omdb.ErrUnreadable):
				// OMDb answered. Having nothing is an answer too, and
				// storing it is what stops the title coming round again.
				// So is an answer that cannot be read even once repaired:
				// OMDb sends the same bytes every time, so it is OMDb
				// having no synopsis to give, not a lookup to retry.
				if errors.Is(a.err, omdb.ErrUnreadable) {
					j.Logger.Info("OMDb's answer is unreadable; no synopsis from it", "tconst", a.id, "err", a.err)
				}
				answered = append(answered, a.id)
				plots = append(plots, textOrNull(a.plot))
				if a.plot != "" {
					found++
				} else {
					blank++
				}
			case errors.Is(a.err, omdb.ErrQuota):
				spent = true
			case errors.Is(a.err, omdb.ErrKey):
				keyErr = a.err
			case errors.Is(a.err, errSkipped):
			case stopping(a.err):
				// The pass stopping, or this one request running out of
				// time: the OMDb client's own timeout is a deadline too.
				// Either way the title is not asked again this pass.
				tried[a.id] = true
			default:
				tried[a.id] = true
				failed++
				last = a.err
				j.Logger.Warn("synopsis lookup failed", "tconst", a.id, "err", a.err)
			}
		}
		if err := j.Store.saveOMDbSynopses(ctx, answered, plots); err != nil {
			if stopping(err) {
				return nil
			}
			return err
		}
		track.step(found + blank + failed)
		if keyErr != nil {
			return &KeyError{Provider: "OMDb", Err: keyErr}
		}
		if spent {
			j.Logger.Info("synopses paused on the daily limit", "found", found, "none", blank, "failed", failed)
			run.pause(notify.DailyLimit, found, failed, quotaResumes(j.Client))
			return nil
		}
	}
}

// ask looks up one round of titles across the workers. The answers come
// back in the order the ids were given, whatever order they finished in.
func (j *SynopsisJob) ask(ctx context.Context, ids []string, workers int) []plotAnswer {
	out := make([]plotAnswer, len(ids))
	next := make(chan int)
	// Set on a spent quota or a refused key. Every lookup after it would
	// be told the same thing.
	var stop atomic.Bool
	var wg sync.WaitGroup
	for w := 0; w < min(workers, len(ids)); w++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := range next {
				if ctx.Err() != nil || stop.Load() {
					out[i] = plotAnswer{id: ids[i], err: errSkipped}
					continue
				}
				got, err := j.Client.Lookup(ctx, ids[i])
				if errors.Is(err, omdb.ErrQuota) || errors.Is(err, omdb.ErrKey) {
					stop.Store(true)
				}
				out[i] = plotAnswer{id: ids[i], plot: got.Plot, err: err}
			}
		}()
	}
	for i := range ids {
		next <- i
	}
	close(next)
	wg.Wait()
	return out
}

// saveOMDbSynopses writes a round's answers and takes those titles out
// of the queue.
func (s *Store) saveOMDbSynopses(ctx context.Context, ids []string, plots []*string) error {
	if len(ids) == 0 {
		return nil
	}
	if err := s.writeOMDbSynopses(ctx, ids, plots); err != nil {
		return err
	}
	if _, err := s.pool.Exec(ctx, `DELETE FROM meta.synopsis_queue WHERE tconst = ANY($1)`, ids); err != nil {
		return fmt.Errorf("catalog: synopsis queue: %w", err)
	}
	return nil
}

// refillSynopsisQueue clears what OMDb has answered for, or has left the
// catalog, and queues every well-known film the poster pass has answered
// and OMDb has not been asked about for its plot. Like the TMDb id queue,
// only a title new to the queue or one whose votes have moved is written.
func (s *Store) refillSynopsisQueue(ctx context.Context, minVotes int) error {
	if _, err := s.pool.Exec(ctx, `
		DELETE FROM meta.synopsis_queue q
		WHERE NOT `+omdbUnanswered("q.tconst")+`
		   OR NOT EXISTS (SELECT 1 FROM `+Live+`.titles t WHERE t.tconst = q.tconst)`); err != nil {
		return fmt.Errorf("catalog: clear answered synopsis queue: %w", err)
	}
	err := s.fillQueue(ctx, `
		INSERT INTO meta.synopsis_queue (tconst, votes)
		SELECT t.tconst, coalesce(r.num_votes, 0)
		FROM `+Live+`.titles t
		LEFT JOIN `+Live+`.ratings r USING (tconst)
		WHERE `+synopsisFilm+`
		  AND coalesce(r.num_votes, 0) >= $1
		  AND `+omdbUnanswered("t.tconst")+`
		  AND `+posterPassAnswered("t.tconst")+`
		  AND NOT EXISTS (
		      SELECT 1 FROM meta.synopsis_queue q
		      WHERE q.tconst = t.tconst AND q.votes = coalesce(r.num_votes, 0))
		ON CONFLICT (tconst) DO UPDATE SET votes = EXCLUDED.votes`, minVotes)
	if err != nil {
		return fmt.Errorf("catalog: fill synopsis queue: %w", err)
	}
	return nil
}

// synopsesOutstanding is how many titles this pass has to ask about: the
// same sets synopsesWanted hands out, counted once at the start.
func (s *Store) synopsesOutstanding(ctx context.Context, minVotes int) (int64, error) {
	var n int64
	err := s.pool.QueryRow(ctx, `
		SELECT
		    (SELECT count(*)
		     FROM meta.synopsis_queue q
		     WHERE (q.wanted_at IS NOT NULL OR q.votes >= $1)
		       AND `+omdbUnanswered("q.tconst")+`
		       AND `+posterPassAnswered("q.tconst")+`)
		  + (SELECT count(*)
		     FROM meta.synopses
		     WHERE source = 'tmdb' AND fetched_at < now() - make_interval(days => $2))`,
		minVotes, tmdbRefreshDays).Scan(&n)
	if err != nil {
		return 0, fmt.Errorf("catalog: count titles wanting a synopsis: %w", err)
	}
	return n, nil
}

// synopsesWanted is the head of the next titles to ask OMDb about. A
// lookup that failed is not written down, so it is still here: the job
// leaves out what it has already tried this pass.
//
// The queue first: what a reader has met, newest first, then the best
// known, as far as the poster pass has answered them. Only once that is
// empty, the TMDb overviews past their 150 days, oldest first. Answering
// one of those replaces it with OMDb's plot, or, when OMDb has none,
// drops TMDb's text for OMDb's "none".
func (s *Store) synopsesWanted(ctx context.Context, limit, minVotes int) ([]string, error) {
	ids, err := s.tconsts(ctx, `
		SELECT q.tconst
		FROM meta.synopsis_queue q
		WHERE (q.wanted_at IS NOT NULL OR q.votes >= $2)
		  AND `+omdbUnanswered("q.tconst")+`
		  AND `+posterPassAnswered("q.tconst")+`
		  AND EXISTS (SELECT 1 FROM `+Live+`.titles t WHERE t.tconst = q.tconst)
		ORDER BY q.wanted_at DESC NULLS LAST, q.votes DESC, q.tconst
		LIMIT $1`, limit, minVotes)
	if err != nil || len(ids) > 0 {
		return ids, err
	}
	return s.tconsts(ctx, `
		SELECT tconst
		FROM meta.synopses
		WHERE source = 'tmdb'
		  AND fetched_at < now() - make_interval(days => $2)
		ORDER BY fetched_at, tconst
		LIMIT $1`, limit, tmdbRefreshDays)
}

// tconsts runs a query whose one column is a title id.
func (s *Store) tconsts(ctx context.Context, sql string, args ...any) ([]string, error) {
	rows, err := s.pool.Query(ctx, sql, args...)
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

// markSynopsesWanted queues the titles a reader was shown that OMDb has
// not answered for, ahead of everything the sweep queued. A title the
// poster pass has not reached yet is queued all the same, and waits
// there until the pass has answered it: the pass may well keep its plot
// first.
func (s *Store) markSynopsesWanted(ctx context.Context, ids []string) error {
	_, err := s.pool.Exec(ctx, `
		INSERT INTO meta.synopsis_queue (tconst, votes, wanted_at)
		SELECT t.tconst, coalesce(r.num_votes, 0), now()
		FROM `+Live+`.titles t
		LEFT JOIN `+Live+`.ratings r USING (tconst)
		WHERE t.tconst = ANY($1)
		  AND `+omdbUnanswered("t.tconst")+`
		ON CONFLICT (tconst) DO UPDATE SET wanted_at = EXCLUDED.wanted_at`, ids)
	return err
}
