// Cinedikt Daily: "whose map is it?"
//
// One hidden movie a day. The board is its map with it taken off: every
// card shares an actor or director with it, and every card but three
// starts blank, in its year and at its rating. The reader spends points
// on cards and clues, and guesses; the points left are the score.
//
// The server owns the game. It checks every move against the recorded
// one, applies it once and sends the game back, and it never sends the
// answer, or a blank card's movie, until the game is over. So what is
// here is only what the page needs to draw the game it is handed: what
// a card says, where the guesses have narrowed the answer to, what the
// feed and the result say, and when things move. Everything is pure
// but the midnight watch, which is a timer, and all of it is checked
// without a DOM in daily.test.ts.

import type {
  DailyBoard,
  DailyBoardRow,
  DailyCard,
  DailyClue,
  DailyEntry,
  DailyFilm,
  DailyGame,
  DailyMove,
  DailyPerson,
  DailyTab,
  DailyToday,
} from './api';
import {
  AXIS_H,
  DEFAULT_SETTINGS,
  ratingText,
  xOf,
  type GridLayout,
  type GridPayload,
  type GridSettings,
  type Placed,
  type Row,
  type SpineTuple,
} from './grid';
import type { Theme } from './theme';

// ---- the rules, as far as the page shows them ----

/** Every game starts with this many points. The meter, the share card's
 *  bar and a week's cells are all drawn as a share of it (pointsShare),
 *  and the intro says it. */
export const DAILY_START = 1000;

/** What each clue costs. The server charges; these are what the buttons
 *  and the rules say. */
export const CLUE_COST: Record<DailyClue, number> = {
  director: 150,
  actor: 150,
  genres: 80,
  story: 300,
};

/** The first wrong guess, and how much more each one after it costs, so
 *  fishing for clues by guessing gets dear fast. The server charges, and
 *  sends what the next one costs with the game (`nextCost`); these are
 *  what the intro's rules say. */
export const WRONG_BASE = 100;
export const WRONG_STEP = 50;

/** What turning a card over costs: 20 to 80, in fives, by its rating.
 *  Better-rated movies tend to be better known, so they give more away.
 *
 *  Written exactly as the server works it out, in the same order, so the
 *  float arithmetic agrees to the last bit: 7.0 is 52.5 before rounding,
 *  and both sides make that 55. */
export function flipCost(rating: number): number {
  return Math.max(20, Math.min(80, Math.round((20 + (rating - 4.5) * 13) / 5) * 5));
}

/** How much of the starting points are left, in percent, from 0 to 100:
 *  how full the points meter, the share card's bar and a week's cell
 *  are. Multiplied before it is divided, so a score on a half step (650,
 *  say) is exactly 65 and rounds the same way everywhere. */
export function pointsShare(pts: number): number {
  return Math.max(0, Math.min(100, (pts * 100) / DAILY_START));
}

// ---- what is on the board ----

/** What a face-up card shows: its movie, or, for a close relative turned
 *  over, only how many people it shares. */
export type CardFace = { kind: 'film'; film: DailyFilm } | { kind: 'relative'; shared: number };

/** Every face-up card and what it shows. Before Play nothing is: the
 *  three starting movies turn over when the clock starts. Then the start,
 *  every card turned over, and every card guessed by name, a guess
 *  showing a close relative's movie after all. At the end, every card. */
export function facesOf(today: DailyToday, game: DailyGame | null): Map<string, CardFace> {
  const out = new Map<string, CardFace>();
  if (!game) return out;
  for (const s of today.start) out.set(s.card, { kind: 'film', film: s.film });
  for (const e of game.log) {
    if (e.type === 'flip') {
      out.set(e.card, e.film ? { kind: 'film', film: e.film } : { kind: 'relative', shared: e.relative.shared });
    }
  }
  // After the flips, so a close relative guessed by name shows normally
  // whichever came first.
  for (const e of game.log) {
    if (e.type === 'guess' && e.card) out.set(e.card, { kind: 'film', film: e.film });
  }
  for (const c of game.end?.cards ?? []) out.set(c.id, { kind: 'film', film: c.film });
  return out;
}

/** The cards face up. */
export function revealedOf(today: DailyToday, game: DailyGame | null): Set<string> {
  return new Set(facesOf(today, game).keys());
}

/** The people the page may draw: those bought and found while the game
 *  is on, and everyone once it is over. */
export function peopleOf(game: DailyGame | null): DailyPerson[] {
  if (!game) return [];
  return game.end ? game.end.people : game.known;
}

/** Who of `people` is on each card, in slot order, which is the order a
 *  card's marks and dots are drawn in: directors, then billing. */
export function peopleByCard(people: readonly DailyPerson[]): Map<string, DailyPerson[]> {
  const out = new Map<string, DailyPerson[]>();
  for (const p of [...people].sort((a, b) => a.slot - b.slot)) {
    for (const card of p.cards) {
      const list = out.get(card);
      if (list) list.push(p);
      else out.set(card, [p]);
    }
  }
  return out;
}

/** Each person's place in HUES, for personVars: their slot, wrapped at
 *  sixteen. The Daily colours people by the puzzle's own order rather
 *  than by the visit's, so everyone playing sees Keanu Reeves in the same
 *  colour. */
export function hueSlots(people: Iterable<DailyPerson>): Map<string, number> {
  const out = new Map<string, number>();
  for (const p of people) out.set(p.id, p.slot % 16);
  return out;
}

/** Every person the game has named anywhere: bought, found, and in the
 *  feed's entries. The feed's faces need a colour and a code for each. */
export function everyoneNamed(game: DailyGame | null): DailyPerson[] {
  if (!game) return [];
  const byId = new Map<string, DailyPerson>();
  const add = (p: DailyPerson) => {
    if (!byId.has(p.id)) byId.set(p.id, p);
  };
  for (const p of peopleOf(game)) add(p);
  for (const e of game.log) {
    if (e.type === 'person') e.people.forEach(add);
    if (e.type === 'guess') e.shared.forEach(add);
  }
  return [...byId.values()].sort((a, b) => a.slot - b.slot);
}

/** The movies guessed wrong so far, by IMDb id: the search marks them
 *  "Guessed". */
export function guessedIds(game: DailyGame | null): Set<string> {
  const out = new Set<string>();
  for (const e of game?.log ?? []) if (e.type === 'guess') out.add(e.film.id);
  return out;
}

/** Whether a clue of this kind has been bought: genres and the opening
 *  line are bought once. */
export function bought(game: DailyGame | null, kind: 'genres' | 'story'): boolean {
  return !!game?.log.some((e) => e.type === kind);
}

/** The card most worth putting in front of a reader coming back to a
 *  game: the last one they turned over, or the starting three. */
export function resumeCards(today: DailyToday, game: DailyGame): string[] {
  for (let i = game.log.length - 1; i >= 0; i--) {
    const e = game.log[i];
    if (e.type === 'flip') return [e.card];
  }
  return today.start.map((s) => s.card);
}

// ---- the layout ----

/** The board's settings. Unrated movies never reach the board, so there
 *  is no unrated column to keep a place for; and the answer's year is not
 *  lit, because lighting it would say what it is. */
export const DAILY_SETTINGS: GridSettings = {
  ...DEFAULT_SETTINGS,
  showUnrated: false,
  highlightYear: false,
};

/** What the board is laid out from, for grid.ts's layoutGrid.
 *
 *  The layout wants a searched film, and the Daily has none it can show,
 *  so while the game is on it is handed a stand-in that is on no card and
 *  from year 0: no row is lit, no row is kept for it, and nothing the
 *  layout reads from it says anything. Once the game is over the real
 *  answer takes its place, joins the cards in its own year, and its row
 *  is lit as a searched film's is. */
export function boardPayload(
  today: DailyToday,
  game: DailyGame | null,
): { payload: GridPayload; settings: GridSettings } {
  const films: SpineTuple[] = today.cards.map((c: DailyCard) => [c.id, c.year, c.rating, c.md]);
  const answer = game?.end?.answer;
  if (!answer) {
    return {
      payload: {
        anchor: { id: '', year: 0, rating: null, md: 0, isAnchor: true, title: '', people: [] },
        people: [],
        films,
      },
      settings: DAILY_SETTINGS,
    };
  }
  return {
    payload: {
      anchor: {
        id: answer.id,
        year: answer.year,
        rating: answer.rating,
        md: answer.md,
        isAnchor: true,
        title: answer.title,
        people: [],
      },
      people: [],
      films: [...films, [answer.id, answer.year, answer.rating, answer.md]],
    },
    settings: { ...DAILY_SETTINGS, highlightYear: true },
  };
}

/** Where the last row ends: the bottom of the rows themselves, without
 *  the room the map keeps under them. */
export function rowsBottom(layout: Pick<GridLayout, 'rows'>): number {
  const last = layout.rows[layout.rows.length - 1];
  return last ? last.top + last.height : AXIS_H;
}

/** How far the plot runs on past the right-hand panel, so a card under
 *  it can be scrolled out into view: the panel's 380 and its 16 gutter. */
export const PANEL_CLEAR_W = 396;

/** The plot's size. On a desktop or tablet it runs on past the panel; on
 *  a phone it runs on under the bottom sheet instead, so the last rows
 *  can be scrolled up above it. */
export function plotSize(layout: GridLayout, phone: boolean): { w: number; h: number } {
  return {
    w: layout.plotW + (phone ? 0 : PANEL_CLEAR_W),
    h: rowsBottom(layout) + (phone ? 360 : 140),
  };
}

/** Where each year that has just opened grows out of: the top its next
 *  row had in the layout before, or that layout's bottom. Only the
 *  answer's own year can open, and only at the end, when the answer joins
 *  the board — so its row comes out of the seam between its neighbours
 *  and everything below it slides down to make room. */
export function seamsFor(was: readonly Row[], now: readonly Row[]): Map<number, number> {
  const before = new Set(was.map((r) => r.year));
  const bottom = rowsBottom({ rows: was as Row[] });
  const out = new Map<number, number>();
  for (const r of now) {
    if (before.has(r.year)) continue;
    const next = was.find((w) => w.year > r.year);
    out.set(r.year, next ? next.top : bottom);
  }
  return out;
}

// ---- what wrong guesses rule out ----

/** Where the answer can still be: years from yLo to yHi and ratings from
 *  rLo to rHi, all inclusive. */
export interface Bounds {
  yLo: number;
  yHi: number;
  rLo: number;
  rHi: number;
}

/** Nothing ruled out yet. */
export const OPEN_BOUNDS: Bounds = { yLo: 0, yHi: 9999, rLo: 0, rHi: 10 };

/** The bounds after one wrong guess. Ratings move in tenths, so a guess
 *  rated 7.3 that the answer beats leaves "7.4+".
 *
 *  The same year, or the same rating, pins the bounds to it outright
 *  rather than meeting them: that is the prototype's rule, and a guess
 *  can only ever say the same year inside bounds that already allow it. */
export function narrowed(
  b: Bounds,
  g: Extract<DailyEntry, { type: 'guess' }>,
): Bounds {
  const out = { ...b };
  const y = g.film.year;
  if (g.year === 'newer') out.yLo = Math.max(out.yLo, y + 1);
  else if (g.year === 'older') out.yHi = Math.min(out.yHi, y - 1);
  else if (g.year === 'same') {
    out.yLo = y;
    out.yHi = y;
  }
  const r = g.film.rating;
  if (r != null) {
    if (g.rating === 'higher') out.rLo = Math.max(out.rLo, Math.round((r + 0.1) * 10) / 10);
    else if (g.rating === 'lower') out.rHi = Math.min(out.rHi, Math.round((r - 0.1) * 10) / 10);
    else if (g.rating === 'same') {
      out.rLo = r;
      out.rHi = r;
    }
  }
  return out;
}

/** The bounds the guesses so far leave, or null before any guess. Once
 *  the game is over there are none: the answer is on the board. */
export function boundsOf(game: DailyGame | null): Bounds | null {
  if (!game || game.phase === 'done') return null;
  let b: Bounds | null = null;
  for (const e of game.log) if (e.type === 'guess') b = narrowed(b ?? OPEN_BOUNDS, e);
  return b;
}

/** Whether a card could still be the answer's neighbour in year and
 *  rating; a card outside fades. The hair either side keeps a rating of
 *  exactly the bound inside it, whatever the floats did. */
export function inBounds(card: { year: number; rating: number | null }, b: Bounds | null): boolean {
  if (!b) return true;
  if (card.year < b.yLo || card.year > b.yHi) return false;
  return card.rating == null || (card.rating >= b.rLo - 1e-9 && card.rating <= b.rHi + 1e-9);
}

/** Whether a year's band and label fade. */
export function yearRuledOut(year: number, b: Bounds | null): boolean {
  return !!b && (year < b.yLo || year > b.yHi);
}

/** How opaque a card is, and a year that is ruled out. */
export const OUT_CARD_OPACITY = 0.28;
export const OUT_YEAR_OPACITY = 0.35;

/** A dashed line down the board at a rating bound, and its pill in the
 *  rating strip. */
export interface RatingLine {
  x: number;
  /** Where the pill starts. A ceiling's pill sits left of its line. */
  labelLeft: number;
  before: boolean;
  label: string;
}

/** A dashed line across the board at a year bound, and its pill: at the
 *  top of the years left, or at the bottom. */
export interface YearLine {
  kind: 'from' | 'until';
  y: number;
  labelTop: number;
  /** From the year rail's left edge, which stays at the screen's however
   *  far the map is panned: just past the rail. */
  labelLeft: number;
  label: string;
}

/** What a rating floor says: "7.4+". */
export function floorLabel(r: number): string {
  return `${r.toFixed(1)}+`;
}

/** What a rating ceiling says: "Up to 6.9". */
export function ceilingLabel(r: number): string {
  return `Up to ${r.toFixed(1)}`;
}

/** What the year line at the top of what is left says: "1995 or later",
 *  or just "1995" once the year is pinned. */
export function fromLabel(b: Bounds): string {
  return b.yLo === b.yHi ? `${b.yLo}` : `${b.yLo} or later`;
}

/** What the year line at the bottom says: "1994 or earlier". */
export function untilLabel(b: Bounds): string {
  return `${b.yHi} or earlier`;
}

/** The dashed lines the bounds draw. A year line is drawn only where it
 *  rules a row out; when the year is pinned the top line names it alone,
 *  and the bottom one, which would sit just under it saying the same
 *  thing, is not drawn. */
export function boundLines(
  b: Bounds | null,
  layout: GridLayout,
): { ratings: RatingLine[]; years: YearLine[] } {
  const ratings: RatingLine[] = [];
  const years: YearLine[] = [];
  if (!b) return { ratings, years };
  const m = layout.metrics;
  if (b.rLo > 0) {
    const x = Math.round(xOf(b.rLo, m));
    ratings.push({ x, labelLeft: x + 4, before: false, label: floorLabel(b.rLo) });
  }
  if (b.rHi < 10) {
    const x = Math.round(xOf(b.rHi, m));
    ratings.push({ x, labelLeft: x - 4, before: true, label: ceilingLabel(b.rHi) });
  }
  const rows = layout.rows;
  const labelLeft = m.railW + 10;
  if (rows.length && b.yLo > 0 && b.yLo > rows[0].year) {
    const r = rows.find((w) => w.year >= b.yLo);
    const y = r ? r.top : rowsBottom(layout);
    years.push({ kind: 'from', y, labelTop: y + 6, labelLeft, label: fromLabel(b) });
  }
  if (rows.length && b.yHi < 9999 && b.yHi < rows[rows.length - 1].year && b.yLo !== b.yHi) {
    const r = [...rows].reverse().find((w) => w.year <= b.yHi);
    const y = r ? r.top + r.height : 0;
    years.push({ kind: 'until', y, labelTop: y - 24, labelLeft, label: untilLabel(b) });
  }
  return { ratings, years };
}

// ---- what a card says ----

/** A card's accessible name. A close relative says how near it is and
 *  never its title; a blank card says where it is and what it costs. */
export function cardLabel(card: DailyCard, face: CardFace | undefined): string {
  const rt = card.rating.toFixed(1);
  if (!face) return `Hidden movie from ${card.year}. Turn it over for ${flipCost(card.rating)} points`;
  if (face.kind === 'relative') {
    return `A close relative of today’s movie, from ${card.year}, rated ${rt}. It shares ${face.shared} people with it`;
  }
  return `${face.film.title}, ${card.year}, rated ${rt}`;
}

/** The answer's card, at the end. */
export function answerLabel(answer: DailyFilm): string {
  return `Today’s movie: ${answer.title}, ${answer.year}, rated ${ratingText(answer.rating)}`;
}

/** The most dots a blank card draws. */
export const MAX_DOTS = 5;

// ---- the feed ----

/** A chip in the feed: a movie on the board, which takes the reader to
 *  its card. A close relative's chip names no movie. */
export interface FeedChip {
  card: string;
  title: string;
  film: DailyFilm | null;
  aria: string;
}

/** How an entry is coloured. The latest one, while the game is on, is
 *  washed in the accent; a wrong guess's label is the down colour; the
 *  end is the accent for a win and plain for the rest. */
export type FeedTone = 'plain' | 'miss' | 'win' | 'end';

/** One entry as the panel draws it. */
export interface FeedView {
  key: string;
  label: string;
  tone: FeedTone;
  /** The latest entry of a game still being played. */
  now: boolean;
  /** "−150", or empty. */
  cost: string;
  text: string;
  faces: DailyPerson[];
  quote: string;
  more: string;
  chips: FeedChip[];
}

/** "1999", or nothing for a movie whose year the catalog does not have. */
function titled(f: DailyFilm): string {
  return f.year > 0 ? `${f.title} (${f.year})` : f.title;
}

function chipFor(card: string, face: CardFace | undefined): FeedChip | null {
  if (!face) return null;
  if (face.kind === 'relative') {
    return { card, title: 'Close relative', film: null, aria: 'Show the close relative on the map' };
  }
  return { card, title: face.film.title, film: face.film, aria: `Show ${face.film.title} on the map` };
}

/** What a wrong guess says about where the answer is: "Today’s movie is
 *  older and rated higher." Either half can be missing, for a guess with
 *  no year or no rating to compare, and with both missing it says
 *  nothing. */
export function directionText(
  year: Extract<DailyEntry, { type: 'guess' }>['year'],
  rating: Extract<DailyEntry, { type: 'guess' }>['rating'],
): string {
  const yt = year === 'same' ? 'from the same year' : (year ?? '');
  const rt =
    rating === 'same'
      ? 'has the same rating'
      : rating === 'higher'
        ? 'rated higher'
        : rating === 'lower'
          ? 'rated lower'
          : '';
  if (!yt && !rt) return '';
  return `Today’s movie is ${[yt, rt].filter(Boolean).join(' and ')}.`;
}

/** How many of the board's cards a set of people are on, between them. */
export function cardsMarked(people: readonly DailyPerson[]): number {
  return new Set(people.flatMap((p) => p.cards)).size;
}

/** What a bought person's entry adds under their faces. */
export function markedText(n: number): string {
  if (n === 0) return 'None of their movies are on the map.';
  if (n === 1) return '1 of their movies is marked on the map.';
  return `${n} of their movies are marked on the map.`;
}

/** The feed: one entry per line of the game's record, in order. */
export function feedOf(today: DailyToday, game: DailyGame | null): FeedView[] {
  if (!game) return [];
  const faces = facesOf(today, game);
  const answer = game.end?.answer;
  const last = game.log.length - 1;
  const playing = game.phase === 'play';
  return game.log.map((e, i): FeedView => {
    const o: FeedView = {
      key: `${i}:${e.type}`,
      label: '',
      tone: 'plain',
      now: playing && i === last,
      cost: 'cost' in e && e.cost ? `−${e.cost}` : '',
      text: '',
      faces: [],
      quote: '',
      more: '',
      chips: [],
    };
    switch (e.type) {
      case 'start':
        return {
          ...o,
          label: 'Start',
          text: 'Three movies from its map are showing.',
          chips: today.start.map((s) => chipFor(s.card, faces.get(s.card) ?? { kind: 'film', film: s.film })!),
        };
      case 'flip': {
        const face = faces.get(e.card);
        const chip = chipFor(e.card, face);
        if (face?.kind === 'relative') {
          return {
            ...o,
            label: 'Turned over',
            text: `A close relative: it shares ${face.shared} people with today’s movie, so its title stays hidden.`,
            chips: chip ? [chip] : [],
          };
        }
        return { ...o, label: 'Turned over', chips: chip ? [chip] : [] };
      }
      case 'person':
        return {
          ...o,
          label: e.role === 'director' ? (e.people.length > 1 ? 'Directors' : 'Director') : 'Actor',
          text: e.role === 'director' ? 'It was directed by:' : 'It stars:',
          faces: e.people,
          more: markedText(cardsMarked(e.people)),
        };
      case 'genres':
        return { ...o, label: 'Genres', text: e.genres.join(', ') };
      case 'story':
        return { ...o, label: 'How it starts', quote: e.opening };
      case 'guess':
        return {
          ...o,
          label: 'Not it',
          tone: 'miss',
          text: e.shared.length
            ? `${titled(e.film)} is linked to it through:`
            : `${titled(e.film)} shares no one with today’s movie.`,
          faces: e.shared,
          more: directionText(e.year, e.rating),
        };
      case 'win':
        return {
          ...o,
          now: false,
          label: 'Got it',
          tone: 'win',
          text: answer ? `${titled(answer)} is today’s movie.` : '',
        };
      case 'gaveup':
      case 'out':
        return {
          ...o,
          now: false,
          label: e.type === 'gaveup' ? 'Answer shown' : 'Out of points',
          tone: 'end',
          text: answer ? `Today’s movie was ${titled(answer)}.` : '',
        };
    }
  });
}

// ---- the panel ----

/** One of the four clue buttons. */
export interface ClueButton {
  clue: DailyClue;
  label: string;
  cost: number;
  /** "Seen", "Known", or the price. */
  tag: string;
  /** The tag is a price rather than a note. */
  priced: boolean;
  off: boolean;
  aria: string;
}

/** The clue buttons, in order. Director buys every director not yet
 *  known, Actor the next of the cast in billing order, so each says
 *  "Known" once there is nobody left for it; genres and the opening line
 *  say "Seen" once bought. "Directors" when the answer has more than one,
 *  which the server says up front. */
export function clueButtons(today: DailyToday, game: DailyGame | null): ClueButton[] {
  const known = game?.known ?? [];
  const playing = game?.phase === 'play';
  const pts = game?.pts ?? 0;
  const dirsKnown = known.filter((p) => p.role === 'director').length;
  const castKnown = known.filter((p) => p.role === 'cast').length;
  const make = (clue: DailyClue, label: string, have: boolean, none: boolean): ClueButton => {
    const cost = CLUE_COST[clue];
    return {
      clue,
      label,
      cost,
      tag: have ? 'Seen' : none ? 'Known' : String(cost),
      priced: !have && !none,
      off: !playing || have || none || pts < cost,
      aria: have ? `${label}: seen` : none ? `${label}: already known` : `${label} for ${cost} points`,
    };
  };
  return [
    make('director', today.clues.directors > 1 ? 'Directors' : 'Director', false, dirsKnown >= today.clues.directors),
    make('actor', 'Actor', false, castKnown >= today.clues.cast),
    make('genres', 'Genres', bought(game, 'genres'), false),
    make('story', 'How it starts', bought(game, 'story'), false),
  ];
}

/** The line under the guess field. */
export function hintText(touch: boolean, nextCost: number): string {
  return `${touch ? 'Tap' : 'Click'} a blank card to turn it over. Your next wrong guess costs ${nextCost}.`;
}

/** The meter's colour turns from the accent to the down colour below
 *  this, so a reader near the end can see it coming. */
export const LOW_POINTS = 250;

/** What the guess field's search last answered: the words it was asked,
 *  the movies it found for them, or that it never answered. */
export interface GuessFound<T> {
  q: string;
  hits: readonly T[];
  failed: boolean;
}

/** What the guess list shows for what is typed now.
 *
 *  `rows` are what can be chosen, and only an answer to exactly what is
 *  typed gives any. The search waits for a pause and then the network,
 *  and in that time the reader may have typed on: the last answer's rows
 *  belong to words they have typed past, and Enter on the top one would
 *  fill the field with a movie they never meant, a second Enter making
 *  it a wrong guess that costs points and cannot be taken back. So until
 *  the new answer comes, the old rows are only `held`: drawn faded, so
 *  the list does not collapse at every key, and not to be chosen.
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

// ---- the result ----

/** "a, b and c", without the Oxford comma. */
export function andList(items: readonly string[]): string {
  if (items.length < 2) return items.join('');
  return `${items.slice(0, -1).join(', ')} and ${items[items.length - 1]}`;
}

function plural(n: number, one: string, many: string): string {
  return `${fmtN(n)} ${n === 1 ? one : many}`;
}

/** What a game spent its points on, for the result's line. */
export function usedList(log: readonly DailyEntry[]): string[] {
  const flips = log.filter((e) => e.type === 'flip').length;
  const people = log.reduce((n, e) => n + (e.type === 'person' ? e.people.length : 0), 0);
  const wrong = log.filter((e) => e.type === 'guess').length;
  const out: string[] = [];
  if (flips) out.push(plural(flips, 'card', 'cards'));
  if (people) out.push(plural(people, 'person', 'people'));
  if (log.some((e) => e.type === 'genres')) out.push('the genres');
  if (log.some((e) => e.type === 'story')) out.push('the opening line');
  if (wrong) out.push(`${wrong} wrong ${wrong === 1 ? 'guess' : 'guesses'}`);
  return out;
}

/** A count as the page writes it: "61,240". */
export function fmtN(n: number): string {
  return n.toLocaleString('en-GB');
}

/** The result's heading. */
export function resultTitle(game: DailyGame): string {
  return game.won ? `${fmtN(game.pts)} points` : 'No points today';
}

/** The line under it. */
export function resultSub(game: DailyGame): string {
  if (!game.won) return game.gaveUp ? 'You asked for the answer.' : 'Your points ran out.';
  const clock = mmss(game.secs ?? 0);
  const used = usedList(game.log);
  return used.length
    ? `Solved in ${clock}. You used ${andList(used)}.`
    : `Solved in ${clock}, without spending a point.`;
}

/** The streak once today's game is part of it. The server works it out
 *  when the page loads; a game finished since then, with points, adds
 *  today to the run that ended yesterday. */
export function streakAfter(
  streak: DailyToday['streak'],
  game: DailyGame | null,
): DailyToday['streak'] {
  if (game?.phase === 'done' && game.won && game.pts > 0 && streak.now === 0) {
    return { now: streak.before + 1, before: 0 };
  }
  return streak;
}

/** The streak the intro shows: today's run once today is over, or the
 *  run coming into today until then. Zero shows nothing. */
export function introStreak(streak: DailyToday['streak'], game: DailyGame | null): number {
  const s = streakAfter(streak, game);
  return game?.phase === 'done' ? s.now : s.before;
}

/** One of the result's two numbers. `value` is null while the board it
 *  comes from is on its way. */
export interface StatView {
  value: number | null;
  suffix: string;
  caption: string;
}

/** The result's numbers: how the reader did against everyone else today,
 *  and their streak. */
export function statsOf(
  game: DailyGame,
  board: Pick<DailyBoard, 'beat' | 'solved'> | null,
  streak: DailyToday['streak'],
): StatView[] {
  const first: StatView = game.won
    ? { value: board ? (board.beat ?? 0) : null, suffix: '%', caption: 'of players scored less' }
    : { value: board ? (board.solved ?? 0) : null, suffix: '%', caption: 'of players got it today' };
  const s = streakAfter(streak, game);
  const second: StatView = s.now
    ? { value: s.now, suffix: '', caption: s.now === 1 ? 'day in a row' : 'days in a row' }
    : {
        value: 0,
        suffix: '',
        caption: s.before ? `days in a row. Your ${s.before}-day streak ended.` : 'days in a row',
      };
  return [first, second];
}

/** The copied text's bar, as the share card draws it: ten blocks, filled
 *  to the nearest tenth of the starting points. */
export function shareBar(pts: number): string {
  const full = Math.round(pointsShare(pts) / 10);
  return '▰'.repeat(full) + '▱'.repeat(10 - full);
}

/** The share card's line. */
export function shareLine(game: DailyGame): string {
  return game.won ? `${fmtN(game.pts)} points · ${mmss(game.secs ?? 0)}` : 'Missed';
}

/** What Copy puts on the clipboard: the number, the score, the bar, and
 *  where to play, so a pasted result is also the way in. */
export function shareText(no: number, game: DailyGame, origin: string): string {
  const head = `Cinedikt Daily No. ${no}`;
  const middle = game.won ? `${fmtN(game.pts)} points in ${mmss(game.secs ?? 0)}` : 'Missed it';
  return `${head}\n${middle}\n${shareBar(game.pts)}\n${origin}/daily`;
}

// ---- the leaderboard ----

/** The week's days, as the column heads and the cells' tips name them. */
export const DAY_LETTERS = ['M', 'T', 'W', 'T', 'F', 'S', 'S'] as const;
export const DAY_NAMES = [
  'Monday',
  'Tuesday',
  'Wednesday',
  'Thursday',
  'Friday',
  'Saturday',
  'Sunday',
] as const;

/** A week's cell for one day. */
export interface DayCell {
  /** How full it is, in percent, 0 for an empty cell. */
  h: number;
  missed: boolean;
  played: boolean;
  tip: string;
}

/** One day of a week row: not played, missed (no points), or filled to
 *  its points, never less than a sliver so a poor day still shows. */
export function dayCell(pts: number | null | undefined, day: number): DayCell {
  const name = DAY_NAMES[day];
  if (pts == null) return { h: 0, missed: false, played: false, tip: `${name}: not played` };
  if (pts <= 0) return { h: 0, missed: true, played: true, tip: `${name}: missed` };
  const h = Math.max(8, Math.round(pointsShare(pts)));
  return { h, missed: false, played: true, tip: `${name}: ${fmtN(pts)} points` };
}

/** Initials for a leaderboard avatar: "TK" for Trinity Kimble. */
export function initialsOf(name: string): string {
  return name
    .split(' ')
    .filter(Boolean)
    .map((w) => w[0])
    .join('');
}

/** A row of the leaderboard as it is drawn. */
export type BoardLine =
  | { gap: true; key: string }
  | {
      gap: false;
      key: string;
      rank: string;
      you: boolean;
      name: string;
      sub: string;
      code: string;
      hue: number;
      value: string;
      missed: boolean;
      cells: DayCell[] | null;
    };

/** The leaderboard's rows. The reader's own row says "You", with their
 *  name under it, or that they are not on the board yet; a day row says
 *  "Missed" for no points. The week's rows carry a cell for each day so
 *  far, Monday first, `days` long. */
export function boardLines(
  board: DailyBoard,
  me: string,
  days: number,
): BoardLine[] {
  const listed = board.you?.listed ?? true;
  return board.rows.map((r, i): BoardLine => {
    if ('gap' in r) return { gap: true, key: `gap:${i}` };
    const row = r as DailyBoardRow;
    const week = board.tab === 'week';
    const missed = !week && row.pts <= 0;
    return {
      gap: false,
      key: `${row.rank}:${row.you ? 'you' : row.name}`,
      rank: fmtN(row.rank),
      you: row.you,
      name: row.you ? 'You' : row.name,
      sub: row.you ? (listed ? me : 'Not on the board yet') : '',
      code: row.you ? 'You' : initialsOf(row.name),
      hue: row.hue,
      value: week
        ? fmtN(row.pts)
        : missed
          ? 'Missed'
          : row.secs != null
            ? `${fmtN(row.pts)} · ${mmss(row.secs)}`
            : fmtN(row.pts),
      missed,
      cells: week ? Array.from({ length: days }, (_, d) => dayCell(row.days?.[d], d)) : null,
    };
  });
}

/** How wide the rank column is, so every rank lines up on its right:
 *  7.4px a character, as the handoff sets it. */
export function rankWidth(lines: readonly BoardLine[]): number {
  let most = 0;
  for (const l of lines) if (!l.gap) most = Math.max(most, l.rank.length);
  return Math.ceil(most * 7.4);
}

/** The leaderboards fetched so far, by tab, with 'failed' for one that
 *  could not be had. */
export type BoardsSeen<T> = Partial<Record<DailyTab, T | 'failed'>>;

/** The boards a tab needs: its own, and today's whichever tab is
 *  showing, since the result's first number comes from today's. */
export function boardsWanted(tab: DailyTab): DailyTab[] {
  return tab === 'week' ? ['today', 'week'] : ['today'];
}

/** The same boards with any of `tabs` that failed forgotten, so they are
 *  asked for again: the panel asks for every tab it wants and does not
 *  have, and a failure counts as having it until it is dropped. Dropped
 *  when the reader picks a tab and when they press "Try again". The same
 *  object when nothing failed, so nothing is drawn again for it. */
export function dropFailed<T>(boards: BoardsSeen<T>, tabs: readonly DailyTab[]): BoardsSeen<T> {
  if (!tabs.some((t) => boards[t] === 'failed')) return boards;
  const out = { ...boards };
  for (const t of tabs) if (out[t] === 'failed') delete out[t];
  return out;
}

/** The line under the leaderboard. */
export function boardNote(tab: DailyTab, total: number, me: string, listed: boolean): string {
  if (tab === 'week') {
    return `${plural(total, 'player', 'players')} this week. Each day adds the points you kept. The week starts on Monday.`;
  }
  const who = listed ? `You show as ${me}.` : `You’ll show as ${me} once you’ve played on a few days.`;
  return `${plural(total, 'player', 'players')} so far today. ${who}`;
}

/** A leaderboard avatar's colours: the person's hue as a fill, and what
 *  is written on it. Set inline, as every colour worked out in script is,
 *  because the build rewrites an oklch() it finds in a stylesheet. */
export function avatarColours(hue: number, theme: Theme): { fill: string; ink: string } {
  return theme === 'light'
    ? { fill: `oklch(0.55 0.15 ${hue})`, ink: '#ffffff' }
    : { fill: `oklch(0.78 0.12 ${hue})`, ink: `oklch(0.2 0.03 ${hue})` };
}

// ---- the intro ----

/** The intro's Play button: Play before the game, and the way back to it
 *  when the same screen is opened as the rules. */
export function introButton(no: number, game: DailyGame | null): string {
  if (!game) return `Play No. ${no}`;
  return game.phase === 'done' ? 'Back to your result' : 'Back to the game';
}

/** The count under the Play button: the number, which is set apart, and
 *  the words after it. Null before anyone has played: every puzzle opens
 *  at nought, at the first midnight in the world and all through launch
 *  day, and "0 people have played today" would say the game is empty
 *  rather than new, as the opening screen's banner leaves its count out
 *  for the same reason. */
export function playedText(n: number): { count: string; rest: string } | null {
  if (n <= 0) return null;
  return { count: fmtN(n), rest: n === 1 ? ' person has played today.' : ' people have played today.' };
}

// ---- time ----

/** "3:07". */
export function mmss(secs: number): string {
  const t = Math.max(0, Math.round(secs));
  return `${Math.floor(t / 60)}:${String(t % 60).padStart(2, '0')}`;
}

/** "11:47:03": hours, minutes and seconds left, never below zero. */
export function countdown(ms: number): string {
  const left = Math.max(0, Math.floor(ms / 1000));
  const h = Math.floor(left / 3600);
  const m = Math.floor(left / 60) % 60;
  return `${h}:${String(m).padStart(2, '0')}:${String(left % 60).padStart(2, '0')}`;
}

/** How far the server's clock is ahead of this one, in ms, from the time
 *  it gave and the moment it arrived. The countdown and the game clock
 *  run on the server's time: a reader whose clock is wrong still sees the
 *  next map arrive at their own midnight, which the server worked out.
 *  The time it gave was stamped before the answer set off, so this errs
 *  behind the server, and what waits on it comes late rather than
 *  early. */
export function clockOffset(serverNow: string, localNow: number): number {
  const t = Date.parse(serverNow);
  return Number.isFinite(t) ? t - localNow : 0;
}

/** Seconds since an RFC 3339 time, on the server's clock. */
export function secondsSince(at: string, localNow: number, offset: number): number {
  const t = Date.parse(at);
  return Number.isFinite(t) ? Math.max(0, (localNow + offset - t) / 1000) : 0;
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
 *  screen's banner each ask for the new map with it, the moment the old
 *  one ends. A `next` that cannot be read, or that has already gone by
 *  as the watch starts, is not waited for: the server said it was ahead,
 *  so asking again would only bring back the same answer, again and
 *  again. */
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

/** "Thursday 8 October", as en-GB writes it, in UTC: the calendar date
 *  the puzzle belongs to, which is the reader's own date, written as it
 *  stands whatever zone the page is in. Built by hand from the UTC parts
 *  rather than through Intl, whose en-GB output has moved a comma about
 *  between versions. */
export function dailyDayText(date: string): string {
  const d = new Date(`${date}T00:00:00Z`);
  if (Number.isNaN(d.getTime())) return '';
  return `${DAY_NAMES[weekdayOf(date)]} ${d.getUTCDate()} ${MONTHS[d.getUTCMonth()]}`;
}

/** The header's date: "No. 142 · Thursday 8 October", or "No. 142" on a
 *  phone, where the day does not fit beside the wordmark. */
export function dailyDateText(day: { no: number; date: string }, phone: boolean): string {
  const text = dailyDayText(day.date);
  return phone || !text ? `No. ${day.no}` : `No. ${day.no} · ${text}`;
}

// ---- scrolling ----

/** Where the map scrolls to put cards in front of the reader, in the part
 *  of it the panel leaves clear: `view` is that part's size. The box
 *  round all the cards is centred if it fits, with a margin; if not, the
 *  middle card of them, top to bottom, is. The rating strip takes the top
 *  26px, so the middle of what can be seen is lower by half of it. */
export function scrollTarget(
  cards: readonly Pick<Placed, 'left' | 'top'>[],
  size: { w: number; h: number },
  view: { w: number; h: number },
): { left: number; top: number } | null {
  if (cards.length === 0) return null;
  const box = (list: readonly Pick<Placed, 'left' | 'top'>[]) => ({
    x0: Math.min(...list.map((c) => c.left)),
    x1: Math.max(...list.map((c) => c.left + size.w)),
    y0: Math.min(...list.map((c) => c.top)),
    y1: Math.max(...list.map((c) => c.top + size.h)),
  });
  let b = box(cards);
  if (b.x1 - b.x0 + 48 > view.w || b.y1 - b.y0 + 48 > view.h - AXIS_H) {
    const byTop = [...cards].sort((p, q) => p.top - q.top);
    b = box([byTop[Math.floor((byTop.length - 1) / 2)]]);
  }
  return {
    left: Math.max(0, (b.x0 + b.x1) / 2 - view.w / 2),
    top: Math.max(0, (b.y0 + b.y1) / 2 - (AXIS_H + view.h) / 2),
  };
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
  const arg = move.kind === 'flip' ? move.card : move.kind === 'buy' ? move.clue : move.kind === 'guess' ? move.film : '';
  return `${seq}:${move.kind}:${arg}`;
}

/** What the reader is told when a move is refused, by the server's
 *  reason. Empty for a refusal that is handled without a word: "stale"
 *  hands back the game as it stands, and the page simply draws it. */
export function refusalText(reason: string | null, kind?: DailyMove['kind'] | 'play' | 'name'): string {
  switch (reason) {
    case 'stale':
      return '';
    case 'points':
      return kind === 'flip' ? 'Not enough points left for that card.' : 'Not enough points for that.';
    case 'known':
      if (kind === 'guess') return 'You’ve already guessed that one.';
      if (kind === 'flip') return 'That card is already turned over.';
      return 'You already have that clue.';
    case 'unknown':
      return 'Cinedikt doesn’t know that movie.';
    case 'day':
      return 'That map has ended.';
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

/** What the reader is told when their midnight comes with the page
 *  open: the same as a move refused for the day ("That map has ended."),
 *  if they were part way through a game, which goes as the new map comes;
 *  nothing over an intro or a result, which simply give way to it. */
export function midnightText(game: DailyGame | null): string {
  return game?.phase === 'play' ? refusalText('day') : '';
}

/** Said when a request never got an answer, and for a refusal with no
 *  words of its own. Sending the same move again is safe. */
export const UNREACHABLE = 'Couldn’t reach Cinedikt. Try again.';

/** Said while today's puzzle has not been picked yet. */
export const NOT_READY = 'Today’s map isn’t ready yet. Try again in a few minutes.';

/** The game a "stale" refusal hands back, or null. */
export function staleGame(body: unknown): DailyGame | null {
  const g = (body as { game?: unknown } | null)?.game;
  return g && typeof g === 'object' && Array.isArray((g as DailyGame).log) ? (g as DailyGame) : null;
}

/** Whether a game just handed back has a wrong guess the one before it
 *  did not, which shakes the panel. */
export function newWrongGuess(was: DailyGame | null, now: DailyGame): boolean {
  const count = (g: DailyGame | null) => g?.log.filter((e) => e.type === 'guess').length ?? 0;
  return count(now) > count(was);
}

// ---- the name ----

/** How many names the reroll spins through before it lands. */
export const REEL_STEPS = 9;

/** How long the reel waits on a name that has not arrived before it
 *  stops on the old one and says the server could not be reached. */
export const REEL_GIVE_UP_MS = 8000;

/** How long each name in the spin is shown: quicker at the start, slowing
 *  as it lands, like a reel. */
export function reelDelay(step: number): number {
  return 40 + step * 14;
}

const UPPER = 'ABCDEFGHIJKLMNOPRSTW';
const LOWER = 'aeiounrstlmdkchy';

/** One frame of the reroll. The client holds no pool of names to spin
 *  through — the server makes them — so the reel scrambles the letters
 *  instead, keeping each word's shape. Until the new name arrives it
 *  scrambles the old one; once it has, the frames lock its letters in
 *  from the left over the last steps, and the last frame is the name. */
export function reelFrame(from: string, to: string | null, step: number, rnd: () => number): string {
  const target = to ?? from;
  if (to != null && step >= REEL_STEPS) return to;
  const lock = to == null ? 0 : Math.max(0, Math.round(((step - (REEL_STEPS - 4)) / 4) * target.length));
  let out = '';
  for (let i = 0; i < target.length; i++) {
    const ch = target[i];
    if (i < lock || !/[A-Za-z]/.test(ch)) out += ch;
    else if (ch === ch.toUpperCase()) out += UPPER[Math.floor(rnd() * UPPER.length)];
    else out += LOWER[Math.floor(rnd() * LOWER.length)];
  }
  return out;
}

// ---- motion ----
//
// The design's numbers, written once, so each can be checked. Every one is
// off for a reader who has asked for nothing to move: script animations
// go through motion.ts's animate, which asks, and the CSS transitions
// have their reduced-motion rules at the end of the Daily's section.

/** A card turning over: .62s, overshooting a little as it lands. The
 *  stylesheet's .cd-daily-flip uses the same curve. */
export const FLIP_MS = 620;
export const FLIP_EASE = 'cubic-bezier(0.3, 0.75, 0.25, 1.18)';

/** The starting three, and any cards a move turns over together, turn
 *  one after another this far apart. */
export const STAGGER_MS = 150;

/** At the end the rest of the map turns over outward from the answer:
 *  each card waits 150ms, plus 0.45ms for every pixel it is from the
 *  answer, but never more than 1.3s. */
export const END_STAGGER_BASE_MS = 150;
export const END_STAGGER_PER_PX = 0.45;
export const END_STAGGER_MAX_MS = 1300;

/** How long each card waits to turn, for cards turning over together. */
export function staggerDelays(
  ids: readonly string[],
  done: boolean,
  layout: GridLayout | null,
): Record<string, number> {
  const out: Record<string, number> = {};
  if (!done) {
    ids.forEach((id, i) => {
      out[id] = i * STAGGER_MS;
    });
    return out;
  }
  const a = layout?.anchor;
  if (!a || !layout) return out;
  const at = new Map(layout.cards.map((c) => [c.film.id, c]));
  for (const id of ids) {
    const c = at.get(id);
    if (!c) continue;
    out[id] = Math.round(
      Math.min(END_STAGGER_MAX_MS, END_STAGGER_BASE_MS + Math.hypot(c.left - a.left, c.top - a.top) * END_STAGGER_PER_PX),
    );
  }
  return out;
}

/** Cards just turned over keep the accent ring this long after the last
 *  of them has turned, so the eye can find them. */
export const FRESH_HOLD_MS = 1900;

/** How long the ring lasts on a card a feed chip took the reader to. */
export const PULSE_MS = 1500;

/** The points total rolls to its new value over this long. */
export const ROLL_MS = 450;

/** What was spent floats up off the total and fades. */
export const SPEND_FLOAT_MS = 1100;
export const SPEND_FLOAT_KEYFRAMES: Keyframe[] = [
  { opacity: 0, transform: 'translateY(4px)' },
  { opacity: 1, transform: 'translateY(-8px)', offset: 0.25 },
  { opacity: 0, transform: 'translateY(-24px)' },
];

/** A wrong guess, and a loss, shake the panel. */
export const SHAKE_MS = 420;
export const SHAKE_KEYFRAMES: Keyframe[] = [0, -9, 8, -5, 3, 0].map((x) => ({
  transform: `translateX(${x}px)`,
}));

/** The curve things land on with a little bounce: the answer popping
 *  into its place, and a new name. */
export const BOUNCE_EASE = 'cubic-bezier(0.2, 0.9, 0.3, 1.2)';

/** The answer pops into its place at the end. */
export const ANSWER_POP_MS = 460;
export const ANSWER_POP_KEYFRAMES: Keyframe[] = [
  { opacity: 0, transform: 'scale(0.86)' },
  { opacity: 1, transform: 'scale(1)' },
];

/** A solve's score rises over the answer, after a beat, and is gone after
 *  SCORE_SHOWN_MS. */
export const SCORE_RISE_MS = 1900;
export const SCORE_RISE_DELAY_MS = 650;
export const SCORE_SHOWN_MS = 3000;
export const SCORE_RISE_KEYFRAMES: Keyframe[] = [
  { opacity: 0, transform: 'translateY(10px) scale(0.8)' },
  { opacity: 1, transform: 'translateY(0) scale(1.08)', offset: 0.2 },
  { opacity: 1, transform: 'translateY(-8px) scale(1)', offset: 0.75 },
  { opacity: 0, transform: 'translateY(-26px) scale(1)' },
];

/** A solve bursts into confetti from the answer this long after the end
 *  is drawn, on the beat of the answer's pop and the score's rise. The
 *  map may still be gliding to the answer then — a smooth scroll across
 *  a few thousand pixels of rows takes a second or more — so the burst
 *  does not stay where the card was when it began: it follows the card
 *  wherever the scroll carries it (burstShift). */
export const CONFETTI_AFTER_MS = 560;
export const CONFETTI_MS = 2400;
export const CONFETTI_PIECES = 150;
export const CONFETTI_COLOURS: Record<Theme, readonly string[]> = {
  dark: ['#b69cff', '#ffd166', '#7ad7ff', '#ff86b1', '#86e39a', '#ffffff'],
  light: ['#6b3fe4', '#e0a100', '#1c9ad6', '#e24a86', '#2fa84f', '#3a2f8f'],
};

/** How far the confetti is drawn from where it burst, so it stays on the
 *  answer's card: the card's centre now, on the screen, less the burst's
 *  origin, which was the card's centre when it burst. The pieces fly in
 *  the burst's own frame and the whole of it is carried by this, so the
 *  map gliding on to the card after the burst, or the reader scrolling
 *  during it, takes the confetti along. Nothing moves once the card is
 *  gone. */
export function burstShift(
  origin: { x: number; y: number },
  card: Pick<DOMRect, 'left' | 'top' | 'width' | 'height'> | null,
): { dx: number; dy: number } {
  if (!card) return { dx: 0, dy: 0 };
  return { dx: card.left + card.width / 2 - origin.x, dy: card.top + card.height / 2 - origin.y };
}

/** The result replaces the play panel this long after the end, once the
 *  answer has landed and the map has turned; its parts come in one after
 *  another while it is fresh. */
export const RESULTS_AFTER_MS = 1300;
export const RESULTS_FRESH_MS = 2600;
export const RESULTS_IN_MS = 460;
export function resultsDelay(i: number): number {
  return 60 + i * 80;
}

/** The result's numbers count up this long. */
export const COUNT_UP_MS = 900;

/** The intro arriving: the screen fades in, and its parts rise into place
 *  one after another. */
export const INTRO_IN_MS = 240;
export const INTRO_PART_MS = 520;
export function introDelay(i: number): number {
  return 80 + i * 70;
}

/** The intro leaving, before the game starts or the rules close: it
 *  fades up and away, quickly. */
export const INTRO_OUT_MS = 260;
export const INTRO_OUT_EASE = 'cubic-bezier(0.4, 0, 1, 1)';
export const INTRO_OUT_KEYFRAMES: Keyframe[] = [
  { opacity: 1, transform: 'scale(1)' },
  { opacity: 0, transform: 'scale(1.03)' },
];

/** The intro's cards and the glow under Play breathe while it is up. */
export const FAN_MIDDLE_MS = 1900;
export const FAN_LEFT_MS = 2300;
export const FAN_RIGHT_MS = 2100;
export const GLOW_MS = 1300;

/** While the intro is up, a blank card behind it now and then starts to
 *  turn over and thinks better of it. */
export const TWINKLE_EVERY_MS = 750;
export const TWINKLE_MS = 760;
export const TWINKLE_EASE = 'cubic-bezier(0.4, 0, 0.2, 1)';
export const TWINKLE_KEYFRAMES: Keyframe[] = [
  { transform: 'rotateY(0deg)' },
  { transform: 'rotateY(34deg)' },
  { transform: 'rotateY(0deg)' },
];

/** A new name lands with a bounce. */
export const NAME_BOUNCE_MS = 420;

/** How long the guess list stays after the field loses the focus, so a
 *  press on a row lands before the list goes. */
export const LIST_CLOSE_MS = 120;

/** How long typing has to pause before the guess field searches. The
 *  header's search waits as long. */
export const SEARCH_WAIT_MS = 250;

/** The most guesses the list offers. */
export const MAX_SUGGESTIONS = 6;
