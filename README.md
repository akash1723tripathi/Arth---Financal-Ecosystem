# Arth

Personal finance tracker: accounts, income/expense transactions, budgets, dashboard charts, CSV export,
AI receipt scan and budget emails. Android app (Expo), web app (Next.js) and a Go backend with its own auth on PostgreSQL.

Status: the Go API (own auth on Postgres) is built and the Android app runs on it; Clerk and Supabase are gone.
The Next.js web app is next. See `docs/PROGRESS.md`.

| Path | What |
| --- | --- |
| `apps/mobile` | Expo SDK 54 app (Android first) |
| `apps/api` | Go API (built) + worker (stub) |
| `apps/web` | Next.js web app (placeholder) |
| `packages/api-client` | `@arth/api-client`: generated TS types + client, money helpers, session core |
| `api/openapi.yaml` | HTTP contract (source of truth) |
| `docs/` | Architecture, decisions, migration, schema, API, auth, roadmap, progress |
| `legacy/` | Welth's Supabase backend, reference only |

Preview the whole stack locally (Postgres → API → demo data → Android emulator): `docs/PREVIEW_LOCALLY.md`. Started from the Welth boilerplate by piyush-eon (no licence file; see `docs/MIGRATION.md` §7).
