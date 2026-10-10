package catalog

// Development's Play again: a movie of their own for one player on a
// day's puzzle, so whoever is working on Daily can play it as a stranger,
// on a movie they have not yet seen the answer to, as often as they
// like, while everyone else's game, and everyone else's movie, stays as
// it was. The API offers it only with the Daily's development tools on;
// nothing here knows whether they are, so nothing else may call it.

import (
	"context"
	"errors"
	"fmt"
	"math/rand/v2"
	"strconv"

	"cinedikt/internal/daily"
)

// ErrNoOtherAnswer is a day no other movie can be the answer to: every
// candidate is the day's own answer, another day's within
// daily.RepeatDays, the player's own deal of it, or makes no fair puzzle.
var ErrNoOtherAnswer = errors.New("catalog: no other movie can be that day's answer")

// realGameSQL is a game of meta.daily_games g played on its day's own
// puzzle, rather than on a movie its player was dealt for themselves
// (DealDailyPuzzle). Every count of the day's players, every board and
// every figure reads only these: a practice game is on another movie,
// and its points and its turns say nothing of the day's.
const realGameSQL = `NOT EXISTS (SELECT 1 FROM meta.daily_deals d WHERE d.player = g.player AND d.no = g.no)`

// DealDailyPuzzle deals player a movie of their own for puzzle No. no:
// another answer on the same day under the same number, from the job's
// candidates and its rules, the order drawn at random rather than from
// the day (daily.OrderBy), leaving out the day's own answer, the other
// answers within daily.RepeatDays and any deal the player had of it. The
// player's game of no is played on it from then on (DailyDeal), and any
// game they had of it goes, with its moves, in the same transaction: a
// game is moves made on one movie's cast, and replayed on another's it
// would be nonsense. Nobody else's game, and nobody else's movie,
// changes.
//
// When no other movie fits it is ErrNoOtherAnswer, and nothing changes.
// Whatever it returns or fails with never names the answer it dealt, nor
// any candidate: whoever reads the logs is about to play it.
func (s *Store) DealDailyPuzzle(ctx context.Context, player int64, no int) error {
	day, err := s.DailyPuzzleNo(ctx, no)
	if err != nil {
		return err
	}
	cands, err := s.dailyCandidates(ctx)
	if err != nil {
		return err
	}
	recent, err := s.dailyRecent(ctx, day.Day)
	if err != nil {
		return err
	}
	// The day is inside its own window, so its answer is among the
	// recent ones already; said here as well, since dealing it would be
	// a deal that changed nothing.
	recent.Answers[day.Answer.ID] = true
	had, err := s.DailyDeal(ctx, player, no)
	switch {
	case err == nil:
		recent.Answers[had.Answer.ID] = true
	case !errors.Is(err, ErrNotFound):
		return err
	}
	order := daily.OrderBy(daily.Seeded(strconv.FormatUint(rand.Uint64(), 36)), cands, recent)
	p, err := s.firstFair(ctx, nil, nil, no, day.Day, order)
	if errors.Is(err, errNoneFair) {
		return ErrNoOtherAnswer
	}
	if err != nil {
		return err
	}
	return s.putDailyDeal(ctx, player, p)
}

// putDailyDeal keeps p as player's own deal of its number, in place of
// any they had, and deletes their game of it, the moves going with it.
func (s *Store) putDailyDeal(ctx context.Context, player int64, p *daily.Puzzle) error {
	cols, err := puzzleColumns(p)
	if err != nil {
		return err
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("catalog: begin dealing daily puzzle %d: %w", p.No, err)
	}
	defer tx.Rollback(context.WithoutCancel(ctx))
	if _, err := tx.Exec(ctx, `
		INSERT INTO meta.daily_deals
		    (player, no, answer, title, year, rating, md, length, colour, genres, directors, billed, movies,
		     era, genre, picked_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, now())
		ON CONFLICT (player, no) DO UPDATE
		SET answer = $3, title = $4, year = $5, rating = $6, md = $7, length = $8, colour = $9, genres = $10,
		    directors = $11, billed = $12, movies = $13, era = $14, genre = $15, picked_at = now()`,
		append([]any{player, p.No}, cols...)...); err != nil {
		return fmt.Errorf("catalog: deal daily puzzle %d: %w", p.No, err)
	}
	if _, err := tx.Exec(ctx, `DELETE FROM meta.daily_games WHERE player = $1 AND no = $2`, player, p.No); err != nil {
		return fmt.Errorf("catalog: delete a game of daily puzzle %d: %w", p.No, err)
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("catalog: commit a deal of daily puzzle %d: %w", p.No, err)
	}
	return nil
}

// DailyDeal is the movie of their own player was dealt for puzzle No.
// no, under its number and on its day, or ErrNotFound when they were
// dealt none, as nobody is without the development tools.
func (s *Store) DailyDeal(ctx context.Context, player int64, no int) (*daily.Puzzle, error) {
	return s.scanDailyPuzzle(ctx, fmt.Sprintf("player %d's deal of %d", player, no), `
		SELECT z.no, z.day, d.answer, d.title, d.year, d.rating::float8, d.md, d.length, d.colour, d.genres,
		       d.directors, d.billed, d.movies, d.era, d.genre
		FROM meta.daily_deals d JOIN meta.daily_puzzles z ON z.no = d.no
		WHERE d.player = $1 AND d.no = $2`, player, no)
}
