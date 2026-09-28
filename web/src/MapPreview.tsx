import { useEffect, useLayoutEffect, useRef, useState, type CSSProperties } from 'react';
import type { FaceCardEvents } from './faceCard';
import { genreLine, type GridFilm, type GridPerson } from './grid';
import { headingFor } from './GridSheet';
import { stillNow } from './motion';
import { personVars } from './personColour';
import { PersonFace, faceStart } from './PersonFace';
import { hueOf } from './poster';
import {
  SYN_LINE_H,
  SYN_LINES,
  SYN_SETTLE_MS,
  fitTrailer,
  holdInside,
  type PreviewBounds,
  type PreviewPlace,
} from './preview';
import { ENTER_MS } from './sheet';
import type { Theme } from './theme';
import { TrailerRow, useTrailer, type Player } from './TrailerRow';
import { PLAYER, isFor, videoHeight, wellHeight, type Play } from './trailer';

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
}

/** How a trailer opening in the preview made room for itself: the top
 *  the preview had before (`y0`), the one it glides to while the trailer
 *  is open (`y1`), and how far the synopsis folds. `y0` is kept after
 *  the trailer closes, so opening it again starts from the same place. */
interface Lift {
  y0: number;
  y1: number;
  synLines: number | null;
}

/** A card's people, synopsis and trailer, beside it on the map, for a
 *  pointer that has come to rest on it. Mounted afresh for each card it
 *  shows, so each one slides in from its card.
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
}: Props) {
  const box = useRef<HTMLDivElement>(null);
  const syn = useRef<HTMLParagraphElement>(null);
  // In a moment after it mounts, so its entrance has somewhere to run
  // from; at once for a reader who has asked for nothing to move. A
  // timer, as the sheets use, rather than a frame.
  const [shown, setShown] = useState(stillNow);
  useEffect(() => {
    const t = window.setTimeout(() => setShown(true), ENTER_MS);
    return () => window.clearTimeout(t);
  }, []);

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
  // box left for the close to play in.
  const { drop } = player;
  useEffect(() => () => drop('preview'), [drop]);
  // And a bigger photo opened from one of its faces, or on its way: the
  // face under the pointer goes without a mouseleave.
  useEffect(() => () => offFace('peek'), [offFace]);
  const play = previewPlay(player.play, film.id);
  // YouTube allows nothing over its player, and a bigger photo opened
  // from a face can land on it. While a trailer is set in the preview its
  // faces open none, and one already showing goes as the trailer starts.
  const trailerSet = play != null;
  useEffect(() => {
    if (trailerSet) offFace('peek');
  }, [trailerSet, offFace]);

  const [lift, setLift] = useState<Lift | null>(null);
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
      className={`cd-preview${spot.side < 0 ? ' cd-preview-left' : ''}${shown ? ' cd-preview-in' : ''}${play ? ' cd-preview-trailer' : ''}${play?.open ? ' cd-preview-trailer-open' : ''}`}
      style={style}
      role="group"
      aria-label={`${film.title}, preview`}
      onMouseEnter={onEnter}
      onMouseLeave={onLeave}
    >
      <span className="cd-preview-wash" aria-hidden="true" />
      <div className="cd-preview-head">
        <span className="cd-preview-title">{film.title}</span>
        <span className="cd-preview-meta">
          <span>{film.year}</span>
          <span className="cd-preview-pill">
            {film.rating == null ? 'No rating' : film.rating.toFixed(1)}
          </span>
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
              cardable={!trailerSet}
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
    </div>
  );
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
 *  preview closing, for a scroll, another map or anything else, drops
 *  its player only as it unmounts, which is after it has gone from the
 *  screen, so the layer goes with the preview itself rather than
 *  waiting for that. It takes no pointer: leaving the preview still
 *  closes it. */
export function TrailerFocus({ play, showing }: { play: Play | null; showing: string | null }) {
  const on = previewPlay(play, showing);
  if (!on) return null;
  return <div className={`cd-trailer-focus${on.open ? ' cd-trailer-focus-in' : ''}`} aria-hidden="true" />;
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
   *  while a trailer is set in the preview. */
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
 *  tooltip, and so does every face while a trailer is set in the
 *  preview, when none opens: that is how a pointer learns who it is. */
export function faceTitle(name: string, cardShows: boolean): string | undefined {
  return cardShows ? undefined : name;
}

/** A preview face's events: a pointer resting on it asks for that
 *  person's bigger photo, unless `cardable` is false (a trailer is set in
 *  the preview), and leaving puts it away. */
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
export const PREVIEW_FACES = 9;

/** The preview's row of faces. */
export interface FacesRow {
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
