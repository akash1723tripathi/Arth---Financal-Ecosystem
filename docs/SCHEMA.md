# Database schema

The source of truth is the goose migrations in `apps/api/db/migrations/` (§3). The SQL in this doc mirrors them for reading
(§3.1) or shows planned tables (`auth_tokens` at the end of §3.1, post-MVP tables in §3.2). Change the migration files first, then update this doc.

## 1. Current schema (Welth, `legacy/scripts/schema.sql`, hosted on Supabase)

| Table | Columns | Notes / problems |
| --- | --- | --- |
| `users` | `clerk_id text PK`, `email`, `name`, `image_url`, `currency` (NULL until onboarding), `created_at` | Clerk id is the PK and is used as every FK. No timezone. No email preferences. |
| `accounts` | `id uuid PK`, `user_id → users.clerk_id`, `name`, `type` (`CASH\|BANK\|CREDIT_CARD\|SAVINGS`, no CHECK), `balance numeric`, `is_default`, `created_at` | Float-ish `numeric` balance updated by the client. No per-account currency. Nothing enforces a single default. |
| `transactions` | `id`, `user_id`, `account_id → accounts`, `type` (`INCOME\|EXPENSE`), `amount numeric`, `category text`, `description`, `date timestamptz`, `status` (always `COMPLETED`), `input_method` (`MANUAL\|RECEIPT_SCAN\|VOICE`), `voice_transcript`, `is_flagged`, `flag_reason`, `created_at`, `updated_at` | No CHECKs. Category is free text. No indexes beyond the PK. Nothing ensures `account_id` belongs to `user_id`. `status`/`is_flagged`/`flag_reason` are unused. |
| `budgets` | `id`, `user_id UNIQUE`, `amount numeric`, `last_alert_sent`, `last_alert_threshold numeric`, timestamps | One overall budget per user. Alert state is mixed into the budget row. |

RLS: each table has policies of the form `user_id = auth.jwt()->>'sub'` (Clerk's JWT). Clerk, Supabase and RLS all go away.
Arth owns users and credentials, and authorization lives in the Go API (§4). No production data exists, so there is no data migration;
the optional dev-data port is in §7.

## 2. Design rules

- **Money:** `*_minor BIGINT` (e.g. ₹123.45 → `12345`) + `currency CHAR(3)` (ISO 4217). The exponent comes from an
  ISO-4217 table in Go (`INR/USD/EUR=2`, `JPY/KRW=0`, `KWD/BHD/OMR=3`). Amounts on transactions are **always positive**.
  The sign comes from `type` via the generated `delta_minor`. Supported currencies are an explicit allowlist (no `XAU` etc.).
- **Enums:** `text` + `CHECK (… IN (…))`, not Postgres `ENUM`. Adding a value is then a one-line migration.
- **IDs:** `uuid DEFAULT gen_random_uuid()`. (UUIDv7 generated in Go is an option later, for index locality.)
- **Ownership:** every user-owned row has `user_id uuid NOT NULL`. Child rows that reference another user-owned row use a
  **composite FK `(x_id, user_id) → parent(id, user_id)`**, so the DB rejects cross-user references even if a query is wrong.
- **Dates:** `occurred_on DATE` is the user's calendar day. `timestamptz` is used only for audit fields.
- **Timestamps:** `created_at/updated_at timestamptz NOT NULL DEFAULT now()`. `updated_at` is set explicitly in sqlc `UPDATE`s (no triggers).
- **Requirements:** PostgreSQL ≥ 15 (`UNIQUE NULLS NOT DISTINCT`, `ON DELETE SET NULL (col)`). Local dev runs **Postgres 16** (`docker-compose.yml`).
  Production must run the same major version (host is OPEN).
- **Secrets at rest:** passwords are stored only as Argon2id hashes. Refresh/verification/reset tokens are stored only as SHA-256 hashes.

## 3. Schema

**The migrations are now the source of truth.** If this section and a migration disagree, the migration wins. Fix this doc.

| File | Contents |
| --- | --- |
| `apps/api/db/migrations/00001_init.sql` | MVP baseline: `users`, `sessions`, `refresh_tokens`, `categories`, `accounts`, `transactions`, `budgets`, `notifications` with their indexes and constraints |
| `apps/api/db/migrations/00002_seed_categories.sql` | The 21 system categories (data, idempotent) |

Migrations are embedded in the binary and applied with `api migrate up` (goose as a library, D-18). `/readyz` reports
`migrations_pending` when the database is behind the binary.

### 3.1 MVP baseline migration (`00001_init.sql`)

```sql
-- users ---------------------------------------------------------------
CREATE TABLE users (
  id                   uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  email                text NOT NULL CHECK (length(email) BETWEEN 3 AND 254),  -- stored as entered, trimmed
  email_verified_at    timestamptz,                    -- NULL until verified (flow is OPEN: needs email provider)
  password_hash        text NOT NULL,                  -- Argon2id PHC string: $argon2id$v=19$m=…,t=…,p=…$salt$hash
  password_changed_at  timestamptz NOT NULL DEFAULT now(),
  name                 text CHECK (length(name) <= 100),
  avatar_key           text,                           -- object-storage key (OPEN; NULL = no avatar)
  currency             char(3),                        -- default currency; NULL until onboarding
  timezone             text NOT NULL DEFAULT 'UTC',    -- IANA, validated in Go
  onboarded_at         timestamptz,
  email_budget_alerts  boolean NOT NULL DEFAULT true,
  email_weekly_tips    boolean NOT NULL DEFAULT true,
  created_at           timestamptz NOT NULL DEFAULT now(),
  updated_at           timestamptz NOT NULL DEFAULT now()
);
-- case-insensitive uniqueness without the citext extension; look-ups use lower(email) too
CREATE UNIQUE INDEX users_email_lower_uq ON users (lower(email));

-- sessions: one per signed-in device/browser ---------------------------
CREATE TABLE sessions (
  id             uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  user_id        uuid NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  client         text NOT NULL CHECK (client IN ('MOBILE','WEB')),
  user_agent     text CHECK (length(user_agent) <= 512),
  ip             inet,
  created_at     timestamptz NOT NULL DEFAULT now(),
  last_used_at   timestamptz NOT NULL DEFAULT now(),
  expires_at     timestamptz NOT NULL,               -- absolute lifetime (created_at + 90 days, DECISIONS Q-4)
  revoked_at     timestamptz,
  revoke_reason  text CHECK (revoke_reason IN ('LOGOUT','LOGOUT_ALL','REFRESH_REUSE','PASSWORD_CHANGED','ACCOUNT_DELETED','ADMIN'))
);
CREATE INDEX sessions_user_active_idx ON sessions (user_id) WHERE revoked_at IS NULL;

-- refresh_tokens: rotating, single-use, stored hashed -----------------
CREATE TABLE refresh_tokens (
  id            uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  session_id    uuid NOT NULL REFERENCES sessions(id) ON DELETE CASCADE,
  token_hash    bytea NOT NULL UNIQUE,              -- SHA-256 of the opaque 256-bit token
  created_at    timestamptz NOT NULL DEFAULT now(),
  expires_at    timestamptz NOT NULL,               -- idle lifetime (30 days, renewed on rotation, DECISIONS Q-4)
  used_at       timestamptz,                        -- set on rotation; presenting it again = reuse → revoke session
  replaced_by   uuid REFERENCES refresh_tokens(id) ON DELETE SET NULL
);
CREATE INDEX refresh_tokens_session_idx ON refresh_tokens (session_id);
```

**Still planned, not created by any migration yet:** `auth_tokens` comes with the email-provider decision (D-OPEN-2):

```sql
-- one-time tokens for email verification and password reset ----------
CREATE TABLE auth_tokens (
  id           uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  user_id      uuid NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  purpose      text NOT NULL CHECK (purpose IN ('VERIFY_EMAIL','RESET_PASSWORD')),
  token_hash   bytea NOT NULL UNIQUE,               -- SHA-256; raw token only ever in the email link
  created_at   timestamptz NOT NULL DEFAULT now(),
  expires_at   timestamptz NOT NULL,                -- short: e.g. 30 min reset, 24 h verify
  used_at      timestamptz
);
CREATE INDEX auth_tokens_user_purpose_idx ON auth_tokens (user_id, purpose) WHERE used_at IS NULL;
```

Rate-limit state for login/register is in memory (single API instance, decided in `docs/DECISIONS.md` Q-10). It moves to a
Postgres table before more than one instance runs, so there is no table for it yet.

```sql

-- categories ------------------------------------------------------------
-- System categories: user_id NULL + stable key (the 21 keys, seeded in 00002).
-- Custom categories (post-MVP UI): user_id set, key NULL.
CREATE TABLE categories (
  id          uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  user_id     uuid REFERENCES users(id) ON DELETE CASCADE,
  key         text,
  name        text NOT NULL CHECK (length(name) BETWEEN 1 AND 40),
  kind        text NOT NULL CHECK (kind IN ('INCOME','EXPENSE')),
  icon        text,
  color       text CHECK (color ~ '^#[0-9A-Fa-f]{6}$'),
  sort_order  int  NOT NULL DEFAULT 0,
  archived_at timestamptz,
  created_at  timestamptz NOT NULL DEFAULT now(),
  CHECK ((user_id IS NULL) = (key IS NOT NULL))
);
CREATE UNIQUE INDEX categories_system_key_uq ON categories (key) WHERE user_id IS NULL;
CREATE UNIQUE INDEX categories_user_name_uq  ON categories (user_id, lower(name)) WHERE user_id IS NOT NULL;

-- accounts --------------------------------------------------------------
CREATE TABLE accounts (
  id                     uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  user_id                uuid NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  name                   text NOT NULL CHECK (length(name) BETWEEN 1 AND 60),
  type                   text NOT NULL CHECK (type IN ('CASH','BANK','CREDIT_CARD','SAVINGS')),
  currency               char(3) NOT NULL,
  opening_balance_minor  bigint NOT NULL DEFAULT 0,
  balance_minor          bigint NOT NULL DEFAULT 0,  -- cache: opening + Σ delta_minor
  is_default             boolean NOT NULL DEFAULT false,
  archived_at            timestamptz,
  created_at             timestamptz NOT NULL DEFAULT now(),
  updated_at             timestamptz NOT NULL DEFAULT now(),
  UNIQUE (id, user_id)                               -- target for composite FKs
);
CREATE UNIQUE INDEX accounts_one_default_per_user ON accounts (user_id) WHERE is_default;
CREATE INDEX accounts_user_idx ON accounts (user_id, created_at);

-- transactions ----------------------------------------------------------
CREATE TABLE transactions (
  id               uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  user_id          uuid NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  account_id       uuid NOT NULL,
  category_id      uuid REFERENCES categories(id) ON DELETE RESTRICT,
  type             text NOT NULL CHECK (type IN ('INCOME','EXPENSE','TRANSFER_IN','TRANSFER_OUT')),
  amount_minor     bigint NOT NULL CHECK (amount_minor > 0 AND amount_minor <= 999999999999999),
  currency         char(3) NOT NULL,
  delta_minor      bigint NOT NULL GENERATED ALWAYS AS (
                     CASE WHEN type IN ('INCOME','TRANSFER_IN') THEN amount_minor ELSE -amount_minor END
                   ) STORED,
  occurred_on      date NOT NULL,
  description      text CHECK (length(description) <= 200),
  input_method     text NOT NULL DEFAULT 'MANUAL'
                     CHECK (input_method IN ('MANUAL','RECEIPT_SCAN','VOICE','RECURRING','IMPORT')),
  voice_transcript text CHECK (length(voice_transcript) <= 2000),
  transfer_id      uuid,                              -- shared by both legs of a transfer
  created_at       timestamptz NOT NULL DEFAULT now(),
  updated_at       timestamptz NOT NULL DEFAULT now(),
  FOREIGN KEY (account_id, user_id) REFERENCES accounts (id, user_id) ON DELETE CASCADE,
  CHECK ((type IN ('TRANSFER_IN','TRANSFER_OUT')) = (transfer_id IS NOT NULL)),
  CHECK ((type IN ('TRANSFER_IN','TRANSFER_OUT')) OR category_id IS NOT NULL)
);
-- list / keyset pagination
CREATE INDEX tx_user_date_idx     ON transactions (user_id, occurred_on DESC, created_at DESC, id DESC);
-- account filter + reconcile + cascade
CREATE INDEX tx_account_date_idx  ON transactions (account_id, occurred_on DESC);
-- budget progress / dashboard breakdown
CREATE INDEX tx_user_expense_cat_idx ON transactions (user_id, occurred_on, category_id) WHERE type = 'EXPENSE';
-- one OUT + one IN per transfer
CREATE UNIQUE INDEX tx_transfer_leg_uq ON transactions (transfer_id, type) WHERE transfer_id IS NOT NULL;

-- budgets ---------------------------------------------------------------
CREATE TABLE budgets (
  id                uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  user_id           uuid NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  category_id       uuid REFERENCES categories(id) ON DELETE CASCADE,   -- NULL = overall budget
  period            text NOT NULL DEFAULT 'MONTHLY' CHECK (period IN ('MONTHLY')),
  amount_minor      bigint NOT NULL CHECK (amount_minor > 0),
  currency          char(3) NOT NULL,
  alert_thresholds  smallint[] NOT NULL DEFAULT '{80,100}',
  created_at        timestamptz NOT NULL DEFAULT now(),
  updated_at        timestamptz NOT NULL DEFAULT now(),
  CONSTRAINT budgets_user_category_period_uq UNIQUE NULLS NOT DISTINCT (user_id, category_id, period)
);

-- notifications (dedupe ledger + audit for emails/push) -----------------
CREATE TABLE notifications (
  id                   uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  user_id              uuid NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  kind                 text NOT NULL CHECK (kind IN ('BUDGET_ALERT','WEEKLY_TIPS')),
  channel              text NOT NULL DEFAULT 'EMAIL' CHECK (channel IN ('EMAIL','PUSH')),
  dedupe_key           text NOT NULL,   -- 'budget:{budget_id}:{YYYY-MM}:{80}' | 'weekly_tips:{YYYY-Www}'
  payload              jsonb NOT NULL DEFAULT '{}',
  status               text NOT NULL DEFAULT 'PENDING'
                         CHECK (status IN ('PENDING','SENT','FAILED','SKIPPED')),
  provider_message_id  text,
  error                text,
  created_at           timestamptz NOT NULL DEFAULT now(),
  sent_at              timestamptz,
  UNIQUE (user_id, dedupe_key)
);
CREATE INDEX notifications_user_created_idx ON notifications (user_id, created_at DESC);
```

Around the baseline:

- **Job-queue tables: not created.** The queue library is still OPEN (D-OPEN-9; `cmd/worker` is a stub, D-22). If River is confirmed,
  generate its tables with `river migrate-get --up --all` and commit them as a goose migration, so goose owns all DDL.
- **Seed (done):** `00002_seed_categories.sql` inserts the 21 system categories (same keys and kinds as `apps/mobile/constants/categories.ts`;
  names, emoji icons and colours copied from Welth, see `docs/MIGRATION.md` §7). Idempotent: `ON CONFLICT (key) WHERE user_id IS NULL DO NOTHING`.
  `sort_order`: expenses 10–150, income 210–260, "other" last in each kind.
- **Roles: not created yet (TODO at hosting time).** `arth_migrator` (DDL) and `arth_app` (DML on app tables + job-queue tables only); the API
  and worker will connect as `arth_app`. Creating roles needs a superuser and per-environment secrets, so it happens when hosting is set up
  (D-OPEN-3), not in goose. Locally everything runs as the docker-compose user.

Changes from Welth: `status`, `is_flagged` and `flag_reason` are dropped (never written). Reintroduce them with a real feature.
The starting balance becomes `opening_balance_minor`. Budget alert state moves to `notifications`.

### 3.2 Post-MVP tables (each in its own migration when the feature is built)

```sql
-- recurring_rules -------------------------------------------------------
CREATE TABLE recurring_rules (
  id              uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  user_id         uuid NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  account_id      uuid NOT NULL,
  category_id     uuid NOT NULL REFERENCES categories(id) ON DELETE RESTRICT,
  type            text NOT NULL CHECK (type IN ('INCOME','EXPENSE')),
  amount_minor    bigint NOT NULL CHECK (amount_minor > 0),
  currency        char(3) NOT NULL,
  description     text CHECK (length(description) <= 200),
  frequency       text NOT NULL CHECK (frequency IN ('DAILY','WEEKLY','MONTHLY','YEARLY')),
  interval_count  int  NOT NULL DEFAULT 1 CHECK (interval_count BETWEEN 1 AND 365),
  anchor_day      smallint CHECK (anchor_day BETWEEN 1 AND 31),  -- MONTHLY: clamp to month end
  starts_on       date NOT NULL,
  ends_on         date,
  next_occurs_on  date NOT NULL,
  auto_post       boolean NOT NULL DEFAULT true,                 -- false = remind only
  paused_at       timestamptz,
  created_at      timestamptz NOT NULL DEFAULT now(),
  updated_at      timestamptz NOT NULL DEFAULT now(),
  FOREIGN KEY (account_id, user_id) REFERENCES accounts (id, user_id) ON DELETE CASCADE
);
CREATE INDEX recurring_due_idx ON recurring_rules (next_occurs_on) WHERE paused_at IS NULL;
ALTER TABLE transactions ADD COLUMN recurring_rule_id uuid REFERENCES recurring_rules(id) ON DELETE SET NULL;
-- materialize idempotently: one posting per rule per date
CREATE UNIQUE INDEX tx_recurring_once_uq ON transactions (recurring_rule_id, occurred_on) WHERE recurring_rule_id IS NOT NULL;

-- goals -----------------------------------------------------------------
CREATE TABLE goals (
  id                 uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  user_id            uuid NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  name               text NOT NULL CHECK (length(name) BETWEEN 1 AND 60),
  target_minor       bigint NOT NULL CHECK (target_minor > 0),
  currency           char(3) NOT NULL,
  target_date        date,
  linked_account_id  uuid,                 -- progress = account balance when linked
  saved_minor        bigint NOT NULL DEFAULT 0 CHECK (saved_minor >= 0),  -- manual progress when not linked
  status             text NOT NULL DEFAULT 'ACTIVE' CHECK (status IN ('ACTIVE','ACHIEVED','ARCHIVED')),
  created_at         timestamptz NOT NULL DEFAULT now(),
  updated_at         timestamptz NOT NULL DEFAULT now(),
  FOREIGN KEY (linked_account_id, user_id) REFERENCES accounts (id, user_id) ON DELETE SET NULL (linked_account_id)
);

-- attachments (receipt images; needs object storage) --------------------
CREATE TABLE attachments (
  id              uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  user_id         uuid NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  transaction_id  uuid REFERENCES transactions(id) ON DELETE SET NULL,  -- orphan → swept + object deleted
  kind            text NOT NULL DEFAULT 'RECEIPT' CHECK (kind IN ('RECEIPT')),
  storage_key     text NOT NULL UNIQUE,      -- e.g. users/{user_id}/{uuid}.jpg
  content_type    text NOT NULL,
  size_bytes      int  NOT NULL CHECK (size_bytes BETWEEN 1 AND 10485760),
  sha256          bytea NOT NULL,
  created_at      timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX attachments_tx_idx ON attachments (transaction_id);
CREATE INDEX attachments_orphan_idx ON attachments (created_at) WHERE transaction_id IS NULL;
```

Note: `ON DELETE SET NULL (col)` with a column list needs PostgreSQL ≥ 15, which is already required.
Shared accounts, multi-currency (FX rates, per-transaction `fx_rate`), and bank import (`import_batches`, external ids with a
unique constraint) are deliberately not designed yet. The composite-FK ownership model will need to change for shared accounts
(`account_members`). Decide that before building it.

## 4. Authorization without RLS

- Middleware resolves `uid` once per request. Every sqlc query on user data takes `user_id` as a parameter, and
  `WHERE … AND user_id = @user_id` is mandatory (review rule, plus a test helper that runs each repository method with a
  second user and expects not-found).
- Composite FKs make cross-user references between accounts and transactions (and rules/goals) impossible at the DB level.
- Category references (system rows have `user_id IS NULL`) are checked in the query:
  `WHERE id = @id AND (user_id IS NULL OR user_id = @user_id) AND kind = @kind`.
- Cross-user queries exist **only** in the worker (scan jobs). They live in a separate sqlc file `queries/worker_*.sql` so they are easy to audit.

## 5. Balance updates (one DB transaction)

`accounts.balance_minor` is a cache for fast reads. The truth is `opening_balance_minor + Σ transactions.delta_minor`.
Every write that changes it runs inside one `pgx` transaction. The `UPDATE` is relative (`balance_minor = balance_minor + Δ`),
so concurrent requests never lose updates.

```sql
-- name: LockAccount :one
SELECT id, currency, archived_at FROM accounts WHERE id = @id AND user_id = @user_id FOR UPDATE;

-- name: InsertTransaction :one
INSERT INTO transactions (user_id, account_id, category_id, type, amount_minor, currency, occurred_on,
                          description, input_method, voice_transcript, transfer_id)
VALUES (@user_id, @account_id, @category_id, @type, @amount_minor, @currency, @occurred_on,
        @description, @input_method, @voice_transcript, @transfer_id)
RETURNING *;

-- name: ApplyBalanceDelta :execrows
UPDATE accounts SET balance_minor = balance_minor + @delta, updated_at = now()
WHERE id = @id AND user_id = @user_id;

-- name: DeleteTransaction :one
DELETE FROM transactions WHERE id = @id AND user_id = @user_id RETURNING account_id, delta_minor, transfer_id;
```

| Operation | Steps inside the tx |
| --- | --- |
| Create | `LockAccount` (404 if missing, 409 if archived; the currency is copied from the account) → validate category → `InsertTransaction` → `ApplyBalanceDelta(+delta)` (must affect 1 row) → enqueue the budget-check job in the same tx (**not yet**: `TODO(D-OPEN-9)`, no queue exists) → commit |
| Delete | `DeleteTransaction … RETURNING` → `ApplyBalanceDelta(-delta)`. If `transfer_id`, `DeleteTransferPeer` deletes the other leg(s) and each delta is reversed |
| Update | `SELECT … FOR UPDATE` on the old row → lock the old and new accounts **in ascending id order** (`LockAccounts`, prevents deadlocks; 409 `currency_mismatch` if the new account's currency differs) → reverse the old delta → update the row → apply the new delta → budget-check enqueue (`TODO(D-OPEN-9)`) |
| Transfer | **Not creatable yet** (D-21; no endpoint). Planned: lock both accounts in id order → insert `TRANSFER_OUT` + `TRANSFER_IN` with the same `transfer_id` → two deltas |
| Onboarding | Update user (currency, timezone, `onboarded_at`) → set the default account's `opening_balance_minor` and `balance_minor += opening` |
| Make default | `LockUserAccounts` locks **all** of the user's accounts (ascending id, same order as `LockAccounts`) to serialise concurrent default changes → no-op if already default → `ClearDefaultAccount` → `SetDefaultAccount` (the one-default partial unique index is not deferrable) |
| Delete account | `LockAccount` → 409 `default_account` / `account_has_transactions` (unless `cascade=true`) → `DELETE FROM accounts WHERE id AND user_id` (cascades transactions). Planned once transfers exist: delete and reverse peer legs in other accounts first (not implemented; no transfers can exist yet) |

Guards: integration tests assert the invariant (`testutil.AssertBalance`: cached balance = opening + Σ deltas) after the writes. The weekly
`balance_reconcile` job that logs drift is planned with the worker (D-OPEN-9), not built.
Isolation is READ COMMITTED (default). That is safe because of the row locks and relative updates.

## 6. Key read queries (sketch)

- **Dashboard month** (user-local month `[from, to)`):
  `SELECT type, category_id, SUM(amount_minor) FROM transactions WHERE user_id=$1 AND occurred_on >= $2 AND occurred_on < $3 AND type IN ('INCOME','EXPENSE') GROUP BY ROLLUP …`
- **Keyset page:** `… WHERE user_id=$1 [filters] AND (occurred_on, created_at, id) < ($c_date, $c_created, $c_id) ORDER BY occurred_on DESC, created_at DESC, id DESC LIMIT $n+1`.
  The cursor is base64 of the last tuple.
- **Search (`q`):** `description ILIKE '%' || $q || '%'` within the user's rows (small per-user sets). Add a `pg_trgm` index only if needed.

## 7. Optional: port Welth dev data

There are no production users, so nothing has to migrate. If the old Supabase project holds dev data worth keeping, use a one-off Go command
(`apps/api/cmd/tools/import-welth`). Clerk users cannot be ported with passwords (Arth never sees them), so imported users must set a new
password, which needs the reset flow (OPEN). Mapping: `numeric amount` → `round(amount * 10^exponent)::bigint` (reject non-integral values);
`date timestamptz` → `(date AT TIME ZONE tz)::date`; category text key → `categories.id`; "Starting balance" income rows →
`accounts.opening_balance_minor`; recompute and compare `balance_minor`.
