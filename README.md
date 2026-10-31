# Arth

Personal finance tracker: accounts, income/expense transactions, budgets, dashboard charts, CSV export,
AI receipt scan and budget emails. Android app (Expo), web app (Next.js) and a Go backend with its own auth on PostgreSQL.

Status: the mobile app is restructured into the monorepo, but it **still runs on the legacy Welth stack (Clerk + Supabase)**.
The Go backend is next. See `docs/PROGRESS.md`.

| Path | What |
| --- | --- |
| `apps/mobile` | Expo SDK 54 app (Android first) |
| `apps/api` | Go API + worker (placeholder) |
| `apps/web` | Next.js web app (placeholder) |
| `packages/api-client` | Generated TS client (placeholder) |
| `api/openapi.yaml` | HTTP contract (source of truth) |
| `docs/` | Architecture, decisions, migration, schema, API, auth, roadmap, progress |
| `legacy/` | Welth's Supabase backend, reference only |

Run the mobile app locally: `docs/RUN_LOCALLY.md`. Started from the Welth boilerplate by piyush-eon (no licence file; see `docs/MIGRATION.md` §7).
