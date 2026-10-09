package catalog

// Development's Play again: a day's puzzle dealt afresh with another
// answer, so whoever is working on Daily can play it as a stranger, on a
// movie they have not yet seen the answer to, as often as they like. The
// API offers it only outside production; nothing here knows which it is
// in, so nothing else may call it.

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
// daily.RepeatDays, or makes no fair puzzle.
var ErrNoOtherAnswer = errors.New("catalog: no other movie can be that day's answer")

// RepickDailyPuzzle deals puzzle No. no again on its own day, from
// another answer: the job's candidates and its rules, the order drawn at
// random rather than from the day (daily.OrderBy), and the answer it has
// now left out with the other answers within daily.RepeatDays. The
// puzzle keeps its number and day, and every game of it, with their
// moves, goes in the same transaction: a game is moves made on one
// movie's cast, and replayed on another's it would be nonsense. The
// players stay, on the other days' boards.
//
// When no other movie fits it is ErrNoOtherAnswer, and nothing changes.
// Whatever it returns or fails with never names the answer it dealt, nor
// any candidate: whoever reads the logs is about to play it.
func (s *Store) RepickDailyPuzzle(ctx context.Context, no int) error {
	was, err := s.DailyPuzzleNo(ctx, no)
	if err != nil {
		return err
	}
	cands, err := s.dailyCandidates(ctx)
	if err != nil {
		return err
	}
	recent, err := s.dailyRecent(ctx, was.Day)
	if err != nil {
		return err
	}
	// The day is inside its own window, so its answer is among the
	// recent ones already; said here as well, since dealing it back would
	// be a reset that changed nothing.
	recent.Answers[was.Answer.ID] = true
	order := daily.OrderBy(daily.Seeded(strconv.FormatUint(rand.Uint64(), 36)), cands, recent)
	p, err := s.firstFair(ctx, nil, nil, no, was.Day, order)
	if errors.Is(err, errNoneFair) {
		return ErrNoOtherAnswer
	}
	if err != nil {
		return err
	}
	return s.replaceDailyPuzzle(ctx, was.Answer.ID, p)
}

// replaceDailyPuzzle writes p over the puzzle with its number and day,
// while its answer is still was, and deletes every game of it, the
// moves going with them. Two resets of one puzzle at once would each
// have left out only the answer they read; the second finds the answer
// changed under it and fails, rather than perhaps dealing back the one
// the first just dealt.
func (s *Store) replaceDailyPuzzle(ctx context.Context, was string, p *daily.Puzzle) error {
	cols, err := puzzleColumns(p)
	if err != nil {
		return err
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("catalog: begin re-picking daily puzzle %d: %w", p.No, err)
	}
	defer tx.Rollback(context.WithoutCancel(ctx))
	tag, err := tx.Exec(ctx, `
		UPDATE meta.daily_puzzles
		SET answer = $3, title = $4, year = $5, rating = $6, md = $7, length = $8, colour = $9, genres = $10,
		    directors = $11, billed = $12, movies = $13, era = $14, genre = $15, picked_at = now()
		WHERE no = $1 AND day = $2 AND answer = $16`,
		append(append([]any{p.No, p.Day}, cols...), was)...)
	if err != nil {
		return fmt.Errorf("catalog: re-pick daily puzzle %d: %w", p.No, err)
	}
	if tag.RowsAffected() != 1 {
		return fmt.Errorf("catalog: daily puzzle %d changed while it was being re-picked", p.No)
	}
	if _, err := tx.Exec(ctx, `DELETE FROM meta.daily_games WHERE no = $1`, p.No); err != nil {
		return fmt.Errorf("catalog: delete daily puzzle %d's games: %w", p.No, err)
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("catalog: commit re-picked daily puzzle %d: %w", p.No, err)
	}
	return nil
}
