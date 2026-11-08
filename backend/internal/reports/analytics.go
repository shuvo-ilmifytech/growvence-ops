package reports

import (
	"context"
	"encoding/csv"
	"fmt"
	"io"
	"time"

	"github.com/google/uuid"

	"github.com/solvinx/opsflow/internal/db"
)

// Analytics is the Analytics page: how much work came in and went out over a range, what
// it earned, how each team and person performed, where time goes, and where the work
// stands now. Revenue is what was received after the marketplace's fee, as on the CEO's
// dashboard, so whoever the CEO opens the page to sees it too.
type Analytics struct {
	Range     map[string]string          `json:"range"`
	Previous  map[string]string          `json:"previous_range"` // the same length just before
	Bucket    string                     `json:"bucket"`         // day, week or month: the series' step
	TeamID    *uuid.UUID                 `json:"team_id"`
	Totals    db.AnalyticsTotalsRow      `json:"totals"`
	PrevTotal db.AnalyticsTotalsRow      `json:"previous_totals"`
	Revenue   []db.AnalyticsRevenueRow   `json:"revenue"` // per team and currency
	PrevRev   []db.AnalyticsRevenueRow   `json:"previous_revenue"`
	Series    []db.AnalyticsSeriesRow    `json:"series"`
	Teams     []db.AnalyticsTeamsRow     `json:"teams"` // always every team, to compare against
	People    []db.AnalyticsPeopleRow    `json:"people"`
	Types     []db.AnalyticsByTypeRow    `json:"types"`
	Sales     []db.AnalyticsSalesRow     `json:"sales"`
	Steps     []db.AnalyticsStepTimesRow `json:"step_times"` // time in each step, delivered projects
	StuckNow  []db.AnalyticsStuckNowRow  `json:"stuck_now"`  // open projects, time in their current step
	Lateness  db.AnalyticsLatenessRow    `json:"lateness"`
}

// Previous is the range of the same length that ends just before r starts.
func (r Range) Previous() Range {
	span := r.To.Sub(r.From) + time.Nanosecond
	from := r.From.Add(-span)
	to := r.From.Add(-time.Nanosecond)
	return Range{From: from, To: to, FromDay: from.Format(time.DateOnly), ToDay: to.Format(time.DateOnly)}
}

// AutoBucket picks a step that keeps the chart readable: days up to a month, weeks up to
// half a year, months beyond.
func AutoBucket(r Range) string {
	days := r.To.Sub(r.From).Hours() / 24
	switch {
	case days <= 45:
		return "day"
	case days <= 190:
		return "week"
	}
	return "month"
}

func (s *Service) Analytics(ctx context.Context, r Range, bucket string, team *uuid.UUID) (Analytics, error) {
	q := s.st.Queries
	prev := r.Previous()
	out := Analytics{
		Range: map[string]string{"from": r.FromDay, "to": r.ToDay}, Previous: map[string]string{"from": prev.FromDay, "to": prev.ToDay},
		Bucket: bucket, TeamID: team,
	}
	var err error
	if out.Totals, err = q.AnalyticsTotals(ctx, db.AnalyticsTotalsParams{FromTs: r.From, ToTs: r.To, TeamID: team}); err != nil {
		return out, err
	}
	if out.PrevTotal, err = q.AnalyticsTotals(ctx, db.AnalyticsTotalsParams{FromTs: prev.From, ToTs: prev.To, TeamID: team}); err != nil {
		return out, err
	}
	if out.Revenue, err = q.AnalyticsRevenue(ctx, db.AnalyticsRevenueParams{FromTs: r.From, ToTs: r.To, TeamID: team}); err != nil {
		return out, err
	}
	if out.PrevRev, err = q.AnalyticsRevenue(ctx, db.AnalyticsRevenueParams{FromTs: prev.From, ToTs: prev.To, TeamID: team}); err != nil {
		return out, err
	}
	if out.Steps, err = q.AnalyticsStepTimes(ctx, db.AnalyticsStepTimesParams{FromTs: r.From, ToTs: r.To, TeamID: team}); err != nil {
		return out, err
	}
	if out.StuckNow, err = q.AnalyticsStuckNow(ctx, team); err != nil {
		return out, err
	}
	if out.Lateness, err = q.AnalyticsLateness(ctx, db.AnalyticsLatenessParams{FromTs: r.From, ToTs: r.To, TeamID: team}); err != nil {
		return out, err
	}
	if out.Series, err = q.AnalyticsSeries(ctx, db.AnalyticsSeriesParams{FromTs: r.From, ToTs: r.To, Bucket: bucket, TeamID: team}); err != nil {
		return out, err
	}
	if out.Teams, err = q.AnalyticsTeams(ctx, db.AnalyticsTeamsParams{FromTs: r.From, ToTs: r.To}); err != nil {
		return out, err
	}
	if out.People, err = q.AnalyticsPeople(ctx, db.AnalyticsPeopleParams{FromTs: r.From, ToTs: r.To, TeamID: team}); err != nil {
		return out, err
	}
	if out.Types, err = q.AnalyticsByType(ctx, db.AnalyticsByTypeParams{FromTs: r.From, ToTs: r.To, TeamID: team}); err != nil {
		return out, err
	}
	if out.Sales, err = q.AnalyticsSales(ctx, db.AnalyticsSalesParams{FromTs: r.From, ToTs: r.To, TeamID: team}); err != nil {
		return out, err
	}
	return out, nil
}

// ── The projects behind a number ───────────────────────────

// Metrics are the numbers on the page a viewer can open as a list of projects.
var Metrics = []string{
	"incoming", "delivered", "completed", "cancelled", "on_time", "late", "revisions",
	"open", "member_open", "overdue", "awaiting_client", "new", "in_progress", "blocked", "ready_for_review", "ready_to_deliver",
}

type ProjectFilter struct {
	Metric                    string
	Range                     Range
	Team, Type, Sales, Person *uuid.UUID
}

func (s *Service) AnalyticsProjects(ctx context.Context, f ProjectFilter) ([]db.AnalyticsProjectsRow, error) {
	return s.st.AnalyticsProjects(ctx, db.AnalyticsProjectsParams{
		Metric: f.Metric, FromTs: f.Range.From, ToTs: f.Range.To,
		TeamID: f.Team, TypeID: f.Type, SalesID: f.Sales, UserID: f.Person,
	})
}

func hours(v any) string {
	if f, ok := v.(float64); ok {
		return fmt.Sprintf("%.1f", f)
	}
	return ""
}

// WriteAnalyticsCSV writes every project that came in or was delivered in the range, with
// its timings and money, for working on the numbers in a spreadsheet.
func (s *Service) WriteAnalyticsCSV(ctx context.Context, w io.Writer, r Range, team *uuid.UUID) error {
	loc := s.Location(ctx)
	rows, err := s.AnalyticsProjects(ctx, ProjectFilter{Metric: "all", Range: r, Team: team})
	if err != nil {
		return err
	}
	c := csv.NewWriter(w)
	_ = c.Write([]string{"Project #", "Project", "Status", "Team", "Type", "Sales", "Worked on by", "Client", "Order ID",
		"Received", "Assigned", "Delivered", "Completed", "Cancelled", "Due", "Hours late (minus = early)", "Hours order to delivery",
		"Revisions", "Agreed price", "Net after fee", "Currency"})
	for _, p := range rows {
		due := ""
		if t, err := time.Parse(time.RFC3339, p.Due); err == nil {
			due = t.In(loc).Format("2006-01-02 15:04")
		}
		_ = c.Write([]string{
			fmt.Sprint(p.Number), cell(str(p.Name)), p.Status, cell(str(p.TeamName)), cell(str(p.TypeName)), cell(p.SalesName),
			cell(p.Members), cell(str(p.ClientName)), cell(str(p.OrderID)),
			stamp(p.FirstSubmittedAt, loc), stamp(p.FirstAssignedAt, loc), stamp(p.DeliveredAt, loc), stamp(p.CompletedAt, loc),
			stamp(p.CancelledAt, loc), due, hours(p.LateH), hours(p.TurnaroundH),
			fmt.Sprint(p.RevisionCount), p.AgreedPrice, p.Net, p.Currency,
		})
	}
	c.Flush()
	return c.Error()
}

// ── Trend ──────────────────────────────────────────────────

type TrendTeam struct {
	ID     uuid.UUID                 `json:"id"`
	Name   string                    `json:"name"`
	Months map[string]TrendTeamMonth `json:"months"`
}

type TrendTeamMonth struct {
	Delivered int64             `json:"delivered"`
	Revenue   map[string]string `json:"revenue"` // currency -> net after fee
	Target    *TargetAmount     `json:"target"`
}

type TargetAmount struct {
	Amount   string `json:"amount"`
	Currency string `json:"currency"`
}

type TrendPerson struct {
	ID       uuid.UUID        `json:"id"`
	Name     string           `json:"name"`
	TeamName *string          `json:"team_name"`
	Months   map[string]int64 `json:"months"` // deliveries per month
}

type Trend struct {
	Months []string      `json:"months"` // YYYY-MM, oldest first
	Teams  []TrendTeam   `json:"teams"`
	People []TrendPerson `json:"people"`
}

// Trend is deliveries per person and per team (with revenue and target) for the last n
// months, this month included.
func (s *Service) Trend(ctx context.Context, n int, team *uuid.UUID) (Trend, error) {
	loc := s.Location(ctx)
	now := time.Now().In(loc)
	first := time.Date(now.Year(), now.Month()-time.Month(n-1), 1, 0, 0, 0, 0, loc)
	out := Trend{Teams: []TrendTeam{}, People: []TrendPerson{}}
	for i := 0; i < n; i++ {
		out.Months = append(out.Months, first.AddDate(0, i, 0).Format("2006-01"))
	}
	q := s.st.Queries
	teams, err := q.AnalyticsTeams(ctx, db.AnalyticsTeamsParams{FromTs: first, ToTs: now})
	if err != nil {
		return out, err
	}
	idx := map[uuid.UUID]int{}
	for _, t := range teams {
		if team != nil && t.ID != *team {
			continue
		}
		idx[t.ID] = len(out.Teams)
		tt := TrendTeam{ID: t.ID, Name: t.Name, Months: map[string]TrendTeamMonth{}}
		for _, m := range out.Months {
			tt.Months[m] = TrendTeamMonth{Revenue: map[string]string{}}
		}
		out.Teams = append(out.Teams, tt)
	}
	tm, err := q.AnalyticsTeamTrend(ctx, db.AnalyticsTeamTrendParams{FromTs: first, TeamID: team})
	if err != nil {
		return out, err
	}
	for _, r := range tm {
		if r.TeamID == nil {
			continue
		}
		i, ok := idx[*r.TeamID]
		if !ok {
			continue
		}
		m, ok := out.Teams[i].Months[r.Month]
		if !ok {
			continue
		}
		m.Delivered += r.Delivered
		if r.Net != "0" && r.Net != "0.00" {
			m.Revenue[r.Currency] = r.Net
		}
		out.Teams[i].Months[r.Month] = m
	}
	targets, err := q.ListTargetHistory(ctx, out.Months[0]+"-01")
	if err != nil {
		return out, err
	}
	for _, t := range targets {
		i, ok := idx[t.TeamID]
		if !ok {
			continue
		}
		if m, ok := out.Teams[i].Months[t.Month]; ok {
			m.Target = &TargetAmount{Amount: t.Amount, Currency: t.Currency}
			out.Teams[i].Months[t.Month] = m
		}
	}
	people, err := q.AnalyticsPeople(ctx, db.AnalyticsPeopleParams{FromTs: first, ToTs: now, TeamID: team})
	if err != nil {
		return out, err
	}
	pidx := map[uuid.UUID]int{}
	for _, p := range people {
		pidx[p.ID] = len(out.People)
		out.People = append(out.People, TrendPerson{ID: p.ID, Name: p.Name, TeamName: p.TeamName, Months: map[string]int64{}})
	}
	pm, err := q.AnalyticsPersonTrend(ctx, db.AnalyticsPersonTrendParams{FromTs: first, TeamID: team})
	if err != nil {
		return out, err
	}
	for _, r := range pm {
		if i, ok := pidx[r.UserID]; ok {
			out.People[i].Months[r.Month] = r.Delivered
		}
	}
	return out, nil
}

// ── Targets ────────────────────────────────────────────────

// Targets is every active team's revenue target for one month and how far along it is.
type Targets struct {
	Month   string                  `json:"month"`   // YYYY-MM
	Elapsed float64                 `json:"elapsed"` // share of the month gone: 0 before it, 1 after
	Teams   []db.ListTeamTargetsRow `json:"teams"`
}

// MonthRange is a calendar month (YYYY-MM) in the org timezone.
func (s *Service) MonthRange(ctx context.Context, month string) (time.Time, time.Time, error) {
	loc := s.Location(ctx)
	t, err := time.ParseInLocation("2006-01", month, loc)
	if err != nil {
		return t, t, err
	}
	return t, t.AddDate(0, 1, 0).Add(-time.Nanosecond), nil
}

func (s *Service) Targets(ctx context.Context, month string) (Targets, error) {
	from, to, err := s.MonthRange(ctx, month)
	if err != nil {
		return Targets{}, err
	}
	out := Targets{Month: month}
	now := time.Now()
	switch {
	case now.After(to):
		out.Elapsed = 1
	case now.After(from):
		out.Elapsed = float64(now.Sub(from)) / float64(to.Sub(from))
	}
	out.Teams, err = s.st.ListTeamTargets(ctx, db.ListTeamTargetsParams{FromTs: from, ToTs: to, Month: month + "-01"})
	return out, err
}
