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
import { rangesKey, toneStyle } from './daily';
import {
  MOVIES_LOADING,
  SHEET_CARD_PAD,
  SHEET_POSTER_W,
  moviesFailed,
  phoneSheetHeight,
  sheetLayout,
  sheetView,
  tabWrap,
  widenReach,
  type SheetCard,
  type SheetView,
} from './dailyMovies';
import { useLiveScreen } from './dailyScreen';
import { warmSpan, warmTop } from './grid';
import { PersonFace } from './PersonFace';
import { PosterImage } from './PosterImage';
import { posterFallback } from './poster';
import { useDrag, useEscape, useFocusTrapped } from './sheet';
import type { Theme } from './theme';

// The Movies sheet: one showing person's movies on a small Cinedikt map,
// with the reader's bought decade and rating band drawn on it and today's
// movie among the cards, unmarked. Titles show only inside those ranges,
// never for a genre or a length bought: the server sends every other
// movie as a blank tile, a year and a place on the rating axis, so there
// is nothing here to read off it and no names to combine. Its rules —
// the copy, the metrics — are daily.ts's (the Movies sheet section), put
// together for the sheet in dailyMovies.ts. This file asks the server for
// the movies, again once the decade or a rating range is bought, keeps
// what the reader has tapped, and draws it (MoviesSheetView, which takes
// everything as it stands, so it can be rendered without a DOM).
//
// A side panel held 12px in from the right, top and bottom from 640px up;
// below that a bottom sheet 90% of the visual viewport tall, with a
// grabber, closed by dragging its top edge down past 90px. Which of the
// two, and how tall, follow the window as it is now (dailyScreen.ts). The
// scrim, the close button and Escape close it on every size.

export interface MoviesSheetProps {
  /** The puzzle's number, for GET /daily/{no}/movies. */
  no: number;
  /** The person the sheet opens on, by IMDb name id. */
  person: string;
  /** The game as it stands: the showing slots, the facts bought, the
   *  wrong guesses, and the next wrong guess's price. */
  game: DailyGame;
  theme: 'light' | 'dark';
  /** Guess it: the page closes the sheet and guesses this movie, by IMDb
   *  id, as usual. */
  onGuess(id: string): void;
  /** The scrim, Escape, the close button, or a drag down on a phone. */
  onClose(): void;
  /** The server would not show this person's map ("sheet"): while the
   *  game is on it answers only for the one map the reader chose, and the
   *  page had that wrong. The page catches up and opens the right one, or
   *  closes the sheet; until then it says the movies are on their way.
   *  Without it, the refusal is said in the map's place, as any other. */
  onNotChosen?(): void;
}

/** The person's movies: on their way, here, or not to be had, with what
 *  to say. Kept with whose they are, so the movies of the person before
 *  are never drawn under another's name. */
type Load =
  | { state: 'wait' }
  | { state: 'ok'; person: string; movies: DailyMovie[] }
  | { state: 'failed'; text: string };

/** Everything in the sheet that takes the focus, in the order Tab visits
 *  it. A disabled button does not: a blank card is never a stop. */
const FOCUSABLE = 'button:not(:disabled), [href], input:not(:disabled), [tabindex]:not([tabindex="-1"])';

export function DailyMoviesSheet(props: MoviesSheetProps): JSX.Element {
  const { no, person, game, theme, onGuess, onClose, onNotChosen } = props;
  const { phone, viewH } = useLiveScreen();
  const ref = useRef<HTMLDivElement>(null);
  const mapRef = useRef<HTMLDivElement>(null);
  const [load, setLoad] = useState<Load>({ state: 'wait' });
  const [attempt, setAttempt] = useState(0);
  const [pick, setPick] = useState<string | null>(null);
  const [width, setWidth] = useState(0);
  const [reach, setReach] = useState<{ top: number; bottom: number } | null>(null);
  const drag = useDrag(phone, onClose);
  useEscape(onClose);
  useFocusTrapped(ref);

  // Asked once for the person, again on Try again, and again whenever the
  // decade or a rating range is bought (rangesKey), which is what turns
  // blank cards into titles. A second ask for the same person keeps the
  // map it has until the answer comes; a card picked that the answer
  // leaves blank is let go by sheetView, which only offers a readable
  // one. A refusal of the person, as not the map chosen, is the page's to
  // put right (onNotChosen), read when it comes rather than asked again
  // for.
  const key = rangesKey(game.facts);
  const notChosen = useRef(onNotChosen);
  notChosen.current = onNotChosen;
  useEffect(() => {
    const ctl = new AbortController();
    setLoad((was) => (was.state === 'ok' && was.person === person ? was : { state: 'wait' }));
    fetchDailyMovies(no, person, ctl.signal).then(
      (body) => setLoad({ state: 'ok', person, movies: body.movies ?? [] }),
      (err: unknown) => {
        if (ctl.signal.aborted) return;
        const reason = err instanceof ApiError ? err.reason : null;
        if (reason === 'sheet' && notChosen.current) {
          notChosen.current();
          return;
        }
        setLoad({ state: 'failed', text: moviesFailed(reason) });
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
  // The pointer at the named cards is shown once on a device, the first
  // time a map has any, and is gone from the first tap.
  const [coachDue] = useState(() => !coachSeen());
  const [tapped, setTapped] = useState(false);
  const view = sheetView({ game, person, movies, layout, pick, reach, theme, coach: coachDue && !tapped });
  const coached = view.coach != null;
  useEffect(() => {
    if (coached) markCoachSeen();
  }, [coached]);

  // It opens scrolled to the first readable movie, once, as soon as there
  // is a map to scroll. Instant whatever the motion setting: nothing has
  // been drawn anywhere else yet to travel from.
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
        card: (id) => {
          setPick(id);
          setTapped(true);
        },
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

/** Where the device keeps that the pointer at the named cards has been
 *  shown. */
const COACH_KEY = 'cinedikt.daily.coach';

function coachSeen(): boolean {
  try {
    return localStorage.getItem(COACH_KEY) != null;
  } catch {
    // With storage blocked it would be shown on every map, so it is not.
    return true;
  }
}

function markCoachSeen(): void {
  try {
    localStorage.setItem(COACH_KEY, '1');
  } catch {
    // Nothing to keep it in; coachSeen says so already.
  }
}

/** The band of the map around where the scroller stands, in the warm
 *  band's steps: a screen above it and two below, as the map warms. */
function bandAt(el: HTMLElement): { top: number; bottom: number } {
  return warmSpan(warmTop(el.scrollTop, 0), el.clientHeight || window.innerHeight);
}

export interface SheetHandlers {
  close(): void;
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
              {/* Said again when a range bought turns blank cards into
                  titles: how much of the map can be read. */}
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
                  <span className={`cd-msheet-year${r.inDecade ? ' cd-msheet-year-in' : ''}`} style={{ top: r.labelTop }}>
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
                  key={c.key}
                  card={c}
                  width={view.cardW}
                  height={view.cardH}
                  theme={theme}
                  onPick={on.card}
                />
              ))}
              {/* Drawn only: the footer says the same to a screen reader,
                  and a press goes through it to the cards. */}
              {view.coach && (
                <div
                  className="cd-msheet-coach"
                  style={{
                    left: view.coach.left,
                    top: view.coach.top,
                    width: view.coach.width,
                    ['--caret' as string]: `${view.coach.caret}px`,
                  }}
                  aria-hidden="true"
                >
                  {view.coach.text}
                </div>
              )}
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

/** One movie on the map. A readable one is its poster, its title and its
 *  rating, and can be tapped; today's is drawn by this same rule from
 *  what every card carries — its stand-in in its own title's hue, or a
 *  poster as every readable card has one — so nothing about it stands
 *  out. A card's poster loads once the reader has been near it; until
 *  then, and when there is none, the stand-in shows. A blank one is the
 *  same tile with an empty poster and nothing written on it, faded, and
 *  cannot be pressed: there is nothing to guess. */
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
  const place = { left: card.left, top: card.top, width, height, opacity: card.opacity };
  if (card.kind === 'blank') {
    return (
      <button type="button" className="cd-msheet-card cd-msheet-card-blank" style={place} aria-label={card.aria} disabled>
        <span className="cd-msheet-poster" aria-hidden="true" />
      </button>
    );
  }
  const fill = { ['--poster-fill' as string]: posterFallback(card.title, theme) };
  const posterH = height - 2 * SHEET_CARD_PAD;
  return (
    <button
      type="button"
      className={`cd-msheet-card${card.picked ? ' cd-msheet-card-picked' : ''}`}
      style={place}
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
