package catalog

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/riverqueue/river"
	"github.com/riverqueue/river/rivertest"
	"github.com/riverqueue/river/rivertype"

	"cinedikt/internal/streaming"
)

// changesFeed stands in for the API's /changes: for each country and
// kind of change, the changes in time order, served a page at a time
// with a cursor, with the shows they touch keyed by show id. It
// remembers every page it was asked for.
type changesFeed struct {
	mu       sync.Mutex
	t        *testing.T
	pageSize int
	changes  map[string][]feedChange
	shows    map[string]map[string]any
	// fail answers a page, named country/kind/cursor, with a status.
	fail map[string]int
	// noCursor leaves nextCursor out of a page that has more after it.
	noCursor bool
	// before, when set, is called with each page's name as it is asked.
	before func(page string)
	asks   []feedAsk
}

type feedChange struct {
	show string
	at   time.Time
}

type feedAsk struct {
	country, kind, cursor string
	from, to              time.Time
}

func (f *changesFeed) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	if r.URL.Path != "/changes" || q.Get("item_type") != "show" || q.Get("show_type") != "movie" ||
		q.Get("order_direction") != "asc" || r.Header.Get("X-API-Key") != "test-key-not-real" {
		f.t.Errorf("asked %s with key %q", r.URL, r.Header.Get("X-API-Key"))
	}
	from, _ := strconv.ParseInt(q.Get("from"), 10, 64)
	to, _ := strconv.ParseInt(q.Get("to"), 10, 64)
	ask := feedAsk{country: q.Get("country"), kind: q.Get("change_type"), cursor: q.Get("cursor"),
		from: time.Unix(from, 0).UTC(), to: time.Unix(to, 0).UTC()}
	f.mu.Lock()
	defer f.mu.Unlock()
	f.asks = append(f.asks, ask)
	if f.before != nil {
		f.before(ask.country + "/" + ask.kind + "/" + ask.cursor)
	}
	if status, ok := f.fail[ask.country+"/"+ask.kind+"/"+ask.cursor]; ok {
		http.Error(w, `{"message":"failing on purpose"}`, status)
		return
	}
	var in []feedChange
	for _, c := range f.changes[ask.country+"/"+ask.kind] {
		if !c.at.Before(ask.from) && !c.at.After(ask.to) {
			in = append(in, c)
		}
	}
	start, _ := strconv.Atoi(ask.cursor)
	start = min(start, len(in))
	end := min(start+f.pageSize, len(in))
	changes := []map[string]any{}
	shows := map[string]any{}
	for _, c := range in[start:end] {
		changes = append(changes, map[string]any{
			"changeType": ask.kind, "itemType": "show", "showId": c.show, "showType": "movie",
			"season": nil, "episode": nil, "timestamp": c.at.Unix(),
		})
		if show, ok := f.shows[c.show]; ok {
			shows[c.show] = show
		}
	}
	page := map[string]any{"changes": changes, "shows": shows, "hasMore": end < len(in)}
	if end < len(in) && !f.noCursor {
		page["nextCursor"] = strconv.Itoa(end)
	}
	_ = json.NewEncoder(w).Encode(page)
}

// show adds a show the feed carries. Nil options is a show without the
// field at all.
func (f *changesFeed) show(id, imdbID string, options map[string][]streaming.StreamingOption) {
	f.mu.Lock()
	defer f.mu.Unlock()
	show := map[string]any{"itemType": "show", "showType": "movie", "id": id, "imdbId": imdbID}
	if options != nil {
		show["streamingOptions"] = options
	}
	f.shows[id] = show
}

// add appends changes of one kind in one country, in time order.
func (f *changesFeed) add(country, kind string, changes ...feedChange) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.changes[country+"/"+kind] = append(f.changes[country+"/"+kind], changes...)
}

func (f *changesFeed) failPage(page string, status int) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.fail[page] = status
}

func (f *changesFeed) clearFailures() {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.fail = map[string]int{}
}

// taken is every page asked for since the last time, and forgets them.
func (f *changesFeed) taken() []feedAsk {
	f.mu.Lock()
	defer f.mu.Unlock()
	asks := f.asks
	f.asks = nil
	return asks
}

// changesFixture is watchFixture with the country list filled and the
// service's feed pointed at a stand-in.
func changesFixture(t *testing.T) (*Store, *WhereToWatch, *Queue, *fakeStreaming, *changesFeed) {
	t.Helper()
	feed := &changesFeed{t: t, pageSize: 25, changes: map[string][]feedChange{},
		shows: map[string]map[string]any{}, fail: map[string]int{}}
	srv := httptest.NewServer(feed)
	t.Cleanup(srv.Close)
	api := &fakeStreaming{t: t, countries: coveredCountries,
		feed: streaming.New("test-key-not-real", streaming.WithBaseURL(srv.URL), streaming.WithRate(1000))}
	s, w, q := watchFixture(t, api)
	if err := s.KeepStreamingCountries(context.Background(), coveredCountries); err != nil {
		t.Fatal(err)
	}
	return s, w, q, api, feed
}

// keepAnswer keeps an answer the way a reader's first ask does.
func keepAnswer(t *testing.T, w *WhereToWatch, tconst, country string, options []streaming.StreamingOption) {
	t.Helper()
	now := time.Now()
	if err := w.keep(context.Background(), tconst, country, streaming.Shape(options, now), streaming.NextExpiry(options, now)); err != nil {
		t.Fatal(err)
	}
}

func subscription(id, name string) []streaming.StreamingOption {
	return []streaming.StreamingOption{{Service: service(id, name), Type: "subscription", Link: "https://" + id + "/title"}}
}

func streamNames(t *testing.T, s *Store, tconst, country string) []string {
	t.Helper()
	row, err := s.WhereToWatch(context.Background(), tconst, country)
	if err != nil {
		t.Fatal(err)
	}
	if !row.Stored {
		return nil
	}
	names := []string{}
	for _, o := range row.Answer.Stream {
		names = append(names, o.Name)
	}
	return names
}

func syncRow(t *testing.T, s *Store, country string) (syncedTo, ranAt time.Time, ok bool) {
	t.Helper()
	err := s.pool.QueryRow(context.Background(),
		`SELECT synced_to, updated_at FROM meta.streaming_sync WHERE country = $1`, country).Scan(&syncedTo, &ranAt)
	if err != nil {
		return time.Time{}, time.Time{}, false
	}
	return syncedTo, ranAt, true
}

func runFor(runs []changesRun, country string) (changesRun, bool) {
	for _, r := range runs {
		if r.Country == country {
			return r, true
		}
	}
	return changesRun{}, false
}

func near(a, b time.Time) bool { return a.Sub(b).Abs() <= 5*time.Second }

// TestAChangedMovieIsWrittenFromTheFeedAndNothingElseIsTouched: a change
// to a movie kept for the country is written from the feed's own options,
// its leaving option scheduled; one whose show came without options for
// the country is refreshed instead; and changes to anything not kept for
// that country cost the page they came on and nothing more.
func TestAChangedMovieIsWrittenFromTheFeedAndNothingElseIsTouched(t *testing.T) {
	s, w, q, api, feed := changesFixture(t)
	ctx := context.Background()
	keepAnswer(t, w, "tt0133093", "us", netflixOnly())
	keepAnswer(t, w, "tt0111161", "us", netflixOnly())
	keepAnswer(t, w, "tt0234215", "gb", netflixOnly())

	base := time.Now().Add(-12 * time.Hour).Truncate(time.Second).UTC()
	leaves := time.Now().Add(72 * time.Hour).Truncate(time.Second).UTC()
	feed.show("s1", "tt0133093", map[string][]streaming.StreamingOption{"us": {
		{Service: service("hulu", "Hulu"), Type: "subscription", Link: "h", ExpiresOn: leaves.Unix(), ExpiresSoon: true},
	}})
	feed.show("s2", "tt0234215", map[string][]streaming.StreamingOption{"us": subscription("max", "Max")})
	feed.show("s3", "tt7777777", map[string][]streaming.StreamingOption{"us": subscription("max", "Max")})
	feed.show("s4", "tt0111161", map[string][]streaming.StreamingOption{"gb": subscription("max", "Max")})
	feed.show("s5", "tt0000001", nil)
	feed.add("us", streaming.ChangeNew, feedChange{"s1", base.Add(time.Minute)})
	feed.add("us", streaming.ChangeUpdated,
		feedChange{"s2", base.Add(2 * time.Minute)}, feedChange{"s3", base.Add(3 * time.Minute)}, feedChange{"s4", base.Add(4 * time.Minute)})
	feed.add("us", streaming.ChangeRemoved, feedChange{"s5", base.Add(5 * time.Minute)})
	feed.add("gb", streaming.ChangeUpdated, feedChange{"s1", base.Add(2 * time.Minute)})

	runs, err := w.syncChanges(ctx)
	if err != nil {
		t.Fatal(err)
	}
	us, ok := runFor(runs, "us")
	if !ok || us.Pages != 3 || us.Changes != 5 || us.Written != 1 || us.Queued != 1 || us.Capped || !us.SyncedTo.Equal(us.To) {
		t.Errorf("us = %+v", us)
	}
	gb, ok := runFor(runs, "gb")
	if !ok || gb.Pages != 3 || gb.Changes != 1 || gb.Written != 0 || gb.Queued != 0 {
		t.Errorf("gb = %+v", gb)
	}

	if got := streamNames(t, s, "tt0133093", "us"); len(got) != 1 || got[0] != "Hulu" {
		t.Errorf("the changed movie = %v, want Hulu from the feed", got)
	}
	if row, _ := s.WhereToWatch(ctx, "tt0133093", "us"); !row.ExpiresAt.Equal(leaves) {
		t.Errorf("expires_at = %v, want %v", row.ExpiresAt, leaves)
	}
	if got := streamNames(t, s, "tt0111161", "us"); len(got) != 1 || got[0] != "Netflix" {
		t.Errorf("a show without options for us was written: %v", got)
	}
	if got := streamNames(t, s, "tt0234215", "gb"); len(got) != 1 || got[0] != "Netflix" {
		t.Errorf("a change in us reached gb: %v", got)
	}
	for _, pair := range [][2]string{{"tt0234215", "us"}, {"tt7777777", "us"}, {"tt0000001", "us"}, {"tt0133093", "gb"}} {
		if got := streamNames(t, s, pair[0], pair[1]); got != nil {
			t.Errorf("%s/%s was kept from the feed: %v", pair[0], pair[1], got)
		}
	}
	if c := api.calls(); len(c) != 0 {
		t.Errorf("the API was asked about shows: %v", c)
	}

	// The written answer's leaving option has its job; the show without
	// options has one refresh, as soon as possible.
	var scheduled, soon []WatchRefreshArgs
	var soonJob *river.Job[WatchRefreshArgs]
	for _, j := range refreshJobs(t, q) {
		args := refreshArgs(t, j)
		if args.At.IsZero() {
			soon = append(soon, args)
			soonJob = &river.Job[WatchRefreshArgs]{JobRow: j, Args: args}
			if j.State != rivertype.JobStateAvailable {
				t.Errorf("the refresh is %s, want available", j.State)
			}
			continue
		}
		scheduled = append(scheduled, args)
		if j.State != rivertype.JobStateScheduled || !j.ScheduledAt.Equal(leaves) {
			t.Errorf("the expiry's job is %s at %v, want scheduled at %v", j.State, j.ScheduledAt, leaves)
		}
	}
	if len(scheduled) != 1 || scheduled[0].Tconst != "tt0133093" || scheduled[0].Country != "us" || !scheduled[0].At.Equal(leaves) {
		t.Errorf("scheduled = %+v", scheduled)
	}
	if len(soon) != 1 || soon[0].Tconst != "tt0111161" || soon[0].Country != "us" {
		t.Fatalf("refreshes = %+v, want one for tt0111161/us", soon)
	}

	// Working that refresh asks the API once, and keeps its answer.
	api.set("tt0111161/us", subscription("max", "Max"), nil)
	if err := (&watchRefreshWorker{w: w}).Work(ctx, soonJob); err != nil {
		t.Fatal(err)
	}
	if got := streamNames(t, s, "tt0111161", "us"); len(got) != 1 || got[0] != "Max" || len(api.calls()) != 1 {
		t.Errorf("after the refresh = %v, calls %v", got, api.calls())
	}
}

// TestAMovieChangedSeveralTimesInARunIsHandledOnce, across pages and
// across kinds of change: how it stands now is the same however many
// changes brought it there.
func TestAMovieChangedSeveralTimesInARunIsHandledOnce(t *testing.T) {
	_, w, q, api, feed := changesFixture(t)
	ctx := context.Background()
	feed.pageSize = 2
	keepAnswer(t, w, "tt0133093", "us", netflixOnly())
	keepAnswer(t, w, "tt0111161", "us", netflixOnly())
	base := time.Now().Add(-12 * time.Hour).Truncate(time.Second).UTC()
	feed.show("s1", "tt0133093", map[string][]streaming.StreamingOption{"us": subscription("hulu", "Hulu")})
	feed.show("s4", "tt0111161", nil)
	feed.add("us", streaming.ChangeNew,
		feedChange{"s1", base.Add(1 * time.Minute)}, feedChange{"s4", base.Add(2 * time.Minute)}, feedChange{"s1", base.Add(3 * time.Minute)})
	feed.add("us", streaming.ChangeUpdated, feedChange{"s1", base.Add(4 * time.Minute)}, feedChange{"s4", base.Add(5 * time.Minute)})
	feed.add("us", streaming.ChangeRemoved, feedChange{"s1", base.Add(6 * time.Minute)})

	runs, err := w.syncChanges(ctx)
	if err != nil {
		t.Fatal(err)
	}
	us, _ := runFor(runs, "us")
	if us.Pages != 4 || us.Changes != 6 || us.Written != 1 || us.Queued != 1 {
		t.Errorf("us = %+v, want 4 pages, 6 changes, one write and one refresh", us)
	}
	if jobs := refreshJobs(t, q); len(jobs) != 1 {
		t.Errorf("refreshes = %d, want one", len(jobs))
	}
	if c := api.calls(); len(c) != 0 {
		t.Errorf("calls = %v", c)
	}
}

// TestTheChangesWindowStartsWhereTheLastRunGotTo: a first run starts a
// day back, or at the oldest kept answer when that is older; a country is
// read again a day after its last run, from where that run got to; and a
// start older than the feed keeps is cut to its 31 days, and said.
func TestTheChangesWindowStartsWhereTheLastRunGotTo(t *testing.T) {
	s, w, _, _, feed := changesFixture(t)
	ctx := context.Background()
	keepAnswer(t, w, "tt0133093", "us", netflixOnly())

	runs, err := w.syncChanges(ctx)
	if err != nil {
		t.Fatal(err)
	}
	first, _ := runFor(runs, "us")
	asks := feed.taken()
	if len(asks) != 3 {
		t.Fatalf("asks = %+v, want one page of each kind", asks)
	}
	kinds := map[string]bool{}
	for _, a := range asks {
		kinds[a.kind] = true
		if a.country != "us" || a.cursor != "" || !near(a.from, time.Now().Add(-ChangesFirstReach)) || !near(a.to, time.Now()) {
			t.Errorf("first run asked %+v, want a day back to now", a)
		}
	}
	if len(kinds) != 3 {
		t.Errorf("kinds = %v", kinds)
	}
	syncedTo, ranAt, ok := syncRow(t, s, "us")
	if !ok || !syncedTo.Equal(first.To) || !near(ranAt, time.Now()) {
		t.Errorf("sync row = %v, %v, %v; want the run's end", syncedTo, ranAt, ok)
	}

	// Inside the day, a look reads nothing.
	for _, ago := range []string{"0 hours", "23 hours"} {
		if _, err := s.pool.Exec(ctx, `UPDATE meta.streaming_sync SET updated_at = now() - $1::interval`, ago); err != nil {
			t.Fatal(err)
		}
		if runs, err := w.syncChanges(ctx); err != nil || len(runs) != 0 {
			t.Errorf("%s after the last run: runs %+v, %v", ago, runs, err)
		}
		if asks := feed.taken(); len(asks) != 0 {
			t.Errorf("%s after the last run, asked %+v", ago, asks)
		}
	}

	// A day on, it carries on from where the last run got to.
	if _, err := s.pool.Exec(ctx, `UPDATE meta.streaming_sync SET updated_at = now() - interval '24 hours'`); err != nil {
		t.Fatal(err)
	}
	if _, err := w.syncChanges(ctx); err != nil {
		t.Fatal(err)
	}
	for _, a := range feed.taken() {
		if !a.from.Equal(first.To) {
			t.Errorf("the next run asked from %v, want %v", a.from, first.To)
		}
	}

	// A last run 40 days ago is further back than the feed goes.
	if _, err := s.pool.Exec(ctx,
		`UPDATE meta.streaming_sync SET synced_to = now() - interval '40 days', updated_at = now() - interval '2 days'`); err != nil {
		t.Fatal(err)
	}
	var log logSink
	w.Logger = log.logger()
	runs, err = w.syncChanges(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if run, _ := runFor(runs, "us"); !run.Clamped {
		t.Errorf("run = %+v, want it clamped", run)
	}
	for _, a := range feed.taken() {
		if !near(a.from, time.Now().Add(-ChangesReach+changesReachMargin)) {
			t.Errorf("a 40-day gap asked from %v", a.from)
		}
	}
	if out := log.String(); !strings.Contains(out, "level=WARN") || !strings.Contains(out, "31 days") {
		t.Errorf("the gap was not logged: %q", out)
	}

	// A first run with an answer kept 10 days ago starts there.
	if _, err := s.pool.Exec(ctx, `DELETE FROM meta.streaming_sync`); err != nil {
		t.Fatal(err)
	}
	if _, err := s.pool.Exec(ctx, `UPDATE meta.where_to_watch SET fetched_at = now() - interval '10 days'`); err != nil {
		t.Fatal(err)
	}
	if _, err := w.syncChanges(ctx); err != nil {
		t.Fatal(err)
	}
	for _, a := range feed.taken() {
		if !near(a.from, time.Now().Add(-10*24*time.Hour)) {
			t.Errorf("a first run with a 10-day-old answer asked from %v", a.from)
		}
	}
}

// TestThePageCapStopsARunAndTheNextCarriesOn from the last change the
// stopped run handled, so nothing between is skipped.
func TestThePageCapStopsARunAndTheNextCarriesOn(t *testing.T) {
	s, w, _, _, feed := changesFixture(t)
	ctx := context.Background()
	feed.pageSize = 2
	w.ChangesMaxPages = 4
	movies := []string{"tt0133093", "tt0111161", "tt0234215", "tt0000001", "tt0000002"}
	for i, tconst := range movies {
		keepAnswer(t, w, tconst, "us", netflixOnly())
		feed.show("s"+strconv.Itoa(i), tconst, map[string][]streaming.StreamingOption{"us": subscription("hulu", "Hulu")})
	}
	base := time.Now().Add(-12 * time.Hour).Truncate(time.Second).UTC()
	at := func(i int) time.Time { return base.Add(time.Duration(i) * time.Minute) }
	feed.add("us", streaming.ChangeUpdated,
		feedChange{"s0", at(1)}, feedChange{"s1", at(2)}, feedChange{"s2", at(3)},
		feedChange{"s3", at(4)}, feedChange{"s4", at(5)}, feedChange{"s0", at(6)})

	var log logSink
	w.Logger = log.logger()
	runs, err := w.syncChanges(ctx)
	if err != nil {
		t.Fatal(err)
	}
	first, _ := runFor(runs, "us")
	if first.Pages != 4 || !first.Capped || first.Written != 4 || !first.SyncedTo.Equal(at(4)) {
		t.Errorf("first run = %+v, want 4 pages, capped, 4 written, synced to %v", first, at(4))
	}
	if syncedTo, _, _ := syncRow(t, s, "us"); !syncedTo.Equal(at(4)) {
		t.Errorf("synced_to = %v, want %v", syncedTo, at(4))
	}
	if got := streamNames(t, s, "tt0000002", "us"); len(got) != 1 || got[0] != "Netflix" {
		t.Errorf("a change past the cap was handled: %v", got)
	}
	if out := log.String(); !strings.Contains(out, "page cap") || !strings.Contains(out, "pages=4") {
		t.Errorf("the capped run was not logged: %q", out)
	}
	feed.taken()

	if _, err := s.pool.Exec(ctx, `UPDATE meta.streaming_sync SET updated_at = now() - interval '25 hours'`); err != nil {
		t.Fatal(err)
	}
	runs, err = w.syncChanges(ctx)
	if err != nil {
		t.Fatal(err)
	}
	next, _ := runFor(runs, "us")
	if next.Capped || next.Pages != 4 || next.Written != 3 || !next.SyncedTo.Equal(next.To) {
		t.Errorf("next run = %+v, want it to finish with 3 written", next)
	}
	for _, a := range feed.taken() {
		if !a.from.Equal(at(4)) {
			t.Errorf("the next run asked %+v, want from %v", a, at(4))
		}
	}
	if got := streamNames(t, s, "tt0000002", "us"); len(got) != 1 || got[0] != "Hulu" {
		t.Errorf("the next run did not carry on: %v", got)
	}
}

// TestACapSpentInsideOneSecondMovesPastIt: starting from that second
// again would read the same pages every day.
func TestACapSpentInsideOneSecondMovesPastIt(t *testing.T) {
	s, w, _, _, feed := changesFixture(t)
	ctx := context.Background()
	feed.pageSize = 2
	w.ChangesMaxPages = 3
	from := time.Now().Add(-48 * time.Hour).Truncate(time.Second).UTC()
	for i, tconst := range []string{"tt0133093", "tt0111161", "tt0234215"} {
		keepAnswer(t, w, tconst, "us", netflixOnly())
		feed.show("s"+strconv.Itoa(i), tconst, map[string][]streaming.StreamingOption{"us": subscription("hulu", "Hulu")})
		feed.add("us", streaming.ChangeUpdated, feedChange{"s" + strconv.Itoa(i), from})
	}
	if _, err := s.pool.Exec(ctx, `INSERT INTO meta.streaming_sync VALUES ('us', $1, now() - interval '2 days')`, from); err != nil {
		t.Fatal(err)
	}
	var log logSink
	w.Logger = log.logger()
	runs, err := w.syncChanges(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if run, _ := runFor(runs, "us"); !run.Capped || !run.SyncedTo.Equal(from.Add(time.Second)) {
		t.Errorf("run = %+v, want capped and synced a second on", run)
	}
	if out := log.String(); !strings.Contains(out, "inside one second") {
		t.Errorf("moving past the second was not logged: %q", out)
	}
}

// TestAFailingChangesPageLeavesTheSyncWhereItWas: the run ends there.
// Changes handled before it, in every kind, are recorded, and nothing
// else. A run that read a page waits a day even when it got nowhere, so
// a page that keeps failing does not have the pages before it read again
// on every try; one that read none, or was stopped, is tried again. A
// refused key cancels the job rather than retrying it.
func TestAFailingChangesPageLeavesTheSyncWhereItWas(t *testing.T) {
	s, w, _, _, feed := changesFixture(t)
	ctx := context.Background()
	keepAnswer(t, w, "tt0133093", "us", netflixOnly())
	keepAnswer(t, w, "tt0111161", "us", netflixOnly())
	from := time.Now().Add(-48 * time.Hour).Truncate(time.Second).UTC()
	if _, err := s.pool.Exec(ctx, `INSERT INTO meta.streaming_sync VALUES ('us', $1, now() - interval '2 days')`, from); err != nil {
		t.Fatal(err)
	}
	_, ranBefore, _ := syncRow(t, s, "us")
	feed.show("s1", "tt0133093", map[string][]streaming.StreamingOption{"us": subscription("hulu", "Hulu")})
	feed.show("s2", "tt0111161", map[string][]streaming.StreamingOption{"us": subscription("hulu", "Hulu")})
	feed.add("us", streaming.ChangeNew, feedChange{"s1", from.Add(10 * time.Second)})
	feed.add("us", streaming.ChangeUpdated, feedChange{"s2", from.Add(20 * time.Second)}, feedChange{"s1", from.Add(30 * time.Second)})

	// The very first page failing read nothing, so nothing is recorded
	// and the next try asks again.
	feed.failPage("us/new/", http.StatusServiceUnavailable)
	var log logSink
	w.Logger = log.logger()
	worker := &changesWorker{w: w}
	err := worker.Work(ctx, nil)
	var cancel *river.JobCancelError
	if err == nil || errors.As(err, &cancel) {
		t.Errorf("a failing page: err = %v, want one River retries", err)
	}
	if out := log.String(); !strings.Contains(out, "level=WARN") || !strings.Contains(out, "HTTP 503") || !strings.Contains(out, "changes in us") {
		t.Errorf("the failure was not logged: %q", out)
	}
	if syncedTo, ranAt, _ := syncRow(t, s, "us"); !syncedTo.Equal(from) || !ranAt.Equal(ranBefore) {
		t.Errorf("after a failing first page: synced_to %v, updated_at %v; want both as they were", syncedTo, ranAt)
	}

	// A run stopped part way is not the page's fault either.
	feed.clearFailures()
	stopped, stop := context.WithCancel(ctx)
	defer stop()
	feed.before = func(page string) {
		if page == "us/removed/" {
			stop()
		}
	}
	feed.failPage("us/removed/", http.StatusServiceUnavailable)
	if _, err := w.syncChanges(stopped); !errors.Is(err, context.Canceled) {
		t.Errorf("a stopped run: err = %v, want it stopped", err)
	}
	feed.before = nil
	if syncedTo, ranAt, _ := syncRow(t, s, "us"); !syncedTo.Equal(from) || !ranAt.Equal(ranBefore) {
		t.Errorf("after a stopped run: synced_to %v, updated_at %v; want both as they were", syncedTo, ranAt)
	}

	// A first page failing after others were read records the run where
	// it started, and the country waits a day.
	feed.clearFailures()
	feed.failPage("us/updated/", http.StatusServiceUnavailable)
	feed.taken()
	if err := worker.Work(ctx, nil); err == nil || errors.As(err, &cancel) {
		t.Errorf("a failing page after others: err = %v, want one River retries", err)
	}
	if syncedTo, ranAt, _ := syncRow(t, s, "us"); !syncedTo.Equal(from) || !near(ranAt, time.Now()) {
		t.Errorf("after a failing page after others: synced_to %v, updated_at %v; want %v and now", syncedTo, ranAt, from)
	}
	feed.taken()
	if err := worker.Work(ctx, nil); err != nil {
		t.Errorf("the try after: %v", err)
	}
	if asks := feed.taken(); len(asks) != 0 {
		t.Errorf("the try after read the pages again: %+v", asks)
	}

	// A later page failing keeps what every kind got past before it.
	if _, err := s.pool.Exec(ctx, `UPDATE meta.streaming_sync SET updated_at = now() - interval '2 days'`); err != nil {
		t.Fatal(err)
	}
	feed.pageSize = 1
	feed.clearFailures()
	feed.failPage("us/updated/1", http.StatusServiceUnavailable)
	if err := worker.Work(ctx, nil); err == nil {
		t.Error("a failing second page succeeded")
	}
	if syncedTo, _, _ := syncRow(t, s, "us"); !syncedTo.Equal(from.Add(20 * time.Second)) {
		t.Errorf("after a failing second page: synced_to %v, want %v", syncedTo, from.Add(20*time.Second))
	}

	// A refused key.
	if _, err := s.pool.Exec(ctx, `UPDATE meta.streaming_sync SET updated_at = now() - interval '2 days'`); err != nil {
		t.Fatal(err)
	}
	syncedBefore, _, _ := syncRow(t, s, "us")
	feed.clearFailures()
	feed.failPage("us/new/", http.StatusUnauthorized)
	log.reset()
	feed.taken()
	err = worker.Work(ctx, nil)
	if !errors.As(err, &cancel) || !errors.Is(err, streaming.ErrKey) {
		t.Errorf("a refused key: err = %v, want a cancelled job", err)
	}
	if asks := feed.taken(); len(asks) != 1 {
		t.Errorf("a refused key was asked again: %+v", asks)
	}
	if out := log.String(); !strings.Contains(out, "level=ERROR") || !strings.Contains(out, "refused") {
		t.Errorf("a refused key was not logged as an error: %q", out)
	}
	if syncedTo, _, _ := syncRow(t, s, "us"); !syncedTo.Equal(syncedBefore) {
		t.Errorf("a refused key moved synced_to to %v", syncedTo)
	}
}

// TestAChangeToAShowThePageDidNotCarryIsAWarning: it cannot be matched to
// a movie, so the answer it changed is left until it is WatchFresh old,
// and the log says so rather than passing it over quietly.
func TestAChangeToAShowThePageDidNotCarryIsAWarning(t *testing.T) {
	s, w, _, _, feed := changesFixture(t)
	ctx := context.Background()
	keepAnswer(t, w, "tt0133093", "us", netflixOnly())
	base := time.Now().Add(-12 * time.Hour).Truncate(time.Second).UTC()
	feed.show("s1", "tt0133093", map[string][]streaming.StreamingOption{"us": subscription("hulu", "Hulu")})
	feed.show("s2", "not-an-imdb-id", map[string][]streaming.StreamingOption{"us": subscription("hulu", "Hulu")})
	feed.add("us", streaming.ChangeUpdated,
		feedChange{"s1", base.Add(time.Minute)}, feedChange{"gone", base.Add(2 * time.Minute)}, feedChange{"s2", base.Add(3 * time.Minute)})

	var log logSink
	w.Logger = log.logger()
	runs, err := w.syncChanges(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if us, _ := runFor(runs, "us"); us.Written != 1 || us.Unmatched != 2 || !us.SyncedTo.Equal(us.To) {
		t.Errorf("us = %+v, want one written and two unmatched", us)
	}
	if out := log.String(); !strings.Contains(out, "level=WARN") || !strings.Contains(out, "did not carry") || !strings.Contains(out, "unmatched=2") {
		t.Errorf("the unmatched changes were not a warning: %q", out)
	}

	// A run where every change matched says nothing of it.
	log.reset()
	if _, err := s.pool.Exec(ctx, `DELETE FROM meta.streaming_sync`); err != nil {
		t.Fatal(err)
	}
	feed.mu.Lock()
	feed.changes = map[string][]feedChange{}
	feed.mu.Unlock()
	feed.add("us", streaming.ChangeUpdated, feedChange{"s1", base.Add(time.Minute)})
	if _, err := w.syncChanges(ctx); err != nil {
		t.Fatal(err)
	}
	if out := log.String(); strings.Contains(out, "level=WARN") || !strings.Contains(out, "unmatched=0") {
		t.Errorf("a run where every change matched: %q", out)
	}
}

// TestAPageWithMoreAndNoCursorIsAWarning: that feed stops there for the
// run, and a feed that keeps doing it falls behind, so it is said.
func TestAPageWithMoreAndNoCursorIsAWarning(t *testing.T) {
	s, w, _, _, feed := changesFixture(t)
	ctx := context.Background()
	feed.pageSize = 1
	feed.noCursor = true
	keepAnswer(t, w, "tt0133093", "us", netflixOnly())
	feed.show("s1", "tt0133093", map[string][]streaming.StreamingOption{"us": subscription("hulu", "Hulu")})
	first := time.Now().Add(-20 * time.Hour).Truncate(time.Second).UTC()
	feed.add("us", streaming.ChangeUpdated, feedChange{"s1", first}, feedChange{"s1", first.Add(time.Hour)})

	var log logSink
	w.Logger = log.logger()
	runs, err := w.syncChanges(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if us, _ := runFor(runs, "us"); us.Capped || us.Pages != 3 || !us.SyncedTo.Equal(first) {
		t.Errorf("us = %+v, want 3 pages, not capped, synced to %v", us, first)
	}
	if syncedTo, _, _ := syncRow(t, s, "us"); !syncedTo.Equal(first) {
		t.Errorf("synced_to = %v, want %v", syncedTo, first)
	}
	if out := log.String(); !strings.Contains(out, "level=WARN") || !strings.Contains(out, "gave no cursor") ||
		!strings.Contains(out, "change_type=updated") {
		t.Errorf("the missing cursor was not a warning: %q", out)
	}
}

// TestOnlyCoveredCountriesWithKeptAnswersAreRead: a country nobody has
// kept an answer for costs nothing, and neither does one the API does not
// cover.
func TestOnlyCoveredCountriesWithKeptAnswersAreRead(t *testing.T) {
	s, w, _, _, feed := changesFixture(t)
	ctx := context.Background()
	if runs, err := w.syncChanges(ctx); err != nil || len(runs) != 0 {
		t.Errorf("with nothing kept: runs %+v, %v", runs, err)
	}
	if asks := feed.taken(); len(asks) != 0 {
		t.Errorf("with nothing kept, asked %+v", asks)
	}

	keepAnswer(t, w, "tt0133093", "us", netflixOnly())
	if _, err := s.pool.Exec(ctx, `
		INSERT INTO meta.where_to_watch (tconst, country, answer, fetched_at)
		VALUES ('tt0133093', 'ng', '{"stream":[],"free":[],"rent":[],"buy":[]}', now())`); err != nil {
		t.Fatal(err)
	}
	runs, err := w.syncChanges(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(runs) != 1 || runs[0].Country != "us" {
		t.Errorf("runs = %+v, want us alone", runs)
	}
	for _, a := range feed.taken() {
		if a.country != "us" {
			t.Errorf("asked about %s", a.country)
		}
	}
	if _, _, ok := syncRow(t, s, "ng"); ok {
		t.Error("an uncovered country was recorded")
	}
}

// TestTheChangesCapHasAFloorOfOnePagePerKind: a country's progress is the
// least any of its feeds made, so a cap that left one of them unread
// would leave the country where it was, every day.
func TestTheChangesCapHasAFloorOfOnePagePerKind(t *testing.T) {
	for set, want := range map[int]int{0: DefaultChangesMaxPages, -1: DefaultChangesMaxPages, 1: 3, 3: 3, 12: 12} {
		if got := (&WhereToWatch{ChangesMaxPages: set}).changesMaxPages(); got != want {
			t.Errorf("ChangesMaxPages %d = %d pages, want %d", set, got, want)
		}
	}
}

// TestOneChangesRunAtATime: the look is unique among the jobs not yet
// finished, so a new leader's run-on-start, or the next hour's, cannot
// stack a second read of the same changes on one already queued.
func TestOneChangesRunAtATime(t *testing.T) {
	_, _, q, _, _ := changesFixture(t)
	ctx := context.Background()
	first, err := q.client.Insert(ctx, StreamingChangesArgs{}, nil)
	if err != nil || first.UniqueSkippedAsDuplicate {
		t.Fatalf("first insert = %+v, %v", first, err)
	}
	second, err := q.client.Insert(ctx, StreamingChangesArgs{}, nil)
	if err != nil || !second.UniqueSkippedAsDuplicate {
		t.Errorf("second insert = %+v, %v; want it skipped as a duplicate", second, err)
	}
	rivertest.RequireInserted(ctx, t, q.driver(), StreamingChangesArgs{},
		&rivertest.RequireInsertedOpts{Schema: QueueSchema, MaxAttempts: queuePeriodicTries})
}

// TestTheQueueReadsTheChangesWhenStarted: end to end, a started queue's
// leader queues the look on start, and it writes the change.
func TestTheQueueReadsTheChangesWhenStarted(t *testing.T) {
	s, w, q, _, feed := changesFixture(t)
	ctx := context.Background()
	keepAnswer(t, w, "tt0133093", "us", netflixOnly())
	feed.show("s1", "tt0133093", map[string][]streaming.StreamingOption{"us": subscription("hulu", "Hulu")})
	feed.add("us", streaming.ChangeUpdated, feedChange{"s1", time.Now().Add(-time.Hour).Truncate(time.Second).UTC()})
	if err := q.Start(ctx); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(30 * time.Second)
	for {
		if got := streamNames(t, s, "tt0133093", "us"); len(got) == 1 && got[0] == "Hulu" {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("the started queue never read the changes")
		}
		time.Sleep(50 * time.Millisecond)
	}
	stopCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	q.Stop(stopCtx)
}
