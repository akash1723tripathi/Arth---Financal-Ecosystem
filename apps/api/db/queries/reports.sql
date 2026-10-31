-- reports / dashboard aggregates. Scoped by user_id. Transfers (TRANSFER_IN/OUT) are always excluded.
-- Month ranges use start_on (inclusive) / end_before (exclusive); DailyTotals uses from_date/to_date INCLUSIVE.
-- No FX: aggregates are restricted to one currency.

-- name: MonthTotals :one
SELECT COALESCE(SUM(amount_minor) FILTER (WHERE type = 'INCOME'), 0)::bigint  AS income_minor,
       COALESCE(SUM(amount_minor) FILTER (WHERE type = 'EXPENSE'), 0)::bigint AS expense_minor
FROM transactions
WHERE user_id = @user_id
  AND type IN ('INCOME', 'EXPENSE')
  AND currency = @currency
  AND occurred_on >= @start_on::date
  AND occurred_on <  @end_before::date;

-- name: ExpenseByCategory :many
-- Largest first. category_id is never NULL for EXPENSE rows (CHECK), but the column is nullable in Go.
SELECT category_id, SUM(amount_minor)::bigint AS amount_minor
FROM transactions
WHERE user_id = @user_id
  AND type = 'EXPENSE'
  AND currency = @currency
  AND occurred_on >= @start_on::date
  AND occurred_on <  @end_before::date
GROUP BY category_id
ORDER BY amount_minor DESC, category_id;

-- name: DailyTotals :many
-- Zero-filled in SQL via generate_series: exactly one row per day in [from_date, to_date] (inclusive), ascending.
-- account_id NULL = all accounts; currency NULL = all currencies (pass the account's or the user's currency).
-- The caller enforces the max range (93 days).
SELECT d.day::date AS day,
       COALESCE(SUM(t.amount_minor) FILTER (WHERE t.type = 'INCOME'), 0)::bigint  AS income_minor,
       COALESCE(SUM(t.amount_minor) FILTER (WHERE t.type = 'EXPENSE'), 0)::bigint AS expense_minor
FROM generate_series(@from_date::date, @to_date::date, interval '1 day') AS d(day)
LEFT JOIN transactions t
  ON t.occurred_on = d.day::date
 AND t.user_id = @user_id
 AND t.type IN ('INCOME', 'EXPENSE')
 AND (sqlc.narg('currency')::text IS NULL OR t.currency = sqlc.narg('currency')::text)
 AND (sqlc.narg('account_id')::uuid IS NULL OR t.account_id = sqlc.narg('account_id')::uuid)
GROUP BY d.day
ORDER BY d.day;
