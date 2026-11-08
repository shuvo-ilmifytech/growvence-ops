-- +goose Up
-- +goose StatementBegin
-- Knowledge base / SOP wiki (2026-11-01): internal articles for processes, SOPs and training material.
CREATE TABLE kb_articles (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    title TEXT NOT NULL,
    slug TEXT UNIQUE NOT NULL,
    content TEXT NOT NULL,
    category TEXT NOT NULL DEFAULT 'general',
    created_by UUID REFERENCES users(id) ON DELETE SET NULL,
    updated_by UUID REFERENCES users(id) ON DELETE SET NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    is_published BOOLEAN NOT NULL DEFAULT true
);
CREATE INDEX idx_kb_articles_category ON kb_articles(category);
CREATE INDEX idx_kb_articles_slug ON kb_articles(slug);
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP TABLE IF EXISTS kb_articles;
-- +goose StatementEnd
