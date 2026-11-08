package httpapi

import (
	"context"
	"testing"
	"time"

	"github.com/solvinx/opsflow/internal/db"
	"github.com/solvinx/opsflow/internal/testutil"
)

func updateOf(r resp) map[string]any {
	if u, ok := r.body["update"].(map[string]any); ok {
		return u
	}
	return nil
}

func TestUpdatesFlow(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	e.addUser("sales2", "sales", false)
	profile := testutil.Must(e.store.CreateProfile(ctx, db.CreateProfileParams{Name: "Studio Main"}))
	ptype := testutil.Must(e.store.CreateProjectType(ctx, db.CreateProjectTypeParams{Name: "Website"}))
	sales, sales2, tl, mem, mgmt, admin := e.login("sales"), e.login("sales2"), e.login("tl"), e.login("member"), e.login("ceo"), e.login("admin")
	future := time.Now().AddDate(0, 1, 0).Format(time.DateOnly)

	r := e.do("POST", "/api/projects", sales, map[string]any{
		"name": "Updates test", "profile_id": profile.ID, "client_name": "Client Co", "order_id": "U-1",
		"project_type_id": ptype.ID, "scope": "Scope", "agreed_price": 100, "deadline": future,
	})
	pid := projectOf(r)["id"].(string)
	ppath := "/api/projects/" + pid

	// Before assignment nobody can write updates.
	expect(t, e.do("POST", ppath+"/updates", mem, map[string]any{"message": "x"}), 404, "unassigned member")
	expect(t, e.do("POST", ppath+"/assign", tl, map[string]any{"assignee_id": e.users["member"].ID, "priority": "high"}), 200, "assign")
	for _, who := range []string{sales, tl, mgmt} {
		r := e.do("POST", ppath+"/updates", who, map[string]any{"message": "x"})
		if r.status != 403 {
			t.Fatalf("non-assignee created an update: %d", r.status)
		}
	}

	// ── Draft → pending → changes requested → pending → approved ──
	r = e.do("POST", ppath+"/updates", mem, map[string]any{"destination": "inbox"})
	expect(t, r, 201, "create draft")
	uid := updateOf(r)["id"].(string)
	upath := "/api/updates/" + uid
	if u := updateOf(r); u["status"] != "draft" || u["author_name"] != "member" || u["order_id"] != "U-1" || u["client_name"] != "Client Co" {
		t.Fatalf("draft = %v", u)
	}
	expect(t, e.do("GET", upath, sales, nil), 404, "sales can't see drafts")
	expect(t, e.do("GET", upath, tl, nil), 404, "tl doesn't see drafts")
	expect(t, e.do("GET", upath, mgmt, nil), 200, "management sees drafts")
	expect(t, e.do("POST", upath+"/submit", mem, nil), 422, "empty message")
	expect(t, e.do("PATCH", upath, mem, map[string]any{"message": "Hi! First draft of the homepage is ready.", "destination": "nowhere"}), 422, "bad destination")
	expect(t, e.do("PATCH", upath, mem, map[string]any{"message": "Hi! First draft of the homepage is ready."}), 200, "write")
	r = e.do("POST", upath+"/submit", mem, nil)
	expect(t, r, 200, "submit")
	if updateOf(r)["status"] != "pending_approval" || updateOf(r)["submitted_at"] == nil {
		t.Fatalf("after submit: %v", updateOf(r))
	}
	expect(t, e.do("GET", upath, sales, nil), 404, "sales can't see pending")
	expect(t, e.do("POST", upath+"/approve", mem, nil), 403, "member approve")
	expect(t, e.do("POST", upath+"/approve", mgmt, nil), 403, "mgmt approve")
	expect(t, e.do("POST", upath+"/request-changes", tl, map[string]any{"comment": " "}), 422, "changes need a reason")
	r = e.do("POST", upath+"/request-changes", tl, map[string]any{"comment": "Mention the staging link."})
	expect(t, r, 200, "request changes")
	if updateOf(r)["status"] != "changes_requested" || len(r.body["comments"].([]any)) != 1 {
		t.Fatalf("after changes requested: %v / %v", updateOf(r), r.body["comments"])
	}
	expect(t, e.do("POST", upath+"/sent", sales, nil), 404, "sales can't send what it can't see")
	expect(t, e.do("PATCH", upath, mem, map[string]any{"message": "Hi! First draft is ready: https://staging.example"}), 200, "rework")
	expect(t, e.do("POST", upath+"/submit", mem, nil), 200, "resubmit")
	expect(t, e.do("POST", upath+"/sent", sales, nil), 404, "pending can't be sent")
	r = e.do("POST", upath+"/approve", tl, map[string]any{"destination": "order_page"})
	expect(t, r, 200, "approve with destination correction")
	if u := updateOf(r); u["status"] != "approved" || u["destination"] != "order_page" || u["checked_by_name"] != "tl" || u["checked_at"] == nil {
		t.Fatalf("after approve: %v", u)
	}

	t.Run("sales sees approved, never the ops thread", func(t *testing.T) {
		r := e.do("GET", upath, sales, nil)
		expect(t, r, 200, "sales sees approved")
		if r.body["permissions"].(map[string]any)["can_read_ops"] != false || updateOf(r)["ops_comment_count"].(float64) != 0 {
			t.Fatalf("ops thread leaked to sales: %v", r.body["permissions"])
		}
		for _, c := range r.body["comments"].([]any) {
			if c.(map[string]any)["thread"] == "ops" {
				t.Fatal("sales received an ops comment")
			}
		}
		expect(t, e.do("POST", upath+"/comments", sales, map[string]any{"thread": "ops", "body": "x"}), 403, "sales ops comment")
		expect(t, e.do("POST", upath+"/comments", sales, map[string]any{"thread": "sales", "body": "Posting it now"}), 200, "sales thread")
		expect(t, e.do("POST", upath+"/comments", mgmt, map[string]any{"thread": "sales", "body": "x"}), 403, "mgmt comment")
		// Sales are one team: a colleague sees approved updates to send them while the owner is away.
		expect(t, e.do("GET", upath, sales2, nil), 200, "other sales")
		l := e.do("GET", "/api/updates?status=approved", sales, nil)
		if l.body["total"].(float64) != 1 {
			t.Fatalf("sales queue: %v", l.body["total"])
		}
		if l := e.do("GET", "/api/updates", sales, nil); l.body["total"].(float64) != 1 {
			t.Fatalf("sales sees non-approved updates in list: %v", l.body["total"])
		}
	})

	t.Run("editing approved text sends it back for approval", func(t *testing.T) {
		r := e.do("PATCH", upath, mem, map[string]any{"message": "Hi! First draft is ready: https://staging.example (v2)"})
		expect(t, r, 200, "edit approved")
		if u := updateOf(r); u["status"] != "pending_approval" || u["checked_by"] != nil {
			t.Fatalf("after editing approved: %v", u)
		}
		expect(t, e.do("GET", upath, sales, nil), 404, "hidden from sales again")
		expect(t, e.do("POST", upath+"/approve", tl, nil), 200, "re-approve")
	})

	t.Run("only sales mark sent, once", func(t *testing.T) {
		// An approved update rings every Sales browser until somebody sends it.
		unsent := func(cookie string) float64 {
			r := e.do("GET", "/api/alerts", cookie, nil)
			expect(t, r, 200, "alerts")
			for _, a := range r.body["items"].([]any) {
				if a := a.(map[string]any); a["kind"] == "unsent_updates" {
					return a["count"].(float64)
				}
			}
			return 0
		}
		if n := unsent(sales); n != 1 {
			t.Fatalf("approved but unsent = %v", n)
		}
		if n := unsent(sales2); n != 1 {
			t.Fatal("a Sales colleague should hear it too (they can send it)")
		}
		for who, cookie := range map[string]string{"member": mem, "tl": tl} {
			if n := unsent(cookie); n != 0 {
				t.Fatalf("%s is rung about sending updates: %v", who, n)
			}
		}
		expect(t, e.do("POST", upath+"/sent", mem, nil), 403, "member")
		expect(t, e.do("POST", upath+"/sent", tl, nil), 403, "tl")
		if r := e.do("GET", upath, sales2, nil); r.body["permissions"].(map[string]any)["can_mark_sent"] != true {
			t.Fatalf("a Sales colleague should be able to send it: %v", r.body["permissions"])
		}
		r := e.do("POST", upath+"/sent", sales, nil)
		expect(t, r, 200, "sent")
		if u := updateOf(r); u["status"] != "sent" || u["sent_by_name"] != "sales" || u["sent_at"] == nil {
			t.Fatalf("after sent: %v", u)
		}
		if n := unsent(sales); n != 0 {
			t.Fatalf("still ringing after it was sent: %v", n)
		}
		expect(t, e.do("POST", upath+"/sent", sales, nil), 409, "sent twice")
		expect(t, e.do("PATCH", upath, mem, map[string]any{"message": "x"}), 409, "edit sent")
		if s := projectOf(e.do("GET", ppath, tl, nil))["status"]; s != "in_progress" {
			t.Fatalf("an order-page update must not complete the project: %v", s)
		}
	})

	t.Run("inbox approval setting", func(t *testing.T) {
		expect(t, e.do("PUT", "/api/admin/settings/inbox_requires_approval", admin, map[string]any{"value": false}), 200, "turn off")
		r := e.do("POST", ppath+"/updates", mem, map[string]any{"message": "Quick check-in", "destination": "inbox"})
		r = e.do("POST", "/api/updates/"+updateOf(r)["id"].(string)+"/submit", mem, nil)
		if u := updateOf(r); u["status"] != "approved" || u["auto_approved"] != true || u["checked_by"] != nil {
			t.Fatalf("inbox with approval off: %v", u)
		}
		r = e.do("POST", ppath+"/updates", mem, map[string]any{"message": "Order note", "destination": "order_page"})
		r = e.do("POST", "/api/updates/"+updateOf(r)["id"].(string)+"/submit", mem, nil)
		if updateOf(r)["status"] != "pending_approval" {
			t.Fatalf("order page skipped approval: %v", updateOf(r)["status"])
		}
		expect(t, e.do("PUT", "/api/admin/settings/inbox_requires_approval", admin, map[string]any{"value": true}), 200, "turn on")
	})

	// ── Delivery: attachment required, sending completes the project, revisions are counted ──
	deliver := func(label string, wantRevision float64) string {
		t.Helper()
		r := e.do("POST", ppath+"/updates", mem, map[string]any{"message": "Here is the delivery " + label, "destination": "delivery"})
		expect(t, r, 201, "create delivery "+label)
		id := updateOf(r)["id"].(string)
		path := "/api/updates/" + id
		r = e.do("POST", path+"/submit", mem, nil)
		if r.status != 422 || fieldErr(r, "attachments") == "" {
			t.Fatalf("delivery without attachment: %d %v", r.status, r.body)
		}
		body := []byte("final files " + label)
		r = e.do("POST", path+"/attachments/uploads", mem, map[string]any{"filename": "final.zip", "size": len(body), "content_type": "application/zip"})
		expect(t, r, 201, "init attachment")
		putToStorage(t, r.body, body)
		r = e.do("POST", path+"/attachments/uploads/"+r.body["upload_id"].(string)+"/complete", mem, nil)
		expect(t, r, 200, "complete attachment")
		if len(r.body["attachments"].([]any)) != 1 {
			t.Fatalf("attachments = %v", r.body["attachments"])
		}
		att := r.body["attachments"].([]any)[0].(map[string]any)
		status := func() any { return projectOf(e.do("GET", ppath, tl, nil))["status"] }
		r = e.do("POST", path+"/submit", mem, nil)
		expect(t, r, 200, "submit delivery")
		if got := status(); got != "ready_for_review" {
			t.Fatalf("submitting a Delivery sends the project for the TL's check, got %v", got)
		}
		if updateOf(r)["revision_number"].(float64) != wantRevision {
			t.Fatalf("revision_number = %v, want %v", updateOf(r)["revision_number"], wantRevision)
		}
		// Sales can't fetch attachments before approval, and can after.
		if dl := e.raw("GET", "/api/files/versions/"+att["version_id"].(string)+"/download", sales); dl.StatusCode != 404 {
			t.Fatalf("sales downloaded a pending attachment: %d", dl.StatusCode)
		}
		expect(t, e.do("POST", path+"/approve", tl, nil), 200, "approve delivery")
		if got := status(); got != "ready_to_deliver" {
			t.Fatalf("approving the Delivery makes the project ready to deliver, got %v", got)
		}
		if dl := e.raw("GET", "/api/files/versions/"+att["version_id"].(string)+"/download", sales); dl.StatusCode != 302 {
			t.Fatalf("sales can't download approved attachment: %d", dl.StatusCode)
		}
		expect(t, e.do("POST", path+"/sent", sales, nil), 200, "send delivery")
		return id
	}

	deliver("v1", 0)
	p := e.do("GET", ppath, tl, nil)
	history := p.body["history"].([]any)
	last := history[len(history)-1].(map[string]any)
	if projectOf(p)["status"] != "delivered" || last["comment"] != "Delivery sent (update #4)" {
		t.Fatalf("delivery sent should mark the project delivered: %v / %v", projectOf(p)["status"], last)
	}
	expect(t, e.do("POST", ppath+"/transition", sales, map[string]any{"to": "completed"}), 200, "client accepted")
	expect(t, e.do("POST", ppath+"/updates", mem, map[string]any{"message": "x"}), 409, "no updates on completed project")

	expect(t, e.do("POST", ppath+"/transition", sales, map[string]any{"to": "in_progress", "comment": "Client wants a darker footer"}), 200, "revision")
	deliver("v2", 1)
	p = e.do("GET", ppath, mgmt, nil)
	history = p.body["history"].([]any)
	if c := history[len(history)-1].(map[string]any)["comment"]; projectOf(p)["status"] != "delivered" || c != "Revision 1 delivered (update #5)" {
		t.Fatalf("revision delivery: %v / %v", projectOf(p)["status"], c)
	}

	t.Run("lists and counts by role", func(t *testing.T) {
		all := e.do("GET", "/api/updates?project_id="+pid+"&page_size=200", mgmt, nil)
		if all.body["total"].(float64) != 5 {
			t.Fatalf("management total = %v", all.body["total"])
		}
		s := e.do("GET", "/api/updates?project_id="+pid, sales, nil)
		for _, u := range s.body["updates"].([]any) {
			if st := u.(map[string]any)["status"]; st != "approved" && st != "sent" {
				t.Fatalf("sales list contains %v", st)
			}
		}
		s2 := e.do("GET", "/api/updates?project_id="+pid, sales2, nil)
		expect(t, s2, 200, "other sales project filter")
		for _, u := range s2.body["updates"].([]any) {
			if st := u.(map[string]any)["status"]; st != "approved" && st != "sent" {
				t.Fatalf("other sales list contains %v", st)
			}
		}
		expect(t, e.do("GET", "/api/updates?status=bogus", tl, nil), 422, "bad filter")
	})
}
