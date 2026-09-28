import { capture } from './analytics';
import type { GridSettings } from './grid';
import type { ToastSpec } from './Toast';

/** A settings update, the way GridApp's setSettings takes one. */
type SettingsUpdate = (update: (was: GridSettings) => GridSettings) => void;

/** What the toast says after the quick "Hide empty years" switch is
 *  flipped on a phone or short screen, where the switch is an icon with
 *  no words to say it. `hidden` is how many years turning it on takes
 *  away, counted before the flip. Turned on, it offers Undo, which turns
 *  the setting back off, puts the toast away and says where the change
 *  came from. The undo is an update rather than a whole settings value,
 *  so it leaves alone anything else set while the toast was up. */
export function hideEmptyToast(
  on: boolean,
  hidden: number,
  setSettings: SettingsUpdate,
  hide: () => void,
): ToastSpec {
  if (!on) return { text: 'Showing every year' };
  return {
    text: `Hiding ${hidden} empty year${hidden === 1 ? '' : 's'}`,
    action: {
      label: 'Undo',
      run: () => {
        setSettings((was) => ({ ...was, hideEmptyYears: false }));
        hide();
        capture('hide_empty_years', { on: false, from: 'toast' });
      },
    },
  };
}
