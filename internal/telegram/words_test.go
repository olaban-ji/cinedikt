package telegram

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"cinedikt/internal/notify"
)

// golden compares got with testdata/<dir>/<name>.golden. Run with
// UPDATE_GOLDEN=1 to write the references, then read them: they are
// exactly what the chat receives, and reading them is the review.
func golden(t *testing.T, dir, name, got string) {
	t.Helper()
	path := filepath.Join("testdata", dir, name+".golden")
	if os.Getenv("UPDATE_GOLDEN") != "" {
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(got), 0o644); err != nil {
			t.Fatal(err)
		}
		return
	}
	want, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("no golden for %s (%v); run with UPDATE_GOLDEN=1 and read what it wrote", name, err)
	}
	if got != string(want) {
		t.Errorf("%s changed.\n--- got ---\n%s\n--- want ---\n%s", name, got, want)
	}
}

var (
	firstLine = regexp.MustCompile(`^(✅|⏳|⏸️|⚠️|🔴) <b>([^<]+)</b>$`)
	allowed   = regexp.MustCompile(`</?(b|i|blockquote( expandable)?|tg-time( [^>]*)?)>`)
)

// shapeOf checks the rules every push keeps: a mark and a bold headline
// of at most 45 characters, the only tags Telegram is promised, no
// exclamation marks, no "I".
func shapeOf(t *testing.T, name, text string) {
	t.Helper()
	for _, block := range strings.Split(text, "\n\n") {
		lines := strings.Split(block, "\n")
		m := firstLine.FindStringSubmatch(lines[0])
		if m == nil {
			t.Errorf("%s: first line %q is not a mark and a bold headline", name, lines[0])
			continue
		}
		if n := utf8.RuneCountInString(stripTags(m[2])); n > 45 {
			t.Errorf("%s: headline %q is %d characters, over 45", name, stripTags(m[2]), n)
		}
	}
	if left := allowed.ReplaceAllString(text, ""); strings.ContainsAny(tagRE.FindString(left), "<>") {
		t.Errorf("%s: a tag outside b, i, blockquote and tg-time: %q", name, tagRE.FindString(left))
	}
	visibleText := stripTags(regexp.MustCompile(`(?s)<blockquote expandable>.*?</blockquote>`).ReplaceAllString(text, ""))
	if strings.Contains(visibleText, "!") {
		t.Errorf("%s: an exclamation mark in %q", name, visibleText)
	}
	if regexp.MustCompile(`\bI\b`).MatchString(visibleText) {
		t.Errorf("%s: the chat says \"I\" in %q", name, visibleText)
	}
}

// failingJob is a job in the middle of a streak.
func failingJob(cause notify.Cause, fails int, since time.Duration) *job {
	return &job{
		State:     stFailing,
		Cause:     cause,
		Fails:     fails,
		FailSince: testNow.Add(-since),
		NextTry:   testNow.Add(40 * time.Minute),
		Detail:    "catalog: GET title.principals: HTTP 503",
	}
}

// messages is every push the chat can receive, by name, as sent.
func messages() map[string][]part {
	w := writer{loc: lagos, now: testNow, token: "123:secret"}
	yesterday := time.Date(2026, 9, 26, 3, 12, 0, 0, lagos)
	friday := time.Date(2026, 9, 25, 3, 12, 0, 0, lagos)
	published := notify.Event{Job: notify.JobImport, Kind: notify.Published,
		Films: 757802, People: 3120442, PrevFilms: 756598, PrevAt: yesterday, Took: 107 * time.Minute}

	imdb := failingJob(notify.IMDbDown, 2, 65*time.Minute)
	imdb.Status = 503
	mismatch := failingJob(notify.FilesMismatch, 2, 61*time.Minute)
	mismatch.Integrity = 0.9731
	mismatch.Detail = "catalog: only 0.9731 of credits name a stored title, want 0.99 (the files are probably from different generations)"
	odd := failingJob(notify.Unknown, 2, 61*time.Minute)
	odd.Detail = "catalog: title.basics has no Last-Modified"
	busy := failingJob(notify.DatabaseBusy, 3, 55*time.Minute)
	busy.Detail = "catalog: find titles wanting a poster: ERROR: canceling statement due to statement timeout (SQLSTATE 57014)"
	colours := failingJob(notify.AllFailed, 2, 70*time.Minute)
	colours.Provider, colours.Lookups = "poster hosts", 87
	colours.Detail = `catalog: all 87 lookups to poster hosts failed; the last: Get "https://m.media-amazon.com/images/M/x.jpg": dial tcp 10.1.2.3:443: i/o timeout`
	idsOdd := failingJob(notify.Unknown, 2, 12*time.Minute)
	idsOdd.Detail = "tmdb: HTTP 400: Invalid id"
	longFail := failingJob(notify.IMDbDown, 40, 40*time.Hour)
	longFail.Status = 503
	oddLong := failingJob(notify.Unknown, 20, 20*time.Hour)
	oddLong.Detail = "catalog: title.basics has no Last-Modified"
	incomplete := failingJob(notify.FilesMismatch, 12, 12*time.Hour)
	incomplete.Detail = "catalog: no principals loaded"
	omdbDown := failingJob(notify.AllFailed, 2, 24*time.Hour)
	omdbDown.Provider, omdbDown.Lookups = "OMDb", 1893
	omdbDown.NextTry = testNow.Add(23 * time.Hour)
	omdbDown.Detail = `catalog: all 1893 lookups to OMDb failed; the last: Get "https://www.omdbapi.com/?apikey=abc&i=tt1": context deadline exceeded`

	return map[string][]part{
		"p1-new-catalog":           {w.published(published, time.Time{})},
		"p1-new-catalog-first":     {w.published(notify.Event{Films: 757802, People: 3120442, Took: 2 * time.Hour}, time.Time{})},
		"p1-new-catalog-fewer":     {w.published(notify.Event{Films: 757000, People: 3120442, PrevFilms: 757802, PrevAt: friday, Took: 98 * time.Minute}, time.Time{})},
		"p1-new-catalog-fixed":     {w.published(published, testNow.Add(-3*time.Hour))},
		"p2-catalog-failing-imdb":  {w.importFailing(imdb, yesterday)},
		"p2-catalog-failing-files": {w.importFailing(mismatch, yesterday)},
		"p2-catalog-failing-empty": {w.importFailing(imdb, time.Time{})},
		"p3-catalog-unexpected":    {w.importFailing(odd, yesterday)},
		"p4-database-down":         {w.databaseDown(testNow.Add(-2*time.Minute), "failed to connect to `user=postgres database=railway`: 10.1.2.3:5432 (postgres.railway.internal): dial error: dial tcp 10.1.2.3:5432: connect: connection refused")},
		"p4r-database-back":        {w.databaseBack(testNow.Add(-17*time.Minute), true)},
		"p5-stale-36h":             {w.stale(1, testNow.Add(-37*time.Hour), nil)},
		"p5-stale-36h-failing":     {w.stale(1, testNow.Add(-37*time.Hour), incomplete)},
		"p5-stale-72h-failing":     {w.stale(2, testNow.Add(-73*time.Hour), longFail)},
		"p5-stale-36h-unexpected":  {w.stale(1, testNow.Add(-37*time.Hour), oddLong)},
		"p6-posters-failing":       {w.jobFailing(notify.JobPosters, busy)},
		"p6-colours-failing":       {w.jobFailing(notify.JobColours, colours)},
		"p6-posters-omdb-down":     {w.jobFailing(notify.JobPosters, omdbDown)},
		"p6-search-unexpected":     {w.jobFailing(notify.JobTMDbIDs, idsOdd)},
		"p7-tmdb-key":              {w.keyRejected("TMDb", "tmdb: HTTP 401: Invalid API key: You must be granted a valid key.")},
		"p7-omdb-key":              {w.keyRejected("OMDb", "catalog: OMDb refused the key: omdb: invalid API key")},
		"p8-posters-working-again": {w.workingAgain(notify.JobPosters, testNow.Add(-(2*time.Hour + 10*time.Minute)), yesterday, false)},
		"p8-catalog-working-again": {w.workingAgain(notify.JobImport, testNow.Add(-(3*time.Hour + 5*time.Minute)), yesterday, true)},
		"p8-tmdb-key-back":         {w.keyBack("TMDb", testNow.Add(-26*time.Hour))},
		"p9-posters-reminder":      {w.reminder(&alert{Key: "job:posters", Since: busy.FailSince.Add(-24 * time.Hour), Severity: sevWarn}, notify.JobPosters, busy)},
		"p9-tmdb-key-reminder":     {w.reminder(&alert{Key: "key:tmdb", Since: testNow.Add(-25 * time.Hour), Severity: sevRed, Provider: "TMDb"}, notify.JobTMDbPosters, &job{Detail: "tmdb: HTTP 401"})},
		"p9-database-reminder":     {w.reminder(&alert{Key: alertDatabase, Since: testNow.Add(-25 * time.Hour), Severity: sevRed, Detail: "dial error"}, "", nil)},
		"p10-posters":              {w.longPass(notify.JobPosters, notify.Event{Done: 312410, None: 217964, Errors: 1893, Took: 41 * time.Minute}, "")},
		"p10-posters-fixed":        {w.longPass(notify.JobPosters, notify.Event{Done: 312410, Took: 41 * time.Minute}, w.fixedSentence(testNow.Add(-5*time.Hour)))},
		"p10-search-matching":      {w.longPass(notify.JobTMDbIDs, notify.Event{Done: 52114, None: 3120, Took: 34 * time.Minute}, "")},
		"p10-backup-posters":       {w.longPass(notify.JobTMDbPosters, notify.Event{Done: 1204, None: 96, Took: 2*time.Hour + 5*time.Minute}, "")},
		"p11-manual-published":     {mustManual(w, notify.Event{Job: notify.JobImport, Kind: notify.Published, Films: 757802, People: 3120442, PrevFilms: 756598, Took: 97 * time.Minute})},
		"p11-manual-nothing-new":   {mustManual(w, notify.Event{Job: notify.JobImport, Kind: notify.Checked, LiveSince: yesterday})},
		"p11-manual-skipped":       {mustManual(w, notify.Event{Job: notify.JobImport, Kind: notify.Skipped, Cause: notify.FileMoved})},
		"p11-manual-locked":        {mustManual(w, notify.Event{Job: notify.JobImport, Kind: notify.Checked, Cause: notify.Locked})},
		"p11-manual-failed":        {mustManual(w, notify.Event{Job: notify.JobImport, Kind: notify.Failed, Cause: notify.IMDbDown, Status: 503, Detail: "catalog: HEAD title.basics: HTTP 503"})},
		"p11-manual-posters":       {mustManual(w, notify.Event{Job: notify.JobPosters, Kind: notify.Finished, Done: 1830, Errors: 12}, notify.Event{Job: notify.JobTMDbPosters, Kind: notify.Finished, Done: 214})},
		"p11-manual-posters-limit": {mustManual(w, notify.Event{Job: notify.JobPosters, Kind: notify.Paused, Cause: notify.DailyLimit, Done: 950})},
		// Three jobs with news in the same two seconds: a refused key, a
		// failure that has lasted, and a long pass that ended.
		"coalesced-key-failing-and-done": {w.longPass(notify.JobPosters, notify.Event{Done: 1830, None: 1204, Took: 34 * time.Minute}, ""),
			w.jobFailing(notify.JobColours, colours), w.keyRejected("TMDb", "tmdb: HTTP 401: Invalid API key")},
		// The import's second failed hour lands as the catalog turns a
		// day and a half old: the stale alert says the cause, once.
		"coalesced-import-failing-and-stale": {w.importFailing(mismatch, yesterday), w.stale(1, testNow.Add(-37*time.Hour), mismatch)},
	}
}

func mustManual(w writer, events ...notify.Event) part {
	p, ok := w.manual(events)
	if !ok {
		panic("no manual summary")
	}
	return p
}

// TestEveryPushReadsAsWritten renders every message the chat can get,
// exactly as it is sent, against its golden.
func TestEveryPushReadsAsWritten(t *testing.T) {
	for name, parts := range messages() {
		text, _ := message(parts)
		shapeOf(t, name, text)
		golden(t, "push", name, text)
	}
}

func TestCoalescedPartsPutWhatNeedsYouFirst(t *testing.T) {
	text, loud := message(messages()["coalesced-key-failing-and-done"])
	if !loud {
		t.Error("a message with a loud part went out quiet")
	}
	red := strings.Index(text, "🔴")
	amber := strings.Index(text, markWarn)
	ok := strings.Index(text, "✅")
	if !(red >= 0 && red < amber && amber < ok) {
		t.Errorf("order is not red, amber, green:\n%s", text)
	}
	if strings.Count(text, "\n\n") != 2 {
		t.Errorf("parts are not separated by one blank line each:\n%s", text)
	}
}

func TestATooLongMessageLosesItsDetailsFirst(t *testing.T) {
	w := writer{loc: lagos, now: testNow}
	var parts []part
	for i := 0; i < 20; i++ {
		parts = append(parts, w.databaseDown(testNow, strings.Repeat("x", 300)))
	}
	text, _ := message(parts)
	if strings.Contains(text, "blockquote") {
		t.Error("the details were kept in a message over 4,000 characters")
	}
	if !strings.Contains(text, "Can&#39;t reach the database") {
		t.Error("the message lost its headlines")
	}
}

// TestTheStaleAlertStandsInForTheImportsOwn: a failing import's cause
// and details are said once per message, by the stale alert, however
// the parts were queued.
func TestTheStaleAlertStandsInForTheImportsOwn(t *testing.T) {
	text, loud := message(messages()["coalesced-import-failing-and-stale"])
	if strings.Contains(text, "Catalog update failing since") {
		t.Errorf("the import's own alert went out beside the stale one:\n%s", text)
	}
	if n := strings.Count(text, "IMDb's files don't match"); n != 1 {
		t.Errorf("the cause is said %d times, want once:\n%s", n, text)
	}
	if n := strings.Count(text, "<blockquote"); n != 1 {
		t.Errorf("%d details quotes, want one:\n%s", n, text)
	}
	if !loud {
		t.Error("the reminder of a failing import went out quiet")
	}
}
