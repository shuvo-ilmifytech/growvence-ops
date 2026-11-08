package httpapi

import (
	"bytes"
	"encoding/json"
	"errors"
	"slices"
	"strconv"
	"strings"

	"github.com/gofiber/fiber/v3"
	"github.com/google/uuid"

	"github.com/solvinx/opsflow/internal/apperr"
	"github.com/solvinx/opsflow/internal/projects"
	"github.com/solvinx/opsflow/internal/validate"
	"github.com/solvinx/opsflow/internal/workflow"
)

var sorts = []string{"activity", "due", "priority", "created"}

func (s *Server) listProjects(c fiber.Ctx) error {
	u := currentUser(c)
	v := validate.Errors{}
	f := projects.ListFilter{Sort: c.Query("sort", "activity"), Page: 1, PageSize: 25}

	if raw := c.Query("status"); raw != "" {
		for _, st := range strings.Split(raw, ",") {
			if !workflow.Status(st).Valid() {
				v.Add("status", "contains an unknown status")
			}
			f.Statuses = append(f.Statuses, st)
		}
	}
	switch raw := c.Query("assignee"); raw {
	case "":
	case "me":
		f.Assignee = &u.ID
	default:
		id, err := uuid.Parse(raw)
		if err != nil {
			v.Add("assignee", "must be a user id or 'me'")
		}
		f.Assignee = &id
	}
	if p := c.Query("priority"); p != "" {
		v.OneOf("priority", p, projects.Priorities...)
		f.Priority = &p
	}
	if q := strings.TrimSpace(c.Query("q")); q != "" {
		v.MaxLen("q", q, 200)
		f.Q = &q
	}
	if raw := c.Query("team"); raw != "" {
		id, err := uuid.Parse(raw)
		if err != nil {
			v.Add("team", "is not a valid id")
		}
		f.Team = &id
	}
	f.Overdue = c.Query("overdue") == "true"
	if !slices.Contains(sorts, f.Sort) {
		v.OneOf("sort", f.Sort, sorts...)
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
		if err != nil || n < 1 || n > 100 {
			v.Add("page_size", "must be between 1 and 100")
		}
		f.PageSize = n
	}
	if !v.OK() {
		return errValidation(v)
	}
	res, err := s.projects.List(c.Context(), u, f)
	if err != nil {
		return err
	}
	return c.JSON(res)
}

// decodeObject reads a JSON object as raw fields so PATCH can tell "absent" from "null".
func decodeObject(c fiber.Ctx) (map[string]json.RawMessage, error) {
	var raw map[string]json.RawMessage
	dec := json.NewDecoder(bytes.NewReader(c.Body()))
	if err := dec.Decode(&raw); err != nil || raw == nil {
		return nil, errBadRequest("Body must be a JSON object")
	}
	return raw, nil
}

func (s *Server) createProject(c fiber.Ctx) error {
	raw, err := decodeObject(c)
	if err != nil {
		return err
	}
	id, err := s.projects.Create(c.Context(), currentUser(c), raw)
	if err != nil {
		return err
	}
	return s.projectDetail(c.Status(fiber.StatusCreated), id)
}

func (s *Server) getProject(c fiber.Ctx) error {
	id, err := paramUUID(c, "id")
	if err != nil {
		return err
	}
	return s.projectDetail(c, id)
}

func (s *Server) projectDetail(c fiber.Ctx, id uuid.UUID) error {
	res, err := s.projects.Get(c.Context(), currentUser(c), id)
	if err != nil {
		return err
	}
	return c.JSON(res)
}

func (s *Server) updateProject(c fiber.Ctx) error {
	id, err := paramUUID(c, "id")
	if err != nil {
		return err
	}
	raw, err := decodeObject(c)
	if err != nil {
		return err
	}
	if err := s.projects.Update(c.Context(), currentUser(c), id, raw); err != nil {
		return err
	}
	return s.projectDetail(c, id)
}

type transitionReq struct {
	To      string `json:"to"`
	Comment string `json:"comment"`
}

func (s *Server) transitionProject(c fiber.Ctx) error {
	id, err := paramUUID(c, "id")
	if err != nil {
		return err
	}
	var req transitionReq
	if err := decodeJSON(c, &req); err != nil {
		return err
	}
	if err := s.projects.Transition(c.Context(), currentUser(c), id, workflow.Status(req.To), req.Comment); err != nil {
		return err
	}
	return s.projectDetail(c, id)
}

func (s *Server) assignProject(c fiber.Ctx) error {
	id, err := paramUUID(c, "id")
	if err != nil {
		return err
	}
	var req projects.AssignInput
	if err := decodeJSON(c, &req); err != nil {
		return err
	}
	if err := s.projects.Assign(c.Context(), currentUser(c), id, req); err != nil {
		return err
	}
	return s.projectDetail(c, id)
}

func (s *Server) moveProjectTeam(c fiber.Ctx) error {
	id, err := paramUUID(c, "id")
	if err != nil {
		return err
	}
	var req projects.MoveTeamInput
	if err := decodeJSON(c, &req); err != nil {
		return err
	}
	if err := s.projects.MoveTeam(c.Context(), currentUser(c), id, req); err != nil {
		return err
	}
	// A lead who handed it to a team they don't run can no longer see it.
	res, err := s.projects.Get(c.Context(), currentUser(c), id)
	var ae *apperr.Error
	if errors.As(err, &ae) && ae.Status == fiber.StatusNotFound {
		return c.JSON(fiber.Map{"moved": true, "visible": false})
	} else if err != nil {
		return err
	}
	return c.JSON(res)
}

func (s *Server) planProject(c fiber.Ctx) error {
	id, err := paramUUID(c, "id")
	if err != nil {
		return err
	}
	var req projects.PlanInput
	if err := decodeJSON(c, &req); err != nil {
		return err
	}
	if err := s.projects.Plan(c.Context(), currentUser(c), id, req); err != nil {
		return err
	}
	return s.projectDetail(c, id)
}

type commentReq struct {
	Body string `json:"body"`
}

func (s *Server) commentProject(c fiber.Ctx) error {
	id, err := paramUUID(c, "id")
	if err != nil {
		return err
	}
	var req commentReq
	if err := decodeJSON(c, &req); err != nil {
		return err
	}
	cm, err := s.projects.Comment(c.Context(), currentUser(c), id, req.Body)
	if err != nil {
		return err
	}
	return c.Status(fiber.StatusCreated).JSON(fiber.Map{"comment": cm})
}

func (s *Server) initUpload(c fiber.Ctx) error {
	id, err := paramUUID(c, "id")
	if err != nil {
		return err
	}
	var req projects.UploadInput
	if err := decodeJSON(c, &req); err != nil {
		return err
	}
	ticket, err := s.projects.InitUpload(c.Context(), currentUser(c), id, req)
	if err != nil {
		return err
	}
	return c.Status(fiber.StatusCreated).JSON(ticket)
}

func (s *Server) completeUpload(c fiber.Ctx) error {
	id, err := paramUUID(c, "id")
	if err != nil {
		return err
	}
	uploadID, err := paramUUID(c, "upload_id")
	if err != nil {
		return err
	}
	if err := s.projects.CompleteUpload(c.Context(), currentUser(c), id, uploadID.String()); err != nil {
		return err
	}
	return s.projectDetail(c, id)
}

func (s *Server) removeFile(c fiber.Ctx) error {
	id, err := paramUUID(c, "id")
	if err != nil {
		return err
	}
	fileID, err := paramUUID(c, "file_id")
	if err != nil {
		return err
	}
	if err := s.projects.RemoveFile(c.Context(), currentUser(c), id, fileID); err != nil {
		return err
	}
	return s.projectDetail(c, id)
}

// downloadFile redirects to a short-lived presigned URL; the API never streams file bytes.
func (s *Server) downloadFile(c fiber.Ctx) error {
	versionID, err := paramUUID(c, "version_id")
	if err != nil {
		return err
	}
	url, err := s.projects.DownloadURL(c.Context(), currentUser(c), versionID)
	if err != nil {
		return err
	}
	c.Set(fiber.HeaderCacheControl, "no-store")
	return c.Redirect().Status(fiber.StatusFound).To(url)
}

func (s *Server) acknowledgeProject(c fiber.Ctx) error {
	id, err := paramUUID(c, "id")
	if err != nil {
		return err
	}
	if err := s.projects.Acknowledge(c.Context(), currentUser(c), id); err != nil {
		return err
	}
	return s.projectDetail(c, id)
}

func (s *Server) viewProject(c fiber.Ctx) error {
	id, err := paramUUID(c, "id")
	if err != nil {
		return err
	}
	prev, err := s.projects.MarkViewed(c.Context(), currentUser(c), id)
	if err != nil {
		return err
	}
	return c.JSON(fiber.Map{"previous_viewed_at": prev})
}

// search is the one box across projects, cards, files and people.
func (s *Server) search(c fiber.Ctx) error {
	res, err := s.projects.Search(c.Context(), currentUser(c), c.Query("q"))
	if err != nil {
		return err
	}
	return c.JSON(res)
}

func (s *Server) setStoreAccess(c fiber.Ctx) error {
	id, err := paramUUID(c, "id")
	if err != nil {
		return err
	}
	var req projects.StoreAccessInput
	if err := decodeJSON(c, &req); err != nil {
		return err
	}
	if err := s.projects.SetStoreAccess(c.Context(), currentUser(c), id, req); err != nil {
		return err
	}
	return s.projectDetail(c, id)
}
