package httpapi

import (
	"bytes"
	"strconv"
	"strings"
	"time"

	"github.com/gofiber/fiber/v3"
	"github.com/google/uuid"

	"github.com/solvinx/opsflow/internal/db"
	"github.com/solvinx/opsflow/internal/hr"
	"github.com/solvinx/opsflow/internal/store"
	"github.com/solvinx/opsflow/internal/validate"
)

// HRM handlers are thin: access rules and validation live in the hr service.

var leaveStatuses = []string{"pending", "awaiting_hr", "approved", "rejected", "cancelled"}

func queryUUID(c fiber.Ctx, v validate.Errors, key string) *uuid.UUID {
	raw := c.Query(key)
	if raw == "" {
		return nil
	}
	id, err := uuid.Parse(raw)
	if err != nil {
		v.Add(key, "is not a valid id")
		return nil
	}
	return &id
}

func queryDate(c fiber.Ctx, v validate.Errors, key string) *string {
	raw := c.Query(key)
	if raw == "" {
		return nil
	}
	if _, err := hr.ParseDate(raw); err != nil {
		v.Add(key, "must be a date (YYYY-MM-DD)")
		return nil
	}
	return &raw
}

type noteReq struct {
	Note string `json:"note"`
}

// optionalNote decodes {"note": "..."} when a body was sent.
func optionalNote(c fiber.Ctx) (string, error) {
	if len(bytes.TrimSpace(c.Body())) == 0 {
		return "", nil
	}
	var req noteReq
	if err := decodeJSON(c, &req); err != nil {
		return "", err
	}
	return req.Note, nil
}

// ── Self service ───────────────────────────────────────────

func (s *Server) hrMe(c fiber.Ctx) error {
	out, err := s.hr.Me(c.Context(), currentUser(c))
	if err != nil {
		return err
	}
	out.ClientIP = c.IP()
	return c.JSON(out)
}

func (s *Server) hrUpdateMyProfile(c fiber.Ctx) error {
	var req hr.PersonalInput
	if err := decodeJSON(c, &req); err != nil {
		return err
	}
	e, err := s.hr.UpdatePersonal(c.Context(), currentUser(c), req)
	if err != nil {
		return err
	}
	return c.JSON(fiber.Map{"employee": e})
}

// checkReq is a check-in/out: an optional note and, when the office location check is
// on, where the device is.
type checkReq struct {
	Note     string   `json:"note"`
	Lat      *float64 `json:"lat"`
	Lng      *float64 `json:"lng"`
	Accuracy float64  `json:"accuracy"`
}

func decodeCheck(c fiber.Ctx) (string, *hr.Position, error) {
	if len(bytes.TrimSpace(c.Body())) == 0 {
		return "", nil, nil
	}
	var req checkReq
	if err := decodeJSON(c, &req); err != nil {
		return "", nil, err
	}
	if req.Lat == nil || req.Lng == nil {
		return req.Note, nil, nil
	}
	return req.Note, &hr.Position{Lat: *req.Lat, Lng: *req.Lng, Accuracy: req.Accuracy}, nil
}

// clientOrigin is the caller's public IP and browser, recorded with each check-in/out.
func clientOrigin(c fiber.Ctx) hr.Origin {
	return hr.Origin{IP: c.IP(), UserAgent: c.Get(fiber.HeaderUserAgent)}
}

func (s *Server) hrCheckIn(c fiber.Ctx) error {
	note, pos, err := decodeCheck(c)
	if err != nil {
		return err
	}
	if err := s.hr.CheckIn(c.Context(), currentUser(c), note, pos, clientOrigin(c)); err != nil {
		return err
	}
	return s.hrMe(c)
}

func (s *Server) hrCheckOut(c fiber.Ctx) error {
	note, pos, err := decodeCheck(c)
	if err != nil {
		return err
	}
	if err := s.hr.CheckOut(c.Context(), currentUser(c), note, pos, clientOrigin(c)); err != nil {
		return err
	}
	return s.hrMe(c)
}

func (s *Server) hrMyCorrections(c fiber.Ctx) error {
	rows, err := s.hr.MyCorrections(c.Context(), currentUser(c))
	if err != nil {
		return err
	}
	return c.JSON(fiber.Map{"corrections": rows})
}

func (s *Server) hrRequestCorrection(c fiber.Ctx) error {
	var req hr.CorrectionInput
	if err := decodeJSON(c, &req); err != nil {
		return err
	}
	out, err := s.hr.RequestCorrection(c.Context(), currentUser(c), req)
	if err != nil {
		return err
	}
	return c.Status(fiber.StatusCreated).JSON(fiber.Map{"correction": out})
}

func (s *Server) hrCancelCorrection(c fiber.Ctx) error {
	id, err := paramUUID(c, "id")
	if err != nil {
		return err
	}
	if err := s.hr.CancelCorrection(c.Context(), currentUser(c), id); err != nil {
		return err
	}
	return c.SendStatus(fiber.StatusNoContent)
}

func (s *Server) hrApplyLeave(c fiber.Ctx) error {
	var req hr.LeaveInput
	if err := decodeJSON(c, &req); err != nil {
		return err
	}
	out, err := s.hr.Apply(c.Context(), currentUser(c), req)
	if err != nil {
		return err
	}
	return c.Status(fiber.StatusCreated).JSON(fiber.Map{"leave": out})
}

func (s *Server) hrCancelLeave(c fiber.Ctx) error {
	id, err := paramUUID(c, "id")
	if err != nil {
		return err
	}
	out, err := s.hr.CancelLeave(c.Context(), currentUser(c), id)
	if err != nil {
		return err
	}
	return c.JSON(fiber.Map{"leave": out})
}

// ── Shared reads ───────────────────────────────────────────

func (s *Server) hrDirectory(c fiber.Ctx) error {
	rows, err := s.hr.Directory(c.Context(), currentUser(c))
	if err != nil {
		return err
	}
	return c.JSON(fiber.Map{"employees": rows})
}

func (s *Server) hrGetEmployee(c fiber.Ctx) error {
	id, err := paramUUID(c, "id")
	if err != nil {
		return err
	}
	out, err := s.hr.GetEmployee(c.Context(), currentUser(c), id)
	if err != nil {
		return err
	}
	return c.JSON(out)
}

func (s *Server) hrUpdateEmployee(c fiber.Ctx) error {
	id, err := paramUUID(c, "id")
	if err != nil {
		return err
	}
	var req hr.EmployeeInput
	if err := decodeJSON(c, &req); err != nil {
		return err
	}
	e, err := s.hr.UpdateEmployee(c.Context(), currentUser(c), id, req)
	if err != nil {
		return err
	}
	return c.JSON(fiber.Map{"employee": e})
}

func (s *Server) hrBalances(c fiber.Ctx) error {
	id, err := paramUUID(c, "id")
	if err != nil {
		return err
	}
	year := time.Now().Year()
	if raw := c.Query("year"); raw != "" {
		if year, err = strconv.Atoi(raw); err != nil || year < 2000 || year > 2100 {
			return errValidation(validate.Errors{"year": "is invalid"})
		}
	}
	rows, err := s.hr.Balances(c.Context(), currentUser(c), id, year)
	if err != nil {
		return err
	}
	return c.JSON(fiber.Map{"balances": rows, "year": year})
}

func (s *Server) hrAttendance(c fiber.Ctx) error {
	v := validate.Errors{}
	f := hr.SheetFilter{UserID: queryUUID(c, v, "user_id"), DepartmentID: queryUUID(c, v, "department_id")}
	if m := c.Query("month"); m != "" {
		from, to, err := hr.MonthRange(m)
		if err != nil {
			return err
		}
		f.From, f.To = from, to
	} else {
		from, to := queryDate(c, v, "from"), queryDate(c, v, "to")
		if from == nil || to == nil {
			v.Add("month", "or from and to are required")
		} else {
			f.From, f.To = *from, *to
		}
	}
	if !v.OK() {
		return errValidation(v)
	}
	sheet, err := s.hr.Attendance(c.Context(), currentUser(c), f)
	if err != nil {
		return err
	}
	return c.JSON(sheet)
}

func (s *Server) hrExportAttendance(c fiber.Ctx) error {
	month := c.Query("month")
	var buf bytes.Buffer
	if err := s.hr.WriteAttendanceCSV(c.Context(), &buf, currentUser(c), month); err != nil {
		return err
	}
	return sendCSV(c, "attendance_"+month+".csv", &buf)
}

func (s *Server) hrListLeave(c fiber.Ctx) error {
	v := validate.Errors{}
	f := hr.LeaveFilter{UserID: queryUUID(c, v, "user_id"), From: queryDate(c, v, "from"), To: queryDate(c, v, "to")}
	if raw := c.Query("status"); raw != "" {
		for _, st := range strings.Split(raw, ",") {
			v.OneOf("status", st, leaveStatuses...)
			f.Statuses = append(f.Statuses, st)
		}
	}
	if !v.OK() {
		return errValidation(v)
	}
	rows, err := s.hr.ListLeave(c.Context(), currentUser(c), f)
	if err != nil {
		return err
	}
	return c.JSON(fiber.Map{"leave": rows})
}

func (s *Server) hrWhosOut(c fiber.Ctx) error {
	rows, err := s.hr.WhosOut(c.Context(), c.Query("from"), c.Query("to"))
	if err != nil {
		return err
	}
	return c.JSON(fiber.Map{"out": rows})
}

func (s *Server) hrApprovals(c fiber.Ctx) error {
	out, err := s.hr.Approvals(c.Context(), currentUser(c))
	if err != nil {
		return err
	}
	return c.JSON(out)
}

// hrReview handles approve/reject for leave and corrections.
func (s *Server) hrReview(kind string, approve bool) fiber.Handler {
	return func(c fiber.Ctx) error {
		id, err := paramUUID(c, "id")
		if err != nil {
			return err
		}
		note, err := optionalNote(c)
		if err != nil {
			return err
		}
		if kind == "leave" {
			out, err := s.hr.ReviewLeave(c.Context(), currentUser(c), id, approve, note)
			if err != nil {
				return err
			}
			return c.JSON(fiber.Map{"leave": out})
		}
		out, err := s.hr.ReviewCorrection(c.Context(), currentUser(c), id, approve, note)
		if err != nil {
			return err
		}
		return c.JSON(fiber.Map{"correction": out})
	}
}

func (s *Server) hrLeaveTypes(c fiber.Ctx) error {
	rows, err := s.store.ListLeaveTypes(c.Context(), c.Query("all") != "true")
	if err != nil {
		return err
	}
	return c.JSON(fiber.Map{"leave_types": rows})
}

func (s *Server) hrDepartments(c fiber.Ctx) error {
	rows, err := s.store.ListDepartments(c.Context(), c.Query("all") != "true")
	if err != nil {
		return err
	}
	return c.JSON(fiber.Map{"departments": rows})
}

func (s *Server) hrHolidays(c fiber.Ctx) error {
	year := time.Now().Year()
	if raw := c.Query("year"); raw != "" {
		y, err := strconv.Atoi(raw)
		if err != nil || y < 2000 || y > 2100 {
			return errValidation(validate.Errors{"year": "is invalid"})
		}
		year = y
	}
	rows, err := s.store.ListHolidays(c.Context(), db.ListHolidaysParams{
		FromDate: strconv.Itoa(year) + "-01-01", ToDate: strconv.Itoa(year) + "-12-31",
	})
	if err != nil {
		return err
	}
	return c.JSON(fiber.Map{"holidays": rows, "year": year})
}

// ── Admin: HR configuration ────────────────────────────────

type departmentReq struct {
	Name     *string    `json:"name"`
	IsActive *bool      `json:"is_active"`
	HeadID   nullableID `json:"head_id"` // the department head runs every team in it; null clears
}

func (s *Server) adminCreateDepartment(c fiber.Ctx) error {
	var req departmentReq
	if err := decodeJSON(c, &req); err != nil {
		return err
	}
	if req.Name == nil {
		return errValidation(validate.Errors{"name": "is required"})
	}
	name := strings.TrimSpace(*req.Name)
	v := validate.Errors{}
	v.Len("name", name, 1, 120)
	if !v.OK() {
		return errValidation(v)
	}
	d, err := s.store.CreateDepartment(c.Context(), name)
	if store.IsUniqueViolation(err) {
		return errValidation(validate.Errors{"name": "already exists"})
	} else if err != nil {
		return err
	}
	s.logAdmin(c, "department.created", "department", d.ID, d)
	return c.Status(fiber.StatusCreated).JSON(fiber.Map{"department": d})
}

func (s *Server) adminUpdateDepartment(c fiber.Ctx) error {
	id, err := paramUUID(c, "id")
	if err != nil {
		return err
	}
	var req departmentReq
	if err := decodeJSON(c, &req); err != nil {
		return err
	}
	v := validate.Errors{}
	if req.Name != nil {
		*req.Name = strings.TrimSpace(*req.Name)
		v.Len("name", *req.Name, 1, 120)
	}
	if err := s.checkLead(c.Context(), v, "head_id", req.HeadID.ID); err != nil {
		return err
	}
	if !v.OK() {
		return errValidation(v)
	}
	d, err := s.store.UpdateDepartment(c.Context(), db.UpdateDepartmentParams{
		ID: id, Name: req.Name, IsActive: req.IsActive, SetHead: req.HeadID.Set, HeadID: req.HeadID.ID,
	})
	switch {
	case store.IsNotFound(err):
		return errNotFound("Department")
	case store.IsUniqueViolation(err):
		return errValidation(validate.Errors{"name": "already exists"})
	case err != nil:
		return err
	}
	s.logAdmin(c, "department.updated", "department", d.ID, req)
	return c.JSON(fiber.Map{"department": d})
}

type leaveTypeReq struct {
	Name        *string  `json:"name"`
	AnnualQuota *float64 `json:"annual_quota"`
	IsLimited   *bool    `json:"is_limited"`
	IsPaid      *bool    `json:"is_paid"`
	IsActive    *bool    `json:"is_active"`
}

func (r *leaveTypeReq) validate(create bool) validate.Errors {
	v := validate.Errors{}
	if r.Name != nil {
		*r.Name = strings.TrimSpace(*r.Name)
		v.Len("name", *r.Name, 1, 60)
	} else if create {
		v.Add("name", "is required")
	}
	if q := r.AnnualQuota; q != nil && (*q < 0 || *q > 365 || *q*2 != float64(int(*q*2))) {
		v.Add("annual_quota", "must be between 0 and 365, in half days")
	}
	return v
}

func quotaStr(q *float64) *string {
	if q == nil {
		return nil
	}
	s := strconv.FormatFloat(*q, 'f', -1, 64)
	return &s
}

func (s *Server) adminCreateLeaveType(c fiber.Ctx) error {
	var req leaveTypeReq
	if err := decodeJSON(c, &req); err != nil {
		return err
	}
	if v := req.validate(true); !v.OK() {
		return errValidation(v)
	}
	quota, limited, paid := "0", true, true
	if req.AnnualQuota != nil {
		quota = *quotaStr(req.AnnualQuota)
	}
	if req.IsLimited != nil {
		limited = *req.IsLimited
	}
	if req.IsPaid != nil {
		paid = *req.IsPaid
	}
	id, err := s.store.CreateLeaveType(c.Context(), db.CreateLeaveTypeParams{Name: *req.Name, AnnualQuota: quota, IsLimited: limited, IsPaid: paid})
	if store.IsUniqueViolation(err) {
		return errValidation(validate.Errors{"name": "already exists"})
	} else if err != nil {
		return err
	}
	t, err := s.store.GetLeaveType(c.Context(), id)
	if err != nil {
		return err
	}
	s.logAdmin(c, "leave_type.created", "leave_type", id, t)
	return c.Status(fiber.StatusCreated).JSON(fiber.Map{"leave_type": t})
}

func (s *Server) adminUpdateLeaveType(c fiber.Ctx) error {
	id, err := paramUUID(c, "id")
	if err != nil {
		return err
	}
	var req leaveTypeReq
	if err := decodeJSON(c, &req); err != nil {
		return err
	}
	if v := req.validate(false); !v.OK() {
		return errValidation(v)
	}
	_, err = s.store.UpdateLeaveType(c.Context(), db.UpdateLeaveTypeParams{
		ID: id, Name: req.Name, AnnualQuota: quotaStr(req.AnnualQuota), IsLimited: req.IsLimited, IsPaid: req.IsPaid, IsActive: req.IsActive,
	})
	switch {
	case store.IsNotFound(err):
		return errNotFound("Leave type")
	case store.IsUniqueViolation(err):
		return errValidation(validate.Errors{"name": "already exists"})
	case err != nil:
		return err
	}
	t, err := s.store.GetLeaveType(c.Context(), id)
	if err != nil {
		return err
	}
	s.logAdmin(c, "leave_type.updated", "leave_type", id, req)
	return c.JSON(fiber.Map{"leave_type": t})
}

type holidayReq struct {
	Date string `json:"date"`
	Name string `json:"name"`
}

func (s *Server) adminCreateHoliday(c fiber.Ctx) error {
	var req holidayReq
	if err := decodeJSON(c, &req); err != nil {
		return err
	}
	req.Name = strings.TrimSpace(req.Name)
	v := validate.Errors{}
	if _, err := hr.ParseDate(req.Date); err != nil {
		v.Add("date", "must be a date (YYYY-MM-DD)")
	}
	v.Len("name", req.Name, 1, 120)
	if !v.OK() {
		return errValidation(v)
	}
	actor := currentUser(c)
	h, err := s.store.CreateHoliday(c.Context(), db.CreateHolidayParams{Date: req.Date, Name: req.Name, CreatedBy: &actor.ID})
	if store.IsUniqueViolation(err) {
		return errValidation(validate.Errors{"date": "already has a holiday"})
	} else if err != nil {
		return err
	}
	s.logAdmin(c, "holiday.created", "holiday", h.ID, h)
	return c.Status(fiber.StatusCreated).JSON(fiber.Map{"holiday": h})
}

func (s *Server) adminDeleteHoliday(c fiber.Ctx) error {
	id, err := paramUUID(c, "id")
	if err != nil {
		return err
	}
	h, err := s.store.DeleteHoliday(c.Context(), id)
	if store.IsNotFound(err) {
		return errNotFound("Holiday")
	} else if err != nil {
		return err
	}
	s.logAdmin(c, "holiday.deleted", "holiday", id, h)
	return c.SendStatus(fiber.StatusNoContent)
}

func (s *Server) adminSetAllocation(c fiber.Ctx) error {
	var req hr.AllocationInput
	if err := decodeJSON(c, &req); err != nil {
		return err
	}
	if err := s.hr.SetAllocation(c.Context(), currentUser(c), req); err != nil {
		return err
	}
	return c.SendStatus(fiber.StatusNoContent)
}

// ── Shifts (HR + CEO) ──────────────────────────────────────

func (s *Server) hrShifts(c fiber.Ctx) error {
	rows, err := s.store.ListShifts(c.Context(), c.Query("all") != "true")
	if err != nil {
		return err
	}
	return c.JSON(fiber.Map{"shifts": rows})
}

type shiftReq struct {
	Name         *string `json:"name"`
	StartTime    *string `json:"start_time"`
	EndTime      *string `json:"end_time"`
	GraceMinutes *int32  `json:"grace_minutes"`
	OffDays      []int32 `json:"off_days"`
	IsActive     *bool   `json:"is_active"`
}

func (r *shiftReq) validate(create bool) validate.Errors {
	v := validate.Errors{}
	if r.Name != nil {
		*r.Name = strings.TrimSpace(*r.Name)
		v.Len("name", *r.Name, 1, 60)
	} else if create {
		v.Add("name", "is required")
	}
	for field, p := range map[string]*string{"start_time": r.StartTime, "end_time": r.EndTime} {
		if p == nil {
			if create {
				v.Add(field, "is required")
			}
			continue
		}
		if _, err := hr.ParseClock(*p); err != nil {
			v.Add(field, "must be a time (HH:MM)")
		}
	}
	if r.StartTime != nil && r.EndTime != nil && *r.StartTime == *r.EndTime {
		v.Add("end_time", "must differ from the start time")
	}
	if r.GraceMinutes != nil && (*r.GraceMinutes < 0 || *r.GraceMinutes > 240) {
		v.Add("grace_minutes", "must be between 0 and 240")
	}
	if r.OffDays != nil {
		seen := map[int32]bool{}
		for _, d := range r.OffDays {
			if d < 0 || d > 6 || seen[d] {
				v.Add("off_days", "must be distinct days from 0 (Sunday) to 6 (Saturday)")
			}
			seen[d] = true
		}
		if len(r.OffDays) > 6 {
			v.Add("off_days", "must leave at least one working day")
		}
	}
	return v
}

func (s *Server) shiftByID(c fiber.Ctx, id uuid.UUID) (db.ListShiftsRow, error) {
	rows, err := s.store.ListShifts(c.Context(), false)
	if err != nil {
		return db.ListShiftsRow{}, err
	}
	for _, r := range rows {
		if r.ID == id {
			return r, nil
		}
	}
	return db.ListShiftsRow{}, errNotFound("Shift")
}

func (s *Server) adminCreateShift(c fiber.Ctx) error {
	var req shiftReq
	if err := decodeJSON(c, &req); err != nil {
		return err
	}
	if v := req.validate(true); !v.OK() {
		return errValidation(v)
	}
	grace, off := int32(15), req.OffDays
	if req.GraceMinutes != nil {
		grace = *req.GraceMinutes
	}
	if off == nil {
		off = []int32{5, 6}
	}
	id, err := s.store.CreateShift(c.Context(), db.CreateShiftParams{
		Name: *req.Name, StartTime: *req.StartTime, EndTime: *req.EndTime, GraceMinutes: grace, OffDays: off,
	})
	if store.IsUniqueViolation(err) {
		return errValidation(validate.Errors{"name": "already exists"})
	} else if err != nil {
		return err
	}
	sh, err := s.shiftByID(c, id)
	if err != nil {
		return err
	}
	s.logAdmin(c, "shift.created", "shift", id, sh)
	return c.Status(fiber.StatusCreated).JSON(fiber.Map{"shift": sh})
}

func (s *Server) adminUpdateShift(c fiber.Ctx) error {
	id, err := paramUUID(c, "id")
	if err != nil {
		return err
	}
	var req shiftReq
	if err := decodeJSON(c, &req); err != nil {
		return err
	}
	if v := req.validate(false); !v.OK() {
		return errValidation(v)
	}
	before, err := s.shiftByID(c, id)
	if err != nil {
		return err
	}
	if req.IsActive != nil && !*req.IsActive && before.MemberCount > 0 {
		return errConflict("Move the people on this shift to another shift before retiring it")
	}
	start, end := before.StartTime, before.EndTime
	if req.StartTime != nil {
		start = *req.StartTime
	}
	if req.EndTime != nil {
		end = *req.EndTime
	}
	if start == end {
		return errValidation(validate.Errors{"end_time": "must differ from the start time"})
	}
	_, err = s.store.UpdateShift(c.Context(), db.UpdateShiftParams{
		ID: id, Name: req.Name, StartTime: req.StartTime, EndTime: req.EndTime,
		GraceMinutes: req.GraceMinutes, OffDays: req.OffDays, IsActive: req.IsActive,
	})
	if store.IsUniqueViolation(err) {
		return errValidation(validate.Errors{"name": "already exists"})
	} else if err != nil {
		return err
	}
	sh, err := s.shiftByID(c, id)
	if err != nil {
		return err
	}
	s.logAdmin(c, "shift.updated", "shift", id, req)
	return c.JSON(fiber.Map{"shift": sh})
}

type assignShiftReq struct {
	ShiftID *uuid.UUID  `json:"shift_id"` // null = back to the default office hours
	UserIDs []uuid.UUID `json:"user_ids"`
}

func (s *Server) adminAssignShift(c fiber.Ctx) error {
	var req assignShiftReq
	if err := decodeJSON(c, &req); err != nil {
		return err
	}
	if len(req.UserIDs) == 0 || len(req.UserIDs) > 500 {
		return errValidation(validate.Errors{"user_ids": "must list 1 to 500 people"})
	}
	if req.ShiftID != nil {
		sh, err := s.shiftByID(c, *req.ShiftID)
		if err != nil {
			return err
		}
		if !sh.IsActive {
			return errValidation(validate.Errors{"shift_id": "is retired"})
		}
	}
	actor := currentUser(c)
	var n int64
	err := s.store.Tx(c.Context(), func(q *db.Queries) error {
		var err error
		if n, err = q.AssignShift(c.Context(), db.AssignShiftParams{ShiftID: req.ShiftID, UserIds: req.UserIDs, UpdatedBy: &actor.ID}); err != nil {
			return err
		}
		return store.LogActivity(c.Context(), q, store.Activity{
			Actor: &actor.ID, Action: "shift.assigned", EntityType: "shift", EntityID: req.ShiftID,
			Metadata: map[string]any{"user_ids": req.UserIDs},
		})
	})
	if err != nil {
		return err
	}
	return c.JSON(fiber.Map{"assigned": n})
}

// ── Salaries (HR + CEO, never your own) ───────────────────

func (s *Server) hrSalaries(c fiber.Ctx) error {
	rows, err := s.hr.CurrentSalaries(c.Context(), currentUser(c))
	if err != nil {
		return err
	}
	return c.JSON(fiber.Map{"salaries": rows})
}

func (s *Server) hrSalaryHistory(c fiber.Ctx) error {
	id, err := paramUUID(c, "id")
	if err != nil {
		return err
	}
	rows, err := s.hr.SalaryHistory(c.Context(), currentUser(c), id)
	if err != nil {
		return err
	}
	return c.JSON(fiber.Map{"history": rows})
}

func (s *Server) hrAddSalary(c fiber.Ctx) error {
	id, err := paramUUID(c, "id")
	if err != nil {
		return err
	}
	var req hr.SalaryInput
	if err := decodeJSON(c, &req); err != nil {
		return err
	}
	rows, err := s.hr.AddSalary(c.Context(), currentUser(c), id, req)
	if err != nil {
		return err
	}
	return c.Status(fiber.StatusCreated).JSON(fiber.Map{"history": rows})
}

func (s *Server) hrDeleteSalary(c fiber.Ctx) error {
	id, err := paramUUID(c, "id")
	if err != nil {
		return err
	}
	if err := s.hr.DeleteSalary(c.Context(), currentUser(c), id); err != nil {
		return err
	}
	return c.SendStatus(fiber.StatusNoContent)
}
