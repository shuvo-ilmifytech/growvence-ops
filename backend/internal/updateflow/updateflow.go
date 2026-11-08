// Package updateflow holds the rules for client updates: who may see, write,
// approve and send them, and how their status moves. It is pure so every rule
// can be tested exhaustively.
package updateflow

import (
	"fmt"
	"slices"
	"strings"

	"github.com/solvinx/opsflow/internal/apperr"
	"github.com/solvinx/opsflow/internal/policy"
	"github.com/solvinx/opsflow/internal/validate"
)

type Status string

const (
	Draft            Status = "draft"
	PendingApproval  Status = "pending_approval"
	ChangesRequested Status = "changes_requested"
	Approved         Status = "approved"
	Sent             Status = "sent"
)

var All = []Status{Draft, PendingApproval, ChangesRequested, Approved, Sent}

var labels = map[Status]string{
	Draft: "Draft Update", PendingApproval: "Pending Approval", ChangesRequested: "Changes Requested",
	Approved: "Approved", Sent: "Sent",
}

func (s Status) Label() string { return labels[s] }

type Destination string

const (
	Inbox     Destination = "inbox"
	OrderPage Destination = "order_page"
	Delivery  Destination = "delivery"
)

var Destinations = []string{string(Inbox), string(OrderPage), string(Delivery)}

// Actor is the acting user's relationship to the update's project.
type Actor struct {
	Role         policy.Role
	IsSalesOwner bool // acts for the project's client: any Sales user (Sales cover for each other)
	IsAssignee   bool // currently assigned to the project (a member, or a Team Leader who took it on)
	// OnTeam: on the team a client request is with, without holding its cards. A request is
	// the whole team's work, so they read what has been written up — never a draft — and
	// write nothing.
	OnTeam bool
}

func (a Actor) salesOwner() bool { return a.Role == policy.Sales && a.IsSalesOwner }
func (a Actor) tl() bool         { return a.Role == policy.TL }
func (a Actor) assignee() bool {
	return a.IsAssignee && (a.Role == policy.Member || a.Role == policy.TL)
}

// Works reports whether the actor writes the project's updates.
func (a Actor) Works() bool { return a.assignee() }

// Projects accept new updates only while work is under way.
var writableProject = []string{"in_progress", "blocked", "ready_for_review", "ready_to_deliver", "delivered"}

// Context is everything a rule may depend on besides the actor.
type Context struct {
	Status                Status
	Destination           Destination
	Attachments           int64
	ProjectStatus         string
	InboxRequiresApproval bool
}

// CanView: Sales never see drafts, pending or changes-requested updates.
func CanView(a Actor, s Status) bool {
	switch a.Role {
	case policy.Sales:
		return a.IsSalesOwner && (s == Approved || s == Sent)
	case policy.TL:
		// Reviewers skip drafts; a leader working the project sees them like any assignee.
		return s != Draft || a.IsAssignee
	case policy.Member:
		return a.IsAssignee || (a.OnTeam && s != Draft)
	case policy.CEO:
		return true
	}
	return false
}

func CanCreate(a Actor, projectStatus string) error {
	if !a.assignee() {
		return apperr.Forbidden()
	}
	if !slices.Contains(writableProject, projectStatus) {
		return apperr.Conflict("Updates can only be written while the project is being worked on")
	}
	return nil
}

// needsApproval: every destination needs TL approval, except Inbox when the admin turns it off.
func needsApproval(d Destination, inboxRequiresApproval bool) bool {
	return d != Inbox || inboxRequiresApproval
}

func requireAttachments(d Destination, n int64) error {
	if d == Delivery && n == 0 {
		return apperr.Validation(validate.Errors{"attachments": "A Delivery update needs at least one attachment"})
	}
	return nil
}

func ValidDestination(d string) bool { return slices.Contains(Destinations, d) }

// ValidateMessage checks the update text.
func ValidateMessage(msg string, required bool) error {
	v := validate.Errors{}
	if required && strings.TrimSpace(msg) == "" {
		v.Add("message", "is required")
	}
	v.MaxLen("message", msg, 50000)
	if !v.OK() {
		return apperr.Validation(v)
	}
	return nil
}

// Edit: the assignee changes the text, destination or attachments.
// Returns the status the update should be in afterwards.
func Edit(a Actor, c Context) (Status, error) {
	if !a.assignee() {
		return "", apperr.Forbidden()
	}
	switch c.Status {
	case Draft, ChangesRequested, PendingApproval:
		return c.Status, nil
	case Approved:
		// Approved text is locked: editing sends it back for approval (unless none is
		// needed, or the author is a Team Leader, who is the approver).
		if needsApproval(c.Destination, c.InboxRequiresApproval) && !a.tl() {
			return PendingApproval, nil
		}
		return Approved, nil
	}
	return "", apperr.Conflict("A sent update can't be changed")
}

// Submit: the assignee sends a draft (or a reworked update) for approval. A Team
// Leader working the project is the approver, so their own update is approved on submit.
func Submit(a Actor, c Context) (to Status, autoApproved bool, err error) {
	if !a.assignee() {
		return "", false, apperr.Forbidden()
	}
	if c.Status != Draft && c.Status != ChangesRequested {
		return "", false, apperr.InvalidTransition(fmt.Sprintf("An update that is %s can't be submitted", c.Status.Label()))
	}
	if err := requireAttachments(c.Destination, c.Attachments); err != nil {
		return "", false, err
	}
	if !needsApproval(c.Destination, c.InboxRequiresApproval) || a.tl() {
		return Approved, true, nil
	}
	return PendingApproval, false, nil
}

// Approve: the TL approves, optionally correcting the destination first.
func Approve(a Actor, c Context) error {
	if !a.tl() {
		return apperr.Forbidden()
	}
	if c.Status != PendingApproval {
		return apperr.InvalidTransition(fmt.Sprintf("Only updates pending approval can be approved (this one is %s)", c.Status.Label()))
	}
	return requireAttachments(c.Destination, c.Attachments)
}

// RequestChanges: the TL sends it back with a reason.
func RequestChanges(a Actor, c Context, comment string) error {
	if !a.tl() {
		return apperr.Forbidden()
	}
	if c.Status != PendingApproval {
		return apperr.InvalidTransition(fmt.Sprintf("Only updates pending approval can be sent back (this one is %s)", c.Status.Label()))
	}
	if strings.TrimSpace(comment) == "" {
		return apperr.Validation(validate.Errors{"comment": "Tell the member what to change"})
	}
	return nil
}

// SetDestination: the TL corrects "Update To" while reviewing or after approving.
func SetDestination(a Actor, c Context) error {
	if !a.tl() {
		return apperr.Forbidden()
	}
	if c.Status != PendingApproval && c.Status != Approved {
		return apperr.Conflict("The destination can only be corrected while the update is pending or approved")
	}
	return requireAttachments(c.Destination, c.Attachments)
}

// MarkSent: the project's Sales owner records that they posted it on the platform.
func MarkSent(a Actor, c Context) error {
	if !a.salesOwner() {
		return apperr.Forbidden()
	}
	if c.Status != Approved {
		return apperr.InvalidTransition("Only approved updates can be marked as sent")
	}
	if c.ProjectStatus == "cancelled" {
		return apperr.Conflict("This project was cancelled")
	}
	return nil
}

type Thread string

const (
	OpsThread   Thread = "ops"
	SalesThread Thread = "sales"
)

// Threads the actor can read on a visible update.
func ReadableThreads(a Actor) []string {
	switch a.Role {
	case policy.Sales:
		return []string{string(SalesThread)}
	case policy.TL, policy.Member, policy.CEO:
		return []string{string(OpsThread), string(SalesThread)}
	}
	return nil
}

// CanComment: the Ops thread is TL + assignee; the Sales thread adds the Sales owner. Management only reads.
func CanComment(a Actor, t Thread) bool {
	switch t {
	case OpsThread:
		return a.tl() || a.assignee()
	case SalesThread:
		return a.tl() || a.assignee() || a.salesOwner()
	}
	return false
}
