package catalog

import (
	"errors"
	"fmt"
	"io"
	"net"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgconn"

	"cinedikt/internal/notify"
	"cinedikt/internal/omdb"
	"cinedikt/internal/tmdb"
)

// TestClassifyNamesEveryKnownCause is the table the notifier's words
// hang on. Each error is also wrapped the way the code wraps it, since
// what reaches the runner is never the bare error.
func TestClassifyNamesEveryKnownCause(t *testing.T) {
	refused := &net.OpError{Op: "dial", Net: "tcp", Err: errors.New("connection refused")}
	for _, c := range []struct {
		name      string
		err       error
		cause     notify.Cause
		provider  string
		status    int
		integrity float64
	}{
		{"omdb key", &KeyError{Provider: "OMDb", Err: omdb.ErrKey}, notify.KeyRejected, "OMDb", 0, 0},
		{"tmdb key, wrapped", fmt.Errorf("tmdb posters: %w", &KeyError{Provider: "TMDb", Err: &tmdb.StatusError{Status: 401}}), notify.KeyRejected, "TMDb", 0, 0},
		{"every lookup", &LookupsFailedError{Provider: "OMDb", Count: 60, Last: errors.New("x")}, notify.AllFailed, "OMDb", 0, 0},
		{"imdb unreachable", &IMDbError{Method: "HEAD", File: TitleBasics, Err: refused}, notify.IMDbDown, "IMDb", 0, 0},
		{"imdb 503", &IMDbError{Method: "GET", File: TitlePrincipals, Status: 503}, notify.IMDbDown, "IMDb", 503, 0},
		{"imdb 429", &IMDbError{Method: "HEAD", File: TitleBasics, Status: 429}, notify.IMDbDown, "IMDb", 429, 0},
		{"imdb short body", &IMDbError{Err: fmt.Errorf("title.crew is 1 bytes, expected 2: %w", io.ErrUnexpectedEOF)}, notify.IMDbDown, "IMDb", 0, 0},
		{"imdb 404", &IMDbError{Method: "GET", File: TitleCrew, Status: 404}, notify.Unknown, "IMDb", 404, 0},
		{"no connection", fmt.Errorf("catalog: acquire connection: %w", &pgconn.ConnectError{}), notify.DatabaseDown, "", 0, 0},
		{"connection class", fmt.Errorf("catalog: save: %w", &pgconn.PgError{Code: "08006"}), notify.DatabaseDown, "", 0, 0},
		{"statement timeout", fmt.Errorf("catalog: count: %w", &pgconn.PgError{Code: "57014"}), notify.DatabaseBusy, "", 0, 0},
		{"lock timeout", fmt.Errorf("catalog: drop: %w", &pgconn.PgError{Code: "55P03"}), notify.DatabaseBusy, "", 0, 0},
		{"other sql", fmt.Errorf("catalog: x: %w", &pgconn.PgError{Code: "42P01"}), notify.Unknown, "", 0, 0},
		{"mismatch", &IntegrityError{Share: 0.9731, Want: 0.99}, notify.FilesMismatch, "IMDb", 0, 0.9731},
		{"empty load", &IntegrityError{Want: 0.99, Reason: "no titles loaded"}, notify.FilesMismatch, "IMDb", 0, 0},
		{"anything else", errors.New("catalog: title.basics has no Last-Modified"), notify.Unknown, "", 0, 0},
	} {
		cause, provider, status, integrity := classify(c.err)
		if cause != c.cause || provider != c.provider || status != c.status || integrity != c.integrity {
			t.Errorf("%s: classify = %s %q %d %v, want %s %q %d %v",
				c.name, cause, provider, status, integrity, c.cause, c.provider, c.status, c.integrity)
		}
	}
}

func TestTypedErrorsKeepTheirText(t *testing.T) {
	for _, c := range []struct {
		err  error
		want string
	}{
		{&IMDbError{Method: "GET", File: TitlePrincipals, Status: 503}, "catalog: GET title.principals: HTTP 503"},
		{&IMDbError{Method: "HEAD", File: TitleBasics, Err: errors.New("dial tcp: refused")}, "catalog: HEAD title.basics: dial tcp: refused"},
		{&IMDbError{Err: fmt.Errorf("%s is %d bytes, expected %d", TitleCrew, 10, 20)}, "catalog: title.crew is 10 bytes, expected 20"},
		{&IntegrityError{Want: MinIntegrity, Reason: "no titles loaded"}, "catalog: no titles loaded"},
		{&IntegrityError{Share: 0.9731, Want: MinIntegrity}, "catalog: only 0.9731 of credits name a stored title, want 0.99 (the files are probably from different generations)"},
	} {
		if got := c.err.Error(); got != c.want {
			t.Errorf("Error() = %q, want %q", got, c.want)
		}
	}
}

func TestFailureCarriesTheFactsAndTheRawText(t *testing.T) {
	next := time.Date(2026, 9, 27, 15, 0, 0, 0, time.UTC)
	live := next.Add(-30 * time.Hour)
	err := fmt.Errorf("wrapped: %w", &LookupsFailedError{Provider: "TMDb", Count: 73, Last: errors.New("timeout")})
	e := failure(notify.JobTMDbIDs, err, next, live)
	if e.Kind != notify.Failed || e.Job != notify.JobTMDbIDs || e.Cause != notify.AllFailed || e.Provider != "TMDb" ||
		e.Errors != 73 || !e.NextTry.Equal(next) || !e.LiveSince.Equal(live) || e.Detail != err.Error() {
		t.Errorf("failure = %+v", e)
	}
}

// TestAHeldBackRetryIsTheNextTry is the OMDb backfill: its failed
// lookups wait a day, so the pass twenty minutes on is not when it next
// learns whether OMDb answers, and the event says the day.
func TestAHeldBackRetryIsTheNextTry(t *testing.T) {
	next := time.Date(2026, 9, 27, 15, 0, 0, 0, time.UTC)
	retry := next.Add(24 * time.Hour)
	e := failure(notify.JobPosters, &LookupsFailedError{Provider: "OMDb", Count: 60, RetryAt: retry}, next, time.Time{})
	if !e.NextTry.Equal(retry) {
		t.Errorf("next try = %v, want the held-back retry %v", e.NextTry, retry)
	}
}
