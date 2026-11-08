package httpapi

import (
	"context"
	"fmt"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/solvinx/opsflow/internal/db"
	"github.com/solvinx/opsflow/internal/policy"
	"github.com/solvinx/opsflow/internal/testutil"
)

// Teams own projects: each project type routes to one team, and a Team Leader sees and
// assigns only their team's projects (plus projects with no team).
func TestTeamsRouteProjects(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	e.addUser("tl2", policy.TL, false)
	e.addUser("member2", policy.Member, false)
	profile := testutil.Must(e.store.CreateProfile(ctx, db.CreateProfileParams{Name: "Studio Main", Platform: "Fiverr"}))
	hr, admin, sales, ceo := e.login("hr"), e.login("admin"), e.login("sales"), e.login("ceo")
	tl, tl2 := e.login("tl"), e.login("tl2")
	future := time.Now().AddDate(0, 1, 0).Format(time.DateOnly)

	// ── Teams (HR + CEO) ────────────────────────────────
	expect(t, e.do("POST", "/api/admin/teams", tl, map[string]any{"name": "Shopify"}), 403, "TL can't create teams")
	r := e.do("POST", "/api/admin/teams", hr, map[string]any{"name": "Shopify", "lead_id": e.users["sales"].ID})
	if r.status != 422 || fieldErr(r, "lead_id") == "" {
		t.Fatalf("lead must be a TL: %d %v", r.status, r.body)
	}
	r = e.do("POST", "/api/admin/teams", hr, map[string]any{"name": "Shopify", "lead_id": e.users["tl"].ID})
	expect(t, r, 201, "create Shopify team")
	shopify := r.body["team"].(map[string]any)["id"].(string)
	r = e.do("POST", "/api/admin/teams", ceo, map[string]any{"name": "Web Dev", "lead_id": e.users["tl2"].ID})
	expect(t, r, 201, "create Web team")
	web := r.body["team"].(map[string]any)["id"].(string)
	expect(t, e.do("POST", "/api/admin/teams", hr, map[string]any{"name": "shopify"}), 422, "duplicate name")

	expect(t, e.do("PUT", "/api/admin/teams/"+shopify+"/members/"+e.users["sales"].ID.String(), hr, nil), 422, "only members join")
	expect(t, e.do("PUT", "/api/admin/teams/"+shopify+"/members/"+e.users["member"].ID.String(), hr, nil), 204, "add member")
	expect(t, e.do("PUT", "/api/admin/teams/"+shopify+"/members/"+e.users["member2"].ID.String(), hr, nil), 204, "add member2")
	expect(t, e.do("PUT", "/api/admin/teams/"+web+"/members/"+e.users["member2"].ID.String(), hr, nil), 204, "move member2 to Web")
	r = e.do("GET", "/api/admin/teams/"+shopify, hr, nil)
	expect(t, r, 200, "team detail")
	if ms := r.body["members"].([]any); len(ms) != 1 {
		t.Fatalf("Shopify members = %v (member2 should have moved)", ms)
	}

	// ── Project types route to teams (system admin) ─────
	typeID := func(name string, team any) string {
		body := map[string]any{"name": name}
		if team != nil {
			body["team_id"] = team
		}
		r := e.do("POST", "/api/admin/project-types", admin, body)
		expect(t, r, 201, "create type "+name)
		return r.body["project_type"].(map[string]any)["id"].(string)
	}
	shopifyType, webType, designType := typeID("Shopify Store", shopify), typeID("Website", web), typeID("Design", nil)

	n := 0
	submit := func(typ string) string {
		n++
		r := e.do("POST", "/api/projects", sales, map[string]any{
			"name": fmt.Sprintf("Project %d", n), "profile_id": profile.ID, "client_name": "Acme", "order_id": fmt.Sprintf("FO-%d", n),
			"project_type_id": typ, "scope": "Build it", "agreed_price": "100", "deadline": future,
		})
		expect(t, r, 201, "create")
		id := projectOf(r)["id"].(string)
		return id
	}
	teamOf := func(id string) any {
		return projectOf(e.do("GET", "/api/projects/"+id, ceo, nil))["team_name"]
	}
	listed := func(cookie, id string) bool {
		for _, p := range e.do("GET", "/api/projects", cookie, nil).body["projects"].([]any) {
			if p.(map[string]any)["id"] == id {
				return true
			}
		}
		return false
	}

	sp := submit(shopifyType)
	t.Run("a Shopify project goes to the Shopify team only", func(t *testing.T) {
		if got := teamOf(sp); got != "Shopify" {
			t.Fatalf("team = %v", got)
		}
		expect(t, e.do("GET", "/api/projects/"+sp, tl, nil), 200, "Shopify lead sees it")
		expect(t, e.do("GET", "/api/projects/"+sp, tl2, nil), 404, "Web lead doesn't")
		if !listed(tl, sp) || listed(tl2, sp) {
			t.Fatal("list scoping by team is wrong")
		}
		expect(t, e.do("POST", "/api/projects/"+sp+"/transition", tl2, map[string]any{"to": "cancelled", "comment": "x"}), 404, "Web lead can't act")
		titles := func(cookie string) string {
			var out []string
			for _, n := range e.do("GET", "/api/notifications", cookie, nil).body["notifications"].([]any) {
				out = append(out, fmt.Sprint(n.(map[string]any)["title"]))
			}
			return strings.Join(out, "|")
		}
		if !strings.Contains(titles(tl), "Project 1") || strings.Contains(titles(tl2), "Project 1") {
			t.Fatalf("submit notification: tl=%q tl2=%q", titles(tl), titles(tl2))
		}
	})

	t.Run("the lead assigns only their own team's members", func(t *testing.T) {
		r := e.do("POST", "/api/projects/"+sp+"/assign", tl, map[string]any{"assignee_id": e.users["member2"].ID, "priority": "high"})
		if r.status != 422 || fieldErr(r, "assignee_id") == "" {
			t.Fatalf("assign outside team: %d %v", r.status, r.body)
		}
		expect(t, e.do("POST", "/api/projects/"+sp+"/assign", tl, map[string]any{"assignee_id": e.users["member"].ID, "priority": "high"}), 200, "assign in team")
	})

	t.Run("a type without a team is open to every lead", func(t *testing.T) {
		dp := submit(designType)
		if got := teamOf(dp); got != nil {
			t.Fatalf("team = %v", got)
		}
		expect(t, e.do("GET", "/api/projects/"+dp, tl, nil), 200, "tl")
		expect(t, e.do("GET", "/api/projects/"+dp, tl2, nil), 200, "tl2")
	})

	t.Run("changing the type re-routes a waiting project", func(t *testing.T) {
		wp := submit(webType)
		if got := teamOf(wp); got != "Web Dev" {
			t.Fatalf("team = %v", got)
		}
		expect(t, e.do("PATCH", "/api/projects/"+wp, sales, map[string]any{"project_type_id": shopifyType}), 200, "change type")
		if got := teamOf(wp); got != "Shopify" {
			t.Fatalf("after type change team = %v", got)
		}
	})

	t.Run("remapping a type moves its waiting projects", func(t *testing.T) {
		wp := submit(webType)
		expect(t, e.do("PATCH", "/api/admin/project-types/"+webType, admin, map[string]any{"team_id": shopify}), 200, "remap")
		if got := teamOf(wp); got != "Shopify" {
			t.Fatalf("after remap team = %v", got)
		}
		expect(t, e.do("PATCH", "/api/admin/project-types/"+webType, admin, map[string]any{"team_id": web}), 200, "remap back")
		expect(t, e.do("PATCH", "/api/admin/project-types/"+webType, hr, map[string]any{"team_id": web}), 403, "HR doesn't do system setup")
	})

	t.Run("a team without an active lead is open to every lead", func(t *testing.T) {
		expect(t, e.do("PATCH", "/api/admin/teams/"+shopify, hr, map[string]any{"lead_id": nil}), 200, "clear lead")
		expect(t, e.do("GET", "/api/projects/"+sp, tl2, nil), 200, "tl2 covers")
		expect(t, e.do("PATCH", "/api/admin/teams/"+shopify, hr, map[string]any{"lead_id": e.users["tl"].ID}), 200, "restore lead")
		expect(t, e.do("GET", "/api/projects/"+sp, tl2, nil), 404, "back to Shopify only")
	})

	t.Run("workload shows a lead only their own team", func(t *testing.T) {
		r := e.do("GET", "/api/reports/workload", tl, nil)
		expect(t, r, 200, "Shopify lead's workload")
		if r.body["team"] != "Shopify" {
			t.Fatalf("team: %v", r.body["team"])
		}
		names := []string{}
		for _, m := range r.body["members"].([]any) {
			names = append(names, m.(map[string]any)["name"].(string))
		}
		// member is in Shopify, member2 moved to Web Dev; the lead is listed too, since leads take projects on.
		slices.Sort(names)
		if len(names) != 2 || names[0] != "member" || names[1] != "tl" {
			t.Fatalf("Shopify workload: %v", names)
		}
		if teams := r.body["teams"].([]any); len(teams) != 1 || teams[0].(map[string]any)["name"] != "Shopify" {
			t.Fatalf("teams: %v", r.body["teams"])
		}
		// The Web Dev lead sees their own member, never Shopify's.
		w := e.do("GET", "/api/reports/workload", tl2, nil)
		expect(t, w, 200, "Web lead's workload")
		for _, m := range w.body["members"].([]any) {
			if m.(map[string]any)["name"] == "member" {
				t.Fatal("Web lead sees a Shopify member's workload")
			}
		}
		// The CEO sees every team.
		if teams := e.do("GET", "/api/reports/workload", ceo, nil).body["teams"].([]any); len(teams) < 2 {
			t.Fatalf("CEO teams: %v", teams)
		}
	})

	t.Run("activity is scoped to the lead's projects", func(t *testing.T) {
		for _, a := range e.do("GET", "/api/activity", tl2, nil).body["activity"].([]any) {
			if a.(map[string]any)["project_id"] == sp {
				t.Fatal("Web lead sees Shopify project activity")
			}
		}
	})

	t.Run("a team belongs to a department", func(t *testing.T) {
		r := e.do("POST", "/api/admin/departments", hr, map[string]any{"name": "Operations"})
		expect(t, r, 201, "create department")
		ops := r.body["department"].(map[string]any)["id"].(string)
		expect(t, e.do("PATCH", "/api/admin/teams/"+shopify, hr, map[string]any{"department_id": ops}), 200, "set department")
		var got any
		for _, tm := range e.do("GET", "/api/teams", tl, nil).body["teams"].([]any) {
			if tm.(map[string]any)["id"] == shopify {
				got = tm.(map[string]any)["department_name"]
			}
		}
		if got != "Operations" {
			t.Fatalf("department = %v", got)
		}
		r = e.do("PATCH", "/api/admin/teams/"+shopify, hr, map[string]any{"department_id": "00000000-0000-0000-0000-000000000001"})
		if r.status != 422 || fieldErr(r, "department_id") == "" {
			t.Fatalf("unknown department: %d %v", r.status, r.body)
		}
	})

	t.Run("work is handed over before access is revoked", func(t *testing.T) {
		user := func(label string) string { return "/api/admin/users/" + e.users[label].ID.String() }
		r := e.do("PATCH", user("member2"), hr, map[string]any{"is_active": false})
		if r.status != 422 || fieldErr(r, "reason") == "" {
			t.Fatalf("deactivate without a reason: %d %v", r.status, r.body)
		}
		r = e.do("PATCH", user("tl"), hr, map[string]any{"is_active": false, "reason": "Left"})
		if r.status != 409 || !strings.Contains(fmt.Sprint(r.body), "team") {
			t.Fatalf("deactivate a team lead: %d %v", r.status, r.body)
		}
		r = e.do("PATCH", user("member"), hr, map[string]any{"is_active": false, "reason": "Left"})
		if r.status != 409 || !strings.Contains(fmt.Sprint(r.body), "open project") {
			t.Fatalf("deactivate a member with open work: %d %v", r.status, r.body)
		}
		expect(t, e.do("PATCH", user("tl"), hr, map[string]any{"role": "member"}), 409, "demote a team lead")
		expect(t, e.do("PATCH", user("member2"), hr, map[string]any{"is_active": false, "reason": "Contract ended"}), 200, "nothing to hand over")
		found := false
		for _, a := range e.do("GET", "/api/activity?type=user", ceo, nil).body["activity"].([]any) {
			m := a.(map[string]any)
			if m["action"] == "user.access_revoked" && strings.Contains(fmt.Sprint(m["metadata"]), "Contract ended") {
				found = true
			}
		}
		if !found {
			t.Fatal("revoke with its reason is not in the access log")
		}
		expect(t, e.do("PATCH", user("member2"), hr, map[string]any{"is_active": true}), 200, "restore")
	})

	expect(t, e.do("PATCH", "/api/admin/teams/"+shopify, hr, map[string]any{"is_active": false}), 409, "can't retire a team with open projects")
}

// A department head runs every team in their department like its lead: they see,
// assign and get notified about those teams' projects, and their workload covers
// them all. Other leads keep their own scope.
func TestDepartmentHeadRunsTeams(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	e.addUser("tl2", policy.TL, false)
	e.addUser("head", policy.TL, false)
	e.addUser("member2", policy.Member, false)
	profile := testutil.Must(e.store.CreateProfile(ctx, db.CreateProfileParams{Name: "Studio Main", Platform: "Fiverr"}))
	hr, admin, sales, ceo := e.login("hr"), e.login("admin"), e.login("sales"), e.login("ceo")
	tl2, head := e.login("tl2"), e.login("head")
	future := time.Now().AddDate(0, 1, 0).Format(time.DateOnly)
	id := func(r resp, key string) string { return r.body[key].(map[string]any)["id"].(string) }

	r := e.do("POST", "/api/admin/departments", hr, map[string]any{"name": "CMS"})
	expect(t, r, 201, "create CMS")
	cms := id(r, "department")
	r = e.do("POST", "/api/admin/departments", hr, map[string]any{"name": "META"})
	expect(t, r, 201, "create META")
	meta := id(r, "department")
	r = e.do("POST", "/api/admin/teams", hr, map[string]any{"name": "Shopify", "lead_id": e.users["tl"].ID, "department_id": cms})
	expect(t, r, 201, "Shopify in CMS")
	shopify := id(r, "team")
	r = e.do("POST", "/api/admin/teams", hr, map[string]any{"name": "Ads", "lead_id": e.users["tl2"].ID, "department_id": meta})
	expect(t, r, 201, "Ads in META")
	ads := id(r, "team")
	expect(t, e.do("PUT", "/api/admin/teams/"+shopify+"/members/"+e.users["member"].ID.String(), hr, nil), 204, "member to Shopify")
	expect(t, e.do("PUT", "/api/admin/teams/"+ads+"/members/"+e.users["member2"].ID.String(), hr, nil), 204, "member2 to Ads")

	r = e.do("POST", "/api/admin/project-types", admin, map[string]any{"name": "Shopify Store", "team_id": shopify})
	expect(t, r, 201, "Shopify type")
	shopifyType := id(r, "project_type")
	r = e.do("POST", "/api/admin/project-types", admin, map[string]any{"name": "Ads", "team_id": ads})
	expect(t, r, 201, "Ads type")
	adsType := id(r, "project_type")

	n := 0
	submit := func(typ string) string {
		n++
		r := e.do("POST", "/api/projects", sales, map[string]any{
			"name": fmt.Sprintf("Project %d", n), "profile_id": profile.ID, "client_name": "Acme", "order_id": fmt.Sprintf("FO-%d", n),
			"project_type_id": typ, "scope": "Build it", "agreed_price": "100", "deadline": future,
		})
		expect(t, r, 201, "create")
		pid := projectOf(r)["id"].(string)
		return pid
	}
	listed := func(cookie, pid string) bool {
		for _, p := range e.do("GET", "/api/projects", cookie, nil).body["projects"].([]any) {
			if p.(map[string]any)["id"] == pid {
				return true
			}
		}
		return false
	}

	t.Run("only an active Team Leader can head a department", func(t *testing.T) {
		r := e.do("PATCH", "/api/admin/departments/"+cms, hr, map[string]any{"head_id": e.users["sales"].ID})
		if r.status != 422 || fieldErr(r, "head_id") == "" {
			t.Fatalf("sales as head: %d %v", r.status, r.body)
		}
		expect(t, e.do("PATCH", "/api/admin/departments/"+cms, head, map[string]any{"head_id": e.users["head"].ID}), 403, "a TL can't appoint")
		expect(t, e.do("PATCH", "/api/admin/departments/"+cms, ceo, map[string]any{"head_id": e.users["head"].ID}), 200, "CEO appoints")
		r = e.do("GET", "/api/hr/departments", hr, nil)
		expect(t, r, 200, "departments")
		for _, d := range r.body["departments"].([]any) {
			if m := d.(map[string]any); m["id"] == cms && m["head_id"] != e.users["head"].ID.String() {
				t.Fatalf("head not listed: %v", m)
			}
		}
	})

	sp := submit(shopifyType)
	ap := submit(adsType)
	t.Run("the head sees and acts on their department's teams only", func(t *testing.T) {
		expect(t, e.do("GET", "/api/projects/"+sp, head, nil), 200, "head sees Shopify")
		expect(t, e.do("GET", "/api/projects/"+ap, head, nil), 404, "head doesn't see META's Ads")
		expect(t, e.do("GET", "/api/projects/"+sp, tl2, nil), 404, "Ads lead still doesn't see Shopify")
		if !listed(head, sp) || listed(head, ap) {
			t.Fatal("head's list scoping is wrong")
		}
		r := e.do("POST", "/api/projects/"+sp+"/assign", head, map[string]any{"assignee_id": e.users["member2"].ID, "priority": "high"})
		if r.status != 422 || fieldErr(r, "assignee_id") == "" {
			t.Fatalf("head assigns outside the project's team: %d %v", r.status, r.body)
		}
		expect(t, e.do("POST", "/api/projects/"+sp+"/assign", head, map[string]any{"assignee_id": e.users["member"].ID, "priority": "high"}), 200, "head assigns in team")
	})

	t.Run("the head is notified about their teams' projects", func(t *testing.T) {
		var titles []string
		for _, x := range e.do("GET", "/api/notifications", head, nil).body["notifications"].([]any) {
			titles = append(titles, fmt.Sprint(x.(map[string]any)["title"]))
		}
		all := strings.Join(titles, "|")
		if !strings.Contains(all, "Project 1") || strings.Contains(all, "Project 2") {
			t.Fatalf("head notifications: %q", all)
		}
	})

	t.Run("the head's workload covers every team they run", func(t *testing.T) {
		expect(t, e.do("PATCH", "/api/admin/teams/"+ads, hr, map[string]any{"department_id": cms}), 200, "Ads joins CMS")
		r := e.do("GET", "/api/reports/workload", head, nil)
		expect(t, r, 200, "head workload")
		if r.body["team"] != "Ads, Shopify" {
			t.Fatalf("team label: %v", r.body["team"])
		}
		// Two members plus the two teams' leads, who take projects on too.
		if len(r.body["teams"].([]any)) != 2 || len(r.body["members"].([]any)) != 4 {
			t.Fatalf("teams %v members %v", r.body["teams"], r.body["members"])
		}
		expect(t, e.do("GET", "/api/projects/"+ap, head, nil), 200, "head now sees Ads too")
	})

	t.Run("a head hands over before losing the role", func(t *testing.T) {
		user := "/api/admin/users/" + e.users["head"].ID.String()
		r := e.do("PATCH", user, hr, map[string]any{"role": "member"})
		if r.status != 409 || !strings.Contains(fmt.Sprint(r.body), "department") {
			t.Fatalf("demote a head: %d %v", r.status, r.body)
		}
		expect(t, e.do("PATCH", "/api/admin/departments/"+cms, hr, map[string]any{"head_id": nil}), 200, "clear head")
		expect(t, e.do("GET", "/api/projects/"+sp, head, nil), 404, "no longer sees Shopify")
	})
}

// A Team Leader can take a project on themselves (or be given it by their department's
// head) and then works it like a member: starts it, writes updates, which are approved
// on submit because the leader is the approver, and hands it in.
func TestLeaderTakesProject(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	e.addUser("tl2", policy.TL, false)
	e.addUser("head", policy.TL, false)
	profile := testutil.Must(e.store.CreateProfile(ctx, db.CreateProfileParams{Name: "Studio Main", Platform: "Fiverr"}))
	hr, admin, sales := e.login("hr"), e.login("admin"), e.login("sales")
	tl, tl2, head := e.login("tl"), e.login("tl2"), e.login("head")
	future := time.Now().AddDate(0, 1, 0).Format(time.DateOnly)
	id := func(r resp, key string) string { return r.body[key].(map[string]any)["id"].(string) }

	r := e.do("POST", "/api/admin/departments", hr, map[string]any{"name": "CMS"})
	expect(t, r, 201, "CMS")
	cms := id(r, "department")
	expect(t, e.do("PATCH", "/api/admin/departments/"+cms, hr, map[string]any{"head_id": e.users["head"].ID}), 200, "head")
	r = e.do("POST", "/api/admin/teams", hr, map[string]any{"name": "Shopify", "lead_id": e.users["tl"].ID, "department_id": cms})
	expect(t, r, 201, "Shopify")
	r = e.do("POST", "/api/admin/project-types", admin, map[string]any{"name": "Shopify Store", "team_id": id(r, "team")})
	expect(t, r, 201, "type")
	typ := id(r, "project_type")
	n := 0
	submit := func() string {
		n++
		r := e.do("POST", "/api/projects", sales, map[string]any{
			"name": fmt.Sprintf("Store %d", n), "profile_id": profile.ID, "client_name": "Acme", "order_id": fmt.Sprintf("FO-%d", n),
			"project_type_id": typ, "scope": "Build it", "agreed_price": "100", "deadline": future,
		})
		expect(t, r, 201, "create")
		pid := projectOf(r)["id"].(string)
		return pid
	}

	p := submit()
	path := "/api/projects/" + p
	t.Run("only a leader who runs the team can be assigned", func(t *testing.T) {
		r := e.do("POST", path+"/assign", tl, map[string]any{"assignee_ids": []any{e.users["tl2"].ID}, "priority": "high"})
		if r.status != 422 || fieldErr(r, "assignee_ids") == "" {
			t.Fatalf("another team's lead: %d %v", r.status, r.body)
		}
		r = e.do("POST", path+"/assign", tl, map[string]any{"assignee_ids": []any{e.users["sales"].ID}, "priority": "high"})
		expect(t, r, 422, "sales can't be assigned")
	})

	t.Run("the lead takes it and works it through", func(t *testing.T) {
		expect(t, e.do("POST", path+"/assign", tl, map[string]any{"assignee_ids": []any{e.users["tl"].ID}, "priority": "high"}), 200, "self-assign")
		if got := projectOf(e.do("GET", path, tl, nil))["status"]; got != "in_progress" {
			t.Fatalf("assigning starts the project, got %v", got)
		}
		r := e.do("POST", path+"/updates", tl, map[string]any{"message": "Theme installed", "destination": "order_page"})
		expect(t, r, 201, "lead writes an update")
		uid := updateOf(r)["id"].(string)
		r = e.do("POST", "/api/updates/"+uid+"/submit", tl, nil)
		expect(t, r, 200, "submit")
		found := false
		for _, x := range e.do("GET", "/api/updates?project_id="+p, tl, nil).body["updates"].([]any) {
			found = found || x.(map[string]any)["id"] == uid
		}
		if !found {
			t.Fatal("lead's own update missing from the list")
		}
		if got := updateOf(e.do("GET", "/api/updates/"+uid, tl, nil))["status"]; got != "approved" {
			t.Fatalf("lead's own update should be approved on submit, got %v", got)
		}
		// A leader's own work skips the check and goes straight to Sales.
		expect(t, e.do("POST", path+"/transition", tl, map[string]any{"to": "ready_for_review"}), 403, "no self-check step")
		expect(t, e.do("POST", path+"/transition", tl, map[string]any{"to": "ready_to_deliver"}), 200, "ready to deliver")
		expect(t, e.do("POST", path+"/transition", tl, map[string]any{"to": "delivered"}), 403, "Sales deliver")
		expect(t, e.do("POST", path+"/transition", sales, map[string]any{"to": "delivered"}), 200, "delivered")
		expect(t, e.do("POST", path+"/transition", tl, map[string]any{"to": "completed"}), 200, "complete")
		expect(t, e.do("POST", path+"/transition", tl2, map[string]any{"to": "in_progress", "comment": "x"}), 404, "other lead still can't see it")
	})

	t.Run("the department head can hand it to the team's lead", func(t *testing.T) {
		q := submit()
		expect(t, e.do("POST", "/api/projects/"+q+"/assign", head, map[string]any{"assignee_ids": []any{e.users["tl"].ID}, "priority": "medium"}), 200, "head assigns the lead")
		if m := projectOf(e.do("GET", "/api/projects/"+q, tl, nil))["member_ids"].([]any); len(m) != 1 || m[0] != e.users["tl"].ID.String() {
			t.Fatalf("members: %v", m)
		}
	})

	t.Run("the lead's own projects count in the workload", func(t *testing.T) {
		r := e.do("GET", "/api/reports/workload", head, nil)
		expect(t, r, 200, "workload")
		for _, m := range r.body["members"].([]any) {
			if m := m.(map[string]any); m["name"] == "tl" {
				if m["open_projects"].(float64) != 1 {
					t.Fatalf("lead open projects: %v", m)
				}
				return
			}
		}
		t.Fatalf("lead missing from workload: %v", r.body["members"])
	})
}

// A Team Leader hands a project to another team, even after it has started: its members
// come off it, it goes back to New in the new team's queue, and the history keeps the move.
func TestMoveProjectTeam(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	e.addUser("tl2", policy.TL, false)
	e.addUser("member2", policy.Member, false)
	profile := testutil.Must(e.store.CreateProfile(ctx, db.CreateProfileParams{Name: "Studio Main", Platform: "Fiverr"}))
	hr, admin, sales, ceo := e.login("hr"), e.login("admin"), e.login("sales"), e.login("ceo")
	tl, tl2, member := e.login("tl"), e.login("tl2"), e.login("member")
	future := time.Now().AddDate(0, 1, 0).Format(time.DateOnly)

	team := func(name, lead, mem string) string {
		r := e.do("POST", "/api/admin/teams", hr, map[string]any{"name": name, "lead_id": e.users[lead].ID})
		expect(t, r, 201, "create "+name)
		id := r.body["team"].(map[string]any)["id"].(string)
		expect(t, e.do("PUT", "/api/admin/teams/"+id+"/members/"+e.users[mem].ID.String(), hr, nil), 204, "add "+mem)
		return id
	}
	design, web := team("Design", "tl", "member"), team("Web Dev", "tl2", "member2")
	r := e.do("POST", "/api/admin/project-types", admin, map[string]any{"name": "Logo", "team_id": design})
	expect(t, r, 201, "type")
	designType := r.body["project_type"].(map[string]any)["id"].(string)
	r = e.do("POST", "/api/admin/project-types", admin, map[string]any{"name": "Landing page", "team_id": web})
	expect(t, r, 201, "type")
	webType := r.body["project_type"].(map[string]any)["id"].(string)

	n := 0
	started := func() string {
		n++
		r := e.do("POST", "/api/projects", sales, map[string]any{
			"name": fmt.Sprintf("Move %d", n), "profile_id": profile.ID, "client_name": "Acme", "order_id": fmt.Sprintf("MV-%d", n),
			"project_type_id": designType, "scope": "Logo then site", "agreed_price": "100", "deadline": future,
		})
		expect(t, r, 201, "create")
		id := projectOf(r)["id"].(string)
		expect(t, e.do("POST", "/api/projects/"+id+"/assign", tl, map[string]any{"assignee_id": e.users["member"].ID, "priority": "medium"}), 200, "assign")
		return id
	}
	move := func(cookie, id, to, comment string) resp {
		return e.do("POST", "/api/projects/"+id+"/team", cookie, map[string]any{"team_id": to, "comment": comment})
	}
	get := func(id string) map[string]any { return projectOf(e.do("GET", "/api/projects/"+id, ceo, nil)) }

	p := started()
	t.Run("only a Team Leader moves it, with a reason, to another active team", func(t *testing.T) {
		expect(t, move(member, p, web, "x"), 403, "member")
		expect(t, move(sales, p, web, "x"), 403, "sales")
		expect(t, move(ceo, p, web, "x"), 403, "CEO is read-only")
		expect(t, move(tl2, p, web, "x"), 404, "a lead who can't see it")
		if r := move(tl, p, web, " "); r.status != 422 || fieldErr(r, "comment") == "" {
			t.Fatalf("no reason: %d %v", r.status, r.body)
		}
		if r := move(tl, p, design, "x"); r.status != 422 || fieldErr(r, "team_id") == "" {
			t.Fatalf("same team: %d %v", r.status, r.body)
		}
		if r := move(tl, p, "00000000-0000-0000-0000-000000000001", "x"); r.status != 422 || fieldErr(r, "team_id") == "" {
			t.Fatalf("unknown team: %d %v", r.status, r.body)
		}
	})

	t.Run("an In Progress project moves and waits for the new team", func(t *testing.T) {
		r := move(tl, p, web, "Logo done, the site is Web Dev's")
		expect(t, r, 200, "move")
		if r.body["visible"] != false {
			t.Fatalf("the Design lead should lose sight of it: %v", r.body)
		}
		got := get(p)
		if got["team_name"] != "Web Dev" || got["status"] != "new" || got["assigned_to"] != nil {
			t.Fatalf("after move: team=%v status=%v assigned=%v", got["team_name"], got["status"], got["assigned_to"])
		}
		expect(t, e.do("GET", "/api/projects/"+p, tl, nil), 404, "old lead")
		expect(t, e.do("GET", "/api/projects/"+p, member, nil), 404, "old member")
		expect(t, e.do("GET", "/api/projects/"+p, tl2, nil), 200, "new lead")
		titles := func(cookie string) string {
			var out []string
			for _, n := range e.do("GET", "/api/notifications", cookie, nil).body["notifications"].([]any) {
				out = append(out, fmt.Sprint(n.(map[string]any)["title"]))
			}
			return strings.Join(out, "|")
		}
		if !strings.Contains(titles(tl2), "moved to Web Dev") || !strings.Contains(titles(member), "taken off") ||
			!strings.Contains(titles(sales), "moved to Web Dev") {
			t.Fatalf("notifications: tl2=%q member=%q sales=%q", titles(tl2), titles(member), titles(sales))
		}
		hist := fmt.Sprint(e.do("GET", "/api/projects/"+p, ceo, nil).body)
		if !strings.Contains(hist, "Moved from Design to Web Dev: Logo done") {
			t.Fatal("the move is not in the timeline")
		}
		// The new lead assigns their own member, which starts it again.
		r = e.do("POST", "/api/projects/"+p+"/assign", tl2, map[string]any{"assignee_id": e.users["member2"].ID, "priority": "high"})
		expect(t, r, 200, "new lead assigns")
		if projectOf(r)["status"] != "in_progress" {
			t.Fatalf("status after reassign: %v", projectOf(r)["status"])
		}
	})

	t.Run("a type change after a move doesn't send it back", func(t *testing.T) {
		q := started()
		expect(t, move(tl, q, web, "Wrong team"), 200, "move")
		r := e.do("POST", "/api/admin/project-types", admin, map[string]any{"name": "Icon set", "team_id": design})
		expect(t, r, 201, "type")
		iconType := r.body["project_type"].(map[string]any)["id"].(string)
		expect(t, e.do("PATCH", "/api/projects/"+q, sales, map[string]any{"project_type_id": iconType}), 200, "type edit")
		if got := get(q)["team_name"]; got != "Web Dev" {
			t.Fatalf("a hand-moved project was re-routed by its type: %v", got)
		}
		// Once its type is one that routes to where it is, type edits route it again.
		expect(t, e.do("PATCH", "/api/projects/"+q, sales, map[string]any{"project_type_id": webType}), 200, "type edit")
		expect(t, e.do("PATCH", "/api/projects/"+q, sales, map[string]any{"project_type_id": designType}), 200, "type edit")
		if got := get(q)["team_name"]; got != "Design" {
			t.Fatalf("team = %v", got)
		}
	})

	t.Run("remapping the type doesn't send a moved project back", func(t *testing.T) {
		r := e.do("POST", "/api/admin/teams", hr, map[string]any{"name": "Print", "lead_id": e.users["tl"].ID})
		expect(t, r, 201, "Print")
		printTeam := r.body["team"].(map[string]any)["id"].(string)
		r = e.do("POST", "/api/admin/project-types", admin, map[string]any{"name": "Flyer", "team_id": design})
		expect(t, r, 201, "type")
		flyer := r.body["project_type"].(map[string]any)["id"].(string)
		mk := func(name string) string {
			r := e.do("POST", "/api/projects", sales, map[string]any{
				"name": name, "profile_id": profile.ID, "client_name": "Acme", "order_id": "FL-" + name,
				"project_type_id": flyer, "scope": "Flyer", "agreed_price": "50", "deadline": future,
			})
			expect(t, r, 201, "create "+name)
			return projectOf(r)["id"].(string)
		}
		moved, waiting := mk("moved"), mk("waiting")
		expect(t, move(tl, moved, web, "Web Dev does flyers now"), 200, "move")
		expect(t, e.do("PATCH", "/api/admin/project-types/"+flyer, admin, map[string]any{"team_id": printTeam}), 200, "remap")
		if got := get(moved)["team_name"]; got != "Web Dev" {
			t.Fatalf("moved project re-routed by a remap: %v", got)
		}
		if got := get(waiting)["team_name"]; got != "Print" {
			t.Fatalf("waiting project didn't follow its type: %v", got)
		}
	})

	t.Run("a blocked project moves; review and later stay put", func(t *testing.T) {
		b := started()
		expect(t, e.do("POST", "/api/projects/"+b+"/transition", member, map[string]any{"to": "blocked", "comment": "Needs dev"}), 200, "block")
		expect(t, move(tl, b, web, "Dev work"), 200, "move blocked")
		if got := get(b)["status"]; got != "new" {
			t.Fatalf("blocked moved: %v", got)
		}
		rv := started()
		expect(t, e.do("POST", "/api/projects/"+rv+"/transition", member, map[string]any{"to": "ready_for_review"}), 200, "send for check")
		if r := move(tl, rv, web, "x"); r.status != 409 || errCode(r) != "invalid_transition" {
			t.Fatalf("in review: %d %v", r.status, r.body)
		}
		c := started()
		expect(t, e.do("POST", "/api/projects/"+c+"/transition", tl, map[string]any{"to": "cancelled", "comment": "x"}), 200, "cancel")
		expect(t, move(tl, c, web, "x"), 409, "cancelled")
	})

	t.Run("a lead who runs both teams keeps working it", func(t *testing.T) {
		// tl takes the project on themselves, then hands it to a team they also lead.
		r := e.do("POST", "/api/admin/teams", hr, map[string]any{"name": "Brand", "lead_id": e.users["tl"].ID})
		expect(t, r, 201, "Brand")
		brand := r.body["team"].(map[string]any)["id"].(string)
		k := started()
		expect(t, e.do("POST", "/api/projects/"+k+"/assign", tl, map[string]any{"assignee_ids": []any{e.users["tl"].ID}, "priority": "medium"}), 200, "lead takes it")
		r = move(tl, k, brand, "Brand work")
		expect(t, r, 200, "move")
		if got := projectOf(r); got["status"] != "in_progress" || got["team_name"] != "Brand" {
			t.Fatalf("kept lead: status=%v team=%v", got["status"], got["team_name"])
		}
	})
}
