package catalog

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"golang.org/x/time/rate"

	"cinedikt/internal/config"
	"cinedikt/internal/notify"
	"cinedikt/internal/tmdb"
)

// The fixture's people, by the votes of their best known film: Shawshank
// for the first three, The Matrix for the rest.
const (
	freeman   = "nm0000151"
	robbins   = "nm0000209"
	darabont  = "nm0001104"
	keanu     = "nm0000206"
	moss      = "nm0000401"
	lilly     = "nm0905152"
	lana      = "nm0905154"
	fishburne = "nm0915989"
)

// sweepOrder is everyone in the fixture, the way the sweep takes them:
// most votes first, and by id between equals.
var sweepOrder = []string{freeman, robbins, darabont, keanu, moss, lilly, lana, fishburne}

// fakeFaces is TMDb for the people job. A person in photos is one TMDb
// has, with that photo path, empty for none; anybody else is not a
// person TMDb has. It records what it was asked, in order.
type fakeFaces struct {
	mu     sync.Mutex
	photos map[string]string
	err    map[string]error
	asked  []string
	// onFind, when set, runs as each person is asked about, before the
	// answer, outside the lock.
	onFind func(nconst string)
}

func (f *fakeFaces) FindPersonByIMDb(_ context.Context, nconst string) (tmdb.FoundPerson, error) {
	f.mu.Lock()
	f.asked = append(f.asked, nconst)
	hook := f.onFind
	f.mu.Unlock()
	if hook != nil {
		hook(nconst)
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if err := f.err[nconst]; err != nil {
		return tmdb.FoundPerson{}, err
	}
	path, ok := f.photos[nconst]
	if !ok {
		return tmdb.FoundPerson{}, tmdb.ErrNotFound
	}
	return tmdb.FoundPerson{ID: 1, Profile: path}, nil
}

func (f *fakeFaces) seen() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.asked...)
}

// peopleFixture publishes the fixture catalog with nothing known about
// anybody's photo, then runs the setup statements.
func peopleFixture(t *testing.T, s *Store, stmts ...string) {
	t.Helper()
	publishFixture(t, s)
	for _, stmt := range append([]string{
		`DELETE FROM meta.people`,
		`DELETE FROM meta.people_queue`,
		`DELETE FROM meta.posters`,
	}, stmts...) {
		if _, err := s.pool.Exec(context.Background(), stmt); err != nil {
			t.Fatal(err)
		}
	}
}

// personRow reads what meta.people says of a person: ok is false when
// TMDb was never asked, and path is empty for "no photo".
func personRow(t *testing.T, s *Store, nconst string) (path string, ok bool) {
	t.Helper()
	var p *string
	err := s.pool.QueryRow(context.Background(),
		`SELECT profile_path FROM meta.people WHERE nconst = $1`, nconst).Scan(&p)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", false
	}
	if err != nil {
		t.Fatal(err)
	}
	if p != nil {
		path = *p
	}
	return path, true
}

// wantedPeople is who the queue holds as wanted, by id, with when.
func wantedPeople(t *testing.T, s *Store) map[string]time.Time {
	t.Helper()
	rows, err := s.pool.Query(context.Background(),
		`SELECT nconst, wanted_at FROM meta.people_queue WHERE wanted_at IS NOT NULL`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	out := map[string]time.Time{}
	for rows.Next() {
		var id string
		var at time.Time
		if err := rows.Scan(&id, &at); err != nil {
			t.Fatal(err)
		}
		out[id] = at
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	return out
}

func mark(t *testing.T, s *Store, ids ...string) {
	t.Helper()
	if _, err := s.markPeopleWanted(context.Background(), ids); err != nil {
		t.Fatal(err)
	}
}

// TestThePeopleJobServesWantedPeopleFirst: the people on maps readers
// have opened go before the sweep, newest mark first, and then the sweep
// takes everyone else, best known first. A mark for somebody the catalog
// does not hold queues nothing.
func TestThePeopleJobServesWantedPeopleFirst(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()
	peopleFixture(t, s)
	mark(t, s, lana)
	mark(t, s, moss)
	mark(t, s, "nm7777777")
	if _, ok := wantedPeople(t, s)["nm7777777"]; ok {
		t.Error("a person the catalog does not hold was queued")
	}
	faces := &fakeFaces{photos: map[string]string{
		moss: "/moss.jpg", lana: "", keanu: "/keanu.jpg", freeman: "/freeman.jpg",
	}}
	var sink recordingSink
	job := &PersonPhotoJob{Store: s, TMDb: faces, Logger: quietLogger(), MinVotes: 1_000_000, Batch: 10, Notify: &sink}
	if err := job.Run(ctx); err != nil {
		t.Fatal(err)
	}
	want := []string{moss, lana, freeman, robbins, darabont, keanu, lilly, fishburne}
	if got := faces.seen(); !reflect.DeepEqual(got, want) {
		t.Fatalf("asked %v, want the wanted people, newest first, then the sweep: %v", got, want)
	}
	// A photo, TMDb's "none", and "no such person" are all answers.
	for id, want := range map[string]string{moss: "/moss.jpg", keanu: "/keanu.jpg", lana: "", robbins: ""} {
		if got, ok := personRow(t, s, id); !ok || got != want {
			t.Errorf("%s = %q (row %v), want %q", id, got, ok, want)
		}
	}
	// An answer takes the person out of the queue, want and all.
	var left int
	if err := s.pool.QueryRow(ctx, `SELECT count(*) FROM meta.people_queue`).Scan(&left); err != nil {
		t.Fatal(err)
	}
	if left != 0 {
		t.Errorf("%d people still queued after every one was answered", left)
	}
	if fin := sink.of(notify.Finished); len(fin) != 1 || fin[0].Job != notify.JobPeople || fin[0].Done != 3 || fin[0].None != 5 {
		t.Errorf("finished = %+v, want 3 found and 5 without a photo", fin)
	}

	// Every answer is fresh, so a second pass asks nothing.
	if err := job.Run(ctx); err != nil {
		t.Fatal(err)
	}
	if got := faces.seen(); len(got) != len(want) {
		t.Errorf("caught up, but asked %v", got[len(want):])
	}
}

// TestThePeopleSweepReachesEveryoneAMapCanShow: at the default floor of
// zero the sweep takes a person whose best known film has five votes;
// at a floor of ten it does not. A person with no film a map can show,
// only a documentary or an adult title, is never swept.
func TestThePeopleSweepReachesEveryoneAMapCanShow(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()
	peopleFixture(t, s,
		`INSERT INTO `+Live+`.titles (tconst, primary_title, original_title, is_adult, start_year)
		 VALUES ('tt9800001', 'A Small Film', 'A Small Film', false, 2001)`,
		`INSERT INTO `+Live+`.ratings (tconst, average_rating, num_votes) VALUES ('tt9800001', 6.1, 5)`,
		`INSERT INTO `+Live+`.principals (tconst, ordering, nconst, category) VALUES
		    ('tt9800001', 1, 'nm9800001', 'actress'),
		    ('tt0000002', 1, 'nm9800002', 'actor'),
		    ('tt0000003', 1, 'nm9800003', 'actor')`,
		`INSERT INTO `+Live+`.names (nconst, primary_name) VALUES
		    ('nm9800001', 'Five Votes'), ('nm9800002', 'Documentary Only'), ('nm9800003', 'Adult Only')`,
	)
	t.Cleanup(func() {
		for _, stmt := range []string{
			`DELETE FROM ` + Live + `.titles WHERE tconst = 'tt9800001'`,
			`DELETE FROM ` + Live + `.ratings WHERE tconst = 'tt9800001'`,
			`DELETE FROM ` + Live + `.principals WHERE nconst LIKE 'nm98%'`,
			`DELETE FROM ` + Live + `.names WHERE nconst LIKE 'nm98%'`,
			`DELETE FROM meta.people WHERE nconst LIKE 'nm98%'`,
			`DELETE FROM meta.people_queue WHERE nconst LIKE 'nm98%'`,
		} {
			_, _ = s.pool.Exec(context.Background(), stmt)
		}
	})
	faces := &fakeFaces{photos: map[string]string{"nm9800001": "/five.jpg"}}

	job := &PersonPhotoJob{Store: s, TMDb: faces, Logger: quietLogger(), MinVotes: 10}
	if err := job.Run(ctx); err != nil {
		t.Fatal(err)
	}
	if got := faces.seen(); !reflect.DeepEqual(got, sweepOrder) {
		t.Fatalf("at a floor of 10, asked %v, want %v", got, sweepOrder)
	}

	job = &PersonPhotoJob{Store: s, TMDb: faces, Logger: quietLogger(), MinVotes: config.DefaultPeopleSweepMinVotes}
	if err := job.Run(ctx); err != nil {
		t.Fatal(err)
	}
	if got := faces.seen()[len(sweepOrder):]; !reflect.DeepEqual(got, []string{"nm9800001"}) {
		t.Errorf("at the default floor, asked %v, want only the five-vote person", got)
	}
	if got, ok := personRow(t, s, "nm9800001"); !ok || got != "/five.jpg" {
		t.Errorf("five votes = %q (row %v)", got, ok)
	}
	for _, id := range []string{"nm9800002", "nm9800003"} {
		if _, ok := personRow(t, s, id); ok {
			t.Errorf("%s, who has no film a map can show, was swept", id)
		}
	}
}

// TestTheSweepPaceHoldsBackTheSweepButNotReaders: the sweep waits on its
// own pace; the people on a map a reader has opened do not, and a mark
// that arrives while the sweep is waiting is served at once.
func TestTheSweepPaceHoldsBackTheSweepButNotReaders(t *testing.T) {
	s := testStore(t)
	peopleFixture(t, s)
	mark(t, s, keanu)
	mark(t, s, moss)

	// One lookup an hour, and this hour's already spent.
	pace := rate.NewLimiter(rate.Every(time.Hour), 1)
	pace.Allow()
	wanted := make(chan struct{}, 1)
	faces := &fakeFaces{}
	go func() {
		// Once both wanted people are answered the pass has nobody but
		// the sweep left, and waits on its pace. A reader opens a map
		// with Lana on it.
		for deadline := time.Now().Add(time.Second); len(faces.seen()) < 2 && time.Now().Before(deadline); {
			time.Sleep(10 * time.Millisecond)
		}
		time.Sleep(150 * time.Millisecond)
		if _, err := s.markPeopleWanted(context.Background(), []string{lana}); err != nil {
			t.Error(err)
		}
		wanted <- struct{}{}
	}()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	job := &PersonPhotoJob{Store: s, TMDb: faces, Logger: quietLogger(), MinVotes: 0, Sweep: pace, Wanted: wanted}
	if err := job.Run(ctx); err != nil {
		t.Fatal(err)
	}
	if got, want := faces.seen(), []string{moss, keanu, lana}; !reflect.DeepEqual(got, want) {
		t.Fatalf("asked %v, want the wanted people and nobody from the sweep: %v", got, want)
	}

	// Two turns of the pace are two people from the sweep, and no more.
	ctx, cancel = context.WithTimeout(context.Background(), 500*time.Millisecond)
	defer cancel()
	job.Sweep = rate.NewLimiter(rate.Every(time.Hour), 2)
	if err := job.Run(ctx); err != nil {
		t.Fatal(err)
	}
	if got := faces.seen()[3:]; !reflect.DeepEqual(got, []string{freeman, robbins}) {
		t.Errorf("with two turns, the sweep asked %v, want [%s %s]", got, freeman, robbins)
	}
}

// TestAnAnswerPast150DaysIsAskedAgainAfterTheSweep: TMDb's terms want an
// answer asked again within six months. It is the last thing the job
// does, after everybody never asked, and the new answer replaces the
// old in place. A younger answer is left alone, and so is one whose
// person has left the catalog: that is the backstop's to delete.
func TestAnAnswerPast150DaysIsAskedAgainAfterTheSweep(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()
	peopleFixture(t, s,
		`INSERT INTO meta.people (nconst, tmdb_id, profile_path, asked_at) VALUES
		    ('`+keanu+`', 6384, '/old-keanu.jpg', now() - interval '151 days'),
		    ('`+moss+`', 530, '/moss.jpg', now() - interval '149 days'),
		    ('nm7777777', 9, NULL, now() - interval '160 days')`,
	)
	faces := &fakeFaces{photos: map[string]string{keanu: "/new-keanu.jpg"}}
	job := &PersonPhotoJob{Store: s, TMDb: faces, Logger: quietLogger(), MinVotes: 0}
	if err := job.Run(ctx); err != nil {
		t.Fatal(err)
	}
	want := []string{freeman, robbins, darabont, lilly, lana, fishburne, keanu}
	if got := faces.seen(); !reflect.DeepEqual(got, want) {
		t.Fatalf("asked %v, want the sweep and then the answer that came due: %v", got, want)
	}
	if got, _ := personRow(t, s, keanu); got != "/new-keanu.jpg" {
		t.Errorf("keanu = %q, want the new answer", got)
	}
	var fresh bool
	if err := s.pool.QueryRow(ctx,
		`SELECT asked_at > now() - interval '1 minute' FROM meta.people WHERE nconst = $1`, keanu).Scan(&fresh); err != nil {
		t.Fatal(err)
	}
	if !fresh {
		t.Error("the re-ask did not renew the answer's stamp")
	}
	if got, _ := personRow(t, s, moss); got != "/moss.jpg" {
		t.Errorf("a 149-day answer was touched: %q", got)
	}
}

// TestAFaultIsNotStoredAndARefusedKeyEndsThePass: a failed lookup keeps
// nobody from their next chance and is asked once a pass; a refused key
// stops the pass with a KeyError, since only a person can fix it.
func TestAFaultIsNotStoredAndARefusedKeyEndsThePass(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()
	peopleFixture(t, s)
	faces := &fakeFaces{photos: map[string]string{robbins: "/robbins.jpg"}, err: map[string]error{freeman: errors.New("tmdb down")}}
	job := &PersonPhotoJob{Store: s, TMDb: faces, Logger: quietLogger(), MinVotes: 0}
	if err := job.Run(ctx); err != nil {
		t.Fatal(err)
	}
	if got := faces.seen(); !reflect.DeepEqual(got, sweepOrder) {
		t.Errorf("asked %v, want everybody once: %v", got, sweepOrder)
	}
	if _, ok := personRow(t, s, freeman); ok {
		t.Error("a failed lookup was stored as an answer")
	}

	faces = &fakeFaces{err: map[string]error{freeman: &tmdb.StatusError{Status: 401, Message: "Invalid API key"}}}
	job = &PersonPhotoJob{Store: s, TMDb: faces, Logger: quietLogger(), MinVotes: 0}
	err := job.Run(ctx)
	var key *KeyError
	if !errors.As(err, &key) || key.Provider != "TMDb" {
		t.Fatalf("err = %v, want a KeyError for TMDb", err)
	}
	if got := faces.seen(); len(got) != 1 {
		t.Errorf("asked %v after the key was refused", got)
	}
}

// TestAMarkOnlyWantsWhoStillNeedsAsking: a person with no answer, or one
// that has come due, is marked; a fresh answer, which the job may have
// given a moment before the mark was written, is not. A second mark
// within the minute for a person already wanted changes nothing, and
// says so, which is what keeps a map opened again straight away from
// waking the job. A mark older than that is renewed.
func TestAMarkOnlyWantsWhoStillNeedsAsking(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()
	peopleFixture(t, s,
		`INSERT INTO meta.people (nconst, tmdb_id, profile_path, asked_at) VALUES
		    ('`+keanu+`', 6384, '/keanu.jpg', now()),
		    ('`+moss+`', 530, NULL, now()),
		    ('`+fishburne+`', 2975, '/fishburne.jpg', now() - interval '151 days')`,
	)
	n, err := s.markPeopleWanted(ctx, []string{keanu, moss, fishburne, lana, "nm7777777"})
	if err != nil {
		t.Fatal(err)
	}
	if n != 2 {
		t.Errorf("marked %d, want the due answer and the unasked person", n)
	}
	first := wantedPeople(t, s)
	if len(first) != 2 || first[fishburne].IsZero() || first[lana].IsZero() {
		t.Fatalf("wanted %v, want %s and %s", first, fishburne, lana)
	}
	n, err = s.markPeopleWanted(ctx, []string{keanu, moss, fishburne, lana})
	if err != nil {
		t.Fatal(err)
	}
	if n != 0 {
		t.Errorf("marking the same people again changed %d rows", n)
	}
	if again := wantedPeople(t, s); !reflect.DeepEqual(again, first) {
		t.Errorf("a second mark moved the wants from %v to %v", first, again)
	}

	// A refill keeps the due answer's want: it is the answer waiting to
	// be replaced, not one that has been.
	if err := s.refillPeopleQueue(ctx, 0); err != nil {
		t.Fatal(err)
	}
	if after := wantedPeople(t, s); !reflect.DeepEqual(after, first) {
		t.Errorf("after a refill, wanted %v, want %v", after, first)
	}

	// Two minutes on, Lana's map is opened again: her mark is renewed,
	// and Fishburne's, made a moment ago, is left as it is.
	if _, err := s.pool.Exec(ctx,
		`UPDATE meta.people_queue SET wanted_at = wanted_at - interval '2 minutes' WHERE nconst = $1`, lana); err != nil {
		t.Fatal(err)
	}
	n, err = s.markPeopleWanted(ctx, []string{fishburne, lana})
	if err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Errorf("marking again after two minutes changed %d rows, want Lana's", n)
	}
	renewed := wantedPeople(t, s)
	if !renewed[lana].After(first[lana]) {
		t.Errorf("lana's two-minute-old mark was not renewed: %v, was %v", renewed[lana], first[lana])
	}
	if !renewed[fishburne].Equal(first[fishburne]) {
		t.Errorf("fishburne's fresh mark moved from %v to %v", first[fishburne], renewed[fishburne])
	}
}

// TestAWantedPersonWhoFailedIsAskedAgainWhenMarkedAgain: a failed lookup
// for a wanted person holds them back for the rest of the pass, which in
// the sweep can run for days. A reader opening their map again once the
// mark is more than a minute old renews it and wakes the job, and they
// are asked again straight after the lookup in hand.
func TestAWantedPersonWhoFailedIsAskedAgainWhenMarkedAgain(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()
	peopleFixture(t, s)
	mark(t, s, lana)
	wanted := make(chan struct{}, 1)
	faces := &fakeFaces{
		photos: map[string]string{lana: "/lana.jpg"},
		err:    map[string]error{lana: errors.New("tmdb down")},
	}
	faces.onFind = func(nconst string) {
		if nconst != freeman {
			return
		}
		// Lana has failed and the sweep has moved on. TMDb is back, her
		// mark is two minutes old, and a reader opens her map again,
		// which renews it and wakes the job.
		faces.mu.Lock()
		faces.err = nil
		faces.mu.Unlock()
		if _, err := s.pool.Exec(ctx,
			`UPDATE meta.people_queue SET wanted_at = wanted_at - interval '2 minutes' WHERE nconst = $1`, lana); err != nil {
			t.Error(err)
		}
		if n, err := s.markPeopleWanted(ctx, []string{lana}); err != nil || n != 1 {
			t.Errorf("marking Lana again changed %d rows (%v), want 1", n, err)
		}
		wanted <- struct{}{}
	}
	job := &PersonPhotoJob{Store: s, TMDb: faces, Logger: quietLogger(), MinVotes: 0, Batch: 10, Wanted: wanted}
	if err := job.Run(ctx); err != nil {
		t.Fatal(err)
	}
	want := []string{lana, freeman, lana, robbins, darabont, keanu, moss, lilly, fishburne}
	if got := faces.seen(); !reflect.DeepEqual(got, want) {
		t.Errorf("asked %v, want Lana again straight after the lookup in hand: %v", got, want)
	}
	if got, ok := personRow(t, s, lana); !ok || got != "/lana.jpg" {
		t.Errorf("lana = %q (row %v), want /lana.jpg", got, ok)
	}
}

// TestAMapsMarkIsWrittenAndWakesThePeopleJob is the reader's half:
// WantPeople returns at once, and the buffer writes the mark and sends
// the signal the runner's listener turns into the job's wake.
func TestAMapsMarkIsWrittenAndWakesThePeopleJob(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()
	peopleFixture(t, s)

	conn, err := pgx.Connect(ctx, os.Getenv("CATALOG_TEST_URL"))
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close(ctx)
	if _, err := conn.Exec(ctx, "LISTEN "+NotifyPersonWanted); err != nil {
		t.Fatal(err)
	}

	s.WantPeople(keanu, moss)

	wait, cancel := context.WithTimeout(ctx, 3*wantFlush)
	defer cancel()
	note, err := conn.WaitForNotification(wait)
	if err != nil {
		t.Fatalf("no %s signal: %v", NotifyPersonWanted, err)
	}
	if note.Channel != NotifyPersonWanted {
		t.Errorf("signal on %q", note.Channel)
	}
	if got := wantedPeople(t, s); len(got) != 2 {
		t.Errorf("wanted %v, want %s and %s", got, keanu, moss)
	}
}

// flushedWith waits for the wants buffer to have written everything
// before a mark for sentinel, by marking sentinel and waiting until it
// is wanted: one buffer, written in order.
func flushedWith(t *testing.T, s *Store, sentinel string) {
	t.Helper()
	s.WantPeople(sentinel)
	until(t, sentinel+"'s mark to be written", func() bool {
		_, ok := wantedPeople(t, s)[sentinel]
		return ok
	})
}

// TestAMapCarriesFreshPhotosAndMarksTheMissingOnce: the chip row carries
// the photo of everybody the job has one for, a due answer's included,
// and leaves it out for "none", for no answer, and for an answer past
// the 175 days TMDb's terms allow. Opening the map marks the people
// with no answer, or a due one; opening it again straight away marks
// nobody new; and once the job has answered them, "none" included,
// opening it marks nobody at all. Reading a map's cards marks nobody.
func TestAMapCarriesFreshPhotosAndMarksTheMissingOnce(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()
	// People on no film at all, whose marks show when the buffer has
	// written everything before them.
	sentinels := []string{"nm9800010", "nm9800011", "nm9800012", "nm9800013"}
	peopleFixture(t, s,
		`INSERT INTO meta.people (nconst, tmdb_id, profile_path, asked_at) VALUES
		    ('`+keanu+`', 6384, '/keanu.jpg', now() - interval '1 hour'),
		    ('`+moss+`', 530, NULL, now() - interval '1 hour'),
		    ('`+fishburne+`', 2975, '/fishburne.jpg', now() - interval '151 days'),
		    ('`+lana+`', 9340, '/lana.jpg', now() - interval '176 days')`,
		`INSERT INTO `+Live+`.names (nconst, primary_name)
		 SELECT id, 'Sentinel' FROM unnest(ARRAY['`+strings.Join(sentinels, "','")+`']) AS id`,
	)
	t.Cleanup(func() {
		for _, stmt := range []string{
			`DELETE FROM ` + Live + `.names WHERE nconst LIKE 'nm98%'`,
			`DELETE FROM meta.people WHERE nconst LIKE 'nm98%'`,
			`DELETE FROM meta.people_queue WHERE nconst LIKE 'nm98%'`,
		} {
			_, _ = s.pool.Exec(context.Background(), stmt)
		}
	})

	// The cards' detail reads the chip row too, and marks nobody.
	if _, err := s.Films(ctx, "tt0133093", []string{"tt0133093", "tt0234215"}); err != nil {
		t.Fatal(err)
	}
	flushedWith(t, s, sentinels[0])
	if got := wantedPeople(t, s); len(got) != 1 {
		t.Fatalf("after reading cards, wanted %v; want only the sentinel", got)
	}

	grid, err := s.Grid(ctx, "tt0133093")
	if err != nil {
		t.Fatal(err)
	}
	photos := map[string]string{}
	for _, p := range grid.People {
		photos[p.ID] = p.Photo
	}
	for id, want := range map[string]string{
		keanu:     tmdb.ImageBase + "/w185/keanu.jpg",
		fishburne: tmdb.ImageBase + "/w185/fishburne.jpg",
		moss:      "",
		lana:      "",
		lilly:     "",
	} {
		if got, ok := photos[id]; !ok || got != want {
			t.Errorf("%s photo = %q (on the map %v), want %q", id, got, ok, want)
		}
	}
	raw, err := json.Marshal(grid.People)
	if err != nil {
		t.Fatal(err)
	}
	if n := strings.Count(string(raw), `"photo":`); n != 2 {
		t.Errorf("%d people carry a photo key, want 2: %s", n, raw)
	}

	flushedWith(t, s, sentinels[1])
	first := wantedPeople(t, s)
	for _, id := range []string{fishburne, lana, lilly} {
		if first[id].IsZero() {
			t.Errorf("%s was not marked: wanted %v", id, first)
		}
	}
	for _, id := range []string{keanu, moss} {
		if _, ok := first[id]; ok {
			t.Errorf("%s, whose answer is fresh, was marked", id)
		}
	}

	// Opened again straight away: the same marks stand, and nobody new
	// is wanted.
	if _, err := s.Grid(ctx, "tt0133093"); err != nil {
		t.Fatal(err)
	}
	flushedWith(t, s, sentinels[2])
	again := wantedPeople(t, s)
	if len(again) != len(first)+1 {
		t.Errorf("reopening the map wanted %v, want %v and the sentinel", again, first)
	}
	for _, id := range []string{fishburne, lana, lilly} {
		if !again[id].Equal(first[id]) {
			t.Errorf("reopening the map moved %s's mark from %v to %v", id, first[id], again[id])
		}
	}

	// The job answers everybody wanted: Lilly with TMDb's "none".
	faces := &fakeFaces{photos: map[string]string{
		fishburne: "/fishburne-new.jpg", lana: "/lana-new.jpg", lilly: "",
	}}
	job := &PersonPhotoJob{Store: s, TMDb: faces, Logger: quietLogger(), MinVotes: 1 << 30}
	if err := job.Run(ctx); err != nil {
		t.Fatal(err)
	}
	if got := faces.seen(); len(got) != 6 {
		t.Errorf("the job asked %v, want the three on the map and three sentinels", got)
	}

	// Opened once more, the map marks nobody: every answer is fresh, and
	// "none" is an answer.
	grid, err = s.Grid(ctx, "tt0133093")
	if err != nil {
		t.Fatal(err)
	}
	flushedWith(t, s, sentinels[3])
	if got := wantedPeople(t, s); len(got) != 1 {
		t.Errorf("wanted %v after every answer on the map was fresh; want only the sentinel", got)
	}
	for _, p := range grid.People {
		if p.ID == lana && p.Photo != tmdb.ImageBase+"/w185/lana-new.jpg" {
			t.Errorf("lana's photo = %q after the job answered", p.Photo)
		}
		if p.ID == lilly && p.Photo != "" {
			t.Errorf("lilly's photo = %q, want none", p.Photo)
		}
	}
}

// TestPhotosAreReadForTheEndpoint: the endpoint's read says, for each
// person, whether the catalog holds them, the answer that may be shown,
// and when it was given, so the endpoint can tell a due answer.
func TestPhotosAreReadForTheEndpoint(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()
	peopleFixture(t, s,
		`INSERT INTO meta.people (nconst, tmdb_id, profile_path, asked_at) VALUES
		    ('`+keanu+`', 6384, '/keanu.jpg', now() - interval '1 hour'),
		    ('`+moss+`', 530, NULL, now() - interval '1 hour'),
		    ('`+fishburne+`', 2975, '/fishburne.jpg', now() - interval '151 days'),
		    ('`+lana+`', 9340, '/lana.jpg', now() - interval '176 days')`,
	)
	rows, err := s.PeoplePhotos(ctx, []string{keanu, moss, fishburne, lana, lilly, "nm7777777"})
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	for id, want := range map[string]struct {
		person, asked, due bool
		photo              string
	}{
		keanu:       {true, true, false, tmdb.ImageBase + "/w185/keanu.jpg"},
		moss:        {true, true, false, ""},
		fishburne:   {true, true, true, tmdb.ImageBase + "/w185/fishburne.jpg"},
		lana:        {true, false, false, ""},
		lilly:       {true, false, false, ""},
		"nm7777777": {false, false, false, ""},
	} {
		got := rows[id]
		if got.Person != want.person || got.Asked != want.asked || got.Due(now) != want.due || got.Photo != want.photo {
			t.Errorf("%s = %+v (due %v), want %+v", id, got, got.Due(now), want)
		}
	}
}
