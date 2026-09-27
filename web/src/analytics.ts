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
    current: (): T | null => client,
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

/** What the page needs to report, as /api/analytics-config serves it. */
interface AnalyticsConfig {
  token?: string;
  host?: string;
  mixpanel_token?: string;
}

function pageHostname(): string {
  return globalThis.window?.location?.hostname ?? '';
}

function isLoopback(hostname: string): boolean {
  return LOOPBACK.has(hostname);
}

function tracingHostnames(): string[] {
  const hostname = pageHostname();
  if (!hostname || isLoopback(hostname)) return [];
  return [hostname];
}

function startPostHog(token: string | undefined, host: string): void {
  if (!token) {
    posthog.never();
    return;
  }
  void import('posthog-js')
    .then(({ default: ph }) => {
      ph.init(token, {
        api_host: host,
        defaults: '2026-05-30',
        // Same-origin /api calls. Hostname only, never a port or URL.
        tracing_headers: tracingHostnames(),
      });
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
 *  paying for them up front. Vite bakes VITE_* in at build time; whatever
 *  is not baked in is asked of the API, which serves each environment's
 *  own tokens from its variables, so one build reports to the right
 *  projects everywhere. Loopback never initialises, so local sessions
 *  reach neither service. */
export function initAnalytics(): void {
  if (isLoopback(pageHostname())) {
    posthog.never();
    mixpanel.never();
    return;
  }
  const baked: AnalyticsConfig = {
    token: import.meta.env.VITE_POSTHOG_PROJECT_TOKEN as string | undefined,
    host: import.meta.env.VITE_POSTHOG_HOST as string | undefined,
    mixpanel_token: import.meta.env.VITE_MIXPANEL_PROJECT_TOKEN as string | undefined,
  };
  const config: Promise<AnalyticsConfig> =
    baked.token && baked.mixpanel_token
      ? Promise.resolve(baked)
      : fetch('/api/analytics-config')
          .then((r) => (r.ok ? r.json() : {}))
          .then((served: AnalyticsConfig | null) => ({
            token: baked.token || served?.token,
            host: baked.token ? baked.host : served?.host,
            mixpanel_token: baked.mixpanel_token || served?.mixpanel_token,
          }))
          .catch(() => baked);
  void config.then((cfg) => {
    startPostHog(cfg.token, cfg.host || DEFAULT_HOST);
    startMixpanel(cfg.mixpanel_token);
  });
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

/** Headers for /api fetches that leave before posthog-js patches `fetch`. */
export function analyticsHeaders(): Record<string, string> {
  const client = posthog.current();
  if (!client) return {};
  const headers: Record<string, string> = {};
  try {
    const distinctId = client.get_distinct_id();
    if (distinctId) headers['X-PostHog-Distinct-Id'] = distinctId;
    const sessionId = client.get_session_id();
    if (sessionId) headers['X-PostHog-Session-Id'] = sessionId;
  } catch {
    // SDK not initialised yet.
  }
  return headers;
}
