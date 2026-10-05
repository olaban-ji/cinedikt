// Package httpretry is the retrying GET the TMDb and Streaming
// Availability clients share: each attempt waits on the client's rate
// limiter, a failure that will pass is tried again after the wait the
// server asked for or a doubling backoff, and anything else is returned
// at once.
package httpretry

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"golang.org/x/time/rate"
)

// Retryable is a failed attempt worth another: no answer at all, a 429
// or a 5xx. After is the wait the server asked for in Retry-After, zero
// when it asked for none.
type Retryable struct {
	Err   error
	After time.Duration
}

func (e *Retryable) Error() string { return e.Err.Error() }
func (e *Retryable) Unwrap() error { return e.Err }

// RetryStatus reports whether a response's status is worth another
// attempt: a rate limit, or the server failing.
func RetryStatus(code int) bool {
	return code == http.StatusTooManyRequests || code >= 500
}

// Policy is how one client retries.
type Policy struct {
	// Name starts the error returned after the last attempt, as the
	// client starts all of its own.
	Name string
	// Attempts is the most tries one request gets, the first included.
	Attempts int
	// Limiter is waited on before every attempt, retries included, so a
	// retry spends the same budget as a first try.
	Limiter *rate.Limiter
	// Sleep waits between attempts. Nil is SleepCtx; a test passes one
	// that returns at once.
	Sleep func(context.Context, time.Duration) error
}

// Do makes up to p.Attempts tries of attempt. A *Retryable failure is
// tried again after the wait the server asked for, or 0.5s, 1s, 2s and
// so on; any other error, and success, return at once. There is no wait
// after the last try, and none that would outlast ctx's deadline:
// whoever asked has gone by the time it ends, so the failure that asked
// for it is returned instead.
func (p Policy) Do(ctx context.Context, attempt func() ([]byte, error)) ([]byte, error) {
	sleep := p.Sleep
	if sleep == nil {
		sleep = SleepCtx
	}
	var last error
	for try := 0; try < p.Attempts; try++ {
		if err := p.Limiter.Wait(ctx); err != nil {
			return nil, err
		}
		body, err := attempt()
		var re *Retryable
		if !errors.As(err, &re) {
			return body, err
		}
		if err := ctx.Err(); err != nil {
			// The request failed because the caller went, which is no
			// fault of the server's and nothing to try again.
			return nil, err
		}
		last = re.Err
		if try == p.Attempts-1 {
			break
		}
		wait := re.After
		if wait <= 0 {
			wait = time.Duration(1<<try) * 500 * time.Millisecond
		}
		if deadline, ok := ctx.Deadline(); ok && time.Until(deadline) < wait {
			return nil, last
		}
		if err := sleep(ctx, wait); err != nil {
			return nil, err
		}
	}
	return nil, fmt.Errorf("%s: giving up after %d attempts: %w", p.Name, p.Attempts, last)
}

// ParseRetryAfter reads either form a Retry-After header takes: a number
// of seconds, or a date. Anything else, or a moment already past, is no
// wait at all.
func ParseRetryAfter(v string, now time.Time) time.Duration {
	v = strings.TrimSpace(v)
	if v == "" {
		return 0
	}
	if secs, err := strconv.Atoi(v); err == nil {
		if secs <= 0 {
			return 0
		}
		return time.Duration(secs) * time.Second
	}
	if at, err := http.ParseTime(v); err == nil && at.After(now) {
		return at.Sub(now)
	}
	return 0
}

// SleepCtx waits d, or until ctx is done, whichever comes first.
func SleepCtx(ctx context.Context, d time.Duration) error {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-t.C:
		return nil
	}
}
