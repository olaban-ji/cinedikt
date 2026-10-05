package tmdb

// Finding a movie or a person by their IMDb id. The catalog is keyed by
// IMDb's ids, and TMDb maps one to its own record in a single call: a
// tconst to a movie for the poster fallback, the id matcher and the
// trailer job, and an nconst to a person for the people job.

import (
	"context"
	"fmt"
	"net/url"
	"strings"
	"time"

	"cinedikt/internal/imdbid"
)

// ImageBase is where TMDb serves artwork. It needs no key, and the
// segment after /t/p/ is the width.
const ImageBase = "https://image.tmdb.org/t/p"

// PosterWidth is the size a stored TMDb poster address names. It is
// what the cards and the share card both resize from, so it has to be
// at least as wide as the largest of them draws.
const PosterWidth = "w780"

// ProfileWidth is the size a person's photo is served at. TMDb keeps
// profiles at w45, w185, h632 and the original: w45 blurs on a screen
// of twice the density, and w185 is enough for a face beside a name
// without sending a full portrait for it.
const ProfileWidth = "w185"

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
	// A malformed id is kept out of the URL path.
	if !imdbid.Title(imdbID) {
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

// FoundPerson is what a lookup by IMDb name id turned up.
type FoundPerson struct {
	// ID is TMDb's own person id.
	ID int
	// Profile is the path of the person's photo on TMDb's image host,
	// such as "/abc.jpg", which PosterURL turns into an address at a
	// width. Empty when TMDb has the person but no photo, which is a
	// definite answer, and for a person TMDb marks adult, whose photo
	// is not one to put beside a movie's cast.
	Profile string
}

// FindPersonByIMDb maps an IMDb name id to TMDb's own person.
//
// ErrNotFound means TMDb has no person for that id: an answer, not a
// failure, and the caller should stop asking.
func (c *Client) FindPersonByIMDb(ctx context.Context, nconst string) (FoundPerson, error) {
	// A title id sent here would find a movie's results and none of a
	// person's, and read as "no such person".
	if !imdbid.Name(nconst) {
		return FoundPerson{}, fmt.Errorf("tmdb: %q is not an IMDb name id", nconst)
	}
	var payload struct {
		People []struct {
			ID          int     `json:"id"`
			ProfilePath *string `json:"profile_path"`
			Adult       bool    `json:"adult"`
		} `json:"person_results"`
	}
	q := url.Values{"external_source": {"imdb_id"}}
	if err := c.get(ctx, "/find/"+nconst, q, &payload); err != nil {
		return FoundPerson{}, err
	}
	if len(payload.People) == 0 {
		return FoundPerson{}, ErrNotFound
	}
	// One IMDb id maps to one person, as it does to one movie.
	found := payload.People[0]
	out := FoundPerson{ID: found.ID}
	if found.ProfilePath != nil && !found.Adult {
		out.Profile = strings.TrimSpace(*found.ProfilePath)
	}
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
