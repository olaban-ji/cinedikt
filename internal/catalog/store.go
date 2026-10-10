package catalog

import (
	"context"
	_ "embed"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

//go:embed schema.sql
var schemaSQL string

//go:embed indexes.sql
var indexesSQL string

//go:embed meta.sql
var metaSQL string

//go:embed firstrun.sql
var firstRunSQL string

// eras are the opening screen's eras, each its first and last year.
// firstrun.sql ranks the best known movies of each into the pool the
// cold screen offers one of each from, and the Daily mixes a week's
// answers across them (dailyCandidates), so both place a year by these
// and nothing else. They are not equal spans: the point is a spread of
// eras a reader recognises, and more movies anyone has heard of were
// made recently than in the 1930s.
var eras = [...][2]int{
	{1920, 1959}, {1960, 1979}, {1980, 1994}, {1995, 2004},
	{2005, 2012}, {2013, 2018}, {2019, 2023}, {2024, 2100},
}

// erasSQL is eras as the rows of a VALUES list, "(1920, 1959), (1960,
// 1979), …", for a query to join a year to its era by: AS e (lo, hi).
func erasSQL() string {
	rows := make([]string, len(eras))
	for i, e := range eras {
		rows[i] = fmt.Sprintf("(%d, %d)", e[0], e[1])
	}
	return strings.Join(rows, ", ")
}

// firstRunFor is firstrun.sql for schema, with the eras written in.
func firstRunFor(schema string) string {
	return forSchema(strings.ReplaceAll(firstRunSQL, "{{eras}}", erasSQL()), schema)
}

// The schema a reader reads and the one a load writes. The names are
// swapped at publish; nothing outside this file mentions either.
const (
	Live    = "catalog"
	Staging = "catalog_next"
	retired = "catalog_old"
)

// Store is the catalog's Postgres connection. The importer and the API
// each hold their own, with their own pool size, so a bulk COPY cannot
// take the connections a reader needs.
type Store struct {
	pool *pgxpool.Pool
	// Titles a reader tried to look at and found no picture for, no
	// synopsis, or no trailer, and people on a map with no photo answer,
	// on their way to being written down. See wants.go.
	wants     chan string
	synWants  chan string
	trWants   chan string
	peWants   chan string
	wantsDone chan struct{}
	stop      chan struct{}
	stopOnce  sync.Once
	// deals is what development's Play again deals from, kept between
	// presses (dealCandidates).
	deals keptCandidates
}

// Open connects and makes sure meta exists. meta is created on every
// start because it is the one schema that is never dropped and never
// renamed, and an empty database has to be able to take a first import.
func Open(ctx context.Context, url string, maxConns int32) (*Store, error) {
	cfg, err := poolConfig(url, maxConns, false)
	if err != nil {
		return nil, err
	}
	return open(ctx, cfg)
}

// OpenForJobs is Open for the pool the catalog jobs run on: the import
// and the poster, id and colour passes. Its queries each run on one
// backend, without parallel workers.
//
// Postgres splits a large scan or join across parallel workers, and
// they share their working state through /dev/shm. Railway's Postgres
// container gives /dev/shm 64 MB. The count of titles wanting a poster,
// which joins every title to its poster row on every pass, asked for
// more and failed with "could not resize shared memory segment ... No
// space left on device". A background pass loses nothing by taking a
// little longer, and it leaves that memory to the queries readers are
// waiting on.
func OpenForJobs(ctx context.Context, url string, maxConns int32) (*Store, error) {
	cfg, err := poolConfig(url, maxConns, true)
	if err != nil {
		return nil, err
	}
	return open(ctx, cfg)
}

// poolConfig is the pool Open and OpenForJobs build, before it connects.
func poolConfig(url string, maxConns int32, jobs bool) (*pgxpool.Config, error) {
	cfg, err := pgxpool.ParseConfig(url)
	if err != nil {
		return nil, fmt.Errorf("catalog: parse database url: %w", err)
	}
	if maxConns > 0 {
		cfg.MaxConns = maxConns
	}
	// Every object this package creates or reads is schema-qualified,
	// with one thing that cannot be: the trigram operator class an
	// index is declared with. It lives wherever pg_trgm was installed,
	// so both candidates are on the path. `public` may not exist, and a
	// missing schema on the search path is skipped rather than an
	// error — which is exactly what makes this safe.
	if cfg.ConnConfig.RuntimeParams == nil {
		cfg.ConnConfig.RuntimeParams = map[string]string{}
	}
	cfg.ConnConfig.RuntimeParams["search_path"] = "meta, public"
	if jobs {
		cfg.ConnConfig.RuntimeParams["max_parallel_workers_per_gather"] = "0"
	}
	return cfg, nil
}

func open(ctx context.Context, cfg *pgxpool.Config) (*Store, error) {
	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		return nil, fmt.Errorf("catalog: connect: %w", err)
	}
	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		return nil, fmt.Errorf("catalog: ping: %w", err)
	}
	s := &Store{
		pool:      pool,
		wants:     make(chan string, wantQueue),
		synWants:  make(chan string, wantQueue),
		trWants:   make(chan string, wantQueue),
		peWants:   make(chan string, wantQueue),
		wantsDone: make(chan struct{}),
		stop:      make(chan struct{}),
	}
	if _, err := pool.Exec(ctx, metaSQL); err != nil {
		pool.Close()
		return nil, fmt.Errorf("catalog: create meta: %w", err)
	}
	go s.collectWants()
	return s, nil
}

// Close stops the store. The marks still in hand are written first:
// they are the record of what readers could not see, and a shutdown is
// no reason to lose it.
func (s *Store) Close() {
	if s.wants != nil {
		s.stopOnce.Do(func() { close(s.stop) })
		<-s.wantsDone
	}
	s.pool.Close()
}

// Ping reports whether the database is reachable, for the health check.
func (s *Store) Ping(ctx context.Context) error { return s.pool.Ping(ctx) }

// forSchema swaps the placeholder for a real schema name. The name is
// never user input — it is one of two constants in this file — so this
// cannot become an injection.
func forSchema(sql, schema string) string {
	return strings.ReplaceAll(sql, "{{schema}}", schema)
}

// ResetStaging drops any half-finished load and creates the tables. A
// process that died mid-import leaves catalog_next behind; it is dropped
// here rather than on the way out, because a crash has no way out.
func (s *Store) ResetStaging(ctx context.Context) error {
	if _, err := s.pool.Exec(ctx, `DROP SCHEMA IF EXISTS `+Staging+` CASCADE`); err != nil {
		return fmt.Errorf("catalog: drop %s: %w", Staging, err)
	}
	if _, err := s.pool.Exec(ctx, forSchema(schemaSQL, Staging)); err != nil {
		return fmt.Errorf("catalog: create %s: %w", Staging, err)
	}
	return nil
}

// Finish builds the indexes, makes the tables durable and gathers the
// statistics the planner needs. In that order: indexing unlogged tables
// is cheaper, and ANALYZE on an unindexed table tells the planner
// nothing useful.
func (s *Store) Finish(ctx context.Context, logger *slog.Logger) error {
	step := time.Now()
	if _, err := s.pool.Exec(ctx, forSchema(indexesSQL, Staging)); err != nil {
		return fmt.Errorf("catalog: build indexes: %w", err)
	}
	logger.Info("indexes built", "took", time.Since(step).Round(time.Second))
	step = time.Now()
	for _, table := range []string{"titles", "names", "principals", "directors", "ratings", "first_run"} {
		// SET LOGGED rewrites the table through the WAL. It is the price
		// of the load having skipped it, and it is paid once.
		if _, err := s.pool.Exec(ctx, fmt.Sprintf(`ALTER TABLE %s.%s SET LOGGED`, Staging, table)); err != nil {
			return fmt.Errorf("catalog: set %s logged: %w", table, err)
		}
	}
	logger.Info("tables made durable", "took", time.Since(step).Round(time.Second))
	step = time.Now()
	if _, err := s.pool.Exec(ctx, firstRunFor(Staging)); err != nil {
		return fmt.Errorf("catalog: build the first-run pool: %w", err)
	}
	logger.Info("first-run pool built", "took", time.Since(step).Round(time.Second))
	step = time.Now()
	if _, err := s.pool.Exec(ctx, `ANALYZE `+Staging+`.titles, `+Staging+`.names, `+
		Staging+`.principals, `+Staging+`.directors, `+Staging+`.ratings`); err != nil {
		return fmt.Errorf("catalog: analyze: %w", err)
	}
	logger.Info("statistics gathered", "took", time.Since(step).Round(time.Second))
	return nil
}

// Counts is how many rows each table holds.
type Counts struct {
	Titles     int64 `json:"titles"`
	Names      int64 `json:"names"`
	Principals int64 `json:"principals"`
	Directors  int64 `json:"directors"`
	Ratings    int64 `json:"ratings"`
}

// CountStaging reads the row counts of the load.
func (s *Store) CountStaging(ctx context.Context) (Counts, error) {
	var c Counts
	err := s.pool.QueryRow(ctx, fmt.Sprintf(`
		SELECT (SELECT count(*) FROM %[1]s.titles),
		       (SELECT count(*) FROM %[1]s.names),
		       (SELECT count(*) FROM %[1]s.principals),
		       (SELECT count(*) FROM %[1]s.directors),
		       (SELECT count(*) FROM %[1]s.ratings)`, Staging)).
		Scan(&c.Titles, &c.Names, &c.Principals, &c.Directors, &c.Ratings)
	if err != nil {
		return Counts{}, fmt.Errorf("catalog: count %s: %w", Staging, err)
	}
	return c, nil
}

// MinIntegrity is how much of the credit rows must point at a title that
// is actually stored. It is not 100%: IMDb's files are built separately
// and a handful of credits always name a title that is not in the set.
// A real mismatch — principals from one generation, titles from another
// — shows up as a number nowhere near this.
const MinIntegrity = 0.99

// Check rejects a load that is empty or internally inconsistent, before
// anything is published. It returns the share of credits whose title
// exists, for the log.
func (s *Store) Check(ctx context.Context, counts Counts) (float64, error) {
	switch {
	case counts.Titles == 0:
		return 0, &IntegrityError{Want: MinIntegrity, Reason: "no titles loaded"}
	case counts.Names == 0:
		return 0, &IntegrityError{Want: MinIntegrity, Reason: "no names loaded"}
	case counts.Principals == 0:
		return 0, &IntegrityError{Want: MinIntegrity, Reason: "no principals loaded"}
	case counts.Directors == 0:
		return 0, &IntegrityError{Want: MinIntegrity, Reason: "no directors loaded"}
	case counts.Ratings == 0:
		return 0, &IntegrityError{Want: MinIntegrity, Reason: "no ratings loaded"}
	}
	var share float64
	err := s.pool.QueryRow(ctx, fmt.Sprintf(`
		WITH credits AS (
		    SELECT tconst FROM %[1]s.principals
		    UNION ALL
		    SELECT tconst FROM %[1]s.directors
		)
		SELECT count(*) FILTER (WHERE t.tconst IS NOT NULL)::float8 / greatest(count(*), 1)
		FROM credits c LEFT JOIN %[1]s.titles t USING (tconst)`, Staging)).Scan(&share)
	if err != nil {
		return 0, fmt.Errorf("catalog: integrity check: %w", err)
	}
	if share < MinIntegrity {
		return share, &IntegrityError{Share: share, Want: MinIntegrity}
	}
	return share, nil
}

// PublishLockTimeout bounds how long the swap waits for its locks.
// Without it Postgres waits forever, and a rename queued behind one slow
// reader takes every request after it into the same queue. Failing fast
// leaves the previous catalog serving, which is the right way to lose.
const PublishLockTimeout = 5 * time.Second

// Publish makes the load live. The rename is one transaction; the old
// schema is not dropped here. It is left for the next run to drop, so
// in-flight readers holding a plan against the old tables finish against
// data that still exists.
func (s *Store) Publish(ctx context.Context, gen Generation, counts Counts) error {
	files, err := json.Marshal(stampsJSON(gen))
	if err != nil {
		return err
	}
	rows, err := json.Marshal(counts)
	if err != nil {
		return err
	}
	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return fmt.Errorf("catalog: begin publish: %w", err)
	}
	defer tx.Rollback(context.WithoutCancel(ctx))

	if _, err := tx.Exec(ctx, fmt.Sprintf(`SET LOCAL lock_timeout = '%dms'`, PublishLockTimeout.Milliseconds())); err != nil {
		return fmt.Errorf("catalog: set lock_timeout: %w", err)
	}
	// Anything left from a previous run goes first: two old schemas
	// would make the rename below fail on a name that is taken.
	if _, err := tx.Exec(ctx, `DROP SCHEMA IF EXISTS `+retired+` CASCADE`); err != nil {
		return fmt.Errorf("catalog: drop %s: %w", retired, err)
	}
	// A first run has no live schema to step aside.
	var liveExists bool
	if err := tx.QueryRow(ctx,
		`SELECT EXISTS (SELECT 1 FROM pg_namespace WHERE nspname = $1)`, Live).Scan(&liveExists); err != nil {
		return fmt.Errorf("catalog: look for %s: %w", Live, err)
	}
	if liveExists {
		if _, err := tx.Exec(ctx, `ALTER SCHEMA `+Live+` RENAME TO `+retired); err != nil {
			return fmt.Errorf("catalog: retire %s: %w", Live, err)
		}
	}
	if _, err := tx.Exec(ctx, `ALTER SCHEMA `+Staging+` RENAME TO `+Live); err != nil {
		return fmt.Errorf("catalog: promote %s: %w", Staging, err)
	}
	if _, err := tx.Exec(ctx, `
		INSERT INTO meta.generation (id, files, row_counts, imported_at)
		VALUES (1, $1, $2, now())
		ON CONFLICT (id) DO UPDATE
		SET files = EXCLUDED.files, row_counts = EXCLUDED.row_counts, imported_at = EXCLUDED.imported_at`,
		files, rows); err != nil {
		return fmt.Errorf("catalog: record generation: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("catalog: commit publish: %w", err)
	}
	// The vote snapshots the TMDb queue is ordered by belong to the
	// generation that just went live. Only rows still waiting are
	// touched — a title already answered for is out of the queue and
	// its old number is of no interest — and only where the number has
	// actually moved, so a night with no change rewrites nothing.
	if err := s.refreshQueueVotes(ctx); err != nil {
		return err
	}
	// New titles need posters, their posters need colours, and the
	// queue has just been reordered. Whoever holds the lease is
	// listening.
	s.notify(ctx, NotifyPublished)
	return nil
}

// refreshQueueVotes brings the TMDb queue's ordering up to date with
// the generation that has just been published.
func (s *Store) refreshQueueVotes(ctx context.Context) error {
	_, err := s.pool.Exec(ctx, `
		UPDATE meta.posters p
		SET votes = coalesce(r.num_votes, 0)
		FROM `+Live+`.titles t
		LEFT JOIN `+Live+`.ratings r USING (tconst)
		WHERE p.tconst = t.tconst
		  AND p.tmdb_at IS NULL
		  AND p.votes IS DISTINCT FROM coalesce(r.num_votes, 0)`)
	if err != nil {
		return fmt.Errorf("catalog: refresh queue votes: %w", err)
	}
	return nil
}

// DropRetired removes the previous generation. Called at the start of a
// run rather than at the end of the last one, so readers had the whole
// gap between imports to finish with it.
//
// The timeout and the drop share one transaction. As two statements on
// the pool they could land on different connections, leaving the drop
// to wait forever and the timeout behind on a connection the import's
// long statements would later borrow. SET LOCAL ends with the
// transaction, so nothing outlives the drop.
func (s *Store) DropRetired(ctx context.Context) error {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("catalog: begin drop %s: %w", retired, err)
	}
	defer tx.Rollback(context.WithoutCancel(ctx))

	if _, err := tx.Exec(ctx, fmt.Sprintf(`SET LOCAL lock_timeout = '%dms'`, PublishLockTimeout.Milliseconds())); err != nil {
		return fmt.Errorf("catalog: set lock_timeout: %w", err)
	}
	if _, err := tx.Exec(ctx, `DROP SCHEMA IF EXISTS `+retired+` CASCADE`); err != nil {
		return fmt.Errorf("catalog: drop %s: %w", retired, err)
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("catalog: commit drop %s: %w", retired, err)
	}
	return nil
}

// stampJSON is how a generation is written to meta. Its fields are
// Stamp's, so each converts to the other.
type stampJSON struct {
	LastModified time.Time `json:"last_modified"`
	ETag         string    `json:"etag,omitempty"`
	Length       int64     `json:"length,omitempty"`
}

func stampsJSON(gen Generation) map[string]stampJSON {
	out := make(map[string]stampJSON, len(gen))
	for f, s := range gen {
		out[string(f)] = stampJSON(s)
	}
	return out
}

// Published is the generation the live catalog was built from, and when.
// An empty generation means nothing has ever been published.
func (s *Store) Published(ctx context.Context) (Generation, time.Time, error) {
	var raw []byte
	var at time.Time
	err := s.pool.QueryRow(ctx, `SELECT files, imported_at FROM meta.generation WHERE id = 1`).Scan(&raw, &at)
	if err != nil {
		if err == pgx.ErrNoRows {
			return Generation{}, time.Time{}, nil
		}
		return nil, time.Time{}, fmt.Errorf("catalog: read generation: %w", err)
	}
	var stamps map[string]stampJSON
	if err := json.Unmarshal(raw, &stamps); err != nil {
		return nil, time.Time{}, fmt.Errorf("catalog: decode generation: %w", err)
	}
	gen := make(Generation, len(stamps))
	for name, s := range stamps {
		gen[File(name)] = Stamp(s)
	}
	return gen, at, nil
}

// PublishedFilms is how many films the live catalog holds, from the
// counts recorded when it was published. Zero when nothing has been.
func (s *Store) PublishedFilms(ctx context.Context) (int64, error) {
	var n *int64
	err := s.pool.QueryRow(ctx, `SELECT (row_counts->>'titles')::bigint FROM meta.generation WHERE id = 1`).Scan(&n)
	if err != nil {
		if err == pgx.ErrNoRows {
			return 0, nil
		}
		return 0, fmt.Errorf("catalog: read published counts: %w", err)
	}
	if n == nil {
		return 0, nil
	}
	return *n, nil
}

// LoadNotifyState is what the notifier last saved about what it had
// said, or nothing on a database it has never written to. With
// SaveNotifyState it makes the Store a notify.Memory.
func (s *Store) LoadNotifyState(ctx context.Context) ([]byte, error) {
	var raw []byte
	err := s.pool.QueryRow(ctx, `SELECT state FROM meta.notify WHERE id = 1`).Scan(&raw)
	if err != nil {
		if err == pgx.ErrNoRows {
			return nil, nil
		}
		return nil, fmt.Errorf("catalog: read notify state: %w", err)
	}
	return raw, nil
}

// SaveNotifyState replaces the notifier's saved state.
func (s *Store) SaveNotifyState(ctx context.Context, state []byte) error {
	_, err := s.pool.Exec(ctx, `
		INSERT INTO meta.notify (id, state, updated_at)
		VALUES (1, $1, now())
		ON CONFLICT (id) DO UPDATE
		SET state = EXCLUDED.state, updated_at = EXCLUDED.updated_at`, state)
	if err != nil {
		return fmt.Errorf("catalog: save notify state: %w", err)
	}
	return nil
}

// LiveReady reports whether a catalog has ever been published. Search
// and the grid answer 503 until it has: there is nothing to serve, and
// saying so is better than an empty map.
func (s *Store) LiveReady(ctx context.Context) (bool, error) {
	var ok bool
	err := s.pool.QueryRow(ctx,
		`SELECT EXISTS (SELECT 1 FROM pg_namespace WHERE nspname = $1)
		   AND EXISTS (SELECT 1 FROM meta.generation WHERE id = 1)`, Live).Scan(&ok)
	if err != nil {
		return false, fmt.Errorf("catalog: readiness: %w", err)
	}
	return ok, nil
}
