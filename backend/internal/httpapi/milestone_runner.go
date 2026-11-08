package httpapi

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/solvinx/opsflow/internal/db"
	"github.com/solvinx/opsflow/internal/notify"
	"github.com/solvinx/opsflow/internal/policy"
)

// Milestone payments, daily job (Phase 2): when a milestone's due date arrives,
// generate its client invoice and tell the client + Sales; when an invoiced
// milestone goes past its due date, mark it overdue and tell everyone.
// Idempotent: every step re-checks the status it expects, so a re-run (or two
// API instances racing) can never duplicate an invoice or a notification.

// ProcessDueMilestones runs one pass of the daily milestone job. Called from
// the scheduled job in cmd/api (boot + every 24h, Redis-locked).
func ProcessDueMilestones(ctx context.Context, pool *pgxpool.Pool) error {
	q := db.New(pool)
	due, err := q.ListMilestonesDueForInvoice(ctx)
	if err != nil {
		return err
	}
	for _, m := range due {
		if err := invoiceDueMilestone(ctx, pool, q, m); err != nil {
			// Log and continue with the next milestone.
			continue
		}
	}
	overdue, err := q.ListMilestonesToMarkOverdue(ctx)
	if err != nil {
		return err
	}
	for _, m := range overdue {
		if err := markMilestoneOverdue(ctx, pool, q, m); err != nil {
			continue
		}
	}
	return nil
}

func strVal(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}

// invoiceDueMilestone generates the client invoice for one due milestone and
// flips it to 'invoiced'. Sprint 1 (P0-4): claim-first — the conditional
// UPDATE runs inside one transaction BEFORE the invoice insert, with a
// pre-generated invoice id. Only the run that wins the flip inserts an
// invoice, so a re-run (or two API instances racing) can never leave an
// orphan invoice behind. The number comes from invoice_num_seq, not MAX+1.
func invoiceDueMilestone(ctx context.Context, pool *pgxpool.Pool, q *db.Queries, m db.ListMilestonesDueForInvoiceRow) error {
	amount := parseMoney(m.Amount)
	projectName := strVal(m.ProjectName)
	// Who to bill: the project's client-portal user, if one is linked.
	var clientName, clientEmail string
	clients, err := q.ListProjectClientUsers(ctx, m.ProjectID)
	if err != nil {
		return err
	}
	if len(clients) > 0 {
		clientName = clients[0].Name
		clientEmail = clients[0].Email
	}
	if clientName == "" {
		clientName = projectName
	}

	tx, err := pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)

	invoiceID := uuid.New()
	ct, err := tx.Exec(ctx, `
		UPDATE payment_milestones
		SET invoice_id = $2, status = 'invoiced', updated_at = now()
		WHERE id = $1 AND status = 'scheduled' AND invoice_id IS NULL`,
		m.ID, invoiceID)
	if err != nil {
		return err
	}
	if ct.RowsAffected() == 0 {
		return nil // another run already invoiced it; nothing to do
	}

	number, err := nextInvoiceNumber(ctx, tx)
	if err != nil {
		return err
	}
	itemsJSON, _ := json.Marshal([]invoiceItem{{
		Description: fmt.Sprintf("Milestone: %s — %s", m.Label, projectName),
		Quantity:    1,
		UnitPrice:   amount,
	}})
	_, err = tx.Exec(ctx, `
		INSERT INTO client_invoices (id, invoice_number, project_id, client_name, client_email, items,
		                      subtotal, tax_rate, tax_amount, total, currency, status, due_date, notes)
		VALUES ($1,$2,$3,$4,$5,$6,$7,0,0,$7,$8,'sent',$9::date,$10)`,
		invoiceID, number, m.ProjectID, clientName, clientEmail, itemsJSON,
		moneyStr(amount), m.Currency, m.DueDate,
		fmt.Sprintf("Auto-generated for milestone: %s", m.Label),
	)
	if err != nil {
		return err
	}
	if err := tx.Commit(ctx); err != nil {
		return err
	}

	systemActor := uuid.Nil
	body := fmt.Sprintf("%s — %.2f %s due %s. Invoice %s.",
		m.Label, amount, m.Currency, m.DueDate, number)
	// The client sees it in their portal; Sales sees it on the payments board.
	_ = notifyMilestoneClientsTx(ctx, q, m.ProjectID, systemActor, notify.MilestoneInvoice,
		"Invoice "+number+": "+m.Label, body, "/client/"+m.ProjectID.String())
	_ = notifyMilestoneTeamTx(ctx, q, m.ProjectID, systemActor, notify.MilestoneInvoice,
		"Invoice "+number+" issued: "+m.Label, body, "/projects/"+m.ProjectID.String()+"/payments")
	return nil
}

// markMilestoneOverdue flips one invoiced, past-due milestone to 'overdue'.
func markMilestoneOverdue(ctx context.Context, pool *pgxpool.Pool, q *db.Queries, m db.ListMilestonesToMarkOverdueRow) error {
	ct, err := pool.Exec(ctx, `
		UPDATE payment_milestones SET status = 'overdue', updated_at = now()
		WHERE id = $1 AND status = 'invoiced'`, m.ID)
	if err != nil {
		return err
	}
	if ct.RowsAffected() == 0 {
		return nil
	}
	systemActor := uuid.Nil
	amount := parseMoney(m.Amount)
	body := fmt.Sprintf("%s — %.2f %s was due %s and is still unpaid.",
		m.Label, amount, m.Currency, m.DueDate)
	_ = notifyMilestoneClientsTx(ctx, q, m.ProjectID, systemActor, notify.MilestoneOverdue,
		"Payment overdue: "+m.Label, body, "/client/"+m.ProjectID.String())
	_ = notifyMilestoneTeamTx(ctx, q, m.ProjectID, systemActor, notify.MilestoneOverdue,
		"Milestone overdue: "+m.Label, body, "/projects/"+m.ProjectID.String()+"/payments")
	return nil
}

// notifyMilestoneClientsTx / notifyMilestoneTeamTx are the runner's versions of
// the Server helpers: same recipients, no Server receiver (the job has none).
func notifyMilestoneClientsTx(ctx context.Context, q *db.Queries, pid uuid.UUID, actor uuid.UUID, typ, title, body, link string) error {
	users, err := q.ListProjectClientUsers(ctx, pid)
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
	return notify.Send(ctx, q, recipients, actor, notify.Notification{
		Type: typ, ProjectID: &pid, Title: title, Body: body, Link: link,
	})
}

func notifyMilestoneTeamTx(ctx context.Context, q *db.Queries, pid uuid.UUID, actor uuid.UUID, typ, title, body, link string) error {
	recipients, err := notify.Role(ctx, q, policy.Sales)
	if err != nil {
		return err
	}
	return notify.Send(ctx, q, recipients, actor, notify.Notification{
		Type: typ, ProjectID: &pid, Title: title, Body: body, Link: link,
	})
}
