import { createElement } from 'react';
import { renderToStaticMarkup } from 'react-dom/server';
import { describe, expect, it } from 'vitest';
import css from './grid.css?raw';
import { FaceView, PersonFace, alreadyLoaded, faceAfter, faceStart, photoArrived, type FaceState } from './PersonFace';

const KEANU = 'https://image.tmdb.org/t/p/w185/keanu.jpg';
const OTHER = 'https://image.tmdb.org/t/p/w185/other.jpg';

/** The face as its first render draws it. */
function face(over: { photo?: string; square?: boolean; size?: 'chip' | 'preview' | 'sheet' } = {}): string {
  return renderToStaticMarkup(
    createElement(PersonFace, { code: 'KR', size: 'chip', square: false, ...over }),
  );
}

/** The face drawn in a given state. */
function drawn(state: FaceState, square = false): string {
  return renderToStaticMarkup(
    createElement(FaceView, { photo: state.photo, code: 'KR', size: 'chip', square, face: state }),
  );
}

describe('a person’s face', () => {
  it('is the initials alone, and no image, without a photo', () => {
    const html = face();
    expect(html).toBe(
      '<span class="cd-face cd-face-chip" aria-hidden="true"><span class="cd-face-initials">KR</span></span>',
    );
    expect(html).not.toContain('<img');
    expect(html).not.toContain('cd-face-ring');
  });

  it('keeps the initials showing until the photo has loaded, then takes cd-face-in', () => {
    const html = face({ photo: KEANU });
    // Decorative: the chip or the row around it names the person. (A
    // server render puts a preload for the image before it, which the
    // browser's own render does not.)
    expect(html).toContain(
      '<span class="cd-face cd-face-chip" aria-hidden="true"><span class="cd-face-initials">KR</span>' +
        `<span class="cd-face-ring"></span><img class="cd-face-photo" src="${KEANU}" alt="" decoding="async"/></span>`,
    );
    const loaded = faceAfter(faceStart(KEANU, new Set()), { type: 'load', photo: KEANU });
    expect(loaded).toEqual({ photo: KEANU, loaded: true, failed: false });
    expect(drawn(loaded)).toContain('class="cd-face cd-face-chip cd-face-in"');
    // The initials stay in the box, faded out by the stylesheet, under the photo.
    expect(drawn(loaded)).toContain('<span class="cd-face-initials">KR</span>');
  });

  it('drops the image and keeps the initials after an error', () => {
    const failed = faceAfter(faceStart(KEANU, new Set()), { type: 'error', photo: KEANU });
    expect(failed).toEqual({ photo: KEANU, loaded: false, failed: true });
    const html = drawn(failed);
    expect(html).toBe(
      '<span class="cd-face cd-face-chip" aria-hidden="true"><span class="cd-face-initials">KR</span></span>',
    );
    // A load that somehow follows does not bring it back.
    expect(faceAfter(failed, { type: 'load', photo: KEANU })).toBe(failed);
  });

  it('is a rounded square for square, a circle otherwise', () => {
    expect(face({ square: true })).toContain('class="cd-face cd-face-chip cd-face-square"');
    expect(face({ square: false })).not.toContain('cd-face-square');
    expect(face({ size: 'sheet' })).toContain('class="cd-face cd-face-sheet"');
    expect(face({ size: 'preview', square: true })).toContain('class="cd-face cd-face-preview cd-face-square"');
  });

  it('starts again, neither loaded nor failed, for a new address', () => {
    const none = new Set<string>();
    const loaded = faceAfter(faceStart(KEANU, none), { type: 'load', photo: KEANU });
    const failed = faceAfter(faceStart(KEANU, none), { type: 'error', photo: KEANU });
    for (const was of [loaded, failed]) {
      const now = faceAfter(was, { type: 'photo', photo: OTHER });
      expect(now.photo).toBe(OTHER);
      expect(now.loaded).toBe(false);
      expect(now.failed).toBe(false);
    }
    // And the same address changes nothing.
    expect(faceAfter(loaded, { type: 'photo', photo: KEANU })).toBe(loaded);
  });

  it('ignores an image’s events for an address it has moved on from', () => {
    const now = faceStart(OTHER, new Set());
    expect(faceAfter(now, { type: 'load', photo: KEANU })).toBe(now);
    expect(faceAfter(now, { type: 'error', photo: KEANU })).toBe(now);
  });

  it('starts with the photo showing when its address has loaded this visit', () => {
    expect(faceStart(KEANU, new Set([KEANU]))).toEqual({ photo: KEANU, loaded: true, failed: false });
    expect(faceStart(OTHER, new Set([KEANU]))).toEqual({ photo: OTHER, loaded: false, failed: false });
    expect(faceStart(undefined, new Set([KEANU]))).toEqual({ photo: undefined, loaded: false, failed: false });
  });

  it('draws a photo that has loaded this visit as shown from the first render, wherever it appears next', () => {
    const SEEN = 'https://image.tmdb.org/t/p/w185/seen.jpg';
    expect(face({ photo: SEEN })).toContain('class="cd-face cd-face-chip"');
    photoArrived(SEEN);
    expect(faceStart(SEEN)).toEqual({ photo: SEEN, loaded: true, failed: false });
    expect(face({ photo: SEEN })).toContain('class="cd-face cd-face-chip cd-face-in"');
    expect(face({ photo: SEEN, size: 'preview' })).toContain('class="cd-face cd-face-preview cd-face-in"');
  });
});

describe('alreadyLoaded', () => {
  it('takes an image the browser already has in hand as loaded', () => {
    expect(alreadyLoaded({ complete: true, naturalWidth: 185 })).toBe(true);
  });

  it('does not take one still on its way, one that failed, or none at all', () => {
    expect(alreadyLoaded({ complete: false, naturalWidth: 0 })).toBe(false);
    // A failed image is complete too, with nothing in it.
    expect(alreadyLoaded({ complete: true, naturalWidth: 0 })).toBe(false);
    expect(alreadyLoaded(null)).toBe(false);
  });
});

/** The declarations of every rule with exactly this selector, @media
 *  ones included, later ones winning. */
function decls(selector: string, sheet = css): Map<string, string> {
  const text = sheet.replace(/\/\*[\s\S]*?\*\//g, '');
  const out = new Map<string, string>();
  for (const m of text.matchAll(/([^{}]+)\{([^{}]*)\}/g)) {
    if (!m[1].split(',').map((x) => x.trim()).includes(selector)) continue;
    for (const d of m[2].split(';')) {
      const at = d.indexOf(':');
      if (at > 0) out.set(d.slice(0, at).trim(), d.slice(at + 1).trim());
    }
  }
  return out;
}

/** Every @media block in the sheet: its query and its body. */
function mediaBlocks(): { query: string; body: string; from: number; to: number }[] {
  const out: { query: string; body: string; from: number; to: number }[] = [];
  for (let at = css.indexOf('@media'); at >= 0; at = css.indexOf('@media', at + 1)) {
    const open = css.indexOf('{', at);
    let depth = 0;
    for (let i = open; i < css.length; i++) {
      if (css[i] === '{') depth++;
      else if (css[i] === '}' && --depth === 0) {
        out.push({ query: css.slice(at + 7, open).trim(), body: css.slice(open + 1, i), from: at, to: i + 1 });
        at = i;
        break;
      }
    }
  }
  return out;
}

/** The sheet as it applies with no @media query matching. */
function outside(): string {
  let text = '';
  let from = 0;
  for (const b of mediaBlocks()) {
    text += css.slice(from, b.from);
    from = b.to;
  }
  return text + css.slice(from);
}

const px = (v: string | undefined) => Number(v?.match(/^(-?[\d.]+)px$/)?.[1] ?? NaN);

describe('the face’s sizes', () => {
  // The handoff's table: the box, a 2px ring, the gap inside it, and the
  // photo in the middle; a director's corner on the box and the photo.
  const sizes = [
    { selector: '.cd-face-chip', box: 28, gap: 1.5, photo: 21, corner: 8, photoCorner: 4.5, fontSize: '10.5px' },
    { selector: '.cd-face-preview', box: 30, gap: 1.5, photo: 23, corner: 9, photoCorner: 5.5, fontSize: '10.5px' },
    { selector: '.cd-face-sheet', box: 44, gap: 2.5, photo: 35, corner: null, photoCorner: null, fontSize: '13px' },
  ];

  it('are the handoff’s, with the photo inside a 2px ring and its gap', () => {
    const base = outside();
    expect(decls('.cd-face-ring', base).get('box-shadow')).toBe('inset 0 0 0 2px var(--tone)');
    for (const s of sizes) {
      const d = decls(s.selector, base);
      const inset = px(d.get('--face-inset'));
      expect(px(d.get('--face')), s.selector).toBe(s.box);
      expect(inset, s.selector).toBe(2 + s.gap);
      expect(s.box - 2 * inset, s.selector).toBe(s.photo);
      expect(d.get('font-size'), s.selector).toBe(s.fontSize);
      if (s.corner == null) {
        // The panel draws everyone round: the role line says who directed.
        expect(d.has('--face-r'), s.selector).toBe(false);
      } else {
        expect(px(d.get('--face-r')), s.selector).toBe(s.corner);
        expect(s.corner - inset, s.selector).toBe(s.photoCorner);
      }
    }
    expect(decls('.cd-face-square .cd-face-photo').get('border-radius')).toBe(
      'calc(var(--face-r) - var(--face-inset))',
    );
  });

  it('grows to 38px in a chip that is a thumb’s target, 3px inside it all round', () => {
    // In the very block that makes the chip 44px tall.
    const touch = mediaBlocks().filter((b) => px(decls('.cd-chip', b.body).get('height')) === 44);
    expect(touch).toHaveLength(1);
    expect(touch[0].query).toBe('(max-width: 1023.98px), (max-height: 499.98px), (pointer: coarse)');
    const chip = decls('.cd-face-chip', touch[0].body);
    expect(px(chip.get('--face'))).toBe(38);
    expect(chip.get('font-size')).toBe('12.5px');
    const base = outside();
    // The same ring and gap, so a 31px photo.
    expect(38 - 2 * px(decls('.cd-face-chip', base).get('--face-inset'))).toBe(31);
    // The chip's padding and height put a 28px face 3px inside it, and
    // 44 − 38 does the same on touch.
    expect(decls('.cd-chip', base).get('padding')).toBe('0 12px 0 3px');
    expect(px(decls('.cd-chip', base).get('height')) - px(decls('.cd-face-chip', base).get('--face'))).toBe(6);
  });

  it('cross-fades over 240ms, at once for a reader who asked for nothing to move', () => {
    const base = outside();
    expect(decls('.cd-face', base).get('--face-fade')).toBe('240ms');
    for (const part of ['.cd-face-initials', '.cd-face-ring', '.cd-face-photo']) {
      expect(decls(part, base).get('transition'), part).toBe('opacity var(--face-fade) ease');
    }
    // Before: the initials, and neither the ring nor the photo.
    expect(decls('.cd-face-ring', base).get('opacity')).toBe('0');
    expect(decls('.cd-face-photo', base).get('opacity')).toBe('0');
    // After: the other way about.
    expect(decls('.cd-face-in .cd-face-initials', base).get('opacity')).toBe('0');
    expect(decls('.cd-face-in .cd-face-photo', base).get('opacity')).toBe('1');
    expect(decls('.cd-face-in .cd-face-ring', base).get('opacity')).toBe('1');
    const still = mediaBlocks().filter((b) => b.query === '(prefers-reduced-motion: reduce)' && decls('.cd-face', b.body).size > 0);
    expect(still.map((b) => decls('.cd-face', b.body).get('--face-fade'))).toEqual(['0ms']);
  });

  it('brings the ring back as a border under forced colours, which drop box-shadow', () => {
    const forced = mediaBlocks().filter((b) => b.query === '(forced-colors: active)' && decls('.cd-face-ring', b.body).size > 0);
    expect(forced).toHaveLength(1);
    const ring = decls('.cd-face-ring', forced[0].body);
    expect(ring.get('box-shadow')).toBe('none');
    expect(ring.get('border')).toBe('2px solid CanvasText');
  });
});
