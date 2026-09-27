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
      drainingSeconds: 10,
      // How long the old container keeps serving after the new one is
      // healthy, before it is told to stop, so a request still on its
      // way to the old one is answered rather than dropped. Only one of
      // the two runs the catalog jobs and edits the Telegram board; the
      // other waits for the lease, so the overlap never doubles them.
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
      // permanently dirty. Both matter to this service — the importer
      // runs between requests and a sleeping machine would drop that
      // work — but the default is already what we want.
    },
    // Every variable the service holds is named here so this file is the
    // whole picture, and every one is preserve()d: the values stay in
    // Railway, out of the repository, and are not touched by an apply.
    // A name missing from this list would be deleted on the next apply.
    env: {
      TMDB_API_KEY: preserve(),
      TMDB_ACCESS_TOKEN: preserve(),
      TMDB_RATE_PER_SEC: preserve(),
      // How well known a movie has to be before TMDb is asked for a
      // poster nobody has wanted yet. 0 fetches one for every title
      // TMDb has one for; unset takes the code's default of 100. It is
      // an operational dial rather than a fact about the service, so
      // like the rest it is set in Railway and only named here.
      TMDB_SWEEP_MIN_VOTES: preserve(),
      OMDB_API_KEY: preserve(),
      // The catalog. The importer writes it; the API only reads. The
      // value is the private URL of the Postgres service, kept out of
      // the repo the same way the other credentials are.
      DATABASE_URL: preserve(),
      POSTHOG_PROJECT_TOKEN: preserve(),
      POSTHOG_HOST: preserve(),
      // A bot that keeps a pinned status board of the catalog jobs and
      // says when something needs attention. Both empty means silence. The token is from
      // BotFather; the chat id is the private chat or group it posts into.
      TELEGRAM_BOT_TOKEN: preserve(),
      TELEGRAM_CHAT_ID: preserve(),
      // The zone the bot writes times in, such as Africa/Lagos. Unset
      // means UTC, and the board says so.
      NOTIFY_TIMEZONE: preserve(),
      WEB_DIR: preserve(),
    },
  });

  return project("cinedikt", {
    resources: [cinedikt],
  });
});
