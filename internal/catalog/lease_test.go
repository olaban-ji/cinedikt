package catalog

import (
	"context"
	"os"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
)

// leaseURL is the test database, or a skip.
func leaseURL(t *testing.T) string {
	t.Helper()
	url := os.Getenv("CATALOG_TEST_URL")
	if url == "" {
		t.Skip("set CATALOG_TEST_URL to run the Postgres tests")
	}
	return url
}

// counted is work that records whether it was ever running twice.
type counted struct {
	now  atomic.Int32
	both atomic.Bool
	runs atomic.Int32
}

func (c *counted) work(ctx context.Context, _ *Wakes) {
	c.runs.Add(1)
	if c.now.Add(1) > 1 {
		c.both.Store(true)
	}
	defer c.now.Add(-1)
	<-ctx.Done()
}

// until waits for a condition, so the tests do not depend on how long
// a connection takes.
func until(t *testing.T, what string, ok func() bool) {
	t.Helper()
	for i := 0; i < 200; i++ {
		if ok() {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for %s", what)
}

// TestOnlyOneRunnerHoldsTheLease is the whole point of the lock. Two
// runners would call OMDb and TMDb twice over, hand each other the same
// page — nothing claims rows — and drain at twice the rate either
// service allows.
func TestOnlyOneRunnerHoldsTheLease(t *testing.T) {
	url := leaseURL(t)
	var c counted

	first, stopFirst := context.WithCancel(context.Background())
	defer stopFirst()
	go HoldLease(first, url, quietLogger(), nil, c.work)
	until(t, "the first runner to start", func() bool { return c.now.Load() == 1 })

	second, stopSecond := context.WithCancel(context.Background())
	defer stopSecond()
	go HoldLease(second, url, quietLogger(), nil, c.work)

	time.Sleep(300 * time.Millisecond)
	if c.both.Load() {
		t.Error("two runners ran at once")
	}
	if got := c.now.Load(); got != 1 {
		t.Errorf("%d runners running, want 1", got)
	}
}

// TestTheLeasePassesOnWhenAHolderStops is the deploy. Railway runs the
// new container beside the old one until the health check passes, so
// the new process always asks while the old one still holds it. A
// runner that asked once at startup and gave up would leave the jobs
// dead from the first deploy onwards — and say so in one log line
// nobody reads.
func TestTheLeasePassesOnWhenAHolderStops(t *testing.T) {
	url := leaseURL(t)
	was := LeaseRetry
	LeaseRetry = 50 * time.Millisecond
	t.Cleanup(func() { LeaseRetry = was })
	var c counted

	leaving, stopLeaving := context.WithCancel(context.Background())
	go HoldLease(leaving, url, quietLogger(), nil, c.work)
	until(t, "the outgoing runner to start", func() bool { return c.now.Load() == 1 })

	arriving, stopArriving := context.WithCancel(context.Background())
	defer stopArriving()
	go HoldLease(arriving, url, quietLogger(), nil, c.work)
	time.Sleep(200 * time.Millisecond)
	if c.runs.Load() != 1 {
		t.Fatalf("the arriving runner started while the lease was held")
	}

	// The old container goes away.
	stopLeaving()
	until(t, "the arriving runner to take over", func() bool { return c.runs.Load() == 2 })
	if c.both.Load() {
		t.Error("the two overlapped")
	}
}

// leaseSink is a sink that counts being detached and being closed.
type leaseSink struct {
	recordingSink
	detached, closed atomic.Int32
}

func (s *leaseSink) Detach()               { s.detached.Add(1) }
func (s *leaseSink) Close(context.Context) { s.closed.Add(1) }

// TestALostLeaseDetachesTheSink: when the lease connection dies under a
// running process, another may take the jobs at once, and with them the
// board. The sink is told, so the old holder stops writing it; shutting
// down is the other way to end, and is a Close.
func TestALostLeaseDetachesTheSink(t *testing.T) {
	url := leaseURL(t)
	var c counted
	var sink leaseSink
	ctx, stop := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		defer close(done)
		HoldLease(ctx, url, quietLogger(), &sink, c.work)
	}()
	until(t, "the runner to start", func() bool { return c.now.Load() == 1 })

	admin, err := pgx.Connect(context.Background(), url)
	if err != nil {
		t.Fatal(err)
	}
	defer admin.Close(context.Background())
	if _, err := admin.Exec(context.Background(), `
		SELECT pg_terminate_backend(pid) FROM pg_locks
		WHERE locktype = 'advisory' AND classid = 0 AND objid = $1 AND granted`, int64(LeaseKey)); err != nil {
		t.Fatal(err)
	}
	until(t, "the sink to be detached", func() bool { return sink.detached.Load() == 1 })
	if sink.closed.Load() != 0 {
		t.Error("a lost lease closed the sink; it is only detached")
	}
	until(t, "the runner to take the lease back", func() bool { return c.runs.Load() == 2 })

	stop()
	<-done
	if sink.closed.Load() != 1 || sink.detached.Load() != 1 {
		t.Errorf("after shutdown: %d closes, %d detaches; want the one close and no more detaches",
			sink.closed.Load(), sink.detached.Load())
	}
}

// TestAPublishWakesEveryLoopThatWaitsForIt is why each loop has a
// channel of its own. A send is taken by exactly one receiver, so the
// OMDb backfill and the TMDb id matcher sharing one would split a
// publish between them, and the one that missed it would sleep out its
// backstop with a new generation's titles waiting.
func TestAPublishWakesEveryLoopThatWaitsForIt(t *testing.T) {
	wakes := newWakes()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Each loop waits between passes on the channel it waits on in the
	// runner, with a backstop far longer than the test, so only the
	// wake can end the wait.
	loops := map[string]chan struct{}{
		"omdb posters":    wakes.Published,
		"tmdb ids":        wakes.PublishedIDs,
		"tmdb posters":    wakes.Wanted,
		"opening colours": wakes.Ready,
	}
	woke := make(chan string, len(loops))
	for name, wake := range loops {
		go func() {
			if waitFor(ctx, wake, time.Hour) {
				woke <- name
			}
		}()
	}

	// One notification, as listen hands it on.
	wakes.signal(NotifyPublished)

	seen := map[string]bool{}
	for range loops {
		select {
		case name := <-woke:
			seen[name] = true
		case <-time.After(2 * time.Second):
			for name := range loops {
				if !seen[name] {
					t.Errorf("one publish did not wake the %s loop", name)
				}
			}
			return
		}
	}
}
