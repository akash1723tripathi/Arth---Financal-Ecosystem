# apps/api: Go API + worker

> Nothing is implemented yet. The next session builds it. Ask before adding any Go module not listed under "Dependencies".

## Layout (planned)

```text
apps/api/
  go.mod
  cmd/api/main.go          HTTP server; subcommands: serve (default), migrate up|down|status
  cmd/worker/main.go       Postgres-backed job worker + periodic jobs; subcommand: run <job>
  internal/config/         env parsing + validation (fail fast at boot)
  internal/httpapi/        chi router, middleware, handler implementations
  internal/httpapi/gen/    oapi-codegen output from ../../api/openapi.yaml (never edit)
  internal/auth/           password hashing, token issue/verify, refresh rotation, auth middleware (docs/AUTH.md)
  internal/service/        business logic per domain: me, accounts, transactions, budgets, reports, export
  internal/db/             sqlc output (never edit) + transaction helper
  internal/money/          Money type, ISO-4217 exponents, decimal-string parse/format
  internal/provider/       INTERFACES for outside providers + local fakes:
    ai/  email/  storage/  push/   (real adapters are added only after the DECISIONS.md item is decided)
  internal/jobs/           job args + workers (budget alerts, weekly tips, reconcile)
  internal/apierr/         domain errors → RFC 9457 problem+json
  db/migrations/           goose SQL migrations (embedded via embed.FS)
  db/queries/              sqlc .sql files (worker_*.sql = cross-user queries, worker only)
  sqlc.yaml  Dockerfile  .env.example
```

Handlers stay thin: decode → call a service → map the result or error. Services own DB transactions and business rules.
Only `internal/db` touches SQL.

## Provider interfaces (no vendor is decided)

`internal/provider/{ai,email,storage,push}` define small interfaces (e.g. `ai.ReceiptExtractor`, `email.Sender`, `storage.BlobStore`).
Each ships with a **fake/log driver** used in dev and tests, selected by `AI_DRIVER` / `EMAIL_DRIVER` / `STORAGE_DRIVER`.
A real adapter is written only after the user decides the OPEN item in `docs/DECISIONS.md`. Never import a vendor SDK outside its adapter package.

## Error handling

- Wrap errors with context: `fmt.Errorf("create transaction: %w", err)`. Never discard them. No `panic` outside `main`.
- Domain sentinels live in `internal/apierr` (`ErrNotFound`, `ErrConflict`, `ErrValidation`, `ErrUnauthorized`, each with a stable `code`).
  **One** mapper writes problem+json. Unknown errors → 500 `internal` with no internals leaked.
- `pgx.ErrNoRows` → `ErrNotFound`. Unique violation `23505` → `ErrConflict` with a specific code.
- Log once at the boundary with slog (`request_id`, `user_id`, `route`, `err`). Never log passwords, tokens, token hashes, emails at info,
  amounts, descriptions, or AI payloads.
- Every outbound call takes a `ctx` with a timeout.

## Data access (sqlc + goose)

1. Migration: `goose -dir db/migrations create <name> sql`. Expand → contract (see `.claude/rules/db-migrations.md`). Update `docs/SCHEMA.md`.
2. Queries in `db/queries/<domain>.sql`. **Every user-data query has `user_id = @user_id`.**
3. `sqlc generate` then `sqlc vet`.
4. Multi-statement writes go through `db.InTx(ctx, pool, func(q *db.Queries) error {...})`. Balance updates are relative
   (`balance_minor = balance_minor + @delta`). Lock several accounts in ascending id order (`docs/SCHEMA.md` §5).
5. No ORM, no string-built SQL.

## Money

- `money.Money{Minor int64; Currency string}`. **`float64` is banned for amounts.**
- `money.Parse("123.45", "INR") → 12345` with exact decimal-string parsing. Reject excess precision rather than round.
- Format only at the edges (CSV, emails). JSON carries `amount_minor` + `currency`. SQL sums stay `bigint` (cast `SUM(...)::bigint`).
- Amounts from an LLM are requested as strings and go through `money.Parse`.

## Auth security rules (design: `docs/AUTH.md`)

- Passwords: Argon2id with per-hash salt, parameters encoded in the stored hash (PHC string). Rehash on login when parameters change.
  Compare in constant time. Run a dummy hash for unknown emails (no timing oracle).
- Access tokens: short-lived signed JWT. Verify the algorithm explicitly (allow-list), plus `exp`, `iss`, `aud`. The user id comes **only** from the token.
- Refresh tokens: random 256-bit, stored **hashed** (SHA-256), single use, rotated on every refresh. Reuse of a used token revokes the whole session family.
- Rate-limit register/login/refresh per IP and per email. Generic error messages ("invalid email or password").
- No auth secrets in code or logs. Signing keys come from env, with rotation via `kid`.

## Testing

- `go test ./...`. Table-driven unit tests for `money`, cursors, threshold logic, token rotation.
- Integration tests run against **real Postgres** (docker-compose locally) with migrations applied. No DB mocks. Each test creates its own users.
- Every endpoint needs: happy path, validation error, **another user's resource → 404**, unauthenticated → 401. Writes also check **the balance invariant holds**.
- Auth needs a checklist test suite (`docs/AUTH.md` §9).
- Providers use the fake drivers. Real-provider tests are opt-in via a build tag.

## Dependencies

Decided: chi · pgx v5 · sqlc (tool) · goose · oapi-codegen v2 · slog (std) · a Postgres-backed job queue (River proposed; confirm, see DECISIONS.md).
**Need approval before adding:** `golang.org/x/crypto` (argon2id), a JWT library or a stdlib-only implementation, `google/uuid`,
`golang.org/x/time/rate`, golangci-lint (tool). No vendor SDKs until their OPEN item is decided.

## Commands (planned)

| Task | Command (from `apps/api`) |
| --- | --- |
| Run API / worker | `go run ./cmd/api` · `go run ./cmd/worker` |
| Migrate | `go run ./cmd/api migrate up` |
| Generate | `sqlc generate` · `go generate ./...` |
| Test | `go test ./...` (needs `TEST_DATABASE_URL`, local Postgres via `pnpm db:up`) |
| Vet | `go vet ./...` |
