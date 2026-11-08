-- name: ListTemplates :many
SELECT t.*, u.name AS creator_name
FROM project_templates t
LEFT JOIN users u ON u.id = t.created_by
WHERE (sqlc.arg('category')::text = '' OR t.category = sqlc.arg('category'))
ORDER BY t.created_at DESC;

-- name: GetTemplate :one
SELECT * FROM project_templates WHERE id = $1;

-- name: CreateTemplate :one
INSERT INTO project_templates (name, description, category, tasks, created_by)
VALUES ($1, $2, $3, $4, $5)
RETURNING *;

-- name: UpdateTemplate :one
UPDATE project_templates
SET name = $2, description = $3, category = $4, tasks = $5, updated_at = now()
WHERE id = $1
RETURNING *;

-- name: DeleteTemplate :exec
DELETE FROM project_templates WHERE id = $1;

-- name: ListTemplateCategories :many
SELECT DISTINCT category FROM project_templates ORDER BY category;
