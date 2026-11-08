package httpapi

import (
	"context"
	"testing"
	"time"

	"github.com/solvinx/opsflow/internal/db"
	"github.com/solvinx/opsflow/internal/testutil"
)

// Investor Finance Forum (Phase 3): full HTTP flow against a migrated test
// DB — create, share, invest, distribute, withdraw, approve, pay — plus the
// guards (investors see only themselves, members see nothing).
func TestInvestors(t *testing.T) {
	e := newEnv(t)
	ceo := e.login("ceo")
	hr := e.login("hr")
	invUser := e.login("investor")
	member := e.login("member")

	// A member may not even list investors.
	if r := e.do("GET", "/api/investors", member, nil); r.status != 403 {
		t.Fatalf("member list investors: got %d, want 403", r.status)
	}

	// CEO creates two investors.
	var ira, ivan map[string]any
	if r := e.do("POST", "/api/investors", ceo, map[string]any{
		"name": "Ira Investor", "email": "ira@test.local", "join_date": "2026-01-01",
	}); r.status != 201 {
		t.Fatalf("create Ira: got %d (%v), want 201", r.status, r.body)
	} else {
		ira = r.body
	}
	if r := e.do("POST", "/api/investors", ceo, map[string]any{
		"name": "Ivan Investor", "email": "ivan@test.local", "join_date": "2026-01-01",
	}); r.status != 201 {
		t.Fatalf("create Ivan: got %d (%v), want 201", r.status, r.body)
	} else {
		ivan = r.body
	}
	iraID, ivanID := ira["id"].(string), ivan["id"].(string)

	// Shares: Ira 25%, Ivan 15%. Reserve defaults to 10%.
	for _, tc := range []struct {
		id  string
		pct float64
	}{{iraID, 25}, {ivanID, 15}} {
		if r := e.do("PUT", "/api/investors/"+tc.id+"/share", ceo, map[string]any{
			"share_percent": tc.pct, "effective_from": "2026-01-01",
		}); r.status != 201 {
			t.Fatalf("set share %v: got %d (%v), want 201", tc, r.status, r.body)
		}
	}
	// HR may not set shares.
	if r := e.do("PUT", "/api/investors/"+iraID+"/share", hr, map[string]any{
		"share_percent": 30, "effective_from": "2026-01-01",
	}); r.status != 403 {
		t.Fatalf("HR set share: got %d, want 403", r.status)
	}
	// A share that pushes the split past 100% is refused (Ivan 15 + new 76 + reserve 10 = 101).
	if r := e.do("PUT", "/api/investors/"+iraID+"/share", ceo, map[string]any{
		"share_percent": 76, "effective_from": "2026-01-01",
	}); r.status != 400 {
		t.Fatalf("over-100 share: got %d, want 400", r.status)
	}
	// ... and the refused share did not stick.
	if r := e.do("GET", "/api/investors/"+iraID, ceo, nil); r.status != 200 {
		t.Fatalf("get Ira: got %d", r.status)
	} else if got := r.body["investor"].(map[string]any)["share_percent"].(string); got != "25.00" {
		t.Fatalf("Ira share after refused change: got %s, want 25.00", got)
	}

	// Ira invests $1000 at 120 -> 120000 BDT.
	if r := e.do("POST", "/api/investors/"+iraID+"/investments", ceo, map[string]any{
		"amount": 1000, "currency": "USD", "rate_to_bdt": "120",
		"method": "PayPal", "invested_at": "2026-01-02", "note": "seed",
	}); r.status != 201 {
		t.Fatalf("record investment: got %d (%v), want 201", r.status, r.body)
	}
	if r := e.do("GET", "/api/investors/"+iraID, ceo, nil); r.status != 200 {
		t.Fatalf("get Ira: got %d", r.status)
	} else {
		inv := r.body["investor"].(map[string]any)
		if inv["invested_bdt"].(string) != "120000.00" {
			t.Fatalf("Ira invested: got %s, want 120000.00", inv["invested_bdt"])
		}
		if len(r.body["investments"].([]any)) != 1 {
			t.Fatalf("investments: got %d, want 1", len(r.body["investments"].([]any)))
		}
	}

	// Manual distribution of 100000 BDT: Ira 25000, Ivan 15000, reserve 10000.
	src := "11111111-1111-1111-1111-111111111111"
	if r := e.do("POST", "/api/investors/"+iraID+"/distribute", ceo, map[string]any{
		"source_type": "manual", "source_id": src, "amount": 100000,
		"currency": "BDT", "note": "January profit",
	}); r.status != 201 {
		t.Fatalf("distribute: got %d (%v), want 201", r.status, r.body)
	} else if int(r.body["rows_created"].(float64)) != 3 {
		t.Fatalf("distribute rows: got %v, want 3", r.body["rows_created"])
	}
	// Same source again: 409, nothing paid twice.
	if r := e.do("POST", "/api/investors/"+iraID+"/distribute", ceo, map[string]any{
		"source_type": "manual", "source_id": src, "amount": 100000, "currency": "BDT",
	}); r.status != 409 {
		t.Fatalf("redistribute: got %d, want 409", r.status)
	}
	if r := e.do("GET", "/api/investors/"+iraID, ceo, nil); r.status != 200 {
		t.Fatalf("get Ira: got %d", r.status)
	} else if got := r.body["investor"].(map[string]any)["earned_bdt"].(string); got != "25000.00" {
		t.Fatalf("Ira earned: got %s, want 25000.00", got)
	}
	if r := e.do("GET", "/api/company-account", ceo, nil); r.status != 200 {
		t.Fatalf("company account: got %d", r.status)
	} else {
		totals := r.body["totals"].(map[string]any)
		if totals["reserve_bdt"].(string) != "10000.00" {
			t.Fatalf("reserve: got %s, want 10000.00", totals["reserve_bdt"])
		}
		if totals["total_distributed_bdt"].(string) != "40000.00" {
			t.Fatalf("distributed: got %s, want 40000.00", totals["total_distributed_bdt"])
		}
	}

	// Auto-distribution: recording a milestone payment splits it too
	// (25% Ira, 15% Ivan, 10% reserve) without any manual step.
	sales := e.login("sales")
	ctx := context.Background()
	profile := testutil.Must(e.store.CreateProfile(ctx, db.CreateProfileParams{Name: "Studio Main", Platform: "Fiverr"}))
	ptype := testutil.Must(e.store.CreateProjectType(ctx, db.CreateProjectTypeParams{Name: "Website"}))
	today := time.Now().UTC().Format("2006-01-02")
	r := e.do("POST", "/api/projects", sales, map[string]any{
		"name": "Acme storefront", "profile_id": profile.ID, "client_name": "Acme Ltd",
		"order_id": "FO-1001", "project_type_id": ptype.ID, "scope": "Build a store",
		"agreed_price": "1000.50", "deadline": today,
	})
	if r.status != 201 {
		t.Fatalf("create project: got %d (%v), want 201", r.status, r.body)
	}
	pid := r.body["project"].(map[string]any)["id"].(string)
	r = e.do("POST", "/api/projects/"+pid+"/milestones", sales, map[string]any{
		"label": "Starting amount", "amount": 20000, "currency": "BDT", "due_date": today,
	})
	if r.status != 201 {
		t.Fatalf("create milestone: got %d (%v), want 201", r.status, r.body)
	}
	mid := r.body["id"].(string)
	r = e.do("POST", "/api/projects/"+pid+"/milestones/"+mid+"/record-payment", sales, map[string]any{
		"amount": 20000, "currency": "BDT", "method": "bKash", "paid_at": today,
	})
	if r.status != 200 {
		t.Fatalf("record milestone payment: got %d (%v), want 200", r.status, r.body)
	}
	if r := e.do("GET", "/api/investors/"+iraID, ceo, nil); r.status != 200 {
		t.Fatalf("get Ira: got %d", r.status)
	} else if got := r.body["investor"].(map[string]any)["earned_bdt"].(string); got != "30000.00" {
		t.Fatalf("Ira earned after milestone: got %s, want 30000.00 (25000 manual + 5000 auto)", got)
	}
	if r := e.do("GET", "/api/company-account", ceo, nil); r.status != 200 {
		t.Fatalf("company account: got %d", r.status)
	} else if got := r.body["totals"].(map[string]any)["reserve_bdt"].(string); got != "12000.00" {
		t.Fatalf("reserve after milestone: got %s, want 12000.00 (10000 + 2000 auto)", got)
	}

	// Withdrawal: over the balance is refused; 5000 goes through the states.
	if r := e.do("POST", "/api/investors/"+iraID+"/withdrawals", ceo, map[string]any{
		"amount": 999999, "currency": "BDT", "method": "bKash",
	}); r.status != 400 {
		t.Fatalf("over-balance withdrawal: got %d, want 400", r.status)
	}
	var w map[string]any
	if r := e.do("POST", "/api/investors/"+iraID+"/withdrawals", ceo, map[string]any{
		"amount": 5000, "currency": "BDT", "method": "bKash", "note": "first",
	}); r.status != 201 {
		t.Fatalf("request withdrawal: got %d (%v), want 201", r.status, r.body)
	} else {
		w = r.body
	}
	wid := w["id"].(string)
	// HR may not approve.
	if r := e.do("PUT", "/api/withdrawals/"+wid+"/approve", hr, nil); r.status != 403 {
		t.Fatalf("HR approve: got %d, want 403", r.status)
	}
	// Paying before approval is refused.
	if r := e.do("PUT", "/api/withdrawals/"+wid+"/mark-paid", ceo, nil); r.status != 400 {
		t.Fatalf("mark-paid before approve: got %d, want 400", r.status)
	}
	if r := e.do("PUT", "/api/withdrawals/"+wid+"/approve", ceo, nil); r.status != 200 {
		t.Fatalf("approve: got %d (%v), want 200", r.status, r.body)
	}
	if r := e.do("PUT", "/api/withdrawals/"+wid+"/mark-paid", ceo, nil); r.status != 200 {
		t.Fatalf("mark-paid: got %d (%v), want 200", r.status, r.body)
	}
	if r := e.do("GET", "/api/investors/"+iraID, ceo, nil); r.status != 200 {
		t.Fatalf("get Ira: got %d", r.status)
	} else {
		inv := r.body["investor"].(map[string]any)
		if inv["withdrawn_bdt"].(string) != "5000.00" {
			t.Fatalf("Ira withdrawn: got %s, want 5000.00", inv["withdrawn_bdt"])
		}
	}

	// Expenses: add, list, delete (CEO only).
	var exp map[string]any
	if r := e.do("POST", "/api/investor-expenses", hr, map[string]any{
		"category": "Tools", "description": "Canva Pro", "amount": 1500,
		"currency": "BDT", "expense_date": "2026-01-05",
	}); r.status != 201 {
		t.Fatalf("add expense: got %d (%v), want 201", r.status, r.body)
	} else {
		exp = r.body
	}
	if r := e.do("POST", "/api/investor-expenses", hr, map[string]any{
		"category": "Bogus", "description": "x", "amount": 10, "currency": "BDT",
	}); r.status != 400 {
		t.Fatalf("bad category: got %d, want 400", r.status)
	}
	if r := e.do("DELETE", "/api/investor-expenses/"+exp["id"].(string), hr, nil); r.status != 403 {
		t.Fatalf("HR delete expense: got %d, want 403", r.status)
	}
	if r := e.do("GET", "/api/investor-expenses", ceo, nil); r.status != 200 {
		t.Fatalf("list expenses: got %d", r.status)
	} else if len(r.body["expenses"].([]any)) != 1 {
		t.Fatalf("expenses: got %d, want 1", len(r.body["expenses"].([]any)))
	}
	if r := e.do("DELETE", "/api/investor-expenses/"+exp["id"].(string), ceo, nil); r.status != 200 {
		t.Fatalf("CEO delete expense: got %d, want 200", r.status)
	}

	// Currency rates: set USD 121, read it back as latest.
	if r := e.do("POST", "/api/currency-rates", ceo, map[string]any{
		"currency": "USD", "rate": 121, "effective_from": "2026-01-01",
	}); r.status != 201 {
		t.Fatalf("set rate: got %d (%v), want 201", r.status, r.body)
	}
	if r := e.do("POST", "/api/currency-rates", ceo, map[string]any{
		"currency": "USD", "rate": 122, "effective_from": "2026-01-01",
	}); r.status != 409 {
		t.Fatalf("duplicate rate: got %d, want 409", r.status)
	}
	if r := e.do("GET", "/api/currency-rates/latest?currency=USD", ceo, nil); r.status != 200 {
		t.Fatalf("latest rate: got %d", r.status)
	} else if r.body["rate"].(string) != "121.0000" {
		t.Fatalf("latest rate: got %v, want 121.0000", r.body["rate"])
	}

	// Investor self-service: link the investor-role user to Ira, then they see
	// only their own dashboard — and nobody else's record.
	if _, err := e.store.Pool.Exec(t.Context(), `UPDATE investors SET user_id = $1 WHERE id = $2`,
		e.users["investor"].ID, iraID); err != nil {
		t.Fatalf("link investor user: %v", err)
	}
	if r := e.do("GET", "/api/investor/me", invUser, nil); r.status != 200 {
		t.Fatalf("investor me: got %d (%v), want 200", r.status, r.body)
	} else {
		inv := r.body["investor"].(map[string]any)
		if inv["name"].(string) != "Ira Investor" {
			t.Fatalf("investor me name: got %v", inv["name"])
		}
		if r.body["reserve_bdt"].(string) != "12000.00" {
			t.Fatalf("investor me reserve: got %v, want 12000.00", r.body["reserve_bdt"])
		}
		if _, ok := r.body["investors"]; ok {
			t.Fatalf("investor me leaked the investor list")
		}
	}
	// The same investor may not open Ivan's record.
	if r := e.do("GET", "/api/investors/"+ivanID, invUser, nil); r.status != 403 {
		t.Fatalf("investor opening another record: got %d, want 403", r.status)
	}
	// ... but may open their own.
	if r := e.do("GET", "/api/investors/"+iraID, invUser, nil); r.status != 200 {
		t.Fatalf("investor opening own record: got %d, want 200", r.status)
	}
	// And may request their own withdrawal (within balance: 30000 earned − 5000 paid = 25000).
	if r := e.do("POST", "/api/investors/"+iraID+"/withdrawals", invUser, map[string]any{
		"amount": 20000, "currency": "BDT", "method": "Bank Transfer",
	}); r.status != 201 {
		t.Fatalf("investor self withdrawal: got %d (%v), want 201", r.status, r.body)
	}
	// 20000 already pending leaves 5000 available, so 5001 is refused.
	if r := e.do("POST", "/api/investors/"+iraID+"/withdrawals", invUser, map[string]any{
		"amount": 5001, "currency": "BDT", "method": "Cash",
	}); r.status != 400 {
		t.Fatalf("withdrawal past pending balance: got %d, want 400", r.status)
	}

	// Reserve: only the CEO, and shares + reserve must stay within 100%.
	if r := e.do("PUT", "/api/profit-settings", hr, map[string]any{"reserve_percent": 20}); r.status != 403 {
		t.Fatalf("HR set reserve: got %d, want 403", r.status)
	}
	if r := e.do("PUT", "/api/profit-settings", ceo, map[string]any{"reserve_percent": 61}); r.status != 400 {
		t.Fatalf("reserve over 100: got %d, want 400", r.status)
	}
	if r := e.do("PUT", "/api/profit-settings", ceo, map[string]any{"reserve_percent": 12}); r.status != 200 {
		t.Fatalf("set reserve 12: got %d (%v), want 200", r.status, r.body)
	}
	if r := e.do("GET", "/api/profit-settings", ceo, nil); r.status != 200 {
		t.Fatalf("get settings: got %d", r.status)
	} else if r.body["reserve_percent"].(float64) != 12 {
		t.Fatalf("reserve: got %v, want 12", r.body["reserve_percent"])
	}
}
