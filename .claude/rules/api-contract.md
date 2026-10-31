---
paths:
  - "api/**"
  - "packages/api-client/**"
  - "apps/api/internal/httpapi/**"
---

# OpenAPI-first contract

- `api/openapi.yaml` (OpenAPI 3.1) is the single source of truth. Change it **first**, then regenerate the Go server stubs and the TS client,
  then implement. Never hand-edit generated code (`apps/api/internal/httpapi/gen/`, generated files in `packages/api-client/`).
- Conventions live in `docs/API.md`: `/v1` prefix, `snake_case`, RFC 9457 problem+json errors with a stable `code`, keyset cursor pagination,
  `amount_minor` (integer) + `currency` for money, `YYYY-MM-DD` dates.
- Every endpoint except `/v1/auth/*` (register/login/refresh), `/healthz`, `/readyz` and `/v1/public/*` requires a bearer access token.
- Changes inside `/v1` must be additive (old mobile builds stay in the field). Update `docs/API.md` in the same commit.
