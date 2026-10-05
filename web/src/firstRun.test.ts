import { describe, expect, it } from 'vitest';
import {
  COLD_MAX,
  coldScreenCount,
  coldTopPad,
  tilesFrom,
} from './firstRun';

describe('coldTopPad', () => {
  it('is fixed on phones and landscape phones', () => {
    expect(coldTopPad(390, 844)).toBe(36);
    expect(coldTopPad(844, 390)).toBe(18);
  });

  it('follows the window’s height everywhere else, faster from 860 up', () => {
    expect(coldTopPad(1180, 820)).toBeCloseTo(820 * 0.06);
    expect(coldTopPad(1440, 900)).toBeCloseTo(900 * 0.09);
    expect(coldTopPad(820, 1180)).toBeCloseTo(1180 * 0.09);
    expect(coldTopPad(1024, 520)).toBeCloseTo(31.2);
    // And never more than 110. (The 28 floor is below anything but a
    // landscape phone, which has its own 18.)
    expect(coldTopPad(1440, 1600)).toBe(110);
  });
});

describe('coldScreenCount', () => {
  it('fills a desktop window and a portrait tablet', () => {
    expect(coldScreenCount(1440, 900)).toBe(COLD_MAX);
    expect(coldScreenCount(820, 1180)).toBe(COLD_MAX);
  });

  it('shows all eight in their one row on a landscape phone', () => {
    expect(coldScreenCount(844, 390)).toBe(COLD_MAX);
    expect(coldScreenCount(667, 375)).toBe(COLD_MAX);
  });

  it('shows whole rows, never a ragged last one', () => {
    // Columns as `.cd-tiles` lays them: two on a phone, all eight in one
    // row on a landscape phone, four everywhere else.
    for (const [vw, vh, columns] of [
      [1440, 500, 4],
      [1440, 600, 4],
      [1440, 700, 4],
      [1180, 820, 4],
      [1440, 900, 4],
      [820, 1180, 4],
      [390, 844, 2],
      [375, 667, 2],
      [844, 390, 8],
    ]) {
      const n = coldScreenCount(vw, vh);
      expect(n % columns, `${vw}x${vh}`).toBe(0);
    }
  });

  it('never offers more than there are', () => {
    expect(coldScreenCount(2560, 2000)).toBeLessThanOrEqual(COLD_MAX);
  });

  it('still offers something on a short phone', () => {
    expect(coldScreenCount(390, 640)).toBeGreaterThan(0);
  });

  // The window sizes this used to get wrong, with every class's numbers
  // written out again here: header row 64 / 52 / 66, the padding above
  // the headline, the headline and sub-line with 12 after each, the
  // grid's margin 28 (16 on a landscape phone), and its bottom padding
  // 48 (24). A tile is its 2:3 frame and a 9 + 16.2 + 2 + 14.4 caption
  // (9 + 14.4 + 2 + 13.2 on a landscape phone).
  it('asks for no more rows than the window can show', () => {
    const cases: [number, number, number, number, number, number, number, number][] = [
      // vw, vh, columns, gap, max width, header row, intro + margins, foot
      [1280, 720, 4, 18, 640, 66, 49.68 + 12 + 48 + 12 + 28, 48],
      [1280, 760, 4, 18, 640, 66, 49.68 + 12 + 48 + 12 + 28, 48],
      [1440, 900, 4, 18, 640, 66, 49.68 + 12 + 48 + 12 + 28, 48],
      [1180, 820, 4, 18, 640, 66, 49.68 + 12 + 48 + 12 + 28, 48],
      [820, 1180, 4, 18, 640, 66, 49.68 + 12 + 48 + 12 + 28, 48],
      [390, 844, 2, 14, 640, 64, 69.12 + 12 + 48 + 12 + 28, 48],
      [844, 390, 8, 12, 820, 52, 30.24 + 12 + 21.75 + 12 + 16, 24],
    ];
    for (const [vw, vh, columns, gap, maxW, header, intro, foot] of cases) {
      const n = coldScreenCount(vw, vh);
      const rows = n / columns;
      const innerW = Math.min(vw - 40, maxW);
      const tileW = (innerW - gap * (columns - 1)) / columns;
      const caption = columns === 8 ? 9 + 14.4 + 2 + 13.2 : 9 + 16.2 + 2 + 14.4;
      const tall = rows * (tileW * 1.5 + caption) + (rows - 1) * gap;
      const room = vh - header - coldTopPad(vw, vh) - intro - foot;
      expect(tall, `${vw}x${vh} asked for ${rows} rows`).toBeLessThanOrEqual(room);
    }
  });

  it('lands where the handoff’s screenshots put the grid', () => {
    // The grid starts at 297 at 1440×900, 322 at 820×1180 and 162 at
    // 844×390 in the handoff's renders; the stand-in comes to the same
    // place, so it asks for the same rows a measurement would.
    expect(coldScreenCount(1440, 900, 297)).toBe(coldScreenCount(1440, 900));
    expect(coldScreenCount(820, 1180, 322)).toBe(coldScreenCount(820, 1180));
    expect(coldScreenCount(844, 390, 162)).toBe(coldScreenCount(844, 390));
  });

  it('uses the measured grid top when it has one', () => {
    // The stand-in numbers are a second copy of the stylesheet. Given a
    // real measurement it uses that instead.
    expect(coldScreenCount(1440, 900, 300)).toBe(8);
    expect(coldScreenCount(1440, 900, 600)).toBe(4);
    expect(coldScreenCount(1440, 900, 300) % 4).toBe(0);
  });
});

describe('tilesFrom', () => {
  const hit = (id: string, title: string, year = 1999) => ({
    id,
    title,
    year,
    poster: `https://m.media-amazon.com/images/M/${id}._V1_SX300.jpg`,
  });

  it('keeps what the API sent, as tiles', () => {
    const got = tilesFrom(
      [
        hit('tt0133093', 'The Matrix'),
        hit('tt0111161', 'Shawshank', 1994),
        hit('tt0468569', 'The Dark Knight', 2008),
      ],
      3,
    );
    expect(got.map((f) => f.id)).toEqual(['tt0133093', 'tt0111161', 'tt0468569']);
    expect(got[0].year).toBe(1999);
  });

  it('drops a film with nothing to show', () => {
    const got = tilesFrom(
      [
        { id: 'tt1', title: 'No Poster', year: 1999, poster: '' },
        { id: 'tt2', title: '', year: 1999, poster: 'https://x/p.jpg' },
        { id: 'tt3', title: 'No Year', year: 0, poster: 'https://x/p.jpg' },
        hit('tt0133093', 'The Matrix'),
      ],
      1,
    );
    expect(got.map((f) => f.id)).toEqual(['tt0133093']);
  });

  it('keeps a long title and lets the ellipsis deal with it', () => {
    // One long name must not empty the screen: the catalog picks these
    // now, and plenty of real films have long names.
    const long = 'Indiana Jones and the Temple of Doom';
    expect(tilesFrom([hit('tt1', long)], 1).map((f) => f.title)).toEqual([long]);
  });

  it('never shows the same film twice', () => {
    const got = tilesFrom([hit('tt1', 'One'), hit('tt1', 'One'), hit('tt2', 'Two')], 2);
    expect(got.map((f) => f.id)).toEqual(['tt1', 'tt2']);
  });

  it('shows what it has rather than nothing', () => {
    expect(tilesFrom([hit('tt1', 'One')], 8)).toHaveLength(1);
  });
});
