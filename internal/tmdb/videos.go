package tmdb

import (
	"context"
	"fmt"
	"strings"
	"time"
)

// Video is one clip TMDb lists for a movie: a trailer, a teaser, a
// featurette. Key is the id on the site that hosts it, which for YouTube
// is what goes after watch?v=.
type Video struct {
	Key  string
	Site string
	Type string
	// Official is TMDb's flag for a clip the studio itself published.
	Official bool
	// Language is the clip's ISO 639-1 code, such as "en". Empty when
	// TMDb does not say.
	Language string
	// Published is when the clip went up. Zero when TMDb does not say.
	Published time.Time
}

// Videos lists the clips TMDb has for a movie, by TMDb's own id.
//
// ErrNotFound means TMDb has no movie with that id any more, which is an
// answer: it has no clips for it either.
func (c *Client) Videos(ctx context.Context, id int) ([]Video, error) {
	if id <= 0 {
		return nil, fmt.Errorf("tmdb: %d is not a movie id", id)
	}
	var payload struct {
		Results []struct {
			Key         string `json:"key"`
			Site        string `json:"site"`
			Type        string `json:"type"`
			Official    bool   `json:"official"`
			Language    string `json:"iso_639_1"`
			PublishedAt string `json:"published_at"`
		} `json:"results"`
	}
	if err := c.get(ctx, fmt.Sprintf("/movie/%d/videos", id), nil, &payload); err != nil {
		return nil, err
	}
	out := make([]Video, 0, len(payload.Results))
	for _, r := range payload.Results {
		v := Video{
			Key:      strings.TrimSpace(r.Key),
			Site:     strings.TrimSpace(r.Site),
			Type:     strings.TrimSpace(r.Type),
			Official: r.Official,
			Language: strings.ToLower(strings.TrimSpace(r.Language)),
		}
		// TMDb writes "2014-10-02T19:00:25.000Z". RFC 3339 reads the
		// fraction whether or not the layout names one.
		if when, err := time.Parse(time.RFC3339, strings.TrimSpace(r.PublishedAt)); err == nil {
			v.Published = when
		}
		out = append(out, v)
	}
	return out, nil
}
