package httpapi

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gofiber/fiber/v3"

	"github.com/solvinx/opsflow/internal/auth"
	"github.com/solvinx/opsflow/internal/config"
	"github.com/solvinx/opsflow/internal/db"
	"github.com/solvinx/opsflow/internal/notify"
	"github.com/solvinx/opsflow/internal/policy"
	"github.com/solvinx/opsflow/internal/projects"
	"github.com/solvinx/opsflow/internal/store"
	"github.com/solvinx/opsflow/internal/testutil"
)

const (
	testOrigin = "http://app.test"
	testPass   = "password123!"
)

type env struct {
	t     *testing.T
	app   *fiber.App
	svc   *projects.Service
	hub   *notify.Hub
	store *store.Store
	users map[string]db.User // by label
}

func newEnv(t *testing.T) *env {
	st := testutil.NewStore(t, "httpapi")
	rdb := testutil.NewRedis(t, 15)
	cfg := config.Config{
		AppOrigins: []string{testOrigin}, SessionTTL: time.Hour, SessionMaxAge: 24 * time.Hour,
		MaxUploadSize: 5 << 20,
	}
	hub := notify.NewHub()
	app, svc, _, _ := NewWithServices(cfg, st, rdb, testutil.NewStorage(t), hub)
	e := &env{t: t, app: app, svc: svc, hub: hub, store: st, users: map[string]db.User{}}
	e.addUser("admin", policy.TL, true)
	for _, r := range policy.Roles {
		e.addUser(string(r), r, false)
	}
	return e
}

func (e *env) addUser(label string, role policy.Role, admin bool) db.User {
	hash := testutil.Must(auth.HashPassword(testPass))
	u := testutil.Must(e.store.CreateUser(context.Background(), db.CreateUserParams{
		Email: label + "@test.local", Name: label, PasswordHash: hash, Role: string(role), IsAdmin: admin,
	}))
	e.users[label] = u
	return u
}

type resp struct {
	status int
	body   map[string]any
	cookie string
}

func (e *env) do(method, path, cookie string, body any) resp {
	e.t.Helper()
	var r io.Reader
	if body != nil {
		b, _ := json.Marshal(body)
		r = bytes.NewReader(b)
	}
	req := httptest.NewRequest(method, path, r)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Origin", testOrigin)
	req.Header.Set("User-Agent", "opsflow-test")
	if cookie != "" {
		req.AddCookie(&http.Cookie{Name: sessionCookie, Value: cookie})
	}
	res, err := e.app.Test(req, fiber.TestConfig{Timeout: 5 * time.Second})
	if err != nil {
		e.t.Fatalf("%s %s: %v", method, path, err)
	}
	defer res.Body.Close()
	out := resp{status: res.StatusCode, body: map[string]any{}}
	raw, _ := io.ReadAll(res.Body)
	_ = json.Unmarshal(raw, &out.body)
	for _, c := range res.Cookies() {
		if c.Name == sessionCookie {
			out.cookie = c.Value
		}
	}
	return out
}

func (e *env) login(label string) string {
	e.t.Helper()
	r := e.do("POST", "/api/auth/login", "", map[string]string{"email": label + "@test.local", "password": testPass})
	if r.status != 200 || r.cookie == "" {
		e.t.Fatalf("login %s: status %d body %v", label, r.status, r.body)
	}
	return r.cookie
}

func expect(t *testing.T, r resp, status int, what string) {
	t.Helper()
	if r.status != status {
		t.Fatalf("%s: got %d want %d (body %v)", what, r.status, status, r.body)
	}
}

func TestPhase1(t *testing.T) {
	e := newEnv(t)

	t.Run("protected routes need a session", func(t *testing.T) {
		for _, p := range []string{"/api/auth/me", "/api/settings", "/api/profiles", "/api/users/directory", "/api/admin/users"} {
			expect(t, e.do("GET", p, "", nil), 401, p)
			expect(t, e.do("GET", p, "forged-token", nil), 401, p+" forged")
		}
	})

	t.Run("state-changing requests need an allowed origin", func(t *testing.T) {
		req := httptest.NewRequest("POST", "/api/auth/login", strings.NewReader(`{"email":"sales@test.local","password":"password123!"}`))
		req.Header.Set("Origin", "http://evil.test")
		res, _ := e.app.Test(req)
		if res.StatusCode != 403 {
			t.Fatalf("got %d want 403", res.StatusCode)
		}
	})

	t.Run("wrong password and unknown email look the same", func(t *testing.T) {
		a := e.do("POST", "/api/auth/login", "", map[string]string{"email": "sales@test.local", "password": "wrong-password"})
		b := e.do("POST", "/api/auth/login", "", map[string]string{"email": "nobody@test.local", "password": "wrong-password"})
		expect(t, a, 401, "wrong password")
		expect(t, b, 401, "unknown email")
		if a.body["error"].(map[string]any)["code"] != b.body["error"].(map[string]any)["code"] {
			t.Fatal("error codes differ")
		}
	})

	t.Run("me returns role permissions, never the hash", func(t *testing.T) {
		r := e.do("GET", "/api/auth/me", e.login("member"), nil)
		expect(t, r, 200, "me")
		u := r.body["user"].(map[string]any)
		if _, leaked := u["password_hash"]; leaked {
			t.Fatal("password hash leaked")
		}
		perms := u["permissions"].([]any)
		if len(perms) != len(policy.ActionsFor(policy.Member)) {
			t.Fatalf("permissions = %v", perms)
		}
	})

	t.Run("people routes are HR + CEO; system setup is admins + CEO", func(t *testing.T) {
		some := e.users["sales"].ID.String()
		people := []struct{ m, p string }{
			{"GET", "/api/admin/users"},
			{"POST", "/api/admin/users"},
			{"PATCH", "/api/admin/users/" + some},
			{"POST", "/api/admin/users/" + some + "/reset-password"},
			{"POST", "/api/admin/departments"},
			{"POST", "/api/admin/holidays"},
			{"PUT", "/api/admin/settings/work_start"},
		}
		system := []struct{ m, p string }{
			{"POST", "/api/admin/profiles"},
			{"POST", "/api/admin/project-types"},
			{"PUT", "/api/admin/settings/inbox_requires_approval"},
		}
		for _, label := range []string{"admin", "sales", "tl", "member", "ceo", "hr"} {
			cookie := e.login(label)
			managesPeople := label == "ceo" || label == "hr"
			sysAdmin := label == "ceo" || label == "admin"
			for _, rt := range people {
				r := e.do(rt.m, rt.p, cookie, map[string]any{})
				if (r.status == 403) == managesPeople {
					t.Errorf("%s %s %s: status %d", label, rt.m, rt.p, r.status)
				}
			}
			for _, rt := range system {
				r := e.do(rt.m, rt.p, cookie, map[string]any{})
				if (r.status == 403) == sysAdmin {
					t.Errorf("%s %s %s: status %d", label, rt.m, rt.p, r.status)
				}
			}
		}
	})

	admin, hr, ceo := e.login("admin"), e.login("hr"), e.login("ceo")

	t.Run("user and settings input is validated", func(t *testing.T) {
		r := e.do("POST", "/api/admin/users", hr, map[string]any{"email": "x", "name": "", "role": "boss", "password": "short"})
		expect(t, r, 422, "invalid user")
		fields := r.body["error"].(map[string]any)["fields"].(map[string]any)
		for _, f := range []string{"email", "name", "role", "password"} {
			if _, ok := fields[f]; !ok {
				t.Errorf("missing field error for %s", f)
			}
		}
		expect(t, e.do("POST", "/api/admin/users", hr, map[string]any{"email": "a@b.co", "extra": 1}), 400, "unknown field")
		expect(t, e.do("PUT", "/api/admin/settings/nope", admin, map[string]any{"value": true}), 404, "unknown setting")
		expect(t, e.do("PUT", "/api/admin/settings/org_timezone", admin, map[string]any{"value": "Mars/Base"}), 422, "bad tz")
		expect(t, e.do("PUT", "/api/admin/settings/batch_window_minutes", admin, map[string]any{"value": 0}), 422, "bad window")
		expect(t, e.do("PUT", "/api/admin/settings/inbox_requires_approval", admin, map[string]any{"value": "yes"}), 422, "bad bool")
	})

	t.Run("admin can toggle inbox approval; settings are role-filtered", func(t *testing.T) {
		expect(t, e.do("PUT", "/api/admin/settings/inbox_requires_approval", admin, map[string]any{"value": false}), 200, "toggle")
		// Privileged roles (ceo/hr/admin) see the full map.
		r := e.do("GET", "/api/settings", ceo, nil)
		expect(t, r, 200, "ceo reads settings")
		if r.body["settings"].(map[string]any)["inbox_requires_approval"] != false {
			t.Fatalf("settings = %v", r.body)
		}
		// Other internal roles get office_location only — never org config.
		r = e.do("GET", "/api/settings", e.login("sales"), nil)
		expect(t, r, 200, "sales reads settings")
		if _, leaked := r.body["settings"].(map[string]any)["inbox_requires_approval"]; leaked {
			t.Fatalf("sales sees privileged setting: %v", r.body)
		}
		// Client and investor callers are blocked outright.
		expect(t, e.do("GET", "/api/settings", e.login("client"), nil), 403, "client reads settings")
		expect(t, e.do("GET", "/api/settings", e.login("investor"), nil), 403, "investor reads settings")
	})

	t.Run("role change applies on the next request", func(t *testing.T) {
		u := e.addUser("switcher", policy.Sales, false)
		cookie := e.login("switcher")
		expect(t, e.do("PATCH", "/api/admin/users/"+u.ID.String(), hr, map[string]any{"role": "tl"}), 200, "change role")
		r := e.do("GET", "/api/auth/me", cookie, nil)
		if r.body["user"].(map[string]any)["role"] != "tl" {
			t.Fatalf("role not updated: %v", r.body)
		}
	})

	t.Run("HR and CEO boundaries", func(t *testing.T) {
		u := e.addUser("climber", policy.Member, false)
		path := "/api/admin/users/" + u.ID.String()
		ceoPath := "/api/admin/users/" + e.users["ceo"].ID.String()
		hrPath := "/api/admin/users/" + e.users["hr"].ID.String()
		// HR can't hand out HR or admin access, or touch the CEO or other HR accounts.
		expect(t, e.do("PATCH", path, hr, map[string]any{"role": "hr"}), 403, "hr grants hr")
		expect(t, e.do("PATCH", path, hr, map[string]any{"is_admin": true}), 403, "hr grants admin")
		expect(t, e.do("POST", "/api/admin/users", hr, map[string]any{"email": "n@test.local", "name": "N", "role": "hr", "password": testPass}), 403, "hr creates hr")
		expect(t, e.do("PATCH", ceoPath, hr, map[string]any{"is_active": false}), 403, "hr deactivates ceo")
		expect(t, e.do("POST", ceoPath+"/reset-password", hr, map[string]any{"password": "a-new-password"}), 403, "hr resets ceo password")
		// Nobody becomes CEO through the API.
		expect(t, e.do("PATCH", path, ceo, map[string]any{"role": "ceo"}), 422, "second ceo")
		// The CEO grants HR and admin access and manages HR accounts.
		expect(t, e.do("PATCH", path, ceo, map[string]any{"role": "hr", "is_admin": true}), 200, "ceo grants")
		expect(t, e.do("PATCH", path, hr, map[string]any{"is_active": false}), 403, "hr deactivates another hr")
		expect(t, e.do("PATCH", path, ceo, map[string]any{"is_active": false, "reason": "Left the company"}), 200, "ceo deactivates hr")
		// Nobody changes their own access; the CEO may fix their own name.
		expect(t, e.do("PATCH", hrPath, hr, map[string]any{"role": "tl"}), 409, "hr self role")
		expect(t, e.do("PATCH", ceoPath, ceo, map[string]any{"is_active": false}), 409, "ceo self deactivate")
		expect(t, e.do("PATCH", ceoPath, ceo, map[string]any{"name": "The CEO"}), 200, "ceo renames self")
	})

	t.Run("deactivation ends sessions and blocks login", func(t *testing.T) {
		u := e.addUser("leaver", policy.Member, false)
		cookie := e.login("leaver")
		expect(t, e.do("PATCH", "/api/admin/users/"+u.ID.String(), hr, map[string]any{"is_active": false, "reason": "Left the company"}), 200, "deactivate")
		expect(t, e.do("GET", "/api/auth/me", cookie, nil), 401, "old session")
		expect(t, e.do("POST", "/api/auth/login", "", map[string]string{"email": "leaver@test.local", "password": testPass}), 401, "login")
	})

	t.Run("password change keeps this session and ends the others", func(t *testing.T) {
		e.addUser("pw", policy.Sales, false)
		a, b := e.login("pw"), e.login("pw")
		expect(t, e.do("POST", "/api/auth/password", a, map[string]any{"current_password": "wrong-one", "new_password": "a-new-password"}), 422, "wrong current")
		expect(t, e.do("POST", "/api/auth/password", a, map[string]any{"current_password": testPass, "new_password": "a-new-password"}), 204, "change")
		expect(t, e.do("GET", "/api/auth/me", a, nil), 200, "this session")
		expect(t, e.do("GET", "/api/auth/me", b, nil), 401, "other session")
	})

	t.Run("logout ends the session", func(t *testing.T) {
		c := e.login("tl")
		expect(t, e.do("POST", "/api/auth/logout", c, nil), 204, "logout")
		expect(t, e.do("GET", "/api/auth/me", c, nil), 401, "after logout")
	})

	t.Run("repeated failures are rate limited", func(t *testing.T) {
		e.addUser("victim", policy.Sales, false)
		bad := map[string]string{"email": "victim@test.local", "password": "wrong-password"}
		for range maxFailsPerEmail {
			expect(t, e.do("POST", "/api/auth/login", "", bad), 401, "fail")
		}
		expect(t, e.do("POST", "/api/auth/login", "", bad), 429, "limited")
		good := map[string]string{"email": "victim@test.local", "password": testPass}
		expect(t, e.do("POST", "/api/auth/login", "", good), 429, "limited even with right password")
	})

	t.Run("admin actions are written to the activity log", func(t *testing.T) {
		var n int
		err := e.store.Pool.QueryRow(context.Background(),
			`SELECT count(*) FROM activity_log WHERE action IN ('user.updated','setting.updated')`).Scan(&n)
		if err != nil || n < 3 {
			t.Fatalf("activity rows = %d, err %v", n, err)
		}
	})
}

func cookieFor(v string) *http.Cookie { return &http.Cookie{Name: sessionCookie, Value: v} }
