# Authentication (design, implemented in the Go API session)

Arth owns authentication. Clerk is replaced by the Go API, with credentials stored in Postgres. This design is **implemented** in
`apps/api/internal/auth` (hashing, password policy, tokens, refresh rotation, rate limits) and `apps/api/internal/httpapi/server_auth.go`
(the `/v1/auth/*` handlers and `DELETE /v1/me`) in the Go API session. The questions marked **Q-n** are listed in §11; their
resolutions are in `docs/DECISIONS.md` ("Auth questions"). Where the text below still says "proposal" or offers options, DECISIONS.md
records what was chosen. Tables: `docs/SCHEMA.md` (`users`, `sessions`, `refresh_tokens`, later `auth_tokens`). Endpoints:
`docs/API.md` → Auth (contract: `api/openapi.yaml`).

## 1. Goals and non-goals

- **Goals:** email + password accounts; long sessions on mobile; immediate revocation (logout, logout-all, password change, account deletion);
  safe against credential stuffing, token theft replay, and enumeration where practical; no third-party auth service.
- **MVP non-goals:** social login (Google/Apple), MFA/TOTP, passkeys, magic links, SSO. MFA and passkeys are post-MVP candidates (Q-9).

## 2. Building blocks

| Piece | Proposal |
| --- | --- |
| Password hashing | **Argon2id** (`golang.org/x/crypto/argon2`, needs approval). Starting parameters: OWASP baseline `m = 19 MiB, t = 2, p = 1`, 16-byte random salt, 32-byte key. Stored as a PHC string (`$argon2id$v=19$m=19456,t=2,p=1$<salt>$<hash>`), so parameters can rise later. Rehash on successful login when the stored parameters are older. Tune after measuring on the production host (target ~50–100 ms) (Q-1). |
| Access token | Short-lived signed **JWT**, 15 min (Q-2). Claims: `sub` (user id), `sid` (session id), `iat`, `exp`, `iss`, `aud`, `jti`. Algorithm fixed server-side and verified with an allow-list (never trust `alg` from the token). Signing key from env with a `kid` header for rotation. HS256 (single service, simplest) vs EdDSA (Ed25519; lets other services verify) is Q-3. |
| Refresh token | Opaque, 32 random bytes (`crypto/rand`), base64url. **Stored only as SHA-256** in `refresh_tokens.token_hash`. Single use: every refresh returns a new pair and marks the old token `used_at`. Idle lifetime 30 days, session absolute lifetime 90 days (Q-4). |
| Session | Row in `sessions`, one per login (device/browser). All refresh tokens of a login chain to it. Revoking the session kills every token in the chain. |
| Revocation check for access tokens | Option A: stateless (a revoked session's access token lives up to 15 min). Option B: middleware checks `sessions.revoked_at` by `sid` on every request (one indexed lookup; can be cached for a few seconds). Q-5. |

## 3. Flows

### Register

`POST /v1/auth/register {email, password, name, client}`

1. Normalise the email (trim; compare on `lower(email)`). Validate the format, the password policy (§4) and the name length.
2. Hash the password with Argon2id.
3. In one DB transaction: insert the user, insert the default "Cash" account, insert a session.
4. Issue an access token plus a refresh token (insert `refresh_tokens`). Return `TokenPair`.
5. If the email exists: `409 email_taken` (enumeration trade-off, §10 / Q-6).
6. Email verification is **OPEN** (needs an email provider, `docs/DECISIONS.md`). Until then `email_verified_at` stays NULL and nothing is gated on it (Q-7).

### Login

`POST /v1/auth/login {email, password, client}`

1. Rate-limit check (§5).
2. Look up by `lower(email)`. If not found, still run an Argon2id verify against a fixed dummy hash, so response time doesn't reveal whether the account exists.
3. Constant-time compare (the argon2 output via `subtle.ConstantTimeCompare`). On failure: `401 invalid_credentials`, the same body for an unknown email and a wrong password.
4. On success: rehash if the parameters changed, create a session, issue tokens.

### Refresh (rotation with reuse detection)

`POST /v1/auth/refresh {refresh_token}`

1. Hash the presented token and look it up **with `FOR UPDATE`** inside a transaction.
2. Not found / expired / session revoked / session past absolute expiry → `401 refresh_invalid`.
3. **Already used (`used_at` set) → reuse detected.** Revoke the whole session (`revoke_reason = 'REFRESH_REUSE'`) and return `401 refresh_invalid`.
   Log a security event (no token values).
4. Otherwise: set `used_at = now()`, insert the successor token (`replaced_by` link), bump `sessions.last_used_at`, and return the new pair.
5. Concurrency: mobile can fire two refreshes at once (two requests hit 401 together). Proposal: the client serialises refreshes (a single in-flight
   promise). Optionally, the server allows a short grace window (e.g. 20 s) in which re-presenting the just-used token returns the **same**
   successor instead of triggering reuse (Q-8).

### Logout / logout-all

- `POST /v1/auth/logout {refresh_token}`: revoke that token's session (`LOGOUT`). The client deletes its stored tokens.
- `POST /v1/auth/logout-all`: revoke all of the user's sessions (`LOGOUT_ALL`).
- Q-5 is decided as a per-request session check (docs/DECISIONS.md), so a revoked session's access tokens stop working immediately (tested in `apps/api/internal/httpapi/wiring_test.go` and `apitest/auth/sessions_test.go`).

### Change password

`POST /v1/auth/password {current_password, new_password}`. Verify the current password, rehash, set `password_changed_at`, and revoke **all other**
sessions (`PASSWORD_CHANGED`). The current session continues.

### Password reset (OPEN: needs email)

`request {email}` → always `202` (no enumeration). If the user exists, create an `auth_tokens` row (`RESET_PASSWORD`, 30 min, SHA-256 stored)
and email a link with the raw token. `confirm {token, new_password}`: single use, set the new hash, revoke all sessions.
**Without an email provider there is no self-service reset.** That is a launch blocker (Q-7).

### Email verification (OPEN: needs email)

Same pattern (`VERIFY_EMAIL`, 24 h). Gate sensitive emails (budget alerts, weekly tips) on a verified address. Decide whether login requires verification (Q-7).

### Account deletion

`DELETE /v1/me {password}`: re-verify the password, then in one transaction delete the user (cascades to accounts, transactions, budgets,
notifications, sessions and tokens). Pending jobs for that user then find no row and exit cleanly. Delete stored objects asynchronously once storage exists.
Google Play also requires a web path to request deletion (web app, session 3).

## 4. Password policy (proposal, NIST SP 800-63B style)

- Length 8–128 characters. No composition rules (no forced symbols). Allow all printable Unicode and spaces. Normalise to NFC before hashing (Q-1).
- Reject the top-N common passwords via a small embedded list (no external service). A k-anonymity breach check would be a third-party call (not proposed).
- The mobile zod schema (`apps/mobile/lib/schemas/auth.ts:7`, min 8) matches the minimum. Add the max length.

## 5. Rate limiting and abuse

| Endpoint | Limit (starting point) | Key |
| --- | --- | --- |
| login | 5 failures / 15 min per (email), 20 / 15 min per IP; then `429` with `Retry-After` | email + IP |
| register | 5 / hour per IP (**per IP only**, no per-email limit). Checked after the email/password checks, so a `422` does not use a slot | IP |
| refresh | 60 / hour per session (a refused refresh does not consume the token) | session |
| change password, `DELETE /v1/me` | Share the **login** buckets: every attempt uses the per-IP login bucket, a wrong password also counts as a failed login for the account's email. `429` once either is exhausted | email + IP |
| password-reset request (not built, D-OPEN-2) | 3 / hour per email, 10 / hour per IP | email + IP |

- Storage: in-memory token buckets (`golang.org/x/time/rate`, approved, D-16) work for one API instance. With several instances, use a Postgres table
  or sticky limits (Q-10).
- No account lockout (it lets attackers lock users out). Use throttling plus generic errors. Trade-off (DECISIONS D-24): because change-password
  and delete-me share the login buckets, someone holding a stolen access token can throttle login for that email until the bucket refills (≤ 15 min).
- Behind a proxy, trust `X-Forwarded-For` only from the configured proxy (host OPEN).

## 6. Clients

- **Mobile:** store the refresh token (and optionally the access token) in **`expo-secure-store`** (already installed; Keychain/Keystore-backed).
  Keep the access token in memory. The API client attaches it, and on `401` performs one serialised refresh and retries, then signs out
  on failure. Sign-out deletes the secure-store entries.
- **Web (OPEN, Q-11):**
  - (a) httpOnly `Secure` `SameSite=Lax` cookies set by the API. Simple and XSS-resistant, but needs CSRF protection and the API and web on
    the same registrable domain.
  - (b) Access token in memory, refresh token in an httpOnly cookie scoped to `/v1/auth/refresh`.
  - (c) Next.js BFF: route handlers hold the tokens server-side and proxy to the API.
  - Not proposed: tokens in `localStorage`.

## 7. Server-side implementation notes

- `internal/auth`: `Hasher` (Argon2id), `TokenIssuer`/`TokenVerifier` (interface, so tests can use a fixed key), `SessionStore` (sqlc).
- The middleware extracts the bearer token, verifies it and puts `user_id` + `session_id` in the context. Handlers never read a user id from the body.
- Never log passwords, tokens, token hashes, or full emails. Log `user_id`, `session_id` and the outcome.
- Signing keys: `AUTH_ACCESS_TOKEN_SIGNING_KEY` (plus an optional previous key for rotation). Generated per environment and never committed.
- Clock skew tolerance: 30 s on `exp`/`iat`.
- A JWT library vs a small stdlib-only implementation is Q-12.

## 8. Threat model (summary)

| Threat | Mitigation |
| --- | --- |
| Credential stuffing / brute force | Rate limits per email and IP, slow hash, generic errors, common-password list |
| User enumeration | Generic login error + dummy hash. Reset request always 202. Register leaks existence (Q-6) |
| DB leak | Argon2id hashes. Refresh/reset tokens stored only as SHA-256. No plaintext secrets in the DB |
| Stolen refresh token | Rotation + reuse detection revokes the session. Absolute session lifetime. Logout-all |
| Stolen access token | 15 min lifetime. Optional session check (Q-5). HTTPS only |
| JWT forgery / `alg` confusion | Fixed algorithm allow-list, key from env, `iss`/`aud`/`exp` checks |
| XSS on web | Depends on Q-11. Prefer httpOnly cookies; CSP on the web app |
| CSRF on web (if cookies) | SameSite + CSRF token or double-submit for state-changing requests |
| Device theft (mobile) | Secure-store tokens. Optional biometric lock (existing UI toggle, not implemented). Logout-all from another device |
| Cross-user data access | User id only from the token. Every query scoped by `user_id`. Composite FKs. 404 for others' rows |
| Timing attacks | Constant-time compares. Dummy hash for unknown users |
| Log leakage | Redaction rules (§7) |

## 9. Test checklist (Go integration tests against real Postgres)

Implemented in `apps/api/internal/apitest/auth/` unless another path is given.

- [x] Register → 201 with a token pair. Duplicate email in any case (`A@x.com` vs `a@x.com`) → 409. (`register_login_test.go`)
- [x] Weak/short/too-long password → 422 `weak_password`. (`register_login_test.go`)
- [x] Login with the correct password → 200. Wrong password and unknown email → identical 401 body. Timing roughly equal (smoke test). (`register_login_test.go`)
- [x] Stored hash is a PHC Argon2id string. The plaintext never appears in the DB or logs. (`register_login_test.go`; logs: `TestNoSecretsInLogs` in `ratelimit_test.go`)
- [x] Access token with a wrong signature / `alg: none` / wrong `aud` / expired → 401. (`tokens_test.go`)
- [x] Refresh rotates: the old token is marked used, the new one works. (`refresh_test.go`)
- [x] Re-using a used refresh token → 401 and the whole session revoked (the successor stops working too). (`refresh_test.go`)
- [x] Concurrent refresh behaviour matches the Q-8 decision. (`refresh_test.go`)
- [x] Logout revokes only that session. Logout-all revokes every session. (`sessions_test.go`)
- [x] Password change revokes other sessions but not the current one. (`sessions_test.go`)
- [x] Rate limits trigger 429 with `Retry-After` and reset after the window. (`ratelimit_test.go`; refresh: `refresh_test.go`; change password: `sessions_test.go`)
- [x] `DELETE /v1/me` with a wrong password → 401. With the right one, all user rows are gone and the tokens are dead. (`sessions_test.go`)
- [x] Protected endpoint without a token → 401. With user A's token on user B's resource → 404. (`tokens_test.go`; the 404 cases live in each
  domain slice: `apitest/accounts/accounts_test.go`, `apitest/transactions/transactions_test.go`, `apitest/budgets/budgets_test.go`, plus dashboard and export; e.g. `TestAuthorization`, `TestBudgetsAuthorization`)
- [x] Session past its absolute lifetime → refresh fails. (`refresh_test.go`)

## 10. Known trade-offs

- **Register enumeration:** without email verification, "email already registered" cannot be hidden from the person registering. Once email
  exists, register can always answer "check your inbox".
- **No reset without email:** a forgotten password means a lost account until an email provider is chosen. This blocks the public launch.

## 11. Questions and resolutions

The reasoning and who decided each one are in `docs/DECISIONS.md` → "Auth questions". Rows marked "review needed" there took the
default from this document.

| # | Question | Resolution |
| --- | --- | --- |
| Q-1 | Final Argon2id parameters (measure on the chosen host). Unicode NFC normalisation: yes/no | Resolved: DECISIONS.md Q-1 and Q-1 (NFC) |
| Q-2 | Access token lifetime (proposal 15 min) | Resolved: DECISIONS.md Q-2 |
| Q-3 | Signing: HS256 shared secret vs EdDSA key pair | Resolved: DECISIONS.md Q-3 |
| Q-4 | Refresh idle (30 d) and absolute session (90 d) lifetimes. Sliding vs fixed | Resolved: DECISIONS.md Q-4 |
| Q-5 | Stateless access tokens vs per-request session check | Resolved: DECISIONS.md Q-5 |
| Q-6 | Accept register-time enumeration in MVP, or require verification first | Resolved: DECISIONS.md Q-6 |
| Q-7 | Email provider (DECISIONS D-OPEN-2): needed for reset + verification. Is login blocked until verified? | Partly resolved: DECISIONS.md Q-7 (provider still OPEN, D-OPEN-2) |
| Q-8 | Refresh concurrency: client-only serialisation, or a server grace window too | Resolved: DECISIONS.md Q-8 |
| Q-9 | MFA / passkeys / Google sign-in after MVP? (Google sign-in would reintroduce a third party) | Deferred: DECISIONS.md Q-9 |
| Q-10 | Rate-limit storage: in-memory (single instance) vs Postgres | Resolved: DECISIONS.md Q-10 |
| Q-11 | Web token transport (a/b/c in §6) (DECISIONS D-OPEN-6) | Still OPEN: DECISIONS.md Q-11 / D-OPEN-6 (web session) |
| Q-12 | JWT library (needs approval) vs a minimal stdlib implementation | Resolved: DECISIONS.md Q-12 |
