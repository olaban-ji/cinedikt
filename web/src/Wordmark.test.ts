import { describe, expect, it } from 'vitest';
import css from './grid.css?raw';

/** The declarations of the one top-level rule with exactly this
 *  selector, comments gone. */
function decls(selector: string): Map<string, string> {
  const text = css.replace(/\/\*[\s\S]*?\*\//g, '');
  const found = new Map<string, string>();
  for (const m of text.matchAll(/([^{}]+)\{([^{}]*)\}/g)) {
    if (m[1].trim() !== selector) continue;
    for (const d of m[2].split(';')) {
      const at = d.indexOf(':');
      if (at > 0) found.set(d.slice(0, at).trim(), d.slice(at + 1).trim());
    }
  }
  return found;
}

describe('the mark', () => {
  it('draws past its box, in the header and as the loader', () => {
    // The bowl's outside is the bottom of the viewBox (38 + 14 + 2.5 =
    // 54.5), so an SVG clipped to its box shaves the bottom off the C
    // whenever the box falls between device pixels. The loader lands as
    // the header's mark, so the two have to be drawn the same way.
    expect(decls('.cd-mark').get('overflow')).toBe('visible');
    expect(decls('.cd-cold-mark-svg').get('overflow')).toBe('visible');
  });
});
