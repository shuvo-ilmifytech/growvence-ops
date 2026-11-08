package httpapi

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"net/url"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/gofiber/fiber/v3"

	"github.com/solvinx/opsflow/internal/db"
	"github.com/solvinx/opsflow/internal/mcp"
	"github.com/solvinx/opsflow/internal/policy"
	"github.com/solvinx/opsflow/internal/testutil"
)

// send makes a request the way an assistant does: no portal Origin, no session cookie.
func (e *env) send(method, path string, header map[string]string, body string) (int, http.Header, []byte) {
	e.t.Helper()
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	for k, v := range header {
		req.Header.Set(k, v)
	}
	res, err := e.app.Test(req, fiber.TestConfig{Timeout: 10 * time.Second})
	if err != nil {
		e.t.Fatalf("%s %s: %v", method, path, err)
	}
	defer res.Body.Close()
	b, _ := io.ReadAll(res.Body)
	return res.StatusCode, res.Header, b
}

type rpcReply struct {
	Result map[string]any `json:"result"`
	Error  *struct {
		Code    int    `json:"code"`
		Message string `json:"message"`
		Data    any    `json:"data"`
	} `json:"error"`
}

// legacyCall is an initialize-era request (2025-06-18): version only in the header.
func (e *env) legacyCall(token, method string, params any) (int, rpcReply) {
	e.t.Helper()
	body, _ := json.Marshal(map[string]any{"jsonrpc": "2.0", "id": 1, "method": method, "params": params})
	status, _, raw := e.send("POST", "/mcp", map[string]string{
		"Authorization": "Bearer " + token, "Content-Type": "application/json",
		"Accept": "application/json, text/event-stream", "MCP-Protocol-Version": "2025-06-18",
	}, string(body))
	var r rpcReply
	_ = json.Unmarshal(raw, &r)
	return status, r
}

// modernCall is a 2026-07-28 request: per-request _meta, mirrored into headers.
func (e *env) modernCall(token, method string, params map[string]any, headers map[string]string) (int, rpcReply) {
	e.t.Helper()
	if params == nil {
		params = map[string]any{}
	}
	params["_meta"] = map[string]any{
		"io.modelcontextprotocol/protocolVersion":    mcp.ModernVersion,
		"io.modelcontextprotocol/clientInfo":         map[string]any{"name": "test", "version": "1"},
		"io.modelcontextprotocol/clientCapabilities": map[string]any{},
	}
	body, _ := json.Marshal(map[string]any{"jsonrpc": "2.0", "id": "x", "method": method, "params": params})
	h := map[string]string{
		"Authorization": "Bearer " + token, "Content-Type": "application/json", "Accept": "application/json, text/event-stream",
		"MCP-Protocol-Version": mcp.ModernVersion, "Mcp-Method": method,
	}
	if n, ok := params["name"].(string); ok {
		h["Mcp-Name"] = n
	}
	for k, v := range headers {
		if v == "" {
			delete(h, k)
		} else {
			h[k] = v
		}
	}
	status, _, raw := e.send("POST", "/mcp", h, string(body))
	var r rpcReply
	_ = json.Unmarshal(raw, &r)
	return status, r
}

// tool calls a tool (legacy era) and returns its structured result, or its error text.
func (e *env) tool(token, name string, args map[string]any) (map[string]any, string) {
	e.t.Helper()
	status, r := e.legacyCall(token, "tools/call", map[string]any{"name": name, "arguments": args})
	if status != 200 {
		e.t.Fatalf("tools/call %s: status %d", name, status)
	}
	if r.Error != nil {
		return nil, fmt.Sprintf("rpc %d: %s", r.Error.Code, r.Error.Message)
	}
	if r.Result["isError"] == true {
		return nil, r.Result["content"].([]any)[0].(map[string]any)["text"].(string)
	}
	return r.Result["structuredContent"].(map[string]any), ""
}

func toolNames(r rpcReply) []string {
	out := []string{}
	for _, t := range r.Result["tools"].([]any) {
		out = append(out, t.(map[string]any)["name"].(string))
	}
	return out
}

func (e *env) personalToken(cookie, name string) string {
	e.t.Helper()
	r := e.do("POST", "/api/account/assistant-tokens", cookie, map[string]any{"name": name})
	expect(e.t, r, 201, "create token "+name)
	return r.body["token"].(string)
}

func TestMCP(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	e.addUser("tl2", policy.TL, false)
	e.addUser("member2", policy.Member, false)
	profile := testutil.Must(e.store.CreateProfile(ctx, db.CreateProfileParams{Name: "Studio Main", Platform: "Fiverr"}))
	hr, admin, sales, ceo, tlS, memS, mem2S := e.login("hr"), e.login("admin"), e.login("sales"), e.login("ceo"), e.login("tl"), e.login("member"), e.login("member2")
	id := func(r resp, key string) string { return r.body[key].(map[string]any)["id"].(string) }

	// Shopify team (tl, member) and Web team (tl2, member2), one project each, assigned.
	r := e.do("POST", "/api/admin/teams", hr, map[string]any{"name": "Shopify", "lead_id": e.users["tl"].ID})
	shopify := id(r, "team")
	r = e.do("POST", "/api/admin/teams", hr, map[string]any{"name": "Web", "lead_id": e.users["tl2"].ID})
	web := id(r, "team")
	expect(t, e.do("PUT", "/api/admin/teams/"+shopify+"/members/"+e.users["member"].ID.String(), hr, nil), 204, "member")
	expect(t, e.do("PUT", "/api/admin/teams/"+web+"/members/"+e.users["member2"].ID.String(), hr, nil), 204, "member2")
	sType := id(e.do("POST", "/api/admin/project-types", admin, map[string]any{"name": "Store", "team_id": shopify}), "project_type")
	wType := id(e.do("POST", "/api/admin/project-types", admin, map[string]any{"name": "Site", "team_id": web}), "project_type")
	n := 0
	project := func(typ string, lead string, who string) (string, float64) {
		n++
		r := e.do("POST", "/api/projects", sales, map[string]any{
			"name": fmt.Sprintf("Project %d", n), "profile_id": profile.ID, "client_name": "Acme", "order_id": fmt.Sprintf("FO-%d", n),
			"project_type_id": typ, "scope": "Build it", "agreed_price": "100", "deadline": time.Now().AddDate(0, 1, 0).Format(time.DateOnly),
		})
		p := projectOf(r)
		pid := p["id"].(string)
		expect(t, e.do("POST", "/api/projects/"+pid+"/assign", lead, map[string]any{"assignee_id": e.users[who].ID, "priority": "high"}), 200, "assign")
		return pid, p["number"].(float64)
	}
	_, shopNum := project(sType, tlS, "member")
	_, webNum := project(wType, e.login("tl2"), "member2")

	memTok := e.personalToken(memS, "Claude Code")

	t.Run("no token: 401 that points at the protected resource metadata", func(t *testing.T) {
		status, h, _ := e.send("POST", "/mcp", map[string]string{"Content-Type": "application/json"}, `{"jsonrpc":"2.0","id":1,"method":"ping"}`)
		if status != 401 || !strings.Contains(h.Get("WWW-Authenticate"), `resource_metadata="http://app.test/.well-known/oauth-protected-resource/mcp"`) {
			t.Fatalf("status %d, challenge %q", status, h.Get("WWW-Authenticate"))
		}
		status, h, _ = e.send("POST", "/mcp", map[string]string{"Authorization": "Bearer ofk_nope"}, `{}`)
		if status != 401 || !strings.Contains(h.Get("WWW-Authenticate"), `invalid_token`) {
			t.Fatalf("bad token: %d %q", status, h.Get("WWW-Authenticate"))
		}
		if status, _, _ := e.send("GET", "/mcp", nil, ""); status != 405 {
			t.Fatalf("GET /mcp = %d", status)
		}
	})

	t.Run("a foreign Origin is refused", func(t *testing.T) {
		status, _, _ := e.send("POST", "/mcp", map[string]string{"Authorization": "Bearer " + memTok, "Origin": "https://evil.example"},
			`{"jsonrpc":"2.0","id":1,"method":"ping"}`)
		if status != 403 {
			t.Fatalf("status %d", status)
		}
	})

	t.Run("legacy handshake", func(t *testing.T) {
		status, r := e.legacyCall(memTok, "initialize", map[string]any{"protocolVersion": "2025-06-18", "capabilities": map[string]any{}, "clientInfo": map[string]any{"name": "t", "version": "1"}})
		if status != 200 || r.Result["protocolVersion"] != "2025-06-18" || r.Result["capabilities"].(map[string]any)["tools"] == nil {
			t.Fatalf("initialize: %d %v", status, r)
		}
		status, _, body := e.send("POST", "/mcp", map[string]string{"Authorization": "Bearer " + memTok, "Content-Type": "application/json"},
			`{"jsonrpc":"2.0","method":"notifications/initialized"}`)
		if status != 202 || len(body) != 0 {
			t.Fatalf("notification: %d %q", status, body)
		}
	})

	t.Run("modern requests: discover, header checks, version errors", func(t *testing.T) {
		status, r := e.modernCall(memTok, "server/discover", nil, nil)
		if status != 200 || r.Result["resultType"] != "complete" || r.Result["supportedVersions"] == nil {
			t.Fatalf("discover: %d %v", status, r)
		}
		status, r = e.modernCall(memTok, "tools/list", nil, map[string]string{"Mcp-Method": ""})
		if status != 400 || r.Error == nil || r.Error.Code != -32020 {
			t.Fatalf("missing Mcp-Method: %d %v", status, r.Error)
		}
		status, r = e.modernCall(memTok, "tools/call", map[string]any{"name": "whoami", "arguments": map[string]any{}}, map[string]string{"Mcp-Name": "my_work"})
		if status != 400 || r.Error == nil || r.Error.Code != -32020 {
			t.Fatalf("Mcp-Name mismatch: %d %v", status, r.Error)
		}
		status, r = e.modernCall(memTok, "tools/call", map[string]any{"name": "whoami", "arguments": map[string]any{}}, nil)
		if status != 200 || r.Result["structuredContent"].(map[string]any)["name"] != "member" {
			t.Fatalf("modern whoami: %d %v", status, r)
		}
		status, r = e.modernCall(memTok, "nope/nope", nil, nil)
		if status != 404 || r.Error.Code != -32601 {
			t.Fatalf("unknown method: %d %v", status, r.Error)
		}
		body := `{"jsonrpc":"2.0","id":1,"method":"tools/list","params":{"_meta":{"io.modelcontextprotocol/protocolVersion":"1900-01-01","io.modelcontextprotocol/clientCapabilities":{}}}}`
		status, _, raw := e.send("POST", "/mcp", map[string]string{"Authorization": "Bearer " + memTok, "MCP-Protocol-Version": "1900-01-01", "Mcp-Method": "tools/list"}, body)
		if status != 400 || !strings.Contains(string(raw), "-32022") {
			t.Fatalf("unsupported version: %d %s", status, raw)
		}
	})

	t.Run("each role gets the tools the portal allows", func(t *testing.T) {
		names := func(tok string) string {
			_, r := e.legacyCall(tok, "tools/list", map[string]any{})
			return strings.Join(toolNames(r), ",")
		}
		mem := names(memTok)
		if !strings.Contains(mem, "my_work") || !strings.Contains(mem, "move_project") || strings.Contains(mem, "team_workload") ||
			strings.Contains(mem, "company_overview") || strings.Contains(mem, "assign_project") {
			t.Fatalf("member tools: %s", mem)
		}
		ceoTools := names(e.personalToken(ceo, "CEO's Claude"))
		if !strings.Contains(ceoTools, "company_overview") || strings.Contains(ceoTools, "move_project") || strings.Contains(ceoTools, "draft_client_update") {
			t.Fatalf("CEO tools (read-only): %s", ceoTools)
		}
		tlTools := names(e.personalToken(tlS, "TL Claude"))
		if !strings.Contains(tlTools, "assign_project") || !strings.Contains(tlTools, "team_workload") || strings.Contains(tlTools, "company_overview") {
			t.Fatalf("TL tools: %s", tlTools)
		}
		r := e.do("POST", "/api/account/assistant-tokens", hr, map[string]any{"name": "HR bot"})
		expect(t, r, 400, "HR has no project tools to connect")
	})

	t.Run("reads are scoped like the portal", func(t *testing.T) {
		out, msg := e.tool(memTok, "my_work", nil)
		if msg != "" {
			t.Fatal(msg)
		}
		wait := out["waiting_on_you"].(map[string]any)["projects"].([]any)
		if len(wait) != 1 || wait[0].(map[string]any)["number"].(float64) != shopNum {
			t.Fatalf("my_work: %v", wait)
		}
		if _, msg := e.tool(memTok, "get_project", map[string]any{"project": fmt.Sprintf("#%d", int(shopNum))}); msg != "" {
			t.Fatalf("own project: %s", msg)
		}
		if _, msg := e.tool(memTok, "get_project", map[string]any{"project": fmt.Sprint(int(webNum))}); !strings.Contains(msg, "not found") {
			t.Fatalf("another team's project should be not found, got %q", msg)
		}
		if _, msg := e.tool(memTok, "team_workload", nil); !strings.Contains(msg, "Unknown tool") {
			t.Fatalf("member workload: %q", msg)
		}
		ceoTok := e.personalToken(ceo, "CEO 2")
		if _, msg := e.tool(ceoTok, "move_project", map[string]any{"project": fmt.Sprint(int(shopNum)), "to": "in_progress"}); !strings.Contains(msg, "Unknown tool") {
			t.Fatalf("CEO write: %q", msg)
		}
		if out, msg := e.tool(ceoTok, "company_overview", nil); msg != "" || out["kpis"] == nil {
			t.Fatalf("CEO overview: %v %s", out, msg)
		}
	})

	t.Run("writes follow the workflow and are marked as made by an assistant", func(t *testing.T) {
		ref := fmt.Sprint(int(shopNum))
		if _, msg := e.tool(memTok, "move_project", map[string]any{"project": ref, "to": "completed"}); msg == "" {
			t.Fatal("a member can't complete a project")
		}
		out, msg := e.tool(memTok, "move_project", map[string]any{"project": ref, "to": "ready_for_review"})
		if msg != "" || out["project"].(map[string]any)["status"] != "ready_for_review" {
			t.Fatalf("send for delivery: %v %s", out, msg)
		}
		out, msg = e.tool(memTok, "draft_client_update", map[string]any{"project": ref, "message": "Theme installed"})
		if msg != "" || out["status"] != "draft" {
			t.Fatalf("draft: %v %s", out, msg)
		}
		u := updateOf(e.do("GET", "/api/updates/"+out["update_id"].(string), memS, nil))
		if u["status"] != "draft" {
			t.Fatalf("an assistant's update must stay a draft, got %v", u["status"])
		}
		found := false
		for _, a := range e.do("GET", "/api/activity?type=project", ceo, nil).body["activity"].([]any) {
			m := a.(map[string]any)
			if strings.Contains(fmt.Sprint(m["action"]), "status") {
				meta := m["metadata"].(map[string]any)
				found = found || (meta["via"] == "mcp" && meta["via_client"] == "Claude Code")
			}
		}
		if !found {
			t.Fatal("the assistant's status change isn't marked via mcp in the activity log")
		}
		card, msg := e.tool(e.personalToken(tlS, "TL tasks"), "add_card", map[string]any{"project": ref, "title": "Set up payments"})
		if msg != "" {
			t.Fatal(msg)
		}
		cid := card["card"].(map[string]any)["id"].(string)
		if out, msg := e.tool(memTok, "move_card", map[string]any{"card": cid, "column": "in_progress"}); msg != "" || out["card"].(map[string]any)["column"] != "in_progress" {
			t.Fatalf("move card: %v %s", out, msg)
		}
		if _, msg := e.tool(e.personalToken(mem2S, "Other"), "move_card", map[string]any{"card": cid, "column": "done"}); msg == "" {
			t.Fatal("someone off the project moved its card")
		}
	})

	t.Run("revoking a token cuts the assistant off at once", func(t *testing.T) {
		tok := e.personalToken(memS, "Temp")
		var tid string
		for _, x := range e.do("GET", "/api/account/assistant-tokens", memS, nil).body["tokens"].([]any) {
			if x.(map[string]any)["name"] == "Temp" {
				tid = x.(map[string]any)["id"].(string)
			}
		}
		expect(t, e.do("DELETE", "/api/account/assistant-tokens/"+tid, memS, nil), 204, "revoke")
		if status, _ := e.legacyCall(tok, "tools/list", map[string]any{}); status != 401 {
			t.Fatalf("revoked token still works: %d", status)
		}
		expect(t, e.do("DELETE", "/api/admin/users/"+e.users["member"].ID.String()+"/assistant-tokens", hr, nil), 200, "HR revokes all")
		if status, _ := e.legacyCall(memTok, "tools/list", map[string]any{}); status != 401 {
			t.Fatalf("token survived HR's revoke: %d", status)
		}
		expect(t, e.do("DELETE", "/api/admin/users/"+e.users["ceo"].ID.String()+"/assistant-tokens", hr, nil), 403, "HR can't touch the CEO's")
	})

	t.Run("OAuth: register, consent, code with PKCE, tokens, refresh rotation", func(t *testing.T) {
		status, _, raw := e.send("GET", "/.well-known/oauth-authorization-server", nil, "")
		if status != 200 || !strings.Contains(string(raw), `"S256"`) || !strings.Contains(string(raw), `"issuer":"http://app.test"`) {
			t.Fatalf("AS metadata: %d %s", status, raw)
		}
		status, _, raw = e.send("POST", "/oauth/register", map[string]string{"Content-Type": "application/json"},
			`{"client_name":"Test Assistant","redirect_uris":["http://localhost:7777/cb"]}`)
		if status != 201 {
			t.Fatalf("register: %d %s", status, raw)
		}
		var reg struct {
			ClientID string `json:"client_id"`
		}
		_ = json.Unmarshal(raw, &reg)
		if status, _, _ := e.send("POST", "/oauth/register", map[string]string{"Content-Type": "application/json"},
			`{"client_name":"x","redirect_uris":["http://evil.example/cb"]}`); status != 400 {
			t.Fatalf("plain-http non-local redirect accepted: %d", status)
		}

		verifier := strings.Repeat("v", 50)
		sum := sha256.Sum256([]byte(verifier))
		challenge := base64.RawURLEncoding.EncodeToString(sum[:])
		q := url.Values{
			"response_type": {"code"}, "client_id": {reg.ClientID}, "redirect_uri": {"http://localhost:7777/cb"},
			"state": {"xyz"}, "code_challenge": {challenge}, "code_challenge_method": {"S256"}, "resource": {"http://app.test/mcp"},
		}
		// Not signed in: off to the login page, coming back here after.
		status, h, _ := e.send("GET", "/oauth/authorize?"+q.Encode(), nil, "")
		if status != 302 || !strings.HasPrefix(h.Get("Location"), "/login?next=") {
			t.Fatalf("unauthenticated authorize: %d %s", status, h.Get("Location"))
		}
		// A redirect URI the client didn't register is shown, never followed.
		bad := url.Values{}
		for k, v := range q {
			bad[k] = v
		}
		bad.Set("redirect_uri", "https://evil.example/cb")
		if status, h, _ := e.send("GET", "/oauth/authorize?"+bad.Encode(), map[string]string{"Cookie": sessionCookie + "=" + memS}, ""); status != 400 || h.Get("Location") != "" {
			t.Fatalf("bad redirect: %d %q", status, h.Get("Location"))
		}
		noPKCE := url.Values{}
		for k, v := range q {
			noPKCE[k] = v
		}
		noPKCE.Del("code_challenge")
		if status, h, _ := e.send("GET", "/oauth/authorize?"+noPKCE.Encode(), map[string]string{"Cookie": sessionCookie + "=" + memS}, ""); status != 302 || !strings.Contains(h.Get("Location"), "error=invalid_request") {
			t.Fatalf("no PKCE: %d %q", status, h.Get("Location"))
		}

		status, _, page := e.send("GET", "/oauth/authorize?"+q.Encode(), map[string]string{"Cookie": sessionCookie + "=" + memS}, "")
		m := regexp.MustCompile(`name="token" value="([^"]+)"`).FindSubmatch(page)
		if status != 200 || m == nil || !strings.Contains(string(page), "Test Assistant") {
			t.Fatalf("consent page: %d", status)
		}
		form := url.Values{"token": {string(m[1])}, "decision": {"allow"}}.Encode()
		formH := map[string]string{"Cookie": sessionCookie + "=" + memS, "Content-Type": "application/x-www-form-urlencoded", "Origin": testOrigin}
		if status, _, _ := e.send("POST", "/oauth/authorize", map[string]string{"Cookie": sessionCookie + "=" + memS, "Content-Type": "application/x-www-form-urlencoded", "Origin": "https://evil.example"}, form); status != 403 {
			t.Fatalf("cross-site consent: %d", status)
		}
		status, h, _ = e.send("POST", "/oauth/authorize", formH, form)
		loc, _ := url.Parse(h.Get("Location"))
		if status != 302 || loc.Query().Get("code") == "" || loc.Query().Get("state") != "xyz" || loc.Query().Get("iss") != "http://app.test" {
			t.Fatalf("consent: %d %s", status, h.Get("Location"))
		}
		code := loc.Query().Get("code")
		if status, _, _ := e.send("POST", "/oauth/authorize", formH, form); status != 400 {
			t.Fatalf("consent token reused: %d", status)
		}

		tokenH := map[string]string{"Content-Type": "application/x-www-form-urlencoded"}
		exchange := url.Values{"grant_type": {"authorization_code"}, "code": {code}, "redirect_uri": {"http://localhost:7777/cb"},
			"client_id": {reg.ClientID}, "resource": {"http://app.test/mcp"}}
		wrong := url.Values{}
		for k, v := range exchange {
			wrong[k] = v
		}
		wrong.Set("code_verifier", strings.Repeat("w", 50))
		if status, _, _ := e.send("POST", "/oauth/token", tokenH, wrong.Encode()); status != 400 {
			t.Fatalf("wrong verifier accepted: %d", status)
		}
		// A failed exchange burns the code; get another.
		status, _, page = e.send("GET", "/oauth/authorize?"+q.Encode(), map[string]string{"Cookie": sessionCookie + "=" + memS}, "")
		m = regexp.MustCompile(`name="token" value="([^"]+)"`).FindSubmatch(page)
		_, h, _ = e.send("POST", "/oauth/authorize", formH, url.Values{"token": {string(m[1])}, "decision": {"allow"}}.Encode())
		loc, _ = url.Parse(h.Get("Location"))
		exchange.Set("code", loc.Query().Get("code"))
		exchange.Set("code_verifier", verifier)
		status, _, raw = e.send("POST", "/oauth/token", tokenH, exchange.Encode())
		var tok struct {
			Access  string `json:"access_token"`
			Refresh string `json:"refresh_token"`
		}
		_ = json.Unmarshal(raw, &tok)
		if status != 200 || !strings.HasPrefix(tok.Access, "ofa_") || !strings.HasPrefix(tok.Refresh, "ofr_") {
			t.Fatalf("token: %d %s", status, raw)
		}
		if status, _, _ := e.send("POST", "/oauth/token", tokenH, exchange.Encode()); status != 400 {
			t.Fatalf("code reused: %d", status)
		}
		if out, msg := e.tool(tok.Access, "whoami", nil); msg != "" || out["name"] != "member" {
			t.Fatalf("oauth token use: %v %s", out, msg)
		}

		refresh := url.Values{"grant_type": {"refresh_token"}, "refresh_token": {tok.Refresh}, "client_id": {reg.ClientID}}
		status, _, raw = e.send("POST", "/oauth/token", tokenH, refresh.Encode())
		var next struct {
			Access string `json:"access_token"`
		}
		_ = json.Unmarshal(raw, &next)
		if status != 200 || next.Access == "" || next.Access == tok.Access {
			t.Fatalf("refresh: %d %s", status, raw)
		}
		if status, _, _ := e.send("POST", "/oauth/token", tokenH, refresh.Encode()); status != 400 {
			t.Fatalf("old refresh token still works: %d", status)
		}
		if status, _ := e.legacyCall(tok.Access, "tools/list", map[string]any{}); status != 401 {
			t.Fatalf("old access token still works after refresh: %d", status)
		}
	})

	t.Run("a metadata-document client on a private address is not fetched", func(t *testing.T) {
		q := url.Values{"response_type": {"code"}, "client_id": {"https://127.0.0.1/client.json"}, "redirect_uri": {"http://localhost:7777/cb"},
			"code_challenge": {"x"}, "code_challenge_method": {"S256"}}
		status, h, page := e.send("GET", "/oauth/authorize?"+q.Encode(), map[string]string{"Cookie": sessionCookie + "=" + memS}, "")
		if status != 400 || h.Get("Location") != "" || !strings.Contains(string(page), "not public") {
			t.Fatalf("private client_id: %d %s", status, page)
		}
	})
}

func TestPublicAddr(t *testing.T) {
	for addr, want := range map[string]bool{
		"8.8.8.8": true, "2606:4700::1111": true,
		"127.0.0.1": false, "10.1.2.3": false, "172.17.0.1": false, "192.168.1.1": false, "169.254.169.254": false,
		"100.64.0.1": false, "0.0.0.0": false, "::1": false, "fd00::1": false, "fe80::1": false, "::ffff:127.0.0.1": false,
	} {
		if got := mcp.PublicAddr(netip.MustParseAddr(addr)); got != want {
			t.Errorf("PublicAddr(%s) = %v, want %v", addr, got, want)
		}
	}
}
