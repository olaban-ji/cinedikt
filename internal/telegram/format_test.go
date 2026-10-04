package telegram

import (
	"strings"
	"testing"
	"time"
	_ "time/tzdata"
	"unicode/utf8"
)

// lagos is the zone the owner reads in, and testNow a fixed moment in
// it: Sunday 27 September 2026, twenty past two in the afternoon.
var (
	lagos   = mustZone("Africa/Lagos")
	testNow = time.Date(2026, 9, 27, 14, 20, 0, 0, lagos)
)

func mustZone(name string) *time.Location {
	loc, err := time.LoadLocation(name)
	if err != nil {
		panic(err)
	}
	return loc
}

func TestHumanSaysADurationTheWayAPersonWould(t *testing.T) {
	for _, c := range []struct {
		d    time.Duration
		want string
	}{
		{59 * time.Second, "under a minute"},
		{4 * time.Minute, "4 min"},
		{time.Hour + 47*time.Minute, "1 hr 47 min"},
		{9*time.Hour + 59*time.Minute, "9 hr 59 min"},
		{10 * time.Hour, "10 hr"},
		{14*time.Hour + 20*time.Minute, "14 hr"},
		{36*time.Hour + time.Minute, "1 day 12 hr"},
		{74 * time.Hour, "3 days 2 hr"},
		{240 * time.Hour, "10 days"},
		{48 * time.Hour, "2 days"},
		{2 * time.Hour, "2 hr"},
	} {
		if got := human(c.d); got != c.want {
			t.Errorf("human(%v) = %q, want %q", c.d, got, c.want)
		}
	}
}

func TestWhenIsRelativeToTodayInTheReadersZone(t *testing.T) {
	for _, c := range []struct {
		t    time.Time
		want string
	}{
		{time.Date(2026, 9, 27, 9, 5, 0, 0, lagos), "09:05"},
		{time.Date(2026, 9, 26, 21, 14, 0, 0, lagos), "yesterday 21:14"},
		{time.Date(2026, 9, 28, 8, 40, 0, 0, lagos), "tomorrow 08:40"},
		{time.Date(2026, 9, 25, 21, 14, 0, 0, lagos), "Fri 21:14"},
		{time.Date(2026, 9, 12, 21, 14, 0, 0, lagos), "12 Sep"},
		// Stored in UTC, read in Lagos: 23:30 UTC on the 26th is already
		// the 27th there.
		{time.Date(2026, 9, 26, 23, 30, 0, 0, time.UTC), "00:30"},
	} {
		if got := when(c.t, testNow, lagos); got != c.want {
			t.Errorf("when(%v) = %q, want %q", c.t, got, c.want)
		}
	}
}

func TestPossessiveNamesTheCatalogsDay(t *testing.T) {
	for _, c := range []struct {
		t    time.Time
		want string
	}{
		{time.Date(2026, 9, 27, 3, 0, 0, 0, lagos), "today's"},
		{time.Date(2026, 9, 26, 3, 0, 0, 0, lagos), "yesterday's"},
		{time.Date(2026, 9, 25, 3, 0, 0, 0, lagos), "Friday's"},
		{time.Date(2026, 9, 12, 3, 0, 0, 0, lagos), "the 12 Sep"},
	} {
		if got := possessive(c.t, testNow, lagos); got != c.want {
			t.Errorf("possessive(%v) = %q, want %q", c.t, got, c.want)
		}
	}
}

func TestCountAndPercentAreExactAndHonest(t *testing.T) {
	for n, want := range map[int64]string{0: "0", 999: "999", 1000: "1,000", 757802: "757,802", -1204: "-1,204", 3120442: "3,120,442"} {
		if got := count(n); got != want {
			t.Errorf("count(%d) = %q, want %q", n, got, want)
		}
	}
	for share, want := range map[float64]int{0: 0, 0.009: 0, 0.4199: 41, 0.995: 99, 1: 99, 1.3: 99} {
		if got := pct(share); got != want {
			t.Errorf("pct(%v) = %d, want %d", share, got, want)
		}
	}
	if got := tenth(0.98999); got != "98.9" {
		t.Errorf("tenth(0.98999) = %q; a share short of 99%% must not print as 99.0", got)
	}
}

func TestCutCountsRunesAndStaysValidUTF8(t *testing.T) {
	s := "ab🔴cd"
	for n := 1; n <= 6; n++ {
		got := cut(s, n)
		if !utf8.ValidString(got) {
			t.Errorf("cut(%q, %d) = %q, not valid UTF-8", s, n, got)
		}
		if r := utf8.RuneCountInString(got); r > n {
			t.Errorf("cut(%q, %d) has %d runes", s, n, r)
		}
	}
	if got := cut(s, 3); got != "ab…" {
		t.Errorf("cut = %q", got)
	}
	if got := cut(s, 5); got != s {
		t.Errorf("cut of a string that fits = %q", got)
	}
}

func TestSizeAndUpperFirst(t *testing.T) {
	for n, want := range map[int64]string{512: "1 KB", 800 << 10: "800 KB", 1 << 20: "1.0 MB", 9871234: "9.4 MB"} {
		if got := size(n); got != want {
			t.Errorf("size(%d) = %q, want %q", n, got, want)
		}
	}
	for s, want := range map[string]string{"the 12 Sep": "The 12 Sep", "": ""} {
		if got := upperFirst(s); got != want {
			t.Errorf("upperFirst(%q) = %q, want %q", s, got, want)
		}
	}
}

func TestDetailIsEscapedAndRedacted(t *testing.T) {
	w := writer{loc: lagos, now: testNow, token: "123:secret"}
	p := w.push(sevWarn, true, "Posters failing since 12:00", `omdb: GET tt0133093: <html> & "quotes"`, "")
	if !strings.Contains(p.detail, "&lt;html&gt; &amp; &#34;quotes&#34;") {
		t.Errorf("detail = %q, want < and & escaped", p.detail)
	}
	if !strings.Contains(p.detail, "tt0133093") {
		t.Errorf("detail = %q, want the tconst kept", p.detail)
	}

	raw := "Get \"https://www.omdbapi.com/?apikey=abc123&i=tt1\": dial tcp 10.1.2.3:443: refused; " +
		"tmdb https://api.themoviedb.org/3/find/tt1?api_key=zzz9&external_source=imdb_id: HTTP 503; " +
		"failed to connect to `user=postgres database=railway`: [fd12:3456:789a::5]:5432 (postgres.railway.internal): " +
		"dial error; telegram bot123:secret/sendMessage"
	got := redact(raw, "123:secret")
	for _, leak := range []string{"abc123", "apikey", "api_key", "zzz9", "postgres database", "user=", "database=", "railway`", "fd12", "10.1.2.3", "123:secret"} {
		if strings.Contains(got, leak) {
			t.Errorf("redacted text %q still contains %q", got, leak)
		}
	}
	for _, keep := range []string{"www.omdbapi.com", "api.themoviedb.org", "postgres.railway.internal", "HTTP 503", "tt1"} {
		if !strings.Contains(got, keep) {
			t.Errorf("redacted text %q lost %q", got, keep)
		}
	}
}

func TestDetailIsCutTo300Runes(t *testing.T) {
	w := writer{loc: lagos, now: testNow}
	if got := w.detail(strings.Repeat("x", 1000)); utf8.RuneCountInString(got) != 300 {
		t.Errorf("detail is %d runes, want 300", utf8.RuneCountInString(got))
	}
}
