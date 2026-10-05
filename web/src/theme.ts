/** Which theme the app draws in, and how it is chosen.
 *
 *  Three choices, not two: "system" is the default, because a reader
 *  who has set their machine to light at sunset has already said what
 *  they want and should not have to say it again here. Light and dark
 *  are for the times they want this one page to disagree with it. */

import { useCallback, useEffect, useState } from 'react';

export type ThemePref = 'system' | 'light' | 'dark';
export type Theme = 'light' | 'dark';

/** Where the choice lives. Read by the inline script in index.html
 *  before any stylesheet, so a reload never flashes the wrong ground. */
const THEME_KEY = 'cinedikt.theme';

/** The browser chrome around the page, so the notch and the tab strip
 *  are the same colour as the page under them: each theme's ground
 *  (--g), written as hex, which every browser's chrome understands. The
 *  inline script in index.html carries the same two values for the
 *  first paint, and a test holds the two to each other. */
export const THEME_COLORS: Record<Theme, string> = { dark: '#13100d', light: '#f9f4ee' };

/** How long the crossfade lasts. The class is added for the duration of
 *  the switch and then removed: leaving it on would put a 250 ms
 *  transition on every hover state in the app. */
const CROSSFADE_MS = 250;

function prefers(query: string): boolean {
  return typeof matchMedia === 'function' && matchMedia(query).matches;
}

/** The theme a preference actually resolves to right now. */
export function resolved(pref: ThemePref): Theme {
  if (pref !== 'system') return pref;
  return prefers('(prefers-color-scheme: light)') ? 'light' : 'dark';
}

/** A stored value, read back. Anything else means the default. */
export function prefFrom(raw: string | null): ThemePref {
  return raw === 'light' || raw === 'dark' || raw === 'system' ? raw : 'system';
}

/** Put a theme on the page. `animate` crossfades it, which is right for
 *  a reader who just pressed the button and wrong for an OS that
 *  changed underneath them while they were reading. */
function apply(pref: ThemePref, animate: boolean): Theme {
  const theme = resolved(pref);
  const el = document.documentElement;
  if (animate && !prefers('(prefers-reduced-motion: reduce)')) {
    el.classList.add('cd-theming');
    window.setTimeout(() => el.classList.remove('cd-theming'), CROSSFADE_MS);
  }
  el.dataset.theme = theme;
  document.getElementById('theme-color')?.setAttribute('content', THEME_COLORS[theme]);
  return theme;
}

/** The reader's choice, and a way to change it. */
export function useTheme(): [ThemePref, (p: ThemePref) => void] {
  const [pref, setPref] = useState<ThemePref>(() => {
    try {
      return prefFrom(localStorage.getItem(THEME_KEY));
    } catch {
      return 'system';
    }
  });

  const choose = useCallback((next: ThemePref) => {
    setPref(next);
    try {
      localStorage.setItem(THEME_KEY, next);
    } catch {
      // A reader with storage blocked still gets this session's choice.
    }
    apply(next, true);
  }, []);

  // While the choice is "system", the page follows the machine as it
  // changes — without the crossfade, which belongs to a button press.
  useEffect(() => {
    if (pref !== 'system' || typeof matchMedia !== 'function') return;
    const mq = matchMedia('(prefers-color-scheme: light)');
    const follow = () => apply('system', false);
    follow();
    mq.addEventListener('change', follow);
    return () => mq.removeEventListener('change', follow);
  }, [pref]);

  return [pref, choose];
}

/** The theme as it is drawn, for the few things that are painted in
 *  JavaScript rather than CSS — the poster fallback gradient, whose
 *  lightness depends on the theme and which is set inline so that its
 *  oklch() stops reach the browser as written.
 *
 *  It watches the attribute rather than the preference, so it is right
 *  whether the change came from the picker or from the machine. */
export function useResolvedTheme(): Theme {
  const [theme, setTheme] = useState<Theme>(() =>
    typeof document === 'undefined'
      ? 'dark'
      : document.documentElement.dataset.theme === 'light'
        ? 'light'
        : 'dark',
  );
  useEffect(() => {
    const el = document.documentElement;
    const read = () => setTheme(el.dataset.theme === 'light' ? 'light' : 'dark');
    read();
    const watch = new MutationObserver(read);
    watch.observe(el, { attributes: true, attributeFilter: ['data-theme'] });
    return () => watch.disconnect();
  }, []);
  return theme;
}

/** Whether the reader has asked for as little movement as possible.
 *
 *  Watched rather than read once: the setting can change while the tab
 *  is open, and a screen that is mid-animation when it does should
 *  settle rather than finish its flourish. */
export function useReducedMotion(): boolean {
  const [still, setStill] = useState(() => prefers('(prefers-reduced-motion: reduce)'));
  useEffect(() => {
    if (typeof matchMedia !== 'function') return;
    const mq = matchMedia('(prefers-reduced-motion: reduce)');
    const read = () => setStill(mq.matches);
    read();
    mq.addEventListener('change', read);
    return () => mq.removeEventListener('change', read);
  }, []);
  return still;
}
