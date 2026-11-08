package httpapi

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/gofiber/fiber/v3"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/solvinx/opsflow/internal/db"
	"github.com/solvinx/opsflow/internal/policy"
)

// Invoice generator: client invoices with line items and a status workflow
// (draft -> sent -> paid/overdue). sales/admin/ceo/hr can manage; TL can view.

type invoiceItem struct {
	Description string  `json:"description"`
	Quantity    float64 `json:"quantity"`
	UnitPrice   float64 `json:"unit_price"`
}

type invoiceOut struct {
	ID            uuid.UUID     `json:"id"`
	InvoiceNumber string        `json:"invoice_number"`
	ProjectID     *uuid.UUID    `json:"project_id"`
	ProjectName   *string       `json:"project_name"`
	ClientName    string        `json:"client_name"`
	ClientEmail   string        `json:"client_email"`
	Items         []invoiceItem `json:"items"`
	Subtotal      float64       `json:"subtotal"`
	TaxRate       float64       `json:"tax_rate"`
	TaxAmount     float64       `json:"tax_amount"`
	Total         float64       `json:"total"`
	Currency      string        `json:"currency"`
	Status        string        `json:"status"`
	DueDate       *string       `json:"due_date"`
	Notes         string        `json:"notes"`
	CreatedBy     *uuid.UUID    `json:"created_by"`
	CreatedByName *string       `json:"created_by_name"`
	CreatedAt     time.Time     `json:"created_at"`
	UpdatedAt     time.Time     `json:"updated_at"`
}

func canManageInvoices(u db.User) bool {
	r := roleOf(u)
	return r == policy.Sales || r == policy.HR || isSystemAdmin(u)
}

func canViewInvoices(u db.User) bool {
	return canManageInvoices(u) || roleOf(u) == policy.TL
}

func scanInvoiceRow(scanner interface {
	Scan(...any) error
}, inv *invoiceOut) error {
	var itemsJSON []byte
	var dueDate *string
	err := scanner.Scan(
		&inv.ID, &inv.InvoiceNumber, &inv.ProjectID, &inv.ProjectName,
		&inv.ClientName, &inv.ClientEmail, &itemsJSON,
		&inv.Subtotal, &inv.TaxRate, &inv.TaxAmount, &inv.Total,
		&inv.Currency, &inv.Status, &dueDate, &inv.Notes,
		&inv.CreatedBy, &inv.CreatedByName, &inv.CreatedAt, &inv.UpdatedAt,
	)
	if err != nil {
		return err
	}
	inv.Items = []invoiceItem{}
	if len(itemsJSON) > 0 {
		_ = json.Unmarshal(itemsJSON, &inv.Items)
	}
	inv.DueDate = dueDate
	return nil
}

const invoiceSelect = `
	SELECT i.id, i.invoice_number, i.project_id, p.name AS project_name,
	       i.client_name, i.client_email, i.items,
	       i.subtotal, i.tax_rate, i.tax_amount, i.total,
	       i.currency, i.status, i.due_date::text, i.notes,
	       i.created_by, u.name AS created_by_name, i.created_at, i.updated_at
	FROM client_invoices i
	LEFT JOIN projects p ON p.id = i.project_id
	LEFT JOIN users u ON u.id = i.created_by`

func (s *Server) listInvoices(c fiber.Ctx) error {
	u := currentUser(c)
	if !canViewInvoices(u) {
		return errForbidden()
	}
	status := c.Query("status", "")
	limit, _ := strconv.Atoi(c.Query("limit", "50"))
	offset, _ := strconv.Atoi(c.Query("offset", "0"))
	if limit < 1 || limit > 200 {
		limit = 50
	}
	if offset < 0 {
		offset = 0
	}
	rows, err := s.store.Pool.Query(c.Context(), invoiceSelect+`
		WHERE ($1 = '' OR i.status = $1)
		ORDER BY i.created_at DESC
		LIMIT $2 OFFSET $3`, status, limit, offset)
	if err != nil {
		return err
	}
	defer rows.Close()
	out := []invoiceOut{}
	for rows.Next() {
		var inv invoiceOut
		if err := scanInvoiceRow(rows, &inv); err != nil {
			return err
		}
		out = append(out, inv)
	}
	return c.JSON(fiber.Map{"invoices": out})
}

func (s *Server) getInvoice(c fiber.Ctx) error {
	u := currentUser(c)
	if !canViewInvoices(u) {
		return errForbidden()
	}
	id, err := uuid.Parse(c.Params("id"))
	if err != nil {
		return errBadRequest("Invalid id")
	}
	var inv invoiceOut
	err = scanInvoiceRow(s.store.Pool.QueryRow(c.Context(), invoiceSelect+` WHERE i.id = $1`, id), &inv)
	if err != nil {
		return errNotFound("Invoice not found")
	}
	return c.JSON(inv)
}

type invoiceInput struct {
	ProjectID   *string       `json:"project_id"`
	ClientName  string        `json:"client_name"`
	ClientEmail string        `json:"client_email"`
	Items       []invoiceItem `json:"items"`
	TaxRate     float64       `json:"tax_rate"`
	Currency    string        `json:"currency"`
	DueDate     *string       `json:"due_date"`
	Notes       string        `json:"notes"`
}

// validateInvoiceInput checks the money-critical fields of a client invoice
// (Sprint 1, P0-5) and returns the totals rounded to 2 decimals. Bad input
// is a 400 here, never a 500 from a database CHECK or a silent bad row.
func validateInvoiceInput(ctx context.Context, pool *pgxpool.Pool, in invoiceInput) (subtotal, taxAmount, total float64, projectID *uuid.UUID, dueDate *string, err error) {
	if len(in.Items) == 0 {
		return 0, 0, 0, nil, nil, errBadRequest("At least one line item is required")
	}
	for _, it := range in.Items {
		if it.Quantity <= 0 {
			return 0, 0, 0, nil, nil, errBadRequest("Line item quantity must be greater than 0")
		}
		if it.UnitPrice < 0 {
			return 0, 0, 0, nil, nil, errBadRequest("Line item unit price cannot be negative")
		}
	}
	if in.TaxRate < 0 || in.TaxRate > 100 {
		return 0, 0, 0, nil, nil, errBadRequest("Tax rate must be between 0 and 100")
	}
	if strings.TrimSpace(in.ClientName) == "" {
		return 0, 0, 0, nil, nil, errBadRequest("Client name is required")
	}
	if in.ProjectID != nil && *in.ProjectID != "" {
		pid, perr := uuid.Parse(*in.ProjectID)
		if perr != nil {
			return 0, 0, 0, nil, nil, errBadRequest("Invalid project_id")
		}
		var exists int
		if qerr := pool.QueryRow(ctx, `SELECT 1 FROM projects WHERE id = $1`, pid).Scan(&exists); qerr != nil {
			if errors.Is(qerr, sql.ErrNoRows) {
				return 0, 0, 0, nil, nil, errBadRequest("Project not found")
			}
			return 0, 0, 0, nil, nil, qerr
		}
		projectID = &pid
	}
	if in.DueDate != nil && *in.DueDate != "" {
		if !validMilestoneDate(*in.DueDate) {
			return 0, 0, 0, nil, nil, errBadRequest("Due date must be a date (YYYY-MM-DD)")
		}
		dueDate = in.DueDate
	}
	for _, it := range in.Items {
		subtotal += it.Quantity * it.UnitPrice
	}
	subtotal = round2(subtotal)
	taxAmount = round2(subtotal * in.TaxRate / 100)
	total = round2(subtotal + taxAmount)
	return subtotal, taxAmount, total, projectID, dueDate, nil
}

func (s *Server) createInvoice(c fiber.Ctx) error {
	u := currentUser(c)
	if !canManageInvoices(u) {
		return errForbidden()
	}
	var in invoiceInput
	if err := c.Bind().JSON(&in); err != nil {
		return errBadRequest("Invalid request body")
	}
	subtotal, taxAmount, total, projectID, dueDate, err := validateInvoiceInput(c.Context(), s.store.Pool, in)
	if err != nil {
		return err
	}
	currency := strings.ToUpper(strings.TrimSpace(in.Currency))
	if currency == "" {
		currency = "USD"
	}
	if !validCurrency(currency) {
		return errBadRequest("Currency must be a 3-letter code like BDT or USD")
	}
	itemsJSON, _ := json.Marshal(in.Items)

	number, err := nextInvoiceNumber(c.Context(), s.store.Pool)
	if err != nil {
		return err
	}

	var id uuid.UUID
	err = s.store.Pool.QueryRow(c.Context(), `
		INSERT INTO client_invoices (invoice_number, project_id, client_name, client_email, items,
		                      subtotal, tax_rate, tax_amount, total, currency, due_date, notes, created_by)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11::date,$12,$13)
		RETURNING id`,
		number, projectID, in.ClientName, in.ClientEmail, itemsJSON,
		moneyStr(subtotal), in.TaxRate, moneyStr(taxAmount), moneyStr(total), currency, dueDate, in.Notes, u.ID,
	).Scan(&id)
	if err != nil {
		return err
	}
	return c.Status(fiber.StatusCreated).JSON(fiber.Map{"id": id, "invoice_number": number})
}

func (s *Server) updateInvoice(c fiber.Ctx) error {
	u := currentUser(c)
	if !canManageInvoices(u) {
		return errForbidden()
	}
	id, err := uuid.Parse(c.Params("id"))
	if err != nil {
		return errBadRequest("Invalid id")
	}
	var in invoiceInput
	if err := c.Bind().JSON(&in); err != nil {
		return errBadRequest("Invalid request body")
	}
	subtotal, taxAmount, total, projectID, dueDate, err := validateInvoiceInput(c.Context(), s.store.Pool, in)
	if err != nil {
		return err
	}
	// An empty currency keeps the invoice's existing currency; a supplied
	// one must be a valid 3-letter code.
	currency := strings.ToUpper(strings.TrimSpace(in.Currency))
	if currency == "" {
		if err := s.store.Pool.QueryRow(c.Context(),
			`SELECT currency FROM client_invoices WHERE id = $1`, id).Scan(&currency); err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				return errBadRequest("Invoice not found or not editable (only drafts can be edited)")
			}
			return err
		}
	}
	if !validCurrency(currency) {
		return errBadRequest("Currency must be a 3-letter code like BDT or USD")
	}
	itemsJSON, _ := json.Marshal(in.Items)
	ct, err := s.store.Pool.Exec(c.Context(), `
		UPDATE client_invoices
		SET project_id=$2, client_name=$3, client_email=$4, items=$5,
		    subtotal=$6, tax_rate=$7, tax_amount=$8, total=$9, currency=$10,
		    due_date=$11::date, notes=$12, updated_at=now()
		WHERE id=$1 AND status = 'draft'`,
		id, projectID, in.ClientName, in.ClientEmail, itemsJSON,
		moneyStr(subtotal), in.TaxRate, moneyStr(taxAmount), moneyStr(total), currency, dueDate, in.Notes)
	if err != nil {
		return err
	}
	if ct.RowsAffected() == 0 {
		return errBadRequest("Invoice not found or not editable (only drafts can be edited)")
	}
	return c.JSON(fiber.Map{"ok": true})
}

func (s *Server) deleteInvoice(c fiber.Ctx) error {
	u := currentUser(c)
	if !canManageInvoices(u) {
		return errForbidden()
	}
	id, err := uuid.Parse(c.Params("id"))
	if err != nil {
		return errBadRequest("Invalid id")
	}
	ct, err := s.store.Pool.Exec(c.Context(), `DELETE FROM client_invoices WHERE id=$1 AND status='draft'`, id)
	if err != nil {
		return err
	}
	if ct.RowsAffected() == 0 {
		return errBadRequest("Invoice not found or not deletable (only drafts can be deleted)")
	}
	return c.JSON(fiber.Map{"ok": true})
}

func (s *Server) sendInvoice(c fiber.Ctx) error {
	u := currentUser(c)
	if !canManageInvoices(u) {
		return errForbidden()
	}
	id, err := uuid.Parse(c.Params("id"))
	if err != nil {
		return errBadRequest("Invalid id")
	}
	var number, clientName string
	var total float64
	err = s.store.Pool.QueryRow(c.Context(),
		`UPDATE client_invoices SET status='sent', updated_at=now() WHERE id=$1 AND status='draft'
		 RETURNING invoice_number, client_name, total`, id).Scan(&number, &clientName, &total)
	if err != nil {
		return errBadRequest("Invoice not found or already sent")
	}
	// Notify the team that an invoice went out.
	_, _ = s.store.Pool.Exec(c.Context(), `
		INSERT INTO notifications (recipient_id, type, title, body, data, is_important)
		SELECT id, 'invoice.sent', $2, $3, jsonb_build_object('link', '/billing'), false
		FROM users WHERE role IN ('sales','tl','hr','ceo') OR is_admin`,
		fmt.Sprintf("Invoice %s sent", number),
		fmt.Sprintf("%s — %s %.2f", clientName, "total", total))
	return c.JSON(fiber.Map{"ok": true, "invoice_number": number})
}

func (s *Server) markInvoicePaid(c fiber.Ctx) error {
	u := currentUser(c)
	if !canManageInvoices(u) {
		return errForbidden()
	}
	id, err := uuid.Parse(c.Params("id"))
	if err != nil {
		return errBadRequest("Invalid id")
	}
	ct, err := s.store.Pool.Exec(c.Context(),
		`UPDATE client_invoices SET status='paid', updated_at=now() WHERE id=$1 AND status IN ('sent','overdue')`, id)
	if err != nil {
		return err
	}
	if ct.RowsAffected() == 0 {
		return errBadRequest("Invoice not found or not payable")
	}
	return c.JSON(fiber.Map{"ok": true})
}
