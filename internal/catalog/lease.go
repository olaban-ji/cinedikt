package catalog

// The right to be the process that runs the background jobs.
//
// The import already takes an advisory lock for the length of one
// attempt, so two publishes cannot collide. The poster jobs took
// nothing: two runners would both call OMDb and TMDb, select the same
// page — nothing claims rows — and do the whole drain twice at twice
// the rate limit.
//
// A session lock is the claim. It lives on one connection and is gone
// the moment that connection is, which is exactly the property wanted:
// a runner that dies stops being the runner, without a stale row
// anywhere to clean up.

import (
	"context"
	"log/slog"
	"time"

	"github.com/jackc/pgx/v5"
)

// LeaseKey is the advisory lock the runner holds for its whole life.
// Deliberately not importLockKey: the import keeps its own short lock
// around the download and the swap, so a one-off `-once` still cannot
// publish beside anything else.
const LeaseKey int64 = 0x6369_6e6a // "cinj"

// LeaseRetry is how often a process that did not get the lease tries
// again.
//
// It must try again. Railway runs the new container beside the old one
// until the health check passes, so at every deploy the new process
// asks while the old one still holds it. Asking once at startup and
// giving up would mean the jobs stop for good at the first deploy —
// quietly, because the only sign is one line in a log nobody reads.
//
// A var rather than a const so a test can watch a handover happen
// without waiting out the real interval.
var LeaseRetry = 30 * time.Second

// Signals the runner listens for. Work is discovered by being told
// about it; the rest intervals are only a backstop for a notification
// that went missing while nobody was connected.
const (
	NotifyPublished = "catalog_published"
	NotifyWanted    = "poster_wanted"
	NotifyReady     = "poster_ready"
)

// Wakes is what a job loop waits on: one channel per loop that reacts
// to a signal, each holding at most one pending wake.
//
// One loop per channel, never two. A send is taken by whichever
// receiver gets there first, so two loops waiting on the same channel
// would share one wake between them: a publish would start one and
// leave the other asleep until its backstop. A second job that wants
// the same signal gets a channel of its own, poked alongside the first.
type Wakes struct {
	// A new generation, for the OMDb poster backfill.
	Published chan struct{}
	// The same signal, for the TMDb id matcher.
	PublishedIDs chan struct{}
	// A reader met a film with no picture, for the TMDb poster
	// fallback.
	Wanted chan struct{}
	// A poster landed, for the opening screen's colours.
	Ready chan struct{}
}

func newWakes() *Wakes {
	return &Wakes{
		// Depth one, and a non-blocking send: a signal that arrives
		// while a pass is running means "go round again when this one
		// returns", not "start a second pass". OMDb saving five
		// hundred posters a second must not start five hundred colour
		// runs.
		Published:    make(chan struct{}, 1),
		PublishedIDs: make(chan struct{}, 1),
		Wanted:       make(chan struct{}, 1),
		Ready:        make(chan struct{}, 1),
	}
}

// signal wakes every loop that a notification on `channel` concerns.
// It is apart from listen so the fan-out can be tested without a
// Postgres connection to carry the notification.
func (w *Wakes) signal(channel string) {
	switch channel {
	case NotifyPublished:
		// A new generation brings new titles, which need TMDb ids and
		// posters, which need colours.
		poke(w.Published)
		poke(w.PublishedIDs)
		poke(w.Wanted)
		poke(w.Ready)
	case NotifyWanted:
		poke(w.Wanted)
	case NotifyReady:
		poke(w.Ready)
	}
}

func poke(c chan struct{}) {
	select {
	case c <- struct{}{}:
	default:
	}
}

// HoldLease runs `work` for as long as this process holds the lease.
//
// It blocks until ctx is done. Each time the lease is taken, `work` is
// started with a context that is cancelled the moment the lease is
// lost — a dropped connection releases the lock in Postgres, and a
// runner that kept working after that would be the second runner this
// lock exists to prevent.
func HoldLease(ctx context.Context, url string, logger *slog.Logger, work func(context.Context, *Wakes)) {
	told := false
	for ctx.Err() == nil {
		held, conn := takeLease(ctx, url, logger, &told)
		if !held {
			select {
			case <-ctx.Done():
			case <-time.After(LeaseRetry):
			}
			continue
		}
		told = false
		logger.Info("running the catalog jobs")

		inner, stop := context.WithCancel(ctx)
		wakes := newWakes()
		done := make(chan struct{})
		go func() {
			defer close(done)
			work(inner, wakes)
		}()

		// The same connection carries the lock and the notifications.
		// When it ends, both end, which is why losing it has to stop
		// the work rather than only the listening.
		err := listen(inner, conn, wakes)
		stop()
		<-done
		_ = conn.Close(context.WithoutCancel(ctx))
		if ctx.Err() == nil {
			logger.Warn("lost the catalog jobs lease", "err", err)
		}
	}
}

// takeLease opens a connection of its own and tries to claim the lock
// on it. The connection is deliberately outside the pool: a lock taken
// on a pooled connection is released when the pool recycles it, and the
// pool would then hand that session to unrelated queries.
func takeLease(ctx context.Context, url string, logger *slog.Logger, told *bool) (bool, *pgx.Conn) {
	conn, err := pgx.Connect(ctx, url)
	if err != nil {
		if !*told {
			logger.Warn("catalog jobs: cannot connect for the lease", "err", err)
			*told = true
		}
		return false, nil
	}
	var got bool
	if err := conn.QueryRow(ctx, `SELECT pg_try_advisory_lock($1)`, LeaseKey).Scan(&got); err != nil || !got {
		_ = conn.Close(context.WithoutCancel(ctx))
		if !*told {
			// Ordinary during a deploy: the container being replaced
			// still holds it. Said once, not every thirty seconds.
			logger.Info("the catalog jobs are running elsewhere; waiting", "retry", LeaseRetry)
			*told = true
		}
		return false, nil
	}
	return true, conn
}

// listen turns notifications into wakes until the connection fails or
// the context ends. Its error is why the lease ended.
func listen(ctx context.Context, conn *pgx.Conn, wakes *Wakes) error {
	for _, channel := range []string{NotifyPublished, NotifyWanted, NotifyReady} {
		if _, err := conn.Exec(ctx, "LISTEN "+channel); err != nil {
			return err
		}
	}
	for {
		note, err := conn.WaitForNotification(ctx)
		if err != nil {
			return err
		}
		wakes.signal(note.Channel)
	}
}

// notify tells whoever is listening that there is work. It is called
// from the pool that did the writing, which is not always the pool the
// runner holds — and may not even be the same process.
func (s *Store) notify(ctx context.Context, channel string) {
	// Bookkeeping, not the work itself: a signal that cannot be sent
	// costs one rest interval, never a row.
	_, _ = s.pool.Exec(ctx, "SELECT pg_notify($1, '')", channel)
}

// waitFor waits for a wake, a backstop timer, or the end.
//
// The timer is no longer how new work is found — it is what covers a
// notification sent while nobody was listening, and rows whose last
// attempt failed.
func waitFor(ctx context.Context, wake <-chan struct{}, backstop time.Duration) bool {
	timer := time.NewTimer(backstop)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-wake:
		return true
	case <-timer.C:
		return true
	}
}
