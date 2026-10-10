package telegram

import (
	"fmt"
	"sort"
	"strings"
	"time"

	"cinedikt/internal/notify"
)

// Every sentence the chat ever shows is written in this file, so the
// voice stays one voice: neutral, "you" for the reader, never "I", no
// app name, no exclamation marks, and a full stop at the end of every
// sentence and nowhere else.

// The five marks a line can start with. One per line, always followed
// by words, so nothing is carried by the colour alone. ⏸️ and ⚠️ are
// written with U+FE0F so every client draws them as emoji.
const (
	markOK    = "✅"
	markWork  = "⏳"
	markPause = "⏸️"
	markWarn  = "⚠️"
	markRed   = "🔴"
	// markFigures leads a line of figures rather than a state: the
	// Daily's day before.
	markFigures = "📊"
)

// Severity orders the parts of a push and picks its mark: something
// that needs you comes first, then a heads-up, then something that
// stopped on purpose, then good news.
const (
	sevOK = iota
	sevPause
	sevWarn
	sevRed
)

func markFor(sev int) string {
	switch sev {
	case sevRed:
		return markRed
	case sevWarn:
		return markWarn
	case sevPause:
		return markPause
	default:
		return markOK
	}
}

// jobInfo is how a job is named. The same names are used on the board
// and in every push, so a person learns one name per job and no more.
type jobInfo struct {
	id    string
	label string
	// failing is the name in a failure headline, where "Catalog
	// failing" would read as the catalog itself being broken.
	failing string
	// offWhy is why the job is off when it is.
	offWhy string
}

var jobList = []jobInfo{
	{notify.JobImport, "Catalog", "Catalog update", ""},
	{notify.JobPosters, "Posters", "Posters", "no OMDb key"},
	{notify.JobTMDbPosters, "Backup posters", "Backup posters", "no TMDb key"},
	{notify.JobTMDbIDs, "Search matching", "Search matching", "no TMDb key"},
	{notify.JobSynopses, "Synopses", "Synopses", "no OMDb key"},
	{notify.JobTrailers, "Trailers", "Trailers", "no TMDb key"},
	{notify.JobPeople, "People photos", "People photos", "no TMDb key"},
	{notify.JobColours, "Opening colours", "Opening colours", ""},
	{notify.JobDaily, "Daily puzzles", "Daily puzzles", ""},
	// The check runs on where to watch's queue, so it is off with
	// MaxMind credentials set when where to watch is: no
	// STREAMING_API_KEY, or a queue that would not start.
	{notify.JobGeoIP, "Country lookup", "Country lookup", "no MaxMind credentials, or where to watch is off"},
}

func info(id string) jobInfo {
	for _, j := range jobList {
		if j.id == id {
			return j
		}
	}
	return jobInfo{id: id, label: id, failing: id}
}

// loudFor says whether a job's trouble may make a sound at all. The
// opening colours are a placeholder tint on the first screen while its
// posters load; nothing about them is worth waking anyone for.
func loudFor(id string) bool { return id != notify.JobColours }

// part is one push's worth of words. Several parts queued within a
// couple of seconds of each other go out as one message.
type part struct {
	sev  int
	loud bool
	body string
	// detail is the expandable quote with the raw error, kept apart so
	// a message that would run past Telegram's limit can lose it first.
	detail string
	// about names what the part reports, and covers what it makes
	// unnecessary to say in the same message: the stale alert for a
	// failing import already says why, so the import's own failure
	// alert is left out beside it.
	about, covers string
}

// What a part can be about, for covers.
const aboutImportFailing = "import-failing"

// writer renders words for one moment in one place.
type writer struct {
	loc   *time.Location
	now   time.Time
	token string
}

func (w writer) when(t time.Time) string { return when(t, w.now, w.loc) }
func (w writer) day(t time.Time) string  { return possessive(t, w.now, w.loc) }

// push lays out one part: the headline in bold, then what it asks of
// you, then the lines that explain it, then the details quote if there
// is a raw error to show. The action is second because a lock screen
// shows three or four lines and cuts the rest, and "do I need to do
// anything?" is the question a sound raises. Good news has no action.
func (w writer) push(sev int, loud bool, headline, detail, action string, lines ...string) part {
	var b strings.Builder
	b.WriteString(markFor(sev))
	b.WriteString(" <b>")
	b.WriteString(esc(headline))
	b.WriteString("</b>")
	for _, l := range append([]string{action}, lines...) {
		if l == "" {
			continue
		}
		b.WriteByte('\n')
		b.WriteString(l)
	}
	p := part{sev: sev, loud: loud, body: b.String()}
	if d := w.detail(detail); d != "" {
		p.detail = "\n<blockquote expandable>Details: " + esc(d) + "</blockquote>"
	}
	return p
}

// detail is the raw error made fit to show: redacted, one line, and
// short enough that it stays a detail.
func (w writer) detail(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return ""
	}
	return cut(redact(raw, w.token), 300)
}

// times is a count of failures in words.
func times(n int) string {
	switch n {
	case 1:
		return "once"
	case 2:
		return "twice"
	default:
		return fmt.Sprintf("%d times", n)
	}
}

// failure is what a job's current streak is about, as the words need it.
type failure struct {
	cause     notify.Cause
	provider  string
	status    int
	integrity float64
	lookups   int64
}

func (j *job) failure() failure {
	return failure{cause: j.Cause, provider: j.Provider, status: j.Status, integrity: j.Integrity, lookups: j.Lookups}
}

// imageHosts is the provider the colour job names: the hosts it
// downloads poster images from, which it does not look anything up on.
const imageHosts = "poster hosts"

// causeSentence is why it failed, in one full sentence.
func causeSentence(f failure) string {
	switch f.cause {
	case notify.IMDbDown:
		if f.status != 0 {
			return fmt.Sprintf("IMDb's download server isn't answering (error %d).", f.status)
		}
		return "IMDb's download server isn't answering."
	case notify.FilesMismatch:
		if f.integrity > 0 {
			return "IMDb's files don't match each other yet (" + tenth(f.integrity) +
				"% match; 99% needed), which usually means IMDb is partway through publishing."
		}
		return "IMDb's files look incomplete."
	case notify.DatabaseBusy:
		return "The database is too busy: a query timed out."
	case notify.DatabaseDown:
		return "Can't connect to the database."
	case notify.AllFailed:
		if f.provider == imageHosts {
			return fmt.Sprintf("All %s poster images failed to download.", count(f.lookups))
		}
		return fmt.Sprintf("All %s lookups to %s failed.", count(f.lookups), esc(provider(f.provider)))
	case notify.KeyRejected:
		return esc(provider(f.provider)) + " turned down our key."
	case notify.ProviderDown:
		if f.status != 0 {
			return fmt.Sprintf("%s's download server isn't answering (error %d).", esc(provider(f.provider)), f.status)
		}
		return esc(provider(f.provider)) + "'s download server isn't answering."
	default:
		return "The error isn't one the app recognises, so it may need a code fix."
	}
}

// shortCause is the same thing as a fragment, for a board line.
func shortCause(f failure) string {
	switch f.cause {
	case notify.IMDbDown:
		if f.status != 0 {
			return fmt.Sprintf("IMDb not answering (%d)", f.status)
		}
		return "IMDb not answering"
	case notify.DatabaseBusy:
		return "database too busy"
	case notify.DatabaseDown:
		return "can't reach the database"
	case notify.FilesMismatch:
		return "IMDb files don't match yet"
	case notify.KeyRejected:
		return provider(f.provider) + " turned down our key"
	case notify.ProviderDown:
		if f.status != 0 {
			return fmt.Sprintf("%s not answering (%d)", provider(f.provider), f.status)
		}
		return provider(f.provider) + " not answering"
	case notify.AllFailed:
		if f.provider == imageHosts {
			return "poster images not loading"
		}
		return "every lookup failed"
	default:
		return "unexpected error"
	}
}

func provider(p string) string {
	if p == "" {
		return "the service"
	}
	return p
}

// siteSentence is what a catalog failure means for the people using
// the site, which is the second thing anyone wants to know.
func (w writer) siteSentence(liveSince time.Time) string {
	if liveSince.IsZero() {
		return "The site has no films until the first catalog is built."
	}
	return "The site is fine and still shows " + w.day(liveSince) + " catalog."
}

// impactSentence is the same for a background job. j is the job, for
// the one sentence that turns on its state: whether the country lookup
// has a build to place readers with. It may be nil.
func impactSentence(id string, j *job) string {
	switch id {
	case notify.JobPosters:
		return "No new posters are being saved; the site still works."
	case notify.JobTMDbPosters:
		return "Films OMDb has no poster for stay blank for now; the site still works."
	case notify.JobTMDbIDs:
		return "Search still works for films already matched."
	case notify.JobSynopses:
		return "Films without a synopsis stay without one for now; the site still works."
	case notify.JobTrailers:
		return "Films without a trailer stay without one for now; the site still works."
	case notify.JobPeople:
		return "People without a photo stay without one for now; the site still works."
	case notify.JobColours:
		return "New films on the opening screen show without their placeholder colour; nothing else is affected."
	case notify.JobDaily:
		return "No new days of Cinedikt Daily are being picked; the days already picked, up to eight days ahead, still play."
	case notify.JobGeoIP:
		if j == nil || j.Built.IsZero() {
			return "Until the first GeoLite2 build is downloaded, readers can't be placed in a country, so the site leaves out where to watch."
		}
		return "Readers' countries come from the build already in use; where to watch still works."
	default:
		return ""
	}
}

// keyVar is the variable to check when a provider refuses the key.
// MaxMind's permalink takes the account id with the key, so a key that
// is right under the wrong account is refused too.
func keyVar(p string) string {
	switch p {
	case "TMDb":
		return "TMDB_ACCESS_TOKEN (or TMDB_API_KEY)"
	case "MaxMind":
		return "MAXMIND_LICENSE_KEY and MAXMIND_ACCOUNT_ID"
	}
	return "OMDB_API_KEY"
}

// keyImpact is what a refused key has stopped, and what the site shows
// meanwhile, so "stopped" is not read as pictures vanishing. j is a job
// on the key, for the country lookup's sentence about its build. It may
// be nil.
func keyImpact(p string, j *job) string {
	switch p {
	case "TMDb":
		return "Backup posters, search matching, trailers and people photos have stopped. Films OMDb has no poster for stay blank until this is fixed."
	case "MaxMind":
		return "Checks for new GeoLite2 builds have stopped. " + impactSentence(notify.JobGeoIP, j)
	}
	return "Poster and synopsis lookups have stopped. Posters already saved still show; new films will have none until this is fixed."
}

// keyAction is what fixes a refused key.
func keyAction(p string) string {
	return "<b>To fix:</b> check " + keyVar(p) + " under Variables in Railway, then redeploy."
}

func unexpectedSentence(fails int) string {
	return "It failed " + times(fails) + " in a row with an error the app doesn't recognise, so it may need a code fix."
}

// unexpectedAction is what to do about an error of no known kind: look
// at what it said. The details quote is only offered when there is one.
func (w writer) unexpectedAction(detail, lead string, next time.Time) string {
	look := "tap Details below or check the Railway logs."
	if w.detail(detail) == "" {
		look = "check the Railway logs."
	}
	return "<b>To fix:</b> " + look + w.nextTry(lead, next, ".")
}

// databaseRetry is the promise both database alerts end on.
const databaseRetry = "The app retries every 30 s and will post here when the database is back."

// P1: a new catalog went live. fixedSince, when set, is an import
// failure streak that this publish ended and that had been announced.
func (w writer) published(e notify.Event, fixedSince time.Time) part {
	films := count(e.Films) + " films"
	if e.PrevFilms > 0 {
		prev := "the last"
		if !e.PrevAt.IsZero() {
			prev = w.day(e.PrevAt)
		}
		switch diff := e.Films - e.PrevFilms; {
		case diff > 0:
			films += " (" + count(diff) + " more than " + prev + " catalog)"
		case diff < 0:
			films += " (" + count(-diff) + " fewer than " + prev + " catalog)"
		default:
			films += " (same as " + prev + " catalog)"
		}
	}
	body := films + " and " + count(e.People) + " people. Took " + human(e.Took) + "."
	fixed := ""
	if !fixedSince.IsZero() {
		fixed = "Updates had been failing since " + w.when(fixedSince) + "; that's fixed."
	}
	return w.push(sevOK, false, liveHeadline(e), "", "", body, fixed)
}

// liveHeadline tells the first catalog a site ever had from the daily
// one, which is the difference between "it works now" and "as usual".
func liveHeadline(e notify.Event) string {
	if e.PrevFilms == 0 {
		return "First catalog is live"
	}
	return "New catalog is live"
}

// P2 and P3: the import has been failing long enough to say so.
func (w writer) importFailing(j *job, liveSince time.Time) part {
	f := j.failure()
	var p part
	if f.cause == notify.Unknown {
		p = w.push(sevRed, true, "Catalog update failing: unexpected error", j.Detail,
			w.unexpectedAction(j.Detail, " Next try ", j.NextTry),
			unexpectedSentence(j.Fails)+" "+w.siteSentence(liveSince))
	} else {
		p = w.push(sevWarn, true, "Catalog update failing since "+w.when(j.FailSince), j.Detail,
			"Nothing for you to do."+w.nextTry(" Next try ", j.NextTry, "."),
			causeSentence(f)+" "+w.siteSentence(liveSince))
	}
	p.about = aboutImportFailing
	return p
}

// nextTry is "Next try 14:00." and friends, or nothing when there is
// no scheduled time to name: a time that is not real is never shown.
func (w writer) nextTry(lead string, t time.Time, tail string) string {
	if t.IsZero() {
		return ""
	}
	return lead + w.when(t) + tail
}

// P4: the lease connection has not reached Postgres for a while.
func (w writer) databaseDown(since time.Time, detail string) part {
	return w.push(sevRed, true, "Can't reach the database", detail,
		"<b>To fix:</b> check the Postgres service in Railway.",
		"The app hasn't been able to connect to Postgres since "+w.when(since)+
			", so the site is probably down and the catalog jobs are on hold. "+databaseRetry)
}

// P4r: it can again. jobsHere says whether this process has taken the
// jobs back, or whether another one is running them.
func (w writer) databaseBack(since time.Time, jobsHere bool) part {
	then := "The catalog jobs have started again."
	if !jobsHere {
		then = "The catalog jobs are running again."
	}
	return w.push(sevOK, false, "Database is back", "", "",
		"It was unreachable for "+human(w.now.Sub(since))+" ("+w.when(since)+" to "+w.when(w.now)+"). "+then)
}

// P5: the live catalog is getting old. imp is the import job, whose
// failure streak, if any, is the likely reason. When it is, this is also
// the reminder of that streak, and says its cause in place of the
// import's own alert if both land together.
func (w writer) stale(level int, liveSince time.Time, imp *job) part {
	head := "Catalog is " + human(w.now.Sub(liveSince)) + " old"
	failing := imp != nil && imp.Fails > 0
	body := "The live catalog was built " + w.when(liveSince) +
		" and IMDb hasn't put out a complete new set since. The site works; it's just not up to date."
	detail := ""
	if failing {
		body = "Updates have failed " + times(imp.Fails) + " since " + w.when(imp.FailSince) + ". " +
			causeSentence(imp.failure()) + " The site works but is getting out of date."
		detail = imp.Detail
	}
	var p part
	switch {
	case level >= 2:
		p = w.push(sevRed, true, head, detail,
			"<b>To fix:</b> check whether datasets.imdbws.com loads in a browser. If it does, look at the Railway logs.", body)
	case failing && imp.Cause == notify.Unknown && imp.Fails >= 2:
		// The same streak the import's own alert said may need a code
		// fix; a day and a half on, it does not now say to wait.
		p = w.push(severity(imp), true, head, detail, w.unexpectedAction(detail, " Next try ", imp.NextTry), body)
	case failing:
		p = w.push(sevWarn, true, head, detail,
			"Nothing for you to do unless it passes 3 days."+w.nextTry(" Next try ", imp.NextTry, "."), body)
	default:
		// IMDb late with its files is nobody's to fix, so no sound.
		p = w.push(sevWarn, false, head, "", "Nothing for you to do unless it passes 3 days. Checking every hour.", body)
	}
	if failing {
		p.covers = aboutImportFailing
	}
	return p
}

// P6: a background job has been failing long enough to say so.
func (w writer) jobFailing(id string, j *job) part {
	in := info(id)
	f := j.failure()
	if f.cause == notify.Unknown {
		return w.push(sevRed, loudFor(id), in.failing+" failing: unexpected error", j.Detail,
			w.unexpectedAction(j.Detail, " Next try by ", j.NextTry),
			unexpectedSentence(j.Fails)+" "+impactSentence(id, j))
	}
	why := causeSentence(f) + " " + impactSentence(id, j)
	if f.cause == notify.DatabaseBusy {
		why += " If it lasts, check the database's load in Railway."
	}
	return w.push(sevWarn, loudFor(id), in.failing+" failing since "+w.when(j.FailSince), j.Detail,
		"Nothing for you to do yet."+w.nextTry(" Next try by ", j.NextTry, "."), why)
}

// P7: a provider refused the key. Nothing fixes that but a person. j is
// the job that met the refusal, for its details and its build.
func (w writer) keyRejected(p string, j *job) part {
	return w.push(sevRed, true, p+" turned down our key", j.Detail, keyAction(p), keyImpact(p, j))
}

// fixedSentence is how a recovery reads, inside P8 or folded into P10
// or P12.
// It names no subject, so it reads the same under any headline.
func (w writer) fixedSentence(since time.Time) string {
	return "The problem lasted " + human(w.now.Sub(since)) + " (" + w.when(since) + " to " + w.when(w.now) + ")."
}

// P8: an announced failure is over.
func (w writer) workingAgain(id string, since time.Time, liveSince time.Time, viaCheck bool) part {
	label := info(id).label
	if id == notify.JobImport {
		label = "Catalog updates"
	}
	body := w.fixedSentence(since)
	if id == notify.JobImport && viaCheck && !liveSince.IsZero() {
		body += " IMDb has no new files yet, so the site still shows " + w.day(liveSince) + " catalog."
	}
	return w.push(sevOK, false, label+" working again", "", "", body)
}

// P8, for a key every job that uses it has stopped failing on.
func (w writer) keyBack(p string, since time.Time) part {
	back := "Poster lookups are running again."
	switch p {
	case "TMDb":
		back = "Backup posters, search matching, trailers and people photos are running again."
	case "MaxMind":
		back = "Checks for new GeoLite2 builds are running again."
	}
	return w.push(sevOK, false, p+" key works again", "", "",
		"It was turned down from "+w.when(since)+" to "+w.when(w.now)+" ("+human(w.now.Sub(since))+"). "+back)
}

// P9: an alert still open a day after it was last said. What was
// "nothing for you to do yet" a day ago is now something to look at.
func (w writer) reminder(a *alert, id string, j *job) part {
	lasted := w.when(a.Since) + " (" + human(w.now.Sub(a.Since)) + ")"
	switch {
	case a.Key == alertDatabase:
		return w.push(sevRed, true, "Still can't reach the database", a.Detail,
			"<b>To fix:</b> check the Postgres service in Railway.",
			"Unreachable since "+lasted+", so the site is probably down and the catalog jobs are on hold. "+databaseRetry)
	case strings.HasPrefix(a.Key, "key:"):
		p := a.Provider
		detail := ""
		if j != nil {
			detail = j.Detail
		}
		return w.push(sevRed, true, p+" key still turned down", detail, keyAction(p),
			"Turned down since "+lasted+". "+keyImpact(p, j))
	}
	if j == nil {
		return part{}
	}
	f := j.failure()
	in := info(id)
	why := "Failing since " + lasted + ". " + causeSentence(f) + " " + impactSentence(id, j)
	if f.cause == notify.Unknown {
		return w.push(sevRed, loudFor(id), in.failing+" still failing", j.Detail,
			w.unexpectedAction(j.Detail, " Next try by ", j.NextTry), why)
	}
	action := "It hasn't fixed itself in a day. <b>To fix:</b> look at the Railway logs."
	if f.cause == notify.DatabaseBusy {
		action = "It hasn't fixed itself in a day. <b>To fix:</b> check the database's load in Railway."
	}
	return w.push(a.Severity, loudFor(id), in.failing+" still failing", j.Detail, action, why)
}

// P10: a pass long enough that its end is news. fixed, when set, is
// the recovery sentence of an announced failure this pass ended.
func (w writer) longPass(id string, e notify.Event, fixed string) part {
	var head string
	body := "Took " + human(e.Took) + "."
	switch id {
	case notify.JobPosters:
		head = "Posters done: " + count(e.Done) + " saved"
		if e.None > 0 {
			body += " " + count(e.None) + " films have no poster on OMDb."
		}
		if e.Errors > 0 {
			body += " " + count(e.Errors) + " lookups failed and will be retried tomorrow."
		}
	case notify.JobTMDbIDs:
		head = "Search matching done: " + count(e.Done) + " matched"
		if e.None > 0 {
			body += " " + count(e.None) + " films have no match on TMDb."
		}
	case notify.JobTMDbPosters:
		head = "Backup posters done: " + count(e.Done) + " found"
		if e.None > 0 {
			body += " " + count(e.None) + " films have no poster on TMDb either."
		}
	case notify.JobSynopses:
		head = "Synopses done: " + count(e.Done) + " saved"
		if e.None > 0 {
			body += " " + count(e.None) + " films have no synopsis on OMDb."
		}
		if e.Errors > 0 {
			body += " " + count(e.Errors) + " lookups failed and will be retried."
		}
	case notify.JobTrailers:
		head = "Trailers done: " + count(e.Done) + " found"
		if e.None > 0 {
			body += " " + count(e.None) + " films have no trailer that can play here."
		}
	case notify.JobPeople:
		head = "People photos done: " + count(e.Done) + " found"
		if e.None > 0 {
			body += " " + count(e.None) + " people have no photo on TMDb."
		}
	default:
		head = info(id).label + " done: " + count(e.Done) + " coloured"
	}
	return w.push(sevOK, false, head, "", "", body, fixed)
}

// P12: a new GeoLite2 build was downloaded, checked and is in use. The
// first one a database ever had says so, which is the difference between
// "readers can be placed now" and "as usual". fixed, when set, is the
// recovery sentence of an announced failure this download ended.
func (w writer) downloaded(e notify.Event, fixed string) part {
	build := "A new GeoLite2 build"
	if !e.LiveSince.IsZero() {
		build = upperFirst(w.day(e.LiveSince)) + " GeoLite2 build"
	}
	if e.Bytes > 0 {
		build += " (" + size(e.Bytes) + ")"
	}
	head := "Country lookup: new build downloaded"
	body := build + " is in use now"
	switch {
	case e.PrevAt.IsZero():
		head = "Country lookup: first build downloaded"
		body += ", so where to watch can tell which country each reader is in."
	case !e.LiveSince.IsZero() && dayGap(e.PrevAt, e.LiveSince, w.loc) == 0:
		// MaxMind put out two builds in a day. Naming the same day twice
		// would read as the build replacing itself.
		body += ", replacing an earlier build from the same day."
	default:
		body += ", replacing " + w.day(e.PrevAt) + " build."
	}
	return w.push(sevOK, false, head, "", "", body, fixed)
}

// message is what goes out for a batch of parts: the most urgent
// first, a blank line between them, and a sound if any part wants one.
// A part another one covers is left out, so the same cause and the same
// details are never said twice in one message. Telegram refuses
// anything past 4,096 characters, so past 4,000 the details quotes go
// first; they are the part nobody needs at a glance.
func message(parts []part) (text string, loud bool) {
	covered := map[string]bool{}
	for _, p := range parts {
		if p.covers != "" {
			covered[p.covers] = true
		}
	}
	sorted := make([]part, 0, len(parts))
	for _, p := range parts {
		if p.about == "" || !covered[p.about] {
			sorted = append(sorted, p)
		}
	}
	sort.SliceStable(sorted, func(a, b int) bool { return sorted[a].sev > sorted[b].sev })
	join := func(details bool) string {
		out := make([]string, 0, len(sorted))
		for _, p := range sorted {
			s := p.body
			if details {
				s += p.detail
			}
			out = append(out, s)
		}
		return strings.Join(out, "\n\n")
	}
	for _, p := range sorted {
		loud = loud || p.loud
	}
	text = join(true)
	if visible(text) > 4000 {
		text = join(false)
	}
	return text, loud
}
