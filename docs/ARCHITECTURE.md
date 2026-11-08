# Architecture

§1 = the Welth design the app started from (historical since 2026-11-01). §2 = the decided design (`docs/DECISIONS.md`), which is now
built for `apps/api` and `apps/mobile` (own auth, Go API on Postgres, `@arth/api-client`); `apps/web` and the worker are not built yet.

## 1. Before the repoint (Welth design, historical)

```text
                 ┌──────────────────── apps/mobile (Expo SDK 54) ─────────────────────┐
                 │ Expo Router · NativeWind · TanStack Query · Zustand · RHF + zod    │
   sign-in/up ◄──┤ @clerk/expo (ClerkProvider, useSignIn/useSignUp, tokenCache)  ────┼──► Clerk (SaaS)
                 │ supabase-js + Clerk session JWT      lib/supabase.ts, hooks/,      │
                 │                                      lib/services/*, 3 screens     │
                 │ fetch …/gemini…:generateContent?key=EXPO_PUBLIC_GEMINI_API_KEY ────┼──► Gemini API
                 └──────────┬─────────────────────────────────────────────────────────┘
                            │ PostgREST (HTTPS), RLS: auth.jwt()->>'sub'
                            ▼
              Supabase Postgres: users · accounts · transactions · budgets
                            ▲  service-role key
   pg_cron → pg_net → Supabase Edge Functions (Deno; source now in legacy/supabase/functions)
                        ├─ check-budget-alerts (daily 09:00 UTC) ──────────► Resend
                        └─ weekly-tips (Mon 09:00 UTC) ─► Gemini ──────────► Resend
```

- **Auth:** Clerk owns users and sessions. Supabase trusts Clerk's JWT, and RLS compares `sub` with `user_id`.
- **Data:** all business logic runs on the device (balance updates, onboarding, default account, user provisioning). Every screen loads
  every transaction and aggregates client-side.
- **AI:** the Gemini key is compiled into the app bundle.
- **Jobs:** two Deno edge functions triggered by pg_cron, emailing through Resend.
- **Storage:** none of its own. Avatars live in Clerk. Receipt and voice data are sent to Gemini and never stored.

Known structural issues: `docs/MIGRATION.md` §6.

## 2. Target

```text
   apps/mobile (Expo, EAS Build)                   apps/web (Next.js)
   ├─ own AuthProvider: tokens in expo-secure-store ├─ own auth (transport OPEN: cookie vs bearer)
   └─ @arth/api-client + TanStack Query             └─ @arth/api-client + TanStack Query
              │                                              │
              └──────── HTTPS  /v1/*   (access token) ───────┘
                                   │
                                   ▼
     ┌──────────────── apps/api · cmd/api (Go, Docker) ─────────────────────────────┐
     │ chi → middleware: request-id · slog · recover · CORS · rate-limit · auth      │
     │     → handlers generated from api/openapi.yaml (oapi-codegen)                 │
     │     → services (rules, money, DB transactions)                                │
     │     → sqlc queries (pgx)          every user query: WHERE user_id = $auth     │
     │ internal/auth: Argon2id passwords · access JWT · rotating refresh tokens       │
     │ internal/provider/{ai,email,storage,push}: INTERFACES (+ fake drivers)        │
     └──────────────┬───────────────────────────────────────────┬───────────────────┘
                    │ pgx                                        │ adapters only after
                    ▼                                            │ each OPEN item is decided
             PostgreSQL 16  ◄──── cmd/worker (Postgres-backed job queue)
             (local: docker-compose; production host: OPEN)
                                   │  jobs: budget alerts · weekly tips · reconcile
                                   ▼
                      LLM provider (OPEN) · Email provider (OPEN) · Object storage (OPEN) · Push (OPEN)
```

## 3. How the pieces connect

| From → To | Mechanism | Notes |
| --- | --- | --- |
| Mobile → API | HTTPS JSON. `Authorization: Bearer <access token>`. Refresh via `POST /v1/auth/refresh` | Tokens stored in `expo-secure-store` (already a dependency). The client retries once after a refresh. |
| Web → API | HTTPS JSON, cross-origin with a CORS allowlist | Token transport is OPEN (`docs/DECISIONS.md` D-OPEN-6). |
| API → Postgres | pgx pool. sqlc only. One DB transaction per multi-statement write | Composite FKs make cross-user references impossible (`docs/SCHEMA.md`). |
| API → Worker | Job inserted in the **same** DB transaction as the triggering write | Postgres-backed queue (River proposed), so no Redis. |
| API/Worker → providers | Go interfaces in `internal/provider/*`, selected by `*_DRIVER` env vars | `fake`/`log` drivers for dev and tests. Real adapters wait for the decisions. |
| Clients → providers | **Never** | No provider keys or SDKs in clients. |

## 4. Trust boundaries

- Clients hold no secrets. They hold only the API base URL and their own tokens.
- The API alone verifies passwords and issues tokens. The user id always comes from the verified access token, never from a request body.
- Authorization is application-level: `user_id` comes from the auth middleware, and every sqlc query requires it. RLS is not used.
- The DB has a migration role (DDL) and an app role (DML only).
- Provider credentials (when chosen) live only in API/worker env vars.

## 5. Request lifecycle (create expense)

1. Mobile `POST /v1/transactions` with an access token.
2. Middleware verifies the token signature, algorithm, `exp`, `iss` and `aud`, and puts `user_id` in the context. An expired token gets a 401;
   the client calls `/v1/auth/refresh` and retries once.
3. The service opens a DB transaction: lock the account (`WHERE id = $1 AND user_id = $2`), validate the category, insert the transaction,
   `balance_minor = balance_minor + delta`, enqueue `budget_alert_check(user)`, commit.
4. `201` with the transaction and the updated account. The client invalidates its `transactions`, `accounts` and `dashboard` queries.
