import { useRef, useState, type MutableRefObject } from 'react';
import { capture } from './analytics';
import { RATING_STOPS, rungLabel, type GridSettings } from './grid';
import { useScreen } from './screen';
import { useCloser, useDrag, useEscape, useFocusTrapped, useGlide, VIEW_EXIT_MS } from './sheet';
import { ThemePicker } from './ThemePicker';
import type { ThemePref } from './theme';
import { YearRange } from './YearRange';

/** What the reader can change about how the map is drawn. The order is
 *  the order of the switches. */
const SWITCHES: {
  key: keyof GridSettings;
  label: string;
  /** A second line, for a switch whose name does not say what it does. */
  sub?: string;
  on: (s: GridSettings) => boolean;
  set: (s: GridSettings, on: boolean) => GridSettings;
}[] = [
  {
    key: 'yearOrder',
    label: 'Newest first',
    on: (s) => s.yearOrder === 'newest',
    set: (s, on) => ({ ...s, yearOrder: on ? 'newest' : 'oldest' }),
  },
  {
    key: 'showUnrated',
    label: 'Show unrated movies',
    on: (s) => s.showUnrated,
    set: (s, on) => ({ ...s, showUnrated: on }),
  },
  {
    key: 'highlightYear',
    label: 'Highlight the searched year',
    on: (s) => s.highlightYear,
    set: (s, on) => ({ ...s, highlightYear: on }),
  },
  {
    key: 'hideEmptyYears',
    label: 'Hide empty years',
    sub: 'Years where nothing matches your filters',
    on: (s) => s.hideEmptyYears,
    set: (s, on) => ({ ...s, hideEmptyYears: on }),
  },
];

/** Which settings pull the plot out from under the reader, so the map
 *  has to be put back on the searched film afterwards.
 *
 *  Hiding the empty years is not one of them: it keeps the searched
 *  film where it is on the glass and moves the rest around it, so
 *  recentring would undo the one thing that makes it readable. */
export function movesTheMap(key: keyof GridSettings): boolean {
  return (
    key === 'yearOrder' ||
    key === 'showUnrated' ||
    key === 'yearFrom' ||
    key === 'yearTo'
  );
}

interface Props {
  settings: GridSettings;
  onChange: (s: GridSettings) => void;
  /** Called after a change that rearranges the plot. */
  onRelaid: () => void;
  /** The rating rungs live here below 1024 px wide, where the header has
   *  no room for them. */
  rungs: boolean;
  /** The oldest and newest year this map holds, for the year control. */
  bounds: { lo: number; hi: number };
  /** How many films each of those years holds, for the histogram over
   *  the year control (see yearCounts). */
  perYear: ReadonlyMap<number, number>;
  /** The searched film's year, for the report of what was asked for. */
  anchorYear: number;
  /** The range holds none of this cast's other films, which is why the
   *  reader is looking at one row. See `rangeHoldsNone`. */
  rangeEmpty: boolean;
  onFloor: (r: number | null) => void;
  theme: ThemePref;
  onTheme: (p: ThemePref) => void;
  onClose: () => void;
  /** Given the panel's own way out while it is up, so opening a map can
   *  close it on its exit first. */
  closer?: MutableRefObject<((then?: () => void) => void) | null>;
}

/** A popover over the View button on a desktop or tablet, a bottom
 *  sheet on a phone, and a panel down the left of a landscape phone,
 *  which has the width to spare and not the height. Each arrives and
 *  leaves the way the film sheet does, a little quicker. */
export function ViewPanel({
  settings,
  onChange,
  onRelaid,
  rungs,
  bounds,
  perYear,
  anchorYear,
  rangeEmpty,
  onFloor,
  theme,
  onTheme,
  onClose,
  closer,
}: Props) {
  const ref = useRef<HTMLDivElement>(null);
  // What the readout says. It follows the settled range, not the one
  // under the thumb.
  const [readout, setReadout] = useState({ from: settings.yearFrom, to: settings.yearTo });
  const set = readout.from != null || readout.to != null;
  const screen = useScreen();
  const { phone, short } = screen;
  const { phase, leave } = useGlide(onClose, VIEW_EXIT_MS);
  const drag = useDrag(phone, leave);
  useEscape(leave);
  useCloser(closer, leave);
  useFocusTrapped(ref);
  const held = drag.held && drag.y > 0;

  return (
    <>
      <div
        className={`cd-view-scrim${phone || short ? ' cd-view-scrim-dim' : ''}${phase === 'in' ? ' cd-view-scrim-in' : ''}`}
        onClick={() => leave()}
        aria-hidden="true"
      />
      <div
        // The class sets where it sits and how it travels. From the
        // screen class in script, the same one the rest of the layout
        // reads, rather than a media query of its own that could
        // disagree with it at a boundary.
        className={`cd-view cd-view-${screen.cls} cd-view-${phase}`}
        style={held ? { transform: `translateY(${drag.y}px)`, transition: 'none' } : undefined}
        role="dialog"
        aria-modal="true"
        aria-label="View"
        tabIndex={-1}
        ref={ref}
      >
        {phone && (
          <span
            className="cd-view-grip"
            aria-hidden="true"
            onPointerDown={drag.onPointerDown}
            onPointerMove={drag.onPointerMove}
            onPointerUp={drag.onPointerUp}
            onPointerCancel={drag.onPointerUp}
          />
        )}
        {rungs && (
          <div className="cd-view-section cd-view-rungs-section">
            <div className="cd-view-heading">Light movies rated at least</div>
            <div className="cd-view-rungs" role="group" aria-label="Light movies by rating">
              <Rung
                on={settings.minRating == null}
                label={rungLabel(null)}
                onPick={() => onFloor(null)}
              />
              {RATING_STOPS.map((r) => (
                <Rung
                  key={r}
                  on={settings.minRating === r}
                  label={rungLabel(r)}
                  aria={`Light movies rated at least ${r.toFixed(1)}`}
                  onPick={() => onFloor(settings.minRating === r ? null : r)}
                />
              ))}
            </div>
          </div>
        )}
        <div className="cd-view-section cd-years">
          <div className="cd-view-heading-row">
            <span className="cd-view-heading" id="cd-years-h">
              Years
            </span>
            {/* The range in words, beside its heading. It settles
                rather than following the thumb: a number changing
                sixty times a second is not a readout. */}
            <span
              className={`cd-range-readout${set ? '' : ' cd-range-readout-all'}`}
              aria-live="polite"
            >
              {set ? `${readout.from ?? bounds.lo} – ${readout.to ?? bounds.hi}` : 'All years'}
            </span>
            {/* Always here, so the row never changes height when a
                range is set. "Reset" alone says nothing out of context,
                so what it resets is in its name. */}
            <button
              type="button"
              className="cd-link"
              style={set ? undefined : { visibility: 'hidden' }}
              aria-hidden={set ? undefined : true}
              aria-label="Reset years"
              tabIndex={set ? undefined : -1}
              onClick={() => {
                onChange({ ...settings, yearFrom: null, yearTo: null });
                setReadout({ from: null, to: null });
                onRelaid();
              }}
            >
              Reset
            </button>
          </div>
          <YearRange
            lo={bounds.lo}
            hi={bounds.hi}
            from={shown(settings.yearFrom, bounds)}
            to={shown(settings.yearTo, bounds)}
            counts={perYear}
            onChange={(yearFrom, yearTo) => onChange({ ...settings, yearFrom, yearTo })}
            onSettled={() => {
              setReadout({ from: settings.yearFrom, to: settings.yearTo });
              onRelaid();
              // On commit only: a drag passes through eighty years and
              // the reader asked for one of them.
              capture('year_range_set', {
                from: settings.yearFrom,
                to: settings.yearTo,
                anchor_year: anchorYear,
              });
            }}
          />
          {rangeEmpty && (
            <p className="cd-range-note">
              Your range {settings.yearFrom ?? bounds.lo}–{settings.yearTo ?? bounds.hi} has
              none of this cast&rsquo;s movies
            </p>
          )}
        </div>
        <div className="cd-view-section cd-view-switches">
          {SWITCHES.map((sw) => (
            <button
              key={sw.key}
              type="button"
              className="cd-switch"
              role="switch"
              aria-checked={sw.on(settings)}
              onClick={() => {
                const on = !sw.on(settings);
                onChange(sw.set(settings, on));
                // Said apart from the same switch beside View (from: 'quick').
                if (sw.key === 'hideEmptyYears') capture('hide_empty_years', { on, from: 'view' });
                if (movesTheMap(sw.key)) onRelaid();
              }}
            >
              <span className="cd-switch-label">
                {sw.label}
                {sw.sub && <span className="cd-switch-sub">{sw.sub}</span>}
              </span>
              <span className="cd-switch-track" aria-hidden="true">
                <span className="cd-switch-knob" />
              </span>
            </button>
          ))}
        </div>
        <div className="cd-view-section cd-view-appearance">
          <div className="cd-view-heading">Appearance</div>
          <ThemePicker value={theme} onChange={onTheme} />
        </div>
      </div>
    </>
  );
}

/** A year drawn inside this map's bounds. What was set is left as it
 *  was: the slider only needs a number it can place. */
function shown(year: number | null, bounds: { lo: number; hi: number }): number | null {
  if (year == null) return null;
  return Math.min(Math.max(year, bounds.lo), bounds.hi);
}

function Rung({
  on,
  label,
  aria,
  onPick,
}: {
  on: boolean;
  label: string;
  aria?: string;
  onPick: () => void;
}) {
  return (
    <button
      type="button"
      className={`cd-rung${on ? ' cd-rung-on' : ''}`}
      aria-pressed={on}
      aria-label={aria}
      onClick={onPick}
    >
      {label}
    </button>
  );
}
