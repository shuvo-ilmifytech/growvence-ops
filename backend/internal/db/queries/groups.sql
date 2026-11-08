-- Group chat: named conversations with a roster.

-- name: CreateChatGroup :one
INSERT INTO chat_groups (name, created_by) VALUES (btrim(sqlc.arg('name')), sqlc.arg('created_by'))
RETURNING *;

-- name: RenameChatGroup :one
UPDATE chat_groups SET name = btrim(sqlc.arg('name')) WHERE id = sqlc.arg('id')
RETURNING *;

-- name: DeleteChatGroup :exec
DELETE FROM chat_groups WHERE id = $1;

-- name: GetChatGroup :one
SELECT g.*, (SELECT count(*) FROM chat_group_members m WHERE m.group_id = g.id)::bigint AS member_count
FROM chat_groups g WHERE g.id = $1;

-- name: AddChatGroupMember :exec
-- Re-adding someone already in the group leaves their read mark alone.
INSERT INTO chat_group_members (group_id, user_id) VALUES (sqlc.arg('group_id'), sqlc.arg('user_id'))
ON CONFLICT (group_id, user_id) DO NOTHING;

-- name: RemoveChatGroupMember :execrows
DELETE FROM chat_group_members WHERE group_id = sqlc.arg('group_id') AND user_id = sqlc.arg('user_id');

-- name: IsChatGroupMember :one
SELECT EXISTS (
    SELECT 1 FROM chat_group_members WHERE group_id = sqlc.arg('group_id') AND user_id = sqlc.arg('user_id')
) AS is_member;

-- name: ChatGroupMembers :many
SELECT u.id, u.name, u.role, u.is_active, m.added_at
FROM chat_group_members m JOIN users u ON u.id = m.user_id
WHERE m.group_id = $1
ORDER BY u.name;

-- name: ChatGroupMemberIDs :many
SELECT user_id FROM chat_group_members WHERE group_id = $1;

-- name: MyChatGroups :many
-- The groups this person is in, most recently spoken in first, with what's waiting.
WITH newest AS (
    SELECT DISTINCT ON (gm.group_id)
           gm.group_id, gm.body, gm.created_at, s.name AS sender_name,
           (gm.sender_id = sqlc.arg('me')) AS from_me
    FROM group_messages gm JOIN users s ON s.id = gm.sender_id
    ORDER BY gm.group_id, gm.created_at DESC
)
SELECT g.id, g.name, g.created_by, g.created_at,
       (SELECT count(*) FROM chat_group_members m2 WHERE m2.group_id = g.id)::bigint AS member_count,
       newest.body AS last_body,
       newest.created_at AS last_at,
       newest.sender_name AS last_sender_name,
       newest.from_me AS last_from_me,
       (SELECT count(*) FROM group_messages gm
         WHERE gm.group_id = g.id AND gm.sender_id <> sqlc.arg('me') AND gm.created_at > m.last_read_at)::bigint AS unread
FROM chat_group_members m
JOIN chat_groups g ON g.id = m.group_id
LEFT JOIN newest ON newest.group_id = g.id
WHERE m.user_id = sqlc.arg('me')
ORDER BY newest.created_at DESC NULLS LAST, g.created_at DESC;

-- name: SendGroupMessage :one
INSERT INTO group_messages (group_id, sender_id, body)
VALUES (sqlc.arg('group_id'), sqlc.arg('sender_id'), btrim(sqlc.arg('body')))
RETURNING *;

-- name: GroupThread :many
-- Newest first; `before` pages backwards.
SELECT gm.*, s.name AS sender_name
FROM group_messages gm JOIN users s ON s.id = gm.sender_id
WHERE gm.group_id = sqlc.arg('group_id')
  AND (sqlc.narg('before')::timestamptz IS NULL OR gm.created_at < sqlc.narg('before'))
ORDER BY gm.created_at DESC
LIMIT sqlc.arg('lim');

-- name: MarkChatGroupRead :exec
UPDATE chat_group_members SET last_read_at = now()
WHERE group_id = sqlc.arg('group_id') AND user_id = sqlc.arg('user_id');

-- name: CountUnreadGroupMessages :one
SELECT coalesce(sum(
    (SELECT count(*) FROM group_messages gm
      WHERE gm.group_id = m.group_id AND gm.sender_id <> m.user_id AND gm.created_at > m.last_read_at)
), 0)::bigint AS unread
FROM chat_group_members m WHERE m.user_id = $1;
