package api

import (
	"context"
	"log/slog"
	"net/http"
	"time"

	"cinedikt/internal/catalog"
	"cinedikt/internal/imdbid"
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
	ids, ok := idsParam(w, r, MaxPhotoIDs, imdbid.Name, "ids must be IMDb name ids, such as nm0000206")
	if !ok {
		return
	}
	body := photosAnswer{Photos: make(map[string]*string, len(ids)), Pending: []string{}}
	for _, id := range ids {
		body.Photos[id] = nil
	}
	if s.photoStore != nil {
		rows, err := s.photoStore.PeoplePhotos(r.Context(), ids)
		if err != nil {
			s.failed(w, r, http.StatusInternalServerError, "could not read the photos", slog.LevelWarn, err, "people's photos")
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
