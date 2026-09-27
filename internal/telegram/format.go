package telegram

import (
	"fmt"
	"html"
	"math"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// count is a number a person can read at a glance: 757802 is a log
// figure, 757,802 is a count. Always exact; a rounded 758K would be a
// second number to check against the first.
func count(n int64) string {
	sign := ""
	if n < 0 {
		sign = "-"
		n = -n
	}
	s := strconv.FormatInt(n, 10)
	lead := len(s) % 3
	if lead == 0 {
		lead = 3
	}
	var b strings.Builder
	b.WriteString(sign)
	b.WriteString(s[:lead])
	for i := lead; i < len(s); i += 3 {
		b.WriteByte(',')
		b.WriteString(s[i : i+3])
	}
	return b.String()
}

// pct is a running share as a whole percentage, rounded down and never
// more than 99. A pass that says 100% while it is still running is a
// pass that has not finished and claims it has.
func pct(share float64) int {
	p := int(math.Floor(share * 100))
	switch {
	case p < 0:
		return 0
	case p > 99:
		return 99
	}
	return p
}

// tenth is the integrity share with one decimal, rounded down so a
// load that fell short can never print as the 99% it needed.
func tenth(share float64) string {
	return strconv.FormatFloat(math.Floor(share*1000)/10, 'f', 1, 64)
}

// human is a duration as a person says it. Minutes matter under ten
// hours and stop mattering above it, which is where the rounding moves
// from the minute to the hour.
func human(d time.Duration) string {
	if d < time.Minute {
		return "under a minute"
	}
	if d < 10*time.Hour {
		m := d.Round(time.Minute)
		if m < time.Hour {
			return fmt.Sprintf("%d min", int(m/time.Minute))
		}
		if m < 10*time.Hour {
			h := int(m / time.Hour)
			mins := int((m % time.Hour) / time.Minute)
			if mins == 0 {
				return fmt.Sprintf("%d hr", h)
			}
			return fmt.Sprintf("%d hr %d min", h, mins)
		}
	}
	h := int(d.Round(time.Hour) / time.Hour)
	if h < 24 {
		return fmt.Sprintf("%d hr", h)
	}
	days, hrs := h/24, h%24
	unit := "days"
	if days == 1 {
		unit = "day"
	}
	if days >= 10 || hrs == 0 {
		return fmt.Sprintf("%d %s", days, unit)
	}
	return fmt.Sprintf("%d %s %d hr", days, unit, hrs)
}

// dayGap is how many calendar days t is after now, in loc. Yesterday
// is -1 whatever the hours, which is how a person counts.
func dayGap(t, now time.Time, loc *time.Location) int {
	ty, tm, td := t.In(loc).Date()
	ny, nm, nd := now.In(loc).Date()
	a := time.Date(ty, tm, td, 0, 0, 0, 0, time.UTC)
	b := time.Date(ny, nm, nd, 0, 0, 0, 0, time.UTC)
	return int(a.Sub(b).Hours() / 24)
}

// when is a moment as the clock on the wall says it, relative to today:
// "21:14", "yesterday 21:14", "Fri 21:14", or "12 Sep" once it is
// more than a week away and the hour no longer helps.
func when(t, now time.Time, loc *time.Location) string {
	local := t.In(loc)
	clock := local.Format("15:04")
	switch gap := dayGap(t, now, loc); {
	case gap == 0:
		return clock
	case gap == -1:
		return "yesterday " + clock
	case gap == 1:
		return "tomorrow " + clock
	case gap >= -6 && gap <= 6:
		return local.Format("Mon") + " " + clock
	default:
		return local.Format("2 Jan")
	}
}

// possessive is the day a catalog was built, as the word before
// "catalog": "today's", "yesterday's", "Friday's", or "the 12 Sep".
func possessive(t, now time.Time, loc *time.Location) string {
	switch gap := dayGap(t, now, loc); {
	case gap == 0:
		return "today's"
	case gap == -1:
		return "yesterday's"
	case gap == 1:
		return "tomorrow's"
	case gap >= -6 && gap <= 6:
		return t.In(loc).Format("Monday") + "'s"
	default:
		return "the " + t.In(loc).Format("2 Jan")
	}
}

// hhmm is the bare clock time.
func hhmm(t time.Time, loc *time.Location) string { return t.In(loc).Format("15:04") }

// roundETA keeps an estimate from pretending to be precise: to five
// minutes when it is more than half an hour away, to the minute nearer.
func roundETA(eta, now time.Time) time.Time {
	if eta.Sub(now) > 30*time.Minute {
		return eta.Round(5 * time.Minute)
	}
	return eta.Round(time.Minute)
}

// esc makes a dynamic value safe inside parse_mode HTML. Every value
// that did not come from this package goes through it.
func esc(s string) string { return html.EscapeString(s) }

// cut shortens s to at most n runes, ending in an ellipsis. It counts
// runes rather than bytes so an emoji on the boundary is never split
// into invalid UTF-8.
func cut(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	if n < 1 {
		return ""
	}
	return string(r[:n-1]) + "…"
}

var (
	tagRE     = regexp.MustCompile(`<[^>]+>`)
	queryRE   = regexp.MustCompile("\\?[^\\s\"'`]*")
	secretRE  = regexp.MustCompile("(?i)\\b(user|database|password|api_?key|access_token|token)=[^\\s`'\"]+")
	ipv6RE    = regexp.MustCompile(`\[[0-9a-fA-F:.]+\](:\d+)?`)
	ipv4RE    = regexp.MustCompile(`\b\d{1,3}(\.\d{1,3}){3}(:\d+)?\b`)
	emptyRE   = regexp.MustCompile("`\\s*`|\"\\s*\"|\\(\\s*\\)")
	spaceRE   = regexp.MustCompile(`[ \t]{2,}`)
	orphanRE  = regexp.MustCompile(`\s+:`)
	newlineRE = regexp.MustCompile(`\s*\n\s*`)
)

// redact takes out of an error everything that is nobody's business in
// a chat: query strings (where API keys travel), database users and
// names, IP addresses, and the bot's own token. Hostnames, status codes
// and title ids stay, because they are what makes a detail useful.
func redact(s, token string) string {
	if token != "" {
		s = strings.ReplaceAll(s, token, "…")
	}
	s = queryRE.ReplaceAllStringFunc(s, func(q string) string {
		// The punctuation that ended the sentence the URL sat in is
		// not part of the query.
		if n := len(q); n > 1 && strings.ContainsRune(":,;.)", rune(q[n-1])) {
			return q[n-1:]
		}
		return ""
	})
	s = secretRE.ReplaceAllString(s, "")
	s = ipv6RE.ReplaceAllString(s, "")
	s = ipv4RE.ReplaceAllString(s, "")
	s = emptyRE.ReplaceAllString(s, "")
	s = newlineRE.ReplaceAllString(s, " ")
	s = orphanRE.ReplaceAllString(s, ":")
	s = spaceRE.ReplaceAllString(s, " ")
	return strings.TrimSpace(s)
}

// stripTags is the HTML text as it reads with no formatting at all:
// what a plain-text resend carries, and what the length limit counts.
func stripTags(s string) string {
	return html.UnescapeString(tagRE.ReplaceAllString(s, ""))
}

// visible is how many characters a person sees.
func visible(s string) int { return len([]rune(stripTags(s))) }
