package httpapi

import (
	"context"
	"fmt"
	"time"

	"github.com/solvinx/opsflow/internal/db"
)

// nextInvoiceNumber allocates the next client invoice number, INV-YYYY-XXXX,
// from the invoice_num_seq sequence (migration 00048). A sequence — unlike
// the old MAX(invoice_number)+1 scan — can never hand the same number to two
// concurrent callers, and a rolled-back transaction only leaves a harmless
// gap. Takes a db.DBTX so both plain-pool callers and in-transaction runners
// can use it.
func nextInvoiceNumber(ctx context.Context, q db.DBTX) (string, error) {
	var seq int64
	if err := q.QueryRow(ctx, `SELECT nextval('invoice_num_seq')`).Scan(&seq); err != nil {
		return "", err
	}
	return fmt.Sprintf("INV-%s-%04d", time.Now().Format("2006"), seq), nil
}
