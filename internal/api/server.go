// Package api serves the catalog's maps over HTTP as JSON for the map.
package api

import (
	"context"
	"encoding/json"
	"log/slog"
	"math"
	"net/http"
	"time"
)

// Dependency is a backing service the health check speaks for.
type Dependency struct {
	Name string
	Ping func(ctx context.Context) error
}

// Server is the routed API: the catalog's handlers, Cinedikt Daily, the
// health check, and one log line for every request served.
type Server struct {
	catalog *CatalogServer
	daily   *dailyRoutes
	logger  *slog.Logger
	// health are the dependencies /healthz reports on.
	health []Dependency
}

// HealthTimeout bounds the whole health check.
const HealthTimeout = 3 * time.Second

// New builds the API over the catalog's handlers. A nil logger uses
// slog.Default.
func New(c *CatalogServer, logger *slog.Logger) *Server {
	if logger == nil {
		logger = slog.Default()
	}
	return &Server{catalog: c, daily: newDailyRoutes(logger), logger: logger}
}

// WithHealth registers the dependencies /healthz reports on. Without it
// the check only says the process is running.
func (s *Server) WithHealth(deps ...Dependency) *Server {
	s.health = append(s.health, deps...)
	return s
}

// Handler returns the routed HTTP handler.
func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", s.healthz)
	mux.HandleFunc("GET /search/movies", s.catalog.searchMovies)
	// The opening screen. {$} is the path itself, not every unmatched
	// GET: this is the API root the map calls on first paint.
	mux.HandleFunc("GET /{$}", s.catalog.firstRun)
	mux.HandleFunc("GET /grid/{id}", s.catalog.movieGrid)
	mux.HandleFunc("GET /grid/{id}/films", s.catalog.movieGridFilms)
	mux.HandleFunc("GET /posters/{id}", s.catalog.posterStandIn)
	mux.HandleFunc("GET /trailers/{id}", s.catalog.trailerFor)
	mux.HandleFunc("GET /people/photos", s.catalog.peoplePhotos)
	mux.HandleFunc("GET /where-to-watch/{id}", s.catalog.whereToWatch)
	s.daily.register(mux)
	return s.logRequests(mux)
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
