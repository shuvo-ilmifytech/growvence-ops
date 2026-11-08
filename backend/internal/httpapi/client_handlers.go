package httpapi

import (
	"strings"

	"github.com/gofiber/fiber/v3"
	"github.com/google/uuid"

	"github.com/solvinx/opsflow/internal/policy"
)

// requireClient gates the client portal: only users with role 'client' may
// use these endpoints. Clients see a limited view of their granted projects.
func requireClient(c fiber.Ctx) error {
	u := currentUser(c)
	if roleOf(u) == policy.Client {
		return c.Next()
	}
	return errForbidden()
}

// clientHasAccess reports whether the client user may see the project.
func (s *Server) clientHasAccess(ctx fiber.Ctx, clientUserID, projectID uuid.UUID) bool {
	var ok bool
	err := s.store.Pool.QueryRow(ctx.Context(), `
		SELECT EXISTS(SELECT 1 FROM client_project_access
		              WHERE client_user_id = $1 AND project_id = $2)`,
		clientUserID, projectID).Scan(&ok)
	return err == nil && ok
}

type clientProjectOut struct {
	ID        uuid.UUID `json:"id"`
	Number    int64     `json:"number"`
	Name      string    `json:"name"`
	Status    string    `json:"status"`
	Priority  string    `json:"priority"`
	DueDate   *string   `json:"due_date"`
	UpdatedAt string    `json:"updated_at"`
}

// clientProjects lists the projects the logged-in client has access to.
// Limited fields only: no financials, no team info, no internal notes.
func (s *Server) clientProjects(c fiber.Ctx) error {
	u := currentUser(c)
	rows, err := s.store.Pool.Query(c.Context(), `
		SELECT p.id, p.number, p.name, p.status, p.priority,
		       p.due_date::text, p.updated_at::text
		FROM projects p
		JOIN client_project_access a ON a.project_id = p.id
		WHERE a.client_user_id = $1
		  AND p.status NOT IN ('draft', 'cancelled')
		ORDER BY p.updated_at DESC`, u.ID)
	if err != nil {
		return err
	}
	defer rows.Close()

	out := []clientProjectOut{}
	for rows.Next() {
		var pr clientProjectOut
		var due *string
		if err := rows.Scan(&pr.ID, &pr.Number, &pr.Name, &pr.Status, &pr.Priority, &due, &pr.UpdatedAt); err != nil {
			return err
		}
		pr.DueDate = due
		out = append(out, pr)
	}
	if err := rows.Err(); err != nil {
		return err
	}
	return c.JSON(fiber.Map{"projects": out})
}

type clientProjectDetailOut struct {
	ID       uuid.UUID `json:"id"`
	Number   int64     `json:"number"`
	Name     string    `json:"name"`
	Status   string    `json:"status"`
	Priority string    `json:"priority"`
	Scope    string    `json:"scope"`
	DueDate  *string   `json:"due_date"`
}

// clientProject returns a single project's client-safe details.
func (s *Server) clientProject(c fiber.Ctx) error {
	u := currentUser(c)
	pid, err := uuid.Parse(c.Params("id"))
	if err != nil {
		return errBadRequest("Invalid project id")
	}
	if !s.clientHasAccess(c, u.ID, pid) {
		return errForbidden()
	}
	var d clientProjectDetailOut
	var due *string
	var scope *string
	err = s.store.Pool.QueryRow(c.Context(), `
		SELECT id, number, name, status, priority, scope, due_date::text
		FROM projects WHERE id = $1`, pid).
		Scan(&d.ID, &d.Number, &d.Name, &d.Status, &d.Priority, &scope, &due)
	if err != nil {
		return errNotFound("Project not found")
	}
	if scope != nil {
		d.Scope = *scope
	}
	d.DueDate = due
	return c.JSON(d)
}

type clientUpdateOut struct {
	ID        uuid.UUID `json:"id"`
	Number    int64     `json:"number"`
	Message   string    `json:"message"`
	SentAt    string    `json:"sent_at"`
	CreatedAt string    `json:"created_at"`
}

// clientProjectUpdates returns the SENT updates for a client project —
// these are the official communications the team pushed to the client.
func (s *Server) clientProjectUpdates(c fiber.Ctx) error {
	u := currentUser(c)
	pid, err := uuid.Parse(c.Params("id"))
	if err != nil {
		return errBadRequest("Invalid project id")
	}
	if !s.clientHasAccess(c, u.ID, pid) {
		return errForbidden()
	}
	rows, err := s.store.Pool.Query(c.Context(), `
		SELECT id, number, message, sent_at::text, created_at::text
		FROM updates
		WHERE project_id = $1 AND status = 'sent'
		ORDER BY sent_at DESC`, pid)
	if err != nil {
		return err
	}
	defer rows.Close()

	out := []clientUpdateOut{}
	for rows.Next() {
		var up clientUpdateOut
		if err := rows.Scan(&up.ID, &up.Number, &up.Message, &up.SentAt, &up.CreatedAt); err != nil {
			return err
		}
		out = append(out, up)
	}
	if err := rows.Err(); err != nil {
		return err
	}
	return c.JSON(fiber.Map{"updates": out})
}

type clientFeedbackOut struct {
	ID        uuid.UUID `json:"id"`
	Body      string    `json:"body"`
	CreatedAt string    `json:"created_at"`
	Mine      bool      `json:"mine"`
}

// clientFeedback lists feedback on the project (client's own + team's view).
func (s *Server) clientFeedback(c fiber.Ctx) error {
	u := currentUser(c)
	pid, err := uuid.Parse(c.Params("id"))
	if err != nil {
		return errBadRequest("Invalid project id")
	}
	if !s.clientHasAccess(c, u.ID, pid) {
		return errForbidden()
	}
	rows, err := s.store.Pool.Query(c.Context(), `
		SELECT id, body, created_at::text, client_user_id = $2 AS mine
		FROM client_feedback
		WHERE project_id = $1
		ORDER BY created_at DESC`, pid, u.ID)
	if err != nil {
		return err
	}
	defer rows.Close()

	out := []clientFeedbackOut{}
	for rows.Next() {
		var f clientFeedbackOut
		if err := rows.Scan(&f.ID, &f.Body, &f.CreatedAt, &f.Mine); err != nil {
			return err
		}
		out = append(out, f)
	}
	if err := rows.Err(); err != nil {
		return err
	}
	return c.JSON(fiber.Map{"feedback": out})
}

// clientSubmitFeedback lets the client post feedback on their project.
func (s *Server) clientSubmitFeedback(c fiber.Ctx) error {
	u := currentUser(c)
	pid, err := uuid.Parse(c.Params("id"))
	if err != nil {
		return errBadRequest("Invalid project id")
	}
	if !s.clientHasAccess(c, u.ID, pid) {
		return errForbidden()
	}
	var body struct {
		Body string `json:"body"`
	}
	if err := c.Bind().JSON(&body); err != nil {
		return errBadRequest("Invalid request")
	}
	body.Body = strings.TrimSpace(body.Body)
	if body.Body == "" || len(body.Body) > 10000 {
		return errBadRequest("Feedback must be 1-10000 characters")
	}
	var id uuid.UUID
	err = s.store.Pool.QueryRow(c.Context(), `
		INSERT INTO client_feedback (project_id, client_user_id, body)
		VALUES ($1, $2, $3) RETURNING id`, pid, u.ID, body.Body).Scan(&id)
	if err != nil {
		return err
	}
	// Notify the project's team (lead + assignees) about the new feedback.
	s.notifyClientFeedback(c, pid, u.ID, body.Body)
	return c.Status(fiber.StatusCreated).JSON(fiber.Map{"id": id})
}

// notifyClientFeedback inserts a notification for the project's team members
// (team lead and assigned members) when a client submits feedback.
func (s *Server) notifyClientFeedback(c fiber.Ctx, projectID, clientUser uuid.UUID, feedback string) {
	// Find the project name and the team members to notify.
	var projectName string
	var projectNumber int64
	_ = s.store.Pool.QueryRow(c.Context(), `
		SELECT name, number FROM projects WHERE id = $1`, projectID).
		Scan(&projectName, &projectNumber)

	rows, err := s.store.Pool.Query(c.Context(), `
		SELECT DISTINCT u.id
		FROM users u
		LEFT JOIN project_members pm ON pm.user_id = u.id AND pm.project_id = $1
		LEFT JOIN team_members tm ON tm.user_id = u.id
		LEFT JOIN teams t ON t.id = tm.team_id AND t.lead_id IS NOT NULL
		WHERE u.is_active
		  AND (pm.user_id IS NOT NULL           -- assigned to the project
		       OR u.id IN (SELECT lead_id FROM teams WHERE lead_id IS NOT NULL)  -- all team leads
		       OR u.is_admin)                   -- admins
		  AND u.role <> 'client'`,
		projectID)
	if err != nil {
		return
	}
	defer rows.Close()

	title := "New client feedback"
	if projectName != "" {
		title = "New feedback on #" + itoa(projectNumber) + " " + projectName
	}
	bodyPreview := feedback
	if len(bodyPreview) > 200 {
		bodyPreview = bodyPreview[:197] + "..."
	}
	for rows.Next() {
		var recipientID uuid.UUID
		if err := rows.Scan(&recipientID); err != nil {
			continue
		}
		if recipientID == clientUser {
			continue
		}
		_, _ = s.store.Pool.Exec(c.Context(), `
			INSERT INTO notifications (recipient_id, type, project_id, title, body, data)
			VALUES ($1, 'client.feedback', $2, $3, $4, $5)`,
			recipientID, projectID, title, bodyPreview,
			`{"link": "/projects/`+projectID.String()+`"}`)
	}
}

func itoa(n int64) string {
	if n == 0 {
		return "0"
	}
	neg := n < 0
	if neg {
		n = -n
	}
	var b [20]byte
	i := len(b)
	for n > 0 {
		i--
		b[i] = byte('0' + n%10)
		n /= 10
	}
	if neg {
		i--
		b[i] = '-'
	}
	return string(b[i:])
}
