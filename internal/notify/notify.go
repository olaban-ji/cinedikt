// Package notify is the event a catalog job raises when something about
// it is worth telling a person. It carries facts, never sentences: the
// transport decides every word, and whether a phone should make a sound
// at all. It has no transport of its own. A process with nowhere to send
// an event uses a nil Sink, and the jobs do not change what they do.
package notify

import (
	"context"
	"time"
)

// Kind is what happened.
type Kind string

const (
	// Started is the beginning of a pass or an import.
	Started Kind = "started"
	// Progress is how far a running pass has got.
	Progress Kind = "progress"
	// Finished is a pass that got to the end of its work.
	Finished Kind = "finished"
	// Checked is a look that found nothing to do, or a pass that ended
	// without anything going wrong. It is what clears a failure.
	Checked Kind = "checked"
	// Published is a new catalog going live.
	Published Kind = "published"
	// Skipped is an import thrown away for a reason that is not a
	// failure, such as a file moving while the set was fetched.
	Skipped Kind = "skipped"
	// Paused is a pass stopped on purpose until a known time.
	Paused Kind = "paused"
	// Stopped is the process letting go of the jobs.
	Stopped Kind = "stopped"
	// Failed is a pass or an import that went wrong.
	Failed Kind = "failed"
	// Stale is the live catalog being older than it should be.
	Stale Kind = "stale"
	// TookOver is this process becoming the one that runs the jobs.
	TookOver Kind = "took_over"
)

// The jobs the catalog runs, and the two things that are not jobs but
// still have something to say: the process as a whole, and the
// connection the lease lives on. The names are stable, so a transport
// can lay them out in the same order every time and keep what it said
// about each of them across a restart.
const (
	JobImport      = "import"
	JobPosters     = "posters"
	JobTMDbPosters = "tmdb-posters"
	JobTMDbIDs     = "tmdb-ids"
	JobColours     = "colours"
	JobSystem      = "system"
	JobDatabase    = "database"
)

// Cause is why a job failed, paused or skipped, as one of the few kinds
// a person can do something about. The raw error still travels in
// Event.Detail for whoever wants the particulars.
type Cause string

const (
	CauseNone     Cause = ""
	DatabaseDown  Cause = "database_down"
	DatabaseBusy  Cause = "database_busy"
	IMDbDown      Cause = "imdb_down"
	FilesMismatch Cause = "files_mismatch"
	FileMoved     Cause = "file_moved"
	DailyLimit    Cause = "daily_limit"
	KeyRejected   Cause = "key_rejected"
	AllFailed     Cause = "all_failed"
	// Locked is an import that did not run because another one held
	// the import lock. Only a Checked event carries it.
	Locked  Cause = "locked"
	Unknown Cause = "unknown"
)

// The four steps of an import, in the order they happen.
const (
	PhaseDownload = "downloading"
	PhaseLoad     = "loading"
	PhaseIndexes  = "indexes"
	PhaseLive     = "live"
)

// Event is one fact about one job. Only the fields that belong to its
// Kind are set; the rest are zero.
type Event struct {
	Job  string
	Kind Kind
	// At is when it happened. The catalog stamps it when it is zero.
	At time.Time

	// The work of one pass. Done is what the pass exists to produce:
	// posters saved, pictures found, films matched or coloured. None is
	// what came back with no answer to give, and Errors is what failed.
	Total, Done, None, Errors int64

	// Progress. Share is capped at 0.99 so a running pass never reads
	// as finished; ETA stays zero until there is enough behind it to
	// trust. Bytes is set instead of Share when a download's length is
	// not known.
	Share float64
	ETA   time.Time
	Bytes int64

	// An import's place: step 1 to 4 of 4, and for the download, file
	// i of n and what that file holds (films, people, credits,
	// directors or ratings).
	Step, Steps int
	Phase       string
	File, Files int
	Noun        string

	// Published. Took is also set on Finished.
	Films, People, PrevFilms int64
	PrevAt                   time.Time
	Took                     time.Duration

	// Failed. Detail is the raw error text; a transport redacts it
	// before anyone sees it.
	Cause     Cause
	Provider  string
	Status    int
	Integrity float64
	Detail    string

	// NextTry is when the job will look again. LiveSince is when the
	// catalog that is live now was built. Since is the first failed
	// connection, for JobDatabase.
	NextTry, LiveSince, Since time.Time

	// Jobs is, on TookOver, every job this process runs. Anything left
	// out is off here.
	Jobs []string
}

// Sink receives events. Note must return quickly and must not fail the
// caller: a job's work does not depend on anyone hearing about it.
type Sink interface {
	Note(Event)
}

// Memory is where a sink keeps what it has already said, so a restart
// does not say it again. The catalog's Store is one.
type Memory interface {
	LoadNotifyState(ctx context.Context) ([]byte, error)
	SaveNotifyState(ctx context.Context, state []byte) error
}

// Attacher is a sink that can be given a Memory. Only the process that
// runs the jobs attaches, which is what keeps two containers from both
// editing the same message during a deploy.
type Attacher interface {
	Attach(Memory)
}

// Detacher is a sink that can be told this process has lost the jobs
// without shutting down: the lease connection dropped, and another
// process may take them, and with them the board and the memory.
type Detacher interface {
	Detach()
}

// Closer is a sink with something left to send when the process ends.
type Closer interface {
	Close(ctx context.Context)
}

// Attach hands m to s, if s wants one. A nil sink, or one that keeps no
// memory, is left as it is.
func Attach(s Sink, m Memory) {
	if s == nil {
		return
	}
	if a, ok := s.(Attacher); ok {
		a.Attach(m)
	}
}

// Detach tells s this process no longer runs the jobs, if s cares.
func Detach(s Sink) {
	if s == nil {
		return
	}
	if d, ok := s.(Detacher); ok {
		d.Detach()
	}
}

// Close gives s up to timeout to say what it still has to say. A nil
// sink, or one with nothing to flush, returns at once.
func Close(s Sink, timeout time.Duration) {
	if s == nil {
		return
	}
	c, ok := s.(Closer)
	if !ok {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	c.Close(ctx)
}
