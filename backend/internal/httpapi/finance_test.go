package httpapi

import (
	"context"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/solvinx/opsflow/internal/db"
	"github.com/solvinx/opsflow/internal/policy"
	"github.com/solvinx/opsflow/internal/testutil"
)

func TestFinance(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	admin, tl, member, sales, ceo, hr := e.login("admin"), e.login("tl"), e.login("member"), e.login("sales"), e.login("ceo"), e.login("hr")
	fields := func(r resp) map[string]any {
		t.Helper()
		f, _ := r.body["error"].(map[string]any)["fields"].(map[string]any)
		return f
	}
	exec := func(sql string, args ...any) {
		t.Helper()
		if _, err := e.store.Pool.Exec(ctx, sql, args...); err != nil {
			t.Fatal(err)
		}
	}

	cats := e.do("GET", "/api/finance/categories", hr, nil)
	expect(t, cats, 200, "hr lists categories")
	catID := map[string]string{}
	for _, raw := range cats.body["categories"].([]any) {
		c := raw.(map[string]any)
		catID[c["name"].(string)] = c["id"].(string)
	}
	if catID["Office rent"] == "" || catID["Direct client payment"] == "" {
		t.Fatalf("seeded categories missing: %v", catID)
	}
	for name := range catID {
		if strings.Contains(strings.ToLower(name), "salar") {
			t.Fatalf("a %q category would double-count payroll", name)
		}
	}

	office := e.login("office_admin")

	t.Run("only HR and the CEO", func(t *testing.T) {
		entry := map[string]any{"category_id": catID["Other income"], "amount": "100", "entry_date": "2026-01-02", "description": "x"}
		probes := []struct{ label, cookie string }{{"admin flag", admin}, {"tl", tl}, {"member", member}, {"sales", sales}, {"office admin", office}}
		for _, who := range probes {
			for _, p := range []string{"/api/finance/summary", "/api/finance/entries", "/api/finance/entries/export.csv", "/api/finance/categories", "/api/finance/rates"} {
				if r := e.do("GET", p, who.cookie, nil); r.status != 403 {
					t.Errorf("%s reads %s: %d", who.label, p, r.status)
				}
			}
			if r := e.do("POST", "/api/finance/entries", who.cookie, entry); r.status != 403 {
				t.Errorf("%s adds an entry: %d", who.label, r.status)
			}
			if r := e.do("POST", "/api/finance/rates", who.cookie, map[string]any{"currency": "USD", "rate": "1", "effective_from": "2026-01-01"}); r.status != 403 {
				t.Errorf("%s adds a rate: %d", who.label, r.status)
			}
		}
		// Invoices are the one part of finance an Office Admin reaches, and only their own.
		for _, who := range probes[:4] {
			if r := e.do("GET", "/api/finance/invoices", who.cookie, nil); r.status != 403 {
				t.Errorf("%s lists invoices: %d", who.label, r.status)
			}
			if r := e.do("GET", "/api/finance/rates/on?currency=USD&date=2026-01-02", who.cookie, nil); r.status != 403 {
				t.Errorf("%s reads a rate: %d", who.label, r.status)
			}
		}
		expect(t, e.do("GET", "/api/finance/invoices", office, nil), 200, "office admin lists invoices")
		expect(t, e.do("GET", "/api/finance/rates/on?currency=USD&date=2026-01-02", office, nil), 200, "office admin reads a rate")
	})

	// Rates: USD 120 from January, 125 from 20 February.
	expect(t, e.do("POST", "/api/finance/rates", hr, map[string]any{"currency": "usd", "rate": "120", "effective_from": "2026-01-01"}), 201, "hr adds a rate")
	expect(t, e.do("POST", "/api/finance/rates", ceo, map[string]any{"currency": "USD", "rate": "125", "effective_from": "2026-02-20"}), 201, "ceo adds a rate")
	expect(t, e.do("POST", "/api/finance/rates", hr, map[string]any{"currency": "USD", "rate": "121", "effective_from": "2026-01-01"}), 422, "duplicate rate date")
	expect(t, e.do("POST", "/api/finance/rates", hr, map[string]any{"currency": "BDT", "rate": "1", "effective_from": "2026-01-01"}), 422, "BDT rate")
	if r := e.do("GET", "/api/finance/rates/on?currency=USD&date=2026-02-19", hr, nil); r.body["rate"] != "120.0000" {
		t.Fatalf("rate on 19 Feb = %v", r.body)
	}

	t.Run("entries: income only, validation, edit, delete, CEO writes", func(t *testing.T) {
		income := catID["Other income"]
		bad := e.do("POST", "/api/finance/entries", hr, map[string]any{"category_id": income, "amount": "-5", "entry_date": "2026-13-01", "description": ""})
		expect(t, bad, 422, "invalid entry")
		for _, f := range []string{"amount", "entry_date", "description"} {
			if _, ok := fields(bad)[f]; !ok {
				t.Errorf("no error for %s: %v", f, bad.body)
			}
		}
		// Expenses are not entered here at all: they are raised as invoices the CEO approves.
		noExpense := e.do("POST", "/api/finance/entries", hr, map[string]any{"category_id": catID["Office rent"], "amount": "10", "entry_date": "2026-01-03", "description": "Rent by hand"})
		expect(t, noExpense, 422, "expense entered as a ledger line")
		if msg := fields(noExpense)["category_id"]; !strings.Contains(msg.(string), "invoice") {
			t.Errorf("expense refusal doesn't point at invoices: %v", noExpense.body)
		}
		noRate := e.do("POST", "/api/finance/entries", hr, map[string]any{"category_id": income, "amount": "10", "currency": "USD", "entry_date": "2025-12-31", "description": "Before any rate"})
		expect(t, noRate, 422, "USD with no rate for the date")
		if _, ok := fields(noRate)["rate_to_bdt"]; !ok {
			t.Errorf("missing rate not reported: %v", noRate.body)
		}
		bdt := e.do("POST", "/api/finance/entries", hr, map[string]any{"category_id": income, "amount": "10", "currency": "BDT", "rate_to_bdt": "99", "entry_date": "2026-01-03", "description": "BDT is always 1"})
		expect(t, bdt, 201, "BDT entry ignores a given rate")
		id := bdt.body["id"].(string)
		expect(t, e.do("PATCH", "/api/finance/entries/"+id, hr, map[string]any{"category_id": catID["Office rent"], "amount": "10", "entry_date": "2026-01-03", "description": "Turned into an expense"}), 422, "income edited into an expense")
		expect(t, e.do("PATCH", "/api/finance/entries/"+id, ceo, map[string]any{"category_id": income, "amount": "1,234.50", "entry_date": "2026-01-03", "description": "Edited by the CEO"}), 204, "ceo edits")
		list := e.do("GET", "/api/finance/entries?from=2026-01-03&to=2026-01-03", hr, nil).body["entries"].([]any)
		row := list[0].(map[string]any)
		if len(list) != 1 || row["amount_bdt"] != "1234.50" || row["rate_to_bdt"] != "1.0000" || row["updated_by_name"] != "ceo" || row["created_by_name"] != "hr" || row["invoice_number"].(float64) != 0 {
			t.Fatalf("edited entry = %v", list)
		}
		expect(t, e.do("PATCH", "/api/finance/entries/"+id, tl, map[string]any{"description": "x"}), 403, "tl edits")
		expect(t, e.do("DELETE", "/api/finance/entries/"+id, ceo, nil), 204, "ceo deletes")
		expect(t, e.do("DELETE", "/api/finance/entries/"+id, hr, nil), 404, "deleted twice")
	})

	t.Run("categories: add, rename, deactivate", func(t *testing.T) {
		expect(t, e.do("POST", "/api/finance/categories", hr, map[string]any{"name": "Office rent", "kind": "expense"}), 422, "duplicate name")
		expect(t, e.do("POST", "/api/finance/categories", ceo, map[string]any{"name": "Courier", "kind": "expense"}), 201, "ceo adds")
		var id string
		for _, raw := range e.do("GET", "/api/finance/categories", hr, nil).body["categories"].([]any) {
			if c := raw.(map[string]any); c["name"] == "Courier" {
				id = c["id"].(string)
			}
		}
		expect(t, e.do("PATCH", "/api/finance/categories/"+id, hr, map[string]any{"name": "Courier & post", "is_active": false}), 204, "rename and deactivate")
		r := e.do("POST", "/api/finance/invoices", hr, map[string]any{
			"description": "Parcel", "expense_date": "2026-01-03",
			"items": []map[string]any{{"category_id": id, "description": "Parcel", "quantity": "1", "unit_price": "10"}},
		})
		expect(t, r, 422, "invoice line in an inactive category")
	})

	// Ledger: January rent 15,000 BDT and a 200 USD client payment received at 118;
	// February 50 USD of tools with no rate given, so 21 Feb takes 125. The two expenses are
	// raised as invoices by the CEO, which approves and posts them in one step.
	expect(t, e.do("POST", "/api/finance/entries", hr, map[string]any{
		"category_id": catID["Direct client payment"], "amount": "200", "currency": "USD", "rate_to_bdt": "118",
		"entry_date": "2026-01-20", "description": "Direct client",
	}), 201, "add Direct client")
	for _, in := range []map[string]any{
		{"description": "January rent", "expense_date": "2026-01-05", "items": []map[string]any{
			{"category_id": catID["Office rent"], "description": "January rent", "quantity": "1", "unit_price": "15000"},
		}},
		{"description": "Hosting", "currency": "USD", "expense_date": "2026-02-21", "items": []map[string]any{
			{"category_id": catID["Software & tools"], "description": "Hosting", "quantity": "1", "unit_price": "50"},
		}},
	} {
		r := e.do("POST", "/api/finance/invoices", ceo, in)
		expect(t, r, 201, "invoice "+in["description"].(string))
		if r.body["status"] != "approved" || r.body["ledger_lines"].(float64) != 1 {
			t.Fatalf("the CEO's own invoice wasn't posted: %v", r.body)
		}
	}

	// A 1,000 USD project completed on 15 February: 800 net after the 20% fee, at 125.
	profile := testutil.Must(e.store.CreateProfile(ctx, db.CreateProfileParams{Name: "Main"}))
	ptype := testutil.Must(e.store.CreateProjectType(ctx, db.CreateProjectTypeParams{Name: "Store"}))
	r := e.do("POST", "/api/projects", sales, map[string]any{
		"name": "Shop", "profile_id": profile.ID, "client_name": "Client", "order_id": "F-1", "project_type_id": ptype.ID,
		"scope": "Scope", "agreed_price": 1000, "deadline": time.Now().AddDate(0, 1, 0).Format(time.DateOnly),
	})
	expect(t, r, 201, "create project")
	pid := projectOf(r)["id"].(string)
	expect(t, e.do("POST", "/api/projects/"+pid+"/assign", tl, map[string]any{"assignee_id": e.users["member"].ID, "priority": "medium"}), 200, "assign")
	for _, step := range []struct{ cookie, to string }{{member, "ready_for_review"}, {tl, "ready_to_deliver"}, {sales, "delivered"}, {sales, "completed"}} {
		expect(t, e.do("POST", "/api/projects/"+pid+"/transition", step.cookie, map[string]any{"to": step.to}), 200, step.to)
	}
	exec(`UPDATE projects SET completed_at = '2026-02-15 12:00:00+06' WHERE id = $1`, pid)

	// Fixed salaries: member 30,000 BDT from January; sales 500 USD from February; a leaver
	// at 20,000 BDT who left on 10 February; a joiner on 5 March paid 100 EUR (no EUR rate).
	// Only pay recorded on the payroll sheet counts in the analytics.
	leaver := e.addUser("leaver", policy.Member, false)
	joiner := e.addUser("joiner", policy.Member, false)
	for _, s := range []struct {
		id         string
		amount, cu string
		from       string
	}{
		{e.users["member"].ID.String(), "30000", "BDT", "2026-01-01"},
		{e.users["sales"].ID.String(), "500", "USD", "2026-02-01"},
		{leaver.ID.String(), "20000", "BDT", "2026-01-01"},
		{joiner.ID.String(), "100", "EUR", "2026-01-01"},
	} {
		expect(t, e.do("POST", "/api/hr/employees/"+s.id+"/salary", hr, map[string]any{"amount": s.amount, "currency": s.cu, "effective_from": s.from}), 201, "salary")
	}
	exec(`UPDATE employees SET join_date = '2026-03-05' WHERE user_id = $1`, joiner.ID)
	exec(`UPDATE users SET is_active = false WHERE id = $1`, leaver.ID)
	var departures int
	if err := e.store.Pool.QueryRow(ctx, `SELECT count(*) FROM user_departures WHERE user_id = $1`, leaver.ID).Scan(&departures); err != nil || departures != 1 {
		t.Fatalf("deactivation not recorded: %d %v", departures, err)
	}
	exec(`UPDATE user_departures SET left_at = '2026-02-10 18:00:00+06' WHERE user_id = $1`, leaver.ID)
	// Two days of approved unpaid leave for the member (Monday and Tuesday 2–3 February).
	exec(`INSERT INTO leave_requests (user_id, leave_type_id, start_date, end_date, days, status)
	      SELECT $1, id, '2026-02-02', '2026-02-03', 2, 'approved' FROM leave_types WHERE name = 'Unpaid'`, e.users["member"].ID)

	memberID, salesID := e.users["member"].ID.String(), e.users["sales"].ID.String()
	pay := func(cookie, userID, month string, body map[string]any) resp {
		return e.do("PUT", "/api/finance/payroll/"+userID+"/"+month, cookie, body)
	}
	sheet := func(month string) map[string]map[string]any {
		t.Helper()
		r := e.do("GET", "/api/finance/payroll?month="+month, hr, nil)
		expect(t, r, 200, "payroll sheet")
		out := map[string]map[string]any{}
		for _, raw := range r.body["rows"].([]any) {
			row := raw.(map[string]any)
			out[row["name"].(string)] = row
		}
		return out
	}

	t.Run("payroll: sheet, record, adjust, delete", func(t *testing.T) {
		for _, who := range []struct{ label, cookie string }{{"admin flag", admin}, {"tl", tl}, {"member", member}, {"sales", sales}} {
			if r := e.do("GET", "/api/finance/payroll", who.cookie, nil); r.status != 403 {
				t.Errorf("%s reads payroll: %d", who.label, r.status)
			}
			if r := pay(who.cookie, memberID, "2026-01", map[string]any{"base_amount": "1", "paid_on": "2026-01-31"}); r.status != 403 {
				t.Errorf("%s records pay: %d", who.label, r.status)
			}
		}
		expect(t, pay(hr, e.users["hr"].ID.String(), "2026-01", map[string]any{"base_amount": "1", "paid_on": "2026-01-31"}), 404, "own pay")

		// February: the member, sales and the leaver (left in February); the joiner starts in March.
		feb := sheet("2026-02")
		if len(feb) != 3 || feb["member"] == nil || feb["sales"] == nil || feb["leaver"] == nil {
			t.Fatalf("February sheet = %v", feb)
		}
		if s := feb["sales"]; s["salary_amount"] != "500.00" || s["salary_currency"] != "USD" || s["payment"] != nil {
			t.Errorf("sales row = %v", s)
		}
		if a, _ := feb["member"]["attendance"].(map[string]any); a == nil || a["unpaid_leave_days"] != 2.0 || a["working_days"].(float64) < 18 {
			t.Errorf("member attendance = %v", feb["member"]["attendance"])
		}
		if feb["leaver"]["attendance"] != nil {
			t.Errorf("leaver attendance = %v", feb["leaver"]["attendance"])
		}

		bad := pay(hr, memberID, "2026-01", map[string]any{"base_amount": "30000", "paid_on": "2026-01-31", "adjustments": []map[string]any{
			{"kind": "bonus", "reason": " ", "amount": "10"}, {"kind": "fine", "reason": "x", "amount": "1"}, {"kind": "deduction", "reason": "x", "amount": "0"},
		}})
		expect(t, bad, 422, "invalid lines")
		for _, f := range []string{"adjustments.0", "adjustments.1", "adjustments.2"} {
			if _, ok := fields(bad)[f]; !ok {
				t.Errorf("no error for %s: %v", f, bad.body)
			}
		}
		expect(t, pay(hr, memberID, "2026-01", map[string]any{"base_amount": "30000", "paid_on": "2026-01-31", "adjustments": []map[string]any{
			{"kind": "deduction", "reason": "Advance", "amount": "30000.01"},
		}}), 422, "deductions above the salary")
		expect(t, pay(hr, memberID, "2026-13", map[string]any{"base_amount": "1", "paid_on": "2026-01-31"}), 422, "bad month")

		// January: member 30,000 − 2,000 unpaid absence + 5,000 bonus = 33,000 (saved twice, so
		// the second save replaces the lines); the leaver 20,000.
		expect(t, pay(hr, memberID, "2026-01", map[string]any{"base_amount": "30000", "paid_on": "2026-01-31", "adjustments": []map[string]any{
			{"kind": "bonus", "reason": "Old line", "amount": "1"},
		}}), 204, "record")
		expect(t, pay(ceo, memberID, "2026-01", map[string]any{"base_amount": "30,000", "paid_on": "2026-02-01", "note": "Paid by bank", "adjustments": []map[string]any{
			{"kind": "deduction", "reason": "Unpaid absence (2 days)", "amount": "2000"},
			{"kind": "bonus", "reason": "Eid bonus", "amount": "5000"},
		}}), 204, "ceo edits")
		expect(t, pay(hr, leaver.ID.String(), "2026-01", map[string]any{"base_amount": "20000", "paid_on": "2026-01-31"}), 204, "leaver")
		p := sheet("2026-01")["member"]["payment"].(map[string]any)
		adj := p["adjustments"].([]any)
		if p["net_amount"] != "33000.00" || p["net_bdt"] != "33000.00" || p["updated_by_name"] != "ceo" || len(adj) != 2 ||
			adj[0].(map[string]any)["reason"] != "Unpaid absence (2 days)" || adj[1].(map[string]any)["kind"] != "bonus" {
			t.Errorf("January member payment = %v", p)
		}

		// February: sales 500 USD, the rate for 28 February (125) filled in.
		expect(t, pay(hr, salesID, "2026-02", map[string]any{"base_amount": "500", "currency": "USD", "paid_on": "2026-02-28"}), 204, "USD pay")
		if p := sheet("2026-02")["sales"]["payment"].(map[string]any); p["rate_to_bdt"] != "125.0000" || p["net_bdt"] != "62500.00" {
			t.Errorf("February sales payment = %v", p)
		}

		// March: the joiner in EUR, which has no rate until one is given.
		noRate := pay(ceo, joiner.ID.String(), "2026-03", map[string]any{"base_amount": "100", "currency": "EUR", "paid_on": "2026-03-31"})
		expect(t, noRate, 422, "EUR with no rate")
		if _, ok := fields(noRate)["rate_to_bdt"]; !ok {
			t.Errorf("missing rate not reported: %v", noRate.body)
		}
		expect(t, pay(ceo, joiner.ID.String(), "2026-03", map[string]any{"base_amount": "100", "currency": "EUR", "rate_to_bdt": "130", "paid_on": "2026-03-31"}), 204, "EUR pay")

		// A mistaken payment is deleted.
		expect(t, pay(hr, memberID, "2026-03", map[string]any{"base_amount": "1", "paid_on": "2026-03-31"}), 204, "mistake")
		id := sheet("2026-03")["member"]["payment"].(map[string]any)["id"].(string)
		expect(t, e.do("DELETE", "/api/finance/payroll/"+id, tl, nil), 403, "tl deletes")
		expect(t, e.do("DELETE", "/api/finance/payroll/"+id, hr, nil), 204, "delete")
		expect(t, e.do("DELETE", "/api/finance/payroll/"+id, hr, nil), 404, "deleted twice")
		if sheet("2026-03")["member"]["payment"] != nil {
			t.Error("deleted payment still on the sheet")
		}
	})

	t.Run("monthly analytics", func(t *testing.T) {
		r := e.do("GET", "/api/finance/summary?from=2026-01&to=2026-03", ceo, nil)
		expect(t, r, 200, "summary")
		months := r.body["months"].([]any)
		if len(months) != 3 {
			t.Fatalf("months = %v", months)
		}
		num := func(m map[string]any, k string) float64 {
			f, err := strconv.ParseFloat(m[k].(string), 64)
			if err != nil {
				t.Fatalf("%s = %v", k, m[k])
			}
			return f
		}
		want := []struct {
			month                                      string
			income, expense, earnings, payroll, profit float64
			people                                     float64
		}{
			{"2026-01", 23600, 15000, 0, 33000 + 20000, 23600 - 15000 - 53000, 2},
			{"2026-02", 0, 6250, 100000, 62500, 100000 - 6250 - 62500, 1},
			{"2026-03", 0, 0, 0, 13000, -13000, 1},
		}
		for i, w := range want {
			m := months[i].(map[string]any)
			got := []float64{num(m, "ledger_income"), num(m, "ledger_expense"), num(m, "project_earnings"), num(m, "payroll"), num(m, "profit")}
			exp := []float64{w.income, w.expense, w.earnings, w.payroll, w.profit}
			if m["month"] != w.month || got[0] != exp[0] || got[1] != exp[1] || got[2] != exp[2] || got[3] != exp[3] || got[4] != exp[4] || m["paid_people"].(float64) != w.people {
				t.Errorf("%s: got %v people %v, want %v people %v", w.month, got, m["paid_people"], exp, w.people)
			}
		}
		if m := months[1].(map[string]any); m["projects"].(float64) != 1 {
			t.Errorf("February projects = %v", m["projects"])
		}
		// Totals only: no project names, clients or order IDs reach HR.
		status, s := e.get("/api/finance/summary?from=2026-01&to=2026-03", hr)
		if status != 200 {
			t.Fatalf("hr summary: %d", status)
		}
		if strings.Contains(s, "Shop") || strings.Contains(s, "F-1") || strings.Contains(s, "Client\"") {
			t.Errorf("summary leaks project details: %s", s)
		}
		cats := r.body["categories"].([]any)
		if len(cats) != 3 {
			t.Errorf("category totals = %v", cats)
		}
		expect(t, e.do("GET", "/api/finance/summary?from=2026-04&to=2026-01", hr, nil), 422, "reversed range")
		expect(t, e.do("GET", "/api/finance/summary?from=2020-01&to=2026-01", hr, nil), 422, "range too long")
	})

	t.Run("restoring someone clears their departure", func(t *testing.T) {
		exec(`UPDATE users SET is_active = true WHERE id = $1`, leaver.ID)
		var n int
		if err := e.store.Pool.QueryRow(ctx, `SELECT count(*) FROM user_departures WHERE user_id = $1`, leaver.ID).Scan(&n); err != nil || n != 0 {
			t.Fatalf("departure kept after restore: %d %v", n, err)
		}
	})

	t.Run("CSV and activity", func(t *testing.T) {
		status, body := e.get("/api/finance/entries/export.csv?from=2026-01-01&to=2026-02-28", hr)
		if status != 200 || !strings.Contains(body, "January rent") || !strings.Contains(body, "23600.00") || strings.Count(body, "\n") != 4 {
			t.Fatalf("csv %d:\n%s", status, body)
		}
		// Team Leaders read Activity, so finance amounts must never be in it.
		rows, err := e.store.Pool.Query(ctx, `SELECT metadata FROM activity_log WHERE entity_type LIKE 'finance_%' OR entity_type = 'invoice'`)
		if err != nil {
			t.Fatal(err)
		}
		defer rows.Close()
		n := 0
		for rows.Next() {
			var meta map[string]any
			if err := rows.Scan(&meta); err != nil {
				t.Fatal(err)
			}
			n++
			for k := range meta {
				if strings.Contains(k, "amount") || strings.Contains(k, "rate") {
					t.Errorf("activity metadata carries %q: %v", k, meta)
				}
			}
		}
		if n == 0 {
			t.Fatal("no finance activity logged")
		}
	})
}
