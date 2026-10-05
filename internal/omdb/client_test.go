package omdb

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func newTestClient(t *testing.T, handler http.HandlerFunc, opts ...Option) *Client {
	t.Helper()
	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)
	return New("k3y", append([]Option{WithBaseURL(srv.URL + "/")}, opts...)...)
}

// TestMalformedIDStaysOurProblem: OMDb's "Incorrect IMDb ID." about an
// id we should never have sent is a bug here, not an answer from OMDb,
// and filing it away as one would hide it.
func TestMalformedIDStaysOurProblem(t *testing.T) {
	c := newTestClient(t, func(w http.ResponseWriter, _ *http.Request) {
		w.Write([]byte(`{"Response":"False","Error":"Incorrect IMDb ID."}`))
	})
	for _, id := range []string{"603", "tt", "nm0000206", "tt12x45"} {
		_, err := c.Lookup(context.Background(), id)
		if err == nil || errors.Is(err, ErrNotFound) {
			t.Errorf("%q gave %v, want an error that is not an answer", id, err)
		}
	}
}

func TestQuotaPausesLookups(t *testing.T) {
	var hits atomic.Int32
	c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		w.WriteHeader(http.StatusUnauthorized)
		w.Write([]byte(`{"Response":"False","Error":"Request limit reached!"}`))
	})
	now := time.Now()
	c.now = func() time.Time { return now }

	for i := 0; i < 3; i++ {
		if _, err := c.Lookup(context.Background(), "tt0133093"); !errors.Is(err, ErrQuota) {
			t.Fatalf("call %d: error = %v, want ErrQuota", i, err)
		}
	}
	if hits.Load() != 1 {
		t.Errorf("server hits = %d, want 1: lookups must pause after a quota error", hits.Load())
	}
	// After the pause the client tries again.
	now = now.Add(QuotaPause + time.Second)
	c.Lookup(context.Background(), "tt0133093")
	if hits.Load() != 2 {
		t.Errorf("server hits = %d after the pause, want 2", hits.Load())
	}
}

// TestATransportErrorDoesNotCarryTheKey is the reason the *url.Error is
// unwrapped: its text is the whole request URL, key included, and that
// text goes to the log and, redacted or not, towards a chat.
func TestATransportErrorDoesNotCarryTheKey(t *testing.T) {
	c := newTestClient(t, func(w http.ResponseWriter, _ *http.Request) {
		hj, ok := w.(http.Hijacker)
		if !ok {
			t.Fatal("no hijacker")
		}
		conn, _, _ := hj.Hijack()
		conn.Close()
	})
	_, err := c.Lookup(context.Background(), "tt0133093")
	if err == nil {
		t.Fatal("a closed connection was not an error")
	}
	for _, leak := range []string{"apikey", "k3y"} {
		if strings.Contains(err.Error(), leak) {
			t.Errorf("error %q contains %q", err, leak)
		}
	}
	if !strings.Contains(err.Error(), "tt0133093") {
		t.Errorf("error %q should still say which lookup failed", err)
	}
}

func TestPausedUntilIsWhenTheQuotaLetsGo(t *testing.T) {
	now := time.Date(2026, 9, 27, 12, 0, 0, 0, time.UTC)
	c := newTestClient(t, func(w http.ResponseWriter, _ *http.Request) {
		w.Write([]byte(`{"Response":"False","Error":"Request limit reached!"}`))
	})
	c.now = func() time.Time { return now }
	if !c.PausedUntil().IsZero() {
		t.Fatal("a fresh client is paused")
	}
	if _, err := c.Lookup(context.Background(), "tt0133093"); !errors.Is(err, ErrQuota) {
		t.Fatalf("err = %v, want ErrQuota", err)
	}
	if got := c.PausedUntil(); !got.Equal(now.Add(QuotaPause)) {
		t.Errorf("PausedUntil = %v, want %v", got, now.Add(QuotaPause))
	}
}
