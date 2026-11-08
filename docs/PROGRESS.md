# Progress

Last updated: **2026-11-01**, branch `feat/mobile-go-wiring` (mobile repoint complete; not pushed). Start every session by reading this file (`/status`).

## Session: wire mobile to the Go API (branch `feat/mobile-go-wiring`, 2026-11-01)

| Step | What | Status |
| --- | --- | --- |
| 0 | `@arth/api-client` (openapi-typescript + openapi-fetch, `pnpm gen:api`), money module + `pnpm check:money`, session core with one serialized refresh, mobile API layer, `EXPO_PUBLIC_API_URL` | Done |
| 1 | Own auth screens, secure-store tokens, session restore, logout / logout-all / change password, 401 handling | Done |
| 2 | `/v1/me` + onboarding | Done |
| 3 | Accounts + categories | Done |
| 4 | Transactions | Done |
| 5 | Budgets | Done (progress shown on Home in step 6) |
| 6 | Dashboard + reports | Done (daily report landed with step 4) |
| 7 | CSV export | Done |
| 8 | Account deletion + privacy link | Done |
| 9 | Receipt scan (server); voice + assistant hidden in release | Done |
| 10 | Remove Clerk, Supabase, client Gemini, `EXPO_PUBLIC_BACKEND` | Done |
| 11 | Local preview tooling (`dev:*`, seed, `docs/PREVIEW_LOCALLY.md`) | Done (kept in sync with later steps) |

Commits (oldest first): `8baf22e` step 0 · `dd2c5b8` step 1 · `2a4841b` step 2 · `aeedb22` step 3 · `267f21f` step 11 · `d0b4ed5` step 4 ·
`0632fd3` step 5 · `7f2ce76` step 6 · `fabc436` step 7 · `454edc5` step 8 · `6560d7c` step 9 · `e54c09e` step 10 · then this docs commit.
`main` already contained `feat/go-api` (merge `310ed18`), so no merge was needed before branching.

**The Go API was not changed** (`git diff main -- apps/api api/` is empty): no API bug was found.

### How the temporary `EXPO_PUBLIC_BACKEND` switch worked (steps 1–9, deleted in step 10)

It selected the auth system at the root (Clerk provider + Clerk screens vs own auth). Data slices were moved to the API one by one and
were API-only once moved; screens not yet moved kept their legacy hooks under `hooks/legacy/` so every commit compiled. `supabase` mode was
never run (no Clerk/Supabase credentials on this PC) and is gone now.

### Verification evidence (commands actually run, final state)

| Check | Result |
| --- | --- |
| Tools | Node v24.2.0, pnpm 10.34.9, go1.26.5 windows/amd64, Docker 29.5.3 (Desktop had to be started), Postgres 16 container `arth-postgres` |
| Android | **No Android SDK / adb / emulator on this PC** (`adb` not found, no `%LOCALAPPDATA%\Android\Sdk`, no Java). Nothing ran on a device |
| `pnpm verify:mobile` (tsc + eslint + `expo export --platform android`) | exit 0 after every step; final: tsc clean, **eslint 0 problems** (the 2 legacy warnings went with the deleted files), release bundle 7.24 MB (was 9.26 MB with Clerk/Supabase) |
| Dev bundle | `npx expo export --platform android --dev` → 2420 modules bundled |
| `npx expo install --check` | "Dependencies are up to date" |
| `pnpm check:money` | 16 money checks passed ("450.50" INR → 45050, float traps like 0.29, JPY/KWD exponents, Indian grouping, round-trips for all 33 currencies) |
| `pnpm --filter @arth/api-client typecheck` | exit 0 |
| `pnpm --filter @arth/api-client live-check` (against the running local API) | 22 steps passed: register → login → wrong password 401 → `/v1/me` → "450.50" → 45050 → accounts (create/PATCH/make-default/default 409/`transaction_count` 409/cascade) → transactions (2 keyset pages, `q`, PATCH, DELETE restores balance, daily report) → budgets → dashboard totals = Σ balances → CSV export → receipt upload 200 + text/plain 415 → `currency_locked` → **5 parallel calls with an expired token = 1 refresh; 6 parallel 401s = 1 refresh, each retried once, session survives** → negative control: 2 unserialized refreshes → 200 + 401 and the session is revoked → refused refresh clears tokens → password change → logout → logout-all → DELETE /v1/me (wrong 401, right 204) |
| `pnpm dev:api` | `go -C apps/api run ./cmd/api migrate up` → "no pending migrations", then "api listening addr=:8080" |
| `pnpm dev:seed` | demo user + 3 accounts + 61 transactions + overall and 4 category budgets; dashboard total ₹3,39,738.10, this month expense ₹11,554.51 |
| DB spot check | `amount_minor 45050, occurred_on 2026-11-01, "Groceries (₹450.50 check)"` for the demo user |
| `go build ./...`, `go vet ./...` (apps/api) | ok (unchanged code) |
| Release bundle grep | no Gemini URL, no `EXPO_PUBLIC_GEMINI` in the bytecode |

### I must test manually (could not run here)

- [ ] Install the Android SDK + an emulator (`docs/RUN_LOCALLY.md` §1), then the whole of `docs/PREVIEW_LOCALLY.md` §2 and the §7 checklist
- [ ] Session restore after killing the app; silent refresh after > 15 min idle; sign-out redirects (route guards on device)
- [ ] expo-secure-store on a real Android Keystore (only typechecked/bundled here)
- [ ] Receipt scan with the camera and the gallery (RN `FormData` file upload `{uri, name, type}`: verified from Node with a Blob, not from RN)
- [ ] CSV export share sheet (expo-file-system `File` + expo-sharing)
- [ ] Edit screen back navigation (the `(root)` group became a `Stack`); tab bar without the Assistant tab in a release build
- [ ] The look of the new rows/sheets (Change password, Delete account, Privacy policy, budget scope pills, per-category budget rows)
- [ ] Physical phone over LAN (firewall rule) and `adb reverse` variant
- [ ] **Revoke the old Gemini key** in Google AI Studio (it shipped in old bundles)

## Phase / status

- **Phase 1 (mobile restructure): done** (previous session, merged into `main` as a fast-forward).
- **Phase 2 (Go API incl. own auth): done for MVP scope.** All 33 operations in `api/openapi.yaml` are implemented in `apps/api` and
  tested against real Postgres. Not built: transfers, password reset / email verification, worker jobs, per-user AI rate limit,
  idempotency keys, real providers (all blocked on OPEN items or post-MVP, see below).
- **Phase 4 (repoint mobile + drop Clerk/Supabase): done** (branch `feat/mobile-go-wiring`, ran before web: D-31). Emulator pass pending (Phase 5).
- **Next: Phase 3, the Next.js web app** (see "Next session" below).

## Done (Go API session, branch `feat/go-api`, merged)

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
2. `packages/api-client` **exists** (openapi-typescript 7.13 + openapi-fetch 0.17, money helpers, session core). Reuse it from `apps/web`;
   if D-OPEN-6 picks bearer tokens, plug an in-memory/cookie storage into `createSession`; if cookies/BFF, only the typed client is reused.
3. Scaffold `apps/web` (Next.js App Router) per `docs/WEB_APP.md`, screens from `docs/MOBILE_SCREENS.md`, against the local API (`docs/RUN_API_LOCALLY.md`).
4. Review the "decided by default" items below.

## What later sessions must pick up

- **Transfers** (`/v1/transfers`, MVP-stretch): schema + `DeleteTransferPeer` exist; account delete must then reverse transfer peers.
- **Worker** (D-OPEN-9 job library): budget alerts, weekly tips, balance reconcile; the budget-check enqueue TODO is in `server_transactions.go`.
- **Email** (D-OPEN-2, launch blocker): password reset, email verification, `auth_tokens` migration, unsubscribe endpoint.
- **Hardening:** per-user AI rate limit, `Idempotency-Key`, rate-limit storage for >1 instance (Q-10), DB roles `arth_migrator`/`arth_app`, X-Forwarded-For once a proxy exists.
- **Phase 5 (emulator testing):** `docs/PREVIEW_LOCALLY.md` §7 checklist on a device; then EAS dev build.
- **Delete `legacy/`** once the worker jobs are ported (nothing imports it).

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
10. Mobile (after the repoint), still open: `nodeLinker: hoisted` required; Welth artwork; the biometric switch is UI only; avatars
    unavailable (storage OPEN); unused `react-native-linear-gradient` (MIGRATION §6.13). **Fixed by the repoint:** INR-only TransactionRow,
    sign-in second-factor dead end, client AI key, non-atomic balance writes, float money, `toISOString()` date drift, starting balance as
    income, every screen loading every transaction, the two hook-dependency lint warnings.
11. Behaviour changes to know (MOBILE_SCREENS §12): search matches descriptions only (server `q`), export ignores the type filter and
    search, new accounts always use the user's currency (no picker; multi-currency is post-MVP), budget currency follows D-26.
12. A refresh that fails on the network keeps the session (D-32); a user who is offline at start sees the "Can't reach the server" retry
    screen rather than the sign-in screen.
13. The API's in-memory rate limits are easy to hit while developing (register 5/h, login 20/15 min per IP). Restart `pnpm dev:api`.
14. The Node check scripts need Node ≥ 22.18 (type stripping) although `package.json` engines say ≥ 20.19.4.
