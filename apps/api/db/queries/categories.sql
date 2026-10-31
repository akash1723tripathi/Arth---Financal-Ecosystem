-- categories: system rows (user_id IS NULL) are visible to everyone; custom rows only to their owner.

-- name: ListCategories :many
-- System first, then custom; each by sort_order, name. kind NULL = both kinds. Archived rows are excluded.
SELECT * FROM categories
WHERE (user_id IS NULL OR user_id = @user_id::uuid)
  AND archived_at IS NULL
  AND (sqlc.narg('kind')::text IS NULL OR kind = sqlc.narg('kind')::text)
ORDER BY (user_id IS NOT NULL), sort_order, lower(name), id;

-- name: GetUsableCategory :one
-- Validates a category reference for a transaction/budget: visible to the user, matching kind, not archived.
SELECT * FROM categories
WHERE id = @id
  AND (user_id IS NULL OR user_id = @user_id::uuid)
  AND kind = @kind
  AND archived_at IS NULL;

-- name: GetCategory :one
-- Any visible category (archived included), e.g. to render an old transaction.
SELECT * FROM categories
WHERE id = @id AND (user_id IS NULL OR user_id = @user_id::uuid);

-- name: GetSystemCategoryByKey :one
-- Maps an AI/legacy category key to the system category (not user data).
SELECT * FROM categories WHERE key = @key::text AND user_id IS NULL AND archived_at IS NULL;
