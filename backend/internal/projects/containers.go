package projects

import (
	"context"
	"fmt"
	"slices"

	"github.com/google/uuid"

	"github.com/solvinx/opsflow/internal/apperr"
	"github.com/solvinx/opsflow/internal/db"
	"github.com/solvinx/opsflow/internal/notify"
	"github.com/solvinx/opsflow/internal/policy"
	"github.com/solvinx/opsflow/internal/store"
	"github.com/solvinx/opsflow/internal/workflow"
)

// A board card, a client update and a file hang on a container: a project, or a client
// request for a client who has no project in the portal. Both answer the same questions —
// who it belongs to, who works it, which team it is with — so the board and the update
// rules read a container and never care which of the two they are looking at.
type container struct {
	ID      uuid.UUID
	Request bool   // a client request rather than a project
	Status  string // the project's workflow status, or open / done / cancelled
	SalesID uuid.UUID
	// Members work it: a project's assigned members, a request's card assignees.
	Members []uuid.UUID
	// Assignable is who a card may be given to: a project's members, or everyone on the
	// team the request is with (that is how a request's work gets handed out).
	Assignable []uuid.UUID
	TeamID     *uuid.UUID
	// The Team Leaders over it: the team's lead and its department's head.
	LeadID, HeadID *uuid.UUID
	Label          string // how a notification or an activity entry names it
}

func (c container) projectID() *uuid.UUID {
	if c.Request {
		return nil
	}
	return &c.ID
}

func (c container) requestID() *uuid.UUID {
	if !c.Request {
		return nil
	}
	return &c.ID
}

func (c container) link() string {
	if c.Request {
		return requestLink(c.ID)
	}
	return projectLink(c.ID)
}

// entity names the container for the activity log.
func (c container) entity() string {
	if c.Request {
		return "client_request"
	}
	return "project"
}

func requestLink(id uuid.UUID) string { return "/requests/" + id.String() }

// requestLabel is how a client request reads in a notification: "CR-7 · Nadia (Shopify)".
func requestLabel(number int64, client string) string {
	return fmt.Sprintf("CR-%d · %s", number, client)
}

func projectContainer(p lockedProject) container {
	return container{
		ID: p.ID, Status: p.Status, SalesID: p.CreatedBy, Members: p.Members, Assignable: p.Members,
		TeamID: p.TeamID, Label: label(p.Number, p.Name),
	}
}

func requestContainer(r db.ClientRequestDetail, team []uuid.UUID) container {
	return container{
		ID: r.ID, Request: true, Status: r.Status, SalesID: r.CreatedBy, Members: r.MemberIDs,
		Assignable: team, TeamID: &r.TeamID, LeadID: r.TeamLeadID, HeadID: r.TeamHeadID,
		Label: requestLabel(r.Number, r.ClientName),
	}
}

// updateContainer reads the container's identity straight off an update's read model,
// which already carries it under its project_* columns whichever kind it is.
func updateContainer(d db.UpdateDetail) container {
	c := container{
		Status: d.ProjectStatus, SalesID: d.ProjectSalesID, Members: d.ProjectMemberIDs, Assignable: d.ProjectMemberIDs,
		TeamID: d.ProjectTeamID, LeadID: d.ProjectTeamLeadID, HeadID: d.ProjectTeamHeadID,
		Label: label(d.ProjectNumber, d.ProjectName),
	}
	if d.ClientRequestID != nil {
		c.ID, c.Request = *d.ClientRequestID, true
		c.Label = requestLabel(d.ProjectNumber, deref(d.ClientName))
		return c
	}
	c.ID = deref(d.ProjectID)
	return c
}

// updateRowContainer is the container an update row hangs on, for touching and logging.
func updateRowContainer(row db.Update) container {
	if row.ClientRequestID != nil {
		return container{ID: *row.ClientRequestID, Request: true}
	}
	return container{ID: deref(row.ProjectID)}
}

// containerFlowStatus is the status the update rules read: a client request presents
// itself as work under way while it is open, and as delivered work once it is closed,
// which is exactly how a project's updates behave at those points.
func containerFlowStatus(c container) string {
	if !c.Request {
		return c.Status
	}
	switch c.Status {
	case requestOpen:
		return string(workflow.InProgress)
	case requestDone:
		return string(workflow.Delivered)
	}
	return string(workflow.Cancelled)
}

// canViewRequest: Sales (one team, covering for each other) see every request; a Team
// Leader those with a team they lead or whose department they head (and any whose team
// has no active lead, like a project with no team); everyone on the team it is with, so
// the team reads the work from the moment Sales send it over, before the leader has
// handed out its cards; the CEO everything. onTeam says whether the viewer is on that
// team — viewerTeam looks it up, since only a member is decided by it.
func canViewRequest(u db.User, r db.ClientRequestDetail, onTeam bool) bool {
	switch policy.Role(u.Role) {
	case policy.Sales:
		return true
	case policy.TL:
		return r.TeamLeadID == nil || *r.TeamLeadID == u.ID || (r.TeamHeadID != nil && *r.TeamHeadID == u.ID)
	case policy.Member:
		// On the team now, or still holding a card after being moved to another team.
		return onTeam || slices.Contains(r.MemberIDs, u.ID)
	case policy.CEO:
		return true
	}
	return false
}

// viewerTeam is the team the viewer is on, when that is what decides what they may see:
// a client request belongs to a team, not to the people holding its cards. Only a member
// is scoped that way — Sales, the leaders and the CEO are scoped by role — so nobody else
// is looked up. nil means "on no team", which sees no request.
func viewerTeam(ctx context.Context, q *db.Queries, u db.User) (*uuid.UUID, error) {
	if policy.Role(u.Role) != policy.Member {
		return nil, nil
	}
	id, err := q.UserTeamID(ctx, u.ID)
	if store.IsNotFound(err) {
		return nil, nil
	} else if err != nil {
		return nil, err
	}
	return &id, nil
}

// onTeam reports whether the viewer is on the given team.
func onTeam(team *uuid.UUID, id uuid.UUID) bool { return team != nil && *team == id }

// visibleRequest reads a client request the user may see, or 404s (never revealing that
// it exists).
func (s *Service) visibleRequest(ctx context.Context, q *db.Queries, u db.User, id uuid.UUID) (db.ClientRequestDetail, error) {
	r, err := q.GetClientRequestDetail(ctx, id)
	if store.IsNotFound(err) {
		return r, apperr.NotFound("Client request")
	} else if err != nil {
		return r, err
	}
	team, err := viewerTeam(ctx, q, u)
	if err != nil {
		return r, err
	}
	if !canViewRequest(u, r, onTeam(team, r.TeamID)) {
		return r, apperr.NotFound("Client request")
	}
	return r, nil
}

// teamRoster is everyone a request's work can be given to: the team's active members, its
// lead and its department's head (leaders do delivery work themselves). A team whose
// members were never added still has its lead, so the work can always be handed out.
func teamRoster(ctx context.Context, q *db.Queries, r db.ClientRequestDetail) ([]db.ListTeamRosterRow, []uuid.UUID, error) {
	rows, err := q.ListTeamRoster(ctx, r.TeamID)
	if err != nil {
		return nil, nil, err
	}
	ids := make([]uuid.UUID, len(rows))
	for i, m := range rows {
		ids[i] = m.ID
	}
	return rows, ids, nil
}

// lockVisibleRequest locks a client request the user may see, or 404s (never revealing
// that it exists).
func (s *Service) lockVisibleRequest(ctx context.Context, q *db.Queries, u db.User, id uuid.UUID) (db.ClientRequestDetail, container, error) {
	var c container
	if _, err := q.LockClientRequest(ctx, id); store.IsNotFound(err) {
		return db.ClientRequestDetail{}, c, apperr.NotFound("Client request")
	} else if err != nil {
		return db.ClientRequestDetail{}, c, err
	}
	r, err := s.visibleRequest(ctx, q, u, id)
	if err != nil {
		return r, c, err
	}
	_, ids, err := teamRoster(ctx, q, r)
	if err != nil {
		return r, c, err
	}
	return r, requestContainer(r, ids), nil
}

// lockContainer locks whichever container an id points at. Cards and updates carry both
// ids, exactly one of which is set.
func (s *Service) lockContainer(ctx context.Context, q *db.Queries, u db.User, projectID, requestID *uuid.UUID) (container, error) {
	if requestID != nil {
		_, c, err := s.lockVisibleRequest(ctx, q, u, *requestID)
		return c, err
	}
	if projectID == nil {
		return container{}, apperr.NotFound("Project")
	}
	p, err := lockVisible(ctx, q, u, *projectID)
	return projectContainer(p), err
}

// touch moves the container's "last activity" so lists stay in the right order.
func touch(ctx context.Context, q *db.Queries, c container) error {
	if c.Request {
		return q.TouchClientRequest(ctx, c.ID)
	}
	return q.TouchProject(ctx, c.ID)
}

// log writes an activity entry against the container.
func (c container) log(ctx context.Context, q *db.Queries, actor uuid.UUID, action, entityType string,
	entityID *uuid.UUID, meta map[string]any) error {
	return store.LogActivity(ctx, q, store.Activity{
		Actor: &actor, Action: action, EntityType: entityType, EntityID: entityID,
		ProjectID: c.projectID(), ClientRequestID: c.requestID(), Metadata: meta,
	})
}

// note fills in which container a notification is about.
func (c container) note(n notify.Notification) notify.Notification {
	n.ProjectID, n.ClientRequestID = c.projectID(), c.requestID()
	return n
}

// leadsOn are the Team Leaders who can see the container: for a project its team's lead
// (or every TL when it has no team), for a request its team's lead and department head.
func (s *Service) leadsOn(ctx context.Context, q *db.Queries, c container) notify.Recipients {
	if !c.Request {
		return s.leads(ctx, q, c.ID)
	}
	if c.LeadID == nil && c.HeadID == nil {
		// Nobody leads the team: every Team Leader can see it, as with an unteamed project.
		return s.role(ctx, q, policy.TL)
	}
	return notify.Recipients{}.AddPtr(c.LeadID).AddPtr(c.HeadID)
}

// live says whether the container is still something to work on. A cancelled project and
// a cancelled request are closed for good; everything else can still be touched.
func (c container) live() bool {
	if c.Request {
		return c.Status != requestCancelled
	}
	return workflow.Status(c.Status) != workflow.Cancelled
}
