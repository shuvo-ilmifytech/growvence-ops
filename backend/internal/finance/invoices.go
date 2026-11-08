package finance

// Expense invoices: every company expense starts here, so nobody books company money alone.
//
//	Office Admin raises one ("1 fan for the office, Tk 2,000")  -> pending
//	HR approves it                                              -> awaiting_ceo
//	the CEO approves it                                         -> approved, and it is posted
//	                                                               to the ledger as an expense
//
// HR raising one skips the HR step (they are the one vouching for it); the CEO's own is
// approved on the spot. Either reviewer can reject with a reason, and whoever raised it can
// withdraw it while HR still has it. The amount is never re-typed: the ledger line is copied
// from the invoice, so an expense can never be counted twice or quietly changed on the way.

import (
	"context"
	"fmt"
	"regexp"
	"slices"
	"strings"

	"github.com/google/uuid"

	"github.com/solvinx/opsflow/internal/apperr"
	"github.com/solvinx/opsflow/internal/db"
	"github.com/solvinx/opsflow/internal/notify"
	"github.com/solvinx/opsflow/internal/policy"
	"github.com/solvinx/opsflow/internal/store"
	"github.com/solvinx/opsflow/internal/validate"
)

// Invoice statuses.
const (
	Pending     = "pending"      // waiting for HR
	AwaitingCEO = "awaiting_ceo" // HR let it through, waiting for the CEO
	Approved    = "approved"     // in the ledger
	Rejected    = "rejected"
	Cancelled   = "cancelled" // withdrawn by whoever raised it
)

var openStatuses = []string{Pending, AwaitingCEO}

const invoicesLink = "/invoices"

// canRaise: the Office Admin, HR and the CEO.
func canRaise(u db.User) error {
	if !policy.Can(policy.Role(u.Role), policy.InvoiceRaise) {
		return apperr.Forbidden()
	}
	return nil
}

// reviewsInvoices reports whether the actor decides on other people's invoices, which is
// also who sees every invoice rather than only their own.
func reviewsInvoices(u db.User) bool {
	return policy.Can(policy.Role(u.Role), policy.InvoiceReview)
}

func isCEO(u db.User) bool { return policy.Role(u.Role) == policy.CEO }

// Ref is how an invoice is named to people: INV-0007.
func Ref(number int64) string { return fmt.Sprintf("INV-%04d", number) }

// ── Listing ──────────────────────────────────────────────────

type InvoiceFilter struct {
	Status string // one status, "open" for both waiting steps, or "" for all
	From   string
	To     string
	Mine   bool
	Q      string
}

// InvoiceOut is an invoice as the UI and the printed copy need it: its header, with the
// lines it covers.
type InvoiceOut struct {
	db.InvoiceDetail
	Items []db.ListInvoiceItemsRow `json:"items_list"`
}

type InvoiceList struct {
	Invoices []InvoiceOut `json:"invoices"`
	// Categories the actor may file a line under (active expense categories).
	Categories []db.ListFinanceCategoriesRow `json:"categories"`
	Waiting    InvoiceCounts                 `json:"waiting"`
	Today      string                        `json:"today"`
	// CanReview is true for HR and the CEO, who also see everyone else's invoices.
	CanReview bool `json:"can_review"`
}

type InvoiceCounts struct {
	PendingHR   int64 `json:"pending_hr"`
	AwaitingCEO int64 `json:"awaiting_ceo"`
	MineOpen    int64 `json:"mine_open"`
}

// Invoices lists what the actor may see: their own for an Office Admin, all of them for HR
// and the CEO, with the categories and counts the page needs in the same call.
func (s *Service) Invoices(ctx context.Context, actor db.User, f InvoiceFilter) (InvoiceList, error) {
	if err := canRaise(actor); err != nil {
		return InvoiceList{}, err
	}
	p := db.ListInvoicesParams{}
	v := validate.Errors{}
	switch {
	case f.Status == "open":
		p.Statuses = openStatuses
	case f.Status != "":
		v.OneOf("status", f.Status, Pending, AwaitingCEO, Approved, Rejected, Cancelled)
		p.Statuses = []string{f.Status}
	}
	for _, d := range []struct {
		field, value string
		into         **string
	}{{"from", f.From, &p.FromDay}, {"to", f.To, &p.ToDay}} {
		if d.value == "" {
			continue
		}
		if !validDate(d.value) {
			v.Add(d.field, "must be a date (YYYY-MM-DD)")
			continue
		}
		val := d.value
		*d.into = &val
	}
	if !v.OK() {
		return InvoiceList{}, apperr.Validation(v)
	}
	if f.Mine || !reviewsInvoices(actor) {
		p.RequestedBy = &actor.ID
	}
	if q := strings.TrimSpace(f.Q); q != "" {
		p.Q = &q
	}
	rows, err := s.st.ListInvoices(ctx, p)
	if err != nil {
		return InvoiceList{}, err
	}
	withItems, err := s.attachItems(ctx, s.st.Queries, rows)
	if err != nil {
		return InvoiceList{}, err
	}
	cats, err := s.expenseCategories(ctx)
	if err != nil {
		return InvoiceList{}, err
	}
	counts, err := s.st.CountInvoicesWaiting(ctx, actor.ID)
	if err != nil {
		return InvoiceList{}, err
	}
	out := InvoiceList{
		Invoices: withItems, Categories: cats, Today: s.today(ctx), CanReview: reviewsInvoices(actor),
		Waiting: InvoiceCounts{PendingHR: counts.PendingHr, AwaitingCEO: counts.AwaitingCeo, MineOpen: counts.MineOpen},
	}
	if !out.CanReview {
		// Nothing is waiting on an Office Admin: the other two counts are not theirs to see.
		out.Waiting.PendingHR, out.Waiting.AwaitingCEO = 0, 0
	}
	return out, nil
}

// expenseCategories are the active expense categories an invoice may be filed under.
func (s *Service) expenseCategories(ctx context.Context) ([]db.ListFinanceCategoriesRow, error) {
	all, err := s.st.ListFinanceCategories(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]db.ListFinanceCategoriesRow, 0, len(all))
	for _, c := range all {
		if c.Kind == "expense" && c.IsActive {
			out = append(out, c)
		}
	}
	return out, nil
}

// attachItems reads the lines of every invoice given, in one query.
func (s *Service) attachItems(ctx context.Context, q *db.Queries, rows []db.InvoiceDetail) ([]InvoiceOut, error) {
	out := make([]InvoiceOut, len(rows))
	ids := make([]uuid.UUID, len(rows))
	for i, r := range rows {
		out[i] = InvoiceOut{InvoiceDetail: r, Items: []db.ListInvoiceItemsRow{}}
		ids[i] = r.ID
	}
	if len(ids) == 0 {
		return out, nil
	}
	items, err := q.ListInvoiceItems(ctx, ids)
	if err != nil {
		return nil, err
	}
	at := map[uuid.UUID]int{}
	for i, r := range rows {
		at[r.ID] = i
	}
	for _, it := range items {
		if i, ok := at[it.InvoiceID]; ok {
			out[i].Items = append(out[i].Items, it)
		}
	}
	return out, nil
}

// loadInvoice reads one invoice with its lines, inside the caller's transaction.
func (s *Service) loadInvoice(ctx context.Context, q *db.Queries, id uuid.UUID) (InvoiceOut, error) {
	inv, err := q.GetInvoiceDetail(ctx, id)
	if err != nil {
		return InvoiceOut{}, err
	}
	rows, err := s.attachItems(ctx, q, []db.InvoiceDetail{inv})
	if err != nil {
		return InvoiceOut{}, err
	}
	return rows[0], nil
}

// Invoice is one invoice with its lines, if the actor may see it. This is what the printed
// copy is built from.
func (s *Service) Invoice(ctx context.Context, actor db.User, id uuid.UUID) (InvoiceOut, error) {
	if err := canRaise(actor); err != nil {
		return InvoiceOut{}, err
	}
	inv, err := s.st.GetInvoiceDetail(ctx, id)
	if store.IsNotFound(err) || (err == nil && inv.RequestedBy != actor.ID && !reviewsInvoices(actor)) {
		return InvoiceOut{}, apperr.NotFound("Invoice")
	} else if err != nil {
		return InvoiceOut{}, err
	}
	return s.loadInvoice(ctx, s.st.Queries, id)
}

// ── Raising and editing ──────────────────────────────────────

// maxItems is a sanity limit on one purchase, not a business rule.
const maxItems = 50

type InvoiceInput struct {
	Currency    string             `json:"currency"`
	RateToBdt   string             `json:"rate_to_bdt"` // empty: the rate set for the invoice's date
	ExpenseDate string             `json:"expense_date"`
	Description string             `json:"description"` // what the whole purchase is for
	Items       []InvoiceItemInput `json:"items"`
}

// InvoiceItemInput is one line: "Office chair, 4 × 1,000". The category is per line, so a
// purchase that mixes a fan with tea reaches the ledger under both headings.
type InvoiceItemInput struct {
	CategoryID  string `json:"category_id"`
	Description string `json:"description"`
	Quantity    string `json:"quantity"` // empty counts as 1
	UnitPrice   string `json:"unit_price"`
}

var quantityRe = regexp.MustCompile(`^\d{1,9}(\.\d{1,3})?$`)

// cleanInvoice validates an invoice and its lines, filling in the rate for its date. Line
// errors come back as "items.0", the same shape as a payroll line.
func (s *Service) cleanInvoice(ctx context.Context, q *db.Queries, in *InvoiceInput) error {
	in.Currency = strings.ToUpper(strings.TrimSpace(in.Currency))
	if in.Currency == "" {
		in.Currency = "BDT"
	}
	in.RateToBdt = cleanNumber(in.RateToBdt)
	in.Description = strings.TrimSpace(in.Description)

	v := validate.Errors{}
	if !currencyRe.MatchString(in.Currency) {
		v.Add("currency", "must be a 3-letter code like BDT or USD")
	}
	if !validDate(in.ExpenseDate) {
		v.Add("expense_date", "must be a date (YYYY-MM-DD)")
	}
	v.Len("description", in.Description, 1, 200)
	switch {
	case in.Currency == "BDT":
		in.RateToBdt = "1"
	case in.RateToBdt != "":
		if !positive(rateRe, in.RateToBdt) {
			v.Add("rate_to_bdt", "must be a positive number with at most 4 decimals")
		}
	case currencyRe.MatchString(in.Currency) && validDate(in.ExpenseDate):
		rate, err := q.FinanceRateOn(ctx, db.FinanceRateOnParams{Currency: in.Currency, OnDay: in.ExpenseDate})
		if err != nil {
			return err
		}
		if rate == "" {
			v.Add("rate_to_bdt", fmt.Sprintf("no %s rate is set for this date; enter the rate you paid", in.Currency))
		}
		in.RateToBdt = rate
	}

	switch {
	case len(in.Items) == 0:
		v.Add("items", "add at least one line")
	case len(in.Items) > maxItems:
		v.Add("items", fmt.Sprintf("at most %d lines", maxItems))
	}
	cats, err := s.expenseCategoryIDs(ctx, q)
	if err != nil {
		return err
	}
	for i := range in.Items {
		it := &in.Items[i]
		it.Description = strings.TrimSpace(it.Description)
		it.Quantity = cleanNumber(it.Quantity)
		it.UnitPrice = cleanNumber(it.UnitPrice)
		if it.Quantity == "" {
			it.Quantity = "1"
		}
		field := fmt.Sprintf("items.%d", i)
		catID, err := uuid.Parse(it.CategoryID)
		switch {
		case it.Description == "" || len([]rune(it.Description)) > 200:
			v.Add(field, "needs a description (up to 200 characters)")
		case err != nil || !cats[catID]:
			v.Add(field, "pick an active expense category")
		case !positive(quantityRe, it.Quantity):
			v.Add(field, "quantity must be a positive number with at most 3 decimals")
		case !positive(amountRe, it.UnitPrice):
			v.Add(field, "unit price must be a positive amount with at most 2 decimals")
		}
	}
	if !v.OK() {
		return apperr.Validation(v)
	}
	return nil
}

// expenseCategoryIDs is the set an invoice line may be filed under.
func (s *Service) expenseCategoryIDs(ctx context.Context, q *db.Queries) (map[uuid.UUID]bool, error) {
	rows, err := q.ListFinanceCategories(ctx)
	if err != nil {
		return nil, err
	}
	out := map[uuid.UUID]bool{}
	for _, c := range rows {
		if c.Kind == "expense" && c.IsActive {
			out[c.ID] = true
		}
	}
	return out, nil
}

// writeItems replaces an invoice's lines with the ones given, keeping their order.
func writeItems(ctx context.Context, q *db.Queries, id uuid.UUID, items []InvoiceItemInput) error {
	if err := q.DeleteInvoiceItems(ctx, id); err != nil {
		return err
	}
	for i, it := range items {
		catID, err := uuid.Parse(it.CategoryID)
		if err != nil {
			return err
		}
		if err := q.CreateInvoiceItem(ctx, db.CreateInvoiceItemParams{
			InvoiceID: id, Position: int32(i), CategoryID: catID,
			Description: it.Description, Quantity: it.Quantity, UnitPrice: it.UnitPrice,
		}); err != nil {
			return err
		}
	}
	return nil
}

// CreateInvoice raises one. Who raises it decides where it starts: an Office Admin's waits
// for HR, HR's waits for the CEO, and the CEO's is approved and posted at once.
func (s *Service) CreateInvoice(ctx context.Context, actor db.User, in InvoiceInput) (InvoiceOut, error) {
	if err := canRaise(actor); err != nil {
		return InvoiceOut{}, err
	}
	var out InvoiceOut
	err := s.st.Tx(ctx, func(q *db.Queries) error {
		if err := s.cleanInvoice(ctx, q, &in); err != nil {
			return err
		}
		p := db.CreateInvoiceParams{
			Currency: in.Currency, RateToBdt: in.RateToBdt, ExpenseDate: in.ExpenseDate,
			Description: in.Description, RequestedBy: actor.ID, Status: Pending,
		}
		switch {
		case isCEO(actor):
			p.Status, p.CeoReviewedBy = Approved, &actor.ID
		case reviewsInvoices(actor): // HR: their own approval is the first step
			p.Status, p.HrReviewedBy = AwaitingCEO, &actor.ID
		}
		row, err := q.CreateInvoice(ctx, p)
		if err != nil {
			return err
		}
		if err := writeItems(ctx, q, row.ID, in.Items); err != nil {
			return err
		}
		if p.Status == Approved {
			if err := s.post(ctx, q, actor, row.ID); err != nil {
				return err
			}
		}
		if out, err = s.loadInvoice(ctx, q, row.ID); err != nil {
			return err
		}
		if err := s.tellReviewers(ctx, q, actor, p.Status, out.InvoiceDetail, notify.InvoiceRaised,
			out.RequestedByName+" raised "+Ref(out.Number)); err != nil {
			return err
		}
		return store.LogActivity(ctx, q, store.Activity{
			Actor: &actor.ID, Action: "invoice.created", EntityType: "invoice", EntityID: &row.ID,
			Metadata: map[string]any{"number": row.Number, "status": p.Status, "items": len(in.Items)},
		})
	})
	return out, err
}

// UpdateInvoice corrects one before it is approved, lines and all: whoever raised it while
// HR still has it, and HR once it is theirs to hand on. Settled invoices are fixed.
func (s *Service) UpdateInvoice(ctx context.Context, actor db.User, id uuid.UUID, in InvoiceInput) (InvoiceOut, error) {
	if err := canRaise(actor); err != nil {
		return InvoiceOut{}, err
	}
	var out InvoiceOut
	err := s.st.Tx(ctx, func(q *db.Queries) error {
		inv, err := q.GetInvoiceForUpdate(ctx, id)
		if store.IsNotFound(err) {
			return apperr.NotFound("Invoice")
		} else if err != nil {
			return err
		}
		if inv.RequestedBy != actor.ID && !reviewsInvoices(actor) {
			return apperr.NotFound("Invoice")
		}
		switch {
		case !slices.Contains(openStatuses, inv.Status):
			return apperr.Conflict(Ref(inv.Number) + " is already " + statusWord(inv.Status))
		case inv.Status == AwaitingCEO && !reviewsInvoices(actor):
			return apperr.Conflict(Ref(inv.Number) + " is with the CEO; ask HR to change it")
		}
		if err := s.cleanInvoice(ctx, q, &in); err != nil {
			return err
		}
		if err := q.UpdateInvoice(ctx, db.UpdateInvoiceParams{
			ID: id, Currency: in.Currency, RateToBdt: in.RateToBdt,
			ExpenseDate: in.ExpenseDate, Description: in.Description,
		}); err != nil {
			return err
		}
		if err := writeItems(ctx, q, id, in.Items); err != nil {
			return err
		}
		if out, err = s.loadInvoice(ctx, q, id); err != nil {
			return err
		}
		return store.LogActivity(ctx, q, store.Activity{
			Actor: &actor.ID, Action: "invoice.updated", EntityType: "invoice", EntityID: &id,
			Metadata: map[string]any{"number": inv.Number, "status": inv.Status, "items": len(in.Items)},
		})
	})
	return out, err
}

// CancelInvoice withdraws one. Only the person who raised it, and only while it is still
// waiting: once the CEO has approved it, it is in the ledger and theirs to correct.
func (s *Service) CancelInvoice(ctx context.Context, actor db.User, id uuid.UUID) (InvoiceOut, error) {
	if err := canRaise(actor); err != nil {
		return InvoiceOut{}, err
	}
	var out InvoiceOut
	err := s.st.Tx(ctx, func(q *db.Queries) error {
		inv, err := q.GetInvoiceForUpdate(ctx, id)
		if store.IsNotFound(err) {
			return apperr.NotFound("Invoice")
		} else if err != nil {
			return err
		}
		switch {
		case inv.RequestedBy != actor.ID:
			if !reviewsInvoices(actor) {
				return apperr.NotFound("Invoice")
			}
			return apperr.Forbidden()
		case !slices.Contains(openStatuses, inv.Status):
			return apperr.Conflict(Ref(inv.Number) + " is already " + statusWord(inv.Status))
		}
		if err := q.CancelInvoice(ctx, id); err != nil {
			return err
		}
		if out, err = s.loadInvoice(ctx, q, id); err != nil {
			return err
		}
		if err := s.tellReviewers(ctx, q, actor, inv.Status, out.InvoiceDetail, notify.InvoiceWithdrawn,
			actor.Name+" withdrew "+Ref(out.Number)); err != nil {
			return err
		}
		return store.LogActivity(ctx, q, store.Activity{
			Actor: &actor.ID, Action: "invoice.cancelled", EntityType: "invoice", EntityID: &id,
			Metadata: map[string]any{"number": inv.Number, "stage": inv.Status},
		})
	})
	return out, err
}

// ── Deciding ─────────────────────────────────────────────────

// ReviewInvoice is one step's decision.
//   - Waiting for HR: HR approves (it moves on to the CEO) or rejects. The CEO may also decide
//     here, e.g. while HR is away, and that decision is final.
//   - Waiting for the CEO: only the CEO, and that is what posts it to the ledger.
//
// Nobody decides on an invoice they raised themselves, except where raising it already
// counted as their step.
func (s *Service) ReviewInvoice(ctx context.Context, actor db.User, id uuid.UUID, approve bool, note string) (InvoiceOut, error) {
	if !reviewsInvoices(actor) {
		return InvoiceOut{}, apperr.Forbidden()
	}
	note = strings.TrimSpace(note)
	v := validate.Errors{}
	v.MaxLen("note", note, 1000)
	if !approve && note == "" {
		v.Add("note", "is required when rejecting")
	}
	if !v.OK() {
		return InvoiceOut{}, apperr.Validation(v)
	}
	var out InvoiceOut
	err := s.st.Tx(ctx, func(q *db.Queries) error {
		inv, err := q.GetInvoiceForUpdate(ctx, id)
		if store.IsNotFound(err) {
			return apperr.NotFound("Invoice")
		} else if err != nil {
			return err
		}
		ceo := isCEO(actor)
		switch {
		case !slices.Contains(openStatuses, inv.Status):
			return apperr.Conflict(Ref(inv.Number) + " is already " + statusWord(inv.Status))
		case inv.RequestedBy == actor.ID:
			return apperr.Forbidden()
		case inv.Status == AwaitingCEO && !ceo:
			return apperr.Forbidden()
		}

		// HR letting it through: hand it to the CEO, nothing is posted yet.
		if approve && inv.Status == Pending && !ceo {
			if err := q.SetInvoiceHRReview(ctx, db.SetInvoiceHRReviewParams{
				ID: id, Status: AwaitingCEO, ReviewedBy: &actor.ID, Note: note,
			}); err != nil {
				return err
			}
			if out, err = s.loadInvoice(ctx, q, id); err != nil {
				return err
			}
			if err := notify.Send(ctx, q, notify.Recipients{inv.RequestedBy}, actor.ID, notify.Notification{
				Type:  notify.InvoiceApproved,
				Title: actor.Name + " approved " + Ref(inv.Number) + "; waiting for the CEO",
				Body:  withNote(out.Description, note), Link: invoicesLink,
			}); err != nil {
				return err
			}
			if err := s.tellRole(ctx, q, policy.CEO, actor, notify.Notification{
				Type:  notify.InvoiceRaised,
				Title: Ref(inv.Number) + " needs your approval",
				Body:  out.Description + "\nApproved by " + actor.Name, Link: invoicesLink, Important: true,
			}); err != nil {
				return err
			}
			return store.LogActivity(ctx, q, store.Activity{
				Actor: &actor.ID, Action: "invoice.hr_approved", EntityType: "invoice", EntityID: &id,
				Metadata: map[string]any{"number": inv.Number, "requested_by": inv.RequestedBy},
			})
		}

		status, typ := Rejected, notify.InvoiceRejected
		if approve {
			status, typ = Approved, notify.InvoiceApproved
		}
		// The deciding step is recorded against the reviewer's own seat.
		if ceo {
			err = q.SetInvoiceCEOReview(ctx, db.SetInvoiceCEOReviewParams{ID: id, Status: status, ReviewedBy: &actor.ID, Note: note})
		} else {
			err = q.SetInvoiceHRReview(ctx, db.SetInvoiceHRReviewParams{ID: id, Status: status, ReviewedBy: &actor.ID, Note: note})
		}
		if err != nil {
			return err
		}
		if status == Approved {
			if err := s.post(ctx, q, actor, id); err != nil {
				return err
			}
		}
		if out, err = s.loadInvoice(ctx, q, id); err != nil {
			return err
		}
		to := notify.Recipients{inv.RequestedBy}.AddPtr(out.HrReviewedBy) // keep HR in the loop
		if err := notify.Send(ctx, q, to, actor.ID, notify.Notification{
			Type:  typ,
			Title: actor.Name + " " + statusWord(status) + " " + Ref(inv.Number),
			Body:  withNote(out.Description, note), Link: invoicesLink,
		}); err != nil {
			return err
		}
		return store.LogActivity(ctx, q, store.Activity{
			Actor: &actor.ID, Action: "invoice." + status, EntityType: "invoice", EntityID: &id,
			Metadata: map[string]any{"number": inv.Number, "requested_by": inv.RequestedBy, "stage": inv.Status},
		})
	})
	return out, err
}

// post copies an approved invoice into the ledger: one line per category it covers, so a
// purchase that mixes a fan with tea counts under both headings. The amounts and the rate
// come from the invoice itself, so what is booked is exactly what was approved.
func (s *Service) post(ctx context.Context, q *db.Queries, actor db.User, id uuid.UUID) error {
	groups, err := q.InvoiceTotalsByCategory(ctx, id)
	if err != nil {
		return err
	}
	for _, g := range groups {
		entryID, err := q.PostInvoiceLine(ctx, db.PostInvoiceLineParams{
			InvoiceID: id, CategoryID: g.CategoryID, Amount: g.Amount,
			Description: ledgerLine(g.Lines), ActorID: actor.ID,
		})
		if err != nil {
			return err
		}
		if err := store.LogActivity(ctx, q, store.Activity{
			Actor: &actor.ID, Action: "finance_entry.created", EntityType: "finance_entry", EntityID: &entryID,
			Metadata: map[string]any{"invoice_id": id, "category_id": g.CategoryID},
		}); err != nil {
			return err
		}
	}
	return nil
}

// ledgerLine is what the ledger shows for one category of an invoice: the lines it covers
// there, within the 500 characters a ledger entry allows.
func ledgerLine(lines string) string {
	const max = 500
	if r := []rune(lines); len(r) > max {
		return string(r[:max-1]) + "…"
	}
	return lines
}

// ── Notifications ────────────────────────────────────────────

// tellReviewers tells whoever is (or was) waiting on the invoice at that stage. Nothing is
// sent for one that needs no decision, such as an invoice the CEO raised themselves.
func (s *Service) tellReviewers(ctx context.Context, q *db.Queries, actor db.User, stage string, inv db.InvoiceDetail, typ, title string) error {
	var role policy.Role
	switch stage {
	case Pending:
		role = policy.HR
	case AwaitingCEO:
		role = policy.CEO
	default:
		return nil
	}
	return s.tellRole(ctx, q, role, actor, notify.Notification{
		Type: typ, Title: title, Body: inv.Description, Link: invoicesLink, Important: typ == notify.InvoiceRaised,
	})
}

func (s *Service) tellRole(ctx context.Context, q *db.Queries, role policy.Role, actor db.User, n notify.Notification) error {
	to, err := notify.Role(ctx, q, role)
	if err != nil {
		return err
	}
	return notify.Send(ctx, q, to, actor.ID, n)
}

// ── Helpers ──────────────────────────────────────────────────

// statusWord is how a status reads in a sentence ("… is already withdrawn").
func statusWord(status string) string {
	if status == Cancelled {
		return "withdrawn"
	}
	return status
}

func withNote(summary, note string) string {
	if note == "" {
		return summary
	}
	return summary + "\n" + note
}
