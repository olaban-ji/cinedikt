package catalog

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"time"

	"cinedikt/internal/notify"
)

// importLockKey is the advisory lock the whole attempt is held under.
// Two importers must not run: they would write the same staging schema.
// The loser exits rather than waiting, because the winner is about to do
// the work anyway.
const importLockKey int64 = 0x6369_6e65 // "cine"

// Importer runs one generation from the dataset host into the catalog.
type Importer struct {
	Store  *Store
	Client *http.Client
	Dir    string
	Logger *slog.Logger

	// Notify hears the start and each step. Nil means the log is the
	// only record, which is every run that has no chat configured.
	Notify notify.Sink
}

// SkipReason is why an attempt that did not fail did not import either.
type SkipReason int

const (
	SkipNone SkipReason = iota
	// SkipLocked is another importer holding the import lock.
	SkipLocked
	// SkipNotReady is IMDb not having published a whole new set yet.
	SkipNotReady
	// SkipMoved is a file changing while the set was being fetched.
	SkipMoved
)

// Outcome says what an attempt did, for the log and for the alert.
type Outcome struct {
	Ran       bool
	Reason    string
	Skip      SkipReason
	Counts    Counts
	Integrity float64
	Took      time.Duration
	// LiveSince is when the catalog that was live as the attempt began
	// was built, and PrevFilms how many films it held. PrevAt is the
	// same moment as LiveSince, named for what it is once a new
	// catalog has replaced it. All three are zero before a first
	// publish.
	LiveSince time.Time
	PrevFilms int64
	PrevAt    time.Time
}

// RunOnce is one hour's attempt. It reads the five stamps, and runs a
// whole import only when all five have moved past what was published.
//
// Every way of stopping leaves the live catalog exactly as it was.
func (im *Importer) RunOnce(ctx context.Context) (Outcome, error) {
	started := time.Now()

	// The lock is taken before the HEADs so two importers do not both
	// spend a request deciding the same thing.
	conn, err := im.Store.pool.Acquire(ctx)
	if err != nil {
		return Outcome{}, fmt.Errorf("catalog: acquire connection: %w", err)
	}
	defer conn.Release()
	var got bool
	if err := conn.QueryRow(ctx, `SELECT pg_try_advisory_lock($1)`, importLockKey).Scan(&got); err != nil {
		return Outcome{}, fmt.Errorf("catalog: take import lock: %w", err)
	}
	if !got {
		return Outcome{Reason: "another importer is running", Skip: SkipLocked}, nil
	}
	defer func() {
		_, _ = conn.Exec(context.WithoutCancel(ctx), `SELECT pg_advisory_unlock($1)`, importLockKey)
	}()

	published, at, err := im.Store.Published(ctx)
	if err != nil {
		return Outcome{}, err
	}
	// What the live catalog is, for every outcome from here: an alert
	// about a failed hour says which catalog the site is still serving.
	live := Outcome{LiveSince: at, PrevAt: at}
	if live.PrevFilms, err = im.Store.PublishedFilms(ctx); err != nil {
		return live, err
	}
	opened, err := Head(ctx, im.Client, Files)
	if err != nil {
		// A HEAD that failed is not a reason to guess. The hour is
		// skipped and the alert is the caller's to raise.
		return live, err
	}
	if ready, why := Ready(published, opened, Files); !ready {
		live.Reason, live.Skip = why, SkipNotReady
		return live, nil
	}

	im.Logger.Info("import starting", "files", len(Files), "step", "1/4 download")
	// With the catalog still live, so a board that knew nothing before
	// this (a first deploy, or a state that could not be read) does not
	// spend the next two hours saying the site has none.
	report(im.Notify, notify.Event{Job: notify.JobImport, Kind: notify.Started,
		Step: 1, LiveSince: live.LiveSince, Films: live.PrevFilms})

	// Last generation's schema goes now rather than at the end of the
	// run that made it, so its readers had the whole gap to finish.
	if err := im.Store.DropRetired(ctx); err != nil {
		return live, err
	}

	paths, err := Download(ctx, im.Client, im.Dir, Files, im.Logger, im.Notify)
	if err != nil {
		return live, err
	}
	defer Discard(paths)

	// The set is read again now everything is on disk. If anything moved
	// while it was being fetched, what we hold is a mixture.
	after, err := Head(ctx, im.Client, Files)
	if err != nil {
		return live, err
	}
	if same, which := Same(opened, after, Files); !same {
		why := fmt.Sprintf("%s moved while the set was being fetched", which)
		live.Reason, live.Skip = why, SkipMoved
		return live, nil
	}

	im.Logger.Info("download complete", "step", "2/4 load")
	counts, integrity, err := im.load(ctx, paths)
	if err != nil {
		return live, err
	}

	if err := im.Store.Publish(ctx, opened, counts); err != nil {
		return live, err
	}
	out := live
	out.Ran, out.Reason, out.Counts = true, "published", counts
	out.Integrity, out.Took = integrity, time.Since(started)
	return out, nil
}

// step tells the notifier which of the four steps the import is on.
func (im *Importer) step(step int, noun string) {
	report(im.Notify, notify.Event{Job: notify.JobImport, Kind: notify.Progress, Step: step, Noun: noun})
}

// load reads the five files into the staging schema, in the order the
// filtering needs: titles first for the allow-list, names last because
// only then is it known who was credited.
func (im *Importer) load(ctx context.Context, paths map[File]string) (Counts, float64, error) {
	if err := im.Store.ResetStaging(ctx); err != nil {
		return Counts{}, 0, err
	}

	im.step(2, "films")
	titles, err := OpenFile(paths[TitleBasics])
	if err != nil {
		return Counts{}, 0, err
	}
	kept, n, err := im.Store.LoadTitles(ctx, im.Logger, titles)
	titles.Close()
	if err != nil {
		return Counts{}, 0, err
	}
	im.Logger.Info("loaded titles", "movies", n)

	credited := make(NConsts, 1<<20)

	im.step(2, "credits")
	principals, err := OpenFile(paths[TitlePrincipals])
	if err != nil {
		return Counts{}, 0, err
	}
	n, err = im.Store.LoadPrincipals(ctx, im.Logger, principals, kept, credited)
	principals.Close()
	if err != nil {
		return Counts{}, 0, err
	}
	im.Logger.Info("loaded principals", "credits", n)

	im.step(2, "directors")
	crew, err := OpenFile(paths[TitleCrew])
	if err != nil {
		return Counts{}, 0, err
	}
	n, err = im.Store.LoadDirectors(ctx, im.Logger, crew, kept, credited)
	crew.Close()
	if err != nil {
		return Counts{}, 0, err
	}
	im.Logger.Info("loaded directors", "credits", n)

	im.step(2, "ratings")
	ratings, err := OpenFile(paths[TitleRatings])
	if err != nil {
		return Counts{}, 0, err
	}
	n, err = im.Store.LoadRatings(ctx, im.Logger, ratings, kept)
	ratings.Close()
	if err != nil {
		return Counts{}, 0, err
	}
	im.Logger.Info("loaded ratings", "rated", n)

	im.step(2, "people")
	names, err := OpenFile(paths[NameBasics])
	if err != nil {
		return Counts{}, 0, err
	}
	n, err = im.Store.LoadNames(ctx, im.Logger, names, credited)
	names.Close()
	if err != nil {
		return Counts{}, 0, err
	}
	im.Logger.Info("loaded names", "people", n)

	im.Logger.Info("building indexes and making the tables durable",
		"step", "3/4 finish", "note", "this takes a minute or two and logs nothing until it is done")
	im.step(3, "")
	if err := im.Store.Finish(ctx, im.Logger); err != nil {
		return Counts{}, 0, err
	}
	counts, err := im.Store.CountStaging(ctx)
	if err != nil {
		return Counts{}, 0, err
	}
	integrity, err := im.Store.Check(ctx, counts)
	if err != nil {
		return counts, integrity, err
	}
	im.Logger.Info("load checked", "step", "4/4 publish", "integrity", fmt.Sprintf("%.4f", integrity))
	im.step(4, "")
	return counts, integrity, nil
}

// Posters are not filled in here. They are learned from an API a title
// at a time, and there are more titles than a generation's gap is long,
// so the job runs continuously beside the import instead — see
// PosterJob. `meta` outlives every swap, so what it learns is kept.

// StaleAfter is when a catalog is old enough to be worth an alert. IMDb
// publishes daily, so this is a day and a half of slack.
//
// It is deliberately not part of the health check: a stale catalog still
// serves perfectly well, and failing a health check on it would turn a
// late upstream publish into a failed deploy.
const StaleAfter = 36 * time.Hour

// Stale reports whether the published catalog is old enough to alert
// on, and when it was built: zero when nothing has been published.
func (s *Store) Stale(ctx context.Context, now time.Time) (bool, time.Time, error) {
	_, at, err := s.Published(ctx)
	if err != nil {
		return false, time.Time{}, err
	}
	if at.IsZero() {
		return true, at, nil
	}
	return now.Sub(at) > StaleAfter, at, nil
}
