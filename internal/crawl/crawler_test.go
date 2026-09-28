package crawl

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"sort"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"cinedikt/internal/graph"
	"cinedikt/internal/omdb"
	"cinedikt/internal/tmdb"
)

// fakeRatings answers for tt1 only and fails for ttboom.
type fakeRatings struct{ calls atomic.Int32 }

func (f *fakeRatings) IMDbRating(_ context.Context, imdbID string) (omdb.Rating, error) {
	f.calls.Add(1)
	switch imdbID {
	case "tt1":
		return omdb.Rating{Value: 8.7, Votes: 100}, nil
	case "ttboom":
		return omdb.Rating{}, errors.New("quota")
	}
	return omdb.Rating{}, omdb.ErrNotFound
}

// fakeSource serves a small hand-built TMDb from memory and counts calls.
type fakeSource struct {
	movies  map[int]*tmdb.Movie
	people  map[int]*tmdb.Person
	credits map[int]*tmdb.MovieCredits

	mu    sync.Mutex
	calls map[string]int
}

func (f *fakeSource) count(key string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.calls == nil {
		f.calls = map[string]int{}
	}
	f.calls[key]++
}

func (f *fakeSource) Movie(_ context.Context, id int) (*tmdb.Movie, error) {
	f.count("movie")
	m, ok := f.movies[id]
	if !ok {
		return nil, tmdb.ErrNotFound
	}
	return m, nil
}

// Person returns the profile with the filmography appended, as the client does.
func (f *fakeSource) Person(_ context.Context, id int) (*tmdb.Person, error) {
	f.count("person")
	p, ok := f.people[id]
	if !ok {
		return nil, tmdb.ErrNotFound
	}
	cp := *p
	cp.MovieCredits = f.credits[id]
	return &cp, nil
}

// fakeWriter records what the crawler writes.
type fakeWriter struct {
	mu            sync.Mutex
	movieMeta     map[int]graph.Movie
	movies        map[int][]graph.CastEntry
	directors     map[int][]graph.Person
	filmographies map[int][]graph.FilmCredit
	failMovie     int // WriteMovieCast for this id fails
}

func newFakeWriter() *fakeWriter {
	return &fakeWriter{movieMeta: map[int]graph.Movie{}, movies: map[int][]graph.CastEntry{}, directors: map[int][]graph.Person{}, filmographies: map[int][]graph.FilmCredit{}}
}

func (w *fakeWriter) WriteMovieCast(_ context.Context, m graph.Movie, cast []graph.CastEntry, directors []graph.Person) error {
	w.mu.Lock()
	defer w.mu.Unlock()
	if m.ID == w.failMovie {
		return errors.New("boom")
	}
	w.movieMeta[m.ID] = m
	w.movies[m.ID] = cast
	w.directors[m.ID] = directors
	return nil
}

func (w *fakeWriter) WriteFilmography(_ context.Context, p graph.Person, credits []graph.FilmCredit) error {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.filmographies[p.ID] = credits
	return nil
}

func (w *fakeWriter) WriteIMDbRating(_ context.Context, movieID int, rating float64, votes int) error {
	w.mu.Lock()
	defer w.mu.Unlock()
	m := w.movieMeta[movieID]
	m.IMDbRating, m.IMDbVotes = rating, votes
	w.movieMeta[movieID] = m
	return nil
}

func (w *fakeWriter) writtenPeople() []int {
	w.mu.Lock()
	defer w.mu.Unlock()
	ids := make([]int, 0, len(w.filmographies))
	for id := range w.filmographies {
		ids = append(ids, id)
	}
	sort.Ints(ids)
	return ids
}

func (w *fakeWriter) writtenMovies() []int {
	w.mu.Lock()
	defer w.mu.Unlock()
	ids := make([]int, 0, len(w.movies))
	for id := range w.movies {
		ids = append(ids, id)
	}
	sort.Ints(ids)
	return ids
}

// Fixture: movie 1 (seed) stars 10 (popular), 11 (popular but crew), 12
// (unpopular), 13 (appears as Self), and is directed by 30. Person 10 also
// acted in movie 2 (well known) and 3 (obscure). Movie 2 stars 10 and 20
// (popular enough for depth 2). Person 30 also directed movie 5.
func fixture() *fakeSource {
	actor := func(id int, name string, pop float64, order int, dept string) tmdb.CastMember {
		return tmdb.CastMember{ID: id, Name: name, Character: "Role", Order: order, Popularity: pop, KnownForDepartment: dept}
	}
	director := tmdb.CrewMember{ID: 30, Name: "Director", Job: tmdb.JobDirector, Department: "Directing", Popularity: 5, KnownForDepartment: "Directing"}
	self := actor(13, "Doc Host", 50, 3, "Acting")
	self.Character = "Self"
	return &fakeSource{
		movies: map[int]*tmdb.Movie{
			1: {ID: 1, Title: "Seed", ReleaseDate: "1999-03-31", IMDbID: "tt1", Credits: &tmdb.Credits{
				Cast: []tmdb.CastMember{
					actor(10, "Lead", 40, 0, "Acting"),
					actor(11, "Director Cameo", 40, 1, "Directing"),
					actor(12, "Extra", 1, 2, "Acting"),
					self,
				},
				Crew: []tmdb.CrewMember{director, {ID: 30, Name: "Director", Job: "Writer", Department: "Writing"}},
			}},
			2: {ID: 2, Title: "Other", ReleaseDate: "2003-05-15", IMDbID: "ttboom", Credits: &tmdb.Credits{Cast: []tmdb.CastMember{
				actor(10, "Lead", 40, 0, "Acting"),
				actor(20, "Costar", 40, 1, "Acting"),
			}}},
			3: {ID: 3, Title: "Obscure", ReleaseDate: "2001-01-01", Credits: &tmdb.Credits{}},
			5: {ID: 5, Title: "Earlier", ReleaseDate: "1994-01-01", Credits: &tmdb.Credits{}},
		},
		people: map[int]*tmdb.Person{
			10: {ID: 10, Name: "Lead", Popularity: 40, KnownForDepartment: "Acting"},
			12: {ID: 12, Name: "Extra", Popularity: 1, KnownForDepartment: "Acting"},
			20: {ID: 20, Name: "Costar", Popularity: 40, KnownForDepartment: "Acting"},
			30: {ID: 30, Name: "Director", Popularity: 0.2, KnownForDepartment: "Directing"},
		},
		credits: map[int]*tmdb.MovieCredits{
			10: {ID: 10, Cast: []tmdb.MovieCredit{
				{ID: 1, Title: "Seed", ReleaseDate: "1999-03-31", Character: "Role", Order: 0, VoteCount: 1000},
				{ID: 2, Title: "Other", ReleaseDate: "2003-05-15", Character: "Role", Order: 0, VoteCount: 1000},
				{ID: 3, Title: "Obscure", ReleaseDate: "2001-01-01", Character: "Role", Order: 0, VoteCount: 2},
				{ID: 4, Title: "Making Of", ReleaseDate: "2000-01-01", Character: "Self", Order: 0, VoteCount: 1000},
			}},
			20: {ID: 20, Cast: []tmdb.MovieCredit{
				{ID: 2, Title: "Other", ReleaseDate: "2003-05-15", Character: "Role", Order: 1, VoteCount: 1000},
			}},
			30: {ID: 30, Crew: []tmdb.CrewCredit{
				{ID: 1, Title: "Seed", ReleaseDate: "1999-03-31", Job: tmdb.JobDirector, VoteCount: 1000},
				{ID: 5, Title: "Earlier", ReleaseDate: "1994-01-01", Job: tmdb.JobDirector, VoteCount: 800},
				{ID: 6, Title: "Student Film", ReleaseDate: "1988-01-01", Job: tmdb.JobDirector, VoteCount: 2},
				{ID: 1, Title: "Seed", ReleaseDate: "1999-03-31", Job: "Writer", VoteCount: 1000},
			}},
		},
	}
}

func newTestCrawler(src Source, w Writer) *Crawler {
	return New(src, w, Options{Concurrency: 2, Logger: slog.New(slog.NewTextHandler(io.Discard, nil))})
}

func TestRunDepth1(t *testing.T) {
	src := fixture()
	w := newFakeWriter()
	stats, err := newTestCrawler(src, w).Run(context.Background(), 1, 1)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}

	if got, want := w.writtenMovies(), []int{1}; !equal(got, want) {
		t.Errorf("movies written = %v, want %v", got, want)
	}
	// Everyone but the Self credit is written as cast, regardless of score.
	if got := len(w.movies[1]); got != 3 {
		t.Errorf("seed cast written = %d entries, want 3", got)
	}
	if got := len(w.directors[1]); got != 1 || w.directors[1][0].ID != 30 {
		t.Errorf("seed directors = %+v, want person 30", w.directors[1])
	}
	// The popular actor and the director both get a filmography. The
	// director's Writer credit is dropped; the student film is written
	// but does not seed the next level.
	if got, want := w.writtenPeople(), []int{10, 30}; !equal(got, want) {
		t.Errorf("filmographies written = %v, want %v", got, want)
	}
	if got := len(w.filmographies[10]); got != 3 {
		t.Errorf("filmography of 10 = %d credits, want 3", got)
	}
	if got := len(w.filmographies[30]); got != 3 {
		t.Errorf("filmography of 30 = %d credits, want 3 directed films", got)
	}
	if stats.MoviesFetched != 1 || stats.PeopleFetched != 2 || stats.PeopleSkipped != 0 {
		t.Errorf("stats = %+v", *stats)
	}
	if src.calls["person"] != 2 {
		t.Errorf("person fetches = %d, want 2", src.calls["person"])
	}
}

func TestRunDepth2ExpandsWellKnownMoviesOnly(t *testing.T) {
	src := fixture()
	w := newFakeWriter()
	if _, err := newTestCrawler(src, w).Run(context.Background(), 1, 2); err != nil {
		t.Fatalf("Run: %v", err)
	}
	// Movie 2 is fetched at depth 2 from the lead's filmography; movie 5
	// from the director's. The obscure movie 3 and the student film 6 are not.
	if got, want := w.writtenMovies(), []int{1, 2, 5}; !equal(got, want) {
		t.Errorf("movies written = %v, want %v", got, want)
	}
	// Person 20 (popularity 40, order 1) clears the depth-2 threshold of 1.5.
	if got, want := w.writtenPeople(), []int{10, 20, 30}; !equal(got, want) {
		t.Errorf("filmographies written = %v, want %v", got, want)
	}
	// Person 10 appears in both movies but is fetched once; 30 is the director.
	if src.calls["person"] != 3 {
		t.Errorf("person fetches = %d, want 3", src.calls["person"])
	}
}

func TestRunPhase2Skips(t *testing.T) {
	src := fixture()
	// Person 10 looks popular in the credits payload but their profile says otherwise.
	src.people[10] = &tmdb.Person{ID: 10, Name: "Lead", Popularity: 0.5, KnownForDepartment: "Acting"}
	w := newFakeWriter()
	stats, err := newTestCrawler(src, w).Run(context.Background(), 1, 1)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if len(w.writtenPeople()) != 1 || w.writtenPeople()[0] != 30 {
		t.Errorf("filmographies written = %v, want only the director", w.writtenPeople())
	}
	if stats.PeopleSkipped != 1 {
		t.Errorf("PeopleSkipped = %d, want 1", stats.PeopleSkipped)
	}
}

func TestRunMissingSeed(t *testing.T) {
	_, err := newTestCrawler(fixture(), newFakeWriter()).Run(context.Background(), 999, 1)
	if !errors.Is(err, tmdb.ErrNotFound) {
		t.Fatalf("Run(missing) error = %v, want ErrNotFound", err)
	}
}

func TestRunSurvivesItemFailures(t *testing.T) {
	src := fixture()
	delete(src.people, 10) // profile lookup fails
	w := newFakeWriter()
	stats, err := newTestCrawler(src, w).Run(context.Background(), 1, 1)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if stats.FetchErrors != 1 {
		t.Errorf("FetchErrors = %d, want 1", stats.FetchErrors)
	}
}

func TestWriteFailureAllowsRetry(t *testing.T) {
	src := fixture()
	w := newFakeWriter()
	w.failMovie = 1
	c := newTestCrawler(src, w)
	if _, err := c.Run(context.Background(), 1, 1); err == nil {
		t.Fatal("Run with failing writer: want error")
	}
	w.failMovie = 0
	if _, err := c.Run(context.Background(), 1, 1); err != nil {
		t.Fatalf("Run after writer recovered: %v", err)
	}
	if got, want := w.writtenMovies(), []int{1}; !equal(got, want) {
		t.Errorf("movies written = %v, want %v", got, want)
	}
}

func TestExpandMovie(t *testing.T) {
	src := fixture()
	w := newFakeWriter()
	c := newTestCrawler(src, w)

	if _, err := c.ExpandMovie(context.Background(), 2, 1); err != nil {
		t.Fatalf("ExpandMovie: %v", err)
	}
	if got, want := w.writtenPeople(), []int{10, 20}; !equal(got, want) {
		t.Errorf("after ExpandMovie, filmographies = %v, want %v", got, want)
	}

	// Expanding the same movie again does no further fetching: the
	// crawler remembers what it has just written.
	movieFetches, personFetches := src.calls["movie"], src.calls["person"]
	if _, err := c.ExpandMovie(context.Background(), 2, 1); err != nil {
		t.Fatalf("second ExpandMovie: %v", err)
	}
	if src.calls["movie"] != movieFetches || src.calls["person"] != personFetches {
		t.Errorf("second expand fetched again: movies %d->%d, people %d->%d",
			movieFetches, src.calls["movie"], personFetches, src.calls["person"])
	}
}

func TestExpandMovieRefetchesOnceMemoryExpires(t *testing.T) {
	src := fixture()
	w := newFakeWriter()
	c := New(src, w, Options{
		Concurrency: 2,
		MemoryTTL:   time.Millisecond,
		Logger:      slog.New(slog.NewTextHandler(io.Discard, nil)),
	})
	if _, err := c.ExpandMovie(context.Background(), 2, 1); err != nil {
		t.Fatal(err)
	}
	before := src.calls["movie"]
	time.Sleep(5 * time.Millisecond)
	if _, err := c.ExpandMovie(context.Background(), 2, 1); err != nil {
		t.Fatal(err)
	}
	if src.calls["movie"] <= before {
		t.Error("expired memory did not allow a refetch")
	}
}

func TestPanicInACrawlItemDoesNotEscape(t *testing.T) {
	src := fixture()
	w := newFakeWriter()
	c := newTestCrawler(src, w)
	// One id panics; the fan-out must survive it and keep the others.
	got := c.fanOut(context.Background(), []int{1, 2, 3}, func(_ context.Context, id int) ([]int, error) {
		if id == 2 {
			panic("malformed record")
		}
		return []int{id * 10}, nil
	})
	sort.Ints(got)
	if want := []int{10, 30}; !equal(got, want) {
		t.Errorf("fanOut = %v, want %v: the panicking id is dropped, the rest complete", got, want)
	}
}

func TestIMDbRatings(t *testing.T) {
	src := fixture()
	w := newFakeWriter()
	ratings := &fakeRatings{}
	c := New(src, w, Options{Concurrency: 2, Ratings: ratings, Logger: slog.New(slog.NewTextHandler(io.Discard, nil))})
	stats, err := c.Run(context.Background(), 1, 2)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if m := w.movieMeta[1]; m.IMDbRating != 8.7 || m.IMDbVotes != 100 {
		t.Errorf("seed written with %+v, want IMDb 8.7/100", m)
	}
	// A failed lookup still writes the movie, without a rating.
	if m, ok := w.movieMeta[2]; !ok || m.IMDbRating != 0 {
		t.Errorf("movie 2 written = %v, meta %+v; want written without rating", ok, m)
	}
	if stats.RatingErrors != 1 {
		t.Errorf("RatingErrors = %d, want 1", stats.RatingErrors)
	}
	// Only crawled movies (with an IMDb id) are looked up, never filmography entries.
	if ratings.calls.Load() != 2 {
		t.Errorf("rating lookups = %d, want 2", ratings.calls.Load())
	}
}

// unreadableRatings answers for tt1 and cannot read OMDb's answer for
// any other.
type unreadableRatings struct{}

func (unreadableRatings) IMDbRating(_ context.Context, imdbID string) (omdb.Rating, error) {
	if imdbID == "tt1" {
		return omdb.Rating{Value: 8.7, Votes: 100}, nil
	}
	return omdb.Rating{}, fmt.Errorf("%w: unexpected end of JSON input", omdb.ErrUnreadable)
}

// TestAnUnreadableRatingIsNoRating: OMDb's answer that cannot be read
// even once repaired is a movie with no rating to be had, like one OMDb
// has none for. The movie is written without one, and it is not counted
// as a failed lookup.
func TestAnUnreadableRatingIsNoRating(t *testing.T) {
	w := newFakeWriter()
	c := New(fixture(), w, Options{Concurrency: 2, Ratings: unreadableRatings{}, Logger: slog.New(slog.NewTextHandler(io.Discard, nil))})
	stats, err := c.Run(context.Background(), 1, 2)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if m, ok := w.movieMeta[2]; !ok || m.IMDbRating != 0 {
		t.Errorf("movie 2 written = %v, meta %+v; want written without rating", ok, m)
	}
	if stats.RatingErrors != 0 {
		t.Errorf("RatingErrors = %d, want 0", stats.RatingErrors)
	}
}

func TestNoRatingSourceIsFine(t *testing.T) {
	w := newFakeWriter()
	if _, err := newTestCrawler(fixture(), w).Run(context.Background(), 1, 1); err != nil {
		t.Fatal(err)
	}
	if w.movieMeta[1].IMDbRating != 0 {
		t.Errorf("rating without a source = %v", w.movieMeta[1].IMDbRating)
	}
}

func TestMaxPeoplePerMovie(t *testing.T) {
	src := fixture()
	// Movie 2 has two scorable people; cap at one.
	w := newFakeWriter()
	c := New(src, w, Options{Concurrency: 2, MaxPeoplePerMovie: 1, Logger: slog.New(slog.NewTextHandler(io.Discard, nil))})
	if _, err := c.ExpandMovie(context.Background(), 2, 1); err != nil {
		t.Fatal(err)
	}
	if got, want := w.writtenPeople(), []int{10}; !equal(got, want) {
		t.Errorf("filmographies written = %v, want only the top-billed %v", got, want)
	}
}

func TestDirectorIgnoresMaxPeoplePerMovie(t *testing.T) {
	src := fixture()
	w := newFakeWriter()
	c := New(src, w, Options{Concurrency: 2, MaxPeoplePerMovie: 1, Logger: slog.New(slog.NewTextHandler(io.Discard, nil))})
	if _, err := c.ExpandMovie(context.Background(), 1, 1); err != nil {
		t.Fatal(err)
	}
	// Cap keeps the lead; the director is extra.
	if got, want := w.writtenPeople(), []int{10, 30}; !equal(got, want) {
		t.Errorf("filmographies written = %v, want lead and director %v", got, want)
	}
}

// slowSource delays every person fetch until released, so a second caller
// can arrive while an expansion is in progress.
type slowSource struct {
	*fakeSource
	release chan struct{}
}

func (s *slowSource) Person(ctx context.Context, id int) (*tmdb.Person, error) {
	<-s.release
	return s.fakeSource.Person(ctx, id)
}

func TestConcurrentExpandWaitsForTheFirst(t *testing.T) {
	src := &slowSource{fakeSource: fixture(), release: make(chan struct{})}
	w := newFakeWriter()
	c := newTestCrawler(src, w)

	done := make(chan struct{})
	go func() {
		defer close(done)
		if _, err := c.ExpandMovie(context.Background(), 1, 1); err != nil {
			t.Error(err)
		}
	}()
	// Second caller for the same movie while the first is blocked on people.
	second := make(chan struct{})
	go func() {
		defer close(second)
		if _, err := c.ExpandMovie(context.Background(), 1, 1); err != nil {
			t.Error(err)
		}
		// By the time it returns, the filmographies must be written.
		if got := w.writtenPeople(); len(got) != 2 {
			t.Errorf("second caller returned before filmographies were written: %v", got)
		}
	}()
	select {
	case <-second:
		t.Fatal("second caller returned while the first expansion was still in progress")
	case <-time.After(50 * time.Millisecond):
	}
	close(src.release)
	<-done
	<-second
	if src.calls["movie"] != 1 {
		t.Errorf("movie fetched %d times, want 1", src.calls["movie"])
	}
}

func TestRunRejectsBadDepth(t *testing.T) {
	if _, err := newTestCrawler(fixture(), newFakeWriter()).Run(context.Background(), 1, 0); err == nil {
		t.Fatal("Run(depth 0): want error")
	}
}

func equal(a, b []int) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func TestTakeKeepsTheHeadForTheFirstScreen(t *testing.T) {
	first, rest := take([]int{1, 2, 3, 4, 5}, 2)
	if !equal(first, []int{1, 2}) || !equal(rest, []int{3, 4, 5}) {
		t.Fatalf("take = %v %v", first, rest)
	}
	all, none := take([]int{1, 2}, 4)
	if !equal(all, []int{1, 2}) || none != nil {
		t.Fatalf("short = %v %v", all, none)
	}
}
