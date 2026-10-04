package catalog

import (
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5/pgconn"

	"cinedikt/internal/geoip"
	"cinedikt/internal/notify"
)

// The errors a job can fail with that a person can do something about,
// or at least stop worrying about. Each keeps the text it always had,
// so the log reads the same; what they add is a type, so the notifier
// is told why in a word instead of being handed a string to guess at.

// IMDbError is the dataset host failing a request. Status is zero when
// no answer came back at all: a refused connection, a stalled body.
type IMDbError struct {
	Method string
	File   File
	Status int
	Err    error
}

func (e *IMDbError) Error() string {
	switch {
	case e.Status != 0:
		return fmt.Sprintf("catalog: %s %s: HTTP %d", e.Method, e.File, e.Status)
	case e.Method != "":
		return fmt.Sprintf("catalog: %s %s: %v", e.Method, e.File, e.Err)
	default:
		return fmt.Sprintf("catalog: %v", e.Err)
	}
}

func (e *IMDbError) Unwrap() error { return e.Err }

// IntegrityError is a load that did not hold together, so it was not
// published. Share is the fraction of credits that named a stored
// title; zero when the load was rejected before that was worked out.
type IntegrityError struct {
	Share, Want float64
	Reason      string
}

func (e *IntegrityError) Error() string {
	if e.Reason != "" {
		return "catalog: " + e.Reason
	}
	return fmt.Sprintf("catalog: only %.4f of credits name a stored title, want %.2f "+
		"(the files are probably from different generations)", e.Share, e.Want)
}

// KeyError is a provider refusing the key. Only a new key fixes it.
type KeyError struct {
	Provider string
	Err      error
}

func (e *KeyError) Error() string {
	return fmt.Sprintf("catalog: %s refused the key: %v", e.Provider, e.Err)
}

func (e *KeyError) Unwrap() error { return e.Err }

// LookupsFailedError is a pass in which every lookup failed and none
// came back with an answer: not one bad title, but the service, or the
// way there, being down.
type LookupsFailedError struct {
	Provider string
	Count    int64
	Last     error
	// RetryAt is when the failed lookups will next be asked about, if
	// that is later than the job's next pass: the OMDb backfill holds a
	// failed title back for a day. Zero is the next pass.
	RetryAt time.Time
}

func (e *LookupsFailedError) Error() string {
	return fmt.Sprintf("catalog: all %d lookups to %s failed; the last: %v", e.Count, e.Provider, e.Last)
}

func (e *LookupsFailedError) Unwrap() error { return e.Last }

// failedLookups is how many lookups a pass has to lose, with nothing
// coming back, before the pass is a failure rather than a bad batch.
const failedLookups = 50

// classify is why err happened, as one of the causes the notifier
// knows. Anything it does not recognise is Unknown, which is itself
// worth knowing: an error of no known kind may be a bug.
func classify(err error) (cause notify.Cause, provider string, status int, integrity float64) {
	var key *KeyError
	var lookups *LookupsFailedError
	var imdb *IMDbError
	var connect *pgconn.ConnectError
	var pg *pgconn.PgError
	var check *IntegrityError
	var download *geoip.DownloadError
	switch {
	case errors.As(err, &key):
		return notify.KeyRejected, key.Provider, 0, 0
	case errors.Is(err, geoip.ErrKey):
		return notify.KeyRejected, "MaxMind", 0, 0
	case errors.As(err, &download):
		// The same line as IMDb's: no answer, a rate limit or a server
		// error is MaxMind's to fix and fixes itself; any other refusal
		// is not one this knows.
		if download.Status == 0 || download.Status == 429 || download.Status >= 500 {
			return notify.ProviderDown, "MaxMind", download.Status, 0
		}
		return notify.Unknown, "MaxMind", download.Status, 0
	case errors.As(err, &lookups):
		return notify.AllFailed, lookups.Provider, 0, 0
	case errors.As(err, &imdb):
		if imdb.Status == 0 || imdb.Status == 429 || imdb.Status >= 500 {
			return notify.IMDbDown, "IMDb", imdb.Status, 0
		}
		return notify.Unknown, "IMDb", imdb.Status, 0
	case errors.As(err, &connect):
		return notify.DatabaseDown, "", 0, 0
	case errors.As(err, &pg):
		switch {
		case len(pg.Code) >= 2 && pg.Code[:2] == "08":
			return notify.DatabaseDown, "", 0, 0
		case pg.Code == "57014", pg.Code == "55P03":
			return notify.DatabaseBusy, "", 0, 0
		}
		return notify.Unknown, "", 0, 0
	case errors.As(err, &check):
		return notify.FilesMismatch, "IMDb", 0, check.Share
	}
	return notify.Unknown, "", 0, 0
}

// failure is the event for a job that failed with err. next is when it
// will try again; liveSince, for the import, is when the catalog still
// being served was built, and for the GeoIP check, when the build still
// in use was.
func failure(job string, err error, next, liveSince time.Time) notify.Event {
	cause, provider, status, integrity := classify(err)
	e := notify.Event{
		Job:       job,
		Kind:      notify.Failed,
		Cause:     cause,
		Provider:  provider,
		Status:    status,
		Integrity: integrity,
		Detail:    err.Error(),
		NextTry:   next,
		LiveSince: liveSince,
	}
	var lookups *LookupsFailedError
	if errors.As(err, &lookups) {
		e.Errors = lookups.Count
		// The next pass would find nothing to retry, so it is not when
		// the job next learns anything.
		if lookups.RetryAt.After(e.NextTry) {
			e.NextTry = lookups.RetryAt
		}
	}
	return e
}
