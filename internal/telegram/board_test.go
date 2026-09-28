package telegram

import (
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"cinedikt/internal/notify"
)

var testPlace = place{env: "dev", commit: "d017008c9a4e"}

var allJobs = []string{notify.JobImport, notify.JobColours, notify.JobPosters, notify.JobTMDbPosters, notify.JobTMDbIDs,
	notify.JobSynopses, notify.JobTrailers, notify.JobPeople}

// quietDay is a process that took over this morning, published last
// night's catalog, and has every job up to date.
func quietDay(t *testing.T) *policy {
	p := newPolicy(t)
	p.now = time.Date(2026, 9, 27, 6, 2, 0, 0, lagos)
	p.note(notify.Event{Job: notify.JobSystem, Kind: notify.TookOver, Jobs: allJobs})
	p.note(notify.Event{Job: notify.JobImport, Kind: notify.Published, Films: 757802, People: 3120442,
		PrevFilms: 756598, PrevAt: p.now.Add(-24 * time.Hour), Took: 107 * time.Minute,
		LiveSince: time.Date(2026, 9, 27, 4, 58, 0, 0, lagos), NextTry: p.now.Add(time.Hour)})
	p.note(notify.Event{Job: notify.JobPosters, Kind: notify.Started, Total: 1204})
	p.after(4 * time.Minute).note(notify.Event{Job: notify.JobPosters, Kind: notify.Finished, Done: 1180, Errors: 24, Took: 4 * time.Minute})
	p.note(notify.Event{Job: notify.JobTMDbPosters, Kind: notify.Started, Total: 41})
	p.note(notify.Event{Job: notify.JobTMDbPosters, Kind: notify.Finished, Done: 37, None: 4, Took: time.Minute})
	p.note(notify.Event{Job: notify.JobTMDbIDs, Kind: notify.Started, Total: 1300})
	p.note(notify.Event{Job: notify.JobTMDbIDs, Kind: notify.Finished, Done: 1288, None: 12, Took: 2 * time.Minute})
	p.note(notify.Event{Job: notify.JobSynopses, Kind: notify.Started, Total: 1204})
	p.note(notify.Event{Job: notify.JobSynopses, Kind: notify.Finished, Done: 1150, None: 54, Took: 3 * time.Minute})
	p.note(notify.Event{Job: notify.JobTrailers, Kind: notify.Started, Total: 318})
	p.note(notify.Event{Job: notify.JobTrailers, Kind: notify.Finished, Done: 301, None: 17, Took: time.Minute})
	p.note(notify.Event{Job: notify.JobPeople, Kind: notify.Started, Total: 2140})
	p.note(notify.Event{Job: notify.JobPeople, Kind: notify.Finished, Done: 1893, None: 247, Took: 7 * time.Minute})
	p.note(notify.Event{Job: notify.JobColours, Kind: notify.Checked})
	p.now = testNow
	p.note(notify.Event{Job: notify.JobImport, Kind: notify.Checked, NextTry: p.now.Add(40 * time.Minute),
		LiveSince: time.Date(2026, 9, 27, 4, 58, 0, 0, lagos), Films: 757802})
	return p
}

func boards(t *testing.T) map[string]*policy {
	out := map[string]*policy{}

	out["quiet-day"] = quietDay(t)

	running := quietDay(t)
	running.note(notify.Event{Job: notify.JobImport, Kind: notify.Started, Step: 1, Steps: 4, Phase: notify.PhaseDownload})
	running.after(3 * time.Minute).note(notify.Event{Job: notify.JobImport, Kind: notify.Progress, Step: 1, Steps: 4,
		Phase: notify.PhaseDownload, File: 3, Files: 5, Noun: "credits", Share: 0.41, Total: 700 << 20, Done: 287 << 20})
	out["import-downloading"] = running

	loading := quietDay(t)
	loading.note(notify.Event{Job: notify.JobImport, Kind: notify.Started, Step: 1, Steps: 4})
	loading.after(9 * time.Minute).note(notify.Event{Job: notify.JobImport, Kind: notify.Progress, Step: 2, Steps: 4, Phase: notify.PhaseLoad, Noun: "credits"})
	out["import-loading"] = loading

	failing := quietDay(t)
	down := notify.Event{Job: notify.JobImport, Kind: notify.Failed, Cause: notify.IMDbDown, Status: 503,
		Detail: "catalog: HEAD title.basics: HTTP 503", LiveSince: time.Date(2026, 9, 27, 4, 58, 0, 0, lagos)}
	down.NextTry = failing.now.Add(time.Hour)
	failing.note(down)
	failing.after(time.Hour)
	down.NextTry = failing.now.Add(time.Hour)
	failing.note(down)
	out["import-failing"] = failing

	stale := newPolicy(t)
	stale.note(notify.Event{Job: notify.JobSystem, Kind: notify.TookOver, Jobs: allJobs})
	for _, id := range allJobs[1:] {
		stale.note(notify.Event{Job: id, Kind: notify.Checked})
	}
	stale.note(notify.Event{Job: notify.JobImport, Kind: notify.Checked, NextTry: stale.now.Add(40 * time.Minute),
		LiveSince: stale.now.Add(-(49*time.Hour + 20*time.Minute)), Films: 756598})
	out["stale"] = stale

	backfill := quietDay(t)
	backfill.now = time.Date(2026, 9, 27, 9, 0, 0, 0, lagos)
	backfill.note(notify.Event{Job: notify.JobPosters, Kind: notify.Started, Total: 757802})
	backfill.after(40 * time.Minute).note(notify.Event{Job: notify.JobPosters, Kind: notify.Paused, Cause: notify.DailyLimit,
		Done: 402113, Errors: 311, NextTry: testNow.Add(50 * time.Minute)})
	// The synopsis job shares the poster pass's OMDb client, so the one
	// limit pauses both.
	backfill.note(notify.Event{Job: notify.JobSynopses, Kind: notify.Started, Total: 38000})
	backfill.note(notify.Event{Job: notify.JobSynopses, Kind: notify.Paused, Cause: notify.DailyLimit,
		Done: 9120, NextTry: testNow.Add(50 * time.Minute)})
	backfill.note(notify.Event{Job: notify.JobTMDbIDs, Kind: notify.Started, Total: 180422})
	backfill.now = testNow
	backfill.note(notify.Event{Job: notify.JobTMDbIDs, Kind: notify.Progress, Total: 180422, Done: 75777, Share: 0.42,
		ETA: testNow.Add(2*time.Hour + 13*time.Minute)})
	out["big-backfill-omdb-limit"] = backfill

	dbDown := quietDay(t)
	dbDown.note(notify.Event{Job: notify.JobSystem, Kind: notify.Stopped})
	dbDown.after(2 * time.Minute).note(notify.Event{Job: notify.JobDatabase, Kind: notify.Failed, Cause: notify.DatabaseDown,
		Since: dbDown.now.Add(-2 * time.Minute), Detail: "dial error"})
	out["database-down"] = dbDown

	restarting := quietDay(t)
	restarting.note(notify.Event{Job: notify.JobPosters, Kind: notify.Started, Total: 5000})
	restarting.st.shutting = true
	for _, j := range restarting.st.Jobs {
		if j.State == stRunning {
			j.State = stInterrupted
		}
	}
	out["restarting"] = restarting

	first := newPolicy(t)
	first.note(notify.Event{Job: notify.JobSystem, Kind: notify.TookOver,
		Jobs: []string{notify.JobImport, notify.JobColours, notify.JobPosters, notify.JobSynopses}})
	first.note(notify.Event{Job: notify.JobImport, Kind: notify.Started, Step: 1, Steps: 4, Phase: notify.PhaseDownload})
	out["first-start-no-catalog"] = first

	// A first deploy of the board, or a state that could not be read,
	// with the startup import already running: the site has a catalog,
	// and the import's first event says so.
	fresh := newPolicy(t)
	fresh.note(notify.Event{Job: notify.JobSystem, Kind: notify.TookOver, Jobs: allJobs})
	fresh.note(notify.Event{Job: notify.JobImport, Kind: notify.Started, Step: 1, Steps: 4, Phase: notify.PhaseDownload,
		LiveSince: time.Date(2026, 9, 26, 4, 58, 0, 0, lagos), Films: 756598})
	out["fresh-state-import-running"] = fresh

	// No catalog, and the hour's check found nothing to import (another
	// import held the lock): nothing is wrong, but it is not all good.
	none := newPolicy(t)
	none.note(notify.Event{Job: notify.JobSystem, Kind: notify.TookOver, Jobs: allJobs})
	none.note(notify.Event{Job: notify.JobImport, Kind: notify.Checked, Cause: notify.Locked, NextTry: none.now.Add(time.Hour)})
	out["no-catalog-yet"] = none

	// Three days of IMDb not answering: the header and the line agree.
	old := newPolicy(t)
	old.note(notify.Event{Job: notify.JobSystem, Kind: notify.TookOver, Jobs: allJobs})
	for _, id := range allJobs[1:] {
		old.note(notify.Event{Job: id, Kind: notify.Checked})
	}
	built := old.now.Add(-(73*time.Hour + 5*time.Minute))
	gone := notify.Event{Job: notify.JobImport, Kind: notify.Failed, Cause: notify.IMDbDown, Status: 503,
		Detail: "catalog: HEAD title.basics: HTTP 503", LiveSince: built, NextTry: old.now.Add(time.Hour)}
	for i := 0; i < 39; i++ {
		old.note(gone)
	}
	out["import-failing-3-days"] = old

	// OMDb failing every lookup: the failed titles wait a day, so that
	// is the next try, and passes in between do not clear it.
	lookups := quietDay(t)
	lookups.note(notify.Event{Job: notify.JobPosters, Kind: notify.Started, Total: 1893})
	lookups.note(notify.Event{Job: notify.JobPosters, Kind: notify.Failed, Cause: notify.AllFailed, Provider: "OMDb",
		Errors: 1893, NextTry: lookups.now.Add(24 * time.Hour), Detail: "catalog: all 1893 lookups to OMDb failed"})
	lookups.after(20 * time.Minute).note(notify.Event{Job: notify.JobPosters, Kind: notify.Checked})
	out["posters-lookups-failing"] = lookups

	keyAndBusy := quietDay(t)
	keyAndBusy.note(notify.Event{Job: notify.JobTMDbPosters, Kind: notify.Failed, Cause: notify.KeyRejected, Provider: "TMDb",
		NextTry: keyAndBusy.now.Add(10 * time.Minute)})
	keyAndBusy.note(notify.Event{Job: notify.JobTMDbIDs, Kind: notify.Failed, Cause: notify.KeyRejected, Provider: "TMDb",
		NextTry: keyAndBusy.now.Add(10 * time.Minute)})
	keyAndBusy.note(notify.Event{Job: notify.JobTrailers, Kind: notify.Failed, Cause: notify.KeyRejected, Provider: "TMDb",
		NextTry: keyAndBusy.now.Add(30 * time.Minute)})
	keyAndBusy.note(notify.Event{Job: notify.JobPeople, Kind: notify.Failed, Cause: notify.KeyRejected, Provider: "TMDb",
		NextTry: keyAndBusy.now.Add(30 * time.Minute)})
	out["tmdb-key-refused"] = keyAndBusy

	// The people sweep's first pass: over a million people at a pace of
	// its own, counted in people rather than films.
	faces := quietDay(t)
	faces.now = testNow.Add(-3 * time.Hour)
	faces.note(notify.Event{Job: notify.JobPeople, Kind: notify.Started, Total: 1332981})
	faces.now = testNow
	faces.note(notify.Event{Job: notify.JobPeople, Kind: notify.Progress, Total: 1332981,
		Done: 54012, Share: 0.041, ETA: testNow.Add(71 * time.Hour)})
	out["people-sweep-running"] = faces

	return out
}

func TestEveryBoardReadsAsWritten(t *testing.T) {
	for name, p := range boards(t) {
		full, body := p.st.render(writer{loc: lagos, now: p.now}, testPlace)
		golden(t, "board", name, full)
		if strings.Contains(body, "tg-time") {
			t.Errorf("%s: the body compared for edits still has the stamp", name)
		}
		for i, line := range strings.Split(stripTags(full), "\n") {
			if n := utf8.RuneCountInString(line); n > lineMax && i > 0 {
				t.Errorf("%s: line %q is %d runes, over %d", name, line, n, lineMax)
			}
		}
		for _, in := range jobList {
			if !strings.Contains(full, "<b>"+in.label+"</b> · ") {
				t.Errorf("%s: no line for %s", name, in.label)
			}
		}
	}
}

func TestABoardWithoutAZoneSaysItsTimesAreUTC(t *testing.T) {
	p := quietDay(t)
	full, _ := p.st.render(writer{loc: time.UTC, now: p.now}, place{utc: true})
	if !strings.Contains(full, "· local ·") || !strings.HasSuffix(full, " · times UTC</i>") {
		t.Errorf("footer = %q", full[strings.LastIndex(full, "\n"):])
	}
}

func TestAPauseDoesNotChangeTheHeader(t *testing.T) {
	p := boards(t)["big-backfill-omdb-limit"]
	p.note(notify.Event{Job: notify.JobTMDbIDs, Kind: notify.Finished, Done: 180000})
	_, head := p.st.header(writer{loc: lagos, now: p.now})
	if head != "All good" {
		t.Errorf("header = %q with only a paused job, want All good", head)
	}
}
