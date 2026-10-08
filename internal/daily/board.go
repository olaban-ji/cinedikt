package daily

import (
	"encoding/json"
	"math"
	"slices"
	"time"
)

// The leaderboards, the streak, and the reader's standing on the
// opening screens.

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

// TopRows is how many of the best a board always shows, and Around how
// many either side of the player.
const (
	TopRows = 5
	Around  = 2
)

// Ranked is one player's place on a board: today's points and time, or
// the week's sums with each day's points, Monday first, nil for a day
// not played.
type Ranked struct {
	Rank   int
	Player int64
	Name   string
	Hue    int
	Pts    int
	MS     int64
	Days   []*int
}

// Row is a line of a board as the page draws it, or the gap between two
// that are not next to each other.
type Row struct {
	Gap  bool
	Rank int
	Name string
	Hue  int
	Pts  int
	// Secs is today's time; Days the week's points, Monday first.
	Secs *int
	Days []*int
	You  bool
}

// MarshalJSON writes a gap as {"gap": true} and a row as a row.
func (r Row) MarshalJSON() ([]byte, error) {
	if r.Gap {
		return []byte(`{"gap":true}`), nil
	}
	return json.Marshal(struct {
		Rank int    `json:"rank"`
		Name string `json:"name"`
		Hue  int    `json:"hue"`
		Pts  int    `json:"pts"`
		Secs *int   `json:"secs,omitempty"`
		You  bool   `json:"you"`
		Days []*int `json:"days,omitempty"`
	}{r.Rank, r.Name, r.Hue, r.Pts, r.Secs, r.You, r.Days})
}

// You is the player's own place, whether or not they are listed.
type You struct {
	Rank   int    `json:"rank"`
	Pts    int    `json:"pts"`
	Secs   int    `json:"secs"`
	Listed bool   `json:"listed"`
	Days   []*int `json:"days,omitempty"`
}

// Board is one tab of the leaderboard.
type Board struct {
	Tab string `json:"tab"`
	// Total is how many players the board ranks: those listed, and the
	// player when they are not.
	Total int   `json:"total"`
	You   *You  `json:"you"`
	Rows  []Row `json:"rows"`
	// Beat is, today, the share of everyone who finished (listed or not)
	// with fewer points than the player; Solved the share of finished
	// games that were won. Both percentages, null on the week's tab and
	// when there is nothing to count.
	Beat   *int `json:"beat"`
	Solved *int `json:"solved"`
}

// Lay is the rows a board shows from its ranked players: the top
// TopRows, and Around either side of me, with a gap wherever the ranks
// jump. The ranked players may be all of them or only those rows; the
// rule picks the same lines either way.
func Lay(ranked []Ranked, me int64, tab string) []Row {
	mine := 0
	for _, r := range ranked {
		if me != 0 && r.Player == me {
			mine = r.Rank
		}
	}
	shown := slices.Clone(ranked)
	shown = slices.DeleteFunc(shown, func(r Ranked) bool {
		return r.Rank > TopRows && (mine == 0 || r.Rank < mine-Around || r.Rank > mine+Around)
	})
	slices.SortFunc(shown, func(a, b Ranked) int { return a.Rank - b.Rank })
	rows := make([]Row, 0, len(shown)+2)
	prev := 0
	for _, r := range shown {
		if r.Rank > prev+1 {
			rows = append(rows, Row{Gap: true})
		}
		prev = r.Rank
		row := Row{Rank: r.Rank, Name: r.Name, Hue: r.Hue, Pts: r.Pts, You: mine != 0 && r.Rank == mine}
		if tab == TabWeek {
			row.Days = r.Days
		} else {
			secs := Secs(r.MS)
			row.Secs = &secs
		}
		rows = append(rows, row)
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

// Shown is the streak the title screen shows: the run ending at the
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
// the reader cannot be on yet, and it would start them off behind. They
// show the reader's own standing instead, their place on this week's
// board, which the result's This week tab then shows them on.

// Week is the reader's place on the week's board: their rank, and how
// many players the board ranks, the reader among them.
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
