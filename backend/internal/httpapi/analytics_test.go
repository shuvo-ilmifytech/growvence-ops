package httpapi

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/solvinx/opsflow/internal/db"
	"github.com/solvinx/opsflow/internal/policy"
	"github.com/solvinx/opsflow/internal/testutil"
)

func TestAnalytics(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	e.addUser("tl2", policy.TL, false)
	e.addUser("sales2", policy.Sales, false)
	sales, tl, mem, ceo, hr := e.login("sales"), e.login("tl"), e.login("member"), e.login("ceo"), e.login("hr")

	// Shopify (lead tl, member) gets the Shopify type; Web (lead tl2) gets nothing in this test.
	r := e.do("POST", "/api/admin/teams", hr, map[string]any{"name": "Shopify", "lead_id": e.users["tl"].ID})
	expect(t, r, 201, "team")
	shopify := r.body["team"].(map[string]any)["id"].(string)
	r = e.do("POST", "/api/admin/teams", hr, map[string]any{"name": "Web", "lead_id": e.users["tl2"].ID})
	expect(t, r, 201, "team")
	web := r.body["team"].(map[string]any)["id"].(string)
	expect(t, e.do("PUT", "/api/admin/teams/"+shopify+"/members/"+e.users["member"].ID.String(), hr, nil), 204, "add member")
	r = e.do("POST", "/api/admin/project-types", ceo, map[string]any{"name": "Shopify Store", "team_id": shopify})
	expect(t, r, 201, "type")
	ptype := r.body["project_type"].(map[string]any)["id"].(string)
	profile := testutil.Must(e.store.CreateProfile(ctx, db.CreateProfileParams{Name: "Studio Main"}))
	future := time.Now().AddDate(0, 1, 0).Format(time.DateOnly)

	mk := func(name, order string) string {
		r := e.do("POST", "/api/projects", sales, map[string]any{
			"name": name, "profile_id": profile.ID, "client_name": "Client " + name, "order_id": order,
			"project_type_id": ptype, "scope": "Scope", "agreed_price": 100, "deadline": future,
		})
		expect(t, r, 201, "create "+name)
		return projectOf(r)["id"].(string)
	}
	tr := func(cookie, id, to string) {
		t.Helper()
		expect(t, e.do("POST", "/api/projects/"+id+"/transition", cookie, map[string]any{"to": to, "comment": "Because"}), 200, id+" -> "+to)
	}
	deliver := func(id string) {
		tr(mem, id, "ready_for_review")
		tr(tl, id, "ready_to_deliver")
		tr(sales, id, "delivered")
	}

	// A: delivered on time and completed. B: delivered late (due three days ago), after a
	// revision. C: new. D: cancelled. E: in progress and overdue.
	a, b, _, d, ee := mk("Alpha", "A-1"), mk("Beta", "B-1"), mk("Gamma", "C-1"), mk("Delta", "D-1"), mk("Echo", "E-1")
	for _, id := range []string{a, b, ee} {
		expect(t, e.do("POST", "/api/projects/"+id+"/assign", tl, map[string]any{"assignee_id": e.users["member"].ID, "priority": "medium"}), 200, "assign")
	}
	tr(sales, d, "cancelled")
	deliver(a)
	tr(sales, a, "completed")
	past := time.Now().AddDate(0, 0, -3).Format(time.DateOnly)
	for _, id := range []string{b, ee} {
		if _, err := e.store.Pool.Exec(ctx, `UPDATE projects SET due_date = $1 WHERE id = $2`, past, id); err != nil {
			t.Fatal(err)
		}
	}
	deliver(b)
	tr(sales, b, "in_progress") // the client asked for changes
	deliver(b)

	t.Run("who can open it", func(t *testing.T) {
		for label, who := range map[string]string{"sales": sales, "tl": tl, "member": mem, "hr": hr} {
			expect(t, e.do("GET", "/api/analytics", who, nil), 403, label+" without the grant")
		}
		expect(t, e.do("GET", "/api/analytics", ceo, nil), 200, "the CEO always")
		r := e.do("GET", "/api/auth/me", ceo, nil)
		if !hasPerm(r.body, "oversight.view_analytics") {
			t.Fatalf("CEO lacks the permission: %v", r.body)
		}
	})

	t.Run("only the CEO grants it", func(t *testing.T) {
		tl2 := e.users["tl2"].ID.String()
		expect(t, e.do("PATCH", "/api/admin/users/"+tl2, hr, map[string]any{"can_view_analytics": true}), 403, "HR can't grant")
		expect(t, e.do("PATCH", "/api/admin/users/"+e.users["ceo"].ID.String(), ceo, map[string]any{"can_view_analytics": true}), 409, "not to oneself")
		r := e.do("POST", "/api/admin/users", hr, map[string]any{
			"email": "new@test.local", "name": "New", "role": "sales", "password": testPass, "can_view_analytics": true,
		})
		expect(t, r, 403, "HR can't create someone with it")

		expect(t, e.do("PATCH", "/api/admin/users/"+tl2, ceo, map[string]any{"can_view_analytics": true}), 200, "CEO grants a lead")
		expect(t, e.do("PATCH", "/api/admin/users/"+e.users["sales2"].ID.String(), ceo, map[string]any{"can_view_analytics": true}), 200, "and a salesperson")
		lead, rabbi := e.login("tl2"), e.login("sales2")
		expect(t, e.do("GET", "/api/analytics", lead, nil), 200, "granted lead")
		expect(t, e.do("GET", "/api/analytics", rabbi, nil), 200, "granted salesperson")
		if r := e.do("GET", "/api/auth/me", rabbi, nil); !hasPerm(r.body, "oversight.view_analytics") {
			t.Fatalf("granted user lacks the permission: %v", r.body)
		}
		// Granting the page gives nothing else: still no CEO dashboard.
		expect(t, e.do("GET", "/api/reports/summary", rabbi, nil), 403, "summary stays CEO-only")

		var n int
		if err := e.store.Pool.QueryRow(ctx, `SELECT count(*) FROM activity_log WHERE action = 'user.access_changed'
			AND metadata->'changes' ? 'can_view_analytics'`).Scan(&n); err != nil || n != 2 {
			t.Fatalf("access log: %d %v", n, err)
		}

		expect(t, e.do("PATCH", "/api/admin/users/"+e.users["sales2"].ID.String(), ceo, map[string]any{"can_view_analytics": false}), 200, "CEO removes it")
		expect(t, e.do("GET", "/api/analytics", rabbi, nil), 403, "removed at once")
	})

	t.Run("numbers", func(t *testing.T) {
		r := e.do("GET", "/api/analytics", ceo, nil)
		expect(t, r, 200, "analytics")
		tot := r.body["totals"].(map[string]any)
		want := map[string]float64{
			"incoming": 5, "delivered": 2, "completed": 1, "cancelled": 1, "delivered_with_due": 2, "on_time": 1,
			"delivered_with_revisions": 1, "live_now": 2, "new_now": 1, "in_progress_now": 1, "delivered_now": 1, "overdue_now": 1,
		}
		for k, v := range want {
			if tot[k].(float64) != v {
				t.Fatalf("totals[%s] = %v, want %v (%v)", k, tot[k], v, tot)
			}
		}
		if r.body["bucket"] != "day" || len(r.body["series"].([]any)) != 30 {
			t.Fatalf("series: %v %d", r.body["bucket"], len(r.body["series"].([]any)))
		}
		var in, out float64
		for _, s := range r.body["series"].([]any) {
			in += s.(map[string]any)["incoming"].(float64)
			out += s.(map[string]any)["delivered"].(float64)
		}
		if in != 5 || out != 2 {
			t.Fatalf("series sums: in %v out %v", in, out)
		}

		teams := map[string]map[string]any{}
		for _, x := range r.body["teams"].([]any) {
			teams[x.(map[string]any)["name"].(string)] = x.(map[string]any)
		}
		if s := teams["Shopify"]; s["incoming"].(float64) != 5 || s["delivered"].(float64) != 2 || s["on_time"].(float64) != 1 ||
			s["overdue_now"].(float64) != 1 || s["lead_name"] != "tl" {
			t.Fatalf("Shopify: %v", s)
		}
		if w := teams["Web"]; w == nil || w["incoming"].(float64) != 0 {
			t.Fatalf("Web is listed with nothing: %v", w)
		}

		var member map[string]any
		for _, p := range r.body["people"].([]any) {
			if p.(map[string]any)["name"] == "member" {
				member = p.(map[string]any)
			}
		}
		if member == nil || member["delivered"].(float64) != 2 || member["on_time"].(float64) != 1 || member["completed"].(float64) != 1 ||
			member["open_now"].(float64) != 1 || member["overdue_now"].(float64) != 1 || member["team_name"] != "Shopify" {
			t.Fatalf("member: %v", member)
		}
		if s := r.body["sales"].([]any); len(s) != 1 || s[0].(map[string]any)["incoming"].(float64) != 5 || s[0].(map[string]any)["cancelled"].(float64) != 1 {
			t.Fatalf("sales: %v", s)
		}
		if ty := r.body["types"].([]any); len(ty) != 1 || ty[0].(map[string]any)["incoming"].(float64) != 5 {
			t.Fatalf("types: %v", ty)
		}
	})

	t.Run("one team, buckets and validation", func(t *testing.T) {
		r := e.do("GET", "/api/analytics?team_id="+web, ceo, nil)
		expect(t, r, 200, "web only")
		if r.body["totals"].(map[string]any)["incoming"].(float64) != 0 || len(r.body["people"].([]any)) != 1 {
			t.Fatalf("web: %v", r.body)
		}
		// The 1st of the month eleven months back (Oct 31 less 11 months would roll into December).
		now := time.Now()
		from := time.Date(now.Year(), now.Month()-11, 1, 0, 0, 0, 0, time.UTC).Format(time.DateOnly)
		r = e.do("GET", "/api/analytics?from="+from, ceo, nil)
		expect(t, r, 200, "a year")
		if r.body["bucket"] != "month" || len(r.body["series"].([]any)) != 12 {
			t.Fatalf("months: %v %d", r.body["bucket"], len(r.body["series"].([]any)))
		}
		r = e.do("GET", "/api/analytics?bucket=week", ceo, nil)
		expect(t, r, 200, "weeks")
		var in float64
		for _, s := range r.body["series"].([]any) {
			in += s.(map[string]any)["incoming"].(float64)
		}
		if in != 5 {
			t.Fatalf("weekly incoming: %v", in)
		}
		expect(t, e.do("GET", "/api/analytics?bucket=year", ceo, nil), 422, "bad bucket")
		expect(t, e.do("GET", "/api/analytics?bucket=day&from="+from, ceo, nil), 422, "a year of days")
		expect(t, e.do("GET", "/api/analytics?team_id=nope", ceo, nil), 422, "bad team")
	})
}

func TestAnalyticsDetailAndTargets(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	e.addUser("sales2", policy.Sales, false)
	sales, tl, mem, ceo, hr := e.login("sales"), e.login("tl"), e.login("member"), e.login("ceo"), e.login("hr")
	r := e.do("POST", "/api/admin/teams", hr, map[string]any{"name": "Shopify", "lead_id": e.users["tl"].ID})
	expect(t, r, 201, "team")
	shopify := r.body["team"].(map[string]any)["id"].(string)
	expect(t, e.do("PUT", "/api/admin/teams/"+shopify+"/members/"+e.users["member"].ID.String(), hr, nil), 204, "add member")
	r = e.do("POST", "/api/admin/project-types", ceo, map[string]any{"name": "Shopify Store", "team_id": shopify})
	expect(t, r, 201, "type")
	ptype := r.body["project_type"].(map[string]any)["id"].(string)
	profile := testutil.Must(e.store.CreateProfile(ctx, db.CreateProfileParams{Name: "Studio Main"}))
	future := time.Now().AddDate(0, 1, 0).Format(time.DateOnly)
	mk := func(name string, price int) string {
		r := e.do("POST", "/api/projects", sales, map[string]any{
			"name": name, "profile_id": profile.ID, "client_name": "Client " + name, "order_id": "O-" + name,
			"project_type_id": ptype, "scope": "Scope", "agreed_price": price, "deadline": future,
		})
		expect(t, r, 201, "create "+name)
		return projectOf(r)["id"].(string)
	}
	tr := func(cookie, id, to string) {
		t.Helper()
		expect(t, e.do("POST", "/api/projects/"+id+"/transition", cookie, map[string]any{"to": to, "comment": "Because"}), 200, id+" -> "+to)
	}
	// A ($500, on time) and B ($250, late) delivered; C open and in progress.
	a, b, c := mk("Alpha", 500), mk("Beta", 250), mk("Gamma", 100)
	for _, id := range []string{a, b, c} {
		expect(t, e.do("POST", "/api/projects/"+id+"/assign", tl, map[string]any{"assignee_id": e.users["member"].ID, "priority": "medium"}), 200, "assign")
	}
	if _, err := e.store.Pool.Exec(ctx, `UPDATE projects SET due_date = $1 WHERE id = $2`, time.Now().AddDate(0, 0, -2).Format(time.DateOnly), b); err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{a, b} {
		tr(mem, id, "ready_for_review")
		tr(tl, id, "ready_to_deliver")
		tr(sales, id, "delivered")
	}
	// Make A's in-progress step take a known 10 hours: move its history back.
	if _, err := e.store.Pool.Exec(ctx, `UPDATE project_status_history SET created_at = created_at - interval '10 hours'
		WHERE project_id = $1 AND to_status = 'in_progress'`, a); err != nil {
		t.Fatal(err)
	}
	month := time.Now().Format("2006-01")

	t.Run("revenue, previous period, steps", func(t *testing.T) {
		r := e.do("GET", "/api/analytics", ceo, nil)
		expect(t, r, 200, "analytics")
		rev := r.body["revenue"].([]any)
		// 750 agreed, less Fiverr's 20%.
		if len(rev) != 1 || rev[0].(map[string]any)["net"] != "600.00" || rev[0].(map[string]any)["currency"] != "USD" {
			t.Fatalf("revenue: %v", rev)
		}
		if r.body["previous_totals"].(map[string]any)["delivered"].(float64) != 0 || r.body["previous_range"] == nil {
			t.Fatalf("previous: %v", r.body["previous_totals"])
		}
		steps := map[string]map[string]any{}
		for _, x := range r.body["step_times"].([]any) {
			steps[x.(map[string]any)["step"].(string)] = x.(map[string]any)
		}
		if s := steps["in_progress"]; s == nil || s["projects"].(float64) != 2 || s["max_h"].(float64) < 9.9 {
			t.Fatalf("step times: %v", steps)
		}
		if l := r.body["lateness"].(map[string]any); l["late"].(float64) != 1 || l["avg_h"].(float64) <= 0 {
			t.Fatalf("lateness: %v", l)
		}
		stuck := r.body["stuck_now"].([]any)
		if len(stuck) != 2 { // C in progress, A and B awaiting the client
			t.Fatalf("stuck now: %v", stuck)
		}
	})

	t.Run("projects behind a number", func(t *testing.T) {
		names := func(metric, extra string) []string {
			t.Helper()
			r := e.do("GET", "/api/analytics/projects?metric="+metric+extra, ceo, nil)
			expect(t, r, 200, metric)
			out := []string{}
			for _, p := range r.body["projects"].([]any) {
				out = append(out, p.(map[string]any)["name"].(string))
			}
			return out
		}
		if n := names("late", ""); len(n) != 1 || n[0] != "Beta" {
			t.Fatalf("late: %v", n)
		}
		if n := names("on_time", ""); len(n) != 1 || n[0] != "Alpha" {
			t.Fatalf("on time: %v", n)
		}
		if n := names("incoming", "&user_id="+e.users["member"].ID.String()); len(n) != 3 {
			t.Fatalf("member's incoming: %v", n)
		}
		if n := names("member_open", "&team_id="+shopify); len(n) != 1 || n[0] != "Gamma" {
			t.Fatalf("open: %v", n)
		}
		if n := names("delivered", "&sales_id="+e.users["sales2"].ID.String()); len(n) != 0 {
			t.Fatalf("other salesperson: %v", n)
		}
		expect(t, e.do("GET", "/api/analytics/projects?metric=everything", ceo, nil), 422, "bad metric")
		expect(t, e.do("GET", "/api/analytics/projects?metric=late", tl, nil), 403, "needs the grant")
	})

	t.Run("csv", func(t *testing.T) {
		code, body := e.get("/api/analytics/export.csv", ceo)
		if code != 200 || !strings.Contains(body, "Hours late") || strings.Count(body, "\n") != 4 || !strings.Contains(body, "400.00") {
			t.Fatalf("csv %d: %s", code, body)
		}
	})

	t.Run("trend", func(t *testing.T) {
		r := e.do("GET", "/api/analytics/trend?months=3", ceo, nil)
		expect(t, r, 200, "trend")
		months := r.body["months"].([]any)
		if len(months) != 3 || months[2] != month {
			t.Fatalf("months: %v", months)
		}
		team := r.body["teams"].([]any)[0].(map[string]any)["months"].(map[string]any)[month].(map[string]any)
		if team["delivered"].(float64) != 2 || team["revenue"].(map[string]any)["USD"] != "600.00" {
			t.Fatalf("team trend: %v", team)
		}
		var member map[string]any
		for _, p := range r.body["people"].([]any) {
			if p.(map[string]any)["name"] == "member" {
				member = p.(map[string]any)
			}
		}
		if member["months"].(map[string]any)[month].(float64) != 2 {
			t.Fatalf("person trend: %v", member)
		}
		expect(t, e.do("GET", "/api/analytics/trend?months=40", ceo, nil), 422, "too many months")
	})

	t.Run("targets", func(t *testing.T) {
		path := "/api/analytics/targets/" + shopify + "/" + month
		expect(t, e.do("PATCH", "/api/admin/users/"+e.users["sales2"].ID.String(), ceo, map[string]any{"can_view_analytics": true}), 200, "grant")
		rabbi := e.login("sales2")
		expect(t, e.do("PUT", path, rabbi, map[string]any{"amount": "1000"}), 403, "granted user can't set")
		expect(t, e.do("PUT", path, hr, map[string]any{"amount": "1000"}), 403, "HR can't set")
		expect(t, e.do("PUT", path, ceo, map[string]any{"amount": "-5"}), 422, "negative")
		expect(t, e.do("PUT", path, ceo, map[string]any{"amount": "100", "currency": "dollars"}), 422, "bad currency")
		expect(t, e.do("PUT", "/api/analytics/targets/"+shopify+"/2026-13", ceo, map[string]any{"amount": "100"}), 422, "bad month")
		expect(t, e.do("PUT", "/api/analytics/targets/"+uuidZero+"/"+month, ceo, map[string]any{"amount": "100"}), 404, "no team")
		r := e.do("PUT", path, ceo, map[string]any{"amount": "1,000"})
		expect(t, r, 200, "CEO sets")
		if tg := r.body["target"].(map[string]any); tg["amount"] != "1000.00" || tg["currency"] != "USD" {
			t.Fatalf("target: %v", tg)
		}
		expect(t, e.do("PUT", path, ceo, map[string]any{"amount": "1200"}), 200, "CEO changes")

		r = e.do("GET", "/api/analytics/targets", rabbi, nil)
		expect(t, r, 200, "granted user sees")
		row := r.body["teams"].([]any)[0].(map[string]any)
		if row["amount"] != "1200.00" || row["currency"] != "USD" || row["achieved"] != "600.00" || row["projects"].(float64) != 2 || row["set_by_name"] != "ceo" {
			t.Fatalf("targets row: %v", row)
		}
		if el := r.body["elapsed"].(float64); el <= 0 || el >= 1 {
			t.Fatalf("elapsed: %v", el)
		}
		r = e.do("GET", "/api/analytics/trend?months=2", ceo, nil)
		tm := r.body["teams"].([]any)[0].(map[string]any)["months"].(map[string]any)[month].(map[string]any)
		if tm["target"].(map[string]any)["amount"] != "1200.00" {
			t.Fatalf("trend target: %v", tm)
		}

		var n int
		if err := e.store.Pool.QueryRow(ctx, `SELECT count(*) FROM activity_log WHERE action = 'team_target.set'`).Scan(&n); err != nil || n != 2 {
			t.Fatalf("activity: %d %v", n, err)
		}
		expect(t, e.do("DELETE", path, rabbi, nil), 403, "granted user can't clear")
		expect(t, e.do("DELETE", path, ceo, nil), 204, "CEO clears")
		expect(t, e.do("DELETE", path, ceo, nil), 404, "already gone")
		r = e.do("GET", "/api/analytics/targets?month="+month, ceo, nil)
		if row := r.body["teams"].([]any)[0].(map[string]any); row["amount"] != "" {
			t.Fatalf("cleared: %v", row)
		}
		expect(t, e.do("GET", "/api/analytics/targets?month=26-1", ceo, nil), 422, "bad month")
	})
}

const uuidZero = "00000000-0000-0000-0000-000000000000"

func hasPerm(body map[string]any, p string) bool {
	u, _ := body["user"].(map[string]any)
	perms, _ := u["permissions"].([]any)
	for _, x := range perms {
		if x == p {
			return true
		}
	}
	return false
}
