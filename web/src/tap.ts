import { useCallback, useEffect, useRef, useState } from 'react';

/** How far a pointer may travel and still count as a tap. */
export const TAP_SLOP = 8;

/** How long after a scroll a tap is still the end of that scroll rather
 *  than a choice. */
export const SCROLL_QUIET_MS = 140;

/** A press is a tap when the finger stayed put and nothing was moving
 *  under it. Flick-to-stop and scroll-then-release both fail here, which
 *  is the point: neither of them meant to open anything. */
export function isTap(moved: boolean, sinceScroll: number): boolean {
  return !moved && sinceScroll >= SCROLL_QUIET_MS;
}

export interface TapGuard {
  /** Whether the click that is happening now was meant. */
  allows: () => boolean;
  onPointerDown: (e: { clientX: number; clientY: number }) => void;
  onPointerMove: (e: { clientX: number; clientY: number }) => void;
  onScroll: () => void;
}

/** Watches one scrolling surface so the things inside it can tell a tap
 *  from the end of a flick. Refs throughout: none of this is drawn, and
 *  a re-render for every pointermove would be the whole cost of it. */
export function useTapGuard(): TapGuard {
  const at = useRef({ x: 0, y: 0 });
  const moved = useRef(false);
  const scrolled = useRef(0);

  const onPointerDown = useCallback((e: { clientX: number; clientY: number }) => {
    at.current = { x: e.clientX, y: e.clientY };
    moved.current = false;
  }, []);

  const onPointerMove = useCallback((e: { clientX: number; clientY: number }) => {
    if (moved.current) return;
    if (Math.hypot(e.clientX - at.current.x, e.clientY - at.current.y) > TAP_SLOP) {
      moved.current = true;
    }
  }, []);

  const onScroll = useCallback(() => {
    scrolled.current = Date.now();
  }, []);

  const allows = useCallback(() => isTap(moved.current, Date.now() - scrolled.current), []);

  return { allows, onPointerDown, onPointerMove, onScroll };
}

/** Whether the pointer on this device can rest on something. A finger
 *  cannot, so previews and hover lighting are for mice only. */
export function canHover(): boolean {
  return window.matchMedia('(hover: hover)').matches;
}

/** Whether the pointer can rest on something precisely: a mouse or a
 *  trackpad, not a finger and not a stylus hovering over a screen.
 *  Resting on a card to preview it, and on Watch trailer to start it
 *  muted, are for this pointer only. */
export function canRest(): boolean {
  return (
    typeof window !== 'undefined' &&
    typeof window.matchMedia === 'function' &&
    window.matchMedia('(hover: hover) and (pointer: fine)').matches
  );
}

/** How long a pointer has to rest on a chip before it previews. Crossing
 *  the row on the way somewhere else should not make the map flash. */
export const HOVER_DELAY_MS = 140;

/** A preview that waits to be meant. */
export function useHoverDelay(set: (id: string | null) => void): {
  enter: (id: string) => void;
  leave: () => void;
} {
  const timer = useRef(0);
  const put = useRef(set);
  put.current = set;

  useEffect(() => () => window.clearTimeout(timer.current), []);

  const enter = useCallback((id: string) => {
    if (!canHover()) return;
    window.clearTimeout(timer.current);
    timer.current = window.setTimeout(() => put.current(id), HOVER_DELAY_MS);
  }, []);

  const leave = useCallback(() => {
    window.clearTimeout(timer.current);
    put.current(null);
  }, []);

  return { enter, leave };
}

/** How far inside an edge the searched film has to be before it counts
 *  as gone, so Recenter does not blink on and off at the boundary. */
export const OFF_EDGE = 24;

/** Whether a point has left the box, by more than the margin on any
 *  side. `x`/`y` are the card's centre in the scroller's own space. */
export function isOffScreen(
  x: number,
  y: number,
  view: { left: number; top: number; width: number; height: number },
): boolean {
  return (
    x < view.left + OFF_EDGE ||
    x > view.left + view.width - OFF_EDGE ||
    y < view.top + OFF_EDGE ||
    y > view.top + view.height - OFF_EDGE
  );
}

/** Scroll is a flood and this answer is a yes or a no, so it is asked
 *  at most this often. */
export const OFF_THROTTLE_MS = 60;

/** Tracks whether the searched film has scrolled out of sight. */
export function useOffScreen(
  el: { current: HTMLElement | null },
  where: () => { x: number; y: number } | null,
  deps: unknown[],
): boolean {
  const [off, setOff] = useState(false);
  const look = useRef(where);
  look.current = where;

  useEffect(() => {
    const node = el.current;
    if (!node) return;
    let last = 0;
    let timer = 0;
    const read = () => {
      last = Date.now();
      const at = look.current();
      if (!at) return;
      setOff(
        isOffScreen(at.x, at.y, {
          left: node.scrollLeft,
          top: node.scrollTop,
          width: node.clientWidth,
          height: node.clientHeight,
        }),
      );
    };
    const onScroll = () => {
      const due = OFF_THROTTLE_MS - (Date.now() - last);
      if (due <= 0) {
        read();
        return;
      }
      if (timer) return;
      timer = window.setTimeout(() => {
        timer = 0;
        read();
      }, due);
    };
    read();
    node.addEventListener('scroll', onScroll, { passive: true });
    return () => {
      window.clearTimeout(timer);
      node.removeEventListener('scroll', onScroll);
    };
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, deps);

  return off;
}
