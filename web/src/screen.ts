import { useEffect, useState } from 'react';

/** The four kinds of screen the app is laid out for.
 *
 *  `phone` is anything under 640 px wide, whatever its height. `short` is
 *  a landscape phone: wide enough for more than a phone's layout, too
 *  short to give the header its full height and still show a map under
 *  it. The rule is the height alone, so a wide window squashed under
 *  500 px (1280×480, say) is `short` too, and gets the same layout.
 *  `tablet` is 640–1023 px wide with room to spare in height, and
 *  `desktop` is 1024 px and up. The order matters: a phone is a phone
 *  first, then a short screen is short, and only then does the width
 *  decide between the last two. */
export type ScreenClass = 'phone' | 'short' | 'tablet' | 'desktop';

/** What the layout needs to know about the window. Every flag here has a
 *  twin in the media-query vocabulary at the top of grid.css, and the two
 *  must say the same thing at the same sizes. */
export interface Screen {
  cls: ScreenClass;
  phone: boolean;
  short: boolean;
  tablet: boolean;
  desktop: boolean;
  /** Narrower than 1024 px, whatever the height. The rating rungs leave
   *  the header for the View panel below this: they only have room beside
   *  the wordmark and the search field from 1024 up. A wide, short window
   *  keeps them in its header. */
  narrow: boolean;
  /** A phone or a landscape phone. The header lies over the map as glass
   *  and goes up out of the way on scroll, and the map uses the compact
   *  card. */
  overlay: boolean;
  /** Sized for a finger: everything narrower than 1024, every short
   *  screen, and any screen whose main pointer is coarse — a tablet held
   *  landscape is 1024 wide or more and still has no mouse. Controls grow
   *  to 44 px here, and inputs to 16 px so iOS does not zoom on focus. */
  touch: boolean;
}

const PHONE_MAX = 640;
const SHORT_MAX = 500;
/** The width the header first has room for everything at once. */
const RUNGS_MIN = 1024;

/** The header row's height in each class, as `.cd-header-row` sets it.
 *  The opening screen has no chip row, so this is its whole header. */
export const HEADER_ROW_H: Record<ScreenClass, number> = {
  phone: 64,
  short: 52,
  tablet: 66,
  desktop: 66,
};

/** How far down the map content starts when the header lies over it:
 *  the header row and the chip row under it (64 + 52 on a phone, 52 + 50
 *  on a landscape phone). Fixed numbers rather than a measurement, the
 *  same ones the stylesheet gives those two rows. A header measured on a
 *  page nobody was painting — one opened in a background tab — came back
 *  as nothing, and the map started underneath it. */
export const OVER_H = { phone: 116, short: 102 } as const;

export function screenOf(width: number, height: number, coarse = false): Screen {
  const phone = width < PHONE_MAX;
  const short = !phone && height < SHORT_MAX;
  const narrow = width < RUNGS_MIN;
  const tablet = !phone && !short && narrow;
  const desktop = !phone && !short && !narrow;
  const cls: ScreenClass = phone ? 'phone' : short ? 'short' : tablet ? 'tablet' : 'desktop';
  return {
    cls,
    phone,
    short,
    tablet,
    desktop,
    narrow,
    overlay: phone || short,
    touch: narrow || short || coarse,
  };
}

/** The offset the map starts at under a header lying over it, or 0 when
 *  the header sits above the map instead. */
export function overOffset(s: Screen): number {
  return s.phone ? OVER_H.phone : s.short ? OVER_H.short : 0;
}

const COARSE = '(pointer: coarse)';

function coarsePointer(): boolean {
  return typeof window.matchMedia === 'function' && window.matchMedia(COARSE).matches;
}

function sameScreen(a: Screen, b: Screen): boolean {
  return a.cls === b.cls && a.narrow === b.narrow && a.touch === b.touch;
}

/** Watches the window rather than an element: the header spans it. A
 *  resize or a turn of the phone changes the size; plugging in a mouse,
 *  or folding a tablet's keyboard away, changes the pointer. */
export function useScreen(): Screen {
  const [s, setS] = useState<Screen>(() =>
    typeof window === 'undefined'
      ? screenOf(1280, 800)
      : screenOf(window.innerWidth, window.innerHeight, coarsePointer()),
  );
  useEffect(() => {
    const read = () => {
      const next = screenOf(window.innerWidth, window.innerHeight, coarsePointer());
      setS((was) => (sameScreen(was, next) ? was : next));
    };
    read();
    const pointer = typeof window.matchMedia === 'function' ? window.matchMedia(COARSE) : null;
    window.addEventListener('resize', read);
    window.addEventListener('orientationchange', read);
    pointer?.addEventListener('change', read);
    return () => {
      window.removeEventListener('resize', read);
      window.removeEventListener('orientationchange', read);
      pointer?.removeEventListener('change', read);
    };
  }, []);
  return s;
}
