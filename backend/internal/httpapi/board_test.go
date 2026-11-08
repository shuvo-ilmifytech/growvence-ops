package httpapi

import (
	"context"
	"fmt"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/solvinx/opsflow/internal/db"
	"github.com/solvinx/opsflow/internal/policy"
	"github.com/solvinx/opsflow/internal/testutil"
)

// Several members per project, and the project board (client requests and tasks).
func TestProjectMembersAndBoard(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	e.addUser("member2", policy.Member, false)
	e.addUser("member3", policy.Member, false)
	e.addUser("tl2", policy.TL, false)
	profile := testutil.Must(e.store.CreateProfile(ctx, db.CreateProfileParams{Name: "Studio Main", Platform: "Fiverr"}))
	tlID, tl2ID := e.users["tl"].ID, e.users["tl2"].ID
	team := testutil.Must(e.store.CreateTeam(ctx, db.CreateTeamParams{Name: "Shopify", LeadID: &tlID}))
	testutil.Must(e.store.CreateTeam(ctx, db.CreateTeamParams{Name: "Web Dev", LeadID: &tl2ID}))
	for _, m := range []string{"member", "member2", "member3"} {
		if err := e.store.SetTeamMember(ctx, db.SetTeamMemberParams{UserID: e.users[m].ID, TeamID: team.ID}); err != nil {
			t.Fatal(err)
		}
	}
	ptype := testutil.Must(e.store.CreateProjectType(ctx, db.CreateProjectTypeParams{Name: "Shopify Store", TeamID: &team.ID}))
	sales, tl, tl2, ceo := e.login("sales"), e.login("tl"), e.login("tl2"), e.login("ceo")
	m1, m2, m3 := e.login("member"), e.login("member2"), e.login("member3")
	id := func(label string) uuid.UUID { return e.users[label].ID }

	r := e.do("POST", "/api/projects", sales, map[string]any{
		"name": "Acme store", "profile_id": profile.ID, "client_name": "Acme", "order_id": "FO-1",
		"project_type_id": ptype.ID, "scope": "Build", "agreed_price": "100", "deadline": time.Now().AddDate(0, 1, 0).Format(time.DateOnly),
	})
	expect(t, r, 201, "create")
	pid := projectOf(r)["id"].(string)
	path := "/api/projects/" + pid

	// ── Several members ─────────────────────────────────
	r = e.do("POST", path+"/assign", tl, map[string]any{"assignee_ids": []uuid.UUID{id("member"), id("member2")}, "priority": "high"})
	expect(t, r, 200, "assign two members")
	if p := projectOf(r); p["status"] != "in_progress" || len(p["member_ids"].([]any)) != 2 || p["assigned_to"] != id("member").String() {
		t.Fatalf("after assign: %v", p)
	}
	expect(t, e.do("GET", path, m2, nil), 200, "second member sees it")
	expect(t, e.do("GET", path, m3, nil), 404, "unassigned member doesn't")
	expect(t, e.do("POST", path+"/transition", m2, map[string]any{"to": "blocked", "comment": "Waiting on logo"}), 200, "any member blocks")
	expect(t, e.do("POST", path+"/transition", m1, map[string]any{"to": "in_progress"}), 200, "any member unblocks")

	t.Run("adding and removing members keeps the status", func(t *testing.T) {
		r := e.do("POST", path+"/assign", tl, map[string]any{"assignee_ids": []uuid.UUID{id("member"), id("member3")}, "priority": "high"})
		expect(t, r, 200, "swap member2 for member3")
		if p := projectOf(r); p["status"] != "in_progress" {
			t.Fatalf("status = %v", p["status"])
		}
		expect(t, e.do("GET", path, m2, nil), 404, "removed member loses access")
		expect(t, e.do("GET", path, m3, nil), 200, "added member sees it")
		expect(t, e.do("POST", path+"/assign", tl, map[string]any{"assignee_ids": []uuid.UUID{id("member"), id("member3")}, "priority": "high"}), 409, "no change")
		expect(t, e.do("POST", path+"/assign", tl, map[string]any{"assignee_ids": []uuid.UUID{}, "priority": "high"}), 422, "nobody")
	})

	// ── Board ───────────────────────────────────────────
	board := func(cookie string) []any {
		r := e.do("GET", path+"/cards", cookie, nil)
		expect(t, r, 200, "board")
		return r.body["cards"].([]any)
	}
	newCard := func(cookie string, body map[string]any) (resp, string) {
		r := e.do("POST", path+"/cards", cookie, body)
		if c, ok := r.body["card"].(map[string]any); ok {
			return r, c["id"].(string)
		}
		return r, ""
	}

	r, clientCard := newCard(sales, map[string]any{"title": "Change header colour", "description": "Client wants navy."})
	expect(t, r, 201, "Sales adds a client request")
	if r.body["card"].(map[string]any)["source"] != "client" {
		t.Fatalf("source = %v", r.body["card"])
	}
	r, task := newCard(tl, map[string]any{"title": "Set up theme", "assignee_ids": []uuid.UUID{id("member3")}})
	expect(t, r, 201, "TL adds a task for member3")
	r, _ = newCard(tl, map[string]any{"title": "x", "assignee_ids": []uuid.UUID{id("member2")}})
	expect(t, r, 422, "card assignees must be on the project")
	r, _ = newCard(m1, map[string]any{"title": "x"})
	expect(t, r, 403, "members don't create cards")
	r, _ = newCard(ceo, map[string]any{"title": "x"})
	expect(t, r, 403, "CEO is read-only")
	r, _ = newCard(tl2, map[string]any{"title": "x"})
	expect(t, r, 404, "another team's lead can't see the project")

	t.Run("who sees which cards", func(t *testing.T) {
		if n := len(board(sales)); n != 1 {
			t.Fatalf("Sales sees %d cards, want only their own", n)
		}
		for _, who := range []string{tl, m1, ceo} {
			if n := len(board(who)); n != 2 {
				t.Fatalf("sees %d cards, want 2", n)
			}
		}
		expect(t, e.do("GET", "/api/cards/"+task, sales, nil), 404, "Sales can't open the TL's task")
		expect(t, e.do("GET", "/api/cards/"+task, m2, nil), 404, "removed member can't open a card")
		expect(t, e.do("GET", "/api/cards/"+clientCard, tl2, nil), 404, "other team's lead")
	})

	t.Run("the all-projects board shows the same cards in one place", func(t *testing.T) {
		count := func(cookie, query string) int {
			r := e.do("GET", "/api/cards"+query, cookie, nil)
			expect(t, r, 200, "all cards")
			return len(r.body["cards"].([]any))
		}
		if n := count(sales, ""); n != 1 {
			t.Fatalf("Sales sees %d", n)
		}
		for who, cookie := range map[string]string{"tl": tl, "member": m1, "ceo": ceo} {
			if n := count(cookie, ""); n != 2 {
				t.Fatalf("%s sees %d", who, n)
			}
		}
		if count(tl2, "") != 0 || count(m2, "") != 0 {
			t.Fatal("another team's lead or a removed member sees cards")
		}
		if n := count(m3, "?mine=true"); n != 1 {
			t.Fatalf("member3's own cards = %d", n)
		}
		if n := count(tl, "?source=client"); n != 1 {
			t.Fatalf("client cards = %d", n)
		}
		r := e.do("GET", "/api/cards", sales, nil)
		c := r.body["cards"].([]any)[0].(map[string]any)
		if c["project_name"] != "Acme store" {
			t.Fatalf("card project = %v", c["project_name"])
		}
		ps := r.body["projects"].([]any)
		if len(ps) != 1 || ps[0].(map[string]any)["can_create"] != true || ps[0].(map[string]any)["can_move"] != false {
			t.Fatalf("Sales projects = %v", ps)
		}
		expect(t, e.do("GET", "/api/cards?source=bogus", tl, nil), 422, "bad filter")
	})

	t.Run("global search spans projects, cards, files and people, within each scope", func(t *testing.T) {
		find := func(cookie, q string) map[string][]string {
			r := e.do("GET", "/api/search?q="+url.QueryEscape(q), cookie, nil)
			expect(t, r, 200, "search "+q)
			out := map[string][]string{}
			for _, x := range r.body["results"].([]any) {
				m := x.(map[string]any)
				out[m["kind"].(string)] = append(out[m["kind"].(string)], m["title"].(string))
			}
			return out
		}
		if got := find(tl, "FO-1")["project"]; len(got) != 1 || got[0] != "Acme store" {
			t.Fatalf("by order id: %v", got)
		}
		if got := find(tl, "Acme")["project"]; len(got) != 1 {
			t.Fatalf("by client name: %v", got)
		}
		if got := find(tl, "header colour")["card"]; len(got) != 1 {
			t.Fatalf("by card title: %v", got)
		}
		if got := find(sales, "Set up theme")["card"]; len(got) != 0 {
			t.Fatalf("Sales found the TL's task: %v", got)
		}
		if got := find(tl, "member3")["person"]; len(got) != 1 {
			t.Fatalf("by person: %v", got)
		}
		if got := find(tl2, "Acme"); len(got["project"]) != 0 || len(got["card"]) != 0 {
			t.Fatalf("another team's lead found a project: %v", got)
		}
		if got := find(m2, "Acme")["project"]; len(got) != 0 {
			t.Fatalf("a member not on the project found it: %v", got)
		}
		if n := len(e.do("GET", "/api/search?q=a", tl, nil).body["results"].([]any)); n != 0 {
			t.Fatalf("one letter should not search: %d results", n)
		}
	})

	t.Run("the browser rings for unopened tasks and unsent updates", func(t *testing.T) {
		alerts := func(cookie string) map[string]int64 {
			r := e.do("GET", "/api/alerts", cookie, nil)
			expect(t, r, 200, "alerts")
			out := map[string]int64{}
			for _, a := range r.body["items"].([]any) {
				a := a.(map[string]any)
				out[a["kind"].(string)] = int64(a["count"].(float64))
			}
			return out
		}
		// Omar holds the TL's task and has never opened it.
		if n := alerts(m3)["unopened_cards"]; n != 1 {
			t.Fatalf("omar's unopened cards = %d", n)
		}
		// Opening it stops the ringing, and twice is the same as once.
		for i := 0; i < 2; i++ {
			expect(t, e.do("POST", "/api/cards/"+task+"/opened", m3, nil), 204, "mark opened")
		}
		if n := alerts(m3)["unopened_cards"]; n != 0 {
			t.Fatalf("still ringing after opening: %d", n)
		}
		// Someone who isn't assigned to it is never rung about it.
		if n := alerts(m1)["unopened_cards"]; n != 0 {
			t.Fatalf("milo is rung about omar's card: %d", n)
		}
		// Nothing rings for a card you can't even see.
		expect(t, e.do("POST", "/api/cards/"+task+"/opened", m2, nil), 404, "a stranger cannot mark it opened")
		// Sales hear about approved updates nobody has sent; HR and the CEO hear nothing.
		if n := alerts(sales)["unsent_updates"]; n != 0 {
			t.Fatalf("nothing is approved yet, got %d", n)
		}
		for who, cookie := range map[string]string{"hr": e.login("hr"), "ceo": ceo} {
			if items := e.do("GET", "/api/alerts", cookie, nil).body["items"].([]any); len(items) != 0 {
				t.Fatalf("%s is rung about work they don't do: %v", who, items)
			}
		}
	})

	t.Run("a Sales colleague covers while the owner is away", func(t *testing.T) {
		e.addUser("sales2", policy.Sales, false)
		s2 := e.login("sales2")
		expect(t, e.do("GET", path, s2, nil), 200, "colleague sees the project")
		cards := e.do("GET", path+"/cards", s2, nil).body["cards"].([]any)
		if len(cards) != 1 || cards[0].(map[string]any)["source"] != "client" {
			t.Fatalf("colleague sees %v (client requests only, never tasks)", cards)
		}
		expect(t, e.do("PATCH", "/api/cards/"+clientCard, s2, map[string]any{"title": "x"}), 403, "only its author edits a request")
		r, cover := newCard(s2, map[string]any{"title": "Client also wants a FAQ page"})
		expect(t, r, 201, "colleague logs a request for the client")
		expect(t, e.do("POST", "/api/cards/"+cover+"/comments", sales, map[string]any{"body": "Thanks for covering"}), 200, "owner comments on it")
		expect(t, e.do("POST", "/api/cards/"+cover+"/move", m1, map[string]any{"column": "done"}), 200, "done")
		if !strings.Contains(titles(e, sales), "Client request done: Client also wants a FAQ page") {
			t.Fatalf("project owner not told: %s", titles(e, sales))
		}
		expect(t, e.do("DELETE", "/api/cards/"+cover, tl, nil), 204, "tidy up")
	})

	t.Run("members move cards; Sales hears when their request is done", func(t *testing.T) {
		expect(t, e.do("POST", "/api/cards/"+clientCard+"/move", sales, map[string]any{"column": "in_progress"}), 403, "Sales can't move")
		expect(t, e.do("POST", "/api/cards/"+clientCard+"/move", ceo, map[string]any{"column": "in_progress"}), 403, "CEO can't move")
		expect(t, e.do("POST", "/api/cards/"+clientCard+"/move", m1, map[string]any{"column": "sideways"}), 422, "bad column")
		expect(t, e.do("POST", "/api/cards/"+clientCard+"/move", m1, map[string]any{"column": "review", "index": 0}), 200, "to review")
		if !strings.Contains(titles(e, tl), "Card ready for review: Change header colour") {
			t.Fatalf("TL not told: %s", titles(e, tl))
		}
		r := e.do("POST", "/api/cards/"+clientCard+"/move", m3, map[string]any{"column": "done", "index": 0})
		expect(t, r, 200, "to done")
		if c := r.body["card"].(map[string]any); c["column_key"] != "done" || c["done_at"] == nil {
			t.Fatalf("card = %v", c)
		}
		if !strings.Contains(titles(e, sales), "Client request done: Change header colour") {
			t.Fatalf("Sales not told: %s", titles(e, sales))
		}
		if !strings.Contains(titles(e, m1), "New client request on") {
			t.Fatalf("member not told about the new request: %s", titles(e, m1))
		}
	})

	t.Run("ordering within a column", func(t *testing.T) {
		_, a := newCard(tl, map[string]any{"title": "A"})
		_, b := newCard(tl, map[string]any{"title": "B"})
		expect(t, e.do("POST", "/api/cards/"+b+"/move", m1, map[string]any{"column": "todo", "index": 0}), 200, "B to the top")
		var order []string
		for _, c := range board(tl) {
			c := c.(map[string]any)
			if c["column_key"] == "todo" {
				order = append(order, c["title"].(string))
			}
		}
		if strings.Join(order, ",") != "B,Set up theme,A" {
			t.Fatalf("todo order = %v", order)
		}
		_ = a
	})

	t.Run("attachments on cards (the client's files)", func(t *testing.T) {
		attach := func(cookie, card string, body []byte, name string) resp {
			r := e.do("POST", "/api/cards/"+card+"/attachments/uploads", cookie, map[string]any{"filename": name, "size": len(body), "content_type": "image/png"})
			if r.status != 201 {
				return r
			}
			putToStorage(t, r.body, body)
			return e.do("POST", "/api/cards/"+card+"/attachments/uploads/"+r.body["upload_id"].(string)+"/complete", cookie, nil)
		}
		logo := []byte("client-logo")
		r := attach(sales, clientCard, logo, "logo.png")
		expect(t, r, 200, "Sales attaches the client's file")
		atts := r.body["attachments"].([]any)
		if len(atts) != 1 || r.body["card"].(map[string]any)["attachment_count"].(float64) != 1 {
			t.Fatalf("attachments = %v", r.body)
		}
		vid := atts[0].(map[string]any)["version_id"].(string)
		dl := e.raw("GET", "/api/files/versions/"+vid+"/download", m1)
		if dl.StatusCode != 302 || string(fetch(t, dl.Header.Get("Location"))) != "client-logo" {
			t.Fatalf("member download: %d", dl.StatusCode)
		}
		if dl := e.raw("GET", "/api/files/versions/"+vid+"/download", tl2); dl.StatusCode != 404 {
			t.Fatalf("other team's lead downloaded: %d", dl.StatusCode)
		}
		r = attach(m3, task, []byte("draft"), "draft.png")
		expect(t, r, 200, "member attaches to a task")
		taskFile := r.body["attachments"].([]any)[0].(map[string]any)
		if dl := e.raw("GET", "/api/files/versions/"+taskFile["version_id"].(string)+"/download", sales); dl.StatusCode != 404 {
			t.Fatalf("Sales downloaded a task attachment: %d", dl.StatusCode)
		}
		expect(t, attach(ceo, clientCard, logo, "x.png"), 403, "CEO can't attach")
		fid := atts[0].(map[string]any)["id"].(string)
		expect(t, e.do("DELETE", "/api/cards/"+clientCard+"/attachments/"+fid, m1, nil), 403, "only the uploader or TL removes")
		files := e.do("GET", path, tl, nil).body["files"].([]any)
		if len(files) != 0 {
			t.Fatalf("card files leaked into the project's files: %v", files)
		}
		expect(t, e.do("DELETE", "/api/cards/"+clientCard+"/attachments/"+fid, sales, nil), 200, "uploader removes")
	})

	t.Run("editing, assigning, commenting and removing", func(t *testing.T) {
		expect(t, e.do("PATCH", "/api/cards/"+clientCard, sales, map[string]any{"description": "Navy, not black."}), 200, "Sales edits own card")
		expect(t, e.do("PATCH", "/api/cards/"+task, m1, map[string]any{"title": "y"}), 403, "members don't edit content")
		expect(t, e.do("PUT", "/api/cards/"+task+"/assignees", m1, map[string]any{"assignee_ids": []uuid.UUID{id("member"), id("member3")}}), 200, "member assigns")
		expect(t, e.do("PUT", "/api/cards/"+task+"/assignees", m1, map[string]any{"assignee_ids": []uuid.UUID{id("member2")}}), 422, "not on the project")
		r := e.do("POST", "/api/cards/"+clientCard+"/comments", sales, map[string]any{"body": "Client confirmed navy."})
		expect(t, r, 200, "Sales comments on own card")
		if cs := r.body["comments"].([]any); len(cs) != 1 {
			t.Fatalf("comments = %v", cs)
		}
		expect(t, e.do("POST", "/api/cards/"+task+"/comments", m3, map[string]any{"body": "On it"}), 200, "member comments")
		expect(t, e.do("DELETE", "/api/cards/"+clientCard, sales, nil), 409, "Sales can't remove a request Ops has started")
		expect(t, e.do("DELETE", "/api/cards/"+clientCard, tl, nil), 204, "TL removes it")
		expect(t, e.do("GET", "/api/cards/"+clientCard, tl, nil), 404, "removed")
	})

	t.Run("taking a member off the project takes them off its cards", func(t *testing.T) {
		expect(t, e.do("POST", path+"/assign", tl, map[string]any{"assignee_ids": []uuid.UUID{id("member")}, "priority": "high"}), 200, "drop member3")
		c := e.do("GET", "/api/cards/"+task, tl, nil).body["card"].(map[string]any)
		if fmt.Sprint(c["assignee_ids"]) != fmt.Sprint([]any{id("member").String()}) {
			t.Fatalf("assignees = %v", c["assignee_ids"])
		}
	})
}

func titles(e *env, cookie string) string {
	var out []string
	for _, n := range e.do("GET", "/api/notifications?limit=100", cookie, nil).body["notifications"].([]any) {
		out = append(out, fmt.Sprint(n.(map[string]any)["title"]))
	}
	return strings.Join(out, " | ")
}
