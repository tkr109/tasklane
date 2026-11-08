# Tasklane dashboard

The web dashboard for Tasklane: queue and worker counts, the autoscaler at work, jobs with
live progress, teams and their plans, and dead jobs grouped by error. See `docs/design.md`
for the bigger picture.

Built with Vite, React, TypeScript, React Router, TanStack Query and Recharts. Styles are
plain CSS with theme tokens in `src/styles/tokens.css`.

## Running it

```sh
npm install
npm run dev        # against the Go API on localhost:8080
npm run dev:mock   # against mock data, no backend needed
```

The dev server proxies `/api` and `/healthz` to `http://localhost:8080`. Point it somewhere
else with `TASKLANE_API=http://host:port npm run dev`.

There are no logins. Job routes run as the team that owns `VITE_API_KEY`, and the admin
routes take `VITE_ADMIN_TOKEN`. `.env.development` sets both to the Makefile's defaults
(`tk_live_localdev` and `dev-admin-token`); override them in `.env.development.local`.

```sh
npm run build      # type check and production build into dist/
npm run lint
```

`npm run build -- --mode mock` builds a static demo that runs entirely on mock data.

## Mock mode

With `VITE_MOCK=1` (which `dev:mock` sets through `.env.mock`), [MSW](https://mswjs.io)
answers every `/api` request in the browser. A small simulation in `src/mocks/` keeps a
queue, workers that scale between 2 and 10, teams with rate limits, and a few hundred job
records, so every page works and "Flood 10k jobs" shows the autoscaler react.

The mocks return the same shapes and errors the real API does or will, auth included, so
pages switch over as endpoints land. Without the mock flag, a page whose endpoint doesn't
exist yet says so instead of breaking.

In the mock data the `default` team is on Enterprise rather than Pro, so a 10k flood fits
in its burst. Switch it to Pro on the Teams page to watch the flood hit a 429.

## API the dashboard expects

Already in the backend: the job routes (`/api/jobs`, `/api/jobs/batch`, `/api/jobs/{id}`,
cancel and retry) and the admin routes for plans, teams and keys.

Still to build, with the shapes in `src/api/types.ts` and `src/api/live.ts`:

| Endpoint | Used by |
| --- | --- |
| `GET /api/stats` | Overview, sidebar counts, workers stats row |
| `GET /api/workers` | Workers page, autoscaler decisions |
| `GET /api/dead-groups`, `GET /api/dead-groups/{id}/jobs`, `POST /api/dead-groups/{id}/retry` | Dead jobs |
| `GET /api/events?token=` (SSE) | Live updates: `job`, `stats`, `workers`, `decision`, `team_usage` |

These are admin routes. `EventSource` can't send an `Authorization` header, so the stream
takes the admin token as a `token` query parameter. Per-minute usage and fair share on the
Teams page only arrive through `team_usage` events for now.

Until `/api/events` exists, the dashboard polls every five seconds instead.
