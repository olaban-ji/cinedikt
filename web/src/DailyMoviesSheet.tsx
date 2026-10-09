import {
  useEffect,
  useLayoutEffect,
  useMemo,
  useRef,
  useState,
  type CSSProperties,
  type JSX,
  type KeyboardEvent,
  type PointerEvent,
  type Ref,
} from 'react';
import { ApiError, fetchDailyMovies, type DailyGame, type DailyMovie } from './api';
import { toneStyle } from './daily';
import {
  MOVIES_LOADING,
  NO_PICKS,
  SHEET_CARD_PAD,
  SHEET_POSTER_W,
  moviesFailed,
  moviesKey,
  phoneSheetHeight,
  sheetLayout,
  sheetView,
  tabWrap,
  togglePick,
  widenReach,
  type SheetCard,
  type SheetChip,
  type SheetPicks,
  type SheetView,
} from './dailyMovies';
import { warmSpan, warmTop } from './grid';
import { wheelSideways } from './PeopleChips';
import { PersonFace } from './PersonFace';
import { PosterImage } from './PosterImage';
import { posterFallback } from './poster';
import { useScreen } from './screen';
import { useDrag, useEscape, useFocusTrapped } from './sheet';
import type { Theme } from './theme';

// The Movies sheet: one showing person's movies on a small Cinedikt map,
// with the reader's bought facts drawn on it and today's movie among the
// cards, unmarked. Its rules — what is lit, the title, the legend, the
// footer, the metrics — are daily.ts's (the Movies sheet section), put
// together for the sheet in dailyMovies.ts. This file asks the server for
// the movies, keeps what the reader has switched on and tapped, and draws
// it (MoviesSheetView, which takes everything as it stands, so it can be
// rendered without a DOM).
//
// A side panel held 12px in from the right, top and bottom from 640px up;
// below that a bottom sheet 90% of the visual viewport tall, with a
// grabber, closed by dragging its top edge down past 90px. The scrim, the
// close button and Escape close it on every size.

export interface MoviesSheetProps {
  /** The puzzle's number, for GET /daily/{no}/movies. */
  no: number;
  /** The person the sheet opens on, by IMDb name id: always selected, and
   *  never switched off. */
  person: string;
  /** The game as it stands: the showing slots, the facts bought, the
   *  overlaps owned, the wrong guesses, the points and the next wrong
   *  guess's price. */
  game: DailyGame;
  theme: 'light' | 'dark';
  /** Buys a showing person's overlap; true once it is theirs. */
  onOverlap(person: string): Promise<boolean>;
  /** Guess it: the page closes the sheet and guesses this movie, by IMDb
   *  id, as usual. */
  onGuess(id: string): void;
  /** The scrim, Escape, the close button, or a drag down on a phone. */
  onClose(): void;
}

/** The person's movies: on their way, here, or not to be had, with what
 *  to say. Kept with whose they are, so the movies of the person before
 *  are never drawn under another's name. */
type Load =
  | { state: 'wait' }
  | { state: 'ok'; person: string; movies: DailyMovie[] }
  | { state: 'failed'; text: string };

/** Everything in the sheet that takes the focus, in the order Tab visits
 *  it. A disabled button does not. */
const FOCUSABLE = 'button:not(:disabled), [href], input:not(:disabled), [tabindex]:not([tabindex="-1"])';

export function DailyMoviesSheet(props: MoviesSheetProps): JSX.Element {
  const { no, person, game, theme, onOverlap, onGuess, onClose } = props;
  const { phone } = useScreen();
  const viewH = useViewHeight();
  const ref = useRef<HTMLDivElement>(null);
  const mapRef = useRef<HTMLDivElement>(null);
  const [load, setLoad] = useState<Load>({ state: 'wait' });
  const [attempt, setAttempt] = useState(0);
  const [picks, setPicks] = useState<SheetPicks>(NO_PICKS);
  const [pick, setPick] = useState<string | null>(null);
  const [buying, setBuying] = useState<string | null>(null);
  const [width, setWidth] = useState(0);
  const [reach, setReach] = useState<{ top: number; bottom: number } | null>(null);
  const drag = useDrag(phone, onClose);
  useEscape(onClose);
  useFocusTrapped(ref);

  // An overlap answered after the sheet has gone changes nothing here.
  // Set on the way in as well as cleared on the way out: StrictMode
  // mounts the sheet twice in development, and a flag only ever cleared
  // would leave a name held still for good after its first purchase.
  const alive = useRef(true);
  useEffect(() => {
    alive.current = true;
    return () => {
      alive.current = false;
    };
  }, []);

  // Asked once for the person, again on Try again, and again should the
  // showing names or Director change underneath it (moviesKey). A second
  // ask for the same person keeps the map it has until the answer comes.
  const key = moviesKey(game);
  useEffect(() => {
    const ctl = new AbortController();
    setLoad((was) => (was.state === 'ok' && was.person === person ? was : { state: 'wait' }));
    fetchDailyMovies(no, person, ctl.signal).then(
      (body) => setLoad({ state: 'ok', person, movies: body.movies ?? [] }),
      (err: unknown) => {
        if (ctl.signal.aborted) return;
        setLoad({ state: 'failed', text: moviesFailed(err instanceof ApiError ? err.reason : null) });
      },
    );
    return () => ctl.abort();
  }, [no, person, attempt, key]);

  // The layout follows the scroller's own width, which the stylesheet
  // keeps whether or not a scrollbar is showing (scrollbar-gutter), so
  // the map never relays itself out as its movies arrive.
  useLayoutEffect(() => {
    const el = mapRef.current;
    if (!el) return;
    const read = () => setWidth(el.clientWidth);
    read();
    if (typeof ResizeObserver !== 'function') {
      window.addEventListener('resize', read);
      return () => window.removeEventListener('resize', read);
    }
    const ro = new ResizeObserver(read);
    ro.observe(el);
    return () => ro.disconnect();
  }, []);

  const movies = load.state === 'ok' && load.person === person ? load.movies : null;
  const layout = useMemo(
    () => (movies && width > 0 ? sheetLayout(movies, width, phone) : null),
    [movies, width, phone],
  );
  const view = sheetView({ game, person, movies, layout, picks, pick, buying, reach, theme });

  // It opens scrolled to the first lit movie, once, as soon as there is a
  // map to scroll: a later change of who is switched on leaves the reader
  // where they are. Instant whatever the motion setting: nothing has been
  // drawn anywhere else yet to travel from.
  const opened = useRef(false);
  const scrollTo = view.scrollTo;
  useLayoutEffect(() => {
    const el = mapRef.current;
    if (opened.current || !el || !layout) return;
    opened.current = true;
    el.scrollTop = scrollTo;
    setReach(bandAt(el));
  }, [layout, scrollTo]);

  const onScroll = () => {
    const el = mapRef.current;
    if (!el || !opened.current) return;
    const band = bandAt(el);
    setReach((was) => widenReach(was, band));
  };

  const pressChip = (chip: SheetChip) => {
    if (!chip.can) return;
    if (chip.act === 'toggle') {
      setPicks((was) => togglePick(was, chip.key));
      setPick(null);
      return;
    }
    if (chip.act !== 'buy') return;
    // Held still until the server answers, so one tap is one purchase.
    // Once it is theirs it is switched on, which is what it was bought
    // for; a refusal the page has already said leaves it as it was.
    setBuying(chip.key);
    onOverlap(chip.key).then(
      (ok) => {
        if (!alive.current) return;
        setBuying(null);
        if (!ok) return;
        setPicks((was) => (was.others.includes(chip.key) ? was : togglePick(was, chip.key)));
        setPick(null);
      },
      () => {
        if (alive.current) setBuying(null);
      },
    );
  };

  const guess = () => {
    if (view.foot.kind === 'pick' && view.foot.can) onGuess(view.foot.id);
  };

  // A modal sheet keeps Tab inside it, round from the last control to
  // the first and back.
  const onKeyDown = (e: KeyboardEvent<HTMLDivElement>) => {
    if (e.key !== 'Tab' || !ref.current) return;
    const items = [...ref.current.querySelectorAll<HTMLElement>(FOCUSABLE)];
    const to = tabWrap(items.indexOf(document.activeElement as HTMLElement), items.length, e.shiftKey);
    if (to == null) return;
    e.preventDefault();
    items[to].focus();
  };

  // The top edge — grabber and header — follows a finger down, but not
  // from the close button, which is pressed, not dragged.
  const dragDown = (e: PointerEvent) => {
    if (e.target instanceof Element && e.target.closest('button')) return;
    drag.onPointerDown(e);
  };

  return (
    <MoviesSheetView
      view={view}
      phone={phone}
      theme={theme}
      height={phone ? phoneSheetHeight(viewH) : null}
      held={drag.held ? drag.y : 0}
      loading={load.state === 'wait'}
      failed={load.state === 'failed' ? load.text : null}
      dialogRef={ref}
      mapRef={mapRef}
      on={{
        close: onClose,
        chip: pressChip,
        card: setPick,
        guess,
        retry: () => setAttempt((n) => n + 1),
        key: onKeyDown,
        scroll: onScroll,
        dragDown,
        dragMove: drag.onPointerMove,
        dragUp: drag.onPointerUp,
      }}
    />
  );
}

/** The band of the map around where the scroller stands, in the warm
 *  band's steps: a screen above it and two below, as the map warms. */
function bandAt(el: HTMLElement): { top: number; bottom: number } {
  return warmSpan(warmTop(el.scrollTop, 0), el.clientHeight || window.innerHeight);
}

/** The visual viewport's height, which on iOS shrinks for the keyboard
 *  and the toolbars where the layout viewport does not. The window's
 *  inner height where there is no visual viewport. */
function viewHeight(): number {
  if (typeof window === 'undefined') return 0;
  return window.visualViewport?.height ?? window.innerHeight;
}

function useViewHeight(): number {
  const [h, setH] = useState(viewHeight);
  useEffect(() => {
    const vv = window.visualViewport;
    const read = () => setH(viewHeight());
    read();
    vv?.addEventListener('resize', read);
    window.addEventListener('resize', read);
    return () => {
      vv?.removeEventListener('resize', read);
      window.removeEventListener('resize', read);
    };
  }, []);
  return h;
}

export interface SheetHandlers {
  close(): void;
  chip(chip: SheetChip): void;
  card(id: string): void;
  guess(): void;
  retry(): void;
  key?(e: KeyboardEvent<HTMLDivElement>): void;
  scroll?(): void;
  dragDown?(e: PointerEvent): void;
  dragMove?(e: PointerEvent): void;
  dragUp?(e: PointerEvent): void;
}

export interface MoviesSheetViewProps {
  view: SheetView;
  phone: boolean;
  theme: Theme;
  /** The phone sheet's height in pixels; null for the side panel, which
   *  is held in from the top and the bottom instead. */
  height: number | null;
  /** How far down a drag is holding the phone sheet: it follows the
   *  finger with no transition, and springs back from wherever it is let
   *  go short of closing. */
  held: number;
  /** The movies are on their way. */
  loading: boolean;
  /** What to say when they cannot be had, with Try again. */
  failed: string | null;
  dialogRef?: Ref<HTMLDivElement>;
  mapRef?: Ref<HTMLDivElement>;
  on: SheetHandlers;
}

/** The sheet as it stands. */
export function MoviesSheetView({
  view,
  phone,
  theme,
  height,
  held,
  loading,
  failed,
  dialogRef,
  mapRef,
  on,
}: MoviesSheetViewProps) {
  const style: CSSProperties = {};
  if (height != null) style.height = height;
  if (held > 0) Object.assign(style, { transform: `translateY(${held}px)`, transition: 'none' });
  const foot = view.foot;
  return (
    <>
      <div className="cd-msheet-scrim" onClick={on.close} aria-hidden="true" />
      <div
        className={`cd-msheet${phone ? ' cd-msheet-phone' : ''}`}
        style={style}
        role="dialog"
        aria-modal="true"
        aria-label={view.title}
        tabIndex={-1}
        ref={dialogRef}
        onKeyDown={on.key}
      >
        <div
          className="cd-msheet-top"
          onPointerDown={on.dragDown}
          onPointerMove={on.dragMove}
          onPointerUp={on.dragUp}
          onPointerCancel={on.dragUp}
        >
          {phone && (
            <div className="cd-msheet-grab" aria-hidden="true">
              <span />
            </div>
          )}
          <div className="cd-msheet-head" style={view.person ? toneStyle(view.tone) : undefined}>
            {view.person && <PersonFace photo={view.person.photo} code={view.code} size="chip" square={false} />}
            <div className="cd-msheet-heading">
              <h2 className="cd-msheet-title">{view.title}</h2>
              {/* Said again as names are switched on and off: how much of
                  the map is still lit is what a switch is for. */}
              <p className="cd-msheet-sub" aria-live="polite">
                {view.sub}
              </p>
            </div>
            <button type="button" className="cd-msheet-close" aria-label="Close" onClick={on.close}>
              <svg
                width="18"
                height="18"
                viewBox="0 0 24 24"
                fill="none"
                stroke="currentColor"
                strokeWidth="2.2"
                strokeLinecap="round"
                aria-hidden="true"
                focusable="false"
              >
                <path d="M6 6l12 12M18 6L6 18" />
              </svg>
            </button>
          </div>
        </div>

        {/* Its scrollbar is hidden, so a mouse wheel is what moves it
            sideways on a desktop, as the map's own chip row does. */}
        <div className="cd-msheet-chips" onWheel={(e) => wheelSideways(e, e.currentTarget)}>
          {view.chips.map((c) => (
            <button
              key={c.key}
              type="button"
              className={`cd-msheet-chip${c.on ? ' cd-msheet-chip-on' : ''}${c.faint ? ' cd-msheet-chip-faint' : ''}`}
              style={toneStyle(c.tone)}
              // A name for sale is bought, not switched, so it says what
              // it costs rather than whether it is pressed.
              aria-pressed={c.act === 'buy' ? undefined : c.on}
              aria-label={c.aria || undefined}
              disabled={!c.can}
              onClick={() => on.chip(c)}
            >
              <PersonFace photo={c.person.photo} code={c.code} size="chip" square={c.director} />
              {c.label}
              {c.price && <span className="cd-msheet-price">{c.price}</span>}
            </button>
          ))}
        </div>

        <p className="cd-msheet-legend">{view.legend}</p>

        <div
          className="cd-msheet-map"
          ref={mapRef}
          role="region"
          aria-label="Movies by year and rating"
          aria-busy={loading || undefined}
          onScroll={on.scroll}
        >
          {failed ? (
            <div className="cd-msheet-note">
              <p>{failed}</p>
              <button type="button" className="cd-msheet-retry" onClick={on.retry}>
                Try again
              </button>
            </div>
          ) : loading ? (
            <div className="cd-msheet-note">
              <p>{MOVIES_LOADING}</p>
            </div>
          ) : (
            <div className="cd-msheet-plot" style={{ height: view.plotH }}>
              {view.ticks.length > 0 && (
                <div className="cd-msheet-axis" aria-hidden="true">
                  <span className="cd-msheet-axis-title">Rating →</span>
                  {view.ticks.map((t) => (
                    <span key={t.rating} className="cd-msheet-axis-label" style={{ left: t.labelLeft }}>
                      {t.label}
                    </span>
                  ))}
                </div>
              )}
              {view.rows.map((r) => (
                <div key={r.key} className={r.band} style={{ top: r.top, height: r.height }} aria-hidden="true">
                  <span className={`cd-msheet-year${r.inYears ? ' cd-msheet-year-in' : ''}`} style={{ top: r.labelTop }}>
                    {r.year}
                  </span>
                </div>
              ))}
              {view.column && (
                <div
                  className="cd-msheet-range"
                  style={{ left: view.column.left, width: view.column.width }}
                  aria-hidden="true"
                />
              )}
              {view.ticks.map((t) => (
                <div key={t.rating} className="cd-msheet-line" style={{ left: t.x }} aria-hidden="true" />
              ))}
              {view.cards.map((c) => (
                <SheetCardButton
                  key={c.id}
                  card={c}
                  width={view.cardW}
                  height={view.cardH}
                  theme={theme}
                  onPick={on.card}
                />
              ))}
            </div>
          )}
        </div>

        <div className="cd-msheet-foot">
          <div className="cd-msheet-say" aria-live="polite">
            {foot.kind === 'pick' ? (
              <>
                <span className="cd-msheet-pick-title">{foot.title}</span>
                <span className="cd-msheet-pick-line">{foot.line}</span>
              </>
            ) : (
              <span className="cd-msheet-hint">{foot.text}</span>
            )}
          </div>
          {foot.kind === 'pick' && (
            <button type="button" className="cd-msheet-guess" disabled={!foot.can} onClick={on.guess}>
              {foot.label}
            </button>
          )}
        </div>
      </div>
    </>
  );
}

/** One movie on the map. Today's is drawn by this same rule from what
 *  every card carries — its stand-in in its own title's hue, or a poster
 *  as every card has one — so nothing about it stands out. A card's
 *  poster loads once the reader has been near it; until then, and when
 *  there is none, the stand-in shows. */
function SheetCardButton({
  card,
  width,
  height,
  theme,
  onPick,
}: {
  card: SheetCard;
  width: number;
  height: number;
  theme: Theme;
  onPick: (id: string) => void;
}) {
  const fill = { ['--poster-fill' as string]: posterFallback(card.title, theme) };
  const posterH = height - 2 * SHEET_CARD_PAD;
  return (
    <button
      type="button"
      className={`cd-msheet-card${card.picked ? ' cd-msheet-card-picked' : ''}`}
      style={{ left: card.left, top: card.top, width, height, opacity: card.opacity }}
      aria-label={card.aria}
      aria-pressed={card.picked}
      onClick={() => onPick(card.id)}
    >
      {card.warm ? (
        <PosterImage
          id={card.id}
          url={card.poster}
          cssPx={SHEET_POSTER_W}
          className="cd-msheet-poster"
          width={SHEET_POSTER_W}
          height={posterH}
          style={fill}
        />
      ) : (
        <span className="cd-msheet-poster" style={fill} aria-hidden="true" />
      )}
      <span className="cd-msheet-card-body" aria-hidden="true">
        <span className="cd-msheet-card-title">{card.title}</span>
        <span className="cd-msheet-card-foot">
          {card.rating}
          {card.tried && <span className="cd-msheet-tried">✕</span>}
        </span>
      </span>
    </button>
  );
}
