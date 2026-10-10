import { useEffect, useState } from 'react';
import { liveScreen, type LiveScreen } from './daily';

// The window as Cinedikt Daily lays itself out by it (daily.ts's
// LiveScreen), read afresh every time it can have changed: a resize, the
// phone turned, and the visual viewport resizing, which on iOS is the
// only one of the three that hears the toolbars come and go. Never a
// size kept from when the page loaded: the header's date, the four
// results on a phone, the Movies sheet from the bottom and the landscape
// column all follow the window as it is now.

/** The window's own reading, now; a desktop's where there is no window,
 *  as on the server renderer. */
export function readScreen(win: Pick<Window, 'innerWidth' | 'innerHeight' | 'visualViewport'> | undefined): LiveScreen {
  if (!win) return liveScreen(1280, 800);
  return liveScreen(win.innerWidth, win.innerHeight, win.visualViewport?.height);
}

/** Whether two readings would lay the page out alike, so a resize that
 *  changes nothing the Daily reads redraws nothing. */
export function sameScreen(a: LiveScreen, b: LiveScreen): boolean {
  return a.phone === b.phone && a.land === b.land && a.viewH === b.viewH;
}

/** Something that can be listened to: the window, and its visual
 *  viewport. */
interface Heard {
  addEventListener(type: string, fn: () => void): void;
  removeEventListener(type: string, fn: () => void): void;
}

/** Calls `read` on every change of size the Daily reads: the window's
 *  resize and orientationchange, and the visual viewport's resize.
 *  Hands back what stops listening. */
export function watchScreen(win: Heard & { visualViewport?: Heard | null }, read: () => void): () => void {
  const vv = win.visualViewport;
  win.addEventListener('resize', read);
  win.addEventListener('orientationchange', read);
  vv?.addEventListener('resize', read);
  return () => {
    win.removeEventListener('resize', read);
    win.removeEventListener('orientationchange', read);
    vv?.removeEventListener('resize', read);
  };
}

/** The window as it is now, kept up to date for as long as the caller is
 *  up. */
export function useLiveScreen(): LiveScreen {
  const [screen, setScreen] = useState<LiveScreen>(() => readScreen(typeof window === 'undefined' ? undefined : window));
  useEffect(() => {
    const read = () => {
      const now = readScreen(window);
      setScreen((was) => (sameScreen(was, now) ? was : now));
    };
    read();
    return watchScreen(window, read);
  }, []);
  return screen;
}
