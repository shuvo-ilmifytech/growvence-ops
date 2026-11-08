-- name: ListKbArticles :many
SELECT a.*, u1.name AS author_name, u2.name AS editor_name
FROM kb_articles a
LEFT JOIN users u1 ON u1.id = a.created_by
LEFT JOIN users u2 ON u2.id = a.updated_by
WHERE (sqlc.arg('category')::text = '' OR a.category = sqlc.arg('category'))
  AND (sqlc.arg('search')::text = '' OR a.title ILIKE '%' || sqlc.arg('search') || '%' OR a.content ILIKE '%' || sqlc.arg('search') || '%')
  AND (NOT sqlc.arg('published_only')::boolean OR a.is_published)
ORDER BY a.updated_at DESC
LIMIT sqlc.arg('lim') OFFSET sqlc.arg('off');

-- name: GetKbArticleBySlug :one
SELECT a.*, u1.name AS author_name, u2.name AS editor_name
FROM kb_articles a
LEFT JOIN users u1 ON u1.id = a.created_by
LEFT JOIN users u2 ON u2.id = a.updated_by
WHERE a.slug = $1;

-- name: GetKbArticle :one
SELECT * FROM kb_articles WHERE id = $1;

-- name: CreateKbArticle :one
INSERT INTO kb_articles (title, slug, content, category, created_by, updated_by)
VALUES ($1, $2, $3, $4, $5, $5)
RETURNING *;

-- name: UpdateKbArticle :one
UPDATE kb_articles
SET title = $2, slug = $3, content = $4, category = $5, is_published = $6,
    updated_by = $7, updated_at = now()
WHERE id = $1
RETURNING *;

-- name: DeleteKbArticle :exec
DELETE FROM kb_articles WHERE id = $1;

-- name: KbArticleSlugExists :one
SELECT EXISTS(SELECT 1 FROM kb_articles WHERE slug = $1 AND id != $2);

-- name: ListKbCategories :many
SELECT DISTINCT category FROM kb_articles ORDER BY category;
