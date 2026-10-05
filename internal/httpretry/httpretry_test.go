package httpretry

import (
	"context"
	"errors"
	"net/http"
	"testing"
	"time"

	"golang.org/x/time/rate"
)

// policy retries at no rate limit, and records the waits it would take
// instead of taking them.
func policy(attempts int, waits *[]time.Duration) Policy {
	return Policy{Name: "test", Attempts: attempts, Limiter: rate.NewLimiter(rate.Inf, 1),
		Sleep: func(_ context.Context, d time.Duration) error {
			*waits = append(*waits, d)
			return nil
		}}
}

// TestAFailureThatWillPassIsTriedAgainWithABackoff: the backoff doubles,
// and a Retry-After takes its place for the attempt it came with.
func TestAFailureThatWillPassIsTriedAgainWithABackoff(t *testing.T) {
	var waits []time.Duration
	calls := 0
	body, err := policy(4, &waits).Do(context.Background(), func() ([]byte, error) {
		calls++
		switch calls {
		case 1, 2:
			return nil, &Retryable{Err: errors.New("down")}
		case 3:
			return nil, &Retryable{Err: errors.New("slow down"), After: 7 * time.Second}
		}
		return []byte("ok"), nil
	})
	if err != nil || string(body) != "ok" {
		t.Fatalf("body %q, err %v", body, err)
	}
	want := []time.Duration{500 * time.Millisecond, time.Second, 7 * time.Second}
	if len(waits) != len(want) {
		t.Fatalf("waits = %v, want %v", waits, want)
	}
	for i := range want {
		if waits[i] != want[i] {
			t.Errorf("waits = %v, want %v", waits, want)
			break
		}
	}
}

// TestTheLastFailureIsTheReasonAndNotWaitedOut: no wait follows the last
// attempt, since nothing comes after it, and the error says how many
// there were and wraps the last failure.
func TestTheLastFailureIsTheReasonAndNotWaitedOut(t *testing.T) {
	var waits []time.Duration
	calls := 0
	last := errors.New("HTTP 503")
	_, err := policy(3, &waits).Do(context.Background(), func() ([]byte, error) {
		calls++
		return nil, &Retryable{Err: last}
	})
	if calls != 3 || len(waits) != 2 {
		t.Errorf("%d calls and %d waits, want 3 and 2", calls, len(waits))
	}
	if !errors.Is(err, last) || err.Error() != "test: giving up after 3 attempts: HTTP 503" {
		t.Errorf("err = %v", err)
	}
}

// TestAnythingElseIsReturnedAtOnce: an answer that is not a fault that
// will pass, such as a refused key or a 404, is not asked again.
func TestAnythingElseIsReturnedAtOnce(t *testing.T) {
	var waits []time.Duration
	calls := 0
	refused := errors.New("refused")
	_, err := policy(4, &waits).Do(context.Background(), func() ([]byte, error) {
		calls++
		return nil, refused
	})
	if err != refused || calls != 1 || len(waits) != 0 {
		t.Errorf("err %v after %d calls and %d waits", err, calls, len(waits))
	}
}

// TestAWaitLongerThanTheDeadlineIsNotTaken: whoever set the deadline has
// gone by the time such a wait ends, so the failure is returned instead.
func TestAWaitLongerThanTheDeadlineIsNotTaken(t *testing.T) {
	var waits []time.Duration
	calls := 0
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	limited := errors.New("HTTP 429")
	_, err := policy(4, &waits).Do(ctx, func() ([]byte, error) {
		calls++
		return nil, &Retryable{Err: limited, After: time.Minute}
	})
	if err != limited || calls != 1 || len(waits) != 0 {
		t.Errorf("err %v after %d calls and %d waits", err, calls, len(waits))
	}
}

// TestACallerWhoHasGoneIsNotRetried: a request that failed because its
// context ended is the caller's doing, and the context's error is the
// answer.
func TestACallerWhoHasGoneIsNotRetried(t *testing.T) {
	var waits []time.Duration
	calls := 0
	ctx, cancel := context.WithCancel(context.Background())
	_, err := policy(4, &waits).Do(ctx, func() ([]byte, error) {
		calls++
		cancel()
		return nil, &Retryable{Err: errors.New("dial: operation was canceled")}
	})
	if !errors.Is(err, context.Canceled) || calls != 1 || len(waits) != 0 {
		t.Errorf("err %v after %d calls and %d waits", err, calls, len(waits))
	}
}

func TestRetryStatusIsARateLimitOrAServerFault(t *testing.T) {
	for code, want := range map[int]bool{200: false, 400: false, 401: false, 404: false, 429: true, 500: true, 502: true, 503: true} {
		if got := RetryStatus(code); got != want {
			t.Errorf("%d: %v, want %v", code, got, want)
		}
	}
}

func TestRetryAfterReadsSecondsOrADate(t *testing.T) {
	now := time.Date(2026, 10, 3, 12, 0, 0, 0, time.UTC)
	if got := ParseRetryAfter("3", now); got != 3*time.Second {
		t.Errorf("3 = %v", got)
	}
	if got := ParseRetryAfter(now.Add(10*time.Second).Format(http.TimeFormat), now); got != 10*time.Second {
		t.Errorf("date = %v", got)
	}
	for _, v := range []string{"", "0", "-1", "soon", now.Add(-time.Minute).Format(http.TimeFormat)} {
		if got := ParseRetryAfter(v, now); got != 0 {
			t.Errorf("%q = %v", v, got)
		}
	}
}
