package api

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"cinedikt/internal/catalog"
)

// failingCatalog answers every read with fail(ctx). Readiness passes
// unless notReady is set, so a test can reach either the readiness
// check or the handler's own query.
type failingCatalog struct {
	notReady bool
	fail     func(ctx context.Context) error
}

func (f failingCatalog) Grid(ctx context.Context, _ string) (*catalog.Grid, error) {
	return nil, f.fail(ctx)
}

func (f failingCatalog) Films(ctx context.Context, _ string, _ []string) ([]catalog.Movie, error) {
	return nil, f.fail(ctx)
}

func (f failingCatalog) Search(ctx context.Context, _ string, _ int) ([]catalog.Hit, error) {
	return nil, f.fail(ctx)
}

func (f failingCatalog) ByTMDB(ctx context.Context, _ []int) ([]catalog.Hit, error) {
	return nil, f.fail(ctx)
}

func (f failingCatalog) FirstRun(ctx context.Context, _ int) ([]catalog.Hit, error) {
	return nil, f.fail(ctx)
}

func (f failingCatalog) LiveReady(ctx context.Context) (bool, error) {
	if f.notReady {
		return false, f.fail(ctx)
	}
	return true, nil
}

func (f failingCatalog) Ping(ctx context.Context) error { return f.fail(ctx) }

// errorCount counts the records logged at Error, which are the ones
// that reach PostHog's error tracking.
type errorCount struct {
	mu sync.Mutex
	n  int
}

func (c *errorCount) Enabled(context.Context, slog.Level) bool { return true }
func (c *errorCount) WithAttrs([]slog.Attr) slog.Handler       { return c }
func (c *errorCount) WithGroup(string) slog.Handler            { return c }
func (c *errorCount) Handle(_ context.Context, r slog.Record) error {
	if r.Level >= slog.LevelError {
		c.mu.Lock()
		c.n++
		c.mu.Unlock()
	}
	return nil
}

func TestACancelledRequestIsNotLoggedAsAnError(t *testing.T) {
	routes := []struct {
		name   string
		target string
		serve  func(s *CatalogServer, w http.ResponseWriter, r *http.Request)
	}{
		{"grid", "/grid/tt0133093", (*CatalogServer).movieGrid},
		{"films", "/grid/tt0133093/films?ids=tt0133093", (*CatalogServer).movieGridFilms},
		{"first run", "/", (*CatalogServer).firstRun},
		{"search", "/search?q=matrix", (*CatalogServer).searchMovies},
	}
	cases := []struct {
		name string
		// ctx is the request's context as the handler sees it.
		ctx func() (context.Context, context.CancelFunc)
		// fail is what the catalog answers under that context.
		fail     func(ctx context.Context) error
		wantLogs bool
	}{
		{
			// The page dropped the request: a newer search, or the
			// reader left the opening screen, or closed the tab.
			name: "reader went away",
			ctx: func() (context.Context, context.CancelFunc) {
				ctx, cancel := context.WithCancel(context.Background())
				cancel()
				return ctx, func() {}
			},
			fail:     func(ctx context.Context) error { return ctx.Err() },
			wantLogs: false,
		},
		{
			// withTimeout ran out: the catalog was too slow, which is
			// worth hearing about.
			name: "request timed out",
			ctx: func() (context.Context, context.CancelFunc) {
				return context.WithDeadline(context.Background(), time.Now().Add(-time.Second))
			},
			fail:     func(ctx context.Context) error { return ctx.Err() },
			wantLogs: true,
		},
		{
			name: "catalog failed",
			ctx: func() (context.Context, context.CancelFunc) {
				return context.WithCancel(context.Background())
			},
			fail:     func(context.Context) error { return errors.New("connection refused") },
			wantLogs: true,
		},
	}
	for _, route := range routes {
		for _, c := range cases {
			// Once failing in the readiness check every route makes
			// first, and once in the route's own query.
			for _, notReady := range []bool{true, false} {
				where := "own query"
				if notReady {
					where = "readiness"
				}
				t.Run(route.name+"/"+c.name+"/"+where, func(t *testing.T) {
					counted := &errorCount{}
					s := NewCatalogServer(failingCatalog{notReady: notReady, fail: c.fail}, slog.New(counted))
					ctx, cancel := c.ctx()
					defer cancel()
					req := httptest.NewRequest(http.MethodGet, route.target, nil).WithContext(ctx)
					req.SetPathValue("id", "tt0133093")
					route.serve(s, httptest.NewRecorder(), req)
					if got := counted.n > 0; got != c.wantLogs {
						t.Errorf("logged %d errors, want errors logged = %v", counted.n, c.wantLogs)
					}
				})
			}
		}
	}
}
