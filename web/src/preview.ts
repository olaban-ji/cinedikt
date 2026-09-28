// The map's hover preview: where it goes beside a card, how it makes
// room for a trailer opening inside it, and when it opens and closes.
// Kept apart from the component, as numbers and small functions, so
// each rule is written once and can be checked without a DOM. Every
// place here is in plot coordinates: the ones the cards are placed in,
// which start at the top of the plot inside the map's scroller.

/** How wide the preview is. */
export const PREVIEW_W = 360;
/** The gap between the preview and its card. */
export const PREVIEW_GAP = 8;
/** How far it keeps from the year rail on its left. */
export const PREVIEW_RAIL_GAP = 4;
/** How far it keeps from the map's right and bottom edges. */
export const PREVIEW_EDGE = 12;
/** How far below the map's top edge its top stays: the 26px pinned
 *  rating axis and 8 more. */
export const PREVIEW_TOP = 34;
/** The same, once an open trailer has had to rise over the axis to fit. */
export const PREVIEW_TOP_LOOSE = 8;
/** The room below a card that is always enough to grow down into. */
export const PREVIEW_ROOM = 300;

/** One line of the synopsis: 13.5px at a line height of 1.5. */
export const SYN_LINE_H = 20.25;
/** The most lines of it the preview shows. */
export const SYN_LINES = 6;
/** The box's gap, which a synopsis folded away entirely takes back. */
export const SYN_GAP = 12;
/** How long the synopsis takes to fold, and a little over: its line
 *  clamp, with the ellipsis, moves only after this, so nothing snaps. */
export const SYN_SETTLE_MS = 540;

/** A pointer resting this long on a card opens its preview. */
export const PREVIEW_REST_MS = 480;
/** With a preview showing, or one just closed, the next opens after this. */
export const PREVIEW_SWAP_MS = 90;
/** How recently a preview has to have closed for the next to count as a swap. */
export const PREVIEW_WARM_MS = 350;
/** A pointer that has left both the card and the preview closes it after this. */
export const PREVIEW_LEAVE_MS = 160;
/** The same while its trailer plays, so a slip off the edge does not stop it. */
export const PREVIEW_LEAVE_PLAYING_MS = 450;

/** What of the map's scroller the preview is placed against. */
export interface MapView {
  scrollLeft: number;
  scrollTop: number;
  clientWidth: number;
  clientHeight: number;
}

/** The room the preview has, in plot coordinates: its left edge between
 *  `x0` and `x1 - PREVIEW_W`, and its box between `vt` and `vb`. */
export interface PreviewBounds {
  x0: number;
  x1: number;
  vt: number;
  vb: number;
}

/** The room on the glass right now. The plot starts `overlayH` down the
 *  scroller, below a header lying over it; `pinLift` is how far down
 *  the map's visible top edge is, the header's height while it is
 *  showing and 0 once it has gone up (the same as the pinned year
 *  labels' --pin-lift). `loose` lets a preview that an open trailer has
 *  grown rise over the pinned rating axis. */
export function previewBounds(
  view: MapView,
  railW: number,
  overlayH: number,
  pinLift: number,
  loose = false,
): PreviewBounds {
  const top = view.scrollTop - overlayH;
  return {
    x0: view.scrollLeft + railW + PREVIEW_RAIL_GAP,
    x1: view.scrollLeft + view.clientWidth - PREVIEW_EDGE,
    vt: top + pinLift + (loose ? PREVIEW_TOP_LOOSE : PREVIEW_TOP),
    vb: top + view.clientHeight - PREVIEW_EDGE,
  };
}

/** Where a preview sits: its left edge, which side of the card it is on
 *  (1 to the right, -1 to the left), and the one edge it is held by. It
 *  grows down from `top`, or up from `bottom`, which is measured up from
 *  the foot of the plot, as CSS measures it. */
export interface PreviewPlace {
  x: number;
  side: 1 | -1;
  top: number | null;
  bottom: number | null;
}

/** Beside the card: to its right, else to its left, else on whichever
 *  side has more room, then held inside the map. Down from the card's
 *  top when there is plenty of room below it, or more below than above;
 *  otherwise up from its bottom. */
export function placePreview(
  card: { left: number; top: number },
  size: { cardW: number; cardH: number },
  b: PreviewBounds,
  plotH: number,
): PreviewPlace {
  const rx = card.left + size.cardW + PREVIEW_GAP;
  const lx = card.left - PREVIEW_GAP - PREVIEW_W;
  const side: 1 | -1 =
    rx + PREVIEW_W <= b.x1 ? 1 : lx >= b.x0 ? -1 : b.x1 - rx >= card.left - b.x0 ? 1 : -1;
  const x = Math.round(Math.max(b.x0, Math.min(side > 0 ? rx : lx, b.x1 - PREVIEW_W)));
  const below = b.vb - card.top;
  const above = card.top + size.cardH - b.vt;
  if (below >= PREVIEW_ROOM || below >= above) {
    return { x, side, top: Math.max(card.top, b.vt), bottom: null };
  }
  return { x, side, top: null, bottom: plotH - Math.min(card.top + size.cardH, b.vb) };
}

/** The preview as it stands when its trailer is asked for. */
export interface PreviewNow {
  /** Its top, as it was before any trailer moved it. */
  y0: number;
  /** Its height now. */
  height: number;
  /** How much taller the open trailer makes it: the well under the row,
   *  less the bottom padding the video takes over. */
  grow: number;
  /** The synopsis's height, or null when there is no synopsis line. */
  synH: number | null;
}

/** How a preview makes room for its trailer, in this order, stopping as
 *  soon as it fits: its top glides up, just far enough; it rises over
 *  the pinned rating axis; the synopsis folds to fewer lines, or away
 *  entirely. `top` is where its top glides to; `synLines` is how many
 *  lines the synopsis folds to (0 for none), or null when it keeps them
 *  all. Its bottom always stays inside the strict bounds. */
export function fitTrailer(
  now: PreviewNow,
  strict: PreviewBounds,
  loose: PreviewBounds,
): { top: number; synLines: number | null } {
  let grown = now.height + now.grow;
  let top = strict.vt;
  if (grown > strict.vb - top) top = loose.vt;
  let synLines: number | null = null;
  if (grown > strict.vb - top && now.synH != null) {
    const shown = Math.round(now.synH / SYN_LINE_H);
    const rest = grown - now.synH;
    synLines = 0;
    for (let lines = shown - 1; lines >= 1; lines--) {
      if (rest + lines * SYN_LINE_H <= strict.vb - top) {
        synLines = lines;
        break;
      }
    }
    grown = synLines > 0 ? rest + synLines * SYN_LINE_H : rest - SYN_GAP;
  }
  // In whole pixels, each rounded the way that keeps its bound.
  return { top: Math.max(Math.ceil(top), Math.min(now.y0, Math.floor(strict.vb - grown))), synLines };
}

/** A preview held inside the map once its height is known. It grows
 *  from the edge placePreview chose, which keeps that edge inside the
 *  bounds; a box taller than the room on that side would take its other
 *  edge past them, and so is moved back in, as far as the room allows,
 *  held by its top from then on. */
export function holdInside(place: PreviewPlace, height: number, b: PreviewBounds, plotH: number): PreviewPlace {
  if (place.top != null) {
    if (place.top + height <= b.vb) return place;
    return { ...place, top: Math.max(Math.ceil(b.vt), Math.floor(b.vb - height)) };
  }
  const foot = plotH - (place.bottom ?? 0);
  if (foot - height >= b.vt) return place;
  return { ...place, top: Math.ceil(b.vt), bottom: null };
}

/** How long a pointer resting on a card waits for its preview. A swap
 *  from one showing, or from one just closed, is quick; but while the
 *  preview's trailer plays, drifting over other cards on the way
 *  somewhere does not swap it, so the full wait applies. */
export function restDelay(showing: boolean, sinceShut: number, playing: boolean): number {
  if (playing) return PREVIEW_REST_MS;
  return showing || sinceShut < PREVIEW_WARM_MS ? PREVIEW_SWAP_MS : PREVIEW_REST_MS;
}

/** How long after the pointer leaves the preview closes. */
export function leaveDelay(playing: boolean): number {
  return playing ? PREVIEW_LEAVE_PLAYING_MS : PREVIEW_LEAVE_MS;
}

/** What the scheduler asks of the map. Read at the moment it asks, so
 *  it always sees what is on screen now rather than at the last render. */
export interface PreviewHost {
  /** The card whose preview is showing, if one is. */
  showing: () => string | null;
  /** Whether the preview's trailer is open. */
  playing: () => boolean;
  /** Show this card's preview, in place of any other. */
  open: (id: string) => void;
  /** Take the preview away. */
  close: () => void;
}

/** Time, for the scheduler. The page's own by default; a test's own
 *  hand-turned clock in the tests. */
export interface PreviewClock {
  now: () => number;
  after: (ms: number, run: () => void) => number;
  cancel: (timer: number) => void;
}

const pageClock: PreviewClock = {
  now: () => performance.now(),
  after: (ms, run) => window.setTimeout(run, ms),
  cancel: (timer) => window.clearTimeout(timer),
};

export interface PreviewScheduler {
  /** The pointer has come to rest on a card that can be opened. */
  rest: (id: string) => void;
  /** The pointer has left a card, or the preview. */
  leave: () => void;
  /** The pointer is on the preview, which keeps it. */
  hold: () => void;
  /** Close it now, and forget anything on its way: a scroll, Escape, a
   *  panel or View opening, the map changing. */
  shut: () => void;
  /** Let go of any timer, for good. */
  dispose: () => void;
}

/** When the preview opens, swaps and closes. One timer: whatever the
 *  pointer did last replaces whatever it was going to do before. */
export function previewScheduler(host: PreviewHost, clock: PreviewClock = pageClock): PreviewScheduler {
  let timer = 0;
  let shutAt = -Infinity;
  const cancel = () => {
    if (timer) clock.cancel(timer);
    timer = 0;
  };
  const shut = () => {
    cancel();
    if (host.showing() == null) return;
    shutAt = clock.now();
    host.close();
  };
  return {
    rest: (id) => {
      cancel();
      const showing = host.showing();
      // Back on the card whose preview it is: it simply stays.
      if (showing === id) return;
      const wait = restDelay(showing != null, clock.now() - shutAt, host.playing());
      timer = clock.after(wait, () => {
        timer = 0;
        host.open(id);
      });
    },
    leave: () => {
      cancel();
      if (host.showing() == null) return;
      timer = clock.after(leaveDelay(host.playing()), () => {
        timer = 0;
        shut();
      });
    },
    hold: cancel,
    shut,
    dispose: cancel,
  };
}
