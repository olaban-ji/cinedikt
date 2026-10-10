package catalog

// Filling in the colours the opening screen draws before its pictures,
// and the colour Cinedikt Daily fills its hidden card with.
//
// The work is bounded by design. The cold screen only ever shows films
// from the first-run pool, which is two thousand rows, and the Daily's
// answer is only ever a movie with daily.MinVotes votes, about 2,750 in
// October 2026, half of them outside the pool. That is the whole of what
// needs a colour. Colouring the other three-quarters of a million would
// be downloading three-quarters of a million images to fill frames
// nobody will see.
//
// Coloured here, a Daily answer is never coloured when it is picked: the
// pick would have to fetch its poster then, under ColourFetch, and a
// poster host that does not answer held the pick, or a press of Play
// again, for the whole of it.

import (
	"context"
	"log/slog"
	"net/http"
	"slices"
	"time"

	"cinedikt/internal/daily"
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

// ColourJob gives every film the opening screen might show a colour, and
// every movie that could be a Daily answer.
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
	track := newProgress(j.Logger, "colouring posters", outstanding)
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
		fresh := slices.DeleteFunc(rows, func(r uncolouredRow) bool { return tried[r.tconst] })
		if len(fresh) == 0 {
			track.done(done + failed)
			j.Logger.Info("posters coloured", "filled", done, "failed", failed)
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
				j.Logger.Warn("poster colour", "tconst", r.tconst, "err", err)
				track.step(done + failed)
				continue
			}
			if err := j.Store.saveColour(ctx, r.tconst, hex); err != nil {
				if stopping(err) {
					return nil
				}
				j.Logger.Warn("poster colour: save", "tconst", r.tconst, "err", err)
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

// colourWanted is the films in schema that need a colour: the first-run
// pool's, and every movie with at least the votes the placeholder votes
// is given, daily.MinVotes, which could be a Daily answer.
func colourWanted(schema, votes string) string {
	return `
		SELECT tconst FROM ` + schema + `.first_run
		UNION
		SELECT tconst FROM ` + schema + `.ratings WHERE num_votes >= ` + votes
}

// uncolouredRow is one film the opening screen may show, or the Daily
// may pick, that has no colour yet.
type uncolouredRow struct {
	tconst string
	url    string
}

// uncoloured is the next limit of them, the most voted first.
func (s *Store) uncoloured(ctx context.Context, schema string, limit int) ([]uncolouredRow, error) {
	rows, err := s.pool.Query(ctx, `
		WITH wanted AS (`+colourWanted(schema, "$2")+`)
		SELECT w.tconst, p.poster_url
		FROM wanted w
		JOIN meta.posters p USING (tconst)
		LEFT JOIN `+schema+`.ratings r USING (tconst)
		WHERE p.colour IS NULL
		  AND p.poster_url IS NOT NULL AND btrim(p.poster_url) <> ''
		ORDER BY r.num_votes DESC NULLS LAST, w.tconst
		LIMIT $1`, limit, daily.MinVotes)
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
		WITH wanted AS (`+colourWanted(schema, "$1")+`)
		SELECT count(*)
		FROM wanted w
		JOIN meta.posters p USING (tconst)
		WHERE p.colour IS NULL
		  AND p.poster_url IS NOT NULL AND btrim(p.poster_url) <> ''`, daily.MinVotes).Scan(&n)
	return n, err
}

func (s *Store) saveColour(ctx context.Context, tconst, hex string) error {
	_, err := s.pool.Exec(ctx,
		`UPDATE meta.posters SET colour = $2 WHERE tconst = $1`, tconst, hex)
	return err
}
