-- name: GetProjectDetail :one
SELECT * FROM project_details WHERE id = $1;

-- name: ListProjectDetails :many
-- Scope params restrict by role; filter params come from the UI.
SELECT sqlc.embed(v), count(*) OVER () AS total,
  -- "New updates" marker: someone other than the viewer acted since they last opened it.
  EXISTS (
    SELECT 1 FROM activity_log al
    WHERE al.project_id = v.id AND al.actor_id <> sqlc.arg('viewer')::uuid
      AND al.created_at > coalesce(
        (SELECT pv.last_viewed_at FROM project_views pv WHERE pv.project_id = v.id AND pv.user_id = sqlc.arg('viewer')::uuid),
        '-infinity'::timestamptz)
  ) AS has_updates
FROM project_details v
WHERE (sqlc.narg('scope_creator')::uuid IS NULL OR v.created_by = sqlc.narg('scope_creator'))
  -- Sales: every project, but only their own drafts.
  AND (sqlc.narg('scope_sales')::uuid IS NULL OR v.created_by = sqlc.narg('scope_sales') OR v.status <> 'draft')
  AND (sqlc.narg('scope_assignee')::uuid IS NULL OR sqlc.narg('scope_assignee')::uuid = ANY (v.member_ids))
  AND (NOT sqlc.arg('hide_drafts')::boolean OR v.status <> 'draft')
  -- Team Leaders see their own team's projects plus those with no team or no active lead.
  AND (sqlc.narg('scope_lead')::uuid IS NULL OR v.team_id IS NULL OR v.team_lead_id IS NULL
       OR v.team_lead_id = sqlc.narg('scope_lead') OR v.team_head_id = sqlc.narg('scope_lead'))
  AND (sqlc.narg('team')::uuid IS NULL OR v.team_id = sqlc.narg('team'))
  AND (sqlc.narg('statuses')::text[] IS NULL OR v.status = ANY (sqlc.narg('statuses')::text[]))
  AND (sqlc.narg('assignee')::uuid IS NULL OR sqlc.narg('assignee')::uuid = ANY (v.member_ids))
  AND (sqlc.narg('priority')::text IS NULL OR v.priority = sqlc.narg('priority'))
  AND (NOT sqlc.arg('overdue')::boolean OR (
        v.effective_due::timestamptz < now()
        AND v.status NOT IN ('delivered', 'completed', 'cancelled')))
  AND (sqlc.narg('q')::text IS NULL
       OR v.name ILIKE '%' || sqlc.narg('q') || '%'
       OR v.client_name ILIKE '%' || sqlc.narg('q') || '%'
       OR v.order_id ILIKE '%' || sqlc.narg('q') || '%'
       OR v.number::text = sqlc.narg('q'))
ORDER BY
  CASE WHEN sqlc.arg('sort')::text = 'due' THEN v.effective_due END ASC NULLS LAST,
  CASE WHEN sqlc.arg('sort')::text = 'priority' THEN
    CASE v.priority WHEN 'urgent' THEN 0 WHEN 'high' THEN 1 WHEN 'medium' THEN 2 ELSE 3 END END ASC,
  CASE WHEN sqlc.arg('sort')::text = 'created' THEN v.created_at END DESC,
  v.last_activity_at DESC
LIMIT sqlc.arg('lim') OFFSET sqlc.arg('off');

-- name: CountProjectsByStatus :many
SELECT v.status, count(*) AS n
FROM project_details v
WHERE (sqlc.narg('scope_creator')::uuid IS NULL OR v.created_by = sqlc.narg('scope_creator'))
  -- Sales: every project, but only their own drafts.
  AND (sqlc.narg('scope_sales')::uuid IS NULL OR v.created_by = sqlc.narg('scope_sales') OR v.status <> 'draft')
  AND (sqlc.narg('scope_assignee')::uuid IS NULL OR sqlc.narg('scope_assignee')::uuid = ANY (v.member_ids))
  AND (NOT sqlc.arg('hide_drafts')::boolean OR v.status <> 'draft')
  -- Team Leaders see their own team's projects plus those with no team or no active lead.
  AND (sqlc.narg('scope_lead')::uuid IS NULL OR v.team_id IS NULL OR v.team_lead_id IS NULL
       OR v.team_lead_id = sqlc.narg('scope_lead') OR v.team_head_id = sqlc.narg('scope_lead'))
GROUP BY v.status;

-- name: LockProject :one
SELECT * FROM projects WHERE id = $1 FOR UPDATE;

-- name: CreateProject :one
INSERT INTO projects (
    name, profile_id, client_name, order_id, client_contact, project_type_id, scope,
    agreed_price, platform_fee_percent, currency, start_date, deadline, priority, special_notes, created_by,
    first_submitted_at)
VALUES (
    sqlc.narg('name'), sqlc.narg('profile_id'), sqlc.narg('client_name'), sqlc.narg('order_id'),
    sqlc.narg('client_contact'), sqlc.narg('project_type_id'), sqlc.narg('scope'),
    sqlc.narg('agreed_price')::text::numeric,
    -- The marketplace's share comes from the profile, so nobody types it.
    coalesce((SELECT mp.fee_percent FROM marketplace_profiles mp WHERE mp.id = sqlc.narg('profile_id')), 20),
    sqlc.arg('currency'),
    sqlc.narg('start_date')::text::date, sqlc.narg('deadline')::text::timestamptz, sqlc.arg('priority'),
    sqlc.narg('special_notes'), sqlc.arg('created_by'),
    now())
RETURNING id;

-- name: UpdateProjectDetails :exec
-- Writes every editable column; the service merges the patch first.
UPDATE projects SET
    name = sqlc.narg('name'),
    profile_id = sqlc.narg('profile_id'),
    client_name = sqlc.narg('client_name'),
    order_id = sqlc.narg('order_id'),
    client_contact = sqlc.narg('client_contact'),
    project_type_id = sqlc.narg('project_type_id'),
    scope = sqlc.narg('scope'),
    agreed_price = sqlc.narg('agreed_price')::text::numeric,
    -- Moving to another profile takes that profile's share; otherwise the recorded one stays.
    platform_fee_percent = CASE WHEN profile_id IS DISTINCT FROM sqlc.narg('profile_id')
        THEN coalesce((SELECT mp.fee_percent FROM marketplace_profiles mp WHERE mp.id = sqlc.narg('profile_id')), platform_fee_percent)
        ELSE platform_fee_percent END,
    currency = sqlc.arg('currency'),
    start_date = sqlc.narg('start_date')::text::date,
    deadline = sqlc.narg('deadline')::text::timestamptz,
    overdue_alert_at = CASE WHEN deadline IS DISTINCT FROM sqlc.narg('deadline')::text::timestamptz THEN NULL ELSE overdue_alert_at END,
    priority = sqlc.arg('priority'),
    special_notes = sqlc.narg('special_notes'),
    lock_version = lock_version + 1,
    last_activity_at = now(),
    updated_at = now()
WHERE projects.id = sqlc.arg('id');

-- name: SetProjectStatus :exec
UPDATE projects SET
    status = sqlc.arg('status'),
    delivered_at = CASE WHEN sqlc.arg('status') = 'delivered' THEN now() ELSE delivered_at END,
    completed_at = CASE WHEN sqlc.arg('status') = 'completed' THEN now()
                        WHEN sqlc.arg('status') = 'in_progress' THEN NULL ELSE completed_at END,
    cancelled_at = CASE WHEN sqlc.arg('status') = 'cancelled' THEN now() ELSE cancelled_at END,
    revision_count = revision_count + sqlc.arg('revision_inc')::int,
    lock_version = lock_version + 1,
    last_activity_at = now(),
    updated_at = now()
WHERE id = sqlc.arg('id');

-- name: AssignProject :exec
UPDATE projects SET
    assigned_to = sqlc.arg('assigned_to'),
    priority = sqlc.arg('priority'),
    due_date = sqlc.narg('due_date')::text::date,
    overdue_alert_at = NULL,
    status = sqlc.arg('status'),
    first_assigned_at = coalesce(first_assigned_at, now()),
    lock_version = lock_version + 1,
    last_activity_at = now(),
    updated_at = now()
WHERE id = sqlc.arg('id');

-- name: SetProjectPlanning :exec
UPDATE projects SET
    priority = sqlc.arg('priority'),
    due_date = sqlc.narg('due_date')::text::date,
    overdue_alert_at = NULL,
    lock_version = lock_version + 1,
    last_activity_at = now(),
    updated_at = now()
WHERE id = sqlc.arg('id');

-- name: TouchProject :exec
UPDATE projects SET last_activity_at = now() WHERE id = $1;

-- name: CloseAssignment :exec
UPDATE project_assignments SET unassigned_at = now()
WHERE project_id = $1 AND unassigned_at IS NULL;

-- name: OpenAssignment :exec
INSERT INTO project_assignments (project_id, user_id, assigned_by, priority, due_date)
VALUES ($1, $2, $3, $4, sqlc.narg('due_date')::text::date);

-- name: InsertStatusHistory :exec
INSERT INTO project_status_history (project_id, from_status, to_status, changed_by, comment)
VALUES ($1, $2, $3, $4, $5);

-- name: InsertProjectChange :one
INSERT INTO project_changes (project_id, changed_by, field, old_value, new_value, is_important)
VALUES ($1, $2, $3, $4, $5, $6)
RETURNING id;

-- name: ListStatusHistory :many
SELECT h.id, h.from_status, h.to_status, h.comment, h.created_at, h.changed_by, u.name AS changed_by_name
FROM project_status_history h JOIN users u ON u.id = h.changed_by
WHERE h.project_id = $1
ORDER BY h.created_at, h.id;

-- name: ListAssignments :many
SELECT a.id, a.user_id, u.name AS user_name, a.assigned_by, b.name AS assigned_by_name,
       a.priority, a.due_date, a.assigned_at, a.unassigned_at
FROM project_assignments a
JOIN users u ON u.id = a.user_id
JOIN users b ON b.id = a.assigned_by
WHERE a.project_id = $1
ORDER BY a.assigned_at;

-- name: ListProjectChanges :many
SELECT c.id, c.field, c.old_value, c.new_value, c.is_important, c.created_at, c.changed_by, u.name AS changed_by_name
FROM project_changes c JOIN users u ON u.id = c.changed_by
WHERE c.project_id = $1
ORDER BY c.created_at, c.id;

-- name: CreateProjectComment :one
INSERT INTO project_comments (project_id, author_id, body) VALUES ($1, $2, $3)
RETURNING *;

-- name: ListProjectComments :many
SELECT c.id, c.body, c.created_at, c.edited_at, c.author_id, u.name AS author_name, u.role AS author_role
FROM project_comments c JOIN users u ON u.id = c.author_id
WHERE c.project_id = $1
ORDER BY c.created_at, c.id;

-- name: GetActiveLookup :one
SELECT
  EXISTS (SELECT 1 FROM marketplace_profiles mp WHERE mp.id = sqlc.narg('profile_id')::uuid AND mp.is_active) AS profile_ok,
  EXISTS (SELECT 1 FROM project_types pt WHERE pt.id = sqlc.narg('project_type_id')::uuid AND pt.is_active) AS type_ok;

-- name: SetProjectTeam :exec
UPDATE projects SET team_id = sqlc.narg('team_id') WHERE id = sqlc.arg('id');

-- name: MoveProjectTeam :one
-- Hands the project to another team. When none of its members came along it goes back
-- to New, unassigned, so the new team's lead assigns it (which starts it again).
UPDATE projects SET
    team_id = sqlc.arg('team_id'),
    assigned_to = (SELECT m.user_id FROM project_members m WHERE m.project_id = projects.id
                   ORDER BY m.added_at, m.user_id LIMIT 1),
    status = CASE WHEN EXISTS (SELECT 1 FROM project_members m WHERE m.project_id = projects.id)
                  THEN status ELSE 'new' END,
    -- The new team is told directly, so a moved project never gets the "waiting 24h" alert
    -- (it counts from the first submission, and would fire at once).
    unassigned_alert_at = coalesce(unassigned_alert_at, now()),
    lock_version = lock_version + 1,
    last_activity_at = now(),
    updated_at = now()
WHERE id = sqlc.arg('id')
RETURNING status;

-- name: RouteTeamForType :one
-- The team a project of this type goes to (NULL when the type has no active team).
SELECT t.id FROM project_types pt JOIN teams t ON t.id = pt.team_id AND t.is_active
WHERE pt.id = $1;

-- name: ActiveTeamLead :one
SELECT l.id FROM teams t JOIN users l ON l.id = t.lead_id AND l.is_active AND l.role = 'tl'
WHERE t.id = $1;

-- name: ActiveTeamHead :one
-- The active head of the team's department, who acts on the team like its lead.
SELECT h.id FROM teams t
JOIN departments d ON d.id = t.department_id AND d.is_active
JOIN users h ON h.id = d.head_id AND h.is_active AND h.role = 'tl'
WHERE t.id = $1;

-- name: IsTeamMember :one
SELECT EXISTS (SELECT 1 FROM team_members WHERE team_id = $1 AND user_id = $2);

-- name: ProjectLeadIDs :many
-- Active Team Leaders who can see the project: its team's lead and its department's
-- head, or every TL when the project has no team or the team has no active lead.
SELECT u.id
FROM projects p
LEFT JOIN teams t ON t.id = p.team_id
LEFT JOIN users l ON l.id = t.lead_id AND l.is_active AND l.role = 'tl'
LEFT JOIN departments d ON d.id = t.department_id AND d.is_active
LEFT JOIN users h ON h.id = d.head_id AND h.is_active AND h.role = 'tl'
JOIN users u ON u.is_active AND u.role = 'tl' AND (p.team_id IS NULL OR l.id IS NULL OR u.id = l.id OR u.id = h.id)
WHERE p.id = $1;

-- name: ProjectMemberIDs :many
SELECT user_id FROM project_members WHERE project_id = $1 ORDER BY added_at, user_id;

-- name: AddProjectMember :exec
-- clock_timestamp keeps the order the Team Leader picked within one assignment.
INSERT INTO project_members (project_id, user_id, added_by, added_at) VALUES ($1, $2, $3, clock_timestamp());

-- name: RemoveProjectMember :exec
DELETE FROM project_members WHERE project_id = $1 AND user_id = $2;

-- name: SetPrimaryAssignee :exec
-- The first remaining member; NULL when nobody is left.
UPDATE projects SET assigned_to = (
    SELECT m.user_id FROM project_members m WHERE m.project_id = projects.id ORDER BY m.added_at, m.user_id LIMIT 1),
  lock_version = lock_version + 1, last_activity_at = now(), updated_at = now()
WHERE id = $1;

-- name: CloseMemberAssignment :exec
UPDATE project_assignments SET unassigned_at = now()
WHERE project_id = $1 AND user_id = $2 AND unassigned_at IS NULL;

-- name: UnassignMemberCards :exec
-- A member taken off a project is also taken off its cards.
DELETE FROM card_assignees ca USING project_cards c
WHERE ca.card_id = c.id AND c.project_id = $1 AND ca.user_id = $2;

-- name: SetStoreAccess :exec
UPDATE projects SET
    store_access = sqlc.arg('store_access'),
    store_access_link = sqlc.narg('store_access_link'),
    store_access_changed_at = now(),
    lock_version = lock_version + 1,
    last_activity_at = now(),
    updated_at = now()
WHERE projects.id = sqlc.arg('id');
