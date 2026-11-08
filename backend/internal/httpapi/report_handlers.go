package httpapi

import (
	"bytes"
	"fmt"
	"strconv"
	"strings"

	"github.com/gofiber/fiber/v3"
	"github.com/google/uuid"

	"github.com/solvinx/opsflow/internal/reports"
	"github.com/solvinx/opsflow/internal/updateflow"
	"github.com/solvinx/opsflow/internal/validate"
	"github.com/solvinx/opsflow/internal/workflow"
)

func (s *Server) reportSummary(c fiber.Ctx) error {
	r, err := s.reports.ParseRange(c.Context(), c.Query("from"), c.Query("to"))
	if err != nil {
		return err
	}
	sum, err := s.reports.Summary(c.Context(), r)
	if err != nil {
		return err
	}
	return c.JSON(sum)
}

// reportWorkload is who is carrying what: a Team Leader's own team, or everyone for the CEO.
func (s *Server) reportWorkload(c fiber.Ctx) error {
	w, err := s.reports.Workload(c.Context(), currentUser(c))
	if err != nil {
		return err
	}
	return c.JSON(w)
}

// reportCompany is the CEO's dashboard: company numbers as of now.
func (s *Server) reportCompany(c fiber.Ctx) error {
	out, err := s.reports.Company(c.Context())
	if err != nil {
		return err
	}
	return c.JSON(out)
}

// analytics is the Analytics page: intake, output and performance over a range, for the
// whole company or one team (?team_id=). The step is ?bucket=day|week|month, or picked
// from the range's length.
func (s *Server) analytics(c fiber.Ctx) error {
	r, err := s.reports.ParseRange(c.Context(), c.Query("from"), c.Query("to"))
	if err != nil {
		return err
	}
	v := validate.Errors{}
	bucket := c.Query("bucket")
	if bucket == "" {
		bucket = reports.AutoBucket(r)
	}
	v.OneOf("bucket", bucket, "day", "week", "month")
	if bucket == "day" && r.To.Sub(r.From).Hours() > 24*190 {
		v.Add("bucket", "day is for ranges up to six months; pick week or month")
	}
	var team *uuid.UUID
	if raw := c.Query("team_id"); raw != "" {
		id, err := uuid.Parse(raw)
		if err != nil {
			v.Add("team_id", "is not a valid id")
		}
		team = &id
	}
	if !v.OK() {
		return errValidation(v)
	}
	out, err := s.reports.Analytics(c.Context(), r, bucket, team)
	if err != nil {
		return err
	}
	return c.JSON(out)
}

func sendCSV(c fiber.Ctx, name string, buf *bytes.Buffer) error {
	c.Set(fiber.HeaderContentType, "text/csv; charset=utf-8")
	c.Set(fiber.HeaderContentDisposition, fmt.Sprintf(`attachment; filename="%s"`, name))
	c.Set(fiber.HeaderCacheControl, "no-store")
	// UTF-8 BOM so Excel opens non-ASCII text (e.g. Bangla client names) correctly.
	return c.Send(append([]byte("\xEF\xBB\xBF"), buf.Bytes()...))
}

func (s *Server) exportUpdates(c fiber.Ctx) error {
	r, err := s.reports.ParseRange(c.Context(), c.Query("from"), c.Query("to"))
	if err != nil {
		return err
	}
	v := validate.Errors{}
	f := reports.UpdateExportFilter{Range: r}
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
		f.Q = &q
	}
	if !v.OK() {
		return errValidation(v)
	}
	var buf bytes.Buffer
	if err := s.reports.WriteUpdatesCSV(c.Context(), &buf, f); err != nil {
		return err
	}
	return sendCSV(c, fmt.Sprintf("updates_%s_to_%s.csv", r.FromDay, r.ToDay), &buf)
}

func (s *Server) exportProjects(c fiber.Ctx) error {
	r, err := s.reports.ParseRange(c.Context(), c.Query("from"), c.Query("to"))
	if err != nil {
		return err
	}
	v := validate.Errors{}
	f := reports.ProjectExportFilter{Range: r}
	if raw := c.Query("status"); raw != "" {
		for _, st := range strings.Split(raw, ",") {
			if !workflow.Status(st).Valid() {
				v.Add("status", "contains an unknown status")
			}
			f.Statuses = append(f.Statuses, st)
		}
	}
	if q := strings.TrimSpace(c.Query("q")); q != "" {
		f.Q = &q
	}
	if !v.OK() {
		return errValidation(v)
	}
	var buf bytes.Buffer
	if err := s.reports.WriteProjectsCSV(c.Context(), &buf, currentUser(c), f); err != nil {
		return err
	}
	return sendCSV(c, fmt.Sprintf("projects_%s_to_%s.csv", r.FromDay, r.ToDay), &buf)
}

func (s *Server) listActivity(c fiber.Ctx) error {
	v := validate.Errors{}
	f := reports.ActivityFilter{Limit: 50}
	if raw := c.Query("before_id"); raw != "" {
		n, err := strconv.ParseInt(raw, 10, 64)
		if err != nil {
			v.Add("before_id", "must be a number")
		}
		f.BeforeID = &n
	}
	for key, dst := range map[string]**uuid.UUID{"project_id": &f.ProjectID, "actor_id": &f.ActorID} {
		if raw := c.Query(key); raw != "" {
			id, err := uuid.Parse(raw)
			if err != nil {
				v.Add(key, "is not a valid id")
			}
			*dst = &id
		}
	}
	if t := c.Query("type"); t != "" {
		v.OneOf("type", t, "project", "update", "user", "setting", "auth", "profile", "project_type", "employee", "attendance", "leave", "department", "leave_type", "holiday", "shift", "salary", "team", "card", "finance_entry", "finance_category", "finance_rate", "team_target")
		f.ActionPrefix = &t
	}
	if raw := c.Query("limit"); raw != "" {
		n, err := strconv.Atoi(raw)
		if err != nil || n < 1 || n > 200 {
			v.Add("limit", "must be between 1 and 200")
		}
		f.Limit = n
	}
	if !v.OK() {
		return errValidation(v)
	}
	rows, err := s.reports.Activity(c.Context(), currentUser(c), f)
	if err != nil {
		return err
	}
	return c.JSON(fiber.Map{"activity": rows})
}
