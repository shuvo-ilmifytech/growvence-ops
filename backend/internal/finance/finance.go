// Package finance is the company ledger and its analytics, for HR and the CEO only.
//
// Income and expenses are entered by hand, each converted to BDT at a rate kept on the
// entry. The monthly analytics add project earnings (completed projects net of the
// marketplace fee), which nobody enters, and payroll, the pay recorded month by month in
// payroll.go. Amounts never go in the activity log, because Team Leaders can read it.
package finance

import (
	"context"
	"encoding/csv"
	"fmt"
	"io"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/solvinx/opsflow/internal/apperr"
	"github.com/solvinx/opsflow/internal/db"
	"github.com/solvinx/opsflow/internal/hr"
	"github.com/solvinx/opsflow/internal/policy"
	"github.com/solvinx/opsflow/internal/reports"
	"github.com/solvinx/opsflow/internal/store"
	"github.com/solvinx/opsflow/internal/validate"
)

type Service struct {
	st  *store.Store
	hr  *hr.Service
	now func() time.Time
}

func NewService(st *store.Store, h *hr.Service) *Service {
	return &Service{st: st, hr: h, now: time.Now}
}

// allowed: HR and the CEO, the same people who manage salaries.
func allowed(u db.User) error {
	if !policy.ManagesPeople(policy.Role(u.Role)) {
		return apperr.Forbidden()
	}
	return nil
}

func (s *Service) today(ctx context.Context) string { return s.hr.Schedule(ctx).Today(s.now()) }

var (
	amountRe   = regexp.MustCompile(`^\d{1,12}(\.\d{1,2})?$`)
	rateRe     = regexp.MustCompile(`^\d{1,8}(\.\d{1,4})?$`)
	currencyRe = regexp.MustCompile(`^[A-Z]{3}$`)
	monthRe    = regexp.MustCompile(`^\d{4}-(0[1-9]|1[0-2])$`)
)

// cleanNumber drops thousands separators and spaces, so "1,20,000" and "120 000" work.
func cleanNumber(s string) string {
	return strings.NewReplacer(",", "", " ", "").Replace(strings.TrimSpace(s))
}

func positive(re *regexp.Regexp, s string) bool {
	f, err := strconv.ParseFloat(s, 64)
	return re.MatchString(s) && err == nil && f > 0
}

func validDate(s string) bool {
	_, err := hr.ParseDate(s)
	return err == nil
}

// ── Categories ───────────────────────────────────────────────

func (s *Service) Categories(ctx context.Context, actor db.User) ([]db.ListFinanceCategoriesRow, error) {
	if err := allowed(actor); err != nil {
		return nil, err
	}
	return s.st.ListFinanceCategories(ctx)
}

type CategoryInput struct {
	Name     *string `json:"name"`
	Kind     string  `json:"kind"`
	IsActive *bool   `json:"is_active"`
}

func (s *Service) CreateCategory(ctx context.Context, actor db.User, in CategoryInput) error {
	if err := allowed(actor); err != nil {
		return err
	}
	name := ""
	if in.Name != nil {
		name = strings.TrimSpace(*in.Name)
	}
	v := validate.Errors{}
	v.Len("name", name, 1, 80)
	v.OneOf("kind", in.Kind, "income", "expense")
	if !v.OK() {
		return apperr.Validation(v)
	}
	err := s.st.Tx(ctx, func(q *db.Queries) error {
		id, err := q.CreateFinanceCategory(ctx, db.CreateFinanceCategoryParams{Kind: in.Kind, Name: name})
		if err != nil {
			return err
		}
		return store.LogActivity(ctx, q, store.Activity{
			Actor: &actor.ID, Action: "finance_category.created", EntityType: "finance_category", EntityID: &id,
			Metadata: map[string]any{"name": name, "kind": in.Kind},
		})
	})
	if store.IsUniqueViolation(err) {
		return apperr.Validation(validate.Errors{"name": "already exists"})
	}
	return err
}

// UpdateCategory renames or (de)activates a category. Its kind never changes, since
// entries already filed under it would switch sides.
func (s *Service) UpdateCategory(ctx context.Context, actor db.User, id uuid.UUID, in CategoryInput) error {
	if err := allowed(actor); err != nil {
		return err
	}
	if in.Name != nil {
		n := strings.TrimSpace(*in.Name)
		in.Name = &n
		v := validate.Errors{}
		v.Len("name", n, 1, 80)
		if !v.OK() {
			return apperr.Validation(v)
		}
	}
	err := s.st.Tx(ctx, func(q *db.Queries) error {
		before, err := q.GetFinanceCategory(ctx, id)
		if err != nil {
			return err
		}
		if err := q.UpdateFinanceCategory(ctx, db.UpdateFinanceCategoryParams{ID: id, Name: in.Name, IsActive: in.IsActive}); err != nil {
			return err
		}
		meta := map[string]any{"name": before.Name}
		if in.Name != nil && *in.Name != before.Name {
			meta["renamed_to"] = *in.Name
		}
		if in.IsActive != nil {
			meta["is_active"] = *in.IsActive
		}
		return store.LogActivity(ctx, q, store.Activity{
			Actor: &actor.ID, Action: "finance_category.updated", EntityType: "finance_category", EntityID: &id, Metadata: meta,
		})
	})
	switch {
	case store.IsNotFound(err):
		return apperr.NotFound("Category")
	case store.IsUniqueViolation(err):
		return apperr.Validation(validate.Errors{"name": "already exists"})
	}
	return err
}

// ── Rates ────────────────────────────────────────────────────

func (s *Service) Rates(ctx context.Context, actor db.User) ([]db.ListFinanceRatesRow, error) {
	if err := allowed(actor); err != nil {
		return nil, err
	}
	return s.st.ListFinanceRates(ctx)
}

type RateInput struct {
	Currency      string `json:"currency"`
	Rate          string `json:"rate"` // BDT for one unit of the currency
	EffectiveFrom string `json:"effective_from"`
}

func (s *Service) AddRate(ctx context.Context, actor db.User, in RateInput) error {
	if err := allowed(actor); err != nil {
		return err
	}
	in.Currency = strings.ToUpper(strings.TrimSpace(in.Currency))
	in.Rate = cleanNumber(in.Rate)
	v := validate.Errors{}
	switch {
	case !currencyRe.MatchString(in.Currency):
		v.Add("currency", "must be a 3-letter code like USD")
	case in.Currency == "BDT":
		v.Add("currency", "BDT is always 1")
	}
	if !positive(rateRe, in.Rate) {
		v.Add("rate", "must be a positive number with at most 4 decimals")
	}
	if !validDate(in.EffectiveFrom) {
		v.Add("effective_from", "must be a date (YYYY-MM-DD)")
	}
	if !v.OK() {
		return apperr.Validation(v)
	}
	err := s.st.Tx(ctx, func(q *db.Queries) error {
		id, err := q.CreateFinanceRate(ctx, db.CreateFinanceRateParams{
			Currency: in.Currency, Rate: in.Rate, EffectiveFrom: in.EffectiveFrom, CreatedBy: actor.ID,
		})
		if err != nil {
			return err
		}
		return store.LogActivity(ctx, q, store.Activity{
			Actor: &actor.ID, Action: "finance_rate.added", EntityType: "finance_rate", EntityID: &id,
			Metadata: map[string]any{"currency": in.Currency, "effective_from": in.EffectiveFrom},
		})
	})
	if store.IsUniqueViolation(err) {
		return apperr.Validation(validate.Errors{"effective_from": "already has a rate for " + in.Currency + "; delete it first or pick another date"})
	}
	return err
}

// DeleteRate removes a mistaken rate. Entries keep the rate they were saved with.
func (s *Service) DeleteRate(ctx context.Context, actor db.User, id uuid.UUID) error {
	if err := allowed(actor); err != nil {
		return err
	}
	return s.st.Tx(ctx, func(q *db.Queries) error {
		r, err := q.GetFinanceRate(ctx, id)
		if store.IsNotFound(err) {
			return apperr.NotFound("Rate")
		} else if err != nil {
			return err
		}
		if err := q.DeleteFinanceRate(ctx, id); err != nil {
			return err
		}
		return store.LogActivity(ctx, q, store.Activity{
			Actor: &actor.ID, Action: "finance_rate.deleted", EntityType: "finance_rate", EntityID: &id,
			Metadata: map[string]any{"currency": r.Currency, "effective_from": r.EffectiveFrom},
		})
	})
}

// RateOn is the BDT rate for a currency on a day, "" when none is set. The invoice form
// needs it too, so whoever may raise an invoice may ask for it.
func (s *Service) RateOn(ctx context.Context, actor db.User, currency, day string) (string, error) {
	if err := canRaise(actor); err != nil {
		return "", err
	}
	currency = strings.ToUpper(strings.TrimSpace(currency))
	if !currencyRe.MatchString(currency) || !validDate(day) {
		return "", apperr.BadRequest("currency and date are required")
	}
	return s.st.FinanceRateOn(ctx, db.FinanceRateOnParams{Currency: currency, OnDay: day})
}

// ── Entries ──────────────────────────────────────────────────

type EntryFilter struct {
	From       string
	To         string
	Kind       string
	CategoryID string
	Q          string
}

// Entries lists the ledger for a date range (default: the current month).
func (s *Service) Entries(ctx context.Context, actor db.User, f EntryFilter) ([]db.ListFinanceEntriesRow, string, string, error) {
	if err := allowed(actor); err != nil {
		return nil, "", "", err
	}
	if f.From == "" || f.To == "" {
		t := s.today(ctx)
		first, _ := hr.ParseDate(t[:8] + "01")
		if f.From == "" {
			f.From = first.Format(time.DateOnly)
		}
		if f.To == "" {
			f.To = first.AddDate(0, 1, -1).Format(time.DateOnly)
		}
	}
	v := validate.Errors{}
	if !validDate(f.From) {
		v.Add("from", "must be a date (YYYY-MM-DD)")
	}
	if !validDate(f.To) {
		v.Add("to", "must be a date (YYYY-MM-DD)")
	}
	if f.Kind != "" {
		v.OneOf("kind", f.Kind, "income", "expense")
	}
	p := db.ListFinanceEntriesParams{FromDay: f.From, ToDay: f.To}
	if f.Kind != "" {
		p.Kind = &f.Kind
	}
	if f.CategoryID != "" {
		id, err := uuid.Parse(f.CategoryID)
		if err != nil {
			v.Add("category_id", "is not valid")
		}
		p.CategoryID = &id
	}
	if q := strings.TrimSpace(f.Q); q != "" {
		p.Q = &q
	}
	if !v.OK() {
		return nil, "", "", apperr.Validation(v)
	}
	rows, err := s.st.ListFinanceEntries(ctx, p)
	return rows, f.From, f.To, err
}

type EntryInput struct {
	CategoryID  string `json:"category_id"`
	Amount      string `json:"amount"`
	Currency    string `json:"currency"`
	RateToBdt   string `json:"rate_to_bdt"` // empty: the rate set for the entry's date
	EntryDate   string `json:"entry_date"`
	Description string `json:"description"`
}

// clean validates an entry against q, filling in the rate for its date when none is given.
func (s *Service) clean(ctx context.Context, q *db.Queries, in *EntryInput) (uuid.UUID, error) {
	in.Amount = cleanNumber(in.Amount)
	in.RateToBdt = cleanNumber(in.RateToBdt)
	in.Currency = strings.ToUpper(strings.TrimSpace(in.Currency))
	if in.Currency == "" {
		in.Currency = "BDT"
	}
	in.Description = strings.TrimSpace(in.Description)
	v := validate.Errors{}
	catID, err := uuid.Parse(in.CategoryID)
	if err != nil {
		v.Add("category_id", "pick a category")
	} else if cat, err := q.GetFinanceCategory(ctx, catID); store.IsNotFound(err) || (err == nil && !cat.IsActive) {
		v.Add("category_id", "pick an active category")
	} else if err != nil {
		return uuid.Nil, err
	}
	if !positive(amountRe, in.Amount) {
		v.Add("amount", "must be a positive amount with at most 2 decimals")
	}
	if !currencyRe.MatchString(in.Currency) {
		v.Add("currency", "must be a 3-letter code like BDT or USD")
	}
	if !validDate(in.EntryDate) {
		v.Add("entry_date", "must be a date (YYYY-MM-DD)")
	}
	v.Len("description", in.Description, 1, 500)
	switch {
	case in.Currency == "BDT":
		in.RateToBdt = "1"
	case in.RateToBdt != "":
		if !positive(rateRe, in.RateToBdt) {
			v.Add("rate_to_bdt", "must be a positive number with at most 4 decimals")
		}
	case currencyRe.MatchString(in.Currency) && validDate(in.EntryDate):
		rate, err := q.FinanceRateOn(ctx, db.FinanceRateOnParams{Currency: in.Currency, OnDay: in.EntryDate})
		if err != nil {
			return uuid.Nil, err
		}
		if rate == "" {
			v.Add("rate_to_bdt", fmt.Sprintf("no %s rate is set for this date; enter the rate you received", in.Currency))
		}
		in.RateToBdt = rate
	}
	if !v.OK() {
		return uuid.Nil, apperr.Validation(v)
	}
	return catID, nil
}

// CreateEntry adds an income line. Expenses are not entered here: every one of them is
// raised as an invoice and posted by the CEO's approval (invoices.go), so HR cannot book
// company money alone and no amount is ever typed twice.
func (s *Service) CreateEntry(ctx context.Context, actor db.User, in EntryInput) (uuid.UUID, error) {
	if err := allowed(actor); err != nil {
		return uuid.Nil, err
	}
	var id uuid.UUID
	err := s.st.Tx(ctx, func(q *db.Queries) error {
		catID, err := s.clean(ctx, q, &in)
		if err != nil {
			return err
		}
		if cat, err := q.GetFinanceCategory(ctx, catID); err != nil {
			return err
		} else if cat.Kind == "expense" {
			return apperr.Validation(validate.Errors{"category_id": "expenses are raised as an invoice, for the CEO to approve"})
		}
		id, err = q.CreateFinanceEntry(ctx, db.CreateFinanceEntryParams{
			CategoryID: catID, Amount: in.Amount, Currency: in.Currency, RateToBdt: in.RateToBdt,
			EntryDate: in.EntryDate, Description: in.Description, ActorID: actor.ID,
		})
		if err != nil {
			return err
		}
		return store.LogActivity(ctx, q, store.Activity{
			Actor: &actor.ID, Action: "finance_entry.created", EntityType: "finance_entry", EntityID: &id,
			Metadata: map[string]any{"entry_date": in.EntryDate, "category_id": catID},
		})
	})
	return id, err
}

func (s *Service) UpdateEntry(ctx context.Context, actor db.User, id uuid.UUID, in EntryInput) error {
	if err := allowed(actor); err != nil {
		return err
	}
	err := s.st.Tx(ctx, func(q *db.Queries) error {
		e, err := q.GetFinanceEntry(ctx, id)
		if err != nil {
			return err
		}
		if err := s.mayChangeEntry(actor, e); err != nil {
			return err
		}
		catID, err := s.clean(ctx, q, &in)
		if err != nil {
			return err
		}
		if cat, err := q.GetFinanceCategory(ctx, catID); err != nil {
			return err
		} else if cat.Kind != e.Kind {
			return apperr.Validation(validate.Errors{"category_id": "must stay " + e.Kind + "; delete the line instead"})
		}
		if err := q.UpdateFinanceEntry(ctx, db.UpdateFinanceEntryParams{
			ID: id, CategoryID: catID, Amount: in.Amount, Currency: in.Currency, RateToBdt: in.RateToBdt,
			EntryDate: in.EntryDate, Description: in.Description, ActorID: actor.ID,
		}); err != nil {
			return err
		}
		return store.LogActivity(ctx, q, store.Activity{
			Actor: &actor.ID, Action: "finance_entry.updated", EntityType: "finance_entry", EntityID: &id,
			Metadata: map[string]any{"entry_date": in.EntryDate, "category_id": catID},
		})
	})
	if store.IsNotFound(err) {
		return apperr.NotFound("Entry")
	}
	return err
}

// mayChangeEntry: a line the CEO approved as an invoice is the CEO's to correct. If HR could
// edit or delete it afterwards, approving it would mean nothing.
func (s *Service) mayChangeEntry(actor db.User, e db.GetFinanceEntryRow) error {
	if e.InvoiceID != nil && !isCEO(actor) {
		return apperr.Conflict("This expense came from " + Ref(e.InvoiceNumber) + ", which the CEO approved; only the CEO can change it")
	}
	return nil
}

func (s *Service) DeleteEntry(ctx context.Context, actor db.User, id uuid.UUID) error {
	if err := allowed(actor); err != nil {
		return err
	}
	return s.st.Tx(ctx, func(q *db.Queries) error {
		e, err := q.GetFinanceEntry(ctx, id)
		if store.IsNotFound(err) {
			return apperr.NotFound("Entry")
		} else if err != nil {
			return err
		}
		if err := s.mayChangeEntry(actor, e); err != nil {
			return err
		}
		if err := q.DeleteFinanceEntry(ctx, id); err != nil {
			return err
		}
		return store.LogActivity(ctx, q, store.Activity{
			Actor: &actor.ID, Action: "finance_entry.deleted", EntityType: "finance_entry", EntityID: &id,
			Metadata: map[string]any{"entry_date": e.EntryDate, "category_id": e.CategoryID},
		})
	})
}

// ── Analytics ────────────────────────────────────────────────

type Summary struct {
	From         string                    `json:"from"` // YYYY-MM
	To           string                    `json:"to"`
	CurrentMonth string                    `json:"current_month"`
	Months       []db.FinanceMonthlyRow    `json:"months"`
	Categories   []db.FinanceByCategoryRow `json:"categories"`
}

// Summary covers whole months from..to (YYYY-MM), default the last 12 including this one.
func (s *Service) Summary(ctx context.Context, actor db.User, from, to string) (Summary, error) {
	if err := allowed(actor); err != nil {
		return Summary{}, err
	}
	today := s.today(ctx)
	current := today[:7]
	if to == "" {
		to = current
	}
	v := validate.Errors{}
	if !monthRe.MatchString(to) {
		v.Add("to", "must be a month (YYYY-MM)")
	}
	if from == "" && v.OK() {
		t, _ := hr.ParseDate(to + "-01")
		from = t.AddDate(0, -11, 0).Format("2006-01")
	}
	if !monthRe.MatchString(from) {
		v.Add("from", "must be a month (YYYY-MM)")
	}
	if !v.OK() {
		return Summary{}, apperr.Validation(v)
	}
	start, _ := hr.ParseDate(from + "-01")
	end, _ := hr.ParseDate(to + "-01")
	switch {
	case end.Before(start):
		return Summary{}, apperr.Validation(validate.Errors{"from": "must not be after to"})
	case start.AddDate(0, 36, 0).Before(end.AddDate(0, 1, 0)):
		return Summary{}, apperr.Validation(validate.Errors{"from": "the range can be at most 36 months"})
	}
	count := (end.Year()-start.Year())*12 + int(end.Month()-start.Month()) + 1
	months, err := s.st.FinanceMonthly(ctx, db.FinanceMonthlyParams{FromMonth: from + "-01", MonthCount: int32(count)})
	if err != nil {
		return Summary{}, err
	}
	cats, err := s.st.FinanceByCategory(ctx, db.FinanceByCategoryParams{
		FromDay: from + "-01", ToDay: end.AddDate(0, 1, -1).Format(time.DateOnly),
	})
	if err != nil {
		return Summary{}, err
	}
	return Summary{From: from, To: to, CurrentMonth: current, Months: months, Categories: cats}, nil
}

// WriteEntriesCSV writes the ledger for a date range, filtered like the list.
func (s *Service) WriteEntriesCSV(ctx context.Context, w io.Writer, actor db.User, f EntryFilter) (string, string, error) {
	rows, from, to, err := s.Entries(ctx, actor, f)
	if err != nil {
		return "", "", err
	}
	cw := csv.NewWriter(w)
	_ = cw.Write([]string{"Date", "Type", "Category", "Description", "Amount", "Currency", "Rate to BDT", "Amount (BDT)", "Added by", "Last edited by"})
	for _, r := range rows {
		_ = cw.Write([]string{r.EntryDate, r.Kind, reports.Cell(r.CategoryName), reports.Cell(r.Description), r.Amount, r.Currency,
			r.RateToBdt, r.AmountBdt, reports.Cell(r.CreatedByName), reports.Cell(r.UpdatedByName)})
	}
	cw.Flush()
	return from, to, cw.Error()
}
