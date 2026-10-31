# apps/api: Go API (+ worker stub)

Arth's backend: a Go module (`arth/api`) with its own authentication (docs/AUTH.md) on PostgreSQL 16.

- `cmd/api`: HTTP server (chi + oapi-codegen strict server) and `migrate up|down|status` (goose, embedded migrations).
- `cmd/worker`: stub. The job queue library is still OPEN (docs/DECISIONS.md D-OPEN-9).
- Contract: `../../api/openapi.yaml` (33 operations). Schema: `db/migrations/`. Queries: `db/queries/` (sqlc).
- AI, email, storage and push sit behind interfaces in `internal/provider/` with fake/log/none drivers only.

Run it on Windows: `../../docs/RUN_API_LOCALLY.md`. Conventions for contributors and agents: `CLAUDE.md`.
Status, test results and known problems: `../../docs/PROGRESS.md`.
