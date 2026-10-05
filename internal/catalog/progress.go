package catalog

import (
	"fmt"
	"io"
	"log/slog"
	"sync/atomic"
	"time"

	"cinedikt/internal/notify"
)

// ProgressEvery is how often a long step says where it has got to. Often
// enough that the importer never looks wedged, rare enough that an hour
// of logs is still readable.
const ProgressEvery = 5 * time.Second

// progress reports how far a step has got, at most once every
// ProgressEvery. Every long phase of an import owns one: a download of
// three-quarters of a gigabyte and a scan of twelve million rows are
// both minutes of silence otherwise, and silence and a hang look alike.
type progress struct {
	logger *slog.Logger
	what   string
	start  time.Time
	last   time.Time
	// total is what `done` is counted against; 0 when it is not known.
	total int64
	// bytes marks a download, whose total is a length rather than a
	// number of rows. A long row count would otherwise look like one.
	bytes bool
	// hear, if set, is told how far the step has got each time the log
	// is. It is how a notifier keeps a status board current without a
	// message per tick. Nil changes nothing, and the log stays the
	// record either way.
	hear func(done int64)
}

func newProgress(logger *slog.Logger, what string, total int64) *progress {
	now := time.Now()
	return &progress{logger: logger, what: what, start: now, last: now, total: total}
}

// newByteProgress is a download's progress: `total` is a content length.
func newByteProgress(logger *slog.Logger, what string, total int64) *progress {
	p := newProgress(logger, what, total)
	p.bytes = true
	return p
}

// watch sends each tick to sink as progress, not as a new
// notification: base says whose progress it is, and the tick fills in
// how far. A nil sink leaves the tracker as it was.
func (p *progress) watch(sink notify.Sink, base notify.Event) {
	if sink == nil {
		return
	}
	p.hear = func(done int64) {
		report(sink, p.event(base, done))
	}
}

// step reports `done` so far, unless it reported recently.
func (p *progress) step(done int64) {
	if time.Since(p.last) < ProgressEvery {
		return
	}
	p.last = time.Now()
	p.logger.Info(p.what, p.fields(done)...)
	if p.hear != nil {
		p.hear(done)
	}
}

// done reports the final figure, whenever it lands. Only to the log:
// the end of a pass is its own event, with the result in it.
func (p *progress) done(done int64) {
	p.logger.Info(p.what+" done", p.fields(done)...)
}

// Enough behind an estimate to trust it: a share this far in, after
// this long. An ETA from the first thirty seconds of a two-hour pass
// swings by an hour a tick.
const (
	etaMinShare   = 0.05
	etaMinElapsed = 2 * time.Minute
)

// event is base with the tick filled in. Share stops at 0.99, because a
// running pass is not finished however close it is.
func (p *progress) event(base notify.Event, done int64) notify.Event {
	e := base
	e.Kind = notify.Progress
	e.Total = p.total
	if p.total <= 0 {
		if p.bytes {
			e.Bytes = done
		}
		return e
	}
	share := float64(done) / float64(p.total)
	e.Share = min(share, 0.99)
	elapsed := time.Since(p.start)
	if share >= etaMinShare && share < 1 && elapsed >= etaMinElapsed {
		left := time.Duration(float64(elapsed) * (1 - share) / share)
		e.ETA = time.Now().Add(left)
	}
	return e
}

func (p *progress) fields(done int64) []any {
	elapsed := time.Since(p.start)
	out := []any{"elapsed", clock(elapsed)}
	if p.total > 0 {
		share := float64(done) / float64(p.total)
		if share > 1 {
			share = 1
		}
		out = append(out, "progress", fmt.Sprintf("%.0f%%", share*100))
		// Only worth guessing once there is enough behind it to guess from.
		if share > 0.02 && share < 1 {
			left := time.Duration(float64(elapsed) * (1 - share) / share)
			out = append(out, "left", clock(left))
		}
	}
	// A download is counted in bytes. Everything else is rows, and once
	// the size of the job is known the row count is the percentage above.
	secs := elapsed.Seconds()
	if p.bytes {
		out = append(out, "read", mib(done)+" of "+mib(p.total))
		if secs > 0 {
			out = append(out, "rate", fmt.Sprintf("%.1fMB/s", float64(done)/(1<<20)/secs))
		}
		return out
	}
	if p.total > 0 {
		if secs > 0 {
			out = append(out, "rate", fmt.Sprintf("%.0f rows/s", float64(done)/secs))
		}
		return out
	}
	out = append(out, "rows", done)
	if secs > 0 {
		out = append(out, "rate", fmt.Sprintf("%.0f rows/s", float64(done)/secs))
	}
	return out
}

// clock is a duration a person can read. slog prints a Duration as its
// nanoseconds, which is how "16 minutes" came out as 995000000000.
func clock(d time.Duration) string {
	d = d.Round(time.Second)
	if d < time.Second {
		return "0s"
	}
	return d.String()
}

func mib(n int64) string { return fmt.Sprintf("%.0fMB", float64(n)/(1<<20)) }

// countingReader passes bytes through and counts them, so a download can
// say how far along it is without the caller reading the body twice.
type countingReader struct {
	r    io.Reader
	n    atomic.Int64
	each func(int64)
}

func (c *countingReader) Read(p []byte) (int, error) {
	n, err := c.r.Read(p)
	if n > 0 {
		c.each(c.n.Add(int64(n)))
	}
	return n, err
}
