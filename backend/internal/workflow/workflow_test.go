package workflow

import (
	"errors"
	"net/http"
	"testing"

	"github.com/solvinx/opsflow/internal/apperr"
	"github.com/solvinx/opsflow/internal/policy"
)

// Every kind of actor the API can present.
var actors = map[string]Actor{
	"sales-owner":     {Role: policy.Sales, IsOwner: true},
	"sales-other":     {Role: policy.Sales},
	"tl":              {Role: policy.TL},
	"tl-assignee":     {Role: policy.TL, IsAssignee: true}, // a leader who took the project on
	"member-assignee": {Role: policy.Member, IsAssignee: true},
	"member-other":    {Role: policy.Member},
	"management":      {Role: policy.CEO, IsOwner: true, IsAssignee: true}, // flags must not matter
}

type move struct{ from, to Status }

// spec is the allowed-transition table written from the requirements,
// independently of the rules slice.
var spec = map[move][]string{
	{InProgress, Blocked}:            {"member-assignee", "tl-assignee"},
	{Blocked, InProgress}:            {"member-assignee", "tl-assignee"},
	{InProgress, ReadyForReview}:     {"member-assignee"},   // the team sends it for delivery...
	{InProgress, ReadyToDeliver}:     {"tl-assignee"},       // ...a leader's own work skips the check
	{ReadyForReview, ReadyToDeliver}: {"tl", "tl-assignee"}, // the TL's check
	{ReadyForReview, InProgress}:     {"tl", "tl-assignee"},
	{ReadyToDeliver, Delivered}:      {"sales-owner", "sales-other"}, // Sales deliver on the marketplace
	{ReadyToDeliver, InProgress}:     {"sales-owner", "sales-other", "tl", "tl-assignee"},
	{Delivered, Completed}:           {"sales-owner", "sales-other", "tl", "tl-assignee"},
	{Delivered, InProgress}:          {"sales-owner", "sales-other", "tl", "tl-assignee"}, // revision
	{Completed, InProgress}:          {"sales-owner", "sales-other", "tl", "tl-assignee"}, // revision
	{New, Cancelled}:                 {"sales-owner", "tl", "tl-assignee"},
	{InProgress, Cancelled}:          {"sales-owner", "tl", "tl-assignee"},
	{Blocked, Cancelled}:             {"sales-owner", "tl", "tl-assignee"},
	{ReadyForReview, Cancelled}:      {"sales-owner", "tl", "tl-assignee"},
	{ReadyToDeliver, Cancelled}:      {"sales-owner", "tl", "tl-assignee"},
	{Delivered, Cancelled}:           {"sales-owner", "tl", "tl-assignee"},
}

func status(err error) int {
	var e *apperr.Error
	if errors.As(err, &e) {
		return e.Status
	}
	return 0
}

func TestEveryTransitionForEveryActor(t *testing.T) {
	for _, from := range All {
		for _, to := range All {
			allowed, exists := spec[move{from, to}]
			for name, actor := range actors {
				_, err := Check(from, to, actor, "a reason")
				switch {
				case !exists:
					if status(err) != http.StatusConflict {
						t.Errorf("%s -> %s by %s: want 409 invalid transition, got %v", from, to, name, err)
					}
				case contains(allowed, name):
					if err != nil {
						t.Errorf("%s -> %s by %s: want allowed, got %v", from, to, name, err)
					}
				default:
					if status(err) != http.StatusForbidden {
						t.Errorf("%s -> %s by %s: want 403, got %v", from, to, name, err)
					}
				}
			}
		}
	}
}

func TestRulesMatchSpecExactly(t *testing.T) {
	seen := map[move]bool{}
	for _, r := range rules {
		m := move{r.From, r.To}
		if seen[m] {
			t.Errorf("duplicate rule %s -> %s", r.From, r.To)
		}
		seen[m] = true
		if _, ok := spec[m]; !ok {
			t.Errorf("rule %s -> %s is not in the spec", r.From, r.To)
		}
	}
	for m := range spec {
		if !seen[m] {
			t.Errorf("spec move %s -> %s has no rule", m.from, m.to)
		}
	}
}

func TestCommentRequired(t *testing.T) {
	needs := []struct {
		m     move
		actor string
	}{
		{move{InProgress, Blocked}, "member-assignee"},
		{move{ReadyForReview, InProgress}, "tl"},
		{move{ReadyToDeliver, InProgress}, "sales-other"},
		{move{Delivered, InProgress}, "sales-other"},
		{move{Completed, InProgress}, "sales-owner"},
	}
	for _, n := range needs {
		_, err := Check(n.m.from, n.m.to, actors[n.actor], "   ")
		if status(err) != http.StatusUnprocessableEntity {
			t.Errorf("%s -> %s without comment: want 422, got %v", n.m.from, n.m.to, err)
		}
	}
	if _, err := Check(ReadyToDeliver, Delivered, actors["sales-other"], ""); err != nil {
		t.Errorf("marking delivered should not need a comment: %v", err)
	}
}

func TestTerminalStates(t *testing.T) {
	for _, to := range All {
		for name, a := range actors {
			if _, err := Check(Cancelled, to, a, "x"); err == nil {
				t.Errorf("cancelled -> %s by %s must be rejected", to, name)
			}
		}
	}
	for _, from := range []Status{Delivered, Completed} {
		r, err := Check(from, InProgress, actors["tl"], "client wants changes")
		if err != nil || !r.Revision {
			t.Fatalf("%s -> in_progress must be a revision: %+v %v", from, r, err)
		}
	}
}

func TestUnknownStatusIsValidationError(t *testing.T) {
	for _, bad := range []Status{"archived", Draft, Submitted, NeedsInfo, Assigned} {
		if _, err := Check(New, bad, actors["sales-owner"], ""); status(err) != http.StatusUnprocessableEntity {
			t.Fatalf("%s: want 422, got %v", bad, err)
		}
	}
	if Submitted.Label() != "Submitted" || ReadyForReview.Label() != "In Review" {
		t.Fatal("old statuses keep their labels for history")
	}
}

func TestManagementCanNeverActOrAssign(t *testing.T) {
	m := actors["management"]
	for _, from := range All {
		if len(Available(from, m)) != 0 || CanAssign(from, m) || CanMoveTeam(from, m) || CanPlan(from, m) || CanEditDetails(from, m) {
			t.Errorf("management has an action from %s", from)
		}
	}
}

func TestAssignment(t *testing.T) {
	ok := map[Status]bool{New: true, InProgress: true, Blocked: true}
	for _, s := range All {
		for name, a := range actors {
			want := ok[s] && (name == "tl" || name == "tl-assignee")
			if got := CanAssign(s, a); got != want {
				t.Errorf("CanAssign(%s, %s) = %v want %v", s, name, got, want)
			}
			if got := CanMoveTeam(s, a); got != want {
				t.Errorf("CanMoveTeam(%s, %s) = %v want %v", s, name, got, want)
			}
		}
	}
}

func TestAssigningStartsANewProject(t *testing.T) {
	if AfterAssign(New) != InProgress || AfterAssign(Blocked) != Blocked || AfterAssign(InProgress) != InProgress {
		t.Fatal("assigning starts a new project and keeps any other status")
	}
}

func TestSalesEditingStopsAtTerminal(t *testing.T) {
	for _, s := range All {
		want := !s.Terminal()
		if got := CanEditDetails(s, actors["sales-owner"]); got != want {
			t.Errorf("CanEditDetails(%s) = %v want %v", s, got, want)
		}
		if CanEditDetails(s, actors["sales-other"]) || CanEditDetails(s, actors["tl"]) {
			t.Errorf("only the owning Sales user may edit details (%s)", s)
		}
	}
}

func TestAvailableMatchesCheck(t *testing.T) {
	for _, from := range All {
		for name, a := range actors {
			for _, r := range Available(from, a) {
				if _, err := Check(from, r.To, a, "x"); err != nil {
					t.Errorf("Available offered %s -> %s to %s but Check rejects: %v", from, r.To, name, err)
				}
			}
		}
	}
}

func contains(xs []string, x string) bool {
	for _, v := range xs {
		if v == x {
			return true
		}
	}
	return false
}
