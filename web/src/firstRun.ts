import type { FirstRunHit, SearchHit } from './api';
import { HEADER_ROW_H, screenOf, type ScreenClass } from './screen';

export type FirstRunFilm = SearchHit & { year: number; c?: string };

/** The most tiles the cold screen ever shows: one per era. A short
 *  window shows fewer, so the set still fits under the header. */
export const COLD_MAX = 8;

/** The opening screen's geometry in each screen class: the numbers the
 *  stylesheet gives `.cd-cold`, `.cd-tiles` and the tiles' captions. */
interface ColdGeometry {
  /** Columns in `.cd-tiles`. A landscape phone lays all eight in a row. */
  columns: number;
  /** The grid's gap, and the most it grows to across. */
  gap: number;
  maxW: number;
  /** The grid's own top margin, and the bottom padding the last row must
   *  not sit under. */
  gridMargin: number;
  foot: number;
  /** A tile's caption under its 2:3 frame: the 9px between them, then
   *  the title and the year, each one line at a line-height of 1.2, with
   *  2px between them. */
  caption: number;
}

/** Title 13.5px and year 12px; on a landscape phone, 12 and 11. */
const CAPTION = 9 + 13.5 * 1.2 + 2 + 12 * 1.2;
const CAPTION_SHORT = 9 + 12 * 1.2 + 2 + 11 * 1.2;

const GEOMETRY: Record<ScreenClass, ColdGeometry> = {
  phone: { columns: 2, gap: 14, maxW: 640, gridMargin: 28, foot: 48, caption: CAPTION },
  short: { columns: 8, gap: 12, maxW: 820, gridMargin: 16, foot: 24, caption: CAPTION_SHORT },
  tablet: { columns: 4, gap: 18, maxW: 640, gridMargin: 28, foot: 48, caption: CAPTION },
  desktop: { columns: 4, gap: 18, maxW: 640, gridMargin: 28, foot: 48, caption: CAPTION },
};

/** The sides `.cd-cold` pads in by, and the gap between its lines. */
const SIDE = 20;
const COLUMN_GAP = 12;

/** The padding `.cd-cold` takes off the top: 36 on a phone, 18 on a
 *  landscape phone, and otherwise `clamp(28px, 6vh, 110px)`, or 9vh on a
 *  window 860px tall or more. */
export function coldTopPad(vw: number, vh: number): number {
  const cls = screenOf(vw, vh).cls;
  if (cls === 'phone') return 36;
  if (cls === 'short') return 18;
  return Math.min(Math.max(28, vh * (vh < 860 ? 0.06 : 0.09)), 110);
}

/** The headline and the sub-line, with the gaps after each, for when the
 *  grid has not been measured. The headline takes two lines on a phone,
 *  one on a landscape phone, and elsewhere one where it fits (Young Serif
 *  46 at 1.08 needs about 625px) and two where it does not. The sub-line
 *  is two lines at 16px in its 480px, and one at 14.5px on a landscape
 *  phone, where it has 640. Where in doubt it counts the extra line: a
 *  row too few leaves room to spare, a row too many is cut off. */
function introHeight(cls: ScreenClass, vw: number): number {
  if (cls === 'short') return 28 * 1.08 + COLUMN_GAP + 14.5 * 1.5 + COLUMN_GAP;
  const size = cls === 'phone' ? 32 : 46;
  const lines = cls === 'phone' || vw - SIDE * 2 < 640 ? 2 : 1;
  return lines * size * 1.08 + COLUMN_GAP + 2 * 16 * 1.5 + COLUMN_GAP;
}

/** How many tiles fit in the window as whole rows.
 *
 *  `gridTop` is the distance from the top of the window to the top of
 *  the tile grid, measured once the grid is on screen. It is what this
 *  should be given: the stand-in numbers below are a second copy of the
 *  stylesheet, and they once said 12, 24 and 52 while the CSS said 14,
 *  40 and about 90, so on a window around 700–760px tall this asked for
 *  a row that could not fit and the last one was cut off. They are the
 *  CSS's numbers now, and only used when there is no measurement. */
export function coldScreenCount(vw: number, vh: number, gridTop?: number): number {
  const cls = screenOf(vw, vh).cls;
  const { columns, gap, maxW, gridMargin, foot, caption } = GEOMETRY[cls];
  const innerW = Math.min(Math.max(0, vw - SIDE * 2), maxW);
  const tileW = (innerW - gap * (columns - 1)) / columns;
  const tileH = tileW * 1.5 + caption;
  const top =
    gridTop ?? HEADER_ROW_H[cls] + coldTopPad(vw, vh) + introHeight(cls, vw) + gridMargin;
  const available = vh - top - foot;
  if (!(tileH > 0) || available <= 0) return columns;
  const rows = Math.max(1, Math.floor((available + gap) / (tileH + gap)));
  return Math.min(COLD_MAX, columns * rows);
}

/** The films the cold screen can actually show, out of what the API
 *  sent: one tile per film, each with a picture and a year.
 *
 *  A long title is kept and left to the ellipsis. The catalog picks the
 *  movies, and "Indiana Jones and the Temple of Doom" is an ordinary
 *  answer: refusing it would empty the whole screen over one movie. */
export function tilesFrom(hits: FirstRunHit[], want = 8): FirstRunFilm[] {
  const seen = new Set<string>();
  const out: FirstRunFilm[] = [];
  for (const h of hits) {
    if (!h.poster || !h.year || !h.title) continue;
    if (seen.has(h.id)) continue;
    seen.add(h.id);
    out.push({
      id: h.id,
      title: h.title,
      year: h.year,
      poster: h.poster,
      c: h.c,
    });
  }
  return out.slice(0, want);
}

/** Posters asked for ahead of the rest. The first row is what a reader
 *  looks at first, and a browser given eight equal requests will not
 *  guess which. */
export const EAGER_TILES = 4;

