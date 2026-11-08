package httpapi

import (
	"time"

	"github.com/gofiber/fiber/v3"
	"github.com/google/uuid"

	"github.com/solvinx/opsflow/internal/policy"
)

type deadlineItem struct {
	ID           uuid.UUID `json:"id"`
	Number       int       `json:"number"`
	Name         string    `json:"name"`
	Status       string    `json:"status"`
	DueDate      *string   `json:"due_date"`
	DaysUntilDue int       `json:"days_until_due"` // negative = overdue
	AssigneeID   *uuid.UUID `json:"assignee_id"`
	AssigneeName *string   `json:"assignee_name"`
	TeamName     *string   `json:"team_name"`
	Urgency      string    `json:"urgency"` // "overdue" | "today" | "soon" | "upcoming"
}

type deadlinesOut struct {
	Overdue     []deadlineItem `json:"overdue"`
	DueSoon     []deadlineItem `json:"due_soon"` // today/tomorrow/within 3 days
	OverdueN    int            `json:"overdue_count"`
	DueWeekN    int            `json:"due_week_count"`
}

// deadlines lists projects with upcoming or past due dates, sorted by urgency.
// Regular users see only their own projects; TL/admin/CEO/HR see all.
func (s *Server) deadlines(c fiber.Ctx) error {
	u := currentUser(c)
	r := roleOf(u)
	seeAll := u.IsAdmin || r == policy.CEO || r == policy.TL || r == policy.HR

	rows, err := s.store.Pool.Query(c.Context(), `
		SELECT p.id, p.number, p.name, p.status,
		       coalesce(p.due_date, p.deadline)::text AS due,
		       p.assigned_to, au.name AS assignee_name, t.name AS team_name
		FROM projects p
		LEFT JOIN users au ON au.id = p.assigned_to
		LEFT JOIN teams t ON t.id = p.team_id
		WHERE p.status NOT IN ('completed', 'cancelled', 'draft')
		  AND coalesce(p.due_date, p.deadline) IS NOT NULL
		  AND coalesce(p.due_date, p.deadline) < CURRENT_DATE + interval '8 days'
		ORDER BY coalesce(p.due_date, p.deadline) ASC`)
	if err != nil {
		return err
	}
	defer rows.Close()

	today := time.Now().Truncate(24 * time.Hour)
	out := deadlinesOut{Overdue: []deadlineItem{}, DueSoon: []deadlineItem{}}

	for rows.Next() {
		var it deadlineItem
		var dueStr *string
		var assigneeID *uuid.UUID
		if err := rows.Scan(&it.ID, &it.Number, &it.Name, &it.Status, &dueStr, &assigneeID, &it.AssigneeName, &it.TeamName); err != nil {
			return err
		}
		it.DueDate = dueStr
		it.AssigneeID = assigneeID

		// Permission filter: non-privileged users only see their own projects.
		if !seeAll && (assigneeID == nil || *assigneeID != u.ID) {
			continue
		}

		days := 999
		if dueStr != nil {
			if d, err := time.Parse("2006-01-02", (*dueStr)[:10]); err == nil {
				days = int(d.Sub(today).Hours() / 24)
			}
		}
		it.DaysUntilDue = days

		switch {
		case days < 0:
			it.Urgency = "overdue"
			out.Overdue = append(out.Overdue, it)
		case days <= 3:
			if days <= 1 {
				it.Urgency = "today"
			} else {
				it.Urgency = "soon"
			}
			out.DueSoon = append(out.DueSoon, it)
		default:
			it.Urgency = "upcoming"
			out.DueSoon = append(out.DueSoon, it)
		}
	}
	if err := rows.Err(); err != nil {
		return err
	}

	out.OverdueN = len(out.Overdue)
	for _, it := range out.DueSoon {
		if it.DaysUntilDue <= 7 {
			out.DueWeekN++
		}
	}
	return c.JSON(out)
}
