package sheets

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/solvinx/opsflow/internal/auth"
	"github.com/solvinx/opsflow/internal/db"
	"github.com/solvinx/opsflow/internal/store"
	"github.com/solvinx/opsflow/internal/testutil"
)

// ── A fake Google, so the whole backup is testable without credentials ──

type fakeSheets struct {
	mu      sync.Mutex
	rows    [][]string // the spreadsheet
	fail    bool       // make every data call fail
	appends int
	updates int
	tokens  int
	srv     *httptest.Server
}

func newFake(t *testing.T) *fakeSheets {
	t.Helper()
	f := &fakeSheets{}
	mux := http.NewServeMux()
	mux.HandleFunc("/token", func(w http.ResponseWriter, r *http.Request) {
		if err := r.ParseForm(); err != nil {
			t.Errorf("token form: %v", err)
		}
		if got := r.Form.Get("grant_type"); got != "urn:ietf:params:oauth:grant-type:jwt-bearer" {
			t.Errorf("grant_type = %q", got)
		}
		if parts := strings.Split(r.Form.Get("assertion"), "."); len(parts) != 3 {
			t.Errorf("assertion is not a signed JWT: %q", r.Form.Get("assertion"))
		}
		f.mu.Lock()
		f.tokens++
		f.mu.Unlock()
		_ = json.NewEncoder(w).Encode(map[string]any{"access_token": "test-token", "expires_in": 3600})
	})
	mux.HandleFunc("/v4/spreadsheets/", func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer test-token" {
			http.Error(w, `{"error":"unauthorised"}`, http.StatusUnauthorized)
			return
		}
		f.mu.Lock()
		defer f.mu.Unlock()
		if f.fail {
			http.Error(w, `{"error":{"message":"Google is having a day"}}`, http.StatusInternalServerError)
			return
		}
		switch {
		case strings.HasSuffix(r.URL.Path, ":append"):
			var body struct{ Values [][]any }
			_ = json.NewDecoder(r.Body).Decode(&body)
			first := len(f.rows) + 1
			for _, row := range body.Values {
				f.rows = append(f.rows, cells(row))
			}
			f.appends++
			_ = json.NewEncoder(w).Encode(map[string]any{
				"updates": map[string]any{"updatedRange": fmt.Sprintf("Projects!A%d:U%d", first, len(f.rows))},
			})
		case strings.HasSuffix(r.URL.Path, "values:batchUpdate"):
			var body struct {
				Data []struct {
					Range  string
					Values [][]any
				}
			}
			_ = json.NewDecoder(r.Body).Decode(&body)
			for _, d := range body.Data {
				row, err := firstRowOf(d.Range)
				if err != nil {
					t.Errorf("bad range %q: %v", d.Range, err)
					continue
				}
				for len(f.rows) < row {
					f.rows = append(f.rows, nil)
				}
				f.rows[row-1] = cells(d.Values[0])
			}
			f.updates++
			_ = json.NewEncoder(w).Encode(map[string]any{"totalUpdatedRows": len(body.Data)})
		case r.Method == "GET":
			out := [][]string{}
			for _, row := range f.rows {
				if len(row) == 0 {
					out = append(out, []string{""})
					continue
				}
				out = append(out, []string{row[0]})
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"values": out})
		default:
			t.Errorf("unexpected call %s %s", r.Method, r.URL.Path)
		}
	})
	f.srv = httptest.NewServer(mux)
	t.Cleanup(f.srv.Close)
	return f
}

func cells(row []any) []string {
	out := make([]string, len(row))
	for i, v := range row {
		out[i] = fmt.Sprint(v)
	}
	return out
}

func (f *fakeSheets) snapshot() [][]string {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([][]string, len(f.rows))
	copy(out, f.rows)
	return out
}

func (f *fakeSheets) setFail(v bool) {
	f.mu.Lock()
	f.fail = v
	f.mu.Unlock()
}

// client points a real Client at the fake, with a freshly generated service-account key.
func (f *fakeSheets) client(t *testing.T) *Client {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	der, err := x509.MarshalPKCS8PrivateKey(key)
	if err != nil {
		t.Fatal(err)
	}
	creds, err := json.Marshal(map[string]string{
		"type":         "service_account",
		"client_email": "opsflow-backup@example.iam.gserviceaccount.com",
		"private_key":  string(pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: der})),
	})
	if err != nil {
		t.Fatal(err)
	}
	c, err := New(Config{
		SpreadsheetID: "sheet-123", Tab: "Projects", CredentialsJSON: creds,
		TokenURL: f.srv.URL + "/token", APIBase: f.srv.URL,
	})
	if err != nil {
		t.Fatal(err)
	}
	return c
}

// ── Fixtures ───────────────────────────────────────────────

type fixture struct {
	t       *testing.T
	st      *store.Store
	sales   db.User
	ops     db.User
	profile uuid.UUID
	ptype   uuid.UUID
}

func newFixture(t *testing.T) *fixture {
	st := testutil.NewStore(t, "sheets")
	ctx := context.Background()
	hash := testutil.Must(auth.HashPassword("password123!"))
	mk := func(email, name, role string) db.User {
		return testutil.Must(st.CreateUser(ctx, db.CreateUserParams{
			Email: email, Name: name, PasswordHash: hash, Role: role,
		}))
	}
	profile := testutil.Must(st.CreateProfile(ctx, db.CreateProfileParams{Name: "Studio Main", Platform: "Fiverr"}))
	ptype := testutil.Must(st.CreateProjectType(ctx, db.CreateProjectTypeParams{Name: "Shopify Store"}))
	return &fixture{
		t: t, st: st,
		sales:   mk("sales@test.local", "Sam Sales", "sales"),
		ops:     mk("milo@test.local", "Milo Member", "member"),
		profile: profile.ID, ptype: ptype.ID,
	}
}

// project inserts a project the way the app does, as a draft, and returns its id.
func (f *fixture) project(name, client, order, price, deadline string) uuid.UUID {
	f.t.Helper()
	scope := "Build the storefront."
	return testutil.Must(f.st.CreateProject(context.Background(), db.CreateProjectParams{
		Name: &name, ClientName: &client, OrderID: &order, AgreedPrice: &price,
		ProfileID: &f.profile, ProjectTypeID: &f.ptype, Scope: &scope,
		Currency: "USD", Deadline: &deadline, Priority: "medium", CreatedBy: f.sales.ID,
	}))
}

// setStatus moves a project the way the workflow would, giving it an assignee once
// it's with the team (the database insists on one).
func (f *fixture) setStatus(id uuid.UUID, status string) {
	f.t.Helper()
	active := map[string]bool{"in_progress": true, "blocked": true, "ready_for_review": true, "ready_to_deliver": true}
	q := `UPDATE projects SET status = $1 WHERE id = $2`
	if active[status] {
		q = `UPDATE projects SET status = $1, assigned_to = $3 WHERE id = $2`
		if _, err := f.st.Pool.Exec(context.Background(), q, status, id, f.ops.ID); err != nil {
			f.t.Fatal(err)
		}
		return
	}
	if _, err := f.st.Pool.Exec(context.Background(), q, status, id); err != nil {
		f.t.Fatal(err)
	}
}

func (f *fixture) pending() int64 {
	f.t.Helper()
	row := testutil.Must(f.st.SheetSyncStatus(context.Background()))
	return row.Pending
}

// ── Tests ──────────────────────────────────────────────────

func TestSheetBackup(t *testing.T) {
	f := newFixture(t)
	fake := newFake(t)
	syncer := NewSyncer(f.st, fake.client(t))
	ctx := context.Background()

	t.Run("a new project is queued straight away", func(t *testing.T) {
		f.project("First store", "Nobody", "D-1", "100.00", "2026-12-01T17:00:00Z")
		if f.pending() != 1 {
			t.Fatal("a new project was not queued")
		}
	})

	t.Run("the first write adds the header and the project's row", func(t *testing.T) {
		n, err := syncer.Once(ctx)
		if err != nil || n != 1 {
			t.Fatalf("wrote %d (%v)", n, err)
		}
		rows := fake.snapshot()
		if len(rows) != 2 {
			t.Fatalf("rows: %v", rows)
		}
		if strings.Join(rows[0], "|") != strings.Join(Header, "|") {
			t.Fatalf("header: %v", rows[0])
		}
		got := rows[1]
		if got[0] != "1" || got[1] != "First store" || got[2] != "New" || got[3] != "Nobody" ||
			got[5] != "D-1" || got[9] != "Sam Sales" || got[13] != "100.00" || got[14] != "USD" {
			t.Fatalf("row: %v", got)
		}
		if f.pending() != 0 {
			t.Fatal("still queued after a successful write")
		}
	})

	t.Run("a change rewrites the same row", func(t *testing.T) {
		var id uuid.UUID
		if err := f.st.Pool.QueryRow(ctx, `SELECT id FROM projects WHERE order_id = 'D-1'`).Scan(&id); err != nil {
			t.Fatal(err)
		}
		f.setStatus(id, "in_progress")
		if f.pending() != 1 {
			t.Fatal("a status change did not queue it")
		}
		before := fake.appends
		if _, err := syncer.Once(ctx); err != nil {
			t.Fatal(err)
		}
		rows := fake.snapshot()
		if len(rows) != 2 {
			t.Fatalf("a change added a row: %v", rows)
		}
		if rows[1][2] != "In progress" {
			t.Fatalf("status not updated: %v", rows[1])
		}
		if fake.appends != before {
			t.Fatal("an update went through append")
		}
	})

	t.Run("members and later projects", func(t *testing.T) {
		var id uuid.UUID
		if err := f.st.Pool.QueryRow(ctx, `SELECT id FROM projects WHERE order_id = 'D-1'`).Scan(&id); err != nil {
			t.Fatal(err)
		}
		if _, err := f.st.Pool.Exec(ctx,
			`INSERT INTO project_members (project_id, user_id, added_by) VALUES ($1, $2, $3)`, id, f.ops.ID, f.sales.ID); err != nil {
			t.Fatal(err)
		}
		if f.pending() != 1 {
			t.Fatal("adding a member did not queue the project")
		}
		f.project("Second store", "Acme Ltd", "D-2", "2500.00", "2026-11-20T09:30:00Z")
		if _, err := syncer.Once(ctx); err != nil {
			t.Fatal(err)
		}
		rows := fake.snapshot()
		if len(rows) != 3 {
			t.Fatalf("rows: %v", rows)
		}
		if rows[1][11] != "Milo Member" {
			t.Fatalf("members column: %v", rows[1])
		}
		if rows[2][0] != "2" || rows[2][1] != "Second store" || rows[2][3] != "Acme Ltd" {
			t.Fatalf("second project's row: %v", rows[2])
		}
		// The deadline is written as a moment, in the organisation's timezone.
		if !strings.HasPrefix(rows[2][15], "2026-11-20") {
			t.Fatalf("deadline: %q", rows[2][15])
		}
	})

	t.Run("a failure is recorded and backed off, then recovers", func(t *testing.T) {
		var id uuid.UUID
		if err := f.st.Pool.QueryRow(ctx, `SELECT id FROM projects WHERE order_id = 'D-2'`).Scan(&id); err != nil {
			t.Fatal(err)
		}
		fake.setFail(true)
		f.setStatus(id, "blocked")
		if _, err := syncer.Once(ctx); err == nil {
			t.Fatal("a failing Google did not surface an error")
		}
		st := testutil.Must(ReadStatus(ctx, f.st, nil))
		if st.Pending != 1 || st.Failing != 1 || !strings.Contains(st.LastError, "Google is having a day") {
			t.Fatalf("status after failure: %+v", st)
		}
		// Backed off, so the next pass leaves it alone.
		if n, err := syncer.Once(ctx); err != nil || n != 0 {
			t.Fatalf("retried immediately: %d (%v)", n, err)
		}
		fake.setFail(false)
		if _, err := f.st.Pool.Exec(ctx, `UPDATE sheet_sync SET next_attempt_at = now() WHERE project_id = $1`, id); err != nil {
			t.Fatal(err)
		}
		if n, err := syncer.Once(ctx); err != nil || n != 1 {
			t.Fatalf("recovery wrote %d (%v)", n, err)
		}
		if rows := fake.snapshot(); rows[2][2] != "Blocked" {
			t.Fatalf("row after recovery: %v", rows[2])
		}
		if st := testutil.Must(ReadStatus(ctx, f.st, nil)); st.Pending != 0 || st.Failing != 0 || st.LastSyncedAt == nil {
			t.Fatalf("status after recovery: %+v", st)
		}
	})

	t.Run("backfill rewrites every project without duplicating rows", func(t *testing.T) {
		n, err := syncer.Backfill(ctx)
		if err != nil {
			t.Fatal(err)
		}
		if n != 2 {
			t.Fatalf("backfilled %d", n)
		}
		rows := fake.snapshot()
		if len(rows) != 3 {
			t.Fatalf("backfill duplicated rows: %v", rows)
		}
		if rows[1][0] != "1" || rows[2][0] != "2" {
			t.Fatalf("rows out of order: %v", rows)
		}
	})

	t.Run("one token is reused across calls", func(t *testing.T) {
		if fake.tokens != 1 {
			t.Fatalf("asked google for %d tokens", fake.tokens)
		}
	})

	t.Run("the check reads the sheet and writes nothing", func(t *testing.T) {
		c := fake.client(t)
		if err := c.Check(ctx); err != nil {
			t.Fatal(err)
		}
		fake.setFail(true)
		defer fake.setFail(false)
		err := c.Check(ctx)
		if err == nil || !strings.Contains(err.Error(), "shared with opsflow-backup@example.iam.gserviceaccount.com") {
			t.Fatalf("unhelpful failure: %v", err)
		}
	})
}

func TestLoadConfig(t *testing.T) {
	cases := []struct {
		name string
		env  map[string]string
		on   bool
		err  string
	}{
		{name: "off by default", env: map[string]string{}},
		{
			name: "a sheet without a key is a mistake worth reporting",
			env:  map[string]string{"SHEETS_SPREADSHEET_ID": "abc"},
			on:   true, err: "no service account key",
		},
		{
			name: "inline json, default tab",
			env:  map[string]string{"SHEETS_SPREADSHEET_ID": "abc", "GOOGLE_SERVICE_ACCOUNT_JSON": `{"client_email":"x"}`},
			on:   true,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			cfg, on, err := LoadConfig(func(k string) string { return tc.env[k] })
			if on != tc.on {
				t.Fatalf("enabled = %v", on)
			}
			if tc.err == "" && err != nil {
				t.Fatal(err)
			}
			if tc.err != "" && (err == nil || !strings.Contains(err.Error(), tc.err)) {
				t.Fatalf("error = %v, want %q", err, tc.err)
			}
			if on && err == nil && cfg.Tab != "Projects" {
				t.Fatalf("tab = %q", cfg.Tab)
			}
		})
	}
}

func TestColumnNameAndRange(t *testing.T) {
	for n, want := range map[int]string{1: "A", 21: "U", 26: "Z", 27: "AA", 52: "AZ"} {
		if got := columnName(n); got != want {
			t.Errorf("columnName(%d) = %q, want %q", n, got, want)
		}
	}
	for a1, want := range map[string]int{"Projects!A14:U14": 14, "A2": 2, "'My tab'!A7:B9": 7} {
		got, err := firstRowOf(a1)
		if err != nil || got != want {
			t.Errorf("firstRowOf(%q) = %d, %v; want %d", a1, got, err, want)
		}
	}
	if _, err := firstRowOf("Projects!AA"); err == nil {
		t.Error("a range with no row should be refused")
	}
}

// The token should not be re-fetched while it is valid.
func TestTokenCache(t *testing.T) {
	fake := newFake(t)
	c := fake.client(t)
	ctx := context.Background()
	for i := 0; i < 3; i++ {
		if _, err := c.accessToken(ctx); err != nil {
			t.Fatal(err)
		}
	}
	if fake.tokens != 1 {
		t.Fatalf("fetched %d tokens", fake.tokens)
	}
	// An expiring token is replaced.
	c.expires = time.Now().Add(10 * time.Second)
	if _, err := c.accessToken(ctx); err != nil {
		t.Fatal(err)
	}
	if fake.tokens != 2 {
		t.Fatalf("expiring token not refreshed: %d", fake.tokens)
	}
}
