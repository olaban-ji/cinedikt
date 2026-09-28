// Package tmdb is a small client for The Movie Database API with
// response caching and a token-bucket rate limiter.
package tmdb

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"golang.org/x/time/rate"
)

const defaultBaseURL = "https://api.themoviedb.org/3"

// ErrNotFound is returned when TMDb has no record for the requested id.
var ErrNotFound = errors.New("tmdb: not found")

// Cache stores raw response bodies keyed by request path and query.
type Cache interface {
	Get(key string) ([]byte, bool)
	Set(key string, body []byte) error
}

// noCache is the default when no cache is configured.
type noCache struct{}

func (noCache) Get(string) ([]byte, bool) { return nil, false }
func (noCache) Set(string, []byte) error  { return nil }

// Auth carries TMDb credentials. AccessToken (v4) is preferred when both are set.
type Auth struct {
	APIKey      string
	AccessToken string
}

// Client talks to the TMDb v3 API. It is safe for concurrent use.
type Client struct {
	http    *http.Client
	baseURL string
	auth    Auth
	limiter *rate.Limiter
	cache   Cache
	sleep   func(context.Context, time.Duration) error
}

// Option configures a Client.
type Option func(*Client)

// WithBaseURL points the client at a different server (used by tests).
func WithBaseURL(u string) Option { return func(c *Client) { c.baseURL = strings.TrimRight(u, "/") } }

// WithHTTPClient replaces the default HTTP client.
func WithHTTPClient(h *http.Client) Option { return func(c *Client) { c.http = h } }

// WithCache stores successful responses in cache and serves repeats from it.
func WithCache(cache Cache) Option { return func(c *Client) { c.cache = cache } }

// DefaultRatePerSecond is half of what TMDb takes from one address.
// TMDb counts about 40 requests a second per IP, not per key, so this is
// the budget for every client in a process together: they share one
// limiter (NewLimiter, WithLimiter) rather than each spending a full one.
// The other half is room for a deploy, when the container being replaced
// and its successor both run for a few seconds, and for retries.
const DefaultRatePerSecond = 20

// DefaultBurst is how many requests may go at once after a quiet spell.
// Small and fixed rather than a multiple of the rate, so a process that
// has been quiet sends at most five requests at once, well inside what
// TMDb takes from one address.
const DefaultBurst = 5

// NewLimiter is the one limiter a process hands every TMDb client, so
// the search fallback, the poster stand-in, the trailer lookup and every
// background job draw on the same budget. A rate of zero or less takes
// DefaultRatePerSecond.
func NewLimiter(perSecond float64) *rate.Limiter {
	if perSecond <= 0 {
		perSecond = DefaultRatePerSecond
	}
	return rate.NewLimiter(rate.Limit(perSecond), DefaultBurst)
}

// WithLimiter makes the client wait on l, which other clients may share.
// A nil l leaves the client's own.
func WithLimiter(l *rate.Limiter) Option {
	return func(c *Client) {
		if l != nil {
			c.limiter = l
		}
	}
}

// WithRateLimit gives the client a limiter of its own at this rate.
func WithRateLimit(limit rate.Limit, burst int) Option {
	return func(c *Client) { c.limiter = rate.NewLimiter(limit, burst) }
}

// New returns a client with a limiter of its own at DefaultRatePerSecond.
// A process with more than one client gives them all one limiter with
// WithLimiter instead.
func New(auth Auth, opts ...Option) *Client {
	c := &Client{
		http:    &http.Client{Timeout: 30 * time.Second},
		baseURL: defaultBaseURL,
		auth:    auth,
		limiter: rate.NewLimiter(DefaultRatePerSecond, DefaultBurst),
		cache:   noCache{},
		sleep:   sleepCtx,
	}
	for _, opt := range opts {
		opt(c)
	}
	return c
}

// Limiter is the budget this client waits on. It is exposed so a process
// can check that every client it built draws on the same one.
func (c *Client) Limiter() *rate.Limiter { return c.limiter }

// Movie fetches a movie together with its full cast in one request.
func (c *Client) Movie(ctx context.Context, id int) (*Movie, error) {
	var m Movie
	q := url.Values{"append_to_response": {"credits"}}
	if err := c.get(ctx, fmt.Sprintf("/movie/%d", id), q, &m); err != nil {
		return nil, err
	}
	return &m, nil
}

// Person fetches a person's profile together with every movie they acted
// in or crewed on, in one request: a second round trip for the filmography
// would cost more than the extra payload for the people scoring then rejects.
func (c *Client) Person(ctx context.Context, id int) (*Person, error) {
	var p Person
	q := url.Values{"append_to_response": {"movie_credits"}}
	if err := c.get(ctx, fmt.Sprintf("/person/%d", id), q, &p); err != nil {
		return nil, err
	}
	return &p, nil
}

// SearchMovies returns the first page of movies matching query.
func (c *Client) SearchMovies(ctx context.Context, query string) (*SearchResults, error) {
	var sr SearchResults
	q := url.Values{"query": {query}, "include_adult": {"false"}}
	if err := c.get(ctx, "/search/movie", q, &sr); err != nil {
		return nil, err
	}
	return &sr, nil
}

// get performs a cached, rate-limited GET and decodes the JSON body into out.
func (c *Client) get(ctx context.Context, path string, q url.Values, out any) error {
	key := path
	if len(q) > 0 {
		key += "?" + q.Encode()
	}
	if body, ok := c.cache.Get(key); ok {
		if err := json.Unmarshal(body, out); err == nil {
			return nil
		}
		// A corrupt entry falls through to a fresh fetch that overwrites it.
	}

	body, err := c.fetch(ctx, key)
	if err != nil {
		return err
	}
	if err := json.Unmarshal(body, out); err != nil {
		return fmt.Errorf("tmdb: decode %s: %w", path, err)
	}
	if err := c.cache.Set(key, body); err != nil {
		return fmt.Errorf("tmdb: cache %s: %w", path, err)
	}
	return nil
}

const maxAttempts = 4

// fetch retries on 429 and 5xx with exponential backoff, honouring Retry-After.
func (c *Client) fetch(ctx context.Context, pathAndQuery string) ([]byte, error) {
	var lastErr error
	for attempt := 0; attempt < maxAttempts; attempt++ {
		if err := c.limiter.Wait(ctx); err != nil {
			return nil, err
		}
		body, retryAfter, err := c.do(ctx, pathAndQuery)
		if err == nil {
			return body, nil
		}
		var re *retryableError
		if !errors.As(err, &re) {
			return nil, err
		}
		lastErr = err
		wait := retryAfter
		if wait == 0 {
			wait = time.Duration(1<<attempt) * 500 * time.Millisecond
		}
		if err := c.sleep(ctx, wait); err != nil {
			return nil, err
		}
	}
	return nil, fmt.Errorf("tmdb: giving up after %d attempts: %w", maxAttempts, lastErr)
}

func (c *Client) do(ctx context.Context, pathAndQuery string) ([]byte, time.Duration, error) {
	u := c.baseURL + pathAndQuery
	if c.auth.AccessToken == "" {
		sep := "?"
		if strings.Contains(pathAndQuery, "?") {
			sep = "&"
		}
		u += sep + "api_key=" + url.QueryEscape(c.auth.APIKey)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return nil, 0, err
	}
	req.Header.Set("Accept", "application/json")
	if c.auth.AccessToken != "" {
		req.Header.Set("Authorization", "Bearer "+c.auth.AccessToken)
	}

	resp, err := c.http.Do(req)
	if err != nil {
		return nil, 0, &retryableError{err: err}
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 8<<20))
	if err != nil {
		return nil, 0, &retryableError{err: err}
	}

	switch {
	case resp.StatusCode == http.StatusOK:
		return body, 0, nil
	case resp.StatusCode == http.StatusNotFound:
		return nil, 0, ErrNotFound
	case resp.StatusCode == http.StatusTooManyRequests || resp.StatusCode >= 500:
		return nil, parseRetryAfter(resp.Header.Get("Retry-After")), &retryableError{err: apiError(resp.StatusCode, body)}
	default:
		return nil, 0, apiError(resp.StatusCode, body)
	}
}

type retryableError struct{ err error }

func (e *retryableError) Error() string { return e.err.Error() }
func (e *retryableError) Unwrap() error { return e.err }

// StatusError is TMDb answering with a status that is not an answer.
// It carries the status so a caller can tell a refused key (401, 403),
// which only a person can fix, from a fault that will pass.
type StatusError struct {
	Status  int
	Message string
}

func (e *StatusError) Error() string {
	if e.Message == "" {
		return fmt.Sprintf("tmdb: HTTP %d", e.Status)
	}
	return fmt.Sprintf("tmdb: HTTP %d: %s", e.Status, e.Message)
}

func apiError(status int, body []byte) error {
	var msg struct {
		StatusMessage string `json:"status_message"`
	}
	_ = json.Unmarshal(body, &msg)
	return &StatusError{Status: status, Message: msg.StatusMessage}
}

func parseRetryAfter(v string) time.Duration {
	secs, err := strconv.Atoi(v)
	if err != nil || secs <= 0 {
		return 0
	}
	return time.Duration(secs) * time.Second
}

func sleepCtx(ctx context.Context, d time.Duration) error {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-t.C:
		return nil
	}
}
