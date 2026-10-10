// The Movies sheet's pure parts, which DailyMoviesSheet.tsx draws: one
// showing name's movies laid out on a small Cinedikt map, which cards can
// be read and how strongly each is drawn, the bands the bought decade and
// rating draw over it, where the sheet opens scrolled to, and what its
// footer says. daily.ts holds the rules these rest on — the copy, the
// small metrics — and this file puts them together for the sheet. Like
// daily.ts it is checked without a DOM (dailyMovies.test.ts).
//
// What can be read is the server's to say, not this file's. A movie
// inside every range the reader has bought, the decade and the rating
// band, comes with its title, rating and the rest; any other comes as a
// blank card, its year and its place on the rating axis, and nothing
// here could put a title on it. A genre bought is never drawn here, nor
// named as a range: it reads nothing on the map. Today's movie is one
// more card among them, by the same rule, and nothing here can tell it
// from the rest or tries: every card is laid out and drawn from what
// every card of its kind carries.

import type { DailyBlankMovie, DailyGame, DailyMovie, DailyPerson, DailyReadableMovie } from './api';
import {
  SHEET_COACH,
  SHEET_METRICS,
  SHEET_PHONE_SHARE,
  SHEET_READ_TOP,
  UNREACHABLE,
  blankCardLabel,
  codesOf,
  decadeSpan,
  guessedIds,
  hueColour,
  pickLine,
  rangeBought,
  ratingSpan,
  refusalText,
  sheetCardLabel,
  sheetHint,
  sheetLegend,
  sheetSub,
  sheetTitle,
  shownSlots,
} from './daily';
import {
  AXIS_LABEL_W,
  DEFAULT_SETTINGS,
  bandClass,
  inWarmSpan,
  layoutGrid,
  ratingText,
  xOf,
  type GridFilm,
  type GridLayout,
  type GridPayload,
  type GridSettings,
  type LayoutSizes,
} from './grid';
import type { Theme } from './theme';

// ---- the movies ----

/** Whether the server sent this movie readable: only a readable movie
 *  has an id. */
export function isReadable(movie: DailyMovie): movie is DailyReadableMovie {
  return 'id' in movie && typeof movie.id === 'string';
}

/** Where a card sits on the rating axis: a readable movie at its rating,
 *  a blank one at the half its rating was rounded to, which is all the
 *  page is told of it. */
export function ratingAt(movie: DailyMovie): number {
  return isReadable(movie) ? movie.rating : movie.at;
}

/** A movie as the map holds it: under a key of its own, its id for a
 *  readable one. A blank card has no id, so it is keyed by its year and
 *  its place, and a count for any others in the same spot, which says
 *  nothing the card's place on the map does not. The keys of blank cards
 *  start with "~", after every IMDb id, so a year's readable cards are
 *  stacked first and its blank ones after them, whatever they are. */
export interface SheetEntry {
  key: string;
  movie: DailyMovie;
}

/** The movies the map can place: a year to sit in and a place on the
 *  rating axis. The server sends only rated movies that pass the map's
 *  own test, so this leaves nothing out in practice; a row headed "0"
 *  would look like a broken map. Counted the same way as drawn, so the
 *  line's "of 13" is the cards there are. */
export function sheetEntries(movies: readonly DailyMovie[]): SheetEntry[] {
  const seen = new Map<string, number>();
  const out: SheetEntry[] = [];
  for (const movie of movies) {
    if (!(movie.year > 0) || !Number.isFinite(ratingAt(movie))) continue;
    if (isReadable(movie)) {
      out.push({ key: movie.id, movie });
      continue;
    }
    const spot = `~${movie.year}:${movie.at.toFixed(1)}`;
    const n = seen.get(spot) ?? 0;
    seen.set(spot, n + 1);
    out.push({ key: `${spot}:${n}`, movie });
  }
  return out;
}

// ---- the map ----

/** The card's poster down its left side, inside the card's padding: 24px
 *  wide, and as tall as the card leaves it. */
export const SHEET_POSTER_W = 24;
export const SHEET_CARD_PAD = 3;

/** As the handoff lays the small map out: the lowest rating's card 4px
 *  clear of the year rail, the highest's 8px clear of the edge, and 12px
 *  under the last row, so its cards are not flush with the footer's rule.
 *  The map has no floating buttons to scroll clear of, as the page's
 *  map does. */
export const SHEET_EDGE_LEFT = 4;
export const SHEET_EDGE_RIGHT = 8;
export const SHEET_BOTTOM_PAD = 12;

/** The narrowest the map is laid out at. A phone is 320px at the least
 *  and the side panel 560px at the most, so this only holds a layout
 *  together for the moment before the map has been measured. */
export const SHEET_MIN_W = 300;

/** The layout's sizes on this screen: the handoff's small card, rail and
 *  gaps (daily.ts's SHEET_METRICS), the 112px card on a panel, 100px on a
 *  phone. */
export function sheetSizes(phone: boolean): LayoutSizes {
  const card = phone ? SHEET_METRICS.phone : SHEET_METRICS.desktop;
  return {
    cardW: card.cardW,
    cardH: card.cardH,
    posterW: SHEET_POSTER_W,
    posterH: card.cardH - 2 * SHEET_CARD_PAD,
    railW: card.railW,
    gap: SHEET_METRICS.gap,
    padTop: SHEET_METRICS.padTop,
    padBottom: SHEET_METRICS.padBottom,
    gapMark: SHEET_METRICS.gapMark,
    edgeLeft: SHEET_EDGE_LEFT,
    edgeRight: SHEET_EDGE_RIGHT,
    bottomPad: SHEET_BOTTOM_PAD,
  };
}

/** The searched film the map's layout asks for, which the sheet does not
 *  have: an id no movie carries, so no card is anyone's anchor and none
 *  is drawn apart from the rest. */
const NO_ANCHOR: GridFilm = { id: '', title: '', year: 0, rating: null, md: 0, people: [], isAnchor: false };

/** Every year, oldest first, with no year lit for an anchor and no
 *  unrated column: every movie the server sends is rated. */
const SHEET_SETTINGS: GridSettings = { ...DEFAULT_SETTINGS, showUnrated: false, highlightYear: false };

/** The small map: the app's own layoutGrid, year down and rating across,
 *  at the sheet's sizes and the width of its scroller, over the movies it
 *  can place, each under its key (sheetEntries). A readable card sits at
 *  its rating and a blank one at its place. The spine has no month or
 *  day, so a year's cards stack by key, which is stable and says nothing
 *  about any of them. */
export function sheetLayout(movies: readonly DailyMovie[], width: number, phone: boolean): GridLayout {
  const payload: GridPayload = {
    anchor: NO_ANCHOR,
    people: [],
    films: sheetEntries(movies).map((e) => [e.key, e.movie.year, ratingAt(e.movie), 0]),
  };
  return layoutGrid(payload, Math.max(width, SHEET_MIN_W), SHEET_SETTINGS, undefined, phone, sizesOf(phone));
}

/** sheetSizes, made once per screen class: the layout runs on every
 *  width the scroller reports, and its sizes never change between. */
const SIZES = { phone: sheetSizes(true), panel: sheetSizes(false) };
function sizesOf(phone: boolean): LayoutSizes {
  return phone ? SIZES.phone : SIZES.panel;
}

/** A year's label sits this far above the middle of the row's first
 *  lane: half of its 11.5px line, so it is centred on the first card. */
export const YEAR_LABEL_RISE = 7;

/** A card's strength: readable, readable but already guessed (it cannot
 *  be the answer, but it is still inside everything the reader has
 *  narrowed to), and blank, a tile there only to keep the map's shape. */
export const CARD_READ = 1;
export const CARD_TRIED = 0.55;
export const CARD_BLANK = 0.3;

export function cardOpacity(readable: boolean, tried: boolean): number {
  if (!readable) return CARD_BLANK;
  return tried ? CARD_TRIED : CARD_READ;
}

/** The phone sheet's height: 90% of the visual viewport rather than the
 *  layout viewport, so Safari's toolbar never covers its foot. */
export function phoneSheetHeight(viewH: number): number {
  return Math.round(Math.max(viewH, 0) * SHEET_PHONE_SHARE);
}

/** What the sheet says when the movies cannot be had: the refusal's own
 *  words for a reason it has some for, and otherwise that Cinedikt could
 *  not be reached, which asking again may cure. */
export function moviesFailed(reason: string | null): string {
  return refusalText(reason, 'movies') || UNREACHABLE;
}

/** The stretch of the map whose posters are loaded: every band the
 *  reader has been near, as one span, so a poster once shown stays shown
 *  when its card is scrolled away and back. The same object when the new
 *  band is already inside it. */
export function widenReach(
  was: { top: number; bottom: number } | null,
  band: { top: number; bottom: number },
): { top: number; bottom: number } {
  if (!was) return band;
  if (band.top >= was.top && band.bottom <= was.bottom) return was;
  return { top: Math.min(was.top, band.top), bottom: Math.max(was.bottom, band.bottom) };
}

/** The showing slot of the person the sheet opened on, or null when they
 *  are not one: the server then refuses the sheet, and it says so. */
export function openedOn(
  game: Pick<DailyGame, 'slots'>,
  person: string,
): Extract<DailyGame['slots'][number], { shown: true }> | null {
  return shownSlots(game).find((s) => s.person.id === person) ?? null;
}

// ---- the cards, the rows and the bands ----

/** A card on the map: one the reader can read and guess, or a blank tile
 *  in its place, which has nothing to say and cannot be pressed. */
export type SheetCard =
  | {
      kind: 'readable';
      key: string;
      id: string;
      title: string;
      /** Sent for every readable card or for none. */
      poster?: string;
      left: number;
      top: number;
      /** "7.8". */
      rating: string;
      /** Guessed already: an ✕ by its rating, and drawn at 0.55. */
      tried: boolean;
      /** Tapped, so the footer offers it: a 2px accent ring. */
      picked: boolean;
      opacity: number;
      aria: string;
      /** Its poster is loaded: it is inside the stretch of the map the
       *  reader has been near. Every other card keeps its stand-in. */
      warm: boolean;
    }
  | {
      kind: 'blank';
      key: string;
      left: number;
      top: number;
      opacity: number;
      aria: string;
    };

export interface SheetRow {
  /** The year, as a key. */
  key: string;
  year: number;
  top: number;
  height: number;
  /** Inside the bought decade: washed in the accent, and its label in
   *  the accent's text colour. */
  inDecade: boolean;
  /** The map's own band classes (striped, ruled at a decade), and the
   *  wash for a bought year. */
  band: string;
  /** The label's top inside the row: centred on the first lane. */
  labelTop: number;
}

/** A whole rating on the axis, and its gridline. */
export interface SheetTick {
  rating: number;
  label: string;
  x: number;
  labelLeft: number;
}

/** The axis's whole ratings, 4 to 9, as the handoff labels them: plain
 *  numbers, where the page's map writes "4.0". */
export function sheetTicks(layout: Pick<GridLayout, 'metrics'>): SheetTick[] {
  return SHEET_METRICS.labels.map((rating) => {
    const x = Math.round(xOf(rating, layout.metrics));
    return { rating, label: String(rating), x, labelLeft: x - AXIS_LABEL_W / 2 };
  });
}

/** The bought rating band as a column across the map, between the card
 *  centres its floor and ceiling sit at, clamped to the axis, and never
 *  narrower than 4px. Null with no rating bought. */
export function ratingColumn(
  facts: DailyGame['facts'],
  layout: Pick<GridLayout, 'metrics'>,
): { left: number; width: number } | null {
  const span = ratingSpan(facts);
  if (!span) return null;
  const a = Math.round(xOf(span[0], layout.metrics));
  const b = Math.round(xOf(span[1], layout.metrics));
  return { left: a, width: Math.max(4, b - a) };
}

/** The rows, each washed when it is inside the decade bought. */
export function sheetRows(facts: DailyGame['facts'], layout: Pick<GridLayout, 'rows' | 'metrics'>): SheetRow[] {
  const decade = decadeSpan(facts);
  const labelTop = SHEET_METRICS.padTop + layout.metrics.cardH / 2 - YEAR_LABEL_RISE;
  return layout.rows.map((r) => {
    const inDecade = !!decade && r.year >= decade[0] && r.year <= decade[1];
    return {
      key: String(r.year),
      year: r.year,
      top: r.top,
      height: r.height,
      inDecade,
      band: `${bandClass(r)}${inDecade ? ' cd-msheet-band-in' : ''}`,
      labelTop,
    };
  });
}

/** Where the sheet opens scrolled to: the first readable card,
 *  SHEET_READ_TOP from the map's top, so a bought range is already in
 *  view. The top when there is nothing to read yet. */
export function openingScroll(cards: readonly Pick<SheetCard, 'top' | 'kind'>[]): number {
  let first: number | null = null;
  for (const c of cards) if (c.kind === 'readable' && (first == null || c.top < first)) first = c.top;
  return first == null ? 0 : Math.max(0, first - SHEET_READ_TOP);
}

/** The one-time pointer at the named cards: a pill under the first card
 *  that can still be guessed, its caret at the card's middle. */
export interface SheetCoach {
  text: string;
  left: number;
  top: number;
  width: number;
  /** The caret's middle, from the pill's left edge. */
  caret: number;
}

/** The pill's width: the words on one line at the hint's size. */
export const COACH_W = 172;
/** Between the card's foot and the caret's tip. */
const COACH_GAP = 9;
/** How near the pill's edge the caret may come, past its rounding. */
const CARET_INSET = 18;

/** Where the pointer goes: under the first named card not yet tried, in
 *  reading order, the card the sheet opened scrolled to, or one near it.
 *  It sits under the card, not over it, since the axis covers the top of
 *  the map, and is held inside the plot, its caret still on the card.
 *  Null with no card to point at. */
export function coachAt(
  cards: readonly SheetCard[],
  plotW: number,
  cardW: number,
  cardH: number,
): SheetCoach | null {
  let first: Extract<SheetCard, { kind: 'readable' }> | null = null;
  for (const c of cards) {
    if (c.kind !== 'readable' || c.tried) continue;
    if (!first || c.top < first.top || (c.top === first.top && c.left < first.left)) first = c;
  }
  if (!first) return null;
  const mid = first.left + cardW / 2;
  const left = Math.max(0, Math.min(mid - COACH_W / 2, plotW - COACH_W));
  const caret = Math.max(CARET_INSET, Math.min(mid - left, COACH_W - CARET_INSET));
  return { text: SHEET_COACH, left: Math.round(left), top: first.top + cardH + COACH_GAP, width: COACH_W, caret: Math.round(caret) };
}

// ---- the footer ----

export const GUESS_IT = 'Guess it';
export const ALREADY_TRIED = 'Already tried';

/** What the map's place says while the movies are on their way, so a
 *  slow answer is not an empty sheet that looks broken. */
export const MOVIES_LOADING = 'Loading their movies…';

export type SheetFoot =
  | {
      kind: 'pick';
      id: string;
      title: string;
      /** "1993 · IMDb 7.8". */
      line: string;
      /** Guess it, or Already tried. */
      label: string;
      /** Guess it can be pressed: not tried, and the game still on. */
      can: boolean;
    }
  | { kind: 'hint'; text: string };

/** The footer: the card tapped, with Guess it, or Already tried at half
 *  strength; or, with nothing tapped, how to guess and what a wrong guess
 *  costs now, or, before any range is bought, where the ranges are. Only
 *  a readable card can be tapped. */
export function sheetFoot(
  movie: DailyReadableMovie | null,
  game: Pick<DailyGame, 'phase' | 'nextCost' | 'log' | 'facts'>,
): SheetFoot {
  if (!movie) return { kind: 'hint', text: sheetHint(game.nextCost, rangeBought(game.facts)) };
  const tried = guessedIds(game).has(movie.id);
  return {
    kind: 'pick',
    id: movie.id,
    title: movie.title,
    line: pickLine(movie),
    label: tried ? ALREADY_TRIED : GUESS_IT,
    can: !tried && game.phase === 'play',
  };
}

// ---- the whole sheet ----

export interface SheetState {
  game: DailyGame;
  /** The person it opened on, by IMDb id. */
  person: string;
  /** Their movies as the server sent them, or null while on the way. */
  movies: readonly DailyMovie[] | null;
  /** sheetLayout over them, or null while they are on the way. */
  layout: GridLayout | null;
  /** The card tapped, by IMDb id. */
  pick: string | null;
  /** The stretch of the map the reader has been near, whose posters
   *  load; null until the sheet has been scrolled to where it opens. */
  reach: { top: number; bottom: number } | null;
  theme: Theme;
  /** The one-time pointer at the named cards is still to be shown on
   *  this device, and nothing has been tapped yet. */
  coach?: boolean;
}

export interface SheetView {
  /** The person it opened on, for the header's face, or null when they
   *  are not showing. */
  person: DailyPerson | null;
  tone: string;
  code: string;
  title: string;
  /** "13 movies · on Cinedikt", or "5 of 13 movies named · on
   *  Cinedikt" once a range is bought; nothing while the movies are on
   *  their way. */
  sub: string;
  /** Which movies are named, and how many today's could be. */
  legend: string;
  rows: SheetRow[];
  ticks: SheetTick[];
  column: { left: number; width: number } | null;
  cards: SheetCard[];
  /** The map's height, every row and the room under the last. */
  plotH: number;
  cardW: number;
  cardH: number;
  foot: SheetFoot;
  /** Where to scroll to when the sheet opens. */
  scrollTo: number;
  /** The one-time pointer, when it is due and there is a card to point
   *  at. */
  coach: SheetCoach | null;
}

/** Everything the sheet draws, from where it stands. */
export function sheetView(s: SheetState): SheetView {
  const { game, person, layout, theme } = s;
  const ranged = rangeBought(game.facts);
  const lead = openedOn(game, person);
  const codes = codesOf(shownSlots(game).map((x) => x.person));
  const tried = guessedIds(game);
  const byKey = new Map((s.movies ? sheetEntries(s.movies) : []).map((e) => [e.key, e.movie]));
  const cards: SheetCard[] = [];
  if (layout) {
    const { cardH } = layout.metrics;
    for (const c of layout.cards) {
      const m = byKey.get(c.film.id);
      if (!m) continue;
      cards.push(isReadable(m) ? readableCard(m, c, s, tried.has(m.id), cardH) : blankCard(m, c, ranged));
    }
  }
  const named = cards.filter((c): c is Extract<SheetCard, { kind: 'readable' }> => c.kind === 'readable');
  const left = named.filter((c) => !c.tried).length;
  const picked = s.pick ? byKey.get(s.pick) : undefined;
  return {
    person: lead?.person ?? null,
    tone: lead ? hueColour(lead.person.hue, theme) : '',
    code: lead ? (codes.get(lead.person.id) ?? '?') : '',
    title: sheetTitle(lead?.person.name ?? ''),
    sub: s.movies && layout ? sheetSub(named.length, cards.length, ranged) : '',
    legend: sheetLegend(game.facts, s.movies && layout ? { named: named.length, left } : null),
    rows: layout ? sheetRows(game.facts, layout) : [],
    ticks: layout ? sheetTicks(layout) : [],
    column: layout ? ratingColumn(game.facts, layout) : null,
    cards,
    plotH: layout?.plotH ?? 0,
    cardW: layout?.metrics.cardW ?? 0,
    cardH: layout?.metrics.cardH ?? 0,
    foot: sheetFoot(picked && isReadable(picked) ? picked : null, game),
    scrollTo: openingScroll(cards),
    coach:
      s.coach && layout && !s.pick ? coachAt(cards, layout.plotW, layout.metrics.cardW, layout.metrics.cardH) : null,
  };
}

function readableCard(
  m: DailyReadableMovie,
  c: { left: number; top: number },
  s: Pick<SheetState, 'pick' | 'reach'>,
  tried: boolean,
  cardH: number,
): SheetCard {
  return {
    kind: 'readable',
    key: m.id,
    id: m.id,
    title: m.title,
    poster: m.poster,
    left: c.left,
    top: c.top,
    rating: ratingText(m.rating),
    tried,
    picked: s.pick === m.id,
    opacity: cardOpacity(true, tried),
    aria: sheetCardLabel(m, tried),
    warm: s.reach != null && inWarmSpan(c.top, cardH, s.reach),
  };
}

function blankCard(m: DailyBlankMovie, c: { film: { id: string }; left: number; top: number }, ranged: boolean): SheetCard {
  return {
    kind: 'blank',
    key: c.film.id,
    left: c.left,
    top: c.top,
    opacity: CARD_BLANK,
    aria: blankCardLabel(m.year, ranged),
  };
}

// ---- the focus ----

/** Where Tab goes from the `at`th of `count` controls in the open sheet,
 *  going back with Shift: round from the last to the first, and from the
 *  first to the last, so the focus never leaves a modal sheet for the
 *  page dimmed behind it. `at` is -1 when the focus is on the sheet
 *  itself, as it is when the sheet opens. Null when Tab can go where it
 *  would anyway. */
export function tabWrap(at: number, count: number, back: boolean): number | null {
  if (count <= 0) return null;
  if (at < 0) return back ? count - 1 : 0;
  if (back && at === 0) return count - 1;
  if (!back && at === count - 1) return 0;
  return null;
}
