import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';

const init = vi.fn();
const captureEvent = vi.fn();
const getDistinctId = vi.fn(() => 'distinct-1');
const getSessionId = vi.fn(() => 'session-1');

const mpInit = vi.fn();
const mpTrack = vi.fn();

vi.mock('mixpanel-browser', () => ({
  default: { init: mpInit, track: mpTrack },
}));

vi.mock('posthog-js', () => ({
  default: {
    init,
    capture: captureEvent,
    get_distinct_id: getDistinctId,
    get_session_id: getSessionId,
  },
}));

describe('analytics', () => {
  beforeEach(() => {
    vi.resetModules();
    vi.unstubAllEnvs();
    vi.stubGlobal('window', { location: { hostname: 'map.example' } });
    init.mockClear();
    captureEvent.mockClear();
    getDistinctId.mockClear();
    getSessionId.mockClear();
    mpInit.mockClear();
    mpTrack.mockClear();
  });

  afterEach(() => {
    vi.unstubAllGlobals();
  });

  it('loads the SDK from a baked token and flushes queued captures', async () => {
    vi.stubEnv('VITE_POSTHOG_PROJECT_TOKEN', 'phc_test');
    vi.stubEnv('VITE_POSTHOG_HOST', 'https://example.posthog.com');
    vi.stubGlobal('fetch', vi.fn().mockResolvedValue({ ok: false }));
    const { initAnalytics, capture, analyticsHeaders } = await import('./analytics');

    capture('movie_selected', { movie_id: 603, title: 'The Matrix' });
    expect(captureEvent).not.toHaveBeenCalled();
    expect(analyticsHeaders()).toEqual({});

    initAnalytics();
    await vi.waitFor(() => expect(init).toHaveBeenCalledTimes(1));

    expect(init).toHaveBeenCalledWith('phc_test', {
      api_host: 'https://example.posthog.com',
      defaults: '2026-05-30',
      tracing_headers: ['map.example'],
    });
    expect(captureEvent).toHaveBeenCalledWith('movie_selected', {
      movie_id: 603,
      title: 'The Matrix',
    });
    expect(analyticsHeaders()).toEqual({
      'X-PostHog-Distinct-Id': 'distinct-1',
      'X-PostHog-Session-Id': 'session-1',
    });
  });

  it('does not load the SDK when no token is available', async () => {
    const fetchMock = vi.fn().mockResolvedValue({ ok: false });
    vi.stubGlobal('fetch', fetchMock);
    const { initAnalytics, capture } = await import('./analytics');

    initAnalytics();
    capture('movie_selected', { movie_id: 1 });
    await Promise.resolve();
    await Promise.resolve();

    expect(fetchMock).toHaveBeenCalledWith('/api/analytics-config');
    expect(init).not.toHaveBeenCalled();
    expect(captureEvent).not.toHaveBeenCalled();
  });

  it('loads the SDK from the API config when no token is baked in', async () => {
    const fetchMock = vi.fn().mockResolvedValue({
      ok: true,
      json: async () => ({ token: 'phc_from_api', host: 'https://api.posthog.com' }),
    });
    vi.stubGlobal('fetch', fetchMock);
    const { initAnalytics, capture } = await import('./analytics');

    initAnalytics();
    capture('movie_selected', { movie_id: 7 });
    await vi.waitFor(() => expect(init).toHaveBeenCalledTimes(1));

    expect(init).toHaveBeenCalledWith('phc_from_api', {
      api_host: 'https://api.posthog.com',
      defaults: '2026-05-30',
      tracing_headers: ['map.example'],
    });
    expect(captureEvent).toHaveBeenCalledWith('movie_selected', { movie_id: 7 });
  });

  it('does not load the SDK on localhost', async () => {
    vi.stubGlobal('window', { location: { hostname: 'localhost' } });
    vi.stubEnv('VITE_POSTHOG_PROJECT_TOKEN', 'phc_test');
    const fetchMock = vi.fn();
    vi.stubGlobal('fetch', fetchMock);
    const { initAnalytics, capture, analyticsHeaders } = await import('./analytics');

    initAnalytics();
    capture('movie_selected', { movie_id: 1 });
    await Promise.resolve();

    expect(init).not.toHaveBeenCalled();
    expect(mpInit).not.toHaveBeenCalled();
    expect(fetchMock).not.toHaveBeenCalled();
    expect(captureEvent).not.toHaveBeenCalled();
    expect(mpTrack).not.toHaveBeenCalled();
    expect(analyticsHeaders()).toEqual({});
  });

  it('loads Mixpanel from the API config and sends it the same events', async () => {
    const fetchMock = vi.fn().mockResolvedValue({
      ok: true,
      json: async () => ({
        token: 'phc_from_api',
        host: 'https://api.posthog.com',
        mixpanel_token: 'mp_from_api',
      }),
    });
    vi.stubGlobal('fetch', fetchMock);
    const { initAnalytics, capture } = await import('./analytics');

    capture('movie_selected', { movie_id: 7 });
    initAnalytics();
    await vi.waitFor(() => expect(mpInit).toHaveBeenCalledTimes(1));

    expect(mpInit).toHaveBeenCalledWith('mp_from_api', {
      autocapture: true,
      record_sessions_percent: 100,
    });
    expect(mpTrack).toHaveBeenCalledWith('movie_selected', { movie_id: 7 });
    await vi.waitFor(() => expect(captureEvent).toHaveBeenCalledWith('movie_selected', { movie_id: 7 }));
  });

  it('asks the API for nothing when both tokens are baked in', async () => {
    vi.stubEnv('VITE_POSTHOG_PROJECT_TOKEN', 'phc_test');
    vi.stubEnv('VITE_MIXPANEL_PROJECT_TOKEN', 'mp_baked');
    const fetchMock = vi.fn();
    vi.stubGlobal('fetch', fetchMock);
    const { initAnalytics } = await import('./analytics');

    initAnalytics();
    await vi.waitFor(() => expect(mpInit).toHaveBeenCalledTimes(1));

    expect(mpInit).toHaveBeenCalledWith('mp_baked', expect.any(Object));
    expect(init).toHaveBeenCalledTimes(1);
    expect(fetchMock).not.toHaveBeenCalled();
  });

  it('takes the Mixpanel token from the API when only PostHog is baked in', async () => {
    vi.stubEnv('VITE_POSTHOG_PROJECT_TOKEN', 'phc_test');
    const fetchMock = vi.fn().mockResolvedValue({
      ok: true,
      json: async () => ({ token: 'phc_other', mixpanel_token: 'mp_from_api' }),
    });
    vi.stubGlobal('fetch', fetchMock);
    const { initAnalytics } = await import('./analytics');

    initAnalytics();
    await vi.waitFor(() => expect(mpInit).toHaveBeenCalledTimes(1));

    expect(mpInit).toHaveBeenCalledWith('mp_from_api', expect.any(Object));
    // The baked PostHog token still wins over the served one.
    expect(init).toHaveBeenCalledWith('phc_test', expect.any(Object));
  });

  it('leaves Mixpanel off when no token is served, and keeps nothing waiting', async () => {
    const fetchMock = vi.fn().mockResolvedValue({
      ok: true,
      json: async () => ({ token: 'phc_from_api' }),
    });
    vi.stubGlobal('fetch', fetchMock);
    const { initAnalytics, capture } = await import('./analytics');

    initAnalytics();
    await vi.waitFor(() => expect(init).toHaveBeenCalledTimes(1));
    capture('movie_selected', { movie_id: 9 });

    expect(mpInit).not.toHaveBeenCalled();
    expect(mpTrack).not.toHaveBeenCalled();
    expect(captureEvent).toHaveBeenCalledWith('movie_selected', { movie_id: 9 });
  });
});
