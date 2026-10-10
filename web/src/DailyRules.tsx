import { useRef, type KeyboardEvent } from 'react';
import { HOW_IT_WORKS } from './daily';
import { useEscape, useFocusTrapped } from './sheet';

/** The elements a Tab can land on inside `box`, in order. */
function tabbable(box: HTMLElement): HTMLElement[] {
  return [
    ...box.querySelectorAll<HTMLElement>(
      'button:not([disabled]), a[href], input:not([disabled]), [tabindex]:not([tabindex="-1"])',
    ),
  ];
}

/** Where Tab goes next inside a dialog with `count` stops, from `at` (-1
 *  for the dialog itself): round to the first after the last, and to the
 *  last before the first, so the focus never leaves it. */
export function nextStop(at: number, count: number, back: boolean): number {
  if (count === 0) return -1;
  if (back) return at <= 0 ? count - 1 : at - 1;
  return at < 0 || at >= count - 1 ? 0 : at + 1;
}

/** A modal dialog's keys: Tab and Shift-Tab go round its own stops
 *  (nextStop), never out of `box`. The Daily's two dialogs, How it works
 *  and the question before the Movies map is chosen, both keep to it. */
export function keepTabIn(box: HTMLElement | null, e: KeyboardEvent<HTMLElement>): void {
  if (e.key !== 'Tab' || !box) return;
  const stops = tabbable(box);
  const next = nextStop(stops.indexOf(document.activeElement as HTMLElement), stops.length, e.shiftKey);
  if (next < 0) return;
  e.preventDefault();
  stops[next].focus();
}

/** How it works, from the header's button: six numbered items, the prices,
 *  and Got it. A modal dialog over a scrim: the focus starts on it and
 *  stays in it, Escape, the scrim and Got it close it, and the focus goes
 *  back where it was. Its words and prices are the rules' (HOW_IT_WORKS),
 *  so the two can never disagree. */
export function DailyRules({ onClose }: { onClose: () => void }) {
  const box = useRef<HTMLDivElement>(null);
  useFocusTrapped(box);
  useEscape(onClose);
  return (
    <div className="cd-nd-rules-scrim" onClick={onClose}>
      <div
        ref={box}
        className="cd-nd-rules"
        role="dialog"
        aria-modal="true"
        aria-labelledby="cd-nd-rules-title"
        tabIndex={-1}
        onClick={(e) => e.stopPropagation()}
        onKeyDown={(e) => keepTabIn(box.current, e)}
      >
        <h2 id="cd-nd-rules-title" className="cd-nd-rules-title">
          {HOW_IT_WORKS.heading}
        </h2>
        <ol className="cd-nd-rules-list">
          {HOW_IT_WORKS.items.map((item, i) => (
            <li key={i} className="cd-nd-rules-item">
              <span className="cd-nd-badge" aria-hidden="true">
                {i + 1}
              </span>
              <span>{item}</span>
            </li>
          ))}
        </ol>
        <p className="cd-nd-rules-then">{HOW_IT_WORKS.then}</p>
        <button type="button" className="cd-nd-rules-close" onClick={onClose}>
          {HOW_IT_WORKS.close}
        </button>
      </div>
    </div>
  );
}
