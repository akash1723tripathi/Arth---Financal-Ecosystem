# apps/api: Go API + worker (DRAFT)

> Draft conventions; nothing is implemented yet. Ask before adding any module not listed under "Dependencies".

## Layout

```text
apps/api/
  cmd/api/main.go           HTTP server; subcommands: serve (default), migrate up|down|status
  cmd/worker/main.go        River workers + periodic jobs; subcommand: run <job> (manual trigger)
  internal/config/          env parsing + validation (fail fast at boot)
  internal/httpapi/         chi router, middleware, strict-handler implementations
  internal/httpapi/gen/     oapi-codegen output from ../../api/openapi.yaml (never edit)
  internal/auth/            Clerk JWT verification (JWKS), sub → users.id, context helpers
  internal/service/         business logic per domain: me, accounts, transactions, budgets, reports, export, ai
  internal/db/              sqlc output (never edit) + tx helper
  internal/money/           Money type, ISO-4217 exponents, decimal-string parse/format
  internal/ai/              Gemini client, prompts/, response schemas
  internal/jobs/            River job args + workers + periodic schedule
  internal/email/           Resend client, templates/ (html/template + text)
  internal/apierr/          domain errors → RFC 9457 problem+json
  db/migrations/            goose SQL migrations (embedded via embed.FS)
  db/queries/               sqlc .sql files (worker_*.sql = cross-user queries, worker only)
  sqlc.yaml  oapi-codegen.yaml  Dockerfile  .env.example
```

Handlers stay thin: decode (generated) → call a service → map the result or error. Services own transactions and rules.
Only `internal/db` touches SQL.

## Error handling

- Wrap with context: `fmt.Errorf("create transaction: %w", err)`. Never discard errors. No `panic` outside `main`.
- Domain sentinels in `internal/apierr` (`ErrNotFound`, `ErrConflict`, `ErrValidation`, …, with a stable `code`).
  Services return them. **One** mapper turns them into problem+json. Everything else → 500 `internal`.
- `pgx.ErrNoRows` → `ErrNotFound` at the service boundary. Unique violations (`23505`) → `ErrConflict` with a specific code.
- Log once, at the boundary (middleware/job runner), with slog: `request_id`, `user_id`, `route`, `err`. Do not log amounts,
  descriptions, emails, tokens or AI payloads at info level.
- Every outbound call (Gemini, Resend, Clerk) takes a `ctx` with a timeout.

## Data access (sqlc + goose)

1. Write or alter a migration: `goose -dir db/migrations create <name> sql` (forward-only in practice. Expand → contract).
2. Write the queries in `db/queries/<domain>.sql`. **Every user-data query has `user_id = @user_id`.**
3. `sqlc generate`, then `sqlc vet` (CI checks that generated code is current).
4. Multi-statement writes go through `db.InTx(ctx, pool, func(q *db.Queries) error {...})`.
   Balance changes use relative updates (see `docs/SCHEMA.md` §5). Lock multiple accounts in ascending id order.
5. Enqueue jobs inside the same tx with River `InsertTx`.
- No ORM, no string-built SQL, no `SELECT *` in queries that are part of the API contract.
- The API uses the pooled `DATABASE_URL`. Worker and migrations use `DATABASE_URL_DIRECT`.

## Money

- The type is `money.Money{Minor int64; Currency string}`. **`float64` is banned for amounts** (review rule; consider a lint check).
- Parse with `money.Parse("123.45", "INR") → 12345` (exact decimal-string parsing; reject excess precision rather than rounding).
- Format only at the edge (CSV, emails) with `money.Format`. JSON always carries `amount_minor` + `currency`.
- Sums stay in SQL as `bigint` (`SUM(amount_minor)` → `numeric` in Postgres; cast back with `::bigint` and guard against overflow).
- Gemini returns amounts as **strings**. Parse with `money.Parse`; on failure return null.

## Testing

- `go test ./...`. Table-driven unit tests for `money`, cursors, threshold logic, and prompt-output parsing.
- Integration tests hit a real Postgres (docker-compose locally, a service container in CI) with migrations applied. No DB mocks.
  Each test creates its own users. Clean up with `TRUNCATE … CASCADE` between packages or use unique ids.
- Required for every endpoint: happy path, validation error, **another user gets 404**, and for writes, **the balance invariant holds**.
- HTTP tests go through the real router with a fake JWT verifier (`auth.Verifier` interface).
- AI: `AI_DRIVER=fake` returns canned extractions. One opt-in test (`-tags=gemini`) hits the real API.
- Jobs: River's `rivertest` helpers. Assert that a notification dedupes (running twice sends once).

## Dependencies (decided)

chi · pgx v5 · sqlc · goose · slog (std) · oapi-codegen v2 · River (+ riverpgxv5) · Resend Go SDK · Gemini Go SDK (`google.golang.org/genai`).
**Proposed, need approval:** Clerk Go SDK v2 (JWKS verify + Backend API), a cron-expression parser for River periodic jobs, `golang.org/x/time/rate`
(rate limiting), `google/uuid` (if generating ids in Go), `golangci-lint` (tool).

## Commands (planned)

| Task | Command |
| --- | --- |
| Run API | `go run ./cmd/api` |
| Run worker | `go run ./cmd/worker` · one job: `go run ./cmd/worker run budget_alerts_scan` |
| Migrate | `go run ./cmd/api migrate up` |
| New migration | `goose -dir db/migrations create add_x sql` |
| Generate | `sqlc generate` · `go generate ./...` (oapi-codegen) |
| Test | `go test ./...` (needs `TEST_DATABASE_URL`) |
| Vet | `go vet ./...` · `govulncheck ./...` |
