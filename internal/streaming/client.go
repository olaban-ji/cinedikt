// Package streaming asks the Streaming Availability API, by Movie of the
// Night, which services carry a movie in one country, and shapes the
// answer into what the page shows: where it streams, where it is free,
// and where it can be rented or bought.
//
// The API is metered per request, so nothing here caches or retries
// beyond what one answer needs. Keeping answers is the caller's job.
package streaming

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"

	"golang.org/x/time/rate"
)

// DefaultBaseURL is version 4 of the API on Movie of the Night's own
// developer platform. Not RapidAPI's copy of it: the key is issued by
// Movie of the Night, and only their host takes it.
const DefaultBaseURL = "https://api.movieofthenight.com/v4"

// DefaultRatePerSecond is how fast one process asks. The API is metered
// per request, so the rate is not what keeps the bill down; answers being
// kept is. What it bounds is a burst of readers opening movies nobody has
// asked about, which should queue for a moment rather than all leave at
// once.
const DefaultRatePerSecond = 5.0

// ErrNotFound is the API having no show for an id. It is an answer, not a
// failure: the movie is on no service the API knows of, anywhere.
var ErrNotFound = errors.New("streaming: no such show")

// ErrKey is the API refusing the key itself, with a 401 or a 403. Nothing
// but a new key fixes it, so it is worth telling apart from a fault that
// will pass.
var ErrKey = errors.New("streaming: the API key was refused")

// StatusError is the API answering with a status that is not an answer.
type StatusError struct {
	Status int
	// Message is what the API said about it, trimmed. It never carries
	// the key, which only ever travels in a request header.
	Message string
}

func (e *StatusError) Error() string {
	if e.Message == "" {
		return fmt.Sprintf("streaming: HTTP %d", e.Status)
	}
	return fmt.Sprintf("streaming: HTTP %d: %s", e.Status, e.Message)
}

// Client asks the API. It is safe for concurrent use.
type Client struct {
	http    *http.Client
	baseURL string
	key     string
	limiter *rate.Limiter
	sleep   func(context.Context, time.Duration) error
}

// Option configures a Client.
type Option func(*Client)

// WithBaseURL points the client at a different server, which is how the
// tests stand in for the API.
func WithBaseURL(u string) Option { return func(c *Client) { c.baseURL = strings.TrimRight(u, "/") } }

// WithHTTPClient replaces the default HTTP client. That one follows no
// redirect, and one given here should not either, or the key goes with
// the redirect wherever it points.
func WithHTTPClient(h *http.Client) Option { return func(c *Client) { c.http = h } }

// WithRate sets how many requests a second the client may make. Zero or
// less, or a number that is not one, takes DefaultRatePerSecond.
func WithRate(perSecond float64) Option {
	return func(c *Client) { c.limiter = newLimiter(perSecond) }
}

// New returns a client that sends key with every request.
func New(key string, opts ...Option) *Client {
	c := &Client{
		http: &http.Client{
			Timeout: 15 * time.Second,
			// The key is a custom header, which Go carries across a
			// redirect to any host. The API does not redirect, so none is
			// followed: a redirect comes back as the StatusError it is.
			CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
		},
		baseURL: DefaultBaseURL,
		key:     key,
		limiter: newLimiter(DefaultRatePerSecond),
		sleep:   sleepCtx,
	}
	for _, opt := range opts {
		opt(c)
	}
	return c
}

// newLimiter allows a second's worth of requests at once, and at least
// one, so a quiet process answers its first readers without a wait.
func newLimiter(perSecond float64) *rate.Limiter {
	if math.IsNaN(perSecond) || math.IsInf(perSecond, 0) || perSecond <= 0 {
		perSecond = DefaultRatePerSecond
	}
	return rate.NewLimiter(rate.Limit(perSecond), max(1, int(math.Ceil(perSecond))))
}

// Show is what streams a movie in one country, in the order the API gives
// it. The API takes IMDb ids itself, so there is no lookup in between.
//
// ErrNotFound means the API has no such show: the movie is on nothing,
// which is as definite as an empty list.
func (c *Client) Show(ctx context.Context, imdbID, country string) ([]StreamingOption, error) {
	if !validIMDbID(imdbID) {
		return nil, fmt.Errorf("streaming: %q is not an IMDb title id", imdbID)
	}
	if !ValidCountry(country) {
		return nil, fmt.Errorf("streaming: %q is not a country code", country)
	}
	q := url.Values{
		"country": {country},
		// A movie has no seasons; "show" keeps the answer to the movie
		// itself whatever the API makes of the id.
		"series_granularity": {"show"},
		"output_language":    {"en"},
	}
	var show struct {
		StreamingOptions map[string][]StreamingOption `json:"streamingOptions"`
	}
	if err := c.get(ctx, "/shows/"+imdbID, q, &show); err != nil {
		return nil, err
	}
	return show.StreamingOptions[country], nil
}

// Country is one country the API covers.
type Country struct {
	// Code is ISO 3166-1 alpha-2, lowercased, the way the API keys its
	// answers and the way this app names countries everywhere.
	Code string
	Name string
}

// Countries is every country the API covers, by code.
func (c *Client) Countries(ctx context.Context) ([]Country, error) {
	var raw map[string]struct {
		CountryCode string `json:"countryCode"`
		Name        string `json:"name"`
	}
	if err := c.get(ctx, "/countries", url.Values{"output_language": {"en"}}, &raw); err != nil {
		return nil, err
	}
	out := make([]Country, 0, len(raw))
	for key, v := range raw {
		code := strings.ToLower(strings.TrimSpace(v.CountryCode))
		if code == "" {
			code = strings.ToLower(strings.TrimSpace(key))
		}
		if !ValidCountry(code) {
			continue
		}
		name := strings.TrimSpace(v.Name)
		if name == "" {
			name = strings.ToUpper(code)
		}
		out = append(out, Country{Code: code, Name: name})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Code < out[j].Code })
	return out, nil
}

// ValidCountry is a lowercased ISO 3166-1 alpha-2 code's shape.
func ValidCountry(cc string) bool {
	return len(cc) == 2 && cc[0] >= 'a' && cc[0] <= 'z' && cc[1] >= 'a' && cc[1] <= 'z'
}

// validIMDbID is IMDb's title id: "tt" and digits.
func validIMDbID(id string) bool {
	if len(id) < 3 || len(id) > 20 || !strings.HasPrefix(id, "tt") {
		return false
	}
	for i := 2; i < len(id); i++ {
		if id[i] < '0' || id[i] > '9' {
			return false
		}
	}
	return true
}

// maxAttempts is one request and two retries. A reader is waiting on the
// first ask, so there is no budget for more.
const maxAttempts = 3

// get is one answer: rate-limited, retried on 429, 5xx and a failed
// connection, honouring Retry-After, and decoded into out.
func (c *Client) get(ctx context.Context, path string, q url.Values, out any) error {
	target := c.baseURL + path
	if len(q) > 0 {
		target += "?" + q.Encode()
	}
	var lastErr error
	for attempt := 0; attempt < maxAttempts; attempt++ {
		if err := c.limiter.Wait(ctx); err != nil {
			return err
		}
		body, retryAfter, err := c.do(ctx, path, target)
		if err == nil {
			if err := json.Unmarshal(body, out); err != nil {
				return fmt.Errorf("streaming: decode %s: %w", path, err)
			}
			return nil
		}
		var re *retryable
		if !errors.As(err, &re) {
			return err
		}
		lastErr = re.err
		if attempt == maxAttempts-1 {
			break
		}
		wait := retryAfter
		if wait <= 0 {
			wait = time.Duration(1<<attempt) * 500 * time.Millisecond
		}
		// A wait that outlasts the caller's deadline is no wait at all:
		// whoever asked has gone by the time it ends.
		if deadline, ok := ctx.Deadline(); ok && time.Until(deadline) < wait {
			return lastErr
		}
		if err := c.sleep(ctx, wait); err != nil {
			return err
		}
	}
	return fmt.Errorf("streaming: giving up after %d attempts: %w", maxAttempts, lastErr)
}

// do is one request. The key goes in a header, never the address, so no
// error that quotes the address can carry it.
func (c *Client) do(ctx context.Context, path, target string) ([]byte, time.Duration, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, target, nil)
	if err != nil {
		return nil, 0, err
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("X-API-Key", c.key)
	resp, err := c.http.Do(req)
	if err != nil {
		if ctx.Err() != nil {
			return nil, 0, ctx.Err()
		}
		return nil, 0, &retryable{err: fmt.Errorf("streaming: GET %s: %w", path, err)}
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 8<<20))
	if err != nil {
		return nil, 0, &retryable{err: fmt.Errorf("streaming: read %s: %w", path, err)}
	}
	switch {
	case resp.StatusCode == http.StatusOK:
		return body, 0, nil
	case resp.StatusCode == http.StatusNotFound:
		return nil, 0, ErrNotFound
	case resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden:
		return nil, 0, fmt.Errorf("%w (HTTP %d)", ErrKey, resp.StatusCode)
	case resp.StatusCode == http.StatusTooManyRequests || resp.StatusCode >= 500:
		return nil, parseRetryAfter(resp.Header.Get("Retry-After"), time.Now()), &retryable{err: statusError(resp.StatusCode, body)}
	default:
		return nil, 0, statusError(resp.StatusCode, body)
	}
}

type retryable struct{ err error }

func (e *retryable) Error() string { return e.err.Error() }
func (e *retryable) Unwrap() error { return e.err }

func statusError(status int, body []byte) error {
	var msg struct {
		Message string `json:"message"`
	}
	_ = json.Unmarshal(body, &msg)
	m := strings.TrimSpace(msg.Message)
	if len(m) > 200 {
		m = m[:200]
	}
	return &StatusError{Status: status, Message: m}
}

// parseRetryAfter reads either form the header takes: a number of
// seconds, or a date.
func parseRetryAfter(v string, now time.Time) time.Duration {
	v = strings.TrimSpace(v)
	if v == "" {
		return 0
	}
	if secs, err := strconv.Atoi(v); err == nil {
		if secs <= 0 {
			return 0
		}
		return time.Duration(secs) * time.Second
	}
	if at, err := http.ParseTime(v); err == nil && at.After(now) {
		return at.Sub(now)
	}
	return 0
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
