package updateflow

import (
	"errors"
	"net/http"
	"testing"

	"github.com/solvinx/opsflow/internal/apperr"
	"github.com/solvinx/opsflow/internal/policy"
)

var actors = map[string]Actor{
	"sales-owner": {Role: policy.Sales, IsSalesOwner: true},
	"sales-other": {Role: policy.Sales},
	"tl":          {Role: policy.TL},
	"assignee":    {Role: policy.Member, IsAssignee: true},
	"member":      {Role: policy.Member},
	"management":  {Role: policy.CEO, IsSalesOwner: true, IsAssignee: true},
}

func code(err error) int {
	var e *apperr.Error
	if errors.As(err, &e) {
		return e.Status
	}
	return 0
}

func ctx(s Status, d Destination, attachments int64) Context {
	return Context{Status: s, Destination: d, Attachments: attachments, ProjectStatus: "in_progress", InboxRequiresApproval: true}
}

func TestVisibility(t *testing.T) {
	want := map[string][]Status{
		"sales-owner": {Approved, Sent},
		"sales-other": {},
		"tl":          {PendingApproval, ChangesRequested, Approved, Sent},
		"assignee":    All,
		"member":      {},
		"management":  All,
	}
	for name, a := range actors {
		for _, s := range All {
			exp := false
			for _, w := range want[name] {
				exp = exp || w == s
			}
			if got := CanView(a, s); got != exp {
				t.Errorf("CanView(%s, %s) = %v, want %v", name, s, got, exp)
			}
		}
	}
}

func TestOnlyAssigneeWritesAndSubmits(t *testing.T) {
	for name, a := range actors {
		_, editErr := Edit(a, ctx(Draft, Inbox, 0))
		_, _, subErr := Submit(a, ctx(Draft, Inbox, 0))
		createErr := CanCreate(a, "in_progress")
		if name == "assignee" {
			if editErr != nil || subErr != nil || createErr != nil {
				t.Errorf("assignee blocked: %v %v %v", editErr, subErr, createErr)
			}
			continue
		}
		if code(editErr) != 403 || code(subErr) != 403 || code(createErr) != 403 {
			t.Errorf("%s may write: edit=%v submit=%v create=%v", name, editErr, subErr, createErr)
		}
	}
}

func TestCreateOnlyWhileWorking(t *testing.T) {
	for _, ps := range []string{"draft", "submitted", "needs_info", "completed", "cancelled"} {
		if code(CanCreate(actors["assignee"], ps)) != http.StatusConflict {
			t.Errorf("update allowed on %s project", ps)
		}
	}
}

func TestSubmitTransitions(t *testing.T) {
	a := actors["assignee"]
	for _, s := range All {
		to, _, err := Submit(a, ctx(s, OrderPage, 0))
		if s == Draft || s == ChangesRequested {
			if err != nil || to != PendingApproval {
				t.Errorf("submit from %s = %s %v", s, to, err)
			}
		} else if code(err) != http.StatusConflict {
			t.Errorf("submit from %s should be 409, got %v", s, err)
		}
	}
}

func TestInboxApprovalSetting(t *testing.T) {
	a := actors["assignee"]
	c := ctx(Draft, Inbox, 0)
	if to, auto, _ := Submit(a, c); to != PendingApproval || auto {
		t.Errorf("inbox with approval on: %s auto=%v", to, auto)
	}
	c.InboxRequiresApproval = false
	if to, auto, _ := Submit(a, c); to != Approved || !auto {
		t.Errorf("inbox with approval off: %s auto=%v", to, auto)
	}
	// The setting never affects other destinations.
	for _, d := range []Destination{OrderPage, Delivery} {
		c := Context{Status: Draft, Destination: d, Attachments: 1, ProjectStatus: "in_progress", InboxRequiresApproval: false}
		if to, _, _ := Submit(a, c); to != PendingApproval {
			t.Errorf("%s skipped approval: %s", d, to)
		}
	}
}

func TestDeliveryNeedsAttachment(t *testing.T) {
	if _, _, err := Submit(actors["assignee"], ctx(Draft, Delivery, 0)); code(err) != 422 {
		t.Errorf("submit delivery without attachment: %v", err)
	}
	if err := Approve(actors["tl"], ctx(PendingApproval, Delivery, 0)); code(err) != 422 {
		t.Errorf("approve delivery without attachment: %v", err)
	}
	if err := SetDestination(actors["tl"], ctx(Approved, Delivery, 0)); code(err) != 422 {
		t.Errorf("tl switching to delivery without attachment: %v", err)
	}
	if err := Approve(actors["tl"], ctx(PendingApproval, Delivery, 1)); err != nil {
		t.Errorf("approve delivery with attachment: %v", err)
	}
}

func TestApproveAndRequestChanges(t *testing.T) {
	for name, a := range actors {
		err := Approve(a, ctx(PendingApproval, Inbox, 0))
		if (name == "tl") != (err == nil) {
			t.Errorf("Approve by %s: %v", name, err)
		}
		err = RequestChanges(a, ctx(PendingApproval, Inbox, 0), "fix it")
		if (name == "tl") != (err == nil) {
			t.Errorf("RequestChanges by %s: %v", name, err)
		}
	}
	for _, s := range All {
		if s == PendingApproval {
			continue
		}
		if code(Approve(actors["tl"], ctx(s, Inbox, 0))) != http.StatusConflict {
			t.Errorf("approve from %s allowed", s)
		}
	}
	if code(RequestChanges(actors["tl"], ctx(PendingApproval, Inbox, 0), "  ")) != 422 {
		t.Error("request changes needs a comment")
	}
}

func TestEditingApprovedReturnsToPending(t *testing.T) {
	a := actors["assignee"]
	if to, _ := Edit(a, ctx(Approved, OrderPage, 0)); to != PendingApproval {
		t.Errorf("edit approved order-page update -> %s", to)
	}
	c := ctx(Approved, Inbox, 0)
	c.InboxRequiresApproval = false
	if to, _ := Edit(a, c); to != Approved {
		t.Errorf("edit auto-approved inbox update -> %s", to)
	}
	for _, s := range []Status{Draft, ChangesRequested, PendingApproval} {
		if to, _ := Edit(a, ctx(s, Inbox, 0)); to != s {
			t.Errorf("edit %s -> %s", s, to)
		}
	}
	if _, err := Edit(a, ctx(Sent, Inbox, 0)); code(err) != http.StatusConflict {
		t.Errorf("sent update editable: %v", err)
	}
}

func TestOnlyApprovedCanBeSentBySalesOwner(t *testing.T) {
	for _, s := range All {
		err := MarkSent(actors["sales-owner"], ctx(s, Inbox, 0))
		if (s == Approved) != (err == nil) {
			t.Errorf("MarkSent from %s: %v", s, err)
		}
	}
	for name, a := range actors {
		if name == "sales-owner" {
			continue
		}
		if code(MarkSent(a, ctx(Approved, Inbox, 0))) != 403 {
			t.Errorf("%s may mark sent", name)
		}
	}
	c := ctx(Approved, Inbox, 0)
	c.ProjectStatus = "cancelled"
	if code(MarkSent(actors["sales-owner"], c)) != http.StatusConflict {
		t.Error("sending on a cancelled project")
	}
}

func TestThreads(t *testing.T) {
	cases := map[string]struct{ ops, sales bool }{
		"sales-owner": {false, true},
		"sales-other": {false, false},
		"tl":          {true, true},
		"assignee":    {true, true},
		"member":      {false, false},
		"management":  {false, false},
	}
	for name, want := range cases {
		a := actors[name]
		if CanComment(a, OpsThread) != want.ops || CanComment(a, SalesThread) != want.sales {
			t.Errorf("%s comment rights wrong", name)
		}
	}
	for _, th := range ReadableThreads(actors["sales-owner"]) {
		if th == string(OpsThread) {
			t.Error("Sales can read the Ops thread")
		}
	}
	if len(ReadableThreads(actors["management"])) != 2 {
		t.Error("Management reads both threads")
	}
}
