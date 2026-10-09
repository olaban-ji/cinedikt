import { useEffect, useId, useState, type KeyboardEvent, type MouseEvent } from 'react';
import { fetchDailyBoard, type DailyBoard, type DailyGame, type DailyTab, type DailyToday } from './api';
import {
  BOARD_TABS,
  CHART_LABELS,
  NEXT_MOVIE_IN,
  PLAY_AGAIN,
  SCORE_COUNT_MS,
  betterLine,
  boardNote,
  boardRows,
  boardsWanted,
  chartBars,
  countUp,
  countdown,
  dropFailed,
  fmtN,
  namesLine,
  paidFor,
  resultFaces,
  resultKicker,
  streakPill,
  weekLetters,
  type BoardsSeen,
} from './daily';
import { DailyFace } from './DailyFace';
import { filmPath } from './movieParam';
import { stillNow } from './motion';
import type { Theme } from './theme';

// The end of the game: the result card and the leaderboard, between About
// the movie and the cast.

/** The score as the result draws it: counting up from nought, easing out,
 *  over SCORE_COUNT_MS when the game has just ended on the page, and simply
 *  there for a game opened already over, or for a reader who has asked for
 *  stillness. Drawn a frame at a time, but finished by a timer: a page
 *  nobody is painting gets no frames, and the number must still be right
 *  when the reader looks. */
export function useCountUp(score: number, play: boolean): number {
  const [shown, setShown] = useState(() => (play && !stillNow() ? 0 : score));
  useEffect(() => {
    if (!play || stillNow() || typeof requestAnimationFrame !== 'function') {
      setShown(score);
      return;
    }
    const t0 = performance.now();
    let frame = 0;
    const step = (t: number) => {
      setShown(countUp(score, t - t0));
      if (t - t0 < SCORE_COUNT_MS) frame = requestAnimationFrame(step);
    };
    frame = requestAnimationFrame(step);
    const end = window.setTimeout(() => {
      cancelAnimationFrame(frame);
      setShown(score);
    }, SCORE_COUNT_MS + 50);
    return () => {
      cancelAnimationFrame(frame);
      window.clearTimeout(end);
    };
  }, [score, play]);
  return shown;
}

/** "11:47:03" to the reader's midnight, on the server's clock, ticking.
 *  Its own component, so the tick redraws one number and not the page. */
function Countdown({ to, offset }: { to: string; offset: number }) {
  const [now, setNow] = useState(() => Date.now());
  useEffect(() => {
    const t = window.setInterval(() => setNow(Date.now()), 1000);
    return () => window.clearInterval(t);
  }, []);
  const at = Date.parse(to);
  return <>{countdown(Number.isFinite(at) ? at - (now + offset) : 0)}</>;
}

/** Play again, in development only, at the foot of the result and beside
 *  Show the answer while the game is on: quieter than anything near it,
 *  and saying what it is. */
export function AgainButton({ onAgain }: { onAgain: () => void }) {
  return (
    <button type="button" className="cd-nd-again" onClick={onAgain}>
      {PLAY_AGAIN}
    </button>
  );
}

/** A click the browser keeps: one with a modifier held, or not with the
 *  main button, opens the link as the reader asked rather than here. */
function browserKeeps(e: MouseEvent<HTMLAnchorElement>): boolean {
  return e.metaKey || e.ctrlKey || e.shiftKey || e.altKey || e.button !== 0;
}

interface ResultProps {
  today: Pick<DailyToday, 'no' | 'next' | 'streak'>;
  game: DailyGame;
  /** Today's leaderboard, for the figure and the chart; null while it is
   *  on its way, or when it could not be had. */
  board: DailyBoard | null;
  /** The game has just ended on the page: the score counts up. */
  fresh: boolean;
  /** The server's clock less this one's. */
  offset: number;
  codes: ReadonlyMap<string, string>;
  theme: Theme;
  onShare: () => void;
  /** Map this movie: the app's own way to a movie's map. */
  onOpenMovie?: (id: string, title: string) => void;
  /** Play again, offered only by a server in development. */
  onAgain?: () => void;
}

/** The result: how it ended and the score; the streak; the names it took,
 *  as six faces; what was paid for; how today's players did, with the
 *  scores chart; and Share result, Map this movie and the countdown to the
 *  next movie. */
export function DailyResult({ today, game, board, fresh, offset, codes, theme, onShare, onOpenMovie, onAgain }: ResultProps) {
  const score = game.won ? game.pts : 0;
  const shown = useCountUp(score, fresh);
  const pill = streakPill(today.streak, game);
  const paid = paidFor(game.log);
  const better = betterLine(game, board);
  const bars = chartBars(board?.chart, game);
  const answer = game.end?.answer;
  return (
    <section className="cd-nd-res" aria-label="Your result">
      <div className="cd-nd-res-top">
        <div className="cd-nd-res-head">
          <span className="cd-nd-res-kicker">{resultKicker(game)}</span>
          <span className="cd-nd-res-score">
            {/* The count is for the eye; a screen reader hears the score. */}
            <span className="cd-nd-res-n" aria-hidden="true">
              {fmtN(shown)}
            </span>
            <span className="cd-sr-live">{fmtN(score)}</span>
            <span className="cd-nd-res-word">points</span>
          </span>
        </div>
        {pill && (
          <span className="cd-nd-streak">
            <svg width="13" height="13" viewBox="0 0 24 24" fill="currentColor" aria-hidden="true">
              <path d="M12 2c1 3.5-1.5 5.2-1.5 7.8 0 1.4 1 2.4 2.2 2.4 1.5 0 2.3-1.2 2.1-3 2.3 1.6 3.7 4.2 3.7 6.8A6.5 6.5 0 0 1 12 22.5 6.5 6.5 0 0 1 5.5 16c0-4.6 4-7.3 6.5-14z" />
            </svg>
            {pill}
          </span>
        )}
      </div>
      <div className="cd-nd-res-names">
        <span className="cd-nd-res-line">{namesLine(game)}</span>
        <div className="cd-nd-res-faces">
          {resultFaces(game).map(({ person, seen }) => (
            <span key={person.id} className="cd-nd-res-face" title={person.name}>
              <DailyFace person={person} code={codes.get(person.id) ?? '?'} size="result" theme={theme} dim={!seen} />
            </span>
          ))}
        </div>
        {paid.length > 0 && (
          <div className="cd-nd-paid">
            {paid.map((c) => (
              <span key={c.label} className="cd-nd-paid-chip">
                {c.label}
                <span className="cd-nd-paid-cost">{c.price}</span>
              </span>
            ))}
          </div>
        )}
      </div>
      <div className="cd-nd-res-crowd">
        <p className="cd-nd-better">
          {better ? (
            <>
              <span className="cd-nd-better-n">{better.figure}</span>
              {better.rest}
            </>
          ) : (
            // Held open while today's board is on its way, so the chart
            // does not jump when the line arrives.
            ' '
          )}
        </p>
        <div className="cd-nd-chart" aria-hidden="true">
          {bars.map((b, i) => (
            <span
              key={i}
              className={`cd-nd-chart-bar${b.you ? (game.won ? ' cd-nd-chart-you' : ' cd-nd-chart-miss') : ''}`}
              style={{ height: `${b.h}%` }}
            />
          ))}
        </div>
        <div className="cd-nd-chart-labels" aria-hidden="true">
          {CHART_LABELS.map((l) => (
            <span key={l}>{l}</span>
          ))}
        </div>
      </div>
      <div className="cd-nd-res-actions">
        <button type="button" className="cd-nd-share" onClick={onShare}>
          <svg
            width="17"
            height="17"
            viewBox="0 0 24 24"
            fill="none"
            stroke="currentColor"
            strokeWidth="2.2"
            strokeLinecap="round"
            strokeLinejoin="round"
            aria-hidden="true"
          >
            <path d="M4 12v7a1 1 0 0 0 1 1h14a1 1 0 0 0 1-1v-7" />
            <path d="M12 3v12" />
            <path d="M7 8l5-5 5 5" />
          </svg>
          Share result
        </button>
        {answer && (
          <a
            className="cd-nd-mapit"
            href={filmPath(answer.id, answer.title)}
            onClick={(e) => {
              if (!onOpenMovie || browserKeeps(e)) return;
              e.preventDefault();
              onOpenMovie(answer.id, answer.title);
            }}
          >
            Map this movie
          </a>
        )}
        {/* At the right, on its own line on a narrow phone too. */}
        <span className="cd-nd-countdown">
          <span className="cd-nd-countdown-label">{NEXT_MOVIE_IN}</span>
          <span className="cd-nd-countdown-n">
            <Countdown to={today.next} offset={offset} />
          </span>
        </span>
      </div>
      {/* Last in the result, where the prototype has its own. */}
      {onAgain && <AgainButton onAgain={onAgain} />}
    </section>
  );
}

/** The leaderboards fetched so far, by tab, and the tab showing. Every
 *  board the tab wants and does not have is asked for; today's whichever
 *  tab is showing, since the result's figure and chart come from it. One
 *  that failed counts as had, so it is not asked for again and again on
 *  its own; picking a tab, or Try again, forgets the failure, and that
 *  asks again. Nothing is asked for until `on`, the game being over. */
export function useBoards(no: number, on: boolean) {
  const [tab, setTab] = useState<DailyTab>('today');
  const [boards, setBoards] = useState<BoardsSeen<DailyBoard>>({});
  const want = boardsWanted(tab);
  const wantKey = on ? want.filter((t) => !boards[t]).join(',') : '';
  useEffect(() => {
    if (!wantKey) return;
    const ctrl = new AbortController();
    for (const t of wantKey.split(',') as DailyTab[]) {
      fetchDailyBoard(no, t, ctrl.signal)
        .then((b) => {
          if (!ctrl.signal.aborted) setBoards((was) => ({ ...was, [t]: b }));
        })
        .catch(() => {
          if (!ctrl.signal.aborted) setBoards((was) => ({ ...was, [t]: 'failed' }));
        });
    }
    return () => ctrl.abort();
  }, [wantKey, no]);
  return {
    tab,
    boards,
    pick: (next: DailyTab) => {
      setTab(next);
      setBoards((b) => dropFailed(b, boardsWanted(next)));
    },
    retry: () => setBoards((b) => dropFailed(b, want)),
  };
}

/** The leaderboard: Today and This week, each the reader and the players
 *  nearest them, never a top list, which would be the only reason to look
 *  an answer up. Equal scores share a place ("=7,804"). This week's rows
 *  carry a cell for each day so far, under day initials. */
export function DailyLeaderboard({
  date,
  tab,
  boards,
  onTab,
  onRetry,
}: {
  /** The puzzle's date, for the week's day initials. */
  date: string;
  tab: DailyTab;
  boards: BoardsSeen<DailyBoard>;
  onTab: (tab: DailyTab) => void;
  onRetry: () => void;
}) {
  const ids = useId();
  const seen = boards[tab];
  const board = seen && seen !== 'failed' ? seen : null;
  const rows = board ? boardRows(board) : [];
  const onTabKey = (e: KeyboardEvent<HTMLButtonElement>) => {
    if (e.key !== 'ArrowLeft' && e.key !== 'ArrowRight') return;
    e.preventDefault();
    const next: DailyTab = tab === 'today' ? 'week' : 'today';
    onTab(next);
    document.getElementById(`${ids}-${next}`)?.focus();
  };
  return (
    <section className="cd-nd-board" aria-labelledby={`${ids}-title`}>
      <div className="cd-nd-board-head">
        <h2 id={`${ids}-title`} className="cd-nd-board-title">
          Leaderboard
        </h2>
        <div className="cd-nd-tabs" role="tablist" aria-label="Leaderboard">
          {BOARD_TABS.map(({ tab: t, label }) => (
            <button
              key={t}
              id={`${ids}-${t}`}
              type="button"
              role="tab"
              aria-selected={tab === t}
              aria-controls={`${ids}-panel`}
              tabIndex={tab === t ? 0 : -1}
              className={`cd-nd-tab${tab === t ? ' cd-nd-tab-on' : ''}`}
              onClick={() => onTab(t)}
              onKeyDown={onTabKey}
            >
              {label}
            </button>
          ))}
        </div>
      </div>
      <div id={`${ids}-panel`} className="cd-nd-board-panel" role="tabpanel" aria-labelledby={`${ids}-${tab}`}>
        {tab === 'week' && rows.length > 0 && (
          <div className="cd-nd-board-days" aria-hidden="true">
            {weekLetters(date).map((d, i) => (
              <span key={i} className="cd-nd-board-day">
                {d}
              </span>
            ))}
          </div>
        )}
        {rows.length > 0 && (
          <ol className="cd-nd-board-rows">
            {rows.map((r) => (
              <li key={r.key} className={`cd-nd-board-row${r.you ? ' cd-nd-board-you' : ''}`}>
                <span className="cd-nd-place">{r.place}</span>
                <span className="cd-nd-board-who">
                  <span className="cd-nd-board-name">{r.name}</span>
                  {r.days && (
                    <span className="cd-nd-days">
                      {r.days.map((d, i) => (
                        <span key={i} className={`cd-nd-day${d.zero ? ' cd-nd-day-zero' : ''}`}>
                          {d.v}
                        </span>
                      ))}
                    </span>
                  )}
                </span>
                <span className="cd-nd-board-pts">{r.pts}</span>
              </li>
            ))}
          </ol>
        )}
        {board ? (
          <p className="cd-nd-board-note">{boardNote(tab, board.total)}</p>
        ) : seen === 'failed' ? (
          <div className="cd-nd-board-retry">
            <p className="cd-nd-board-note">The leaderboard didn’t load.</p>
            <button type="button" className="cd-nd-board-again" onClick={onRetry}>
              Try again
            </button>
          </div>
        ) : (
          <p className="cd-nd-board-note">Loading the leaderboard…</p>
        )}
      </div>
    </section>
  );
}
