-- One-to-one chat and the call log.

-- name: SendMessage :one
INSERT INTO messages (sender_id, recipient_id, body)
VALUES (sqlc.arg('sender_id'), sqlc.arg('recipient_id'), btrim(sqlc.arg('body')))
RETURNING *;

-- name: MessageThread :many
-- Every message between two people, newest first; `before` pages backwards.
SELECT m.*, s.name AS sender_name
FROM messages m JOIN users s ON s.id = m.sender_id
WHERE ((m.sender_id = sqlc.arg('me') AND m.recipient_id = sqlc.arg('other'))
    OR (m.sender_id = sqlc.arg('other') AND m.recipient_id = sqlc.arg('me')))
  AND (sqlc.narg('before')::timestamptz IS NULL OR m.created_at < sqlc.narg('before'))
ORDER BY m.created_at DESC
LIMIT sqlc.arg('lim');

-- name: GetMessage :one
SELECT m.*, s.name AS sender_name
FROM messages m JOIN users s ON s.id = m.sender_id
WHERE m.id = $1;

-- name: Inbox :many
-- One row per person this user has talked to: the last thing said and what's unread.
WITH mine AS (
    SELECT m.*, CASE WHEN m.sender_id = sqlc.arg('me') THEN m.recipient_id ELSE m.sender_id END AS other
    FROM messages m
    WHERE m.sender_id = sqlc.arg('me') OR m.recipient_id = sqlc.arg('me')
), newest AS (
    SELECT DISTINCT ON (other) other, body, created_at, (sender_id = sqlc.arg('me')) AS from_me
    FROM mine ORDER BY other, created_at DESC
), unread AS (
    SELECT other, count(*) AS waiting FROM mine
    WHERE recipient_id = sqlc.arg('me') AND read_at IS NULL
    GROUP BY other
)
SELECT u.id AS user_id, u.name AS user_name, u.role AS user_role, u.is_active,
       newest.body AS last_body, newest.created_at AS last_at,
       newest.from_me AS last_from_me,
       coalesce(unread.waiting, 0)::bigint AS unread
FROM newest
JOIN users u ON u.id = newest.other
LEFT JOIN unread ON unread.other = newest.other
ORDER BY newest.created_at DESC
LIMIT sqlc.arg('lim');

-- name: MarkThreadRead :execrows
UPDATE messages SET read_at = now()
WHERE recipient_id = sqlc.arg('me') AND sender_id = sqlc.arg('other') AND read_at IS NULL;

-- name: CountUnreadMessages :one
SELECT count(*) FROM messages WHERE recipient_id = $1 AND read_at IS NULL;

-- ── Calls ──────────────────────────────────────────────────

-- name: StartCall :one
INSERT INTO calls (caller_id, callee_id) VALUES (sqlc.arg('caller_id'), sqlc.arg('callee_id'))
RETURNING *;

-- name: GetCall :one
SELECT c.*, caller.name AS caller_name, callee.name AS callee_name
FROM calls c
JOIN users caller ON caller.id = c.caller_id
JOIN users callee ON callee.id = c.callee_id
WHERE c.id = $1;

-- name: SetCallStatus :one
-- Only ever moves a live call on, so a late "ended" can't revive a finished one.
UPDATE calls SET
    status      = sqlc.arg('status'),
    answered_at = CASE WHEN sqlc.arg('status')::text = 'answered' THEN now() ELSE answered_at END,
    ended_at    = CASE WHEN sqlc.arg('status')::text IN ('declined', 'missed', 'ended', 'failed') THEN now() ELSE ended_at END
WHERE id = sqlc.arg('id') AND status IN ('ringing', 'answered')
RETURNING *;

-- name: LiveCallFor :one
-- The call this person is in, if any (either end of it).
SELECT c.*, caller.name AS caller_name, callee.name AS callee_name
FROM calls c
JOIN users caller ON caller.id = c.caller_id
JOIN users callee ON callee.id = c.callee_id
WHERE (c.caller_id = sqlc.arg('me') OR c.callee_id = sqlc.arg('me'))
  AND c.status IN ('ringing', 'answered')
ORDER BY c.started_at DESC
LIMIT 1;

-- name: ExpireRingingCalls :many
-- A call nobody picked up counts as missed, so the ringing stops by itself.
UPDATE calls SET status = 'missed', ended_at = now()
WHERE status = 'ringing' AND started_at < now() - (sqlc.arg('after_seconds')::int * interval '1 second')
RETURNING id, caller_id, callee_id;

-- name: RecentCalls :many
SELECT c.*, caller.name AS caller_name, callee.name AS callee_name
FROM calls c
JOIN users caller ON caller.id = c.caller_id
JOIN users callee ON callee.id = c.callee_id
WHERE c.caller_id = sqlc.arg('me') OR c.callee_id = sqlc.arg('me')
ORDER BY c.started_at DESC
LIMIT sqlc.arg('lim');

-- name: CallsBetween :many
-- Calls between two people in a time window, so they show in their conversation
-- among the messages of the same period. Newest first.
SELECT c.*, caller.name AS caller_name, callee.name AS callee_name
FROM calls c
JOIN users caller ON caller.id = c.caller_id
JOIN users callee ON callee.id = c.callee_id
WHERE ((c.caller_id = sqlc.arg('me') AND c.callee_id = sqlc.arg('other'))
    OR (c.caller_id = sqlc.arg('other') AND c.callee_id = sqlc.arg('me')))
  AND c.status NOT IN ('ringing', 'answered')
  AND (sqlc.narg('after')::timestamptz IS NULL OR c.started_at >= sqlc.narg('after'))
  AND (sqlc.narg('before')::timestamptz IS NULL OR c.started_at < sqlc.narg('before'))
ORDER BY c.started_at DESC
LIMIT sqlc.arg('lim');

-- ── Attachments ────────────────────────────────────────────

-- name: AddChatAttachment :one
INSERT INTO chat_attachments (message_id, group_message_id, uploaded_by, name, size_bytes, mime_type, storage_key)
VALUES (sqlc.narg('message_id'), sqlc.narg('group_message_id'), sqlc.arg('uploaded_by'),
        sqlc.arg('name'), sqlc.arg('size_bytes'), sqlc.arg('mime_type'), sqlc.arg('storage_key'))
RETURNING *;

-- name: AttachmentsOfMessages :many
SELECT * FROM chat_attachments WHERE message_id = ANY (sqlc.arg('ids')::uuid[]) ORDER BY created_at, name;

-- name: AttachmentsOfGroupMessages :many
SELECT * FROM chat_attachments WHERE group_message_id = ANY (sqlc.arg('ids')::uuid[]) ORDER BY created_at, name;

-- name: ChatAttachmentForViewer :one
-- An attachment, only if this person is in its conversation: either end of the
-- one-to-one thread, or a current member of the group.
SELECT a.* FROM chat_attachments a
LEFT JOIN messages m ON m.id = a.message_id
LEFT JOIN group_messages gm ON gm.id = a.group_message_id
WHERE a.id = sqlc.arg('id')
  AND (m.sender_id = sqlc.arg('viewer') OR m.recipient_id = sqlc.arg('viewer')
       OR EXISTS (SELECT 1 FROM chat_group_members gmm
                  WHERE gmm.group_id = gm.group_id AND gmm.user_id = sqlc.arg('viewer')));
