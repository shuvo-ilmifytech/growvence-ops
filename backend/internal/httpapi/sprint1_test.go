package httpapi

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/solvinx/opsflow/internal/db"
	"github.com/solvinx/opsflow/internal/testutil"
)

// Sprint 1 (Money Safety, 2026-11-08): regression tests for the audit P0
// fixes — concurrent withdrawals, exact distribution sums, billing
// validation, internal-only surfaces, migration guards, recurring claims.

// TestSplitProfit checks the largest-remainder split in integer paisa: the
// cuts always sum exactly to the amount actually split, even for awkward
// amounts and for shares + reserve under 100%.
func TestSplitProfit(t *testing.T) {
	cases := []struct {
		name       string
		total      int64 // paisa
		parts      []profitPart
		wantSplit  int64
		wantShares map[string]int64 // "" key = reserve
	}{
		{
			name:  "100.01 split 33/33/34",
			total: 10001,
			parts: []profitPart{
				{investorID: uuidPtr("11111111-1111-1111-1111-111111111111"), pct: 33},
				{investorID: uuidPtr("22222222-2222-2222-2222-222222222222"), pct: 33},
				{investorID: nil, pct: 34},
			},
			wantSplit: 10001,
			wantShares: map[string]int64{
				"11111111-1111-1111-1111-111111111111": 3300,
				"22222222-2222-2222-2222-222222222222": 3300,
				"": 3401,
			},
		},
		{
			name:  "100000 split 25/15/10 leaves half undistributed",
			total: 10000000,
			parts: []profitPart{
				{investorID: uuidPtr("11111111-1111-1111-1111-111111111111"), pct: 25},
				{investorID: uuidPtr("22222222-2222-2222-2222-222222222222"), pct: 15},
				{investorID: nil, pct: 10},
			},
			wantSplit: 5000000,
			wantShares: map[string]int64{
				"11111111-1111-1111-1111-111111111111": 2500000,
				"22222222-2222-2222-2222-222222222222": 1500000,
				"": 1000000,
			},
		},
		{
			name:  "100.01 split 25/15/10 rounds the half paisa up",
			total: 10001,
			parts: []profitPart{
				{investorID: uuidPtr("11111111-1111-1111-1111-111111111111"), pct: 25},
				{investorID: uuidPtr("22222222-2222-2222-2222-222222222222"), pct: 15},
				{investorID: nil, pct: 10},
			},
			wantSplit: 5001,
			wantShares: map[string]int64{
				"11111111-1111-1111-1111-111111111111": 2501,
				"22222222-2222-2222-2222-222222222222": 1500,
				"": 1000,
			},
		},
		{
			name:  "one paisa to a single investor",
			total: 1,
			parts: []profitPart{
				{investorID: uuidPtr("11111111-1111-1111-1111-111111111111"), pct: 100},
			},
			wantSplit:  1,
			wantShares: map[string]int64{"11111111-1111-1111-1111-111111111111": 1},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			cuts, split := splitProfit(tc.total, tc.parts)
			if split != tc.wantSplit {
				t.Fatalf("split = %d, want %d", split, tc.wantSplit)
			}
			var sum int64
			got := map[string]int64{}
			for _, c := range cuts {
				sum += c.paisa
				got[idStr(c.investorID)] = c.paisa
			}
			if sum != split {
				t.Fatalf("cuts sum %d, want split %d", sum, split)
			}
			for k, want := range tc.wantShares {
				if got[k] != want {
					t.Fatalf("share %q = %d, want %d (all %v)", k, got[k], want, got)
				}
			}
		})
	}
}

func uuidPtr(s string) *uuid.UUID {
	id := uuid.MustParse(s)
	return &id
}

// TestSprint1ConcurrentWithdrawals: two withdrawal requests for the same
// investor, fired at the same time, each larger than half the balance —
// exactly one may succeed. Before the fix both read the same balance and
// both inserted, overdrawing the investor.
func TestSprint1ConcurrentWithdrawals(t *testing.T) {
	e := newEnv(t)
	ceo := e.login("ceo")

	r := e.do("POST", "/api/investors", ceo, map[string]any{
		"name": "Race Investor", "join_date": "2026-01-01",
	})
	if r.status != 201 {
		t.Fatalf("create investor: got %d (%v)", r.status, r.body)
	}
	invID := r.body["id"].(string)
	// 100% share, no reserve, so a 100.00 distribution earns exactly 100.00.
	expect(t, e.do("PUT", "/api/profit-settings", ceo, map[string]any{"reserve_percent": 0}), 200, "reserve 0")
	expect(t, e.do("PUT", "/api/investors/"+invID+"/share", ceo, map[string]any{"share_percent": 100, "effective_from": "2026-01-01"}), 201, "share 100")
	src := "33333333-3333-3333-3333-333333333333"
	expect(t, e.do("POST", "/api/investors/"+invID+"/distribute", ceo, map[string]any{
		"source_type": "manual", "source_id": src, "amount": 100, "currency": "BDT",
	}), 201, "distribute 100")

	// Two concurrent 60.00 withdrawals against a 100.00 balance.
	statuses := make([]int, 2)
	var wg sync.WaitGroup
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			statuses[i] = e.do("POST", "/api/investors/"+invID+"/withdrawals", ceo, map[string]any{
				"amount": 60, "currency": "BDT", "method": "bKash",
			}).status
		}(i)
	}
	wg.Wait()
	var ok, bad int
	for _, s := range statuses {
		switch s {
		case 201:
			ok++
		case 400:
			bad++
		default:
			t.Fatalf("withdrawal status %d, want 201 or 400", s)
		}
	}
	if ok != 1 || bad != 1 {
		t.Fatalf("concurrent withdrawals: %d succeeded, %d refused, want 1 and 1 (statuses %v)", ok, bad, statuses)
	}
	// Exactly 60.00 is now pending; the other 60 never landed.
	var pending string
	if err := e.store.Pool.QueryRow(context.Background(),
		`SELECT COALESCE(sum(amount_bdt), 0)::text FROM withdrawals WHERE investor_id = $1 AND status IN ('pending','approved')`,
		invID).Scan(&pending); err != nil {
		t.Fatalf("pending sum: %v", err)
	}
	if pending != "60.00" {
		t.Fatalf("pending withdrawals = %s, want 60.00", pending)
	}
}

// TestSprint1DistributionReconciles: an awkward manual distribution sums
// exactly to the distributable amount in the database, and a distribution
// without a caller-supplied source_id is refused.
func TestSprint1DistributionReconciles(t *testing.T) {
	e := newEnv(t)
	ceo := e.login("ceo")

	var ids []string
	for _, name := range []string{"Ira", "Ivan"} {
		r := e.do("POST", "/api/investors", ceo, map[string]any{"name": name, "join_date": "2026-01-01"})
		if r.status != 201 {
			t.Fatalf("create %s: got %d (%v)", name, r.status, r.body)
		}
		ids = append(ids, r.body["id"].(string))
	}
	expect(t, e.do("PUT", "/api/investors/"+ids[0]+"/share", ceo, map[string]any{"share_percent": 25, "effective_from": "2026-01-01"}), 201, "Ira 25")
	expect(t, e.do("PUT", "/api/investors/"+ids[1]+"/share", ceo, map[string]any{"share_percent": 15, "effective_from": "2026-01-01"}), 201, "Ivan 15")
	// Reserve defaults to 10, so the distributable half of 100.01 is 50.01.
	src := "44444444-4444-4444-4444-444444444444"
	expect(t, e.do("POST", "/api/investors/"+ids[0]+"/distribute", ceo, map[string]any{
		"source_type": "manual", "source_id": src, "amount": 100.01, "currency": "BDT",
	}), 201, "distribute 100.01")
	var sum string
	if err := e.store.Pool.QueryRow(context.Background(),
		`SELECT COALESCE(sum(amount_bdt), 0)::text FROM profit_distributions WHERE source_type = 'manual' AND source_id = $1`,
		src).Scan(&sum); err != nil {
		t.Fatalf("distribution sum: %v", err)
	}
	if sum != "50.01" {
		t.Fatalf("distributed sum = %s, want 50.01", sum)
	}
	// No source_id: the server must not invent one (P0-11).
	if r := e.do("POST", "/api/investors/"+ids[0]+"/distribute", ceo, map[string]any{
		"source_type": "manual", "amount": 100, "currency": "BDT",
	}); r.status != 400 {
		t.Fatalf("distribute without source_id: got %d, want 400", r.status)
	}
}

// TestSprint1BillingValidation: the billing endpoints reject bad money input
// with 400 and round totals to 2 decimals on the happy path.
func TestSprint1BillingValidation(t *testing.T) {
	e := newEnv(t)
	sales := e.login("sales")

	base := map[string]any{
		"client_name": "Acme Ltd", "currency": "BDT", "tax_rate": 10,
		"items": []map[string]any{{"description": "Work", "quantity": 3, "unit_price": 33.33}},
	}
	bad := []struct {
		name string
		mut  func(map[string]any)
	}{
		{"negative quantity", func(m map[string]any) {
			m["items"] = []map[string]any{{"description": "Work", "quantity": -1, "unit_price": 10}}
		}},
		{"zero quantity", func(m map[string]any) {
			m["items"] = []map[string]any{{"description": "Work", "quantity": 0, "unit_price": 10}}
		}},
		{"negative unit price", func(m map[string]any) {
			m["items"] = []map[string]any{{"description": "Work", "quantity": 1, "unit_price": -5}}
		}},
		{"tax over 100", func(m map[string]any) { m["tax_rate"] = 150 }},
		{"tax below 0", func(m map[string]any) { m["tax_rate"] = -1 }},
		{"bad currency", func(m map[string]any) { m["currency"] = "XX" }},
		{"empty client name", func(m map[string]any) { m["client_name"] = "  " }},
		{"bad due date", func(m map[string]any) { m["due_date"] = "not-a-date" }},
		{"unknown project", func(m map[string]any) {
			m["project_id"] = "55555555-5555-5555-5555-555555555555"
		}},
	}
	for _, tc := range bad {
		t.Run(tc.name, func(t *testing.T) {
			body := map[string]any{}
			for k, v := range base {
				body[k] = v
			}
			tc.mut(body)
			if r := e.do("POST", "/api/billing", sales, body); r.status != 400 {
				t.Fatalf("got %d, want 400 (body %v)", r.status, r.body)
			}
		})
	}

	// Happy path: 3 x 33.33 = 99.99, tax 10% = 10.00, total 109.99.
	r := e.do("POST", "/api/billing", sales, base)
	if r.status != 201 {
		t.Fatalf("create invoice: got %d (%v)", r.status, r.body)
	}
	id := r.body["id"].(string)
	if r.body["invoice_number"] == nil || r.body["invoice_number"] == "" {
		t.Fatalf("invoice has no number: %v", r.body)
	}
	r = e.do("GET", "/api/billing/"+id, sales, nil)
	if r.status != 200 {
		t.Fatalf("get invoice: got %d", r.status)
	}
	if r.body["subtotal"] != 99.99 || r.body["tax_amount"] != 10.0 || r.body["total"] != 109.99 {
		t.Fatalf("totals = subtotal %v tax %v total %v, want 99.99 / 10.00 / 109.99", r.body["subtotal"], r.body["tax_amount"], r.body["total"])
	}
}

// TestSprint1InternalSurfaces: knowledge base, reviews, templates and
// recurring reads are internal-only — external roles get 403, the team gets 200.
func TestSprint1InternalSurfaces(t *testing.T) {
	e := newEnv(t)
	sales := e.login("sales")
	paths := []string{"/api/kb/articles", "/api/reviews", "/api/reviews/stats", "/api/templates", "/api/recurring"}
	for _, p := range paths {
		expect(t, e.do("GET", p, sales, nil), 200, "sales "+p)
		for _, label := range []string{"client", "investor", "office_admin"} {
			expect(t, e.do("GET", p, e.login(label), nil), 403, label+" "+p)
		}
	}
}

// TestSprint1MigrationGuards verifies the 00048 database guards directly:
// non-negative invoice money, one login per investor, sequence numbers.
func TestSprint1MigrationGuards(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()

	// Negative invoice total is rejected by the CHECK.
	_, err := e.store.Pool.Exec(ctx, `
		INSERT INTO client_invoices (invoice_number, client_name, items, subtotal, tax_rate, tax_amount, total, currency, status)
		VALUES ('INV-TEST-1', 'Acme', '[]'::jsonb, 10, 0, 0, -1, 'BDT', 'draft')`)
	if err == nil {
		t.Fatal("negative invoice total was accepted")
	}
	// Tax rate outside 0-100 is rejected too.
	_, err = e.store.Pool.Exec(ctx, `
		INSERT INTO client_invoices (invoice_number, client_name, items, subtotal, tax_rate, tax_amount, total, currency, status)
		VALUES ('INV-TEST-2', 'Acme', '[]'::jsonb, 10, 101, 0, 10, 'BDT', 'draft')`)
	if err == nil {
		t.Fatal("tax_rate 101 was accepted")
	}
	// The sequence hands out increasing numbers.
	var a, b int64
	if err := e.store.Pool.QueryRow(ctx, `SELECT nextval('invoice_num_seq')`).Scan(&a); err != nil {
		t.Fatalf("nextval: %v", err)
	}
	if err := e.store.Pool.QueryRow(ctx, `SELECT nextval('invoice_num_seq')`).Scan(&b); err != nil {
		t.Fatalf("nextval: %v", err)
	}
	if b != a+1 {
		t.Fatalf("sequence not monotonic: %d then %d", a, b)
	}

	// One login links to at most one investor record.
	uid := e.users["investor"].ID
	inv1 := testutil.Must(e.store.Queries.CreateInvestor(ctx, db.CreateInvestorParams{
		UserID: &uid, Name: "First", JoinDate: "2026-01-01",
	}))
	_ = inv1
	_, err = e.store.Pool.Exec(ctx,
		`INSERT INTO investors (user_id, name, join_date) VALUES ($1, 'Second', '2026-01-01')`, uid)
	if err == nil {
		t.Fatal("second investor with the same user_id was accepted")
	}
	// A second investor WITHOUT a login is still fine.
	if _, err := e.store.Queries.CreateInvestor(ctx, db.CreateInvestorParams{
		Name: "NoLogin", JoinDate: "2026-01-01",
	}); err != nil {
		t.Fatalf("investor without user_id: %v", err)
	}
}

// TestSprint1RecurringClaim: two concurrent runs of the same recurring task
// create exactly one card. Before the fix the card insert and the schedule
// advance were separate statements, so a race duplicated the card.
func TestSprint1RecurringClaim(t *testing.T) {
	e := newEnv(t)
	admin := e.login("admin")
	sales := e.login("sales")
	ctx := context.Background()
	profile := testutil.Must(e.store.CreateProfile(ctx, db.CreateProfileParams{Name: "Studio Main", Platform: "Fiverr"}))
	ptype := testutil.Must(e.store.CreateProjectType(ctx, db.CreateProjectTypeParams{Name: "Website"}))
	today := time.Now().UTC().Format("2006-01-02")
	r := e.do("POST", "/api/projects", sales, map[string]any{
		"name": "Race project", "profile_id": profile.ID, "client_name": "Acme Ltd",
		"order_id": "FO-2001", "project_type_id": ptype.ID, "scope": "Build",
		"agreed_price": "100", "deadline": today,
	})
	if r.status != 201 {
		t.Fatalf("create project: got %d (%v)", r.status, r.body)
	}
	pid := r.body["project"].(map[string]any)["id"].(string)
	r = e.do("POST", "/api/recurring", admin, map[string]any{
		"title": "Daily standup card", "frequency": "daily", "project_id": pid, "next_run": today,
	})
	if r.status != 201 {
		t.Fatalf("create recurring: got %d (%v)", r.status, r.body)
	}
	taskID := uuid.MustParse(r.body["id"].(string))
	actor := e.users["admin"].ID

	errs := make([]error, 2)
	var wg sync.WaitGroup
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			errs[i] = runRecurringTask(context.Background(), e.store.Pool, taskID, actor)
		}(i)
	}
	wg.Wait()
	for _, err := range errs {
		if err != nil {
			t.Fatalf("runRecurringTask: %v", err)
		}
	}
	var cards int
	if err := e.store.Pool.QueryRow(ctx,
		`SELECT COUNT(*) FROM project_cards WHERE project_id = $1 AND title = 'Daily standup card'`,
		pid).Scan(&cards); err != nil {
		t.Fatalf("count cards: %v", err)
	}
	if cards != 1 {
		t.Fatalf("cards created = %d, want exactly 1", cards)
	}
}
