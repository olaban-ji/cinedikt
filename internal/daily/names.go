package daily

import (
	"context"
	"strings"
	"sync"
	"time"
	"unicode"
)

// Players' names are generated, never typed: a first name from one
// movie character joined to a surname from another, "Trinity Kimble",
// each a word some real person has in that place, so a name still reads
// as a person. Valid alone is not what keeps a client's choice off the
// board: any first word with any last word is valid, and a pair can be
// made to say what neither word does. The API keeps a new player's name
// only when it offered it (internal/api's nameOffers), so every name on
// a board is one a draw made.

// NameVotes is how well known a movie must be for its characters to
// lend players their names. Well known enough that the names are ones
// people have heard, and there are still about fifty thousand of them.
const NameVotes = 25000

// NameMax is the longest a name may be, in characters, so it fits a
// board row on a phone.
const NameMax = 18

// nameTries is how many draws a name gets before the fallback.
const nameTries = 80

// FallbackName is the name when every draw fails, which takes a pool
// of almost nobody.
const FallbackName = "Neo Kimble"

// stopWords are words that make a character a role rather than a name:
// "Old Man", "Police Officer", "Himself", and titles and articles that
// some real person's name happens to start with too, which the people
// check in NewNames would let by: "Coach Wise", "Boss Hogg", "El
// Mariachi". A character with any of them in it lends nothing.
var stopWords = wordSet(`
		Himself Herself Self Themselves Narrator Man Woman Men Women Boy Girl Boys Girls Kid Child
		Guard Officer Police Policeman Cop Soldier Waiter Waitress Nurse Doctor Reporter Driver
		Young Old Little Big The Mr Mrs Ms Miss Dr Agent Captain Detective Sergeant Lieutenant
		Colonel General Major Professor Judge President King Queen Prince Princess Lord Lady Sir
		Father Mother Sister Brother Uncle Aunt Voice Announcer Host Uncredited Additional
		Teacher Student Patron Owner Bartender Clerk Customer Passenger Prisoner Inmate Priest
		Cowboy Pilot Thug Gangster Henchman Dancer Singer Worker Secretary Manager Executive
		Neighbour Neighbor Friend Wife Husband Son Daughter Baby Grandma Grandpa Mom Dad
		Stranger Villager Townsman Hostess Maid Butler Chef Cook Vendor Tourist Guest Member
		Security Bouncer Doorman Sheriff Deputy Marshal Ranger Commander Admiral Private Corporal
		Trooper Mayor Governor Senator Minister Ambassador Count Countess Duke Duchess Baron
		Emperor Empress Boss Bad Chief Director Master Coach Cousin Doc Saint Papa Mama Ma Pa Pop
		Senor Señor El Le La`)

// deniedWords are slurs, obscenities and the names of hate's figureheads,
// which no player is ever named with. A character with one in it lends
// nothing, as with a stop word: "Fat Bastard" gives no Fat either. Each
// part of a word is checked on its own, whatever its case, so a hyphen
// hides nothing, but never a part of a part, so a word that only holds
// one (Scunthorpe, Cockburn) stays a name. A real surname that is also
// a slur (Coon) is on it too: on a board nobody can tell the two apart.
var deniedWords = wordSet(`
		whore slut bitch bastard pussy cunt twat wanker fuck fucker motherfucker shit shithead
		asshole arsehole cock cocksucker dickhead prick tits titty boobs dildo penis vagina
		rape rapist pimp hooker skank
		nigger nigga negro coon chink gook jap spic wetback kike kyke beaner raghead towelhead
		paki gyppo fag faggot dyke tranny retard spastic spaz
		hitler nazi führer fuhrer goebbels himmler mengele klansman satan`)

// denied is whether any part of a word, split at its hyphens and
// apostrophes, is on the deny list.
func denied(w string) bool {
	for _, part := range strings.FieldsFunc(w, func(r rune) bool { return r == '-' || r == '\'' || r == '’' }) {
		if deniedWords[strings.ToLower(part)] {
			return true
		}
	}
	return false
}

func wordSet(words string) map[string]bool {
	set := map[string]bool{}
	for _, w := range strings.Fields(words) {
		set[w] = true
	}
	return set
}

// named is a character's words when it reads as somebody's name: one to
// three words, each a capital and then letters, with an apostrophe or
// hyphen allowed inside a word but never at either end, and no word of a
// single letter, on the stop list or on the deny list. Latin letters
// only: the board's face draws those, and a name half in another script
// would read as a mistake.
func named(character string) ([]string, bool) {
	words := strings.Fields(character)
	if len(words) == 0 || len(words) > 3 {
		return nil, false
	}
	for _, w := range words {
		if stopWords[w] || denied(w) || !nameWord(w) {
			return nil, false
		}
	}
	return words, true
}

// nameWord is whether w is shaped like a word of a name. A lower-case
// letter straight after an apostrophe is a possessive ("Hud's Fiancee"
// is a role, and would lend Hud's and Fiancee) or a suffix in another
// tongue (Baris'in), never a name, which has a capital there: O'Brien,
// D'Angelo. A full stop is a title or initials run together (Mr.Tideman,
// D.D.S). A word with no lower-case letter at all is a numeral (II, IV)
// or an acronym (CIA, DJ), never a first name or a surname.
func nameWord(w string) bool {
	rs := []rune(w)
	if len(rs) < 2 || !unicode.IsUpper(rs[0]) || !latinLetter(rs[0]) || !latinLetter(rs[len(rs)-1]) {
		return false
	}
	lower := false
	for i := 1; i < len(rs); i++ {
		switch r := rs[i]; {
		case latinLetter(r):
			lower = lower || unicode.IsLower(r)
		case r == '\'' || r == '’':
			// Never the last rune, which is a letter.
			if unicode.IsLower(rs[i+1]) {
				return false
			}
		case r != '-':
			return false
		}
	}
	return lower
}

func latinLetter(r rune) bool {
	return unicode.IsLetter(r) && unicode.Is(unicode.Latin, r)
}

// Credits are what a pool of names is made from, off the same
// well-known movies: the characters, whose words are drawn, and the
// names of the actors, actresses and directors credited on them, which
// are never handed out. Two characters' words can make a real person
// ("David" and "Lynch"), and a board should never seem to list one.
type Credits struct {
	Characters []string
	People     []string
}

// Names is a pool of characters to make names from.
type Names struct {
	// first and last are the characters' first words and last words,
	// one entry a character, so a draw is as likely to land on a name as
	// characters are to have it: Jack and Harris come up more than
	// Shalulu and Gyobu, and a board reads as people rather than a list
	// of the catalog's rarest words. With people to check against, a
	// word is only in first when some person's name begins with it, and
	// in last when some person's ends with it (NewNames).
	first, last []string
	isFirst     map[string]bool
	isLast      map[string]bool
	// pairs are first and last words that belong to one character, and
	// full every character's whole name: a name that is either would be
	// that character rather than a new one.
	pairs map[[2]string]bool
	full  map[string]bool
	// real are the people's names a draw could make: two words, a first
	// word the pool has and a last word it has. A longer name, or one
	// with a word the pool lacks, can never come up, so it is not kept.
	// On the full catalog that is about twelve thousand of the
	// thirty-five thousand people credited, and they would otherwise be
	// about one draw in ninety: every word drawn is some person's.
	real map[string]bool
}

// NewNames builds a pool from credits as the catalog has them, keeping
// the characters that read as names, and the people a draw could name.
//
// A character's credit is as often a role as a name, and a capital and
// a stop list cannot tell them apart: "Militiaman" and "Witch" are one
// word each, and would lend both halves of a name; "Persian Emissary"
// lends Emissary as a surname, and "Coach Wise" Coach as a first name.
// What a role's word lacks is a person who has it. So with people to
// check against, a character's first word is drawn only when it begins
// some credited person's name, and its last word only when it ends one.
// On the full catalog that keeps about twenty-one thousand of the
// thirty-five thousand named characters' first words and sixteen
// thousand of their last, some four thousand different words each, and
// the names drawn read as people's. Rare names only a character has
// (Korben, Furiosa) go with the roles. A character still
// counts in full and pairs whichever of its words are drawn, so its own
// name is never handed out. Without people (the built-in pool, before
// the catalog answers) every named character lends both words: there is
// nothing to check against, and filtering would leave nothing.
func NewNames(credits Credits) *Names {
	n := &Names{isFirst: map[string]bool{}, isLast: map[string]bool{}, pairs: map[[2]string]bool{}, full: map[string]bool{},
		real: map[string]bool{}}
	var begins, ends map[string]bool
	if len(credits.People) > 0 {
		begins, ends = map[string]bool{}, map[string]bool{}
		for _, person := range credits.People {
			if words := strings.Fields(person); len(words) >= 2 {
				begins[words[0]], ends[words[len(words)-1]] = true, true
			}
		}
	}
	for _, c := range credits.Characters {
		words, ok := named(strings.TrimSpace(c))
		if !ok {
			continue
		}
		a, b := words[0], words[len(words)-1]
		n.full[strings.Join(words, " ")] = true
		n.pairs[[2]string{a, b}] = true
		if begins == nil || begins[a] {
			n.isFirst[a] = true
			n.first = append(n.first, a)
		}
		if ends == nil || ends[b] {
			n.isLast[b] = true
			n.last = append(n.last, b)
		}
	}
	for _, person := range credits.People {
		if words := strings.Fields(person); len(words) == 2 && n.isFirst[words[0]] && n.isLast[words[1]] {
			n.real[words[0]+" "+words[1]] = true
		}
	}
	return n
}

// drawable is whether a draw can make a name at all: a pool with no
// first word or no last word only ever gives FallbackName.
func (n *Names) drawable() bool { return len(n.first) > 0 && len(n.last) > 0 }

// Make draws a name nobody has: up to nameTries draws of a first word
// and a last word, each thrown back when it is one character's own,
// repeats a word, is a character's whole name or a real person's, runs
// past NameMax, or is taken. intn is a number in [0, n); taken may be
// nil.
func (n *Names) Make(intn func(int) int, taken func(string) bool) string {
	if n.drawable() {
		for range nameTries {
			a, b := n.first[intn(len(n.first))], n.last[intn(len(n.last))]
			name := a + " " + b
			if n.unfit(a, b, name) || (taken != nil && taken(name)) {
				continue
			}
			return name
		}
	}
	return FallbackName
}

// Valid is whether a name is one Make could have drawn from this pool,
// which is the only kind a player can keep.
func (n *Names) Valid(name string) bool {
	a, b, ok := strings.Cut(name, " ")
	if !ok || strings.Contains(b, " ") {
		return false
	}
	return n.isFirst[a] && n.isLast[b] && !n.unfit(a, b, name)
}

func (n *Names) unfit(a, b, name string) bool {
	return a == b || n.pairs[[2]string{a, b}] || n.full[name] || n.real[name] || len([]rune(name)) > NameMax
}

// builtinCharacters stand in for the catalog's while it has none to
// give: a first start, before the first import publishes. They come
// with no people to check names against; that lasts only until the
// catalog answers, NamesRetry later at most.
var builtinCharacters = []string{
	"Neo", "Trinity", "Morpheus", "Richard Kimble", "Ellen Ripley", "Marty McFly",
	"Indiana Jones", "Clarice Starling", "Hannibal Lecter", "Vito Corleone", "Rick Blaine",
	"Ilsa Lund", "Holly Golightly", "Travis Bickle", "Jules Winnfield", "Mia Wallace",
	"Andy Dufresne", "Forrest Gump", "Sarah Connor", "John McClane", "Hans Gruber",
	"Amelie Poulain", "Atticus Finch", "Norman Bates", "Marion Crane", "Ethan Hunt",
	"Lisbeth Salander", "Harry Lime", "Dorothy Gale", "Jack Torrance", "Ferris Bueller",
	"Juno MacGuff", "Furiosa", "Mathilda", "Thelma Dickinson", "Louise Sawyer",
	"Marge Gunderson", "Jeffrey Lebowski", "Walter Sobchak", "Elle Woods", "Rocky Balboa",
	"Ennis Del Mar", "Jack Twist", "Amy Dunne", "Nick Dunne", "Leeloo", "Korben Dallas",
}

// NamesEvery is how long a pool loaded from the catalog is kept before
// it is read again. The characters change only with a new generation,
// and a day-old pool names players just as well.
const NamesEvery = 24 * time.Hour

// NamesRetry is how soon a pool that fell back on the built-in
// characters asks the catalog again.
const NamesRetry = time.Minute

// namesLoadBudget bounds one read of the catalog's characters and
// people: about a fifth of a second on the full catalog, and the pool
// built from it about ten megabytes.
const namesLoadBudget = 10 * time.Second

// NamePool is the pool the API names players from, read from the
// catalog the first time a name is needed and kept in memory. When the
// catalog cannot give one (there is no catalog yet, or the read fails)
// it falls back on a handful of built-in characters and asks again
// NamesRetry later.
type NamePool struct {
	// Load reads the characters and people, as catalog.Store.DailyNames
	// does.
	Load func(ctx context.Context) (Credits, error)
	// Now is the clock; nil is time.Now.
	Now func() time.Time

	mu      sync.Mutex
	names   *Names
	at      time.Time
	live    bool
	loading bool
}

// Names is the pool as it stands. A pool loaded from the catalog that
// has come due is read again behind the caller, who is handed the one
// in memory; only a first read, or one after a fallback, is waited for.
func (p *NamePool) Names(ctx context.Context) *Names {
	p.mu.Lock()
	defer p.mu.Unlock()
	now := p.now()
	switch {
	case p.names != nil && p.live && now.Sub(p.at) < NamesEvery,
		p.names != nil && !p.live && now.Sub(p.at) < NamesRetry:
		return p.names
	case p.names != nil && p.live:
		if !p.loading {
			p.loading = true
			go func() {
				ctx, cancel := context.WithTimeout(context.Background(), namesLoadBudget)
				defer cancel()
				names, live := p.read(ctx)
				p.mu.Lock()
				defer p.mu.Unlock()
				p.loading = false
				// A refresh that fails keeps the pool it was refreshing,
				// and tries again in NamesRetry rather than a day.
				if live {
					p.names, p.at, p.live = names, p.now(), true
				} else {
					p.at = p.now().Add(NamesRetry - NamesEvery)
				}
			}()
		}
		return p.names
	}
	ctx, cancel := context.WithTimeout(ctx, namesLoadBudget)
	defer cancel()
	p.names, p.live = p.read(ctx)
	p.at = now
	return p.names
}

func (p *NamePool) read(ctx context.Context) (*Names, bool) {
	if p.Load != nil {
		if credits, err := p.Load(ctx); err == nil {
			// A catalog whose characters all lend nothing is no better
			// than none: it would name everybody Neo Kimble.
			if names := NewNames(credits); names.drawable() {
				return names, true
			}
		}
	}
	return NewNames(Credits{Characters: builtinCharacters}), false
}

func (p *NamePool) now() time.Time {
	if p.Now != nil {
		return p.Now()
	}
	return time.Now()
}
