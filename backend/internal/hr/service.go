package hr

import (
	"context"
	"encoding/json"
	"slices"
	"strconv"
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

// Who sees what:
//   - Everyone: their own record, attendance and leave; the directory (basic fields) and who's out.
//   - A manager: the attendance and leave of everyone below them in the reporting line,
//     and approves their leave and attendance corrections. Never their own.
//   - HR and the CEO: everything; edit HR records and approve anyone's requests but their own.
//     (The admin flag is system setup only and grants nothing here.)
//
// Restricted fields (date of birth, address, emergency contact, national ID) are shown
// only to the person themselves, HR and the CEO.

type Service struct {
	st  *store.Store
	now func() time.Time
}

func NewService(st *store.Store) *Service { return &Service{st: st, now: time.Now} }

// seesAll is HR and the CEO.
func seesAll(u db.User) bool { return policy.ManagesPeople(policy.Role(u.Role)) }

// Schedule reads the working pattern from settings, falling back to the defaults.
func (s *Service) Schedule(ctx context.Context) Schedule {
	sch := DefaultSchedule()
	rows, err := s.st.ListSettings(ctx)
	if err != nil {
		return sch
	}
	for _, r := range rows {
		switch r.Key {
		case "org_timezone":
			var tz string
			if json.Unmarshal(r.Value, &tz) == nil {
				if l, err := time.LoadLocation(tz); err == nil {
					sch.Loc = l
				}
			}
		case "work_start", "work_end":
			var c string
			if json.Unmarshal(r.Value, &c) == nil {
				if m, err := ParseClock(c); err == nil {
					if r.Key == "work_start" {
						sch.WorkStart = m
					} else {
						sch.WorkEnd = m
					}
				}
			}
		case "attendance_start":
			var d string
			if json.Unmarshal(r.Value, &d) == nil {
				sch.TrackFrom = d
			}
		case "late_grace_minutes":
			var n int
			if json.Unmarshal(r.Value, &n) == nil {
				sch.GraceMinutes = n
			}
		case "weekly_off_days":
			var days []int
			if json.Unmarshal(r.Value, &days) == nil {
				sch.OffDays = map[time.Weekday]bool{}
				for _, d := range days {
					sch.OffDays[time.Weekday(d)] = true
				}
			}
		}
	}
	return sch
}

// Schedules is the default office hours plus one schedule per shift.
type Schedules struct {
	Default Schedule
	Shifts  map[uuid.UUID]Schedule
}

// For returns the schedule a person works: their shift's, or the default office hours.
func (s Schedules) For(shiftID *uuid.UUID) Schedule {
	if shiftID != nil {
		if sch, ok := s.Shifts[*shiftID]; ok {
			return sch
		}
	}
	return s.Default
}

// withShift applies a shift's hours, grace period and off days to the default schedule
// (keeping its timezone and tracking start).
func withShift(base Schedule, sh db.ListShiftsRow) Schedule {
	out := base
	id, name := sh.ID.String(), sh.Name
	out.ShiftID, out.ShiftName = &id, name
	if m, err := ParseClock(sh.StartTime); err == nil {
		out.WorkStart = m
	}
	if m, err := ParseClock(sh.EndTime); err == nil {
		out.WorkEnd = m
	}
	out.GraceMinutes = int(sh.GraceMinutes)
	out.OffDays = map[time.Weekday]bool{}
	for _, d := range sh.OffDays {
		out.OffDays[time.Weekday(d)] = true
	}
	return out
}

// AllSchedules loads the default office hours and every shift (inactive ones too, so people
// still assigned to a retired shift keep their pattern).
func (s *Service) AllSchedules(ctx context.Context) (Schedules, error) {
	out := Schedules{Default: s.Schedule(ctx), Shifts: map[uuid.UUID]Schedule{}}
	rows, err := s.st.ListShifts(ctx, false)
	for _, sh := range rows {
		out.Shifts[sh.ID] = withShift(out.Default, sh)
	}
	return out, err
}

// ScheduleFor is the schedule one person works.
func (s *Service) ScheduleFor(ctx context.Context, userID uuid.UUID) (Schedule, error) {
	all, err := s.AllSchedules(ctx)
	if err != nil {
		return Schedule{}, err
	}
	e, err := s.st.GetEmployee(ctx, userID)
	if err != nil {
		return Schedule{}, err
	}
	return all.For(e.ShiftID), nil
}

// isAbove reports whether actor is anywhere above target in the reporting line.
func isAbove(ctx context.Context, q *db.Queries, actor, target uuid.UUID) (bool, error) {
	chain, err := q.ManagerChain(ctx, target)
	return slices.Contains(chain, actor), err
}

// canReview: HR, the CEO or anyone above the person; never for themselves.
func canReview(ctx context.Context, q *db.Queries, actor db.User, target uuid.UUID) (bool, error) {
	if actor.ID == target {
		return false, nil
	}
	if seesAll(actor) {
		return true, nil
	}
	return isAbove(ctx, q, actor.ID, target)
}

// canSee: the person, anyone above them, admins and Management.
func (s *Service) canSee(ctx context.Context, actor db.User, target uuid.UUID) (bool, error) {
	if actor.ID == target || seesAll(actor) {
		return true, nil
	}
	return isAbove(ctx, s.st.Queries, actor.ID, target)
}

// scope returns the people the actor may see HR data for (nil = everyone).
func (s *Service) scope(ctx context.Context, actor db.User) ([]uuid.UUID, error) {
	if seesAll(actor) {
		return nil, nil
	}
	ids, err := s.st.ReportIDs(ctx, actor.ID)
	if err != nil {
		return nil, err
	}
	return append(ids, actor.ID), nil
}

// approvers are who a request goes to: the person's manager, else HR, else the CEO.
func approvers(ctx context.Context, q *db.Queries, userID uuid.UUID) (notify.Recipients, error) {
	e, err := q.GetEmployee(ctx, userID)
	if err != nil {
		return nil, err
	}
	if e.ManagerID != nil {
		if m, err := q.GetUserByID(ctx, *e.ManagerID); err == nil && m.IsActive {
			return notify.Recipients{m.ID}, nil
		}
	}
	for _, role := range []policy.Role{policy.HR, policy.CEO} {
		ids, err := q.ActiveUserIDsByRole(ctx, string(role))
		if err != nil {
			return nil, err
		}
		if to := notify.Recipients(ids).Without(userID); len(to) > 0 {
			return to, nil
		}
	}
	return notify.Recipients{}, nil
}

// ── Employee records ───────────────────────────────────────

type EmployeeOut struct {
	db.EmployeeDetail
	Restricted bool `json:"restricted"` // restricted fields were withheld from this viewer
}

func redact(e db.EmployeeDetail, full bool) EmployeeOut {
	if full {
		return EmployeeOut{EmployeeDetail: e}
	}
	e.DateOfBirth, e.Address, e.EmergencyContactName, e.EmergencyContactPhone, e.NationalID = nil, "", "", "", ""
	return EmployeeOut{EmployeeDetail: e, Restricted: true}
}

func (s *Service) viewEmployee(actor db.User, e db.EmployeeDetail) EmployeeOut {
	return redact(e, actor.ID == e.UserID || seesAll(actor))
}

type DirectoryEntry struct {
	EmployeeOut
	OutToday bool `json:"out_today"`
}

// Directory lists active people with basic fields, and whether each is on leave today.
func (s *Service) Directory(ctx context.Context, actor db.User) ([]DirectoryEntry, error) {
	active := true
	rows, err := s.st.ListEmployees(ctx, &active)
	if err != nil {
		return nil, err
	}
	today := s.Schedule(ctx).Today(s.now())
	out, err := s.st.ListLeaveRequests(ctx, db.ListLeaveRequestsParams{
		Statuses: []string{"approved"}, FromDate: &today, ToDate: &today,
	})
	if err != nil {
		return nil, err
	}
	away := map[uuid.UUID]bool{}
	for _, l := range out {
		away[l.UserID] = true
	}
	list := make([]DirectoryEntry, len(rows))
	for i, e := range rows {
		list[i] = DirectoryEntry{EmployeeOut: redact(e, false), OutToday: away[e.UserID]}
	}
	return list, nil
}

type EmployeeView struct {
	Employee EmployeeOut             `json:"employee"`
	CanEdit  bool                    `json:"can_edit"`
	CanSeeHR bool                    `json:"can_see_hr"` // attendance + leave
	Balances []db.LeaveBalancesRow   `json:"balances"`
	Leave    []db.LeaveRequestDetail `json:"leave"`
	Reports  []EmployeeOut           `json:"reports"` // direct reports
}

func (s *Service) GetEmployee(ctx context.Context, actor db.User, id uuid.UUID) (EmployeeView, error) {
	e, err := s.st.GetEmployee(ctx, id)
	if store.IsNotFound(err) {
		return EmployeeView{}, apperr.NotFound("Employee")
	} else if err != nil {
		return EmployeeView{}, err
	}
	v := EmployeeView{Employee: s.viewEmployee(actor, e), CanEdit: seesAll(actor), Balances: []db.LeaveBalancesRow{}, Leave: []db.LeaveRequestDetail{}}
	if v.CanSeeHR, err = s.canSee(ctx, actor, id); err != nil {
		return v, err
	}
	if v.CanSeeHR {
		year := int32(s.now().In(s.Schedule(ctx).Loc).Year())
		if v.Balances, err = s.st.LeaveBalances(ctx, db.LeaveBalancesParams{UserID: id, Year: year}); err != nil {
			return v, err
		}
		if v.Leave, err = s.st.ListLeaveRequests(ctx, db.ListLeaveRequestsParams{UserIds: []uuid.UUID{id}}); err != nil {
			return v, err
		}
	}
	all, err := s.st.ListEmployees(ctx, nil)
	if err != nil {
		return v, err
	}
	v.Reports = []EmployeeOut{}
	for _, r := range all {
		if r.ManagerID != nil && *r.ManagerID == id && r.IsActive {
			v.Reports = append(v.Reports, redact(r, false))
		}
	}
	return v, nil
}

type EmployeeInput struct {
	EmployeeCode          *string    `json:"employee_code"`
	Designation           string     `json:"designation"`
	DepartmentID          *uuid.UUID `json:"department_id"`
	ManagerID             *uuid.UUID `json:"manager_id"`
	EmploymentType        string     `json:"employment_type"`
	JoinDate              *string    `json:"join_date"`
	Phone                 string     `json:"phone"`
	DateOfBirth           *string    `json:"date_of_birth"`
	Address               string     `json:"address"`
	EmergencyContactName  string     `json:"emergency_contact_name"`
	EmergencyContactPhone string     `json:"emergency_contact_phone"`
	NationalID            string     `json:"national_id"`
	ShiftID               *uuid.UUID `json:"shift_id"` // nil = default office hours
}

var EmploymentTypes = []string{"full_time", "part_time", "intern", "contract"}

func trimPtr(p *string) *string {
	if p == nil {
		return nil
	}
	v := strings.TrimSpace(*p)
	if v == "" {
		return nil
	}
	return &v
}

func optionalDate(v validate.Errors, field string, p *string) {
	if p == nil {
		return
	}
	if _, err := ParseDate(*p); err != nil {
		v.Add(field, "must be a date (YYYY-MM-DD)")
	}
}

func personalFields(v validate.Errors, phone, address, ecName, ecPhone *string) {
	for _, f := range []struct {
		name string
		p    *string
		max  int
	}{{"phone", phone, 40}, {"address", address, 500}, {"emergency_contact_name", ecName, 120}, {"emergency_contact_phone", ecPhone, 40}} {
		*f.p = strings.TrimSpace(*f.p)
		v.MaxLen(f.name, *f.p, f.max)
	}
}

// UpdateEmployee replaces a person's HR record (HR and the CEO).
func (s *Service) UpdateEmployee(ctx context.Context, actor db.User, id uuid.UUID, in EmployeeInput) (EmployeeOut, error) {
	if !seesAll(actor) {
		return EmployeeOut{}, apperr.Forbidden()
	}
	in.EmployeeCode, in.JoinDate, in.DateOfBirth = trimPtr(in.EmployeeCode), trimPtr(in.JoinDate), trimPtr(in.DateOfBirth)
	in.Designation, in.NationalID = strings.TrimSpace(in.Designation), strings.TrimSpace(in.NationalID)
	v := validate.Errors{}
	if in.EmployeeCode != nil {
		v.MaxLen("employee_code", *in.EmployeeCode, 30)
	}
	v.MaxLen("designation", in.Designation, 120)
	v.MaxLen("national_id", in.NationalID, 40)
	v.OneOf("employment_type", in.EmploymentType, EmploymentTypes...)
	optionalDate(v, "join_date", in.JoinDate)
	optionalDate(v, "date_of_birth", in.DateOfBirth)
	personalFields(v, &in.Phone, &in.Address, &in.EmergencyContactName, &in.EmergencyContactPhone)
	if in.ManagerID != nil && *in.ManagerID == id {
		v.Add("manager_id", "cannot be the person themselves")
	}
	if !v.OK() {
		return EmployeeOut{}, apperr.Validation(v)
	}

	var out db.EmployeeDetail
	err := s.st.Tx(ctx, func(q *db.Queries) error {
		before, err := q.GetEmployee(ctx, id)
		if err != nil {
			return err
		}
		if in.ManagerID != nil {
			m, err := q.GetUserByID(ctx, *in.ManagerID)
			if store.IsNotFound(err) || (err == nil && !m.IsActive) {
				return apperr.Validation(validate.Errors{"manager_id": "must be an active user"})
			} else if err != nil {
				return err
			}
			// The new manager must not report (directly or not) to this person.
			if loop, err := isAbove(ctx, q, id, *in.ManagerID); err != nil {
				return err
			} else if loop {
				return apperr.Validation(validate.Errors{"manager_id": "reports to this person, which would create a loop"})
			}
		}
		if err := q.UpdateEmployeeHR(ctx, db.UpdateEmployeeHRParams{
			UserID: id, EmployeeCode: in.EmployeeCode, Designation: in.Designation, DepartmentID: in.DepartmentID,
			ManagerID: in.ManagerID, EmploymentType: in.EmploymentType, JoinDate: in.JoinDate, Phone: in.Phone,
			DateOfBirth: in.DateOfBirth, Address: in.Address, EmergencyContactName: in.EmergencyContactName,
			EmergencyContactPhone: in.EmergencyContactPhone, NationalID: in.NationalID, ShiftID: in.ShiftID, UpdatedBy: &actor.ID,
		}); err != nil {
			return err
		}
		if out, err = q.GetEmployee(ctx, id); err != nil {
			return err
		}
		return store.LogActivity(ctx, q, store.Activity{
			Actor: &actor.ID, Action: "employee.updated", EntityType: "employee", EntityID: &id,
			Metadata: employeeDiff(before, out),
		})
	})
	switch {
	case store.IsNotFound(err):
		return EmployeeOut{}, apperr.NotFound("Employee")
	case store.IsUniqueViolation(err):
		return EmployeeOut{}, apperr.Validation(validate.Errors{"employee_code": "is already in use"})
	case store.IsForeignKeyViolation(err):
		return EmployeeOut{}, apperr.Validation(validate.Errors{"department_id": "or shift does not exist"})
	case err != nil:
		return EmployeeOut{}, err
	}
	return EmployeeOut{EmployeeDetail: out}, nil
}

// employeeDiff records which fields changed; restricted values are not copied into the log.
func employeeDiff(a, b db.EmployeeDetail) map[string]any {
	d := map[string]any{}
	str := func(p *string) string {
		if p == nil {
			return ""
		}
		return *p
	}
	id := func(p *uuid.UUID) string {
		if p == nil {
			return ""
		}
		return p.String()
	}
	pair := func(k, x, y string) {
		if x != y {
			d[k] = [2]string{x, y}
		}
	}
	pair("employee_code", str(a.EmployeeCode), str(b.EmployeeCode))
	pair("designation", a.Designation, b.Designation)
	pair("department", str(a.DepartmentName), str(b.DepartmentName))
	pair("manager", id(a.ManagerID), id(b.ManagerID))
	pair("employment_type", a.EmploymentType, b.EmploymentType)
	pair("join_date", str(a.JoinDate), str(b.JoinDate))
	pair("phone", a.Phone, b.Phone)
	pair("shift", str(a.ShiftName), str(b.ShiftName))
	changed := []string{}
	if str(a.DateOfBirth) != str(b.DateOfBirth) {
		changed = append(changed, "date_of_birth")
	}
	if a.Address != b.Address {
		changed = append(changed, "address")
	}
	if a.EmergencyContactName != b.EmergencyContactName || a.EmergencyContactPhone != b.EmergencyContactPhone {
		changed = append(changed, "emergency_contact")
	}
	if a.NationalID != b.NationalID {
		changed = append(changed, "national_id")
	}
	if len(changed) > 0 {
		d["restricted_changed"] = changed
	}
	return d
}

type PersonalInput struct {
	Phone                 string `json:"phone"`
	Address               string `json:"address"`
	EmergencyContactName  string `json:"emergency_contact_name"`
	EmergencyContactPhone string `json:"emergency_contact_phone"`
}

// UpdatePersonal lets anyone keep their own contact details current.
func (s *Service) UpdatePersonal(ctx context.Context, actor db.User, in PersonalInput) (EmployeeOut, error) {
	v := validate.Errors{}
	personalFields(v, &in.Phone, &in.Address, &in.EmergencyContactName, &in.EmergencyContactPhone)
	if !v.OK() {
		return EmployeeOut{}, apperr.Validation(v)
	}
	var out db.EmployeeDetail
	err := s.st.Tx(ctx, func(q *db.Queries) error {
		if err := q.UpdateEmployeePersonal(ctx, db.UpdateEmployeePersonalParams{
			UserID: actor.ID, Phone: in.Phone, Address: in.Address,
			EmergencyContactName: in.EmergencyContactName, EmergencyContactPhone: in.EmergencyContactPhone,
		}); err != nil {
			return err
		}
		var err error
		if out, err = q.GetEmployee(ctx, actor.ID); err != nil {
			return err
		}
		return store.LogActivity(ctx, q, store.Activity{
			Actor: &actor.ID, Action: "employee.personal_updated", EntityType: "employee", EntityID: &actor.ID,
		})
	})
	return EmployeeOut{EmployeeDetail: out}, err
}

// ── Helpers ────────────────────────────────────────────────

func fmtDays(f float64) string { return strconv.FormatFloat(f, 'f', -1, 64) }

func parseDays(s string) float64 {
	f, _ := strconv.ParseFloat(s, 64)
	return f
}

func humanDate(s string) string {
	d, err := ParseDate(s)
	if err != nil {
		return s
	}
	return d.Format("2 Jan")
}

func dateRange(start, end string) string {
	if start == end {
		return humanDate(start)
	}
	return humanDate(start) + " – " + humanDate(end)
}

func plural(n float64, one string) string {
	if n == 1 {
		return "1 " + one
	}
	return fmtDays(n) + " " + one + "s"
}
