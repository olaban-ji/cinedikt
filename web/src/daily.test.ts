import { afterEach, describe, expect, it, vi } from 'vitest';
import type { DailyBoard, DailyEntry, DailyFilm, DailyGame, DailyPerson, DailyToday } from './api';
import {
  AGAIN_FAILED,
  ANSWER_POP_MS,
  CLUE_COST,
  CONFETTI_AFTER_MS,
  CONFETTI_MS,
  CONFETTI_PIECES,
  COUNT_UP_MS,
  DAILY_SETTINGS,
  DAILY_START,
  END_STAGGER_MAX_MS,
  FLIP_EASE,
  FLIP_MAX,
  FLIP_MIN,
  FLIP_MS,
  FRESH_HOLD_MS,
  GAME_NAME,
  INTRO_IN_MS,
  INTRO_OUT_MS,
  INTRO_PART_MS,
  MIDNIGHT_STEP_MS,
  OPEN_BOUNDS,
  PLAY_AGAIN,
  PULSE_MS,
  REEL_STEPS,
  RESULTS_AFTER_MS,
  ROLL_MS,
  SCORE_RISE_DELAY_MS,
  SCORE_RISE_MS,
  SCORE_SHOWN_MS,
  SHAKE_KEYFRAMES,
  SHAKE_MS,
  SPEND_FLOAT_MS,
  STAGGER_MS,
  STEPS_WIDE_MIN,
  STEPS_WIDE_QUERY,
  TWINKLE_EASE,
  TWINKLE_EVERY_MS,
  TWINKLE_MS,
  UNREACHABLE,
  WRONG_BASE,
  WRONG_STEP,
  andList,
  answerLabel,
  avatarColours,
  boardLayout,
  boardLines,
  boardNote,
  boardPayload,
  boardsWanted,
  boundLines,
  boundsOf,
  burstShift,
  cardLabel,
  cardsMarked,
  clockOffset,
  clueButtons,
  costSpan,
  countdown,
  dailyDateText,
  dailyDayText,
  dayCell,
  directionText,
  dropFailed,
  everyoneNamed,
  facesOf,
  feedOf,
  flipCost,
  guessOptions,
  guessedIds,
  hintText,
  hueSlots,
  inBounds,
  initialsOf,
  introButton,
  introDelay,
  introStreak,
  markedText,
  midnightText,
  mmss,
  moveSig,
  narrowed,
  newKey,
  ord,
  newWrongGuess,
  peopleByCard,
  peopleOf,
  playedText,
  plotSize,
  pointsShare,
  rankWidth,
  reelDelay,
  reelFrame,
  refusalText,
  resultSub,
  resultTitle,
  resultsDelay,
  resumeCards,
  revealedOf,
  rowBoundsOf,
  rowsBottom,
  scrollTarget,
  seamsFor,
  secondsSince,
  shareBar,
  shareLine,
  shareText,
  staggerDelays,
  staleGame,
  standingText,
  statsOf,
  stepColumns,
  streakAfter,
  usedList,
  watchMidnight,
  weekdayOf,
  yearOf,
  yearRuledOut,
  yearTargets,
  YEAR_PLACEHOLDER,
  type Bounds,
  type BoardsSeen,
  type GuessFound,
} from './daily';
import css from './grid.css?raw';
import { bandClass, railYearClass, xOf, type GridLayout } from './grid';

// ---- a puzzle to play with: The Matrix, on a board of eight ----

const film = (id: string, title: string, year: number, rating: number | null, poster?: string): DailyFilm => ({
  id,
  title,
  year,
  rating,
  md: 0,
  ...(poster ? { poster } : {}),
});

const person = (
  slot: number,
  name: string,
  role: 'director' | 'cast',
  cards: string[],
): DailyPerson => ({ id: `nm${String(slot).padStart(7, '0')}`, name, role, slot, cards });

const MAN_AND_BOY = film('tt0068907', 'Man and Boy', 1971, 5.5);
const BOBBY = film('tt0108065', 'Searching for Bobby Fischer', 1993, 7.3);
const BABY = film('tt0109190', 'Baby’s Day Out', 1994, 6.3);
const RELOADED = film('tt0234215', 'The Matrix Reloaded', 2003, 7.2);
const BOUND = film('tt0115736', 'Bound', 1996, 7.3);
const MATRIX = { ...film('tt0133093', 'The Matrix', 1999, 8.7), md: 331, genres: ['Action', 'Sci-Fi'] };

const LANA = person(0, 'Lana Wachowski', 'director', ['c4', 'c5']);
const LILLY = person(1, 'Lilly Wachowski', 'director', ['c4', 'c5']);
const KEANU = person(2, 'Keanu Reeves', 'cast', ['c4', 'c7']);
const FISHBURNE = person(3, 'Laurence Fishburne', 'cast', ['c2', 'c4']);
const PANTOLIANO = person(7, 'Joe Pantoliano', 'cast', ['c3', 'c5']);
const FOSTER = person(6, 'Gloria Foster', 'cast', ['c1']);

function todayOf(over: Partial<DailyToday> = {}): DailyToday {
  return {
    no: 142,
    date: '2026-10-08',
    now: '2026-10-08T12:00:00Z',
    next: '2026-10-09T00:00:00Z',
    cards: [
      { id: 'c1', year: 1971, rating: 5.5, md: 0 },
      { id: 'c2', year: 1993, rating: 7.3, md: 811 },
      { id: 'c3', year: 1994, rating: 6.3, md: 701 },
      { id: 'c4', year: 2003, rating: 7.2, md: 515 },
      { id: 'c5', year: 1996, rating: 7.3, md: 1004 },
      { id: 'c6', year: 2000, rating: 7.0, md: 0 },
      { id: 'c7', year: 1999, rating: 6.1, md: 0 },
      { id: 'c8', year: 2010, rating: 7.0, md: 0 },
    ],
    start: [
      { card: 'c1', film: MAN_AND_BOY },
      { card: 'c2', film: BOBBY },
      { card: 'c3', film: BABY },
    ],
    clues: { directors: 2, cast: 6 },
    player: { name: 'Trinity Kimble', saved: false },
    played: 61240,
    streak: { now: 0, before: 3 },
    game: null,
    ...over,
  };
}

function gameOf(log: DailyEntry[], over: Partial<DailyGame> = {}): DailyGame {
  return {
    phase: 'play',
    pts: 1000,
    seq: log.length - 1,
    startedAt: '2026-10-08T11:58:00Z',
    finishedAt: null,
    secs: null,
    won: false,
    gaveUp: false,
    nextCost: 100,
    log: [{ type: 'start' }, ...log],
    known: [],
    end: null,
    ...over,
  };
}

const guess = (
  f: DailyFilm,
  year: Extract<DailyEntry, { type: 'guess' }>['year'],
  rating: Extract<DailyEntry, { type: 'guess' }>['rating'],
  over: Partial<Extract<DailyEntry, { type: 'guess' }>> = {},
): Extract<DailyEntry, { type: 'guess' }> => ({
  type: 'guess',
  cost: 100,
  film: f,
  card: null,
  shared: [],
  year,
  rating,
  ...over,
});

/** The game once it is over, with everything it kept back. */
function ended(g: DailyGame, won: boolean): DailyGame {
  return {
    ...g,
    phase: 'done',
    won,
    secs: 192,
    finishedAt: '2026-10-08T12:01:12Z',
    end: {
      answer: MATRIX,
      cards: [
        { id: 'c1', film: MAN_AND_BOY, people: [6] },
        { id: 'c2', film: BOBBY, people: [3] },
        { id: 'c3', film: BABY, people: [7] },
        { id: 'c4', film: RELOADED, people: [0, 1, 2, 3] },
        { id: 'c5', film: BOUND, people: [0, 1, 7] },
        { id: 'c6', film: film('tt0000006', 'Six', 2000, 7.0), people: [] },
        { id: 'c7', film: film('tt0000007', 'Seven', 1999, 6.1), people: [2] },
        { id: 'c8', film: film('tt0000008', 'Eight', 2010, 7.0), people: [] },
      ],
      people: [LANA, LILLY, KEANU, FISHBURNE, FOSTER, PANTOLIANO],
    },
  };
}

const layoutFor = (today: DailyToday, game: DailyGame | null, width = 1440, compact?: boolean): GridLayout =>
  boardLayout(boardPayload(today, game), width, compact);

/** The Year clue's entry, as the server logs it. */
const yearEntry = (year: number): DailyEntry => ({ type: 'year', cost: 200, year });

describe('what turning a card over costs', () => {
  it('runs from 20 to 80 in fives, by the card’s rating', () => {
    // The handoff's table, a tenth at a time.
    const table: [number, number, number][] = [
      [4.0, 4.6, 20],
      [4.7, 5.0, 25],
      [5.1, 5.4, 30],
      [5.5, 5.8, 35],
      [5.9, 6.2, 40],
      [6.3, 6.6, 45],
      [6.7, 6.9, 50],
      [7.0, 7.3, 55],
      [7.4, 7.7, 60],
      [7.8, 8.1, 65],
      [8.2, 8.5, 70],
      [8.6, 8.9, 75],
      [9.0, 9.9, 80],
    ];
    for (const [lo, hi, cost] of table) {
      for (let r = Math.round(lo * 10); r <= Math.round(hi * 10); r++) {
        expect(flipCost(r / 10), `${r / 10}`).toBe(cost);
      }
    }
  });

  it('rounds 7.0’s 52.5 up to 55, as the server does', () => {
    expect(flipCost(7)).toBe(55);
  });

  it('never goes under 20 or over 80, however low or high the rating', () => {
    expect(flipCost(1.2)).toBe(20);
    expect(flipCost(10)).toBe(80);
    // The ends the title screen's "Card 20–80" is written from.
    expect([FLIP_MIN, FLIP_MAX]).toEqual([20, 80]);
    expect([flipCost(0), flipCost(10)]).toEqual([FLIP_MIN, FLIP_MAX]);
  });
});

describe('the rules’ prices', () => {
  it('start every game with a thousand points and price the clues and wrong guesses as the rules do', () => {
    // The server charges, and sends the next wrong guess's price with the
    // game; these are what the intro says.
    expect(DAILY_START).toBe(1000);
    expect(CLUE_COST).toEqual({ director: 150, actor: 150, genres: 80, year: 200 });
    expect([WRONG_BASE, WRONG_STEP]).toEqual([100, 50]);
  });
});

describe('the title screen', () => {
  it('names the game Point Blank', () => {
    expect(GAME_NAME).toBe('Point Blank');
  });

  it('writes a price that runs between two numbers as a range, and one that does not as the number', () => {
    expect(costSpan(FLIP_MIN, FLIP_MAX)).toBe('20–80');
    expect(costSpan(80, 20)).toBe('20–80');
    expect(costSpan(CLUE_COST.director, CLUE_COST.actor)).toBe('150');
    expect(costSpan(150, 200)).toBe('150–200');
  });

  it('lays its steps four across from a 760px window, and two by two below it', () => {
    for (const w of [320, 375, 390, 639, 660, 759, 759.5]) expect(stepColumns(w), `${w}`).toBe(2);
    for (const w of [760, 768, 1024, 1440]) expect(stepColumns(w), `${w}`).toBe(4);
    expect(STEPS_WIDE_MIN).toBe(760);
    // What DailyIntro watches the window with: the same edge, written as
    // a query, so the two cannot turn at different widths.
    expect(STEPS_WIDE_QUERY).toBe('(min-width: 760px)');
    // And not a query in the stylesheet, which keeps to its screen
    // classes (screen.test.ts).
    expect(css).not.toContain(STEPS_WIDE_QUERY);
  });
});

describe('the points left as a share of the start', () => {
  it('is a percentage of the thousand a game starts with', () => {
    expect(pointsShare(DAILY_START)).toBe(100);
    expect(pointsShare(695)).toBe(69.5);
    expect(pointsShare(0)).toBe(0);
  });

  it('never runs under nothing or over full', () => {
    expect(pointsShare(-50)).toBe(0);
    expect(pointsShare(DAILY_START + 1)).toBe(100);
  });

  it('fills the meter, the share bar and a week’s cell alike', () => {
    // The bar to the nearest tenth, a half tenth rounding up as before;
    // the cell to the nearest percent.
    expect(shareBar(650)).toBe('▰▰▰▰▰▰▰▱▱▱');
    expect(shareBar(640)).toBe('▰▰▰▰▰▰▱▱▱▱');
    expect(dayCell(695, 0).h).toBe(70);
  });
});

describe('the board’s faces', () => {
  const today = todayOf();

  it('shows nothing before Play, not even the starting three', () => {
    expect(facesOf(today, null).size).toBe(0);
    expect(revealedOf(today, null).size).toBe(0);
  });

  it('shows the starting three once the game is on', () => {
    const faces = facesOf(today, gameOf([]));
    expect([...faces.keys()]).toEqual(['c1', 'c2', 'c3']);
    expect(faces.get('c2')).toEqual({ kind: 'film', film: BOBBY });
  });

  it('shows a card turned over, and only how near a close relative is', () => {
    const faces = facesOf(
      today,
      gameOf([
        { type: 'flip', card: 'c5', cost: 55, film: BOUND },
        { type: 'flip', card: 'c4', cost: 55, relative: { shared: 4 } },
      ]),
    );
    expect(faces.get('c5')).toEqual({ kind: 'film', film: BOUND });
    expect(faces.get('c4')).toEqual({ kind: 'relative', shared: 4 });
  });

  it('shows a close relative guessed by name as itself', () => {
    const faces = facesOf(
      today,
      gameOf([
        { type: 'flip', card: 'c4', cost: 55, relative: { shared: 4 } },
        guess(RELOADED, 'older', 'higher', { card: 'c4' }),
      ]),
    );
    expect(faces.get('c4')).toEqual({ kind: 'film', film: RELOADED });
  });

  it('turns a card guessed by name over, and leaves one off the board alone', () => {
    const g = gameOf([guess(BOUND, 'newer', 'higher', { card: 'c5' }), guess(MATRIX, 'same', 'same')]);
    expect(revealedOf(today, g)).toEqual(new Set(['c1', 'c2', 'c3', 'c5']));
  });

  it('shows every card once the game is over', () => {
    const faces = facesOf(today, ended(gameOf([{ type: 'flip', card: 'c4', cost: 55, relative: { shared: 4 } }]), true));
    expect(faces.size).toBe(8);
    expect(faces.get('c4')).toEqual({ kind: 'film', film: RELOADED });
  });
});

describe('the people on the board', () => {
  it('are those known while the game is on, and everyone at the end', () => {
    const g = gameOf([], { known: [KEANU] });
    expect(peopleOf(null)).toEqual([]);
    expect(peopleOf(g)).toEqual([KEANU]);
    expect(peopleOf(ended(g, true))).toHaveLength(6);
  });

  it('are drawn on each card in slot order: directors, then billing', () => {
    const by = peopleByCard([KEANU, LILLY, LANA]);
    expect(by.get('c4')!.map((p) => p.name)).toEqual(['Lana Wachowski', 'Lilly Wachowski', 'Keanu Reeves']);
    expect(by.get('c7')!.map((p) => p.name)).toEqual(['Keanu Reeves']);
    expect(by.has('c1')).toBe(false);
  });

  it('take their colour from their slot, the same for everyone playing, wrapping at sixteen', () => {
    const late = { ...KEANU, id: 'nm9999999', slot: 18 };
    expect(hueSlots([LANA, KEANU, late])).toEqual(
      new Map([
        [LANA.id, 0],
        [KEANU.id, 2],
        ['nm9999999', 2],
      ]),
    );
  });

  it('include everyone the feed names, once each, for their faces', () => {
    const g = gameOf(
      [
        { type: 'person', role: 'director', cost: 150, people: [LANA, LILLY] },
        guess(BOUND, 'newer', 'higher', { shared: [PANTOLIANO, LANA] }),
      ],
      { known: [LANA, LILLY, PANTOLIANO] },
    );
    expect(everyoneNamed(g).map((p) => p.slot)).toEqual([0, 1, 7]);
  });

  it('count the cards a set of people mark between them', () => {
    expect(cardsMarked([LANA, LILLY])).toBe(2);
    expect(cardsMarked([KEANU, FISHBURNE])).toBe(3);
    expect(cardsMarked([])).toBe(0);
  });
});

describe('the guesses so far', () => {
  it('are the wrong ones, by IMDb id, for the search to mark', () => {
    const g = gameOf([guess(BOUND, 'newer', 'higher'), { type: 'flip', card: 'c5', cost: 55, film: BOUND }]);
    expect(guessedIds(g)).toEqual(new Set([BOUND.id]));
    expect(guessedIds(null).size).toBe(0);
  });

  it('shake the panel when one more of them is wrong', () => {
    const was = gameOf([]);
    const now = gameOf([guess(BOUND, 'newer', 'higher')]);
    expect(newWrongGuess(was, now)).toBe(true);
    expect(newWrongGuess(now, now)).toBe(false);
    expect(newWrongGuess(null, gameOf([]))).toBe(false);
  });
});

describe('coming back to a game', () => {
  it('goes to the last card turned over', () => {
    const g = gameOf([
      { type: 'flip', card: 'c5', cost: 55, film: BOUND },
      { type: 'genres', cost: 80, genres: ['Action'] },
    ]);
    expect(resumeCards(todayOf(), g)).toEqual(['c5']);
  });

  it('goes to the starting three when nothing has been turned', () => {
    expect(resumeCards(todayOf(), gameOf([]))).toEqual(['c1', 'c2', 'c3']);
  });
});

describe('the board’s layout', () => {
  const today = todayOf();

  it('is handed a stand-in for the answer that is on no card and from no year', () => {
    const { payload, settings } = boardPayload(today, gameOf([]));
    expect(payload.anchor.id).toBe('');
    expect(payload.anchor.year).toBe(0);
    expect(payload.films.some(([id]) => id === '')).toBe(false);
    expect(payload.films).toContainEqual(['c2', 1993, 7.3, 811]);
    expect(settings).toBe(DAILY_SETTINGS);
    expect(settings.showUnrated).toBe(false);
    expect(settings.highlightYear).toBe(false);
  });

  it('lights no year and keeps no row for one while the game is on', () => {
    const l = layoutFor(today, gameOf([]));
    expect(l.anchor).toBeNull();
    expect(l.rows.some((r) => r.anchorYear)).toBe(false);
    expect(l.rows.map((r) => r.year)).toEqual([1971, 1993, 1994, 1996, 1999, 2000, 2003, 2010]);
    // No unrated column to keep a place for.
    expect(l.axisTitleLeft).toBe(l.metrics.railW + 10);
  });

  it('never carries the answer before the end, whatever has been played', () => {
    const g = gameOf([
      { type: 'flip', card: 'c4', cost: 55, relative: { shared: 4 } },
      guess(BOUND, 'newer', 'higher', { card: 'c5', shared: [LANA] }),
    ]);
    const said = JSON.stringify(boardPayload(today, g));
    expect(said).not.toContain(MATRIX.id);
    expect(said).not.toContain(MATRIX.title);
  });

  it('puts the answer in its own year at the end, and lights that year', () => {
    const { payload, settings } = boardPayload(today, ended(gameOf([]), true));
    expect(payload.anchor.id).toBe(MATRIX.id);
    expect(payload.films).toContainEqual([MATRIX.id, 1999, 8.7, 331]);
    expect(settings.highlightYear).toBe(true);
    const l = layoutFor(today, ended(gameOf([]), true));
    expect(l.anchor?.film.id).toBe(MATRIX.id);
    expect(l.rows.filter((r) => r.anchorYear).map((r) => r.year)).toEqual([1999]);
  });

  it('runs the plot on past the panel, or under the phone’s sheet', () => {
    const l = layoutFor(today, null);
    expect(plotSize(l, false)).toEqual({ w: l.plotW + 396, h: rowsBottom(l) + 140 });
    expect(plotSize(l, true)).toEqual({ w: l.plotW, h: rowsBottom(l) + 360 });
    expect(rowsBottom({ rows: [] })).toBe(26);
  });

  it('opens a year that has just arrived out of the seam where it goes', () => {
    const lone = todayOf({ cards: today.cards.filter((c) => c.year !== 1999) });
    const was = layoutFor(lone, gameOf([]));
    const now = layoutFor(lone, ended(gameOf([]), true));
    const seams = seamsFor(was.rows, now.rows);
    // 1999 is new, and opens where 2000 used to start.
    expect([...seams.keys()]).toEqual([1999]);
    expect(seams.get(1999)).toBe(was.rows.find((r) => r.year === 2000)!.top);
    expect(seamsFor(now.rows, now.rows).size).toBe(0);
  });

  it('opens a year past the last one out of the old bottom', () => {
    const early = todayOf({ cards: today.cards.filter((c) => c.year < 1999) });
    const was = layoutFor(early, gameOf([]));
    const now = layoutFor(early, ended(gameOf([]), true));
    expect(seamsFor(was.rows, now.rows).get(1999)).toBe(rowsBottom(was));
  });
});

describe('narrowing on a wrong guess', () => {
  it('moves the year floor past an answer that is newer, and the ceiling under one that is older', () => {
    expect(narrowed(OPEN_BOUNDS, guess(BABY, 'newer', null)).yLo).toBe(1995);
    expect(narrowed(OPEN_BOUNDS, guess(RELOADED, 'older', null)).yHi).toBe(2002);
  });

  it('pins the year outright on the same year, rather than meeting the bounds', () => {
    const b: Bounds = { ...OPEN_BOUNDS, yLo: 1995, yHi: 2002 };
    expect(narrowed(b, guess(film('tt1', 'X', 1999, 6), 'same', null))).toMatchObject({ yLo: 1999, yHi: 1999 });
  });

  it('moves the ratings a tenth past the guess', () => {
    expect(narrowed(OPEN_BOUNDS, guess(BOBBY, null, 'higher')).rLo).toBe(7.4);
    expect(narrowed(OPEN_BOUNDS, guess(film('tt1', 'X', 2000, 7.0), null, 'lower')).rHi).toBe(6.9);
    expect(narrowed(OPEN_BOUNDS, guess(BOBBY, null, 'same'))).toMatchObject({ rLo: 7.3, rHi: 7.3 });
  });

  it('keeps the tightest of each bound', () => {
    let b = narrowed(OPEN_BOUNDS, guess(BABY, 'newer', 'higher'));
    b = narrowed(b, guess(MAN_AND_BOY, 'newer', 'higher'));
    expect(b).toEqual({ yLo: 1995, yHi: 9999, rLo: 6.4, rHi: 10 });
  });

  it('leaves the ratings alone for an unrated guess, and the years for one without a year', () => {
    expect(narrowed(OPEN_BOUNDS, guess(film('tt1', 'X', 0, null), null, null))).toEqual(OPEN_BOUNDS);
  });

  it('starts with nothing ruled out, and rules nothing out once the game is over', () => {
    expect(boundsOf(null)).toBeNull();
    expect(boundsOf(gameOf([]))).toBeNull();
    const g = gameOf([guess(BABY, 'newer', 'higher')]);
    expect(boundsOf(g)).toEqual({ yLo: 1995, yHi: 9999, rLo: 6.4, rHi: 10 });
    expect(boundsOf(ended(g, true))).toBeNull();
  });

  it('fades cards and years outside, keeping a rating exactly on a bound inside', () => {
    const b: Bounds = { yLo: 1995, yHi: 2004, rLo: 7.4, rHi: 8.9 };
    expect(inBounds({ year: 1999, rating: 7.4 }, b)).toBe(true);
    expect(inBounds({ year: 1999, rating: 0.1 + 0.2 + 7.1 }, b)).toBe(true);
    expect(inBounds({ year: 1994, rating: 8 }, b)).toBe(false);
    expect(inBounds({ year: 1999, rating: 7.3 }, b)).toBe(false);
    expect(inBounds({ year: 1999, rating: null }, b)).toBe(true);
    expect(inBounds({ year: 1950, rating: 1 }, null)).toBe(true);
    expect(yearRuledOut(1994, b)).toBe(true);
    expect(yearRuledOut(2004, b)).toBe(false);
    expect(yearRuledOut(1994, null)).toBe(false);
  });
});

describe('the bound lines', () => {
  const today = todayOf();
  const l = layoutFor(today, null);

  it('say where the answer can be in years: "1995 or later" and "2004 or earlier"', () => {
    const { years } = boundLines({ yLo: 1995, yHi: 2004, rLo: 0, rHi: 10 }, l);
    const at = (y: number) => l.rows.find((r) => r.year === y)!;
    expect(years).toEqual([
      { kind: 'from', y: at(1996).top, labelTop: at(1996).top + 6, labelLeft: l.metrics.railW + 10, label: '1995 or later' },
      {
        kind: 'until',
        y: at(2003).top + at(2003).height,
        labelTop: at(2003).top + at(2003).height - 24,
        labelLeft: l.metrics.railW + 10,
        label: '2004 or earlier',
      },
    ]);
  });

  it('rule a pinned year on both edges of its row, and name it once, inside the row', () => {
    const { years } = boundLines({ yLo: 1999, yHi: 1999, rLo: 0, rHi: 10 }, l);
    const r = l.rows.find((w) => w.year === 1999)!;
    expect(years).toEqual([
      { kind: 'from', y: r.top, labelTop: r.top + 6, labelLeft: l.metrics.railW + 10, label: '1999' },
      { kind: 'until', y: r.top + r.height, labelTop: r.top + r.height - 24, labelLeft: l.metrics.railW + 10, label: '' },
    ]);
  });

  it('draw no year line that would rule nothing out', () => {
    expect(boundLines({ yLo: 1960, yHi: 2020, rLo: 0, rHi: 10 }, l).years).toEqual([]);
  });

  it('say where it can be in ratings: "7.4+" and "Up to 6.9", each pill on its own side', () => {
    const { ratings } = boundLines({ yLo: 0, yHi: 9999, rLo: 7.4, rHi: 6.9 }, l);
    const x = (r: number) => Math.round(xOf(r, l.metrics));
    expect(ratings).toEqual([
      { x: x(7.4), labelLeft: x(7.4) + 4, before: false, label: '7.4+' },
      { x: x(6.9), labelLeft: x(6.9) - 4, before: true, label: 'Up to 6.9' },
    ]);
  });

  it('draw nothing before a guess', () => {
    expect(boundLines(null, l)).toEqual({ ratings: [], years: [] });
  });
});

describe('the year on the map', () => {
  // The Matrix's own case: nothing else on its map is from 1999, and
  // there are cards from the years either side of it, so the row it
  // opens closes a gap.
  const matrixLike = todayOf({
    cards: [
      { id: 'c1', year: 1971, rating: 5.5, md: 0 },
      { id: 'c2', year: 1993, rating: 7.3, md: 811 },
      { id: 'c3', year: 1994, rating: 6.3, md: 701 },
      { id: 'c9', year: 1998, rating: 6.6, md: 0 },
      { id: 'c6', year: 2000, rating: 7.0, md: 0 },
      { id: 'c4', year: 2003, rating: 7.2, md: 515 },
    ],
  });
  const bought = gameOf([yearEntry(1999)], { pts: 800 });
  const at = (l: GridLayout, y: number) => l.rows.find((r) => r.year === y)!;
  const span = (l: GridLayout) => l.rows.map((r) => [r.year, r.top, r.height]);

  it('is known from the entry Year logs, and from nothing before it', () => {
    expect(yearOf(null)).toBeNull();
    expect(yearOf(gameOf([guess(BOUND, 'newer', 'higher')]))).toBeNull();
    expect(yearOf(bought)).toBe(1999);
  });

  it('never reaches the board before it is bought', () => {
    const g = gameOf([
      { type: 'flip', card: 'c4', cost: 55, relative: { shared: 4 } },
      guess(BOUND, 'newer', 'higher', { shared: [LANA] }),
    ]);
    const { payload, settings } = boardPayload(matrixLike, g);
    expect(JSON.stringify(payload)).not.toContain('1999');
    expect(payload.anchor.year).toBe(0);
    expect(settings.highlightYear).toBe(false);
  });

  it('opens an empty row between 1998 and 2000, and the gap mark between them goes', () => {
    const was = layoutFor(matrixLike, gameOf([]));
    const now = layoutFor(matrixLike, bought);
    // Before: 1998 and 2000 do not meet, and the 10px mark says so.
    expect(was.rows.map((r) => r.year)).not.toContain(1999);
    expect(at(was, 2000).top).toBe(at(was, 1998).top + at(was, 1998).height + 10);
    // After: three years in a row, meeting.
    expect(at(now, 1999).top).toBe(at(now, 1998).top + at(now, 1998).height);
    expect(at(now, 2000).top).toBe(at(now, 1999).top + at(now, 1999).height);
    // The plot grows by the row, less the mark it closed.
    expect(rowsBottom(now) - rowsBottom(was)).toBe(114 - 10);
    expect(plotSize(now, false).h - plotSize(was, false).h).toBe(104);
  });

  it('is one lane tall: 114 on a desktop, 96 on a phone', () => {
    expect(at(layoutFor(matrixLike, bought), 1999)).toMatchObject({ height: 114, lanes: 1 });
    expect(at(layoutFor(matrixLike, bought, 390, true), 1999)).toMatchObject({ height: 96, lanes: 1 });
  });

  it('holds the row open with a placeholder the layout keeps and the board never draws', () => {
    // Rated, so the board's settings, which take unrated movies off it,
    // let it through to make its row.
    const { payload } = boardPayload(matrixLike, bought);
    const held = payload.films.find(([id]) => id === YEAR_PLACEHOLDER);
    expect(held?.[1]).toBe(1999);
    expect(held?.[2]).toEqual(expect.any(Number));
    // Taken off the cards again: nothing to draw, press, count or turn.
    const l = layoutFor(matrixLike, bought);
    expect(l.cards.map((c) => c.film.id).sort()).toEqual(matrixLike.cards.map((c) => c.id).sort());
    expect(l.anchor).toBeNull();
  });

  it('lights the year’s band and label as the map lights a searched year, and nothing before', () => {
    const l = layoutFor(matrixLike, bought);
    expect(l.rows.filter((r) => r.anchorYear).map((r) => r.year)).toEqual([1999]);
    expect(bandClass(at(l, 1999))).toContain('cd-band-anchor');
    expect(railYearClass(at(l, 1999))).toContain('cd-rail-anchor');
    expect(layoutFor(matrixLike, gameOf([])).rows.some((r) => r.anchorYear)).toBe(false);
  });

  it('adds no row when a card shares the year, and marks that one', () => {
    // The fixture's c7 is from 1999.
    const today = todayOf();
    const g = gameOf([yearEntry(1999)]);
    const was = layoutFor(today, gameOf([]));
    const now = layoutFor(today, g);
    expect(boardPayload(today, g).payload.films.some(([id]) => id === YEAR_PLACEHOLDER)).toBe(false);
    expect(span(now)).toEqual(span(was));
    expect(seamsFor(was.rows, now.rows).size).toBe(0);
    expect(now.rows.filter((r) => r.anchorYear).map((r) => r.year)).toEqual([1999]);
  });

  it('grows out of the seam between its neighbours', () => {
    const was = layoutFor(matrixLike, gameOf([]));
    const now = layoutFor(matrixLike, bought);
    expect(seamsFor(was.rows, now.rows)).toEqual(new Map([[1999, at(was, 2000).top]]));
  });

  it('fades every other year’s row, and leaves the cards at full strength', () => {
    const rows = rowBoundsOf(bought);
    expect(rows).toEqual({ yLo: 1999, yHi: 1999, rLo: 0, rHi: 10 });
    for (const y of [1971, 1998, 2000, 2003]) expect(yearRuledOut(y, rows), `${y}`).toBe(true);
    expect(yearRuledOut(1999, rows)).toBe(false);
    // The cards go by the guesses alone, and there are none.
    expect(boundsOf(bought)).toBeNull();
    for (const c of matrixLike.cards) expect(inBounds(c, boundsOf(bought)), c.id).toBe(true);
  });

  it('leaves a card a wrong guess ruled out at its fade', () => {
    const g = gameOf([guess(BABY, 'newer', 'higher'), yearEntry(1999)]);
    expect(inBounds({ year: 1971, rating: 5.5 }, boundsOf(g))).toBe(false);
    expect(inBounds({ year: 2003, rating: 7.2 }, boundsOf(g))).toBe(true);
    // Its row is out either way.
    expect(rowBoundsOf(g)).toEqual({ yLo: 1999, yHi: 1999, rLo: 6.4, rHi: 10 });
  });

  it('keeps the exact year on the rows after a wrong guess, and adds the guess’s rating line', () => {
    const g = gameOf([yearEntry(1999), guess(RELOADED, 'older', 'higher')]);
    expect(rowBoundsOf(g)).toEqual({ yLo: 1999, yHi: 1999, rLo: 7.3, rHi: 10 });
    const lines = boundLines(rowBoundsOf(g), layoutFor(matrixLike, g));
    expect(lines.ratings.map((r) => r.label)).toEqual(['7.3+']);
    // "2002 or earlier" gives way to the year.
    expect(lines.years.map((y) => y.label)).toEqual(['1999', '']);
  });

  it('rules both edges of its row across the board, with one tag inside it, just past the rail', () => {
    for (const [width, compact, railW] of [
      [1440, false, 72],
      [390, true, 52],
    ] as const) {
      const l = layoutFor(matrixLike, bought, width, compact);
      const r = at(l, 1999);
      expect(boundLines(rowBoundsOf(bought), l).years).toEqual([
        { kind: 'from', y: r.top, labelTop: r.top + 6, labelLeft: railW + 10, label: '1999' },
        { kind: 'until', y: r.top + r.height, labelTop: r.top + r.height - 24, labelLeft: railW + 10, label: '' },
      ]);
    }
  });

  it('moves the tag to the bottom line on the map’s first year, which has no line over it', () => {
    const first = todayOf({ cards: matrixLike.cards.filter((c) => c.year > 1999) });
    const l = layoutFor(first, bought);
    const r = at(l, 1999);
    expect(l.rows[0]).toBe(r);
    const { years } = boundLines(rowBoundsOf(bought), l);
    expect(years).toEqual([
      { kind: 'until', y: r.top + r.height, labelTop: r.top + r.height - 24, labelLeft: 82, label: '1999' },
    ]);
    // Still inside the row: the tag is 18px tall.
    expect(years[0].labelTop).toBeGreaterThanOrEqual(r.top);
    expect(years[0].labelTop + 18).toBeLessThanOrEqual(r.top + r.height);
  });

  it('draws no line under the map’s last year', () => {
    const last = todayOf({ cards: matrixLike.cards.filter((c) => c.year < 1999) });
    const l = layoutFor(last, bought);
    const r = at(l, 1999);
    expect(l.rows[l.rows.length - 1]).toBe(r);
    expect(boundLines(rowBoundsOf(bought), l).years).toEqual([
      { kind: 'from', y: r.top, labelTop: r.top + 6, labelLeft: 82, label: '1999' },
    ]);
  });

  it('is filled by the answer at the end, so nothing moves and the plot keeps its height', () => {
    for (const [width, compact] of [
      [1440, false],
      [390, true],
    ] as const) {
      const play = layoutFor(matrixLike, bought, width, compact);
      const end = layoutFor(matrixLike, ended(bought, true), width, compact);
      expect(span(end)).toEqual(span(play));
      expect(seamsFor(play.rows, end.rows).size).toBe(0);
      expect(plotSize(end, compact)).toEqual(plotSize(play, compact));
      expect(end.anchor?.top).toBe(at(play, 1999).top + 12);
      // Its band keeps the wash.
      expect(at(end, 1999).anchorYear).toBe(true);
    }
    // And the lines go: the answer is on the board.
    expect(rowBoundsOf(ended(bought, true))).toBeNull();
  });

  it('is scrolled to: its cards, or a row opened empty at its left edge', () => {
    const l = layoutFor(matrixLike, bought);
    expect(yearTargets(l, 1999)).toEqual([{ left: 72 + 8, top: at(l, 1999).top + 12 }]);
    // Centred in the part the panel leaves clear, which keeps it at the
    // left edge.
    const view = { w: 1366 - 380 - 32, h: 700 };
    expect(scrollTarget(yearTargets(l, 1999), { w: 168, h: 90 }, view)).toEqual({
      left: 0,
      top: Math.max(0, at(l, 1999).top + 12 + 45 - (26 + 700) / 2),
    });
    const shared = layoutFor(todayOf(), gameOf([yearEntry(1999)]));
    expect(yearTargets(shared, 1999)).toEqual([shared.cards.find((c) => c.film.id === 'c7')]);
    expect(yearTargets(l, 1950)).toEqual([]);
  });
});

describe('a card’s label', () => {
  const card = { id: 'c5', year: 1996, rating: 7.3, md: 0 };

  it('says where a blank card is and what turning it costs', () => {
    expect(cardLabel(card, undefined)).toBe('Hidden movie from 1996. Turn it over for 55 points');
  });

  it('names a face-up card', () => {
    expect(cardLabel(card, { kind: 'film', film: BOUND })).toBe('Bound, 1996, rated 7.3');
  });

  it('says how near a close relative is, never what it is', () => {
    expect(cardLabel(card, { kind: 'relative', shared: 3 })).toBe(
      'A close relative of today’s movie, from 1996, rated 7.3. It shares 3 people with it',
    );
  });

  it('names the answer at the end', () => {
    expect(answerLabel(MATRIX)).toBe('Today’s movie: The Matrix, 1999, rated 8.7');
  });
});

describe('the feed', () => {
  const today = todayOf();

  it('opens with the starting three, each a chip to its card', () => {
    const [start] = feedOf(today, gameOf([]));
    expect(start).toMatchObject({ label: 'Start', text: 'Three movies from its map are showing.', now: true, cost: '' });
    expect(start.chips).toEqual([
      { card: 'c1', title: 'Man and Boy', film: MAN_AND_BOY, aria: 'Show Man and Boy on the map' },
      { card: 'c2', title: 'Searching for Bobby Fischer', film: BOBBY, aria: 'Show Searching for Bobby Fischer on the map' },
      { card: 'c3', title: 'Baby’s Day Out', film: BABY, aria: 'Show Baby’s Day Out on the map' },
    ]);
  });

  it('washes only the latest entry while the game is on', () => {
    const feed = feedOf(today, gameOf([{ type: 'genres', cost: 80, genres: ['Action', 'Sci-Fi'] }]));
    expect(feed.map((e) => e.now)).toEqual([false, true]);
  });

  it('says a card turned over, with what it cost', () => {
    const [, e] = feedOf(today, gameOf([{ type: 'flip', card: 'c5', cost: 55, film: BOUND }]));
    expect(e).toMatchObject({ label: 'Turned over', cost: '−55', text: '' });
    expect(e.chips.map((c) => c.title)).toEqual(['Bound']);
  });

  it('keeps a close relative’s title hidden while the game is on', () => {
    const [, e] = feedOf(today, gameOf([{ type: 'flip', card: 'c4', cost: 55, relative: { shared: 4 } }]));
    expect(e.text).toBe('A close relative: it shares 4 people with today’s movie, so its title stays hidden.');
    expect(e.chips).toEqual([{ card: 'c4', title: 'Close relative', film: null, aria: 'Show the close relative on the map' }]);
  });

  it('names a close relative once it has been guessed, and at the end', () => {
    const log: DailyEntry[] = [
      { type: 'flip', card: 'c4', cost: 55, relative: { shared: 4 } },
      guess(RELOADED, 'older', 'higher', { card: 'c4' }),
    ];
    expect(feedOf(today, gameOf(log))[1].chips[0].title).toBe('The Matrix Reloaded');
    const done = ended(gameOf([log[0]]), false);
    expect(feedOf(today, done)[1]).toMatchObject({ text: '', chips: [{ title: 'The Matrix Reloaded' }] });
  });

  it('says who a person clue found, and how many cards they mark', () => {
    const feed = feedOf(
      today,
      gameOf([
        { type: 'person', role: 'director', cost: 150, people: [LANA, LILLY] },
        { type: 'person', role: 'director', cost: 150, people: [LANA] },
        { type: 'person', role: 'actor', cost: 150, people: [KEANU] },
      ]),
    );
    expect(feed[1]).toMatchObject({
      label: 'Directors',
      text: 'It was directed by:',
      faces: [LANA, LILLY],
      more: '2 of their movies are marked on the map.',
      cost: '−150',
    });
    expect(feed[2].label).toBe('Director');
    expect(feed[3]).toMatchObject({ label: 'Actor', text: 'It stars:', faces: [KEANU] });
  });

  it('says how many movies are marked in the singular too, and when none are', () => {
    expect(markedText(1)).toBe('1 of their movies is marked on the map.');
    expect(markedText(0)).toBe('None of their movies are on the map.');
    expect(markedText(14)).toBe('14 of their movies are marked on the map.');
  });

  it('lists the genres, and says the year and that the map marks it', () => {
    const feed = feedOf(today, gameOf([{ type: 'genres', cost: 80, genres: ['Action', 'Sci-Fi'] }, yearEntry(1999)]));
    expect(feed[1]).toMatchObject({ label: 'Genres', text: 'Action, Sci-Fi', cost: '−80' });
    expect(feed[2]).toMatchObject({
      label: 'Year',
      cost: '−200',
      text: 'It came out in 1999. The map marks where that year sits.',
      faces: [],
      chips: [],
    });
    // No text clue is left to quote.
    expect(Object.keys(feed[2])).not.toContain('quote');
  });

  it('says what a wrong guess shares, and where the answer is from it', () => {
    const feed = feedOf(
      today,
      gameOf([
        guess(BOUND, 'newer', 'higher', { shared: [LANA, LILLY, PANTOLIANO] }),
        guess(film('tt0000099', 'Casablanca', 1942, 8.5), 'newer', 'higher', { cost: 150 }),
      ]),
    );
    expect(feed[1]).toMatchObject({
      label: 'Not it',
      tone: 'miss',
      text: 'Bound (1996) is linked to it through:',
      faces: [LANA, LILLY, PANTOLIANO],
      more: 'Today’s movie is newer and rated higher.',
    });
    expect(feed[2]).toMatchObject({
      text: 'Casablanca (1942) shares no one with today’s movie.',
      faces: [],
      cost: '−150',
      now: true,
    });
  });

  it('writes a guess without a year as its title alone', () => {
    const feed = feedOf(today, gameOf([guess(film('tt9', 'Lost Reel', 0, null), null, null)]));
    expect(feed[1]).toMatchObject({ text: 'Lost Reel shares no one with today’s movie.', more: '' });
  });

  it('says how the game ended, naming the answer', () => {
    const won = ended(gameOf([{ type: 'win' }]), true);
    expect(feedOf(today, won)[1]).toMatchObject({ label: 'Got it', tone: 'win', text: 'The Matrix (1999) is today’s movie.', now: false });
    const shown = ended(gameOf([{ type: 'gaveup' }]), false);
    expect(feedOf(today, shown)[1]).toMatchObject({ label: 'Answer shown', tone: 'end', text: 'Today’s movie was The Matrix (1999).' });
    const out = ended(gameOf([guess(BOUND, 'newer', 'higher'), { type: 'out' }]), false);
    expect(feedOf(today, out).map((e) => e.label)).toEqual(['Start', 'Not it', 'Out of points']);
    expect(feedOf(today, out).some((e) => e.now)).toBe(false);
  });

  it('is empty before Play', () => {
    expect(feedOf(today, null)).toEqual([]);
  });
});

describe('where a wrong guess puts the answer', () => {
  it('reads both halves, either half, or neither', () => {
    expect(directionText('older', 'lower')).toBe('Today’s movie is older and rated lower.');
    expect(directionText('same', 'same')).toBe('Today’s movie is from the same year and has the same rating.');
    expect(directionText('newer', null)).toBe('Today’s movie is newer.');
    expect(directionText(null, 'higher')).toBe('Today’s movie is rated higher.');
    expect(directionText(null, null)).toBe('');
  });
});

describe('the clue buttons', () => {
  const today = todayOf();

  it('offer Directors, Actor, Genres and Year at their prices', () => {
    const b = clueButtons(today, gameOf([]));
    expect(b.map((x) => [x.clue, x.label, x.tag, x.off])).toEqual([
      ['director', 'Directors', '150', false],
      ['actor', 'Actor', '150', false],
      ['genres', 'Genres', '80', false],
      ['year', 'Year', '200', false],
    ]);
    expect(b[0].aria).toBe('Directors for 150 points');
    expect(b[3]).toMatchObject({ priced: true, aria: 'Year for 200 points' });
  });

  it('say "Seen" for the year once it is bought, and take no more for it', () => {
    const b = clueButtons(today, gameOf([yearEntry(1999)], { pts: 800 }))[3];
    expect(b).toMatchObject({ tag: 'Seen', priced: false, off: true, aria: 'Year: seen' });
  });

  it('keep the year’s price showing when the points will not cover it', () => {
    const b = clueButtons(today, gameOf([], { pts: 199 }))[3];
    expect(b).toMatchObject({ tag: '200', priced: true, off: true, aria: 'Year for 200 points' });
    expect(clueButtons(today, gameOf([], { pts: 200 }))[3].off).toBe(false);
  });

  it('say "Director" when the answer has one', () => {
    expect(clueButtons(todayOf({ clues: { directors: 1, cast: 6 } }), gameOf([]))[0].label).toBe('Director');
  });

  it('say "Known" once there is nobody left to buy, and "Seen" once a clue is bought', () => {
    const g = gameOf(
      [
        { type: 'person', role: 'director', cost: 150, people: [LANA, LILLY] },
        { type: 'genres', cost: 80, genres: ['Action'] },
      ],
      { known: [LANA, LILLY] },
    );
    const b = clueButtons(today, g);
    expect(b[0]).toMatchObject({ tag: 'Known', priced: false, off: true, aria: 'Directors: already known' });
    expect(b[2]).toMatchObject({ tag: 'Seen', priced: false, off: true, aria: 'Genres: seen' });
    expect(b[1]).toMatchObject({ tag: '150', priced: true, off: false });
  });

  it('count cast found by guesses towards "Known"', () => {
    const cast = [KEANU, FISHBURNE, FOSTER, PANTOLIANO];
    const b = clueButtons(todayOf({ clues: { directors: 2, cast: 4 } }), gameOf([], { known: cast }));
    expect(b[1].tag).toBe('Known');
  });

  it('are off when the points will not cover them, or the game is over', () => {
    const b = clueButtons(today, gameOf([], { pts: 120 }));
    expect(b.map((x) => x.off)).toEqual([true, true, false, true]);
    expect(clueButtons(today, ended(gameOf([]), true)).every((x) => x.off)).toBe(true);
    expect(clueButtons(today, null).every((x) => x.off)).toBe(true);
  });

  it('come with a hint that says what the next wrong guess costs', () => {
    expect(hintText(false, 150)).toBe('Click a blank card to turn it over. Your next wrong guess costs 150.');
    expect(hintText(true, 100)).toBe('Tap a blank card to turn it over. Your next wrong guess costs 100.');
  });
});

describe('the guess list', () => {
  const godfather = { id: 'tt0068646', title: 'The Godfather' };
  const partII = { id: 'tt0071562', title: 'The Godfather Part II' };
  const seen = (q: string, hits: { id: string; title: string }[], failed = false): GuessFound<{ id: string; title: string }> => ({
    q,
    hits,
    failed,
  });

  it('offers the rows found for exactly what is typed', () => {
    expect(guessOptions('godfather part ii', seen('godfather part ii', [partII, godfather]))).toEqual({
      rows: [partII, godfather],
      held: [],
      note: '',
    });
  });

  it('offers nothing to choose from words the reader has typed past, only holds them faded', () => {
    // Enter on the top row would fill the field with The Godfather, and a
    // second Enter would make it a wrong guess.
    const { rows, held, note } = guessOptions('godfather part ii', seen('godfather part', [godfather, partII]));
    expect(rows).toEqual([]);
    expect(held).toEqual([godfather, partII]);
    expect(note).toBe('');
  });

  it('says it is searching while there is nothing yet to hold', () => {
    expect(guessOptions('heat', null)).toEqual({ rows: [], held: [], note: 'Searching…' });
    expect(guessOptions('heat', seen('hea', []))).toEqual({ rows: [], held: [], note: 'Searching…' });
    expect(guessOptions('heat', seen('hea', [], true))).toEqual({ rows: [], held: [], note: 'Searching…' });
  });

  it('says the search never answered, never that nothing matched', () => {
    const { rows, note } = guessOptions('heat', seen('heat', [], true));
    expect(rows).toEqual([]);
    expect(note).toBe(UNREACHABLE);
    expect(note).not.toMatch(/No movies match/);
  });

  it('says nothing matched when that is the catalog’s answer', () => {
    expect(guessOptions('qqzx', seen('qqzx', []))).toEqual({ rows: [], held: [], note: 'No movies match “qqzx”' });
  });
});

describe('the result', () => {
  it('joins a list without the Oxford comma', () => {
    expect(andList([])).toBe('');
    expect(andList(['4 cards'])).toBe('4 cards');
    expect(andList(['4 cards', '1 person'])).toBe('4 cards and 1 person');
    expect(andList(['4 cards', '1 person', 'the genres'])).toBe('4 cards, 1 person and the genres');
  });

  it('counts what was used, in the singular and the plural', () => {
    expect(
      usedList([
        { type: 'start' },
        { type: 'flip', card: 'c5', cost: 55, film: BOUND },
        { type: 'person', role: 'director', cost: 150, people: [LANA, LILLY] },
        yearEntry(1999),
        guess(BOUND, 'newer', 'higher'),
      ]),
    ).toEqual(['1 card', '2 people', 'the year', '1 wrong guess']);
    expect(
      usedList([
        { type: 'flip', card: 'c5', cost: 55, film: BOUND },
        { type: 'flip', card: 'c6', cost: 55, film: BOUND },
        { type: 'person', role: 'actor', cost: 150, people: [KEANU] },
        { type: 'genres', cost: 80, genres: [] },
        guess(BOUND, 'newer', 'higher'),
        guess(BABY, 'newer', 'higher'),
      ]),
    ).toEqual(['2 cards', '1 person', 'the genres', '2 wrong guesses']);
  });

  it('heads a solve with its points, and anything else with none', () => {
    expect(resultTitle(ended(gameOf([], { pts: 6950 }), true))).toBe('6,950 points');
    expect(resultTitle(ended(gameOf([], { pts: 0 }), false))).toBe('No points today');
  });

  it('joins the year to the others, after the genres', () => {
    const g = ended(
      gameOf(
        [
          { type: 'flip', card: 'c5', cost: 55, film: BOUND },
          { type: 'flip', card: 'c6', cost: 55, film: BOUND },
          { type: 'genres', cost: 80, genres: ['Action'] },
          yearEntry(1999),
          { type: 'win' },
        ],
        { pts: 610 },
      ),
      true,
    );
    expect(resultSub(g)).toBe('Solved in 3:12. You used 2 cards, the genres and the year.');
  });

  it('says how long a solve took and what it used', () => {
    const g = ended(gameOf([{ type: 'flip', card: 'c5', cost: 55, film: BOUND }, guess(BOUND, 'newer', 'higher'), { type: 'win' }], { pts: 845 }), true);
    expect(resultSub(g)).toBe('Solved in 3:12. You used 1 card and 1 wrong guess.');
    expect(resultSub(ended(gameOf([{ type: 'win' }]), true))).toBe('Solved in 3:12, without spending a point.');
  });

  it('says why a game scored nothing', () => {
    expect(resultSub({ ...ended(gameOf([{ type: 'gaveup' }]), false), gaveUp: true })).toBe('You asked for the answer.');
    expect(resultSub(ended(gameOf([{ type: 'out' }]), false))).toBe('Your points ran out.');
  });
});

describe('the streak', () => {
  const won = ended(gameOf([{ type: 'win' }], { pts: 700 }), true);
  const lost = ended(gameOf([{ type: 'gaveup' }], { pts: 0, gaveUp: true }), false);

  it('adds today to the run coming in once today is solved here', () => {
    expect(streakAfter({ now: 0, before: 3 }, won)).toEqual({ now: 4, before: 0 });
  });

  it('is what the server said for a game already over, or not over yet', () => {
    expect(streakAfter({ now: 4, before: 0 }, won)).toEqual({ now: 4, before: 0 });
    expect(streakAfter({ now: 0, before: 3 }, gameOf([]))).toEqual({ now: 0, before: 3 });
    expect(streakAfter({ now: 0, before: 3 }, lost)).toEqual({ now: 0, before: 3 });
  });

  it('shows on the intro as the run coming in until today is over', () => {
    expect(introStreak({ now: 0, before: 3 }, null)).toBe(3);
    expect(introStreak({ now: 0, before: 3 }, gameOf([]))).toBe(3);
    expect(introStreak({ now: 0, before: 3 }, won)).toBe(4);
    expect(introStreak({ now: 0, before: 3 }, lost)).toBe(0);
  });

  it('says a broken run ended', () => {
    const [, broke] = statsOf(lost, null, { now: 0, before: 3 });
    expect(broke).toEqual({ value: 0, suffix: '', caption: 'days in a row. Your 3-day streak ended.' });
    const [, none] = statsOf(lost, null, { now: 0, before: 0 });
    expect(none.caption).toBe('days in a row');
    const [, one] = statsOf(won, null, { now: 0, before: 0 });
    expect(one).toEqual({ value: 1, suffix: '', caption: 'day in a row' });
    expect(statsOf(won, null, { now: 0, before: 3 })[1].caption).toBe('days in a row');
  });
});

describe('the result’s first number', () => {
  const board = { beat: 72, solved: 91 };

  it('is the share who scored less, for a solve', () => {
    const won = ended(gameOf([{ type: 'win' }]), true);
    expect(statsOf(won, board, { now: 0, before: 0 })[0]).toEqual({ value: 72, suffix: '%', caption: 'of players scored less' });
  });

  it('is the share who got it, for anything else', () => {
    const lost = ended(gameOf([{ type: 'out' }], { pts: 0 }), false);
    expect(statsOf(lost, board, { now: 0, before: 0 })[0]).toEqual({ value: 91, suffix: '%', caption: 'of players got it today' });
  });

  it('waits for the board', () => {
    expect(statsOf(ended(gameOf([]), true), null, { now: 0, before: 0 })[0].value).toBeNull();
  });
});

describe('sharing a result', () => {
  it('fills the bar to the nearest hundred points', () => {
    expect(shareBar(0)).toBe('▱▱▱▱▱▱▱▱▱▱');
    expect(shareBar(695)).toBe('▰▰▰▰▰▰▰▱▱▱');
    expect(shareBar(1000)).toBe('▰▰▰▰▰▰▰▰▰▰');
  });

  it('copies the number, the score and the bar, and where to play', () => {
    const won = ended(gameOf([{ type: 'win' }], { pts: 695 }), true);
    expect(shareText(142, won, 'https://cinedikt.com')).toBe(
      'Cinedikt Daily No. 142\n695 points in 3:12\n▰▰▰▰▰▰▰▱▱▱\nhttps://cinedikt.com/daily',
    );
    const lost = ended(gameOf([{ type: 'gaveup' }], { pts: 0 }), false);
    expect(shareText(142, lost, 'https://cinedikt.com')).toBe(
      'Cinedikt Daily No. 142\nMissed it\n▱▱▱▱▱▱▱▱▱▱\nhttps://cinedikt.com/daily',
    );
  });

  it('puts the score and the time on the card, or "Missed"', () => {
    expect(shareLine(ended(gameOf([], { pts: 1000 }), true))).toBe('1,000 points · 3:12');
    expect(shareLine(ended(gameOf([], { pts: 0 }), false))).toBe('Missed');
  });
});

describe('the leaderboard', () => {
  const today: DailyBoard = {
    tab: 'today',
    total: 61240,
    you: { rank: 4211, pts: 695, secs: 192, listed: true },
    rows: [
      { rank: 1, name: 'Neo Kimble', hue: 40, pts: 1000, secs: 17, you: false },
      { gap: true },
      { rank: 4211, name: 'Trinity Kimble', hue: 200, pts: 695, secs: 192, you: true },
      { rank: 4212, name: 'Ellen Hart', hue: 300, pts: 0, secs: 400, you: false },
    ],
    beat: 72,
    solved: 91,
  };

  it('draws the reader as "You", with their name under it', () => {
    const lines = boardLines(today, 'Trinity Kimble', 4);
    expect(lines[0]).toMatchObject({ gap: false, rank: '1', name: 'Neo Kimble', code: 'NK', value: '1,000 · 0:17', sub: '' });
    expect(lines[1]).toMatchObject({ gap: true });
    expect(lines[2]).toMatchObject({ rank: '4,211', you: true, name: 'You', sub: 'Trinity Kimble', code: 'You', value: '695 · 3:12' });
    expect(lines[3]).toMatchObject({ value: 'Missed', missed: true, cells: null });
  });

  it('says a reader who is not listed is not on the board yet', () => {
    const lines = boardLines({ ...today, you: { ...today.you!, listed: false } }, 'Trinity Kimble', 4);
    expect(lines[2]).toMatchObject({ sub: 'Not on the board yet' });
  });

  it('gives each week row a cell for each day so far, Monday first', () => {
    const week: DailyBoard = {
      ...today,
      tab: 'week',
      rows: [{ rank: 1, name: 'Neo Kimble', hue: 40, pts: 1610, you: false, days: [610, 0, null, 1000, null, null, null] }],
    };
    const [line] = boardLines(week, 'Trinity Kimble', 4);
    expect(line).toMatchObject({ value: '1,610', missed: false });
    expect(line.gap === false && line.cells).toEqual([
      { h: 61, missed: false, played: true, tip: 'Monday: 610 points' },
      { h: 0, missed: true, played: true, tip: 'Tuesday: missed' },
      { h: 0, missed: false, played: false, tip: 'Wednesday: not played' },
      { h: 100, missed: false, played: true, tip: 'Thursday: 1,000 points' },
    ]);
  });

  it('never fills a day’s cell to less than a sliver', () => {
    expect(dayCell(30, 4)).toEqual({ h: 8, missed: false, played: true, tip: 'Friday: 30 points' });
    expect(dayCell(undefined, 6).tip).toBe('Sunday: not played');
  });

  it('sizes the rank column for the longest rank', () => {
    expect(rankWidth(boardLines(today, 'x', 4))).toBe(Math.ceil(5 * 7.4));
    expect(rankWidth([])).toBe(0);
  });

  it('writes initials from the first letter of each word', () => {
    expect(initialsOf('Trinity Kimble')).toBe('TK');
    expect(initialsOf('Neo')).toBe('N');
  });

  it('says how many are playing, and how the reader shows', () => {
    expect(boardNote('today', 61240, 'Trinity Kimble', true)).toBe(
      '61,240 players so far today. You show as Trinity Kimble.',
    );
    expect(boardNote('today', 1, 'Trinity Kimble', false)).toBe(
      '1 player so far today. You’ll show as Trinity Kimble once you’ve played on a few days.',
    );
    expect(boardNote('week', 83500, 'x', true)).toBe(
      '83,500 players this week. Each day adds the points you kept. The week starts on Monday.',
    );
  });

  it('asks for today’s board whichever tab is showing, since the result’s first number is from it', () => {
    expect(boardsWanted('today')).toEqual(['today']);
    expect(boardsWanted('week')).toEqual(['today', 'week']);
  });

  it('forgets a board that failed when it is wanted again, so it is asked for again', () => {
    const failed: BoardsSeen<DailyBoard> = { today: 'failed' };
    expect(dropFailed(failed, boardsWanted('today'))).toEqual({});
    // A board that came is kept.
    const mixed: BoardsSeen<DailyBoard> = { today, week: 'failed' };
    expect(dropFailed(mixed, boardsWanted('week'))).toEqual({ today });
    expect(dropFailed(mixed, boardsWanted('today'))).toBe(mixed);
    // The week's tab forgets both.
    expect(dropFailed({ today: 'failed', week: 'failed' }, boardsWanted('week'))).toEqual({});
  });

  it('keeps the same boards when nothing failed, so nothing is drawn again', () => {
    const had: BoardsSeen<DailyBoard> = { today };
    expect(dropFailed(had, ['today', 'week'])).toBe(had);
  });

  it('colours an avatar in the player’s hue for each theme', () => {
    expect(avatarColours(205, 'dark')).toEqual({ fill: 'oklch(0.78 0.12 205)', ink: 'oklch(0.2 0.03 205)' });
    expect(avatarColours(205, 'light')).toEqual({ fill: 'oklch(0.55 0.15 205)', ink: '#ffffff' });
  });
});

describe('the intro', () => {
  it('leads into the game, and back to it as the rules', () => {
    expect(introButton(142, null)).toBe('Play No. 142');
    expect(introButton(142, gameOf([]))).toBe('Back to the game');
    expect(introButton(142, ended(gameOf([]), true))).toBe('Back to your result');
  });

  it('counts today’s players, one or many', () => {
    expect(playedText(61240)).toEqual({ count: '61,240', rest: ' people have played today.' });
    expect(playedText(1)).toEqual({ count: '1', rest: ' person has played today.' });
  });

  it('counts nobody before anyone has played, as the banner does', () => {
    // Every puzzle opens at nought: "0 people have played today" would
    // say the game is empty rather than new.
    expect(playedText(0)).toBeNull();
  });
});

describe('a place this week', () => {
  it('is written with its suffix, "th" for the teens, and en-GB thousands', () => {
    expect([1, 2, 3, 4, 11, 12, 13, 21, 101, 111, 1204].map(ord)).toEqual([
      '1st',
      '2nd',
      '3rd',
      '4th',
      '11th',
      '12th',
      '13th',
      '21st',
      '101st',
      '111th',
      '1,204th',
    ]);
    expect([22, 23, 102, 112, 1203, 1000].map(ord)).toEqual(['22nd', '23rd', '102nd', '112th', '1,203rd', '1,000th']);
  });

  it('says where the reader stands this week, or nothing', () => {
    expect(standingText({ rank: 1204, players: 83500 })).toBe('1,204th this week');
    expect(standingText(null)).toBe('');
    expect(standingText(undefined)).toBe('');
  });
});

describe('time', () => {
  it('writes a game’s clock as minutes and seconds', () => {
    expect(mmss(0)).toBe('0:00');
    expect(mmss(7.4)).toBe('0:07');
    expect(mmss(192)).toBe('3:12');
    expect(mmss(3725)).toBe('62:05');
    expect(mmss(-4)).toBe('0:00');
  });

  it('counts down to the next map in hours, minutes and seconds', () => {
    expect(countdown(42_423_000)).toBe('11:47:03');
    expect(countdown(59_999)).toBe('0:00:59');
    expect(countdown(-1)).toBe('0:00:00');
  });

  it('runs on the server’s clock', () => {
    const local = Date.parse('2026-10-08T11:59:50Z');
    const offset = clockOffset('2026-10-08T12:00:00Z', local);
    expect(offset).toBe(10_000);
    expect(secondsSince('2026-10-08T11:58:00Z', local, offset)).toBe(120);
    expect(clockOffset('not a time', local)).toBe(0);
    expect(secondsSince('not a time', local, 0)).toBe(0);
  });
});

describe('the reader’s midnight', () => {
  afterEach(() => {
    vi.useRealTimers();
  });

  // Tokyo's midnight going into 9 October is 15:00 UTC on the 8th. This
  // clock is 10 s behind the server's, so it reads 14:59:50 at midnight.
  const TOKYO_NEXT = '2026-10-09T00:00:00+09:00';
  const AT_MIDNIGHT_HERE = Date.parse('2026-10-08T14:59:50Z');

  it('comes once, when the server’s clock reaches it, however this one is set', () => {
    vi.useFakeTimers();
    vi.setSystemTime(AT_MIDNIGHT_HERE - 60_000);
    const struck = vi.fn();
    watchMidnight(TOKYO_NEXT, 10_000, struck);
    vi.advanceTimersByTime(59_999);
    expect(struck).not.toHaveBeenCalled();
    vi.advanceTimersByTime(1);
    expect(struck).toHaveBeenCalledTimes(1);
    vi.advanceTimersByTime(86_400_000);
    expect(struck).toHaveBeenCalledTimes(1);
  });

  it('looks at the clock in steps, so a machine that slept through midnight is caught up within one', () => {
    vi.useFakeTimers();
    vi.setSystemTime(AT_MIDNIGHT_HERE - 4 * 3_600_000);
    const struck = vi.fn();
    watchMidnight(TOKYO_NEXT, 10_000, struck);
    // Asleep: the clock moves on past midnight and no timer runs.
    vi.setSystemTime(AT_MIDNIGHT_HERE + 600_000);
    expect(struck).not.toHaveBeenCalled();
    vi.advanceTimersByTime(MIDNIGHT_STEP_MS);
    expect(struck).toHaveBeenCalledTimes(1);
    expect(MIDNIGHT_STEP_MS).toBeLessThanOrEqual(60_000);
  });

  it('is not waited for when it has already gone, or cannot be read', () => {
    // Asking again would only bring the same answer back, and again.
    vi.useFakeTimers();
    vi.setSystemTime(AT_MIDNIGHT_HERE + 1);
    const struck = vi.fn();
    watchMidnight(TOKYO_NEXT, 10_000, struck);
    watchMidnight('not a time', 0, struck);
    watchMidnight('', 0, struck);
    expect(vi.getTimerCount()).toBe(0);
    vi.advanceTimersByTime(86_400_000);
    expect(struck).not.toHaveBeenCalled();
  });

  it('is called off by what it hands back', () => {
    vi.useFakeTimers();
    vi.setSystemTime(AT_MIDNIGHT_HERE - 3_600_000);
    const struck = vi.fn();
    const stop = watchMidnight(TOKYO_NEXT, 10_000, struck);
    vi.advanceTimersByTime(MIDNIGHT_STEP_MS * 3);
    stop();
    vi.advanceTimersByTime(3_600_000);
    expect(struck).not.toHaveBeenCalled();
    expect(vi.getTimerCount()).toBe(0);
  });

  it('tells a reader part way through a game that its map has ended, and nobody else anything', () => {
    expect(midnightText(gameOf([]))).toBe('That map has ended.');
    expect(midnightText(gameOf([{ type: 'win' }], { phase: 'done', won: true }))).toBe('');
    expect(midnightText(null)).toBe('');
  });
});

describe('the date', () => {
  afterEach(() => {
    vi.unstubAllEnvs();
  });

  it('is read in UTC, with Monday as the first day of the week', () => {
    expect(weekdayOf('2026-10-05')).toBe(0);
    expect(weekdayOf('2026-10-08')).toBe(3);
    expect(weekdayOf('2026-10-11')).toBe(6);
  });

  it('reads as en-GB writes it', () => {
    expect(dailyDayText('2026-10-08')).toBe('Thursday 8 October');
    expect(dailyDayText('2027-01-01')).toBe('Friday 1 January');
    expect(dailyDayText('nonsense')).toBe('');
  });

  it('puts the number and the day in the header, and the number alone on a phone', () => {
    expect(dailyDateText({ no: 142, date: '2026-10-08' }, false)).toBe('No. 142 · Thursday 8 October');
    expect(dailyDateText({ no: 142, date: '2026-10-08' }, true)).toBe('No. 142');
  });

  it('is the puzzle’s calendar date wherever the reader is, never a day either side', () => {
    // The two ends of the world's clocks: midnight UTC is still the 7th
    // in Pago Pago, and local midnight on the 8th in Kiritimati is the
    // 7th in UTC. A date read through either zone would slip a day.
    for (const [zone, localDate] of [
      ['Pacific/Pago_Pago', 7],
      ['Pacific/Kiritimati', 8],
    ] as const) {
      vi.stubEnv('TZ', zone);
      // The zone has taken: this process now reads dates in it.
      expect(new Date('2026-10-08T00:00:00Z').getDate()).toBe(localDate);
      expect(new Date(2026, 9, 8).getUTCDate()).toBe(zone === 'Pacific/Kiritimati' ? 7 : 8);
      expect(dailyDayText('2026-10-08')).toBe('Thursday 8 October');
      expect(dailyDayText('2027-01-01')).toBe('Friday 1 January');
      expect(weekdayOf('2026-10-08')).toBe(3);
      expect(dailyDateText({ no: 142, date: '2026-10-08' }, false)).toBe('No. 142 · Thursday 8 October');
    }
  });
});

describe('scrolling to cards', () => {
  const size = { w: 168, h: 90 };
  const view = { w: 1000, h: 700 };

  it('centres the cards in the part the panel leaves clear, under the rating strip', () => {
    expect(scrollTarget([{ left: 1200, top: 900 }], size, view)).toEqual({
      left: 1284 - 500,
      top: 945 - (26 + 700) / 2,
    });
  });

  it('centres the middle card when they do not all fit', () => {
    const cards = [
      { left: 100, top: 100 },
      { left: 2000, top: 1500 },
      { left: 900, top: 900 },
    ];
    expect(scrollTarget(cards, size, view)).toEqual({ left: 984 - 500, top: 945 - 363 });
  });

  it('never scrolls past the top or the left', () => {
    expect(scrollTarget([{ left: 10, top: 40 }], size, view)).toEqual({ left: 0, top: 0 });
    expect(scrollTarget([], size, view)).toBeNull();
  });
});

describe('a move', () => {
  afterEach(() => {
    vi.unstubAllGlobals();
  });

  it('is named by a key the server accepts', () => {
    const a = newKey();
    expect(a).toMatch(/^[A-Za-z0-9_-]{8,64}$/);
    expect(newKey()).not.toBe(a);
  });

  it('still gets a key where randomUUID is missing', () => {
    vi.stubGlobal('crypto', { getRandomValues: (b: Uint8Array) => b.fill(171) });
    expect(newKey()).toBe('ab'.repeat(16));
  });

  it('is the same request from the same point, and a new one from the next', () => {
    expect(moveSig({ kind: 'flip', card: 'c5' }, 3)).toBe(moveSig({ kind: 'flip', card: 'c5' }, 3));
    expect(moveSig({ kind: 'flip', card: 'c5' }, 3)).not.toBe(moveSig({ kind: 'flip', card: 'c5' }, 4));
    expect(moveSig({ kind: 'buy', clue: 'genres' }, 3)).not.toBe(moveSig({ kind: 'buy', clue: 'year' }, 3));
    expect(moveSig({ kind: 'guess', film: 'tt1' }, 3)).toBe('3:guess:tt1');
    expect(moveSig({ kind: 'reveal' }, 5)).toBe('5:reveal:');
  });
});

describe('a refused move', () => {
  it('says what the prototype says for points, by what was being bought', () => {
    expect(refusalText('points', 'flip')).toBe('Not enough points left for that card.');
    expect(refusalText('points', 'buy')).toBe('Not enough points for that.');
  });

  it('says a guess has been made already', () => {
    expect(refusalText('known', 'guess')).toBe('You’ve already guessed that one.');
  });

  it('says the map has ended when the day has turned', () => {
    expect(refusalText('day', 'flip')).toBe('That map has ended.');
  });

  it('says nothing when it hands back the game as it stands', () => {
    expect(refusalText('stale', 'flip')).toBe('');
  });

  it('falls back to the network’s words for anything it does not know', () => {
    expect(refusalText(null)).toBe('Couldn’t reach Cinedikt. Try again.');
    expect(refusalText('unavailable')).toBe('Couldn’t reach Cinedikt. Try again.');
    expect(refusalText('not-ready')).toBe('Today’s map isn’t ready yet. Try again in a few minutes.');
  });

  it('carries the game back when it is stale', () => {
    const g = gameOf([]);
    expect(staleGame({ error: 'x', reason: 'stale', game: g })).toBe(g);
    expect(staleGame({ error: 'x', reason: 'stale' })).toBeNull();
    expect(staleGame(null)).toBeNull();
  });
});

describe('the name reel', () => {
  const seq = (...xs: number[]) => {
    let i = 0;
    return () => xs[i++ % xs.length];
  };

  it('scrambles the old name, keeping each word’s shape, until the new one arrives', () => {
    const f = reelFrame('Trinity Kimble', null, 7, seq(0.1, 0.5, 0.9));
    expect(f).toHaveLength('Trinity Kimble'.length);
    expect(f[7]).toBe(' ');
    expect(f[0]).toMatch(/[A-Z]/);
    expect(f[8]).toMatch(/[A-Z]/);
    expect(f.slice(1, 7)).toMatch(/^[a-z]+$/);
  });

  it('locks the new name in from the left, and lands on it', () => {
    const to = 'Neo Kimble';
    expect(reelFrame('Trinity Kimble', to, 4, () => 0)).not.toContain('Neo');
    expect(reelFrame('Trinity Kimble', to, 7, () => 0).slice(0, 5)).toBe('Neo K');
    expect(reelFrame('Trinity Kimble', to, REEL_STEPS, () => 0)).toBe(to);
  });

  it('slows as it lands', () => {
    expect([1, 2, 8].map(reelDelay)).toEqual([54, 68, 152]);
    expect(REEL_STEPS).toBe(9);
  });
});

describe('cards turning together', () => {
  it('turn 150ms apart while the game is on', () => {
    expect(staggerDelays(['c1', 'c2', 'c3'], false, null)).toEqual({ c1: 0, c2: 150, c3: 300 });
  });

  it('turn outward from the answer at the end, the furthest no later than 1.3s', () => {
    const today = todayOf();
    const done = ended(gameOf([]), true);
    const l = layoutFor(today, done);
    const d = staggerDelays(['c7', 'c1', 'c8'], true, l);
    // c7 shares the answer's year; c1 is three decades away.
    expect(d.c7).toBeLessThan(d.c1);
    expect(Math.max(...Object.values(d))).toBeLessThanOrEqual(END_STAGGER_MAX_MS);
    expect(Math.min(...Object.values(d))).toBeGreaterThanOrEqual(150);
    const a = l.anchor!;
    const c7 = l.cards.find((c) => c.film.id === 'c7')!;
    expect(d.c7).toBe(Math.round(Math.min(1300, 150 + Math.hypot(c7.left - a.left, c7.top - a.top) * 0.45)));
  });

  it('wait for nothing without an answer to turn from', () => {
    expect(staggerDelays(['c1'], true, null)).toEqual({});
  });
});

describe('the Daily’s motion', () => {
  it('matches the design’s numbers', () => {
    // Cinedikt Daily.dc.html: the flip, the stagger and the ring.
    expect(FLIP_MS).toBe(620);
    expect(FLIP_EASE).toBe('cubic-bezier(0.3, 0.75, 0.25, 1.18)');
    expect(STAGGER_MS).toBe(150);
    expect(FRESH_HOLD_MS).toBe(1900);
    expect(PULSE_MS).toBe(1500);
    // The points, the spend and the shake.
    expect(ROLL_MS).toBe(450);
    expect(SPEND_FLOAT_MS).toBe(1100);
    expect(SHAKE_MS).toBe(420);
    expect(SHAKE_KEYFRAMES.map((k) => k.transform)).toEqual([
      'translateX(0px)',
      'translateX(-9px)',
      'translateX(8px)',
      'translateX(-5px)',
      'translateX(3px)',
      'translateX(0px)',
    ]);
    // The end.
    expect(ANSWER_POP_MS).toBe(460);
    expect([SCORE_RISE_MS, SCORE_RISE_DELAY_MS, SCORE_SHOWN_MS]).toEqual([1900, 650, 3000]);
    expect([CONFETTI_AFTER_MS, CONFETTI_MS, CONFETTI_PIECES]).toEqual([560, 2400, 150]);
    expect(RESULTS_AFTER_MS).toBe(1300);
    expect([0, 1, 4].map(resultsDelay)).toEqual([60, 140, 380]);
    expect(COUNT_UP_MS).toBe(900);
    // The intro.
    expect([INTRO_IN_MS, INTRO_PART_MS, INTRO_OUT_MS]).toEqual([240, 520, 260]);
    expect([0, 1, 7].map(introDelay)).toEqual([80, 150, 570]);
    expect([TWINKLE_EVERY_MS, TWINKLE_MS]).toEqual([750, 760]);
    expect(TWINKLE_EASE).toBe('cubic-bezier(0.4, 0, 0.2, 1)');
  });

  it('turns a card on the same curve in the stylesheet', () => {
    expect(css).toContain(`transition: transform ${FLIP_MS / 1000}s ${FLIP_EASE};`);
  });

  it('bursts from the answer and stays on it while the map glides on', () => {
    const origin = { x: 300, y: 200 };
    // Where it burst, nothing to carry.
    expect(burstShift(origin, { left: 220, top: 160, width: 160, height: 80 })).toEqual({ dx: 0, dy: 0 });
    // The map has carried the card 400px right and 1,100px up since.
    expect(burstShift(origin, { left: 620, top: -940, width: 160, height: 80 })).toEqual({ dx: 400, dy: -1100 });
    // No card, nothing to follow.
    expect(burstShift(origin, null)).toEqual({ dx: 0, dy: 0 });
  });
});

/** The declarations of the rules naming exactly `selector` (alone or in
 *  a list) inside `@media <media>`, or outside any @media for null. */
function declsIn(selector: string, media: string | null): Map<string, string> {
  const text = css.replace(/\/\*[\s\S]*?\*\//g, '');
  const out = new Map<string, string>();
  const open: { prelude: string; start: number }[] = [];
  let mark = 0;
  for (let i = 0; i < text.length; i++) {
    if (text[i] === '{') {
      open.push({ prelude: text.slice(mark, i).trim(), start: i + 1 });
      mark = i + 1;
    } else if (text[i] === '}') {
      const block = open.pop();
      mark = i + 1;
      if (!block || block.prelude.startsWith('@')) continue;
      const at = open.find((b) => b.prelude.startsWith('@media'));
      if ((at ? at.prelude.replace(/^@media\s+/, '') : null) !== media) continue;
      if (!block.prelude.split(',').map((x) => x.trim()).includes(selector)) continue;
      for (const d of text.slice(block.start, i).split(';')) {
        const c = d.indexOf(':');
        if (c > 0) out.set(d.slice(0, c).trim(), d.slice(c + 1).trim().replace(/\s+/g, ' '));
      }
    }
  }
  return out;
}

describe('the year’s marks in the stylesheet', () => {
  const REDUCED = '(prefers-reduced-motion: reduce)';

  it('washes the year’s band and lights its label, a decade keeping its face', () => {
    expect(declsIn('.cd-band-anchor', null).get('background')).toBe('var(--accWash)');
    const label = declsIn('.cd-rail-anchor', null);
    expect([label.get('font-size'), label.get('font-weight'), label.get('color')]).toEqual(['13px', '700', 'var(--accText)']);
    const decade = declsIn('.cd-rail-decade.cd-rail-anchor', null);
    expect([decade.get('font-size'), decade.get('font-weight')]).toEqual(['16px', '400']);
    expect(declsIn('.cd-rail-decade', null).get('font-family')).toBe("'Young Serif', serif");
  });

  it('opens the row on the map’s reflow curve, fading the other years and the new label over .4s', () => {
    expect(declsIn('.cd-daily .cd-band', null).get('transition')).toBe(
      'top 0.5s var(--ease-row), height 0.5s var(--ease-row), background-color 0.3s ease, opacity 0.4s ease',
    );
    expect(declsIn('.cd-daily .cd-rail-slot', null).get('transition')).toBe(
      'top 0.5s var(--ease-row), height 0.5s var(--ease-row), opacity 0.4s ease',
    );
    expect(declsIn('.cd-daily-plot', null).get('transition')).toBe('height 0.5s var(--ease-row)');
  });

  it('rules its edges across the board in the accent, over the bands and under the cards', () => {
    const across = declsIn('.cd-daily-bound-across', null);
    expect(across.get('border-top')).toBe('1.5px dashed var(--acc)');
    expect([across.get('left'), across.get('right')]).toEqual(['0', '0']);
    expect(across.get('z-index')).toBe('5');
    expect(across.get('transition')).toBe('top 0.5s ease');
    const tag = declsIn('.cd-daily-bound-pill', null);
    expect([tag.get('height'), tag.get('padding'), tag.get('border-radius')]).toEqual(['18px', '0 7px', '6px']);
    expect([tag.get('background'), tag.get('color'), tag.get('font-size'), tag.get('font-weight')]).toEqual([
      'var(--acc)',
      'var(--accInk)',
      '10.5px',
      '700',
    ]);
  });

  it('moves none of it for a reader who has asked for stillness', () => {
    for (const sel of [
      '.cd-daily .cd-band',
      '.cd-daily .cd-rail-slot',
      '.cd-daily-plot',
      '.cd-daily-card',
      '.cd-daily-bound-across',
      '.cd-daily-bound-pill-year',
    ]) {
      expect(declsIn(sel, REDUCED).get('transition'), sel).toBe('none');
    }
  });

  it('dims the Year button at .45 once it is seen or cannot be paid for', () => {
    expect(declsIn('.cd-daily-clue:disabled', null).get('opacity')).toBe('0.45');
  });

  it('draws the reader’s place in the streak pill’s box, in the text colour with numbers that line up', () => {
    const box = declsIn('.cd-daily-streak', null);
    expect([box.get('height'), box.get('padding'), box.get('border-radius'), box.get('gap')]).toEqual([
      '24px',
      '0 9px 0 7px',
      '8px',
      '5px',
    ]);
    expect([box.get('background'), box.get('box-shadow'), box.get('font-size'), box.get('font-weight')]).toEqual([
      'var(--c)',
      'inset 0 0 0 1px var(--ln3)',
      '12.5px',
      '700',
    ]);
    const own = declsIn('.cd-daily-standing', null);
    expect([own.get('color'), own.get('font-variant-numeric')]).toEqual(['var(--t)', 'tabular-nums']);
    expect(declsIn('.cd-daily-streak svg', null).get('color')).toBe('var(--acc)');
    expect(css).not.toContain('.cd-daily-quote');
  });
});

describe('the title screen in the stylesheet', () => {
  const PHONE = '(max-width: 639.98px)';
  /** What a declaration list sets, of these properties. */
  const of = (decls: Map<string, string>, names: string[]) => names.map((n) => decls.get(n));

  it('sets the name at 46px, 34 on a phone, and the goal line under it at 17px, 15.5 on a phone', () => {
    const title = declsIn('.cd-daily-intro-title', null);
    expect(of(title, ['font-family', 'font-weight', 'font-size', 'line-height'])).toEqual([
      "'Young Serif', serif",
      '400',
      '46px',
      '1.05',
    ]);
    expect(declsIn('.cd-daily-intro-title', PHONE).get('font-size')).toBe('34px');
    const goal = declsIn('.cd-daily-goal', null);
    expect(of(goal, ['max-width', 'font-size', 'line-height', 'color', 'text-wrap'])).toEqual([
      '520px',
      '17px',
      '1.5',
      'var(--t2)',
      'balance',
    ]);
    expect(declsIn('.cd-daily-goal', PHONE).get('font-size')).toBe('15.5px');
  });

  it('lays the steps four across in at most 880px, 10px apart, or two by two', () => {
    const list = declsIn('.cd-daily-steps', null);
    expect(of(list, ['display', 'grid-template-columns', 'gap', 'width', 'max-width', 'list-style'])).toEqual([
      'grid',
      'repeat(4, minmax(0, 1fr))',
      '10px',
      '100%',
      '880px',
      'none',
    ]);
    expect(declsIn('.cd-daily-steps-pairs', null).get('grid-template-columns')).toBe('repeat(2, minmax(0, 1fr))');
    // The grid's own stretch keeps a row of steps one height: nothing
    // may set it otherwise.
    expect(list.get('align-items')).toBeUndefined();
    expect(declsIn('.cd-daily-step', null).get('align-self')).toBeUndefined();
  });

  it('boxes each step with 14px corners, 12px in, on the surface with its ring, 9px between its parts', () => {
    const step = declsIn('.cd-daily-step', null);
    expect(
      of(step, ['display', 'flex-direction', 'gap', 'min-width', 'padding', 'box-sizing', 'border-radius', 'background', 'box-shadow']),
    ).toEqual(['flex', 'column', '9px', '0', '12px', 'border-box', '14px', 'var(--s)', 'inset 0 0 0 1px var(--ln2)']);
  });

  it('numbers each step in a 20px accent circle and names it at 14px bold', () => {
    const n = declsIn('.cd-daily-step-n', null);
    expect(of(n, ['width', 'height', 'border-radius', 'background', 'color', 'font-size', 'font-weight', 'line-height'])).toEqual([
      '20px',
      '20px',
      '50%',
      'var(--accSoft)',
      'var(--accText)',
      '11px',
      '700',
      '20px',
    ]);
    expect(of(declsIn('.cd-daily-step-head', null), ['display', 'align-items', 'gap'])).toEqual(['flex', 'center', '8px']);
    expect(of(declsIn('.cd-daily-step-name', null), ['font-size', 'font-weight'])).toEqual(['14px', '700']);
  });

  it('draws each picture on the ground with 10px corners, at least 80px tall, and Look at exactly 80', () => {
    const pic = declsIn('.cd-daily-step-pic', null);
    expect(of(pic, ['min-height', 'box-sizing', 'border-radius', 'background', 'padding', 'gap'])).toEqual([
      '80px',
      'border-box',
      '10px',
      'var(--g)',
      '8px',
      '7px',
    ]);
    // A floor, not a height, so a picture whose chips wrap grows.
    expect(pic.get('height')).toBeUndefined();
    const look = declsIn('.cd-daily-step-look', null);
    expect(of(look, ['height', 'padding', 'position'])).toEqual(['80px', '20px 8px 7px', 'relative']);
    expect(of(declsIn('.cd-daily-step-board', null), ['grid-template-columns', 'grid-template-rows', 'gap', 'height'])).toEqual([
      'repeat(5, minmax(0, 1fr))',
      'repeat(3, minmax(0, 1fr))',
      '4px',
      '100%',
    ]);
    expect(of(declsIn('.cd-daily-step-axis', null), ['top', 'font-size', 'font-weight', 'color'])).toEqual([
      '4px',
      '10px',
      '600',
      'var(--t3)',
    ]);
    expect(of(declsIn('.cd-daily-step-score', null), ['gap', 'padding'])).toEqual(['9px', '8px 10px']);
  });

  it('draws the hidden card dashed in the accent, and the face-up ones with a poster in a token', () => {
    const hidden = declsIn('.cd-daily-step-card-hidden', null);
    expect(of(hidden, ['border', 'background', 'font-family', 'font-size', 'color'])).toEqual([
      '1.5px dashed var(--acc)',
      'var(--ancBg)',
      "'Young Serif', serif",
      '10px',
      'var(--accText)',
    ]);
    expect(declsIn('.cd-daily-step-card-up', null).get('background')).toBe(
      'linear-gradient(90deg, var(--miniPoster) 0 34%, var(--c2) 34%)',
    );
    expect(declsIn(':root', null).get('--miniPoster')).toBe('oklch(0.56 0.09 230)');
    expect(declsIn('.cd-daily-step-card-blank', null).get('box-shadow')).toBe('inset 0 0 0 1px var(--ln3)');
    // The person is in their own colour, set inline.
    expect(declsIn('.cd-daily-step-dot', null).get('background')).toBe('var(--tone)');
  });

  it('makes every chip 18px tall, 0 6px in, with 6px corners, at 10.5px bold', () => {
    const chip = declsIn('.cd-daily-step-chip', null);
    expect(of(chip, ['height', 'padding', 'border-radius', 'font-size', 'font-weight', 'line-height'])).toEqual([
      '18px',
      '0 6px',
      '6px',
      '10.5px',
      '700',
      '18px',
    ]);
    expect(of(chip, ['background', 'color'])).toEqual(['var(--accSoft)', 'var(--accText)']);
    expect(of(declsIn('.cd-daily-step-chip-down', null), ['background', 'color'])).toEqual([
      'color-mix(in oklch, var(--down) 18%, transparent)',
      'var(--down)',
    ]);
    expect(of(declsIn('.cd-daily-step-chip-plain', null), ['background', 'box-shadow', 'color'])).toEqual([
      'var(--c)',
      'inset 0 0 0 1px var(--ln3)',
      'var(--t2)',
    ]);
  });

  it('keeps everything inside its picture however narrow the column', () => {
    // At 320px a step is 135px wide and its picture's inside 95: the
    // chips wrap, a chip is cut at the picture's edge rather than run
    // past it, the guessed title gives way to an ellipsis, and "points
    // left" goes under the number.
    expect(declsIn('.cd-daily-step', null).get('min-width')).toBe('0');
    expect(declsIn('.cd-daily-step-chips', null).get('flex-wrap')).toBe('wrap');
    expect(of(declsIn('.cd-daily-step-chip', null), ['max-width', 'overflow', 'white-space'])).toEqual(['100%', 'hidden', 'nowrap']);
    expect(of(declsIn('.cd-daily-step-typed', null), ['flex', 'min-width', 'overflow', 'text-overflow', 'white-space'])).toEqual([
      '1',
      '0',
      'hidden',
      'ellipsis',
      'nowrap',
    ]);
    expect(declsIn('.cd-daily-step-chip-down', null).get('flex-shrink')).toBe('0');
    expect(declsIn('.cd-daily-step-kept', null).get('flex-wrap')).toBe('wrap');
    expect(declsIn('.cd-daily-step-axis', null).get('white-space')).toBe('nowrap');
  });

  it('captions each step at 12.5px on 1.4, in the second ink', () => {
    expect(of(declsIn('.cd-daily-step-caption', null), ['font-size', 'line-height', 'color'])).toEqual([
      '12.5px',
      '1.4',
      'var(--t2)',
    ]);
  });

  it('moves none of the steps itself, so stillness leaves them simply there', () => {
    // They arrive with the screen's entrance, played from script, which
    // plays nothing for a reader who has asked for stillness. A
    // transition here would be one the Daily's reduced-motion rules at
    // the end of its section would have to undo, and none of them does.
    const text = css.replace(/\/\*[\s\S]*?\*\//g, '');
    for (const m of text.matchAll(/([^{}]*\.cd-daily-step[^{}]*)\{([^{}]*)\}/g)) {
      expect(m[2], m[1].trim()).not.toMatch(/\b(transition|animation)\s*:/);
    }
    const still = /@media \(prefers-reduced-motion: reduce\) \{\s*\.cd-daily,[\s\S]*?\n\}/.exec(text)?.[0] ?? '';
    expect(still).toContain('.cd-daily-play-button');
    expect(still).not.toContain('.cd-daily-step');
  });

  it('keeps each step’s edge in forced colours, where its ring is dropped', () => {
    expect(declsIn('.cd-daily-step', '(forced-colors: active)').get('outline')).toBe('1px solid ButtonText');
  });

  it('keeps nothing of the cost tiles, the rules paragraph or the lead', () => {
    for (const gone of ['.cd-daily-costs', '.cd-daily-cost-item', '.cd-daily-cost-label', '.cd-daily-rules', '.cd-daily-lead', '.cd-daily-tag-down']) {
      expect(css).not.toMatch(new RegExp(`${gone.replace('.', '\\.')}\\b(?!-)`));
    }
  });
});

describe('Play again in the stylesheet', () => {
  const HOVER = '(hover: hover)';
  /** What a declaration list sets, of these properties. */
  const of = (decls: Map<string, string>, names: string[]) => names.map((n) => decls.get(n));

  it('is dressed as the prototype’s own at the foot of the result: the quietest thing there, centred', () => {
    // Cinedikt Daily.dc.html, "Play again (prototype only)".
    const own = declsIn('.cd-daily-again', null);
    expect(
      of(own, ['align-self', 'height', 'padding', 'border', 'border-radius', 'background', 'color', 'font-size', 'font-weight']),
    ).toEqual(['center', '32px', '0 10px', '0', '9px', 'none', 'var(--t3)', '12.5px', '600']);
    expect(own.get('transition')).toBe('background-color 0.15s ease, color 0.15s ease');
  });

  it('is dressed as “Show the answer” beside it while the game is on', () => {
    const reveal = declsIn('.cd-daily-reveal', null);
    const beside = new Map([...declsIn('.cd-daily-again', null), ...declsIn('.cd-daily-hintrow .cd-daily-again', null)]);
    for (const name of ['height', 'padding', 'border', 'border-radius', 'background', 'color', 'font-size', 'font-weight', 'white-space']) {
      expect(beside.get(name), name).toBe(reveal.get(name));
    }
  });

  it('gives the hint its own line only where Play again is offered, and puts the two buttons under it at the end', () => {
    expect(declsIn('.cd-daily-hintrow', null).get('flex-wrap')).toBeUndefined();
    const dev = declsIn('.cd-daily-hintrow-dev', null);
    expect(of(dev, ['flex-wrap', 'justify-content'])).toEqual(['wrap', 'flex-end']);
    expect(declsIn('.cd-daily-hintrow-dev .cd-daily-hint', null).get('flex-basis')).toBe('100%');
  });

  it('lights up under the pointer as “Show the answer” does, and takes the focus ring', () => {
    expect(of(declsIn('.cd-daily-again:hover', HOVER), ['background', 'color'])).toEqual(['var(--ln2)', 'var(--t)']);
    expect(declsIn('.cd-daily-again:focus-visible', null).get('outline')).toBe('2px solid var(--acc)');
  });
});

describe('the guess list in forced colours', () => {
  it('keeps the list’s edge, which is otherwise only a shadow', () => {
    expect(declsIn('.cd-daily-options', '(forced-colors: active)').get('outline')).toBe('1px solid ButtonText');
  });

  it('rings the row the arrows are on, which is otherwise only a wash', () => {
    // Enter takes that row: a reader in High Contrast has to see which.
    expect(declsIn('.cd-daily-option-at', '(forced-colors: active)').get('outline')).toBe('2px solid Highlight');
  });
});

describe('the Daily’s words', () => {
  it('say "movie", never "film"', () => {
    // Every sentence the logic writes for a full game, one of each kind.
    const today = todayOf();
    const g = ended(
      gameOf([
        { type: 'flip', card: 'c4', cost: 55, relative: { shared: 4 } },
        { type: 'person', role: 'director', cost: 150, people: [LANA, LILLY] },
        { type: 'genres', cost: 80, genres: ['Action'] },
        yearEntry(1999),
        guess(BOUND, 'newer', 'higher', { shared: [LANA] }),
        guess(film('tt9', 'Casablanca', 1942, 8.5), 'newer', 'higher'),
        { type: 'win' },
      ]),
      true,
    );
    const words = [
      ...feedOf(today, gameOf([{ type: 'flip', card: 'c4', cost: 55, relative: { shared: 4 } }])).flatMap((e) => [e.text, e.more, ...e.chips.map((c) => c.aria)]),
      ...feedOf(today, g).flatMap((e) => [e.label, e.text, e.more, ...e.chips.map((c) => c.aria)]),
      resultTitle(g),
      resultSub(g),
      ...statsOf(g, { beat: 1, solved: 1 }, today.streak).map((s) => s.caption),
      shareText(142, g, 'https://cinedikt.com'),
      cardLabel(today.cards[0], undefined),
      cardLabel(today.cards[0], { kind: 'relative', shared: 3 }),
      answerLabel(MATRIX),
      ...['points', 'known', 'unknown', 'day', 'done', 'cookie', 'busy', 'not-ready', null].map((r) => refusalText(r, 'guess')),
      ...clueButtons(today, gameOf([])).flatMap((b) => [b.label, b.aria]),
      standingText({ rank: 1204, players: 83500 }),
      boardNote('today', 2, 'x', false),
      boardNote('week', 2, 'x', true),
      hintText(true, 100),
      midnightText(gameOf([])),
      PLAY_AGAIN,
      AGAIN_FAILED,
    ];
    for (const w of words) expect(w).not.toMatch(/\bfilms?\b/i);
  });
});
