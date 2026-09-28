import { createElement } from 'react';
import { renderToStaticMarkup } from 'react-dom/server';
import { describe, expect, it, vi } from 'vitest';
import css from './grid.css?raw';
import { bigPhoto, faceCardController, type FaceCard } from './faceCard';
import matrix from './fixtures/matrix-grid.json';
import type { GridPayload, GridPerson } from './grid';
import { roleLine } from './GridSheet';
import { PersonCardView } from './PersonCard';
import { personColour } from './personColour';
import type { PreviewClock } from './preview';

const KEANU = 'https://image.tmdb.org/t/p/w185/keanu.jpg';
const LANA = 'https://image.tmdb.org/t/p/w185/lana.jpg';
const PEOPLE = (matrix as GridPayload).people;
const who = (name: string) => PEOPLE.find((p) => p.name === name)!;

/** The card as it is drawn once it is in. */
function card(person: GridPerson, photo: string, at: Partial<FaceCard> = {}, shown = true): string {
  return renderToStaticMarkup(
    createElement(PersonCardView, {
      card: { id: person.id, from: 'chip', x: 120, y: 142, down: true, ...at },
      person,
      photo,
      anchorTitle: 'The Matrix',
      theme: 'dark',
      shown,
    }),
  );
}

describe('a bigger photo of a person', () => {
  it('shows their name, the panel’s role line and the w342 photo', () => {
    const keanu = who('Keanu Reeves');
    const html = card(keanu, KEANU);
    expect(roleLine(keanu, 'The Matrix')).toBe('Neo in The Matrix');
    expect(html).toBe(
      `<div class="cd-person-card cd-person-card-in" style="left:120px;top:142px;--tone:${personColour(keanu.id, 'dark')}" aria-hidden="true">` +
        '<span class="cd-person-card-frame"><span class="cd-person-card-photo" style="background-image:url(&quot;https://image.tmdb.org/t/p/w342/keanu.jpg&quot;)"></span></span>' +
        '<span class="cd-person-card-text"><span class="cd-person-card-name">Keanu Reeves</span>' +
        '<span class="cd-person-card-role">Neo in The Matrix</span></span></div>',
    );
  });

  it('is the same portrait rectangle for a director, whose role line says so', () => {
    const lana = who('Lana Wachowski');
    const html = card(lana, LANA);
    expect(html).toContain('<span class="cd-person-card-role">Directed The Matrix</span>');
    expect(html).toContain('<span class="cd-person-card-frame"><span class="cd-person-card-photo" style=');
    expect(html).not.toMatch(/square|director/);
  });

  it('stands above what asked for it, held by its bottom edge and growing from it', () => {
    const html = card(who('Keanu Reeves'), KEANU, { from: 'peek', x: 541, y: 476, down: false });
    expect(html).toMatch(/^<div class="cd-person-card cd-person-card-in cd-person-card-up" style="left:541px;bottom:476px;--tone:/);
  });

  it('comes in from scaled down and transparent', () => {
    expect(card(who('Keanu Reeves'), KEANU, {}, false)).toMatch(/^<div class="cd-person-card" style=/);
  });

  it('is not asked for before its w342 photo has loaded', () => {
    vi.useFakeTimers();
    try {
      const clock: PreviewClock = {
        now: () => Date.now(),
        after: (ms, run) => setTimeout(run, ms),
        cancel: (t) => clearTimeout(t),
      };
      let arrive: (ok: boolean) => void = () => {};
      const asked: string[] = [];
      const opened: FaceCard[] = [];
      const cards = faceCardController(
        {
          hovers: () => true,
          photo: () => KEANU,
          place: () => ({ x: 120, y: 142, down: true }),
          open: (c) => opened.push(c),
          close: () => {},
        },
        clock,
        (url, done) => {
          asked.push(url);
          arrive = done;
          return () => {};
        },
      );
      cards.onFace(who('Keanu Reeves').id, {} as HTMLElement, 'chip');
      vi.advanceTimersByTime(2000);
      expect(opened).toEqual([]);
      arrive(true);
      expect(opened).toHaveLength(1);
      // The photo it waited for is the one the card draws.
      expect(asked).toEqual([bigPhoto(KEANU)]);
      expect(card(who('Keanu Reeves'), KEANU)).toContain(`url(&quot;${asked[0]}&quot;)`);
    } finally {
      vi.useRealTimers();
    }
  });
});

/** The declarations of every rule with exactly this selector: outside
 *  any @media block, or with `media` only inside blocks with exactly
 *  that query. A later one wins. */
function decls(selector: string, media?: string): Map<string, string> {
  const text = css.replace(/\/\*[\s\S]*?\*\//g, '');
  const all = mediaBlocks(text);
  const scope =
    media == null
      ? all.reduceRight((t, b) => t.slice(0, b.from) + t.slice(b.to), text)
      : all.filter((b) => b.query === media).map((b) => b.body).join('\n');
  const out = new Map<string, string>();
  for (const m of scope.matchAll(/([^{}]+)\{([^{}]*)\}/g)) {
    if (!m[1].split(',').map((x) => x.trim()).includes(selector)) continue;
    for (const d of m[2].split(';')) {
      const at = d.indexOf(':');
      if (at > 0) out.set(d.slice(0, at).trim(), d.slice(at + 1).trim().replace(/\s+/g, ' '));
    }
  }
  return out;
}

/** Every @media block: its query, its body and where it sits. */
function mediaBlocks(text: string): { query: string; body: string; from: number; to: number }[] {
  const out: { query: string; body: string; from: number; to: number }[] = [];
  for (let at = text.indexOf('@media'); at >= 0; at = text.indexOf('@media', at + 1)) {
    const open = text.indexOf('{', at);
    let depth = 0;
    for (let i = open; i < text.length; i++) {
      if (text[i] === '{') depth++;
      else if (text[i] === '}' && --depth === 0) {
        out.push({ query: text.slice(at + 7, open).trim(), body: text.slice(open + 1, i), from: at, to: i + 1 });
        at = i;
        break;
      }
    }
  }
  return out;
}

describe('the card as the stylesheet draws it', () => {
  it('is the handoff’s: 148px, fixed over the sheets and under the toast, never taking the pointer', () => {
    const d = decls('.cd-person-card');
    expect(d.get('position')).toBe('fixed');
    expect(d.get('z-index')).toBe('45');
    expect(d.get('width')).toBe('148px');
    expect(d.get('box-sizing')).toBe('border-box');
    expect(d.get('gap')).toBe('8px');
    expect(d.get('padding')).toBe('6px 6px 10px');
    expect(d.get('border-radius')).toBe('16px');
    expect(d.get('background')).toBe('var(--s)');
    expect(d.get('box-shadow')).toBe('inset 0 0 0 1px var(--ln2), var(--pop)');
    expect(d.get('pointer-events')).toBe('none');
    expect(d.get('opacity')).toBe('0');
    expect(d.get('transform')).toBe('scale(0.96)');
    expect(d.get('transform-origin')).toBe('50% 0');
    expect(decls('.cd-person-card-up').get('transform-origin')).toBe('50% 100%');
    expect(decls('.cd-person-card-in').get('opacity')).toBe('1');
    expect(decls('.cd-person-card-in').get('transform')).toBe('none');
  });

  it('frames the photo in the small faces’ ring, a 2:3 portrait on the surface colour', () => {
    const frame = decls('.cd-person-card-frame');
    expect(frame.get('padding')).toBe('4px');
    expect(frame.get('border-radius')).toBe('12px');
    expect(frame.get('box-shadow')).toBe('inset 0 0 0 2px var(--tone)');
    const photo = decls('.cd-person-card-photo');
    expect(photo.get('aspect-ratio')).toBe('2 / 3');
    expect(photo.get('border-radius')).toBe('8px');
    expect(photo.get('background')).toBe('var(--c2) 50% 22% / cover no-repeat');
    expect(decls('.cd-person-card-name').get('font-size')).toBe('14px');
    expect(decls('.cd-person-card-name').get('font-weight')).toBe('600');
    expect(decls('.cd-person-card-role').get('font-size')).toBe('12.5px');
    expect(decls('.cd-person-card-role').get('color')).toBe('var(--t3)');
  });

  it('comes in without moving for a reader who asked for nothing to move', () => {
    const still = decls('.cd-person-card', '(prefers-reduced-motion: reduce)');
    expect(still.get('transition')).toBe('none');
    expect(still.get('transform')).toBe('none');
  });

  it('keeps its edge and its ring as borders under forced colours', () => {
    expect(decls('.cd-person-card', '(forced-colors: active)').get('border')).toBe('1px solid CanvasText');
    const frame = decls('.cd-person-card-frame', '(forced-colors: active)');
    expect(frame.get('box-shadow')).toBe('none');
    expect(frame.get('border')).toBe('2px solid CanvasText');
  });
});
