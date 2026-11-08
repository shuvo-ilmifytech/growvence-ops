package httpapi

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/solvinx/opsflow/internal/notify"
	"github.com/solvinx/opsflow/internal/policy"
)

// nextWorkingPair returns a Sunday and Monday at least a week ahead (Fri/Sat are off by default).
func nextWorkingPair() (string, string) {
	loc, _ := time.LoadLocation("Asia/Dhaka")
	d := time.Now().In(loc).AddDate(0, 0, 7)
	for d.Weekday() != time.Sunday {
		d = d.AddDate(0, 0, 1)
	}
	return d.Format(time.DateOnly), d.AddDate(0, 0, 1).Format(time.DateOnly)
}

func fields(r resp) map[string]any {
	if e, ok := r.body["error"].(map[string]any); ok {
		if f, ok := e["fields"].(map[string]any); ok {
			return f
		}
	}
	return map[string]any{}
}

func employeeBody(over map[string]any) map[string]any {
	b := map[string]any{
		"employee_code": nil, "designation": "", "department_id": nil, "manager_id": nil,
		"employment_type": "full_time", "join_date": nil, "phone": "", "date_of_birth": nil,
		"address": "", "emergency_contact_name": "", "emergency_contact_phone": "", "national_id": "",
	}
	for k, v := range over {
		b[k] = v
	}
	return b
}

func TestHRM(t *testing.T) {
	e := newEnv(t)
	admin, tl, member, sales, mgmt, hr := e.login("admin"), e.login("tl"), e.login("member"), e.login("sales"), e.login("ceo"), e.login("hr")
	memberID, tlID := e.users["member"].ID.String(), e.users["tl"].ID.String()

	t.Run("every user has an employee record and a /hr/me", func(t *testing.T) {
		for _, c := range []string{admin, tl, member, sales, mgmt, hr} {
			r := e.do("GET", "/api/hr/me", c, nil)
			expect(t, r, 200, "hr/me")
			if r.body["schedule"].(map[string]any)["work_start"] != "09:00" {
				t.Fatalf("schedule = %v", r.body["schedule"])
			}
		}
		r := e.do("GET", "/api/hr/directory", sales, nil)
		expect(t, r, 200, "directory")
		if n := len(r.body["employees"].([]any)); n != len(policy.Roles)+1 {
			t.Fatalf("directory has %d people", n)
		}
	})

	t.Run("only HR and the CEO edit HR records; reporting loops are refused", func(t *testing.T) {
		body := employeeBody(map[string]any{"manager_id": tlID, "designation": "Developer", "national_id": "1990123456789", "date_of_birth": "1995-04-02"})
		for _, c := range []string{admin, tl, member, sales} {
			expect(t, e.do("PUT", "/api/hr/employees/"+memberID, c, body), 403, "edit without HR rights")
		}
		expect(t, e.do("PUT", "/api/hr/employees/"+memberID, hr, body), 200, "hr edit")
		loop := e.do("PUT", "/api/hr/employees/"+tlID, hr, employeeBody(map[string]any{"manager_id": memberID}))
		expect(t, loop, 422, "loop")
		if _, ok := fields(loop)["manager_id"]; !ok {
			t.Fatalf("loop error = %v", loop.body)
		}
		self := e.do("PUT", "/api/hr/employees/"+tlID, hr, employeeBody(map[string]any{"manager_id": tlID}))
		expect(t, self, 422, "self manager")
		bad := e.do("PUT", "/api/hr/employees/"+tlID, hr, employeeBody(map[string]any{"employment_type": "slave", "join_date": "soon"}))
		expect(t, bad, 422, "bad fields")
	})

	t.Run("restricted fields: self, HR and the CEO only", func(t *testing.T) {
		for _, c := range []struct {
			who, cookie string
			full        bool
		}{{"self", member, true}, {"admin flag only", admin, false}, {"hr", hr, true}, {"ceo", mgmt, true}, {"manager", tl, false}, {"sales", sales, false}} {
			r := e.do("GET", "/api/hr/employees/"+memberID, c.cookie, nil)
			expect(t, r, 200, c.who)
			emp := r.body["employee"].(map[string]any)
			if got := emp["national_id"] == "1990123456789"; got != c.full {
				t.Errorf("%s sees national_id=%v, want %v", c.who, emp["national_id"], c.full)
			}
		}
		// Sales is not in the member's reporting line: no balances or leave.
		r := e.do("GET", "/api/hr/employees/"+memberID, sales, nil)
		if r.body["can_see_hr"] != false {
			t.Fatal("sales can see member's HR data")
		}
		r = e.do("GET", "/api/hr/employees/"+memberID, tl, nil)
		if r.body["can_see_hr"] != true || len(r.body["balances"].([]any)) != 4 {
			t.Fatalf("manager view = %v", r.body)
		}
	})

	t.Run("anyone can update their own contact details", func(t *testing.T) {
		r := e.do("PUT", "/api/hr/me/profile", mgmt, map[string]any{"phone": " 01700000000 ", "address": "Dhaka", "emergency_contact_name": "A", "emergency_contact_phone": "1"})
		expect(t, r, 200, "management self edit")
		if r.body["employee"].(map[string]any)["phone"] != "01700000000" {
			t.Fatalf("phone not trimmed: %v", r.body)
		}
	})

	t.Run("check in and out once a day, Management included", func(t *testing.T) {
		r := e.do("POST", "/api/hr/me/check-in", member, nil)
		expect(t, r, 200, "check in")
		if r.body["today_record"] == nil {
			t.Fatal("no record after check-in")
		}
		expect(t, e.do("POST", "/api/hr/me/check-in", member, nil), 409, "double check-in")
		expect(t, e.do("POST", "/api/hr/me/check-out", member, map[string]any{"note": "done"}), 200, "check out")
		expect(t, e.do("POST", "/api/hr/me/check-out", member, nil), 409, "double check-out")
		expect(t, e.do("POST", "/api/hr/me/check-out", sales, nil), 409, "check-out without check-in")
		expect(t, e.do("POST", "/api/hr/me/check-in", mgmt, nil), 200, "management check-in")

		// Each check-in and out records the network and browser it came from.
		rows, err := e.store.Pool.Query(context.Background(), `SELECT metadata FROM activity_log
			WHERE actor_id = $1 AND action IN ('attendance.check_in','attendance.check_out')`, e.users["member"].ID)
		if err != nil {
			t.Fatal(err)
		}
		defer rows.Close()
		n := 0
		for rows.Next() {
			var m map[string]any
			if err := rows.Scan(&m); err != nil {
				t.Fatal(err)
			}
			if m["ip"] == nil || m["ip"] == "" || m["user_agent"] == nil {
				t.Fatalf("check metadata = %v", m)
			}
			n++
		}
		if n != 2 {
			t.Fatalf("logged %d check-ins/outs, want 2", n)
		}
	})

	t.Run("office location: HR sets it, check-in and out only nearby", func(t *testing.T) {
		office := map[string]any{"enabled": true, "lat": 23.7808, "lng": 90.4074, "radius_m": 150}
		expect(t, e.do("PUT", "/api/admin/settings/office_location", member, map[string]any{"value": office}), 403, "member sets office")
		expect(t, e.do("PUT", "/api/admin/settings/office_location", hr, map[string]any{"value": map[string]any{"enabled": true, "radius_m": 150}}), 422, "on without a place")
		expect(t, e.do("PUT", "/api/admin/settings/office_location", hr, map[string]any{"value": office}), 200, "hr sets office")

		expect(t, e.do("POST", "/api/hr/me/check-in", tl, nil), 400, "no location")
		far := e.do("POST", "/api/hr/me/check-in", tl, map[string]any{"lat": 23.7383, "lng": 90.3958, "accuracy": 20})
		expect(t, far, 403, "check-in ~5 km away")
		if far.body["error"].(map[string]any)["code"] != "outside_office" {
			t.Fatalf("far = %v", far.body)
		}
		// ~200 m north: outside 150 m, but inside once a 60 m accuracy is allowed for.
		expect(t, e.do("POST", "/api/hr/me/check-in", tl, map[string]any{"lat": 23.7826, "lng": 90.4074, "accuracy": 60}), 200, "check-in near the edge")
		expect(t, e.do("POST", "/api/hr/me/check-out", tl, map[string]any{"lat": 23.7383, "lng": 90.3958, "accuracy": 20}), 403, "check-out far away")
		expect(t, e.do("POST", "/api/hr/me/check-out", tl, map[string]any{"lat": 23.7809, "lng": 90.4075, "accuracy": 15, "note": "bye"}), 200, "check-out at the office")

		expect(t, e.do("PUT", "/api/admin/settings/office_location", hr, map[string]any{"value": map[string]any{"enabled": false, "lat": 23.7808, "lng": 90.4074, "radius_m": 150}}), 200, "switch off")
		expect(t, e.do("POST", "/api/hr/me/check-in", sales, nil), 200, "check-in with the check off")
	})

	t.Run("office network: HR and the CEO set the IPs, check-in only from them", func(t *testing.T) {
		set := func(who string, v map[string]any) resp {
			return e.do("PUT", "/api/admin/settings/office_network", who, map[string]any{"value": v})
		}
		expect(t, set(member, map[string]any{"enabled": false, "ips": []string{"203.0.113.5"}}), 403, "member sets office IPs")
		expect(t, set(hr, map[string]any{"enabled": false, "ips": []string{"not-an-ip"}}), 422, "bad IP")
		expect(t, set(hr, map[string]any{"enabled": true, "ips": []string{}}), 422, "on with no IPs")
		expect(t, set(mgmt, map[string]any{"enabled": false, "ips": []string{"203.0.113.0/24", "2001:db8::1"}}), 200, "ceo sets office IPs")

		myIP, _ := e.do("GET", "/api/hr/me", admin, nil).body["client_ip"].(string)
		if myIP == "" {
			t.Fatal("hr/me has no client_ip")
		}
		expect(t, set(hr, map[string]any{"enabled": true, "ips": []string{"203.0.113.5"}}), 200, "hr switches it on")
		off := e.do("POST", "/api/hr/me/check-in", admin, nil)
		expect(t, off, 403, "check-in off the office network")
		if off.body["error"].(map[string]any)["code"] != "outside_office_network" {
			t.Fatalf("off = %v", off.body)
		}
		expect(t, set(hr, map[string]any{"enabled": true, "ips": []string{"203.0.113.5", myIP}}), 200, "hr adds this network")
		expect(t, e.do("POST", "/api/hr/me/check-in", admin, nil), 200, "check-in on the office network")

		var onNet bool
		if err := e.store.Pool.QueryRow(context.Background(), `SELECT (metadata->>'office_network')::bool FROM activity_log
			WHERE actor_id = $1 AND action = 'attendance.check_in'`, e.users["admin"].ID).Scan(&onNet); err != nil || !onNet {
			t.Fatalf("office_network = %v, %v", onNet, err)
		}
		expect(t, set(hr, map[string]any{"enabled": false, "ips": []string{}}), 200, "switch off")
	})

	sun, mon := nextWorkingPair()
	var leaveID string

	t.Run("leave: apply, notify the manager, approve", func(t *testing.T) {
		types := e.do("GET", "/api/hr/leave-types", member, nil)
		var casual string
		for _, raw := range types.body["leave_types"].([]any) {
			if lt := raw.(map[string]any); lt["name"] == "Casual" {
				casual = lt["id"].(string)
			}
		}
		r := e.do("POST", "/api/hr/me/leave", member, map[string]any{"leave_type_id": casual, "start_date": sun, "end_date": mon, "reason": "Family event"})
		expect(t, r, 201, "apply")
		l := r.body["leave"].(map[string]any)
		leaveID = l["id"].(string)
		if l["days"] != "2.0" || l["status"] != "pending" {
			t.Fatalf("leave = %v", l)
		}
		if n := ofType(e.notes(tl), notify.LeaveRequested); len(n) != 1 || !strings.Contains(n[0].Body, "2 days") {
			t.Fatalf("manager notifications = %v", n)
		}
		if n := ofType(e.notes(hr), notify.LeaveRequested); len(n) != 0 {
			t.Fatal("HR notified although the member has a manager")
		}

		expect(t, e.do("POST", "/api/hr/me/leave", member, map[string]any{"leave_type_id": casual, "start_date": mon, "end_date": mon}), 409, "overlap")
		big := e.do("POST", "/api/hr/me/leave", member, map[string]any{"leave_type_id": casual, "start_date": mon[:4] + "-12-01", "end_date": mon[:4] + "-12-31"})
		if big.status != 422 && big.status != 409 {
			t.Fatalf("over balance: %d %v", big.status, big.body)
		}
		// A Friday–Saturday range has no working days.
		fri := mustDate(sun).AddDate(0, 0, 5).Format(time.DateOnly)
		sat := mustDate(sun).AddDate(0, 0, 6).Format(time.DateOnly)
		expect(t, e.do("POST", "/api/hr/me/leave", member, map[string]any{"leave_type_id": casual, "start_date": fri, "end_date": sat}), 422, "weekend only")

		approve := "/api/hr/leave/" + leaveID + "/approve"
		expect(t, e.do("POST", approve, member, nil), 403, "own approval")
		expect(t, e.do("POST", approve, sales, nil), 403, "outsider approval")
		expect(t, e.do("POST", approve, admin, nil), 403, "admin flag alone cannot approve")
		expect(t, e.do("POST", "/api/hr/leave/"+leaveID+"/reject", tl, nil), 422, "reject needs a note")

		ap := e.do("GET", "/api/hr/approvals", tl, nil)
		if len(ap.body["leave"].([]any)) != 1 {
			t.Fatalf("approvals = %v", ap.body)
		}
		// Step 1: the Team Lead approves; it moves on to HR.
		r = e.do("POST", approve, tl, map[string]any{"note": "Enjoy"})
		expect(t, r, 200, "tl approve")
		if st := r.body["leave"].(map[string]any)["status"]; st != "awaiting_hr" {
			t.Fatalf("after TL approval status = %v", st)
		}
		if n := ofType(e.notes(member), notify.LeaveApproved); len(n) != 1 || !strings.Contains(n[0].Title, "waiting for HR") {
			t.Fatalf("member notifications = %v", n)
		}
		if n := ofType(e.notes(hr), notify.LeaveRequested); len(n) != 1 || !strings.Contains(n[0].Title, "final approval") {
			t.Fatalf("hr notifications = %v", n)
		}
		// Step 2 is HR's: the TL can't approve twice, and it now sits on HR's list.
		expect(t, e.do("POST", approve, tl, nil), 403, "tl approves step 2")
		if ap := e.do("GET", "/api/hr/approvals", tl, nil); len(ap.body["leave"].([]any)) != 0 {
			t.Fatalf("still on TL's list: %v", ap.body)
		}
		if ap := e.do("GET", "/api/hr/approvals", hr, nil); len(ap.body["leave"].([]any)) != 1 {
			t.Fatalf("not on HR's list: %v", ap.body)
		}
		r = e.do("POST", approve, hr, map[string]any{"note": "Approved"})
		expect(t, r, 200, "hr approve")
		if l := r.body["leave"].(map[string]any); l["status"] != "approved" || l["manager_reviewed_by_name"] != "tl" {
			t.Fatalf("after HR approval = %v", l)
		}
		expect(t, e.do("POST", approve, hr, nil), 409, "approve twice")
		if n := ofType(e.notes(member), notify.LeaveApproved); len(n) != 2 {
			t.Fatalf("member not told about the final approval: %v", n)
		}
		if n := ofType(e.notes(tl), notify.LeaveApproved); len(n) != 1 {
			t.Fatalf("TL not told about HR's decision: %v", n)
		}
		bal := e.do("GET", "/api/hr/employees/"+memberID+"/balances?year="+sun[:4], member, nil)
		for _, raw := range bal.body["balances"].([]any) {
			if b := raw.(map[string]any); b["leave_type_name"] == "Casual" && b["approved"] != "2.0" {
				t.Fatalf("casual balance = %v", b)
			}
		}
		out := e.do("GET", "/api/hr/whos-out?from="+sun+"&to="+sun, sales, nil)
		if len(out.body["out"].([]any)) != 1 {
			t.Fatalf("whos-out = %v", out.body)
		}
	})

	t.Run("no manager: leave goes straight to HR", func(t *testing.T) {
		types := e.do("GET", "/api/hr/leave-types", sales, nil)
		var sick string
		for _, raw := range types.body["leave_types"].([]any) {
			if lt := raw.(map[string]any); lt["name"] == "Sick" {
				sick = lt["id"].(string)
			}
		}
		r := e.do("POST", "/api/hr/me/leave", sales, map[string]any{"leave_type_id": sick, "start_date": sun, "end_date": sun})
		expect(t, r, 201, "apply")
		l := r.body["leave"].(map[string]any)
		if l["status"] != "awaiting_hr" {
			t.Fatalf("status = %v", l["status"])
		}
		expect(t, e.do("POST", "/api/hr/leave/"+l["id"].(string)+"/reject", hr, map[string]any{"note": "Busy week"}), 200, "hr reject")
	})

	t.Run("future approved leave can be cancelled by the person", func(t *testing.T) {
		expect(t, e.do("POST", "/api/hr/me/leave/"+leaveID+"/cancel", sales, nil), 404, "someone else's leave")
		r := e.do("POST", "/api/hr/me/leave/"+leaveID+"/cancel", member, nil)
		expect(t, r, 200, "cancel")
		if r.body["leave"].(map[string]any)["status"] != "cancelled" {
			t.Fatalf("cancel = %v", r.body)
		}
		if n := ofType(e.notes(tl), notify.LeaveCancelled); len(n) != 1 {
			t.Fatalf("manager not told about cancellation: %v", n)
		}
	})

	t.Run("attendance corrections go to the manager and update the sheet", func(t *testing.T) {
		loc, _ := time.LoadLocation("Asia/Dhaka")
		yesterday := time.Now().In(loc).AddDate(0, 0, -1).Format(time.DateOnly)
		expect(t, e.do("POST", "/api/hr/me/corrections", member, map[string]any{"work_date": yesterday, "check_in": "09:00", "check_out": "08:00", "reason": "x"}), 422, "out before in")
		r := e.do("POST", "/api/hr/me/corrections", member, map[string]any{"work_date": yesterday, "check_in": "09:05", "check_out": "18:10", "reason": "Forgot to check in"})
		expect(t, r, 201, "request")
		id := r.body["correction"].(map[string]any)["id"].(string)
		expect(t, e.do("POST", "/api/hr/me/corrections", member, map[string]any{"work_date": yesterday, "check_in": "09:05", "reason": "again"}), 409, "duplicate pending")
		expect(t, e.do("POST", "/api/hr/corrections/"+id+"/approve", sales, nil), 403, "outsider")
		expect(t, e.do("POST", "/api/hr/corrections/"+id+"/approve", tl, nil), 200, "approve")

		sheet := e.do("GET", "/api/hr/attendance?from="+yesterday+"&to="+yesterday+"&user_id="+memberID, tl, nil)
		expect(t, sheet, 200, "sheet")
		day := sheet.body["people"].([]any)[0].(map[string]any)["days"].([]any)[0].(map[string]any)
		if day["source"] != "correction" || day["status"] != "present" || day["worked_minutes"].(float64) != 545 {
			t.Fatalf("corrected day = %v", day)
		}
	})

	t.Run("attendance sheets are scoped to the reporting line", func(t *testing.T) {
		month := time.Now().Format("2006-01")
		count := func(cookie string) int {
			r := e.do("GET", "/api/hr/attendance?month="+month, cookie, nil)
			expect(t, r, 200, "sheet")
			return len(r.body["people"].([]any))
		}
		if n := count(tl); n != 2 {
			t.Errorf("manager sees %d people, want 2 (self + report)", n)
		}
		if n := count(sales); n != 1 {
			t.Errorf("sales sees %d people, want 1", n)
		}
		if n := count(mgmt); n != len(policy.Roles)+1 {
			t.Errorf("management sees %d people", n)
		}
		expect(t, e.do("GET", "/api/hr/attendance?month="+month+"&user_id="+memberID, sales, nil), 404, "outside scope")
		expect(t, e.do("GET", "/api/hr/attendance?from=2026-01-01&to=2026-06-01", tl, nil), 400, "range too long")

		code, body := e.get("/api/hr/attendance/export.csv?month="+month, mgmt)
		if code != 200 || !strings.Contains(body, "Hours worked") {
			t.Fatalf("csv %d %s", code, body)
		}
		if code, _ := e.get("/api/hr/attendance/export.csv?month="+month, tl); code != 403 {
			t.Fatalf("tl csv = %d", code)
		}
	})

	t.Run("shifts: HR creates them and assigns people", func(t *testing.T) {
		night := map[string]any{"name": "Night", "start_time": "22:00", "end_time": "06:00", "grace_minutes": 10, "off_days": []int{5}}
		for _, c := range []string{admin, tl, member, sales} {
			expect(t, e.do("POST", "/api/admin/shifts", c, night), 403, "create shift without HR rights")
		}
		bad := e.do("POST", "/api/admin/shifts", hr, map[string]any{"name": "", "start_time": "9am", "end_time": "9am", "off_days": []int{7, 7}})
		expect(t, bad, 422, "invalid shift")
		for _, f := range []string{"name", "start_time", "off_days"} {
			if _, ok := fields(bad)[f]; !ok {
				t.Errorf("missing field error for %s: %v", f, fields(bad))
			}
		}
		r := e.do("POST", "/api/admin/shifts", hr, night)
		expect(t, r, 201, "create night shift")
		shift := r.body["shift"].(map[string]any)
		id := shift["id"].(string)
		if shift["start_time"] != "22:00" || shift["end_time"] != "06:00" {
			t.Fatalf("shift = %v", shift)
		}
		expect(t, e.do("POST", "/api/admin/shifts", hr, night), 422, "duplicate name")
		// The CEO runs shifts too.
		day := e.do("POST", "/api/admin/shifts", mgmt, map[string]any{"name": "Day", "start_time": "09:00", "end_time": "18:00"})
		expect(t, day, 201, "ceo creates shift")
		dayID := day.body["shift"].(map[string]any)["id"].(string)
		expect(t, e.do("POST", "/api/admin/shifts/assign", mgmt, map[string]any{"shift_id": dayID, "user_ids": []string{e.users["tl"].ID.String()}}), 200, "ceo assigns")
		expect(t, e.do("PATCH", "/api/admin/shifts/"+dayID, mgmt, map[string]any{"grace_minutes": 5}), 200, "ceo edits")

		salesID := e.users["sales"].ID.String()
		expect(t, e.do("POST", "/api/admin/shifts/assign", tl, map[string]any{"shift_id": id, "user_ids": []string{salesID}}), 403, "tl assigns")
		r = e.do("POST", "/api/admin/shifts/assign", hr, map[string]any{"shift_id": id, "user_ids": []string{salesID}})
		expect(t, r, 200, "assign")
		me := e.do("GET", "/api/hr/me", sales, nil)
		sch := me.body["schedule"].(map[string]any)
		if sch["shift_name"] != "Night" || sch["work_start"] != "22:00" || sch["overnight"] != true || sch["grace_minutes"].(float64) != 10 {
			t.Fatalf("sales schedule = %v", sch)
		}
		list := e.do("GET", "/api/hr/shifts", member, nil)
		for _, raw := range list.body["shifts"].([]any) {
			if sh := raw.(map[string]any); sh["name"] == "Night" && sh["member_count"].(float64) != 1 {
				t.Fatalf("night member_count = %v", sh["member_count"])
			}
		}
		expect(t, e.do("PATCH", "/api/admin/shifts/"+id, hr, map[string]any{"is_active": false}), 409, "retire with people on it")
		expect(t, e.do("POST", "/api/admin/shifts/assign", hr, map[string]any{"shift_id": nil, "user_ids": []string{salesID}}), 200, "back to default")
		expect(t, e.do("PATCH", "/api/admin/shifts/"+id, hr, map[string]any{"is_active": false}), 200, "retire")
		expect(t, e.do("POST", "/api/admin/shifts/assign", hr, map[string]any{"shift_id": id, "user_ids": []string{salesID}}), 422, "assign to retired")
		if sch := e.do("GET", "/api/hr/me", sales, nil).body["schedule"].(map[string]any); sch["shift_name"] != "" {
			t.Fatalf("still on a shift: %v", sch)
		}
	})

	t.Run("salaries: HR and CEO only, never your own", func(t *testing.T) {
		path := "/api/hr/employees/" + memberID + "/salary"
		body := map[string]any{"amount": "45,000", "effective_from": "2026-01-01", "note": "Joining salary"}
		// Nobody else can add or read a salary, not even the person's manager or an admin.
		for _, c := range []string{admin, tl, sales, member} {
			r := e.do("POST", path, c, body)
			if r.status != 403 && r.status != 404 {
				t.Errorf("salary write allowed: %d", r.status)
			}
			if r := e.do("GET", path, c, nil); r.status != 403 && r.status != 404 {
				t.Errorf("salary read allowed: %d", r.status)
			}
		}
		expect(t, e.do("GET", "/api/hr/salaries", member, nil), 403, "member lists salaries")

		bad := e.do("POST", path, hr, map[string]any{"amount": "-5", "currency": "taka", "effective_from": "soon"})
		expect(t, bad, 422, "invalid salary")
		r := e.do("POST", path, hr, body)
		expect(t, r, 201, "hr adds salary")
		expect(t, e.do("POST", path, mgmt, map[string]any{"amount": "52000", "effective_from": "2026-07-01", "note": "Raise"}), 201, "ceo adds raise")
		expect(t, e.do("POST", path, hr, body), 422, "same effective date twice")
		hist := e.do("GET", path, hr, nil).body["history"].([]any)
		if len(hist) != 2 || hist[0].(map[string]any)["amount"] != "52000.00" || hist[1].(map[string]any)["currency"] != "BDT" {
			t.Fatalf("history = %v", hist)
		}
		// The current salary shows on HR's list; the person can't see theirs anywhere.
		list := e.do("GET", "/api/hr/salaries", hr, nil).body["salaries"].([]any)
		if len(list) != 1 || list[0].(map[string]any)["amount"] != "52000.00" {
			t.Fatalf("current salaries = %v", list)
		}
		for _, p := range []string{"/api/hr/me", "/api/hr/employees/" + memberID} {
			if raw := e.do("GET", p, member, nil); strings.Contains(fmtBody(raw), "52000") {
				t.Fatalf("%s leaks the salary to its owner", p)
			}
		}
		// HR's own salary: set by the CEO, invisible to HR.
		hrPath := "/api/hr/employees/" + e.users["hr"].ID.String() + "/salary"
		expect(t, e.do("POST", hrPath, hr, body), 404, "hr sets own salary")
		expect(t, e.do("GET", hrPath, hr, nil), 404, "hr reads own salary")
		expect(t, e.do("POST", hrPath, mgmt, body), 201, "ceo sets hr salary")
		for _, raw := range e.do("GET", "/api/hr/salaries", hr, nil).body["salaries"].([]any) {
			if raw.(map[string]any)["user_id"] == e.users["hr"].ID.String() {
				t.Fatal("hr sees own salary in the list")
			}
		}
		// The CEO's own salary: set by HR, invisible to the CEO.
		ceoPath := "/api/hr/employees/" + e.users["ceo"].ID.String() + "/salary"
		expect(t, e.do("GET", ceoPath, mgmt, nil), 404, "ceo reads own salary")
		expect(t, e.do("POST", ceoPath, hr, body), 201, "hr sets ceo salary")
		// Mistakes can be removed, by HR/CEO only.
		id := hist[1].(map[string]any)["id"].(string)
		expect(t, e.do("DELETE", "/api/hr/salaries/"+id, tl, nil), 403, "tl deletes")
		expect(t, e.do("DELETE", "/api/hr/salaries/"+id, hr, nil), 204, "hr deletes")
	})

	t.Run("HR has no project access", func(t *testing.T) {
		r := e.do("GET", "/api/projects", hr, nil)
		expect(t, r, 200, "projects list")
		if r.body["total"].(float64) != 0 {
			t.Fatalf("hr sees projects: %v", r.body["total"])
		}
		r = e.do("GET", "/api/updates", hr, nil)
		if r.body["total"].(float64) != 0 {
			t.Fatalf("hr sees updates: %v", r.body["total"])
		}
		expect(t, e.do("POST", "/api/projects", hr, map[string]any{"name": "x"}), 403, "hr creates project")
		expect(t, e.do("GET", "/api/reports/summary", hr, nil), 403, "hr reports")
		expect(t, e.do("GET", "/api/activity", hr, nil), 403, "hr activity")
	})

	t.Run("HR configuration is HR + CEO only and validated", func(t *testing.T) {
		for _, c := range []string{admin, tl, member, sales} {
			expect(t, e.do("POST", "/api/admin/holidays", c, map[string]any{"date": "2026-12-16", "name": "Victory Day"}), 403, "holiday")
		}
		expect(t, e.do("POST", "/api/admin/holidays", hr, map[string]any{"date": "2026-12-16", "name": "Victory Day"}), 201, "holiday")
		expect(t, e.do("POST", "/api/admin/holidays", hr, map[string]any{"date": "2026-12-16", "name": "Again"}), 422, "duplicate holiday")
		expect(t, e.do("POST", "/api/admin/departments", hr, map[string]any{"name": "Operations"}), 201, "department")
		expect(t, e.do("POST", "/api/admin/leave-types", hr, map[string]any{"name": "Maternity", "annual_quota": 0.3}), 422, "quota not in half days")
		expect(t, e.do("POST", "/api/admin/leave-types", hr, map[string]any{"name": "Maternity", "annual_quota": 120}), 201, "leave type")
		expect(t, e.do("PUT", "/api/admin/settings/weekly_off_days", hr, map[string]any{"value": []int{5, 5}}), 422, "duplicate off day")
		expect(t, e.do("PUT", "/api/admin/settings/work_start", hr, map[string]any{"value": "9am"}), 422, "bad clock")
		expect(t, e.do("PUT", "/api/admin/settings/late_grace_minutes", hr, map[string]any{"value": 10}), 200, "grace")
		h := e.do("GET", "/api/hr/holidays?year=2026", member, nil)
		if len(h.body["holidays"].([]any)) != 1 {
			t.Fatalf("holidays = %v", h.body)
		}
	})
}

func fmtBody(r resp) string {
	b, _ := json.Marshal(r.body)
	return string(b)
}
