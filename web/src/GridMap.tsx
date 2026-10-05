import {
  memo,
  useCallback,
  useEffect,
  useLayoutEffect,
  useMemo,
  useRef,
  useState,
  type RefObject,
} from 'react';
import {
  genreMask,
  hasGenres,
  inWarmSpan,
  initialsFor,
  isLit,
  layoutGrid,
  markersFor,
  passesFloor,
  railLabelTop,
  ratingText,
  revealDelay,
  rowKey,
  seamArriving,
  seamLeaving,
  searchedTagAt,
  type GridFilm,
  warmSpan,
  type GridLayout,
  type GridPayload,
  type GridPerson,
  type GridSettings,
  type Placed,
  type Row,
  type SpineFilm,
} from './grid';
import type { FaceCardEvents } from './faceCard';
import { MapPreview, TrailerFocus } from './MapPreview';
import { PosterImage } from './PosterImage';
import { posterFallback, sheetPosterPx, sheetPosterURL } from './poster';
import {
  PREVIEW_BACK_MS,
  PREVIEW_DRIFT_EXTRA_MS,
  PREVIEW_OUT_MS,
  PREVIEW_OUT_PLAYING_MS,
  PREVIEW_SWAP_OUT_MS,
  placePreview,
  previewBounds,
  previewFloatClear,
  previewScheduler,
  type PreviewBounds,
  type PreviewHost,
  type PreviewPlace,
} from './preview';
import { isFor } from './trailer';
import { useScreen } from './screen';
import { personVars } from './personColour';
import { canHover, canRest, useOffScreen, useTapGuard } from './tap';
import type { Player } from './TrailerRow';
import { useResolvedTheme, type Theme } from './theme';
import { markAppScroll, type AppScroll } from './overHeader';
import { askWhereToWatch } from './whereToWatch';
import {
  AXIS_FADE_MS,
  EASE,
  LEAVE_REACH_PX,
  REFLOW_MS,
  REVEAL_FLIP_MS,
  SPREAD_AFTER_LANDING_MS,
  SPREAD_RISE_PX,
  SPREAD_SCALE,
  animate,
  arriveKeyframes,
  leaveKeyframes,
  reflowScroll,
  revealWindow,
  stayKeyframes,
  stayShift,
  stillNow,
  type Box,
} from './motion';

/** A flown copy of a card on its way to land on this map's searched
 *  film (see GridApp's glideTo). The map hides that card until the copy
 *  is on it, then shows it in the same frame the copy goes. */
interface Landing {
  /** Turns the copy, wherever it has got to, towards this box, and calls
   *  `done` once it is on it. How long that will take, or null when it
   *  cannot move and has in effect already landed. */
  land: (to: Box, done: () => void) => number | null;
  /** Called once the copy is on the card. */
  onLanded: () => void;
}

/** How long past a landing's own length to wait for its finish before
 *  taking it as landed anyway: a page nobody is painting never plays the
 *  animation to its end. */
const LANDED_GRACE_MS = 120;

interface Props {
  /** What each visible card says, by film id. A card with nothing here
   *  is drawn as its own empty box until its detail arrives. */
  detail: Map<string, GridFilm>;
  /** Cards in view whose detail we do not have yet. */
  onNeedDetail: (ids: string[]) => void;
  payload: GridPayload;
  settings: GridSettings;
  /** People the reader has selected, as places in the chip row, which is
   *  how the spine names them; empty means everyone. Both the dimming and
   *  the hiding of empty years are judged on the spine, because a card
   *  nobody has scrolled to has no detail to judge. */
  selectedIdx: Set<number>;
  /** A person being previewed by a pointer resting on their chip. */
  hovered: string | null;
  /** Called with the people on the card under the pointer, to light chips. */
  onCardHover: (people: string[]) => void;
  onOpen: (filmId: string) => void;
  /** The moment the cards start appearing, so the loading toast can go. */
  onRevealed?: () => void;
  /** A sheet or popover is up: the floating buttons get out of its way. */
  covered?: boolean;
  /** Bumped when a setting has rearranged the plot underneath. */
  recentreKey?: number;
  /** The scrolling element, held by the app so the header can watch it. */
  scroller: RefObject<HTMLDivElement | null>;
  /** How much of the top of the scroller the header is lying over. The
   *  plot starts that far down, and centring only ever uses what is
   *  below it. Zero when the header sits above the map instead. */
  overlayH?: number;
  /** The header lying over the map has gone up out of the way, so the
   *  pinned year labels go up with it. */
  headerAway?: boolean;
  /** The small card, for a phone or a landscape phone. A screen-class
   *  call rather than a width: a landscape phone is as wide as a small
   *  tablet and still has a phone's height to fit rows into. */
  compact?: boolean;
  /** Marked before every scroll the map makes by itself, so the header
   *  lying over it can tell those from the reader's own. */
  appScroll?: RefObject<AppScroll>;
  /** The card a copy has just taken off from, on its way to becoming the
   *  next map. It goes from this map at once: the copy is it now. */
  flown?: string | null;
  /** This map is being left: it fades out, and while the next one is
   *  fetched it stays out of sight and out of reach. */
  leaving?: boolean;
  /** Set while a flown card is on its way to this map's searched film. */
  landing?: Landing | null;
  /** The page's one trailer player, which the hover preview's trailer
   *  plays in. */
  player: Player;
  /** The search field has text in it, which Escape clears before it
   *  closes the hover preview. */
  searchTyped?: boolean;
  /** A person's photo, from the payload or asked for since, for the
   *  hover preview's faces; undefined while there is none to show. */
  photoOf: (p: GridPerson) => string | undefined;
  /** A pointer resting on one of the hover preview's faces, and leaving
   *  it, for that person's bigger photo (see useFaceCard). */
  onFace: FaceCardEvents['onFace'];
  offFace: FaceCardEvents['offFace'];
}

/** How opaque a card that does not match the selection is. */
const DIM_SELECTED = 0.12;
const DIM_PREVIEW = 0.22;

/** Roughly how long Recenter's smooth scroll takes, after which the
 *  searched card is ringed so the reader can see where they were put,
 *  and how long the ring stays. A landing plays the same ring. */
const RECENTRE_GLIDE_MS = 420;
export const RING_MS = 900;

/** How long after a Recenter the ring starts. With reduced motion asked
 *  for, the map jumps rather than glides, so there is nothing to wait
 *  for: the ring starts with the jump. */
export function ringDelay(reduced: boolean): number {
  return reduced ? 0 : RECENTRE_GLIDE_MS;
}

/** The transitions a card wears while the map spreads open, in the
 *  order the stylesheet lists them on .cd-card-entering, and the delay
 *  each takes: the card's own wait for the arrival, and none at all for
 *  the rest, so hover answers at once even mid-spread. */
export function spreadDelays(delay: number): string {
  return `${delay}ms, ${delay}ms, 0s, 0s, 0s`;
}

/** How far the reader can scroll before a new band of cards is mounted.
 *  Well inside the screen that is already warm, so the mount happens
 *  before those cards reach the glass. */
const WARM_STEP = 64;

/** The grid: one card per film, year down, rating across.
 *
 *  Selecting people dims the cards they are not on and puts those out
 *  of reach (filteredOut). Unless the empty years are hidden, that is
 *  all it does: the layout is computed from the payload, the width and
 *  the settings, so a card never moves because of who is selected. With
 *  them hidden, the years left with nothing lit close up. */
export function GridMap({
  payload,
  settings,
  selectedIdx,
  hovered,
  onCardHover,
  onOpen,
  detail,
  onNeedDetail,
  onRevealed,
  covered = false,
  recentreKey = 0,
  scroller,
  overlayH = 0,
  headerAway = false,
  compact,
  appScroll,
  flown = null,
  leaving = false,
  landing = null,
  player,
  searchTyped = false,
  photoOf,
  onFace,
  offFace,
}: Props) {
  const tap = useTapGuard();
  // The poster fallback is painted in JavaScript, not CSS, so it is the
  // one colour that has to be read rather than inherited.
  const theme = useResolvedTheme();
  // Known before the scroller is measured, so the first paint already has
  // a screen of cards rather than a blank plot. The observer corrects it.
  const [width, setWidth] = useState(() => window.innerWidth);
  // Height 0 means the scroller has not been measured yet. Until then the
  // grid opens on the searched film rather than on the top of the plot.
  const [view, setView] = useState({ scrollTop: 0, height: 0, anchorId: payload.anchor.id });
  if (view.anchorId !== payload.anchor.id) {
    setView({ scrollTop: 0, height: 0, anchorId: payload.anchor.id });
  }

  const anchorIdRef = useRef(payload.anchor.id);
  anchorIdRef.current = payload.anchor.id;
  // Read inside listeners that outlive a render, so they never centre
  // on a header height that has since changed.
  const lift = useRef(overlayH);
  lift.current = overlayH;

  const readView = useCallback(() => {
    const el = scroller.current;
    if (!el) return;
    const height = el.clientHeight || window.innerHeight;
    const scrollTop = warmTop(el.scrollTop, lift.current);
    const anchorId = anchorIdRef.current;
    setWidth(el.clientWidth || window.innerWidth);
    setView((prev) =>
      prev.scrollTop === scrollTop && prev.height === height && prev.anchorId === anchorId
        ? prev
        : { scrollTop, height, anchorId },
    );
  }, []);

  // The layout follows the scroller's width, not the window's: the panel
  // and the scrollbar both take from it.
  //
  // A first measurement of zero is real: a page loaded in a background
  // tab is never laid out, so the element and the document both measure
  // nothing and a ResizeObserver does not fire either. `window.innerWidth`
  // is known regardless, so the grid is drawn at roughly the right size
  // rather than left blank until the reader looks at it; the observer
  // corrects it the moment there is true layout to read.
  //
  // Scroll keeps the warm band with the reader. One screen above and one
  // below stay mounted, so a flick in either direction meets cards that
  // are already there. The rest of the plot is only its height.
  useLayoutEffect(() => {
    const el = scroller.current;
    if (!el) return;
    // Scrolling is a flood, so it is throttled to a frame. Resizing is
    // not, and must not be: a page that has never been laid out — one
    // opened in a background tab — measures 0 everywhere, and a frame
    // never comes while it is hidden. Deferring the recovery to one
    // would leave that reader looking at an empty plot for good.
    let frame = 0;
    const onScroll = () => {
      tap.onScroll();
      if (frame) return;
      frame = requestAnimationFrame(() => {
        frame = 0;
        readView();
      });
    };
    readView();
    // And if there was nothing to measure yet, look again off a timer,
    // which does run while hidden.
    const retry = window.setTimeout(readView, 0);
    const ro = new ResizeObserver(readView);
    ro.observe(el);
    el.addEventListener('scroll', onScroll, { passive: true });
    window.addEventListener('resize', readView);
    return () => {
      if (frame) cancelAnimationFrame(frame);
      window.clearTimeout(retry);
      ro.disconnect();
      el.removeEventListener('scroll', onScroll);
      window.removeEventListener('resize', readView);
    };
  }, [readView, tap, scroller]);

  // The genres picked, as the bits the spine carries. Zero without a
  // legend, which asks for nothing.
  const want = useMemo(() => genreMask(payload, settings.genres), [payload, settings.genres]);
  // Cards the reader already has keep their seat when a later page
  // arrives. A new map, or a width that changes the axis, starts again.
  // The spine is the whole grid, so this runs once per grid and width.
  // There is no earlier layout to carry forward: every card's place was
  // already final the first time. A change of card size — a window
  // resized across the landscape-phone line — is a new layout too, not
  // a reflow: nothing is where it was to glide from.
  const layout = useMemo(
    () =>
      width > 0
        ? layoutGrid(
            payload,
            width,
            settings,
            (f) => isLit(f, selectedIdx, settings.minRating, want),
            compact,
          )
        : null,
    [payload, width, settings, selectedIdx, compact, want],
  );
  // The chip under the pointer, in the spine's own terms. -1 for an id
  // this row does not hold, which no card carries.
  const hoveredIdx = useMemo(
    () => (hovered == null ? null : payload.people.findIndex((p) => p.id === hovered)),
    [hovered, payload.people],
  );
  // While a copy lands on it, the searched card is not drawn, and it
  // appears without a fade when the copy goes (see `snapped`).
  const anchorHidden = landing != null;
  const cardOpacity = (c: Placed) =>
    (c.film.isAnchor && anchorHidden) || isFlown(c, flown)
      ? 0
      : opacityOf(c, selectedIdx, hoveredIdx, settings.minRating, want);
  // What the rows are laid out against, so a reflow can tell a change
  // of filter from a change of map or of width.
  const filterSig = `${settings.hideEmptyYears}|${settings.minRating}|${[...selectedIdx].sort((a, b) => a - b).join(',')}|${want}`;
  const reflow = useReflow({
    layout,
    sig: filterSig,
    scroller,
    hiding: settings.hideEmptyYears,
    appScroll,
    mapId: payload.anchor.id,
    overlayH,
    opacity: cardOpacity,
  });
  const codes = useMemo(() => initialsFor(payload.people), [payload.people]);
  const byId = useMemo(
    () => new Map(payload.people.map((p) => [p.id, p])),
    [payload.people],
  );

  // A map a flown card lands on spreads a beat later than one opened
  // any other way: the landing goes first. The held cards are let go
  // REVEAL_FLIP_MS after the map mounts, while the landing starts in the
  // mount's own layout pass, so the hold-back gives that frame back to
  // put the spread exactly SPREAD_AFTER_LANDING_MS behind the landing.
  const reveal = useReveal(
    payload.anchor.id,
    onRevealed,
    landing ? SPREAD_AFTER_LANDING_MS - REVEAL_FLIP_MS : 0,
  );
  // Set the moment the glide lands, so the reader's eye is told where it
  // was taken rather than being left to find the film again.
  const [confirming, setConfirming] = useState(false);
  // The searched card goes from hidden to shown in one frame, with no
  // fade, while a flown copy lands on it and for the ring after: the
  // copy leaves in the same frame the card appears, and a card fading
  // up under a copy that has already gone would blink.
  const [snapped, setSnapped] = useState(false);
  const ring = useRef(0);
  useEffect(() => () => window.clearTimeout(ring.current), []);

  const playRing = useCallback((after: number) => {
    window.clearTimeout(ring.current);
    const on = () => {
      setConfirming(true);
      ring.current = window.setTimeout(() => {
        setConfirming(false);
        setSnapped(false);
      }, RING_MS);
    };
    if (after <= 0) on();
    else ring.current = window.setTimeout(on, after);
  }, []);

  const recentre = useCallback(
    (smooth: boolean) => {
      const el = scroller.current;
      const to = layout && el ? centredScroll(layout, el, overlayH) : null;
      if (!el || !to) return;
      const reduced = stillNow();
      markAppScroll(appScroll, smooth && !reduced);
      el.scrollTo({ ...to, behavior: smooth && !reduced ? 'smooth' : 'auto' });
      if (!smooth) return;
      playRing(ringDelay(reduced));
    },
    [layout, overlayH, scroller, appScroll, playRing],
  );

  // A setting that reorders the years or adds a column pulls the plot
  // out from under the reader. The searched film goes back to the middle
  // at once, without a glide: nothing is where it was to glide from.
  const laidOut = useRef(recentreKey);
  useEffect(() => {
    if (recentreKey === laidOut.current) return;
    laidOut.current = recentreKey;
    recentre(false);
    readView();
  }, [recentreKey, recentre, readView]);

  // Every new grid opens centred on the film that was searched for. A
  // resize is not a new grid, so it keeps the reader where they were.
  // Until that scroll has landed, the cards drawn are the ones around the
  // film — not the top of the plot, which is where the scroller still is.
  const [placedFor, setPlacedFor] = useState<string | null>(null);
  useLayoutEffect(() => {
    const el = scroller.current;
    if (!layout || !el || placedFor === payload.anchor.id) return;
    // The layout has to be the one for the width on screen. The first
    // guess is the window, and recentring on it would miss once the
    // scroller reports its own width.
    if (width !== (el.clientWidth || window.innerWidth)) return;
    recentre(false);
    readView();
    setPlacedFor(payload.anchor.id);
  }, [layout, width, payload.anchor.id, placedFor, recentre, readView, scroller]);

  // The axis, the year bands and their labels fade in as the cards
  // spread, once per new map. Played from script because the bands and
  // labels are keyed by year and reused from one map to the next, where
  // a stylesheet animation would not start again.
  const faded = useRef<string | null>(null);
  useLayoutEffect(() => {
    const el = scroller.current;
    if (!el || !layout || !reveal.entering || faded.current === payload.anchor.id) return;
    faded.current = payload.anchor.id;
    for (const n of el.querySelectorAll('.cd-axis, .cd-band, .cd-rail-year')) {
      animate(n, [{ opacity: 0 }, { opacity: 1 }], { duration: AXIS_FADE_MS, fill: 'backwards' });
    }
  }, [payload.anchor.id, layout, reveal.entering, scroller]);

  // The landing. Once the new map is centred on its searched film, the
  // flown copy is turned, from wherever its flight has got to and at
  // whatever speed, onto that card's exact box; then the card takes its
  // place in the same frame and rings. A timer stands behind the
  // animation's own finish, which a page nobody is painting never
  // reaches.
  const landingRef = useRef(landing);
  landingRef.current = landing;
  const landingOn = landing != null;
  const landedFor = useRef<string | null>(null);
  useLayoutEffect(() => {
    if (!landingOn) {
      landedFor.current = null;
      return;
    }
    const el = scroller.current;
    const anchorId = payload.anchor.id;
    if (!el || placedFor !== anchorId || landedFor.current === anchorId) return;
    landedFor.current = anchorId;
    let over = false;
    let timer = 0;
    const node = el.querySelector<HTMLElement>(`[data-card="${CSS.escape(anchorId)}"]`);
    const done = () => {
      if (over) return;
      over = true;
      window.clearTimeout(timer);
      setSnapped(true);
      playRing(0);
      landingRef.current?.onLanded();
      // The card the reader pressed "Map" on went out of reach with the
      // map it was on. A keyboard is put down on the card it became.
      if (!document.activeElement || document.activeElement === document.body) {
        node?.focus({ preventScroll: true });
      }
    };
    const plan = landingRef.current;
    const to = node?.getBoundingClientRect();
    const took = plan && to && to.width > 0 ? plan.land(to, done) : null;
    if (took == null) {
      done();
      return;
    }
    timer = window.setTimeout(done, took + LANDED_GRACE_MS);
    return () => {
      // Called off (the glide was cancelled) or run again (StrictMode's
      // second mount): a later finish must not ring a card nobody
      // landed on, and a second run starts the landing again from
      // wherever the copy is.
      window.clearTimeout(timer);
      if (!over) landedFor.current = null;
      over = true;
    };
  }, [landingOn, placedFor, payload.anchor.id, scroller, playRing]);

  // Rows added above the screen would shove the cards the reader is
  // looking at down the page. Pin a card that is on the glass — not
  // only the searched film, which they may have already scrolled past.
  // Kept with the map it was taken on. The next map, drawn in this same
  // scroller, opens centred on its own film; a card both maps happen to
  // hold must not pull it away from there.
  const pin = useRef<{ map: string; id: string; top: number } | null>(null);
  const pinHere = (el: HTMLDivElement, at: GridLayout) => {
    const seen = cardOnGlass(at, el.scrollTop - overlayH, el.clientHeight);
    return seen ? { map: anchorIdRef.current, ...seen } : null;
  };
  useLayoutEffect(() => {
    const el = scroller.current;
    if (!layout || !el) return;
    const prev = pin.current?.map === payload.anchor.id ? pin.current : null;
    // A reflow pins the searched film itself and has already moved the
    // scroller for it. Pinning a second card on top of that would move
    // the map twice for one change.
    if (reflow.handled.current) {
      reflow.handled.current = false;
      readView();
    } else if (prev) {
      const card = layout.cards.find((c) => c.film.id === prev.id);
      if (card) {
        const delta = card.top - prev.top;
        if (delta !== 0) {
          markAppScroll(appScroll, false);
          el.scrollTop += delta;
          readView();
        }
      }
    }
    pin.current = pinHere(el, layout);
    // pinHere reads only the scroller, the header's height and the map.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [layout, readView, overlayH, scroller, reflow, appScroll]);

  useLayoutEffect(() => {
    const el = scroller.current;
    if (!layout || !el) return;
    const remember = () => {
      pin.current = pinHere(el, layout);
    };
    el.addEventListener('scroll', remember, { passive: true });
    return () => el.removeEventListener('scroll', remember);
    // pinHere reads only the scroller, the header's height and the map.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [layout, overlayH, scroller]);

  const viewH = view.height > 0 ? view.height : window.innerHeight;
  // A reflow draws the cards around where its scroll is about to take
  // the reader, not around where they were: the ones that glide into
  // view have to be there to glide.
  const scrollTop =
    reflow.spanTop ??
    (placedFor === payload.anchor.id && view.height > 0
      ? view.scrollTop
      : layout
        ? openedAt(layout, viewH)
        : 0);
  const span = warmSpan(scrollTop, viewH);
  const screen = { top: scrollTop, bottom: scrollTop + viewH };
  const cards = layout
    ? layout.cards.filter(
        (c) => inWarmSpan(c.top, layout.metrics.cardH, span) || reflow.keep.has(c.film.id),
      )
    : [];

  // Every card's place is already known, so scrolling never moves one.
  // What it does ask for is what the cards in reach actually say.
  // A flick that ends on a card was a flick. Only a press that stayed
  // put, on a map that had already stopped, opens anything.
  const openIfMeant = useCallback(
    (id: string) => {
      if (tap.allows()) onOpen(id);
    },
    [tap, onOpen],
  );

  // A finger cannot rest on a card, so on a touch screen a "hover" is
  // the tap itself, and lighting the chips from it says nothing.
  const lightIfHovering = useCallback(
    (people: string[]) => {
      if (canHover()) onCardHover(people);
    },
    [onCardHover],
  );

  // The hover preview beside a card the pointer rests on. Gone at once
  // while a panel or View is up, and while the map is being left or
  // landed on.
  const previewBlocked = covered || leaving || landing != null;
  // The screen's class: it sizes the floating buttons the preview keeps
  // clear of, and the poster the panel draws.
  const screenKind = useScreen();
  const preview = usePreview({
    layout,
    scroller,
    overlayH,
    headerAway,
    floatClear: previewFloatClear(screenKind.touch),
    blocked: previewBlocked,
    selected: selectedIdx,
    minRating: settings.minRating,
    want,
    said: (id) => wordsFor({ id, isAnchor: id === payload.anchor.id }, detail, payload.anchor),
    light: lightIfHovering,
    player,
    searchTyped,
  });
  const peek = previewBlocked ? null : preview.peek;
  const gone = previewBlocked ? null : preview.gone;
  const peekFilm = peek
    ? wordsFor({ id: peek.id, isAnchor: peek.id === payload.anchor.id }, detail, payload.anchor)
    : undefined;
  // The film whose preview is drawn, which the layer behind its trailer
  // goes with.
  const previewing = layout && peek && peekFilm ? peek.id : null;

  // Recenter is only worth offering when the film it would go to is not
  // already in front of the reader.
  const anchorAt = useCallback(() => {
    const card = layout?.anchor;
    if (!card || !layout) return null;
    return {
      x: card.left + layout.metrics.cardW / 2,
      y: card.top + overlayH + layout.metrics.cardH / 2,
    };
  }, [layout, overlayH]);
  const anchorAway = useOffScreen(scroller, anchorAt, [anchorAt]);
  // Not over a map that is being left, nor while a copy is landing on
  // the very card it would go back to.
  const offer = anchorAway && !covered && !leaving && !landing;

  const wantedKey = cards.map((c) => c.film.id).join(',');
  useEffect(() => {
    if (cards.length === 0) return;
    const missing = cards.map((c) => c.film.id).filter((id) => !detail.has(id));
    if (missing.length > 0) onNeedDetail(missing);
    // wantedKey stands for the set of cards in reach.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [wantedKey, detail, onNeedDetail]);

  // The sheet draws a larger poster than the card. For the cards on the
  // glass, that file is fetched now, quietly, so opening one does not
  // wait on the network. It is the file for this screen's sheet, which
  // is a different width on a phone, a landscape phone and anything
  // larger, so it is the one the sheet will ask for.
  const sheetPx = sheetPosterPx(screenKind);
  const glassKey = layout
    ? cards
        .filter((c) => inWarmSpan(c.top, layout.metrics.cardH, screen))
        .map((c) => c.film.id)
        .join(',')
    : '';
  const warmed = useRef(new Set<string>());
  useEffect(() => {
    if (!glassKey) return;
    for (const id of glassKey.split(',')) {
      const src = sheetPosterURL(detail.get(id)?.poster, sheetPx);
      if (!src || warmed.current.has(src)) continue;
      warmed.current.add(src);
      const img = new Image();
      img.decoding = 'async';
      img.fetchPriority = 'low';
      img.src = src;
    }
  }, [glassKey, detail, sheetPx]);

  const snap = anchorHidden || snapped;
  // Hidden and out of reach as it fades, and for as long as the next map
  // is on its way: its cards answer for a film the reader has left.
  const away = leaving || undefined;

  return (
    <>
      <div
        className={`cd-scroller${leaving ? ' cd-scroller-leaving' : ''}`}
        ref={scroller}
        id="cd-grid"
        role="region"
        aria-label="Movies by year and rating"
        aria-hidden={away}
        inert={away}
        onPointerDown={tap.onPointerDown}
        onPointerMove={tap.onPointerMove}
      >
        {layout && (
          <div
            className="cd-plot-wrap"
            style={{
              width: layout.plotW,
              ['--rail-w' as string]: `${layout.metrics.railW}px`,
              // How far down the pinned year labels sit: below the header
              // while it lies over the map, and up with it when it hides.
              ['--pin-lift' as string]: `${headerAway ? 0 : overlayH}px`,
            }}
          >
            {overlayH > 0 && <div style={{ height: overlayH }} aria-hidden="true" />}
            {/* The rating scale, said in words. It is inside the plot, so
                it pans sideways with the gridlines it labels, and after
                the overlay spacer, so on a phone it starts under the
                over-header and appears as that header goes up. */}
            <div className="cd-axis" aria-hidden="true">
              {settings.showUnrated && (
                <span className="cd-axis-label cd-axis-unrated">Unrated</span>
              )}
              {/* What the columns are, said once where the scale starts.
                  It is there with the unrated column off too, when it
                  takes the place "Unrated" had. */}
              <span className="cd-axis-label cd-axis-title" style={{ left: layout.axisTitleLeft }}>
                IMDb rating →
              </span>
              {layout.lines.map((l) => (
                <span key={l.rating} className="cd-axis-label" style={{ left: l.labelLeft }}>
                  {l.label}
                </span>
              ))}
            </div>
            {/* Held at its old height while the rows close up (see
                useReflow), so the scroll is never clamped under them. */}
            <div className="cd-plot" style={{ height: layout.plotH, minHeight: reflow.plotH }}>
              {/* The copies of years leaving come first, so the bands that
                  stay are drawn over them. */}
              {reflow.rows.map(({ row: r }) => (
                <YearBand key={`leaving:${rowKey(r)}`} row={r} leaving />
              ))}
              {layout.rows.map((r) => (
                <YearBand key={rowKey(r)} row={r} />
              ))}
              {settings.showUnrated && (
                <div className="cd-unrated-edge" style={{ left: layout.unratedEdge }} />
              )}
              {layout.lines.map((l) => (
                <div key={l.rating} className="cd-gridline" style={{ left: l.x }} />
              ))}
              {/* Before the cards, so the ones that stay are drawn over the
                  copies of the ones leaving. */}
              {reflow.cards.map(({ card: c, opacity }) => (
                <Card
                  key={`ghost:${c.film.id}`}
                  card={c}
                  said={detail.get(c.film.id)}
                  layout={layout}
                  people={byId}
                  codes={codes}
                  opacity={opacity}
                  eager={false}
                  enter={null}
                  ringed={false}
                  theme={theme}
                  ghost
                  onOpen={openIfMeant}
                  onHover={lightIfHovering}
                />
              ))}
              {cards.map((c) => (
                <Card
                  // Per map as well as per film: a film on both maps is a
                  // new card on the new one, arriving with the rest of it,
                  // not the old card sliding across from where it was.
                  key={`${payload.anchor.id}:${c.film.id}`}
                  card={c}
                  said={wordsFor(c.film, detail, payload.anchor)}
                  layout={layout}
                  people={byId}
                  codes={codes}
                  opacity={cardOpacity(c)}
                  off={filteredOut(c.film, selectedIdx, settings.minRating, want)}
                  eager={inWarmSpan(c.top, layout.metrics.cardH, screen)}
                  enter={
                    reveal.entering
                      ? {
                          hidden: !reveal.shown && !c.film.isAnchor,
                          delay: revealDelay(c, layout.anchor, reveal.after),
                        }
                      : null
                  }
                  ringed={confirming && c.film.isAnchor}
                  flown={isFlown(c, flown)}
                  snap={snap && c.film.isAnchor}
                  theme={theme}
                  hover={preview.held === c.film.id}
                  onOpen={openIfMeant}
                  onHover={lightIfHovering}
                  onPreview={preview.onCard}
                />
              ))}
              {/* The one giving way first, so the new one is drawn over it. */}
              {previewsDrawn(payload.anchor.id, gone, peek).map(({ key, p, leaving }) => {
                const film =
                  p === peek
                    ? peekFilm
                    : wordsFor({ id: p.id, isAnchor: p.id === payload.anchor.id }, detail, payload.anchor);
                return (
                  film && (
                    <MapPreview
                      key={key}
                      film={film}
                      leaving={leaving}
                      people={payload.people}
                      anchorTitle={payload.anchor.title}
                      theme={theme}
                      codes={codes}
                      photoOf={photoOf}
                      place={p.place}
                      player={player}
                      bounds={preview.bounds}
                      plotH={layout.plotH}
                      onEnter={preview.onEnter}
                      onLeave={preview.onLeave}
                      onFace={onFace}
                      offFace={offFace}
                    />
                  )
                );
              })}
              <div className="cd-rail-layer" style={{ height: layout.plotH, width: layout.plotW }}>
                <div className="cd-rail" style={{ width: layout.metrics.railW, height: layout.plotH }}>
                  {reflow.rows.map(({ row: r, labelAt }) => (
                    <YearLabel key={`leaving:${rowKey(r)}`} row={r} leaving labelAt={labelAt} />
                  ))}
                  {layout.rows.map((r) => (
                    <YearLabel key={rowKey(r)} row={r} />
                  ))}
                </div>
              </div>
            </div>
          </div>
        )}
      </div>
      {/* Outside the scroller, which it covers along with the header and
          the floating buttons while the preview's trailer plays. */}
      <TrailerFocus play={player.play} showing={previewing} out={peek?.out} back={peek?.back} />
      <button
        type="button"
        className={`cd-float cd-recentre${offer ? ' cd-float-up' : ''}`}
        aria-label={`Recenter on ${payload.anchor.title}`}
        aria-hidden={offer ? undefined : true}
        inert={offer ? undefined : true}
        onClick={() => recentre(true)}
      >
        <span className="cd-float-pill">
          <svg width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" aria-hidden="true">
            <circle cx="12" cy="12" r="7" />
            <circle cx="12" cy="12" r="2" />
            <path d="M12 2v3M12 19v3M2 12h3M19 12h3" />
          </svg>
          {/* Its own element so a phone can drop it and keep the icon
              while the quick switch beside View needs the room. */}
          <span className="cd-recentre-label">Recenter</span>
        </span>
      </button>
    </>
  );
}

/** A year's band across the plot, or the break's. `leaving` draws the
 *  copy a reflow closes into its seam. */
function YearBand({ row: r, leaving = false }: { row: Row; leaving?: boolean }) {
  const key = rowKey(r);
  return (
    <div
      className={
        r.isBreak
          ? 'cd-band cd-band-break'
          : `cd-band${r.index % 2 === 1 ? ' cd-band-odd' : ''}${r.anchorYear ? ' cd-band-anchor' : ''}${r.decade ? ' cd-band-decade' : ''}`
      }
      style={{ top: r.top, height: r.height }}
      data-band={leaving ? undefined : key}
      data-band-leaving={leaving ? key : undefined}
      aria-hidden={r.isBreak || leaving ? 'true' : undefined}
    />
  );
}

/** A year's label on the rail, or the break's "· · ·". `leaving` draws
 *  the copy a reflow closes into its seam, its label held `labelAt` down
 *  it, where the label was drawn: pinned to the top of the rail, perhaps,
 *  which a copy away from its place can no longer be. */
function YearLabel({
  row: r,
  leaving = false,
  labelAt,
}: {
  row: Row;
  leaving?: boolean;
  labelAt?: number;
}) {
  const key = rowKey(r);
  const tag = {
    'data-rail': leaving ? undefined : key,
    'data-rail-leaving': leaving ? key : undefined,
  };
  if (r.isBreak) {
    return (
      <span
        className="cd-rail-year cd-rail-break"
        style={{ top: railLabelTop(r) }}
        aria-hidden="true"
        {...tag}
      >
        · · ·
      </span>
    );
  }
  // The row's own stretch of the rail. Its label is pinned inside it
  // (grid.css), so a year taller than the screen still says which year
  // it is until its last card has gone by.
  return (
    <div
      className={`cd-rail-slot${leaving ? ' cd-rail-slot-leaving' : ''}`}
      style={{ top: r.top, height: r.height }}
      aria-hidden={leaving ? 'true' : undefined}
      {...tag}
    >
      <span
        className={`cd-rail-year${r.decade ? ' cd-rail-decade' : ''}${r.anchorYear ? ' cd-rail-anchor' : ''}`}
        style={leaving && labelAt != null ? { top: labelAt } : undefined}
      >
        {r.year}
      </span>
    </div>
  );
}

/** Where the scroller goes to put the searched card in the middle of what
 *  the reader can see: below a header lying over the top of it, and no
 *  further than the plot goes, so a card near an edge of a map, or on a
 *  map smaller than the screen, sits where the edge leaves it. The plot
 *  is `overlayH` down the scroller; the axis over it takes no room
 *  (grid.css). Across, it runs its full width and on past it wherever a
 *  card nudged into a lane (fitLane) hangs over the edge, as the
 *  browser's own scroll width does, so a top-rated card is never left
 *  cut off at the right. */
export function centredScroll(
  layout: GridLayout,
  view: { clientWidth: number; clientHeight: number },
  overlayH: number,
): { left: number; top: number } | null {
  const card = layout.anchor;
  if (!card) return null;
  const { cardW, cardH } = layout.metrics;
  const wide = layout.cards.reduce((w, c) => Math.max(w, c.left + cardW), layout.plotW);
  const fit = (want: number, most: number) => Math.max(0, Math.min(want, most));
  return {
    left: fit(card.left + cardW / 2 - view.clientWidth / 2, wide - view.clientWidth),
    top: fit(
      card.top + overlayH + cardH / 2 - (view.clientHeight + overlayH) / 2,
      overlayH + layout.plotH - view.clientHeight,
    ),
  };
}

/** Where a map's searched card will be on screen once that map has opened
 *  in this scroller, centred as every new map opens, on a clean slate:
 *  no one selected and no filters, as a map opened forward starts. A copy
 *  flying to the map uses it to head straight there, before the map is
 *  even drawn, rather than for the middle and then back out to a card
 *  the edge of the plot has kept from it. Laid out exactly as GridMap
 *  will lay it out, from the same width and settings. */
export function openingBox(
  payload: GridPayload,
  settings: GridSettings,
  scroller: HTMLElement,
  overlayH: number,
  compact: boolean,
): Box | null {
  const width = scroller.clientWidth || window.innerWidth;
  if (!(width > 0)) return null;
  const none = new Set<number>();
  const want = genreMask(payload, settings.genres);
  const lit = (f: SpineFilm) => isLit(f, none, settings.minRating, want);
  const layout = layoutGrid(payload, width, settings, lit, compact);
  const card = layout.anchor;
  const at = centredScroll(layout, scroller, overlayH);
  if (!card || !at) return null;
  const r = scroller.getBoundingClientRect();
  return {
    left: r.left + scroller.clientLeft + card.left - at.left,
    top: r.top + scroller.clientTop + overlayH + card.top - at.top,
    width: layout.metrics.cardW,
    height: layout.metrics.cardH,
  };
}

/** A card the reader can see, so a later layout can keep that spot still. */
function cardOnGlass(
  layout: GridLayout,
  scrollTop: number,
  viewH: number,
): { id: string; top: number } | null {
  const h = layout.metrics.cardH;
  const bottom = scrollTop + Math.max(viewH, 1);
  const seen = layout.cards.find((c) => c.top + h > scrollTop && c.top < bottom);
  if (seen) return { id: seen.film.id, top: seen.top };
  return layout.anchor ? { id: layout.anchor.film.id, top: layout.anchor.top } : null;
}

/** The opening ripple. A new map mounts with everything but the searched
 *  film hidden; a moment later they are all let go at once, each waiting
 *  for its own distance from that film before it arrives.
 *
 *  `after` holds the whole spread back, and is read once per map: the
 *  map a flown card lands on starts spreading 220 ms into the landing.
 *
 *  Timers, not frames: a page that is not being painted — one opened in a
 *  background tab — never gets a frame, and a map hung off one would
 *  still be invisible when the reader finally looked at it. */
function useReveal(
  anchorId: string,
  onRevealed: (() => void) | undefined,
  after: number,
): { entering: boolean; shown: boolean; after: number } {
  // A reader who has asked for nothing to move gets the map whole, with
  // no hidden state to come out of.
  const still = useRef(false);
  const fresh = () => {
    still.current = stillNow();
    return { id: anchorId, entering: !still.current, shown: still.current, after };
  };
  const [state, setState] = useState(fresh);
  if (state.id !== anchorId) setState(fresh());
  const landed = useRef(onRevealed);
  landed.current = onRevealed;
  useEffect(() => {
    if (still.current) {
      landed.current?.();
      return;
    }
    const flip = window.setTimeout(() => {
      setState((was) => (was.id === anchorId ? { ...was, shown: true } : was));
      landed.current?.();
    }, REVEAL_FLIP_MS);
    const settle = window.setTimeout(
      () => setState((was) => (was.id === anchorId ? { ...was, entering: false } : was)),
      REVEAL_FLIP_MS + revealWindow(state.after),
    );
    return () => {
      window.clearTimeout(flip);
      window.clearTimeout(settle);
    };
    // The hold-back belongs to the map, read when it arrived.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [anchorId]);
  return { entering: state.entering, shown: state.shown, after: state.after };
}

/** Where a new grid should open: the searched film in the middle of the
 *  screen, in the same steps the scroll listener uses, so the first paint
 *  and the paint after measuring ask for the same cards. */
function openedAt(layout: GridLayout, viewH: number): number {
  const card = layout.anchor;
  if (!card || viewH <= 0) return 0;
  const raw = Math.max(0, card.top + layout.metrics.cardH / 2 - viewH / 2);
  return Math.floor(raw / WARM_STEP) * WARM_STEP;
}

/** A reflow worked out from the layout the reader was looking at, `was`,
 *  and the one replacing it, `now`, with the scroller at `scrollTop`
 *  showing `viewH` of the map and the plot starting `overlayH` down it:
 *  where the scroll goes to keep the searched card still, what leaves
 *  within LEAVE_REACH_PX of the screen (anything further simply goes),
 *  and the cards that stay from within that reach, which are kept drawn
 *  through the motion wherever it carries them. */
export function reflowPlan(
  was: GridLayout,
  now: GridLayout,
  scrollTop: number,
  viewH: number,
  overlayH: number,
): { to: number; cards: Placed[]; rows: Row[]; keep: Set<string> } {
  const a = was.anchor;
  const b = now.anchor;
  const want = a && b && a.film.id === b.film.id ? b.top - a.top : 0;
  // What was on the glass, and LEAVE_REACH_PX either side of it, in the
  // old layout's terms.
  const reach = {
    top: scrollTop - overlayH - LEAVE_REACH_PX,
    bottom: scrollTop - overlayH + viewH + LEAVE_REACH_PX,
  };
  const staying = new Set(now.cards.map((c) => c.film.id));
  const rowsNow = new Set(now.rows.map(rowKey));
  const near = was.cards.filter((c) => inWarmSpan(c.top, was.metrics.cardH, reach));
  return {
    to: reflowScroll(scrollTop, want, overlayH + now.plotH - viewH),
    cards: near.filter((c) => !staying.has(c.film.id)),
    rows: was.rows.filter((r) => !rowsNow.has(rowKey(r)) && inWarmSpan(r.top, r.height, reach)),
    keep: new Set(near.filter((c) => staying.has(c.film.id)).map((c) => c.film.id)),
  };
}

/** The top of the screen in the plot, `lift` being how far down the
 *  scroller the plot starts, in the steps the warm band moves by. */
function warmTop(scrollTop: number, lift: number): number {
  return Math.floor(Math.max(0, scrollTop - lift) / WARM_STEP) * WARM_STEP;
}

/** A card leaving the map in a reflow, and the opacity it was drawn at. */
interface LeavingCard {
  card: Placed;
  opacity: number;
}

/** A year leaving the map in a reflow, and how far down its stretch of
 *  the rail its label was drawn. */
interface LeavingRow {
  row: Row;
  labelAt: number | undefined;
}

/** One reflow, from the render that changes the filters until its motion
 *  has ended. */
interface Flight {
  mapId: string;
  sig: string;
  /** The layout it starts from, the one the reader was looking at. */
  from: GridLayout;
  /** The scroller's position before the new layout committed, and the
   *  one that keeps the searched card still on the new layout. */
  scrollTop: number;
  to: number;
  /** The plot's height before the new layout committed. The plot keeps
   *  at least this height until the motion ends. */
  plotH: number;
  /** What leaves within reach of the screen, drawn where it was as it
   *  goes. */
  cards: LeavingCard[];
  rows: LeavingRow[];
  /** Where each year's label was on screen before the new layout
   *  committed, by rowKey. */
  labels: Map<string, number>;
  /** Cards that stay and were within reach of the screen. They are drawn
   *  through the motion wherever it takes them, so a card carried off the
   *  screen is seen going rather than simply gone. */
  keep: Set<string>;
  /** False for a reader who has asked for nothing to move. */
  moving: boolean;
  /** The scroller has moved and the motion has started. */
  played: boolean;
}

const NO_CARDS = new Set<string>();

/** Hiding or showing the empty years: the rows close up, or open out, as
 *  one motion (see motion.ts).
 *
 *  Rows leaving above the reader would carry the whole map up the
 *  screen, so the searched film is pinned — not a card that happens to
 *  be on the glass, which is what an ordinary relayout pins. This is
 *  the one movement the reader asked for, and it should look like the
 *  map closing up around the film it is of.
 *
 *  The scroll and the plot's height are taken in the render that
 *  changes the filters, before the new layout commits. A plot that
 *  shrank in the commit would have had the browser clamp the scroll by
 *  the time a layout effect could read it, and every distance measured
 *  after that would start from the wrong place. So the plot is drawn at
 *  no less than its old height until the motion ends, and the layout
 *  effect moves the scroll on from the position taken before.
 *
 *  The motion is played with Web Animations, started in the layout
 *  effect, before the browser paints: the first frame drawn already has
 *  everything where it was. */
function useReflow({
  layout,
  sig,
  scroller,
  hiding,
  appScroll,
  mapId,
  overlayH,
  opacity,
}: {
  layout: GridLayout | null;
  /** What the rows are laid out against. */
  sig: string;
  scroller: RefObject<HTMLDivElement | null>;
  hiding: boolean;
  appScroll: RefObject<AppScroll> | undefined;
  mapId: string;
  /** How far down the scroller the plot starts. */
  overlayH: number;
  /** The opacity each card is drawn at on this layout. */
  opacity: (c: Placed) => number;
}): {
  /** Copies of the cards leaving, and of the years' bands and labels. */
  cards: LeavingCard[];
  rows: LeavingRow[];
  /** Cards to draw whether or not they are in the warm band. */
  keep: Set<string>;
  /** Where to take the warm band from, for the render the reflow
   *  commits in: where the scroll is about to go. */
  spanTop: number | null;
  /** The plot's least height while the motion plays. */
  plotH: number | undefined;
  /** Set for the one layout this hook moved the scroller for, so the
   *  ordinary pin does not move it a second time. */
  handled: RefObject<boolean>;
} {
  const handled = useRef(false);
  // The layout on screen: the last one committed, what it was laid out
  // against, and the opacity each of its cards was drawn at.
  const last = useRef<{
    mapId: string;
    sig: string;
    hiding: boolean;
    layout: GridLayout;
    opacity: (c: Placed) => number;
  } | null>(null);
  const drawnAt = useRef(opacity);
  drawnAt.current = opacity;
  const [flight, setFlight] = useState<Flight | null>(null);
  // A flight belongs to its map. A map opened meanwhile holds nothing.
  const live = flight?.mapId === mapId ? flight : null;
  const release = useRef(0);
  useEffect(() => () => window.clearTimeout(release.current), []);

  // Only a change of filter reflows, and only one that hides the empty
  // years or stops hiding them. A new map, a resize or a year range each
  // put the reader somewhere else entirely, and gliding three hundred
  // cards across that would be motion about nothing. A floor, a
  // selection or the genres changed with nothing hidden only dims and
  // lights in place.
  const was = last.current;
  const el = scroller.current;
  if (
    layout &&
    el &&
    was &&
    was.mapId === mapId &&
    was.sig !== sig &&
    (hiding || was.hiding) &&
    live?.sig !== sig
  ) {
    const scrollTop = el.scrollTop;
    const moving = !stillNow();
    const plan = reflowPlan(was.layout, layout, scrollTop, el.clientHeight, overlayH);
    // A year's label is pinned to the top of the rail while its row runs
    // under it (grid.css), so where each is drawn is known only by
    // looking, and only now, before the new layout commits.
    const labels = new Map<string, number>();
    const labelAt = new Map<string, number>();
    if (moving) {
      for (const n of el.querySelectorAll<HTMLElement>('[data-rail]')) {
        const key = n.dataset.rail ?? '';
        const label = railLabel(n);
        const top = label.getBoundingClientRect().top;
        labels.set(key, top);
        if (label !== n) labelAt.set(key, top - n.getBoundingClientRect().top);
      }
    }
    setFlight({
      mapId,
      sig,
      from: was.layout,
      scrollTop,
      to: plan.to,
      // A reflow that follows another before its motion ends starts from
      // a plot still held at the height before that one.
      plotH: Math.max(was.layout.plotH, live?.plotH ?? 0),
      cards: moving ? plan.cards.map((card) => ({ card, opacity: was.opacity(card) })) : [],
      rows: moving
        ? plan.rows.map((row) => ({ row, labelAt: labelAt.get(rowKey(row)) }))
        : [],
      labels,
      keep: moving ? plan.keep : NO_CARDS,
      moving,
      played: false,
    });
  }

  useLayoutEffect(() => {
    const node = scroller.current;
    if (!live || live.played || !layout || !node) return;
    handled.current = true;
    if (node.scrollTop !== live.to) {
      markAppScroll(appScroll, false);
      node.scrollTop = live.to;
    }
    // What the scroll actually moved by, which is what everything else is
    // measured against: the browser may round it.
    const applied = node.scrollTop - live.scrollTop;
    window.clearTimeout(release.current);
    if (!live.moving) {
      setFlight(null);
      return;
    }
    setFlight((f) => (f === live ? { ...f, played: true } : f));
    playReflow(node, live, layout, applied, drawnAt.current);
    // The copies go, and the plot lets go of its old height, as the
    // motion ends. The scroll was kept inside the new plot, so it does
    // not move when the plot shortens.
    release.current = window.setTimeout(
      () => setFlight((f) => (f?.sig === live.sig ? null : f)),
      REFLOW_MS,
    );
  }, [live, layout, scroller, appScroll]);

  useLayoutEffect(() => {
    last.current = layout ? { mapId, sig, hiding, layout, opacity } : null;
  });

  return {
    cards: live?.cards ?? [],
    rows: live?.rows ?? [],
    keep: live?.keep ?? NO_CARDS,
    spanTop: live && !live.played ? warmTop(live.to, overlayH) : null,
    plotH: live?.plotH,
    handled,
  };
}

/** Starts a reflow's motion, on the layout React has just committed and
 *  after the scroll has moved by `applied`. Everything is placed where it
 *  was on screen and let go: what stays glides to its new place, what
 *  arrives opens out of its seam, and the copies of what leaves close
 *  into theirs. For a reader who has asked for nothing to move, animate
 *  does nothing and the new layout simply stands. */
function playReflow(
  el: HTMLElement,
  f: Flight,
  layout: GridLayout,
  applied: number,
  opacity: (c: Placed) => number,
): void {
  const wasRows = f.from.rows;
  const nowRows = layout.rows;
  const wasCard = new Map(f.from.cards.map((c) => [c.film.id, c]));
  const nowCard = new Map(layout.cards.map((c) => [c.film.id, c]));
  const wasRow = new Map(wasRows.map((r) => [rowKey(r), r]));
  const nowRow = new Map(nowRows.map((r) => [rowKey(r), r]));
  const run = (n: Element, frames: Keyframe[], fill?: FillMode) =>
    animate(n, frames, { duration: REFLOW_MS, easing: EASE.row, fill });
  // A year's label that stays goes from where it was drawn to where it is
  // drawn now. That is its row's move, unless the label was pinned to
  // the top of the rail before or is now, which only the two looks can
  // tell. Every label is looked at before anything starts to move.
  const labels = [...el.querySelectorAll<HTMLElement>('[data-rail]')].map((n) => {
    const key = n.dataset.rail ?? '';
    const label = railLabel(n);
    const was = f.labels.get(key);
    return { n, key, label, dy: was == null ? null : was - label.getBoundingClientRect().top };
  });

  // The cards, and the searched card's tag, which sits beside it rather
  // than in it and so is moved with it.
  for (const n of el.querySelectorAll<HTMLElement>('[data-card], [data-card-tag]')) {
    const now = nowCard.get(n.dataset.card ?? n.dataset.cardTag ?? '');
    if (!now) continue;
    const was = wasCard.get(now.film.id);
    if (was) {
      const { dx, dy } = stayShift(was, now, applied);
      if (dx !== 0 || dy !== 0) run(n, stayKeyframes(dx, dy));
    } else {
      // Up to the opacity this layout gives it rather than the one its
      // style shows now: a card the filters dim arrives dim.
      const seam = seamArriving(rowKey(now.film), wasRows, nowRows, applied);
      run(n, arriveKeyframes(seam - now.top, opacity(now), false));
    }
  }
  // The years' bands. What stays glides by its row's move.
  for (const n of el.querySelectorAll<HTMLElement>('[data-band]')) {
    const key = n.dataset.band ?? '';
    const now = nowRow.get(key);
    if (!now) continue;
    const was = wasRow.get(key);
    if (was) {
      const { dy } = stayShift(was, now, applied);
      if (dy !== 0) run(n, stayKeyframes(0, dy));
    } else {
      run(n, arriveKeyframes(seamArriving(key, wasRows, nowRows, applied) - now.top, 1, true));
    }
  }
  // Their labels on the rail (looked at above).
  for (const { n, key, label, dy } of labels) {
    const now = nowRow.get(key);
    if (!now) continue;
    const was = wasRow.get(key);
    if (was) {
      const by = dy ?? stayShift(was, now, applied).dy;
      if (by !== 0) run(label, stayKeyframes(0, by));
    } else {
      run(n, arriveKeyframes(seamArriving(key, wasRows, nowRows, applied) - now.top, 1, false));
    }
  }
  // The copies of what leaves, drawn where it was. They hold their last
  // frame until the flight lets them go.
  const leaving = new Map(f.cards.map((g) => [g.card.film.id, g]));
  for (const n of el.querySelectorAll<HTMLElement>('[data-card-leaving]')) {
    const g = leaving.get(n.dataset.cardLeaving ?? '');
    if (!g) continue;
    const seam = seamLeaving(rowKey(g.card.film), wasRows, nowRows);
    run(n, leaveKeyframes(applied, seam - g.card.top, g.opacity, false), 'forwards');
  }
  for (const n of el.querySelectorAll<HTMLElement>('[data-band-leaving], [data-rail-leaving]')) {
    const band = n.dataset.bandLeaving != null;
    const key = (band ? n.dataset.bandLeaving : n.dataset.railLeaving) ?? '';
    const was = wasRow.get(key);
    if (!was) continue;
    const seam = seamLeaving(key, wasRows, nowRows);
    run(n, leaveKeyframes(applied, seam - was.top, 1, band), 'forwards');
  }
}

/** The label in a year's stretch of the rail, or the break's "· · ·",
 *  which is its own element. */
function railLabel(n: HTMLElement): HTMLElement {
  return n.classList.contains('cd-rail-slot') && n.firstElementChild instanceof HTMLElement
    ? n.firstElementChild
    : n;
}

/** The hover preview on show: whose it is, where it sits, and the
 *  layout it was placed on. A new layout is a map whose cards may have
 *  moved, so a preview placed on an older one is not drawn (see
 *  usePreview for the one exception). */
export interface Peek {
  id: string;
  /** Which opening of a preview this is. It is part of its key, so a card
   *  opened again while its last preview is still fading out as `gone`
   *  mounts a new preview, whose entrance runs, rather than taking over
   *  the fading one: React would move that one in the page, and an
   *  element moved loses its transitions and snaps to full. */
  n: number;
  place: PreviewPlace;
  on: GridLayout;
  /** Set once it has begun to leave: `playing` when its trailer is set,
   *  which then goes with it, sound and blur and all. */
  out?: 'plain' | 'playing';
  /** Taken back while it was leaving: the sound and the blur behind come
   *  back quicker than they first came (PREVIEW_BACK_MS). */
  back?: boolean;
}

/** What the map hands usePreview. */
interface PreviewOptions {
  layout: GridLayout | null;
  scroller: RefObject<HTMLDivElement | null>;
  overlayH: number;
  headerAway: boolean;
  /** The strip along the map's bottom that View and Recenter float in,
   *  which the preview never reaches into (previewFloatClear). */
  floatClear: number;
  /** Something else has the map: a panel or View is up, or the map is
   *  being left or landed on. */
  blocked: boolean;
  selected: Set<number>;
  minRating: number | null;
  /** The genres picked, as spine bits (see genreMask). */
  want: number;
  /** What a card says, once its words have come: who is on it, for its
   *  chips, and all the preview has to show. */
  said: (id: string) => GridFilm | undefined;
  /** Lights these people's chips. */
  light: (people: string[]) => void;
  player: Player;
  /** The search field has text in it. */
  searchTyped: boolean;
}

/** What of the map the preview's host reads, at the moment it asks. */
export type PreviewMap = Pick<
  PreviewOptions,
  'layout' | 'blocked' | 'selected' | 'minRating' | 'want' | 'said' | 'light' | 'player'
>;

/** The previews to draw, each keyed by its film on this map and which
 *  opening of it it is. One list, so a preview moving from the live slot
 *  to `gone` keeps its key, its instance and its DOM, and fades from
 *  where it is. The gone one comes first, so the new one is drawn over
 *  it. */
export function previewsDrawn(anchorId: string, gone: Peek | null, peek: Peek | null) {
  return [gone, peek]
    .filter((p): p is Peek => p != null)
    .map((p) => ({ key: `${anchorId}:${p.id}:${p.n}`, p, leaving: p === gone ? ('gone' as const) : p.out }));
}

/** What the scheduler asks of the map, for usePreview: asking where a
 *  card's movie can be watched as the pointer begins to rest on it
 *  (`resting`), opening a card's preview, letting it leave or taking it
 *  away at once, and bringing it back; and, for the pointer leaving a
 *  card, holding that card's hover state for its preview (`leftCard`).
 *  It draws through `put` (the live preview, which `now` reads back
 *  between renders), `gone` (the one giving way on a swap) and `held`
 *  (the card drawn as hovered for its preview). `showing` is the card
 *  whose preview is showing, as of now, and `under` the card the pointer
 *  is on. */
export function previewHost(o: {
  map: () => PreviewMap;
  bounds: (loose: boolean) => PreviewBounds;
  now: () => Peek | null;
  put: (p: Peek | null) => void;
  gone: (p: Peek | null) => void;
  held: (id: string | null) => void;
  showing: { current: string | null };
  under: { current: string | null };
}): PreviewHost & { leftCard: (id: string) => void; dispose: () => void } {
  // The leaving preview's unmount, the gone one's, the card whose preview
  // opens once a preview leaving with its trailer has gone, and how many
  // previews have opened (Peek.n).
  let unmount = 0;
  let goneTimer = 0;
  let next: string | null = null;
  let opened = 0;

  const playsIn = (id: string) => isFor(o.map().player.now(), 'preview', id);
  // The player's sound, when it has any to move: open and not muted.
  const heard = (id: string) => {
    const p = o.map().player.now();
    return isFor(p, 'preview', id) && p.open && !p.muted;
  };
  const stopUnmount = () => {
    window.clearTimeout(unmount);
    unmount = 0;
  };
  const stopGone = () => {
    window.clearTimeout(goneTimer);
    goneTimer = 0;
    o.gone(null);
  };
  // The chips a card or its preview lit go with it, unless the pointer
  // is on a card, which lit its own.
  const letGo = () => {
    o.held(null);
    if (o.under.current == null) o.map().light([]);
  };
  // Only a card whose words have come has a preview to show. One that
  // is still an empty box opens nothing, and so neither counts as a
  // preview showing nor keeps its hover state for one.
  const opens = (id: string): Placed | null => {
    const { layout, blocked, selected, minRating, want, said } = o.map();
    const card = layout?.cards.find((c) => c.film.id === id);
    if (!layout || !card || blocked || filteredOut(card.film, selected, minRating, want) || !said(id)) return null;
    return card;
  };
  const placeFor = (id: string): Peek | null => {
    const card = opens(id);
    const { layout } = o.map();
    if (!card || !layout) return null;
    return { id, n: ++opened, place: placePreview(card, layout.metrics, o.bounds(false), layout.plotH), on: layout };
  };
  const show = (p: Peek) => {
    o.showing.current = p.id;
    o.put(p);
  };
  // Gone at once, with whatever was waiting on it.
  const takeAway = () => {
    stopUnmount();
    next = null;
    o.showing.current = null;
    o.put(null);
    letGo();
  };
  // Begins to leave: it fades where it stands and drifts back towards
  // its card, and unmounts once both have finished. It still counts as
  // showing until then, so resting on its card is being back on the card
  // whose preview it is. Its trailer, if set, plays on through the fade
  // while its sound and the blur behind go with it.
  const leave = () => {
    const p = o.now();
    if (!p || p.out) return;
    if (stillNow()) {
      takeAway();
      return;
    }
    const playing = playsIn(p.id);
    const ms = playing ? PREVIEW_OUT_PLAYING_MS : PREVIEW_OUT_MS;
    o.put({ ...p, out: playing ? 'playing' : 'plain', back: false });
    letGo();
    if (heard(p.id)) o.map().player.fade(0, ms);
    stopUnmount();
    unmount = window.setTimeout(() => {
      unmount = 0;
      const then = next;
      next = null;
      // Its player goes here, while the frame is still in the page, so
      // the volume put back as it goes reaches YouTube. MapPreview's own
      // unmount would drop it too, but only once the frame has gone.
      o.map().player.drop('preview', p.id);
      o.showing.current = null;
      o.put(null);
      // The next card's preview, if the pointer is still on that card.
      const q = then != null && o.under.current === then ? placeFor(then) : null;
      if (q) show(q);
    }, ms + PREVIEW_DRIFT_EXTRA_MS + 20);
  };

  return {
    showing: () => o.showing.current,
    // Where to watch is asked for as the pointer begins to rest, rather
    // than as the preview opens, so the preview's Stream row is mostly
    // there from its first paint. Only for a card whose preview could
    // open: the API behind it is paid for by the request.
    resting: (id) => {
      if (opens(id)) void askWhereToWatch(id);
    },
    playing: () => o.map().player.now()?.where === 'preview',
    open: (id) => {
      const cur = o.now();
      const still = stillNow();
      // A preview with its trailer set leaves first, sound and blur and
      // all, and the next opens as it goes. For a reader who has asked
      // for nothing to move, it is simply replaced, as any other is.
      if (cur && playsIn(cur.id) && !still) {
        next = id;
        leave();
        return;
      }
      const q = placeFor(id);
      if (!q) return;
      if (cur && !still) {
        // The old one fades out under the new one, which mounts with its
        // own key, so its entrance runs. One already fading is replaced
        // at once: it has nearly gone.
        stopUnmount();
        next = null;
        stopGone();
        if (cur.id !== id) {
          o.gone(cur);
          goneTimer = window.setTimeout(stopGone, PREVIEW_SWAP_OUT_MS + 40);
        }
      }
      show(q);
    },
    close: (hard) => {
      if (!hard) {
        // A scroll or Escape forgets the card waiting on this leave, as it
        // forgets any other open on its way.
        next = null;
        leave();
        return;
      }
      stopGone();
      takeAway();
    },
    keep: () => {
      const p = o.now();
      if (!p?.out) return;
      stopUnmount();
      next = null;
      o.put({ ...p, out: undefined, back: true });
      const { light, said, player } = o.map();
      o.held(p.id);
      light(said(p.id)?.people ?? []);
      if (heard(p.id)) player.fade(100, PREVIEW_BACK_MS);
    },
    // The pointer has left this card, perhaps on its way to its preview.
    // The card keeps its hover state, and the chips it lit stay lit, until
    // the preview closes or the pointer settles on another card. A preview
    // already leaving carries on leaving, and its card's hover goes with
    // it: nothing would put that back once the preview had gone.
    leftCard: (id) => {
      if (o.showing.current !== id || o.now()?.out) return;
      const { light, said } = o.map();
      o.held(id);
      light(said(id)?.people ?? []);
    },
    dispose: () => {
      window.clearTimeout(unmount);
      window.clearTimeout(goneTimer);
    },
  };
}

/** The hover preview's state and the pointer's part in it. A pointer
 *  that can rest precisely, resting on a card that can be opened, opens
 *  its preview beside it (see previewScheduler for when). Moving onto
 *  the preview keeps it, and the card keeps its hover state and its
 *  chips lit meanwhile. The pointer leaving, a scroll or Escape let it
 *  leave as it came, fading back towards its card, and a pointer back on
 *  it or its card before it has gone brings it back. Another map, a
 *  panel or View opening, a new layout and reduced motion take it away
 *  at once, except while its trailer is fullscreen. On a swap between
 *  cards the old preview fades out under the new one (`gone`), unless
 *  its trailer is set: then it leaves first, and the next card's opens
 *  as it goes if the pointer is still there (see previewHost). */
function usePreview(o: PreviewOptions): {
  /** The preview to draw, if there is one. */
  peek: Peek | null;
  /** The preview giving way to `peek` on a swap, fading out under it. */
  gone: Peek | null;
  /** The card drawn as hovered for its preview. */
  held: string | null;
  onCard: (id: string, over: boolean) => void;
  onEnter: () => void;
  onLeave: () => void;
  bounds: (loose: boolean) => PreviewBounds;
} {
  // Read by timers and listeners that outlive the render they began in.
  const live = useRef(o);
  live.current = o;
  const [peek, setPeek] = useState<Peek | null>(null);
  // The same, as of now, for timers and handlers between renders.
  const peekNow = useRef<Peek | null>(null);
  const [put] = useState(() => (next: Peek | null) => {
    peekNow.current = next;
    setPeek(next);
  });
  const [gone, setGone] = useState<Peek | null>(null);
  // What is showing as of now, for a pointer faster than a render. A
  // leaving preview still counts until it has gone.
  const showing = useRef<string | null>(null);
  const [held, setHeld] = useState<string | null>(null);
  // The card the pointer is on, if it is on one.
  const under = useRef<string | null>(null);

  const [bounds] = useState(() => (loose: boolean) => {
    const { layout, scroller, overlayH, headerAway, floatClear } = live.current;
    const el = scroller.current ?? { scrollLeft: 0, scrollTop: 0, clientWidth: 0, clientHeight: 0 };
    return previewBounds(el, layout?.metrics.railW ?? 0, overlayH, headerAway ? 0 : overlayH, floatClear, loose);
  });

  const [host] = useState(() =>
    previewHost({
      map: () => live.current,
      bounds,
      now: () => peekNow.current,
      put,
      gone: setGone,
      held: setHeld,
      showing,
      under,
    }),
  );
  const [sched] = useState(() => previewScheduler(host));
  useEffect(
    () => () => {
      sched.dispose();
      host.dispose();
    },
    [sched, host],
  );

  // Whether the preview's own trailer has gone fullscreen. The browser
  // then sizes the page's viewport to the screen, so the map lays itself
  // out again, and pins its scroll, under a preview the reader cannot
  // see. Closing it for that would unmount the frame and end the
  // fullscreen the moment it began.
  const [fullscreen] = useState(() => () => {
    const f = live.current.player.frame.current;
    return f != null && document.fullscreenElement === f;
  });

  useEffect(() => {
    if (o.blocked) sched.shut(true);
  }, [o.blocked, sched]);
  useEffect(() => {
    if (!fullscreen()) {
      sched.shut(true);
      return;
    }
    // Carried across to the new layout, so it is still drawn. Its place
    // was worked out on the old one, which is out of sight behind the
    // fullscreen frame; once that ends, the next new layout or scroll
    // closes it as usual.
    const now = o.layout;
    const p = peekNow.current;
    if (p && now) put({ ...p, on: now });
  }, [o.layout, sched, fullscreen, put]);
  useEffect(() => {
    const el = o.scroller.current;
    const onScroll = () => {
      if (!fullscreen()) sched.shut();
    };
    // In the capture phase, before this same press reaches the search
    // field, which empties itself on it: a field with text in it takes
    // this Escape, and the preview waits for the next one.
    const onKey = (e: KeyboardEvent) => {
      if (e.key === 'Escape' && !live.current.searchTyped) sched.shut();
    };
    el?.addEventListener('scroll', onScroll, { passive: true });
    window.addEventListener('keydown', onKey, true);
    return () => {
      el?.removeEventListener('scroll', onScroll);
      window.removeEventListener('keydown', onKey, true);
    };
  }, [o.scroller, sched, fullscreen]);

  const onCard = useCallback(
    (id: string, over: boolean) => {
      if (over) {
        under.current = id;
        // The pointer is on a card of its own now, which is hovered in
        // its own right.
        setHeld(null);
        if (canRest()) sched.rest(id);
        return;
      }
      if (under.current === id) under.current = null;
      host.leftCard(id);
      sched.leave();
    },
    [sched, host],
  );

  const onEnter = useCallback(() => {
    sched.hold();
    const id = showing.current;
    if (!id) return;
    setHeld(id);
    live.current.light(live.current.said(id)?.people ?? []);
  }, [sched]);

  const onLeave = useCallback(() => sched.leave(), [sched]);

  // Read as it draws, not only once the effect above has carried it
  // across: the draw with the new layout comes first, and a preview
  // missing from it would already be gone, and its frame with it.
  const drawn = peek && (peek.on === o.layout || fullscreen()) ? peek : null;
  const fading = gone && gone.on === o.layout ? gone : null;
  return { peek: drawn, gone: fading, held, onCard, onEnter, onLeave, bounds };
}

export const Card = memo(function Card({
  card,
  layout,
  people,
  codes,
  said,
  opacity,
  off = false,
  eager,
  enter,
  ringed,
  flown = false,
  snap = false,
  theme,
  ghost = false,
  hover = false,
  onOpen,
  onHover,
  onPreview,
}: {
  card: Placed;
  /** What this card says, once it has arrived. Its place is already
   *  settled either way, so nothing moves when it does. */
  said: GridFilm | undefined;
  layout: GridLayout;
  people: Map<string, GridPerson>;
  codes: Map<string, string>;
  opacity: number;
  /** Filtered out by the chosen people, the rating floor or the genres (see
   *  filteredOut): dimmed, and with nothing to open. It cannot be
   *  clicked, tapped, hovered or focused. */
  off?: boolean;
  /** On the glass, so its poster loads ahead of the ones waiting above and below. */
  eager: boolean;
  /** Set while the map is opening: whether this card is still waiting to
   *  appear, and how long it waits once they are all let go. */
  enter: { hidden: boolean; delay: number } | null;
  /** Just been scrolled back to, and saying so for a moment. */
  ringed: boolean;
  /** Taken off the map by a copy on its way to becoming the next one:
   *  gone in a single frame, as the copy appears over it. */
  flown?: boolean;
  /** Shown and hidden without a fade: the searched card while a copy
   *  lands on it, and for the ring after. */
  snap?: boolean;
  /** Which theme the poster fallback and the people's colours are
   *  mixed for. */
  theme: Theme;
  /** A copy of a card a reflow has just taken off the map, drawn where
   *  it was while it closes into its seam (see useReflow). */
  ghost?: boolean;
  /** Drawn as if the pointer were on it, as it is on its hover preview
   *  or on the way there. */
  hover?: boolean;
  onOpen: (filmId: string) => void;
  onHover: (people: string[]) => void;
  /** The pointer has come to rest on this card, or left it, for the
   *  hover preview. Never called for a filtered-out card. */
  onPreview?: (filmId: string, over: boolean) => void;
}) {
  const { film } = card;
  const { cardW, cardH, titleLines, posterW, posterH } = layout.metrics;
  const on = said?.people ?? [];
  // The searched film is everyone's, so saying so on the card says nothing.
  const markers = film.isAnchor
    ? { show: [], extra: 0, initials: false }
    : markersFor(on, layout.metrics, film.rating, codes);
  const fill = {
    ['--poster-fill' as string]: posterFallback(said?.title ?? String(film.id), theme),
  };
  const waiting = enter?.hidden ?? false;
  const shownAt = waiting ? 0 : opacity;
  // The searched card says so in its label, so the tag beside it is
  // drawn and not read.
  const label = `${said?.title ?? 'Loading'}, ${film.year}, rated ${film.rating == null ? 'not yet' : film.rating.toFixed(1)}${film.isAnchor ? ', the searched movie' : ''}${off ? ', filtered out' : ''}`;
  const tag = film.isAnchor && !ghost ? searchedTagAt(card) : null;
  // Whether the pointer is resting on this card. A card the filters turn
  // off under the pointer lets go of the chips it lit itself: a disabled
  // button may never hear the pointer leave.
  const under = useRef(false);
  useEffect(() => {
    if (!off || !under.current) return;
    under.current = false;
    onHover([]);
  }, [off, onHover]);
  // Taken off the map with the pointer still on it, as when hiding the
  // empty years closes up its row: nothing says the pointer left then
  // either. onHover and onPreview are stable, so this runs only as the
  // card goes.
  useEffect(
    () => () => {
      if (!under.current) return;
      onHover([]);
      onPreview?.(film.id, false);
    },
    // A card's film never changes: it is keyed by it.
    // eslint-disable-next-line react-hooks/exhaustive-deps
    [onHover, onPreview],
  );
  return (
    <>
      <button
        type="button"
        data-card={ghost ? undefined : film.id}
        data-card-leaving={ghost ? film.id : undefined}
        aria-hidden={ghost || undefined}
        inert={ghost || undefined}
        disabled={off || undefined}
        // A card waiting to spread is put in its hidden state at once
        // (.cd-card-held has no transitions), and let go into it from
        // there with its own delay.
        className={`cd-card${film.isAnchor ? ' cd-card-anchor' : ''}${said ? '' : ' cd-card-waiting'}${waiting ? ' cd-card-held' : enter ? ' cd-card-entering' : ''}${ringed ? ' cd-card-ringed' : ''}${flown ? ' cd-card-flown' : ''}${snap ? ' cd-card-snap' : ''}${off ? ' cd-card-off' : ''}${hover && !off ? ' cd-card-hover' : ''}`}
        style={{
          left: card.left,
          top: card.top,
          width: cardW,
          height: cardH,
          opacity: shownAt,
          transform: waiting ? `translateY(${SPREAD_RISE_PX}px) scale(${SPREAD_SCALE})` : undefined,
          transitionDelay: enter && !waiting && enter.delay > 0 ? spreadDelays(enter.delay) : undefined,
          pointerEvents: waiting || ghost ? 'none' : undefined,
          ['--lines' as string]: titleLines,
          ['--poster-w' as string]: `${posterW}px`,
        }}
        tabIndex={waiting ? -1 : undefined}
        aria-label={label}
        // Guarded as well as disabled: browsers differ over which mouse
        // events a disabled button still gets, and a filtered-out card
        // must neither open nor light chips from whichever do arrive.
        onClick={() => {
          if (!off) onOpen(film.id);
        }}
        onMouseEnter={() => {
          if (off) return;
          under.current = true;
          onHover(on);
          onPreview?.(film.id, true);
        }}
        onMouseLeave={() => {
          under.current = false;
          onHover([]);
          onPreview?.(film.id, false);
        }}
      >
        <PosterImage
          id={film.id}
          url={said?.poster}
          cssPx={posterW}
          className="cd-card-poster"
          width={posterW}
          height={posterH}
          eager={eager}
          style={fill}
        />
        <span className="cd-card-body">
          <span className="cd-card-title">{said?.title ?? ''}</span>
          <span className="cd-card-foot">
            <span className={`cd-card-rating${film.rating == null ? ' cd-card-unrated' : ''}`}>
              {ratingText(film.rating)}
            </span>
            <span className="cd-card-spacer" />
            {/* For the eye only: the card's own label is what is read
                out, and the sheet names everyone by name. */}
            {markers.show.map((id) => (
              <span
                key={id}
                className="cd-card-mark"
                style={personVars({ id, role: people.get(id)?.role ?? 'cast' }, theme)}
                aria-hidden="true"
              >
                <span className="cd-card-swatch" />
                {markers.initials && (codes.get(id) ?? '?')}
              </span>
            ))}
            {markers.extra > 0 && (
              <span className="cd-more" aria-hidden="true">
                +{markers.extra}
              </span>
            )}
          </span>
        </span>
      </button>
      {tag && (
        // Beside the card rather than in it, because the card clips what
        // overflows it. It comes and goes with the card, and a reflow
        // carries it along by the same id.
        <span
          className={`cd-searched-tag${snap ? ' cd-card-snap' : ''}`}
          data-card-tag={film.id}
          aria-hidden="true"
          style={{ left: tag.left, top: tag.top, opacity: shownAt }}
        >
          Searched
        </span>
      )}
    </>
  );
});

/** What a card says. The searched film's words come with the map, so its
 *  card has them from the first paint rather than waiting on the detail
 *  like the rest: a copy landing on it must be taken over by the same
 *  card, not by an empty box that fills in a moment later. */
export function wordsFor(
  film: { id: string; isAnchor: boolean },
  detail: Map<string, GridFilm>,
  anchor: GridFilm,
): GridFilm | undefined {
  return detail.get(film.id) ?? (film.isAnchor && anchor.id === film.id ? anchor : undefined);
}

/** Whether this is the card a copy has just taken off from. Never the
 *  searched film: that is the one the copy is flying to, on the next map. */
export function isFlown(card: Placed, flown: string | null): boolean {
  return flown != null && card.film.id === flown && !card.film.isAnchor;
}

/** A card is full strength when nothing is narrowing the grid, or when it
 *  holds someone being previewed or selected, clears the rating floor and
 *  has every genre picked. A hovered chip previews just that person and
 *  overrides the selection while the pointer is on it; a card without
 *  the genres dims under it as one under the floor does.
 *
 *  Judged on the spine, which says who is on every card from the first
 *  paint. The detail is not needed, and waiting for it would light a card
 *  scrolled into view under a selection and then dim it when its words
 *  arrived.
 *
 *  Narrowing changes opacity, and whether a card opens (see
 *  filteredOut); which rows are drawn is Hide empty years' business, in
 *  layoutGrid. The searched film is the one exception: it is the centre
 *  of its own map and stays lit. */
export function opacityOf(
  card: Placed,
  /** The selection, as places in the chip row. */
  selected: Set<number>,
  /** The chip being previewed, as a place in the chip row. A person
   *  the row does not hold — a hover left over from the film just
   *  left — is any index no card carries, and dims them all. */
  hovered: number | null,
  minRating: number | null = null,
  /** The genres picked, as spine bits (see genreMask). */
  want = 0,
): number {
  if (card.film.isAnchor) return 1;
  if (hovered != null) {
    return passesFloor(card.film.rating, minRating) &&
      hasGenres(card.film.genres, want) &&
      card.film.people.includes(hovered)
      ? 1
      : DIM_PREVIEW;
  }
  return isLit(card.film, selected, minRating, want) ? 1 : DIM_SELECTED;
}

/** Whether a card is filtered out: dimmed by the chosen people, the
 *  rating floor or the genres, the same test that gives it DIM_SELECTED
 *  above, and so a card with nothing to open.
 *
 *  A chip preview is not part of it. It dims cards only while the
 *  pointer is on the chip, and a card the selection lights stays one
 *  the reader can open. The searched film is never filtered out. */
export function filteredOut(
  film: SpineFilm,
  selected: Set<number>,
  minRating: number | null,
  want = 0,
): boolean {
  return !film.isAnchor && !isLit(film, selected, minRating, want);
}
