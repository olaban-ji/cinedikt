import { defineRailway, preserve, project, service } from "railway/iac";

// This repository manages only the API service. Postgres is provisioned
// beside it and is deliberately not claimed here.
// See https://docs.railway.com/infrastructure-as-code#multi-repo-projects
export const partial = "cinedikt";

export default defineRailway(() => {
  const cinedikt = service("cinedikt", {
    build: {
      // Without this the service falls back to Railway's default builder,
      // which would ignore the multi-stage Dockerfile that builds the map,
      // trims the binary and sets APP_ENV=production.
      builder: "DOCKERFILE",
      dockerfilePath: "Dockerfile",
    },
    deploy: {
      // /api/healthz pings Postgres, so a machine that cannot serve
      // never joins the load balancer. The timeout covers a cold start.
      healthcheckPath: "/api/healthz",
      healthcheckTimeout: 120,
      // How long the old container keeps running after it is told to
      // stop. The API gives in-flight requests up to ten seconds, and
      // the notifier uses the first three of them to write its last
      // board and save what it said before the next container reads it.
      // The queue gives its running jobs five, alongside, then cancels
      // them for the next container to retry.
      drainingSeconds: 10,
      // How long the old container keeps serving after the new one is
      // healthy, before it is told to stop, so a request still on its
      // way to the old one is answered rather than dropped. Only one of
      // the two runs the catalog jobs and edits the Telegram board; the
      // other waits for the lease, so the overlap never doubles them.
      // Both run the queue, which hands each job to one of them and lets
      // only its elected leader queue the periodic ones, so that is never
      // doubled either.
      overlapSeconds: 10,
      // The size the service runs at, set in the dashboard before this
      // file declared it. Named here because an apply resets anything
      // left out, and a null limit is not the same machine.
      limitOverride: {
        containers: {
          cpu: 2,
          memoryBytes: 4_000_000_000,
        },
      },
      // restartPolicyType (ON_FAILURE) and sleepApplication (false) are
      // Railway's defaults and are deliberately not declared: the API
      // stores a default as null, so declaring one leaves `config plan`
      // permanently dirty. Both matter to this service — the catalog
      // jobs run between requests and a sleeping machine would drop that
      // work — but the default is already what we want.
    },
    // Every variable the service holds is named here so this file is the
    // whole picture, and every one is preserve()d: the values stay in
    // Railway, out of the repository, and are not touched by an apply.
    // A name missing from this list would be deleted on the next apply.
    env: {
      // "true" turns on the Daily's tools for working on it, Play again
      // among them, while the Dockerfile's APP_ENV=production keeps the
      // JSON logs and analytics. Set on dev only; prod never sets it.
      DAILY_DEV_TOOLS: preserve(),
      // The Daily's launch day, "2026-10-12": No. 1 is for it, and nothing
      // before it is picked, so the service can be deployed ahead of it
      // with the Daily out of sight until each reader's midnight that
      // day. Unset launches on the day of the first pass. Read only by a
      // database with no puzzle yet, so it can stay set.
      DAILY_LAUNCH: preserve(),
      TMDB_API_KEY: preserve(),
      TMDB_ACCESS_TOKEN: preserve(),
      // TMDb requests a second for the whole process, every client and
      // job together; unset takes 20, and below 1 will not start.
      TMDB_RATE_PER_SEC: preserve(),
      // How well known a movie has to be before TMDb is asked for a
      // poster nobody has wanted yet. 0 fetches one for every title
      // TMDb has one for; unset takes the code's default of 100. It is
      // an operational dial rather than a fact about the service, so
      // like the rest it is set in Railway and only named here.
      TMDB_SWEEP_MIN_VOTES: preserve(),
      // How well known a film has to be for the trailer job's sweep to reach
      // it; unset takes 0, every film. A film below a higher floor is looked
      // up only once a reader opens it. An operational dial like the one
      // above.
      TRAILER_SWEEP_MIN_VOTES: preserve(),
      // How well known a person's best movie has to be for the people
      // job's sweep to reach them; unset takes 0, everyone. A person below
      // a higher floor is looked up only once a map with them on it is
      // opened. An operational dial like the ones above.
      PEOPLE_SWEEP_MIN_VOTES: preserve(),
      // How many lookups a second that sweep may take out of
      // TMDB_RATE_PER_SEC; unset takes 5, and 0 or less will not start.
      PEOPLE_SWEEP_RATE: preserve(),
      OMDB_API_KEY: preserve(),
      // How well known a film has to be before OMDb is asked for a
      // synopsis nobody has met yet; unset takes 0, every film. An
      // operational dial like the ones above.
      SYNOPSIS_SWEEP_MIN_VOTES: preserve(),
      // The catalog, which the API serves and its jobs keep current; the
      // API will not start without it. The value is the private URL of
      // the Postgres service, kept out of the repo the same way the other
      // credentials are.
      DATABASE_URL: preserve(),
      // Every tracker's switch (PostHog and Mixpanel, page and server);
      // off unless "true", with the tokens below left in place.
      ANALYTICS_ENABLED: preserve(),
      POSTHOG_PROJECT_TOKEN: preserve(),
      POSTHOG_HOST: preserve(),
      // The Mixpanel project this environment reports to. The API hands
      // it to the page in production; unset, the page never loads it.
      MIXPANEL_PROJECT_TOKEN: preserve(),
      // A bot that keeps a pinned status board of the catalog jobs and
      // says when something needs attention. Both empty means silence. The token is from
      // BotFather; the chat id is the private chat or group it posts into.
      TELEGRAM_BOT_TOKEN: preserve(),
      TELEGRAM_CHAT_ID: preserve(),
      // The zone the bot writes times in, such as Africa/Lagos. Unset
      // means UTC, and the board says so.
      NOTIFY_TIMEZONE: preserve(),
      // Where to watch. The Streaming Availability API key, from Movie of
      // the Night; unset, the section is left out. Its requests a second
      // for the process; unset takes 5. The most pages of its changes
      // feed the daily changes job reads per country per run, a dial on
      // what that job costs; unset takes 40.
      STREAMING_API_KEY: preserve(),
      STREAMING_RATE_PER_SEC: preserve(),
      STREAMING_CHANGES_MAX_PAGES: preserve(),
      // GeoLite2 Country, which places a reader's address in a country.
      // The service downloads it itself with these, and needs both;
      // without them nobody is placed. The account id is not a secret,
      // but it lives in Railway with the key it belongs to.
      MAXMIND_LICENSE_KEY: preserve(),
      MAXMIND_ACCOUNT_ID: preserve(),
      WEB_DIR: preserve(),
    },
  });

  return project("cinedikt", {
    resources: [cinedikt],
  });
});
