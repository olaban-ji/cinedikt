package omdb

import (
	"context"
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
	// The whole plot, not the one-line summary: the film panel shows all
	// of it, and the preview clamps it to six lines itself.
	body, err := c.get(ctx, url.Values{"i": {imdbID}, "plot": {"full"}}, imdbID)
	if err != nil {
		return Title{}, err
	}
	t, err := parseTitle(body, imdbID)
	if err == ErrQuota {
		c.pause()
	}
	return t, err
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
