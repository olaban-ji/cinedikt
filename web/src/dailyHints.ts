// Which of the Daily's first-time hints this device has shown (daily.ts,
// the first-time hints), so each is shown once: the hand on the next row
// (`idle`), the note on a Movies map nothing names yet (`map`), the line
// on cold, warm and hot after the first wrong guess (`warm`), and the
// map's pointer at its named cards (`coach`). Kept in localStorage under
// one key, as {"idle":1,"map":1,"warm":1,"coach":1}.
//
// With storage blocked every hint counts as seen, so none is shown,
// rather than every one in every game. Play again, in development,
// starts the reader again from nothing, and forgets them.

export type HintKind = 'idle' | 'map' | 'warm' | 'coach';

export const HINTS_KEY = 'cinedikt.daily.hints';

const KINDS: readonly HintKind[] = ['idle', 'map', 'warm', 'coach'];

/** The hints seen, from what the key holds: only the four, and only
 *  when marked 1, so anything else there, or nothing, is none seen. */
export function hintsFrom(raw: string | null): Set<HintKind> {
  const seen = new Set<HintKind>();
  let got: unknown = null;
  try {
    got = raw == null ? null : JSON.parse(raw);
  } catch {
    return seen;
  }
  if (!got || typeof got !== 'object') return seen;
  for (const k of KINDS) if ((got as Record<string, unknown>)[k] === 1) seen.add(k);
  return seen;
}

function written(seen: Set<HintKind>): string {
  return JSON.stringify(Object.fromEntries(KINDS.filter((k) => seen.has(k)).map((k) => [k, 1])));
}

/** Whether this device has shown the hint, or cannot say. */
export function hintSeen(kind: HintKind): boolean {
  try {
    return hintsFrom(localStorage.getItem(HINTS_KEY)).has(kind);
  } catch {
    return true;
  }
}

/** The hint has been shown here. */
export function markHint(kind: HintKind): void {
  try {
    const seen = hintsFrom(localStorage.getItem(HINTS_KEY));
    seen.add(kind);
    localStorage.setItem(HINTS_KEY, written(seen));
  } catch {
    // Nothing to keep it in; hintSeen says so already.
  }
}

/** Every hint unseen again: a reader started over. */
export function forgetHints(): void {
  try {
    localStorage.removeItem(HINTS_KEY);
  } catch {
    // Nothing kept, so nothing to forget.
  }
}
