// Command opsctl is the admin CLI: migrations, bootstrap admin and dev seed data.
//
//	opsctl migrate
//	opsctl create-admin -email a@b.c -name "Admin" -role tl   (password from OPSCTL_PASSWORD or prompt-free random)
//	opsctl transfer-ceo -email new@b.c [-old-role member]   (the current CEO gets -old-role)
//	opsctl seed-dev
//	opsctl sheet-check                 (are the Google credentials and sharing right?)
//	opsctl sheet-backfill              (write every project to the sheet again)
package main

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"flag"
	"fmt"
	"os"
	"strings"
	_ "time/tzdata"

	"github.com/google/uuid"

	"github.com/solvinx/opsflow/internal/auth"
	"github.com/solvinx/opsflow/internal/db"
	"github.com/solvinx/opsflow/internal/policy"
	"github.com/solvinx/opsflow/internal/sheets"
	"github.com/solvinx/opsflow/internal/store"
)

func main() {
	if len(os.Args) < 2 {
		usage()
	}
	ctx := context.Background()
	url := os.Getenv("DATABASE_URL")
	if url == "" {
		fail("DATABASE_URL is required")
	}
	st, err := store.Open(ctx, url)
	if err != nil {
		fail(err.Error())
	}
	defer st.Close()

	switch os.Args[1] {
	case "migrate":
		must(st.Migrate(ctx))
		fmt.Println("migrations applied")
	case "create-admin":
		fs := flag.NewFlagSet("create-admin", flag.ExitOnError)
		email := fs.String("email", "", "email")
		name := fs.String("name", "Administrator", "display name")
		role := fs.String("role", string(policy.TL), "role: sales|tl|member|hr|ceo")
		_ = fs.Parse(os.Args[2:])
		if *email == "" || !policy.Role(*role).Valid() {
			fail("create-admin needs -email and a valid -role")
		}
		pw := os.Getenv("OPSCTL_PASSWORD")
		if pw == "" {
			pw = randomPassword()
			defer fmt.Printf("generated password: %s\n", pw)
		}
		must(st.Migrate(ctx))
		u := createUser(ctx, st, *email, *name, *role, true, pw)
		fmt.Printf("created admin %s (%s)\n", u.Email, u.Role)
	case "transfer-ceo":
		fs := flag.NewFlagSet("transfer-ceo", flag.ExitOnError)
		email := fs.String("email", "", "email of the new CEO")
		oldRole := fs.String("old-role", string(policy.Member), "role for the current CEO: sales|tl|member|hr")
		_ = fs.Parse(os.Args[2:])
		if *email == "" || !policy.Role(*oldRole).Valid() || policy.Role(*oldRole) == policy.CEO {
			fail("transfer-ceo needs -email and a valid -old-role (not ceo)")
		}
		must(st.Migrate(ctx))
		transferCEO(ctx, st, strings.ToLower(*email), *oldRole)
	case "seed-dev":
		seedDev(ctx, st)
	case "sheet-check", "sheet-backfill":
		cfg, on, err := sheets.LoadConfig(os.Getenv)
		must(err)
		if !on {
			fail("no Google Sheet configured: set SHEETS_SPREADSHEET_ID and a service account key")
		}
		client, err := sheets.New(cfg)
		must(err)
		must(client.Check(ctx))
		fmt.Printf("sheet %s, tab %q, reachable as %s\n", client.SpreadsheetID(), client.Tab(), client.ServiceAccountEmail())
		if os.Args[1] == "sheet-backfill" {
			n, err := sheets.NewSyncer(st, client).Backfill(ctx)
			must(err)
			fmt.Printf("backed up %d project%s\n", n, plural(n))
		}
	default:
		usage()
	}
}

// seedDev creates one user per role plus lookups. Refuses to run on a non-empty DB.
func seedDev(ctx context.Context, st *store.Store) {
	must(st.Migrate(ctx))
	n, err := st.CountUsers(ctx)
	must(err)
	if n > 0 {
		fail("seed-dev only runs on an empty database")
	}
	const pw = "password123!"
	admin := createUser(ctx, st, "admin@opsflow.local", "Ava Admin", string(policy.TL), true, pw)
	sales := createUser(ctx, st, "sales@opsflow.local", "Sam Sales", string(policy.Sales), false, pw)
	tl := createUser(ctx, st, "tl@opsflow.local", "Tara Lead", string(policy.TL), false, pw)
	member := createUser(ctx, st, "member@opsflow.local", "Milo Member", string(policy.Member), false, pw)
	member2 := createUser(ctx, st, "member2@opsflow.local", "Mina Member", string(policy.Member), false, pw)
	member3 := createUser(ctx, st, "member3@opsflow.local", "Omar Member", string(policy.Member), false, pw)
	ceo := createUser(ctx, st, "ceo@opsflow.local", "Cara CEO", string(policy.CEO), false, pw)
	hrUser := createUser(ctx, st, "hr@opsflow.local", "Hana HR", string(policy.HR), false, pw)
	office := createUser(ctx, st, "office@opsflow.local", "Omi Office", string(policy.OfficeAdmin), false, pw)
	// Two demo investors (Phase 3): linked logins, capital, and shares that
	// add to 40% against the 10% default reserve.
	inv1 := createUser(ctx, st, "investor1@opsflow.local", "Ira Investor", string(policy.Investor), false, pw)
	inv2 := createUser(ctx, st, "investor2@opsflow.local", "Ivan Investor", string(policy.Investor), false, pw)
	for _, seed := range []struct {
		u      db.User
		amount string
		cur    string
		rate   string
		share  string
	}{
		{inv1, "500000", "BDT", "1", "25"},
		{inv2, "2000", "USD", "120", "15"},
	} {
		var invID uuid.UUID
		err := st.Pool.QueryRow(ctx, `
			INSERT INTO investors (user_id, name, email, join_date)
			VALUES ($1, $2, $3, CURRENT_DATE - 60) RETURNING id`,
			seed.u.ID, seed.u.Name, seed.u.Email).Scan(&invID)
		must(err)
		_, err = st.Pool.Exec(ctx, `
			INSERT INTO investments (investor_id, amount, currency, rate_to_bdt, method, invested_at, note, created_by)
			VALUES ($1, $2::numeric, $3, $4::numeric, 'Bank Transfer', CURRENT_DATE - 60, 'Seed capital', $5)`,
			invID, seed.amount, seed.cur, seed.rate, ceo.ID)
		must(err)
		_, err = st.Pool.Exec(ctx, `
			INSERT INTO investor_shares (investor_id, share_percent, effective_from, created_by)
			VALUES ($1, $2::numeric, CURRENT_DATE - 60, $3)`,
			invID, seed.share, ceo.ID)
		must(err)
	}
	for _, p := range [][2]string{{"Studio Main", "Fiverr"}, {"Studio Pro", "Upwork"}} {
		_, err := st.CreateProfile(ctx, db.CreateProfileParams{Name: p[0], Platform: p[1]})
		must(err)
	}
	// The org chart: departments, the reporting line, and two Operations teams.
	depts := map[string]uuid.UUID{}
	for _, name := range []string{"Sales", "BDT", "Operations", "HR"} {
		d, err := st.CreateDepartment(ctx, name)
		must(err)
		depts[name] = d.ID
	}
	for _, p := range []struct {
		u       db.User
		dept    string
		manager *db.User
	}{
		{ceo, "", nil}, {hrUser, "HR", &ceo}, {office, "HR", &hrUser}, {sales, "Sales", &ceo},
		{admin, "Operations", &ceo}, {tl, "Operations", &ceo},
		{member, "Operations", &tl}, {member3, "Operations", &tl}, {member2, "Operations", &admin},
	} {
		var dept, manager *uuid.UUID
		if id, ok := depts[p.dept]; ok {
			dept = &id
		}
		if p.manager != nil {
			manager = &p.manager.ID
		}
		_, err := st.Pool.Exec(ctx, "UPDATE employees SET department_id = $2, manager_id = $3 WHERE user_id = $1", p.u.ID, dept, manager)
		must(err)
	}
	ops := depts["Operations"]
	// Shopify (Tara + Milo, Omar) gets Shopify Store projects, Web Dev (Ava + Mina) Websites.
	shopify, err := st.CreateTeam(ctx, db.CreateTeamParams{Name: "Shopify", LeadID: &tl.ID, DepartmentID: &ops})
	must(err)
	web, err := st.CreateTeam(ctx, db.CreateTeamParams{Name: "Web Dev", LeadID: &admin.ID, DepartmentID: &ops})
	must(err)
	must(st.SetTeamMember(ctx, db.SetTeamMemberParams{UserID: member.ID, TeamID: shopify.ID}))
	must(st.SetTeamMember(ctx, db.SetTeamMemberParams{UserID: member3.ID, TeamID: shopify.ID}))
	must(st.SetTeamMember(ctx, db.SetTeamMemberParams{UserID: member2.ID, TeamID: web.ID}))
	for t, team := range map[string]*uuid.UUID{"Website": &web.ID, "Shopify Store": &shopify.ID, "Mobile App": nil, "Design": nil} {
		_, err := st.CreateProjectType(ctx, db.CreateProjectTypeParams{Name: t, TeamID: team})
		must(err)
	}
	fmt.Println("seeded dev users (password: " + pw + "): admin@, sales@, tl@, member@, member2@, member3@, ceo@, hr@, office@, investor1@, investor2@ opsflow.local")
}

// transferCEO hands the CEO role to another active user in one transaction.
func transferCEO(ctx context.Context, st *store.Store, email, oldRole string) {
	next, err := st.GetUserByEmail(ctx, email)
	if err != nil || !next.IsActive {
		fail("no active user with email " + email)
	}
	ceo := string(policy.CEO)
	must(st.Tx(ctx, func(q *db.Queries) error {
		current, err := q.ListUsers(ctx, db.ListUsersParams{Role: &ceo})
		if err != nil {
			return err
		}
		for _, u := range current {
			if u.ID == next.ID {
				return fmt.Errorf("%s is already the CEO", email)
			}
			if _, err := q.UpdateUser(ctx, db.UpdateUserParams{ID: u.ID, Role: &oldRole}); err != nil {
				return err
			}
		}
		_, err = q.UpdateUser(ctx, db.UpdateUserParams{ID: next.ID, Role: &ceo})
		if err != nil {
			return err
		}
		return store.LogActivity(ctx, q, store.Activity{
			Action: "user.ceo_transferred", EntityType: "user", EntityID: &next.ID,
			Metadata: map[string]any{"to": email, "previous_ceo_role": oldRole},
		})
	}))
	fmt.Printf("%s is now the CEO\n", email)
}

func createUser(ctx context.Context, st *store.Store, email, name, role string, admin bool, pw string) db.User {
	if len(pw) < auth.MinPasswordLen {
		fail(fmt.Sprintf("password must be at least %d characters", auth.MinPasswordLen))
	}
	hash, err := auth.HashPassword(pw)
	must(err)
	u, err := st.CreateUser(ctx, db.CreateUserParams{
		Email: strings.ToLower(email), Name: name, PasswordHash: hash, Role: role, IsAdmin: admin,
	})
	must(err)
	return u
}

func randomPassword() string {
	b := make([]byte, 15)
	_, _ = rand.Read(b)
	return base64.RawURLEncoding.EncodeToString(b)
}

func must(err error) {
	if err != nil {
		fail(err.Error())
	}
}

func fail(msg string) {
	fmt.Fprintln(os.Stderr, "opsctl:", msg)
	os.Exit(1)
}

func usage() {
	fmt.Fprintln(os.Stderr, "usage: opsctl migrate | create-admin -email E [-name N] [-role R] | transfer-ceo -email E [-old-role R] | seed-dev | sheet-check | sheet-backfill")
	os.Exit(2)
}

func plural(n int) string {
	if n == 1 {
		return ""
	}
	return "s"
}
