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

	"cinedikt/internal/notify"
)

// LeaseKey is the advisory lock the runner holds for its whole life.
// Deliberately not importLockKey: the import keeps its own short lock
// around the download and the swap, so an import still unwinding in a
// process that has just lost the lease cannot publish beside the one
// the new holder starts.
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

// DatabaseAlertAfter is how long the lease connection has to keep
// failing before the notifier is told the database is unreachable. A
// restart of Postgres, or a blip on the private network, is over well
// inside it; an outage is not. A var so a test need not wait it out.
var DatabaseAlertAfter = 2 * time.Minute

// DatabaseRemindEvery is how often the notifier is told again while the
// database stays out of reach. It has no clock of its own: a reminder a
// day into an outage, and a board whose stamp shows it is still trying,
// both need something to happen. A var so a test need not wait it out.
var DatabaseRemindEvery = 30 * time.Minute

// shutdownFlush is how long a process on its way out keeps the lease
// while the notifier writes its last board and saves what it said, so
// the next holder reads that state rather than the one before it.
const shutdownFlush = 3 * time.Second

// Signals the runner listens for. Work is discovered by being told
// about it; the rest intervals are only a backstop for a notification
// that went missing while nobody was connected.
const (
	NotifyPublished      = "catalog_published"
	NotifyWanted         = "poster_wanted"
	NotifyReady          = "poster_ready"
	NotifySynopsisWanted = "synopsis_wanted"
	NotifyTrailerWanted  = "trailer_wanted"
	NotifyPersonWanted   = "person_wanted"
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
	// A reader met a film OMDb has not answered for, for the synopsis
	// job. A new generation is not news to it: the poster pass keeps
	// the plot of every title it brings.
	Synopses chan struct{}
	// A new generation, for the trailer job, which queues its films.
	Trailers chan struct{}
	// A reader opened a film whose trailer has not been looked up, or
	// whose answer has come due, for the same job. Its own channel, so
	// the job can tell a reader waiting on one title from a publish that
	// brings a catalog's worth, and so a pass already running can hear
	// it between titles.
	TrailersWanted chan struct{}
	// A new generation, for the people job, which queues everyone a map
	// can show.
	People chan struct{}
	// A reader opened a map with people the job has no photo answer for,
	// or one that has come due, for the same job. Its own channel for
	// the reasons TrailersWanted has one.
	PeopleWanted chan struct{}
	// A new generation, for the daily puzzles job: a day it could not
	// pick for may have a fair answer in the new catalog.
	Daily chan struct{}
}

func newWakes() *Wakes {
	return &Wakes{
		// Depth one, and a non-blocking send: a signal that arrives
		// while a pass is running means "go round again when this one
		// returns", not "start a second pass". OMDb saving five
		// hundred posters a second must not start five hundred colour
		// runs.
		Published:      make(chan struct{}, 1),
		PublishedIDs:   make(chan struct{}, 1),
		Wanted:         make(chan struct{}, 1),
		Ready:          make(chan struct{}, 1),
		Synopses:       make(chan struct{}, 1),
		Trailers:       make(chan struct{}, 1),
		TrailersWanted: make(chan struct{}, 1),
		People:         make(chan struct{}, 1),
		PeopleWanted:   make(chan struct{}, 1),
		Daily:          make(chan struct{}, 1),
	}
}

// signal wakes every loop that a notification on `channel` concerns.
// It is apart from listen so the fan-out can be tested without a
// Postgres connection to carry the notification.
func (w *Wakes) signal(channel string) {
	switch channel {
	case NotifyPublished:
		// A new generation brings new titles, which need TMDb ids,
		// posters and trailers, and new people, who need photos.
		// Posters need colours, and synopses come with their posters.
		// And the daily puzzles may find an answer they could not.
		poke(w.Published)
		poke(w.PublishedIDs)
		poke(w.Wanted)
		poke(w.Ready)
		poke(w.Trailers)
		poke(w.People)
		poke(w.Daily)
	case NotifyWanted:
		poke(w.Wanted)
	case NotifyReady:
		poke(w.Ready)
	case NotifySynopsisWanted:
		poke(w.Synopses)
	case NotifyTrailerWanted:
		poke(w.TrailersWanted)
	case NotifyPersonWanted:
		poke(w.PeopleWanted)
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
//
// sink hears about the lease connection itself: once it has failed for
// DatabaseAlertAfter, every DatabaseRemindEvery after that, and again
// when it connects. It is detached when the lease is lost, so a process
// that no longer runs the jobs stops writing the board.
func HoldLease(ctx context.Context, url string, logger *slog.Logger, sink notify.Sink, work func(context.Context, *Wakes)) {
	w := leaseWatch{logger: logger, sink: sink}
	for ctx.Err() == nil {
		held, conn := w.take(ctx, url)
		if !held {
			select {
			case <-ctx.Done():
			case <-time.After(LeaseRetry):
			}
			continue
		}
		w.told = false
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
		if ctx.Err() != nil {
			// Shutting down. The lock is still held, so no other
			// process can read the notifier's state until it has
			// written its last word.
			notify.Close(sink, shutdownFlush)
		} else {
			// Lost, not shutting down. Another process may take the
			// lease within seconds, and the board and what was said
			// are its to write from then on.
			notify.Detach(sink)
		}
		_ = conn.Close(context.WithoutCancel(ctx))
		if ctx.Err() == nil {
			logger.Warn("lost the catalog jobs lease", "err", err)
		}
	}
}

// leaseWatch is what the lease loop remembers between attempts: whether
// it has said it is waiting, how long the database has been out of
// reach, and when the sink was last told so.
type leaseWatch struct {
	logger *slog.Logger
	sink   notify.Sink
	told   bool
	down   time.Time
	said   time.Time
}

// take opens a connection of its own and tries to claim the lock on it.
// The connection is deliberately outside the pool: a lock taken on a
// pooled connection is released when the pool recycles it, and the pool
// would then hand that session to unrelated queries.
func (w *leaseWatch) take(ctx context.Context, url string) (bool, *pgx.Conn) {
	conn, err := pgx.Connect(ctx, url)
	if err == nil {
		var got bool
		if err = conn.QueryRow(ctx, `SELECT pg_try_advisory_lock($1)`, LeaseKey).Scan(&got); err == nil {
			w.reached(got)
			if got {
				return true, conn
			}
			_ = conn.Close(context.WithoutCancel(ctx))
			if !w.told {
				// Ordinary during a deploy: the container being replaced
				// still holds it. Said once, not every thirty seconds.
				w.logger.Info("the catalog jobs are running elsewhere; waiting", "retry", LeaseRetry)
				w.told = true
			}
			return false, nil
		}
		_ = conn.Close(context.WithoutCancel(ctx))
	}
	if ctx.Err() != nil {
		return false, nil
	}
	if !w.told {
		w.logger.Warn("catalog jobs: cannot connect for the lease", "err", err)
		w.told = true
	}
	w.unreachable(err)
	return false, nil
}

// unreachable notes a failed attempt, and tells the sink once the
// database has been out of reach for DatabaseAlertAfter, then again
// every DatabaseRemindEvery. The sink says it once; the repeats are what
// let it remind a day on, and keep the board's stamp moving.
func (w *leaseWatch) unreachable(err error) {
	now := time.Now()
	if w.down.IsZero() {
		w.down = now
	}
	if now.Sub(w.down) < DatabaseAlertAfter {
		return
	}
	if !w.said.IsZero() && now.Sub(w.said) < DatabaseRemindEvery {
		return
	}
	w.said = now
	report(w.sink, notify.Event{Job: notify.JobDatabase, Kind: notify.Failed,
		Cause: notify.DatabaseDown, Since: w.down, Detail: err.Error()})
}

// reached notes a connection that worked. When the lock was taken, the
// runner's TookOver is what tells the sink the database is back; when
// another process holds it, this is.
func (w *leaseWatch) reached(got bool) {
	if !w.said.IsZero() && !got {
		report(w.sink, notify.Event{Job: notify.JobDatabase, Kind: notify.Checked})
	}
	w.down, w.said = time.Time{}, time.Time{}
}

// listen turns notifications into wakes until the connection fails or
// the context ends. Its error is why the lease ended.
func listen(ctx context.Context, conn *pgx.Conn, wakes *Wakes) error {
	for _, channel := range []string{NotifyPublished, NotifyWanted, NotifyReady, NotifySynopsisWanted, NotifyTrailerWanted, NotifyPersonWanted} {
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

// waitForEither is waitFor with two wakes: a new generation and a
// reader's mark, for the jobs that have both. byWake says it was the
// first. A nil channel never fires, so a job with one wake passes nil
// for the other.
func waitForEither(ctx context.Context, wake, wanted <-chan struct{}, backstop time.Duration) (woke, byWake bool) {
	timer := time.NewTimer(backstop)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return false, false
	case <-wake:
		return true, true
	case <-wanted:
		return true, false
	case <-timer.C:
		return true, false
	}
}
