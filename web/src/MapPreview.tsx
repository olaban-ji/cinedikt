import { useEffect, useLayoutEffect, useRef, useState, type CSSProperties } from 'react';
import { genreLine, type GridFilm } from './grid';
import { stillNow } from './motion';
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
import { TrailerRow, useTrailer, type Player } from './TrailerRow';
import { PLAYER, isFor, videoHeight, wellHeight } from './trailer';

interface Props {
  film: GridFilm;
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

/** A card's synopsis and trailer, beside it on the map, for a pointer
 *  that has come to rest on it. Mounted afresh for each card it shows,
 *  so each one slides in from its card.
 *
 *  It never takes the focus. The pointer that opened it keeps it open
 *  by moving onto it; the map closes it (see GridMap). */
export function MapPreview({ film, place, player, bounds, plotH, onEnter, onLeave }: Props) {
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
  const play = isFor(player.play, 'preview', film.id) ? player.play : null;

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
