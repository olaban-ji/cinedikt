package daily

import (
	"cmp"
	"math"
	"slices"
	"time"
)

// The leaderboards, the streak, and the reader's standing on the start
// screen's banner.

// Board tabs: today's puzzle, and the week so far.
const (
	TabToday = "today"
	TabWeek  = "week"
)

// EarlierGames is how many earlier puzzles a player must have finished
// to be listed on a board: a private window is a new player, and a
// board anyone could top on their first try would be a board of first
// tries. Fewer while there have been fewer earlier puzzles than that,
// so the first days' boards are not empty.
func EarlierGames(no int) int {
	return max(0, min(3, no-1))
}

// Ranked is one player on a board, placed: today's points, or the
// week's sum with each day's points from Monday to the puzzle's day, 0
// for a day not played. Place is a competition rank, one more than the
// players on the board with more points, so equal scores share a place,
// and Tied is whether anyone else on the board has the same points. Both
// are worked out over the whole board, which a few of its rows cannot
// say, so they come with each row.
type Ranked struct {
	Place  int
	Tied   bool
	Player int64
	Name   string
	Hue    int
	Pts    int
	Days   []int
}

// Place gives each of a whole board its competition place and whether
// it is tied, as the store's window functions do: the rule written
// once, for the tests and for anything that ranks a board in memory.
func Place(board []Ranked) {
	for i := range board {
		above, same := 0, 0
		for _, o := range board {
			switch {
			case o.Pts > board[i].Pts:
				above++
			case o.Pts == board[i].Pts:
				same++
			}
		}
		board[i].Place, board[i].Tied = above+1, same > 1
	}
}

// Row is a line of a board as the page draws it. Days is the week's
// points, Monday first; the today tab has none.
type Row struct {
	Place int    `json:"place"`
	Tied  bool   `json:"tied"`
	Name  string `json:"name"`
	Hue   int    `json:"hue"`
	Pts   int    `json:"pts"`
	Days  []int  `json:"days,omitempty"`
	You   bool   `json:"you"`
}

// You is the player's own place on a board.
type You struct {
	Place int   `json:"place"`
	Tied  bool  `json:"tied"`
	Pts   int   `json:"pts"`
	Days  []int `json:"days,omitempty"`
}

// ChartBars is how many bars the result's chart of today's scores has:
// a hundred points each, 0–99 up to 900–999, and 1,000 on its own.
const ChartBars = 11

// Bar is the bar of the chart a score falls in.
func Bar(pts int) int {
	return min(ChartBars-1, max(0, pts)/100)
}

// Board is one tab of the leaderboard.
type Board struct {
	Tab string `json:"tab"`
	// Total is how many players the board ranks: those listed, and the
	// player when they are not.
	Total int  `json:"total"`
	You   *You `json:"you"`
	// Rows are the players around the reader, never the top: a top spot
	// would be the one reason to look answers up. Empty for a reader who
	// is not on the board.
	Rows []Row `json:"rows"`
	// Beat is, today, the share of everyone who finished (listed or not)
	// with fewer points than the player; Solved the share of finished
	// games that were won. Both percentages, null on the week's tab and
	// when there is nothing to count.
	Beat   *int `json:"beat"`
	Solved *int `json:"solved"`
	// Chart is, today, how many finished games (listed or not) scored in
	// each of the ChartBars bars; null on the week's tab.
	Chart []int `json:"chart"`
}

// Around is the rows a board shows the player me from its ranked
// players, by points, ties falling to who joined first. Today: up to two
// players just above them, with more points, the nearest first; them;
// up to one other on their score; and up to one just below. This week:
// the two ahead of them and the two behind, as the week's order has
// them. Nobody, for a player not on the board. The ranked players may
// be the whole board or only the rows the store reads around me; the
// rule picks the same lines either way.
func Around(ranked []Ranked, me int64, tab string) []Row {
	order := slices.Clone(ranked)
	slices.SortFunc(order, func(a, b Ranked) int { return cmp.Or(cmp.Compare(b.Pts, a.Pts), cmp.Compare(a.Player, b.Player)) })
	at := slices.IndexFunc(order, func(r Ranked) bool { return me != 0 && r.Player == me })
	if at < 0 {
		return []Row{}
	}
	var shown []Ranked
	if tab == TabWeek {
		shown = order[max(0, at-2):min(len(order), at+3)]
	} else {
		mine := order[at].Pts
		var above, same, below []Ranked
		for i, r := range order {
			switch {
			case i == at:
			case r.Pts > mine:
				above = append(above, r)
			case r.Pts == mine:
				same = append(same, r)
			default:
				below = append(below, r)
			}
		}
		shown = append(shown, above[max(0, len(above)-2):]...)
		shown = append(shown, order[at])
		shown = append(shown, same[:min(1, len(same))]...)
		shown = append(shown, below[:min(1, len(below))]...)
	}
	rows := make([]Row, len(shown))
	for i, r := range shown {
		rows[i] = Row{Place: r.Place, Tied: r.Tied, Name: r.Name, Hue: r.Hue, Pts: r.Pts, You: r.Player == me}
		if tab == TabWeek {
			rows[i].Days = r.Days
		}
	}
	return rows
}

// Percent is part of whole as a whole percentage, or nil when there is
// no whole.
func Percent(part, whole int) *int {
	if whole <= 0 {
		return nil
	}
	p := int(math.Round(100 * float64(part) / float64(whole)))
	return &p
}

// Streak is the player's run of days that scored, as the page shows it.
// "Today" is the reader's own puzzle, the one for their date: Now is
// the run ending at it, once it has scored; otherwise Before, the run
// that ended at the puzzle before, which theirs can still extend.
type Streak struct {
	Now    int `json:"now"`
	Before int `json:"before"`
}

// Played is one puzzle and whether the player finished it with more
// than nothing.
type Played struct {
	No     int
	Scored bool
}

// StreakOf is the streak on puzzle No. today, the reader's, from every
// puzzle up to it, newest first. Consecutive means consecutive puzzles: a day the job
// missed has no puzzle to play, and breaks nobody's run.
func StreakOf(today int, days []Played) Streak {
	run := func(from int) int {
		n := 0
		for _, d := range days[from:] {
			if !d.Scored {
				break
			}
			n++
		}
		return n
	}
	for i, d := range days {
		if d.No > today {
			continue
		}
		if d.No == today && d.Scored {
			return Streak{Now: run(i)}
		}
		if d.No == today {
			continue
		}
		return Streak{Before: run(i)}
	}
	return Streak{}
}

// Shown is the streak GET /daily/me answers with: the run ending at the
// reader's puzzle once they have finished it, and until then the run
// they can still extend by finishing it. A game finished with nothing
// shows nothing: the run it ended is over.
func (s Streak) Shown(finished bool) int {
	if finished {
		return s.Now
	}
	return s.Before
}

// Neither opening screen shows a leaderboard: today's would be strangers
// the reader cannot be on yet, and it would start them off behind. The
// start screen's banner shows the reader's own standing instead, their
// place on this week's board, which the result's This week tab then
// shows them on.

// Week is the reader's place on the week's board: their competition
// rank by points, one more than the players with more, so equal totals
// share it as the This week tab's places do, and how many players the
// board ranks, the reader among them.
type Week struct {
	Rank    int `json:"rank"`
	Players int `json:"players"`
}

// StandingDays are the puzzle days the reader's standing adds up: from
// the Monday of the ISO week their puzzle's day falls in, through their
// puzzle's day once they have finished it, and through the day before
// until then, so not having played yet never counts against them. On a
// Monday before playing, through is the Sunday before from, and no day
// counts at all.
func StandingDays(day time.Time, finished bool) (from, through time.Time) {
	day = Today(day)
	if !finished {
		return Monday(day), day.AddDate(0, 0, -1)
	}
	return Monday(day), day
}
