# Progress

Last updated: **2026-11-01**, branch `feat/go-api` (Go API session complete). Start every session by reading this file (`/status`).

## Session: wire mobile to the Go API (branch `feat/mobile-go-wiring`, 2026-11-01)

| Step | What | Status |
| --- | --- | --- |
| 0 | `@arth/api-client` (openapi-typescript + openapi-fetch, `pnpm gen:api`), money module + `pnpm check:money`, session core with one serialized refresh, mobile API layer, `EXPO_PUBLIC_API_URL` | Done |
| 1 | Own auth screens, secure-store tokens, session restore, logout / logout-all / change password, 401 handling | Not started |
| 2 | `/v1/me` + onboarding | Not started |
| 3 | Accounts + categories | Not started |
| 4 | Transactions | Not started |
| 5 | Budgets | Not started |
| 6 | Dashboard + reports | Not started |
| 7 | CSV export | Not started |
| 8 | Account deletion + privacy link | Not started |
| 9 | Receipt scan (server); voice + assistant hidden in release | Not started |
| 10 | Remove Clerk, Supabase, client Gemini, `EXPO_PUBLIC_BACKEND` | Not started |
| 11 | Local preview tooling (`dev:*`, seed, `docs/PREVIEW_LOCALLY.md`) | Not started |

## Phase / status

- **Phase 1 (mobile restructure): done** (previous session, merged into `main` as a fast-forward).
- **Phase 2 (Go API incl. own auth): done for MVP scope.** All 33 operations in `api/openapi.yaml` are implemented in `apps/api` and
  tested against real Postgres. Not built: transfers, password reset / email verification, worker jobs, per-user AI rate limit,
  idempotency keys, real providers (all blocked on OPEN items or post-MVP, see below).
- **Mobile still uses Clerk + Supabase + client-side Gemini.** Untouched this session; Phase 4 repoints it.
- **Next: Phase 3, the Next.js web app** (see "Next session" below).

## Done (this session, branch `feat/go-api`)

| Commit | What |
| --- | --- |
| `8707c60` | Scaffold: `cmd/api` (serve, migrate), `cmd/worker` stub, config + `.env` loader, slog, middleware, `/healthz`, `/readyz`, graceful shutdown, Dockerfile, Makefile, `scripts/dev.ps1`. Auth decisions recorded in DECISIONS.md |
| `c5c61a8` | Core libs: `money`, `auth` primitives (Argon2id + NFC, policy, HS256 JWT, refresh tokens, rate limiters), `provider/{ai,email,storage,push}` interfaces + fake drivers |
| `5e88e23` | DB: goose migrations (MVP schema + 21 categories), sqlc config + queries, `InTx`, schema tests |
| `575f876` | Contract: full OpenAPI 3.1 spec, oapi-codegen strict server, kin-openapi validation, auth middleware with per-request session check, testutil |
| `f48dca3` | Auth endpoints (register, login, refresh rotation + reuse detection, logout, logout-all, password, DELETE /v1/me) + AUTH.md §9 tests |
| `24dd7b5` | Me + onboarding |
| `396b473` | Accounts + categories |
| `3aedb3b` | Transactions (balance deltas in one DB transaction, keyset pagination) |
| `368abaf` | Budgets, dashboard, daily report |
| `5d3c035` | CSV export |
| `ad04a18` | Receipt extraction behind the AI provider interface (fake driver) |
| *(next commit)* | Spec fixes (PATCH /v1/me 409, logout / rate-limit texts) + docs: API, SCHEMA, MIGRATION, AUTH, DECISIONS, ROADMAP, PROGRESS, RUN_API_LOCALLY, api CLAUDE.md/README |

Note: the first three commits do not build on their own (the router and migration runner reference packages from later commits).
Every commit from `575f876` on was checked out in a temporary worktree and passes `go build ./...` + `go vet ./...`.

### Verification evidence (commands actually run)

| Check | Result |
| --- | --- |
| Tools | go1.26.5 windows/amd64, Docker 29.5.3, Postgres 16.14 (container `arth-postgres`), sqlc 1.30.0 (Docker), oapi-codegen v2.8.1 (`go tool`) |
| `go build ./...` / `go vet ./...` (apps/api) | exit 0 / exit 0 |
| `gofmt -l .` | no files |
| `go test -count=1 ./...` | **15 packages ok, 442 tests + subtests passed, 0 failed, 0 skipped** (149 top-level tests). Per package: apitest/auth 53, transactions 55, accounts 39, me 37, ai 29, budgets 28, dashboard 21, export 7; auth 66, httpapi 44, config 24, db 13, money 6, providers 20 |
| `go test -race` | auth slice + core libs pass with `CGO_ENABLED=1` (gcc from MSYS2) |
| Docker image | `docker build` ok, 29.4 MB distroless; container ran `migrate status`, `/readyz` ready, clean SIGTERM shutdown |
| `go run ./cmd/api migrate up` on the dev DB | `OK up 00001_init.sql`, `OK up 00002_seed_categories.sql` |
| Live smoke test (curl, git-bash) | register 201 → login 200 → wrong password 401 → onboarding 200 → create account 201 → create transaction 201 (balance 10000000 → 9954950) → dashboard 200 (`total_balance_minor` 10454950 = 500000 + 9954950). Every other endpoint returned its expected status (list in the session's final report) |
| Live auth checks | refresh 200; re-using the old refresh token 401 `refresh_invalid` and its successor also 401 (session revoked); password change 204; logout 204 then 401; logout-all 204 then 401; DELETE /v1/me wrong password 401, right 204, then 401 |
| Server log (61 lines) | 0 passwords, 0 Argon2 hashes, 0 emails, 0 JWTs; reuse security event present |
| `docs/RUN_API_LOCALLY.md` §5 | run as written in Windows PowerShell 5.1.26100 (Invoke-RestMethod). Piping JSON into `curl.exe` failed with 400 (BOM) and was replaced |

### I must test manually (not run in this session)

- [ ] Ctrl+C graceful shutdown of `go run ./cmd/api` in a native Windows console (tested only via Docker SIGTERM)
- [ ] `docs/RUN_API_LOCALLY.md` §2–§4 from a fresh clone (the `.env` generator ran in a scratch folder; the real `.env` files were created differently)
- [ ] The Docker run command in RUN_API_LOCALLY §7 with your own env file
- [ ] Any real AI/email/storage provider (none chosen)
- [ ] Mobile app against the new API (Phase 4)

## Next session: Phase 3, Next.js web (`apps/web`)

1. **Ask the owner first:** D-OPEN-6 (web token transport: (a) httpOnly cookies from the API, (b) in-memory access + httpOnly refresh cookie,
   (c) Next.js BFF). (a)/(b) need API changes (cookie variants of register/login/refresh, CSRF, CORS with credentials) **before** the web build.
   Also the UI kit + charts library (D-OPEN-10) and D-OPEN-5 (hosting) if deployment matters yet.
2. `packages/api-client`: generate types with **openapi-typescript** + client with **openapi-fetch** (decided) from `api/openapi.yaml`;
   add `formatMinor` / `parseMoneyInput` money helpers. Ask before adding the two packages to package.json (decided, but list the versions).
3. Scaffold `apps/web` (Next.js App Router) per `docs/WEB_APP.md`, screens from `docs/MOBILE_SCREENS.md`, against the local API (`docs/RUN_API_LOCALLY.md`).
4. Review the "decided by default" items below.

## What later sessions must pick up

- **Transfers** (`/v1/transfers`, MVP-stretch): schema + `DeleteTransferPeer` exist; account delete must then reverse transfer peers.
- **Worker** (D-OPEN-9 job library): budget alerts, weekly tips, balance reconcile; the budget-check enqueue TODO is in `server_transactions.go`.
- **Email** (D-OPEN-2, launch blocker): password reset, email verification, `auth_tokens` migration, unsubscribe endpoint.
- **Hardening:** per-user AI rate limit, `Idempotency-Key`, rate-limit storage for >1 instance (Q-10), DB roles `arth_migrator`/`arth_app`, X-Forwarded-For once a proxy exists.
- **Phase 4 (repoint mobile):** `docs/MIGRATION.md` (every legacy call now maps to an implemented endpoint or a "not built" reason).

## Decisions to review (decided by default this session)

See `docs/DECISIONS.md`: AUTH Q-1 (Argon2id params), Q-2, Q-4, Q-6, Q-7, Q-8, Q-10, and implementation choices D-19 … D-30
(INR placeholder for the default account, logout semantics, shared login buckets for wrong passwords, budget currency fixed at PUT time, etc.).

## Known problems

1. Commits `8707c60`, `c5c61a8`, `5e88e23` don't build individually (see above).
2. The per-request session check uses the DB clock (`now()`) while refresh expiry uses the app clock. Fine in production; tests that move the
   fake clock backwards get 401.
3. An impossible date in a query param (`2026-02-30`) returns 400 from the generated binder instead of 422; a non-multipart body on the receipt
   endpoint returns 400 instead of 415. Both documented in API.md.
4. No per-user AI rate limit yet; the fake AI driver ignores the image.
5. Rate limits are in memory (single instance only, Q-10). Five wrong current passwords on change-password also throttle login for that email (D-24).
6. `readyz` 503s are logged at ERROR on every failed probe (noisy if a load balancer polls a cold DB).
7. 405 responses carry no `Allow` header (chi limitation with a custom handler).
8. Category names, emoji and colours in `00002_seed_categories.sql` are copied from Welth (MIGRATION.md §7, licence D-OPEN-11).
9. A GitHub remote `origin` now exists although DECISIONS D-13 says git is local only; nothing was pushed. Update D-13 when you decide.
10. Earlier mobile problems still apply (`nodeLinker: hoisted`, Welth artwork, INR-only TransactionRow, sign-in second-factor dead end, …);
    see the previous version of this file in git (`git show 9b265e5:docs/PROGRESS.md`).
