-- +goose Up
-- Each team belongs to one department (e.g. Shopify and Web Dev in Operations).
ALTER TABLE teams ADD COLUMN department_id uuid REFERENCES departments (id);
CREATE INDEX teams_department_idx ON teams (department_id);

-- +goose Down
ALTER TABLE teams DROP COLUMN department_id;
