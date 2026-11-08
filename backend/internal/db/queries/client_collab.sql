-- Client collaboration queries (Phase 4).

-- name: ListClients :many
-- Every client with how many projects they are linked to.
SELECT c.id, c.client_code, c.company_name, c.contact_person, c.email, c.phone,
       c.address, c.type, c.status, c.created_by, c.created_at, c.updated_at,
       count(pc.project_id)::int AS project_count
FROM clients c
LEFT JOIN project_clients pc ON pc.client_id = c.id
GROUP BY c.id
ORDER BY c.company_name ASC;

-- name: GetClient :one
SELECT c.id, c.client_code, c.company_name, c.contact_person, c.email, c.phone,
       c.address, c.type, c.status, c.created_by, c.created_at, c.updated_at,
       count(pc.project_id)::int AS project_count
FROM clients c
LEFT JOIN project_clients pc ON pc.client_id = c.id
WHERE c.id = $1
GROUP BY c.id;

-- name: CreateClient :one
-- The code comes from the sequence: CL-0001, CL-0002, …
INSERT INTO clients (client_code, company_name, contact_person, email, phone, address, type, created_by)
VALUES ('CL-' || lpad(nextval('client_code_seq')::text, 4, '0'),
        sqlc.arg('company_name'), sqlc.arg('contact_person'), sqlc.arg('email'),
        sqlc.arg('phone'), sqlc.arg('address'), sqlc.arg('type'), sqlc.arg('created_by'))
RETURNING id, client_code;

-- name: UpdateClient :exec
UPDATE clients
SET company_name = sqlc.arg('company_name'), contact_person = sqlc.arg('contact_person'),
    email = sqlc.arg('email'), phone = sqlc.arg('phone'), address = sqlc.arg('address'),
    type = sqlc.arg('type'), status = sqlc.arg('status'), updated_at = now()
WHERE id = sqlc.arg('id');

-- name: ListClientContacts :many
SELECT id, client_id, name, email, phone, role, is_primary, created_at
FROM client_contacts
WHERE client_id = $1
ORDER BY is_primary DESC, name ASC;

-- name: AddClientContact :one
INSERT INTO client_contacts (client_id, name, email, phone, role, is_primary)
VALUES ($1, $2, $3, $4, $5, $6)
RETURNING id;

-- name: DeleteClientContact :exec
DELETE FROM client_contacts WHERE id = $1 AND client_id = $2;

-- name: ListProjectClients :many
SELECT pc.id, pc.project_id, pc.client_id, pc.created_at,
       c.client_code, c.company_name, c.contact_person, c.email, c.type, c.status
FROM project_clients pc
JOIN clients c ON c.id = pc.client_id
WHERE pc.project_id = $1
ORDER BY c.company_name ASC;

-- name: ListClientProjects :many
SELECT pc.id, pc.project_id, pc.client_id, pc.created_at,
       p.number, p.name, p.status
FROM project_clients pc
JOIN projects p ON p.id = pc.project_id
WHERE pc.client_id = $1
ORDER BY p.updated_at DESC;

-- name: LinkClientToProject :one
INSERT INTO project_clients (project_id, client_id, created_by)
VALUES ($1, $2, $3)
ON CONFLICT (project_id, client_id) DO NOTHING
RETURNING id;

-- name: UnlinkClientFromProject :exec
DELETE FROM project_clients WHERE project_id = $1 AND client_id = $2;

-- name: ProjectHasClient :one
SELECT count(*) FROM project_clients WHERE project_id = $1 AND client_id = $2;

-- name: ListTaskComments :many
-- Team view: every comment, internal ones included.
SELECT tc.id, tc.card_id, tc.author_id, tc.body, tc.is_internal, tc.created_at, tc.updated_at,
       u.name AS author_name, u.role AS author_role
FROM task_comments tc
LEFT JOIN users u ON u.id = tc.author_id
WHERE tc.card_id = $1
ORDER BY tc.created_at ASC;

-- name: ListTaskCommentsForClient :many
-- Client view: internal-only comments never leave the team.
SELECT tc.id, tc.card_id, tc.author_id, tc.body, tc.is_internal, tc.created_at, tc.updated_at,
       u.name AS author_name, u.role AS author_role
FROM task_comments tc
LEFT JOIN users u ON u.id = tc.author_id
WHERE tc.card_id = $1 AND tc.is_internal = false
ORDER BY tc.created_at ASC;

-- name: AddTaskComment :one
INSERT INTO task_comments (card_id, author_id, body, is_internal)
VALUES ($1, $2, $3, $4)
RETURNING id, created_at;

-- name: SetCardVisibleToClient :exec
UPDATE project_cards SET visible_to_client = $2, updated_at = now() WHERE id = $1;

-- name: GetPendingApproval :one
SELECT id, card_id, requested_by, requested_at, status, reviewed_by, reviewed_at, review_note
FROM task_approvals
WHERE card_id = $1 AND status = 'pending';

-- name: ListTaskApprovals :many
SELECT a.id, a.card_id, a.requested_by, a.requested_at, a.status, a.reviewed_by, a.reviewed_at, a.review_note,
       u.name AS requested_by_name, r.name AS reviewed_by_name
FROM task_approvals a
LEFT JOIN users u ON u.id = a.requested_by
LEFT JOIN users r ON r.id = a.reviewed_by
WHERE a.card_id = $1
ORDER BY a.requested_at DESC;

-- name: RequestTaskApproval :one
INSERT INTO task_approvals (card_id, requested_by)
VALUES ($1, $2)
RETURNING id;

-- name: ReviewTaskApproval :exec
-- Only a pending approval can be reviewed; the partial unique index keeps
-- one pending row per card, so this is the only open one.
UPDATE task_approvals
SET status = $2, reviewed_by = $3, reviewed_at = now(), review_note = $4
WHERE id = $1 AND status = 'pending';

-- name: ListClientMessages :many
SELECT m.id, m.project_id, m.author_id, m.body, m.created_at,
       u.name AS author_name, u.role AS author_role
FROM client_messages m
LEFT JOIN users u ON u.id = m.author_id
WHERE m.project_id = $1
ORDER BY m.created_at ASC;

-- name: AddClientMessage :one
INSERT INTO client_messages (project_id, author_id, body)
VALUES ($1, $2, $3)
RETURNING id, created_at;

-- name: ListClientFiles :many
SELECT f.id, f.project_id, f.client_id, f.uploaded_by, f.file_name, f.file_url,
       f.file_size, f.is_client_upload, f.uploaded_at,
       u.name AS uploaded_by_name, c.client_code
FROM client_files f
LEFT JOIN users u ON u.id = f.uploaded_by
LEFT JOIN clients c ON c.id = f.client_id
WHERE f.project_id = $1
ORDER BY f.uploaded_at DESC;

-- name: AddClientFile :one
INSERT INTO client_files (project_id, client_id, uploaded_by, file_name, file_url, file_size, is_client_upload)
VALUES ($1, $2, $3, $4, $5, $6, $7)
RETURNING id;

-- name: ListVisibleCardsForClient :many
-- The client portal's task list: only cards the team opted in, on a project
-- the client may open. Approval state rides along so the portal can offer
-- Approve / Request changes exactly when one is pending.
SELECT cd.*, a.status AS approval_status, a.requested_at AS approval_requested_at
FROM card_details cd
LEFT JOIN task_approvals a ON a.card_id = cd.id AND a.status = 'pending'
WHERE cd.container_id = $1 AND cd.visible_to_client = true
ORDER BY CASE cd.column_key WHEN 'todo' THEN 0 WHEN 'in_progress' THEN 1 WHEN 'review' THEN 2 ELSE 3 END,
         cd.position, cd.number;
