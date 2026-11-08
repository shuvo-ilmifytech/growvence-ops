package hr

import (
	"testing"
	"time"
)

func day(s string) time.Time {
	d, err := ParseDate(s)
	if err != nil {
		panic(err)
	}
	return d
}

func TestLeaveDays(t *testing.T) {
	s := DefaultSchedule() // Friday + Saturday off
	hol := map[string]bool{"2026-10-20": true}
	cases := []struct {
		name       string
		start, end string
		half       bool
		want       float64
	}{
		{"single working day", "2026-10-18", "2026-10-18", false, 1}, // Sunday
		{"half day", "2026-10-18", "2026-10-18", true, 0.5},
		{"week skips Fri/Sat", "2026-10-18", "2026-10-24", false, 4}, // Sun–Sat minus Tue holiday, Fri, Sat
		{"only weekend", "2026-10-23", "2026-10-24", false, 0},
		{"half day on a holiday", "2026-10-20", "2026-10-20", true, 0},
		{"two weeks", "2026-10-25", "2026-11-07", false, 10},
	}
	for _, c := range cases {
		if got := s.LeaveDays(day(c.start), day(c.end), c.half, hol); got != c.want {
			t.Errorf("%s: got %v want %v", c.name, got, c.want)
		}
	}
}

func TestStartFor(t *testing.T) {
	s := DefaultSchedule()
	s.TrackFrom = "2026-10-18"
	early, late := "2025-01-01", "2026-11-01"
	if s.StartFor(nil) != "2026-10-18" || s.StartFor(&early) != "2026-10-18" || s.StartFor(&late) != late {
		t.Fatal("StartFor must pick the later of tracking start and join date")
	}
}

func TestLateAndEarly(t *testing.T) {
	s := DefaultSchedule()
	s.Loc = time.FixedZone("BDT", 6*3600)
	at := func(clock string) time.Time {
		v, err := s.At("2026-10-18", clock)
		if err != nil {
			t.Fatal(err)
		}
		return v
	}
	if s.IsLate(at("09:15")) {
		t.Error("09:15 is within the grace period")
	}
	if !s.IsLate(at("09:16")) {
		t.Error("09:16 is late")
	}
	// Same instant expressed in UTC must still be judged in local time.
	if !s.IsLate(at("09:16").UTC()) {
		t.Error("lateness must use the schedule's timezone")
	}
	if !s.LeftEarly("2026-10-18", at("17:59")) || s.LeftEarly("2026-10-18", at("18:00")) {
		t.Error("early-leave boundary is 18:00")
	}
	if s.IsLate(at("08:50")) {
		t.Error("arriving early is not late")
	}
}

func TestNightShift(t *testing.T) {
	s := DefaultSchedule()
	s.Loc = time.FixedZone("BDT", 6*3600)
	s.WorkStart, s.WorkEnd = 22*60, 6*60 // 22:00–06:00
	at := func(day, clock string) time.Time {
		v, err := s.At(day, clock)
		if err != nil {
			t.Fatal(err)
		}
		return v
	}
	if !s.Overnight() || s.Length() != 8*60 {
		t.Fatalf("overnight=%v length=%d", s.Overnight(), s.Length())
	}
	cases := []struct {
		day, clock, workDate string
		late                 bool
	}{
		{"2026-10-18", "21:50", "2026-10-18", false}, // a little early
		{"2026-10-18", "22:10", "2026-10-18", false}, // within grace
		{"2026-10-18", "22:30", "2026-10-18", true},
		{"2026-10-19", "00:30", "2026-10-18", true}, // after midnight: still the 18th's shift
		{"2026-10-19", "13:59", "2026-10-18", false},
		{"2026-10-19", "14:00", "2026-10-19", false}, // second half of the gap: next shift, early
	}
	for _, c := range cases {
		in := at(c.day, c.clock)
		if got := s.WorkDate(in); got != c.workDate {
			t.Errorf("%s %s: work date %s want %s", c.day, c.clock, got, c.workDate)
		}
		if got := s.IsLate(in); got != c.late {
			t.Errorf("%s %s: late=%v want %v", c.day, c.clock, got, c.late)
		}
	}
	if !s.LeftEarly("2026-10-18", at("2026-10-19", "05:30")) || s.LeftEarly("2026-10-18", at("2026-10-19", "06:00")) {
		t.Error("night shift ends 06:00 the next day")
	}
}

func TestDayStatus(t *testing.T) {
	s := DefaultSchedule()
	today := "2026-10-21"
	in := func(clock string) *time.Time {
		v, _ := s.At("2026-10-19", clock)
		return &v
	}
	cases := []struct {
		name string
		in   DayInput
		want string
	}{
		{"on time", DayInput{Date: day("2026-10-19"), CheckIn: in("09:00")}, DayPresent},
		{"late", DayInput{Date: day("2026-10-19"), CheckIn: in("10:00")}, DayLate},
		{"late but half-day leave", DayInput{Date: day("2026-10-19"), CheckIn: in("14:00"), OnLeave: true, HalfDay: true}, DayPresent},
		{"leave", DayInput{Date: day("2026-10-19"), OnLeave: true}, DayLeave},
		{"half-day leave, no check-in", DayInput{Date: day("2026-10-19"), OnLeave: true, HalfDay: true}, DayHalfDay},
		{"holiday", DayInput{Date: day("2026-10-19"), IsHoliday: true}, DayHoliday},
		{"weekend", DayInput{Date: day("2026-10-23")}, DayOff},
		{"absent", DayInput{Date: day("2026-10-19")}, DayAbsent},
		{"today, not in yet", DayInput{Date: day(today)}, DayUpcoming},
		{"future leave", DayInput{Date: day("2026-10-25"), OnLeave: true}, DayLeave},
		{"before tracking started", DayInput{Date: day("2026-10-19"), StartFrom: "2026-10-20"}, DayUntracked},
		{"before start but checked in", DayInput{Date: day("2026-10-19"), StartFrom: "2026-10-20", CheckIn: in("09:00")}, DayPresent},
		{"worked on a holiday", DayInput{Date: day("2026-10-19"), IsHoliday: true, CheckIn: in("09:00")}, DayPresent},
	}
	for _, c := range cases {
		if got := s.DayStatus(c.in, today); got != c.want {
			t.Errorf("%s: got %s want %s", c.name, got, c.want)
		}
	}
}
