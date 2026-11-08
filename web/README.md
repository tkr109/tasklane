# Tasklane dashboard

The web dashboard for Tasklane: queue and worker counts, the autoscaler at work, jobs with
live progress, schedules, teams and their plans, and dead jobs grouped by error. See `docs/design.md`
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

The mocks return the same shapes and errors the real API does, auth included, and send the
same events on the stream, so mock mode is a fair stand-in for the real thing.

In the mock data the `default` team is on Enterprise rather than Pro, so a 10k flood fits
in its burst. Switch it to Pro on the Teams page to watch the flood hit a 429. Against the
real API, switch `default` to Enterprise first.

## API the dashboard uses

The job and schedule routes run as the team of `VITE_API_KEY`. The admin routes take
`VITE_ADMIN_TOKEN`: plans, teams and keys, `GET /api/stats`, `GET /api/workers`, the dead
job groups, and the event stream at `GET /api/events`. The job page reads through
`GET /api/admin/jobs/{id}`, as workers and dead groups link to every team's jobs, and falls
back to the team's key if the admin token is refused. `EventSource` can't send an
`Authorization` header, so the stream takes the admin token as a `token` query parameter.

The stream's events are typed in `src/api/live.ts`. `job`, `worker`, `stats` and
`team_usage` patch the query cache in place, `decision` adds to the scaler log and `reset`
refetches everything. While the stream is down, every query polls every five seconds.

Stats carry a `scaler` field only while a scaler runs; without one the overview and workers
pages say so and keep working.
