// Every timing the app moves by, and the arithmetic behind the moves that
// need a measurement: where the opening's mark is drawn and where it
// flies, where a remapped card flies to and lands, how long each card
// and chip waits its turn. Kept apart from the components so each number
// is written once and can be checked without a DOM.

/** The curves played from script, as grid.css names them. Script-driven
 *  animations cannot read a custom property, so they take the same
 *  values from here; the stylesheet's tokens and these must agree
 *  (motion.test.ts checks that they do). */
export const EASE = {
  /** Most movement: flights, FLIP, the mark gliding home. */
  glide: 'cubic-bezier(0.22, 0.9, 0.24, 1)',
  /** Things arriving and coming to rest: posters, tiles. */
  settle: 'cubic-bezier(0.16, 1, 0.3, 1)',
  /** Sheets and panels leaving. */
  exit: 'cubic-bezier(0.4, 0, 0.8, 0.4)',
  /** Cards spreading out from the searched film. */
  spread: 'cubic-bezier(0.2, 0.8, 0.2, 1)',
  /** The opening mark coming into focus. */
  focus: 'cubic-bezier(0.25, 0.7, 0.2, 1)',
  /** Rows closing up or opening out as the empty years hide or show: a
   *  gentle start and a long, soft settle. */
  row: 'cubic-bezier(0.32, 0.72, 0, 1)',
} as const;

/** A box in viewport pixels: what getBoundingClientRect gives, and all
 *  of it the maths here reads. */
export interface Box {
  left: number;
  top: number;
  width: number;
  height: number;
}

/** A point in viewport pixels. */
export interface Spot {
  x: number;
  y: number;
}

/** Whether the reader has asked for nothing to move, read at the moment
 *  it is asked. A stylesheet's reduced-motion rules do not reach an
 *  animation started from script, so every one of those asks this. */
export function stillNow(): boolean {
  return (
    typeof window !== 'undefined' &&
    typeof window.matchMedia === 'function' &&
    window.matchMedia('(prefers-reduced-motion: reduce)').matches
  );
}

/** Element.animate, for a reader who has not asked for stillness and a
 *  browser that has it. Null otherwise, and the caller carries on with
 *  the end state: nothing here is needed for the page to be right, only
 *  for it to arrive nicely. */
export function animate(
  el: Element | null | undefined,
  keyframes: Keyframe[],
  options: KeyframeAnimationOptions,
): Animation | null {
  if (!el || typeof el.animate !== 'function' || stillNow()) return null;
  try {
    return el.animate(keyframes, options);
  } catch {
    return null;
  }
}

// ---- the opening ("focus pull") ----
//
// Every time below is measured from the moment the frames are on screen,
// which is when the opening screen mounts.

/** A list already in hand by then. There is nothing to wait for, so
 *  there is nothing to show waiting: the header is simply complete. */
export const FAST_PATH_MS = 120;

/** The headline, the sub-line and the theme picker each fade in over
 *  this, one step apart, from the start. Linear, as the design's own
 *  fade is: an eased fade on type reads as a pop and then a shimmer. */
export const COPY_FADE_MS = 600;
export const COPY_STEP_MS = 120;

/** The veil: the page under the header dims while the mark comes into
 *  focus over it, and comes back up as the mark leaves. */
export const VEIL_OPACITY = 0.72;
export const VEIL_IN_MS = 420;
export const VEIL_OUT_MS = 600;
/** The veil starts to rise once a fast list has been ruled out, not at
 *  0 as the design has it. The design also skips the whole sequence for
 *  a list in hand within FAST_PATH_MS, and both cannot hold: a veil
 *  raised at 0 is already a third of the way down by then, and a fast
 *  load would dim the page and bring it back for nothing. The mark is
 *  still invisible until FOCUS_DELAY_MS, and the veil is fully down well
 *  before the focus settles. */
export const VEIL_AT_MS = FAST_PATH_MS;

/** The mark's size while it is drawn over the tiles (the header's is
 *  Wordmark.tsx's MARK_W × MARK_H). */
export const LOADER_W = 46;
export const LOADER_H = 70;
/** The lowest the mark's centre sits: clear of the bottom of a short
 *  window, whatever the tiles below it are doing. */
export const LOADER_FLOOR = 48;

/** The mark racks into focus like a projector finding the screen: past
 *  sharp, a slight hunt back, then settled. One curve over the whole
 *  thing, not one per step, which is why it is played from script
 *  rather than as a stylesheet keyframe. */
export const FOCUS_DELAY_MS = 140;
export const FOCUS_MS = 920;
export const FOCUS_END_MS = FOCUS_DELAY_MS + FOCUS_MS;
export const FOCUS_KEYFRAMES: Keyframe[] = [
  { opacity: 0, filter: 'blur(14px)', transform: 'scale(1.45)' },
  { opacity: 1, filter: 'blur(0px)', transform: 'scale(0.985)', offset: 0.62 },
  { filter: 'blur(1.2px)', transform: 'scale(1.006)', offset: 0.8 },
  { opacity: 1, filter: 'blur(0px)', transform: 'scale(1)' },
];

/** While a slow list keeps it waiting, the focus breathes: in and out
 *  of true, over and over, until the list lands. It starts just after
 *  the focus has settled, if the list is still out then; the app cannot
 *  know in advance that it will be slow. */
export const BREATHE_AT_MS = 1120;
export const BREATHE_MS = 1600;
export const BREATHE_BLUR_PX = 1.8;

/** The glide into the header, and the softening the mark goes through
 *  on the way (sharpest at either end, a touch soft at 40%). */
export const GLIDE_MS = 520;
export const GLIDE_BLUR_PX = 1.4;
/** The posters start just after the mark lifts off, so the two are
 *  never drawn in the same pixels. */
export const TILES_AFTER_LIFT_MS = 80;
/** "inedikt" comes into focus just before its C lands. */
export const WORD_BEFORE_LANDING_MS = 200;
export const WORD_MS = 440;

/** Each poster comes into focus in reading order: its fill over
 *  TILE_FILL_MS, its caption over TILE_CAPTION_MS a little behind. The
 *  stylesheet draws these from the same numbers (.cd-cold-fill). */
export const TILE_FILL_MS = 640;
export const TILE_FILL_DELAY_MS = 20;
export const TILE_CAPTION_MS = 400;
export const TILE_CAPTION_DELAY_MS = 110;
export const TILE_STEP_MS = 70;

export function tileFillDelay(i: number): number {
  return TILE_FILL_DELAY_MS + i * TILE_STEP_MS;
}

export function tileCaptionDelay(i: number): number {
  return TILE_CAPTION_DELAY_MS + i * TILE_STEP_MS;
}

/** Coming back to the opening screen from a map does not replay the
 *  opening. The tiles rise into place instead, in reading order. */
export const RETURN_TILE_MS = 420;
export const RETURN_TILE_DELAY_MS = 80;
export const RETURN_TILE_STEP_MS = 45;
export const RETURN_RISE_PX = 10;

export function returnTileDelay(i: number): number {
  return RETURN_TILE_DELAY_MS + i * RETURN_TILE_STEP_MS;
}

/** The About page arriving: its intro, its credits and its footer each
 *  fade up into place on the settle curve, one step behind the one
 *  before. */
export const ABOUT_IN_MS = 520;
export const ABOUT_STEP_MS = 80;
export const ABOUT_RISE_PX = 8;

/** When each part of the opening happens, for a list that arrived at
 *  `listAt`. `fast` means none of it does. */
export interface OpeningPlan {
  fast: boolean;
  /** The focus is let run to the end even for a quick list: cutting a
   *  mark loose while it is still blurred is worse than the wait. */
  lift: number;
  /** Whether the list was still out when the focus settled. */
  breathes: boolean;
  tilesIn: number;
  word: number;
  /** The loader goes and the header's own mark appears. */
  done: number;
}

export function openingPlan(listAt: number): OpeningPlan {
  const lift = Math.max(listAt, FOCUS_END_MS);
  return {
    fast: listAt < FAST_PATH_MS,
    lift,
    breathes: listAt > BREATHE_AT_MS,
    tilesIn: lift + TILES_AFTER_LIFT_MS,
    word: lift + GLIDE_MS - WORD_BEFORE_LANDING_MS,
    done: lift + GLIDE_MS,
  };
}

/** Where the mark comes into focus: centred across the tile grid, and
 *  halfway down the part of it the reader can see, from the first frame
 *  to the grid's bottom or the window's, whichever comes first. Never
 *  so low that it sits on the bottom edge.
 *
 *  Null when the frames have no size — a page nobody is painting, such
 *  as one opened in a background tab — and the opening is skipped. */
export function loaderSpot(grid: Box, frame: Box, vh: number): Spot | null {
  if (!(frame.height > 0)) return null;
  const top = Math.max(grid.top, frame.top);
  const bottom = Math.min(grid.top + grid.height, vh);
  return {
    x: grid.left + grid.width / 2,
    y: Math.min((top + bottom) / 2, vh - LOADER_FLOOR),
  };
}

/** The trip from the tiles to the header's slot: centre to centre, and
 *  shrinking to the slot's width on the way. */
export function markFlight(slot: Box, from: Spot): { dx: number; dy: number; scale: number } {
  return {
    dx: slot.left + slot.width / 2 - from.x,
    dy: slot.top + slot.height / 2 - from.y,
    scale: slot.width / LOADER_W,
  };
}

// ---- opening a map ----

/** The map being left fades out over this while the next is fetched. A
 *  map already in hand switches at once: there is no wait to cover. */
export const MAP_FADE_MS = 220;

/** Every card but the searched one arrives from a little below and a
 *  little small, waiting for its distance from the searched film (see
 *  revealDelay in grid.ts). */
export const SPREAD_MS = 460;
export const SPREAD_RISE_PX = 12;
export const SPREAD_SCALE = 0.96;

/** The axis bar, the year bands and their labels fade in with it. */
export const AXIS_FADE_MS = 280;

/** The map is laid out first and let go a moment later, so every card
 *  has a painted state to travel out of. */
export const REVEAL_FLIP_MS = 30;
/** How long the spread stays open after that: the longest wait plus
 *  the spread itself, with a frame to spare. A card that dims after it
 *  closes does so at once, with no ripple behind it. */
export const REVEAL_WINDOW_MS = 1000;

/** The window for a map that opens `after` ms late, as one landing a
 *  flown card does. */
export function revealWindow(after: number): number {
  return after + REVEAL_WINDOW_MS;
}

// ---- moving between maps ----
//
// The move is one flight. A copy of the tapped card takes off from it,
// comes towards the reader on its way to where the next map will hold
// it, and sinks back into that map as its searched film. It rides a
// spring rather than a curve: a spring can say where it is and how fast
// it is going at any moment, so when the next map is in and the exact
// place to land is known, the flight is steered onto it in mid-air with
// no jolt, and a map that is slow to arrive leaves the copy resting in
// the middle until it does. A curve would stop dead at each of those
// points and start again at full speed.

/** How the copy flies. Its natural frequency (radians a second) and its
 *  damping: a hair under 1, so it arrives sooner than a critically
 *  damped spring would, and overshoots by less than a pixel. From rest
 *  it gathers speed over its first few frames rather than leaving at
 *  full tilt, is most of the way to the middle in a third of a second,
 *  and comes to rest in about three quarters. */
export const GLIDE_SPRING: Spring = { omega: 11, zeta: 0.92 };

/** How far the copy grows towards the reader while it is in the air. It
 *  gets this far only when it has to wait for its map; a map already in
 *  hand turns it back towards the page a little before. */
export const FLY_SCALE = 1.12;

/** The next map is set once the one being left has faded out, if it is
 *  in hand by then, and as soon as it lands otherwise. The copy flies on
 *  meanwhile. */
export const SET_AT_MS = MAP_FADE_MS + 20;

/** The copy takes on the searched card's fill and ring over this as it
 *  leaves, and the card it was copied from, marks and all, fades out
 *  from under it after. */
export const FACE_MS = 240;
export const PLAIN_OUT_MS = 120;

/** The Searched tag fades in on the copy over the end of its landing,
 *  and is there on the real card in the same place when it takes over. */
export const TAG_IN_MS = 220;

/** The rest of the new map spreads from the landing card this much
 *  later than a map opened any other way, so the map opens round the
 *  card as it settles. */
export const SPREAD_AFTER_LANDING_MS = 160;

/** The longest a landing may take before the move is called off
 *  anyway. A page nobody is painting never lays the map out, and a copy
 *  must not be left hanging over it for good. */
export const LAND_TIMEOUT_MS = 2000;

/** A spring: its natural frequency, in radians a second, and its damping
 *  ratio (1 arrives without overshooting; less is quicker and overshoots). */
export interface Spring {
  omega: number;
  zeta: number;
}

/** Where the copy is, as a move from its own untransformed box: a
 *  translation in pixels and a scale on each axis. */
export interface Pose {
  x: number;
  y: number;
  sx: number;
  sy: number;
}

/** The copy as it was put down: where it started, standing still. */
export const REST: Pose = { x: 0, y: 0, sx: 1, sy: 1 };
const STILL: Pose = { x: 0, y: 0, sx: 0, sy: 0 };

/** One leg of a flight: let go at `from` with velocity `vel` (per
 *  second), and heading for `to`. */
export interface Leg {
  from: Pose;
  vel: Pose;
  to: Pose;
  spring: Spring;
}

/** One axis of a damped spring, in closed form: `t` seconds after it was
 *  let go `d0` from its rest point at velocity `v0`, how far from that
 *  point it is and how fast it is going. Anything damped more than
 *  critically is treated as critical. */
export function springAxis(
  d0: number,
  v0: number,
  { omega: w, zeta }: Spring,
  t: number,
): { d: number; v: number } {
  const z = Math.min(zeta, 1);
  if (z >= 1) {
    const b = v0 + w * d0;
    const e = Math.exp(-w * t);
    return { d: (d0 + b * t) * e, v: (v0 - w * b * t) * e };
  }
  const wd = w * Math.sqrt(1 - z * z);
  const b = (v0 + z * w * d0) / wd;
  const e = Math.exp(-z * w * t);
  const c = Math.cos(wd * t);
  const s = Math.sin(wd * t);
  return {
    d: e * (d0 * c + b * s),
    v: e * ((b * wd - z * w * d0) * c - (d0 * wd + z * w * b) * s),
  };
}

const AXES = ['x', 'y', 'sx', 'sy'] as const;

/** Where a leg has got to `t` seconds in, and how fast it is going. */
export function legAt(leg: Leg, t: number): { pose: Pose; vel: Pose } {
  const pose = { ...leg.to };
  const vel = { ...STILL };
  for (const k of AXES) {
    const { d, v } = springAxis(leg.from[k] - leg.to[k], leg.vel[k], leg.spring, t);
    pose[k] = leg.to[k] + d;
    vel[k] = v;
  }
  return { pose, vel };
}

/** Close enough to land: under half a pixel from the spot and barely
 *  moving, and within a fifth of a percent of the size. Below this the
 *  last frame's step onto the exact spot cannot be seen. */
const ARRIVED_PX = 0.5;
const ARRIVED_PX_PER_S = 12;
const ARRIVED_SCALE = 0.002;
const ARRIVED_SCALE_PER_S = 0.03;

function arrived(at: { pose: Pose; vel: Pose }, to: Pose): boolean {
  return (
    Math.abs(at.pose.x - to.x) < ARRIVED_PX &&
    Math.abs(at.pose.y - to.y) < ARRIVED_PX &&
    Math.hypot(at.vel.x, at.vel.y) < ARRIVED_PX_PER_S &&
    Math.abs(at.pose.sx - to.sx) < ARRIVED_SCALE &&
    Math.abs(at.pose.sy - to.sy) < ARRIVED_SCALE &&
    Math.abs(at.vel.sx) < ARRIVED_SCALE_PER_S &&
    Math.abs(at.vel.sy) < ARRIVED_SCALE_PER_S
  );
}

/** A leg drawn out one frame at a time, for an animation to play back
 *  on the compositor, where a busy page cannot make it stutter. The last
 *  frame is exactly `to`. `duration` is in milliseconds. */
export const FRAME_MS = 1000 / 60;
const LONGEST_LEG_MS = 3000;

export function legFrames(leg: Leg): { frames: Pose[]; duration: number } {
  const frames: Pose[] = [leg.from];
  for (let ms = FRAME_MS; ms < LONGEST_LEG_MS; ms += FRAME_MS) {
    const at = legAt(leg, ms / 1000);
    if (arrived(at, leg.to)) break;
    frames.push(at.pose);
  }
  frames.push(leg.to);
  return { frames, duration: (frames.length - 1) * FRAME_MS };
}

/** A pose as a transform, about the copy's own centre. */
export function poseTransform(p: Pose): string {
  const move = `translate(${p.x.toFixed(2)}px, ${p.y.toFixed(2)}px)`;
  return `${move} scale(${p.sx.toFixed(4)}, ${p.sy.toFixed(4)})`;
}

/** Drives one element from pose to pose on GLIDE_SPRING, each leg
 *  taking over from wherever the last one had got to and at whatever
 *  speed it was going. Every leg is a Web Animation of the element's
 *  transform, so it runs off the main thread: the next map can be put
 *  together underneath it without the copy missing a frame. */
export class Glider {
  private leg: Leg | null = null;
  private run: Animation | null = null;
  private legMs = 0;

  constructor(private readonly el: HTMLElement) {}

  /** Where the element is now, and how fast it is going. */
  private now(): { pose: Pose; vel: Pose } {
    if (!this.leg || !this.run) return { pose: REST, vel: STILL };
    const ms = Number(this.run.currentTime ?? 0);
    return ms >= this.legMs ? { pose: this.leg.to, vel: STILL } : legAt(this.leg, ms / 1000);
  }

  /** Sends the element towards `to`, and calls `done` once it is there.
   *  Returns how long that will take, in milliseconds, or null when
   *  nothing moves at all (a reader who has asked for stillness, or a
   *  browser without Web Animations): the caller carries on with the end
   *  state. */
  aim(to: Pose, done?: () => void): number | null {
    const was = this.run;
    const at = this.now();
    const moving = was != null && was.playState === 'running' && was.startTime != null;
    const leg: Leg = { from: at.pose, vel: at.vel, to, spring: GLIDE_SPRING };
    const { frames, duration } = legFrames(leg);
    const run = animate(
      this.el,
      frames.map((p) => ({ transform: poseTransform(p) })),
      { duration, easing: 'linear', fill: 'forwards' },
    );
    if (!run) return null;
    // A leg that takes over from one in flight starts at the moment the
    // pose it starts from was read, so the two agree on the clock and
    // the handover lands between frames. From rest it is left to start
    // on the first frame it is drawn in, the default: work done before
    // that frame must not be spent out of the gentle start.
    if (moving) run.startTime = Number(was.startTime) + Number(was.currentTime ?? 0);
    was?.cancel();
    this.leg = leg;
    this.run = run;
    this.legMs = duration;
    if (done) run.addEventListener('finish', () => done(), { once: true });
    return duration;
  }

  /** Puts another animation on the current leg's clock, so that one
   *  timed to end with the leg does. A leg that took over in mid-air
   *  started when its pose was read, which can be a few frames before
   *  anything started after it gets its first frame. */
  follow(anim: Animation | null): void {
    const start = this.run?.startTime;
    if (anim && start != null) anim.startTime = start;
  }

  /** Drops the flight where it is. */
  cancel(): void {
    this.run?.cancel();
    this.run = null;
    this.leg = null;
  }
}

/** How far the flying copy moves: from the card to the middle of the
 *  map the reader can see, which on a phone starts under the header
 *  lying over it (`over` pixels of the scroller's top). */
export function flightTo(card: Box, scroller: Box, over: number): { tx: number; ty: number } {
  return {
    tx: scroller.left + scroller.width / 2 - (card.left + card.width / 2),
    ty: scroller.top + over + (scroller.height - over) / 2 - (card.top + card.height / 2),
  };
}

/** The landing: from where the copy started to exactly where the new
 *  searched card is, centre to centre, and stretched to its size. The
 *  new card can be a different size — the window may have crossed a
 *  screen class — so the two axes scale apart. */
export function landingTransform(
  from: Box,
  to: Box,
): { tx: number; ty: number; sx: number; sy: number } {
  return {
    tx: to.left + to.width / 2 - (from.left + from.width / 2),
    ty: to.top + to.height / 2 - (from.top + from.height / 2),
    sx: from.width > 0 ? to.width / from.width : 1,
    sy: from.height > 0 ? to.height / from.height : 1,
  };
}

/** The chip row after a move: a chip both maps share slides from where
 *  it was to where it is now; a new one rises in after them, one short
 *  step behind the one before it. */
export const CHIP_FLIP_MS = 420;
export const CHIP_IN_MS = 260;
export const CHIP_IN_DELAY_MS = 260;
export const CHIP_IN_STEP_MS = 35;
export const CHIP_IN_RISE_PX = 6;

export function chipInDelay(k: number): number {
  return CHIP_IN_DELAY_MS + k * CHIP_IN_STEP_MS;
}

/** How far back to put a chip so it starts where it used to be. */
export function chipShift(was: Box, now: Box): { dx: number; dy: number } {
  return { dx: was.left - now.left, dy: was.top - now.top };
}

// ---- a bigger photo of a person ----
//
// The card with a person's photo, name and role (PersonCard, timed by
// faceCard.ts). A pointer resting on a chip or a preview face opens it
// after the dwell, a little longer than the chip's own 140ms preview, so
// sweeping along the row only lights films. On touch a press held on a
// chip opens it. Once one is showing, or within the warm window of one
// closing, the next person's swaps in at once. It fades in over
// FACE_CARD_IN_MS, the opacity transition .cd-person-card draws.

export const FACE_CARD_DWELL_MS = 400;
export const FACE_CARD_HOLD_MS = 450;
export const FACE_CARD_WARM_MS = 250;
export const FACE_CARD_IN_MS = 160;

// ---- narrowing the map ----
//
// Hiding or showing the empty years closes the rows up, or opens them
// out, as one motion. The searched card stays still: the scroller moves
// by its shift. Every card, year label and year band that stays glides
// to its new place; a year that leaves closes into the seam it leaves
// between its neighbours, shrinking and fading as it goes; a year that
// comes back opens out of that seam. Undoing plays the same motion the
// other way. Every keyframe offset below is in eased progress, on
// EASE.row.

export const REFLOW_MS = 600;

/** How small a card or a year label is at the seam. A band closes into
 *  it from its top instead, to almost nothing. */
export const SEAM_SCALE = 0.94;
export const SEAM_BAND_SCALE = 0.02;

/** Something leaving has faded out by halfway. Something arriving stays
 *  out of sight until 0.3, and is at its own opacity by 0.85. */
export const LEAVE_FADED_AT = 0.5;
export const ARRIVE_FADE_FROM = 0.3;
export const ARRIVE_FADE_TO = 0.85;

/** Only what is this close to the part of the map on screen is drawn
 *  leaving. Anything further out simply goes: nobody would see it. */
export const LEAVE_REACH_PX = 240;

/** Where the scroller goes to keep the searched card still: on by that
 *  card's shift, `want`, from `from`, and no further than either end of
 *  the new plot allows (`most` is its last scroll position). Near an
 *  end of the map it cannot take the whole shift. */
export function reflowScroll(from: number, want: number, most: number): number {
  return Math.min(Math.max(0, from + want), Math.max(0, most));
}

/** How far back to put something that stays, so it starts where it was
 *  on screen: its move in the layout, less the part the scroller has
 *  already taken (`applied`, what the scroll actually moved by). For the
 *  searched card that is nothing, except where the scroll was clamped,
 *  and then it glides what the scroll could not take. A year's band and
 *  label have no `left`: they only ever move down or up. */
export function stayShift(
  was: { left?: number; top: number },
  now: { left?: number; top: number },
  applied: number,
): { dx: number; dy: number } {
  return { dx: (was.left ?? 0) - (now.left ?? 0), dy: was.top - now.top + applied };
}

/** Something that stays, from where it was on screen to its new place. */
export function stayKeyframes(dx: number, dy: number): Keyframe[] {
  return [{ transform: `translate(${dx}px, ${dy}px)` }, { transform: 'none' }];
}

/** A copy of something leaving, drawn where it was in the old layout:
 *  from there, which the scroll has moved by `from`, into the seam, `to`
 *  away from where it is drawn, fading out from `opacity` by halfway. A
 *  band closes from its top. */
export function leaveKeyframes(from: number, to: number, opacity: number, band: boolean): Keyframe[] {
  return [
    { transform: `translateY(${from}px)`, opacity },
    { opacity: 0, offset: LEAVE_FADED_AT },
    { transform: `translateY(${to}px) ${seamScale(band)}`, opacity: 0 },
  ];
}

/** Something arriving, out of the seam, `from` away from its new place,
 *  and up to `opacity`, its own: a card the filters dim arrives dim. */
export function arriveKeyframes(from: number, opacity: number, band: boolean): Keyframe[] {
  return [
    { transform: `translateY(${from}px) ${seamScale(band)}`, opacity: 0 },
    { opacity: 0, offset: ARRIVE_FADE_FROM },
    { opacity, offset: ARRIVE_FADE_TO },
    { transform: 'none', opacity },
  ];
}

function seamScale(band: boolean): string {
  return band ? `scaleY(${SEAM_BAND_SCALE})` : `scale(${SEAM_SCALE})`;
}
