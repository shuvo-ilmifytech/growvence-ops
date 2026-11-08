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
	"github.com/solvinx/opsflow/internal/store"
	"github.com/solvinx/opsflow/internal/validate"
)

const approvalsLink = "/hr/approvals"

// ScheduleOut is the working pattern as the UI shows it.
type ScheduleOut struct {
	ShiftID      *string `json:"shift_id"`
	ShiftName    string  `json:"shift_name"` // "" = default office hours
	Overnight    bool    `json:"overnight"`
	Timezone     string  `json:"timezone"`
	WorkStart    string  `json:"work_start"`
	WorkEnd      string  `json:"work_end"`
	GraceMinutes int     `json:"grace_minutes"`
	OffDays      []int   `json:"off_days"`
}

func (s Schedule) Out() ScheduleOut {
	clock := func(m int) string { return fmt.Sprintf("%02d:%02d", m/60, m%60) }
	off := []int{}
	for d := time.Sunday; d <= time.Saturday; d++ {
		if s.OffDays[d] {
			off = append(off, int(d))
		}
	}
	return ScheduleOut{ShiftID: s.ShiftID, ShiftName: s.ShiftName, Overnight: s.Overnight(), Timezone: s.Loc.String(), WorkStart: clock(s.WorkStart), WorkEnd: clock(s.WorkEnd), GraceMinutes: s.GraceMinutes, OffDays: off}
}

// ── Me ─────────────────────────────────────────────────────

type MeOut struct {
	Employee      EmployeeOut           `json:"employee"`
	Today         string                `json:"today"`
	WorkDate      string                `json:"work_date"` // the shift day now belongs to (differs from today on night shifts)
	TodayRecord   *db.ListAttendanceRow `json:"today_record"`
	IsLate        bool                  `json:"is_late"`
	IsWorkingDay  bool                  `json:"is_working_day"`
	Schedule      ScheduleOut           `json:"schedule"`
	Balances      []db.LeaveBalancesRow `json:"balances"`
	PendingLeave  int                   `json:"pending_leave"`
	ApprovalCount int                   `json:"approval_count"` // requests waiting for this person to review
	ClientIP      string                `json:"client_ip"`      // the caller's public IP, set by the HTTP layer
}

func (s *Service) Me(ctx context.Context, actor db.User) (MeOut, error) {
	sch, err := s.ScheduleFor(ctx, actor.ID)
	if err != nil {
		return MeOut{}, err
	}
	now := s.now()
	today, workDate := sch.Today(now), sch.WorkDate(now)
	e, err := s.st.GetEmployee(ctx, actor.ID)
	if err != nil {
		return MeOut{}, err
	}
	out := MeOut{Employee: EmployeeOut{EmployeeDetail: e}, Today: today, WorkDate: workDate, Schedule: sch.Out()}
	rows, err := s.st.ListAttendance(ctx, db.ListAttendanceParams{UserIds: []uuid.UUID{actor.ID}, FromDate: workDate, ToDate: workDate})
	if err != nil {
		return out, err
	}
	if len(rows) == 1 {
		out.TodayRecord = &rows[0]
		out.IsLate = sch.IsLate(rows[0].CheckInAt)
	}
	hol, err := s.holidaySet(ctx, workDate, workDate)
	if err != nil {
		return out, err
	}
	d, _ := ParseDate(workDate)
	out.IsWorkingDay = sch.IsWorkingDay(d, hol)
	if out.Balances, err = s.st.LeaveBalances(ctx, db.LeaveBalancesParams{UserID: actor.ID, Year: int32(now.In(sch.Loc).Year())}); err != nil {
		return out, err
	}
	pending, err := s.st.ListLeaveRequests(ctx, db.ListLeaveRequestsParams{UserIds: []uuid.UUID{actor.ID}, Statuses: []string{LeavePending, LeaveAwaitingHR}})
	if err != nil {
		return out, err
	}
	out.PendingLeave = len(pending)
	ap, err := s.Approvals(ctx, actor)
	out.ApprovalCount = len(ap.Leave) + len(ap.Corrections)
	return out, err
}

func (s *Service) holidaySet(ctx context.Context, from, to string) (map[string]bool, error) {
	rows, err := s.st.ListHolidays(ctx, db.ListHolidaysParams{FromDate: from, ToDate: to})
	set := map[string]bool{}
	for _, h := range rows {
		set[h.Date] = true
	}
	return set, err
}

// ── Check in / out ─────────────────────────────────────────

func (s *Service) CheckIn(ctx context.Context, actor db.User, note string, pos *Position, from Origin) error {
	note = strings.TrimSpace(note)
	if len([]rune(note)) > 500 {
		return apperr.Validation(validate.Errors{"note": "is too long"})
	}
	onNet, err := s.checkNetwork(ctx, from.IP, "check in")
	if err != nil {
		return err
	}
	dist, err := s.checkLocation(ctx, pos, "check in")
	if err != nil {
		return err
	}
	sch, err := s.ScheduleFor(ctx, actor.ID)
	if err != nil {
		return err
	}
	workDate := sch.WorkDate(s.now())
	return s.st.Tx(ctx, func(q *db.Queries) error {
		id, err := q.CheckIn(ctx, db.CheckInParams{UserID: actor.ID, WorkDate: workDate, Note: note})
		if store.IsNotFound(err) {
			return apperr.Conflict("You have already checked in for this shift")
		} else if err != nil {
			return err
		}
		return store.LogActivity(ctx, q, store.Activity{Actor: &actor.ID, Action: "attendance.check_in", EntityType: "attendance", EntityID: &id, Metadata: checkMeta(dist, onNet, from)})
	})
}

func (s *Service) CheckOut(ctx context.Context, actor db.User, note string, pos *Position, from Origin) error {
	note = strings.TrimSpace(note)
	if len([]rune(note)) > 500 {
		return apperr.Validation(validate.Errors{"note": "is too long"})
	}
	onNet, err := s.checkNetwork(ctx, from.IP, "check out")
	if err != nil {
		return err
	}
	dist, err := s.checkLocation(ctx, pos, "check out")
	if err != nil {
		return err
	}
	return s.st.Tx(ctx, func(q *db.Queries) error {
		id, err := q.CheckOut(ctx, db.CheckOutParams{UserID: actor.ID, Note: note})
		if store.IsNotFound(err) {
			return apperr.Conflict("There is no open check-in to close. Request a correction instead.")
		} else if err != nil {
			return err
		}
		return store.LogActivity(ctx, q, store.Activity{Actor: &actor.ID, Action: "attendance.check_out", EntityType: "attendance", EntityID: &id, Metadata: checkMeta(dist, onNet, from)})
	})
}

// Origin is where a check-in or out came from: the client's public IP (as Caddy saw it)
// and its browser, so HR can tell an office network from mobile data or someone's home.
type Origin struct {
	IP        string
	UserAgent string
}

const maxUserAgent = 300

// checkMeta records the network and device a check-in or out came from, whether that
// network is the office's (once office IPs are set) and, when the office location check
// is on, how far from the office it was made.
func checkMeta(dist int, onNet *bool, from Origin) map[string]any {
	m := map[string]any{}
	if dist >= 0 {
		m["office_distance_m"] = dist
	}
	if onNet != nil {
		m["office_network"] = *onNet
	}
	if ip := strings.TrimSpace(from.IP); ip != "" {
		m["ip"] = ip
	}
	if ua := strings.TrimSpace(from.UserAgent); ua != "" {
		if r := []rune(ua); len(r) > maxUserAgent {
			ua = string(r[:maxUserAgent])
		}
		m["user_agent"] = ua
	}
	return m
}

// ── Attendance sheet ───────────────────────────────────────

type DayCell struct {
	Date          string     `json:"date"`
	Status        string     `json:"status"`
	CheckIn       *time.Time `json:"check_in"`
	CheckOut      *time.Time `json:"check_out"`
	WorkedMinutes int        `json:"worked_minutes"`
	LeftEarly     bool       `json:"left_early"`
	Source        string     `json:"source,omitempty"`
	LeaveType     string     `json:"leave_type,omitempty"`
	Holiday       string     `json:"holiday,omitempty"`
}

type PersonSheet struct {
	UserID         uuid.UUID      `json:"user_id"`
	Name           string         `json:"name"`
	Designation    string         `json:"designation"`
	DepartmentName *string        `json:"department_name"`
	Schedule       ScheduleOut    `json:"schedule"`
	Days           []DayCell      `json:"days"`
	Summary        map[string]int `json:"summary"`
	WorkedMinutes  int            `json:"worked_minutes"`
	// For payroll: working days in the range on the person's schedule (before they joined
	// too), and how many of them were approved unpaid leave (a half day counts 0.5).
	WorkingDays     int     `json:"working_days"`
	UnpaidLeaveDays float64 `json:"unpaid_leave_days"`
}

type Sheet struct {
	From     string               `json:"from"`
	To       string               `json:"to"`
	Today    string               `json:"today"`
	Schedule ScheduleOut          `json:"schedule"`
	Holidays []db.ListHolidaysRow `json:"holidays"`
	People   []PersonSheet        `json:"people"`
}

type SheetFilter struct {
	From, To     string
	UserID       *uuid.UUID
	DepartmentID *uuid.UUID
}

const maxSheetDays = 62

// Attendance builds a day-by-day sheet for everyone in the actor's scope (or one person).
func (s *Service) Attendance(ctx context.Context, actor db.User, f SheetFilter) (Sheet, error) {
	from, err1 := ParseDate(f.From)
	to, err2 := ParseDate(f.To)
	if err1 != nil || err2 != nil || to.Before(from) {
		return Sheet{}, apperr.BadRequest("from and to must be dates (YYYY-MM-DD), from ≤ to")
	}
	if to.Sub(from) > maxSheetDays*24*time.Hour {
		return Sheet{}, apperr.BadRequest(fmt.Sprintf("At most %d days at a time", maxSheetDays))
	}
	people, err := s.sheetPeople(ctx, actor, f)
	if err != nil {
		return Sheet{}, err
	}
	all, err := s.AllSchedules(ctx)
	if err != nil {
		return Sheet{}, err
	}
	sch := all.Default
	sheet := Sheet{From: f.From, To: f.To, Today: sch.Today(s.now()), Schedule: sch.Out(), People: []PersonSheet{}}
	shiftOf := map[uuid.UUID]*uuid.UUID{}
	for _, p := range people {
		shiftOf[p.UserID] = p.ShiftID
	}
	if sheet.Holidays, err = s.st.ListHolidays(ctx, db.ListHolidaysParams{FromDate: f.From, ToDate: f.To}); err != nil {
		return sheet, err
	}
	holidays := map[string]string{}
	holSet := map[string]bool{}
	for _, h := range sheet.Holidays {
		holidays[h.Date], holSet[h.Date] = h.Name, true
	}
	ids := make([]uuid.UUID, len(people))
	for i, p := range people {
		ids[i] = p.UserID
	}
	att, err := s.st.ListAttendance(ctx, db.ListAttendanceParams{UserIds: ids, FromDate: f.From, ToDate: f.To})
	if err != nil {
		return sheet, err
	}
	type key struct {
		u uuid.UUID
		d string
	}
	byDay := map[key]db.ListAttendanceRow{}
	for _, a := range att {
		byDay[key{a.UserID, a.WorkDate}] = a
	}
	leave, err := s.st.ListLeaveRequests(ctx, db.ListLeaveRequestsParams{
		UserIds: ids, Statuses: []string{"approved"}, FromDate: &f.From, ToDate: &f.To,
	})
	if err != nil {
		return sheet, err
	}
	onLeave := map[key]db.LeaveRequestDetail{}
	for _, l := range leave {
		ls, _ := ParseDate(l.StartDate)
		le, _ := ParseDate(l.EndDate)
		ps := all.For(shiftOf[l.UserID])
		for d := ls; !d.After(le); d = d.AddDate(0, 0, 1) {
			// Off days and holidays inside a leave range stay off days/holidays.
			if ps.IsWorkingDay(d, holSet) {
				onLeave[key{l.UserID, d.Format(time.DateOnly)}] = l
			}
		}
	}

	for _, p := range people {
		sch := all.For(p.ShiftID)
		start := sch.StartFor(p.JoinDate)
		ps := PersonSheet{UserID: p.UserID, Name: p.Name, Designation: p.Designation, DepartmentName: p.DepartmentName,
			Schedule: sch.Out(), Days: []DayCell{}, Summary: map[string]int{}}
		for d := from; !d.After(to); d = d.AddDate(0, 0, 1) {
			ds := d.Format(time.DateOnly)
			cell := DayCell{Date: ds, Holiday: holidays[ds]}
			in := DayInput{Date: d, IsHoliday: holSet[ds], StartFrom: start}
			if a, ok := byDay[key{p.UserID, ds}]; ok {
				in.CheckIn = &a.CheckInAt
				cell.CheckIn, cell.CheckOut, cell.Source = &a.CheckInAt, a.CheckOutAt, a.Source
				if a.CheckOutAt != nil {
					cell.WorkedMinutes = int(a.CheckOutAt.Sub(a.CheckInAt).Minutes())
					cell.LeftEarly = sch.LeftEarly(a.WorkDate, *a.CheckOutAt)
				}
			}
			if sch.IsWorkingDay(d, holSet) {
				ps.WorkingDays++
			}
			if l, ok := onLeave[key{p.UserID, ds}]; ok {
				in.OnLeave, in.HalfDay, cell.LeaveType = true, l.HalfDay, l.LeaveTypeName
				if !l.IsPaid && l.HalfDay {
					ps.UnpaidLeaveDays += 0.5
				} else if !l.IsPaid {
					ps.UnpaidLeaveDays++
				}
			}
			cell.Status = sch.DayStatus(in, sheet.Today)
			ps.Summary[cell.Status]++
			ps.WorkedMinutes += cell.WorkedMinutes
			ps.Days = append(ps.Days, cell)
		}
		sheet.People = append(sheet.People, ps)
	}
	return sheet, nil
}

func (s *Service) sheetPeople(ctx context.Context, actor db.User, f SheetFilter) ([]db.EmployeeDetail, error) {
	if f.UserID != nil {
		ok, err := s.canSee(ctx, actor, *f.UserID)
		if err != nil {
			return nil, err
		}
		e, err := s.st.GetEmployee(ctx, *f.UserID)
		if !ok || store.IsNotFound(err) {
			return nil, apperr.NotFound("Employee")
		} else if err != nil {
			return nil, err
		}
		return []db.EmployeeDetail{e}, nil
	}
	scope, err := s.scope(ctx, actor)
	if err != nil {
		return nil, err
	}
	active := true
	all, err := s.st.ListEmployees(ctx, &active)
	if err != nil {
		return nil, err
	}
	out := []db.EmployeeDetail{}
	for _, e := range all {
		if scope != nil && !containsID(scope, e.UserID) {
			continue
		}
		if f.DepartmentID != nil && (e.DepartmentID == nil || *e.DepartmentID != *f.DepartmentID) {
			continue
		}
		out = append(out, e)
	}
	return out, nil
}

func containsID(ids []uuid.UUID, id uuid.UUID) bool {
	for _, x := range ids {
		if x == id {
			return true
		}
	}
	return false
}

// ── Corrections ────────────────────────────────────────────

type CorrectionInput struct {
	WorkDate string  `json:"work_date"`
	CheckIn  string  `json:"check_in"`  // "HH:MM" local time
	CheckOut *string `json:"check_out"` // optional
	Reason   string  `json:"reason"`
}

const correctionWindowDays = 31

func (s *Service) RequestCorrection(ctx context.Context, actor db.User, in CorrectionInput) (db.AttendanceCorrectionDetail, error) {
	sch, err := s.ScheduleFor(ctx, actor.ID)
	if err != nil {
		return db.AttendanceCorrectionDetail{}, err
	}
	today := sch.WorkDate(s.now())
	in.Reason = strings.TrimSpace(in.Reason)
	v := validate.Errors{}
	day, err := ParseDate(in.WorkDate)
	if err != nil {
		v.Add("work_date", "must be a date (YYYY-MM-DD)")
	} else if in.WorkDate > today {
		v.Add("work_date", "cannot be in the future")
	} else if t, _ := ParseDate(today); t.Sub(day) > correctionWindowDays*24*time.Hour {
		v.Add("work_date", fmt.Sprintf("must be within the last %d days", correctionWindowDays))
	}
	checkIn, err := sch.At(in.WorkDate, in.CheckIn)
	if err != nil {
		v.Add("check_in", "must be a time (HH:MM)")
	}
	var checkOut *time.Time
	if in.CheckOut != nil && strings.TrimSpace(*in.CheckOut) != "" {
		t, err := sch.At(in.WorkDate, strings.TrimSpace(*in.CheckOut))
		if err == nil && !t.After(checkIn) {
			t = t.AddDate(0, 0, 1) // e.g. in 22:00, out 06:00: the next morning
		}
		switch {
		case err != nil:
			v.Add("check_out", "must be a time (HH:MM)")
		case t.Sub(checkIn) > 20*time.Hour:
			v.Add("check_out", "is more than 20 hours after check-in")
		default:
			checkOut = &t
		}
	}
	v.Len("reason", in.Reason, 1, 1000)
	if !v.OK() {
		return db.AttendanceCorrectionDetail{}, apperr.Validation(v)
	}
	var out db.AttendanceCorrectionDetail
	err = s.st.Tx(ctx, func(q *db.Queries) error {
		id, err := q.CreateCorrection(ctx, db.CreateCorrectionParams{
			UserID: actor.ID, WorkDate: in.WorkDate, CheckInAt: checkIn, CheckOutAt: checkOut, Reason: in.Reason,
		})
		if err != nil {
			return err
		}
		if out, err = q.GetCorrection(ctx, id); err != nil {
			return err
		}
		to, err := approvers(ctx, q, actor.ID)
		if err != nil {
			return err
		}
		if err := notify.Send(ctx, q, to, actor.ID, notify.Notification{
			Type: notify.CorrectionAsked, Title: actor.Name + " asked for an attendance correction",
			Body: humanDate(in.WorkDate) + ": " + in.Reason, Link: approvalsLink,
		}); err != nil {
			return err
		}
		return store.LogActivity(ctx, q, store.Activity{
			Actor: &actor.ID, Action: "attendance.correction_requested", EntityType: "attendance_correction", EntityID: &id,
			Metadata: map[string]any{"work_date": in.WorkDate},
		})
	})
	if store.IsUniqueViolation(err) {
		return out, apperr.Conflict("You already have a pending correction for this day")
	}
	return out, err
}

func (s *Service) CancelCorrection(ctx context.Context, actor db.User, id uuid.UUID) error {
	return s.st.Tx(ctx, func(q *db.Queries) error {
		c, err := q.GetCorrection(ctx, id)
		if store.IsNotFound(err) || (err == nil && c.UserID != actor.ID) {
			return apperr.NotFound("Correction")
		} else if err != nil {
			return err
		}
		if c.Status != "pending" {
			return apperr.Conflict("Only pending corrections can be cancelled")
		}
		if err := q.SetCorrectionStatus(ctx, db.SetCorrectionStatusParams{ID: id, Status: "cancelled"}); err != nil {
			return err
		}
		return store.LogActivity(ctx, q, store.Activity{Actor: &actor.ID, Action: "attendance.correction_cancelled", EntityType: "attendance_correction", EntityID: &id})
	})
}

func (s *Service) ReviewCorrection(ctx context.Context, actor db.User, id uuid.UUID, approve bool, note string) (db.AttendanceCorrectionDetail, error) {
	note = strings.TrimSpace(note)
	if len([]rune(note)) > 1000 {
		return db.AttendanceCorrectionDetail{}, apperr.Validation(validate.Errors{"note": "is too long"})
	}
	if !approve && note == "" {
		return db.AttendanceCorrectionDetail{}, apperr.Validation(validate.Errors{"note": "is required when rejecting"})
	}
	var out db.AttendanceCorrectionDetail
	err := s.st.Tx(ctx, func(q *db.Queries) error {
		c, err := q.GetCorrection(ctx, id)
		if store.IsNotFound(err) {
			return apperr.NotFound("Correction")
		} else if err != nil {
			return err
		}
		if err := q.LockEmployee(ctx, c.UserID); err != nil {
			return err
		}
		if ok, err := canReview(ctx, q, actor, c.UserID); err != nil {
			return err
		} else if !ok {
			return apperr.Forbidden()
		}
		if c, err = q.GetCorrection(ctx, id); err != nil {
			return err
		}
		if c.Status != "pending" {
			return apperr.Conflict("This correction has already been " + c.Status)
		}
		status, typ, verb := "rejected", notify.CorrectionDone, "rejected"
		if approve {
			status, verb = "approved", "approved"
			if err := q.UpsertAttendance(ctx, db.UpsertAttendanceParams{
				UserID: c.UserID, WorkDate: c.WorkDate, CheckInAt: c.CheckInAt, CheckOutAt: c.CheckOutAt,
				Source: "correction", Note: truncate("Corrected: "+c.Reason, 500),
			}); err != nil {
				return err
			}
		}
		if err := q.SetCorrectionStatus(ctx, db.SetCorrectionStatusParams{ID: id, Status: status, ReviewedBy: &actor.ID, ReviewNote: note}); err != nil {
			return err
		}
		if out, err = q.GetCorrection(ctx, id); err != nil {
			return err
		}
		body := humanDate(c.WorkDate)
		if note != "" {
			body += ": " + note
		}
		if err := notify.Send(ctx, q, notify.Recipients{c.UserID}, actor.ID, notify.Notification{
			Type: typ, Title: actor.Name + " " + verb + " your attendance correction", Body: body, Link: "/hr",
		}); err != nil {
			return err
		}
		return store.LogActivity(ctx, q, store.Activity{
			Actor: &actor.ID, Action: "attendance.correction_" + status, EntityType: "attendance_correction", EntityID: &id,
			Metadata: map[string]any{"user_id": c.UserID, "work_date": c.WorkDate},
		})
	})
	return out, err
}

func truncate(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n-1]) + "…"
}
