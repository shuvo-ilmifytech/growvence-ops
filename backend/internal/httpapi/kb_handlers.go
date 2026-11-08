package httpapi

import (
	"regexp"
	"strconv"
	"strings"

	"github.com/gofiber/fiber/v3"
	"github.com/google/uuid"

	"github.com/solvinx/opsflow/internal/db"
	"github.com/solvinx/opsflow/internal/policy"
)

// canEditKb: team leads, HR, the CEO and system admins curate the knowledge base.
// Everyone else (sales, members) has read access.
func canEditKb(u db.User) bool {
	r := roleOf(u)
	return r == policy.TL || r == policy.HR || r == policy.CEO || u.IsAdmin
}

var kbSlugRe = regexp.MustCompile(`[^a-z0-9-]+`)

func kbSlugify(title string) string {
	s := strings.ToLower(strings.TrimSpace(title))
	s = kbSlugRe.ReplaceAllString(s, "-")
	s = strings.Trim(s, "-")
	if s == "" {
		s = "article"
	}
	return s
}

// listKbArticles returns all knowledge base articles, optionally filtered.
// Sprint 1 (P0-8): internal-only; client, investor and office_admin callers
// are blocked.
func (s *Server) listKbArticles(c fiber.Ctx) error {
	if !canViewInternal(currentUser(c)) {
		return errForbidden()
	}
	limit, _ := strconv.Atoi(c.Query("limit", "50"))
	offset, _ := strconv.Atoi(c.Query("offset", "0"))
	if limit < 1 || limit > 100 || offset < 0 {
		return errBadRequest("limit must be 1-100 and offset >= 0")
	}
	rows, err := s.store.ListKbArticles(c.Context(), db.ListKbArticlesParams{
		Category:      c.Query("category", ""),
		Search:        c.Query("search", ""),
		PublishedOnly: true,
		Lim:           int32(limit),
		Off:           int32(offset),
	})
	if err != nil {
		return err
	}
	cats, err := s.store.ListKbCategories(c.Context())
	if err != nil {
		return err
	}
	return c.JSON(fiber.Map{"articles": rows, "categories": cats})
}

// getKbArticle returns one article by slug. Internal-only (P0-8).
func (s *Server) getKbArticle(c fiber.Ctx) error {
	if !canViewInternal(currentUser(c)) {
		return errForbidden()
	}
	row, err := s.store.GetKbArticleBySlug(c.Context(), c.Params("slug"))
	if err != nil {
		return errNotFound("Article not found")
	}
	return c.JSON(row)
}

type kbArticleInput struct {
	Title       string `json:"title"`
	Content     string `json:"content"`
	Category    string `json:"category"`
	Slug        string `json:"slug"`
	IsPublished *bool  `json:"is_published"`
}

func (s *Server) createKbArticle(c fiber.Ctx) error {
	u := currentUser(c)
	if !canEditKb(u) {
		return errForbidden()
	}
	var in kbArticleInput
	if err := c.Bind().JSON(&in); err != nil {
		return errBadRequest("Invalid request body")
	}
	in.Title = strings.TrimSpace(in.Title)
	in.Content = strings.TrimSpace(in.Content)
	if in.Title == "" || in.Content == "" {
		return errBadRequest("Title and content are required")
	}
	category := strings.TrimSpace(in.Category)
	if category == "" {
		category = "general"
	}
	slug := strings.TrimSpace(in.Slug)
	if slug == "" {
		slug = kbSlugify(in.Title)
	} else {
		slug = kbSlugify(slug)
	}
	// Ensure unique slug.
	base := slug
	for i := 2; ; i++ {
		exists, err := s.store.KbArticleSlugExists(c.Context(), db.KbArticleSlugExistsParams{Slug: slug, ID: uuid.Nil})
		if err != nil {
			return err
		}
		if !exists {
			break
		}
		slug = base + "-" + strconv.Itoa(i)
	}
	row, err := s.store.CreateKbArticle(c.Context(), db.CreateKbArticleParams{
		Title: in.Title, Slug: slug, Content: in.Content, Category: category,
		CreatedBy: &u.ID,
	})
	if err != nil {
		return err
	}
	return c.Status(fiber.StatusCreated).JSON(row)
}

func (s *Server) updateKbArticle(c fiber.Ctx) error {
	u := currentUser(c)
	if !canEditKb(u) {
		return errForbidden()
	}
	id, err := uuid.Parse(c.Params("id"))
	if err != nil {
		return errBadRequest("Invalid id")
	}
	existing, err := s.store.GetKbArticle(c.Context(), id)
	if err != nil {
		return errNotFound("Article not found")
	}
	var in kbArticleInput
	if err := c.Bind().JSON(&in); err != nil {
		return errBadRequest("Invalid request body")
	}
	title := strings.TrimSpace(in.Title)
	if title == "" {
		title = existing.Title
	}
	content := strings.TrimSpace(in.Content)
	if content == "" {
		content = existing.Content
	}
	category := strings.TrimSpace(in.Category)
	if category == "" {
		category = existing.Category
	}
	slug := existing.Slug
	if newSlug := kbSlugify(strings.TrimSpace(in.Slug)); in.Slug != "" && newSlug != existing.Slug {
		base := newSlug
		for i := 2; ; i++ {
			exists, err := s.store.KbArticleSlugExists(c.Context(), db.KbArticleSlugExistsParams{Slug: newSlug, ID: id})
			if err != nil {
				return err
			}
			if !exists {
				break
			}
			newSlug = base + "-" + strconv.Itoa(i)
		}
		slug = newSlug
	}
	published := existing.IsPublished
	if in.IsPublished != nil {
		published = *in.IsPublished
	}
	row, err := s.store.UpdateKbArticle(c.Context(), db.UpdateKbArticleParams{
		ID: id, Title: title, Slug: slug, Content: content, Category: category,
		IsPublished: published, UpdatedBy: &u.ID,
	})
	if err != nil {
		return err
	}
	return c.JSON(row)
}

func (s *Server) deleteKbArticle(c fiber.Ctx) error {
	u := currentUser(c)
	if !canEditKb(u) {
		return errForbidden()
	}
	id, err := uuid.Parse(c.Params("id"))
	if err != nil {
		return errBadRequest("Invalid id")
	}
	if err := s.store.DeleteKbArticle(c.Context(), id); err != nil {
		return err
	}
	return c.SendStatus(fiber.StatusNoContent)
}
