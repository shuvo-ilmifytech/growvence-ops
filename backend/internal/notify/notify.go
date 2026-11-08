// Package notify creates in-app notifications inside the caller's transaction.
// A Postgres trigger NOTIFYs on commit; the Hub streams them to browsers.
package notify

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/google/uuid"

	"github.com/solvinx/opsflow/internal/db"
	"github.com/solvinx/opsflow/internal/policy"
)

// Notification types (stable strings: the UI keys icons off them).
const (
	ProjectSubmitted  = "project.submitted"
	ProjectAssigned   = "project.assigned"
	ProjectUnassigned = "project.unassigned" // removed from a project by reassignment
	ProjectMoved      = "project.moved"      // handed to another team
	ProjectNeedsInfo  = "project.needs_info"
	ProjectChanges    = "project.changes"   // batched Sales edits/files/comments
	ProjectImportant  = "project.important" // scope/budget/deadline change, needs acknowledgement
	ProjectBlocked    = "project.blocked"
	ProjectReview     = "project.ready_for_review"
	ProjectToDeliver  = "project.ready_to_deliver"
	ProjectDelivered  = "project.delivered"
	ProjectSentBack   = "project.sent_back"
	ProjectRevision   = "project.revision"
	ProjectCompleted  = "project.completed"
	ProjectCancelled  = "project.cancelled"
	ProjectOverdue    = "project.overdue"
	ProjectWaiting    = "project.unassigned_24h"
	UpdateSubmitted   = "update.submitted"
	UpdateApproved    = "update.approved"
	UpdateChanges     = "update.changes_requested"
	UpdateSent        = "update.sent"
	UpdateComments    = "update.comments" // batched
	UpdateUnsent      = "update.unsent_24h"
	CardCreated       = "card.created" // a client request (or a task) was added to the board
	CardAssigned      = "card.assigned"
	CardReview        = "card.review"   // moved to Review
	CardDone          = "card.done"     // a Sales card reached Done
	CardComments      = "card.comments" // batched
	LeaveRequested    = "hr.leave_requested"
	LeaveApproved     = "hr.leave_approved"
	LeaveRejected     = "hr.leave_rejected"
	LeaveCancelled    = "hr.leave_cancelled"
	CorrectionAsked   = "hr.correction_requested"
	CorrectionDone    = "hr.correction_reviewed"
	InvoiceRaised     = "invoice.raised"   // an expense invoice needs this person's decision
	InvoiceApproved   = "invoice.approved" // a step passed, or the CEO approved it for good
	InvoiceRejected   = "invoice.rejected"
	InvoiceWithdrawn  = "invoice.withdrawn" // the person who raised it took it back
	ProjectDueSoon    = "project.due_soon"  // due within 3 days
	ExpenseSubmitted  = "finance.expense_submitted"
	ExpenseApproved   = "finance.expense_approved"
	ExpenseRejected   = "finance.expense_rejected"
	RecurringRan      = "recurring.ran" // a recurring task created its card
	// Milestone payments (Phase 2): invoice generated, payment due, overdue, paid.
	MilestoneInvoice  = "milestone.invoice"
	MilestoneDue      = "milestone.due"
	MilestoneOverdue  = "milestone.overdue"
	MilestonePaid     = "milestone.paid"
	// Retainer billing (Phase 5): a retainer's monthly invoice was generated.
	RetainerInvoice = "retainer.invoice"
	// Client collaboration (Phase 4): a comment, approval request/decision,
	// message or shared file crossed the team/client line.
	CollabComment       = "collab.comment"
	CollabApprovalAsked = "collab.approval_requested"
	CollabApproved      = "collab.approved"
	CollabChanges       = "collab.changes_requested"
	CollabMessage       = "collab.message"
	CollabFile          = "collab.file"
)

type Notification struct {
	Type string
	// What it is about: a project, or a client request (a client with no project).
	ProjectID       *uuid.UUID
	ClientRequestID *uuid.UUID
	UpdateID        *uuid.UUID
	Title           string
	Body            string
	Link            string
	Important       bool
}

// Recipients is an ordered set of user ids.
type Recipients []uuid.UUID

func (r Recipients) Add(ids ...uuid.UUID) Recipients {
	for _, id := range ids {
		if id == uuid.Nil {
			continue
		}
		dup := false
		for _, x := range r {
			dup = dup || x == id
		}
		if !dup {
			r = append(r, id)
		}
	}
	return r
}

func (r Recipients) AddPtr(id *uuid.UUID) Recipients {
	if id == nil {
		return r
	}
	return r.Add(*id)
}

func (r Recipients) Without(id uuid.UUID) Recipients {
	out := Recipients{}
	for _, x := range r {
		if x != id {
			out = append(out, x)
		}
	}
	return out
}

// Role returns every active user with the role.
func Role(ctx context.Context, q *db.Queries, role policy.Role) (Recipients, error) {
	ids, err := q.ActiveUserIDsByRole(ctx, string(role))
	return Recipients(ids), err
}

// Send creates one notification per recipient, never for the actor themselves.
func Send(ctx context.Context, q *db.Queries, to Recipients, actor uuid.UUID, n Notification) error {
	data, _ := json.Marshal(map[string]any{"link": n.Link})
	for _, r := range to.Without(actor) {
		if _, err := q.InsertNotification(ctx, db.InsertNotificationParams{
			RecipientID: r, Type: n.Type, ProjectID: n.ProjectID, ClientRequestID: n.ClientRequestID,
			UpdateID: n.UpdateID, Title: n.Title, Body: n.Body, Data: data, IsImportant: n.Important,
		}); err != nil {
			return err
		}
	}
	return nil
}

type batchData struct {
	Link  string   `json:"link"`
	Lines []string `json:"lines"`
	Count int      `json:"count"`
}

const maxBodyLines = 6

// Batch appends a line to each recipient's open notification about this project or client
// request (same type, unread, started within windowMinutes) or starts a new one.
// "5 files in 10 minutes" becomes one notification listing all five.
func Batch(ctx context.Context, q *db.Queries, to Recipients, actor uuid.UUID, projectID, requestID *uuid.UUID,
	windowMinutes int, typ, title, link string, lines ...string) error {
	if len(lines) == 0 {
		return nil
	}
	for _, r := range to.Without(actor) {
		open, err := q.FindOpenBatch(ctx, db.FindOpenBatchParams{
			RecipientID: r, ProjectID: projectID, ClientRequestID: requestID, Type: typ, WindowMinutes: int32(windowMinutes),
		})
		var d batchData
		if err == nil {
			_ = json.Unmarshal(open.Data, &d)
		}
		d.Link = link
		d.Lines = append(d.Lines, lines...)
		d.Count = len(d.Lines)
		raw, _ := json.Marshal(d)
		body := summarize(d.Lines)
		if err == nil {
			if err := q.UpdateNotificationBatch(ctx, db.UpdateNotificationBatchParams{ID: open.ID, Title: title, Body: body, Data: raw}); err != nil {
				return err
			}
			continue
		}
		if _, err := q.InsertNotification(ctx, db.InsertNotificationParams{
			RecipientID: r, Type: typ, ProjectID: projectID, ClientRequestID: requestID,
			Title: title, Body: body, Data: raw,
		}); err != nil {
			return err
		}
	}
	return nil
}

func summarize(lines []string) string {
	if len(lines) <= maxBodyLines {
		return strings.Join(lines, "\n")
	}
	return strings.Join(lines[:maxBodyLines-1], "\n") + fmt.Sprintf("\n…and %d more changes", len(lines)-maxBodyLines+1)
}
