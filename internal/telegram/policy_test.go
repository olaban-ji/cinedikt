package telegram

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"cinedikt/internal/notify"
)

// policy drives the state machine directly, with a clock that only
// moves when a test moves it.
type policy struct {
	t   *testing.T
	st  *state
	now time.Time
}

func newPolicy(t *testing.T) *policy {
	return &policy{t: t, st: newState(), now: testNow}
}

func (p *policy) after(d time.Duration) *policy {
	p.now = p.now.Add(d)
	return p
}

func (p *policy) note(e notify.Event) effects {
	e.At = p.now
	return p.st.apply(e, writer{loc: lagos, now: p.now})
}

// sounds is how many of the parts would make a phone ring.
func sounds(fx effects) int {
	n := 0
	for _, part := range fx.parts {
		if part.loud {
			n++
		}
	}
	return n
}

func failed(job string, cause notify.Cause) notify.Event {
	return notify.Event{Job: job, Kind: notify.Failed, Cause: cause, Detail: "boom"}
}

func TestAFailureStreakMakesOneSoundAndItsEndOneQuietMessage(t *testing.T) {
	p := newPolicy(t)
	busy := failed(notify.JobPosters, notify.DatabaseBusy)

	if fx := p.note(busy); len(fx.parts) != 0 {
		t.Fatalf("the first failure pushed %d parts; it belongs on the board only", len(fx.parts))
	}
	if fx := p.after(20 * time.Minute).note(busy); len(fx.parts) != 0 {
		t.Fatalf("a second failure after 20 min pushed; it has not lasted yet")
	}
	fx := p.after(30 * time.Minute).note(busy)
	if len(fx.parts) != 1 || sounds(fx) != 1 {
		t.Fatalf("at 3 failures over 50 min: %d parts, %d loud; want exactly one loud push", len(fx.parts), sounds(fx))
	}
	if !strings.Contains(stripTags(fx.parts[0].body), "Posters failing since 14:20") {
		t.Errorf("push = %q", fx.parts[0].body)
	}
	if fx := p.after(time.Hour).note(busy); len(fx.parts) != 0 {
		t.Fatalf("the streak was announced again")
	}
	fx = p.after(10 * time.Minute).note(notify.Event{Job: notify.JobPosters, Kind: notify.Checked})
	if len(fx.parts) != 1 || sounds(fx) != 0 {
		t.Fatalf("recovery: %d parts, %d loud; want one quiet push", len(fx.parts), sounds(fx))
	}
	if !strings.Contains(stripTags(fx.parts[0].body), "Posters working again") {
		t.Errorf("recovery = %q", fx.parts[0].body)
	}
	if len(p.st.Alerts) != 0 {
		t.Errorf("alerts left open: %v", p.st.Alerts)
	}
}

func TestTheImportAnnouncesOnItsSecondFailedHour(t *testing.T) {
	p := newPolicy(t)
	down := failed(notify.JobImport, notify.IMDbDown)
	if fx := p.note(down); len(fx.parts) != 0 {
		t.Fatal("the first failed hour pushed")
	}
	// The ticker is not exact; a few minutes short of an hour still
	// counts as the second hour.
	if fx := p.after(57 * time.Minute).note(down); sounds(fx) != 1 {
		t.Fatalf("the second failed hour made %d sounds, want 1", sounds(fx))
	}
}

func TestAnUnrecoveredStreakThatNeverLastsStaysSilent(t *testing.T) {
	p := newPolicy(t)
	p.note(failed(notify.JobColours, notify.AllFailed))
	fx := p.after(10 * time.Minute).note(notify.Event{Job: notify.JobColours, Kind: notify.Checked})
	if len(fx.parts) != 0 {
		t.Fatal("recovery from a failure nobody was told about pushed")
	}
}

func TestARefusedKeyIsSaidAtOnceAndOnceForBothJobs(t *testing.T) {
	p := newPolicy(t)
	key := notify.Event{Job: notify.JobTMDbPosters, Kind: notify.Failed, Cause: notify.KeyRejected, Provider: "TMDb", Detail: "tmdb: HTTP 401"}
	fx := p.note(key)
	if sounds(fx) != 1 || fx.parts[0].sev != sevRed || !strings.Contains(stripTags(fx.parts[0].body), "TMDb turned down our key") {
		t.Fatalf("parts = %+v, want one loud red push at the first failure", fx.parts)
	}
	key.Job = notify.JobTMDbIDs
	if fx := p.after(time.Minute).note(key); len(fx.parts) != 0 {
		t.Fatal("the second job on the same key pushed; it is one problem")
	}
	if fx := p.after(time.Hour).note(notify.Event{Job: notify.JobTMDbPosters, Kind: notify.Checked}); len(fx.parts) != 0 {
		t.Fatal("the key was said to work while another job still failed on it")
	}
	fx = p.after(time.Minute).note(notify.Event{Job: notify.JobTMDbIDs, Kind: notify.Checked})
	if len(fx.parts) != 1 || sounds(fx) != 0 || !strings.Contains(stripTags(fx.parts[0].body), "TMDb key works again") {
		t.Fatalf("parts = %+v, want one quiet key recovery once no job fails on it", fx.parts)
	}
}

// TestTheOpeningColoursNeverMakeASound: a placeholder tint on the first
// screen is worth a line on the board and a quiet message, never a
// sound, however long it lasts and whatever the error.
func TestTheOpeningColoursNeverMakeASound(t *testing.T) {
	p := newPolicy(t)
	hosts := notify.Event{Job: notify.JobColours, Kind: notify.Failed, Cause: notify.AllFailed, Provider: "poster hosts", Errors: 87}
	p.note(hosts)
	fx := p.after(50 * time.Minute).note(hosts)
	if len(fx.parts) != 1 || sounds(fx) != 0 {
		t.Fatalf("announced: %d parts, %d loud; want one quiet push", len(fx.parts), sounds(fx))
	}
	fx = p.after(24 * time.Hour).note(hosts)
	if len(fx.parts) != 1 || sounds(fx) != 0 {
		t.Fatalf("a day on: %d parts, %d loud; want one quiet reminder", len(fx.parts), sounds(fx))
	}
	q := newPolicy(t)
	odd := failed(notify.JobColours, notify.Unknown)
	q.note(odd)
	if fx := q.after(time.Minute).note(odd); len(fx.parts) != 1 || sounds(fx) != 0 || fx.parts[0].sev != sevRed {
		t.Fatalf("unknown error: %+v, want one quiet red push", fx.parts)
	}
}

// TestADayAndAHalfIsLoudOnlyWhileUpdatesFail: the stale alert is the
// import's reminder when it is failing, and needs nobody when IMDb is
// merely late.
func TestADayAndAHalfIsLoudOnlyWhileUpdatesFail(t *testing.T) {
	p := newPolicy(t)
	down := notify.Event{Job: notify.JobImport, Kind: notify.Failed, Cause: notify.IMDbDown, Status: 503, NextTry: p.now.Add(time.Hour)}
	p.note(down)
	p.after(time.Hour).note(down)
	fx := p.note(notify.Event{Job: notify.JobImport, Kind: notify.Stale, LiveSince: p.now.Add(-37 * time.Hour)})
	if len(fx.parts) != 1 || sounds(fx) != 1 {
		t.Fatalf("stale while failing: %d parts, %d loud; want one loud reminder", len(fx.parts), sounds(fx))
	}
}

func TestAnUnknownErrorIsSaidAtTheSecondFailure(t *testing.T) {
	p := newPolicy(t)
	odd := failed(notify.JobTMDbIDs, notify.Unknown)
	if fx := p.note(odd); len(fx.parts) != 0 {
		t.Fatal("one unknown error pushed")
	}
	fx := p.after(2 * time.Minute).note(odd)
	if sounds(fx) != 1 || fx.parts[0].sev != sevRed {
		t.Fatalf("parts = %+v, want one loud red push at the second failure", fx.parts)
	}
}

func TestADatabaseOutageIsOneAlertNotFive(t *testing.T) {
	p := newPolicy(t)
	fx := p.note(notify.Event{Job: notify.JobDatabase, Kind: notify.Failed, Cause: notify.DatabaseDown,
		Since: p.now.Add(-2 * time.Minute), Detail: "dial error"})
	if sounds(fx) != 1 || !strings.Contains(stripTags(fx.parts[0].body), "Can't reach the database") {
		t.Fatalf("parts = %+v, want the database alert", fx.parts)
	}
	down := failed(notify.JobPosters, notify.DatabaseDown)
	p.after(time.Minute).note(down)
	if fx := p.after(time.Hour).note(down); len(fx.parts) != 0 {
		t.Fatal("a job's database failure pushed while the database alert was open")
	}
	fx = p.after(time.Minute).note(notify.Event{Job: notify.JobSystem, Kind: notify.TookOver,
		Jobs: []string{notify.JobImport, notify.JobPosters, notify.JobColours}})
	if len(fx.parts) != 1 || sounds(fx) != 0 || !strings.Contains(stripTags(fx.parts[0].body), "Database is back") {
		t.Fatalf("parts = %+v, want one quiet database recovery", fx.parts)
	}
}

// TestADatabaseOutageThatLastsADayIsSaidAgain: the lease repeats its
// report while the database is away, and that repeat is what lets the
// reminder come a day on. The repeats themselves say nothing new.
func TestADatabaseOutageThatLastsADayIsSaidAgain(t *testing.T) {
	p := newPolicy(t)
	down := notify.Event{Job: notify.JobDatabase, Kind: notify.Failed, Cause: notify.DatabaseDown,
		Since: p.now.Add(-2 * time.Minute), Detail: "dial error"}
	p.note(down)
	for i := 0; i < 47; i++ {
		fx := p.after(30 * time.Minute).note(down)
		if len(fx.parts) != 0 {
			t.Fatalf("repeat %d, %v into the outage, said %q", i+1, p.now.Sub(down.Since), fx.parts[0].body)
		}
		if !fx.force {
			t.Fatal("a repeat did not rewrite the board, so its stamp stops moving")
		}
	}
	fx := p.after(30 * time.Minute).note(down)
	if len(fx.parts) != 1 || sounds(fx) != 1 || !strings.Contains(stripTags(fx.parts[0].body), "Still can't reach the database") {
		t.Fatalf("a day in: %+v, want the one loud reminder", fx.parts)
	}
}

func TestStaleIsSaidOncePerCatalogAtEachLevel(t *testing.T) {
	p := newPolicy(t)
	built := p.now.Add(-37 * time.Hour)
	stale := notify.Event{Job: notify.JobImport, Kind: notify.Stale, LiveSince: built}

	// IMDb being late is nobody's to fix, so a day and a half is said
	// without a sound; three days needs a person, and has one.
	fx := p.note(stale)
	if len(fx.parts) != 1 || sounds(fx) != 0 || fx.parts[0].sev != sevWarn {
		t.Fatalf("at 37 h: %+v, want one quiet amber push", fx.parts)
	}
	if fx := p.after(time.Hour).note(stale); len(fx.parts) != 0 {
		t.Fatal("the same catalog was called old twice at the same level")
	}
	fx = p.after(35 * time.Hour).note(stale)
	if sounds(fx) != 1 || fx.parts[0].sev != sevRed {
		t.Fatalf("at 73 h: %+v, want one loud red push", fx.parts)
	}

	// A deploy: the state goes through Postgres and comes back.
	raw, err := json.Marshal(p.st)
	if err != nil {
		t.Fatal(err)
	}
	p.st = loadState(raw)
	if fx := p.after(time.Hour).note(stale); len(fx.parts) != 0 {
		t.Fatal("a restart said the catalog was old again")
	}

	// A new catalog starts from nothing.
	p.note(notify.Event{Job: notify.JobImport, Kind: notify.Published, Films: 10, People: 20, LiveSince: p.now})
	fresh := notify.Event{Job: notify.JobImport, Kind: notify.Stale, LiveSince: p.now}
	if fx := p.after(37 * time.Hour).note(fresh); len(fx.parts) != 1 {
		t.Fatal("a new catalog going stale was not said")
	}

	// And nothing ever published is no stale alert at all.
	q := newPolicy(t)
	if fx := q.note(notify.Event{Job: notify.JobImport, Kind: notify.Stale}); len(fx.parts) != 0 {
		t.Fatal("a catalog that was never built was called old")
	}
}

// TestStaleIsSaidOnceWhenEachHourNamesTheCatalogAfresh is the hourly
// event as a running process builds it: a time from the database, which
// carries no monotonic clock reading, and a LiveSince worked out from
// this moment's clock, which does, and a different one every hour.
// time.Equal compares monotonic readings when both have one, so a state
// that kept them would take every hour for a new catalog and say it
// again.
func TestStaleIsSaidOnceWhenEachHourNamesTheCatalogAfresh(t *testing.T) {
	p := newPolicy(t)
	built := p.now.Add(-36 * time.Hour).Round(0)
	said := 0
	for i := 0; i < 3; i++ {
		now := time.Now()
		live := now.Add(-now.Sub(built))
		said += len(p.after(time.Hour).note(notify.Event{Job: notify.JobImport, Kind: notify.Stale, LiveSince: live}).parts)
	}
	if said != 1 {
		t.Fatalf("three hours of the same stale catalog were said %d times, want once", said)
	}
	if lvl := p.st.Stale.Level; lvl != 1 {
		t.Fatalf("stale level = %d, want 1", lvl)
	}
}

// TestALookupOutageIsNotClearedByPassesThatAskedNobody is the OMDb
// backfill in steady state: every lookup fails, the failed titles wait a
// day, and the passes in between find nothing to do. Those passes must
// not end the streak, or an outage never lasts long enough to be said
// and the board goes back to up to date twenty minutes later.
func TestALookupOutageIsNotClearedByPassesThatAskedNobody(t *testing.T) {
	p := newPolicy(t)
	down := notify.Event{Job: notify.JobPosters, Kind: notify.Failed, Cause: notify.AllFailed, Provider: "OMDb",
		Errors: 1893, NextTry: p.now.Add(24 * time.Hour), Detail: "catalog: all 1893 lookups to OMDb failed"}
	p.note(down)
	for i := 0; i < 3; i++ {
		p.after(20 * time.Minute).note(notify.Event{Job: notify.JobPosters, Kind: notify.Checked})
	}
	j := p.st.Jobs[notify.JobPosters]
	if j.State != stFailing || j.Fails != 1 {
		t.Fatalf("after passes with nothing to retry: state %s, %d fails; want still failing, 1 fail", j.State, j.Fails)
	}
	if _, text := p.st.line(notify.JobPosters, writer{loc: lagos, now: p.now}); !strings.Contains(text, "next try by tomorrow") {
		t.Errorf("board line = %q, want the real next try", text)
	}
	p.now = p.now.Add(24*time.Hour - time.Hour)
	down.NextTry = p.now.Add(24 * time.Hour)
	if fx := p.note(down); sounds(fx) != 1 {
		t.Fatalf("the retry a day later failed too: %d sounds, want the one announcement", sounds(fx))
	}
	// A pass once the retry is due, with nothing left to ask, is a pass
	// that would have asked: that one does clear it.
	p.now = down.NextTry
	fx := p.note(notify.Event{Job: notify.JobPosters, Kind: notify.Checked})
	if p.st.Jobs[notify.JobPosters].State != stIdle || len(fx.parts) != 1 {
		t.Fatalf("a check after the retry was due: state %s, %d parts; want idle and one recovery",
			p.st.Jobs[notify.JobPosters].State, len(fx.parts))
	}
}

func TestAnOpenAlertIsRemindedDailyExceptTheImports(t *testing.T) {
	p := newPolicy(t)
	busy := failed(notify.JobPosters, notify.DatabaseBusy)
	imp := failed(notify.JobImport, notify.IMDbDown)
	p.note(busy)
	p.note(imp)
	p.after(time.Hour)
	p.note(imp)
	p.note(busy)
	if len(p.st.Alerts) != 2 {
		t.Fatalf("alerts = %v, want the import's and the posters'", p.st.Alerts)
	}
	fx := p.after(23 * time.Hour).note(imp)
	if len(fx.parts) != 0 {
		t.Fatal("a reminder came before a day had passed")
	}
	fx = p.after(time.Hour).note(imp)
	if len(fx.parts) != 1 || sounds(fx) != 1 || !strings.Contains(stripTags(fx.parts[0].body), "Posters still failing") {
		t.Fatalf("parts = %+v, want one loud reminder, for the posters only", fx.parts)
	}
	if fx := p.after(time.Hour).note(imp); len(fx.parts) != 0 {
		t.Fatal("the reminder was repeated within the day")
	}
}

func TestAPublishFoldsInTheRecovery(t *testing.T) {
	p := newPolicy(t)
	mismatch := notify.Event{Job: notify.JobImport, Kind: notify.Failed, Cause: notify.FilesMismatch, Integrity: 0.9731}
	p.note(mismatch)
	p.after(time.Hour).note(mismatch)
	fx := p.after(time.Hour).note(notify.Event{Job: notify.JobImport, Kind: notify.Published,
		Films: 757802, People: 3120442, PrevFilms: 756598, PrevAt: p.now.Add(-26 * time.Hour), Took: 107 * time.Minute, LiveSince: p.now})
	if len(fx.parts) != 1 || sounds(fx) != 0 {
		t.Fatalf("parts = %+v, want one quiet message", fx.parts)
	}
	if !strings.Contains(stripTags(fx.parts[0].body), "New catalog is live") || !strings.Contains(stripTags(fx.parts[0].body), "that's fixed") {
		t.Errorf("body = %q, want the publish with the recovery folded in", fx.parts[0].body)
	}
	if !fx.force {
		t.Error("a publish did not force the board")
	}
}

func TestACheckClearsAPauseOnlyOnceItIsOver(t *testing.T) {
	p := newPolicy(t)
	p.note(notify.Event{Job: notify.JobPosters, Kind: notify.Paused, Cause: notify.DailyLimit, NextTry: p.now.Add(time.Hour)})
	p.after(20 * time.Minute).note(notify.Event{Job: notify.JobPosters, Kind: notify.Checked})
	if got := p.st.Jobs[notify.JobPosters].State; got != stPaused {
		t.Fatalf("state = %s before the pause ended, want paused", got)
	}
	p.after(time.Hour).note(notify.Event{Job: notify.JobPosters, Kind: notify.Checked})
	if got := p.st.Jobs[notify.JobPosters].State; got != stIdle {
		t.Fatalf("state = %s after the pause ended, want idle", got)
	}
}

func TestTakingOverInterruptsWhatWasRunningAndTurnsOffTheRest(t *testing.T) {
	p := newPolicy(t)
	p.note(notify.Event{Job: notify.JobImport, Kind: notify.Started, Step: 1, Steps: 4})
	p.note(notify.Event{Job: notify.JobPosters, Kind: notify.Started, Total: 100})
	p.note(notify.Event{Job: notify.JobSystem, Kind: notify.Stopped})
	if got := p.st.Jobs[notify.JobImport].State; got != stInterrupted {
		t.Fatalf("import = %s after a stop, want interrupted", got)
	}
	p.note(notify.Event{Job: notify.JobTMDbIDs, Kind: notify.Started, Total: 5})
	p.note(notify.Event{Job: notify.JobSystem, Kind: notify.TookOver,
		Jobs: []string{notify.JobImport, notify.JobColours, notify.JobPosters}})
	for id, want := range map[string]string{
		notify.JobImport:      stInterrupted,
		notify.JobPosters:     stInterrupted,
		notify.JobTMDbIDs:     stOff,
		notify.JobTMDbPosters: stOff,
		notify.JobColours:     stStarting,
	} {
		if got := p.st.Jobs[id].State; got != want {
			t.Errorf("%s = %s, want %s", id, got, want)
		}
	}
}

func TestOnlyALongPassIsNews(t *testing.T) {
	p := newPolicy(t)
	p.note(notify.Event{Job: notify.JobPosters, Kind: notify.Started, Total: 100})
	if fx := p.note(notify.Event{Job: notify.JobPosters, Kind: notify.Finished, Done: 100, Took: 29 * time.Minute}); len(fx.parts) != 0 {
		t.Fatal("a 29 minute pass pushed")
	}
	p.note(notify.Event{Job: notify.JobPosters, Kind: notify.Started, Total: 100})
	fx := p.note(notify.Event{Job: notify.JobPosters, Kind: notify.Finished, Done: 100, Took: 31 * time.Minute})
	if len(fx.parts) != 1 || sounds(fx) != 0 {
		t.Fatalf("a 31 minute pass: %+v, want one quiet milestone", fx.parts)
	}
}
