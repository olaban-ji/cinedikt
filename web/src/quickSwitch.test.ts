import { beforeEach, describe, expect, it, vi } from 'vitest';
import { DEFAULT_SETTINGS, type GridSettings } from './grid';
import { hideEmptyToast } from './quickSwitch';

const { capture } = vi.hoisted(() => ({ capture: vi.fn() }));
vi.mock('./analytics', () => ({ capture }));

/** GridApp's settings, held here so an update can be seen landing. */
function holder(start: GridSettings) {
  const box = { settings: start };
  const set = (update: (was: GridSettings) => GridSettings) => {
    box.settings = update(box.settings);
  };
  return { box, set };
}

describe('the quick switch’s toast', () => {
  beforeEach(() => capture.mockClear());

  it('counts the years it hides, with the singular for one', () => {
    const { set } = holder(DEFAULT_SETTINGS);
    expect(hideEmptyToast(true, 1, set, () => {}).text).toBe('Hiding 1 empty year');
    expect(hideEmptyToast(true, 2, set, () => {}).text).toBe('Hiding 2 empty years');
    expect(hideEmptyToast(true, 12, set, () => {}).text).toBe('Hiding 12 empty years');
  });

  it('says every year is back when turned off, with nothing to undo', () => {
    const { set } = holder(DEFAULT_SETTINGS);
    const spec = hideEmptyToast(false, 3, set, () => {});
    expect(spec.text).toBe('Showing every year');
    expect(spec.action).toBeUndefined();
  });

  it('offers Undo when turned on, which turns the setting back off', () => {
    // A floor set while the toast was up stays set: Undo takes back the
    // hiding and nothing else.
    const { box, set } = holder({ ...DEFAULT_SETTINGS, hideEmptyYears: true, minRating: 7 });
    const hide = vi.fn();
    const spec = hideEmptyToast(true, 4, set, hide);
    expect(spec.action?.label).toBe('Undo');
    spec.action?.run();
    expect(box.settings).toEqual({ ...DEFAULT_SETTINGS, hideEmptyYears: false, minRating: 7 });
    expect(hide).toHaveBeenCalledTimes(1);
    expect(capture).toHaveBeenCalledWith('hide_empty_years', { on: false, from: 'toast' });
  });

  it('changes nothing until Undo is pressed', () => {
    const start = { ...DEFAULT_SETTINGS, hideEmptyYears: true };
    const { box, set } = holder(start);
    const hide = vi.fn();
    hideEmptyToast(true, 2, set, hide);
    expect(box.settings).toBe(start);
    expect(hide).not.toHaveBeenCalled();
    expect(capture).not.toHaveBeenCalled();
  });
});
