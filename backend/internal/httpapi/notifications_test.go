package httpapi

import (
	"context"
	"errors"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/solvinx/opsflow/internal/db"
	"github.com/solvinx/opsflow/internal/notify"
	"github.com/solvinx/opsflow/internal/testutil"
)

type note struct {
	ID        float64
	Type      string
	Title     string
	Body      string
	Important bool
	Read      bool
}

func (e *env) notes(cookie string) []note {
	e.t.Helper()
	r := e.do("GET", "/api/notifications?limit=100", cookie, nil)
	expect(e.t, r, 200, "list notifications")
	var out []note
	for _, raw := range r.body["notifications"].([]any) {
		n := raw.(map[string]any)
		out = append(out, note{
			ID: n["id"].(float64), Type: n["type"].(string), Title: n["title"].(string), Body: n["body"].(string),
			Important: n["is_important"].(bool), Read: n["read_at"] != nil,
		})
	}
	return out
}

func ofType(ns []note, typ string) []note {
	var out []note
	for _, n := range ns {
		if n.Type == typ {
			out = append(out, n)
		}
	}
	return out
}

func TestNotifications(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	profile := testutil.Must(e.store.CreateProfile(ctx, db.CreateProfileParams{Name: "Studio Main"}))
	ptype := testutil.Must(e.store.CreateProjectType(ctx, db.CreateProjectTypeParams{Name: "Website"}))
	sales, tl, mem, mgmt := e.login("sales"), e.login("tl"), e.login("member"), e.login("ceo")
	future := time.Now().AddDate(0, 1, 0).Format(time.DateOnly)

	// Real-time: listen on the test database like the API does.
	listenCtx, stop := context.WithCancel(ctx)
	defer stop()
	go e.hub.Listen(listenCtx, e.store.Pool)
	memberStream, unsubscribe := e.hub.Subscribe(e.users["member"].ID)
	defer unsubscribe()
	time.Sleep(200 * time.Millisecond) // let LISTEN start

	r := e.do("POST", "/api/projects", sales, map[string]any{
		"name": "Acme", "profile_id": profile.ID, "client_name": "Acme Ltd", "order_id": "N-1",
		"project_type_id": ptype.ID, "scope": "Scope", "agreed_price": 1000, "deadline": future,
	})
	pid := projectOf(r)["id"].(string)
	path := "/api/projects/" + pid
	transition := func(cookie, to, comment string) {
		t.Helper()
		expect(t, e.do("POST", path+"/transition", cookie, map[string]any{"to": to, "comment": comment}), 200, "to "+to)
	}

	// A new project goes straight to its team's leaders.
	if n := ofType(e.notes(tl), notify.ProjectSubmitted); len(n) != 1 || !strings.Contains(n[0].Title, "#1 Acme") || n[0].Body != "From sales" {
		t.Fatalf("TL new-project notification: %+v", n)
	}
	if len(e.notes(sales)) != 0 || len(e.notes(mgmt)) != 0 {
		t.Fatal("actor and management must not be told about a new project")
	}

	expect(t, e.do("POST", path+"/assign", tl, map[string]any{"assignee_id": e.users["member"].ID, "priority": "high", "due_date": future}), 200, "assign")
	n := ofType(e.notes(mem), notify.ProjectAssigned)
	if len(n) != 1 || !strings.HasPrefix(n[0].Body, "Priority High · due ") {
		t.Fatalf("assigned notification: %+v", n)
	}
	select {
	case id := <-memberStream:
		if float64(id) != n[0].ID {
			t.Fatalf("stream delivered %d, want %v", id, n[0].ID)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("assignment was not delivered in real time")
	}

	t.Run("sales edits batch into one notification that says what changed", func(t *testing.T) {
		expect(t, e.do("PATCH", path, sales, map[string]any{"client_contact": "a@acme.test", "agreed_price": "1200"}), 200, "edit 1")
		for i := range 5 {
			body := []byte("file " + string(rune('A'+i)))
			r := e.do("POST", path+"/files/uploads", sales, map[string]any{"filename": "asset-" + string(rune('a'+i)) + ".png", "size": len(body), "content_type": "image/png"})
			putToStorage(t, r.body, body)
			expect(t, e.do("POST", path+"/files/uploads/"+r.body["upload_id"].(string)+"/complete", sales, nil), 200, "upload")
		}
		for _, who := range []string{mem, tl} {
			batch := ofType(e.notes(who), notify.ProjectChanges)
			if len(batch) != 1 {
				t.Fatalf("want one batched notification, got %d: %+v", len(batch), batch)
			}
			b := batch[0]
			if !strings.Contains(b.Body, "sales: Agreed price changed from 1,000.00 USD to 1,200.00 USD") ||
				!strings.Contains(b.Body, "sales: Client contact changed from empty to a@acme.test") ||
				!strings.Contains(b.Body, "sales: Added file asset-c.png") ||
				!strings.Contains(b.Body, "…and 2 more changes") || b.Title != "Changes on #1 Acme" {
				t.Fatalf("batch body:\n%s", b.Body)
			}
		}
		// Reading it closes the batch; the next change starts a new one.
		b := ofType(e.notes(mem), notify.ProjectChanges)[0]
		expect(t, e.do("POST", "/api/notifications/"+ftoa(b.ID)+"/read", mem, nil), 200, "read")
		expect(t, e.do("PATCH", path, sales, map[string]any{"client_name": "Acme Group"}), 200, "edit 2")
		if batch := ofType(e.notes(mem), notify.ProjectChanges); len(batch) != 2 || batch[0].Body != "sales: Client name changed from Acme Ltd to Acme Group" {
			t.Fatalf("new batch after reading: %+v", batch)
		}
	})

	t.Run("important changes skip the batch and are flagged", func(t *testing.T) {
		newDeadline := time.Now().AddDate(0, 0, 20).Format(time.DateOnly)
		expect(t, e.do("PATCH", path, sales, map[string]any{"deadline": newDeadline}), 200, "deadline")
		imp := ofType(e.notes(mem), notify.ProjectImportant)
		// A date-only deadline means the end of that day, shown with its time.
		at := func(d string) string { return mustDate(d).Format("Jan 2") + ", 23:59" }
		want := "Delivery deadline changed from " + at(future) + " to " + at(newDeadline)
		if len(imp) != 1 || !imp[0].Important || imp[0].Body != want || !strings.HasPrefix(imp[0].Title, "Please acknowledge") {
			t.Fatalf("member important notification: %+v (want body %q)", imp, want)
		}
		if tlImp := ofType(e.notes(tl), notify.ProjectImportant); len(tlImp) != 1 || !tlImp[0].Important {
			t.Fatalf("TL important notification: %+v", tlImp)
		}
	})

	transition(mem, "blocked", "Waiting on API key")
	for _, who := range []string{tl, mgmt} {
		if b := ofType(e.notes(who), notify.ProjectBlocked); len(b) != 1 || b[0].Body != "Waiting on API key" {
			t.Fatalf("blocked notification: %+v", b)
		}
	}
	transition(mem, "in_progress", "")

	t.Run("update lifecycle", func(t *testing.T) {
		r := e.do("POST", path+"/updates", mem, map[string]any{"message": "Draft ready", "destination": "order_page"})
		uid := updateOf(r)["id"].(string)
		upath := "/api/updates/" + uid
		expect(t, e.do("POST", upath+"/submit", mem, nil), 200, "submit")
		if n := ofType(e.notes(tl), notify.UpdateSubmitted); len(n) != 1 || n[0].Body != "“Draft ready”" {
			t.Fatalf("TL update submitted: %+v", n)
		}
		expect(t, e.do("POST", upath+"/request-changes", tl, map[string]any{"comment": "Add the link"}), 200, "changes")
		if n := ofType(e.notes(mem), notify.UpdateChanges); len(n) != 1 || n[0].Body != "Add the link" {
			t.Fatalf("member changes requested: %+v", n)
		}
		expect(t, e.do("POST", upath+"/submit", mem, nil), 200, "resubmit")
		expect(t, e.do("POST", upath+"/approve", tl, nil), 200, "approve")
		for _, who := range []string{mem, sales} {
			if n := ofType(e.notes(who), notify.UpdateApproved); len(n) != 1 {
				t.Fatalf("approved notification: %+v", n)
			}
		}
		expect(t, e.do("POST", upath+"/sent", sales, nil), 200, "sent")
		for _, who := range []string{mem, tl} {
			if n := ofType(e.notes(who), notify.UpdateSent); len(n) != 1 {
				t.Fatalf("sent notification: %+v", n)
			}
		}
		if len(ofType(e.notes(sales), notify.UpdateSent)) != 0 {
			t.Fatal("Sales was notified about their own action")
		}
	})

	t.Run("unread count and read all", func(t *testing.T) {
		r := e.do("GET", "/api/notifications/unread-count", mem, nil)
		before := r.body["unread_count"].(float64)
		if before == 0 {
			t.Fatal("member should have unread notifications")
		}
		r = e.do("POST", "/api/notifications/read-all", mem, nil)
		if r.body["unread_count"].(float64) != 0 {
			t.Fatalf("after read-all: %v", r.body)
		}
		// Someone else's notification id can't be marked read by you.
		other := e.notes(tl)[0]
		expect(t, e.do("POST", "/api/notifications/"+ftoa(other.ID)+"/read", mem, nil), 200, "foreign read is a no-op")
		if e.notes(tl)[0].Read {
			t.Fatal("member marked the TL's notification read")
		}
	})

	t.Run("scheduled checks fire once", func(t *testing.T) {
		past := time.Now().AddDate(0, 0, -2).Format(time.DateOnly)
		if _, err := e.store.Pool.Exec(ctx, `UPDATE projects SET due_date = $1 WHERE id = $2`, past, pid); err != nil {
			t.Fatal(err)
		}
		// A second project added 25h ago and never assigned.
		r := e.do("POST", "/api/projects", sales, map[string]any{
			"name": "Waiting", "profile_id": profile.ID, "client_name": "W", "order_id": "N-2",
			"project_type_id": ptype.ID, "scope": "S", "agreed_price": 1, "deadline": future,
		})
		wid := projectOf(r)["id"].(string)
		if _, err := e.store.Pool.Exec(ctx, `UPDATE projects SET first_submitted_at = now() - interval '25 hours' WHERE id = $1`, wid); err != nil {
			t.Fatal(err)
		}
		// An update approved 25h ago and never sent.
		r = e.do("POST", path+"/updates", mem, map[string]any{"message": "Old approved", "destination": "order_page"})
		uid := updateOf(r)["id"].(string)
		expect(t, e.do("POST", "/api/updates/"+uid+"/submit", mem, nil), 200, "submit")
		expect(t, e.do("POST", "/api/updates/"+uid+"/approve", tl, nil), 200, "approve")
		if _, err := e.store.Pool.Exec(ctx, `UPDATE updates SET checked_at = now() - interval '25 hours' WHERE id = $1`, uid); err != nil {
			t.Fatal(err)
		}

		for range 2 { // second run must not duplicate anything
			if err := e.svc.RunChecks(ctx, time.Now()); err != nil {
				t.Fatal(err)
			}
		}
		for _, who := range []string{tl, mgmt} {
			if o := ofType(e.notes(who), notify.ProjectOverdue); len(o) != 1 || !o[0].Important {
				t.Fatalf("overdue for %v: %+v", who == tl, o)
			}
		}
		if w := ofType(e.notes(mgmt), notify.ProjectWaiting); len(w) != 1 || !strings.Contains(w[0].Title, "Waiting") {
			t.Fatalf("unassigned 24h: %+v", w)
		}
		if len(ofType(e.notes(tl), notify.ProjectWaiting)) != 0 {
			t.Fatal("unassigned 24h goes to Management only")
		}
		for _, who := range []string{sales, tl} {
			if u := ofType(e.notes(who), notify.UpdateUnsent); len(u) != 1 {
				t.Fatalf("unsent 24h: %+v", u)
			}
		}
		// Changing the due date re-arms the overdue alert.
		expect(t, e.do("PUT", path+"/planning", tl, map[string]any{"priority": "high", "due_date": future}), 200, "replan")
		if _, err := e.store.Pool.Exec(ctx, `UPDATE projects SET due_date = $1 WHERE id = $2`, past, pid); err != nil {
			t.Fatal(err)
		}
		_ = e.svc.RunChecks(ctx, time.Now())
		if o := ofType(e.notes(tl), notify.ProjectOverdue); len(o) != 2 {
			t.Fatalf("overdue should re-fire after a new due date: %d", len(o))
		}
	})

	t.Run("rolled-back notifications are never streamed", func(t *testing.T) {
		for len(memberStream) > 0 {
			<-memberStream
		}
		boom := errors.New("rollback")
		err := e.store.Tx(ctx, func(q *db.Queries) error {
			if err := notify.Send(ctx, q, notify.Recipients{}.Add(e.users["member"].ID), e.users["tl"].ID, notify.Notification{Type: "test", Title: "ghost"}); err != nil {
				return err
			}
			return boom
		})
		if !errors.Is(err, boom) {
			t.Fatal(err)
		}
		select {
		case id := <-memberStream:
			t.Fatalf("rolled-back notification %d was streamed", id)
		case <-time.After(500 * time.Millisecond):
		}
	})
}

func ftoa(f float64) string { return strconv.FormatInt(int64(f), 10) }

func mustDate(s string) time.Time {
	t, _ := time.Parse(time.DateOnly, s)
	return t
}
