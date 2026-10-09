// The Movies sheet's pure parts, which DailyMoviesSheet.tsx draws: one
// showing name's movies laid out on a small Cinedikt map, which cards are
// lit and how strongly, the bands the bought years and rating draw over
// it, the chips and what each one does, where the sheet opens scrolled
// to, and what its footer says. daily.ts holds the rules these rest on —
// what lights a card, the copy, the small metrics — and this file puts
// them together for the sheet. Like daily.ts it is checked without a DOM
// (dailyMovies.test.ts).
//
// Nothing here can tell today's movie from the rest, and nothing tries.
// The server marks none of them and sends a poster for every card or for
// none; every card is laid out, lit and drawn by the same rule from what
// every card carries, so the answer is one more card among them.

import type { DailyGame, DailyMovie, DailyPerson, DailySlot } from './api';
import {
  OVERLAP_COST,
  SHEET_LIT_TOP,
  SHEET_METRICS,
  SHEET_PHONE_SHARE,
  UNREACHABLE,
  affords,
  andList,
  codesOf,
  guessedIds,
  hueColour,
  movieLit,
  overlapChip,
  pickLine,
  ratingSpan,
  refusalText,
  sheetCardLabel,
  sheetHint,
  sheetLegend,
  sheetSub,
  sheetTitle,
  shownSlots,
  yearSpan,
  type SheetChoice,
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

type Shown = Extract<DailySlot, { shown: true }>;

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

/** The movies the map can place: a year to sit in and a rating to sit at.
 *  The server sends only rated movies that pass the map's own test, so
 *  this leaves nothing out in practice; a row headed "0" would look like
 *  a broken map. Counted the same way as drawn, so the line's "of 14"
 *  is the cards there are. */
export function placeable(movies: readonly DailyMovie[]): DailyMovie[] {
  return movies.filter((m) => m.year > 0 && Number.isFinite(m.rating));
}

/** The small map: the app's own layoutGrid, year down and rating across,
 *  at the sheet's sizes and the width of its scroller, over the movies it
 *  can place. The spine has no month or day, so a year's cards stack by
 *  id, which is stable and says nothing about any of them. */
export function sheetLayout(movies: readonly DailyMovie[], width: number, phone: boolean): GridLayout {
  const payload: GridPayload = {
    anchor: NO_ANCHOR,
    people: [],
    films: placeable(movies).map((m) => [m.id, m.year, m.rating, 0]),
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

/** A card's strength: lit, lit but already guessed (it cannot be the
 *  answer, but it is still inside everything the reader has narrowed to),
 *  and faded. */
export const CARD_LIT = 1;
export const CARD_TRIED = 0.55;
export const CARD_DIM = 0.14;

export function cardOpacity(lit: boolean, tried: boolean): number {
  if (!lit) return CARD_DIM;
  return tried ? CARD_TRIED : CARD_LIT;
}

/** The phone sheet's height: 90% of the visual viewport rather than the
 *  layout viewport, so Safari's toolbar never covers its foot. */
export function phoneSheetHeight(viewH: number): number {
  return Math.round(Math.max(viewH, 0) * SHEET_PHONE_SHARE);
}

/** What the movies the server sent depend on, besides whose they are:
 *  which slots are showing (each movie's `on` lists only those) and
 *  whether Director has been bought (`dir` is only sent after). Asked
 *  again when it changes, so a card never lights on a name the server
 *  had not counted. Neither can change while the sheet is up, which
 *  covers the page; this keeps the sheet right if that ever stops being
 *  so. */
export function moviesKey(game: Pick<DailyGame, 'slots' | 'facts'>): string {
  const slots = shownSlots(game).map((s) => s.slot);
  return `${slots.join(',')}|${game.facts.director?.length ? 'dir' : ''}`;
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

// ---- who is selected ----

/** The chip for today's directors: one chip for all of them, since it
 *  lights a movie any of them is on. */
export const DIRECTOR_CHIP = 'director';

/** The names switched on besides the one the sheet opened on, which is
 *  always on: the others, by IMDb id, and the director chip. */
export interface SheetPicks {
  others: readonly string[];
  director: boolean;
}

export const NO_PICKS: SheetPicks = { others: [], director: false };

/** The showing slot of the person the sheet opened on, or null when they
 *  are not one: the server then refuses the sheet, and it says so. */
export function openedOn(game: Pick<DailyGame, 'slots'>, person: string): Shown | null {
  return shownSlots(game).find((s) => s.person.id === person) ?? null;
}

/** Whether a showing name's overlap is the reader's: bought once, and a
 *  free switch for the rest of the game. */
export function owned(game: Pick<DailyGame, 'overlaps'>, person: string): boolean {
  return game.overlaps.includes(person);
}

/** Who the lit rule asks for: the slot the sheet opened on, every other
 *  name switched on whose overlap is theirs, and the director chip once
 *  Director has been bought. A name switched on but not owned — it never
 *  should be — asks for nothing rather than lighting what it has not
 *  paid for. */
export function sheetChoice(
  game: Pick<DailyGame, 'slots' | 'overlaps' | 'facts'>,
  person: string,
  picks: SheetPicks,
): SheetChoice {
  const slots: number[] = [];
  for (const s of shownSlots(game)) {
    if (s.person.id === person || (picks.others.includes(s.person.id) && owned(game, s.person.id))) slots.push(s.slot);
  }
  return { slots, director: picks.director && !!game.facts.director?.length };
}

/** Picks after a chip is pressed: a name or the director switched the
 *  other way. The name the sheet opened on is not anyone's to switch. */
export function togglePick(picks: SheetPicks, key: string): SheetPicks {
  if (key === DIRECTOR_CHIP) return { ...picks, director: !picks.director };
  return picks.others.includes(key)
    ? { ...picks, others: picks.others.filter((id) => id !== key) }
    : { ...picks, others: [...picks.others, key] };
}

/** The names in the title, in the order the chips run: the person it
 *  opened on, the others switched on by their place in the cast, then
 *  the directors together, as their chip names them. */
export function titleNames(
  game: Pick<DailyGame, 'slots' | 'overlaps' | 'facts'>,
  person: string,
  picks: SheetPicks,
): string[] {
  const lead = openedOn(game, person);
  const chosen = sheetChoice(game, person, picks);
  const names = lead ? [lead.person.name] : [];
  for (const s of shownSlots(game)) {
    if (s.person.id !== person && chosen.slots.includes(s.slot)) names.push(s.person.name);
  }
  if (chosen.director) names.push(directorsName(game.facts.director ?? []));
  return names;
}

function directorsName(directors: readonly DailyPerson[]): string {
  return andList(directors.map((p) => p.name));
}

// ---- the chips ----

/** What pressing a chip does: nothing (the person it opened on), switch
 *  a name that is the reader's, or buy one's overlap. */
export type ChipAct = 'none' | 'toggle' | 'buy';

export interface SheetChip {
  /** The person's IMDb id, or DIRECTOR_CHIP. */
  key: string;
  /** Whose face it carries: the person, or the first director. */
  person: DailyPerson;
  /** The face is a rounded square, as a director's is on the map. */
  director: boolean;
  /** Their colour, for this theme, set inline as --tone. */
  tone: string;
  /** Initials for a face with no photo. */
  code: string;
  /** "Joe Pantoliano", "+ Gloria Foster", "Lana Wachowski and Lilly
   *  Wachowski". */
  label: string;
  /** "−250" on a name not yet bought; nothing otherwise. */
  price: string;
  /** Switched on: drawn in the person's colour. */
  on: boolean;
  act: ChipAct;
  /** Pressing it does something now. */
  can: boolean;
  /** At half strength: an overlap the points cannot cover, or one the
   *  game is over for. */
  faint: boolean;
  /** For a name for sale, what pressing buys and what it costs; the
   *  label says the rest. */
  aria: string;
}

/** The chip row, in order: the person it opened on, always on and never
 *  switched off; every other showing name, by their place in the cast —
 *  "+ Gloria Foster −250" until their overlap is bought, then a free
 *  switch; and, once Director has been bought, the directors' chip, free
 *  from the start. `buying` is a name whose overlap is being bought, held
 *  still until the answer comes. */
export function sheetChips(
  game: Pick<DailyGame, 'phase' | 'pts' | 'slots' | 'overlaps' | 'facts'>,
  person: string,
  picks: SheetPicks,
  buying: string | null,
  theme: Theme,
): SheetChip[] {
  const shown = shownSlots(game);
  const directors = game.facts.director ?? [];
  const codes = codesOf([...shown.map((s) => s.person), ...directors]);
  const playing = game.phase === 'play';
  const chosen = sheetChoice(game, person, picks);
  const lead = shown.find((s) => s.person.id === person);
  const others = shown.filter((s) => s.person.id !== person);
  const chip = (p: DailyPerson, over: Omit<SheetChip, 'person' | 'tone' | 'code' | 'director'>): SheetChip => ({
    person: p,
    director: false,
    tone: hueColour(p.hue, theme),
    code: codes.get(p.id) ?? '?',
    ...over,
  });
  const out: SheetChip[] = [];
  if (lead) {
    const p = lead.person;
    out.push(chip(p, { key: p.id, label: p.name, price: '', on: true, act: 'none', can: false, faint: false, aria: '' }));
  }
  for (const s of others) {
    const p = s.person;
    const mine = owned(game, p.id);
    const { label, price } = overlapChip(p.name, mine);
    if (mine) {
      out.push(chip(p, { key: p.id, label, price, on: chosen.slots.includes(s.slot), act: 'toggle', can: true, faint: false, aria: '' }));
      continue;
    }
    const covered = playing && affords(game.pts, OVERLAP_COST);
    out.push(
      chip(p, {
        key: p.id,
        label,
        price,
        on: false,
        act: 'buy',
        can: covered && buying == null,
        faint: !covered,
        aria: `Add ${p.name} to light only the movies they share. It costs ${OVERLAP_COST} points.`,
      }),
    );
  }
  if (directors.length) {
    const p = directors[0];
    out.push({
      ...chip(p, {
        key: DIRECTOR_CHIP,
        label: directorsName(directors),
        price: '',
        on: chosen.director,
        act: 'toggle',
        can: true,
        faint: false,
        aria: '',
      }),
      director: true,
    });
  }
  return out;
}

// ---- the cards, the rows and the bands ----

export interface SheetCard {
  id: string;
  title: string;
  /** Sent for every card or for none. */
  poster?: string;
  left: number;
  top: number;
  /** "7.8". */
  rating: string;
  lit: boolean;
  /** Guessed already: an ✕ by its rating, and lit at 0.55. */
  tried: boolean;
  /** Tapped, so the footer offers it: a 2px accent ring. */
  picked: boolean;
  opacity: number;
  aria: string;
  /** Its poster is loaded: it is inside the stretch of the map the
   *  reader has been near. Every other card keeps its stand-in. */
  warm: boolean;
}

export interface SheetRow {
  /** The year, as a key. */
  key: string;
  year: number;
  top: number;
  height: number;
  /** Inside the bought decade or five years: washed in the accent, and
   *  its label in the accent's text colour. */
  inYears: boolean;
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

/** The rows, each washed when it is inside the years bought. */
export function sheetRows(facts: DailyGame['facts'], layout: Pick<GridLayout, 'rows' | 'metrics'>): SheetRow[] {
  const years = yearSpan(facts);
  const labelTop = SHEET_METRICS.padTop + layout.metrics.cardH / 2 - YEAR_LABEL_RISE;
  return layout.rows.map((r) => {
    const inYears = !!years && r.year >= years[0] && r.year <= years[1];
    return {
      key: String(r.year),
      year: r.year,
      top: r.top,
      height: r.height,
      inYears,
      band: `${bandClass(r)}${inYears ? ' cd-msheet-band-in' : ''}`,
      labelTop,
    };
  });
}

/** Where the sheet opens scrolled to: the first lit card, SHEET_LIT_TOP
 *  from the map's top, so a bought range is already in view. The top
 *  when nothing is lit. */
export function openingScroll(cards: readonly Pick<SheetCard, 'top' | 'lit'>[]): number {
  let first: number | null = null;
  for (const c of cards) if (c.lit && (first == null || c.top < first)) first = c.top;
  return first == null ? 0 : Math.max(0, first - SHEET_LIT_TOP);
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
      /** Guess it can be pressed: not tried, the game still on, and no
       *  name being bought. */
      can: boolean;
    }
  | { kind: 'hint'; text: string };

/** The footer: the card tapped, with Guess it, or Already tried at half
 *  strength; or, with nothing tapped, how to guess and what a wrong
 *  guess costs now.
 *
 *  Guess it is held while a name's overlap is being bought (`buying`),
 *  as the names for sale are: the page makes one move at a time, so a
 *  guess pressed then would close the sheet on a guess never sent, and
 *  the name paid for would never be switched on. */
export function sheetFoot(
  movie: DailyMovie | null,
  game: Pick<DailyGame, 'phase' | 'nextCost' | 'log'>,
  buying: string | null = null,
): SheetFoot {
  if (!movie) return { kind: 'hint', text: sheetHint(game.nextCost) };
  const tried = guessedIds(game).has(movie.id);
  return {
    kind: 'pick',
    id: movie.id,
    title: movie.title,
    line: pickLine(movie),
    label: tried ? ALREADY_TRIED : GUESS_IT,
    can: !tried && game.phase === 'play' && buying == null,
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
  picks: SheetPicks;
  /** The card tapped, by IMDb id. */
  pick: string | null;
  buying: string | null;
  /** The stretch of the map the reader has been near, whose posters
   *  load; null until the sheet has been scrolled to where it opens. */
  reach: { top: number; bottom: number } | null;
  theme: Theme;
}

export interface SheetView {
  /** The person it opened on, for the header's face, or null when they
   *  are not showing. */
  person: DailyPerson | null;
  tone: string;
  code: string;
  title: string;
  /** "6 of 14 movies lit · on Cinedikt"; nothing while the movies are on
   *  their way. */
  sub: string;
  legend: string;
  chips: SheetChip[];
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
}

/** Everything the sheet draws, from where it stands. */
export function sheetView(s: SheetState): SheetView {
  const { game, person, picks, layout, theme } = s;
  const movies = s.movies ? placeable(s.movies) : null;
  const chips = sheetChips(game, person, picks, s.buying, theme);
  const lead = chips.find((c) => c.key === person) ?? null;
  const chosen = sheetChoice(game, person, picks);
  const tried = guessedIds(game);
  const byId = new Map((movies ?? []).map((m) => [m.id, m]));
  const cards: SheetCard[] = [];
  if (layout) {
    const { cardH } = layout.metrics;
    for (const c of layout.cards) {
      const m = byId.get(c.film.id);
      if (!m) continue;
      const lit = movieLit(m, chosen, game.facts);
      const was = tried.has(m.id);
      cards.push({
        id: m.id,
        title: m.title,
        poster: m.poster,
        left: c.left,
        top: c.top,
        rating: ratingText(m.rating),
        lit,
        tried: was,
        picked: s.pick === m.id,
        opacity: cardOpacity(lit, was),
        aria: sheetCardLabel(m, lit, was),
        warm: s.reach != null && inWarmSpan(c.top, cardH, s.reach),
      });
    }
  }
  const lit = cards.filter((c) => c.lit).length;
  const picked = (s.pick && byId.get(s.pick)) || null;
  return {
    person: lead?.person ?? null,
    tone: lead?.tone ?? '',
    code: lead?.code ?? '',
    title: sheetTitle(titleNames(game, person, picks)),
    sub: movies && layout ? sheetSub(lit, cards.length) : '',
    legend: sheetLegend(game.facts),
    chips,
    rows: layout ? sheetRows(game.facts, layout) : [],
    ticks: layout ? sheetTicks(layout) : [],
    column: layout ? ratingColumn(game.facts, layout) : null,
    cards,
    plotH: layout?.plotH ?? 0,
    cardW: layout?.metrics.cardW ?? 0,
    cardH: layout?.metrics.cardH ?? 0,
    foot: sheetFoot(picked, game, s.buying),
    scrollTo: openingScroll(cards),
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
