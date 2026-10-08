package daily

import (
	"encoding/json"
	"fmt"
	"testing"
	"time"
)

func ranked(n int) []Ranked {
	out := make([]Ranked, n)
	for i := range out {
		out[i] = Ranked{Rank: i + 1, Player: int64(100 + i), Name: fmt.Sprintf("Player %d", i+1), Pts: 1000 - 10*i, MS: int64(60000 + 1000*i)}
	}
	return out
}

func layout(rows []Row) string {
	s := ""
	for _, r := range rows {
		switch {
		case r.Gap:
			s += "··· "
		case r.You:
			s += fmt.Sprintf("[%d] ", r.Rank)
		default:
			s += fmt.Sprintf("%d ", r.Rank)
		}
	}
	return s
}

// TestABoardShowsTheTopAndAroundYou, with a gap wherever the ranks jump
// and none where they meet.
func TestABoardShowsTheTopAndAroundYou(t *testing.T) {
	for _, c := range []struct {
		total int
		me    int64
		want  string
	}{
		{20, 0, "1 2 3 4 5 "},
		{3, 0, "1 2 3 "},
		{20, 101, "1 [2] 3 4 5 "},
		{20, 106, "1 2 3 4 5 6 [7] 8 9 "},
		{20, 107, "1 2 3 4 5 6 7 [8] 9 10 "},
		{20, 108, "1 2 3 4 5 ··· 7 8 [9] 10 11 "},
		{20, 119, "1 2 3 4 5 ··· 18 19 [20] "},
		{20, 999, "1 2 3 4 5 "},
	} {
		if got := layout(Lay(ranked(c.total), c.me, TabToday)); got != c.want {
			t.Errorf("%d players, me %d: %q, want %q", c.total, c.me, got, c.want)
		}
	}
	// The rows the store selects lay out the same as all of them.
	all := ranked(40)
	some := append(append([]Ranked{}, all[:5]...), all[18:23]...)
	if a, b := layout(Lay(all, 120, TabToday)), layout(Lay(some, 120, TabToday)); a != b {
		t.Errorf("all the rows lay out as %q, the store's as %q", a, b)
	}
}

func TestARowSaysWhatItsTabShows(t *testing.T) {
	r := ranked(1)
	pts := 610
	r[0].Days = []*int{&pts, nil, nil, nil, nil, nil, nil}
	r[0].MS = 95400
	today, _ := json.Marshal(Lay(r, 100, TabToday))
	if want := `[{"rank":1,"name":"Player 1","hue":0,"pts":1000,"secs":95,"you":true}]`; string(today) != want {
		t.Errorf("today = %s, want %s", today, want)
	}
	week, _ := json.Marshal(Lay(r, 0, TabWeek))
	if want := `[{"rank":1,"name":"Player 1","hue":0,"pts":1000,"you":false,"days":[610,null,null,null,null,null,null]}]`; string(week) != want {
		t.Errorf("week = %s, want %s", week, want)
	}
	gap, _ := json.Marshal(Row{Gap: true, Rank: 9})
	if string(gap) != `{"gap":true}` {
		t.Errorf("gap = %s", gap)
	}
}

func TestABoardListsPlayersWithThreeEarlierGames(t *testing.T) {
	for no, want := range map[int]int{1: 0, 2: 1, 3: 2, 4: 3, 142: 3} {
		if got := EarlierGames(no); got != want {
			t.Errorf("EarlierGames(%d) = %d, want %d", no, got, want)
		}
	}
}

func TestPercentIsWholeOrNull(t *testing.T) {
	if p := Percent(1, 3); p == nil || *p != 33 {
		t.Errorf("Percent(1, 3) = %v", p)
	}
	if p := Percent(2, 3); p == nil || *p != 67 {
		t.Errorf("Percent(2, 3) = %v", p)
	}
	if p := Percent(0, 0); p != nil {
		t.Errorf("Percent(0, 0) = %d, want null", *p)
	}
}

// TestTheStreakIsConsecutivePuzzlesThatScored, ending today once today
// has scored, or yesterday while it has not.
func TestTheStreakIsConsecutivePuzzlesThatScored(t *testing.T) {
	days := func(scored ...bool) []Played {
		out := make([]Played, len(scored))
		for i, s := range scored {
			out[i] = Played{No: 10 - i, Scored: s}
		}
		return out
	}
	for _, c := range []struct {
		name string
		days []Played
		want Streak
	}{
		{"today and two before", days(true, true, true, false, true), Streak{Now: 3}},
		{"not today yet", days(false, true, true, false), Streak{Before: 2}},
		{"today scored nothing", days(false, true, true, true), Streak{Before: 3}},
		{"nothing yesterday", days(false, false, true), Streak{}},
		{"every day", days(true, true, true, true), Streak{Now: 4}},
		{"no games", nil, Streak{}},
		// A day with no puzzle is not in the list, so it breaks nothing.
		{"a missed day", []Played{{10, true}, {9, true}, {7, true}, {6, false}}, Streak{Now: 3}},
		// Today's puzzle not picked yet: the run up to yesterday.
		{"no puzzle today", []Played{{9, true}, {8, true}}, Streak{Before: 2}},
	} {
		if got := StreakOf(10, c.days); got != c.want {
			t.Errorf("%s: %+v, want %+v", c.name, got, c.want)
		}
	}
}

// TestTheTitleScreenShowsTodaysRunOnceTodayIsFinished, and until then
// the run today can still extend; finished with nothing, the run is
// over.
func TestTheTitleScreenShowsTodaysRunOnceTodayIsFinished(t *testing.T) {
	for _, c := range []struct {
		name     string
		streak   Streak
		finished bool
		want     int
	}{
		{"finished and scored", Streak{Now: 4}, true, 4},
		{"not finished yet", Streak{Before: 3}, false, 3},
		{"finished with nothing", Streak{Before: 3}, true, 0},
		{"no run at all", Streak{}, false, 0},
	} {
		if got := c.streak.Shown(c.finished); got != c.want {
			t.Errorf("%s: %d, want %d", c.name, got, c.want)
		}
	}
}

// TestTheStandingCountsTheDaysBeforeTodayUntilTodayIsFinished: on
// Thursday 8 October, Monday to Wednesday before the game and Monday to
// Thursday after it; on the Monday, no day at all before the game; and
// on the Sunday after it, the whole week. A moment in the day is its
// day.
func TestTheStandingCountsTheDaysBeforeTodayUntilTodayIsFinished(t *testing.T) {
	for _, c := range []struct {
		day           time.Time
		finished      bool
		from, through string
		days          int
	}{
		{time.Date(2026, 10, 8, 0, 0, 0, 0, time.UTC), false, "2026-10-05", "2026-10-07", 3},
		{time.Date(2026, 10, 8, 0, 0, 0, 0, time.UTC), true, "2026-10-05", "2026-10-08", 4},
		{time.Date(2026, 10, 5, 0, 0, 0, 0, time.UTC), false, "2026-10-05", "2026-10-04", 0},
		{time.Date(2026, 10, 5, 0, 0, 0, 0, time.UTC), true, "2026-10-05", "2026-10-05", 1},
		{time.Date(2026, 10, 11, 0, 0, 0, 0, time.UTC), true, "2026-10-05", "2026-10-11", 7},
		{time.Date(2026, 10, 8, 23, 30, 0, 0, time.UTC), false, "2026-10-05", "2026-10-07", 3},
	} {
		from, through := StandingDays(c.day, c.finished)
		if DayString(from) != c.from || DayString(through) != c.through || daysBetween(from, through)+1 != c.days {
			t.Errorf("StandingDays(%s, finished %v) = %s to %s, want %s to %s, %d days",
				c.day.Format(time.RFC3339), c.finished, DayString(from), DayString(through), c.from, c.through, c.days)
		}
	}
}

// TestTheDayIsTheReadersOwnDate: at 23:30 UTC on 8 October, Tokyo and
// Kiritimati are already on the 9th, while Los Angeles and UTC are
// still on the 8th. Today is UTC's date, wherever the moment was
// written.
func TestTheDayIsTheReadersOwnDate(t *testing.T) {
	at := time.Date(2026, 10, 8, 23, 30, 0, 0, time.UTC)
	for zone, want := range map[string]string{
		"Asia/Tokyo":          "2026-10-09",
		"Pacific/Kiritimati":  "2026-10-09",
		"America/Los_Angeles": "2026-10-08",
		"UTC":                 "2026-10-08",
		"Etc/GMT+12":          "2026-10-08",
	} {
		day := DayIn(at, Zone(zone))
		if got := DayString(day); got != want {
			t.Errorf("23:30 UTC in %s is %s, want %s", zone, got, want)
		}
		if day.Location() != time.UTC || day.Hour() != 0 {
			t.Errorf("%s's day is kept as %v, want midnight UTC", zone, day)
		}
	}
	lagos := time.FixedZone("WAT", 3600)
	if got := DayString(Today(time.Date(2026, 10, 8, 0, 30, 0, 0, lagos))); got != "2026-10-07" {
		t.Errorf("00:30 in Lagos is %s in UTC, want the 7th", got)
	}
	for day, want := range map[string]string{
		"2026-10-05": "2026-10-05", "2026-10-08": "2026-10-05", "2026-10-11": "2026-10-05", "2026-10-12": "2026-10-12",
	} {
		d, _ := time.Parse("2006-01-02", day)
		if got := DayString(Monday(d)); got != want {
			t.Errorf("Monday(%s) = %s, want %s", day, got, want)
		}
	}
	first := time.Date(2026, 5, 20, 0, 0, 0, 0, time.UTC)
	if got := Number(time.Date(2026, 10, 8, 13, 0, 0, 0, time.UTC), first); got != 142 {
		t.Errorf("Number = %d, want 142", got)
	}
	if got := Number(first, first); got != 1 {
		t.Errorf("the first day is No. %d", got)
	}
}

// TestTheDayEndsAtLocalMidnight, clock changes included: London's last
// Sunday of October is 25 hours long and its last Sunday of March 23,
// and Santiago skips midnight itself in September, so its day begins at
// one.
func TestTheDayEndsAtLocalMidnight(t *testing.T) {
	for _, c := range []struct {
		zone     string
		at, want string
	}{
		{"UTC", "2026-10-08T23:59:00Z", "2026-10-09T00:00:00Z"},
		{"Asia/Tokyo", "2026-10-08T23:30:00Z", "2026-10-09T15:00:00Z"},
		{"America/Los_Angeles", "2026-10-08T23:30:00Z", "2026-10-09T07:00:00Z"},
		// 23:30 BST on Saturday the 24th: midnight is still on summer time.
		{"Europe/London", "2026-10-24T22:30:00Z", "2026-10-24T23:00:00Z"},
		// 01:30 BST on Sunday the 25th, before the clocks go back at two,
		// and noon after: either way the day ends at midnight GMT.
		{"Europe/London", "2026-10-25T00:30:00Z", "2026-10-26T00:00:00Z"},
		{"Europe/London", "2026-10-25T12:00:00Z", "2026-10-26T00:00:00Z"},
		{"Europe/London", "2027-03-27T23:30:00Z", "2027-03-28T00:00:00Z"},
		{"Europe/London", "2027-03-28T00:30:00Z", "2027-03-28T23:00:00Z"},
		{"America/Santiago", "2026-09-05T15:00:00Z", "2026-09-06T04:00:00Z"},
	} {
		at, _ := time.Parse(time.RFC3339, c.at)
		want, _ := time.Parse(time.RFC3339, c.want)
		if got := Next(at, Zone(c.zone)); !got.Equal(want) {
			t.Errorf("Next(%s in %s) = %s, want %s", c.at, c.zone, got.UTC().Format(time.RFC3339), c.want)
		}
	}
	// Every hour of a year, in zones that change their clocks at one, at
	// midnight, by half an hour, or never: Next is the very moment the
	// date turns, never a moment early or late.
	for _, name := range []string{"Europe/London", "America/Santiago", "America/Havana", "Asia/Beirut",
		"Australia/Lord_Howe", "Pacific/Chatham", "Asia/Tokyo", "Pacific/Kiritimati", "Etc/GMT+12"} {
		zone := Zone(name)
		for at := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC); at.Year() == 2026; at = at.Add(time.Hour) {
			day, next := DayIn(at, zone), Next(at, zone)
			if !DayIn(next, zone).Equal(day.AddDate(0, 0, 1)) || !DayIn(next.Add(-time.Second), zone).Equal(day) {
				t.Fatalf("in %s at %s, Next = %s, which is not where %s ends", name, at.Format(time.RFC3339), next.Format(time.RFC3339), DayString(day))
			}
		}
	}
}

// TestADayIsCurrentWhileItIsADateSomewhere: from when it begins at
// UTC+14 to when it ends at UTC−12, fifty hours in all.
func TestADayIsCurrentWhileItIsADateSomewhere(t *testing.T) {
	oct9 := time.Date(2026, 10, 9, 0, 0, 0, 0, time.UTC)
	for at, want := range map[string]bool{
		"2026-10-08T09:59:59Z": false,
		"2026-10-08T10:00:00Z": true, // midnight in Kiritimati
		"2026-10-09T12:00:00Z": true,
		"2026-10-10T11:59:59Z": true, // a second to midnight at UTC−12
		"2026-10-10T12:00:00Z": false,
	} {
		now, _ := time.Parse(time.RFC3339, at)
		if got := Current(oct9, now); got != want {
			t.Errorf("Current(9 October, %s) = %v, want %v", at, got, want)
		}
	}
}

func TestTheGeneratorIsSeededAndInRange(t *testing.T) {
	a, b := Seeded("2026-10-08"), Seeded("2026-10-08")
	for range 100 {
		if a.Uint32() != b.Uint32() {
			t.Fatal("one seed drew two sequences")
		}
	}
	if Seeded("2026-10-08").Uint32() == Seeded("2026-10-09").Uint32() {
		t.Error("two seeds drew the same first number")
	}
	r := Seeded("range")
	seen := map[int]bool{}
	for range 1000 {
		n := r.IntN(7)
		if n < 0 || n >= 7 {
			t.Fatalf("IntN(7) = %d", n)
		}
		seen[n] = true
	}
	if len(seen) != 7 {
		t.Errorf("IntN(7) drew only %v", seen)
	}
}

func TestATokenIsOneHundredAndTwentyEightRandomBits(t *testing.T) {
	a, b := NewToken(), NewToken()
	if a == b || !TokenOK(a) || len(a) != 22 {
		t.Errorf("tokens %q and %q", a, b)
	}
	for _, bad := range []string{"", "short", a + "A", "!!!!!!!!!!!!!!!!!!!!!!", a[:21] + "="} {
		if TokenOK(bad) {
			t.Errorf("TokenOK(%q) = true", bad)
		}
	}
	if h := TokenHash(a); len(h) != 32 || string(h) == a {
		t.Errorf("hash = %x", h)
	}
}
