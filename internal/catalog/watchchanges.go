package catalog

// Keeping kept where-to-watch answers right from the Streaming
// Availability API's /changes feed.
//
// A kept answer stays right until something about the movie changes in
// that country: a service starts carrying it, stops, or changes how. The
// feed lists those changes, a country at a time, so once a day the job
// reads what changed since it last looked in each country somebody has
// a kept answer for, and rewrites only the answers that changed. A movie
// is not asked about again just because its answer is old. The few the
// feed does not keep right, an option leaving and a change it missed,
// are caught by the expiry jobs and by WatchFresh.
//
// A page lists up to 25 changes to any movie in the country, kept or
// not, so the cost is the size of the country's changes, not of what is
// kept. It is capped per country per run (ChangesMaxPages). A run the cap
// stops records how far it got, and the next carries on from there.

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/riverqueue/river"

	"cinedikt/internal/streaming"
)

// ChangesEvery is how long a country's changes are left before they are
// read again. The job looks every ChangesCheckEvery, and reads each
// country whose last run is at least this old, so a deploy restarting
// the queue neither reads them early nor holds them back.
const ChangesEvery = 24 * time.Hour

// ChangesFirstReach is how far back a country's first run starts: a day,
// or back to the oldest answer kept for it when that is older, so the
// first run reads every change since any of its answers was fetched.
const ChangesFirstReach = 24 * time.Hour

// ChangesReach is as far back as the feed goes: it refuses a window that
// starts more than 31 days ago.
const ChangesReach = 31 * 24 * time.Hour

// changesReachMargin keeps a window cut to ChangesReach inside it by the
// API's clock too, for every page of a run that takes a while.
const changesReachMargin = time.Hour

// DefaultChangesMaxPages is the most pages of the feed one country's run
// reads, when none is configured.
const DefaultChangesMaxPages = 40

// changeTypes are the kinds of change read. Each is its own feed with its
// own pages.
var changeTypes = []string{streaming.ChangeNew, streaming.ChangeRemoved, streaming.ChangeUpdated}

// changesRun is what one country's run read and did, for the log, where
// it makes the cost visible, and for the tests.
type changesRun struct {
	Country  string
	From, To time.Time
	// Clamped is a window cut to ChangesReach: changes before From were
	// never read.
	Clamped bool
	Pages   int
	// Changes is every change read, to any movie.
	Changes int
	// Written is the kept answers rewritten straight from the feed, and
	// Queued the refreshes queued for the ones the feed carried no options
	// for.
	Written int
	Queued  int
	// Unmatched is the changes whose show the page did not carry, or
	// carried without an IMDb id.
	Unmatched int
	// Capped is a run the page cap stopped.
	Capped bool
	// SyncedTo is where the next run starts; zero when the run recorded
	// nothing.
	SyncedTo time.Time
}

// watchedCountry is a covered country somebody has a kept answer for.
type watchedCountry struct {
	Code string
	// Oldest is when its oldest kept answer was fetched.
	Oldest time.Time
	// Synced is whether a run has recorded how far it got; SyncedTo is
	// that, and RanAt when it was recorded.
	Synced   bool
	SyncedTo time.Time
	RanAt    time.Time
}

// changesFrom is where a country's run starts: where the last run got
// to, or for a first run ChangesFirstReach back, and never before the
// feed's reach. clamped reports a start cut to that reach.
func changesFrom(c watchedCountry, now time.Time) (from time.Time, clamped bool) {
	if c.Synced {
		from = c.SyncedTo
	} else {
		from = now.Add(-ChangesFirstReach)
		if !c.Oldest.IsZero() && c.Oldest.Before(from) {
			from = c.Oldest
		}
	}
	floor := now.Add(-ChangesReach + changesReachMargin)
	if from.Before(floor) {
		return floor.Truncate(time.Second), true
	}
	return from.Truncate(time.Second), false
}

func (w *WhereToWatch) changesMaxPages() int {
	n := w.ChangesMaxPages
	if n <= 0 {
		n = DefaultChangesMaxPages
	}
	// A country's progress is the least any of its feeds made, so a run
	// that read nothing of one of them would get nowhere, every day. A
	// run always reads the first page of each.
	return max(n, len(changeTypes))
}

// syncChanges reads the changes for every country that is due, and
// returns what each run did. A country whose run fails is left as its
// run leaves it, and the others still run; a refused key ends the job,
// since every country would meet it.
func (w *WhereToWatch) syncChanges(ctx context.Context) ([]changesRun, error) {
	countries, err := w.Store.watchedCountries(ctx)
	if err != nil {
		return nil, err
	}
	var runs []changesRun
	var errs []error
	for _, c := range countries {
		if !w.changesDue(c) {
			continue
		}
		run, err := w.syncCountry(ctx, c)
		runs = append(runs, run)
		w.logChanges(ctx, run)
		if err != nil {
			err = fmt.Errorf("catalog: changes in %s: %w", c.Code, err)
			if errors.Is(err, streaming.ErrKey) {
				return runs, err
			}
			errs = append(errs, err)
		}
		if ctx.Err() != nil {
			return runs, errors.Join(append(errs, ctx.Err())...)
		}
	}
	return runs, errors.Join(errs...)
}

// changesDue reports whether a country's changes are due: never read, or
// last read ChangesEvery ago, less half a look's period, so the look that
// comes a day after the last run reads it rather than the one an hour
// after that.
func (w *WhereToWatch) changesDue(c watchedCountry) bool {
	return !c.Synced || w.clock().Sub(c.RanAt) >= ChangesEvery-ChangesCheckEvery/2
}

// feedProgress is one kind of change being read in one country's run.
type feedProgress struct {
	kind   string
	cursor string
	// done is a feed with nothing more to read in this run.
	done bool
	// reached is the time of the last change handled, from the start of
	// the window until one is, and the end of the window once the feed
	// has got there. Every change before it has been handled, since the
	// feed is oldest first; changes at that same second may not have
	// been.
	reached time.Time
}

// syncCountry is one country's run. It reads the three feeds a page at a
// time in turn, so the cap stops all three part way rather than starving
// the last, and records where the next run starts: the end of the window
// when every feed got there, or the least any unfinished one reached.
//
// A page that cannot be read, or a change that cannot be kept, ends the
// run, and so does a stop. What every feed got past before that is
// recorded, and the country is not read again until tomorrow. When that
// is nothing, it is still recorded once a page was read; when no page
// was read, or the run was stopped, nothing is, and River's retry or the
// next look tries again.
func (w *WhereToWatch) syncCountry(ctx context.Context, c watchedCountry) (changesRun, error) {
	now := w.clock().UTC().Truncate(time.Second)
	run := changesRun{Country: c.Code, To: now}
	run.From, run.Clamped = changesFrom(c, now)
	feeds := make([]*feedProgress, len(changeTypes))
	for i, kind := range changeTypes {
		feeds[i] = &feedProgress{kind: kind, reached: run.From}
	}
	// A movie changed several times in a run, in one feed or several, is
	// handled once: the feed's show is how it stands now, whichever change
	// brought it, and a refresh asks how it stands now.
	handled := map[string]bool{}
	maxPages := w.changesMaxPages()
	var failure error
read:
	for {
		open := false
		for _, f := range feeds {
			if f.done {
				continue
			}
			if run.Pages >= maxPages {
				run.Capped = true
				break read
			}
			page, err := w.API.Changes(ctx, streaming.ChangesQuery{
				Country: c.Code, Type: f.kind, From: run.From, To: run.To, Cursor: f.cursor,
			})
			if err != nil {
				failure = err
				break read
			}
			run.Pages++
			if err := w.applyChanges(ctx, c.Code, page, f, handled, &run); err != nil {
				failure = err
				break read
			}
			switch {
			case !page.HasMore:
				f.done, f.reached = true, run.To
			case page.NextCursor == "":
				// More, and no way to ask for it: the feed stops here
				// for this run, and the next starts where it stopped.
				f.done = true
				if w.Logger != nil {
					w.Logger.WarnContext(ctx, "a page of streaming changes said there were more but gave no cursor; the rest waits for the next run",
						"country", c.Code, "change_type", f.kind, "reached", f.reached)
				}
			default:
				f.cursor = page.NextCursor
				open = true
			}
		}
		if !open {
			break
		}
	}

	synced := run.To
	for _, f := range feeds {
		if f.reached.Before(synced) {
			synced = f.reached
		}
	}
	if failure != nil {
		// A failure before any page was read spent nothing metered, and a
		// stop is not the page's fault: either way the next look, or
		// River's retry, tries again. Once a page has been read, the run
		// is recorded even when it got nowhere, so a page that always
		// fails waits a day rather than having the pages before it read
		// again on every try of every hourly look.
		if !synced.After(run.From) && (run.Pages == 0 || ctx.Err() != nil) {
			return run, failure
		}
	} else if run.Capped && !synced.After(run.From) {
		// Every page the cap allowed held changes at the second the run
		// started from. Starting there again would read the same pages,
		// every day; the rest of that second's changes are left to the
		// expiry jobs and WatchFresh.
		synced = run.From.Add(time.Second)
		if w.Logger != nil {
			w.Logger.Warn("the page cap was spent inside one second of streaming changes; moving past it",
				"country", c.Code, "second", run.From, "pages", run.Pages)
		}
	}
	// The record outlasts a stop: what was handled stays handled.
	keepCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), watchEnqueue)
	defer cancel()
	if err := w.Store.keepChangesSync(keepCtx, c.Code, synced); err != nil {
		return run, errors.Join(failure, err)
	}
	run.SyncedTo = synced
	return run, failure
}

// applyChanges handles one page in order: each change to a movie kept
// for the country, not handled already in this run, is written straight
// from the feed when the feed carries the movie's options there, and
// otherwise asked again by a refresh. Changes to anything else are
// passed over. f.reached follows the last change handled, so an error
// part way leaves it where it is true.
func (w *WhereToWatch) applyChanges(ctx context.Context, country string, page streaming.ChangesPage, f *feedProgress, handled map[string]bool, run *changesRun) error {
	run.Changes += len(page.Changes)
	var ids []string
	for _, ch := range page.Changes {
		if show, ok := page.Shows[ch.ShowID]; ok && !handled[show.IMDbID] {
			ids = append(ids, show.IMDbID)
		}
	}
	kept, err := w.Store.keptAnswers(ctx, country, ids)
	if err != nil {
		return err
	}
	for _, ch := range page.Changes {
		show, ok := page.Shows[ch.ShowID]
		if !ok {
			run.Unmatched++
		}
		if ok && kept[show.IMDbID] && !handled[show.IMDbID] {
			written, queued, err := w.applyChange(ctx, country, show)
			if err != nil {
				return err
			}
			if written {
				run.Written++
			}
			if queued {
				run.Queued++
			}
			handled[show.IMDbID] = true
		}
		if !ch.At.IsZero() && ch.At.After(f.reached) {
			f.reached = ch.At
		}
	}
	return nil
}

// applyChange keeps one movie's answer right. The feed's own options for
// the country are written as they are, through the same keep as every
// other answer, which schedules the job for the first that leaves. A
// show that came without them is asked about by a refresh, once: the
// feed saying nothing is not the movie being on nothing.
func (w *WhereToWatch) applyChange(ctx context.Context, country string, show streaming.ChangedShow) (written, queued bool, err error) {
	if options, ok := show.Options(country); ok {
		now := w.clock()
		if err := w.keep(ctx, show.IMDbID, country, streaming.Shape(options, now), streaming.NextExpiry(options, now)); err != nil {
			return false, false, err
		}
		return true, false, nil
	}
	if w.queue == nil {
		return false, false, errors.New("catalog: no queue to ask for a where-to-watch refresh")
	}
	res, err := w.queue.client.Insert(ctx, WatchRefreshArgs{Tconst: show.IMDbID, Country: country}, nil)
	if err != nil {
		return false, false, fmt.Errorf("catalog: queue a where-to-watch refresh of %s/%s: %w", show.IMDbID, country, err)
	}
	return false, !res.UniqueSkippedAsDuplicate, nil
}

// logChanges says what a country's run cost and did. A run the cap
// stopped is a warning: the cap is a dial, and a country that reaches it
// every day has more changes than it allows. So is a run with changes no
// show on the page matched: whatever they changed waits for the expiry
// jobs or WatchFresh, and the feed is not what this reads it as.
func (w *WhereToWatch) logChanges(ctx context.Context, run changesRun) {
	if w.Logger == nil || run.Country == "" {
		return
	}
	if run.Clamped {
		// The answers those changes touched are asked again once they are
		// WatchFresh old.
		w.Logger.WarnContext(ctx, "streaming changes from before the feed's 31 days could not be read",
			"country", run.Country, "from", run.From)
	}
	const msg = "read the streaming changes"
	attrs := []any{
		"country", run.Country, "from", run.From, "to", run.To,
		"pages", run.Pages, "changes", run.Changes, "written", run.Written, "queued", run.Queued,
		"unmatched", run.Unmatched, "capped", run.Capped,
	}
	if !run.SyncedTo.IsZero() {
		attrs = append(attrs, "synced_to", run.SyncedTo)
	}
	// A run can be both, and each is said.
	warning := ""
	if run.Unmatched > 0 {
		warning += "; some changes named shows the page did not carry"
	}
	if run.Capped {
		warning += "; the page cap stopped the run, and the next carries on"
	}
	if warning != "" {
		w.Logger.WarnContext(ctx, msg+warning, attrs...)
		return
	}
	w.Logger.InfoContext(ctx, msg, attrs...)
}

// watchedCountries is every covered country somebody has a kept answer
// for, with how far its last run got. A country nobody has kept an
// answer for costs nothing, and neither does one the API stopped
// covering.
func (s *Store) watchedCountries(ctx context.Context) ([]watchedCountry, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT w.country, min(w.fetched_at), s.synced_to, s.updated_at
		FROM meta.where_to_watch w
		JOIN meta.streaming_countries c ON c.code = w.country
		LEFT JOIN meta.streaming_sync s ON s.country = w.country
		GROUP BY w.country, s.synced_to, s.updated_at
		ORDER BY w.country`)
	if err != nil {
		return nil, fmt.Errorf("catalog: countries with where-to-watch answers: %w", err)
	}
	var out []watchedCountry
	for rows.Next() {
		var c watchedCountry
		var syncedTo, ranAt *time.Time
		if err := rows.Scan(&c.Code, &c.Oldest, &syncedTo, &ranAt); err != nil {
			rows.Close()
			return nil, fmt.Errorf("catalog: countries with where-to-watch answers: %w", err)
		}
		if syncedTo != nil && ranAt != nil {
			c.Synced, c.SyncedTo, c.RanAt = true, *syncedTo, *ranAt
		}
		out = append(out, c)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("catalog: countries with where-to-watch answers: %w", err)
	}
	return out, nil
}

// keptAnswers is which of tconsts have an answer kept for country.
func (s *Store) keptAnswers(ctx context.Context, country string, tconsts []string) (map[string]bool, error) {
	kept := map[string]bool{}
	if len(tconsts) == 0 {
		return kept, nil
	}
	rows, err := s.pool.Query(ctx, `
		SELECT tconst FROM meta.where_to_watch WHERE country = $1 AND tconst = ANY($2)`, country, tconsts)
	if err != nil {
		return nil, fmt.Errorf("catalog: kept where-to-watch answers in %s: %w", country, err)
	}
	tconst := ""
	if _, err := pgx.ForEachRow(rows, []any{&tconst}, func() error {
		kept[tconst] = true
		return nil
	}); err != nil {
		return nil, fmt.Errorf("catalog: kept where-to-watch answers in %s: %w", country, err)
	}
	return kept, nil
}

// keepChangesSync records where a country's next run starts, and that a
// run has just ended.
func (s *Store) keepChangesSync(ctx context.Context, country string, syncedTo time.Time) error {
	_, err := s.pool.Exec(ctx, `
		INSERT INTO meta.streaming_sync (country, synced_to, updated_at)
		VALUES ($1, $2, now())
		ON CONFLICT (country) DO UPDATE
		SET synced_to  = EXCLUDED.synced_to,
		    updated_at = EXCLUDED.updated_at`, country, syncedTo)
	if err != nil {
		return fmt.Errorf("catalog: keep where the streaming changes in %s got to: %w", country, err)
	}
	return nil
}

// StreamingChangesArgs is the hourly look for countries whose changes
// are due.
type StreamingChangesArgs struct{}

// Kind names the job in River's tables.
func (StreamingChangesArgs) Kind() string { return "streaming_changes" }

// InsertOpts keeps one look queued or running at a time, across every
// process: two runs at once would read the same pages twice.
func (StreamingChangesArgs) InsertOpts() river.InsertOpts {
	return river.InsertOpts{MaxAttempts: queuePeriodicTries, UniqueOpts: river.UniqueOpts{ByState: pendingStates}}
}

type changesWorker struct {
	river.WorkerDefaults[StreamingChangesArgs]
	w *WhereToWatch
}

func (k *changesWorker) Timeout(*river.Job[StreamingChangesArgs]) time.Duration {
	return queueChangesTimeout
}

func (k *changesWorker) Work(ctx context.Context, _ *river.Job[StreamingChangesArgs]) error {
	_, err := k.w.syncChanges(ctx)
	logJob(ctx, k.w.Logger, "read the streaming changes", err)
	if errors.Is(err, streaming.ErrKey) {
		// The same key gets the same answer in every country. The next
		// look is an hour away, and a fixed key is used then.
		return river.JobCancel(err)
	}
	return err
}
