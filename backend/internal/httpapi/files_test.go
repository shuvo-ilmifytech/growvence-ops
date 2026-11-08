package httpapi

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"io"
	"net/http"
	"testing"
	"time"

	"github.com/solvinx/opsflow/internal/db"
	"github.com/solvinx/opsflow/internal/testutil"
)

// putToStorage performs the browser's half of an upload: PUT to the presigned URL.
func putToStorage(t *testing.T, ticket map[string]any, body []byte) {
	t.Helper()
	req, _ := http.NewRequest("PUT", ticket["url"].(string), bytes.NewReader(body))
	for k, v := range ticket["headers"].(map[string]any) {
		req.Header.Set(k, v.(string))
	}
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("PUT to storage: %v", err)
	}
	res.Body.Close()
	if res.StatusCode != 200 {
		t.Fatalf("PUT to storage: status %d", res.StatusCode)
	}
}

func TestFilesAcknowledgementsAndUnread(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	e.addUser("sales2", "sales", false)
	e.addUser("outsider", "member", false)
	profile := testutil.Must(e.store.CreateProfile(ctx, db.CreateProfileParams{Name: "Studio Main"}))
	ptype := testutil.Must(e.store.CreateProjectType(ctx, db.CreateProjectTypeParams{Name: "Website"}))
	sales, sales2, tl, mem, mgmt := e.login("sales"), e.login("sales2"), e.login("tl"), e.login("member"), e.login("ceo")
	outsider := e.login("outsider")
	future := time.Now().AddDate(0, 1, 0).Format(time.DateOnly)

	r := e.do("POST", "/api/projects", sales, map[string]any{
		"name": "Files test", "profile_id": profile.ID, "client_name": "C", "order_id": "F-1",
		"project_type_id": ptype.ID, "scope": "Scope v1", "agreed_price": 100, "deadline": future,
	})
	expect(t, r, 201, "create")
	id := projectOf(r)["id"].(string)
	path := "/api/projects/" + id
	expect(t, e.do("POST", path+"/assign", tl, map[string]any{"assignee_id": e.users["member"].ID, "priority": "medium"}), 200, "assign")

	upload := func(cookie string, body map[string]any) resp {
		return e.do("POST", path+"/files/uploads", cookie, body)
	}
	file := map[string]any{"filename": "brief.pdf", "size": 11, "content_type": "application/pdf"}

	t.Run("upload validation and permissions", func(t *testing.T) {
		expect(t, upload(sales, map[string]any{"filename": "setup.exe", "size": 10, "content_type": "application/octet-stream"}), 422, "exe")
		expect(t, upload(sales, map[string]any{"filename": "big.zip", "size": 6 << 20, "content_type": "application/zip"}), 422, "too big")
		expect(t, upload(sales, map[string]any{"filename": "a.txt", "size": 0}), 422, "empty")
		expect(t, upload(sales, map[string]any{"filename": "a.txt", "size": 1, "content_type": "not a type;;"}), 422, "bad type")
		expect(t, upload(mem, file), 201, "the assigned member uploads project files")
		expect(t, upload(outsider, file), 404, "a member not on the project doesn't see it")
		expect(t, upload(tl, file), 403, "a tl who isn't working the project can't upload")
		expect(t, upload(mgmt, file), 403, "management can't upload")
		expect(t, upload(sales2, file), 403, "other sales see the project but only its owner changes files")
	})

	v1 := []byte("hello brief")
	r = upload(sales, file)
	expect(t, r, 201, "init upload")
	ticket := r.body
	putToStorage(t, ticket, v1)
	r = e.do("POST", path+"/files/uploads/"+ticket["upload_id"].(string)+"/complete", sales, nil)
	expect(t, r, 200, "complete")
	expect(t, e.do("POST", path+"/files/uploads/"+ticket["upload_id"].(string)+"/complete", sales, nil), 404, "complete twice")

	files := r.body["files"].([]any)
	if len(files) != 1 {
		t.Fatalf("files = %v", files)
	}
	fileID := files[0].(map[string]any)["id"].(string)
	sum := sha256.Sum256(v1)
	if got := r.body["versions"].([]any)[0].(map[string]any)["sha256"]; got != hex.EncodeToString(sum[:]) {
		t.Fatalf("sha256 = %v", got)
	}

	t.Run("new version keeps the old one", func(t *testing.T) {
		v2 := []byte("hello brief v2!")
		r := upload(sales, map[string]any{"filename": "brief-final.pdf", "size": len(v2), "content_type": "application/pdf", "replace_file_id": fileID})
		expect(t, r, 201, "init v2")
		putToStorage(t, r.body, v2)
		r = e.do("POST", path+"/files/uploads/"+r.body["upload_id"].(string)+"/complete", sales, nil)
		expect(t, r, 200, "complete v2")
		f := r.body["files"].([]any)[0].(map[string]any)
		if f["version_no"].(float64) != 2 || f["version_count"].(float64) != 2 || f["original_name"] != "brief-final.pdf" {
			t.Fatalf("after v2: %v", f)
		}
		versions := r.body["versions"].([]any)
		if len(versions) != 2 {
			t.Fatalf("versions = %v", versions)
		}
		// The assigned member can download both versions; each has its own content.
		for i, want := range [][]byte{v2, v1} {
			vid := versions[i].(map[string]any)["id"].(string)
			dl := e.raw("GET", "/api/files/versions/"+vid+"/download", mem)
			if dl.StatusCode != 302 {
				t.Fatalf("download v%d: %d", 2-i, dl.StatusCode)
			}
			got := fetch(t, dl.Header.Get("Location"))
			if !bytes.Equal(got, want) {
				t.Fatalf("download v%d content = %q", 2-i, got)
			}
			if dl := e.raw("GET", "/api/files/versions/"+vid+"/download", sales2); dl.StatusCode != 302 {
				t.Fatalf("other sales (one team) should download: %d", dl.StatusCode)
			}
		}
	})

	t.Run("size mismatch and missing upload are rejected", func(t *testing.T) {
		r := upload(sales, map[string]any{"filename": "x.txt", "size": 10, "content_type": "text/plain"})
		putToStorage(t, r.body, []byte("short"))
		expect(t, e.do("POST", path+"/files/uploads/"+r.body["upload_id"].(string)+"/complete", sales, nil), 422, "size mismatch")
		r = upload(sales, map[string]any{"filename": "y.txt", "size": 3, "content_type": "text/plain"})
		expect(t, e.do("POST", path+"/files/uploads/"+r.body["upload_id"].(string)+"/complete", sales, nil), 409, "never uploaded")
		r = upload(sales, map[string]any{"filename": "z.txt", "size": 3, "content_type": "text/plain"})
		expect(t, e.do("POST", path+"/files/uploads/"+r.body["upload_id"].(string)+"/complete", sales2, nil), 404, "someone else's upload")
	})

	t.Run("the assigned member adds and replaces files, and removes only their own", func(t *testing.T) {
		perms := e.do("GET", path, mem, nil).body["permissions"].(map[string]any)
		if perms["can_upload"] != true || perms["can_remove_files"] != false {
			t.Fatalf("member permissions = %v", perms)
		}
		body := []byte("wireframes")
		r := upload(mem, map[string]any{"filename": "wireframes.pdf", "size": len(body), "content_type": "application/pdf"})
		expect(t, r, 201, "member init upload")
		putToStorage(t, r.body, body)
		r = e.do("POST", path+"/files/uploads/"+r.body["upload_id"].(string)+"/complete", mem, nil)
		expect(t, r, 200, "member completes upload")
		var own string
		for _, raw := range r.body["files"].([]any) {
			if f := raw.(map[string]any); f["original_name"] == "wireframes.pdf" {
				own = f["id"].(string)
			}
		}
		if own == "" {
			t.Fatalf("member's file not listed: %v", r.body["files"])
		}
		// A new version of the Sales brief keeps the old one.
		v3 := []byte("brief from the team")
		r = upload(mem, map[string]any{"filename": "brief-team.pdf", "size": len(v3), "content_type": "application/pdf", "replace_file_id": fileID})
		expect(t, r, 201, "member replaces the brief")
		putToStorage(t, r.body, v3)
		expect(t, e.do("POST", path+"/files/uploads/"+r.body["upload_id"].(string)+"/complete", mem, nil), 200, "member completes the new version")
		expect(t, e.do("DELETE", path+"/files/"+own, mem, nil), 200, "member removes their own file")
	})

	t.Run("remove keeps history but hides the file", func(t *testing.T) {
		expect(t, e.do("DELETE", path+"/files/"+fileID, mem, nil), 403, "member removes the Sales brief")
		r := e.do("DELETE", path+"/files/"+fileID, sales, nil)
		expect(t, r, 200, "remove")
		if len(r.body["files"].([]any)) != 0 {
			t.Fatal("file still listed")
		}
		var fields []string
		for _, c := range r.body["changes"].([]any) {
			fields = append(fields, c.(map[string]any)["field"].(string))
		}
		want := []string{"file_added", "file_replaced", "file_removed"}
		for _, w := range want {
			found := false
			for _, f := range fields {
				found = found || f == w
			}
			if !found {
				t.Errorf("change %s not recorded (have %v)", w, fields)
			}
		}
	})

	t.Run("important changes need the member's acknowledgement", func(t *testing.T) {
		expect(t, e.do("PATCH", path, sales, map[string]any{"scope": "Scope v2", "client_contact": "x@y.z"}), 200, "edit")
		later := time.Now().AddDate(0, 2, 0).Format(time.DateOnly)
		expect(t, e.do("PATCH", path, sales, map[string]any{"deadline": later}), 200, "edit deadline")

		d := e.do("GET", path, mem, nil).body
		if d["pending_acks"].(float64) != 2 || d["permissions"].(map[string]any)["can_acknowledge"] != true {
			t.Fatalf("member pending acks = %v perms = %v", d["pending_acks"], d["permissions"])
		}
		acks := e.do("GET", path, tl, nil).body["acknowledgements"].([]any)
		if len(acks) != 2 || acks[0].(map[string]any)["acknowledged_at"] != nil || acks[0].(map[string]any)["user_name"] != "member" {
			t.Fatalf("tl sees acks = %v", acks)
		}
		expect(t, e.do("POST", path+"/acknowledge", tl, nil), 403, "tl can't acknowledge for the member")
		expect(t, e.do("POST", path+"/acknowledge", mgmt, nil), 403, "mgmt")
		r := e.do("POST", path+"/acknowledge", mem, nil)
		expect(t, r, 200, "member acknowledges")
		if r.body["pending_acks"].(float64) != 0 {
			t.Fatalf("still pending: %v", r.body["pending_acks"])
		}
		expect(t, e.do("POST", path+"/acknowledge", mem, nil), 409, "nothing left")
		for _, a := range e.do("GET", path, mgmt, nil).body["acknowledgements"].([]any) {
			if a.(map[string]any)["acknowledged_at"] == nil {
				t.Fatal("management should see the acknowledgement time")
			}
		}
	})

	t.Run("unread marker tracks other people's changes", func(t *testing.T) {
		hasUpdates := func(cookie string) bool {
			for _, p := range e.do("GET", "/api/projects", cookie, nil).body["projects"].([]any) {
				if p.(map[string]any)["id"] == id {
					return p.(map[string]any)["has_updates"].(bool)
				}
			}
			t.Fatal("project not in list")
			return false
		}
		if !hasUpdates(tl) {
			t.Fatal("TL hasn't opened it since Sales edited: should be unread")
		}
		r := e.do("POST", path+"/view", tl, nil)
		expect(t, r, 200, "view")
		if r.body["previous_viewed_at"] != nil {
			t.Fatalf("first view should have no previous: %v", r.body)
		}
		if hasUpdates(tl) {
			t.Fatal("still unread after opening")
		}
		expect(t, e.do("POST", path+"/comments", tl, map[string]any{"body": "my own note"}), 201, "own comment")
		if hasUpdates(tl) {
			t.Fatal("your own activity must not mark a project unread")
		}
		time.Sleep(10 * time.Millisecond)
		expect(t, e.do("POST", path+"/comments", sales, map[string]any{"body": "client replied"}), 201, "sales comment")
		if !hasUpdates(tl) {
			t.Fatal("someone else's comment should mark it unread")
		}
		if r := e.do("POST", path+"/view", tl, nil); r.body["previous_viewed_at"] == nil {
			t.Fatal("second view should return the previous visit time")
		}
		expect(t, e.do("POST", path+"/view", sales2, nil), 200, "other sales see submitted projects")
	})

	t.Run("files freeze when the project is cancelled", func(t *testing.T) {
		expect(t, e.do("POST", path+"/transition", sales, map[string]any{"to": "cancelled"}), 200, "cancel")
		expect(t, upload(sales, file), 409, "upload after cancel")
		expect(t, upload(mem, file), 409, "member upload after cancel")
	})
}

// raw performs a request without following redirects.
func (e *env) raw(method, path, cookie string) *http.Response {
	e.t.Helper()
	req, _ := http.NewRequest(method, path, nil)
	req.Header.Set("Origin", testOrigin)
	req.AddCookie(&http.Cookie{Name: sessionCookie, Value: cookie})
	res, err := e.app.Test(req)
	if err != nil {
		e.t.Fatal(err)
	}
	return res
}

func fetch(t *testing.T, url string) []byte {
	t.Helper()
	res, err := http.Get(url)
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	if res.StatusCode != 200 {
		t.Fatalf("fetch %s: %d", url, res.StatusCode)
	}
	b, _ := io.ReadAll(res.Body)
	return b
}
