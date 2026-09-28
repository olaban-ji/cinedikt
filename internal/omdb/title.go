package omdb

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"strings"
	"time"
)

// Title is what OMDb knows that the IMDb datasets do not: the address of
// a poster, the day a film opened, and what it is about. The dump carries
// only a year.
type Title struct {
	Poster string
	// Released is the full date. Zero when OMDb has only a year, or
	// nothing at all.
	Released time.Time
	// Plot is the full synopsis, cleaned of control characters and
	// trimmed. Empty when OMDb has none.
	Plot string
}

// titleCacheKey is where a lookup's body is kept. It names the plot
// length asked for: a body cached before lookups asked for the full plot
// holds the short one, and must not be served as the answer to this.
func titleCacheKey(imdbID string) string { return "tf:" + imdbID }

// Lookup reads the poster address, release date and full plot for an
// IMDb id.
//
// It returns a Title and no error when OMDb answered but had none of
// them: a film with no poster is a fact worth storing, or it would be
// asked for again every night forever. An answer that cannot be read
// even once repaired is ErrUnreadable, and is a fact of the same kind.
func (c *Client) Lookup(ctx context.Context, imdbID string) (Title, error) {
	if c.paused() {
		return Title{}, ErrQuota
	}
	key := titleCacheKey(imdbID)
	body, ok := c.cache.Get(key)
	if !ok {
		// The whole plot, not the one-line summary: the film panel shows
		// all of it, and the preview clamps it to six lines itself.
		fetched, err := c.get(ctx, url.Values{"i": {imdbID}, "plot": {"full"}}, imdbID)
		if err != nil {
			return Title{}, err
		}
		body = fetched
	}
	t, err := parseTitle(body, imdbID)
	if err != nil {
		if err == ErrQuota {
			c.pause()
		}
		return Title{}, err
	}
	if !ok {
		if cerr := c.cache.Set(key, body); cerr != nil {
			return t, nil // a cache that will not write is not a lookup failure
		}
	}
	return t, nil
}

// ParsePlot reads OMDb's plot, cleaned of control characters and
// trimmed. "N/A" and an empty value are both no synopsis at all.
func ParsePlot(raw string) string {
	raw = cleanText(raw)
	if raw == "N/A" {
		return ""
	}
	return raw
}

// posterIsSafe rejects any address that carries a key. OMDb also serves
// images from img.omdbapi.com, where the key is part of the URL; putting
// one of those in a page would publish the key to every reader.
func posterIsSafe(raw string) bool {
	if raw == "" || raw == "N/A" {
		return false
	}
	lower := strings.ToLower(raw)
	if strings.Contains(lower, "apikey") || strings.Contains(lower, "omdbapi.com") {
		return false
	}
	return strings.HasPrefix(lower, "https://") || strings.HasPrefix(lower, "http://")
}

// ReleasedLayout is how OMDb writes a date: "31 Mar 1999".
const ReleasedLayout = "02 Jan 2006"

// ParseReleased reads OMDb's date. "N/A", an empty value, or a year on
// its own gives the zero time, which stores as no date at all.
func ParseReleased(raw string) (time.Time, bool) {
	raw = strings.TrimSpace(raw)
	if raw == "" || raw == "N/A" {
		return time.Time{}, false
	}
	when, err := time.Parse(ReleasedLayout, raw)
	if err != nil {
		return time.Time{}, false
	}
	return when, true
}

func parseTitle(body []byte, asked string) (Title, error) {
	var payload struct {
		Response string `json:"Response"`
		Error    string `json:"Error"`
		Poster   string `json:"Poster"`
		Released string `json:"Released"`
		Plot     string `json:"Plot"`
	}
	if err := decode(body, &payload); err != nil {
		return Title{}, err
	}
	if !strings.EqualFold(payload.Response, "true") {
		switch {
		case strings.Contains(strings.ToLower(payload.Error), "limit reached"):
			return Title{}, ErrQuota
		case badKey(payload.Error):
			return Title{}, ErrKey
		case settled(payload.Error, asked):
			return Title{}, ErrNotFound
		default:
			return Title{}, fmt.Errorf("omdb: %s", payload.Error)
		}
	}
	var t Title
	if posterIsSafe(payload.Poster) {
		t.Poster = payload.Poster
	}
	if when, ok := ParseReleased(payload.Released); ok {
		t.Released = when
	}
	t.Plot = ParsePlot(payload.Plot)
	return t, nil
}

// Hit is one search result.
type Hit struct {
	IMDbID string `json:"id"`
	Title  string `json:"title"`
	Year   int    `json:"year"`
	Poster string `json:"poster,omitempty"`
}

// SearchLimit is how many results OMDb returns on a page. It is the
// API's own page size, not a choice made here.
const SearchLimit = 10

// Search finds movies by title. `type=movie` is not optional: without it
// the answer is mostly series and episodes, which have no map.
//
// An empty result is not an error. OMDb says "Movie not found!" for a
// query nobody matches, and the reader should be told nothing matched
// rather than shown a failure.
func (c *Client) Search(ctx context.Context, query string) ([]Hit, error) {
	query = strings.TrimSpace(query)
	if query == "" {
		return nil, nil
	}
	if c.paused() {
		return nil, ErrQuota
	}
	key := "s:" + strings.ToLower(query)
	body, ok := c.cache.Get(key)
	if !ok {
		fetched, err := c.get(ctx, url.Values{"s": {query}, "type": {"movie"}}, "search "+query)
		if err != nil {
			return nil, err
		}
		body = fetched
	}
	hits, err := parseSearch(body)
	if err != nil {
		if err == ErrQuota {
			c.pause()
		}
		// An answer that cannot be read is no more use to the reader
		// than one with nothing in it, and is told the same way.
		if err == ErrNotFound || errors.Is(err, ErrUnreadable) {
			return nil, nil
		}
		return nil, err
	}
	if !ok {
		_ = c.cache.Set(key, body)
	}
	return hits, nil
}

func parseSearch(body []byte) ([]Hit, error) {
	var payload struct {
		Response string `json:"Response"`
		Error    string `json:"Error"`
		Search   []struct {
			Title  string `json:"Title"`
			Year   string `json:"Year"`
			IMDbID string `json:"imdbID"`
			Type   string `json:"Type"`
			Poster string `json:"Poster"`
		} `json:"Search"`
	}
	if err := decode(body, &payload); err != nil {
		return nil, err
	}
	if !strings.EqualFold(payload.Response, "true") {
		switch {
		case strings.Contains(strings.ToLower(payload.Error), "limit reached"):
			return nil, ErrQuota
		case saysNo(payload.Error),
			strings.Contains(strings.ToLower(payload.Error), "too many results"):
			return nil, ErrNotFound
		default:
			return nil, fmt.Errorf("omdb: %s", payload.Error)
		}
	}
	out := make([]Hit, 0, len(payload.Search))
	for _, r := range payload.Search {
		// type=movie is asked for, and checked: the parameter is the
		// server's promise and this is the one that matters.
		if !strings.EqualFold(r.Type, "movie") || r.IMDbID == "" {
			continue
		}
		hit := Hit{IMDbID: r.IMDbID, Title: cleanText(r.Title), Year: searchYear(r.Year)}
		if posterIsSafe(r.Poster) {
			hit.Poster = r.Poster
		}
		out = append(out, hit)
	}
	return out, nil
}

// searchYear reads the year off a search hit. A film is one year; the
// field can still arrive as a range, so only the first is read.
func searchYear(raw string) int {
	raw = strings.TrimSpace(raw)
	if len(raw) < 4 {
		return 0
	}
	var year int
	for i := 0; i < 4; i++ {
		d := raw[i]
		if d < '0' || d > '9' {
			return 0
		}
		year = year*10 + int(d-'0')
	}
	return year
}
