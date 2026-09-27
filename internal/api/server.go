// Package api serves the graph over HTTP as node/edge JSON for the frontend.
package api

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"math"
	"net/http"
	"strconv"
	"time"

	"github.com/posthog/posthog-go"
	"golang.org/x/sync/singleflight"

	"cinedikt/internal/analytics"
	"cinedikt/internal/crawl"
	"cinedikt/internal/graph"
	"cinedikt/internal/tmdb"
)

// Reader is the part of the graph store the API queries.
type Reader interface {
	MovieCrawled(ctx context.Context, movieID int) (bool, error)
	GridReady(ctx context.Context, movieID, need int) (bool, error)
	UnexpandedCast(ctx context.Context, movieID int) ([]int, error)
	Pathways(ctx context.Context, movieID, costars, films int, f graph.PathwayFilter) (*graph.Pathways, error)
	// Grid is the spine of a film's map: its people, and where every card
	// goes. The whole spine at once, so no card ever has to move.
	Grid(ctx context.Context, movieID int, q graph.GridQuery) (*graph.GridPayload, error)
	// GridFilms is what the cards the reader can see actually say.
	GridFilms(ctx context.Context, movieID int, ids []int) ([]graph.GridFilm, error)
}

// Dependency is a backing service the health check speaks for.
type Dependency struct {
	Name string
	Ping func(ctx context.Context) error
}

// Expander is the part of the crawler the API drives.
type Expander interface {
	ExpandMovie(ctx context.Context, movieID, depth int) (*crawl.Stats, error)
	ExpandPeople(ctx context.Context, ids []int) error
}

// Searcher finds seed movies by title.
type Searcher interface {
	SearchMovies(ctx context.Context, query string) (*tmdb.SearchResults, error)
}

// Server holds the handlers' dependencies.
type Server struct {
	reader   Reader
	expander Expander
	searcher Searcher
	logger   *slog.Logger

	// seeds collapses concurrent first requests for the same uncrawled
	// movie into one crawl.
	seeds singleflight.Group
	// gate bounds cold crawls across all callers and keeps readers ahead
	// of the warmer.
	gate *crawlGate
	// cold, when set, offers the first-run screen a different eight films
	// each time; without it the client falls back to its built-in set.
	cold *firstRunCache
	// warm, when started, crawls the films a pathways response points at
	// before the client asks for them.
	warm *warmer
	// health are the dependencies /healthz reports on.
	health []Dependency
	// slotWait is how long a request waits for a cold-crawl slot.
	slotWait time.Duration
	// analytics is the public PostHog configuration handed to the map.
	analytics AnalyticsConfig
	// catalog, when set, serves maps out of the IMDb catalog instead of
	// the graph. It answers the same routes and takes the same
	// middleware; what changes is that a request only ever reads.
	catalog *CatalogServer
}

// WithCatalog serves maps from the catalog. The graph, the crawler and
// the TMDb client are then never reached: the routes below go straight
// to Postgres, and nothing a reader does writes anything.
func (s *Server) WithCatalog(c *CatalogServer) { s.catalog = c }

// AnalyticsConfig is what the map needs to report to PostHog and to
// Mixpanel itself. An empty token turns that service off in the page.
type AnalyticsConfig struct {
	Token         string `json:"token"`
	Host          string `json:"host"`
	MixpanelToken string `json:"mixpanel_token,omitempty"`
}

// Limits on query parameters and on-demand crawling.
const (
	// SeedTimeout bounds the depth-1 crawl a cold request triggers. It is
	// short enough that a wedged crawl frees its slot while readers are
	// still waiting, and long enough for a slow film on a cold cache.
	SeedTimeout = 25 * time.Second
	// seedPoll is how often a waiting request looks again. The first
	// screen should leave as soon as one person has been looked at, not
	// when the last extra's career arrives.
	seedPoll = 15 * time.Millisecond
	// HealthTimeout bounds the whole health check.
	HealthTimeout = 3 * time.Second
	// Pathway limits. Costars, films, billing and the vote floor are the
	// pool the map ranks. The client caps what it places and does not
	// restate them: a parameter that is always the same is a default
	// wearing a costume. Ten co-stars is the searched film's fan; eight
	// films is that fan plus the spare the map ranks past when a title is
	// already on the map. A career-wide follow is the one call that asks
	// for more films than this.
	DefaultCostars  = 10
	MaxCostars      = 30
	DefaultFilms    = 8
	MaxFilms        = 20
	DefaultBilling  = 0
	DefaultMinVotes = 200
)

// New builds the server with DefaultLimits. A nil logger uses slog.Default.
func New(reader Reader, expander Expander, searcher Searcher, logger *slog.Logger) *Server {
	return NewWithLimits(reader, expander, searcher, DefaultLimits, logger)
}

// NewWithLimits builds the server with explicit limits.
func NewWithLimits(reader Reader, expander Expander, searcher Searcher, limits Limits, logger *slog.Logger) *Server {
	if logger == nil {
		logger = slog.Default()
	}
	if limits.SlotWait <= 0 {
		limits.SlotWait = DefaultLimits.SlotWait
	}
	return &Server{
		reader:   reader,
		expander: expander,
		searcher: searcher,
		logger:   logger,
		gate:     newCrawlGate(limits.ColdCrawls),
		slotWait: limits.SlotWait,
	}
}

// WithAnalytics sets the public PostHog configuration served to the map.
func (s *Server) WithAnalytics(cfg AnalyticsConfig) *Server {
	s.analytics = cfg
	return s
}

// WithFirstRun lets the cold screen draw its films from the graph. Without
// it the endpoint answers with nothing and the client uses the set it
// ships with, which is the same behaviour as the graph being unreachable.
func (s *Server) WithFirstRun(r FirstRunReader) *Server {
	s.cold = newFirstRunCache(r)
	return s
}

// WarmFirstRun fills the cold screen's candidates before anyone asks, so
// the first visitor does not pay for the scan that fills them.
func (s *Server) WarmFirstRun(ctx context.Context) {
	if s.cold == nil {
		return
	}
	if films := s.cold.films(ctx); len(films) == 0 {
		s.logger.Warn("first-run candidates are empty; the map will offer its built-in set")
	}
}

// firstRunFilms is the cold screen's eight, or nil when it is not set up.
func (s *Server) firstRunFilms(ctx context.Context) []graph.Node {
	if s.cold == nil {
		return nil
	}
	return s.cold.films(ctx)
}

// WithHealth registers the dependencies /healthz reports on. Without it
// the check only says the process is running.
func (s *Server) WithHealth(deps ...Dependency) *Server {
	s.health = append(s.health, deps...)
	return s
}

// StartWarming runs workers that pre-crawl the films handed out by
// /pathways until ctx ends. Without it every hop is crawled on demand.
func (s *Server) StartWarming(ctx context.Context, workers int) {
	s.warm = newWarmer(s.reader, s.expander, s.gate, s.logger)
	s.warm.run(ctx, workers)
}

// Handler returns the routed HTTP handler.
func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", s.healthz)
	mux.HandleFunc("GET /analytics-config", s.analyticsConfig)
	if s.catalog != nil {
		mux.HandleFunc("GET /search/movies", s.catalog.searchMovies)
		mux.HandleFunc("GET /{$}", s.catalog.firstRun)
		mux.HandleFunc("GET /grid/{id}", s.catalog.movieGrid)
		mux.HandleFunc("GET /grid/{id}/films", s.catalog.movieGridFilms)
		mux.HandleFunc("GET /posters/{id}", s.catalog.posterStandIn)
		return s.logRequests(posthog.NewRequestContextMiddleware(mux))
	}
	mux.HandleFunc("GET /search/movies", s.searchMovies)
	// The opening eight. {$} is the path itself, not every unmatched GET:
	// this is the API root the map calls on first paint.
	mux.HandleFunc("GET /{$}", s.firstRun)
	mux.HandleFunc("GET /movies/{id}/pathways", s.moviePathways)
	mux.HandleFunc("GET /grid/{id}", s.movieGrid)
	mux.HandleFunc("GET /grid/{id}/films", s.movieGridFilms)
	return s.logRequests(posthog.NewRequestContextMiddleware(mux))
}

// healthz reports whether this instance can actually serve: a process
// that answers while its database is unreachable only keeps a broken
// machine in the load balancer.
func (s *Server) healthz(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), HealthTimeout)
	defer cancel()

	type result struct {
		name string
		err  error
	}
	results := make(chan result, len(s.health))
	for _, dep := range s.health {
		go func() {
			results <- result{dep.Name, dep.Ping(ctx)}
		}()
	}
	status := map[string]string{"status": "ok"}
	code := http.StatusOK
	for range s.health {
		got := <-results
		if got.err != nil {
			status[got.name] = "unreachable"
			status["status"] = "degraded"
			code = http.StatusServiceUnavailable
			s.logger.Error("health check failed", "dependency", got.name, "err", got.err)
			continue
		}
		status[got.name] = "ok"
	}
	writeJSON(w, code, status)
}

// searchMovies proxies a title search to TMDb so a client can pick a seed.
func (s *Server) searchMovies(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query().Get("q")
	if q == "" {
		writeError(w, http.StatusBadRequest, "q is required")
		return
	}
	res, err := s.searcher.SearchMovies(r.Context(), q)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	type hit struct {
		ID          int    `json:"id"`
		Title       string `json:"title"`
		ReleaseDate string `json:"release_date"`
		// Poster lets the search list show the film rather than describe it.
		Poster string `json:"poster,omitempty"`
	}
	hits := make([]hit, 0, len(res.Results))
	for _, m := range res.Results {
		hits = append(hits, hit{
			ID: m.ID, Title: m.Title, ReleaseDate: m.ReleaseDate,
			Poster: graph.PosterURL(m.PosterPath),
		})
	}
	writeJSON(w, http.StatusOK, map[string]any{"results": hits, "total": res.TotalResults})
	s.capture(r, "movie_search_completed", posthog.NewProperties().
		Set("result_count", len(hits)))
}

// errBusy is returned when every cold-crawl slot is taken.
var errBusy = errors.New("api: too many crawls in progress")

// ensureSeeded starts a depth-1 crawl unless the movie's own cast and
// directors are already written. It returns as soon as that write is
// visible: the rest of the cast keeps expanding behind the answer.
// Concurrent callers share one crawl, detached from any single request.
func (s *Server) ensureSeeded(ctx context.Context, movieID int) error {
	return s.waitReady(ctx, movieID, func(ctx context.Context) (bool, error) {
		return s.reader.MovieCrawled(ctx, movieID)
	})
}

// ensureGridSeeded waits until the movie itself is written. The first
// people are already being fetched; the screen does not wait for them.
func (s *Server) ensureGridSeeded(ctx context.Context, movieID, _ int) error {
	crawled, err := s.reader.MovieCrawled(ctx, movieID)
	if err != nil {
		return err
	}
	if crawled {
		s.finishCast(ctx, movieID)
		return nil
	}
	return s.waitReady(ctx, movieID, func(ctx context.Context) (bool, error) {
		return s.reader.MovieCrawled(ctx, movieID)
	})
}

// beginSeed starts one crawl for movieID, or joins the one already
// running. The crawl holds the gate, not the caller, so the first screen
// can leave while people are still being fetched.
func (s *Server) beginSeed(movieID int) <-chan singleflight.Result {
	return s.seeds.DoChan(strconv.Itoa(movieID), func() (any, error) {
		slotCtx, cancel := context.WithTimeout(context.Background(), s.slotWait)
		defer cancel()
		if err := s.gate.acquire(slotCtx); err != nil {
			return nil, errBusy
		}
		defer s.gate.release()
		crawlCtx, cancel := context.WithTimeout(context.Background(), SeedTimeout)
		defer cancel()
		s.logger.Info("seeding movie on demand", "movie", movieID)
		_, err := s.expander.ExpandMovie(crawlCtx, movieID, 1)
		return nil, err
	})
}

func (s *Server) waitReady(ctx context.Context, movieID int, ready func(context.Context) (bool, error)) error {
	ok, err := ready(ctx)
	if err != nil || ok {
		return err
	}
	ch := s.beginSeed(movieID)
	tick := time.NewTicker(seedPoll)
	defer tick.Stop()
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case res := <-ch:
			return res.Err
		case <-tick.C:
			ok, err := ready(ctx)
			if err != nil || ok {
				return err
			}
		}
	}
}

// finishCast fetches anyone on a written movie who still has no
// filmography, without holding the request that found them.
func (s *Server) finishCast(ctx context.Context, movieID int) {
	ids, err := s.reader.UnexpandedCast(ctx, movieID)
	if err != nil || len(ids) == 0 {
		return
	}
	go func() {
		crawlCtx, cancel := context.WithTimeout(context.Background(), SeedTimeout)
		defer cancel()
		if err := s.expander.ExpandPeople(crawlCtx, ids); err != nil {
			s.logger.Warn("finishing cast", "movie", movieID, "err", err)
		}
	}()
}

// moviePathways is GET /movies/{id}/pathways?person=525:
// the lean expansion of one stop — its lead cast, its director, and each
// person's most voted other films, plus their newest — which is all the
// map needs to grow from it. costars, films, billing and min_votes are
// optional and default to the pool the map ranks; the client does not
// send them on an ordinary hop. person narrows the answer to one career,
// and that call may raise films because a filmography is wider than a
// hop. The movie is crawled first if it never was, and the films returned
// are queued for warming so the reader's next hop is already in the graph.
func (s *Server) moviePathways(w http.ResponseWriter, r *http.Request) {
	id, err := pathInt(r, "id")
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	costars, err := queryInt(r, "costars", DefaultCostars, 1, MaxCostars)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	films, err := queryInt(r, "films", DefaultFilms, 1, MaxFilms)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	billing, err := queryInt(r, "billing", DefaultBilling, 0, 1000)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	minVotes, err := queryInt(r, "min_votes", DefaultMinVotes, 0, 1_000_000)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	person, err := queryInt(r, "person", 0, 0, math.MaxInt32)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if err := s.ensureSeeded(r.Context(), id); err != nil {
		s.fail(w, r, err)
		return
	}
	pw, err := s.reader.Pathways(r.Context(), id, costars, films, graph.PathwayFilter{MaxBilling: billing, MinVotes: minVotes, PersonID: person})
	if err != nil {
		s.fail(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, pw)
	if s.warm != nil {
		// After the response: warming is the next reader's benefit, not
		// this one's cost.
		s.warm.enqueue(nextHop(pw))
	}
	s.capture(r, "movie_pathways_opened", posthog.NewProperties().
		Set("movie_id", id).
		Set("costars", costars).
		Set("films", films).
		Set("person", person).
		Set("billing", billing).
		Set("min_votes", minVotes))
}

// warmFilmsPerCostar is how many of each co-star's films are warmed. The
// map places a co-star's best film not already on it, so the first one or
// two are what will be asked for next.
const warmFilmsPerCostar = 2

// nextHop lists the films a client is likely to expand next, most likely
// first: every co-star's best film, then every co-star's second.
func nextHop(pw *graph.Pathways) []int {
	var ids []int
	for rank := 0; rank < warmFilmsPerCostar; rank++ {
		for _, c := range pw.Cast {
			if rank < len(c.Films) {
				ids = append(ids, c.Films[rank].TMDBID)
			}
		}
	}
	return ids
}

// analyticsConfig is the public PostHog project token and host for the
// map, and the Mixpanel project token. Both tokens are write-only keys,
// the same class of credential the SDKs would otherwise bake in at build
// time. Serving them here is what lets each environment point at its own
// projects through its variables, with one build for all of them.
func (s *Server) analyticsConfig(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, s.analytics)
}

// capture records a successful public action. Distinct IDs come from the
// PostHog request-context middleware (the map sends them as headers); when
// a caller has none, EnqueueWithContext emits a personless event. Outside
// production there is no client, so this is a no-op.
func (s *Server) capture(r *http.Request, event string, properties posthog.Properties) {
	if r == nil {
		return
	}
	client := analytics.Client()
	if client == nil {
		return
	}
	_ = posthog.EnqueueWithContext(r.Context(), client, posthog.Capture{
		Event:      event,
		Properties: properties,
	})
}

// fail maps an error to a status code and logs anything unexpected.
func (s *Server) fail(w http.ResponseWriter, r *http.Request, err error) {
	switch {
	case errors.Is(err, graph.ErrNotFound), errors.Is(err, tmdb.ErrNotFound):
		writeError(w, http.StatusNotFound, err.Error())
	case errors.Is(err, errBusy):
		w.Header().Set("Retry-After", retryAfterSeconds(s.slotWait))
		writeError(w, http.StatusServiceUnavailable, "busy crawling; try again shortly")
	case errors.Is(err, context.Canceled), errors.Is(err, context.DeadlineExceeded):
		// Client went away or ran out of time; nothing useful to send.
	default:
		s.logger.Error("request failed", "method", r.Method, "path", r.URL.Path, "err", err)
		writeError(w, http.StatusInternalServerError, "internal error")
	}
}

// logRequests writes one line per request once it has been served, with
// the status, wall time and response size.
func (s *Server) logRequests(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		rec := &responseRecorder{ResponseWriter: w, status: http.StatusOK}
		next.ServeHTTP(rec, r)
		s.logger.Info("request",
			"method", r.Method,
			"path", r.URL.RequestURI(),
			"status", rec.status,
			// Milliseconds, not a Duration: a Duration prints as "259µs"
			// in text and as raw nanoseconds in JSON, and a collector
			// cannot compare either. A number here is filterable —
			// @duration_ms:>500 — and still readable in a terminal.
			"duration_ms", msSince(start),
			"bytes", rec.bytes,
		)
	})
}

// msSince is elapsed milliseconds, to three decimal places so a
// sub-millisecond answer is still a number rather than a zero.
func msSince(start time.Time) float64 {
	return math.Round(float64(time.Since(start).Microseconds())) / 1000
}

// responseRecorder captures the status code and body size written by a
// handler.
type responseRecorder struct {
	http.ResponseWriter
	status int
	bytes  int
}

func (r *responseRecorder) WriteHeader(status int) {
	r.status = status
	r.ResponseWriter.WriteHeader(status)
}

func (r *responseRecorder) Write(b []byte) (int, error) {
	n, err := r.ResponseWriter.Write(b)
	r.bytes += n
	return n, err
}

func pathInt(r *http.Request, name string) (int, error) {
	v, err := strconv.Atoi(r.PathValue(name))
	if err != nil || v <= 0 {
		return 0, fmt.Errorf("%s must be a positive integer", name)
	}
	return v, nil
}

// queryInt reads an optional integer query parameter within [min, max].
func queryInt(r *http.Request, name string, def, min, max int) (int, error) {
	raw := r.URL.Query().Get(name)
	if raw == "" {
		return def, nil
	}
	v, err := strconv.Atoi(raw)
	if err != nil || v < min || v > max {
		return 0, fmt.Errorf("%s must be an integer in %d..%d", name, min, max)
	}
	return v, nil
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(v); err != nil {
		slog.Default().Warn("write response", "err", err)
	}
}

func writeError(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]string{"error": msg})
}
