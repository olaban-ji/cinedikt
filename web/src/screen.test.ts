import { describe, expect, it } from 'vitest';
import css from './grid.css?raw';
import { OVER_H, overOffset, screenOf, type Screen } from './screen';

describe('screenOf', () => {
  it('calls a narrow screen a phone, whatever its height', () => {
    for (const [w, h] of [
      [375, 667],
      [390, 844],
      [390, 400],
    ]) {
      const s = screenOf(w, h);
      expect(s.cls, `${w}x${h}`).toBe('phone');
      expect(s.phone && !s.short && !s.tablet && !s.desktop).toBe(true);
      expect(s.overlay).toBe(true);
      expect(s.touch).toBe(true);
      expect(s.narrow).toBe(true);
    }
  });

  it('calls a landscape phone short, not a phone', () => {
    // The header lies over the map as it does on a phone, but "inedikt"
    // has the room to stay.
    for (const [w, h] of [
      [667, 375],
      [844, 390],
    ]) {
      const s = screenOf(w, h);
      expect(s.cls, `${w}x${h}`).toBe('short');
      expect(s.phone).toBe(false);
      expect(s.overlay).toBe(true);
      expect(s.touch).toBe(true);
    }
  });

  it('calls a wide window under 500 px tall short too', () => {
    // The rule is the height, so a squashed desktop window takes the
    // landscape phone's layout and its finger-sized controls — but it is
    // wide enough for the rating rungs to stay in the header.
    const s = screenOf(1280, 480);
    expect(s.cls).toBe('short');
    expect(s.overlay).toBe(true);
    expect(s.touch).toBe(true);
    expect(s.narrow).toBe(false);
  });

  it('calls a portrait tablet a tablet, sized for a finger', () => {
    const s = screenOf(820, 1180);
    expect(s.cls).toBe('tablet');
    expect(s.overlay).toBe(false);
    expect(s.touch).toBe(true);
    expect(s.narrow).toBe(true);
  });

  it('calls a wide window a desktop, sized for a mouse', () => {
    for (const [w, h] of [
      [1180, 820],
      [1440, 900],
    ]) {
      const s = screenOf(w, h);
      expect(s.cls, `${w}x${h}`).toBe('desktop');
      expect(s.overlay).toBe(false);
      expect(s.touch).toBe(false);
      expect(s.narrow).toBe(false);
    }
  });

  it('sizes a desktop-wide screen for a finger when its pointer is coarse', () => {
    // A tablet held landscape: the desktop layout, with touch targets.
    const s = screenOf(1180, 820, true);
    expect(s.cls).toBe('desktop');
    expect(s.touch).toBe(true);
    expect(s.overlay).toBe(false);
  });

  it('puts the boundaries where the spec puts them', () => {
    expect(screenOf(639, 900).cls).toBe('phone');
    expect(screenOf(640, 900).cls).toBe('tablet');
    expect(screenOf(900, 499).cls).toBe('short');
    expect(screenOf(900, 500).cls).toBe('tablet');
    expect(screenOf(1023, 900).cls).toBe('tablet');
    expect(screenOf(1024, 900).cls).toBe('desktop');
    expect(screenOf(1023, 900).narrow).toBe(true);
    expect(screenOf(1024, 900).narrow).toBe(false);
    expect(screenOf(1023, 900).touch).toBe(true);
    expect(screenOf(1024, 900).touch).toBe(false);
    expect(screenOf(1024, 499).touch).toBe(true);
  });
});

describe('overOffset', () => {
  it('starts the map under the header where the header lies over it', () => {
    expect(overOffset(screenOf(390, 844))).toBe(OVER_H.phone);
    expect(overOffset(screenOf(844, 390))).toBe(OVER_H.short);
    expect(OVER_H).toEqual({ phone: 116, short: 102 });
  });

  it('leaves no offset where the header sits above the map', () => {
    expect(overOffset(screenOf(820, 1180))).toBe(0);
    expect(overOffset(screenOf(1440, 900))).toBe(0);
  });
});

/** The stylesheet's screen classes, as its vocabulary comment writes
 *  them. Each is the CSS twin of a flag on Screen. */
const VOCABULARY: Record<Exclude<keyof Screen, 'cls'>, string> = {
  phone: '(max-width: 639.98px)',
  short: '(min-width: 640px) and (max-height: 499.98px)',
  overlay: '(max-width: 639.98px), (max-height: 499.98px)',
  tablet: '(min-width: 640px) and (max-width: 1023.98px) and (min-height: 500px)',
  narrow: '(max-width: 1023.98px)',
  desktop: '(min-width: 1024px) and (min-height: 500px)',
  touch: '(max-width: 1023.98px), (max-height: 499.98px), (pointer: coarse)',
};

/** The other queries the stylesheet may use, which are not screen
 *  classes. */
const OTHERS = [
  '(max-height: 859.98px)',
  '(max-width: 359.98px)',
  '(hover: hover)',
  '(prefers-reduced-motion: reduce)',
  '(forced-colors: active)',
];

/** Enough of a media-query evaluator for the vocabulary: lists of
 *  `and`-ed min/max width/height features and the coarse pointer. */
function matches(query: string, w: number, h: number, coarse: boolean): boolean {
  return query.split(',').some((part) =>
    part
      .trim()
      .split(/\s+and\s+/)
      .every((feature) => {
        const m = feature.match(/^\((min|max)-(width|height): ([\d.]+)px\)$/);
        if (m) {
          const v = m[2] === 'width' ? w : h;
          return m[1] === 'min' ? v >= Number(m[3]) : v <= Number(m[3]);
        }
        if (feature === '(pointer: coarse)') return coarse;
        throw new Error(`not in the vocabulary: ${feature}`);
      }),
  );
}

describe('the stylesheet’s screen classes', () => {
  const uncommented = css.replace(/\/\*[\s\S]*?\*\//g, '');
  const used = [...uncommented.matchAll(/@media\s+([^{]+)\{/g)].map((m) => m[1].trim());

  it('are the only media queries it uses', () => {
    expect(used.length).toBeGreaterThan(10);
    const allowed = new Set([...Object.values(VOCABULARY), ...OTHERS]);
    for (const q of used) expect([...allowed], q).toContain(q);
  });

  it('are written down where the next rule will look for them', () => {
    for (const q of Object.values(VOCABULARY)) expect(css).toContain(q);
  });

  it('agree with screenOf at every size either side of a boundary', () => {
    for (const w of [320, 375, 390, 639, 640, 667, 820, 844, 1023, 1024, 1180, 1280, 1440]) {
      for (const h of [375, 390, 480, 499, 500, 667, 820, 844, 900, 1180]) {
        for (const coarse of [false, true]) {
          const s = screenOf(w, h, coarse);
          for (const [flag, query] of Object.entries(VOCABULARY)) {
            expect(matches(query, w, h, coarse), `${flag} at ${w}x${h}${coarse ? ', coarse' : ''}`).toBe(
              s[flag as keyof typeof VOCABULARY],
            );
          }
        }
      }
    }
  });
});
