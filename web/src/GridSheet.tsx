import {
  useEffect,
  useLayoutEffect,
  useMemo,
  useRef,
  useState,
  type CSSProperties,
  type MutableRefObject,
  type Ref,
} from 'react';
import { genreLine, initialsFor, type GridFilm, type GridPayload, type GridPerson } from './grid';
import { stillNow } from './motion';
import { personColour } from './personColour';
import { PosterImage } from './PosterImage';
import { hueOf, posterFallback, sheetPosterPx } from './poster';
import { useScreen } from './screen';
import { SHEET_EXIT_MS, useCloser, useDrag, useEscape, useFocusTrapped, useGlide } from './sheet';
import {
  LESS_SETTLE_MS,
  SYN_LH,
  SYN_MOST,
  SYN_TR,
  overflows,
  restingLines,
  roomForTrailer,
  scrollEase,
  type SynChange,
} from './synopsis';
import { useResolvedTheme } from './theme';
import { TrailerRow, useTrailer, type Player } from './TrailerRow';
import { TRAILER_OPEN_MS, escapeCloses, isFor, videoHeight, type Play } from './trailer';

interface Props {
  film: GridFilm;
  payload: GridPayload;
  onOnly: (personId: string) => void;
  onRemap: (film: GridFilm) => void;
  onClose: () => void;
  /** Given the sheet's own way out while it is up, so opening a map can
   *  close it on its exit first. */
  closer?: MutableRefObject<((then?: () => void) => void) | null>;
  /** The page's one trailer player, which the panel's trailer plays in. */
  player: Player;
}

/** Everything a card cannot hold: the full title, what the film is
 *  about and its trailer, how the film sits against the searched one,
 *  and who put it on the grid.
 *
 *  A panel down the right on a desktop, a tablet or a landscape phone,
 *  and a sheet from the bottom on a phone, each held in from the edges.
 *  It arrives and leaves under its own power. Whatever it was asked to
 *  do — narrow the map, map another film — waits until it is gone, so
 *  nothing ever changes underneath a sheet that is still on the way out. */
export function GridSheet({ film, payload, onOnly, onRemap, onClose, closer, player }: Props) {
  const ref = useRef<HTMLDivElement>(null);
  const screen = useScreen();
  const { phone } = screen;
  const { phase, leave } = useGlide(onClose, SHEET_EXIT_MS);
  const drag = useDrag(phone, leave);
  useEscape(() => sheetEscape(player, film.id, leave));
  useCloser(closer, leave);
  useFocusTrapped(ref);
  const trailer = useTrailer(film.id);
  // The trailer closes as the panel starts to go, however it was sent
  // away, so its sound stops at once.
  const { stop: stopTrailer, drop: dropTrailer } = player;
  useEffect(() => {
    if (phase === 'out') stopTrailer('panel');
  }, [phase, stopTrailer]);
  // Once the panel has gone, its player goes at once too, closing or
  // not: there is no close left to play, and the same film's panel
  // opened straight after must not find a player it never started. A
  // panel taken away with no exit, the route moving underneath it, lands
  // here as well.
  useEffect(() => () => dropTrailer('panel'), [dropTrailer]);
  const play = isFor(player.play, 'panel', film.id) ? player.play : null;
  const playing = play != null;
  const synopsis = film.synopsis?.trim();
  const genres = genreLine(film);

  const body = useRef<HTMLDivElement>(null);
  const synText = useRef<HTMLParagraphElement>(null);
  const synTimer = useRef(0);
  // Where the body scrolls to as the player opens, when folding the
  // synopsis could not make all the room it needs.
  const liftTo = useRef<number | null>(null);
  const [synKept, setSyn] = useState(() => restingSyn(film.id));
  const syn = synFor(synKept, film.id);
  const synView = synopsisView(syn, play, stillNow());

  // Measured once the panel is laid out and before it is painted, so it
  // is never seen at any other length; again for another film, when the
  // screen class gives the panel another size, and when the genre line
  // changes, which can take the head to another line and move the
  // synopsis down with it.
  useLayoutEffect(() => {
    const el = body.current;
    const p = synText.current;
    if (!el || !p) return;
    const m = measureSynopsis(el, p);
    setSyn((was) => measuredSyn(was, film.id, m));
  }, [film.id, synopsis, genres, screen.cls]);

  useEffect(() => () => window.clearTimeout(synTimer.current), []);

  // A trailer that folded the synopsis gives it back at its resting
  // lines, collapsed, however its play ends: as its close starts, or at
  // once when it goes with no close to play, as it does for a reader who
  // has asked for nothing to move. Before paint, so it is never drawn
  // open for a frame.
  const folds = holdsFold(play);
  const folded = useRef(false);
  useLayoutEffect(() => {
    if (folded.current && !folds) {
      window.clearTimeout(synTimer.current);
      setSyn((was) => collapsedSyn(synFor(was, film.id)));
    }
    folded.current = folds;
  }, [folds, film.id]);

  // More is disabled as the synopsis folds away, and a browser drops the
  // focus from a disabled control onto the page itself, where a screen
  // reader loses its place. So a reader on it is moved to the panel, in
  // the same commit, before the browser can take it away.
  const synGone = synView.gone;
  useLayoutEffect(() => {
    if (!synGone || !document.activeElement?.closest('.cd-sheet-syn-more')) return;
    ref.current?.focus({ preventScroll: true });
  }, [synGone]);

  // The body scrolls, when it has to, in step with the player opening.
  // A close before it is done stops it where it is.
  const opened = play?.open === true;
  useEffect(() => {
    const el = body.current;
    const to = liftTo.current;
    if (!opened || !el || to == null) return;
    liftTo.current = null;
    return glideScroll(el, to, TRAILER_OPEN_MS);
  }, [opened]);

  const toggleSyn = () => {
    // A trailer holding the synopsis folded keeps it so (see holdsFold).
    if (folds) return;
    window.clearTimeout(synTimer.current);
    const still = stillNow();
    const expand = !synView.expanded;
    // Measured again as it opens, in case the text has been laid out
    // anew since (a web font arriving, say), but only from rest: taking
    // its max-height off mid-move would cancel the transition running,
    // and its clamp is on only when nothing is moving. Folding back
    // needs only its resting height.
    const full =
      expand && synText.current && synView.clamp !== 'unset' ? fullHeight(synText.current) : syn.full;
    setSyn((was) => toggledSyn(synFor(was, film.id), expand, full, still));
    if (!expand && !still) {
      synTimer.current = window.setTimeout(() => setSyn((was) => ({ ...was, easing: false })), LESS_SETTLE_MS);
    }
  };

  // Worked out as Watch trailer is pressed or rested on, before the
  // player mounts, from the panel as it stands.
  const startTrailer = (muted: boolean, W: number) => {
    // A rest on the button that comes due as the panel goes starts
    // nothing.
    if (phase === 'out') return;
    const was = player.now();
    let synLines: number | null;
    if (isFor(was, 'panel', film.id)) {
      if (!was.closing) return;
      // Taken back while it closes: the room worked out when it first
      // opened stands, and the body has already scrolled.
      synLines = was.synLines;
      liftTo.current = null;
    } else {
      const room = roomInPanel(body.current, synText.current, videoHeight(W), syn);
      synLines = room.synLines;
      liftTo.current = room.to;
    }
    if (synLines != null) {
      window.clearTimeout(synTimer.current);
      setSyn((was) => foldingSyn(synFor(was, film.id)));
    }
    player.start('panel', film.id, muted, W, synLines);
  };
  const people = payload.people.filter((p) => film.people.includes(p.id));
  // Worked out over the whole map rather than this film's few, so a
  // person has the same initials here as on every card.
  const codes = useMemo(() => initialsFor(payload.people), [payload.people]);
  const theme = useResolvedTheme();
  const held = drag.held && drag.y > 0;
  const posterPx = sheetPosterPx(screen);
  const remap = () => leave(() => onRemap(film));
  // The film's own hue, which the wash at the top is drawn in. A number
  // here; the stylesheet makes the colour, one for each theme.
  const style: CSSProperties = { ['--h' as string]: hueOf(film.title) };
  if (held) Object.assign(style, { transform: `translateY(${drag.y}px)`, transition: 'none' });

  return (
    <>
      <div
        className={`cd-scrim${phase === 'in' ? ' cd-scrim-in' : ''}`}
        onClick={() => leave()}
        aria-hidden="true"
      />
      <div
        // The class sets where it sits, how far in from the edges and
        // how it travels. From the screen class in script, the same one
        // the poster's size is read from, rather than a media query of
        // its own that could disagree with it at a boundary.
        className={`cd-sheet cd-sheet-${screen.cls} cd-sheet-${phase}`}
        style={style}
        role="dialog"
        aria-modal="true"
        aria-label={film.title}
        tabIndex={-1}
        ref={ref}
      >
        <div className="cd-sheet-wash">
          {phone && (
            <span
              className="cd-sheet-grip"
              aria-hidden="true"
              onPointerDown={drag.onPointerDown}
              onPointerMove={drag.onPointerMove}
              onPointerUp={drag.onPointerUp}
              onPointerCancel={drag.onPointerUp}
            />
          )}
          <button type="button" className="cd-sheet-close" aria-label="Close" onClick={() => leave()}>
            ×
          </button>
        </div>
        <div className={`cd-sheet-body${playing ? ' cd-sheet-playing' : ''}`} ref={body}>
          <div className="cd-sheet-head">
            <PosterImage
              id={film.id}
              url={film.poster}
              cssPx={posterPx}
              className="cd-sheet-poster"
              width={posterPx}
              height={Math.round(posterPx * 1.5)}
              style={{ background: posterFallback(film.title, theme) }}
            />
            <div className="cd-sheet-head-text">
              {film.isAnchor && <span className="cd-sheet-eyebrow">Searched movie</span>}
              <h2 className="cd-sheet-title">{film.title}</h2>
              <div className="cd-sheet-meta">
                <span>{film.year}</span>
                <span className="cd-sheet-pill">
                  {film.rating == null ? 'No rating' : film.rating.toFixed(1)}
                </span>
                {genres && <span className="cd-sheet-genres">{genres}</span>}
              </div>
            </div>
          </div>

          {/* What the film is about, short enough to leave its trailer
              in view, and the trailer, which plays here rather than on
              another tab. */}
          <div className="cd-sheet-about">
            {synopsis && (
              <SheetSynopsis text={synopsis} view={synView} onToggle={toggleSyn} textRef={synText} />
            )}
            <TrailerRow
              where="panel"
              id={film.id}
              title={film.title}
              trailer={trailer}
              player={player}
              onPlay={startTrailer}
            />
          </div>

          <VersusBlock film={film} anchor={payload.anchor} />

          {/* On a phone the button is pinned to the foot instead: a
              thumb cannot reach the middle of a tall sheet, and a
              scroll that ends in a tap must not land on it. */}
          {!film.isAnchor && !phone && <RemapButton film={film} onRemap={remap} />}

          {people.length > 0 && (
            <div className="cd-sheet-people">
              <div className="cd-sheet-heading">{headingFor(film, payload.anchor.title)}</div>
              {people.map((p) => (
                <button
                  key={p.id}
                  type="button"
                  className="cd-sheet-person"
                  style={{ ['--tone' as string]: personColour(p.id, theme) }}
                  aria-label={`Show only ${p.name}'s movies`}
                  onClick={() => leave(() => onOnly(p.id))}
                >
                  {/* Round whatever the role: the line beside it already
                      says who directed, so only the swatches, which have
                      no words next to them, need the shape to say it. */}
                  <span className="cd-sheet-initials" aria-hidden="true">
                    {codes.get(p.id) ?? '?'}
                  </span>
                  <span className="cd-sheet-person-text">
                    <span className="cd-sheet-name">{p.name}</span>
                    <span className="cd-sheet-role">{roleLine(p, payload.anchor.title)}</span>
                  </span>
                  <span className="cd-sheet-only" aria-hidden="true">
                    Show only
                  </span>
                </button>
              ))}
            </div>
          )}
        </div>

        {!film.isAnchor && phone && (
          <div className="cd-sheet-foot">
            <RemapButton film={film} onRemap={remap} />
          </div>
        )}
      </div>
    </>
  );
}

/** Escape in the panel: the trailer first, while one is open in it, and
 *  the panel itself with the next press. */
export function sheetEscape(player: Pick<Player, 'now' | 'stop'>, id: string, leave: () => void): void {
  if (escapeCloses(player.now(), 'panel', id) === 'trailer') player.stop('panel');
  else leave();
}

/** The panel's synopsis, as the panel keeps it beside the player's own
 *  state (whose synLines and synSettled say how a trailer has folded
 *  it). */
export interface SynState {
  /** The film it was measured for. */
  id: string;
  /** The lines it rests at. */
  lines: number;
  /** Whether it offers More: its whole text runs past those lines. */
  over: boolean;
  /** Its whole text's height, which More opens it to. */
  full: number;
  /** Whether More has opened it. */
  expanded: boolean;
  /** Set while Less folds it back. Its line clamp stays off until then. */
  easing: boolean;
  /** What last changed it, which its transition is timed for. */
  change: SynChange;
}

/** A synopsis not yet measured: at its most lines, collapsed. */
export function restingSyn(id: string): SynState {
  return { id, lines: SYN_MOST, over: false, full: 0, expanded: false, easing: false, change: 'still' };
}

/** The synopsis kept for this film, or a fresh one when what is kept is
 *  another film's: the panel opening another film starts collapsed. */
export function synFor(s: SynState, id: string): SynState {
  return s.id === id ? s : restingSyn(id);
}

/** What measuring the panel finds. */
export interface SynMeasure {
  lines: number;
  over: boolean;
  full: number;
}

/** A measurement taken in. The same film keeps More as it was; another
 *  starts collapsed. Nothing moves for a measurement. */
export function measuredSyn(was: SynState, id: string, m: SynMeasure): SynState {
  const s = synFor(was, id);
  if (s.lines === m.lines && s.over === m.over && s.full === m.full && s.change === 'still') return s;
  return { ...s, ...m, change: 'still' };
}

/** More or Less pressed. Less keeps the clamp off while it folds back,
 *  unless nothing is to move. */
export function toggledSyn(s: SynState, expand: boolean, full: number, still: boolean): SynState {
  return { ...s, expanded: expand, easing: !expand && !still, full, change: expand ? 'more' : 'less' };
}

/** A trailer opening that needs the synopsis's room. More collapses as
 *  part of the fold, which moves with the player. */
export function foldingSyn(s: SynState): SynState {
  return { ...s, expanded: false, easing: false, change: 'open' };
}

/** Whether `play`, this film's trailer in the panel, holds the synopsis
 *  folded to make its room: from when it is asked for until its close
 *  starts. More opens nothing while it does, since the whole text would
 *  push the video, fitted to the body, down past the body's bottom edge. */
export function holdsFold(play: Play | null): boolean {
  return play != null && !play.closing && play.synLines != null;
}

/** The synopsis a folding trailer gives back as its play ends: at its
 *  resting lines, collapsed. */
export function collapsedSyn(s: SynState): SynState {
  return s.expanded || s.easing ? { ...s, expanded: false, easing: false } : s;
}

/** How the synopsis is drawn. */
export interface SynView {
  /** The paragraph's max-height, in px. */
  maxHeight: number;
  /** Its line clamp, which gives the ellipsis; off while it moves or
   *  shows its whole text, so nothing snaps. */
  clamp: number | 'unset';
  /** Folded away entirely to make room for a trailer. */
  gone: boolean;
  /** Showing its whole text, so the toggle reads Less. */
  expanded: boolean;
  /** Whether it has the toggle at all. */
  more: boolean;
  /** Its transition, as --syn-tr. */
  tr: string;
}

/** The synopsis as it stands: at rest, opened by More, or folded for
 *  `play`, this film's trailer in the panel if one is open. The fold
 *  follows the player: it starts as the player opens, and as the player
 *  closes the synopsis unfolds to its resting lines, its clamp coming
 *  back once the player has gone. It shows its whole text only when no
 *  fold holds it; More pressed as the player closes opens it at once. */
export function synopsisView(s: SynState, play: Play | null, still: boolean): SynView {
  const fold = play?.open && play.synLines != null ? play.synLines : null;
  const gone = fold === 0;
  const lines = fold ?? s.lines;
  const expanded = s.expanded && fold == null;
  const folding = play != null && play.synLines != null && !play.synSettled;
  const clamped = !expanded && !s.easing && !folding;
  return {
    maxHeight: gone ? 0 : expanded ? s.full : lines * SYN_LH,
    clamp: clamped ? Math.max(1, lines) : 'unset',
    gone,
    expanded,
    more: s.over,
    tr: SYN_TR[still ? 'still' : play?.closing ? 'close' : s.change],
  };
}

/** An element's top inside the body, which is the offset parent its
 *  contents are measured against. */
function topIn(el: HTMLElement, body: HTMLElement): number {
  let top = 0;
  for (let e: HTMLElement | null = el; e && e !== body; e = e.offsetParent as HTMLElement | null) {
    top += e.offsetTop;
  }
  return top;
}

/** The paragraph's height with its whole text drawn: its clamp and its
 *  max-height taken off for one read, then put back before anything is
 *  painted. */
export function fullHeight(p: HTMLElement): number {
  const { webkitLineClamp, maxHeight } = p.style;
  p.style.webkitLineClamp = 'unset';
  p.style.maxHeight = 'none';
  const h = p.getBoundingClientRect().height;
  p.style.webkitLineClamp = webkitLineClamp;
  p.style.maxHeight = maxHeight;
  return h;
}

/** The lines the synopsis rests at in the panel as it is laid out now,
 *  and whether its whole text runs past them. Its top is taken where it
 *  rests: its block has no margin then, and pulls itself up over the gap
 *  above the trailer row only while a trailer folds it away, so whatever
 *  margin it has now, all of it or partway there, is put back. */
export function measureSynopsis(body: HTMLElement, p: HTMLElement): SynMeasure {
  const block = p.parentElement;
  const pulled = block ? parseFloat(getComputedStyle(block).marginTop) || 0 : 0;
  const lines = restingLines(body.clientHeight, topIn(p, body) - pulled);
  const full = fullHeight(p);
  return { lines, over: overflows(full, lines), full };
}

/** How the panel makes room for a trailer about to open under its
 *  synopsis (see roomForTrailer): how far the synopsis folds, and where
 *  the body scrolls to, if it has to. */
export function roomInPanel(
  body: HTMLElement | null,
  p: HTMLElement | null,
  VH: number,
  rest: Pick<SynState, 'lines' | 'over'>,
): { synLines: number | null; to: number | null } {
  const row = body?.querySelector<HTMLElement>('.cd-trailer');
  if (!body || !row) return { synLines: null, to: null };
  const { synLines, lift } = roomForTrailer({
    slotTop: topIn(row, body),
    VH,
    scrollTop: body.scrollTop,
    bodyH: body.clientHeight,
    shown: p ? p.getBoundingClientRect().height : null,
    lines: rest.lines,
    over: rest.over,
  });
  return { synLines, to: lift > 0 ? body.scrollTop + lift : null };
}

/** Scrolls the body to `to` on an ease-out over `ms`, so the scroll and
 *  the player opening beside it read as one motion; at once for a reader
 *  who has asked for nothing to move. Returns what stops it. */
function glideScroll(el: HTMLElement, to: number, ms: number): () => void {
  if (stillNow()) {
    el.scrollTop = to;
    return () => {};
  }
  const from = el.scrollTop;
  const t0 = performance.now();
  let frame = 0;
  const step = () => {
    const k = Math.min(1, (performance.now() - t0) / ms);
    el.scrollTop = from + (to - from) * scrollEase(k);
    if (k < 1) frame = requestAnimationFrame(step);
  };
  frame = requestAnimationFrame(step);
  return () => cancelAnimationFrame(frame);
}

/** What the film is about, at the lines its view gives it, with More or
 *  Less under it when the text runs past its resting lines. Its lengths
 *  are set here, one film's measurements; how it moves between them is
 *  the stylesheet's, timed by --syn-tr. */
export function SheetSynopsis({
  text,
  view,
  onToggle,
  textRef,
}: {
  text: string;
  view: SynView;
  onToggle: () => void;
  textRef?: Ref<HTMLParagraphElement>;
}) {
  return (
    <div
      className={`cd-sheet-syn${view.gone ? ' cd-sheet-syn-gone' : ''}`}
      style={{ ['--syn-tr' as string]: view.tr }}
    >
      <p
        ref={textRef}
        className="cd-sheet-syn-text"
        style={{ maxHeight: view.maxHeight, WebkitLineClamp: view.clamp }}
      >
        {text}
      </p>
      {view.more && (
        <div className="cd-sheet-syn-more">
          <button type="button" aria-expanded={view.expanded} disabled={view.gone} onClick={onToggle}>
            {view.expanded ? 'Less' : 'More'}
          </button>
        </div>
      )}
    </div>
  );
}

/** Whose map this is. The searched film is not connected to itself. */
export function headingFor(film: GridFilm, anchorTitle: string): string {
  return film.isAnchor ? 'Its cast and directors' : `Connected to ${anchorTitle} through`;
}

/** What a person did on the searched film. */
export function roleLine(p: GridPerson, anchorTitle: string): string {
  if (p.role === 'director') return `Directed ${anchorTitle}`;
  return p.character ? `${p.character} in ${anchorTitle}` : `In ${anchorTitle}`;
}

export type Versus = {
  dir: 'up' | 'down' | 'same';
  delta: number;
  title: string;
};

/** How this film's rating sits against the searched one. Omitted for the
 *  searched film itself and for anything nobody has rated. */
export function versus(film: GridFilm, anchor: GridFilm): Versus | null {
  if (film.isAnchor || film.rating == null || anchor.rating == null) return null;
  const delta = Math.round((film.rating - anchor.rating) * 10) / 10;
  if (delta === 0) return { dir: 'same', delta: 0, title: anchor.title };
  return { dir: delta > 0 ? 'up' : 'down', delta, title: anchor.title };
}

/** The comparison, in words: "0.4 above The Matrix", "1.4 below The
 *  Matrix", or "Same as The Matrix". */
export function versusText(v: Versus): string {
  if (v.dir === 'same') return `Same as ${v.title}`;
  return `${Math.abs(v.delta).toFixed(1)} ${v.dir === 'up' ? 'above' : 'below'} ${v.title}`;
}

/** Where a rating sits along the comparison's scale, as a percentage.
 *  The scale runs 4 to 9, where nearly every film on a map is; anything
 *  outside it is held at the end rather than drawn off the track. */
export function scalePct(rating: number): number {
  return ((Math.min(Math.max(rating, 4), 9) - 4) / 5) * 100;
}

/** How this film's rating sits against the searched one: said, then
 *  drawn on a 4–9 scale, the film as a knob at the end of its fill and
 *  the searched film as an accent tick. */
function VersusBlock({ film, anchor }: { film: GridFilm; anchor: GridFilm }) {
  const cmp = versus(film, anchor);
  if (!cmp || film.rating == null || anchor.rating == null) return null;
  const at = scalePct(film.rating);
  return (
    <div className="cd-sheet-versus">
      <div className="cd-sheet-versus-row">
        <span className={`cd-sheet-versus-text cd-sheet-versus-${cmp.dir}`}>{versusText(cmp)}</span>
        <span className="cd-sheet-versus-nums">
          {film.rating.toFixed(1)} · {anchor.rating.toFixed(1)}
        </span>
      </div>
      {/* The sentence above already says it. */}
      <div className="cd-sheet-scale" aria-hidden="true">
        <span className="cd-sheet-scale-line" />
        <span className="cd-sheet-scale-fill" style={{ width: `${at}%` }} />
        <span className="cd-sheet-scale-knob" style={{ left: `${at}%` }} />
        <span
          className="cd-sheet-scale-tick"
          style={{ left: `${scalePct(anchor.rating)}%` }}
          title={anchor.title}
        />
      </div>
    </div>
  );
}

/** The longest title the button will name. Past this the sentence is
 *  longer than the sheet and the name is cut to nothing useful, so it
 *  says what it does instead. */
const NAMEABLE = 28;

/** "Map Bad Words" rather than "Map this movie instead".
 *
 *  The old label described the control; this one says what will
 *  happen, which is the only thing the reader is deciding. The arrow
 *  is the going. */
function RemapButton({ film, onRemap }: { film: GridFilm; onRemap: () => void }) {
  const named = film.title.length <= NAMEABLE;
  return (
    <button
      type="button"
      className="cd-sheet-primary"
      aria-label={`Map ${film.title}`}
      onClick={onRemap}
    >
      <span className="cd-sheet-primary-text">
        {named ? `Map ${film.title}` : 'Map this movie'}
      </span>
      <svg
        width="16"
        height="16"
        viewBox="0 0 24 24"
        fill="none"
        stroke="currentColor"
        strokeWidth="2.4"
        strokeLinecap="round"
        strokeLinejoin="round"
        aria-hidden="true"
      >
        <path d="M5 12h14M13 6l6 6-6 6" />
      </svg>
    </button>
  );
}
