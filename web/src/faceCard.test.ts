import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import css from './grid.css?raw';
import {
  FACE_CARD_PHOTO_W,
  FACE_CARD_W,
  bigPhoto,
  faceCardController,
  placeFaceCard,
  warmBigPhotos,
  type FaceCard,
  type FaceCardHost,
} from './faceCard';
import { FACE_CARD_DWELL_MS, FACE_CARD_HOLD_MS, FACE_CARD_IN_MS, FACE_CARD_WARM_MS, FACE_MS } from './motion';
import type { PreviewClock } from './preview';

const KEANU = 'https://image.tmdb.org/t/p/w185/keanu.jpg';
const HUGO = 'https://image.tmdb.org/t/p/w185/hugo.jpg';
const PHOTOS: Record<string, string> = { keanu: KEANU, hugo: HUGO };

const chip = { name: 'chip' } as unknown as HTMLElement;
const face = { name: 'face' } as unknown as HTMLElement;

/** A controller on the test's timers. Images arrive when the test says
 *  so (`arrive`), or at once with `instant`; `log` is what it asked of
 *  the page, in order. */
function setup(o: { hovers?: boolean; instant?: boolean; gone?: boolean } = {}) {
  const clock: PreviewClock = {
    now: () => Date.now(),
    after: (ms, run) => setTimeout(run, ms),
    cancel: (t) => clearTimeout(t),
  };
  const log: string[] = [];
  const opened: FaceCard[] = [];
  const asked: string[] = [];
  const waiting = new Map<string, (ok: boolean) => void>();
  const host: FaceCardHost = {
    hovers: () => o.hovers ?? true,
    photo: (id) => PHOTOS[id],
    place: (el, from) => (o.gone ? null : { x: el === chip ? 10 : 300, y: 50, down: from === 'chip' }),
    open: (card) => {
      opened.push(card);
      log.push(`open ${card.id} ${card.from}`);
    },
    close: () => log.push('close'),
  };
  const cards = faceCardController(host, clock, (url, done) => {
    asked.push(url);
    if (o.instant) done(true);
    else waiting.set(url, done);
    return () => waiting.delete(url);
  });
  const arrive = (url: string, ok = true) => waiting.get(url)?.(ok);
  return { cards, log, opened, asked, arrive, waiting };
}

beforeEach(() => {
  vi.useFakeTimers();
});
afterEach(() => {
  vi.useRealTimers();
});

describe('the face card’s timings', () => {
  it('are the handoff’s, in motion.ts beside the face cross-fade they are not', () => {
    expect([FACE_CARD_DWELL_MS, FACE_CARD_HOLD_MS, FACE_CARD_WARM_MS, FACE_CARD_IN_MS]).toEqual([400, 450, 250, 160]);
    // FACE_MS is the flight's own fade, and stays what it was.
    expect(FACE_MS).toBe(240);
  });

  it('fade the card in over FACE_CARD_IN_MS, as the stylesheet draws it', () => {
    const text = css.replace(/\/\*[\s\S]*?\*\//g, '');
    const rule = text.match(/(?:^|\})\s*\.cd-person-card\s*\{([^}]*)\}/)?.[1] ?? '';
    expect(rule.replace(/\s+/g, ' ')).toContain(
      `transition: opacity ${FACE_CARD_IN_MS}ms ease, transform 220ms var(--ease-glide);`,
    );
  });
});

describe('a pointer resting on a chip or a face', () => {
  it('opens the card after 400ms, once its bigger photo has arrived', () => {
    const { cards, log, asked, arrive } = setup();
    cards.onFace('keanu', chip, 'chip');
    // The bigger photo starts loading as the dwell does.
    expect(asked).toEqual([bigPhoto(KEANU)]);
    vi.advanceTimersByTime(FACE_CARD_DWELL_MS);
    // Nothing yet: there is only the blurry small one to show.
    expect(log).toEqual([]);
    arrive(bigPhoto(KEANU));
    expect(log).toEqual(['open keanu chip']);
  });

  it('opens it as the dwell ends when the photo is already in hand', () => {
    const { cards, log, arrive } = setup();
    cards.onFace('keanu', face, 'peek');
    arrive(bigPhoto(KEANU));
    vi.advanceTimersByTime(FACE_CARD_DWELL_MS - 1);
    expect(log).toEqual([]);
    vi.advanceTimersByTime(1);
    expect(log).toEqual(['open keanu peek']);
  });

  it('gives nobody without a photo a card, and asks for no image for them', () => {
    const { cards, log, asked } = setup({ instant: true });
    cards.onFace('gloria', chip, 'chip');
    vi.advanceTimersByTime(2000);
    expect(log).toEqual([]);
    expect(asked).toEqual([]);
  });

  it('gives no card when the bigger photo fails to load', () => {
    const { cards, log, arrive } = setup();
    cards.onFace('keanu', chip, 'chip');
    arrive(bigPhoto(KEANU), false);
    vi.advanceTimersByTime(2000);
    expect(log).toEqual([]);
  });

  it('opens nothing for a pointer that leaves before the dwell ends', () => {
    const { cards, log, waiting } = setup({ instant: true });
    cards.onFace('keanu', chip, 'chip');
    vi.advanceTimersByTime(300);
    cards.offFace();
    vi.advanceTimersByTime(2000);
    expect(log).toEqual([]);
    expect(waiting.size).toBe(0);
  });

  it('opens nothing when the chip or face has left the page on the way', () => {
    const { cards, log } = setup({ instant: true, gone: true });
    cards.onFace('keanu', chip, 'chip');
    vi.advanceTimersByTime(FACE_CARD_DWELL_MS);
    expect(log).toEqual([]);
  });

  it('ignores a pointer that cannot rest on anything', () => {
    const { cards, log, asked } = setup({ hovers: false, instant: true });
    cards.onFace('keanu', chip, 'chip');
    vi.advanceTimersByTime(2000);
    expect(log).toEqual([]);
    expect(asked).toEqual([]);
  });
});

describe('moving between people', () => {
  it('swaps at once within 250ms of a card closing', () => {
    const { cards, log } = setup({ instant: true });
    cards.onFace('keanu', chip, 'chip');
    vi.advanceTimersByTime(FACE_CARD_DWELL_MS);
    cards.offFace();
    vi.advanceTimersByTime(FACE_CARD_WARM_MS - 1);
    cards.onFace('hugo', chip, 'chip');
    vi.advanceTimersByTime(0);
    expect(log).toEqual(['open keanu chip', 'close', 'open hugo chip']);
  });

  it('waits the whole dwell again once the warm window has passed', () => {
    const { cards, log } = setup({ instant: true });
    cards.onFace('keanu', chip, 'chip');
    vi.advanceTimersByTime(FACE_CARD_DWELL_MS);
    cards.offFace();
    vi.advanceTimersByTime(FACE_CARD_WARM_MS);
    cards.onFace('hugo', chip, 'chip');
    vi.advanceTimersByTime(FACE_CARD_DWELL_MS - 1);
    expect(log).toEqual(['open keanu chip', 'close']);
    vi.advanceTimersByTime(1);
    expect(log).toEqual(['open keanu chip', 'close', 'open hugo chip']);
  });

  it('swaps at once while a card is showing', () => {
    const { cards, opened } = setup({ instant: true });
    cards.onFace('keanu', face, 'peek');
    vi.advanceTimersByTime(FACE_CARD_DWELL_MS);
    cards.onFace('hugo', face, 'peek');
    vi.advanceTimersByTime(0);
    expect(opened.map((c) => c.id)).toEqual(['keanu', 'hugo']);
  });

  it('never counts a card that never opened as one that closed', () => {
    const { cards, log } = setup({ instant: true });
    cards.onFace('keanu', chip, 'chip');
    vi.advanceTimersByTime(100);
    cards.offFace();
    cards.onFace('hugo', chip, 'chip');
    vi.advanceTimersByTime(FACE_CARD_DWELL_MS - 1);
    expect(log).toEqual([]);
  });
});

describe('what closes the card', () => {
  it('the preview going takes only a card from its faces, or one on its way', () => {
    const { cards, log } = setup({ instant: true });
    cards.onFace('keanu', chip, 'chip');
    vi.advanceTimersByTime(FACE_CARD_DWELL_MS);
    cards.offFace('peek');
    expect(log).toEqual(['open keanu chip']);
    cards.offFace();
    vi.advanceTimersByTime(1000);
    cards.onFace('hugo', face, 'peek');
    vi.advanceTimersByTime(FACE_CARD_DWELL_MS);
    cards.offFace('peek');
    expect(log).toEqual(['open keanu chip', 'close', 'open hugo peek', 'close']);
    // And one still on its way does not open after it.
    vi.advanceTimersByTime(1000);
    cards.onFace('keanu', face, 'peek');
    vi.advanceTimersByTime(100);
    cards.offFace('peek');
    vi.advanceTimersByTime(1000);
    expect(log).toHaveLength(4);
  });

  it('the map scrolling, or another map, closes it and forgets one on its way', () => {
    const { cards, log } = setup({ instant: true });
    cards.onFace('keanu', chip, 'chip');
    vi.advanceTimersByTime(FACE_CARD_DWELL_MS);
    cards.shut();
    expect(log).toEqual(['open keanu chip', 'close']);
    vi.advanceTimersByTime(1000);
    cards.onFace('hugo', chip, 'chip');
    vi.advanceTimersByTime(100);
    cards.shut();
    vi.advanceTimersByTime(1000);
    expect(log).toEqual(['open keanu chip', 'close']);
  });
});

describe('a finger held on a chip', () => {
  it('opens the card after 450ms, and lifting closes it and takes the click', () => {
    const { cards, log } = setup({ instant: true, hovers: false });
    cards.down('keanu', chip, 'touch');
    vi.advanceTimersByTime(FACE_CARD_HOLD_MS - 1);
    expect(log).toEqual([]);
    vi.advanceTimersByTime(1);
    expect(log).toEqual(['open keanu chip']);
    expect(cards.menu()).toBe(true);
    cards.up();
    expect(log).toEqual(['open keanu chip', 'close']);
    expect(cards.click()).toBe(true);
    expect(cards.click()).toBe(false);
    expect(cards.menu()).toBe(false);
  });

  it('keeps the click it ends when the photo was still on its way', () => {
    const { cards, log } = setup({ hovers: false });
    cards.down('keanu', chip, 'touch');
    vi.advanceTimersByTime(FACE_CARD_HOLD_MS);
    cards.up();
    expect(log).toEqual([]);
    expect(cards.click()).toBe(true);
  });

  it('leaves a tap, and a press called off, to the chip', () => {
    const { cards, log } = setup({ instant: true, hovers: false });
    cards.down('keanu', chip, 'touch');
    expect(cards.menu()).toBe(true);
    vi.advanceTimersByTime(200);
    cards.up();
    expect(cards.click()).toBe(false);
    cards.down('keanu', chip, 'touch');
    vi.advanceTimersByTime(300);
    cards.cancel();
    vi.advanceTimersByTime(1000);
    expect(log).toEqual([]);
    expect(cards.menu()).toBe(false);
  });

  it('closes a card already open when the press is called off', () => {
    const { cards, log } = setup({ instant: true, hovers: false });
    cards.down('keanu', chip, 'touch');
    vi.advanceTimersByTime(FACE_CARD_HOLD_MS);
    cards.cancel();
    expect(log).toEqual(['open keanu chip', 'close']);
    // No click follows a cancel, so none is waited for.
    expect(cards.click()).toBe(false);
  });

  it('holds nothing for someone without a photo, whose chip a long press still toggles', () => {
    const { cards, log } = setup({ instant: true, hovers: false });
    cards.down('gloria', chip, 'touch');
    vi.advanceTimersByTime(1000);
    cards.up();
    expect(log).toEqual([]);
    expect(cards.click()).toBe(false);
  });

  it('is not a mouse: a mouse press starts no hold, and lets a dwell run on', () => {
    const { cards, log } = setup({ instant: true });
    cards.onFace('keanu', chip, 'chip');
    cards.down('keanu', chip, 'mouse');
    vi.advanceTimersByTime(100);
    cards.up();
    expect(cards.click()).toBe(false);
    vi.advanceTimersByTime(FACE_CARD_DWELL_MS);
    expect(log).toEqual(['open keanu chip']);
  });

  it('is not a pointer resting on the chip, for the mouse events a tap makes up', () => {
    const { cards, log } = setup({ instant: true });
    cards.down('keanu', chip, 'touch');
    vi.advanceTimersByTime(100);
    cards.up();
    // Between the lift and the click, a browser that can hover sends
    // mouseenter for the tap. It opens nothing.
    cards.onFace('keanu', chip, 'chip');
    vi.advanceTimersByTime(2000);
    expect(cards.click()).toBe(false);
    expect(log).toEqual([]);
    // After the click, a real mouse is back to resting.
    cards.onFace('keanu', chip, 'chip');
    vi.advanceTimersByTime(FACE_CARD_DWELL_MS);
    expect(log).toEqual(['open keanu chip']);
  });
});

describe('warmBigPhotos', () => {
  it('starts each bigger photo once, and none for someone without a photo', () => {
    const asked: string[] = [];
    const load = (url: string) => {
      asked.push(url);
      return () => {};
    };
    const warmed = new Set<string>();
    warmBigPhotos([KEANU, undefined, HUGO, KEANU], warmed, load);
    expect(asked).toEqual([bigPhoto(KEANU), bigPhoto(HUGO)]);
    // A photo found later is started then; the rest are not asked for again.
    const LATER = 'https://image.tmdb.org/t/p/w185/later.jpg';
    warmBigPhotos([KEANU, HUGO, LATER], warmed, load);
    expect(asked).toEqual([bigPhoto(KEANU), bigPhoto(HUGO), bigPhoto(LATER)]);
  });
});

describe('placeFaceCard', () => {
  const win = { width: 1366, height: 768 };

  it('centres the card on a chip and hangs it 8px below', () => {
    const at = placeFaceCard({ left: 400, top: 60, width: 150, bottom: 94 }, 'chip', win);
    expect(at).toEqual({ x: Math.round(475 - FACE_CARD_W / 2), y: 102, down: true });
  });

  it('keeps it 8px inside the window’s edges, for a chip near either', () => {
    expect(placeFaceCard({ left: 2, top: 60, width: 90, bottom: 94 }, 'chip', win).x).toBe(8);
    expect(placeFaceCard({ left: 1300, top: 60, width: 60, bottom: 94 }, 'chip', win).x).toBe(1366 - 148 - 8);
    expect(placeFaceCard({ left: 330, top: 60, width: 50, bottom: 94 }, 'chip', { width: 390, height: 844 }).x).toBe(
      390 - 148 - 8,
    );
  });

  it('hangs it below a preview face near the top of the window, and stands it 8px above one lower down', () => {
    expect(placeFaceCard({ left: 600, top: 299, width: 30, bottom: 329 }, 'peek', win)).toEqual({
      x: 541,
      y: 337,
      down: true,
    });
    // Held by its bottom edge, 8px above the face's top.
    expect(placeFaceCard({ left: 600, top: 300, width: 30, bottom: 330 }, 'peek', win)).toEqual({
      x: 541,
      y: 768 - 300 + 8,
      down: false,
    });
  });
});

describe('bigPhoto', () => {
  it('asks TMDb for its w342 profile image, from the payload’s w185', () => {
    expect(FACE_CARD_PHOTO_W).toBe(128);
    expect(bigPhoto(KEANU)).toBe('https://image.tmdb.org/t/p/w342/keanu.jpg');
  });
});
