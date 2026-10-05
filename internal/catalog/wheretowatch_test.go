package catalog

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/netip"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/riverqueue/river"
	"github.com/riverqueue/river/riverdriver/riverpgxv5"
	"github.com/riverqueue/river/rivertest"
	"github.com/riverqueue/river/rivertype"

	"cinedikt/internal/geoip"
	"cinedikt/internal/geoip/geoiptest"
	"cinedikt/internal/streaming"
)

// fakeStreaming stands in for the Streaming Availability API and
// remembers what it was asked.
type fakeStreaming struct {
	mu        sync.Mutex
	t         *testing.T
	shows     map[string][]streaming.StreamingOption
	showErr   error
	countries []streaming.Country
	listErr   error
	asked     []string
	listed    int
	// gate, when set, holds every Show until it is closed.
	gate chan struct{}
	// feed, when set, is the real client pointed at a stand-in for the
	// changes feed. Without one the feed has no changes.
	feed *streaming.Client
}

func (f *fakeStreaming) Show(_ context.Context, imdbID, country string) ([]streaming.StreamingOption, error) {
	f.mu.Lock()
	f.asked = append(f.asked, imdbID+"/"+country)
	gate, err, options := f.gate, f.showErr, f.shows[imdbID+"/"+country]
	f.mu.Unlock()
	if gate != nil {
		<-gate
	}
	if err != nil {
		return nil, err
	}
	if options == nil {
		return nil, streaming.ErrNotFound
	}
	return options, nil
}

func (f *fakeStreaming) Countries(context.Context) ([]streaming.Country, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.listed++
	return f.countries, f.listErr
}

func (f *fakeStreaming) Changes(ctx context.Context, q streaming.ChangesQuery) (streaming.ChangesPage, error) {
	if f.feed == nil {
		return streaming.ChangesPage{}, nil
	}
	return f.feed.Changes(ctx, q)
}

func (f *fakeStreaming) set(key string, options []streaming.StreamingOption, err error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.shows == nil {
		f.shows = map[string][]streaming.StreamingOption{}
	}
	f.shows[key] = options
	f.showErr = err
}

func (f *fakeStreaming) calls() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.asked...)
}

func (f *fakeStreaming) lists() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.listed
}

var coveredCountries = []streaming.Country{{Code: "gb", Name: "United Kingdom"}, {Code: "us", Name: "United States"}}

func service(id, name string) streaming.Service {
	return streaming.Service{ID: id, Name: name, ImageSet: streaming.ImageSet{
		LightThemeImage: "https://img/" + id + "-light.svg", DarkThemeImage: "https://img/" + id + "-dark.svg",
	}}
}

// watchFixture publishes the fixture catalog with nothing known about
// where to watch and no jobs queued, and builds the service with a queue
// that is not started, so the tests see each job it inserts and work
// them by hand.
func watchFixture(t *testing.T, api *fakeStreaming) (*Store, *WhereToWatch, *Queue) {
	t.Helper()
	s := testStore(t)
	publishFixture(t, s)
	ctx := context.Background()
	w := &WhereToWatch{Store: s, API: api, Logger: quietLogger()}
	q, err := OpenQueue(ctx, QueueConfig{DatabaseURL: leaseURL(t), Logger: quietLogger(), WhereToWatch: w})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { q.Stop(context.Background()) })
	for _, stmt := range []string{
		`DELETE FROM meta.where_to_watch`,
		`DELETE FROM meta.streaming_countries`,
		`DELETE FROM meta.streaming_sync`,
		`DELETE FROM ` + QueueSchema + `.river_job`,
	} {
		if _, err := s.pool.Exec(ctx, stmt); err != nil {
			t.Fatal(err)
		}
	}
	return s, w, q
}

func (q *Queue) driver() *riverpgxv5.Driver { return riverpgxv5.New(q.pool) }

// refreshJobs is every refresh in the queue, oldest first.
func refreshJobs(t *testing.T, q *Queue) []*rivertype.JobRow {
	t.Helper()
	res, err := q.client.JobList(context.Background(), river.NewJobListParams().Kinds(WatchRefreshArgs{}.Kind()).First(100))
	if err != nil {
		t.Fatal(err)
	}
	return res.Jobs
}

func refreshArgs(t *testing.T, job *rivertype.JobRow) WatchRefreshArgs {
	t.Helper()
	var args WatchRefreshArgs
	if err := json.Unmarshal(job.EncodedArgs, &args); err != nil {
		t.Fatal(err)
	}
	return args
}

func netflixOnly() []streaming.StreamingOption {
	return []streaming.StreamingOption{
		{Service: service("netflix", "Netflix"), Type: "subscription", Link: "https://netflix/title/1"},
	}
}

// TestAnUncoveredCountryIsAnsweredWithoutAskingTheAPI, and so is a
// reader who could not be placed.
func TestAnUncoveredCountryIsAnsweredWithoutAskingTheAPI(t *testing.T) {
	api := &fakeStreaming{t: t, countries: coveredCountries}
	_, w, _ := watchFixture(t, api)
	ctx := context.Background()
	for country, want := range map[string]string{"ng": "ng", "": Uncovered, Uncovered: Uncovered, "US": Uncovered} {
		got, err := w.Answer(ctx, "tt0133093", country)
		if err != nil {
			t.Fatalf("%q: %v", country, err)
		}
		if got.Country != want || got.Covered || got.Answer != nil || got.CountryName != "" {
			t.Errorf("%q = %+v, want %q uncovered with no lists", country, got, want)
		}
	}
	raw, _ := json.Marshal(WatchReply{Country: Uncovered})
	if string(raw) != `{"country":"xx","covered":false}` {
		t.Errorf("uncovered body = %s", raw)
	}
	if c := api.calls(); len(c) != 0 {
		t.Errorf("the API was asked about %v", c)
	}
}

// TestAFirstAskIsKeptAndTheNextIsServedFromTheStore: the API is asked
// once per movie and country, and the answer names the country.
func TestAFirstAskIsKeptAndTheNextIsServedFromTheStore(t *testing.T) {
	api := &fakeStreaming{t: t, countries: coveredCountries}
	api.set("tt0133093/us", netflixOnly(), nil)
	s, w, q := watchFixture(t, api)
	ctx := context.Background()

	for i := range 3 {
		got, err := w.Answer(ctx, "tt0133093", "us")
		if err != nil {
			t.Fatal(err)
		}
		if !got.Covered || got.Country != "us" || got.CountryName != "United States" || got.Answer == nil ||
			len(got.Stream) != 1 || got.Stream[0].Name != "Netflix" {
			t.Fatalf("answer %d = %+v", i, got)
		}
	}
	if c := api.calls(); len(c) != 1 {
		t.Errorf("the API was asked %d times: %v", len(c), c)
	}
	row, err := s.WhereToWatch(ctx, "tt0133093", "us")
	if err != nil {
		t.Fatal(err)
	}
	if !row.Stored || time.Since(row.FetchedAt) > time.Minute {
		t.Errorf("row = %+v", row)
	}
	if got := expiresAt(t, s, "tt0133093", "us"); !got.IsZero() {
		t.Errorf("expires_at = %v, want none", got)
	}
	// Nothing in it is leaving, so nothing is scheduled.
	if jobs := refreshJobs(t, q); len(jobs) != 0 {
		t.Errorf("jobs = %d, want none", len(jobs))
	}
	// Another country is another answer.
	api.set("tt0133093/gb", netflixOnly(), nil)
	if got, err := w.Answer(ctx, "tt0133093", "gb"); err != nil || got.CountryName != "United Kingdom" {
		t.Fatalf("gb = %+v, %v", got, err)
	}
	if c := api.calls(); len(c) != 2 || c[1] != "tt0133093/gb" {
		t.Errorf("calls = %v", c)
	}
}

// TestAFailedAskIsNotKept: the reader gets an error, and the next reader
// asks again rather than being served the failure.
func TestAFailedAskIsNotKept(t *testing.T) {
	api := &fakeStreaming{t: t, countries: coveredCountries}
	api.set("tt0133093/us", nil, errors.New("streaming: HTTP 503"))
	s, w, _ := watchFixture(t, api)
	ctx := context.Background()
	if _, err := w.Answer(ctx, "tt0133093", "us"); !errors.Is(err, ErrUpstream) {
		t.Fatalf("a failed ask: err = %v, want ErrUpstream", err)
	}
	if row, err := s.WhereToWatch(ctx, "tt0133093", "us"); err != nil || row.Stored {
		t.Fatalf("after a failure: row %+v, err %v", row, err)
	}
	api.set("tt0133093/us", netflixOnly(), nil)
	if got, err := w.Answer(ctx, "tt0133093", "us"); err != nil || len(got.Stream) != 1 {
		t.Fatalf("after the API came back: %+v, %v", got, err)
	}
	if c := api.calls(); len(c) != 2 {
		t.Errorf("calls = %v, want the failure and the retry", c)
	}
}

// TestAMovieOnNothingIsKeptAsAnAnswer: a show the API does not know is
// as definite as an empty list, and asking again would only cost.
func TestAMovieOnNothingIsKeptAsAnAnswer(t *testing.T) {
	api := &fakeStreaming{t: t, countries: coveredCountries}
	_, w, _ := watchFixture(t, api)
	ctx := context.Background()
	for range 2 {
		got, err := w.Answer(ctx, "tt0111161", "us")
		if err != nil {
			t.Fatal(err)
		}
		raw, _ := json.Marshal(got)
		if string(raw) != `{"country":"us","countryName":"United States","covered":true,"stream":[],"free":[],"rent":[],"buy":[]}` {
			t.Errorf("body = %s", raw)
		}
	}
	if c := api.calls(); len(c) != 1 {
		t.Errorf("calls = %v", c)
	}
}

func TestAMovieTheCatalogDoesNotHoldIsNeverAskedAbout(t *testing.T) {
	api := &fakeStreaming{t: t, countries: coveredCountries}
	_, w, _ := watchFixture(t, api)
	if _, err := w.Answer(context.Background(), "tt9999999", "us"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("err = %v, want ErrNotFound", err)
	}
	if c := api.calls(); len(c) != 0 {
		t.Errorf("calls = %v", c)
	}
}

// TestReadersAskingTogetherAskOnce: the second reader waits on the first
// reader's ask.
func TestReadersAskingTogetherAskOnce(t *testing.T) {
	api := &fakeStreaming{t: t, countries: coveredCountries, gate: make(chan struct{})}
	api.set("tt0133093/us", netflixOnly(), nil)
	_, w, _ := watchFixture(t, api)
	var wg sync.WaitGroup
	errs := make(chan error, 4)
	for range 4 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			got, err := w.Answer(context.Background(), "tt0133093", "us")
			if err == nil && len(got.Stream) != 1 {
				err = errors.New("no stream")
			}
			errs <- err
		}()
	}
	// Let them all arrive, then let the one ask through.
	until(t, "the first ask", func() bool { return len(api.calls()) == 1 })
	time.Sleep(50 * time.Millisecond)
	close(api.gate)
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Error(err)
		}
	}
	if c := api.calls(); len(c) != 1 {
		t.Errorf("calls = %v", c)
	}
}

// TestAnOldAnswerIsServedAndRefreshedOnce: the changes feed keeps answers
// right, so one is not asked about again until it is 30 days old; and
// then it is still the answer, and however many readers find it old, one
// refresh is queued.
func TestAnOldAnswerIsServedAndRefreshedOnce(t *testing.T) {
	api := &fakeStreaming{t: t, countries: coveredCountries}
	api.set("tt0133093/us", netflixOnly(), nil)
	s, w, q := watchFixture(t, api)
	ctx := context.Background()
	if _, err := w.Answer(ctx, "tt0133093", "us"); err != nil {
		t.Fatal(err)
	}

	// A day, or 29, is not old yet.
	for _, age := range []string{"25 hours", "29 days"} {
		if _, err := s.pool.Exec(ctx, `UPDATE meta.where_to_watch SET fetched_at = now() - $1::interval`, age); err != nil {
			t.Fatal(err)
		}
		if _, err := w.Answer(ctx, "tt0133093", "us"); err != nil {
			t.Fatal(err)
		}
		if jobs := refreshJobs(t, q); len(jobs) != 0 {
			t.Fatalf("a %s old answer queued %d refreshes", age, len(jobs))
		}
	}

	if _, err := s.pool.Exec(ctx, `UPDATE meta.where_to_watch SET fetched_at = now() - interval '31 days'`); err != nil {
		t.Fatal(err)
	}
	api.set("tt0133093/us", []streaming.StreamingOption{
		{Service: service("hulu", "Hulu"), Type: "subscription", Link: "h"},
	}, nil)
	for range 3 {
		got, err := w.Answer(ctx, "tt0133093", "us")
		if err != nil {
			t.Fatal(err)
		}
		if len(got.Stream) != 1 || got.Stream[0].Name != "Netflix" {
			t.Errorf("the old answer was not served: %+v", got.Stream)
		}
	}
	if c := api.calls(); len(c) != 1 {
		t.Errorf("serving an old answer asked the API: %v", c)
	}
	job := rivertest.RequireInserted(ctx, t, q.driver(), WatchRefreshArgs{},
		&rivertest.RequireInsertedOpts{Schema: QueueSchema, State: rivertype.JobStateAvailable, MaxAttempts: queueRefreshAttempts})
	if job.Args.Tconst != "tt0133093" || job.Args.Country != "us" || !job.Args.At.IsZero() {
		t.Errorf("job args = %+v", job.Args)
	}

	// Working it asks again and keeps the new answer.
	worker := &watchRefreshWorker{w: w}
	if err := worker.Work(ctx, job); err != nil {
		t.Fatal(err)
	}
	got, err := w.Answer(ctx, "tt0133093", "us")
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Stream) != 1 || got.Stream[0].Name != "Hulu" {
		t.Errorf("after the refresh = %+v", got.Stream)
	}
	if c := api.calls(); len(c) != 2 {
		t.Errorf("calls = %v", c)
	}

	// The same job again finds the answer kept since it was asked for,
	// and leaves the API alone.
	if err := worker.Work(ctx, job); err != nil {
		t.Fatal(err)
	}
	if c := api.calls(); len(c) != 2 {
		t.Errorf("a refresh already done asked again: %v", c)
	}
}

// TestALeavingOptionSchedulesARefreshForItsMoment: the job is due when
// the option leaves; working it keeps the answer without that option and
// schedules the next one.
func TestALeavingOptionSchedulesARefreshForItsMoment(t *testing.T) {
	now := time.Now().Truncate(time.Second)
	leaves := now.Add(48 * time.Hour).UTC()
	then := now.Add(120 * time.Hour).UTC()
	api := &fakeStreaming{t: t, countries: coveredCountries}
	api.set("tt0133093/us", []streaming.StreamingOption{
		{Service: service("netflix", "Netflix"), Type: "subscription", Link: "n", ExpiresOn: leaves.Unix()},
		{Service: service("hulu", "Hulu"), Type: "subscription", Link: "h", ExpiresOn: then.Unix()},
		{Service: service("max", "Max"), Type: "subscription", Link: "m"},
	}, nil)
	s, w, q := watchFixture(t, api)
	ctx := context.Background()

	got, err := w.Answer(ctx, "tt0133093", "us")
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Stream) != 3 {
		t.Fatalf("stream = %+v", got.Stream)
	}
	if got := expiresAt(t, s, "tt0133093", "us"); !got.Equal(leaves) {
		t.Fatalf("expires_at = %v, want %v", got, leaves)
	}
	job := rivertest.RequireInserted(ctx, t, q.driver(), WatchRefreshArgs{},
		&rivertest.RequireInsertedOpts{Schema: QueueSchema, ScheduledAt: leaves, State: rivertype.JobStateScheduled})
	if job.Args.Tconst != "tt0133093" || job.Args.Country != "us" || !job.Args.At.Equal(leaves) {
		t.Fatalf("job args = %+v", job.Args)
	}

	// A stale refresh and the expiry's are different jobs, and neither
	// stands in for the other; a second of either is the first.
	w.refreshSoon(ctx, "tt0133093", "us")
	w.refreshSoon(ctx, "tt0133093", "us")
	if _, err := q.client.Insert(ctx, WatchRefreshArgs{Tconst: "tt0133093", Country: "us", At: leaves}, &river.InsertOpts{ScheduledAt: leaves}); err != nil {
		t.Fatal(err)
	}
	if jobs := refreshJobs(t, q); len(jobs) != 2 {
		t.Fatalf("jobs = %d, want the expiry's and one stale refresh", len(jobs))
	}
	if _, err := s.pool.Exec(ctx, `DELETE FROM `+QueueSchema+`.river_job WHERE scheduled_at < $1`, leaves); err != nil {
		t.Fatal(err)
	}

	// The moment comes. The API still lists Netflix, as it can for a
	// while, with its day passed; it is left out, and Hulu's leaving is
	// the next job.
	w.now = func() time.Time { return leaves.Add(time.Minute) }
	if err := (&watchRefreshWorker{w: w}).Work(ctx, job); err != nil {
		t.Fatal(err)
	}
	w.now = nil
	row, err := s.WhereToWatch(ctx, "tt0133093", "us")
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, o := range row.Answer.Stream {
		names = append(names, o.Name)
	}
	if len(names) != 2 || names[0] != "Hulu" || names[1] != "Max" {
		t.Errorf("after the refresh = %v, want Hulu and Max", names)
	}
	if got := expiresAt(t, s, "tt0133093", "us"); !got.Equal(then) {
		t.Errorf("expires_at = %v, want %v", got, then)
	}
	var next []WatchRefreshArgs
	for _, j := range refreshJobs(t, q) {
		if j.ID != job.ID {
			next = append(next, refreshArgs(t, j))
			if !j.ScheduledAt.Equal(then) {
				t.Errorf("next job scheduled at %v, want %v", j.ScheduledAt, then)
			}
		}
	}
	if len(next) != 1 || !next[0].At.Equal(then) {
		t.Errorf("next jobs = %+v, want one for %v", next, then)
	}
}

// TestARefusedKeyStopsTheRefresh: the old answer stays, and the job is
// cancelled rather than retried with the same key.
func TestARefusedKeyStopsTheRefresh(t *testing.T) {
	api := &fakeStreaming{t: t, countries: coveredCountries}
	api.set("tt0133093/us", netflixOnly(), nil)
	s, w, q := watchFixture(t, api)
	ctx := context.Background()
	if _, err := w.Answer(ctx, "tt0133093", "us"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.pool.Exec(ctx, `UPDATE meta.where_to_watch SET fetched_at = now() - interval '2 days'`); err != nil {
		t.Fatal(err)
	}
	api.set("tt0133093/us", nil, streaming.ErrKey)
	w.refreshSoon(ctx, "tt0133093", "us")
	job := rivertest.RequireInserted(ctx, t, q.driver(), WatchRefreshArgs{}, &rivertest.RequireInsertedOpts{Schema: QueueSchema})
	var log logSink
	w.Logger = log.logger()
	err := (&watchRefreshWorker{w: w}).Work(ctx, job)
	var cancel *river.JobCancelError
	if !errors.As(err, &cancel) || !errors.Is(err, streaming.ErrKey) {
		t.Errorf("err = %v, want a cancelled job", err)
	}
	if out := log.String(); !strings.Contains(out, "level=ERROR") || !strings.Contains(out, "tconst=tt0133093") {
		t.Errorf("a refused key was not logged as an error: %q", out)
	}
	if row, _ := s.WhereToWatch(ctx, "tt0133093", "us"); !row.Stored || len(row.Answer.Stream) != 1 {
		t.Errorf("the old answer went: %+v", row)
	}
}

// TestTheCountryListIsFilledOnceAndAskedForWeekly: the first reader's
// request fills it, the daily job leaves a fresh list alone, and a list
// that comes back empty never replaces one.
func TestTheCountryListIsFilledOnceAndAskedForWeekly(t *testing.T) {
	api := &fakeStreaming{t: t, countries: coveredCountries}
	api.set("tt0133093/us", netflixOnly(), nil)
	s, w, _ := watchFixture(t, api)
	ctx := context.Background()
	if got, err := w.Answer(ctx, "tt0133093", "us"); err != nil || got.CountryName != "United States" {
		t.Fatalf("answer = %+v, %v", got, err)
	}
	if got, err := w.Answer(ctx, "tt0133093", "ng"); err != nil || got.Covered {
		t.Fatalf("ng = %+v, %v", got, err)
	}
	if api.lists() != 1 {
		t.Errorf("the list was asked for %d times", api.lists())
	}

	worker := &countriesWorker{w: w}
	if err := worker.Work(ctx, nil); err != nil {
		t.Fatal(err)
	}
	if api.lists() != 1 {
		t.Errorf("a fresh list was asked for again")
	}

	if _, err := s.pool.Exec(ctx, `UPDATE meta.streaming_countries SET fetched_at = now() - interval '8 days'`); err != nil {
		t.Fatal(err)
	}
	api.mu.Lock()
	api.countries = []streaming.Country{{Code: "us", Name: "United States"}, {Code: "ng", Name: "Nigeria"}}
	api.mu.Unlock()
	if err := worker.Work(ctx, nil); err != nil {
		t.Fatal(err)
	}
	if api.lists() != 2 {
		t.Errorf("a week-old list was not asked for")
	}
	if name, known, _, _ := s.StreamingCountry(ctx, "ng"); !known || name != "Nigeria" {
		t.Errorf("ng = %q %v", name, known)
	}
	if _, known, _, _ := s.StreamingCountry(ctx, "gb"); known {
		t.Error("a country the API dropped is still covered")
	}

	if _, err := s.pool.Exec(ctx, `UPDATE meta.streaming_countries SET fetched_at = now() - interval '8 days'`); err != nil {
		t.Fatal(err)
	}
	api.mu.Lock()
	api.countries = nil
	api.mu.Unlock()
	if err := worker.Work(ctx, nil); err == nil {
		t.Error("an empty list was accepted")
	}
	if _, n, _ := s.StreamingCountriesAge(ctx); n != 2 {
		t.Errorf("an empty list replaced the kept one: %d countries", n)
	}
}

// TestACountryListThatCannotBeFetchedIsAnError: with no list there is no
// telling coverage, and saying "none" would be untrue. The failure stands
// for a minute, so an API that is down is not asked by every reader.
func TestACountryListThatCannotBeFetchedIsAnError(t *testing.T) {
	api := &fakeStreaming{t: t, listErr: errors.New("streaming: HTTP 500")}
	_, w, _ := watchFixture(t, api)
	ctx := context.Background()
	now := time.Now()
	w.now = func() time.Time { return now }
	for range 3 {
		if got, err := w.Answer(ctx, "tt0133093", "us"); !errors.Is(err, ErrUpstream) {
			t.Fatalf("answered %+v, %v; want ErrUpstream", got, err)
		}
	}
	if api.lists() != 1 {
		t.Errorf("the list was asked for %d times inside a minute", api.lists())
	}
	if c := api.calls(); len(c) != 0 {
		t.Errorf("calls = %v", c)
	}

	// A minute on, it is asked again, and an answer ends the wait.
	now = now.Add(countriesCooldown + time.Second)
	api.mu.Lock()
	api.listErr, api.countries = nil, coveredCountries
	api.mu.Unlock()
	api.set("tt0133093/us", netflixOnly(), nil)
	if got, err := w.Answer(ctx, "tt0133093", "us"); err != nil || !got.Covered {
		t.Fatalf("after a minute: %+v, %v", got, err)
	}
	if api.lists() != 2 {
		t.Errorf("lists = %d", api.lists())
	}
}

// TestTheQueueLivesInItsOwnSchema, and two processes migrating it at
// once take turns rather than colliding.
func TestTheQueueLivesInItsOwnSchema(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()
	var wg sync.WaitGroup
	errs := make(chan error, 2)
	for range 2 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			q, err := OpenQueue(ctx, QueueConfig{DatabaseURL: leaseURL(t), Logger: quietLogger()})
			if err == nil {
				q.Stop(ctx)
			}
			errs <- err
		}()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatal(err)
		}
	}
	var inRiver bool
	if err := s.pool.QueryRow(ctx, `SELECT to_regclass('`+QueueSchema+`.river_job') IS NOT NULL`).Scan(&inRiver); err != nil {
		t.Fatal(err)
	}
	if !inRiver {
		t.Error("river_job is not in the queue's schema")
	}
	// The daily swap drops the retired catalog and renames the rest;
	// none of that reaches the queue.
	resetLive(t, s)
	if err := s.pool.QueryRow(ctx, `SELECT to_regclass('`+QueueSchema+`.river_job') IS NOT NULL`).Scan(&inRiver); err != nil || !inRiver {
		t.Errorf("after a reset, river_job there = %v (%v)", inRiver, err)
	}
}

// TestTheQueueRunsARefreshWhenStarted: end to end, a started queue picks
// up the stale refresh a reader asked for and keeps the new answer.
func TestTheQueueRunsARefreshWhenStarted(t *testing.T) {
	api := &fakeStreaming{t: t, countries: coveredCountries}
	api.set("tt0133093/us", netflixOnly(), nil)
	s, w, q := watchFixture(t, api)
	ctx := context.Background()
	if _, err := w.Answer(ctx, "tt0133093", "us"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.pool.Exec(ctx, `UPDATE meta.where_to_watch SET fetched_at = now() - interval '31 days'`); err != nil {
		t.Fatal(err)
	}
	api.set("tt0133093/us", []streaming.StreamingOption{{Service: service("hulu", "Hulu"), Type: "subscription", Link: "h"}}, nil)
	if _, err := w.Answer(ctx, "tt0133093", "us"); err != nil {
		t.Fatal(err)
	}
	if err := q.Start(ctx); err != nil {
		t.Fatal(err)
	}
	until(t, "the refresh", func() bool {
		row, err := s.WhereToWatch(ctx, "tt0133093", "us")
		return err == nil && len(row.Answer.Stream) == 1 && row.Answer.Stream[0].Name == "Hulu"
	})
	stopCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	q.Stop(stopCtx)
}

// TestTheGeoIPDatabaseIsKeptForTheNextProcess: the check downloads only
// a new build, keeps the old one through a bad download, and a second
// process reads the kept one without asking MaxMind at all.
func TestTheGeoIPDatabaseIsKeptForTheNextProcess(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()
	if _, err := s.pool.Exec(ctx, `DELETE FROM meta.geoip`); err != nil {
		t.Fatal(err)
	}
	built := time.Date(2026, 9, 29, 0, 0, 0, 0, time.UTC)
	gb := geoiptest.Database(t, map[string]string{"81.2.69.0/24": "GB"})
	srv := geoiptest.NewServer(t, geoiptest.Archive(t, gb), built)
	first := &geoip.Lookup{}
	u := &geoip.Updater{
		AccountID: geoiptest.Account, LicenseKey: geoiptest.License,
		Store: s, Lookup: first, HTTP: srv.Client(),
		PermalinkURL: srv.PermalinkURL(),
	}
	worker := &geoIPWorker{u: u}

	if err := worker.Work(ctx, nil); err != nil {
		t.Fatal(err)
	}
	if err := worker.Work(ctx, nil); err != nil {
		t.Fatal(err)
	}
	if srv.Heads() != 2 || srv.Gets() != 1 {
		t.Errorf("%d HEAD, %d GET; want two checks and one download", srv.Heads(), srv.Gets())
	}
	stamp, mmdb, err := s.GeoIP(ctx)
	if err != nil || stamp != built.Format("Mon, 02 Jan 2006 15:04:05 GMT") || len(mmdb) != len(gb) {
		t.Fatalf("kept %q, %d bytes, %v", stamp, len(mmdb), err)
	}

	// A bad build keeps the good one, in the table and in use.
	srv.Set([]byte("<html>maintenance</html>"), built.Add(96*time.Hour))
	if err := worker.Work(ctx, nil); err == nil {
		t.Error("a bad download was accepted")
	}
	if got, _ := s.GeoIPStamp(ctx); got != stamp {
		t.Errorf("a bad download replaced the kept stamp: %q", got)
	}
	if cc, _ := first.Country(netip.MustParseAddr("81.2.69.142")); cc != "gb" {
		t.Errorf("after a bad download = %q", cc)
	}

	// Another process reads it straight from the table.
	heads, gets := srv.Heads(), srv.Gets()
	second := &geoip.Lookup{}
	other := &geoip.Updater{Store: s, Lookup: second, HTTP: srv.Client(), PermalinkURL: srv.PermalinkURL()}
	if err := other.LoadStored(ctx); err != nil {
		t.Fatal(err)
	}
	if cc, ok := second.Country(netip.MustParseAddr("81.2.69.142")); !ok || cc != "gb" {
		t.Errorf("second process = %q %v", cc, ok)
	}
	if srv.Heads() != heads || srv.Gets() != gets {
		t.Error("the second process asked MaxMind")
	}

	// A refused key is not retried with the same key.
	u.LicenseKey = "wrong"
	var cancel *river.JobCancelError
	if err := worker.Work(ctx, nil); !errors.As(err, &cancel) {
		t.Errorf("a refused key: err = %v, want a cancelled job", err)
	}
}

// logSink keeps what a logger is told, at every level.
type logSink struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (l *logSink) Write(p []byte) (int, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.buf.Write(p)
}

func (l *logSink) String() string {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.buf.String()
}

func (l *logSink) reset() {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.buf.Reset()
}

func (l *logSink) logger() *slog.Logger {
	return slog.New(slog.NewTextHandler(l, &slog.HandlerOptions{Level: slog.LevelDebug}))
}

// memGeoIP keeps the database in memory, in place of meta.geoip.
type memGeoIP struct {
	stamp string
	mmdb  []byte
}

func (m *memGeoIP) GeoIP(context.Context) (string, []byte, error) { return m.stamp, m.mmdb, nil }
func (m *memGeoIP) GeoIPStamp(context.Context) (string, error)    { return m.stamp, nil }
func (m *memGeoIP) KeepGeoIP(_ context.Context, s string, b []byte) error {
	m.stamp, m.mmdb = s, b
	return nil
}

// TestAFailedGeoIPCheckIsLogged: River reports a failed job at info and a
// cancelled one at debug, which the queue's logger does not pass, so the
// job says so itself. A refused key is an error: until someone fixes it,
// nobody is placed. A bad build is a warning, and a check cut short by a
// stop says nothing.
func TestAFailedGeoIPCheckIsLogged(t *testing.T) {
	built := time.Date(2026, 9, 29, 0, 0, 0, 0, time.UTC)
	srv := geoiptest.NewServer(t, geoiptest.Archive(t, geoiptest.Database(t, map[string]string{"81.2.69.0/24": "GB"})), built)
	var log logSink
	u := &geoip.Updater{
		AccountID: geoiptest.Account, LicenseKey: "wrong",
		Store: &memGeoIP{}, Lookup: &geoip.Lookup{}, HTTP: srv.Client(),
		PermalinkURL: srv.PermalinkURL(), Logger: log.logger(),
	}
	worker := &geoIPWorker{u: u}
	ctx := context.Background()

	err := worker.Work(ctx, nil)
	var cancel *river.JobCancelError
	if !errors.As(err, &cancel) {
		t.Errorf("a refused key: err = %v, want a cancelled job", err)
	}
	if out := log.String(); !strings.Contains(out, "level=ERROR") || !strings.Contains(out, "refused the license key") {
		t.Errorf("a refused key was not logged as an error: %q", out)
	}

	log.reset()
	u.LicenseKey = geoiptest.License
	srv.Set([]byte("<html>maintenance</html>"), built)
	if err := worker.Work(ctx, nil); err == nil || errors.As(err, &cancel) {
		t.Errorf("a bad build: err = %v, want one River retries", err)
	}
	if out := log.String(); !strings.Contains(out, "level=WARN") || !strings.Contains(out, "not gzip") {
		t.Errorf("a bad build was not logged as a warning: %q", out)
	}

	log.reset()
	stopped, stop := context.WithCancel(ctx)
	stop()
	if err := worker.Work(stopped, nil); err == nil {
		t.Error("a stopped check succeeded")
	}
	if out := log.String(); out != "" {
		t.Errorf("a stopped check was logged: %q", out)
	}
}

// TestAQueuedJobThatPanicsIsLogged, since River reports that at info too.
func TestAQueuedJobThatPanicsIsLogged(t *testing.T) {
	var log logSink
	queuePanics{log.logger()}.HandlePanic(context.Background(),
		&rivertype.JobRow{ID: 7, Kind: GeoIPCheckArgs{}.Kind(), Attempt: 1}, "boom", "goroutine 1 [running]")
	if out := log.String(); !strings.Contains(out, "level=ERROR") || !strings.Contains(out, "kind=geoip_check") || !strings.Contains(out, "panic=boom") {
		t.Errorf("log = %q", out)
	}
}

// TestAFailedStreamingJobIsLogged: a refresh the API fails is a warning
// River retries, and a refused key on the country list an error.
func TestAFailedStreamingJobIsLogged(t *testing.T) {
	api := &fakeStreaming{t: t, countries: coveredCountries}
	api.set("tt0133093/us", netflixOnly(), nil)
	s, w, q := watchFixture(t, api)
	ctx := context.Background()
	if _, err := w.Answer(ctx, "tt0133093", "us"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.pool.Exec(ctx, `UPDATE meta.where_to_watch SET fetched_at = now() - interval '2 days'`); err != nil {
		t.Fatal(err)
	}
	var log logSink
	w.Logger = log.logger()

	api.set("tt0133093/us", nil, errors.New("streaming: HTTP 502"))
	w.refreshSoon(ctx, "tt0133093", "us")
	job := rivertest.RequireInserted(ctx, t, q.driver(), WatchRefreshArgs{}, &rivertest.RequireInsertedOpts{Schema: QueueSchema})
	if err := (&watchRefreshWorker{w: w}).Work(ctx, job); err == nil {
		t.Error("a failed refresh succeeded")
	}
	if out := log.String(); !strings.Contains(out, "level=WARN") || !strings.Contains(out, "HTTP 502") || !strings.Contains(out, "country=us") {
		t.Errorf("a failed refresh was not logged: %q", out)
	}

	log.reset()
	if _, err := s.pool.Exec(ctx, `UPDATE meta.streaming_countries SET fetched_at = now() - interval '8 days'`); err != nil {
		t.Fatal(err)
	}
	api.mu.Lock()
	api.listErr = streaming.ErrKey
	api.mu.Unlock()
	err := (&countriesWorker{w: w}).Work(ctx, nil)
	var cancel *river.JobCancelError
	if !errors.As(err, &cancel) {
		t.Errorf("a refused key: err = %v, want a cancelled job", err)
	}
	if out := log.String(); !strings.Contains(out, "level=ERROR") || !strings.Contains(out, "refused") {
		t.Errorf("a refused key was not logged as an error: %q", out)
	}
}
