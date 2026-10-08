package catalog

// Cinedikt Daily's puzzles: the job that picks one for every day ahead,
// and the reads of them.
//
// A day's answer is a well-known movie, its board the app's own map of
// it with the answer hidden. What makes a fair answer and how its board
// is dealt are internal/daily's to decide; this file reads the
// candidates and each one's map, and keeps what was picked in
// meta.daily_puzzles, copied whole, so nothing about a puzzle moves when
// the catalog does.

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"

	"cinedikt/internal/daily"
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
		j.Logger.Info("daily puzzle picked", "day", daily.DayString(day), "no", p.No, "cards", len(p.Cards))
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
	for i, c := range order {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		p, err := j.Store.dailyPuzzleOf(ctx, no, day, c)
		var unfit daily.Unfit
		if errors.As(err, &unfit) {
			j.Logger.Debug("not a daily answer", "day", daily.DayString(day), "candidate", i+1, "why", unfit)
			continue
		}
		if err != nil {
			return nil, unnamed{err: err, id: c.ID}
		}
		return p, nil
	}
	return nil, fmt.Errorf("catalog: none of %d candidates can be the daily answer for %s", len(order), daily.DayString(day))
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

// dailyCandidates are the movies that could be an answer: the most voted
// 250 of each era in the opening screen's pool, so every one is well
// known and the eras are spread, each rated and with a poster, which
// the end of the game shows. Nothing is asked of its synopsis: no clue
// is a line of its text any more, so a movie OMDb has no plot for is as
// fair an answer as any.
func (s *Store) dailyCandidates(ctx context.Context) ([]daily.Candidate, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT t.tconst, t.primary_title, t.start_year, r.average_rating::float8, p.released,
		       f.era, t.genres, f.num_votes
		FROM `+Live+`.first_run f
		JOIN `+Live+`.titles t USING (tconst)
		JOIN `+Live+`.ratings r USING (tconst)
		JOIN meta.posters p USING (tconst)
		WHERE p.status = 'ok'
		  AND btrim(coalesce(p.poster_url, '')) <> ''
		  AND `+gridFilm)
	if err != nil {
		return nil, fmt.Errorf("catalog: daily candidates: %w", err)
	}
	defer rows.Close()
	var out []daily.Candidate
	for rows.Next() {
		var c daily.Candidate
		var released *time.Time
		if err := rows.Scan(&c.ID, &c.Title, &c.Year, &c.Rating, &released, &c.Era, &c.Genres, &c.Votes); err != nil {
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

// dailyPuzzleOf reads a candidate's people and map and deals its
// puzzle, or says, as daily.Unfit, why it cannot be one. The map is the
// app's own, peopleOn and spine, so the board is exactly the map a
// reader would open, with the same film test and the same cap.
func (s *Store) dailyPuzzleOf(ctx context.Context, no int, day time.Time, c daily.Candidate) (*daily.Puzzle, error) {
	people, _, err := s.peopleOn(ctx, c.ID)
	if err != nil {
		return nil, err
	}
	slots := make([]daily.Slot, len(people))
	directors, cast := 0, 0
	for i, p := range people {
		slots[i] = daily.Slot{ID: p.ID, Name: p.Name, Role: p.Role}
		if p.Role == daily.RoleDirector {
			directors++
		} else {
			cast++
		}
	}
	// Build says the same, but saying it here saves reading a map that
	// could never be a board.
	if directors < daily.MinDirectors || cast < daily.MinCast {
		return nil, daily.Unfit(fmt.Sprintf("it has %d directors and %d billed cast", directors, cast))
	}
	spine, err := s.spine(ctx, c.ID, people)
	if err != nil {
		return nil, err
	}
	ids := make([]string, len(spine))
	for i, row := range spine {
		ids[i] = row[0].(string)
	}
	named, err := s.titlesAndVotes(ctx, ids)
	if err != nil {
		return nil, err
	}
	films := make([]daily.MapFilm, 0, len(spine))
	for _, row := range spine {
		id := row[0].(string)
		f := daily.MapFilm{ID: id, Title: named[id].title, Year: row[1].(int), MD: row[3].(int), Votes: named[id].votes, People: row[4].([]int)}
		if score, ok := row[2].(float64); ok {
			f.Rating = &score
		}
		films = append(films, f)
	}
	return daily.Build(no, day, c, slots, films)
}

type titleVotes struct {
	title string
	votes int
}

// titlesAndVotes is the title and vote count of each movie: what a card
// is called, and how well known it is, which the spine leaves out.
func (s *Store) titlesAndVotes(ctx context.Context, ids []string) (map[string]titleVotes, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT t.tconst, t.primary_title, coalesce(r.num_votes, 0)
		FROM `+Live+`.titles t
		LEFT JOIN `+Live+`.ratings r USING (tconst)
		WHERE t.tconst = ANY($1)`, ids)
	if err != nil {
		return nil, fmt.Errorf("catalog: titles and votes: %w", err)
	}
	defer rows.Close()
	out := make(map[string]titleVotes, len(ids))
	for rows.Next() {
		var id string
		var tv titleVotes
		if err := rows.Scan(&id, &tv.title, &tv.votes); err != nil {
			return nil, fmt.Errorf("catalog: scan title and votes: %w", err)
		}
		out[id] = tv
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
	people, err := json.Marshal(p.People)
	if err != nil {
		return false, err
	}
	cards, err := json.Marshal(p.Cards)
	if err != nil {
		return false, err
	}
	// A nil list is a SQL null, which the columns refuse; none is empty.
	genres, start := p.Answer.Genres, p.Start
	if genres == nil {
		genres = []string{}
	}
	if start == nil {
		start = []string{}
	}
	tag, err := s.pool.Exec(ctx, `
		INSERT INTO meta.daily_puzzles
		    (no, day, answer, title, year, rating, md, people, genres, cards, start, era, genre, picked_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, now())
		ON CONFLICT (day) DO NOTHING`,
		p.No, p.Day, p.Answer.ID, p.Answer.Title, p.Answer.Year, p.Answer.Rating, p.Answer.MD,
		people, genres, cards, start, p.Era, p.Genre)
	if err != nil {
		return false, fmt.Errorf("catalog: keep the daily puzzle for %s: %w", daily.DayString(p.Day), err)
	}
	return tag.RowsAffected() == 1, nil
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
	var people, cards []byte
	err := s.pool.QueryRow(ctx, `
		SELECT no, day, answer, title, year, rating::float8, md, people, genres, cards, start, era, genre
		FROM meta.daily_puzzles WHERE `+where, arg).
		Scan(&p.No, &p.Day, &p.Answer.ID, &p.Answer.Title, &p.Answer.Year, &p.Answer.Rating, &p.Answer.MD,
			&people, &p.Answer.Genres, &cards, &p.Start, &p.Era, &p.Genre)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, fmt.Errorf("catalog: daily puzzle %v: %w", arg, ErrNotFound)
	}
	if err != nil {
		return nil, fmt.Errorf("catalog: read daily puzzle %v: %w", arg, err)
	}
	if err := json.Unmarshal(people, &p.People); err != nil {
		return nil, fmt.Errorf("catalog: decode daily puzzle %d's people: %w", p.No, err)
	}
	if err := json.Unmarshal(cards, &p.Cards); err != nil {
		return nil, fmt.Errorf("catalog: decode daily puzzle %d's cards: %w", p.No, err)
	}
	p.Day = daily.Today(p.Day)
	return &p, nil
}
