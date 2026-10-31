---
paths:
  - "apps/api/db/**"
  - "apps/api/sqlc.yaml"
---

# Migrations and queries

- Schema changes are goose SQL migrations in `apps/api/db/migrations/`, written expand → contract (never drop or rename a column that a
  deployed API or a mobile build in the field still reads). Keep `docs/SCHEMA.md` in sync in the same commit.
- Queries are sqlc files in `apps/api/db/queries/`. **Every query on user data filters by `user_id = @user_id`** (the authenticated id from
  middleware). Cross-user queries are allowed only in `worker_*.sql`, for background jobs.
- Money columns are `bigint` minor units plus `char(3)` currency. No `numeric`, `real` or `double precision` for amounts.
- After editing queries: `sqlc generate` and `sqlc vet`. Tests run against real Postgres (docker-compose), never mocks.
