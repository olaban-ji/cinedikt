// The rating grid.
//
// Y is the release year, as it has always been. Inside a year, lanes
// follow month and day so stacked cards read in calendar order — we do
// not label those, we only sit them that way. X is the rating: low on
// the left, high on the right. There are no edges — who links a film to
// the searched one is said on the card and in the panel.
//
// Everything here is pure: `layoutGrid` takes the payload, a width and
// the reader's settings, and returns positions. Nothing touches the DOM,
// so §10's acceptance checks can be made as unit tests.

export interface GridPerson {
  /** An IMDb name id, such as nm0000206. */
  id: string;
  name: string;
  role: 'cast' | 'director';
  character?: string;
  /** The address of the person's photo on TMDb's image host, 185 pixels
   *  wide. Absent when the server's people job has not answered for them
   *  yet, when TMDb has no photo, and once an answer is too old to show;
   *  fetchPeoplePhotos tells the first of those from the others. */
  photo?: string;
}

/** Where a card goes and whose it is, and nothing else. The server
 *  sends the whole spine at once, so the layout is final from the first
 *  paint and no card ever moves again. */
export type SpineTuple = [
  id: string,
  year: number,
  rating: number | null,
  /** Month and day as MMDD, 0 when the date says only a year. */
  md: number,
  /** Who of the chip row is on this film, as indexes into
   *  `payload.people`. Indexes rather than name ids, because there are
   *  four hundred films and the ids would be most of the payload. */
  people?: number[],
  /** The film's genres, as bits over `payload.genres`: bit i is
   *  genres[i]. Absent on a spine that carries no genres. */
  genres?: number,
];

export interface SpineFilm {
  /** An IMDb title id, such as tt0133093. */
  id: string;
  year: number;
  rating: number | null;
  /** Month and day as MMDD. Cards stacked in one year sit in calendar
   *  order; the year is already the row, so nothing else is needed. */
  md: number;
  /** Places in the chip row. It is on the spine rather than only in the
   *  detail so that a row can be judged before anybody has scrolled to
   *  it: hiding the empty years asks of every year whether anything in
   *  it is lit, including the years nobody has looked at yet. */
  people: number[];
  /** The film's genres as bits over `payload.genres`, 0 for none. On the
   *  spine for the same reason as the people: the genre filter lights
   *  cards, and hiding the empty years asks it of every year. */
  genres: number;
  isAnchor: boolean;
}

/** What a card says, which arrives a screen at a time into a box that
 *  already exists. Its people are name ids, not spine indexes: the card
 *  draws a marker per person and looks each one up by id. */
export interface GridFilm extends Omit<SpineFilm, 'people' | 'genres'> {
  title: string;
  poster?: string;
  /** What the film is about, from OMDb or, where it has none, TMDb.
   *  Absent when no source has one yet. */
  synopsis?: string;
  /** IMDb's genres, up to three, in IMDb's order. Absent when IMDb lists none. */
  genres?: string[];
  people: string[];
}

/** The genres as the preview and the panel show them: "Action · Sci-Fi". */
export function genreLine(f: Pick<GridFilm, 'genres'>): string {
  return (f.genres ?? []).map((g) => g.trim()).filter(Boolean).join(' · ');
}

/** What the server sends: the searched film, its people, and the spine. */
export interface GridPayload {
  anchor: GridFilm;
  people: GridPerson[];
  films: SpineTuple[];
  /** The stamp on this movie's share image, so the client can ask for
   *  it while the map opens rather than leaving the first reader to
   *  paste a link at an address that has nothing behind it yet. */
  og_v?: string;
  /** The legend for the genre bits on the spine: every genre a mapped
   *  film can carry, in IMDb's alphabetical order. The server sends it so
   *  the client never keeps a list of its own. Absent where the spine
   *  carries no genres, and then there is no genre filter. */
  genres?: string[];
}

/** The spine, read into something with names on it. */
export function spineOf(payload: GridPayload): SpineFilm[] {
  return payload.films.map(([id, year, rating, md, people, genres]) => ({
    id,
    year,
    rating,
    md: md ?? 0,
    people: people ?? [],
    genres: genres ?? 0,
    isAnchor: id === payload.anchor.id,
  }));
}

/** The bits for the genres picked, over the payload's legend. A name the
 *  legend does not hold sets nothing, and without a legend nothing is
 *  asked for at all. */
export function genreMask(payload: Pick<GridPayload, 'genres'>, names: readonly string[]): number {
  const legend = payload.genres ?? [];
  let want = 0;
  for (const name of names) {
    const i = legend.indexOf(name);
    if (i >= 0) want |= 1 << i;
  }
  return want;
}

/** Whether a film has every genre asked for. Nothing asked for is
 *  everything. */
export function hasGenres(genres: number, want: number): boolean {
  return (genres & want) === want;
}

/** The oldest and newest years this map holds. The year control's ends
 *  are the map's own, so a reader is never offered a decade the cast
 *  never worked in.
 *
 *  With the unrated column off, a year held only by unrated films is not
 *  a year the map can show, so it does not stretch the control either. */
export function yearBounds(
  payload: GridPayload,
  showUnrated = true,
): { lo: number; hi: number } {
  let lo = payload.anchor.year;
  let hi = payload.anchor.year;
  for (const [, year, rating] of payload.films) {
    if (!year) continue;
    if (!showUnrated && rating == null) continue;
    if (year < lo) lo = year;
    if (year > hi) hi = year;
  }
  return { lo, hi };
}

/** How many films this map holds in each year, for the histogram over
 *  the year slider. Every film on the spine counts, whatever the people,
 *  the floor or the range are doing: it is the shape of the whole map,
 *  so a reader can see where the work is before choosing years.
 *
 *  Counted by yearBounds' rule — no dateless films, and no unrated ones
 *  while the unrated column is off — so every bar has a year on the
 *  slider under it. The searched film always counts: it is always on
 *  the plot, and its year is always inside the bounds. */
export function yearCounts(payload: GridPayload, showUnrated = true): Map<number, number> {
  const out = new Map<number, number>();
  for (const [id, year, rating] of payload.films) {
    if (!year) continue;
    if (!showUnrated && rating == null && id !== payload.anchor.id) continue;
    out.set(year, (out.get(year) ?? 0) + 1);
  }
  return out;
}

/** A histogram bar's least height, and how much taller the busiest
 *  year's is, in pixels: a year with one film still shows, and none is
 *  taller than the space over the track. */
const HIST_MIN_H = 4;
const HIST_RANGE_H = 26;

interface HistBar {
  year: number;
  /** Where along the slider's track it stands, 0–1. */
  at: number;
  /** Its height in pixels: 4, plus up to 26 more for the busiest year. */
  h: number;
  /** Inside the chosen years, which draws it in the accent. */
  inRange: boolean;
}

/** The histogram's bars, oldest first: one for every year that holds a
 *  film, scaled against the busiest. `from` and `to` are where the
 *  thumbs are, so the bars change colour as a thumb passes them. */
export function histBars(
  counts: ReadonlyMap<number, number>,
  lo: number,
  hi: number,
  from: number,
  to: number,
): HistBar[] {
  let max = 0;
  for (const n of counts.values()) if (n > max) max = n;
  if (max === 0) return [];
  const span = Math.max(hi - lo, 1);
  return [...counts]
    .filter(([year, n]) => n > 0 && year >= lo && year <= hi)
    .sort(([a], [b]) => a - b)
    .map(([year, n]) => ({
      year,
      at: (year - lo) / span,
      h: Math.round(HIST_MIN_H + (n / max) * HIST_RANGE_H),
      inRange: year >= from && year <= to,
    }));
}

export interface GridSettings {
  yearOrder: 'oldest' | 'newest';
  showUnrated: boolean;
  highlightYear: boolean;
  /** Dim anything rated below this. Null lights everything. It never
   *  removes a card: the grid's whole argument is where a film sits on
   *  the scale, and a film that leaves the page cannot make it. */
  minRating: number | null;
  /** Inclusive; null is open on that side.
   *
   *  Years are the rows themselves, so cropping them takes nothing away
   *  from what the grid is claiming — unlike the rating floor, where a
   *  film's place on the scale is the whole point and removing it would
   *  be removing the argument.
   *
   *  Absolute years, not an offset from the searched film. The range
   *  belongs to the visit it was set on: another movie starts clear,
   *  and coming back restores it. */
  yearFrom: number | null;
  yearTo: number | null;
  /** Collapse rows where nothing is lit. Applies to every filter. */
  hideEmptyYears: boolean;
  /** Light only films with every one of these genres, named as the
   *  payload's legend names them, in the order they were picked. Like
   *  the years, it belongs to the visit: another movie starts clear, and
   *  coming back restores it. */
  genres: string[];
}

export const DEFAULT_SETTINGS: GridSettings = {
  yearOrder: 'oldest',
  showUnrated: true,
  highlightYear: true,
  minRating: null,
  yearFrom: null,
  yearTo: null,
  hideEmptyYears: false,
  genres: [],
};

/** Settings a stored string, read back. Anything missing takes its
 *  default.
 *
 *  A string rather than the storage itself, so it can be tested without
 *  one and so a blocked localStorage is the caller's problem. */
export function settingsFrom(raw: string | null): GridSettings {
  if (!raw) return DEFAULT_SETTINGS;
  try {
    const parsed: unknown = JSON.parse(raw);
    if (!parsed || typeof parsed !== 'object') return DEFAULT_SETTINGS;
    return { ...DEFAULT_SETTINGS, ...(parsed as Partial<GridSettings>) };
  } catch {
    return DEFAULT_SETTINGS;
  }
}

/** The rungs the rating filter offers. Whole and half points, because a
 *  reader thinks in "at least a seven", not in decimals. */
export const RATING_STOPS = [6, 6.5, 7, 7.5, 8, 8.5] as const;

/** What a rung says: "Any" for no floor, otherwise the floor with a plus
 *  ("7.0+"), because it lights that rating and everything above it. The
 *  header's rungs, the View panel's and the filter pill all say it the
 *  same way. */
export function rungLabel(floor: number | null): string {
  return floor == null ? 'Any' : `${floor.toFixed(1)}+`;
}

/** The rating domain is fixed rather than taken from the data, so the
 *  same rating sits in the same place on every map. */
export const R_LO = 3.5;
export const R_HI = 9.2;

/** Gap between cards, on both axes. */
export const GAP = 6;

/** The height of the rating strip along the top of the plot. The rows
 *  start this far down, and the strip is pulled back over them with a
 *  negative margin so the bands and cards slide underneath it rather
 *  than stopping at a gutter.
 *
 *  It used to be zero, on the theory that the gridlines say the rating
 *  by themselves. They do not once the map has been panned sideways —
 *  which on a phone is the only way to reach the high end at all.
 *
 *  26, the handoff's bar: its 11px labels sit 7px down, with room under
 *  them before the bottom rule. .cd-axis draws it at this height. */
export const AXIS_H = 26;

/** How far right a card may be nudged to join a lane before a new lane is
 *  opened. Beyond this the card would be lying about its rating. */
export const NUDGE_RATIO = 0.25;

interface Metrics {
  /** The small card: a phone's, and a landscape phone's. */
  compact: boolean;
  cardW: number;
  cardH: number;
  /** The poster down the card's left side. It fills the card's height
   *  inside its 6px padding, so it is taller than 2:3 — 50×78 on the
   *  large card, 40×60 on the compact one — and is cropped to fit. */
  posterW: number;
  posterH: number;
  railW: number;
  plotW: number;
  unratedW: number;
  titleLines: number;
  /** x of R_LO and R_HI, at card centres. */
  left: number;
  right: number;
}

/** The card, the year rail and the plot, for a scroller this wide.
 *
 *  `compact` is the screen's call, not the width's: a landscape phone is
 *  as wide as a small tablet, and still gets the phone's card, because it
 *  has a phone's height to fit rows into. Left out, it falls back to the
 *  width alone, which is right for everything but a landscape phone. */
export function metricsFor(width: number, s: GridSettings, compact = width < 640): Metrics {
  const posterW = compact ? 40 : 50;
  const cardW = compact ? 132 : 168;
  const cardH = compact ? 72 : 90;
  const posterH = cardH - 12;
  const railW = compact ? 52 : 72;
  const plotW = Math.max(width, compact ? 820 : 980);
  const unratedW = s.showUnrated ? cardW + 16 : 0;
  const left = railW + unratedW + cardW / 2 + 10;
  const right = plotW - cardW / 2 - 16;
  return {
    compact,
    cardW,
    cardH,
    posterW,
    posterH,
    railW,
    plotW,
    unratedW,
    // Two lines everywhere. Even the compact card has room: the title
    // sits beside a 60px poster, and two lines of 13px at 1.22 come to
    // under 32px, which leaves the foot row its line below them.
    titleLines: 2,
    left,
    right,
  };
}

export function clampRating(r: number): number {
  return Math.min(Math.max(r, R_LO), R_HI);
}

/** The x of a rating, at the card's centre. */
export function xOf(r: number, m: Metrics): number {
  return m.left + ((clampRating(r) - R_LO) / (R_HI - R_LO)) * (m.right - m.left);
}

interface GridLine {
  rating: number;
  label: string;
  x: number;
  labelLeft: number;
}

/** The width of a rating label's box on the axis. It is centred on its
 *  gridline, so its left edge is half of this before the line. "9.0" at
 *  11px is about 17px, so the box has room either side of it. */
export const AXIS_LABEL_W = 28;

/** A gridline at every whole rating the axis can show. */
export function gridLines(m: Metrics): GridLine[] {
  const out: GridLine[] = [];
  for (let r = 4; r <= 9; r++) {
    const x = Math.round(xOf(r, m));
    out.push({ rating: r, label: r.toFixed(1), x, labelLeft: x - AXIS_LABEL_W / 2 });
  }
  return out;
}

export interface Placed {
  film: SpineFilm;
  left: number;
  top: number;
  lane: number;
}

export interface Row {
  year: number;
  top: number;
  height: number;
  lanes: number;
  decade: boolean;
  anchorYear: boolean;
  /** Index among rendered rows, for the alternating band. */
  index: number;
  /** The gap between the searched film's row and the range the reader
   *  asked for, when the two are not next to each other. It holds no
   *  cards and carries no year: it is the years that are missing. */
  isBreak?: boolean;
}

export interface GridLayout {
  metrics: Metrics;
  rows: Row[];
  cards: Placed[];
  lines: GridLine[];
  plotW: number;
  plotH: number;
  unratedEdge: number;
  /** Where the axis's "IMDb rating →" starts: 10px past the unrated
   *  column's edge, so it names the scale from where the scale begins.
   *  With the column off that edge is the rail's, and the words take the
   *  place "Unrated" had. */
  axisTitleLeft: number;
  anchor: Placed | null;
}

/** The gap between the unrated column's edge and "IMDb rating →". */
const AXIS_TITLE_GAP = 10;

/** How far down its row a year's label sits on the rail. A decade is set
 *  in Young Serif at 16px and every other year in Figtree at 12px (13px
 *  for the searched one), so the decade starts higher to put the two on
 *  much the same line. The break row's "· · ·" sits where a decade would. */
export function railLabelTop(row: Row): number {
  return row.top + (row.decade || row.isBreak ? 8 : 11);
}

/** Where the searched card's "Searched" tag goes: 10px in from the
 *  card's left edge and 10px above its top, so it sits on the card's
 *  ring. It is drawn beside the card, not inside it, because the card
 *  clips what overflows it. The copy that lands on the card carries the
 *  same tag in the same place, from a box of its own at 0, 0. */
export function searchedTagAt(anchor: { left: number; top: number }): {
  left: number;
  top: number;
} {
  return { left: anchor.left + 10, top: anchor.top - 10 };
}

/** A row's identity from one layout to the next: its year, or the break,
 *  which carries no year. A film gives the key of the row it is in. */
export function rowKey(row: Pick<Row, 'year' | 'isBreak'>): string {
  return row.isBreak ? 'break' : String(row.year);
}

/** Where a year that is leaving closes to, when hiding or showing the
 *  empty years moves the rows: the new top of the next row below it that
 *  stays or, with none staying below it, the new bottom of the last row
 *  above it that stays. In the new layout's coordinates. A card leaving
 *  a year that stays goes by the same rule, which takes it to the foot
 *  of its own row while a row below it stays. `was` and `now` are the
 *  two layouts' rows, top to bottom. */
export function seamLeaving(key: string, was: Row[], now: Row[]): number {
  const kept = new Map(now.map((r) => [rowKey(r), r]));
  const i = was.findIndex((r) => rowKey(r) === key);
  for (let j = i + 1; i >= 0 && j < was.length; j++) {
    const k = kept.get(rowKey(was[j]));
    if (k) return k.top;
  }
  for (let j = i - 1; j >= 0; j--) {
    const k = kept.get(rowKey(was[j]));
    if (k) return k.top + k.height;
  }
  return 0;
}

/** Where a year that is coming back opens out of: the old top of the
 *  next row below it that was already showing or, with none below it,
 *  the old bottom of the row above it. Each is moved on by `shift`, what
 *  the scroll moved by, so it is the spot on screen where the two rows
 *  met, in the new layout's coordinates. A card arriving in a year that
 *  was already showing goes by the same rule. */
export function seamArriving(key: string, was: Row[], now: Row[], shift: number): number {
  const showing = new Map(was.map((r) => [rowKey(r), r]));
  const i = now.findIndex((r) => rowKey(r) === key);
  for (let j = i + 1; i >= 0 && j < now.length; j++) {
    const k = showing.get(rowKey(now[j]));
    if (k) return k.top + shift;
  }
  for (let j = i - 1; j >= 0; j--) {
    const k = showing.get(rowKey(now[j]));
    if (k) return k.top + k.height + shift;
  }
  return shift;
}

/** The band of cards to have ready: the screen the reader is on, plus
 *  the same amount above it and below it. `viewH` is that screen, so a
 *  phone and a monitor warm different distances and the rule is the same.
 *  Wherever they have scrolled to, the next screen in either direction is
 *  already drawn. */
export function warmSpan(scrollTop: number, viewH: number): { top: number; bottom: number } {
  const screen = Math.max(viewH, 0);
  const at = Math.max(scrollTop, 0);
  return { top: at - screen, bottom: at + screen * 2 };
}

/** How far the reader can scroll before a new band of cards is mounted.
 *  Well inside the screen that is already warm, so the mount happens
 *  before those cards reach the glass. The map and the Daily's board
 *  both move their warm band by it. */
export const WARM_STEP = 64;

/** The top of the screen in the plot, `lift` being how far down the
 *  scroller the plot starts, in the steps the warm band moves by. */
export function warmTop(scrollTop: number, lift: number): number {
  return Math.floor(Math.max(0, scrollTop - lift) / WARM_STEP) * WARM_STEP;
}

/** A year's band across the plot: striped every other row, lit for the
 *  searched film's year, ruled for a decade; or the break's. The map and
 *  the Daily's board draw the same bands, so a modifier added here
 *  reaches both. */
export function bandClass(r: Row): string {
  if (r.isBreak) return 'cd-band cd-band-break';
  return `cd-band${r.index % 2 === 1 ? ' cd-band-odd' : ''}${r.anchorYear ? ' cd-band-anchor' : ''}${r.decade ? ' cd-band-decade' : ''}`;
}

/** A year's label on the rail: a decade's numeral larger, the searched
 *  film's year lit. For the map and the Daily's board alike. */
export function railYearClass(r: Row): string {
  return `cd-rail-year${r.decade ? ' cd-rail-decade' : ''}${r.anchorYear ? ' cd-rail-anchor' : ''}`;
}

/** Whether a card's box meets `span`. A card that only just crosses the
 *  edge still counts: half a poster is how a scroll should arrive. */
export function inWarmSpan(
  top: number,
  height: number,
  span: { top: number; bottom: number },
): boolean {
  return top + height > span.top && top < span.bottom;
}

/** Bottom breathing room, so the last row can always be scrolled clear
 *  of the floating buttons rather than sitting under them. */
const BOTTOM_PAD = 110;

/** Extra room above a row whose year is more than one after the last. */
const GAP_MARK = 10;

/** The height of the break row between the searched film and a year
 *  range that does not hold it. */
const BREAK_H = 28;

/** Whether a year is inside the reader's range. Either end may be open,
 *  and both open is no range at all. */
function inYearRange(year: number, settings: GridSettings): boolean {
  if (settings.yearFrom != null && year < settings.yearFrom) return false;
  if (settings.yearTo != null && year > settings.yearTo) return false;
  return true;
}

/** Whether a film is on the plot at all: not taken off with the unrated
 *  column, and not cropped by the year range. The searched film always
 *  is — a map without the movie it is of is not a shorter map, it is a
 *  different one.
 *
 *  This is the layout's own test, so anything that has to agree with
 *  what the page holds asks it rather than keeping a copy. Whether a
 *  film on the plot is lit is a separate question: see `isLit`. */
export function onPlot(f: SpineFilm, settings: GridSettings): boolean {
  if (f.isAnchor) return true;
  if (!settings.showUnrated && f.rating == null) return false;
  return inYearRange(f.year, settings);
}

/** The films besides the searched one that the plot holds, lit or not:
 *  what hiding the empty years has to work with. When this is empty the
 *  year range or the unrated column has already left the searched film
 *  alone, and hiding empty years has nothing to hide. */
export function othersOnPlot(payload: GridPayload, settings: GridSettings): SpineFilm[] {
  return spineOf(payload).filter((f) => !f.isAnchor && onPlot(f, settings));
}

/** The films besides the searched one that the page holds and lights.
 *
 *  With empty years hidden these are exactly the other cards the layout
 *  draws, so counting them, or their years, counts what is on screen —
 *  not what the payload happens to hold. A film the unrated column or the
 *  year range has taken off the plot lights nothing, however well it
 *  would match. */
export function litOthers(
  payload: GridPayload,
  settings: GridSettings,
  selected: Set<number>,
): SpineFilm[] {
  const want = genreMask(payload, settings.genres);
  return othersOnPlot(payload, settings).filter((f) => isLit(f, selected, settings.minRating, want));
}

/** Whether the reader's year range holds none of this cast's other films,
 *  whatever their rating — which is why the map is down to the searched
 *  film, and something the View panel has to say.
 *
 *  Judged on the films themselves, not on the map's bounds: a range can
 *  sit wholly inside the years the cast worked and still fall in a gap
 *  between two of their films. No range set is no claim. */
export function rangeHoldsNone(payload: GridPayload, settings: GridSettings): boolean {
  if (settings.yearFrom == null && settings.yearTo == null) return false;
  return othersOnPlot(payload, { ...settings, showUnrated: true }).length === 0;
}

/** Whether hiding the empty years has left the searched film alone on
 *  the page — which is a real answer, but only if it is said.
 *
 *  Only when hiding them is what did it: there are other films on the
 *  plot, and none of them is lit. If the year range or the unrated
 *  column has left nothing else on the plot at all, hiding empty years
 *  emptied nothing — the same map with it off would be just as bare —
 *  and showing every year again would bring nothing back. */
export function aloneAfterHiding(
  payload: GridPayload,
  settings: GridSettings,
  selected: Set<number>,
): boolean {
  if (!settings.hideEmptyYears) return false;
  const want = genreMask(payload, settings.genres);
  const others = othersOnPlot(payload, settings);
  return others.length > 0 && !others.some((f) => isLit(f, selected, settings.minRating, want));
}

/** How many years on the plot hold nothing lit: what hiding the empty
 *  years takes away. The searched film's year always counts as lit. */
export function emptyYearCount(
  payload: GridPayload,
  settings: GridSettings,
  selected: Set<number>,
): number {
  const want = genreMask(payload, settings.genres);
  const lit = new Map<number, boolean>();
  for (const f of spineOf(payload)) {
    if (!onPlot(f, settings)) continue;
    lit.set(f.year, (lit.get(f.year) ?? false) || isLit(f, selected, settings.minRating, want));
  }
  let n = 0;
  for (const on of lit.values()) if (!on) n += 1;
  return n;
}

/** Whether a film is lit by what the reader has asked for.
 *
 *  The searched film always is: it is the centre of its own map. An
 *  unrated film clears no floor, because there is nothing to compare;
 *  a film needs every genre picked (`want`, from genreMask); with nobody
 *  selected, everyone counts.
 *
 *  A hovered chip is not part of this. A preview that reflowed the grid
 *  would move the cards out from under the pointer that asked for it. */
export function isLit(
  f: SpineFilm,
  selected: Set<number>,
  floor: number | null,
  want = 0,
): boolean {
  if (f.isAnchor) return true;
  if (!passesFloor(f.rating, floor)) return false;
  if (want && !hasGenres(f.genres, want)) return false;
  if (selected.size === 0) return true;
  return f.people.some((i) => selected.has(i));
}

export function layoutGrid(
  payload: GridPayload,
  width: number,
  settings: GridSettings = DEFAULT_SETTINGS,
  /** What counts as lit, for `hideEmptyYears`. Without it nothing is
   *  hidden, whatever the setting says — a caller that cannot judge a
   *  film should not be collapsing rows on a guess. */
  lit?: (f: SpineFilm) => boolean,
  /** The compact card, for a phone or a landscape phone. See metricsFor. */
  compact?: boolean,
): GridLayout {
  const m = metricsFor(width, settings, compact);
  // The rating floor is not a filter, it is a highlight: every film the
  // page holds is laid out, and the floor only decides what is lit. The
  // unrated column and the year range are a different thing — turning
  // the column off takes it off the plot, and rows outside the range are
  // removed rather than dimmed, so those films really do leave.
  let films = spineOf(payload).filter((f) => onPlot(f, settings));
  if (settings.hideEmptyYears && lit) {
    films = films.filter((f) => f.isAnchor || lit(f));
  }

  const byYear = new Map<number, SpineFilm[]>();
  for (const f of films) {
    const list = byYear.get(f.year);
    if (list) list.push(f);
    else byYear.set(f.year, [f]);
  }
  const years = [...byYear.keys()].sort((a, b) =>
    settings.yearOrder === 'newest' ? b - a : a - b,
  );
  // Where the searched film's year sits apart from the range, if it
  // does. It is at one end of the list, because a range is contiguous
  // and the anchor's year is outside it on one side or the other.
  const orphan =
    years.length > 1 &&
    payload.anchor.year > 0 &&
    !inYearRange(payload.anchor.year, settings)
      ? payload.anchor.year
      : null;

  const rows: Row[] = [];
  const cards: Placed[] = [];
  // Below the rating strip, which is pinned to the top of the scroller.
  let top = AXIS_H;
  let previous: number | null = null;

  for (const year of years) {
    // The gap the crop left, said in one row rather than by a number
    // the reader has to work out from two years that do not meet.
    // Either side: the searched film's row comes first when the rows
    // run oldest first and its year is below the range, and last when
    // they run the other way.
    if (orphan !== null && previous !== null && (year === orphan || previous === orphan)) {
      rows.push({
        year: 0,
        top,
        height: BREAK_H,
        lanes: 0,
        decade: false,
        anchorYear: false,
        index: rows.length,
        isBreak: true,
      });
      top += BREAK_H;
      // The break stands in for the jump, so the jump mark would be
      // saying the same thing twice.
      previous = year;
    }
    // A jump in the years is worth seeing, whichever way the rows run.
    if (previous !== null && Math.abs(year - previous) > 1) top += GAP_MARK;
    previous = year;

    // The whole axis runs one way. If the newest year is at the top,
    // the newest month inside it is too: otherwise time would run
    // backwards between years and forwards inside them.
    const inYear = [...byYear.get(year)!].sort(byDateThenId(settings.yearOrder === 'newest'));
    const lanes: number[] = [];
    const placedHere: Placed[] = [];
    // Date order, so January sits above December — and it holds for the
    // whole row, not only for cards that happen to collide. A card may
    // never take a lane above the one before it, which is what makes the
    // vertical position inside a year mean something. Every film the grid
    // holds is here already — that is what the spine is for — so a card
    // is placed once and there is never a later arrival to make room for.
    let floor = 0;
    for (const f of inYear) {
      const ideal =
        f.rating == null
          ? m.railW + 8
          : Math.round(xOf(f.rating, m) - m.cardW / 2);
      const { lane, left } = fitLane(lanes, ideal, m.cardW, floor);
      lanes[lane] = left + m.cardW;
      floor = lane;
      placedHere.push({ film: f, left, top: 0, lane });
    }

    const height = 12 + Math.max(lanes.length, 1) * (m.cardH + GAP) + 6;
    const rowTop = top;
    for (const p of placedHere) {
      cards.push({ ...p, top: rowTop + 12 + p.lane * (m.cardH + GAP) });
    }
    rows.push({
      year,
      top: rowTop,
      height,
      lanes: Math.max(lanes.length, 1),
      decade: year % 10 === 0,
      anchorYear: settings.highlightYear && year === payload.anchor.year,
      index: rows.length,
    });
    top += height;
  }

  return {
    metrics: m,
    rows,
    cards,
    lines: gridLines(m),
    plotW: m.plotW,
    plotH: top + BOTTOM_PAD,
    unratedEdge: m.railW + m.unratedW,
    axisTitleLeft: m.railW + m.unratedW + AXIS_TITLE_GAP,
    anchor: cards.find((c) => c.film.isAnchor) ?? null,
  };
}

/** What the reader has narrowed the map with, said in the header.
 *
 *  Nothing that hides content may be invisible: a year range or a
 *  collapsed empty year has to be named, or a map with rows gone
 *  looks broken.
 *
 *  The rating floor is only named here when the rungs are not in the
 *  header — on a desktop they already say it, and saying it twice is
 *  noise. The empty string means nothing is narrowed and there is no
 *  pill to draw. */
export function activeFilters(settings: GridSettings, rungsInView: boolean): string {
  const parts: string[] = [];
  if (rungsInView && settings.minRating != null) {
    parts.push(rungLabel(settings.minRating));
  }
  const { yearFrom: from, yearTo: to } = settings;
  if (from != null && to != null) parts.push(`${from}–${to}`);
  else if (from != null) parts.push(`From ${from}`);
  else if (to != null) parts.push(`To ${to}`);
  const genres = settings.genres;
  if (genres.length === 1) parts.push(genres[0]);
  else if (genres.length === 2) parts.push(`${genres[0]} & ${genres[1]}`);
  else if (genres.length > 2) parts.push(`${genres.length} genres`);
  // Hiding the empty years is not here. It filters no movie out — it
  // only closes up the rows between the ones already showing — and the
  // reader can see it has happened. A pill is for what is hidden.
  return parts.join(' · ');
}

/** The settings once the pill's ✕ has been pressed: everything the pill
 *  names is cleared, and nothing it does not: the years, the genres, and
 *  the floor where the pill shows it.
 *
 *  It used to clear the year window alone. On a phone, where the pill
 *  also names the rating floor, a pill that said only "6.5+" had an ✕
 *  that did nothing at all, and one that said "6.5+ · 2000–2010" left
 *  "6.5+" behind with the same dead ✕. What the pill says and what its
 *  ✕ clears are one rule, so they are decided together, here, beside
 *  `activeFilters`.
 *
 *  Hiding the empty years is left alone, because the pill does not name
 *  it; so is a floor the header's own rungs are showing. */
export function withoutPill(settings: GridSettings, rungsInView: boolean): GridSettings {
  return {
    ...settings,
    minRating: rungsInView ? null : settings.minRating,
    yearFrom: null,
    yearTo: null,
    genres: [],
  };
}

/** One toggle in the View panel's Genres section. */
interface GenreChoice {
  /** As the legend writes it: "Sci-Fi", "Film-Noir". */
  name: string;
  /** How many films on the plot have every genre picked and this one. */
  count: number;
  picked: boolean;
  /** Picking it would light nothing, so it cannot be picked. A picked
   *  genre never is: it can always be unpicked. */
  disabled: boolean;
  /** What the toggle says to a screen reader: "Sci-Fi, 12 movies". */
  label: string;
}

/** The Genres section's toggles, in the legend's order, so each genre
 *  keeps its place from map to map. Empty without a legend, and then
 *  there is no section.
 *
 *  A count is of the films on the plot — the unrated column and the year
 *  range decide that, and the searched film is always on it — that have
 *  every genre picked plus this one. The rating floor and the chosen
 *  people are left out, so a count says what the map holds rather than
 *  what the other filters happen to leave lit. */
export function genreChoices(payload: GridPayload, settings: GridSettings): GenreChoice[] {
  const legend = payload.genres ?? [];
  if (legend.length === 0) return [];
  const plot = spineOf(payload).filter((f) => onPlot(f, settings));
  const picked = genreMask(payload, settings.genres);
  return legend.map((name, i) => {
    const bit = 1 << i;
    const on = (picked & bit) !== 0;
    const want = picked | bit;
    let count = 0;
    let holding = 0;
    for (const f of plot) {
      if (hasGenres(f.genres, want)) count += 1;
      if (f.genres & bit) holding += 1;
    }
    const says =
      count > 0
        ? `${count} ${count === 1 ? 'movie' : 'movies'}`
        : holding === 0
          ? 'none on this map'
          : 'none with the genres picked';
    return { name, count, picked: on, disabled: !on && count === 0, label: `${name}, ${says}` };
  });
}

/** The settings with a genre picked, or unpicked if it was. Picks are
 *  kept in the order they were made, which is the order the pill names
 *  two of them in. */
export function toggleGenre(settings: GridSettings, name: string): GridSettings {
  const genres = settings.genres.includes(name)
    ? settings.genres.filter((g) => g !== name)
    : [...settings.genres, name];
  return { ...settings, genres };
}

/** How many settings differ from the defaults, for the View button.
 *
 *  A range counts once however many ends it has: the reader set one
 *  thing. So do the genres, however many are picked. The rating floor
 *  counts only where it is changed from — in the panel — so the number
 *  matches what opening the panel would show.
 */
export function changedCount(settings: GridSettings, rungsInView: boolean): number {
  let n = 0;
  if (settings.yearOrder !== DEFAULT_SETTINGS.yearOrder) n++;
  if (settings.showUnrated !== DEFAULT_SETTINGS.showUnrated) n++;
  if (settings.highlightYear !== DEFAULT_SETTINGS.highlightYear) n++;
  if (settings.yearFrom != null || settings.yearTo != null) n++;
  if (rungsInView && settings.minRating != null) n++;
  if (settings.genres.length > 0) n++;
  return n;
}

/** Whether a film clears the reader's rating floor. An unrated film
 *  clears no floor at all — there is nothing to compare — but with no
 *  floor asked for, everything is lit. */
export function passesFloor(rating: number | null, floor: number | null): boolean {
  if (floor == null) return true;
  return rating != null && rating >= floor;
}

/** Where a card sits inside its year. The spine carries the month and
 *  day as MMDD; a film whose date says only a year sits at the head of
 *  it, which is where an unknown month belongs. */
export function dateOrd(f: Pick<SpineFilm, 'year' | 'md'>): number {
  const md = f.md > 0 ? f.md : 101;
  return (f.year || 0) * 10000 + md;
}

/** Date order, then id. The spine has no titles — that is the point of
 *  it — so the tiebreak is the id, which is stable and does not change
 *  when the detail for a card arrives.
 *
 *  It follows the year axis: with the newest year at the top, the
 *  newest month in that year is at the top of it too, so time runs one
 *  way down the whole page. */
function byDateThenId(newerFirst: boolean) {
  return (a: SpineFilm, b: SpineFilm): number => {
    const d = dateOrd(a) - dateOrd(b);
    if (d !== 0) return newerFirst ? -d : d;
    // Two films of the same day still need one order, and an IMDb id is
    // roughly the order the record was made, which is as good a
    // tiebreak as any and is stable between renders.
    return a.id < b.id ? -1 : a.id > b.id ? 1 : 0;
  };
}




/** Finds a lane the card fits in at its honest x, or one it can reach with
 *  a nudge small enough that the card still reads at its own rating.
 *  Otherwise it opens a new lane and keeps x exactly. */
export function fitLane(
  lanes: number[],
  ideal: number,
  cardW: number,
  /** The lowest lane this card may take. Films are placed in date order,
   *  so passing the previous card's lane keeps the row reading top to
   *  bottom as January to December. Without it a card with a distinctive
   *  rating drops into an early lane and sits above films from months
   *  before it — an order the row appears to have and does not. */
  from = 0,
): { lane: number; left: number } {
  for (let i = from; i < lanes.length; i++) {
    if (ideal >= lanes[i] + GAP) return { lane: i, left: ideal };
  }
  const nudge = Math.round(cardW * NUDGE_RATIO);
  for (let i = from; i < lanes.length; i++) {
    if (lanes[i] + GAP - ideal <= nudge) return { lane: i, left: lanes[i] + GAP };
  }
  return { lane: lanes.length, left: ideal };
}

/** Whether a rating floor has left a lone selected person with nothing
 *  lit. The searched film always stays lit and is not part of the answer,
 *  and a person with no other film on the map is not claimed either.
 *
 *  Judged over the whole spine, which is the same ground the dimming
 *  itself stands on. It used to be the detail, which only holds the
 *  cards somebody has scrolled to — so a film of theirs that cleared the
 *  floor further down the page was not counted, and the toast said
 *  nothing of theirs did. */
export function nothingLit(
  spine: Iterable<SpineFilm>,
  /** The person, as a place in the chip row. */
  person: number,
  floor: number,
): boolean {
  let theirs = false;
  for (const film of spine) {
    if (film.isAnchor || !film.people.includes(person)) continue;
    theirs = true;
    if (passesFloor(film.rating, floor)) return false;
  }
  return theirs;
}

/** The longest a card ever waits to appear, and how much of its
 *  distance from the searched film it waits for. */
export const REVEAL_MAX_MS = 520;
const REVEAL_PER_PX = 0.35;

/** How long a card waits before it appears, so the map opens outward
 *  from the film that was searched for rather than all at once.
 *
 *  Every card is the same size, so the gap between two cards' corners is
 *  the gap between their centres. `after` holds the whole spread back —
 *  a map a flown card lands on starts spreading once the landing is
 *  under way — and never the searched film, which is already there. */
export function revealDelay(card: Placed, anchor: Placed | null, after = 0): number {
  if (!anchor || card.film.isAnchor) return 0;
  const d = Math.hypot(card.left - anchor.left, card.top - anchor.top);
  return after + Math.min(REVEAL_MAX_MS, Math.round(d * REVEAL_PER_PX));
}

/** The most marks a card draws. Past this it draws one fewer and says
 *  how many more there are: "+3". */
export const MAX_MARKS = 5;

/** A mark's swatch, square or round, 7px either way. */
const DOT = 7;

/** The card's foot row: rating, spacer, marks and "+N", 5px apart. */
const FOOT_GAP = 5;

/** Between a swatch and the initials beside it. */
const SWATCH_GAP = 3;

/** The card's own box, as the handoff draws it: 6px of padding all
 *  round, 9px between the poster and the words, and 2px kept clear at
 *  the right of the words. The room a foot row has is what these leave,
 *  so .cd-card must never give the words a narrower column than this:
 *  the marks would be measured for room the card does not have. */
const CARD_PAD = 6;
const CARD_GAP = 9;
const BODY_PAD_RIGHT = 2;

/** The foot row's type, as the handoff sets it: the rating at 12.5px,
 *  the initials and the "+N" at 10.5px, all Figtree 700. As with the
 *  box, the stylesheet may set it smaller, never larger. */
const RATING_PX = 12.5;
const MARK_PX = 10.5;

/** Kept free at the end of the row, for the table below reading a
 *  little narrow. Measured against Chrome, its worst letter is 0.005em
 *  short, which is about 0.42px across two four-letter codes at 10.5px,
 *  so half a pixel still keeps the last mark on the card. Any more and
 *  a row that does fit loses its initials: both Wachowskis on a
 *  desktop card leave under a pixel spare. */
const FOOT_SLACK = 0.5;

/** Figtree 700's advance widths, in ems. Digits and "+" are tabular,
 *  which is how every count and rating is set. A character missing
 *  from the table is taken to be as wide as a W, so a guess only ever
 *  errs towards fewer marks. */
const FIGTREE_700: Record<string, number> = {
  A: 0.71, B: 0.61, C: 0.72, D: 0.7, E: 0.59, F: 0.55, G: 0.74, H: 0.75, I: 0.29,
  J: 0.55, K: 0.67, L: 0.54, M: 0.86, N: 0.77, O: 0.79, P: 0.6, Q: 0.79, R: 0.64,
  S: 0.61, T: 0.58, U: 0.7, V: 0.72, W: 0.98, X: 0.68, Y: 0.65, Z: 0.63,
  a: 0.53, b: 0.6, c: 0.55, d: 0.6, e: 0.55, f: 0.39, g: 0.6, h: 0.57, i: 0.26,
  j: 0.29, k: 0.54, l: 0.25, m: 0.87, n: 0.57, o: 0.58, p: 0.6, q: 0.59, r: 0.38,
  s: 0.47, t: 0.4, u: 0.57, v: 0.56, w: 0.84, x: 0.54, y: 0.58, z: 0.48,
  '0': 0.632, '1': 0.632, '2': 0.632, '3': 0.632, '4': 0.632, '5': 0.632,
  '6': 0.632, '7': 0.632, '8': 0.632, '9': 0.632, '+': 0.632, '.': 0.25, ' ': 0.24,
  '?': 0.51,
};
const WIDEST = 0.98;

/** How wide a run of Figtree 700 is at this size. An accented letter is
 *  measured as the letter under the accent. */
export function textWidth(text: string, px: number): number {
  let em = 0;
  for (const ch of text) em += FIGTREE_700[ch] ?? FIGTREE_700[ch.normalize('NFD')[0]] ?? WIDEST;
  return em * px;
}

/** What a rating says on a card, in the hover preview and in the panel. */
export function ratingText(rating: number | null): string {
  return rating == null ? 'No rating' : rating.toFixed(1);
}

interface Markers {
  /** People to draw, in order. */
  show: string[];
  /** People counted in the "+N" rather than drawn. */
  extra: number;
  /** Initials beside the swatches. Only on a desktop or tablet card, only
   *  for one or two people, and only when they fit. */
  initials: boolean;
}

/** How wide a card's foot row is. */
export function footRoom(m: Metrics): number {
  return m.cardW - 2 * CARD_PAD - m.posterW - CARD_GAP - BODY_PAD_RIGHT;
}

/** How much of the foot row a set of marks takes, the rating included.
 *  The spacer between them is a flex item too, so the rating is followed
 *  by a gap before it and every mark by a gap before itself. */
export function footWidth(
  markers: Markers,
  rating: number | null,
  codes: ReadonlyMap<string, string> = new Map(),
): number {
  let w = textWidth(ratingText(rating), RATING_PX) + FOOT_GAP;
  for (const id of markers.show) {
    w += FOOT_GAP + DOT;
    if (markers.initials) w += SWATCH_GAP + textWidth(codes.get(id) ?? '?', MARK_PX);
  }
  if (markers.extra > 0) w += FOOT_GAP + textWidth(`+${markers.extra}`, MARK_PX);
  return w;
}

/** What a card shows of its people.
 *
 *  The handoff's rule is up to five marks, or four and "+N", with
 *  initials on the desktop and tablet card when there are one or two
 *  people. Its card clips whatever does not fit, and on a small card
 *  with a long rating that is a real row: an unrated phone card has no
 *  room for even one swatch. A mark cut in half says nothing, so the
 *  card steps down until the row fits: swatches rather than initials,
 *  then fewer swatches and a bigger "+N". The rating is what the card
 *  is placed by, so it keeps its room; when nothing else fits the card
 *  shows no marks, and the sheet still names everyone.
 *
 *  `codes` are the initials the card would print, so they can be
 *  measured; a person with none is drawn as "?". */
export function markersFor(
  people: string[],
  m: Metrics,
  rating: number | null,
  codes: ReadonlyMap<string, string> = new Map(),
): Markers {
  const none: Markers = { show: [], extra: 0, initials: false };
  const n = people.length;
  if (n === 0) return none;
  const room = footRoom(m) - FOOT_SLACK;
  const fits = (k: Markers) => footWidth(k, rating, codes) <= room;

  if (!m.compact && n <= 2) {
    const named: Markers = { show: people, extra: 0, initials: true };
    if (fits(named)) return named;
  }
  // Replacing one swatch with "+1" never saves room, so the loop goes on
  // to the next count down rather than stopping there. It stops at one
  // swatch: a "+12" with nothing before it reads as a rating, not as
  // twelve more people.
  for (let fit = n <= MAX_MARKS ? n : MAX_MARKS - 1; fit >= 1; fit--) {
    const dots: Markers = { show: people.slice(0, fit), extra: n - fit, initials: false };
    if (fits(dots)) return dots;
  }
  return none;
}

/** The initials a card shows for one person: first letter of the first
 *  name and of the last, "KR" for Keanu Reeves.
 *
 *  The spec says a collision should take the first two letters of the
 *  last name, but the reference design's own people break that: Lana and
 *  Lilly Wachowski both give "LWA". Lengthening the *first* name is what
 *  actually tells them apart, so a collision grows "LaW" and "LiW", and
 *  keeps growing until the codes differ or the names run out. */
export function initialsFor(people: GridPerson[]): Map<string, string> {
  const codes = new Map<string, string>();
  for (const p of people) codes.set(p.id, initials(p.name, 1));
  for (let take = 2; take <= 4; take++) {
    const clashing = collisions(people, codes);
    if (clashing.size === 0) break;
    for (const p of people) {
      if (clashing.has(codes.get(p.id)!)) codes.set(p.id, initials(p.name, take));
    }
  }
  return codes;
}

/** The codes more than one person is using. */
function collisions(people: GridPerson[], codes: Map<string, string>): Set<string> {
  const count = new Map<string, number>();
  for (const p of people) {
    const code = codes.get(p.id)!;
    count.set(code, (count.get(code) ?? 0) + 1);
  }
  return new Set([...count].filter(([, n]) => n > 1).map(([code]) => code));
}

/** `take` letters of the first name, then one of the last. */
function initials(name: string, take: number): string {
  const parts = name.trim().split(/\s+/).filter(Boolean);
  if (parts.length === 0) return '?';
  const first = parts[0].slice(0, take);
  const last = parts.length > 1 ? parts[parts.length - 1][0] : '';
  return (first.charAt(0).toUpperCase() + first.slice(1).toLowerCase() + last.toUpperCase()).trim();
}
