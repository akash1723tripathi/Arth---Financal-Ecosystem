-- budgets: monthly, overall (category_id NULL) or per category. Scoped by user_id.
-- Range params start_on (inclusive) / end_before (exclusive) = one user-local month [first day, first day of next).

-- name: UpsertBudget :one
-- category_id NULL = overall budget. UNIQUE NULLS NOT DISTINCT makes the NULL row conflict too.
INSERT INTO budgets (user_id, category_id, amount_minor, currency)
VALUES (@user_id, sqlc.narg('category_id'), @amount_minor, @currency)
ON CONFLICT ON CONSTRAINT budgets_user_category_period_uq DO UPDATE
SET amount_minor = EXCLUDED.amount_minor,
    currency     = EXCLUDED.currency,
    updated_at   = now()
RETURNING *;

-- name: GetBudgets :many
-- Overall first, then per-category budgets.
SELECT * FROM budgets
WHERE user_id = @user_id
ORDER BY category_id NULLS FIRST, created_at;

-- name: GetBudget :one
-- category_id NULL = the overall budget.
SELECT * FROM budgets
WHERE user_id = @user_id
  AND category_id IS NOT DISTINCT FROM sqlc.narg('category_id')::uuid
  AND period = 'MONTHLY';

-- name: DeleteBudget :execrows
-- category_id NULL = the overall budget.
DELETE FROM budgets
WHERE user_id = @user_id
  AND category_id IS NOT DISTINCT FROM sqlc.narg('category_id')::uuid
  AND period = 'MONTHLY';

-- name: SpentInRange :one
-- EXPENSE total in [start_on, end_before) in one currency (the budget's). category_id NULL = all categories.
SELECT COALESCE(SUM(amount_minor), 0)::bigint AS spent_minor
FROM transactions
WHERE user_id = @user_id
  AND type = 'EXPENSE'
  AND currency = @currency
  AND occurred_on >= @start_on::date
  AND occurred_on <  @end_before::date
  AND (sqlc.narg('category_id')::uuid IS NULL OR category_id = sqlc.narg('category_id')::uuid);
