-- +goose Up
-- +goose StatementBegin
-- Fiverr review tracker (2026-11-01): track client reviews/ratings per Fiverr order,
-- linked to the project and the member who did the work.
CREATE TABLE IF NOT EXISTS fiverr_reviews (
    id            uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    order_id      text NOT NULL,
    project_id    uuid REFERENCES projects(id) ON DELETE SET NULL,
    member_id     uuid REFERENCES users(id) ON DELETE SET NULL,
    rating        int NOT NULL CHECK (rating BETWEEN 1 AND 5),
    review_text   text NOT NULL DEFAULT '',
    reviewer_name text NOT NULL DEFAULT '',
    reviewed_at   date NOT NULL DEFAULT CURRENT_DATE,
    created_at    timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX IF NOT EXISTS fiverr_reviews_member_idx ON fiverr_reviews (member_id);
CREATE INDEX IF NOT EXISTS fiverr_reviews_project_idx ON fiverr_reviews (project_id);
CREATE INDEX IF NOT EXISTS fiverr_reviews_reviewed_at_idx ON fiverr_reviews (reviewed_at DESC);
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP TABLE fiverr_reviews;
-- +goose StatementEnd
