# HTTP API (v1)

> **`api/openapi.yaml` (OpenAPI 3.1) is the single source of truth.** This page is the human overview and the design
> conventions. Change the spec first, regenerate the Go stubs (oapi-codegen) and the TS client (`@arth/api-client`),
> then implement. If this page and the spec disagree, the spec wins. Fix this page.

## 1. Conventions

| Topic | Rule |
| --- | --- |
| Base URL | `https://api.<domain>` (staging: `https://api.staging.<domain>`; local: `http://localhost:8080`). Resource paths start with `/v1`; the probes `/healthz` and `/readyz` sit at the root. |
| Auth | Own auth (`docs/AUTH.md`). `Authorization: Bearer <access token>` on everything except `/healthz`, `/readyz` and `POST /v1/auth/register`, `login`, `refresh` (and `/v1/public/*` once it exists). Missing, invalid or expired → `401 unauthorized`. Web transport may become a cookie (OPEN, D-OPEN-6). |
| 401 handling | Clients refresh and retry **only** on code `unauthorized` (one serialised refresh, then retry once; sign out if the refresh fails). Never refresh on `invalid_credentials` (wrong password: show the error) or `refresh_invalid` (sign out). |
| Ownership | The server derives `user_id` from the token. Clients never send `user_id`. Another user's resource → `404 not_found` (not `403`). |
| Format | JSON, UTF-8, `snake_case` fields. Unknown request fields → `400 bad_request`. A JSON body with another `Content-Type` → `415 unsupported_media_type`. JSON bodies are capped at 1 MiB (`413 payload_too_large`). Responses may gain fields at any time (clients must ignore unknown fields). |
| IDs | UUID strings. A malformed id in the path → `422 validation_failed` (field `path.id`). |
| Money | `*_minor` integers in minor units of `currency` (ISO 4217, server allowlist). Transaction and budget amounts: `0 < amount_minor ≤ 999 999 999 999 999` (safe in JS numbers). `opening_balance_minor` may be negative, within `±999 999 999 999 999`. Balances, `remaining_minor` and `net_minor` may be negative. Never decimals or floats in JSON. |
| Dates | Calendar dates `YYYY-MM-DD` (`occurred_on`, `from`, `to`; ranges inclusive). Months `YYYY-MM`. Timestamps RFC 3339 UTC (`created_at`). "Today"/"this month" use the user's timezone (`Me.timezone`). In a **query parameter**, a wrong shape (`from=yesterday`) → `422 validation_failed`, but a well-shaped impossible date (`from=2026-02-30`) passes the spec's pattern and fails in the generated parameter binder → `400 bad_request`. |
| Single resource | Returned bare: `{ "id": …, … }`. |
| Collections | `{ "data": [...] }`. Paginated collections add `"next_cursor": "…" \| null`. |
| Pagination | Cursor (keyset): `?limit=50&cursor=<opaque>`. `limit` 1–200, default 50. `cursor` is an opaque string of 1–512 characters (do not parse it). Internally it is base64url (no padding) of a small JSON object: the last row's `(occurred_on, created_at, id)` plus a fingerprint of the filters it was issued for (`type`, `account_id`, `category_id`, `from`, `to`, `q`; **not** `limit`, so the page size may change mid-scroll). Changing any filter while sending an old cursor, or a malformed cursor → `400 bad_request`. No offset pagination. |
| Filtering | Plain query params, AND-combined. Transactions: `type` (`INCOME\|EXPENSE\|TRANSFER_IN\|TRANSFER_OUT`), `account_id`, `category_id`, `from`, `to`, `q` (case-insensitive substring of the description, 1–100 chars). Every filter is **single-valued** (no repeatable params in v1). |
| Sorting | Fixed per endpoint for MVP (transactions: `occurred_on desc, created_at desc, id desc`). |
| Writes | `POST` creates a resource → `201` + body + `Location` (register, accounts, transactions); action endpoints (`onboarding`, `make-default`, AI) return `200`, sign-out endpoints `204`. `PATCH` partially updates: omitted = unchanged, **`null` is not accepted** (no null-clearing, D-20). `""` clears only a transaction's `description` (and `voice_transcript`); it does **not** clear `name` on `PATCH /v1/me` (`minLength: 1`; blank or whitespace-only → `422`), so a name cannot be removed once set. `PUT` upserts singletons (budgets). `DELETE` → `204` (budget deletes are idempotent: `204` even when none was set). |
| Idempotency | `Idempotency-Key` is **not implemented yet** (hardening phase, `docs/ROADMAP.md` Phase 6). The header passes CORS but is ignored. Planned for `POST /v1/transactions`, `/v1/ai/*` and transfers once they exist. |
| Tracing | **Every** response carries `X-Request-Id`: the client's value is echoed if it is ≤ 64 chars of `[A-Za-z0-9_-]`, otherwise the server generates one. Problem bodies repeat it as `request_id`. |
| Rate limits | `429 rate_limited` + `Retry-After` (seconds) on register (per IP only, 5/hour), login (5 failures/15 min per email, 20/15 min per IP), refresh (60/hour per session), `POST /v1/auth/password` and `DELETE /v1/me` (these share the **login** buckets: every attempt uses the caller's per-IP login bucket, a wrong password also counts as a failed login for the account's email). Details: `docs/AUTH.md` §5 (in memory, single instance: DECISIONS Q-10). `POST /v1/ai/receipt-extractions` declares `429`, but the per-user AI limit (proposal 30/hour, 100/day) is **not implemented yet**. |
| CORS | Allowlist of web origins (configured per environment). Allowed request headers: `Authorization, Content-Type, Idempotency-Key, X-Request-Id`. Exposed: `X-Request-Id, Retry-After, Location, Content-Disposition`. Mobile is not subject to CORS. |
| Unknown routes | Unknown path → `404 not_found`. Known path, wrong method → `405 method_not_allowed`. Both are problem+json. |
| Versioning | Additive changes only within `/v1`. Breaking change → `/v2` running in parallel. Old mobile builds supported ≥ 6 months. |
| Uploads | `multipart/form-data`, a single `file` part. Limits per endpoint. Wrong type → `415 unsupported_media_type`, too big → `413 payload_too_large`. A body that is not multipart at all (e.g. JSON) or a malformed multipart body → `400 bad_request` (the generated multipart decoder rejects it). |

## 2. Error format (RFC 9457, `application/problem+json`)

```json
{
  "type": "/problems/validation_failed",
  "title": "Request validation failed",
  "status": 422,
  "code": "validation_failed",
  "detail": "amount_minor must be at least 1",
  "errors": [{ "field": "amount_minor", "message": "must be at least 1" }],
  "request_id": "…"
}
```

`code` is the stable, machine-readable field that clients switch on. `title`/`detail` are for humans and may change.
`type` is the relative reference `/problems/<code>`. `errors[].field` is the body path (`amount_minor`, `items.0.name`) or
`<in>.<name>` for parameters (`query.limit`, `path.id`). Messages never echo the submitted value.
Some problems carry an extra member (`transaction_count`, `reason`). Internal errors return `500` with `code: internal`, no `detail`
and no internals. Details go to the logs under `request_id`.

| Status | `code` | When |
| --- | --- | --- |
| 400 | `bad_request` | Malformed JSON, unknown field, missing body, bad or mismatched cursor, an impossible date in a query parameter (`2026-02-30`), a non-multipart body on the receipt upload |
| 401 | `unauthorized` | Missing, invalid or expired access token (or its session is revoked). The **only** code that triggers refresh-and-retry |
| 401 | `invalid_credentials` | Login with a wrong email or password (same response for both); wrong `current_password` on `POST /v1/auth/password`; wrong `password` on `DELETE /v1/me`. Not a session problem: do not refresh or sign out |
| 401 | `refresh_invalid` | Refresh token unknown, expired, revoked, or reused (reuse also revokes the session). The client signs out |
| 404 | `not_found` | Missing **or not yours**; also unknown routes |
| 405 | `method_not_allowed` | Known path, unsupported method |
| 409 | `email_taken` | Register with an existing email, case-insensitive (enumeration trade-off: `docs/AUTH.md` §10) |
| 409 | `conflict` | Generic state conflict (e.g. editing a transfer leg with `PATCH /v1/transactions/{id}`) |
| 409 | `account_has_transactions` | `DELETE /v1/accounts/{id}` without `cascade=true`. Extra member `transaction_count` |
| 409 | `default_account` | Deleting the default account |
| 409 | `currency_locked` | Onboarding would change the default account's currency, but that account already has transactions (same currency is fine); or `PATCH /v1/me` changes `currency` while the user has any transaction |
| 409 | `currency_mismatch` | `PATCH /v1/transactions/{id}` moves the transaction (`account_id`) to an account with a different currency |
| 409 | `already_onboarded` | Repeat onboarding |
| 413 | `payload_too_large` | Upload over its limit, or a JSON body over 1 MiB |
| 415 | `unsupported_media_type` | Upload type not allowed, or a request body whose `Content-Type` the operation does not accept |
| 422 | `validation_failed` | Schema or semantic violation (`errors[]` populated) |
| 422 | `weak_password` | Password fails the policy (8–128 characters, not a common password; `docs/AUTH.md` §4) |
| 429 | `rate_limited` | See rate limits. `Retry-After` header |
| 502 | `ai_unavailable` | The AI provider (OPEN) failed or timed out after retries |
| 503 | `not_ready` | `/readyz` only. Extra member `reason`: `database_unavailable` or `migrations_pending` |
| 500 | `internal` | Anything else |

## 3. Resources & endpoints

MVP = needed for launch. POST = post-MVP. Each row maps to a row in `docs/MIGRATION.md` (§1–§4).
**Status:** `In spec` = the operation is in `api/openapi.yaml`. `Not in spec yet (reason)` = deliberately left out of the contract
for now (`docs/DECISIONS.md` D-21); the shape shown is the plan, and it goes into the spec first when it is built.

### System

| Method | Path | Notes | Scope | Status |
| --- | --- | --- | --- | --- |
| GET | `/healthz` | Liveness, no DB → `200 {status: "ok"}` | MVP | In spec |
| GET | `/readyz` | DB ping + migrations at the expected version → `200 {status: "ready"}`, else `503 not_ready` problem with `reason` | MVP | In spec |

### Auth (own; design in `docs/AUTH.md`)

| Method | Path | Body → Response | Auth | Scope | Status |
| --- | --- | --- | --- | --- | --- |
| POST | `/v1/auth/register` | `{email (3–254), password, name (1–100, required), client: MOBILE\|WEB}` → `201 TokenPair` + `Location`. `409 email_taken`, `422 weak_password` | none, rate-limited per IP only (5/hour) | MVP | In spec |
| POST | `/v1/auth/login` | `{email, password, client}` → `200 TokenPair` (`401 invalid_credentials` on failure) | none, rate-limited | MVP | In spec |
| POST | `/v1/auth/refresh` | `{refresh_token}` → `200 TokenPair` (rotated; the old token is now used). `401 refresh_invalid` | none (refresh token in the body), rate-limited | MVP | In spec |
| POST | `/v1/auth/logout` | `{refresh_token}` → `204`. Revokes the session that owns the token **only if it is the caller's**. Already revoked, unknown, malformed or another user's token → `204` no-op (not an oracle) | access token | MVP | In spec |
| POST | `/v1/auth/logout-all` | → `204` (revokes every session of the user, including this one) | access token | MVP | In spec |
| POST | `/v1/auth/password` | `{current_password, new_password}` → `204` (revokes other sessions; this one continues). `401 invalid_credentials`, `422 weak_password`. Wrong passwords count against the login limits | access token, rate-limited | MVP | In spec |
| POST | `/v1/auth/verify-email`, `/v1/auth/verify-email/resend` | `{token}` / — → `204` | — / access | OPEN (email) | Not in spec yet (needs the email provider, D-OPEN-2) |
| POST | `/v1/auth/password-reset/request`, `/v1/auth/password-reset/confirm` | `{email}` → `202` always / `{token, new_password}` → `204` | none, rate-limited | OPEN (email) | Not in spec yet (needs the email provider, D-OPEN-2) |
| GET / DELETE | `/v1/auth/sessions`, `/v1/auth/sessions/{id}` | list devices / revoke one | access token | POST | Not in spec yet (post-MVP) |

`TokenPair = {access_token, access_expires_in (seconds), refresh_token, refresh_expires_at, user: Me}`.
`register` also creates the default "Cash" account (currency INR as a placeholder until onboarding, D-19) and the session in the same
DB transaction; the user starts with `needs_onboarding: true`. Passwords are 1–1024 characters at the schema level; the real policy is
checked by the server and reported as `422 weak_password`. If web ends up using cookies (OPEN), the web variants set httpOnly cookies
instead of returning tokens in the body.

### Me (current user)

| Method | Path | Body → Response | Scope | Status |
| --- | --- | --- | --- | --- |
| GET | `/v1/me` | → `Me` | MVP | In spec |
| PATCH | `/v1/me` | `{name? (1–100), currency?, timezone?, email_budget_alerts?, email_weekly_tips?}` → `Me`. Unsupported currency, invalid IANA zone or blank `name` → `422` (the name cannot be cleared). Changing the currency does not change existing accounts; changing it once the user has any transaction → `409 currency_locked` | MVP | In spec |
| POST | `/v1/me/onboarding` | `{currency, timezone, opening_balance_minor}` → `200 Me`. `409 already_onboarded`, `409 currency_locked` | MVP | In spec |
| DELETE | `/v1/me` | `{password}` → `204`. Deletes all data and revokes all sessions. Wrong password → `401 invalid_credentials` (counts against the login limits). Rate-limited | MVP | In spec |
| PUT | `/v1/me/avatar` | multipart `file` → `Me` | OPEN (storage) | Not in spec yet (needs object storage, D-OPEN-7) |
| GET | `/v1/me/export` | Full JSON data export | POST | Not in spec yet (post-MVP) |

`Me = {id, email, email_verified, name|null, avatar_url|null (always null until storage is decided), currency|null (null until onboarding), timezone, needs_onboarding, email_budget_alerts, email_weekly_tips, created_at}`

### Categories

| Method | Path | Notes | Scope | Status |
| --- | --- | --- | --- | --- |
| GET | `/v1/categories?kind=INCOME\|EXPENSE` | System + own (none in the MVP), ordered by `sort_order`. Archived ones omitted | MVP | In spec |
| POST / PATCH / DELETE | `/v1/categories[/{id}]` | Custom categories (delete = archive if used) | POST | Not in spec yet (post-MVP) |

`Category = {id, key|null, name, kind: INCOME|EXPENSE, icon|null, color|null (#RRGGBB), is_system}`

### Accounts

| Method | Path | Body → Response | Scope | Status |
| --- | --- | --- | --- | --- |
| GET | `/v1/accounts` | → `{data: Account[]}` (default first, then `created_at`) | MVP | In spec |
| POST | `/v1/accounts` | `{name (1–60), type, currency?, opening_balance_minor?}` → `201 Account` + `Location`. `currency` defaults to the user's (INR if none yet); `opening_balance_minor` defaults to 0 and may be negative. Never the default account | MVP | In spec |
| GET | `/v1/accounts/{id}` | → `Account` | MVP | In spec |
| PATCH | `/v1/accounts/{id}` | `{name?, type?}` → `Account` (currency and balances are not editable) | MVP | In spec |
| POST | `/v1/accounts/{id}/make-default` | → `200 Account` (the previous default is unset in the same DB transaction) | MVP | In spec |
| DELETE | `/v1/accounts/{id}[?cascade=true]` | → `204`, `409 default_account`, or `409 account_has_transactions` (`transaction_count`) unless `cascade=true` | MVP | In spec |

`Account = {id, name, type: CASH|BANK|CREDIT_CARD|SAVINGS, currency, balance_minor, opening_balance_minor, is_default, created_at}`

### Transactions

| Method | Path | Body → Response | Scope | Status |
| --- | --- | --- | --- | --- |
| GET | `/v1/transactions` | `?type&account_id&category_id&from&to&q&limit&cursor` → `{data: Transaction[], next_cursor}`. Bad cursor → `400` | MVP | In spec |
| POST | `/v1/transactions` | `{account_id, category_id, type: INCOME\|EXPENSE, amount_minor, occurred_on, description? (≤ 200), input_method?: MANUAL\|RECEIPT_SCAN\|VOICE, voice_transcript? (≤ 2000)}` → `201 {transaction, account}` + `Location`. Unknown account → `404`; category missing or of the wrong kind → `422`; `409` possible | MVP | In spec |
| GET | `/v1/transactions/{id}` | → `Transaction` | MVP | In spec |
| PATCH | `/v1/transactions/{id}` | Any create field → `{transaction, accounts: Account[]}` (1 or 2 affected). Moving it to an account with another currency → `409 currency_mismatch`; to an archived account or editing a transfer leg → `409 conflict` | MVP | In spec |
| DELETE | `/v1/transactions/{id}` | → `204` (a transfer leg deletes the whole transfer) | MVP | In spec |
| GET | `/v1/transactions/export?format=csv&from&to&account_id?` | `from`, `to` required → `Content-Type: text/csv; charset=utf-8` attachment (`Content-Disposition: attachment; filename="arth-transactions-<from>-<to>.csv"`), UTF-8 with a header row, amounts as decimal strings. Max range 366 days (`422`). Unknown account → `404` | MVP | In spec |

`Transaction = {id, account_id, category_id|null (null only for transfer legs), type: INCOME|EXPENSE|TRANSFER_IN|TRANSFER_OUT, amount_minor (always positive), currency, occurred_on, description|null, input_method: MANUAL|RECEIPT_SCAN|VOICE|RECURRING|IMPORT, voice_transcript|null, transfer_id|null, created_at, updated_at}`

The currency is taken from the account. Clients do not send it. Requests accept only `type` `INCOME|EXPENSE` and `input_method`
`MANUAL|RECEIPT_SCAN|VOICE`; the other values appear only in responses.

### Transfers

| Method | Path | Body → Response | Scope | Status |
| --- | --- | --- | --- | --- |
| POST | `/v1/transfers` | `{from_account_id, to_account_id, amount_minor, occurred_on, description?}` → `201 {transfer_id, legs: Transaction[2], accounts: Account[2]}` | MVP-stretch | Not in spec yet (stretch goal, not built this session: D-21) |
| DELETE | `/v1/transfers/{transfer_id}` | → `204` | MVP-stretch | Not in spec yet (stretch goal, not built this session: D-21) |

Same-currency only until multi-currency.

### Budgets

| Method | Path | Body → Response | Scope | Status |
| --- | --- | --- | --- | --- |
| GET | `/v1/budgets?month=YYYY-MM` | → `{data: BudgetProgress[]}` (overall first, then category budgets; default month = current local month). Spending counts EXPENSE only | MVP | In spec |
| PUT | `/v1/budgets/overall` | `{amount_minor}` → `200 BudgetProgress` (currency = the user's; progress for the current local month). `409` possible | MVP | In spec |
| DELETE | `/v1/budgets/overall` | → `204` (also when none was set) | MVP | In spec |
| PUT | `/v1/budgets/categories/{category_id}` | `{amount_minor}` → `200 BudgetProgress`. Unknown category → `404`; an INCOME category → `422`; `409` possible | API in MVP, UI POST | In spec |
| DELETE | `/v1/budgets/categories/{category_id}` | → `204` (also when none was set) | API in MVP, UI POST | In spec |

`BudgetProgress = {id, category_id|null (null = overall), amount_minor, currency, month, spent_minor, remaining_minor (amount − spent; negative when over), percent_used (floor(spent × 100 / amount); may exceed 100)}`

### Dashboard & reports

| Method | Path | Response | Scope | Status |
| --- | --- | --- | --- | --- |
| GET | `/v1/dashboard?month=YYYY-MM` | `{currency, total_balance_minor, month: {income_minor, expense_minor, net_minor}, budget: BudgetProgress\|null, expense_by_category: [{category_id, amount_minor}] (largest first), recent: Transaction[≤ 5]}` | MVP | In spec |
| GET | `/v1/reports/daily?from&to&account_id?` | `{data: [{date, income_minor, expense_minor}]}`, zero-filled, oldest first, ≤ 93 days (`422`). Default = all accounts in the user's currency; unknown account → `404` | MVP | In spec |
| GET | `/v1/reports/monthly?months=12` | Month totals for trend charts | POST | Not in spec yet (post-MVP) |

Dashboard totals are in the user's currency (INR before onboarding); `total_balance_minor` sums the accounts in that currency, and
`recent` holds the 5 most recent transactions of any month. Transfers are excluded from all income/expense aggregates.

### AI

| Method | Path | Body → Response | Scope | Status |
| --- | --- | --- | --- | --- |
| POST | `/v1/ai/receipt-extractions` | multipart `file` (JPEG/PNG/WebP/HEIC, ≤ 5 MB) → `200 Extraction`. `413`, `415`, `429`, `502 ai_unavailable` | MVP | In spec |
| POST | `/v1/ai/voice-extractions` | multipart `file` (m4a/aac/mp3/wav, ≤ 60 s / 2 MB) → `Extraction` incl. `transcript` | POST | Not in spec yet (post-MVP; LLM provider D-OPEN-1) |
| POST | `/v1/assistant/messages` | `{message (≤ 500 chars), history?: [{role, content}] (≤ 10)}` → `{reply}` | POST | Not in spec yet (post-MVP; LLM provider D-OPEN-1) |

`Extraction = {type|null, amount_minor|null, currency, category_id|null, category_key|null, description|null, occurred_on|null, transcript|null}`.
`currency` is the user's when the receipt does not say. `transcript` is always null for receipts.
Nothing is persisted. The client reviews the result and then calls `POST /v1/transactions`.

### Email preferences (public)

| Method | Path | Notes | Scope | Status |
| --- | --- | --- | --- | --- |
| POST | `/v1/public/email-preferences/unsubscribe` | `{token}` (HMAC-signed, from the email link) → `204`. Used by the web `/unsubscribe` page and the `List-Unsubscribe-Post` one-click | MVP | Not in spec yet (no emails are sent until D-OPEN-2 and the worker, D-OPEN-9) |

### Post-MVP resources (shape TBD in the spec when built)

`/v1/recurring-rules` (CRUD + `pause`/`resume`), `/v1/goals` (CRUD), `/v1/attachments` (presigned upload: `POST` → `{upload_url, attachment_id}`,
then link it on the transaction; needs object storage, OPEN), `/v1/devices` (push tokens; push provider OPEN).

## 4. Client generation

- Go: **oapi-codegen v2.8.1** (a Go `tool` in `apps/api/go.mod`), chi server + strict server + models + embedded spec
  (config `apps/api/internal/httpapi/gen/oapi-codegen.yaml`) → `apps/api/internal/httpapi/gen/gen.go` (never hand-edited).
  Regenerate from `apps/api` with `go generate ./internal/httpapi/gen/`. Handlers implement the generated `StrictServerInterface` in
  `apps/api/internal/httpapi/server_*.go`. Requests are validated against the embedded spec by kin-openapi `openapi3filter`
  middleware (D-17).
- TS: **openapi-typescript + openapi-fetch** (decided by the owner 2026-11-01, `docs/DECISIONS.md` D-OPEN-10). `packages/api-client`
  (`@arth/api-client`) is generated from the same file with `pnpm gen:api` (built in the mobile repoint, 2026-11-01). Exports: types,
  `createArthClient({ baseUrl, fetch })`, `ApiError` + `unwrap`, money helpers (`@arth/api-client/money`) and the session core
  (`@arth/api-client/session`: one serialized refresh, retry once on `401 unauthorized`).
- CI fails if regenerating either one produces a diff (once a remote and CI exist).
