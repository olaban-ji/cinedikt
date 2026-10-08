package daily

import (
	"strings"
	"testing"
	"unicode/utf8"
)

func TestTheOpeningIsTheFirstSentence(t *testing.T) {
	for _, c := range []struct{ name, text, want string }{
		{"a full stop", "Neo learns the truth. Then he fights.", "Neo learns the truth."},
		{"a question", "Who is Keyser Söze? Nobody knows.", "Who is Keyser Söze?"},
		{"an exclamation", "Run! The shark is coming.", "Run!"},
		{"the whole text", "A man wakes with no memory", "A man wakes with no memory"},
		{"space around it", "  One sentence.  ", "One sentence."},
		{"an abbreviation", "Dr. Jones finds the ark. He keeps it.", "Dr. Jones finds the ark."},
		{"two abbreviations", "Mr. and Mrs. Smith are spies. Neither knows.", "Mr. and Mrs. Smith are spies."},
		{"an initial", "Thomas A. Anderson leads two lives. One is a lie.", "Thomas A. Anderson leads two lives."},
		{"a country", "A U.S. Marshal hunts a fugitive. He is innocent.", "A U.S. Marshal hunts a fugitive."},
		{"versus", "It is Kramer vs. Kramer in court. The boy decides.", "It is Kramer vs. Kramer in court."},
		{"a lower-case word after", "He paid 3.50 for it. it was worth it. Then more.", "He paid 3.50 for it. it was worth it."},
		{"a quote after", `He says: "Stop." "Why?" she asks.`, `He says: "Stop."`},
		{"a quote opening the next", `The war is over. "Go home," they say.`, "The war is over."},
		{"curly quotes", "She whispers “Rosebud.” Then she dies.", "She whispers “Rosebud.”"},
		{"an ellipsis", "Wait... Then it begins.", "Wait..."},
		{"no space after", "Version 2.0 ships.Then nothing.", "Version 2.0 ships.Then nothing."},
		{"nothing", "", ""},
	} {
		if got := Opening(c.text); got != c.want {
			t.Errorf("%s: Opening(%q) = %q, want %q", c.name, c.text, got, c.want)
		}
	}
}

// TestALongOpeningIsCutAtAWord: at most OpeningMax characters, ending
// with an ellipsis, never in the middle of a word.
func TestALongOpeningIsCutAtAWord(t *testing.T) {
	long := strings.Repeat("Señor Hidalgo walks, ", 30) + "and stops."
	got := Opening(long)
	if n := utf8.RuneCountInString(got); n > OpeningMax {
		t.Errorf("%d characters, want at most %d", n, OpeningMax)
	}
	if !strings.HasSuffix(got, "…") || strings.HasSuffix(got, ",…") || strings.HasSuffix(got, " …") {
		t.Errorf("the cut ends %q", got[len(got)-12:])
	}
	if !strings.HasPrefix(long, strings.TrimSuffix(got, "…")) {
		t.Errorf("the cut is not the start of the sentence: %q", got)
	}
	if words := strings.Fields(strings.TrimSuffix(got, "…")); words[len(words)-1] != "walks" {
		t.Errorf("cut mid-word: ends %q", words[len(words)-1])
	}
}

func TestMentionsFindsTheTitleAsAWholePhrase(t *testing.T) {
	for _, c := range []struct {
		text, title string
		want        bool
	}{
		{"Neo learns that the Matrix is a lie.", "The Matrix", true},
		{"Neo learns that his matrix of lies is a dream.", "The Matrix", true},
		{"Neo learns the matrices are lies.", "The Matrix", false},
		{"An alien creature stalks the crew.", "Alien", true},
		{"The aliens arrive.", "Alien", false},
		{"Amélie helps her neighbours.", "Amelie", true},
		{"Fast and furious, they race.", "Fast & Furious", true},
		{"In 1917, two soldiers cross the line.", "1917", true},
		{"Two soldiers cross the line in 1918.", "1917", false},
		{"Anything at all.", "!!!", false},
	} {
		if got := Mentions(c.text, c.title); got != c.want {
			t.Errorf("Mentions(%q, %q) = %v, want %v", c.text, c.title, got, c.want)
		}
	}
}

func TestNormalIsThePrototypesNorm(t *testing.T) {
	for in, want := range map[string]string{
		"The Matrix":         "the matrix",
		"  Amélie  ":         "amelie",
		"Fast & Furious":     "fast and furious",
		"Spider-Man: No Way": "spider man no way",
		"WALL·E":             "wall e",
	} {
		if got := Normal(in); got != want {
			t.Errorf("Normal(%q) = %q, want %q", in, got, want)
		}
	}
}
