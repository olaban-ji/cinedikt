package daily

import (
	"strings"
	"unicode"

	"golang.org/x/text/unicode/norm"
)

// OpeningMax is the longest "How it starts" runs before it is cut at a
// word and ended with an ellipsis. It is one sentence in a panel a few
// hundred pixels wide, and a sentence that runs on past this is
// usually telling the whole story.
const OpeningMax = 320

// abbreviations end with a full stop that does not end a sentence. "Dr.
// No" is one movie, not two sentences.
var abbreviations = map[string]bool{
	"Mr": true, "Mrs": true, "Ms": true, "Dr": true, "St": true, "Jr": true, "Sr": true,
	"Lt": true, "Col": true, "Gen": true, "Sgt": true, "Capt": true, "Prof": true,
	"vs": true, "U.S": true, "U.K": true,
}

// Opening is the first sentence of an overview: up to a full stop,
// exclamation or question mark, with any closing quote or bracket after
// it, followed by space and then a capital letter or an opening quote;
// or the whole text when nothing ends it sooner. A full stop after an
// abbreviation, or after a lone capital (the "J." of a name), does not
// end it.
func Opening(text string) string {
	rs := []rune(strings.TrimSpace(text))
	end := len(rs)
	for i, r := range rs {
		if r != '.' && r != '!' && r != '?' {
			continue
		}
		j := i + 1
		for j < len(rs) && closing(rs[j]) {
			j++
		}
		k := j
		for k < len(rs) && unicode.IsSpace(rs[k]) {
			k++
		}
		if k == j || k == len(rs) {
			continue
		}
		if !unicode.IsUpper(rs[k]) && !opening(rs[k]) {
			continue
		}
		if r == '.' && abbreviated(rs[:i]) {
			continue
		}
		end = j
		break
	}
	return clip(strings.TrimSpace(string(rs[:end])), OpeningMax)
}

func closing(r rune) bool {
	switch r {
	case '"', '\'', '”', '’', ')', ']':
		return true
	}
	return false
}

func opening(r rune) bool {
	switch r {
	case '"', '\'', '“', '‘':
		return true
	}
	return false
}

// abbreviated is whether the word that ends before reads as an
// abbreviation, or a lone capital.
func abbreviated(before []rune) bool {
	i := len(before)
	for i > 0 && !unicode.IsSpace(before[i-1]) {
		i--
	}
	word := strings.TrimLeft(string(before[i:]), "(\"'“‘")
	if abbreviations[word] {
		return true
	}
	w := []rune(word)
	return len(w) == 1 && unicode.IsUpper(w[0])
}

// clip cuts s to at most n runes at a word, ending it with an ellipsis.
func clip(s string, n int) string {
	rs := []rune(s)
	if len(rs) <= n {
		return s
	}
	cut := rs[:n-1]
	if i := lastSpace(cut); i > 0 {
		cut = cut[:i]
	}
	return strings.TrimRight(string(cut), " ,;:–—-") + "…"
}

func lastSpace(rs []rune) int {
	for i := len(rs) - 1; i >= 0; i-- {
		if unicode.IsSpace(rs[i]) {
			return i
		}
	}
	return -1
}

// Mentions is whether text names title: the title, normalised, as a
// whole-word phrase in the text, normalised the same way. A title that
// starts with an article is also looked for without it, since an
// overview calls The Matrix "the Matrix" as often as not, and "a matrix"
// gives just as much away.
func Mentions(text, title string) bool {
	hay := " " + Normal(text) + " "
	t := Normal(title)
	if t == "" {
		return false
	}
	if strings.Contains(hay, " "+t+" ") {
		return true
	}
	for _, article := range []string{"the ", "a ", "an "} {
		if rest, ok := strings.CutPrefix(t, article); ok && rest != "" {
			return strings.Contains(hay, " "+rest+" ")
		}
	}
	return false
}

// Normal is text as titles are compared: lower case, accents dropped,
// "&" read as "and", and everything but letters and digits a single
// space. It is the prototype's norm().
func Normal(text string) string {
	var b strings.Builder
	space := true
	put := func(s string) {
		for _, r := range s {
			if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') {
				b.WriteRune(r)
				space = false
				continue
			}
			if !space {
				b.WriteByte(' ')
				space = true
			}
		}
	}
	for _, r := range norm.NFD.String(strings.ToLower(text)) {
		switch {
		case unicode.Is(unicode.Mn, r):
		case r == '&':
			put(" and ")
		default:
			put(string(r))
		}
	}
	return strings.TrimSpace(b.String())
}
