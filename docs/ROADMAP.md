# Roadmap: 6-month MVP (Android first)

Window: **Nov 2026 → end of Apr 2027**. Phases follow the decided session order (`docs/DECISIONS.md` D-11). Target months are guides, not promises.
Tags: **[MVP]** must ship · **[POST]** after launch · **[STRETCH]** only if ahead · **[OPEN]** blocked on a decision.
Tick boxes only when a commit proves the work (`/update-docs`). Day-to-day detail lives in `docs/PROGRESS.md`.

Critical-path dates:

- **Email provider decided by end of Phase 2.** Password reset and alerts need it, and it blocks launch.
- **Closed test live with ≥ 12 testers by ~1 Mar 2027.** New personal Play developer accounts must run 14 continuous days of closed testing
  before applying for production. Verify the current policy when the account is created.
- **Launch target: late Apr 2027.**

---

## Phase 1: Mobile restructure ✅ (done 2026-10-31)

- [x] `git init -b main`, Welth baseline committed, work branch `feat/mobile-restructure`
- [x] Restored the 3 broken route files + tsconfig from upstream. tsc, lint and the Android bundle pass
- [x] Monorepo: `apps/mobile`, `legacy/`, placeholders (`apps/api`, `apps/web`, `packages/api-client`, `api/openapi.yaml`), docker-compose Postgres 16, pnpm workspace (hoisted)
- [x] Renamed to Arth (`fyi.akashbuilds.arth`, scheme `arth`)
- [x] Project memory: CLAUDE.md files, `.claude/` settings/commands/rules
- [x] Docs updated for own auth + provider interfaces. New: AUTH, DECISIONS, MOBILE_SCREENS, PROGRESS, RUN_LOCALLY

## Phase 2: Go API incl. auth (Nov → Dec 2026)

Foundation

- [ ] `apps/api` Go module, `cmd/api` + `cmd/worker`, config from env, slog, graceful shutdown, `/healthz` `/readyz`
- [ ] docker-compose Postgres up. goose baseline migration (`docs/SCHEMA.md` §3.1 incl. users/sessions/refresh_tokens), category seed
- [ ] sqlc setup, `internal/money` with table tests, `internal/apierr` (problem+json)
- [ ] `api/openapi.yaml` filled in for auth + me. oapi-codegen wired. TS client generator chosen [OPEN D-OPEN-10]
- [ ] Integration-test harness against real Postgres (users per test, "other user gets 404" helper)

Auth [MVP]

- [ ] Argon2id hasher, token issuer/verifier, auth middleware (decide AUTH.md Q-1..Q-5, Q-8, Q-10, Q-12 first)
- [ ] `POST /v1/auth/register|login|refresh|logout|logout-all|password`, rate limits
- [ ] AUTH.md §9 checklist tests all green
- [ ] [OPEN D-OPEN-2] verify-email + password-reset flows (email interface + `log` driver now; real adapter after the decision)

Domain [MVP]

- [ ] `GET/PATCH/DELETE /v1/me`, `POST /v1/me/onboarding`
- [ ] Categories, accounts (CRUD, make-default, two-step delete)
- [ ] Transactions (keyset list, filters, `q`, get, create, patch, delete) with the balance invariant tested, including concurrency
- [ ] Budgets (overall), dashboard, daily report, CSV export
- [ ] [STRETCH] Transfers
- [ ] `POST /v1/ai/receipt-extractions` behind `ai.ReceiptExtractor` (fake driver) [OPEN D-OPEN-1 for the real one]
- [ ] Worker: job queue chosen [OPEN D-OPEN-9]; budget alerts, weekly tips (fake AI + log email), balance reconcile

## Phase 3: Next.js web (Jan 2027)

- [ ] `apps/web` scaffold with `@arth/api-client` + TanStack Query. Auth transport decided [OPEN D-OPEN-6]
- [ ] [MVP] Every screen in `docs/MOBILE_SCREENS.md`: auth, onboarding, dashboard, transactions (+ export), add/edit transaction, accounts, budget, settings
- [ ] [MVP] Public pages: privacy, terms, delete-account (Play requirement), unsubscribe
- [ ] Hosting decided [OPEN D-OPEN-5]

## Phase 4: Repoint mobile + drop Clerk/Supabase (Feb 2027)

- [ ] AuthProvider + secure-store tokens. Register/login/logout screens on the API (UI kept)
- [ ] Slice by slice (`docs/MIGRATION.md` §5): me/onboarding → accounts → transactions → budget → dashboard → export → receipt scan
- [ ] Money helpers replace `parseFloat`. `occurred_on` dates. Transaction rows use the user's currency (fix MIGRATION §6.5)
- [ ] Remove `@clerk/expo`, `@supabase/supabase-js`, the client Gemini code and the `EXPO_PUBLIC_*` legacy vars. **Revoke the old Gemini key**
- [ ] Voice + assistant hidden in release builds [POST until their endpoints exist]
- [ ] Delete `legacy/`

## Phase 5: Android emulator testing (Feb → early Mar 2027)

- [ ] Full manual pass on an emulator (`docs/RUN_LOCALLY.md`): every screen and state in `docs/MOBILE_SCREENS.md`
- [ ] Dev client build via EAS (`eas.json` profiles). Real-device check
- [ ] Bug-fix loop. `pnpm verify:mobile` + `go test ./...` green

## Phase 6: Hardening (Mar 2027)

- [ ] Production hosting stood up [OPEN D-OPEN-3, D-OPEN-4]: backups/PITR verified with a test restore
- [ ] Real providers wired (LLM, email) per decisions. Secrets only in host env
- [ ] Security pass: authz tests on every endpoint, rate limits, upload limits, headers, `govulncheck`, `pnpm audit`
- [ ] Idempotency-Key on POST transactions/AI
- [ ] Rebrand artwork + provenance checklist cleared (`docs/MIGRATION.md` §7) [OPEN D-OPEN-11]
- [ ] Error tracking / uptime [OPEN D-OPEN-10]
- [ ] Performance: 50k transactions for one user, p95 list/dashboard < 200 ms

## Phase 7: Play closed test (Mar → Apr 2027)

- [ ] Play Console account + app. Data safety, privacy policy URL, account-deletion URL, content rating, financial-features declaration
- [ ] Package id confirmed (D-1). EAS production build. `eas submit` to the closed track
- [ ] ≥ 12 testers opted in for ≥ 14 continuous days. Feedback fixed

## Phase 8: Launch (late Apr 2027)

- [ ] Apply for production access. Store listing (screenshots, description, support contact)
- [ ] Staged rollout 10% → 50% → 100% with monitoring
- [ ] Web production on the custom domain

## Post-MVP backlog

- [ ] [POST] Voice entry · AI assistant · per-category budget UI · recurring transactions · goals
- [ ] [POST] Receipt attachments + avatars (object storage, D-OPEN-7) · push notifications (D-OPEN-8)
- [ ] [POST] MFA/passkeys (AUTH.md Q-9) · session list UI · data export
- [ ] [POST] iOS (Apple developer account, TestFlight) · multi-currency · shared accounts · bank import
