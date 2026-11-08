-- Queue behind the Google Sheet backup. Rows are marked dirty by triggers (see
-- migration 00016); the worker claims them, writes the sheet, then marks them synced.

-- name: ClaimSheetSync :many
-- The projects waiting to be written, oldest first, with everything a row needs.
SELECT s.project_id, s.row_number, s.attempts, sqlc.embed(v)
FROM sheet_sync s
JOIN project_details v ON v.id = s.project_id
WHERE s.dirty AND s.next_attempt_at <= now()
ORDER BY s.queued_at
LIMIT sqlc.arg('lim');

-- name: MarkSheetSynced :exec
UPDATE sheet_sync SET
    dirty = false, row_number = sqlc.arg('row_number'), synced_at = now(),
    attempts = 0, last_error = ''
WHERE project_id = sqlc.arg('project_id')
  -- Another change arrived while we were writing: leave it dirty for the next pass.
  AND queued_at <= sqlc.arg('claimed_at')::timestamptz;

-- name: MarkSheetFailed :exec
UPDATE sheet_sync SET
    attempts = attempts + 1,
    last_error = left(sqlc.arg('last_error')::text, 500),
    -- Back off a minute per try, up to ten.
    next_attempt_at = now() + (least(attempts + 1, 10) * interval '1 minute')
WHERE project_id = sqlc.arg('project_id');

-- name: SheetSyncStatus :one
SELECT
    count(*) AS total,
    count(*) FILTER (WHERE dirty) AS pending,
    count(*) FILTER (WHERE dirty AND attempts > 0) AS failing,
    coalesce((SELECT last_error FROM sheet_sync WHERE last_error <> '' ORDER BY queued_at DESC LIMIT 1), '')::text AS last_error
FROM sheet_sync;

-- name: LastSheetSync :one
-- Not found means nothing has been written to the sheet yet.
SELECT synced_at::timestamptz FROM sheet_sync
WHERE synced_at IS NOT NULL ORDER BY synced_at DESC LIMIT 1;

-- name: EnqueueAllProjects :execrows
-- Back up everything again (used by the backfill command and button).
INSERT INTO sheet_sync (project_id)
SELECT id FROM projects WHERE status <> 'draft'
ON CONFLICT (project_id) DO UPDATE SET
    dirty = true, queued_at = now(), next_attempt_at = now(), attempts = 0, last_error = '',
    -- Forget where the row was: the backfill rebuilds the sheet from scratch.
    row_number = NULL;

-- name: ForgetSheetRows :exec
UPDATE sheet_sync SET row_number = NULL;
