// A colour for each person, so a card can say who links it to the
// searched film without anyone having to read a name.
//
// Colour means the person and shape means the role: a circle for cast,
// a square for a director. A person keeps their colour for as long as
// the page is open, so someone met on one map is the same colour on the
// next one, which is how the eye follows them from map to map.
//
// Everything here is pure apart from the one session-wide table, and
// every function that reads or writes it takes the table as an optional
// argument, so a test can bring its own.

import type { CSSProperties } from 'react';
import type { GridPerson } from './grid';
import type { Theme } from './theme';

/** The hues people are given, in the order they are met. Sixteen, so a
 *  map with more people than that repeats a colour: the handoff's rule,
 *  and the cost of never changing one already given. */
export const HUES = [205, 232, 78, 28, 345, 150, 118, 255, 180, 52, 5, 128, 95, 165, 40, 62] as const;

/** Person id to a place in HUES, for the whole visit. It is filled as
 *  maps are shown and never emptied: going home and starting again is
 *  still the same visit, and the people met so far keep their colours. */
const SESSION = new Map<string, number>();

/** Gives everyone on a map who has no colour yet the next one, in the
 *  order the map lists them (directors, then the billed cast).
 *
 *  Nobody who already has a colour is touched, so running it twice for
 *  the same map changes nothing. That is what makes it safe to call
 *  while rendering: a second render of the same map gives the same
 *  answer as the first. */
export function assignHues(
  people: readonly { id: string }[],
  into: Map<string, number> = SESSION,
): void {
  for (const p of people) {
    if (!into.has(p.id)) into.set(p.id, into.size % HUES.length);
  }
}

/** A person's hue. Anyone not met yet gets the first one, which only a
 *  card drawn ahead of its own map could ever ask for. */
export function hueFor(id: string, from: ReadonlyMap<string, number> = SESSION): number {
  return HUES[from.get(id) ?? 0];
}

/** The colour a person is drawn in. Lighter and softer on the dark
 *  ground, darker and stronger on paper, so it reads on both.
 *
 *  It goes into an inline custom property, never the stylesheet: the
 *  build rewrites an oklch() it finds in a stylesheet as hex. */
export function personColour(
  id: string,
  theme: Theme,
  from: ReadonlyMap<string, number> = SESSION,
): string {
  const h = hueFor(id, from);
  return theme === 'light' ? `oklch(0.55 0.15 ${h})` : `oklch(0.78 0.12 ${h})`;
}

/** The corner a person's swatch has: round for cast, a square with
 *  softened corners for a director. */
export function swatchRadius(role: GridPerson['role']): string {
  return role === 'director' ? '2px' : '50%';
}

/** What an element drawing this person needs: their colour as --tone,
 *  and their swatch's corner as --swatch-r. The stylesheet does the
 *  rest with var(). */
export function personVars(
  p: Pick<GridPerson, 'id' | 'role'>,
  theme: Theme,
  from: ReadonlyMap<string, number> = SESSION,
): CSSProperties {
  return {
    ['--tone' as string]: personColour(p.id, theme, from),
    ['--swatch-r' as string]: swatchRadius(p.role),
  };
}

/** The people on the map arriving who were also on the map just shown.
 *  Nobody is carried from the opening screen, which has no map. */
export function carriedFrom(
  shown: readonly string[] | null,
  next: readonly { id: string }[],
): Set<string> {
  if (!shown) return new Set();
  const was = new Set(shown);
  return new Set(next.filter((p) => was.has(p.id)).map((p) => p.id));
}

/** The chip row's order: the people carried over from the last map
 *  first, so the reader finds the ones they were following where they
 *  left them, then everyone else. Each group keeps the map's own order.
 *
 *  A copy, and only for drawing. The map's own list must never be
 *  reordered: the spine names people by their place in it. */
export function carriedFirst<T extends { id: string }>(
  people: readonly T[],
  carried: ReadonlySet<string>,
): T[] {
  if (carried.size === 0) return [...people];
  return [...people.filter((p) => carried.has(p.id)), ...people.filter((p) => !carried.has(p.id))];
}

/** The map the reader last saw, and who of it the map on screen shares. */
interface Shown {
  /** The searched film of the map last shown; null on the opening screen. */
  id: string | null;
  /** Its people's ids, to compare the next map against. */
  people: readonly string[] | null;
  /** Who of the map on screen was also on the one shown before it. */
  carried: ReadonlySet<string>;
}

export const NOTHING_SHOWN: Shown = { id: null, people: null, carried: new Set() };

/** What `Shown` becomes for this render. The same object when nothing
 *  has changed, so it can be set while rendering without a loop.
 *
 *  A new map is judged against the last one actually shown: a load in
 *  progress, or one that failed, is not a map the reader saw, so it
 *  carries nothing and replaces nothing. Going home forgets it, as the
 *  opening screen has no map. Home is judged first because the old map
 *  is still in hand for a render after the route has gone home, and it
 *  must not be taken for a new one. */
export function nextShown(
  was: Shown,
  movieId: string | null,
  map: { anchor: { id: string }; people: readonly { id: string }[] } | null,
): Shown {
  if (movieId === null) return was.id === null ? was : NOTHING_SHOWN;
  if (!map || map.anchor.id === was.id) return was;
  return {
    id: map.anchor.id,
    people: map.people.map((p) => p.id),
    carried: carriedFrom(was.people, map.people),
  };
}
