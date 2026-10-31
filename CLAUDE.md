# Arth — project memory

Personal-finance ecosystem: multiple accounts, income/expense transactions, budgets,
dashboard charts, CSV export, AI receipt scan, budget-alert + weekly-tips emails.
Working name **Arth** (may change). Solo developer. 6-month MVP, Android first, iOS later.

Started from **Welth** (Expo + Clerk + Supabase + Gemini, by piyush-eon), a third-party
boilerplate that has **no licence file**. See "Provenance" in @docs/MIGRATION.md.

## Status

- Phase 0: documentation only. No Go or web code exists yet; the Expo app still lives at the repo root.
- The repo is **not a git repo yet**, and three route files in the copied app are broken
  (see `docs/MIGRATION.md` → Step 0). Fix both before any other work.

## Monorepo map (target; pnpm workspaces)

```text
apps/api/            Go module: cmd/api (HTTP), cmd/worker (River jobs)     → apps/api/CLAUDE.md
apps/web/            Next.js (App Router), Clerk, TanStack Query            → docs/WEB_APP.md
apps/mobile/         this Expo app (moves here from repo root)               → apps/mobile/CLAUDE.md
packages/api-client/ TS client generated from api/openapi.yaml
api/openapi.yaml     single source of truth for the HTTP contract
docker-compose.yml   local Postgres
.github/workflows/   path-filtered CI per app
docs/                architecture, migration, schema, API, roadmap, web, deployment
```

Until the move, the Expo app sits at the root (`app/`, `components/`, `hooks/`, `lib/`, …), and
`apps/mobile/CLAUDE.md` applies to those folders.

## Decided (do not change without asking)

- **Backend:** Go, chi, pgx + sqlc, goose migrations, slog, oapi-codegen (OpenAPI-first),
  River for background jobs (Postgres-backed, no Redis), Resend for email, and Gemini through the official Go SDK, server-side only.
- **Auth:** Clerk. Go middleware verifies Clerk JWTs via JWKS. Every query is scoped by `user_id`, which replaces Supabase RLS.
- **DB:** plain managed PostgreSQL (Neon). Money is `BIGINT` minor units plus an ISO-4217 currency code.
- **Deploy:** API and worker run in Docker on Railway or Render, web on Vercel, and mobile ships through EAS Build/Submit/Update.
- **MVP:** accounts, transactions, budget, dashboard aggregates, CSV export, receipt scan,
  budget alerts and weekly tips. **Post-MVP:** voice entry, AI assistant, recurring transactions, goals,
  multi-currency, shared accounts, bank import.

## Hard rules

1. Never commit secrets. Each app keeps a `.env.example` with variable **names only**.
2. All DB access goes through sqlc queries. Every query on user data is scoped by the authenticated `user_id`
   from middleware, never by an id the client sends.
3. Never use floats for money (Go `float64`, JS `parseFloat`/`Number` math, SQL `numeric`/`real`).
   Use integer minor units and parse decimal strings.
4. API changes start in `api/openapi.yaml`. Then regenerate the Go stubs and the TS client, then implement.
5. AI keys (and every other secret) never ship in client code. No secrets in `EXPO_PUBLIC_*` or `NEXT_PUBLIC_*`.
6. Ask before adding a dependency or changing a decided item.
7. After each completed phase, tick the checkboxes in `docs/ROADMAP.md` and update the Status section here.
8. Welth is third-party code. Flag any file that looks copied verbatim so its licence can be
   checked before launch (inventory in `docs/MIGRATION.md` → Provenance).

## Conventions

- **API:** REST under `/v1`, JSON, `snake_case` fields, RFC 9457 `problem+json` errors, and cursor pagination
  (see `docs/API.md`). Another user's resource returns 404, never 403.
- **Money in JSON:** `amount_minor` (integer) plus `currency`. Format for display only, at the edge.
- **Dates:** a transaction date is a calendar `date` (`occurred_on`, `YYYY-MM-DD`) in the user's timezone.
  Timestamps are RFC 3339 UTC.
- **Migrations:** expand/contract only. Old mobile builds stay in the field for months.
- **TS (web/mobile):** server state lives in TanStack Query through `@arth/api-client`. Zustand holds UI/session state only.
- **Go:** see `apps/api/CLAUDE.md` (layout, errors, tests, sqlc/goose, money).
- Small vertical slices: contract → migration → queries → handler → tests → client → UI.

## Commands

Fill these in as they come into existence; until then they are the planned names.

| Task | Command (planned) |
| --- | --- |
| Install JS deps | `pnpm install` |
| Local DB | `docker compose up -d db` |
| Run API / worker | `cd apps/api && go run ./cmd/api` / `go run ./cmd/worker` |
| Migrate | `cd apps/api && go run ./cmd/api migrate up` (goose, embedded migrations) |
| Generate DB code | `cd apps/api && sqlc generate` |
| Generate API stubs + TS client | `pnpm gen:api` (runs oapi-codegen + TS generator) |
| Go tests | `cd apps/api && go test ./...` |
| Web dev | `pnpm --filter @arth/web dev` |
| Mobile dev | `pnpm --filter @arth/mobile start` |
| Current app (pre-move) | `npx expo start -c` |

## Docs (detail lives here, not in this file)

@docs/ARCHITECTURE.md
@docs/MIGRATION.md
@docs/SCHEMA.md
@docs/API.md
@docs/ROADMAP.md
@docs/WEB_APP.md
@docs/DEPLOYMENT.md

## Mobile guidance (temporary, until the app moves to apps/mobile/)

@AGENTS.md
