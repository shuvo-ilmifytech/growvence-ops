package hr

import (
	"context"
	"regexp"
	"strconv"
	"strings"

	"github.com/google/uuid"

	"github.com/solvinx/opsflow/internal/apperr"
	"github.com/solvinx/opsflow/internal/db"
	"github.com/solvinx/opsflow/internal/store"
	"github.com/solvinx/opsflow/internal/validate"
)

// Salaries are seen and managed only by HR and the CEO, and never by the person they
// belong to (HR's salary is handled by the CEO, the CEO's by HR). Amounts are kept out of
// the activity log; the salary history itself is the record of changes.

// canManageSalary: HR or the CEO, for anyone but themselves. Anything else looks like
// "not found" so the endpoint doesn't confirm whose salary exists.
func canManageSalary(actor db.User, target uuid.UUID) error {
	if !seesAll(actor) || actor.ID == target {
		return apperr.NotFound("Salary")
	}
	return nil
}

func (s *Service) SalaryHistory(ctx context.Context, actor db.User, userID uuid.UUID) ([]db.SalaryHistoryRow, error) {
	if err := canManageSalary(actor, userID); err != nil {
		return nil, err
	}
	return s.st.SalaryHistory(ctx, userID)
}

// CurrentSalaries lists everyone's current salary except the actor's own.
func (s *Service) CurrentSalaries(ctx context.Context, actor db.User) ([]db.CurrentSalariesRow, error) {
	if !seesAll(actor) {
		return nil, apperr.Forbidden()
	}
	today := s.Schedule(ctx).Today(s.now())
	rows, err := s.st.CurrentSalaries(ctx, today)
	if err != nil {
		return nil, err
	}
	out := make([]db.CurrentSalariesRow, 0, len(rows))
	for _, r := range rows {
		if r.UserID != actor.ID {
			out = append(out, r)
		}
	}
	return out, nil
}

type SalaryInput struct {
	Amount        string `json:"amount"` // monthly gross, e.g. "45000" or "45000.50"
	Currency      string `json:"currency"`
	EffectiveFrom string `json:"effective_from"`
	Note          string `json:"note"`
}

var amountRe = regexp.MustCompile(`^\d{1,10}(\.\d{1,2})?$`)
var currencyRe = regexp.MustCompile(`^[A-Z]{3}$`)

func (s *Service) AddSalary(ctx context.Context, actor db.User, userID uuid.UUID, in SalaryInput) ([]db.SalaryHistoryRow, error) {
	if err := canManageSalary(actor, userID); err != nil {
		return nil, err
	}
	in.Amount = strings.ReplaceAll(strings.TrimSpace(in.Amount), ",", "")
	in.Currency = strings.ToUpper(strings.TrimSpace(in.Currency))
	if in.Currency == "" {
		in.Currency = "BDT"
	}
	in.Note = strings.TrimSpace(in.Note)
	v := validate.Errors{}
	if f, err := strconv.ParseFloat(in.Amount, 64); !amountRe.MatchString(in.Amount) || err != nil || f <= 0 {
		v.Add("amount", "must be a positive amount with at most 2 decimals")
	}
	if !currencyRe.MatchString(in.Currency) {
		v.Add("currency", "must be a 3-letter code like BDT or USD")
	}
	if _, err := ParseDate(in.EffectiveFrom); err != nil {
		v.Add("effective_from", "must be a date (YYYY-MM-DD)")
	}
	v.MaxLen("note", in.Note, 500)
	if !v.OK() {
		return nil, apperr.Validation(v)
	}
	err := s.st.Tx(ctx, func(q *db.Queries) error {
		if _, err := q.GetEmployee(ctx, userID); err != nil {
			return err
		}
		id, err := q.CreateSalary(ctx, db.CreateSalaryParams{
			UserID: userID, Amount: in.Amount, Currency: in.Currency, EffectiveFrom: in.EffectiveFrom, Note: in.Note, CreatedBy: actor.ID,
		})
		if err != nil {
			return err
		}
		return store.LogActivity(ctx, q, store.Activity{
			Actor: &actor.ID, Action: "salary.added", EntityType: "salary", EntityID: &id,
			Metadata: map[string]any{"user_id": userID, "effective_from": in.EffectiveFrom},
		})
	})
	switch {
	case store.IsNotFound(err):
		return nil, apperr.NotFound("Employee")
	case store.IsUniqueViolation(err):
		return nil, apperr.Validation(validate.Errors{"effective_from": "already has a salary entry; delete it first or pick another date"})
	case err != nil:
		return nil, err
	}
	return s.st.SalaryHistory(ctx, userID)
}

// DeleteSalary removes a mistaken entry.
func (s *Service) DeleteSalary(ctx context.Context, actor db.User, id uuid.UUID) error {
	return s.st.Tx(ctx, func(q *db.Queries) error {
		row, err := q.GetSalary(ctx, id)
		if store.IsNotFound(err) {
			return apperr.NotFound("Salary")
		} else if err != nil {
			return err
		}
		if err := canManageSalary(actor, row.UserID); err != nil {
			return err
		}
		if err := q.DeleteSalary(ctx, id); err != nil {
			return err
		}
		return store.LogActivity(ctx, q, store.Activity{
			Actor: &actor.ID, Action: "salary.deleted", EntityType: "salary", EntityID: &id,
			Metadata: map[string]any{"user_id": row.UserID, "effective_from": row.EffectiveFrom},
		})
	})
}
