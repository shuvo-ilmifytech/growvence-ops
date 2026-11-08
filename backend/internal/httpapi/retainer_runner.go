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

// Retainer billing, daily job (Phase 5): when a retainer's billing date
// arrives, generate its monthly client invoice and tell Sales, then advance
// the next billing date by one month (or pause the retainer when auto-renew
// is off). Idempotent like the milestone job: every step re-checks the state
// it expects, so a re-run (or two API instances racing) can never bill twice.

// ProcessDueRetainers runs one pass of the daily retainer job. Called from
// the scheduled job in cmd/api (boot + every 24h, Redis-locked).
func ProcessDueRetainers(ctx context.Context, pool *pgxpool.Pool) error {
	q := db.New(pool)
	due, err := q.ListDueRetainers(ctx)
	if err != nil {
		return err
	}
	for _, r := range due {
		if err := billRetainer(ctx, pool, q, r); err != nil {
			// Log and continue with the next retainer.
			continue
		}
	}
	return nil
}

// billRetainer generates one monthly invoice for a due retainer, then
// advances the next billing date (or pauses when auto-renew is off).
// Sprint 1 (P0-4): claim-first — the conditional UPDATE runs inside one
// transaction BEFORE the invoice insert, with a pre-generated invoice id.
// Only the run that wins the advance (or the pause) inserts an invoice, so a
// re-run (or two API instances racing) can never leave an orphan invoice
// behind. The number comes from invoice_num_seq, not MAX+1.
func billRetainer(ctx context.Context, pool *pgxpool.Pool, q *db.Queries, r db.ListDueRetainersRow) error {
	amount := parseMoney(r.MonthlyFee)
	dueDate := strVal(r.NextBillingDate)

	tx, err := pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)

	invoiceID := uuid.New()
	var ct interface{ RowsAffected() int64 }
	if r.AutoRenew {
		ct, err = tx.Exec(ctx, `
			UPDATE retainers
			SET next_billing_date = (next_billing_date + interval '1 month')::date, updated_at = now()
			WHERE id = $1 AND status = 'active' AND next_billing_date = $2::date`,
			r.ID, dueDate)
	} else {
		ct, err = tx.Exec(ctx, `
			UPDATE retainers SET status = 'paused', updated_at = now()
			WHERE id = $1 AND status = 'active' AND next_billing_date = $2::date`,
			r.ID, dueDate)
	}
	if err != nil {
		return err
	}
	if ct.RowsAffected() == 0 {
		return nil // another run already billed this cycle; nothing to do
	}

	number, err := nextInvoiceNumber(ctx, tx)
	if err != nil {
		return err
	}
	itemsJSON, _ := json.Marshal([]invoiceItem{{
		Description: fmt.Sprintf("Retainer: %s — %s (%s)", r.Title, r.ClientName, dueDate),
		Quantity:    1,
		UnitPrice:   amount,
	}})
	_, err = tx.Exec(ctx, `
		INSERT INTO client_invoices (id, invoice_number, project_id, client_name, client_email, items,
		                      subtotal, tax_rate, tax_amount, total, currency, status, due_date, notes)
		VALUES ($1,$2,NULL,$3,$4,$5,$6,0,0,$6,$7,'sent',$8::date,$9)`,
		invoiceID, number, r.ClientName, strVal(r.ClientEmail), itemsJSON,
		moneyStr(amount), r.Currency, dueDate,
		fmt.Sprintf("Auto-generated for retainer: %s", r.Title),
	)
	if err != nil {
		return err
	}
	if err := tx.Commit(ctx); err != nil {
		return err
	}

	systemActor := uuid.Nil
	body := fmt.Sprintf("Retainer %q for %s — %.2f %s due %s. Invoice %s.",
		r.Title, r.ClientName, amount, r.Currency, dueDate, number)
	recipients, err := notify.Role(ctx, q, policy.Sales)
	if err != nil {
		return err
	}
	return notify.Send(ctx, q, recipients, systemActor, notify.Notification{
		Type: notify.RetainerInvoice, Title: "Retainer invoice " + number + ": " + r.Title,
		Body: body, Link: "/retainers",
	})
}
