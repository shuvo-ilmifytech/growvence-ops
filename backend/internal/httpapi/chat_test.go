package httpapi

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/solvinx/opsflow/internal/policy"
)

// One-to-one chat: anyone signed in may message any active colleague, each thread is
// private to the two of them, and unread counts follow what has actually been read.
func TestMessages(t *testing.T) {
	e := newEnv(t)
	sales, tl, member, hr := e.login("sales"), e.login("tl"), e.login("member"), e.login("hr")
	salesID, tlID, memberID := e.users["sales"].ID.String(), e.users["tl"].ID.String(), e.users["member"].ID.String()

	t.Run("a message reaches the other person and nobody else", func(t *testing.T) {
		r := e.do("POST", "/api/messages/"+memberID, tl, map[string]any{"body": "  Can you take the Acme store?  "})
		expect(t, r, 201, "send")
		m := r.body["message"].(map[string]any)
		if m["body"] != "Can you take the Acme store?" {
			t.Fatalf("body not trimmed: %q", m["body"])
		}
		if m["mine"] != false && m["sender_name"] != "tl" {
			t.Fatalf("message: %v", m)
		}
		// The member sees it, unread.
		in := e.do("GET", "/api/messages", member, nil)
		expect(t, in, 200, "inbox")
		if in.body["unread"].(float64) != 1 {
			t.Fatalf("unread = %v", in.body["unread"])
		}
		th := in.body["threads"].([]any)
		if len(th) != 1 || th[0].(map[string]any)["user_name"] != "tl" || th[0].(map[string]any)["unread"].(float64) != 1 {
			t.Fatalf("threads: %v", th)
		}
		if th[0].(map[string]any)["last_from_me"] != false {
			t.Fatal("the member did not send it")
		}
		// Sales has nothing: it was not their conversation.
		if n := e.do("GET", "/api/messages", sales, nil).body["unread"].(float64); n != 0 {
			t.Fatalf("sales unread = %v", n)
		}
		if len(e.do("GET", "/api/messages", sales, nil).body["threads"].([]any)) != 0 {
			t.Fatal("sales can see someone else's thread list")
		}
		// And Sales reading that pair's thread sees an empty conversation, not their words.
		other := e.do("GET", "/api/messages/"+tlID, sales, nil)
		expect(t, other, 200, "unrelated thread")
		if len(other.body["messages"].([]any)) != 0 {
			t.Fatalf("sales sees the TL's messages to the member: %v", other.body["messages"])
		}
	})

	t.Run("the thread reads oldest first and marks as read", func(t *testing.T) {
		expect(t, e.do("POST", "/api/messages/"+tlID, member, map[string]any{"body": "Yes, starting now"}), 201, "reply")
		r := e.do("GET", "/api/messages/"+tlID, member, nil)
		expect(t, r, 200, "thread")
		msgs := r.body["messages"].([]any)
		if len(msgs) != 2 {
			t.Fatalf("messages: %v", msgs)
		}
		first, second := msgs[0].(map[string]any), msgs[1].(map[string]any)
		if first["body"] != "Can you take the Acme store?" || first["mine"] != false {
			t.Fatalf("first: %v", first)
		}
		if second["mine"] != true {
			t.Fatalf("second should be the member's own: %v", second)
		}
		if r.body["user"].(map[string]any)["name"] != "tl" || r.body["has_more"] != false {
			t.Fatalf("thread head: %v %v", r.body["user"], r.body["has_more"])
		}
		expect(t, e.do("POST", "/api/messages/"+tlID+"/read", member, nil), 204, "mark read")
		if n := e.do("GET", "/api/messages", member, nil).body["unread"].(float64); n != 0 {
			t.Fatalf("still unread after reading: %v", n)
		}
		// The TL now has the reply waiting.
		if n := e.do("GET", "/api/messages", tl, nil).body["unread"].(float64); n != 1 {
			t.Fatalf("tl unread = %v", n)
		}
	})

	t.Run("what is refused", func(t *testing.T) {
		expect(t, e.do("POST", "/api/messages/"+tlID, tl, map[string]any{"body": "note to self"}), 422, "to yourself")
		expect(t, e.do("POST", "/api/messages/"+memberID, tl, map[string]any{"body": "   "}), 422, "empty")
		expect(t, e.do("POST", "/api/messages/"+memberID, tl, map[string]any{"body": strings.Repeat("x", 4001)}), 422, "too long")
		expect(t, e.do("POST", "/api/messages/"+salesID+"x", tl, map[string]any{"body": "hi"}), 404, "not a person")
		// HR run people, not projects, but chat is for everyone.
		expect(t, e.do("POST", "/api/messages/"+tlID, hr, map[string]any{"body": "Please approve the leave"}), 201, "HR can message")
		// The CEO never operates a project, but talking to people is not project work.
		ceo := e.login("ceo")
		expect(t, e.do("POST", "/api/messages/"+tlID, ceo, map[string]any{"body": "Ring me when you are free"}), 201, "the CEO can message")
		expect(t, e.do("POST", "/api/messages/"+tlID+"/read", ceo, nil), 204, "the CEO can mark read")
		// Someone whose access was revoked can't be messaged.
		gone := e.addUser("gone", policy.Member, false)
		if _, err := e.store.Pool.Exec(context.Background(), `UPDATE users SET is_active = false WHERE id = $1`, gone.ID); err != nil {
			t.Fatal(err)
		}
		expect(t, e.do("POST", "/api/messages/"+gone.ID.String(), tl, map[string]any{"body": "hello?"}), 400, "deactivated")
	})
}

// Voice calls: the server only introduces the two browsers. It never carries audio,
// but it decides who may ring whom, who may answer, and where signals are allowed to go.
func TestCalls(t *testing.T) {
	e := newEnv(t)
	tl, member, sales := e.login("tl"), e.login("member"), e.login("sales")
	tlID, memberID := e.users["tl"].ID.String(), e.users["member"].ID.String()

	t.Run("ice servers are offered, with turn only when configured", func(t *testing.T) {
		r := e.do("GET", "/api/calls/ice", member, nil)
		expect(t, r, 200, "ice")
		if r.body["has_turn"] != false {
			t.Fatal("no TURN is configured in tests")
		}
		if len(r.body["ice_servers"].([]any)) == 0 {
			t.Fatal("no STUN servers offered")
		}
	})

	var callID string
	t.Run("ringing, answering and hanging up", func(t *testing.T) {
		r := e.do("POST", "/api/calls/"+memberID, tl, nil)
		expect(t, r, 201, "ring")
		c := r.body["call"].(map[string]any)
		callID = c["id"].(string)
		if c["status"] != "ringing" || c["callee_name"] != "member" || c["incoming"] != false {
			t.Fatalf("call: %v", c)
		}
		// Both ends can find it again after a reload; the callee sees it as incoming.
		live := e.do("GET", "/api/calls/live", member, nil).body["call"].(map[string]any)
		if live["id"] != callID || live["incoming"] != true {
			t.Fatalf("live for the callee: %v", live)
		}
		if e.do("GET", "/api/calls/live", sales, nil).body["call"] != nil {
			t.Fatal("a bystander is shown the call")
		}
		// Only the person being called may answer.
		expect(t, e.do("POST", "/api/calls/"+callID+"/status", tl, map[string]any{"status": "answered"}), 403, "caller answers")
		expect(t, e.do("POST", "/api/calls/"+callID+"/status", sales, map[string]any{"status": "answered"}), 404, "stranger answers")
		r = e.do("POST", "/api/calls/"+callID+"/status", member, map[string]any{"status": "answered"})
		expect(t, r, 200, "answer")
		if c := r.body["call"].(map[string]any); c["status"] != "answered" || c["answered_at"] == nil {
			t.Fatalf("answered: %v", c)
		}
		// Either end may hang up, and a second hang-up is harmless.
		expect(t, e.do("POST", "/api/calls/"+callID+"/status", tl, map[string]any{"status": "ended"}), 200, "hang up")
		r = e.do("POST", "/api/calls/"+callID+"/status", member, map[string]any{"status": "ended"})
		expect(t, r, 200, "hang up twice")
		if c := r.body["call"].(map[string]any); c["status"] != "ended" {
			t.Fatalf("after a second hang-up: %v", c)
		}
		if e.do("GET", "/api/calls/live", tl, nil).body["call"] != nil {
			t.Fatal("a finished call is still live")
		}
	})

	t.Run("signals only travel inside a live call, between its two ends", func(t *testing.T) {
		sdp, _ := json.Marshal(map[string]string{"type": "offer", "sdp": "v=0..."})
		// The call above has ended.
		expect(t, e.do("POST", "/api/calls/"+callID+"/signal", tl, map[string]any{"kind": "offer", "payload": json.RawMessage(sdp)}), 409, "ended call")
		r := e.do("POST", "/api/calls/"+memberID, tl, nil)
		expect(t, r, 201, "ring again")
		id := r.body["call"].(map[string]any)["id"].(string)
		expect(t, e.do("POST", "/api/calls/"+id+"/signal", tl, map[string]any{"kind": "offer", "payload": json.RawMessage(sdp)}), 204, "offer")
		expect(t, e.do("POST", "/api/calls/"+id+"/signal", member, map[string]any{"kind": "candidate", "payload": json.RawMessage(`{"candidate":"x"}`)}), 204, "candidate")
		expect(t, e.do("POST", "/api/calls/"+id+"/signal", sales, map[string]any{"kind": "offer", "payload": json.RawMessage(sdp)}), 404, "a stranger signalling")
		expect(t, e.do("POST", "/api/calls/"+id+"/signal", tl, map[string]any{"kind": "whatever", "payload": json.RawMessage(sdp)}), 400, "unknown signal")
		// Tidy up so the next subtest starts with nobody on a call.
		expect(t, e.do("POST", "/api/calls/"+id+"/status", member, map[string]any{"status": "declined"}), 200, "decline")
	})

	t.Run("one call at a time, and never to yourself", func(t *testing.T) {
		expect(t, e.do("POST", "/api/calls/"+tlID, tl, nil), 400, "calling yourself")
		r := e.do("POST", "/api/calls/"+memberID, tl, nil)
		expect(t, r, 201, "ring")
		id := r.body["call"].(map[string]any)["id"].(string)
		expect(t, e.do("POST", "/api/calls/"+memberID, sales, nil), 409, "the member is busy")
		expect(t, e.do("POST", "/api/calls/"+e.users["sales"].ID.String(), tl, nil), 409, "the caller is busy")
		expect(t, e.do("POST", "/api/calls/"+id+"/status", member, map[string]any{"status": "declined"}), 200, "decline")
		// Once declined, both are free again.
		expect(t, e.do("POST", "/api/calls/"+memberID, sales, nil), 201, "sales can now ring")
	})

	t.Run("the CEO may ring anyone, read-only or not", func(t *testing.T) {
		ceo := e.login("ceo")
		r := e.do("POST", "/api/calls/"+tlID, ceo, nil)
		expect(t, r, 201, "the CEO rings")
		id := r.body["call"].(map[string]any)["id"].(string)
		expect(t, e.do("POST", "/api/calls/"+id+"/status", tl, map[string]any{"status": "declined"}), 200, "declined")
	})

	t.Run("the call log shows both sides", func(t *testing.T) {
		calls := e.do("GET", "/api/calls", member, nil).body["calls"].([]any)
		if len(calls) < 3 {
			t.Fatalf("the member's log has %d calls", len(calls))
		}
		var declined, ended bool
		for _, c := range calls {
			switch c.(map[string]any)["status"] {
			case "declined":
				declined = true
			case "ended":
				ended = true
			}
			if c.(map[string]any)["incoming"] != true {
				t.Fatalf("every call here was incoming for the member: %v", c)
			}
		}
		if !declined || !ended {
			t.Fatalf("log should hold declined and ended calls: %v", calls)
		}
	})
}

// Group chat: anyone may start a named group, only its members can see or write to
// it, whoever started it renames and removes, anyone may leave, and unread counts
// follow each member's own read mark.
func TestChatGroups(t *testing.T) {
	e := newEnv(t)
	tl, member, sales, ceo := e.login("tl"), e.login("member"), e.login("sales"), e.login("ceo")
	tlID, memberID, salesID := e.users["tl"].ID.String(), e.users["member"].ID.String(), e.users["sales"].ID.String()

	var groupID string
	t.Run("anyone can start a group and everyone in it sees it", func(t *testing.T) {
		r := e.do("POST", "/api/groups", tl, map[string]any{"name": "  Ops  ", "member_ids": []string{memberID}})
		expect(t, r, 201, "create")
		g := r.body["group"].(map[string]any)
		groupID = g["id"].(string)
		if g["name"] != "Ops" || g["member_count"].(float64) != 2 || g["mine"] != true {
			t.Fatalf("group: %v", g)
		}
		// The member it was started with has it too, without being told twice.
		mine := e.do("GET", "/api/groups", member, nil).body["groups"].([]any)
		if len(mine) != 1 || mine[0].(map[string]any)["id"] != groupID || mine[0].(map[string]any)["mine"] != false {
			t.Fatalf("the member's groups: %v", mine)
		}
		// Sales were not invited, so for them it does not exist.
		if len(e.do("GET", "/api/groups", sales, nil).body["groups"].([]any)) != 0 {
			t.Fatal("sales can see a group they are not in")
		}
		expect(t, e.do("GET", "/api/groups/"+groupID, sales, nil), 404, "outsider reads")
		expect(t, e.do("POST", "/api/groups/"+groupID, sales, map[string]any{"body": "hello"}), 404, "outsider writes")
	})

	t.Run("messages reach everyone in the group and count as unread once", func(t *testing.T) {
		expect(t, e.do("POST", "/api/groups/"+groupID, tl, map[string]any{"body": "  Stand-up at 10  "}), 201, "send")
		r := e.do("GET", "/api/groups/"+groupID, member, nil)
		expect(t, r, 200, "thread")
		msgs := r.body["messages"].([]any)
		if len(msgs) != 1 || msgs[0].(map[string]any)["body"] != "Stand-up at 10" {
			t.Fatalf("messages: %v", msgs)
		}
		if msgs[0].(map[string]any)["mine"] != false || msgs[0].(map[string]any)["sender_name"] != "tl" {
			t.Fatalf("who sent it: %v", msgs[0])
		}
		if len(r.body["members"].([]any)) != 2 {
			t.Fatalf("members: %v", r.body["members"])
		}
		// Waiting for the member, not for the person who wrote it.
		if n := groupUnread(t, e, member, groupID); n != 1 {
			t.Fatalf("the member's unread = %v", n)
		}
		if n := groupUnread(t, e, tl, groupID); n != 0 {
			t.Fatalf("the sender's own message is unread: %v", n)
		}
		// The sidebar badge counts groups too.
		if e.do("GET", "/api/messages", member, nil).body["unread"].(float64) < 1 {
			t.Fatal("the inbox badge ignores groups")
		}
		expect(t, e.do("POST", "/api/groups/"+groupID+"/read", member, nil), 204, "mark read")
		if n := groupUnread(t, e, member, groupID); n != 0 {
			t.Fatalf("still unread after reading: %v", n)
		}
	})

	t.Run("any member brings people in; only the owner renames and removes", func(t *testing.T) {
		// The member adds Sales, who then sees the history of the room they joined.
		expect(t, e.do("POST", "/api/groups/"+groupID+"/members", member, map[string]any{"user_ids": []string{salesID}}), 204, "add")
		r := e.do("GET", "/api/groups/"+groupID, sales, nil)
		expect(t, r, 200, "sales read it now")
		if len(r.body["messages"].([]any)) != 1 {
			t.Fatalf("sales joined to an empty room: %v", r.body["messages"])
		}
		expect(t, e.do("PATCH", "/api/groups/"+groupID, member, map[string]any{"name": "Not mine"}), 403, "a member renames")
		expect(t, e.do("PATCH", "/api/groups/"+groupID, tl, map[string]any{"name": "Ops team"}), 200, "the owner renames")
		expect(t, e.do("PATCH", "/api/groups/"+groupID, tl, map[string]any{"name": "   "}), 422, "an empty name")
		expect(t, e.do("DELETE", "/api/groups/"+groupID+"/members/"+memberID, sales, nil), 403, "removing someone else")
		expect(t, e.do("DELETE", "/api/groups/"+groupID+"/members/"+salesID, sales, nil), 204, "leaving")
		expect(t, e.do("GET", "/api/groups/"+groupID, sales, nil), 404, "after leaving")
		expect(t, e.do("DELETE", "/api/groups/"+groupID+"/members/"+memberID, tl, nil), 204, "the owner removes")
	})

	t.Run("the CEO may start a group too, and the last one out closes it", func(t *testing.T) {
		r := e.do("POST", "/api/groups", ceo, map[string]any{"name": "Leads", "member_ids": []string{tlID}})
		expect(t, r, 201, "the CEO starts a group")
		id := r.body["group"].(map[string]any)["id"].(string)
		expect(t, e.do("POST", "/api/groups/"+id, ceo, map[string]any{"body": "Numbers please"}), 201, "the CEO writes")
		expect(t, e.do("DELETE", "/api/groups/"+id+"/members/"+tlID, tl, nil), 204, "the TL leaves")
		expect(t, e.do("DELETE", "/api/groups/"+id+"/members/"+e.users["ceo"].ID.String(), ceo, nil), 204, "the last one leaves")
		// Gone for good: not even its owner can find it.
		expect(t, e.do("GET", "/api/groups/"+id, ceo, nil), 404, "the empty group")
	})

	t.Run("what is refused", func(t *testing.T) {
		expect(t, e.do("POST", "/api/groups", tl, map[string]any{"name": "   "}), 422, "no name")
		expect(t, e.do("POST", "/api/groups", tl, map[string]any{"name": strings.Repeat("x", 81)}), 422, "name too long")
		expect(t, e.do("POST", "/api/groups", tl, map[string]any{"name": "Ghosts", "member_ids": []string{uuid.NewString()}}), 404, "a stranger")
		expect(t, e.do("POST", "/api/groups/"+groupID, tl, map[string]any{"body": "   "}), 422, "an empty message")
		expect(t, e.do("GET", "/api/groups/not-a-uuid", tl, nil), 404, "not a group")
	})
}

// groupUnread reads what this person has waiting in one group.
func groupUnread(t *testing.T, e *env, as, group string) float64 {
	t.Helper()
	for _, g := range e.do("GET", "/api/groups", as, nil).body["groups"].([]any) {
		if g.(map[string]any)["id"] == group {
			return g.(map[string]any)["unread"].(float64)
		}
	}
	t.Fatalf("group %s is not in that person's list", group)
	return 0
}

// Files in chat: uploaded to storage first, then sent with (or instead of) text, only
// readable by the people in the conversation. Finished calls show in the thread.
func TestChatAttachmentsAndCallHistory(t *testing.T) {
	e := newEnv(t)
	tl, member, sales := e.login("tl"), e.login("member"), e.login("sales")
	tlID, memberID := e.users["tl"].ID.String(), e.users["member"].ID.String()
	content := []byte("hello from a file")
	ticket := func(cookie string, size int) map[string]any {
		r := e.do("POST", "/api/chat/uploads", cookie, map[string]any{"filename": "brief.txt", "size": size, "content_type": "text/plain"})
		expect(t, r, 201, "upload ticket")
		return r.body
	}

	var attID string
	t.Run("a file-only message reaches the other person", func(t *testing.T) {
		tk := ticket(tl, len(content))
		putToStorage(t, tk, content)
		r := e.do("POST", "/api/messages/"+memberID, tl, map[string]any{"body": "", "attachments": []any{tk["upload_id"]}})
		expect(t, r, 201, "send file only")
		atts := r.body["message"].(map[string]any)["attachments"].([]any)
		if len(atts) != 1 || atts[0].(map[string]any)["name"] != "brief.txt" {
			t.Fatalf("attachments: %v", atts)
		}
		attID = atts[0].(map[string]any)["id"].(string)
		th := e.do("GET", "/api/messages/"+tlID, member, nil)
		msgs := th.body["messages"].([]any)
		if len(msgs) != 1 || len(msgs[0].(map[string]any)["attachments"].([]any)) != 1 {
			t.Fatalf("thread: %v", msgs)
		}
	})

	t.Run("only the two people can download it", func(t *testing.T) {
		dl := e.raw("GET", "/api/chat/attachments/"+attID, member)
		if dl.StatusCode != 302 {
			t.Fatalf("member download: %d", dl.StatusCode)
		}
		if got := fetch(t, dl.Header.Get("Location")); string(got) != string(content) {
			t.Fatalf("content = %q", got)
		}
		if dl := e.raw("GET", "/api/chat/attachments/"+attID, sales); dl.StatusCode != 404 {
			t.Fatalf("outsider download: %d", dl.StatusCode)
		}
	})

	t.Run("uploads are checked and used once", func(t *testing.T) {
		expect(t, e.do("POST", "/api/messages/"+memberID, tl, map[string]any{"body": ""}), 422, "empty message")
		tk := ticket(tl, len(content))
		expect(t, e.do("POST", "/api/messages/"+memberID, tl, map[string]any{"attachments": []any{tk["upload_id"]}}), 409, "never uploaded")
		tk = ticket(tl, len(content)+5)
		putToStorage(t, tk, content)
		expect(t, e.do("POST", "/api/messages/"+memberID, tl, map[string]any{"attachments": []any{tk["upload_id"]}}), 422, "size mismatch")
		tk = ticket(tl, len(content))
		putToStorage(t, tk, content)
		expect(t, e.do("POST", "/api/messages/"+memberID, member, map[string]any{"attachments": []any{tk["upload_id"]}}), 422, "someone else's upload")
		expect(t, e.do("POST", "/api/chat/uploads", tl, map[string]any{"filename": "run.exe", "size": 10, "content_type": "application/octet-stream"}), 422, "blocked type")
	})

	t.Run("group messages carry files for members only", func(t *testing.T) {
		r := e.do("POST", "/api/groups", tl, map[string]any{"name": "Ops", "member_ids": []any{memberID}})
		expect(t, r, 201, "group")
		gid := r.body["group"].(map[string]any)["id"].(string)
		tk := ticket(member, len(content))
		putToStorage(t, tk, content)
		r = e.do("POST", "/api/groups/"+gid, member, map[string]any{"body": "the brief", "attachments": []any{tk["upload_id"]}})
		expect(t, r, 201, "group send")
		gatt := r.body["message"].(map[string]any)["attachments"].([]any)[0].(map[string]any)["id"].(string)
		msgs := e.do("GET", "/api/groups/"+gid, tl, nil).body["messages"].([]any)
		if len(msgs[len(msgs)-1].(map[string]any)["attachments"].([]any)) != 1 {
			t.Fatalf("group thread: %v", msgs)
		}
		if dl := e.raw("GET", "/api/chat/attachments/"+gatt, tl); dl.StatusCode != 302 {
			t.Fatalf("member download: %d", dl.StatusCode)
		}
		if dl := e.raw("GET", "/api/chat/attachments/"+gatt, sales); dl.StatusCode != 404 {
			t.Fatalf("non-member download: %d", dl.StatusCode)
		}
	})

	t.Run("finished calls show in the conversation, live ones don't", func(t *testing.T) {
		r := e.do("POST", "/api/calls/"+memberID, tl, nil)
		expect(t, r, 201, "ring")
		callID := r.body["call"].(map[string]any)["id"].(string)
		if calls := e.do("GET", "/api/messages/"+memberID, tl, nil).body["calls"].([]any); len(calls) != 0 {
			t.Fatalf("a ringing call is in the history: %v", calls)
		}
		expect(t, e.do("POST", "/api/calls/"+callID+"/status", member, map[string]any{"status": "declined"}), 200, "decline")
		calls := e.do("GET", "/api/messages/"+memberID, tl, nil).body["calls"].([]any)
		if len(calls) != 1 || calls[0].(map[string]any)["status"] != "declined" || calls[0].(map[string]any)["incoming"] != false {
			t.Fatalf("caller's history: %v", calls)
		}
		if c := e.do("GET", "/api/messages/"+tlID, member, nil).body["calls"].([]any); len(c) != 1 || c[0].(map[string]any)["incoming"] != true {
			t.Fatalf("callee's history: %v", c)
		}
		if c := e.do("GET", "/api/messages/"+tlID, sales, nil).body["calls"].([]any); len(c) != 0 {
			t.Fatalf("outsider sees the pair's calls: %v", c)
		}
	})
}
