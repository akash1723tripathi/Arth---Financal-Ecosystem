-- sessions + refresh_tokens (docs/AUTH.md). refresh_tokens has no user_id column: it is reached only through its
-- session, and every session query is scoped by user_id. GetRefreshTokenForUpdate is the one lookup by secret
-- (token hash) and returns the owning session's user_id for the caller to use from then on.

-- name: CreateSession :one
INSERT INTO sessions (user_id, client, user_agent, ip, expires_at)
VALUES (@user_id, @client, sqlc.narg('user_agent'), sqlc.narg('ip'), @expires_at)
RETURNING *;

-- name: GetSession :one
SELECT * FROM sessions WHERE id = @id AND user_id = @user_id;

-- name: IsSessionActive :one
SELECT EXISTS (
  SELECT 1 FROM sessions
  WHERE id = @id AND user_id = @user_id AND revoked_at IS NULL AND expires_at > now()
) AS active;

-- name: TouchSession :execrows
UPDATE sessions SET last_used_at = now()
WHERE id = @id AND user_id = @user_id AND revoked_at IS NULL;

-- name: RevokeSession :execrows
-- Already-revoked sessions are left untouched (0 rows).
UPDATE sessions SET revoked_at = now(), revoke_reason = @reason::text
WHERE id = @id AND user_id = @user_id AND revoked_at IS NULL;

-- name: RevokeAllUserSessions :execrows
-- except_id NULL = revoke every active session; otherwise keep that one.
UPDATE sessions SET revoked_at = now(), revoke_reason = @reason::text
WHERE user_id = @user_id AND revoked_at IS NULL
  AND (sqlc.narg('except_id')::uuid IS NULL OR id <> sqlc.narg('except_id')::uuid);

-- name: RevokeOtherSessions :execrows
-- Password change: revoke every active session except the current one.
UPDATE sessions SET revoked_at = now(), revoke_reason = @reason::text
WHERE user_id = @user_id AND revoked_at IS NULL AND id <> @keep_id;

-- name: CreateRefreshToken :one
INSERT INTO refresh_tokens (session_id, token_hash, expires_at)
VALUES (@session_id, @token_hash, @expires_at)
RETURNING *;

-- name: GetRefreshTokenForUpdate :one
-- Locks the token row so two concurrent refreshes with the same token serialize: the second one sees used_at set
-- (reuse -> revoke the session).
SELECT rt.id, rt.session_id, rt.token_hash, rt.created_at, rt.expires_at, rt.used_at, rt.replaced_by,
       s.user_id,
       s.client,
       s.expires_at   AS session_expires_at,
       s.revoked_at   AS session_revoked_at,
       s.revoke_reason AS session_revoke_reason
FROM refresh_tokens rt
JOIN sessions s ON s.id = rt.session_id
WHERE rt.token_hash = @token_hash
FOR UPDATE OF rt;

-- name: MarkRefreshTokenUsed :execrows
-- 0 rows = it was already used (treat as reuse).
UPDATE refresh_tokens SET used_at = now() WHERE id = @id AND used_at IS NULL;

-- name: SetRefreshTokenReplacedBy :exec
UPDATE refresh_tokens SET replaced_by = @replaced_by::uuid WHERE id = @id;
