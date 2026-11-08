package reports

import (
	"context"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/solvinx/opsflow/internal/db"
	"github.com/solvinx/opsflow/internal/policy"
)

// Workload answers "who is carrying what" for the teams a Team Leader runs (the
// ones they lead, plus every team in a department they head), or for the whole
// company for the CEO. A Team Leader who runs no team sees the members who are in
// no team, mirroring the project rule (they get the unteamed projects).
type Workload struct {
	Scope   string                 `json:"scope"` // "team" or "company"
	Team    *string                `json:"team"`  // the team names, comma-separated, when scope is "team"
	Members []db.WorkloadRow       `json:"members"`
	Teams   []db.TeamThroughputRow `json:"teams"`
}

func (s *Service) Workload(ctx context.Context, u db.User) (Workload, error) {
	out := Workload{Scope: "company", Members: []db.WorkloadRow{}, Teams: []db.TeamThroughputRow{}}
	var teams []uuid.UUID
	unteamed := false
	if policy.Role(u.Role) == policy.TL {
		out.Scope = "team"
		run, err := s.st.TeamsRunBy(ctx, u.ID)
		if err != nil {
			return out, err
		}
		if len(run) == 0 {
			unteamed = true
		} else {
			names := make([]string, len(run))
			teams = make([]uuid.UUID, len(run))
			for i, t := range run {
				teams[i], names[i] = t.ID, t.Name
			}
			label := strings.Join(names, ", ")
			out.Team = &label
		}
	}
	members, err := s.st.Workload(ctx, db.WorkloadParams{Teams: teams, Unteamed: unteamed})
	if err != nil {
		return out, err
	}
	out.Members = members
	// A Team Leader with no team has no team numbers to show; without this they'd
	// get every team's, which isn't theirs to see.
	if unteamed {
		return out, nil
	}
	tp, err := s.st.TeamThroughput(ctx, teams)
	if err != nil {
		return out, err
	}
	out.Teams = tp
	return out, nil
}

// Company is the CEO's view of the whole business as of now: the numbers that
// matter today, the teams behind them, and the two lists worth acting on.
type Company struct {
	Kpis             db.CompanyKpisRow        `json:"kpis"`
	Teams            []db.TeamThroughputRow   `json:"teams"`
	RevenueThisMonth []db.CompanyRevenueRow   `json:"revenue_this_month"`
	Overdue          []db.ReportOverdueRow    `json:"overdue"`
	Unassigned       []db.ReportUnassignedRow `json:"unassigned"`
	Month            string                   `json:"month"` // the month the revenue and completions cover
}

func (s *Service) Company(ctx context.Context) (Company, error) {
	loc := s.Location(ctx)
	now := time.Now().In(loc)
	from := time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, loc)
	out := Company{
		Teams: []db.TeamThroughputRow{}, RevenueThisMonth: []db.CompanyRevenueRow{},
		Overdue: []db.ReportOverdueRow{}, Unassigned: []db.ReportUnassignedRow{},
		Month: from.Format("2006-01"),
	}
	q := s.st.Queries
	var err error
	if out.Kpis, err = q.CompanyKpis(ctx); err != nil {
		return out, err
	}
	if out.Teams, err = q.TeamThroughput(ctx, nil); err != nil {
		return out, err
	}
	if out.RevenueThisMonth, err = q.CompanyRevenue(ctx, db.CompanyRevenueParams{FromTs: from, ToTs: now}); err != nil {
		return out, err
	}
	if out.Overdue, err = q.ReportOverdue(ctx); err != nil {
		return out, err
	}
	if out.Unassigned, err = q.ReportUnassigned(ctx); err != nil {
		return out, err
	}
	return out, nil
}
