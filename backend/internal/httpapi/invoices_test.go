package httpapi

import (
	"context"
	"strings"
	"testing"
)

// TestInvoices walks the expense invoice flow: an Office Admin raises one purchase with
// several lines, HR approves it, the CEO approves it and it lands in the ledger under each
// line's own category — plus every refusal along the way.
func TestInvoices(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	office, hr, ceo, tl := e.login("office_admin"), e.login("hr"), e.login("ceo"), e.login("tl")

	fields := func(r resp) map[string]any {
		t.Helper()
		f, _ := r.body["error"].(map[string]any)["fields"].(map[string]any)
		return f
	}
	cat := map[string]string{}
	for _, raw := range e.do("GET", "/api/finance/categories", hr, nil).body["categories"].([]any) {
		c := raw.(map[string]any)
		cat[c["name"].(string)] = c["id"].(string)
	}
	item := func(category, what, qty, price string) map[string]any {
		return map[string]any{"category_id": cat[category], "description": what, "quantity": qty, "unit_price": price}
	}
	// One purchase, three lines, two categories: Tk 2,000 + 4 × 1,000 + 500 = Tk 6,500.
	office_run := map[string]any{
		"description": "Meeting room kit", "expense_date": "2026-03-02",
		"items": []map[string]any{
			item("Equipment", "Ceiling fan", "1", "2000"),
			item("Equipment", "Office chair", "4", "1,000"),
			item("Food & refreshments", "Tea and sugar", "1", "500"),
		},
	}

	// The Office Admin is offered the expense categories with the list, and nothing else.
	list := e.do("GET", "/api/finance/invoices", office, nil)
	expect(t, list, 200, "office admin lists invoices")
	if list.body["can_review"] != false {
		t.Errorf("office admin can_review = %v", list.body["can_review"])
	}
	for _, raw := range list.body["categories"].([]any) {
		if c := raw.(map[string]any); c["kind"] != "expense" || c["is_active"] != true {
			t.Errorf("offered category %v", c)
		}
	}

	t.Run("validation", func(t *testing.T) {
		bad := e.do("POST", "/api/finance/invoices", office, map[string]any{
			"description": "", "expense_date": "2026-13-40", "items": []map[string]any{},
		})
		expect(t, bad, 422, "invalid invoice")
		for _, f := range []string{"description", "expense_date", "items"} {
			if _, ok := fields(bad)[f]; !ok {
				t.Errorf("no error for %s: %v", f, bad.body)
			}
		}
		lines := e.do("POST", "/api/finance/invoices", office, map[string]any{
			"description": "Mixed bag", "expense_date": "2026-03-02",
			"items": []map[string]any{
				{"category_id": cat["Equipment"], "description": " ", "quantity": "1", "unit_price": "10"},
				{"category_id": cat["Other income"], "description": "Not an expense", "quantity": "1", "unit_price": "10"},
				item("Equipment", "Free chair", "1", "0"),
				item("Equipment", "Half a fan", "0", "10"),
			},
		})
		expect(t, lines, 422, "invalid lines")
		for _, f := range []string{"items.0", "items.1", "items.2", "items.3"} {
			if _, ok := fields(lines)[f]; !ok {
				t.Errorf("no error for %s: %v", f, lines.body)
			}
		}
		many := make([]map[string]any, 51)
		for i := range many {
			many[i] = item("Equipment", "Pen", "1", "10")
		}
		expect(t, e.do("POST", "/api/finance/invoices", office, map[string]any{
			"description": "A lot of pens", "expense_date": "2026-03-02", "items": many,
		}), 422, "too many lines")
	})

	// The Office Admin raises it: it waits for HR, and HR is told.
	raised := e.do("POST", "/api/finance/invoices", office, office_run)
	expect(t, raised, 201, "office admin raises an invoice")
	id := raised.body["id"].(string)
	if raised.body["status"] != "pending" || raised.body["amount_bdt"] != "6500.00" || raised.body["items"].(float64) != 3 ||
		raised.body["number"].(float64) != 1 || raised.body["requested_by_name"] != "office_admin" ||
		raised.body["ledger_lines"].(float64) != 0 {
		t.Fatalf("raised invoice = %v", raised.body)
	}
	t.Run("the lines come back in order, each worked out", func(t *testing.T) {
		items := raised.body["items_list"].([]any)
		if len(items) != 3 {
			t.Fatalf("lines = %v", items)
		}
		chair := items[1].(map[string]any)
		if chair["description"] != "Office chair" || chair["quantity"] != "4.000" || chair["unit_price"] != "1000.00" ||
			chair["amount"] != "4000.00" || chair["category_name"] != "Equipment" {
			t.Errorf("chair line = %v", chair)
		}
		if items[2].(map[string]any)["category_name"] != "Food & refreshments" {
			t.Errorf("tea line = %v", items[2])
		}
	})
	if n := ofType(e.notes(hr), "invoice.raised"); len(n) != 1 || !strings.Contains(n[0].Title, "INV-0001") || !n[0].Important {
		t.Errorf("HR wasn't told: %v", e.notes(hr))
	}
	if len(ofType(e.notes(ceo), "invoice.raised")) != 0 {
		t.Error("the CEO was told before HR had decided")
	}

	t.Run("who sees it and who may decide", func(t *testing.T) {
		for _, who := range []struct{ label, cookie string }{{"tl", tl}, {"office admin", office}} {
			if r := e.do("POST", "/api/finance/invoices/"+id+"/approve", who.cookie, nil); r.status != 403 {
				t.Errorf("%s approves: %d", who.label, r.status)
			}
		}
		if r := e.do("GET", "/api/finance/invoices/"+id, tl, nil); r.status != 403 {
			t.Errorf("tl opens an invoice: %d", r.status)
		}
		// HR raises one of their own; the Office Admin must not see it.
		own := e.do("POST", "/api/finance/invoices", hr, map[string]any{
			"description": "March electricity", "expense_date": "2026-03-03",
			"items": []map[string]any{item("Utilities", "Electricity bill", "1", "3200")},
		})
		expect(t, own, 201, "hr raises an invoice")
		if own.body["status"] != "awaiting_ceo" || own.body["hr_reviewed_by_name"] != "hr" {
			t.Fatalf("HR's own invoice = %v", own.body)
		}
		hrID := own.body["id"].(string)
		expect(t, e.do("GET", "/api/finance/invoices/"+hrID, office, nil), 404, "office admin opens HR's invoice")
		if r := e.do("GET", "/api/finance/invoices", office, nil); len(r.body["invoices"].([]any)) != 1 {
			t.Errorf("office admin sees %v", r.body["invoices"])
		}
		if r := e.do("GET", "/api/finance/invoices", hr, nil); len(r.body["invoices"].([]any)) != 2 {
			t.Errorf("hr sees %v", r.body["invoices"])
		}
		// Nobody decides on their own, even at a step they would otherwise hold.
		expect(t, e.do("POST", "/api/finance/invoices/"+hrID+"/approve", hr, nil), 403, "hr approves their own")
		expect(t, e.do("POST", "/api/finance/invoices/"+hrID+"/approve", ceo, nil), 200, "ceo approves HR's")
	})

	t.Run("editing while it waits replaces the lines", func(t *testing.T) {
		fixed := map[string]any{
			"description": "Meeting room kit", "expense_date": "2026-03-02",
			"items": []map[string]any{
				item("Equipment", "Ceiling fan", "1", "2500"),
				item("Equipment", "Office chair", "4", "1000"),
				item("Food & refreshments", "Tea and sugar", "1", "500"),
			},
		}
		out := e.do("PATCH", "/api/finance/invoices/"+id, office, fixed)
		expect(t, out, 200, "the raiser corrects a line")
		if out.body["amount_bdt"] != "7000.00" || out.body["items"].(float64) != 3 {
			t.Fatalf("corrected invoice = %v", out.body)
		}
		// Back to the original price, and down to two lines.
		expect(t, e.do("PATCH", "/api/finance/invoices/"+id, office, map[string]any{
			"description": "Meeting room kit", "expense_date": "2026-03-02",
			"items": []map[string]any{
				item("Equipment", "Ceiling fan", "1", "2000"),
				item("Food & refreshments", "Tea and sugar", "1", "500"),
			},
		}), 200, "a line is dropped")
		if r := e.do("GET", "/api/finance/invoices/"+id, hr, nil); r.body["amount_bdt"] != "2500.00" || len(r.body["items_list"].([]any)) != 2 {
			t.Fatalf("after dropping a line = %v", r.body)
		}
	})

	t.Run("HR approves, then the CEO, and each category reaches the ledger", func(t *testing.T) {
		expect(t, e.do("POST", "/api/finance/invoices/"+id+"/reject", hr, map[string]any{"note": "  "}), 422, "rejecting without a reason")

		step1 := e.do("POST", "/api/finance/invoices/"+id+"/approve", hr, map[string]any{"note": "We do need one"})
		expect(t, step1, 200, "hr approves")
		if step1.body["status"] != "awaiting_ceo" || step1.body["hr_reviewed_by_name"] != "hr" || step1.body["ledger_lines"].(float64) != 0 {
			t.Fatalf("after HR = %v", step1.body)
		}
		// Nothing is in the ledger until the CEO says so.
		if rows := e.do("GET", "/api/finance/entries?from=2026-03-01&to=2026-03-31", hr, nil).body["entries"].([]any); len(rows) != 1 {
			t.Fatalf("ledger after HR's step = %v", rows)
		}
		if n := ofType(e.notes(office), "invoice.approved"); len(n) != 1 || !strings.Contains(n[0].Title, "waiting for the CEO") {
			t.Errorf("the raiser wasn't told: %v", e.notes(office))
		}
		// The Office Admin is out of it now; HR can still correct it for the CEO.
		expect(t, e.do("PATCH", "/api/finance/invoices/"+id, office, map[string]any{
			"description": "Sneaky", "expense_date": "2026-03-02",
			"items": []map[string]any{item("Equipment", "Gold fan", "1", "90000")},
		}), 409, "the raiser edits it after HR let it through")
		expect(t, e.do("POST", "/api/finance/invoices/"+id+"/cancel", office, nil), 200, "withdrawn after HR's step")

		// Withdrawn by mistake: raise it again, three lines and all, and run it through.
		again := e.do("POST", "/api/finance/invoices", office, office_run)
		expect(t, again, 201, "raised again")
		id2 := again.body["id"].(string)
		expect(t, e.do("POST", "/api/finance/invoices/"+id2+"/approve", hr, nil), 200, "hr approves again")

		done := e.do("POST", "/api/finance/invoices/"+id2+"/approve", ceo, map[string]any{"note": "Approved"})
		expect(t, done, 200, "ceo approves")
		if done.body["status"] != "approved" || done.body["ceo_reviewed_by_name"] != "ceo" || done.body["ledger_lines"].(float64) != 2 {
			t.Fatalf("after the CEO = %v", done.body)
		}
		// Two ledger lines: Tk 6,000 of Equipment and Tk 500 of refreshments, named by line.
		posted := map[string]map[string]any{}
		for _, raw := range e.do("GET", "/api/finance/entries?from=2026-03-01&to=2026-03-31&kind=expense", hr, nil).body["entries"].([]any) {
			r := raw.(map[string]any)
			if r["invoice_number"].(float64) == 3 {
				posted[r["category_name"].(string)] = r
			}
		}
		if len(posted) != 2 {
			t.Fatalf("posted lines = %v", posted)
		}
		kit, tea := posted["Equipment"], posted["Food & refreshments"]
		if kit["amount_bdt"] != "6000.00" || kit["description"] != "Ceiling fan, Office chair" ||
			kit["entry_date"] != "2026-03-02" || kit["created_by_name"] != "office_admin" || kit["updated_by_name"] != "ceo" {
			t.Errorf("equipment line = %v", kit)
		}
		if tea["amount_bdt"] != "500.00" || tea["description"] != "Tea and sugar" {
			t.Errorf("refreshments line = %v", tea)
		}

		// Approving it means something: HR cannot edit or delete the lines afterwards.
		entryID := kit["id"].(string)
		expect(t, e.do("PATCH", "/api/finance/entries/"+entryID, hr, map[string]any{
			"category_id": cat["Equipment"], "amount": "1", "entry_date": "2026-03-02", "description": "Quietly changed",
		}), 409, "hr edits an approved expense")
		expect(t, e.do("DELETE", "/api/finance/entries/"+entryID, hr, nil), 409, "hr deletes an approved expense")
		expect(t, e.do("DELETE", "/api/finance/entries/"+entryID, ceo, nil), 204, "the ceo corrects it")
		// The invoice stays as the record of what was approved, with one line left.
		if r := e.do("GET", "/api/finance/invoices/"+id2, ceo, nil); r.body["status"] != "approved" ||
			r.body["ledger_lines"].(float64) != 1 || len(r.body["items_list"].([]any)) != 3 {
			t.Errorf("invoice after a line was removed = %v", r.body)
		}
		expect(t, e.do("POST", "/api/finance/invoices/"+id2+"/approve", ceo, nil), 409, "deciding twice")
		expect(t, e.do("POST", "/api/finance/invoices/"+id2+"/cancel", office, nil), 409, "withdrawing an approved invoice")
	})

	t.Run("rejection", func(t *testing.T) {
		r := e.do("POST", "/api/finance/invoices", office, map[string]any{
			"description": "Kitchen upgrade", "expense_date": "2026-03-05",
			"items": []map[string]any{item("Equipment", "Espresso machine", "1", "95000")},
		})
		expect(t, r, 201, "raised")
		rid := r.body["id"].(string)
		out := e.do("POST", "/api/finance/invoices/"+rid+"/reject", hr, map[string]any{"note": "Not this quarter"})
		expect(t, out, 200, "hr rejects")
		if out.body["status"] != "rejected" || out.body["hr_note"] != "Not this quarter" || out.body["ledger_lines"].(float64) != 0 {
			t.Fatalf("rejected invoice = %v", out.body)
		}
		if n := ofType(e.notes(office), "invoice.rejected"); len(n) != 1 || !strings.Contains(n[0].Body, "Not this quarter") {
			t.Errorf("the raiser wasn't told why: %v", e.notes(office))
		}
		expect(t, e.do("PATCH", "/api/finance/invoices/"+rid, office, map[string]any{
			"description": "Cheaper one", "expense_date": "2026-03-05",
			"items": []map[string]any{item("Equipment", "Kettle", "1", "1500")},
		}), 409, "editing a rejected invoice")
	})

	t.Run("withdrawing while HR still has it", func(t *testing.T) {
		r := e.do("POST", "/api/finance/invoices", office, map[string]any{
			"description": "Office tea", "expense_date": "2026-03-06",
			"items": []map[string]any{item("Food & refreshments", "Tea and biscuits", "1", "600")},
		})
		expect(t, r, 201, "raised")
		wid := r.body["id"].(string)
		expect(t, e.do("POST", "/api/finance/invoices/"+wid+"/cancel", hr, nil), 403, "hr withdraws someone else's")
		expect(t, e.do("POST", "/api/finance/invoices/"+wid+"/cancel", office, nil), 200, "the raiser withdraws it")
		expect(t, e.do("POST", "/api/finance/invoices/"+wid+"/approve", hr, nil), 409, "approving a withdrawn invoice")
		if n := ofType(e.notes(hr), "invoice.withdrawn"); len(n) != 1 {
			t.Errorf("HR wasn't told it was withdrawn: %v", e.notes(hr))
		}
	})

	t.Run("another currency takes the rate for its date", func(t *testing.T) {
		expect(t, e.do("POST", "/api/finance/rates", hr, map[string]any{"currency": "USD", "rate": "122", "effective_from": "2026-03-01"}), 201, "rate")
		r := e.do("POST", "/api/finance/invoices", office, map[string]any{
			"description": "Design tools", "currency": "USD", "expense_date": "2026-03-10",
			"items": []map[string]any{
				item("Software & tools", "Figma seat", "2", "15"),
				item("Software & tools", "Stock photos", "1", "20"),
			},
		})
		expect(t, r, 201, "USD invoice")
		// 2 × 15 + 20 = $50 at 122 = Tk 6,100.
		if r.body["rate_to_bdt"] != "122.0000" || r.body["amount"] != "50.00" || r.body["amount_bdt"] != "6100.00" {
			t.Fatalf("USD invoice = %v", r.body)
		}
	})

	t.Run("filters and counts", func(t *testing.T) {
		all := e.do("GET", "/api/finance/invoices", ceo, nil)
		expect(t, all, 200, "ceo lists")
		w := all.body["waiting"].(map[string]any)
		// Waiting: the espresso machine was rejected, the tea withdrawn — the USD tools are left.
		if w["pending_hr"].(float64) != 1 || w["awaiting_ceo"].(float64) != 0 {
			t.Errorf("waiting counts = %v", w)
		}
		if r := e.do("GET", "/api/finance/invoices?status=approved", ceo, nil); len(r.body["invoices"].([]any)) != 2 {
			t.Errorf("approved = %v", r.body["invoices"])
		}
		if r := e.do("GET", "/api/finance/invoices?status=open", ceo, nil); len(r.body["invoices"].([]any)) != 1 {
			t.Errorf("open = %v", r.body["invoices"])
		}
		if r := e.do("GET", "/api/finance/invoices?mine=true", ceo, nil); len(r.body["invoices"].([]any)) != 0 {
			t.Errorf("the ceo raised none: %v", r.body["invoices"])
		}
		// The search covers the lines, not just what the invoice is called.
		if r := e.do("GET", "/api/finance/invoices?q=ceiling+fan", ceo, nil); len(r.body["invoices"].([]any)) != 2 {
			t.Errorf("search by line = %v", r.body["invoices"])
		}
		if r := e.do("GET", "/api/finance/invoices?q=meeting+room", ceo, nil); len(r.body["invoices"].([]any)) != 2 {
			t.Errorf("search by title = %v", r.body["invoices"])
		}
		if r := e.do("GET", "/api/finance/invoices?from=2026-03-05&to=2026-03-10", ceo, nil); len(r.body["invoices"].([]any)) != 3 {
			t.Errorf("date range = %v", r.body["invoices"])
		}
		expect(t, e.do("GET", "/api/finance/invoices?status=nonsense", ceo, nil), 422, "bad status")
		expect(t, e.do("GET", "/api/finance/invoices?from=nonsense", ceo, nil), 422, "bad date")
	})

	t.Run("the trail is logged without amounts", func(t *testing.T) {
		rows, err := e.store.Pool.Query(ctx, `SELECT action, metadata FROM activity_log WHERE entity_type = 'invoice' ORDER BY id`)
		if err != nil {
			t.Fatal(err)
		}
		defer rows.Close()
		seen := map[string]int{}
		for rows.Next() {
			var action string
			var meta map[string]any
			if err := rows.Scan(&action, &meta); err != nil {
				t.Fatal(err)
			}
			seen[action]++
			for k := range meta {
				if strings.Contains(k, "amount") || strings.Contains(k, "price") || strings.Contains(k, "rate") {
					t.Errorf("%s metadata carries %q: %v", action, k, meta)
				}
			}
		}
		for _, want := range []string{"invoice.created", "invoice.updated", "invoice.hr_approved", "invoice.approved", "invoice.rejected", "invoice.cancelled"} {
			if seen[want] == 0 {
				t.Errorf("nothing logged for %s (got %v)", want, seen)
			}
		}
	})
}
