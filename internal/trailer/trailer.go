// Package trailer chooses the YouTube trailer a film's panel plays in
// place. The trailer job that fills meta.trailers chooses through Pick;
// GET /api/trailers/{tconst} only reads what it chose.
package trailer

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"time"

	"golang.org/x/time/rate"

	"cinedikt/internal/tmdb"
)

// Lister is TMDb's list of clips for a movie.
type Lister interface {
	Videos(ctx context.Context, id int) ([]tmdb.Video, error)
}

// Checker says whether YouTube lets a video play inside another site.
type Checker interface {
	Embeddable(ctx context.Context, key string) (bool, error)
}

// MaxCandidates is how many of a film's ranked trailers are checked
// before it is taken to have none that can be embedded. A studio that
// blocks embedding blocks all of its uploads, and checking a long list
// one by one would spend YouTube's budget, and a reader's wait, on a
// film whose answer is already plain.
const MaxCandidates = 8

// Pick returns the key of the best trailer for a TMDb movie that YouTube
// will play embedded, or "" when there is none. An empty answer is an
// answer, worth storing; an error is a lookup that failed and should be
// asked again.
//
// Candidates are tried in Rank's order, and a refusal moves on to the
// next: studios often block embedding, and the player would otherwise
// open on "Video unavailable". A check that fails outright ends the pick
// with an error rather than moving on, because settling for the next
// candidate would store a worse trailer for good.
func Pick(ctx context.Context, list Lister, check Checker, tmdbID int) (string, error) {
	videos, err := list.Videos(ctx, tmdbID)
	if errors.Is(err, tmdb.ErrNotFound) {
		return "", nil
	}
	if err != nil {
		return "", err
	}
	ranked := Rank(videos)
	if len(ranked) > MaxCandidates {
		ranked = ranked[:MaxCandidates]
	}
	for _, v := range ranked {
		ok, err := check.Embeddable(ctx, v.Key)
		if err != nil {
			return "", err
		}
		if ok {
			return v.Key, nil
		}
	}
	return "", nil
}

// Rank keeps the YouTube trailers and teasers and orders them best
// first: a trailer before a teaser, then the studio's own, then English,
// then the newest. The rest of what TMDb lists (featurettes, clips,
// behind the scenes) is left out, because the button says "Watch
// trailer".
func Rank(videos []tmdb.Video) []tmdb.Video {
	out := make([]tmdb.Video, 0, len(videos))
	for _, v := range videos {
		if !strings.EqualFold(v.Site, "YouTube") || kind(v.Type) < 0 || !validKey(v.Key) {
			continue
		}
		out = append(out, v)
	}
	sort.SliceStable(out, func(i, j int) bool {
		a, b := out[i], out[j]
		if ka, kb := kind(a.Type), kind(b.Type); ka != kb {
			return ka < kb
		}
		if a.Official != b.Official {
			return a.Official
		}
		if ea, eb := a.Language == "en", b.Language == "en"; ea != eb {
			return ea
		}
		if !a.Published.Equal(b.Published) {
			return a.Published.After(b.Published)
		}
		return a.Key < b.Key
	})
	return out
}

// kind orders the clip types a trailer can be: 0 for a trailer, 1 for a
// teaser, and -1 for anything else.
func kind(t string) int {
	switch {
	case strings.EqualFold(t, "Trailer"):
		return 0
	case strings.EqualFold(t, "Teaser"):
		return 1
	}
	return -1
}

// validKey is the shape of a YouTube video id. The key ends up in an
// embed address on the page, so anything else is dropped here rather
// than escaped there.
func validKey(key string) bool {
	if key == "" || len(key) > 64 {
		return false
	}
	for _, r := range key {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '-', r == '_':
		default:
			return false
		}
	}
	return true
}

// WatchURL is the page YouTube serves a video on.
func WatchURL(key string) string { return "https://www.youtube.com/watch?v=" + key }

const defaultOEmbedEndpoint = "https://www.youtube.com/oembed"

// OEmbedRate is how many checks a second go to YouTube. It is its own
// budget, well apart from TMDb's: the checks go to another company, and
// asking politely is the whole of what keeps a key-less endpoint open.
const OEmbedRate = 5

// OEmbedTimeout bounds one check. A reader who has opened a film nobody
// had looked up is waiting on the job's answer, and one check that hangs
// must not hold it for long.
const OEmbedTimeout = 3 * time.Second

// OEmbed asks YouTube's oEmbed endpoint whether a video can be embedded.
// It needs no key. The trailer job holds the process's one, and it is
// the only thing that asks YouTube, so the process keeps to OEmbedRate.
type OEmbed struct {
	http     *http.Client
	endpoint string
	limiter  *rate.Limiter
}

// OEmbedOption configures an OEmbed.
type OEmbedOption func(*OEmbed)

// WithEndpoint points the checker at a different server (used by tests).
func WithEndpoint(u string) OEmbedOption { return func(o *OEmbed) { o.endpoint = u } }

// WithCheckRate replaces the checker's limiter (used by tests).
func WithCheckRate(limit rate.Limit, burst int) OEmbedOption {
	return func(o *OEmbed) { o.limiter = rate.NewLimiter(limit, burst) }
}

// NewOEmbed returns a checker at OEmbedRate with OEmbedTimeout.
func NewOEmbed(opts ...OEmbedOption) *OEmbed {
	o := &OEmbed{
		http:     &http.Client{Timeout: OEmbedTimeout},
		endpoint: defaultOEmbedEndpoint,
		limiter:  rate.NewLimiter(OEmbedRate, OEmbedRate),
	}
	for _, opt := range opts {
		opt(o)
	}
	return o
}

// CheckError is YouTube's oEmbed failing to answer, as opposed to TMDb
// failing, so whoever reports it can name the right service.
type CheckError struct {
	Key string
	// Status is the HTTP status, zero when no answer came back at all.
	Status int
	Err    error
}

func (e *CheckError) Error() string {
	if e.Status != 0 {
		return fmt.Sprintf("trailer: oembed %s: HTTP %d", e.Key, e.Status)
	}
	return fmt.Sprintf("trailer: oembed %s: %v", e.Key, e.Err)
}

func (e *CheckError) Unwrap() error { return e.Err }

// Embeddable is true on a 200, and false on a 400, 401, 403 or 404:
// embedding turned off, a private video, or one that is gone. YouTube
// answers 400 rather than 404 for many video ids it no longer holds, and
// TMDb's lists often still carry those dead uploads, so reading a 400 as
// a fault would fail the film's lookup on every open instead of moving
// on to its next trailer. Any other status, or no answer at all, is a
// *CheckError, because it says nothing about the video.
func (o *OEmbed) Embeddable(ctx context.Context, key string) (bool, error) {
	if err := o.limiter.Wait(ctx); err != nil {
		return false, err
	}
	q := url.Values{"format": {"json"}, "url": {WatchURL(key)}}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, o.endpoint+"?"+q.Encode(), nil)
	if err != nil {
		return false, err
	}
	resp, err := o.http.Do(req)
	if err != nil {
		return false, &CheckError{Key: key, Err: err}
	}
	// Read what little there is, so the connection can be used again.
	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 64<<10))
	resp.Body.Close()
	switch resp.StatusCode {
	case http.StatusOK:
		return true, nil
	case http.StatusBadRequest, http.StatusUnauthorized, http.StatusForbidden, http.StatusNotFound:
		return false, nil
	}
	return false, &CheckError{Key: key, Status: resp.StatusCode}
}
