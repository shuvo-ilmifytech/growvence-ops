package httpapi

import (
	"strings"

	"github.com/gofiber/fiber/v3"
	"github.com/google/uuid"

	"github.com/solvinx/opsflow/internal/apperr"
	"github.com/solvinx/opsflow/internal/policy"
	"github.com/solvinx/opsflow/internal/projects"
	"github.com/solvinx/opsflow/internal/validate"
)

// Project board: every rule lives in the projects service (cards.go).

func (s *Server) projectBoard(c fiber.Ctx) error {
	id, err := paramUUID(c, "id")
	if err != nil {
		return err
	}
	b, err := s.projects.Board(c.Context(), currentUser(c), id)
	if err != nil {
		return err
	}
	return c.JSON(b)
}

func (s *Server) createCard(c fiber.Ctx) error {
	id, err := paramUUID(c, "id")
	if err != nil {
		return err
	}
	var req projects.CardInput
	if err := decodeJSON(c, &req); err != nil {
		return err
	}
	card, err := s.projects.CreateCard(c.Context(), currentUser(c), id, req)
	if err != nil {
		return err
	}
	return c.Status(fiber.StatusCreated).JSON(fiber.Map{"card": card})
}

// cardResult answers a card change with the fresh card and its comments.
func (s *Server) cardResult(c fiber.Ctx, id uuid.UUID) error {
	v, err := s.projects.GetCard(c.Context(), currentUser(c), id)
	if err != nil {
		return err
	}
	return c.JSON(v)
}

func (s *Server) getCard(c fiber.Ctx) error {
	id, err := paramUUID(c, "cid")
	if err != nil {
		return err
	}
	return s.cardResult(c, id)
}

func (s *Server) updateCard(c fiber.Ctx) error {
	id, err := paramUUID(c, "cid")
	if err != nil {
		return err
	}
	var req projects.CardInput
	if err := decodeJSON(c, &req); err != nil {
		return err
	}
	if err := s.projects.UpdateCard(c.Context(), currentUser(c), id, req); err != nil {
		return err
	}
	return s.cardResult(c, id)
}

func (s *Server) moveCard(c fiber.Ctx) error {
	id, err := paramUUID(c, "cid")
	if err != nil {
		return err
	}
	var req projects.MoveInput
	if err := decodeJSON(c, &req); err != nil {
		return err
	}
	if err := s.projects.MoveCard(c.Context(), currentUser(c), id, req); err != nil {
		return err
	}
	return s.cardResult(c, id)
}

type cardAssigneesReq struct {
	AssigneeIDs []uuid.UUID `json:"assignee_ids"`
}

func (s *Server) setCardAssignees(c fiber.Ctx) error {
	id, err := paramUUID(c, "cid")
	if err != nil {
		return err
	}
	var req cardAssigneesReq
	if err := decodeJSON(c, &req); err != nil {
		return err
	}
	if err := s.projects.SetCardAssignees(c.Context(), currentUser(c), id, req.AssigneeIDs); err != nil {
		return err
	}
	return s.cardResult(c, id)
}

func (s *Server) removeCard(c fiber.Ctx) error {
	id, err := paramUUID(c, "cid")
	if err != nil {
		return err
	}
	if err := s.projects.RemoveCard(c.Context(), currentUser(c), id); err != nil {
		return err
	}
	return c.SendStatus(fiber.StatusNoContent)
}

func (s *Server) commentCard(c fiber.Ctx) error {
	id, err := paramUUID(c, "cid")
	if err != nil {
		return err
	}
	var req commentReq
	if err := decodeJSON(c, &req); err != nil {
		return err
	}
	if _, err := s.projects.CommentCard(c.Context(), currentUser(c), id, req.Body); err != nil {
		return err
	}
	return s.cardResult(c, id)
}

// allCards is the all-projects board: every card the user can see, with filters.
func (s *Server) allCards(c fiber.Ctx) error {
	v := validate.Errors{}
	f := projects.AllCardsFilter{}
	for key, dst := range map[string]**uuid.UUID{"project": &f.Project, "request": &f.Request, "team": &f.Team} {
		if raw := c.Query(key); raw != "" {
			id, err := uuid.Parse(raw)
			if err != nil {
				v.Add(key, "is not a valid id")
			}
			*dst = &id
		}
	}
	if c.Query("mine") == "true" {
		u := currentUser(c)
		me := u.ID
		// Sales don't get cards assigned to them: theirs are the ones they logged.
		if policy.Role(u.Role) == policy.Sales {
			f.Author = &me
		} else {
			f.Assignee = &me
		}
	}
	if src := c.Query("source"); src != "" {
		v.OneOf("source", src, "client", "internal")
		f.Source = &src
	}
	if q := strings.TrimSpace(c.Query("q")); q != "" {
		v.MaxLen("q", q, 200)
		f.Q = &q
	}
	if !v.OK() {
		return errValidation(v)
	}
	res, err := s.projects.AllCards(c.Context(), currentUser(c), f)
	if err != nil {
		return err
	}
	return c.JSON(res)
}

func (s *Server) initCardUpload(c fiber.Ctx) error {
	id, err := paramUUID(c, "cid")
	if err != nil {
		return err
	}
	var req projects.UploadInput
	if err := decodeJSON(c, &req); err != nil {
		return err
	}
	t, err := s.projects.InitCardUpload(c.Context(), currentUser(c), id, req)
	if err != nil {
		return err
	}
	return c.Status(fiber.StatusCreated).JSON(t)
}

func (s *Server) completeCardUpload(c fiber.Ctx) error {
	id, err := paramUUID(c, "cid")
	if err != nil {
		return err
	}
	if err := s.projects.CompleteCardUpload(c.Context(), currentUser(c), id, c.Params("upload_id")); err != nil {
		return err
	}
	return s.cardResult(c, id)
}

func (s *Server) removeCardAttachment(c fiber.Ctx) error {
	id, err := paramUUID(c, "cid")
	if err != nil {
		return err
	}
	fid, err := paramUUID(c, "file_id")
	if err != nil {
		return err
	}
	if err := s.projects.RemoveCardAttachment(c.Context(), currentUser(c), id, fid); err != nil {
		return err
	}
	return s.cardResult(c, id)
}

// markCardOpened stops the "unopened task" alert ringing for this person.
func (s *Server) markCardOpened(c fiber.Ctx) error {
	id, err := uuid.Parse(c.Params("cid"))
	if err != nil {
		return apperr.NotFound("Card")
	}
	if err := s.projects.MarkCardOpened(c.Context(), currentUser(c), id); err != nil {
		return err
	}
	return c.SendStatus(fiber.StatusNoContent)
}

// alerts is what the browser rings about: work sitting there unhandled.
func (s *Server) alerts(c fiber.Ctx) error {
	out, err := s.projects.Alerts(c.Context(), currentUser(c))
	if err != nil {
		return err
	}
	return c.JSON(out)
}
