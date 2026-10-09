import type { DailyToday } from './api';
import { TITLE_HUE, TITLE_SCREEN, cardColour, dailyDayText, hueColour, playedText, toneStyle } from './daily';
import type { Theme } from './theme';

/** Name Drop's title screen, before Play: the game in one picture — the
 *  hidden card in today's poster colour beside three rows of a cast list,
 *  one shown, one next, one hidden — then the pill and the day, the name,
 *  what it asks, three steps, the fine print, Play, and how many have
 *  played. Every number in its words is the rules' (TITLE_SCREEN). The
 *  picture is for the eye, and hidden from a screen reader, which hears
 *  the steps as a list. */
export function DailyTitle({
  today,
  busy,
  theme,
  onPlay,
}: {
  today: DailyToday;
  /** Play has been pressed and the server has not answered yet. */
  busy: boolean;
  theme: Theme;
  onPlay: () => void;
}) {
  const day = dailyDayText(today.date);
  const played = playedText(today.played);
  return (
    <div className="cd-nd-title">
      <div className="cd-nd-title-col">
        <div className="cd-nd-title-pic" aria-hidden="true">
          <span className="cd-nd-title-card" style={toneStyle(cardColour(today.colour))}>
            ?
          </span>
          <span className="cd-nd-title-rows">
            <span className="cd-nd-title-row cd-nd-title-row-shown">
              <span className="cd-nd-title-face" style={toneStyle(hueColour(TITLE_HUE, theme))} />
              <span className="cd-nd-title-bar" />
            </span>
            <span className="cd-nd-title-row cd-nd-title-row-next">
              <span className="cd-nd-title-face" />
              <span className="cd-nd-title-bar" />
            </span>
            <span className="cd-nd-title-row cd-nd-title-row-hidden">
              <span className="cd-nd-title-face" />
              <span className="cd-nd-title-bar" />
            </span>
          </span>
        </div>
        <div className="cd-nd-title-meta">
          <span className="cd-daily-pill">{TITLE_SCREEN.pill}</span>
          {day && <span className="cd-nd-title-day">{day}</span>}
        </div>
        <div className="cd-nd-title-words">
          <h1 className="cd-nd-title-heading">{TITLE_SCREEN.heading}</h1>
          <p className="cd-nd-title-lead">{TITLE_SCREEN.lead}</p>
        </div>
        <ol className="cd-nd-steps" aria-label="How to play">
          {TITLE_SCREEN.steps.map((step, i) => (
            <li key={step} className="cd-nd-step">
              <span className="cd-nd-badge" aria-hidden="true">
                {i + 1}
              </span>
              {step}
            </li>
          ))}
        </ol>
        <p className="cd-nd-title-fine">{TITLE_SCREEN.fine}</p>
        <button
          type="button"
          className="cd-nd-play"
          onClick={onPlay}
          disabled={busy}
          aria-busy={busy || undefined}
        >
          {TITLE_SCREEN.play}
          <svg
            width="18"
            height="18"
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
        {played && (
          <p className="cd-nd-title-played">
            <span className="cd-nd-title-count">{played.count}</span>
            {played.rest}
          </p>
        )}
      </div>
    </div>
  );
}
