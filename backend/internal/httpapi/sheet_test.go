package httpapi

import (
	"testing"
)

// The Google Sheet backup is system setup: only the admin flag (or the CEO) reaches it,
// and with no sheet configured the status is honest rather than an error.
func TestSheetBackupEndpoints(t *testing.T) {
	e := newEnv(t)
	admin, ceo := e.login("admin"), e.login("ceo")

	for who, cookie := range map[string]string{"sales": e.login("sales"), "tl": e.login("tl"), "member": e.login("member"), "hr": e.login("hr")} {
		expect(t, e.do("GET", "/api/admin/sheet", cookie, nil), 403, who+" cannot read the backup status")
		expect(t, e.do("POST", "/api/admin/sheet/backfill", cookie, nil), 403, who+" cannot trigger a backup")
		expect(t, e.do("POST", "/api/admin/sheet/check", cookie, nil), 403, who+" cannot test the connection")
	}

	for who, cookie := range map[string]string{"admin": admin, "ceo": ceo} {
		r := e.do("GET", "/api/admin/sheet", cookie, nil)
		expect(t, r, 200, who+" reads the backup status")
		if r.body["configured"] != false {
			t.Fatalf("%s: configured = %v (no sheet is set in tests)", who, r.body["configured"])
		}
		if _, ok := r.body["pending"]; !ok {
			t.Fatalf("%s: no queue counts: %v", who, r.body)
		}
	}

	// Nothing to write to, so both actions explain themselves instead of failing silently.
	for _, path := range []string{"/api/admin/sheet/check", "/api/admin/sheet/backfill"} {
		r := e.do("POST", path, admin, nil)
		expect(t, r, 400, path+" without a sheet")
		if msg := r.body["error"].(map[string]any)["message"]; msg != "No Google Sheet is configured" {
			t.Fatalf("%s: message = %q", path, msg)
		}
	}
}
