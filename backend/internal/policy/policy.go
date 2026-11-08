// Package policy is the single source of truth for who may do what.
//
// Two layers:
//   - Role capabilities (this file): may a role perform an action at all?
//   - Resource checks (added per feature): ownership, assignment, status.
//
// Handlers must call policy; hiding UI controls is never sufficient.
package policy

import "slices"

type Role string

const (
	Sales  Role = "sales"
	TL     Role = "tl"
	Member Role = "member"
	CEO    Role = "ceo" // one per organisation: sees everything, runs people and settings
	HR     Role = "hr"  // people, attendance and leave; no project access
	// OfficeAdmin buys what the office needs and raises the invoice for it. No project
	// access and no ledger: only their own invoices, plus self-service HR.
	OfficeAdmin Role = "office_admin"
	// Client is an external client: sees only granted projects via the client portal.
	Client Role = "client"
	// Investor is a capital partner: sees only their own finance profile
	// (investments, distributions, withdrawals, balance) via /investor.
	Investor Role = "investor"
)

var Roles = []Role{Sales, TL, Member, CEO, HR, OfficeAdmin, Client, Investor}

func (r Role) Valid() bool {
	switch r {
	case Sales, TL, Member, CEO, HR, OfficeAdmin, Client, Investor:
		return true
	}
	return false
}

type Action string

const (
	// Projects
	ProjectCreate       Action = "project.create"
	ProjectEditDetails  Action = "project.edit_details" // Sales: own, until completed/cancelled
	ProjectUploadFile   Action = "project.upload_file"
	ProjectSubmit       Action = "project.submit"
	ProjectCancel       Action = "project.cancel"
	ProjectViewDrafts   Action = "project.view_drafts" // other users' drafts
	ProjectRequestInfo  Action = "project.request_info"
	ProjectAssign       Action = "project.assign"      // also reassign, priority, due date
	ProjectWorkStatus   Action = "project.work_status" // in_progress / blocked / ready_for_review
	ProjectComplete     Action = "project.complete"
	ProjectReturnToWork Action = "project.return_to_work" // ready_for_review -> in_progress
	ProjectRevision     Action = "project.request_revision"
	ProjectComment      Action = "project.comment"
	ProjectAcknowledge  Action = "project.acknowledge"
	ProjectViewAcks     Action = "project.view_acks"

	// Updates
	UpdateWrite          Action = "update.write" // create, edit, submit
	UpdateApprove        Action = "update.approve"
	UpdateRequestChanges Action = "update.request_changes"
	UpdateSetDestination Action = "update.set_destination"
	UpdateMarkSent       Action = "update.mark_sent"
	UpdateViewUnapproved Action = "update.view_unapproved" // draft / pending / changes_requested
	UpdateCommentOps     Action = "update.comment_ops"
	UpdateCommentSales   Action = "update.comment_sales"
	UpdateViewOpsThread  Action = "update.view_ops_thread"

	// Oversight
	ViewAll       Action = "oversight.view_all" // every project, regardless of owner/assignee
	ViewActivity  Action = "oversight.view_activity"
	ViewDashboard Action = "oversight.view_dashboard"
	ViewWorkload  Action = "oversight.view_workload" // who is carrying what, across a team
	ExportCSV     Action = "oversight.export_csv"
	// Granted per person, not per role: see CanViewAnalytics.
	ViewAnalytics Action = "oversight.view_analytics"

	// People: user accounts, roles, HR records, leave and attendance for everyone, HR settings.
	PeopleManage Action = "people.manage"

	// Expense invoices: every company expense is raised as one, approved by HR, then the CEO.
	InvoiceRaise  Action = "invoice.raise"  // write one, and see your own
	InvoiceReview Action = "invoice.review" // decide at your step, and see everyone's
)

var matrix = map[Role]map[Action]bool{
	Sales: set(
		ProjectCreate, ProjectEditDetails, ProjectUploadFile, ProjectSubmit, ProjectCancel,
		ProjectRevision, ProjectComment,
		UpdateMarkSent, UpdateCommentSales,
	),
	TL: set(
		ProjectUploadFile, // on projects they took on themselves
		ProjectCancel, ProjectRequestInfo, ProjectAssign, ProjectComplete, ProjectReturnToWork,
		ProjectRevision, ProjectComment, ProjectViewAcks,
		UpdateApprove, UpdateRequestChanges, UpdateSetDestination, UpdateViewUnapproved,
		UpdateCommentOps, UpdateCommentSales, UpdateViewOpsThread,
		ViewAll, ViewActivity, ViewWorkload,
	),
	Member: set(
		ProjectWorkStatus, ProjectComment, ProjectAcknowledge, ProjectUploadFile,
		UpdateWrite, UpdateViewUnapproved, UpdateCommentOps, UpdateCommentSales, UpdateViewOpsThread,
	),
	// The CEO oversees projects without operating them (view actions only) and runs people.
	CEO: set(
		ProjectViewDrafts, ProjectViewAcks, UpdateViewUnapproved, UpdateViewOpsThread,
		ViewAll, ViewActivity, ViewDashboard, ViewWorkload, ExportCSV,
		PeopleManage, InvoiceRaise, InvoiceReview,
	),
	HR: set(PeopleManage, InvoiceRaise, InvoiceReview),
	// The Office Admin only raises invoices for what the office needs; HR and then the CEO
	// decide. Everything else about them is self-service HR, which needs no capability.
	OfficeAdmin: set(InvoiceRaise),
}

// Can reports whether the role is ever allowed to perform the action.
func Can(r Role, a Action) bool { return matrix[r][a] }

// CanViewAnalytics reports whether someone may open the Analytics page: the CEO always,
// anyone else only when the CEO has switched it on for them (users.can_view_analytics).
func CanViewAnalytics(r Role, granted bool) bool { return r == CEO || granted }

// IsReadOnly reports whether the role may never mutate project data.
func IsReadOnly(r Role) bool { return r == CEO }

// ManagesPeople reports whether the role runs accounts, roles and HR for everyone.
func ManagesPeople(r Role) bool { return Can(r, PeopleManage) }

// ActionsFor lists a role's capabilities (sorted), so the UI can show or hide controls.
func ActionsFor(r Role) []Action {
	out := make([]Action, 0, len(matrix[r]))
	for a := range matrix[r] {
		out = append(out, a)
	}
	slices.Sort(out)
	return out
}

func set(actions ...Action) map[Action]bool {
	m := make(map[Action]bool, len(actions))
	for _, a := range actions {
		m[a] = true
	}
	return m
}
