package telegram

import (
	"encoding/json"
	"sort"
	"strings"
	"time"

	"cinedikt/internal/notify"
)

// What the chat has been told, and what every job is doing, kept as one
// value the loop goroutine owns. It is saved to Postgres after every
// push and every board write, so a deploy picks up where the last
// process left off instead of saying it all again.

const stateVersion = 1

// A job's state is one of these, always. A finished job reads as
// finished and a failing one as failing; nothing is inferred from the
// absence of news.
const (
	stIdle        = "idle"
	stRunning     = "running"
	stPaused      = "paused"
	stFailing     = "failing"
	stSkipped     = "skipped"
	stInterrupted = "interrupted"
	stOff         = "off"
	stWaiting     = "waiting"
	stStarting    = "starting"
)

// The thresholds the loudness rules turn on.
const (
	// announceAfter is how long a failure has to last before it makes
	// a sound. Forty-five minutes rather than sixty absorbs the jitter
	// in the hourly ticker, so the import's second failed hour counts.
	announceAfter = 45 * time.Minute
	// remindAfter is how long an open alert goes unsaid before it is
	// said again.
	remindAfter = 24 * time.Hour
	// longPass is how long a pass has to run before its end is news.
	longPass = 30 * time.Minute
	// Stale levels, measured from when the live catalog was built.
	staleWarn = 36 * time.Hour
	staleRed  = 72 * time.Hour
)

const alertDatabase = "database"

type state struct {
	V            int               `json:"v"`
	BoardID      int               `json:"board_id,omitempty"`
	BoardBody    string            `json:"board_body,omitempty"`
	RunningSince time.Time         `json:"running_since,omitzero"`
	NextCheck    time.Time         `json:"next_check,omitzero"`
	LiveSince    time.Time         `json:"live_since,omitzero"`
	Films        int64             `json:"films,omitempty"`
	Stale        staleMark         `json:"stale"`
	Jobs         map[string]*job   `json:"jobs"`
	Alerts       map[string]*alert `json:"alerts"`

	// shutting is the last board of a process on its way out. It is
	// not saved: the next process to take the jobs clears it anyway.
	shutting bool
}

// staleMark is how loudly the current live catalog has been called
// old: 0 not at all, 1 at a day and a half, 2 at three days. It is
// keyed on LiveSince, so a new catalog starts again from nothing.
type staleMark struct {
	LiveSince time.Time `json:"live_since,omitzero"`
	Level     int       `json:"level,omitempty"`
}

type job struct {
	State string `json:"state"`

	// While it runs.
	Step      int       `json:"step,omitempty"`
	File      int       `json:"file,omitempty"`
	Files     int       `json:"files,omitempty"`
	Noun      string    `json:"noun,omitempty"`
	Share     float64   `json:"share,omitempty"`
	ETA       time.Time `json:"eta,omitzero"`
	Total     int64     `json:"total,omitempty"`
	Bytes     int64     `json:"bytes,omitempty"`
	PassBegan time.Time `json:"pass_began,omitzero"`

	// The last pass that ended. For the GeoIP check, LastAt is the last
	// check that went well, and Built is when the build in use was built,
	// zero for none: what its line names, and what decides whether its
	// failures leave readers placed.
	LastAt     time.Time `json:"last_at,omitzero"`
	LastDone   int64     `json:"last_done,omitempty"`
	LastNone   int64     `json:"last_none,omitempty"`
	LastErrors int64     `json:"last_errors,omitempty"`
	Built      time.Time `json:"built,omitzero"`

	// The current failure streak. Fails counts consecutive failures;
	// it survives the job starting a new pass and ends only when a pass
	// or a check goes well.
	Cause     notify.Cause `json:"cause,omitempty"`
	Provider  string       `json:"provider,omitempty"`
	Status    int          `json:"status,omitempty"`
	Integrity float64      `json:"integrity,omitempty"`
	Lookups   int64        `json:"lookups,omitempty"`
	Detail    string       `json:"detail,omitempty"`
	Fails     int          `json:"fails,omitempty"`
	FailSince time.Time    `json:"fail_since,omitzero"`
	NextTry   time.Time    `json:"next_try,omitzero"`
	// Alert is the key of the announced alert this streak belongs to.
	Alert string `json:"alert,omitempty"`
}

// alert is something that has made, or will make, a sound. Several
// jobs failing for one reason share one: TMDb refusing the key stops
// two jobs and is one problem.
type alert struct {
	Key       string    `json:"key"`
	Since     time.Time `json:"since"`
	Announced time.Time `json:"announced,omitzero"`
	Reminded  time.Time `json:"reminded,omitzero"`
	Severity  int       `json:"severity"`
	Provider  string    `json:"provider,omitempty"`
	Detail    string    `json:"detail,omitempty"`
}

func newState() *state {
	return &state{V: stateVersion, Jobs: map[string]*job{}, Alerts: map[string]*alert{}}
}

// loadState reads a saved state. Anything it cannot read is a fresh
// start rather than an error: the worst that costs is one repeated
// message.
func loadState(raw []byte) *state {
	st := newState()
	if len(raw) == 0 {
		return st
	}
	var saved state
	if err := json.Unmarshal(raw, &saved); err != nil || saved.V != stateVersion {
		return st
	}
	if saved.Jobs == nil {
		saved.Jobs = map[string]*job{}
	}
	if saved.Alerts == nil {
		saved.Alerts = map[string]*alert{}
	}
	return &saved
}

func (s *state) job(id string) *job {
	j, ok := s.Jobs[id]
	if !ok {
		j = &job{State: stStarting}
		s.Jobs[id] = j
	}
	return j
}

// effects is what one event asks of the loop: parts to push, and
// whether the board needs rewriting, or rewriting even if it reads the
// same (so its "updated" stamp moves).
type effects struct {
	parts []part
	dirty bool
	force bool
}

func (s *state) apply(e notify.Event, w writer) effects {
	fx := effects{dirty: true}
	at := e.At
	// Without its monotonic clock reading. A LiveSince worked out from
	// time.Now() carries one, different every hour, and time.Equal
	// compares those when both sides have one: the same catalog would
	// read as a new one on every stale event. What comes back from
	// Postgres has none, so this is also what a restart would see.
	e.LiveSince = e.LiveSince.Round(0)
	switch e.Kind {
	case notify.Started:
		j := s.job(e.Job)
		clearRunning(j)
		j.State, j.PassBegan = stRunning, at
		running(j, e)
		if e.Job == notify.JobImport {
			s.catalogFacts(e)
		}

	case notify.Progress:
		j := s.job(e.Job)
		j.State = stRunning
		if j.PassBegan.IsZero() {
			j.PassBegan = at
		}
		running(j, e)

	case notify.Finished:
		j := s.job(e.Job)
		clearRunning(j)
		j.State = stIdle
		j.LastAt, j.LastDone, j.LastNone, j.LastErrors = at, e.Done, e.None, e.Errors
		rec := s.recover(e.Job, j, w)
		if e.Took >= longPass {
			fixed := ""
			if rec.announced {
				fixed = w.fixedSentence(rec.since)
			}
			fx.parts = append(fx.parts, w.longPass(e.Job, e, fixed))
		} else if rec.announced {
			fx.parts = append(fx.parts, w.workingAgain(e.Job, rec.since, s.LiveSince, false))
		}
		fx.parts = append(fx.parts, rec.parts...)

	case notify.Checked:
		if e.Job == notify.JobDatabase {
			fx.parts = append(fx.parts, s.closeDatabase(w, false)...)
			break
		}
		j := s.job(e.Job)
		if e.Job == notify.JobImport {
			s.catalogFacts(e)
			if !e.NextTry.IsZero() {
				s.NextCheck = e.NextTry
			}
			fx.force = true
		}
		if e.Job == notify.JobGeoIP {
			// The same build found again: nothing to say, but the line's
			// "checked" time moves, which is how it shows the check is
			// alive.
			j.LastAt = at
			if !e.LiveSince.IsZero() {
				j.Built = e.LiveSince
			}
			if j.State == stOff {
				// The check runs in this process whatever the last one
				// ran, and may report before this process's takeover,
				// which then keeps it on.
				j.State = stStarting
			}
		}
		if j.State == stFailing && j.Cause == notify.AllFailed && at.Before(j.NextTry) {
			// Nothing that failed has been asked again yet. The OMDb
			// backfill holds a failed lookup back for a day, so the
			// passes in between find nothing to do, and a pass that
			// asked nobody says nothing about whether they answer.
			break
		}
		rec := s.recover(e.Job, j, w)
		if rec.announced {
			fx.parts = append(fx.parts, w.workingAgain(e.Job, rec.since, s.LiveSince, true))
		}
		fx.parts = append(fx.parts, rec.parts...)
		switch j.State {
		case stFailing, stInterrupted, stStarting, stRunning, stSkipped, stWaiting, "":
			clearRunning(j)
			j.State = stIdle
		case stPaused:
			if !at.Before(j.NextTry) {
				j.State = stIdle
			}
		}

	case notify.Published:
		j := s.job(notify.JobImport)
		clearRunning(j)
		j.State = stIdle
		s.Films = e.Films
		s.LiveSince = e.LiveSince
		if s.LiveSince.IsZero() {
			s.LiveSince = at
		}
		if !e.NextTry.IsZero() {
			s.NextCheck = e.NextTry
		}
		s.Stale = staleMark{}
		rec := s.recover(notify.JobImport, j, w)
		fixed := time.Time{}
		if rec.announced {
			fixed = rec.since
		}
		fx.parts = append(fx.parts, w.published(e, fixed))
		fx.force = true

	case notify.Downloaded:
		j := s.job(e.Job)
		clearRunning(j)
		j.State, j.LastAt, j.Built = stIdle, at, e.LiveSince
		rec := s.recover(e.Job, j, w)
		fixed := ""
		if rec.announced {
			fixed = w.fixedSentence(rec.since)
		}
		fx.parts = append(fx.parts, w.downloaded(e, fixed))
		fx.parts = append(fx.parts, rec.parts...)

	case notify.Skipped:
		j := s.job(notify.JobImport)
		clearRunning(j)
		j.State, j.NextTry = stSkipped, e.NextTry
		s.catalogFacts(e)
		if !e.NextTry.IsZero() {
			s.NextCheck = e.NextTry
		}
		fx.force = true

	case notify.Paused:
		j := s.job(e.Job)
		clearRunning(j)
		j.State, j.NextTry = stPaused, e.NextTry
		j.LastAt, j.LastDone, j.LastErrors = at, e.Done, e.Errors

	case notify.Stopped:
		for _, j := range s.Jobs {
			if j.State == stRunning {
				j.State = stInterrupted
			}
		}

	case notify.Failed:
		if e.Job == notify.JobDatabase {
			if s.Alerts[alertDatabase] == nil {
				since := e.Since
				if since.IsZero() {
					since = at
				}
				s.Alerts[alertDatabase] = &alert{Key: alertDatabase, Since: since, Announced: at, Severity: sevRed, Detail: e.Detail}
				fx.parts = append(fx.parts, w.databaseDown(since, e.Detail))
			}
			fx.force = true
			break
		}
		j := s.job(e.Job)
		clearRunning(j)
		j.State = stFailing
		j.Cause, j.Provider, j.Status, j.Integrity, j.Detail = e.Cause, e.Provider, e.Status, e.Integrity, e.Detail
		j.Lookups = e.Errors
		j.NextTry = e.NextTry
		if e.Job == notify.JobGeoIP {
			// Whether the process that failed still has a build to place
			// readers with, which is what its messages say about them.
			j.Built = e.LiveSince
		}
		if j.Fails == 0 {
			j.FailSince = at
		}
		j.Fails++
		if e.Job == notify.JobImport {
			s.catalogFacts(e)
			if !e.NextTry.IsZero() {
				s.NextCheck = e.NextTry
			}
			fx.force = true
		}
		fx.parts = append(fx.parts, s.announce(e.Job, j, at, w)...)

	case notify.Stale:
		if e.LiveSince.IsZero() {
			fx.dirty = false
			break
		}
		s.LiveSince = e.LiveSince
		if !s.Stale.LiveSince.Equal(e.LiveSince) {
			s.Stale = staleMark{LiveSince: e.LiveSince}
		}
		level := 0
		switch age := at.Sub(e.LiveSince); {
		case age > staleRed:
			level = 2
		case age > staleWarn:
			level = 1
		}
		if level > s.Stale.Level {
			s.Stale.Level = level
			fx.parts = append(fx.parts, w.stale(level, e.LiveSince, s.Jobs[notify.JobImport]))
		}

	case notify.TookOver:
		s.RunningSince = at
		s.shutting = false
		on := map[string]bool{}
		for _, id := range e.Jobs {
			on[id] = true
		}
		for _, in := range jobList {
			j := s.job(in.id)
			switch {
			case !on[in.id]:
				s.leave(j)
				*j = job{State: stOff, LastAt: j.LastAt, LastDone: j.LastDone, LastNone: j.LastNone, LastErrors: j.LastErrors}
			case j.State == stRunning:
				j.State = stInterrupted
			case j.State == stOff || j.State == "":
				j.State = stStarting
			}
		}
		fx.parts = append(fx.parts, s.closeDatabase(w, true)...)
		fx.force = true

	default:
		fx.dirty = false
	}
	fx.parts = append(fx.parts, s.reminders(w)...)
	return fx
}

// catalogFacts keeps what an import event knows about the live catalog.
func (s *state) catalogFacts(e notify.Event) {
	if !e.LiveSince.IsZero() {
		s.LiveSince = e.LiveSince
	}
	if e.Films > 0 {
		s.Films = e.Films
	}
}

func running(j *job, e notify.Event) {
	j.Step, j.File, j.Files, j.Noun = e.Step, e.File, e.Files, e.Noun
	j.Share, j.ETA, j.Bytes = e.Share, e.ETA, e.Bytes
	if e.Total > 0 {
		j.Total = e.Total
	}
}

func clearRunning(j *job) {
	j.Step, j.File, j.Files, j.Noun = 0, 0, 0, ""
	j.Share, j.ETA, j.Total, j.Bytes, j.PassBegan = 0, time.Time{}, 0, 0, time.Time{}
}

// severity is how a failing job's line and alert are marked: red when
// only a person can fix it, amber when it is expected to fix itself.
func severity(j *job) int {
	if j.Cause == notify.KeyRejected || (j.Cause == notify.Unknown && j.Fails >= 2) {
		return sevRed
	}
	return sevWarn
}

// alertKey groups failures that are one problem.
func alertKey(id string, j *job) string {
	if j.Cause == notify.KeyRejected {
		return "key:" + strings.ToLower(provider(j.Provider))
	}
	return "job:" + id
}

// announce is the rule for when a failure streak makes a sound: at once
// for a refused key, at the second failure for an error of no known
// kind, and otherwise once it has failed twice across at least
// announceAfter. Each streak is announced once.
func (s *state) announce(id string, j *job, now time.Time, w writer) []part {
	if j.Cause == notify.DatabaseDown && s.Alerts[alertDatabase] != nil {
		// The database alert already said it, louder and once.
		return nil
	}
	key := alertKey(id, j)
	if j.Alert != "" {
		if j.Alert == key || !strings.HasPrefix(key, "key:") {
			return nil
		}
		// Escalating to a refused key: that is a new problem, and the
		// message about it supersedes the one already sent.
		s.leave(j)
	}
	var due bool
	switch j.Cause {
	case notify.KeyRejected:
		due = true
	case notify.Unknown:
		due = j.Fails >= 2
	default:
		due = j.Fails >= 2 && now.Sub(j.FailSince) >= announceAfter
	}
	if !due {
		return nil
	}
	j.Alert = key
	if s.Alerts[key] != nil {
		// Another job already said this; one problem, one sound.
		return nil
	}
	a := &alert{Key: key, Since: j.FailSince, Announced: now, Severity: severity(j), Detail: j.Detail}
	if j.Cause == notify.KeyRejected {
		a.Provider = provider(j.Provider)
	}
	s.Alerts[key] = a
	switch {
	case j.Cause == notify.KeyRejected:
		return []part{w.keyRejected(a.Provider, j)}
	case id == notify.JobImport:
		return []part{w.importFailing(j, s.LiveSince)}
	default:
		return []part{w.jobFailing(id, j)}
	}
}

// recovery is what ending a streak has to say.
type recovery struct {
	// announced is a job alert that was said and is now over; since
	// is when its streak began.
	announced bool
	since     time.Time
	// parts is anything said separately, such as a shared key alert
	// closing once the last job on it has stopped failing.
	parts []part
}

// recover ends j's failure streak.
func (s *state) recover(id string, j *job, w writer) recovery {
	if j.Fails == 0 && j.Alert == "" {
		return recovery{}
	}
	r := recovery{since: j.FailSince}
	key := j.Alert
	endStreak(j)
	if key == "" || s.failingOn(key) > 0 {
		return r
	}
	a := s.Alerts[key]
	delete(s.Alerts, key)
	if a == nil {
		return r
	}
	if strings.HasPrefix(key, "key:") {
		r.parts = append(r.parts, w.keyBack(a.Provider, a.Since))
		return r
	}
	r.announced = true
	return r
}

func endStreak(j *job) {
	j.Cause, j.Provider, j.Status, j.Integrity, j.Lookups, j.Detail = notify.CauseNone, "", 0, 0, 0, ""
	j.Fails, j.FailSince, j.Alert = 0, time.Time{}, ""
}

// leave takes j off its alert, closing the alert without a word when
// nobody else is on it. Used when a job is turned off, or when its
// problem turns into a different one that is announced in its place.
func (s *state) leave(j *job) {
	key := j.Alert
	j.Alert = ""
	if key != "" && s.failingOn(key) == 0 {
		delete(s.Alerts, key)
	}
}

func (s *state) failingOn(key string) int {
	n := 0
	for _, j := range s.Jobs {
		if j.Alert == key && j.Fails > 0 {
			n++
		}
	}
	return n
}

// closeDatabase ends the database alert, if one is open.
func (s *state) closeDatabase(w writer, jobsHere bool) []part {
	a := s.Alerts[alertDatabase]
	if a == nil {
		return nil
	}
	delete(s.Alerts, alertDatabase)
	if a.Announced.IsZero() {
		return nil
	}
	return []part{w.databaseBack(a.Since, jobsHere)}
}

// reminders says again what has been open for a day since it was last
// said. The import has none of its own: its stale alerts, at a day and
// a half and at three days, are the reminder.
func (s *state) reminders(w writer) []part {
	var out []part
	keys := make([]string, 0, len(s.Alerts))
	for key := range s.Alerts {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		a := s.Alerts[key]
		if a.Announced.IsZero() || key == "job:"+notify.JobImport {
			continue
		}
		last := a.Announced
		if a.Reminded.After(last) {
			last = a.Reminded
		}
		if w.now.Sub(last) < remindAfter {
			continue
		}
		id, j := s.onAlert(key)
		p := w.reminder(a, id, j)
		if p.body == "" {
			continue
		}
		a.Reminded = w.now
		out = append(out, p)
	}
	return out
}

// onAlert is one job on an alert, for the words of its reminder.
func (s *state) onAlert(key string) (string, *job) {
	for _, in := range jobList {
		if j := s.Jobs[in.id]; j != nil && j.Alert == key && j.Fails > 0 {
			return in.id, j
		}
	}
	return "", nil
}
