package httpapi

import (
	"context"
	"math"
	"testing"
	"time"

	"github.com/solvinx/opsflow/internal/db"
	"github.com/solvinx/opsflow/internal/testutil"
)

// B2B project features (Phase 5): contracts, retainers, milestone teams +
// SLA, retainer billing and the B2B report. Runs against a throwaway DB
// (TEST_DATABASE_URL) + Redis (TEST_REDIS_URL) + S3 (TEST_S3_BUCKET).
func TestB2B(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	sales := e.login("sales")
	ceo, hr := e.login("ceo"), e.login("hr")
	member := e.login("member")
	tl := e.login("tl")
	today := time.Now().UTC().Format("2006-01-02")
	nextYear := time.Now().UTC().AddDate(1, 0, 0).Format("2006-01-02")

	// ── Client + project + team ───────────────────────────
	r := e.do("POST", "/api/clients", sales, map[string]any{"company_name": "Acme Ltd", "type": "B2B"})
	expect(t, r, 201, "create client")
	clientID := r.body["id"].(string)
	if r.body["client_code"].(string) != "CL-0001" {
		t.Fatalf("client code: got %v, want CL-0001", r.body["client_code"])
	}

	profile := testutil.Must(e.store.CreateProfile(ctx, db.CreateProfileParams{Name: "Studio Main", Platform: "Fiverr"}))
	ptype := testutil.Must(e.store.CreateProjectType(ctx, db.CreateProjectTypeParams{Name: "Website"}))
	r = e.do("POST", "/api/projects", sales, map[string]any{
		"name": "Acme storefront", "profile_id": profile.ID, "client_name": "Acme Ltd",
		"order_id": "FO-1001", "project_type_id": ptype.ID, "scope": "Build a store",
		"agreed_price": "50000", "deadline": nextYear,
	})
	expect(t, r, 201, "create project")
	pid := r.body["project"].(map[string]any)["id"].(string)

	r = e.do("POST", "/api/projects/"+pid+"/link-client", sales, map[string]any{"client_id": clientID})
	expect(t, r, 201, "link client to project")

	r = e.do("POST", "/api/admin/teams", hr, map[string]any{"name": "Shopify", "lead_id": e.users["tl"].ID})
	expect(t, r, 201, "create Shopify team")
	teamID := r.body["team"].(map[string]any)["id"].(string)

	// ── Contracts ─────────────────────────────────────────
	r = e.do("POST", "/api/contracts", sales, map[string]any{
		"client_id": clientID, "project_id": pid, "title": "Website build",
		"value": 50000, "currency": "BDT", "start_date": today, "end_date": nextYear,
		"terms": "50% advance, 50% on delivery",
	})
	expect(t, r, 201, "create contract")
	if r.body["contract_number"].(string) != "CTR-0001" {
		t.Fatalf("contract number: got %v, want CTR-0001", r.body["contract_number"])
	}
	contractID := r.body["id"].(string)

	// A contract without an end date: the nullable date must scan (pgx
	// date -> *string) instead of failing the read.
	r = e.do("POST", "/api/contracts", sales, map[string]any{
		"client_id": clientID, "title": "Ongoing support",
		"value": 10000, "currency": "BDT", "start_date": today,
	})
	expect(t, r, 201, "create open-ended contract")
	if r.body["contract_number"].(string) != "CTR-0002" {
		t.Fatalf("contract number: got %v, want CTR-0002", r.body["contract_number"])
	}
	openID := r.body["id"].(string)
	r = e.do("GET", "/api/contracts/"+openID, sales, nil)
	expect(t, r, 200, "read open-ended contract")
	if got := r.body["contract"].(map[string]any)["end_date"]; got != nil {
		t.Fatalf("open-ended end_date: got %v, want null", got)
	}

	// Bad dates are refused.
	r = e.do("POST", "/api/contracts", sales, map[string]any{
		"client_id": clientID, "title": "Bad dates",
		"value": 1, "currency": "BDT", "start_date": nextYear, "end_date": today,
	})
	expect(t, r, 400, "end before start refused")

	// Role gates: members may not read or write contracts; CEO/HR read.
	expect(t, e.do("GET", "/api/contracts", member, nil), 403, "member cannot read contracts")
	expect(t, e.do("POST", "/api/contracts", member, map[string]any{}), 403, "member cannot create contracts")
	expect(t, e.do("GET", "/api/contracts", ceo, nil), 200, "ceo reads contracts")
	expect(t, e.do("GET", "/api/contracts", hr, nil), 200, "hr reads contracts")

	// Status flow: draft -> active (signs it) -> completed; draft is unreachable
	// again, and a completed contract is read-only.
	r = e.do("PUT", "/api/contracts/"+contractID, sales, map[string]any{
		"client_id": clientID, "project_id": pid, "title": "Website build",
		"value": 50000, "currency": "BDT", "start_date": today, "end_date": nextYear,
		"terms": "50% advance, 50% on delivery", "status": "active",
	})
	expect(t, r, 200, "activate contract")
	r = e.do("GET", "/api/contracts/"+contractID, sales, nil)
	if r.body["contract"].(map[string]any)["signed_at"] == nil {
		t.Fatalf("activating did not set signed_at")
	}
	r = e.do("PUT", "/api/contracts/"+contractID, sales, map[string]any{
		"client_id": clientID, "title": "Website build",
		"value": 50000, "currency": "BDT", "start_date": today, "end_date": nextYear,
		"status": "draft",
	})
	expect(t, r, 400, "active -> draft refused")
	r = e.do("PUT", "/api/contracts/"+contractID, sales, map[string]any{
		"client_id": clientID, "title": "Website build",
		"value": 50000, "currency": "BDT", "start_date": today, "end_date": nextYear,
		"status": "completed",
	})
	expect(t, r, 200, "complete contract")
	r = e.do("PUT", "/api/contracts/"+contractID, sales, map[string]any{
		"client_id": clientID, "title": "Website build",
		"value": 50000, "currency": "BDT", "start_date": today, "end_date": nextYear,
		"status": "active",
	})
	expect(t, r, 400, "completed contract is read-only")

	// Renew: a fresh draft (new CTR number) and the old one stays completed.
	r = e.do("POST", "/api/contracts/"+contractID+"/renew", sales, map[string]any{
		"start_date": nextYear, "end_date": time.Now().UTC().AddDate(2, 0, 0).Format("2006-01-02"),
	})
	expect(t, r, 201, "renew contract")
	if r.body["contract_number"].(string) != "CTR-0003" {
		t.Fatalf("renewed number: got %v, want CTR-0003", r.body["contract_number"])
	}
	r = e.do("GET", "/api/contracts/"+contractID, sales, nil)
	if r.body["contract"].(map[string]any)["status"].(string) != "completed" {
		t.Fatalf("renewed-from status: got %v, want completed", r.body["contract"].(map[string]any)["status"])
	}
	r = e.do("POST", "/api/contracts/"+openID+"/renew", sales, map[string]any{"start_date": today})
	expect(t, r, 400, "draft cannot be renewed")

	// ── Retainers ─────────────────────────────────────────
	// Start day 7 <= billing day 15: first billing is the 15th of the start month.
	r = e.do("POST", "/api/retainers", sales, map[string]any{
		"client_id": clientID, "title": "Monthly maintenance",
		"monthly_fee": 5000, "currency": "BDT", "start_date": "2026-11-07", "billing_day": 15,
	})
	expect(t, r, 201, "create retainer")
	if r.body["next_billing_date"].(string) != "2026-11-15" {
		t.Fatalf("first billing: got %v, want 2026-11-15", r.body["next_billing_date"])
	}
	retainerID := r.body["id"].(string)
	// Start day 20 > billing day 10: first billing is the 10th of next month.
	r = e.do("POST", "/api/retainers", sales, map[string]any{
		"client_id": clientID, "title": "SEO retainer",
		"monthly_fee": 3000, "currency": "BDT", "start_date": "2026-11-20", "billing_day": 10,
	})
	expect(t, r, 201, "create second retainer")
	if r.body["next_billing_date"].(string) != "2026-12-10" {
		t.Fatalf("first billing: got %v, want 2026-12-10", r.body["next_billing_date"])
	}
	r = e.do("POST", "/api/retainers", sales, map[string]any{
		"client_id": clientID, "title": "Bad day",
		"monthly_fee": 100, "currency": "BDT", "start_date": "2026-11-07", "billing_day": 29,
	})
	expect(t, r, 400, "billing day 29 refused")

	// Retainer billing run: make the first retainer due today, bill it, and
	// prove a second run bills nothing (idempotency). The no-renew retainer
	// is due today already and pauses after its one bill.
	r = e.do("POST", "/api/retainers", sales, map[string]any{
		"client_id": clientID, "title": "One-off retainer",
		"monthly_fee": 7000, "currency": "BDT", "start_date": today,
		"billing_day": dayOf(today), "auto_renew": false,
	})
	expect(t, r, 201, "create one-off retainer")
	oneOffID := r.body["id"].(string)
	if r.body["next_billing_date"].(string) != today {
		t.Fatalf("one-off first billing: got %v, want %s", r.body["next_billing_date"], today)
	}
	if _, err := e.store.Pool.Exec(ctx, `UPDATE retainers SET next_billing_date = CURRENT_DATE WHERE id = $1`, retainerID); err != nil {
		t.Fatalf("make retainer due: %v", err)
	}
	if err := ProcessDueRetainers(ctx, e.store.Pool); err != nil {
		t.Fatalf("retainer billing run: %v", err)
	}
	var invoiceCount int
	if err := e.store.Pool.QueryRow(ctx, `SELECT count(*) FROM client_invoices`).Scan(&invoiceCount); err != nil {
		t.Fatalf("count invoices: %v", err)
	}
	if invoiceCount != 2 {
		t.Fatalf("invoices after first run: got %d, want 2", invoiceCount)
	}
	var nextBilling string
	if err := e.store.Pool.QueryRow(ctx, `SELECT next_billing_date::text FROM retainers WHERE id = $1`, retainerID).Scan(&nextBilling); err != nil {
		t.Fatalf("read next billing: %v", err)
	}
	wantNext := time.Now().UTC().AddDate(0, 1, 0).Format("2006-01-02")
	if nextBilling != wantNext {
		t.Fatalf("next billing advanced: got %s, want %s", nextBilling, wantNext)
	}
	var oneOffStatus string
	if err := e.store.Pool.QueryRow(ctx, `SELECT status FROM retainers WHERE id = $1`, oneOffID).Scan(&oneOffStatus); err != nil {
		t.Fatalf("read one-off status: %v", err)
	}
	if oneOffStatus != "paused" {
		t.Fatalf("one-off status: got %s, want paused", oneOffStatus)
	}
	if err := ProcessDueRetainers(ctx, e.store.Pool); err != nil {
		t.Fatalf("retainer billing re-run: %v", err)
	}
	if err := e.store.Pool.QueryRow(ctx, `SELECT count(*) FROM client_invoices`).Scan(&invoiceCount); err != nil {
		t.Fatalf("count invoices: %v", err)
	}
	if invoiceCount != 2 {
		t.Fatalf("invoices after re-run: got %d, want 2 (no double billing)", invoiceCount)
	}
	// Due list is empty now (both billed), and the invoice numbers are INV-YYYY-XXXX.
	r = e.do("GET", "/api/retainers/due", sales, nil)
	expect(t, r, 200, "due retainers")
	if len(r.body["retainers"].([]any)) != 0 {
		t.Fatalf("due retainers: got %d, want 0", len(r.body["retainers"].([]any)))
	}
	var invNum string
	if err := e.store.Pool.QueryRow(ctx, `SELECT invoice_number FROM client_invoices ORDER BY created_at LIMIT 1`).Scan(&invNum); err != nil {
		t.Fatalf("read invoice number: %v", err)
	}
	if len(invNum) != 13 || invNum[:4] != "INV-" {
		t.Fatalf("invoice number: got %q, want INV-YYYY-XXXX", invNum)
	}

	// Pause/resume through the API.
	r = e.do("PUT", "/api/retainers/"+retainerID, sales, map[string]any{
		"client_id": clientID, "title": "Monthly maintenance",
		"monthly_fee": 5000, "currency": "BDT", "start_date": "2026-11-07", "billing_day": 15,
		"auto_renew": true, "status": "paused",
	})
	expect(t, r, 200, "pause retainer")
	r = e.do("PUT", "/api/retainers/"+retainerID, sales, map[string]any{
		"client_id": clientID, "title": "Monthly maintenance",
		"monthly_fee": 5000, "currency": "BDT", "start_date": "2026-11-07", "billing_day": 15,
		"auto_renew": true, "status": "cancelled",
	})
	expect(t, r, 200, "cancel retainer")
	r = e.do("PUT", "/api/retainers/"+retainerID, sales, map[string]any{
		"client_id": clientID, "title": "Monthly maintenance",
		"monthly_fee": 5000, "currency": "BDT", "start_date": "2026-11-07", "billing_day": 15,
		"auto_renew": true, "status": "active",
	})
	expect(t, r, 400, "cancelled retainer cannot resume")

	// ── Milestone team + SLA ──────────────────────────────
	r = e.do("POST", "/api/projects/"+pid+"/milestones", sales, map[string]any{
		"label": "Starting amount", "amount": 20000, "currency": "BDT", "due_date": today,
		"team_id": teamID, "sla_response_hours": 24,
	})
	expect(t, r, 201, "create milestone with team + SLA")
	mid := r.body["id"].(string)
	r = e.do("GET", "/api/projects/"+pid+"/milestones", sales, nil)
	expect(t, r, 200, "list milestones")
	ms := r.body["milestones"].([]any)[0].(map[string]any)
	if ms["team_name"].(string) != "Shopify" {
		t.Fatalf("milestone team: got %v, want Shopify", ms["team_name"])
	}
	if ms["sla_response_hours"].(float64) != 24 {
		t.Fatalf("milestone SLA: got %v, want 24", ms["sla_response_hours"])
	}
	// Reassign through the dedicated endpoint; bogus teams are refused.
	r = e.do("PUT", "/api/projects/"+pid+"/milestones/"+mid+"/team", tl, map[string]any{
		"team_id": teamID, "sla_response_hours": 48,
	})
	expect(t, r, 200, "reassign milestone team")
	r = e.do("PUT", "/api/projects/"+pid+"/milestones/"+mid+"/team", tl, map[string]any{
		"team_id": "00000000-0000-0000-0000-000000000000", "sla_response_hours": 48,
	})
	expect(t, r, 404, "bogus team refused")
	r = e.do("PUT", "/api/projects/"+pid+"/milestones/"+mid+"/team", member, map[string]any{
		"team_id": teamID,
	})
	expect(t, r, 403, "member cannot assign milestone team")

	// Pay the first milestone on time; leave a second one overdue.
	r = e.do("POST", "/api/projects/"+pid+"/milestones/"+mid+"/record-payment", sales, map[string]any{
		"amount": 20000, "method": "bKash", "paid_at": today,
	})
	expect(t, r, 200, "record milestone payment")
	r = e.do("POST", "/api/projects/"+pid+"/milestones", sales, map[string]any{
		"label": "Final delivery", "amount": 30000, "currency": "BDT",
		"due_date": time.Now().UTC().AddDate(0, 0, -3).Format("2006-01-02"),
	})
	expect(t, r, 201, "create second milestone")
	mid2 := r.body["id"].(string)
	if _, err := e.store.Pool.Exec(ctx, `UPDATE payment_milestones SET status = 'overdue' WHERE id = $1`, mid2); err != nil {
		t.Fatalf("mark milestone overdue: %v", err)
	}

	// ── B2B report ────────────────────────────────────────
	r = e.do("GET", "/api/b2b/report", member, nil)
	expect(t, r, 403, "member cannot read the B2B report")
	r = e.do("GET", "/api/b2b/report", ceo, nil)
	expect(t, r, 200, "ceo reads the B2B report")
	rev := r.body["revenue_by_client"].([]any)
	if len(rev) != 1 {
		t.Fatalf("revenue rows: got %d, want 1", len(rev))
	}
	if rev[0].(map[string]any)["revenue_bdt"].(float64) != 20000 {
		t.Fatalf("revenue: got %v, want 20000", rev[0].(map[string]any)["revenue_bdt"])
	}
	out := r.body["outstanding"].([]any)
	if len(out) != 1 || out[0].(map[string]any)["outstanding_bdt"].(float64) != 30000 {
		t.Fatalf("outstanding: got %v, want 30000", out)
	}
	prof := r.body["profitability"].([]any)
	if len(prof) != 1 {
		t.Fatalf("profitability rows: got %d, want 1", len(prof))
	}
	p := prof[0].(map[string]any)
	if p["agreed_bdt"].(float64) != 50000 || p["received_bdt"].(float64) != 20000 || p["outstanding_bdt"].(float64) != 30000 {
		t.Fatalf("profitability: got agreed %v received %v outstanding %v, want 50000/20000/30000",
			p["agreed_bdt"], p["received_bdt"], p["outstanding_bdt"])
	}
	health := r.body["health"].([]any)
	if len(health) != 1 {
		t.Fatalf("health rows: got %d, want 1", len(health))
	}
	h := health[0].(map[string]any)
	if h["on_time"].(float64) != 100 {
		t.Fatalf("health on_time: got %v, want 100", h["on_time"])
	}
	if h["response_rate"].(float64) != 100 {
		t.Fatalf("health response: got %v, want 100", h["response_rate"])
	}
	if h["overdue_count"].(float64) != 1 {
		t.Fatalf("health overdue: got %v, want 1", h["overdue_count"])
	}
	wantScore := math.Round(0.5*h["on_time"].(float64) + 0.25*h["response_rate"].(float64) + 0.25*h["active_score"].(float64))
	if h["score"].(float64) != wantScore {
		t.Fatalf("health score: got %v, want %v (50%% on-time + 25%% response + 25%% active)", h["score"], wantScore)
	}
	// Retainers due soon (7d) and contracts expiring soon (30d) ride along.
	if len(r.body["retainers_due"].([]any)) != 0 {
		t.Fatalf("retainers due soon: got %d, want 0", len(r.body["retainers_due"].([]any)))
	}
}

func dayOf(date string) int {
	d, _ := time.Parse("2006-01-02", date)
	return d.Day()
}
