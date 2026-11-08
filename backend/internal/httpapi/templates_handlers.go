package httpapi

import (
	"encoding/json"

	"github.com/gofiber/fiber/v3"

	"github.com/solvinx/opsflow/internal/db"
	"github.com/solvinx/opsflow/internal/policy"
	"github.com/solvinx/opsflow/internal/projects"
)

// canEditTemplates: team leads, HR, the CEO and system admins manage templates.
// Everyone else can view and apply them.
func canEditTemplates(u db.User) bool {
	r := roleOf(u)
	return r == policy.TL || r == policy.HR || r == policy.CEO || u.IsAdmin
}

// listTemplates returns every template for the requested type. Internal-only
// (P0-8): client, investor and office_admin callers are blocked.
func (s *Server) listTemplates(c fiber.Ctx) error {
	if !canViewInternal(currentUser(c)) {
		return errForbidden()
	}
	rows, err := s.store.ListTemplates(c.Context(), c.Query("category", ""))
	if err != nil {
		return err
	}
	cats, err := s.store.ListTemplateCategories(c.Context())
	if err != nil {
		return err
	}
	return c.JSON(fiber.Map{"templates": rows, "categories": cats})
}

// getTemplate returns one template by id. Internal-only (P0-8).
func (s *Server) getTemplate(c fiber.Ctx) error {
	if !canViewInternal(currentUser(c)) {
		return errForbidden()
	}
	id, err := paramUUID(c, "id")
	if err != nil {
		return err
	}
	t, err := s.store.GetTemplate(c.Context(), id)
	if err != nil {
		return errNotFound("Template not found")
	}
	return c.JSON(t)
}

type templateTaskInput struct {
	Title         string `json:"title"`
	Description   string `json:"description"`
	EstimatedDays int    `json:"estimated_days"`
}

type templateInput struct {
	Name        string              `json:"name"`
	Description string              `json:"description"`
	Category    string              `json:"category"`
	Tasks       []templateTaskInput `json:"tasks"`
}

func (s *Server) createTemplate(c fiber.Ctx) error {
	u := currentUser(c)
	if !canEditTemplates(u) {
		return errForbidden()
	}
	var in templateInput
	if err := decodeJSON(c, &in); err != nil {
		return errBadRequest("Invalid request body")
	}
	if in.Name == "" {
		return errBadRequest("Name is required")
	}
	tasksJSON, _ := json.Marshal(in.Tasks)
	t, err := s.store.CreateTemplate(c.Context(), db.CreateTemplateParams{
		Name: in.Name, Description: in.Description, Category: in.Category,
		Tasks: tasksJSON, CreatedBy: &u.ID,
	})
	if err != nil {
		return err
	}
	return c.Status(fiber.StatusCreated).JSON(t)
}

func (s *Server) updateTemplate(c fiber.Ctx) error {
	u := currentUser(c)
	if !canEditTemplates(u) {
		return errForbidden()
	}
	id, err := paramUUID(c, "id")
	if err != nil {
		return err
	}
	var in templateInput
	if err := decodeJSON(c, &in); err != nil {
		return errBadRequest("Invalid request body")
	}
	if in.Name == "" {
		return errBadRequest("Name is required")
	}
	tasksJSON, _ := json.Marshal(in.Tasks)
	t, err := s.store.UpdateTemplate(c.Context(), db.UpdateTemplateParams{
		ID: id, Name: in.Name, Description: in.Description,
		Category: in.Category, Tasks: tasksJSON,
	})
	if err != nil {
		return err
	}
	return c.JSON(t)
}

func (s *Server) deleteTemplate(c fiber.Ctx) error {
	u := currentUser(c)
	if !canEditTemplates(u) {
		return errForbidden()
	}
	id, err := paramUUID(c, "id")
	if err != nil {
		return err
	}
	if err := s.store.DeleteTemplate(c.Context(), id); err != nil {
		return err
	}
	return c.SendStatus(fiber.StatusNoContent)
}

// applyTemplateInput: the full project-creation payload (same shape as POST
// /projects) plus the template to apply. The project is created first, then a
// board card is created for each task in the template.
type applyTemplateInput struct {
	Project map[string]json.RawMessage `json:"project"`
}

func (s *Server) applyTemplate(c fiber.Ctx) error {
	u := currentUser(c)
	tid, err := paramUUID(c, "id")
	if err != nil {
		return err
	}
	t, err := s.store.GetTemplate(c.Context(), tid)
	if err != nil {
		return errNotFound("Template not found")
	}
	var in applyTemplateInput
	if err := decodeJSON(c, &in); err != nil {
		return errBadRequest("Invalid request body")
	}
	if len(in.Project) == 0 {
		return errBadRequest("Project details are required")
	}
	// Create the project via the normal service (permission + validation apply).
	projectID, err := s.projects.Create(c.Context(), u, in.Project)
	if err != nil {
		return err
	}
	// Create a board card per template task.
	var tasks []templateTaskInput
	_ = json.Unmarshal(t.Tasks, &tasks)
	cardsCreated := 0
	for _, task := range tasks {
		if task.Title == "" {
			continue
		}
		title, desc := task.Title, task.Description
		_, err := s.projects.CreateCard(c.Context(), u, projectID, projects.CardInput{
			Title: &title, Description: &desc,
		})
		if err != nil {
			// Cards are best-effort: the project already exists.
			continue
		}
		cardsCreated++
	}
	return c.Status(fiber.StatusCreated).JSON(fiber.Map{
		"project_id":    projectID,
		"cards_created": cardsCreated,
	})
}

