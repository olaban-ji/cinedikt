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
// told, what the result and the share say, what the Movies sheet says,
// and when and how things move. Everything is pure but the midnight
// watch, which is a timer, and all of it is checked without a DOM in
// daily.test.ts.

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
  DailyPerson,
  DailyReadableMovie,
  DailySlot,
  DailyTab,
  DailyToday,
  DailyWarmth,
  DailyWeek,
} from './api';
import { initialsFor } from './grid';
import { screenOf } from './screen';
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

/** The next name, shown from the cast's dashed next row. */
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

/** The hues of the title screen's three faces: the row that is always
 *  shown, then the two that drop into the next and hidden rows. */
export const TITLE_HUES = [118, 205, 345] as const;

/** How far the hidden card's glow is lightened towards white from the
 *  poster colour, as a share of the way. */
export const GLOW_LIGHTEN = 0.45;

/** The glow round the hidden card, at alpha `a`: today's poster colour
 *  lightened towards white, so a dark poster still lights the page round
 *  it. Worked out here and set inline (as --glow, or in an animation's
 *  frames), since a stylesheet colour could not follow the day's poster;
 *  rgba rather than oklch, so it reads the same in every browser that
 *  can animate a shadow. Null for anything but the "#rrggbb" the server
 *  promises, and then the card simply has no glow. */
export function posterGlow(colour: string | null | undefined, a: number): string | null {
  if (!isPosterColour(colour)) return null;
  const n = parseInt(colour.slice(1), 16);
  const lift = (v: number) => Math.round(v + (255 - v) * GLOW_LIGHTEN);
  return `rgba(${lift((n >> 16) & 255)}, ${lift((n >> 8) & 255)}, ${lift(n & 255)}, ${a})`;
}

/** The accent as the design's glows draw it, rgba(170, 140, 255), at
 *  alpha `a`: the title screen's next row pulsing and the facts panel's
 *  nudge. Only ever set from script, into an animation's frames, never
 *  written in the stylesheet, which keeps its colours in tokens. */
export function accentGlow(a: number): string {
  return `rgba(170, 140, 255, ${a})`;
}

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
  return isPosterColour(colour) ? colour : 'var(--c)';
}

/** Whether a colour is the "#rrggbb" the server promises for a poster's
 *  average, and so safe to put into a style. */
function isPosterColour(colour: string | null | undefined): colour is string {
  return typeof colour === 'string' && /^#[0-9a-f]{6}$/i.test(colour);
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

/** The facts panel's heading: what is for sale, or About the movie. */
export function factsHeading(done: boolean): string {
  return done ? 'About the movie' : 'Buy a fact';
}

/** The note beside the heading while the game is on, and what it becomes
 *  when the reader seems stuck (factNudge). */
export const FACTS_NOTE = 'Each one also marks the map';
export const NUDGE_NOTE = 'Stuck? A fact narrows it down.';

/** The facts are easy to miss, so once three names are showing and not
 *  one fact has been bought, the panel asks: its ring turns the accent,
 *  its note says so, and, once only, it pulses. */
export const NUDGE_NAMES = 3;

export function factNudge(game: Pick<DailyGame, 'phase' | 'slots' | 'facts'>): boolean {
  return (
    game.phase === 'play' &&
    shownSlots(game).length >= NUDGE_NAMES &&
    !FACT_ORDER.some((kind) => hasFact(kind, game.facts))
  );
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

/** Whether the next name can be asked for: a game on, a name still
 *  hidden, and the points to leave one over after it. */
export function canNext(game: Pick<DailyGame, 'phase' | 'pts' | 'slots'>): boolean {
  return game.phase === 'play' && game.slots.some((s) => !s.shown) && affords(game.pts, NEXT_COST);
}

/** The slot the next name is: the first hidden one in reveal order, or
 *  null when it cannot be asked for. */
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
      /** Its Movies button, while the game is on (moviesButton). */
      movies: MoviesButton;
    }
  /** The next to show: the whole row is a button (NEXT_ROW_LABEL). */
  | { slot: number; state: 'next'; bar: number }
  | { slot: number; state: 'hidden'; bar: number };

/** The cast list, in reveal order. The row the next name would fill is
 *  the "next" one, the only way to ask for it, only while it can be
 *  pressed; otherwise every hidden row is just hidden. A hidden row
 *  carries no name at all: the page is never told one. */
export function castRows(game: Pick<DailyGame, 'phase' | 'pts' | 'slots' | 'log' | 'sheet'>): CastRow[] {
  const next = nextSlot(game);
  const seen = seenSlots(game);
  return game.slots.map((s): CastRow => {
    if (s.shown) {
      const movies = moviesButton(game, s.person);
      return { slot: s.slot, state: 'shown', person: s.person, line: castLine(s), seen: seen.has(s.slot), movies };
    }
    return { slot: s.slot, state: s.slot === next ? 'next' : 'hidden', bar: BAR_WIDTHS[s.slot] ?? 50 };
  });
}

/** The next row's name for a screen reader, which is the whole row. */
export const NEXT_ROW_LABEL = `Show the next name. It costs ${NEXT_COST} points.`;

/** A shown row's Movies button, for a screen reader. */
export function moviesLabel(name: string): string {
  return `See ${name}’s movies on a Cinedikt map`;
}

/** The person whose Movies map the reader chose, among the names
 *  showing, or null before they have chosen. The server lets only a
 *  showing person be chosen, and a name once shown stays shown, so the
 *  one chosen is always found. */
export function sheetPerson(game: Pick<DailyGame, 'sheet' | 'slots'>): DailyPerson | null {
  if (!game.sheet) return null;
  return shownSlots(game).find((s) => s.person.id === game.sheet)?.person ?? null;
}

/** A shown row's Movies button while the game is on. One Movies map a
 *  game (DailyGame's `sheet`), so it is one of three:
 *  - "choose" before any map is chosen: pressing it asks first
 *    (sheetAsk), since the choice holds for the whole game;
 *  - "open" on the person chosen, whose map opens straight away, as
 *    often as the reader likes;
 *  - "locked" on everyone else: left in its place, so the rows keep
 *    their shape and the reader sees why, but it cannot be pressed, and
 *    its label, said and shown as its tooltip, names whose map is open. */
export interface MoviesButton {
  state: 'choose' | 'open' | 'locked';
  /** Its name for a screen reader; a locked one's tooltip as well. */
  label: string;
}

export function moviesButton(
  game: Pick<DailyGame, 'sheet' | 'slots'>,
  person: Pick<DailyPerson, 'id' | 'name'>,
): MoviesButton {
  if (!game.sheet) return { state: 'choose', label: moviesLabel(person.name) };
  if (game.sheet === person.id) return { state: 'open', label: moviesLabel(person.name) };
  return { state: 'locked', label: sheetLockedLabel(sheetPerson(game)?.name ?? null) };
}

/** The slot whose colour runs out of the hidden card as a ripple, now
 *  that `now` has come back from `was`: the name that has just appeared,
 *  in a game still on. One name bought is that name. A wrong guess fills
 *  in everyone it shares and then shows the next name, and the ripple is
 *  that last one's, the newest, as the cast list shows them coming;
 *  failing that, the last it filled in. Null when no name has appeared,
 *  for a game just opened (`was` null) and for one that has ended, whose
 *  names arrive at the end with no ripple. */
export function rippleSlot(
  was: Pick<DailyGame, 'slots'> | null,
  now: Pick<DailyGame, 'phase' | 'slots' | 'log'>,
): number | null {
  if (!was || now.phase !== 'play') return null;
  const before = new Set(shownSlots(was).map((s) => s.slot));
  const fresh = shownSlots(now)
    .map((s) => s.slot)
    .filter((k) => !before.has(k));
  if (!fresh.length) return null;
  const shared = new Set(newestGuess(now)?.shared ?? []);
  const next = fresh.filter((k) => !shared.has(k));
  const pool = next.length ? next : fresh;
  return pool[pool.length - 1];
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

// The guess bar is one row, the field and Guess, with no points line and
// no Next name button: the next name comes from the dashed next row in
// the cast. What the line used to say is in the row's own words.

/** The field's placeholder: what the next miss costs, rising as wrong
 *  guesses get dearer, or, when that miss would end the game, that this
 *  is the last guess. */
export function guessPlaceholder(game: Pick<DailyGame, 'pts' | 'nextCost'>): string {
  return lastGuess(game)
    ? `${GUESS_FIELD} · last guess`
    : `${GUESS_FIELD} · a miss costs ${fmtN(game.nextCost)}`;
}

/** Guess, with the points getting it now would score: "Guess · 1,000". */
export function guessLabel(game: Pick<DailyGame, 'pts'>): string {
  return `Guess · ${fmtN(game.pts)}`;
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
 *  then the wrong guesses, as one chip with what they cost between them.
 *  The names are the names line's to say. */
export function paidFor(log: readonly DailyEntry[]): PaidChip[] {
  const out: PaidChip[] = [];
  const facts = new Map<DailyFactKind, number>();
  let wrong = 0;
  let wrongSpent = 0;
  for (const e of log) {
    if (e.type === 'fact') facts.set(e.kind, e.cost);
    else if (e.type === 'guess') {
      wrong += 1;
      wrongSpent += e.cost;
    }
  }
  for (const kind of FACT_ORDER) {
    const cost = facts.get(kind);
    if (cost != null) out.push({ label: FACTS[kind].paid, price: `−${fmtN(cost)}` });
  }
  if (wrong) out.push({ label: wrong === 1 ? '1 wrong guess' : `${wrong} wrong guesses`, price: `−${fmtN(wrongSpent)}` });
  return out;
}

/** What a solve used, for the share text: "3 names, the decade and one
 *  wrong guess". */
export function usedText(game: Pick<DailyGame, 'log'>): string {
  const n = seenSlots(game).size;
  const parts = [n === 1 ? 'one name' : `${n} names`];
  const bought = new Set<DailyFactKind>();
  let wrong = 0;
  for (const e of game.log) {
    if (e.type === 'fact') bought.add(e.kind);
    else if (e.type === 'guess') wrong += 1;
  }
  for (const kind of FACT_ORDER) if (bought.has(kind)) parts.push(FACTS[kind].used);
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
    'Guess whenever you like. Each wrong guess, or each tap on the next name, shows another person, working up to the star.',
    'Every wrong guess says how warm it was: cold, warm or hot, with the decade and genre compared.',
    'Stuck? Buy a fact about the movie: a length range, a rating range, its genre, the decade and then a five-year range, or the director.',
    'Tap Movies on a name to see their movies on a Cinedikt map. You get one map a game, so choose whose. Titles only show inside the ranges you’ve bought.',
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

/** The guess field's name, which its placeholder starts with
 *  (guessPlaceholder). "/" reaches for it. */
export const GUESS_FIELD = 'Name the movie';

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
// bought facts drawn on it. Titles only show inside the ranges bought,
// and the server is what keeps to that: a movie outside them comes as a
// blank card, a year and a place on the rating axis, so the page cannot
// show what it was never told, and two people's maps cannot be laid
// side by side to find the one movie both are on. Today's movie is among
// the cards by the same rule as any other, unmarked. Length is not a
// range here, since other movies' runtimes are not on a map.
//
// And there is one map a game. With titles readable inside the ranges
// bought, two people's readable titles side by side almost always leave
// only today's movie: the overlap the game took away, back for the price
// of a range. So the reader chooses whose map to open, once, and the
// server holds them to it, answering for no one else until the end. The
// page asks before the choice is made (sheetAsk), and after it every
// other name's button stays put, shut (moviesButton).

/** The rule, said wherever the choice comes up. */
export const SHEET_ONE = 'You get one Movies map a game.';

/** What the page asks before the game's one map is chosen. */
export interface SheetAsk {
  /** "Open Joe Pantoliano’s movies?" */
  title: string;
  body: string;
  /** Opens it, for good. */
  yes: string;
  /** Leaves every map closed, and the choice still to make. */
  no: string;
}

export function sheetAsk(name: string): SheetAsk {
  return {
    title: `Open ${name}’s movies?`,
    body: `${SHEET_ONE} The other names’ maps stay closed.`,
    yes: 'Open the map',
    no: 'Not now',
  };
}

/** A shut Movies button's label and tooltip: "One Movies map a game. You
 *  opened Joe Pantoliano’s." Without the name, which the page always has,
 *  it still says why. */
export function sheetLockedLabel(name: string | null): string {
  return name ? `One Movies map a game. You opened ${name}’s.` : 'One Movies map a game. You’ve opened yours.';
}

/** The sheet the page draws, of the one it has open: only the map the
 *  game says was chosen, and only while the game is on. So the page never
 *  asks the server for anyone else's, whatever it was left holding. */
export function shownSheet(game: Pick<DailyGame, 'phase' | 'sheet'>, open: string | null): string | null {
  return game.phase === 'play' && open != null && open === game.sheet ? open : null;
}

/** What becomes of the Movies map once the page has caught up with the
 *  game as the server has it (`now`, or null when it could not be had),
 *  after a choice was refused because one was made already, or a sheet
 *  the server would not show (`refused`). The map chosen opens, whoever's
 *  it is: the server's choice is the one that holds. With none to open,
 *  the reader is told the rule. The sheet refused is never opened again
 *  from here, so a page and a server that disagree cannot go round
 *  asking for it. Nothing to say once the game is over: the end takes
 *  over by itself. */
export function sheetAfter(
  now: Pick<DailyGame, 'phase' | 'sheet'> | null,
  refused: string | null = null,
): { open: string | null; say: boolean } {
  if (now && now.phase !== 'play') return { open: null, say: false };
  if (now?.sheet && now.sheet !== refused) return { open: now.sheet, say: false };
  return { open: null, say: true };
}

/** What a Movies press does, read against the game as it stands now
 *  rather than as the row was drawn (moviesButton): the map chosen opens;
 *  before one is chosen the page asks first (sheetAsk); a shut one does
 *  nothing. Nor does one that would ask while a move is on its way
 *  (`busy`), the choice of map among them, just as the page sends one
 *  move at a time: until a choice lands, every row still offers to
 *  choose, and a question put over it would be overtaken by the map that
 *  lands, which opened beneath it still asking about someone else's. The
 *  map chosen opens all the same, since opening it sends nothing and
 *  changes nothing in the game. */
export function moviesPress(
  game: Pick<DailyGame, 'phase' | 'sheet' | 'slots'>,
  person: Pick<DailyPerson, 'id' | 'name'>,
  busy: boolean,
): 'open' | 'ask' | 'none' {
  if (game.phase !== 'play') return 'none';
  const { state } = moviesButton(game, person);
  if (state === 'open') return 'open';
  return state === 'choose' && !busy ? 'ask' : 'none';
}

/** The question the page puts, of the name it was left asking about
 *  (`asking`): only while the game is on and no map is chosen. A choice
 *  that lands, from this tab or another, takes the question down, so the
 *  map chosen never opens under a question still asking about another
 *  name, nor answers Escape for it unseen. */
export function shownAsk<P>(game: Pick<DailyGame, 'phase' | 'sheet'>, asking: P | null): P | null {
  return game.phase === 'play' && !game.sheet ? asking : null;
}

/** Whether a choice of map sent from the game at `was` came back with
 *  the game moved on under it and no map chosen (`now`, the game the
 *  page has since): refused as stale, another tab having played on
 *  without choosing, which the server says nothing more about. The
 *  reader has answered, so the page sends the choice once more from
 *  there; overtaken again, it puts the question back up rather than drop
 *  the answer without a word. A choice refused for anything else leaves
 *  the game where it was, and has its reason said (refusalText). */
export function choiceOvertaken(
  was: Pick<DailyGame, 'seq'>,
  now: Pick<DailyGame, 'phase' | 'seq' | 'sheet'> | null,
): boolean {
  return now != null && now.phase === 'play' && now.seq !== was.seq && !now.sheet;
}

/** The years a bought decade or five years take in, inclusive, or null
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

/** The ratings a bought rating band takes in, or null with none bought. */
export function ratingSpan(facts: Pick<DailyFacts, 'rating'>): readonly [number, number] | null {
  return facts.rating == null ? null : (RATING_SPANS[facts.rating] ?? null);
}

/** Whether any range is bought that makes a card readable: the decade or
 *  the five years, the rating band, or the genre. Length never does. */
export function rangeBought(facts: DailyFacts): boolean {
  return facts.decade != null || facts.years != null || facts.rating != null || facts.genre != null;
}

/** What the movies the server sends depend on, besides whose they are:
 *  the ranges bought. The sheet asks again whenever this changes, so a
 *  range bought shows the titles inside it. */
export function rangesKey(facts: DailyFacts): string {
  return [facts.decade ?? '', facts.years ?? '', facts.rating ?? '', facts.genre?.join(',') ?? '-'].join('|');
}

/** The sheet's title: "Joe Pantoliano’s movies". */
export function sheetTitle(name: string): string {
  return `${name}’s movies`;
}

function moviesWord(n: number): string {
  return n === 1 ? 'movie' : 'movies';
}

/** The line under it: "13 movies · on Cinedikt" before any range, then
 *  "5 of 13 movies readable · on Cinedikt". */
export function sheetSub(readable: number, total: number, ranged: boolean): string {
  return ranged
    ? `${fmtN(readable)} of ${fmtN(total)} ${moviesWord(total)} readable · on Cinedikt`
    : `${fmtN(total)} ${moviesWord(total)} · on Cinedikt`;
}

/** The legend over the map: where titles show, and that today's movie is
 *  there. Once a range is bought it names the ranges, and says the
 *  answer is not marked. */
export function sheetLegend(facts: DailyFacts): string {
  const marks: string[] = [];
  if (facts.years != null) marks.push(yearsText(facts.years));
  else if (facts.decade != null) marks.push(decadeText(facts.decade));
  if (facts.rating != null) marks.push(`rated ${inSentence(ratingBandText(facts.rating))}`);
  if (facts.genre?.length) marks.push(facts.genre.join(', '));
  return rangeBought(facts)
    ? `Titles show inside your ranges: ${marks.join(' · ')}. Today’s movie is one of these cards, but it isn’t marked.`
    : 'Titles only show inside the ranges you buy: the decade, the years, a rating range or the genre. Today’s movie is one of these cards.';
}

/** The footer with nothing picked: how to guess from the map, once there
 *  are titles to guess, and before that where the ranges are sold. */
export function sheetHint(nextCost: number, ranged: boolean): string {
  return ranged
    ? `Tap a movie to guess it. A wrong guess costs ${fmtN(nextCost)}.`
    : 'Buy a range to read this map. The facts are under the card.';
}

/** The footer's line under a picked card's title: "1993 · IMDb 7.8". */
export function pickLine(movie: Pick<DailyReadableMovie, 'year' | 'rating'>): string {
  const rated = `IMDb ${movie.rating.toFixed(1)}`;
  return movie.year > 0 ? `${movie.year} · ${rated}` : rated;
}

/** A readable card's name for a screen reader: what it is, and whether
 *  it has been tried. */
export function sheetCardLabel(movie: Pick<DailyReadableMovie, 'title' | 'year' | 'rating'>, tried: boolean): string {
  return `${movie.title}, ${movie.year}, rated ${movie.rating.toFixed(1)}${tried ? ', already tried' : ''}`;
}

/** A blank card's: only its year, which is all the page knows of it, and
 *  what would read it. */
export function blankCardLabel(year: number, ranged: boolean): string {
  return ranged ? `A movie from ${year}, outside your ranges` : `A movie from ${year}. Buy a range to read it`;
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

/** The sheet opens scrolled so its first readable card sits this far
 *  from the map's top, a bought range already in view. */
export const SHEET_READ_TOP = 36;

// ---- the page ----

/** The game page's parts, top to bottom. While the game is on: the top
 *  block, the facts right under the card, where they cannot be missed,
 *  and then the cast. Once it is over: the top block, About the movie,
 *  the result, the leaderboard, "The cast" heading and the six names. The
 *  page draws them keyed in this order, so the order on the page is the
 *  order a screen reader hears, and the cast keeps its place in the tree
 *  as the rest arrive round it, last both times: a list moved in the tree
 *  would start its rows' transitions afresh, and the names the reader
 *  didn't see would arrive all at once. */
export type PageSection = 'top' | 'cast' | 'facts' | 'about' | 'result' | 'board' | 'castHead';
export function pageSections(done: boolean): PageSection[] {
  return done ? ['top', 'about', 'result', 'board', 'castHead', 'cast'] : ['top', 'facts', 'cast'];
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
 *  bought or the years before the decade, a purchase the points would not
 *  leave one over from, a movie already guessed, a second Movies map or
 *  one for a name not showing. Empty for a move worth sending. The
 *  page's buttons already hold back most of these; this is for the rest,
 *  and for a press that lands as the game changes under it. */
export function earlyRefusal(
  game: Pick<DailyGame, 'pts' | 'slots' | 'facts' | 'log' | 'sheet'>,
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
    case 'guess':
      return guessedIds(game).has(move.film) ? TOASTS.tried : '';
    case 'sheet':
      if (game.sheet) return refusalText('known', 'sheet');
      return shownSlots(game).some((s) => s.person.id === move.person) ? '' : refusalText('bad', 'sheet');
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
    move.kind === 'buy' ? move.fact : move.kind === 'guess' ? move.film : move.kind === 'sheet' ? move.person : '';
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
      if (kind === 'sheet') return 'You’ve already opened your Movies map.';
      return 'You already have that fact.';
    case 'bad':
      if (kind === 'buy') return 'Buy the decade first.';
      if (kind === 'movies' || kind === 'sheet') return 'That name isn’t showing yet.';
      return UNREACHABLE;
    // GET /movies for anyone but the person whose map was chosen, while
    // the game is on. The page catches up and opens the one chosen; this
    // is for when there is none to open.
    case 'sheet':
      return SHEET_ONE;
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

/** The end on the page: the card turns and the names not seen arrive,
 *  and this long after it the page moves on down to the result, its top
 *  RESULT_GAP_PX under the column's. Smoothly, or in one jump with
 *  stillness asked for, after the same pause, so the answer at the top
 *  is still seen first. */
export const RESULT_AFTER_MS = 1400;
export const RESULT_GAP_PX = 12;

/** Where the page's scroller goes to bring the result up: from where it
 *  stands, by how far the result's top is under the scroller's, less the
 *  gap. Never above the top. */
export function resultScroll(scrollTop: number, resultTop: number, mainTop: number): number {
  return Math.max(0, Math.round(scrollTop + resultTop - mainTop - RESULT_GAP_PX));
}

/** The score waits at nought while the card turns and the page reaches
 *  the result, then counts up from nought over SCORE_COUNT_MS, easing
 *  out, when the game ends on the page. A finished game opened later, or
 *  one ended with stillness asked for, just shows it. */
export const SCORE_COUNT_AFTER_MS = 1500;
export const SCORE_COUNT_MS = 1100;

/** The score `elapsed` ms into its count: ease-out cubic, landing on the
 *  score itself. */
export function countUp(score: number, elapsed: number): number {
  const p = Math.min(1, Math.max(0, elapsed / SCORE_COUNT_MS));
  return Math.round(score * (1 - Math.pow(1 - p, 3)));
}

/** The score as drawn `sinceEnd` ms after the game ended on the page:
 *  nought through the wait, then counting up. */
export function scoreAt(score: number, sinceEnd: number): number {
  return sinceEnd < SCORE_COUNT_AFTER_MS ? 0 : countUp(score, sinceEnd - SCORE_COUNT_AFTER_MS);
}

/** When a name appears, the next row is the only way to the one after,
 *  so once the new name has settled in the page makes sure that row is
 *  in sight above the guess bar, with this much to spare. */
export const NEXT_IN_VIEW_MS = 650;
export const NEXT_IN_VIEW_GAP = 34;

/** How far down the page must move for a row whose bottom is at
 *  `rowBottom` to sit NEXT_IN_VIEW_GAP clear of the scroller's bottom, at
 *  `mainBottom`: nothing when it already does. */
export function nextInView(rowBottom: number, mainBottom: number): number {
  return Math.max(0, Math.round(rowBottom + NEXT_IN_VIEW_GAP - mainBottom));
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

// ---- motion played from script ----
//
// Every loop and one-off below is played through the Web Animations API
// (motion.ts's animate) on elements the components hold refs to, started
// once as they are put down: a re-render never starts one again, and
// none starts for a reader who has asked for stillness. The stylesheet
// draws each element at rest, which is all such a reader sees: the card
// tilted, the dropping rows and the shines out of sight.

/** An animation as Element.animate takes it. */
export interface Motion {
  keyframes: Keyframe[];
  options: KeyframeAnimationOptions;
}

/** A loop that goes there and back, for ever, easing in and out. */
function swing(keyframes: Keyframe[], ms: number): Motion {
  return { keyframes, options: { duration: ms, direction: 'alternate', iterations: Infinity, easing: 'ease-in-out' } };
}

/** The title screen's parts rise in one after another, in the order
 *  they are drawn: the picture, the pill row, the heading and lead, the
 *  steps, the fine print, Play, then the players line. Each fades in from
 *  RISE_PX down over RISE_MS on the settle curve (motion.ts's EASE.settle),
 *  the first 80ms in and the rest 90ms apart, each held out of sight
 *  (`backwards`) until its turn. */
export const RISE_PX = 14;
export const RISE_MS = 560;
export const RISE_EASE = 'cubic-bezier(0.16, 1, 0.3, 1)';
export const RISE_FIRST_MS = 80;
export const RISE_STEP_MS = 90;

export function titleRise(i: number): Motion {
  return {
    keyframes: [
      { opacity: 0, translate: `0 ${RISE_PX}px` },
      { opacity: 1, translate: '0 0' },
    ],
    options: { duration: RISE_MS, delay: RISE_FIRST_MS + i * RISE_STEP_MS, easing: RISE_EASE, fill: 'backwards' },
  };
}

/** The title screen's picture as the game's own loop: a filled row drops
 *  into the next row, then another into the hidden one, over and over.
 *  Each falls from DROP_FROM_PX above, DROP_OVER_PX past its place, lands
 *  `land` of the way into the loop (DROP_LANDS: 24% and 52%), holds to
 *  84% and is gone by 94%, the rows empty again for the next round. Out
 *  of sight until a tenth of the loop before it lands. */
export const DROP_LOOP_MS = 6000;
export const DROP_AFTER_MS = 700;
export const DROP_FROM_PX = 28;
export const DROP_OVER_PX = 3;
export const DROP_LANDS = [0.24, 0.52] as const;
export const DROP_HOLD_TO = 0.84;
export const DROP_GONE_AT = 0.94;

export function dropKeyframes(land: number): Keyframe[] {
  const above = `0 -${DROP_FROM_PX}px`;
  // In hundredths, as the design gives them, with no float left over.
  const at = (offset: number) => Math.round(offset * 100) / 100;
  return [
    { opacity: 0, translate: above, offset: 0 },
    { opacity: 0, translate: above, offset: at(land - 0.1) },
    { opacity: 1, translate: `0 ${DROP_OVER_PX}px`, offset: land },
    { opacity: 1, translate: '0 0', offset: at(land + 0.04) },
    { opacity: 1, translate: '0 0', offset: DROP_HOLD_TO },
    { opacity: 0, translate: '0 0', offset: DROP_GONE_AT },
    { opacity: 0, translate: '0 0', offset: 1 },
  ];
}

/** Play's shine, as the start-screen banner's (DailyBanner.tsx's
 *  SHINE_KEYFRAMES): a band of light crosses in the first 28% of each
 *  3.6s and rests off to the side for the rest, starting 1.4s in. */
export const PLAY_SHINE_KEYFRAMES: Keyframe[] = [
  { translate: '-120% 0', offset: 0 },
  { translate: '320% 0', offset: 0.28 },
  { translate: '320% 0', offset: 1 },
];

/** The title screen's loops, by the part each plays on (DailyTitle.tsx
 *  marks each with data-anim):
 *  - float: the card rises 7px and turns from −4° to −2°, 3.2s each way;
 *  - bob: its "?" bobs 4px, 1.6s each way;
 *  - sheen: a band of white crosses the card every 4.2s, 900ms in;
 *  - pulse: the next row breathes a 5px ring of the accent, 1.1s each way;
 *  - drop1, drop2: the rows dropping in, 700ms in;
 *  - shine: Play's. */
export type TitleLoop = 'float' | 'bob' | 'sheen' | 'pulse' | 'drop1' | 'drop2' | 'shine';

export const TITLE_LOOPS: Record<TitleLoop, Motion> = {
  float: swing(
    [
      { translate: '0 0', rotate: '-4deg' },
      { translate: '0 -7px', rotate: '-2deg' },
    ],
    3200,
  ),
  bob: swing([{ translate: '0 0' }, { translate: '0 -4px' }], 1600),
  sheen: {
    keyframes: [
      { translate: '-160% 0', offset: 0 },
      { translate: '260% 0', offset: 0.3 },
      { translate: '260% 0', offset: 1 },
    ],
    options: { duration: 4200, delay: 900, iterations: Infinity, easing: 'ease-in-out' },
  },
  pulse: swing([{ boxShadow: `0 0 0 0 ${accentGlow(0)}` }, { boxShadow: `0 0 0 5px ${accentGlow(0.2)}` }], 1100),
  drop1: {
    keyframes: dropKeyframes(DROP_LANDS[0]),
    options: { duration: DROP_LOOP_MS, delay: DROP_AFTER_MS, iterations: Infinity, easing: 'ease-out' },
  },
  drop2: {
    keyframes: dropKeyframes(DROP_LANDS[1]),
    options: { duration: DROP_LOOP_MS, delay: DROP_AFTER_MS, iterations: Infinity, easing: 'ease-out' },
  },
  shine: {
    keyframes: PLAY_SHINE_KEYFRAMES,
    options: { duration: 3600, delay: 1400, iterations: Infinity, easing: 'ease-in-out' },
  },
};

/** The title screen's card glows 38px round in the lightened poster
 *  colour at 30% (posterGlow), still, set inline as --glow. */
export const TITLE_GLOW_ALPHA = 0.3;

/** The hidden card while the game is on: its glow breathes in the
 *  lightened poster colour, from a faint 16px at 12% to a 34px halo at
 *  42%, 2.6s each way. Null with no colour to glow in. */
export function cardGlow(colour: string | null | undefined): Motion | null {
  const faint = posterGlow(colour, 0.12);
  const full = posterGlow(colour, 0.42);
  if (!faint || !full) return null;
  return swing([{ boxShadow: `0 0 16px 1px ${faint}` }, { boxShadow: `0 0 34px 8px ${full}` }], 2600);
}

/** Its "?" bobs 4px, 1.8s each way, while the game is on. */
export const CARD_BOB: Motion = swing([{ translate: '0 0' }, { translate: '0 -4px' }], 1800);

/** A name appearing sends a ripple of that person's colour out from the
 *  card's edge, RIPPLE_PX out and fading as it goes, over RIPPLE_MS. */
export const RIPPLE_MS = 850;
export const RIPPLE_PX = 22;
export const RIPPLE_EASE = 'cubic-bezier(0.2, 0.8, 0.2, 1)';

export function cardRipple(colour: string): Motion {
  return {
    keyframes: [{ boxShadow: `0 0 0 0 ${colour}` }, { boxShadow: `0 0 0 ${RIPPLE_PX}px transparent` }],
    options: { duration: RIPPLE_MS, easing: RIPPLE_EASE },
  };
}

/** The facts panel's nudge (factNudge), once: three pulses of a ring of
 *  the accent spreading 12px and fading, 900ms each, over the panel's own
 *  accent edge. */
export const NUDGE_PULSE_MS = 900;
export const NUDGE_PULSES = 3;
export const NUDGE_PULSE_PX = 12;

/** The nudge's pulses, round a panel whose edge is `ring`: the theme's
 *  --acc as the page resolved it.
 *
 *  An animated box-shadow stands in for the stylesheet's whole
 *  box-shadow while it runs, so the panel's 1px edge has to be drawn
 *  again in every frame, or it would vanish for the 2.7s of the pulses.
 *  It is drawn in --acc, as the stylesheet then keeps it: in the light
 *  theme that is a much darker purple than the design's glow, which the
 *  edge would otherwise show until the last pulse and then snap from.
 *  Only the ring spreading out is the glow's own lavender. The colour is
 *  read and passed in rather than written as var(--acc), which not every
 *  engine resolves inside an animation's frames; with none to hand the
 *  edge is the glow's colour, as near as the dark theme's --acc. */
export function nudgePulse(ring: string): Motion {
  const edge = `inset 0 0 0 1px ${ring || accentGlow(1)}`;
  return {
    keyframes: [
      { boxShadow: `${edge}, 0 0 0 0 ${accentGlow(0.45)}` },
      { boxShadow: `${edge}, 0 0 0 ${NUDGE_PULSE_PX}px ${accentGlow(0)}` },
    ],
    options: { duration: NUDGE_PULSE_MS, iterations: NUDGE_PULSES, easing: 'ease-out' },
  };
}

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
  /** A leaderboard tab, 30px. */
  tab: '-7px 0',
} as const;

// ---- the screen ----

/** A landscape phone: under 520px tall and wider than tall. The guess bar
 *  becomes a 340px column on the right of the cast. */
export const LANDSCAPE_MAX_H = 520;
export function isLandscapePhone(width: number, height: number): boolean {
  return height < LANDSCAPE_MAX_H && width > height;
}

/** The window as the Daily lays itself out by it: a phone, under 640px
 *  wide as the app's screen classes have it (screen.ts), for the header's
 *  date, the four results and the Movies sheet from the bottom; a
 *  landscape phone, for the guess bar's column; and the visual viewport's
 *  height, for the phone sheet's, which the window's stands in for where
 *  there is none. Worked out afresh from the window whenever it changes
 *  (dailyScreen.ts), never kept from when the page loaded: a phone turned
 *  on its side, or a window dragged narrow, is laid out for what it is
 *  now. */
export interface LiveScreen {
  phone: boolean;
  land: boolean;
  viewH: number;
}

export function liveScreen(width: number, height: number, viewH?: number | null): LiveScreen {
  return {
    phone: screenOf(width, height).phone,
    land: isLandscapePhone(width, height),
    viewH: viewH != null && viewH > 0 ? viewH : height,
  };
}
