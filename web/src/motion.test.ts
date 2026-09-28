import { describe, expect, it } from 'vitest';
import css from './grid.css?raw';
import {
  AXIS_FADE_MS,
  BREATHE_AT_MS,
  CHIP_FLIP_MS,
  CHIP_IN_MS,
  COPY_FADE_MS,
  COPY_STEP_MS,
  EASE,
  FAST_PATH_MS,
  FACE_MS,
  FLY_SCALE,
  FRAME_MS,
  FOCUS_DELAY_MS,
  FOCUS_END_MS,
  FOCUS_KEYFRAMES,
  FOCUS_MS,
  GLIDE_MS,
  GLIDE_SPRING,
  LAND_TIMEOUT_MS,
  LOADER_H,
  LOADER_W,
  MAP_FADE_MS,
  REFLOW_MS,
  ARRIVE_FADE_FROM,
  ARRIVE_FADE_TO,
  LEAVE_FADED_AT,
  LEAVE_REACH_PX,
  SEAM_BAND_SCALE,
  SEAM_SCALE,
  REST,
  REVEAL_WINDOW_MS,
  SET_AT_MS,
  SPREAD_AFTER_LANDING_MS,
  SPREAD_MS,
  VEIL_AT_MS,
  VEIL_IN_MS,
  VEIL_OPACITY,
  VEIL_OUT_MS,
  arriveKeyframes,
  chipInDelay,
  chipShift,
  flightTo,
  landingTransform,
  leaveKeyframes,
  legAt,
  legFrames,
  loaderSpot,
  markFlight,
  openingPlan,
  poseTransform,
  reflowScroll,
  returnTileDelay,
  revealWindow,
  springAxis,
  stayKeyframes,
  stayShift,
  tileCaptionDelay,
  tileFillDelay,
} from './motion';
import { REVEAL_MAX_MS } from './grid';

const box = (left: number, top: number, width: number, height: number) => ({
  left,
  top,
  width,
  height,
});

describe('the curves', () => {
  it('are the stylesheet’s own, for the animations played from script', () => {
    // The stylesheet names them; element.animate cannot read var(), so
    // the same curves are written again here and must not drift.
    const tokens: Record<string, string> = {
      glide: '--ease-glide',
      settle: '--ease-settle',
      exit: '--ease-exit',
      spread: '--ease-spread',
      focus: '--ease-focus',
      row: '--ease-row',
    };
    for (const [name, prop] of Object.entries(tokens)) {
      const m = css.match(new RegExp(`${prop}:\\s*([^;]+);`));
      expect(m?.[1].trim(), prop).toBe(EASE[name as keyof typeof EASE]);
    }
  });
});

describe('the opening timeline', () => {
  it('matches the design’s numbers', () => {
    expect(FAST_PATH_MS).toBe(120);
    expect([COPY_FADE_MS, COPY_STEP_MS]).toEqual([600, 120]);
    expect([VEIL_OPACITY, VEIL_IN_MS, VEIL_OUT_MS]).toEqual([0.72, 420, 600]);
    expect([LOADER_W, LOADER_H]).toEqual([46, 70]);
    // In focus from 140 to 1060.
    expect([FOCUS_DELAY_MS, FOCUS_MS, FOCUS_END_MS]).toEqual([140, 920, 1060]);
    expect(GLIDE_MS).toBe(520);
  });

  it('racks the mark past sharp and back, ending exactly in focus', () => {
    expect(FOCUS_KEYFRAMES[0]).toMatchObject({ opacity: 0, filter: 'blur(14px)', transform: 'scale(1.45)' });
    expect(FOCUS_KEYFRAMES[1]).toMatchObject({ opacity: 1, offset: 0.62, transform: 'scale(0.985)' });
    expect(FOCUS_KEYFRAMES[2]).toMatchObject({ offset: 0.8, filter: 'blur(1.2px)', transform: 'scale(1.006)' });
    expect(FOCUS_KEYFRAMES[3]).toMatchObject({ opacity: 1, filter: 'blur(0px)', transform: 'scale(1)' });
  });

  it('skips everything for a list already in hand', () => {
    expect(openingPlan(0).fast).toBe(true);
    expect(openingPlan(119).fast).toBe(true);
    expect(openingPlan(120).fast).toBe(false);
  });

  it('holds the veil until a fast list has been ruled out', () => {
    // A list in hand within FAST_PATH_MS skips the sequence, veil and
    // all, so the veil cannot start rising any earlier than that.
    expect(VEIL_AT_MS).toBe(FAST_PATH_MS);
    // And it is still fully down before the mark has settled in focus.
    expect(VEIL_AT_MS + VEIL_IN_MS).toBeLessThan(FOCUS_END_MS);
  });

  it('lets the focus finish before the mark lifts off, however quick the list', () => {
    const plan = openingPlan(300);
    expect(plan.lift).toBe(1060);
    expect(plan.breathes).toBe(false);
  });

  it('lifts off the moment a slow list lands, and breathes until then', () => {
    const plan = openingPlan(2400);
    expect(plan.lift).toBe(2400);
    expect(plan.breathes).toBe(true);
    // The breathing starts just after the focus settles.
    expect(BREATHE_AT_MS).toBe(1120);
    expect(openingPlan(BREATHE_AT_MS).breathes).toBe(false);
  });

  it('times the posters, the word and the landing from the lift', () => {
    const { lift, tilesIn, word, done } = openingPlan(500);
    expect(tilesIn - lift).toBe(80);
    expect(word - lift).toBe(320);
    // The loader goes, and the header's mark appears, as the glide ends.
    expect(done - lift).toBe(GLIDE_MS);
  });

  it('brings each poster into focus in reading order', () => {
    // This replaces firstRun's TILE_STEP_MS of 40, whose test held all
    // eight within 320 ms: the fills were then a quick fade of colour.
    // They are a focus pull now, as the design times it, 70 ms apart,
    // so the eighth starts 510 ms in and its caption 600 ms in.
    expect(tileFillDelay(0)).toBe(20);
    expect(tileFillDelay(7)).toBe(20 + 7 * 70);
    expect(tileCaptionDelay(0)).toBe(110);
    expect(tileCaptionDelay(7)).toBe(110 + 7 * 70);
  });

  it('draws the poster stagger in the stylesheet from the same numbers', () => {
    // .cd-cold-fill and .cd-cold-meta write the delays as calc()s; this
    // keeps them saying what tileFillDelay and tileCaptionDelay say.
    expect(css).toContain('640ms var(--ease-settle) calc(20ms + var(--i, 0) * 70ms)');
    expect(css).toContain('400ms ease calc(110ms + var(--i, 0) * 70ms)');
  });

  it('raises the tiles in order on the way back from a map instead', () => {
    expect(returnTileDelay(0)).toBe(80);
    expect(returnTileDelay(3)).toBe(80 + 3 * 45);
  });
});

describe('loaderSpot', () => {
  it('centres the mark across the grid and down the part of it on screen', () => {
    // Grid from 300 to 1100 in a 900 tall window: visible from the first
    // frame (300) to the bottom of the window (900).
    expect(loaderSpot(box(400, 300, 640, 800), box(400, 300, 146, 219), 900)).toEqual({
      x: 720,
      y: 600,
    });
  });

  it('stops at the grid’s bottom when the whole grid is on screen', () => {
    expect(loaderSpot(box(20, 250, 350, 290), box(20, 250, 168, 252), 844)).toEqual({
      x: 195,
      y: 395,
    });
  });

  it('never sits closer than 48px to the bottom of the window', () => {
    // A grid that starts almost at the bottom of a short window.
    const at = loaderSpot(box(0, 360, 800, 400), box(0, 360, 90, 135), 390);
    expect(at?.y).toBe(390 - 48);
  });

  it('gives up on frames with no size, so the opening is skipped', () => {
    expect(loaderSpot(box(0, 0, 0, 0), box(0, 0, 0, 0), 900)).toBeNull();
  });
});

describe('markFlight', () => {
  it('flies centre to centre and shrinks to the slot’s width', () => {
    const f = markFlight(box(26, 21, 14.7, 22.5), { x: 720, y: 600 });
    expect(f.dx).toBeCloseTo(26 + 7.35 - 720);
    expect(f.dy).toBeCloseTo(21 + 11.25 - 600);
    expect(f.scale).toBeCloseTo(14.7 / 46);
  });
});

describe('the move to another map', () => {
  it('sets the next map once the old one has faded, and flies on meanwhile', () => {
    expect(MAP_FADE_MS).toBe(220);
    expect(SET_AT_MS).toBeGreaterThan(MAP_FADE_MS);
    expect(SET_AT_MS).toBeLessThan(MAP_FADE_MS + 50);
    expect(FLY_SCALE).toBe(1.12);
    expect(FACE_MS).toBe(240);
    expect(SPREAD_AFTER_LANDING_MS).toBe(160);
    expect(GLIDE_SPRING).toEqual({ omega: 11, zeta: 0.92 });
  });

  it('flies the copy to the middle of the map the reader can see', () => {
    // A desktop map: the scroller is under a 114px header, nothing over it.
    const t = flightTo(box(1117, 576, 168, 90), box(0, 114, 1440, 786), 0);
    expect(1117 + 84 + t.tx).toBe(720);
    expect(576 + 45 + t.ty).toBe(114 + 393);
  });

  it('keeps clear of a header lying over the map on a phone', () => {
    // The phone's scroller starts at the top of the window, 116px of it
    // under the header: the middle is the middle of what is left.
    const t = flightTo(box(200, 500, 132, 72), box(0, 0, 390, 844), 116);
    expect(500 + 36 + t.ty).toBe(116 + (844 - 116) / 2);
    expect(200 + 66 + t.tx).toBe(195);
  });

  it('lands on the new card’s exact box, whatever its size', () => {
    const from = box(100, 100, 168, 90);
    const t = landingTransform(from, box(600, 400, 132, 72));
    expect(100 + 84 + t.tx).toBe(600 + 66);
    expect(100 + 45 + t.ty).toBe(400 + 36);
    expect(t.sx).toBeCloseTo(132 / 168);
    expect(t.sy).toBeCloseTo(72 / 90);
  });

  it('lands in place when the card is where the copy started', () => {
    expect(landingTransform(box(5, 5, 10, 10), box(5, 5, 10, 10))).toEqual({
      tx: 0,
      ty: 0,
      sx: 1,
      sy: 1,
    });
  });

  it('keeps the spread open long enough for the landing’s later start', () => {
    // A card's longest wait is 520 ms and its arrival 460 ms. A map a copy
    // lands on starts spreading 160 ms later, so its window is that much
    // longer, or the last cards would lose their timing mid-arrival.
    expect(SPREAD_MS).toBe(460);
    expect(revealWindow(0)).toBe(REVEAL_WINDOW_MS);
    expect(revealWindow(0)).toBeGreaterThanOrEqual(REVEAL_MAX_MS + SPREAD_MS);
    expect(revealWindow(SPREAD_AFTER_LANDING_MS)).toBeGreaterThanOrEqual(
      SPREAD_AFTER_LANDING_MS + REVEAL_MAX_MS + SPREAD_MS,
    );
    expect(AXIS_FADE_MS).toBe(280);
  });

  it('slides a shared chip from where it was, and staggers new ones behind', () => {
    expect(chipShift(box(300, 70, 120, 34), box(180, 70, 120, 34))).toEqual({ dx: 120, dy: 0 });
    expect([CHIP_FLIP_MS, CHIP_IN_MS]).toEqual([420, 260]);
    expect(chipInDelay(0)).toBe(260);
    expect(chipInDelay(4)).toBe(260 + 4 * 35);
  });
});

describe('the flight’s spring', () => {
  const s = GLIDE_SPRING;

  it('starts where it was let go, at the speed it was going', () => {
    expect(springAxis(-200, 0, s, 0)).toEqual({ d: -200, v: 0 });
    const at = springAxis(-200, 350, s, 0);
    expect(at.d).toBeCloseTo(-200);
    expect(at.v).toBeCloseTo(350);
  });

  it('gathers speed from rest rather than leaving at full tilt', () => {
    // The old flight covered 47px of a 255px trip in its first frame.
    const frame = (n: number) => 255 + springAxis(-255, 0, s, (n * FRAME_MS) / 1000).d;
    const steps = [1, 2, 3, 4].map((n) => frame(n) - frame(n - 1));
    expect(steps[0]).toBeLessThan(6);
    expect(steps[1]).toBeGreaterThan(steps[0]);
    expect(steps[2]).toBeGreaterThan(steps[1]);
  });

  it('is most of the way in a third of a second, and all but there in three quarters', () => {
    expect(Math.abs(springAxis(-1, 0, s, 0.34).d)).toBeLessThan(0.1);
    expect(Math.abs(springAxis(-255, 0, s, 0.75).d)).toBeLessThan(0.5);
  });

  it('overshoots by less than a pixel on a long trip', () => {
    let most = 0;
    for (let t = 0; t < 2; t += 0.004) most = Math.max(most, springAxis(-900, 0, s, t).d);
    expect(most).toBeLessThan(1);
  });

  it('reports the speed it actually moves at', () => {
    const t = 0.12;
    const dt = 1e-5;
    const ahead = springAxis(-255, 120, s, t + dt).d;
    const behind = springAxis(-255, 120, s, t - dt).d;
    const slope = (ahead - behind) / (2 * dt);
    expect(springAxis(-255, 120, s, t).v).toBeCloseTo(slope, 2);
  });

  it('treats more than critical damping as critical', () => {
    expect(springAxis(-10, 0, { omega: 11, zeta: 3 }, 0.2)).toEqual(
      springAxis(-10, 0, { omega: 11, zeta: 1 }, 0.2),
    );
  });
});

describe('a leg of the flight', () => {
  const to = { x: 120, y: -80, sx: 1.12, sy: 1.12 };
  const leg = { from: REST, vel: { x: 0, y: 0, sx: 0, sy: 0 }, to, spring: GLIDE_SPRING };

  it('is drawn a frame at a time and ends exactly on its spot', () => {
    const { frames, duration } = legFrames(leg);
    expect(frames[0]).toEqual(REST);
    expect(frames[frames.length - 1]).toEqual(to);
    expect(duration).toBeCloseTo((frames.length - 1) * FRAME_MS);
    // Comes to rest in under a second.
    expect(duration).toBeLessThan(1000);
    expect(duration).toBeGreaterThan(400);
  });

  it('never steps further than the frame before would suggest', () => {
    // No frame jumps: each step is within a little of the one before it.
    const { frames } = legFrames(leg);
    const steps = frames.slice(1).map((f, i) => Math.hypot(f.x - frames[i].x, f.y - frames[i].y));
    for (let i = 1; i < steps.length; i++) {
      expect(Math.abs(steps[i] - steps[i - 1])).toBeLessThan(6);
    }
  });

  it('hands over in mid-air at the same place and speed', () => {
    // The landing takes over from the flight 280 ms in, towards a spot a
    // little off the middle. Its first moment is the flight's last.
    const was = legAt(leg, 0.28);
    const landing = {
      from: was.pose,
      vel: was.vel,
      to: { x: 126, y: -60, sx: 1, sy: 1 },
      spring: GLIDE_SPRING,
    };
    const now = legAt(landing, 0);
    for (const k of ['x', 'y', 'sx', 'sy'] as const) {
      expect(now.pose[k]).toBeCloseTo(was.pose[k], 6);
      expect(now.vel[k]).toBeCloseTo(was.vel[k], 6);
    }
    // And the next frame is where the flight's would have been, give or
    // take the few pixels the new spot pulls it by.
    const next = legAt(landing, FRAME_MS / 1000).pose;
    const would = legAt(leg, 0.28 + FRAME_MS / 1000).pose;
    expect(Math.hypot(next.x - would.x, next.y - would.y)).toBeLessThan(1);
  });

  it('lands from rest in place when there is nowhere to go', () => {
    const still = legFrames({ ...leg, to: REST });
    expect(still.frames).toEqual([REST, REST]);
  });

  it('writes a pose as a transform about the copy’s own centre', () => {
    expect(poseTransform({ x: 1.234, y: -5, sx: 1.12, sy: 1 })).toBe(
      'translate(1.23px, -5.00px) scale(1.1200, 1.0000)',
    );
  });

  it('gives a landing time to finish before calling it off', () => {
    // The longest leg the page will ever ask for is under a second; the
    // move is only called off for a map that never lands at all.
    const far = legFrames({ ...leg, to: { x: 900, y: 700, sx: 0.8, sy: 0.8 } });
    expect(far.duration).toBeLessThan(LAND_TIMEOUT_MS / 2);
  });
});

describe('the reflow', () => {
  it('closes the rows up over 600 ms, with a gentle start and a long, soft settle', () => {
    expect(REFLOW_MS).toBe(600);
    expect(EASE.row).toBe('cubic-bezier(0.32, 0.72, 0, 1)');
    expect(css).toMatch(/--ease-row:\s*cubic-bezier\(0\.32, 0\.72, 0, 1\);/);
    // All of it is played from script, so no transition in the
    // stylesheet times any part of it.
    expect(css).not.toContain('.cd-card-arrive');
    expect(css).not.toContain('.cd-card-ghost');
  });

  it('draws copies only for what is within 240 px of the screen', () => {
    expect(LEAVE_REACH_PX).toBe(240);
  });

  it('glides what stays from where it was on screen to its new place', () => {
    expect(stayKeyframes(-12, 340)).toEqual([
      { transform: 'translate(-12px, 340px)' },
      { transform: 'none' },
    ]);
  });

  it('closes what leaves into its seam, faded out by halfway', () => {
    expect([SEAM_SCALE, LEAVE_FADED_AT]).toEqual([0.94, 0.5]);
    expect(leaveKeyframes(-80, 200, 0.12, false)).toEqual([
      { transform: 'translateY(-80px)', opacity: 0.12 },
      { opacity: 0, offset: 0.5 },
      { transform: 'translateY(200px) scale(0.94)', opacity: 0 },
    ]);
  });

  it('closes a band from its top, to almost nothing', () => {
    expect(SEAM_BAND_SCALE).toBe(0.02);
    expect(leaveKeyframes(0, -60, 1, true).at(-1)).toEqual({
      transform: 'translateY(-60px) scaleY(0.02)',
      opacity: 0,
    });
    // Which only reads as closing into the seam if it shrinks towards
    // its top edge, where the seam is.
    expect(css).toMatch(/\.cd-band \{[^}]*transform-origin: top;/);
  });

  it('opens what arrives out of its seam, unseen until 0.3 and at its own opacity by 0.85', () => {
    expect([ARRIVE_FADE_FROM, ARRIVE_FADE_TO]).toEqual([0.3, 0.85]);
    // A dimmed card arrives dimmed.
    expect(arriveKeyframes(-150, 0.12, false)).toEqual([
      { transform: 'translateY(-150px) scale(0.94)', opacity: 0 },
      { opacity: 0, offset: 0.3 },
      { opacity: 0.12, offset: 0.85 },
      { transform: 'none', opacity: 0.12 },
    ]);
    expect(arriveKeyframes(40, 1, true)[0]).toEqual({
      transform: 'translateY(40px) scaleY(0.02)',
      opacity: 0,
    });
  });

  it('keeps the searched card still by moving the scroll by its shift', () => {
    // As at 924 × 540 on The Matrix, where hiding the empty years takes
    // 2450 px of rows from above the searched film.
    const was = { left: 480, top: 3478 };
    const now = { left: 480, top: 1028 };
    const to = reflowScroll(3208, now.top - was.top, 1400);
    expect(to).toBe(758);
    expect(stayShift(was, now, to - 3208)).toEqual({ dx: 0, dy: 0 });
  });

  it('glides the searched card by what is left when the top of the map clamps the scroll', () => {
    // The reader was 300 px down; the rows above close up by 2450 px, and
    // the scroll can only give 300 of it.
    const was = { left: 480, top: 2620 };
    const now = { left: 480, top: 170 };
    const to = reflowScroll(300, now.top - was.top, 900);
    expect(to).toBe(0);
    expect(stayShift(was, now, to - 300)).toEqual({ dx: 0, dy: 2150 });
  });

  it('glides it by what is left when the new plot ends before the scroll can follow', () => {
    // Hiding near the bottom of the map: the rows above the searched film
    // close up by 300 px, but those below it close up by more, so the new
    // plot's last scroll position is 500 px above where the reader was.
    const was = { left: 480, top: 2000 };
    const now = { left: 480, top: 1700 };
    const to = reflowScroll(1900, now.top - was.top, 1400);
    expect(to).toBe(1400);
    expect(stayShift(was, now, to - 1900)).toEqual({ dx: 0, dy: -200 });
    // A plot shorter than the screen has nowhere to scroll at all.
    expect(reflowScroll(40, -500, -120)).toBe(0);
  });

  it('moves a year’s band and label up or down only', () => {
    expect(stayShift({ top: 400 }, { top: 250 }, -100)).toEqual({ dx: 0, dy: 50 });
  });
});
