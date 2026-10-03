package streaming

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"strconv"
	"time"
)

// The kinds of change the /changes feed reports that have already
// happened. The feed also reports expiring and upcoming ones, which are
// still to come; leaving options are kept right by their own expiry, so
// nothing here asks for those.
const (
	ChangeNew     = "new"
	ChangeRemoved = "removed"
	ChangeUpdated = "updated"
)

// ChangesQuery is one page of the /changes feed: the changes of one kind
// to movies in one country, between From and To, both inclusive.
type ChangesQuery struct {
	Country string
	// Type is ChangeNew, ChangeRemoved or ChangeUpdated.
	Type     string
	From, To time.Time
	// Cursor is the previous page's NextCursor; "" for the first page.
	Cursor string
}

// ChangesPage is one page of the feed, oldest change first.
type ChangesPage struct {
	Changes []Change
	// Shows are the shows these changes touch, by the API's own show id,
	// which is what a Change names. A show that could not be read, or
	// carried no valid IMDb id, is left out, and its changes match
	// nothing.
	Shows      map[string]ChangedShow
	HasMore    bool
	NextCursor string
}

// Change is one change to one show.
type Change struct {
	Type   string
	ShowID string
	// At is when it happened. Zero when the API gave no time, which it
	// says it never does for a change that has happened.
	At time.Time
}

// ChangedShow is a show as the feed carries it beside its changes.
type ChangedShow struct {
	// IMDbID is the show's IMDb id, the catalog's tconst; "" when the
	// show did not carry a valid one.
	IMDbID string
	// options are its streaming options by country, still unread, so a
	// country whose list cannot be read costs that country alone.
	options map[string]json.RawMessage
}

// Options is what streams the show in country now, as the feed carries
// it, and whether the feed carried a list for that country at all. A
// show without the field, or without the country in it, or whose list
// for it cannot be read, is false: that says nothing about whether the
// movie streams there, and taking it for an empty list would tell
// readers it is on nothing. An empty list the feed did send is true.
func (s ChangedShow) Options(country string) ([]StreamingOption, bool) {
	raw, ok := s.options[country]
	if !ok || isNull(raw) {
		return nil, false
	}
	var options []StreamingOption
	if err := json.Unmarshal(raw, &options); err != nil {
		return nil, false
	}
	if options == nil {
		options = []StreamingOption{}
	}
	return options, true
}

// Changes is one page of the /changes feed. It asks about movies only,
// whole shows rather than seasons or episodes, oldest first, so a caller
// that stops part way knows every change before the last one it read has
// been read.
func (c *Client) Changes(ctx context.Context, q ChangesQuery) (ChangesPage, error) {
	if !ValidCountry(q.Country) {
		return ChangesPage{}, fmt.Errorf("streaming: %q is not a country code", q.Country)
	}
	switch q.Type {
	case ChangeNew, ChangeRemoved, ChangeUpdated:
	default:
		return ChangesPage{}, fmt.Errorf("streaming: %q is not a kind of change that has happened", q.Type)
	}
	v := url.Values{
		"country":         {q.Country},
		"change_type":     {q.Type},
		"item_type":       {"show"},
		"show_type":       {"movie"},
		"order_direction": {"asc"},
		"output_language": {"en"},
	}
	if !q.From.IsZero() {
		v.Set("from", strconv.FormatInt(q.From.Unix(), 10))
	}
	if !q.To.IsZero() {
		v.Set("to", strconv.FormatInt(q.To.Unix(), 10))
	}
	if q.Cursor != "" {
		v.Set("cursor", q.Cursor)
	}
	var raw struct {
		Changes []struct {
			ChangeType string `json:"changeType"`
			ShowID     string `json:"showId"`
			Timestamp  *int64 `json:"timestamp"`
		} `json:"changes"`
		Shows      json.RawMessage `json:"shows"`
		HasMore    bool            `json:"hasMore"`
		NextCursor string          `json:"nextCursor"`
	}
	if err := c.get(ctx, "/changes", v, &raw); err != nil {
		return ChangesPage{}, err
	}
	page := ChangesPage{
		Changes:    make([]Change, 0, len(raw.Changes)),
		Shows:      readShows(raw.Shows),
		HasMore:    raw.HasMore,
		NextCursor: raw.NextCursor,
	}
	for _, ch := range raw.Changes {
		change := Change{Type: ch.ChangeType, ShowID: ch.ShowID}
		if ch.Timestamp != nil && *ch.Timestamp > 0 {
			change.At = time.Unix(*ch.Timestamp, 0).UTC()
		}
		page.Changes = append(page.Changes, change)
	}
	return page, nil
}

// readShows reads the feed's shows one at a time, so one the API shapes
// unexpectedly costs its own changes and not the page. The documentation
// keys them by show id; a list of shows, each with its id, is read the
// same way, and a show keyed by anything else is also found by the id it
// carries, which is the one its changes name.
func readShows(raw json.RawMessage) map[string]ChangedShow {
	out := map[string]ChangedShow{}
	if len(raw) == 0 || isNull(raw) {
		return out
	}
	var byID map[string]json.RawMessage
	if err := json.Unmarshal(raw, &byID); err != nil {
		var list []json.RawMessage
		if err := json.Unmarshal(raw, &list); err != nil {
			return out
		}
		byID = make(map[string]json.RawMessage, len(list))
		for _, item := range list {
			var id struct {
				ID string `json:"id"`
			}
			if json.Unmarshal(item, &id) == nil && id.ID != "" {
				byID[id.ID] = item
			}
		}
	}
	for id, item := range byID {
		var show struct {
			ID               string          `json:"id"`
			IMDbID           string          `json:"imdbId"`
			StreamingOptions json.RawMessage `json:"streamingOptions"`
		}
		if err := json.Unmarshal(item, &show); err != nil || !validIMDbID(show.IMDbID) {
			continue
		}
		changed := ChangedShow{IMDbID: show.IMDbID}
		// Options that cannot be read as a map of countries leave the
		// show without any, which asks again rather than trusting them.
		var options map[string]json.RawMessage
		if len(show.StreamingOptions) > 0 && json.Unmarshal(show.StreamingOptions, &options) == nil {
			changed.options = options
		}
		out[id] = changed
		// A show keyed by its own id keeps that place: the key is what
		// the documentation says a change names.
		if show.ID != "" && show.ID != id {
			if _, taken := byID[show.ID]; !taken {
				out[show.ID] = changed
			}
		}
	}
	return out
}

func isNull(raw json.RawMessage) bool {
	return bytes.Equal(bytes.TrimSpace(raw), []byte("null"))
}
