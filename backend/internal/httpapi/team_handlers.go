package httpapi

import (
	"context"
	"encoding/json"
	"strings"

	"github.com/gofiber/fiber/v3"
	"github.com/google/uuid"

	"github.com/solvinx/opsflow/internal/db"
	"github.com/solvinx/opsflow/internal/policy"
	"github.com/solvinx/opsflow/internal/store"
	"github.com/solvinx/opsflow/internal/validate"
)

// Teams: each Ops team has a Team Lead and members and owns the projects routed to it by
// project type. HR and the CEO run teams; system admins map project types to teams.

// nullableID tells an absent field from an explicit null in a JSON patch.
type nullableID struct {
	Set bool
	ID  *uuid.UUID
}

func (n *nullableID) UnmarshalJSON(b []byte) error {
	n.Set = true
	if string(b) == "null" {
		return nil
	}
	var id uuid.UUID
	if err := json.Unmarshal(b, &id); err != nil {
		return err
	}
	n.ID = &id
	return nil
}

// listTeams is for pickers and display: active teams, or all with ?all=true.
func (s *Server) listTeams(c fiber.Ctx) error {
	rows, err := s.store.ListTeams(c.Context(), c.Query("all") != "true")
	if err != nil {
		return err
	}
	return c.JSON(fiber.Map{"teams": rows})
}

func (s *Server) adminGetTeam(c fiber.Ctx) error {
	id, err := paramUUID(c, "id")
	if err != nil {
		return err
	}
	t, err := s.store.GetTeam(c.Context(), id)
	if store.IsNotFound(err) {
		return errNotFound("Team")
	} else if err != nil {
		return err
	}
	members, err := s.store.ListTeamMembers(c.Context(), id)
	if err != nil {
		return err
	}
	return c.JSON(fiber.Map{"team": t, "members": members})
}

type teamReq struct {
	Name         *string    `json:"name"`
	LeadID       nullableID `json:"lead_id"`
	DepartmentID nullableID `json:"department_id"`
	IsActive     *bool      `json:"is_active"`
}

// checkLead requires id, when given, to be an active Team Leader: a team lead or a department head.
func (s *Server) checkLead(ctx context.Context, v validate.Errors, field string, id *uuid.UUID) error {
	if id == nil {
		return nil
	}
	u, err := s.store.GetUserByID(ctx, *id)
	if store.IsNotFound(err) || (err == nil && (!u.IsActive || u.Role != string(policy.TL))) {
		v.Add(field, "must be an active Team Leader")
		return nil
	}
	return err
}

func (s *Server) adminCreateTeam(c fiber.Ctx) error {
	var req teamReq
	if err := decodeJSON(c, &req); err != nil {
		return err
	}
	v := validate.Errors{}
	if req.Name == nil {
		v.Add("name", "is required")
	} else {
		*req.Name = strings.TrimSpace(*req.Name)
		v.Len("name", *req.Name, 1, 120)
	}
	if err := s.checkLead(c.Context(), v, "lead_id", req.LeadID.ID); err != nil {
		return err
	}
	if err := s.checkDepartment(c.Context(), v, req.DepartmentID.ID); err != nil {
		return err
	}
	if !v.OK() {
		return errValidation(v)
	}
	t, err := s.store.CreateTeam(c.Context(), db.CreateTeamParams{Name: *req.Name, LeadID: req.LeadID.ID, DepartmentID: req.DepartmentID.ID})
	if store.IsUniqueViolation(err) {
		return errValidation(validate.Errors{"name": "already exists"})
	} else if err != nil {
		return err
	}
	s.logAdmin(c, "team.created", "team", t.ID, t)
	return c.Status(fiber.StatusCreated).JSON(fiber.Map{"team": t})
}

func (s *Server) adminUpdateTeam(c fiber.Ctx) error {
	id, err := paramUUID(c, "id")
	if err != nil {
		return err
	}
	var req teamReq
	if err := decodeJSON(c, &req); err != nil {
		return err
	}
	v := validate.Errors{}
	if req.Name != nil {
		*req.Name = strings.TrimSpace(*req.Name)
		v.Len("name", *req.Name, 1, 120)
	}
	if err := s.checkLead(c.Context(), v, "lead_id", req.LeadID.ID); err != nil {
		return err
	}
	if err := s.checkDepartment(c.Context(), v, req.DepartmentID.ID); err != nil {
		return err
	}
	if !v.OK() {
		return errValidation(v)
	}
	if req.IsActive != nil && !*req.IsActive {
		n, err := s.store.CountOpenTeamProjects(c.Context(), &id)
		if err != nil {
			return err
		}
		if n > 0 {
			return errConflict("This team still has open projects. Finish or cancel them before retiring it.")
		}
	}
	t, err := s.store.UpdateTeam(c.Context(), db.UpdateTeamParams{
		ID: id, Name: req.Name, IsActive: req.IsActive, SetLead: req.LeadID.Set, LeadID: req.LeadID.ID,
		SetDepartment: req.DepartmentID.Set, DepartmentID: req.DepartmentID.ID,
	})
	switch {
	case store.IsNotFound(err):
		return errNotFound("Team")
	case store.IsUniqueViolation(err):
		return errValidation(validate.Errors{"name": "already exists"})
	case err != nil:
		return err
	}
	s.logAdmin(c, "team.updated", "team", t.ID, t)
	return c.JSON(fiber.Map{"team": t})
}

// adminAddTeamMember adds a member to the team, moving them out of their previous one.
func (s *Server) adminAddTeamMember(c fiber.Ctx) error {
	id, err := paramUUID(c, "id")
	if err != nil {
		return err
	}
	userID, err := paramUUID(c, "user_id")
	if err != nil {
		return err
	}
	t, err := s.store.GetTeam(c.Context(), id)
	if store.IsNotFound(err) {
		return errNotFound("Team")
	} else if err != nil {
		return err
	}
	if !t.IsActive {
		return errConflict("This team is retired")
	}
	u, err := s.store.GetUserByID(c.Context(), userID)
	if store.IsNotFound(err) || (err == nil && (!u.IsActive || u.Role != string(policy.Member))) {
		return errValidation(validate.Errors{"user_id": "must be an active team member"})
	} else if err != nil {
		return err
	}
	if err := s.store.SetTeamMember(c.Context(), db.SetTeamMemberParams{UserID: userID, TeamID: id}); err != nil {
		return err
	}
	s.logAdmin(c, "team.member_added", "team", id, map[string]any{"user_id": userID, "name": u.Name})
	return c.SendStatus(fiber.StatusNoContent)
}

func (s *Server) adminRemoveTeamMember(c fiber.Ctx) error {
	id, err := paramUUID(c, "id")
	if err != nil {
		return err
	}
	userID, err := paramUUID(c, "user_id")
	if err != nil {
		return err
	}
	n, err := s.store.RemoveTeamMember(c.Context(), db.RemoveTeamMemberParams{TeamID: id, UserID: userID})
	if err != nil {
		return err
	}
	if n == 0 {
		return errNotFound("Team member")
	}
	s.logAdmin(c, "team.member_removed", "team", id, map[string]any{"user_id": userID})
	return c.SendStatus(fiber.StatusNoContent)
}

// checkTeam: a project type can only be routed to an active team.
func (s *Server) checkTeam(ctx context.Context, v validate.Errors, id *uuid.UUID) error {
	if id == nil {
		return nil
	}
	t, err := s.store.GetTeam(ctx, *id)
	if store.IsNotFound(err) || (err == nil && !t.IsActive) {
		v.Add("team_id", "must be an active team")
		return nil
	}
	return err
}

// checkDepartment: a team can only be put in an active department.
func (s *Server) checkDepartment(ctx context.Context, v validate.Errors, id *uuid.UUID) error {
	if id == nil {
		return nil
	}
	d, err := s.store.GetDepartment(ctx, *id)
	if store.IsNotFound(err) || (err == nil && !d.IsActive) {
		v.Add("department_id", "must be an active department")
		return nil
	}
	return err
}
