// Package projects implements project creation, editing, visibility, the status
// workflow and assignment. Every permission decision is made here, server-side.
package projects

import (
	"context"
	"encoding/json"
	"errors"
	"slices"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"

	"github.com/solvinx/opsflow/internal/apperr"
	"github.com/solvinx/opsflow/internal/db"
	"github.com/solvinx/opsflow/internal/policy"
	"github.com/solvinx/opsflow/internal/storage"
	"github.com/solvinx/opsflow/internal/store"
	"github.com/solvinx/opsflow/internal/validate"
	"github.com/solvinx/opsflow/internal/workflow"
)

type Service struct {
	st        *store.Store
	files     *storage.Storage
	rdb       *redis.Client
	maxUpload int64
}

func NewService(st *store.Store, files *storage.Storage, rdb *redis.Client, maxUpload int64) *Service {
	return &Service{st: st, files: files, rdb: rdb, maxUpload: maxUpload}
}

// ── Visibility ─────────────────────────────────────────────

// actorFor: IsAssignee means "one of the project's assigned members".
func actorFor(u db.User, createdBy uuid.UUID, members []uuid.UUID) workflow.Actor {
	return workflow.Actor{
		Role:       policy.Role(u.Role),
		IsOwner:    createdBy == u.ID,
		IsAssignee: slices.Contains(members, u.ID),
	}
}

// canView: Sales (one team, covering for each other) see every project but other people's
// drafts; a TL their team's projects (and any project with no
// team or whose team has no active lead) except drafts; members only projects they are
// assigned to now; the CEO everything. teamLead is the team's active lead, nil if none;
// teamHead is the active head of the team's department, who sees it like its lead.
func canView(u db.User, status string, createdBy uuid.UUID, members []uuid.UUID, teamID, teamLead, teamHead *uuid.UUID) bool {
	switch policy.Role(u.Role) {
	case policy.Sales:
		return createdBy == u.ID || status != string(workflow.Draft)
	case policy.TL:
		return status != string(workflow.Draft) &&
			(teamID == nil || teamLead == nil || *teamLead == u.ID || (teamHead != nil && *teamHead == u.ID))
	case policy.Member:
		return status != string(workflow.Draft) && slices.Contains(members, u.ID)
	case policy.CEO:
		return true
	}
	return false
}

// lockedProject is a locked project row with its assigned members.
type lockedProject struct {
	db.Project
	Members []uuid.UUID
}

// canSee is canView for a locked project row, looking up its team's lead and
// department head when it matters.
func canSee(ctx context.Context, q *db.Queries, u db.User, p lockedProject) (bool, error) {
	var lead, head *uuid.UUID
	if p.TeamID != nil && policy.Role(u.Role) == policy.TL {
		var err error
		if lead, err = optionalID(q.ActiveTeamLead(ctx, *p.TeamID)); err != nil {
			return false, err
		}
		if head, err = optionalID(q.ActiveTeamHead(ctx, *p.TeamID)); err != nil {
			return false, err
		}
	}
	return canView(u, p.Status, p.CreatedBy, p.Members, p.TeamID, lead, head), nil
}

// optionalID turns a single-id lookup into nil when there is no row.
func optionalID(id uuid.UUID, err error) (*uuid.UUID, error) {
	if store.IsNotFound(err) {
		return nil, nil
	} else if err != nil {
		return nil, err
	}
	return &id, nil
}

// lockVisible locks a project the user may see, or 404s (never revealing it exists).
func lockVisible(ctx context.Context, q *db.Queries, u db.User, id uuid.UUID) (lockedProject, error) {
	var row lockedProject
	p, err := q.LockProject(ctx, id)
	if store.IsNotFound(err) {
		return row, apperr.NotFound("Project")
	} else if err != nil {
		return row, err
	}
	members, err := q.ProjectMemberIDs(ctx, id)
	if err != nil {
		return row, err
	}
	row = lockedProject{Project: p, Members: members}
	ok, err := canSee(ctx, q, u, row)
	if err != nil {
		return row, err
	} else if !ok {
		return row, apperr.NotFound("Project")
	}
	return row, nil
}

func canComment(u db.User, a workflow.Actor) bool {
	switch a.Role {
	case policy.Sales:
		return true // any Sales user, so a colleague can answer while the owner is away
	case policy.TL:
		return true
	case policy.Member:
		return a.IsAssignee
	}
	return false
}

// ── Time ───────────────────────────────────────────────────

// today returns the current date in the organisation's timezone.
func (s *Service) today(ctx context.Context) string {
	return time.Now().In(s.location(ctx)).Format(time.DateOnly)
}

// location is the organisation's timezone: the one deadlines and due dates are written in.
func (s *Service) location(ctx context.Context) *time.Location {
	if row, err := s.st.GetSetting(ctx, "org_timezone"); err == nil {
		var tz string
		if json.Unmarshal(row.Value, &tz) == nil {
			if l, err := time.LoadLocation(tz); err == nil {
				return l
			}
		}
	}
	return time.UTC
}

// normaliseDeadline turns what the browser sends ("2026-11-18T15:30", or a plain
// "2026-11-18" meaning the end of that day) into an exact moment in the org timezone.
func (s *Service) normaliseDeadline(ctx context.Context, f *Fields) error {
	if f.Deadline == nil {
		return nil
	}
	t, err := parseDeadlineInput(*f.Deadline, s.location(ctx))
	if err != nil {
		return apperr.Validation(validate.Errors{"deadline": deadlineFormatHint})
	}
	// Same shape the database renders (ISO 8601 UTC), so a reformat never looks like a change.
	iso := t.UTC().Format(time.RFC3339)
	f.Deadline = &iso
	return nil
}

// isOverdue: the effective due moment (the TL's due date at end of day, else the
// delivery deadline) has passed.
func isOverdue(status string, effectiveDue *string, now time.Time) bool {
	switch workflow.Status(status) {
	case workflow.Delivered, workflow.Completed, workflow.Cancelled:
		return false
	}
	t, err := parseMoment(effectiveDue)
	return err == nil && t.Before(now)
}

// parseMoment reads a timestamp the database rendered as text.
func parseMoment(s *string) (time.Time, error) {
	if s == nil {
		return time.Time{}, errNoMoment
	}
	return time.Parse(timestampLayout, *s)
}

var errNoMoment = errors.New("no moment")

// timestampLayout is how the database renders timestamps (see the iso() SQL function).
const timestampLayout = time.RFC3339

// ── Read ───────────────────────────────────────────────────

type ProjectOut struct {
	db.ProjectDetail
	IsOverdue  bool `json:"is_overdue"`
	HasUpdates bool `json:"has_updates"` // changed by someone else since this user last opened it
}

type ListFilter struct {
	Team     *uuid.UUID
	Statuses []string
	Assignee *uuid.UUID
	Priority *string
	Q        *string
	Overdue  bool
	Sort     string
	Page     int
	PageSize int
}

type ListResult struct {
	Projects []ProjectOut     `json:"projects"`
	Total    int64            `json:"total"`
	Counts   map[string]int64 `json:"counts"` // by status, within the user's scope
	Page     int              `json:"page"`
	PageSize int              `json:"page_size"`
}

// listScope narrows a list to what the role may see (nil fields don't restrict).
type listScope struct {
	creator, sales, assignee, lead *uuid.UUID
	hideDrafts                     bool
}

func scopeFor(u db.User) listScope {
	switch policy.Role(u.Role) {
	case policy.Sales:
		return listScope{sales: &u.ID} // every project, only their own drafts
	case policy.TL:
		return listScope{lead: &u.ID, hideDrafts: true}
	case policy.Member:
		return listScope{assignee: &u.ID, hideDrafts: true}
	case policy.CEO:
		return listScope{}
	}
	// Unknown role: match nothing.
	nobody := uuid.Nil
	return listScope{creator: &nobody, assignee: &nobody, hideDrafts: true}
}

func (s *Service) List(ctx context.Context, u db.User, f ListFilter) (ListResult, error) {
	sc := scopeFor(u)
	rows, err := s.st.ListProjectDetails(ctx, db.ListProjectDetailsParams{
		Viewer:       u.ID,
		ScopeCreator: sc.creator, ScopeSales: sc.sales, ScopeAssignee: sc.assignee, ScopeLead: sc.lead, HideDrafts: sc.hideDrafts,
		Team: f.Team, Statuses: f.Statuses, Assignee: f.Assignee, Priority: f.Priority, Q: f.Q,
		Overdue: f.Overdue, Sort: f.Sort,
		Lim: int32(f.PageSize), Off: int32((f.Page - 1) * f.PageSize),
	})
	if err != nil {
		return ListResult{}, err
	}
	counts, err := s.st.CountProjectsByStatus(ctx, db.CountProjectsByStatusParams{
		ScopeCreator: sc.creator, ScopeSales: sc.sales, ScopeAssignee: sc.assignee, ScopeLead: sc.lead, HideDrafts: sc.hideDrafts,
	})
	if err != nil {
		return ListResult{}, err
	}
	now := time.Now()
	res := ListResult{Projects: make([]ProjectOut, len(rows)), Counts: map[string]int64{}, Page: f.Page, PageSize: f.PageSize}
	for i, r := range rows {
		res.Total = r.Total
		d := r.ProjectDetail
		res.Projects[i] = ProjectOut{ProjectDetail: d, IsOverdue: isOverdue(d.Status, d.EffectiveDue, now), HasUpdates: r.HasUpdates}
	}
	for _, c := range counts {
		res.Counts[c.Status] = c.N
	}
	return res, nil
}

type Permissions struct {
	CanEdit        bool `json:"can_edit"`
	CanSetPriority bool `json:"can_set_priority"` // Sales, before assignment
	CanAssign      bool `json:"can_assign"`
	CanMoveTeam    bool `json:"can_move_team"`
	CanPlan        bool `json:"can_plan"`
	CanComment     bool `json:"can_comment"`
	CanUpload      bool `json:"can_upload"`       // Sales owner and assignees, until Completed/Cancelled
	CanRemoveFiles bool `json:"can_remove_files"` // any file (Sales owner); assignees only their own
	CanAcknowledge bool `json:"can_acknowledge"`  // assigned member with pending important changes
	CanSetAccess   bool `json:"can_set_access"`   // store access: Sales, the project's TLs and its assignees
}

type DetailResult struct {
	Project     ProjectOut                         `json:"project"`
	Permissions Permissions                        `json:"permissions"`
	Actions     []workflow.Rule                    `json:"actions"`
	History     []db.ListStatusHistoryRow          `json:"history"`
	Assignments []db.ListAssignmentsRow            `json:"assignments"`
	Comments    []db.ListProjectCommentsRow        `json:"comments"`
	Changes     []db.ListProjectChangesRow         `json:"changes"`
	Files       []db.ListProjectFilesRow           `json:"files"`
	Versions    []db.ListFileVersionsForProjectRow `json:"versions"`
	// Acknowledgements of important changes; the TL and Management see who has acknowledged.
	Acknowledgements []db.ListProjectAcknowledgementsRow `json:"acknowledgements"`
	PendingAcks      int                                 `json:"pending_acks"` // for the current user
}

// load fetches a project the user may see, or 404s (never revealing it exists).
func (s *Service) load(ctx context.Context, q *db.Queries, u db.User, id uuid.UUID) (db.ProjectDetail, error) {
	p, err := q.GetProjectDetail(ctx, id)
	if store.IsNotFound(err) || (err == nil && !canView(u, p.Status, p.CreatedBy, p.MemberIDs, p.TeamID, p.TeamLeadID, p.TeamHeadID)) {
		return p, apperr.NotFound("Project")
	}
	return p, err
}

func (s *Service) Get(ctx context.Context, u db.User, id uuid.UUID) (DetailResult, error) {
	p, err := s.load(ctx, s.st.Queries, u, id)
	if err != nil {
		return DetailResult{}, err
	}
	a := actorFor(u, p.CreatedBy, p.MemberIDs)
	st := workflow.Status(p.Status)
	res := DetailResult{
		Project: ProjectOut{ProjectDetail: p, IsOverdue: isOverdue(p.Status, p.EffectiveDue, time.Now())},
		Permissions: Permissions{
			CanEdit:        workflow.CanEditDetails(st, a),
			CanSetPriority: workflow.CanEditDetails(st, a) && workflow.SalesSetsPriority(st),
			CanAssign:      workflow.CanAssign(st, a),
			CanMoveTeam:    workflow.CanMoveTeam(st, a),
			CanPlan:        workflow.CanPlan(st, a),
			CanComment:     canComment(u, a),
			CanUpload:      workflow.CanManageFiles(st, a),
			CanRemoveFiles: workflow.CanRemoveAnyFile(st, a),
			CanSetAccess:   canSetStoreAccess(u, a, p.Status),
		},
		Actions: workflow.Available(st, a),
	}
	if res.Actions == nil {
		res.Actions = []workflow.Rule{}
	}
	if res.History, err = s.st.ListStatusHistory(ctx, id); err != nil {
		return res, err
	}
	if res.Assignments, err = s.st.ListAssignments(ctx, id); err != nil {
		return res, err
	}
	if res.Comments, err = s.st.ListProjectComments(ctx, id); err != nil {
		return res, err
	}
	if res.Changes, err = s.st.ListProjectChanges(ctx, id); err != nil {
		return res, err
	}
	if res.Files, err = s.st.ListProjectFiles(ctx, &id); err != nil {
		return res, err
	}
	if res.Versions, err = s.st.ListFileVersionsForProject(ctx, &id); err != nil {
		return res, err
	}
	if res.Acknowledgements, err = s.st.ListProjectAcknowledgements(ctx, id); err != nil {
		return res, err
	}
	for _, ack := range res.Acknowledgements {
		if ack.UserID == u.ID && ack.AcknowledgedAt == nil {
			res.PendingAcks++
		}
	}
	res.Permissions.CanAcknowledge = res.PendingAcks > 0 && a.Works()
	return res, nil
}

// MarkViewed records that the user opened the project and returns when they
// last did before now (nil on first visit), so the UI can mark what's new.
func (s *Service) MarkViewed(ctx context.Context, u db.User, id uuid.UUID) (*time.Time, error) {
	if _, err := s.load(ctx, s.st.Queries, u, id); err != nil {
		return nil, err
	}
	var prev *time.Time
	err := s.st.Tx(ctx, func(q *db.Queries) error {
		t, err := q.GetLastViewed(ctx, db.GetLastViewedParams{UserID: u.ID, ProjectID: id})
		if err == nil {
			prev = &t
		} else if !store.IsNotFound(err) {
			return err
		}
		return q.MarkProjectViewed(ctx, db.MarkProjectViewedParams{UserID: u.ID, ProjectID: id})
	})
	return prev, err
}

// Acknowledge marks every pending important change on the project as seen by the assigned member.
func (s *Service) Acknowledge(ctx context.Context, u db.User, id uuid.UUID) error {
	return s.st.Tx(ctx, func(q *db.Queries) error {
		p, err := s.load(ctx, q, u, id)
		if err != nil {
			return err
		}
		a := actorFor(u, p.CreatedBy, p.MemberIDs)
		if !a.Works() {
			return apperr.Forbidden()
		}
		ids, err := q.AcknowledgeAll(ctx, db.AcknowledgeAllParams{ProjectID: id, UserID: u.ID})
		if err != nil {
			return err
		}
		if len(ids) == 0 {
			return apperr.Conflict("There is nothing to acknowledge")
		}
		return store.LogActivity(ctx, q, store.Activity{
			Actor: &u.ID, Action: "project.changes_acknowledged", EntityType: "project", EntityID: &id, ProjectID: &id,
			Metadata: map[string]any{"count": len(ids)},
		})
	})
}

// recordChange stores one visible change and, when it is important, asks every
// assigned member to acknowledge it.
func recordChange(ctx context.Context, q *db.Queries, projectID, by uuid.UUID, members []uuid.UUID, field string, oldV, newV *string, important bool) error {
	changeID, err := q.InsertProjectChange(ctx, db.InsertProjectChangeParams{
		ProjectID: projectID, ChangedBy: by, Field: field, OldValue: oldV, NewValue: newV, IsImportant: important,
	})
	if err != nil {
		return err
	}
	if !important {
		return nil
	}
	for _, m := range members {
		if err := q.CreateAcknowledgement(ctx, db.CreateAcknowledgementParams{ProjectChangeID: changeID, UserID: m}); err != nil {
			return err
		}
	}
	return nil
}

// ── Create & edit ──────────────────────────────────────────

// Create makes a Draft owned by the Sales user. Partial data is fine for drafts.
func (s *Service) Create(ctx context.Context, u db.User, raw map[string]json.RawMessage) (uuid.UUID, error) {
	if !policy.Can(policy.Role(u.Role), policy.ProjectCreate) {
		return uuid.Nil, apperr.Forbidden()
	}
	f := defaultFields()
	if err := applyPatch(&f, raw); err != nil {
		return uuid.Nil, err
	}
	if err := s.normaliseDeadline(ctx, &f); err != nil {
		return uuid.Nil, err
	}
	// No drafts: a new project goes straight to its team, so it needs everything up front.
	if err := s.validate(ctx, s.st.Queries, f, Fields{}, true); err != nil {
		return uuid.Nil, err
	}
	var id uuid.UUID
	err := s.st.Tx(ctx, func(q *db.Queries) error {
		var err error
		id, err = q.CreateProject(ctx, db.CreateProjectParams{
			Name: f.Name, ProfileID: f.ProfileID, ClientName: f.ClientName, OrderID: f.OrderID,
			ClientContact: f.ClientContact, ProjectTypeID: f.ProjectTypeID, Scope: f.Scope,
			AgreedPrice: f.AgreedPrice, Currency: f.Currency,
			StartDate: f.StartDate, Deadline: f.Deadline, Priority: f.Priority,
			SpecialNotes: f.SpecialNotes, CreatedBy: u.ID,
		})
		if err != nil {
			return err
		}
		if err := route(ctx, q, id, f.ProjectTypeID); err != nil {
			return err
		}
		if err := q.InsertStatusHistory(ctx, db.InsertStatusHistoryParams{
			ProjectID: id, ToStatus: string(workflow.New), ChangedBy: u.ID,
		}); err != nil {
			return err
		}
		p, err := q.LockProject(ctx, id)
		if err != nil {
			return err
		}
		if err := s.notifyTransition(ctx, q, u, lockedProject{Project: p}, "", workflow.New, ""); err != nil {
			return err
		}
		return store.LogActivity(ctx, q, store.Activity{
			Actor: &u.ID, Action: "project.created", EntityType: "project", EntityID: &id, ProjectID: &id,
		})
	})
	return id, mapUnique(err)
}

// validate checks shape, lookups (only when changed, so an old project can keep
// a since-deactivated profile) and, when required, the submit fields.
func (s *Service) validate(ctx context.Context, q *db.Queries, f, before Fields, required bool) error {
	v := validateShape(f)
	changedProfile := f.ProfileID != nil && (before.ProfileID == nil || *before.ProfileID != *f.ProfileID)
	changedType := f.ProjectTypeID != nil && (before.ProjectTypeID == nil || *before.ProjectTypeID != *f.ProjectTypeID)
	if changedProfile || changedType {
		ok, err := q.GetActiveLookup(ctx, db.GetActiveLookupParams{ProfileID: f.ProfileID, ProjectTypeID: f.ProjectTypeID})
		if err != nil {
			return err
		}
		if changedProfile && !ok.ProfileOk {
			v.Add("profile_id", "is not an active profile")
		}
		if changedType && !ok.TypeOk {
			v.Add("project_type_id", "is not an active project type")
		}
	}
	if required {
		for k, msg := range requiredForSubmit(f) {
			v.Add(k, msg)
		}
	}
	if !v.OK() {
		return apperr.Validation(v)
	}
	return nil
}

func mapUnique(err error) error {
	if store.IsUniqueViolation(err) {
		return apperr.Validation(validate.Errors{"order_id": "is already used by another project on this profile"})
	}
	return err
}

// Update applies a partial edit by the owning Sales user. After submission the
// required fields must stay filled, and every change is recorded field by field.
func (s *Service) Update(ctx context.Context, u db.User, id uuid.UUID, raw map[string]json.RawMessage) error {
	var lockVersion *int32
	if lv, ok := raw["lock_version"]; ok {
		var n int32
		if json.Unmarshal(lv, &n) != nil {
			return apperr.Validation(validate.Errors{"lock_version": "must be a number"})
		}
		lockVersion = &n
	}
	return mapUnique(s.st.Tx(ctx, func(q *db.Queries) error {
		row, err := lockVisible(ctx, q, u, id)
		if err != nil {
			return err
		}
		st := workflow.Status(row.Status)
		a := actorFor(u, row.CreatedBy, row.Members)
		if !workflow.CanEditDetails(st, a) {
			if st.Terminal() && a.IsOwner {
				return apperr.Conflict("This project is " + st.Label() + " and can no longer be edited")
			}
			return apperr.Forbidden()
		}
		if lockVersion != nil && *lockVersion != row.LockVersion {
			return apperr.Conflict("Someone else changed this project. Reload to see the latest version.")
		}
		cur, err := q.GetProjectDetail(ctx, id)
		if err != nil {
			return err
		}
		before := fieldsFromDetail(cur)
		after := before
		if err := applyPatch(&after, raw); err != nil {
			return err
		}
		if err := s.normaliseDeadline(ctx, &after); err != nil {
			return err
		}
		if !workflow.SalesSetsPriority(st) && after.Priority != before.Priority {
			return apperr.Validation(validate.Errors{"priority": "is set by the Team Leader once the project is assigned"})
		}
		if err := s.validate(ctx, q, after, before, true); err != nil {
			return err
		}
		changes := diff(before, after)
		if len(changes) == 0 {
			return nil
		}
		if err := q.UpdateProjectDetails(ctx, db.UpdateProjectDetailsParams{
			ID: id, Name: after.Name, ProfileID: after.ProfileID, ClientName: after.ClientName,
			OrderID: after.OrderID, ClientContact: after.ClientContact, ProjectTypeID: after.ProjectTypeID,
			Scope: after.Scope, AgreedPrice: after.AgreedPrice, Currency: after.Currency,
			StartDate: after.StartDate, Deadline: after.Deadline, Priority: after.Priority, SpecialNotes: after.SpecialNotes,
		}); err != nil {
			return err
		}
		// A new type re-routes a project that is still waiting for assignment, unless a
		// Team Leader moved it to a team other than its old type's.
		if !sameID(after.ProjectTypeID, before.ProjectTypeID) && waitingForTeam(st, row.AssignedTo) {
			routed, err := routedByType(ctx, q, row.TeamID, before.ProjectTypeID)
			if err != nil {
				return err
			}
			if routed {
				if err := route(ctx, q, id, after.ProjectTypeID); err != nil {
					return err
				}
			}
		}
		// Drafts are private scratch work; changes are only tracked once others can see the project.
		if st != workflow.Draft {
			names, err := lookupNames(ctx, q)
			if err != nil {
				return err
			}
			var important, other []string
			for _, c := range changes {
				oldV, newV := display(c.Old, names), display(c.New, names)
				if err := recordChange(ctx, q, id, u.ID, row.Members, c.Field, oldV, newV, importantFields[c.Field]); err != nil {
					return err
				}
				line := changeLine(c.Field, oldV, newV, after.Currency, s.location(ctx))
				if importantFields[c.Field] {
					important = append(important, line)
				} else {
					other = append(other, line)
				}
			}
			if err := s.notifySalesEdit(ctx, q, u, cur, important, other); err != nil {
				return err
			}
		}
		fieldsChanged := make([]string, len(changes))
		for i, c := range changes {
			fieldsChanged[i] = c.Field
		}
		return store.LogActivity(ctx, q, store.Activity{
			Actor: &u.ID, Action: "project.updated", EntityType: "project", EntityID: &id, ProjectID: &id,
			Metadata: map[string]any{"fields": fieldsChanged},
		})
	}))
}

// waitingForTeam: new and not yet assigned, so the team can still change.
func waitingForTeam(st workflow.Status, assignedTo *uuid.UUID) bool {
	return assignedTo == nil && st == workflow.New
}

func sameID(a, b *uuid.UUID) bool {
	return (a == nil && b == nil) || (a != nil && b != nil && *a == *b)
}

// route sends the project to its type's team (no team when the type has none).
func route(ctx context.Context, q *db.Queries, id uuid.UUID, typeID *uuid.UUID) error {
	var team *uuid.UUID
	if typeID != nil {
		t, err := q.RouteTeamForType(ctx, *typeID)
		if err == nil {
			team = &t
		} else if !store.IsNotFound(err) {
			return err
		}
	}
	return q.SetProjectTeam(ctx, db.SetProjectTeamParams{ID: id, TeamID: team})
}

// routedByType: the project's team is the one its type sends it to (or neither has one),
// so it wasn't moved there by hand.
func routedByType(ctx context.Context, q *db.Queries, team, typeID *uuid.UUID) (bool, error) {
	var want *uuid.UUID
	if typeID != nil {
		t, err := optionalID(q.RouteTeamForType(ctx, *typeID))
		if err != nil {
			return false, err
		}
		want = t
	}
	return sameID(team, want), nil
}

// lookupNames maps profile/type ids to names so change history reads "Studio Main → Studio Pro".
func lookupNames(ctx context.Context, q *db.Queries) (map[string]string, error) {
	names := map[string]string{}
	profiles, err := q.ListProfiles(ctx, false)
	if err != nil {
		return nil, err
	}
	for _, p := range profiles {
		names[p.ID.String()] = p.Name
	}
	types, err := q.ListProjectTypes(ctx, false)
	if err != nil {
		return nil, err
	}
	for _, t := range types {
		names[t.ID.String()] = t.Name
	}
	return names, nil
}

func display(v *string, names map[string]string) *string {
	if v == nil {
		return nil
	}
	if n, ok := names[*v]; ok {
		return &n
	}
	return v
}

// ── Workflow ───────────────────────────────────────────────

func (s *Service) Transition(ctx context.Context, u db.User, id uuid.UUID, to workflow.Status, comment string) error {
	comment = strings.TrimSpace(comment)
	if len(comment) > 5000 {
		return apperr.Validation(validate.Errors{"comment": "is too long"})
	}
	return s.st.Tx(ctx, func(q *db.Queries) error {
		row, err := lockVisible(ctx, q, u, id)
		if err != nil {
			return err
		}
		from := workflow.Status(row.Status)
		rule, err := workflow.Check(from, to, actorFor(u, row.CreatedBy, row.Members), comment)
		if err != nil {
			return err
		}
		inc := int32(0)
		if rule.Revision {
			inc = 1
		}
		if err := q.SetProjectStatus(ctx, db.SetProjectStatusParams{ID: id, Status: string(to), RevisionInc: inc}); err != nil {
			return err
		}
		var c *string
		if comment != "" {
			c = &comment
		}
		if err := q.InsertStatusHistory(ctx, db.InsertStatusHistoryParams{
			ProjectID: id, FromStatus: &row.Status, ToStatus: string(to), ChangedBy: u.ID, Comment: c,
		}); err != nil {
			return err
		}
		if err := s.notifyTransition(ctx, q, u, row, from, to, comment); err != nil {
			return err
		}
		return store.LogActivity(ctx, q, store.Activity{
			Actor: &u.ID, Action: "project.status_changed", EntityType: "project", EntityID: &id, ProjectID: &id,
			Metadata: map[string]any{"from": row.Status, "to": to, "comment": comment, "revision": rule.Revision},
		})
	})
}

type AssignInput struct {
	AssigneeIDs []uuid.UUID `json:"assignee_ids"` // the full list of members working on it
	AssigneeID  uuid.UUID   `json:"assignee_id"`  // single-member shorthand
	Priority    string      `json:"priority"`
	DueDate     *string     `json:"due_date"`
}

// Assign sets who works on the project: one or more active members of its team. Assigning
// a New project starts it (In Progress); changing who works on it keeps the status.
func (s *Service) Assign(ctx context.Context, u db.User, id uuid.UUID, in AssignInput) error {
	ids := in.AssigneeIDs
	if len(ids) == 0 && in.AssigneeID != uuid.Nil {
		ids = []uuid.UUID{in.AssigneeID}
	}
	ids = uniqueIDs(ids)
	v := validate.Errors{}
	switch {
	case len(ids) == 0:
		v.Add("assignee_ids", "pick at least one member")
	case len(ids) > 20:
		v.Add("assignee_ids", "can have at most 20 members")
	}
	v.OneOf("priority", in.Priority, Priorities...)
	today := s.today(ctx)
	checkDue(v, in.DueDate, today)
	if !v.OK() {
		return apperr.Validation(v)
	}
	return s.st.Tx(ctx, func(q *db.Queries) error {
		row, err := lockVisible(ctx, q, u, id)
		if err != nil {
			return err
		}
		from := workflow.Status(row.Status)
		a := actorFor(u, row.CreatedBy, row.Members)
		if !workflow.CanAssign(from, a) {
			if a.Role != policy.TL {
				return apperr.Forbidden()
			}
			return apperr.InvalidTransition("A project that is " + from.Label() + " can't be assigned")
		}
		// Besides the team's members, the project can go to a Team Leader who runs its
		// team: the acting leader themselves, the team's lead or its department's head.
		leaders := map[uuid.UUID]bool{u.ID: true}
		if row.TeamID != nil {
			for _, lookup := range []func(context.Context, uuid.UUID) (uuid.UUID, error){q.ActiveTeamLead, q.ActiveTeamHead} {
				id, err := optionalID(lookup(ctx, *row.TeamID))
				if err != nil {
					return err
				}
				if id != nil {
					leaders[*id] = true
				}
			}
		}
		names := map[uuid.UUID]string{}
		for _, mid := range ids {
			member, err := q.GetUserByID(ctx, mid)
			if store.IsNotFound(err) || (err == nil && !member.IsActive) {
				return apperr.Validation(validate.Errors{"assignee_ids": "must all be active team members"})
			} else if err != nil {
				return err
			}
			if member.Role == string(policy.TL) {
				if !leaders[mid] {
					return apperr.Validation(validate.Errors{"assignee_ids": member.Name + " doesn't run this project's team"})
				}
				names[mid] = member.Name
				continue
			}
			if member.Role != string(policy.Member) {
				return apperr.Validation(validate.Errors{"assignee_ids": "must all be active team members"})
			}
			if row.TeamID != nil {
				ok, err := q.IsTeamMember(ctx, db.IsTeamMemberParams{TeamID: *row.TeamID, UserID: mid})
				if err != nil {
					return err
				}
				if !ok {
					return apperr.Validation(validate.Errors{"assignee_ids": member.Name + " is not in this project's team"})
				}
			}
			names[mid] = member.Name
		}
		var added, removed []uuid.UUID
		for _, mid := range ids {
			if !slices.Contains(row.Members, mid) {
				added = append(added, mid)
			}
		}
		for _, mid := range row.Members {
			if !slices.Contains(ids, mid) {
				removed = append(removed, mid)
			}
		}
		if len(added) == 0 && len(removed) == 0 {
			return apperr.Conflict("These members are already assigned to this project")
		}
		status := workflow.AfterAssign(from)
		for _, mid := range removed {
			if err := takeOff(ctx, q, id, mid); err != nil {
				return err
			}
			if names[mid] == "" {
				if prev, err := q.GetUserByID(ctx, mid); err == nil {
					names[mid] = prev.Name
				}
			}
		}
		for _, mid := range added {
			if err := q.AddProjectMember(ctx, db.AddProjectMemberParams{ProjectID: id, UserID: mid, AddedBy: u.ID}); err != nil {
				return err
			}
			if err := q.OpenAssignment(ctx, db.OpenAssignmentParams{
				ProjectID: id, UserID: mid, AssignedBy: u.ID, Priority: in.Priority, DueDate: in.DueDate,
			}); err != nil {
				return err
			}
		}
		if err := q.AssignProject(ctx, db.AssignProjectParams{
			ID: id, AssignedTo: &ids[0], Priority: in.Priority, DueDate: in.DueDate, Status: string(status),
		}); err != nil {
			return err
		}
		if err := q.SetPrimaryAssignee(ctx, id); err != nil {
			return err
		}
		if err := s.notifyAssigned(ctx, q, u, row.Project, added, removed, in.Priority, in.DueDate); err != nil {
			return err
		}
		nameList := func(list []uuid.UUID) string {
			out := make([]string, len(list))
			for i, mid := range list {
				out[i] = names[mid]
			}
			return strings.Join(out, ", ")
		}
		var parts []string
		action := "project.assigned"
		if len(row.Members) == 0 {
			parts = append(parts, "Assigned to "+nameList(added))
		} else {
			action = "project.reassigned"
			if len(added) > 0 {
				parts = append(parts, "Added "+nameList(added))
			}
			if len(removed) > 0 {
				parts = append(parts, "Removed "+nameList(removed))
			}
		}
		note := strings.Join(parts, "; ")
		if err := q.InsertStatusHistory(ctx, db.InsertStatusHistoryParams{
			ProjectID: id, FromStatus: &row.Status, ToStatus: string(status), ChangedBy: u.ID, Comment: &note,
		}); err != nil {
			return err
		}
		return store.LogActivity(ctx, q, store.Activity{
			Actor: &u.ID, Action: action, EntityType: "project", EntityID: &id, ProjectID: &id,
			Metadata: map[string]any{"added": added, "removed": removed, "members": ids, "priority": in.Priority, "due_date": in.DueDate},
		})
	})
}

// takeOff removes a member from the project: their assignment closes and their cards are unassigned.
func takeOff(ctx context.Context, q *db.Queries, id, member uuid.UUID) error {
	if err := q.RemoveProjectMember(ctx, db.RemoveProjectMemberParams{ProjectID: id, UserID: member}); err != nil {
		return err
	}
	if err := q.CloseMemberAssignment(ctx, db.CloseMemberAssignmentParams{ProjectID: id, UserID: member}); err != nil {
		return err
	}
	return q.UnassignMemberCards(ctx, db.UnassignMemberCardsParams{ProjectID: &id, UserID: member})
}

type MoveTeamInput struct {
	TeamID  uuid.UUID `json:"team_id"`
	Comment string    `json:"comment"` // why it's moving, kept in the timeline
}

// MoveTeam hands the project to another team, even after it has started. Members who
// can't work for the new team (not in it, and not its lead or head) are taken off; if
// nobody is left the project goes back to New for the new team's lead to assign.
func (s *Service) MoveTeam(ctx context.Context, u db.User, id uuid.UUID, in MoveTeamInput) error {
	v := validate.Errors{}
	if in.TeamID == uuid.Nil {
		v.Add("team_id", "is required")
	}
	in.Comment = strings.TrimSpace(in.Comment)
	if in.Comment == "" {
		v.Add("comment", "say why it's moving")
	} else if len(in.Comment) > 2000 {
		v.Add("comment", "is too long")
	}
	if !v.OK() {
		return apperr.Validation(v)
	}
	return s.st.Tx(ctx, func(q *db.Queries) error {
		row, err := lockVisible(ctx, q, u, id)
		if err != nil {
			return err
		}
		from := workflow.Status(row.Status)
		a := actorFor(u, row.CreatedBy, row.Members)
		if !workflow.CanMoveTeam(from, a) {
			if a.Role != policy.TL {
				return apperr.Forbidden()
			}
			return apperr.InvalidTransition("A project that is " + from.Label() + " can't move to another team")
		}
		if row.TeamID != nil && *row.TeamID == in.TeamID {
			return apperr.Validation(validate.Errors{"team_id": "is already this project's team"})
		}
		team, err := q.GetTeam(ctx, in.TeamID)
		if store.IsNotFound(err) || (err == nil && !team.IsActive) {
			return apperr.Validation(validate.Errors{"team_id": "must be an active team"})
		} else if err != nil {
			return err
		}
		oldTeam := "no team"
		if row.TeamID != nil {
			if t, err := q.GetTeam(ctx, *row.TeamID); err == nil {
				oldTeam = t.Name
			} else if !store.IsNotFound(err) {
				return err
			}
		}
		leaders := map[uuid.UUID]bool{}
		for _, lookup := range []func(context.Context, uuid.UUID) (uuid.UUID, error){q.ActiveTeamLead, q.ActiveTeamHead} {
			lid, err := optionalID(lookup(ctx, in.TeamID))
			if err != nil {
				return err
			}
			if lid != nil {
				leaders[*lid] = true
			}
		}
		var kept, removed []uuid.UUID
		for _, mid := range row.Members {
			stays := leaders[mid]
			if !stays {
				ok, err := q.IsTeamMember(ctx, db.IsTeamMemberParams{TeamID: in.TeamID, UserID: mid})
				if err != nil {
					return err
				}
				stays = ok
			}
			if stays {
				kept = append(kept, mid)
				continue
			}
			if err := takeOff(ctx, q, id, mid); err != nil {
				return err
			}
			removed = append(removed, mid)
		}
		st, err := q.MoveProjectTeam(ctx, db.MoveProjectTeamParams{ID: id, TeamID: &in.TeamID})
		if err != nil {
			return err
		}
		to := workflow.Status(st)
		note := "Moved from " + oldTeam + " to " + team.Name + ": " + in.Comment
		if err := q.InsertStatusHistory(ctx, db.InsertStatusHistoryParams{
			ProjectID: id, FromStatus: &row.Status, ToStatus: st, ChangedBy: u.ID, Comment: &note,
		}); err != nil {
			return err
		}
		if err := s.notifyMoved(ctx, q, u, row, team.Name, removed, to, in.Comment); err != nil {
			return err
		}
		return store.LogActivity(ctx, q, store.Activity{
			Actor: &u.ID, Action: "project.team_changed", EntityType: "project", EntityID: &id, ProjectID: &id,
			Metadata: map[string]any{
				"from_team": row.TeamID, "to_team": in.TeamID, "from": row.Status, "to": st,
				"kept": kept, "removed": removed, "comment": in.Comment,
			},
		})
	})
}

func uniqueIDs(ids []uuid.UUID) []uuid.UUID {
	out := make([]uuid.UUID, 0, len(ids))
	for _, id := range ids {
		if id != uuid.Nil && !slices.Contains(out, id) {
			out = append(out, id)
		}
	}
	return out
}

func checkDue(v validate.Errors, due *string, today string) {
	if due == nil {
		return
	}
	if _, ok := parseDate(v, "due_date", due); ok && *due < today {
		v.Add("due_date", "can't be in the past")
	}
}

type PlanInput struct {
	Priority string  `json:"priority"`
	DueDate  *string `json:"due_date"`
}

// Plan lets the TL change priority and due date without reassigning.
func (s *Service) Plan(ctx context.Context, u db.User, id uuid.UUID, in PlanInput) error {
	v := validate.Errors{}
	v.OneOf("priority", in.Priority, Priorities...)
	if !v.OK() {
		return apperr.Validation(v)
	}
	today := s.today(ctx)
	return s.st.Tx(ctx, func(q *db.Queries) error {
		row, err := lockVisible(ctx, q, u, id)
		if err != nil {
			return err
		}
		if !workflow.CanPlan(workflow.Status(row.Status), actorFor(u, row.CreatedBy, row.Members)) {
			if policy.Role(u.Role) == policy.TL {
				return apperr.Conflict("Priority and due date can't change once a project is " + workflow.Status(row.Status).Label())
			}
			return apperr.Forbidden()
		}
		cur, err := q.GetProjectDetail(ctx, id)
		if err != nil {
			return err
		}
		// An unchanged past due date is fine (already overdue); a new one must not be in the past.
		if deref(in.DueDate) != deref(cur.DueDate) {
			checkDue(v, in.DueDate, today)
			if !v.OK() {
				return apperr.Validation(v)
			}
		}
		if err := q.SetProjectPlanning(ctx, db.SetProjectPlanningParams{ID: id, Priority: in.Priority, DueDate: in.DueDate}); err != nil {
			return err
		}
		return store.LogActivity(ctx, q, store.Activity{
			Actor: &u.ID, Action: "project.planning_updated", EntityType: "project", EntityID: &id, ProjectID: &id,
			Metadata: map[string]any{
				"priority": [2]string{cur.Priority, in.Priority},
				"due_date": [2]*string{cur.DueDate, in.DueDate},
			},
		})
	})
}

// ── Comments ───────────────────────────────────────────────

func (s *Service) Comment(ctx context.Context, u db.User, id uuid.UUID, body string) (db.ProjectComment, error) {
	body = strings.TrimSpace(body)
	v := validate.Errors{}
	v.Len("body", body, 1, 10000)
	if !v.OK() {
		return db.ProjectComment{}, apperr.Validation(v)
	}
	var c db.ProjectComment
	err := s.st.Tx(ctx, func(q *db.Queries) error {
		p, err := s.load(ctx, q, u, id)
		if err != nil {
			return err
		}
		if !canComment(u, actorFor(u, p.CreatedBy, p.MemberIDs)) {
			return apperr.Forbidden()
		}
		if c, err = q.CreateProjectComment(ctx, db.CreateProjectCommentParams{ProjectID: id, AuthorID: u.ID, Body: body}); err != nil {
			return err
		}
		if err := s.notifyProjectActivity(ctx, q, u, p, "commented "+quote(body)); err != nil {
			return err
		}
		if err := q.TouchProject(ctx, id); err != nil {
			return err
		}
		return store.LogActivity(ctx, q, store.Activity{
			Actor: &u.ID, Action: "project.commented", EntityType: "project_comment", EntityID: &c.ID, ProjectID: &id,
		})
	})
	return c, err
}
