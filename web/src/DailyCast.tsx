import { useEffect, useRef, useState, type PointerEvent } from 'react';
import type { DailyPerson } from './api';
import {
  BAR_WIDTHS,
  CAST_LIST_LABEL,
  NEXT_ROW_LABEL,
  PEEK_REST_MS,
  hueColour,
  moviesLabel,
  peekOpensUp,
  toneStyle,
  type CastRow,
} from './daily';
import { DailyFace } from './DailyFace';
import type { Theme } from './theme';

// The cast list: today's six names in reveal order, sixth-billed first and
// the star last. A hidden row is its number and a bar, and nothing else:
// the page is never told a hidden name, so none can be read off it.
//
// Every row draws both of its layers all the time, the placeholder and the
// name, and the row's state says which shows, so a name arriving is a
// change of class on parts already there: the placeholder fades out, the
// name fades in and rises, the face fades in and grows with a little
// bounce. Parts put down in the same render as the change would simply
// appear, as a transition needs something to start from.

/** A hidden row, for a screen reader: no name, as the page has none. */
export const HIDDEN_ROW = 'Not shown yet';

/** The three offset cards of the Movies button. */
function MoviesIcon() {
  return (
    <svg
      width="15"
      height="15"
      viewBox="0 0 24 24"
      fill="none"
      stroke="currentColor"
      strokeWidth="2"
      strokeLinecap="round"
      strokeLinejoin="round"
      aria-hidden="true"
    >
      <rect x="3" y="4" width="8" height="5" rx="1.5" />
      <rect x="13" y="9.5" width="8" height="5" rx="1.5" />
      <rect x="6" y="15" width="8" height="5" rx="1.5" />
    </svg>
  );
}

interface CastProps {
  rows: readonly CastRow[];
  /** The game is on and its names are out: shown rows offer Movies. */
  playing: boolean;
  /** At the end, when each name the reader didn't see appears, by slot
   *  (revealDelays). Empty for a game opened already over, or with
   *  stillness asked for, when they are simply there. */
  delays: ReadonlyMap<number, number>;
  codes: ReadonlyMap<string, string>;
  theme: Theme;
  onNext: () => void;
  onMovies: (person: DailyPerson) => void;
}

/** The list, labelled "Today's names" for a screen reader. Its photo
 *  preview is one at a time: on a mouse, after resting on a shown face for
 *  PEEK_REST_MS, gone on leaving it; on touch, a tap on the face toggles
 *  it, and a tap anywhere else puts it away. */
export function DailyCast({ rows, playing, delays, codes, theme, onNext, onMovies }: CastProps) {
  const [peek, setPeek] = useState<number | null>(null);
  const rest = useRef(0);
  useEffect(() => () => window.clearTimeout(rest.current), []);
  // A tap anywhere but a face puts the preview away.
  useEffect(() => {
    if (peek == null) return;
    const away = (e: globalThis.PointerEvent) => {
      if (!(e.target instanceof Element) || !e.target.closest('[data-peek]')) setPeek(null);
    };
    window.addEventListener('pointerdown', away);
    return () => window.removeEventListener('pointerdown', away);
  }, [peek]);
  // A row that stops being shown, as at Play again, takes its preview.
  const open = peek != null && rows.some((r) => r.slot === peek && r.state === 'shown') ? peek : null;

  const enter = (slot: number) => (e: PointerEvent) => {
    if (e.pointerType !== 'mouse') return;
    window.clearTimeout(rest.current);
    rest.current = window.setTimeout(() => setPeek(slot), PEEK_REST_MS);
  };
  const leave = (slot: number) => (e: PointerEvent) => {
    if (e.pointerType !== 'mouse') return;
    window.clearTimeout(rest.current);
    setPeek((p) => (p === slot ? null : p));
  };
  const tap = (slot: number) => (e: PointerEvent) => {
    if (e.pointerType === 'mouse') return;
    setPeek((p) => (p === slot ? null : slot));
  };

  return (
    <ol className="cd-nd-cast" aria-label={CAST_LIST_LABEL}>
      {rows.map((r) => {
        const shown = r.state === 'shown';
        const next = r.state === 'next';
        const d = delays.get(r.slot);
        const late = d ? { transitionDelay: `${d}ms` } : undefined;
        const colour = shown ? toneStyle(hueColour(r.person.hue, theme)) : undefined;
        return (
          <li key={r.slot} className={`cd-nd-row cd-nd-row-${r.state}`}>
            <span
              className="cd-nd-facebox"
              data-peek={shown ? '1' : undefined}
              onPointerEnter={shown ? enter(r.slot) : undefined}
              onPointerLeave={shown ? leave(r.slot) : undefined}
              onPointerDown={shown ? tap(r.slot) : undefined}
            >
              {shown && (
                <span
                  className={`cd-nd-peek${peekOpensUp(r.slot) ? ' cd-nd-peek-up' : ''}${open === r.slot ? ' cd-nd-peek-in' : ''}`}
                  style={colour}
                  aria-hidden="true"
                >
                  <span className="cd-nd-peek-photo">
                    <span className="cd-nd-peek-code">{codes.get(r.person.id) ?? '?'}</span>
                    {r.person.photo && <PeekPhoto src={r.person.photo} />}
                  </span>
                  <span className="cd-nd-peek-name">{r.person.name}</span>
                  {r.line && <span className="cd-nd-peek-line">{r.line}</span>}
                </span>
              )}
              <span className="cd-nd-ph-face" style={late} aria-hidden="true">
                ?
              </span>
              <span className="cd-nd-facewrap" style={late} aria-hidden="true">
                {shown && <DailyFace person={r.person} code={codes.get(r.person.id) ?? '?'} size="row" theme={theme} />}
              </span>
            </span>
            <span className="cd-nd-text">
              <span className="cd-nd-ph" style={late} aria-hidden="true">
                <span className="cd-nd-skel" style={{ width: `${shown ? BAR_WIDTHS[r.slot] : r.bar}%` }} />
                {next && <span className="cd-nd-next-word">Next</span>}
              </span>
              <span className="cd-nd-who" style={late}>
                {shown ? (
                  <>
                    <span className="cd-nd-name">{r.person.name}</span>
                    {r.line && <span className="cd-nd-also">{r.line}</span>}
                  </>
                ) : (
                  // A screen reader hears six names, some not out yet;
                  // the next row's button says what it costs to show one.
                  !next && <span className="cd-sr-live">{HIDDEN_ROW}</span>
                )}
              </span>
            </span>
            {shown && playing && (
              <button
                type="button"
                className="cd-nd-movies"
                aria-label={moviesLabel(r.person.name)}
                onClick={() => onMovies(r.person)}
              >
                <MoviesIcon />
                Movies
              </button>
            )}
            {next && <button type="button" className="cd-nd-row-go" aria-label={NEXT_ROW_LABEL} onClick={onNext} />}
          </li>
        );
      })}
    </ol>
  );
}

/** The preview's photo, which goes, leaving the initials, if it will not
 *  load. */
function PeekPhoto({ src }: { src: string }) {
  const [failed, setFailed] = useState<string | null>(null);
  if (failed === src) return null;
  return <img className="cd-nd-peek-img" src={src} alt="" decoding="async" onError={() => setFailed(src)} />;
}
