# Roadmap: 6-month MVP (Android first)

Window: **Mon 2 Nov 2026 → Fri 30 Apr 2027**. Each "month" is about 4 weeks. Tags: **[MVP]** must ship · **[POST]** after launch · **[STRETCH]** only if ahead of schedule.
Tick the boxes as work lands. After each phase, update the Status section in the root `CLAUDE.md`.

Critical-path dates (do not slip):

- **Week 1:** hello-world API deployed to staging.
- **End of Month 2:** mobile fully on the Go API, Supabase off.
- **1 Mar 2027:** Google Play **closed test running with ≥ 12 opted-in testers**. New personal developer accounts must run 14 continuous days
  of closed testing before applying for production access, and the review takes extra days. Verify the current policy when creating the account.
- **26 Apr 2027:** production rollout.

---

## Phase 0: Documentation & baseline (now → 1 Nov 2026)

- [x] Explore the Welth copy and write project memory (`CLAUDE.md`, `docs/*`, draft `apps/*/CLAUDE.md`)
- [ ] Decide the open questions (see the end of this file)

## Month 1: Scaffold + core API (2 Nov → 29 Nov 2026)

**Week 1: scaffold and deploy hello-world to staging**

- [ ] `git init`, commit the untouched Welth baseline, add `.gitignore` entries (`supabase/.temp/`, `.env*`)
- [ ] Step 0 fixes: restore `app/_layout.tsx`, `app/index.tsx`, `app/(root)/_layout.tsx`, `tsconfig.json`; the app runs on an Android emulator against Supabase
- [ ] Licence: contact piyush-eon for permission, or schedule rewrites (see `docs/MIGRATION.md` §7)
- [ ] pnpm workspace: `pnpm-workspace.yaml`, move the Expo app to `apps/mobile` (follow the Expo SDK 54 monorepo guide), remove `package-lock.json`
- [ ] `apps/api`: Go module, `cmd/api` with `/healthz` + `/readyz`, slog, config from env, graceful shutdown
- [ ] `apps/api/Dockerfile` (multi-stage, distroless/static), `docker-compose.yml` (Postgres 17)
- [ ] Neon project: `main` (prod) + `staging` branches; roles `arth_migrator`, `arth_app`
- [ ] Railway **or** Render: staging API service from the Docker image, deployed and answering `/healthz`
- [ ] GitHub repo + Actions skeleton: `api.yml` (build + test), path filters

**Week 2: contract + DB foundation**

- [ ] `api/openapi.yaml` skeleton: info, servers, security scheme, problem+json, pagination components
- [ ] oapi-codegen config + `go generate`; TS client package `packages/api-client` + `pnpm gen:api`
- [ ] goose baseline migration (MVP tables from `docs/SCHEMA.md`, River tables, category seed); `api migrate` subcommand with embedded migrations
- [ ] sqlc config + first queries; `internal/money` (ISO-4217 exponents, decimal-string parse/format) with table tests
- [ ] Clerk JWT middleware (JWKS cache, `iss`/`exp`/`azp` checks) + `sub → users.id` resolution

**Week 3: identity + accounts + categories**

- [ ] [MVP] `GET/PATCH/DELETE /v1/me`, `POST /v1/me/onboarding` (with provisioning tx)
- [ ] [MVP] `GET /v1/categories`
- [ ] [MVP] Accounts CRUD + `make-default` + delete with `409 account_has_transactions`
- [ ] Integration-test harness: test DB, per-test cleanup, "second user can't see it" helper

**Week 4: transactions + budget**

- [ ] [MVP] Transactions list (keyset, filters, `q`), get, create, patch, delete, with balance updates in one tx
- [ ] [MVP] Balance invariant asserted in tests; concurrent-create test
- [ ] [MVP] Budgets: `GET /v1/budgets`, `PUT/DELETE /v1/budgets/overall`; per-category endpoints (API only)
- [ ] [STRETCH] Transfers `POST/DELETE /v1/transfers`
- [ ] Staging auto-deploy on merge to `main`; migrations run pre-deploy

## Month 2: Parity + repoint mobile (30 Nov → 27 Dec 2026)

- [ ] [MVP] `GET /v1/dashboard`, `GET /v1/reports/daily`
- [ ] [MVP] `GET /v1/transactions/export` (CSV streaming)
- [ ] Mobile Step 0b: move direct Supabase calls from screens into hooks; add the `EXPO_PUBLIC_BACKEND` switch
- [ ] Mobile: `useApi`, `useMeQuery`; replace `useUserSync`; onboarding on the API
- [ ] Mobile: accounts slice → transactions slice (`useInfiniteQuery`) → budget slice → dashboard/chart → export
- [ ] Mobile: money helpers replace every `parseFloat`; `occurred_on` dates
- [ ] Mobile: transaction edit screen (new, uses `PATCH`)
- [ ] Mobile: hide voice + assistant behind a feature flag (still client-side Gemini until POST)
- [ ] **Cut-over:** default backend = api; delete Supabase code, deps, env; unschedule Supabase cron; pause the Supabase project
- [ ] EAS: `eas.json` profiles (development / preview / production), Android package id, dev client build on a real device
- [ ] `mobile.yml` CI (typecheck, lint, expo-doctor)

## Month 3: Web app (28 Dec 2026 → 24 Jan 2027)

- [ ] `apps/web` Next.js App Router + `@clerk/nextjs` + TanStack Query + `@arth/api-client`
- [ ] [MVP] Sign-in/up, onboarding, dashboard, transactions (table, filters, search, export), add/edit transaction, accounts, budget, settings
- [ ] [MVP] Public pages: landing, `/privacy`, `/terms`, `/delete-account` (Play requires a web deletion path), `/unsubscribe`
- [ ] [MVP] Account deletion flow end-to-end (API + Clerk)
- [ ] Vercel project: preview → staging API, production → prod API; CORS allowlist on the API
- [ ] `web.yml` CI (typecheck, lint, build)

## Month 4: AI + worker (25 Jan → 21 Feb 2027)

- [ ] [MVP] `internal/ai` Gemini client (Go SDK, timeouts, retries, response schemas, prompt files)
- [ ] [MVP] `POST /v1/ai/receipt-extractions` + per-user rate limit; mobile + web repointed
- [ ] [MVP] Delete client Gemini code + `EXPO_PUBLIC_GEMINI_API_KEY`; **rotate the Gemini key**
- [ ] [MVP] `cmd/worker` with River: `budget_alerts_scan`, `budget_alert_check`, `weekly_tips_scan`, `weekly_tips`, `send_email`, `balance_reconcile`
- [ ] [MVP] Email templates (`html/template`, text part), Resend domain verified, `List-Unsubscribe`, unsubscribe endpoint + web page
- [ ] [MVP] Email prefs toggles in mobile + web settings
- [ ] [MVP] Enqueue `budget_alert_check` on expense writes (near-real-time alerts)
- [ ] Worker deployed to staging (direct DB URL); `worker run <job>` CLI for manual runs
- [ ] [POST] `POST /v1/ai/voice-extractions`, `POST /v1/assistant/messages`

## Month 5: Hardening + closed beta (22 Feb → 28 Mar 2027)

- [ ] Rebrand complete: icons, splash, colours, copy, `app.json` identifiers; provenance checklist (`docs/MIGRATION.md` §7) all cleared
- [ ] Error tracking + uptime checks (tool TBD: ask before adding)
- [ ] Idempotency-Key support on POST transactions/transfers/AI
- [ ] Security pass: authz tests for every endpoint, rate limits, upload limits, headers, dependency audit (`govulncheck`, `pnpm audit`)
- [ ] Neon: point-in-time restore verified (do a test restore); prod branch protected
- [ ] Performance: seed 50k transactions for one user; p95 list/dashboard < 200 ms on staging
- [ ] Accessibility + small-screen pass on Android; offline/poor-network behaviour (retry, error states)
- [ ] Play Console: developer account, app created, Data safety form, privacy policy URL, account-deletion URL, content rating, target audience, financial-features declaration
- [ ] **By 1 Mar:** closed testing track live with ≥ 12 testers; keep it running ≥ 14 days; fix the feedback
- [ ] EAS Update channels wired (preview, production); `runtimeVersion` policy decided
- [ ] Production environment stood up (Neon main, Clerk prod instance + domain, Resend prod domain, API + worker prod services)

## Month 6: Store launch (29 Mar → 30 Apr 2027)

- [ ] Apply for production access (after 14 days of closed testing)
- [ ] Store listing: screenshots, feature graphic, description, support email/site
- [ ] Prod deploy checklist: migrations, env, CORS, rate limits, email from-address, monitoring alerts
- [ ] Staged rollout 10% → 50% → 100%, watching crash and error rates
- [ ] Web production launch on the custom domain
- [ ] Post-launch: weekly backup-restore drill, on-call runbook (solo: alerts to phone)
- [ ] Buffer week for review rejections or slips

## Post-MVP backlog (after launch, roughly in priority order)

- [ ] [POST] Voice entry (server-side) → re-enable in the app
- [ ] [POST] AI spending assistant (server-side context)
- [ ] [POST] Per-category budgets UI
- [ ] [POST] Recurring transactions (`recurring_rules` + `recurring_materialize` job)
- [ ] [POST] Goals
- [ ] [POST] Receipt attachments (object storage + `attachments`)
- [ ] [POST] Custom categories
- [ ] [POST] Push notifications (`devices`, notification channel `PUSH`)
- [ ] [POST] iOS: Apple developer account, bundle id, TestFlight, App Store review
- [ ] [POST] Multi-currency (FX), shared accounts, bank import
- [ ] [POST] Full data export (`GET /v1/me/export`), Clerk webhooks

## Open questions (resolve before or early in Month 1)

- [ ] Licence/permission status for the Welth code
- [ ] Railway vs Render (pick one)
- [ ] TS client generator + Go helper libraries (Clerk SDK, cron parser, etc.): approve the dependency list
- [ ] Are transfers in MVP?
- [ ] Product name, domain, Android package id
