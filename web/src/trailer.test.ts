import { describe, expect, it } from 'vitest';
import css from './grid.css?raw';
import { screenOf, type ScreenClass } from './screen';
import {
  MIN_VIDEO_H,
  PLAYER,
  TRAILER_CLOSE_MS,
  TRAILER_GONE_MS,
  TRAILER_OPEN_MS,
  YT_LISTENING,
  catchUp,
  embedSrc,
  escapeCloses,
  playerLink,
  playerVars,
  saysReady,
  startPlay,
  stopPlay,
  videoHeight,
  videoOrigin,
  videoWidth,
  watchUrl,
  wellHeight,
  ytCommand,
  type Play,
} from './trailer';

/** The declarations of every rule with exactly this selector, @media
 *  ones included unless `media` is false, later ones winning. Enough of
 *  a parser for a sheet with no nesting beyond @media. */
function decls(selector: string, media = true): Map<string, string> {
  let text = css.replace(/\/\*[\s\S]*?\*\//g, '');
  if (!media) text = text.replace(/@media[^{]*\{(?:[^{}]*\{[^{}]*\})*[^{}]*\}/g, '');
  const out = new Map<string, string>();
  for (const m of text.matchAll(/([^{}]+)\{([^{}]*)\}/g)) {
    if (!m[1].split(',').map((s) => s.trim()).includes(selector)) continue;
    for (const d of m[2].split(';')) {
      const at = d.indexOf(':');
      if (at > 0) out.set(d.slice(0, at).trim(), d.slice(at + 1).trim());
    }
  }
  return out;
}

const px = (v: string | undefined) => Number(v?.match(/^(-?[\d.]+)px$/)?.[1] ?? NaN);

describe('the video’s size', () => {
  it('is 16:9', () => {
    expect(videoHeight(400)).toBe(225);
    expect(videoHeight(430)).toBe(241.875);
  });

  it('is never shorter than YouTube allows', () => {
    expect(MIN_VIDEO_H).toBe(200);
    expect(videoHeight(344)).toBe(200);
    expect(videoHeight(333.5)).toBe(200);
    expect(videoHeight(0)).toBe(200);
  });

  it('bleeds across the padding on both sides of the row', () => {
    // The preview is 360 wide with 16 of padding, so its row is 328.
    expect(videoWidth('preview', 328)).toBe(360);
    expect(videoWidth('panel', 386)).toBe(430);
  });
});

describe('the panel’s video at every screen class', () => {
  /** How wide the sheet is on each screen, as the stylesheet sets it. */
  function sheetWidth(cls: ScreenClass, w: number): number {
    const sel = cls === 'desktop' ? '.cd-sheet' : `.cd-sheet-${cls}`;
    const d = decls(sel);
    if (cls === 'phone') {
      // Held in from both sides rather than given a width.
      expect(d.get('width')).toBe('auto');
      return w - px(d.get('left')) - px(d.get('right'));
    }
    const width = d.get('width')!;
    const min = width.match(/^min\((\d+)px, (\d+)vw\)$/);
    return min ? Math.min(Number(min[1]), (w * Number(min[2])) / 100) : px(width);
  }

  it('sits in the same padding at every class, which the row’s corner and the bleed are measured from', () => {
    expect(decls('.cd-sheet-body').get('padding')).toBe(`0 ${PLAYER.panel.pad}px 24px`);
    for (const cls of ['phone', 'short', 'tablet', 'desktop']) {
      expect(decls(`.cd-sheet-${cls} .cd-sheet-body`).has('padding'), cls).toBe(false);
    }
    expect(decls('.cd-trailer-controls').get('margin')).toBe(`0 -${PLAYER.panel.pad}px`);
    expect(decls('.cd-trailer-well').get('margin')).toBe(`0 -${PLAYER.panel.pad}px`);
    expect(decls('.cd-trailer-preview .cd-trailer-controls').get('margin')).toBe(`0 -${PLAYER.preview.pad}px`);
    expect(decls('.cd-trailer-preview .cd-trailer-well').get('margin')).toBe(`0 -${PLAYER.preview.pad}px`);
  });

  it('runs the full width of the sheet and is at least 200 tall', () => {
    const want: Record<string, [ScreenClass, number, number]> = {
      '360x640': ['phone', 344, 200],
      '390x844': ['phone', 374, 210.375],
      '667x375': ['short', 333.5, 200],
      '924x540': ['tablet', 400, 225],
      '820x1180': ['tablet', 400, 225],
      '1180x820': ['desktop', 430, 241.875],
      '1440x900': ['desktop', 430, 241.875],
    };
    for (const [size, [cls, W, VH]] of Object.entries(want)) {
      const [w, h] = size.split('x').map(Number);
      const s = screenOf(w, h);
      expect(s.cls, size).toBe(cls);
      const sheet = sheetWidth(s.cls, w);
      // The row is the sheet less its padding; the video is the row and
      // the padding on either side, so the sheet's width exactly.
      const row = sheet - 2 * PLAYER.panel.pad;
      expect(videoWidth('panel', row), size).toBe(W);
      expect(videoWidth('panel', row), size).toBe(sheet);
      expect(videoHeight(W), size).toBe(VH);
    }
  });

  it('never goes under the close button, which the body is pulled up beneath on a phone', () => {
    // The button's box, and the 44px its hit area reaches to.
    const close = decls('.cd-sheet-close');
    const hit = -px(decls('.cd-sheet-close::before').get('inset'));
    const bottom = px(close.get('top')) + px(close.get('height')) + hit;
    const fromRight = px(close.get('right')) + px(close.get('width')) + hit;
    expect(bottom).toBe(56);
    expect(fromRight).toBe(56);
    const bodyTop = (cls: string) => {
      const wash = decls(`.cd-sheet-${cls} .cd-sheet-wash`).get('height') ?? decls('.cd-sheet-wash').get('height');
      const pull = decls(`.cd-sheet-${cls} .cd-sheet-body`).get('margin-top') ?? decls('.cd-sheet-body').get('margin-top');
      return px(wash) + px(pull);
    };
    // A desktop's and a tablet's body start below the button.
    expect(bodyTop('desktop')).toBeGreaterThanOrEqual(bottom);
    expect(bodyTop('tablet')).toBeGreaterThanOrEqual(bottom);
    // A phone's and a landscape phone's are cut away under it while a
    // trailer is open, exactly as far as it reaches.
    for (const cls of ['phone', 'short']) {
      const clip = decls(`.cd-sheet-${cls} .cd-sheet-playing`).get('clip-path');
      const notch = bottom - bodyTop(cls);
      expect(notch, cls).toBeGreaterThan(0);
      expect(clip, cls).toBe(
        `polygon(0 0, calc(100% - ${fromRight}px) 0, calc(100% - ${fromRight}px) ${notch}px, 100% ${notch}px, 100% 100%, 0 100%)`,
      );
    }
  });
});

describe('the well', () => {
  it('grows to the gap and the video', () => {
    expect(wellHeight('panel', 241.875)).toBe(245.875);
    expect(wellHeight('preview', 202.5)).toBe(208.5);
  });
});

describe('the player’s timings', () => {
  it('are the ones the stylesheet opens and closes it with', () => {
    expect(TRAILER_OPEN_MS).toBe(520);
    expect(TRAILER_CLOSE_MS).toBe(400);
    expect(decls('.cd-trailer-open .cd-trailer-well', false).get('transition')).toBe(
      `height ${TRAILER_OPEN_MS}ms var(--ease-glide)`,
    );
    expect(decls('.cd-trailer-well', false).get('transition')).toBe(`height ${TRAILER_CLOSE_MS}ms var(--ease-close)`);
    // Written over two lines in the stylesheet.
    const flat = (v: string | undefined) => v?.replace(/\s+/g, ' ');
    expect(flat(decls('.cd-trailer-open .cd-trailer-video', false).get('transition'))).toBe(
      `transform ${TRAILER_OPEN_MS}ms var(--ease-glide), opacity 260ms ease`,
    );
    expect(flat(decls('.cd-trailer-video', false).get('transition'))).toBe(
      `transform ${TRAILER_CLOSE_MS}ms var(--ease-close), opacity 220ms ease 160ms`,
    );
  });

  it('keep a closing player in the tree until its close has played', () => {
    expect(TRAILER_CLOSE_MS).toBeLessThan(TRAILER_GONE_MS);
  });

  it('are all undone for a reader who has asked for nothing to move', () => {
    for (const sel of ['.cd-trailer-well', '.cd-trailer-open .cd-trailer-well', '.cd-trailer-video', '.cd-trailer-open .cd-trailer-video']) {
      expect(decls(sel).get('transition'), sel).toBe('none');
    }
  });
});

describe('the video’s origin', () => {
  it('is the button’s top-left corner: the padding in from the edge, at the top', () => {
    expect(videoOrigin('panel')).toBe('22px 0');
    expect(videoOrigin('preview')).toBe('16px 0');
  });
});

describe('playerVars', () => {
  it('hands the stylesheet the size, the gap, the well and the origin', () => {
    const play: Play = {
      where: 'panel',
      id: 'tt0133093',
      muted: false,
      open: true,
      closing: false,
      W: 430,
      VH: 241.875,
      synLines: null,
    };
    expect(playerVars(play)).toEqual({
      '--trailer-w': '430px',
      '--trailer-vh': '241.875px',
      '--trailer-gap': '4px',
      '--trailer-well': '245.875px',
      '--trailer-origin': '22px 0',
    });
  });
});

describe('startPlay', () => {
  it('mounts closed, so its opening has somewhere to run from', () => {
    expect(startPlay(null, 'panel', 'tt1', false, 430, false)).toEqual({
      where: 'panel',
      id: 'tt1',
      muted: false,
      open: false,
      closing: false,
      W: 430,
      VH: 241.875,
      synLines: null,
    });
  });

  it('opens at once for a reader who has asked for nothing to move', () => {
    expect(startPlay(null, 'panel', 'tt1', true, 430, true)?.open).toBe(true);
  });

  it('leaves one already open or opening as it is', () => {
    const opening = startPlay(null, 'panel', 'tt1', true, 430, false)!;
    expect(startPlay(opening, 'panel', 'tt1', false, 430, false)).toBeNull();
    expect(startPlay({ ...opening, open: true }, 'panel', 'tt1', false, 430, false)).toBeNull();
  });

  it('takes back one that is still closing, open again at once', () => {
    const closing = stopPlay({ ...startPlay(null, 'panel', 'tt1', true, 430, false)!, open: true })!;
    const back = startPlay(closing, 'panel', 'tt1', false, 430, false)!;
    expect(back.open).toBe(true);
    expect(back.closing).toBe(false);
    expect(back.muted).toBe(false);
  });

  it('replaces any other, so only one plays at a time', () => {
    const preview = startPlay(null, 'preview', 'tt1', true, 360, false)!;
    const panel = startPlay({ ...preview, open: true }, 'panel', 'tt1', false, 430, false)!;
    expect(panel.where).toBe('panel');
    expect(panel.open).toBe(false);
    expect(startPlay(panel, 'panel', 'tt2', false, 430, false)?.id).toBe('tt2');
  });
});

describe('stopPlay', () => {
  it('starts the close, keeping the player to play it', () => {
    const open = { ...startPlay(null, 'panel', 'tt1', false, 430, false)!, open: true };
    expect(stopPlay(open)).toEqual({ ...open, open: false, closing: true });
  });

  it('does nothing with nothing open, or one already closing', () => {
    expect(stopPlay(null)).toBeNull();
    const open = { ...startPlay(null, 'panel', 'tt1', false, 430, false)!, open: true };
    expect(stopPlay(stopPlay(open))).toBeNull();
  });
});

describe('escapeCloses', () => {
  const open = { ...startPlay(null, 'panel', 'tt1', false, 430, false)!, open: true };

  it('closes the layer’s own open trailer first', () => {
    expect(escapeCloses(open, 'panel', 'tt1')).toBe('trailer');
  });

  it('closes the layer once the trailer is closing or gone', () => {
    expect(escapeCloses(stopPlay(open), 'panel', 'tt1')).toBe('layer');
    expect(escapeCloses(null, 'panel', 'tt1')).toBe('layer');
  });

  it('leaves a trailer that belongs somewhere else alone', () => {
    expect(escapeCloses(open, 'panel', 'tt2')).toBe('layer');
    expect(escapeCloses(open, 'preview', 'tt1')).toBe('layer');
  });
});

describe('the player’s address', () => {
  it('asks the privacy-enhanced host for exactly these settings', () => {
    expect(embedSrc('vKQi3bBA1y8', false, 'https://cinedikt.com')).toBe(
      'https://www.youtube-nocookie.com/embed/vKQi3bBA1y8?autoplay=1&mute=0&playsinline=1&rel=0&iv_load_policy=3&enablejsapi=1&origin=https%3A%2F%2Fcinedikt.com',
    );
  });

  it('starts muted when asked', () => {
    expect(embedSrc('vKQi3bBA1y8', true, 'http://localhost:5173')).toContain('&mute=1&');
  });

  it('leaves the origin out when the page has none to give', () => {
    expect(embedSrc('vKQi3bBA1y8', false, 'null')).not.toContain('origin=');
    expect(embedSrc('vKQi3bBA1y8', false, '')).not.toContain('origin=');
  });

  it('links to the trailer on YouTube itself', () => {
    expect(watchUrl('vKQi3bBA1y8')).toBe('https://www.youtube.com/watch?v=vKQi3bBA1y8');
  });
});

describe('ytCommand', () => {
  it('is the IFrame API’s command message', () => {
    expect(ytCommand('pauseVideo')).toBe('{"event":"command","func":"pauseVideo","args":[]}');
    expect(JSON.parse(ytCommand('unMute'))).toEqual({ event: 'command', func: 'unMute', args: [] });
  });

  it('asks the player to start talking with the widget handshake', () => {
    expect(JSON.parse(YT_LISTENING)).toEqual({ event: 'listening', id: 1, channel: 'widget' });
  });
});

describe('saysReady', () => {
  it('hears the player say it is ready', () => {
    expect(saysReady('{"event":"onReady","id":1,"channel":"widget"}')).toBe(true);
  });

  it('ignores anything else it or another frame says', () => {
    expect(saysReady('{"event":"infoDelivery","info":{"muted":true},"id":1,"channel":"widget"}')).toBe(false);
    expect(saysReady('{"event":"initialDelivery","id":1}')).toBe(false);
    expect(saysReady('not json')).toBe(false);
    expect(saysReady('null')).toBe(false);
    expect(saysReady({ event: 'onReady' })).toBe(false);
    expect(saysReady(undefined)).toBe(false);
  });
});

describe('catchUp', () => {
  const open = { ...startPlay(null, 'panel', 'tt1', false, 430, false)!, open: true };

  it('brings a ready player into line with its controls', () => {
    expect(catchUp(open)).toEqual(['unMute', 'playVideo']);
    expect(catchUp({ ...open, muted: true })).toEqual(['mute']);
    expect(catchUp(stopPlay(open))).toEqual(['pauseVideo']);
    expect(catchUp(null)).toEqual([]);
  });
});

describe('playerLink', () => {
  const READY = '{"event":"onReady","id":1,"channel":"widget"}';
  const said = (posted: string[]) => posted.map((m) => JSON.parse(m).func);

  it('holds a sound toggle made while the player loads, then sends the toggled state once it is ready', () => {
    const posted: string[] = [];
    const link = playerLink((m) => posted.push(m));
    // Started muted by a rest on the button, then turned up before the
    // player could hear it: what sound() does.
    let play: Play = { ...startPlay(null, 'panel', 'tt1', true, 430, false)!, open: true };
    link.reset();
    link.send('unMute');
    link.send('playVideo');
    play = { ...play, muted: false };
    expect(posted).toEqual([]);
    // Other news from the player is not it saying it is ready.
    link.heard('{"event":"initialDelivery","id":1}', play);
    expect(posted).toEqual([]);
    link.heard(READY, play);
    expect(said(posted)).toEqual(['unMute', 'playVideo']);
  });

  it('turns a player down that was turned down while it loaded', () => {
    const posted: string[] = [];
    const link = playerLink((m) => posted.push(m));
    link.send('mute');
    link.heard(READY, { ...startPlay(null, 'panel', 'tt1', true, 430, false)!, open: true });
    expect(said(posted)).toEqual(['mute']);
  });

  it('pauses a player that was closed before it was ready', () => {
    const posted: string[] = [];
    const link = playerLink((m) => posted.push(m));
    link.send('pauseVideo');
    link.heard(READY, stopPlay({ ...startPlay(null, 'panel', 'tt1', false, 430, false)!, open: true }));
    expect(said(posted)).toEqual(['pauseVideo']);
  });

  it('sends straight through once ready, and holds again for the next frame', () => {
    const posted: string[] = [];
    const link = playerLink((m) => posted.push(m));
    const play = { ...startPlay(null, 'panel', 'tt1', true, 430, false)!, open: true };
    link.heard(READY, play);
    posted.length = 0;
    link.send('pauseVideo');
    expect(said(posted)).toEqual(['pauseVideo']);
    // Said twice, the state is not sent again.
    link.heard(READY, play);
    expect(said(posted)).toEqual(['pauseVideo']);
    link.reset();
    link.send('mute');
    expect(said(posted)).toEqual(['pauseVideo']);
  });
});
