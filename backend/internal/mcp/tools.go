package mcp

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/solvinx/opsflow/internal/apperr"
	"github.com/solvinx/opsflow/internal/db"
	"github.com/solvinx/opsflow/internal/policy"
	"github.com/solvinx/opsflow/internal/projects"
	"github.com/solvinx/opsflow/internal/reports"
	"github.com/solvinx/opsflow/internal/store"
	"github.com/solvinx/opsflow/internal/workflow"
)

// errBadArgs marks a tool call whose arguments don't make sense; the message is for the assistant.
var errBadArgs = errors.New("bad arguments")

func badArgs(format string, a ...any) error {
	return fmt.Errorf("%w: %s", errBadArgs, fmt.Sprintf(format, a...))
}

type tool struct {
	name, title, description string
	input                    map[string]any
	// write: changes OpsFlow. Hidden from read-only roles, and logged as made by an assistant.
	write bool
	// who may use it, beyond write; nil means everyone who works on projects.
	who func(db.User) bool
	run func(ctx context.Context, s *Server, u db.User, args json.RawMessage) (any, error)
}

// worksOnProjects: HR and the Office Admin have no project access in the portal, so no
// project tools either.
func worksOnProjects(u db.User) bool {
	switch policy.Role(u.Role) {
	case policy.Sales, policy.TL, policy.Member, policy.CEO:
		return true
	}
	return false
}

// WithoutProjects reports whether this person gets no project tools at all, which the
// consent page says in place of the usual list.
func WithoutProjects(u db.User) bool { return !worksOnProjects(u) }

func (t *tool) allowed(u db.User) bool {
	if t.name == "whoami" {
		return true
	}
	if !worksOnProjects(u) {
		return false
	}
	// The CEO oversees projects but never operates them: the portal refuses their writes.
	if t.write && policy.IsReadOnly(policy.Role(u.Role)) {
		return false
	}
	return t.who == nil || t.who(u)
}

func can(a policy.Action) func(db.User) bool {
	return func(u db.User) bool { return policy.Can(policy.Role(u.Role), a) }
}

func role(r policy.Role) func(db.User) bool {
	return func(u db.User) bool { return policy.Role(u.Role) == r }
}

func (s *Server) tool(name string) *tool {
	for i := range tools {
		if tools[i].name == name {
			return &tools[i]
		}
	}
	return nil
}

// list is the tools this person may use, in a fixed order.
func (s *Server) list(u db.User) []map[string]any {
	out := []map[string]any{}
	for i := range tools {
		t := &tools[i]
		if !t.allowed(u) {
			continue
		}
		out = append(out, map[string]any{
			"name": t.name, "title": t.title, "description": t.description, "inputSchema": t.input,
			"annotations": map[string]any{
				"title": t.title, "readOnlyHint": !t.write, "destructiveHint": false,
				"idempotentHint": !t.write, "openWorldHint": false,
			},
		})
	}
	return out
}

// ── schema helpers ─────────────────────────────────────────

func obj(required []string, props map[string]any) map[string]any {
	m := map[string]any{"type": "object", "properties": props, "additionalProperties": false}
	if len(required) > 0 {
		m["required"] = required
	}
	return m
}

func str(desc string) map[string]any { return map[string]any{"type": "string", "description": desc} }

func enum(desc string, values ...string) map[string]any {
	return map[string]any{"type": "string", "description": desc, "enum": values}
}

var projectRef = map[string]any{
	"type": "string", "description": `The project's number ("12" or "#12") or its id.`,
}

func decode(args json.RawMessage, v any) error {
	d := json.NewDecoder(strings.NewReader(string(args)))
	d.DisallowUnknownFields()
	if err := d.Decode(v); err != nil {
		return badArgs("%v", err)
	}
	return nil
}

// projectID resolves "12", "#12" or a UUID. Whether the person may see it is up to the service.
func (s *Server) projectID(ctx context.Context, ref string) (uuid.UUID, error) {
	ref = strings.TrimSpace(ref)
	if id, err := uuid.Parse(ref); err == nil {
		return id, nil
	}
	n, err := strconv.ParseInt(strings.TrimPrefix(ref, "#"), 10, 64)
	if err != nil {
		return uuid.Nil, badArgs("project must be a project number like 12 or #12, or a project id")
	}
	id, err := s.st.ProjectIDByNumber(ctx, n)
	if store.IsNotFound(err) {
		return uuid.Nil, apperr.NotFound("Project")
	}
	return id, err
}

func uuidArg(name, v string) (uuid.UUID, error) {
	id, err := uuid.Parse(strings.TrimSpace(v))
	if err != nil {
		return uuid.Nil, badArgs("%s must be an id", name)
	}
	return id, nil
}

// ── compact shapes (what an assistant needs, not every column) ─────────

type projectBrief struct {
	ID          uuid.UUID `json:"id"`
	Number      int64     `json:"number"`
	Name        string    `json:"name"`
	Client      string    `json:"client"`
	OrderID     string    `json:"order_id,omitempty"`
	Type        string    `json:"type,omitempty"`
	Team        string    `json:"team,omitempty"`
	Status      string    `json:"status"`
	Priority    string    `json:"priority"`
	Deadline    string    `json:"delivery_deadline,omitempty"`
	Due         string    `json:"due,omitempty"`
	Overdue     bool      `json:"overdue"`
	Members     []string  `json:"members"`
	StoreAccess string    `json:"store_access"` // not_requested, requested or given
	CreatedBy   string    `json:"created_by"`
	LastChanged time.Time `json:"last_activity_at"`
	URL         string    `json:"url"`
}

func deref(p *string) string {
	if p == nil {
		return ""
	}
	return *p
}

func (s *Server) brief(p projects.ProjectOut) projectBrief {
	return projectBrief{
		ID: p.ID, Number: p.Number, Name: deref(p.Name), Client: deref(p.ClientName), OrderID: deref(p.OrderID),
		Type: deref(p.ProjectTypeName), Team: deref(p.TeamName), Status: p.Status, Priority: p.Priority,
		Deadline: deref(p.Deadline), Due: deref(p.EffectiveDue), Overdue: p.IsOverdue, Members: p.MemberNames,
		StoreAccess: p.StoreAccess,
		CreatedBy:   p.CreatedByName, LastChanged: p.LastActivityAt, URL: fmt.Sprintf("%s/projects/%s", s.portal, p.ID),
	}
}

type cardBrief struct {
	ID uuid.UUID `json:"id"`
	// A card is on a project or on a client request (a client with no project).
	ProjectID       *uuid.UUID `json:"project_id,omitempty"`
	ClientRequestID *uuid.UUID `json:"client_request_id,omitempty"`
	Number          int32      `json:"number"`
	Title           string     `json:"title"`
	Column          string     `json:"column"`
	Kind            string     `json:"kind"` // client_request or task
	Due             string     `json:"due_date,omitempty"`
	Assignees       []string   `json:"assignees"`
	Comments        int64      `json:"comments"`
}

func cardOf(c db.CardDetail) cardBrief {
	kind := "task"
	if c.Source == "client" {
		kind = "client_request"
	}
	return cardBrief{
		ID: c.ID, ProjectID: c.ProjectID, ClientRequestID: c.ClientRequestID,
		Number: c.Number, Title: c.Title, Column: c.ColumnKey, Kind: kind,
		Due: deref(c.DueDate), Assignees: c.AssigneeNames, Comments: c.CommentCount,
	}
}

type updateBrief struct {
	ID          uuid.UUID `json:"id"`
	Number      int64     `json:"number"`
	Project     int64     `json:"project_number"`
	ProjectName string    `json:"project_name"`
	Author      string    `json:"author"`
	Status      string    `json:"status"`
	Destination string    `json:"destination"`
	Message     string    `json:"message"`
	CreatedAt   time.Time `json:"created_at"`
}

func updateOf(u db.UpdateDetail) updateBrief {
	return updateBrief{
		ID: u.ID, Number: u.Number, Project: u.ProjectNumber, ProjectName: deref(u.ProjectName), Author: u.AuthorName,
		Status: u.Status, Destination: u.Destination, Message: u.Message, CreatedAt: u.CreatedAt,
	}
}

// ── my work: the My work page, built on the server ─────────

// What each role is waiting to act on, as on the My work page.
var mine = map[policy.Role]struct {
	projects, updates []string
	title             string
}{
	policy.Sales:  {[]string{"ready_to_deliver", "delivered"}, []string{"approved"}, "Projects to deliver or close"},
	policy.TL:     {[]string{"new", "ready_for_review", "blocked"}, []string{"pending_approval"}, "Projects waiting on you"},
	policy.Member: {[]string{"in_progress", "blocked"}, []string{"draft", "changes_requested"}, "Projects you're working on"},
	policy.CEO:    {[]string{"blocked", "new"}, []string{"approved"}, "Projects that need a push"},
}

func myWork(ctx context.Context, s *Server, u db.User, _ json.RawMessage) (any, error) {
	r := policy.Role(u.Role)
	m := mine[r]
	out := map[string]any{"role": r}

	f := projects.ListFilter{Statuses: m.projects, Sort: "due", Page: 1, PageSize: 25}
	if r == policy.Member {
		f.Assignee = &u.ID
	}
	list, err := s.projects.List(ctx, u, f)
	if err != nil {
		return nil, err
	}
	out["waiting_on_you"] = map[string]any{"title": m.title, "projects": s.briefs(list.Projects), "total": list.Total}

	// A Team Leader may also be doing projects themselves.
	if r == policy.TL {
		own, err := s.projects.List(ctx, u, projects.ListFilter{
			Statuses: []string{"in_progress", "blocked", "ready_for_review", "ready_to_deliver"}, Assignee: &u.ID, Sort: "due", Page: 1, PageSize: 25,
		})
		if err != nil {
			return nil, err
		}
		out["working_on"] = s.briefs(own.Projects)
	}

	cf := projects.AllCardsFilter{Assignee: &u.ID}
	if r == policy.Sales {
		cf = projects.AllCardsFilter{Author: &u.ID}
	}
	cards, err := s.projects.AllCards(ctx, u, cf)
	if err != nil {
		return nil, err
	}
	open := []cardBrief{}
	for _, c := range cards.Cards {
		if c.ColumnKey != "done" {
			open = append(open, cardOf(c.CardDetail))
		}
	}
	slices.SortStableFunc(open, func(a, b cardBrief) int {
		switch {
		case a.Due == b.Due:
			return 0
		case a.Due == "":
			return 1
		case b.Due == "":
			return -1
		}
		return strings.Compare(a.Due, b.Due)
	})
	out["open_cards"] = open

	ups, err := s.projects.ListUpdates(ctx, u, projects.UpdateFilter{Statuses: m.updates, Page: 1, PageSize: 25})
	if err != nil {
		return nil, err
	}
	updates := make([]updateBrief, len(ups.Updates))
	for i, x := range ups.Updates {
		updates[i] = updateOf(x.UpdateDetail)
	}
	out["updates_waiting"] = updates
	return out, nil
}

func (s *Server) briefs(ps []projects.ProjectOut) []projectBrief {
	out := make([]projectBrief, len(ps))
	for i, p := range ps {
		out[i] = s.brief(p)
	}
	return out
}

// ── the tools ──────────────────────────────────────────────

var statusList = func() []string {
	out := make([]string, len(workflow.All))
	for i, s := range workflow.All {
		out[i] = string(s)
	}
	return out
}()

var tools = []tool{
	{
		name: "whoami", title: "Who am I",
		description: "The person this assistant acts for in OpsFlow: name, role, and whether they can change things. Call this first.",
		input:       obj(nil, map[string]any{}),
		run: func(ctx context.Context, s *Server, u db.User, _ json.RawMessage) (any, error) {
			r := policy.Role(u.Role)
			note := map[policy.Role]string{
				policy.Sales:       "Sales: creates projects from marketplace orders, logs client requests, sends approved updates to clients.",
				policy.TL:          "Team Leader: assigns and reviews their teams' projects, approves client updates, and may work projects themselves.",
				policy.Member:      "Team member: works the projects assigned to them and writes client updates.",
				policy.CEO:         "CEO: sees everything, changes nothing on projects (read-only here too).",
				policy.HR:          "HR: people, attendance and leave. OpsFlow's project tools are not available to HR.",
				policy.OfficeAdmin: "Office Admin: raises invoices for what the office needs. OpsFlow's project tools are not available to them.",
			}[r]
			return map[string]any{
				"id": u.ID, "name": u.Name, "email": u.Email, "role": r, "about_role": note,
				"can_change_projects": worksOnProjects(u) && !policy.IsReadOnly(r), "portal": s.portal,
			}, nil
		},
	},
	{
		name: "my_work", title: "My work",
		description: "What is waiting on this person right now: projects that need them, projects they're working on, their open board cards (soonest due first) and client updates waiting on them. The best starting point for \"what should I do next?\".",
		input:       obj(nil, map[string]any{}),
		run:         myWork,
	},
	{
		name: "list_projects", title: "List projects",
		description: "Projects this person can see, newest activity first. Filter by status, a text search (name, client, client username, order ID, number), the person assigned, or overdue only.",
		input: obj(nil, map[string]any{
			"status":   map[string]any{"type": "array", "items": map[string]any{"type": "string", "enum": statusList}, "description": "Only these statuses."},
			"q":        str("Search text: project name, client, client username, order ID or number."),
			"assignee": str(`"me", or a person's id.`),
			"overdue":  map[string]any{"type": "boolean", "description": "Only projects past their due date."},
			"page":     map[string]any{"type": "integer", "minimum": 1, "description": "Page of 25, from 1."},
		}),
		run: func(ctx context.Context, s *Server, u db.User, args json.RawMessage) (any, error) {
			var a struct {
				Status   []string `json:"status"`
				Q        string   `json:"q"`
				Assignee string   `json:"assignee"`
				Overdue  bool     `json:"overdue"`
				Page     int      `json:"page"`
			}
			if err := decode(args, &a); err != nil {
				return nil, err
			}
			f := projects.ListFilter{Statuses: a.Status, Overdue: a.Overdue, Sort: "activity", Page: max(a.Page, 1), PageSize: 25}
			for _, st := range a.Status {
				if !workflow.Status(st).Valid() {
					return nil, badArgs("unknown status %q", st)
				}
			}
			if q := strings.TrimSpace(a.Q); q != "" {
				f.Q = &q
			}
			switch a.Assignee {
			case "":
			case "me":
				f.Assignee = &u.ID
			default:
				id, err := uuidArg("assignee", a.Assignee)
				if err != nil {
					return nil, err
				}
				f.Assignee = &id
			}
			list, err := s.projects.List(ctx, u, f)
			if err != nil {
				return nil, err
			}
			return map[string]any{"projects": s.briefs(list.Projects), "total": list.Total, "page": list.Page, "counts_by_status": list.Counts}, nil
		},
	},
	{
		name: "get_project", title: "Get a project",
		description: "One project in full: details and scope, who is on it, what this person may do next (the allowed status moves), recent status history and comments, files, and its client updates.",
		input:       obj([]string{"project"}, map[string]any{"project": projectRef}),
		run: func(ctx context.Context, s *Server, u db.User, args json.RawMessage) (any, error) {
			var a struct {
				Project string `json:"project"`
			}
			if err := decode(args, &a); err != nil {
				return nil, err
			}
			id, err := s.projectID(ctx, a.Project)
			if err != nil {
				return nil, err
			}
			d, err := s.projects.Get(ctx, u, id)
			if err != nil {
				return nil, err
			}
			moves := make([]map[string]any, len(d.Actions))
			for i, r := range d.Actions {
				moves[i] = map[string]any{"to": r.To, "label": r.Label, "needs_comment": r.NeedsComment}
			}
			history := []map[string]any{}
			for _, h := range lastN(d.History, 10) {
				history = append(history, map[string]any{"from": deref(h.FromStatus), "to": h.ToStatus, "by": h.ChangedByName, "at": h.CreatedAt, "comment": deref(h.Comment)})
			}
			comments := []map[string]any{}
			for _, c := range lastN(d.Comments, 10) {
				comments = append(comments, map[string]any{"by": c.AuthorName, "at": c.CreatedAt, "text": c.Body})
			}
			files := []map[string]any{}
			for _, f := range d.Files {
				files = append(files, map[string]any{"name": f.OriginalName, "version": f.VersionNo, "size_bytes": f.SizeBytes, "uploaded_by": f.UploadedByName})
			}
			ups, err := s.projects.ListUpdates(ctx, u, projects.UpdateFilter{ProjectID: &id, Page: 1, PageSize: 10})
			if err != nil {
				return nil, err
			}
			updates := make([]updateBrief, len(ups.Updates))
			for i, x := range ups.Updates {
				updates[i] = updateOf(x.UpdateDetail)
			}
			p := d.Project
			return map[string]any{
				"project": s.brief(p), "scope": deref(p.Scope), "special_notes": deref(p.SpecialNotes),
				"profile": deref(p.ProfileName), "agreed_price": deref(p.AgreedPrice), "currency": p.Currency,
				"marketplace_fee_percent": p.PlatformFeePercent, "marketplace_fee": p.PlatformFee, "we_receive": p.NetAmount,
				"store_access": p.StoreAccess, "store_access_link": deref(p.StoreAccessLink),
				"revisions": p.RevisionCount, "allowed_moves": moves, "can_assign": d.Permissions.CanAssign,
				"history": history, "comments": comments, "files": files, "client_updates": updates,
			}, nil
		},
	},
	{
		name: "project_board", title: "Project board",
		description: "A project's task board: cards by column (todo, in_progress, review, done), with who they're assigned to and when they're due. Client requests are cards too.",
		input:       obj([]string{"project"}, map[string]any{"project": projectRef}),
		run: func(ctx context.Context, s *Server, u db.User, args json.RawMessage) (any, error) {
			var a struct {
				Project string `json:"project"`
			}
			if err := decode(args, &a); err != nil {
				return nil, err
			}
			id, err := s.projectID(ctx, a.Project)
			if err != nil {
				return nil, err
			}
			b, err := s.projects.Board(ctx, u, id)
			if err != nil {
				return nil, err
			}
			cards := make([]cardBrief, len(b.Cards))
			for i, c := range b.Cards {
				cards[i] = cardOf(c.CardDetail)
			}
			return map[string]any{"columns": b.Columns, "cards": cards, "can_add_cards": b.CanCreate, "can_move_cards": b.CanMove}, nil
		},
	},
	{
		name: "team_workload", title: "Team workload",
		description: "Who is carrying what across the teams this person runs (the CEO: everyone): open, overdue and blocked projects and cards per person, who is on leave today, and each team's numbers.",
		input:       obj(nil, map[string]any{}),
		who:         can(policy.ViewWorkload),
		run: func(ctx context.Context, s *Server, u db.User, _ json.RawMessage) (any, error) {
			return s.reports.Workload(ctx, u)
		},
	},
	{
		name: "company_overview", title: "Company overview",
		description: "The CEO's dashboard: revenue and projects delivered this month, live and unassigned projects, people in today, every team's numbers, and the overdue and unassigned lists.",
		input:       obj(nil, map[string]any{}),
		who:         can(policy.ViewDashboard),
		run: func(ctx context.Context, s *Server, _ db.User, _ json.RawMessage) (any, error) {
			return s.reports.Company(ctx)
		},
	},
	{
		name: "recent_activity", title: "Recent activity",
		description: "What changed recently on the projects this person can see, newest first; optionally one project only.",
		input: obj(nil, map[string]any{
			"project": projectRef,
			"limit":   map[string]any{"type": "integer", "minimum": 1, "maximum": 100, "description": "How many entries (default 30)."},
		}),
		who: can(policy.ViewActivity),
		run: func(ctx context.Context, s *Server, u db.User, args json.RawMessage) (any, error) {
			var a struct {
				Project string `json:"project"`
				Limit   int    `json:"limit"`
			}
			if err := decode(args, &a); err != nil {
				return nil, err
			}
			f := reports.ActivityFilter{Limit: 30}
			if a.Limit > 0 {
				f.Limit = min(a.Limit, 100)
			}
			if a.Project != "" {
				id, err := s.projectID(ctx, a.Project)
				if err != nil {
					return nil, err
				}
				if _, err := s.projects.Get(ctx, u, id); err != nil {
					return nil, err
				}
				f.ProjectID = &id
			}
			rows, err := s.reports.Activity(ctx, u, f)
			if err != nil {
				return nil, err
			}
			out := make([]map[string]any, len(rows))
			for i, r := range rows {
				out[i] = map[string]any{
					"at": r.CreatedAt, "who": deref(r.ActorName), "action": r.Action,
					"project_number": r.ProjectNumber, "project": r.ProjectName, "details": json.RawMessage(r.Metadata),
				}
			}
			return map[string]any{"activity": out}, nil
		},
	},
	{
		name: "search", title: "Search",
		description: "Search everything this person can reach: projects (name, client, client username, order ID, number), board cards, files and people.",
		input:       obj([]string{"q"}, map[string]any{"q": str("What to look for.")}),
		run: func(ctx context.Context, s *Server, u db.User, args json.RawMessage) (any, error) {
			var a struct {
				Q string `json:"q"`
			}
			if err := decode(args, &a); err != nil {
				return nil, err
			}
			return s.projects.Search(ctx, u, a.Q)
		},
	},

	// ── writes ─────────────────────────────────────────────
	{
		name: "move_project", title: "Move a project",
		description: "Change a project's status under the portal's workflow rules (new → in_progress → ready_for_review → ready_to_deliver → delivered → completed): e.g. mark blocked (needs a comment saying why), send for delivery (ready_for_review), approve for delivery (ready_to_deliver, Team Leader), mark delivered (Sales), completed, or a client revision back to in_progress (with a comment). get_project lists the moves this person may make.",
		input: obj([]string{"project", "to"}, map[string]any{
			"project": projectRef,
			"to":      enum("The new status.", statusList...),
			"comment": str("Why: required for blocked, sending back and revisions."),
		}),
		write: true,
		run: func(ctx context.Context, s *Server, u db.User, args json.RawMessage) (any, error) {
			var a struct {
				Project string `json:"project"`
				To      string `json:"to"`
				Comment string `json:"comment"`
			}
			if err := decode(args, &a); err != nil {
				return nil, err
			}
			id, err := s.projectID(ctx, a.Project)
			if err != nil {
				return nil, err
			}
			if err := s.projects.Transition(ctx, u, id, workflow.Status(a.To), a.Comment); err != nil {
				return nil, err
			}
			return s.after(ctx, u, id, "Moved to "+workflow.Status(a.To).Label()+".")
		},
	},
	{
		name: "set_store_access", title: "Set store access",
		description: "Record where getting access to the client's store stands (not_requested, requested, given), and optionally the link the client gave (https://…). Never put passwords here.",
		input: obj([]string{"project", "status"}, map[string]any{
			"project": projectRef,
			"status":  enum("Where access stands.", projects.StoreAccessStates...),
			"link":    str("The client's access link, e.g. the store admin or collaborator URL. Empty clears it; leave out to keep the current one."),
		}),
		write: true,
		run: func(ctx context.Context, s *Server, u db.User, args json.RawMessage) (any, error) {
			var a struct {
				Project string  `json:"project"`
				Status  string  `json:"status"`
				Link    *string `json:"link"`
			}
			if err := decode(args, &a); err != nil {
				return nil, err
			}
			id, err := s.projectID(ctx, a.Project)
			if err != nil {
				return nil, err
			}
			if err := s.projects.SetStoreAccess(ctx, u, id, projects.StoreAccessInput{Status: a.Status, Link: a.Link}); err != nil {
				return nil, err
			}
			return s.after(ctx, u, id, "Store access set to "+a.Status+".")
		},
	},
	{
		name: "assign_project", title: "Assign a project",
		description: "Team Leader: give a new project to people on its team (or take it yourself with \"me\"), with a priority and an internal due date. Also changes who is on a project already assigned.",
		input: obj([]string{"project", "assignees"}, map[string]any{
			"project":   projectRef,
			"assignees": map[string]any{"type": "array", "minItems": 1, "items": map[string]any{"type": "string"}, "description": `People's ids, or "me".`},
			"priority":  enum("Priority (default: keep the project's).", "low", "medium", "high", "urgent"),
			"due_date":  str("Internal due date, YYYY-MM-DD."),
		}),
		write: true,
		who:   role(policy.TL),
		run: func(ctx context.Context, s *Server, u db.User, args json.RawMessage) (any, error) {
			var a struct {
				Project   string   `json:"project"`
				Assignees []string `json:"assignees"`
				Priority  string   `json:"priority"`
				DueDate   string   `json:"due_date"`
			}
			if err := decode(args, &a); err != nil {
				return nil, err
			}
			id, err := s.projectID(ctx, a.Project)
			if err != nil {
				return nil, err
			}
			in := projects.AssignInput{Priority: a.Priority}
			for _, raw := range a.Assignees {
				if raw == "me" {
					in.AssigneeIDs = append(in.AssigneeIDs, u.ID)
					continue
				}
				pid, err := uuidArg("assignees", raw)
				if err != nil {
					return nil, err
				}
				in.AssigneeIDs = append(in.AssigneeIDs, pid)
			}
			if in.Priority == "" {
				d, err := s.projects.Get(ctx, u, id)
				if err != nil {
					return nil, err
				}
				in.Priority = d.Project.Priority
			}
			if a.DueDate != "" {
				in.DueDate = &a.DueDate
			}
			if err := s.projects.Assign(ctx, u, id, in); err != nil {
				return nil, err
			}
			return s.after(ctx, u, id, "Assigned.")
		},
	},
	{
		name: "draft_client_update", title: "Draft a client update",
		description: "Write a client update on a project you are working on. It is saved as a DRAFT only: the person checks it and submits it in the portal; it never goes to Sales or the client from here. Destinations: order_page (default), inbox, delivery (delivery needs files attached in the portal).",
		input: obj([]string{"project", "message"}, map[string]any{
			"project":     projectRef,
			"message":     str("The update, written for the client."),
			"destination": enum("Where it will be posted.", "order_page", "inbox", "delivery"),
		}),
		write: true,
		run: func(ctx context.Context, s *Server, u db.User, args json.RawMessage) (any, error) {
			var a struct {
				Project     string `json:"project"`
				Message     string `json:"message"`
				Destination string `json:"destination"`
			}
			if err := decode(args, &a); err != nil {
				return nil, err
			}
			id, err := s.projectID(ctx, a.Project)
			if err != nil {
				return nil, err
			}
			if a.Destination == "" {
				a.Destination = "order_page"
			}
			// CreateUpdate only ever writes a draft; submitting is left to the person.
			uid, err := s.projects.CreateUpdate(ctx, u, id, projects.UpdateInput{Message: &a.Message, Destination: &a.Destination})
			if err != nil {
				return nil, err
			}
			return map[string]any{
				"update_id": uid, "status": "draft",
				"note": "Saved as a draft. Open it in the portal to check, attach files if needed, and submit.",
				"url":  fmt.Sprintf("%s/projects/%s", s.portal, id),
			}, nil
		},
	},
	{
		name: "add_card", title: "Add a board card",
		description: "Add a card to a project's board. For Sales this logs a client request; for the Team Leader it is a task (optionally assigned to people on the project).",
		input: obj([]string{"project", "title"}, map[string]any{
			"project":     projectRef,
			"title":       str("Short title."),
			"description": str("Details."),
			"due_date":    str("YYYY-MM-DD."),
			"assignees":   map[string]any{"type": "array", "items": map[string]any{"type": "string"}, "description": "Team Leader only: ids of people on the project."},
		}),
		write: true,
		run: func(ctx context.Context, s *Server, u db.User, args json.RawMessage) (any, error) {
			var a struct {
				Project     string   `json:"project"`
				Title       string   `json:"title"`
				Description string   `json:"description"`
				DueDate     string   `json:"due_date"`
				Assignees   []string `json:"assignees"`
			}
			if err := decode(args, &a); err != nil {
				return nil, err
			}
			id, err := s.projectID(ctx, a.Project)
			if err != nil {
				return nil, err
			}
			in := projects.CardInput{Title: &a.Title, Description: &a.Description}
			if a.DueDate != "" {
				in.DueDate = &a.DueDate
			}
			for _, raw := range a.Assignees {
				pid, err := uuidArg("assignees", raw)
				if err != nil {
					return nil, err
				}
				in.AssigneeIDs = append(in.AssigneeIDs, pid)
			}
			c, err := s.projects.CreateCard(ctx, u, id, in)
			if err != nil {
				return nil, err
			}
			return map[string]any{"card": cardOf(c.CardDetail)}, nil
		},
	},
	{
		name: "move_card", title: "Move a board card",
		description: "Move a card to another column (todo, in_progress, review, done), at the top unless an index is given.",
		input: obj([]string{"card", "column"}, map[string]any{
			"card":   str("The card's id (from project_board or my_work)."),
			"column": enum("Target column.", projects.Columns...),
			"index":  map[string]any{"type": "integer", "minimum": 0, "description": "Position in the column, 0 = top."},
		}),
		write: true,
		run: func(ctx context.Context, s *Server, u db.User, args json.RawMessage) (any, error) {
			var a struct {
				Card   string `json:"card"`
				Column string `json:"column"`
				Index  int    `json:"index"`
			}
			if err := decode(args, &a); err != nil {
				return nil, err
			}
			id, err := uuidArg("card", a.Card)
			if err != nil {
				return nil, err
			}
			if err := s.projects.MoveCard(ctx, u, id, projects.MoveInput{Column: a.Column, Index: a.Index}); err != nil {
				return nil, err
			}
			v, err := s.projects.GetCard(ctx, u, id)
			if err != nil {
				return nil, err
			}
			return map[string]any{"card": cardOf(v.Card.CardDetail)}, nil
		},
	},
	{
		name: "comment", title: "Comment",
		description: "Add a comment to a project, or to a board card when card is given instead of project.",
		input: obj([]string{"text"}, map[string]any{
			"project": projectRef,
			"card":    str("A card's id, to comment on the card instead."),
			"text":    str("The comment."),
		}),
		write: true,
		run: func(ctx context.Context, s *Server, u db.User, args json.RawMessage) (any, error) {
			var a struct {
				Project string `json:"project"`
				Card    string `json:"card"`
				Text    string `json:"text"`
			}
			if err := decode(args, &a); err != nil {
				return nil, err
			}
			switch {
			case a.Card != "" && a.Project == "":
				id, err := uuidArg("card", a.Card)
				if err != nil {
					return nil, err
				}
				c, err := s.projects.CommentCard(ctx, u, id, a.Text)
				if err != nil {
					return nil, err
				}
				return map[string]any{"comment_id": c.ID, "on": "card"}, nil
			case a.Project != "" && a.Card == "":
				id, err := s.projectID(ctx, a.Project)
				if err != nil {
					return nil, err
				}
				c, err := s.projects.Comment(ctx, u, id, a.Text)
				if err != nil {
					return nil, err
				}
				return map[string]any{"comment_id": c.ID, "on": "project"}, nil
			}
			return nil, badArgs("give either project or card")
		},
	},
	{
		name: "start_file_upload", title: "Start a file upload",
		description: "Step 1 of adding a file to a project: returns a short-lived URL. PUT the file's bytes to it with the returned headers (e.g. curl -X PUT -H 'Content-Type: …' --upload-file FILE 'URL'), then call finish_file_upload. Up to the portal's size limit (1 GB).",
		input: obj([]string{"project", "filename", "size"}, map[string]any{
			"project":      projectRef,
			"filename":     str("The file's name."),
			"size":         map[string]any{"type": "integer", "minimum": 1, "description": "Exact size in bytes."},
			"content_type": str("MIME type, e.g. application/pdf."),
		}),
		write: true,
		run: func(ctx context.Context, s *Server, u db.User, args json.RawMessage) (any, error) {
			var a struct {
				Project     string `json:"project"`
				Filename    string `json:"filename"`
				Size        int64  `json:"size"`
				ContentType string `json:"content_type"`
			}
			if err := decode(args, &a); err != nil {
				return nil, err
			}
			id, err := s.projectID(ctx, a.Project)
			if err != nil {
				return nil, err
			}
			t, err := s.projects.InitUpload(ctx, u, id, projects.UploadInput{Filename: a.Filename, Size: a.Size, ContentType: a.ContentType})
			if err != nil {
				return nil, err
			}
			return map[string]any{
				"upload_id": t.UploadID, "method": t.Method, "url": t.URL, "headers": t.Headers, "expires_at": t.ExpiresAt,
				"next": "PUT the file to url with these headers, then call finish_file_upload with this upload_id.",
			}, nil
		},
	},
	{
		name: "finish_file_upload", title: "Finish a file upload",
		description: "Step 2: after the file has been PUT to the upload URL, check it and add it to the project.",
		input: obj([]string{"project", "upload_id"}, map[string]any{
			"project":   projectRef,
			"upload_id": str("From start_file_upload."),
		}),
		write: true,
		run: func(ctx context.Context, s *Server, u db.User, args json.RawMessage) (any, error) {
			var a struct {
				Project  string `json:"project"`
				UploadID string `json:"upload_id"`
			}
			if err := decode(args, &a); err != nil {
				return nil, err
			}
			id, err := s.projectID(ctx, a.Project)
			if err != nil {
				return nil, err
			}
			if err := s.projects.CompleteUpload(ctx, u, id, a.UploadID); err != nil {
				return nil, err
			}
			return s.after(ctx, u, id, "File added.")
		},
	},
}

// after answers a project change with where the project now stands.
func (s *Server) after(ctx context.Context, u db.User, id uuid.UUID, done string) (any, error) {
	d, err := s.projects.Get(ctx, u, id)
	if err != nil {
		return nil, err
	}
	return map[string]any{"done": done, "project": s.brief(d.Project)}, nil
}

func lastN[T any](s []T, n int) []T {
	if len(s) > n {
		return s[len(s)-n:]
	}
	return s
}
