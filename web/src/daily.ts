// Cinedikt Daily, and its game, Name Drop.
//
// One hidden movie a day. Its cast shows up one name at a time, from the
// sixth-billed up to the star, each with a face and another movie they
// were in, and the reader names the movie with as few names, facts and
// wrong guesses as they can. The points left are the score.
//
// The server owns the game. It checks every move against the recorded
// one, applies it once and sends the game back, and it never sends the
// answer, a hidden name or a fact nobody has paid for until the game is
// over. So what is here is only what the page needs to draw the game it
// is handed: what each row, fact, chip and line says, what a wrong guess
// told, what the result and the share say, what the Movies sheet lights,
// and when things move. Everything is pure but the midnight watch, which
// is a timer, and all of it is checked without a DOM in daily.test.ts.

import type { CSSProperties } from 'react';
import type {
  DailyAlso,
  DailyAnswer,
  DailyBoard,
  DailyEntry,
  DailyFactKind,
  DailyFacts,
  DailyGame,
  DailyGuess,
  DailyMove,
  DailyMovie,
  DailyPerson,
  DailySlot,
  DailyTab,
  DailyToday,
  DailyWarmth,
  DailyWeek,
} from './api';
import { initialsFor } from './grid';
import type { Theme } from './theme';

/** The game's name: the title screen's heading, the game page's kicker,
 *  the banner's title and the share text's first line. Named for what it
 *  is played with, the cast's names, dropped one at a time. cmd/api's
 *  dailyShareTitle says it to a scraper ("Cinedikt Daily: Name Drop"),
 *  and the two must agree. */
export const GAME_NAME = 'Name Drop';

// ---- the rules, as far as the page shows them ----
//
// The server charges; these are what the buttons, the lines and the copy
// say, and what the page checks before it asks, so a move the server
// would refuse is not sent. The rule for any fact added here, as for the
// server's: none may give the answer away in a single search. So no
// plot, tagline, quote or character's name, and the length, the rating
// and the years only ever as ranges, so no one number can be checked
// against a candidate. The exact values come with the end.

/** Every game starts with this many points, and the score is what is
 *  left, so the most is this. */
export const DAILY_START = 1000;

/** The names in a puzzle: the six billed cast, in reveal order. */
export const CAST_SIZE = 6;

/** Next name. */
export const NEXT_COST = 100;

/** The first wrong guess, and how much more each one after it costs, so
 *  guessing never beats asking for the next name. The server sends what
 *  the next one costs with the game (`nextCost`); these are what the
 *  rules say. */
export const WRONG_BASE = 100;
export const WRONG_STEP = 50;

/** What the `n`th wrong guess costs, counting from nought: 100, 150,
 *  200… as the server's NextWrong charges. */
export function wrongCost(n: number): number {
  return WRONG_BASE + WRONG_STEP * n;
}

/** What each fact costs. */
export const FACT_COST: Record<DailyFactKind, number> = {
  length: 50,
  rating: 50,
  genre: 100,
  decade: 100,
  years: 100,
  director: 250,
};

/** An overlap on the Movies sheet: once per person, and then theirs to
 *  switch on and off for nothing. */
export const OVERLAP_COST = 250;

/** Whether the points cover a purchase: a purchase must leave at least
 *  one point, so it is refused at exactly its cost, as the server
 *  refuses it. A wrong guess is not a purchase: one the points cannot
 *  cover ends the game instead (lastGuess). */
export function affords(pts: number, cost: number): boolean {
  return pts > cost;
}

/** Whether the next wrong guess would end the game: the points cannot
 *  cover it, and it scores nought. */
export function lastGuess(game: Pick<DailyGame, 'pts' | 'nextCost'>): boolean {
  return game.pts <= game.nextCost;
}

// ---- colours ----
//
// Every colour worked out here is a string the page sets inline, as a
// custom property (toneStyle), never written in the stylesheet: the
// build rewrites an oklch() it finds in a declaration as hex, and the
// token tests refuse one there.

/** A person's colour, from the hue the server gives them: lighter and
 *  softer on the dark ground, darker and stronger on paper. */
export function hueColour(hue: number, theme: Theme): string {
  return theme === 'light' ? `oklch(0.56 0.16 ${hue})` : `oklch(0.76 0.13 ${hue})`;
}

/** The hue of the face on the title screen's shown row. */
export const TITLE_HUE = 118;

/** What a wrong guess's warmth is called, cold to hot. */
export const WARMTH_LABELS: Record<DailyWarmth, 'Cold' | 'Warm' | 'Hot'> = { 0: 'Cold', 1: 'Warm', 2: 'Hot' };

/** The warmth colours, cold to hot, in each theme. */
export const WARMTH_COLOURS: Record<Theme, readonly [string, string, string]> = {
  dark: ['oklch(0.76 0.1 235)', 'oklch(0.82 0.13 78)', 'oklch(0.74 0.17 32)'],
  light: ['oklch(0.5 0.13 240)', 'oklch(0.55 0.13 65)', 'oklch(0.54 0.19 30)'],
};

export function warmthColour(warmth: DailyWarmth, theme: Theme): string {
  return WARMTH_COLOURS[theme][warmth];
}

/** A colour as the element drawing it carries it: --tone, which the
 *  stylesheet reads for a face's ring and fill (mixed into --s with
 *  color-mix), a warmth pill's dot and wash, and a chip's ring. One name
 *  for every per-element colour, which the token tests already allow. */
export function toneStyle(colour: string): CSSProperties {
  return { ['--tone' as string]: colour };
}

/** The poster colour the hidden card is filled with, when it is the
 *  "#rrggbb" the server promises; anything else is not put into a style
 *  at all, and the card falls back to the card ground. */
export function cardColour(colour: string | null | undefined): string {
  return typeof colour === 'string' && /^#[0-9a-f]{6}$/i.test(colour) ? colour : 'var(--c)';
}

/** The initials drawn on a face with no photo, unique among `people`: the
 *  map's own codes (grid.ts's initialsFor), "KR" for Keanu Reeves. */
export function codesOf(people: readonly DailyPerson[]): Map<string, string> {
  return initialsFor(people.map((p) => ({ id: p.id, name: p.name, role: 'cast' as const })));
}

// ---- facts ----

/** The four length bands, by the band the server sends. */
export const LENGTH_BANDS = ['Under 1h 30m', '1h 30m to 2h', '2h to 2h 30m', 'Over 2h 30m'] as const;

/** The four rating bands, by the band the server sends. */
export const RATING_BANDS = ['Below 6.0', '6.0 to 6.9', '7.0 to 7.9', '8.0 or higher'] as const;

/** A band as it reads, or nothing for one that is not a band. */
export function lengthBandText(band: number): string {
  return LENGTH_BANDS[band] ?? '';
}

export function ratingBandText(band: number): string {
  return RATING_BANDS[band] ?? '';
}

/** A band written to head a fact ("Below 6.0"), as it reads inside a
 *  sentence: "rated below 6.0". The bands that start with a figure are
 *  as they were. */
function inSentence(band: string): string {
  return band.charAt(0).toLowerCase() + band.slice(1);
}

/** "1990s", from the decade's first year. */
export function decadeText(decade: number): string {
  return `${decade}s`;
}

/** "1995–1999", from the five years' first. */
export function yearsText(years: number): string {
  return `${years}–${years + 4}`;
}

/** A runtime as the facts write it: "2h 16m", "2h" on the hour, as the
 *  bands write two hours, and "45m" under one. */
export function hoursMinutes(minutes: number): string {
  const h = Math.floor(minutes / 60);
  const m = Math.round(minutes - h * 60);
  if (!h) return `${m}m`;
  return m ? `${h}h ${m}m` : `${h}h`;
}

/** Each fact as the page names it: for sale ("Length range"), bought
 *  ("Length"), among what the result says was paid for ("Five-year
 *  range"), in the share text ("the decade"), and what its button says
 *  to a screen reader before its price. */
export const FACTS: Record<
  DailyFactKind,
  { offer: string; bought: string; paid: string; used: string; aria: string }
> = {
  length: {
    offer: 'Length range',
    bought: 'Length',
    paid: 'Length',
    used: 'the length range',
    aria: 'Show roughly how long it is.',
  },
  rating: {
    offer: 'Rating range',
    bought: 'Rating',
    paid: 'Rating',
    used: 'the rating range',
    aria: 'Show its IMDb rating range.',
  },
  genre: { offer: 'Genre', bought: 'Genre', paid: 'Genre', used: 'the genre', aria: 'Show its genre.' },
  decade: {
    offer: 'Decade',
    bought: 'Decade',
    paid: 'Decade',
    used: 'the decade',
    aria: 'Show the decade it came out.',
  },
  years: {
    offer: 'Narrow the years',
    bought: 'Years',
    paid: 'Five-year range',
    used: 'a five-year range',
    aria: 'Narrow the years to five.',
  },
  director: {
    offer: 'Director',
    bought: 'Director',
    paid: 'Director',
    used: 'the director',
    aria: 'Show the director.',
  },
};

/** The facts in the order the row offers them, which is also the order
 *  the result and the share list them in. */
export const FACT_ORDER: readonly DailyFactKind[] = ['length', 'rating', 'genre', 'decade', 'years', 'director'];

/** What a bought fact says, or nothing for one not bought. */
export function factValue(kind: DailyFactKind, facts: DailyFacts): string {
  switch (kind) {
    case 'length':
      return facts.length == null ? '' : lengthBandText(facts.length);
    case 'rating':
      return facts.rating == null ? '' : ratingBandText(facts.rating);
    case 'genre':
      return facts.genre ? facts.genre.join(', ') : '';
    case 'decade':
      return facts.decade == null ? '' : decadeText(facts.decade);
    case 'years':
      return facts.years == null ? '' : yearsText(facts.years);
    case 'director':
      return facts.director ? andList(facts.director.map((p) => p.name)) : '';
  }
}

/** Whether a fact has been bought: only a bought fact is sent. */
export function hasFact(kind: DailyFactKind, facts: DailyFacts): boolean {
  return facts[kind] != null;
}

/** One item in the facts row while the game is on: a fact for sale, or
 *  one bought. */
export interface FactItem {
  kind: DailyFactKind;
  bought: boolean;
  /** "Length range" for sale, "Length" bought. */
  label: string;
  /** What it says once bought, "2h to 2h 30m"; nothing for sale. */
  value: string;
  cost: number;
  /** "−50" for sale; nothing once bought. */
  price: string;
  /** For sale, in a game still on, and the points cover it with one to
   *  spare. Drawn at half strength when not. */
  can: boolean;
  /** For sale: what it shows and its price, "Show its genre. It costs
   *  100 points." Nothing once bought, when the label and value say it. */
  aria: string;
}

/** The facts row while the game is on, in order: length, rating, genre,
 *  the years, director. The years take one place: Decade for sale; then
 *  the decade bought, followed by "Narrow the years" for sale; then the
 *  five years in the decade's place. */
export function factItems(game: Pick<DailyGame, 'phase' | 'pts' | 'facts'>): FactItem[] {
  const playing = game.phase === 'play';
  const facts = game.facts;
  const bought = (kind: DailyFactKind): FactItem => ({
    kind,
    bought: true,
    label: FACTS[kind].bought,
    value: factValue(kind, facts),
    cost: FACT_COST[kind],
    price: '',
    can: false,
    aria: '',
  });
  const offer = (kind: DailyFactKind): FactItem => {
    const cost = FACT_COST[kind];
    return {
      kind,
      bought: false,
      label: FACTS[kind].offer,
      value: '',
      cost,
      price: `−${cost}`,
      can: playing && affords(game.pts, cost),
      aria: `${FACTS[kind].aria} It costs ${cost} points.`,
    };
  };
  const out: FactItem[] = [];
  for (const kind of FACT_ORDER) {
    if (kind === 'years') continue;
    if (kind === 'decade') {
      if (hasFact('years', facts)) out.push(bought('years'));
      else if (hasFact('decade', facts)) out.push(bought('decade'), offer('years'));
      else out.push(offer('decade'));
      continue;
    }
    out.push(hasFact(kind, facts) ? bought(kind) : offer(kind));
  }
  return out;
}

/** About the movie, once the game is over: every fact, exact. Anything
 *  the catalog has nothing for is left out rather than drawn empty. */
export function aboutItems(end: { answer: DailyAnswer; directors: DailyPerson[] }): { label: string; value: string }[] {
  const a = end.answer;
  return [
    { label: 'Length', value: a.length > 0 ? hoursMinutes(a.length) : '' },
    { label: 'Genre', value: a.genres.join(', ') },
    { label: 'IMDb rating', value: Number.isFinite(a.rating) ? a.rating.toFixed(1) : '' },
    { label: 'Year', value: a.year > 0 ? String(a.year) : '' },
    { label: 'Director', value: andList(end.directors.map((p) => p.name)) },
  ].filter((f) => f.value);
}

/** The facts row's heading: what is for sale, or About the movie. */
export function factsHeading(done: boolean): string {
  return done ? 'About the movie' : 'Buy a fact. Each one also marks the map';
}

// ---- the cast ----

/** The slots the reader saw, which is what the result counts and the
 *  share squares fill: the first, each name bought, and for each wrong
 *  guess everyone it shared and then the next name, as the server's
 *  replay shows them. Worked out from the log rather than from the slots,
 *  because at the end every slot is shown. A last wrong guess that runs
 *  the points out still showed its names. */
export function seenSlots(game: Pick<DailyGame, 'log'> | null): Set<number> {
  const seen = new Set<number>();
  if (!game) return seen;
  seen.add(0);
  const showNext = () => {
    for (let k = 0; k < CAST_SIZE; k++) {
      if (!seen.has(k)) {
        seen.add(k);
        return;
      }
    }
  };
  for (const e of game.log) {
    if (e.type === 'next') seen.add(e.slot);
    else if (e.type === 'guess') {
      for (const k of e.guess.shared) if (k >= 0 && k < CAST_SIZE) seen.add(k);
      showNext();
    }
  }
  return seen;
}

/** The slots showing now, by slot. */
export function shownSlots(game: Pick<DailyGame, 'slots'>): Extract<DailySlot, { shown: true }>[] {
  return game.slots.filter((s): s is Extract<DailySlot, { shown: true }> => s.shown);
}

/** Whether Next name can be pressed: a game on, a name still hidden, and
 *  the points to leave one over after it. */
export function canNext(game: Pick<DailyGame, 'phase' | 'pts' | 'slots'>): boolean {
  return game.phase === 'play' && game.slots.some((s) => !s.shown) && affords(game.pts, NEXT_COST);
}

/** The slot Next name shows: the first hidden one in reveal order, or
 *  null when it cannot be pressed. */
export function nextSlot(game: Pick<DailyGame, 'phase' | 'pts' | 'slots'>): number | null {
  if (!canNext(game)) return null;
  return game.slots.find((s) => !s.shown)?.slot ?? null;
}

/** "Also in The Fugitive (1993)", "From your guess, Speed", or nothing:
 *  the line under a shown name. A name a guess filled in says so, which
 *  is worth more to the reader than another movie. */
export function castLine(slot: Pick<Extract<DailySlot, { shown: true }>, 'also' | 'from'>): string {
  if (slot.from) return `From your guess, ${slot.from.title}`;
  if (slot.also) return `Also in ${titled(slot.also)}`;
  return '';
}

/** A hidden row's bar, as a share of its text column, by slot. */
export const BAR_WIDTHS = [46, 58, 40, 52, 62, 44] as const;

/** One row of the cast list. */
export type CastRow =
  | {
      slot: number;
      state: 'shown';
      person: DailyPerson;
      line: string;
      /** Seen during the game. At the end the rest appear one after
       *  another (endRevealDelay), at once with stillness asked for. */
      seen: boolean;
    }
  /** The next to show: the whole row is a button (NEXT_ROW_LABEL). */
  | { slot: number; state: 'next'; bar: number }
  | { slot: number; state: 'hidden'; bar: number };

/** The cast list, in reveal order. The row Next name would show is the
 *  "next" one only while it can be pressed; otherwise every hidden row is
 *  just hidden. A hidden row carries no name at all: the page is never
 *  told one. */
export function castRows(game: Pick<DailyGame, 'phase' | 'pts' | 'slots' | 'log'>): CastRow[] {
  const next = nextSlot(game);
  const seen = seenSlots(game);
  return game.slots.map((s): CastRow => {
    if (s.shown) return { slot: s.slot, state: 'shown', person: s.person, line: castLine(s), seen: seen.has(s.slot) };
    return { slot: s.slot, state: s.slot === next ? 'next' : 'hidden', bar: BAR_WIDTHS[s.slot] ?? 50 };
  });
}

/** The next row's name for a screen reader, which is the whole row. */
export const NEXT_ROW_LABEL = `Show the next name. It costs ${NEXT_COST} points.`;

/** A shown row's Movies button, for a screen reader. */
export function moviesLabel(name: string): string {
  return `See ${name}’s movies on a Cinedikt map`;
}

/** The Next name button in the guess bar: its words, its price, and
 *  whether it can be pressed. Once all six are out it says so. */
export function nextButton(game: Pick<DailyGame, 'phase' | 'pts' | 'slots'>): {
  label: string;
  price: string;
  can: boolean;
} {
  const left = game.slots.some((s) => !s.shown);
  return {
    label: left ? 'Next name' : 'Everyone’s showing',
    price: left ? `−${NEXT_COST}` : '',
    can: canNext(game),
  };
}

/** Whether a row's photo preview opens upwards: the lower three do, so it
 *  stays on the page. */
export function peekOpensUp(slot: number): boolean {
  return slot >= 3;
}

// ---- wrong guesses ----

/** The wrong guesses, in order. */
export function guessesOf(game: Pick<DailyGame, 'log'> | null): DailyGuess[] {
  const out: DailyGuess[] = [];
  for (const e of game?.log ?? []) if (e.type === 'guess') out.push(e.guess);
  return out;
}

/** The movies guessed wrong so far, by IMDb id: the results list marks
 *  them "Tried", and the Movies sheet crosses them. */
export function guessedIds(game: Pick<DailyGame, 'log'> | null): Set<string> {
  return new Set(guessesOf(game).map((g) => g.id));
}

/** Whether a game just handed back has a wrong guess the one before it
 *  did not: the message box shows the new one. */
export function newWrongGuess(was: Pick<DailyGame, 'log'> | null, now: Pick<DailyGame, 'log'>): boolean {
  return guessesOf(now).length > guessesOf(was).length;
}

/** The people a wrong guess shares, as the game shows them, in slot
 *  order. A guess fills in everyone it shares, so each is showing. */
export function sharedPeople(guess: Pick<DailyGuess, 'shared'>, game: Pick<DailyGame, 'slots'>): DailyPerson[] {
  const wanted = new Set(guess.shared);
  return shownSlots(game)
    .filter((s) => wanted.has(s.slot))
    .map((s) => s.person);
}

/** A wrong guess's chip in the top block: its title, its warmth, and
 *  what it says it does. */
export interface TriedChip {
  id: string;
  title: string;
  warmth: DailyWarmth;
  aria: string;
}

export function triedChips(game: Pick<DailyGame, 'log'>): TriedChip[] {
  return guessesOf(game).map((g) => ({
    id: g.id,
    title: g.title,
    warmth: g.warmth,
    aria: `${g.title}, ${WARMTH_LABELS[g.warmth].toLowerCase()}. Show what it told you`,
  }));
}

/** What the message box says after a wrong guess, in its one row: the
 *  title, never cut short; the warmth; the faces it shares; and three
 *  chips, the decade, the genre and the cast. Never which way in time,
 *  nor which genre: those are facts the reader pays for. */
export interface GuessMessage {
  title: string;
  warmth: DailyWarmth;
  label: 'Cold' | 'Warm' | 'Hot';
  faces: DailyPerson[];
  chips: string[];
}

export function guessMessage(guess: DailyGuess, game: Pick<DailyGame, 'slots'>): GuessMessage {
  const faces = sharedPeople(guess, game);
  return {
    title: `Not ${guess.title}`,
    warmth: guess.warmth,
    label: WARMTH_LABELS[guess.warmth],
    faces,
    chips: [
      guess.sameDecade ? 'Same decade' : 'Different decade',
      guess.sharesGenre ? 'Shares a genre' : 'No shared genre',
      faces.length ? `Shares ${andList(faces.map((p) => p.name))}` : 'No one from today’s cast',
    ],
  };
}

/** The points line over the field: what getting it now scores, and what
 *  the next wrong guess costs, or, when that guess would end the game,
 *  that it is the last. The number is set apart; `text` is the whole. */
export function pointsLine(game: Pick<DailyGame, 'pts' | 'nextCost'>): {
  lead: string;
  pts: string;
  tail: string;
  text: string;
} {
  const pts = fmtN(game.pts);
  const [lead, tail] = lastGuess(game)
    ? ['Last guess, for ', ' points']
    : ['Get it now for ', ` points · wrong guess −${fmtN(game.nextCost)}`];
  return { lead, pts, tail, text: `${lead}${pts}${tail}` };
}

// ---- the results list ----

/** What the guess field's search last answered: the words it was asked,
 *  the movies it found for them, or that it never answered. */
export interface GuessFound<T> {
  q: string;
  hits: readonly T[];
  failed: boolean;
}

/** What the results list shows for what is typed now.
 *
 *  `rows` are what can be chosen, and only an answer to exactly what is
 *  typed gives any. The search waits for a pause and then the network,
 *  and in that time the reader may have typed on: the last answer's rows
 *  belong to words they have typed past, and Enter on the top one would
 *  make a guess they never meant, which costs points and cannot be taken
 *  back. So until the new answer comes, the old rows are only `held`:
 *  drawn faded, so the list does not collapse at every key, and not to
 *  be chosen.
 *
 *  `note` stands in for rows: "Searching…" while the answer is on its
 *  way and there is nothing to hold, the network's words when the search
 *  never answered (which is not the same as nothing matching), and
 *  nothing matching when that is what the catalog said. */
export function guessOptions<T>(
  typed: string,
  found: GuessFound<T> | null,
): { rows: readonly T[]; held: readonly T[]; note: string } {
  if (!found || found.q !== typed) {
    const held = found && !found.failed ? found.hits : [];
    return { rows: [], held, note: held.length ? '' : 'Searching…' };
  }
  if (found.failed) return { rows: [], held: [], note: UNREACHABLE };
  return { rows: found.hits, held: [], note: found.hits.length ? '' : `No movies match “${typed}”` };
}

/** A title as two results are compared: case, accents and punctuation
 *  set aside, so "Amélie" and "Amelie" are the same title. */
function titleKey(title: string): string {
  return title
    .toLowerCase()
    .normalize('NFD')
    .replace(/[̀-ͯ]/g, '')
    .replace(/&/g, ' and ')
    .replace(/[^a-z0-9]+/g, ' ')
    .trim();
}

/** Whether each result shows its year: only where two results share a
 *  title, so a bought range of years cannot be used to sift the list. */
export function yearsShown(hits: readonly { title: string }[]): boolean[] {
  const count = new Map<string, number>();
  for (const h of hits) count.set(titleKey(h.title), (count.get(titleKey(h.title)) ?? 0) + 1);
  return hits.map((h) => (count.get(titleKey(h.title)) ?? 0) > 1);
}

/** The most results the list offers: four on a phone, where the keyboard
 *  takes half the screen, and six elsewhere. */
export function resultsMax(phone: boolean): number {
  return phone ? 4 : 6;
}

// ---- the result ----

/** A count as the page writes it: "61,240". */
export function fmtN(n: number): string {
  return n.toLocaleString('en-GB');
}

/** "a, b and c", without the Oxford comma. */
export function andList(items: readonly string[]): string {
  if (items.length < 2) return items.join('');
  return `${items.slice(0, -1).join(', ')} and ${items[items.length - 1]}`;
}

/** "1999", or nothing for a movie whose year the catalog does not have. */
function titled(f: Pick<DailyAlso, 'title' | 'year'>): string {
  return f.year > 0 ? `${f.title} (${f.year})` : f.title;
}

/** The result's kicker: how the game ended. "On the first name" is a
 *  solve with nothing paid for at all. */
export function resultKicker(game: Pick<DailyGame, 'won' | 'gaveUp' | 'log'>): string {
  if (game.won) return seenSlots(game).size === 1 && paidFor(game.log).length === 0 ? 'Got it on the first name' : 'Got it';
  return game.gaveUp ? 'You asked for the answer' : 'Your points ran out';
}

/** "You needed 3 of the 6 names", or, for a loss, "You saw 4 of the 6
 *  names". */
export function namesLine(game: Pick<DailyGame, 'won' | 'log'>): string {
  const n = seenSlots(game).size;
  return game.won ? `You needed ${n} of the ${CAST_SIZE} names` : `You saw ${n} of the ${CAST_SIZE} names`;
}

/** One chip of What you paid for: "Decade −100". */
export interface PaidChip {
  label: string;
  price: string;
}

/** What you paid for, from the log: each fact bought, in the row's order,
 *  then the overlaps and the wrong guesses, each as one chip with what
 *  they cost between them. The names are the names line's to say. */
export function paidFor(log: readonly DailyEntry[]): PaidChip[] {
  const out: PaidChip[] = [];
  const facts = new Map<DailyFactKind, number>();
  let overlaps = 0;
  let overlapCost = 0;
  let wrong = 0;
  let wrongSpent = 0;
  for (const e of log) {
    if (e.type === 'fact') facts.set(e.kind, e.cost);
    else if (e.type === 'overlap') {
      overlaps += 1;
      overlapCost += e.cost;
    } else if (e.type === 'guess') {
      wrong += 1;
      wrongSpent += e.cost;
    }
  }
  for (const kind of FACT_ORDER) {
    const cost = facts.get(kind);
    if (cost != null) out.push({ label: FACTS[kind].paid, price: `−${fmtN(cost)}` });
  }
  if (overlaps) out.push({ label: overlaps === 1 ? '1 overlap' : `${overlaps} overlaps`, price: `−${fmtN(overlapCost)}` });
  if (wrong) out.push({ label: wrong === 1 ? '1 wrong guess' : `${wrong} wrong guesses`, price: `−${fmtN(wrongSpent)}` });
  return out;
}

/** What a solve used, for the share text: "3 names, the decade and one
 *  wrong guess". */
export function usedText(game: Pick<DailyGame, 'log'>): string {
  const n = seenSlots(game).size;
  const parts = [n === 1 ? 'one name' : `${n} names`];
  const bought = new Set<DailyFactKind>();
  let overlaps = 0;
  let wrong = 0;
  for (const e of game.log) {
    if (e.type === 'fact') bought.add(e.kind);
    else if (e.type === 'overlap') overlaps += 1;
    else if (e.type === 'guess') wrong += 1;
  }
  for (const kind of FACT_ORDER) if (bought.has(kind)) parts.push(FACTS[kind].used);
  if (overlaps) parts.push(overlaps === 1 ? 'one overlap' : `${overlaps} overlaps`);
  if (wrong) parts.push(wrong === 1 ? 'one wrong guess' : `${wrong} wrong guesses`);
  return andList(parts);
}

/** The six squares, ■ for each name the reader saw, in reveal order. */
export function shareMarks(game: Pick<DailyGame, 'log'>): string {
  const seen = seenSlots(game);
  let out = '';
  for (let k = 0; k < CAST_SIZE; k++) out += seen.has(k) ? '■' : '□';
  return out;
}

/** What Share result puts on the clipboard: the puzzle and the game, the
 *  score and what it took (or how it was missed), the squares, and where
 *  to play, so a pasted result is also the way in. */
export function shareText(no: number, game: Pick<DailyGame, 'won' | 'gaveUp' | 'pts' | 'log'>, origin: string): string {
  const line = game.won ? `${fmtN(game.pts)} points · ${usedText(game)}` : game.gaveUp ? 'Gave up' : 'Missed it';
  return `Cinedikt Daily No. ${no} · ${GAME_NAME}\n${line}\n${shareMarks(game)}\n${origin}/daily`;
}

/** The streak once today's game is part of it. The server works it out
 *  when the page loads; a game finished since then, with points, adds
 *  today to the run that ended yesterday. */
export function streakAfter(
  streak: DailyToday['streak'],
  game: Pick<DailyGame, 'phase' | 'won' | 'pts'> | null,
): DailyToday['streak'] {
  if (game?.phase === 'done' && game.won && game.pts > 0 && streak.now === 0) {
    return { now: streak.before + 1, before: 0 };
  }
  return streak;
}

/** The result's streak pill: "2-day streak", "Streak started", "Streak
 *  ended", or nothing for a miss with no run to end. */
export function streakPill(streak: DailyToday['streak'], game: Pick<DailyGame, 'phase' | 'won' | 'pts'>): string {
  const s = streakAfter(streak, game);
  if (game.won) return s.now >= 2 ? `${fmtN(s.now)}-day streak` : 'Streak started';
  return s.before > 0 ? 'Streak ended' : '';
}

/** The line under the result's rule: the figure, set apart, and the rest.
 *  Null while today's board is on its way. */
export function betterLine(
  game: Pick<DailyGame, 'won'>,
  board: Pick<DailyBoard, 'beat' | 'solved'> | null,
): { figure: string; rest: string } | null {
  if (!board) return null;
  return game.won
    ? { figure: `${board.beat ?? 0}%`, rest: ' of today’s players scored less than you.' }
    : { figure: `${board.solved ?? 0}%`, rest: ' of today’s players got it.' };
}

/** The scores chart's bars: eleven, 0–99 up to 900–999 and then 1,000,
 *  each as a share of the tallest in percent, never under 6 so an empty
 *  one still shows. The reader's own is lit: the hundred their score is
 *  in, or the first for a loss, which scored nought. */
export function chartBars(
  chart: readonly number[] | null | undefined,
  game: Pick<DailyGame, 'won' | 'pts'>,
): { h: number; you: boolean }[] {
  const counts = Array.from({ length: 11 }, (_, i) => Math.max(0, chart?.[i] ?? 0));
  const most = Math.max(...counts);
  const mine = game.won ? Math.min(10, Math.max(0, Math.floor(game.pts / 100))) : 0;
  return counts.map((v, i) => ({ h: most ? Math.max(6, Math.round((v / most) * 100)) : 6, you: i === mine }));
}

/** The chart's three labels, under its first, middle and last bars. */
export const CHART_LABELS = ['0', '500', '1,000'] as const;

// ---- the leaderboard ----

/** A place as the board writes it: "7,804", or "=7,804" when another
 *  player listed shares it. */
export function placeText(place: number, tied: boolean): string {
  return `${tied ? '=' : ''}${fmtN(place)}`;
}

/** The week's days, as the column heads and the date name them. */
export const DAY_LETTERS = ['M', 'T', 'W', 'T', 'F', 'S', 'S'] as const;
export const DAY_NAMES = ['Monday', 'Tuesday', 'Wednesday', 'Thursday', 'Friday', 'Saturday', 'Sunday'] as const;

/** The day initials over the week's cells: Monday to the puzzle's day. */
export function weekLetters(date: string): string[] {
  return DAY_LETTERS.slice(0, weekdayOf(date) + 1);
}

/** A row of the leaderboard as it is drawn. */
export interface BoardRowView {
  key: string;
  place: string;
  /** "You" for the reader. */
  name: string;
  you: boolean;
  pts: string;
  /** The week's cells, Monday first, a day not played as a 0 in the third
   *  ink; null on today's board. */
  days: { v: string; zero: boolean }[] | null;
}

export function boardRows(board: Pick<DailyBoard, 'tab' | 'rows'>): BoardRowView[] {
  const week = board.tab === 'week';
  return board.rows.map((r, i) => ({
    key: `${i}:${r.you ? 'you' : r.name}`,
    place: placeText(r.place, r.tied),
    name: r.you ? 'You' : r.name,
    you: r.you,
    pts: fmtN(r.pts),
    days: week ? (r.days ?? []).map((d) => ({ v: fmtN(d), zero: d === 0 })) : null,
  }));
}

/** The note under the leaderboard. */
export function boardNote(tab: DailyTab, total: number): string {
  return tab === 'week'
    ? 'The players around your total since Monday. A day you didn’t play counts as 0.'
    : `The players around your score. Equal scores share a place. ${fmtN(total)} played today.`;
}

/** The leaderboard's tabs, in order. */
export const BOARD_TABS: readonly { tab: DailyTab; label: string }[] = [
  { tab: 'today', label: 'Today' },
  { tab: 'week', label: 'This week' },
];

/** The leaderboards fetched so far, by tab, with 'failed' for one that
 *  could not be had. */
export type BoardsSeen<T> = Partial<Record<DailyTab, T | 'failed'>>;

/** The boards a tab needs: its own, and today's whichever tab is
 *  showing, since the result's figure and chart come from today's. */
export function boardsWanted(tab: DailyTab): DailyTab[] {
  return tab === 'week' ? ['today', 'week'] : ['today'];
}

/** The same boards with any of `tabs` that failed forgotten, so they are
 *  asked for again: the page asks for every tab it wants and does not
 *  have, and a failure counts as having it until it is dropped. The same
 *  object when nothing failed, so nothing is drawn again for it. */
export function dropFailed<T>(boards: BoardsSeen<T>, tabs: readonly DailyTab[]): BoardsSeen<T> {
  if (!tabs.some((t) => boards[t] === 'failed')) return boards;
  const out = { ...boards };
  for (const t of tabs) if (out[t] === 'failed') delete out[t];
  return out;
}

/** A place as the page writes it: "1st", "112th", "1,204th". The teens
 *  are "th" whatever they end in (11th, 12th, 13th, and 111th), and
 *  otherwise a number ending 1, 2 or 3 takes "st", "nd" or "rd". */
export function ord(n: number): string {
  const teen = n % 100 >= 11 && n % 100 <= 13;
  return `${fmtN(n)}${teen ? 'th' : (['th', 'st', 'nd', 'rd'][n % 10] ?? 'th')}`;
}

/** What an opening screen says of the reader's week: "1,204th this
 *  week", or nothing with no place to say. */
export function standingText(week: DailyWeek | null | undefined): string {
  return week ? `${ord(week.rank)} this week` : '';
}

// ---- the words ----

/** The title screen. Its numbers are the rules'. */
export const TITLE_SCREEN = {
  pill: 'Cinedikt Daily',
  heading: GAME_NAME,
  lead: 'Today’s movie is hidden. Its cast shows up one name at a time, working up to the star. Name the movie in as few names as you can.',
  steps: ['See who’s in it', 'Guess, or show the next name', 'Fewer names, more points'],
  fine: `You start with ${fmtN(DAILY_START)} points. Extra names, facts and wrong guesses cost points. There’s no clock.`,
  play: 'Play',
} as const;

/** The count under Play: the number, which is set apart, and the words
 *  after it. Null before anyone has played: every puzzle opens at nought,
 *  and "0 people have played today" would say the game is empty rather
 *  than new, as the banner leaves its count out for the same reason. */
export function playedText(n: number): { count: string; rest: string } | null {
  if (n <= 0) return null;
  return { count: fmtN(n), rest: n === 1 ? ' person has played today.' : ' people have played today.' };
}

/** How it works: six numbered items, then the prices, then Got it. The
 *  prices come from the rules, so the two never disagree. */
export const HOW_IT_WORKS = {
  heading: 'How it works',
  items: [
    'Today’s movie starts as a blank card in its poster’s colour, and you see one person from its cast, with another movie they were in.',
    'Guess whenever you like. Each wrong guess, or each tap on Next name, shows another person, working up to the star.',
    'Every wrong guess says how warm it was: cold, warm or hot, with the decade and genre compared.',
    'Stuck? Buy a fact about the movie: a length range, a rating range, its genre, the decade and then a five-year range, or the director.',
    `Tap Movies on any name to see their movies on a Cinedikt map. Facts you buy mark the map, and for ${OVERLAP_COST} you can add another name to light only the movies they share.`,
    'A wrong guess also fills in anyone from the cast it shares with today’s movie.',
  ],
  then: `You start with ${fmtN(DAILY_START)} points. Each extra name costs ${NEXT_COST}. Wrong guesses cost ${wrongCost(0)}, then ${wrongCost(1)}, ${wrongCost(2)} and so on. Facts cost ${Math.min(...Object.values(FACT_COST))} to ${Math.max(...Object.values(FACT_COST))}. There’s no clock.`,
  close: 'Got it',
} as const;

/** The game page's heading while the game is on; the answer's title once
 *  it is over. */
export const PLAY_HEADING = 'Name today’s movie';

/** The line under it, while the game is on; nothing once it is over. */
export const PLAY_LINE = 'Its cast shows up one name at a time, working up to the star.';

export function pageHeading(game: Pick<DailyGame, 'end'> | null): string {
  return game?.end ? game.end.answer.title : PLAY_HEADING;
}

export function pageLine(game: Pick<DailyGame, 'phase'> | null): string {
  return game?.phase === 'done' ? '' : PLAY_LINE;
}

/** The cast list's name for a screen reader, and its heading once the
 *  game is over. */
export const CAST_LIST_LABEL = 'Today’s names';
export const CAST_HEADING = 'The cast';

/** The guess field's placeholder and name. "/" reaches for it. */
export const GUESS_PLACEHOLDER = 'Name the movie';

/** Show the answer takes two presses: this, then SHOW_ANSWER_SURE within
 *  REVEAL_CONFIRM_MS. */
export const SHOW_ANSWER = 'Show the answer';
export const SHOW_ANSWER_SURE = 'Sure? Show it';

/** The toasts. */
export const TOASTS = {
  tried: 'You’ve already tried that one',
  copied: 'Result copied',
  copyFailed: 'Couldn’t copy the result',
} as const;

/** The result's label for the countdown to the reader's midnight. */
export const NEXT_MOVIE_IN = 'Next movie in';

// ---- the banner on the opening screen ----

/** The line beside the banner's button: the points left during a game,
 *  the score or the miss once it is over, and before it the reader's
 *  place this week or, without one, how many are playing. Nobody yet is
 *  not a crowd worth mentioning: "0 playing today" would say the game is
 *  empty, not that it is new. */
export function bannerLine(
  game: Pick<DailyGame, 'phase' | 'won' | 'pts'> | null,
  played: number,
  week: DailyWeek | null | undefined,
): string {
  if (game?.phase === 'done') return game.won ? `${fmtN(game.pts)} points today` : 'Missed today';
  if (game?.phase === 'play') return `${fmtN(game.pts)} points left`;
  return standingText(week) || (played > 0 ? `${fmtN(played)} playing today` : '');
}

/** The banner's name for a screen reader, as the handoff writes it:
 *  "Cinedikt Daily, No. 143: Name Drop. Play", and once the game is over
 *  "Cinedikt Daily, No. 143. See your result". A game under way ends on
 *  the button's own words, Keep going, so a reader who says what they see
 *  to their speech software is understood. */
export function bannerLabel(no: number, game: Pick<DailyGame, 'phase'> | null): string {
  if (game?.phase === 'done') return `Cinedikt Daily, No. ${no}. See your result`;
  return `Cinedikt Daily, No. ${no}: ${GAME_NAME}. ${game ? 'Keep going' : 'Play'}`;
}

// ---- the Movies sheet ----
//
// One showing person's movies on a small Cinedikt map, with the reader's
// bought facts drawn on it. Today's movie is among the cards, unmarked:
// the server sends a poster for every card or for none, and nothing here
// can tell it from the rest. Length is not drawn, since other movies'
// runtimes are not on a map.

/** The years a bought decade or five years light, inclusive, or null
 *  with neither bought. The five years win: they are inside the decade. */
export function yearSpan(facts: Pick<DailyFacts, 'decade' | 'years'>): [number, number] | null {
  if (facts.years != null) return [facts.years, facts.years + 4];
  if (facts.decade != null) return [facts.decade, facts.decade + 9];
  return null;
}

/** Each rating band as ratings, from its floor up to, and not including,
 *  its ceiling. The top band has none. */
export const RATING_SPANS: readonly (readonly [number, number])[] = [
  [0, 6],
  [6, 7],
  [7, 8],
  [8, Infinity],
];

/** The ratings a bought rating band lights, or null with none bought. */
export function ratingSpan(facts: Pick<DailyFacts, 'rating'>): readonly [number, number] | null {
  return facts.rating == null ? null : (RATING_SPANS[facts.rating] ?? null);
}

/** Who the sheet has selected: the showing slots switched on, the one it
 *  opened on always among them, and the director chip, which means any
 *  of today's directors. */
export interface SheetChoice {
  slots: readonly number[];
  director: boolean;
}

/** Whether a card is lit: every selected person is on it, it is inside
 *  the bought years and rating band, and it has every one of the answer's
 *  genres once Genre is bought. The rest fade. */
export function movieLit(
  movie: Pick<DailyMovie, 'year' | 'rating' | 'genres' | 'on' | 'dir'>,
  chosen: SheetChoice,
  facts: DailyFacts,
): boolean {
  if (!chosen.slots.every((k) => movie.on.includes(k))) return false;
  if (chosen.director && !movie.dir) return false;
  const years = yearSpan(facts);
  if (years && (movie.year < years[0] || movie.year > years[1])) return false;
  const ratings = ratingSpan(facts);
  if (ratings && !(movie.rating >= ratings[0] && movie.rating < ratings[1])) return false;
  if (facts.genre && !facts.genre.every((g) => movie.genres.includes(g))) return false;
  return true;
}

/** The sheet's title: "Joe Pantoliano’s movies", or with more selected,
 *  "Movies with Joe Pantoliano and Gloria Foster". The person it opened
 *  on comes first; the director chip names every director. */
export function sheetTitle(names: readonly string[]): string {
  return names.length > 1 ? `Movies with ${andList(names)}` : `${names[0] ?? ''}’s movies`;
}

/** The line under it: "6 of 14 movies lit · on Cinedikt". */
export function sheetSub(lit: number, total: number): string {
  return `${fmtN(lit)} of ${fmtN(total)} ${total === 1 ? 'movie' : 'movies'} lit · on Cinedikt`;
}

/** The legend over the map: the facts drawn on it, or what buying them
 *  and adding names would do. Either way, that today's movie is there and
 *  not marked. */
export function sheetLegend(facts: DailyFacts): string {
  const marks: string[] = [];
  if (facts.years != null) marks.push(yearsText(facts.years));
  else if (facts.decade != null) marks.push(decadeText(facts.decade));
  if (facts.rating != null) marks.push(`rated ${inSentence(ratingBandText(facts.rating))}`);
  if (facts.genre?.length) marks.push(facts.genre.join(', '));
  const tail = 'Today’s movie is one of these cards, but it isn’t marked.';
  return marks.length
    ? `Your facts are on the map: ${marks.join(' · ')}. ${tail}`
    : `Facts you buy mark this map. Add another name to light only the movies they share. ${tail}`;
}

/** The footer with nothing picked. */
export function sheetHint(nextCost: number): string {
  return `Tap a movie to guess it. A wrong guess costs ${fmtN(nextCost)}.`;
}

/** The footer's line under a picked card's title: "1993 · IMDb 7.8". */
export function pickLine(movie: Pick<DailyMovie, 'year' | 'rating'>): string {
  const rated = `IMDb ${movie.rating.toFixed(1)}`;
  return movie.year > 0 ? `${movie.year} · ${rated}` : rated;
}

/** A card's name for a screen reader: what it is, and whether it is
 *  faded or already tried. */
export function sheetCardLabel(movie: Pick<DailyMovie, 'title' | 'year' | 'rating'>, lit: boolean, tried: boolean): string {
  return `${movie.title}, ${movie.year}, rated ${movie.rating.toFixed(1)}${tried ? ', already tried' : ''}${lit ? '' : ', dimmed'}`;
}

/** A person's chip on the sheet: their name once theirs to switch, or
 *  "+ Gloria Foster" and its price before their overlap is bought. */
export function overlapChip(name: string, owned: boolean): { label: string; price: string } {
  return owned ? { label: name, price: '' } : { label: `+ ${name}`, price: `−${OVERLAP_COST}` };
}

/** The sheet's small map: the app's layout at the handoff's metrics. */
export const SHEET_METRICS = {
  desktop: { cardW: 112, cardH: 42, railW: 44 },
  phone: { cardW: 100, cardH: 42, railW: 40 },
  gap: 4,
  padTop: 3,
  padBottom: 3,
  gapMark: 4,
  axisH: 26,
  /** The ratings the axis labels. */
  labels: [4, 5, 6, 7, 8, 9],
} as const;

/** The sheet opens scrolled so its first lit card sits this far from the
 *  map's top, a bought range already in view. */
export const SHEET_LIT_TOP = 36;

// ---- the page ----

/** The game page's parts, top to bottom. While the game is on: the top
 *  block, the cast and then the facts. Once it is over: the top block,
 *  About the movie, the result, the leaderboard, "The cast" heading and
 *  the six names. The page draws them keyed in this order, so the order
 *  on the page is the order a screen reader hears, and the cast keeps its
 *  place in the tree as the rest arrive round it: a list moved in the
 *  tree would start its rows' transitions afresh, and the names the
 *  reader didn't see would arrive all at once. */
export type PageSection = 'top' | 'cast' | 'facts' | 'about' | 'result' | 'board' | 'castHead';
export function pageSections(done: boolean): PageSection[] {
  return done ? ['top', 'about', 'result', 'board', 'castHead', 'cast'] : ['top', 'cast', 'facts'];
}

/** The cast list while Play hides every name (PLAY_HIDE_MS): six rows,
 *  none next, so the first name animates in from nothing. */
export function openingRows(): CastRow[] {
  return Array.from({ length: CAST_SIZE }, (_, slot): CastRow => ({ slot, state: 'hidden', bar: BAR_WIDTHS[slot] }));
}

/** When each name the reader didn't see appears at the end, by slot:
 *  one after another in reveal order (endRevealDelay). Names seen during
 *  the game are already there, and are left out. */
export function revealDelays(rows: readonly CastRow[]): Map<number, number> {
  const out = new Map<number, number>();
  for (const r of rows) if (r.state === 'shown' && !r.seen) out.set(r.slot, endRevealDelay(out.size));
  return out;
}

/** Everyone the page can name today, for the initials on faces with no
 *  photo: the names showing, then the directors once bought or the game
 *  is over. Unique by id. */
export function todaysPeople(game: Pick<DailyGame, 'slots' | 'facts' | 'end'>): DailyPerson[] {
  const seen = new Map<string, DailyPerson>();
  const add = (p: DailyPerson) => {
    if (!seen.has(p.id)) seen.set(p.id, p);
  };
  shownSlots(game).forEach((s) => add(s.person));
  (game.end?.directors ?? game.facts.director ?? []).forEach(add);
  return [...seen.values()];
}

/** The wrong guess for this movie, for the message box: the newest one,
 *  or the one whose chip was pressed. */
export function guessById(game: Pick<DailyGame, 'log'> | null, id: string | null): DailyGuess | null {
  if (!id) return null;
  return guessesOf(game).find((g) => g.id === id) ?? null;
}

/** The newest wrong guess, which the message box shows as it comes back. */
export function newestGuess(game: Pick<DailyGame, 'log'>): DailyGuess | null {
  const all = guessesOf(game);
  return all[all.length - 1] ?? null;
}

/** The most faces the message box draws, overlapping, before its chips. */
export const MESSAGE_FACES = 4;

/** The result's six faces, in reveal order, each marked seen or not: the
 *  ones the reader saw are drawn in colour, the rest faded and grey. */
export function resultFaces(game: Pick<DailyGame, 'slots' | 'log'>): { person: DailyPerson; seen: boolean }[] {
  const seen = seenSlots(game);
  return shownSlots(game).map((s) => ({ person: s.person, seen: seen.has(s.slot) }));
}

/** The catalog's search answers from two characters, as the header's. */
export const SEARCH_MIN_CHARS = 2;

/** What the reader is told, at once and without asking, about a move the
 *  server would refuse: a name when everyone is showing, a fact already
 *  bought or the years before the decade, an overlap already owned or for
 *  someone not showing, a purchase the points would not leave one over
 *  from, a movie already guessed. Empty for a move worth sending. The
 *  page's buttons already hold back most of these; this is for the rest,
 *  and for a press that lands as the game changes under it. */
export function earlyRefusal(
  game: Pick<DailyGame, 'pts' | 'slots' | 'facts' | 'overlaps' | 'log'>,
  move: DailyMove,
): string {
  switch (move.kind) {
    case 'next':
      if (!game.slots.some((s) => !s.shown)) return refusalText('known', 'next');
      return affords(game.pts, NEXT_COST) ? '' : refusalText('points', 'next');
    case 'buy':
      if (hasFact(move.fact, game.facts)) return refusalText('known', 'buy');
      if (move.fact === 'years' && !hasFact('decade', game.facts)) return refusalText('bad', 'buy');
      return affords(game.pts, FACT_COST[move.fact]) ? '' : refusalText('points', 'buy');
    case 'overlap':
      if (game.overlaps.includes(move.person)) return refusalText('known', 'overlap');
      if (!shownSlots(game).some((s) => s.person.id === move.person)) return refusalText('bad', 'overlap');
      return affords(game.pts, OVERLAP_COST) ? '' : refusalText('points', 'overlap');
    case 'guess':
      return guessedIds(game).has(move.film) ? TOASTS.tried : '';
    case 'reveal':
      return '';
  }
}

/** Where the page sits in the visual viewport: from its top, as tall as
 *  it is, so the guess bar rides just above an on-screen keyboard rather
 *  than under it, as iOS leaves the layout viewport full height behind
 *  it. Null while the reader is zoomed in, when the visual viewport is a
 *  window onto the page rather than the room it has, and the page keeps
 *  its own size. */
export function viewportFit(
  vv: { height: number; offsetTop: number; scale: number } | null | undefined,
): { top: number; height: number } | null {
  if (!vv || !(vv.height > 0) || Math.abs(vv.scale - 1) > 0.01) return null;
  return { top: Math.max(0, Math.round(vv.offsetTop)), height: Math.round(vv.height) };
}

// ---- time ----

/** "11:47:03": hours, minutes and seconds left, never below zero. */
export function countdown(ms: number): string {
  const left = Math.max(0, Math.floor(ms / 1000));
  const h = Math.floor(left / 3600);
  const m = Math.floor(left / 60) % 60;
  return `${h}:${String(m).padStart(2, '0')}:${String(left % 60).padStart(2, '0')}`;
}

/** How far the server's clock is ahead of this one, in ms, from the time
 *  it gave and the moment it arrived. The countdown runs on the server's
 *  time: a reader whose clock is wrong still sees the next movie arrive
 *  at their own midnight, which the server worked out. The time it gave
 *  was stamped before the answer set off, so this errs behind the server,
 *  and what waits on it comes late rather than early. */
export function clockOffset(serverNow: string, localNow: number): number {
  const t = Date.parse(serverNow);
  return Number.isFinite(t) ? t - localNow : 0;
}

/** The longest the midnight watch goes without looking at the clock. A
 *  timer set hours ahead can be held back by a machine asleep or a tab in
 *  the background, and may count on a clock that stops while the machine
 *  sleeps, so the wait is taken in steps no longer than this, each
 *  measured afresh; the last step lands on midnight itself. */
export const MIDNIGHT_STEP_MS = 30_000;

/** Calls `onMidnight` once, when `next` (the reader's next midnight, an
 *  RFC 3339 instant) arrives on the server's clock, and hands back what
 *  calls it off. The one timer in this file: the page and the opening
 *  screen's banner each ask for the new puzzle with it, the moment the
 *  old one ends. A `next` that cannot be read, or that has already gone
 *  by as the watch starts, is not waited for: the server said it was
 *  ahead, so asking again would only bring back the same answer, again
 *  and again. */
export function watchMidnight(next: string, offset: number, onMidnight: () => void): () => void {
  const at = Date.parse(next);
  let timer: ReturnType<typeof setTimeout> | undefined;
  const look = (first: boolean) => {
    const left = at - (Date.now() + offset);
    if (left <= 0) {
      if (!first) onMidnight();
      return;
    }
    timer = setTimeout(() => look(false), Math.min(left, MIDNIGHT_STEP_MS));
  };
  if (Number.isFinite(at)) look(true);
  return () => clearTimeout(timer);
}

// ---- the date ----

const MONTHS = [
  'January',
  'February',
  'March',
  'April',
  'May',
  'June',
  'July',
  'August',
  'September',
  'October',
  'November',
  'December',
] as const;

/** The puzzle's day of the week, 0 for Monday, which is where its week
 *  starts. The day is a calendar date with no zone of its own: it is
 *  read as midnight UTC, and in UTC, so the reader's zone can never move
 *  it a day either way. */
export function weekdayOf(date: string): number {
  const d = new Date(`${date}T00:00:00Z`);
  return (d.getUTCDay() + 6) % 7;
}

/** "Friday 9 October", as en-GB writes it, in UTC: the calendar date the
 *  puzzle belongs to, which is the reader's own date, written as it
 *  stands whatever zone the page is in. Built by hand from the UTC parts
 *  rather than through Intl, whose en-GB output has moved a comma about
 *  between versions. */
export function dailyDayText(date: string): string {
  const d = new Date(`${date}T00:00:00Z`);
  if (Number.isNaN(d.getTime())) return '';
  return `${DAY_NAMES[weekdayOf(date)]} ${d.getUTCDate()} ${MONTHS[d.getUTCMonth()]}`;
}

/** The header's date: "No. 143 · Friday 9 October", or "No. 143" on a
 *  phone, where the day does not fit beside the wordmark. */
export function dailyDateText(day: { no: number; date: string }, phone: boolean): string {
  const text = dailyDayText(day.date);
  return phone || !text ? `No. ${day.no}` : `No. ${day.no} · ${text}`;
}

// ---- moves ----

/** A fresh idempotency key for a move: what crypto.randomUUID gives, or,
 *  where it is missing, as many random bits written in hex. */
export function newKey(): string {
  const c = globalThis.crypto;
  if (typeof c?.randomUUID === 'function') return c.randomUUID();
  const bytes = new Uint8Array(16);
  if (typeof c?.getRandomValues === 'function') c.getRandomValues(bytes);
  else for (let i = 0; i < bytes.length; i++) bytes[i] = Math.floor(Math.random() * 256);
  return [...bytes].map((b) => b.toString(16).padStart(2, '0')).join('');
}

/** What a move is, from where: the same move from the same point in the
 *  game is the same request, and is sent again under the same key after
 *  an answer that never came. */
export function moveSig(move: DailyMove, seq: number): string {
  const arg =
    move.kind === 'buy' ? move.fact : move.kind === 'overlap' ? move.person : move.kind === 'guess' ? move.film : '';
  return `${seq}:${move.kind}:${arg}`;
}

/** Said when a request never got an answer, and for a refusal with no
 *  words of its own. Sending the same move again is safe. */
export const UNREACHABLE = 'Couldn’t reach Cinedikt. Try again.';

/** Said while today's puzzle has not been picked yet. */
export const NOT_READY = 'Today’s movie isn’t ready yet. Try again in a few minutes.';

/** Said when a move is refused because the reader's day has turned, and
 *  when their midnight comes with a game part way through. */
export const DAY_OVER = 'That day’s game is over.';

/** What the reader is told when a move is refused, by the server's
 *  reason. Empty for a refusal that is handled without a word: "stale"
 *  hands back the game as it stands, and the page simply draws it. */
export function refusalText(
  reason: string | null,
  kind?: DailyMove['kind'] | 'play' | 'name' | 'movies',
): string {
  switch (reason) {
    case 'stale':
      return '';
    case 'points':
      return 'Not enough points for that.';
    case 'known':
      if (kind === 'guess') return TOASTS.tried;
      if (kind === 'next') return 'Everyone’s showing.';
      if (kind === 'overlap') return 'You’ve already added them.';
      return 'You already have that fact.';
    case 'bad':
      if (kind === 'buy') return 'Buy the decade first.';
      if (kind === 'overlap' || kind === 'movies') return 'That name isn’t showing yet.';
      return UNREACHABLE;
    case 'unknown':
      return 'Cinedikt doesn’t know that movie.';
    case 'day':
      return DAY_OVER;
    case 'done':
      return 'This game is already over.';
    case 'cookie':
    case 'no-game':
      return 'Your game couldn’t be found. Press Play to start again.';
    case 'busy':
      return 'Too many new players from here just now. Try again in a few minutes.';
    case 'not-ready':
      return NOT_READY;
    default:
      return UNREACHABLE;
  }
}

/** What the reader is told when their midnight comes with the page open:
 *  that the day is over, if they were part way through a game, which goes
 *  as the new puzzle comes; nothing over the title screen or a result,
 *  which simply give way to it. */
export function midnightText(game: Pick<DailyGame, 'phase'> | null): string {
  return game?.phase === 'play' ? DAY_OVER : '';
}

/** The button that starts the reader again, as a new player on a newly
 *  picked movie. Development only, and it says so: a server in
 *  production never offers it (DailyToday's `dev`). */
export const PLAY_AGAIN = 'Play again (development only)';

/** Said when Play again could not start the reader again. The page is
 *  left as it was, and pressing again is safe. */
export const AGAIN_FAILED = 'Couldn’t start again. Try again.';

/** The game a "stale" refusal hands back, or null. */
export function staleGame(body: unknown): DailyGame | null {
  const g = (body as { game?: unknown } | null)?.game;
  if (!g || typeof g !== 'object') return null;
  const game = g as DailyGame;
  return Array.isArray(game.log) && Array.isArray(game.slots) ? game : null;
}

// ---- motion ----
//
// The design's numbers, written once, so each can be checked. Every one is
// off for a reader who has asked for nothing to move: names appear at
// once, the card does not turn, the score does not count and scrolls are
// instant. Script animations go through motion.ts's animate, which asks;
// the stylesheet's transitions have reduced-motion rules of their own.

/** Play hides every name for this long, and then the first one comes in. */
export const PLAY_HIDE_MS = 450;

/** A name appearing: its placeholder fades out; its text fades in and
 *  rises from 6px; its face fades in and grows from 0.7 with a bounce. */
export const NAME_OUT_MS = 300;
export const NAME_IN_MS = 450;
export const NAME_RISE_PX = 6;
export const FACE_GROW_MS = 500;
export const FACE_GROW_FROM = 0.7;
export const FACE_GROW_EASE = 'cubic-bezier(0.2, 0.9, 0.3, 1.25)';

/** At the end the names the reader didn't see appear one after another:
 *  the first 350ms in, then 140ms apart. */
export const END_REVEAL_FIRST_MS = 350;
export const END_REVEAL_STEP_MS = 140;
export function endRevealDelay(i: number): number {
  return END_REVEAL_FIRST_MS + i * END_REVEAL_STEP_MS;
}

/** The photo preview: on a mouse, after resting on a face this long; it
 *  fades in quickly and grows from 0.92 with a little bounce, 54px from
 *  the face's left edge. */
export const PEEK_REST_MS = 160;
export const PEEK_FADE_MS = 160;
export const PEEK_GROW_MS = 220;
export const PEEK_GROW_FROM = 0.92;
export const PEEK_EASE = 'cubic-bezier(0.2, 0.9, 0.3, 1.2)';
export const PEEK_LEFT = 54;

/** The hidden card turning over at the end. */
export const CARD_TURN_MS = 800;
export const CARD_TURN_EASE = 'cubic-bezier(0.4, 0, 0.2, 1)';

/** The score counts up from nought over this long, easing out, when the
 *  game ends on the page. A finished game opened later just shows it. */
export const SCORE_COUNT_MS = 1100;

/** The score `elapsed` ms into its count: ease-out cubic, landing on the
 *  score itself. */
export function countUp(score: number, elapsed: number): number {
  const p = Math.min(1, Math.max(0, elapsed / SCORE_COUNT_MS));
  return Math.round(score * (1 - Math.pow(1 - p, 3)));
}

/** How long "Sure? Show it" waits for its second press before it goes
 *  back to Show the answer. */
export const REVEAL_CONFIRM_MS = 3500;

/** The Movies sheet on a phone: 90% of the visual viewport tall, closed
 *  by a drag of its top edge past 90px, and otherwise springing back. */
export const SHEET_PHONE_SHARE = 0.9;
export const SHEET_CLOSE_PX = 90;
export const SHEET_SPRING_MS = 250;
export const SHEET_SPRING_EASE = 'cubic-bezier(0.2, 0.9, 0.3, 1)';

/** How long the results list stays after the field loses the focus, so a
 *  press on a row lands before the list goes. */
export const LIST_CLOSE_MS = 120;

/** How long typing has to pause before the guess field searches. The
 *  header's search waits as long. */
export const SEARCH_WAIT_MS = 250;

// ---- touch ----

/** Every control's hit area is at least 44px, even where it looks smaller,
 *  through a transparent ::before on the control, set this far out
 *  (inset, vertical then horizontal). The stylesheet's rules are checked
 *  against these. */
export const HIT_INSETS = {
  /** A wrong guess's chip, 26px tall. */
  triedChip: '-9px -3px',
  /** Show the answer, 32px. */
  showAnswer: '-6px 0',
  /** A fact for sale, 34px. */
  fact: '-5px -3px',
  /** A shown row's Movies button, 34px. */
  movies: '-5px -2px',
  /** Next name, 36px. */
  next: '-4px 0',
  /** A leaderboard tab, 30px. */
  tab: '-7px 0',
} as const;

/** The Movies sheet's chips are this tall on a phone, a finger's size
 *  without a hit area of their own. */
export const SHEET_CHIP_PHONE_H = 40;

/** A landscape phone: under 520px tall and wider than tall. The guess bar
 *  becomes a 340px column on the right of the cast. */
export const LANDSCAPE_MAX_H = 520;
export const LANDSCAPE_QUERY = `(max-height: ${LANDSCAPE_MAX_H - 0.02}px) and (orientation: landscape)`;
export function isLandscapePhone(width: number, height: number): boolean {
  return height < LANDSCAPE_MAX_H && width > height;
}
