// Package reports serves the Management dashboard, CSV exports and the activity feed.
package reports

import (
	"context"
	"encoding/csv"
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/solvinx/opsflow/internal/apperr"
	"github.com/solvinx/opsflow/internal/db"
	"github.com/solvinx/opsflow/internal/policy"
	"github.com/solvinx/opsflow/internal/store"
	"github.com/solvinx/opsflow/internal/validate"
)

type Service struct{ st *store.Store }

func NewService(st *store.Store) *Service { return &Service{st: st} }

// Location is the organisation's timezone (setting), used for "today" and date ranges.
func (s *Service) Location(ctx context.Context) *time.Location {
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

// Range is an inclusive span of whole days in the org timezone.
type Range struct {
	From, To time.Time // To is the last instant of the last day
	FromDay  string
	ToDay    string
}

// ParseRange reads YYYY-MM-DD from/to; the default is the last 30 days including today.
func (s *Service) ParseRange(ctx context.Context, from, to string) (Range, error) {
	loc := s.Location(ctx)
	now := time.Now().In(loc)
	v := validate.Errors{}
	parse := func(key, raw string, def time.Time) time.Time {
		if raw == "" {
			return def
		}
		t, err := time.ParseInLocation(time.DateOnly, raw, loc)
		if err != nil {
			v.Add(key, "must be a date (YYYY-MM-DD)")
		}
		return t
	}
	today := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, loc)
	f := parse("from", from, today.AddDate(0, 0, -29))
	t := parse("to", to, today)
	if v.OK() && t.Before(f) {
		v.Add("to", "must be on or after from")
	}
	if v.OK() && t.Sub(f) > 366*24*time.Hour*3 {
		v.Add("from", "range can be at most three years")
	}
	if !v.OK() {
		return Range{}, apperr.Validation(v)
	}
	return Range{From: f, To: t.AddDate(0, 0, 1).Add(-time.Nanosecond), FromDay: f.Format(time.DateOnly), ToDay: t.Format(time.DateOnly)}, nil
}

type Summary struct {
	Range       map[string]string                        `json:"range"`
	ByStatus    []db.ReportProjectsByStatusRow           `json:"projects_by_status"`
	ByMember    []db.ReportProjectsByMemberRow           `json:"projects_by_member"`
	Overdue     []db.ReportOverdueRow                    `json:"overdue"`
	Unassigned  []db.ReportUnassignedRow                 `json:"unassigned"`
	Timing      db.ReportTimingRow                       `json:"timing"`
	Updates     []db.ReportUpdatesByDestinationStatusRow `json:"updates_by_destination_status"`
	Approval    db.ReportApprovalTurnaroundRow           `json:"approval_turnaround"`
	Revisions   db.ReportRevisionsRow                    `json:"revisions"`
	TopRevision []db.ReportTopRevisionsRow               `json:"top_revisions"`
	Sales       []SalesRow                               `json:"sales_by_person"`
}

type SalesRow struct {
	db.ReportSalesByPersonRow
	Revenue map[string]string `json:"revenue"` // currency -> what was received for completed projects (agreed price less the fee)
}

func (s *Service) Summary(ctx context.Context, r Range) (Summary, error) {
	q := s.st.Queries
	out := Summary{Range: map[string]string{"from": r.FromDay, "to": r.ToDay}}
	var err error
	if out.ByStatus, err = q.ReportProjectsByStatus(ctx); err != nil {
		return out, err
	}
	if out.ByMember, err = q.ReportProjectsByMember(ctx); err != nil {
		return out, err
	}
	if out.Overdue, err = q.ReportOverdue(ctx); err != nil {
		return out, err
	}
	if out.Unassigned, err = q.ReportUnassigned(ctx); err != nil {
		return out, err
	}
	if out.Timing, err = q.ReportTiming(ctx, db.ReportTimingParams{FromTs: r.From, ToTs: r.To}); err != nil {
		return out, err
	}
	if out.Updates, err = q.ReportUpdatesByDestinationStatus(ctx, db.ReportUpdatesByDestinationStatusParams{FromTs: r.From, ToTs: r.To}); err != nil {
		return out, err
	}
	if out.Approval, err = q.ReportApprovalTurnaround(ctx, db.ReportApprovalTurnaroundParams{FromTs: r.From, ToTs: r.To}); err != nil {
		return out, err
	}
	if out.Revisions, err = q.ReportRevisions(ctx, db.ReportRevisionsParams{FromTs: r.From, ToTs: r.To}); err != nil {
		return out, err
	}
	if out.TopRevision, err = q.ReportTopRevisions(ctx); err != nil {
		return out, err
	}
	people, err := q.ReportSalesByPerson(ctx, db.ReportSalesByPersonParams{FromTs: r.From, ToTs: r.To})
	if err != nil {
		return out, err
	}
	revenue, err := q.ReportSalesRevenue(ctx, db.ReportSalesRevenueParams{FromTs: r.From, ToTs: r.To})
	if err != nil {
		return out, err
	}
	for _, p := range people {
		row := SalesRow{ReportSalesByPersonRow: p, Revenue: map[string]string{}}
		for _, rv := range revenue {
			if rv.UserID == p.ID {
				row.Revenue[rv.Currency] = rv.Total
			}
		}
		out.Sales = append(out.Sales, row)
	}
	return out, nil
}

// ── Activity ───────────────────────────────────────────────

type ActivityFilter struct {
	BeforeID     *int64
	ProjectID    *uuid.UUID
	ActorID      *uuid.UUID
	ActionPrefix *string
	Limit        int
}

func (s *Service) Activity(ctx context.Context, u db.User, f ActivityFilter) ([]db.ListActivityRow, error) {
	return s.st.ListActivity(ctx, db.ListActivityParams{
		BeforeID: f.BeforeID, ProjectID: f.ProjectID, ActorID: f.ActorID, ActionPrefix: f.ActionPrefix,
		ProjectsOnly: policy.Role(u.Role) != policy.CEO,
		ScopeLead:    scopeLead(u),
		Lim:          int32(f.Limit),
	})
}

// ── CSV ────────────────────────────────────────────────────

// cell neutralises spreadsheet formula injection (=, +, -, @, tab, CR at the start).
func cell(s string) string {
	if s != "" && strings.ContainsRune("=+-@\t\r", rune(s[0])) {
		return "'" + s
	}
	return s
}

// Cell is cell for other packages that write CSV.
func Cell(s string) string { return cell(s) }

func str(p *string) string {
	if p == nil {
		return ""
	}
	return *p
}

func stamp(t *time.Time, loc *time.Location) string {
	if t == nil {
		return ""
	}
	return t.In(loc).Format("2006-01-02 15:04")
}

var destinationNames = map[string]string{"inbox": "Inbox", "order_page": "Order page", "delivery": "Delivery"}
var updateStatusNames = map[string]string{
	"draft": "Draft Update", "pending_approval": "Pending Approval", "changes_requested": "Changes Requested",
	"approved": "Approved", "sent": "Sent",
}

type UpdateExportFilter struct {
	Statuses    []string
	Destination *string
	Q           *string
	Range       Range
}

// WriteUpdatesCSV writes the update tracker with the same columns as the old Google Sheet.
func (s *Service) WriteUpdatesCSV(ctx context.Context, w io.Writer, f UpdateExportFilter) error {
	loc := s.Location(ctx)
	rows, err := s.st.ListUpdateDetails(ctx, db.ListUpdateDetailsParams{
		Statuses: f.Statuses, Destination: f.Destination, Q: f.Q,
		CreatedFrom: &f.Range.From, CreatedTo: &f.Range.To, Lim: 50000,
	})
	if err != nil {
		return err
	}
	c := csv.NewWriter(w)
	_ = c.Write([]string{"Update ID", "Date", "Project #", "Project", "Order ID", "Profile Name", "Client Name",
		"Update By", "Update Message", "Update To", "Status", "Revision", "Checked By", "Checked At", "Sent By", "Sent At", "Attachments"})
	for _, r := range rows {
		u := r.UpdateDetail
		rev := ""
		if u.RevisionNumber != nil {
			rev = fmt.Sprint(*u.RevisionNumber)
		}
		checked := str(u.CheckedByName)
		if checked == "" && u.AutoApproved {
			checked = "Auto-approved"
		}
		_ = c.Write([]string{
			fmt.Sprintf("UPD-%d", u.Number), stamp(&u.CreatedAt, loc), fmt.Sprint(u.ProjectNumber), cell(str(u.ProjectName)),
			cell(str(u.OrderID)), cell(str(u.ProfileName)), cell(str(u.ClientName)), cell(u.AuthorName), cell(u.Message),
			destinationNames[u.Destination], updateStatusNames[u.Status], rev, cell(checked), stamp(u.CheckedAt, loc),
			cell(str(u.SentByName)), stamp(u.SentAt, loc), fmt.Sprint(u.AttachmentCount),
		})
	}
	c.Flush()
	return c.Error()
}

type ProjectExportFilter struct {
	Statuses []string
	Q        *string
	Range    Range
}

// WriteProjectsCSV writes every project created in the range.
func (s *Service) WriteProjectsCSV(ctx context.Context, w io.Writer, u db.User, f ProjectExportFilter) error {
	loc := s.Location(ctx)
	now := time.Now()
	rows, err := s.st.ListProjectDetails(ctx, db.ListProjectDetailsParams{
		Viewer: u.ID, Statuses: f.Statuses, Q: f.Q, Sort: "created", Lim: 50000,
	})
	if err != nil {
		return err
	}
	c := csv.NewWriter(w)
	_ = c.Write([]string{"Project #", "Project", "Status", "Profile", "Client", "Order ID", "Type", "Sales", "Assigned To",
		"Priority", "Agreed Price", "Fee %", "Fee", "Net", "Currency", "Start Date", "Deadline", "Due Date", "Overdue", "Revisions",
		"Submitted At", "Assigned At", "Completed At", "Created At"})
	for _, r := range rows {
		p := r.ProjectDetail
		if p.CreatedAt.Before(f.Range.From) || p.CreatedAt.After(f.Range.To) {
			continue
		}
		active := p.Status != "delivered" && p.Status != "completed" && p.Status != "cancelled"
		overdue := ""
		// The due date is a moment now, so compare moments: due at 17:00 is not late at noon.
		if active && passed(p.EffectiveDue, now) {
			overdue = "Yes"
		}
		_ = c.Write([]string{
			fmt.Sprint(p.Number), cell(str(p.Name)), p.Status, cell(str(p.ProfileName)), cell(str(p.ClientName)),
			cell(str(p.OrderID)), cell(str(p.ProjectTypeName)), cell(p.CreatedByName), cell(str(p.AssignedToName)),
			p.Priority, str(p.AgreedPrice), p.PlatformFeePercent, p.PlatformFee, p.NetAmount, p.Currency, str(p.StartDate), str(p.Deadline), str(p.DueDate),
			overdue, fmt.Sprint(p.RevisionCount), stamp(p.FirstSubmittedAt, loc), stamp(p.FirstAssignedAt, loc),
			stamp(p.CompletedAt, loc), stamp(&p.CreatedAt, loc),
		})
	}
	c.Flush()
	return c.Error()
}

// passed reports whether a timestamp the database rendered as text is in the past.
func passed(iso *string, now time.Time) bool {
	if iso == nil {
		return false
	}
	t, err := time.Parse(time.RFC3339, *iso)
	return err == nil && t.Before(now)
}

// scopeLead limits a Team Leader's feed to the projects they can see.
func scopeLead(u db.User) *uuid.UUID {
	if policy.Role(u.Role) == policy.TL {
		return &u.ID
	}
	return nil
}
