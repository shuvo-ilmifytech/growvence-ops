package httpapi

import (
	"bytes"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/gofiber/fiber/v3"
	"github.com/google/uuid"

	"github.com/solvinx/opsflow/internal/db"
	"github.com/solvinx/opsflow/internal/policy"
	"github.com/solvinx/opsflow/internal/reports"
	"github.com/solvinx/opsflow/internal/store"
	"github.com/solvinx/opsflow/internal/validate"
)

var (
	targetAmountRe   = regexp.MustCompile(`^\d{1,10}(\.\d{1,2})?$`)
	targetCurrencyRe = regexp.MustCompile(`^[A-Z]{3}$`)
	monthRe          = regexp.MustCompile(`^\d{4}-(0[1-9]|1[0-2])$`)
)

// optionalIDs reads optional uuid query parameters into dst, reporting bad ones.
func optionalIDs(c fiber.Ctx, v validate.Errors, dst map[string]**uuid.UUID) {
	for key, out := range dst {
		if raw := c.Query(key); raw != "" {
			id, err := uuid.Parse(raw)
			if err != nil {
				v.Add(key, "is not a valid id")
				continue
			}
			*out = &id
		}
	}
}

// analyticsProjects lists the projects behind one number on the Analytics page
// (?metric=, the range, and optional team_id, user_id, type_id, sales_id).
func (s *Server) analyticsProjects(c fiber.Ctx) error {
	r, err := s.reports.ParseRange(c.Context(), c.Query("from"), c.Query("to"))
	if err != nil {
		return err
	}
	v := validate.Errors{}
	f := reports.ProjectFilter{Metric: c.Query("metric"), Range: r}
	v.OneOf("metric", f.Metric, reports.Metrics...)
	optionalIDs(c, v, map[string]**uuid.UUID{"team_id": &f.Team, "user_id": &f.Person, "type_id": &f.Type, "sales_id": &f.Sales})
	if !v.OK() {
		return errValidation(v)
	}
	rows, err := s.reports.AnalyticsProjects(c.Context(), f)
	if err != nil {
		return err
	}
	return c.JSON(fiber.Map{"projects": rows})
}

// analyticsExport is every project that came in or was delivered in the range, as CSV.
func (s *Server) analyticsExport(c fiber.Ctx) error {
	r, err := s.reports.ParseRange(c.Context(), c.Query("from"), c.Query("to"))
	if err != nil {
		return err
	}
	v := validate.Errors{}
	var team *uuid.UUID
	optionalIDs(c, v, map[string]**uuid.UUID{"team_id": &team})
	if !v.OK() {
		return errValidation(v)
	}
	var buf bytes.Buffer
	if err := s.reports.WriteAnalyticsCSV(c.Context(), &buf, r, team); err != nil {
		return err
	}
	return sendCSV(c, fmt.Sprintf("analytics_%s_to_%s.csv", r.FromDay, r.ToDay), &buf)
}

// analyticsTrend is deliveries per person and per team (with revenue and target) by month.
func (s *Server) analyticsTrend(c fiber.Ctx) error {
	v := validate.Errors{}
	n := 6
	if raw := c.Query("months"); raw != "" {
		m, err := strconv.Atoi(raw)
		if err != nil || m < 2 || m > 24 {
			v.Add("months", "must be between 2 and 24")
		}
		n = m
	}
	var team *uuid.UUID
	optionalIDs(c, v, map[string]**uuid.UUID{"team_id": &team})
	if !v.OK() {
		return errValidation(v)
	}
	out, err := s.reports.Trend(c.Context(), n, team)
	if err != nil {
		return err
	}
	return c.JSON(out)
}

func (s *Server) thisMonth(c fiber.Ctx) string {
	return time.Now().In(s.reports.Location(c.Context())).Format("2006-01")
}

// analyticsTargets is every team's revenue target for a month (?month=YYYY-MM, default
// this month) and how much of it was delivered.
func (s *Server) analyticsTargets(c fiber.Ctx) error {
	month := c.Query("month", s.thisMonth(c))
	if !monthRe.MatchString(month) {
		return errValidation(validate.Errors{"month": "must be YYYY-MM"})
	}
	out, err := s.reports.Targets(c.Context(), month)
	if err != nil {
		return err
	}
	return c.JSON(out)
}

type targetReq struct {
	Amount   string `json:"amount"`
	Currency string `json:"currency"`
}

// targetParams checks the CEO is asking, and reads the team and month from the path.
func (s *Server) targetParams(c fiber.Ctx) (db.Team, string, error) {
	if roleOf(currentUser(c)) != policy.CEO {
		return db.Team{}, "", errForbidden()
	}
	id, err := paramUUID(c, "team_id")
	if err != nil {
		return db.Team{}, "", err
	}
	month := c.Params("month")
	if !monthRe.MatchString(month) {
		return db.Team{}, "", errValidation(validate.Errors{"month": "must be YYYY-MM"})
	}
	team, err := s.store.GetTeam(c.Context(), id)
	if store.IsNotFound(err) || (err == nil && !team.IsActive) {
		return team, "", errNotFound("Team")
	}
	return team, month, err
}

// setTeamTarget sets or changes a team's revenue target for a month. CEO only.
func (s *Server) setTeamTarget(c fiber.Ctx) error {
	team, month, err := s.targetParams(c)
	if err != nil {
		return err
	}
	var req targetReq
	if err := decodeJSON(c, &req); err != nil {
		return err
	}
	req.Amount = strings.NewReplacer(",", "", " ", "").Replace(strings.TrimSpace(req.Amount))
	req.Currency = strings.ToUpper(strings.TrimSpace(req.Currency))
	if req.Currency == "" {
		req.Currency = "USD"
	}
	v := validate.Errors{}
	if f, err := strconv.ParseFloat(req.Amount, 64); err != nil || f <= 0 || !targetAmountRe.MatchString(req.Amount) {
		v.Add("amount", "must be a positive amount with at most 2 decimals")
	}
	if !targetCurrencyRe.MatchString(req.Currency) {
		v.Add("currency", "must be a 3-letter code like USD")
	}
	if !v.OK() {
		return errValidation(v)
	}
	actor := currentUser(c)
	var out db.UpsertTeamTargetRow
	err = s.store.Tx(c.Context(), func(q *db.Queries) error {
		var err error
		out, err = q.UpsertTeamTarget(c.Context(), db.UpsertTeamTargetParams{
			TeamID: team.ID, Month: month + "-01", Amount: req.Amount, Currency: req.Currency, SetBy: actor.ID,
		})
		if err != nil {
			return err
		}
		return store.LogActivity(c.Context(), q, store.Activity{
			Actor: &actor.ID, Action: "team_target.set", EntityType: "team_target", EntityID: &team.ID,
			Metadata: map[string]any{"team": team.Name, "month": month, "amount": out.Amount, "currency": out.Currency},
		})
	})
	if err != nil {
		return err
	}
	return c.JSON(fiber.Map{"target": out})
}

// clearTeamTarget removes a team's target for a month. CEO only.
func (s *Server) clearTeamTarget(c fiber.Ctx) error {
	team, month, err := s.targetParams(c)
	if err != nil {
		return err
	}
	actor := currentUser(c)
	err = s.store.Tx(c.Context(), func(q *db.Queries) error {
		n, err := q.DeleteTeamTarget(c.Context(), db.DeleteTeamTargetParams{TeamID: team.ID, Month: month + "-01"})
		if err != nil {
			return err
		}
		if n == 0 {
			return errNotFound("Target")
		}
		return store.LogActivity(c.Context(), q, store.Activity{
			Actor: &actor.ID, Action: "team_target.cleared", EntityType: "team_target", EntityID: &team.ID,
			Metadata: map[string]any{"team": team.Name, "month": month},
		})
	})
	if err != nil {
		return err
	}
	return c.SendStatus(fiber.StatusNoContent)
}
