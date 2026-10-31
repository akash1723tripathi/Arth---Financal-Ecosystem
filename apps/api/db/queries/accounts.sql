-- accounts: every query is scoped by user_id. Balance writes are relative and run inside db.InTx.

-- name: ListAccounts :many
-- Default first, then oldest first. Archived accounts are excluded.
SELECT * FROM accounts
WHERE user_id = @user_id AND archived_at IS NULL
ORDER BY is_default DESC, created_at, id;

-- name: GetAccount :one
SELECT * FROM accounts WHERE id = @id AND user_id = @user_id;

-- name: GetDefaultAccount :one
SELECT * FROM accounts WHERE user_id = @user_id AND is_default;

-- name: LockAccount :one
SELECT * FROM accounts WHERE id = @id AND user_id = @user_id FOR UPDATE;

-- name: LockAccounts :many
-- Locks several accounts in ascending id order (deadlock-free; docs/SCHEMA.md §5). Returns only the caller's rows:
-- compare len(result) with the number of distinct ids to detect a missing/foreign account.
SELECT * FROM accounts
WHERE user_id = @user_id AND id = ANY(@ids::uuid[])
ORDER BY id
FOR UPDATE;

-- name: CreateAccount :one
-- balance_minor starts at the opening balance.
INSERT INTO accounts (user_id, name, type, currency, opening_balance_minor, balance_minor, is_default)
VALUES (@user_id, @name, @type, @currency, @opening_balance_minor, @opening_balance_minor, @is_default)
RETURNING *;

-- name: UpdateAccount :one
-- PATCH /v1/accounts/{id}: NULL argument = unchanged.
UPDATE accounts SET
  name       = COALESCE(sqlc.narg('name')::text, name),
  type       = COALESCE(sqlc.narg('type')::text, type),
  updated_at = now()
WHERE id = @id AND user_id = @user_id
RETURNING *;

-- name: ClearDefaultAccount :exec
-- Run before SetDefaultAccount in the same tx (the one-default index is not deferrable).
UPDATE accounts SET is_default = false, updated_at = now()
WHERE user_id = @user_id AND is_default;

-- name: SetDefaultAccount :one
UPDATE accounts SET is_default = true, updated_at = now()
WHERE id = @id AND user_id = @user_id
RETURNING *;

-- name: CountAccountTransactions :one
SELECT count(*)::bigint AS transaction_count
FROM transactions WHERE account_id = @account_id AND user_id = @user_id;

-- name: DeleteAccount :execrows
-- Cascades to the account's transactions (composite FK ON DELETE CASCADE).
DELETE FROM accounts WHERE id = @id AND user_id = @user_id;

-- name: ApplyBalanceDelta :execrows
-- Relative update: concurrent writers never lose updates. Must affect exactly 1 row.
UPDATE accounts SET balance_minor = balance_minor + @delta::bigint, updated_at = now()
WHERE id = @id AND user_id = @user_id;

-- name: SetDefaultAccountOpening :one
-- Onboarding: set the default account's currency and opening balance. The balance moves by (new - old opening),
-- so the invariant balance = opening + sum(delta) holds even if the opening was not 0. The caller checks first
-- that a currency change is allowed (no transactions -> else 409 currency_locked).
UPDATE accounts SET
  currency              = @currency,
  balance_minor         = balance_minor - opening_balance_minor + @opening_balance_minor::bigint,
  opening_balance_minor = @opening_balance_minor::bigint,
  updated_at            = now()
WHERE user_id = @user_id AND is_default
RETURNING *;

-- name: SumBalancesByCurrency :many
-- Dashboard total balance, one row per currency (no FX conversion). Archived accounts excluded.
SELECT currency, COALESCE(SUM(balance_minor), 0)::bigint AS total_minor
FROM accounts
WHERE user_id = @user_id AND archived_at IS NULL
GROUP BY currency
ORDER BY currency;

-- name: LockUserAccounts :many
-- Locks every account of the user in ascending id order (same order as LockAccounts, so it cannot deadlock with
-- transaction writes). make-default uses it to serialise concurrent default changes: after it returns, the
-- following statements see every committed change (READ COMMITTED takes a new snapshot per statement).
SELECT id FROM accounts
WHERE user_id = @user_id
ORDER BY id
FOR UPDATE;
