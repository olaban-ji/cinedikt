package catalog

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"cinedikt/internal/notify"
)

// unreachable is an address nothing listens on: every connection to it
// is refused at once, which is how a stopped Postgres looks from inside
// the private network.
const unreachable = "postgres://nobody@127.0.0.1:1/none?connect_timeout=2"

// lazyStore is a Store whose pool has never connected. pgxpool opens
// connections on first use, so every query fails the way a real one
// does when Postgres is gone, or when ctx has already ended.
func lazyStore(t *testing.T) *Store {
	t.Helper()
	pool, err := pgxpool.New(context.Background(), unreachable)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)
	return &Store{pool: pool}
}

// TestAStoppedImportSaysNothing is the deploy: the process is told to
// stop in the middle of an import, and the import returns an error
// that only says so. Reporting it would put "Import failed: context
// canceled" in the chat at every deploy that lands during an import.
func TestAStoppedImportSaysNothing(t *testing.T) {
	var sink recordingSink
	r := &Runner{store: lazyStore(t), Logger: quietLogger(), Notify: &sink}
	im, _, _ := r.build()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	r.attempt(ctx, im)
	if got := sink.all(); len(got) != 0 {
		t.Fatalf("a stopped import said %+v, want nothing", got)
	}
}

// TestAFailedImportSaysWhy is the other side: a real failure reaches
// the notifier as a cause, with the next try and the raw text.
func TestAFailedImportSaysWhy(t *testing.T) {
	var sink recordingSink
	r := &Runner{store: lazyStore(t), Logger: quietLogger(), Notify: &sink}
	im, _, _ := r.build()
	before := time.Now()
	r.attempt(context.Background(), im)
	failed := sink.of(notify.Failed)
	if len(failed) != 1 {
		t.Fatalf("events = %+v, want one Failed", sink.all())
	}
	e := failed[0]
	if e.Job != notify.JobImport || e.Cause != notify.DatabaseDown || e.Detail == "" || e.At.IsZero() {
		t.Errorf("failure = %+v, want the import, DatabaseDown, the raw text and a time", e)
	}
	if next := e.NextTry.Sub(before); next < PollInterval-time.Second || next > PollInterval+time.Minute {
		t.Errorf("next try is %v away, want the next hourly check", next)
	}
}

// TestAnUnreachableDatabaseIsSaidOnce is the lease watching its own
// connection: once it has failed for DatabaseAlertAfter, one event, not
// one every thirty seconds.
func TestAnUnreachableDatabaseIsSaidOnce(t *testing.T) {
	retry, after := LeaseRetry, DatabaseAlertAfter
	LeaseRetry, DatabaseAlertAfter = 10*time.Millisecond, 100*time.Millisecond
	t.Cleanup(func() { LeaseRetry, DatabaseAlertAfter = retry, after })

	var sink recordingSink
	ctx, cancel := context.WithTimeout(context.Background(), 600*time.Millisecond)
	defer cancel()
	start := time.Now()
	HoldLease(ctx, unreachable, quietLogger(), &sink, func(context.Context, *Wakes) {
		t.Error("the jobs ran without the lease")
	})
	failed := sink.of(notify.Failed)
	if len(failed) != 1 {
		t.Fatalf("events = %+v, want exactly one Failed", sink.all())
	}
	e := failed[0]
	if e.Job != notify.JobDatabase || e.Cause != notify.DatabaseDown || e.Detail == "" {
		t.Errorf("event = %+v", e)
	}
	if e.Since.Before(start) || e.Since.After(start.Add(50*time.Millisecond)) {
		t.Errorf("since = %v, want the first failed attempt at %v", e.Since, start)
	}
	if e.At.Sub(e.Since) < DatabaseAlertAfter {
		t.Errorf("said after %v, want at least %v", e.At.Sub(e.Since), DatabaseAlertAfter)
	}
}

// TestAnOutageThatLastsIsSaidAgain: the notifier has no clock of its
// own, so while the database stays away the lease tells it again, with
// the same start, every DatabaseRemindEvery.
func TestAnOutageThatLastsIsSaidAgain(t *testing.T) {
	retry, after, every := LeaseRetry, DatabaseAlertAfter, DatabaseRemindEvery
	LeaseRetry, DatabaseAlertAfter, DatabaseRemindEvery = 10*time.Millisecond, 50*time.Millisecond, 150*time.Millisecond
	t.Cleanup(func() { LeaseRetry, DatabaseAlertAfter, DatabaseRemindEvery = retry, after, every })

	var sink recordingSink
	ctx, cancel := context.WithTimeout(context.Background(), 600*time.Millisecond)
	defer cancel()
	HoldLease(ctx, unreachable, quietLogger(), &sink, func(context.Context, *Wakes) {
		t.Error("the jobs ran without the lease")
	})
	failed := sink.of(notify.Failed)
	if len(failed) < 2 || len(failed) > 5 {
		t.Fatalf("%d Failed events over 600 ms, want one about every 150 ms after the first", len(failed))
	}
	for i := 1; i < len(failed); i++ {
		if !failed[i].Since.Equal(failed[0].Since) {
			t.Errorf("event %d says the outage began at %v, want %v", i, failed[i].Since, failed[0].Since)
		}
		if gap := failed[i].At.Sub(failed[i-1].At); gap < DatabaseRemindEvery {
			t.Errorf("events %d and %d were %v apart, want at least %v", i-1, i, gap, DatabaseRemindEvery)
		}
	}
}

// TestAJobThatWasStoppedSaysNothing covers the background loops: a
// run cut short by the context is not a failure, and a clean one is
// the check that clears one.
func TestAJobThatWasStoppedSaysNothing(t *testing.T) {
	var sink recordingSink
	ctx, cancel := context.WithCancel(context.Background())
	reportRun(ctx, &sink, notify.JobPosters, nil, time.Time{}, nil)
	cancel()
	reportRun(ctx, &sink, notify.JobPosters, context.Canceled, time.Time{}, nil)
	got := sink.all()
	if len(got) != 1 || got[0].Kind != notify.Checked || got[0].Job != notify.JobPosters {
		t.Fatalf("events = %+v, want one Checked and nothing for the stopped run", got)
	}
}

// TestAPassThatWentWellCarriesWhatItHasToTell: the Daily's figures ride
// on its Checked event, filled in by the loop's hook; a pass that failed
// sends its failure alone, since a Checked is what clears one, and a
// stopping process sends nothing.
func TestAPassThatWentWellCarriesWhatItHasToTell(t *testing.T) {
	var sink recordingSink
	ctx := context.Background()
	asked := 0
	fill := func(_ context.Context, e *notify.Event) {
		asked++
		e.Daily = &notify.DailyDay{No: 7, Played: 3}
	}
	reportRun(ctx, &sink, notify.JobDaily, nil, time.Time{}, fill)
	reportRun(ctx, &sink, notify.JobDaily, errors.New("catalog: none of 9 candidates can be the daily answer"), time.Now(), fill)
	stopped, stop := context.WithCancel(ctx)
	stop()
	reportRun(stopped, &sink, notify.JobDaily, nil, time.Time{}, fill)
	got := sink.all()
	if len(got) != 2 || got[0].Kind != notify.Checked || got[0].Daily == nil || got[0].Daily.No != 7 ||
		got[1].Kind != notify.Failed || got[1].Daily != nil {
		t.Errorf("events = %+v", got)
	}
	if asked != 1 {
		t.Errorf("the hook was asked %d times, want once", asked)
	}
}

// TestTakingOverNamesTheQueuesJobs: the GeoIP check runs on the queue,
// not here, and the takeover names it with the runner's own jobs, so the
// board shows it on; a job left out reads as off there.
func TestTakingOverNamesTheQueuesJobs(t *testing.T) {
	for _, queued := range [][]string{nil, {notify.JobGeoIP}} {
		var sink recordingSink
		r := &Runner{store: lazyStore(t), Logger: quietLogger(), Notify: &sink, Queued: queued}
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		r.run(ctx, newWakes())
		took := sink.of(notify.TookOver)
		if len(took) != 1 {
			t.Fatalf("queued %v: %d takeovers, want 1", queued, len(took))
		}
		named := false
		for _, id := range took[0].Jobs {
			named = named || id == notify.JobGeoIP
		}
		if named != (len(queued) > 0) {
			t.Errorf("queued %v: takeover named %v", queued, took[0].Jobs)
		}
	}
}
