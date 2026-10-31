# HTTP API (v1)

> **`api/openapi.yaml` is the single source of truth.** This page is the human overview and the design
> conventions. Change the spec first, regenerate the Go stubs (oapi-codegen) and the TS client (`@arth/api-client`),
> then implement. If this page and the spec disagree, the spec wins. Fix this page.

## 1. Conventions

| Topic | Rule |
| --- | --- |
| Base URL | `https://api.<domain>/v1` (staging: `https://api.staging.<domain>/v1`; local: `http://localhost:8080/v1`) |
| Auth | Own auth (`docs/AUTH.md`). `Authorization: Bearer <access token>` on everything under `/v1` except `POST /v1/auth/register`, `login`, `refresh` and `/v1/public/*`. Missing, invalid or expired → `401` (the client refreshes once, then signs out). Web transport may become a cookie (OPEN). |
| Ownership | The server derives `user_id` from the token. Clients never send `user_id`. Another user's resource → `404` (not `403`). |
| Format | JSON, UTF-8, `snake_case` fields. Unknown request fields → `400`. Responses may gain fields at any time (clients must ignore unknown fields). |
| IDs | UUID strings. |
| Money | `amount_minor` (integer, minor units) + `currency` (ISO 4217). `0 < amount_minor ≤ 999 999 999 999 999` (safe in JS numbers). Never decimals or floats in JSON. |
| Dates | Calendar dates `YYYY-MM-DD` (`occurred_on`, `from`, `to`; `to` inclusive). Months `YYYY-MM`. Timestamps RFC 3339 UTC (`created_at`). "Today"/"this month" use `users.timezone`. |
| Single resource | Returned bare: `{ "id": …, … }`. |
| Collections | `{ "data": [...] }`. Paginated collections add `"next_cursor": "…" \| null`. |
| Pagination | Cursor (keyset): `?limit=50&cursor=<opaque>`. `limit` 1–200, default 50. Cursors are opaque base64 and are invalidated by filter changes (send the same filters with a cursor). No offset pagination. |
| Filtering | Plain query params, AND-combined: `type`, `account_id`, `category_id`, `from`, `to`, `q` (substring on description, ≤ 100 chars). Repeatable params (`account_id=a&account_id=b`) where the spec says so. |
| Sorting | Fixed per endpoint for MVP (transactions: `occurred_on desc, created_at desc, id desc`). |
| Writes | `POST` creates (`201` + body + `Location`). `PATCH` partially updates (omitted = unchanged, `null` = clear where allowed). `PUT` upserts singletons (budgets). `DELETE` → `204`. |
| Idempotency | `Idempotency-Key: <uuid>` accepted on `POST /v1/transactions`, `/v1/transfers`, `/v1/ai/*` (implemented in the hardening phase; ignored before then). |
| Tracing | The response always carries `X-Request-Id` (echoed if the client sends one). |
| Rate limits | `/v1/auth/*`: per IP and per email (`docs/AUTH.md` §5). `/v1/ai/*`: per user (initial: 30/hour, 100/day). `429` + `Retry-After`. |
| CORS | Allowlist of web origins (configured per environment). Mobile is not subject to CORS. |
| Versioning | Additive changes only within `/v1`. Breaking change → `/v2` running in parallel. Old mobile builds supported ≥ 6 months. |
| Uploads | `multipart/form-data`, a single `file` part. Limits per endpoint. Wrong type → `415`, too big → `413`. |

## 2. Error format (RFC 9457, `application/problem+json`)

```json
{
  "type": "https://api.<domain>/problems/validation_failed",
  "title": "Request validation failed",
  "status": 422,
  "code": "validation_failed",
  "detail": "amount_minor must be greater than 0",
  "errors": [{ "field": "amount_minor", "message": "must be greater than 0" }],
  "request_id": "01JB…"
}
```

`code` is the stable, machine-readable field that clients switch on. `title`/`detail` are for humans and may change.
Internal errors return `500` with `code: internal` and no internals. Details go to the logs under `request_id`.

| Status | `code` | When |
| --- | --- | --- |
| 400 | `bad_request` | Malformed JSON, unknown field, bad cursor |
| 401 | `unauthorized` | Missing, invalid or expired access token |
| 401 | `invalid_credentials` | Login with a wrong email or password (same response for both) |
| 401 | `refresh_invalid` | Refresh token unknown, expired, revoked, or reused (reuse also revokes the session) |
| 409 | `email_taken` | Register with an existing email (enumeration trade-off: `docs/AUTH.md` §10) |
| 422 | `weak_password` | Password fails the policy (`docs/AUTH.md` §4) |
| 404 | `not_found` | Missing **or not yours** |
| 409 | `conflict` | Generic state conflict |
| 409 | `account_has_transactions` | `DELETE /accounts/{id}` without `cascade=true`. Extra field `transaction_count` |
| 409 | `default_account` | Deleting the default account |
| 409 | `currency_locked` | Changing currency after transactions exist |
| 409 | `currency_mismatch` | Transaction currency ≠ account currency |
| 409 | `already_onboarded` | Repeat onboarding |
| 413 | `payload_too_large` | Upload over the limit |
| 415 | `unsupported_media_type` | Upload type not allowed |
| 422 | `validation_failed` | Schema-valid but semantically invalid (`errors[]` populated) |
| 429 | `rate_limited` | See rate limits |
| 502 | `ai_unavailable` | The AI provider (OPEN) failed or timed out after retries |
| 500 | `internal` | Anything else |

## 3. Resources & endpoints

MVP = needed for launch. POST = post-MVP. Each row maps to a row in `docs/MIGRATION.md` (§1–§4).

### System

| Method | Path | Notes | Scope |
| --- | --- | --- | --- |
| GET | `/healthz` | Liveness, no DB | MVP |
| GET | `/readyz` | DB ping + migrations at the expected version | MVP |

### Auth (own; design in `docs/AUTH.md`)

| Method | Path | Body → Response | Auth | Scope |
| --- | --- | --- | --- | --- |
| POST | `/v1/auth/register` | `{email, password, name, client: MOBILE\|WEB}` → `201 TokenPair` | none, rate-limited | MVP |
| POST | `/v1/auth/login` | `{email, password, client}` → `TokenPair` (`401 invalid_credentials` on failure) | none, rate-limited | MVP |
| POST | `/v1/auth/refresh` | `{refresh_token}` → `TokenPair` (rotated; old token now used) | refresh token | MVP |
| POST | `/v1/auth/logout` | `{refresh_token}` → `204` (revokes this session) | access token | MVP |
| POST | `/v1/auth/logout-all` | → `204` (revokes every session of the user) | access token | MVP |
| POST | `/v1/auth/password` | `{current_password, new_password}` → `204` (revokes other sessions) | access token | MVP |
| POST | `/v1/auth/verify-email`, `/v1/auth/verify-email/resend` | `{token}` / — → `204` | — / access | OPEN (email) |
| POST | `/v1/auth/password-reset/request`, `/v1/auth/password-reset/confirm` | `{email}` → `202` always / `{token, new_password}` → `204` | none, rate-limited | OPEN (email) |
| GET / DELETE | `/v1/auth/sessions`, `/v1/auth/sessions/{id}` | list devices / revoke one | access token | POST |

`TokenPair = {access_token, access_expires_in (seconds), refresh_token, refresh_expires_at, user: Me}`.
`register` also creates the default "Cash" account in the same transaction. If web ends up using cookies (OPEN), the web variants set
httpOnly cookies instead of returning tokens in the body.

### Me (current user)

| Method | Path | Body → Response | Scope |
| --- | --- | --- | --- |
| GET | `/v1/me` | → `Me` | MVP |
| PATCH | `/v1/me` | `{name?, currency?, timezone?, email_budget_alerts?, email_weekly_tips?}` → `Me` | MVP |
| POST | `/v1/me/onboarding` | `{currency, timezone, opening_balance_minor}` → `Me` | MVP |
| DELETE | `/v1/me` | `{password}` → `204`. Deletes all data and revokes all sessions | MVP |
| PUT | `/v1/me/avatar` | multipart `file` → `Me` | OPEN (storage) |
| GET | `/v1/me/export` | Full JSON data export | POST |

`Me = {id, email, email_verified, name, avatar_url|null, currency|null, timezone, needs_onboarding, email_budget_alerts, email_weekly_tips, created_at}`

### Categories

| Method | Path | Notes | Scope |
| --- | --- | --- | --- |
| GET | `/v1/categories?kind=INCOME\|EXPENSE` | System + own, ordered by `sort_order` | MVP |
| POST / PATCH / DELETE | `/v1/categories[/{id}]` | Custom categories (delete = archive if used) | POST |

`Category = {id, key|null, name, kind, icon, color, is_system}`

### Accounts

| Method | Path | Body → Response | Scope |
| --- | --- | --- | --- |
| GET | `/v1/accounts` | → `{data: Account[]}` (default first, then `created_at`) | MVP |
| POST | `/v1/accounts` | `{name, type, currency?, opening_balance_minor?}` → `201 Account` | MVP |
| GET | `/v1/accounts/{id}` | → `Account` | MVP |
| PATCH | `/v1/accounts/{id}` | `{name?, type?}` → `Account` | MVP |
| POST | `/v1/accounts/{id}/make-default` | → `Account` | MVP |
| DELETE | `/v1/accounts/{id}[?cascade=true]` | → `204` or `409 account_has_transactions` | MVP |

`Account = {id, name, type: CASH|BANK|CREDIT_CARD|SAVINGS, currency, balance_minor, opening_balance_minor, is_default, created_at}`

### Transactions

| Method | Path | Body → Response | Scope |
| --- | --- | --- | --- |
| GET | `/v1/transactions` | `?type&account_id&category_id&from&to&q&limit&cursor` → `{data: Transaction[], next_cursor}` | MVP |
| POST | `/v1/transactions` | `{account_id, category_id, type: INCOME\|EXPENSE, amount_minor, occurred_on, description?, input_method?, voice_transcript?}` → `201 {transaction, account}` | MVP |
| GET | `/v1/transactions/{id}` | → `Transaction` | MVP |
| PATCH | `/v1/transactions/{id}` | Any create field → `{transaction, accounts: Account[]}` (1 or 2 affected) | MVP |
| DELETE | `/v1/transactions/{id}` | → `204` (a transfer leg deletes the whole transfer) | MVP |
| GET | `/v1/transactions/export?format=csv&from&to&account_id` | → `text/csv` attachment. Max range 366 days | MVP |

`Transaction = {id, account_id, category_id|null, type, amount_minor, currency, occurred_on, description|null, input_method, voice_transcript|null, transfer_id|null, created_at, updated_at}`

The currency is taken from the account. Clients do not send it.

### Transfers

| Method | Path | Body → Response | Scope |
| --- | --- | --- | --- |
| POST | `/v1/transfers` | `{from_account_id, to_account_id, amount_minor, occurred_on, description?}` → `201 {transfer_id, legs: Transaction[2], accounts: Account[2]}` | MVP-stretch |
| DELETE | `/v1/transfers/{transfer_id}` | → `204` | MVP-stretch |

Same-currency only until multi-currency.

### Budgets

| Method | Path | Body → Response | Scope |
| --- | --- | --- | --- |
| GET | `/v1/budgets?month=YYYY-MM` | → `{data: BudgetProgress[]}` (default month = current local month) | MVP |
| PUT | `/v1/budgets/overall` | `{amount_minor}` → `BudgetProgress` | MVP |
| DELETE | `/v1/budgets/overall` | → `204` | MVP |
| PUT / DELETE | `/v1/budgets/categories/{category_id}` | `{amount_minor}` → `BudgetProgress` | API in MVP, UI POST |

`BudgetProgress = {id, category_id|null, amount_minor, currency, month, spent_minor, remaining_minor, percent_used}`

### Dashboard & reports

| Method | Path | Response | Scope |
| --- | --- | --- | --- |
| GET | `/v1/dashboard?month=YYYY-MM` | `{currency, total_balance_minor, month: {income_minor, expense_minor, net_minor}, budget: BudgetProgress\|null, expense_by_category: [{category_id, amount_minor}], recent: Transaction[5]}` | MVP |
| GET | `/v1/reports/daily?from&to&account_id?` | `{data: [{date, income_minor, expense_minor}]}`, zero-filled, ≤ 93 days | MVP |
| GET | `/v1/reports/monthly?months=12` | Month totals for trend charts | POST |

Transfers are excluded from all income/expense aggregates.

### AI

| Method | Path | Body → Response | Scope |
| --- | --- | --- | --- |
| POST | `/v1/ai/receipt-extractions` | multipart `file` (jpeg/png/webp/heic, ≤ 5 MB) → `Extraction` | MVP |
| POST | `/v1/ai/voice-extractions` | multipart `file` (m4a/aac/mp3/wav, ≤ 60 s / 2 MB) → `Extraction` incl. `transcript` | POST |
| POST | `/v1/assistant/messages` | `{message (≤ 500 chars), history?: [{role, content}] (≤ 10)}` → `{reply}` | POST |

`Extraction = {type|null, amount_minor|null, currency, category_id|null, category_key|null, description|null, occurred_on|null, transcript|null}`.
Nothing is persisted. The client reviews the result and then calls `POST /v1/transactions`.

### Email preferences (public)

| Method | Path | Notes | Scope |
| --- | --- | --- | --- |
| POST | `/v1/public/email-preferences/unsubscribe` | `{token}` (HMAC-signed, from the email link) → `204`. Used by the web `/unsubscribe` page and the `List-Unsubscribe-Post` one-click | MVP |

### Post-MVP resources (shape TBD in the spec when built)

`/v1/recurring-rules` (CRUD + `pause`/`resume`), `/v1/goals` (CRUD), `/v1/attachments` (presigned upload: `POST` → `{upload_url, attachment_id}`,
then link it on the transaction; needs object storage, OPEN), `/v1/devices` (push tokens; push provider OPEN).

## 4. Client generation

- Go: `oapi-codegen` with chi server + strict-server + models → `apps/api/internal/httpapi/gen/` (never hand-edited).
- TS: `packages/api-client` generated from the same file (generator choice pending, see the open question in the roadmap).
  It exports types, `createApiClient({ baseUrl, getToken })`, `ApiError`, and money helpers (`@arth/api-client/money`).
- CI fails if regenerating either one produces a diff.
