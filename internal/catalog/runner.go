package catalog

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"sync"
	"time"

	"golang.org/x/time/rate"

	"cinedikt/internal/notify"
	"cinedikt/internal/omdb"
	"cinedikt/internal/tmdb"
	"cinedikt/internal/trailer"
)

// Runner keeps a catalog up to date: it imports one if there is none,
// checks hourly for a new generation, and fills in posters, synopses,
// trailers and people's photos continuously beside both. It also keeps
// Cinedikt Daily's puzzles picked, from UTC yesterday to eight days
// ahead.
//
// The API runs it beside the server, so starting the app on an empty
// database leaves a working map rather than a 503.
type Runner struct {
	// DatabaseURL and MaxConns open the pool the jobs write through. It
	// is theirs alone, so a bulk load never takes a connection a reader
	// is waiting for. The lease is held on a connection of its own to the
	// same database.
	DatabaseURL string
	MaxConns    int32
	Logger      *slog.Logger
	// OMDbKey enables posters, release dates and synopses. Empty leaves
	// the catalog working, without pictures.
	OMDbKey       string
	BackfillRate  float64
	PosterWorkers int
	// SynopsisSweepMinVotes is how well known a title has to be for its
	// synopsis to be asked for before a reader meets it. Zero sweeps the
	// whole catalog.
	SynopsisSweepMinVotes int
	// TMDbAuth turns on the second-chance poster fetch for titles OMDb
	// has no picture for, and for addresses that have stopped
	// answering, the id matcher, the trailer job and the people job.
	// Empty leaves all of those off.
	TMDbAuth tmdb.Auth
	// TMDbLimiter is the process's one TMDb budget, the same limiter
	// the request-path client waits on. TMDb counts requests per
	// address, so the jobs and a reader's lookups have to add up to one
	// rate between them. Nil gives the jobs a limiter of their own at
	// tmdb.DefaultRatePerSecond, which is right only for a process that
	// makes no other TMDb calls.
	TMDbLimiter *rate.Limiter
	// TMDbSweepMinVotes is how well known a title has to be to be
	// fetched ahead of anybody asking. Zero sweeps the whole catalog.
	TMDbSweepMinVotes int
	// TrailerSweepMinVotes is how well known a film has to be for the
	// trailer job's sweep to reach it. Zero sweeps every film; a film a
	// reader opens is looked up whatever its votes.
	TrailerSweepMinVotes int
	// PeopleSweepMinVotes is how well known a person's best known film a
	// map can show has to be for the people job's sweep to reach them.
	// Zero sweeps everyone; a person on a map a reader opens is looked
	// up whatever their votes.
	PeopleSweepMinVotes int
	// PeopleSweepRate is the pace of the people job's sweep and re-asks,
	// in lookups a second, on top of TMDbLimiter. Zero or less takes
	// PeopleSweepPerSecond.
	PeopleSweepRate float64
	// Notify is told what every job is doing: when an import starts,
	// publishes or fails, when the catalog goes stale, and how the other
	// jobs are getting on. It decides for itself what is worth a sound;
	// the jobs report every fact, and the stale check reports every hour
	// it is stale. Nil leaves all of that in the log.
	Notify notify.Sink
	// Queued is the jobs this process runs on its queue rather than
	// here, that report to Notify too (Queue.Jobs): the GeoIP check, when
	// there are MaxMind credentials (a license key and an account id). The
	// runner names them among its own when it takes the jobs, so the board
	// shows them on rather than off.
	Queued []string
	// DailyLaunch is the date Cinedikt Daily's first puzzle is for, zero
	// for the day the first pass runs (DailyJob.Launch).
	DailyLaunch time.Time

	// store is the jobs' pool, opened by Start.
	store *Store
}

// Start runs the whole cycle until ctx is done. It returns as soon as
// the pool is open; everything else happens behind it, so a caller that
// also serves requests can answer "still being built" while the first
// import runs.
//
// Nothing starts until this process holds the lease, and everything
// stops if it loses it. Two runners would call OMDb and TMDb twice over
// and hand each other the same page.
func (r *Runner) Start(ctx context.Context) error {
	if r.DatabaseURL == "" {
		return errors.New("catalog: the runner needs a DatabaseURL")
	}
	store, err := OpenForJobs(ctx, r.DatabaseURL, r.MaxConns)
	if err != nil {
		return fmt.Errorf("catalog: open the runner's pool: %w", err)
	}
	r.store = store
	r.Logger.Info("the catalog jobs have their own pool", "max_conns", r.MaxConns)
	go func() {
		defer store.Close()
		HoldLease(ctx, r.DatabaseURL, r.Logger, r.Notify, r.run)
	}()
	return nil
}

// run is everything the runner does while it holds the lease. It
// returns when ctx is cancelled, which is either shutdown or the lease
// being lost.
func (r *Runner) run(ctx context.Context, wakes *Wakes) {
	// The notifier owns the board from here, and reads back what it had
	// already said, so this process does not say it again.
	notify.Attach(r.Notify, r.store)
	im, posters, synopses := r.build()
	client := r.tmdbClient()
	enabled := []string{notify.JobImport, notify.JobColours, notify.JobDaily}
	if posters != nil {
		enabled = append(enabled, notify.JobPosters, notify.JobSynopses)
	}
	if client != nil {
		enabled = append(enabled, notify.JobTMDbPosters, notify.JobTMDbIDs, notify.JobTrailers, notify.JobPeople)
	}
	enabled = append(enabled, r.Queued...)
	report(r.Notify, notify.Event{Job: notify.JobSystem, Kind: notify.TookOver, Jobs: enabled})
	var wg sync.WaitGroup
	start := func(loop func()) {
		wg.Add(1)
		go func() {
			defer wg.Done()
			loop()
		}()
	}
	keep := func(loop jobLoop) { start(func() { r.keepRunning(ctx, loop) }) }

	if posters != nil {
		// Deliberately not part of an import: three-quarters of a million
		// lookups take longer than the gap between generations, so tying
		// the two together would leave the catalog permanently a day
		// behind its own pictures.
		keep(jobLoop{name: "poster backfill", job: notify.JobPosters, rest: PosterRest,
			run:  func(ctx context.Context) error { return posters.Run(ctx, Live) },
			wake: wakes.Published})
	}
	if synopses != nil {
		// A reader meeting a film OMDb has not answered for is new work;
		// the rest interval covers a wake sent while nobody was
		// listening, and brings the queue's refill.
		keep(jobLoop{name: "synopses", job: notify.JobSynopses, rest: SynopsisRest,
			run: synopses.Run, wake: wakes.Synopses})
	}
	// Whatever was learned from TMDb goes before its six months are up,
	// whether or not the jobs that re-ask it are running or succeeding.
	overviewDays := tmdbForgetDays
	if synopses == nil {
		// Nothing asks OMDb to replace a TMDb overview, so each is
		// dropped the day it comes due instead.
		overviewDays = tmdbRefreshDays
	}
	start(func() { forgetTMDbDataWhenDue(ctx, r.store, r.Logger, overviewDays) })
	// One client between the TMDb jobs, waiting on the process's one
	// limiter, so the jobs and a reader's lookups share one budget.
	if client != nil {
		// The poster fallback runs beside the OMDb backfill rather than
		// inside it: the two answer to different rate limits, and
		// chaining them would drop the faster one to the pace of the
		// slower. A reader who opens a film with no picture is the best
		// reason there is to ask TMDb about it, and should not have to
		// wait out a rest for the asking.
		tmdbPosters := &TMDbJob{
			Store:    r.store,
			Client:   client,
			Logger:   r.Logger.With("job", "tmdb-posters"),
			MinVotes: r.TMDbSweepMinVotes,
			Notify:   r.Notify,
		}
		keep(jobLoop{name: "tmdb posters", job: notify.JobTMDbPosters, rest: TMDbRest,
			run: tmdbPosters.Run, wake: wakes.Wanted})
		// A new generation is the only thing that brings the matcher new
		// titles. Its own channel, not the poster backfill's: sharing one
		// would give a publish to whichever of the two took it first.
		ids := &TMDbIDJob{
			Store:  r.store,
			Client: client,
			Logger: r.Logger.With("job", "tmdb-ids"),
			Notify: r.Notify,
		}
		keep(jobLoop{name: "tmdb ids", job: notify.JobTMDbIDs, rest: TMDbRest,
			run: ids.Run, wake: wakes.PublishedIDs})
		trailers := &TrailerJob{
			Store: r.store,
			TMDb:  client,
			// The only thing in the process that asks YouTube, so
			// its limiter is the whole of that budget.
			Check:    trailer.NewOEmbed(),
			Logger:   r.Logger.With("job", "trailers"),
			MinVotes: r.TrailerSweepMinVotes,
			Wanted:   wakes.TrailersWanted,
			Notify:   r.Notify,
		}
		keep(trailerLoop(trailers, wakes))
		sweepRate := r.PeopleSweepRate
		if sweepRate <= 0 {
			sweepRate = PeopleSweepPerSecond
		}
		people := &PersonPhotoJob{
			Store:    r.store,
			TMDb:     client,
			Logger:   r.Logger.With("job", "people-photos"),
			MinVotes: r.PeopleSweepMinVotes,
			// A pace of the sweep's own, on top of the limiter the
			// client waits on, so the rest of that budget stays free
			// for what readers are waiting on.
			Sweep:  rate.NewLimiter(rate.Limit(sweepRate), 1),
			Wanted: wakes.PeopleWanted,
			Notify: r.Notify,
		}
		keep(peopleLoop(people, wakes))
	} else {
		r.Logger.Info("no TMDb credentials; movies OMDb has no poster for will have none, an empty search stays empty, and no trailers or people's photos are found")
	}
	// And the colours the opening screen fills its frames with. It
	// needs no credentials — the posters are public — so it runs
	// wherever the catalog does. A poster landing for a film on the
	// opening screen is the only thing that makes new work here, and
	// the wake carries at most one pending run — the backfill stores
	// five hundred a second and none of them wants its own pass.
	colours := &ColourJob{
		Store:  r.store,
		Logger: r.Logger.With("job", "opening-colours"),
		Notify: r.Notify,
	}
	keep(jobLoop{name: "opening screen colours", job: notify.JobColours, rest: ColourRest,
		run:  func(ctx context.Context) error { return colours.Run(ctx, Live) },
		wake: wakes.Ready})
	// And Cinedikt Daily's puzzles, for every reader's date and a week
	// past it. It needs nothing but the catalog, so it runs wherever the
	// catalog does. Every pass is a read once the days are picked; the
	// one after midnight UTC picks the new last day, and a new generation
	// wakes it in case a day it could not pick for has an answer now.
	puzzles := &DailyJob{Store: r.store, Logger: r.Logger.With("job", "daily-puzzles"), Launch: r.DailyLaunch}
	keep(jobLoop{name: "daily puzzles", job: notify.JobDaily, rest: DailyRest,
		run: puzzles.Run, wake: wakes.Daily, checked: puzzles.Stats})
	start(func() {
		// The files are rebuilt once a day. The hourly check is not
		// about catching the moment they land; it is about not waiting
		// most of a day after they have. The first attempt is now,
		// which is what imports an empty catalog on startup.
		r.attempt(ctx, im)
		ticker := time.NewTicker(PollInterval)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				r.attempt(ctx, im)
			}
		}
	})
	wg.Wait()
	report(r.Notify, notify.Event{Job: notify.JobSystem, Kind: notify.Stopped})
}

// reportRun tells the notifier how one run of a background job ended:
// a failure, or a check that clears one. next is when it will run
// again. A run cut short by ctx says nothing; being stopped is not news.
func reportRun(ctx context.Context, sink notify.Sink, job string, err error, next time.Time,
	checked func(context.Context, *notify.Event)) {
	if ctx.Err() != nil {
		return
	}
	if err != nil {
		report(sink, failure(job, err, next, time.Time{}))
		return
	}
	e := notify.Event{Job: job, Kind: notify.Checked}
	if checked != nil && sink != nil {
		checked(ctx, &e)
	}
	report(sink, e)
}

// build makes the import and the two OMDb jobs. The poster pass and the
// synopsis job are handed one client, the same value, so they share its
// rate limit and its daily-quota pause: when OMDb says the day's
// requests are spent, both stop until it resets. Without an OMDb key
// both are nil.
func (r *Runner) build() (*Importer, *PosterJob, *SynopsisJob) {
	im := &Importer{
		Store: r.store,
		// Two files are most of a gigabyte; the per-file deadline lives
		// in the downloader, so this client has none of its own.
		Client: &http.Client{},
		Dir:    os.TempDir() + "/cinedikt-catalog",
		Logger: r.Logger,
		Notify: r.Notify,
	}
	if r.OMDbKey == "" {
		r.Logger.Warn("OMDB_API_KEY is not set; posters, release dates and synopses will be missing")
		return im, nil, nil
	}
	workers := r.PosterWorkers
	if workers <= 0 {
		workers = DefaultPosterWorkers
	}
	client := omdb.New(r.OMDbKey,
		omdb.WithRateLimit(r.BackfillRate, int(r.BackfillRate)),
		omdb.WithHTTPTimeout(15*time.Second),
		omdb.WithConnections(workers))
	posters := &PosterJob{
		Store:   r.store,
		Client:  client,
		Logger:  r.Logger,
		Batch:   DefaultPosterBatch,
		Workers: workers,
		Notify:  r.Notify,
	}
	synopses := &SynopsisJob{
		Store:  r.store,
		Client: client,
		// `job`, not `component`: the runner's logger already carries a
		// component, and a second one makes two keys of the same name in
		// every JSON line this writes. A strict reader keeps one of them.
		// Every job's logger is named the same way.
		Logger:   r.Logger.With("job", "synopses"),
		MinVotes: r.SynopsisSweepMinVotes,
		Notify:   r.Notify,
	}
	return im, posters, synopses
}

// tmdbClient is the one TMDb client the runner's jobs share, waiting on
// the process's limiter. Nil when there are no credentials: the TMDb
// jobs then have nothing to do.
func (r *Runner) tmdbClient() *tmdb.Client {
	if r.TMDbAuth.APIKey == "" && r.TMDbAuth.AccessToken == "" {
		return nil
	}
	return tmdb.New(r.TMDbAuth, tmdb.WithLimiter(r.TMDbLimiter))
}

func (r *Runner) attempt(ctx context.Context, im *Importer) {
	began := time.Now()
	next := began.Add(PollInterval)
	out, err := im.RunOnce(ctx)
	if err != nil && (ctx.Err() != nil || errors.Is(err, context.Canceled)) {
		// Shutdown, or the lease going to another process. Nothing
		// failed: the next holder starts the import again.
		r.Logger.Info("import stopped", "err", err)
		return
	}
	if err != nil {
		// A failed hour is not a crisis, but a run of them means the
		// catalog is going stale. The notifier decides when a run of
		// them is worth a sound; this reports every one.
		r.Logger.Error("import failed", "err", err)
		report(r.Notify, failure(notify.JobImport, err, next, out.LiveSince))
		r.warnIfStale(ctx)
		return
	}
	if !out.Ran {
		// Most hours are this one. It still reaches the notifier, which
		// is how the board knows the hourly check is alive.
		r.Logger.Info("no import this hour", "reason", out.Reason)
		switch out.Skip {
		case SkipMoved:
			report(r.Notify, notify.Event{Job: notify.JobImport, Kind: notify.Skipped,
				Cause: notify.FileMoved, NextTry: next, LiveSince: out.LiveSince})
		default:
			e := notify.Event{Job: notify.JobImport, Kind: notify.Checked,
				NextTry: next, LiveSince: out.LiveSince, Films: out.PrevFilms}
			if out.Skip == SkipLocked {
				e.Cause = notify.Locked
			}
			report(r.Notify, e)
		}
		r.warnIfStale(ctx)
		return
	}
	r.Logger.Info("import published",
		"titles", out.Counts.Titles,
		"names", out.Counts.Names,
		"principals", out.Counts.Principals,
		"directors", out.Counts.Directors,
		"ratings", out.Counts.Ratings,
		"integrity", out.Integrity,
		"took", out.Took.Round(time.Second))
	report(r.Notify, notify.Event{Job: notify.JobImport, Kind: notify.Published,
		Films: out.Counts.Titles, People: out.Counts.Names,
		PrevFilms: out.PrevFilms, PrevAt: out.PrevAt, Took: out.Took,
		LiveSince: time.Now(), NextTry: next})
	if n, err := r.store.ForgetUnknownPosters(ctx); err != nil {
		r.Logger.Warn("forget withdrawn posters", "err", err)
	} else if n > 0 {
		r.Logger.Info("forgot posters for withdrawn titles", "rows", n)
	}
	r.warnIfStale(ctx)
}

// warnIfStale says so when the published catalog is old. It is a log
// line and an event, never a health check: a stale catalog serves
// perfectly well, and failing a health check on it would turn a late
// upstream publish into a failed deploy.
//
// It reports every hour the catalog is stale. Saying it once is the
// notifier's job, and it remembers across restarts, which a field on
// the runner could not.
func (r *Runner) warnIfStale(ctx context.Context) {
	now := time.Now()
	stale, built, err := r.store.Stale(ctx, now)
	if err != nil {
		r.Logger.Warn("read generation", "err", err)
		return
	}
	if !stale {
		return
	}
	var age time.Duration
	if !built.IsZero() {
		age = now.Sub(built)
	}
	r.Logger.Warn("catalog is stale", "age", age.Round(time.Minute), "after", StaleAfter)
	// The time as the database has it, not now less the age: that would
	// carry this moment's monotonic clock reading, and no two hours would
	// then name quite the same catalog.
	report(r.Notify, notify.Event{Job: notify.JobImport, Kind: notify.Stale, LiveSince: built})
}

// PosterRest is how long the backfill waits after catching up, or after
// being told the key is spent, before looking for work again.
const PosterRest = 20 * time.Minute

// catalogPoll is how often a job looks while there is no catalog to work
// on yet. On a first start the import is running and will finish in
// minutes; resting the full period would leave the jobs asleep for most
// of the time they could have been working.
const catalogPoll = 15 * time.Second

// jobLoop is one of the background jobs keepRunning keeps going.
type jobLoop struct {
	// name is what the log calls the job, and job what the notifier
	// calls it.
	name string
	job  string
	// rest is how long it waits after a pass before looking again.
	rest time.Duration
	// run is one pass.
	run func(context.Context) error
	// wake cuts a rest short: whatever signal means there is new work.
	wake <-chan struct{}
	// wanted, for the jobs a reader waits on, is a second wake: a
	// reader's mark, where wake is a new generation. A pass already
	// running hears it for itself.
	wanted <-chan struct{}
	// refill, when set, is called when wake ends a rest, so the next
	// pass refills its queue from the new generation. A reader's mark
	// brings no new names to queue, so it does not call it.
	refill func()
	// checked, when set, fills in the Checked event a pass that went
	// well reports, with whatever the job has to tell the board: the
	// Daily's figures for the day before. Only a pass that went well,
	// since a Checked is what clears a failure, and a second one sent
	// on its own would read as a recovery.
	checked func(context.Context, *notify.Event)
}

// keepRunning runs a job for as long as the process does: nothing until
// a catalog has been published — on a first start that is a few minutes
// away, so the wait is short — then a pass, then a rest a wake can cut
// short.
func (r *Runner) keepRunning(ctx context.Context, loop jobLoop) {
	waited := false
	for {
		ready, err := r.store.LiveReady(ctx)
		if err != nil && ctx.Err() == nil {
			r.Logger.Warn(loop.name+": readiness", "err", err)
		}
		wait := loop.rest
		if err != nil || !ready {
			if !waited {
				r.Logger.Info(loop.name + " waiting for a catalog")
				waited = true
			}
			wait = catalogPoll
		} else {
			waited = false
			err := loop.run(ctx)
			if err != nil && ctx.Err() == nil {
				r.Logger.Warn(loop.name, "err", err)
			}
			reportRun(ctx, r.Notify, loop.job, err, time.Now().Add(wait), loop.checked)
		}
		woke, byWake := waitForEither(ctx, loop.wake, loop.wanted, wait)
		if !woke {
			return
		}
		if byWake && loop.refill != nil {
			loop.refill()
		}
	}
}
