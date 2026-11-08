package httpapi

import (
	"context"
	"encoding/csv"
	"io"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/solvinx/opsflow/internal/db"
	"github.com/solvinx/opsflow/internal/testutil"
)

func (e *env) get(path, cookie string) (int, string) {
	e.t.Helper()
	req := httptest.NewRequest("GET", path, nil)
	req.AddCookie(cookieFor(cookie))
	res, err := e.app.Test(req)
	if err != nil {
		e.t.Fatal(err)
	}
	b, _ := io.ReadAll(res.Body)
	return res.StatusCode, string(b)
}

func TestReportsExportsAndActivity(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	profile := testutil.Must(e.store.CreateProfile(ctx, db.CreateProfileParams{Name: "Studio Main"}))
	ptype := testutil.Must(e.store.CreateProjectType(ctx, db.CreateProjectTypeParams{Name: "Website"}))
	sales, tl, mem, mgmt := e.login("sales"), e.login("tl"), e.login("member"), e.login("ceo")
	future := time.Now().AddDate(0, 1, 0).Format(time.DateOnly)

	mk := func(name, order string, price int) string {
		r := e.do("POST", "/api/projects", sales, map[string]any{
			"name": name, "profile_id": profile.ID, "client_name": "Client " + name, "order_id": order,
			"project_type_id": ptype.ID, "scope": "Scope", "agreed_price": price, "deadline": future,
		})
		expect(t, r, 201, "create "+name)
		return projectOf(r)["id"].(string)
	}
	tr := func(cookie, id, to, comment string) {
		t.Helper()
		expect(t, e.do("POST", "/api/projects/"+id+"/transition", cookie, map[string]any{"to": to, "comment": comment}), 200, id+" -> "+to)
	}
	assign := func(id string) {
		expect(t, e.do("POST", "/api/projects/"+id+"/assign", tl, map[string]any{"assignee_id": e.users["member"].ID, "priority": "medium"}), 200, "assign")
	}

	// A: completed with one revision. B: in progress and overdue. C: new, unassigned. D: cancelled.
	a, b := mk("Alpha", "R-1", 1000), mk("Beta", "R-2", 500)
	mk("Gamma", "R-3", 250)
	tr(sales, mk("Cancelled one", "R-4", 10), "cancelled", "Client changed their mind")
	assign(a)
	assign(b)
	deliver := func() {
		tr(mem, a, "ready_for_review", "")
		tr(tl, a, "ready_to_deliver", "")
		tr(sales, a, "delivered", "")
		tr(sales, a, "completed", "")
	}
	deliver()
	tr(sales, a, "in_progress", "Client wants changes")
	deliver()
	past := time.Now().AddDate(0, 0, -3).Format(time.DateOnly)
	if _, err := e.store.Pool.Exec(ctx, `UPDATE projects SET due_date = $1 WHERE id = $2`, past, b); err != nil {
		t.Fatal(err)
	}

	// Updates on B: one approved after being sent back once, one with a formula-like message.
	r := e.do("POST", "/api/projects/"+b+"/updates", mem, map[string]any{"message": "First look", "destination": "order_page"})
	u1 := updateOf(r)["id"].(string)
	expect(t, e.do("POST", "/api/updates/"+u1+"/submit", mem, nil), 200, "submit u1")
	expect(t, e.do("POST", "/api/updates/"+u1+"/request-changes", tl, map[string]any{"comment": "Add link"}), 200, "changes u1")
	expect(t, e.do("POST", "/api/updates/"+u1+"/submit", mem, nil), 200, "resubmit u1")
	expect(t, e.do("POST", "/api/updates/"+u1+"/approve", tl, nil), 200, "approve u1")
	expect(t, e.do("POST", "/api/updates/"+u1+"/sent", sales, nil), 200, "send u1")
	r = e.do("POST", "/api/projects/"+b+"/updates", mem, map[string]any{"message": "=HYPERLINK(\"http://evil\")", "destination": "inbox"})
	u2 := updateOf(r)["id"].(string)
	expect(t, e.do("POST", "/api/updates/"+u2+"/submit", mem, nil), 200, "submit u2")

	t.Run("dashboard is management-only", func(t *testing.T) {
		for _, who := range []string{sales, tl, mem} {
			expect(t, e.do("GET", "/api/reports/summary", who, nil), 403, "summary")
			if code, _ := e.get("/api/reports/export/updates.csv", who); code != 403 {
				t.Fatalf("export allowed: %d", code)
			}
		}
		expect(t, e.do("GET", "/api/reports/summary?from=2026-13-01", mgmt, nil), 422, "bad date")
		expect(t, e.do("GET", "/api/reports/summary?from=2026-05-02&to=2026-05-01", mgmt, nil), 422, "inverted range")
	})

	t.Run("summary numbers", func(t *testing.T) {
		r := e.do("GET", "/api/reports/summary", mgmt, nil)
		expect(t, r, 200, "summary")
		byStatus := map[string]float64{}
		for _, s := range r.body["projects_by_status"].([]any) {
			byStatus[s.(map[string]any)["status"].(string)] = s.(map[string]any)["n"].(float64)
		}
		if byStatus["completed"] != 1 || byStatus["in_progress"] != 1 || byStatus["new"] != 1 || byStatus["cancelled"] != 1 {
			t.Fatalf("by status: %v", byStatus)
		}
		m := r.body["projects_by_member"].([]any)[0].(map[string]any)
		if m["name"] != "member" || m["active"].(float64) != 1 || m["overdue"].(float64) != 1 || m["completed"].(float64) != 1 {
			t.Fatalf("by member: %v", m)
		}
		// Due at the end of that day, so three calendar days back is two whole days late.
		if o := r.body["overdue"].([]any); len(o) != 1 || o[0].(map[string]any)["days_overdue"].(float64) != 2 {
			t.Fatalf("overdue: %v", o)
		}
		if u := r.body["unassigned"].([]any); len(u) != 1 || u[0].(map[string]any)["name"] != "Gamma" {
			t.Fatalf("unassigned: %v", u)
		}
		timing := r.body["timing"].(map[string]any)
		if timing["assigned_n"].(float64) != 2 || timing["completed_n"].(float64) != 1 {
			t.Fatalf("timing: %v", timing)
		}
		ap := r.body["approval_turnaround"].(map[string]any)
		if ap["n"].(float64) != 2 || ap["sent_back"].(float64) != 1 {
			t.Fatalf("approval turnaround: %v", ap)
		}
		rev := r.body["revisions"].(map[string]any)
		if rev["completed_n"].(float64) != 1 || rev["avg_revisions"].(float64) != 1 || rev["with_revisions"].(float64) != 1 {
			t.Fatalf("revisions: %v", rev)
		}
		s := r.body["sales_by_person"].([]any)[0].(map[string]any)
		if s["name"] != "sales" || s["created"].(float64) != 4 || s["completed"].(float64) != 1 ||
			// 1000 agreed, less the marketplace's 20%.
			s["revenue"].(map[string]any)["USD"] != "800.00" {
			t.Fatalf("sales by person: %v", s)
		}
		dest := map[string]float64{}
		for _, d := range r.body["updates_by_destination_status"].([]any) {
			d := d.(map[string]any)
			dest[d["destination"].(string)+"/"+d["status"].(string)] = d["n"].(float64)
		}
		if dest["order_page/sent"] != 1 || dest["inbox/pending_approval"] != 1 {
			t.Fatalf("updates by destination: %v", dest)
		}
	})

	t.Run("workload and the company dashboard", func(t *testing.T) {
		// Workload is for the Team Leader and the CEO; the company view is the CEO's alone.
		for _, who := range []string{sales, mem} {
			expect(t, e.do("GET", "/api/reports/workload", who, nil), 403, "workload")
		}
		for _, who := range []string{sales, tl, mem} {
			expect(t, e.do("GET", "/api/reports/company", who, nil), 403, "company")
		}

		r := e.do("GET", "/api/reports/workload", tl, nil)
		expect(t, r, 200, "workload for the lead")
		if r.body["scope"] != "team" {
			t.Fatalf("scope: %v", r.body["scope"])
		}
		rows := r.body["members"].([]any)
		if len(rows) != 1 {
			t.Fatalf("members: %v", rows)
		}
		// Beta is open and late; Alpha was completed just now.
		m := rows[0].(map[string]any)
		if m["name"] != "member" || m["open_projects"].(float64) != 1 || m["overdue_projects"].(float64) != 1 ||
			m["completed_30d"].(float64) != 1 || m["on_leave_today"] != false {
			t.Fatalf("workload row: %v", m)
		}
		if ceoView := e.do("GET", "/api/reports/workload", mgmt, nil); ceoView.body["scope"] != "company" {
			t.Fatalf("CEO scope: %v", ceoView.body["scope"])
		}
		// This lead has no team here, so there are no team numbers to show them.
		if teams := r.body["teams"].([]any); len(teams) != 0 {
			t.Fatalf("a lead without a team got team numbers: %v", teams)
		}

		c := e.do("GET", "/api/reports/company", mgmt, nil)
		expect(t, c, 200, "company")
		k := c.body["kpis"].(map[string]any)
		if k["live_projects"].(float64) != 2 || k["unassigned_projects"].(float64) != 1 ||
			k["overdue_projects"].(float64) != 1 || k["completed_this_month"].(float64) != 1 ||
			k["updates_pending_approval"].(float64) != 1 || k["updates_unsent"].(float64) != 0 ||
			k["headcount"].(float64) < 4 {
			t.Fatalf("kpis: %v", k)
		}
		if rev := c.body["revenue_this_month"].([]any); len(rev) != 1 ||
			rev[0].(map[string]any)["currency"] != "USD" || rev[0].(map[string]any)["total"] != "800.00" { // net of the 20% fee
			t.Fatalf("revenue: %v", c.body["revenue_this_month"])
		}
		if o := c.body["overdue"].([]any); len(o) != 1 || o[0].(map[string]any)["name"] != "Beta" {
			t.Fatalf("overdue: %v", o)
		}
		if un := c.body["unassigned"].([]any); len(un) != 1 || un[0].(map[string]any)["name"] != "Gamma" {
			t.Fatalf("unassigned: %v", un)
		}
	})

	t.Run("updates CSV matches the sheet and is injection-safe", func(t *testing.T) {
		code, body := e.get("/api/reports/export/updates.csv", mgmt)
		if code != 200 || !strings.HasPrefix(body, "\xEF\xBB\xBF") {
			t.Fatalf("export: %d %q", code, body[:min(40, len(body))])
		}
		rows, err := csv.NewReader(strings.NewReader(strings.TrimPrefix(body, "\xEF\xBB\xBF"))).ReadAll()
		if err != nil {
			t.Fatal(err)
		}
		if strings.Join(rows[0][:11], "|") != "Update ID|Date|Project #|Project|Order ID|Profile Name|Client Name|Update By|Update Message|Update To|Status" {
			t.Fatalf("header: %v", rows[0])
		}
		if len(rows) != 3 {
			t.Fatalf("want 2 updates, got %d rows", len(rows)-1)
		}
		var sawFormula, sawSent bool
		for _, row := range rows[1:] {
			if strings.HasPrefix(row[8], "'=HYPERLINK") {
				sawFormula = true
			}
			if row[10] == "Sent" && row[12] == "tl" && row[14] == "sales" && row[9] == "Order page" {
				sawSent = true
			}
		}
		if !sawFormula || !sawSent {
			t.Fatalf("rows: %v", rows[1:])
		}
		_, body = e.get("/api/reports/export/updates.csv?status=sent", mgmt)
		if n := strings.Count(strings.TrimSpace(body), "\n"); n != 1 {
			t.Fatalf("status filter: %d data rows", n)
		}
		if code, _ := e.get("/api/reports/export/updates.csv?status=bogus", mgmt); code != 422 {
			t.Fatalf("bad filter: %d", code)
		}
	})

	t.Run("projects CSV", func(t *testing.T) {
		code, body := e.get("/api/reports/export/projects.csv", mgmt)
		rows, _ := csv.NewReader(strings.NewReader(strings.TrimPrefix(body, "\xEF\xBB\xBF"))).ReadAll()
		if code != 200 || len(rows) != 5 {
			t.Fatalf("projects export: %d, %d rows", code, len(rows))
		}
		var beta []string
		for _, row := range rows {
			if row[1] == "Beta" {
				beta = row
			}
		}
		// Agreed price, then the fee % / fee / net columns, then the dates.
		if beta == nil || beta[18] != "Yes" || beta[8] != "member" || beta[10] != "500.00" ||
			beta[11] != "20.00" || beta[12] != "100.00" || beta[13] != "400.00" {
			t.Fatalf("Beta row: %v", beta)
		}
	})

	t.Run("activity feed", func(t *testing.T) {
		expect(t, e.do("GET", "/api/activity", mem, nil), 403, "member")
		expect(t, e.do("GET", "/api/activity", sales, nil), 403, "sales")
		all := e.do("GET", "/api/activity?limit=200", mgmt, nil).body["activity"].([]any)
		tlFeed := e.do("GET", "/api/activity?limit=200", tl, nil).body["activity"].([]any)
		var mgmtLogins, tlNonProject int
		for _, a := range all {
			if a.(map[string]any)["action"] == "auth.login" {
				mgmtLogins++
			}
		}
		for _, a := range tlFeed {
			a := a.(map[string]any)
			if a["project_id"] == nil {
				tlNonProject++
			}
		}
		if mgmtLogins == 0 || tlNonProject != 0 {
			t.Fatalf("mgmt logins %d, TL non-project rows %d", mgmtLogins, tlNonProject)
		}
		page1 := e.do("GET", "/api/activity?limit=5", mgmt, nil).body["activity"].([]any)
		last := int64(page1[4].(map[string]any)["id"].(float64))
		page2 := e.do("GET", "/api/activity?limit=5&before_id="+ftoa(float64(last)), mgmt, nil).body["activity"].([]any)
		if int64(page2[0].(map[string]any)["id"].(float64)) >= last {
			t.Fatal("pagination went backwards")
		}
		onlyB := e.do("GET", "/api/activity?project_id="+b, mgmt, nil).body["activity"].([]any)
		for _, a := range onlyB {
			if a.(map[string]any)["project_id"] != b {
				t.Fatal("project filter leaked other projects")
			}
		}
	})
}
