import { useCallback, useEffect, useRef, useState, type MutableRefObject } from 'react';
import { stillNow } from './motion';

/** A layer arrives in three steps: it mounts off-screen, a moment later
 *  it is let in, and on the way out it is given its exit before anything
 *  else happens. The moment is a timer rather than a frame, so a layer
 *  raised on a page that is not being painted still opens. */
export const ENTER_MS = 20;

/** Whether something that has just mounted is let in yet: not for the
 *  first ENTER_MS, so its entrance has somewhere to run from, and at
 *  once for a reader who has asked for nothing to move. A timer, as the
 *  layers use, rather than a frame. */
export function useEntered(): boolean {
  const [shown, setShown] = useState(stillNow);
  useEffect(() => {
    const t = window.setTimeout(() => setShown(true), ENTER_MS);
    return () => window.clearTimeout(t);
  }, []);
  return shown;
}

/** How long each layer's exit is given before it leaves the tree, and
 *  before whatever it was asked to do is done: the length of the exit
 *  its stylesheet draws. The film sheet's is also the first step of the
 *  move to another map, so the lift that follows starts as it goes. */
export const SHEET_EXIT_MS = 280;
export const VIEW_EXIT_MS = 260;

/** How long to wait for an exit. None at all for a reader who has asked
 *  for nothing to move: the layer is simply gone, and whatever it was
 *  asked to do happens at once rather than after a pause with nothing
 *  on screen to explain it. */
export function exitDelay(ms: number, still: boolean): number {
  return still ? 0 : ms;
}

/** Let a phone sheet go further down than this and it closes; anything
 *  less and it springs back. */
export const CLOSE_AT = 90;

/** How far a dragged sheet has moved. Downwards only: pulling up would
 *  otherwise lift it off the bottom of the screen. */
export function dragOffset(from: number, to: number): number {
  return Math.max(0, to - from);
}

export function closesOn(offset: number): boolean {
  return offset > CLOSE_AT;
}

type Phase = 'mounted' | 'in' | 'out';

interface Glide {
  phase: Phase;
  /** Start the exit. `then` runs once the layer is gone, so nothing
   *  happens underneath a sheet that is still leaving. */
  leave: (then?: () => void) => void;
}

export function useGlide(onGone: () => void, exitMs: number): Glide {
  const [phase, setPhase] = useState<Phase>('mounted');
  const gone = useRef(onGone);
  gone.current = onGone;
  const timer = useRef(0);

  useEffect(() => {
    const t = window.setTimeout(() => setPhase('in'), ENTER_MS);
    return () => {
      window.clearTimeout(t);
      window.clearTimeout(timer.current);
    };
  }, []);

  const leave = useCallback((then?: () => void) => {
    window.clearTimeout(timer.current);
    setPhase('out');
    // Read now rather than watched, so a setting changed while the layer
    // is open is the one its exit obeys.
    const wait = exitDelay(exitMs, stillNow());
    timer.current = window.setTimeout(() => {
      gone.current();
      then?.();
    }, wait);
  }, [exitMs]);

  return { phase, leave };
}

/** Hands the layer's own way out — its exit, then whatever comes next —
 *  to whoever holds `closer`, for as long as the layer is up. Opening a
 *  map uses it to close a sheet or panel on its exit before moving. */
export function useCloser(
  closer: MutableRefObject<((then?: () => void) => void) | null> | undefined,
  leave: (then?: () => void) => void,
): void {
  useEffect(() => {
    if (!closer) return;
    closer.current = leave;
    return () => {
      if (closer.current === leave) closer.current = null;
    };
  }, [closer, leave]);
}

/** Every layer that Escape can close, newest last. */
const layers: object[] = [];

/** Escape closes the layer on top. Each one pushes itself on the way in
 *  and takes itself off on the way out, so the one opened last answers
 *  and the ones beneath it stay where they are. */
export function useEscape(close: () => void): void {
  const shut = useRef(close);
  shut.current = close;
  useEffect(() => {
    const mine = {};
    layers.push(mine);
    const onKey = (e: KeyboardEvent) => {
      if (e.key !== 'Escape' || layers[layers.length - 1] !== mine) return;
      e.stopPropagation();
      shut.current();
    };
    window.addEventListener('keydown', onKey);
    return () => {
      const at = layers.indexOf(mine);
      if (at >= 0) layers.splice(at, 1);
      window.removeEventListener('keydown', onKey);
    };
  }, []);
}

/** Focus goes to the layer when it opens and comes back to whatever the
 *  reader was on when it closes — the card they tapped, usually. */
export function useFocusTrapped(ref: { current: HTMLElement | null }): void {
  useEffect(() => {
    const was = document.activeElement as HTMLElement | null;
    ref.current?.focus({ preventScroll: true });
    return () => was?.focus?.({ preventScroll: true });
    // The ref is stable for the life of the layer.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, []);
}

interface Drag {
  /** How far down the sheet is being held, in pixels. */
  y: number;
  /** Held right now, so the sheet follows the finger with no transition. */
  held: boolean;
  onPointerDown: (e: React.PointerEvent) => void;
  onPointerMove: (e: React.PointerEvent) => void;
  onPointerUp: (e: React.PointerEvent) => void;
}

/** The phone sheet's drag handle. */
export function useDrag(enabled: boolean, close: () => void): Drag {
  const [y, setY] = useState(0);
  const [held, setHeld] = useState(false);
  const from = useRef(0);

  const onPointerDown = useCallback(
    (e: React.PointerEvent) => {
      if (!enabled) return;
      from.current = e.clientY;
      setHeld(true);
      // Capture keeps the drag with the handle when the finger slides off
      // it. A pointer that is already gone cannot be captured, and that
      // is not a reason to drop the drag.
      try {
        e.currentTarget.setPointerCapture?.(e.pointerId);
      } catch {
        // Nothing to hold on to; the handle's own events will do.
      }
    },
    [enabled],
  );

  const onPointerMove = useCallback(
    (e: React.PointerEvent) => {
      if (!held) return;
      setY(dragOffset(from.current, e.clientY));
    },
    [held],
  );

  const onPointerUp = useCallback(
    (e: React.PointerEvent) => {
      if (!held) return;
      setHeld(false);
      try {
        e.currentTarget.releasePointerCapture?.(e.pointerId);
      } catch {
        // Already released, or never captured.
      }
      if (closesOn(y)) close();
      setY(0);
    },
    [held, y, close],
  );

  return { y, held, onPointerDown, onPointerMove, onPointerUp };
}
