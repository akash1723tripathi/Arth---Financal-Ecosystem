# apps/api: Go backend (placeholder)

Not built yet; this is the next session. It will be a Go module with two entrypoints:
`cmd/api` (HTTP, chi + oapi-codegen) and `cmd/worker` (Postgres-backed background jobs).
It owns authentication (docs/AUTH.md) and talks to PostgreSQL directly (pgx + sqlc, goose migrations).

Conventions: `apps/api/CLAUDE.md`. Contract: `api/openapi.yaml`. Schema: `docs/SCHEMA.md`.
