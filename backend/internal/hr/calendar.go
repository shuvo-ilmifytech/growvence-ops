// Package hr holds the HRM rules: the work calendar, attendance, leave and
// who may see or approve what. Calendar logic here is pure and unit-tested.
package hr

import (
	"fmt"
	"time"
)

// Schedule is one working pattern: the organisation's default office hours (settings) or a
// shift. A shift whose end is at or before its start runs overnight (e.g. 22:00–06:00); its
// work date is the day it starts.
type Schedule struct {
	Loc          *time.Location
	ShiftID      *string // nil = the default office hours
	ShiftName    string
	WorkStart    int // minutes after midnight, e.g. 9:00 = 540
	WorkEnd      int
	GraceMinutes int
	OffDays      map[time.Weekday]bool
	TrackFrom    string // first day attendance is recorded ("" = always)
}

// DefaultSchedule matches the migration defaults: 09:00–18:00, 15 min grace, Friday + Saturday off.
func DefaultSchedule() Schedule {
	return Schedule{
		Loc: time.UTC, WorkStart: 9 * 60, WorkEnd: 18 * 60, GraceMinutes: 15,
		OffDays: map[time.Weekday]bool{time.Friday: true, time.Saturday: true},
	}
}

// ParseClock turns "09:30" into minutes after midnight.
func ParseClock(s string) (int, error) {
	t, err := time.Parse("15:04", s)
	if err != nil {
		return 0, fmt.Errorf("invalid time %q", s)
	}
	return t.Hour()*60 + t.Minute(), nil
}

// ParseDate parses a calendar day "2026-10-18".
func ParseDate(s string) (time.Time, error) {
	return time.Parse(time.DateOnly, s)
}

// Today is the current calendar day in the schedule's timezone.
func (s Schedule) Today(now time.Time) string {
	return now.In(s.Loc).Format(time.DateOnly)
}

// IsWorkingDay reports whether the day is neither a weekly off day nor a holiday.
func (s Schedule) IsWorkingDay(day time.Time, holidays map[string]bool) bool {
	return !s.OffDays[day.Weekday()] && !holidays[day.Format(time.DateOnly)]
}

// LeaveDays counts the working days in [start, end]; a half day counts 0.5.
// Weekly off days and holidays inside the range are not charged.
func (s Schedule) LeaveDays(start, end time.Time, halfDay bool, holidays map[string]bool) float64 {
	n := 0.0
	for d := start; !d.After(end); d = d.AddDate(0, 0, 1) {
		if s.IsWorkingDay(d, holidays) {
			n++
		}
	}
	if halfDay && n > 0 {
		return 0.5
	}
	return n
}

const minutesPerDay = 24 * 60

func clockOf(t time.Time, loc *time.Location) int {
	t = t.In(loc)
	return t.Hour()*60 + t.Minute()
}

// Overnight reports whether the shift ends the day after it starts.
func (s Schedule) Overnight() bool { return s.WorkEnd <= s.WorkStart }

// Length is the shift's length in minutes.
func (s Schedule) Length() int {
	return ((s.WorkEnd-s.WorkStart)%minutesPerDay + minutesPerDay) % minutesPerDay
}

// IsLate reports whether a check-in is after the start plus the grace period. Minutes are
// counted from the start around the clock, so 00:30 is 150 minutes late for a 22:00 shift
// and 21:50 is early, not 23 hours late (anything over 12 hours counts as early).
func (s Schedule) IsLate(checkIn time.Time) bool {
	diff := ((clockOf(checkIn, s.Loc)-s.WorkStart)%minutesPerDay + minutesPerDay) % minutesPerDay
	return diff > s.GraceMinutes && diff < minutesPerDay/2
}

// LeftEarly reports whether a check-out is before the scheduled end of that work date's shift.
func (s Schedule) LeftEarly(workDate string, checkOut time.Time) bool {
	start, err := s.At(workDate, fmt.Sprintf("%02d:%02d", s.WorkStart/60, s.WorkStart%60))
	if err != nil {
		return false
	}
	return checkOut.Before(start.Add(time.Duration(s.Length()) * time.Minute))
}

// WorkDate is the day a check-in at `now` belongs to. For an overnight shift, check-ins in
// the first half of the gap between shifts still belong to the shift that started yesterday
// (22:00–06:00: anything before 14:00 counts for the previous day).
func (s Schedule) WorkDate(now time.Time) string {
	t := now.In(s.Loc)
	if s.Overnight() {
		gap := ((s.WorkStart-s.WorkEnd)%minutesPerDay + minutesPerDay) % minutesPerDay
		if clockOf(now, s.Loc) < s.WorkEnd+gap/2 {
			return t.AddDate(0, 0, -1).Format(time.DateOnly)
		}
	}
	return t.Format(time.DateOnly)
}

// At returns the instant for a local wall-clock time ("HH:MM") on a calendar day.
func (s Schedule) At(day, clock string) (time.Time, error) {
	t, err := time.ParseInLocation("2006-01-02 15:04", day+" "+clock, s.Loc)
	if err != nil {
		return time.Time{}, fmt.Errorf("invalid date/time %s %s", day, clock)
	}
	return t, nil
}

// Day statuses shown on attendance calendars and reports.
const (
	DayPresent   = "present"
	DayLate      = "late"
	DayAbsent    = "absent"
	DayLeave     = "leave"
	DayHalfDay   = "half_day_leave"
	DayHoliday   = "holiday"
	DayOff       = "weekend"
	DayUpcoming  = "upcoming"
	DayUntracked = "untracked" // before tracking started or before the person joined
)

// DayInput is what is known about one person on one day.
type DayInput struct {
	Date      time.Time
	CheckIn   *time.Time
	OnLeave   bool
	HalfDay   bool
	IsHoliday bool
	StartFrom string // the person's first tracked day ("" = no limit)
}

// StartFor is the later of the tracking start and the person's join date.
func (s Schedule) StartFor(joinDate *string) string {
	start := s.TrackFrom
	if joinDate != nil && *joinDate > start {
		start = *joinDate
	}
	return start
}

// DayStatus classifies a day. Attendance wins over the calendar: someone who
// checks in on a holiday is present. Days after today are upcoming unless on leave.
func (s Schedule) DayStatus(in DayInput, today string) string {
	date := in.Date.Format(time.DateOnly)
	switch {
	case in.CheckIn != nil && s.IsLate(*in.CheckIn) && !in.HalfDay:
		return DayLate
	case in.CheckIn != nil:
		return DayPresent
	case in.OnLeave && in.HalfDay:
		return DayHalfDay
	case in.OnLeave:
		return DayLeave
	case in.StartFrom != "" && date < in.StartFrom:
		return DayUntracked
	case in.IsHoliday:
		return DayHoliday
	case s.OffDays[in.Date.Weekday()]:
		return DayOff
	case date >= today:
		// Today without a check-in isn't "absent" until the day is over.
		return DayUpcoming
	}
	return DayAbsent
}
