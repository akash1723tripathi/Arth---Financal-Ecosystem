-- users: the authenticated user's own row. "user data" scoping here is id = @id (the principal's user id),
-- except GetUserByEmail, which is the login/register lookup and runs before a principal exists.

-- name: CreateUser :one
INSERT INTO users (email, password_hash, name)
VALUES (@email, @password_hash, sqlc.narg('name'))
RETURNING *;

-- name: GetUserByID :one
SELECT * FROM users WHERE id = @id;

-- name: GetUserByEmail :one
-- Case-insensitive; uses users_email_lower_uq.
SELECT * FROM users WHERE lower(email) = lower(@email::text);

-- name: UpdateUserPasswordHash :exec
-- Password change: also bumps password_changed_at. Revoke other sessions in the same tx.
UPDATE users
SET password_hash = @password_hash, password_changed_at = now(), updated_at = now()
WHERE id = @id;

-- name: RehashUserPassword :exec
-- Transparent rehash on login when Argon2 parameters changed: password_changed_at is NOT touched.
UPDATE users SET password_hash = @password_hash, updated_at = now() WHERE id = @id;

-- name: UpdateUserProfile :one
-- PATCH /v1/me. NULL argument = unchanged. name = '' clears the name (stored as NULL).
UPDATE users SET
  name                = CASE WHEN sqlc.narg('name')::text IS NULL THEN name
                             ELSE NULLIF(sqlc.narg('name')::text, '') END,
  currency            = COALESCE(sqlc.narg('currency')::text, currency),
  timezone            = COALESCE(sqlc.narg('timezone')::text, timezone),
  email_budget_alerts = COALESCE(sqlc.narg('email_budget_alerts')::boolean, email_budget_alerts),
  email_weekly_tips   = COALESCE(sqlc.narg('email_weekly_tips')::boolean, email_weekly_tips),
  updated_at          = now()
WHERE id = @id
RETURNING *;

-- name: CompleteOnboarding :one
-- Only succeeds once: returns pgx.ErrNoRows when the user is already onboarded (map to 409 already_onboarded
-- after GetUserByID confirmed the user exists).
UPDATE users
SET currency = @currency::text, timezone = @timezone, onboarded_at = now(), updated_at = now()
WHERE id = @id AND onboarded_at IS NULL
RETURNING *;

-- name: DeleteUser :execrows
-- Cascades to sessions, refresh_tokens, categories (custom), accounts, transactions, budgets, notifications.
DELETE FROM users WHERE id = @id;
