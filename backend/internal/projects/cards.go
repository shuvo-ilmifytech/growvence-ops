package projects

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"slices"
	"strings"

	"github.com/google/uuid"

	"github.com/solvinx/opsflow/internal/apperr"
	"github.com/solvinx/opsflow/internal/db"
	"github.com/solvinx/opsflow/internal/notify"
	"github.com/solvinx/opsflow/internal/policy"
	"github.com/solvinx/opsflow/internal/store"
	"github.com/solvinx/opsflow/internal/validate"
	"github.com/solvinx/opsflow/internal/workflow"
)

// The board: cards in four fixed columns, on a project or on a client request.
//
//   - Sales (any of them: they cover for each other) add client requests anywhere and
//     see every client request, never the Team Leader's tasks. They edit their own requests.
//   - The Team Leader adds internal tasks and sees, edits, moves and removes every card.
//   - The members working it see every card, move them and assign them.
//   - The CEO sees every card and changes nothing.

var Columns = []string{"todo", "in_progress", "review", "done"}

var columnNames = map[string]string{"todo": "To do", "in_progress": "In progress", "review": "Review", "done": "Done"}

type boardActor struct {
	u db.User
	c container
	// onTeam: on the team a client request is with, without holding any of its cards. A
	// request is the team's work, so the team arranges its board — handing the work out
	// stays with the Team Leader.
	onTeam bool
}

func (b boardActor) role() policy.Role { return policy.Role(b.u.Role) }
func (b boardActor) isSales() bool     { return b.role() == policy.Sales }
func (b boardActor) isMember() bool {
	return (b.role() == policy.Member || b.role() == policy.TL) && slices.Contains(b.c.Members, b.u.ID)
}
func (b boardActor) isLead() bool { return b.role() == policy.TL }

// works: the Team Leader and the members working it run the board.
func (b boardActor) works() bool { return b.isLead() || b.isMember() }

// movesCards: who may drag a card between columns — whoever works it, plus anyone on the
// team a client request is with, since the whole team reads and runs that board.
func (b boardActor) movesCards() bool { return b.works() || b.onTeam }

func (b boardActor) open() bool { return b.c.live() }

func (b boardActor) canCreate() bool { return b.open() && (b.isSales() || b.isLead()) }

func (b boardActor) seesCard(c db.CardDetail) bool {
	if b.isSales() {
		return c.Source == "client"
	}
	return true
}

func (b boardActor) canEdit(c db.CardDetail) bool {
	return b.open() && (b.isLead() || (b.isSales() && c.CreatedBy == b.u.ID))
}

func (b boardActor) canRemove(c db.CardDetail) bool {
	return b.isLead() || (b.isSales() && c.CreatedBy == b.u.ID && c.ColumnKey == "todo")
}

func (b boardActor) canComment(c db.CardDetail) bool {
	return b.open() && (b.works() || (b.isSales() && c.Source == "client"))
}

type CardOut struct {
	db.CardDetail
	CanEdit    bool `json:"can_edit"`
	CanRemove  bool `json:"can_remove"`
	CanComment bool `json:"can_comment"`
}

type BoardResult struct {
	Cards     []CardOut `json:"cards"`
	Columns   []string  `json:"columns"`
	CanCreate bool      `json:"can_create"`
	CanMove   bool      `json:"can_move"`   // drag cards between columns
	CanAssign bool      `json:"can_assign"` // hand a card to someone
	// Sales see client requests only (not the Team Leader's tasks); the UI says so.
	OwnOnly bool `json:"own_only"`
}

func (s *Service) boardFor(ctx context.Context, q *db.Queries, u db.User, projectID uuid.UUID) (boardActor, error) {
	p, err := lockVisible(ctx, q, u, projectID)
	return boardActor{u: u, c: projectContainer(p)}, err
}

// boardOn is boardFor for either container, from the ids a card carries.
func (s *Service) boardOn(ctx context.Context, q *db.Queries, u db.User, projectID, requestID *uuid.UUID) (boardActor, error) {
	c, err := s.lockContainer(ctx, q, u, projectID, requestID)
	if err != nil {
		return boardActor{u: u, c: c}, err
	}
	return s.actorOnContainer(ctx, q, u, c)
}

// actorOnContainer is the actor on a container, with whether they are on the team a client
// request is with — what lets the team arrange its board.
func (s *Service) actorOnContainer(ctx context.Context, q *db.Queries, u db.User, c container) (boardActor, error) {
	b := boardActor{u: u, c: c}
	if !c.Request || c.TeamID == nil {
		return b, nil
	}
	team, err := viewerTeam(ctx, q, u)
	if err != nil {
		return b, err
	}
	b.onTeam = onTeam(team, *c.TeamID)
	return b, nil
}

func (b boardActor) out(c db.CardDetail) CardOut {
	return CardOut{CardDetail: c, CanEdit: b.canEdit(c), CanRemove: b.canRemove(c), CanComment: b.canComment(c)}
}

// Board returns the cards the user may see, in column order.
func (s *Service) Board(ctx context.Context, u db.User, projectID uuid.UUID) (BoardResult, error) {
	var res BoardResult
	err := s.st.Tx(ctx, func(q *db.Queries) error {
		b, err := s.boardFor(ctx, q, u, projectID)
		if err != nil {
			return err
		}
		var only *string
		if b.isSales() {
			client := "client"
			only = &client
		}
		rows, err := q.ListCards(ctx, db.ListCardsParams{ContainerID: projectID, Source: only})
		if err != nil {
			return err
		}
		res = BoardResult{
			Cards: make([]CardOut, len(rows)), Columns: Columns, CanCreate: b.canCreate(),
			CanMove: b.movesCards() && b.open(), CanAssign: b.works() && b.open(), OwnOnly: only != nil,
		}
		for i, c := range rows {
			res.Cards[i] = b.out(c)
		}
		return nil
	})
	return res, err
}

type CardInput struct {
	Title       *string     `json:"title"`
	Description *string     `json:"description"`
	DueDate     *string     `json:"due_date"`
	AssigneeIDs []uuid.UUID `json:"assignee_ids"` // create only; the Team Leader's tasks
}

func checkCard(title, description string, due *string) validate.Errors {
	v := validate.Errors{}
	v.Len("title", title, 1, 200)
	v.MaxLen("description", description, 20000)
	if due != nil {
		parseDate(v, "due_date", due)
	}
	return v
}

// CreateCard adds a client request (Sales) or an internal task (Team Leader) to To do, on
// a project or on a client request.
func (s *Service) CreateCard(ctx context.Context, u db.User, projectID uuid.UUID, in CardInput) (CardOut, error) {
	return s.createCardOn(ctx, u, &projectID, nil, in)
}

// CreateRequestCard adds a card to a client request.
func (s *Service) CreateRequestCard(ctx context.Context, u db.User, requestID uuid.UUID, in CardInput) (CardOut, error) {
	return s.createCardOn(ctx, u, nil, &requestID, in)
}

func (s *Service) createCardOn(ctx context.Context, u db.User, projectID, requestID *uuid.UUID, in CardInput) (CardOut, error) {
	title, description := strings.TrimSpace(deref(in.Title)), strings.TrimSpace(deref(in.Description))
	if v := checkCard(title, description, in.DueDate); !v.OK() {
		return CardOut{}, apperr.Validation(v)
	}
	var out CardOut
	err := s.st.Tx(ctx, func(q *db.Queries) error {
		b, err := s.boardOn(ctx, q, u, projectID, requestID)
		if err != nil {
			return err
		}
		if !b.canCreate() {
			if (b.isSales() || b.isLead()) && !b.open() {
				if b.c.Request {
					return apperr.Conflict("A cancelled client request is closed")
				}
				return apperr.Conflict("A cancelled project's board is closed")
			}
			return apperr.Forbidden()
		}
		source := "internal"
		if b.isSales() {
			source = "client"
		}
		c, err := q.CreateCard(ctx, db.CreateCardParams{
			ProjectID: b.c.projectID(), ClientRequestID: b.c.requestID(), ContainerID: b.c.ID,
			Title: title, Description: description, Source: source, DueDate: in.DueDate, CreatedBy: u.ID,
		})
		if err != nil {
			return err
		}
		assignees := uniqueIDs(in.AssigneeIDs)
		if len(assignees) > 0 {
			if !b.isLead() {
				return apperr.Validation(validate.Errors{"assignee_ids": "are set by the Team Leader or the members working it"})
			}
			if err := setAssignees(ctx, q, b, c.ID, assignees); err != nil {
				return err
			}
		}
		d, err := q.GetCardDetail(ctx, c.ID)
		if err != nil {
			return err
		}
		out = b.out(d)
		if err := s.notifyCardCreated(ctx, q, b, d); err != nil {
			return err
		}
		if err := touch(ctx, q, b.c); err != nil {
			return err
		}
		return b.c.log(ctx, q, u.ID, "card.created", "card", &c.ID,
			map[string]any{"number": c.Number, "title": title, "source": source})
	})
	return out, err
}

// loadCard locks a card the user may see on a project they may see.
func (s *Service) loadCard(ctx context.Context, q *db.Queries, u db.User, cardID uuid.UUID) (boardActor, db.CardDetail, error) {
	row, err := q.LockCard(ctx, cardID)
	if store.IsNotFound(err) {
		return boardActor{}, db.CardDetail{}, apperr.NotFound("Card")
	} else if err != nil {
		return boardActor{}, db.CardDetail{}, err
	}
	b, err := s.boardOn(ctx, q, u, row.ProjectID, row.ClientRequestID)
	var ae *apperr.Error
	if errors.As(err, &ae) && ae.Status == http.StatusNotFound {
		return b, db.CardDetail{}, apperr.NotFound("Card")
	} else if err != nil {
		return b, db.CardDetail{}, err
	}
	d, err := q.GetCardDetail(ctx, cardID)
	if err != nil {
		return b, d, err
	}
	if !b.seesCard(d) {
		return b, d, apperr.NotFound("Card")
	}
	return b, d, nil
}

type CardView struct {
	Card        CardOut                  `json:"card"`
	Comments    []db.ListCardCommentsRow `json:"comments"`
	Attachments []db.ListCardFilesRow    `json:"attachments"`
	CanAttach   bool                     `json:"can_attach"`
}

func (s *Service) GetCard(ctx context.Context, u db.User, cardID uuid.UUID) (CardView, error) {
	var res CardView
	err := s.st.Tx(ctx, func(q *db.Queries) error {
		b, d, err := s.loadCard(ctx, q, u, cardID)
		if err != nil {
			return err
		}
		res.Card = b.out(d)
		res.CanAttach = b.canComment(d)
		if res.Comments, err = q.ListCardComments(ctx, cardID); err != nil {
			return err
		}
		res.Attachments, err = q.ListCardFiles(ctx, &cardID)
		return err
	})
	return res, err
}

// UpdateCard edits the title, description and due date.
func (s *Service) UpdateCard(ctx context.Context, u db.User, cardID uuid.UUID, in CardInput) error {
	return s.st.Tx(ctx, func(q *db.Queries) error {
		b, d, err := s.loadCard(ctx, q, u, cardID)
		if err != nil {
			return err
		}
		if !b.canEdit(d) {
			return apperr.Forbidden()
		}
		title, description, due := d.Title, d.Description, d.DueDate
		if in.Title != nil {
			title = strings.TrimSpace(*in.Title)
		}
		if in.Description != nil {
			description = strings.TrimSpace(*in.Description)
		}
		if in.DueDate != nil {
			due = in.DueDate
			if *due == "" {
				due = nil
			}
		}
		if v := checkCard(title, description, due); !v.OK() {
			return apperr.Validation(v)
		}
		if in.AssigneeIDs != nil {
			return apperr.Validation(validate.Errors{"assignee_ids": "are changed with the assignees action"})
		}
		if err := q.UpdateCard(ctx, db.UpdateCardParams{ID: cardID, Title: title, Description: description, DueDate: due}); err != nil {
			return err
		}
		return b.c.log(ctx, q, u.ID, "card.updated", "card", &cardID,
			map[string]any{"number": d.Number, "title": title})
	})
}

type MoveInput struct {
	Column string `json:"column"`
	Index  int    `json:"index"` // position within the target column, 0 = top
}

// MoveCard drags a card to a column and position; the column is renumbered.
func (s *Service) MoveCard(ctx context.Context, u db.User, cardID uuid.UUID, in MoveInput) error {
	v := validate.Errors{}
	v.OneOf("column", in.Column, Columns...)
	if in.Index < 0 {
		v.Add("index", "can't be negative")
	}
	if !v.OK() {
		return apperr.Validation(v)
	}
	return s.st.Tx(ctx, func(q *db.Queries) error {
		b, d, err := s.loadCard(ctx, q, u, cardID)
		if err != nil {
			return err
		}
		if !b.movesCards() || !b.open() {
			return apperr.Forbidden()
		}
		ids, err := q.ColumnCardIDs(ctx, db.ColumnCardIDsParams{ContainerID: d.ContainerID, ColumnKey: in.Column})
		if err != nil {
			return err
		}
		ids = slices.DeleteFunc(ids, func(id uuid.UUID) bool { return id == cardID })
		at := min(in.Index, len(ids))
		ids = slices.Insert(ids, at, cardID)
		for i, id := range ids {
			col := in.Column
			if err := q.PlaceCard(ctx, db.PlaceCardParams{ID: id, ColumnKey: col, Position: int32(i + 1)}); err != nil {
				return err
			}
		}
		if d.ColumnKey == in.Column {
			return nil // reordered within the column: no news
		}
		if err := s.notifyCardMoved(ctx, q, b, d, in.Column); err != nil {
			return err
		}
		if err := touch(ctx, q, b.c); err != nil {
			return err
		}
		return b.c.log(ctx, q, u.ID, "card.moved", "card", &cardID,
			map[string]any{"number": d.Number, "title": d.Title, "from": d.ColumnKey, "to": in.Column})
	})
}

func setAssignees(ctx context.Context, q *db.Queries, b boardActor, cardID uuid.UUID, ids []uuid.UUID) error {
	for _, id := range ids {
		if !slices.Contains(b.c.Assignable, id) {
			who := "must be members assigned to this project"
			if b.c.Request {
				who = "must be on the team the request is with"
			}
			return apperr.Validation(validate.Errors{"assignee_ids": who})
		}
	}
	if err := q.ClearCardAssignees(ctx, cardID); err != nil {
		return err
	}
	for _, id := range ids {
		if err := q.AddCardAssignee(ctx, db.AddCardAssigneeParams{CardID: cardID, UserID: id}); err != nil {
			return err
		}
	}
	return nil
}

// SetCardAssignees replaces who works on a card (any of the project's members).
func (s *Service) SetCardAssignees(ctx context.Context, u db.User, cardID uuid.UUID, ids []uuid.UUID) error {
	ids = uniqueIDs(ids)
	return s.st.Tx(ctx, func(q *db.Queries) error {
		b, d, err := s.loadCard(ctx, q, u, cardID)
		if err != nil {
			return err
		}
		if !b.works() || !b.open() {
			return apperr.Forbidden()
		}
		if err := setAssignees(ctx, q, b, cardID, ids); err != nil {
			return err
		}
		var added []uuid.UUID
		for _, id := range ids {
			if !slices.Contains(d.AssigneeIDs, id) {
				added = append(added, id)
			}
		}
		if err := notify.Send(ctx, q, notify.Recipients{}.Add(added...), u.ID, b.c.note(notify.Notification{
			Type: notify.CardAssigned, Link: cardLink(d),
			Title: u.Name + " gave you a card: " + d.Title, Body: b.c.Label,
		})); err != nil {
			return err
		}
		return b.c.log(ctx, q, u.ID, "card.assigned", "card", &cardID,
			map[string]any{"number": d.Number, "title": d.Title, "assignees": ids})
	})
}

// RemoveCard hides a card from the board (kept for history).
func (s *Service) RemoveCard(ctx context.Context, u db.User, cardID uuid.UUID) error {
	return s.st.Tx(ctx, func(q *db.Queries) error {
		b, d, err := s.loadCard(ctx, q, u, cardID)
		if err != nil {
			return err
		}
		if !b.canRemove(d) {
			if b.isSales() && d.CreatedBy == u.ID {
				return apperr.Conflict("Ops has started on this request; ask the Team Leader to remove it")
			}
			return apperr.Forbidden()
		}
		if err := q.RemoveCard(ctx, db.RemoveCardParams{ID: cardID, RemovedBy: &u.ID}); err != nil {
			return err
		}
		return b.c.log(ctx, q, u.ID, "card.removed", "card", &cardID,
			map[string]any{"number": d.Number, "title": d.Title})
	})
}

func (s *Service) CommentCard(ctx context.Context, u db.User, cardID uuid.UUID, body string) (db.CardComment, error) {
	body = strings.TrimSpace(body)
	v := validate.Errors{}
	v.Len("body", body, 1, 10000)
	if !v.OK() {
		return db.CardComment{}, apperr.Validation(v)
	}
	var c db.CardComment
	err := s.st.Tx(ctx, func(q *db.Queries) error {
		b, d, err := s.loadCard(ctx, q, u, cardID)
		if err != nil {
			return err
		}
		if !b.canComment(d) {
			return apperr.Forbidden()
		}
		if c, err = q.CreateCardComment(ctx, db.CreateCardCommentParams{CardID: cardID, AuthorID: u.ID, Body: body}); err != nil {
			return err
		}
		// The card's people: who added it, who works on it, and the Team Leader.
		to := s.leadsOn(ctx, q, b.c).Add(d.CreatedBy).Add(d.AssigneeIDs...)
		if err := notify.Batch(ctx, q, to, u.ID, b.c.projectID(), b.c.requestID(), s.batchWindow(ctx, q), notify.CardComments,
			fmt.Sprintf("New comments on card #%d %s", d.Number, d.Title), cardLink(d), u.Name+": "+quote(body)); err != nil {
			return err
		}
		return b.c.log(ctx, q, u.ID, "card.commented", "card", &cardID,
			map[string]any{"number": d.Number, "title": d.Title})
	})
	return c, err
}

// ── Notifications ──────────────────────────────────────────

func cardLink(d db.CardDetail) string {
	if d.ClientRequestID != nil {
		return requestLink(*d.ClientRequestID) + "?card=" + d.ID.String()
	}
	return projectLink(d.ContainerID) + "?card=" + d.ID.String()
}

func (s *Service) projectLabel(ctx context.Context, q *db.Queries, id uuid.UUID) string {
	p, err := q.GetProjectDetail(ctx, id)
	if err != nil {
		return ""
	}
	return label(p.Number, p.Name)
}

// notifyCardCreated: a client request goes to the Team Leader and the people working it;
// a Team Leader's task to whoever it was given to.
func (s *Service) notifyCardCreated(ctx context.Context, q *db.Queries, b boardActor, d db.CardDetail) error {
	lbl := b.c.Label
	if d.Source == "client" {
		if !b.c.Request && workflow.Status(b.c.Status) == workflow.Draft {
			return nil // nobody else can see a draft yet
		}
		return notify.Send(ctx, q, s.leadsOn(ctx, q, b.c).Add(b.c.Members...), b.u.ID, b.c.note(notify.Notification{
			Type: notify.CardCreated, Link: cardLink(d), Important: true,
			Title: "New client request on " + lbl + ": " + d.Title, Body: "From " + b.u.Name,
		}))
	}
	return notify.Send(ctx, q, notify.Recipients{}.Add(d.AssigneeIDs...), b.u.ID, b.c.note(notify.Notification{
		Type: notify.CardAssigned, Link: cardLink(d),
		Title: b.u.Name + " gave you a card: " + d.Title, Body: lbl,
	}))
}

// notifyCardMoved: Review asks the Team Leader to check; Done tells Sales their request is done.
func (s *Service) notifyCardMoved(ctx context.Context, q *db.Queries, b boardActor, d db.CardDetail, to string) error {
	lbl := b.c.Label
	switch to {
	case "review":
		return notify.Send(ctx, q, s.leadsOn(ctx, q, b.c), b.u.ID, b.c.note(notify.Notification{
			Type: notify.CardReview, Link: cardLink(d),
			Title: "Card ready for review: " + d.Title, Body: lbl + " · moved by " + b.u.Name,
		}))
	case "done":
		if d.Source != "client" {
			return nil
		}
		// Whoever logged it, and the salesperson it belongs to if a colleague logged it for them.
		return notify.Send(ctx, q, notify.Recipients{}.Add(d.CreatedBy, b.c.SalesID), b.u.ID, b.c.note(notify.Notification{
			Type: notify.CardDone, Link: cardLink(d),
			Title: "Client request done: " + d.Title, Body: lbl + " · " + columnNames[to],
		}))
	}
	return nil
}

// ── All my cards ───────────────────────────────────────────

type AllCardsFilter struct {
	Project  *uuid.UUID
	Request  *uuid.UUID
	Team     *uuid.UUID
	Assignee *uuid.UUID
	Author   *uuid.UUID
	Source   *string
	Q        *string
}

type BoardCardOut struct {
	CardOut
	ProjectNumber   int64   `json:"project_number"`
	ProjectName     *string `json:"project_name"`
	ProjectTeamName *string `json:"project_team_name"`
}

// BoardRequest is a client request on the all-projects board: a container of cards for a
// client with no project. Shaped like BoardProject so the board treats them alike.
type BoardRequest struct {
	ID          uuid.UUID   `json:"id"`
	Number      int64       `json:"number"`
	Title       string      `json:"title"`
	Status      string      `json:"status"`
	TeamName    string      `json:"team_name"`
	ClientName  string      `json:"client_name"`
	OrderID     *string     `json:"order_id"`
	ProfileName string      `json:"profile_name"`
	MemberIDs   []uuid.UUID `json:"member_ids"`
	MemberNames []string    `json:"member_names"`
	CanCreate   bool        `json:"can_create"`
	CanMove     bool        `json:"can_move"`   // drag its cards between columns
	CanAssign   bool        `json:"can_assign"` // hand its cards to someone
}

// BoardProject is a project on the all-projects board: its members (for assigning
// cards) and what the viewer may do there.
type BoardProject struct {
	ID       uuid.UUID `json:"id"`
	Number   int64     `json:"number"`
	Name     *string   `json:"name"`
	Status   string    `json:"status"`
	TeamName *string   `json:"team_name"`
	// How Sales recognise a project: the client and the order.
	ClientName    *string     `json:"client_name"`
	ClientContact *string     `json:"client_contact"` // usually the buyer's marketplace username
	OrderID       *string     `json:"order_id"`
	ProfileName   *string     `json:"profile_name"`
	AgreedPrice   *string     `json:"agreed_price"`
	Currency      string      `json:"currency"`
	MemberIDs     []uuid.UUID `json:"member_ids"`
	MemberNames   []string    `json:"member_names"`
	CanCreate     bool        `json:"can_create"`
	CanMove       bool        `json:"can_move"`
	CanAssign     bool        `json:"can_assign"`
}

type AllCardsResult struct {
	Cards    []BoardCardOut `json:"cards"`
	Projects []BoardProject `json:"projects"`
	Requests []BoardRequest `json:"requests"`
	Columns  []string       `json:"columns"`
	OwnOnly  bool           `json:"own_only"` // Sales: client requests only
}

func actorOn(u db.User, id, createdBy uuid.UUID, status string, members []uuid.UUID) boardActor {
	return boardActor{u: u, c: container{ID: id, SalesID: createdBy, Status: status, Members: members, Assignable: members}}
}

// actorOnRequest is actorOn for a client request. Its cards can be given to anyone on the
// team it is with, which the list doesn't carry, so assigning is checked again on write.
// viewerTeam is the team the viewer is on, so the whole team can arrange its board.
func actorOnRequest(u db.User, id, salesID uuid.UUID, status string, members []uuid.UUID, team uuid.UUID, viewerTeam *uuid.UUID) boardActor {
	return boardActor{
		u: u, onTeam: onTeam(viewerTeam, team),
		c: container{ID: id, Request: true, SalesID: salesID, Status: status, Members: members, TeamID: &team},
	}
}

// AllCards is every card the user can see across their projects, in one board.
func (s *Service) AllCards(ctx context.Context, u db.User, f AllCardsFilter) (AllCardsResult, error) {
	sc := scopeFor(u)
	// A filter for one project asks for that project's cards alone, and one for a client
	// request for that request's; with neither, both containers' cards share the board.
	var rows []db.ListAllCardsRow
	var err error
	if f.Request == nil {
		rows, err = s.st.ListAllCards(ctx, db.ListAllCardsParams{
			ScopeCreator: sc.creator, ScopeSales: sc.sales, ScopeAssignee: sc.assignee, ScopeLead: sc.lead, HideDrafts: sc.hideDrafts,
			Project: f.Project, Team: f.Team, Assignee: f.Assignee, Author: f.Author, Source: f.Source, Q: f.Q,
		})
		if err != nil {
			return AllCardsResult{}, err
		}
	}
	var reqRows []db.ListAllRequestCardsRow
	if f.Project == nil {
		reqRows, err = s.st.ListAllRequestCards(ctx, db.ListAllRequestCardsParams{
			ScopeSales: sc.sales, ScopeAssignee: sc.assignee, ScopeLead: sc.lead,
			Request: f.Request, Team: f.Team, Assignee: f.Assignee, Author: f.Author, Source: f.Source, Q: f.Q,
		})
		if err != nil {
			return AllCardsResult{}, err
		}
	}
	team, err := viewerTeam(ctx, s.st.Queries, u)
	if err != nil {
		return AllCardsResult{}, err
	}
	res := AllCardsResult{Cards: make([]BoardCardOut, 0, len(rows)+len(reqRows)), Columns: Columns, OwnOnly: policy.Role(u.Role) == policy.Sales}
	for _, r := range rows {
		b := actorOn(u, r.CardDetail.ContainerID, r.ProjectSalesID, r.ProjectStatus, r.ProjectMemberIDs)
		res.Cards = append(res.Cards, BoardCardOut{CardOut: b.out(r.CardDetail), ProjectNumber: r.ProjectNumber,
			ProjectName: r.ProjectName, ProjectTeamName: r.ProjectTeamName})
	}
	for _, r := range reqRows {
		b := actorOnRequest(u, r.CardDetail.ContainerID, r.RequestSalesID, r.RequestStatus, r.RequestMemberIDs,
			r.RequestTeamID, team)
		title, team := r.RequestTitle, r.RequestTeamName
		res.Cards = append(res.Cards, BoardCardOut{CardOut: b.out(r.CardDetail), ProjectNumber: r.RequestNumber,
			ProjectName: &title, ProjectTeamName: &team})
	}
	sortBoard(res.Cards)
	if res.Requests, err = s.boardRequests(ctx, u, team); err != nil {
		return AllCardsResult{}, err
	}
	// Every open project the user can see, for the project filter, adding cards and assigning.
	open := []string{}
	for _, st := range workflow.All {
		if st != workflow.Cancelled {
			open = append(open, string(st))
		}
	}
	list, err := s.List(ctx, u, ListFilter{Statuses: open, Page: 1, PageSize: 500})
	if err != nil {
		return AllCardsResult{}, err
	}
	res.Projects = make([]BoardProject, len(list.Projects))
	for i, p := range list.Projects {
		b := actorOn(u, p.ID, p.CreatedBy, p.Status, p.MemberIDs)
		res.Projects[i] = BoardProject{
			ID: p.ID, Number: p.Number, Name: p.Name, Status: p.Status, TeamName: p.TeamName,
			ClientName: p.ClientName, ClientContact: p.ClientContact, OrderID: p.OrderID, ProfileName: p.ProfileName,
			AgreedPrice: p.AgreedPrice, Currency: p.Currency,
			MemberIDs: p.MemberIDs, MemberNames: p.MemberNames, CanCreate: b.canCreate(),
			CanMove: b.works() && b.open(), CanAssign: b.works() && b.open(),
		}
	}
	return res, nil
}

// sortBoard puts the cards back in board order after the two containers are merged:
// column, then what is due soonest, then what moved last.
func sortBoard(cards []BoardCardOut) {
	rank := map[string]int{"todo": 0, "in_progress": 1, "review": 2, "done": 3}
	slices.SortStableFunc(cards, func(a, b BoardCardOut) int {
		if n := rank[a.ColumnKey] - rank[b.ColumnKey]; n != 0 {
			return n
		}
		switch {
		case a.DueDate == nil && b.DueDate != nil:
			return 1
		case a.DueDate != nil && b.DueDate == nil:
			return -1
		case a.DueDate != nil && *a.DueDate != *b.DueDate:
			return strings.Compare(*a.DueDate, *b.DueDate)
		}
		return b.UpdatedAt.Compare(a.UpdatedAt)
	})
}

// boardRequests are the open client requests the user can see, for the board's filter,
// adding cards and assigning them. team is the team the viewer is on.
func (s *Service) boardRequests(ctx context.Context, u db.User, team *uuid.UUID) ([]BoardRequest, error) {
	list, err := s.ListRequests(ctx, u, RequestFilter{Statuses: []string{requestOpen, requestDone}, Page: 1, PageSize: 500})
	if err != nil {
		return nil, err
	}
	out := make([]BoardRequest, len(list.Requests))
	for i, r := range list.Requests {
		b := actorOnRequest(u, r.ID, r.CreatedBy, r.Status, r.MemberIDs, r.TeamID, team)
		out[i] = BoardRequest{
			ID: r.ID, Number: r.Number, Title: r.Title, Status: r.Status, TeamName: r.TeamName,
			ClientName: r.ClientName, OrderID: r.OrderID, ProfileName: r.ProfileName,
			MemberIDs: r.MemberIDs, MemberNames: r.MemberNames,
			CanCreate: b.canCreate(), CanMove: b.movesCards() && b.open(), CanAssign: b.works() && b.open(),
		}
	}
	return out, nil
}

// ── Attachments ────────────────────────────────────────────
// Whoever may comment on a card may attach files to it (Sales: the client's files on a
// client request; the Team Leader and members: anything). The uploader or the TL removes them.

func (s *Service) InitCardUpload(ctx context.Context, u db.User, cardID uuid.UUID, in UploadInput) (UploadTicket, error) {
	name, ctype, err := s.cleanUpload(in)
	if err != nil {
		return UploadTicket{}, err
	}
	var c container
	err = s.st.Tx(ctx, func(q *db.Queries) error {
		b, d, err := s.loadCard(ctx, q, u, cardID)
		if err != nil {
			return err
		}
		if !b.canComment(d) {
			return apperr.Forbidden()
		}
		c = b.c
		return nil
	})
	if err != nil {
		return UploadTicket{}, err
	}
	return s.issueTicket(ctx, pendingUpload{
		ProjectID: c.projectID(), RequestID: c.requestID(), CardID: &cardID,
		UserID: u.ID, Name: name, Size: in.Size, ContentType: ctype,
	})
}

func (s *Service) CompleteCardUpload(ctx context.Context, u db.User, cardID uuid.UUID, uploadID string) error {
	p, obj, sum, err := s.takePending(ctx, u, uploadID)
	if err == nil && (p.CardID == nil || *p.CardID != cardID) {
		err = apperr.NotFound("Upload")
	}
	if err != nil {
		return err
	}
	err = s.st.Tx(ctx, func(q *db.Queries) error {
		b, d, err := s.loadCard(ctx, q, u, cardID)
		if err != nil {
			return err
		}
		if !b.canComment(d) {
			return apperr.Forbidden()
		}
		file, err := q.CreateFile(ctx, db.CreateFileParams{
			ProjectID: b.c.projectID(), ClientRequestID: b.c.requestID(), CardID: &cardID,
			OriginalName: p.Name, UploadedBy: u.ID,
		})
		if err != nil {
			return err
		}
		if _, err := storeVersion(ctx, q, file.ID, p, obj, sum, u.ID); err != nil {
			return err
		}
		if err := touch(ctx, q, b.c); err != nil {
			return err
		}
		return b.c.log(ctx, q, u.ID, "card.attachment_added", "card", &cardID,
			map[string]any{"number": d.Number, "title": d.Title, "name": p.Name})
	})
	if err != nil {
		s.discard(ctx, p.Key)
	}
	return err
}

func (s *Service) RemoveCardAttachment(ctx context.Context, u db.User, cardID, fileID uuid.UUID) error {
	return s.st.Tx(ctx, func(q *db.Queries) error {
		b, d, err := s.loadCard(ctx, q, u, cardID)
		if err != nil {
			return err
		}
		f, err := q.LockFile(ctx, fileID)
		if store.IsNotFound(err) || (err == nil && (f.CardID == nil || *f.CardID != cardID || f.RemovedAt != nil)) {
			return apperr.NotFound("Attachment")
		} else if err != nil {
			return err
		}
		if !b.open() || !(b.isLead() || (f.UploadedBy == u.ID && b.canComment(d))) {
			return apperr.Forbidden()
		}
		if err := q.RemoveFile(ctx, fileID); err != nil {
			return err
		}
		return b.c.log(ctx, q, u.ID, "card.attachment_removed", "card", &cardID,
			map[string]any{"number": d.Number, "title": d.Title, "name": f.OriginalName})
	})
}
