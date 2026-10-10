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
	synopsesDown := failingJob(notify.AllFailed, 2, 70*time.Minute)
	synopsesDown.Provider, synopsesDown.Lookups = "OMDb", 50
	synopsesDown.Detail = `catalog: all 50 lookups to OMDb failed; the last: omdb: HTTP 503 for tt0133093`
	trailersDown := failingJob(notify.AllFailed, 2, 70*time.Minute)
	trailersDown.Provider, trailersDown.Lookups = "TMDb", 50
	trailersDown.Detail = `catalog: all 50 lookups to TMDb failed; the last: tmdb: giving up after 4 attempts: tmdb: HTTP 503`
	// A day with no movie that makes a fair puzzle, two hourly passes
	// running.
	dailyNone := failingJob(notify.Unknown, 2, 61*time.Minute)
	dailyNone.NextTry = testNow.Add(time.Hour)
	dailyNone.Detail = "catalog: none of 1842 candidates can be the daily answer for 2026-10-04"
	peopleDown := failingJob(notify.AllFailed, 2, 70*time.Minute)
	peopleDown.Provider, peopleDown.Lookups = "TMDb", 50
	peopleDown.Detail = `catalog: all 50 lookups to TMDb failed; the last: tmdb: giving up after 4 attempts: tmdb: HTTP 503`

	// The GeoIP check fails once per twelve-hourly check, so its streaks
	// are twelve hours apart and its next try half a day away.
	maxmindDown := failingJob(notify.ProviderDown, 2, 12*time.Hour)
	maxmindDown.Provider, maxmindDown.Status, maxmindDown.Built = "MaxMind", 503, fridayBuild
	maxmindDown.NextTry = testNow.Add(12 * time.Hour)
	maxmindDown.Detail = "geoip: HEAD the database: HTTP 503"
	maxmindGone := failingJob(notify.ProviderDown, 2, 12*time.Hour)
	maxmindGone.Provider = "MaxMind"
	maxmindGone.NextTry = testNow.Add(12 * time.Hour)
	maxmindGone.Detail = "geoip: GET the database: dial tcp: lookup download.maxmind.com: no such host"
	geoOdd := failingJob(notify.Unknown, 2, 12*time.Hour)
	geoOdd.Built, geoOdd.NextTry = fridayBuild, testNow.Add(12*time.Hour)
	geoOdd.Detail = "geoip: the download is not gzip: gzip: invalid header"
	geoKey := &job{Detail: "geoip: MaxMind refused the license key (HTTP 401)", Built: fridayBuild}
	geoKeyFirst := &job{Detail: "geoip: MaxMind refused the license key (HTTP 401)"}
	newBuild := notify.Event{Job: notify.JobGeoIP, Kind: notify.Downloaded, LiveSince: fridayBuild, PrevAt: tuesdayBuild, Bytes: 9871234}
	firstBuild := notify.Event{Job: notify.JobGeoIP, Kind: notify.Downloaded, LiveSince: fridayBuild, Bytes: 9871234}

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
		"p6-synopses-omdb-down":    {w.jobFailing(notify.JobSynopses, synopsesDown)},
		"p6-trailers-tmdb-down":    {w.jobFailing(notify.JobTrailers, trailersDown)},
		"p6-people-tmdb-down":      {w.jobFailing(notify.JobPeople, peopleDown)},
		"p6-daily-unexpected":      {w.jobFailing(notify.JobDaily, dailyNone)},
		"p7-tmdb-key":              {w.keyRejected("TMDb", &job{Detail: "tmdb: HTTP 401: Invalid API key: You must be granted a valid key."})},
		"p7-omdb-key":              {w.keyRejected("OMDb", &job{Detail: "catalog: OMDb refused the key: omdb: invalid API key"})},
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
		"p10-synopses":             {w.longPass(notify.JobSynopses, notify.Event{Done: 36120, None: 1880, Errors: 14, Took: 52 * time.Minute}, "")},
		"p10-trailers":             {w.longPass(notify.JobTrailers, notify.Event{Done: 9310, None: 1204, Took: 38 * time.Minute}, "")},
		"p10-people-photos":        {w.longPass(notify.JobPeople, notify.Event{Done: 41280, None: 8812, Took: 2*time.Hour + 47*time.Minute}, "")},

		// The country lookup's messages, from the GeoIP check on the queue.
		"p12-country-lookup-updated":      {w.downloaded(newBuild, "")},
		"p12-country-lookup-first":        {w.downloaded(firstBuild, "")},
		"p12-country-lookup-fixed":        {w.downloaded(newBuild, w.fixedSentence(testNow.Add(-24*time.Hour)))},
		"p6-country-lookup-maxmind-down":  {w.jobFailing(notify.JobGeoIP, maxmindDown)},
		"p6-country-lookup-no-build":      {w.jobFailing(notify.JobGeoIP, maxmindGone)},
		"p6-country-lookup-unexpected":    {w.jobFailing(notify.JobGeoIP, geoOdd)},
		"p7-maxmind-key":                  {w.keyRejected("MaxMind", geoKey)},
		"p7-maxmind-key-no-build":         {w.keyRejected("MaxMind", geoKeyFirst)},
		"p8-maxmind-key-back":             {w.keyBack("MaxMind", testNow.Add(-36*time.Hour))},
		"p8-country-lookup-working-again": {w.workingAgain(notify.JobGeoIP, testNow.Add(-24*time.Hour), time.Time{}, true)},
		"p9-maxmind-key-reminder":         {w.reminder(&alert{Key: "key:maxmind", Since: testNow.Add(-24 * time.Hour), Severity: sevRed, Provider: "MaxMind"}, notify.JobGeoIP, geoKey)},
		"p9-country-lookup-reminder":      {w.reminder(&alert{Key: "job:geoip", Since: testNow.Add(-36 * time.Hour), Severity: sevWarn}, notify.JobGeoIP, maxmindDown)},

		// Three jobs with news in the same two seconds: a refused key, a
		// failure that has lasted, and a long pass that ended.
		"coalesced-key-failing-and-done": {w.longPass(notify.JobPosters, notify.Event{Done: 1830, None: 1204, Took: 34 * time.Minute}, ""),
			w.jobFailing(notify.JobColours, colours), w.keyRejected("TMDb", &job{Detail: "tmdb: HTTP 401: Invalid API key"})},
		// The import's second failed hour lands as the catalog turns a
		// day and a half old: the stale alert says the cause, once.
		"coalesced-import-failing-and-stale": {w.importFailing(mismatch, yesterday), w.stale(1, testNow.Add(-37*time.Hour), mismatch)},
	}
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

// TestADownloadNamesItsBuildWhateverItsDay: two builds on one day are
// not named as the same day twice, and a build more than a week old
// opens its sentence with a capital.
func TestADownloadNamesItsBuildWhateverItsDay(t *testing.T) {
	w := writer{loc: lagos, now: testNow}
	same := w.downloaded(notify.Event{Job: notify.JobGeoIP, Kind: notify.Downloaded,
		LiveSince: fridayBuild, PrevAt: fridayBuild.Add(-2 * time.Hour), Bytes: 9871234}, "")
	if body := stripTags(same.body); !strings.HasSuffix(body, "replacing an earlier build from the same day.") {
		t.Errorf("two builds in a day: %q", body)
	}
	old := w.downloaded(notify.Event{Job: notify.JobGeoIP, Kind: notify.Downloaded,
		LiveSince: testNow.Add(-10 * 24 * time.Hour), PrevAt: testNow.Add(-14 * 24 * time.Hour), Bytes: 9871234}, "")
	lines := strings.Split(stripTags(old.body), "\n")
	if len(lines) < 2 || !strings.HasPrefix(lines[1], "The ") {
		t.Errorf("a build from 17 Sep: %q, want its sentence to start \"The \"", lines)
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

// TestTheDailyFiguresAreWholePercentagesOfTheFinishedGames: the board's
// Daily lines count what was played, but read difficulty from the games
// that ended, rounding to the nearest whole percentage, so a day every
// finished game solved says 100%, which a running share never may.
func TestTheDailyFiguresAreWholePercentagesOfTheFinishedGames(t *testing.T) {
	for _, c := range []struct {
		part, all int64
		want      string
	}{{0, 0, "0%"}, {1, 3, "33%"}, {2, 3, "67%"}, {1, 2, "50%"}, {3, 3, "100%"}, {771, 1204, "64%"}} {
		if got := share(c.part, c.all); got != c.want {
			t.Errorf("share(%d, %d) = %q, want %q", c.part, c.all, got, c.want)
		}
	}
	s := &state{Jobs: map[string]*job{notify.JobDaily: {Daily: &notify.DailyDay{No: 4, Played: 5}}}}
	if got := s.dailyLines(); len(got) != 1 || got[0][1] != "No. 4 · 5 played · none finished" {
		t.Errorf("a day with games but none finished: %q", got)
	}
	s.Jobs[notify.JobDaily].Daily = &notify.DailyDay{No: 4, Played: 5, Finished: 2, Facts: 1, Sheets: 0, Median: 0}
	if got := s.dailyLines(); len(got) != 2 || got[0][1] != "No. 4 · 5 played · 0% solved" ||
		got[1][1] != "50% bought a fact · 0% opened a map · median 0" {
		t.Errorf("a day nobody solved, without the names: %q", got)
	}
	for _, l := range s.dailyLines() {
		if n := len([]rune(boardLine(markFigures, l[0], l[1]))); n > lineMax+len("<b></b>") {
			t.Errorf("%q runs past the line", l)
		}
	}
	if got := (&state{Jobs: map[string]*job{}}).dailyLines(); got != nil {
		t.Errorf("before the job has said: %q", got)
	}
}
