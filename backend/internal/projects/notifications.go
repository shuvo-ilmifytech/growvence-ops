package projects

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/solvinx/opsflow/internal/db"
	"github.com/solvinx/opsflow/internal/notify"
	"github.com/solvinx/opsflow/internal/policy"
	"github.com/solvinx/opsflow/internal/workflow"
)

// Who gets told about what lives in this file, so it can be audited in one place.

func projectLink(id uuid.UUID) string { return "/projects/" + id.String() }
func updateLink(id uuid.UUID) string  { return "/updates?view=all&open=" + id.String() }

func label(number int64, name *string) string {
	if name == nil || *name == "" {
		return fmt.Sprintf("#%d", number)
	}
	return fmt.Sprintf("#%d %s", number, *name)
}

func (s *Service) role(ctx context.Context, q *db.Queries, r policy.Role) notify.Recipients {
	ids, err := notify.Role(ctx, q, r)
	if err != nil {
		return nil
	}
	return ids
}

// leads are the Team Leaders who can see the project: its team's lead, or every TL
// when it has no team (or the team has no active lead).
func (s *Service) leads(ctx context.Context, q *db.Queries, projectID uuid.UUID) notify.Recipients {
	ids, err := q.ProjectLeadIDs(ctx, projectID)
	if err != nil {
		return nil
	}
	return ids
}

func (s *Service) batchWindow(ctx context.Context, q *db.Queries) int {
	if row, err := q.GetSetting(ctx, "batch_window_minutes"); err == nil {
		var n int
		if json.Unmarshal(row.Value, &n) == nil && n > 0 {
			return n
		}
	}
	return 10
}

var fieldNamesHuman = map[string]string{
	"name": "Project name", "profile_id": "Profile", "client_name": "Client name", "order_id": "Order ID",
	"client_contact": "Client contact", "project_type_id": "Project type", "scope": "Scope",
	"budget": "Budget", "agreed_price": "Agreed price", "currency": "Currency", "start_date": "Start date",
	"deadline": "Delivery deadline", "priority": "Priority", "special_notes": "Special notes",
}

// humanMoment renders a stored timestamp in the organisation's timezone, e.g. "Nov 18, 15:30".
func (s *Service) humanMoment(ctx context.Context, ts *string) string {
	return humanDate(ts, s.location(ctx))
}

// humanDate renders a stored date, or a stored moment (the delivery deadline) with its
// time, in the organisation's timezone.
func humanDate(s *string, loc *time.Location) string {
	if s == nil {
		return "none"
	}
	if t, err := time.Parse(time.DateOnly, *s); err == nil {
		return t.Format("Jan 2")
	}
	if t, err := time.Parse(timestampLayout, *s); err == nil {
		return t.In(loc).Format("Jan 2, 15:04")
	}
	return *s
}

func humanMoney(s *string, currency string) string {
	if s == nil {
		return "none"
	}
	f, err := strconv.ParseFloat(*s, 64)
	if err != nil {
		return *s
	}
	whole := strconv.FormatInt(int64(f), 10)
	for i := len(whole) - 3; i > 0; i -= 3 {
		whole = whole[:i] + "," + whole[i:]
	}
	return fmt.Sprintf("%s.%02d %s", whole, int64(f*100+0.5)%100, currency)
}

// changeLine renders one field change, e.g. "Deadline changed from Nov 10 to Nov 5".
func changeLine(field string, oldV, newV *string, currency string, loc *time.Location) string {
	name := fieldNamesHuman[field]
	switch field {
	case "scope", "special_notes":
		return name + " updated"
	case "deadline", "start_date":
		return fmt.Sprintf("%s changed from %s to %s", name, humanDate(oldV, loc), humanDate(newV, loc))
	case "budget", "agreed_price":
		return fmt.Sprintf("%s changed from %s to %s", name, humanMoney(oldV, currency), humanMoney(newV, currency))
	}
	o, n := "empty", "empty"
	if oldV != nil {
		o = *oldV
	}
	if newV != nil {
		n = *newV
	}
	if len(o) > 60 {
		o = o[:57] + "…"
	}
	if len(n) > 60 {
		n = n[:57] + "…"
	}
	return fmt.Sprintf("%s changed from %s to %s", name, o, n)
}

func capitalize(s string) string {
	if s == "" {
		return s
	}
	return strings.ToUpper(s[:1]) + s[1:]
}

func quote(body string) string {
	body = strings.Join(strings.Fields(body), " ")
	if len(body) > 120 {
		body = body[:117] + "…"
	}
	return "“" + body + "”"
}

// notifySalesEdit: Sales changed details. Important fields go out immediately and flagged;
// everything else joins the per-project batch. Recipients: assigned member and TLs.
func (s *Service) notifySalesEdit(ctx context.Context, q *db.Queries, u db.User, p db.ProjectDetail, important, other []string) error {
	to := s.leads(ctx, q, p.ID).Add(p.MemberIDs...)
	lbl := label(p.Number, p.Name)
	if len(important) > 0 {
		body := strings.Join(important, "\n")
		if len(p.MemberIDs) > 0 {
			if err := notify.Send(ctx, q, notify.Recipients{}.Add(p.MemberIDs...), u.ID, notify.Notification{
				Type: notify.ProjectImportant, ProjectID: &p.ID, Important: true, Link: projectLink(p.ID),
				Title: "Please acknowledge: " + u.Name + " changed " + lbl, Body: body,
			}); err != nil {
				return err
			}
		}
		if err := notify.Send(ctx, q, s.leads(ctx, q, p.ID), u.ID, notify.Notification{
			Type: notify.ProjectImportant, ProjectID: &p.ID, Important: true, Link: projectLink(p.ID),
			Title: u.Name + " made an important change to " + lbl, Body: body,
		}); err != nil {
			return err
		}
	}
	for i, l := range other {
		other[i] = u.Name + ": " + l
	}
	return notify.Batch(ctx, q, to, u.ID, &p.ID, nil, s.batchWindow(ctx, q), notify.ProjectChanges,
		"Changes on "+lbl, projectLink(p.ID), other...)
}

// notifyProjectActivity batches file and comment activity to everyone working on the project.
func (s *Service) notifyProjectActivity(ctx context.Context, q *db.Queries, u db.User, p db.ProjectDetail, line string) error {
	if workflow.Status(p.Status) == workflow.Draft {
		return nil // nobody else can see a draft
	}
	to := s.leads(ctx, q, p.ID).Add(p.MemberIDs...).Add(p.CreatedBy)
	return notify.Batch(ctx, q, to, u.ID, &p.ID, nil, s.batchWindow(ctx, q), notify.ProjectChanges,
		"Changes on "+label(p.Number, p.Name), projectLink(p.ID), u.Name+": "+capitalize(line))
}

func (s *Service) notifyFileChange(ctx context.Context, q *db.Queries, u db.User, projectID uuid.UUID, line string) error {
	p, err := q.GetProjectDetail(ctx, projectID)
	if err != nil {
		return err
	}
	return s.notifyProjectActivity(ctx, q, u, p, line)
}

// joinLines joins the non-empty lines of a notification body.
func joinLines(lines ...string) string {
	var out []string
	for _, l := range lines {
		if l = strings.TrimSpace(l); l != "" {
			out = append(out, l)
		}
	}
	return strings.Join(out, "\n")
}

// notifyTransition covers every project status change.
func (s *Service) notifyTransition(ctx context.Context, q *db.Queries, u db.User, p lockedProject, from, to workflow.Status, comment string) error {
	lbl := label(p.Number, p.Name)
	link := projectLink(p.ID)
	n := notify.Notification{ProjectID: &p.ID, Link: link, Body: comment}
	var recipients notify.Recipients
	switch to {
	case workflow.New:
		recipients = s.leads(ctx, q, p.ID)
		n.Type, n.Title, n.Body = notify.ProjectSubmitted, "New project to assign: "+lbl, "From "+u.Name
	case workflow.Blocked:
		recipients = s.leads(ctx, q, p.ID).Add(s.role(ctx, q, policy.CEO)...)
		n.Type, n.Title, n.Important = notify.ProjectBlocked, lbl+" is blocked", true
	case workflow.ReadyForReview:
		recipients = s.leads(ctx, q, p.ID)
		n.Type, n.Title = notify.ProjectReview, "Check before delivery: "+lbl
		n.Body = joinLines("From "+u.Name, comment)
	case workflow.ReadyToDeliver:
		// The handoff to Sales: every salesperson hears it, as they cover for each other.
		recipients = s.role(ctx, q, policy.Sales).Add(p.CreatedBy)
		n.Type, n.Title, n.Important = notify.ProjectToDeliver, "Ready to deliver on Fiverr: "+lbl, true
		n.Body = joinLines("Checked by "+u.Name, comment)
	case workflow.Delivered:
		recipients = s.leads(ctx, q, p.ID).Add(p.Members...).Add(p.CreatedBy)
		n.Type, n.Title = notify.ProjectDelivered, lbl+" was delivered"
		n.Body = joinLines("By "+u.Name, comment)
	case workflow.Completed:
		recipients = s.role(ctx, q, policy.CEO).Add(p.CreatedBy).Add(p.Members...)
		n.Type, n.Title = notify.ProjectCompleted, lbl+" is completed"
	case workflow.Cancelled:
		recipients = s.leads(ctx, q, p.ID).Add(p.Members...).Add(p.CreatedBy)
		n.Type, n.Title = notify.ProjectCancelled, lbl+" was cancelled"
	case workflow.InProgress:
		switch from {
		case workflow.ReadyForReview, workflow.ReadyToDeliver:
			recipients = notify.Recipients{}.Add(p.Members...).Add(s.leads(ctx, q, p.ID)...)
			n.Type, n.Title = notify.ProjectSentBack, "Sent back for changes: "+lbl
		case workflow.Delivered, workflow.Completed:
			recipients = notify.Recipients{}.Add(p.Members...).Add(s.leads(ctx, q, p.ID)...).Add(p.CreatedBy)
			n.Type, n.Title, n.Important = notify.ProjectRevision, "Client requested a revision on "+lbl, true
		}
	}
	if recipients == nil {
		return nil
	}
	return notify.Send(ctx, q, recipients, u.ID, n)
}

// notifyAssigned tells members who joined the project, and those taken off it.
func (s *Service) notifyAssigned(ctx context.Context, q *db.Queries, u db.User, p db.Project, added, removed []uuid.UUID, priority string, due *string) error {
	lbl := label(p.Number, p.Name)
	body := "Priority " + strings.ToUpper(priority[:1]) + priority[1:]
	if due != nil {
		body += " · due " + humanDate(due, time.UTC)
	}
	if err := notify.Send(ctx, q, notify.Recipients{}.Add(added...), u.ID, notify.Notification{
		Type: notify.ProjectAssigned, ProjectID: &p.ID, Link: projectLink(p.ID),
		Title: "You've been assigned " + lbl, Body: body,
	}); err != nil {
		return err
	}
	return notify.Send(ctx, q, notify.Recipients{}.Add(removed...), u.ID, notify.Notification{
		Type: notify.ProjectUnassigned, ProjectID: &p.ID,
		Title: "You were taken off " + lbl, Body: "You no longer have access to it.",
	})
}

// notifyMoved tells the new team's leaders, anyone taken off and the Sales owner.
func (s *Service) notifyMoved(ctx context.Context, q *db.Queries, u db.User, p lockedProject, team string, removed []uuid.UUID, to workflow.Status, comment string) error {
	lbl := label(p.Number, p.Name)
	body := joinLines("By "+u.Name, comment)
	title := lbl + " moved to " + team
	if to == workflow.New {
		title = "New project to assign: " + lbl + " (moved to " + team + ")"
	}
	if err := notify.Send(ctx, q, s.leads(ctx, q, p.ID).Add(p.CreatedBy), u.ID, notify.Notification{
		Type: notify.ProjectMoved, ProjectID: &p.ID, Link: projectLink(p.ID), Title: title, Body: body,
	}); err != nil {
		return err
	}
	return notify.Send(ctx, q, notify.Recipients{}.Add(removed...), u.ID, notify.Notification{
		Type: notify.ProjectUnassigned, ProjectID: &p.ID,
		Title: "You were taken off " + lbl, Body: "It moved to " + team + ". You no longer have access to it.",
	})
}

// ── Updates ────────────────────────────────────────────────

func (s *Service) notifyUpdate(ctx context.Context, q *db.Queries, u db.User, d db.UpdateDetail, typ string, comment string) error {
	c := updateContainer(d)
	lbl := c.Label
	upd := fmt.Sprintf("UPD-%d", d.Number)
	n := c.note(notify.Notification{Type: typ, UpdateID: &d.ID, Link: updateLink(d.ID)})
	var to notify.Recipients
	switch typ {
	case notify.UpdateSubmitted:
		to = s.leadsOn(ctx, q, c)
		n.Title, n.Body = "Update to approve: "+upd+" on "+lbl, quote(d.Message)
	case notify.UpdateApproved:
		to = notify.Recipients{}.Add(d.AuthorID, d.ProjectSalesID).Add(d.ProjectMemberIDs...)
		n.Title, n.Body = "Ready to send: "+upd+" on "+lbl, "Post it to the "+destinationName(d.Destination)+", then mark it as sent."
	case notify.UpdateChanges:
		to = notify.Recipients{}.Add(d.AuthorID).Add(d.ProjectMemberIDs...)
		n.Title, n.Body = "Changes requested on "+upd, comment
	case notify.UpdateSent:
		to = s.leadsOn(ctx, q, c).Add(d.AuthorID).Add(d.ProjectMemberIDs...)
		n.Title, n.Body = upd+" was sent to the client", lbl+" · "+destinationName(d.Destination)
	}
	return notify.Send(ctx, q, to, u.ID, n)
}

// notifyFresh re-reads the update (after the write) and notifies about it.
func (s *Service) notifyFresh(ctx context.Context, q *db.Queries, u db.User, id uuid.UUID, typ, comment string) error {
	d, err := q.GetUpdateDetail(ctx, id)
	if err != nil {
		return err
	}
	return s.notifyUpdate(ctx, q, u, d, typ, comment)
}

func (s *Service) notifyUpdateComment(ctx context.Context, q *db.Queries, u db.User, d db.UpdateDetail, thread, body string) error {
	c := updateContainer(d)
	to := s.leadsOn(ctx, q, c).Add(d.ProjectMemberIDs...).Add(d.AuthorID)
	if thread == "sales" {
		to = to.Add(d.ProjectSalesID)
	}
	return notify.Batch(ctx, q, to, u.ID, c.projectID(), c.requestID(), s.batchWindow(ctx, q), notify.UpdateComments,
		fmt.Sprintf("New comments on UPD-%d", d.Number), updateLink(d.ID), u.Name+": "+quote(body))
}

func destinationName(d string) string {
	switch d {
	case "inbox":
		return "Inbox"
	case "order_page":
		return "Order page"
	case "delivery":
		return "Delivery section"
	}
	return d
}

// ── Scheduled checks ───────────────────────────────────────

// RunChecks sends the time-based alerts. Each alert fires once per project/update
// (the *_alert_at columns), and is re-armed when the relevant date or status changes.
func (s *Service) RunChecks(ctx context.Context, now time.Time) error {
	return s.st.Tx(ctx, func(q *db.Queries) error {
		mgmt := s.role(ctx, q, policy.CEO)

		overdue, err := q.OverdueProjects(ctx)
		if err != nil {
			return err
		}
		for _, p := range overdue {
			if err := notify.Send(ctx, q, s.leads(ctx, q, p.ID).Add(mgmt...), uuid.Nil, notify.Notification{
				Type: notify.ProjectOverdue, ProjectID: &p.ID, Link: projectLink(p.ID), Important: true,
				Title: label(p.Number, p.Name) + " is overdue", Body: "It was due " + s.humanMoment(ctx, &p.Due),
			}); err != nil {
				return err
			}
			if err := q.MarkOverdueAlerted(ctx, p.ID); err != nil {
				return err
			}
		}

		waiting, err := q.UnassignedProjects(ctx, now.Add(-24*time.Hour))
		if err != nil {
			return err
		}
		for _, p := range waiting {
			if err := notify.Send(ctx, q, mgmt, uuid.Nil, notify.Notification{
				Type: notify.ProjectWaiting, ProjectID: &p.ID, Link: projectLink(p.ID),
				Title: label(p.Number, p.Name) + " has been unassigned for over 24 hours",
				Body:  "Submitted " + p.SubmittedAt.Format("Jan 2, 15:04"),
			}); err != nil {
				return err
			}
			if err := q.MarkUnassignedAlerted(ctx, p.ID); err != nil {
				return err
			}
		}

		unsent, err := q.UnsentApprovedUpdates(ctx, now.Add(-24*time.Hour))
		if err != nil {
			return err
		}
		for _, u := range unsent {
			// A project's leads, or — on a client request — every Team Leader who could
			// act on it; either way the salesperson who has to post it.
			to := notify.Recipients{}.Add(u.SalesID)
			if u.ProjectID != nil {
				to = s.leads(ctx, q, *u.ProjectID).Add(u.SalesID)
			}
			name := u.ProjectName
			if err := notify.Send(ctx, q, to, uuid.Nil, notify.Notification{
				Type: notify.UpdateUnsent, ProjectID: u.ProjectID, ClientRequestID: u.ClientRequestID,
				UpdateID: &u.ID, Link: updateLink(u.ID), Important: true,
				Title: fmt.Sprintf("UPD-%d is approved but still not sent", u.Number),
				Body:  label(u.ProjectNumber, &name) + " · approved " + u.ApprovedAt.Format("Jan 2, 15:04"),
			}); err != nil {
				return err
			}
			if err := q.MarkUnsentReminded(ctx, u.ID); err != nil {
				return err
			}
		}
		return nil
	})
}
