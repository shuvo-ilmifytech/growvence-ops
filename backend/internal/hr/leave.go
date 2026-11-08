package hr

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/solvinx/opsflow/internal/apperr"
	"github.com/solvinx/opsflow/internal/db"
	"github.com/solvinx/opsflow/internal/notify"
	"github.com/solvinx/opsflow/internal/policy"
	"github.com/solvinx/opsflow/internal/store"
	"github.com/solvinx/opsflow/internal/validate"
)

type LeaveInput struct {
	LeaveTypeID uuid.UUID `json:"leave_type_id"`
	StartDate   string    `json:"start_date"`
	EndDate     string    `json:"end_date"`
	HalfDay     bool      `json:"half_day"`
	Reason      string    `json:"reason"`
}

const (
	leaveBackdateDays = 30  // sick leave is often filed after the fact
	leaveAheadDays    = 366 // how far ahead leave can be booked
)

// Apply files a leave request for the actor. Days are counted in working days
// and checked against the balance (approved + pending) for limited leave types.
func (s *Service) Apply(ctx context.Context, actor db.User, in LeaveInput) (db.LeaveRequestDetail, error) {
	sch, err := s.ScheduleFor(ctx, actor.ID)
	if err != nil {
		return db.LeaveRequestDetail{}, err
	}
	today, _ := ParseDate(sch.Today(s.now()))
	in.Reason = strings.TrimSpace(in.Reason)
	v := validate.Errors{}
	start, err1 := ParseDate(in.StartDate)
	end, err2 := ParseDate(in.EndDate)
	switch {
	case err1 != nil:
		v.Add("start_date", "must be a date (YYYY-MM-DD)")
	case err2 != nil:
		v.Add("end_date", "must be a date (YYYY-MM-DD)")
	case end.Before(start):
		v.Add("end_date", "must be on or after the start date")
	case start.Year() != end.Year():
		v.Add("end_date", "must be in the same year as the start date; split the leave into two requests")
	case in.HalfDay && !start.Equal(end):
		v.Add("half_day", "only applies to a single day")
	case today.Sub(start) > leaveBackdateDays*24*time.Hour:
		v.Add("start_date", fmt.Sprintf("cannot be more than %d days ago", leaveBackdateDays))
	case end.Sub(today) > leaveAheadDays*24*time.Hour:
		v.Add("end_date", "is too far ahead")
	}
	v.MaxLen("reason", in.Reason, 1000)
	if !v.OK() {
		return db.LeaveRequestDetail{}, apperr.Validation(v)
	}
	lt, err := s.st.GetLeaveType(ctx, in.LeaveTypeID)
	if store.IsNotFound(err) || (err == nil && !lt.IsActive) {
		return db.LeaveRequestDetail{}, apperr.Validation(validate.Errors{"leave_type_id": "is not an available leave type"})
	} else if err != nil {
		return db.LeaveRequestDetail{}, err
	}
	hol, err := s.holidaySet(ctx, in.StartDate, in.EndDate)
	if err != nil {
		return db.LeaveRequestDetail{}, err
	}
	days := sch.LeaveDays(start, end, in.HalfDay, hol)
	if days == 0 {
		return db.LeaveRequestDetail{}, apperr.Validation(validate.Errors{"end_date": "the range has no working days (only weekly off days or holidays)"})
	}

	var out db.LeaveRequestDetail
	err = s.st.Tx(ctx, func(q *db.Queries) error {
		if err := q.LockEmployee(ctx, actor.ID); err != nil {
			return err
		}
		n, err := q.CountOverlappingLeave(ctx, db.CountOverlappingLeaveParams{UserID: actor.ID, StartDate: in.StartDate, EndDate: in.EndDate})
		if err != nil {
			return err
		}
		if n > 0 {
			return apperr.Conflict("You already have leave booked or pending on some of these days")
		}
		if lt.IsLimited {
			if left, err := remaining(ctx, q, actor.ID, lt.ID, int32(start.Year())); err != nil {
				return err
			} else if days > left {
				return apperr.Validation(validate.Errors{"leave_type_id": fmt.Sprintf(
					"not enough %s leave: %s requested, %s left in %d", lt.Name, plural(days, "day"), plural(left, "day"), start.Year())})
			}
		}
		// Step 1 is the person's manager; with no active manager it goes straight to HR.
		manager, err := activeManager(ctx, q, actor.ID)
		if err != nil {
			return err
		}
		status, to := LeaveAwaitingHR, notify.Recipients{}
		if manager != nil {
			status, to = LeavePending, to.Add(*manager)
		} else if to, err = hrReviewers(ctx, q, actor.ID); err != nil {
			return err
		}
		id, err := q.CreateLeaveRequest(ctx, db.CreateLeaveRequestParams{
			UserID: actor.ID, LeaveTypeID: lt.ID, StartDate: in.StartDate, EndDate: in.EndDate,
			HalfDay: in.HalfDay, Days: fmtDays(days), Reason: in.Reason, Status: status,
		})
		if err != nil {
			return err
		}
		if out, err = q.GetLeaveRequest(ctx, id); err != nil {
			return err
		}
		body := fmt.Sprintf("%s leave, %s (%s)", lt.Name, dateRange(in.StartDate, in.EndDate), plural(days, "day"))
		if in.Reason != "" {
			body += "\n" + truncate(in.Reason, 200)
		}
		if err := notify.Send(ctx, q, to, actor.ID, notify.Notification{
			Type: notify.LeaveRequested, Title: actor.Name + " requested leave", Body: body, Link: approvalsLink,
		}); err != nil {
			return err
		}
		return store.LogActivity(ctx, q, store.Activity{
			Actor: &actor.ID, Action: "leave.requested", EntityType: "leave_request", EntityID: &id,
			Metadata: map[string]any{"type": lt.Name, "start": in.StartDate, "end": in.EndDate, "days": fmtDays(days)},
		})
	})
	return out, err
}

// Leave statuses. Approval takes two steps: the manager (usually the Team Lead), then HR.
const (
	LeavePending    = "pending"     // waiting for the manager
	LeaveAwaitingHR = "awaiting_hr" // the manager approved; HR makes the final decision
	LeaveApproved   = "approved"
	LeaveRejected   = "rejected"
	LeaveCancelled  = "cancelled"
)

// open reports whether a request still waits for a decision.
func open(status string) bool { return status == LeavePending || status == LeaveAwaitingHR }

// activeManager is the person's manager if they have one and the account is active.
func activeManager(ctx context.Context, q *db.Queries, userID uuid.UUID) (*uuid.UUID, error) {
	e, err := q.GetEmployee(ctx, userID)
	if err != nil || e.ManagerID == nil {
		return nil, err
	}
	m, err := q.GetUserByID(ctx, *e.ManagerID)
	if err != nil || !m.IsActive {
		return nil, err
	}
	return &m.ID, nil
}

// hrReviewers make HR's final decision: the HR team, or the CEO if there's no one in HR.
func hrReviewers(ctx context.Context, q *db.Queries, exclude uuid.UUID) (notify.Recipients, error) {
	for _, role := range []policy.Role{policy.HR, policy.CEO} {
		ids, err := q.ActiveUserIDsByRole(ctx, string(role))
		if err != nil {
			return nil, err
		}
		if to := notify.Recipients(ids).Without(exclude); len(to) > 0 {
			return to, nil
		}
	}
	return notify.Recipients{}, nil
}

// remaining is entitlement minus approved and pending days for one leave type and year.
func remaining(ctx context.Context, q *db.Queries, userID, typeID uuid.UUID, year int32) (float64, error) {
	rows, err := q.LeaveBalances(ctx, db.LeaveBalancesParams{UserID: userID, Year: year})
	if err != nil {
		return 0, err
	}
	for _, r := range rows {
		if r.LeaveTypeID == typeID {
			return parseDays(r.Entitled) - parseDays(r.Approved) - parseDays(r.Pending), nil
		}
	}
	return 0, nil
}

// CancelLeave: the person cancels a request still waiting for a decision, or approved leave
// that hasn't started. HR and the CEO can cancel any open or approved request.
func (s *Service) CancelLeave(ctx context.Context, actor db.User, id uuid.UUID) (db.LeaveRequestDetail, error) {
	today := s.Schedule(ctx).Today(s.now())
	var out db.LeaveRequestDetail
	err := s.st.Tx(ctx, func(q *db.Queries) error {
		l, err := q.GetLeaveRequest(ctx, id)
		own := err == nil && l.UserID == actor.ID
		if store.IsNotFound(err) || (err == nil && !own && !seesAll(actor)) {
			return apperr.NotFound("Leave request")
		} else if err != nil {
			return err
		}
		if err := q.LockEmployee(ctx, l.UserID); err != nil {
			return err
		}
		if l, err = q.GetLeaveRequest(ctx, id); err != nil {
			return err
		}
		switch {
		case !open(l.Status) && l.Status != LeaveApproved:
			return apperr.Conflict("This request is already " + l.Status)
		case l.Status == LeaveApproved && !seesAll(actor) && l.StartDate <= today:
			return apperr.Conflict("Leave that has already started can only be cancelled by HR")
		}
		if err := q.SetLeaveStatus(ctx, db.SetLeaveStatusParams{ID: id, Status: LeaveCancelled, CancelledBy: &actor.ID}); err != nil {
			return err
		}
		if out, err = q.GetLeaveRequest(ctx, id); err != nil {
			return err
		}
		// Tell whoever else is involved: when the person cancels, the manager and (past step 1) HR;
		// when HR cancels, the person.
		to := notify.Recipients{l.UserID}
		if own {
			to = notify.Recipients{}.AddPtr(l.ManagerReviewedBy).AddPtr(l.ReviewedBy)
			if l.Status == LeavePending {
				m, err := activeManager(ctx, q, l.UserID)
				if err != nil {
					return err
				}
				to = to.AddPtr(m)
			} else if l.Status == LeaveAwaitingHR {
				hr, err := hrReviewers(ctx, q, l.UserID)
				if err != nil {
					return err
				}
				to = to.Add(hr...)
			}
		}
		if err := notify.Send(ctx, q, to, actor.ID, notify.Notification{
			Type: notify.LeaveCancelled, Title: actor.Name + " cancelled " + possessive(own, l.UserName) + " leave",
			Body: fmt.Sprintf("%s leave, %s", l.LeaveTypeName, dateRange(l.StartDate, l.EndDate)), Link: "/hr",
		}); err != nil {
			return err
		}
		return store.LogActivity(ctx, q, store.Activity{
			Actor: &actor.ID, Action: "leave.cancelled", EntityType: "leave_request", EntityID: &id,
			Metadata: map[string]any{"user_id": l.UserID, "was": l.Status},
		})
	})
	return out, err
}

func possessive(own bool, name string) string {
	if own {
		return "their"
	}
	return name + "'s"
}

// ReviewLeave decides on a request.
//   - Waiting for the manager: anyone above the person in the reporting line approves
//     (it moves on to HR) or rejects.
//   - Waiting for HR: HR or the CEO approves (final) or rejects.
//   - HR or the CEO may also decide at step 1, e.g. when the Team Lead is away; that decision is final.
//   - Nobody decides on their own leave.
func (s *Service) ReviewLeave(ctx context.Context, actor db.User, id uuid.UUID, approve bool, note string) (db.LeaveRequestDetail, error) {
	note = strings.TrimSpace(note)
	if len([]rune(note)) > 1000 {
		return db.LeaveRequestDetail{}, apperr.Validation(validate.Errors{"note": "is too long"})
	}
	if !approve && note == "" {
		return db.LeaveRequestDetail{}, apperr.Validation(validate.Errors{"note": "is required when rejecting"})
	}
	var out db.LeaveRequestDetail
	err := s.st.Tx(ctx, func(q *db.Queries) error {
		l, err := q.GetLeaveRequest(ctx, id)
		if store.IsNotFound(err) {
			return apperr.NotFound("Leave request")
		} else if err != nil {
			return err
		}
		if err := q.LockEmployee(ctx, l.UserID); err != nil {
			return err
		}
		if l, err = q.GetLeaveRequest(ctx, id); err != nil {
			return err
		}
		if !open(l.Status) {
			return apperr.Conflict("This request is already " + l.Status)
		}
		hr := seesAll(actor)
		above, err := isAbove(ctx, q, actor.ID, l.UserID)
		if err != nil {
			return err
		}
		switch {
		case actor.ID == l.UserID:
			return apperr.Forbidden()
		case l.Status == LeavePending && !above && !hr:
			return apperr.Forbidden()
		case l.Status == LeaveAwaitingHR && !hr:
			return apperr.Forbidden()
		}

		summary := fmt.Sprintf("%s leave, %s (%s)", l.LeaveTypeName, dateRange(l.StartDate, l.EndDate), plural(parseDays(l.Days), "day"))
		withNote := summary
		if note != "" {
			withNote += "\n" + note
		}

		// Step 1 done by the manager: hand over to HR.
		if approve && l.Status == LeavePending && !hr {
			if err := q.SetLeaveManagerApproved(ctx, db.SetLeaveManagerApprovedParams{ID: id, ReviewedBy: &actor.ID, Note: note}); err != nil {
				return err
			}
			if out, err = q.GetLeaveRequest(ctx, id); err != nil {
				return err
			}
			if err := notify.Send(ctx, q, notify.Recipients{l.UserID}, actor.ID, notify.Notification{
				Type: notify.LeaveApproved, Title: actor.Name + " approved your leave; waiting for HR", Body: withNote, Link: "/hr",
			}); err != nil {
				return err
			}
			reviewers, err := hrReviewers(ctx, q, l.UserID)
			if err != nil {
				return err
			}
			if err := notify.Send(ctx, q, reviewers, actor.ID, notify.Notification{
				Type: notify.LeaveRequested, Title: l.UserName + "'s leave needs final approval",
				Body: summary + "\nApproved by " + actor.Name, Link: approvalsLink,
			}); err != nil {
				return err
			}
			return store.LogActivity(ctx, q, store.Activity{
				Actor: &actor.ID, Action: "leave.manager_approved", EntityType: "leave_request", EntityID: &id,
				Metadata: map[string]any{"user_id": l.UserID, "days": l.Days},
			})
		}

		status, typ := LeaveRejected, notify.LeaveRejected
		if approve {
			status, typ = LeaveApproved, notify.LeaveApproved
		}
		if err := q.SetLeaveStatus(ctx, db.SetLeaveStatusParams{ID: id, Status: status, ReviewedBy: &actor.ID, ReviewNote: note}); err != nil {
			return err
		}
		if out, err = q.GetLeaveRequest(ctx, id); err != nil {
			return err
		}
		if err := notify.Send(ctx, q, notify.Recipients{l.UserID}, actor.ID, notify.Notification{
			Type: typ, Title: actor.Name + " " + status + " your leave", Body: withNote, Link: "/hr",
		}); err != nil {
			return err
		}
		// Keep the manager who approved step 1 in the loop on HR's decision.
		if l.ManagerReviewedBy != nil {
			if err := notify.Send(ctx, q, notify.Recipients{*l.ManagerReviewedBy}, actor.ID, notify.Notification{
				Type: typ, Title: actor.Name + " " + status + " " + l.UserName + "'s leave", Body: withNote, Link: "/hr/team",
			}); err != nil {
				return err
			}
		}
		return store.LogActivity(ctx, q, store.Activity{
			Actor: &actor.ID, Action: "leave." + status, EntityType: "leave_request", EntityID: &id,
			Metadata: map[string]any{"user_id": l.UserID, "days": l.Days, "stage": l.Status},
		})
	})
	return out, err
}

// ── Lists ──────────────────────────────────────────────────

type LeaveFilter struct {
	UserID   *uuid.UUID
	Statuses []string
	From, To *string
}

// ListLeave returns leave requests in the actor's scope.
func (s *Service) ListLeave(ctx context.Context, actor db.User, f LeaveFilter) ([]db.LeaveRequestDetail, error) {
	p := db.ListLeaveRequestsParams{Statuses: f.Statuses, FromDate: f.From, ToDate: f.To}
	if f.UserID != nil {
		if ok, err := s.canSee(ctx, actor, *f.UserID); err != nil {
			return nil, err
		} else if !ok {
			return nil, apperr.NotFound("Employee")
		}
		p.UserIds = []uuid.UUID{*f.UserID}
	} else {
		scope, err := s.scope(ctx, actor)
		if err != nil {
			return nil, err
		}
		p.UserIds = scope
	}
	return s.st.ListLeaveRequests(ctx, p)
}

type Approvals struct {
	Leave       []db.LeaveRequestDetail         `json:"leave"`
	Corrections []db.AttendanceCorrectionDetail `json:"corrections"`
}

// Approvals lists the requests waiting on the actor: their reports' leave at step 1, and for
// HR and the CEO every request at step 2. Corrections are one step (manager or HR).
func (s *Service) Approvals(ctx context.Context, actor db.User) (Approvals, error) {
	out := Approvals{Leave: []db.LeaveRequestDetail{}, Corrections: []db.AttendanceCorrectionDetail{}}
	reports, err := s.st.ReportIDs(ctx, actor.ID)
	if err != nil {
		return out, err
	}
	hr := seesAll(actor)
	if len(reports) > 0 {
		step1, err := s.st.ListLeaveRequests(ctx, db.ListLeaveRequestsParams{UserIds: reports, Statuses: []string{LeavePending}})
		if err != nil {
			return out, err
		}
		out.Leave = append(out.Leave, step1...)
	}
	if hr {
		step2, err := s.st.ListLeaveRequests(ctx, db.ListLeaveRequestsParams{Statuses: []string{LeaveAwaitingHR}})
		if err != nil {
			return out, err
		}
		for _, l := range step2 {
			if l.UserID != actor.ID {
				out.Leave = append(out.Leave, l)
			}
		}
	}
	var ids []uuid.UUID // corrections: nil = everyone, for HR and the CEO
	if !hr {
		if len(reports) == 0 {
			return out, nil
		}
		ids = reports
	}
	pending := LeavePending
	corr, err := s.st.ListCorrections(ctx, db.ListCorrectionsParams{UserIds: ids, Status: &pending})
	if err != nil {
		return out, err
	}
	for _, c := range corr {
		if c.UserID != actor.ID {
			out.Corrections = append(out.Corrections, c)
		}
	}
	return out, nil
}

// MyCorrections lists the actor's own correction requests.
func (s *Service) MyCorrections(ctx context.Context, actor db.User) ([]db.AttendanceCorrectionDetail, error) {
	return s.st.ListCorrections(ctx, db.ListCorrectionsParams{UserIds: []uuid.UUID{actor.ID}})
}

// WhosOut lists approved leave overlapping [from, to] for everyone (names and dates only).
type OutEntry struct {
	UserID        uuid.UUID `json:"user_id"`
	UserName      string    `json:"user_name"`
	LeaveTypeName string    `json:"leave_type_name"`
	StartDate     string    `json:"start_date"`
	EndDate       string    `json:"end_date"`
	HalfDay       bool      `json:"half_day"`
}

func (s *Service) WhosOut(ctx context.Context, from, to string) ([]OutEntry, error) {
	if _, err := ParseDate(from); err != nil {
		return nil, apperr.BadRequest("from must be a date (YYYY-MM-DD)")
	}
	if _, err := ParseDate(to); err != nil || to < from {
		return nil, apperr.BadRequest("to must be a date (YYYY-MM-DD), on or after from")
	}
	rows, err := s.st.ListLeaveRequests(ctx, db.ListLeaveRequestsParams{Statuses: []string{"approved"}, FromDate: &from, ToDate: &to})
	if err != nil {
		return nil, err
	}
	out := make([]OutEntry, len(rows))
	for i, l := range rows {
		out[i] = OutEntry{UserID: l.UserID, UserName: l.UserName, LeaveTypeName: l.LeaveTypeName, StartDate: l.StartDate, EndDate: l.EndDate, HalfDay: l.HalfDay}
	}
	return out, nil
}

// Balances returns one person's leave balances for a year (if the actor may see them).
func (s *Service) Balances(ctx context.Context, actor db.User, userID uuid.UUID, year int) ([]db.LeaveBalancesRow, error) {
	if ok, err := s.canSee(ctx, actor, userID); err != nil {
		return nil, err
	} else if !ok {
		return nil, apperr.NotFound("Employee")
	}
	return s.st.LeaveBalances(ctx, db.LeaveBalancesParams{UserID: userID, Year: int32(year)})
}

type AllocationInput struct {
	UserID      uuid.UUID `json:"user_id"`
	LeaveTypeID uuid.UUID `json:"leave_type_id"`
	Year        int       `json:"year"`
	Days        *float64  `json:"days"` // null removes the override (back to the type's quota)
}

// SetAllocation overrides one person's entitlement for a leave type and year (HR and the CEO).
func (s *Service) SetAllocation(ctx context.Context, actor db.User, in AllocationInput) error {
	if !seesAll(actor) {
		return apperr.Forbidden()
	}
	v := validate.Errors{}
	if in.Year < 2000 || in.Year > 2100 {
		v.Add("year", "is invalid")
	}
	if in.Days != nil && (*in.Days < 0 || *in.Days > 365 || *in.Days*2 != float64(int(*in.Days*2))) {
		v.Add("days", "must be between 0 and 365, in half days")
	}
	if !v.OK() {
		return apperr.Validation(v)
	}
	err := s.st.Tx(ctx, func(q *db.Queries) error {
		var err error
		meta := map[string]any{"user_id": in.UserID, "leave_type_id": in.LeaveTypeID, "year": in.Year}
		if in.Days == nil {
			err = q.DeleteAllocation(ctx, db.DeleteAllocationParams{UserID: in.UserID, LeaveTypeID: in.LeaveTypeID, Year: int32(in.Year)})
		} else {
			meta["days"] = *in.Days
			err = q.UpsertAllocation(ctx, db.UpsertAllocationParams{
				UserID: in.UserID, LeaveTypeID: in.LeaveTypeID, Year: int32(in.Year), Days: fmtDays(*in.Days), UpdatedBy: &actor.ID,
			})
		}
		if err != nil {
			return err
		}
		return store.LogActivity(ctx, q, store.Activity{
			Actor: &actor.ID, Action: "leave.allocation_set", EntityType: "employee", EntityID: &in.UserID, Metadata: meta,
		})
	})
	if store.IsForeignKeyViolation(err) {
		return apperr.Validation(validate.Errors{"leave_type_id": "or user does not exist"})
	}
	return err
}
