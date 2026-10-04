package catalog

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/riverqueue/river"
	"github.com/riverqueue/river/rivertype"

	"cinedikt/internal/geoip"
	"cinedikt/internal/geoip/geoiptest"
	"cinedikt/internal/notify"
)

// geoIPJob is a run of the periodic check as River hands it to the
// worker: try attempt of queuePeriodicTries, queued at created.
func geoIPJob(attempt int, created time.Time) *river.Job[GeoIPCheckArgs] {
	return &river.Job[GeoIPCheckArgs]{JobRow: &rivertype.JobRow{
		Kind: GeoIPCheckArgs{}.Kind(), Attempt: attempt, MaxAttempts: queuePeriodicTries, CreatedAt: created,
	}}
}

// geoIPFixture is a worker reporting to a recording sink, against a
// stand-in for MaxMind serving a first build, with the database kept in
// memory.
func geoIPFixture(t *testing.T, built time.Time) (*geoIPWorker, *recordingSink, *geoiptest.Server, *memGeoIP, []byte) {
	t.Helper()
	mmdb := geoiptest.Database(t, map[string]string{"81.2.69.0/24": "GB"})
	srv := geoiptest.NewServer(t, geoiptest.Archive(t, mmdb), built)
	store := &memGeoIP{}
	sink := &recordingSink{}
	u := &geoip.Updater{
		AccountID: geoiptest.Account, LicenseKey: geoiptest.License,
		Store: store, Lookup: &geoip.Lookup{}, HTTP: srv.Client(),
		PermalinkURL: srv.PermalinkURL(), LegacyURL: srv.LegacyURL(), Logger: quietLogger(),
	}
	return &geoIPWorker{u: u, notify: sink}, sink, srv, store, mmdb
}

// TestAGeoIPCheckReportsEachNewBuildOnce: the check that downloads a
// build says so once, with when MaxMind built it, its size, and the build
// it replaced; the first build replaces none.
func TestAGeoIPCheckReportsEachNewBuildOnce(t *testing.T) {
	built := time.Date(2026, 9, 25, 14, 43, 7, 0, time.UTC)
	w, sink, srv, _, mmdb := geoIPFixture(t, built)
	ctx := context.Background()

	if err := w.Work(ctx, geoIPJob(1, time.Now())); err != nil {
		t.Fatal(err)
	}
	got := sink.all()
	if len(got) != 1 || got[0].Job != notify.JobGeoIP || got[0].Kind != notify.Downloaded {
		t.Fatalf("the first download: events = %+v, want one Downloaded", got)
	}
	if e := got[0]; !e.LiveSince.Equal(built) || e.Bytes != int64(len(mmdb)) || !e.PrevAt.IsZero() || e.At.IsZero() {
		t.Errorf("the first download = %+v; want built %v, %d bytes, nothing replaced, and a time", e, built, len(mmdb))
	}

	newer := built.Add(96 * time.Hour)
	ie := geoiptest.Database(t, map[string]string{"81.2.69.0/24": "IE"})
	srv.Set(geoiptest.Archive(t, ie), newer)
	if err := w.Work(ctx, geoIPJob(1, time.Now())); err != nil {
		t.Fatal(err)
	}
	downloads := sink.of(notify.Downloaded)
	if len(downloads) != 2 {
		t.Fatalf("after a second build: %d Downloaded events, want 2", len(downloads))
	}
	if e := downloads[1]; !e.LiveSince.Equal(newer) || !e.PrevAt.Equal(built) || e.Bytes != int64(len(ie)) {
		t.Errorf("the second download = %+v; want built %v in place of %v, %d bytes", e, newer, built, len(ie))
	}
}

// TestAGeoIPCheckThatFindsTheSameBuildIsNoNews: a check with an unchanged
// Last-Modified downloads nothing and reports no download, only that it
// checked, which is what moves the board's "checked" time.
func TestAGeoIPCheckThatFindsTheSameBuildIsNoNews(t *testing.T) {
	built := time.Date(2026, 9, 25, 14, 43, 7, 0, time.UTC)
	w, sink, srv, _, _ := geoIPFixture(t, built)
	ctx := context.Background()
	if err := w.Work(ctx, geoIPJob(1, time.Now())); err != nil {
		t.Fatal(err)
	}
	for range 2 {
		if err := w.Work(ctx, geoIPJob(1, time.Now())); err != nil {
			t.Fatal(err)
		}
	}
	if srv.Gets() != 1 {
		t.Errorf("%d GETs, want the one download", srv.Gets())
	}
	if n := len(sink.of(notify.Downloaded)); n != 1 {
		t.Errorf("%d Downloaded events, want only the first check's", n)
	}
	checked := sink.of(notify.Checked)
	if len(checked) != 2 {
		t.Fatalf("%d Checked events, want one for each check that found the same build", len(checked))
	}
	for _, e := range checked {
		if e.Job != notify.JobGeoIP || !e.LiveSince.Equal(built) {
			t.Errorf("checked = %+v, want the GeoIP check naming the build in use", e)
		}
	}
}

// TestASecondProcessHasNoNewsOfABuildAnotherKept: only the check that
// kept a build reports it. A process that loads the kept build, at start
// or at its own check, says nothing of a download, so one build is one
// message however many processes there are.
func TestASecondProcessHasNoNewsOfABuildAnotherKept(t *testing.T) {
	built := time.Date(2026, 9, 25, 14, 43, 7, 0, time.UTC)
	w, sink, srv, store, _ := geoIPFixture(t, built)
	ctx := context.Background()
	if err := w.Work(ctx, geoIPJob(1, time.Now())); err != nil {
		t.Fatal(err)
	}

	otherSink := &recordingSink{}
	other := &geoIPWorker{notify: otherSink, u: &geoip.Updater{
		AccountID: geoiptest.Account, LicenseKey: geoiptest.License,
		Store: store, Lookup: &geoip.Lookup{}, HTTP: srv.Client(),
		PermalinkURL: srv.PermalinkURL(), LegacyURL: srv.LegacyURL(),
	}}
	if err := other.u.LoadStored(ctx); err != nil {
		t.Fatal(err)
	}
	if got := otherSink.all(); len(got) != 0 {
		t.Fatalf("loading the kept build reported %+v", got)
	}
	if err := other.Work(ctx, geoIPJob(1, time.Now())); err != nil {
		t.Fatal(err)
	}
	if got := otherSink.all(); len(got) != 1 || got[0].Kind != notify.Checked || !got[0].LiveSince.Equal(built) {
		t.Errorf("the second process's check reported %+v, want one Checked and no download", got)
	}
	if n := len(sink.of(notify.Downloaded)) + len(otherSink.of(notify.Downloaded)); n != 1 {
		t.Errorf("%d Downloaded events between the two processes, want 1", n)
	}
}

// TestARefusedMaxMindKeyIsReportedAtOnce: River does not retry it, so the
// first try reports it, as a refused key the notifier names MaxMind's,
// with the next check as the next try and the build still in use.
func TestARefusedMaxMindKeyIsReportedAtOnce(t *testing.T) {
	built := time.Date(2026, 9, 25, 14, 43, 7, 0, time.UTC)
	w, sink, _, _, _ := geoIPFixture(t, built)
	ctx := context.Background()
	if err := w.Work(ctx, geoIPJob(1, time.Now())); err != nil {
		t.Fatal(err)
	}
	w.u.LicenseKey = "wrong"
	queued := time.Now().Add(-time.Minute)
	err := w.Work(ctx, geoIPJob(1, queued))
	var cancel *river.JobCancelError
	if !errors.As(err, &cancel) {
		t.Errorf("err = %v, want a cancelled job", err)
	}
	failed := sink.of(notify.Failed)
	if len(failed) != 1 {
		t.Fatalf("%d Failed events, want the one refusal", len(failed))
	}
	e := failed[0]
	if e.Job != notify.JobGeoIP || e.Cause != notify.KeyRejected || e.Provider != "MaxMind" {
		t.Errorf("failed = %+v, want MaxMind's key refused", e)
	}
	if !e.NextTry.Equal(queued.Add(GeoIPCheckEvery)) || !e.LiveSince.Equal(built) {
		t.Errorf("next try %v, build %v; want the next check and the build in use", e.NextTry, e.LiveSince)
	}
}

// TestAFailedGeoIPCheckIsReportedOnceRiverGivesUp: a try River repeats in
// a moment says nothing, so a check is one failure however many tries it
// took; the last try says why. A check cut short by a stop says nothing.
func TestAFailedGeoIPCheckIsReportedOnceRiverGivesUp(t *testing.T) {
	ctx := context.Background()
	down := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "maintenance", http.StatusServiceUnavailable)
	}))
	defer down.Close()
	sink := &recordingSink{}
	w := &geoIPWorker{notify: sink, u: &geoip.Updater{
		AccountID: geoiptest.Account, LicenseKey: geoiptest.License, Store: &memGeoIP{}, Lookup: &geoip.Lookup{},
		PermalinkURL: down.URL + geoiptest.PermalinkPath + "?suffix=tar.gz",
	}}
	queued := time.Now()
	for attempt := 1; attempt < queuePeriodicTries; attempt++ {
		if err := w.Work(ctx, geoIPJob(attempt, queued)); err == nil {
			t.Fatal("a check against a server that is down succeeded")
		}
	}
	if got := sink.all(); len(got) != 0 {
		t.Fatalf("tries River repeats reported %+v", got)
	}
	if err := w.Work(ctx, geoIPJob(queuePeriodicTries, queued)); err == nil {
		t.Fatal("the last try succeeded")
	}
	got := sink.all()
	if len(got) != 1 || got[0].Kind != notify.Failed || got[0].Cause != notify.ProviderDown ||
		got[0].Provider != "MaxMind" || got[0].Status != http.StatusServiceUnavailable {
		t.Fatalf("the last try reported %+v, want MaxMind down with its status", got)
	}
	if !got[0].LiveSince.IsZero() || !got[0].NextTry.Equal(queued.Add(GeoIPCheckEvery)) {
		t.Errorf("failed = %+v, want no build in use and the next check as the next try", got[0])
	}

	// A download that is wrong is not MaxMind being down.
	built := time.Date(2026, 9, 25, 14, 43, 7, 0, time.UTC)
	bad, badSink, srv, _, _ := geoIPFixture(t, built)
	srv.Set([]byte("<html>maintenance</html>"), built)
	if err := bad.Work(ctx, geoIPJob(queuePeriodicTries, queued)); err == nil {
		t.Fatal("a bad download was accepted")
	}
	if got := badSink.all(); len(got) != 1 || got[0].Cause != notify.Unknown {
		t.Errorf("a bad download reported %+v, want one failure of no known kind", got)
	}

	stopped, stop := context.WithCancel(ctx)
	stop()
	before := len(sink.all())
	if err := w.Work(stopped, geoIPJob(queuePeriodicTries, queued)); err == nil {
		t.Error("a stopped check succeeded")
	}
	if n := len(sink.all()) - before; n != 0 {
		t.Errorf("a stopped check reported %d events", n)
	}
}

// TestTheQueueReportsTheFirstGeoLite2Build: end to end, a started queue's
// leader runs the check on start, which on an empty meta.geoip downloads
// the first build, keeps it, and reports it once.
func TestTheQueueReportsTheFirstGeoLite2Build(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()
	built := time.Date(2026, 9, 25, 14, 43, 7, 0, time.UTC)
	srv := geoiptest.NewServer(t, geoiptest.Archive(t, geoiptest.Database(t, map[string]string{"81.2.69.0/24": "GB"})), built)
	sink := &recordingSink{}
	q, err := OpenQueue(ctx, QueueConfig{DatabaseURL: leaseURL(t), Logger: quietLogger(), Notify: sink, GeoIP: &geoip.Updater{
		AccountID: geoiptest.Account, LicenseKey: geoiptest.License,
		Store: s, Lookup: &geoip.Lookup{}, HTTP: srv.Client(),
		PermalinkURL: srv.PermalinkURL(), LegacyURL: srv.LegacyURL(),
	}})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { q.Stop(context.Background()) })
	for _, stmt := range []string{`DELETE FROM meta.geoip`, `DELETE FROM ` + QueueSchema + `.river_job`} {
		if _, err := s.pool.Exec(ctx, stmt); err != nil {
			t.Fatal(err)
		}
	}
	if got := q.Jobs(); len(got) != 1 || got[0] != notify.JobGeoIP {
		t.Errorf("Jobs = %v, want the GeoIP check", got)
	}
	if err := q.Start(ctx); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(30 * time.Second)
	for len(sink.of(notify.Downloaded)) == 0 {
		if time.Now().After(deadline) {
			t.Fatalf("the started queue never reported a download; heard %+v", sink.all())
		}
		time.Sleep(50 * time.Millisecond)
	}
	stopCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	q.Stop(stopCtx)

	downloads := sink.of(notify.Downloaded)
	if len(downloads) != 1 || !downloads[0].LiveSince.Equal(built) || !downloads[0].PrevAt.IsZero() {
		t.Errorf("downloads = %+v, want the first build once", downloads)
	}
	if stamp, err := s.GeoIPStamp(ctx); err != nil || stamp != built.Format(http.TimeFormat) {
		t.Errorf("kept %q, %v", stamp, err)
	}
	if (&Queue{}).Jobs() != nil || (*Queue)(nil).Jobs() != nil {
		t.Error("a queue without a GeoIP check names one")
	}
}

// stoppingGeoIP keeps a build and then stops the check, the way a deploy
// stopping the queue can land between the keep and the report.
type stoppingGeoIP struct {
	memGeoIP
	stop context.CancelFunc
}

func (s *stoppingGeoIP) KeepGeoIP(ctx context.Context, stamp string, mmdb []byte) error {
	err := s.memGeoIP.KeepGeoIP(ctx, stamp, mmdb)
	s.stop()
	return err
}

// TestABuildKeptAsTheStopCameIsStillReported: the build is kept, so the
// next check finds it and has nothing to say. Its one message is this
// check's, stop or no stop.
func TestABuildKeptAsTheStopCameIsStillReported(t *testing.T) {
	built := time.Date(2026, 9, 25, 14, 43, 7, 0, time.UTC)
	w, sink, _, _, _ := geoIPFixture(t, built)
	ctx, stop := context.WithCancel(context.Background())
	defer stop()
	w.u.Store = &stoppingGeoIP{stop: stop}
	if err := w.Work(ctx, geoIPJob(1, time.Now())); err != nil {
		t.Fatal(err)
	}
	if ctx.Err() == nil {
		t.Fatal("the check was not stopped")
	}
	if got := sink.of(notify.Downloaded); len(got) != 1 || !got[0].LiveSince.Equal(built) {
		t.Errorf("downloads = %+v, want the kept build reported once", sink.all())
	}
}
