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
  inWarmSpan,
  initialsFor,
  isLit,
  layoutGrid,
  markersFor,
  passesFloor,
  railLabelTop,
  revealDelay,
  searchedTagAt,
  type GridFilm,
  warmSpan,
  type GridLayout,
  type GridPayload,
  type GridPerson,
  type GridSettings,
  type Placed,
  type SpineFilm,
} from './grid';
import { PosterImage } from './PosterImage';
import { posterFallback, sheetPosterPx, sheetPosterURL } from './poster';
import { useScreen } from './screen';
import { personVars } from './personColour';
import { canHover, useOffScreen, useTapGuard } from './tap';
import { useResolvedTheme, type Theme } from './theme';
import { markAppScroll, type AppScroll } from './overHeader';
import {
  ARRIVE_DELAY_MS,
  ARRIVE_MS,
  AXIS_FADE_MS,
  FLIP_MS,
  GHOST_MS,
  REFLOW_MS,
  REVEAL_FLIP_MS,
  SPREAD_AFTER_LANDING_MS,
  SPREAD_RISE_PX,
  SPREAD_SCALE,
  animate,
  revealWindow,
  stillNow,
  type Box,
} from './motion';

/** A flown copy of a card on its way to land on this map's searched
 *  film (see GridApp's glideTo). The map hides that card until the copy
 *  is on it, then shows it in the same frame the copy goes. */
export interface Landing {
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
 *  Selecting people changes opacity and nothing else — the layout is
 *  computed from the payload and the width alone, so a card never moves
 *  because of who is selected. */
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
  compact,
  appScroll,
  flown = null,
  leaving = false,
  landing = null,
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
    const scrollTop = Math.floor(Math.max(0, el.scrollTop - lift.current) / WARM_STEP) * WARM_STEP;
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
            (f) => isLit(f, selectedIdx, settings.minRating),
            compact,
          )
        : null,
    [payload, width, settings, selectedIdx, compact],
  );
  // What the rows are laid out against, so a reflow can tell a change
  // of filter from a change of map or of width.
  const filterSig = `${settings.hideEmptyYears}|${settings.minRating}|${[...selectedIdx].sort((a, b) => a - b).join(',')}`;
  const reflow = useReflow(
    layout,
    filterSig,
    scroller,
    settings.hideEmptyYears,
    appScroll,
    payload.anchor.id,
  );
  const codes = useMemo(() => initialsFor(payload.people), [payload.people]);
  const byId = useMemo(
    () => new Map(payload.people.map((p) => [p.id, p])),
    [payload.people],
  );
  // The chip under the pointer, in the spine's own terms. -1 for an id
  // this row does not hold, which no card carries.
  const hoveredIdx = useMemo(
    () => (hovered == null ? null : payload.people.findIndex((p) => p.id === hovered)),
    [hovered, payload.people],
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
  const scrollTop =
    placedFor === payload.anchor.id && view.height > 0
      ? view.scrollTop
      : layout
        ? openedAt(layout, viewH)
        : 0;
  const span = warmSpan(scrollTop, viewH);
  const screen = { top: scrollTop, bottom: scrollTop + viewH };
  const cards = layout
    ? layout.cards.filter((c) => inWarmSpan(c.top, layout.metrics.cardH, span))
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
  const sheetPx = sheetPosterPx(useScreen());
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

  // While a copy lands on it, the searched card is not drawn, and it
  // appears without a fade when the copy goes (see `snapped`).
  const anchorHidden = landing != null;
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
            style={{ width: layout.plotW, ['--rail-w' as string]: `${layout.metrics.railW}px` }}
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
            <div className="cd-plot" style={{ height: layout.plotH }}>
              {layout.rows.map((r) =>
                r.isBreak ? (
                  <div
                    key="break"
                    className="cd-band cd-band-break"
                    style={{ top: r.top, height: r.height }}
                    aria-hidden="true"
                  />
                ) : (
                  <div
                    key={r.year}
                    className={`cd-band${r.index % 2 === 1 ? ' cd-band-odd' : ''}${r.anchorYear ? ' cd-band-anchor' : ''}${r.decade ? ' cd-band-decade' : ''}`}
                    style={{ top: r.top, height: r.height }}
                  />
                ),
              )}
              {settings.showUnrated && (
                <div className="cd-unrated-edge" style={{ left: layout.unratedEdge }} />
              )}
              {layout.lines.map((l) => (
                <div key={l.rating} className="cd-gridline" style={{ left: l.x }} />
              ))}
              {reflow.ghosts.map((c) => (
                <Card
                  key={`ghost:${c.film.id}`}
                  card={c}
                  said={detail.get(c.film.id)}
                  layout={layout}
                  people={byId}
                  codes={codes}
                  opacity={reflow.ghostsOut ? 0 : 1}
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
                  opacity={
                    (c.film.isAnchor && anchorHidden) || isFlown(c, flown)
                      ? 0
                      : opacityOf(c, selectedIdx, hoveredIdx, settings.minRating)
                  }
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
                  arriving={reflow.arriving.has(c.film.id)}
                  fadingIn={reflow.fadingIn.has(c.film.id)}
                  onOpen={openIfMeant}
                  onHover={lightIfHovering}
                />
              ))}
              <div className="cd-rail-layer" style={{ height: layout.plotH, width: layout.plotW }}>
                <div className="cd-rail" style={{ width: layout.metrics.railW, height: layout.plotH }}>
                  {layout.rows.map((r) =>
                    r.isBreak ? (
                      <span
                        key="break"
                        className="cd-rail-year cd-rail-break"
                        style={{ top: railLabelTop(r) }}
                        aria-hidden="true"
                      >
                        · · ·
                      </span>
                    ) : (
                      <span
                        key={r.year}
                        className={`cd-rail-year${r.decade ? ' cd-rail-decade' : ''}${r.anchorYear ? ' cd-rail-anchor' : ''}`}
                        style={{ top: railLabelTop(r) }}
                      >
                        {r.year}
                      </span>
                    ),
                  )}
                </div>
              </div>
            </div>
          </div>
        )}
      </div>
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
          Recenter
        </span>
      </button>
    </>
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
  const lit = (f: SpineFilm) => isLit(f, none, settings.minRating);
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

/** The FLIP reflow for hiding and showing the empty years.
 *
 *  Rows leaving above the reader would carry the whole map up the
 *  screen, so the searched film is pinned — not a card that happens to
 *  be on the glass, which is what an ordinary relayout pins. This is
 *  the one movement the reader asked for, and it should look like the
 *  map closing up around the film it is of.
 *
 *  Cards are moved by writing to their style directly. React has just
 *  committed their new positions; what is wanted is the old one for a
 *  single frame, and a state round trip for that would be a frame late.
 */
function useReflow(
  layout: GridLayout | null,
  sig: string,
  scroller: RefObject<HTMLDivElement | null>,
  hiding: boolean,
  appScroll: RefObject<AppScroll> | undefined,
  mapId: string,
): {
  ghosts: Placed[];
  ghostsOut: boolean;
  /** Cards the reflow has just placed, held out of sight for a frame. */
  arriving: Set<string>;
  /** The same cards, fading in to their own opacity (.cd-card-arrive). */
  fadingIn: Set<string>;
  /** Set for the one layout this hook moved the scroller for, so the
   *  ordinary pin does not move it a second time. */
  handled: RefObject<boolean>;
} {
  const handled = useRef(false);
  const last = useRef<{ mapId: string; sig: string; hiding: boolean; cards: Placed[] } | null>(
    null,
  );
  const [ghosts, setGhosts] = useState<Placed[]>([]);
  // A ghost mounts where it was, at the opacity it had, and is let go a
  // frame later. Mounted already faded, it would simply vanish.
  const [ghostsOut, setGhostsOut] = useState(false);
  const [arriving, setArriving] = useState<Set<string>>(new Set());
  const [fadingIn, setFadingIn] = useState<Set<string>>(new Set());
  const timers = useRef<number[]>([]);
  useEffect(
    () => () => {
      for (const t of timers.current) window.clearTimeout(t);
    },
    [],
  );

  useLayoutEffect(() => {
    const el = scroller.current;
    if (!layout || !el) return;
    const before = last.current;
    last.current = { mapId, sig, hiding, cards: layout.cards };
    // Only a change of filter reflows. A new map, a resize or a year
    // range each put the reader somewhere else entirely, and gliding
    // three hundred cards across that would be motion about nothing.
    // Back onto a map whose empty years were hidden is a new map too,
    // though its filters differ from the last one's.
    if (!before || before.mapId !== mapId || before.sig === sig || (!hiding && !before.hiding)) {
      return;
    }

    const was = new Map(before.cards.map((c) => [c.film.id, c]));
    const now = new Map(layout.cards.map((c) => [c.film.id, c]));
    const anchorId = layout.anchor?.film.id;
    const anchorWas = anchorId ? was.get(anchorId) : undefined;
    const anchorNow = anchorId ? now.get(anchorId) : undefined;
    const shift = anchorWas && anchorNow ? anchorNow.top - anchorWas.top : 0;
    if (shift !== 0) {
      markAppScroll(appScroll, false);
      el.scrollTop += shift;
    }
    handled.current = true;

    const left = before.cards.filter((c) => !now.has(c.film.id));
    const came = new Set([...now.keys()].filter((id) => !was.has(id)));

    if (stillNow()) {
      setGhosts([]);
      setArriving(new Set());
      setFadingIn(new Set());
      return;
    }

    setGhosts(left);
    setGhostsOut(false);
    setArriving(came);
    setFadingIn(came);
    timers.current.push(window.setTimeout(() => setGhostsOut(true), FLIP_MS));
    timers.current.push(
      window.setTimeout(() => {
        setGhosts([]);
        setGhostsOut(false);
      }, FLIP_MS + GHOST_MS),
    );
    // Let go once the hidden state has been painted. The fade itself
    // waits its 60 ms in the stylesheet (.cd-card-arrive), so the cards
    // that only moved are on their way before anything new appears
    // among them, and it ends at the card's own opacity — a card the
    // filters dim arrives dim rather than lit and then dimmed.
    timers.current.push(window.setTimeout(() => setArriving(new Set()), FLIP_MS));
    timers.current.push(
      window.setTimeout(() => setFadingIn(new Set()), FLIP_MS + ARRIVE_DELAY_MS + ARRIVE_MS),
    );

    // Put every card that stayed back where it was on screen, then let
    // it go on the next frame.
    const moved: HTMLElement[] = [];
    for (const [id, card] of now) {
      const old = was.get(id);
      if (!old) continue;
      const dx = old.left - card.left;
      const dy = old.top - card.top + shift;
      if (dx === 0 && dy === 0) continue;
      // The searched card's tag sits beside it, not in it, so it is
      // moved with it.
      const key = CSS.escape(id);
      for (const node of el.querySelectorAll<HTMLElement>(
        `[data-card="${key}"], [data-card-tag="${key}"]`,
      )) {
        node.style.transition = 'none';
        node.style.transform = `translate(${dx}px, ${dy}px)`;
        moved.push(node);
      }
    }
    if (moved.length === 0) return;
    const frame = requestAnimationFrame(() => {
      for (const node of moved) {
        node.style.transition = `transform ${REFLOW_MS}ms var(--ease-glide)`;
        node.style.transform = '';
      }
      timers.current.push(
        window.setTimeout(() => {
          for (const node of moved) {
            node.style.transition = '';
            node.style.transform = '';
          }
        }, REFLOW_MS),
      );
    });
    return () => cancelAnimationFrame(frame);
  }, [layout, sig, hiding, scroller, appScroll, mapId]);

  return { ghosts, ghostsOut, arriving, fadingIn, handled };
}

const Card = memo(function Card({
  card,
  layout,
  people,
  codes,
  said,
  opacity,
  eager,
  enter,
  ringed,
  flown = false,
  snap = false,
  theme,
  arriving = false,
  fadingIn = false,
  ghost = false,
  onOpen,
  onHover,
}: {
  card: Placed;
  /** What this card says, once it has arrived. Its place is already
   *  settled either way, so nothing moves when it does. */
  said: GridFilm | undefined;
  layout: GridLayout;
  people: Map<string, GridPerson>;
  codes: Map<string, string>;
  opacity: number;
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
  /** Just placed by a reflow, and held out of sight for a frame. */
  arriving?: boolean;
  /** Just placed by a reflow and fading in, a moment behind the cards
   *  that only moved. */
  fadingIn?: boolean;
  /** A card that has just left, held at its old place for long enough
   *  to fade rather than vanish. */
  ghost?: boolean;
  onOpen: (filmId: string) => void;
  onHover: (people: string[]) => void;
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
  const shownAt = waiting || arriving ? 0 : opacity;
  // The searched card says so in its label, so the tag beside it is
  // drawn and not read.
  const label = `${said?.title ?? 'Loading'}, ${film.year}, rated ${film.rating == null ? 'not yet' : film.rating.toFixed(1)}${film.isAnchor ? ', the searched movie' : ''}`;
  const tag = film.isAnchor && !ghost ? searchedTagAt(card) : null;
  return (
    <>
      <button
        type="button"
        data-card={ghost ? undefined : film.id}
        aria-hidden={ghost || undefined}
        inert={ghost || undefined}
        // A card waiting to spread is put in its hidden state at once
        // (.cd-card-held has no transitions), and let go into it from
        // there with its own delay.
        className={`cd-card${film.isAnchor ? ' cd-card-anchor' : ''}${said ? '' : ' cd-card-waiting'}${waiting ? ' cd-card-held' : enter ? ' cd-card-entering' : ''}${ringed ? ' cd-card-ringed' : ''}${flown ? ' cd-card-flown' : ''}${snap ? ' cd-card-snap' : ''}${fadingIn ? ' cd-card-arrive' : ''}${ghost ? ' cd-card-ghost' : ''}`}
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
        onClick={() => onOpen(film.id)}
        onMouseEnter={() => onHover(on)}
        onMouseLeave={() => onHover([])}
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
              {film.rating == null ? 'No rating' : film.rating.toFixed(1)}
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
 *  holds someone being previewed or selected and clears the rating floor.
 *  A hovered chip previews just that person and overrides the selection
 *  while the pointer is on it.
 *
 *  Judged on the spine, which says who is on every card from the first
 *  paint. The detail is not needed, and waiting for it would light a card
 *  scrolled into view under a selection and then dim it when its words
 *  arrived.
 *
 *  Narrowing only ever changes opacity. The searched film is the one
 *  exception: it is the centre of its own map and stays lit. */
export function opacityOf(
  card: Placed,
  /** The selection, as places in the chip row. */
  selected: Set<number>,
  /** The chip being previewed, as a place in the chip row. A person
   *  the row does not hold — a hover left over from the film just
   *  left — is any index no card carries, and dims them all. */
  hovered: number | null,
  minRating: number | null = null,
): number {
  if (card.film.isAnchor) return 1;
  if (hovered != null) {
    return passesFloor(card.film.rating, minRating) && card.film.people.includes(hovered)
      ? 1
      : DIM_PREVIEW;
  }
  return isLit(card.film, selected, minRating) ? 1 : DIM_SELECTED;
}
