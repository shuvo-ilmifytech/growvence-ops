package projects

import (
	"context"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/solvinx/opsflow/internal/apperr"
	"github.com/solvinx/opsflow/internal/db"
	"github.com/solvinx/opsflow/internal/notify"
	"github.com/solvinx/opsflow/internal/policy"
	"github.com/solvinx/opsflow/internal/store"
	"github.com/solvinx/opsflow/internal/validate"
)

// Client requests: work for a client with no project in the portal — one on monthly
// management, or an order delivered before the portal existed. Sales raise the request
// with the profile it goes out on, the client, the order ID if there is one, and the team
// that should do it; the team works its board cards and writes its client updates, which
// follow the same approval as a project's.
//
//   - Sales (any of them, covering for each other) raise, edit and close requests, and
//     send the approved updates on the marketplace.
//   - The Team Leader of the team it is with hands the cards out, checks the updates and
//     may pass the request to another team.
//   - Everyone on that team sees the request as soon as Sales send it over, whether or not
//     the leader has handed them a card: it is the team's work, not one person's.
//   - The members holding its cards work it and write its updates.
//   - The CEO reads everything and changes nothing.

const (
	requestOpen      = "open"
	requestDone      = "done"
	requestCancelled = "cancelled"
)

var RequestStatuses = []string{requestOpen, requestDone, requestCancelled}

var requestStatusLabels = map[string]string{
	requestOpen: "Open", requestDone: "Done", requestCancelled: "Cancelled",
}

// canRaiseRequest: Sales raise the work they are asked for. Nobody else has a client
// asking them, and the CEO never operates work.
func canRaiseRequest(u db.User) bool { return policy.Role(u.Role) == policy.Sales }

type RequestInput struct {
	ProfileID  *uuid.UUID `json:"profile_id"`
	ClientName *string    `json:"client_name"`
	OrderID    *string    `json:"order_id"` // optional: an old order, or none at all
	Title      *string    `json:"title"`
	Details    *string    `json:"details"`
	TeamID     *uuid.UUID `json:"team_id"`
}

type RequestOut struct {
	db.ClientRequestDetail
	StatusLabel string `json:"status_label"`
}

type RequestPermissions struct {
	CanEdit      bool `json:"can_edit"`       // Sales, while it is open
	CanMoveTeam  bool `json:"can_move_team"`  // the Team Leader it is with
	CanClose     bool `json:"can_close"`      // Sales mark it done once the client has what they asked for
	CanCancel    bool `json:"can_cancel"`     // Sales call it off
	CanReopen    bool `json:"can_reopen"`     // Sales reopen a closed one
	CanAddCard   bool `json:"can_add_card"`   // Sales add another ask; the Team Leader adds tasks
	CanWriteNote bool `json:"can_write_note"` // the members working it write client updates
	CanAssign    bool `json:"can_assign"`     // the Team Leader (or a member holding it) hands the work out
}

type RequestDetailResult struct {
	Request     RequestOut             `json:"request"`
	Permissions RequestPermissions     `json:"permissions"`
	Cards       []CardOut              `json:"cards"`
	Columns     []string               `json:"columns"`
	Roster      []db.ListTeamRosterRow `json:"roster"` // who the cards can be given to
	Activity    []db.ListActivityRow   `json:"activity"`
	CanMoveCard bool                   `json:"can_move_card"`
}

func out(r db.ClientRequestDetail) RequestOut {
	return RequestOut{ClientRequestDetail: r, StatusLabel: requestStatusLabels[r.Status]}
}

func checkRequest(in RequestInput) (validate.Errors, string, string, string, *string) {
	v := validate.Errors{}
	client := strings.TrimSpace(deref(in.ClientName))
	title := strings.TrimSpace(deref(in.Title))
	details := strings.TrimSpace(deref(in.Details))
	v.Len("client_name", client, 1, 200)
	v.Len("title", title, 1, 200)
	v.MaxLen("details", details, 20000)
	if in.ProfileID == nil {
		v.Add("profile_id", "is required")
	}
	var order *string
	if o := strings.TrimSpace(deref(in.OrderID)); o != "" {
		if len(o) > 100 {
			v.Add("order_id", "is too long")
		}
		order = &o
	}
	return v, client, title, details, order
}

// Raise creates a client request and sends it to a team.
func (s *Service) Raise(ctx context.Context, u db.User, in RequestInput) (uuid.UUID, error) {
	if !canRaiseRequest(u) {
		return uuid.Nil, apperr.Forbidden()
	}
	v, client, title, details, order := checkRequest(in)
	if in.TeamID == nil {
		v.Add("team_id", "is required: pick the team that should do the work")
	}
	if !v.OK() {
		return uuid.Nil, apperr.Validation(v)
	}
	var id uuid.UUID
	err := s.st.Tx(ctx, func(q *db.Queries) error {
		if err := checkProfileAndTeam(ctx, q, *in.ProfileID, *in.TeamID); err != nil {
			return err
		}
		r, err := q.CreateClientRequest(ctx, db.CreateClientRequestParams{
			ProfileID: *in.ProfileID, ClientName: client, OrderID: order,
			Title: title, Details: details, TeamID: *in.TeamID, CreatedBy: u.ID,
		})
		if err != nil {
			return err
		}
		id = r.ID
		d, err := q.GetClientRequestDetail(ctx, r.ID)
		if err != nil {
			return err
		}
		// The ask itself starts on the team's board, so the work shows up where the team
		// already looks and the Team Leader hands it to someone.
		c := requestContainer(d, nil)
		card, err := q.CreateCard(ctx, db.CreateCardParams{
			ClientRequestID: &r.ID, ContainerID: r.ID, Title: title, Description: details,
			Source: "client", CreatedBy: u.ID,
		})
		if err != nil {
			return err
		}
		cd, err := q.GetCardDetail(ctx, card.ID)
		if err != nil {
			return err
		}
		if err := notify.Send(ctx, q, s.leadsOn(ctx, q, c), u.ID, c.note(notify.Notification{
			Type: notify.CardCreated, Link: cardLink(cd), Important: true,
			Title: "New client request for " + d.ClientName + ": " + title,
			Body:  "From " + u.Name + " · " + d.TeamName + " · no project",
		})); err != nil {
			return err
		}
		return c.log(ctx, q, u.ID, "request.raised", "client_request", &r.ID, map[string]any{
			"number": r.Number, "title": title, "client": client, "team": d.TeamName, "order_id": deref(order),
		})
	})
	return id, err
}

func checkProfileAndTeam(ctx context.Context, q *db.Queries, profileID, teamID uuid.UUID) error {
	v := validate.Errors{}
	if p, err := q.GetProfile(ctx, profileID); store.IsNotFound(err) || (err == nil && !p.IsActive) {
		v.Add("profile_id", "is not an active profile")
	} else if err != nil {
		return err
	}
	if t, err := q.GetTeam(ctx, teamID); store.IsNotFound(err) || (err == nil && !t.IsActive) {
		v.Add("team_id", "is not an active team")
	} else if err != nil {
		return err
	}
	if !v.OK() {
		return apperr.Validation(v)
	}
	return nil
}

// EditRequest changes what the client asked for. Sales keep it right; the team reads it.
func (s *Service) EditRequest(ctx context.Context, u db.User, id uuid.UUID, in RequestInput) error {
	v, client, title, details, order := checkRequest(in)
	if !v.OK() {
		return apperr.Validation(v)
	}
	return s.st.Tx(ctx, func(q *db.Queries) error {
		r, c, err := s.lockVisibleRequest(ctx, q, u, id)
		if err != nil {
			return err
		}
		if !canRaiseRequest(u) {
			return apperr.Forbidden()
		}
		if r.Status != requestOpen {
			return apperr.Conflict("A " + strings.ToLower(requestStatusLabels[r.Status]) + " request can't be changed. Reopen it first.")
		}
		if err := checkProfileAndTeam(ctx, q, *in.ProfileID, r.TeamID); err != nil {
			return err
		}
		if err := q.UpdateClientRequest(ctx, db.UpdateClientRequestParams{
			ID: id, ProfileID: *in.ProfileID, ClientName: client, OrderID: order, Title: title, Details: details,
		}); err != nil {
			return err
		}
		if err := touch(ctx, q, c); err != nil {
			return err
		}
		// The team is already working from it, so tell them it changed.
		if err := notify.Batch(ctx, q, s.leadsOn(ctx, q, c).Add(r.MemberIDs...), u.ID, nil, &id,
			s.batchWindow(ctx, q), notify.ProjectChanges, "Client request changed: "+title,
			requestLink(id), u.Name+" updated what the client asked for"); err != nil {
			return err
		}
		return c.log(ctx, q, u.ID, "request.updated", "client_request", &id,
			map[string]any{"number": r.Number, "title": title, "client": client})
	})
}

type RequestTeamInput struct {
	TeamID uuid.UUID `json:"team_id"`
	Reason string    `json:"reason"`
}

// MoveRequestTeam hands a request to another team: Sales may have sent it to the wrong one,
// or it turns out to be someone else's work. The Team Leader it is with decides, with a
// reason, and the cards' assignees come off it since they are not on the new team.
func (s *Service) MoveRequestTeam(ctx context.Context, u db.User, id uuid.UUID, in RequestTeamInput) error {
	reason := strings.TrimSpace(in.Reason)
	v := validate.Errors{}
	v.Len("reason", reason, 3, 1000)
	if in.TeamID == uuid.Nil {
		v.Add("team_id", "is required")
	}
	if !v.OK() {
		return apperr.Validation(v)
	}
	return s.st.Tx(ctx, func(q *db.Queries) error {
		r, c, err := s.lockVisibleRequest(ctx, q, u, id)
		if err != nil {
			return err
		}
		if policy.Role(u.Role) != policy.TL {
			return apperr.Forbidden()
		}
		if r.Status != requestOpen {
			return apperr.Conflict("Only an open request can be moved to another team")
		}
		if r.TeamID == in.TeamID {
			return apperr.Conflict("The request is already with that team")
		}
		to, err := q.GetTeam(ctx, in.TeamID)
		if store.IsNotFound(err) || (err == nil && !to.IsActive) {
			return apperr.Validation(validate.Errors{"team_id": "is not an active team"})
		} else if err != nil {
			return err
		}
		if err := q.SetClientRequestTeam(ctx, db.SetClientRequestTeamParams{ID: id, TeamID: in.TeamID}); err != nil {
			return err
		}
		// Whoever was working it is not on the new team: the new leader hands it out again.
		if err := q.ClearRequestCardAssignees(ctx, &id); err != nil {
			return err
		}
		if err := touch(ctx, q, c); err != nil {
			return err
		}
		moved, err := q.GetClientRequestDetail(ctx, id)
		if err != nil {
			return err
		}
		nc := requestContainer(moved, nil)
		if err := notify.Send(ctx, q, s.leadsOn(ctx, q, nc).Add(r.MemberIDs...).Add(r.CreatedBy), u.ID,
			nc.note(notify.Notification{
				Type: notify.ProjectMoved, Link: requestLink(id), Important: true,
				Title: "Client request moved to " + to.Name + ": " + r.Title,
				Body:  u.Name + " moved it from " + r.TeamName + " · " + reason,
			})); err != nil {
			return err
		}
		return nc.log(ctx, q, u.ID, "request.team_changed", "client_request", &id, map[string]any{
			"number": r.Number, "from": r.TeamName, "to": to.Name, "reason": reason,
		})
	})
}

type RequestStatusInput struct {
	Status string `json:"status"`
	Reason string `json:"reason"`
}

// SetRequestStatus closes a request (done), calls it off (cancelled) or reopens it. Sales
// decide: they are the ones who know the client has what they asked for.
func (s *Service) SetRequestStatus(ctx context.Context, u db.User, id uuid.UUID, in RequestStatusInput) error {
	reason := strings.TrimSpace(in.Reason)
	v := validate.Errors{}
	v.OneOf("status", in.Status, RequestStatuses...)
	v.MaxLen("reason", reason, 1000)
	if in.Status == requestCancelled && reason == "" {
		v.Add("reason", "is required when a request is cancelled")
	}
	if !v.OK() {
		return apperr.Validation(v)
	}
	return s.st.Tx(ctx, func(q *db.Queries) error {
		r, c, err := s.lockVisibleRequest(ctx, q, u, id)
		if err != nil {
			return err
		}
		if !canRaiseRequest(u) {
			return apperr.Forbidden()
		}
		if r.Status == in.Status {
			return nil
		}
		if r.Status == requestCancelled && in.Status == requestDone {
			return apperr.Conflict("A cancelled request can only be reopened")
		}
		if in.Status != requestOpen && r.UnsentUpdateCount > 0 {
			return apperr.Conflict("An approved update hasn't been sent to the client yet. Send it first.")
		}
		var by *uuid.UUID
		var at *time.Time
		if in.Status != requestOpen {
			now := time.Now()
			by, at = &u.ID, &now
		}
		if err := q.SetClientRequestStatus(ctx, db.SetClientRequestStatusParams{
			ID: id, Status: in.Status, ClosedBy: by, ClosedAt: at,
		}); err != nil {
			return err
		}
		if err := touch(ctx, q, c); err != nil {
			return err
		}
		title := map[string]string{
			requestOpen:      "Client request reopened: " + r.Title,
			requestDone:      "Client request closed: " + r.Title,
			requestCancelled: "Client request cancelled: " + r.Title,
		}[in.Status]
		body := u.Name + " · " + r.ClientName
		if reason != "" {
			body += " · " + reason
		}
		if err := notify.Send(ctx, q, s.leadsOn(ctx, q, c).Add(r.MemberIDs...), u.ID, c.note(notify.Notification{
			Type: notify.ProjectCompleted, Link: requestLink(id), Title: title, Body: body,
			Important: in.Status == requestCancelled,
		})); err != nil {
			return err
		}
		return c.log(ctx, q, u.ID, "request.status_changed", "client_request", &id, map[string]any{
			"number": r.Number, "from": r.Status, "to": in.Status, "reason": reason,
		})
	})
}

// ── Read ───────────────────────────────────────────────────

type RequestFilter struct {
	Statuses []string
	Team     *uuid.UUID
	Mine     bool
	Q        *string
	Page     int
	PageSize int
}

type RequestListResult struct {
	Requests []RequestOut     `json:"requests"`
	Total    int64            `json:"total"`
	Counts   map[string]int64 `json:"counts"`
	Page     int              `json:"page"`
	PageSize int              `json:"page_size"`
	CanRaise bool             `json:"can_raise"`
}

// requestScope narrows a list to what the role may see, matching canViewRequest. A
// member's id scopes it to the requests with their team (and any they still hold a card
// on); the SQL does that lookup so the list stays one query.
func requestScope(u db.User) (member, lead *uuid.UUID) {
	switch policy.Role(u.Role) {
	case policy.Sales, policy.CEO:
		return nil, nil
	case policy.TL:
		return nil, &u.ID
	case policy.Member:
		return &u.ID, nil
	}
	nobody := uuid.Nil
	return &nobody, nil
}

func (s *Service) ListRequests(ctx context.Context, u db.User, f RequestFilter) (RequestListResult, error) {
	member, lead := requestScope(u)
	var author *uuid.UUID
	if f.Mine {
		author = &u.ID
	}
	rows, err := s.st.ListClientRequests(ctx, db.ListClientRequestsParams{
		ScopeMember: member, ScopeLead: lead, Statuses: f.Statuses, Team: f.Team, Author: author, Q: f.Q,
		Lim: int32(f.PageSize), Off: int32((f.Page - 1) * f.PageSize),
	})
	if err != nil {
		return RequestListResult{}, err
	}
	counts, err := s.st.CountClientRequestsByStatus(ctx, db.CountClientRequestsByStatusParams{
		ScopeMember: member, ScopeLead: lead, Author: author,
	})
	if err != nil {
		return RequestListResult{}, err
	}
	res := RequestListResult{
		Requests: make([]RequestOut, len(rows)), Counts: map[string]int64{},
		Page: f.Page, PageSize: f.PageSize, CanRaise: canRaiseRequest(u),
	}
	for i, r := range rows {
		res.Total = r.Total
		res.Requests[i] = out(r.ClientRequestDetail)
	}
	for _, c := range counts {
		res.Counts[c.Status] = c.N
	}
	return res, nil
}

func (s *Service) GetRequest(ctx context.Context, u db.User, id uuid.UUID) (RequestDetailResult, error) {
	q := s.st.Queries
	r, err := s.visibleRequest(ctx, q, u, id)
	if err != nil {
		return RequestDetailResult{}, err
	}
	roster, ids, err := teamRoster(ctx, q, r)
	if err != nil {
		return RequestDetailResult{}, err
	}
	b, err := s.actorOnContainer(ctx, q, u, requestContainer(r, ids))
	if err != nil {
		return RequestDetailResult{}, err
	}
	res := RequestDetailResult{
		// Every list goes out as a list, even an empty one: a team with nobody on it must
		// not send the page a null to read.
		Request: out(r), Columns: Columns, Cards: []CardOut{}, Roster: roster,
		// The whole team arranges the board; handing the work out is the leader's.
		Activity: []db.ListActivityRow{}, CanMoveCard: b.movesCards() && b.open(),
		Permissions: RequestPermissions{
			CanEdit:      canRaiseRequest(u) && r.Status == requestOpen,
			CanMoveTeam:  policy.Role(u.Role) == policy.TL && r.Status == requestOpen,
			CanClose:     canRaiseRequest(u) && r.Status == requestOpen,
			CanCancel:    canRaiseRequest(u) && r.Status == requestOpen,
			CanReopen:    canRaiseRequest(u) && r.Status != requestOpen,
			CanAddCard:   b.canCreate(),
			CanWriteNote: b.isMember() && r.Status == requestOpen,
			CanAssign:    b.works() && b.open(),
		},
	}
	var only *string
	if b.isSales() {
		client := "client"
		only = &client
	}
	cards, err := q.ListCards(ctx, db.ListCardsParams{ContainerID: id, Source: only})
	if err != nil {
		return res, err
	}
	res.Cards = make([]CardOut, len(cards))
	for i, c := range cards {
		res.Cards[i] = b.out(c)
	}
	res.Activity, err = q.ListActivity(ctx, db.ListActivityParams{ClientRequestID: &id, Lim: 100})
	return res, err
}
