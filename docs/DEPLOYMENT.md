# Deployment

Variable **names only** below. Real values live in local `.env` files (gitignored) and, later, in the chosen hosts' secret stores.
Each app has a `.env.example` with these names.

> Hosting is **OPEN**. The earlier plan (Clerk, Neon, Railway/Render, Vercel) is withdrawn by the no-SaaS decision.
> Choices: `docs/DECISIONS.md` D-OPEN-3 (Postgres), D-OPEN-4 (API/worker), D-OPEN-5 (web). Mobile builds use EAS (allowed build tool).

## 1. Environments

| | Local (now) | Staging (later) | Production (later) |
| --- | --- | --- | --- |
| Postgres | `docker-compose.yml` → `postgres:16-alpine` on `127.0.0.1:5432` | OPEN host, separate DB | OPEN host, PITR backups |
| API + worker | `go run ./cmd/api`, `go run ./cmd/worker` | OPEN host, Docker image | OPEN host, same image |
| Auth | Built-in (Go). Signing key from local `.env` | Own key | Own key (rotated) |
| AI | `AI_DRIVER=fake` | OPEN vendor or fake | OPEN vendor |
| Email | `EMAIL_DRIVER=log` (prints to the log) | OPEN vendor, allowlisted recipients | OPEN vendor |
| Storage | `STORAGE_DRIVER=none`/`fs` | OPEN | OPEN |
| Web | `next dev` | OPEN host | OPEN host, custom domain |
| Mobile | Expo dev server (Expo Go or dev client) → local API on the LAN / `10.0.2.2` from the emulator | EAS `preview` build → staging API | EAS `production` build → Play Store → prod API |

## 2. Environment variables (names only)

### Root `.env` (docker-compose)

`POSTGRES_USER`, `POSTGRES_PASSWORD`, `POSTGRES_DB`, `POSTGRES_PORT`

### apps/api (planned; see `apps/api/.env.example`)

| Name | Purpose |
| --- | --- |
| `APP_ENV` | `local` \| `staging` \| `production` |
| `PORT`, `LOG_LEVEL` | HTTP port, log level |
| `DATABASE_URL` | App role connection string |
| `MIGRATIONS_DATABASE_URL` | Migration role (DDL) connection string |
| `AUTH_ACCESS_TOKEN_SIGNING_KEY` | Access-token signing key (+ an optional previous key for rotation) |
| `AUTH_ACCESS_TOKEN_TTL`, `AUTH_REFRESH_TOKEN_TTL` | Token lifetimes (`docs/AUTH.md` Q-2, Q-4) |
| `CORS_ALLOWED_ORIGINS` | Web origins |
| `APP_PUBLIC_URL` | Links in emails |
| `AI_DRIVER`, `EMAIL_DRIVER`, `STORAGE_DRIVER` | Provider selection (`fake`/`log`/`none` locally) |
| *(vendor keys)* | Added only after each OPEN provider decision |

### apps/web (later)

`NEXT_PUBLIC_API_BASE_URL`, `NEXT_PUBLIC_APP_ENV` (plus cookie/auth settings once D-OPEN-6 is decided). No secrets in `NEXT_PUBLIC_*`.

### apps/mobile

| Name | Status |
| --- | --- |
| `EXPO_PUBLIC_CLERK_PUBLISHABLE_KEY` | **Legacy**, needed to run today, removed in Phase 4 |
| `EXPO_PUBLIC_SUPABASE_URL`, `EXPO_PUBLIC_SUPABASE_KEY` | **Legacy**, removed in Phase 4 |
| `EXPO_PUBLIC_GEMINI_API_KEY` | **Legacy and unsafe** (AI key in the bundle). Removed in Phase 4; revoke the key |
| `EXPO_PUBLIC_API_BASE_URL` | Target (Phase 4) |
| `EXPO_PUBLIC_APP_ENV` | Target |

## 3. CI/CD

There is no Git remote yet (D-13), so there is no CI. Until there is, run the checks locally before every commit:

- Mobile: `pnpm verify:mobile` (tsc + eslint + Android bundle) or `/verify-mobile`.
- API: `go vet ./...` and `go test ./...` against docker-compose Postgres. `sqlc vet` once sqlc exists.
- Contract: regenerate stubs/client and check `git diff --exit-code`.

When a remote exists, the same checks become path-filtered GitHub Actions workflows: `api` (apps/api/**, api/openapi.yaml), `web`
(apps/web/**, packages/api-client/**), `mobile` (apps/mobile/**, packages/api-client/**), `contract` (api/openapi.yaml).

## 4. Release steps (once hosts are chosen)

### API + worker

1. Build one Docker image (multi-stage, static Go binary) and tag it with the git SHA.
2. Run `api migrate up` (migration role) before rolling out. Migrations are expand → contract only.
3. Roll out API + worker, then smoke test `/readyz`, register/login, create and delete a transaction.
4. Rollback = previous image. Migrations are forward-only.

### Web

Build `next build`, deploy to the chosen host, smoke-test login + dashboard.

### Mobile (Android first)

1. JS-only change: `eas update --channel <preview|production>` (only for builds with a matching `runtimeVersion`).
2. Native change or new version: bump `version` (EAS remote `versionCode`), run `eas build -p android --profile production`, then `eas submit -p android`
   to the internal/closed track, test, and promote with a staged rollout.
3. The API stays backward-compatible with every mobile version in the field (`/v1` additive only).
