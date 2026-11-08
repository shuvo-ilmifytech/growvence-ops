package httpapi

import (
	"strconv"
	"strings"

	"github.com/gofiber/fiber/v3"
	"github.com/google/uuid"

	"github.com/solvinx/opsflow/internal/projects"
	"github.com/solvinx/opsflow/internal/updateflow"
	"github.com/solvinx/opsflow/internal/validate"
)

var updateStatuses = []string{"draft", "pending_approval", "changes_requested", "approved", "sent"}

func (s *Server) listUpdates(c fiber.Ctx) error {
	v := validate.Errors{}
	f := projects.UpdateFilter{Page: 1, PageSize: 50}
	if raw := c.Query("status"); raw != "" {
		for _, st := range strings.Split(raw, ",") {
			v.OneOf("status", st, updateStatuses...)
			f.Statuses = append(f.Statuses, st)
		}
	}
	if d := c.Query("destination"); d != "" {
		v.OneOf("destination", d, updateflow.Destinations...)
		f.Destination = &d
	}
	if q := strings.TrimSpace(c.Query("q")); q != "" {
		v.MaxLen("q", q, 200)
		f.Q = &q
	}
	if raw := c.Query("project_id"); raw != "" {
		id, err := uuid.Parse(raw)
		if err != nil {
			v.Add("project_id", "is not a valid id")
		}
		f.ProjectID = &id
	}
	// Updates on a client request: a client with no project in the portal.
	if raw := c.Query("client_request_id"); raw != "" {
		id, err := uuid.Parse(raw)
		if err != nil {
			v.Add("client_request_id", "is not a valid id")
		}
		f.RequestID = &id
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
	res, err := s.projects.ListUpdates(c.Context(), currentUser(c), f)
	if err != nil {
		return err
	}
	return c.JSON(res)
}

func (s *Server) updateDetail(c fiber.Ctx, id uuid.UUID) error {
	res, err := s.projects.GetUpdate(c.Context(), currentUser(c), id)
	if err != nil {
		return err
	}
	return c.JSON(res)
}

func (s *Server) getUpdate(c fiber.Ctx) error {
	id, err := paramUUID(c, "uid")
	if err != nil {
		return err
	}
	return s.updateDetail(c, id)
}

func (s *Server) createUpdate(c fiber.Ctx) error {
	pid, err := paramUUID(c, "id")
	if err != nil {
		return err
	}
	var in projects.UpdateInput
	if err := decodeJSON(c, &in); err != nil {
		return err
	}
	id, err := s.projects.CreateUpdate(c.Context(), currentUser(c), pid, in)
	if err != nil {
		return err
	}
	return s.updateDetail(c.Status(fiber.StatusCreated), id)
}

// withUpdate runs an action on /updates/:uid and responds with the fresh detail.
func (s *Server) withUpdate(fn func(c fiber.Ctx, id uuid.UUID) error) fiber.Handler {
	return func(c fiber.Ctx) error {
		id, err := paramUUID(c, "uid")
		if err != nil {
			return err
		}
		if err := fn(c, id); err != nil {
			return err
		}
		return s.updateDetail(c, id)
	}
}

func (s *Server) editUpdate(c fiber.Ctx, id uuid.UUID) error {
	var in projects.UpdateInput
	if err := decodeJSON(c, &in); err != nil {
		return err
	}
	return s.projects.EditUpdate(c.Context(), currentUser(c), id, in)
}

func (s *Server) submitUpdate(c fiber.Ctx, id uuid.UUID) error {
	return s.projects.SubmitUpdate(c.Context(), currentUser(c), id)
}

func (s *Server) approveUpdate(c fiber.Ctx, id uuid.UUID) error {
	var in projects.ApproveInput
	if len(c.Body()) > 0 {
		if err := decodeJSON(c, &in); err != nil {
			return err
		}
	}
	return s.projects.ApproveUpdate(c.Context(), currentUser(c), id, in)
}

func (s *Server) requestUpdateChanges(c fiber.Ctx, id uuid.UUID) error {
	var in struct {
		Comment string `json:"comment"`
	}
	if err := decodeJSON(c, &in); err != nil {
		return err
	}
	return s.projects.RequestUpdateChanges(c.Context(), currentUser(c), id, in.Comment)
}

func (s *Server) markUpdateSent(c fiber.Ctx, id uuid.UUID) error {
	return s.projects.MarkSent(c.Context(), currentUser(c), id)
}

func (s *Server) commentUpdate(c fiber.Ctx, id uuid.UUID) error {
	var in struct {
		Thread string `json:"thread"`
		Body   string `json:"body"`
	}
	if err := decodeJSON(c, &in); err != nil {
		return err
	}
	_, err := s.projects.CommentUpdate(c.Context(), currentUser(c), id, in.Thread, in.Body)
	return err
}

func (s *Server) initUpdateUpload(c fiber.Ctx) error {
	id, err := paramUUID(c, "uid")
	if err != nil {
		return err
	}
	var in projects.UploadInput
	if err := decodeJSON(c, &in); err != nil {
		return err
	}
	ticket, err := s.projects.InitUpdateUpload(c.Context(), currentUser(c), id, in)
	if err != nil {
		return err
	}
	return c.Status(fiber.StatusCreated).JSON(ticket)
}

func (s *Server) completeUpdateUpload(c fiber.Ctx, id uuid.UUID) error {
	uploadID, err := paramUUID(c, "upload_id")
	if err != nil {
		return err
	}
	return s.projects.CompleteUpdateUpload(c.Context(), currentUser(c), id, uploadID.String())
}

func (s *Server) removeUpdateAttachment(c fiber.Ctx, id uuid.UUID) error {
	fileID, err := paramUUID(c, "file_id")
	if err != nil {
		return err
	}
	return s.projects.RemoveUpdateAttachment(c.Context(), currentUser(c), id, fileID)
}
