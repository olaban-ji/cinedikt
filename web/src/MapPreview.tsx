import { useCallback, useEffect, useLayoutEffect, useRef, useState, type CSSProperties, type Ref } from 'react';
import { flushSync } from 'react-dom';
import type { FaceCardEvents } from './faceCard';
import { genreLine, ratingText, type GridFilm, type GridPerson } from './grid';
import { WatchLogo, headingFor } from './GridSheet';
import { stillNow } from './motion';
import { personVars } from './personColour';
import { PersonFace, faceStart } from './PersonFace';
import { hueOf } from './poster';
import {
  STREAM_GROW_MS,
  SYN_GAP,
  SYN_LINE_H,
  SYN_LINES,
  SYN_SETTLE_MS,
  fitTrailer,
  holdInside,
  streamShare,
  type PreviewBounds,
  type PreviewPlace,
} from './preview';
import { ENTER_MS, useEntered } from './sheet';
import type { Theme } from './theme';
import { TrailerRow, useTrailer, type Player } from './TrailerRow';
import { PLAYER, isFor, videoHeight, wellHeight, type Play } from './trailer';
import { offerLabel, streamPeek, useWhereToWatch, type WatchOffer } from './whereToWatch';

interface Props {
  film: GridFilm;
  /** The map's people, in the chip row's order (payload.people). */
  people: GridPerson[];
  /** The searched film's title, which the faces row's label names. */
  anchorTitle: string;
  /** The theme the people's colours are drawn for. */
  theme: Theme;
  /** Each person's initials, from initialsFor over the whole map. */
  codes: Map<string, string>;
  /** A person's photo, from the payload or asked for since; undefined
   *  while there is none to show. */
  photoOf: (p: GridPerson) => string | undefined;
  /** Where it sits, worked out as it opened (placePreview). */
  place: PreviewPlace;
  /** The page's one trailer player, which the preview's trailer plays in. */
  player: Player;
  /** The room the map has for it right now; `loose` lets it rise over
   *  the pinned rating axis. */
  bounds: (loose: boolean) => PreviewBounds;
  /** How tall the plot is, which a preview held by its bottom is placed
   *  up from. */
  plotH: number;
  onEnter: () => void;
  onLeave: () => void;
  /** A pointer resting on one of its faces, and leaving it, for that
   *  person's bigger photo (see useFaceCard). */
  onFace: FaceCardEvents['onFace'];
  offFace: FaceCardEvents['offFace'];
  /** Leaving: fading back towards its card (`plain`), the same slower
   *  with its trailer set (`playing`), or giving way to the next card's
   *  preview on a swap (`gone`). A leaving preview can still be taken
   *  back by the pointer; a gone one cannot. */
  leaving?: 'plain' | 'playing' | 'gone';
}

/** How a trailer opening in the preview made room for itself: the top
 *  the preview had before (`y0`), the one it glides to while the trailer
 *  is open (`y1`), and how far the synopsis folds. `y0` is kept after
 *  the trailer closes, so opening it again starts from the same place. */
export interface Lift {
  y0: number;
  y1: number;
  synLines: number | null;
}

/** A card's people, synopsis and trailer, and where its movie streams,
 *  beside it on the map, for a pointer that has come to rest on it.
 *  Mounted afresh for each card it shows, so each one slides in from its
 *  card.
 *
 *  It never takes the focus. The pointer that opened it keeps it open
 *  by moving onto it; the map closes it (see GridMap). */
export function MapPreview({
  film,
  people,
  anchorTitle,
  theme,
  codes,
  photoOf,
  place,
  player,
  bounds,
  plotH,
  onEnter,
  onLeave,
  onFace,
  offFace,
  leaving,
}: Props) {
  const box = useRef<HTMLDivElement>(null);
  const syn = useRef<HTMLParagraphElement>(null);
  const shown = useEntered();

  // Where it sits once its height is known: moved back inside the map
  // if it grows past an edge. Measured before the first paint, so it is
  // never seen anywhere else.
  const [held, setHeld] = useState<PreviewPlace | null>(null);
  useLayoutEffect(() => {
    const el = box.current;
    if (!el) return;
    const at = holdInside(place, el.getBoundingClientRect().height, bounds(false), plotH);
    if (at !== place) setHeld(at);
    // Once, as it opens: a preview is placed for the card it is beside.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, []);
  const spot = held ?? place;

  const trailer = useTrailer(film.id);
  // A preview that goes takes its trailer with it at once: there is no
  // box left for the close to play in. Only its own: one fading out on a
  // swap must never take the next preview's.
  const { drop } = player;
  const filmId = film.id;
  useEffect(() => () => drop('preview', filmId), [drop, filmId]);
  // And a bigger photo opened from one of its faces, or on its way: the
  // face under the pointer goes without a mouseleave. Not for one that
  // gave way on a swap, which has been inert since, so by the time it
  // unmounts a bigger photo from a preview's face can only be the next
  // preview's.
  const goneNow = useRef(false);
  goneNow.current = leaving === 'gone';
  useEffect(
    () => () => {
      if (!goneNow.current) offFace('peek');
    },
    [offFace],
  );
  const play = previewPlay(player.play, film.id);
  // YouTube allows nothing over its player, and a bigger photo opened
  // from a face can land on it. While a trailer is set in the preview its
  // faces open none, and one already showing goes as the trailer starts.
  // Nor while the preview is leaving: one from its faces, or on its way,
  // goes as the leave begins, rather than hanging over the preview as it
  // fades.
  const trailerSet = play != null;
  const noCard = trailerSet || leaving != null;
  useEffect(() => {
    if (noCard) offFace('peek');
  }, [noCard, offFace]);

  const [lift, setLift] = useState<Lift | null>(null);

  // Where the movie streams, as the preview's last line. An answer in
  // as it opened is there from the first paint; one that comes after
  // grows in, from a moment after it is first drawn closed. One coming
  // as the preview leaves waits: it would grow as the box fades. While a
  // trailer is set in the preview the row folds away, and opens out
  // again once the trailer has gone.
  const peekRow = streamPeek(useWhereToWatch(film.id));
  const streamRow = useRef<HTMLDivElement>(null);
  const [grown, setGrown] = useState(() => streamStartsGrown(peekRow != null, stillNow()));
  const rowPhase = streamPhaseOf(peekRow != null, grown, leaving, trailerSet);
  const drawRow = rowPhase != null;

  // The row opening out makes the box taller, so once it has, the box
  // is held inside the map again. Read at that moment, from the latest
  // render: by then a trailer may have opened in it and moved it since.
  // Not while the preview is leaving: it fades where it stands. Taken
  // back, it is held again.
  const latest = useRef({ spot, lift, bounds, plotH, leaving, trailerSet });
  latest.current = { spot, lift, bounds, plotH, leaving, trailerSet };
  const reHold = useCallback(() => {
    const el = box.current;
    const now = latest.current;
    if (!el || now.leaving != null) return;
    const again = holdAgain(now.spot, now.lift, el.getBoundingClientRect().height, now.bounds(false), now.plotH);
    if (again.held) setHeld(again.held);
    if (again.lift) setLift(again.lift);
  }, []);

  // A preview held by its top with too little room below for the row
  // would grow down past the edge, and only then be moved back up. So as
  // the row starts to grow in, such a preview is held by its bottom, where
  // its bottom is then, and grows upwards instead (growsUp). The row's
  // own height is there to read while it is still closed: only the
  // wrapper round it is clipped to nothing.
  const growIn = useCallback(() => {
    const el = box.current;
    const row = streamRow.current?.querySelector('.cd-preview-stream-row');
    const now = latest.current;
    if (el && row && now.leaving == null && !now.trailerSet) {
      const up = growsUp(
        now.spot,
        now.lift,
        el.getBoundingClientRect().height,
        row.getBoundingClientRect().height + SYN_GAP,
        now.bounds(false),
        now.plotH,
      );
      if (up) setHeld(up);
    }
    setGrown(true);
  }, []);
  useEffect(() => (drawRow && !grown ? growSoon(growIn) : undefined), [drawRow, grown, growIn]);

  const rowOpen = rowHolds(rowPhase, leaving);
  const wasOpen = useRef(rowOpen);
  // Before the paint, so a row that simply appears, for a reader who has
  // asked for nothing to move, is held in the same frame (afterGrow).
  useLayoutEffect(() => {
    if (!rowOpen) {
      wasOpen.current = false;
      return;
    }
    if (wasOpen.current) return;
    wasOpen.current = true;
    return afterGrow(reHold);
  }, [rowOpen, reHold]);

  // Logos come in after the first paint, and a row of wide ones, or of
  // names standing in for ones that failed, can wrap to a second line
  // once they have, after the box was held. So while the row is open the
  // box is held again whenever the row's height changes, before that
  // frame is painted, so the taller box is never seen past the edge. The
  // inner row is watched rather than its grid wrapper, so the wrapper's
  // own grow and fold set nothing off.
  useEffect(() => {
    const row = streamRow.current?.querySelector('.cd-preview-stream-row');
    if (!rowOpen || !row) return;
    let was = row.getBoundingClientRect().height;
    const seen = new ResizeObserver(() => {
      const h = row.getBoundingClientRect().height;
      if (h === was) return;
      was = h;
      flushSync(reHold);
    });
    seen.observe(row);
    return () => seen.disconnect();
  }, [rowOpen, reHold]);

  // The synopsis folds as the trailer opens, and its line clamp follows
  // only once the fold has finished, so the ellipsis does not snap to
  // its new line mid-move. Closing puts the clamp back first, then the
  // synopsis unfolds.
  const folded = play?.open && play.synLines != null ? play.synLines : null;
  const [settled, setSettled] = useState(false);
  useEffect(() => {
    if (folded == null) {
      setSettled(false);
      return;
    }
    const t = window.setTimeout(() => setSettled(true), stillNow() ? 0 : SYN_SETTLE_MS);
    return () => window.clearTimeout(t);
  }, [folded]);

  const onPlay = (muted: boolean, W: number) => {
    const el = box.current;
    if (!el) return;
    const was = player.now();
    let fit = lift;
    // Taken back while it closes, the box is still partway open, so the
    // room worked out when it first opened stands.
    if (!fit || !(isFor(was, 'preview', film.id) && was.closing)) {
      const y0 = lift?.y0 ?? el.offsetTop;
      const room = fitTrailer(
        {
          y0,
          height: el.getBoundingClientRect().height,
          grow: wellHeight('preview', videoHeight(W)) - PLAYER.preview.pad,
          synH: syn.current ? syn.current.getBoundingClientRect().height : null,
          streamH: streamShare(
            streamRow.current && {
              height: streamRow.current.getBoundingClientRect().height,
              marginTop: parseFloat(getComputedStyle(streamRow.current).marginTop) || 0,
            },
          ),
        },
        bounds(false),
        bounds(true),
      );
      fit = { y0, y1: room.top, synLines: room.synLines };
      setLift(fit);
    }
    player.start('preview', film.id, muted, W, fit.synLines);
  };

  // Once a trailer has opened in it, the preview is held by its top,
  // which glides up with the opening and back down with the close.
  const top = lift ? (play?.open ? lift.y1 : lift.y0) : spot.top;
  const bottom = lift ? null : spot.bottom;
  const style: CSSProperties = {
    left: spot.x,
    top: top ?? undefined,
    bottom: bottom ?? undefined,
    // The film's hue, which the wash is drawn in (see .cd-preview-wash).
    ['--h' as string]: hueOf(film.title),
  };
  const synopsis = film.synopsis?.trim();
  const genres = genreLine(film);
  const faces = facesRow(film, people, anchorTitle);
  const synStyle: CSSProperties | undefined =
    folded == null
      ? undefined
      : {
          maxHeight: folded * SYN_LINE_H,
          WebkitLineClamp: settled ? Math.max(1, folded) : SYN_LINES,
        };

  return (
    <div
      ref={box}
      className={`cd-preview${spot.side < 0 ? ' cd-preview-left' : ''}${shown ? ' cd-preview-in' : ''}${play ? ' cd-preview-trailer' : ''}${play?.open ? ' cd-preview-trailer-open' : ''}${leavingClass(leaving)}`}
      style={style}
      role="group"
      aria-label={`${film.title}, preview`}
      inert={leaving === 'gone'}
      aria-hidden={leaving === 'gone' ? true : undefined}
      onMouseEnter={onEnter}
      onMouseLeave={onLeave}
    >
      <span className="cd-preview-wash" aria-hidden="true" />
      <div className="cd-preview-head">
        <span className="cd-preview-title">{film.title}</span>
        <span className="cd-preview-meta">
          <span>{film.year}</span>
          <span className="cd-preview-pill">{ratingText(film.rating)}</span>
          {genres && <span className="cd-preview-genres">{genres}</span>}
        </span>
      </div>
      {faces.shown.length > 0 && (
        <div
          className={`cd-preview-faces${faces.named ? ' cd-preview-faces-named' : ''}`}
          role="img"
          aria-label={faces.label}
        >
          {faces.shown.map((p) => (
            <PreviewFace
              key={p.id}
              person={p}
              photo={photoOf(p)}
              code={codes.get(p.id) ?? '?'}
              theme={theme}
              named={faces.named}
              cardable={!noCard}
              onFace={onFace}
              offFace={offFace}
            />
          ))}
          {faces.more > 0 && <span className="cd-preview-faces-more">+{faces.more}</span>}
        </div>
      )}
      <p
        ref={syn}
        className={`cd-preview-syn${synopsis ? '' : ' cd-preview-syn-none'}${folded === 0 ? ' cd-preview-syn-gone' : ''}`}
        style={synStyle}
      >
        {synopsis || 'No synopsis yet.'}
      </p>
      <TrailerRow
        where="preview"
        id={film.id}
        title={film.title}
        trailer={trailer}
        player={player}
        onPlay={onPlay}
      />
      {rowPhase && peekRow && (
        <PreviewStream ref={streamRow} shown={peekRow.shown} more={peekRow.more} theme={theme} phase={rowPhase} />
      )}
    </div>
  );
}

/** How the preview's Stream row is drawn: closed, about to grow in for
 *  an answer that came after the preview opened (`out`); open (`in`); or
 *  folded away while a trailer is set in the preview (`folded`). */
type StreamPhase = 'out' | 'in' | 'folded';

/** Whether a preview's Stream row starts open: for an answer in as the
 *  preview opened, and for a reader who has asked for nothing to move,
 *  whose row simply appears. Otherwise it is drawn closed first, and
 *  grows in. */
export function streamStartsGrown(answeredAtOpen: boolean, still: boolean): boolean {
  return answeredAtOpen || still;
}

/** How the preview's Stream row is drawn now, or null for no row:
 *  nothing to stream, or an answer that came while the preview was
 *  leaving, which waits rather than growing as the box fades, and grows
 *  in if the preview is taken back. One that has grown goes with the
 *  preview's leave, folding nothing on its own. */
export function streamPhaseOf(
  hasRow: boolean,
  grown: boolean,
  leaving: Props['leaving'],
  trailerSet: boolean,
): StreamPhase | null {
  if (!hasRow || (!grown && leaving != null)) return null;
  return !grown ? 'out' : trailerSet ? 'folded' : 'in';
}

/** Whether a preview is held inside the map again for its Stream row,
 *  once the row has opened out and whenever its height changes: only
 *  while the row is open, and not while the preview is leaving, which
 *  fades where it stands. */
export function rowHolds(phase: StreamPhase | null, leaving: Props['leaving']): boolean {
  return phase === 'in' && leaving == null;
}

/** Opens a Stream row drawn closed a moment after it is drawn, so its
 *  grow has the closed state to run from; at once for a reader who has
 *  asked for nothing to move. A timer, as the preview's own entrance
 *  uses, rather than a frame. Returns what cancels it. */
export function growSoon(open: () => void): () => void {
  const t = window.setTimeout(open, stillNow() ? 0 : ENTER_MS);
  return () => window.clearTimeout(t);
}

/** Runs `then` once a Stream row opening out has finished, and a little
 *  over. For a reader who has asked for nothing to move, the row is open
 *  as soon as it is drawn, so `then` runs there and then: called before
 *  a paint, the box is never seen past the edge. Returns what cancels
 *  it. */
export function afterGrow(then: () => void): () => void {
  if (stillNow()) {
    then();
    return () => {};
  }
  const t = window.setTimeout(then, STREAM_GROW_MS + 20);
  return () => window.clearTimeout(t);
}

/** Where a preview that has grown taller goes, held inside the map
 *  again (holdInside): `held`, its new place, while no trailer has
 *  opened in it; once one has, it is held by its top at the lift's `y0`,
 *  and `lift` is that lift with `y0` moved. Neither when it still fits
 *  where it is. */
export function holdAgain(
  spot: PreviewPlace,
  lift: Lift | null,
  height: number,
  b: PreviewBounds,
  plotH: number,
): { held?: PreviewPlace; lift?: Lift } {
  if (lift) {
    const at = holdInside({ ...spot, top: lift.y0, bottom: null }, height, b, plotH);
    return at.top != null && at.top !== lift.y0 ? { lift: { ...lift, y0: at.top } } : {};
  }
  const at = holdInside(spot, height, b, plotH);
  return at === spot ? {} : { held: at };
}

/** Where a preview goes as its Stream row starts to grow in, `added`
 *  taller once it has: held by its bottom, where its bottom is now, when
 *  it is held by its top and the row would take it past the bottom of
 *  the room, so it grows upwards rather than down past the edge and back.
 *  Null, leaving it where it is, when it is held by its bottom already,
 *  a trailer has opened in it (`lift`), the row fits below, or growing
 *  upwards would take it past the top; holdAgain settles any of those
 *  once the row has grown. */
export function growsUp(
  spot: PreviewPlace,
  lift: Lift | null,
  height: number,
  added: number,
  b: PreviewBounds,
  plotH: number,
): PreviewPlace | null {
  if (lift || spot.top == null) return null;
  const foot = spot.top + height;
  if (foot + added <= b.vb || spot.top - added < b.vt) return null;
  return { ...spot, top: null, bottom: plotH - foot };
}

/** The preview's last line: "Stream", up to three services the movie
 *  streams on with a subscription or for free, each opening it there in
 *  a new tab, and a count of the rest. It grows in and folds away on a
 *  grid row going between 0fr and 1fr, its margin taking back the box's
 *  gap above it as it closes. Closed or folded, it is inert as well as
 *  hidden, so a keyboard never lands on a link that cannot be seen and a
 *  screen reader does not read one out. */
export function PreviewStream({
  ref,
  shown,
  more,
  theme,
  phase,
}: {
  ref?: Ref<HTMLDivElement>;
  shown: WatchOffer[];
  more: number;
  theme: Theme;
  phase: StreamPhase;
}) {
  return (
    <div
      ref={ref}
      className={`cd-preview-stream${phase === 'in' ? '' : ` cd-preview-stream-${phase}`}`}
      inert={phase !== 'in' || undefined}
      aria-hidden={phase !== 'in' || undefined}
    >
      <div className="cd-preview-stream-clip">
        <div className="cd-preview-stream-row">
          <span className="cd-preview-stream-label">Stream</span>
          {shown.map((o) => (
            <a
              key={o.id}
              className="cd-preview-stream-chip"
              href={o.link}
              target="_blank"
              rel="noopener noreferrer"
              aria-label={offerLabel('stream', o)}
            >
              <WatchLogo offer={o} theme={theme} logoClass="cd-preview-stream-logo" nameClass="cd-preview-stream-name" />
            </a>
          ))}
          {more > 0 && <span className="cd-preview-stream-more">+{more}</span>}
        </div>
      </div>
    </div>
  );
}

/** The classes a leaving preview takes on (see `leaving`). */
export function leavingClass(leaving: Props['leaving']): string {
  if (!leaving) return '';
  return ` cd-preview-out${leaving === 'playing' ? ' cd-preview-out-playing' : leaving === 'gone' ? ' cd-preview-gone' : ''}`;
}

/** The page's player, if it is set in the preview for this film:
 *  opening, open or closing. Null for one in the panel, for another
 *  film's, and when no preview is drawn (`id` null). */
export function previewPlay(play: Play | null, id: string | null): Play | null {
  return id != null && isFor(play, 'preview', id) ? play : null;
}

/** Behind a trailer playing in the hover preview, one layer over the
 *  whole window blurs and dims the map, the header and the floating
 *  buttons, and the preview rises above it (.cd-preview-trailer). It is
 *  there while the preview's player is set, in once that player is open,
 *  and out again as it closes, so its fade runs with the player's own.
 *  It goes when the player is cleared.
 *
 *  `showing` is the film whose preview is drawn, null while none is. A
 *  preview leaving keeps its player to the end, and the layer lifts in
 *  step with it (`out`), coming back with it if it is taken back
 *  (`back`). A preview taken away at once (another map, a new layout)
 *  drops its player only as it unmounts, after it has gone from the
 *  screen, so the layer goes with the preview itself rather than
 *  waiting for that. It takes no pointer: leaving the preview still
 *  closes it. */
export function TrailerFocus({
  play,
  showing,
  out,
  back,
}: {
  play: Play | null;
  showing: string | null;
  /** The showing preview is leaving: the layer lifts with it. */
  out?: 'plain' | 'playing';
  /** It was taken back mid-leave: the layer comes back quicker. */
  back?: boolean;
}) {
  const on = previewPlay(play, showing);
  if (!on) return null;
  return (
    <div
      className={`cd-trailer-focus${on.open && !out ? ' cd-trailer-focus-in' : ''}${out ? ' cd-trailer-focus-out' : ''}${back ? ' cd-trailer-focus-back' : ''}`}
      aria-hidden="true"
    />
  );
}

/** One face in the preview's row, in its person's colour, named beside
 *  it when the row is short enough. */
function PreviewFace({
  person: p,
  photo,
  code,
  theme,
  named,
  cardable,
  onFace,
  offFace,
}: {
  person: GridPerson;
  photo: string | undefined;
  code: string;
  theme: Theme;
  named: boolean;
  /** Whether a pointer resting on it may open the bigger photo: not
   *  while a trailer is set in the preview, nor while it is leaving. */
  cardable: boolean;
  onFace: FaceCardEvents['onFace'];
  offFace: FaceCardEvents['offFace'];
}) {
  // Whether the photo is showing. One that has already loaded this
  // visit, as the chips' usually have, shows from the first paint, so the
  // face goes without a title from the first paint too.
  const [shown, setShown] = useState(() => faceStart(photo).loaded);
  return (
    <span
      className="cd-preview-face"
      title={faceTitle(p.name, shown && cardable)}
      style={personVars(p, theme)}
      {...faceEvents(p.id, onFace, offFace, cardable)}
    >
      <PersonFace photo={photo} code={code} size="preview" square={p.role === 'director'} onShown={setShown} />
      {named && <span className="cd-preview-face-name">{p.name}</span>}
    </span>
  );
}

/** A preview face's title: the person's name, unless a bigger photo can
 *  open from it. A face showing its photo has that bigger one for a
 *  pointer resting on it, which names them, and the browser's tooltip
 *  would say it again. A face still showing its initials, because there
 *  is no photo, it failed to load or it has not arrived yet, keeps the
 *  tooltip, and so does every face while none opens (a trailer set in
 *  the preview, or the preview leaving): that is how a pointer learns
 *  who it is. */
export function faceTitle(name: string, cardShows: boolean): string | undefined {
  return cardShows ? undefined : name;
}

/** A preview face's events: a pointer resting on it asks for that
 *  person's bigger photo, unless `cardable` is false (a trailer is set in
 *  the preview, or it is leaving), and leaving puts it away. */
export function faceEvents(
  id: string,
  onFace: FaceCardEvents['onFace'],
  offFace: FaceCardEvents['offFace'],
  cardable = true,
) {
  return {
    onMouseEnter: (e: { currentTarget: HTMLElement }) => {
      if (cardable) onFace(id, e.currentTarget, 'peek');
    },
    onMouseLeave: () => offFace(),
  };
}

/** How many faces fit across the preview: nine at 30px with their gaps
 *  inside its 360px. Past that, one makes way for the count. */
const PREVIEW_FACES = 9;

/** The preview's row of faces. */
interface FacesRow {
  /** The faces drawn, in the chip row's order. */
  shown: GridPerson[];
  /** How many more there are than are drawn, said as "+N". */
  more: number;
  /** Few enough to put each name beside its face. */
  named: boolean;
  /** The row's accessible name: the panel's heading, then every name. */
  label: string;
}

/** Who links a film to the searched one, as the preview's faces row
 *  shows them: the film's people in the chip row's order (directors, then
 *  the billed cast), or everyone for the searched film. One or two are
 *  named beside their faces; more are faces alone. Up to PREVIEW_FACES
 *  are drawn; past that, one fewer and a count. The label names them
 *  all, after the heading the panel gives the same list. */
export function facesRow(
  film: Pick<GridFilm, 'isAnchor' | 'people'>,
  people: readonly GridPerson[],
  anchorTitle: string,
): FacesRow {
  const who = film.isAnchor ? [...people] : people.filter((p) => film.people.includes(p.id));
  const fit = who.length <= PREVIEW_FACES ? who.length : PREVIEW_FACES - 1;
  return {
    shown: who.slice(0, fit),
    more: who.length - fit,
    named: who.length <= 2,
    label: `${headingFor(film, anchorTitle)}: ${who.map((p) => p.name).join(', ')}`,
  };
}
