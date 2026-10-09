package catalog

import (
	"context"
	"fmt"
	"io"
	"log/slog"

	"github.com/jackc/pgx/v5"
)

// TConsts is the allow-list of movie ids that the other four files are
// filtered against. It is a set of roughly three-quarters of a million
// short strings, held for the length of one import.
type TConsts map[string]struct{}

func (k TConsts) Has(id string) bool { _, ok := k[id]; return ok }
func (k TConsts) add(id string)      { k[id] = struct{}{} }

// NConsts is the set of people who kept at least one movie credit.
type NConsts map[string]struct{}

func (n NConsts) Has(id string) bool { _, ok := n[id]; return ok }
func (n NConsts) add(id string)      { n[id] = struct{}{} }

// copyRows streams rows into a table with COPY. pgx drives the source,
// pulling one row at a time, so a file of forty million rows is never
// held in memory.
func (s *Store) copyRows(ctx context.Context, table string, columns []string, src pgx.CopyFromSource) (int64, error) {
	n, err := s.pool.CopyFrom(ctx, pgx.Identifier{Staging, table}, columns, src)
	if err != nil {
		return n, fmt.Errorf("catalog: copy into %s: %w", table, err)
	}
	return n, nil
}

// rowSource adapts a Reader to pgx's COPY source.
//
// `rows` turns one line into the rows it becomes: none for a line that
// is dropped, one for most files, several for title.crew, where a film's
// director list is exploded. The batch is drained before the reader
// moves on, so a film with two directors cannot cost the next film its
// own row.
type rowSource struct {
	r     *Reader
	rows  func(*Reader) [][]any
	batch [][]any
	at    int
	err   error
	// track says how far the file has got. Counted in rows kept, which
	// is what the caller cares about; the lines read past are most of
	// title.basics and are not worth reporting separately.
	track *progress
	kept  int64
}

func (s *rowSource) Next() bool {
	for s.at >= len(s.batch) {
		if !s.r.Next() {
			s.err = s.r.Err()
			return false
		}
		s.batch, s.at = s.rows(s.r), 0
	}
	s.at++
	s.kept++
	if s.track != nil {
		s.track.step(s.kept)
	}
	return true
}

func (s *rowSource) Values() ([]any, error) { return s.batch[s.at-1], nil }
func (s *rowSource) Err() error             { return s.err }

// one wraps a single row, for the four files where a line is a row.
func one(row []any, ok bool) [][]any {
	if !ok {
		return nil
	}
	return [][]any{row}
}

// LoadTitles reads title.basics, keeps the movies, and returns their
// ids. Everything else in the import is filtered against that set, so
// this runs first and alone.
func (s *Store) LoadTitles(ctx context.Context, logger *slog.Logger, r io.Reader) (TConsts, int64, error) {
	reader, err := NewReader(r)
	if err != nil {
		return nil, 0, err
	}
	defer reader.Close()
	if err := reader.Require("tconst", "titleType", "primaryTitle", "originalTitle",
		"isAdult", "startYear", "runtimeMinutes", "genres"); err != nil {
		return nil, 0, err
	}
	// Sized for the movie count rather than the file's twelve million
	// rows, so the map is not rehashed on the way up.
	kept := make(TConsts, 1<<20)
	src := &rowSource{track: newProgress(logger, "loading movies", 0), r: reader, rows: func(r *Reader) [][]any {
		t, ok := ReadTitle(r)
		if !ok {
			return nil
		}
		kept.add(t.TConst)
		// A film with no genres is a film with an empty list, not a
		// null one: the column is NOT NULL, and a COPY that names the
		// column supplies the value rather than falling to its default.
		genres := t.Genres
		if genres == nil {
			genres = []string{}
		}
		return one([]any{t.TConst, t.Primary, t.Original, t.IsAdult,
			nullInt(t.StartYear), genres, nullInt(t.Runtime)}, true)
	}}
	n, err := s.copyRows(ctx, "titles",
		[]string{"tconst", "primary_title", "original_title", "is_adult",
			"start_year", "genres", "runtime_minutes"}, src)
	if err != nil {
		return nil, n, err
	}
	if err := src.Err(); err != nil {
		return nil, n, err
	}
	return kept, n, nil
}

// LoadPrincipals reads title.principals, keeping billed cast and
// directors of kept movies, and collects the people it credited.
func (s *Store) LoadPrincipals(ctx context.Context, logger *slog.Logger, r io.Reader, kept TConsts, credited NConsts) (int64, error) {
	reader, err := NewReader(r)
	if err != nil {
		return 0, err
	}
	defer reader.Close()
	if err := reader.Require("tconst", "ordering", "nconst", "category", "characters"); err != nil {
		return 0, err
	}
	src := &rowSource{track: newProgress(logger, "loading credits", 0), r: reader, rows: func(r *Reader) [][]any {
		p, ok := ReadPrincipal(r, kept.Has)
		if !ok {
			return nil
		}
		credited.add(p.NConst)
		return one([]any{p.TConst, p.Ordering, p.NConst, p.Category, nullText(p.Character)}, true)
	}}
	n, err := s.copyRows(ctx, "principals",
		[]string{"tconst", "ordering", "nconst", "category", "character"}, src)
	if err != nil {
		return n, err
	}
	return n, src.Err()
}

// LoadDirectors explodes title.crew. A director who is missing from the
// billed principals is here, which is why the file is read at all.
func (s *Store) LoadDirectors(ctx context.Context, logger *slog.Logger, r io.Reader, kept TConsts, credited NConsts) (int64, error) {
	reader, err := NewReader(r)
	if err != nil {
		return 0, err
	}
	defer reader.Close()
	if err := reader.Require("tconst", "directors"); err != nil {
		return 0, err
	}
	// One crew line becomes a row per director. The position in the
	// film's own list is kept, because that is the order the chips show.
	src := &rowSource{track: newProgress(logger, "loading directors", 0), r: reader, rows: func(r *Reader) [][]any {
		people := ReadDirectors(r, kept.Has)
		if len(people) == 0 {
			return nil
		}
		out := make([][]any, len(people))
		for i, d := range people {
			credited.add(d.NConst)
			out[i] = []any{d.TConst, d.NConst, i}
		}
		return out
	}}
	n, err := s.copyRows(ctx, "directors", []string{"tconst", "nconst", "ordering"}, src)
	if err != nil {
		return n, err
	}
	return n, src.Err()
}

// LoadRatings reads title.ratings for kept movies.
func (s *Store) LoadRatings(ctx context.Context, logger *slog.Logger, r io.Reader, kept TConsts) (int64, error) {
	reader, err := NewReader(r)
	if err != nil {
		return 0, err
	}
	defer reader.Close()
	if err := reader.Require("tconst", "averageRating", "numVotes"); err != nil {
		return 0, err
	}
	src := &rowSource{track: newProgress(logger, "loading ratings", 0), r: reader, rows: func(r *Reader) [][]any {
		rating, ok := ReadRating(r, kept.Has)
		if !ok {
			return nil
		}
		return one([]any{rating.TConst, rating.Average, rating.Votes}, true)
	}}
	n, err := s.copyRows(ctx, "ratings", []string{"tconst", "average_rating", "num_votes"}, src)
	if err != nil {
		return n, err
	}
	return n, src.Err()
}

// LoadNames reads name.basics last, keeping only people a kept credit
// named. Most of the file is known only from television.
func (s *Store) LoadNames(ctx context.Context, logger *slog.Logger, r io.Reader, credited NConsts) (int64, error) {
	reader, err := NewReader(r)
	if err != nil {
		return 0, err
	}
	defer reader.Close()
	if err := reader.Require("nconst", "primaryName"); err != nil {
		return 0, err
	}
	src := &rowSource{track: newProgress(logger, "loading people", 0), r: reader, rows: func(r *Reader) [][]any {
		n, ok := ReadName(r, credited.Has)
		if !ok {
			return nil
		}
		return one([]any{n.NConst, n.Primary}, true)
	}}
	n, err := s.copyRows(ctx, "names",
		[]string{"nconst", "primary_name"}, src)
	if err != nil {
		return n, err
	}
	return n, src.Err()
}

// nullInt writes 0 as NULL: the datasets have no year zero and no movie
// of no minutes, and a null column says "not known" where a zero would
// say "the year 0".
func nullInt(v int) any {
	if v == 0 {
		return nil
	}
	return v
}

func nullText(v string) any {
	if v == "" {
		return nil
	}
	return v
}
