// Package daily is Cinedikt Daily's rules, with no database and no
// clock of its own. The game is Name Drop: one hidden movie a day, whose
// cast shows up one name at a time, from sixth-billed up to the star,
// and the player spends points on names, facts and wrong guesses until
// they name it. What is left is the score.
//
// The server owns the game. A game is the list of moves recorded for
// it, and everything else about it (the points left, which names are
// showing, which facts are bought) is worked out by replaying those
// moves through the engine here. The page only ever draws what a replay
// says it may see: until the game ends it is never told the answer, nor
// anyone in the cast it has not been shown, nor a fact it has not
// bought.
//
// The catalog package picks each day's puzzle with the functions in
// pick.go and keeps the games; the API renders them with view.go. Both
// call in here, so the rules are written once.
package daily

import (
	"math"
	"time"
)

// Start is the points every game begins with. The score is what is left,
// so the most anyone can score is Start.
const Start = 1000

// NameCost is what the next name costs: one more of the cast, in reveal
// order.
const NameCost = 100

// What each fact costs. Length and rating are cheapest: each is one of
// four wide bands. Genre and the decade narrow further. The director is
// dearest, because a name can be searched: a filmography is a few steps
// from the answer.
//
// The rule for any fact added here: none may give the answer away in a
// single search. So no plot, no tagline, no quote and no characters'
// names, since any line of a movie's text can be pasted into a search
// engine; and no exact number while the game is on, so the length,
// rating and year come only as ranges, and no one value can be checked
// against a candidate. "How it starts", the first sentence of the
// synopsis, was retired for exactly that: The Matrix's named Neo and
// Morpheus.
//
// Nor may the facts together leave a Movies sheet with nothing but the
// answer inside them. A sheet shows every card's year and rating, so a
// fact narrows it by eye as well as by what it reads. Name Drop sold the
// five years inside the decade too, and with them, the rating and the
// genre bought, a cast member's sheet kept almost nothing else: two of
// the 7,181 movies with 25,000 votes kept three others on every one of
// their six's sheets. So the decade is as fine as the year goes, and
// each of the six's sheets keeps MinCrowd others inside it and the
// rating band (Build).
const (
	LengthCost   = 50
	RatingCost   = 50
	GenreCost    = 100
	DecadeCost   = 100
	DirectorCost = 250
)

// There is no move that puts two people on one Movies sheet. Name Drop
// first sold one, an "overlap" that lit only the movies two names
// shared, and the answer is the one movie all six share: two or three
// names in, it was most of the way to the answer for 250 points. A sheet
// now reads only inside the ranges bought (view.go).
//
// Nor may two sheets be read side by side while the game is on, which
// is the overlap again by hand: inside a range or two the readable
// titles on two people's sheets almost never have more than today's
// movie in common. So a game opens one Movies sheet, chosen once, for
// nothing, from the names showing (KindSheet), and until the end it is
// the only one the server will send.

// The first wrong guess costs WrongCost, and each one after it
// WrongStep more, so guessing never beats asking for the next name.
const (
	WrongCost = 100
	WrongStep = 50
)

// NextWrong is what the next wrong guess costs after wrong of them.
func NextWrong(wrong int) int {
	return WrongCost + WrongStep*wrong
}

// RelativeShared is how many of the answer's people a movie has to share
// with it to be a close relative: usually a sequel, which would give the
// answer away. No one's "Also in" movie is ever one.
const RelativeShared = 3

// Hues are the colours of the answer's people, in their places on the
// movie: the cast in billing order, the star first, then the directors.
// Everyone keeps one colour all game. The page makes the colour from the
// hue, oklch(0.76 0.13 h) in dark and oklch(0.56 0.16 h) in light; the
// server only says which.
var Hues = []int{232, 28, 345, 150, 78, 205, 118, 255, 180, 52, 5, 128}

// HueAt is the hue of the person at place i on the movie, counting the
// star as 0, then down the billing, then the directors.
func HueAt(i int) int {
	return Hues[((i%len(Hues))+len(Hues))%len(Hues)]
}

// The bands the length and rating facts are sold as. The page has the
// words ("Under 1h 30m"); the API says which band, 0 to 3.

// LengthBand is a runtime in minutes as one of four bands: under 90, 90
// to 119, 120 to 149, and 150 or more.
func LengthBand(minutes int) int {
	switch {
	case minutes < 90:
		return 0
	case minutes < 120:
		return 1
	case minutes < 150:
		return 2
	}
	return 3
}

// ratingFloors are where the rating bands above the first begin, in
// tenths: 6.0, 7.0 and 8.0.
var ratingFloors = [...]int{60, 70, 80}

// RatingBand is an IMDb rating as one of four bands: below 6.0, 6.0 to
// 6.9, 7.0 to 7.9, and 8.0 or higher. Compared in tenths, which is all
// IMDb gives, so 7.0 kept as 6.9999 is still 7.0.
func RatingBand(rating float64) int {
	tenths := int(math.Round(rating * 10))
	band := 0
	for _, floor := range ratingFloors {
		if tenths >= floor {
			band++
		}
	}
	return band
}

// RatingFloors are where the rating bands above the first begin, as
// ratings: 6.0, 7.0 and 8.0. They are what a query bands a rating by,
// with Postgres's width_bucket, which counts the floors at or below it
// just as RatingBand does, so the bands are written once.
func RatingFloors() []float64 {
	out := make([]float64, len(ratingFloors))
	for i, floor := range ratingFloors {
		out[i] = float64(floor) / 10
	}
	return out
}

// Decade is the decade fact, the first year of it: 1999 is 1990.
func Decade(year int) int { return year / 10 * 10 }

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
	ErrKnown   = refuse(409, "known", "that is already showing, bought or guessed")
	ErrDone    = refuse(409, "done", "the game is over")
	// ErrStale is a move made from an old point: another tab, or one
	// left open, has moved the game on since. The API sends the game as
	// it stands with it, so the page can catch up.
	ErrStale = refuse(409, "stale", "the game has moved on since that page last heard")
	// ErrDay is a move on a puzzle whose day is not the player's: for a
	// game, its day in the zone it was started in, which has ended.
	ErrDay = refuse(409, "day", "that puzzle is not today's")
	// ErrSheet is a Movies sheet asked for while the game is on that is
	// not the one the player opened: anyone's before they open one, and
	// anyone else's after.
	ErrSheet = refuse(409, "sheet", "only the Movies map you opened")
)
