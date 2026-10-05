package api

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"

	"cinedikt/internal/catalog"
)

func discardLogger() *slog.Logger { return slog.New(slog.NewTextHandler(io.Discard, nil)) }

// gridCatalog answers the opening screen and search, and answers every
// map with err: catalog.ErrNotFound for a movie it does not hold, or a
// failure of its own.
type gridCatalog struct {
	catalogStub
	err error
}

func (g gridCatalog) Grid(context.Context, string) (*catalog.Grid, error) { return nil, g.err }

func (gridCatalog) FirstRun(context.Context) ([]catalog.Hit, error) {
	return []catalog.Hit{{ID: "tt0133093", Title: "The Matrix"}}, nil
}

// newTestServer serves cat through the API's own router and middleware,
// as a reader reaches it.
func newTestServer(t *testing.T, cat CatalogReader, logger *slog.Logger) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(New(NewCatalogServer(cat, logger), logger).Handler())
	t.Cleanup(srv.Close)
	return srv
}

func do(t *testing.T, method, url string) (int, map[string]any) {
	t.Helper()
	req, err := http.NewRequest(method, url, nil)
	if err != nil {
		t.Fatal(err)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if !strings.HasPrefix(resp.Header.Get("Content-Type"), "application/json") {
		// The mux answers 404s and 405s itself, in plain text.
		return resp.StatusCode, nil
	}
	var body map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatalf("%s %s: decode: %v", method, url, err)
	}
	return resp.StatusCode, body
}

func TestHealthzReportsDependencies(t *testing.T) {
	server := New(NewCatalogServer(nil, discardLogger()), discardLogger())
	server.WithHealth(
		Dependency{Name: "postgres", Ping: func(context.Context) error { return nil }},
		Dependency{Name: "replica", Ping: func(context.Context) error { return errors.New("connection refused") }},
	)
	srv := httptest.NewServer(server.Handler())
	t.Cleanup(srv.Close)

	status, body := do(t, http.MethodGet, srv.URL+"/healthz")
	if status != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want 503 when a dependency is down", status)
	}
	if body["postgres"] != "ok" || body["replica"] != "unreachable" || body["status"] != "degraded" {
		t.Errorf("body = %v", body)
	}
}

func TestHealthzOKWhenEverythingAnswers(t *testing.T) {
	server := New(NewCatalogServer(nil, discardLogger()), discardLogger())
	server.WithHealth(Dependency{Name: "postgres", Ping: func(context.Context) error { return nil }})
	srv := httptest.NewServer(server.Handler())
	t.Cleanup(srv.Close)

	status, body := do(t, http.MethodGet, srv.URL+"/healthz")
	if status != http.StatusOK || body["status"] != "ok" || body["postgres"] != "ok" {
		t.Errorf("status = %d, body = %v", status, body)
	}
}

// TestOpeningFilmsAreTheAPIRoot: the map asks the root for its opening
// screen, and only the root itself answers it.
func TestOpeningFilmsAreTheAPIRoot(t *testing.T) {
	srv := newTestServer(t, gridCatalog{}, discardLogger())

	status, body := do(t, http.MethodGet, srv.URL+"/")
	if status != http.StatusOK {
		t.Fatalf("status = %d, body = %v", status, body)
	}
	if results, _ := body["results"].([]any); len(results) == 0 {
		t.Fatal("root returned no films")
	}
	if status, _ := do(t, http.MethodGet, srv.URL+"/first-run"); status != http.StatusNotFound {
		t.Fatalf("legacy /first-run status = %d, want 404", status)
	}
}

// TestRemovedEndpointsAreGone: addresses an old page or link may still
// ask for are not served at all.
func TestRemovedEndpointsAreGone(t *testing.T) {
	srv := newTestServer(t, gridCatalog{}, discardLogger())
	for _, url := range []string{"/movies/603/pathways", "/movies/603/network", "/movies/603/path/550"} {
		if status, _ := do(t, http.MethodGet, srv.URL+url); status != http.StatusNotFound {
			t.Errorf("GET %s: status = %d, want 404", url, status)
		}
	}
}

// unreadyCatalog has never published a catalog, so any request that
// reaches it is refused with a 503.
type unreadyCatalog struct{ catalogStub }

func (unreadyCatalog) LiveReady(context.Context) (bool, error) { return false, nil }

// TestBadInputIsRefusedBeforeTheCatalogIsAsked: a malformed query or id
// is the reader's mistake, a 400, even while the catalog is still being
// built.
func TestBadInputIsRefusedBeforeTheCatalogIsAsked(t *testing.T) {
	srv := newTestServer(t, unreadyCatalog{}, discardLogger())
	tooMany := strings.TrimSuffix(strings.Repeat("tt0133093,", MaxDetailIDs+1), ",")
	for _, url := range []string{
		"/search/movies",
		"/search/movies?q=a",
		"/search/movies?q=%20a%20",
		"/grid/603",
		"/grid/603/films?ids=tt0133093",
		"/grid/tt0133093/films",
		"/grid/tt0133093/films?ids=tt0133093,603",
		"/grid/tt0133093/films?ids=" + tooMany,
	} {
		if status, body := do(t, http.MethodGet, srv.URL+url); status != http.StatusBadRequest {
			t.Errorf("GET %s: status = %d, body = %v, want 400", url, status, body)
		}
	}
}

func TestNotFoundMapping(t *testing.T) {
	srv := newTestServer(t, gridCatalog{err: catalog.ErrNotFound}, discardLogger())
	if status, _ := do(t, http.MethodGet, srv.URL+"/grid/tt0000404"); status != http.StatusNotFound {
		t.Errorf("missing movie: status = %d, want 404", status)
	}
}

func TestInternalErrorHidesDetail(t *testing.T) {
	failure := errors.New("failed to connect to `user=postgres database=railway`: connection refused")
	srv := newTestServer(t, gridCatalog{err: failure}, discardLogger())
	status, body := do(t, http.MethodGet, srv.URL+"/grid/tt0000500")
	if status != http.StatusInternalServerError {
		t.Errorf("internal failure: status = %d, want 500", status)
	}
	if msg, _ := body["error"].(string); msg == "" || strings.Contains(msg, "postgres") || strings.Contains(msg, "refused") {
		t.Errorf("internal error detail leaked to client: %q", msg)
	}
}

func TestRequestLogIncludesStatusDurationAndSize(t *testing.T) {
	var buf bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&buf, nil))
	srv := newTestServer(t, catalogStub{hits: []catalog.Hit{{ID: "tt0133093", Title: "The Matrix"}}}, logger)

	resp, err := http.Get(srv.URL + "/search/movies?q=matrix")
	if err != nil {
		t.Fatal(err)
	}
	body, _ := io.ReadAll(resp.Body)
	resp.Body.Close()

	line := buf.String()
	for _, want := range []string{
		"msg=request",
		"path=\"/search/movies?q=matrix\"",
		"status=200",
		// Milliseconds, so a collector can compare it.
		"duration_ms=",
		"bytes=" + strconv.Itoa(len(body)),
	} {
		if !strings.Contains(line, want) {
			t.Errorf("log line %q lacks %q", line, want)
		}
	}
}
