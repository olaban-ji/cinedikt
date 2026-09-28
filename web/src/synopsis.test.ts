import { describe, expect, it } from 'vitest';
import css from './grid.css?raw';
import {
  ABOUT_GAP,
  LESS_MS,
  LESS_SETTLE_MS,
  MORE_MS,
  MORE_ROW_H,
  SYN_FEWEST,
  SYN_LH,
  SYN_MOST,
  SYN_TR,
  TRAILER_ROW_H,
  foldLines,
  foldSaves,
  overflows,
  restingLines,
  roomForTrailer,
  scrollEase,
  scrollLift,
  trailerNeed,
  type PanelNow,
} from './synopsis';
import { TRAILER_CLOSE_MS, TRAILER_OPEN_MS } from './trailer';

/** The declarations of every rule with exactly this selector, @media
 *  ones included, later ones winning. Enough of a parser for a sheet
 *  with no nesting beyond @media. */
function decls(selector: string): Map<string, string> {
  const text = css.replace(/\/\*[\s\S]*?\*\//g, '');
  const out = new Map<string, string>();
  for (const m of text.matchAll(/([^{}]+)\{([^{}]*)\}/g)) {
    if (!m[1].split(',').map((s) => s.trim()).includes(selector)) continue;
    for (const d of m[2].split(';')) {
      const at = d.indexOf(':');
      if (at > 0) out.set(d.slice(0, at).trim(), d.slice(at + 1).trim().replace(/\s+/g, ' '));
    }
  }
  return out;
}

const px = (v: string | undefined) => Number(v?.match(/^(-?[\d.]+)px$/)?.[1] ?? NaN);

describe('the resting lines', () => {
  it('are as many as leave the trailer row and 16px under it in view', () => {
    // 188 down, in an 800 tall body: room for far more than four.
    expect(restingLines(800, 188)).toBe(4);
    // 313 tall with the synopsis 134 down: 77px left, three lines.
    expect(restingLines(313, 134)).toBe(3);
    expect(restingLines(298, 134)).toBe(2);
  });

  it('count More’s row and the gap above the trailer row whether or not More shows', () => {
    const top = 100;
    const fixed = 16 + MORE_ROW_H + ABOUT_GAP + TRAILER_ROW_H;
    expect(fixed).toBe(102);
    expect(restingLines(top + fixed + 3 * SYN_LH, top)).toBe(3);
    expect(restingLines(top + fixed + 3 * SYN_LH - 0.5, top)).toBe(2);
  });

  it('are never more than four or fewer than two', () => {
    expect(SYN_MOST).toBe(4);
    expect(SYN_FEWEST).toBe(2);
    expect(restingLines(5000, 0)).toBe(4);
    expect(restingLines(120, 158)).toBe(2);
    expect(restingLines(0, 400)).toBe(2);
  });
});

describe('when More shows', () => {
  it('is when the whole text has more lines than it rests at', () => {
    expect(overflows(5 * SYN_LH, 4)).toBe(true);
    expect(overflows(4 * SYN_LH, 4)).toBe(false);
    expect(overflows(2 * SYN_LH, 4)).toBe(false);
    expect(overflows(4 * SYN_LH, 3)).toBe(true);
  });

  it('counts lines to the nearest, so a fraction of a pixel is not a line', () => {
    expect(overflows(4 * SYN_LH + 0.4, 4)).toBe(false);
    expect(overflows(4 * SYN_LH - 0.4, 3)).toBe(true);
  });
});

describe('what an opening trailer needs', () => {
  it('is the row, the player’s gap and the video, against the body’s visible bottom less 12', () => {
    // A row 323 down, a 241.875 video, an 800 body not scrolled: it
    // ends 175px short of where it may.
    expect(trailerNeed(323, 241.875, 0, 800)).toBe(-175.125);
    // A row 246 down, a 200 video, a 313 body.
    expect(trailerNeed(246, 200, 0, 313)).toBe(193);
    // Scrolled down 100, the same needs 100 less.
    expect(trailerNeed(246, 200, 100, 313)).toBe(93);
  });
});

describe('folding lines', () => {
  it('keeps the most lines that still give back what is needed', () => {
    const shown = 4 * SYN_LH;
    expect(foldLines(shown, 8, 4)).toBe(3);
    expect(foldLines(shown, SYN_LH, 4)).toBe(3);
    expect(foldLines(shown, 30, 4)).toBe(2);
    expect(foldLines(shown, 60, 4)).toBe(1);
  });

  it('folds away when even one line keeps too much', () => {
    expect(foldLines(4 * SYN_LH, 70, 4)).toBe(0);
    expect(foldLines(2 * SYN_LH, 30, 2)).toBe(0);
  });

  it('never keeps more than the resting lines, however much expanded text is showing', () => {
    // Twelve lines open under More: collapsing to the resting four is
    // already enough.
    expect(foldLines(12 * SYN_LH, 10, 4)).toBe(4);
    expect(foldLines(12 * SYN_LH, 10, 3)).toBe(3);
  });

  it('gives back the lines it drops, or everything, More’s row and the gap when it folds away', () => {
    expect(foldSaves(4 * SYN_LH, 3, true)).toBe(SYN_LH);
    expect(foldSaves(12 * SYN_LH, 4, true)).toBe(8 * SYN_LH);
    expect(foldSaves(3 * SYN_LH, 0, true)).toBe(3 * SYN_LH + 28 + 14);
    expect(foldSaves(3 * SYN_LH, 0, false)).toBe(3 * SYN_LH + 14);
  });
});

describe('the scroll', () => {
  it('finds what folding could not', () => {
    expect(scrollLift(40, 300, 0)).toBe(40);
  });

  it('never takes the controls row nearer the body’s top than 8', () => {
    expect(scrollLift(200, 134, 0)).toBe(126);
    expect(scrollLift(200, 134, 100)).toBe(26);
    // Already past it: no scroll at all, never one back down.
    expect(scrollLift(200, 134, 130)).toBe(0);
  });

  it('eases out from where it is to where it goes', () => {
    expect(scrollEase(0)).toBe(0);
    expect(scrollEase(1)).toBe(1);
    expect(scrollEase(0.5)).toBe(0.875);
    for (let k = 0; k < 1; k += 0.1) expect(scrollEase(k + 0.1)).toBeGreaterThan(scrollEase(k));
  });
});

describe('making room for a trailer', () => {
  const now = (over: Partial<PanelNow>): PanelNow => ({
    slotTop: 323,
    VH: 225,
    scrollTop: 0,
    bodyH: 800,
    shown: 4 * SYN_LH,
    lines: 4,
    over: true,
    ...over,
  });

  it('moves nothing when the whole video already fits', () => {
    expect(roomForTrailer(now({}))).toEqual({ synLines: null, lift: 0 });
    // An expanded synopsis stays expanded.
    expect(roomForTrailer(now({ shown: 12 * SYN_LH, slotTop: 509, bodyH: 900 }))).toEqual({ synLines: null, lift: 0 });
  });

  it('folds the synopsis to fewer lines when that is enough, with no scroll', () => {
    // Needs 8: three lines.
    expect(roomForTrailer(now({ bodyH: 600 }))).toEqual({ synLines: 3, lift: 0 });
    // Needs 48: one line.
    expect(roomForTrailer(now({ bodyH: 560 }))).toEqual({ synLines: 1, lift: 0 });
  });

  it('collapses More first, to the resting lines when that alone is enough', () => {
    expect(roomForTrailer(now({ shown: 12 * SYN_LH, slotTop: 509, bodyH: 780 }))).toEqual({ synLines: 4, lift: 0 });
  });

  it('folds it away when one line is too many, still with no scroll when that is enough', () => {
    // Needs 88; one line gives back 69.75, folding away 135.
    expect(roomForTrailer(now({ bodyH: 520 }))).toEqual({ synLines: 0, lift: 0 });
  });

  it('scrolls only for what folding away could not find', () => {
    // A landscape phone: needs 192.75 of a body 313 tall, and folding
    // away three lines, More and the gap gives back 111.75.
    const room = roomForTrailer(now({ slotTop: 245.75, VH: 200, bodyH: 313, shown: 3 * SYN_LH, lines: 3 }));
    expect(room.synLines).toBe(0);
    expect(room.lift).toBeCloseTo(81);
  });

  it('never scrolls the controls row nearer the top than 8, with the row where folding leaves it', () => {
    // A 258 tall body: 136 is still missing after folding away, but the
    // row is 134 down once folded, so the body scrolls 126.
    const room = roomForTrailer(now({ slotTop: 222.5, VH: 200, bodyH: 258, shown: 2 * SYN_LH, lines: 2 }));
    expect(room.synLines).toBe(0);
    expect(room.lift).toBe(126);
  });

  it('can only scroll when there is no synopsis to fold', () => {
    expect(roomForTrailer(now({ bodyH: 560, shown: null }))).toEqual({ synLines: null, lift: 48 });
  });
});

describe('the synopsis’s timing', () => {
  it('is More’s, Less’s, or the player’s own opening and closing', () => {
    expect(MORE_MS).toBe(360);
    expect(LESS_MS).toBe(300);
    expect(LESS_SETTLE_MS).toBe(LESS_MS + 20);
    expect(SYN_TR).toEqual({
      still: '0s',
      more: '360ms var(--ease-glide)',
      less: '300ms var(--ease-close)',
      open: `${TRAILER_OPEN_MS}ms var(--ease-glide)`,
      close: `${TRAILER_CLOSE_MS}ms var(--ease-close)`,
    });
    expect(TRAILER_OPEN_MS).toBe(520);
    expect(TRAILER_CLOSE_MS).toBe(400);
  });

  it('names curves the stylesheet has, with the handoff’s values', () => {
    const root = decls(':root');
    expect(root.get('--ease-glide')).toBe('cubic-bezier(0.22, 0.9, 0.24, 1)');
    expect(root.get('--ease-close')).toBe('cubic-bezier(0.55, 0, 0.3, 1)');
  });

  it('times everything that moves in it, and none of it for a reader who has asked for nothing to move', () => {
    const text = css.replace(/\/\*[\s\S]*?\*\//g, '');
    expect(text).toMatch(/\.cd-sheet-syn \{[^}]*transition: margin-top var\(--syn-tr\);/);
    expect(text).toMatch(/\.cd-sheet-syn-text \{[^}]*transition:\s+max-height var\(--syn-tr\),\s+opacity 0\.2s ease;/);
    expect(text).toMatch(/\.cd-sheet-syn-more \{[^}]*transition:\s+height var\(--syn-tr\),\s+opacity 0\.2s ease;/);
    const still = text.match(/@media \(prefers-reduced-motion: reduce\) \{\s*\.cd-scrim,[^{]*\{[^}]*\}/)?.[0] ?? '';
    for (const sel of ['.cd-sheet-syn,', '.cd-sheet-syn-text,', '.cd-sheet-syn-more,']) expect(still).toContain(sel);
  });
});

describe('the stylesheet and these numbers', () => {
  it('draw a line 23.25 tall', () => {
    const d = decls('.cd-sheet-syn-text');
    expect(px(d.get('font-size')) * Number(d.get('line-height'))).toBe(SYN_LH);
    expect(d.get('display')).toBe('-webkit-box');
    expect(d.get('-webkit-box-orient')).toBe('vertical');
    expect(d.get('overflow')).toBe('hidden');
  });

  it('make More’s row 28: a 24px line with 4 above it, a 44px target that moves nothing', () => {
    expect(px(decls('.cd-sheet-syn-more').get('height'))).toBe(MORE_ROW_H);
    const b = decls('.cd-sheet-syn-more button');
    expect(px(b.get('height')) + px(b.get('margin-top'))).toBe(MORE_ROW_H);
    expect(b.get('line-height')).toBe('24px');
    expect(b.get('font-size')).toBe('14px');
    expect(b.get('font-weight')).toBe('600');
    expect(b.get('color')).toBe('var(--accText)');
    expect(decls('.cd-sheet-syn-more button:hover').get('color')).toBe('var(--acc)');
    const hit = decls('.cd-sheet-syn-more button::before').get('inset')!.split(' ').map((v) => -px(v));
    expect(px(b.get('height')) + 2 * hit[0]).toBe(44);
    const ring = decls('.cd-sheet-syn-more button:focus-visible');
    expect(ring.get('outline')).toBe('2px solid var(--acc)');
    expect(ring.get('outline-offset')).toBe('2px');
  });

  it('put the gap the fold takes back between the synopsis and the trailer row', () => {
    expect(px(decls('.cd-sheet-about').get('gap'))).toBe(ABOUT_GAP);
    expect(px(decls('.cd-sheet-syn-gone').get('margin-top'))).toBe(-ABOUT_GAP);
    const gone = decls('.cd-sheet-syn-gone .cd-sheet-syn-more');
    expect(gone.get('height')).toBe('0');
    expect(gone.get('pointer-events')).toBe('none');
  });

  it('make every panel trailer row 44 tall', () => {
    expect(px(decls('.cd-trailer-btn').get('height'))).toBe(TRAILER_ROW_H);
    expect(px(decls('.cd-trailer-skel').get('height'))).toBe(TRAILER_ROW_H);
    expect(px(decls('.cd-trailer-none').get('height'))).toBe(TRAILER_ROW_H);
    expect(px(decls('.cd-trailer-controls').get('height'))).toBe(TRAILER_ROW_H);
  });
});
