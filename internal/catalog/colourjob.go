package catalog

// Filling in the colours the opening screen draws before its pictures.
//
// The work is bounded by design: the cold screen only ever shows films
// from the first-run pool, which is two thousand rows, so that is the
// whole of what needs a colour. Colouring the other three-quarters of a
// million would be downloading three-quarters of a million images to
// fill frames nobody will see.

import (
	"context"
	"log/slog"
	"net/http"
	"time"

	"cinedikt/internal/notify"
)

// ColourBatch is how many are claimed per round, and ColourRest is how
// long the job waits once the pool is covered. A generation brings new
// films into the pool, so it looks again rather than stopping.
const (
	ColourBatch = 100
	ColourRest  = 30 * time.Minute
)

// ColourFetch bounds one poster download. A slow image host holds up
// nothing here — the row simply stays uncoloured until the next round.
const ColourFetch = 10 * time.Second

// ColourJob gives every film the opening screen might show a colour.
type ColourJob struct {
	Store  *Store
	Logger *slog.Logger
	Client *http.Client
	// Batch is how many are claimed per round; zero takes ColourBatch.
	Batch int
	// Notify hears when a pass starts, how far it has got, and how it
	// ends. Nil leaves that in the log.
	Notify notify.Sink
}

// Run colours what it can before ctx is done.
func (j *ColourJob) Run(ctx context.Context, schema string) error {
	batch := j.Batch
	if batch <= 0 {
		batch = ColourBatch
	}
	client := j.Client
	if client == nil {
		client = &http.Client{Timeout: ColourFetch}
	}
	outstanding, err := j.Store.uncolouredCount(ctx, schema)
	if stopping(err) {
		return nil
	}
	if err != nil {
		return err
	}
	if outstanding == 0 {
		return nil
	}
	track := newProgress(j.Logger, "colouring the opening screen", outstanding)
	track.watch(j.Notify, notify.Event{Job: notify.JobColours})
	run := pass{sink: j.Notify, job: notify.JobColours}

	var done, failed int64
	var last error
	// A poster that will not load leaves its row uncoloured, which
	// would hand it back on the next query for ever. Once per pass.
	tried := make(map[string]bool)
	for {
		if ctx.Err() != nil {
			return nil
		}
		rows, err := j.Store.uncoloured(ctx, schema, batch)
		if stopping(err) {
			return nil
		}
		if err != nil {
			return err
		}
		fresh := rows[:0:0]
		for _, r := range rows {
			if !tried[r.tconst] {
				fresh = append(fresh, r)
			}
		}
		if len(fresh) == 0 {
			track.done(done + failed)
			j.Logger.Info("opening screen coloured", "filled", done, "failed", failed)
			// Not one poster loading is the image hosts, or the way to
			// them, rather than a batch of bad addresses.
			if failed >= failedLookups && done == 0 {
				return &LookupsFailedError{Provider: "poster hosts", Count: failed, Last: last}
			}
			run.finish(done, 0, failed)
			return nil
		}
		run.start(outstanding)
		for _, r := range fresh {
			if ctx.Err() != nil {
				return nil
			}
			tried[r.tconst] = true
			// The size the frame needs, then the address that was stored.
			// An edge will 404 one rendition for a few minutes and keep
			// serving the other, and the colour only needs one of them.
			target := PosterAt(r.url, colourWidth)
			hex, err := PosterColour(ctx, client, target)
			if err != nil && target != r.url && ctx.Err() == nil {
				hex, err = PosterColour(ctx, client, r.url)
			}
			if err != nil {
				if stopping(err) {
					return nil
				}
				failed++
				last = err
				j.Logger.Warn("opening colour", "tconst", r.tconst, "err", err)
				track.step(done + failed)
				continue
			}
			if err := j.Store.saveColour(ctx, r.tconst, hex); err != nil {
				if stopping(err) {
					return nil
				}
				j.Logger.Warn("opening screen colour: save", "tconst", r.tconst, "err", err)
				continue
			}
			done++
			track.step(done + failed)
		}
	}
}

// colourWidth is the size the poster is fetched at to be averaged. The
// answer is one pixel either way, so this is the smallest file that
// still represents the whole picture.
const colourWidth = 185

// uncolouredRow is one film the opening screen may show that has no
// colour yet.
type uncolouredRow struct {
	tconst string
	url    string
}

func (s *Store) uncoloured(ctx context.Context, schema string, limit int) ([]uncolouredRow, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT f.tconst, p.poster_url
		FROM `+schema+`.first_run f
		JOIN meta.posters p USING (tconst)
		WHERE p.colour IS NULL
		  AND p.poster_url IS NOT NULL AND btrim(p.poster_url) <> ''
		ORDER BY f.num_votes DESC
		LIMIT $1`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []uncolouredRow
	for rows.Next() {
		var r uncolouredRow
		if err := rows.Scan(&r.tconst, &r.url); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

func (s *Store) uncolouredCount(ctx context.Context, schema string) (int64, error) {
	var n int64
	err := s.pool.QueryRow(ctx, `
		SELECT count(*)
		FROM `+schema+`.first_run f
		JOIN meta.posters p USING (tconst)
		WHERE p.colour IS NULL
		  AND p.poster_url IS NOT NULL AND btrim(p.poster_url) <> ''`).Scan(&n)
	return n, err
}

func (s *Store) saveColour(ctx context.Context, tconst, hex string) error {
	_, err := s.pool.Exec(ctx,
		`UPDATE meta.posters SET colour = $2 WHERE tconst = $1`, tconst, hex)
	return err
}

// fillColours keeps the opening screen's colours filled in for as long
// as the process runs, beside the other two poster jobs.
func fillColours(ctx context.Context, job *ColourJob, logger *slog.Logger, wakes *Wakes) {
	for {
		ready, err := job.Store.LiveReady(ctx)
		wait := ColourRest
		if err != nil || !ready {
			wait = PosterWaitForCatalog
		} else {
			err := job.Run(ctx, Live)
			if err != nil && ctx.Err() == nil {
				logger.Warn("opening screen colours", "err", err)
			}
			reportRun(ctx, job.Notify, notify.JobColours, err, time.Now().Add(wait))
		}
		// A poster landing for a film on the opening screen is the
		// only thing that makes new work here, and the wake carries at
		// most one pending run — the backfill stores five hundred a
		// second and none of them wants its own pass.
		if !waitFor(ctx, wakes.Ready, wait) {
			return
		}
	}
}
