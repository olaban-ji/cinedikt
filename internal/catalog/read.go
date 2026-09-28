package catalog

import (
	"context"
	"crypto/sha1"
	"encoding/hex"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
)

// ErrNotFound is returned for an id the catalog cannot build a map from.
var ErrNotFound = errors.New("catalog: not found")

// Person is one chip: someone billed on the searched movie.
type Person struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	Role      string `json:"role"` // "cast" or "director"
	Character string `json:"character,omitempty"`
	Order     int    `json:"order"`
	// Photo is the address of the person's photo on TMDb's image host,
	// from the people job's answer. Left out while there is no answer
	// yet, when TMDb has no photo, and once the answer is past the 175
	// days anything of TMDb's is kept.
	Photo string `json:"photo,omitempty"`
}

// Movie is what a card says once its detail has arrived.
type Movie struct {
	ID       string   `json:"id"`
	Title    string   `json:"title"`
	Year     int      `json:"year"`
	Rating   *float64 `json:"rating"`
	MD       int      `json:"md"`
	Poster   string   `json:"poster,omitempty"`
	Released string   `json:"released,omitempty"`
	// Synopsis is what the film is about, from OMDb or, where OMDb has
	// none, TMDb. Empty when neither has one yet.
	Synopsis string `json:"synopsis,omitempty"`
	// Genres are IMDb's, up to three, in the order IMDb lists them.
	// Never Documentary: those films are left off maps.
	Genres   []string `json:"genres,omitempty"`
	People   []string `json:"people"`
	IsAnchor bool     `json:"isAnchor"`
}

// Grid is a whole map: the searched movie, its people, and the spine.
type Grid struct {
	Anchor Movie    `json:"anchor"`
	People []Person `json:"people"`
	Films  [][5]any `json:"films"`
	// OGVersion is the stamp on this movie's share image. The client
	// fetches that address as the map opens, so the picture exists
	// before anyone copies the link — Slack, iMessage and X all cache
	// the first thing they are given.
	OGVersion string `json:"og_v"`
}

// gridFilm is the film test, written once and used by every query that
// asks what belongs on a map. A documentary is a movie and an adult
// title is a movie, so both are stored; neither is mapped.
//
// A movie with no release date yet — one the poster job has not reached
// — is placed on its IMDb year. Holding it back would empty most of the
// map until the backfill finished.
const gridFilm = `
	NOT t.is_adult
	AND t.start_year IS NOT NULL
	AND NOT (t.genres @> ARRAY['Documentary'])
	AND (p.released IS NULL OR p.released <= current_date)
	AND t.start_year <= EXTRACT(year FROM current_date)`

// ReadTimeout bounds one read. A map is two indexed joins; anything
// slower than this is a problem to see rather than to wait through.
const ReadTimeout = 5 * time.Second

// Grid builds a whole map for one movie, live from the tables. There is
// no precomputed spine: the two joins below are indexed, and reading
// them means a poster learned an hour ago shows up now rather than
// after the next import.
func (s *Store) Grid(ctx context.Context, tconst string) (*Grid, error) {
	ctx, cancel := context.WithTimeout(ctx, ReadTimeout)
	defer cancel()

	anchor, asked, err := s.movie(ctx, tconst)
	if err != nil {
		return nil, err
	}
	anchor.IsAnchor = true
	// The searched film's panel is the one most often opened, so a
	// synopsis OMDb has not been asked for is worth asking for now.
	if !asked {
		s.wantSynopsis(tconst)
	}

	people, unasked, err := s.peopleOn(ctx, tconst)
	if err != nil {
		return nil, err
	}
	if len(people) == 0 {
		return nil, fmt.Errorf("catalog: %s has nobody billed: %w", tconst, ErrNotFound)
	}
	// The chip row is where a person's photo is drawn, so the people on
	// it the people job has no answer for, or one that has come due, go
	// to the front of its queue. Only a map being opened marks them:
	// Films reads the same row for every batch of cards a reader
	// scrolls to, and one mark a map is all the job needs.
	s.WantPeople(unasked...)
	// The anchor is a card like any other, and its card draws its
	// people; a null there would be "not known yet" rather than "all of
	// them", which is the opposite of the truth for this one.
	anchor.People = make([]string, len(people))
	for i, p := range people {
		anchor.People[i] = p.ID
	}

	films, err := s.spine(ctx, tconst, people)
	if err != nil {
		return nil, err
	}
	return &Grid{
		Anchor:    anchor,
		People:    people,
		Films:     films,
		OGVersion: OGVersion(anchor.Poster, anchor.Title),
	}, nil
}

// movie reads one row, with its poster, date and synopsis. asked is
// whether OMDb has answered for it, which is not the same as having a
// synopsis: nobody may have one, and TMDb's may be showing while OMDb,
// the better source, is still to be asked.
func (s *Store) movie(ctx context.Context, tconst string) (m Movie, asked bool, err error) {
	var released *time.Time
	var poster, synopsis *string
	err = s.pool.QueryRow(ctx, `
		SELECT t.tconst, t.primary_title, coalesce(t.start_year, 0), t.genres,
		       r.average_rating, p.poster_url, p.released,
		       sy.overview, sy.omdb_at IS NOT NULL
		FROM `+Live+`.titles t
		LEFT JOIN `+Live+`.ratings r USING (tconst)
		LEFT JOIN meta.posters p USING (tconst)
		LEFT JOIN meta.synopses sy USING (tconst)
		WHERE t.tconst = $1`, tconst).
		Scan(&m.ID, &m.Title, &m.Year, &m.Genres, &m.Rating, &poster, &released, &synopsis, &asked)
	if errors.Is(err, pgx.ErrNoRows) {
		return Movie{}, false, fmt.Errorf("catalog: %s: %w", tconst, ErrNotFound)
	}
	if err != nil {
		return Movie{}, false, fmt.Errorf("catalog: read %s: %w", tconst, err)
	}
	if poster != nil {
		m.Poster = *poster
	}
	if released != nil {
		m.Released = released.Format("2006-01-02")
		m.MD = int(released.Month())*100 + released.Day()
	}
	if synopsis != nil {
		m.Synopsis = *synopsis
	}
	return m, asked, nil
}

// peopleOn is the chip row: billed cast, then directors. A person who
// both acted and directed is shown as a director, because that is the
// larger claim on the film.
//
// Each carries their photo, when the people job has one to show.
// unasked is the people it has no answer for, or one that has come due.
// Only Grid marks them; the other reads of the chip row leave them be.
func (s *Store) peopleOn(ctx context.Context, tconst string) (people []Person, unasked []string, err error) {
	rows, err := s.pool.Query(ctx, `
		WITH credited AS (
		    SELECT pr.nconst,
		           CASE WHEN bool_or(pr.category = 'director') THEN 'director' ELSE 'cast' END AS role,
		           min(pr.ordering) AS ord,
		           (array_agg(pr.character ORDER BY pr.ordering)
		             FILTER (WHERE pr.character IS NOT NULL))[1] AS character
		    FROM `+Live+`.principals pr
		    WHERE pr.tconst = $1
		    GROUP BY pr.nconst
		    UNION ALL
		    SELECT d.nconst, 'director', -1000 + d.ordering, NULL
		    FROM `+Live+`.directors d
		    WHERE d.tconst = $1
		), chips AS (
		    SELECT c.nconst,
		           coalesce(n.primary_name, '') AS name,
		           CASE WHEN bool_or(c.role = 'director') THEN 'director' ELSE 'cast' END AS role,
		           min(c.ord) AS ord,
		           (array_agg(c.character) FILTER (WHERE c.character IS NOT NULL))[1] AS character
		    FROM credited c
		    JOIN `+Live+`.names n USING (nconst)
		    GROUP BY c.nconst, n.primary_name
		)
		SELECT ch.nconst, ch.name, ch.role, ch.ord, ch.character,
		       ph.profile_path,
		       ph.nconst IS NULL OR `+photoDue("$3")+`
		FROM chips ch
		LEFT JOIN meta.people ph ON ph.nconst = ch.nconst AND `+photoServed("$2")+`
		ORDER BY ch.ord`, tconst, tmdbForgetDays, tmdbRefreshDays)
	if err != nil {
		return nil, nil, fmt.Errorf("catalog: people on %s: %w", tconst, err)
	}
	defer rows.Close()

	for rows.Next() {
		var p Person
		var character, photo *string
		var ask bool
		if err := rows.Scan(&p.ID, &p.Name, &p.Role, &p.Order, &character, &photo, &ask); err != nil {
			return nil, nil, fmt.Errorf("catalog: scan person: %w", err)
		}
		if character != nil {
			p.Character = *character
		}
		p.Photo = photoURL(photo)
		if ask {
			unasked = append(unasked, p.ID)
		}
		p.Order = len(people)
		people = append(people, p)
	}
	return people, unasked, rows.Err()
}

// MaxSpine bounds one map. A career is wider than a screen, and a
// thousand cards is a map nobody can read; the most voted survive.
const MaxSpine = 400

// spine is every movie those people made, as the five facts that place
// a card and say whose it is: id, year, rating, month-day, and which of
// the searched movie's people are on it. The whole set comes back at
// once, so a card's place is final from the first paint.
//
// The people are indexes into the chip row rather than name ids. There
// are up to four hundred films and several dozen people, so an id on
// every row would be most of the payload; an index is a byte or two and
// the client already has the row to look it up in.
//
// They are there so the client can judge a card it has not fetched
// detail for. Hiding the empty years means deciding whether anything in
// a year is lit, and a year the reader has never scrolled to has no
// detail at all — so without this the map would collapse rows as they
// came into view.
func (s *Store) spine(ctx context.Context, anchor string, people []Person) ([][5]any, error) {
	ids := make([]string, len(people))
	at := make(map[string]int, len(people))
	for i, p := range people {
		ids[i] = p.ID
		at[p.ID] = i
	}
	rows, err := s.pool.Query(ctx, `
		WITH credits AS (
		    SELECT pr.tconst, pr.nconst
		    FROM `+Live+`.principals pr
		    WHERE pr.nconst = ANY($1) AND pr.category IN ('actor','actress','director')
		    UNION
		    SELECT d.tconst, d.nconst
		    FROM `+Live+`.directors d
		    WHERE d.nconst = ANY($1)
		), theirs AS (
		    SELECT tconst, array_agg(nconst) AS people
		    FROM credits
		    GROUP BY tconst
		)
		SELECT t.tconst, t.start_year, r.average_rating, p.released, theirs.people
		FROM theirs
		JOIN `+Live+`.titles t USING (tconst)
		LEFT JOIN `+Live+`.ratings r USING (tconst)
		LEFT JOIN meta.posters p USING (tconst)
		WHERE `+gridFilm+`
		ORDER BY (t.tconst = $2) DESC, coalesce(r.num_votes, 0) DESC
		LIMIT $3`, ids, anchor, MaxSpine)
	if err != nil {
		return nil, fmt.Errorf("catalog: spine for %s: %w", anchor, err)
	}
	defer rows.Close()

	films := make([][5]any, 0, 256)
	for rows.Next() {
		var id string
		var year int
		var rating *float64
		var released *time.Time
		var whose []string
		if err := rows.Scan(&id, &year, &rating, &released, &whose); err != nil {
			return nil, fmt.Errorf("catalog: scan spine row: %w", err)
		}
		md := 0
		if released != nil {
			md = int(released.Month())*100 + released.Day()
		}
		// A typed nil inside an `any` is not nil, and this tuple is
		// read as much by Go as by the client. Unrated is a plain nil.
		var score any
		if rating != nil {
			score = *rating
		}
		films = append(films, [5]any{id, year, score, md, indexesOf(whose, at)})
	}
	return films, rows.Err()
}

// indexesOf turns the name ids on a film into places in the chip row,
// in that row's own order. Never nil: a null there would read as "not
// known yet" rather than "nobody", and every film on the spine is on it
// because somebody from the chip row made it.
func indexesOf(whose []string, at map[string]int) []int {
	out := make([]int, 0, len(whose))
	for _, id := range whose {
		if i, ok := at[id]; ok {
			out = append(out, i)
		}
	}
	sort.Ints(out)
	return out
}

// Films is what the cards on screen say, asked for by id.
func (s *Store) Films(ctx context.Context, anchor string, ids []string) ([]Movie, error) {
	ctx, cancel := context.WithTimeout(ctx, ReadTimeout)
	defer cancel()

	rows, err := s.pool.Query(ctx, `
		SELECT t.tconst, t.primary_title, coalesce(t.start_year, 0), t.genres,
		       r.average_rating, p.poster_url, p.released,
		       sy.overview, sy.omdb_at IS NOT NULL,
		       coalesce(
		           (SELECT array_agg(DISTINCT who.nconst)
		            FROM (
		                SELECT pr.nconst FROM `+Live+`.principals pr
		                WHERE pr.tconst = t.tconst AND pr.nconst = ANY($2)
		                UNION
		                SELECT d.nconst FROM `+Live+`.directors d
		                WHERE d.tconst = t.tconst AND d.nconst = ANY($2)
		            ) who),
		           '{}') AS people
		FROM `+Live+`.titles t
		LEFT JOIN `+Live+`.ratings r USING (tconst)
		LEFT JOIN meta.posters p USING (tconst)
		LEFT JOIN meta.synopses sy USING (tconst)
		WHERE t.tconst = ANY($1)`, ids, s.anchorPeople(ctx, anchor))
	if err != nil {
		return nil, fmt.Errorf("catalog: films: %w", err)
	}
	defer rows.Close()

	out := make([]Movie, 0, len(ids))
	var unasked []string
	for rows.Next() {
		var m Movie
		var poster, synopsis *string
		var released *time.Time
		var asked bool
		if err := rows.Scan(&m.ID, &m.Title, &m.Year, &m.Genres, &m.Rating, &poster, &released, &synopsis, &asked, &m.People); err != nil {
			return nil, fmt.Errorf("catalog: scan film: %w", err)
		}
		if synopsis != nil {
			m.Synopsis = *synopsis
		}
		if !asked {
			unasked = append(unasked, m.ID)
		}
		if m.People == nil {
			m.People = []string{}
		}
		if poster != nil {
			m.Poster = *poster
		}
		if released != nil {
			m.Released = released.Format("2006-01-02")
			m.MD = int(released.Month())*100 + released.Day()
		}
		m.IsAnchor = m.ID == anchor
		out = append(out, m)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	// These are the cards a reader is looking at right now, so the ones
	// among them with nothing to show are the only ones worth another
	// service's time. The mark is dropped into a buffer and written
	// elsewhere; nothing here waits on it.
	var blank []string
	for _, m := range out {
		if strings.TrimSpace(m.Poster) == "" {
			blank = append(blank, m.ID)
		}
	}
	s.wantPoster(blank...)
	// And the ones OMDb has not answered for go to the front of the
	// synopsis job's queue, the same way.
	s.wantSynopsis(unasked...)
	return out, nil
}

// anchorPeople is the ids of the searched movie's people, which is what
// a card's markers are drawn from.
func (s *Store) anchorPeople(ctx context.Context, tconst string) []string {
	people, _, err := s.peopleOn(ctx, tconst)
	if err != nil {
		return nil
	}
	ids := make([]string, len(people))
	for i, p := range people {
		ids[i] = p.ID
	}
	return ids
}

// MovieMeta is the three facts a link preview needs: what the movie is
// called, when it came out, and the picture to draw. One indexed read,
// under the caller's own deadline — a scraper that waited is no better
// than one that got the generic card.
//
// It is deliberately not Grid(): a preview has no use for the cast, the
// spine or anything else that makes a map, and paying for them would
// put a scraper's budget into work nobody reads.
func (s *Store) MovieMeta(ctx context.Context, tconst string) (string, int, string, error) {
	var title string
	var year int
	var poster *string
	err := s.pool.QueryRow(ctx, `
		SELECT t.primary_title, coalesce(t.start_year, 0), p.poster_url
		FROM `+Live+`.titles t
		LEFT JOIN meta.posters p USING (tconst)
		WHERE t.tconst = $1`, tconst).Scan(&title, &year, &poster)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", 0, "", fmt.Errorf("catalog: %s: %w", tconst, ErrNotFound)
	}
	if err != nil {
		return "", 0, "", fmt.Errorf("catalog: movie meta %s: %w", tconst, err)
	}
	if poster == nil {
		return title, year, "", nil
	}
	return title, year, *poster, nil
}

// OGTemplateVersion changes when the share image's design does, so a
// new layout reaches unfurlers that are still holding the old one.
// It must move in step with the renderer in cmd/api/og.go.
const OGTemplateVersion = "2"

// OGVersion is the cache key for a movie's share image: the stamp that
// changes whenever the picture would. It goes in the URL, so a client
// that caches per address picks up a new poster without being told.
//
// Short on purpose. It is not a checksum anyone verifies — it only has
// to differ when the inputs do, and eight hex characters in a URL is a
// stamp rather than a hash to read.
func OGVersion(posterURL, title string) string {
	sum := sha1.Sum([]byte(posterURL + "\x00" + title + "\x00" + OGTemplateVersion))
	return hex.EncodeToString(sum[:])[:8]
}

// OGImage is a share image already rendered, or nil when this version
// has never been made. The read is one primary-key lookup, which is
// what every hit after the first costs.
func (s *Store) OGImage(ctx context.Context, tconst, v string) ([]byte, error) {
	var png []byte
	err := s.pool.QueryRow(ctx, `
		SELECT png FROM meta.og_images WHERE tconst = $1 AND v = $2`, tconst, v).Scan(&png)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("catalog: og image %s: %w", tconst, err)
	}
	return png, nil
}

// PutOGImage keeps a rendered share image. Two requests for the same
// cold movie arrive together often enough — a link pasted into a busy
// channel is fetched by every client at once — and the second one has
// rendered something identical, so it is dropped rather than fought
// over.
func (s *Store) PutOGImage(ctx context.Context, tconst, v string, png []byte) error {
	_, err := s.pool.Exec(ctx, `
		INSERT INTO meta.og_images (tconst, v, png) VALUES ($1, $2, $3)
		ON CONFLICT DO NOTHING`, tconst, v, png)
	if err != nil {
		return fmt.Errorf("catalog: store og image %s: %w", tconst, err)
	}
	return nil
}
