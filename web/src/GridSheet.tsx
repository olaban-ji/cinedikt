import { useEffect, useMemo, useRef, type CSSProperties, type MutableRefObject } from 'react';
import { initialsFor, type GridFilm, type GridPayload, type GridPerson } from './grid';
import { personColour } from './personColour';
import { PosterImage } from './PosterImage';
import { hueOf, posterFallback, sheetPosterPx } from './poster';
import { useScreen } from './screen';
import { SHEET_EXIT_MS, useCloser, useDrag, useEscape, useFocusTrapped, useGlide } from './sheet';
import { useResolvedTheme } from './theme';
import { TrailerRow, useTrailer, type Player } from './TrailerRow';
import { escapeCloses, isFor } from './trailer';

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
  const playing = isFor(player.play, 'panel', film.id);
  const synopsis = film.synopsis?.trim();
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
        <div className={`cd-sheet-body${playing ? ' cd-sheet-playing' : ''}`}>
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
              </div>
            </div>
          </div>

          {/* What the film is about, whole, and its trailer, which plays
              here rather than on another tab. */}
          <div className="cd-sheet-about">
            {synopsis && <p className="cd-sheet-synopsis">{synopsis}</p>}
            <TrailerRow
              where="panel"
              id={film.id}
              title={film.title}
              trailer={trailer}
              player={player}
              onPlay={(muted, W) => {
                // A rest on the button that comes due as the panel goes
                // starts nothing.
                if (phase !== 'out') player.start('panel', film.id, muted, W);
              }}
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
