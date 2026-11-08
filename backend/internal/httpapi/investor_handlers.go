package httpapi

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/gofiber/fiber/v3"
	"github.com/google/uuid"

	"github.com/solvinx/opsflow/internal/db"
	"github.com/solvinx/opsflow/internal/finance"
	"github.com/solvinx/opsflow/internal/policy"
)

// Investor Finance Forum (Phase 3): capital partners, their shares, profit
// distributions, withdrawals and investor-tracked expenses.
//
// Who may do what:
//   - CEO, HR and system admins manage investors, shares, expenses and the
//     company account. Only the CEO sets shares and the reserve percent and
//     approves/rejects withdrawals.
//   - An investor (role "investor") sees ONLY their own profile via
//     /api/investor/me and their own detail page. Every investor endpoint
//     enforces that server-side; hiding UI controls is never sufficient.

func canManageInvestors(u db.User) bool {
	r := roleOf(u)
	return r == policy.CEO || r == policy.HR || isSystemAdmin(u)
}

func canViewInvestors(u db.User) bool { return canManageInvestors(u) }

func requireInvestor(c fiber.Ctx) error {
	if roleOf(currentUser(c)) == policy.Investor {
		return c.Next()
	}
	return errForbidden()
}

// investorSelf loads the investor record linked to a login, if any.
func (s *Server) investorSelf(ctx context.Context, userID uuid.UUID) (db.GetInvestorByUserIDRow, bool) {
	inv, err := s.store.Queries.GetInvestorByUserID(ctx, &userID)
	if err != nil {
		return db.GetInvestorByUserIDRow{}, false
	}
	return inv, true
}

// investorRate resolves the BDT rate for a non-BDT amount: the body's rate
// wins, otherwise the rate set for the date, otherwise a clear error.
func (s *Server) investorRate(ctx context.Context, currency, onDay, bodyRate string) (string, error) {
	if currency == "BDT" {
		return "1", nil
	}
	rate := strings.TrimSpace(bodyRate)
	if rate != "" {
		if r, err := strconv.ParseFloat(rate, 64); err != nil || r <= 0 {
			return "", errBadRequest("Rate must be a positive number")
		}
		return rate, nil
	}
	rate, err := s.store.Queries.FinanceRateOn(ctx, db.FinanceRateOnParams{Currency: currency, OnDay: onDay})
	if err != nil {
		return "", err
	}
	if rate == "" {
		return "", errBadRequest(fmt.Sprintf("No %s rate is set for %s; enter the rate you received", currency, onDay))
	}
	return rate, nil
}

func validCurrency(s string) bool {
	s = strings.ToUpper(strings.TrimSpace(s))
	return len(s) == 3
}

var investorExpenseCategories = map[string]bool{
	"Tools": true, "Software": true, "Office": true, "Marketing": true, "Other": true,
}

type investorInput struct {
	Name     string `json:"name"`
	Email    string `json:"email"`
	Phone    string `json:"phone"`
	JoinDate string `json:"join_date"`
	Notes    string `json:"notes"`
	Status   string `json:"status"`
	UserID   string `json:"user_id"`
}

// listInvestors returns every investor with their money totals, plus the
// reserve percent and the total of all current shares.
func (s *Server) listInvestors(c fiber.Ctx) error {
	u := currentUser(c)
	if !canViewInvestors(u) {
		return errForbidden()
	}
	rows, err := s.store.Queries.ListInvestors(c.Context())
	if err != nil {
		return err
	}
	if rows == nil {
		rows = []db.ListInvestorsRow{}
	}
	settings, err := s.store.Queries.GetProfitSettings(c.Context())
	if err != nil {
		return err
	}
	shares, reserve, err := sharesPlusReserve(c.Context(), s.store.Queries, uuid.Nil)
	if err != nil {
		return err
	}
	return c.JSON(fiber.Map{
		"investors":       rows,
		"reserve_percent": parseMoney(settings.ReservePercent),
		"total_shares":    shares,
		"reserve":         reserve,
	})
}

// createInvestor adds a capital partner. user_id is optional: when given, it
// must be an existing user with the investor role, so they can sign in and
// see their own profile.
func (s *Server) createInvestor(c fiber.Ctx) error {
	u := currentUser(c)
	if !canManageInvestors(u) {
		return errForbidden()
	}
	var in investorInput
	if err := c.Bind().JSON(&in); err != nil {
		return errBadRequest("Invalid request body")
	}
	name := strings.TrimSpace(in.Name)
	if len(name) < 1 || len(name) > 120 {
		return errBadRequest("Name is required (max 120 characters)")
	}
	joinDate := strings.TrimSpace(in.JoinDate)
	if joinDate == "" {
		joinDate = time.Now().UTC().Format("2006-01-02")
	}
	if !validMilestoneDate(joinDate) {
		return errBadRequest("Join date must be a date (YYYY-MM-DD)")
	}
	var userID *uuid.UUID
	if strings.TrimSpace(in.UserID) != "" {
		uid, err := uuid.Parse(strings.TrimSpace(in.UserID))
		if err != nil {
			return errBadRequest("Invalid user_id")
		}
		linked, err := s.store.GetUserByID(c.Context(), uid)
		if err != nil {
			return errBadRequest("Linked user not found")
		}
		if roleOf(linked) != policy.Investor {
			return errBadRequest("Linked user must have the investor role")
		}
		userID = &uid
	}
	if userID != nil {
		// One login links to at most one investor (P0-9; the partial unique
		// index in migration 00048 is the backstop, this is the friendly 400).
		var existing uuid.UUID
		if err := s.store.Pool.QueryRow(c.Context(),
			`SELECT id FROM investors WHERE user_id = $1`, *userID).Scan(&existing); err == nil {
			return errBadRequest("This user is already linked to an investor")
		} else if err != nil && !errors.Is(err, sql.ErrNoRows) {
			return err
		}
	}
	id, err := s.store.Queries.CreateInvestor(c.Context(), db.CreateInvestorParams{
		UserID: userID, Name: name,
		Email:    strPtr(strings.TrimSpace(in.Email)),
		Phone:    strPtr(strings.TrimSpace(in.Phone)),
		JoinDate: joinDate, Notes: strings.TrimSpace(in.Notes),
	})
	if err != nil {
		return err
	}
	return c.Status(fiber.StatusCreated).JSON(fiber.Map{"id": id})
}

func strPtr(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}

// updateInvestor edits an investor. Only the CEO/HR/admin; an investor never
// edits their own record through this route.
func (s *Server) updateInvestor(c fiber.Ctx) error {
	u := currentUser(c)
	if !canManageInvestors(u) {
		return errForbidden()
	}
	id, err := paramUUID(c, "id")
	if err != nil {
		return err
	}
	var in investorInput
	if err := c.Bind().JSON(&in); err != nil {
		return errBadRequest("Invalid request body")
	}
	name := strings.TrimSpace(in.Name)
	if len(name) < 1 || len(name) > 120 {
		return errBadRequest("Name is required (max 120 characters)")
	}
	status := strings.TrimSpace(in.Status)
	if status != "active" && status != "inactive" {
		return errBadRequest("Status must be active or inactive")
	}
	joinDate := strings.TrimSpace(in.JoinDate)
	if joinDate == "" {
		joinDate = time.Now().UTC().Format("2006-01-02")
	}
	if !validMilestoneDate(joinDate) {
		return errBadRequest("Join date must be a date (YYYY-MM-DD)")
	}
	var userID *uuid.UUID
	if strings.TrimSpace(in.UserID) != "" {
		uid, err := uuid.Parse(strings.TrimSpace(in.UserID))
		if err != nil {
			return errBadRequest("Invalid user_id")
		}
		linked, err := s.store.GetUserByID(c.Context(), uid)
		if err != nil {
			return errBadRequest("Linked user not found")
		}
		if roleOf(linked) != policy.Investor {
			return errBadRequest("Linked user must have the investor role")
		}
		userID = &uid
	}
	if userID != nil {
		// One login links to at most one investor (P0-9); exclude this record.
		var existing uuid.UUID
		if err := s.store.Pool.QueryRow(c.Context(),
			`SELECT id FROM investors WHERE user_id = $1 AND id <> $2`, *userID, id).Scan(&existing); err == nil {
			return errBadRequest("This user is already linked to an investor")
		} else if err != nil && !errors.Is(err, sql.ErrNoRows) {
			return err
		}
	}
	if err := s.store.Queries.UpdateInvestor(c.Context(), db.UpdateInvestorParams{
		ID: id, UserID: userID, Name: name,
		Email:    strPtr(strings.TrimSpace(in.Email)),
		Phone:    strPtr(strings.TrimSpace(in.Phone)),
		JoinDate: joinDate, Status: status, Notes: strings.TrimSpace(in.Notes),
	}); err != nil {
		return err
	}
	return c.JSON(fiber.Map{"ok": true})
}

type investorDetail struct {
	Investor      db.GetInvestorRow                   `json:"investor"`
	Investments   []db.ListInvestmentsByInvestorRow   `json:"investments"`
	Distributions []db.ListDistributionsByInvestorRow `json:"distributions"`
	Withdrawals   []db.ListWithdrawalsByInvestorRow   `json:"withdrawals"`
	Shares        []db.ListSharesByInvestorRow        `json:"shares"`
}

// getInvestor returns one investor with their full history. An investor-role
// caller may only open their own record.
func (s *Server) getInvestor(c fiber.Ctx) error {
	u := currentUser(c)
	id, err := paramUUID(c, "id")
	if err != nil {
		return err
	}
	if roleOf(u) == policy.Investor {
		self, ok := s.investorSelf(c.Context(), u.ID)
		if !ok || self.ID != id {
			return errForbidden()
		}
	} else if !canViewInvestors(u) {
		return errForbidden()
	}
	ctx := c.Context()
	inv, err := s.store.Queries.GetInvestor(ctx, id)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return errNotFound("Investor not found")
		}
		return err
	}
	investments, err := s.store.Queries.ListInvestmentsByInvestor(ctx, id)
	if err != nil {
		return err
	}
	if investments == nil {
		investments = []db.ListInvestmentsByInvestorRow{}
	}
	distributions, err := s.store.Queries.ListDistributionsByInvestor(ctx, &id)
	if err != nil {
		return err
	}
	if distributions == nil {
		distributions = []db.ListDistributionsByInvestorRow{}
	}
	withdrawals, err := s.store.Queries.ListWithdrawalsByInvestor(ctx, id)
	if err != nil {
		return err
	}
	if withdrawals == nil {
		withdrawals = []db.ListWithdrawalsByInvestorRow{}
	}
	shares, err := s.store.Queries.ListSharesByInvestor(ctx, id)
	if err != nil {
		return err
	}
	if shares == nil {
		shares = []db.ListSharesByInvestorRow{}
	}
	return c.JSON(investorDetail{
		Investor: inv, Investments: investments,
		Distributions: distributions, Withdrawals: withdrawals, Shares: shares,
	})
}

type investmentInput struct {
	Amount     float64 `json:"amount"`
	Currency   string  `json:"currency"`
	RateToBdt  string  `json:"rate_to_bdt"`
	Method     string  `json:"method"`
	InvestedAt string  `json:"invested_at"`
	Note       string  `json:"note"`
}

// recordInvestment books capital an investor put in, manually, in any
// currency. The BDT value is fixed at the rate recorded here.
func (s *Server) recordInvestment(c fiber.Ctx) error {
	u := currentUser(c)
	if !canManageInvestors(u) {
		return errForbidden()
	}
	id, err := paramUUID(c, "id")
	if err != nil {
		return err
	}
	var in investmentInput
	if err := c.Bind().JSON(&in); err != nil {
		return errBadRequest("Invalid request body")
	}
	if in.Amount <= 0 {
		return errBadRequest("Amount must be greater than 0")
	}
	currency := strings.ToUpper(strings.TrimSpace(in.Currency))
	if currency == "" {
		currency = "BDT"
	}
	if !validCurrency(currency) {
		return errBadRequest("Currency must be a 3-letter code like BDT or USD")
	}
	investedAt := strings.TrimSpace(in.InvestedAt)
	if investedAt == "" {
		investedAt = time.Now().UTC().Format("2006-01-02")
	}
	if !validMilestoneDate(investedAt) {
		return errBadRequest("Invested date must be a date (YYYY-MM-DD)")
	}
	method := strings.TrimSpace(in.Method)
	if method != "" && !finance.ValidPaymentMethod(method) {
		return errBadRequest("Unknown payment method")
	}
	rate, err := s.investorRate(c.Context(), currency, investedAt, in.RateToBdt)
	if err != nil {
		return err
	}
	if _, err := s.store.Queries.GetInvestor(c.Context(), id); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return errNotFound("Investor not found")
		}
		return err
	}
	invID, err := s.store.Queries.CreateInvestment(c.Context(), db.CreateInvestmentParams{
		InvestorID: id, Amount: moneyStr(in.Amount), Currency: currency,
		RateToBdt: rate, Method: strPtr(method), InvestedAt: investedAt,
		Note: strings.TrimSpace(in.Note), CreatedBy: &u.ID,
	})
	if err != nil {
		return err
	}
	return c.Status(fiber.StatusCreated).JSON(fiber.Map{"id": invID})
}

type shareInput struct {
	SharePercent  float64 `json:"share_percent"`
	EffectiveFrom string  `json:"effective_from"`
}

// setShare records a NEW share row (history is never rewritten). Only the
// CEO, and only while shares + reserve stay within 100%. Sprint 1 (P0-2):
// the read-check-write runs inside one transaction with the profit_settings
// row locked FOR UPDATE, so two concurrent share changes cannot both pass
// the <=100% check and push the split past 100%.
func (s *Server) setShare(c fiber.Ctx) error {
	u := currentUser(c)
	if roleOf(u) != policy.CEO {
		return errForbidden()
	}
	id, err := paramUUID(c, "id")
	if err != nil {
		return err
	}
	var in shareInput
	if err := c.Bind().JSON(&in); err != nil {
		return errBadRequest("Invalid request body")
	}
	if in.SharePercent < 0 || in.SharePercent > 100 {
		return errBadRequest("Share percent must be between 0 and 100")
	}
	effectiveFrom := strings.TrimSpace(in.EffectiveFrom)
	if effectiveFrom == "" {
		effectiveFrom = time.Now().UTC().Format("2006-01-02")
	}
	if !validMilestoneDate(effectiveFrom) {
		return errBadRequest("Effective date must be a date (YYYY-MM-DD)")
	}
	tx, err := s.store.Pool.Begin(c.Context())
	if err != nil {
		return err
	}
	defer tx.Rollback(c.Context())
	q := db.New(tx)
	if _, err := q.GetInvestor(c.Context(), id); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return errNotFound("Investor not found")
		}
		return err
	}
	if _, err := tx.Exec(c.Context(), `SELECT id FROM profit_settings WHERE id = 1 FOR UPDATE`); err != nil {
		return err
	}
	others, reserve, err := sharesPlusReserve(c.Context(), q, id)
	if err != nil {
		return err
	}
	if others+in.SharePercent+reserve > 100.0001 {
		return errBadRequest(fmt.Sprintf("Shares (%.2f%%) + new share (%.2f%%) + reserve (%.2f%%) exceed 100%%",
			others, in.SharePercent, reserve))
	}
	shareID, err := q.CreateShare(c.Context(), db.CreateShareParams{
		InvestorID: id, SharePercent: moneyStr(in.SharePercent),
		EffectiveFrom: effectiveFrom, CreatedBy: &u.ID,
	})
	if err != nil {
		return err
	}
	if err := tx.Commit(c.Context()); err != nil {
		return err
	}
	return c.Status(fiber.StatusCreated).JSON(fiber.Map{"id": shareID})
}

// getProfitSettings returns the reserve percent.
func (s *Server) getProfitSettings(c fiber.Ctx) error {
	u := currentUser(c)
	if !canViewInvestors(u) {
		return errForbidden()
	}
	settings, err := s.store.Queries.GetProfitSettings(c.Context())
	if err != nil {
		return err
	}
	return c.JSON(fiber.Map{"reserve_percent": parseMoney(settings.ReservePercent)})
}

type reserveInput struct {
	ReservePercent float64 `json:"reserve_percent"`
}

// updateProfitSettings sets the company reserve percent. Only the CEO, and
// only while shares + reserve stay within 100%. Sprint 1 (P0-2): same
// transaction + FOR UPDATE lock as setShare, so a reserve change cannot race
// a share change past 100%.
func (s *Server) updateProfitSettings(c fiber.Ctx) error {
	u := currentUser(c)
	if roleOf(u) != policy.CEO {
		return errForbidden()
	}
	var in reserveInput
	if err := c.Bind().JSON(&in); err != nil {
		return errBadRequest("Invalid request body")
	}
	if in.ReservePercent < 0 || in.ReservePercent > 100 {
		return errBadRequest("Reserve percent must be between 0 and 100")
	}
	tx, err := s.store.Pool.Begin(c.Context())
	if err != nil {
		return err
	}
	defer tx.Rollback(c.Context())
	q := db.New(tx)
	if _, err := tx.Exec(c.Context(), `SELECT id FROM profit_settings WHERE id = 1 FOR UPDATE`); err != nil {
		return err
	}
	shares, _, err := sharesPlusReserve(c.Context(), q, uuid.Nil)
	if err != nil {
		return err
	}
	if shares+in.ReservePercent > 100.0001 {
		return errBadRequest(fmt.Sprintf("Shares (%.2f%%) + reserve (%.2f%%) exceed 100%%",
			shares, in.ReservePercent))
	}
	if err := q.UpdateProfitSettings(c.Context(), db.UpdateProfitSettingsParams{
		ReservePercent: moneyStr(in.ReservePercent), UpdatedBy: &u.ID,
	}); err != nil {
		return err
	}
	if err := tx.Commit(c.Context()); err != nil {
		return err
	}
	return c.JSON(fiber.Map{"ok": true})
}

type distributeInput struct {
	SourceType string  `json:"source_type"`
	SourceID   string  `json:"source_id"`
	Amount     float64 `json:"amount"`
	Currency   string  `json:"currency"`
	RateToBdt  string  `json:"rate_to_bdt"`
	Note       string  `json:"note"`
}

// distributeManual splits one income event across every active investor by
// their current shares, plus the reserve. The :id anchors the request to a
// real investor; the split itself is company-wide. Idempotent per source:
// distributing the same source_id twice returns 409.
//
// Sprint 1 (P0-11): source_id is REQUIRED. The caller must pass a stable
// UUID for the income event (e.g. the finance entry id, or a UUID generated
// once and reused on retry). A server-generated id would make every retry a
// brand-new distribution.
func (s *Server) distributeManual(c fiber.Ctx) error {
	u := currentUser(c)
	if !canManageInvestors(u) {
		return errForbidden()
	}
	id, err := paramUUID(c, "id")
	if err != nil {
		return err
	}
	var in distributeInput
	if err := c.Bind().JSON(&in); err != nil {
		return errBadRequest("Invalid request body")
	}
	sourceType := strings.TrimSpace(in.SourceType)
	if sourceType == "" {
		sourceType = "manual"
	}
	if sourceType != "milestone" && sourceType != "income" && sourceType != "manual" {
		return errBadRequest("Source type must be milestone, income or manual")
	}
	if in.Amount <= 0 {
		return errBadRequest("Amount must be greater than 0")
	}
	currency := strings.ToUpper(strings.TrimSpace(in.Currency))
	if currency == "" {
		currency = "BDT"
	}
	if !validCurrency(currency) {
		return errBadRequest("Currency must be a 3-letter code like BDT or USD")
	}
	if _, err := s.store.Queries.GetInvestor(c.Context(), id); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return errNotFound("Investor not found")
		}
		return err
	}
	rate, err := s.investorRate(c.Context(), currency, time.Now().UTC().Format("2006-01-02"), in.RateToBdt)
	if err != nil {
		return err
	}
	rateF, _ := strconv.ParseFloat(rate, 64)
	// source_id is required (P0-11): a retry must reuse the same id so the
	// UNIQUE (source_type, source_id, investor_id) guard recognises it as
	// the same distribution instead of paying twice.
	if strings.TrimSpace(in.SourceID) == "" {
		return errBadRequest("source_id is required: pass a stable UUID for this income event so a retry cannot distribute it twice")
	}
	sourceID, err := uuid.Parse(strings.TrimSpace(in.SourceID))
	if err != nil {
		return errBadRequest("Invalid source_id")
	}
	note := strings.TrimSpace(in.Note)
	created, err := distributeProfit(c.Context(), s.store.Pool, sourceType, sourceID,
		in.Amount, currency, rateF, note)
	if err != nil {
		return err
	}
	if created == 0 {
		return errConflict("This income was already distributed (or no shares and no reserve are set)")
	}
	return c.Status(fiber.StatusCreated).JSON(fiber.Map{"ok": true, "rows_created": created, "source_id": sourceID})
}

// listWithdrawals returns every withdrawal, optionally filtered by status.
func (s *Server) listWithdrawals(c fiber.Ctx) error {
	u := currentUser(c)
	if !canViewInvestors(u) {
		return errForbidden()
	}
	status := strings.TrimSpace(c.Query("status", ""))
	if status != "" && status != "pending" && status != "approved" && status != "paid" && status != "rejected" {
		return errBadRequest("Unknown status")
	}
	rows, err := s.store.Queries.ListAllWithdrawals(c.Context(), status)
	if err != nil {
		return err
	}
	if rows == nil {
		rows = []db.ListAllWithdrawalsRow{}
	}
	return c.JSON(fiber.Map{"withdrawals": rows})
}

type withdrawalInput struct {
	Amount    float64 `json:"amount"`
	Currency  string  `json:"currency"`
	RateToBdt string  `json:"rate_to_bdt"`
	Method    string  `json:"method"`
	Note      string  `json:"note"`
}

// requestWithdrawal records a withdrawal request. The investor themself, or
// management on their behalf. The amount must fit the investor's available
// balance: earned minus paid minus already-requested. Sprint 1 (P0-2): the
// balance read and the insert run in one transaction with the investor row
// locked FOR UPDATE, so two concurrent requests cannot both spend the same
// balance.
func (s *Server) requestWithdrawal(c fiber.Ctx) error {
	u := currentUser(c)
	id, err := paramUUID(c, "id")
	if err != nil {
		return err
	}
	if roleOf(u) == policy.Investor {
		self, ok := s.investorSelf(c.Context(), u.ID)
		if !ok || self.ID != id {
			return errForbidden()
		}
	} else if !canManageInvestors(u) {
		return errForbidden()
	}
	var in withdrawalInput
	if err := c.Bind().JSON(&in); err != nil {
		return errBadRequest("Invalid request body")
	}
	if in.Amount <= 0 {
		return errBadRequest("Amount must be greater than 0")
	}
	currency := strings.ToUpper(strings.TrimSpace(in.Currency))
	if currency == "" {
		currency = "BDT"
	}
	if !validCurrency(currency) {
		return errBadRequest("Currency must be a 3-letter code like BDT or USD")
	}
	method := strings.TrimSpace(in.Method)
	if !finance.ValidPaymentMethod(method) {
		return errBadRequest("Unknown payment method")
	}
	rate, err := s.investorRate(c.Context(), currency, time.Now().UTC().Format("2006-01-02"), in.RateToBdt)
	if err != nil {
		return err
	}
	rateF, _ := strconv.ParseFloat(rate, 64)
	amountBDT := round2(in.Amount * rateF)
	tx, err := s.store.Pool.Begin(c.Context())
	if err != nil {
		return err
	}
	defer tx.Rollback(c.Context())
	earned, paid, pending, err := investorBalanceLocked(c.Context(), tx, id)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return errNotFound("Investor not found")
		}
		return err
	}
	available := earned - paid - pending
	if amountBDT > available+0.005 {
		return errBadRequest(fmt.Sprintf("Amount exceeds the available balance of %.2f BDT", available))
	}
	wid, err := db.New(tx).CreateWithdrawal(c.Context(), db.CreateWithdrawalParams{
		InvestorID: id, Amount: moneyStr(in.Amount), Currency: currency,
		RateToBdt: rate, Method: method, Note: strings.TrimSpace(in.Note), RequestedBy: &u.ID,
	})
	if err != nil {
		return err
	}
	if err := tx.Commit(c.Context()); err != nil {
		return err
	}
	return c.Status(fiber.StatusCreated).JSON(fiber.Map{"id": wid})
}

// reviewWithdrawal moves a withdrawal through its states. Only the CEO
// approves or rejects; management marks an approved one paid. Sprint 1
// (P0-2): approving or paying re-checks the investor's balance inside a
// transaction with the withdrawal row locked, so a withdrawal approved
// earlier cannot be paid after the balance was spent elsewhere.
func (s *Server) reviewWithdrawal(c fiber.Ctx, to string) error {
	u := currentUser(c)
	id, err := paramUUID(c, "id")
	if err != nil {
		return err
	}
	if to == "approved" || to == "rejected" {
		if roleOf(u) != policy.CEO {
			return errForbidden()
		}
	} else if !canManageInvestors(u) {
		return errForbidden()
	}
	tx, err := s.store.Pool.Begin(c.Context())
	if err != nil {
		return err
	}
	defer tx.Rollback(c.Context())
	var investorID uuid.UUID
	var amountBDTStr, status string
	err = tx.QueryRow(c.Context(), `
		SELECT investor_id, amount_bdt::text, status FROM withdrawals WHERE id = $1 FOR UPDATE
	`, id).Scan(&investorID, &amountBDTStr, &status)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return errNotFound("Withdrawal not found")
		}
		return err
	}
	from := map[string]string{"approved": "pending", "rejected": "pending", "paid": "approved"}[to]
	if status != from {
		return errBadRequest(fmt.Sprintf("Only a %s withdrawal can move to %s", from, to))
	}
	if to == "approved" || to == "paid" {
		// This withdrawal is already counted in pending, so check it against
		// the balance excluding itself: earned - paid - other pending.
		earned, paid, pending, err := investorBalanceLocked(c.Context(), tx, investorID)
		if err != nil {
			return err
		}
		amountBDT := parseMoney(amountBDTStr)
		others := pending - amountBDT
		if amountBDT > earned-paid-others+0.005 {
			return errBadRequest(fmt.Sprintf("Withdrawal exceeds the investor's available balance of %.2f BDT", earned-paid-others))
		}
	}
	now := time.Now().UTC()
	params := db.UpdateWithdrawalStatusParams{ID: id, Status: to}
	if to == "paid" {
		// Keep the approver and their timestamp: paying only stamps paid_at.
		params.PaidAt = &now
	} else {
		params.ReviewedBy = &u.ID
		params.ReviewedAt = &now
	}
	ct, err := tx.Exec(c.Context(), `
		UPDATE withdrawals
		SET status = $2, reviewed_by = coalesce($3, reviewed_by), reviewed_at = coalesce($4, reviewed_at),
		    paid_at = coalesce($5, paid_at), updated_at = now()
		WHERE id = $1 AND status = $6
	`, id, to, params.ReviewedBy, params.ReviewedAt, params.PaidAt, from)
	if err != nil {
		return err
	}
	if ct.RowsAffected() == 0 {
		return errBadRequest(fmt.Sprintf("Only a %s withdrawal can move to %s", from, to))
	}
	if err := tx.Commit(c.Context()); err != nil {
		return err
	}
	return c.JSON(fiber.Map{"ok": true})
}

// companyAccount is the CEO/HR/admin overview: ledger income, investor
// expenses, what was distributed, the reserve, what was paid out, and the
// balance still in the company account.
func (s *Server) companyAccount(c fiber.Ctx) error {
	u := currentUser(c)
	if !canViewInvestors(u) {
		return errForbidden()
	}
	ctx := c.Context()
	totals, err := s.store.Queries.CompanyTotals(ctx)
	if err != nil {
		return err
	}
	investors, err := s.store.Queries.ListInvestors(ctx)
	if err != nil {
		return err
	}
	if investors == nil {
		investors = []db.ListInvestorsRow{}
	}
	expenses, err := s.store.Queries.InvestorExpenseTotals(ctx)
	if err != nil {
		return err
	}
	if expenses == nil {
		expenses = []db.InvestorExpenseTotalsRow{}
	}
	income := parseMoney(totals.TotalIncomeBdt)
	expenseTotal := parseMoney(totals.TotalExpensesBdt)
	withdrawn := parseMoney(totals.TotalWithdrawnBdt)
	return c.JSON(fiber.Map{
		"totals":         totals,
		"investors":      investors,
		"expense_totals": expenses,
		"net_profit_bdt": income - expenseTotal,
		"balance_bdt":    income - expenseTotal - withdrawn,
	})
}

type investorExpenseInput struct {
	Category    string  `json:"category"`
	Description string  `json:"description"`
	Amount      float64 `json:"amount"`
	Currency    string  `json:"currency"`
	RateToBdt   string  `json:"rate_to_bdt"`
	ExpenseDate string  `json:"expense_date"`
	ReceiptURL  string  `json:"receipt_url"`
}

// listInvestorExpenses returns tools & other expenses, optionally by category,
// with per-category totals.
func (s *Server) listInvestorExpenses(c fiber.Ctx) error {
	u := currentUser(c)
	if !canViewInvestors(u) {
		return errForbidden()
	}
	category := strings.TrimSpace(c.Query("category", ""))
	if category != "" && !investorExpenseCategories[category] {
		return errBadRequest("Unknown category")
	}
	rows, err := s.store.Queries.ListInvestorExpenses(c.Context(), category)
	if err != nil {
		return err
	}
	if rows == nil {
		rows = []db.ListInvestorExpensesRow{}
	}
	totals, err := s.store.Queries.InvestorExpenseTotals(c.Context())
	if err != nil {
		return err
	}
	if totals == nil {
		totals = []db.InvestorExpenseTotalsRow{}
	}
	return c.JSON(fiber.Map{"expenses": rows, "totals": totals})
}

// createInvestorExpense records a tools/software/office/marketing/other cost
// that counts against profit before distribution.
func (s *Server) createInvestorExpense(c fiber.Ctx) error {
	u := currentUser(c)
	if !canManageInvestors(u) {
		return errForbidden()
	}
	var in investorExpenseInput
	if err := c.Bind().JSON(&in); err != nil {
		return errBadRequest("Invalid request body")
	}
	category := strings.TrimSpace(in.Category)
	if !investorExpenseCategories[category] {
		return errBadRequest("Category must be Tools, Software, Office, Marketing or Other")
	}
	description := strings.TrimSpace(in.Description)
	if len(description) < 1 || len(description) > 500 {
		return errBadRequest("Description is required (max 500 characters)")
	}
	if in.Amount <= 0 {
		return errBadRequest("Amount must be greater than 0")
	}
	currency := strings.ToUpper(strings.TrimSpace(in.Currency))
	if currency == "" {
		currency = "BDT"
	}
	if !validCurrency(currency) {
		return errBadRequest("Currency must be a 3-letter code like BDT or USD")
	}
	expenseDate := strings.TrimSpace(in.ExpenseDate)
	if expenseDate == "" {
		expenseDate = time.Now().UTC().Format("2006-01-02")
	}
	if !validMilestoneDate(expenseDate) {
		return errBadRequest("Expense date must be a date (YYYY-MM-DD)")
	}
	rate, err := s.investorRate(c.Context(), currency, expenseDate, in.RateToBdt)
	if err != nil {
		return err
	}
	id, err := s.store.Queries.CreateInvestorExpense(c.Context(), db.CreateInvestorExpenseParams{
		Category: category, Description: description, Amount: moneyStr(in.Amount),
		Currency: currency, RateToBdt: rate, ExpenseDate: expenseDate,
		ReceiptUrl: strPtr(strings.TrimSpace(in.ReceiptURL)), AddedBy: &u.ID,
	})
	if err != nil {
		return err
	}
	return c.Status(fiber.StatusCreated).JSON(fiber.Map{"id": id})
}

// deleteInvestorExpense removes an expense. Only the CEO.
func (s *Server) deleteInvestorExpense(c fiber.Ctx) error {
	u := currentUser(c)
	if roleOf(u) != policy.CEO {
		return errForbidden()
	}
	id, err := paramUUID(c, "id")
	if err != nil {
		return err
	}
	if err := s.store.Queries.DeleteInvestorExpense(c.Context(), id); err != nil {
		return err
	}
	return c.JSON(fiber.Map{"ok": true})
}

// listCurrencyRates returns the USD->BDT rate history. Rates live in the
// finance_rates table (00026), the single rate store for the whole app —
// milestone payments read the same rows.
func (s *Server) listCurrencyRates(c fiber.Ctx) error {
	u := currentUser(c)
	if !canViewInvestors(u) {
		return errForbidden()
	}
	rows, err := s.store.Queries.ListFinanceRates(c.Context())
	if err != nil {
		return err
	}
	if rows == nil {
		rows = []db.ListFinanceRatesRow{}
	}
	return c.JSON(fiber.Map{"rates": rows})
}

type currencyRateInput struct {
	Currency      string  `json:"currency"`
	Rate          float64 `json:"rate"`
	EffectiveFrom string  `json:"effective_from"`
}

// createCurrencyRate sets a rate from a date. A second rate for the same
// currency and date is a 409.
func (s *Server) createCurrencyRate(c fiber.Ctx) error {
	u := currentUser(c)
	if !canManageInvestors(u) {
		return errForbidden()
	}
	var in currencyRateInput
	if err := c.Bind().JSON(&in); err != nil {
		return errBadRequest("Invalid request body")
	}
	currency := strings.ToUpper(strings.TrimSpace(in.Currency))
	if !validCurrency(currency) || currency == "BDT" {
		return errBadRequest("Currency must be a 3-letter code other than BDT")
	}
	if in.Rate <= 0 {
		return errBadRequest("Rate must be greater than 0")
	}
	effectiveFrom := strings.TrimSpace(in.EffectiveFrom)
	if effectiveFrom == "" {
		effectiveFrom = time.Now().UTC().Format("2006-01-02")
	}
	if !validMilestoneDate(effectiveFrom) {
		return errBadRequest("Effective date must be a date (YYYY-MM-DD)")
	}
	id, err := s.store.Queries.CreateFinanceRate(c.Context(), db.CreateFinanceRateParams{
		Currency: currency, Rate: moneyStr(in.Rate),
		EffectiveFrom: effectiveFrom, CreatedBy: u.ID,
	})
	if err != nil {
		if strings.Contains(err.Error(), "duplicate key") {
			return errConflict(fmt.Sprintf("A %s rate is already set for %s", currency, effectiveFrom))
		}
		return err
	}
	return c.Status(fiber.StatusCreated).JSON(fiber.Map{"id": id})
}

// latestCurrencyRate returns the rate in force today for a currency.
func (s *Server) latestCurrencyRate(c fiber.Ctx) error {
	u := currentUser(c)
	if !canViewInvestors(u) {
		return errForbidden()
	}
	currency := strings.ToUpper(strings.TrimSpace(c.Query("currency", "USD")))
	if !validCurrency(currency) {
		return errBadRequest("Currency must be a 3-letter code like USD")
	}
	rate, err := s.store.Queries.FinanceRateOn(c.Context(), db.FinanceRateOnParams{
		Currency: currency, OnDay: time.Now().UTC().Format("2006-01-02"),
	})
	if err != nil {
		return err
	}
	return c.JSON(fiber.Map{"currency": currency, "rate": rate})
}

type investorMeOut struct {
	Investor      db.GetInvestorByUserIDRow           `json:"investor"`
	Investments   []db.ListInvestmentsByInvestorRow   `json:"investments"`
	Distributions []db.ListDistributionsByInvestorRow `json:"distributions"`
	ByMonth       []db.ListDistributionsByMonthRow    `json:"by_month"`
	Withdrawals   []db.ListWithdrawalsByInvestorRow   `json:"withdrawals"`
	ReserveBdt    string                              `json:"reserve_bdt"`
}

// investorMe is the logged-in investor's own dashboard: their totals,
// investments, distributions, monthly breakdown, withdrawals — and the
// company reserve total, read-only. Nothing about other investors.
func (s *Server) investorMe(c fiber.Ctx) error {
	u := currentUser(c)
	inv, ok := s.investorSelf(c.Context(), u.ID)
	if !ok {
		return errNotFound("No investor profile is linked to your account")
	}
	ctx := c.Context()
	investments, err := s.store.Queries.ListInvestmentsByInvestor(ctx, inv.ID)
	if err != nil {
		return err
	}
	if investments == nil {
		investments = []db.ListInvestmentsByInvestorRow{}
	}
	distributions, err := s.store.Queries.ListDistributionsByInvestor(ctx, &inv.ID)
	if err != nil {
		return err
	}
	if distributions == nil {
		distributions = []db.ListDistributionsByInvestorRow{}
	}
	byMonth, err := s.store.Queries.ListDistributionsByMonth(ctx, &inv.ID)
	if err != nil {
		return err
	}
	if byMonth == nil {
		byMonth = []db.ListDistributionsByMonthRow{}
	}
	withdrawals, err := s.store.Queries.ListWithdrawalsByInvestor(ctx, inv.ID)
	if err != nil {
		return err
	}
	if withdrawals == nil {
		withdrawals = []db.ListWithdrawalsByInvestorRow{}
	}
	totals, err := s.store.Queries.CompanyTotals(ctx)
	if err != nil {
		return err
	}
	return c.JSON(investorMeOut{
		Investor: inv, Investments: investments, Distributions: distributions,
		ByMonth: byMonth, Withdrawals: withdrawals, ReserveBdt: totals.ReserveBdt,
	})
}
