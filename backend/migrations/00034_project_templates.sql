-- +goose Up
-- +goose StatementBegin
-- Project templates (2026-11-01): reusable project blueprints with task lists.
-- Applying a template creates a project plus board cards for each task.
CREATE TABLE project_templates (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    name TEXT NOT NULL,
    description TEXT NOT NULL DEFAULT '',
    category TEXT NOT NULL DEFAULT 'general',
    tasks JSONB NOT NULL DEFAULT '[]'::jsonb,
    created_by UUID REFERENCES users(id) ON DELETE SET NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX idx_project_templates_category ON project_templates(category);

-- Seed default templates. tasks = [{title, description, estimated_days}]
INSERT INTO project_templates (name, description, category, tasks) VALUES
('Shopify Store', 'Complete Shopify store build for a Fiverr client.', 'shopify',
 '[
   {"title": "Store setup & settings", "description": "Create store, configure currencies, taxes, shipping zones.", "estimated_days": 1},
   {"title": "Theme selection & customization", "description": "Pick theme, customize homepage, header, footer to brand.", "estimated_days": 3},
   {"title": "Product import & collections", "description": "Import products, write descriptions, organize collections.", "estimated_days": 2},
   {"title": "Payment gateway & checkout", "description": "Connect payments, test checkout flow end to end.", "estimated_days": 1},
   {"title": "Testing & launch", "description": "Full QA pass, speed check, handover to client.", "estimated_days": 2}
  ]'::jsonb),
('Website Redesign', 'Full website redesign project workflow.', 'web',
 '[
   {"title": "Audit current site", "description": "Review existing site, note pain points and goals.", "estimated_days": 1},
   {"title": "Wireframes", "description": "Low-fidelity wireframes for key pages.", "estimated_days": 2},
   {"title": "Visual design", "description": "High-fidelity designs matching brand guidelines.", "estimated_days": 4},
   {"title": "Development", "description": "Build the site from approved designs.", "estimated_days": 5},
   {"title": "QA & launch", "description": "Cross-browser testing, performance check, go live.", "estimated_days": 2}
  ]'::jsonb),
('Brand Identity', 'Logo and brand identity package.', 'branding',
 '[
   {"title": "Research & discovery", "description": "Client questionnaire, competitor and market research.", "estimated_days": 2},
   {"title": "Concept directions", "description": "3 distinct creative concept directions.", "estimated_days": 3},
   {"title": "Logo refinement", "description": "Refine chosen direction into final logo.", "estimated_days": 3},
   {"title": "Brand guidelines", "description": "Colors, typography, usage rules document.", "estimated_days": 2},
   {"title": "Final delivery", "description": "Export all formats, package and hand over.", "estimated_days": 1}
  ]'::jsonb);
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP TABLE IF EXISTS project_templates;
-- +goose StatementEnd
