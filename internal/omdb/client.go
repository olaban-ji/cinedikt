// Package omdb fetches IMDb ratings from the OMDb API (omdbapi.com), the
// only free source for them; TMDb carries its own score, not IMDb's.
package omdb

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"

	"golang.org/x/time/rate"
)

const defaultBaseURL = "https://www.omdbapi.com/"

// ErrNotFound is returned when OMDb has no entry, or no rating, for an id.
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

// Cache stores raw response bodies keyed by IMDb id.
type Cache interface {
	Get(key string) ([]byte, bool)
	Set(key string, body []byte) error
}

type noCache struct{}

func (noCache) Get(string) ([]byte, bool) { return nil, false }
func (noCache) Set(string, []byte) error  { return nil }

// Rating is IMDb's user rating for a title.
type Rating struct {
	Value float64 // 0–10
	Votes int
}

// Client looks up titles by IMDb id. The free tier allows 1,000 requests a
// day, so callers should cache and look up only what they will show.
type Client struct {
	http    *http.Client
	baseURL string
	apiKey  string
	limiter *rate.Limiter
	cache   Cache
	now     func() time.Time

	mu         sync.Mutex
	pausedTill time.Time
}

// Option configures a Client.
type Option func(*Client)

// WithBaseURL points the client at a different server (used by tests).
func WithBaseURL(u string) Option { return func(c *Client) { c.baseURL = u } }

// WithRateLimit sets this client's own request rate. Search and the
// poster backfill each hold a client, so a reader's keystroke is never
// queued behind a bulk job.
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

// WithCache stores successful responses in cache and serves repeats from it.
func WithCache(cache Cache) Option { return func(c *Client) { c.cache = cache } }

// New returns a client with a gentle 5 requests/second limit.
func New(apiKey string, opts ...Option) *Client {
	c := &Client{
		http:    &http.Client{Timeout: 15 * time.Second},
		baseURL: defaultBaseURL,
		apiKey:  apiKey,
		limiter: rate.NewLimiter(5, 5),
		cache:   noCache{},
		now:     time.Now,
	}
	for _, opt := range opts {
		opt(c)
	}
	return c
}

// IMDbRating returns the IMDb rating for an IMDb id such as "tt0133093".
func (c *Client) IMDbRating(ctx context.Context, imdbID string) (Rating, error) {
	if body, ok := c.cache.Get(imdbID); ok {
		return parse(body, imdbID)
	}
	if c.paused() {
		return Rating{}, ErrQuota
	}
	body, err := c.fetch(ctx, imdbID)
	if err != nil {
		return Rating{}, err
	}
	r, err := parse(body, imdbID)
	if errors.Is(err, ErrQuota) {
		c.pause()
		return Rating{}, err
	}
	if err != nil && !errors.Is(err, ErrNotFound) && !errors.Is(err, ErrUnreadable) {
		// Key problems and the like are transient; never cache them.
		return Rating{}, err
	}
	// Cache "not found" too, and an answer that cannot be read: those ids
	// would otherwise be re-queried on every crawl and eat the daily
	// quota, and OMDb would only say the same again.
	if cerr := c.cache.Set(imdbID, body); cerr != nil {
		return Rating{}, fmt.Errorf("omdb: cache %s: %w", imdbID, cerr)
	}
	return r, err
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

func (c *Client) fetch(ctx context.Context, imdbID string) ([]byte, error) {
	return c.get(ctx, url.Values{"i": {imdbID}}, imdbID)
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
	return strings.Contains(strings.ToLower(msg), badID) && validIMDbID(asked)
}

// validIMDbID is the shape of every id this app sends: "tt" then digits.
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

// parse reads OMDb's envelope. Ratings arrive as strings ("8.7",
// "2,081,234") or "N/A".
func parse(body []byte, asked string) (Rating, error) {
	var env struct {
		Response   string `json:"Response"`
		Error      string `json:"Error"`
		IMDbRating string `json:"imdbRating"`
		IMDbVotes  string `json:"imdbVotes"`
	}
	if err := decode(body, &env); err != nil {
		return Rating{}, err
	}
	if env.Response != "True" {
		switch {
		case strings.Contains(strings.ToLower(env.Error), "limit reached"):
			return Rating{}, ErrQuota
		case badKey(env.Error):
			return Rating{}, ErrKey
		case settled(env.Error, asked):
			return Rating{}, ErrNotFound
		}
		return Rating{}, fmt.Errorf("omdb: %s", env.Error)
	}
	if env.IMDbRating == "" || env.IMDbRating == "N/A" {
		return Rating{}, ErrNotFound
	}
	value, err := strconv.ParseFloat(env.IMDbRating, 64)
	if err != nil {
		return Rating{}, fmt.Errorf("omdb: rating %q: %w", env.IMDbRating, err)
	}
	votes, _ := strconv.Atoi(strings.ReplaceAll(env.IMDbVotes, ",", ""))
	return Rating{Value: value, Votes: votes}, nil
}
