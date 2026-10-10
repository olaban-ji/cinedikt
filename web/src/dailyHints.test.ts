import { afterEach, describe, expect, it, vi } from 'vitest';
import { HINTS_KEY, forgetHints, hintSeen, hintsFrom, markHint } from './dailyHints';

/** A localStorage of one's own, as a browser keeps it. */
function storage(): Storage {
  const m = new Map<string, string>();
  return {
    get length() {
      return m.size;
    },
    clear: () => m.clear(),
    getItem: (k) => m.get(k) ?? null,
    key: (i) => [...m.keys()][i] ?? null,
    removeItem: (k) => void m.delete(k),
    setItem: (k, v) => void m.set(k, String(v)),
  };
}

afterEach(() => {
  vi.unstubAllGlobals();
});

describe('the first-time hints seen', () => {
  it('are read from one key, only the four hints, and only when marked 1', () => {
    expect(HINTS_KEY).toBe('cinedikt.daily.hints');
    expect([...hintsFrom('{"idle":1,"map":1,"warm":1,"coach":1}')]).toEqual(['idle', 'map', 'warm', 'coach']);
    expect([...hintsFrom('{"map":1,"warm":0,"coach":true,"other":1}')]).toEqual(['map']);
    for (const raw of [null, '', 'nope', '1', 'null', '[1]']) expect(hintsFrom(raw).size, String(raw)).toBe(0);
  });

  it('are kept as each is shown, and forgotten all at once', () => {
    const store = storage();
    vi.stubGlobal('localStorage', store);
    expect(hintSeen('idle')).toBe(false);
    markHint('warm');
    markHint('idle');
    markHint('warm');
    expect(store.getItem(HINTS_KEY)).toBe('{"idle":1,"warm":1}');
    expect([hintSeen('idle'), hintSeen('map'), hintSeen('warm'), hintSeen('coach')]).toEqual([true, false, true, false]);
    forgetHints();
    expect(store.getItem(HINTS_KEY)).toBeNull();
    expect(hintSeen('warm')).toBe(false);
  });

  it('keep what else the key held right, and mend what was not', () => {
    const store = storage();
    vi.stubGlobal('localStorage', store);
    store.setItem(HINTS_KEY, 'not json');
    markHint('map');
    expect(store.getItem(HINTS_KEY)).toBe('{"map":1}');
  });

  it('count as seen with storage blocked, so none is shown in every game', () => {
    vi.stubGlobal('localStorage', {
      getItem: () => {
        throw new Error('blocked');
      },
      setItem: () => {
        throw new Error('blocked');
      },
      removeItem: () => {
        throw new Error('blocked');
      },
    });
    expect(hintSeen('idle')).toBe(true);
    expect(() => markHint('idle')).not.toThrow();
    expect(() => forgetHints()).not.toThrow();
  });
});
