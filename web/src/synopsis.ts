// The film panel's synopsis: how many lines it rests at, when it offers
// More, and how it makes room for a trailer opening under it. Kept apart
// from the component, as numbers and small functions, so each rule is
// written once and can be checked without a DOM. Every place here is in
// the coordinates of the panel's body, the part of the panel that
// scrolls, measured from the top of its content.

import { PLAYER, TRAILER_CLOSE_MS, TRAILER_OPEN_MS } from './trailer';

/** One line of the synopsis: 15px at a line height of 1.55. */
export const SYN_LH = 23.25;
/** The most lines it rests at, and the fewest, however short the panel. */
export const SYN_MOST = 4;
export const SYN_FEWEST = 2;
/** The More / Less row under it: a 24px line with a 4px gap above. */
export const MORE_ROW_H = 28;
/** The gap between the synopsis and the trailer row. */
export const ABOUT_GAP = 14;
/** The trailer row: Watch trailer, its placeholder, or the player's
 *  controls. */
export const TRAILER_ROW_H = 44;
/** The room kept under the trailer row when the synopsis rests. */
const REST_ROOM = 16;
/** How far above the body's bottom edge an open video ends. */
const VIDEO_ROOM = 12;
/** How far below the body's top edge the controls row may be scrolled. */
const ROW_ROOM = 8;

/** More opens the rest on the glide curve; Less folds it back, easing
 *  in and out. */
export const MORE_MS = 360;
export const LESS_MS = 300;
/** Less keeps the line clamp off until its fold is done, and a little
 *  over, so the ellipsis appears without a jump. */
export const LESS_SETTLE_MS = 320;

/** How many lines the synopsis rests at: as many as leave Watch trailer
 *  in view without a scroll, More's row and the gap above the trailer
 *  row counted whether or not More shows, and never more than four or
 *  fewer than two. `bodyH` is the body's visible height; `synTop` is the
 *  synopsis's top. */
export function restingLines(bodyH: number, synTop: number): number {
  const room = bodyH - REST_ROOM - synTop - MORE_ROW_H - ABOUT_GAP - TRAILER_ROW_H;
  return Math.min(SYN_MOST, Math.max(SYN_FEWEST, Math.floor(room / SYN_LH)));
}

/** Whether text this tall, drawn whole, has more lines than it rests at,
 *  which is when it offers More. */
export function overflows(fullH: number, lines: number): boolean {
  return Math.round(fullH / SYN_LH) > lines;
}

/** How much further down an opening video would end than it may: the
 *  trailer row, the player's gap and the video, against the body's
 *  visible bottom less its margin. Nothing needs to move at 0 or under. */
export function trailerNeed(slotTop: number, VH: number, scrollTop: number, bodyH: number): number {
  return slotTop + TRAILER_ROW_H + PLAYER.panel.gap + VH - (scrollTop + bodyH - VIDEO_ROOM);
}

/** The most lines, at most `lines`, the synopsis can keep and still give
 *  back `need`, from the `shown` pixels it takes now (expanded text
 *  included). 0 when even one line keeps too much, and it folds away. */
export function foldLines(shown: number, need: number, lines: number): number {
  for (let L = lines; L >= 1; L--) if (shown - L * SYN_LH >= need) return L;
  return 0;
}

/** What folding to `L` lines gives back. Folded away, that is the whole
 *  synopsis, More's row when it has one, and the gap above the trailer
 *  row, which the synopsis's block cancels. */
export function foldSaves(shown: number, L: number, over: boolean): number {
  return L > 0 ? shown - L * SYN_LH : shown + (over ? MORE_ROW_H : 0) + ABOUT_GAP;
}

/** How far the body scrolls to find the `left` pixels folding could not:
 *  never so far that the controls row, at `rowTop` once the synopsis has
 *  folded, goes nearer the body's top edge than ROW_ROOM. */
export function scrollLift(left: number, rowTop: number, scrollTop: number): number {
  return Math.min(left, Math.max(0, rowTop - ROW_ROOM - scrollTop));
}

/** The panel as it stands when its trailer is asked for. */
export interface PanelNow {
  /** The trailer row's top. */
  slotTop: number;
  /** The video's height, from videoHeight. */
  VH: number;
  /** The body's scroll position and its visible height. */
  scrollTop: number;
  bodyH: number;
  /** The synopsis's height as shown, expanded text included; null when
   *  the film has none. */
  shown: number | null;
  /** The lines it rests at, and whether it offers More. */
  lines: number;
  over: boolean;
}

/** How the panel makes room for a trailer opening, in this order,
 *  stopping as soon as the whole video fits in the body: the synopsis
 *  folds line by line (an expanded one collapsing first), then away,
 *  and only then does the body scroll, as far as the controls row
 *  allows. `synLines` is how many lines the synopsis folds to (0 for
 *  none at all), or null when nothing needs to move; `lift` is how far
 *  the body scrolls. */
export function roomForTrailer(now: PanelNow): { synLines: number | null; lift: number } {
  const need = trailerNeed(now.slotTop, now.VH, now.scrollTop, now.bodyH);
  if (need <= 0) return { synLines: null, lift: 0 };
  let synLines: number | null = null;
  let saved = 0;
  if (now.shown != null) {
    synLines = foldLines(now.shown, need, now.lines);
    saved = foldSaves(now.shown, synLines, now.over);
  }
  if (saved >= need) return { synLines, lift: 0 };
  return { synLines, lift: scrollLift(need - saved, now.slotTop - saved, now.scrollTop) };
}

/** What last changed the synopsis, which its transition is timed for:
 *  More, Less, a trailer opening or closing, or nothing that moves (a
 *  measurement, or a reader who has asked for nothing to move). */
export type SynChange = 'still' | 'more' | 'less' | 'open' | 'close';

/** The synopsis's transition for each change, as --syn-tr. The trailer's
 *  are the player's own, so the fold and the player move as one. */
export const SYN_TR: Record<SynChange, string> = {
  still: '0s',
  more: `${MORE_MS}ms var(--ease-glide)`,
  less: `${LESS_MS}ms var(--ease-close)`,
  open: `${TRAILER_OPEN_MS}ms var(--ease-glide)`,
  close: `${TRAILER_CLOSE_MS}ms var(--ease-close)`,
};

/** The ease-out the body scrolls on, from 0 to 1 over `k`. */
export function scrollEase(k: number): number {
  return 1 - Math.pow(1 - k, 3);
}
