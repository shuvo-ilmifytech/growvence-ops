package httpapi

import (
	"context"
	"testing"

	"github.com/google/uuid"

	"github.com/solvinx/opsflow/internal/db"
	"github.com/solvinx/opsflow/internal/policy"
	"github.com/solvinx/opsflow/internal/testutil"
)

func requestOf(r resp) map[string]any {
	if m, ok := r.body["request"].(map[string]any); ok {
		return m
	}
	return nil
}

// Client requests: work for a client with no project in the portal. Sales raise one and
// send it to a team; the team works its cards and writes its client updates, which run the
// same approval as a project's.
func TestClientRequests(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	e.addUser("member2", policy.Member, false)
	e.addUser("tl2", policy.TL, false)
	profile := testutil.Must(e.store.CreateProfile(ctx, db.CreateProfileParams{Name: "Studio Main", Platform: "Fiverr"}))
	off := testutil.Must(e.store.CreateProfile(ctx, db.CreateProfileParams{Name: "Old Studio", Platform: "Fiverr"}))
	if _, err := e.store.UpdateProfile(ctx, db.UpdateProfileParams{ID: off.ID, IsActive: ptr(false)}); err != nil {
		t.Fatal(err)
	}
	tlID, tl2ID := e.users["tl"].ID, e.users["tl2"].ID
	shopify := testutil.Must(e.store.CreateTeam(ctx, db.CreateTeamParams{Name: "Shopify", LeadID: &tlID}))
	web := testutil.Must(e.store.CreateTeam(ctx, db.CreateTeamParams{Name: "Web Dev", LeadID: &tl2ID}))
	if err := e.store.SetTeamMember(ctx, db.SetTeamMemberParams{UserID: e.users["member"].ID, TeamID: shopify.ID}); err != nil {
		t.Fatal(err)
	}
	if err := e.store.SetTeamMember(ctx, db.SetTeamMemberParams{UserID: e.users["member2"].ID, TeamID: web.ID}); err != nil {
		t.Fatal(err)
	}
	sales, tl, tl2, ceo, hr := e.login("sales"), e.login("tl"), e.login("tl2"), e.login("ceo"), e.login("hr")
	m1, m2 := e.login("member"), e.login("member2")
	id := func(label string) uuid.UUID { return e.users[label].ID }

	body := func(over map[string]any) map[string]any {
		b := map[string]any{
			"profile_id": profile.ID, "client_name": "Nadia Khan", "order_id": "FO-9001",
			"title": "Add a size chart to the product pages", "details": "In a tab, like the care guide.",
			"team_id": shopify.ID,
		}
		for k, v := range over {
			b[k] = v
		}
		return b
	}

	t.Run("only Sales raise one, and it needs a profile, a client, a title and a team", func(t *testing.T) {
		expect(t, e.do("POST", "/api/client-requests", tl, body(nil)), 403, "the Team Leader can't raise one")
		expect(t, e.do("POST", "/api/client-requests", m1, body(nil)), 403, "a member can't")
		expect(t, e.do("POST", "/api/client-requests", ceo, body(nil)), 403, "the CEO never operates work")
		expect(t, e.do("POST", "/api/client-requests", hr, body(nil)), 403, "HR has no project access")
		expect(t, e.do("POST", "/api/client-requests", sales, body(map[string]any{"client_name": " "})), 422, "no client")
		expect(t, e.do("POST", "/api/client-requests", sales, body(map[string]any{"title": ""})), 422, "no title")
		expect(t, e.do("POST", "/api/client-requests", sales, body(map[string]any{"team_id": nil})), 422, "no team")
		expect(t, e.do("POST", "/api/client-requests", sales, body(map[string]any{"profile_id": nil})), 422, "no profile")
		expect(t, e.do("POST", "/api/client-requests", sales, body(map[string]any{"profile_id": off.ID})), 422, "a profile that is off")
		expect(t, e.do("POST", "/api/client-requests", sales, body(map[string]any{"team_id": uuid.New()})), 422, "a team that isn't there")
	})

	r := e.do("POST", "/api/client-requests", sales, body(nil))
	expect(t, r, 201, "raise")
	rid := requestOf(r)["id"].(string)
	path := "/api/client-requests/" + rid
	if q := requestOf(r); q["status"] != "open" || q["team_name"] != "Shopify" || q["client_name"] != "Nadia Khan" || q["order_id"] != "FO-9001" {
		t.Fatalf("raised: %v", q)
	}
	// The ask itself is on the team's board, so the work shows up where the team looks.
	cards := r.body["cards"].([]any)
	if len(cards) != 1 {
		t.Fatalf("want one card, got %d", len(cards))
	}
	card := cards[0].(map[string]any)
	cardID := card["id"].(string)
	if card["source"] != "client" || card["column_key"] != "todo" || card["project_id"] != nil {
		t.Fatalf("the request's card: %v", card)
	}

	t.Run("the order ID is optional", func(t *testing.T) {
		r := e.do("POST", "/api/client-requests", sales, body(map[string]any{"order_id": "", "client_name": "Old client"}))
		expect(t, r, 201, "no order ID")
		if q := requestOf(r); q["order_id"] != nil {
			t.Fatalf("order_id = %v", q["order_id"])
		}
	})

	// A team can have a lead and nobody under them yet. Its page still has to open: every
	// list in the answer is a list, never null (a null roster blanked the page once).
	t.Run("a team with nobody on it still opens", func(t *testing.T) {
		empty := testutil.Must(e.store.CreateTeam(ctx, db.CreateTeamParams{Name: "SEO", LeadID: &tl2ID}))
		r := e.do("POST", "/api/client-requests", sales, body(map[string]any{"team_id": empty.ID, "client_name": "Lonely team"}))
		expect(t, r, 201, "raise on a team with no members")
		for _, who := range []string{sales, tl2, ceo} {
			got := e.do("GET", "/api/client-requests/"+requestOf(r)["id"].(string), who, nil)
			expect(t, got, 200, "open it")
			for _, key := range []string{"roster", "cards", "activity", "columns"} {
				if _, ok := got.body[key].([]any); !ok {
					t.Fatalf("%s came back as %v, want a list", key, got.body[key])
				}
			}
		}
	})

	// It goes to a team, so the team sees it: Sales send it over and everyone there can
	// read what the client asked for, before the leader has handed the cards out.
	t.Run("who sees it", func(t *testing.T) {
		expect(t, e.do("GET", path, sales, nil), 200, "Sales see every request")
		expect(t, e.do("GET", path, tl, nil), 200, "the team's leader")
		expect(t, e.do("GET", path, ceo, nil), 200, "the CEO")
		expect(t, e.do("GET", path, m1, nil), 200, "anyone on the team it went to, unassigned")
		expect(t, e.do("GET", path, tl2, nil), 404, "another team's leader doesn't")
		expect(t, e.do("GET", path, m2, nil), 404, "nor another team's member")
		expect(t, e.do("GET", path, hr, nil), 404, "nor HR")
		if n := e.do("GET", "/api/client-requests", hr, nil).body["total"]; n != float64(0) {
			t.Fatalf("HR's list = %v", n)
		}
		// And it is in the team's list, not the other team's.
		var mine bool
		for _, q := range e.do("GET", "/api/client-requests", m1, nil).body["requests"].([]any) {
			mine = mine || q.(map[string]any)["id"] == rid
		}
		if !mine {
			t.Fatal("the team's member doesn't have it in their list")
		}
		for _, q := range e.do("GET", "/api/client-requests", m2, nil).body["requests"].([]any) {
			if q.(map[string]any)["id"] == rid {
				t.Fatal("another team's member has it in their list")
			}
		}
	})

	t.Run("it is not a project", func(t *testing.T) {
		if n := e.do("GET", "/api/projects", sales, nil).body["total"]; n != float64(0) {
			t.Fatalf("the project list has %v in it", n)
		}
	})

	t.Run("the team's board carries its card", func(t *testing.T) {
		b := e.do("GET", "/api/cards", tl, nil)
		expect(t, b, 200, "board")
		var found bool
		for _, c := range b.body["cards"].([]any) {
			m := c.(map[string]any)
			found = found || (m["client_request_id"] == rid && m["title"] == "Add a size chart to the product pages")
		}
		if !found {
			t.Fatalf("the request's card is not on the board: %v", b.body["cards"])
		}
		if len(b.body["requests"].([]any)) == 0 {
			t.Fatal("the board doesn't list the request as a container")
		}
		// The team's members see it on their board too, before anyone is assigned.
		var onMemberBoard bool
		for _, c := range e.do("GET", "/api/cards", m1, nil).body["cards"].([]any) {
			onMemberBoard = onMemberBoard || c.(map[string]any)["client_request_id"] == rid
		}
		if !onMemberBoard {
			t.Fatal("the team's member doesn't have the request's card on their board")
		}
		// Another team's leader and member see neither.
		for _, who := range []string{tl2, m2} {
			for _, c := range e.do("GET", "/api/cards", who, nil).body["cards"].([]any) {
				if c.(map[string]any)["client_request_id"] == rid {
					t.Fatal("another team sees the card")
				}
			}
		}
	})

	// The board is the team's: anyone on it drags the cards along. Handing the work out
	// (and everything else) is still the leader's or the card holder's.
	t.Run("anyone on the team moves the cards, and nothing more", func(t *testing.T) {
		mv := func(who, col string) resp {
			return e.do("POST", "/api/cards/"+cardID+"/move", who, map[string]any{"column": col, "index": 0})
		}
		expect(t, mv(m1, "in_progress"), 200, "a member holding no card moves it")
		if c := e.do("GET", path, m1, nil).body["cards"].([]any)[0].(map[string]any); c["column_key"] != "in_progress" {
			t.Fatalf("the card is in %v", c["column_key"])
		}
		expect(t, mv(m2, "todo"), 404, "another team's member can't")
		expect(t, mv(m1, "todo"), 200, "and back")
		d := e.do("GET", path, m1, nil)
		if d.body["can_move_card"] != true {
			t.Fatal("the page doesn't offer the drag")
		}
		if p := d.body["permissions"].(map[string]any); p["can_assign"] != false || p["can_write_note"] != false {
			t.Fatalf("a teammate holding no card got more than the board: %v", p)
		}
		// Moving is the only thing it opens up.
		expect(t, e.do("PUT", "/api/cards/"+cardID+"/assignees", m1, map[string]any{"assignee_ids": []uuid.UUID{id("member")}}), 403,
			"they can't take the card")
		expect(t, e.do("PATCH", "/api/cards/"+cardID, m1, map[string]any{"title": "Renamed by a teammate"}), 403, "nor rename it")
		expect(t, e.do("POST", "/api/cards/"+cardID+"/comments", m1, map[string]any{"body": "hello"}), 403, "nor comment")
		expect(t, e.do("DELETE", "/api/cards/"+cardID, m1, nil), 403, "nor remove it")
		expect(t, e.do("POST", path+"/cards", m1, map[string]any{"title": "Teammate task"}), 403, "nor add one")
	})

	t.Run("nobody can write an update before the work is handed out", func(t *testing.T) {
		// Being on the team is enough to read the request, never to write its updates.
		expect(t, e.do("POST", path+"/updates", m1, map[string]any{"destination": "inbox"}), 403, "a member holding no card")
		expect(t, e.do("POST", path+"/updates", sales, map[string]any{"destination": "inbox"}), 403, "Sales never write updates")
	})

	t.Run("a team with nobody added can still hand the work out: its leader is on the list", func(t *testing.T) {
		alone := testutil.Must(e.store.CreateTeam(ctx, db.CreateTeamParams{Name: "Content", LeadID: &tl2ID}))
		r := e.do("POST", "/api/client-requests", sales, body(map[string]any{"team_id": alone.ID, "client_name": "Only a lead"}))
		expect(t, r, 201, "raise")
		id := requestOf(r)["id"].(string)
		got := e.do("GET", "/api/client-requests/"+id, tl2, nil)
		expect(t, got, 200, "the leader opens it")
		names := []string{}
		for _, m := range got.body["roster"].([]any) {
			names = append(names, m.(map[string]any)["name"].(string))
		}
		if len(names) != 1 || names[0] != "tl2" {
			t.Fatalf("roster = %v, want the team's lead", names)
		}
		// And the leader can take it on themselves, which is what lets them write the update.
		card := got.body["cards"].([]any)[0].(map[string]any)["id"].(string)
		expect(t, e.do("PUT", "/api/cards/"+card+"/assignees", tl2, map[string]any{"assignee_ids": []uuid.UUID{tl2ID}}), 200, "take it on")
		u := e.do("POST", "/api/client-requests/"+id+"/updates", tl2, map[string]any{"destination": "inbox"})
		expect(t, u, 201, "the leader writes the update")
		uid := updateOf(u)["id"].(string)
		expect(t, e.do("PATCH", "/api/updates/"+uid, tl2, map[string]any{"message": "Audit sent to the client."}), 200, "write it")
		r2 := e.do("POST", "/api/updates/"+uid+"/submit", tl2, nil)
		expect(t, r2, 200, "submit")
		// A leader checking their own work would be no check at all, so it is approved on submit.
		if updateOf(r2)["status"] != "approved" {
			t.Fatalf("a leader's own update came out %v", updateOf(r2)["status"])
		}
	})

	t.Run("the Team Leader hands it to someone on their team", func(t *testing.T) {
		expect(t, e.do("PUT", "/api/cards/"+cardID+"/assignees", tl, map[string]any{"assignee_ids": []uuid.UUID{id("member2")}}), 422,
			"someone from another team")
		r := e.do("PUT", "/api/cards/"+cardID+"/assignees", tl, map[string]any{"assignee_ids": []uuid.UUID{id("member")}})
		expect(t, r, 200, "assign")
		expect(t, e.do("GET", path, m1, nil), 200, "the member it was handed to")
		expect(t, e.do("GET", path, m2, nil), 404, "the other team's member still doesn't")
	})

	// ── The update, on the same flow as a project's ─────
	var upd string
	t.Run("the member writes it, the Team Leader checks it, Sales send it", func(t *testing.T) {
		expect(t, e.do("POST", path+"/updates", m1, map[string]any{"destination": "delivery"}), 422,
			"a request has no order to deliver")
		r := e.do("POST", path+"/updates", m1, map[string]any{"destination": "inbox"})
		expect(t, r, 201, "draft")
		upd = updateOf(r)["id"].(string)
		u := updateOf(r)
		// It says what it is going to, taken from the request.
		if u["project_id"] != nil || u["client_request_id"] != rid || u["client_name"] != "Nadia Khan" ||
			u["order_id"] != "FO-9001" || u["profile_name"] != "Studio Main" {
			t.Fatalf("the update's client: %v", u)
		}
		expect(t, e.do("GET", "/api/updates/"+upd, sales, nil), 404, "Sales never see a draft")
		expect(t, e.do("PATCH", "/api/updates/"+upd, m1, map[string]any{"message": "The size chart is live on every product page."}), 200, "write it")
		expect(t, e.do("POST", "/api/updates/"+upd+"/submit", m1, nil), 200, "submit")
		expect(t, e.do("POST", "/api/updates/"+upd+"/approve", tl2, map[string]any{}), 404, "another team's leader can't approve")
		// Sales never see an update before it is approved, so it isn't even there for them.
		expect(t, e.do("POST", "/api/updates/"+upd+"/approve", sales, map[string]any{}), 404, "Sales can't approve")
		expect(t, e.do("POST", "/api/updates/"+upd+"/approve", tl, map[string]any{}), 200, "the team's leader approves")
		expect(t, e.do("POST", "/api/updates/"+upd+"/sent", m1, nil), 403, "the member doesn't post it")
		expect(t, e.do("POST", "/api/updates/"+upd+"/sent", sales, nil), 200, "Sales post it and mark it sent")
	})

	t.Run("it shows in the update list with the client it is for", func(t *testing.T) {
		l := e.do("GET", "/api/updates?client_request_id="+rid, sales, nil)
		expect(t, l, 200, "list")
		us := l.body["updates"].([]any)
		if len(us) != 1 || us[0].(map[string]any)["client_name"] != "Nadia Khan" {
			t.Fatalf("updates on the request: %v", us)
		}
		if e.do("GET", "/api/updates?client_request_id="+uuid.NewString(), sales, nil).status != 404 {
			t.Fatal("a request that isn't there should 404")
		}
	})

	t.Run("a Team Leader may hand the request to another team", func(t *testing.T) {
		expect(t, e.do("POST", path+"/team", sales, map[string]any{"team_id": web.ID, "reason": "It's WordPress"}), 403, "not Sales")
		expect(t, e.do("POST", path+"/team", tl, map[string]any{"team_id": web.ID}), 422, "a reason is required")
		expect(t, e.do("POST", path+"/team", tl, map[string]any{"team_id": shopify.ID, "reason": "No change"}), 409, "already there")
		r := e.do("POST", path+"/team", tl, map[string]any{"team_id": web.ID, "reason": "It's a WordPress site, not Shopify"})
		expect(t, r, 200, "move")
		// The leader who moved it doesn't run the new team, so it is no longer theirs to see.
		if r.body["visible"] != false {
			t.Fatalf("after the move: %v", r.body)
		}
		moved := requestOf(e.do("GET", path, tl2, nil))
		if moved["team_name"] != "Web Dev" || len(moved["member_ids"].([]any)) != 0 {
			t.Fatalf("after the move: %v", moved)
		}
		expect(t, e.do("GET", path, m1, nil), 404, "the old team is off it")
		expect(t, e.do("GET", path, tl2, nil), 200, "the new team's leader sees it")
		expect(t, e.do("GET", path, m2, nil), 200, "and everyone on the new team")
		// The team reads the updates written on the request, drafts excepted: they stay
		// with whoever is writing them.
		l := e.do("GET", "/api/updates?client_request_id="+rid, m2, nil)
		expect(t, l, 200, "the new team's member reads its updates")
		if len(l.body["updates"].([]any)) != 1 {
			t.Fatalf("updates for a teammate holding no card: %v", l.body["updates"])
		}
		// Put it back so the rest of the test runs on the Shopify team.
		expect(t, e.do("POST", path+"/team", tl2, map[string]any{"team_id": shopify.ID, "reason": "Back to Shopify"}), 200, "move back")
	})

	t.Run("Sales close it, reopen it, or call it off", func(t *testing.T) {
		expect(t, e.do("POST", path+"/status", tl, map[string]any{"status": "done"}), 403, "the Team Leader doesn't close it")
		expect(t, e.do("POST", path+"/status", sales, map[string]any{"status": "elsewhere"}), 422, "not a status")
		expect(t, e.do("POST", path+"/status", sales, map[string]any{"status": "cancelled"}), 422, "cancelling needs a reason")
		r := e.do("POST", path+"/status", sales, map[string]any{"status": "done"})
		expect(t, r, 200, "done")
		if q := requestOf(r); q["status"] != "done" || q["closed_by_name"] != "sales" {
			t.Fatalf("closed: %v", q)
		}
		expect(t, e.do("PATCH", path, sales, body(map[string]any{"title": "Changed after closing"})), 409, "a closed request isn't edited")
		expect(t, e.do("POST", path+"/status", sales, map[string]any{"status": "open"}), 200, "reopen")
		expect(t, e.do("PATCH", path, sales, body(map[string]any{"title": "Add a size chart and a care guide"})), 200, "edit while open")
	})

	t.Run("an approved update has to be sent before the request is closed", func(t *testing.T) {
		// The move cleared its assignees, so the leader hands the card out again.
		expect(t, e.do("PUT", "/api/cards/"+cardID+"/assignees", tl, map[string]any{"assignee_ids": []uuid.UUID{id("member")}}), 200, "reassign")
		r := e.do("POST", path+"/updates", m1, map[string]any{"destination": "inbox"})
		expect(t, r, 201, "second update")
		second := updateOf(r)["id"].(string)
		expect(t, e.do("PATCH", "/api/updates/"+second, m1, map[string]any{"message": "And the care guide is in."}), 200, "write")
		expect(t, e.do("POST", "/api/updates/"+second+"/submit", m1, nil), 200, "submit")
		expect(t, e.do("POST", "/api/updates/"+second+"/approve", tl, map[string]any{}), 200, "approve")
		expect(t, e.do("POST", path+"/status", sales, map[string]any{"status": "done"}), 409, "not while one is waiting to be sent")
		expect(t, e.do("POST", "/api/updates/"+second+"/sent", sales, nil), 200, "send it")
		expect(t, e.do("POST", path+"/status", sales, map[string]any{"status": "done"}), 200, "now it closes")
	})

	t.Run("the trail reads back on the request", func(t *testing.T) {
		r := e.do("GET", path, tl, nil)
		expect(t, r, 200, "detail")
		want := map[string]bool{"request.raised": false, "card.assigned": false, "update.sent": false, "request.status_changed": false}
		for _, a := range r.body["activity"].([]any) {
			if k, ok := a.(map[string]any)["action"].(string); ok {
				if _, tracked := want[k]; tracked {
					want[k] = true
				}
			}
		}
		for k, seen := range want {
			if !seen {
				t.Errorf("the timeline is missing %s", k)
			}
		}
	})

	t.Run("search finds it by client and order", func(t *testing.T) {
		for _, q := range []string{"Nadia", "FO-9001"} {
			r := e.do("GET", "/api/search?q="+q, sales, nil)
			expect(t, r, 200, "search "+q)
			var found bool
			for _, h := range r.body["results"].([]any) {
				m := h.(map[string]any)
				found = found || (m["kind"] == "request" && m["id"] == rid)
			}
			if !found {
				t.Fatalf("search for %q didn't find the request: %v", q, r.body["results"])
			}
		}
	})
}
