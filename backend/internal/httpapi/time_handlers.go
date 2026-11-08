package httpapi

import (
	"strconv"
	"time"

	"github.com/gofiber/fiber/v3"
	"github.com/google/uuid"

	"github.com/solvinx/opsflow/internal/db"
	"github.com/solvinx/opsflow/internal/policy"
)

// Time tracking: users run a single timer at a time against an optional project.
// Users see and manage their own entries; TLs, HR, the CEO and system admins
// can view everyone's.

type timeEntryOut struct {
	ID              uuid.UUID  `json:"id"`
	UserID          uuid.UUID  `json:"user_id"`
	UserName        *string    `json:"user_name"`
	ProjectID       *uuid.UUID `json:"project_id"`
	ProjectName     *string    `json:"project_name"`
	Description     string     `json:"description"`
	StartedAt       time.Time  `json:"started_at"`
	EndedAt         *time.Time `json:"ended_at"`
	DurationMinutes *int       `json:"duration_minutes"`
	CreatedAt       time.Time  `json:"created_at"`
}

type timeStartInput struct {
	ProjectID   *string `json:"project_id"`
	Description string  `json:"description"`
}

func canViewAllTime(u db.User) bool {
	r := roleOf(u)
	return r == policy.TL || r == policy.HR || isSystemAdmin(u)
}

// stopRunning ends the user's open timer, if any, computing its duration.
func (s *Server) stopRunning(ctx fiber.Ctx, userID uuid.UUID) error {
	_, err := s.store.Pool.Exec(ctx.Context(), `
		UPDATE time_entries
		SET ended_at = now(),
		    duration_minutes = GREATEST(1, EXTRACT(EPOCH FROM (now() - started_at))::int / 60)
		WHERE user_id = $1 AND ended_at IS NULL`, userID)
	return err
}

func (s *Server) startTimer(c fiber.Ctx) error {
	u := currentUser(c)
	var in timeStartInput
	if err := c.Bind().JSON(&in); err != nil {
		return errBadRequest("Invalid request body")
	}
	var projectID *uuid.UUID
	if in.ProjectID != nil && *in.ProjectID != "" {
		id, err := uuid.Parse(*in.ProjectID)
		if err != nil {
			return errBadRequest("Invalid project_id")
		}
		projectID = &id
	}
	// One timer at a time: stop whatever is running, then start the new one.
	if err := s.stopRunning(c, u.ID); err != nil {
		return err
	}
	var id uuid.UUID
	err := s.store.Pool.QueryRow(c.Context(), `
		INSERT INTO time_entries (user_id, project_id, description)
		VALUES ($1, $2, $3)
		RETURNING id`, u.ID, projectID, in.Description).Scan(&id)
	if err != nil {
		return err
	}
	return c.Status(fiber.StatusCreated).JSON(fiber.Map{"id": id})
}

func (s *Server) stopTimer(c fiber.Ctx) error {
	u := currentUser(c)
	if err := s.stopRunning(c, u.ID); err != nil {
		return err
	}
	return c.JSON(fiber.Map{"ok": true})
}

func (s *Server) runningTimer(c fiber.Ctx) error {
	u := currentUser(c)
	var e timeEntryOut
	var projectName *string
	err := s.store.Pool.QueryRow(c.Context(), `
		SELECT t.id, t.user_id, u.name, t.project_id, p.name,
		       t.description, t.started_at, t.ended_at, t.duration_minutes, t.created_at
		FROM time_entries t
		JOIN users u ON u.id = t.user_id
		LEFT JOIN projects p ON p.id = t.project_id
		WHERE t.user_id = $1 AND t.ended_at IS NULL
		ORDER BY t.started_at DESC LIMIT 1`, u.ID,
	).Scan(&e.ID, &e.UserID, &e.UserName, &e.ProjectID, &projectName,
		&e.Description, &e.StartedAt, &e.EndedAt, &e.DurationMinutes, &e.CreatedAt)
	if err != nil {
		// No running timer: not an error.
		return c.JSON(fiber.Map{"running": nil})
	}
	e.ProjectName = projectName
	return c.JSON(fiber.Map{"running": e})
}

func (s *Server) listTimeEntries(c fiber.Ctx) error {
	u := currentUser(c)
	userID := c.Query("user_id", "")
	projectID := c.Query("project_id", "")
	from := c.Query("from", "") // YYYY-MM-DD
	to := c.Query("to", "")     // YYYY-MM-DD
	limit, _ := strconv.Atoi(c.Query("limit", "50"))
	offset, _ := strconv.Atoi(c.Query("offset", "0"))
	if limit < 1 || limit > 200 {
		limit = 50
	}
	if offset < 0 {
		offset = 0
	}

	// Regular users only see their own entries; privileged roles may filter by user.
	if userID != "" && !canViewAllTime(u) {
		return errForbidden()
	}

	rows, err := s.store.Pool.Query(c.Context(), `
		SELECT t.id, t.user_id, us.name, t.project_id, p.name,
		       t.description, t.started_at, t.ended_at, t.duration_minutes, t.created_at
		FROM time_entries t
		JOIN users us ON us.id = t.user_id
		LEFT JOIN projects p ON p.id = t.project_id
		WHERE ($1 = '' OR t.user_id = $1::uuid)
		  AND ($2 = '' OR t.project_id = $2::uuid)
		  AND ($3 = '' OR t.started_at::date >= $3::date)
		  AND ($4 = '' OR t.started_at::date <= $4::date)
		  AND ($5 = '' OR t.user_id = $5::uuid OR $6)
		ORDER BY t.started_at DESC
		LIMIT $7 OFFSET $8`,
		userID, projectID, from, to,
		u.ID.String(), canViewAllTime(u), limit, offset)
	if err != nil {
		return err
	}
	defer rows.Close()

	entries := []timeEntryOut{}
	for rows.Next() {
		var e timeEntryOut
		if err := rows.Scan(&e.ID, &e.UserID, &e.UserName, &e.ProjectID, &e.ProjectName,
			&e.Description, &e.StartedAt, &e.EndedAt, &e.DurationMinutes, &e.CreatedAt); err != nil {
			return err
		}
		entries = append(entries, e)
	}

	// Summary totals (minutes) for the filtered set: today and last 7 days, for the viewer.
	var todayMin, weekMin int64
	scopeUser := userID
	if scopeUser == "" && !canViewAllTime(u) {
		scopeUser = u.ID.String()
	}
	err = s.store.Pool.QueryRow(c.Context(), `
		SELECT COALESCE(SUM(duration_minutes) FILTER (WHERE started_at::date = CURRENT_DATE), 0),
		       COALESCE(SUM(duration_minutes) FILTER (WHERE started_at::date >= CURRENT_DATE - 6), 0)
		FROM time_entries
		WHERE ($1 = '' OR user_id = $1::uuid) AND duration_minutes IS NOT NULL`,
		scopeUser).Scan(&todayMin, &weekMin)
	if err != nil {
		return err
	}

	return c.JSON(fiber.Map{
		"entries":      entries,
		"today_total":  todayMin,
		"week_total":   weekMin,
		"can_view_all": canViewAllTime(u),
	})
}

func (s *Server) deleteTimeEntry(c fiber.Ctx) error {
	u := currentUser(c)
	id, err := uuid.Parse(c.Params("id"))
	if err != nil {
		return errBadRequest("Invalid id")
	}
	// Users may delete their own entries; privileged roles may delete anyone's.
	query := `DELETE FROM time_entries WHERE id = $1 AND ($2 OR user_id = $3)`
	ct, err := s.store.Pool.Exec(c.Context(), query, id, canViewAllTime(u), u.ID)
	if err != nil {
		return err
	}
	if ct.RowsAffected() == 0 {
		return errNotFound("Time entry not found")
	}
	return c.JSON(fiber.Map{"ok": true})
}
