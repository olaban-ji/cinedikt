import {
  memo,
  useCallback,
  useEffect,
  useLayoutEffect,
  useRef,
  useState,
  type CSSProperties,
  type RefObject,
} from 'react';
import type { DailyCard, DailyFilm, DailyGame, DailyPerson, DailyToday } from './api';
import {
  ANSWER_POP_KEYFRAMES,
  ANSWER_POP_MS,
  BOUNCE_EASE,
  MAX_DOTS,
  OUT_CARD_OPACITY,
  OUT_YEAR_OPACITY,
  SCORE_RISE_DELAY_MS,
  SCORE_RISE_KEYFRAMES,
  SCORE_RISE_MS,
  answerLabel,
  boundLines,
  boundsOf,
  cardLabel,
  flipCost,
  fmtN,
  inBounds,
  plotSize,
  rowBoundsOf,
  seamsFor,
  yearRuledOut,
  type Bounds,
  type CardFace,
} from './daily';
import {
  MAX_MARKS,
  bandClass,
  footRoom,
  inWarmSpan,
  markersFor,
  railYearClass,
  ratingText,
  searchedTagAt,
  textWidth,
  warmSpan,
  warmTop,
  type GridLayout,
  type Placed,
  type Row,
} from './grid';
import { animate } from './motion';
import { personVars } from './personColour';
import { PosterImage } from './PosterImage';
import { posterFallback } from './poster';
import { ENTER_MS } from './sheet';
import type { Theme } from './theme';
import { useReducedMotion } from './theme';

/** The scroller's width, and how far down it is and how tall, which is
 *  what the layout and the warm band of cards are worked out from.
 *
 *  As the map does it (GridMap's readView): the width is known from the
 *  window before the scroller is measured, so the first paint is already
 *  laid out; a first measurement of nothing is real on a page opened in
 *  a background tab, and a timer looks again, since a frame may never
 *  come. Scrolling is a flood and is read once a frame. The warm band
 *  moves in the map's own steps (grid.ts's warmTop); the board starts at
 *  the top of its scroller, so nothing lies over it. */
export function useBoardView(
  scroller: RefObject<HTMLDivElement | null>,
  onScroll?: () => void,
): { width: number; scrollTop: number; height: number } {
  const [view, setView] = useState(() => ({
    width: typeof window === 'undefined' ? 1280 : window.innerWidth,
    scrollTop: 0,
    height: 0,
  }));
  const told = useRef(onScroll);
  told.current = onScroll;
  const read = useCallback(() => {
    const el = scroller.current;
    if (!el) return;
    const width = el.clientWidth || window.innerWidth;
    const height = el.clientHeight || window.innerHeight;
    const scrollTop = warmTop(el.scrollTop, 0);
    setView((was) =>
      was.width === width && was.height === height && was.scrollTop === scrollTop
        ? was
        : { width, scrollTop, height },
    );
  }, [scroller]);
  useLayoutEffect(() => {
    const el = scroller.current;
    if (!el) return;
    let frame = 0;
    const scrolled = () => {
      told.current?.();
      if (frame) return;
      frame = requestAnimationFrame(() => {
        frame = 0;
        read();
      });
    };
    read();
    const retry = window.setTimeout(read, 0);
    const ro = typeof ResizeObserver === 'function' ? new ResizeObserver(read) : null;
    ro?.observe(el);
    el.addEventListener('scroll', scrolled, { passive: true });
    window.addEventListener('resize', read);
    return () => {
      if (frame) cancelAnimationFrame(frame);
      window.clearTimeout(retry);
      ro?.disconnect();
      el.removeEventListener('scroll', scrolled);
      window.removeEventListener('resize', read);
    };
  }, [read, scroller]);
  return view;
}

/** What turning over and ringing does to the board right now: cards just
 *  turned keep a ring, and each waits its turn to turn. */
export interface BoardFx {
  fresh: ReadonlySet<string>;
  delays: Readonly<Record<string, number>>;
}

export const NO_FX: BoardFx = { fresh: new Set(), delays: {} };

interface BoardProps {
  today: DailyToday;
  game: DailyGame | null;
  layout: GridLayout;
  /** Every face-up card and what it shows (facesOf). */
  faces: ReadonlyMap<string, CardFace>;
  /** The known people on each card, in slot order (peopleByCard). */
  people: ReadonlyMap<string, DailyPerson[]>;
  hues: ReadonlyMap<string, number>;
  codes: ReadonlyMap<string, string>;
  phone: boolean;
  /** The scroller's place, for the band of cards to mount. */
  scrollTop: number;
  viewH: number;
  fx: BoardFx;
  /** A card a feed chip has just taken the reader to. */
  pulse: string | null;
  /** The game has only just ended, so the answer pops into its place. */
  popAnswer: boolean;
  /** A solve's score, risen over the answer for a moment. */
  popPts: number | null;
  answerRef: RefObject<HTMLDivElement | null>;
  theme: Theme;
  /** A card pressed. `pointer` is a click or a tap, which the page holds
   *  to its tap guard; a key press is always meant. */
  onFlip: (card: string, pointer: boolean) => void;
}

/** The board: today's map, year down and rating across, with the answer
 *  taken off it. It is drawn from the map's own layout and with the map's
 *  own bands, gridlines and year rail, so a reader who knows Cinedikt
 *  knows how to read it; only the cards are the Daily's own. */
export function DailyBoard({
  today,
  game,
  layout,
  faces,
  people,
  hues,
  codes,
  phone,
  scrollTop,
  viewH,
  fx,
  pulse,
  popAnswer,
  popPts,
  answerRef,
  theme,
  onFlip,
}: BoardProps) {
  const reduced = useReducedMotion();
  const plot = plotSize(layout, phone);
  const m = layout.metrics;
  // Two sets of bounds. The cards fade only for what wrong guesses have
  // ruled out; the rows, their labels and the dashed lines go by the
  // guesses and the year, once it is bought, together.
  const bounds = boundsOf(game);
  const rowBounds = rowBoundsOf(game);
  const lines = boundLines(rowBounds, layout);
  const playing = game?.phase === 'play';
  const pts = game?.pts ?? 0;
  const seams = useSeams(layout.rows, reduced);

  const byId = cardsById(today);
  // Unmeasured, the window's height stands in, as it does for the map.
  const h = viewH > 0 ? viewH : typeof window === 'undefined' ? 800 : window.innerHeight;
  const span = warmSpan(scrollTop, h);
  const screen = { top: scrollTop, bottom: scrollTop + h };
  const answer = game?.end?.answer ?? null;
  const placedAnswer = answer ? layout.anchor : null;

  return (
    <div
      className="cd-plot-wrap"
      style={{ width: plot.w, ['--rail-w' as string]: `${m.railW}px` }}
    >
      <div className="cd-axis" aria-hidden="true">
        <span className="cd-axis-label cd-axis-title" style={{ left: layout.axisTitleLeft }}>
          IMDb rating →
        </span>
        {layout.lines.map((l) => (
          <span key={l.rating} className="cd-axis-label" style={{ left: l.labelLeft }}>
            {l.label}
          </span>
        ))}
        {/* The rating bounds' pills sit in the strip, so they stay in
            view however far down the reader has gone. */}
        {lines.ratings.map((l) => (
          <span
            key={l.before ? 'ceiling' : 'floor'}
            className={`cd-daily-bound-pill cd-daily-bound-pill-axis${l.before ? ' cd-daily-bound-pill-before' : ''}`}
            style={{ left: l.labelLeft }}
          >
            {l.label}
          </span>
        ))}
      </div>
      <div className="cd-plot cd-daily-plot" style={{ height: plot.h }}>
        {layout.rows.map((r) => (
          <Band key={r.year} row={r} seam={seams.get(r.year)} bounds={rowBounds} />
        ))}
        {layout.lines.map((l) => (
          <div key={l.rating} className="cd-gridline" style={{ left: l.x }} />
        ))}
        {layout.cards.map((c) => {
          if (c.film.isAnchor || !inWarmSpan(c.top, m.cardH, span)) return null;
          const card = byId.get(c.film.id);
          if (!card) return null;
          const face = faces.get(card.id);
          const on = !!face;
          return (
            <BoardCard
              key={card.id}
              placed={c}
              card={card}
              face={face}
              who={people.get(card.id)}
              layout={layout}
              can={playing && !on}
              afford={pts >= flipCost(card.rating)}
              hot={on && (fx.fresh.has(card.id) || pulse === card.id)}
              delay={fx.delays[card.id] ?? 0}
              faded={!inBounds(card, bounds)}
              eager={inWarmSpan(c.top, m.cardH, screen)}
              hues={hues}
              codes={codes}
              theme={theme}
              onFlip={onFlip}
            />
          );
        })}
        {/* Keyed by which bound each is, so a line moved by the next guess
            slides to its new place rather than being drawn afresh. */}
        {lines.years.map((l) => (
          <div key={l.kind} className="cd-daily-bound-across" style={{ top: l.y }} aria-hidden="true" />
        ))}
        {lines.ratings.map((l) => (
          <div
            key={l.before ? 'ceiling' : 'floor'}
            className="cd-daily-bound-down"
            style={{ left: l.x }}
            aria-hidden="true"
          />
        ))}
        {answer && placedAnswer && (
          <AnswerCard
            answer={answer}
            placed={placedAnswer}
            layout={layout}
            pop={popAnswer}
            pts={popPts}
            theme={theme}
            answerRef={answerRef}
          />
        )}
        <div className="cd-rail-layer" style={{ height: plot.h, width: plot.w }}>
          <div className="cd-rail" style={{ width: m.railW, height: plot.h }}>
            {layout.rows.map((r) => (
              <YearLabel key={r.year} row={r} seam={seams.get(r.year)} bounds={rowBounds} />
            ))}
            {/* A year bound's pill rides with the rail rather than the
                plot: the map is often panned sideways (the starting
                cards are rarely at the low end), and a pill left at the
                plot's own left edge would be off the screen with it. A
                pinned year is named once: its bottom line draws no pill
                when its top line has one. */}
            {lines.years.map((l) =>
              l.label ? (
                <span
                  key={l.kind}
                  className="cd-daily-bound-pill cd-daily-bound-pill-year"
                  style={{ left: l.labelLeft, top: l.labelTop }}
                  aria-hidden="true"
                >
                  {l.label}
                </span>
              ) : null,
            )}
          </div>
        </div>
      </div>
    </div>
  );
}

/** Today's cards by id. Kept per puzzle, since the cards never change. */
const cardIndex = new WeakMap<DailyToday, Map<string, DailyCard>>();
function cardsById(today: DailyToday): Map<string, DailyCard> {
  let found = cardIndex.get(today);
  if (!found) {
    found = new Map(today.cards.map((c) => [c.id, c]));
    cardIndex.set(today, found);
  }
  return found;
}

/** The years that have just opened, held at the seam they open out of
 *  for a moment so their row can grow from it (see seamsFor). Settled
 *  while rendering, so the seam is what is first drawn; let go a beat
 *  later, as every layer is (ENTER_MS), on a timer rather than a frame. */
function useSeams(rows: Row[], reduced: boolean): Map<number, number> {
  const [seen, setSeen] = useState<{ rows: Row[]; seams: Map<number, number> }>(() => ({
    rows,
    seams: new Map(),
  }));
  if (seen.rows !== rows) {
    setSeen({ rows, seams: reduced ? new Map() : seamsFor(seen.rows, rows) });
  }
  useEffect(() => {
    if (seen.seams.size === 0) return;
    const t = window.setTimeout(
      () => setSeen((was) => (was.seams.size === 0 ? was : { ...was, seams: new Map() })),
      ENTER_MS,
    );
    return () => window.clearTimeout(t);
  }, [seen.seams]);
  return seen.rows === rows ? seen.seams : new Map();
}

/** A year's band across the board. A year the guesses, or the year
 *  bought, have ruled out fades; the answer's own year, once it is
 *  bought and at the end, is lit (the layout's anchorYear). A year that
 *  has just opened grows out of the seam it opens from. */
function Band({ row: r, seam, bounds }: { row: Row; seam: number | undefined; bounds: Bounds | null }) {
  const opening = seam != null;
  return (
    <div
      className={bandClass(r)}
      style={{
        top: opening ? seam : r.top,
        height: opening ? 0 : r.height,
        opacity: opening ? 0 : yearRuledOut(r.year, bounds) ? OUT_YEAR_OPACITY : undefined,
      }}
      data-band={r.year}
    />
  );
}

/** A year's label on the rail, pinned inside its row's own stretch of it
 *  as on the map. */
function YearLabel({ row: r, seam, bounds }: { row: Row; seam: number | undefined; bounds: Bounds | null }) {
  const opening = seam != null;
  return (
    <div
      className="cd-rail-slot"
      style={{
        top: opening ? seam : r.top,
        height: opening ? 0 : r.height,
        opacity: opening ? 0 : yearRuledOut(r.year, bounds) ? OUT_YEAR_OPACITY : undefined,
      }}
      data-rail={r.year}
    >
      <span
        className={railYearClass(r)}
      >
        {r.year}
      </span>
    </div>
  );
}

/** A face-up close relative's marks. It says how many people it shares
 *  where a rating would be, which is wider, so the marks are measured
 *  against that instead; it never carries initials. */
function kinMarks(
  ids: string[],
  layout: GridLayout,
  shared: number,
): { show: string[]; extra: number } {
  const room = footRoom(layout.metrics) - 0.5;
  const base = textWidth(`${shared} in common`, 12.5) + 5;
  const n = ids.length;
  for (let fit = n <= MAX_MARKS ? n : MAX_MARKS - 1; fit >= 1; fit--) {
    const extra = n - fit;
    const w = base + fit * 12 + (extra > 0 ? 5 + textWidth(`+${extra}`, 10.5) : 0);
    if (w <= room) return { show: ids.slice(0, fit), extra };
  }
  return { show: [], extra: 0 };
}

interface CardProps {
  placed: Placed;
  card: DailyCard;
  face: CardFace | undefined;
  /** The known people on this card. */
  who: DailyPerson[] | undefined;
  layout: GridLayout;
  /** Blank, with the game on: it can be turned over. */
  can: boolean;
  afford: boolean;
  /** Ringed in the accent: just turned over, or just taken to. */
  hot: boolean;
  /** How long it waits to turn, when cards turn together. */
  delay: number;
  /** Ruled out by the guesses so far. */
  faded: boolean;
  /** On the glass, so its poster loads ahead of the rest. */
  eager: boolean;
  hues: ReadonlyMap<string, number>;
  codes: ReadonlyMap<string, string>;
  theme: Theme;
  onFlip: (card: string, pointer: boolean) => void;
}

/** A card on the board. Blank, it is a ghost of a card at its year and
 *  rating, with a dot for each known person on it and, under a pointer
 *  or on a touch screen, what turning it over costs. Turned over, in 3D,
 *  it is the map's card: poster, title, rating and marks. A close
 *  relative turned over shows how near it is instead of what it is. */
export const BoardCard = memo(function BoardCard({
  placed,
  card,
  face,
  who,
  layout,
  can,
  afford,
  hot,
  delay,
  faded,
  eager,
  hues,
  codes,
  theme,
  onFlip,
}: CardProps) {
  const m = layout.metrics;
  const on = !!face;
  const people = who ?? [];
  const style: CSSProperties = {
    left: placed.left,
    top: placed.top,
    width: m.cardW,
    height: m.cardH,
    opacity: faded ? OUT_CARD_OPACITY : undefined,
    ['--poster-w' as string]: `${m.posterW}px`,
  };
  return (
    <div
      className={`cd-daily-card${on ? ' cd-daily-card-on' : ''}${can ? ' cd-daily-card-can' : ''}${can && !afford ? ' cd-daily-card-poor' : ''}${hot ? ' cd-daily-card-hot' : ''}`}
      data-card={card.id}
      role={can ? 'button' : 'img'}
      tabIndex={can ? 0 : -1}
      aria-label={cardLabel(card, face)}
      style={style}
      onClick={can ? () => onFlip(card.id, true) : undefined}
      onKeyDown={
        can
          ? (e) => {
              if (e.key !== 'Enter' && e.key !== ' ') return;
              e.preventDefault();
              onFlip(card.id, false);
            }
          : undefined
      }
    >
      <div
        className="cd-daily-flip"
        data-flip={on ? 180 : 0}
        style={delay > 0 ? { transitionDelay: `${delay}ms` } : undefined}
      >
        {/* For the eye only: the card's label is what is read out. */}
        <div className="cd-daily-front" aria-hidden="true">
          <span className="cd-daily-slot" />
          {!on && people.length > 0 && (
            <span className="cd-daily-dots">
              {people.slice(0, MAX_DOTS).map((p) => (
                <span key={p.id} className="cd-daily-dot" style={personVars(p, theme, hues)} />
              ))}
            </span>
          )}
          {can && <span className="cd-daily-cost">{flipCost(card.rating)}</span>}
        </div>
        <div className="cd-daily-back" aria-hidden="true">
          {face && <CardFront card={card} face={face} people={people} layout={layout} eager={eager} hues={hues} codes={codes} theme={theme} />}
        </div>
      </div>
    </div>
  );
});

/** What a face-up card shows: the map card's own parts. */
function CardFront({
  card,
  face,
  people,
  layout,
  eager,
  hues,
  codes,
  theme,
}: {
  card: DailyCard;
  face: CardFace;
  people: DailyPerson[];
  layout: GridLayout;
  eager: boolean;
  hues: ReadonlyMap<string, number>;
  codes: ReadonlyMap<string, string>;
  theme: Theme;
}) {
  const m = layout.metrics;
  const ids = people.map((p) => p.id);
  const byId = new Map(people.map((p) => [p.id, p]));
  const kin = face.kind === 'relative';
  const markers = kin
    ? { ...kinMarks(ids, layout, face.shared), initials: false }
    : markersFor(ids, m, card.rating, codes as Map<string, string>);
  return (
    <>
      {kin ? (
        <span className="cd-card-poster cd-daily-kin" />
      ) : (
        <PosterImage
          id={face.film.id}
          url={face.film.poster}
          cssPx={m.posterW}
          className="cd-card-poster"
          width={m.posterW}
          height={m.posterH}
          eager={eager}
          style={{ ['--poster-fill' as string]: posterFallback(face.film.title, theme) }}
        />
      )}
      <span className="cd-card-body">
        <span className="cd-card-title">{kin ? 'A close relative' : face.film.title}</span>
        <span className="cd-card-foot">
          <span className="cd-card-rating">{kin ? `${face.shared} in common` : ratingText(card.rating)}</span>
          <span className="cd-card-spacer" />
          {markers.show.map((id) => {
            const p = byId.get(id);
            return (
              <span key={id} className="cd-card-mark" style={p ? personVars(p, theme, hues) : undefined}>
                <span className="cd-card-swatch" />
                {markers.initials && (codes.get(id) ?? '?')}
              </span>
            );
          })}
          {markers.extra > 0 && <span className="cd-more">+{markers.extra}</span>}
        </span>
      </span>
    </>
  );
}

/** The answer, once the game is over: in its own place on the map,
 *  ringed and tagged as a searched movie is. It pops in as the game
 *  ends, and a solve's score rises over it. */
function AnswerCard({
  answer,
  placed,
  layout,
  pop,
  pts,
  theme,
  answerRef,
}: {
  answer: DailyFilm;
  placed: Placed;
  layout: GridLayout;
  pop: boolean;
  pts: number | null;
  theme: Theme;
  answerRef: RefObject<HTMLDivElement | null>;
}) {
  const m = layout.metrics;
  const score = useRef<HTMLSpanElement>(null);
  // Read once, as it mounts: a card that has been there all along does
  // not pop in when something else changes.
  const popped = useRef(pop);
  useLayoutEffect(() => {
    if (!popped.current) return;
    animate(answerRef.current, ANSWER_POP_KEYFRAMES, { duration: ANSWER_POP_MS, easing: BOUNCE_EASE });
  }, [answerRef]);
  const showing = pts != null;
  useLayoutEffect(() => {
    if (!showing) return;
    animate(score.current, SCORE_RISE_KEYFRAMES, {
      duration: SCORE_RISE_MS,
      delay: SCORE_RISE_DELAY_MS,
      easing: 'ease-out',
      fill: 'both',
    });
  }, [showing]);
  const tag = searchedTagAt(placed);
  return (
    <>
      <div
        ref={answerRef}
        className="cd-daily-answer"
        data-answer="1"
        role="img"
        aria-label={answerLabel(answer)}
        style={{
          left: placed.left,
          top: placed.top,
          width: m.cardW,
          height: m.cardH,
          ['--poster-w' as string]: `${m.posterW}px`,
        }}
      >
        <PosterImage
          id={answer.id}
          url={answer.poster}
          cssPx={m.posterW}
          className="cd-card-poster"
          width={m.posterW}
          height={m.posterH}
          eager
          style={{ ['--poster-fill' as string]: posterFallback(answer.title, theme) }}
        />
        <span className="cd-card-body" aria-hidden="true">
          <span className="cd-card-title">{answer.title}</span>
          <span className="cd-card-foot">
            <span className="cd-card-rating">{ratingText(answer.rating)}</span>
          </span>
        </span>
      </div>
      {/* Beside the card rather than in it, for the eye only: the card's
          label already says what it is. */}
      <span className="cd-searched-tag" style={{ left: tag.left, top: tag.top }} aria-hidden="true">
        Today’s movie
      </span>
      {showing && (
        <span
          ref={score}
          className="cd-daily-score"
          style={{ left: placed.left + m.cardW / 2, top: placed.top - 46 }}
          aria-hidden="true"
        >
          {fmtN(pts)} points
        </span>
      )}
    </>
  );
}
