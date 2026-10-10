import { useRef } from 'react';
import { sheetAsk } from './daily';
import { keepTabIn } from './DailyRules';
import { useEscape, useFocusTrapped } from './sheet';

// The question before the game's one Movies map is chosen. Readable titles
// on two people's maps, side by side, would almost always leave only
// today's movie, so a game gets one map, and the server holds the reader
// to the first they open. Tapping Movies is too light a touch for a choice
// that lasts the game, so it asks first, in the reader's own words for it:
// whose map, that it is the only one, and a way out that leaves every map
// closed and the choice still to make.

interface AskProps {
  /** The name whose Movies button was tapped. */
  name: string;
  /** Open the map: the page chooses it and opens it. */
  onYes: () => void;
  /** Not now, the scrim or Escape: nothing is chosen. */
  onNo: () => void;
}

/** A small modal dialog over a scrim, as How it works is one: the focus
 *  starts on it and stays in it (keepTabIn), Escape and the scrim say
 *  Not now, and the focus goes back to the Movies button it came from,
 *  whatever was answered. Named by its question, described by the rule,
 *  and an alertdialog, as one that wants an answer before anything else. */
export function DailySheetAsk({ name, onYes, onNo }: AskProps) {
  const box = useRef<HTMLDivElement>(null);
  useFocusTrapped(box);
  useEscape(onNo);
  const ask = sheetAsk(name);
  return (
    <div className="cd-nd-choose-scrim" onClick={onNo}>
      <div
        ref={box}
        className="cd-nd-choose"
        role="alertdialog"
        aria-modal="true"
        aria-labelledby="cd-nd-choose-title"
        aria-describedby="cd-nd-choose-body"
        tabIndex={-1}
        onClick={(e) => e.stopPropagation()}
        onKeyDown={(e) => keepTabIn(box.current, e)}
      >
        <h2 id="cd-nd-choose-title" className="cd-nd-choose-title">
          {ask.title}
        </h2>
        <p id="cd-nd-choose-body" className="cd-nd-choose-body">
          {ask.body}
        </p>
        <div className="cd-nd-choose-row">
          <button type="button" className="cd-nd-choose-no" onClick={onNo}>
            {ask.no}
          </button>
          <button type="button" className="cd-nd-choose-yes" onClick={onYes}>
            {ask.yes}
          </button>
        </div>
      </div>
    </div>
  );
}
