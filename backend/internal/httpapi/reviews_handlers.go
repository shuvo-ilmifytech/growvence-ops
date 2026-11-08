package httpapi

import (
	"strconv"
	"time"

	"github.com/gofiber/fiber/v3"
	"github.com/google/uuid"

	"github.com/solvinx/opsflow/internal/db"
	"github.com/solvinx/opsflow/internal/policy"
)

// Fiverr review tracker: clients' star ratings per order, linked to the project
// and the member who did the work. Everyone can view; sales/TL/admin/CEO can manage.

type reviewOut struct {
	ID           uuid.UUID  `json:"id"`
	OrderID      string     `json:"order_id"`
	ProjectID    *uuid.UUID `json:"project_id"`
	ProjectName  *string    `json:"project_name"`
	MemberID     *uuid.UUID `json:"member_id"`
	MemberName   *string    `json:"member_name"`
	Rating       int        `json:"rating"`
	ReviewText   string     `json:"review_text"`
	ReviewerName string     `json:"reviewer_name"`
	ReviewedAt   string     `json:"reviewed_at"`
	CreatedAt    time.Time  `json:"created_at"`
}

func canManageReviews(u db.User) bool {
	r := roleOf(u)
	return r == policy.Sales || r == policy.TL || isSystemAdmin(u)
}

// listReviews returns all Fiverr reviews, optionally filtered. Internal-only
// (P0-8): client, investor and office_admin callers are blocked.
func (s *Server) listReviews(c fiber.Ctx) error {
	if !canViewInternal(currentUser(c)) {
		return errForbidden()
	}
	rating, _ := strconv.Atoi(c.Query("rating", "0"))
	memberID := c.Query("member_id", "")
	projectID := c.Query("project_id", "")
	q := c.Query("q", "")
	limit, _ := strconv.Atoi(c.Query("limit", "50"))
	offset, _ := strconv.Atoi(c.Query("offset", "0"))
	if limit < 1 || limit > 200 {
		limit = 50
	}
	if offset < 0 {
		offset = 0
	}

	query := `
		SELECT r.id, r.order_id, r.project_id, p.name AS project_name,
		       r.member_id, u.name AS member_name,
		       r.rating, r.review_text, r.reviewer_name,
		       r.reviewed_at::text, r.created_at
		FROM fiverr_reviews r
		LEFT JOIN projects p ON p.id = r.project_id
		LEFT JOIN users u ON u.id = r.member_id
		WHERE ($1 = 0 OR r.rating = $1)
		  AND ($2 = '' OR r.member_id = $2::uuid)
		  AND ($3 = '' OR r.project_id = $3::uuid)
		  AND ($4 = '' OR r.order_id ILIKE '%' || $4 || '%' OR r.reviewer_name ILIKE '%' || $4 || '%' OR r.review_text ILIKE '%' || $4 || '%')
		ORDER BY r.reviewed_at DESC, r.created_at DESC
		LIMIT $5 OFFSET $6`
	rows, err := s.store.Pool.Query(c.Context(), query, rating, memberID, projectID, q, limit, offset)
	if err != nil {
		return err
	}
	defer rows.Close()

	out := []reviewOut{}
	for rows.Next() {
		var r reviewOut
		if err := rows.Scan(&r.ID, &r.OrderID, &r.ProjectID, &r.ProjectName,
			&r.MemberID, &r.MemberName, &r.Rating, &r.ReviewText, &r.ReviewerName,
			&r.ReviewedAt, &r.CreatedAt); err != nil {
			return err
		}
		out = append(out, r)
	}
	return c.JSON(fiber.Map{"reviews": out})
}

type reviewInput struct {
	OrderID      string  `json:"order_id"`
	ProjectID    *string `json:"project_id"`
	MemberID     *string `json:"member_id"`
	Rating       int     `json:"rating"`
	ReviewText   string  `json:"review_text"`
	ReviewerName string  `json:"reviewer_name"`
	ReviewedAt   string  `json:"reviewed_at"`
}

func (s *Server) createReview(c fiber.Ctx) error {
	u := currentUser(c)
	if !canManageReviews(u) {
		return errForbidden()
	}
	var in reviewInput
	if err := c.Bind().JSON(&in); err != nil {
		return errBadRequest("Invalid request body")
	}
	if in.OrderID == "" {
		return errBadRequest("order_id is required")
	}
	if in.Rating < 1 || in.Rating > 5 {
		return errBadRequest("rating must be 1-5")
	}
	var projectID, memberID *uuid.UUID
	if in.ProjectID != nil && *in.ProjectID != "" {
		id, err := uuid.Parse(*in.ProjectID)
		if err != nil {
			return errBadRequest("Invalid project_id")
		}
		projectID = &id
	}
	if in.MemberID != nil && *in.MemberID != "" {
		id, err := uuid.Parse(*in.MemberID)
		if err != nil {
			return errBadRequest("Invalid member_id")
		}
		memberID = &id
	}
	reviewedAt := in.ReviewedAt
	if reviewedAt == "" {
		reviewedAt = time.Now().Format("2006-01-02")
	}

	var id uuid.UUID
	err := s.store.Pool.QueryRow(c.Context(), `
		INSERT INTO fiverr_reviews (order_id, project_id, member_id, rating, review_text, reviewer_name, reviewed_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7::date)
		RETURNING id`,
		in.OrderID, projectID, memberID, in.Rating, in.ReviewText, in.ReviewerName, reviewedAt,
	).Scan(&id)
	if err != nil {
		return err
	}
	return c.Status(fiber.StatusCreated).JSON(fiber.Map{"id": id})
}

func (s *Server) updateReview(c fiber.Ctx) error {
	u := currentUser(c)
	if !canManageReviews(u) {
		return errForbidden()
	}
	id, err := uuid.Parse(c.Params("id"))
	if err != nil {
		return errBadRequest("Invalid id")
	}
	var in reviewInput
	if err := c.Bind().JSON(&in); err != nil {
		return errBadRequest("Invalid request body")
	}
	if in.Rating < 1 || in.Rating > 5 {
		return errBadRequest("rating must be 1-5")
	}
	var projectID, memberID *uuid.UUID
	if in.ProjectID != nil && *in.ProjectID != "" {
		pid, err := uuid.Parse(*in.ProjectID)
		if err != nil {
			return errBadRequest("Invalid project_id")
		}
		projectID = &pid
	}
	if in.MemberID != nil && *in.MemberID != "" {
		mid, err := uuid.Parse(*in.MemberID)
		if err != nil {
			return errBadRequest("Invalid member_id")
		}
		memberID = &mid
	}

	ct, err := s.store.Pool.Exec(c.Context(), `
		UPDATE fiverr_reviews
		SET order_id = $2, project_id = $3, member_id = $4, rating = $5,
		    review_text = $6, reviewer_name = $7, reviewed_at = $8::date
		WHERE id = $1`,
		id, in.OrderID, projectID, memberID, in.Rating, in.ReviewText, in.ReviewerName, in.ReviewedAt,
	)
	if err != nil {
		return err
	}
	if ct.RowsAffected() == 0 {
		return errNotFound("Review not found")
	}
	return c.JSON(fiber.Map{"ok": true})
}

func (s *Server) deleteReview(c fiber.Ctx) error {
	u := currentUser(c)
	if !canManageReviews(u) {
		return errForbidden()
	}
	id, err := uuid.Parse(c.Params("id"))
	if err != nil {
		return errBadRequest("Invalid id")
	}
	ct, err := s.store.Pool.Exec(c.Context(), `DELETE FROM fiverr_reviews WHERE id = $1`, id)
	if err != nil {
		return err
	}
	if ct.RowsAffected() == 0 {
		return errNotFound("Review not found")
	}
	return c.JSON(fiber.Map{"ok": true})
}

// reviewStats returns the aggregate rating statistics. Internal-only (P0-8).
func (s *Server) reviewStats(c fiber.Ctx) error {
	if !canViewInternal(currentUser(c)) {
		return errForbidden()
	}
	var avg float64
	var total int64
	err := s.store.Pool.QueryRow(c.Context(), `
		SELECT COALESCE(AVG(rating), 0), COUNT(*) FROM fiverr_reviews`).Scan(&avg, &total)
	if err != nil {
		return err
	}

	// Count per star rating.
	stars := map[int]int64{1: 0, 2: 0, 3: 0, 4: 0, 5: 0}
	rows, err := s.store.Pool.Query(c.Context(), `
		SELECT rating, COUNT(*) FROM fiverr_reviews GROUP BY rating`)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var rating int
		var count int64
		if err := rows.Scan(&rating, &count); err != nil {
			return err
		}
		stars[rating] = count
	}

	// Monthly trend for the last 6 months.
	trendRows, err := s.store.Pool.Query(c.Context(), `
		SELECT to_char(reviewed_at, 'YYYY-MM') AS month, COUNT(*), COALESCE(AVG(rating), 0)
		FROM fiverr_reviews
		WHERE reviewed_at >= CURRENT_DATE - INTERVAL '6 months'
		GROUP BY month ORDER BY month`)
	if err != nil {
		return err
	}
	defer trendRows.Close()
	trend := []fiber.Map{}
	for trendRows.Next() {
		var month string
		var count int64
		var mavg float64
		if err := trendRows.Scan(&month, &count, &mavg); err != nil {
			return err
		}
		trend = append(trend, fiber.Map{"month": month, "count": count, "avg_rating": mavg})
	}

	// Top members by average rating (min 2 reviews).
	topRows, err := s.store.Pool.Query(c.Context(), `
		SELECT u.name, COUNT(*), AVG(r.rating)
		FROM fiverr_reviews r JOIN users u ON u.id = r.member_id
		GROUP BY u.name HAVING COUNT(*) >= 2
		ORDER BY AVG(r.rating) DESC LIMIT 5`)
	if err != nil {
		return err
	}
	defer topRows.Close()
	top := []fiber.Map{}
	for topRows.Next() {
		var name string
		var count int64
		var mavg float64
		if err := topRows.Scan(&name, &count, &mavg); err != nil {
			return err
		}
		top = append(top, fiber.Map{"name": name, "count": count, "avg_rating": mavg})
	}

	return c.JSON(fiber.Map{
		"avg_rating":   avg,
		"total":        total,
		"by_stars":     stars,
		"trend":        trend,
		"top_members":  top,
		"five_star_pct": func() float64 {
			if total == 0 {
				return 0
			}
			return float64(stars[5]) / float64(total) * 100
		}(),
	})
}
