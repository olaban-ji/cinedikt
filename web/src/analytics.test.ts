import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';

const init = vi.fn();
const captureEvent = vi.fn();

const mpInit = vi.fn();
const mpTrack = vi.fn();

vi.mock('mixpanel-browser', () => ({
  default: { init: mpInit, track: mpTrack },
}));

vi.mock('posthog-js', () => ({
  default: { init, capture: captureEvent },
}));

/** A page whose head holds a cinedikt-analytics tag with this content, as
 *  getAttribute hands it back (already unescaped), or no tag at all. */
function stubPage(content?: string): void {
  vi.stubGlobal('document', {
    querySelector: (selector: string) =>
      content !== undefined && selector === 'meta[name="cinedikt-analytics"]'
        ? { getAttribute: (name: string) => (name === 'content' ? content : null) }
        : null,
  });
}

/** Lets a dynamic import, and anything waiting on it, settle. */
async function settle(): Promise<void> {
  for (let i = 0; i < 5; i++) await new Promise((resolve) => setTimeout(resolve, 0));
}

describe('analytics', () => {
  let fetchMock: ReturnType<typeof vi.fn>;

  beforeEach(() => {
    vi.resetModules();
    vi.stubGlobal('window', { location: { hostname: 'map.example' } });
    stubPage();
    // Nothing here may reach the network, and each test checks that
    // nothing did.
    fetchMock = vi.fn();
    vi.stubGlobal('fetch', fetchMock);
    init.mockClear();
    captureEvent.mockClear();
    mpInit.mockClear();
    mpTrack.mockClear();
  });

  afterEach(() => {
    vi.unstubAllGlobals();
  });

  it('flushes captures made before the SDK loaded', async () => {
    stubPage(JSON.stringify({ token: 'phc_test', host: 'https://example.posthog.com' }));
    const { initAnalytics, capture } = await import('./analytics');

    capture('movie_selected', { movie_id: 603, title: 'The Matrix' });
    expect(captureEvent).not.toHaveBeenCalled();

    initAnalytics();
    await vi.waitFor(() => expect(init).toHaveBeenCalledTimes(1));

    expect(init).toHaveBeenCalledWith('phc_test', {
      api_host: 'https://example.posthog.com',
      defaults: '2026-05-30',
    });
    expect(captureEvent).toHaveBeenCalledWith('movie_selected', {
      movie_id: 603,
      title: 'The Matrix',
    });
    expect(fetchMock).not.toHaveBeenCalled();
  });

  it('starts neither tracker, and asks nothing of the network, when the page has no tag', async () => {
    const { initAnalytics, capture } = await import('./analytics');

    capture('movie_selected', { movie_id: 1 });
    initAnalytics();
    capture('movie_selected', { movie_id: 2 });
    await settle();

    expect(fetchMock).not.toHaveBeenCalled();
    expect(init).not.toHaveBeenCalled();
    expect(mpInit).not.toHaveBeenCalled();
    expect(captureEvent).not.toHaveBeenCalled();
    expect(mpTrack).not.toHaveBeenCalled();
  });

  it('loads PostHog from the tag the server wrote into the page', async () => {
    stubPage(JSON.stringify({ token: 'phc_from_page', host: 'https://page.posthog.com' }));
    const { initAnalytics, capture } = await import('./analytics');

    initAnalytics();
    capture('movie_selected', { movie_id: 7 });
    await vi.waitFor(() => expect(init).toHaveBeenCalledTimes(1));

    expect(init).toHaveBeenCalledWith('phc_from_page', {
      api_host: 'https://page.posthog.com',
      defaults: '2026-05-30',
    });
    expect(captureEvent).toHaveBeenCalledWith('movie_selected', { movie_id: 7 });
    expect(fetchMock).not.toHaveBeenCalled();
  });

  it('loads Mixpanel from the tag and sends it the same events', async () => {
    stubPage(
      JSON.stringify({
        token: 'phc_from_page',
        host: 'https://page.posthog.com',
        mixpanel_token: 'mp_from_page',
      }),
    );
    const { initAnalytics, capture } = await import('./analytics');

    capture('movie_selected', { movie_id: 7 });
    initAnalytics();
    await vi.waitFor(() => expect(mpInit).toHaveBeenCalledTimes(1));

    expect(mpInit).toHaveBeenCalledWith('mp_from_page', {
      autocapture: true,
      record_sessions_percent: 100,
    });
    expect(mpTrack).toHaveBeenCalledWith('movie_selected', { movie_id: 7 });
    await vi.waitFor(() => expect(captureEvent).toHaveBeenCalledWith('movie_selected', { movie_id: 7 }));
    expect(fetchMock).not.toHaveBeenCalled();
  });

  it('uses PostHog’s default host when the tag names none', async () => {
    stubPage(JSON.stringify({ token: 'phc_from_page' }));
    const { initAnalytics } = await import('./analytics');

    initAnalytics();
    await vi.waitFor(() => expect(init).toHaveBeenCalledTimes(1));

    expect(init).toHaveBeenCalledWith(
      'phc_from_page',
      expect.objectContaining({ api_host: 'https://us.i.posthog.com' }),
    );
    expect(fetchMock).not.toHaveBeenCalled();
  });

  it('uses PostHog’s default host when the tag’s host is not a string', async () => {
    stubPage('{"token":"phc_test","host":["x"]}');
    const { initAnalytics } = await import('./analytics');

    initAnalytics();
    await vi.waitFor(() => expect(init).toHaveBeenCalledTimes(1));

    expect(init).toHaveBeenCalledWith(
      'phc_test',
      expect.objectContaining({ api_host: 'https://us.i.posthog.com' }),
    );
    expect(fetchMock).not.toHaveBeenCalled();
  });

  it('starts nothing, and asks nothing of the network, from a malformed tag', async () => {
    for (const content of [
      '',
      '{',
      'not json',
      'null',
      '"phc_test"',
      '42',
      '[]',
      '{"token":42,"host":["x"],"mixpanel_token":{}}',
    ]) {
      vi.resetModules();
      stubPage(content);
      const { initAnalytics, capture } = await import('./analytics');

      expect(() => initAnalytics()).not.toThrow();
      capture('movie_selected', { movie_id: 3 });
      await settle();

      expect(init, content).not.toHaveBeenCalled();
      expect(mpInit, content).not.toHaveBeenCalled();
      expect(captureEvent, content).not.toHaveBeenCalled();
      expect(mpTrack, content).not.toHaveBeenCalled();
    }
    expect(fetchMock).not.toHaveBeenCalled();
  });

  it('does not load the SDK on localhost, whatever the page holds', async () => {
    vi.stubGlobal('window', { location: { hostname: 'localhost' } });
    stubPage(JSON.stringify({ token: 'phc_from_page', mixpanel_token: 'mp_from_page' }));
    const { initAnalytics, capture } = await import('./analytics');

    initAnalytics();
    capture('movie_selected', { movie_id: 1 });
    await settle();

    expect(init).not.toHaveBeenCalled();
    expect(mpInit).not.toHaveBeenCalled();
    expect(fetchMock).not.toHaveBeenCalled();
    expect(captureEvent).not.toHaveBeenCalled();
    expect(mpTrack).not.toHaveBeenCalled();
  });

  it('leaves Mixpanel off when the page has no Mixpanel token, and keeps nothing waiting', async () => {
    stubPage(JSON.stringify({ token: 'phc_from_page' }));
    const { initAnalytics, capture } = await import('./analytics');

    initAnalytics();
    await vi.waitFor(() => expect(init).toHaveBeenCalledTimes(1));
    capture('movie_selected', { movie_id: 9 });

    expect(mpInit).not.toHaveBeenCalled();
    expect(mpTrack).not.toHaveBeenCalled();
    expect(captureEvent).toHaveBeenCalledWith('movie_selected', { movie_id: 9 });
    expect(fetchMock).not.toHaveBeenCalled();
  });
});
