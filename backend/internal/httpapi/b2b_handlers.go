package httpapi

import (
	"context"
	"fmt"
	"math"
	"strings"
	"time"

	"github.com/gofiber/fiber/v3"
	"github.com/google/uuid"

	"github.com/solvinx/opsflow/internal/db"
	"github.com/solvinx/opsflow/internal/notify"
	"github.com/solvinx/opsflow/internal/policy"
)

// B2B project features (Phase 5): contracts, retainers, per-milestone teams
// + SLA, and the B2B report (revenue, outstanding, profitability, health).
//
// Who may do what:
//   - Contracts/retainers: sales and system admins manage; TL, CEO and HR read.
//     (The CEO's writes are blocked by readOnlyGuard anyway, like milestones.)
//   - Milestone team assignment: sales, TL and system admins.
//   - B2B report: CEO, system admins and sales.

func canViewB2B(u db.User) bool {
	r := roleOf(u)
	return isSystemAdmin(u) || r == policy.Sales || r == policy.TL || r == policy.HR
}

func canManageB2B(u db.User) bool {
	return u.IsAdmin || roleOf(u) == policy.Sales
}

func canAssignMilestoneTeam(u db.User) bool {
	r := roleOf(u)
	return u.IsAdmin || r == policy.Sales || r == policy.TL
}

func canViewB2BReport(u db.User) bool {
	return isSystemAdmin(u) || roleOf(u) == policy.Sales
}

var contractStatuses = map[string]bool{
	"draft": true, "active": true, "completed": true, "terminated": true, "expired": true,
}

// contractTransitions: which status a contract may move to. Terminal states
// (completed, terminated, expired) never move again.
var contractTransitions = map[string][]string{
	"draft":  {"active", "terminated"},
	"active": {"completed", "terminated", "expired"},
}

func validContractTransition(from, to string) bool {
	if from == to {
		return true
	}
	for _, next := range contractTransitions[from] {
		if next == to {
			return true
		}
	}
	return false
}

var retainerStatuses = map[string]bool{"active": true, "paused": true, "cancelled": true}

func validRetainerTransition(from, to string) bool {
	if from == to {
		return true
	}
	switch from {
	case "active":
		return to == "paused" || to == "cancelled"
	case "paused":
		return to == "active" || to == "cancelled"
	}
	return false
}

// firstBillingDate: the first billing date for a retainer — the billing day of
// the start month when the start day hasn't passed it yet, otherwise the
// billing day of the next month. billing_day is capped at 28, so the day
// always exists. time.Date normalises month overflow (December + 1 = January).
func firstBillingDate(start time.Time, day int) time.Time {
	if start.Day() <= day {
		return time.Date(start.Year(), start.Month(), day, 0, 0, 0, 0, time.UTC)
	}
	return time.Date(start.Year(), start.Month()+1, day, 0, 0, 0, 0, time.UTC)
}

// nextBillingFrom: the next billing date on/after `from` for a billing day.
func nextBillingFrom(from time.Time, day int) time.Time {
	if from.Day() <= day {
		return time.Date(from.Year(), from.Month(), day, 0, 0, 0, 0, time.UTC)
	}
	return time.Date(from.Year(), from.Month()+1, day, 0, 0, 0, 0, time.UTC)
}

func dateStr(t time.Time) string { return t.Format("2006-01-02") }

// strPtrNil returns nil for an empty string, so an omitted optional value
// stores NULL instead of "".
func strPtrNil(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}

// projectExists: the codebase's usual "does this project exist" check.
func (s *Server) projectExists(ctx context.Context, id uuid.UUID) bool {
	var name string
	err := s.store.Pool.QueryRow(ctx, `SELECT name FROM projects WHERE id = $1`, id).Scan(&name)
	return err == nil
}

// ── Contracts ──────────────────────────────────────────────────

type contractInput struct {
	ClientID    string  `json:"client_id"`
	ProjectID   string  `json:"project_id"`
	Title       string  `json:"title"`
	Value       float64 `json:"value"`
	Currency    string  `json:"currency"`
	StartDate   string  `json:"start_date"`
	EndDate     string  `json:"end_date"`
	Terms       string  `json:"terms"`
	DocumentURL string  `json:"document_url"`
	Status      string  `json:"status"`
}

func (s *Server) listContracts(c fiber.Ctx) error {
	if !canViewB2B(currentUser(c)) {
		return errForbidden()
	}
	status := strings.TrimSpace(c.Query("status"))
	if status != "" && !contractStatuses[status] {
		return errBadRequest("Unknown contract status")
	}
	rows, err := s.store.Queries.ListContracts(c.Context(), status)
	if err != nil {
		return err
	}
	if rows == nil {
		rows = []db.ListContractsRow{}
	}
	return c.JSON(fiber.Map{"contracts": rows})
}

func (s *Server) createContract(c fiber.Ctx) error {
	u := currentUser(c)
	if !canManageB2B(u) {
		return errForbidden()
	}
	var in contractInput
	if err := decodeJSON(c, &in); err != nil {
		return err
	}
	clientID, err := uuid.Parse(strings.TrimSpace(in.ClientID))
	if err != nil {
		return errBadRequest("Invalid client id")
	}
	if _, err := s.store.Queries.GetClient(c.Context(), clientID); err != nil {
		return errNotFound("Client not found")
	}
	var projectID *uuid.UUID
	if pid := strings.TrimSpace(in.ProjectID); pid != "" {
		id, err := uuid.Parse(pid)
		if err != nil {
			return errBadRequest("Invalid project id")
		}
		if !s.projectExists(c.Context(), id) {
			return errNotFound("Project not found")
		}
		projectID = &id
	}
	in.Title = strings.TrimSpace(in.Title)
	if len(in.Title) < 1 || len(in.Title) > 300 {
		return errBadRequest("Title is required (max 300 characters)")
	}
	if in.Value < 0 {
		return errBadRequest("Value must be 0 or more")
	}
	currency := strings.ToUpper(strings.TrimSpace(in.Currency))
	if currency == "" {
		currency = "BDT"
	}
	if !validCurrency(currency) {
		return errBadRequest("Currency must be a 3-letter code like BDT or USD")
	}
	if !validMilestoneDate(in.StartDate) {
		return errBadRequest("Start date must be a date (YYYY-MM-DD)")
	}
	endDate := strings.TrimSpace(in.EndDate)
	if endDate != "" {
		if !validMilestoneDate(endDate) {
			return errBadRequest("End date must be a date (YYYY-MM-DD)")
		}
		if endDate < in.StartDate {
			return errBadRequest("End date must be on or after the start date")
		}
	}
	row, err := s.store.Queries.CreateContract(c.Context(), db.CreateContractParams{
		ClientID: clientID, ProjectID: projectID, Title: in.Title,
		Value: moneyStr(in.Value), Currency: currency,
		StartDate: in.StartDate, EndDate: strPtrNil(endDate),
		Terms: strings.TrimSpace(in.Terms), DocumentUrl: strPtr(strings.TrimSpace(in.DocumentURL)),
		CreatedBy: &u.ID,
	})
	if err != nil {
		return err
	}
	return c.Status(fiber.StatusCreated).JSON(fiber.Map{"id": row.ID, "contract_number": row.ContractNumber})
}

func (s *Server) getContract(c fiber.Ctx) error {
	if !canViewB2B(currentUser(c)) {
		return errForbidden()
	}
	id, err := paramUUID(c, "id")
	if err != nil {
		return err
	}
	row, err := s.store.Queries.GetContract(c.Context(), id)
	if err != nil {
		return errNotFound("Contract not found")
	}
	return c.JSON(fiber.Map{"contract": row})
}

func (s *Server) updateContract(c fiber.Ctx) error {
	u := currentUser(c)
	if !canManageB2B(u) {
		return errForbidden()
	}
	id, err := paramUUID(c, "id")
	if err != nil {
		return err
	}
	existing, err := s.store.Queries.GetContract(c.Context(), id)
	if err != nil {
		return errNotFound("Contract not found")
	}
	if existing.Status == "completed" || existing.Status == "terminated" || existing.Status == "expired" {
		return errBadRequest("A " + existing.Status + " contract cannot be edited (renew it instead)")
	}
	var in contractInput
	if err := decodeJSON(c, &in); err != nil {
		return err
	}
	in.Title = strings.TrimSpace(in.Title)
	if len(in.Title) < 1 || len(in.Title) > 300 {
		return errBadRequest("Title is required (max 300 characters)")
	}
	if in.Value < 0 {
		return errBadRequest("Value must be 0 or more")
	}
	currency := strings.ToUpper(strings.TrimSpace(in.Currency))
	if currency == "" {
		currency = "BDT"
	}
	if !validCurrency(currency) {
		return errBadRequest("Currency must be a 3-letter code like BDT or USD")
	}
	if !validMilestoneDate(in.StartDate) {
		return errBadRequest("Start date must be a date (YYYY-MM-DD)")
	}
	endDate := strings.TrimSpace(in.EndDate)
	if endDate != "" {
		if !validMilestoneDate(endDate) {
			return errBadRequest("End date must be a date (YYYY-MM-DD)")
		}
		if endDate < in.StartDate {
			return errBadRequest("End date must be on or after the start date")
		}
	}
	status := strings.TrimSpace(in.Status)
	if status == "" {
		status = existing.Status
	}
	if !contractStatuses[status] {
		return errBadRequest("Unknown contract status")
	}
	if !validContractTransition(existing.Status, status) {
		return errBadRequest(fmt.Sprintf("Contract cannot move from %s to %s", existing.Status, status))
	}
	signedAt := existing.SignedAt
	if status == "active" && signedAt == nil {
		now := time.Now().UTC()
		signedAt = &now
	}
	if err := s.store.Queries.UpdateContract(c.Context(), db.UpdateContractParams{
		Title: in.Title, Value: moneyStr(in.Value), Currency: currency,
		StartDate: in.StartDate, EndDate: strPtrNil(endDate), Terms: strings.TrimSpace(in.Terms),
		DocumentUrl: strPtr(strings.TrimSpace(in.DocumentURL)),
		Status: status, SignedAt: signedAt, ID: id,
	}); err != nil {
		return err
	}
	return c.JSON(fiber.Map{"ok": true})
}

type renewContractInput struct {
	StartDate string `json:"start_date"`
	EndDate   string `json:"end_date"`
}

// renewContract copies a contract into a fresh draft (new CTR number, new
// dates) and marks the old one completed — one transaction, so a renewal can
// never duplicate or lose the original.
func (s *Server) renewContract(c fiber.Ctx) error {
	u := currentUser(c)
	if !canManageB2B(u) {
		return errForbidden()
	}
	id, err := paramUUID(c, "id")
	if err != nil {
		return err
	}
	var in renewContractInput
	if err := decodeJSON(c, &in); err != nil {
		return err
	}
	if !validMilestoneDate(in.StartDate) {
		return errBadRequest("Start date must be a date (YYYY-MM-DD)")
	}
	endDate := strings.TrimSpace(in.EndDate)
	if endDate != "" {
		if !validMilestoneDate(endDate) {
			return errBadRequest("End date must be a date (YYYY-MM-DD)")
		}
		if endDate < in.StartDate {
			return errBadRequest("End date must be on or after the start date")
		}
	}
	ctx := c.Context()
	tx, err := s.store.Pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	q := db.New(tx)
	old, err := q.GetContract(ctx, id)
	if err != nil {
		return errNotFound("Contract not found")
	}
	if old.Status != "active" && old.Status != "completed" {
		return errBadRequest("Only an active or completed contract can be renewed")
	}
	row, err := q.CreateContract(ctx, db.CreateContractParams{
		ClientID: old.ClientID, ProjectID: old.ProjectID, Title: old.Title,
		Value: old.Value, Currency: old.Currency,
		StartDate: in.StartDate, EndDate: strPtrNil(endDate),
		Terms: old.Terms, DocumentUrl: old.DocumentUrl, CreatedBy: &u.ID,
	})
	if err != nil {
		return err
	}
	if err := q.SetContractStatus(ctx, db.SetContractStatusParams{ID: id, Status: "completed"}); err != nil {
		return err
	}
	if err := tx.Commit(ctx); err != nil {
		return err
	}
	return c.Status(fiber.StatusCreated).JSON(fiber.Map{"id": row.ID, "contract_number": row.ContractNumber})
}

// ── Retainers ──────────────────────────────────────────────────

type retainerInput struct {
	ClientID   string  `json:"client_id"`
	Title      string  `json:"title"`
	MonthlyFee float64 `json:"monthly_fee"`
	Currency   string  `json:"currency"`
	StartDate  string  `json:"start_date"`
	BillingDay int     `json:"billing_day"`
	AutoRenew  *bool   `json:"auto_renew"`
	Status     string  `json:"status"`
}

func (s *Server) listRetainers(c fiber.Ctx) error {
	if !canViewB2B(currentUser(c)) {
		return errForbidden()
	}
	status := strings.TrimSpace(c.Query("status"))
	if status != "" && !retainerStatuses[status] {
		return errBadRequest("Unknown retainer status")
	}
	rows, err := s.store.Queries.ListRetainers(c.Context(), status)
	if err != nil {
		return err
	}
	if rows == nil {
		rows = []db.ListRetainersRow{}
	}
	return c.JSON(fiber.Map{"retainers": rows})
}

func (s *Server) createRetainer(c fiber.Ctx) error {
	u := currentUser(c)
	if !canManageB2B(u) {
		return errForbidden()
	}
	var in retainerInput
	if err := decodeJSON(c, &in); err != nil {
		return err
	}
	clientID, err := uuid.Parse(strings.TrimSpace(in.ClientID))
	if err != nil {
		return errBadRequest("Invalid client id")
	}
	if _, err := s.store.Queries.GetClient(c.Context(), clientID); err != nil {
		return errNotFound("Client not found")
	}
	in.Title = strings.TrimSpace(in.Title)
	if len(in.Title) < 1 || len(in.Title) > 300 {
		return errBadRequest("Title is required (max 300 characters)")
	}
	if in.MonthlyFee <= 0 {
		return errBadRequest("Monthly fee must be greater than 0")
	}
	currency := strings.ToUpper(strings.TrimSpace(in.Currency))
	if currency == "" {
		currency = "BDT"
	}
	if !validCurrency(currency) {
		return errBadRequest("Currency must be a 3-letter code like BDT or USD")
	}
	if !validMilestoneDate(in.StartDate) {
		return errBadRequest("Start date must be a date (YYYY-MM-DD)")
	}
	if in.BillingDay < 1 || in.BillingDay > 28 {
		return errBadRequest("Billing day must be between 1 and 28")
	}
	autoRenew := true
	if in.AutoRenew != nil {
		autoRenew = *in.AutoRenew
	}
	start, _ := time.Parse("2006-01-02", in.StartDate)
	next := firstBillingDate(start, in.BillingDay)
	id, err := s.store.Queries.CreateRetainer(c.Context(), db.CreateRetainerParams{
		ClientID: clientID, Title: in.Title, MonthlyFee: moneyStr(in.MonthlyFee),
		Currency: currency, StartDate: in.StartDate, BillingDay: int32(in.BillingDay),
		AutoRenew: autoRenew, NextBillingDate: dateStr(next), CreatedBy: &u.ID,
	})
	if err != nil {
		return err
	}
	return c.Status(fiber.StatusCreated).JSON(fiber.Map{"id": id, "next_billing_date": dateStr(next)})
}

func (s *Server) getRetainer(c fiber.Ctx) error {
	if !canViewB2B(currentUser(c)) {
		return errForbidden()
	}
	id, err := paramUUID(c, "id")
	if err != nil {
		return err
	}
	row, err := s.store.Queries.GetRetainer(c.Context(), id)
	if err != nil {
		return errNotFound("Retainer not found")
	}
	return c.JSON(fiber.Map{"retainer": row})
}

func (s *Server) updateRetainer(c fiber.Ctx) error {
	u := currentUser(c)
	if !canManageB2B(u) {
		return errForbidden()
	}
	id, err := paramUUID(c, "id")
	if err != nil {
		return err
	}
	existing, err := s.store.Queries.GetRetainer(c.Context(), id)
	if err != nil {
		return errNotFound("Retainer not found")
	}
	var in retainerInput
	if err := decodeJSON(c, &in); err != nil {
		return err
	}
	in.Title = strings.TrimSpace(in.Title)
	if len(in.Title) < 1 || len(in.Title) > 300 {
		return errBadRequest("Title is required (max 300 characters)")
	}
	if in.MonthlyFee <= 0 {
		return errBadRequest("Monthly fee must be greater than 0")
	}
	currency := strings.ToUpper(strings.TrimSpace(in.Currency))
	if currency == "" {
		currency = "BDT"
	}
	if !validCurrency(currency) {
		return errBadRequest("Currency must be a 3-letter code like BDT or USD")
	}
	if in.BillingDay < 1 || in.BillingDay > 28 {
		return errBadRequest("Billing day must be between 1 and 28")
	}
	status := strings.TrimSpace(in.Status)
	if status == "" {
		status = existing.Status
	}
	if !retainerStatuses[status] {
		return errBadRequest("Unknown retainer status")
	}
	if !validRetainerTransition(existing.Status, status) {
		return errBadRequest(fmt.Sprintf("Retainer cannot move from %s to %s", existing.Status, status))
	}
	autoRenew := existing.AutoRenew
	if in.AutoRenew != nil {
		autoRenew = *in.AutoRenew
	}
	// A changed billing day re-times the next billing from today (the DB's
	// date, so it agrees with the daily job); anything else keeps it.
	nextBilling := strVal(existing.NextBillingDate)
	if int32(in.BillingDay) != existing.BillingDay && status == "active" {
		var today string
		if err := s.store.Pool.QueryRow(c.Context(), `SELECT CURRENT_DATE::text`).Scan(&today); err != nil {
			return err
		}
		t, _ := time.Parse("2006-01-02", today)
		nextBilling = dateStr(nextBillingFrom(t, in.BillingDay))
	}
	if err := s.store.Queries.UpdateRetainer(c.Context(), db.UpdateRetainerParams{
		Title: in.Title, MonthlyFee: moneyStr(in.MonthlyFee), Currency: currency,
		BillingDay: int32(in.BillingDay), Status: status, AutoRenew: autoRenew,
		NextBillingDate: nextBilling, ID: id,
	}); err != nil {
		return err
	}
	return c.JSON(fiber.Map{"ok": true})
}

func (s *Server) dueRetainers(c fiber.Ctx) error {
	if !canViewB2B(currentUser(c)) {
		return errForbidden()
	}
	rows, err := s.store.Queries.ListDueRetainers(c.Context())
	if err != nil {
		return err
	}
	if rows == nil {
		rows = []db.ListDueRetainersRow{}
	}
	return c.JSON(fiber.Map{"retainers": rows})
}

// ── Milestone team + SLA ───────────────────────────────────────

type milestoneTeamInput struct {
	TeamID           string `json:"team_id"`
	SlaResponseHours *int   `json:"sla_response_hours"`
}

// setMilestoneTeam assigns (or clears) a milestone's team and SLA. The
// milestone stays editable only while it is scheduled or invoiced, like any
// other milestone edit.
func (s *Server) setMilestoneTeam(c fiber.Ctx) error {
	u := currentUser(c)
	if !canAssignMilestoneTeam(u) {
		return errForbidden()
	}
	pid, err := paramUUID(c, "id")
	if err != nil {
		return err
	}
	mid, err := paramUUID(c, "mid")
	if err != nil {
		return err
	}
	var in milestoneTeamInput
	if err := decodeJSON(c, &in); err != nil {
		return err
	}
	m, err := s.store.Queries.GetMilestone(c.Context(), mid)
	if err != nil {
		return errNotFound("Milestone not found")
	}
	if m.ProjectID != pid {
		return errNotFound("Milestone not found")
	}
	if m.Status != "scheduled" && m.Status != "invoiced" {
		return errBadRequest("Only scheduled or invoiced milestones can change team")
	}
	teamID, err := s.milestoneTeam(c.Context(), in.TeamID)
	if err != nil {
		return err
	}
	var sla *int32
	if in.SlaResponseHours != nil {
		if *in.SlaResponseHours <= 0 {
			return errBadRequest("SLA response hours must be greater than 0")
		}
		h := int32(*in.SlaResponseHours)
		sla = &h
	}
	if err := s.store.Queries.SetMilestoneTeam(c.Context(), db.SetMilestoneTeamParams{
		TeamID: teamID, SlaResponseHours: sla, ID: mid, ProjectID: pid,
	}); err != nil {
		return err
	}
	return c.JSON(fiber.Map{"ok": true})
}

// ── B2B report ─────────────────────────────────────────────────

type b2bRevenueRow struct {
	ClientID    uuid.UUID `json:"client_id"`
	ClientCode  string    `json:"client_code"`
	CompanyName string    `json:"company_name"`
	RevenueBDT  float64   `json:"revenue_bdt"`
	PaidCount   int       `json:"paid_count"`
}

type b2bOutstandingRow struct {
	ClientID      uuid.UUID `json:"client_id"`
	ClientCode    string    `json:"client_code"`
	CompanyName   string    `json:"company_name"`
	OutstandingBDT float64  `json:"outstanding_bdt"`
	OpenCount     int       `json:"open_count"`
}

type b2bProfitRow struct {
	ClientID      uuid.UUID `json:"client_id"`
	ClientCode    string    `json:"client_code"`
	CompanyName   string    `json:"company_name"`
	AgreedBDT     float64   `json:"agreed_bdt"`
	ReceivedBDT   float64   `json:"received_bdt"`
	OutstandingBDT float64  `json:"outstanding_bdt"`
}

type b2bHealthRow struct {
	ClientID       uuid.UUID `json:"client_id"`
	ClientCode     string    `json:"client_code"`
	CompanyName    string    `json:"company_name"`
	Score          int       `json:"score"`
	OnTime         int       `json:"on_time"`
	ResponseRate   int       `json:"response_rate"`
	ActiveScore    int       `json:"active_score"`
	PaidTotal      int       `json:"paid_total"`
	OverdueCount   int       `json:"overdue_count"`
	ApprovalsAsked int       `json:"approvals_asked"`
	ActiveProjects int       `json:"active_projects"`
}

// b2bRate converts an amount to BDT at the rate on a day — the rate set for
// that day, else the latest rate ever for the currency, else 1 (BDT, or a
// currency with no rate on file yet). finance_rate() alone returns NULL when
// no rate was set, which would silently drop the money from the report.
const b2bRate = `COALESCE(finance_rate(%s, %s),
	(SELECT r.rate FROM finance_rates r WHERE r.currency = %s ORDER BY r.effective_from DESC LIMIT 1), 1)`

// b2bReport: revenue by client, outstanding payments, profitability and health
// scores, plus retainers due soon and contracts expiring soon for the B2B
// dashboard.
//
// Honest limits, written down so nobody misreads the numbers:
//   - The finance ledger has no project link, so revenue is the milestones'
//     paid amounts (the money actually received per client), not ledger rows.
//   - "Costs" are not tracked per project, so profitability is agreed price
//     vs received — the gap is what is still outstanding, not profit.
//   - Response rate counts task approvals the client answered; a client never
//     asked for approval scores 100 on that part.
func (s *Server) b2bReport(c fiber.Ctx) error {
	if !canViewB2BReport(currentUser(c)) {
		return errForbidden()
	}
	ctx := c.Context()

	revenue := []b2bRevenueRow{}
	rows, err := s.store.Pool.Query(ctx, fmt.Sprintf(`
		SELECT c.id, c.client_code, c.company_name,
		       COALESCE(SUM(m.paid_amount * %s), 0)::float8 AS revenue_bdt,
		       COUNT(*) FILTER (WHERE m.paid_amount > 0)::int AS paid_count
		FROM clients c
		JOIN project_clients pc ON pc.client_id = c.id
		JOIN payment_milestones m ON m.project_id = pc.project_id
		WHERE m.paid_amount > 0
		GROUP BY c.id
		ORDER BY revenue_bdt DESC
		LIMIT 10`,
		fmt.Sprintf(b2bRate, "m.currency", "COALESCE(m.paid_at::date, CURRENT_DATE)", "m.currency")))
	if err != nil {
		return err
	}
	for rows.Next() {
		var r b2bRevenueRow
		if err := rows.Scan(&r.ClientID, &r.ClientCode, &r.CompanyName, &r.RevenueBDT, &r.PaidCount); err != nil {
			rows.Close()
			return err
		}
		revenue = append(revenue, r)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return err
	}

	outstanding := []b2bOutstandingRow{}
	rows, err = s.store.Pool.Query(ctx, fmt.Sprintf(`
		SELECT c.id, c.client_code, c.company_name,
		       COALESCE(SUM((m.amount - m.paid_amount) * %s), 0)::float8 AS outstanding_bdt,
		       COUNT(*)::int AS open_count
		FROM clients c
		JOIN project_clients pc ON pc.client_id = c.id
		JOIN payment_milestones m ON m.project_id = pc.project_id
		WHERE m.status IN ('scheduled', 'invoiced', 'partially_paid', 'overdue')
		GROUP BY c.id
		ORDER BY outstanding_bdt DESC`,
		fmt.Sprintf(b2bRate, "m.currency", "m.due_date", "m.currency")))
	if err != nil {
		return err
	}
	for rows.Next() {
		var r b2bOutstandingRow
		if err := rows.Scan(&r.ClientID, &r.ClientCode, &r.CompanyName, &r.OutstandingBDT, &r.OpenCount); err != nil {
			rows.Close()
			return err
		}
		outstanding = append(outstanding, r)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return err
	}

	profit := []b2bProfitRow{}
	rows, err = s.store.Pool.Query(ctx, fmt.Sprintf(`
		WITH agreed AS (
			SELECT pc.client_id,
			       SUM(p.agreed_price * %s)::float8 AS agreed_bdt
			FROM project_clients pc
			JOIN projects p ON p.id = pc.project_id
			WHERE p.agreed_price IS NOT NULL
			GROUP BY pc.client_id
		),
		received AS (
			SELECT pc.client_id,
			       SUM(m.paid_amount * %s)::float8 AS received_bdt
			FROM project_clients pc
			JOIN payment_milestones m ON m.project_id = pc.project_id
			WHERE m.paid_amount > 0
			GROUP BY pc.client_id
		)
		SELECT c.id, c.client_code, c.company_name,
		       COALESCE(a.agreed_bdt, 0) AS agreed_bdt,
		       COALESCE(r.received_bdt, 0) AS received_bdt,
		       GREATEST(COALESCE(a.agreed_bdt, 0) - COALESCE(r.received_bdt, 0), 0) AS outstanding_bdt
		FROM clients c
		LEFT JOIN agreed a ON a.client_id = c.id
		LEFT JOIN received r ON r.client_id = c.id
		WHERE a.agreed_bdt IS NOT NULL OR r.received_bdt IS NOT NULL
		ORDER BY COALESCE(a.agreed_bdt, 0) DESC`,
		fmt.Sprintf(b2bRate, "p.currency", "CURRENT_DATE", "p.currency"),
		fmt.Sprintf(b2bRate, "m.currency", "COALESCE(m.paid_at::date, CURRENT_DATE)", "m.currency")))
	if err != nil {
		return err
	}
	for rows.Next() {
		var r b2bProfitRow
		if err := rows.Scan(&r.ClientID, &r.ClientCode, &r.CompanyName, &r.AgreedBDT, &r.ReceivedBDT, &r.OutstandingBDT); err != nil {
			rows.Close()
			return err
		}
		profit = append(profit, r)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return err
	}

	health := []b2bHealthRow{}
	rows, err = s.store.Pool.Query(ctx, `
		WITH pay AS (
			SELECT pc.client_id,
			       COUNT(*) FILTER (WHERE m.status = 'paid')::int AS paid_total,
			       COUNT(*) FILTER (WHERE m.status = 'paid' AND m.paid_at::date <= m.due_date)::int AS paid_on_time,
			       COUNT(*) FILTER (WHERE m.status = 'overdue')::int AS overdue_count
			FROM project_clients pc
			JOIN payment_milestones m ON m.project_id = pc.project_id
			GROUP BY pc.client_id
		),
		resp AS (
			SELECT pc.client_id,
			       COUNT(*)::int AS asked,
			       COUNT(*) FILTER (WHERE ta.status <> 'pending')::int AS answered
			FROM project_clients pc
			JOIN project_cards card ON card.project_id = pc.project_id
			JOIN task_approvals ta ON ta.card_id = card.id
			GROUP BY pc.client_id
		),
		act AS (
			SELECT pc.client_id, COUNT(*)::int AS active_count
			FROM project_clients pc
			JOIN projects p ON p.id = pc.project_id
			WHERE p.status NOT IN ('draft', 'cancelled', 'completed')
			GROUP BY pc.client_id
		)
		SELECT c.id, c.client_code, c.company_name,
		       CASE WHEN COALESCE(pay.paid_total, 0) = 0
		            THEN (CASE WHEN COALESCE(pay.overdue_count, 0) = 0 THEN 100 ELSE 0 END)
		            ELSE ROUND(100.0 * pay.paid_on_time / pay.paid_total)::int END AS on_time,
		       CASE WHEN COALESCE(resp.asked, 0) = 0 THEN 100
		            ELSE ROUND(100.0 * resp.answered / resp.asked)::int END AS response_rate,
		       CASE WHEN COALESCE(act.active_count, 0) >= 2 THEN 100
		            WHEN COALESCE(act.active_count, 0) = 1 THEN 70 ELSE 0 END AS active_score,
		       COALESCE(pay.paid_total, 0) AS paid_total,
		       COALESCE(pay.overdue_count, 0) AS overdue_count,
		       COALESCE(resp.asked, 0) AS approvals_asked,
		       COALESCE(act.active_count, 0) AS active_projects
		FROM clients c
		LEFT JOIN pay ON pay.client_id = c.id
		LEFT JOIN resp ON resp.client_id = c.id
		LEFT JOIN act ON act.client_id = c.id
		ORDER BY c.company_name`)
	if err != nil {
		return err
	}
	for rows.Next() {
		var r b2bHealthRow
		if err := rows.Scan(&r.ClientID, &r.ClientCode, &r.CompanyName, &r.OnTime, &r.ResponseRate,
			&r.ActiveScore, &r.PaidTotal, &r.OverdueCount, &r.ApprovalsAsked, &r.ActiveProjects); err != nil {
			rows.Close()
			return err
		}
		// On-time payment 50% + client response 25% + active projects 25%.
		r.Score = int(math.Round(0.5*float64(r.OnTime) + 0.25*float64(r.ResponseRate) + 0.25*float64(r.ActiveScore)))
		health = append(health, r)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return err
	}

	dueSoon, err := s.store.Queries.ListRetainersDueSoon(ctx, 7)
	if err != nil {
		return err
	}
	if dueSoon == nil {
		dueSoon = []db.ListRetainersDueSoonRow{}
	}
	expiring, err := s.store.Queries.ListExpiringContracts(ctx, 30)
	if err != nil {
		return err
	}
	if expiring == nil {
		expiring = []db.ListExpiringContractsRow{}
	}
	return c.JSON(fiber.Map{
		"revenue_by_client":  revenue,
		"outstanding":        outstanding,
		"profitability":      profit,
		"health":             health,
		"retainers_due":      dueSoon,
		"contracts_expiring": expiring,
	})
}

// notifyB2BSales sends a B2B notice to Sales (the team that runs contracts
// and retainers), the same way the milestone runner notifies Sales.
func (s *Server) notifyB2BSales(ctx context.Context, typ, title, body, link string) error {
	recipients, err := notify.Role(ctx, s.store.Queries, policy.Sales)
	if err != nil {
		return err
	}
	return notify.Send(ctx, s.store.Queries, recipients, uuid.Nil, notify.Notification{
		Type: typ, Title: title, Body: body, Link: link,
	})
}
