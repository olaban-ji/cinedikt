import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import css from './grid.css?raw';
import {
  PREVIEW_BACK_MS,
  PREVIEW_DRIFT_EXTRA_MS,
  PREVIEW_EDGE,
  PREVIEW_FLOAT_CLEAR,
  PREVIEW_FLOAT_CLEAR_TOUCH,
  PREVIEW_GAP,
  PREVIEW_LEAVE_MS,
  PREVIEW_LEAVE_PLAYING_MS,
  PREVIEW_OUT_MS,
  PREVIEW_OUT_PLAYING_MS,
  PREVIEW_RAIL_GAP,
  PREVIEW_REST_MS,
  PREVIEW_ROOM,
  PREVIEW_SWAP_MS,
  PREVIEW_SWAP_OUT_MS,
  PREVIEW_TOP,
  PREVIEW_TOP_LOOSE,
  PREVIEW_W,
  PREVIEW_WARM_MS,
  SYN_GAP,
  SYN_LINE_H,
  SYN_LINES,
  SYN_SETTLE_MS,
  fitTrailer,
  holdInside,
  leaveDelay,
  placePreview,
  previewBounds,
  previewFloatClear,
  previewScheduler,
  restDelay,
  type MapView,
  type PreviewNow,
} from './preview';
import { screenOf } from './screen';
import { PLAYER, TRAILER_CLOSE_MS, TRAILER_OPEN_MS, videoHeight, wellHeight } from './trailer';

/** The declarations of every rule with exactly this selector, @media
 *  ones included unless `media` is false, later ones winning. */
function decls(selector: string, media = true): Map<string, string> {
  let text = css.replace(/\/\*[\s\S]*?\*\//g, '');
  if (!media) text = text.replace(/@media[^{]*\{(?:[^{}]*\{[^{}]*\})*[^{}]*\}/g, '');
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

/** The desktop card. */
const CARD = { cardW: 168, cardH: 90 };
/** A plot tall enough for any of these. */
const PLOT_H = 20000;

/** A desktop map's scroller at 1440×900: the whole width, and the height
 *  under the 114px header. */
const wide: MapView = { scrollLeft: 0, scrollTop: 1000, clientWidth: 1440, clientHeight: 786 };
const RAIL = 72;
const at = (view: MapView, loose = false) => previewBounds(view, RAIL, 0, 0, PREVIEW_FLOAT_CLEAR, loose);
/** The same on a screen sized for a finger, whose floating buttons are
 *  46px tall. */
const atTouch = (view: MapView, loose = false) =>
  previewBounds(view, RAIL, 0, 0, PREVIEW_FLOAT_CLEAR_TOUCH, loose);

describe('the room a preview has', () => {
  it('starts past the year rail and stops short of the right edge', () => {
    const b = at(wide);
    expect(b.x0).toBe(RAIL + PREVIEW_RAIL_GAP);
    expect(b.x1).toBe(1440 - PREVIEW_EDGE);
    expect(at({ ...wide, scrollLeft: 300 }).x0).toBe(300 + RAIL + 4);
  });

  it('keeps below the pinned rating axis and above the strip View and Recenter float in', () => {
    const b = at(wide);
    expect(PREVIEW_TOP).toBe(26 + 8);
    expect(b.vt).toBe(1000 + 34);
    expect(b.vb).toBe(1000 + 786 - 68);
  });

  it('takes the strip as the buttons’ 16px offset, their height and 10px clear', () => {
    expect(PREVIEW_FLOAT_CLEAR).toBe(16 + 42 + 10);
    expect(PREVIEW_FLOAT_CLEAR_TOUCH).toBe(16 + 46 + 10);
    expect(previewFloatClear(false)).toBe(68);
    expect(previewFloatClear(true)).toBe(72);
  });

  it('takes the taller strip wherever the buttons are a finger’s size', () => {
    // A desktop with a mouse has 42px buttons; a tablet with a trackpad,
    // and a desktop-wide screen with a coarse pointer, have 46px ones.
    expect(previewFloatClear(screenOf(1440, 900).touch)).toBe(68);
    expect(previewFloatClear(screenOf(1366, 768).touch)).toBe(68);
    expect(previewFloatClear(screenOf(1180, 820).touch)).toBe(68);
    expect(previewFloatClear(screenOf(1023, 768).touch)).toBe(72);
    expect(previewFloatClear(screenOf(1366, 1024, true).touch)).toBe(72);
    expect(previewFloatClear(screenOf(1280, 480).touch)).toBe(72);
    expect(atTouch(wide).vb).toBe(1000 + 786 - 72);
  });

  it('keeps its sides where they were: the strip is only along the bottom', () => {
    for (const b of [at(wide), atTouch(wide)]) {
      expect(b.x0).toBe(RAIL + PREVIEW_RAIL_GAP);
      expect(b.x1).toBe(1440 - PREVIEW_EDGE);
      expect(b.vt).toBe(1000 + PREVIEW_TOP);
    }
  });

  it('may rise over the axis once a trailer needs the room', () => {
    expect(PREVIEW_TOP_LOOSE).toBe(8);
    expect(at(wide, true).vt).toBe(1000 + 8);
    expect(at(wide, true).vb).toBe(at(wide).vb);
  });

  it('keeps below a header lying over the map while it shows, and not once it has gone up', () => {
    // A phone's plot starts 116px down the scroller, under the header.
    const phone: MapView = { scrollLeft: 0, scrollTop: 1000, clientWidth: 390, clientHeight: 844 };
    const shown = previewBounds(phone, 52, 116, 116, PREVIEW_FLOAT_CLEAR_TOUCH);
    const away = previewBounds(phone, 52, 116, 0, PREVIEW_FLOAT_CLEAR_TOUCH);
    // On screen: 34 below the header's foot, or 34 below the top.
    expect(shown.vt - (1000 - 116)).toBe(116 + 34);
    expect(away.vt - (1000 - 116)).toBe(34);
    expect(shown.vb).toBe(1000 - 116 + 844 - 72);
  });
});

describe('where a preview goes across', () => {
  it('sits 8px to the right of the card when there is room', () => {
    const p = placePreview({ left: 648, top: 1400 }, CARD, at(wide), PLOT_H);
    expect(p.side).toBe(1);
    expect(p.x).toBe(648 + 168 + PREVIEW_GAP);
  });

  it('sits 8px to its left when the right has no room, as The Goonies’ does at 1440', () => {
    const p = placePreview({ left: 995, top: 1400 }, CARD, at(wide), PLOT_H);
    expect(p.side).toBe(-1);
    expect(p.x).toBe(995 - PREVIEW_GAP - PREVIEW_W);
    expect(p.x).toBe(627);
  });

  it('goes on the side with more room when neither fits, held inside the map', () => {
    const narrow: MapView = { ...wide, clientWidth: 700 };
    const b = at(narrow);
    // More room on the left: held against the rail.
    const left = placePreview({ left: 300, top: 1400 }, CARD, b, PLOT_H);
    expect(left.side).toBe(-1);
    expect(left.x).toBe(b.x0);
    // More room on the right: held against the right edge.
    const right = placePreview({ left: 200, top: 1400 }, CARD, b, PLOT_H);
    expect(right.side).toBe(1);
    expect(right.x).toBe(b.x1 - PREVIEW_W);
  });

  it('is clamped between the rail and the right edge as the map scrolls sideways', () => {
    const panned: MapView = { ...wide, clientWidth: 900, scrollLeft: 400 };
    const b = at(panned);
    // A card half under the rail with no room to its left.
    const under = placePreview({ left: 420, top: 1400 }, CARD, b, PLOT_H);
    expect(under.side).toBe(1);
    expect(under.x).toBe(420 + 168 + 8);
    // A card near the right edge, whose left would run under the rail.
    const edge = placePreview({ left: 1050, top: 1400 }, CARD, b, PLOT_H);
    expect(edge.x).toBeGreaterThanOrEqual(b.x0);
    expect(edge.x + PREVIEW_W).toBeLessThanOrEqual(b.x1);
  });
});

describe('which way a preview grows', () => {
  const b = at(wide);

  it('grows down from the card’s top with 300px below it', () => {
    expect(PREVIEW_ROOM).toBe(300);
    const p = placePreview({ left: 648, top: b.vb - 300 }, CARD, b, PLOT_H);
    expect(p).toMatchObject({ top: b.vb - 300, bottom: null });
  });

  it('grows up from the card’s bottom with less below it and more above', () => {
    const top = b.vb - 299;
    const p = placePreview({ left: 648, top }, CARD, b, PLOT_H);
    expect(p).toMatchObject({ top: null, bottom: PLOT_H - (top + 90) });
  });

  it('grows down in a short window when there is more room below than above', () => {
    // 924×600: the scroller is 478 tall under the header, and the
    // floating buttons are a finger's size.
    const short = atTouch({ ...wide, clientWidth: 924, clientHeight: 478 });
    const high = placePreview({ left: 300, top: 1000 + 150 }, CARD, short, PLOT_H);
    expect(high.top).toBe(1150);
    const low = placePreview({ left: 300, top: 1000 + 220 }, CARD, short, PLOT_H);
    expect(low.top).toBeNull();
  });

  it('keeps its top below the axis for a card partly under it', () => {
    const p = placePreview({ left: 648, top: b.vt - 20 }, CARD, b, PLOT_H);
    expect(p.top).toBe(b.vt);
  });

  it('keeps its bottom above the floating buttons’ strip for a card reaching into it', () => {
    const p = placePreview({ left: 648, top: b.vb - 40 }, CARD, b, PLOT_H);
    expect(p.bottom).toBe(PLOT_H - b.vb);
  });
});

describe('holdInside', () => {
  const b = at({ ...wide, clientHeight: 418 });

  it('leaves a preview that fits where it is', () => {
    const down = { x: 0, side: 1 as const, top: b.vt + 10, bottom: null };
    expect(holdInside(down, 200, b, PLOT_H)).toBe(down);
    const up = { x: 0, side: 1 as const, top: null, bottom: PLOT_H - b.vb };
    expect(holdInside(up, 200, b, PLOT_H)).toBe(up);
  });

  it('moves one growing down into the floating buttons’ strip back up, no higher than the axis allows', () => {
    const p = holdInside({ x: 0, side: 1, top: b.vt + 150, bottom: null }, 264.1875, b, PLOT_H);
    expect(p.top).toBe(Math.floor(b.vb - 264.1875));
    expect(p.top! + 264.1875).toBeLessThanOrEqual(b.vb);
    const tall = holdInside({ x: 0, side: 1, top: b.vt + 150, bottom: null }, 1000, b, PLOT_H);
    expect(tall.top).toBe(b.vt);
  });

  it('holds one growing up past the axis by its top instead', () => {
    const p = holdInside({ x: 0, side: -1, top: null, bottom: PLOT_H - (b.vt + 120) }, 200, b, PLOT_H);
    expect(p).toEqual({ x: 0, side: -1, top: b.vt, bottom: null });
  });
});

describe('fitTrailer', () => {
  // The preview's trailer: 360 wide, so 202.5 tall, in a well 6px under
  // the row, taking over the box's 16px of bottom padding.
  const GROW = wellHeight('preview', videoHeight(PREVIEW_W)) - PLAYER.preview.pad;
  // The preview at 1440 with a three-line synopsis, and with six.
  const THREE = 203.4375;
  const SIX = THREE + 3 * SYN_LINE_H;
  /** A tablet's scroller `h` tall, scrolled to 1000, whose floating
   *  buttons are a finger's size. */
  const room = (h: number) => {
    const view = { ...wide, clientWidth: 924, clientHeight: h };
    return [atTouch(view), atTouch(view, true)] as const;
  };
  const now = (height: number, y0: number, synH: number | null = height - THREE + 3 * SYN_LINE_H): PreviewNow => ({
    y0,
    height,
    grow: GROW,
    synH,
  });

  it('grows the preview by the well, less the padding the video takes over', () => {
    expect(GROW).toBe(6 + 202.5 - 16);
  });

  it('leaves a preview where it is when the grown box fits below it', () => {
    const [strict, loose] = room(786);
    expect(fitTrailer(now(THREE, 1100), strict, loose)).toEqual({ top: 1100, synLines: null });
  });

  it('first glides its top up, just far enough', () => {
    // 924×828: room for the grown box, but not from where it is.
    const [strict, loose] = room(706);
    const fit = fitTrailer(now(THREE, 1400), strict, loose);
    expect(fit.synLines).toBeNull();
    expect(fit.top).toBe(Math.floor(strict.vb - (THREE + GROW)));
    expect(fit.top).toBeGreaterThanOrEqual(strict.vt);
  });

  it('then rises over the pinned axis, in a short window', () => {
    // 924×600: 372 between the axis and the floating buttons' strip is
    // too little for the grown box; 398 from 8px down is enough.
    const [strict, loose] = room(478);
    expect(strict.vb - strict.vt).toBe(372);
    expect(strict.vb - loose.vt).toBe(398);
    expect(strict.vb - strict.vt).toBeLessThan(THREE + GROW);
    expect(strict.vb - loose.vt).toBeGreaterThanOrEqual(THREE + GROW);
    const fit = fitTrailer(now(THREE, 1164), strict, loose);
    expect(fit.synLines).toBeNull();
    expect(fit.top).toBe(Math.floor(strict.vb - (THREE + GROW)));
    expect(fit.top).toBeLessThan(strict.vt);
    expect(fit.top).toBeGreaterThanOrEqual(loose.vt);
  });

  it('then folds the synopsis to as many whole lines as fit', () => {
    // Six lines do not fit even over the axis at 924×600; three do.
    const [strict, loose] = room(478);
    const fit = fitTrailer(now(SIX, 1164, SYN_LINES * SYN_LINE_H), strict, loose);
    expect(fit.synLines).toBe(3);
    const grown = SIX - SYN_LINES * SYN_LINE_H + 3 * SYN_LINE_H + GROW;
    expect(fit.top).toBe(Math.floor(strict.vb - grown));
    expect(fit.top).toBeGreaterThanOrEqual(loose.vt);
  });

  it('then folds it away entirely, taking back the gap it sat in', () => {
    const [strict, loose] = room(420);
    const fit = fitTrailer(now(THREE, 1100, 3 * SYN_LINE_H), strict, loose);
    expect(fit.synLines).toBe(0);
    const grown = THREE - 3 * SYN_LINE_H - SYN_GAP + GROW;
    expect(fit.top).toBe(Math.floor(strict.vb - grown));
    expect(fit.top).toBeGreaterThanOrEqual(loose.vt);
  });

  it('folds away "No synopsis yet." as one line, which has no fewer lines to fold to', () => {
    const none = THREE - 2 * SYN_LINE_H;
    const [strict, loose] = room(330);
    expect(fitTrailer(now(none, 1100, SYN_LINE_H), strict, loose).synLines).toBe(0);
  });

  it('makes room in that order as the window gets shorter', () => {
    const stage = (h: number) => {
      const [strict, loose] = room(h);
      const fit = fitTrailer(now(SIX, 1000 + 150, SYN_LINES * SYN_LINE_H), strict, loose);
      if (fit.synLines === 0) return 'no synopsis';
      if (fit.synLines != null) return 'synopsis lines';
      if (fit.top < strict.vt) return 'over the axis';
      return fit.top < 1150 ? 'glide' : 'as it is';
    };
    const seen: string[] = [];
    for (let h = 900; h >= 300; h -= 2) {
      const s = stage(h);
      if (seen[seen.length - 1] !== s) seen.push(s);
    }
    expect(seen).toEqual(['as it is', 'glide', 'over the axis', 'synopsis lines', 'no synopsis']);
  });

  it('never leaves the grown box in the floating buttons’ strip while it can help it', () => {
    // Down to the shortest window the box fits at with its synopsis
    // folded away: 8px above it, and the floating buttons' strip below.
    const least = Math.ceil(THREE - 3 * SYN_LINE_H - SYN_GAP + GROW + PREVIEW_TOP_LOOSE + PREVIEW_FLOAT_CLEAR_TOUCH);
    for (let h = 900; h >= least; h -= 3) {
      const [strict, loose] = room(h);
      for (const [height, synH] of [
        [THREE, 3 * SYN_LINE_H],
        [SIX, SYN_LINES * SYN_LINE_H],
      ]) {
        const fit = fitTrailer(now(height, 1000 + 120, synH), strict, loose);
        const folded = fit.synLines == null ? height : height - synH + (fit.synLines ? fit.synLines * SYN_LINE_H : -SYN_GAP);
        expect(fit.top + folded + GROW, `${h}`).toBeLessThanOrEqual(strict.vb);
        expect(fit.top, `${h}`).toBeGreaterThanOrEqual(loose.vt);
      }
    }
  });
});

describe('the strip View and Recenter float in', () => {
  // The desktop windows the handoff checks by hand, and a shorter one,
  // under the 114px header; and a tablet with a trackpad, under its
  // 122px one, where a pointer can rest on a card and the buttons are a
  // finger's size.
  const windows = [
    { name: '1366×768', view: { ...wide, clientWidth: 1366, clientHeight: 654 }, clear: previewFloatClear(false) },
    { name: '1440×900', view: wide, clear: previewFloatClear(false) },
    { name: '1280×600', view: { ...wide, clientWidth: 1280, clientHeight: 486 }, clear: previewFloatClear(false) },
    { name: '924×828', view: { ...wide, clientWidth: 924, clientHeight: 706 }, clear: previewFloatClear(true) },
  ];
  /** A card near the map's right edge, whose preview, to its right, ends
   *  50px from that edge, over Recenter's pill, which sits 16px in. */
  const nearRight = (view: MapView, top: number) => ({
    left: view.clientWidth - 50 - PREVIEW_W - PREVIEW_GAP - CARD.cardW,
    top,
  });
  // The preview at 1440 with three lines of synopsis and no faces, and
  // with six and the row of faces under its head.
  const SMALL = 203.4375;
  const TALL = SMALL + 3 * SYN_LINE_H + 42;
  const shapes = [
    { height: SMALL, synH: 3 * SYN_LINE_H },
    { height: TALL, synH: SYN_LINES * SYN_LINE_H },
  ];
  const GROW = wellHeight('preview', videoHeight(PREVIEW_W)) - PLAYER.preview.pad;

  it('holds the preview of a card whose top is in the map’s bottom 300px by its bottom, at vb', () => {
    for (const { name, view, clear } of windows) {
      const b = previewBounds(view, RAIL, 0, 0, clear);
      const foot = view.scrollTop + view.clientHeight;
      for (let top = foot - 300; top < foot; top++) {
        const p = placePreview(nearRight(view, top), CARD, b, PLOT_H);
        expect(p.x + PREVIEW_W, `${name}, ${top}`).toBe(view.clientWidth - 50);
        expect(p.top, `${name}, ${top}`).toBeNull();
        // Its bottom is the card's, or vb for a card reaching past it:
        // never in the strip.
        const end = PLOT_H - p.bottom!;
        expect(end, `${name}, ${top}`).toBe(Math.min(top + CARD.cardH, b.vb));
        expect(foot - end, `${name}, ${top}`).toBeGreaterThanOrEqual(clear);
        if (top + CARD.cardH >= b.vb) expect(end, `${name}, ${top}`).toBe(b.vb);
        // And there it stays once its height is known, unless the room
        // above is too little for it, when it is held by its top instead,
        // still ending above the strip.
        for (const { height } of shapes) {
          const held = holdInside(p, height, b, PLOT_H);
          if (end - height >= b.vt) expect(held, `${name}, ${top}`).toBe(p);
          else expect(held.top! + height, `${name}, ${top}`).toBeLessThanOrEqual(b.vb);
        }
      }
    }
  });

  it('never lets the box end below vb, with its trailer open or not', () => {
    for (const { name, view, clear } of windows) {
      const strict = previewBounds(view, RAIL, 0, 0, clear);
      const loose = previewBounds(view, RAIL, 0, 0, clear, true);
      const foot = view.scrollTop + view.clientHeight;
      for (let top = view.scrollTop - CARD.cardH; top < foot; top += 5) {
        for (const { height, synH } of shapes) {
          const place = holdInside(placePreview(nearRight(view, top), CARD, strict, PLOT_H), height, strict, PLOT_H);
          const y0 = place.top ?? PLOT_H - place.bottom! - height;
          expect(y0 + height, `${name}, ${top}`).toBeLessThanOrEqual(strict.vb);
          const fit = fitTrailer({ y0, height, grow: GROW, synH }, strict, loose);
          const folded =
            fit.synLines == null ? height : height - synH + (fit.synLines ? fit.synLines * SYN_LINE_H : -SYN_GAP);
          const end = fit.top + folded + GROW;
          expect(end, `${name}, ${top}`).toBeLessThanOrEqual(strict.vb);
          // So the player ends 10px above the buttons' tops, 16 + their
          // height up from the foot.
          expect(foot - (clear - 10) - end, `${name}, ${top}`).toBeGreaterThanOrEqual(10);
        }
      }
    }
  });
});

describe('the preview’s timings', () => {
  it('are the ones the handoff gives', () => {
    expect(PREVIEW_REST_MS).toBe(480);
    expect(PREVIEW_SWAP_MS).toBe(90);
    expect(PREVIEW_WARM_MS).toBe(350);
    expect(PREVIEW_LEAVE_MS).toBe(160);
    expect(PREVIEW_LEAVE_PLAYING_MS).toBe(450);
    expect(SYN_SETTLE_MS).toBe(540);
    expect(PREVIEW_OUT_MS).toBe(280);
    expect(PREVIEW_OUT_PLAYING_MS).toBe(360);
    expect(PREVIEW_SWAP_OUT_MS).toBe(200);
    expect(PREVIEW_DRIFT_EXTRA_MS).toBe(40);
    expect(PREVIEW_BACK_MS).toBe(240);
  });

  it('let the synopsis finish folding before its clamp moves', () => {
    expect(SYN_SETTLE_MS).toBeGreaterThan(TRAILER_OPEN_MS);
  });

  it('wait the full rest from cold, and swap quickly from a preview showing or just closed', () => {
    expect(restDelay(false, Infinity, false)).toBe(480);
    expect(restDelay(true, Infinity, false)).toBe(90);
    expect(restDelay(false, 349, false)).toBe(90);
    expect(restDelay(false, 350, false)).toBe(480);
  });

  it('never swap quickly while the trailer plays', () => {
    expect(restDelay(true, 0, true)).toBe(480);
  });

  it('close a little later while the trailer plays', () => {
    expect(leaveDelay(false)).toBe(160);
    expect(leaveDelay(true)).toBe(450);
  });
});

describe('the preview as the stylesheet draws it', () => {
  const box = decls('.cd-preview', false);

  it('is the width, padding and gap the layout is worked out with', () => {
    expect(px(box.get('width'))).toBe(PREVIEW_W);
    expect(box.get('box-sizing')).toBe('border-box');
    expect(px(box.get('padding'))).toBe(PLAYER.preview.pad);
    expect(px(box.get('gap'))).toBe(SYN_GAP);
    expect(px(box.get('border-radius'))).toBe(16);
    expect(box.get('box-shadow')).toBe('inset 0 0 0 1px var(--ln2), var(--pop)');
    expect(box.get('overflow')).toBe('hidden');
  });

  it('sits above the cards, the axis and the rail, and under the header', () => {
    const z = Number(box.get('z-index'));
    for (const sel of ['.cd-card', '.cd-card-anchor', '.cd-searched-tag', '.cd-axis', '.cd-rail-layer']) {
      expect(z, sel).toBeGreaterThan(Number(decls(sel, false).get('z-index')));
    }
    expect(z).toBeLessThan(Number(decls('.cd-header', false).get('z-index')));
  });

  it('leaves the way it came: fading where it stands and drifting 6px back towards its card, on the close curve', () => {
    expect(Object.fromEntries(decls('.cd-preview.cd-preview-out', false))).toEqual({
      opacity: '0',
      translate: '-6px 0',
      transition: `opacity ${PREVIEW_OUT_MS}ms var(--ease-close), translate ${PREVIEW_OUT_MS + PREVIEW_DRIFT_EXTRA_MS}ms var(--ease-close)`,
    });
    expect(Object.fromEntries(decls('.cd-preview-left.cd-preview-out', false))).toEqual({ translate: '6px 0' });
    expect(Object.fromEntries(decls('.cd-preview.cd-preview-out-playing', false))).toEqual({
      transition: `opacity ${PREVIEW_OUT_PLAYING_MS}ms var(--ease-close), translate ${PREVIEW_OUT_PLAYING_MS + PREVIEW_DRIFT_EXTRA_MS}ms var(--ease-close)`,
    });
    expect(Object.fromEntries(decls('.cd-preview.cd-preview-gone', false))).toEqual({
      'pointer-events': 'none',
      transition: `opacity ${PREVIEW_SWAP_OUT_MS}ms var(--ease-close), translate ${PREVIEW_SWAP_OUT_MS + PREVIEW_DRIFT_EXTRA_MS}ms var(--ease-close)`,
    });
  });

  it('puts the leaving rules after the trailer’s, so their transitions win', () => {
    const text = css.replace(/\/\*[\s\S]*?\*\//g, '');
    const at = (selector: string) => text.search(new RegExp(`(^|\\})\\s*${selector.replace(/\./g, '\\.')}\\s*\\{`));
    const open = at('.cd-preview-trailer-open');
    expect(at('.cd-preview-trailer')).toBeGreaterThan(0);
    expect(open).toBeGreaterThan(at('.cd-preview-trailer'));
    for (const sel of ['.cd-preview.cd-preview-out', '.cd-preview.cd-preview-out-playing', '.cd-preview.cd-preview-gone']) {
      expect(at(sel), sel).toBeGreaterThan(open);
    }
    expect(at('.cd-preview-left.cd-preview-out')).toBeGreaterThan(at('.cd-preview.cd-preview-out'));
  });

  it('comes in from 6px nearer its card', () => {
    expect(box.get('opacity')).toBe('0');
    expect(box.get('translate')).toBe('-6px 0');
    expect(decls('.cd-preview-left', false).get('translate')).toBe('6px 0');
    expect(decls('.cd-preview-in', false).get('translate')).toBe('none');
    expect(box.get('transition')).toBe('opacity 0.16s ease, translate 0.22s var(--ease-glide)');
  });

  it('glides its top on the player’s own timings', () => {
    expect(decls('.cd-preview-trailer', false).get('transition')).toContain(`top ${TRAILER_CLOSE_MS}ms var(--ease-close)`);
    expect(decls('.cd-preview-trailer-open', false).get('transition')).toContain(`top ${TRAILER_OPEN_MS}ms var(--ease-glide)`);
  });

  it('lets the genres take the year and rating line onto a second row, in its own type', () => {
    const meta = decls('.cd-preview-meta', false);
    expect(meta.get('flex-wrap')).toBe('wrap');
    expect(px(meta.get('gap'))).toBe(8);
    expect(px(meta.get('row-gap'))).toBe(4);
    expect(px(meta.get('font-size'))).toBe(12.5);
    expect(meta.get('color')).toBe('var(--t2)');
    // They inherit the line's size and colour, with no rule of their own.
    expect(decls('.cd-preview-genres').size).toBe(0);
  });

  it('draws the synopsis in the lines it is folded by', () => {
    const syn = decls('.cd-preview-syn', false);
    expect(px(syn.get('font-size')) * Number(syn.get('line-height'))).toBe(SYN_LINE_H);
    expect(px(syn.get('max-height'))).toBe(SYN_LINES * SYN_LINE_H);
    expect(Number(syn.get('-webkit-line-clamp'))).toBe(SYN_LINES);
    expect(px(decls('.cd-preview-syn-gone', false).get('margin-top'))).toBe(-SYN_GAP);
    expect(syn.get('transition')).toContain(`max-height ${TRAILER_CLOSE_MS}ms var(--ease-close)`);
    expect(decls('.cd-preview-trailer-open .cd-preview-syn', false).get('transition')).toContain(
      `max-height ${TRAILER_OPEN_MS}ms var(--ease-glide)`,
    );
  });

  it('keeps a hovered card’s look on the card whose preview the pointer is on', () => {
    const hover = decls('.cd-card-hover:not(.cd-card-anchor)');
    const real = decls('.cd-card:not(.cd-card-anchor):hover');
    expect(hover.get('box-shadow')).toBe(real.get('box-shadow'));
    expect(hover.get('z-index')).toBe(real.get('z-index'));
  });

  it('simply appears, and makes room at once, for a reader who has asked for nothing to move', () => {
    for (const sel of [
      '.cd-preview',
      '.cd-preview-trailer',
      '.cd-preview-trailer-open',
      '.cd-preview-syn',
      '.cd-preview-trailer-open .cd-preview-syn',
      '.cd-preview.cd-preview-out',
      '.cd-preview.cd-preview-out-playing',
      '.cd-preview.cd-preview-gone',
    ]) {
      expect(decls(sel).get('transition'), sel).toBe('none');
    }
  });
});

describe('the scheduler’s part in a preview leaving', () => {
  beforeEach(() => {
    vi.useFakeTimers();
    // The page's clock is the window's.
    vi.stubGlobal('window', globalThis);
  });
  afterEach(() => {
    vi.useRealTimers();
    vi.unstubAllGlobals();
  });

  /** A scheduler over a map showing this card's preview, on the page's
   *  own clock, recording what it asks. A leaving preview still counts
   *  as showing, so `showing` stands throughout. */
  function over(showing: string) {
    const did: string[] = [];
    const sched = previewScheduler({
      showing: () => showing,
      playing: () => false,
      open: (id) => did.push(`open ${id}`),
      close: (hard) => did.push(`close(${hard})`),
      keep: () => did.push('keep'),
    });
    return { did, sched };
  }

  it('asks for the preview back when the pointer rests on its card again', () => {
    const { did, sched } = over('a');
    sched.leave();
    vi.advanceTimersByTime(100);
    sched.rest('a');
    vi.advanceTimersByTime(5000);
    expect(did).toEqual(['keep']);
    sched.dispose();
  });

  it('asks for it back when the pointer moves onto the preview', () => {
    const { did, sched } = over('a');
    sched.leave();
    vi.advanceTimersByTime(100);
    sched.hold();
    vi.advanceTimersByTime(5000);
    expect(did).toEqual(['keep']);
    sched.dispose();
  });

  it('lets the preview leave softly when the pointer has gone, for a scroll and for Escape', () => {
    const { did, sched } = over('a');
    sched.leave();
    vi.advanceTimersByTime(PREVIEW_LEAVE_MS - 1);
    expect(did).toEqual([]);
    vi.advanceTimersByTime(1);
    expect(did).toEqual(['close(false)']);
    sched.shut();
    expect(did).toEqual(['close(false)', 'close(false)']);
  });

  it('takes it away at once only when told to', () => {
    const { did, sched } = over('a');
    sched.leave();
    sched.shut(true);
    vi.advanceTimersByTime(5000);
    expect(did).toEqual(['close(true)']);
  });
});
