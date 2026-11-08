package httpapi

import (
	"time"

	"github.com/gofiber/fiber/v3"
	"github.com/google/uuid"

	"github.com/solvinx/opsflow/internal/db"
	"github.com/solvinx/opsflow/internal/policy"
)

// canEditRecurring: team leads, HR, the CEO and system admins manage recurring
// tasks. Members can view them.
func canEditRecurring(u db.User) bool {
	r := roleOf(u)
	return r == policy.TL || r == policy.HR || r == policy.CEO || u.IsAdmin
}

type recurringTask struct {
	ID          uuid.UUID  `json:"id"`
	Title       string     `json:"title"`
	Description string     `json:"description"`
	ProjectID   *uuid.UUID `json:"project_id"`
	ProjectName *string    `json:"project_name"`
	Frequency   string     `json:"frequency"`
	DayOfWeek   *int       `json:"day_of_week"`
	DayOfMonth  *int       `json:"day_of_month"`
	NextRun     string     `json:"next_run"`
	LastRun     *string    `json:"last_run"`
	IsActive    bool       `json:"is_active"`
	CreatedBy   *uuid.UUID `json:"created_by"`
	CreatorName *string    `json:"creator_name"`
	CreatedAt   time.Time  `json:"created_at"`
}

func scanRecurring(rows interface {
	Scan(...any) error
}) (recurringTask, error) {
	var t recurringTask
	var nextRun time.Time
	var lastRun *time.Time
	err := rows.Scan(&t.ID, &t.Title, &t.Description, &t.ProjectID, &t.ProjectName,
		&t.Frequency, &t.DayOfWeek, &t.DayOfMonth, &nextRun, &lastRun,
		&t.IsActive, &t.CreatedBy, &t.CreatorName, &t.CreatedAt)
	if err != nil {
		return t, err
	}
	t.NextRun = nextRun.Format("2006-01-02")
	if lastRun != nil {
		s := lastRun.Format("2006-01-02")
		t.LastRun = &s
	}
	return t, nil
}

const recurringSelect = `
SELECT rt.id, rt.title, rt.description, rt.project_id, p.name AS project_name,
       rt.frequency, rt.day_of_week, rt.day_of_month, rt.next_run, rt.last_run,
       rt.is_active, rt.created_by, u.name AS creator_name, rt.created_at
FROM recurring_tasks rt
LEFT JOIN projects p ON p.id = rt.project_id
LEFT JOIN users u ON u.id = rt.created_by
`

// listRecurring returns all recurring schedules for the team.
// Internal-only (P0-8): client, investor and office_admin callers are
// blocked.
func (s *Server) listRecurring(c fiber.Ctx) error {
	if !canViewInternal(currentUser(c)) {
		return errForbidden()
	}
	rows, err := s.store.Pool.Query(c.Context(), recurringSelect+` ORDER BY rt.next_run ASC, rt.created_at DESC`)
	if err != nil {
		return err
	}
	defer rows.Close()
	out := []recurringTask{}
	for rows.Next() {
		t, err := scanRecurring(rows)
		if err != nil {
			return err
		}
		out = append(out, t)
	}
	return c.JSON(fiber.Map{"recurring_tasks": out})
}

type recurringInput struct {
	Title       string     `json:"title"`
	Description string     `json:"description"`
	ProjectID   *uuid.UUID `json:"project_id"`
	Frequency   string     `json:"frequency"`
	DayOfWeek   *int       `json:"day_of_week"`
	DayOfMonth  *int       `json:"day_of_month"`
	NextRun     string     `json:"next_run"`
}

func (s *Server) createRecurring(c fiber.Ctx) error {
	u := currentUser(c)
	if !canEditRecurring(u) {
		return errForbidden()
	}
	var in recurringInput
	if err := decodeJSON(c, &in); err != nil {
		return errBadRequest("Invalid request body")
	}
	if in.Title == "" {
		return errBadRequest("Title is required")
	}
	if in.Frequency != "daily" && in.Frequency != "weekly" && in.Frequency != "monthly" {
		return errBadRequest("Frequency must be daily, weekly or monthly")
	}
	nextRun := in.NextRun
	if nextRun == "" {
		nextRun = time.Now().Format("2006-01-02")
	}
	var id uuid.UUID
	err := s.store.Pool.QueryRow(c.Context(), `
		INSERT INTO recurring_tasks (title, description, project_id, frequency, day_of_week, day_of_month, next_run, created_by)
		VALUES ($1, $2, $3, $4, $5, $6, $7::date, $8)
		RETURNING id
	`, in.Title, in.Description, in.ProjectID, in.Frequency, in.DayOfWeek, in.DayOfMonth, nextRun, u.ID).Scan(&id)
	if err != nil {
		return err
	}
	return c.Status(fiber.StatusCreated).JSON(fiber.Map{"id": id})
}

func (s *Server) updateRecurring(c fiber.Ctx) error {
	u := currentUser(c)
	if !canEditRecurring(u) {
		return errForbidden()
	}
	id, err := paramUUID(c, "id")
	if err != nil {
		return err
	}
	var in recurringInput
	if err := decodeJSON(c, &in); err != nil {
		return errBadRequest("Invalid request body")
	}
	if in.Title == "" {
		return errBadRequest("Title is required")
	}
	if in.Frequency != "daily" && in.Frequency != "weekly" && in.Frequency != "monthly" {
		return errBadRequest("Frequency must be daily, weekly or monthly")
	}
	tag, err := s.store.Pool.Exec(c.Context(), `
		UPDATE recurring_tasks
		SET title = $2, description = $3, project_id = $4, frequency = $5,
		    day_of_week = $6, day_of_month = $7, next_run = $8::date, updated_at = now()
		WHERE id = $1
	`, id, in.Title, in.Description, in.ProjectID, in.Frequency, in.DayOfWeek, in.DayOfMonth, in.NextRun)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return errNotFound("Recurring task not found")
	}
	return c.JSON(fiber.Map{"ok": true})
}

func (s *Server) deleteRecurring(c fiber.Ctx) error {
	u := currentUser(c)
	if !canEditRecurring(u) {
		return errForbidden()
	}
	id, err := paramUUID(c, "id")
	if err != nil {
		return err
	}
	tag, err := s.store.Pool.Exec(c.Context(), `DELETE FROM recurring_tasks WHERE id = $1`, id)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return errNotFound("Recurring task not found")
	}
	return c.JSON(fiber.Map{"ok": true})
}

func (s *Server) toggleRecurring(c fiber.Ctx) error {
	u := currentUser(c)
	if !canEditRecurring(u) {
		return errForbidden()
	}
	id, err := paramUUID(c, "id")
	if err != nil {
		return err
	}
	var isActive bool
	err = s.store.Pool.QueryRow(c.Context(), `
		UPDATE recurring_tasks SET is_active = NOT is_active, updated_at = now()
		WHERE id = $1 RETURNING is_active
	`, id).Scan(&isActive)
	if err != nil {
		return errNotFound("Recurring task not found")
	}
	return c.JSON(fiber.Map{"ok": true, "is_active": isActive})
}

// runRecurringNow manually triggers a recurring task immediately.
func (s *Server) runRecurringNow(c fiber.Ctx) error {
	u := currentUser(c)
	if !canEditRecurring(u) {
		return errForbidden()
	}
	id, err := paramUUID(c, "id")
	if err != nil {
		return err
	}
	if err := runRecurringTask(c.Context(), s.store.Pool, id, u.ID); err != nil {
		return err
	}
	return c.JSON(fiber.Map{"ok": true})
}
