import { createElement } from 'react';
import { renderToStaticMarkup } from 'react-dom/server';
import { afterEach, describe, expect, it, vi } from 'vitest';
import { readScreen, sameScreen, useLiveScreen, watchScreen } from './dailyScreen';

// The Daily's size, read live: what it reads off a window, and which
// changes it listens for. The window here is a stand-in that keeps its
// listeners, so no DOM is needed to see them come and go.

/** An event target that remembers what listens to it. */
function target() {
  const heard = new Map<string, Set<() => void>>();
  return {
    heard,
    addEventListener: (type: string, fn: () => void) => {
      if (!heard.has(type)) heard.set(type, new Set());
      heard.get(type)!.add(fn);
    },
    removeEventListener: (type: string, fn: () => void) => heard.get(type)?.delete(fn),
    fire: (type: string) => heard.get(type)?.forEach((fn) => fn()),
  };
}

afterEach(() => {
  vi.unstubAllGlobals();
});

describe('the screen, read off the window', () => {
  it('is a phone under 640px, a landscape phone under 520px tall and wider, with the visual viewport’s height', () => {
    expect(readScreen({ innerWidth: 375, innerHeight: 667, visualViewport: { height: 420 } as VisualViewport })).toEqual({
      phone: true,
      land: false,
      viewH: 420,
    });
    expect(readScreen({ innerWidth: 844, innerHeight: 390, visualViewport: null })).toEqual({
      phone: false,
      land: true,
      viewH: 390,
    });
  });

  it('is a desktop where there is no window to read, as on the server', () => {
    expect(readScreen(undefined)).toEqual({ phone: false, land: false, viewH: 800 });
  });

  it('redraws nothing for a resize that changes nothing the Daily reads', () => {
    const a = { phone: true, land: false, viewH: 600 };
    expect(sameScreen(a, { ...a })).toBe(true);
    expect(sameScreen(a, { ...a, viewH: 420 })).toBe(false);
    expect(sameScreen(a, { ...a, phone: false })).toBe(false);
    expect(sameScreen(a, { ...a, land: true })).toBe(false);
  });
});

describe('the screen, watched', () => {
  it('reads again on a resize, a turn of the phone, and the visual viewport resizing', () => {
    const vv = target();
    const win = { ...target(), visualViewport: vv };
    const read = vi.fn();
    const stop = watchScreen(win, read);
    win.fire('resize');
    win.fire('orientationchange');
    vv.fire('resize');
    expect(read).toHaveBeenCalledTimes(3);
    stop();
    win.fire('resize');
    win.fire('orientationchange');
    vv.fire('resize');
    expect(read).toHaveBeenCalledTimes(3);
    expect([...win.heard.values()].every((s) => s.size === 0)).toBe(true);
    expect([...vv.heard.values()].every((s) => s.size === 0)).toBe(true);
  });

  it('still watches the window where there is no visual viewport', () => {
    const win = { ...target(), visualViewport: null };
    const read = vi.fn();
    watchScreen(win, read);
    win.fire('resize');
    expect(read).toHaveBeenCalledTimes(1);
  });

  it('starts from the window as it is when the page is drawn, never from a size kept from before', () => {
    function Probe() {
      const s = useLiveScreen();
      return createElement('i', null, `${s.phone}/${s.land}/${s.viewH}`);
    }
    vi.stubGlobal('window', { innerWidth: 375, innerHeight: 540 });
    expect(renderToStaticMarkup(createElement(Probe))).toBe('<i>true/false/540</i>');
    vi.stubGlobal('window', { innerWidth: 844, innerHeight: 390 });
    expect(renderToStaticMarkup(createElement(Probe))).toBe('<i>false/true/390</i>');
  });
});
