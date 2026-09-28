package api

import (
	"context"
	"net/http"
	"strings"
	"time"

	"cinedikt/internal/catalog"
)

// PhotoStore is where the people job's answers are read, and where the
// people a reader is waiting on are marked for the job to ask about.
type PhotoStore interface {
	PeoplePhotos(ctx context.Context, nconsts []string) (map[string]catalog.PhotoRow, error)
	// WantPeople never blocks and never fails: the mark is written, and
	// the job woken, behind the answer.
	WantPeople(nconsts ...string)
}

// WithPeoplePhotos answers GET /people/photos from store. looked says
// whether anything is looking photos up, which is to say whether there
// are TMDb credentials for the people job: without them a person nobody
// has asked about has no photo, rather than one on its way that will
// never come.
func (s *CatalogServer) WithPeoplePhotos(store PhotoStore, looked bool) {
	s.photoStore = store
	s.photosLooked = looked
}

// MaxPhotoIDs bounds one photos request: more than a chip row holds, and
// few enough that one request is one small query.
const MaxPhotoIDs = 50

// photosAnswer is the body. Photos has every person asked about: the
// photo's address, or null for none. Pending is the people whose null is
// not an answer yet: the job has been asked, and asking again in a few
// seconds may find a photo.
type photosAnswer struct {
	Photos  map[string]*string `json:"photos"`
	Pending []string           `json:"pending"`
}

// peoplePhotos is GET /people/photos?ids=nm…,nm…: the photos of the
// people on a map, for the ones the map's own payload came without.
//
// It only reads. The people job does every lookup, so a reader never
// waits on TMDb. A stored answer is served as it stands, and one that has
// come due is also marked, so the job asks again. A person with no answer
// yet is marked the same way, which wakes the job and puts them at the
// head of its queue, and the reader is told the answer is pending.
func (s *CatalogServer) peoplePhotos(w http.ResponseWriter, r *http.Request) {
	raw := r.URL.Query().Get("ids")
	if raw == "" {
		writeError(w, http.StatusBadRequest, "ids is required")
		return
	}
	asked := strings.Split(raw, ",")
	if len(asked) > MaxPhotoIDs {
		writeError(w, http.StatusBadRequest, "too many ids")
		return
	}
	var ids []string
	seen := make(map[string]bool, len(asked))
	for _, id := range asked {
		if !validNConst(id) {
			writeError(w, http.StatusBadRequest, "ids must be IMDb name ids, such as nm0000206")
			return
		}
		if !seen[id] {
			seen[id] = true
			ids = append(ids, id)
		}
	}
	body := photosAnswer{Photos: make(map[string]*string, len(ids)), Pending: []string{}}
	for _, id := range ids {
		body.Photos[id] = nil
	}
	if s.photoStore != nil {
		rows, err := s.photoStore.PeoplePhotos(r.Context(), ids)
		if err != nil {
			if gone(r) {
				return
			}
			if s.Logger != nil {
				s.Logger.Warn("people's photos", "err", err)
			}
			writeError(w, http.StatusInternalServerError, "could not read the photos")
			return
		}
		now := time.Now()
		var marks []string
		for _, id := range ids {
			row := rows[id]
			switch {
			case !row.Person:
				// Not a person this catalog holds: no photo, and
				// nothing worth a mark.
			case row.Asked:
				if row.Photo != "" {
					photo := row.Photo
					body.Photos[id] = &photo
				}
				if s.photosLooked && row.Due(now) {
					marks = append(marks, id)
				}
			case s.photosLooked:
				marks = append(marks, id)
				body.Pending = append(body.Pending, id)
			}
		}
		if len(marks) > 0 {
			s.photoStore.WantPeople(marks...)
		}
	}
	noStore(w)
	writeJSON(w, http.StatusOK, body)
}

// validNConst is IMDb's name id: "nm" and digits, the way validTConst is
// its title id.
func validNConst(id string) bool {
	if len(id) < 3 || len(id) > 20 || !strings.HasPrefix(id, "nm") {
		return false
	}
	for i := 2; i < len(id); i++ {
		if id[i] < '0' || id[i] > '9' {
			return false
		}
	}
	return true
}
