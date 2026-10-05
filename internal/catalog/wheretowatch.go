package catalog

// Where a movie can be watched in a reader's country.
//
// The Streaming Availability API is metered per request, so its answers
// are kept in meta.where_to_watch, one per movie and country, and the API
// is not asked again about a pair it has answered. A pair nobody has
// asked about is asked in the reader's request, once however many
// readers arrive together, and kept. A kept answer is served as it
// stands, and kept until something changes it:
//
//   - the API's feed of changes says the movie changed in that country,
//     which the daily changes job reads (watchchanges.go);
//   - one of its options leaves, at the moment a River job was scheduled
//     for, so the page stops offering it then;
//   - or it is WatchFresh old, when it is still served, and a River job
//     asks again behind it, in case the feed missed something.
//
// Only movies readers open are ever asked about. There is no sweep, and
// a map never asks for its cards.

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/riverqueue/river"
	"golang.org/x/sync/singleflight"

	"cinedikt/internal/streaming"
)

// WatchFresh is how old an answer may be before reading it also asks the
// API again, behind the answer. The changes job and the expiry jobs keep
// answers right, so this is only a safety net, for a change the feed
// missed or one from before the feed's 31 days.
const WatchFresh = 30 * 24 * time.Hour

// WatchFirstAsk bounds a reader's wait on a movie and country nobody has
// asked about. Past it the ask fails: the reader is answered 502, nothing
// is kept, and the page leaves the section out.
const WatchFirstAsk = 5 * time.Second

// CountriesFresh is how old the list of covered countries may be before
// it is asked for again.
const CountriesFresh = 7 * 24 * time.Hour

// watchEnqueue bounds a write made on a reader's behalf once their
// answer is in hand: keeping a first ask's answer, or asking for a stale
// answer's refresh.
const watchEnqueue = 2 * time.Second

// countriesCooldown is how long a failed fill of the country list stands
// as the answer before a reader's request tries again. Until the list is
// filled every request needs it, and an API that is down would otherwise
// be asked, and billed, for it by every one of them.
const countriesCooldown = time.Minute

// Uncovered is the country a reader counts as when their address could
// not be placed: a country with no coverage.
const Uncovered = "xx"

// ErrUpstream is the Streaming Availability API failing to answer, which
// the route tells apart from the store failing to be read.
var ErrUpstream = errors.New("catalog: the streaming API could not answer")

// StreamingAPI is what the service asks: the Streaming Availability API.
type StreamingAPI interface {
	Show(ctx context.Context, imdbID, country string) ([]streaming.StreamingOption, error)
	Countries(ctx context.Context) ([]streaming.Country, error)
	Changes(ctx context.Context, q streaming.ChangesQuery) (streaming.ChangesPage, error)
}

// WhereToWatch answers GET /api/where-to-watch/{tconst}.
type WhereToWatch struct {
	// Store is where the answers are kept, through the pool readers are
	// served from: a keep is one row.
	Store  *Store
	API    StreamingAPI
	Logger *slog.Logger
	// ChangesMaxPages is the most pages of the changes feed one country's
	// run reads. Zero or less is DefaultChangesMaxPages.
	ChangesMaxPages int

	// queue schedules the refreshes. OpenQueue sets it; without one,
	// answers are kept and served but never asked again.
	queue  *Queue
	flight singleflight.Group
	// now is the clock, for the tests. Nil is time.Now.
	now func() time.Time

	// fillMu guards the last failed fill of the country list.
	fillMu     sync.Mutex
	fillFailed time.Time
	fillErr    error
}

// WatchReply is the route's answer. A covered country has all four lists,
// each [] when empty; a country without coverage has none of them.
type WatchReply struct {
	Country string `json:"country"`
	// CountryName is the country as the page's sentence names it. Only a
	// covered country has one.
	CountryName string `json:"countryName,omitempty"`
	Covered     bool   `json:"covered"`
	*streaming.Answer
}

// Answer is where tconst can be watched in country, a lowercased ISO
// code; "" or Uncovered for an address that could not be placed.
//
// ErrNotFound is a movie the catalog does not hold, which the API is
// never asked about. ErrUpstream is the API failing to answer, and any
// other error the store failing to be read; nothing is kept from either.
func (w *WhereToWatch) Answer(ctx context.Context, tconst, country string) (WatchReply, error) {
	if !streaming.ValidCountry(country) || country == Uncovered {
		return WatchReply{Country: Uncovered}, nil
	}
	name, covered, err := w.coverage(ctx, country)
	if err != nil {
		return WatchReply{}, err
	}
	if !covered {
		return WatchReply{Country: country}, nil
	}
	row, err := w.Store.WhereToWatch(ctx, tconst, country)
	if err != nil {
		return WatchReply{}, err
	}
	if !row.Title {
		return WatchReply{}, fmt.Errorf("catalog: %s: %w", tconst, ErrNotFound)
	}
	reply := WatchReply{Country: country, CountryName: name, Covered: true}
	if row.Stored {
		if w.clock().Sub(row.FetchedAt) > WatchFresh {
			w.refreshSoon(ctx, tconst, country)
		}
		answer := row.Answer
		reply.Answer = &answer
		return reply, nil
	}
	answer, err := w.firstAsk(ctx, tconst, country)
	if err != nil {
		return WatchReply{}, err
	}
	reply.Answer = &answer
	return reply, nil
}

func (w *WhereToWatch) clock() time.Time {
	if w.now != nil {
		return w.now()
	}
	return time.Now()
}

// coverage is the country's name and whether the API covers it. The list
// is filled the first time anyone needs it, by that reader's request,
// and kept fresh by the daily job after that.
func (w *WhereToWatch) coverage(ctx context.Context, country string) (string, bool, error) {
	name, known, listed, err := w.Store.StreamingCountry(ctx, country)
	if err != nil || listed {
		return name, known, err
	}
	w.fillMu.Lock()
	failed, failure := w.fillFailed, w.fillErr
	w.fillMu.Unlock()
	if failure != nil && w.clock().Sub(failed) < countriesCooldown {
		return "", false, failure
	}
	ch := w.flight.DoChan("countries", func() (any, error) {
		fillCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), WatchFirstAsk)
		defer cancel()
		_, err := w.refreshCountries(fillCtx)
		w.fillMu.Lock()
		w.fillFailed, w.fillErr = w.clock(), err
		w.fillMu.Unlock()
		return nil, err
	})
	select {
	case <-ctx.Done():
		return "", false, ctx.Err()
	case res := <-ch:
		if res.Err != nil {
			return "", false, res.Err
		}
	}
	name, known, _, err = w.Store.StreamingCountry(ctx, country)
	return name, known, err
}

// firstAsk asks the API about a pair it has never answered, keeps the
// answer, and returns it. Readers who arrive while it is being asked wait
// on the same ask. It is detached from the reader who started it, so
// their leaving does not fail the others, and it is given WatchFirstAsk.
func (w *WhereToWatch) firstAsk(ctx context.Context, tconst, country string) (streaming.Answer, error) {
	ch := w.flight.DoChan(tconst+"/"+country, func() (any, error) {
		askCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), WatchFirstAsk)
		defer cancel()
		answer, expires, err := w.fetch(askCtx, tconst, country, w.clock())
		if err != nil {
			return streaming.Answer{}, fmt.Errorf("%w: %w", ErrUpstream, err)
		}
		// A budget of its own: an answer that took most of the ask's
		// seconds to arrive is still worth keeping.
		keepCtx, cancelKeep := context.WithTimeout(context.WithoutCancel(ctx), watchEnqueue)
		defer cancelKeep()
		if err := w.keep(keepCtx, tconst, country, answer, expires); err != nil && w.Logger != nil {
			// The reader still has the answer. The next one asks again,
			// which costs a request and nothing worse.
			w.Logger.Warn("keep a where-to-watch answer", "tconst", tconst, "country", country, "err", err)
		}
		return answer, nil
	})
	select {
	case <-ctx.Done():
		return streaming.Answer{}, ctx.Err()
	case res := <-ch:
		if res.Err != nil {
			return streaming.Answer{}, res.Err
		}
		return res.Val.(streaming.Answer), nil
	}
}

// fetch asks the API and shapes the answer, as of now. A show the API
// does not know is on nothing, which is an answer like any other.
func (w *WhereToWatch) fetch(ctx context.Context, tconst, country string, now time.Time) (streaming.Answer, time.Time, error) {
	options, err := w.API.Show(ctx, tconst, country)
	if errors.Is(err, streaming.ErrNotFound) {
		options, err = nil, nil
	}
	if err != nil {
		return streaming.Answer{}, time.Time{}, err
	}
	return streaming.Shape(options, now), streaming.NextExpiry(options, now), nil
}

// keep stores an answer and, in the same transaction, schedules the ask
// for the moment its first leaving option goes: an answer is never kept
// without the job that keeps it right, and a job is never left for an
// answer that was not kept.
func (w *WhereToWatch) keep(ctx context.Context, tconst, country string, answer streaming.Answer, expires time.Time) error {
	raw, err := json.Marshal(answer)
	if err != nil {
		return err
	}
	var expiresAt *time.Time
	if !expires.IsZero() {
		expiresAt = &expires
	}
	tx, err := w.Store.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("catalog: begin keeping where to watch %s/%s: %w", tconst, country, err)
	}
	defer tx.Rollback(context.WithoutCancel(ctx))
	if _, err := tx.Exec(ctx, `
		INSERT INTO meta.where_to_watch (tconst, country, answer, fetched_at, expires_at)
		VALUES ($1, $2, $3, now(), $4)
		ON CONFLICT (tconst, country) DO UPDATE
		SET answer     = EXCLUDED.answer,
		    fetched_at = EXCLUDED.fetched_at,
		    expires_at = EXCLUDED.expires_at`, tconst, country, raw, expiresAt); err != nil {
		return fmt.Errorf("catalog: keep where to watch %s/%s: %w", tconst, country, err)
	}
	if expiresAt != nil && w.queue != nil {
		args := WatchRefreshArgs{Tconst: tconst, Country: country, At: expires}
		if _, err := w.queue.client.InsertTx(ctx, tx, args, &river.InsertOpts{ScheduledAt: expires}); err != nil {
			return fmt.Errorf("catalog: schedule where to watch %s/%s: %w", tconst, country, err)
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("catalog: commit where to watch %s/%s: %w", tconst, country, err)
	}
	return nil
}

// refreshSoon asks for an old answer to be asked again, as soon as a
// worker is free. Any number of readers may find it old; the job is
// unique among those still to run, so they ask for it once.
func (w *WhereToWatch) refreshSoon(ctx context.Context, tconst, country string) {
	if w.queue == nil {
		return
	}
	ctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), watchEnqueue)
	defer cancel()
	if _, err := w.queue.client.Insert(ctx, WatchRefreshArgs{Tconst: tconst, Country: country}, nil); err != nil && w.Logger != nil {
		w.Logger.Warn("ask for a where-to-watch refresh", "tconst", tconst, "country", country, "err", err)
	}
}

// refresh is one refresh job: ask the API again, keep the new answer, and
// schedule the next ask if something in it is leaving. wanted is when
// the job was asked for.
func (w *WhereToWatch) refresh(ctx context.Context, args WatchRefreshArgs, wanted time.Time) error {
	row, err := w.Store.WhereToWatch(ctx, args.Tconst, args.Country)
	if err != nil {
		return err
	}
	if !row.Title {
		// The movie has left the catalog, and nobody can open it.
		return nil
	}
	// An answer kept since the job was due is already the one it would
	// fetch: another job, or a reader's first ask, got there first.
	due := wanted
	if args.At.After(due) {
		due = args.At
	}
	if row.Stored && !row.FetchedAt.Before(due) {
		return nil
	}
	// As of the moment the job is for, at the earliest: run a moment
	// early by a clock that disagrees with the database's, the option
	// that is leaving must still count as gone.
	now := w.clock()
	if args.At.After(now) {
		now = args.At
	}
	answer, expires, err := w.fetch(ctx, args.Tconst, args.Country, now)
	if errors.Is(err, streaming.ErrKey) {
		// The same key gets the same answer. The old answer stays, and
		// the next reader to find it old asks again.
		return river.JobCancel(err)
	}
	if err != nil {
		return err
	}
	return w.keep(ctx, args.Tconst, args.Country, answer, expires)
}

// refreshCountries asks the API for the countries it covers, unless the
// list kept is less than CountriesFresh old. A list that comes back empty
// is not kept: it would tell every reader they have no coverage. The
// API's failures are ErrUpstream, and the store's are not.
func (w *WhereToWatch) refreshCountries(ctx context.Context) (bool, error) {
	oldest, n, err := w.Store.StreamingCountriesAge(ctx)
	if err != nil {
		return false, err
	}
	if n > 0 && w.clock().Sub(oldest) < CountriesFresh {
		return false, nil
	}
	list, err := w.API.Countries(ctx)
	if err != nil {
		return false, fmt.Errorf("%w: %w", ErrUpstream, err)
	}
	if len(list) == 0 {
		return false, fmt.Errorf("%w: it listed no countries", ErrUpstream)
	}
	return true, w.Store.KeepStreamingCountries(ctx, list)
}

// WatchRow is everything the route needs about one movie in one country,
// read in one query.
type WatchRow struct {
	// Title is whether the catalog holds the movie at all.
	Title bool
	// Stored is whether an answer is kept; Answer is that answer.
	Stored    bool
	Answer    streaming.Answer
	FetchedAt time.Time
}

// WhereToWatch reads what is kept about tconst in country.
func (s *Store) WhereToWatch(ctx context.Context, tconst, country string) (WatchRow, error) {
	var row WatchRow
	var raw []byte
	var fetched *time.Time
	err := s.pool.QueryRow(ctx, `
		SELECT t.tconst IS NOT NULL, w.answer, w.fetched_at
		FROM (SELECT $1::text AS tconst) q
		LEFT JOIN `+Live+`.titles t ON t.tconst = q.tconst
		LEFT JOIN meta.where_to_watch w ON w.tconst = q.tconst AND w.country = $2`, tconst, country).
		Scan(&row.Title, &raw, &fetched)
	if err != nil {
		return WatchRow{}, fmt.Errorf("catalog: where to watch %s/%s: %w", tconst, country, err)
	}
	if raw != nil {
		if err := json.Unmarshal(raw, &row.Answer); err != nil {
			return WatchRow{}, fmt.Errorf("catalog: decode where to watch %s/%s: %w", tconst, country, err)
		}
		row.Answer.Normalise()
		row.Stored = true
	}
	if fetched != nil {
		row.FetchedAt = *fetched
	}
	return row, nil
}

// StreamingCountry is the name of a covered country, whether the API
// covers it, and whether there is a list to have looked in at all.
func (s *Store) StreamingCountry(ctx context.Context, code string) (name string, known, listed bool, err error) {
	var n *string
	err = s.pool.QueryRow(ctx, `
		SELECT (SELECT name FROM meta.streaming_countries WHERE code = $1),
		       EXISTS (SELECT 1 FROM meta.streaming_countries)`, code).Scan(&n, &listed)
	if err != nil {
		return "", false, false, fmt.Errorf("catalog: streaming country %s: %w", code, err)
	}
	if n != nil {
		return *n, true, listed, nil
	}
	return "", false, listed, nil
}

// StreamingCountriesAge is when the kept list was fetched, and how many
// countries it holds.
func (s *Store) StreamingCountriesAge(ctx context.Context) (time.Time, int, error) {
	var oldest *time.Time
	var n int
	if err := s.pool.QueryRow(ctx, `SELECT min(fetched_at), count(*) FROM meta.streaming_countries`).Scan(&oldest, &n); err != nil {
		return time.Time{}, 0, fmt.Errorf("catalog: streaming countries: %w", err)
	}
	if oldest == nil {
		return time.Time{}, n, nil
	}
	return *oldest, n, nil
}

// KeepStreamingCountries replaces the list of covered countries, in one
// transaction, so no reader ever reads half of it.
func (s *Store) KeepStreamingCountries(ctx context.Context, list []streaming.Country) error {
	codes := make([]string, len(list))
	names := make([]string, len(list))
	for i, c := range list {
		codes[i], names[i] = c.Code, c.Name
	}
	return pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		if _, err := tx.Exec(ctx, `DELETE FROM meta.streaming_countries`); err != nil {
			return fmt.Errorf("catalog: clear streaming countries: %w", err)
		}
		if _, err := tx.Exec(ctx, `
			INSERT INTO meta.streaming_countries (code, name, fetched_at)
			SELECT code, name, now() FROM unnest($1::text[], $2::text[]) AS c(code, name)
			ON CONFLICT (code) DO UPDATE SET name = EXCLUDED.name, fetched_at = EXCLUDED.fetched_at`, codes, names); err != nil {
			return fmt.Errorf("catalog: keep streaming countries: %w", err)
		}
		return nil
	})
}

// WatchRefreshArgs is a refresh of one movie in one country.
type WatchRefreshArgs struct {
	Tconst  string `json:"tconst"`
	Country string `json:"country"`
	// At is the moment an option leaves, for the refresh scheduled for
	// it; zero for one asked for as soon as possible, because the answer
	// has grown old or the changes feed said the movie changed without
	// saying how it stands. It is part of what makes a job unique, so the
	// two kinds never stand in for each other, while a second of either
	// collapses into the first.
	At time.Time `json:"at,omitzero"`
}

// Kind names the job in River's tables.
func (WatchRefreshArgs) Kind() string { return "where_to_watch_refresh" }

// InsertOpts makes a refresh unique by its arguments among the jobs not
// yet finished.
func (WatchRefreshArgs) InsertOpts() river.InsertOpts {
	return river.InsertOpts{
		MaxAttempts: queueRefreshAttempts,
		UniqueOpts:  river.UniqueOpts{ByArgs: true, ByState: pendingStates},
	}
}

type watchRefreshWorker struct {
	river.WorkerDefaults[WatchRefreshArgs]
	w *WhereToWatch
}

func (k *watchRefreshWorker) Timeout(*river.Job[WatchRefreshArgs]) time.Duration {
	return queueRefreshTimeout
}

func (k *watchRefreshWorker) Work(ctx context.Context, job *river.Job[WatchRefreshArgs]) error {
	err := k.w.refresh(ctx, job.Args, job.CreatedAt)
	logJob(ctx, k.w.Logger, "refresh where to watch", err,
		"tconst", job.Args.Tconst, "country", job.Args.Country, "attempt", job.Attempt)
	return err
}

// StreamingCountriesArgs is the daily look at how old the country list
// is.
type StreamingCountriesArgs struct{}

// Kind names the job in River's tables.
func (StreamingCountriesArgs) Kind() string { return "streaming_countries" }

// InsertOpts keeps one look queued or running at a time.
func (StreamingCountriesArgs) InsertOpts() river.InsertOpts {
	return river.InsertOpts{MaxAttempts: queuePeriodicTries, UniqueOpts: river.UniqueOpts{ByState: pendingStates}}
}

type countriesWorker struct {
	river.WorkerDefaults[StreamingCountriesArgs]
	w *WhereToWatch
}

func (k *countriesWorker) Work(ctx context.Context, _ *river.Job[StreamingCountriesArgs]) error {
	_, err := k.w.refreshCountries(ctx)
	logJob(ctx, k.w.Logger, "refresh the streaming countries", err)
	if errors.Is(err, streaming.ErrKey) {
		return river.JobCancel(err)
	}
	return err
}
