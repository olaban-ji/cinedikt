package catalog

// GeoLite2 Country, kept in meta.geoip, and the job that keeps it
// current. The download and the checks on it are internal/geoip's; this
// is where the database is stored, so a restart or a second container
// reads it rather than downloading it again, and the River job that asks
// MaxMind twice a day and tells the notifier how each check ended.

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/riverqueue/river"

	"cinedikt/internal/geoip"
	"cinedikt/internal/notify"
)

// GeoIP is the kept database and MaxMind's stamp for it, or "" and nil
// when none has been downloaded. With GeoIPStamp and KeepGeoIP it makes
// the Store a geoip.Store.
func (s *Store) GeoIP(ctx context.Context) (string, []byte, error) {
	var stamp string
	var mmdb []byte
	err := s.pool.QueryRow(ctx, `SELECT last_modified, mmdb FROM meta.geoip WHERE id = 1`).Scan(&stamp, &mmdb)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", nil, nil
	}
	if err != nil {
		return "", nil, fmt.Errorf("catalog: read the GeoIP database: %w", err)
	}
	return stamp, mmdb, nil
}

// GeoIPStamp is the kept database's stamp alone, or "" for none.
func (s *Store) GeoIPStamp(ctx context.Context) (string, error) {
	var stamp string
	err := s.pool.QueryRow(ctx, `SELECT last_modified FROM meta.geoip WHERE id = 1`).Scan(&stamp)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", nil
	}
	if err != nil {
		return "", fmt.Errorf("catalog: read the GeoIP stamp: %w", err)
	}
	return stamp, nil
}

// KeepGeoIP replaces the kept database.
func (s *Store) KeepGeoIP(ctx context.Context, stamp string, mmdb []byte) error {
	_, err := s.pool.Exec(ctx, `
		INSERT INTO meta.geoip (id, last_modified, mmdb, fetched_at)
		VALUES (1, $1, $2, now())
		ON CONFLICT (id) DO UPDATE
		SET last_modified = EXCLUDED.last_modified,
		    mmdb          = EXCLUDED.mmdb,
		    fetched_at    = EXCLUDED.fetched_at`, stamp, mmdb)
	if err != nil {
		return fmt.Errorf("catalog: keep the GeoIP database: %w", err)
	}
	return nil
}

// GeoIPCheckArgs is the twice-daily look for a new GeoLite2 build.
type GeoIPCheckArgs struct{}

// Kind names the job in River's tables.
func (GeoIPCheckArgs) Kind() string { return "geoip_check" }

// InsertOpts keeps one check queued or running at a time.
func (GeoIPCheckArgs) InsertOpts() river.InsertOpts {
	return river.InsertOpts{MaxAttempts: queuePeriodicTries, UniqueOpts: river.UniqueOpts{ByState: pendingStates}}
}

type geoIPWorker struct {
	river.WorkerDefaults[GeoIPCheckArgs]
	u *geoip.Updater
	// notify hears how each check ended. Nil leaves that in the log.
	notify notify.Sink
}

func (w *geoIPWorker) Timeout(*river.Job[GeoIPCheckArgs]) time.Duration { return queueGeoIPTimeout }

func (w *geoIPWorker) Work(ctx context.Context, job *river.Job[GeoIPCheckArgs]) error {
	res, err := w.u.Check(ctx)
	logJob(ctx, w.u.Logger, "check for a new GeoLite2 build", err)
	w.report(ctx, job, res, err)
	if errors.Is(err, geoip.ErrKey) {
		// Asking again with the same key gets the same answer. The next
		// check is twelve hours away, and a fixed key is used then.
		return river.JobCancel(err)
	}
	return err
}

// report tells the notifier how a check ended: a new build in use, the
// same build found again, or a failure River will not try again. A
// failure River retries in a moment is left to that retry, so a check is
// one failure however many tries it took, and the notifier's rules,
// which count failures across time, read a check every twelve hours as
// they read any other job's passes. A check cut short by a stop says
// nothing; the next process to run the queue checks again. One that
// finished as the stop came still says how it ended: a build it kept is
// already kept, and the next check would find nothing new to say.
//
// A new build is reported only by the check that kept it. A process that
// loads it afterwards, here or in Follow, finds it already kept and has
// no news, so each build is said once however many processes run.
func (w *geoIPWorker) report(ctx context.Context, job *river.Job[GeoIPCheckArgs], res geoip.Result, err error) {
	if w.notify == nil || (err != nil && errors.Is(ctx.Err(), context.Canceled)) {
		return
	}
	switch {
	case err == nil && res.Downloaded:
		report(w.notify, notify.Event{Job: notify.JobGeoIP, Kind: notify.Downloaded,
			LiveSince: buildTime(res.Built), PrevAt: buildTime(res.Replaced), Bytes: int64(res.Bytes)})
	case err == nil:
		report(w.notify, notify.Event{Job: notify.JobGeoIP, Kind: notify.Checked, LiveSince: buildTime(res.Built)})
	case errors.Is(err, geoip.ErrKey) || job.Attempt >= job.MaxAttempts:
		now := time.Now()
		report(w.notify, failure(notify.JobGeoIP, err, nextGeoIPCheck(job, now), buildTime(w.u.Lookup.LastModified())))
	}
}

// nextGeoIPCheck is when the periodic check comes round again: a period
// after this run was queued. River's leader queues each run when it is
// due, and a retry moves a job's scheduled time but not when it was
// created. A new leader checks at once, so this is the latest it can be.
func nextGeoIPCheck(job *river.Job[GeoIPCheckArgs], now time.Time) time.Time {
	next := job.CreatedAt.Add(GeoIPCheckEvery)
	if !next.After(now) {
		next = now.Add(GeoIPCheckEvery)
	}
	return next
}

// buildTime is when MaxMind built a build, from its Last-Modified stamp,
// or zero for no build, or a stamp that is not an HTTP date.
func buildTime(stamp string) time.Time {
	t, err := http.ParseTime(stamp)
	if err != nil {
		return time.Time{}
	}
	return t
}
