package policy

import (
	"strings"
	"testing"
)

// expected is the role/capability matrix written out from the spec, independently
// of the implementation, so an accidental grant or revoke fails the test.
var expected = map[Action][]Role{
	ProjectCreate:       {Sales},
	ProjectEditDetails:  {Sales},
	ProjectUploadFile:   {Sales, TL, Member},
	ProjectSubmit:       {Sales},
	ProjectCancel:       {Sales, TL},
	ProjectViewDrafts:   {CEO},
	ProjectRequestInfo:  {TL},
	ProjectAssign:       {TL},
	ProjectWorkStatus:   {Member},
	ProjectComplete:     {TL},
	ProjectReturnToWork: {TL},
	ProjectRevision:     {Sales, TL},
	ProjectComment:      {Sales, TL, Member},
	ProjectAcknowledge:  {Member},
	ProjectViewAcks:     {TL, CEO},

	UpdateWrite:          {Member},
	UpdateApprove:        {TL},
	UpdateRequestChanges: {TL},
	UpdateSetDestination: {TL},
	UpdateMarkSent:       {Sales},
	UpdateViewUnapproved: {TL, Member, CEO},
	UpdateCommentOps:     {TL, Member},
	UpdateCommentSales:   {Sales, TL, Member},
	UpdateViewOpsThread:  {TL, Member, CEO},

	ViewAll:       {TL, CEO},
	ViewActivity:  {TL, CEO},
	ViewDashboard: {CEO},
	ViewWorkload:  {TL, CEO},
	ExportCSV:     {CEO},

	PeopleManage: {CEO, HR},

	InvoiceRaise:  {CEO, HR, OfficeAdmin},
	InvoiceReview: {CEO, HR},
}

func TestMatrixMatchesSpec(t *testing.T) {
	for action, allowed := range expected {
		for _, role := range Roles {
			want := contains(allowed, role)
			if got := Can(role, action); got != want {
				t.Errorf("Can(%s, %s) = %v, want %v", role, action, got, want)
			}
		}
	}
	// Every action granted anywhere must be covered by the expectations above.
	for role, actions := range matrix {
		for a := range actions {
			if _, ok := expected[a]; !ok {
				t.Errorf("action %s granted to %s has no expectation in the test", a, role)
			}
		}
	}
}

func TestCEOIsReadOnlyOnProjects(t *testing.T) {
	// people.* and invoice.* are the CEO's to act on (accounts, HR, approving expenses);
	// everything about projects and updates must be read-only.
	readPrefixes := []string{"project.view_", "update.view_", "oversight.", "people.", "invoice."}
	for _, a := range ActionsFor(CEO) {
		ok := false
		for _, p := range readPrefixes {
			ok = ok || strings.HasPrefix(string(a), p)
		}
		if !ok {
			t.Errorf("ceo has non-read project action %s", a)
		}
	}
	if !IsReadOnly(CEO) {
		t.Error("ceo must be read-only on projects")
	}
}

func TestSalesCannotSeeUnapprovedUpdates(t *testing.T) {
	if Can(Sales, UpdateViewUnapproved) || Can(Sales, UpdateViewOpsThread) {
		t.Error("sales must not see draft/pending updates or the ops thread")
	}
}

func TestUnknownRoleHasNothing(t *testing.T) {
	if Role("admin").Valid() || len(ActionsFor(Role("admin"))) != 0 || Can(Role(""), ProjectCreate) {
		t.Error("unknown roles must have no capabilities")
	}
}

func contains(rs []Role, r Role) bool {
	for _, x := range rs {
		if x == r {
			return true
		}
	}
	return false
}
