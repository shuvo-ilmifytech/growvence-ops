package hr

import (
	"context"
	"encoding/csv"
	"fmt"
	"io"
	"strconv"
	"time"

	"github.com/solvinx/opsflow/internal/apperr"
	"github.com/solvinx/opsflow/internal/db"
	"github.com/solvinx/opsflow/internal/reports"
)

// MonthRange turns "2026-10" into its first and last day.
func MonthRange(month string) (string, string, error) {
	t, err := time.Parse("2006-01", month)
	if err != nil {
		return "", "", apperr.BadRequest("month must be YYYY-MM")
	}
	return t.Format(time.DateOnly), t.AddDate(0, 1, -1).Format(time.DateOnly), nil
}

// WriteAttendanceCSV writes one summary row per person for a month (admins and Management).
func (s *Service) WriteAttendanceCSV(ctx context.Context, w io.Writer, actor db.User, month string) error {
	if !seesAll(actor) {
		return apperr.Forbidden()
	}
	from, to, err := MonthRange(month)
	if err != nil {
		return err
	}
	sheet, err := s.Attendance(ctx, actor, SheetFilter{From: from, To: to})
	if err != nil {
		return err
	}
	cw := csv.NewWriter(w)
	_ = cw.Write([]string{"Name", "Designation", "Department", "Working days", "Present (on time)", "Late",
		"Absent", "Leave", "Half-day leave", "Holidays", "Weekly off", "Not yet", "Hours worked"})
	for _, p := range sheet.People {
		sm := p.Summary
		working := sm[DayPresent] + sm[DayLate] + sm[DayAbsent] + sm[DayLeave] + sm[DayHalfDay] + sm[DayUpcoming]
		dept := ""
		if p.DepartmentName != nil {
			dept = *p.DepartmentName
		}
		row := []string{reports.Cell(p.Name), reports.Cell(p.Designation), reports.Cell(dept)}
		for _, n := range []int{working, sm[DayPresent], sm[DayLate], sm[DayAbsent], sm[DayLeave], sm[DayHalfDay], sm[DayHoliday], sm[DayOff], sm[DayUpcoming]} {
			row = append(row, strconv.Itoa(n))
		}
		row = append(row, fmt.Sprintf("%.1f", float64(p.WorkedMinutes)/60))
		_ = cw.Write(row)
	}
	cw.Flush()
	return cw.Error()
}
