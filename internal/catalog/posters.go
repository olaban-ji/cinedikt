package catalog

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

// Poster is what OMDb knows that the dump does not.
type Poster struct {
	TConst   string
	URL      string
	Released time.Time
	// Plot is OMDb's synopsis, empty when it has none. It rides along on
	// the same answer, so storing it costs no request of its own.
	Plot string
	OK   bool
}

// PosterFiller looks up a title. The importer owns the client, so the
// rate it runs at is set in one place.
type PosterFiller interface {
	Lookup(ctx context.Context, imdbID string) (omdb.Title, error)
}

// PosterJob fills meta.posters. It is the only thing in the catalog that
// calls an API, and nothing a reader does can reach it.
//
// meta is never renamed by the daily swap, so what it learns survives
// every future generation: a title is looked up once, ever, unless the
// lookup itself failed.
type PosterJob struct {
	Store  *Store
	Client PosterFiller
	Logger *slog.Logger
	// Batch is how many ids are claimed per round.
	Batch int

	// Notify hears when a pass starts, how far it has got, and how it
	// ends. Nil leaves that in the log.
	Notify notify.Sink

	// RetryAfter is how long a failed lookup waits before it is asked
	// again. Zero takes PosterRetryAfter.
	RetryAfter time.Duration

	// Workers is how many lookups are in flight at once.
	//
	// Without this the rate limit is not a dial at all: one lookup at a
	// time means throughput is one over the round trip, so setting the
	// limiter to 200/s on a 40ms round trip still gives 25/s. The
	// limiter bounds the rate; this is what lets it be reached.
	Workers int
}

// DefaultPosterBatch is how many titles are taken at a time. Small
// enough that a cancelled job loses almost nothing, large enough that
// the query to find them is not most of the work.
const DefaultPosterBatch = 500

// PosterRetryAfter is how long a lookup that failed waits before it is
// tried again. A day: long enough that a title OMDb keeps failing on is
// not asked about on every twenty-minute pass, short enough that a
// passing fault is repaired by tomorrow.
const PosterRetryAfter = 24 * time.Hour

// DefaultPosterWorkers is how many lookups run at once. Enough that the
// rate limiter is what decides the pace rather than the round trip, and
// few enough that the answers do not outrun the writes behind them.
const DefaultPosterWorkers = 16

// Run fills in what it can before ctx is done.
//
// Order matters more than speed here. Three-quarters of a million
// lookups take days at any polite rate, so the most voted films go
// first: the few thousand anyone actually searches have posters and
// dates within minutes of the first run, and the long tail fills in over
// following nights.
func (j *PosterJob) Run(ctx context.Context, schema string) error {
	// A spent daily quota means every lookup would be told no. Asking
	// anyway would start a pass only to pause it again, every rest
	// interval until the quota comes back.
	if p, ok := j.Client.(interface{ PausedUntil() time.Time }); ok && time.Now().Before(p.PausedUntil()) {
		return nil
	}
	batch := j.Batch
	if batch <= 0 {
		batch = DefaultPosterBatch
	}
	workers := j.Workers
	if workers <= 0 {
		workers = DefaultPosterWorkers
	}
	retryAfter := j.RetryAfter
	if retryAfter <= 0 {
		retryAfter = PosterRetryAfter
	}
	// A failed lookup is written as `missing` and retried once
	// RetryAfter has passed, never twice in the same pass: a row that
	// fails during this pass is stamped close to now, which is later
	// than the cutoff, so neither query hands it back until tomorrow.
	started := time.Now()
	cutoff := started.Add(-retryAfter)
	outstanding, err := j.Store.postersOutstanding(ctx, schema, cutoff)
	if stopping(err) {
		return nil
	}
	if err != nil {
		return err
	}
	// found is posters saved; blank is titles OMDb answered for with no
	// poster, which are answers and are never asked about again, but are
	// not posters; failed is lookups that went wrong.
	var found, blank, failed atomic.Int64
	// Set when the key is spent, or refused. There is no point spending
	// the rest of the budget being told no.
	var spent, keyBad atomic.Bool
	// The last lookup error, for the report when every lookup fails.
	// Boxed, because atomic.Value refuses a second concrete type and
	// errors come in many.
	var lastErr atomic.Pointer[error]
	last := func() error {
		if p := lastErr.Load(); p != nil {
			return *p
		}
		return nil
	}
	track := newProgress(j.Logger, "filling in posters", outstanding)
	run := pass{sink: j.Notify, job: notify.JobPosters}
	if outstanding > 0 {
		track.watch(j.Notify, notify.Event{Job: notify.JobPosters})
	}

	for {
		if keyBad.Load() {
			return &KeyError{Provider: "OMDb", Err: last()}
		}
		if ctx.Err() != nil || spent.Load() {
			j.Logger.Info("poster backfill paused",
				"filled", found.Load(), "none", blank.Load(), "failed", failed.Load(), "quota", spent.Load())
			if spent.Load() {
				until := time.Now().Add(omdb.QuotaPause)
				if p, ok := j.Client.(interface{ PausedUntil() time.Time }); ok && p.PausedUntil().After(time.Now()) {
					until = p.PausedUntil()
				}
				run.pause(notify.DailyLimit, found.Load(), failed.Load(), until)
			}
			return nil
		}
		ids, err := j.Store.postersWanted(ctx, schema, batch, cutoff)
		if stopping(err) {
			return nil
		}
		if err != nil {
			return err
		}
		if len(ids) == 0 {
			answered := found.Load() + blank.Load()
			track.done(answered + failed.Load())
			j.Logger.Info("poster backfill caught up",
				"filled", found.Load(), "none", blank.Load(), "failed", failed.Load())
			// Every lookup failing, with not one answer among them, is
			// not a bad batch of titles. It is OMDb, or the way there.
			// Those titles are held back for retryAfter, so that is when
			// the job next finds out whether OMDb is back.
			if failed.Load() >= failedLookups && answered == 0 {
				return &LookupsFailedError{Provider: "OMDb", Count: failed.Load(), Last: last(),
					RetryAt: time.Now().Add(retryAfter)}
			}
			run.finish(found.Load(), blank.Load(), failed.Load())
			return nil
		}
		run.start(outstanding)

		// Lookups fan out; their answers fan back in to one writer that
		// puts them away in batches. A write per lookup would make the
		// connection pool the ceiling long before the rate limit was.
		queue := make(chan string)
		answers := make(chan Poster, workers*2)
		var wg sync.WaitGroup
		for i := 0; i < workers; i++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				for id := range queue {
					if ctx.Err() != nil || spent.Load() || keyBad.Load() {
						continue // drain, so the sender is never blocked
					}
					got, err := j.Client.Lookup(ctx, id)
					switch {
					case errors.Is(err, omdb.ErrQuota):
						spent.Store(true)
					case errors.Is(err, omdb.ErrKey):
						lastErr.Store(&err)
						keyBad.Store(true)
					case errors.Is(err, context.Canceled), errors.Is(err, context.DeadlineExceeded):
					case errors.Is(err, omdb.ErrNotFound):
						// OMDb answered and has nothing. That is an
						// answer, and it is stored so the title is
						// never asked about again.
						answers <- Poster{TConst: id, OK: true}
					case errors.Is(err, omdb.ErrUnreadable):
						// OMDb answered in a way that cannot be read
						// even once repaired, and will answer the same
						// way tomorrow. It is stored as OMDb having no
						// picture, so the TMDb stand-in takes the title
						// up, rather than as a failure retried daily.
						j.Logger.Info("OMDb's answer is unreadable; no poster from it", "tconst", id, "err", err)
						answers <- Poster{TConst: id, OK: true}
					case err != nil:
						// The lookup failed rather than came back
						// empty, so it is worth asking again tomorrow.
						j.Logger.Warn("poster lookup failed", "tconst", id, "err", err)
						lastErr.Store(&err)
						answers <- Poster{TConst: id}
					default:
						answers <- Poster{TConst: id, URL: got.Poster, Released: got.Released, Plot: got.Plot, OK: true}
					}
				}
			}()
		}

		written := make(chan error, 1)
		go func() {
			written <- j.Store.savePosters(ctx, schema, answers, func(withPoster, without, bad int64) {
				found.Add(withPoster)
				blank.Add(without)
				failed.Add(bad)
				track.step(found.Load() + blank.Load() + failed.Load())
			})
		}()

		for _, id := range ids {
			queue <- id
		}
		close(queue)
		wg.Wait()
		close(answers)
		if err := <-written; err != nil && !stopping(err) {
			return err
		}
	}
}

// stopping reports whether an error is only this run being told to
// stop. A cancelled budget is how the backfill ends — the daily job
// gives it a slice of time and takes it back — so it is not a failure
// to report, and the caller logging it as one says the import broke
// when nothing did.
//
// Every query on this path runs under the same context, so any of them
// can be the one that notices. The loop and the workers already treat
// it this way; this is the rest of them agreeing.
func stopping(err error) bool {
	return errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded)
}

// postersWanted is the next titles to look up: the ones never asked
// about, and the ones whose lookup failed before cutoff, most voted
// first.
func (s *Store) postersWanted(ctx context.Context, schema string, limit int, cutoff time.Time) ([]string, error) {
	rows, err := s.pool.Query(ctx, fmt.Sprintf(`
		SELECT t.tconst
		FROM %s.titles t
		LEFT JOIN %s.ratings r USING (tconst)
		LEFT JOIN meta.posters p USING (tconst)
		WHERE NOT t.is_adult
		  AND (p.tconst IS NULL OR (p.status = 'missing' AND p.fetched_at < $2))
		ORDER BY coalesce(r.num_votes, 0) DESC, t.tconst
		LIMIT $1`, schema, schema), limit, cutoff)
	if err != nil {
		return nil, fmt.Errorf("catalog: find titles wanting a poster: %w", err)
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

// postersOutstanding is how many titles this run still has to ask about.
// It is the same set postersWanted hands out, counted once at the start
// so the log can say how far through that set the run is.
func (s *Store) postersOutstanding(ctx context.Context, schema string, cutoff time.Time) (int64, error) {
	var n int64
	err := s.pool.QueryRow(ctx, fmt.Sprintf(`
		SELECT count(*)
		FROM %s.titles t
		LEFT JOIN meta.posters p USING (tconst)
		WHERE NOT t.is_adult
		  AND (p.tconst IS NULL OR (p.status = 'missing' AND p.fetched_at < $1))`, schema), cutoff).Scan(&n)
	if err != nil {
		return 0, fmt.Errorf("catalog: count titles wanting a poster: %w", err)
	}
	return n, nil
}

// PosterWriteBatch is how many answers go into one statement. Large
// enough that the database is nowhere near the bottleneck, small enough
// that a cancelled run loses almost nothing.
const PosterWriteBatch = 500

// savePosters drains answers and writes them in batches. One statement
// per few hundred titles rather than one per title: at three-quarters of
// a million lookups the round trips would otherwise be most of the work.
//
// progress hears each batch as it is written: answers that came with a
// poster, answers that came without one, and lookups that failed.
func (s *Store) savePosters(ctx context.Context, schema string, answers <-chan Poster, progress func(withPoster, without, bad int64)) error {
	pending := make([]Poster, 0, PosterWriteBatch)
	flush := func() error {
		if len(pending) == 0 {
			return nil
		}
		if err := s.writePosters(ctx, schema, pending); err != nil {
			return err
		}
		var withPoster, without, bad int64
		for _, p := range pending {
			switch {
			case !p.OK:
				bad++
			case p.URL != "":
				withPoster++
			default:
				without++
			}
		}
		progress(withPoster, without, bad)
		pending = pending[:0]
		return nil
	}
	for p := range answers {
		pending = append(pending, p)
		if len(pending) >= PosterWriteBatch {
			if err := flush(); err != nil {
				return err
			}
		}
	}
	return flush()
}

// writePosters puts one batch away in a single statement.
func (s *Store) writePosters(ctx context.Context, schema string, batch []Poster) error {
	ids := make([]string, len(batch))
	urls := make([]*string, len(batch))
	dates := make([]*time.Time, len(batch))
	states := make([]string, len(batch))
	for i, p := range batch {
		ids[i] = p.TConst
		if p.URL != "" {
			url := p.URL
			urls[i] = &url
		}
		if !p.Released.IsZero() {
			when := p.Released
			dates[i] = &when
		}
		states[i] = "missing"
		if p.OK {
			states[i] = "ok"
		}
	}
	// The vote count comes along with the row. A title OMDb has no
	// picture for becomes TMDb's problem at exactly this moment, and
	// the queue it joins is ordered by how well known it is — so the
	// number is taken here, from the generation that is live now,
	// rather than joined at read time out of a schema that is renamed
	// every night.
	_, err := s.pool.Exec(ctx, `
		INSERT INTO meta.posters (tconst, poster_url, released, status, fetched_at, votes)
		SELECT u.tconst, u.url, u.released, u.status, now(), coalesce(r.num_votes, 0)
		FROM unnest($1::text[], $2::text[], $3::date[], $4::text[])
		     AS u(tconst, url, released, status)
		LEFT JOIN `+schema+`.ratings r ON r.tconst = u.tconst
		ON CONFLICT (tconst) DO UPDATE
		SET poster_url    = EXCLUDED.poster_url,
		    released      = EXCLUDED.released,
		    released_tmdb = false,
		    status        = EXCLUDED.status,
		    fetched_at    = EXCLUDED.fetched_at,
		    votes         = EXCLUDED.votes`,
		ids, urls, dates, states)
	if err != nil {
		return fmt.Errorf("catalog: save %d posters: %w", len(batch), err)
	}
	// And the plot each answer carried. Only answers: a lookup that
	// failed says nothing about the synopsis either, and writing it down
	// as "none" would keep the synopsis job from ever asking.
	var answered []string
	var plots []*string
	for _, p := range batch {
		if !p.OK {
			continue
		}
		answered = append(answered, p.TConst)
		plots = append(plots, textOrNull(p.Plot))
	}
	if err := s.writeOMDbSynopses(ctx, answered, plots); err != nil {
		return err
	}
	// One signal per batch. A poster that arrived for a film on the
	// opening screen is a colour waiting to be worked out; five hundred
	// of them are still one pass.
	for _, p := range batch {
		if p.URL != "" {
			s.notify(ctx, NotifyReady)
			break
		}
	}
	return nil
}

func (s *Store) savePoster(ctx context.Context, p Poster) error {
	status := "missing"
	if p.OK {
		status = "ok"
	}
	var released any
	if !p.Released.IsZero() {
		released = p.Released
	}
	var url any
	if p.URL != "" {
		url = p.URL
	}
	_, err := s.pool.Exec(ctx, `
		INSERT INTO meta.posters (tconst, poster_url, released, status, fetched_at)
		VALUES ($1, $2, $3, $4, now())
		ON CONFLICT (tconst) DO UPDATE
		SET poster_url    = EXCLUDED.poster_url,
		    released      = EXCLUDED.released,
		    released_tmdb = false,
		    status        = EXCLUDED.status,
		    fetched_at    = EXCLUDED.fetched_at`,
		p.TConst, url, released, status)
	if err != nil {
		return fmt.Errorf("catalog: save poster %s: %w", p.TConst, err)
	}
	return nil
}

// ForgetUnknownPosters drops rows for titles the live catalog no longer
// holds, so meta does not grow for ever on films IMDb withdrew.
func (s *Store) ForgetUnknownPosters(ctx context.Context) (int64, error) {
	tag, err := s.pool.Exec(ctx, `
		DELETE FROM meta.posters p
		WHERE NOT EXISTS (SELECT 1 FROM `+Live+`.titles t WHERE t.tconst = p.tconst)`)
	if err != nil {
		return 0, fmt.Errorf("catalog: forget posters: %w", err)
	}
	return tag.RowsAffected(), nil
}
