-- Tokens that let an AI assistant act for a person over MCP, and the OAuth clients
-- that hold some of them.

-- name: CreateMCPToken :one
INSERT INTO mcp_tokens (user_id, kind, name, client_id, access_hash, access_expires_at, refresh_hash, refresh_expires_at)
VALUES (sqlc.arg('user_id'), sqlc.arg('kind'), sqlc.arg('name'), sqlc.narg('client_id'),
        sqlc.arg('access_hash'), sqlc.narg('access_expires_at'), sqlc.narg('refresh_hash'), sqlc.narg('refresh_expires_at'))
RETURNING *;

-- name: MCPTokenByAccess :one
-- A live token: not revoked, not expired, and its owner still has access.
SELECT t.* FROM mcp_tokens t
JOIN users u ON u.id = t.user_id AND u.is_active
WHERE t.access_hash = $1 AND t.revoked_at IS NULL
  AND (t.access_expires_at IS NULL OR t.access_expires_at > now());

-- name: MCPTokenByRefresh :one
SELECT t.* FROM mcp_tokens t
JOIN users u ON u.id = t.user_id AND u.is_active
WHERE t.refresh_hash = $1 AND t.revoked_at IS NULL AND t.refresh_expires_at > now()
FOR UPDATE OF t;

-- name: RotateMCPToken :exec
-- A refresh replaces both secrets, so a stolen refresh token works once at most.
UPDATE mcp_tokens SET
    access_hash = sqlc.arg('access_hash'), access_expires_at = sqlc.arg('access_expires_at'),
    refresh_hash = sqlc.arg('refresh_hash'), refresh_expires_at = sqlc.arg('refresh_expires_at')
WHERE id = sqlc.arg('id');

-- name: TouchMCPToken :exec
-- At most once a minute, so a busy assistant doesn't write on every call.
UPDATE mcp_tokens SET last_used_at = now()
WHERE id = $1 AND (last_used_at IS NULL OR last_used_at < now() - interval '1 minute');

-- name: ListMCPTokens :many
SELECT t.id, t.kind, t.name, t.client_id, t.created_at, t.last_used_at, t.access_expires_at, t.refresh_expires_at
FROM mcp_tokens t
WHERE t.user_id = $1 AND t.revoked_at IS NULL
  AND (t.kind = 'personal' OR t.refresh_expires_at > now())
ORDER BY t.created_at DESC;

-- name: RevokeMCPToken :execrows
UPDATE mcp_tokens SET revoked_at = now()
WHERE id = sqlc.arg('id') AND user_id = sqlc.arg('user_id') AND revoked_at IS NULL;

-- name: RevokeUserMCPTokens :execrows
UPDATE mcp_tokens SET revoked_at = now() WHERE user_id = $1 AND revoked_at IS NULL;

-- name: CountUserMCPTokens :one
SELECT count(*) FROM mcp_tokens WHERE user_id = $1 AND revoked_at IS NULL AND kind = 'personal';

-- name: UpsertOAuthClient :one
INSERT INTO oauth_clients (id, kind, name, redirect_uris)
VALUES (sqlc.arg('id'), sqlc.arg('kind'), sqlc.arg('name'), sqlc.arg('redirect_uris'))
ON CONFLICT (id) DO UPDATE SET name = excluded.name, redirect_uris = excluded.redirect_uris, refreshed_at = now()
RETURNING *;

-- name: GetOAuthClient :one
SELECT * FROM oauth_clients WHERE id = $1;

-- name: ProjectIDByNumber :one
-- Assistants (and people) refer to projects as #12; visibility is checked by the caller.
SELECT id FROM projects WHERE number = $1;
