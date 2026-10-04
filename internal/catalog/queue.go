package catalog

// The queue: River, for work that happens at a moment rather than in a
// loop. The jobs above sweep a queue table for as long as the process
// runs; where to watch needs something else, an answer asked again at
// the moment one of its streaming options leaves, and River schedules
// a job for a time and runs it once. Where to watch's periodic work, the
// daily read of the changes feed among it, runs on River too.
//
// River runs in every API process that has a Streaming Availability
// key, and that is safe with two of them, which is what a deploy's
// overlap is:
//
//   - A job is claimed with FOR UPDATE SKIP LOCKED, so however many
//     processes are fetching, each job runs in one of them.
//   - A refresh is unique by its arguments among the jobs still to run or
//     running, so a burst of readers, or both processes, asking for the
//     same refresh insert it once.
//   - The periodic jobs, the GeoIP check, the countries refresh and the
//     look for streaming changes, are inserted only by River's elected
//     leader, and are unique among the jobs still to run, so a new
//     leader's run-on-start cannot stack a second check on one already
//     queued or running, and two processes never read the same changes.

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/riverqueue/river"
	"github.com/riverqueue/river/riverdriver/riverpgxv5"
	"github.com/riverqueue/river/rivermigrate"
	"github.com/riverqueue/river/rivertype"

	"cinedikt/internal/geoip"
	"cinedikt/internal/notify"
	"cinedikt/internal/streaming"
)

// QueueSchema is where River's tables live: a schema of their own. The
// daily swap renames catalog schemas and drops the retired one, and
// never names this; and a database whose public schema has been dropped,
// which is a real state, still has somewhere to put them.
const QueueSchema = "river"

// QueueMaxConns is the size of River's own pool. River holds one
// connection for as long as it runs, to LISTEN for new jobs, and polls
// for work, elects a leader and records results on the others. On the
// pool that serves readers that would be a reader's connection taken for
// good, and on the jobs' pool it would queue behind a bulk COPY; a pool
// of its own keeps both out of its way. The jobs' own writes go through
// the store they are given, not this pool.
const QueueMaxConns = 4

// QueueWorkers is how many jobs one process runs at once. A refresh is
// one API call and one write, and the API's limiter paces them anyway.
const QueueWorkers = 4

// queueSoftStop is how long a stop waits for running jobs before it
// cancels them. Inside the ten seconds Railway gives a draining
// container, with room for the cancelled jobs to return.
const queueSoftStop = 5 * time.Second

// queueMigrateKey is the advisory lock River's migrations run under.
// River takes none of its own, and two containers starting together
// would otherwise both try to create the same tables.
const queueMigrateKey int64 = 0x6369_6e72 // "cinr"

// Periods of the periodic jobs. MaxMind publishes GeoLite2 twice a week,
// so checking twice a day finds a new build within half a day for the
// price of two HEAD requests. The country list changes far more rarely
// than daily; the job looks every day and asks only once the list is a
// week old. The changes job looks every hour, a read of the database,
// and reads a country's changes once its last run is a day old
// (ChangesEvery): a deploy restarts every period, and a daily period
// restarted by each deploy could go days without running.
const (
	GeoIPCheckEvery     = 12 * time.Hour
	CountriesCheckEvery = 24 * time.Hour
	ChangesCheckEvery   = time.Hour
)

// Budgets for one job. A refresh is one API call and one write; a GeoIP
// check may download a few megabytes; a read of the changes is up to
// ChangesMaxPages requests for every country somebody has kept answers
// for, paced by the API's limiter. A periodic job that keeps failing
// gives up after a few tries rather than River's default twenty-five,
// because while it is still retrying, the next period's run is held back
// as its duplicate.
const (
	queueRefreshTimeout  = 30 * time.Second
	queueRefreshAttempts = 5
	queueGeoIPTimeout    = 3 * time.Minute
	queueChangesTimeout  = 30 * time.Minute
	queuePeriodicTries   = 3
)

// pendingStates are the job states uniqueness holds across: every state
// a job is in before it has finished. A finished job stops counting, so
// the next stale answer can ask again.
var pendingStates = []rivertype.JobState{
	rivertype.JobStateAvailable,
	rivertype.JobStatePending,
	rivertype.JobStateRetryable,
	rivertype.JobStateRunning,
	rivertype.JobStateScheduled,
}

// QueueConfig is what OpenQueue runs.
type QueueConfig struct {
	DatabaseURL string
	Logger      *slog.Logger
	// WhereToWatch is the service whose answers are refreshed and kept
	// right from the changes feed, and whose country list is kept up to
	// date. Nil runs none of it.
	WhereToWatch *WhereToWatch
	// GeoIP is the updater the twice-daily check runs. Nil runs no check:
	// there is no license key to download with.
	GeoIP *geoip.Updater
	// Notify is told how each GeoIP check ended. Nil leaves that in the
	// log.
	Notify notify.Sink
}

// Queue is River, with its own pool.
type Queue struct {
	pool     *pgxpool.Pool
	client   *river.Client[pgx.Tx]
	stopOnce sync.Once
	// jobs is what Jobs returns.
	jobs []string
}

// OpenQueue connects, brings River's tables up to date, and builds the
// client with every job the config asks for. Nothing runs until Start.
func OpenQueue(ctx context.Context, cfg QueueConfig) (*Queue, error) {
	if cfg.DatabaseURL == "" {
		return nil, errors.New("catalog: the queue needs a DatabaseURL")
	}
	poolCfg, err := pgxpool.ParseConfig(cfg.DatabaseURL)
	if err != nil {
		return nil, fmt.Errorf("catalog: parse database url: %w", err)
	}
	poolCfg.MaxConns = QueueMaxConns
	pool, err := pgxpool.NewWithConfig(ctx, poolCfg)
	if err != nil {
		return nil, fmt.Errorf("catalog: connect the queue: %w", err)
	}
	logger := cfg.Logger
	if logger == nil {
		logger = slog.Default()
	}
	quiet := slog.New(atLeast{logger.Handler(), slog.LevelWarn})
	if err := migrateQueue(ctx, pool, quiet); err != nil {
		pool.Close()
		return nil, err
	}

	workers := river.NewWorkers()
	var periodic []*river.PeriodicJob
	var jobs []string
	if w := cfg.WhereToWatch; w != nil {
		river.AddWorker(workers, &watchRefreshWorker{w: w})
		river.AddWorker(workers, &countriesWorker{w: w})
		river.AddWorker(workers, &changesWorker{w: w})
		periodic = append(periodic,
			river.NewPeriodicJob(river.PeriodicInterval(CountriesCheckEvery),
				func() (river.JobArgs, *river.InsertOpts) { return StreamingCountriesArgs{}, nil },
				&river.PeriodicJobOpts{ID: StreamingCountriesArgs{}.Kind(), RunOnStart: true}),
			river.NewPeriodicJob(river.PeriodicInterval(ChangesCheckEvery),
				func() (river.JobArgs, *river.InsertOpts) { return StreamingChangesArgs{}, nil },
				&river.PeriodicJobOpts{ID: StreamingChangesArgs{}.Kind(), RunOnStart: true}))
	}
	if cfg.GeoIP != nil {
		river.AddWorker(workers, &geoIPWorker{u: cfg.GeoIP, notify: cfg.Notify})
		periodic = append(periodic, river.NewPeriodicJob(river.PeriodicInterval(GeoIPCheckEvery),
			func() (river.JobArgs, *river.InsertOpts) { return GeoIPCheckArgs{}, nil },
			&river.PeriodicJobOpts{ID: GeoIPCheckArgs{}.Kind(), RunOnStart: true}))
		jobs = append(jobs, notify.JobGeoIP)
	}
	client, err := river.NewClient(riverpgxv5.New(pool), &river.Config{
		Schema:          QueueSchema,
		Queues:          map[string]river.QueueConfig{river.QueueDefault: {MaxWorkers: QueueWorkers}},
		Workers:         workers,
		PeriodicJobs:    periodic,
		Logger:          quiet,
		ErrorHandler:    queuePanics{logger},
		SoftStopTimeout: queueSoftStop,
	})
	if err != nil {
		pool.Close()
		return nil, fmt.Errorf("catalog: build the queue: %w", err)
	}
	q := &Queue{pool: pool, client: client, jobs: jobs}
	if cfg.WhereToWatch != nil {
		cfg.WhereToWatch.queue = q
	}
	return q, nil
}

// Jobs is the notifier's name for each job the queue runs that reports to
// it: JobGeoIP, when there is a MaxMind key. A nil queue runs none. The
// runner names them among its own when it takes the jobs, since the board
// lists what one process runs, and a job it leaves out reads as off.
func (q *Queue) Jobs() []string {
	if q == nil {
		return nil
	}
	return q.jobs
}

// Start runs the queue until Stop. It is not tied to ctx's cancellation:
// a signal would otherwise cancel running jobs at once, and Stop is what
// gives them their few seconds first.
func (q *Queue) Start(ctx context.Context) error {
	return q.client.Start(context.WithoutCancel(ctx))
}

// Stop stops fetching, lets running jobs finish for queueSoftStop and
// then cancels them, and closes the pool. A job cut short is retried by
// whichever process runs the queue next. Calling it again does nothing.
func (q *Queue) Stop(ctx context.Context) {
	q.stopOnce.Do(func() {
		_ = q.client.Stop(ctx)
		q.pool.Close()
	})
}

// migrateQueue brings River's tables up to date, under a lock so two
// processes starting together take turns. The lock is held on a
// connection of its own, and released when that connection is.
func migrateQueue(ctx context.Context, pool *pgxpool.Pool, logger *slog.Logger) error {
	conn, err := pool.Acquire(ctx)
	if err != nil {
		return fmt.Errorf("catalog: connect to migrate the queue: %w", err)
	}
	defer conn.Release()
	if _, err := conn.Exec(ctx, `SELECT pg_advisory_lock($1)`, queueMigrateKey); err != nil {
		return fmt.Errorf("catalog: lock the queue's migrations: %w", err)
	}
	defer func() {
		_, _ = conn.Exec(context.WithoutCancel(ctx), `SELECT pg_advisory_unlock($1)`, queueMigrateKey)
	}()
	if _, err := conn.Exec(ctx, `CREATE SCHEMA IF NOT EXISTS `+QueueSchema); err != nil {
		return fmt.Errorf("catalog: create %s: %w", QueueSchema, err)
	}
	migrator, err := rivermigrate.New(riverpgxv5.New(pool), &rivermigrate.Config{Schema: QueueSchema, Logger: logger})
	if err != nil {
		return fmt.Errorf("catalog: the queue's migrator: %w", err)
	}
	if _, err := migrator.Migrate(ctx, rivermigrate.DirectionUp, nil); err != nil {
		return fmt.Errorf("catalog: migrate the queue: %w", err)
	}
	return nil
}

// logJob says that a job failed. River reports a failed job at info and
// a cancelled one at debug, below what the queue's logger passes, so
// without this a refused key would fail every check without a word. A
// refused key is an error, since only a person can fix it; any other
// failure is a warning, and River tries the job again until its attempts
// run out. A job cut short by a stop has not failed, and the next process
// to run the queue runs it.
func logJob(ctx context.Context, l *slog.Logger, msg string, err error, attrs ...any) {
	if err == nil || l == nil || errors.Is(ctx.Err(), context.Canceled) {
		return
	}
	level := slog.LevelWarn
	if errors.Is(err, geoip.ErrKey) || errors.Is(err, streaming.ErrKey) {
		level = slog.LevelError
	}
	l.Log(ctx, level, msg, append(attrs, "err", err)...)
}

// queuePanics logs a job that panicked, which River reports only at info.
// A job's error is left to the job, which logs it itself: River skips
// this handler for a cancelled job, and a refused key cancels one.
type queuePanics struct{ logger *slog.Logger }

func (queuePanics) HandleError(context.Context, *rivertype.JobRow, error) *river.ErrorHandlerResult {
	return nil
}

func (h queuePanics) HandlePanic(ctx context.Context, job *rivertype.JobRow, panicVal any, trace string) *river.ErrorHandlerResult {
	h.logger.ErrorContext(ctx, "a queued job panicked", "kind", job.Kind, "job", job.ID, "attempt", job.Attempt,
		"panic", fmt.Sprint(panicVal), "trace", trace)
	return nil
}

// atLeast passes on only what is at least min. River reports every job
// it runs at info, which would be most of what this process logs; its
// warnings and errors are what someone reading the log needs. A failed
// job, which River also reports at info, logs itself (logJob).
type atLeast struct {
	slog.Handler
	min slog.Level
}

func (h atLeast) Enabled(ctx context.Context, l slog.Level) bool {
	return l >= h.min && h.Handler.Enabled(ctx, l)
}

func (h atLeast) WithAttrs(attrs []slog.Attr) slog.Handler {
	return atLeast{h.Handler.WithAttrs(attrs), h.min}
}

func (h atLeast) WithGroup(name string) slog.Handler {
	return atLeast{h.Handler.WithGroup(name), h.min}
}
