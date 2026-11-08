package httpapi

import (
	"time"

	"github.com/gofiber/fiber/v3"

	"github.com/solvinx/opsflow/internal/policy"
)

// requireInsights gates the Insights pages: team leads, HR, the CEO and system
// admins may see company-wide performance data; everyone else gets a 403.
func requireInsights(c fiber.Ctx) error {
	u := currentUser(c)
	r := roleOf(u)
	if u.IsAdmin || r == policy.CEO || r == policy.TL || r == policy.HR {
		return c.Next()
	}
	return errForbidden()
}

type forecastMonth struct {
	Month     string  `json:"month"`     // "2026-11"
	Projected float64 `json:"projected"` // sum of agreed_price expected that month
	Currency  string  `json:"currency"`
	Count     int     `json:"count"` // projects contributing to the month
}

type revenueForecastOut struct {
	Months        []forecastMonth `json:"months"`
	TotalPipeline float64         `json:"total_pipeline"`
	Currency      string          `json:"currency"`
}

// revenueForecast projects revenue for the next 6 months from active projects.
// Each active project's agreed_price lands in the month of its effective due
// date (due_date, falling back to deadline, then to the current month).
// Read-only: no new tables, plain SQL over projects.
func (s *Server) revenueForecast(c fiber.Ctx) error {
	rows, err := s.store.Pool.Query(c.Context(), `
		SELECT agreed_price, currency, coalesce(due_date, deadline)
		FROM projects
		WHERE status NOT IN ('completed', 'cancelled')
		  AND agreed_price IS NOT NULL AND agreed_price > 0`)
	if err != nil {
		return err
	}
	defer rows.Close()

	now := time.Now()
	months := make([]forecastMonth, 6)
	for i := range months {
		t := now.AddDate(0, i, 1)
		months[i] = forecastMonth{Month: t.Format("2006-01"), Currency: "USD"}
	}
	total := 0.0

	for rows.Next() {
		var price float64
		var currency string
		var due *time.Time
		if err := rows.Scan(&price, &currency, &due); err != nil {
			return err
		}
		total += price
		m := now.Format("2006-01")
		if due != nil {
			m = due.Format("2006-01")
		}
		for i := range months {
			if months[i].Month == m {
				months[i].Projected += price
				months[i].Count++
				break
			}
		}
		// Projects due outside the 6-month window still count toward the pipeline
		// total but not toward any displayed month.
	}
	if err := rows.Err(); err != nil {
		return err
	}
	return c.JSON(revenueForecastOut{Months: months, TotalPipeline: total, Currency: "USD"})
}

type leaderboardRow struct {
	UserID    string   `json:"user_id"`
	Name      string   `json:"name"`
	Role      string   `json:"role"`
	Completed int      `json:"completed"`
	OnTimePct *float64 `json:"on_time_pct"` // nil when the member completed nothing
	Active    int      `json:"active"`
	AvgRating *float64 `json:"avg_rating"` // nil: no fiverr_reviews table yet
}

// leaderboard ranks members by completed projects, with on-time delivery rate
// and current workload. A project counts as completed for every member on the
// project; on-time means completed_at was on or before the effective due date.
func (s *Server) leaderboard(c fiber.Ctx) error {
	rows, err := s.store.Pool.Query(c.Context(), `
		SELECT u.id, u.name, u.role,
		       count(*) FILTER (WHERE p.status = 'completed') AS completed,
		       count(*) FILTER (WHERE p.status = 'completed'
		                        AND p.completed_at IS NOT NULL
		                        AND coalesce(p.due_date, p.deadline) IS NOT NULL
		                        AND p.completed_at::date <= coalesce(p.due_date, p.deadline)) AS on_time,
		       count(*) FILTER (WHERE p.status NOT IN ('completed', 'cancelled')) AS active,
		       (SELECT avg(rating) FROM fiverr_reviews r WHERE r.member_id = u.id) AS avg_rating
		FROM users u
		LEFT JOIN project_members pm ON pm.user_id = u.id
		LEFT JOIN projects p ON p.id = pm.project_id
		WHERE u.is_active AND u.role IN ('member', 'tl')
		GROUP BY u.id, u.name, u.role
		ORDER BY completed DESC, u.name`)
	if err != nil {
		return err
	}
	defer rows.Close()

	out := []leaderboardRow{}
	for rows.Next() {
		var r leaderboardRow
		var completed, onTime, active int
		if err := rows.Scan(&r.UserID, &r.Name, &r.Role, &completed, &onTime, &active, &r.AvgRating); err != nil {
			return err
		}
		r.Completed = completed
		r.Active = active
		if completed > 0 {
			pct := float64(onTime) / float64(completed) * 100
			r.OnTimePct = &pct
		}
		out = append(out, r)
	}
	if err := rows.Err(); err != nil {
		return err
	}
	return c.JSON(out)
}
