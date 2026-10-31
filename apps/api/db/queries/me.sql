-- me: queries for /v1/me that are not part of the auth-owned users.sql. Scoped by the principal's user id.

-- name: UserHasTransactions :one
-- PATCH /v1/me: the default currency is locked once the user has any transaction (409 currency_locked).
SELECT EXISTS (SELECT 1 FROM transactions WHERE user_id = @user_id)::boolean AS has_transactions;
