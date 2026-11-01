# Migration: Welth stack (Clerk + Supabase + client Gemini) → Arth (own Go API + Postgres)

This is the inventory of every place the mobile app talks to Clerk, Supabase or Gemini, and where each one goes.
**Paths in tables are relative to `apps/mobile/` unless they start with `legacy/` or `apps/api/`.** Line numbers were checked against
branch `feat/mobile-restructure` (2026-10-31). Endpoint shapes are summaries; `api/openapi.yaml` is the contract (`docs/API.md` is the overview).

Legend: **MVP** needed for launch · **POST** post-MVP · **OPEN** blocked on a provider decision (`docs/DECISIONS.md`) ·
`uid` = authenticated user id from the access token.

**Go endpoint status** column: the endpoint and the handler that serves it (`apps/api/internal/httpapi/server_*.go`, method name =
the generated strict-server operation). `implemented (Go API session, 2026-11-01)` = the operation is in `api/openapi.yaml` and has a
handler (the lead verifies with tests at the end of the session). `not built (reason)` = deliberately left out (`docs/DECISIONS.md` D-21).
`n/a` = client-side only, nothing to build on the server.

**Status (2026-11-01, branch `feat/mobile-go-wiring`): the client side of §1–§3 is DONE.** Every Clerk, Supabase and Gemini call in
`apps/mobile` is gone (the file:line references below point at the pre-repoint code, `git show main:apps/mobile/...`). The repoint ran
before the web app (D-31). Still open: the worker jobs (§4) and deleting `legacy/` (nothing imports it).

---

## 0. History of the baseline repair (done)

The ZIP copy had `app/_layout.tsx`, `app/index.tsx` and `app/(root)/_layout.tsx` overwritten with copies of other route files, and
`tsconfig.json` replaced by the Deno one. They were restored from upstream `piyush-eon/react-native-finance-platform@9e52cfb`
(commit `621e15a`). All other files matched upstream exactly. Upstream has no LICENSE either.

---

## 1. Clerk (authentication) → own auth in Go (`docs/AUTH.md`)

| # | Current file:line | What it does | Target | Notes | Go endpoint status |
| --- | --- | --- | --- | --- | --- |
| K1 | `app/_layout.tsx:2-3, 9-13, 19-21` | `ClerkProvider` with publishable key + `tokenCache` (expo-secure-store) | Own `AuthProvider` (React context) holding the session; tokens in `expo-secure-store` | Client-only change. `EXPO_PUBLIC_CLERK_PUBLISHABLE_KEY` removed. | n/a (client-side; tokens come from K6/K9) |
| K2 | `app/index.tsx:5-11` | `useAuth().isSignedIn` → redirect to tabs or `/sign-in` | `AuthProvider.status` (`loading \| signedIn \| signedOut`) | Signed in = a valid refresh token exists and `GET /v1/me` succeeds. | `GET /v1/me` → `apps/api/internal/httpapi/server_me.go` (`GetMe`): implemented (Go API session, 2026-11-01) |
| K3 | `app/(auth)/_layout.tsx:5-9` | Redirect signed-in users away from auth screens | same as K2 | | same as K2 |
| K4 | `app/(root)/_layout.tsx:9, 21-22` | Auth gate for the app group | same as K2 | | same as K2 |
| K5 | `app/(root)/_layout.tsx:14, 23-28` | Runs `useUserSync`, waits for `needsOnboarding`, redirects to onboarding | `GET /v1/me` → `needs_onboarding` | The 1.5 s minimum splash (`:16-19`) is UI and can stay. | `GET /v1/me` → `apps/api/internal/httpapi/server_me.go` (`GetMe`): implemented (Go API session, 2026-11-01) |
| K6 | `app/(auth)/sign-in.tsx:42-56` | `signIn.password({emailAddress, password})` then `finalize` | `POST /v1/auth/login {email, password, client}` → `TokenPair {access_token, access_expires_in, refresh_token, refresh_expires_at, user}` | **MVP.** Generic error on failure (`401 invalid_credentials`). Rate-limited. | `POST /v1/auth/login` → `apps/api/internal/httpapi/server_auth.go` (`Login`): implemented (Go API session, 2026-11-01) |
| K7 | `app/(auth)/sign-in.tsx:57-58` | `needs_second_factor` → `mfa.sendPhoneCode()` | none in MVP | There is no UI to enter the phone code today (dead end). MFA is **POST/OPEN**. | not built (MFA is post-MVP, DECISIONS Q-9) |
| K8 | `app/(auth)/sign-in.tsx:59-65, 71-85, 89-156` | `needs_client_trust` → email code for a new device, verify screen | none in MVP | Clerk's new-device check. Equivalent flows need email (**OPEN**). | not built (needs the email provider, D-OPEN-2) |
| K9 | `app/(auth)/sign-up.tsx:47-63` | `signUp.password({email, password, firstName, lastName})` then `sendEmailCode` | `POST /v1/auth/register {email, password, name, client}` → `201 TokenPair` | **MVP.** `name` = first + last (required, 1–100). Email verification is **OPEN**. | `POST /v1/auth/register` → `apps/api/internal/httpapi/server_auth.go` (`Register`): implemented (Go API session, 2026-11-01) |
| K10 | `app/(auth)/sign-up.tsx:65-79, 85-159, 147` | Verify-email code screen + resend | `POST /v1/auth/verify-email {token}`, `POST /v1/auth/verify-email/resend` | **OPEN** (email provider). Until then, accounts are created unverified. | not built (email verification needs D-OPEN-2; D-21) |
| K11 | `app/(auth)/sign-up.tsx:20, 81-83` | `isSignedIn` short-circuit after sign-up | AuthProvider | | n/a (client-side) |
| K12 | `app/(root)/(tabs)/profile.tsx:84, 141-153` | `signOut()` then `router.replace("/sign-in")` | `POST /v1/auth/logout {refresh_token}` + clear secure store | **MVP.** `POST /v1/auth/logout-all` revokes every session. | `POST /v1/auth/logout` → `apps/api/internal/httpapi/server_auth.go` (`Logout`); `POST /v1/auth/logout-all` → same file (`LogoutAll`): implemented (Go API session, 2026-11-01) |
| K13 | `app/(root)/(tabs)/profile.tsx:103-139` (`:132` `user.setProfileImage`) | Avatar upload to Clerk (base64 data URL) | `PUT /v1/me/avatar` (multipart) | **OPEN** (object storage). MVP fallback: initials/icon avatar (the UI already falls back to an icon). | not built (needs object storage, D-OPEN-7; D-21). `Me.avatar_url` is always null |
| K14 | `app/(root)/(tabs)/index.tsx:155, 162-164`; `profile.tsx:205-207, 220, 228` | Reads `user.firstName`, `imageUrl`, `hasImage`, `lastName`, `emailAddresses[0]` | `GET /v1/me` → `{name, email, avatar_url}` | `name` may be null. | `GET /v1/me` → `apps/api/internal/httpapi/server_me.go` (`GetMe`): implemented (Go API session, 2026-11-01) |
| K15 | `hooks/useSupabase.ts:6-10`; `lib/supabase.ts:19-27` | `getToken()` from Clerk attached to Supabase calls | `@arth/api-client` with `getAccessToken()` + refresh-on-401 | Refresh and retry only on code `unauthorized` (`docs/API.md` §1). | `POST /v1/auth/refresh` → `apps/api/internal/httpapi/server_auth.go` (`RefreshToken`): implemented (Go API session, 2026-11-01) |
| K16 | `user.id` passed as `user_id`: `hooks/queries/useAccountsQuery.ts:8,13`, `useBudgetQuery.ts:8,13`, `useTransactionsQuery.ts:8,13`, `hooks/mutations/useAccountMutations.ts:14,19,66,71`, `useBudgetMutations.ts:8,12`, `app/(root)/(tabs)/add-transaction.tsx:59,171`, `app/(root)/onboarding.tsx:23,55,66,77`, `profile.tsx:177`, `hooks/useUserSync.ts:20,41,62` | Client tells the backend which user it is | **Removed.** The server derives `uid` from the token | Query keys use `me.id`. | n/a (the server takes `uid` only from the access token; no request carries `user_id`) |
| K17 | `lib/schemas/auth.ts:3-23` | zod: sign-up (names, email, password ≥ 8), sign-in, code | Keep, aligned with the server password policy (`docs/AUTH.md` §4) | | n/a (client-side; the server policy is `apps/api/internal/auth/policy.go`) |
| K18 | `app.json:68` plugin `@clerk/expo`; `package.json:15` `@clerk/expo` | Native config + dependency | Remove in the repoint session | `expo-secure-store` (`app.json:47`, `package.json:41`) stays and is reused for tokens. | n/a (client-side, session 4) |

## 2. Supabase (data) → Go API endpoints

### 2.1 Client setup

| # | Current file:line | What it does | Target | Go endpoint status |
| --- | --- | --- | --- | --- |
| C1 | `lib/supabase.ts:3-14` | Reads `EXPO_PUBLIC_SUPABASE_URL/KEY`, creates an unused anon client | Delete. `EXPO_PUBLIC_API_BASE_URL` instead | n/a (client-side) |
| C2 | `lib/supabase.ts:19-27` | Client with Clerk token (for RLS) | Delete. `createApiClient({ baseUrl, getAccessToken, onRefresh })` | n/a (client-side; TS client = openapi-typescript + openapi-fetch, web session) |
| C3 | `hooks/useSupabase.ts:5-13` | Memoised client | Replace with `hooks/useApi.ts` | n/a (client-side) |

### 2.2 Users / onboarding / profile

| # | Current file:line | What it does | New endpoint | Request → Response | Auth rule | Notes | Go endpoint status |
| --- | --- | --- | --- | --- | --- | --- | --- |
| U1 | `hooks/useUserSync.ts:17-21` | Select own user row (`clerk_id`, `currency`) | `GET /v1/me` | → `Me {id, email, email_verified, name\|null, avatar_url\|null, currency\|null, timezone, needs_onboarding, email_budget_alerts, email_weekly_tips, created_at}` | token | **MVP** | `GET /v1/me` → `apps/api/internal/httpapi/server_me.go` (`GetMe`): implemented (Go API session, 2026-11-01) |
| U2 | `hooks/useUserSync.ts:35-49` | Upsert user with client-supplied email/name/image | *(gone)* | — | — | `register` creates the user. The email comes from the registration form and is the login identity. | `POST /v1/auth/register` → `apps/api/internal/httpapi/server_auth.go` (`Register`): implemented (Go API session, 2026-11-01) |
| U3 | `hooks/useUserSync.ts:60-62` | Insert default "Cash" account | *(in register)* | — | — | Same DB tx as the user insert. Currency INR as a placeholder until onboarding (D-19). | `POST /v1/auth/register` → `apps/api/internal/httpapi/server_auth.go` (`Register`): implemented (Go API session, 2026-11-01) |
| U4–U7 | `app/(root)/onboarding.tsx:50-55, 63-68, 76-85, 93-96` | Update currency → find default account → insert "Starting balance" INCOME → update balance (4 calls, not atomic) | `POST /v1/me/onboarding` | `{currency, timezone, opening_balance_minor}` → `Me` | token | **MVP.** One tx. The starting balance becomes `accounts.opening_balance_minor`, not income. `409 already_onboarded`; `409 currency_locked` if the default account already has transactions. | `POST /v1/me/onboarding` → `apps/api/internal/httpapi/server_me.go` (`CompleteOnboarding`): implemented (Go API session, 2026-11-01) |
| U8 | `app/(root)/(tabs)/profile.tsx:174-177` | Update currency | `PATCH /v1/me` | `{name?, currency?, timezone?, email_budget_alerts?, email_weekly_tips?}` → `Me` | token | **MVP.** Changing the default currency does not change existing accounts. Once the user has any transaction → `409 currency_locked` (declared in the spec since commit `119dc14`; the app shows a message). | `PATCH /v1/me` → `apps/api/internal/httpapi/server_me.go` (`UpdateMe`): implemented (Go API session, 2026-11-01) |
| U9 | *(new)* | — | `DELETE /v1/me` | `{password}` → `204` | token + password re-check | **MVP** (Play requires account deletion). Cascades all data, revokes all sessions. Wrong password → `401 invalid_credentials`. | `DELETE /v1/me` → `apps/api/internal/httpapi/server_auth.go` (`DeleteMe`): implemented (Go API session, 2026-11-01) |
| U10 | `profile.tsx:89, 285` | Biometric switch (UI state only, never persisted) | none | — | — | Device-local feature (needs `expo-local-authentication`, ask first) or remove. | n/a (device-local) |

### 2.3 Accounts

| # | Current file:line | What it does | New endpoint | Request → Response | Notes | Go endpoint status |
| --- | --- | --- | --- | --- | --- | --- |
| A1 | `lib/services/accounts.ts:8-13` | List own accounts (default first, then created) | `GET /v1/accounts` | → `{data: Account[]}` | **MVP.** `Account {id, name, type, currency, balance_minor, opening_balance_minor, is_default, created_at}` | `GET /v1/accounts` → `apps/api/internal/httpapi/server_accounts.go` (`ListAccounts`): implemented (Go API session, 2026-11-01) |
| A2 | `lib/services/accounts.ts:24-28` | Insert account, balance 0 | `POST /v1/accounts` | `{name, type, currency?, opening_balance_minor?}` → `201 Account` | **MVP.** `currency` defaults to `users.currency` (INR if none yet). | `POST /v1/accounts` → `apps/api/internal/httpapi/server_accounts.go` (`CreateAccount`): implemented (Go API session, 2026-11-01) |
| A3 | `lib/services/accounts.ts:42-53` | Two sequential updates to switch default (second filters only by id) | `POST /v1/accounts/{id}/make-default` | → `Account` | **MVP.** One tx. A unique partial index guarantees one default. | `POST /v1/accounts/{id}/make-default` → `apps/api/internal/httpapi/server_accounts.go` (`MakeDefaultAccount`): implemented (Go API session, 2026-11-01) |
| A4 | `lib/services/accounts.ts:61-66` | Update name/type by id only | `PATCH /v1/accounts/{id}` | `{name?, type?}` → `Account` | **MVP.** `WHERE id AND user_id`, else 404. | `PATCH /v1/accounts/{id}` → `apps/api/internal/httpapi/server_accounts.go` (`UpdateAccount`): implemented (Go API session, 2026-11-01) |
| A5 | `lib/services/accounts.ts:80-83` | Count transactions before deleting | `DELETE /v1/accounts/{id}` | → `204`, or `409 account_has_transactions {transaction_count}` | **MVP.** Same two-step UX as `components/AccountModal.tsx:73-106`. | `DELETE /v1/accounts/{id}` → `apps/api/internal/httpapi/server_accounts.go` (`DeleteAccount`): implemented (Go API session, 2026-11-01) |
| A6 | `lib/services/accounts.ts:92-95` | Delete account (cascade) | `DELETE /v1/accounts/{id}?cascade=true` | → `204` | **MVP.** Deleting the default account → `409 default_account`. | `DELETE /v1/accounts/{id}?cascade=true` → `apps/api/internal/httpapi/server_accounts.go` (`DeleteAccount`): implemented (Go API session, 2026-11-01) |

### 2.4 Transactions

| # | Current file:line | What it does | New endpoint | Request → Response | Notes | Go endpoint status |
| --- | --- | --- | --- | --- | --- | --- |
| T1 | `lib/services/transactions.ts:23-28` | Select **all** own transactions, optional `type`/`account_id` | `GET /v1/transactions` | `?type&account_id&category_id&from&to&q&limit&cursor` → `{data, next_cursor}` | **MVP.** Keyset pagination. Search moves server-side (today `app/(root)/(tabs)/transactions.tsx:73-81`). | `GET /v1/transactions` → `apps/api/internal/httpapi/server_transactions.go` (`ListTransactions`): implemented (Go API session, 2026-11-01) |
| T2–T4 | `lib/services/transactions.ts:42-46, 50-54, 60-63` | Insert (client sends `user_id`; account ownership unchecked) → read balance → write `balance + delta` | `POST /v1/transactions` | `{account_id, category_id, type, amount_minor, occurred_on, description?, input_method?, voice_transcript?}` → `201 {transaction, account}` | **MVP.** One tx with a relative balance update. Caller: `app/(root)/(tabs)/add-transaction.tsx:170-180`. | `POST /v1/transactions` → `apps/api/internal/httpapi/server_transactions.go` (`CreateTransaction`): implemented (Go API session, 2026-11-01) |
| T5–T7 | `lib/services/transactions.ts:79-82, 86-90, 96-99` | Delete by id (client passes amount/type/account) → read → write reversed balance | `DELETE /v1/transactions/{id}` | → `204` | **MVP.** The server reads the delta from the row. Caller: `transactions.tsx:119-137`. | `DELETE /v1/transactions/{id}` → `apps/api/internal/httpapi/server_transactions.go` (`DeleteTransaction`): implemented (Go API session, 2026-11-01) |
| T8 | *(new)* | — | `GET /v1/transactions/{id}`, `PATCH /v1/transactions/{id}` | partial create body → `{transaction, accounts[]}` | **MVP** (an edit screen is expected; none exists today). | `GET /v1/transactions/{id}` → `apps/api/internal/httpapi/server_transactions.go` (`GetTransaction`); `PATCH` → same file (`UpdateTransaction`): implemented (Go API session, 2026-11-01) |
| T9 | *(new)* | — | `POST /v1/transfers`, `DELETE /v1/transfers/{transfer_id}` | `{from_account_id, to_account_id, amount_minor, occurred_on}` | MVP-stretch. | not built (stretch goal, kept out of the spec, D-21) |
| T10 | `constants/categories.ts:1-150` (static list of 21) | Keys, labels, emoji, colours | `GET /v1/categories` | → `{data: Category[]}` | **MVP.** Seeded from the same keys (`apps/api/db/migrations/00002_seed_categories.sql`). | `GET /v1/categories` → `apps/api/internal/httpapi/server_accounts.go` (`ListCategories`): implemented (Go API session, 2026-11-01) |

### 2.5 Budget

| # | Current file:line | What it does | New endpoint | Request → Response | Notes | Go endpoint status |
| --- | --- | --- | --- | --- | --- | --- |
| B1 | `lib/services/budgets.ts:8-12` | Get the single budget | `GET /v1/budgets?month=YYYY-MM` | → `{data: BudgetProgress[]}` | **MVP.** Overall budget = `category_id: null`. | `GET /v1/budgets` → `apps/api/internal/httpapi/server_budgets.go` (`ListBudgets`): implemented (Go API session, 2026-11-01) |
| B2 | `lib/services/budgets.ts:23-27` | Upsert on `user_id` (caller `components/BudgetModal.tsx:40`) | `PUT /v1/budgets/overall` | `{amount_minor}` → `BudgetProgress` | **MVP** | `PUT /v1/budgets/overall` → `apps/api/internal/httpapi/server_budgets.go` (`PutOverallBudget`): implemented (Go API session, 2026-11-01) |

### 2.6 Client-side aggregates → server

| # | Current file:line | What it does | New endpoint | Notes | Go endpoint status |
| --- | --- | --- | --- | --- | --- |
| D1 | `app/(root)/(tabs)/index.tsx:85-130` | Total balance, month income/expense, top-5 recent, category breakdown from the full list | `GET /v1/dashboard?month=YYYY-MM` | **MVP.** Spec in `docs/MOBILE_SCREENS.md` §Home. | `GET /v1/dashboard` → `apps/api/internal/httpapi/server_budgets.go` (`GetDashboard`): implemented (Go API session, 2026-11-01) |
| D2 | `app/(root)/(tabs)/transactions.tsx:83-101` | Daily income vs expense bars, current month | `GET /v1/reports/daily?from&to&account_id?` | **MVP** | `GET /v1/reports/daily` → `apps/api/internal/httpapi/server_budgets.go` (`GetDailyReport`): implemented (Go API session, 2026-11-01) |
| D3 | `lib/utils/exportTransactions.ts:41-64` (caller `transactions.tsx:103-117`) | CSV of the last 30 days from memory, shared via expo-sharing | `GET /v1/transactions/export?format=csv&from&to` | **MVP.** Mobile downloads it (with the auth header) to cache, then shares. | `GET /v1/transactions/export` → `apps/api/internal/httpapi/server_transactions.go` (`ExportTransactions`): implemented (Go API session, 2026-11-01) |

## 3. Gemini (AI) → API endpoints behind a provider interface

The LLM vendor is **OPEN** (`docs/DECISIONS.md`). Endpoints are vendor-neutral. Go code calls `ai.ReceiptExtractor` / `ai.VoiceExtractor` /
`ai.Assistant` interfaces with a `fake` driver until a vendor is chosen. Only `ai.ReceiptExtractor` exists so far (`apps/api/internal/provider/ai`).

| # | Current file:line | What it does | New endpoint | Request → Response | Notes | Go endpoint status |
| --- | --- | --- | --- | --- | --- | --- |
| G1 | `lib/services/extractTransaction.ts:9-10, 29-60` | `fetch(…gemini-3.1-flash-lite:generateContent?key=EXPO_PUBLIC_GEMINI_API_KEY)` with a JSON response schema | (internal) `internal/provider/ai` | — | The key is in the bundle and the URL. **Rotate/revoke it** when the client code is removed. | n/a (no endpoint; `apps/api/internal/provider/ai` interface with `fake`/`none` drivers; real vendor D-OPEN-1) |
| G2 | `lib/services/extractTransaction.ts:62-77`; called at `app/(root)/(tabs)/add-transaction.tsx:136`; image from `components/ReceiptScannerModal.tsx:31-35` (camera) / `:48-56` (library) | Receipt photo → `{type, amount, category, description, date}` | `POST /v1/ai/receipt-extractions` | multipart `file` (jpeg/png/webp/heic, ≤ 5 MB) → `Extraction {type, amount_minor, currency, category_id, category_key, description, occurred_on, transcript}` (nullable fields except `currency`) | **MVP, OPEN.** Not persisted. The client pre-fills the form (`add-transaction.tsx:103-130`). Amounts are parsed as decimal strings. | `POST /v1/ai/receipt-extractions` → `apps/api/internal/httpapi/server_ai.go` (`ExtractReceipt`): implemented (Go API session, 2026-11-01), fake AI driver only until D-OPEN-1 |
| G3 | `lib/services/extractTransaction.ts:79-95`; called at `components/VoiceRecorderModal.tsx:166-168` (m4a) | Voice note → same + transcript | `POST /v1/ai/voice-extractions` | multipart `file` (≤ 60 s) → `Extraction + transcript` | **POST, OPEN** | not built (voice is post-MVP, D-21) |
| G4 | `lib/services/assistant.ts:10-74` (context), `:76-111` (call); called at `app/(root)/(tabs)/assistant.tsx:82` | Q&A over the last 30 days of client-held data | `POST /v1/assistant/messages` | `{message, history?}` → `{reply}` | **POST, OPEN.** The server builds the context from SQL aggregates. | not built (assistant is post-MVP, D-21) |
| G5 | `legacy/supabase/functions/weekly-tips/index.ts:20-59` | Weekly tips | worker job (§4) | — | **MVP, OPEN** | not built (cmd/worker is a stub, D-OPEN-9) |

## 4. Supabase Edge Functions → Go worker jobs

The job queue is Postgres-backed (River proposed, `docs/DECISIONS.md`). Jobs are idempotent, and retries must never double-send.

| Legacy | file:line | Target jobs | Behaviour changes | Go endpoint status |
| --- | --- | --- | --- | --- |
| `check-budget-alerts` (pg_cron daily 09:00 UTC, `legacy/scripts/cron_jobs.sql:11-24`) | reads budgets `legacy/supabase/functions/check-budget-alerts/index.ts:32-34`, user per budget `:42-46` (N+1), month expenses `:49-54`, threshold logic `:12, 60-71`, email `:81-122`, then marks `:124-130` | `budget_alerts_scan` (periodic, hourly) → one `budget_alert_check{user_id}` per user. Also enqueued after expense writes. Then `send_email{notification_id}` | User-local month (`users.timezone`), not UTC. Dedupe via `notifications` row `budget:{budget_id}:{YYYY-MM}:{threshold}` inserted in the same tx as the email job. Respects `email_budget_alerts`. Email body uses `html/template` (escaped). | not built (cmd/worker is a stub, D-OPEN-9) |
| `weekly-tips` (Mon 09:00 UTC, `legacy/scripts/cron_jobs.sql:26-39`) | all users `legacy/supabase/functions/weekly-tips/index.ts:67-69`, last-7-day transactions `:77-81`, LLM call `:20-59`, email `:113-144` | `weekly_tips_scan` (hourly, picks users whose local time is Monday 09:xx) → `weekly_tips{user_id, iso_week}` → `send_email` | LLM via `ai.TipsGenerator` (OPEN vendor). Only aggregates are sent, no descriptions. Dedupe `weekly_tips:{YYYY-Www}`. Respects `email_weekly_tips`. | not built (cmd/worker is a stub, D-OPEN-9) |
| shared | `legacy/supabase/functions/_shared/resend.ts:3-30`, `emailLayout.ts:8-27`, `supabaseAdmin.ts:5-12` | `internal/provider/email.Sender` (OPEN vendor; `log` driver in dev), `internal/email/templates` | No service-role key. Worker queries live in `worker_*.sql` (cross-user by design). | not built (cmd/worker is a stub, D-OPEN-9). Only the `apps/api/internal/provider/email` interface with `log`/`none`/`memory` drivers exists; no templates |
| — | — | `balance_reconcile` (weekly) | New. Checks `balance = opening + Σ delta` and logs drift. | not built (cmd/worker is a stub, D-OPEN-9) |

Manual trigger for testing (planned): `go run ./cmd/worker run <job>`. There is no HTTP trigger.

## 5. Mobile: what was removed and what replaced it (done 2026-11-01)

**Remove:** `@clerk/expo`, `@supabase/supabase-js`, `lib/supabase.ts`, `hooks/useSupabase.ts`, `hooks/useUserSync.ts`, Supabase-backed
`lib/services/{accounts,transactions,budgets}.ts`, `lib/services/extractTransaction.ts`, `lib/services/assistant.ts`, the direct calls in
`app/(root)/onboarding.tsx:50-96` and `app/(root)/(tabs)/profile.tsx:174-177`, the hand-written DB types in `types/*.ts`, the client aggregates
(`index.tsx:85-130`, `transactions.tsx:73-101`, `lib/utils/exportTransactions.ts`), the Clerk custom flows in `app/(auth)/*`, and the env vars
`EXPO_PUBLIC_CLERK_PUBLISHABLE_KEY`, `EXPO_PUBLIC_SUPABASE_URL`, `EXPO_PUBLIC_SUPABASE_KEY`, `EXPO_PUBLIC_GEMINI_API_KEY`.

**Replace with:** `@arth/api-client` (generated types + typed fetch with refresh-on-401), `hooks/useApi.ts`, an `AuthProvider` (tokens in
`expo-secure-store`), the same TanStack Query hook names (`useAccountsQuery`, `useTransactionsQuery` → infinite, `useBudgetQuery`, plus
`useMeQuery`, `useDashboardQuery`, `useDailyReportQuery`, `useCategoriesQuery`, `useReceiptExtraction`), money helpers
(`parseMoneyInput`/`formatMinor`) instead of every `parseFloat` (`lib/schemas/transaction.ts:10`, `lib/schemas/onboarding.ts:8`,
`components/BudgetModal.tsx:31`, `add-transaction.tsx:168`, `onboarding.tsx:46`), and `occurred_on = yyyy-MM-dd` instead of `toISOString()` (`add-transaction.tsx:177`).

Order inside session 4 (the app stays runnable after each step): auth (register/login/logout + AuthProvider) → me/onboarding →
accounts + categories → transactions → budget → dashboard/reports → export → receipt scan → delete Clerk/Supabase/Gemini code and deps.
Voice and assistant stay hidden in release builds until their endpoints exist.

## 6. Issues found in the current code (fixed by the migration)

1. AI key in the client bundle and in the URL query string (`lib/services/extractTransaction.ts:33`, `lib/services/assistant.ts:93`, `legacy/supabase/functions/weekly-tips/index.ts:40`).
2. Non-atomic balance updates with a read-modify-write race (`lib/services/transactions.ts:42-65, 79-101`, `app/(root)/onboarding.tsx:76-96`).
3. Account ownership unchecked on transaction insert. Updates/deletes filter by `id` only (`lib/services/accounts.ts:52, 64, 94`, `lib/services/transactions.ts:81`).
4. Floats for money (`parseFloat`, JS sums, `numeric` columns). `lib/utils.ts:7` drops decimals (`maximumFractionDigits: 0`).
5. **Transaction rows ignore the user's currency:** `components/TransactionRow.tsx:72` calls `formatPrice(tx.amount)` with the default `"INR"`.
6. Every screen loads every transaction (`lib/services/transactions.ts:23`).
7. Date drift: `toISOString()` for picked days (`add-transaction.tsx:177`). Dashboard uses local months, alerts use UTC months.
8. Starting balance stored as income (`app/(root)/onboarding.tsx:76-85`).
9. Sign-in `needs_second_factor` sends a phone code with no screen to enter it (`app/(auth)/sign-in.tsx:57-58`).
10. Jobs: N+1, sequential, abort on first error, send-then-mark (duplicates), no opt-out, unescaped HTML (`legacy/supabase/functions/*`).
11. Type drift: `types/budget.ts` lacks `last_alert_threshold`. `status`, `is_flagged`, `flag_reason` are never written (read at `components/TransactionRow.tsx:57`).
12. Two React Hook dependency warnings (`hooks/useSupabase.ts:10`, `hooks/useUserSync.ts:74`). Both files are removed in session 4.
13. Unused dependency `react-native-linear-gradient` (the code uses `expo-linear-gradient`).

## 7. Provenance & licence checklist (third-party Welth code)

Repoint session (2026-11-01) rewrites, so the Welth share of these files shrank: the data/auth layer (`hooks/**`, `lib/**`, `store/**`)
is new code; `app/(auth)/sign-in.tsx`, `sign-up.tsx`, `onboarding.tsx`, `(tabs)/index.tsx`, `transactions.tsx`, `assistant.tsx`,
`components/TransactionRow.tsx`, `BudgetModal.tsx`, `CurrencyPicker.tsx` and `TransactionForm.tsx` (moved out of `add-transaction.tsx`) keep
Welth's layout/JSX and classes but their logic was rewritten; `ChangePasswordModal`, `DeleteAccountModal`, `app/(root)/transaction/[id].tsx`
are new. `constants/categories.ts` (copied labels/colours) and `types/*` were deleted.

No LICENSE upstream or here. The README presents it as a course project. **Assume no redistribution rights until the author grants them.**
Before launch, each item is rewritten, replaced or cleared in writing.

| Item | Paths | Action |
| --- | --- | --- |
| Brand artwork | `apps/mobile/assets/images/welth.png`, `welth-light.png`, `welth-bg.png`, `icon.png`, `splash-icon.png`, `android-icon-*.png`, `favicon.png` | **Replace** (still shows the Welth logo). Unused `react-logo*.png`, `partial-react-logo.png` can go. |
| App identity | `apps/mobile/app.json` | Done: Arth, `fyi.akashbuilds.arth` (placeholder; confirm before any Play upload). |
| Screens & components | `apps/mobile/app/**`, `apps/mobile/components/**` | Rewrite progressively or clear. Note rewrites here. |
| Data layer | `apps/mobile/hooks/**`, `lib/**`, `store/**`, `types/**` | Replaced in session 4. |
| Design tokens & categories | `apps/mobile/tailwind.config.js`, `constants/theme.ts`, `constants/categories.ts` | Re-pick the palette. Keys are generic; treat labels/colours as copied. |
| Category seed (Go API) | `apps/api/db/migrations/00002_seed_categories.sql` | **Copied verbatim:** the 21 category display names, emoji icons and hex colours come from Welth's `apps/mobile/constants/categories.ts`. The keys and kinds are generic. Flag for the licence decision (D-OPEN-11): re-pick names/icons/colours in a new migration, or clear them with the author. |
| Prompts | `apps/mobile/lib/services/extractTransaction.ts:66-92`, `lib/services/assistant.ts:87-91`, `legacy/supabase/functions/weekly-tips/index.ts:34-38` | Rewrite in Go. |
| Email templates | `legacy/supabase/functions/_shared/emailLayout.ts`, `check-budget-alerts/index.ts:81-120`, `weekly-tips/index.ts:113-142` | Rewrite in Go templates. |
| SQL & README | `legacy/scripts/*.sql`, `legacy/WELTH_README.md` | Deleted with `legacy/`. |
| Framework boilerplate | `apps/mobile/{babel,metro,eslint}.config.js`, `nativewind-env.d.ts`, `global.css` | Low risk. Keep. |

Apart from the category seed above, **no Welth code or prompts were copied into `apps/api`** (Go API session, 2026-11-01): the Go code,
SQL schema, queries and OpenAPI contract were written new, and no AI prompt exists yet (the receipt extractor has only a fake driver).
