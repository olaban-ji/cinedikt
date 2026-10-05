package api

import (
	"context"
	"log/slog"
	"net/http"
	"time"

	"cinedikt/internal/catalog"
	"cinedikt/internal/imdbid"
)

// TrailerStore is where the trailer job's answers are read, and where a
// film a reader has opened is marked for the job to ask about.
type TrailerStore interface {
	Trailer(ctx context.Context, tconst string) (catalog.TrailerRow, error)
	// WantTrailer never blocks and never fails: the mark is written, and
	// the job woken, behind the answer.
	WantTrailer(tconst string)
}

// WithTrailers answers GET /trailers/{id} from store. looked says
// whether anything is looking trailers up, which is to say whether there
// are TMDb credentials for the trailer job: without them a film nobody
// has asked about is "no trailer", rather than an answer on its way that
// will never come.
func (s *CatalogServer) WithTrailers(store TrailerStore, looked bool) {
	s.trailerStore = store
	s.trailersLooked = looked
}

// trailerAnswer is the body: a YouTube key, or null for none. Pending is
// null that is not an answer yet: the job has been asked, and asking
// again in a few seconds may find one.
type trailerAnswer struct {
	Key     *string `json:"key"`
	Pending bool    `json:"pending,omitempty"`
}

// trailerFor is GET /trailers/{id}: the YouTube trailer a film plays in
// place, or null when there is none that can be embedded.
//
// It only reads. The trailer job does every lookup, so a reader never
// waits on TMDb or YouTube. A stored answer is served as it stands, and
// one that has come due is also marked, so the job asks again. A film
// with no answer yet is marked the same way, which wakes the job and
// puts the film at the head of its queue, and the reader is told the
// answer is pending.
func (s *CatalogServer) trailerFor(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if !imdbid.Title(id) {
		writeError(w, http.StatusBadRequest, "id must be an IMDb title id, such as tt0133093")
		return
	}
	var body trailerAnswer
	if s.trailerStore != nil {
		row, err := s.trailerStore.Trailer(r.Context(), id)
		if err != nil {
			s.failed(w, r, http.StatusInternalServerError, "could not read the trailer", slog.LevelWarn, err, "trailer", "id", id)
			return
		}
		switch {
		case !row.Title:
			// Not a film this catalog holds: nothing to play, and
			// nothing worth a mark.
		case row.Asked:
			if row.Key != "" {
				body.Key = &row.Key
			}
			if s.trailersLooked && row.Due(time.Now()) {
				s.trailerStore.WantTrailer(id)
			}
		case s.trailersLooked:
			s.trailerStore.WantTrailer(id)
			body.Pending = true
		}
	}
	noStore(w)
	writeJSON(w, http.StatusOK, body)
}
