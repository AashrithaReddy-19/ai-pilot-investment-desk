# C23 AI Pilot Investment Decision Desk: frontend

React 18 + TypeScript (strict) + Vite. The browser talks to the Go gateway only, never to PostgreSQL or the Python rules engine directly.

```
Browser (this app, :5173) -> Go gateway (:8080, /api/v1) -> rules engine (FastAPI, :8000)
                                                         -> PostgreSQL (portfolios, observations, decisions, interviews)
                                                         -> Redis (optional cache)
```

## Run

Start the backend first (see `../README.md`): PostgreSQL, the rules engine (`decision-engine`), then the gateway (`backend`, migrations applied).

```
cd frontend
npm install
npm run dev          # http://localhost:5173, proxies /api, /health and /ready to the gateway
npm test             # unit tests
npm run build        # type-check and production build
```

`VITE_API_BASE` (empty by default) and `VITE_PROXY_TARGET` (default `http://127.0.0.1:8080`) are described in `.env.example`.

## What it does

| Route | Purpose |
| --- | --- |
| `/` | The four-stage workflow: add pilots, evaluate, optimize, review and save |
| `/optimize` | The stage-3 optimizer on its own |
| `/portfolios` | List, reopen and delete saved portfolios |
| `/observations` | Preview stop-rule decisions, record real observations, read history |
| `/interviews` | Record and list stakeholder interviews (role, never a name) |
| `/checks` | Live service health and the three documented public checks (synthetic test case) |
| `/about-limits` | How figures are produced and what the product does not do |

## Rules this app follows

- **No fake data.** It opens empty and requests no case data. The synthetic C23 case loads only when the user chooses it, and is labelled synthetic wherever it appears. A failed request shows an error and a retry, never substitute figures.
- **No calculations in React.** Net benefit, payback, conservative haircuts, optimization, overlap exclusion and stop rules all come from the rules engine. The UI formats and arranges what the API returns.
- **Missing is not zero.** Blank inputs are sent as `null`. Values the engine defaulted are tagged "assumed".
- **Stale results are hidden.** Every result carries a key for the inputs behind it; if inputs change, the old numbers are not shown as current.
- **Pilots are session drafts.** The gateway has no endpoint that stores pilots, so typed pilots live in memory and are sent as `fixture` and `extension` overrides. They become durable only inside a saved portfolio. Only the theme preference is kept in `localStorage`.

## Layout

```
src/api         typed client (the only code that touches the network), shared types, health hook
src/domain      draft pilots, validation, payload builders, formatting
src/state       workspace store, theme
src/components  app shell, navigation, status indicator, UI primitives
src/features    workspace stages, observations, portfolios, interviews, system pages
src/styles      design tokens (light and dark) and component styles
```
