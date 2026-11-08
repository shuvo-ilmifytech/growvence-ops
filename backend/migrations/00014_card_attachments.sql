-- +goose Up
-- +goose StatementBegin
-- Attachments on board cards (e.g. the files a client sent with a request). They live in
-- files like project files and update attachments, tagged with the card.
ALTER TABLE files ADD COLUMN card_id uuid REFERENCES project_cards (id);
CREATE INDEX files_card_idx ON files (card_id) WHERE card_id IS NOT NULL;

CREATE OR REPLACE VIEW card_details AS
SELECT c.id, c.project_id, c.number, c.title, c.description, c.source, c.column_key, c.position,
       c.due_date::text AS due_date, c.created_by, cu.name AS created_by_name, c.created_at, c.updated_at, c.done_at,
       coalesce((SELECT array_agg(a.user_id ORDER BY au.name) FROM card_assignees a JOIN users au ON au.id = a.user_id
                 WHERE a.card_id = c.id), '{}')::uuid[] AS assignee_ids,
       coalesce((SELECT array_agg(au.name ORDER BY au.name) FROM card_assignees a JOIN users au ON au.id = a.user_id
                 WHERE a.card_id = c.id), '{}')::text[] AS assignee_names,
       (SELECT count(*) FROM card_comments cc WHERE cc.card_id = c.id) AS comment_count,
       (SELECT count(*) FROM files f WHERE f.card_id = c.id AND f.removed_at IS NULL) AS attachment_count
FROM project_cards c
JOIN users cu ON cu.id = c.created_by
WHERE c.removed_at IS NULL;
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP VIEW card_details;
CREATE VIEW card_details AS
SELECT c.id, c.project_id, c.number, c.title, c.description, c.source, c.column_key, c.position,
       c.due_date::text AS due_date, c.created_by, cu.name AS created_by_name, c.created_at, c.updated_at, c.done_at,
       coalesce((SELECT array_agg(a.user_id ORDER BY au.name) FROM card_assignees a JOIN users au ON au.id = a.user_id
                 WHERE a.card_id = c.id), '{}')::uuid[] AS assignee_ids,
       coalesce((SELECT array_agg(au.name ORDER BY au.name) FROM card_assignees a JOIN users au ON au.id = a.user_id
                 WHERE a.card_id = c.id), '{}')::text[] AS assignee_names,
       (SELECT count(*) FROM card_comments cc WHERE cc.card_id = c.id) AS comment_count
FROM project_cards c
JOIN users cu ON cu.id = c.created_by
WHERE c.removed_at IS NULL;
ALTER TABLE files DROP COLUMN card_id;
-- +goose StatementEnd
