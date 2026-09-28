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
// checks hourly for a new generation, and fills in posters, synopses and
// trailers continuously beside both.
//
// It is the whole of what the importer does, in one place, because the
// API can run it too. Starting the app on an empty database should
// leave you with a working map rather than a 503 and a second command
// to find.
type Runner struct {
	// Store is the pool the jobs write through. Leave it nil and set
	// DatabaseURL to have Start open one of its own — which is what
	// the API does, so a bulk load never takes a connection a reader
	// is waiting for.
	Store *Store
	// DatabaseURL and MaxConns open that pool. Ignored when Store is
	// already set, which is how cmd/importer supplies its own.
	DatabaseURL string
	MaxConns    int32
	Logger      *slog.Logger
	// Dir is where the downloads are kept. Empty means a temp directory.
	Dir string
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
	// answering, the id matcher and the trailer job. Empty leaves all
	// of those off.
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
	// TrailerSweepMinVotes is how well known a film has to be for its
	// trailer to be looked up before anybody opens it.
	TrailerSweepMinVotes int
	// OEmbed is the YouTube check a trailer has to pass, shared with the
	// endpoint so the two keep to one budget. Nil builds one for the
	// runner alone.
	OEmbed trailer.Checker
	// Keep leaves the downloaded files on disk, for development.
	Keep bool
	// Notify is told what every job is doing: when an import starts,
	// publishes or fails, when the catalog goes stale, and how the other
	// jobs are getting on. It decides for itself what is worth a sound;
	// the jobs report every fact, and the stale check reports every hour
	// it is stale. Nil leaves all of that in the log.
	Notify notify.Sink
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
	own := false
	if r.Store == nil {
		if r.DatabaseURL == "" {
			return errors.New("catalog: the runner needs a Store or a DatabaseURL")
		}
		store, err := OpenForJobs(ctx, r.DatabaseURL, r.MaxConns)
		if err != nil {
			return fmt.Errorf("catalog: open the runner's pool: %w", err)
		}
		r.Store = store
		own = true
		r.Logger.Info("the catalog jobs have their own pool", "max_conns", r.MaxConns)
	}
	go func() {
		if own {
			defer r.Store.Close()
		}
		HoldLease(ctx, r.leaseURL(), r.Logger, r.Notify, r.run)
	}()
	return nil
}

// leaseURL is where the lease connection goes. It is the same database
// the pool uses; a runner given a ready-made Store is told the URL the
// same way.
func (r *Runner) leaseURL() string { return r.DatabaseURL }

// run is everything the runner does while it holds the lease. It
// returns when ctx is cancelled, which is either shutdown or the lease
// being lost.
func (r *Runner) run(ctx context.Context, wakes *Wakes) {
	// The notifier owns the board from here, and reads back what it had
	// already said, so this process does not say it again.
	notify.Attach(r.Notify, r.Store)
	im, posters, synopses := r.build()
	client := r.tmdbClient()
	enabled := []string{notify.JobImport, notify.JobColours}
	if posters != nil {
		enabled = append(enabled, notify.JobPosters, notify.JobSynopses)
	}
	if client != nil {
		enabled = append(enabled, notify.JobTMDbPosters, notify.JobTMDbIDs, notify.JobTrailers)
	}
	report(r.Notify, notify.Event{Job: notify.JobSystem, Kind: notify.TookOver, Jobs: enabled})
	var wg sync.WaitGroup
	start := func(loop func()) {
		wg.Add(1)
		go func() {
			defer wg.Done()
			loop()
		}()
	}

	if posters != nil {
		start(func() { fillPosters(ctx, posters, r.Logger, wakes) })
	}
	if synopses != nil {
		start(func() { fillSynopses(ctx, synopses, r.Logger, wakes) })
	}
	// Whatever was learned from TMDb goes before its six months are up,
	// whether or not the jobs that re-ask it are running or succeeding.
	overviewDays := tmdbForgetDays
	if synopses == nil {
		// Nothing asks OMDb to replace a TMDb overview, so each is
		// dropped the day it comes due instead.
		overviewDays = tmdbRefreshDays
	}
	start(func() { forgetTMDbDataWhenDue(ctx, r.Store, r.Logger, overviewDays) })
	// One client between the TMDb jobs, waiting on the process's one
	// limiter, so the jobs and a reader's lookups share one budget.
	if client != nil {
		start(func() {
			fillFromTMDb(ctx, &TMDbJob{
				Store:    r.Store,
				Client:   client,
				Logger:   r.Logger.With("job", "tmdb-posters"),
				MinVotes: r.TMDbSweepMinVotes,
				Notify:   r.Notify,
			}, r.Logger, wakes)
		})
		start(func() {
			fillTMDbIDs(ctx, &TMDbIDJob{
				Store:  r.Store,
				Client: client,
				Logger: r.Logger.With("job", "tmdb-ids"),
				Notify: r.Notify,
			}, r.Logger, wakes)
		})
		start(func() {
			fillTrailers(ctx, &TrailerJob{
				Store:    r.Store,
				Videos:   client,
				Check:    r.oembed(),
				Logger:   r.Logger.With("job", "trailers"),
				MinVotes: r.TrailerSweepMinVotes,
				Notify:   r.Notify,
			}, r.Logger, wakes)
		})
	} else {
		r.Logger.Info("no TMDb credentials; movies OMDb has no poster for will have none, an empty search stays empty, and no trailers are found ahead of readers")
	}
	// And the colours the opening screen fills its frames with. It
	// needs no credentials — the posters are public — so it runs
	// wherever the catalog does.
	start(func() {
		fillColours(ctx, &ColourJob{
			Store:  r.Store,
			Logger: r.Logger.With("job", "opening-colours"),
			Notify: r.Notify,
		}, r.Logger, wakes)
	})
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

// Once runs a single attempt and reports whether it ended well. A run
// that decided not to import is a success: most hours are.
func (r *Runner) Once(ctx context.Context) bool {
	im, _, _ := r.build()
	return r.attempt(ctx, im)
}

// Posters fills in what it can and returns. Nothing else runs.
func (r *Runner) Posters(ctx context.Context) error {
	_, job, _ := r.build()
	if job == nil {
		return nil
	}
	err := job.Run(ctx, Live)
	reportRun(ctx, r.Notify, notify.JobPosters, err, time.Time{})
	return err
}

// reportRun tells the notifier how one run of a background job ended:
// a failure, or a check that clears one. next is when it will run
// again. A run cut short by ctx says nothing; being stopped is not news.
func reportRun(ctx context.Context, sink notify.Sink, job string, err error, next time.Time) {
	if ctx.Err() != nil {
		return
	}
	if err != nil {
		report(sink, failure(job, err, next, time.Time{}))
		return
	}
	report(sink, notify.Event{Job: job, Kind: notify.Checked})
}

// build makes the import and the two OMDb jobs. The poster pass and the
// synopsis job are handed one client, the same value, so they share its
// rate limit and its daily-quota pause: when OMDb says the day's
// requests are spent, both stop until it resets. Without an OMDb key
// both are nil.
func (r *Runner) build() (*Importer, *PosterJob, *SynopsisJob) {
	dir := r.Dir
	if dir == "" {
		dir = os.TempDir() + "/cinedikt-catalog"
	}
	im := &Importer{
		Store: r.Store,
		// Two files are most of a gigabyte; the per-file deadline lives
		// in the downloader, so this client has none of its own.
		Client: &http.Client{},
		Dir:    dir,
		Logger: r.Logger,
		Keep:   r.Keep,
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
		Store:   r.Store,
		Client:  client,
		Logger:  r.Logger,
		Batch:   DefaultPosterBatch,
		Workers: workers,
		Notify:  r.Notify,
	}
	synopses := &SynopsisJob{
		Store:    r.Store,
		Client:   client,
		Logger:   r.Logger.With("job", "synopses"),
		MinVotes: r.SynopsisSweepMinVotes,
		Notify:   r.Notify,
	}
	return im, posters, synopses
}

// TMDbPosters fills in what OMDb could not and returns. Nothing else
// runs, and no credentials means nothing to do.
func (r *Runner) TMDbPosters(ctx context.Context) error {
	job := r.buildTMDb()
	if job == nil {
		return nil
	}
	err := job.Run(ctx)
	reportRun(ctx, r.Notify, notify.JobTMDbPosters, err, time.Time{})
	return err
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

// oembed is the YouTube check the trailer job uses: the one the process
// shares, or one of the runner's own.
func (r *Runner) oembed() trailer.Checker {
	if r.OEmbed != nil {
		return r.OEmbed
	}
	return trailer.NewOEmbed()
}

// buildTMDb is the poster fallback, or nil when there are no TMDb
// credentials to use.
func (r *Runner) buildTMDb() *TMDbJob {
	client := r.tmdbClient()
	if client == nil {
		r.Logger.Info("no TMDb credentials; movies OMDb has no poster for will have none")
		return nil
	}
	return &TMDbJob{
		Store:  r.Store,
		Client: client,
		// `job`, not `component`: the runner's logger already carries a
		// component, and a second one makes two keys of the same name in
		// every JSON line this writes. A strict reader keeps one of them.
		Logger:   r.Logger.With("job", "tmdb-posters"),
		MinVotes: r.TMDbSweepMinVotes,
		Notify:   r.Notify,
	}
}

func (r *Runner) attempt(ctx context.Context, im *Importer) bool {
	began := time.Now()
	next := began.Add(PollInterval)
	out, err := im.RunOnce(ctx)
	if err != nil && (ctx.Err() != nil || errors.Is(err, context.Canceled)) {
		// Shutdown, or the lease going to another process. Nothing
		// failed: the next holder starts the import again.
		r.Logger.Info("import stopped", "err", err)
		return false
	}
	if err != nil {
		// A failed hour is not a crisis, but a run of them means the
		// catalog is going stale. The notifier decides when a run of
		// them is worth a sound; this reports every one.
		r.Logger.Error("import failed", "err", err)
		report(r.Notify, failure(notify.JobImport, err, next, out.LiveSince))
		r.warnIfStale(ctx)
		return false
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
		return true
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
	if n, err := r.Store.ForgetUnknownPosters(ctx); err != nil {
		r.Logger.Warn("forget withdrawn posters", "err", err)
	} else if n > 0 {
		r.Logger.Info("forgot posters for withdrawn titles", "rows", n)
	}
	r.warnIfStale(ctx)
	return true
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
	stale, built, err := r.Store.Stale(ctx, now)
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

// PosterWaitForCatalog is how often it looks while there is no catalog
// to fill in yet. On a first start the import is running and will finish
// in minutes; resting the full period would leave the backfill asleep
// for most of the time it could have been working.
const PosterWaitForCatalog = 15 * time.Second

// fillPosters keeps meta.posters filled for as long as the process runs.
// It is deliberately not part of an import: three-quarters of a million
// lookups take longer than the gap between generations, so tying the two
// together would leave the catalog permanently a day behind its own
// pictures.
func fillPosters(ctx context.Context, job *PosterJob, logger *slog.Logger, wakes *Wakes) {
	waited := false
	for {
		// Nothing to fill until a catalog has been published. On a first
		// start that is a few minutes away, so the wait is short.
		ready, err := job.Store.LiveReady(ctx)
		if err != nil {
			logger.Warn("poster backfill: readiness", "err", err)
		}
		wait := PosterRest
		switch {
		case err != nil || !ready:
			if !waited {
				logger.Info("poster backfill waiting for a catalog")
				waited = true
			}
			wait = PosterWaitForCatalog
		default:
			waited = false
			err := job.Run(ctx, Live)
			if err != nil && ctx.Err() == nil {
				logger.Warn("poster backfill", "err", err)
			}
			reportRun(ctx, job.Notify, notify.JobPosters, err, time.Now().Add(wait))
		}
		if !waitFor(ctx, wakes.Published, wait) {
			return
		}
	}
}
