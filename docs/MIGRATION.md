# Migration: Welth (Expo + Supabase + client-side Gemini) → Arth (Go API + worker)

This is the authoritative inventory of everything that talks to Supabase or Gemini today and where it goes.
Line numbers refer to the copied Welth tree as of 2026-10-31. Endpoint shapes are summaries.
`api/openapi.yaml` is the contract once it exists (see `docs/API.md`).

Legend: **MVP** = needed for launch · **POST** = post-MVP · `uid` = authenticated `users.id` from middleware.

---

## 0. Before anything else (Step 0)

The copied tree does not run. These problems were found during exploration:

| Problem | Evidence | Fix |
| --- | --- | --- |
| Root layout missing | `app/_layout.tsx` is byte-identical to `app/(auth)/_layout.tsx` (an `AuthLayout`). Nothing mounts `ClerkProvider`, `QueryClientProvider`, the `global.css` import, or calls `useUserSync`. | Restore from upstream Welth, or rewrite (providers + `tokenCache` from `expo-secure-store`). |
| Root index missing | `app/index.tsx` is identical to `app/(root)/(tabs)/index.tsx` (HomeScreen, with a `../../../assets` require that cannot resolve from `app/`). | Restore: it should be a redirect based on auth and `needsOnboarding`. |
| `(root)` layout missing | `app/(root)/_layout.tsx` is identical to `app/(root)/(tabs)/_layout.tsx` (a second tab bar). The onboarding gate is missing. | Restore: a Stack that redirects signed-out users to `/sign-in` and `needsOnboarding` users to `/onboarding`. |
| Wrong tsconfig | `tsconfig.json` is identical to `supabase/functions/tsconfig.json`: no `extends: "expo/tsconfig.base"`, no `paths: {"@/*": ["./*"]}`, so `@/` imports will not resolve. | Restore the Expo tsconfig. |
| Dangling references | `package.json` → `scripts/reset-project.js` (missing). `supabase/functions/_shared/emailLayout.ts:2` → `scripts/email_assets_bucket.sql` (missing). | Remove the script entry. Ignore the SQL reference (it goes away with Supabase). |
| Not a git repo; no LICENSE | — | `git init`, commit the baseline as-is (so the provenance diff is provable), then fix. See Provenance below. |
| `supabase/.temp/` not ignored | Contains the linked project ref and pooler host/user (no password found). | Add it to `.gitignore` or delete it. |

Goal of Step 0: the current app runs on an Android emulator against the existing Supabase project, **before** any migration work.
Every later slice is measured against that working baseline.

---

## 1. Interaction map: every Supabase and Gemini call

### 1.1 Client setup (removed)

| # | Current file:line | What it does | Target | Notes |
| --- | --- | --- | --- | --- |
| C1 | `lib/supabase.ts:3-14` | Reads `EXPO_PUBLIC_SUPABASE_URL`/`_KEY`, creates an anon client (`supabase` export, unused) | **Delete** | Replaced by `@arth/api-client` configured with `EXPO_PUBLIC_API_BASE_URL`. |
| C2 | `lib/supabase.ts:19-27` | `createClerkSupabaseClient(getToken)` attaches the Clerk JWT for RLS | **Delete** | Replaced by `createApiClient({ baseUrl, getToken })`, which sends `Authorization: Bearer`. |
| C3 | `hooks/useSupabase.ts:5-13` | Memoised authed client per component | **Replace** with `hooks/useApi.ts` | Same shape (one client per app, token pulled per request). |

### 1.2 Users / onboarding / profile

| # | Current file:line | What it does | New endpoint | Request → Response | Auth rule | Notes |
| --- | --- | --- | --- | --- | --- | --- |
| U1 | `hooks/useUserSync.ts:17-21` | `users.select(clerk_id,currency).eq(clerk_id)` | `GET /v1/me` | — → `Me {id, email, name, image_url, default_currency\|null, timezone, needs_onboarding, email_budget_alerts, email_weekly_tips}` | Bearer. Row = `sub` | **MVP.** Lazily provisions on first call (U2+U3) in one DB tx. |
| U2 | `hooks/useUserSync.ts:37-49` | Upserts user with client-supplied email/name/image | *(folded into U1)* | — | — | The server fetches email/name from the Clerk Backend API, so the client can no longer set someone else's email (today an email-spam vector). |
| U3 | `hooks/useUserSync.ts:60-62` | Inserts default "Cash" account | *(folded into U1)* | — | — | Same tx as the user insert. The unique partial index guarantees one default. |
| U4 | `app/(root)/onboarding.tsx:50-55` | `users.update(currency)` | `POST /v1/me/onboarding` | `{default_currency, timezone, opening_balance_minor}` → `Me` | Bearer | **MVP.** U4–U7 become one atomic call. |
| U5 | `app/(root)/onboarding.tsx:63-68` | Selects default account | *(folded into U4)* | | | |
| U6 | `app/(root)/onboarding.tsx:76-85` | Inserts "Starting balance" as an **INCOME** transaction | *(folded into U4)* | | | Behaviour change: this becomes `accounts.opening_balance_minor`, not income, so month-income stats stop being inflated. |
| U7 | `app/(root)/onboarding.tsx:93-96` | `accounts.update(balance = old + parsed)` | *(folded into U4)* | | | Read-modify-write race is removed. |
| U8 | `app/(root)/(tabs)/profile.tsx:174-177` | `users.update(currency)` | `PATCH /v1/me` | `{default_currency?, timezone?, email_budget_alerts?, email_weekly_tips?}` → `Me` | Bearer | **MVP.** Currency change returns `409 currency_locked` once transactions exist (no FX until multi-currency). Today it silently relabels all amounts. |
| U9 | *(none: new)* | — | `DELETE /v1/me` | — → `204` | Bearer | **MVP.** Google Play requires in-app and web account deletion. Deletes app data (cascade) and the Clerk user via the Backend API. |
| U10 | `profile.tsx:132` `user.setProfileImage` | Avatar upload to Clerk | **unchanged** (Clerk) | — | — | `users.image_url` is refreshed on `GET /v1/me` provisioning, or by a Clerk webhook (POST). |
| U11 | `profile.tsx:89,285` biometric switch | UI-only state, never persisted | **none** | — | — | Either implement locally on the device (`expo-local-authentication`, a new dependency) or remove it. Not an API concern. |

### 1.3 Accounts

| # | Current file:line | What it does | New endpoint | Request → Response | Auth rule | Notes |
| --- | --- | --- | --- | --- | --- | --- |
| A1 | `lib/services/accounts.ts:8-13` | List own accounts, default first, then `created_at` | `GET /v1/accounts` | — → `{data: Account[]}` | `WHERE user_id=uid` | **MVP.** `Account {id, name, type, currency, balance_minor, opening_balance_minor, is_default, created_at}` |
| A2 | `lib/services/accounts.ts:24-28` | Insert account (balance 0) | `POST /v1/accounts` | `{name, type, currency?, opening_balance_minor?}` → `201 Account` | `user_id=uid` set server-side | **MVP.** Currency defaults to `users.default_currency`. |
| A3 | `lib/services/accounts.ts:42-53` | Two sequential updates: unset other defaults, set target | `POST /v1/accounts/{id}/make-default` | — → `Account` | Both updates `AND user_id=uid`, one tx | **MVP.** The second update today filters only by `id` (RLS-dependent). |
| A4 | `lib/services/accounts.ts:61-66` | Update name/type **by id only** | `PATCH /v1/accounts/{id}` | `{name?, type?}` → `Account` | `WHERE id=$1 AND user_id=uid` → 404 | **MVP.** Currency is not editable once transactions exist. |
| A5 | `lib/services/accounts.ts:80-83` | Count transactions for the account (dry run) | `DELETE /v1/accounts/{id}` (no `cascade`) | — → `204`, or `409 account_has_transactions {transaction_count}` | scoped | **MVP.** Same two-step UX as `AccountModal.tsx:76-95`. Deleting the default account → `409 default_account` (make another default first). |
| A6 | `lib/services/accounts.ts:92-95` | Delete account (FK cascade deletes transactions) | `DELETE /v1/accounts/{id}?cascade=true` | — → `204` | scoped | **MVP.** Transfers touching the account: delete both legs in the same tx. |

### 1.4 Transactions

| # | Current file:line | What it does | New endpoint | Request → Response | Auth rule | Notes |
| --- | --- | --- | --- | --- | --- | --- |
| T1 | `lib/services/transactions.ts:23-28` | Select **all** own transactions, optional `type`/`account_id`, order `date desc` | `GET /v1/transactions` | `?type&account_id&category_id&from&to&q&limit&cursor` → `{data: Transaction[], next_cursor}` | scoped | **MVP.** Keyset pagination. Search moves server-side (`transactions.tsx:73-81` searched client-side). Mobile uses `useInfiniteQuery`. |
| T2 | `lib/services/transactions.ts:42-46` | Insert with **client-supplied `user_id`**; `account_id` not checked for ownership | `POST /v1/transactions` | `{account_id, category_id, type: INCOME\|EXPENSE, amount_minor, occurred_on, description?, input_method, voice_transcript?}` → `201 {transaction, account}` | `user_id=uid` server-side. Account and category must belong to uid (composite FK + query) | **MVP.** Today a foreign `account_id` would be accepted (RLS only checks `user_id`), and the follow-up balance update silently matches 0 rows. |
| T3 | `lib/services/transactions.ts:50-54` | Read account balance | *(folded into T2)* | | | |
| T4 | `lib/services/transactions.ts:60-63` | Write `balance = read + delta` (lost-update race, not atomic with T2) | *(folded into T2)* | | | `UPDATE … SET balance_minor = balance_minor + delta` in the same tx. |
| T5 | `lib/services/transactions.ts:79-82` | Delete by id (client passes amount/type/account for reversal) | `DELETE /v1/transactions/{id}` | — → `204` | scoped | **MVP.** The server reads `account_id`/`delta_minor` from the row (`DELETE … RETURNING`) and never trusts client values. |
| T6 | `lib/services/transactions.ts:86-90` | Read balance | *(folded into T5)* | | | |
| T7 | `lib/services/transactions.ts:96-99` | Write reversed balance | *(folded into T5)* | | | |
| T8 | *(none: new)* | — | `PATCH /v1/transactions/{id}` | partial of T2 body → `{transaction, accounts[]}` | scoped | **MVP** (an edit screen is expected). Reverses the old delta and applies the new one, locking both accounts in id order. |
| T9 | *(none: new)* | — | `GET /v1/transactions/{id}` | → `Transaction` | scoped | **MVP** (edit screen, web detail). |
| T10 | *(none: new)* | — | `POST /v1/transfers`, `DELETE /v1/transfers/{transfer_id}` | `{from_account_id, to_account_id, amount_minor, occurred_on, description?}` → `201 {transfer_id, legs[2], accounts[2]}` | both accounts scoped | **MVP-stretch** (see Concerns). Two linked rows. Excluded from income/expense. |
| T11 | `constants/categories.ts` (static) | Category keys, labels, icons, colours | `GET /v1/categories` | `?kind` → `{data: Category[]}` | system + own | **MVP.** Seeded from the 21 keys. Custom categories are **POST**. |

### 1.5 Budgets

| # | Current file:line | What it does | New endpoint | Request → Response | Auth rule | Notes |
| --- | --- | --- | --- | --- | --- | --- |
| B1 | `lib/services/budgets.ts:8-12` | Get the single budget | `GET /v1/budgets?month=YYYY-MM` | → `{data: BudgetProgress[]}` with `{id, category_id\|null, amount_minor, currency, spent_minor, percent_used}` | scoped | **MVP** (overall budget = `category_id: null`). Progress is computed server-side in the user's timezone. |
| B2 | `lib/services/budgets.ts:23-27` | Upsert on `user_id` | `PUT /v1/budgets/overall` | `{amount_minor}` → `BudgetProgress` | scoped | **MVP.** `DELETE /v1/budgets/overall` is new. |
| B3 | *(none: new)* | — | `PUT/DELETE /v1/budgets/categories/{category_id}` | `{amount_minor}` | scoped | Schema ready at MVP. Per-category UI is **POST**. |

### 1.6 Client-side aggregates → server aggregates

| # | Current file:line | What it does | New endpoint | Response | Notes |
| --- | --- | --- | --- | --- | --- |
| D1 | `app/(root)/(tabs)/index.tsx:85-130` | Total balance, month income/expense, top-5 recent, category breakdown, all from the full list | `GET /v1/dashboard?month=YYYY-MM` | `{total_balance_minor, currency, month: {income_minor, expense_minor}, budget: BudgetProgress\|null, expense_by_category: [{category_id, amount_minor}], recent: Transaction[5]}` | **MVP.** Transfers excluded. Today, `isSameMonth` uses device-local time while the alert job uses UTC; this unifies them. |
| D2 | `app/(root)/(tabs)/transactions.tsx:83-101` | Daily income vs expense bars for the current month | `GET /v1/reports/daily?from&to&account_id?` | `{data: [{date, income_minor, expense_minor}]}` | **MVP.** Zero-filled days, max 93-day range. |
| D3 | `lib/utils/exportTransactions.ts:41-64` | CSV of the last 30 days, built from the in-memory list; shared via `expo-sharing` | `GET /v1/transactions/export?format=csv&from&to&account_id?` | `text/csv` stream, `Content-Disposition: attachment` | **MVP.** Mobile downloads to cache with the auth header (verify the `expo-file-system` SDK 54 download API), then shares. Web uses a blob download. Amounts are formatted as decimal strings from minor units. |

### 1.7 Gemini (all calls move server-side)

| # | Current file:line | What it does | New endpoint | Request → Response | Notes |
| --- | --- | --- | --- | --- | --- |
| G1 | `lib/services/extractTransaction.ts:29-60` (`callGemini`) | `fetch(...gemini-3.1-flash-lite:generateContent?key=EXPO_PUBLIC_GEMINI_API_KEY)` with a JSON response schema | `internal/ai` package (Go SDK) | — | The key is in the bundle **and** in the URL query today. **Rotate the key** at cut-over. |
| G2 | `lib/services/extractTransaction.ts:62-77`, called from `app/(root)/(tabs)/add-transaction.tsx:136`; image from `components/ReceiptScannerModal.tsx:31-35, 48-56` (base64) | Receipt → `{type, amount, category, description, date, transcript}` | `POST /v1/ai/receipt-extractions` | `multipart/form-data: image` (jpeg/png/webp/heic, ≤ 5 MB) → `Extraction {type, amount_minor\|null, currency, category_id\|null, category_key\|null, description\|null, occurred_on\|null}` | **MVP.** Not persisted. The client pre-fills the form, then T2 with `input_method=RECEIPT_SCAN`. Ask Gemini for the amount as a **decimal string**, then parse to minor units in Go (no float). Clamp future dates server-side. Rate-limited. |
| G3 | `lib/services/extractTransaction.ts:79-95`, called from `components/VoiceRecorderModal.tsx:166-168` (m4a base64) | Voice → same shape + transcript | `POST /v1/ai/voice-extractions` | `multipart: audio` (m4a, ≤ 60 s) → `Extraction + transcript` | **POST.** "Today" is resolved in the user's timezone server-side. |
| G4 | `lib/services/assistant.ts:76-111` (+ `buildContext` `:10-74`), called from `app/(root)/(tabs)/assistant.tsx:80-82` | Builds a context from all client-held transactions and asks Gemini | `POST /v1/assistant/messages` | `{message, history?: [{role, content}] (≤ 10)}` → `{reply}` | **POST.** The server builds the context from SQL aggregates (last 30 days, budgets). Prompt-injection guard: data goes in a separate part, with an instruction to treat it as data. |
| G5 | `supabase/functions/weekly-tips/index.ts:20-59` | Weekly tips from 7-day aggregates | River job `weekly_tips` (see §2) | — | **MVP.** |

### 1.8 Edge-function DB calls (moved into the worker)

| # | Current file:line | What it does | Target |
| --- | --- | --- | --- |
| F1 | `supabase/functions/check-budget-alerts/index.ts:32-34` | All budgets (service role) | sqlc `ListBudgetsDueForCheck` (worker only, cross-user by design, read-only) |
| F2 | `…/check-budget-alerts/index.ts:42-46` | User per budget (N+1) | Join in the same query |
| F3 | `…/check-budget-alerts/index.ts:49-54` | All expense rows this month (UTC), summed in JS | sqlc `SumExpensesForBudget(user_id, category_id?, month_start, month_end)` in SQL, user-local month |
| F4 | `…/check-budget-alerts/index.ts:122, 124-130` | Send email, **then** update `last_alert_*` (a failure in between means a duplicate email next day) | Insert `notifications` row (unique dedupe key) + enqueue `send_email` in one tx |
| F5 | `supabase/functions/weekly-tips/index.ts:67-69` | All users | sqlc `ListUsersForWeeklyTips(local_hour, weekday)` |
| F6 | `…/weekly-tips/index.ts:77-81` | User's last-7-day transactions | sqlc `WeeklySummary(user_id, from, to)` (totals + by category) |
| F7 | `…/weekly-tips/index.ts:144` | Send email | `send_email` job |
| F8 | `scripts/cron_jobs.sql:11-39` | pg_cron + pg_net + Vault secrets | River periodic jobs. **Unschedule** the Supabase cron at cut-over (`select cron.unschedule('check-budget-alerts'); select cron.unschedule('weekly-tips');`). |

---

## 2. Edge Functions → River jobs (apps/api cmd/worker)

All jobs are idempotent. River retries with backoff, so a retried job must never double-send or double-post.

### 2.1 `check-budget-alerts` → `budget_alerts_scan` + `budget_alert_check` + `send_email`

| Aspect | Today | Target |
| --- | --- | --- |
| Trigger | pg_cron daily 09:00 UTC → HTTP | River periodic `budget_alerts_scan` **hourly**. It also enqueues `budget_alert_check{user_id}` after any expense create/update (`UniqueOpts{ByArgs, ByPeriod: 10m}`), so alerts arrive near-real-time. |
| Fan-out | One Deno invocation loops over every user sequentially; the first error aborts everyone | Scan inserts one `budget_alert_check` per user with budgets (`InsertMany`). Failures are isolated per user. |
| Month boundary | UTC calendar month | User-local calendar month (`users.timezone`) on `occurred_on` dates. Matches the dashboard. |
| Threshold logic | `THRESHOLDS=[100,80]`, highest first; `last_alert_threshold` reset by month (`index.ts:12, 60-70`) | Same rule, per budget (overall or category): `budgets.alert_thresholds` default `{80,100}`; pick the highest crossed threshold that has no notification row yet for this month. |
| Dedupe | `budgets.last_alert_sent/threshold` updated **after** the send | `notifications` row `dedupe_key = "budget:{budget_id}:{YYYY-MM}:{threshold}"`, `UNIQUE(user_id, dedupe_key)`, inserted `ON CONFLICT DO NOTHING`. Only if inserted, enqueue `send_email{notification_id}` via `InsertTx` in the **same** tx. |
| Opt-out | none | Skip if `users.email_budget_alerts = false`. Every email has a one-click unsubscribe link and a `List-Unsubscribe` header. |
| Email | String-built HTML, unescaped `user.name` (`index.ts:86-120`) | `html/template` (auto-escaping) in `internal/email/templates`. Text alternative. Money formatted from minor units. |

### 2.2 `weekly-tips` → `weekly_tips_scan` + `weekly_tips` + `send_email`

| Aspect | Today | Target |
| --- | --- | --- |
| Trigger | Mondays 09:00 UTC | River periodic `weekly_tips_scan` **hourly**. Selects users whose local time is Monday 09:00–09:59, with `email_weekly_tips = true` and ≥ 1 transaction in the previous 7 local days. |
| Work | Sequential loop, Gemini call inline, throw aborts the run | One `weekly_tips{user_id, iso_week}` job per user (`UniqueOpts ByArgs`). `MaxAttempts: 5`. A 30 s Gemini timeout per attempt. |
| AI | REST with `?key=` and a JSON schema of 2–4 tips (`index.ts:12-18, 40-50`) | Go SDK, same response schema. Prompt in `internal/ai/prompts`. Input = aggregates only (no descriptions) to minimise PII sent to Gemini. |
| Dedupe | none (a retry would resend) | `notifications.dedupe_key = "weekly_tips:{YYYY-Www}"`, then `send_email`. |
| Coupling | Imports `../../../constants/categories.ts` from the app | Category labels come from the `categories` table. |

### 2.3 `send_email` (shared)

- Args: `{notification_id}`. Loads the notification and user, renders the template, calls Resend with `Idempotency-Key = notification_id`,
  and stores `provider_message_id` with `status=SENT`. On a permanent error it sets `status=FAILED` and the job stops retrying.
- Local/dev: an `EMAIL_DRIVER=log` env switch writes emails to the log instead of sending.

### 2.4 Other worker jobs

- `balance_reconcile` (weekly): checks `balance_minor = opening_balance_minor + Σ delta_minor` per account and logs mismatches (it does not auto-fix).
- `attachment_sweeper` (POST): deletes orphaned storage objects.
- `recurring_materialize` (POST): posts due `recurring_rules`.
- Manual run for testing replaces "invoke from dashboard": `go run ./cmd/worker run budget_alerts_scan` (CLI subcommand, no HTTP exposure).

---

## 3. Mobile app: remove / replace

### Remove (at cut-over, Month 2; AI pieces Month 4)

| Remove | Where |
| --- | --- |
| `@supabase/supabase-js` dependency | `package.json` |
| `lib/supabase.ts`, `hooks/useSupabase.ts` | whole files |
| Direct Supabase calls in screens | `app/(root)/onboarding.tsx:24,50-96`, `app/(root)/(tabs)/profile.tsx:86,174-177`, `hooks/useUserSync.ts` (whole) |
| Supabase-backed services | `lib/services/accounts.ts`, `transactions.ts`, `budgets.ts` |
| Client-side Gemini + `EXPO_PUBLIC_GEMINI_API_KEY` | `lib/services/extractTransaction.ts`, `lib/services/assistant.ts`, README env list |
| Client aggregates over the full list | `app/(root)/(tabs)/index.tsx:85-130`, `transactions.tsx:73-101`, `lib/utils/exportTransactions.ts` (CSV building) |
| Hand-written DB types | `types/account.ts`, `types/budget.ts`, `types/transaction.ts`: replaced by generated types |
| `EXPO_PUBLIC_SUPABASE_URL`, `EXPO_PUBLIC_SUPABASE_KEY` | `.env`, EAS env |
| `supabase/` folder, `scripts/schema.sql`, `scripts/cron_jobs.sql`, `scripts/seed.sql` | after the Go migrations and seed exist |
| `eslint.config.js` ignore for `supabase/functions/**` | after the folder is deleted |

### Replace with

- **`@arth/api-client`** (workspace package): generated types + a typed fetch client from `api/openapi.yaml`.
  `createApiClient({ baseUrl, getToken })`. Throws a typed `ApiError` carrying the problem+json body.
- **`hooks/useApi.ts`**: one client per app, `getToken` from `@clerk/expo` `useAuth()`.
- **TanStack Query hooks** keep their current names and keys, so screens barely change:
  `useAccountsQuery`, `useTransactionsQuery` (→ `useInfiniteQuery`), `useBudgetQuery`, plus new `useMeQuery`, `useDashboardQuery`,
  `useDailyReportQuery`, `useCategoriesQuery`, `useReceiptExtraction`. Mutations invalidate `accounts`, `transactions`, `dashboard`, `budget`.
- **Zustand `userStore`** shrinks: `currency`/`needsOnboarding` are derived from `useMeQuery()` and no longer duplicated.
- **Money input:** a `parseMoneyInput(str, currency) → amount_minor` decimal-string parser and a `formatMinor(amount_minor, currency)`
  display helper (shared from `@arth/api-client/money`). Replaces `parseFloat` in `lib/schemas/transaction.ts:10`,
  `lib/schemas/onboarding.ts:8`, `components/BudgetModal.tsx:31`, `add-transaction.tsx:168`, `onboarding.tsx:46`, and `lib/utils.ts` `formatPrice`.
- **Dates:** the form keeps a `Date` but sends `occurred_on = format(date, "yyyy-MM-dd")` (local), not `toISOString()`.

---

## 4. Migration order (vertical slices; the app keeps working)

**Principle:** the existing Supabase path is the working fallback until parity. A single build-time switch
`EXPO_PUBLIC_BACKEND=supabase|api` selects the implementation behind each **hook** (screens do not know). Slices are added to the `api`
side one at a time and QA'd with seeded data in Neon. The default stays `supabase` until every MVP slice is done, then flips, and
then the Supabase code is deleted. There are no production users, so no live data migration is required (optional dev-data port in §5).

| # | Slice | API work | Mobile work | Done when |
| --- | --- | --- | --- | --- |
| 0 | Baseline | — | Step 0 fixes. Move the direct Supabase calls (onboarding, profile, useUserSync) **into services/hooks** with no behaviour change. Add the `EXPO_PUBLIC_BACKEND` switch. | The app runs on Android against Supabase. No screen imports `useSupabase`. |
| 1 | Platform | Monorepo, `apps/api` skeleton, `/healthz` `/readyz`, Dockerfile, compose, Neon, staging deploy, CI. Baseline goose migration (MVP schema + River tables) + seed. | App moved to `apps/mobile`, still on Supabase. | Staging URL answers `/healthz`. Mobile runs from `apps/mobile`. |
| 2 | Identity | Clerk JWKS middleware, `GET/PATCH/DELETE /v1/me`, `POST /v1/me/onboarding` | `@arth/api-client`, `useApi`, `useMeQuery`; onboarding + currency on the API side | Sign up → onboarding → home works on `api` (home shows empty data). |
| 3 | Accounts + categories | A1–A6, T11 | Profile accounts section, AccountModal | CRUD + make-default on `api`. |
| 4 | Transactions | T1–T9 (+T10 if in scope) | Add/edit/delete, list with infinite scroll + filters + search | Balances stay correct (integration tests assert the invariant). |
| 5 | Budget | B1–B2 | BudgetModal, home budget card | |
| 6 | Dashboard + reports | D1, D2 | Home + transactions chart read server aggregates | No full-list fetches remain. |
| 7 | Export | D3 | Export button | CSV opens in Sheets with correct decimals. |
| 8 | **Cut-over (core)** | — | Flip the default to `api`. Delete Supabase code/deps/env. Unschedule Supabase cron. Pause the Supabase project. | `grep -r supabase apps/mobile` is empty. |
| 9 | Receipt scan (Month 4) | G2 + rate limit | ReceiptScannerModal → multipart upload | `EXPO_PUBLIC_GEMINI_API_KEY` deleted. **Gemini key rotated.** |
| 10 | Worker (Month 4) | §2 jobs, templates, unsubscribe | Email prefs toggles in profile | Alerts + tips arrive on staging and dedupe correctly. |
| — | Voice + assistant | G3, G4 (POST) | Until then **hidden** in release builds (feature flag), because they still call Gemini from the client | No client Gemini code in the production bundle at launch. |

Between slices 2 and 8 the `api` build is partially functional by design. The `supabase` build is the "always working" app.

---

## 5. Optional: port existing dev data

Only if the Supabase project has data worth keeping. Write it as a one-off Go command (`cmd/tools/import-welth`), not SQL in the app:

- `users.clerk_id` → `users.clerk_user_id`; new uuid `id`; `timezone` defaults to `Asia/Kolkata` or as given.
- `numeric amount` → `round(amount * 10^exponent(currency))::bigint`; reject non-integral results instead of rounding silently.
- `transactions.date timestamptz` → `(date AT TIME ZONE user_tz)::date` as `occurred_on`.
- `category` text key → `categories.id` (system key lookup).
- Rows with `description = 'Starting balance' AND category = 'other_income'` → add to `accounts.opening_balance_minor` and drop the row.
- `budgets.amount` → overall budget. Current-month `last_alert_threshold` → a pre-inserted `notifications` dedupe row, so nobody is re-alerted.
- Recompute `balance_minor` from transactions and compare it with the imported balance. Report differences.

---

## 6. Issues found in the current code (fixed by the migration)

1. **Leaked AI key:** `EXPO_PUBLIC_GEMINI_API_KEY` is compiled into the bundle and passed as a URL query param (`extractTransaction.ts:33`, `assistant.ts:93`, `weekly-tips/index.ts:40`).
2. **Non-atomic balances:** insert/delete and balance update are separate calls, with a read-modify-write race (`transactions.ts:42-65, 79-101`, `onboarding.tsx:76-96`).
3. **Unchecked account ownership** on transaction insert, and updates/deletes filtered by `id` only (they rely on RLS) (`accounts.ts:52, 64, 94`; `transactions.ts:81`).
4. **Floats for money:** `parseFloat` + JS number sums + `numeric` columns; `formatPrice` drops decimals (`maximumFractionDigits: 0`).
5. **Unbounded reads:** every screen loads every transaction (`transactions.ts:23`).
6. **Date drift:** `Date.toISOString()` for user-picked days (`add-transaction.tsx:177`). Dashboard uses local months, alerts use UTC months.
7. **Starting balance counted as income** (`onboarding.tsx:76-85`).
8. **Jobs:** N+1 queries, sequential, abort-on-first-error, send-then-mark (duplicate emails), no opt-out, unescaped HTML (`check-budget-alerts/index.ts`, `weekly-tips/index.ts`).
9. **Client-trusted identity data:** the email used for alerts comes from the client upsert (`useUserSync.ts:35-45`).
10. **Type drift:** `types/budget.ts` lacks `last_alert_threshold`. `status`, `is_flagged`, `flag_reason` are never written (only `TransactionRow.tsx:57` reads `is_flagged`).
11. **Dependency duplication:** both `expo-linear-gradient` (used) and `react-native-linear-gradient` (unused) are installed.

---

## 7. Provenance & licence checklist (third-party Welth code)

No `LICENSE` file exists. The README presents the code as a course project ("Full Stack React Native Course - Build Welth").
Without a licence, default copyright applies: **assume you may not redistribute it until the author grants permission.**
Everything below was found in the copy and looks verbatim from Welth. Before launch, each item must be **rewritten**, **replaced**, or **cleared** in writing.

| Item | Paths | Recommended action |
| --- | --- | --- |
| Brand assets | `assets/images/welth.png`, `welth-light.png`, `welth-bg.png`, `icon.png`, `splash-icon.png`, `android-icon-*.png`, `favicon.png` | **Replace** (mandatory regardless of licence). Delete the unused `react-logo*.png`, `partial-react-logo.png`. |
| App identity | `app.json` (`name`, `slug`, `scheme: welth`, `ios.bundleIdentifier: com.piyusheon.welth`, permission strings "Allow Welth…"), `package.json` `name: welth` | **Replace** with Arth identifiers. Add `android.package`. |
| Screens & UI | `app/**` (all routes), `components/**` (11 files) | Rewrite progressively (screens change anyway when repointed), or clear. |
| Data layer | `hooks/**`, `lib/**`, `store/**`, `types/**` | Deleted or rewritten during migration. |
| Design tokens & categories | `tailwind.config.js` (brand palette), `constants/theme.ts`, `constants/categories.ts` (keys, labels, emoji, colours) | Re-pick colours. Category keys are generic, but treat the label/colour set as copied. |
| Prompts | `lib/services/extractTransaction.ts:66-92`, `lib/services/assistant.ts:87-91`, `supabase/functions/weekly-tips/index.ts:34-38` | Rewrite in `internal/ai/prompts`. |
| Email templates & copy | `supabase/functions/_shared/emailLayout.ts`, `check-budget-alerts/index.ts:81-120`, `weekly-tips/index.ts:113-142` | Rewrite in Go templates. |
| SQL | `scripts/schema.sql`, `scripts/cron_jobs.sql`, `scripts/seed.sql` (contains a specific Clerk user id) | Deleted. The new schema is in `docs/SCHEMA.md`. |
| Docs | `README.md` (course README) | Rewrite. |
| Expo template leftovers | `eslint.config.js`, `babel.config.js`, `metro.config.js`, `nativewind-env.d.ts`, `global.css`, `.vscode/settings.json` (Peacock colours) | Low risk (framework boilerplate). Keep. |
| Agent config | `AGENTS.md`, `.claude/settings.json` | Content moves to `apps/mobile/CLAUDE.md`. Keep the Expo plugin setting if wanted. |
