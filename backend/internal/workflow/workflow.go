// Package workflow is the project status state machine. It is pure (no I/O) so
// every transition rule can be tested exhaustively.
package workflow

import (
	"fmt"
	"strings"

	"github.com/solvinx/opsflow/internal/apperr"
	"github.com/solvinx/opsflow/internal/policy"
	"github.com/solvinx/opsflow/internal/validate"
)

type Status string

// The Fiverr-style flow (2026-10-30): an order goes straight to its team, the TL assigns it,
// the team sends it for delivery, the TL checks it, Sales delivers it on the marketplace and
// marks it Delivered, and it's Completed when the client accepts.
const (
	New            Status = "new"
	InProgress     Status = "in_progress"
	Blocked        Status = "blocked"
	ReadyForReview Status = "ready_for_review" // "In review": waiting for the TL's check
	ReadyToDeliver Status = "ready_to_deliver"
	Delivered      Status = "delivered"
	Completed      Status = "completed"
	Cancelled      Status = "cancelled"
)

// Statuses from the old flow. No project has them any more, but history rows and the
// activity log still do, so they keep their labels.
const (
	Draft     Status = "draft"
	Submitted Status = "submitted"
	NeedsInfo Status = "needs_info"
	Assigned  Status = "assigned"
)

var All = []Status{New, InProgress, Blocked, ReadyForReview, ReadyToDeliver, Delivered, Completed, Cancelled}

var labels = map[Status]string{
	New: "New", InProgress: "In Progress", Blocked: "Blocked", ReadyForReview: "In Review",
	ReadyToDeliver: "Ready to Deliver", Delivered: "Delivered", Completed: "Completed", Cancelled: "Cancelled",
}

var legacyLabels = map[Status]string{Draft: "Draft", Submitted: "Submitted", NeedsInfo: "Needs Info", Assigned: "Assigned"}

func (s Status) Valid() bool { _, ok := labels[s]; return ok }

func (s Status) Label() string {
	if l, ok := labels[s]; ok {
		return l
	}
	return legacyLabels[s]
}

// Terminal statuses end Sales editing; only a revision reopens Completed.
func (s Status) Terminal() bool { return s == Completed || s == Cancelled }

// Who is a set of parties allowed to perform a transition.
type Who uint8

const (
	SalesOwner     Who = 1 << iota // the Sales user who created the project
	TeamLeader                     // any TL
	Assignee                       // whoever is currently assigned: a member, or a Team Leader who took it on
	AnySales                       // any Sales user: they cover for each other on client matters
	MemberAssignee                 // an assigned member: their work goes to the TL's check
	LeadAssignee                   // a Team Leader who took the project on: no check on their own work
)

// Actor is the acting user's relationship to the project.
type Actor struct {
	Role       policy.Role
	IsOwner    bool
	IsAssignee bool
}

// Works reports whether the actor is assigned to do the work. Members and Team
// Leaders can be assigned; a leader who takes a project on works it like a member.
func (a Actor) Works() bool {
	return a.IsAssignee && (a.Role == policy.Member || a.Role == policy.TL)
}

func (a Actor) is(w Who) bool {
	return (w&SalesOwner != 0 && a.Role == policy.Sales && a.IsOwner) ||
		(w&AnySales != 0 && a.Role == policy.Sales) ||
		(w&TeamLeader != 0 && a.Role == policy.TL) ||
		(w&Assignee != 0 && a.Works()) ||
		(w&MemberAssignee != 0 && a.IsAssignee && a.Role == policy.Member) ||
		(w&LeadAssignee != 0 && a.IsAssignee && a.Role == policy.TL)
}

type Rule struct {
	From         Status `json:"from"`
	To           Status `json:"to"`
	By           Who    `json:"-"`
	Label        string `json:"label"`
	NeedsComment bool   `json:"needs_comment"`
	Revision     bool   `json:"revision"` // the client asked for changes after a delivery
}

// rules: each (From, To) pair appears at most once.
var rules = []Rule{
	// New projects move to In Progress when the TL assigns them (see CanAssign), not by a button.
	{From: InProgress, To: Blocked, By: Assignee, Label: "Mark blocked", NeedsComment: true},
	{From: Blocked, To: InProgress, By: Assignee, Label: "Unblock"},
	{From: InProgress, To: ReadyForReview, By: MemberAssignee, Label: "Send for delivery"},
	{From: InProgress, To: ReadyToDeliver, By: LeadAssignee, Label: "Ready to deliver"},

	{From: ReadyForReview, To: ReadyToDeliver, By: TeamLeader, Label: "Approve for delivery"},
	{From: ReadyForReview, To: InProgress, By: TeamLeader, Label: "Send back", NeedsComment: true},
	{From: ReadyToDeliver, To: Delivered, By: AnySales, Label: "Mark delivered"},
	{From: ReadyToDeliver, To: InProgress, By: AnySales | TeamLeader, Label: "Send back", NeedsComment: true},

	{From: Delivered, To: Completed, By: AnySales | TeamLeader, Label: "Mark completed"},
	{From: Delivered, To: InProgress, By: AnySales | TeamLeader, Label: "Client requested revision", NeedsComment: true, Revision: true},
	{From: Completed, To: InProgress, By: AnySales | TeamLeader, Label: "Client requested revision", NeedsComment: true, Revision: true},

	{From: New, To: Cancelled, By: SalesOwner | TeamLeader, Label: "Cancel project"},
	{From: InProgress, To: Cancelled, By: SalesOwner | TeamLeader, Label: "Cancel project"},
	{From: Blocked, To: Cancelled, By: SalesOwner | TeamLeader, Label: "Cancel project"},
	{From: ReadyForReview, To: Cancelled, By: SalesOwner | TeamLeader, Label: "Cancel project"},
	{From: ReadyToDeliver, To: Cancelled, By: SalesOwner | TeamLeader, Label: "Cancel project"},
	{From: Delivered, To: Cancelled, By: SalesOwner | TeamLeader, Label: "Cancel project"},
}

func find(from, to Status) (Rule, bool) {
	for _, r := range rules {
		if r.From == from && r.To == to {
			return r, true
		}
	}
	return Rule{}, false
}

// Check validates a status change. Errors map to HTTP:
// unknown status 422, no such move 409, wrong actor 403, missing comment 422.
func Check(from, to Status, actor Actor, comment string) (Rule, error) {
	if !to.Valid() {
		return Rule{}, apperr.Validation(validate.Errors{"to": "is not a valid status"})
	}
	r, ok := find(from, to)
	if !ok {
		return Rule{}, apperr.InvalidTransition(fmt.Sprintf("A project can't move from %s to %s", from.Label(), to.Label()))
	}
	if !actor.is(r.By) {
		return Rule{}, apperr.Forbidden()
	}
	if r.NeedsComment && strings.TrimSpace(comment) == "" {
		return Rule{}, apperr.Validation(validate.Errors{"comment": "is required for this change"})
	}
	return r, nil
}

// Available lists the transitions this actor may make from the current status (for the UI).
func Available(from Status, actor Actor) []Rule {
	var out []Rule
	for _, r := range rules {
		if r.From == from && actor.is(r.By) {
			out = append(out, r)
		}
	}
	return out
}

// Assignment (and reassignment) is TL-only, from these statuses. Assigning a New project
// starts it (In Progress); reassigning keeps the status.
var assignableFrom = map[Status]bool{New: true, InProgress: true, Blocked: true}

// AfterAssign is the status a project lands on when the TL assigns it.
func AfterAssign(from Status) Status {
	if from == New {
		return InProgress
	}
	return from
}

func CanAssign(from Status, actor Actor) bool {
	return assignableFrom[from] && actor.is(TeamLeader)
}

// CanMoveTeam: a TL hands the project to another team while it's being worked on (the
// same statuses as assignment). Once it's in review or later, the check stays with its team.
func CanMoveTeam(from Status, actor Actor) bool {
	return assignableFrom[from] && actor.is(TeamLeader)
}

// CanPlan: TL may change priority / due date on any submitted, non-terminal project.
func CanPlan(from Status, actor Actor) bool {
	return !from.Terminal() && actor.is(TeamLeader)
}

// CanEditDetails: the owning Sales user, until Completed or Cancelled.
func CanEditDetails(from Status, actor Actor) bool {
	return !from.Terminal() && actor.is(SalesOwner)
}

// CanManageFiles: the owning Sales user and whoever is assigned (members, or a Team Leader
// who took the project on) add files and new versions, until Completed or Cancelled.
func CanManageFiles(from Status, actor Actor) bool {
	return !from.Terminal() && actor.is(SalesOwner|Assignee)
}

// CanRemoveAnyFile: only the Sales owner removes files someone else uploaded; assignees
// remove their own.
func CanRemoveAnyFile(from Status, actor Actor) bool {
	return CanEditDetails(from, actor)
}

// SalesSetsPriority: Sales may suggest a priority until the project is assigned; then it's the TL's call.
func SalesSetsPriority(from Status) bool {
	return from == New
}
