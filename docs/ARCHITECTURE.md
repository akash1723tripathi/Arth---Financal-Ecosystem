# Architecture

Status: current = as found in the Welth copy (Oct 2026). Target = decided design, not yet built.

## 1. Current architecture (as found)

```text
                 ┌──────────────────────── Expo app (client) ───────────────────────┐
                 │ Expo Router · NativeWind · TanStack Query · Zustand · RHF + zod  │
                 │                                                                  │
   sign-in/up ◄──┤ @clerk/expo ─────────────────────────────────────────────► Clerk │
                 │                                                                  │
                 │ supabase-js (anon key + Clerk session JWT as accessToken)        │
                 │   lib/supabase.ts · hooks/useSupabase.ts · lib/services/*         │
                 │   + direct calls in onboarding.tsx, profile.tsx, useUserSync.ts   │
                 └──────┬────────────────────────────────────────┬──────────────────┘
                        │ PostgREST (HTTPS)                      │ fetch ?key=EXPO_PUBLIC_GEMINI_API_KEY
                        ▼                                        ▼
        Supabase Postgres (RLS: auth.jwt()->>'sub')      Gemini API (receipt, voice, assistant)
          users · accounts · transactions · budgets
                        ▲
                        │ service-role key (bypasses RLS)
     pg_cron ─pg_net─► Supabase Edge Functions (Deno)
                         ├─ check-budget-alerts  daily 09:00 UTC ──────────────► Resend
                         └─ weekly-tips          Mon 09:00 UTC ──► Gemini ──────► Resend
```

How it works today:

- **Auth:** Clerk handles sign-in and sign-up (`app/(auth)/*`). Supabase trusts Clerk as a third-party auth provider, so RLS policies
  compare `auth.jwt()->>'sub'` to `user_id` (`scripts/schema.sql`).
- **Data:** every read and write is a PostgREST call from the device. Business logic lives on the client: balance updates,
  default-account switching, onboarding, user provisioning. All aggregates are computed client-side
  from an **unpaginated** list of every transaction (`app/(root)/(tabs)/index.tsx`, `transactions.tsx`, `lib/services/assistant.ts`).
- **AI:** the device calls Gemini directly with a key bundled into the app (`EXPO_PUBLIC_GEMINI_API_KEY`).
- **Jobs:** pg_cron triggers two Deno edge functions over HTTP. They loop over all users sequentially and email through Resend.
- **Storage:** none. Receipt images and voice notes are sent to Gemini as base64 and never stored.
  Avatars go to Clerk (`user.setProfileImage`).

Known structural problems: the full list is in `docs/MIGRATION.md` → "Issues found".
The copied tree is also missing its root layout, so it does not currently run.

## 2. Target architecture

```text
   apps/mobile (Expo, EAS)                 apps/web (Next.js on Vercel)
   ├─ @clerk/expo                          ├─ @clerk/nextjs (middleware + components)
   └─ @arth/api-client + TanStack Query    └─ @arth/api-client + TanStack Query
              │                                         │
              └──── HTTPS  Authorization: Bearer <Clerk session JWT> ────┘
                                       │
                                       ▼
        ┌──────────────────── apps/api  cmd/api  (Go, Docker on Railway/Render) ───────────────┐
        │ chi router → middleware: request-id · slog · recover · CORS · Clerk JWT (JWKS cache)  │
        │            → oapi-codegen strict handlers (generated from api/openapi.yaml)           │
        │            → services (business rules, DB transactions, money)                        │
        │            → sqlc queries (pgx pool)          every query: WHERE user_id = $auth      │
        │ Gemini Go SDK: receipt extraction (MVP), voice + assistant (post-MVP)                 │
        │ River client (insert-only): enqueue jobs inside the same DB transaction               │
        └───────┬───────────────────────────────┬─────────────────────────────┬────────────────┘
                │ pgx (pooled URL)              │ JWKS + Backend API          │ HTTPS
                ▼                               ▼                             ▼
        Neon PostgreSQL                       Clerk                       Gemini API
        app tables + River tables               ▲
                ▲                               │
                │ pgx (direct URL: LISTEN/NOTIFY)
        ┌───────┴──── apps/api  cmd/worker  (same image, different entrypoint) ────┐
        │ River workers + periodic jobs                                            │
        │  budget_alerts_scan → budget_alert_check(user) → send_email              │
        │  weekly_tips_scan   → weekly_tips(user) → Gemini → send_email            │
        │  balance_reconcile (weekly), attachment_sweeper (post-MVP)               │
        └──────────────┬──────────────────────────────┬────────────────────────────┘
                       ▼                              ▼
                    Resend                        Gemini API

        (post-MVP) S3-compatible object storage for receipt attachments: provider TBD
```

## 3. How the pieces connect

| From → To | Mechanism | Notes |
| --- | --- | --- |
| Mobile/Web → Clerk | Clerk SDKs | Sign-in, sign-up, MFA, avatar. Unchanged from Welth. |
| Mobile/Web → API | HTTPS JSON, `Authorization: Bearer <session JWT>` from `getToken()` | Short-lived tokens; the SDK refreshes them. Web calls the API cross-origin, so CORS uses an allowlist. |
| API → Clerk | JWKS (cached) to verify tokens; Backend API to fetch email/name on first provisioning | Checks `iss`, `exp`/`nbf`, and `azp` when present. `sub` = Clerk user id → mapped to internal `users.id`. |
| API → Neon | pgx pool over the **pooled** connection string | Every user-data query includes `user_id`. Multi-statement writes run in one DB transaction. |
| Worker → Neon | pgx over the **direct** (unpooled) connection string | River needs LISTEN/NOTIFY, which transaction-mode poolers do not support. Goose migrations also use direct. |
| API → Worker | River `InsertTx` in the same transaction as the triggering write | No dual-write problem: if the write rolls back, the job is never enqueued. |
| API/Worker → Gemini | Official Go SDK, key in server env only | Timeouts, retries, per-user rate limits. Structured JSON output with a response schema. |
| Worker → Resend | Resend Go SDK | Dedupe through the `notifications` table plus a Resend idempotency key; `html/template` escaping. |
| API → Storage (post-MVP) | Presigned PUT/GET URLs | Client uploads directly. The API stores only metadata in `attachments`. |
| CI → Railway/Render, Vercel, EAS | GitHub Actions (path-filtered) + provider Git integrations | See `docs/DEPLOYMENT.md`. |

## 4. Trust boundaries & secrets

- Clients hold only **publishable** values: the Clerk publishable key and the API base URL. Nothing else.
- The API holds: DB URLs, Clerk secret key, Gemini key.
  The worker holds: DB direct URL, Resend key, Gemini key, unsubscribe signing key.
- Authorization is application-level: middleware sets the `user_id` in the request context, and sqlc queries require it.
  Defence in depth comes from composite foreign keys (`transactions(account_id, user_id) → accounts(id, user_id)`), so the DB itself
  rejects cross-user references (see `docs/SCHEMA.md`).
- Supabase RLS goes away. The Go API connects as a non-superuser app role that owns nothing except DML rights.
  The migration role is separate.

## 5. Request lifecycle (example: create expense)

1. Mobile `POST /v1/transactions {account_id, category_id, type, amount_minor, occurred_on, …}` with a Bearer token.
2. Middleware verifies the JWT, resolves `users.id` from `sub` (indexed lookup, small in-process cache), and puts it in the context.
3. The generated handler validates the request against the OpenAPI schema. The service begins a DB transaction:
   lock the account (`… WHERE id=$1 AND user_id=$2 FOR UPDATE`), check that category ownership and kind match the currency,
   insert the transaction, run `UPDATE accounts SET balance_minor = balance_minor + delta`, and
   enqueue `budget_alert_check(user)` (unique per user per 10 min). Then commit.
4. The response is `201` with the created transaction and the new account balance. The client invalidates the `transactions`, `accounts`
   and `dashboard` queries.

## 6. Cross-cutting

- **Observability:** JSON slog logs with `request_id` and `user_id` (never amounts, descriptions, or PII in logs at info level).
  River job logs. Error tracking: decide in Month 5 (needs a dependency).
- **Rate limiting:** per-user on `/v1/ai/*`, coarse per-IP on everything else.
- **Versioning:** `/v1` path prefix. Changes are additive only. Mobile builds in the field are supported for at least 6 months.
- **Timezones:** `users.timezone` (IANA) drives "this month", "this week" and email send times.
