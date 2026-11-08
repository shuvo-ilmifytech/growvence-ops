package sheets

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strconv"
	"strings"
	"time"

	"github.com/solvinx/opsflow/internal/db"
	"github.com/solvinx/opsflow/internal/store"
)

// Columns of the backup sheet, in order. Project # is column A and identifies the row,
// so inserting a column in the middle is safe but moving A is not.
var Header = []string{
	"Project #", "Project", "Status", "Client", "Client username", "Order ID", "Profile",
	"Type", "Team", "Sales", "Assigned to", "Members", "Priority", "Agreed price", "Currency",
	"Delivery deadline", "Due date", "Submitted at", "Completed at", "Revisions", "Backed up at",
}

// batch is how many projects one pass writes.
const batch = 200

type Syncer struct {
	st     *store.Store
	client *Client
	// loc is resolved per pass from the org_timezone setting, so dates read as they
	// do in the app rather than in UTC.
	now func() time.Time
}

func NewSyncer(st *store.Store, c *Client) *Syncer {
	return &Syncer{st: st, client: c, now: time.Now}
}

// Status is what the admin screen shows.
type Status struct {
	Configured    bool       `json:"configured"`
	SpreadsheetID string     `json:"spreadsheet_id"`
	Tab           string     `json:"tab"`
	ShareWith     string     `json:"share_with"` // the service account the sheet must be shared with
	Total         int64      `json:"total"`
	Pending       int64      `json:"pending"`
	Failing       int64      `json:"failing"`
	LastSyncedAt  *time.Time `json:"last_synced_at"`
	LastError     string     `json:"last_error"`
}

// ReadStatus reports the queue's state. It works whether or not a sheet is configured.
func ReadStatus(ctx context.Context, st *store.Store, c *Client) (Status, error) {
	row, err := st.SheetSyncStatus(ctx)
	if err != nil {
		return Status{}, err
	}
	out := Status{Total: row.Total, Pending: row.Pending, Failing: row.Failing, LastError: row.LastError}
	if c != nil {
		out.Configured, out.SpreadsheetID, out.Tab, out.ShareWith = true, c.SpreadsheetID(), c.Tab(), c.ServiceAccountEmail()
	}
	if at, err := st.LastSheetSync(ctx); err == nil {
		out.LastSyncedAt = &at
	} else if !store.IsNotFound(err) {
		return out, err
	}
	return out, nil
}

// Once writes every project waiting in the queue and reports how many it wrote.
func (s *Syncer) Once(ctx context.Context) (int, error) {
	claimedAt := s.now()
	pending, err := s.st.ClaimSheetSync(ctx, batch)
	if err != nil || len(pending) == 0 {
		return 0, err
	}
	loc := s.location(ctx)

	// Where each project already sits, read from column A. The header is written first
	// so an empty sheet gets its titles before any data.
	numbers, err := s.client.Column(ctx, "A")
	if err != nil {
		s.failAll(ctx, pending, err)
		return 0, err
	}
	rowOf := map[string]int{}
	for i, cell := range numbers {
		if cell = strings.TrimSpace(cell); cell != "" {
			rowOf[cell] = i + 1 // sheet rows are 1-based
		}
	}
	if len(numbers) == 0 {
		head := make([]any, len(Header))
		for i, h := range Header {
			head[i] = h
		}
		if _, err := s.client.Append(ctx, [][]any{head}); err != nil {
			s.failAll(ctx, pending, err)
			return 0, err
		}
		rowOf = map[string]int{}
	}

	updates := map[int][]any{}
	var appendRows [][]any
	var appended []db.ClaimSheetSyncRow
	for _, p := range pending {
		values := s.row(p.ProjectDetail, loc)
		key := strconv.FormatInt(p.ProjectDetail.Number, 10)
		if row, ok := rowOf[key]; ok {
			updates[row] = values
			continue
		}
		if p.RowNumber != nil && int(*p.RowNumber) <= len(numbers) {
			// We wrote it before and column A was cleared; rewrite in place.
			updates[int(*p.RowNumber)] = values
			continue
		}
		appendRows = append(appendRows, values)
		appended = append(appended, p)
	}

	if err := s.client.Update(ctx, updates, len(Header)); err != nil {
		s.failAll(ctx, pending, err)
		return 0, err
	}
	rows, err := s.client.Append(ctx, appendRows)
	if err != nil {
		s.failAll(ctx, pending, err)
		return 0, err
	}

	// Mark each project synced with the row it now occupies.
	placed := map[string]int{}
	for i, p := range appended {
		placed[p.ProjectID.String()] = rows[i]
	}
	for _, p := range pending {
		row := placed[p.ProjectID.String()]
		if row == 0 {
			key := strconv.FormatInt(p.ProjectDetail.Number, 10)
			if r, ok := rowOf[key]; ok {
				row = r
			} else if p.RowNumber != nil {
				row = int(*p.RowNumber)
			}
		}
		n := int32(row)
		if err := s.st.MarkSheetSynced(ctx, db.MarkSheetSyncedParams{
			ProjectID: p.ProjectID, RowNumber: &n, ClaimedAt: claimedAt,
		}); err != nil {
			return 0, err
		}
	}
	return len(pending), nil
}

// failAll records the same error against everything in this pass, so each gets backed off.
func (s *Syncer) failAll(ctx context.Context, pending []db.ClaimSheetSyncRow, cause error) {
	for _, p := range pending {
		if err := s.st.MarkSheetFailed(ctx, db.MarkSheetFailedParams{ProjectID: p.ProjectID, LastError: cause.Error()}); err != nil {
			slog.Error("recording a sheet failure failed", "err", err)
			return
		}
	}
}

func (s *Syncer) location(ctx context.Context) *time.Location {
	if row, err := s.st.GetSetting(ctx, "org_timezone"); err == nil {
		var tz string
		if json.Unmarshal(row.Value, &tz) == nil {
			if l, err := time.LoadLocation(tz); err == nil {
				return l
			}
		}
	}
	return time.UTC
}

// row renders one project in the sheet's column order.
func (s *Syncer) row(p db.ProjectDetail, loc *time.Location) []any {
	return []any{
		p.Number,
		str(p.Name),
		statusNames[p.Status],
		str(p.ClientName),
		str(p.ClientContact),
		str(p.OrderID),
		str(p.ProfileName),
		str(p.ProjectTypeName),
		str(p.TeamName),
		p.CreatedByName,
		str(p.AssignedToName),
		strings.Join(p.MemberNames, ", "),
		priorityNames[p.Priority],
		str(p.AgreedPrice),
		p.Currency,
		moment(p.Deadline, loc),
		day(p.DueDate),
		stamp(p.FirstSubmittedAt, loc),
		stamp(p.CompletedAt, loc),
		p.RevisionCount,
		s.now().In(loc).Format("2006-01-02 15:04"),
	}
}

var statusNames = map[string]string{
	"new": "New", "in_progress": "In progress", "blocked": "Blocked", "ready_for_review": "In review",
	"ready_to_deliver": "Ready to deliver", "delivered": "Delivered", "completed": "Completed", "cancelled": "Cancelled",
	// From the old flow, in case a row is replayed from history.
	"draft": "Draft", "submitted": "Submitted", "needs_info": "Needs info", "assigned": "Assigned",
}

var priorityNames = map[string]string{"low": "Low", "medium": "Medium", "high": "High", "urgent": "Urgent"}

func str(p *string) string {
	if p == nil {
		return ""
	}
	return *p
}

// day keeps a plain date as it is; moment turns an ISO instant into local date and time.
func day(p *string) string {
	s := str(p)
	if len(s) > 10 {
		return s[:10]
	}
	return s
}

func moment(p *string, loc *time.Location) string {
	s := str(p)
	if s == "" {
		return ""
	}
	t, err := time.Parse(time.RFC3339, s)
	if err != nil {
		return day(p)
	}
	return t.In(loc).Format("2006-01-02 15:04")
}

func stamp(t *time.Time, loc *time.Location) string {
	if t == nil {
		return ""
	}
	return t.In(loc).Format("2006-01-02 15:04")
}

// Backfill queues every project again and writes them in one go.
func (s *Syncer) Backfill(ctx context.Context) (int, error) {
	n, err := s.st.EnqueueAllProjects(ctx)
	if err != nil {
		return 0, err
	}
	written := 0
	for written < int(n) {
		done, err := s.Once(ctx)
		if err != nil {
			return written, err
		}
		if done == 0 {
			break
		}
		written += done
	}
	return written, nil
}

// Check writes nothing: it proves the credentials work and the sheet is reachable.
func (c *Client) Check(ctx context.Context) error {
	if _, err := c.Column(ctx, "A"); err != nil {
		return fmt.Errorf("cannot read the sheet (is it shared with %s as an editor?): %w", c.ServiceAccountEmail(), err)
	}
	return nil
}
