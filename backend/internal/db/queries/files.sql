-- name: CreateFile :one
-- A file hangs on a project or on a client request (never both), and within that on the
-- project itself, one of its updates, or one of its board cards.
INSERT INTO files (project_id, client_request_id, update_id, card_id, original_name, uploaded_by)
VALUES (sqlc.narg('project_id'), sqlc.narg('client_request_id'), sqlc.narg('update_id'), sqlc.narg('card_id'),
        sqlc.arg('original_name'), sqlc.arg('uploaded_by'))
RETURNING *;

-- name: LockFile :one
SELECT * FROM files WHERE id = $1 FOR UPDATE;

-- name: NextFileVersionNo :one
SELECT coalesce(max(version_no), 0)::int + 1 FROM file_versions WHERE file_id = $1;

-- name: InsertFileVersion :one
INSERT INTO file_versions (file_id, version_no, storage_key, size_bytes, mime_type, sha256, uploaded_by)
VALUES ($1, $2, $3, $4, $5, $6, $7)
RETURNING *;

-- name: SetFileCurrentVersion :exec
UPDATE files SET current_version_id = $2, original_name = $3 WHERE id = $1;

-- name: RemoveFile :exec
UPDATE files SET removed_at = now() WHERE id = $1;

-- name: ListProjectFiles :many
-- Project-level files (not update attachments) that haven't been removed.
SELECT f.id, f.original_name, f.created_at, f.uploaded_by, fu.name AS uploaded_by_name,
       v.id AS version_id, v.version_no, v.size_bytes, v.mime_type, v.created_at AS version_created_at,
       vu.name AS version_uploaded_by_name,
       (SELECT count(*) FROM file_versions x WHERE x.file_id = f.id) AS version_count
FROM files f
JOIN file_versions v ON v.id = f.current_version_id
JOIN users fu ON fu.id = f.uploaded_by
JOIN users vu ON vu.id = v.uploaded_by
WHERE f.project_id = $1 AND f.update_id IS NULL AND f.card_id IS NULL AND f.removed_at IS NULL
ORDER BY f.created_at;

-- name: ListFileVersionsForProject :many
SELECT v.id, v.file_id, v.version_no, v.size_bytes, v.mime_type, v.sha256, v.created_at,
       v.uploaded_by, u.name AS uploaded_by_name
FROM file_versions v
JOIN files f ON f.id = v.file_id
JOIN users u ON u.id = v.uploaded_by
WHERE f.project_id = $1 AND f.update_id IS NULL AND f.card_id IS NULL AND f.removed_at IS NULL
ORDER BY v.file_id, v.version_no DESC;

-- name: GetFileVersion :one
SELECT v.id, v.file_id, v.version_no, v.storage_key, v.size_bytes, v.mime_type,
       f.project_id, f.update_id, f.card_id, f.original_name, f.removed_at
FROM file_versions v JOIN files f ON f.id = v.file_id
WHERE v.id = $1;

-- name: ListCardFiles :many
SELECT f.id, f.original_name, f.created_at, f.uploaded_by, fu.name AS uploaded_by_name,
       v.id AS version_id, v.size_bytes, v.mime_type
FROM files f
JOIN file_versions v ON v.id = f.current_version_id
JOIN users fu ON fu.id = f.uploaded_by
WHERE f.card_id = $1 AND f.removed_at IS NULL
ORDER BY f.created_at;
