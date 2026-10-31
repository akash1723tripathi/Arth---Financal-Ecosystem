# apps/api: Go API + worker

> **Status (2026-11-01, branch `feat/go-api`):** all 33 operations in `api/openapi.yaml` are implemented (auth, me/onboarding,
> categories, accounts, transactions, budgets, dashboard, daily report, CSV export, receipt extraction with the fake AI driver).
> `go test ./...` passes against real Postgres. **Not built:** transfers (only existing legs can be read/deleted), password reset /
> email verification, the worker (`cmd/worker` is a stub, D-22), per-user AI rate limit, Idempotency-Key, real providers.
> Ask before adding any Go module not listed under "Dependencies".

## Layout (actual)

```text
apps/api/
  cmd/api/main.go            HTTP server (graceful shutdown); subcommands: serve (default), migrate up|down|status
  cmd/worker/main.go         stub until the job queue is chosen (D-OPEN-9)
  internal/config/           env parsing + validation (fail fast at boot; loads ./.env itself)
  internal/httpapi/          router, middleware, kin-openapi request validator, handlers:
    server_<domain>.go       auth, me, accounts (+ categories), transactions, budgets (+ dashboard, reports), ai, system
    auth_service.go  period.go  tx_cursor.go  tx_export.go  convert.go
  internal/httpapi/gen/      oapi-codegen output from ../../api/openapi.yaml (gen.go: never edit)
  internal/auth/             Argon2id hasher, password policy, JWT issue/verify, refresh tokens, in-memory rate limits
  internal/db/               sqlc output (never edit) + tx.go (InTx)
  internal/migrate/          goose as a library (embedded migrations; also used by /readyz)
  internal/money/            minor units, ISO-4217 exponents, decimal-string parse/format
  internal/apierr/           error constructors → RFC 9457 problem+json (one writer)
  internal/provider/         INTERFACES + fake/log drivers: ai/ email/ storage/ push/
  internal/testutil/         migrated test DB pool, httptest server, CreateUser, Token, AssertBalance, fake clock
  internal/apitest/<slice>/  integration tests per slice: auth, me, accounts, transactions, budgets, dashboard, export, ai
  db/migrations/             goose SQL (00001_init, 00002_seed_categories), embedded
  db/queries/                sqlc .sql files per domain
  scripts/dev.ps1  Makefile  sqlc.yaml  Dockerfile  .env.example
```

There is **no `internal/service` and no `internal/jobs` yet**. Handlers own their DB transaction (`db.InTx`) and business rules.
Extract a service layer only when a second caller (the worker) needs the same logic. Only `internal/db` touches SQL.

## Provider interfaces (no vendor is decided)

`internal/provider/{ai,email,storage,push}` define small interfaces (`ai.ReceiptExtractor`, `email.Sender`, `storage.BlobStore`, …)
with fake/log drivers selected by `AI_DRIVER` / `EMAIL_DRIVER` / `STORAGE_DRIVER`. A real adapter is written only after the OPEN item in
`docs/DECISIONS.md` is decided. Never import a vendor SDK outside its adapter package.

## Error handling

- Wrap errors with context: `fmt.Errorf("create transaction: %w", err)`. Never discard them. No `panic` outside `main`.
- Domain errors are built with `internal/apierr` constructors (`NotFound()`, `Conflict(code, detail)`, `Validation(fields...)`,
  `BadRequest(msg)`, …), each with a stable `code`. **One** writer emits problem+json; unknown errors → 500 `internal`, no internals.
- `pgx.ErrNoRows` → `not_found`. Unique violation `23505` → a specific 409 code.
- Log once at the boundary with slog (`request_id`, `user_id`, `route`, `err`). Never log passwords, tokens, token hashes, emails,
  amounts, descriptions, or AI payloads. Every outbound call takes a `ctx` with a timeout.

## Data access (sqlc + goose)

1. New migration in `db/migrations/` (goose SQL). Expand → contract (`.claude/rules/db-migrations.md`). Update `docs/SCHEMA.md`.
2. Queries in `db/queries/<domain>.sql`. **Every user-data query has `user_id = @user_id`.**
3. Regenerate with sqlc (Docker, see Commands).
4. Multi-statement writes go through `db.InTx(ctx, pool, func(q *db.Queries) error {...})`. Balance updates are relative
   (`balance_minor = balance_minor + @delta`). Lock several accounts in ascending id order (`docs/SCHEMA.md` §5).
5. No ORM, no string-built SQL.

## Money

- `float64` is banned for amounts. Exact decimal-string parsing; reject excess precision rather than round.
- Format only at the edges (CSV, emails). JSON carries `amount_minor` + `currency`. SQL sums stay `bigint` (`SUM(...)::bigint`).
- Amounts from an LLM are requested as strings and go through `money.Parse`.

## Auth security rules (design: `docs/AUTH.md`, choices: `docs/DECISIONS.md`)

- Argon2id PHC strings, rehash on login when parameters change, constant-time compare, dummy hash for unknown emails.
- Access tokens: HS256 JWT with `kid`, algorithm allow-list, `exp`/`iss`/`aud` checked; every request also checks the session row.
  The user id comes **only** from the token.
- Refresh tokens: 256-bit random, stored as SHA-256, single use, rotated; reuse revokes the session.
- Rate limits in memory (single instance). Generic errors. No auth secrets in code or logs.

## Testing

- `go test ./...`. Unit tests next to the code; integration tests in `internal/apitest/<slice>/` against **real Postgres**
  (migrated once per test binary). No DB mocks. Each test creates its own users. Without `TEST_DATABASE_URL` they skip.
- Every endpoint: happy path, validation error, **another user's resource → 404**, no token → 401. Writes assert the balance invariant.
- Auth checklist: `docs/AUTH.md` §9. Providers use the fake drivers.

## Dependencies

Approved (D-16): chi · pgx/v5 · goose v3 · oapi-codegen v2 (Go `tool`) + `oapi-codegen/runtime` + `getkin/kin-openapi` ·
`golang-jwt/jwt/v5` · `golang.org/x/crypto` · `google/uuid` · `golang.org/x/time` · `golang.org/x/text`. sqlc runs from Docker.
**Anything else: ask first** (job queue D-OPEN-9, golangci-lint, any vendor SDK).

## Commands (from `apps/api`)

| Task | Command |
| --- | --- |
| Run API | `go run ./cmd/api` (worker stub: `go run ./cmd/worker`) |
| Migrate | `go run ./cmd/api migrate up\|down\|status` |
| Generate server stubs | `go generate ./internal/httpapi/gen/` |
| Generate sqlc | `docker run --rm -v "${PWD}:/src" -w /src sqlc/sqlc:1.30.0 generate` |
| Test | `go test ./...` (`TEST_DATABASE_URL` in `apps/api/.env`; Postgres via `pnpm db:up` from the root) |
| Vet | `go vet ./...` |
| Windows task runner | `./scripts/dev.ps1 <task>`: `run worker test vet build migrate-up migrate-down migrate-status generate sqlc docker-build` |
| Makefile (same tasks) | `mingw32-make <task>` on Windows, `make <task>` elsewhere |
