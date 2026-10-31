# Deployment

Variable **names only** below. Real values live in provider dashboards (Railway/Render, Vercel, EAS, GitHub Environments) and
in local `.env` files that are never committed. Every app keeps a `.env.example` with these names.

## 1. Environments

| | Local | Staging | Production |
| --- | --- | --- | --- |
| API + worker | `go run` on your machine | Railway/Render service `arth-api-staging`, `arth-worker-staging` | `arth-api`, `arth-worker` |
| Database | docker-compose Postgres 17 | Neon branch `staging` | Neon branch `main` (protected, PITR) |
| Clerk | Dev instance | Dev instance | Prod instance (custom domain) |
| Email | `EMAIL_DRIVER=log` (no sends) | Resend with a verified staging subdomain, sends only to an allowlist | Resend prod domain |
| Gemini | Real key (low quota) or `AI_DRIVER=fake` | Staging key | Prod key (separate project, budget alerts on) |
| Web | `next dev` | Vercel Preview deployments | Vercel Production on the custom domain |
| Mobile | Expo dev client (`development` profile) → local or staging API | EAS `preview` profile (internal/closed track) → staging API | EAS `production` profile → Play Store → prod API |

## 2. Environment variables (names only)

### apps/api: `cmd/api`

| Name | Purpose |
| --- | --- |
| `APP_ENV` | `local` \| `staging` \| `production` |
| `PORT` | HTTP port |
| `LOG_LEVEL` | `debug` \| `info` \| `warn` \| `error` |
| `DATABASE_URL` | Neon **pooled** connection string (role `arth_app`) |
| `DATABASE_URL_DIRECT` | Neon **direct** string (migrations, River listener) |
| `MIGRATIONS_DATABASE_URL` | Direct string with role `arth_migrator` (pre-deploy migrate only) |
| `CLERK_SECRET_KEY` | Clerk Backend API (provisioning, account deletion) |
| `CLERK_ISSUER` | Expected `iss` (Clerk Frontend API URL); JWKS is derived from it |
| `CLERK_AUTHORIZED_PARTIES` | Comma-separated allowed `azp` origins |
| `CORS_ALLOWED_ORIGINS` | Comma-separated web origins |
| `GEMINI_API_KEY` | Server-side only |
| `GEMINI_MODEL` | e.g. the flash-lite model currently used (`gemini-3.1-flash-lite`) |
| `AI_DRIVER` | `gemini` \| `fake` |
| `AI_RATE_LIMIT_PER_HOUR` | Per-user limit for `/v1/ai/*` |
| `MAX_UPLOAD_BYTES` | Upload cap |
| `UNSUBSCRIBE_SIGNING_KEY` | HMAC key for unsubscribe tokens (shared with the worker) |
| `APP_PUBLIC_URL` | Web base URL (links) |

### apps/api: `cmd/worker`

| Name | Purpose |
| --- | --- |
| `APP_ENV`, `LOG_LEVEL` | as above |
| `DATABASE_URL_DIRECT` | River requires LISTEN/NOTIFY → direct connection |
| `RESEND_API_KEY` | Email provider |
| `EMAIL_FROM` | e.g. `Arth <alerts@mail.<domain>>` |
| `EMAIL_REPLY_TO` | Support address |
| `EMAIL_DRIVER` | `resend` \| `log` |
| `EMAIL_ALLOWLIST` | Staging only: comma-separated recipients allowed |
| `EMAIL_LOGO_URL` | Public logo URL for email header (optional) |
| `GEMINI_API_KEY`, `GEMINI_MODEL`, `AI_DRIVER` | Weekly tips |
| `UNSUBSCRIBE_SIGNING_KEY` | Build unsubscribe links |
| `APP_PUBLIC_URL` | Links in emails |

### apps/web

| Name | Purpose |
| --- | --- |
| `NEXT_PUBLIC_CLERK_PUBLISHABLE_KEY` | Clerk (public) |
| `CLERK_SECRET_KEY` | Clerk middleware/server (server only) |
| `NEXT_PUBLIC_CLERK_SIGN_IN_URL`, `NEXT_PUBLIC_CLERK_SIGN_UP_URL` | Routes |
| `NEXT_PUBLIC_API_BASE_URL` | Go API base URL |
| `NEXT_PUBLIC_APP_ENV` | Display/env banner |

### apps/mobile (EAS environment variables per profile)

| Name | Purpose |
| --- | --- |
| `EXPO_PUBLIC_CLERK_PUBLISHABLE_KEY` | Clerk (public) |
| `EXPO_PUBLIC_API_BASE_URL` | Go API base URL |
| `EXPO_PUBLIC_APP_ENV` | `development` \| `preview` \| `production` |
| `EXPO_PUBLIC_BACKEND` | Transitional (Months 1–2): `supabase` \| `api`. Delete at cut-over |
| `EXPO_PUBLIC_SUPABASE_URL`, `EXPO_PUBLIC_SUPABASE_KEY` | Transitional. Delete at cut-over |
| ~~`EXPO_PUBLIC_GEMINI_API_KEY`~~ | **Remove.** Must never exist in a release build |

Anything `EXPO_PUBLIC_*` / `NEXT_PUBLIC_*` is public by definition. Never put secrets there.

### CI (GitHub Actions secrets / environments)

`RAILWAY_TOKEN` **or** `RENDER_API_KEY` (+ service ids), `EXPO_TOKEN`, `MIGRATIONS_DATABASE_URL_STAGING`, `MIGRATIONS_DATABASE_URL_PROD`
(only if migrations run from CI rather than as a provider pre-deploy command), and the Play service-account JSON for `eas submit` (stored in EAS, not GitHub).
Vercel deploys through its Git integration, so no token is needed in GitHub.

## 3. CI/CD (path-filtered GitHub Actions)

| Workflow | Triggers (paths) | Jobs |
| --- | --- | --- |
| `api.yml` | `apps/api/**`, `api/openapi.yaml` | `gofmt`/`go vet`, `go test ./...` against a Postgres service container (migrations applied), `sqlc diff` (generated code current), regenerate oapi stubs + `git diff --exit-code`, `govulncheck`, build Docker image. On `main`: deploy to staging. On tag `api-v*` (GitHub Environment `production` with manual approval): deploy prod |
| `web.yml` | `apps/web/**`, `packages/api-client/**` | install (pnpm, cached), typecheck, lint, `next build`. Deploys are handled by Vercel (preview per PR, prod on `main`) |
| `mobile.yml` | `apps/mobile/**`, `packages/api-client/**` | typecheck, lint, `expo-doctor`. On `main`: `eas update --channel preview`. On tag `mobile-v*`: `eas build -p android --profile production` + `eas submit` |
| `contract.yml` | `api/openapi.yaml`, `packages/api-client/**` | Lint the spec (tool TBD), regenerate the TS client, `git diff --exit-code` |

Use `dorny/paths-filter`-style filtering, or plain `on.push.paths` per workflow (adding a third-party action counts as a dependency, so ask).

## 4. Release steps

### API + worker

1. PR → `api.yml` green → merge to `main` → image built once, tagged with the commit SHA.
2. Staging: the provider **pre-deploy command** runs `api migrate up` (role `arth_migrator`, direct URL), then rolls out API + worker.
3. Smoke test: `/readyz`, sign in on the preview mobile/web build, create and delete a transaction.
4. Prod: tag `api-vX.Y.Z` → approval → same image → pre-deploy migrate → rollout.
5. Migrations follow **expand → deploy → contract** (never drop or rename a column that a deployed API or a mobile build in the field still uses).
6. Rollback: redeploy the previous image. Migrations are forward-only, which is why expand/contract matters.

### Web

1. PR → Vercel preview (points at the staging API) → review.
2. Merge to `main` → Vercel production. Rollback = "Promote" a previous deployment in Vercel.

### Mobile (Android first)

1. JS-only change: merge → `eas update --channel preview` → test → `eas update --channel production` (only for builds with a matching `runtimeVersion`).
2. Native change, or a new version: bump `version` (+ `android.versionCode` via EAS remote versioning) → tag `mobile-vX.Y.Z` →
   `eas build -p android --profile production` → `eas submit -p android` to the **internal/closed** track → test → promote to production with a staged rollout.
3. Each API release must stay compatible with every mobile version still in use (see `/v1` rules in `docs/API.md`).
4. iOS (post-MVP): same profiles plus `-p ios`, TestFlight, App Store review.
