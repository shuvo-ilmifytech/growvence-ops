package httpapi

import (
	"context"
	"fmt"
	"log/slog"
	"strconv"
	"strings"
	"time"

	"github.com/gofiber/fiber/v3"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/solvinx/opsflow/internal/db"
	"github.com/solvinx/opsflow/internal/finance"
	"github.com/solvinx/opsflow/internal/notify"
	"github.com/solvinx/opsflow/internal/policy"
	"github.com/solvinx/opsflow/internal/store"
)

// Client milestone payments (Phase 2): a dated payment schedule per project.
// Sales/TL/HR/CEO/admin manage; members never see money. The CEO's writes are
// limited by readOnlyGuard exactly like billing (GETs pass, writes don't).

func canManageMilestones(u db.User) bool {
	r := roleOf(u)
	return r == policy.Sales || r == policy.TL || r == policy.HR || isSystemAdmin(u)
}

func canViewMilestones(u db.User) bool { return canManageMilestones(u) }

func validMilestoneDate(s string) bool {
	_, err := time.Parse("2006-01-02", s)
	return err == nil
}

func moneyStr(f float64) string { return strconv.FormatFloat(f, 'f', 2, 64) }

func parseMoney(s string) float64 {
	f, _ := strconv.ParseFloat(strings.TrimSpace(s), 64)
	return f
}

// numericFloat converts a pgx numeric column to float64 for comparisons.
func numericFloat(n pgtype.Numeric) float64 {
	f, err := n.Float64Value()
	if err != nil || !f.Valid {
		return 0
	}
	return f.Float64
}

type milestoneInput struct {
	Label            string  `json:"label"`
	Amount           float64 `json:"amount"`
	Currency         string  `json:"currency"`
	DueDate          string  `json:"due_date"`
	TeamID           string  `json:"team_id"`
	SlaResponseHours *int    `json:"sla_response_hours"`
}

// milestoneTeam validates an optional team id from a milestone input: empty
// means "no team", otherwise the team must exist.
func (s *Server) milestoneTeam(ctx context.Context, teamID string) (*uuid.UUID, error) {
	teamID = strings.TrimSpace(teamID)
	if teamID == "" {
		return nil, nil
	}
	id, err := uuid.Parse(teamID)
	if err != nil {
		return nil, errBadRequest("Invalid team id")
	}
	if _, err := s.store.Queries.GetTeam(ctx, id); err != nil {
		return nil, errNotFound("Team not found")
	}
	return &id, nil
}

// milestoneSLA validates an optional SLA response time in hours.
func milestoneSLA(in *milestoneInput) (*int32, error) {
	if in.SlaResponseHours == nil {
		return nil, nil
	}
	if *in.SlaResponseHours <= 0 {
		return nil, errBadRequest("SLA response hours must be greater than 0")
	}
	h := int32(*in.SlaResponseHours)
	return &h, nil
}

func (s *Server) milestoneProject(c fiber.Ctx, id uuid.UUID) (name string, value *float64, currency string, err error) {
	var agreed *string
	err = s.store.Pool.QueryRow(c.Context(),
		`SELECT name, agreed_price::text, currency FROM projects WHERE id = $1`, id,
	).Scan(&name, &agreed, &currency)
	if err != nil {
		return "", nil, "", errNotFound("Project not found")
	}
	if agreed != nil && *agreed != "" {
		v := parseMoney(*agreed)
		value = &v
	}
	return name, value, currency, nil
}

type milestoneSummary struct {
	Total         float64  `json:"total"`
	Paid          float64  `json:"paid"`
	Pending       float64  `json:"pending"`
	OverdueAmount float64  `json:"overdue_amount"`
	OverdueCount  int      `json:"overdue_count"`
	Count         int      `json:"count"`
	ProjectValue  *float64 `json:"project_value"`
	ProjectName   string   `json:"project_name"`
	ProjectCur    string   `json:"project_currency"`
}

// listMilestones returns a project's payment schedule with a money summary.
func (s *Server) listMilestones(c fiber.Ctx) error {
	u := currentUser(c)
	if !canViewMilestones(u) {
		return errForbidden()
	}
	pid, err := paramUUID(c, "id")
	if err != nil {
		return err
	}
	_, value, currency, err := s.milestoneProject(c, pid)
	if err != nil {
		return err
	}
	rows, err := s.store.Queries.ListMilestonesByProject(c.Context(), pid)
	if err != nil {
		return err
	}
	if rows == nil {
		rows = []db.ListMilestonesByProjectRow{}
	}
	sum := milestoneSummary{ProjectValue: value, ProjectCur: currency, Count: len(rows)}
	for _, r := range rows {
		amt := parseMoney(r.Amount)
		paid := parseMoney(r.PaidAmount)
		sum.Total += amt
		sum.Paid += paid
		if r.Status == "overdue" {
			sum.OverdueAmount += amt - paid
			sum.OverdueCount++
		}
	}
	sum.Pending = sum.Total - sum.Paid
	return c.JSON(fiber.Map{"milestones": rows, "summary": sum})
}

func (s *Server) createMilestone(c fiber.Ctx) error {
	u := currentUser(c)
	if !canManageMilestones(u) {
		return errForbidden()
	}
	pid, err := paramUUID(c, "id")
	if err != nil {
		return err
	}
	if _, _, _, err := s.milestoneProject(c, pid); err != nil {
		return err
	}
	var in milestoneInput
	if err := c.Bind().JSON(&in); err != nil {
		return errBadRequest("Invalid request body")
	}
	in.Label = strings.TrimSpace(in.Label)
	if len(in.Label) < 1 || len(in.Label) > 200 {
		return errBadRequest("Label is required (max 200 characters)")
	}
	if in.Amount <= 0 {
		return errBadRequest("Amount must be greater than 0")
	}
	currency := strings.ToUpper(strings.TrimSpace(in.Currency))
	if currency == "" {
		currency = "BDT"
	}
	if len(currency) != 3 {
		return errBadRequest("Currency must be a 3-letter code like BDT or USD")
	}
	if !validMilestoneDate(in.DueDate) {
		return errBadRequest("Due date must be a date (YYYY-MM-DD)")
	}
	teamID, err := s.milestoneTeam(c.Context(), in.TeamID)
	if err != nil {
		return err
	}
	sla, err := milestoneSLA(&in)
	if err != nil {
		return err
	}
	id, err := s.store.Queries.CreateMilestone(c.Context(), db.CreateMilestoneParams{
		ProjectID: pid, Label: in.Label, Amount: moneyStr(in.Amount),
		Currency: currency, DueDate: in.DueDate, TeamID: teamID,
		SlaResponseHours: sla, CreatedBy: &u.ID,
	})
	if err != nil {
		return err
	}
	return c.Status(fiber.StatusCreated).JSON(fiber.Map{"id": id})
}

func (s *Server) updateMilestone(c fiber.Ctx) error {
	u := currentUser(c)
	if !canManageMilestones(u) {
		return errForbidden()
	}
	if _, err := paramUUID(c, "id"); err != nil {
		return err
	}
	mid, err := paramUUID(c, "mid")
	if err != nil {
		return err
	}
	var in milestoneInput
	if err := c.Bind().JSON(&in); err != nil {
		return errBadRequest("Invalid request body")
	}
	in.Label = strings.TrimSpace(in.Label)
	if len(in.Label) < 1 || len(in.Label) > 200 {
		return errBadRequest("Label is required (max 200 characters)")
	}
	if in.Amount <= 0 {
		return errBadRequest("Amount must be greater than 0")
	}
	currency := strings.ToUpper(strings.TrimSpace(in.Currency))
	if currency == "" {
		currency = "BDT"
	}
	if len(currency) != 3 {
		return errBadRequest("Currency must be a 3-letter code like BDT or USD")
	}
	if !validMilestoneDate(in.DueDate) {
		return errBadRequest("Due date must be a date (YYYY-MM-DD)")
	}
	teamID, err := s.milestoneTeam(c.Context(), in.TeamID)
	if err != nil {
		return err
	}
	sla, err := milestoneSLA(&in)
	if err != nil {
		return err
	}
	ct, err := s.store.Pool.Exec(c.Context(), `
		UPDATE payment_milestones
		SET label = $2, amount = $3::numeric, currency = $4, due_date = $5::date,
		    team_id = $6, sla_response_hours = $7, updated_at = now()
		WHERE id = $1 AND status IN ('scheduled', 'invoiced')`,
		mid, in.Label, moneyStr(in.Amount), currency, in.DueDate, teamID, sla)
	if err != nil {
		return err
	}
	if ct.RowsAffected() == 0 {
		return errBadRequest("Milestone not found or not editable (only scheduled or invoiced milestones can be edited)")
	}
	return c.JSON(fiber.Map{"ok": true})
}

func (s *Server) deleteMilestone(c fiber.Ctx) error {
	u := currentUser(c)
	if !canManageMilestones(u) {
		return errForbidden()
	}
	if _, err := paramUUID(c, "id"); err != nil {
		return err
	}
	mid, err := paramUUID(c, "mid")
	if err != nil {
		return err
	}
	ct, err := s.store.Pool.Exec(c.Context(), `
		DELETE FROM payment_milestones
		WHERE id = $1 AND status IN ('scheduled', 'invoiced') AND invoice_id IS NULL`, mid)
	if err != nil {
		return err
	}
	if ct.RowsAffected() == 0 {
		return errBadRequest("Milestone not found or not deletable (paid milestones and invoiced ones cannot be deleted)")
	}
	return c.JSON(fiber.Map{"ok": true})
}

type recordPaymentInput struct {
	Amount    float64 `json:"amount"`
	Method    string  `json:"method"`
	Reference string  `json:"reference"`
	PaidAt    string  `json:"paid_at"`     // YYYY-MM-DD, default today
	RateToBdt string  `json:"rate_to_bdt"` // required for non-BDT when no rate is set for the date
	Note      string  `json:"note"`
}

// recordMilestonePayment books a payment (full or partial) against a milestone
// and posts the same amount to the finance ledger as income, in one
// transaction, so the ledger can never disagree with the schedule.
func (s *Server) recordMilestonePayment(c fiber.Ctx) error {
	u := currentUser(c)
	if !canManageMilestones(u) {
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
	var in recordPaymentInput
	if err := c.Bind().JSON(&in); err != nil {
		return errBadRequest("Invalid request body")
	}
	if in.Amount <= 0 {
		return errBadRequest("Amount must be greater than 0")
	}
	if !finance.ValidPaymentMethod(in.Method) {
		return errBadRequest("Unknown payment method")
	}
	paidAt := strings.TrimSpace(in.PaidAt)
	if paidAt == "" {
		paidAt = time.Now().UTC().Format("2006-01-02")
	}
	if !validMilestoneDate(paidAt) {
		return errBadRequest("Paid date must be a date (YYYY-MM-DD)")
	}

	ctx := c.Context()
	tx, err := s.store.Pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	q := db.New(tx)

	// Lock the milestone row: two simultaneous payments can't overpay it.
	var m db.PaymentMilestone
	err = tx.QueryRow(ctx, `
		SELECT id, project_id, label, amount, currency, due_date, status,
		       invoice_id, paid_amount, paid_at, payment_method, payment_reference,
		       created_by, created_at, updated_at
		FROM payment_milestones WHERE id = $1 FOR UPDATE`, mid).Scan(
		&m.ID, &m.ProjectID, &m.Label, &m.Amount, &m.Currency, &m.DueDate, &m.Status,
		&m.InvoiceID, &m.PaidAmount, &m.PaidAt, &m.PaymentMethod, &m.PaymentReference,
		&m.CreatedBy, &m.CreatedAt, &m.UpdatedAt)
	if err != nil {
		if err == pgx.ErrNoRows {
			return errNotFound("Milestone not found")
		}
		return err
	}
	if m.ProjectID != pid {
		return errNotFound("Milestone not found")
	}
	if m.Status == "paid" {
		return errBadRequest("Milestone is already fully paid")
	}
	amount := numericFloat(m.Amount)
	paid := numericFloat(m.PaidAmount)
	remaining := amount - paid
	if in.Amount > remaining+0.005 {
		return errBadRequest(fmt.Sprintf("Amount exceeds the remaining %.2f %s", remaining, m.Currency))
	}
	newPaid := paid + in.Amount
	status := "partially_paid"
	var paidAtTs *time.Time
	if newPaid >= amount-0.005 {
		status = "paid"
		t, _ := time.Parse("2006-01-02", paidAt)
		paidAtTs = &t
	}

	// The BDT rate: 1 for BDT, the body's rate, or the rate set for the date.
	rate := "1"
	if m.Currency != "BDT" {
		rate = strings.TrimSpace(in.RateToBdt)
		if rate == "" {
			rate, err = q.FinanceRateOn(ctx, db.FinanceRateOnParams{Currency: m.Currency, OnDay: paidAt})
			if err != nil {
				return err
			}
			if rate == "" {
				return errBadRequest(fmt.Sprintf("No %s rate is set for %s; enter the rate you received", m.Currency, paidAt))
			}
		} else if r, perr := strconv.ParseFloat(rate, 64); perr != nil || r <= 0 {
			return errBadRequest("Rate must be a positive number")
		}
	}

	_, err = tx.Exec(ctx, `
		UPDATE payment_milestones
		SET paid_amount = $2::numeric, status = $3, paid_at = $4,
		    payment_method = $5, payment_reference = $6, updated_at = now()
		WHERE id = $1`,
		mid, moneyStr(newPaid), status, paidAtTs, in.Method, strings.TrimSpace(in.Reference))
	if err != nil {
		return err
	}

	// Post to the ledger as income (same columns as CreateFinanceEntry).
	var catID uuid.UUID
	err = tx.QueryRow(ctx,
		`SELECT id FROM finance_categories WHERE kind = 'income' AND name = 'Direct client payment' AND is_active = true`).Scan(&catID)
	if err != nil {
		err = tx.QueryRow(ctx,
			`SELECT id FROM finance_categories WHERE kind = 'income' AND is_active = true ORDER BY name LIMIT 1`).Scan(&catID)
		if err != nil {
			return errBadRequest("No income category is configured; ask HR to add one")
		}
	}
	desc := fmt.Sprintf("Milestone payment: %s (%s)", m.Label, strings.TrimSpace(in.Note))
	if strings.TrimSpace(in.Note) == "" {
		desc = fmt.Sprintf("Milestone payment: %s", m.Label)
	}
	var entryID uuid.UUID
	err = tx.QueryRow(ctx, `
		INSERT INTO finance_entries (category_id, amount, currency, rate_to_bdt, entry_date, description, created_by, updated_by)
		VALUES ($1, $2::numeric, $3, $4::numeric, $5::date, $6, $7, $7)
		RETURNING id`,
		catID, moneyStr(in.Amount), m.Currency, rate, paidAt, desc, u.ID).Scan(&entryID)
	if err != nil {
		return err
	}
	_ = store.LogActivity(ctx, q, store.Activity{
		Actor: &u.ID, Action: "milestone.paid", EntityType: "payment_milestone", EntityID: &mid,
		Metadata: map[string]any{"amount": in.Amount, "currency": m.Currency, "method": in.Method, "finance_entry_id": entryID},
	})
	if err := tx.Commit(ctx); err != nil {
		return err
	}

	// Auto-distribute the profit: each active investor's share plus the
	// company reserve, by the shares in force today. A distribution failure
	// is logged, never allowed to fail the payment — the payment is the
	// source of truth, and the same income can be distributed manually later
	// (the UNIQUE constraint keeps it from ever paying twice).
	if rateF, perr := strconv.ParseFloat(rate, 64); perr == nil {
		if _, derr := distributeProfit(ctx, s.store.Pool, "milestone", entryID, in.Amount, m.Currency, rateF, "Milestone payment: "+m.Label); derr != nil {
			slog.Error("investor distribution failed", "err", derr, "milestone_id", mid, "entry_id", entryID)
		}
	}

	// Thank-you notice to the client + a heads-up to Sales (after commit).
	_ = s.notifyMilestoneClients(ctx, pid, u.ID, notify.MilestonePaid,
		"Payment received: "+m.Label,
		fmt.Sprintf("%.2f %s received via %s. Thank you!", in.Amount, m.Currency, in.Method),
		"/client")
	_ = s.notifyMilestoneTeam(ctx, pid, u.ID, notify.MilestonePaid,
		"Milestone paid: "+m.Label,
		fmt.Sprintf("%.2f %s via %s.", in.Amount, m.Currency, in.Method),
		"/payments")
	return c.JSON(fiber.Map{"ok": true, "status": status, "paid_amount": newPaid, "finance_entry_id": entryID})
}

type remindInput struct {
	Note string `json:"note"`
}

// remindMilestone sends a payment reminder to the client (and Sales), at most
// once per milestone per day per person.
func (s *Server) remindMilestone(c fiber.Ctx) error {
	u := currentUser(c)
	if !canManageMilestones(u) {
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
	var in remindInput
	_ = c.Bind().JSON(&in) // note is optional; an empty body is fine
	ctx := c.Context()
	var m db.PaymentMilestone
	err = s.store.Pool.QueryRow(ctx, `
		SELECT id, project_id, label, amount, currency, due_date, status,
		       invoice_id, paid_amount, paid_at, payment_method, payment_reference,
		       created_by, created_at, updated_at
		FROM payment_milestones WHERE id = $1`, mid).Scan(
		&m.ID, &m.ProjectID, &m.Label, &m.Amount, &m.Currency, &m.DueDate, &m.Status,
		&m.InvoiceID, &m.PaidAmount, &m.PaidAt, &m.PaymentMethod, &m.PaymentReference,
		&m.CreatedBy, &m.CreatedAt, &m.UpdatedAt)
	if err != nil {
		if err == pgx.ErrNoRows {
			return errNotFound("Milestone not found")
		}
		return err
	}
	if m.ProjectID != pid {
		return errNotFound("Milestone not found")
	}
	if m.Status == "paid" {
		return errBadRequest("Milestone is already paid")
	}
	n, err := s.store.Queries.CountManualRemindersToday(ctx, db.CountManualRemindersTodayParams{MilestoneID: mid, SentBy: &u.ID})
	if err != nil {
		return err
	}
	if n > 0 {
		return errTooMany("A reminder was already sent for this milestone today")
	}
	note := strings.TrimSpace(in.Note)
	_, err = s.store.Queries.CreateMilestoneReminder(ctx, db.CreateMilestoneReminderParams{
		MilestoneID: mid, SentBy: &u.ID, Channel: "manual", Note: note,
	})
	if err != nil {
		return err
	}
	amount := numericFloat(m.Amount)
	paid := numericFloat(m.PaidAmount)
	body := fmt.Sprintf("%s — %.2f %s due %s (%.2f %s remaining).",
		m.Label, amount, m.Currency, m.DueDate.Time.Format("2006-01-02"), amount-paid, m.Currency)
	if note != "" {
		body += " " + note
	}
	_ = s.notifyMilestoneClients(ctx, pid, u.ID, notify.MilestoneDue,
		"Payment reminder: "+m.Label, body, "/client")
	_ = s.notifyMilestoneTeam(ctx, pid, u.ID, notify.MilestoneDue,
		"Reminder sent: "+m.Label, body, "/payments")
	return c.JSON(fiber.Map{"ok": true})
}

// dueMilestones lists overdue milestones first, then those due within days.
func (s *Server) dueMilestones(c fiber.Ctx) error {
	u := currentUser(c)
	if !canViewMilestones(u) {
		return errForbidden()
	}
	days, _ := strconv.Atoi(c.Query("days", "7"))
	if days < 0 || days > 90 {
		days = 7
	}
	rows, err := s.store.Queries.ListDueMilestones(c.Context(), int32(days))
	if err != nil {
		return err
	}
	if rows == nil {
		rows = []db.ListDueMilestonesRow{}
	}
	return c.JSON(fiber.Map{"milestones": rows})
}

// getMilestone returns one milestone with its reminder history.
func (s *Server) getMilestone(c fiber.Ctx) error {
	u := currentUser(c)
	if !canViewMilestones(u) {
		return errForbidden()
	}
	mid, err := paramUUID(c, "mid")
	if err != nil {
		return err
	}
	m, err := s.store.Queries.GetMilestone(c.Context(), mid)
	if err != nil {
		return errNotFound("Milestone not found")
	}
	reminders, err := s.store.Queries.ListMilestoneReminders(c.Context(), mid)
	if err != nil {
		return err
	}
	if reminders == nil {
		reminders = []db.ListMilestoneRemindersRow{}
	}
	return c.JSON(fiber.Map{"milestone": m, "reminders": reminders})
}

// paymentMethods returns the canonical payment-method list.
func (s *Server) paymentMethods(c fiber.Ctx) error {
	return c.JSON(fiber.Map{"methods": finance.PaymentMethods})
}

// clientMilestones lists a project's milestones for its client-portal user.
func (s *Server) clientMilestones(c fiber.Ctx) error {
	u := currentUser(c)
	pid, err := paramUUID(c, "id")
	if err != nil {
		return err
	}
	if !s.clientHasAccess(c, u.ID, pid) {
		return errForbidden()
	}
	rows, err := s.store.Queries.ListMilestonesByProject(c.Context(), pid)
	if err != nil {
		return err
	}
	if rows == nil {
		rows = []db.ListMilestonesByProjectRow{}
	}
	return c.JSON(fiber.Map{"milestones": rows})
}

// notifyMilestoneClients sends a notification to every client-portal user
// linked to the project.
func (s *Server) notifyMilestoneClients(ctx context.Context, pid uuid.UUID, actor uuid.UUID, typ, title, body, link string) error {
	users, err := s.store.Queries.ListProjectClientUsers(ctx, pid)
	if err != nil {
		return err
	}
	recipients := notify.Recipients{}
	for _, cu := range users {
		recipients = recipients.Add(cu.ID)
	}
	if len(recipients) == 0 {
		return nil
	}
	return notify.Send(ctx, s.store.Queries, recipients, actor, notify.Notification{
		Type: typ, ProjectID: &pid, Title: title, Body: body, Link: link,
	})
}

// notifyMilestoneTeam notifies Sales (+ TL/admin/CEO via the role list) about
// a milestone event.
func (s *Server) notifyMilestoneTeam(ctx context.Context, pid uuid.UUID, actor uuid.UUID, typ, title, body, link string) error {
	recipients, err := notify.Role(ctx, s.store.Queries, policy.Sales)
	if err != nil {
		return err
	}
	return notify.Send(ctx, s.store.Queries, recipients, actor, notify.Notification{
		Type: typ, ProjectID: &pid, Title: title, Body: body, Link: link,
	})
}
