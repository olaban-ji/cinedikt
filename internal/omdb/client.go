// Package omdb reads what the OMDb API (omdbapi.com) knows about an IMDb
// title that the IMDb datasets do not: its poster, its release date and
// its plot.
package omdb

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"golang.org/x/time/rate"

	"cinedikt/internal/imdbid"
)

const defaultBaseURL = "https://www.omdbapi.com/"

// ErrNotFound is returned when OMDb has no entry for an id.
var ErrNotFound = errors.New("omdb: not found")

// ErrQuota is returned once OMDb has reported the daily request limit;
// the client then stops calling out for QuotaPause.
var ErrQuota = errors.New("omdb: daily request limit reached")

// ErrKey is OMDb refusing the key itself. Nothing but a new key fixes
// it, so a caller should stop asking rather than spend the day being
// told the same thing.
var ErrKey = errors.New("omdb: invalid API key")

// ErrUnreadable is an answer from OMDb that cannot be read: not JSON
// even once it has been repaired, or JSON of a shape the client does
// not expect. It wraps the decode error, so a log says why.
//
// It is an answer, not a failed lookup. OMDb sends the same bytes for
// a title every time it is asked, so asking again changes nothing: a
// caller records it the way it records OMDb having nothing, and
// neither retries it nor counts it as a failure.
var ErrUnreadable = errors.New("omdb: unreadable answer")

// QuotaPause is how long lookups are skipped after a quota error. OMDb's
// free quota resets daily; an hour keeps a long-running server from
// spending the whole day being told no.
const QuotaPause = time.Hour

// Client looks up titles by IMDb id. Once OMDb says the day's requests
// are spent, it stops asking for QuotaPause.
type Client struct {
	http    *http.Client
	baseURL string
	apiKey  string
	limiter *rate.Limiter
	now     func() time.Time

	mu         sync.Mutex
	pausedTill time.Time
}

// Option configures a Client.
type Option func(*Client)

// WithBaseURL points the client at a different server (used by tests).
func WithBaseURL(u string) Option { return func(c *Client) { c.baseURL = u } }

// WithRateLimit sets this client's own request rate in place of New's
// gentle default, for a job that runs many lookups at once.
func WithRateLimit(perSecond float64, burst int) Option {
	return func(c *Client) { c.limiter = rate.NewLimiter(rate.Limit(perSecond), burst) }
}

// WithConnections sizes the connection pool for a client that runs
// several lookups at once.
//
// Go keeps two idle connections per host by default, so without this a
// pool of workers spends its time opening and closing sockets against
// the same server — and the rate limit above becomes unreachable for
// reasons that have nothing to do with the rate.
func WithConnections(n int) Option {
	return func(c *Client) {
		if n < 2 {
			n = 2
		}
		t := http.DefaultTransport.(*http.Transport).Clone()
		t.MaxIdleConns = n * 2
		t.MaxIdleConnsPerHost = n
		t.MaxConnsPerHost = n * 2
		c.http.Transport = t
	}
}

// WithHTTPTimeout bounds one request. It changes the timeout and
// nothing else, so it may be given in any order alongside
// WithConnections.
func WithHTTPTimeout(d time.Duration) Option {
	return func(c *Client) { c.http.Timeout = d }
}

// New returns a client with a gentle 5 requests/second limit.
func New(apiKey string, opts ...Option) *Client {
	c := &Client{
		http:    &http.Client{Timeout: 15 * time.Second},
		baseURL: defaultBaseURL,
		apiKey:  apiKey,
		limiter: rate.NewLimiter(5, 5),
		now:     time.Now,
	}
	for _, opt := range opts {
		opt(c)
	}
	return c
}

func (c *Client) paused() bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.now().Before(c.pausedTill)
}

// PausedUntil is when a spent daily quota lets this client ask again.
// A zero or past time means it is not paused.
func (c *Client) PausedUntil() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.pausedTill
}

func (c *Client) pause() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.pausedTill = c.now().Add(QuotaPause)
}

// get is one call, under this client's limiter. `what` names the request
// in an error; a query string carries the key and never belongs in a log.
func (c *Client) get(ctx context.Context, q url.Values, what string) ([]byte, error) {
	if err := c.limiter.Wait(ctx); err != nil {
		return nil, err
	}
	q.Set("apikey", c.apiKey)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.baseURL+"?"+q.Encode(), nil)
	if err != nil {
		return nil, err
	}
	resp, err := c.http.Do(req)
	if err != nil {
		// A *url.Error quotes the whole request URL, and the URL carries
		// the key. What went wrong is the error inside it.
		var ue *url.Error
		if errors.As(err, &ue) {
			err = ue.Err
		}
		return nil, fmt.Errorf("omdb: GET %s: %w", what, err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return nil, fmt.Errorf("omdb: read %s: %w", what, err)
	}
	// OMDb reports most failures (bad key, quota, unknown id) as a 200
	// with Response:"False"; parse handles those. Anything else is transport.
	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusUnauthorized {
		return nil, fmt.Errorf("omdb: HTTP %d for %s", resp.StatusCode, what)
	}
	return body, nil
}

// noEntry is every way OMDb says it has no record of an id. They are
// all the same answer, and all of them are final.
//
// "Error getting data." is the one it actually returns for an id it
// does not hold. The wording reads like a fault, which is why it was
// taken for one — and a caller that takes it for one asks again
// forever. The backfill stored a hundred thousand permanent answers as
// retryable and re-asked every one of them every twenty minutes.
var noEntry = []string{
	"not found",
	"error getting data",
}

// badID is OMDb's answer to an id it will not look up, and on its own
// it is ambiguous: it is what OMDb says both for an id that is
// malformed and for a well-formed one it simply does not hold. That is
// why it is not in noEntry — the message alone does not settle it.
//
// The id does. Every id this app sends is a tconst straight out of
// IMDb's own dump, so a well-formed one coming back "incorrect" is OMDb
// saying it has no such title: final, and not worth asking again.
// Twelve thousand titles were re-asked every twenty minutes for want of
// that distinction. A malformed id is our own bug and stays an error,
// so it surfaces rather than being filed away as an answer.
const badID = "incorrect imdb id"

// settled reports whether OMDb has finally answered that it has nothing
// for the id we asked about.
func settled(msg, asked string) bool {
	if saysNo(msg) {
		return true
	}
	return strings.Contains(strings.ToLower(msg), badID) && imdbid.Title(asked)
}

// saysNo reports whether OMDb has answered, definitively, that it has
// nothing for this id. Matched case-insensitively: the wording is
// theirs, and it is not a contract.
func saysNo(msg string) bool {
	msg = strings.ToLower(msg)
	for _, no := range noEntry {
		if strings.Contains(msg, no) {
			return true
		}
	}
	return false
}

// badKey is OMDb's answer to a key it does not accept: "Invalid API
// key!" for one it has never issued, and the same for one it revoked.
func badKey(msg string) bool {
	return strings.Contains(strings.ToLower(msg), "invalid api key")
}
