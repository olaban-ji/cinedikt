// The bigger photo of a person (PersonCard): when it opens, where it
// goes, and what closes it. The chips and the hover preview's faces only
// pass their events on; the timings live here, as a small controller
// with its clock and its image loading handed in, so each rule can be
// checked without a DOM, and in the useFaceCard hook that runs it for
// the page.

import { useEffect, useMemo, useRef, useState, type RefObject } from 'react';
import { FACE_CARD_DWELL_MS, FACE_CARD_HOLD_MS, FACE_CARD_WARM_MS } from './motion';
import { posterURL } from './poster';
import { pageClock, type PreviewClock } from './preview';
import { canHover } from './tap';

/** What asked for the card: a chip in the row, or a face in the hover
 *  preview. */
export type FaceFrom = 'chip' | 'peek';

/** The card on show. */
export interface FaceCard {
  /** Whose it is. */
  id: string;
  from: FaceFrom;
  /** Its left edge in the window. */
  x: number;
  /** Its top edge in the window when it hangs below what asked for it;
   *  otherwise how far its bottom edge is above the window's bottom. */
  y: number;
  /** Below what asked for it, growing from its top edge, or above it,
   *  growing from its bottom edge. */
  down: boolean;
}

/** How wide the card is. */
export const FACE_CARD_W = 148;
/** How far it keeps from what asked for it, and from the window's edges. */
export const FACE_CARD_GAP = 8;
/** A preview face whose top is nearer the window's top than this has
 *  its card below it; any lower, the card goes above. */
export const FACE_CARD_ROOM = 300;
/** The width the card draws the photo at: its 148px less its own 6px
 *  padding and the frame's 4px, on each side. At twice that for a sharp
 *  screen it is TMDb's w342; the payload's w185 is too soft this big. */
export const FACE_CARD_PHOTO_W = FACE_CARD_W - 2 * 6 - 2 * 4;

/** The address of the card's photo, from the person's small one. */
export function bigPhoto(photo: string): string {
  return posterURL(photo, FACE_CARD_PHOTO_W) ?? photo;
}

/** Where the card goes for a chip or face at `r`, measured as it opens:
 *  centred on it, and kept inside the window's edges. From a chip it
 *  hangs below, the chip row being at the top of the page. From a
 *  preview face it hangs below only near the top of the window, and
 *  otherwise stands above, where there is room for it. */
export function placeFaceCard(
  r: { left: number; top: number; width: number; bottom: number },
  from: FaceFrom,
  win: { width: number; height: number },
): Pick<FaceCard, 'x' | 'y' | 'down'> {
  const x = Math.round(
    Math.min(Math.max(FACE_CARD_GAP, r.left + r.width / 2 - FACE_CARD_W / 2), win.width - FACE_CARD_W - FACE_CARD_GAP),
  );
  const down = from === 'chip' || r.top < FACE_CARD_ROOM;
  return { x, down, y: Math.round(down ? r.bottom + FACE_CARD_GAP : win.height - r.top + FACE_CARD_GAP) };
}

/** Loads an image and calls `done` once, with whether it arrived. The
 *  function it returns stops listening. */
export type ImageLoad = (url: string, done: (ok: boolean) => void) => () => void;

/** The page's own image loading: an Image the browser keeps, so the card
 *  drawing the same address finds it in hand. */
export const loadImage: ImageLoad = (url, done) => {
  let live = true;
  const img = new Image();
  img.onload = () => live && done(true);
  img.onerror = () => live && done(false);
  img.src = url;
  return () => {
    live = false;
  };
};

/** Starts loading the bigger photo for each of these small ones that has
 *  not been started already, noting each in `warmed`. A swap in the warm
 *  window opens the next card as soon as its photo is in hand, so only a
 *  photo fetched ahead of the pointer lets it swap at once. */
export function warmBigPhotos(
  photos: Iterable<string | undefined>,
  warmed: Set<string>,
  load: ImageLoad = loadImage,
): void {
  for (const photo of photos) {
    if (!photo) continue;
    const url = bigPhoto(photo);
    if (warmed.has(url)) continue;
    warmed.add(url);
    load(url, () => {});
  }
}

/** What the controller asks of the page. Read at the moment it asks, so
 *  it always sees what is on screen now. */
export interface FaceCardHost {
  /** Whether the pointer can rest on anything. A finger's "hover" opens
   *  nothing: on touch a card opens only for a press held on a chip. */
  hovers: () => boolean;
  /** The person's photo, if they have one. Nobody without one gets a
   *  card: there is nothing bigger to show. */
  photo: (id: string) => string | undefined;
  /** Where the card goes for this chip or face, measured now; null when
   *  it has left the page on the way. */
  place: (el: HTMLElement, from: FaceFrom) => Pick<FaceCard, 'x' | 'y' | 'down'> | null;
  /** Show this card, in place of any other. */
  open: (card: FaceCard) => void;
  /** Take the card away. */
  close: () => void;
}

/** The events a chip or a preview face passes on. Each is its own
 *  function, so they can be handed out singly. */
export interface FaceCardEvents {
  /** The pointer has come onto a chip or a preview face. */
  onFace: (id: string, el: HTMLElement, from: FaceFrom) => void;
  /** It has left. Given `from`, what held the chips or faces there has
   *  gone, and only a card from there, or one on its way, goes with it. */
  offFace: (from?: FaceFrom) => void;
}

/** A press on a chip, which on touch is how a card opens. */
export interface FaceHold {
  /** A pointer has gone down on the chip. Only a finger's press is
   *  timed: a mouse has its dwell. */
  down: (id: string, el: HTMLElement, pointerType: string) => void;
  /** It has lifted. */
  up: () => void;
  /** The press was called off: the row started to scroll sideways under
   *  it. No click follows. */
  cancel: () => void;
  /** A click on the chip. True when it ends a hold, and so is not the
   *  chip's: the chip is not toggled. */
  click: () => boolean;
  /** Whether a context menu should be kept away now: while a finger is
   *  holding a chip, iOS and Android would put their own over the card. */
  menu: () => boolean;
}

export interface FaceCardController extends FaceCardEvents, FaceHold {
  /** Close it now, and forget any on its way: the map scrolling, a move
   *  to another map. */
  shut: () => void;
  /** Let go of any timer and image, for good. */
  dispose: () => void;
}

/** A card on its way: the dwell or the hold running, and its photo
 *  loading. It opens once both are done. */
interface Pending {
  id: string;
  el: HTMLElement;
  from: FaceFrom;
  /** Started by a finger's press, which only lifting it or calling it off
   *  ends. */
  touch: boolean;
  waited: boolean;
  loaded: boolean;
  timer: number;
  stop: () => void;
}

/** When the card opens, swaps and closes.
 *
 *  A pointer resting on a chip or a preview face opens it after
 *  FACE_CARD_DWELL_MS; a finger held on a chip, after FACE_CARD_HOLD_MS.
 *  Either way the card waits for its bigger photo too, which starts
 *  loading as the wait does: until it has arrived there is nothing to
 *  show but the blurry small one. Once a card is showing, or within
 *  FACE_CARD_WARM_MS of one closing, the next person's opens as soon as
 *  its photo is in hand. One card at a time, and one on its way: whatever
 *  the pointer did last replaces what it was going to do before. */
export function faceCardController(
  host: FaceCardHost,
  clock: PreviewClock = pageClock,
  load: ImageLoad = loadImage,
): FaceCardController {
  let pending: Pending | null = null;
  let showing: FaceCard | null = null;
  let shutAt = -Infinity;
  // A finger is on a chip.
  let pressing = false;
  // Its press lasted the hold, so the click that ends it is not the chip's.
  let held = false;
  // A finger's press whose click has not come yet. The mouse events a
  // browser makes up for a tap arrive in between, and are not a pointer
  // resting on the chip.
  let touched = false;

  const forget = () => {
    if (!pending) return;
    if (pending.timer) clock.cancel(pending.timer);
    pending.stop();
    pending = null;
  };

  const close = () => {
    if (!showing) return;
    showing = null;
    shutAt = clock.now();
    host.close();
  };

  const settle = (p: Pending) => {
    if (pending !== p || !p.waited || !p.loaded) return;
    pending = null;
    const at = host.place(p.el, p.from);
    if (!at) return;
    showing = { id: p.id, from: p.from, ...at };
    host.open(showing);
  };

  const start = (id: string, el: HTMLElement, from: FaceFrom, wait: number, touch: boolean) => {
    forget();
    const photo = host.photo(id);
    if (!photo) return;
    const p: Pending = { id, el, from, touch, waited: false, loaded: false, timer: 0, stop: () => {} };
    pending = p;
    p.timer = clock.after(wait, () => {
      p.timer = 0;
      if (pending !== p) return;
      p.waited = true;
      if (touch) held = true;
      settle(p);
    });
    p.stop = load(bigPhoto(photo), (ok) => {
      if (pending !== p) return;
      if (!ok) {
        forget();
        return;
      }
      p.loaded = true;
      settle(p);
    });
  };

  const lift = () => {
    if (pending?.touch) forget();
    pressing = false;
    if (held) close();
  };

  return {
    onFace: (id, el, from) => {
      if (touched || !host.hovers()) return;
      const warm = showing != null || clock.now() - shutAt < FACE_CARD_WARM_MS;
      start(id, el, from, warm ? 0 : FACE_CARD_DWELL_MS, false);
    },
    offFace: (from) => {
      if (pending && (from == null || pending.from === from)) forget();
      if (showing && (from == null || showing.from === from)) close();
    },
    down: (id, el, pointerType) => {
      held = false;
      touched = false;
      if (pointerType !== 'touch') return;
      pressing = true;
      touched = true;
      start(id, el, 'chip', FACE_CARD_HOLD_MS, true);
    },
    up: lift,
    cancel: () => {
      lift();
      held = false;
      touched = false;
    },
    click: () => {
      const ends = held;
      held = false;
      touched = false;
      return ends;
    },
    menu: () => pressing || held,
    shut: () => {
      forget();
      close();
    },
    dispose: forget,
  };
}

/** The page's one card, and the events that open and close it. Every
 *  function handed out is the same one for the life of the page.
 *
 *  `photo` gives a person's photo by id; `scroller` is the map, whose
 *  scrolling closes the card: it was placed for where the chip or face
 *  was, and does not follow it. */
export function useFaceCard(
  photo: (id: string) => string | undefined,
  scroller: RefObject<HTMLElement | null>,
): FaceCardEvents & { card: FaceCard | null; hold: FaceHold; shut: () => void } {
  const live = useRef({ photo, scroller });
  live.current = { photo, scroller };
  const [card, setCard] = useState<FaceCard | null>(null);
  const [control] = useState(() =>
    faceCardController({
      hovers: canHover,
      photo: (id) => live.current.photo(id),
      place: (el, from) =>
        el.isConnected
          ? placeFaceCard(el.getBoundingClientRect(), from, { width: window.innerWidth, height: window.innerHeight })
          : null,
      open: setCard,
      close: () => setCard(null),
    }),
  );
  useEffect(() => () => control.dispose(), [control]);

  // Scroll events do not bubble, so the map's are caught on their way
  // down to it. The chip row's own sideways scroll is not the map's.
  useEffect(() => {
    const onScroll = (e: Event) => {
      if (e.target === live.current.scroller.current) control.shut();
    };
    document.addEventListener('scroll', onScroll, { capture: true, passive: true });
    return () => document.removeEventListener('scroll', onScroll, { capture: true });
  }, [control]);

  const events = useMemo(
    () => ({
      onFace: control.onFace,
      offFace: control.offFace,
      hold: { down: control.down, up: control.up, cancel: control.cancel, click: control.click, menu: control.menu },
      shut: control.shut,
    }),
    [control],
  );
  return { ...events, card };
}
