import {
  useEffect,
  useId,
  useLayoutEffect,
  useRef,
  useState,
  type KeyboardEvent,
  type RefObject,
} from 'react';
import {
  fetchDailyBoard,
  searchMovies,
  type DailyBoard,
  type DailyClue,
  type DailyGame,
  type DailyPerson,
  type DailyTab,
  type DailyToday,
  type SearchHit,
} from './api';
import {
  COUNT_UP_MS,
  DAILY_START,
  DAY_LETTERS,
  LIST_CLOSE_MS,
  LOW_POINTS,
  MAX_SUGGESTIONS,
  PLAY_AGAIN,
  RESULTS_IN_MS,
  ROLL_MS,
  SEARCH_WAIT_MS,
  SPEND_FLOAT_KEYFRAMES,
  SPEND_FLOAT_MS,
  avatarColours,
  boardLines,
  boardsWanted,
  boardNote,
  clueButtons,
  countdown,
  dropFailed,
  feedOf,
  fmtN,
  guessOptions,
  guessedIds,
  hintText,
  mmss,
  pointsShare,
  rankWidth,
  refusalText,
  resultSub,
  resultTitle,
  resultsDelay,
  secondsSince,
  shareLine,
  statsOf,
  weekdayOf,
  type BoardsSeen,
  type FeedChip,
  type FeedView,
  type GuessFound,
  type StatView,
} from './daily';
import { ratingText } from './grid';
import { EASE, animate, stillNow } from './motion';
import { personVars } from './personColour';
import { PersonFace } from './PersonFace';
import { PosterImage } from './PosterImage';
import { posterFallback } from './poster';
import type { Theme } from './theme';

/** A number that rolls to each new value, easing out, as the points
 *  total does when something is bought and the result's numbers do as
 *  they arrive. It starts at `from` when given, which is how a number
 *  counts up from nothing.
 *
 *  Drawn a frame at a time, but finished by a timer: a page nobody is
 *  painting gets no frames, and the number must still be right when the
 *  reader looks. Nothing rolls for a reader who has asked for stillness. */
export function useRolling(to: number, ms: number, from?: number): number {
  const [shown, setShown] = useState(from ?? to);
  const at = useRef(from ?? to);
  useEffect(() => {
    const start = at.current;
    if (start === to || ms <= 0 || stillNow() || typeof requestAnimationFrame !== 'function') {
      at.current = to;
      setShown(to);
      return;
    }
    const t0 = performance.now();
    let frame = 0;
    const step = (t: number) => {
      const k = Math.min(1, (t - t0) / ms);
      const v = Math.round(start + (to - start) * (1 - (1 - k) ** 3));
      at.current = v;
      setShown(v);
      if (k < 1) frame = requestAnimationFrame(step);
    };
    frame = requestAnimationFrame(step);
    const end = window.setTimeout(() => {
      cancelAnimationFrame(frame);
      at.current = to;
      setShown(to);
    }, ms + 50);
    return () => {
      cancelAnimationFrame(frame);
      window.clearTimeout(end);
    };
  }, [to, ms]);
  return shown;
}

/** Seconds on a clock that ticks once a second, on the server's time.
 *  Its own component, so the tick redraws one number and not the page. */
function Clock({ since, offset }: { since: string; offset: number }) {
  const [now, setNow] = useState(() => Date.now());
  useEffect(() => {
    const t = window.setInterval(() => setNow(Date.now()), 1000);
    return () => window.clearInterval(t);
  }, []);
  return (
    <span className="cd-daily-clock" aria-label="Time so far">
      {mmss(secondsSince(since, now, offset))}
    </span>
  );
}

/** "11:47:03" to the next map, ticking. */
function Countdown({ to, offset }: { to: string; offset: number }) {
  const [now, setNow] = useState(() => Date.now());
  useEffect(() => {
    const t = window.setInterval(() => setNow(Date.now()), 1000);
    return () => window.clearInterval(t);
  }, []);
  const at = Date.parse(to);
  return <>{countdown(Number.isFinite(at) ? at - (now + offset) : 0)}</>;
}

/** What was just spent, floating up off the total. Hidden unless it is
 *  played, so a reader who has asked for stillness sees the total change
 *  and nothing else. */
function Spent({ n }: { n: number }) {
  const el = useRef<HTMLSpanElement>(null);
  useLayoutEffect(() => {
    animate(el.current, SPEND_FLOAT_KEYFRAMES, { duration: SPEND_FLOAT_MS, easing: 'ease-out', fill: 'both' });
  }, []);
  return (
    <span ref={el} className="cd-daily-spent" aria-hidden="true">
      −{n}
    </span>
  );
}

interface PanelProps {
  today: DailyToday;
  game: DailyGame;
  /** The result has replaced the play panel. */
  results: boolean;
  min: boolean;
  onMin: () => void;
  /** The last thing spent, keyed so each spend floats on its own. */
  delta: { k: number; n: number } | null;
  /** The server's clock less this one's. */
  offset: number;
  touch: boolean;
  theme: Theme;
  hues: ReadonlyMap<string, number>;
  codes: ReadonlyMap<string, string>;
  panelRef: RefObject<HTMLElement | null>;
  inputRef: RefObject<HTMLInputElement | null>;
  /** The game has only just ended: the result's parts come in one after
   *  another, and its numbers count up. */
  fresh: boolean;
  player: string;
  onClue: (clue: DailyClue) => void;
  /** A guess at this movie. Whether it was taken, so the field can clear. */
  onGuess: (film: string) => Promise<boolean>;
  onReveal: () => void;
  onFind: (card: string) => void;
  onFindAnswer: () => void;
  onCopy: () => void;
  say: (text: string) => void;
  /** Play again, in development only: the reader starts again as a new
   *  player, on a newly picked movie. Absent from a server in production,
   *  and then nothing offers it. */
  onAgain?: () => void;
}

/** The game's panel: the points along the top, then either the feed and
 *  everything the reader can do, or, once the game is over, the result.
 *  A column on the right of a desktop or tablet, a sheet along the bottom
 *  of a phone. */
export function DailyPanel(p: PanelProps) {
  const { game, results, min } = p;
  const pts = useRolling(game.pts, ROLL_MS);
  const playing = game.phase === 'play';
  return (
    <section
      ref={p.panelRef}
      className={`cd-daily-panel${results ? ' cd-daily-panel-res' : ''}`}
      aria-label="Today’s game"
    >
      <div className="cd-daily-top">
        <span className="cd-daily-points">
          <span className="cd-daily-points-n">{fmtN(pts)}</span>
          <span className="cd-daily-points-word">points</span>
          {p.delta && <Spent key={p.delta.k} n={p.delta.n} />}
        </span>
        <span
          className="cd-daily-meter"
          role="meter"
          aria-label="Points left"
          aria-valuemin={0}
          aria-valuemax={DAILY_START}
          aria-valuenow={game.pts}
        >
          <span
            className={`cd-daily-meter-fill${game.pts < LOW_POINTS ? ' cd-daily-meter-low' : ''}`}
            style={{ width: `${pointsShare(game.pts)}%` }}
          />
        </span>
        {playing && <Clock since={game.startedAt} offset={p.offset} />}
        <button
          type="button"
          className="cd-daily-min"
          aria-label={min ? 'Show the panel' : 'Hide the panel'}
          aria-expanded={!min}
          onClick={p.onMin}
        >
          <svg
            className={`cd-daily-min-icon${min ? ' cd-daily-min-shut' : ''}`}
            width="16"
            height="16"
            viewBox="0 0 24 24"
            fill="none"
            stroke="currentColor"
            strokeWidth="2.2"
            strokeLinecap="round"
            strokeLinejoin="round"
            aria-hidden="true"
          >
            <path d="M6 9l6 6 6-6" />
          </svg>
        </button>
      </div>
      {results ? (
        <Results {...p} />
      ) : (
        <>
          {!min && <Feed {...p} />}
          {playing && <PlayFoot {...p} />}
        </>
      )}
    </section>
  );
}

/** The game so far, an entry a move, the latest at the bottom and washed
 *  in the accent. It is a live region, so each new entry is read out. */
function Feed({ today, game, theme, hues, codes, onFind }: PanelProps) {
  const entries = feedOf(today, game);
  const el = useRef<HTMLDivElement>(null);
  // Kept at the bottom as it grows, where the newest entry is.
  useLayoutEffect(() => {
    const box = el.current;
    if (box) box.scrollTop = box.scrollHeight;
  }, [entries.length]);
  return (
    <div ref={el} className="cd-daily-feed" aria-live="polite">
      {entries.map((e) => (
        <Entry key={e.key} e={e} theme={theme} hues={hues} codes={codes} onFind={onFind} />
      ))}
    </div>
  );
}

function Entry({
  e,
  theme,
  hues,
  codes,
  onFind,
}: {
  e: FeedView;
  theme: Theme;
  hues: ReadonlyMap<string, number>;
  codes: ReadonlyMap<string, string>;
  onFind: (card: string) => void;
}) {
  return (
    <div className={`cd-daily-entry cd-daily-entry-${e.tone}${e.now ? ' cd-daily-entry-now' : ''}`}>
      <span className="cd-daily-entry-label">
        <span>{e.label}</span>
        {e.cost && <span className="cd-daily-entry-cost">{e.cost}</span>}
      </span>
      {e.text && <span className="cd-daily-entry-text">{e.text}</span>}
      {e.faces.length > 0 && <Faces people={e.faces} theme={theme} hues={hues} codes={codes} />}
      {e.more && <span className="cd-daily-more">{e.more}</span>}
      {e.chips.length > 0 && (
        <div className="cd-daily-chips">
          {e.chips.map((c) => (
            <Chip key={c.card} chip={c} theme={theme} onFind={onFind} />
          ))}
        </div>
      )}
    </div>
  );
}

/** People named in an entry: their face, in their colour, and their name.
 *  Directors are rounded squares, the cast round, as on the cards. */
function Faces({
  people,
  theme,
  hues,
  codes,
}: {
  people: DailyPerson[];
  theme: Theme;
  hues: ReadonlyMap<string, number>;
  codes: ReadonlyMap<string, string>;
}) {
  return (
    <div className="cd-daily-faces">
      {people.map((person) => (
        <span key={person.id} className="cd-daily-face" style={personVars(person, theme, hues)}>
          <PersonFace
            photo={person.photo}
            code={codes.get(person.id) ?? '?'}
            size="preview"
            square={person.role === 'director'}
          />
          <span className="cd-daily-face-name">{person.name}</span>
        </span>
      ))}
    </div>
  );
}

/** A movie on the board named in the feed: pressed, the map goes to its
 *  card and rings it. */
function Chip({ chip, theme, onFind }: { chip: FeedChip; theme: Theme; onFind: (card: string) => void }) {
  return (
    <button type="button" className="cd-daily-chip" aria-label={chip.aria} onClick={() => onFind(chip.card)}>
      {chip.film ? (
        <PosterImage
          id={chip.film.id}
          url={chip.film.poster}
          cssPx={16}
          className="cd-daily-chip-thumb"
          style={{ ['--poster-fill' as string]: posterFallback(chip.film.title, theme) }}
        />
      ) : (
        <span className="cd-daily-chip-thumb cd-daily-chip-kin" aria-hidden="true" />
      )}
      <span className="cd-daily-chip-title">{chip.title}</span>
    </button>
  );
}

/** Everything the reader can do while the game is on: buy a clue, guess,
 *  or give up. It stays when the panel is folded away; only the feed goes. */
function PlayFoot(p: PanelProps) {
  const buttons = clueButtons(p.today, p.game);
  return (
    <div className="cd-daily-foot">
      <div className="cd-daily-clues" role="group" aria-label="Clues to buy">
        {buttons.map((b) => (
          <button
            key={b.clue}
            type="button"
            className="cd-daily-clue"
            disabled={b.off}
            aria-label={b.aria}
            onClick={() => p.onClue(b.clue)}
          >
            <span className="cd-daily-clue-label">{b.label}</span>
            <span className={`cd-daily-tag${b.priced ? '' : ' cd-daily-tag-muted'}`}>{b.tag}</span>
          </button>
        ))}
      </div>
      <GuessField
        guessed={guessedIds(p.game)}
        onGuess={p.onGuess}
        say={p.say}
        inputRef={p.inputRef}
        theme={p.theme}
      />
      <div className={`cd-daily-hintrow${p.onAgain ? ' cd-daily-hintrow-dev' : ''}`}>
        <span className="cd-daily-hint">{hintText(p.touch, p.game.nextCost)}</span>
        {/* Beside the way out, so starting again never takes giving the
            answer away first. */}
        {p.onAgain && <AgainButton onAgain={p.onAgain} />}
        <button type="button" className="cd-daily-reveal" onClick={p.onReveal}>
          Show the answer
        </button>
      </div>
    </div>
  );
}

/** Naming the movie. The catalog's own search, as the header's: a pause
 *  in the typing asks, and a newer question calls off the one before.
 *  Guessing takes two steps, so a slip of the finger is never a wrong
 *  guess: choose a row, which fills the field, then press Guess. Movies
 *  already guessed are marked, and cannot be chosen. Unlike the header's,
 *  only rows found for exactly what is typed can be chosen (guessOptions):
 *  a row left from words the reader has typed past is a guess they never
 *  meant, and here a wrong guess costs points. */
function GuessField({
  guessed,
  onGuess,
  say,
  inputRef,
  theme,
}: {
  guessed: ReadonlySet<string>;
  onGuess: (film: string) => Promise<boolean>;
  say: (text: string) => void;
  inputRef: RefObject<HTMLInputElement | null>;
  theme: Theme;
}) {
  const listId = useId();
  const [q, setQ] = useState('');
  // Kept with the words it answers, so rows are never offered under
  // words they were not found for.
  const [found, setFound] = useState<GuessFound<SearchHit> | null>(null);
  const [at, setAt] = useState(0);
  const [pick, setPick] = useState<SearchHit | null>(null);
  const [focus, setFocus] = useState(false);
  const [sending, setSending] = useState(false);
  const blur = useRef(0);
  const typed = q.trim();
  // The catalog answers from two characters.
  const asking = typed.length >= 2 && !pick;

  useEffect(() => {
    if (!asking) return;
    const ctrl = new AbortController();
    const t = window.setTimeout(() => {
      searchMovies(typed, ctrl.signal)
        .then((hits) => {
          if (ctrl.signal.aborted) return;
          setFound({ q: typed, hits: (hits ?? []).slice(0, MAX_SUGGESTIONS), failed: false });
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
  }, [typed, asking]);

  useEffect(() => () => window.clearTimeout(blur.current), []);

  const shown = focus && asking;
  const options = guessOptions(typed, found);
  // What Enter, the arrows and the field's active row go by: only rows
  // found for what is typed.
  const list = shown ? options.rows : [];

  const choose = (hit: SearchHit) => {
    setPick(hit);
    setQ(hit.title);
    setAt(0);
  };

  const submit = async () => {
    if (!pick || sending) return;
    setSending(true);
    const taken = await onGuess(pick.id);
    setSending(false);
    // A guess that never got an answer stays chosen, so trying again is
    // one press.
    if (taken) {
      setPick(null);
      setQ('');
      setFound(null);
    }
  };

  const onKey = (e: KeyboardEvent<HTMLInputElement>) => {
    if (e.key === 'ArrowDown' || e.key === 'ArrowUp') {
      if (!list.length) return;
      e.preventDefault();
      setAt((i) => (i + (e.key === 'ArrowDown' ? 1 : list.length - 1)) % list.length);
    } else if (e.key === 'Enter') {
      e.preventDefault();
      if (pick) {
        void submit();
        return;
      }
      const hit = list[at];
      if (!hit) return;
      if (guessed.has(hit.id)) say(refusalText('known', 'guess'));
      else choose(hit);
    } else if (e.key === 'Escape' && q) {
      // The field's own Escape: it clears what was typed, and goes no
      // further.
      e.stopPropagation();
      setQ('');
      setPick(null);
    }
  };

  return (
    <div className="cd-daily-ask">
      {shown && (
        <div id={listId} className="cd-daily-options" role="listbox" aria-label="Movies">
          {options.note && <div className="cd-daily-options-note">{options.note}</div>}
          {list.map((hit, i) => {
            const used = guessed.has(hit.id);
            return (
              <button
                key={hit.id}
                type="button"
                id={`${listId}-${i}`}
                role="option"
                tabIndex={-1}
                aria-selected={i === at}
                aria-disabled={used || undefined}
                className={`cd-daily-option${i === at ? ' cd-daily-option-at' : ''}${used ? ' cd-daily-option-used' : ''}`}
                // On the press rather than the click, so the field keeps
                // the focus and the list is still there to be pressed.
                onMouseDown={(e) => {
                  e.preventDefault();
                  if (!used) choose(hit);
                }}
                onClick={() => {
                  if (!used) choose(hit);
                }}
              >
                <OptionFace hit={hit} used={used} theme={theme} />
              </button>
            );
          })}
          {/* The last answer's rows while the next is on its way: there,
              so the list does not collapse at every key, but faded and
              out of reach until they are replaced. */}
          {options.held.map((hit) => (
            <div
              key={hit.id}
              role="option"
              aria-selected={false}
              aria-disabled
              className="cd-daily-option cd-daily-option-held"
              // A press on one keeps the focus in the field, so the list
              // is still there when the answer comes.
              onMouseDown={(e) => e.preventDefault()}
            >
              <OptionFace hit={hit} used={guessed.has(hit.id)} theme={theme} />
            </div>
          ))}
        </div>
      )}
      <div className="cd-daily-askrow">
        <label className="cd-daily-field">
          <svg
            className="cd-daily-field-icon"
            width="15"
            height="15"
            viewBox="0 0 24 24"
            fill="none"
            stroke="currentColor"
            strokeWidth="2"
            aria-hidden="true"
          >
            <circle cx="11" cy="11" r="7" />
            <path d="M21 21l-4.3-4.3" />
          </svg>
          <input
            ref={inputRef}
            className="cd-daily-input"
            type="text"
            role="combobox"
            aria-autocomplete="list"
            aria-expanded={shown}
            aria-controls={listId}
            aria-activedescendant={list.length ? `${listId}-${at}` : undefined}
            aria-label="Name the movie"
            placeholder="Name the movie"
            autoComplete="off"
            spellCheck={false}
            enterKeyHint="go"
            value={q}
            onChange={(e) => {
              setQ(e.target.value);
              setPick(null);
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
        </label>
        <button type="button" className="cd-daily-guess" disabled={!pick || sending} onClick={() => void submit()}>
          Guess
        </button>
      </div>
    </div>
  );
}

/** What a row of the guess list shows: the movie's poster, its title and
 *  year, and "Guessed" for one already guessed. */
function OptionFace({ hit, used, theme }: { hit: SearchHit; used: boolean; theme: Theme }) {
  return (
    <>
      <PosterImage
        id={hit.id}
        url={hit.poster}
        cssPx={24}
        className="cd-daily-option-poster"
        style={{ ['--poster-fill' as string]: posterFallback(hit.title, theme) }}
      />
      <span className="cd-daily-option-title">
        {hit.title}
        {hit.year ? <span className="cd-daily-option-year"> ({hit.year})</span> : null}
      </span>
      {used && <span className="cd-daily-option-tag">Guessed</span>}
    </>
  );
}

/** A result's number, counting up from nothing the first time it shows
 *  on a fresh result, and simply there on one come back to. A dash while
 *  the board it comes from is on its way. */
function Stat({ stat, animateIn }: { stat: StatView; animateIn: boolean }) {
  const target = stat.value ?? 0;
  const n = useRolling(target, animateIn ? COUNT_UP_MS : 0, animateIn ? 0 : target);
  return (
    <div className="cd-daily-stat">
      <span className="cd-daily-stat-n">{stat.value == null ? '–' : `${fmtN(n)}${stat.suffix}`}</span>
      <span className="cd-daily-stat-caption">{stat.caption}</span>
    </div>
  );
}

/** The result: the score and what it took, the answer, two numbers, the
 *  card to share, and the leaderboards. */
function Results(p: PanelProps) {
  const { today, game, min, theme } = p;
  const [tab, setTab] = useState<DailyTab>('today');
  const [boards, setBoards] = useState<BoardsSeen<DailyBoard>>({});
  const box = useRef<HTMLDivElement>(null);
  const tabsId = useId();

  // Every board the tab wants and does not have is asked for. One that
  // failed counts as had, so it is not asked for again and again on its
  // own; picking a tab, or "Try again", forgets the failure, and that
  // asks again.
  const want = boardsWanted(tab);
  const wantKey = want.filter((t) => !boards[t]).join(',');
  useEffect(() => {
    if (!wantKey) return;
    const ctrl = new AbortController();
    for (const t of wantKey.split(',') as DailyTab[]) {
      fetchDailyBoard(today.no, t, ctrl.signal)
        .then((b) => {
          if (!ctrl.signal.aborted) setBoards((was) => ({ ...was, [t]: b }));
        })
        .catch(() => {
          if (!ctrl.signal.aborted) setBoards((was) => ({ ...was, [t]: 'failed' }));
        });
    }
    return () => ctrl.abort();
  }, [wantKey, today.no]);

  // On a fresh result its parts come in one after another. Only then:
  // folding the panel away and back does not play it again.
  const freshAtMount = useRef(p.fresh);
  useLayoutEffect(() => {
    if (!freshAtMount.current || !box.current) return;
    [...box.current.children].forEach((part, i) =>
      animate(
        part,
        [
          { opacity: 0, transform: 'translateY(14px)' },
          { opacity: 1, transform: 'none' },
        ],
        { duration: RESULTS_IN_MS, delay: resultsDelay(i), easing: EASE.settle, fill: 'backwards' },
      ),
    );
  }, []);

  const todayBoard = boards.today && boards.today !== 'failed' ? boards.today : null;
  const stats = statsOf(game, todayBoard, today.streak);
  const answer = game.end?.answer;
  const shown = boards[tab];
  const board = shown && shown !== 'failed' ? shown : null;
  const days = weekdayOf(today.date) + 1;
  const lines = board ? boardLines(board, p.player, days) : [];
  const listed = board?.you?.listed ?? true;
  const rankW = rankWidth(lines);

  const pickTab = (next: DailyTab) => {
    setTab(next);
    setBoards((b) => dropFailed(b, boardsWanted(next)));
  };
  const retry = () => setBoards((b) => dropFailed(b, want));

  const onTabKey = (e: KeyboardEvent<HTMLButtonElement>) => {
    if (e.key !== 'ArrowLeft' && e.key !== 'ArrowRight') return;
    e.preventDefault();
    const next: DailyTab = tab === 'today' ? 'week' : 'today';
    pickTab(next);
    document.getElementById(`${tabsId}-${next}`)?.focus();
  };

  return (
    <>
      {!min && (
        <div ref={box} className="cd-daily-res">
          <div className="cd-daily-res-head">
            <h2 className="cd-daily-res-title">{resultTitle(game)}</h2>
            <p className="cd-daily-res-sub">{resultSub(game)}</p>
          </div>
          {answer && (
            <button
              type="button"
              className="cd-daily-res-answer"
              aria-label={`Show ${answer.title} on the map`}
              onClick={p.onFindAnswer}
            >
              <PosterImage
                id={answer.id}
                url={answer.poster}
                cssPx={36}
                className="cd-daily-res-poster"
                style={{ ['--poster-fill' as string]: posterFallback(answer.title, theme) }}
              />
              <span className="cd-daily-res-film">
                <span className="cd-daily-res-kicker">Today’s movie</span>
                <span className="cd-daily-res-name">{answer.title}</span>
                <span className="cd-daily-res-meta">
                  {answer.year} · rated {ratingText(answer.rating)}
                </span>
              </span>
            </button>
          )}
          <div className="cd-daily-stats">
            {stats.map((s, i) => (
              <Stat key={i} stat={s} animateIn={p.fresh} />
            ))}
          </div>
          <div className="cd-daily-share">
            <div className="cd-daily-share-text">
              <span className="cd-daily-share-title">Cinedikt Daily No. {today.no}</span>
              <span className="cd-daily-bar" aria-hidden="true">
                <span className="cd-daily-bar-fill" style={{ width: `${pointsShare(game.pts)}%` }} />
              </span>
              <span className="cd-daily-share-line">{shareLine(game)}</span>
            </div>
            <button type="button" className="cd-daily-soft" onClick={p.onCopy}>
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
                <rect x="9" y="9" width="11" height="11" rx="2.5" />
                <path d="M5 15V6.5A2.5 2.5 0 0 1 7.5 4H15" />
              </svg>
              Copy
            </button>
          </div>
          <div className="cd-daily-lb">
            <div className="cd-daily-lb-head">
              <h3 className="cd-daily-lb-title">Leaderboard</h3>
              <span className="cd-daily-spacer" />
              <div className="cd-daily-tabs" role="tablist" aria-label="Leaderboard">
                {(
                  [
                    ['today', 'Today'],
                    ['week', 'This week'],
                  ] as const
                ).map(([k, label]) => (
                  <button
                    key={k}
                    id={`${tabsId}-${k}`}
                    type="button"
                    role="tab"
                    aria-selected={tab === k}
                    aria-controls={`${tabsId}-list`}
                    tabIndex={tab === k ? 0 : -1}
                    className={`cd-daily-tab${tab === k ? ' cd-daily-tab-on' : ''}`}
                    onClick={() => pickTab(k)}
                    onKeyDown={onTabKey}
                  >
                    {label}
                  </button>
                ))}
              </div>
            </div>
            {tab === 'week' && board && (
              <div className="cd-daily-week-head" aria-hidden="true">
                <span className="cd-daily-spacer" />
                <span className="cd-daily-week-days">
                  {DAY_LETTERS.slice(0, days).map((d, i) => (
                    <span key={i} className="cd-daily-week-day">
                      {d}
                    </span>
                  ))}
                </span>
                <span className="cd-daily-week-points">Points</span>
              </div>
            )}
            <ol
              id={`${tabsId}-list`}
              className={`cd-daily-rows${tab === 'week' ? ' cd-daily-rows-week' : ''}`}
              role="tabpanel"
              aria-labelledby={`${tabsId}-${tab}`}
            >
              {lines.map((l) =>
                l.gap ? (
                  <li key={l.key} className="cd-daily-gap" aria-hidden="true">
                    ···
                  </li>
                ) : (
                  <li key={l.key} className={`cd-daily-row${l.you ? ' cd-daily-row-you' : ''}`}>
                    <span className="cd-daily-rank" style={{ minWidth: rankW }}>
                      {l.rank}
                    </span>
                    {tab === 'today' && (
                      <span
                        className={`cd-daily-avatar${l.you ? ' cd-daily-avatar-you' : ''}`}
                        style={l.you ? undefined : avatarVars(l.hue, theme)}
                        aria-hidden="true"
                      >
                        {l.code}
                      </span>
                    )}
                    <span className="cd-daily-who">
                      <span className="cd-daily-who-name">{l.name}</span>
                      {l.sub && <span className="cd-daily-who-sub">{l.sub}</span>}
                    </span>
                    {l.cells && (
                      <span className="cd-daily-cells">
                        {l.cells.map((c, i) => (
                          <span
                            key={i}
                            className={`cd-daily-cell${c.played ? '' : ' cd-daily-cell-empty'}`}
                            title={c.tip}
                          >
                            {c.h > 0 && <span className="cd-daily-cell-fill" style={{ height: `${c.h}%` }} />}
                            {c.missed && <span className="cd-daily-cell-x">✕</span>}
                          </span>
                        ))}
                      </span>
                    )}
                    <span className={`cd-daily-value${l.missed ? ' cd-daily-value-missed' : ''}`}>{l.value}</span>
                  </li>
                ),
              )}
            </ol>
            {board ? (
              <p className="cd-daily-note">{boardNote(tab, board.total, p.player, listed)}</p>
            ) : shown === 'failed' ? (
              <div className="cd-daily-retry">
                <p className="cd-daily-note">The leaderboard didn’t load.</p>
                <button type="button" className="cd-daily-soft" onClick={retry}>
                  Try again
                </button>
              </div>
            ) : (
              <p className="cd-daily-note">Loading the leaderboard…</p>
            )}
          </div>
          {/* Last in the result, where the prototype had its own. */}
          {p.onAgain && <AgainButton onAgain={p.onAgain} />}
        </div>
      )}
      <div className="cd-daily-res-foot">
        <span className="cd-daily-next">
          Next map in <Countdown to={today.next} offset={p.offset} />
        </span>
        <span className="cd-daily-spacer" />
        <button type="button" className="cd-daily-soft cd-daily-soft-wide" onClick={p.onMin}>
          {min ? 'Show results' : 'Explore the map'}
        </button>
      </div>
    </>
  );
}

/** Play again, in development only, at the foot of the result and
 *  beside "Show the answer" while the game is on: quieter than anything
 *  near it, and saying what it is. */
function AgainButton({ onAgain }: { onAgain: () => void }) {
  return (
    <button type="button" className="cd-daily-again" onClick={onAgain}>
      {PLAY_AGAIN}
    </button>
  );
}

/** A leaderboard avatar's colours, as custom properties the stylesheet
 *  reads. */
function avatarVars(hue: number, theme: Theme) {
  const c = avatarColours(hue, theme);
  return { ['--tone' as string]: c.fill, ['--tone-ink' as string]: c.ink };
}
