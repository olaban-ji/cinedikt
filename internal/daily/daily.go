// Package daily is Cinedikt Daily's rules, with no database and no
// clock of its own: one hidden movie a day, every movie on its map
// shares an actor or director with it, and the player spends points on
// clues until they name it.
//
// The server owns the game. A game is the list of moves recorded for
// it, and everything else about it (the points left, which cards are
// face up, who is known) is worked out by replaying those moves through
// the engine here. The page only ever draws what a replay says it may
// see: until the game ends it is never told the answer, nor what is on a
// card it has not turned over.
//
// The catalog package picks each day's puzzle with the functions in
// pick.go and keeps the games; the API renders them with view.go. Both
// call in here, so the rules are written once.
package daily

import (
	"math"
	"time"
)

// Start is the points every game begins with. The score is what is left.
const Start = 1000

// What each clue costs. A director and an actor cost the same: either
// is one person, and which one gives more away depends on the map.
// Reading how the movie starts is dearest, because a first sentence
// often all but names it.
const (
	DirectorCost = 150
	ActorCost    = 150
	GenresCost   = 80
	StoryCost    = 300
)

// The first wrong guess costs WrongCost, and each one after it
// WrongStep more, so fishing for clues by guessing gets dear fast.
const (
	WrongCost = 100
	WrongStep = 50
)

// FlipCost is what turning over a card costs: dearer the better it is
// rated, because a well-rated movie tends to be a well-known one, and
// knowing it is most of the way to placing its people. 20 to 80, in
// steps of five.
func FlipCost(rating float64) int {
	return clamp(round5(20+(rating-4.5)*13), 20, 80)
}

// round5 rounds to the nearest five, a half away from zero: 52.5 is 55,
// which is the prototype's Math.round for every rating there is.
func round5(x float64) int {
	return int(math.Round(x/5)) * 5
}

func clamp(n, lo, hi int) int {
	return max(lo, min(hi, n))
}

// NextWrong is what the next wrong guess costs after wrong of them.
func NextWrong(wrong int) int {
	return WrongCost + WrongStep*wrong
}

// RelativeShared is how many of the answer's people a card has to share
// with it to be a close relative: usually a sequel, which would give the
// answer away. Its face says only how many it shares.
const RelativeShared = 3

// dayLayout is how a puzzle's day is written: in the API, in the
// database and as the seed of its shuffles.
const dayLayout = "2006-01-02"

// A puzzle belongs to a calendar date, and each reader plays the one for
// their own date, which changes at their own midnight, as Wordle does.
// The date is worked out on the server, from its own clock and the time
// zone the page names, and never from the device's clock: winding a
// phone forward opens nothing early, and claiming another zone moves a
// reader a day at most, the distance from UTC−12 to UTC+14. While two
// dates are current somewhere on Earth, two puzzles are live at once,
// each for the readers on its date.
//
// A day is kept as its date at midnight UTC, whichever zone it was
// worked out in, so days compare, store and print the same everywhere.

// DayIn is the puzzle day a moment falls in for a reader in zone: their
// calendar date there.
func DayIn(now time.Time, zone *time.Location) time.Time {
	y, m, d := now.In(zone).Date()
	return time.Date(y, m, d, 0, 0, 0, 0, time.UTC)
}

// Today is UTC's date at a moment: the day the job counts its picks
// from, and how a date read back from the database is kept.
func Today(now time.Time) time.Time {
	return DayIn(now, time.UTC)
}

// Next is the midnight that ends a reader's day in zone, which the page
// counts down to. It is worked out from the date rather than by adding a
// day's hours, so a day a clock change makes 23 or 25 hours long still
// ends at midnight. Where the change skips midnight itself (Santiago,
// Havana), time.Date lands on the hour before the gap, which is still
// the old day; the new day begins when the clock jumps.
func Next(now time.Time, zone *time.Location) time.Time {
	y, m, d := now.In(zone).Date()
	next := time.Date(y, m, d+1, 0, 0, 0, 0, zone)
	if DayIn(next, zone).Equal(DayIn(now, zone)) {
		if _, jump := next.ZoneBounds(); !jump.IsZero() {
			next = jump
		}
	}
	return next
}

// Current is whether day is somebody's date at now: whether it has
// begun at UTC+14, where every date begins first, and not yet ended at
// UTC−12, where it ends last. Up to three dates are current at once,
// and only a current day's puzzle can be played anywhere.
func Current(day, now time.Time) bool {
	return !day.After(Today(now.Add(14*time.Hour))) && !day.Before(Today(now.Add(-12*time.Hour)))
}

// ZoneMax is the longest time zone name taken. The longest in the IANA
// database is about thirty characters, so anything past this names no
// zone and is not worth asking the zone database about.
const ZoneMax = 64

// Zone is the time zone the page names, as Intl reports it ("Asia/Tokyo"),
// or UTC when the name is missing, too long, or not a zone this server
// knows: a reader whose zone cannot be read plays UTC's day, which is
// never more than a day from their own. "Local" is the server's own
// zone, never a reader's, so it is UTC too. time.LoadLocation refuses a
// path ("../../etc/passwd") on its own, and the zone database is
// embedded in the binary, so a name never reaches a file it should not.
func Zone(name string) *time.Location {
	if name == "" || len(name) > ZoneMax || name == "Local" {
		return time.UTC
	}
	zone, err := time.LoadLocation(name)
	if err != nil {
		return time.UTC
	}
	return zone
}

// DayString is a puzzle day as the API writes it: "2026-10-08".
func DayString(day time.Time) string {
	return day.UTC().Format(dayLayout)
}

// Monday is the first day of the ISO week a puzzle day falls in. The
// week's board adds up the puzzles whose days fall from here to the
// day it is read for: a week of puzzle dates, never of clock time, so a
// game counts in its puzzle's week wherever and whenever it was played.
func Monday(day time.Time) time.Time {
	day = Today(day)
	back := (int(day.Weekday()) + 6) % 7
	return day.AddDate(0, 0, -back)
}

// Number is a puzzle day's number, counting the first puzzle ever made
// as No. 1. A day nothing was picked for still uses its number up, so
// the number always says how long the game has been running.
func Number(day, first time.Time) int {
	return daysBetween(Today(first), Today(day)) + 1
}

// daysBetween is how many calendar days b is after a. Both are UTC
// midnights, so the division is exact.
func daysBetween(a, b time.Time) int {
	return int(b.Sub(a).Hours() / 24)
}

// Refusal is a move the game will not make, as the status and reason
// the API answers with. The page never shows Msg; it maps Reason to its
// own copy.
type Refusal struct {
	Status int
	Reason string
	Msg    string
}

func (r *Refusal) Error() string { return "daily: " + r.Msg }

func refuse(status int, reason, msg string) *Refusal {
	return &Refusal{Status: status, Reason: reason, Msg: msg}
}

// The refusals the engine and the store make. The API adds its own for
// a request it cannot read, a missing cookie and the rest.
var (
	ErrBad     = refuse(400, "bad", "that is not a move this game has")
	ErrPoints  = refuse(402, "points", "there are not enough points left for that")
	ErrNoGame  = refuse(404, "no-game", "there is no game for this puzzle yet")
	ErrUnknown = refuse(404, "unknown", "that movie is not in the catalog")
	ErrKnown   = refuse(409, "known", "that has already been turned over, bought or guessed")
	ErrDone    = refuse(409, "done", "the game is over")
	// ErrStale is a move made from an old point: another tab, or one
	// left open, has moved the game on since. The API sends the game as
	// it stands with it, so the page can catch up.
	ErrStale = refuse(409, "stale", "the game has moved on since that page last heard")
	// ErrDay is a move on a puzzle whose day is not the player's: for a
	// game, its day in the zone it was started in, which has ended.
	ErrDay = refuse(409, "day", "that puzzle is not today's")
)
