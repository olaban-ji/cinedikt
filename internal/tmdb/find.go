package tmdb

// Finding a movie by its IMDb id. This is what the poster fallback and
// the id matcher need from TMDb: the catalog is keyed by tconst, and TMDb
// will map one to its own record in a single call.

import (
	"context"
	"fmt"
	"net/url"
	"strings"
	"time"
)

// ImageBase is where TMDb serves artwork. It needs no key, and the
// segment after /t/p/ is the width.
const ImageBase = "https://image.tmdb.org/t/p"

// PosterWidth is the size a stored TMDb poster address names. It is
// what the cards and the share card both resize from, so it has to be
// at least as wide as the largest of them draws.
const PosterWidth = "w780"

// Found is what a lookup by IMDb id turned up.
type Found struct {
	// ID is TMDb's own movie id. It is what a search result is keyed by,
	// so a later search can be joined back to this title without asking
	// again. Zero only when the struct was built by a caller that did
	// not have one; a movie TMDb returned always has one.
	ID int
	// Poster is a full address, or empty when TMDb has the movie but no
	// artwork for it. Those exist, and they are a definite answer.
	Poster string
	// Released is TMDb's release date, which is sometimes known where
	// OMDb's is not.
	Released time.Time
	// Overview is TMDb's synopsis, trimmed. Empty when it has none. It
	// arrives on the same answer as the id, so keeping it costs no
	// request of its own.
	Overview string
}

// FindByIMDb maps an IMDb title id to TMDb's own record.
//
// ErrNotFound means TMDb has no movie for that id — an answer, not a
// failure, and the caller should stop asking.
func (c *Client) FindByIMDb(ctx context.Context, imdbID string) (Found, error) {
	if !validIMDbID(imdbID) {
		return Found{}, fmt.Errorf("tmdb: %q is not an IMDb title id", imdbID)
	}
	var payload struct {
		Movies []struct {
			ID          int    `json:"id"`
			PosterPath  string `json:"poster_path"`
			ReleaseDate string `json:"release_date"`
			Overview    string `json:"overview"`
		} `json:"movie_results"`
	}
	q := url.Values{"external_source": {"imdb_id"}}
	if err := c.get(ctx, "/find/"+imdbID, q, &payload); err != nil {
		return Found{}, err
	}
	if len(payload.Movies) == 0 {
		return Found{}, ErrNotFound
	}
	// One IMDb id maps to one movie. More than one would be TMDb's
	// mistake, and the first is the only defensible pick.
	found := payload.Movies[0]
	var out Found
	out.ID = found.ID
	if path := strings.TrimSpace(found.PosterPath); path != "" {
		out.Poster = PosterURL(path, PosterWidth)
	}
	if when, err := time.Parse("2006-01-02", found.ReleaseDate); err == nil {
		out.Released = when
	}
	out.Overview = strings.TrimSpace(found.Overview)
	return out, nil
}

// PosterURL is the address of one piece of TMDb artwork at a width.
func PosterURL(path, width string) string {
	if path == "" {
		return ""
	}
	if !strings.HasPrefix(path, "/") {
		path = "/" + path
	}
	return ImageBase + "/" + width + path
}

// validIMDbID keeps a malformed id out of a URL path.
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
