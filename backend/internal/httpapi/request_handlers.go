package httpapi

import (
	"errors"
	"strconv"
	"strings"

	"github.com/gofiber/fiber/v3"
	"github.com/google/uuid"

	"github.com/solvinx/opsflow/internal/apperr"
	"github.com/solvinx/opsflow/internal/projects"
	"github.com/solvinx/opsflow/internal/validate"
)

// Client requests: work for a client with no project. Every rule lives in the projects
// service (requests.go); these handlers only read the query and the body.

func (s *Server) listRequests(c fiber.Ctx) error {
	v := validate.Errors{}
	f := projects.RequestFilter{Page: 1, PageSize: 50}
	if raw := c.Query("status"); raw != "" {
		for _, st := range strings.Split(raw, ",") {
			v.OneOf("status", st, projects.RequestStatuses...)
			f.Statuses = append(f.Statuses, st)
		}
	}
	f.Team = queryUUID(c, v, "team")
	f.Mine = c.Query("mine") == "true"
	if q := strings.TrimSpace(c.Query("q")); q != "" {
		v.MaxLen("q", q, 200)
		f.Q = &q
	}
	if p := c.Query("page"); p != "" {
		n, err := strconv.Atoi(p)
		if err != nil || n < 1 || n > 10000 {
			v.Add("page", "must be a positive number")
		}
		f.Page = n
	}
	if p := c.Query("page_size"); p != "" {
		n, err := strconv.Atoi(p)
		if err != nil || n < 1 || n > 200 {
			v.Add("page_size", "must be between 1 and 200")
		}
		f.PageSize = n
	}
	if !v.OK() {
		return errValidation(v)
	}
	res, err := s.projects.ListRequests(c.Context(), currentUser(c), f)
	if err != nil {
		return err
	}
	return c.JSON(res)
}

func (s *Server) requestDetail(c fiber.Ctx, id uuid.UUID) error {
	res, err := s.projects.GetRequest(c.Context(), currentUser(c), id)
	if err != nil {
		return err
	}
	return c.JSON(res)
}

func (s *Server) getRequest(c fiber.Ctx) error {
	id, err := paramUUID(c, "id")
	if err != nil {
		return err
	}
	return s.requestDetail(c, id)
}

func (s *Server) raiseRequest(c fiber.Ctx) error {
	var in projects.RequestInput
	if err := decodeJSON(c, &in); err != nil {
		return err
	}
	id, err := s.projects.Raise(c.Context(), currentUser(c), in)
	if err != nil {
		return err
	}
	return s.requestDetail(c.Status(fiber.StatusCreated), id)
}

func (s *Server) updateRequest(c fiber.Ctx) error {
	id, err := paramUUID(c, "id")
	if err != nil {
		return err
	}
	var in projects.RequestInput
	if err := decodeJSON(c, &in); err != nil {
		return err
	}
	if err := s.projects.EditRequest(c.Context(), currentUser(c), id, in); err != nil {
		return err
	}
	return s.requestDetail(c, id)
}

func (s *Server) moveRequestTeam(c fiber.Ctx) error {
	id, err := paramUUID(c, "id")
	if err != nil {
		return err
	}
	var in projects.RequestTeamInput
	if err := decodeJSON(c, &in); err != nil {
		return err
	}
	if err := s.projects.MoveRequestTeam(c.Context(), currentUser(c), id, in); err != nil {
		return err
	}
	// A leader who hands it to a team they don't run can no longer see it.
	res, err := s.projects.GetRequest(c.Context(), currentUser(c), id)
	var ae *apperr.Error
	if errors.As(err, &ae) && ae.Status == fiber.StatusNotFound {
		return c.JSON(fiber.Map{"moved": true, "visible": false})
	} else if err != nil {
		return err
	}
	return c.JSON(res)
}

func (s *Server) setRequestStatus(c fiber.Ctx) error {
	id, err := paramUUID(c, "id")
	if err != nil {
		return err
	}
	var in projects.RequestStatusInput
	if err := decodeJSON(c, &in); err != nil {
		return err
	}
	if err := s.projects.SetRequestStatus(c.Context(), currentUser(c), id, in); err != nil {
		return err
	}
	return s.requestDetail(c, id)
}

// createRequestCard adds a card to a client request: Sales a follow-up ask from the
// client, the Team Leader a task for the team.
func (s *Server) createRequestCard(c fiber.Ctx) error {
	id, err := paramUUID(c, "id")
	if err != nil {
		return err
	}
	var in projects.CardInput
	if err := decodeJSON(c, &in); err != nil {
		return err
	}
	card, err := s.projects.CreateRequestCard(c.Context(), currentUser(c), id, in)
	if err != nil {
		return err
	}
	return c.Status(fiber.StatusCreated).JSON(fiber.Map{"card": card})
}

// createRequestUpdate starts a client update on a request. From there it is an ordinary
// update: /api/updates/:uid carries it through the same approval.
func (s *Server) createRequestUpdate(c fiber.Ctx) error {
	id, err := paramUUID(c, "id")
	if err != nil {
		return err
	}
	var in projects.UpdateInput
	if err := decodeJSON(c, &in); err != nil {
		return err
	}
	uid, err := s.projects.CreateRequestUpdate(c.Context(), currentUser(c), id, in)
	if err != nil {
		return err
	}
	return s.updateDetail(c.Status(fiber.StatusCreated), uid)
}
