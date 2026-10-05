const DEFAULT_HOST = 'https://us.i.posthog.com';
const LOOPBACK = new Set(['localhost', '127.0.0.1', '::1', '[::1]']);

type PostHog = typeof import('posthog-js').default;
type Mixpanel = typeof import('mixpanel-browser').default;

/** One analytics service as the rest of the map sees it. A call made
 *  before the SDK has loaded waits in the queue. Once it is known the SDK
 *  never will load (no token, loopback, or the chunk failed), the queue is
 *  dropped and later calls are ignored, so a page with one service turned
 *  off does not keep every event it ever raised. */
function service<T>() {
  let client: T | null = null;
  let off = false;
  const queued: Array<(c: T) => void> = [];
  return {
    use(fn: (c: T) => void): void {
      if (client) fn(client);
      else if (!off) queued.push(fn);
    },
    ready(c: T): void {
      client = c;
      const pending = queued.splice(0);
      for (const fn of pending) fn(c);
    },
    never(): void {
      off = true;
      queued.length = 0;
    },
  };
}

const posthog = service<PostHog>();
const mixpanel = service<Mixpanel>();

/** What the page needs to report. The server writes it into the page it
 *  serves, as the cinedikt-analytics meta tag, and only when tracking is
 *  on; cmd/api's analyticsMeta builds it. */
interface AnalyticsConfig {
  token?: string;
  host?: string;
  mixpanel_token?: string;
}

const CONFIG_META = 'meta[name="cinedikt-analytics"]';

/** The settings the server wrote into the page, or none. A missing, empty
 *  or malformed tag means no trackers rather than an error, since the map
 *  must never depend on analytics, and nothing is asked of the network in
 *  its place. */
function pageConfig(): AnalyticsConfig {
  try {
    const content = globalThis.document?.querySelector(CONFIG_META)?.getAttribute('content');
    if (!content) return {};
    const parsed: unknown = JSON.parse(content);
    if (typeof parsed !== 'object' || parsed === null) return {};
    const { token, host, mixpanel_token } = parsed as Record<string, unknown>;
    return {
      token: typeof token === 'string' ? token : undefined,
      host: typeof host === 'string' ? host : undefined,
      mixpanel_token: typeof mixpanel_token === 'string' ? mixpanel_token : undefined,
    };
  } catch {
    return {};
  }
}

function pageHostname(): string {
  return globalThis.window?.location?.hostname ?? '';
}

function isLoopback(hostname: string): boolean {
  return LOOPBACK.has(hostname);
}

function startPostHog(token: string | undefined, host: string): void {
  if (!token) {
    posthog.never();
    return;
  }
  void import('posthog-js')
    .then(({ default: ph }) => {
      ph.init(token, { api_host: host, defaults: '2026-05-30' });
      posthog.ready(ph);
    })
    .catch(() => {
      // Analytics is optional; a missing SDK should not break the map.
      posthog.never();
    });
}

function startMixpanel(token: string | undefined): void {
  if (!token) {
    mixpanel.never();
    return;
  }
  void import('mixpanel-browser')
    .then(({ default: mp }) => {
      // Autocapture records clicks, inputs and page views, route changes
      // included. Every session is recorded, with all text and inputs
      // masked, which is the SDK's default.
      mp.init(token, { autocapture: true, record_sessions_percent: 100 });
      mixpanel.ready(mp);
    })
    .catch(() => {
      mixpanel.never();
    });
}

/** Load PostHog and Mixpanel, each in its own chunk so the map is not
 *  paying for them up front. Their tokens come only from the tag the
 *  server writes into the page from each environment's own variables, so
 *  one build reports to the right projects everywhere and ANALYTICS_ENABLED
 *  is the one switch: with tracking off the server writes no tag, and both
 *  services are turned off at once without a request of any kind.
 *  Loopback never initialises, so local sessions reach neither service. */
export function initAnalytics(): void {
  if (isLoopback(pageHostname())) {
    posthog.never();
    mixpanel.never();
    return;
  }
  const page = pageConfig();
  startPostHog(page.token, page.host || DEFAULT_HOST);
  startMixpanel(page.mixpanel_token);
}

/** One of the map's own events, sent to every service that is on. */
export function capture(...args: Parameters<PostHog['capture']>): void {
  posthog.use((ph) => {
    ph.capture(...args);
  });
  const [event, properties] = args;
  mixpanel.use((mp) => {
    mp.track(event, properties ?? undefined);
  });
}
