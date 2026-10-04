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
	"slices"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"cinedikt/internal/crawl"
	"cinedikt/internal/graph"
	"cinedikt/internal/tmdb"
)

type fakeReader struct {
	mu            sync.Mutex
	crawled       map[int]bool
	crawledChecks int
	castLimit     int
	lastFilmIDs   []int
	gridQuery     graph.GridQuery
	lastFilter    graph.PathwayFilter
}

func (f *fakeReader) filter() graph.PathwayFilter {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.lastFilter
}

func (f *fakeReader) MovieCrawled(_ context.Context, movieID int) (bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.crawledChecks++
	return f.crawled[movieID], nil
}

func (f *fakeReader) GridReady(_ context.Context, movieID, _ int) (bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.crawled[movieID], nil
}

func (f *fakeReader) UnexpandedCast(_ context.Context, _ int) ([]int, error) {
	return nil, nil
}

func (f *fakeReader) isCrawled(movieID int) bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.crawled[movieID]
}

func (f *fakeReader) checks() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.crawledChecks
}

func (f *fakeReader) lastCastLimit() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.castLimit
}

func (f *fakeReader) lastGridQuery() graph.GridQuery {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.gridQuery
}

func (f *fakeReader) Grid(_ context.Context, movieID int, q graph.GridQuery) (*graph.GridPayload, error) {
	f.mu.Lock()
	f.castLimit = q.CastLimit
	f.gridQuery = q
	f.mu.Unlock()
	if movieID == 404 {
		return nil, graph.ErrNotFound
	}
	anchor := graph.GridFilm{ID: movieID, Title: "Anchor", Year: 1999, IsAnchor: true, People: []int{1, 2}}
	return &graph.GridPayload{
		Anchor: anchor,
		People: []graph.GridPerson{
			{ID: 1, Name: "Dee", Role: graph.RoleDirector},
			{ID: 2, Name: "Ex", Role: graph.RoleCast, Character: "Neo", Order: 0},
		},
		Films: []graph.SpineFilm{{ID: movieID, Year: 1999}, {ID: 7, Year: 2003}},
	}, nil
}

func (f *fakeReader) GridFilms(_ context.Context, movieID int, ids []int) ([]graph.GridFilm, error) {
	f.mu.Lock()
	f.lastFilmIDs = append([]int(nil), ids...)
	f.mu.Unlock()
	if movieID == 404 {
		return nil, graph.ErrNotFound
	}
	out := make([]graph.GridFilm, 0, len(ids))
	for _, id := range ids {
		out = append(out, graph.GridFilm{
			ID: id, Title: "Film", Year: 2003, People: []int{2}, IsAnchor: id == movieID,
		})
	}
	return out, nil
}

func (f *fakeReader) filmIDs() []int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.lastFilmIDs
}

func (f *fakeReader) Pathways(_ context.Context, movieID, costars, films int, filter graph.PathwayFilter) (*graph.Pathways, error) {
	f.mu.Lock()
	f.lastFilter = filter
	f.mu.Unlock()
	switch movieID {
	case 404:
		return nil, graph.ErrNotFound
	case 500:
		return nil, errors.New("neo4j down")
	}
	pw := &graph.Pathways{Movie: graph.Node{ID: graph.MovieNodeID(movieID), Type: graph.KindMovie, TMDBID: movieID}}
	for i := range costars {
		p := graph.Pathway{Person: graph.Node{ID: graph.PersonNodeID(100 + i), Type: graph.KindPerson, TMDBID: 100 + i}, Order: i}
		for j := range films {
			p.Films = append(p.Films, graph.PathwayFilm{Node: graph.Node{ID: graph.MovieNodeID(1000 + i*10 + j), Type: graph.KindMovie, TMDBID: 1000 + i*10 + j}})
		}
		pw.Cast = append(pw.Cast, p)
	}
	return pw, nil
}

type fakeExpander struct {
	mu       sync.Mutex
	expanded []string
	// onExpandMovie, if set, runs inside ExpandMovie (to simulate a slow crawl).
	onExpandMovie func()
	reader        *fakeReader
}

func (f *fakeExpander) ExpandMovie(_ context.Context, movieID, depth int) (*crawl.Stats, error) {
	if movieID == 404 {
		return nil, tmdb.ErrNotFound
	}
	if f.onExpandMovie != nil {
		f.onExpandMovie()
	}
	f.mu.Lock()
	f.expanded = append(f.expanded, graph.MovieNodeID(movieID))
	f.mu.Unlock()
	// A real crawl marks the movie crawled in the graph.
	f.reader.mu.Lock()
	f.reader.crawled[movieID] = true
	f.reader.mu.Unlock()
	return &crawl.Stats{}, nil
}

func (f *fakeExpander) ExpandPeople(_ context.Context, _ []int) error {
	return nil
}

func (f *fakeExpander) count() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.expanded)
}

type fakeSearcher struct{}

func (fakeSearcher) SearchMovies(_ context.Context, q string) (*tmdb.SearchResults, error) {
	return &tmdb.SearchResults{
		Results:      []tmdb.Movie{{ID: 603, Title: "The Matrix", ReleaseDate: "1999-03-31", PosterPath: "/m.jpg"}},
		TotalResults: 1,
	}, nil
}

func discardLogger() *slog.Logger { return slog.New(slog.NewTextHandler(io.Discard, nil)) }

// testLimits give up quickly on a crawl slot, so a test that holds one
// does not wait out the production timeout.
func testLimits() Limits {
	return Limits{ColdCrawls: 8, SlotWait: 100 * time.Millisecond}
}

func newTestServerWith(t *testing.T, limits Limits, crawled ...int) (*httptest.Server, *fakeReader, *fakeExpander) {
	t.Helper()
	reader := &fakeReader{crawled: map[int]bool{}}
	for _, id := range crawled {
		reader.crawled[id] = true
	}
	expander := &fakeExpander{reader: reader}
	srv := httptest.NewServer(NewWithLimits(reader, expander, fakeSearcher{}, limits, discardLogger()).Handler())
	t.Cleanup(srv.Close)
	return srv, reader, expander
}

func newTestServer(t *testing.T) (*httptest.Server, *fakeReader, *fakeExpander) {
	t.Helper()
	return newTestServerWith(t, testLimits(), 603)
}

func do(t *testing.T, method, url string) (int, map[string]any) {
	t.Helper()
	status, body, _ := doWithHeaders(t, method, url, nil)
	return status, body
}

func doWithHeaders(t *testing.T, method, url string, headers map[string]string) (int, map[string]any, http.Header) {
	t.Helper()
	req, err := http.NewRequest(method, url, nil)
	if err != nil {
		t.Fatal(err)
	}
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if !strings.HasPrefix(resp.Header.Get("Content-Type"), "application/json") {
		// The mux answers 405s itself, in plain text.
		return resp.StatusCode, nil, resp.Header
	}
	var body map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatalf("%s %s: decode: %v", method, url, err)
	}
	return resp.StatusCode, body, resp.Header
}

func TestPathways(t *testing.T) {
	srv, _, _ := newTestServer(t)
	status, body := do(t, http.MethodGet, srv.URL+"/movies/603/pathways?costars=2&films=3")
	if status != http.StatusOK {
		t.Fatalf("status = %d, body = %v", status, body)
	}
	if movie, _ := body["movie"].(map[string]any); movie["id"] != "m:603" {
		t.Errorf("movie = %v", body["movie"])
	}
	cast := body["cast"].([]any)
	if len(cast) != 2 || len(cast[0].(map[string]any)["films"].([]any)) != 3 {
		t.Errorf("cast = %v", cast)
	}
}

func TestPathwaysUsesMapDefaults(t *testing.T) {
	srv, _, _ := newTestServer(t)
	status, body := do(t, http.MethodGet, srv.URL+"/movies/603/pathways")
	if status != http.StatusOK {
		t.Fatalf("status = %d, body = %v", status, body)
	}
	cast := body["cast"].([]any)
	if len(cast) != DefaultCostars {
		t.Errorf("cast = %d, want default %d", len(cast), DefaultCostars)
	}
	films := cast[0].(map[string]any)["films"].([]any)
	if len(films) != DefaultFilms {
		t.Errorf("films = %d, want default %d", len(films), DefaultFilms)
	}
}

func TestOpeningFilmsAreTheAPIRoot(t *testing.T) {
	reader := &fakeReader{crawled: map[int]bool{}}
	expander := &fakeExpander{reader: reader}
	s := NewWithLimits(reader, expander, fakeSearcher{}, testLimits(), discardLogger())
	s.WithFirstRun(&fakeFirstRun{})
	srv := httptest.NewServer(s.Handler())
	t.Cleanup(srv.Close)

	status, body := do(t, http.MethodGet, srv.URL+"/")
	if status != http.StatusOK {
		t.Fatalf("status = %d, body = %v", status, body)
	}
	results, _ := body["results"].([]any)
	if len(results) == 0 {
		t.Fatal("root returned no films")
	}
	if status, _ := do(t, http.MethodGet, srv.URL+"/first-run"); status != http.StatusNotFound {
		t.Fatalf("legacy /first-run status = %d, want 404", status)
	}
}

func TestPathwaysSeedsUncrawledMovie(t *testing.T) {
	srv, reader, expander := newTestServer(t)
	if status, _ := do(t, http.MethodGet, srv.URL+"/movies/550/pathways"); status != http.StatusOK {
		t.Fatalf("status = %d", status)
	}
	if expander.count() != 1 {
		t.Errorf("expanded = %v, want one crawl", expander.expanded)
	}
	if !reader.isCrawled(550) {
		t.Error("movie not marked crawled after seeding")
	}
	// Second request finds it crawled and does not expand again.
	if status, _ := do(t, http.MethodGet, srv.URL+"/movies/550/pathways"); status != http.StatusOK {
		t.Fatalf("second status = %d", status)
	}
	if expander.count() != 1 {
		t.Errorf("expanded = %v after second request, want no new crawl", expander.expanded)
	}
}

func TestPathwaysDoesNotReseedCrawledMovie(t *testing.T) {
	srv, _, expander := newTestServer(t)
	if status, _ := do(t, http.MethodGet, srv.URL+"/movies/603/pathways"); status != http.StatusOK {
		t.Fatalf("status = %d", status)
	}
	if expander.count() != 0 {
		t.Errorf("expanded = %v, want none for an already crawled movie", expander.expanded)
	}
}

func TestConcurrentColdRequestsShareOneCrawl(t *testing.T) {
	srv, _, expander := newTestServer(t)
	release := make(chan struct{})
	expander.onExpandMovie = func() { <-release }

	const n = 5
	var wg sync.WaitGroup
	statuses := make([]int, n)
	for i := range n {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			statuses[i], _ = do(t, http.MethodGet, srv.URL+"/movies/550/pathways")
		}(i)
	}
	// Let every request reach the singleflight before the crawl completes.
	time.Sleep(50 * time.Millisecond)
	close(release)
	wg.Wait()

	for i, st := range statuses {
		if st != http.StatusOK {
			t.Errorf("request %d: status = %d", i, st)
		}
	}
	if expander.count() != 1 {
		t.Errorf("crawls = %d, want 1 shared across %d requests", expander.count(), n)
	}
}

func TestColdCrawlsAreCappedAndShedLoad(t *testing.T) {
	limits := testLimits()
	limits.ColdCrawls = 1
	srv, _, expander := newTestServerWith(t, limits)
	release := make(chan struct{})
	expander.onExpandMovie = func() { <-release }

	// One crawl takes the only slot and holds it.
	started := make(chan struct{})
	go func() {
		close(started)
		do(t, http.MethodGet, srv.URL+"/movies/550/pathways")
	}()
	<-started
	time.Sleep(50 * time.Millisecond)

	// A different cold movie cannot get a slot and is turned away rather
	// than queueing behind a rate-limited TMDb.
	status, body, header := doWithHeaders(t, http.MethodGet, srv.URL+"/movies/551/pathways", nil)
	if status != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want 503; body %v", status, body)
	}
	if header.Get("Retry-After") == "" {
		t.Error("503 without a Retry-After header")
	}
	close(release)

	// The slot is released, so the next request is served.
	waitFor(t, time.Second, func() bool {
		status, _ := do(t, http.MethodGet, srv.URL+"/movies/552/pathways")
		return status == http.StatusOK
	})
}

func TestHealthzReportsDependencies(t *testing.T) {
	reader := &fakeReader{crawled: map[int]bool{}}
	server := NewWithLimits(reader, &fakeExpander{reader: reader}, fakeSearcher{}, testLimits(), discardLogger())
	server.WithHealth(
		Dependency{Name: "neo4j", Ping: func(context.Context) error { return nil }},
		Dependency{Name: "redis", Ping: func(context.Context) error { return errors.New("connection refused") }},
	)
	srv := httptest.NewServer(server.Handler())
	t.Cleanup(srv.Close)

	status, body := do(t, http.MethodGet, srv.URL+"/healthz")
	if status != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want 503 when a dependency is down", status)
	}
	if body["neo4j"] != "ok" || body["redis"] != "unreachable" || body["status"] != "degraded" {
		t.Errorf("body = %v", body)
	}
}

func TestHealthzOKWhenEverythingAnswers(t *testing.T) {
	reader := &fakeReader{crawled: map[int]bool{}}
	server := NewWithLimits(reader, &fakeExpander{reader: reader}, fakeSearcher{}, testLimits(), discardLogger())
	server.WithHealth(Dependency{Name: "neo4j", Ping: func(context.Context) error { return nil }})
	srv := httptest.NewServer(server.Handler())
	t.Cleanup(srv.Close)

	status, body := do(t, http.MethodGet, srv.URL+"/healthz")
	if status != http.StatusOK || body["status"] != "ok" || body["neo4j"] != "ok" {
		t.Errorf("status = %d, body = %v", status, body)
	}
}

func TestPathwaysWarmsNextHopOffTheRequestPath(t *testing.T) {
	reader := &fakeReader{crawled: map[int]bool{}}
	expander := &fakeExpander{reader: reader}
	server := NewWithLimits(reader, expander, fakeSearcher{}, testLimits(), discardLogger())
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	server.StartWarming(ctx, 2)
	srv := httptest.NewServer(server.Handler())
	t.Cleanup(srv.Close)

	status, body := do(t, http.MethodGet, srv.URL+"/movies/550/pathways?costars=2&films=3")
	if status != http.StatusOK {
		t.Fatalf("status = %d, body = %v", status, body)
	}
	if !reader.isCrawled(550) {
		t.Error("movie not seeded before pathways")
	}

	// Each co-star's first two films get crawled in the background, best
	// films of every co-star first.
	waitFor(t, 2*time.Second, func() bool { return expander.count() == 5 }) // 550 + 2 co-stars × 2 films
	for _, id := range []int{1000, 1010, 1001, 1011} {
		if !reader.isCrawled(id) {
			t.Errorf("film %d not warmed", id)
		}
	}
	if reader.isCrawled(1002) {
		t.Error("third film warmed; only the first two per co-star should be")
	}
}

func TestNextHopOrder(t *testing.T) {
	film := func(id int) graph.PathwayFilm { return graph.PathwayFilm{Node: graph.Node{TMDBID: id}} }
	pw := &graph.Pathways{Cast: []graph.Pathway{
		{Films: []graph.PathwayFilm{film(1), film(2), film(3)}},
		{Films: []graph.PathwayFilm{film(4)}},
		{Films: []graph.PathwayFilm{film(5), film(6)}},
	}}
	got := nextHop(pw)
	want := []int{1, 4, 5, 2, 6}
	if len(got) != len(want) {
		t.Fatalf("nextHop = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("nextHop = %v, want %v", got, want)
		}
	}
}

func TestPathwaysParamValidation(t *testing.T) {
	srv, _, _ := newTestServer(t)
	for _, url := range []string{
		"/movies/abc/pathways",
		"/movies/0/pathways",
		"/movies/603/pathways?costars=0",
		"/movies/603/pathways?costars=999",
		"/movies/603/pathways?films=abc",
		"/movies/603/pathways?billing=-1",
		"/movies/603/pathways?min_votes=x",
	} {
		if status, _ := do(t, http.MethodGet, srv.URL+url); status != http.StatusBadRequest {
			t.Errorf("GET %s: status = %d, want 400", url, status)
		}
	}
}

func TestNotFoundMapping(t *testing.T) {
	srv, _, _ := newTestServerWith(t, testLimits(), 404)
	if status, _ := do(t, http.MethodGet, srv.URL+"/movies/404/pathways"); status != http.StatusNotFound {
		t.Errorf("missing movie: status = %d, want 404", status)
	}
}

func TestInternalErrorHidesDetail(t *testing.T) {
	srv, _, _ := newTestServerWith(t, testLimits(), 500)
	status, body := do(t, http.MethodGet, srv.URL+"/movies/500/pathways")
	if status != http.StatusInternalServerError {
		t.Errorf("internal failure: status = %d, want 500", status)
	}
	if msg, _ := body["error"].(string); strings.Contains(msg, "neo4j") {
		t.Errorf("internal error detail leaked to client: %q", msg)
	}
}

func TestRequestLogIncludesStatusDurationAndSize(t *testing.T) {
	var buf bytes.Buffer
	reader := &fakeReader{crawled: map[int]bool{603: true}}
	logger := slog.New(slog.NewTextHandler(&buf, nil))
	srv := httptest.NewServer(NewWithLimits(reader, &fakeExpander{reader: reader}, fakeSearcher{}, testLimits(), logger).Handler())
	t.Cleanup(srv.Close)

	resp, err := http.Get(srv.URL + "/movies/603/pathways")
	if err != nil {
		t.Fatal(err)
	}
	body, _ := io.ReadAll(resp.Body)
	resp.Body.Close()

	line := buf.String()
	for _, want := range []string{
		"msg=request",
		"path=/movies/603/pathways",
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

func TestSearch(t *testing.T) {
	srv, _, _ := newTestServer(t)
	if status, _ := do(t, http.MethodGet, srv.URL+"/search/movies"); status != http.StatusBadRequest {
		t.Errorf("missing q: status = %d, want 400", status)
	}
	status, body := do(t, http.MethodGet, srv.URL+"/search/movies?q=matrix")
	if status != http.StatusOK {
		t.Fatalf("status = %d", status)
	}
	results := body["results"].([]any)
	if len(results) != 1 {
		t.Fatalf("results = %v", results)
	}
	hit := results[0].(map[string]any)
	if hit["title"] != "The Matrix" {
		t.Errorf("results = %v", results)
	}
	if poster, _ := hit["poster"].(string); !strings.HasPrefix(poster, graph.PosterBaseURL) {
		t.Errorf("search hit has no poster url: %v", hit)
	}
}

func TestRemovedEndpointsAreGone(t *testing.T) {
	srv, _, _ := newTestServer(t)
	for _, url := range []string{"/movies/603/network", "/movies/603/path/550"} {
		if status, _ := do(t, http.MethodGet, srv.URL+url); status != http.StatusNotFound {
			t.Errorf("GET %s: status = %d, want 404 — the map does not use it and it can hammer Neo4j", url, status)
		}
	}
}

func waitFor(t *testing.T, limit time.Duration, done func() bool) {
	t.Helper()
	deadline := time.Now().Add(limit)
	for time.Now().Before(deadline) {
		if done() {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("condition not met within %s", limit)
}

// The map asks for the same billing and vote floor on every request, so
// the API holds them rather than making the client restate them.
func TestPathwaysDefaultsThePoolTheMapWants(t *testing.T) {
	srv, reader, _ := newTestServer(t)
	if status, _ := do(t, http.MethodGet, srv.URL+"/movies/603/pathways"); status != http.StatusOK {
		t.Fatalf("status = %d", status)
	}
	want := graph.PathwayFilter{MaxBilling: DefaultBilling, MinVotes: DefaultMinVotes}
	if got := reader.filter(); got != want {
		t.Errorf("filter with no query = %+v, want %+v", got, want)
	}

	// Stated explicitly, they still win: the default is a default.
	if status, _ := do(t, http.MethodGet, srv.URL+"/movies/603/pathways?billing=3&min_votes=0&person=525"); status != http.StatusOK {
		t.Fatalf("status = %d", status)
	}
	want = graph.PathwayFilter{MaxBilling: 3, MinVotes: 0, PersonID: 525}
	if got := reader.filter(); got != want {
		t.Errorf("filter with a query = %+v, want %+v", got, want)
	}
}

func TestGrid(t *testing.T) {
	srv, _, _ := newTestServer(t)
	status, body := do(t, http.MethodGet, srv.URL+"/grid/603")
	if status != http.StatusOK {
		t.Fatalf("status = %d, body = %v", status, body)
	}
	anchor, _ := body["anchor"].(map[string]any)
	if anchor["isAnchor"] != true || anchor["id"].(float64) != 603 {
		t.Errorf("anchor = %v", body["anchor"])
	}
	if people, _ := body["people"].([]any); len(people) != 2 {
		t.Errorf("people = %v", body["people"])
	}
	// The searched film is one of the cards, and the spine gives each of
	// them as [id, year, rating].
	films, _ := body["films"].([]any)
	if len(films) != 2 {
		t.Fatalf("films = %v", films)
	}
	first, _ := films[0].([]any)
	if len(first) != 4 || first[0].(float64) != 603 {
		t.Errorf("first film = %v, want the searched one as a tuple", films[0])
	}
}

// The whole cast is the point, so no limit is the default and 0 asks for
// it explicitly.
func TestGridTakesTheWholeCastByDefault(t *testing.T) {
	srv, reader, _ := newTestServer(t)
	if status, _ := do(t, http.MethodGet, srv.URL+"/grid/603"); status != http.StatusOK {
		t.Fatal("default request failed")
	}
	if got := reader.lastCastLimit(); got != graph.AllCast {
		t.Errorf("cast limit with no query = %d, want AllCast", got)
	}
	if status, _ := do(t, http.MethodGet, srv.URL+"/grid/603?cast=0"); status != http.StatusOK {
		t.Error("cast=0 should mean everyone, not a bad request")
	}
	if status, _ := do(t, http.MethodGet, srv.URL+"/grid/603?cast=3"); status != http.StatusOK {
		t.Fatal("cast=3 failed")
	}
	if got := reader.lastCastLimit(); got != 3 {
		t.Errorf("cast limit = %d, want 3", got)
	}
}

func TestGridRejectsBadInput(t *testing.T) {
	srv, _, _ := newTestServer(t)
	for _, path := range []string{"/grid/abc", "/grid/603?cast=-1", "/grid/603?cast=x"} {
		if status, _ := do(t, http.MethodGet, srv.URL+path); status != http.StatusBadRequest {
			t.Errorf("%s: status = %d, want 400", path, status)
		}
	}
	if status, _ := do(t, http.MethodGet, srv.URL+"/grid/404"); status != http.StatusNotFound {
		t.Errorf("unknown movie: want 404")
	}
}

// The spine is the whole grid: every card's place, in one answer, so a
// later request can never move one.
func TestGridReturnsTheWholeSpine(t *testing.T) {
	srv, reader, _ := newTestServer(t)
	status, body := do(t, http.MethodGet, srv.URL+"/grid/603")
	if status != http.StatusOK {
		t.Fatalf("status = %d", status)
	}
	if got := reader.lastGridQuery(); got.CastLimit != graph.AllCast || got.HideUnrated {
		t.Errorf("query = %+v, want the whole cast and every film", got)
	}
	// Films travel as [id, year, rating], not as objects.
	films, _ := body["films"].([]any)
	if len(films) == 0 {
		t.Fatal("no spine")
	}
	first, _ := films[0].([]any)
	if len(first) != 4 {
		t.Errorf("spine film = %v, want [id, year, rating, monthDay]", films[0])
	}
	// Nothing about paging survives.
	if _, ok := body["moreAfter"]; ok {
		t.Error("the spine is complete; there is no next page to flag")
	}
	if status, _ := do(t, http.MethodGet, srv.URL+"/grid/603?unrated=0"); status != http.StatusOK {
		t.Fatal("unrated=0 failed")
	}
	if !reader.lastGridQuery().HideUnrated {
		t.Error("unrated=0 should take the column off the grid")
	}
}

// The cards in front of the reader are asked for by id, because the
// spine already said which ids those are.
func TestGridFilmsAnswersByID(t *testing.T) {
	srv, reader, _ := newTestServer(t)
	status, body := do(t, http.MethodGet, srv.URL+"/grid/603/films?ids=7,9,11")
	if status != http.StatusOK {
		t.Fatalf("status = %d, body = %v", status, body)
	}
	if got := reader.filmIDs(); !slices.Equal(got, []int{7, 9, 11}) {
		t.Errorf("ids = %v, want the three asked for", got)
	}
	films, _ := body["films"].([]any)
	if len(films) != 3 {
		t.Fatalf("films = %v", films)
	}
	first, _ := films[0].(map[string]any)
	if first["title"] != "Film" || first["id"].(float64) != 7 {
		t.Errorf("film = %v", first)
	}
}

func TestGridFilmsRejectsBadInput(t *testing.T) {
	srv, _, _ := newTestServer(t)
	many := make([]string, graph.MaxGridFilms+1)
	for i := range many {
		many[i] = "1"
	}
	for _, path := range []string{
		"/grid/603/films",
		"/grid/603/films?ids=",
		"/grid/603/films?ids=abc",
		"/grid/603/films?ids=7,0",
		"/grid/603/films?ids=" + strings.Join(many, ","),
	} {
		if status, _ := do(t, http.MethodGet, srv.URL+path); status != http.StatusBadRequest {
			t.Errorf("%s: status = %d, want 400", path, status)
		}
	}
	if status, _ := do(t, http.MethodGet, srv.URL+"/grid/404/films?ids=7"); status != http.StatusNotFound {
		t.Error("unknown movie: want 404")
	}
}

// The first screen leaves as soon as the movie is written. The rest of
// the cast keeps arriving behind that answer.
func TestGridAnswersBeforeTheCastIsFinished(t *testing.T) {
	srv, reader, expander := newTestServerWith(t, testLimits())
	release := make(chan struct{})
	expander.onExpandMovie = func() {
		reader.mu.Lock()
		reader.crawled[950] = true
		reader.mu.Unlock()
		<-release
	}
	start := time.Now()
	status, _ := do(t, http.MethodGet, srv.URL+"/grid/950?limit=8")
	elapsed := time.Since(start)
	close(release)
	if status != http.StatusOK {
		t.Fatalf("status = %d", status)
	}
	if elapsed > 300*time.Millisecond {
		t.Fatalf("first screen waited %s for the rest of the cast", elapsed)
	}
}
