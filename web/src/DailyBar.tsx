import { useEffect, useId, useRef, useState, type KeyboardEvent, type RefObject } from 'react';
import { searchMovies, type DailyGame, type SearchHit } from './api';
import {
  DISMISS,
  GUESS_FIELD,
  HINT_RISE,
  LIST_CLOSE_MS,
  MESSAGE_FACES,
  SEARCH_MIN_CHARS,
  SEARCH_WAIT_MS,
  TOASTS,
  guessLabel,
  guessOptions,
  guessPlaceholder,
  resultsMax,
  toneStyle,
  warmthColour,
  yearsShown,
  type GuessFound,
  type GuessMessage,
} from './daily';
import { DailyFace } from './DailyFace';
import { animate } from './motion';
import { PosterImage } from './PosterImage';
import { posterFallback } from './poster';
import type { Theme } from './theme';

// The guess bar, pinned under the page while the game is on (or a column
// beside the cast on a landscape phone): what the last wrong guess told,
// over one row, the field that names the movie and Guess, with its
// results above it. The row says it all: the field what the next miss
// costs, or that it is the last guess, and Guess the points getting it
// now would score. The next name is the cast's dashed next row's to give.

interface BarProps {
  game: DailyGame;
  /** What the wrong guess on show told: the newest, or the one whose chip
   *  was pressed. Null with none on show. */
  message: GuessMessage | null;
  /** The first-time warmth note, over the message, after the game's
   *  first wrong guess (warmthNote); null with none to show. */
  note?: string | null;
  onNoteClose?: () => void;
  codes: ReadonlyMap<string, string>;
  theme: Theme;
  /** Below 640px, now, where the list holds four results rather than
   *  six. */
  phone: boolean;
  /** Play is still hiding the names: nothing can be done yet. */
  opening: boolean;
  guessed: ReadonlySet<string>;
  inputRef: RefObject<HTMLInputElement | null>;
  /** A guess at this movie, by IMDb id. Whether it was taken, so the field
   *  can clear; one that never got an answer stays typed, so trying again
   *  is one press. */
  onGuess: (id: string) => Promise<boolean>;
  say: (text: string) => void;
}

export function DailyBar({
  game,
  message,
  note = null,
  onNoteClose,
  codes,
  theme,
  phone,
  opening,
  guessed,
  inputRef,
  onGuess,
  say,
}: BarProps) {
  const listId = useId();
  const [q, setQ] = useState('');
  // Kept with the words it answers, so rows are never offered under words
  // they were not found for (guessOptions).
  const [found, setFound] = useState<GuessFound<SearchHit> | null>(null);
  const [at, setAt] = useState(0);
  const [focus, setFocus] = useState(false);
  const [sending, setSending] = useState(false);
  const blur = useRef(0);
  const typed = q.trim();
  const asking = typed.length >= SEARCH_MIN_CHARS;
  const most = resultsMax(phone);

  // A pause in the typing asks the catalog, and a newer question calls
  // off the one before, as the header's search does.
  useEffect(() => {
    if (!asking) return;
    const ctrl = new AbortController();
    const t = window.setTimeout(() => {
      searchMovies(typed, ctrl.signal)
        .then((hits) => {
          if (ctrl.signal.aborted) return;
          setFound({ q: typed, hits: (hits ?? []).slice(0, most), failed: false });
          setAt(0);
        })
        .catch(() => {
          // A search that never answered is not one that found nothing.
          if (!ctrl.signal.aborted) setFound({ q: typed, hits: [], failed: true });
        });
    }, SEARCH_WAIT_MS);
    return () => {
      window.clearTimeout(t);
      ctrl.abort();
    };
  }, [typed, asking, most]);

  useEffect(() => () => window.clearTimeout(blur.current), []);

  const options = guessOptions(typed, asking ? found : null);
  // What Enter, the arrows, Guess and the field's active row go by: only
  // rows found for exactly what is typed, so a row left from words typed
  // past is never a guess the reader didn't mean, which costs points.
  const rows = asking ? options.rows : [];
  const listed = focus && asking;
  const active = listed && rows.length ? Math.min(at, rows.length - 1) : null;

  // The highlighted result is what Enter guesses, so it stays in sight
  // as the arrows move it, or as new results come, wherever the list has
  // had to scroll: a landscape phone's short column (grid.css). Only a
  // list that scrolls is asked, so nothing round it moves for this.
  useEffect(() => {
    if (active == null) return;
    const list = document.getElementById(listId);
    if (!list || list.scrollHeight <= list.clientHeight) return;
    document.getElementById(`${listId}-${active}`)?.scrollIntoView({ block: 'nearest' });
  }, [active, found, listId]);

  // While the reader types, the message box gives the results its room.
  const typing = focus && typed.length > 0;
  const can = !opening && !sending && rows.length > 0;

  const guess = async (hit: SearchHit) => {
    if (opening || sending) return;
    if (guessed.has(hit.id)) {
      say(TOASTS.tried);
      return;
    }
    setSending(true);
    const taken = await onGuess(hit.id);
    setSending(false);
    if (taken) {
      setQ('');
      setFound(null);
      setAt(0);
    }
  };

  const submit = () => {
    const hit = rows[Math.min(at, rows.length - 1)];
    if (hit && can) void guess(hit);
  };

  const onKey = (e: KeyboardEvent<HTMLInputElement>) => {
    if (e.key === 'ArrowDown' || e.key === 'ArrowUp') {
      if (!rows.length) return;
      e.preventDefault();
      setAt((i) => (i + (e.key === 'ArrowDown' ? 1 : rows.length - 1)) % rows.length);
    } else if (e.key === 'Enter') {
      e.preventDefault();
      submit();
    } else if (e.key === 'Escape' && q) {
      // The field's own Escape: it clears what was typed, and goes no
      // further.
      e.stopPropagation();
      setQ('');
      setAt(0);
    }
  };

  return (
    <div className="cd-nd-bar">
      <div className="cd-nd-bar-in">
        {listed && (
          <HitList
            id={listId}
            rows={rows}
            held={options.held}
            note={options.note}
            at={at}
            guessed={guessed}
            theme={theme}
            onPick={(hit) => void guess(hit)}
          />
        )}
        {/* Always there, as the message's is, so the note is heard when
            it comes; it gives way to the results as the message does. */}
        <div className="cd-nd-tipwrap" role="status">
          {note && !typing && <HintStrip text={note} onClose={onNoteClose} />}
        </div>
        {/* Always there, so what it comes to say is heard; empty, it takes
            no room (grid.css). */}
        <div className="cd-nd-msgwrap" aria-live="polite">
          {message && !typing && <MessageBox message={message} codes={codes} theme={theme} />}
        </div>
        <div className="cd-nd-ask">
          <input
            ref={inputRef}
            className="cd-nd-input"
            type="text"
            role="combobox"
            aria-autocomplete="list"
            aria-expanded={listed}
            aria-controls={listId}
            aria-activedescendant={active != null ? `${listId}-${active}` : undefined}
            aria-label={GUESS_FIELD}
            placeholder={guessPlaceholder(game)}
            autoComplete="off"
            autoCorrect="off"
            spellCheck={false}
            enterKeyHint="go"
            value={q}
            onChange={(e) => {
              setQ(e.target.value);
              setAt(0);
            }}
            onKeyDown={onKey}
            onFocus={() => {
              window.clearTimeout(blur.current);
              setFocus(true);
            }}
            onBlur={() => {
              blur.current = window.setTimeout(() => setFocus(false), LIST_CLOSE_MS);
            }}
          />
          <button type="button" className="cd-nd-guess" disabled={!can} onClick={submit}>
            {guessLabel(game)}
          </button>
        </div>
      </div>
    </div>
  );
}

/** A first-time note over the message, with its close button, rising in
 *  as it comes (HINT_RISE). */
function HintStrip({ text, onClose }: { text: string; onClose?: () => void }) {
  const el = useRef<HTMLDivElement>(null);
  useEffect(() => {
    const a = animate(el.current, HINT_RISE.keyframes, HINT_RISE.options);
    return () => a?.cancel();
  }, []);
  return (
    <div ref={el} className="cd-nd-tip">
      <span className="cd-nd-tip-text">{text}</span>
      <button type="button" className="cd-nd-tip-x" aria-label={DISMISS} onClick={onClose}>
        <svg
          width="15"
          height="15"
          viewBox="0 0 24 24"
          fill="none"
          stroke="currentColor"
          strokeWidth="2.4"
          strokeLinecap="round"
          aria-hidden="true"
          focusable="false"
        >
          <path d="M6 6l12 12M18 6L6 18" />
        </svg>
      </button>
    </div>
  );
}

/** The results, above the field: up to six (four below 640px), each the
 *  movie's poster and title, the highlighted one on the accent's wash.
 *  `rows` can be chosen, by a press or by Enter on the highlighted one,
 *  and a press on one guesses it at once, as the design has it; `held`
 *  are the last answer's rows, faded and out of reach, while the next is
 *  on its way; `note` stands in for rows (guessOptions). */
export function HitList({
  id,
  rows,
  held,
  note,
  at,
  guessed,
  theme,
  onPick,
}: {
  id: string;
  rows: readonly SearchHit[];
  held: readonly SearchHit[];
  note: string;
  at: number;
  guessed: ReadonlySet<string>;
  theme: Theme;
  onPick: (hit: SearchHit) => void;
}) {
  const years = yearsShown(rows);
  const heldYears = yearsShown(held);
  return (
    <div id={id} className="cd-nd-hits" role="listbox" aria-label="Movies">
      {note && <div className="cd-nd-hits-note">{note}</div>}
      {rows.map((hit, i) => {
        const tried = guessed.has(hit.id);
        return (
          <div
            key={hit.id}
            id={`${id}-${i}`}
            role="option"
            aria-selected={i === at}
            className={`cd-nd-hit${i === at ? ' cd-nd-hit-at' : ''}${tried ? ' cd-nd-hit-tried' : ''}`}
            // On the press, so the field keeps the focus and the list is
            // still there to be pressed.
            onMouseDown={(e) => e.preventDefault()}
            onClick={() => onPick(hit)}
          >
            <ResultFace hit={hit} year={years[i]} tried={tried} theme={theme} />
          </div>
        );
      })}
      {/* The last answer's rows while the next is on its way: there, so the
          list does not collapse at every key, but faded and out of reach
          until they are replaced. */}
      {held.map((hit, i) => (
        <div
          key={hit.id}
          role="option"
          aria-selected={false}
          aria-disabled
          className="cd-nd-hit cd-nd-hit-held"
          onMouseDown={(e) => e.preventDefault()}
        >
          <ResultFace hit={hit} year={heldYears[i]} tried={guessed.has(hit.id)} theme={theme} />
        </div>
      ))}
    </div>
  );
}

/** A result: the movie's poster, its title, its year only where two
 *  results share the title (yearsShown), and "Tried" on one already
 *  guessed. */
function ResultFace({ hit, year, tried, theme }: { hit: SearchHit; year: boolean; tried: boolean; theme: Theme }) {
  return (
    <>
      <PosterImage
        id={hit.id}
        url={hit.poster}
        cssPx={24}
        className="cd-nd-hit-poster"
        style={{ ['--poster-fill' as string]: posterFallback(hit.title, theme) }}
      />
      <span className="cd-nd-hit-text">
        <span className="cd-nd-hit-title">{hit.title}</span>
        {year && hit.year ? <span className="cd-nd-hit-year">{hit.year}</span> : null}
      </span>
      {tried && <span className="cd-nd-hit-tag">Tried</span>}
    </>
  );
}

/** What a wrong guess told, in one row that scrolls sideways and never
 *  wraps: the title, never cut short; the warmth; the faces it shares,
 *  overlapping; then the decade, the genre and the cast compared. */
export function MessageBox({
  message,
  codes,
  theme,
}: {
  message: GuessMessage;
  codes: ReadonlyMap<string, string>;
  theme: Theme;
}) {
  return (
    <div className="cd-nd-msg">
      <span className="cd-nd-msg-title">{message.title}</span>
      <span className="cd-nd-warmth" style={toneStyle(warmthColour(message.warmth, theme))}>
        <span className="cd-nd-warmth-dot" aria-hidden="true" />
        {message.label}
      </span>
      {message.faces.length > 0 && (
        <span className="cd-nd-msg-faces">
          {message.faces.slice(0, MESSAGE_FACES).map((p) => (
            <DailyFace key={p.id} person={p} code={codes.get(p.id) ?? '?'} size="message" theme={theme} />
          ))}
        </span>
      )}
      {message.chips.map((c) => (
        <span key={c} className="cd-nd-msg-chip">
          {c}
        </span>
      ))}
    </div>
  );
}
