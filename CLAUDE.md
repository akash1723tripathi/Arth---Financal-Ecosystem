# Arth: project memory

Arth is a personal finance tracker. Users get multiple accounts, income/expense transactions, a monthly budget, dashboard charts,
CSV export, AI receipt scan, and budget-alert + weekly-tips emails. Voice entry and an AI assistant come later.
Android first (Expo), then a Next.js web app, both backed by a Go API with its **own authentication** on PostgreSQL.
Solo developer, 6-month MVP. The project started from the third-party Welth boilerplate (no licence file; see `docs/MIGRATION.md` §7).

## Status

- **Done:** mobile app restructured into the monorepo and renamed to Arth (placeholder id `fyi.akashbuilds.arth`, scheme `arth`).
- **Done:** Go API in `apps/api` with own auth: all 33 operations in `api/openapi.yaml`, 442 tests against real Postgres, live smoke test
  (branch `feat/go-api`). Not built: transfers, password reset/verification, worker jobs, real providers.
- **Temporary:** the mobile app **still uses Clerk + Supabase + client-side Gemini**. These are marked "to be replaced" and must not be removed
  until Phase 4 repoints mobile to the Go API.
- **Next session:** the Next.js web app (`apps/web`); first decide D-OPEN-6 (web token transport). Full detail: `docs/PROGRESS.md`.

## Session order (decided)

1. Mobile restructure ✅
2. Go API incl. auth ✅
3. Next.js web
4. Repoint mobile to the Go API and drop Clerk/Supabase
5. Android emulator testing
6. Hardening
7. Play closed test
8. Launch

## Monorepo map

```text
apps/mobile/          Expo SDK 54 app (Android first)          → apps/mobile/CLAUDE.md
apps/api/             Go API (done for MVP) + worker (stub)            → apps/api/CLAUDE.md
apps/web/             Next.js web app (placeholder)            → apps/web/CLAUDE.md
packages/api-client/  TS client generated from the contract (placeholder)
api/openapi.yaml      HTTP contract: single source of truth (OpenAPI 3.1, 33 operations)
docker-compose.yml    local Postgres 16 (dev only)
legacy/               Welth Supabase functions + SQL: reference only, deleted later
docs/                 all design docs (index below)
.claude/              settings (permissions), commands (/status, /verify-mobile, /update-docs), path rules
```

pnpm workspace (`pnpm-workspace.yaml`, pnpm 10 via corepack, `nodeLinker: hoisted`, which Metro needs). Run JS commands from the repo root.

## Decisions (details and reasons: `docs/DECISIONS.md`)

- **No third-party SaaS in the product.** No Clerk, no Supabase. Auth is built in Go, and Go talks to PostgreSQL directly.
- Anything that genuinely needs an outside provider sits **behind a Go interface** and is an **OPEN** item until decided:
  LLM (receipt/voice/assistant), email delivery, object storage, push notifications, production Postgres/API/web hosting.
  The existing Gemini/Resend usage is legacy and will be re-decided.
- Go stack: chi, pgx, sqlc, goose, oapi-codegen (OpenAPI-first), slog. Background jobs use a Postgres-backed queue (River or equivalent). Next.js web. Expo mobile.
- Money is `BIGINT` minor units + ISO-4217 currency code, everywhere. Every user-data query is scoped by the authenticated `user_id`.
- Build tools are fine: pnpm, Docker, GitHub Actions (once a remote exists), EAS Build. There is no GitHub repo; git is local only.

## Hard rules

1. **No secrets in git.** Only `.env.example` files with variable names. Never ask the user to paste secrets into chat.
2. **Never use floats for money**, in any language or column type. Use integer minor units and parse decimal strings.
3. **No AI or provider keys in client code.** No secrets in `EXPO_PUBLIC_*` / `NEXT_PUBLIC_*`.
4. **No third-party services without asking.** Every provider goes behind an interface and is recorded in `docs/DECISIONS.md`.
5. **Ask before adding a dependency** or changing a decided item.
6. Every DB query on user data goes through sqlc and is filtered by the authenticated `user_id`.
7. API changes start in `api/openapi.yaml`, then regenerate the code.
8. Never claim something works without running it, and show the real output. Things you cannot run go under "must test manually".
9. After each phase, update `docs/ROADMAP.md` (checkboxes) and `docs/PROGRESS.md` (`/update-docs` helps).
10. Welth is third-party code. Flag anything copied verbatim (`docs/MIGRATION.md` §7).

## Git

Local only. Branch per piece of work (current: `feat/go-api`). Conventional Commits, one commit per task. `main` holds the Welth baseline.

## Commands (from the repo root)

| Task | Command |
| --- | --- |
| Install | `pnpm install` |
| Verify mobile (tsc + lint + Android bundle) | `pnpm verify:mobile` (or `/verify-mobile`) |
| Run mobile | `pnpm mobile` (see `docs/RUN_LOCALLY.md`) |
| Local Postgres | `pnpm db:up` / `pnpm db:down` (needs `.env` from `.env.example`) |
| Go API | `cd apps/api; go run ./cmd/api` (+ `migrate up`, `go test ./...`), see `docs/RUN_API_LOCALLY.md` |
| Local preview (DB → API → seed → app) | `pnpm dev:db`, `pnpm dev:api`, `pnpm dev:seed`, `pnpm dev:mobile` (separate terminals), see `docs/PREVIEW_LOCALLY.md` |
| Regenerate the TS client / money checks | `pnpm gen:api` · `pnpm check:money` · `pnpm --filter @arth/api-client live-check` |

## Docs index

| File | What |
| --- | --- |
| `docs/PROGRESS.md` | Done / next / known problems; start here each session |
| `docs/ROADMAP.md` | Phases with checkboxes, MVP vs post-MVP |
| `docs/DECISIONS.md` | Decided items and OPEN provider decisions |
| `docs/ARCHITECTURE.md` | Current (Welth) vs target architecture |
| `docs/MIGRATION.md` | Every Supabase/Clerk/Gemini call (file:line) → Go endpoint; provenance checklist |
| `docs/AUTH.md` | Own-auth design (proposal) |
| `docs/SCHEMA.md` | Target Postgres schema |
| `docs/API.md` | REST endpoints and conventions |
| `docs/MOBILE_SCREENS.md` | Screen-by-screen spec (also the web app spec) |
| `docs/WEB_APP.md` | Next.js structure |
| `docs/DEPLOYMENT.md` | Environments, env var names, CI/CD (hosting OPEN) |
| `docs/RUN_LOCALLY.md` | Windows steps to run the mobile app on an emulator |
| `docs/RUN_API_LOCALLY.md` | Windows PowerShell steps to run the Go API, migrations, tests and example calls |
| `docs/PREVIEW_LOCALLY.md` | One-command-per-terminal local preview on the Android emulator, troubleshooting, manual test checklist |
