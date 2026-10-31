-- Arth MVP baseline schema (docs/SCHEMA.md §3.1). Requires PostgreSQL >= 15 (UNIQUE NULLS NOT DISTINCT).
--
-- Not in this migration (on purpose):
--   * auth_tokens (email verification / password reset): planned with the email-provider decision (OPEN).
--   * Job-queue tables: added once the queue library (River or equivalent) is confirmed.
--   * TODO(hosting): roles arth_migrator (DDL) and arth_app (DML on app tables only). Creating roles needs a
--     superuser and per-environment secrets, so they are created at hosting time, not by goose.

-- +goose Up

-- users -----------------------------------------------------------------
CREATE TABLE users (
  id                   uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  email                text NOT NULL CHECK (length(email) BETWEEN 3 AND 254),  -- stored as entered (trimmed in Go)
  email_verified_at    timestamptz,
  password_hash        text NOT NULL,                  -- Argon2id PHC string
  password_changed_at  timestamptz NOT NULL DEFAULT now(),
  name                 text CHECK (length(name) <= 100),
  avatar_key           text,
  currency             char(3),                        -- NULL until onboarding
  timezone             text NOT NULL DEFAULT 'UTC',    -- IANA, validated in Go
  onboarded_at         timestamptz,
  email_budget_alerts  boolean NOT NULL DEFAULT true,
  email_weekly_tips    boolean NOT NULL DEFAULT true,
  created_at           timestamptz NOT NULL DEFAULT now(),
  updated_at           timestamptz NOT NULL DEFAULT now()
);
-- case-insensitive uniqueness without citext; look-ups use lower(email) too
CREATE UNIQUE INDEX users_email_lower_uq ON users (lower(email));

-- sessions: one per signed-in device/browser -----------------------------
CREATE TABLE sessions (
  id             uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  user_id        uuid NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  client         text NOT NULL CHECK (client IN ('MOBILE','WEB')),
  user_agent     text CHECK (length(user_agent) <= 512),
  ip             inet,
  created_at     timestamptz NOT NULL DEFAULT now(),
  last_used_at   timestamptz NOT NULL DEFAULT now(),
  expires_at     timestamptz NOT NULL,
  revoked_at     timestamptz,
  revoke_reason  text CHECK (revoke_reason IN ('LOGOUT','LOGOUT_ALL','REFRESH_REUSE','PASSWORD_CHANGED','ACCOUNT_DELETED','ADMIN'))
);
CREATE INDEX sessions_user_active_idx ON sessions (user_id) WHERE revoked_at IS NULL;

-- refresh_tokens: rotating, single-use, stored hashed (SHA-256) ----------
CREATE TABLE refresh_tokens (
  id            uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  session_id    uuid NOT NULL REFERENCES sessions(id) ON DELETE CASCADE,
  token_hash    bytea NOT NULL UNIQUE,
  created_at    timestamptz NOT NULL DEFAULT now(),
  expires_at    timestamptz NOT NULL,
  used_at       timestamptz,
  replaced_by   uuid REFERENCES refresh_tokens(id) ON DELETE SET NULL
);
CREATE INDEX refresh_tokens_session_idx ON refresh_tokens (session_id);

-- categories --------------------------------------------------------------
-- System categories: user_id NULL + stable key (seeded in 00002). Custom categories: user_id set, key NULL.
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

-- accounts ------------------------------------------------------------------
CREATE TABLE accounts (
  id                     uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  user_id                uuid NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  name                   text NOT NULL CHECK (length(name) BETWEEN 1 AND 60),
  type                   text NOT NULL CHECK (type IN ('CASH','BANK','CREDIT_CARD','SAVINGS')),
  currency               char(3) NOT NULL,
  opening_balance_minor  bigint NOT NULL DEFAULT 0,
  balance_minor          bigint NOT NULL DEFAULT 0,  -- cache: opening + sum(delta_minor)
  is_default             boolean NOT NULL DEFAULT false,
  archived_at            timestamptz,
  created_at             timestamptz NOT NULL DEFAULT now(),
  updated_at             timestamptz NOT NULL DEFAULT now(),
  UNIQUE (id, user_id)                               -- target for composite FKs
);
CREATE UNIQUE INDEX accounts_one_default_per_user ON accounts (user_id) WHERE is_default;
CREATE INDEX accounts_user_idx ON accounts (user_id, created_at);

-- transactions --------------------------------------------------------------
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
CREATE INDEX tx_user_date_idx        ON transactions (user_id, occurred_on DESC, created_at DESC, id DESC);
-- account filter + reconcile + cascade
CREATE INDEX tx_account_date_idx     ON transactions (account_id, occurred_on DESC);
-- budget progress / dashboard breakdown
CREATE INDEX tx_user_expense_cat_idx ON transactions (user_id, occurred_on, category_id) WHERE type = 'EXPENSE';
-- one OUT + one IN per transfer
CREATE UNIQUE INDEX tx_transfer_leg_uq ON transactions (transfer_id, type) WHERE transfer_id IS NOT NULL;

-- budgets -------------------------------------------------------------------
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

-- notifications (dedupe ledger + audit for emails/push) ---------------------
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

-- +goose Down
DROP TABLE IF EXISTS notifications;
DROP TABLE IF EXISTS budgets;
DROP TABLE IF EXISTS transactions;
DROP TABLE IF EXISTS accounts;
DROP TABLE IF EXISTS categories;
DROP TABLE IF EXISTS refresh_tokens;
DROP TABLE IF EXISTS sessions;
DROP TABLE IF EXISTS users;
