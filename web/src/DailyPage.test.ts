import { createElement, isValidElement, type ComponentProps, type ReactElement, type ReactNode } from 'react';
import { renderToStaticMarkup } from 'react-dom/server';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import {
  ApiError,
  type DailyAlso,
  type DailyBoard,
  type DailyEntry,
  type DailyGame,
  type DailyGuess,
  type DailyPerson,
  type DailySlot,
  type DailyToday,
  type SearchHit,
} from './api';
import { DailyBar, HitList, MessageBox } from './DailyBar';
import { DailyCast } from './DailyCast';
import { DailyFace } from './DailyFace';
import { DailyGameView, DailyHeaderTail, DailyPage, playAgain } from './DailyPage';
import { DailyLeaderboard, DailyResult } from './DailyResult';
import { DailyRules, keepTabIn, nextStop } from './DailyRules';
import { DailySheetAsk } from './DailySheetAsk';
import {
  HIT_INSETS,
  RISE_STEP_MS,
  TITLE_LOOPS,
  castRows,
  codesOf,
  guessMessage,
  openingRows,
  revealDelays,
  todaysPeople,
  warmthNote,
} from './daily';
import { HINTS_KEY, hintSeen } from './dailyHints';
import css from './grid.css?raw';

// ---- today's movie: The Matrix, No. 143 ----

const person = (id: string, name: string, hue: number, photo?: string): DailyPerson =>
  photo ? { id, name, hue, photo } : { id, name, hue };

// The cast in reveal order, sixth-billed first, each in the hue of their
// place on the movie, the star first, then the directors.
const JOE = person('nm0001592', 'Joe Pantoliano', 205, 'https://image.tmdb.org/t/p/w185/joe.jpg');
const GLORIA = person('nm0287825', 'Gloria Foster', 78);
const HUGO = person('nm0915989', 'Hugo Weaving', 150);
const CARRIE = person('nm0005251', 'Carrie-Anne Moss', 345);
const LAURENCE = person('nm0000401', 'Laurence Fishburne', 28);
const KEANU = person('nm0000206', 'Keanu Reeves', 232);
const LANA = person('nm0905154', 'Lana Wachowski', 118);
const LILLY = person('nm0905152', 'Lilly Wachowski', 255);
const CAST = [JOE, GLORIA, HUGO, CARRIE, LAURENCE, KEANU];

const ALSO: DailyAlso[] = [
  { id: 'tt0106977', title: 'The Fugitive', year: 1993 },
  { id: 'tt0068907', title: 'Man and Boy', year: 1971 },
  { id: 'tt0434409', title: 'V for Vendetta', year: 2005 },
  { id: 'tt0241303', title: 'Chocolat', year: 2000 },
  { id: 'tt0078788', title: 'Apocalypse Now', year: 1979 },
  { id: 'tt2911666', title: 'John Wick', year: 2014 },
];

const MATRIX = {
  id: 'tt0133093',
  title: 'The Matrix',
  year: 1999,
  rating: 8.7,
  length: 136,
  genres: ['Action', 'Sci-Fi'],
  colour: '#26382d',
  poster: 'https://m.media-amazon.com/images/M/matrix.jpg',
};

/** The six slots, each shown as `shown` says, or hidden. */
function slotsOf(shown: Record<number, 'start' | 'next' | 'guess'>): DailySlot[] {
  return CAST.map((p, slot): DailySlot => {
    const via = shown[slot];
    return via ? { slot, shown: true, person: p, also: ALSO[slot], via } : { slot, shown: false };
  });
}

const ALL_SHOWN = slotsOf({ 0: 'start', 1: 'next', 2: 'next', 3: 'guess', 4: 'next', 5: 'next' });

function wrong(
  id: string,
  title: string,
  year: number,
  over: Partial<Pick<DailyGuess, 'shared' | 'sameDecade' | 'sharesGenre' | 'cost' | 'warmth'>> = {},
): Extract<DailyEntry, { type: 'guess' }> {
  const guess: DailyGuess = {
    id,
    title,
    year,
    cost: over.cost ?? 100,
    shared: over.shared ?? [],
    sameDecade: over.sameDecade ?? false,
    sharesGenre: over.sharesGenre ?? false,
    warmth: over.warmth ?? 0,
  };
  return { type: 'guess', cost: guess.cost, guess };
}

const THIRTEENTH = wrong('tt0139809', 'The Thirteenth Floor', 1999, { sameDecade: true, sharesGenre: true, warmth: 1 });
// Keanu Reeves is in it, and it is an action movie: hot.
const SPEED = wrong('tt0111257', 'Speed', 1994, { shared: [5], sharesGenre: true, cost: 150, warmth: 2 });

/** A game just started: the sixth-billed showing, nothing bought. */
function gameOf(over: Partial<DailyGame> = {}): DailyGame {
  return {
    phase: 'play',
    pts: 1000,
    seq: 0,
    startedAt: '2026-10-09T11:58:00Z',
    finishedAt: null,
    won: false,
    gaveUp: false,
    nextCost: 100,
    slots: slotsOf({ 0: 'start' }),
    facts: {},
    log: [],
    sheet: null,
    end: null,
    ...over,
  };
}

/** The screenshots' game in progress: two extra names, the decade, and one
 *  wrong guess, which showed a fourth. */
const PLAYING = gameOf({
  pts: 600,
  seq: 4,
  nextCost: 150,
  slots: slotsOf({ 0: 'start', 1: 'next', 2: 'next', 3: 'guess' }),
  facts: { decade: 1990 },
  log: [
    { type: 'next', cost: 100, slot: 1 },
    { type: 'next', cost: 100, slot: 2 },
    { type: 'fact', kind: 'decade', cost: 100 },
    THIRTEENTH,
  ],
});

function ended(log: DailyEntry[], over: Partial<DailyGame> = {}): DailyGame {
  return gameOf({
    phase: 'done',
    finishedAt: '2026-10-09T12:04:00Z',
    slots: ALL_SHOWN,
    log,
    end: { answer: MATRIX, directors: [LANA, LILLY] },
    ...over,
  });
}

/** The game in progress with its one Movies map chosen: Joe Pantoliano's. */
const CHOSEN = gameOf({
  ...PLAYING,
  seq: PLAYING.seq + 1,
  log: [...PLAYING.log, { type: 'sheet', person: JOE.id }],
  sheet: JOE.id,
});

/** The screenshots' solve, at 600 points. */
const SOLVED = ended([...PLAYING.log, { type: 'win' }], { pts: 600, won: true, facts: { decade: 1990 } });
const OUT = ended([THIRTEENTH, { type: 'out' }], { pts: 0 });
const GAVE_UP = ended([{ type: 'gaveup' }], { pts: 0, gaveUp: true });

/** Today's puzzle, with this game in it, from a server in development
 *  when `dev` is set. */
function todayOf(game: DailyGame | null, dev = false, played = 61240): DailyToday {
  return {
    no: 143,
    date: '2026-10-09',
    now: '2026-10-09T12:00:00Z',
    next: '2026-10-10T00:00:00Z',
    colour: '#26382d',
    player: { name: 'Trinity Kimble', saved: game != null },
    played,
    streak: { now: 0, before: 1 },
    game,
    ...(dev ? { dev: true } : {}),
  };
}

function viewProps(game: DailyGame | null, dev = false, again: () => void = () => {}): ComponentProps<typeof DailyGameView> {
  return {
    today: todayOf(game, dev),
    offset: 0,
    say: () => {},
    reload: () => {},
    again,
    rulesSignal: 0,
    onOpenMovie: () => {},
  };
}

/** The game for this puzzle as its first render draws it. Effects never
 *  run on the server renderer, so nothing is fetched, nothing is scrolled
 *  and nothing moves: what is left is what each state draws. */
function drawn(game: DailyGame | null, dev = false): string {
  return renderToStaticMarkup(createElement(DailyGameView, viewProps(game, dev)));
}

/** What a reader can read: the text, and the words the page says out loud
 *  or as a placeholder. Class names are code. */
function words(markup: string): string {
  const said = [...markup.matchAll(/(?:aria-label|placeholder|title)="([^"]*)"/g)].map((m) => m[1]);
  return `${markup.replace(/<[^>]*>/g, ' ')} ${said.join(' ')}`;
}

/** Where each part starts in the markup, for their order. */
const at = (html: string, cls: string) => html.indexOf(`class="${cls}`);

/** The handlers on every element of `el` with class `cls`, every component
 *  in it drawn out, as one server render would draw it. Called inside a
 *  render of its own, so the hooks they use have one to belong to, and no
 *  DOM is needed to press what they draw. */
function pressesIn(el: ReactElement, cls: string): (() => void)[] {
  const found: (() => void)[] = [];
  const walk = (node: ReactNode): void => {
    if (Array.isArray(node)) return node.forEach(walk);
    if (!isValidElement(node)) return;
    const { type, props } = node as ReactElement<{ className?: string; onClick?: () => void; children?: ReactNode }>;
    if (typeof type === 'function') return walk((type as (p: unknown) => ReactNode)(props));
    if (typeof type === 'string' && props.className?.split(' ').includes(cls) && props.onClick) found.push(props.onClick);
    walk(props.children);
  };
  function Probe() {
    walk(el);
    return null;
  }
  renderToStaticMarkup(createElement(Probe));
  return found;
}

beforeEach(() => {
  vi.stubGlobal('window', { innerWidth: 1366, innerHeight: 768 });
});

afterEach(() => {
  vi.unstubAllGlobals();
});

describe('the title screen', () => {
  it('draws the game in one picture, then the pill and day, the name, the lead, three steps, the fine print and Play', () => {
    const html = drawn(null);
    // The card in today's poster colour, glowing in it lightened, beside
    // three rows: one shown, then the next and the hidden one, each with a
    // filled row waiting to drop into it. For the eye only.
    expect(html).toContain('<div class="cd-nd-title-pic" aria-hidden="true" data-rise="">');
    expect(html).toContain(
      '<span class="cd-nd-title-card" style="--tone:#26382d;--glow:rgba(136, 146, 140, 0.3)" data-anim="float"><span class="cd-nd-title-q" data-anim="bob">?</span><span class="cd-nd-title-sheen" data-anim="sheen"></span></span>',
    );
    expect(html).toContain(
      '<span class="cd-nd-title-row cd-nd-title-row-next" data-anim="pulse"><span class="cd-nd-title-drop" data-anim="drop1"><span class="cd-nd-title-row cd-nd-title-row-shown"><span class="cd-nd-title-face" style="--tone:oklch(0.76 0.13 205)"></span><span class="cd-nd-title-bar"></span></span></span></span>',
    );
    expect(html).toContain(
      '<span class="cd-nd-title-row cd-nd-title-row-hidden"><span class="cd-nd-title-drop" data-anim="drop2"><span class="cd-nd-title-row cd-nd-title-row-shown"><span class="cd-nd-title-face" style="--tone:oklch(0.76 0.13 345)"></span>',
    );
    // The row always shown, its face tinted in hue 118, in the theme drawn.
    expect(html).toContain(
      '<span class="cd-nd-title-rows"><span class="cd-nd-title-row cd-nd-title-row-shown"><span class="cd-nd-title-face" style="--tone:oklch(0.76 0.13 118)"></span>',
    );
    expect(html).toContain('<span class="cd-daily-pill">Cinedikt Daily</span><span class="cd-nd-title-day">Friday 9 October</span>');
    expect(html).toContain('<h1 class="cd-nd-title-heading">Name Drop</h1>');
    expect(html).toContain(
      'Today’s movie is hidden. Its cast shows up one name at a time, working up to the star. Name the movie in as few names as you can.',
    );
    expect(html).toContain('<ol class="cd-nd-steps" aria-label="How to play" data-rise="">');
    for (const [i, step] of ['See who’s in it', 'Guess, or show the next name', 'Fewer names, more points'].entries()) {
      expect(html).toContain(`<span class="cd-nd-badge" aria-hidden="true">${i + 1}</span>${step}</li>`);
    }
    expect(html).toContain(
      'You start with 1,000 points. Extra names, facts and wrong guesses cost points. There’s no clock.',
    );
    expect(html).toMatch(
      /<button type="button" class="cd-nd-play" data-rise=""><span class="cd-nd-play-shine" aria-hidden="true" data-anim="shine"><\/span>Play<svg [^>]*aria-hidden="true">/,
    );
    expect(html).toContain('<span class="cd-nd-title-count">61,240</span> people have played today.');
    // The order the design gives them.
    const order = ['cd-nd-title-pic', 'cd-nd-title-meta', 'cd-nd-title-words', 'cd-nd-steps', 'cd-nd-title-fine', 'cd-nd-play', 'cd-nd-title-played'];
    const found = order.map((c) => at(html, c));
    expect(found.every((n) => n >= 0)).toBe(true);
    expect([...found].sort((a, b) => a - b)).toEqual(found);
  });

  it('rises its seven parts in the order they are drawn, the picture first and the players line last', () => {
    const html = drawn(null);
    const rising = [...html.matchAll(/class="([\w-]+)[^"]*"[^>]*data-rise=""/g)].map((m) => m[1]);
    expect(rising).toEqual([
      'cd-nd-title-pic',
      'cd-nd-title-meta',
      'cd-nd-title-words',
      'cd-nd-steps',
      'cd-nd-title-fine',
      'cd-nd-play',
      'cd-nd-title-played',
    ]);
    expect(RISE_STEP_MS).toBe(90);
  });

  it('marks a part for every loop it plays, and for nothing else', () => {
    const html = drawn(null);
    const marked = [...html.matchAll(/data-anim="(\w+)"/g)].map((m) => m[1]);
    expect(marked.sort()).toEqual(Object.keys(TITLE_LOOPS).sort());
  });

  it('starts nothing moving on the server, and draws every part at rest', () => {
    // Effects never run in a static render, as they never run for a
    // reader who has asked for stillness: what is drawn is the screen at
    // rest, the dropping rows and the shines left to the stylesheet.
    const html = drawn(null);
    expect(html).not.toMatch(/style="[^"]*(opacity|translate|rotate)/);
  });

  it('leaves the card without a glow on a colour the server could not have sent', () => {
    const html = renderToStaticMarkup(
      createElement(DailyGameView, { ...viewProps(null), today: { ...todayOf(null), colour: 'url(x)' } }),
    );
    expect(html).toContain('<span class="cd-nd-title-card" style="--tone:var(--c)" data-anim="float">');
  });

  it('leaves the players line out before anyone has played', () => {
    const html = renderToStaticMarkup(createElement(DailyGameView, { ...viewProps(null), today: todayOf(null, false, 0) }));
    expect(html).not.toContain('cd-nd-title-played');
    expect(words(html)).not.toContain('have played');
  });

  it('opens on the game page, not the title screen, for a game already begun', () => {
    for (const g of [gameOf(), SOLVED]) {
      const html = drawn(g);
      expect(html).not.toContain('cd-nd-title');
      expect(html).toContain('<div class="cd-nd">');
    }
  });
});

describe('the game page while the game is on', () => {
  it('heads it with the game’s name, the task and the line, beside the hidden card in today’s poster colour', () => {
    const html = drawn(gameOf());
    expect(html).toContain(
      '<div class="cd-nd-card" aria-hidden="true"><div class="cd-nd-card-in"><div class="cd-nd-card-front" style="--tone:#26382d"><span class="cd-nd-card-q">?</span></div></div></div>',
    );
    expect(html).toContain('<span class="cd-nd-kicker">Name Drop</span>');
    expect(html).toContain('<h1 class="cd-nd-heading">Name today’s movie</h1>');
    expect(html).toContain('<p class="cd-nd-line">Its cast shows up one name at a time, working up to the star.</p>');
    // The poster is not drawn before the end: the page is not told it.
    expect(html).not.toContain('cd-nd-card-back');
    expect(html).not.toContain('cd-nd-card-over');
  });

  it('puts the top block first, then the facts right under the card, then the cast, with the guess bar under them', () => {
    const html = drawn(PLAYING);
    const order = ['cd-nd-top', 'cd-nd-facts', 'cd-nd-cast', 'cd-nd-bar'].map((c) => at(html, c));
    expect(order.every((n) => n >= 0)).toBe(true);
    expect([...order].sort((a, b) => a - b)).toEqual(order);
    // Nothing of the end.
    for (const c of ['cd-nd-res', 'cd-nd-board', 'cd-nd-cast-head']) expect(html).not.toContain(`class="${c}`);
  });

  it('lists the cast as “Today’s names”: shown rows, the next row as a button, and hidden rows with no name at all', () => {
    const html = drawn(PLAYING);
    expect(html).toContain('<ol class="cd-nd-cast" aria-label="Today’s names">');
    expect(html.match(/<li class="cd-nd-row cd-nd-row-shown">/g)).toHaveLength(4);
    expect(html.match(/<li class="cd-nd-row cd-nd-row-next">/g)).toHaveLength(1);
    expect(html.match(/<li class="cd-nd-row cd-nd-row-hidden">/g)).toHaveLength(1);
    expect(html).toContain('<span class="cd-nd-name">Joe Pantoliano</span><span class="cd-nd-also">Also in The Fugitive (1993)</span>');
    expect(html).toContain(
      '<button type="button" class="cd-nd-row-go" aria-label="Show the next name. It costs 100 points."></button>',
    );
    expect(html).toContain('<span class="cd-nd-next-word">Next</span>');
    // Four names and no more: the hidden two are not on the page, and the
    // hidden row says only that it is not out yet.
    expect(html.match(/class="cd-nd-name"/g)).toHaveLength(4);
    expect(html.match(/<span class="cd-sr-live">Not shown yet<\/span>/g)).toHaveLength(1);
    for (const p of [LAURENCE, KEANU]) expect(html).not.toContain(p.name);
  });

  it('offers each shown name’s Movies, named for a screen reader', () => {
    const html = drawn(PLAYING);
    for (const p of [JOE, GLORIA, HUGO, CARRIE]) {
      expect(html).toContain(`<button type="button" class="cd-nd-movies" aria-label="See ${p.name}’s movies on a Cinedikt map">`);
    }
    expect(html.match(/class="cd-nd-movies"/g)).toHaveLength(4);
  });

  it('keeps every name’s Movies in place once a map is chosen, the chosen one open and the rest shut, saying whose is open', () => {
    const html = drawn(CHOSEN);
    expect(html.match(/class="cd-nd-movies"/g)).toHaveLength(4);
    expect(html).toContain(
      '<button type="button" class="cd-nd-movies" aria-label="See Joe Pantoliano’s movies on a Cinedikt map">',
    );
    const shut =
      '<button type="button" class="cd-nd-movies" aria-label="One Movies map a game. You opened Joe Pantoliano’s." title="One Movies map a game. You opened Joe Pantoliano’s." disabled="">';
    expect(html.split(shut)).toHaveLength(4);
    // Nothing more about anyone else: no other name's map is on the page.
    expect(html).not.toContain('cd-msheet');
  });

  it('draws neither the Movies sheet nor the question as it opens', () => {
    for (const game of [PLAYING, CHOSEN]) {
      const html = drawn(game);
      expect(html).not.toContain('cd-msheet');
      expect(html).not.toContain('cd-nd-choose');
    }
  });

  it('draws each face in the person’s colour, with their photo or their initials', () => {
    const html = drawn(PLAYING);
    expect(html).toContain(
      '<span class="cd-nd-face cd-nd-face-row" style="--tone:oklch(0.76 0.13 205)" aria-hidden="true"><span class="cd-nd-face-code">JP</span><img class="cd-nd-face-photo" src="https://image.tmdb.org/t/p/w185/joe.jpg" alt="" decoding="async"/></span>',
    );
    expect(html).toContain(
      '<span class="cd-nd-face cd-nd-face-row" style="--tone:oklch(0.76 0.13 78)" aria-hidden="true"><span class="cd-nd-face-code">GF</span></span>',
    );
  });

  it('draws faces in the stronger colours on paper', () => {
    const face = renderToStaticMarkup(createElement(DailyFace, { person: GLORIA, code: 'GF', size: 'row', theme: 'light' }));
    expect(face).toContain('style="--tone:oklch(0.56 0.16 78)"');
    const dim = renderToStaticMarkup(createElement(DailyFace, { person: GLORIA, code: 'GF', size: 'result', theme: 'dark', dim: true }));
    expect(dim).toContain('class="cd-nd-face cd-nd-face-result cd-nd-face-dim"');
  });

  it('puts a bigger photo by each shown face, opening down for the first three rows and up for the rest', () => {
    const html = drawn(PLAYING);
    expect(html).toContain(
      '<span class="cd-nd-peek" style="--tone:oklch(0.76 0.13 205)" aria-hidden="true"><span class="cd-nd-peek-photo"><span class="cd-nd-peek-code">JP</span><img class="cd-nd-peek-img"',
    );
    expect(html).toContain('<span class="cd-nd-peek-name">Gloria Foster</span><span class="cd-nd-peek-line">Also in Man and Boy (1971)</span>');
    // Carrie-Anne Moss is the fourth row.
    expect(html).toContain('<span class="cd-nd-peek cd-nd-peek-up" style="--tone:oklch(0.76 0.13 345)"');
    expect(html.match(/class="cd-nd-peek[ "]/g)).toHaveLength(4);
    // Only shown faces answer the pointer.
    expect(html.match(/data-peek="1"/g)).toHaveLength(4);
  });

  it('heads the facts panel “Buy a fact”, with a note beside it, over one row of facts', () => {
    const html = drawn(PLAYING);
    expect(html).toContain(
      '<section class="cd-nd-facts" aria-labelledby="cd-nd-facts-head"><div class="cd-nd-facts-top"><h2 id="cd-nd-facts-head" class="cd-nd-facts-head">Buy a fact</h2><p class="cd-nd-facts-note" aria-live="polite">Decade and rating mark the map</p></div><div class="cd-nd-fact-list">',
    );
  });

  it('asks a reader with three names showing and nothing bought, ringing the panel in the accent', () => {
    const stuck = gameOf({ pts: 800, slots: slotsOf({ 0: 'start', 1: 'next', 2: 'next' }) });
    const html = drawn(stuck);
    expect(html).toContain(
      '<section class="cd-nd-facts cd-nd-facts-nudge" aria-labelledby="cd-nd-facts-head"><div class="cd-nd-facts-top"><h2 id="cd-nd-facts-head" class="cd-nd-facts-head">Buy a fact</h2><p class="cd-nd-facts-note" aria-live="polite">Stuck? A fact narrows it down.</p>',
    );
    // Two names is not stuck, and a fact bought is not either.
    expect(drawn(gameOf({ slots: slotsOf({ 0: 'start', 1: 'next' }) }))).not.toContain('cd-nd-facts-nudge');
    expect(drawn(PLAYING)).not.toContain('cd-nd-facts-nudge');
  });

  it('offers the facts in order, each with its price, what it shows and costs for a screen reader, and the decade bought', () => {
    const html = drawn(PLAYING);
    expect(html).toContain(
      '<button type="button" class="cd-nd-fact" aria-label="Show its genre. It costs 100 points.">Genre<span class="cd-nd-cost">−100</span></button>',
    );
    // The decade bought keeps its place, and the director follows it:
    // there is nothing to narrow the years further.
    expect(html).toContain(
      '<span class="cd-nd-fact-got"><span class="cd-nd-fact-label">Decade</span><span class="cd-nd-fact-value">1990s</span></span><button type="button" class="cd-nd-fact" aria-label="Show the director. It costs 250 points.">Director<span class="cd-nd-cost">−250</span></button>',
    );
    const labels = [...html.matchAll(/class="cd-nd-fact"[^>]*>([^<]+)</g)].map((m) => m[1]);
    expect(labels).toEqual(['Length range', 'Rating range', 'Genre', 'Director']);
    expect(html).not.toContain('Narrow the years');
  });

  it('holds back a fact the points would not leave one over from', () => {
    const html = drawn(gameOf({ pts: 100 }));
    expect(html).toContain('<button type="button" class="cd-nd-fact" aria-label="Show roughly how long it is. It costs 50 points.">');
    expect(html).toContain('<button type="button" class="cd-nd-fact" disabled="" aria-label="Show its genre. It costs 100 points.">');
    expect(html).toContain('<button type="button" class="cd-nd-fact" disabled="" aria-label="Show the director. It costs 250 points.">');
    // And no next row, since a name would leave nothing.
    expect(html).not.toContain('cd-nd-row-next');
  });

  it('pins the guess bar as one row, the field and Guess, with Guess held until something matches', () => {
    const html = drawn(gameOf());
    expect(html).toMatch(
      /<div class="cd-nd-msgwrap" aria-live="polite"><\/div><div class="cd-nd-ask"><input class="cd-nd-input" type="text" role="combobox" aria-autocomplete="list" aria-expanded="false" aria-controls="[^"]+" aria-label="Name the movie" placeholder="Name the movie · a miss costs 100"/,
    );
    expect(html).toContain('<button type="button" class="cd-nd-guess" disabled="">Guess · 1,000</button></div></div></div>');
    // No points line and no Next name: the next name is the cast's next
    // row's to give.
    for (const gone of ['cd-nd-points', 'cd-nd-nextbtn', 'cd-nd-bar-row', 'Next name', 'Get it now']) {
      expect(html).not.toContain(gone);
    }
    expect(html).toContain('<button type="button" class="cd-nd-row-go" aria-label="Show the next name. It costs 100 points."></button>');
  });

  it('says in the field that the next wrong guess is the last when the points cannot cover it', () => {
    const html = drawn(gameOf({ pts: 100, nextCost: 150 }));
    expect(html).toContain('placeholder="Name the movie · last guess"');
    expect(html).toContain('>Guess · 100</button>');
  });

  it('offers no next row once all six are out', () => {
    const html = drawn(gameOf({ pts: 500, slots: ALL_SHOWN }));
    expect(html).not.toContain('cd-nd-row-go');
    expect(html.match(/cd-nd-row-shown/g)).toHaveLength(6);
  });

  it('offers Show the answer under the wrong guesses', () => {
    const html = drawn(PLAYING);
    expect(html).toContain('<button type="button" class="cd-nd-giveup">Show the answer</button>');
  });

  it('draws the guess bar beside the cast on a landscape phone', () => {
    vi.stubGlobal('window', { innerWidth: 844, innerHeight: 390 });
    expect(drawn(PLAYING)).toContain('<div class="cd-nd cd-nd-land">');
    vi.stubGlobal('window', { innerWidth: 390, innerHeight: 844 });
    expect(drawn(PLAYING)).toContain('<div class="cd-nd">');
  });
});

describe('after a wrong guess', () => {
  it('keeps a chip for it in the top block, in its warmth, that says what pressing it does', () => {
    const html = drawn(PLAYING);
    expect(html).toContain('<div class="cd-nd-tried" role="group" aria-label="Wrong guesses">');
    expect(html).toContain(
      '<button type="button" class="cd-nd-tried-chip" style="--tone:oklch(0.82 0.13 78)" aria-label="The Thirteenth Floor, warm. Show what it told you"><span class="cd-nd-tried-dot" aria-hidden="true"></span><span class="cd-nd-tried-title">The Thirteenth Floor</span></button>',
    );
  });

  it('says what the next one costs, dearer than the last', () => {
    expect(drawn(PLAYING)).toContain('placeholder="Name the movie · a miss costs 150"');
    expect(drawn(PLAYING)).toContain('>Guess · 600</button>');
  });

  it('tells what it learned in one row: the title, the warmth, the faces it shares and the three chips', () => {
    const game = gameOf({ pts: 850, nextCost: 150, slots: slotsOf({ 0: 'start', 1: 'next', 5: 'guess' }), log: [SPEED] });
    const codes = codesOf(todaysPeople(game));
    const html = renderToStaticMarkup(
      createElement(MessageBox, { message: guessMessage(SPEED.guess, game), codes, theme: 'dark' }),
    );
    expect(html).toBe(
      '<div class="cd-nd-msg"><span class="cd-nd-msg-title">Not Speed</span>' +
        '<span class="cd-nd-warmth" style="--tone:oklch(0.74 0.17 32)"><span class="cd-nd-warmth-dot" aria-hidden="true"></span>Hot</span>' +
        '<span class="cd-nd-msg-faces"><span class="cd-nd-face cd-nd-face-message" style="--tone:oklch(0.76 0.13 232)" aria-hidden="true"><span class="cd-nd-face-code">KR</span></span></span>' +
        '<span class="cd-nd-msg-chip">Different decade</span><span class="cd-nd-msg-chip">Shares a genre</span><span class="cd-nd-msg-chip">Shares Keanu Reeves</span></div>',
    );
  });

  it('shows the message in the bar’s live region, and none for a game opened later', () => {
    const codes = codesOf(todaysPeople(PLAYING));
    const bar = renderToStaticMarkup(
      createElement(DailyBar, {
        game: PLAYING,
        message: guessMessage(THIRTEENTH.guess, PLAYING),
        codes,
        theme: 'dark',
        phone: false,
        opening: false,
        guessed: new Set([THIRTEENTH.guess.id]),
        inputRef: { current: null },
        onGuess: async () => true,
        say: () => {},
      }),
    );
    expect(bar).toContain(
      '<div class="cd-nd-msgwrap" aria-live="polite"><div class="cd-nd-msg"><span class="cd-nd-msg-title">Not The Thirteenth Floor</span><span class="cd-nd-warmth" style="--tone:oklch(0.82 0.13 78)">',
    );
    expect(bar).toContain('<span class="cd-nd-msg-chip">No one from today’s cast</span>');
    // Reopened, the page starts with no message up, as the design does.
    expect(drawn(PLAYING)).toContain('<div class="cd-nd-msgwrap" aria-live="polite"></div>');
  });

  const barWith = (note: string | null, onNoteClose = () => {}) =>
    createElement(DailyBar, {
      game: PLAYING,
      message: guessMessage(THIRTEENTH.guess, PLAYING),
      note,
      onNoteClose,
      codes: codesOf(todaysPeople(PLAYING)),
      theme: 'dark',
      phone: false,
      opening: false,
      guessed: new Set([THIRTEENTH.guess.id]),
      inputRef: { current: null },
      onGuess: async () => true,
      say: () => {},
    });

  it('puts the first-time warmth note over the message, said as a status, with Dismiss', () => {
    const html = renderToStaticMarkup(barWith(warmthNote(150)));
    expect(html).toContain(
      '<div class="cd-nd-tipwrap" role="status"><div class="cd-nd-tip"><span class="cd-nd-tip-text">Cold, warm or hot says how close that guess was. The next miss costs 150.</span><button type="button" class="cd-nd-tip-x" aria-label="Dismiss"><svg',
    );
    expect(html.indexOf('cd-nd-tipwrap')).toBeLessThan(html.indexOf('cd-nd-msgwrap'));
    const closed = vi.fn();
    const presses = pressesIn(barWith(warmthNote(150), closed), 'cd-nd-tip-x');
    expect(presses).toHaveLength(1);
    presses[0]();
    expect(closed).toHaveBeenCalledTimes(1);
  });

  it('keeps the note’s status there and empty with no note, as a reopened page has', () => {
    expect(renderToStaticMarkup(barWith(null))).toContain('<div class="cd-nd-tipwrap" role="status"></div>');
    expect(drawn(PLAYING)).toContain('<div class="cd-nd-tipwrap" role="status"></div>');
  });
});

describe('the results list', () => {
  const hits: SearchHit[] = [
    { id: 'tt0133093', title: 'The Matrix', year: 1999 },
    { id: 'tt10838180', title: 'The Matrix', year: 2021 },
    { id: 'tt0234215', title: 'The Matrix Reloaded', year: 2003 },
    { id: 'tt0139809', title: 'The Thirteenth Floor', year: 1999 },
  ];
  const list = (over: Partial<ComponentProps<typeof HitList>> = {}) =>
    renderToStaticMarkup(
      createElement(HitList, {
        id: 'hits',
        rows: hits,
        held: [],
        note: '',
        at: 0,
        guessed: new Set(['tt0139809']),
        theme: 'dark',
        onPick: () => {},
        ...over,
      }),
    );

  it('is a listbox of movies, the highlighted one selected, each a poster and a title', () => {
    const html = list({ at: 2 });
    expect(html).toMatch(/^<div id="hits" class="cd-nd-hits" role="listbox" aria-label="Movies">/);
    expect(html).toContain('<div id="hits-2" role="option" aria-selected="true" class="cd-nd-hit cd-nd-hit-at">');
    expect(html).toContain('<span class="cd-nd-hit-poster" style="--poster-fill:linear-gradient(165deg, oklch(0.45 0.07');
  });

  it('shows a year only where two results share a title', () => {
    const html = list();
    expect(html.match(/class="cd-nd-hit-year"/g)).toHaveLength(2);
    expect(html).toContain('<span class="cd-nd-hit-title">The Matrix</span><span class="cd-nd-hit-year">1999</span>');
    expect(html).toContain('<span class="cd-nd-hit-title">The Matrix</span><span class="cd-nd-hit-year">2021</span>');
    expect(html).toContain('<span class="cd-nd-hit-title">The Matrix Reloaded</span></span>');
  });

  it('tags a movie already tried', () => {
    const html = list();
    expect(html).toContain('class="cd-nd-hit cd-nd-hit-tried"');
    expect(html).toContain('<span class="cd-nd-hit-tag">Tried</span>');
    expect(html.match(/>Tried</g)).toHaveLength(1);
  });

  it('says nothing matched, and holds the last rows faded and out of reach while the next answer comes', () => {
    expect(list({ rows: [], note: 'No movies match “zzz”' })).toContain(
      '<div class="cd-nd-hits-note">No movies match “zzz”</div>',
    );
    const held = list({ rows: [], held: hits.slice(0, 1) });
    expect(held).toContain('<div role="option" aria-selected="false" aria-disabled="true" class="cd-nd-hit cd-nd-hit-held">');
    expect(held).not.toContain('id="hits-0"');
  });

  it('guesses the movie pressed', () => {
    const picked: string[] = [];
    const presses = pressesIn(
      createElement(HitList, {
        id: 'hits',
        rows: hits,
        held: [],
        note: '',
        at: 0,
        guessed: new Set<string>(),
        theme: 'dark',
        onPick: (h: SearchHit) => picked.push(h.id),
      }),
      'cd-nd-hit',
    );
    presses[2]();
    expect(picked).toEqual(['tt0234215']);
  });
});

describe('the cast list', () => {
  const codes = codesOf(todaysPeople(SOLVED));

  const castWith = (game: DailyGame, nudge: boolean) =>
    renderToStaticMarkup(
      createElement(DailyCast, {
        rows: castRows(game),
        playing: true,
        delays: new Map(),
        codes,
        theme: 'dark',
        nudge,
        onNext: () => {},
        onMovies: () => {},
      }),
    );

  it('has a hand tap the next row when the page asks, the row saying what a tap does', () => {
    const html = castWith(gameOf(), true);
    expect(html).toContain('<span class="cd-nd-next-word">Tap for the next name · −100</span>');
    // In the row, after its button, for the eye alone.
    expect(html).toMatch(
      /<button type="button" class="cd-nd-row-go" aria-label="Show the next name\. It costs 100 points\."><\/button><span class="cd-nd-hand" aria-hidden="true"><span class="cd-nd-hand-ring"><\/span><span class="cd-nd-hand-glyph"><svg width="46" height="46" viewBox="0 0 24 24" focusable="false"><path class="cd-nd-hand-body" d="M6 4a2/,
    );
    expect(html.match(/cd-nd-hand"/g)).toHaveLength(1);
  });

  it('keeps the row as it was otherwise, and has no hand without a next row', () => {
    const plain = castWith(gameOf(), false);
    expect(plain).toContain('<span class="cd-nd-next-word">Next</span>');
    expect(plain).not.toContain('cd-nd-hand');
    expect(castWith(gameOf({ pts: 100 }), true)).not.toContain('cd-nd-hand');
  });

  it('holds every name back while Play hides them, so the first comes in from nothing', () => {
    const html = renderToStaticMarkup(
      createElement(DailyCast, {
        rows: openingRows(),
        playing: true,
        delays: new Map(),
        codes,
        theme: 'dark',
        onNext: () => {},
        onMovies: () => {},
      }),
    );
    expect(html.match(/cd-nd-row-hidden/g)).toHaveLength(6);
    expect(html).not.toContain('cd-nd-name');
    expect(html).not.toContain('cd-nd-row-go');
  });

  it('brings the names the reader didn’t see in one after another at the end, 350ms in and 140ms apart', () => {
    const rows = castRows(SOLVED);
    const html = renderToStaticMarkup(
      createElement(DailyCast, {
        rows,
        playing: false,
        delays: revealDelays(rows),
        codes,
        theme: 'dark',
        onNext: () => {},
        onMovies: () => {},
      }),
    );
    // Laurence Fishburne and Keanu Reeves were not seen: each one's
    // placeholder, face and name wait their turn.
    expect(html.match(/transition-delay:350ms/g)).toHaveLength(4);
    expect(html.match(/transition-delay:490ms/g)).toHaveLength(4);
    expect(html).not.toContain('transition-delay:630ms');
    // And nothing is offered once it is over.
    expect(html).not.toContain('cd-nd-movies');
  });

  it('asks for the next name, and opens a shown name’s Movies, when pressed', () => {
    const asked: string[] = [];
    const el = createElement(DailyCast, {
      rows: castRows(PLAYING),
      playing: true,
      delays: new Map(),
      codes,
      theme: 'dark',
      onNext: () => asked.push('next'),
      onMovies: (p: DailyPerson) => asked.push(p.id),
    });
    pressesIn(el, 'cd-nd-row-go').forEach((press) => press());
    pressesIn(el, 'cd-nd-movies')[1]();
    expect(asked).toEqual(['next', GLORIA.id]);
  });

  it('asks about the chosen name’s Movies alone once a map is chosen: the shut ones cannot be pressed', () => {
    const asked: string[] = [];
    const el = createElement(DailyCast, {
      rows: castRows(CHOSEN),
      playing: true,
      delays: new Map(),
      codes,
      theme: 'dark',
      onNext: () => {},
      onMovies: (p: DailyPerson) => asked.push(p.id),
    });
    const presses = pressesIn(el, 'cd-nd-movies');
    expect(presses).toHaveLength(1);
    presses[0]();
    expect(asked).toEqual([JOE.id]);
  });
});

describe('the page once the game is over', () => {
  it('turns the card over to the poster, and heads the page with the answer’s title and no line', () => {
    const html = drawn(SOLVED);
    expect(html).toContain('<div class="cd-nd-card" aria-hidden="true"><div class="cd-nd-card-in cd-nd-card-over">');
    expect(html).toContain('<div class="cd-nd-card-in cd-nd-card-over">');
    expect(html).toMatch(/<div class="cd-nd-card-back" style="--poster-fill:linear-gradient\(165deg, oklch\(0\.45 0\.07 \d+\), oklch\(0\.28 0\.05 \d+\)\)">/);
    expect(html).toContain('<h1 class="cd-nd-heading">The Matrix</h1>');
    expect(html).not.toContain('cd-nd-line');
    // The wrong guess stays, as words, not a control.
    expect(html).toContain('<span class="cd-nd-tried-chip" style="--tone:oklch(0.82 0.13 78)">');
    expect(html).not.toContain('Show what it told you');
    expect(html).not.toContain('Show the answer');
  });

  it('writes the stand-in poster’s title on a movie the catalog has no picture of', () => {
    const html = drawn(ended([{ type: 'win' }], { pts: 1000, won: true, end: { answer: { ...MATRIX, poster: undefined }, directors: [LANA, LILLY] } }));
    expect(html).toContain('<span class="cd-nd-card-standin">The Matrix</span>');
  });

  it('reorders the page: the top block, About the movie, the result, the leaderboard, “The cast” and the names', () => {
    const html = drawn(SOLVED);
    const order = ['cd-nd-top', 'cd-nd-facts', 'cd-nd-res', 'cd-nd-board', 'cd-nd-cast-head', 'cd-nd-cast'].map((c) =>
      at(html, c),
    );
    expect(order.every((n) => n >= 0)).toBe(true);
    expect([...order].sort((a, b) => a - b)).toEqual(order);
    expect(html).toContain('<h2 class="cd-nd-cast-head">The cast</h2>');
    // No guess bar, and no Movies.
    expect(html).not.toContain('cd-nd-bar');
    expect(html).not.toContain('cd-nd-movies');
  });

  it('says every fact exactly, as About the movie, in the facts panel with no note and no nudge', () => {
    const html = drawn(SOLVED);
    expect(html).toContain(
      '<section class="cd-nd-facts" aria-labelledby="cd-nd-about-head"><div class="cd-nd-facts-top"><h2 id="cd-nd-about-head" class="cd-nd-facts-head">About the movie</h2></div><div class="cd-nd-fact-list">',
    );
    expect(html).not.toContain('cd-nd-facts-note');
    const facts = [...html.matchAll(/<span class="cd-nd-fact-label">([^<]+)<\/span><span class="cd-nd-fact-value">([^<]+)<\/span>/g)].map(
      (m) => `${m[1]} ${m[2]}`,
    );
    expect(facts).toEqual([
      'Length 2h 16m',
      'Genre Action, Sci-Fi',
      'IMDb rating 8.7',
      'Year 1999',
      'Director Lana Wachowski and Lilly Wachowski',
    ]);
    expect(html).not.toContain('class="cd-nd-fact"');
  });

  it('shows all six names, with no next row', () => {
    const html = drawn(OUT);
    expect(html.match(/cd-nd-row-shown/g)).toHaveLength(6);
    expect(html).not.toContain('cd-nd-row-go');
  });
});

describe('the result', () => {
  const codes = codesOf(todaysPeople(SOLVED));
  const result = (game: DailyGame, over: Partial<ComponentProps<typeof DailyResult>> = {}) =>
    renderToStaticMarkup(
      createElement(DailyResult, {
        today: todayOf(game),
        game,
        board: null,
        fresh: false,
        offset: 0,
        codes,
        theme: 'dark',
        onShare: () => {},
        onOpenMovie: () => {},
        ...over,
      }),
    );
  const BOARD: DailyBoard = {
    tab: 'today',
    total: 61240,
    you: { place: 12427, tied: true, pts: 600 },
    rows: [],
    beat: 78,
    solved: 92,
    chart: [40, 60, 80, 100, 120, 140, 160, 120, 60, 20, 5],
  };

  it('heads a solve with how it ended, the score, and the streak', () => {
    const html = result(SOLVED);
    expect(html).toContain('<section class="cd-nd-res" aria-label="Your result">');
    expect(html).toContain('<span class="cd-nd-res-kicker">Got it</span>');
    expect(html).toContain(
      '<span class="cd-nd-res-n" aria-hidden="true">600</span><span class="cd-sr-live">600</span><span class="cd-nd-res-word">points</span>',
    );
    expect(html).toMatch(/<span class="cd-nd-streak"><svg [^>]*aria-hidden="true">[\s\S]*?<\/svg>2-day streak<\/span>/);
  });

  it('counts the score up from nought when the game has just ended here, and simply shows it otherwise', () => {
    expect(result(SOLVED, { fresh: true })).toContain('<span class="cd-nd-res-n" aria-hidden="true">0</span><span class="cd-sr-live">600</span>');
    expect(result(SOLVED, { fresh: false })).toContain('<span class="cd-nd-res-n" aria-hidden="true">600</span>');
    // With stillness asked for the score doesn't count.
    const media = (q: string) => ({ matches: q === '(prefers-reduced-motion: reduce)' });
    vi.stubGlobal('window', { innerWidth: 1366, innerHeight: 768, matchMedia: media });
    vi.stubGlobal('matchMedia', media);
    expect(result(SOLVED, { fresh: true })).toContain('<span class="cd-nd-res-n" aria-hidden="true">600</span>');
  });

  it('counts the names it took as six faces, the ones not seen faded and grey, and lists what was paid for', () => {
    const html = result(SOLVED);
    expect(html).toContain('<span class="cd-nd-res-line">You needed 4 of the 6 names</span>');
    expect(html.match(/class="cd-nd-res-face"/g)).toHaveLength(6);
    expect(html.match(/cd-nd-face-dim/g)).toHaveLength(2);
    expect(html).toContain('<span class="cd-nd-res-face" title="Keanu Reeves"><span class="cd-nd-face cd-nd-face-result cd-nd-face-dim"');
    expect(html).toContain(
      '<div class="cd-nd-paid"><span class="cd-nd-paid-chip">Decade<span class="cd-nd-paid-cost">−100</span></span><span class="cd-nd-paid-chip">1 wrong guess<span class="cd-nd-paid-cost">−100</span></span></div>',
    );
  });

  it('says how today’s players did, with a chart of eleven bars and the reader’s own lit', () => {
    const html = result(SOLVED, { board: BOARD });
    expect(html).toContain('<p class="cd-nd-better"><span class="cd-nd-better-n">78%</span> of today’s players scored less than you.</p>');
    const bars = [...html.matchAll(/<span class="(cd-nd-chart-bar[^"]*)" style="height:(\d+)%"><\/span>/g)];
    expect(bars).toHaveLength(11);
    expect(bars[6][1]).toBe('cd-nd-chart-bar cd-nd-chart-you');
    expect(bars.filter((b) => b[1] !== 'cd-nd-chart-bar')).toHaveLength(1);
    expect(bars[10][2]).toBe('6');
    expect(html).toContain('<div class="cd-nd-chart-labels" aria-hidden="true"><span>0</span><span>500</span><span>1,000</span></div>');
  });

  it('holds the line open, with the chart flat, while today’s board is on its way', () => {
    const html = result(SOLVED);
    expect(html).toContain('<p class="cd-nd-better"> </p>');
    expect(html.match(/style="height:6%"/g)).toHaveLength(11);
  });

  it('heads each kind of miss, counts the names seen, and lights the first bar in the miss colour', () => {
    const out = result(OUT, { board: BOARD });
    expect(out).toContain('<span class="cd-nd-res-kicker">Your points ran out</span>');
    expect(out).toContain('<span class="cd-nd-res-n" aria-hidden="true">0</span>');
    expect(out).toContain('You saw 2 of the 6 names');
    expect(out).toContain('<span class="cd-nd-better-n">92%</span> of today’s players got it.');
    expect(out).toContain('<span class="cd-nd-chart-bar cd-nd-chart-miss" style="height:25%"></span>');
    expect(out).toContain('Streak ended');
    const gave = result(GAVE_UP);
    expect(gave).toContain('<span class="cd-nd-res-kicker">You asked for the answer</span>');
    expect(gave).toContain('You saw 1 of the 6 names');
    expect(gave).not.toContain('cd-nd-paid');
  });

  it('offers Share result, Map this movie to the answer’s map, and the countdown to the next movie', () => {
    const html = result(SOLVED);
    expect(html).toMatch(/<button type="button" class="cd-nd-share"><svg [^>]*aria-hidden="true">[\s\S]*?<\/svg>Share result<\/button>/);
    expect(html).toContain('<a class="cd-nd-mapit" href="/movie/tt0133093-the-matrix">Map this movie</a>');
    expect(html).toMatch(/<span class="cd-nd-countdown-label">Next movie in<\/span><span class="cd-nd-countdown-n">\d+:\d\d:\d\d<\/span>/);
  });

  it('leaves for the answer’s map through the app’s own way, and leaves a modified click to the browser', () => {
    const opened: string[] = [];
    const el = createElement(DailyResult, {
      today: todayOf(SOLVED),
      game: SOLVED,
      board: null,
      fresh: false,
      offset: 0,
      codes,
      theme: 'dark',
      onShare: () => {},
      onOpenMovie: (id: string, title: string) => opened.push(`${id} ${title}`),
    });
    const [press] = pressesIn(el, 'cd-nd-mapit') as unknown as ((e: unknown) => void)[];
    let kept = 0;
    press({ metaKey: false, ctrlKey: false, shiftKey: false, altKey: false, button: 0, preventDefault: () => kept++ });
    expect(opened).toEqual(['tt0133093 The Matrix']);
    expect(kept).toBe(1);
    press({ metaKey: true, ctrlKey: false, shiftKey: false, altKey: false, button: 0, preventDefault: () => kept++ });
    expect(opened).toHaveLength(1);
    expect(kept).toBe(1);
  });
});

describe('the leaderboard', () => {
  const today: DailyBoard = {
    tab: 'today',
    total: 61240,
    you: { place: 12427, tied: true, pts: 600 },
    rows: [
      { place: 9849, tied: true, name: 'Ferris Corleone', hue: 10, pts: 700, you: false },
      { place: 11063, tied: false, name: 'Ennis Hooper', hue: 20, pts: 650, you: false },
      { place: 12427, tied: true, name: 'Trinity Kimble', hue: 30, pts: 600, you: true },
      { place: 12427, tied: true, name: 'Clarice McFly', hue: 40, pts: 600, you: false },
      { place: 13959, tied: false, name: 'Holly Deckard', hue: 50, pts: 550, you: false },
    ],
    beat: 78,
    solved: 92,
    chart: null,
  };
  const week: DailyBoard = {
    tab: 'week',
    total: 83500,
    you: { place: 1204, tied: false, pts: 2650, days: [700, 0, 850, 500, 600] },
    rows: [
      { place: 1203, tied: false, name: 'Vito Gunderson', hue: 10, pts: 2700, days: [700, 0, 900, 500, 600], you: false },
      { place: 1204, tied: false, name: 'Trinity Kimble', hue: 30, pts: 2650, days: [700, 0, 850, 500, 600], you: true },
    ],
    beat: null,
    solved: null,
  };
  const board = (tab: 'today' | 'week', boards: ComponentProps<typeof DailyLeaderboard>['boards']) =>
    renderToStaticMarkup(createElement(DailyLeaderboard, { date: '2026-10-09', tab, boards, onTab: () => {}, onRetry: () => {} }));

  it('shows the players around the reader under Today and This week tabs, places shared with an equals sign', () => {
    const html = board('today', { today });
    expect(html).toMatch(/<h2 id="[^"]+" class="cd-nd-board-title">Leaderboard<\/h2>/);
    expect(html).toContain('<div class="cd-nd-tabs" role="tablist" aria-label="Leaderboard">');
    expect(html).toMatch(/role="tab" aria-selected="true" aria-controls="[^"]+" tabindex="0" class="cd-nd-tab cd-nd-tab-on">Today</);
    expect(html).toMatch(/role="tab" aria-selected="false" aria-controls="[^"]+" tabindex="-1" class="cd-nd-tab">This week</);
    expect(html).toContain(
      '<li class="cd-nd-board-row cd-nd-board-you"><span class="cd-nd-place">=12,427</span><span class="cd-nd-board-who"><span class="cd-nd-board-name">You</span></span><span class="cd-nd-board-pts">600</span></li>',
    );
    expect(html).toContain('<span class="cd-nd-place">11,063</span>');
    expect(html).toContain(
      '<p class="cd-nd-board-note">The players around your score. Equal scores share a place. 61,240 played today.</p>',
    );
    expect(html).not.toContain('cd-nd-board-days');
  });

  it('gives each name a cell for every day this week, under the days’ initials, a day not played as a faint 0', () => {
    const html = board('week', { today, week });
    expect(html).toContain(
      '<div class="cd-nd-board-days" aria-hidden="true"><span class="cd-nd-board-day">M</span><span class="cd-nd-board-day">T</span><span class="cd-nd-board-day">W</span><span class="cd-nd-board-day">T</span><span class="cd-nd-board-day">F</span></div>',
    );
    expect(html).toContain(
      '<span class="cd-nd-days"><span class="cd-nd-day">700</span><span class="cd-nd-day cd-nd-day-zero">0</span><span class="cd-nd-day">850</span>',
    );
    expect(html).toContain('A day you didn’t play counts as 0.');
  });

  it('says it is loading, and offers to try again when it did not load', () => {
    expect(board('today', {})).toContain('<p class="cd-nd-board-note">Loading the leaderboard…</p>');
    const failed = board('week', { today, week: 'failed' });
    expect(failed).toContain('The leaderboard didn’t load.');
    expect(failed).toContain('<button type="button" class="cd-nd-board-again">Try again</button>');
  });

  it('lists nobody without the reader on it, and says whose places these would be', () => {
    const html = board('today', { today: { ...today, rows: [], you: null } });
    expect(html).not.toContain('cd-nd-board-rows');
    expect(html).toContain('61,240 played today.');
  });
});

describe('How it works', () => {
  it('is a modal dialog of six numbered items, the prices and Got it', () => {
    const html = renderToStaticMarkup(createElement(DailyRules, { onClose: () => {} }));
    expect(html).toContain(
      '<div class="cd-nd-rules" role="dialog" aria-modal="true" aria-labelledby="cd-nd-rules-title" tabindex="-1">',
    );
    expect(html).toContain('<h2 id="cd-nd-rules-title" class="cd-nd-rules-title">How it works</h2>');
    expect(html.match(/<li class="cd-nd-rules-item">/g)).toHaveLength(6);
    expect(html).toContain(
      'Today’s movie starts as a blank card in its poster’s colour, and you see one person from its cast, with another movie they were in.',
    );
    expect(html).toContain(
      '<span>Tap Movies on a name to see their movies on a Cinedikt map. You get one map a game, so choose whose. Movies are named only inside the decade or rating range you’ve bought, and today’s movie is always one of them.</span>',
    );
    expect(html).toContain(
      '<p class="cd-nd-rules-then">You start with 1,000 points. Each extra name costs 100. Wrong guesses cost 100, then 150, 200 and so on. Facts cost 50 to 250. There’s no clock.</p>',
    );
    expect(html).toContain('<button type="button" class="cd-nd-rules-close">Got it</button>');
  });

  it('closes from Got it and from the scrim', () => {
    let closed = 0;
    const el = createElement(DailyRules, { onClose: () => closed++ });
    pressesIn(el, 'cd-nd-rules-close')[0]();
    pressesIn(el, 'cd-nd-rules-scrim')[0]();
    expect(closed).toBe(2);
  });

  it('keeps Tab inside it, round from the last to the first and back', () => {
    expect(nextStop(-1, 1, false)).toBe(0);
    expect(nextStop(0, 1, false)).toBe(0);
    expect(nextStop(0, 1, true)).toBe(0);
    expect(nextStop(0, 3, true)).toBe(2);
    expect(nextStop(2, 3, false)).toBe(0);
    expect(nextStop(-1, 0, false)).toBe(-1);
  });
});

describe('the question before the one Movies map', () => {
  const ask = (onYes = () => {}, onNo = () => {}) =>
    createElement(DailySheetAsk, { name: 'Joe Pantoliano', onYes, onNo });

  it('is a small modal dialog that asks by name, says the rule, and offers Not now and Open the map', () => {
    expect(renderToStaticMarkup(ask())).toBe(
      '<div class="cd-nd-choose-scrim">' +
        '<div class="cd-nd-choose" role="alertdialog" aria-modal="true" aria-labelledby="cd-nd-choose-title" aria-describedby="cd-nd-choose-body" tabindex="-1">' +
        '<h2 id="cd-nd-choose-title" class="cd-nd-choose-title">Open Joe Pantoliano’s movies?</h2>' +
        '<p id="cd-nd-choose-body" class="cd-nd-choose-body">You get one Movies map a game. The other names’ maps stay closed.</p>' +
        '<div class="cd-nd-choose-row">' +
        '<button type="button" class="cd-nd-choose-no">Not now</button>' +
        '<button type="button" class="cd-nd-choose-yes">Open the map</button>' +
        '</div></div></div>',
    );
  });

  it('opens the map from Open the map alone, and leaves it closed from Not now and the scrim', () => {
    const said: string[] = [];
    const el = ask(
      () => said.push('yes'),
      () => said.push('no'),
    );
    pressesIn(el, 'cd-nd-choose-no')[0]();
    pressesIn(el, 'cd-nd-choose-scrim')[0]();
    expect(said).toEqual(['no', 'no']);
    pressesIn(el, 'cd-nd-choose-yes')[0]();
    expect(said).toEqual(['no', 'no', 'yes']);
  });

  it('keeps Tab inside it, round from the last answer to the first and back, as How it works does', () => {
    const stops = [{ focus: vi.fn() }, { focus: vi.fn() }];
    const box = { querySelectorAll: () => stops } as unknown as HTMLElement;
    const tab = (shiftKey: boolean, key = 'Tab') => {
      const e = { key, shiftKey, preventDefault: vi.fn() };
      keepTabIn(box, e as unknown as Parameters<typeof keepTabIn>[1]);
      return e;
    };
    // From Open the map, the last, round to Not now.
    vi.stubGlobal('document', { activeElement: stops[1] });
    expect(tab(false).preventDefault).toHaveBeenCalled();
    expect(stops[0].focus).toHaveBeenCalledTimes(1);
    // From Not now, back round to Open the map.
    vi.stubGlobal('document', { activeElement: stops[0] });
    tab(true);
    expect(stops[1].focus).toHaveBeenCalledTimes(1);
    // From the dialog itself, as it opens, to the first.
    vi.stubGlobal('document', { activeElement: box });
    tab(false);
    expect(stops[0].focus).toHaveBeenCalledTimes(2);
    // Any other key is the dialog's own business, and so is a Tab with no
    // dialog to keep it in.
    expect(tab(false, 'Enter').preventDefault).not.toHaveBeenCalled();
    const lost = { key: 'Tab', shiftKey: false, preventDefault: () => {} };
    keepTabIn(null, lost as unknown as Parameters<typeof keepTabIn>[1]);
    expect(stops[0].focus).toHaveBeenCalledTimes(2);
  });

  it('says "movie", never "film"', () => {
    expect(words(renderToStaticMarkup(ask()))).not.toMatch(/\bfilms?\b/i);
    expect(words(drawn(CHOSEN))).not.toMatch(/\bfilms?\b/i);
  });
});

describe('Play again, in development', () => {
  const AGAIN = '<button type="button" class="cd-nd-again">Play again (development only)</button>';

  it('sits beside “Show the answer” while the game is on', () => {
    expect(drawn(gameOf(), true)).toContain(
      `<div class="cd-nd-giveup-row"><button type="button" class="cd-nd-giveup">Show the answer</button>${AGAIN}</div>`,
    );
  });

  it('comes last in the result', () => {
    const html = drawn(SOLVED, true);
    expect(html).toContain(`${AGAIN}</section>`);
    expect(html.match(/cd-nd-again/g)).toHaveLength(1);
  });

  it('is never offered unless the server says it is not in production, nor on the title screen', () => {
    for (const game of [null, gameOf(), SOLVED]) {
      expect(drawn(game)).not.toContain('cd-nd-again');
      expect(words(drawn(game))).not.toContain('development only');
    }
    expect(drawn(null, true)).not.toContain('cd-nd-again');
  });

  it('asks the page to start again when pressed, from either place', () => {
    for (const game of [gameOf(), SOLVED]) {
      const again = vi.fn();
      const presses = pressesIn(createElement(DailyGameView, viewProps(game, true, again)), 'cd-nd-again');
      expect(presses).toHaveLength(1);
      presses[0]();
      expect(again).toHaveBeenCalledTimes(1);
    }
  });
});

describe('the Daily’s words on the page', () => {
  it('say "movie", never "film", in every state', () => {
    for (const game of [null, gameOf(), PLAYING, SOLVED, OUT, GAVE_UP]) {
      expect(words(drawn(game))).not.toMatch(/\bfilms?\b/i);
      expect(words(drawn(game, true))).not.toMatch(/\bfilms?\b/i);
    }
    expect(words(renderToStaticMarkup(createElement(DailyRules, { onClose: () => {} })))).not.toMatch(/\bfilms?\b/i);
  });
});

describe('starting again', () => {
  it('starts the page again from nothing once the server has started the reader again, and not before', async () => {
    // Before, the page would ask for today's puzzle and be handed the old
    // movie's game back.
    const order: string[] = [];
    let answer = () => {};
    const done = playAgain(
      () =>
        new Promise<void>((resolve) => {
          order.push('reset');
          answer = resolve;
        }),
      () => order.push('restart'),
      (text) => order.push(`say ${text}`),
    );
    await Promise.resolve();
    expect(order).toEqual(['reset']);
    answer();
    await done;
    expect(order).toEqual(['reset', 'restart']);
  });

  it('leaves the page as it was, and says so, when the reset is refused or never answered', async () => {
    for (const failure of [
      new ApiError('no other movie fits today’s puzzle', 503, 'unavailable', {}),
      new TypeError('Failed to fetch'),
    ]) {
      const restart = vi.fn();
      const said: string[] = [];
      await playAgain(() => Promise.reject(failure), restart, (text) => said.push(text));
      expect(restart, failure.name).not.toHaveBeenCalled();
      expect(said, failure.name).toEqual(['Couldn’t start again. Try again.']);
    }
  });

  it('starts the reader again as one who has seen none of the first-time hints, only once the server has', async () => {
    const kept = new Map([[HINTS_KEY, '{"idle":1,"map":1,"warm":1,"coach":1}']]);
    vi.stubGlobal('localStorage', {
      getItem: (k: string) => kept.get(k) ?? null,
      setItem: (k: string, v: string) => void kept.set(k, v),
      removeItem: (k: string) => void kept.delete(k),
    });
    await playAgain(() => Promise.reject(new TypeError('Failed to fetch')), vi.fn(), () => {});
    expect(kept.has(HINTS_KEY)).toBe(true);
    let seenAtRestart = true;
    await playAgain(
      () => Promise.resolve(),
      () => {
        seenAtRestart = hintSeen('idle');
      },
      () => {},
    );
    expect(kept.has(HINTS_KEY)).toBe(false);
    expect(seenAtRestart).toBe(false);
  });
});

describe('the header’s end on /daily', () => {
  it('names the puzzle by number and day, and offers the rules', () => {
    const html = renderToStaticMarkup(createElement(DailyHeaderTail, { day: { no: 143, date: '2026-10-09' }, onRules: () => {} }));
    expect(html).toContain('<span class="cd-daily-pill">Daily</span>');
    expect(html).toContain('No. 143 · Friday 9 October');
    expect(html).toContain('aria-label="How it works"');
  });

  it('names it by number alone on a phone, read from the window as it is, and not at all before it has loaded', () => {
    vi.stubGlobal('window', { innerWidth: 375, innerHeight: 540 });
    const phone = renderToStaticMarkup(createElement(DailyHeaderTail, { day: { no: 143, date: '2026-10-09' }, onRules: () => {} }));
    expect(phone).toContain('>No. 143<');
    // Whatever the app last measured: the window says it is a phone.
    const told = renderToStaticMarkup(
      createElement(DailyHeaderTail, { day: { no: 143, date: '2026-10-09' }, phone: false, onRules: () => {} }),
    );
    expect(told).toContain('>No. 143<');
    const none = renderToStaticMarkup(createElement(DailyHeaderTail, { day: null, onRules: () => {} }));
    expect(none).not.toContain('cd-daily-date');
  });
});

describe('the page as it opens', () => {
  it('draws nothing in its box while today’s puzzle is on its way', () => {
    expect(renderToStaticMarkup(createElement(DailyPage))).toBe('<div class="cd-daily"></div>');
  });

  it('steps back while a map loads over it', () => {
    expect(renderToStaticMarkup(createElement(DailyPage, { dim: true }))).toBe(
      '<div class="cd-daily cd-daily-dim"></div>',
    );
  });
});

// ---- the stylesheet ----

/** Every rule in the sheet outside an @media block with this selector
 *  among its own, as `prop: value` declarations, comments gone. */
function decls(selector: string): Map<string, string> {
  const text = css.replace(/\/\*[\s\S]*?\*\//g, '').replace(/@media[^{]*\{[\s\S]*?\n\}/g, '');
  const out = new Map<string, string>();
  for (const m of text.matchAll(/([^{}]+)\{([^{}]*)\}/g)) {
    const sels = m[1].split(',').map((s) => s.trim().replace(/\s+/g, ' '));
    if (!sels.includes(selector)) continue;
    for (const d of m[2].split(';')) {
      const i = d.indexOf(':');
      if (i > 0) out.set(d.slice(0, i).trim(), d.slice(i + 1).trim().replace(/\s+/g, ' '));
    }
  }
  return out;
}

/** The same, for the rules inside the sheet's blocks for one media
 *  query, written as the sheet writes it. */
function mediaDecls(query: string, selector: string): Map<string, string> {
  const text = css.replace(/\/\*[\s\S]*?\*\//g, '');
  const out = new Map<string, string>();
  for (const block of text.matchAll(/@media ([^{]*)\{([\s\S]*?)\n\}/g)) {
    if (block[1].trim() !== query) continue;
    for (const m of block[2].matchAll(/([^{}]+)\{([^{}]*)\}/g)) {
      const sels = m[1].split(',').map((s) => s.trim().replace(/\s+/g, ' '));
      if (!sels.includes(selector)) continue;
      for (const d of m[2].split(';')) {
        const i = d.indexOf(':');
        if (i > 0) out.set(d.slice(0, i).trim(), d.slice(i + 1).trim().replace(/\s+/g, ' '));
      }
    }
  }
  return out;
}

/** The rules inside the reduced-motion blocks, as one text. */
function stillRules(): string {
  const text = css.replace(/\/\*[\s\S]*?\*\//g, '');
  return [...text.matchAll(/@media \(prefers-reduced-motion: reduce\) \{([\s\S]*?)\n\}/g)].map((m) => m[1]).join('\n');
}

describe('Name Drop’s stylesheet', () => {
  it('sets the first-time hand in the next row at the handoff’s place, out of the way of a press', () => {
    const hand = decls('.cd-nd-hand');
    for (const [k, v] of [
      ['position', 'absolute'],
      ['z-index', '3'],
      ['right', '12px'],
      ['top', '50%'],
      ['width', '46px'],
      ['height', '46px'],
      ['margin-top', '-4px'],
      ['pointer-events', 'none'],
    ])
      expect(hand.get(k), k).toBe(v);
    expect(decls('.cd-nd-row').get('position')).toBe('relative');
    // The ring is centred on the fingertip, 15px across and 4px down.
    const ring = decls('.cd-nd-hand-ring');
    expect([ring.get('left'), ring.get('top'), ring.get('width'), ring.get('opacity')]).toEqual(['-7px', '-18px', '44px', '0']);
    expect(-7 + 44 / 2).toBe(15);
    expect(-18 + 44 / 2).toBe(4);
    expect(decls('.cd-nd-hand-glyph').get('transform-origin')).toBe('15px 4px');
    expect(decls('.cd-nd-hand-glyph').get('filter')).toBe('var(--nd-hand-shadow)');
    expect(decls('.cd-daily').get('--nd-hand-shadow')).toBe('drop-shadow(0 6px 10px rgba(0, 0, 0, 0.35))');
    expect(decls('.cd-nd-hand-body').get('stroke-width')).toBe('1.3');
    expect(decls('.cd-nd-hand-crease').get('stroke-width')).toBe('1.1');
  });

  it('draws the warmth note on the anchor’s wash, wrapping, and gives back its gap when empty', () => {
    const tip = decls('.cd-nd-tip');
    expect([tip.get('padding'), tip.get('border-radius'), tip.get('background'), tip.get('box-shadow')]).toEqual([
      '4px 4px 4px 12px',
      '12px',
      'var(--ancBg)',
      'inset 0 0 0 1px var(--accSoft)',
    ]);
    expect(decls('.cd-nd-tip-text').get('font-size')).toBe('13px');
    expect(decls('.cd-nd-tip-text').get('text-wrap')).toBe('pretty');
    expect(decls('.cd-nd-tipwrap:empty').get('margin-top')).toBe('-8px');
  });

  it('sets out the column, the card and the rows at the design’s numbers', () => {
    expect(decls('.cd-nd-col').get('max-width')).toBe('600px');
    expect(decls('.cd-nd-col').get('padding')).toBe('18px clamp(14px, 4vw, 20px) 22px');
    expect(decls('.cd-nd-col').get('gap')).toBe('16px');
    expect(decls('.cd-nd-card').get('width')).toBe('clamp(64px, 18vw, 104px)');
    expect(decls('.cd-nd-card-q').get('font-size')).toBe('clamp(30px, 8vw, 52px)');
    expect(decls('.cd-nd-card-in').get('transition')).toBe('transform 0.8s cubic-bezier(0.4, 0, 0.2, 1)');
    expect(decls('.cd-nd-card-over').get('transform')).toBe('rotateY(180deg)');
    expect(decls('.cd-nd-row').get('min-height')).toBe('58px');
    expect(decls('.cd-nd-row').get('padding')).toBe('6px 12px 6px 8px');
    expect(decls('.cd-nd-face').get('width')).toBe('44px');
    expect(decls('.cd-nd-face').get('box-shadow')).toBe('0 0 0 2px var(--s), 0 0 0 3.5px var(--tone)');
    expect(decls('.cd-nd-face').get('background')).toBe('color-mix(in oklch, var(--tone) 24%, var(--s))');
    expect(decls('.cd-nd-title-col').get('padding')).toBe('32px 20px 40px');
    expect(decls('.cd-nd-play').get('height')).toBe('56px');
  });

  it('draws the title screen’s picture at v2’s sizes: an 88 by 132 card glowing 38px round, and 164px rows 8px apart', () => {
    const card = decls('.cd-nd-title-card');
    expect([card.get('width'), card.get('height'), card.get('border-radius')]).toEqual(['88px', '132px', '13px']);
    expect(card.get('box-shadow')).toBe('var(--nd-edge), var(--sh), 0 0 38px var(--glow, transparent)');
    expect(card.get('rotate')).toBe('-4deg');
    expect(card.get('overflow')).toBe('hidden');
    expect(decls('.cd-nd-title-rows').get('width')).toBe('164px');
    expect(decls('.cd-nd-title-rows').get('gap')).toBe('8px');
    expect(decls('.cd-nd-title-row').get('height')).toBe('36px');
    expect(decls('.cd-nd-title-row-next').get('border')).toBe('1.5px dashed var(--acc)');
    expect(decls('.cd-nd-title-row-hidden').get('border')).toBe('1.5px dashed var(--ln3)');
    // The rows that drop in rest out of sight, over the dashed edge.
    expect(decls('.cd-nd-title-drop').get('opacity')).toBe('0');
    expect(decls('.cd-nd-title-drop').get('inset')).toBe('-1.5px');
  });

  it('rests the shines off to the side: the card’s 45% wide and tilted 12°, Play’s 40%', () => {
    const sheen = decls('.cd-nd-title-sheen');
    expect([sheen.get('width'), sheen.get('rotate'), sheen.get('translate')]).toEqual(['45%', '12deg', '-160% 0']);
    expect(sheen.get('background')).toBe('linear-gradient(100deg, transparent, var(--nd-card-shine), transparent)');
    const shine = decls('.cd-nd-play-shine');
    expect([shine.get('width'), shine.get('translate')]).toEqual(['40%', '-120% 0']);
    expect(shine.get('background')).toBe('linear-gradient(100deg, transparent, var(--nd-play-shine), transparent)');
    expect(decls('.cd-nd-play').get('overflow')).toBe('hidden');
    expect(decls('.cd-daily').get('--nd-card-shine')).toBe('rgba(255, 255, 255, 0.3)');
    expect(decls('.cd-daily').get('--nd-play-shine')).toBe('rgba(255, 255, 255, 0.4)');
  });

  it('rounds the hidden card in play, so its glow and ripples are its shape', () => {
    expect(decls('.cd-nd-card').get('border-radius')).toBe('14px');
  });

  it('sets the facts out as a panel under the card, one row of chips that scrolls sideways and clips no hit area', () => {
    const panel = decls('.cd-nd-facts');
    expect(panel.get('padding')).toBe('9px 12px 10px');
    expect(panel.get('border-radius')).toBe('14px');
    expect(panel.get('background')).toBe('var(--s)');
    expect(panel.get('box-shadow')).toBe('inset 0 0 0 1px var(--ln2)');
    expect(decls('.cd-nd-facts-nudge').get('box-shadow')).toBe('inset 0 0 0 1px var(--acc)');
    expect(decls('.cd-nd-facts-head').get('font-size')).toBe('13.5px');
    expect(decls('.cd-nd-facts-head').get('color')).toBe('var(--t)');
    expect(decls('.cd-nd-facts-note').get('font-size')).toBe('12.5px');
    expect(decls('.cd-nd-facts-note').get('color')).toBe('var(--t3)');
    expect(decls('.cd-nd-facts-nudge .cd-nd-facts-note').get('color')).toBe('var(--accText)');
    const row = decls('.cd-nd-fact-list');
    expect(row.get('flex-wrap')).toBe('nowrap');
    expect(row.get('overflow-x')).toBe('auto');
    expect(row.get('scrollbar-width')).toBe('none');
    // 5px above and below, given back by the margin, and out to the
    // panel's edges: the facts' -5px hit areas fit inside.
    expect(row.get('padding')).toBe('5px 12px');
    expect(row.get('margin')).toBe('-5px -12px');
    expect(parseFloat(HIT_INSETS.fact)).toBe(-5);
    expect(decls('.cd-nd-fact-list > *').get('flex-shrink')).toBe('0');
    expect(decls('.cd-nd-fact-list > *').get('white-space')).toBe('nowrap');
    // For sale on the page's ground.
    expect(decls('.cd-nd-fact').get('background')).toBe('var(--g)');
  });

  it('moves a name in as the design does: the placeholder out over .3s, the text up from 6px over .45s, the face growing from 0.7', () => {
    expect(decls('.cd-nd-ph').get('transition')).toBe('opacity 0.3s ease');
    expect(decls('.cd-nd-who').get('transition')).toBe('opacity 0.45s ease, translate 0.45s ease');
    expect(decls('.cd-nd-who').get('translate')).toBe('0 6px');
    expect(decls('.cd-nd-facewrap').get('scale')).toBe('0.7');
    expect(decls('.cd-nd-facewrap').get('transition')).toBe('opacity 0.45s ease, scale 0.5s cubic-bezier(0.2, 0.9, 0.3, 1.25)');
    expect(decls('.cd-nd-peek').get('left')).toBe('54px');
    expect(decls('.cd-nd-peek').get('width')).toBe('176px');
    expect(decls('.cd-nd-peek').get('transition')).toBe('opacity 0.16s ease, scale 0.22s cubic-bezier(0.2, 0.9, 0.3, 1.2)');
  });

  it('reaches 44px round every small control, as the handoff sets each one out', () => {
    expect(decls('button.cd-nd-tried-chip::before').get('inset')).toBe(HIT_INSETS.triedChip);
    expect(decls('.cd-nd-giveup::before').get('inset')).toBe(HIT_INSETS.showAnswer);
    expect(decls('.cd-nd-fact::before').get('inset')).toBe(HIT_INSETS.fact);
    expect(decls('.cd-nd-movies::before').get('inset')).toBe(HIT_INSETS.movies);
    expect(decls('.cd-nd-tab::before').get('inset')).toBe(HIT_INSETS.tab);
    expect(decls('.cd-nd-tip-x::before').get('inset')).toBe(HIT_INSETS.dismiss);
    expect(decls('.cd-nd-tip-x').get('width')).toBe('36px');
    expect(36 - 2 * parseFloat(HIT_INSETS.dismiss)).toBe(44);
    for (const c of ['.cd-nd-tried-chip', '.cd-nd-giveup', '.cd-nd-fact', '.cd-nd-movies', '.cd-nd-tab', '.cd-nd-tip-x']) {
      expect(decls(c).get('position'), c).toBe('relative');
    }
  });

  it('pins the guess bar, a 340px column on a landscape phone, with its message on one sideways row', () => {
    expect(decls('.cd-nd-bar').get('padding')).toBe('8px clamp(12px, 3vw, 24px) calc(10px + env(safe-area-inset-bottom))');
    expect(decls('.cd-nd-bar').get('box-shadow')).toBe('0 -1px 0 var(--ln2)');
    expect(decls('.cd-nd-land .cd-nd-bar').get('width')).toBe('340px');
    expect(decls('.cd-nd-land .cd-nd-bar').get('padding')).toBe('12px 14px calc(12px + env(safe-area-inset-bottom))');
    expect(decls('.cd-nd-land .cd-nd-bar').get('box-shadow')).toBe('-1px 0 0 var(--ln2)');
    expect(decls('.cd-nd-land').get('flex-direction')).toBe('row');
    expect(decls('.cd-nd-msg').get('overflow-x')).toBe('auto');
    expect(decls('.cd-nd-msg > *').get('flex-shrink')).toBe('0');
    expect(decls('.cd-nd-msg > *').get('white-space')).toBe('nowrap');
    expect(decls('.cd-nd-input').get('font-size')).toBe('16px');
    expect(decls('.cd-nd-guess:disabled').get('opacity')).toBe('0.45');
    expect(decls('.cd-nd-guess').get('white-space')).toBe('nowrap');
    // The points line and Next name are gone, rules and all.
    expect(css).not.toMatch(/\.cd-nd-(points|nextbtn|bar-row)\b/);
    expect(decls('.cd-daily .cd-toast').get('bottom')).toBe('calc(170px + env(safe-area-inset-bottom))');
  });

  it('keeps the results inside the column on a landscape phone, scrolling rather than running off the top', () => {
    // Six results above the field are 288px and more, taller than a
    // landscape phone's column: hung above it, the best match (drawn
    // first, and the one Enter guesses) went off the top of the screen.
    const hits = decls('.cd-nd-land .cd-nd-hits');
    expect(hits.get('position')).toBe('static');
    expect(hits.get('overflow-y')).toBe('auto');
    expect(hits.get('flex-shrink')).toBe('1');
    // Never less than the panel's top padding and one result, the one
    // Enter guesses.
    expect(hits.get('min-height')).toBe(`${6 + 46}px`);
    expect(decls('.cd-nd-hit').get('min-height')).toBe('46px');
    expect(decls('.cd-nd-hits').get('padding')).toBe('6px');
    const inner = decls('.cd-nd-land .cd-nd-bar-in');
    expect(inner.get('min-height')).toBe('0');
    expect(inner.get('justify-content')).toBe('flex-end');
    expect(css.replace(/\/\*[\s\S]*?\*\//g, '')).toMatch(
      /\.cd-nd-land :is\(\.cd-nd-msgwrap, \.cd-nd-ask\) \{\s*flex-shrink: 0;\s*\}/,
    );
    // Portrait keeps the list hung above the field, over the page.
    expect(decls('.cd-nd-hits').get('position')).toBe('absolute');
    expect(decls('.cd-nd-hits').get('bottom')).toBe('calc(100% + 8px)');
  });

  it('dims Play and shows the waiting cursor while the game is being started', () => {
    const held = decls('.cd-nd-title .cd-nd-play:disabled');
    expect(held.get('cursor')).toBe('progress');
    expect(held.get('filter')).toBe('brightness(0.9)');
  });

  it('sizes the app to the visual viewport while the Daily is up', () => {
    const fit = decls('.cd-app:has(> .cd-daily)');
    expect(fit.get('position')).toBe('fixed');
    expect(fit.get('top')).toBe('var(--vv-top, 0px)');
    expect(fit.get('height')).toBe('var(--vv-h, auto)');
  });

  it('draws the chart and the week’s cells at the design’s sizes', () => {
    expect(decls('.cd-nd-chart').get('height')).toBe('52px');
    expect(decls('.cd-nd-chart').get('gap')).toBe('4px');
    expect(decls('.cd-nd-chart-bar').get('border-radius')).toBe('4px 4px 2px 2px');
    expect(decls('.cd-nd-place').get('width')).toBe('58px');
    expect(decls('.cd-nd-board-days').get('padding-left')).toBe('76px');
    expect(decls('.cd-nd-day').get('width')).toBe('30px');
    expect(decls('.cd-nd-day').get('height')).toBe('18px');
  });

  it('fits a cell for every day on a phone, Sunday’s seven included, without running over the total', () => {
    const phone = '(max-width: 639.98px)';
    const row = mediaDecls(phone, '.cd-nd-board-row:has(.cd-nd-days)');
    expect(row.get('display')).toBe('grid');
    expect(row.get('grid-template-columns')).toBe('58px minmax(0, 1fr) auto');
    expect(row.get('grid-template-areas')).toBe(`'place name pts' 'place days days'`);
    expect(row.get('gap')).toBe('3px 8px');
    expect(mediaDecls(phone, '.cd-nd-board-row:has(.cd-nd-days) .cd-nd-board-who').get('display')).toBe('contents');
    expect(mediaDecls(phone, '.cd-nd-days').get('grid-area')).toBe('days');
    for (const c of ['.cd-nd-day', '.cd-nd-board-day']) {
      expect(mediaDecls(phone, c).get('flex'), c).toBe('0 1 30px');
      expect(mediaDecls(phone, c).get('min-width'), c).toBe('0');
    }
    // The initials reach the row's right padding as the cells do, so the
    // two strips are as wide as each other and shrink alike.
    const pad = parseFloat(decls('.cd-nd-board-row').get('padding')!.split(' ')[1]);
    expect(mediaDecls(phone, '.cd-nd-board-days').get('padding-right')).toBe(`${pad}px`);
    expect(decls('.cd-nd-board-days').get('padding-left')).toBe(`${pad + 58 + 8}px`);

    // Under 360px the cells take the place column's room too, and the
    // initials start at the row's padding.
    const narrow = '(max-width: 359.98px)';
    expect(mediaDecls(narrow, '.cd-nd-board-row:has(.cd-nd-days)').get('grid-template-areas')).toBe(
      `'place name pts' 'days days days'`,
    );
    expect(mediaDecls(narrow, '.cd-nd-board-days').get('padding-left')).toBe(`${pad}px`);

    // The arithmetic, from the sheet's own numbers: the days' strip on a
    // phone is the panel less the row's padding and, from 360px, the
    // place column and its gap. Seven 30px cells fit from 375px, and
    // under 360; between, each gives a pixel, which a "1,000" (29px at
    // 10.5px) still fits.
    const cell = parseFloat(decls('.cd-nd-day').get('width')!);
    const gap = parseFloat(decls('.cd-nd-days').get('gap')!);
    const seven = 7 * cell + 6 * gap;
    const strip = (width: number) => {
      const col = width - 2 * Math.min(Math.max(14, 0.04 * width), 20);
      const panel = col - 2 * parseFloat(decls('.cd-nd-board').get('padding')!.split(' ')[1]);
      return panel - 2 * pad - (width < 360 ? 0 : 58 + 8);
    };
    expect(seven).toBe(228);
    for (const w of [320, 340, 359, 375, 390, 414]) expect(strip(w), `${w}px`).toBeGreaterThanOrEqual(seven);
    expect((strip(360) - 6 * gap) / 7).toBeGreaterThanOrEqual(29);
  });

  it('drops the tabs under the leaderboard’s title where the two cannot share a line', () => {
    expect(decls('.cd-nd-board-head').get('flex-wrap')).toBe('wrap');
    expect(decls('.cd-nd-board-title').get('flex')).toBe('1 1 auto');
    expect(decls('.cd-nd-tabs').get('margin-left')).toBe('auto');
  });

  it('fades a shut Movies button to 0.45, and lights only one that can be pressed under the pointer', () => {
    expect(decls('.cd-nd-movies:disabled').get('opacity')).toBe('0.45');
    expect(mediaDecls('(hover: hover)', '.cd-nd-movies:hover:not(:disabled)').get('box-shadow')).toBe(
      'inset 0 0 0 1px var(--acc)',
    );
    expect(mediaDecls('(hover: hover)', '.cd-nd-movies:hover').size).toBe(0);
  });

  it('draws the question as How it works is drawn, smaller: over its scrim, a 20px card on the surface, its answers the page’s buttons', () => {
    expect(decls('.cd-nd-choose-scrim')).toEqual(decls('.cd-nd-rules-scrim'));
    expect(decls('.cd-nd-choose-scrim').get('z-index')).toBe('70');
    expect(decls('.cd-nd-choose-scrim').get('background')).toBe('var(--scrim)');
    const card = decls('.cd-nd-choose');
    expect(card.get('max-width')).toBe('360px');
    expect(card.get('border-radius')).toBe('20px');
    expect(card.get('background')).toBe('var(--s)');
    expect(card.get('box-shadow')).toBe('var(--pop)');
    expect(decls('.cd-nd-choose-title').get('font-family')).toBe("'Young Serif', serif");
    for (const b of ['.cd-nd-choose-no', '.cd-nd-choose-yes']) {
      expect(decls(b).get('height'), b).toBe('46px');
      expect(decls(b).get('border-radius'), b).toBe('13px');
    }
    // Open the map is the accent's, as Share result is; Not now the
    // surface with an edge, as Map this movie is.
    expect(decls('.cd-nd-choose-yes').get('background')).toBe(decls('.cd-nd-share').get('background'));
    expect(decls('.cd-nd-choose-yes').get('color')).toBe('var(--accInk)');
    expect(decls('.cd-nd-choose-no').get('box-shadow')).toBe(decls('.cd-nd-mapit').get('box-shadow'));
    // Side by side, stacking with Open the map on top where they cannot.
    expect(decls('.cd-nd-choose-row').get('flex-wrap')).toBe('wrap-reverse');
    expect(css).toMatch(/\.cd-nd-choose :is\(button, a\):focus-visible \{\s*outline: 2px solid var\(--acc\)/);
  });

  it('moves nothing with stillness asked for', () => {
    const still = stillRules();
    for (const c of [
      '.cd-nd-card-in',
      '.cd-nd-row',
      '.cd-nd-ph',
      '.cd-nd-who',
      '.cd-nd-facewrap',
      '.cd-nd-peek',
      '.cd-nd-facts',
      '.cd-nd-play',
      '.cd-nd-choose-no',
      '.cd-nd-choose-yes',
    ]) {
      expect(still, c).toMatch(new RegExp(`${c.replace(/[.-]/g, (x) => `\\${x}`)}[,\\s][^}]*transition: none`));
    }
  });
});
