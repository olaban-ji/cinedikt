import { describe, expect, it } from 'vitest';
import css from './grid.css?raw';
import { previewFloatClear } from './preview';

/** The floating buttons and the toast are placed by the stylesheet
 *  alone, so this reads it. What is pinned is the arithmetic a later
 *  edit could quietly break: the pill sits inside a larger transparent
 *  float (Recenter's button; View's holds two, and its slop is on View
 *  itself), so the offset the eye sees is the float's plus its slop. */

const TOUCH = '(max-width: 1023.98px), (max-height: 499.98px), (pointer: coarse)';
const NARROW = '(max-width: 1023.98px)';
const SHORT = '(min-width: 640px) and (max-height: 499.98px)';
const PHONE = '(max-width: 639.98px)';
const OVERLAY = '(max-width: 639.98px), (max-height: 499.98px)';

interface Rule {
  /** The @media query the rule sits in, or null at the top level. */
  media: string | null;
  selector: string;
  decls: Map<string, string>;
}

/** Every rule that holds declarations, with the query it sits in.
 *  Enough of a parser for a sheet with no nesting beyond @media and no
 *  braces inside strings. */
function rules(sheet: string): Rule[] {
  const text = sheet.replace(/\/\*[\s\S]*?\*\//g, '');
  const out: Rule[] = [];
  const open: { prelude: string; start: number }[] = [];
  let mark = 0;
  for (let i = 0; i < text.length; i++) {
    if (text[i] === '{') {
      open.push({ prelude: text.slice(mark, i).trim(), start: i + 1 });
      mark = i + 1;
    } else if (text[i] === '}') {
      const block = open.pop();
      mark = i + 1;
      if (!block || block.prelude.startsWith('@')) continue;
      const media = open.find((b) => b.prelude.startsWith('@media'));
      const decls = new Map<string, string>();
      for (const d of text.slice(block.start, i).split(';')) {
        const at = d.indexOf(':');
        if (at > 0) decls.set(d.slice(0, at).trim(), d.slice(at + 1).trim().replace(/\s+/g, ' '));
      }
      // A selector list is read on one line, so a test can name it.
      const selector = block.prelude.replace(/\s+/g, ' ');
      out.push({ media: media ? media.prelude.replace(/^@media\s+/, '') : null, selector, decls });
    }
  }
  return out;
}

const all = rules(css);

/** The last value this selector gives the property inside this query
 *  (null: outside any), the way the cascade would settle it. */
function get(selector: string, prop: string, media: string | null = null): string | undefined {
  let found: string | undefined;
  for (const r of all) if (r.selector === selector && r.media === media && r.decls.has(prop)) found = r.decls.get(prop);
  return found;
}

/** The pixels in a value such as `6px` or `calc(10px + env(…))`. */
function px(value: string | undefined): number {
  const m = value?.match(/(-?[\d.]+)px/);
  if (!m) throw new Error(`no px in ${value}`);
  return Number(m[1]);
}

describe('the floating buttons', () => {
  const slop = px(get('.cd-float', 'padding'));

  it('have a slop around the pill, so a near-miss still lands', () => {
    expect(slop).toBe(6);
  });

  it('show their pills 16px in from the bottom corners', () => {
    expect(px(get('.cd-float', 'bottom')) + slop).toBe(16);
    expect(px(get('.cd-view-button', 'left')) + slop).toBe(16);
    expect(px(get('.cd-recentre', 'right')) + slop).toBe(16);
  });

  it('bring View to 12px from the left on a phone', () => {
    expect(px(get('.cd-view-button', 'left', PHONE)) + slop).toBe(12);
  });

  it('stay clear of a home indicator, on top of the offset', () => {
    expect(get('.cd-float', 'bottom')).toContain('+ env(safe-area-inset-bottom)');
  });

  it('are 42 high, and 46 at a finger’s size', () => {
    expect(get('.cd-float-pill', 'height')).toBe('42px');
    expect(get('.cd-float-pill', 'height', TOUCH)).toBe('46px');
  });

  it('take the strip the hover preview keeps clear of: their offset, their height and 10px', () => {
    const up = px(get('.cd-float', 'bottom')) + slop;
    expect(previewFloatClear(false)).toBe(up + px(get('.cd-float-pill', 'height')) + 10);
    expect(previewFloatClear(true)).toBe(up + px(get('.cd-float-pill', 'height', TOUCH)) + 10);
  });

  it('have no hover state, only a press', () => {
    // The handoff gives them none: a press and the focus ring are the
    // only states either button has.
    const floating = ['.cd-float', '.cd-view-open', '.cd-view-quick'];
    expect(all.filter((r) => floating.some((f) => r.selector.includes(f)) && r.selector.includes(':hover'))).toEqual([]);
    // Recenter's pill presses whole. View's pill now holds two buttons,
    // View and the quick "Hide empty years" switch, and each presses on
    // its own: a press on either makes their container :active too, so
    // the rule every float used to share would shrink the pill around
    // the button already shrinking inside it.
    expect(get('.cd-recentre:active .cd-float-pill', 'transform')).toBe('scale(0.97)');
    expect(get('.cd-view-open:active, .cd-view-quick-switch:active', 'transform')).toBe('scale(0.97)');
    expect(get('.cd-float:active .cd-float-pill', 'transform')).toBeUndefined();
  });

  it('turn View and the quick switch into icons 46 square on phones and short screens', () => {
    // Each keeps a finger's size with no words: View by its own width,
    // the switch by its icon and the padding either side of it. Both
    // take the pill's height, 46 at these sizes (above).
    expect(get('.cd-view-open', 'width', OVERLAY)).toBe('46px');
    expect(get('.cd-view-open', 'padding', OVERLAY)).toBe('0');
    const icon = px(get('.cd-quick-icon', 'width', OVERLAY));
    expect(icon).toBe(px(get('.cd-quick-icon', 'height', OVERLAY)));
    expect(get('.cd-view-quick-switch', 'padding', OVERLAY)).toBe('0 5px');
    expect(icon + 2 * 5).toBe(46);
    expect(get('.cd-view-label, .cd-quick-label, .cd-quick-track', 'display', OVERLAY)).toBe('none');
    // The icon is only drawn there.
    expect(get('.cd-quick-icon', 'display')).toBe('none');
    expect(get('.cd-quick-icon', 'display', OVERLAY)).toBe('flex');
    // The switch grows out of View to a width that holds the rule
    // before it and all 46 of it.
    expect(px(get('.cd-view-quick-on', 'max-width', OVERLAY))).toBeGreaterThanOrEqual(1 + 46);
  });

  it('keep View’s slop on View itself, now that its float is not a button', () => {
    // Above, below and to the left of the pill, where the float's own
    // padding is; to the right is the quick switch.
    expect(get('.cd-view-open::before', 'inset')).toBe(`-${slop}px 0 -${slop}px -${slop}px`);
  });

  it('hide by sinking and fading, without shrinking', () => {
    expect(get('.cd-float', 'transform')).toBe('translateY(14px)');
    expect(get('.cd-float', 'opacity')).toBe('0');
  });
});

describe('the toast', () => {
  it('sits 24px up on the opening screen, at every width', () => {
    expect(get('.cd-toast', 'bottom')).toBe('calc(24px + env(safe-area-inset-bottom))');
    const elsewhere = all.filter((r) => r.selector === '.cd-toast' && r.media !== null && r.decls.has('bottom'));
    expect(elsewhere).toEqual([]);
  });

  it('rises to 76px on a map on phones, landscape phones and tablets, and on desktop only while the quick switch is out', () => {
    // Narrow is phones and tablets; short is landscape phones, which
    // includes a wide window under 500 tall such as 1280x480.
    expect(get('.cd-toast-map', 'bottom', NARROW)).toBe('calc(76px + env(safe-area-inset-bottom))');
    expect(get('.cd-toast-map', 'bottom', SHORT)).toBe('calc(76px + env(safe-area-inset-bottom))');
    const others = all.filter((r) => r.selector === '.cd-toast-map' && r.media !== NARROW && r.media !== SHORT);
    expect(others).toEqual([]);
    // The quick switch widens View's pill to where a long toast would
    // lie over it on a desktop window near 1024 wide.
    expect(get('[data-quick] .cd-toast-map', 'bottom')).toBe('calc(76px + env(safe-area-inset-bottom))');
  });

  it('keeps its action 36 to the eye and 44 to a finger', () => {
    expect(get('.cd-toast-action', 'height')).toBe('36px');
    const band = get('.cd-toast-action::before', 'inset', TOUCH);
    expect(36 - 2 * px(band)).toBe(44);
    // The band fits inside the toast, which is at least 46 high.
    expect(px(get('.cd-toast', 'min-height'))).toBeGreaterThanOrEqual(44);
  });
});
