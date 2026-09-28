package telegram

import (
	"fmt"
	"strings"
	"time"

	"cinedikt/internal/notify"
)

// The board is one pinned message, edited in place, that answers "is
// everything all right" from the chat list without opening anything:
// a headline, one line per job in a fixed order, and a footer whose
// times double as the liveness signal. A "next catalog check" already
// in the past, or an "updated" stamp over an hour old, means the
// process is stuck; nothing else has to say so.

// lineMax is a hard stop, not a fit. A phone wraps a board line at
// about 35 to 40 characters whatever this says, which is why every line
// leads with its mark and a bold label that the eye can run down, and
// says one thing about its job rather than everything.
const lineMax = 80

// place is what the board says about where it is running.
type place struct {
	env    string
	commit string
	utc    bool
}

// render returns the board as sent, and the same without its
// "updated" stamp, which is what decides whether an edit is worth
// making.
func (s *state) render(w writer, at place) (full, body string) {
	mark, head := s.header(w)
	top := mark + " <b>" + esc(head) + "</b>"
	stamp := fmt.Sprintf(` · updated <tg-time unix="%d" format="r">%s</tg-time>`, w.now.Unix(), hhmm(w.now, w.loc))

	var b strings.Builder
	b.WriteString("\n")
	for _, in := range jobList {
		m, text := s.line(in.id, w)
		b.WriteString("\n")
		b.WriteString(boardLine(m, in.label, text))
	}
	b.WriteString("\n")
	if site := s.siteLine(w); site != "" {
		b.WriteString("\n<i>" + esc(site) + "</i>")
	}
	b.WriteString("\n<i>" + esc(s.footer(w, at)) + "</i>")
	rest := b.String()
	return top + stamp + rest, top + rest
}

// boardLine is "{mark} <b>{label}</b> · {state}", cut to lineMax runes
// before anything is escaped.
func boardLine(mark, label, text string) string {
	room := lineMax - len([]rune(mark+" "+label+" · "))
	return mark + " <b>" + esc(label) + "</b> · " + esc(cut(text, room))
}

// header is the one line that decides whether the rest needs reading.
// The first rule that matches wins, most urgent first.
func (s *state) header(w writer) (string, string) {
	if s.shutting {
		// Not "restarting": a service that is stopped, or removed,
		// leaves this up for good, and it has to stay true.
		return markPause, "Shutting down"
	}
	if s.Alerts[alertDatabase] != nil {
		return markRed, "Can't reach the database"
	}
	// Problems, not jobs: two jobs stopped by one refused key are one
	// thing for a person to fix.
	var red, amber []string
	seen := map[string]bool{}
	for _, in := range jobList {
		j := s.Jobs[in.id]
		if j == nil || j.State != stFailing {
			continue
		}
		key := alertKey(in.id, j)
		if seen[key] {
			continue
		}
		seen[key] = true
		if severity(j) == sevRed {
			red = append(red, in.id)
		} else {
			amber = append(amber, in.id)
		}
	}
	switch {
	case len(red) == 1:
		j := s.Jobs[red[0]]
		if j.Cause == notify.KeyRejected {
			return markRed, provider(j.Provider) + " turned down our key"
		}
		return markRed, info(red[0]).failing + " failing: unexpected error"
	case len(red) > 1:
		return markRed, fmt.Sprintf("%d problems need you", len(red))
	}
	age := s.age(w.now)
	if age > staleRed {
		return markRed, "Catalog is " + human(age) + " old"
	}
	switch {
	case len(amber) == 1:
		return markWarn, info(amber[0]).failing + " failing since " + w.when(s.Jobs[amber[0]].FailSince)
	case len(amber) > 1:
		return markWarn, fmt.Sprintf("%d jobs failing", len(amber))
	}
	if age > staleWarn {
		return markWarn, "Catalog is " + human(age) + " old"
	}
	if j := s.Jobs[notify.JobImport]; j != nil && j.State == stRunning {
		if s.LiveSince.IsZero() {
			return markWork, "Building the first catalog"
		}
		return markWork, "Updating the catalog"
	}
	var busy []string
	for _, in := range jobList[1:] {
		j := s.Jobs[in.id]
		if j != nil && j.State == stRunning && !j.PassBegan.IsZero() && w.now.Sub(j.PassBegan) >= time.Minute {
			busy = append(busy, in.id)
		}
	}
	switch {
	case len(busy) == 1:
		return markWork, busyHeadline(busy[0])
	case len(busy) > 1:
		return markWork, fmt.Sprintf("%d jobs running", len(busy))
	}
	if s.LiveSince.IsZero() {
		// Nothing wrong, and nothing to show either: the site has no
		// films until the first import publishes.
		return markWork, "Waiting for the first catalog"
	}
	return markOK, "All good"
}

func busyHeadline(id string) string {
	switch id {
	case notify.JobPosters:
		return "Filling in posters"
	case notify.JobTMDbPosters:
		return "Checking backup posters"
	case notify.JobTMDbIDs:
		return "Matching films for search"
	case notify.JobSynopses:
		return "Filling in synopses"
	case notify.JobTrailers:
		return "Finding trailers"
	default:
		return "Colouring the opening screen"
	}
}

// age is how old the live catalog is, worked out now rather than when
// something last said so.
func (s *state) age(now time.Time) time.Duration {
	if s.LiveSince.IsZero() {
		return 0
	}
	return now.Sub(s.LiveSince)
}

// line is one job's mark and state.
func (s *state) line(id string, w writer) (string, string) {
	if id == notify.JobImport {
		return s.catalogLine(w)
	}
	j := s.Jobs[id]
	in := info(id)
	state := stStarting
	if j != nil && j.State != "" {
		state = j.State
	}
	if state == stOff {
		return markPause, "off (" + in.offWhy + ")"
	}
	if a := s.Alerts[alertDatabase]; a != nil {
		return markPause, "on hold"
	}
	if (state == stStarting || state == stWaiting) && s.LiveSince.IsZero() {
		return markWork, "waiting for the first catalog"
	}
	switch state {
	case stRunning:
		return markWork, runningText(id, j, w)
	case stPaused:
		return markPause, "OMDb daily limit" + w.after(" · resumes after ", j.NextTry)
	case stFailing:
		if j.Cause == notify.KeyRejected {
			// Retrying with the same key only gets the same answer.
			return markRed, shortCause(j.failure()) + " · new key needed"
		}
		return markFor(severity(j)), shortCause(j.failure()) + w.after(" · next try by ", j.NextTry)
	case stInterrupted:
		return markPause, "interrupted · will carry on"
	case stIdle:
		return markOK, idleText(id, j, w)
	default:
		return markWork, "starting"
	}
}

// after is " · lead {when}", or nothing when there is no time to name.
func (w writer) after(lead string, t time.Time) string {
	if t.IsZero() {
		return ""
	}
	return lead + w.when(t)
}

func (s *state) catalogLine(w writer) (string, string) {
	j := s.Jobs[notify.JobImport]
	if j == nil {
		j = &job{State: stStarting}
	}
	if a := s.Alerts[alertDatabase]; a != nil {
		return markRed, "on hold · no database since " + w.when(a.Since)
	}
	switch j.State {
	case stRunning:
		return markWork, importStep(j)
	case stFailing:
		text := shortCause(j.failure())
		if j.Fails > 1 {
			text += fmt.Sprintf(" · %d tries", j.Fails)
		}
		// Past three days the catalog's age is the problem, whatever
		// the cause, and the line agrees with the header about it.
		m := markFor(severity(j))
		if s.age(w.now) > staleRed {
			m = markRed
		}
		return m, text + w.after(" · next try ", j.NextTry)
	case stSkipped:
		return markPause, "IMDb changed a file mid-download" + w.after(" · next try ", j.NextTry)
	case stInterrupted:
		return markPause, "interrupted · will start over"
	}
	if s.LiveSince.IsZero() {
		return markWork, "not built yet" + w.after(" · next try ", s.NextCheck)
	}
	switch age := s.age(w.now); {
	case age > staleRed:
		return markRed, "live since " + w.when(s.LiveSince) + " · IMDb has nothing new"
	case age > staleWarn:
		return markWarn, "live since " + w.when(s.LiveSince) + " · IMDb has nothing new"
	}
	text := "live since " + w.when(s.LiveSince)
	if s.Films > 0 {
		text = count(s.Films) + " films · " + text
	}
	return markOK, text
}

func importStep(j *job) string {
	switch j.Step {
	case 2:
		noun := j.Noun
		if noun == "" {
			noun = "the files"
		}
		return "step 2 of 4 · loading " + noun
	case 3:
		return "step 3 of 4 · building search indexes"
	case 4:
		return "step 4 of 4 · going live"
	}
	if j.File == 0 {
		return "step 1 of 4 · downloading IMDb files"
	}
	// The verb stays, as it does in step 2, and the share is this
	// file's, said after the file so it is not read as the import's.
	text := fmt.Sprintf("step 1 of 4 · downloading file %d of %d", j.File, j.Files)
	if j.Noun != "" {
		text = fmt.Sprintf("step 1 of 4 · downloading %s (file %d of %d)", j.Noun, j.File, j.Files)
	}
	switch {
	case j.Bytes > 0:
		text += fmt.Sprintf(" · %s MB so far", count(j.Bytes/(1<<20)))
	case pct(j.Share) >= 1:
		text += fmt.Sprintf(" · %d%%", pct(j.Share))
	}
	return text
}

// runningText is a pass's progress: how much there is before anything
// is done, then how far, then when it should end.
func runningText(id string, j *job, w writer) string {
	total := count(j.Total)
	switch {
	case j.Total > 0 && (j.Share >= 0.99 || (!j.ETA.IsZero() && j.ETA.Sub(w.now) < 2*time.Minute)):
		return "almost done"
	case j.Total > 0 && pct(j.Share) >= 1:
		text := fmt.Sprintf("%d%% of %s films", pct(j.Share), total)
		if !j.ETA.IsZero() {
			text += " · done about " + w.when(roundETA(j.ETA, w.now))
		}
		return text
	}
	switch id {
	case notify.JobPosters:
		return total + " films to look up"
	case notify.JobTMDbPosters:
		return "checking " + total + " films"
	case notify.JobTMDbIDs:
		return total + " films to match"
	case notify.JobSynopses:
		return total + " films to look up"
	case notify.JobTrailers:
		return "checking " + total + " films"
	default:
		return total + " films to colour"
	}
}

// idleText is a job that is up to date, and the one thing worth
// knowing about its last pass. Routine retries are not it: a line that
// is fine says so and stops.
func idleText(id string, j *job, w writer) string {
	text := "up to date"
	lately := func(verb string) string {
		if j.LastDone <= 0 || j.LastAt.IsZero() {
			return ""
		}
		switch dayGap(j.LastAt, w.now, w.loc) {
		case 0:
			return " · " + count(j.LastDone) + " " + verb + " today"
		case -1:
			return " · " + count(j.LastDone) + " " + verb + " yesterday"
		}
		return ""
	}
	switch id {
	case notify.JobPosters:
		text += lately("added")
	case notify.JobTMDbPosters:
		text += lately("found")
	case notify.JobTMDbIDs:
		text += lately("matched")
	case notify.JobSynopses:
		text += lately("added")
	case notify.JobTrailers:
		text += lately("found")
	case notify.JobColours:
		if j.LastErrors > 0 {
			text += " · " + count(j.LastErrors) + " left without a colour"
		}
	}
	return text
}

// siteLine says which catalog the site is serving while the import is
// busy or failing, when the catalog line is about the import instead.
// An idle catalog line already says it.
func (s *state) siteLine(w writer) string {
	j := s.Jobs[notify.JobImport]
	busy := j != nil && (j.State == stRunning || j.State == stFailing)
	if !busy && !s.LiveSince.IsZero() {
		return ""
	}
	if s.LiveSince.IsZero() {
		return "Site has no catalog yet"
	}
	return "Site shows the catalog from " + w.when(s.LiveSince)
}

func (s *state) footer(w writer, at place) string {
	env := at.env
	if env == "" {
		env = "local"
	}
	var parts []string
	switch {
	case s.Alerts[alertDatabase] != nil:
		parts = append(parts, "Retrying every 30 s")
	case !s.NextCheck.IsZero():
		parts = append(parts, "Next catalog check "+w.when(s.NextCheck))
	}
	// The deploy, then how long it has been up: "up since" beside a
	// check time could otherwise be read as the check running since.
	where := env
	if len(at.commit) >= 7 {
		where += " " + at.commit[:7]
	} else if at.commit != "" {
		where += " " + at.commit
	}
	parts = append(parts, where)
	if !s.RunningSince.IsZero() {
		parts = append(parts, "up since "+w.when(s.RunningSince))
	}
	if at.utc {
		parts = append(parts, "times UTC")
	}
	return strings.Join(parts, " · ")
}
