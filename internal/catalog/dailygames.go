package catalog

// Cinedikt Daily's players and games.
//
// A game is kept as its moves. Every move is made inside one
// transaction that holds the game's row, so two requests for the same
// game take turns: a retried request is recognised by its key and
// answered with the game as it stands, never charged twice, and a move
// made from an old point (another tab, or one left open) is refused as
// stale. The few columns on the game's row are what the boards and the
// streak read, written with each move.

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"cinedikt/internal/daily"
)

// ErrNameTaken is a name another player already has.
var ErrNameTaken = errors.New("catalog: that name is taken")

// uniqueViolation is Postgres's code for a broken unique constraint.
const uniqueViolation = "23505"

// nameTaken is whether err is the players' unique name refusing one.
func nameTaken(err error) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.Code == uniqueViolation && pgErr.ConstraintName == "daily_players_name_key"
}

// DailyPlayer is the player behind a cookie's hash, and whether there
// is one.
func (s *Store) DailyPlayer(ctx context.Context, token []byte) (daily.Player, bool, error) {
	var p daily.Player
	err := s.pool.QueryRow(ctx, `SELECT id, name, hue FROM meta.daily_players WHERE token = $1`, token).
		Scan(&p.ID, &p.Name, &p.Hue)
	if errors.Is(err, pgx.ErrNoRows) {
		return daily.Player{}, false, nil
	}
	if err != nil {
		return daily.Player{}, false, fmt.Errorf("catalog: read daily player: %w", err)
	}
	return p, true, nil
}

// CreateDailyPlayer adds a player for a cookie's hash, or ErrNameTaken
// when somebody already has the name. A cookie that already has a player,
// from a request that raced this one, keeps the player it has.
func (s *Store) CreateDailyPlayer(ctx context.Context, token []byte, name string, hue int) (daily.Player, error) {
	var p daily.Player
	err := s.pool.QueryRow(ctx, `
		INSERT INTO meta.daily_players (token, name, hue) VALUES ($1, $2, $3)
		ON CONFLICT (token) DO NOTHING
		RETURNING id, name, hue`, token, name, hue).Scan(&p.ID, &p.Name, &p.Hue)
	switch {
	case nameTaken(err):
		return daily.Player{}, ErrNameTaken
	case errors.Is(err, pgx.ErrNoRows):
		got, ok, err := s.DailyPlayer(ctx, token)
		if err != nil {
			return daily.Player{}, err
		}
		if !ok {
			return daily.Player{}, errors.New("catalog: a daily player was neither made nor found")
		}
		return got, nil
	case err != nil:
		return daily.Player{}, fmt.Errorf("catalog: add daily player: %w", err)
	}
	return p, nil
}

// RenameDailyPlayer gives a player a new name, or ErrNameTaken.
func (s *Store) RenameDailyPlayer(ctx context.Context, id int64, name string) error {
	_, err := s.pool.Exec(ctx, `UPDATE meta.daily_players SET name = $2 WHERE id = $1`, id, name)
	if nameTaken(err) {
		return ErrNameTaken
	}
	if err != nil {
		return fmt.Errorf("catalog: rename daily player: %w", err)
	}
	return nil
}

// DailyGame is a player's game of puzzle no, or nil when they have not
// pressed Play on it.
func (s *Store) DailyGame(ctx context.Context, player int64, no int) (*daily.Record, error) {
	rec, _, err := gameOf(ctx, s.pool, player, no, false)
	return rec, err
}

// StartDailyGame makes a player's game of puzzle no, started at at in
// zone with every point, and returns it; a game they already have is
// returned as it is, its zone included, so pressing Play twice never
// restarts the clock nor moves the midnight it ends at.
func (s *Store) StartDailyGame(ctx context.Context, player int64, no int, at time.Time, zone *time.Location) (*daily.Record, error) {
	if _, err := s.pool.Exec(ctx, `
		INSERT INTO meta.daily_games (player, no, pts, started_at, zone) VALUES ($1, $2, $3, $4, $5)
		ON CONFLICT (player, no) DO NOTHING`, player, no, daily.Start, at, zone.String()); err != nil {
		return nil, fmt.Errorf("catalog: start daily game: %w", err)
	}
	rec, err := s.DailyGame(ctx, player, no)
	if err == nil && rec == nil {
		err = errors.New("catalog: a daily game was neither made nor found")
	}
	return rec, err
}

// querier is a pool or a transaction.
type querier interface {
	Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error)
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
}

// gameOf reads a game and its moves, holding its row for the rest of the
// transaction when lock says so. A game that does not exist is nil.
func gameOf(ctx context.Context, q querier, player int64, no int, lock bool) (*daily.Record, int64, error) {
	sql := `SELECT id, started_at, finished_at, zone FROM meta.daily_games WHERE player = $1 AND no = $2`
	if lock {
		sql += ` FOR UPDATE`
	}
	rec := &daily.Record{}
	var id int64
	err := q.QueryRow(ctx, sql, player, no).Scan(&id, &rec.Started, &rec.Finished, &rec.Zone)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, 0, nil
	}
	if err != nil {
		return nil, 0, fmt.Errorf("catalog: read daily game: %w", err)
	}
	rows, err := q.Query(ctx, `
		SELECT seq, key, kind, coalesce(arg, ''), cost, detail, at
		FROM meta.daily_moves WHERE game = $1 ORDER BY seq`, id)
	if err != nil {
		return nil, 0, fmt.Errorf("catalog: read daily moves: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var m daily.Move
		var detail []byte
		if err := rows.Scan(&m.Seq, &m.Key, &m.Kind, &m.Arg, &m.Cost, &detail, &m.At); err != nil {
			return nil, 0, fmt.Errorf("catalog: scan daily move: %w", err)
		}
		if detail != nil {
			m.Guess = &daily.Guessed{}
			if err := json.Unmarshal(detail, m.Guess); err != nil {
				return nil, 0, fmt.Errorf("catalog: decode daily move %d: %w", m.Seq, err)
			}
		}
		rec.Moves = append(rec.Moves, m)
	}
	return rec, id, rows.Err()
}

// DailyAct makes a move in a player's game of p, at at, and returns the
// game as it stands after it. In order, holding the game's row:
//
//   - a request whose key is already recorded is a retry, answered with
//     the game as it is, even past midnight: the move was made in time;
//   - a game whose puzzle's day has ended in the zone it was started in
//     is over, unfinished, and every move on it is daily.ErrDay;
//   - one whose seq is not the number of moves recorded was made from an
//     old point, and is refused as daily.ErrStale, with the game as it
//     is, so the page can catch up;
//   - anything else is checked by the engine, recorded, and the game's
//     row brought up to date with it.
//
// A refusal is a *daily.Refusal. A player with no game for p is
// daily.ErrNoGame.
func (s *Store) DailyAct(ctx context.Context, player int64, p *daily.Puzzle, r daily.Request, at time.Time) (*daily.Record, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("catalog: begin daily move: %w", err)
	}
	defer tx.Rollback(context.WithoutCancel(ctx))

	rec, game, err := gameOf(ctx, tx, player, p.No, true)
	if err != nil {
		return nil, err
	}
	if rec == nil {
		return nil, daily.ErrNoGame
	}
	for _, m := range rec.Moves {
		if m.Key == r.Key {
			return rec, nil
		}
	}
	// The game's own zone, never the request's: checked here, holding
	// the row, against the moment the move is recorded at.
	if !p.On(at, daily.Zone(rec.Zone)) {
		return rec, daily.ErrDay
	}
	if r.Seq != len(rec.Moves) {
		return rec, daily.ErrStale
	}
	state := daily.Replay(p, rec.Moves)
	var looked *daily.Looked
	if r.Kind == daily.KindGuess && r.Arg != p.Answer.ID && !state.Done && !state.Guessed(r.Arg) {
		if looked, err = s.lookGuess(ctx, tx, r.Arg, p.PeopleIDs()); err != nil {
			return nil, err
		}
	}
	m, err := daily.Apply(p, state, r, looked)
	if err != nil {
		return rec, err
	}
	m.Seq, m.At = len(rec.Moves)+1, at
	state.Step(p, m)

	var detail []byte
	if m.Guess != nil {
		if detail, err = json.Marshal(m.Guess); err != nil {
			return nil, err
		}
	}
	if _, err := tx.Exec(ctx, `
		INSERT INTO meta.daily_moves (game, seq, key, kind, arg, cost, detail, at)
		VALUES ($1, $2, $3, $4, nullif($5, ''), $6, $7, $8)`,
		game, m.Seq, m.Key, m.Kind, m.Arg, m.Cost, detail, m.At); err != nil {
		return nil, fmt.Errorf("catalog: record daily move: %w", err)
	}
	var finished *time.Time
	var ms *int64
	if state.Done {
		finished = &at
		took := at.Sub(rec.Started).Milliseconds()
		ms = &took
	}
	if _, err := tx.Exec(ctx, `
		UPDATE meta.daily_games
		SET pts = $2, moves = $3, finished_at = $4, won = $5, gave_up = $6, ms = $7
		WHERE id = $1`, game, state.Pts, m.Seq, finished, state.Won, state.GaveUp, ms); err != nil {
		return nil, fmt.Errorf("catalog: update daily game: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("catalog: commit daily move: %w", err)
	}
	rec.Moves = append(rec.Moves, m)
	rec.Finished = finished
	return rec, nil
}

// lookGuess is what the live catalog says about a guessed movie, with
// which of people are credited on it as actor, actress or director, or
// in its crew as director: the same credits that put a movie on a map.
// Nil when the catalog has no such title.
func (s *Store) lookGuess(ctx context.Context, q querier, tconst string, people []string) (*daily.Looked, error) {
	var l daily.Looked
	var released *time.Time
	err := q.QueryRow(ctx, `
		SELECT t.primary_title, coalesce(t.start_year, 0), r.average_rating::float8, p.released,
		       coalesce(
		           (SELECT array_agg(DISTINCT who.nconst)
		            FROM (
		                SELECT pr.nconst FROM `+Live+`.principals pr
		                WHERE pr.tconst = t.tconst AND pr.nconst = ANY($2)
		                  AND pr.category IN ('actor', 'actress', 'director')
		                UNION
		                SELECT d.nconst FROM `+Live+`.directors d
		                WHERE d.tconst = t.tconst AND d.nconst = ANY($2)
		            ) who),
		           '{}')
		FROM `+Live+`.titles t
		LEFT JOIN `+Live+`.ratings r USING (tconst)
		LEFT JOIN meta.posters p USING (tconst)
		WHERE t.tconst = $1`, tconst, people).
		Scan(&l.Title, &l.Year, &l.Rating, &released, &l.Credited)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("catalog: look up the guess %s: %w", tconst, err)
	}
	l.MD = monthDay(released)
	return &l, nil
}

// DailyLive is the posters and photos to show with a game, read fresh:
// posters as the poster jobs have them now, and photos only while
// they are younger than the 175 days anything of TMDb's is kept.
// Neither is ever copied into a puzzle.
func (s *Store) DailyLive(ctx context.Context, films, people []string) (daily.Live, error) {
	live := daily.Live{Posters: map[string]string{}, Photos: map[string]string{}}
	if len(films) > 0 {
		rows, err := s.pool.Query(ctx, `
			SELECT tconst, poster_url FROM meta.posters
			WHERE tconst = ANY($1) AND btrim(coalesce(poster_url, '')) <> ''`, films)
		if err != nil {
			return live, fmt.Errorf("catalog: daily posters: %w", err)
		}
		for rows.Next() {
			var id, url string
			if err := rows.Scan(&id, &url); err != nil {
				rows.Close()
				return live, fmt.Errorf("catalog: scan daily poster: %w", err)
			}
			live.Posters[id] = url
		}
		rows.Close()
		if err := rows.Err(); err != nil {
			return live, fmt.Errorf("catalog: daily posters: %w", err)
		}
	}
	if len(people) > 0 {
		rows, err := s.pool.Query(ctx, `
			SELECT ph.nconst, ph.profile_path FROM meta.people ph
			WHERE ph.nconst = ANY($1) AND ph.profile_path IS NOT NULL AND `+photoServed("$2"), people, tmdbForgetDays)
		if err != nil {
			return live, fmt.Errorf("catalog: daily photos: %w", err)
		}
		for rows.Next() {
			var id string
			var path *string
			if err := rows.Scan(&id, &path); err != nil {
				rows.Close()
				return live, fmt.Errorf("catalog: scan daily photo: %w", err)
			}
			live.Photos[id] = photoURL(path)
		}
		rows.Close()
		if err := rows.Err(); err != nil {
			return live, fmt.Errorf("catalog: daily photos: %w", err)
		}
	}
	return live, nil
}

// DailyPlayed is how many games of puzzle no have been started, by
// anyone: the intro's "people have played today".
func (s *Store) DailyPlayed(ctx context.Context, no int) (int, error) {
	var n int
	if err := s.pool.QueryRow(ctx, `SELECT count(*) FROM meta.daily_games WHERE no = $1`, no).Scan(&n); err != nil {
		return 0, fmt.Errorf("catalog: count daily games: %w", err)
	}
	return n, nil
}

// DailyStreak is a player's streak on puzzle no: their run of puzzles
// finished with more than nothing, ending at no or the puzzle before.
// The puzzles are read newest first, and only until the first one that
// breaks the run.
func (s *Store) DailyStreak(ctx context.Context, player int64, no int) (daily.Streak, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT z.no, coalesce(g.finished_at IS NOT NULL AND g.pts > 0, false)
		FROM meta.daily_puzzles z
		LEFT JOIN meta.daily_games g ON g.no = z.no AND g.player = $1
		WHERE z.no <= $2
		ORDER BY z.no DESC`, player, no)
	if err != nil {
		return daily.Streak{}, fmt.Errorf("catalog: daily streak: %w", err)
	}
	defer rows.Close()
	var days []daily.Played
	for rows.Next() {
		var d daily.Played
		if err := rows.Scan(&d.No, &d.Scored); err != nil {
			return daily.Streak{}, fmt.Errorf("catalog: scan daily streak: %w", err)
		}
		days = append(days, d)
		if !d.Scored && d.No != no {
			break
		}
	}
	if err := rows.Err(); err != nil {
		return daily.Streak{}, fmt.Errorf("catalog: daily streak: %w", err)
	}
	return daily.StreakOf(no, days), nil
}

// DailyNames is what players' names are made from, off every movie
// with at least daily.NameVotes votes: each character credited on one,
// and the name of each actor, actress and director credited on one, so
// two characters' words never make a real person. One query, each row
// tagged with which it is; the API keeps what it returns for a day.
//
// About fifty thousand characters and thirty-five thousand people on
// the full catalog, read in about a fifth of a second. The movies
// are inlined into both halves rather than materialised once: kept as a
// table, the planner reads all of principals for the characters, three
// times slower.
func (s *Store) DailyNames(ctx context.Context) (daily.Credits, error) {
	var c daily.Credits
	rows, err := s.pool.Query(ctx, `
		WITH known AS NOT MATERIALIZED (
		    SELECT r.tconst FROM `+Live+`.ratings r
		    JOIN `+Live+`.titles t USING (tconst)
		    WHERE r.num_votes >= $1 AND NOT t.is_adult
		)
		SELECT false, character FROM (
		    SELECT DISTINCT pr.character
		    FROM `+Live+`.principals pr JOIN known USING (tconst)
		    WHERE pr.character IS NOT NULL
		) chars
		UNION ALL
		SELECT true, primary_name FROM (
		    SELECT DISTINCT n.primary_name
		    FROM `+Live+`.principals pr JOIN known USING (tconst)
		    JOIN `+Live+`.names n USING (nconst)
		    WHERE pr.category IN ('actor', 'actress', 'director')
		) people`, daily.NameVotes)
	if err != nil {
		return c, fmt.Errorf("catalog: daily names: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var person bool
		var name string
		if err := rows.Scan(&person, &name); err != nil {
			return daily.Credits{}, fmt.Errorf("catalog: scan daily name: %w", err)
		}
		if person {
			c.People = append(c.People, name)
		} else {
			c.Characters = append(c.Characters, name)
		}
	}
	if err := rows.Err(); err != nil {
		return daily.Credits{}, fmt.Errorf("catalog: daily names: %w", err)
	}
	return c, nil
}

// eligibleSQL is the players a board lists for puzzle $1: those who
// finished at least $2 earlier puzzles (daily.EarlierGames), everyone
// when that is none, and always player $3, who sees their own place
// whether or not they are listed.
const eligibleSQL = `
	eligible AS (
	    SELECT player FROM meta.daily_games
	    WHERE no < $1 AND finished_at IS NOT NULL
	    GROUP BY player HAVING count(*) >= $2
	)`

const listedSQL = `($2 = 0 OR g.player IN (SELECT player FROM eligible))`

// weekSQL is the week's board of puzzle $1 as board, every row of it,
// ranked: each player listed for $1 (eligibleSQL, $2), and player $3
// whether or not they are, with the sum of their points over their
// finished games of the puzzles whose days run from $4 to $5, each
// day's points by its offset from $4, and their summed time, ranked by
// points, then time, then who joined first, so a tie always falls the
// same way. The This week tab reads it (DailyBoard) and so does the
// standing on the opening screens (DailyStanding), so the two can never
// rank a player differently.
const weekSQL = `
	WITH ` + eligibleSQL + `,
	week AS (
	    SELECT g.player, sum(g.pts)::int AS pts, sum(g.ms)::bigint AS ms,
	           array_agg(z.day - $4::date ORDER BY z.day) AS offsets,
	           array_agg(g.pts ORDER BY z.day) AS points,
	           bool_or(` + listedSQL + `) AS listed
	    FROM meta.daily_games g
	    JOIN meta.daily_puzzles z ON z.no = g.no
	    WHERE z.day BETWEEN $4::date AND $5::date AND g.finished_at IS NOT NULL
	      AND (` + listedSQL + ` OR g.player = $3)
	    GROUP BY g.player
	), board AS (
	    SELECT w.*, pl.name, pl.hue,
	           row_number() OVER (ORDER BY w.pts DESC, w.ms, w.player) AS rank
	    FROM week w JOIN meta.daily_players pl ON pl.id = w.player
	)`

// DailyBoard is one tab of puzzle p's leaderboard, as player sees it; 0
// is a reader with no player, who sees the top of it.
//
// Today ranks the finished games of p by points, then time: every game
// of puzzle No. p.No, whichever zone it was played in, so the board stays
// open while the zones roll through its day, about fifty hours. The
// week ranks each player's sum of points over the puzzles whose days
// fall in the ISO week of p's day, up to and including it, then their
// summed time, with each day's points: a week of puzzle dates, never of
// when the games were played. Only the rows the board shows are read:
// the top daily.TopRows and those around the player.
func (s *Store) DailyBoard(ctx context.Context, p *daily.Puzzle, tab string, player int64) (daily.Board, error) {
	need := daily.EarlierGames(p.No)
	var rows pgx.Rows
	var err error
	if tab == daily.TabWeek {
		rows, err = s.pool.Query(ctx, weekSQL+`
			SELECT player, name, hue, pts, ms, offsets, points, rank, listed, (SELECT count(*) FROM board)
			FROM board
			WHERE rank <= $6 OR abs(rank - (SELECT rank FROM board WHERE player = $3)) <= $7
			ORDER BY rank`,
			p.No, need, player, daily.Monday(p.Day), p.Day, daily.TopRows, daily.Around)
	} else {
		rows, err = s.pool.Query(ctx, `
			WITH `+eligibleSQL+`,
			board AS (
			    SELECT g.player, pl.name, pl.hue, g.pts, g.ms::bigint AS ms,
			           `+listedSQL+` AS listed,
			           row_number() OVER (ORDER BY g.pts DESC, g.ms, g.player) AS rank
			    FROM meta.daily_games g
			    JOIN meta.daily_players pl ON pl.id = g.player
			    WHERE g.no = $1 AND g.finished_at IS NOT NULL
			      AND (`+listedSQL+` OR g.player = $3)
			)
			SELECT player, name, hue, pts, ms, NULL::int[], NULL::int[], rank, listed, (SELECT count(*) FROM board)
			FROM board
			WHERE rank <= $4 OR abs(rank - (SELECT rank FROM board WHERE player = $3)) <= $5
			ORDER BY rank`,
			p.No, need, player, daily.TopRows, daily.Around)
	}
	if err != nil {
		return daily.Board{}, fmt.Errorf("catalog: daily board: %w", err)
	}
	b := daily.Board{Tab: tab, Rows: []daily.Row{}}
	var ranked []daily.Ranked
	for rows.Next() {
		var r daily.Ranked
		var offsets, points []int
		var listed bool
		if err := rows.Scan(&r.Player, &r.Name, &r.Hue, &r.Pts, &r.MS, &offsets, &points, &r.Rank, &listed, &b.Total); err != nil {
			rows.Close()
			return daily.Board{}, fmt.Errorf("catalog: scan daily board: %w", err)
		}
		if tab == daily.TabWeek {
			r.Days = make([]*int, 7)
			for i, off := range offsets {
				if off >= 0 && off < 7 {
					r.Days[off] = &points[i]
				}
			}
		}
		if player != 0 && r.Player == player {
			b.You = &daily.You{Rank: r.Rank, Pts: r.Pts, Secs: daily.Secs(r.MS), Listed: listed, Days: r.Days}
		}
		ranked = append(ranked, r)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return daily.Board{}, fmt.Errorf("catalog: daily board: %w", err)
	}
	b.Rows = daily.Lay(ranked, player, tab)
	if tab != daily.TabWeek {
		var finished, won, below int
		mine := -1
		if b.You != nil {
			mine = b.You.Pts
		}
		if err := s.pool.QueryRow(ctx, `
			SELECT count(*), count(*) FILTER (WHERE won), count(*) FILTER (WHERE pts < $2)
			FROM meta.daily_games WHERE no = $1 AND finished_at IS NOT NULL`, p.No, mine).
			Scan(&finished, &won, &below); err != nil {
			return daily.Board{}, fmt.Errorf("catalog: daily board counts: %w", err)
		}
		b.Solved = daily.Percent(won, finished)
		if b.You != nil {
			b.Beat = daily.Percent(below, finished)
		}
	}
	return b, nil
}

// DailyStanding is player's place on the week's board of puzzle p, as
// the opening screens show it before any board: the same players, in
// the same order, as p's This week tab (weekSQL), listed by p's own
// rule, so what the opening screens say is what the result shows. Until
// player has finished p it adds up only the puzzles of p's week before
// p's day, so not having played yet never counts against them; once
// they have, it is exactly the tab they are shown (daily.StandingDays).
// Nil when player is not on that board, or is with nothing, as on a
// Monday before playing.
//
// It ranks the whole board to read one row, which is what the tab does
// too; the API keeps the answer for a minute.
func (s *Store) DailyStanding(ctx context.Context, p *daily.Puzzle, player int64, finished bool) (*daily.Week, error) {
	from, through := daily.StandingDays(p.Day, finished)
	if through.Before(from) {
		return nil, nil
	}
	var w daily.Week
	var pts int
	err := s.pool.QueryRow(ctx, weekSQL+`
		SELECT rank, pts, (SELECT count(*) FROM board) FROM board WHERE player = $3`,
		p.No, daily.EarlierGames(p.No), player, from, through).Scan(&w.Rank, &pts, &w.Players)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("catalog: daily standing: %w", err)
	}
	if pts <= 0 {
		return nil, nil
	}
	return &w, nil
}
