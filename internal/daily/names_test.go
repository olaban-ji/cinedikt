package daily

import (
	"context"
	"errors"
	"math/rand/v2"
	"strings"
	"sync/atomic"
	"testing"
	"time"
	"unicode/utf8"
)

// TestOnlyNamesLendNames: a character is somebody's name, not a role, a
// description or a credit with notes in it.
func TestOnlyNamesLendNames(t *testing.T) {
	for character, want := range map[string]bool{
		"Neo":                          true,
		"Richard Kimble":               true,
		"Ellis Boyd Redding":           true,
		"Star-Lord":                    true,
		"Leo Dalcò":                    true,
		"Jean-Luc O'Brien":             true,
		"Tae-ho":                       true,
		"Himself":                      false,
		"Old Man":                      false,
		"Police Officer":               false,
		"Captain Jack Sparrow":         false,
		"Dr. Evil":                     false,
		"Man in Bar":                   false,
		"College Boy 2":                false,
		"Gary (segment: Tape 56)":      false,
		"David 'Sully' Sullivan":       false,
		"Weath - Musician":             false,
		"Lost man's partner":           false,
		"Martin B":                     false,
		"Sammy Davis Jr.":              false,
		"Hattie Rose Mary Younger":     false,
		"Ип Ман":                       false,
		"#1 Fan":                       false,
		"Agent Smith":                  false,
		"Mother/Daughter":              false,
		"The Bride":                    false,
		"":                             false,
		"Ellen Ripley ":                true,
		"Hans  Gruber":                 true,
		"McLovin":                      true,
		"Ace Ventura, Pet Detective":   false,
		"Bill \"The Butcher\" Cutting": false,
		// A possessive is a role, and so is anything with a suffix after
		// an apostrophe; a capital after one is a name.
		"Hud's Fiancee":  false,
		"Hud’s Fiancee":  false,
		"Nisha's Dai":    false,
		"Baris'in":       false,
		"Yup'ik Roberts": false,
		"Conan O'Brien":  true,
		"Vito D'Angelo":  true,
		// A title or initials run together with full stops.
		"Mr.Tideman": false,
		"K.T":        false,
		"D.D.S":      false,
		// A numeral or an acronym is no first name or surname.
		"Harley II":     false,
		"Henry VIII":    false,
		"CIA Agent":     false,
		"DJ Pauly":      false,
		"Thomas McCall": true,
		// A word on the deny list takes its whole character with it, a
		// hyphen hiding nothing, while a word that only holds one stays.
		"Fat Bastard":  false,
		"Adolf Hitler": false,
		"Pussy Galore": false,
		"Nazi-Hunter":  false,
		"Cockburn":     true,
		"Scunthorpe":   true,
	} {
		if _, got := named(strings.TrimSpace(character)); got != want {
			t.Errorf("named(%q) = %v, want %v", character, got, want)
		}
	}
}

var pool = NewNames(Credits{Characters: []string{
	"Neo", "Trinity", "Richard Kimble", "Ellen Ripley", "Marty McFly", "Indiana Jones",
	"Ellis Boyd Redding", "Hannibal Lecter", "Clarice Starling", "Himself", "Old Man",
}})

// TestAMadeNameIsNeverACharacter: across many draws, no name is one
// character's own first and last word, repeats a word, is a character's
// whole name, runs past eighteen characters, or is taken.
func TestAMadeNameIsNeverACharacter(t *testing.T) {
	rng := rand.New(rand.NewPCG(1, 2))
	taken := map[string]bool{"Neo Kimble": true}
	for range 2000 {
		name := pool.Make(rng.IntN, func(n string) bool { return taken[n] })
		a, b, _ := strings.Cut(name, " ")
		switch {
		case name == FallbackName:
			t.Fatalf("a pool of nine characters fell back to %q", name)
		case a == b:
			t.Errorf("%q repeats a word", name)
		case pool.pairs[[2]string{a, b}]:
			t.Errorf("%q is one character's own name", name)
		case utf8.RuneCountInString(name) > NameMax:
			t.Errorf("%q is too long", name)
		case taken[name]:
			t.Errorf("%q was taken", name)
		case name == "Ellis Redding":
			t.Errorf("%q is Red", name)
		}
		if !pool.Valid(name) {
			t.Errorf("Make drew %q, which Valid refuses", name)
		}
	}
}

func TestValidIsOnlyWhatMakeCouldDraw(t *testing.T) {
	for name, want := range map[string]bool{
		"Trinity Kimble":        true,
		"Neo Ripley":            true,
		"Clarice Neo":           true,
		"Richard Kimble":        false, // a character
		"Ellis Redding":         false, // one character's first and last
		"Neo Neo":               false, // one word twice
		"Kimble Trinity":        false, // Kimble is never a first name
		"Hannibal Starling":     true,
		"Hannibal Lecter-Jones": false,
		"Trinity  Kimble":       false,
		"Trinity Kimble Jones":  false,
		"Trinity":               false,
		"Old Ripley":            false,
		"Indiana Starling":      true,
		"Clarice McFly":         true,
		"Robert'); DROP TABLE":  false,
	} {
		if got := pool.Valid(name); got != want {
			t.Errorf("Valid(%q) = %v, want %v", name, got, want)
		}
	}
	long := NewNames(Credits{Characters: []string{"Maximilianus Augustin", "Bartholomew Featherstonehaugh"}})
	if long.Valid("Maximilianus Featherstonehaugh") {
		t.Error("a name past eighteen characters is valid")
	}
}

// TestANameIsNeverARealPersons: two characters' words can make the name
// of somebody credited on the same movies, and neither Make nor Valid
// will have it, while the same words still make other names.
func TestANameIsNeverARealPersons(t *testing.T) {
	names := NewNames(Credits{
		Characters: []string{"David Dunn", "Mary Lynch", "Laura Palmer", "Dale Cooper"},
		People: []string{"David Lynch", "Laura Dern", "Kyle MacLachlan", "Samuel L. Jackson", "Dale  Cooper",
			"Mary Steenburgen", "Betsy Palmer"},
	})
	rng := rand.New(rand.NewPCG(3, 4))
	drawn := map[string]bool{}
	for range 2000 {
		name := names.Make(rng.IntN, nil)
		if name == "David Lynch" {
			t.Fatalf("Make drew %q, a real person", name)
		}
		drawn[name] = true
	}
	for name, want := range map[string]bool{
		"David Lynch":  false, // a director
		"Mary Palmer":  true,
		"David Cooper": true,
		"Laura Lynch":  true,
		"Dale Lynch":   true,
		"Mary Dunn":    false, // nobody credited is a Dunn
	} {
		if got := names.Valid(name); got != want {
			t.Errorf("Valid(%q) = %v, want %v", name, got, want)
		}
	}
	if !drawn["Laura Lynch"] || !drawn["David Palmer"] {
		t.Errorf("the words of a real name stopped making others: drew %v", drawn)
	}
	// Only a name a draw could make is kept: Laura Dern has no character
	// called Dern behind her, and Samuel L. Jackson has three words.
	if len(names.real) != 2 || !names.real["David Lynch"] || !names.real["Dale Cooper"] {
		t.Errorf("real = %v, want David Lynch and Dale Cooper", names.real)
	}
}

// TestRolesLendNoParts: a credit that is a role rather than a name
// passes the grammar ("Persian Emissary", "Militiaman"), but nobody
// credited begins their name with Persian or Militiaman, or ends it with
// Emissary, so with people to check against it lends nothing, while the
// characters with real people's words lend theirs.
func TestRolesLendNoParts(t *testing.T) {
	names := NewNames(Credits{
		Characters: []string{"Persian Emissary", "Hotel Receptionist", "Militiaman", "Witch", "Ellen Ripley", "Richard Kimble"},
		People:     []string{"Ellen Page", "Richard Gere", "Ann Ripley", "Bob Kimble", "Kim Wise"},
	})
	for name, want := range map[string]bool{
		"Ellen Kimble":       true,
		"Richard Ripley":     true,
		"Ellen Emissary":     false,
		"Militiaman Ripley":  false,
		"Ellen Militiaman":   false,
		"Persian Kimble":     false,
		"Hotel Receptionist": false,
		"Witch Kimble":       false,
		"Richard Witch":      false,
		"Ellen Ripley":       false, // still a character's own name
	} {
		if got := names.Valid(name); got != want {
			t.Errorf("Valid(%q) = %v, want %v", name, got, want)
		}
	}
	rng := rand.New(rand.NewPCG(5, 6))
	for range 2000 {
		if name := names.Make(rng.IntN, nil); name != "Ellen Kimble" && name != "Richard Ripley" {
			t.Fatalf("Make drew %q", name)
		}
	}
}

// TestNoNameIsMadeOfADeniedWord: a character with a slur, an obscenity
// or a hate figure's name in it lends no word at all, so neither a draw
// nor a name a page brings can have one.
func TestNoNameIsMadeOfADeniedWord(t *testing.T) {
	names := NewNames(Credits{Characters: []string{
		"Whore", "Chink", "Fat Bastard", "Pussy Galore", "Chet Pussy", "White Bitch", "Adolf Hitler",
		"Neo", "Richard Kimble", "Ellen Ripley",
	}})
	for _, w := range []string{"Whore", "Chink", "Fat", "Bastard", "Pussy", "Galore", "Chet", "White", "Bitch", "Adolf", "Hitler"} {
		if names.isFirst[w] || names.isLast[w] {
			t.Errorf("%s is lent", w)
		}
	}
	for _, name := range []string{"Whore Hitler", "Neo Bastard", "Pussy Kimble", "Ellen Hitler", "Fat Ripley"} {
		if names.Valid(name) {
			t.Errorf("Valid(%q)", name)
		}
	}
	rng := rand.New(rand.NewPCG(7, 8))
	for range 2000 {
		for _, w := range strings.Fields(names.Make(rng.IntN, nil)) {
			if w != "Neo" && w != "Richard" && w != "Ellen" && w != "Kimble" && w != "Ripley" {
				t.Fatalf("Make drew %q", w)
			}
		}
	}
}

func TestATinyPoolFallsBackToNeoKimble(t *testing.T) {
	if got := NewNames(Credits{}).Make(rand.IntN, nil); got != FallbackName {
		t.Errorf("an empty pool made %q", got)
	}
	one := NewNames(Credits{Characters: []string{"Neo"}})
	if got := one.Make(rand.IntN, nil); got != FallbackName {
		t.Errorf("a pool of one word made %q", got)
	}
}

// TestACatalogThatLendsNothingIsNoCatalog: characters that all fail the
// people check make a pool that can only say Neo Kimble, so it is
// treated as no answer: the built-in characters, asked again a minute
// later.
func TestACatalogThatLendsNothingIsNoCatalog(t *testing.T) {
	now := time.Date(2026, 10, 8, 9, 0, 0, 0, time.UTC)
	var reads atomic.Int32
	p := &NamePool{
		Now: func() time.Time { return now },
		Load: func(context.Context) (Credits, error) {
			reads.Add(1)
			return Credits{Characters: []string{"Militiaman", "Persian Emissary"}, People: []string{"Ann Smith"}}, nil
		},
	}
	if got := p.Names(context.Background()); !got.Valid("Trinity Kimble") || reads.Load() != 1 {
		t.Errorf("a catalog that lends nothing: %d reads, the built-in pool %v", reads.Load(), got.Valid("Trinity Kimble"))
	}
	now = now.Add(NamesRetry)
	p.Names(context.Background())
	if reads.Load() != 2 {
		t.Errorf("not asked again a minute later: %d reads", reads.Load())
	}
}

// TestThePoolIsReadOnceAndFallsBackUntilTheCatalogAnswers: no catalog
// yet is the built-in characters, asked again a minute later; a pool
// read from the catalog is kept for a day, then read again behind the
// caller.
func TestThePoolIsReadOnceAndFallsBackUntilTheCatalogAnswers(t *testing.T) {
	now := time.Date(2026, 10, 8, 9, 0, 0, 0, time.UTC)
	var reads atomic.Int32
	var ready atomic.Bool
	refreshed := make(chan struct{}, 4)
	p := &NamePool{
		Now: func() time.Time { return now },
		Load: func(context.Context) (Credits, error) {
			reads.Add(1)
			defer func() {
				select {
				case refreshed <- struct{}{}:
				default:
				}
			}()
			if !ready.Load() {
				return Credits{}, errors.New(`relation "catalog.principals" does not exist`)
			}
			return Credits{Characters: []string{"Trinity", "Richard Kimble", "Ellen Ripley"}}, nil
		},
	}
	ctx := context.Background()
	builtin := p.Names(ctx)
	if !builtin.Valid("Neo Kimble") || reads.Load() != 1 {
		t.Fatalf("without a catalog: %d reads, Neo Kimble valid %v", reads.Load(), builtin.Valid("Neo Kimble"))
	}
	ready.Store(true)
	p.Names(ctx)
	if reads.Load() != 1 {
		t.Errorf("asked again within the minute: %d reads", reads.Load())
	}
	now = now.Add(NamesRetry)
	got := p.Names(ctx)
	if reads.Load() != 2 || !got.Valid("Trinity Ripley") || got.Valid("Neo Kimble") {
		t.Errorf("a minute later: %d reads, the catalog's pool %v", reads.Load(), got.Valid("Trinity Ripley"))
	}
	<-refreshed
	<-refreshed
	now = now.Add(12 * time.Hour)
	p.Names(ctx)
	if reads.Load() != 2 {
		t.Errorf("read again within the day: %d reads", reads.Load())
	}
	now = now.Add(12 * time.Hour)
	if kept := p.Names(ctx); !kept.Valid("Trinity Ripley") {
		t.Error("a pool that came due was not handed out while it was read again")
	}
	select {
	case <-refreshed:
	case <-time.After(2 * time.Second):
		t.Fatal("a pool a day old was not read again")
	}
}
