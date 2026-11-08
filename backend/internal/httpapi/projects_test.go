package httpapi

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/solvinx/opsflow/internal/db"
	"github.com/solvinx/opsflow/internal/notify"
	"github.com/solvinx/opsflow/internal/policy"
	"github.com/solvinx/opsflow/internal/testutil"
)

func projectOf(r resp) map[string]any {
	if p, ok := r.body["project"].(map[string]any); ok {
		return p
	}
	return nil
}

func errCode(r resp) string {
	if e, ok := r.body["error"].(map[string]any); ok {
		return fmt.Sprint(e["code"])
	}
	return ""
}

func fieldErr(r resp, field string) string {
	if e, ok := r.body["error"].(map[string]any); ok {
		if f, ok := e["fields"].(map[string]any); ok {
			return fmt.Sprint(f[field])
		}
	}
	return ""
}

func TestProjectWorkflow(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	e.addUser("sales2", policy.Sales, false)
	member2 := e.addUser("member2", policy.Member, false)
	profile := testutil.Must(e.store.CreateProfile(ctx, db.CreateProfileParams{Name: "Studio Main", Platform: "Fiverr"}))
	ptype := testutil.Must(e.store.CreateProjectType(ctx, db.CreateProjectTypeParams{Name: "Website"}))
	member := e.users["member"]

	sales, sales2 := e.login("sales"), e.login("sales2")
	tl, mem, mem2, mgmt := e.login("tl"), e.login("member"), e.login("member2"), e.login("ceo")
	future := time.Now().AddDate(0, 1, 0).Format(time.DateOnly)
	past := time.Now().AddDate(0, 0, -3).Format(time.DateOnly)

	transition := func(cookie, path, to, comment string) resp {
		return e.do("POST", path+"/transition", cookie, map[string]any{"to": to, "comment": comment})
	}

	// ── Create ──────────────────────────────────────────
	full := func(name, order string, extra map[string]any) map[string]any {
		b := map[string]any{
			"name": name, "profile_id": profile.ID, "client_name": "Acme Ltd", "order_id": order, "project_type_id": ptype.ID,
			"scope": "Build a Shopify store", "agreed_price": "1000.50", "deadline": future,
		}
		for k, v := range extra {
			b[k] = v
		}
		return b
	}
	// No drafts: a project goes straight to its team, so it needs everything up front.
	r := e.do("POST", "/api/projects", sales, map[string]any{"name": "Acme storefront"})
	expect(t, r, 422, "incomplete project")
	for _, f := range []string{"profile_id", "client_name", "order_id", "project_type_id", "scope", "agreed_price", "deadline"} {
		if fieldErr(r, f) == "" || fieldErr(r, f) == "<nil>" {
			t.Errorf("missing required error for %s: %v", f, r.body)
		}
	}
	r = e.do("POST", "/api/projects", sales, full("Acme storefront", "FO-1001", map[string]any{"priority": "high"}))
	expect(t, r, 201, "sales creates a project")
	p := projectOf(r)
	pid := p["id"].(string)
	path := "/api/projects/" + pid
	// The profile's 20%: 200.10 of 1000.50 to the marketplace.
	if p["status"] != "new" || p["currency"] != "USD" ||
		p["platform_fee_percent"] != "20.00" || p["platform_fee"] != "200.10" || p["net_amount"] != "800.40" {
		t.Fatalf("new project = %v", p)
	}
	if _, ok := p["budget"]; ok {
		t.Fatal("budget is gone")
	}

	for _, who := range []string{tl, mem, mgmt} {
		expect(t, e.do("POST", "/api/projects", who, map[string]any{"name": "x"}), 403, "only sales create")
	}
	expect(t, e.do("POST", "/api/projects", sales, map[string]any{"name": "x", "bogus": 1}), 422, "unknown field")
	expect(t, e.do("POST", "/api/projects", sales, map[string]any{"agreed_price": "12,00", "deadline": "tomorrow"}), 422, "bad formats")
	expect(t, e.do("POST", "/api/projects", sales, map[string]any{"name": "x", "budget": 100}), 422, "budget is no longer a field")

	t.Run("the fee comes from the profile, and stays with the project", func(t *testing.T) {
		r := e.do("POST", "/api/projects", sales, full("Fee check", "FO-FEE", map[string]any{"agreed_price": 1200, "client_name": "Fee Co"}))
		expect(t, r, 201, "project with a price")
		fp := "/api/projects/" + projectOf(r)["id"].(string)
		if q := projectOf(e.do("GET", fp, sales, nil)); q["platform_fee"] != "240.00" || q["net_amount"] != "960.00" {
			t.Fatalf("20%% of 1200: fee %v net %v", q["platform_fee"], q["net_amount"])
		}
		r = e.do("POST", "/api/admin/profiles", e.login("admin"), map[string]any{"name": "Upwork main", "platform": "Upwork", "fee_percent": 10})
		expect(t, r, 201, "profile with 10%")
		up := r.body["profile"].(map[string]any)["id"].(string)
		expect(t, e.do("PATCH", fp, sales, map[string]any{"profile_id": up}), 200, "move to the 10% profile")
		if q := projectOf(e.do("GET", fp, sales, nil)); q["platform_fee"] != "120.00" || q["net_amount"] != "1080.00" {
			t.Fatalf("after profile change: fee %v net %v", q["platform_fee"], q["net_amount"])
		}
		expect(t, e.do("PATCH", "/api/admin/profiles/"+up, e.login("admin"), map[string]any{"fee_percent": 15}), 200, "profile fee changes")
		if q := projectOf(e.do("GET", fp, sales, nil)); q["net_amount"] != "1080.00" {
			t.Fatalf("an existing project's fee changed with the profile: %v", q["net_amount"])
		}
		expect(t, e.do("PATCH", "/api/admin/profiles/"+up, e.login("admin"), map[string]any{"fee_percent": 120}), 422, "fee over 100")
		expect(t, transition(sales, fp, "cancelled", "Only a fee check"), 200, "cancel the fee check")
	})

	t.Run("the team sees a new project straight away", func(t *testing.T) {
		expect(t, e.do("GET", path, mgmt, nil), 200, "mgmt")
		expect(t, e.do("GET", path, tl, nil), 200, "tl")
		expect(t, e.do("GET", path, mem, nil), 404, "unassigned member")
		// Sales are one team: a colleague sees the project (to cover for the owner) but can't edit it.
		expect(t, e.do("GET", path, sales2, nil), 200, "other sales see every project")
		expect(t, e.do("PATCH", path, sales2, map[string]any{"name": "x"}), 403, "only the owner edits")
		list := e.do("GET", "/api/projects", sales2, nil)
		if list.body["total"].(float64) != 2 {
			t.Fatalf("sales2 sees %v projects", list.body["total"])
		}
	})

	t.Run("order id is unique per profile", func(t *testing.T) {
		r := e.do("POST", "/api/projects", sales2, map[string]any{"profile_id": profile.ID, "order_id": "fo-1001"})
		expect(t, r, 422, "duplicate order id")
		if fieldErr(r, "order_id") == "" {
			t.Fatalf("want order_id error: %v", r.body)
		}
	})

	for _, old := range []string{"submitted", "needs_info", "assigned", "draft"} {
		expect(t, transition(tl, path, old, "x"), 422, "the old "+old+" status is gone")
	}

	// ── Management and members can't touch it ───────────
	t.Run("management is read-only", func(t *testing.T) {
		expect(t, e.do("PATCH", path, mgmt, map[string]any{"name": "x"}), 403, "mgmt edit")
		expect(t, transition(mgmt, path, "cancelled", "x"), 403, "mgmt cancel")
		expect(t, e.do("POST", path+"/assign", mgmt, map[string]any{"assignee_id": member.ID, "priority": "low"}), 403, "mgmt assign")
		expect(t, e.do("POST", path+"/comments", mgmt, map[string]any{"body": "hi"}), 403, "mgmt comment")
		expect(t, e.do("PUT", path+"/planning", mgmt, map[string]any{"priority": "low"}), 403, "mgmt plan")
	})

	// ── Assignment ──────────────────────────────────────
	expect(t, e.do("POST", path+"/assign", sales, map[string]any{"assignee_id": member.ID, "priority": "high"}), 403, "sales assign")
	r = e.do("POST", path+"/assign", tl, map[string]any{"assignee_id": e.users["sales"].ID, "priority": "high"})
	if r.status != 422 || fieldErr(r, "assignee_id") == "" {
		t.Fatalf("assign to non-member: %d %v", r.status, r.body)
	}
	r = e.do("POST", path+"/assign", tl, map[string]any{"assignee_id": member.ID, "priority": "high", "due_date": past})
	if r.status != 422 || fieldErr(r, "due_date") == "" {
		t.Fatalf("past due date: %d %v", r.status, r.body)
	}
	r = e.do("POST", path+"/assign", tl, map[string]any{"assignee_id": member.ID, "priority": "urgent", "due_date": future})
	expect(t, r, 200, "assign")
	if p := projectOf(r); p["status"] != "in_progress" || p["assigned_to_name"] != "member" || p["priority"] != "urgent" {
		t.Fatalf("after assign: %v", p)
	}
	expect(t, e.do("GET", path, mem, nil), 200, "assignee sees it")
	expect(t, e.do("GET", path, mem2, nil), 404, "other member doesn't")

	t.Run("sales can't change priority after assignment", func(t *testing.T) {
		r := e.do("PATCH", path, sales, map[string]any{"priority": "low"})
		if r.status != 422 || fieldErr(r, "priority") == "" {
			t.Fatalf("got %d %v", r.status, r.body)
		}
	})

	// ── Work loop ───────────────────────────────────────
	expect(t, transition(mem, path, "ready_to_deliver", ""), 403, "a member's work goes to the TL's check first")
	expect(t, transition(tl, path, "completed", ""), 409, "complete before delivery")
	expect(t, transition(mem2, path, "blocked", "x"), 404, "non-assignee is not told it exists")
	expect(t, transition(mem, path, "blocked", " "), 422, "block without reason")
	expect(t, transition(mem, path, "blocked", "Waiting for logo"), 200, "block")
	expect(t, transition(mem, path, "in_progress", ""), 200, "unblock")
	expect(t, transition(mem, path, "ready_for_review", ""), 200, "send for delivery")
	expect(t, transition(mem, path, "ready_to_deliver", ""), 403, "member can't approve their own work")
	expect(t, transition(tl, path, "in_progress", ""), 422, "send back needs reason")
	expect(t, transition(tl, path, "in_progress", "Fix footer"), 200, "send back")
	expect(t, transition(mem, path, "ready_for_review", ""), 200, "send for delivery again")
	expect(t, transition(tl, path, "ready_to_deliver", ""), 200, "approve for delivery")
	// The handoff: every salesperson is told, flagged, since they cover for each other.
	for _, who := range []string{sales, sales2} {
		if n := ofType(e.notes(who), notify.ProjectToDeliver); len(n) != 1 || !n[0].Important || !strings.HasPrefix(n[0].Title, "Ready to deliver on Fiverr") {
			t.Fatalf("ready-to-deliver notification: %+v", n)
		}
	}
	if n := ofType(e.notes(mem), notify.ProjectToDeliver); len(n) != 0 {
		t.Fatal("the member doesn't need the Sales handoff")
	}
	expect(t, transition(tl, path, "delivered", ""), 403, "only Sales deliver on the marketplace")
	expect(t, transition(sales2, path, "delivered", ""), 200, "any salesperson marks it delivered")
	r = transition(sales, path, "completed", "")
	expect(t, r, 200, "complete")
	if projectOf(r)["completed_at"] == nil {
		t.Fatal("completed_at not set")
	}

	// ── After completion ────────────────────────────────
	r = e.do("PATCH", path, sales, map[string]any{"scope": "more"})
	if r.status != 409 {
		t.Fatalf("edit completed: %d %v", r.status, r.body)
	}
	// Any Sales may report the client's revision; the reason is still required.
	expect(t, transition(sales2, path, "in_progress", ""), 422, "other sales reach the revision rule")
	expect(t, transition(sales, path, "in_progress", ""), 422, "revision needs what the client asked")
	r = transition(sales, path, "in_progress", "Client wants a darker header")
	expect(t, r, 200, "revision")
	if p := projectOf(r); p["revision_count"].(float64) != 1 || p["completed_at"] != nil {
		t.Fatalf("after revision: %v", p)
	}

	// ── Sales edits after submission are tracked ────────
	newDeadline := time.Now().AddDate(0, 2, 0).Format(time.DateOnly)
	expect(t, e.do("PATCH", path, sales, map[string]any{"deadline": newDeadline, "special_notes": "Rush"}), 200, "edit")
	r = e.do("PATCH", path, sales, map[string]any{"name": nil})
	if r.status != 422 || fieldErr(r, "name") == "" {
		t.Fatalf("clearing a required field after submit: %d %v", r.status, r.body)
	}
	detail := e.do("GET", path, tl, nil)
	changes := detail.body["changes"].([]any)
	var sawImportant bool
	for _, c := range changes {
		c := c.(map[string]any)
		// Deadlines are stored as a moment: a plain date means the end of that day.
		endOfDay := func(d string) string { return mustEndOfDay(t, d) }
		if c["field"] == "deadline" && c["is_important"] == true && c["new_value"] == endOfDay(newDeadline) && c["old_value"] == endOfDay(future) {
			sawImportant = true
		}
	}
	if !sawImportant {
		t.Fatalf("deadline change not recorded as important: %v", changes)
	}

	t.Run("stale lock_version is rejected", func(t *testing.T) {
		lv := projectOf(e.do("GET", path, sales, nil))["lock_version"].(float64)
		expect(t, e.do("PATCH", path, sales, map[string]any{"client_contact": "a@b.c", "lock_version": lv}), 200, "fresh")
		expect(t, e.do("PATCH", path, sales, map[string]any{"client_contact": "x@y.z", "lock_version": lv}), 409, "stale")
	})

	// ── Reassignment ────────────────────────────────────
	expect(t, e.do("POST", path+"/assign", tl, map[string]any{"assignee_id": member.ID, "priority": "high"}), 409, "same member")
	r = e.do("POST", path+"/assign", tl, map[string]any{"assignee_id": member2.ID, "priority": "high"})
	expect(t, r, 200, "reassign")
	if projectOf(r)["status"] != "in_progress" {
		t.Fatalf("reassigning keeps the status: %v", projectOf(r)["status"])
	}
	expect(t, e.do("GET", path, mem, nil), 404, "old assignee loses access")
	expect(t, e.do("GET", path, mem2, nil), 200, "new assignee sees it")
	assignments := e.do("GET", path, tl, nil).body["assignments"].([]any)
	if len(assignments) != 2 || assignments[0].(map[string]any)["unassigned_at"] == nil {
		t.Fatalf("assignment history: %v", assignments)
	}

	t.Run("planning", func(t *testing.T) {
		expect(t, e.do("PUT", path+"/planning", tl, map[string]any{"priority": "low", "due_date": past}), 422, "past due")
		r := e.do("PUT", path+"/planning", tl, map[string]any{"priority": "low", "due_date": future})
		expect(t, r, 200, "plan")
		if projectOf(r)["priority"] != "low" || projectOf(r)["due_date"] != future {
			t.Fatalf("planning: %v", projectOf(r))
		}
		expect(t, e.do("PUT", path+"/planning", mem2, map[string]any{"priority": "urgent"}), 403, "member plan")
	})

	// ── Comments ────────────────────────────────────────
	expect(t, e.do("POST", path+"/comments", mem2, map[string]any{"body": "On it"}), 201, "assignee comments")
	expect(t, e.do("POST", path+"/comments", sales, map[string]any{"body": "Thanks"}), 201, "owner comments")
	expect(t, e.do("POST", path+"/comments", mem, map[string]any{"body": "x"}), 404, "ex-assignee")
	expect(t, e.do("POST", path+"/comments", tl, map[string]any{"body": "  "}), 422, "empty comment")

	// ── History is complete and attributed ──────────────
	detail = e.do("GET", path, mgmt, nil)
	history := detail.body["history"].([]any)
	wantSeq := []string{"new", "in_progress", "blocked", "in_progress", "ready_for_review", "in_progress", "ready_for_review",
		"ready_to_deliver", "delivered", "completed", "in_progress", "in_progress"}
	if len(history) != len(wantSeq) {
		t.Fatalf("history has %d entries, want %d: %v", len(history), len(wantSeq), history)
	}
	for i, h := range history {
		h := h.(map[string]any)
		if h["to_status"] != wantSeq[i] || h["changed_by_name"] == "" {
			t.Errorf("history[%d] = %v, want to_status %s", i, h, wantSeq[i])
		}
	}
	if len(detail.body["comments"].([]any)) != 2 {
		t.Fatal("comments missing")
	}
	if a := detail.body["actions"].([]any); len(a) != 0 {
		t.Fatalf("management must get no actions, got %v", a)
	}

	t.Run("management writes are refused before any business rule", func(t *testing.T) {
		// Even moves that don't exist (completed -> cancelled is fine here, project is assigned)
		// answer 403, never 409/422, for the read-only role.
		for _, req := range []struct{ m, p string }{
			{"POST", path + "/transition"}, {"PATCH", path}, {"POST", path + "/acknowledge"},
			{"POST", path + "/files/uploads"}, {"POST", "/api/projects"}, {"POST", path + "/updates"},
			{"DELETE", path + "/files/" + pid}, {"POST", "/api/updates/" + pid + "/sent"},
		} {
			expect(t, e.do(req.m, req.p, mgmt, map[string]any{"to": "bogus"}), 403, "mgmt "+req.m+" "+req.p)
		}
		expect(t, e.do("POST", path+"/view", mgmt, nil), 200, "mgmt may mark viewed")
		expect(t, e.do("POST", "/api/notifications/read-all", mgmt, nil), 200, "mgmt may read notifications")
	})

	// ── Cancel ends everything ──────────────────────────
	expect(t, transition(mem2, path, "cancelled", "x"), 403, "member cancel")
	expect(t, transition(sales, path, "cancelled", "Client refunded"), 200, "cancel")
	for _, to := range []string{"in_progress", "new", "completed", "delivered"} {
		expect(t, transition(tl, path, to, "x"), 409, "from cancelled to "+to)
	}
	expect(t, e.do("PATCH", path, sales, map[string]any{"name": "x"}), 409, "edit cancelled")
	expect(t, e.do("POST", path+"/assign", tl, map[string]any{"assignee_id": member.ID, "priority": "low"}), 409, "assign cancelled")
	expect(t, transition(tl, path, "bogus", "x"), 422, "unknown status")

	// ── List: scope, filters and counts ─────────────────
	t.Run("list", func(t *testing.T) {
		// A second project: new, overdue-capable via a deadline we backdate in the DB.
		r := e.do("POST", "/api/projects", sales2, map[string]any{
			"name": "Beta app", "profile_id": profile.ID, "client_name": "Beta", "order_id": "FO-2",
			"project_type_id": ptype.ID, "scope": "App", "agreed_price": 10, "deadline": future,
		})
		expect(t, r, 201, "create 2")
		id2 := projectOf(r)["id"].(string)
		if _, err := e.store.Pool.Exec(ctx, `UPDATE projects SET deadline = $1 WHERE id = $2`, past, id2); err != nil {
			t.Fatal(err)
		}

		l := e.do("GET", "/api/projects", tl, nil)
		if l.body["total"].(float64) != 3 {
			t.Fatalf("tl total = %v", l.body["total"])
		}
		counts := l.body["counts"].(map[string]any)
		if counts["cancelled"].(float64) != 2 || counts["new"].(float64) != 1 {
			t.Fatalf("counts = %v", counts)
		}
		l = e.do("GET", "/api/projects?overdue=true", tl, nil)
		if l.body["total"].(float64) != 1 || l.body["projects"].([]any)[0].(map[string]any)["is_overdue"] != true {
			t.Fatalf("overdue filter: %v", l.body)
		}
		l = e.do("GET", "/api/projects?q=acme", tl, nil)
		if l.body["total"].(float64) != 1 {
			t.Fatalf("search: %v", l.body["total"])
		}
		l = e.do("GET", "/api/projects?status=new,blocked", tl, nil)
		if l.body["total"].(float64) != 1 {
			t.Fatalf("status filter: %v", l.body["total"])
		}
		// member2 was still assigned when it was cancelled, so keeps seeing it; the
		// previous assignee and other members see nothing.
		if l = e.do("GET", "/api/projects", mem2, nil); l.body["total"].(float64) != 1 {
			t.Fatalf("current assignee list: %v", l.body["total"])
		}
		if l = e.do("GET", "/api/projects", mem, nil); l.body["total"].(float64) != 0 {
			t.Fatalf("previous assignee still sees projects: %v", l.body["total"])
		}
		expect(t, e.do("GET", "/api/projects?status=nope", tl, nil), 422, "bad status filter")
		expect(t, e.do("GET", "/api/projects?page_size=500", tl, nil), 422, "page size")
	})
}

// mustEndOfDay renders a plain date the way a stored delivery deadline comes back:
// the last minute of that day in the organisation's timezone, as ISO 8601 UTC.
func mustEndOfDay(t *testing.T, day string) string {
	t.Helper()
	loc, err := time.LoadLocation("Asia/Dhaka")
	if err != nil {
		t.Fatal(err)
	}
	d, err := time.ParseInLocation(time.DateOnly, day, loc)
	if err != nil {
		t.Fatal(err)
	}
	return d.Add(23*time.Hour + 59*time.Minute).UTC().Format(time.RFC3339)
}

// Store access: Sales and the project's Ops people record whether access to the
// client's store has been asked for or given, with the client's link.
func TestStoreAccess(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	e.addUser("member2", policy.Member, false)
	profile := testutil.Must(e.store.CreateProfile(ctx, db.CreateProfileParams{Name: "Studio Main", Platform: "Fiverr"}))
	admin, sales, tl, mem, mem2, ceo := e.login("admin"), e.login("sales"), e.login("tl"), e.login("member"), e.login("member2"), e.login("ceo")
	ptype := e.do("POST", "/api/admin/project-types", admin, map[string]any{"name": "Store"}).body["project_type"].(map[string]any)["id"]
	r := e.do("POST", "/api/projects", sales, map[string]any{
		"name": "Acme store", "profile_id": profile.ID, "client_name": "Acme", "order_id": "FO-1", "project_type_id": ptype,
		"scope": "Build it", "agreed_price": "100", "deadline": time.Now().AddDate(0, 1, 0).Format(time.DateOnly),
	})
	p := projectOf(r)
	path := "/api/projects/" + p["id"].(string)
	if p["store_access"] != "not_requested" || p["store_access_link"] != nil {
		t.Fatalf("new project access: %v %v", p["store_access"], p["store_access_link"])
	}
	expect(t, e.do("POST", path+"/assign", tl, map[string]any{"assignee_id": e.users["member"].ID, "priority": "high"}), 200, "assign")

	set := func(cookie string, body map[string]any) resp { return e.do("PUT", path+"/store-access", cookie, body) }
	expect(t, set(mem, map[string]any{"status": "requested"}), 200, "assigned member asks for access")
	r = set(sales, map[string]any{"status": "given", "link": "https://acme.myshopify.com/admin"})
	expect(t, r, 200, "sales records the client's link")
	if q := projectOf(r); q["store_access"] != "given" || q["store_access_link"] != "https://acme.myshopify.com/admin" {
		t.Fatalf("after given: %v %v", q["store_access"], q["store_access_link"])
	}
	if q := projectOf(set(tl, map[string]any{"status": "given"})); q["store_access_link"] != "https://acme.myshopify.com/admin" {
		t.Fatal("changing only the status dropped the link")
	}
	expect(t, set(sales, map[string]any{"status": "maybe"}), 422, "unknown status")
	expect(t, set(sales, map[string]any{"status": "given", "link": "javascript:alert(1)"}), 422, "not a web link")
	expect(t, set(mem2, map[string]any{"status": "requested"}), 404, "member off the project")
	expect(t, set(ceo, map[string]any{"status": "requested"}), 403, "CEO reads only")

	d := e.do("GET", path, mem, nil)
	if d.body["permissions"].(map[string]any)["can_set_access"] != true {
		t.Fatal("the assigned member should be able to set access")
	}
	found := false
	for _, c := range d.body["changes"].([]any) {
		m := c.(map[string]any)
		found = found || (m["field"] == "store_access" && m["new_value"] == "Access given")
	}
	if !found {
		t.Fatalf("store access change not in the project's history: %v", d.body["changes"])
	}

	tok := e.personalToken(mem, "Claude")
	out, msg := e.tool(tok, "set_store_access", map[string]any{"project": fmt.Sprint(int(p["number"].(float64))), "status": "requested", "link": ""})
	if msg != "" || out["project"].(map[string]any)["store_access"] != "requested" {
		t.Fatalf("assistant set access: %v %s", out, msg)
	}
	if q := projectOf(e.do("GET", path, mem, nil)); q["store_access_link"] != nil {
		t.Fatalf("empty link should clear it: %v", q["store_access_link"])
	}
}
