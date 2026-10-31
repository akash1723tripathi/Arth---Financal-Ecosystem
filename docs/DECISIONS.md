# Decisions

Decided items are fixed unless the owner changes them. OPEN items need the owner's choice. The options and trade-offs below are input
for that choice, not a recommendation. Each provider sits behind a Go interface (`apps/api/internal/provider/*`) with a fake driver,
so the code can be built before a vendor is chosen.

## Decided

| ID | Decision | Reason |
| --- | --- | --- |
| D-1 | Product name **Arth**. Placeholder Android/iOS id `fyi.akashbuilds.arth`, scheme `arth` | Owner's choice. Confirm before any Play upload |
| D-2 | **No third-party SaaS in the product** (no Clerk, no Supabase) | Ownership, cost and portability. Fewer accounts and keys to manage |
| D-3 | **Own authentication in Go** (design: `docs/AUTH.md`) | Follows from D-2 |
| D-4 | Go connects to **PostgreSQL directly**. Local Postgres 16 via docker-compose | Plain Postgres is portable to any host |
| D-5 | Go stack: chi, pgx, sqlc, goose, oapi-codegen, slog | Small, standard, explicit SQL, OpenAPI-first |
| D-6 | **OpenAPI-first**: `api/openapi.yaml` is the contract. Go stubs + TS client are generated | One source of truth for three clients |
| D-7 | Postgres-backed background jobs (River **or equivalent**, see D-OPEN-9) | No Redis. Jobs enqueued in the same DB transaction |
| D-8 | Money = `BIGINT` minor units + ISO-4217 code | No float rounding errors |
| D-9 | Every user-data query scoped by the authenticated `user_id` | Replaces Supabase RLS |
| D-10 | Clients: Expo (existing app, Android first) + Next.js web | Reuse the working mobile UI. Web for desktop users |
| D-11 | Session order: mobile restructure → Go API (incl. auth) → Next.js web → repoint mobile + drop Clerk/Supabase → emulator testing → hardening → Play closed test → launch | Build the backend before removing the old one |
| D-12 | Monorepo, pnpm 10 workspace (`nodeLinker: hoisted`) | Hoisted is required: isolated installs broke Metro/NativeWind (verified) |
| D-13 | Git is local only (no GitHub remote). Conventional Commits, a branch per piece of work | Owner's choice. CI waits until a remote exists |
| D-14 | Build tools allowed: pnpm, Docker, GitHub Actions (when a remote exists), EAS Build | Not product dependencies |
| D-15 | 6-month MVP. MVP = accounts, transactions, budget, dashboard, CSV export, receipt scan, budget alerts, weekly tips. POST = voice, assistant, recurring, goals, multi-currency, shared accounts, bank import | Scope control for a solo developer |

## OPEN

### D-OPEN-1 · LLM provider (receipt scan [MVP], voice entry and assistant [POST], weekly tips [MVP])

| Option | Pros | Cons |
| --- | --- | --- |
| Google Gemini API (legacy choice) | Multimodal incl. audio in one call. Cheap flash/lite tiers. Existing prompts to port | A third party sees financial data. Data-use terms differ between free and paid tiers |
| OpenAI API | Strong vision and transcription models. Mature JSON-schema output | Third party. Voice needs a separate transcription call |
| Anthropic Claude API | Strong vision and structured output | No audio input: voice needs a separate speech-to-text step |
| Self-hosted open model (vision-language model via an inference server) | Data never leaves your infra. No per-call fees | GPU hosting cost and ops. Lower accuracy on messy receipts. Slower |
| On-device OCR (e.g. ML Kit text recognition) + rule-based parsing | Free, offline, private | Needs a native module (no Expo Go). Weaker category/merchant extraction. Not usable for the assistant |
| No AI in MVP (manual entry only) | Zero cost and risk | Drops an MVP feature |

Questions that decide it: is sending receipt images and aggregates to a vendor acceptable? Budget per active user? Is Indian-receipt accuracy (tested on samples) the key metric?

### D-OPEN-2 · Email delivery (password reset, verification, budget alerts, weekly tips)

| Option | Pros | Cons |
| --- | --- | --- |
| Resend (legacy choice) | Simple API, generous free tier | Third party. Newer company |
| Amazon SES | Very cheap at volume | AWS account setup, sandbox exit, more configuration |
| Postmark | Excellent transactional deliverability | Pricier. Strict about marketing-style mail (weekly tips may be borderline) |
| Brevo / Mailgun / SendGrid | Free tiers, mature | Third party. Varying deliverability |
| Self-hosted SMTP | No vendor | Deliverability, IP reputation, SPF/DKIM/DMARC upkeep: high risk of landing in spam |

This is a **launch blocker**: there is no password reset without it (`docs/AUTH.md` §10).

### D-OPEN-3 · Production PostgreSQL hosting

| Option | Pros | Cons |
| --- | --- | --- |
| Neon | Serverless, branching per environment, scale-to-zero | The pooled endpoint doesn't support LISTEN/NOTIFY (use the direct URL for the worker). Cold starts |
| Managed Postgres from a cloud (AWS RDS, GCP Cloud SQL, Azure) | Reliable, PITR backups, Mumbai regions | Cost floor, more setup |
| DigitalOcean / Railway / Render managed Postgres | Simple, co-located with the app host | Smaller feature sets. Check backups and PITR per plan |
| Self-managed on a VPS (Docker) | Cheapest | You own backups, upgrades, monitoring and security |

Requirements whatever the choice: Postgres 16+, daily backups + PITR, a region near users (India?), TLS.

### D-OPEN-4 · API + worker hosting

Options: Railway, Render, Fly.io, Google Cloud Run (the worker needs min-instances ≥ 1), AWS App Runner/ECS, or a single VPS with Docker Compose.
Trade-offs: cost at low traffic, an always-on worker process, deploy simplicity, region, and log/metrics access.

### D-OPEN-5 · Web hosting (Next.js)

Options: Vercel (best Next.js support; a third party), Netlify / Cloudflare (adapter-based; some features differ), self-hosted `next start` in
Docker next to the API. Trade-offs: preview deployments, edge features vs portability, cost.

### D-OPEN-6 · Web auth transport

Options (details in `docs/AUTH.md` §6): (a) httpOnly cookies from the API, (b) in-memory access token + httpOnly refresh cookie, (c) Next.js BFF holds the tokens.
Trade-offs: XSS exposure, CSRF handling, same-site domain requirement, extra hop.

### D-OPEN-7 · Object storage (receipt images, avatars)

| Option | Pros | Cons |
| --- | --- | --- |
| Store nothing in MVP | No cost or vendor. Receipt scan already works without storing | No receipt history. Avatars lost when Clerk goes |
| Cloudflare R2 | S3 API, no egress fees | Third party |
| AWS S3 / GCS | Mature, cheap storage | Egress fees. Account setup |
| Backblaze B2 | Cheap, S3-compatible | Third party. Fewer regions |
| MinIO / filesystem on own server | No vendor | Backups and durability are on you |

### D-OPEN-8 · Push notifications (post-MVP unless decided otherwise)

Options: Expo Push Service (free, wraps FCM/APNs; a third party, but tiny integration), FCM directly (Android; APNs later for iOS), or none (email only).
Trade-offs: an extra SDK/config in the app, token management, reliability.

### D-OPEN-9 · Job queue library

Options: River (Go, Postgres, typed jobs, periodic jobs, UI optional), a hand-rolled `jobs` table with `FOR UPDATE SKIP LOCKED`, or another
Postgres-backed library. Trade-offs: a dependency vs code to maintain. River needs LISTEN/NOTIFY or a poll-only mode.

### D-OPEN-10 · Smaller tooling choices (ask before adding)

- Go: `golang.org/x/crypto` (argon2id), a JWT library vs stdlib, `google/uuid`, `golang.org/x/time/rate`, golangci-lint.
- TS client generator for `packages/api-client`: openapi-typescript + openapi-fetch, orval, or hey-api.
- Error tracking: Sentry (SaaS), self-hosted GlitchTip, or logs only.
- Web UI kit and charts library.

### D-OPEN-11 · Legal

- Welth licence: no LICENSE upstream. Get written permission from the author or rewrite the copied parts before launch (`docs/MIGRATION.md` §7).
- Final package id, domain, privacy policy, and Play Console account type (personal vs organisation).
