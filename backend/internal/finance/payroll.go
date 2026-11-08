package finance

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	"github.com/google/uuid"

	"github.com/solvinx/opsflow/internal/apperr"
	"github.com/solvinx/opsflow/internal/db"
	"github.com/solvinx/opsflow/internal/hr"
	"github.com/solvinx/opsflow/internal/store"
	"github.com/solvinx/opsflow/internal/validate"
)

// Payroll is entered by hand: for each month HR or the CEO records what a person was paid,
// starting from their fixed salary and adding bonuses and deductions (unpaid absence,
// lateness, advances, …). Only recorded pay counts as payroll in the analytics. As with
// salaries, nobody sees or records their own pay.

const maxAdjustments = 20

type Adjustment struct {
	Kind   string `json:"kind"` // bonus or deduction
	Reason string `json:"reason"`
	Amount string `json:"amount"`
}

// PayrollAttendance is the month's attendance, as a hint for deductions. Missing for people
// no longer active.
type PayrollAttendance struct {
	WorkingDays     int     `json:"working_days"`
	Absent          int     `json:"absent"`
	Late            int     `json:"late"`
	UnpaidLeaveDays float64 `json:"unpaid_leave_days"`
}

type PayrollPayment struct {
	db.ListPayrollPaymentsRow
	Adjustments []Adjustment `json:"adjustments"`
}

type PayrollRow struct {
	UserID         uuid.UUID          `json:"user_id"`
	Name           string             `json:"name"`
	IsActive       bool               `json:"is_active"`
	Designation    string             `json:"designation"`
	DepartmentName string             `json:"department_name"`
	SalaryAmount   *string            `json:"salary_amount"` // fixed monthly salary; nil when none is set
	SalaryCurrency *string            `json:"salary_currency"`
	Payment        *PayrollPayment    `json:"payment"` // nil until recorded
	Attendance     *PayrollAttendance `json:"attendance"`
}

type PayrollSheet struct {
	Month string       `json:"month"`
	Today string       `json:"today"`
	Rows  []PayrollRow `json:"rows"`
}

// Payroll lists everyone employed in a month (YYYY-MM, default this one) with a salary,
// plus anyone already paid for it, except the actor.
func (s *Service) Payroll(ctx context.Context, actor db.User, month string) (PayrollSheet, error) {
	if err := allowed(actor); err != nil {
		return PayrollSheet{}, err
	}
	today := s.today(ctx)
	if month == "" {
		month = today[:7]
	}
	if !monthRe.MatchString(month) {
		return PayrollSheet{}, apperr.Validation(validate.Errors{"month": "must be a month (YYYY-MM)"})
	}
	first, last, err := hr.MonthRange(month)
	if err != nil {
		return PayrollSheet{}, err
	}
	people, err := s.st.PayrollPeople(ctx, db.PayrollPeopleParams{MonthStart: first, MonthEnd: last})
	if err != nil {
		return PayrollSheet{}, err
	}
	payments, err := s.st.ListPayrollPayments(ctx, first)
	if err != nil {
		return PayrollSheet{}, err
	}
	adjustments, err := s.st.ListPayrollAdjustments(ctx, first)
	if err != nil {
		return PayrollSheet{}, err
	}
	attendance, err := s.hr.Attendance(ctx, actor, hr.SheetFilter{From: first, To: last})
	if err != nil {
		return PayrollSheet{}, err
	}

	adj := map[uuid.UUID][]Adjustment{}
	for _, a := range adjustments {
		adj[a.PaymentID] = append(adj[a.PaymentID], Adjustment{Kind: a.Kind, Reason: a.Reason, Amount: a.Amount})
	}
	att := map[uuid.UUID]*PayrollAttendance{}
	for _, p := range attendance.People {
		att[p.UserID] = &PayrollAttendance{
			WorkingDays: p.WorkingDays, Absent: p.Summary[hr.DayAbsent], Late: p.Summary[hr.DayLate], UnpaidLeaveDays: p.UnpaidLeaveDays,
		}
	}

	out := PayrollSheet{Month: month, Today: today, Rows: []PayrollRow{}}
	index := map[uuid.UUID]int{}
	for _, p := range people {
		if p.UserID == actor.ID {
			continue
		}
		index[p.UserID] = len(out.Rows)
		out.Rows = append(out.Rows, PayrollRow{
			UserID: p.UserID, Name: p.Name, IsActive: p.IsActive, Designation: p.Designation, DepartmentName: p.DepartmentName,
			SalaryAmount: &p.SalaryAmount, SalaryCurrency: &p.SalaryCurrency, Attendance: att[p.UserID],
		})
	}
	for _, p := range payments {
		if p.UserID == actor.ID {
			continue
		}
		pay := &PayrollPayment{ListPayrollPaymentsRow: p, Adjustments: adj[p.ID]}
		if pay.Adjustments == nil {
			pay.Adjustments = []Adjustment{}
		}
		if i, ok := index[p.UserID]; ok {
			out.Rows[i].Payment = pay
			continue
		}
		// Paid, though no longer on the month's list (their salary was removed, say).
		out.Rows = append(out.Rows, PayrollRow{UserID: p.UserID, Name: p.UserName, IsActive: p.IsActive, Payment: pay, Attendance: att[p.UserID]})
	}
	return out, nil
}

type PaymentInput struct {
	BaseAmount  string       `json:"base_amount"`
	Currency    string       `json:"currency"`
	RateToBdt   string       `json:"rate_to_bdt"` // empty: the rate set for the payment date
	PaidOn      string       `json:"paid_on"`
	Note        string       `json:"note"`
	Adjustments []Adjustment `json:"adjustments"`
}

// cents parses an amount already checked against amountRe.
func cents(s string) int64 {
	whole, frac, _ := strings.Cut(s, ".")
	n, _ := strconv.ParseInt(whole, 10, 64)
	f, _ := strconv.ParseInt((frac + "00")[:2], 10, 64)
	return n*100 + f
}

func fromCents(c int64) string { return fmt.Sprintf("%d.%02d", c/100, c%100) }

// cleanPayment validates a payment and works out its net amount, filling in the rate for
// the payment date when none is given.
func (s *Service) cleanPayment(ctx context.Context, q *db.Queries, in *PaymentInput) (string, error) {
	in.BaseAmount = cleanNumber(in.BaseAmount)
	in.RateToBdt = cleanNumber(in.RateToBdt)
	in.Currency = strings.ToUpper(strings.TrimSpace(in.Currency))
	if in.Currency == "" {
		in.Currency = "BDT"
	}
	in.Note = strings.TrimSpace(in.Note)
	v := validate.Errors{}
	base := int64(0)
	if !amountRe.MatchString(in.BaseAmount) {
		v.Add("base_amount", "must be an amount with at most 2 decimals")
	} else {
		base = cents(in.BaseAmount)
	}
	if !currencyRe.MatchString(in.Currency) {
		v.Add("currency", "must be a 3-letter code like BDT or USD")
	}
	if !validDate(in.PaidOn) {
		v.Add("paid_on", "must be a date (YYYY-MM-DD)")
	}
	v.MaxLen("note", in.Note, 500)
	if len(in.Adjustments) > maxAdjustments {
		v.Add("adjustments", fmt.Sprintf("at most %d lines", maxAdjustments))
	}
	net := base
	for i := range in.Adjustments {
		a := &in.Adjustments[i]
		a.Reason = strings.TrimSpace(a.Reason)
		a.Amount = cleanNumber(a.Amount)
		field := fmt.Sprintf("adjustments.%d", i)
		switch {
		case a.Kind != "bonus" && a.Kind != "deduction":
			v.Add(field, "must be a bonus or a deduction")
		case a.Reason == "" || len([]rune(a.Reason)) > 120:
			v.Add(field, "needs a reason (up to 120 characters)")
		case !positive(amountRe, a.Amount):
			v.Add(field, "must be a positive amount with at most 2 decimals")
		case a.Kind == "bonus":
			net += cents(a.Amount)
		default:
			net -= cents(a.Amount)
		}
	}
	if v.OK() && net < 0 {
		v.Add("adjustments", "deductions are more than the salary and bonuses")
	}
	switch {
	case in.Currency == "BDT":
		in.RateToBdt = "1"
	case in.RateToBdt != "":
		if !positive(rateRe, in.RateToBdt) {
			v.Add("rate_to_bdt", "must be a positive number with at most 4 decimals")
		}
	case currencyRe.MatchString(in.Currency) && validDate(in.PaidOn):
		rate, err := q.FinanceRateOn(ctx, db.FinanceRateOnParams{Currency: in.Currency, OnDay: in.PaidOn})
		if err != nil {
			return "", err
		}
		if rate == "" {
			v.Add("rate_to_bdt", fmt.Sprintf("no %s rate is set for this date; enter the rate you paid at", in.Currency))
		}
		in.RateToBdt = rate
	}
	if !v.OK() {
		return "", apperr.Validation(v)
	}
	return fromCents(net), nil
}

// SavePayment records (or replaces) one person's pay for a month, with its bonus and
// deduction lines.
func (s *Service) SavePayment(ctx context.Context, actor db.User, userID uuid.UUID, month string, in PaymentInput) error {
	if err := allowed(actor); err != nil {
		return err
	}
	if actor.ID == userID {
		return apperr.NotFound("Employee")
	}
	if !monthRe.MatchString(month) {
		return apperr.Validation(validate.Errors{"month": "must be a month (YYYY-MM)"})
	}
	first := month + "-01"
	return s.st.Tx(ctx, func(q *db.Queries) error {
		if _, err := q.GetUserByID(ctx, userID); store.IsNotFound(err) {
			return apperr.NotFound("Employee")
		} else if err != nil {
			return err
		}
		net, err := s.cleanPayment(ctx, q, &in)
		if err != nil {
			return err
		}
		action := "finance_payroll.updated"
		id, err := q.FindPayrollPayment(ctx, db.FindPayrollPaymentParams{UserID: userID, Month: first})
		switch {
		case store.IsNotFound(err):
			action = "finance_payroll.recorded"
			id, err = q.CreatePayrollPayment(ctx, db.CreatePayrollPaymentParams{
				UserID: userID, Month: first, BaseAmount: in.BaseAmount, Currency: in.Currency, RateToBdt: in.RateToBdt,
				NetAmount: net, PaidOn: in.PaidOn, Note: in.Note, ActorID: actor.ID,
			})
			if err != nil {
				return err
			}
		case err != nil:
			return err
		default:
			if err := q.UpdatePayrollPayment(ctx, db.UpdatePayrollPaymentParams{
				ID: id, BaseAmount: in.BaseAmount, Currency: in.Currency, RateToBdt: in.RateToBdt,
				NetAmount: net, PaidOn: in.PaidOn, Note: in.Note, ActorID: actor.ID,
			}); err != nil {
				return err
			}
			if err := q.DeletePayrollAdjustments(ctx, id); err != nil {
				return err
			}
		}
		for i, a := range in.Adjustments {
			if err := q.CreatePayrollAdjustment(ctx, db.CreatePayrollAdjustmentParams{
				PaymentID: id, Kind: a.Kind, Reason: a.Reason, Amount: a.Amount, Position: int32(i),
			}); err != nil {
				return err
			}
		}
		return store.LogActivity(ctx, q, store.Activity{
			Actor: &actor.ID, Action: action, EntityType: "finance_payroll", EntityID: &id,
			Metadata: map[string]any{"user_id": userID, "month": month},
		})
	})
}

func (s *Service) DeletePayment(ctx context.Context, actor db.User, id uuid.UUID) error {
	if err := allowed(actor); err != nil {
		return err
	}
	return s.st.Tx(ctx, func(q *db.Queries) error {
		p, err := q.GetPayrollPayment(ctx, id)
		if store.IsNotFound(err) || (err == nil && p.UserID == actor.ID) {
			return apperr.NotFound("Payment")
		} else if err != nil {
			return err
		}
		if err := q.DeletePayrollPayment(ctx, id); err != nil {
			return err
		}
		return store.LogActivity(ctx, q, store.Activity{
			Actor: &actor.ID, Action: "finance_payroll.deleted", EntityType: "finance_payroll", EntityID: &id,
			Metadata: map[string]any{"user_id": p.UserID, "month": p.Month},
		})
	})
}
