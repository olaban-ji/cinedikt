// Package tmdb is a small client for The Movie Database API with a
// token-bucket rate limiter.
package tmdb

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"golang.org/x/time/rate"

	"cinedikt/internal/httpretry"
)

const defaultBaseURL = "https://api.themoviedb.org/3"

// ErrNotFound is returned when TMDb has no record for the requested id.
var ErrNotFound = errors.New("tmdb: not found")

// ErrKey is TMDb refusing the credentials, with a 401 or a 403. Nothing
// but new credentials fixes it, so it is worth telling apart from a
// fault that will pass. The error TMDb's refusal comes back as is a
// StatusError, which still carries what TMDb said; errors.Is(err,
// ErrKey) is how a caller asks whether it was this.
var ErrKey = errors.New("tmdb: the credentials were refused")

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
	sleep   func(context.Context, time.Duration) error
}

// Option configures a Client.
type Option func(*Client)

// WithBaseURL points the client at a different server (used by tests).
func WithBaseURL(u string) Option { return func(c *Client) { c.baseURL = strings.TrimRight(u, "/") } }

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
		sleep:   httpretry.SleepCtx,
	}
	for _, opt := range opts {
		opt(c)
	}
	return c
}

// Limiter is the budget this client waits on. It is exposed so a process
// can check that every client it built draws on the same one.
func (c *Client) Limiter() *rate.Limiter { return c.limiter }

// SearchMovies returns the first page of movies matching query.
func (c *Client) SearchMovies(ctx context.Context, query string) (*SearchResults, error) {
	var sr SearchResults
	q := url.Values{"query": {query}, "include_adult": {"false"}}
	if err := c.get(ctx, "/search/movie", q, &sr); err != nil {
		return nil, err
	}
	return &sr, nil
}

// get performs a rate-limited GET and decodes the JSON body into out.
func (c *Client) get(ctx context.Context, path string, q url.Values, out any) error {
	pathAndQuery := path
	if len(q) > 0 {
		pathAndQuery += "?" + q.Encode()
	}
	body, err := c.fetch(ctx, pathAndQuery)
	if err != nil {
		return err
	}
	if err := json.Unmarshal(body, out); err != nil {
		return fmt.Errorf("tmdb: decode %s: %w", path, err)
	}
	return nil
}

// maxAttempts is one request and three retries.
const maxAttempts = 4

// fetch is one answer: rate-limited, and retried on 429, 5xx and a
// failed connection, honouring Retry-After.
func (c *Client) fetch(ctx context.Context, pathAndQuery string) ([]byte, error) {
	retry := httpretry.Policy{Name: "tmdb", Attempts: maxAttempts, Limiter: c.limiter, Sleep: c.sleep}
	return retry.Do(ctx, func() ([]byte, error) { return c.do(ctx, pathAndQuery) })
}

func (c *Client) do(ctx context.Context, pathAndQuery string) ([]byte, error) {
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
		return nil, err
	}
	req.Header.Set("Accept", "application/json")
	if c.auth.AccessToken != "" {
		req.Header.Set("Authorization", "Bearer "+c.auth.AccessToken)
	}

	resp, err := c.http.Do(req)
	if err != nil {
		return nil, &httpretry.Retryable{Err: err}
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 8<<20))
	if err != nil {
		return nil, &httpretry.Retryable{Err: err}
	}

	switch {
	case resp.StatusCode == http.StatusOK:
		return body, nil
	case resp.StatusCode == http.StatusNotFound:
		return nil, ErrNotFound
	case httpretry.RetryStatus(resp.StatusCode):
		return nil, &httpretry.Retryable{Err: apiError(resp.StatusCode, body),
			After: httpretry.ParseRetryAfter(resp.Header.Get("Retry-After"), time.Now())}
	default:
		return nil, apiError(resp.StatusCode, body)
	}
}

// StatusError is TMDb answering with a status that is not an answer.
type StatusError struct {
	Status  int
	Message string
}

// Is makes a refusal of the credentials ErrKey, so a caller can tell it
// from a fault that will pass without reading the status.
func (e *StatusError) Is(target error) bool {
	return target == ErrKey && (e.Status == http.StatusUnauthorized || e.Status == http.StatusForbidden)
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
