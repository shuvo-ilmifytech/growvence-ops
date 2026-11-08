package httpapi

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/gofiber/fiber/v3"
	"github.com/google/uuid"

	"github.com/solvinx/opsflow/internal/auth"
	"github.com/solvinx/opsflow/internal/db"
	"github.com/solvinx/opsflow/internal/hr"
	"github.com/solvinx/opsflow/internal/policy"
	"github.com/solvinx/opsflow/internal/store"
	"github.com/solvinx/opsflow/internal/validate"
)

func roleNames() []string {
	out := make([]string, len(policy.Roles))
	for i, r := range policy.Roles {
		out[i] = string(r)
	}
	return out
}

// assignableRoles are the roles people managers can give: the CEO is set with opsctl.
func assignableRoles() []string {
	out := []string{}
	for _, r := range policy.Roles {
		if r != policy.CEO {
			out = append(out, string(r))
		}
	}
	return out
}

// Who may change whom (HR and the CEO reach these handlers):
//   - The CEO's account is changed only by the CEO (name/email) or with opsctl.
//   - HR accounts, the HR role, the admin flag and analytics access are granted or removed
//     only by the CEO.
//   - Nobody changes their own role, deactivates themselves or drops their own admin flag.
func checkUserChange(actor, target db.User, role *string, isAdmin, analytics, isActive *bool) error {
	isCEO := roleOf(actor) == policy.CEO
	changesAnalytics := analytics != nil && *analytics != target.CanViewAnalytics
	changesAccess := (role != nil && *role != target.Role) || (isAdmin != nil && *isAdmin != target.IsAdmin) ||
		changesAnalytics || (isActive != nil && *isActive != target.IsActive)
	switch {
	case target.ID == actor.ID && changesAccess:
		return errConflict("You cannot change your own role or access")
	case roleOf(target) == policy.CEO && target.ID != actor.ID:
		return errForbidden()
	case roleOf(target) == policy.HR && !isCEO:
		return errForbidden()
	case role != nil && *role == string(policy.HR) && *role != target.Role && !isCEO:
		return errForbidden()
	case isAdmin != nil && *isAdmin != target.IsAdmin && !isCEO:
		return errForbidden()
	case changesAnalytics && !isCEO:
		return errForbidden()
	}
	return nil
}

// ── Users ──────────────────────────────────────────────────

type directoryEntry struct {
	ID     uuid.UUID  `json:"id"`
	Name   string     `json:"name"`
	Role   string     `json:"role"`
	TeamID *uuid.UUID `json:"team_id"`
}

// userDirectory lists active users with only id/name/role, for pickers and display.
func (s *Server) userDirectory(c fiber.Ctx) error {
	active := true
	var role *string
	if r := c.Query("role"); r != "" {
		if !policy.Role(r).Valid() {
			return errValidation(validate.Errors{"role": "is invalid"})
		}
		role = &r
	}
	users, err := s.store.ListUsers(c.Context(), db.ListUsersParams{Role: role, IsActive: &active})
	if err != nil {
		return err
	}
	teams, err := s.store.ListAllTeamMembers(c.Context())
	if err != nil {
		return err
	}
	teamOf := make(map[uuid.UUID]uuid.UUID, len(teams))
	for _, m := range teams {
		teamOf[m.UserID] = m.TeamID
	}
	out := make([]directoryEntry, len(users))
	for i, u := range users {
		out[i] = directoryEntry{ID: u.ID, Name: u.Name, Role: u.Role}
		if t, ok := teamOf[u.ID]; ok {
			out[i].TeamID = &t
		}
	}
	return c.JSON(fiber.Map{"users": out})
}

func (s *Server) adminListUsers(c fiber.Ctx) error {
	users, err := s.store.ListUsers(c.Context(), db.ListUsersParams{})
	if err != nil {
		return err
	}
	return c.JSON(fiber.Map{"users": users})
}

type createUserReq struct {
	Email   string `json:"email"`
	Name    string `json:"name"`
	Role    string `json:"role"`
	IsAdmin bool   `json:"is_admin"`
	// CanViewAnalytics opens the Analytics page to someone other than the CEO.
	CanViewAnalytics bool   `json:"can_view_analytics"`
	Password         string `json:"password"`
}

func (s *Server) adminCreateUser(c fiber.Ctx) error {
	var req createUserReq
	if err := decodeJSON(c, &req); err != nil {
		return err
	}
	req.Email = strings.ToLower(strings.TrimSpace(req.Email))
	req.Name = strings.TrimSpace(req.Name)
	v := validate.Errors{}
	v.Email("email", req.Email)
	v.Len("name", req.Name, 1, 120)
	v.OneOf("role", req.Role, assignableRoles()...)
	validatePassword(v, "password", req.Password)
	if !v.OK() {
		return errValidation(v)
	}
	actor := currentUser(c)
	if (req.Role == string(policy.HR) || req.IsAdmin || req.CanViewAnalytics) && roleOf(actor) != policy.CEO {
		return errForbidden()
	}
	hash, err := auth.HashPassword(req.Password)
	if err != nil {
		return err
	}
	var u db.User
	err = s.store.Tx(c.Context(), func(q *db.Queries) error {
		var err error
		u, err = q.CreateUser(c.Context(), db.CreateUserParams{
			Email: req.Email, Name: req.Name, PasswordHash: hash, Role: req.Role, IsAdmin: req.IsAdmin,
			CanViewAnalytics: req.CanViewAnalytics,
		})
		if err != nil {
			return err
		}
		return store.LogActivity(c.Context(), q, store.Activity{
			Actor: &actor.ID, Action: "user.created", EntityType: "user", EntityID: &u.ID,
			Metadata: map[string]any{"email": u.Email, "role": u.Role, "is_admin": u.IsAdmin, "can_view_analytics": u.CanViewAnalytics},
		})
	})
	if store.IsUniqueViolation(err) {
		return errValidation(validate.Errors{"email": "is already in use"})
	} else if err != nil {
		return err
	}
	return c.Status(fiber.StatusCreated).JSON(fiber.Map{"user": u})
}

type updateUserReq struct {
	Email   *string `json:"email"`
	Name    *string `json:"name"`
	Role    *string `json:"role"`
	IsAdmin *bool   `json:"is_admin"`
	// CanViewAnalytics grants or removes the Analytics page (CEO only).
	CanViewAnalytics *bool   `json:"can_view_analytics"`
	IsActive         *bool   `json:"is_active"`
	Reason           *string `json:"reason"` // required to deactivate; kept in the access log
}

// checkHandover refuses to deactivate someone, or move them out of their role, while
// they still hold work: open projects, a team-lead or department-head seat, or direct reports.
func checkHandover(ctx context.Context, q *db.Queries, target db.User, role *string, isActive *bool) error {
	deactivating := isActive != nil && !*isActive && target.IsActive
	leavingRole := role != nil && *role != target.Role
	if !deactivating && !leavingRole {
		return nil
	}
	w, err := q.OpenWorkOf(ctx, target.ID)
	if err != nil {
		return err
	}
	var held []string
	if w.AssignedProjects > 0 {
		held = append(held, plural(w.AssignedProjects, "open project")+" (reassign them)")
	}
	if w.TeamsLed > 0 {
		held = append(held, "the lead of "+plural(w.TeamsLed, "team")+" (pick a new lead under Teams)")
	}
	if w.DepartmentsHeaded > 0 {
		held = append(held, "the head of "+plural(w.DepartmentsHeaded, "department")+" (pick a new head under Departments)")
	}
	if deactivating && w.DirectReports > 0 {
		held = append(held, plural(w.DirectReports, "direct report")+" (set a new manager on their HR records)")
	}
	if len(held) == 0 {
		return nil
	}
	return errConflict(target.Name + " still has work to hand over first: " + strings.Join(held, "; ") + ".")
}

func plural(n int64, noun string) string {
	if n == 1 {
		return "1 " + noun
	}
	return fmt.Sprintf("%d %ss", n, noun)
}

func (s *Server) adminUpdateUser(c fiber.Ctx) error {
	id, err := paramUUID(c, "id")
	if err != nil {
		return err
	}
	var req updateUserReq
	if err := decodeJSON(c, &req); err != nil {
		return err
	}
	v := validate.Errors{}
	if req.Email != nil {
		*req.Email = strings.ToLower(strings.TrimSpace(*req.Email))
		v.Email("email", *req.Email)
	}
	if req.Name != nil {
		*req.Name = strings.TrimSpace(*req.Name)
		v.Len("name", *req.Name, 1, 120)
	}
	if req.Role != nil {
		v.OneOf("role", *req.Role, assignableRoles()...)
	}
	reason := ""
	if req.Reason != nil {
		reason = strings.TrimSpace(*req.Reason)
		v.MaxLen("reason", reason, 500)
	}
	if !v.OK() {
		return errValidation(v)
	}

	actor := currentUser(c)

	ctx := c.Context()
	var before, after db.User
	err = s.store.Tx(ctx, func(q *db.Queries) error {
		var err error
		if before, err = q.GetUserByID(ctx, id); err != nil {
			return err
		}
		if err := checkUserChange(actor, before, req.Role, req.IsAdmin, req.CanViewAnalytics, req.IsActive); err != nil {
			return err
		}
		if req.IsActive != nil && !*req.IsActive && before.IsActive && reason == "" {
			return errValidation(validate.Errors{"reason": "is required to deactivate someone"})
		}
		if err := checkHandover(ctx, q, before, req.Role, req.IsActive); err != nil {
			return err
		}
		after, err = q.UpdateUser(ctx, db.UpdateUserParams{
			ID: id, Email: req.Email, Name: req.Name, Role: req.Role, IsAdmin: req.IsAdmin, IsActive: req.IsActive,
			CanViewAnalytics: req.CanViewAnalytics,
		})
		if err != nil {
			return err
		}
		if err := store.LogActivity(ctx, q, store.Activity{
			Actor: &actor.ID, Action: "user.updated", EntityType: "user", EntityID: &id,
			Metadata: userDiff(before, after),
		}); err != nil {
			return err
		}
		// Access audit: who granted or revoked whose access, when and why.
		if before.IsActive != after.IsActive || before.Role != after.Role || before.IsAdmin != after.IsAdmin ||
			before.CanViewAnalytics != after.CanViewAnalytics {
			action := "user.access_changed"
			if before.IsActive && !after.IsActive {
				action = "user.access_revoked"
			} else if !before.IsActive && after.IsActive {
				action = "user.access_restored"
			}
			return store.LogActivity(ctx, q, store.Activity{
				Actor: &actor.ID, Action: action, EntityType: "user", EntityID: &id,
				Metadata: map[string]any{"name": after.Name, "reason": reason, "changes": userDiff(before, after)},
			})
		}
		return nil
	})
	switch {
	case store.IsNotFound(err):
		return errNotFound("User")
	case store.IsUniqueViolation(err):
		return errValidation(validate.Errors{"email": "is already in use"})
	case err != nil:
		return err
	}
	// Deactivation or a role change takes effect on the next request anyway
	// (requireAuth reloads the user); deactivation also kills live sessions.
	if before.IsActive && !after.IsActive {
		if err := s.sessions.DestroyAllForUser(ctx, id, ""); err != nil {
			return err
		}
	}
	return c.JSON(fiber.Map{"user": after})
}

func userDiff(a, b db.User) map[string][2]any {
	d := map[string][2]any{}
	if a.Email != b.Email {
		d["email"] = [2]any{a.Email, b.Email}
	}
	if a.Name != b.Name {
		d["name"] = [2]any{a.Name, b.Name}
	}
	if a.Role != b.Role {
		d["role"] = [2]any{a.Role, b.Role}
	}
	if a.IsAdmin != b.IsAdmin {
		d["is_admin"] = [2]any{a.IsAdmin, b.IsAdmin}
	}
	if a.CanViewAnalytics != b.CanViewAnalytics {
		d["can_view_analytics"] = [2]any{a.CanViewAnalytics, b.CanViewAnalytics}
	}
	if a.IsActive != b.IsActive {
		d["is_active"] = [2]any{a.IsActive, b.IsActive}
	}
	return d
}

type resetPasswordReq struct {
	Password string `json:"password"`
}

func (s *Server) adminResetPassword(c fiber.Ctx) error {
	id, err := paramUUID(c, "id")
	if err != nil {
		return err
	}
	var req resetPasswordReq
	if err := decodeJSON(c, &req); err != nil {
		return err
	}
	v := validate.Errors{}
	validatePassword(v, "password", req.Password)
	if !v.OK() {
		return errValidation(v)
	}
	target, err := s.store.GetUserByID(c.Context(), id)
	if store.IsNotFound(err) {
		return errNotFound("User")
	} else if err != nil {
		return err
	}
	// The CEO changes their own password on the Account page; HR passwords are reset by the CEO.
	switch actor := currentUser(c); {
	case roleOf(target) == policy.CEO, roleOf(target) == policy.HR && roleOf(actor) != policy.CEO:
		return errForbidden()
	}
	if err := s.setPassword(c.Context(), id, req.Password, ""); err != nil {
		return err
	}
	actor := currentUser(c)
	_ = store.LogActivity(c.Context(), s.store.Queries, store.Activity{
		Actor: &actor.ID, Action: "user.password_reset", EntityType: "user", EntityID: &id,
	})
	return c.SendStatus(fiber.StatusNoContent)
}

// ── Lookups ────────────────────────────────────────────────

func (s *Server) listProfiles(c fiber.Ctx) error {
	rows, err := s.store.ListProfiles(c.Context(), true)
	if err != nil {
		return err
	}
	return c.JSON(fiber.Map{"profiles": rows})
}

func (s *Server) adminListProfiles(c fiber.Ctx) error {
	rows, err := s.store.ListProfiles(c.Context(), false)
	if err != nil {
		return err
	}
	return c.JSON(fiber.Map{"profiles": rows})
}

type profileReq struct {
	Name     *string `json:"name"`
	Platform *string `json:"platform"`
	IsActive *bool   `json:"is_active"`
	// The marketplace's share of each order, in percent (Fiverr keeps 20).
	FeePercent *float64 `json:"fee_percent"`
}

func (r *profileReq) fee() *string {
	if r.FeePercent == nil {
		return nil
	}
	s := strconv.FormatFloat(*r.FeePercent, 'f', 2, 64)
	return &s
}

func (r *profileReq) validate(create bool) validate.Errors {
	v := validate.Errors{}
	if r.Name != nil {
		*r.Name = strings.TrimSpace(*r.Name)
		v.Len("name", *r.Name, 1, 120)
	} else if create {
		v.Add("name", "is required")
	}
	if r.Platform != nil {
		*r.Platform = strings.TrimSpace(*r.Platform)
		v.MaxLen("platform", *r.Platform, 60)
	}
	if r.FeePercent != nil && (*r.FeePercent < 0 || *r.FeePercent > 100) {
		v.Add("fee_percent", "must be between 0 and 100")
	}
	return v
}

func (s *Server) adminCreateProfile(c fiber.Ctx) error {
	var req profileReq
	if err := decodeJSON(c, &req); err != nil {
		return err
	}
	if v := req.validate(true); !v.OK() {
		return errValidation(v)
	}
	platform := ""
	if req.Platform != nil {
		platform = *req.Platform
	}
	p, err := s.store.CreateProfile(c.Context(), db.CreateProfileParams{Name: *req.Name, Platform: platform, FeePercent: req.fee()})
	if store.IsUniqueViolation(err) {
		return errValidation(validate.Errors{"name": "already exists"})
	} else if err != nil {
		return err
	}
	s.logAdmin(c, "profile.created", "marketplace_profile", p.ID, p)
	return c.Status(fiber.StatusCreated).JSON(fiber.Map{"profile": p})
}

func (s *Server) adminUpdateProfile(c fiber.Ctx) error {
	id, err := paramUUID(c, "id")
	if err != nil {
		return err
	}
	var req profileReq
	if err := decodeJSON(c, &req); err != nil {
		return err
	}
	if v := req.validate(false); !v.OK() {
		return errValidation(v)
	}
	p, err := s.store.UpdateProfile(c.Context(), db.UpdateProfileParams{
		ID: id, Name: req.Name, Platform: req.Platform, FeePercent: req.fee(), IsActive: req.IsActive,
	})
	switch {
	case store.IsNotFound(err):
		return errNotFound("Profile")
	case store.IsUniqueViolation(err):
		return errValidation(validate.Errors{"name": "already exists"})
	case err != nil:
		return err
	}
	s.logAdmin(c, "profile.updated", "marketplace_profile", p.ID, req)
	return c.JSON(fiber.Map{"profile": p})
}

func (s *Server) listProjectTypes(c fiber.Ctx) error {
	rows, err := s.store.ListProjectTypes(c.Context(), true)
	if err != nil {
		return err
	}
	return c.JSON(fiber.Map{"project_types": rows})
}

func (s *Server) adminListProjectTypes(c fiber.Ctx) error {
	rows, err := s.store.ListProjectTypes(c.Context(), false)
	if err != nil {
		return err
	}
	return c.JSON(fiber.Map{"project_types": rows})
}

type projectTypeReq struct {
	Name     *string    `json:"name"`
	IsActive *bool      `json:"is_active"`
	TeamID   nullableID `json:"team_id"` // the team its projects are routed to
}

func (s *Server) adminCreateProjectType(c fiber.Ctx) error {
	var req projectTypeReq
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
	if err := s.checkTeam(c.Context(), v, req.TeamID.ID); err != nil {
		return err
	}
	if !v.OK() {
		return errValidation(v)
	}
	t, err := s.store.CreateProjectType(c.Context(), db.CreateProjectTypeParams{Name: *req.Name, TeamID: req.TeamID.ID})
	if store.IsUniqueViolation(err) {
		return errValidation(validate.Errors{"name": "already exists"})
	} else if err != nil {
		return err
	}
	s.logAdmin(c, "project_type.created", "project_type", t.ID, t)
	return c.Status(fiber.StatusCreated).JSON(fiber.Map{"project_type": t})
}

func (s *Server) adminUpdateProjectType(c fiber.Ctx) error {
	id, err := paramUUID(c, "id")
	if err != nil {
		return err
	}
	var req projectTypeReq
	if err := decodeJSON(c, &req); err != nil {
		return err
	}
	v := validate.Errors{}
	if req.Name != nil {
		*req.Name = strings.TrimSpace(*req.Name)
		v.Len("name", *req.Name, 1, 120)
	}
	if err := s.checkTeam(c.Context(), v, req.TeamID.ID); err != nil {
		return err
	}
	if !v.OK() {
		return errValidation(v)
	}
	var t db.ProjectType
	err = s.store.Tx(c.Context(), func(q *db.Queries) error {
		// Where the type sent projects before, so hand-moved ones stay put.
		var oldTeam *uuid.UUID
		if r, err := q.RouteTeamForType(c.Context(), id); err == nil {
			oldTeam = &r
		} else if !store.IsNotFound(err) {
			return err
		}
		var err error
		t, err = q.UpdateProjectType(c.Context(), db.UpdateProjectTypeParams{
			ID: id, Name: req.Name, IsActive: req.IsActive, SetTeam: req.TeamID.Set, TeamID: req.TeamID.ID,
		})
		if err != nil || !req.TeamID.Set {
			return err
		}
		_, err = q.RerouteWaitingProjects(c.Context(), db.RerouteWaitingProjectsParams{ID: id, OldTeam: oldTeam})
		return err
	})
	switch {
	case store.IsNotFound(err):
		return errNotFound("Project type")
	case store.IsUniqueViolation(err):
		return errValidation(validate.Errors{"name": "already exists"})
	case err != nil:
		return err
	}
	s.logAdmin(c, "project_type.updated", "project_type", t.ID, req)
	return c.JSON(fiber.Map{"project_type": t})
}

// ── Settings ───────────────────────────────────────────────

// listSettings returns the app settings map. Sprint 1 (P0-7): role-filtered,
// not open to every caller. The full map is privileged (CEO, HR, system
// admins) — it carries org config such as office_network IPs. Other internal
// roles, including office_admin, get only office_location, which the member
// check-in card needs to know whether geofencing is on. Client and investor
// callers are blocked outright.
func (s *Server) listSettings(c fiber.Ctx) error {
	u := currentUser(c)
	privileged := isSystemAdmin(u) || roleOf(u) == policy.HR
	internal := canViewInternal(u) || roleOf(u) == policy.OfficeAdmin
	if !privileged && !internal {
		return errForbidden()
	}
	rows, err := s.store.ListSettings(c.Context())
	if err != nil {
		return err
	}
	out := make(map[string]json.RawMessage, len(rows))
	for _, r := range rows {
		if privileged || r.Key == "office_location" {
			out[r.Key] = r.Value
		}
	}
	return c.JSON(fiber.Map{"settings": out})
}

// settingValidators define the allowed keys and value shapes.
var settingValidators = map[string]func(json.RawMessage) bool{
	"inbox_requires_approval": func(raw json.RawMessage) bool {
		var b bool
		return json.Unmarshal(raw, &b) == nil
	},
	"org_timezone": func(raw json.RawMessage) bool {
		var tz string
		if json.Unmarshal(raw, &tz) != nil || tz == "" {
			return false
		}
		_, err := time.LoadLocation(tz)
		return err == nil
	},
	"batch_window_minutes": func(raw json.RawMessage) bool {
		var n int
		return json.Unmarshal(raw, &n) == nil && n >= 1 && n <= 120
	},
	"attendance_start": func(raw json.RawMessage) bool {
		var d string
		if json.Unmarshal(raw, &d) != nil {
			return false
		}
		_, err := hr.ParseDate(d)
		return err == nil
	},
	"work_start": validClock,
	"work_end":   validClock,
	"late_grace_minutes": func(raw json.RawMessage) bool {
		var n int
		return json.Unmarshal(raw, &n) == nil && n >= 0 && n <= 240
	},
	"weekly_off_days": func(raw json.RawMessage) bool {
		var days []int
		if json.Unmarshal(raw, &days) != nil || len(days) > 6 {
			return false
		}
		seen := map[int]bool{}
		for _, d := range days {
			if d < 0 || d > 6 || seen[d] {
				return false
			}
			seen[d] = true
		}
		return true
	},
	"office_location": func(raw json.RawMessage) bool {
		var o hr.OfficeLocation
		return json.Unmarshal(raw, &o) == nil && o.Valid()
	},
	"office_network": func(raw json.RawMessage) bool {
		var o hr.OfficeNetwork
		return json.Unmarshal(raw, &o) == nil && o.Valid()
	},
}

func validClock(raw json.RawMessage) bool {
	var c string
	if json.Unmarshal(raw, &c) != nil {
		return false
	}
	_, err := hr.ParseClock(c)
	return err == nil
}

type settingReq struct {
	Value json.RawMessage `json:"value"`
}

// hrSettings are managed by HR and the CEO; every other setting by system admins.
var hrSettings = map[string]bool{
	"work_start": true, "work_end": true, "late_grace_minutes": true, "weekly_off_days": true, "attendance_start": true,
	"office_location": true, "office_network": true,
}

func (s *Server) adminUpdateSetting(c fiber.Ctx) error {
	key := c.Params("key")
	u := currentUser(c)
	if hrSettings[key] && !policy.ManagesPeople(roleOf(u)) || !hrSettings[key] && !isSystemAdmin(u) {
		return errForbidden()
	}
	check, ok := settingValidators[key]
	if !ok {
		return errNotFound("Setting")
	}
	var req settingReq
	if err := decodeJSON(c, &req); err != nil {
		return err
	}
	if !check(req.Value) {
		return errValidation(validate.Errors{"value": "is invalid for " + key})
	}
	actor := currentUser(c)
	var row db.Setting
	err := s.store.Tx(c.Context(), func(q *db.Queries) error {
		before, err := q.GetSetting(c.Context(), key)
		if err != nil {
			return err
		}
		row, err = q.UpdateSetting(c.Context(), db.UpdateSettingParams{Key: key, Value: req.Value, UpdatedBy: &actor.ID})
		if err != nil {
			return err
		}
		return store.LogActivity(c.Context(), q, store.Activity{
			Actor: &actor.ID, Action: "setting.updated", EntityType: "setting",
			Metadata: map[string]any{"key": key, "from": before.Value, "to": row.Value},
		})
	})
	if err != nil {
		return err
	}
	return c.JSON(fiber.Map{"setting": row})
}

func (s *Server) logAdmin(c fiber.Ctx, action, entity string, id uuid.UUID, meta any) {
	actor := currentUser(c)
	_ = store.LogActivity(c.Context(), s.store.Queries, store.Activity{
		Actor: &actor.ID, Action: action, EntityType: entity, EntityID: &id, Metadata: meta,
	})
}
