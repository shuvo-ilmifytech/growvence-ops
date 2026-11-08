package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"html/template"
	"log/slog"
	"net/url"
	"strings"
	"time"

	"github.com/gofiber/fiber/v3"
	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"

	"github.com/solvinx/opsflow/internal/auth"
	"github.com/solvinx/opsflow/internal/db"
	"github.com/solvinx/opsflow/internal/mcp"
	"github.com/solvinx/opsflow/internal/policy"
	"github.com/solvinx/opsflow/internal/store"
)

// AI assistants over MCP (Phase 14). The MCP endpoint and the OAuth endpoints live
// outside /api: they are called by assistants, not by the portal's pages, so they carry
// no session cookie and no portal Origin, and authenticate with a bearer token instead.

const mcpScope = "opsflow"

// rate limit per token: generous for an assistant, low enough to stop a runaway loop.
const mcpCallsPerMinute = 120

func (s *Server) issuer() string      { return strings.TrimRight(s.cfg.AppOrigins[0], "/") }
func (s *Server) mcpResource() string { return s.issuer() + "/mcp" }
func (s *Server) resourceMetadataURL() string {
	return s.issuer() + "/.well-known/oauth-protected-resource/mcp"
}

func (s *Server) mountMCP(app *fiber.App) {
	s.mcpServer = mcp.NewServer(s.store, s.projects, s.reports, s.issuer())

	app.Post("/mcp", s.mcpEndpoint)
	// This revision has no GET stream and no sessions to delete.
	app.Get("/mcp", mcpMethodNotAllowed)
	app.Delete("/mcp", mcpMethodNotAllowed)

	for _, p := range []string{"/.well-known/oauth-protected-resource", "/.well-known/oauth-protected-resource/mcp"} {
		app.Get(p, withCORS(s.protectedResourceMetadata))
		app.Options(p, corsPreflight)
	}
	for _, p := range []string{"/.well-known/oauth-authorization-server", "/.well-known/openid-configuration"} {
		app.Get(p, withCORS(s.authServerMetadata))
		app.Options(p, corsPreflight)
	}
	app.Post("/oauth/register", withCORS(s.oauthRegister))
	app.Options("/oauth/register", corsPreflight)
	app.Post("/oauth/token", withCORS(s.oauthToken))
	app.Options("/oauth/token", corsPreflight)
	app.Get("/oauth/authorize", s.oauthAuthorize)
	app.Post("/oauth/authorize", s.oauthDecide)
}

func mcpMethodNotAllowed(c fiber.Ctx) error {
	c.Set(fiber.HeaderAllow, "POST")
	return c.SendStatus(fiber.StatusMethodNotAllowed)
}

// ── the MCP endpoint ───────────────────────────────────────

func (s *Server) mcpEndpoint(c fiber.Ctx) error {
	// DNS-rebinding defence: a browser page may only call us from the portal itself.
	if o := c.Get(fiber.HeaderOrigin); o != "" && !containsStr(s.cfg.AppOrigins, o) {
		return c.Status(fiber.StatusForbidden).JSON(fiber.Map{"jsonrpc": "2.0", "error": fiber.Map{"code": -32600, "message": "Origin not allowed"}})
	}
	caller, ok, err := s.bearer(c)
	if err != nil {
		return err
	}
	if !ok {
		return s.unauthorizedMCP(c, c.Get(fiber.HeaderAuthorization) != "")
	}
	allowed, err := s.mcpRateOK(c.Context(), caller.tokenID)
	if err != nil {
		return err
	}
	if !allowed {
		c.Set(fiber.HeaderRetryAfter, "60")
		return c.Status(fiber.StatusTooManyRequests).JSON(fiber.Map{"jsonrpc": "2.0", "error": fiber.Map{"code": -32603, "message": "Too many requests: slow down"}})
	}
	status, body := s.mcpServer.Handle(c.Context(), mcp.Caller{User: caller.user, Client: caller.client}, mcp.Headers{
		ProtocolVersion: c.Get("MCP-Protocol-Version"),
		Method:          c.Get("Mcp-Method"),
		Name:            c.Get("Mcp-Name"),
	}, c.Body())
	if body == nil {
		// 202 for a notification: nothing to say, not even a status text.
		return c.Status(status).Send(nil)
	}
	c.Set(fiber.HeaderContentType, fiber.MIMEApplicationJSON)
	c.Set(fiber.HeaderCacheControl, "no-store")
	return c.Status(status).Send(body)
}

func (s *Server) unauthorizedMCP(c fiber.Ctx, hadToken bool) error {
	challenge := fmt.Sprintf(`Bearer resource_metadata="%s", scope="%s"`, s.resourceMetadataURL(), mcpScope)
	if hadToken {
		challenge += `, error="invalid_token", error_description="The token is invalid, expired or revoked"`
	}
	c.Set(fiber.HeaderWWWAuthenticate, challenge)
	return c.Status(fiber.StatusUnauthorized).JSON(fiber.Map{"jsonrpc": "2.0", "error": fiber.Map{"code": -32600, "message": "Sign in to OpsFlow to use this server"}})
}

type mcpCaller struct {
	user    db.User
	client  string
	tokenID uuid.UUID
}

// bearer resolves `Authorization: Bearer …` to the person the token stands for. The
// user row is re-read every time, so a role change or lost access applies at once.
func (s *Server) bearer(c fiber.Ctx) (mcpCaller, bool, error) {
	var out mcpCaller
	h := c.Get(fiber.HeaderAuthorization)
	secret, found := strings.CutPrefix(h, "Bearer ")
	if !found || !(strings.HasPrefix(secret, mcp.PersonalPrefix) || strings.HasPrefix(secret, mcp.AccessPrefix)) {
		return out, false, nil
	}
	t, err := s.store.MCPTokenByAccess(c.Context(), mcp.Hash(secret))
	if store.IsNotFound(err) {
		return out, false, nil
	} else if err != nil {
		return out, false, err
	}
	u, err := s.store.GetUserByID(c.Context(), t.UserID)
	if store.IsNotFound(err) || (err == nil && !u.IsActive) {
		return out, false, nil
	} else if err != nil {
		return out, false, err
	}
	if err := s.store.TouchMCPToken(c.Context(), t.ID); err != nil {
		slog.Warn("mcp token touch failed", "err", err)
	}
	return mcpCaller{user: u, client: t.Name, tokenID: t.ID}, true, nil
}

func (s *Server) mcpRateOK(ctx context.Context, token uuid.UUID) (bool, error) {
	key := fmt.Sprintf("mcprate:%s:%d", token, time.Now().Unix()/60)
	n, err := s.rdb.Incr(ctx, key).Result()
	if err != nil {
		return false, err
	}
	if n == 1 {
		s.rdb.Expire(ctx, key, 2*time.Minute)
	}
	return n <= mcpCallsPerMinute, nil
}

// ── discovery ──────────────────────────────────────────────

func (s *Server) protectedResourceMetadata(c fiber.Ctx) error {
	return c.JSON(fiber.Map{
		"resource":                 s.mcpResource(),
		"resource_name":            "OpsFlow (Growvence Tech)",
		"authorization_servers":    []string{s.issuer()},
		"scopes_supported":         []string{mcpScope},
		"bearer_methods_supported": []string{"header"},
		"resource_documentation":   s.issuer() + "/account",
	})
}

func (s *Server) authServerMetadata(c fiber.Ctx) error {
	is := s.issuer()
	return c.JSON(fiber.Map{
		"issuer":                                         is,
		"authorization_endpoint":                         is + "/oauth/authorize",
		"token_endpoint":                                 is + "/oauth/token",
		"registration_endpoint":                          is + "/oauth/register",
		"response_types_supported":                       []string{"code"},
		"grant_types_supported":                          []string{"authorization_code", "refresh_token"},
		"code_challenge_methods_supported":               []string{"S256"},
		"token_endpoint_auth_methods_supported":          []string{"none"},
		"scopes_supported":                               []string{mcpScope},
		"client_id_metadata_document_supported":          true,
		"authorization_response_iss_parameter_supported": true,
	})
}

func withCORS(h fiber.Handler) fiber.Handler {
	return func(c fiber.Ctx) error {
		c.Set(fiber.HeaderAccessControlAllowOrigin, "*")
		c.Set(fiber.HeaderCacheControl, "no-store")
		return h(c)
	}
}

func corsPreflight(c fiber.Ctx) error {
	c.Set(fiber.HeaderAccessControlAllowOrigin, "*")
	c.Set(fiber.HeaderAccessControlAllowMethods, "GET, POST, OPTIONS")
	c.Set(fiber.HeaderAccessControlAllowHeaders, "Content-Type, Authorization, MCP-Protocol-Version")
	c.Set(fiber.HeaderAccessControlMaxAge, "86400")
	return c.SendStatus(fiber.StatusNoContent)
}

// ── client registration (RFC 7591) ─────────────────────────

func oauthErr(c fiber.Ctx, status int, code, desc string) error {
	c.Set(fiber.HeaderCacheControl, "no-store")
	return c.Status(status).JSON(fiber.Map{"error": code, "error_description": desc})
}

func (s *Server) oauthRegister(c fiber.Ctx) error {
	var req struct {
		ClientName   string   `json:"client_name"`
		RedirectURIs []string `json:"redirect_uris"`
	}
	if err := json.Unmarshal(c.Body(), &req); err != nil {
		return oauthErr(c, fiber.StatusBadRequest, "invalid_client_metadata", "The body must be JSON client metadata")
	}
	// Registering is open by design, so it's capped per address.
	key := "oauthreg:" + c.IP()
	n, err := s.rdb.Incr(c.Context(), key).Result()
	if err != nil {
		return err
	}
	if n == 1 {
		s.rdb.Expire(c.Context(), key, time.Hour)
	}
	if n > 30 {
		return oauthErr(c, fiber.StatusTooManyRequests, "invalid_request", "Too many registrations; try later")
	}
	name := strings.TrimSpace(req.ClientName)
	if name == "" {
		name = "AI assistant"
	}
	if len(name) > 100 {
		name = name[:100]
	}
	if len(req.RedirectURIs) == 0 || len(req.RedirectURIs) > 10 {
		return oauthErr(c, fiber.StatusBadRequest, "invalid_redirect_uri", "Give 1 to 10 redirect_uris")
	}
	for _, r := range req.RedirectURIs {
		if !mcp.ValidRedirect(r) {
			return oauthErr(c, fiber.StatusBadRequest, "invalid_redirect_uri", "Redirect URIs must be https, or http on localhost: "+r)
		}
	}
	cl, err := s.store.UpsertOAuthClient(c.Context(), db.UpsertOAuthClientParams{
		ID: "oc_" + uuid.NewString(), Kind: "registered", Name: name, RedirectUris: req.RedirectURIs,
	})
	if err != nil {
		return err
	}
	c.Set(fiber.HeaderCacheControl, "no-store")
	return c.Status(fiber.StatusCreated).JSON(fiber.Map{
		"client_id": cl.ID, "client_id_issued_at": cl.CreatedAt.Unix(), "client_name": cl.Name,
		"redirect_uris": cl.RedirectUris, "grant_types": []string{"authorization_code", "refresh_token"},
		"response_types": []string{"code"}, "token_endpoint_auth_method": "none",
	})
}

// client finds a registered client, or fetches (and caches for a day) the metadata
// document of a client known by URL.
func (s *Server) client(ctx context.Context, id string) (db.OauthClient, error) {
	cl, err := s.store.GetOAuthClient(ctx, id)
	if err == nil && (cl.Kind == "registered" || time.Since(cl.RefreshedAt) < 24*time.Hour) {
		return cl, nil
	}
	if err != nil && !store.IsNotFound(err) {
		return cl, err
	}
	if !mcp.IsMetadataClientID(id) {
		return cl, errors.New("unknown client")
	}
	doc, err := mcp.FetchClientDoc(ctx, id)
	if err != nil {
		return cl, err
	}
	name := strings.TrimSpace(doc.ClientName)
	if len(name) > 100 {
		name = name[:100]
	}
	return s.store.UpsertOAuthClient(ctx, db.UpsertOAuthClientParams{ID: id, Kind: "metadata", Name: name, RedirectUris: doc.RedirectURIs})
}

// ── authorization: sign in, consent, code ─────────────────

type authRequest struct {
	ClientID      string `json:"client_id"`
	RedirectURI   string `json:"redirect_uri"`
	State         string `json:"state"`
	CodeChallenge string `json:"code_challenge"`
	Resource      string `json:"resource"`
}

// codeGrant is what an authorization code stands for until it is exchanged (once).
type codeGrant struct {
	authRequest
	UserID     uuid.UUID `json:"user_id"`
	ClientName string    `json:"client_name"`
}

// redirectBack sends the browser to the client with the result, and our issuer (RFC 9207).
func (s *Server) redirectBack(c fiber.Ctx, a authRequest, params url.Values) error {
	u, _ := url.Parse(a.RedirectURI)
	q := u.Query()
	for k, v := range params {
		q[k] = v
	}
	if a.State != "" {
		q.Set("state", a.State)
	}
	q.Set("iss", s.issuer())
	u.RawQuery = q.Encode()
	return c.Redirect().Status(fiber.StatusFound).To(u.String())
}

// readAuthRequest validates an authorization request. Until the client and redirect URI
// are known to be right, problems are shown on the page, never redirected anywhere.
// ok is false when the page explaining the problem has been written.
func (s *Server) readAuthRequest(c fiber.Ctx) (a authRequest, cl db.OauthClient, ok bool, err error) {
	a = authRequest{
		ClientID: c.Query("client_id"), RedirectURI: c.Query("redirect_uri"), State: c.Query("state"),
		CodeChallenge: c.Query("code_challenge"), Resource: c.Query("resource"),
	}
	cl, err = s.client(c.Context(), a.ClientID)
	if err != nil {
		return a, cl, false, s.authPage(c, fiber.StatusBadRequest, "This app can't connect", "OpsFlow doesn't recognise this app ("+err.Error()+").", nil)
	}
	if !mcp.RedirectMatches(cl.RedirectUris, a.RedirectURI) {
		return a, cl, false, s.authPage(c, fiber.StatusBadRequest, "This app can't connect", "The app asked to return to an address it didn't register.", nil)
	}
	return a, cl, true, nil
}

func (s *Server) oauthAuthorize(c fiber.Ctx) error {
	a, cl, ok, err := s.readAuthRequest(c)
	if !ok {
		return err
	}
	switch {
	case c.Query("response_type") != "code":
		return s.redirectBack(c, a, url.Values{"error": {"unsupported_response_type"}})
	case a.CodeChallenge == "" || c.Query("code_challenge_method") != "S256":
		return s.redirectBack(c, a, url.Values{"error": {"invalid_request"}, "error_description": {"PKCE with S256 is required"}})
	case a.Resource != "" && strings.TrimRight(a.Resource, "/") != s.mcpResource():
		return s.redirectBack(c, a, url.Values{"error": {"invalid_target"}, "error_description": {"Unknown resource"}})
	}
	u, ok, err := s.sessionUser(c)
	if err != nil {
		return err
	}
	if !ok {
		// Sign in first, then come back here.
		return c.Redirect().Status(fiber.StatusFound).To("/login?next=" + url.QueryEscape(c.OriginalURL()))
	}
	csrf := mcp.NewSecret("")
	raw, _ := json.Marshal(struct {
		authRequest
		UserID uuid.UUID `json:"user_id"`
	}{a, u.ID})
	if err := s.rdb.Set(c.Context(), "oauthconsent:"+mcp.Hash(csrf), raw, 15*time.Minute).Err(); err != nil {
		return err
	}
	redirect, _ := url.Parse(a.RedirectURI)
	return s.authPage(c, fiber.StatusOK, "Connect "+cl.Name+" to OpsFlow?", "", &consentView{
		Client: cl.Name, ClientHost: redirect.Host, User: u.Name, Role: roleLabel(u), Token: csrf,
		CanWrite:   !policy.IsReadOnly(policy.Role(u.Role)) && !mcp.WithoutProjects(u),
		NoProjects: mcp.WithoutProjects(u),
	})
}

// oauthDecide is the consent form's answer. The form carries only a one-time token; what
// was asked for and who asked are read back from Redis, never from the form.
func (s *Server) oauthDecide(c fiber.Ctx) error {
	if o := c.Get(fiber.HeaderOrigin); o != "" && !containsStr(s.cfg.AppOrigins, o) {
		return s.authPage(c, fiber.StatusForbidden, "Request refused", "This form must be sent from OpsFlow itself.", nil)
	}
	u, ok, err := s.sessionUser(c)
	if err != nil {
		return err
	}
	raw, err := s.rdb.GetDel(c.Context(), "oauthconsent:"+mcp.Hash(c.FormValue("token"))).Bytes()
	if errors.Is(err, redis.Nil) || !ok {
		return s.authPage(c, fiber.StatusBadRequest, "This request expired", "Go back to your AI app and connect again.", nil)
	} else if err != nil {
		return err
	}
	var pending struct {
		authRequest
		UserID uuid.UUID `json:"user_id"`
	}
	if err := json.Unmarshal(raw, &pending); err != nil || pending.UserID != u.ID {
		return s.authPage(c, fiber.StatusBadRequest, "This request expired", "Go back to your AI app and connect again.", nil)
	}
	a := pending.authRequest
	if c.FormValue("decision") != "allow" {
		return s.redirectBack(c, a, url.Values{"error": {"access_denied"}})
	}
	cl, err := s.client(c.Context(), a.ClientID)
	if err != nil {
		return s.authPage(c, fiber.StatusBadRequest, "This app can't connect", err.Error(), nil)
	}
	code := mcp.NewSecret(mcp.CodePrefix)
	grant, _ := json.Marshal(codeGrant{authRequest: a, UserID: u.ID, ClientName: cl.Name})
	if err := s.rdb.Set(c.Context(), "oauthcode:"+mcp.Hash(code), grant, mcp.CodeTTL).Err(); err != nil {
		return err
	}
	_ = store.LogActivity(c.Context(), s.store.Queries, store.Activity{
		Actor: &u.ID, Action: "auth.assistant_connected", EntityType: "user", EntityID: &u.ID,
		Metadata: map[string]any{"client": cl.Name},
	})
	return s.redirectBack(c, a, url.Values{"code": {code}})
}

func (s *Server) sessionUser(c fiber.Ctx) (db.User, bool, error) {
	sess, err := s.sessions.Get(c.Context(), c.Cookies(sessionCookie))
	if errors.Is(err, auth.ErrNoSession) {
		return db.User{}, false, nil
	} else if err != nil {
		return db.User{}, false, err
	}
	u, err := s.store.GetUserByID(c.Context(), sess.UserID)
	if store.IsNotFound(err) || (err == nil && !u.IsActive) {
		return db.User{}, false, nil
	}
	return u, err == nil, err
}

// ── token endpoint ─────────────────────────────────────────

func (s *Server) oauthToken(c fiber.Ctx) error {
	if res := c.FormValue("resource"); res != "" && strings.TrimRight(res, "/") != s.mcpResource() {
		return oauthErr(c, fiber.StatusBadRequest, "invalid_target", "Unknown resource")
	}
	switch c.FormValue("grant_type") {
	case "authorization_code":
		raw, err := s.rdb.GetDel(c.Context(), "oauthcode:"+mcp.Hash(c.FormValue("code"))).Bytes()
		if errors.Is(err, redis.Nil) {
			return oauthErr(c, fiber.StatusBadRequest, "invalid_grant", "The code is invalid, expired or already used")
		} else if err != nil {
			return err
		}
		var g codeGrant
		if err := json.Unmarshal(raw, &g); err != nil {
			return err
		}
		switch {
		case c.FormValue("client_id") != g.ClientID:
			return oauthErr(c, fiber.StatusBadRequest, "invalid_grant", "The code was issued to another client")
		case c.FormValue("redirect_uri") != g.RedirectURI:
			return oauthErr(c, fiber.StatusBadRequest, "invalid_grant", "redirect_uri doesn't match the authorization request")
		case !mcp.VerifyPKCE(c.FormValue("code_verifier"), g.CodeChallenge):
			return oauthErr(c, fiber.StatusBadRequest, "invalid_grant", "PKCE verification failed")
		}
		access, refresh := mcp.NewSecret(mcp.AccessPrefix), mcp.NewSecret(mcp.RefreshPrefix)
		now := time.Now()
		accessExp, refreshExp := now.Add(mcp.AccessTTL), now.Add(mcp.RefreshTTL)
		ah, rh := mcp.Hash(access), mcp.Hash(refresh)
		if _, err := s.store.CreateMCPToken(c.Context(), db.CreateMCPTokenParams{
			UserID: g.UserID, Kind: "oauth", Name: g.ClientName, ClientID: &g.ClientID,
			AccessHash: ah, AccessExpiresAt: &accessExp, RefreshHash: &rh, RefreshExpiresAt: &refreshExp,
		}); err != nil {
			return err
		}
		return tokenResponse(c, access, refresh)
	case "refresh_token":
		access, refresh := mcp.NewSecret(mcp.AccessPrefix), mcp.NewSecret(mcp.RefreshPrefix)
		err := s.store.Tx(c.Context(), func(q *db.Queries) error {
			t, err := q.MCPTokenByRefresh(c.Context(), ptr(mcp.Hash(c.FormValue("refresh_token"))))
			if err != nil {
				return err
			}
			if t.ClientID == nil || *t.ClientID != c.FormValue("client_id") {
				return errWrongClient
			}
			now := time.Now()
			ah, rh := mcp.Hash(access), mcp.Hash(refresh)
			return q.RotateMCPToken(c.Context(), db.RotateMCPTokenParams{
				ID: t.ID, AccessHash: ah, AccessExpiresAt: ptr(now.Add(mcp.AccessTTL)),
				RefreshHash: &rh, RefreshExpiresAt: ptr(now.Add(mcp.RefreshTTL)),
			})
		})
		switch {
		case store.IsNotFound(err), errors.Is(err, errWrongClient):
			return oauthErr(c, fiber.StatusBadRequest, "invalid_grant", "The refresh token is invalid, expired or revoked")
		case err != nil:
			return err
		}
		return tokenResponse(c, access, refresh)
	}
	return oauthErr(c, fiber.StatusBadRequest, "unsupported_grant_type", "Use authorization_code or refresh_token")
}

var errWrongClient = errors.New("token belongs to another client")

func ptr[T any](v T) *T { return &v }

func tokenResponse(c fiber.Ctx, access, refresh string) error {
	c.Set(fiber.HeaderCacheControl, "no-store")
	c.Set(fiber.HeaderPragma, "no-cache")
	return c.JSON(fiber.Map{
		"access_token": access, "token_type": "Bearer", "expires_in": int(mcp.AccessTTL.Seconds()),
		"refresh_token": refresh, "scope": mcpScope,
	})
}

// ── personal tokens and connected assistants (Account page) ───

const maxPersonalTokens = 10

func (s *Server) listMCPTokens(c fiber.Ctx) error {
	rows, err := s.store.ListMCPTokens(c.Context(), currentUser(c).ID)
	if err != nil {
		return err
	}
	return c.JSON(fiber.Map{"tokens": rows, "endpoint": s.mcpResource()})
}

func (s *Server) createMCPToken(c fiber.Ctx) error {
	var req struct {
		Name string `json:"name"`
	}
	if err := decodeJSON(c, &req); err != nil {
		return err
	}
	name := strings.TrimSpace(req.Name)
	if name == "" || len(name) > 100 {
		return errValidation(map[string]string{"name": "must be 1 to 100 characters"})
	}
	u := currentUser(c)
	if r := policy.Role(u.Role); r == policy.HR || r == policy.OfficeAdmin {
		return errBadRequest("OpsFlow's assistant tools cover projects, which " + roleLabel(u) + " doesn't work on")
	}
	n, err := s.store.CountUserMCPTokens(c.Context(), u.ID)
	if err != nil {
		return err
	}
	if n >= maxPersonalTokens {
		return errBadRequest(fmt.Sprintf("You can have up to %d tokens; revoke one first", maxPersonalTokens))
	}
	secret := mcp.NewSecret(mcp.PersonalPrefix)
	t, err := s.store.CreateMCPToken(c.Context(), db.CreateMCPTokenParams{UserID: u.ID, Kind: "personal", Name: name, AccessHash: mcp.Hash(secret)})
	if err != nil {
		return err
	}
	_ = store.LogActivity(c.Context(), s.store.Queries, store.Activity{
		Actor: &u.ID, Action: "auth.assistant_token_created", EntityType: "user", EntityID: &u.ID, Metadata: map[string]any{"name": name},
	})
	// The secret is shown this once; only its hash is kept.
	return c.Status(fiber.StatusCreated).JSON(fiber.Map{"token": secret, "id": t.ID, "name": t.Name, "endpoint": s.mcpResource()})
}

func (s *Server) revokeMCPToken(c fiber.Ctx) error {
	id, err := paramUUID(c, "tid")
	if err != nil {
		return err
	}
	u := currentUser(c)
	n, err := s.store.RevokeMCPToken(c.Context(), db.RevokeMCPTokenParams{ID: id, UserID: u.ID})
	if err != nil {
		return err
	}
	if n == 0 {
		return errNotFound("Token")
	}
	_ = store.LogActivity(c.Context(), s.store.Queries, store.Activity{
		Actor: &u.ID, Action: "auth.assistant_token_revoked", EntityType: "user", EntityID: &u.ID,
	})
	return c.SendStatus(fiber.StatusNoContent)
}

// adminRevokeMCPTokens cuts off every assistant acting for someone (HR + CEO; only the
// CEO touches the CEO's account, as everywhere else).
func (s *Server) adminRevokeMCPTokens(c fiber.Ctx) error {
	id, err := paramUUID(c, "id")
	if err != nil {
		return err
	}
	target, err := s.store.GetUserByID(c.Context(), id)
	if store.IsNotFound(err) {
		return errNotFound("User")
	} else if err != nil {
		return err
	}
	actor := currentUser(c)
	if policy.Role(target.Role) == policy.CEO && policy.Role(actor.Role) != policy.CEO {
		return errForbidden()
	}
	n, err := s.store.RevokeUserMCPTokens(c.Context(), id)
	if err != nil {
		return err
	}
	_ = store.LogActivity(c.Context(), s.store.Queries, store.Activity{
		Actor: &actor.ID, Action: "user.assistant_access_revoked", EntityType: "user", EntityID: &id,
		Metadata: map[string]any{"name": target.Name, "tokens": n},
	})
	return c.JSON(fiber.Map{"revoked": n})
}

func roleLabel(u db.User) string {
	switch policy.Role(u.Role) {
	case policy.Sales:
		return "Sales"
	case policy.TL:
		return "Team Leader"
	case policy.Member:
		return "Team member"
	case policy.CEO:
		return "CEO"
	case policy.HR:
		return "HR"
	case policy.OfficeAdmin:
		return "Office Admin"
	}
	return u.Role
}

func containsStr(list []string, v string) bool {
	for _, x := range list {
		if x == v {
			return true
		}
	}
	return false
}

// ── the consent page ───────────────────────────────────────

type consentView struct {
	Client, ClientHost, User, Role, Token string
	// NoProjects: this person has no project access in the portal (HR, Office Admin), so
	// the assistant gets nothing but "who am I".
	CanWrite, NoProjects bool
}

var authTmpl = template.Must(template.New("auth").Parse(`<!doctype html>
<html lang="en"><head><meta charset="utf-8"><meta name="viewport" content="width=device-width, initial-scale=1">
<meta name="robots" content="noindex"><title>{{.Title}} · OpsFlow</title>
<style>
:root{--bg:#f7f6f4;--card:#fff;--fg:#1c1917;--muted:#78716c;--line:#e7e5e4;--primary:#f97316;--primary-fg:#fff}
@media (prefers-color-scheme:dark){:root{--bg:#0c0a09;--card:#1c1917;--fg:#fafaf9;--muted:#a8a29e;--line:#292524}}
*{box-sizing:border-box}body{margin:0;min-height:100svh;display:grid;place-items:center;background:var(--bg);color:var(--fg);
font:15px/1.5 system-ui,-apple-system,"Segoe UI",sans-serif;padding:16px}
.card{width:100%;max-width:440px;background:var(--card);border:1px solid var(--line);border-radius:16px;padding:28px;box-shadow:0 1px 3px rgba(0,0,0,.06)}
.brand{display:flex;align-items:center;gap:8px;font-weight:600;color:var(--muted);font-size:13px;margin-bottom:18px}
.dot{width:10px;height:10px;border-radius:3px;background:var(--primary)}
h1{font-size:20px;line-height:1.3;margin:0 0 8px}p{margin:0 0 14px;color:var(--muted)}
ul{margin:0 0 18px;padding-left:20px}li{margin:4px 0}
.who{background:var(--bg);border:1px solid var(--line);border-radius:10px;padding:10px 12px;margin-bottom:16px;font-size:14px}
.row{display:flex;gap:10px}button{flex:1;font:inherit;font-weight:600;padding:10px 14px;border-radius:10px;border:1px solid var(--line);
background:var(--card);color:var(--fg);cursor:pointer}button.allow{background:var(--primary);border-color:var(--primary);color:var(--primary-fg)}
small{display:block;margin-top:14px;color:var(--muted);font-size:12px}
</style></head><body><main class="card">
<div class="brand"><span class="dot"></span>OpsFlow · Growvence Tech</div>
<h1>{{.Title}}</h1>
{{if .Message}}<p>{{.Message}}</p>{{end}}
{{with .Consent}}
<div class="who">Signed in as <strong>{{.User}}</strong> ({{.Role}})</div>
<p><strong>{{.Client}}</strong> (returns to {{.ClientHost}}) wants to act for you in OpsFlow. It will be able to:</p>
<ul>
{{if .NoProjects}}<li>see who you are (OpsFlow's assistant tools cover projects, which your role doesn't work on)</li>
{{else}}<li>see the projects, boards, client updates and activity you can see</li>
{{if .CanWrite}}<li>move projects and board cards, comment and add files, as you</li>
<li>write client updates as <strong>drafts</strong> for you to check (never sent by itself)</li>
{{else}}<li>nothing more: your access is read-only</li>{{end}}{{end}}
</ul>
<form method="post" action="/oauth/authorize">
<input type="hidden" name="token" value="{{.Token}}">
<div class="row"><button name="decision" value="deny">Cancel</button><button class="allow" name="decision" value="allow" autofocus>Allow</button></div>
</form>
<small>Everything it changes is marked as done by an assistant in the activity log. You can disconnect it any time on your Account page.</small>
{{end}}
</main></body></html>`))

func (s *Server) authPage(c fiber.Ctx, status int, title, message string, consent *consentView) error {
	var b strings.Builder
	if err := authTmpl.Execute(&b, map[string]any{"Title": title, "Message": message, "Consent": consent}); err != nil {
		return err
	}
	c.Set(fiber.HeaderContentType, fiber.MIMETextHTMLCharsetUTF8)
	c.Set(fiber.HeaderXFrameOptions, "DENY")
	c.Set(fiber.HeaderContentSecurityPolicy, "default-src 'none'; style-src 'unsafe-inline'; form-action 'self' https: http://localhost:* http://127.0.0.1:*; frame-ancestors 'none'")
	c.Set(fiber.HeaderCacheControl, "no-store")
	c.Status(status)
	return c.SendString(b.String())
}
