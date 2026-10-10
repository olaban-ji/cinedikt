package catalog

// Cinedikt Daily's puzzles: the job that picks one for every day ahead,
// and the reads of them.
//
// A day's answer is a well-known movie, its cast shown one name at a
// time, and the Movies sheets the six's own movies, with the map's film
// test and its cap, every one of them crowded inside the answer's decade
// and rating band. What makes a fair answer and what its puzzle holds
// are internal/daily's to decide; this file reads the candidates, each
// one's people and their movies, and keeps what was picked in
// meta.daily_puzzles, copied whole, so nothing about a puzzle moves when
// the catalog does.

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"

	"cinedikt/internal/daily"
	"cinedikt/internal/notify"
)

// Each reader plays the puzzle for their own date, so the days the job
// keeps picked are counted from UTC's and reach past it both ways:
// DailyBehind days before, for the zones west of UTC, still on
// yesterday until noon UTC at UTC−12, and DailyAhead days after, for
// the zones east of it, on tomorrow from ten UTC at UTC+14, and then a
// week more. A deploy that restarts the job, a night the catalog is
// late, or a pass that fails for days on end never leaves anyone a day
// with no puzzle, and the board says so long before it would.
const (
	DailyBehind = 1
	DailyAhead  = 8
)

// DailyRest is how long the job waits between passes. Most passes find
// every day already picked and stop at a read; the one after midnight
// UTC picks the new last day. A new generation wakes it sooner.
const DailyRest = time.Hour

// DailyJob keeps a puzzle picked for every day from DailyBehind before
// UTC today to DailyAhead after it. A pass is idempotent: a day already
// picked is left as it is, and two processes picking the same day keep
// whichever wrote first, so a restart never skips a day nor changes one.
type DailyJob struct {
	Store  *Store
	Logger *slog.Logger
	// Days is how many days are kept picked, UTC yesterday first. Zero is
	// every day from DailyBehind before today to DailyAhead after it.
	Days int
	// Now is the clock; nil is time.Now. A test moves it.
	Now func() time.Time
	// Client fetches the poster of a candidate the colour job has not
	// reached, to work its colour out; nil is one with ColourFetch's
	// deadline.
	Client *http.Client
}

// DailyStatsBudget bounds the reading of a day's games for the board.
// It is a day's games replayed in memory, quick even for thousands, and
// what it says is never worth holding up the job's report.
const DailyStatsBudget = 10 * time.Second

// Stats fills in a pass's Checked event with how yesterday's puzzle, UTC
// yesterday's, has gone (Store.DailyDayStats): the board's Daily line,
// which is how the owner sees a day's difficulty without opening the
// database. Yesterday's rather than today's, since today's has hardly
// begun for most readers, and yesterday's has had a full day in every
// zone but the farthest west. A read that fails leaves the event as it
// is: the board keeps the last figures it had, and the job's work is
// unaffected.
func (j *DailyJob) Stats(ctx context.Context, e *notify.Event) {
	now := time.Now
	if j.Now != nil {
		now = j.Now
	}
	ctx, cancel := context.WithTimeout(ctx, DailyStatsBudget)
	defer cancel()
	d, err := j.Store.DailyDayStats(ctx, daily.Today(now()).AddDate(0, 0, -1))
	if err != nil {
		if j.Logger != nil {
			j.Logger.Warn("daily day stats", "err", err)
		}
		return
	}
	e.Daily = d
}

// Run is one pass.
func (j *DailyJob) Run(ctx context.Context) error {
	began := time.Now()
	now := time.Now
	if j.Now != nil {
		now = j.Now
	}
	from := daily.Today(now()).AddDate(0, 0, -DailyBehind)
	days := j.Days
	if days <= 0 {
		days = DailyBehind + 1 + DailyAhead
	}
	first, err := j.Store.firstDailyDay(ctx)
	if err != nil {
		return err
	}
	// No. 1's day is worked back from the lowest number kept
	// (firstDailyDay), and on a table nothing has been picked into it is
	// the first day of this pass's window, UTC yesterday, whether or not
	// that day can be picked: a day missed keeps its number for a later
	// pass. A day before No. 1's is never picked once there is a puzzle:
	// it would be No. 0, and every number after it would move. Readers
	// whose date is earlier wait for the first.
	if first.IsZero() {
		first = from
	}
	var cands []daily.Candidate
	var failed []error
	picked := 0
	for i := range days {
		day := from.AddDate(0, 0, i)
		if day.Before(first) {
			continue
		}
		have, err := j.Store.hasDailyPuzzle(ctx, day)
		if err != nil {
			return err
		}
		if have {
			continue
		}
		if cands == nil {
			if cands, err = j.Store.dailyCandidates(ctx); err != nil {
				return err
			}
		}
		p, err := j.pick(ctx, day, daily.Number(day, first), cands)
		if err != nil {
			if ctx.Err() != nil {
				return ctx.Err()
			}
			failed = append(failed, err)
			continue
		}
		kept, err := j.Store.putDailyPuzzle(ctx, p)
		if err != nil {
			return err
		}
		if !kept {
			// Another process picked it first, which is as good.
			continue
		}
		picked++
		// Never the answer, nor how far down the day's order it was:
		// whoever reads the logs may want to play it too.
		j.Logger.Info("daily puzzle picked", "day", daily.DayString(day), "no", p.No, "movies", len(p.Movies))
	}
	if picked > 0 || len(failed) > 0 {
		j.Logger.Info("daily puzzles pass", "picked", picked, "failed", len(failed),
			"candidates", len(cands), "took", time.Since(began).Round(time.Millisecond))
	}
	return errors.Join(failed...)
}

// pick is the first candidate, in the day's order, that makes a fair
// puzzle. Each candidate's map is read only when it is reached: the
// first usually serves.
//
// Nothing it logs or returns names a candidate. The logs and the
// Telegram board are read by whoever runs the site, who may want to
// play too, and the candidate a failed read was about is most likely
// the day's answer once the read works; one found unfit could become an
// answer when a new catalog grows its map.
func (j *DailyJob) pick(ctx context.Context, day time.Time, no int, cands []daily.Candidate) (*daily.Puzzle, error) {
	recent, err := j.Store.dailyRecent(ctx, day)
	if err != nil {
		return nil, err
	}
	order := daily.Order(day, cands, recent)
	p, err := j.Store.firstFair(ctx, j.Logger, j.Client, no, day, order)
	if errors.Is(err, errNoneFair) {
		return nil, fmt.Errorf("catalog: none of %d candidates can be the daily answer for %s", len(order), daily.DayString(day))
	}
	return p, err
}

// errNoneFair is an order with no candidate in it that makes a fair
// puzzle.
var errNoneFair = errors.New("catalog: no candidate makes a fair puzzle")

// firstFair makes puzzle No. no on day from the first candidate in order
// that makes a fair one, reading each one's people and movies only when
// it is reached, or is errNoneFair. It is the pick, for the job and for
// development's Play again alike, so a day dealt again is held to every
// rule the day was. logger, when there is one, hears why a candidate was
// passed over, by its place in the order and never by name; client
// fetches a poster whose colour is not known yet, nil being one with
// ColourFetch's deadline.
func (s *Store) firstFair(ctx context.Context, logger *slog.Logger, client *http.Client, no int, day time.Time, order []daily.Candidate) (*daily.Puzzle, error) {
	if client == nil {
		client = &http.Client{Timeout: ColourFetch}
	}
	for i, c := range order {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		p, err := s.dailyPuzzleOf(ctx, client, no, day, c)
		var unfit daily.Unfit
		if errors.As(err, &unfit) {
			if logger != nil {
				logger.Debug("not a daily answer", "day", daily.DayString(day), "candidate", i+1, "why", unfit)
			}
			continue
		}
		if err != nil {
			return nil, unnamed{err: err, id: c.ID}
		}
		return p, nil
	}
	return nil, errNoneFair
}

// unnamed is an error about a candidate with the candidate taken out of
// its words: the store's errors name the movie they were reading by its
// tconst ("people on tt0133093"), which pick must not pass on. It still
// unwraps to the error it was.
type unnamed struct {
	err error
	id  string
}

func (u unnamed) Error() string { return strings.ReplaceAll(u.err.Error(), u.id, "a candidate") }

func (u unnamed) Unwrap() error { return u.err }

// ErrNoRuntimes is a live catalog imported before titles kept IMDb's
// runtime, which the Length fact is. Nothing can be picked from it: every
// candidate would be unfit, and the pass would say so of every day ahead
// rather than once why. The next import brings the column, and the pass
// after it picks as usual; until then the page answers "not-ready".
var ErrNoRuntimes = errors.New("catalog: the catalog predates runtimes; the next import adds them")

// hasRuntimes is whether the live catalog's titles keep runtimes.
func (s *Store) hasRuntimes(ctx context.Context) (bool, error) {
	var has bool
	err := s.pool.QueryRow(ctx, `
		SELECT EXISTS (
		    SELECT 1 FROM information_schema.columns
		    WHERE table_schema = $1 AND table_name = 'titles' AND column_name = 'runtime_minutes')`, Live).Scan(&has)
	if err != nil {
		return false, fmt.Errorf("catalog: look for runtimes: %w", err)
	}
	return has, nil
}

// dailyCandidates are the movies that could be an answer: every one with
// daily.MinVotes votes, rated and passing gridFilm, with a runtime, the
// Length fact, and a poster that is ok with an address, which the end of
// the game shows and whose colour fills the hidden card until then. Its
// era is the one of eras its year falls in, so a movie made before the
// first has none and is no candidate, which in October 2026 left out one
// movie with MinVotes votes. Nothing is asked of its synopsis: no fact
// is a line of its text, so a movie OMDb has no plot for is as fair an
// answer as any. A candidate whose poster has no colour yet is still
// one; the pick works it out (dailyPuzzleOf). ErrNoRuntimes before the
// catalog has runtimes at all.
//
// Only the crowded come back: those each of whose six has
// daily.MinCrowd movies besides it on their sheet inside its decade and
// its rating band. Of the movies with MinVotes votes about one in twenty
// is, so asked of a candidate at a time the rule would read twenty
// candidates' people and sheets for every day picked; asked here, of
// every one at once, it is one query, of about two seconds on the full
// catalog. It is daily.Build's rule as the reads of a candidate's puzzle
// see it: the six are peopleOn's first six billed cast, those credited
// on it as actor or actress, with a name, and neither credited as its
// director nor in its crew's directors, in billing order; each one's
// sheet is sheetFilms's, their daily.MaxSheet most voted of the movies
// they are credited on as actor, actress or director or in the crew's
// directors, rated and passing gridFilm, the answer first and ties to
// the lower id, so a movie is on it when its place among theirs, with
// the answer moved first, is within the cap; and the decade is
// daily.Decade's and the band daily.RatingBand's, from
// daily.RatingFloors.
//
// All but one part of it: Build also sets each one's own "Also in"
// aside, which turns on close relatives and shared titles
// (daily.RelativeShared, sharesTitle), more than a query can fairly
// say. So the query keeps every candidate Build would and a few it
// will not, 42 of its 344 in October 2026, each a cheap refusal in
// dailyPuzzleOf before any poster is fetched, and Build, asking the
// whole rule again of what was read, has the last word.
func (s *Store) dailyCandidates(ctx context.Context) ([]daily.Candidate, error) {
	has, err := s.hasRuntimes(ctx)
	if err != nil {
		return nil, err
	}
	if !has {
		return nil, ErrNoRuntimes
	}
	rows, err := s.pool.Query(ctx, `
		WITH cand AS (
		    SELECT t.tconst, t.primary_title, t.start_year, r.average_rating, p.released, t.runtime_minutes,
		           coalesce(p.colour::text, '') AS colour, p.poster_url, e.lo AS era, t.genres, r.num_votes,
		           width_bucket(r.average_rating, $4::numeric[]) AS band
		    FROM `+Live+`.titles t
		    JOIN `+Live+`.ratings r USING (tconst)
		    JOIN meta.posters p USING (tconst)
		    JOIN (VALUES `+erasSQL()+`) AS e (lo, hi) ON t.start_year BETWEEN e.lo AND e.hi
		    WHERE r.num_votes >= $1
		      AND t.runtime_minutes > 0
		      AND p.status = 'ok'
		      AND btrim(coalesce(p.poster_url, '')) <> ''
		      AND `+gridFilm+`
		), billed AS (
		    SELECT pr.tconst AS answer, pr.nconst, min(pr.ordering) AS ord
		    FROM cand c
		    JOIN `+Live+`.principals pr ON pr.tconst = c.tconst
		    JOIN `+Live+`.names n ON n.nconst = pr.nconst
		    GROUP BY pr.tconst, pr.nconst
		    HAVING NOT bool_or(pr.category = 'director')
		       AND NOT EXISTS (SELECT 1 FROM `+Live+`.directors d WHERE d.tconst = pr.tconst AND d.nconst = pr.nconst)
		), six AS (
		    SELECT answer, nconst
		    FROM (SELECT answer, nconst, row_number() OVER (PARTITION BY answer ORDER BY ord) AS slot FROM billed) b
		    WHERE slot <= $5
		), credits AS (
		    SELECT pr.tconst, pr.nconst
		    FROM `+Live+`.principals pr
		    WHERE pr.nconst IN (SELECT nconst FROM six) AND pr.category IN ('actor', 'actress', 'director')
		    UNION
		    SELECT d.tconst, d.nconst
		    FROM `+Live+`.directors d
		    WHERE d.nconst IN (SELECT nconst FROM six)
		), theirs AS (
		    -- Each one's movies, numbered in the order their sheet is
		    -- capped in, but for the answer, which each candidate moves
		    -- first.
		    SELECT c.nconst, c.tconst, t.start_year / 10 AS decade,
		           width_bucket(r.average_rating, $4::numeric[]) AS band,
		           row_number() OVER (PARTITION BY c.nconst ORDER BY r.num_votes DESC, c.tconst) AS n
		    FROM credits c
		    JOIN `+Live+`.titles t USING (tconst)
		    JOIN `+Live+`.ratings r USING (tconst)
		    LEFT JOIN meta.posters p USING (tconst)
		    WHERE `+gridFilm+`
		), crowds AS (
		    -- A movie numbered after the answer keeps its place once the
		    -- answer is moved first; one numbered before it moves down one.
		    SELECT s.answer, count(f.tconst) AS crowd
		    FROM six s
		    JOIN cand c ON c.tconst = s.answer
		    JOIN theirs a ON a.nconst = s.nconst AND a.tconst = s.answer
		    LEFT JOIN theirs f ON f.nconst = s.nconst AND f.tconst <> s.answer
		         AND f.decade = c.start_year / 10 AND f.band = c.band
		         AND f.n + (a.n > f.n)::int <= $2
		    GROUP BY s.answer, s.nconst
		)
		SELECT c.tconst, c.primary_title, c.start_year, c.average_rating::float8, c.released, c.runtime_minutes,
		       c.colour, c.poster_url, c.era, c.genres, c.num_votes
		FROM cand c
		WHERE c.tconst IN (SELECT answer FROM crowds GROUP BY answer HAVING count(*) = $5 AND min(crowd) >= $3)`,
		daily.MinVotes, daily.MaxSheet, daily.MinCrowd, daily.RatingFloors(), daily.MinCast)
	if err != nil {
		return nil, fmt.Errorf("catalog: daily candidates: %w", err)
	}
	defer rows.Close()
	var out []daily.Candidate
	for rows.Next() {
		var c daily.Candidate
		var released *time.Time
		if err := rows.Scan(&c.ID, &c.Title, &c.Year, &c.Rating, &released, &c.Length, &c.Colour, &c.Poster,
			&c.Era, &c.Genres, &c.Votes); err != nil {
			return nil, fmt.Errorf("catalog: scan daily candidate: %w", err)
		}
		c.MD = monthDay(released)
		out = append(out, c)
	}
	return out, rows.Err()
}

// monthDay is a release date as the layout sorts it, month*100 + day,
// or 0 for none.
func monthDay(released *time.Time) int {
	if released == nil {
		return 0
	}
	return int(released.Month())*100 + released.Day()
}

// chipDirector is peopleOn's role for a director: anyone in the crew's
// directors or credited as one, though they acted in it too.
const chipDirector = "director"

// dailyPuzzleOf reads a candidate's people and their movies and makes
// its puzzle, or says, as daily.Unfit, why it cannot be one. The people
// are the app's own chip row (peopleOn), billed cast in billing order and
// directors in crew order, so the six are the six a reader would see on
// its map, and someone who directed it is never one of them. What is
// cheap to refuse on is refused before what is not: the people, then the
// movies, of which each of the six needs daily.MinSheet besides the
// answer and daily.MinCrowd inside its decade and rating band besides
// their own Also in as well (daily.Fit), and only then the colour, which
// may mean fetching the poster. Most candidates have no colour yet, the
// colour job colouring only the opening screen's pool, so a poster is
// fetched only for one that will make a puzzle.
func (s *Store) dailyPuzzleOf(ctx context.Context, client *http.Client, no int, day time.Time, c daily.Candidate) (*daily.Puzzle, error) {
	people, _, err := s.peopleOn(ctx, c.ID)
	if err != nil {
		return nil, err
	}
	ids := make([]string, len(people))
	var cast, directors []daily.Named
	for i, p := range people {
		ids[i] = p.ID
		if p.Role == chipDirector {
			directors = append(directors, daily.Named{ID: p.ID, Name: p.Name})
		} else {
			cast = append(cast, daily.Named{ID: p.ID, Name: p.Name})
		}
	}
	if len(directors) < daily.MinDirectors || len(cast) < daily.MinCast {
		return nil, daily.Unfit(fmt.Sprintf("it has %d directors and %d billed cast", len(directors), len(cast)))
	}
	six := make([]string, daily.MinCast)
	for i, p := range cast[:daily.MinCast] {
		six[i] = p.ID
	}
	films, err := s.sheetFilms(ctx, c.ID, six, ids)
	if err != nil {
		return nil, err
	}
	if err := daily.Fit(c, cast, directors, films); err != nil {
		return nil, err
	}
	if c.Colour, err = s.candidateColour(ctx, client, c); err != nil {
		return nil, err
	}
	return daily.Build(no, day, c, cast, directors, films)
}

// candidateColour is a candidate's poster colour: the colour job's, or,
// for one it has not reached, worked out now from the poster just as the
// job would, at the size it uses and then the address kept, and kept for
// it. Empty when neither picture can be read: the candidate is passed
// over, and may be an answer another day, once the job has it.
func (s *Store) candidateColour(ctx context.Context, client *http.Client, c daily.Candidate) (string, error) {
	if c.Colour != "" {
		return c.Colour, nil
	}
	target := PosterAt(c.Poster, colourWidth)
	hex, err := PosterColour(ctx, client, target)
	if err != nil && target != c.Poster && ctx.Err() == nil {
		hex, err = PosterColour(ctx, client, c.Poster)
	}
	if err != nil {
		// Not the error itself, which names the poster's address and so
		// the movie: only that there is no colour.
		return "", ctx.Err()
	}
	if err := s.saveColour(ctx, c.ID, hex); err != nil {
		return "", fmt.Errorf("catalog: keep a candidate's colour: %w", err)
	}
	return hex, nil
}

// sheetFilms are the movies the six's Movies sheets are made from: each
// one's daily.MaxSheet most voted, as the map caps a spine, the answer
// first among each one's, read together, so a movie in one's top that
// another of the six is on below their own cap is read once, for the
// first. daily.Build orders each one's the same way to keep whose sheets
// a movie is on, so it is on no sheet whose cap it missed. Every one is
// rated and passes the map's film test, with which of the answer's
// people are credited on it, all of them rather than only the six, by
// the credits a map counts (actor, actress or director, and the crew's
// directors), so a close relative can be told.
func (s *Store) sheetFilms(ctx context.Context, answer string, six, people []string) ([]daily.MapFilm, error) {
	rows, err := s.pool.Query(ctx, `
		WITH credits AS (
		    SELECT pr.tconst, pr.nconst
		    FROM `+Live+`.principals pr
		    WHERE pr.nconst = ANY($2) AND pr.category IN ('actor', 'actress', 'director')
		    UNION
		    SELECT d.tconst, d.nconst
		    FROM `+Live+`.directors d
		    WHERE d.nconst = ANY($2)
		), who AS (
		    SELECT tconst, array_agg(nconst ORDER BY nconst) AS people
		    FROM credits
		    GROUP BY tconst
		), capped AS (
		    SELECT c.tconst,
		           row_number() OVER (PARTITION BY c.nconst
		                              ORDER BY (c.tconst = $3) DESC, r.num_votes DESC, c.tconst) AS n
		    FROM credits c
		    JOIN `+Live+`.titles t USING (tconst)
		    JOIN `+Live+`.ratings r USING (tconst)
		    LEFT JOIN meta.posters p USING (tconst)
		    WHERE c.nconst = ANY($1) AND `+gridFilm+`
		)
		SELECT t.tconst, t.primary_title, t.start_year, r.average_rating::float8, p.released, t.genres,
		       r.num_votes, who.people
		FROM (SELECT DISTINCT tconst FROM capped WHERE n <= $4) k
		JOIN who USING (tconst)
		JOIN `+Live+`.titles t USING (tconst)
		JOIN `+Live+`.ratings r USING (tconst)
		LEFT JOIN meta.posters p USING (tconst)`, six, people, answer, daily.MaxSheet)
	if err != nil {
		return nil, fmt.Errorf("catalog: daily sheets for %s: %w", answer, err)
	}
	defer rows.Close()
	var out []daily.MapFilm
	for rows.Next() {
		var f daily.MapFilm
		var released *time.Time
		if err := rows.Scan(&f.ID, &f.Title, &f.Year, &f.Rating, &released, &f.Genres, &f.Votes, &f.People); err != nil {
			return nil, fmt.Errorf("catalog: scan daily sheet movie: %w", err)
		}
		f.MD = monthDay(released)
		out = append(out, f)
	}
	return out, rows.Err()
}

// firstDailyDay is the day No. 1 is, or would have been, or zero before
// anything is kept. It is worked back from the lowest number rather than
// read as the earliest day: a first pass that could not pick its first
// day but kept the next ones has no No. 1 yet, and the earliest day kept
// would then count from 1 again, onto numbers already taken. Worked back,
// the missing day is still No. 1 and the next pass can fill it.
func (s *Store) firstDailyDay(ctx context.Context) (time.Time, error) {
	var first time.Time
	err := s.pool.QueryRow(ctx, `
		SELECT day - (no - 1) FROM meta.daily_puzzles ORDER BY no LIMIT 1`).Scan(&first)
	if errors.Is(err, pgx.ErrNoRows) {
		return time.Time{}, nil
	}
	if err != nil {
		return time.Time{}, fmt.Errorf("catalog: first daily puzzle: %w", err)
	}
	return first, nil
}

func (s *Store) hasDailyPuzzle(ctx context.Context, day time.Time) (bool, error) {
	var have bool
	if err := s.pool.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM meta.daily_puzzles WHERE day = $1)`, day).Scan(&have); err != nil {
		return false, fmt.Errorf("catalog: look for the daily puzzle on %s: %w", daily.DayString(day), err)
	}
	return have, nil
}

// dailyRecent is what the days around a day already used: the answers
// within daily.RepeatDays either side, and the eras and first genres of
// the days just before it.
func (s *Store) dailyRecent(ctx context.Context, day time.Time) (daily.Recent, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT day, answer, era, genre FROM meta.daily_puzzles
		WHERE day BETWEEN $1::date - $2::int AND $1::date + $2::int`, day, daily.RepeatDays)
	if err != nil {
		return daily.Recent{}, fmt.Errorf("catalog: recent daily puzzles: %w", err)
	}
	defer rows.Close()
	recent := daily.Recent{Answers: map[string]bool{}, Eras: map[int]bool{}, Genres: map[string]bool{}}
	for rows.Next() {
		var at time.Time
		var answer, genre string
		var era int
		if err := rows.Scan(&at, &answer, &era, &genre); err != nil {
			return daily.Recent{}, fmt.Errorf("catalog: scan recent daily puzzle: %w", err)
		}
		recent.Answers[answer] = true
		before := day.Sub(at)
		if before > 0 && before <= daily.EraDays*24*time.Hour {
			recent.Eras[era] = true
		}
		if before > 0 && before <= daily.GenreDays*24*time.Hour && genre != "" {
			recent.Genres[genre] = true
		}
	}
	return recent, rows.Err()
}

// putDailyPuzzle keeps a picked puzzle, and says whether it did: a day
// already taken, by a pass in another process, keeps what was there. A
// number already taken by another day is an error, not a quiet skip: it
// means the numbering has gone wrong, and a day left without a puzzle
// for good should be heard about.
func (s *Store) putDailyPuzzle(ctx context.Context, p *daily.Puzzle) (bool, error) {
	cols, err := puzzleColumns(p)
	if err != nil {
		return false, err
	}
	tag, err := s.pool.Exec(ctx, `
		INSERT INTO meta.daily_puzzles
		    (no, day, answer, title, year, rating, md, length, colour, genres, directors, billed, movies,
		     era, genre, picked_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, now())
		ON CONFLICT (day) DO NOTHING`,
		append([]any{p.No, p.Day}, cols...)...)
	if err != nil {
		return false, fmt.Errorf("catalog: keep the daily puzzle for %s: %w", daily.DayString(p.Day), err)
	}
	return tag.RowsAffected() == 1, nil
}

// puzzleColumns is what a puzzle keeps besides its number and day, in
// the columns' order from answer to genre.
func puzzleColumns(p *daily.Puzzle) ([]any, error) {
	// A nil list is a SQL or JSON null, which the columns refuse; none
	// is empty.
	directors, billed, movies := p.Directors, p.Cast, p.Movies
	if directors == nil {
		directors = []daily.Named{}
	}
	if billed == nil {
		billed = []daily.Billed{}
	}
	if movies == nil {
		movies = []daily.Movie{}
	}
	var docs [3][]byte
	for i, v := range []any{directors, billed, movies} {
		raw, err := json.Marshal(v)
		if err != nil {
			return nil, err
		}
		docs[i] = raw
	}
	genres := p.Answer.Genres
	if genres == nil {
		genres = []string{}
	}
	a := p.Answer
	return []any{a.ID, a.Title, a.Year, a.Rating, a.MD, a.Length, a.Colour, genres,
		docs[0], docs[1], docs[2], p.Era, p.Genre}, nil
}

// DailyPuzzle is the puzzle for a day, or ErrNotFound when none has been
// picked for it.
func (s *Store) DailyPuzzle(ctx context.Context, day time.Time) (*daily.Puzzle, error) {
	return s.dailyPuzzle(ctx, `day = $1`, daily.Today(day))
}

// DailyPuzzleNo is puzzle No. no, or ErrNotFound.
func (s *Store) DailyPuzzleNo(ctx context.Context, no int) (*daily.Puzzle, error) {
	return s.dailyPuzzle(ctx, `no = $1`, no)
}

func (s *Store) dailyPuzzle(ctx context.Context, where string, arg any) (*daily.Puzzle, error) {
	var p daily.Puzzle
	var directors, billed, movies []byte
	a := &p.Answer
	err := s.pool.QueryRow(ctx, `
		SELECT no, day, answer, title, year, rating::float8, md, length, colour, genres, directors, billed, movies,
		       era, genre
		FROM meta.daily_puzzles WHERE `+where, arg).
		Scan(&p.No, &p.Day, &a.ID, &a.Title, &a.Year, &a.Rating, &a.MD, &a.Length, &a.Colour, &a.Genres,
			&directors, &billed, &movies, &p.Era, &p.Genre)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, fmt.Errorf("catalog: daily puzzle %v: %w", arg, ErrNotFound)
	}
	if err != nil {
		return nil, fmt.Errorf("catalog: read daily puzzle %v: %w", arg, err)
	}
	for _, doc := range []struct {
		what string
		raw  []byte
		into any
	}{{"directors", directors, &p.Directors}, {"cast", billed, &p.Cast}, {"movies", movies, &p.Movies}} {
		if err := json.Unmarshal(doc.raw, doc.into); err != nil {
			return nil, fmt.Errorf("catalog: decode daily puzzle %d's %s: %w", p.No, doc.what, err)
		}
	}
	p.Day = daily.Today(p.Day)
	return &p, nil
}
