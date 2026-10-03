package catalog

// GeoLite2 Country, kept in meta.geoip, and the job that keeps it
// current. The download and the checks on it are internal/geoip's; this
// is where the database is stored, so a restart or a second container
// reads it rather than downloading it again, and the River job that asks
// MaxMind twice a day.

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/riverqueue/river"

	"cinedikt/internal/geoip"
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
}

func (w *geoIPWorker) Timeout(*river.Job[GeoIPCheckArgs]) time.Duration { return queueGeoIPTimeout }

func (w *geoIPWorker) Work(ctx context.Context, _ *river.Job[GeoIPCheckArgs]) error {
	_, err := w.u.Check(ctx)
	logJob(ctx, w.u.Logger, "check for a new GeoLite2 build", err)
	if errors.Is(err, geoip.ErrKey) {
		// Asking again with the same key gets the same answer. The next
		// check is twelve hours away, and a fixed key is used then.
		return river.JobCancel(err)
	}
	return err
}
