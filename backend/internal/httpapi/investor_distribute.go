package httpapi

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"math"
	"sort"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/solvinx/opsflow/internal/db"
)

// Investor profit distribution (Phase 3): an income event is split across the
// active investors by their current share percent, plus the company reserve.
// Every row is inserted ON CONFLICT DO NOTHING, so a retry — a double-click,
// a crashed request replayed, the milestone hook running twice — can never
// pay the same income twice.
//
// Sprint 1 (P0-3, P0-10): the whole split runs inside ONE transaction, and
// the split is computed in integer paisa with largest-remainder rounding, so
// the distribution rows always sum exactly to the amount actually split — no
// rounding dust, no partial payouts.

func round2(f float64) float64 { return math.Round(f*100) / 100 }

// profitPart is one slice of a distribution: an investor (or nil for the
// company reserve) with its share percent.
type profitPart struct {
	investorID *uuid.UUID
	pct        float64
}

// profitCut is a computed slice in integer paisa.
type profitCut struct {
	investorID *uuid.UUID
	pct        float64
	paisa      int64
}

// splitProfit divides totalPaisa across parts by percent using largest-
// remainder rounding. The amount actually split is totalPaisa * sum(pct)/100
// rounded to the nearest paisa; when shares + reserve are under 100% the
// remainder stays undistributed by design (it is visible as undistributed
// profit in the company account, never silently lost to rounding). The
// returned cuts always sum exactly to the returned split amount. Parts with
// pct <= 0 are skipped.
func splitProfit(totalPaisa int64, parts []profitPart) (cuts []profitCut, splitPaisa int64) {
	var sumPct float64
	for _, p := range parts {
		if p.pct > 0 {
			sumPct += p.pct
		}
	}
	if sumPct <= 0 || totalPaisa <= 0 {
		return nil, 0
	}
	splitPaisa = int64(math.Round(float64(totalPaisa) * sumPct / 100))
	if splitPaisa <= 0 {
		return nil, 0
	}
	type work struct {
		part  profitPart
		floor int64
		rem   float64
	}
	works := make([]work, 0, len(parts))
	var floorSum int64
	for _, p := range parts {
		if p.pct <= 0 {
			continue
		}
		exact := float64(splitPaisa) * p.pct / sumPct
		fl := int64(math.Floor(exact))
		works = append(works, work{part: p, floor: fl, rem: exact - float64(fl)})
		floorSum += fl
	}
	// leftover is the rounding remainder: sum of remainders, so 0 <= leftover
	// < len(works). Hand one paisa each to the largest remainders.
	leftover := splitPaisa - floorSum
	sort.Slice(works, func(i, j int) bool {
		if works[i].rem != works[j].rem {
			return works[i].rem > works[j].rem
		}
		if works[i].part.pct != works[j].part.pct {
			return works[i].part.pct > works[j].part.pct
		}
		return idStr(works[i].part.investorID) < idStr(works[j].part.investorID)
	})
	for i := int64(0); i < leftover && i < int64(len(works)); i++ {
		works[i].floor++
	}
	cuts = make([]profitCut, 0, len(works))
	var check int64
	for _, w := range works {
		if w.floor <= 0 {
			continue
		}
		cuts = append(cuts, profitCut{investorID: w.part.investorID, pct: w.part.pct, paisa: w.floor})
		check += w.floor
	}
	if check != splitPaisa {
		// Defensive: the math above guarantees this, but never persist a
		// split that does not reconcile.
		return nil, 0
	}
	return cuts, splitPaisa
}

func idStr(id *uuid.UUID) string {
	if id == nil {
		return ""
	}
	return id.String()
}

// distributeProfit splits amount (in currency, converted at rate) for one
// income event. sourceID identifies the event; callers must pass a stable
// id so retries reuse it. It returns how many distribution rows were newly
// created (0 means this source was already distributed) and any hard error.
// The whole split — every investor row plus the reserve row — commits or
// rolls back together.
func distributeProfit(ctx context.Context, pool *pgxpool.Pool, sourceType string, sourceID uuid.UUID, amount float64, currency string, rate float64, note string) (int, error) {
	tx, err := pool.Begin(ctx)
	if err != nil {
		return 0, err
	}
	defer tx.Rollback(ctx)
	q := db.New(tx)

	settings, err := q.GetProfitSettings(ctx)
	if err != nil {
		return 0, err
	}
	reservePct := parseMoney(settings.ReservePercent)
	shares, err := q.ListLatestShares(ctx)
	if err != nil {
		return 0, err
	}
	parts := make([]profitPart, 0, len(shares)+1)
	for _, sh := range shares {
		if pct := parseMoney(sh.SharePercent); pct > 0 {
			invID := sh.InvestorID
			parts = append(parts, profitPart{investorID: &invID, pct: pct})
		}
	}
	if reservePct > 0 {
		parts = append(parts, profitPart{investorID: nil, pct: reservePct})
	}
	totalPaisa := int64(math.Round(round2(amount*rate) * 100))
	cuts, splitPaisa := splitProfit(totalPaisa, parts)
	if len(cuts) == 0 {
		return 0, nil
	}
	created := 0
	for _, cut := range cuts {
		amtBDT := float64(cut.paisa) / 100
		amt := round2(amtBDT / rate)
		if cut.investorID == nil {
			_, err = q.CreateReserveDistribution(ctx, db.CreateReserveDistributionParams{
				SourceType: sourceType, SourceID: &sourceID,
				Amount: moneyStr(amt), Currency: currency, RateToBdt: moneyStr(rate),
				AmountBdt: moneyStr(amtBDT), SharePercent: moneyStr(cut.pct), Note: note,
			})
		} else {
			invID := *cut.investorID
			_, err = q.CreateDistribution(ctx, db.CreateDistributionParams{
				SourceType: sourceType, SourceID: &sourceID, InvestorID: &invID,
				Amount: moneyStr(amt), Currency: currency, RateToBdt: moneyStr(rate),
				AmountBdt: moneyStr(amtBDT), SharePercent: moneyStr(cut.pct), Note: note,
			})
		}
		if err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				continue // already distributed for this investor; skip, don't fail
			}
			return created, err
		}
		created++
	}
	// Reconcile: what this run actually inserted must equal the split, unless
	// some rows already existed from an earlier (partial) run of this source.
	if created > 0 {
		var insertedPaisa int64
		err = tx.QueryRow(ctx, `
			SELECT COALESCE(sum(amount_bdt * 100), 0)::bigint
			FROM profit_distributions
			WHERE source_type = $1 AND source_id = $2`, sourceType, sourceID).Scan(&insertedPaisa)
		if err != nil {
			return created, err
		}
		if insertedPaisa != splitPaisa {
			return created, fmt.Errorf("distribution does not reconcile: inserted %d paisa, split was %d", insertedPaisa, splitPaisa)
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return created, err
	}
	return created, nil
}

// investorBalanceLocked reads one investor's earned / paid / pending totals
// inside a transaction after locking the investor row FOR UPDATE. Any
// concurrent withdrawal request for the same investor blocks on the lock
// until this transaction commits, so two requests can never both pass the
// available-balance check against the same funds. Returns sql.ErrNoRows
// when the investor does not exist.
func investorBalanceLocked(ctx context.Context, tx pgx.Tx, investorID uuid.UUID) (earned, paid, pending float64, err error) {
	var earnedS, paidS, pendingS string
	err = tx.QueryRow(ctx, `
		SELECT coalesce((SELECT sum(amount_bdt) FROM profit_distributions WHERE investor_id = $1), 0)::text,
		       coalesce((SELECT sum(amount_bdt) FROM withdrawals WHERE investor_id = $1 AND status = 'paid'), 0)::text,
		       coalesce((SELECT sum(amount_bdt) FROM withdrawals WHERE investor_id = $1 AND status IN ('pending', 'approved')), 0)::text
		FROM investors WHERE id = $1 FOR UPDATE`, investorID).Scan(&earnedS, &paidS, &pendingS)
	if err != nil {
		return 0, 0, 0, err
	}
	return parseMoney(earnedS), parseMoney(paidS), parseMoney(pendingS), nil
}

// sharesPlusReserve totals the current shares of every active investor plus
// the reserve percent, so a change can be rejected before it pushes the
// split past 100%. excludeID (uuid.Nil for none) leaves one investor out —
// their new share replaces the old one.
func sharesPlusReserve(ctx context.Context, q *db.Queries, excludeID uuid.UUID) (shares, reserve float64, err error) {
	settings, err := q.GetProfitSettings(ctx)
	if err != nil {
		return 0, 0, err
	}
	reserve = parseMoney(settings.ReservePercent)
	rows, err := q.ListLatestShares(ctx)
	if err != nil {
		return 0, 0, err
	}
	for _, r := range rows {
		if excludeID != uuid.Nil && r.InvestorID == excludeID {
			continue
		}
		shares += parseMoney(r.SharePercent)
	}
	return shares, reserve, nil
}
