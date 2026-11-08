// Package mcp is OpsFlow's Model Context Protocol server: AI assistants (Claude Code,
// Claude.ai, ChatGPT, Codex) read and update OpsFlow as the person using them.
//
// It speaks Streamable HTTP on one endpoint and serves both protocol eras:
//   - modern (2026-07-28): stateless, every request carries its version and client
//     capabilities in params._meta, mirrored into MCP-Protocol-Version / Mcp-Method /
//     Mcp-Name headers;
//   - legacy (2025-03-26 … 2025-11-25): an `initialize` handshake first. No session is
//     kept: every request is authorised and served on its own either way.
//
// Every request is authenticated by the HTTP layer and arrives here with its caller.
// Tools call the same services the portal does, so they see and may do exactly what
// the person may see and do in the portal.
package mcp

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"slices"
	"strings"

	"github.com/solvinx/opsflow/internal/apperr"
	"github.com/solvinx/opsflow/internal/db"
	"github.com/solvinx/opsflow/internal/projects"
	"github.com/solvinx/opsflow/internal/reports"
	"github.com/solvinx/opsflow/internal/store"
)

const (
	ModernVersion = "2026-07-28"
	serverName    = "opsflow"
	serverTitle   = "OpsFlow (Growvence Tech)"
	serverVersion = "1.0.0"
)

// Versions this server answers, newest first.
var (
	legacyVersions    = []string{"2025-11-25", "2025-06-18", "2025-03-26"}
	supportedVersions = append([]string{ModernVersion}, legacyVersions...)
)

const instructions = `OpsFlow is Growvence Tech's operations portal: client projects from marketplace orders, ` +
	`the teams that deliver them, their task boards and the updates sent to clients. You act as the signed-in ` +
	`person and see and do exactly what they can in the portal. Start with whoami and my_work. Projects can be ` +
	`referred to by number (e.g. 12 or "#12") or id. Client updates you write are saved as drafts for the ` +
	`person to check and submit in the portal; nothing reaches a client without them.`

const (
	codeParse           = -32700
	codeInvalidRequest  = -32600
	codeMethodNotFound  = -32601
	codeInvalidParams   = -32602
	codeInternal        = -32603
	codeHeaderMismatch  = -32020
	codeUnsupportedVers = -32022
)

type Server struct {
	projects *projects.Service
	reports  *reports.Service
	st       *store.Store
	portal   string // public base URL, for links in results
}

func NewServer(st *store.Store, p *projects.Service, r *reports.Service, portalURL string) *Server {
	return &Server{projects: p, reports: r, st: st, portal: strings.TrimRight(portalURL, "/")}
}

// Caller is the person an assistant acts for, and what the assistant is called.
type Caller struct {
	User   db.User
	Client string
}

// Headers are the MCP request headers the transport mirrors from the body.
type Headers struct {
	ProtocolVersion string // MCP-Protocol-Version
	Method          string // Mcp-Method
	Name            string // Mcp-Name
}

type request struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params"`
}

type rpcError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
	Data    any    `json:"data,omitempty"`
}

type response struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id,omitempty"`
	Result  any             `json:"result,omitempty"`
	Error   *rpcError       `json:"error,omitempty"`
}

// Handle serves one POSTed JSON-RPC message. It returns the HTTP status and the
// response body; a nil body means 202 Accepted with nothing to say.
func (s *Server) Handle(ctx context.Context, c Caller, h Headers, body []byte) (int, []byte) {
	var req request
	if err := json.Unmarshal(body, &req); err != nil {
		return reply(http.StatusBadRequest, nil, nil, &rpcError{Code: codeParse, Message: "Parse error: send one JSON-RPC message per request"})
	}
	if req.JSONRPC != "2.0" || req.Method == "" {
		return reply(http.StatusBadRequest, req.ID, nil, &rpcError{Code: codeInvalidRequest, Message: "Invalid JSON-RPC request"})
	}
	// Notifications (no id) need no answer; the legacy handshake sends one.
	if len(req.ID) == 0 || string(req.ID) == "null" {
		return http.StatusAccepted, nil
	}

	var params struct {
		Meta map[string]json.RawMessage `json:"_meta"`
		Name string                     `json:"name"`
	}
	if len(req.Params) > 0 {
		if err := json.Unmarshal(req.Params, &params); err != nil {
			return reply(http.StatusBadRequest, req.ID, nil, &rpcError{Code: codeInvalidParams, Message: "params must be an object"})
		}
	}
	version := ""
	if raw, ok := params.Meta["io.modelcontextprotocol/protocolVersion"]; ok {
		_ = json.Unmarshal(raw, &version)
	}
	if version != "" {
		return s.modern(ctx, c, h, req, version, params.Meta, params.Name)
	}
	return s.legacy(ctx, c, h, req)
}

// modern serves a 2026-07-28 request: per-request metadata, headers checked against the body.
func (s *Server) modern(ctx context.Context, c Caller, h Headers, req request, version string, meta map[string]json.RawMessage, name string) (int, []byte) {
	if version != ModernVersion {
		return reply(http.StatusBadRequest, req.ID, nil, &rpcError{
			Code: codeUnsupportedVers, Message: "Unsupported protocol version",
			Data: map[string]any{"supported": supportedVersions, "requested": version},
		})
	}
	mismatch := func(msg string) (int, []byte) {
		return reply(http.StatusBadRequest, req.ID, nil, &rpcError{Code: codeHeaderMismatch, Message: "Header mismatch: " + msg})
	}
	switch {
	case h.ProtocolVersion != version:
		return mismatch("MCP-Protocol-Version must match the request's protocol version")
	case h.Method != req.Method:
		return mismatch("Mcp-Method must match the request method")
	case req.Method == "tools/call" && decodeHeader(h.Name) != name:
		return mismatch("Mcp-Name must match the tool name")
	}
	if _, ok := meta["io.modelcontextprotocol/clientCapabilities"]; !ok {
		return reply(http.StatusBadRequest, req.ID, nil, &rpcError{Code: codeInvalidParams, Message: "_meta is missing io.modelcontextprotocol/clientCapabilities"})
	}

	var result map[string]any
	switch req.Method {
	case "server/discover":
		result = map[string]any{"supportedVersions": supportedVersions, "capabilities": capabilities(), "instructions": instructions}
	case "ping":
		result = map[string]any{}
	case "tools/list":
		result = map[string]any{"tools": s.list(c.User)}
	case "tools/call":
		r, rerr := s.call(ctx, c, req.Params)
		if rerr != nil {
			return reply(http.StatusOK, req.ID, nil, rerr)
		}
		result = r
	default:
		return reply(http.StatusNotFound, req.ID, nil, &rpcError{Code: codeMethodNotFound, Message: "Method not found: " + req.Method})
	}
	result["resultType"] = "complete"
	result["_meta"] = map[string]any{"io.modelcontextprotocol/serverInfo": serverInfo()}
	return reply(http.StatusOK, req.ID, result, nil)
}

// legacy serves the initialize-handshake revisions, statelessly.
func (s *Server) legacy(ctx context.Context, c Caller, h Headers, req request) (int, []byte) {
	if h.ProtocolVersion != "" && !slices.Contains(legacyVersions, h.ProtocolVersion) {
		if h.ProtocolVersion == ModernVersion {
			return reply(http.StatusBadRequest, req.ID, nil, &rpcError{Code: codeInvalidParams, Message: "_meta is missing io.modelcontextprotocol/protocolVersion"})
		}
		return reply(http.StatusBadRequest, req.ID, nil, &rpcError{
			Code: codeUnsupportedVers, Message: "Unsupported protocol version",
			Data: map[string]any{"supported": supportedVersions, "requested": h.ProtocolVersion},
		})
	}
	switch req.Method {
	case "initialize":
		var p struct {
			ProtocolVersion string `json:"protocolVersion"`
		}
		_ = json.Unmarshal(req.Params, &p)
		version := legacyVersions[0]
		if slices.Contains(legacyVersions, p.ProtocolVersion) {
			version = p.ProtocolVersion
		}
		return reply(http.StatusOK, req.ID, map[string]any{
			"protocolVersion": version, "capabilities": capabilities(), "serverInfo": serverInfo(), "instructions": instructions,
		}, nil)
	case "ping":
		return reply(http.StatusOK, req.ID, map[string]any{}, nil)
	case "tools/list":
		return reply(http.StatusOK, req.ID, map[string]any{"tools": s.list(c.User)}, nil)
	case "tools/call":
		r, rerr := s.call(ctx, c, req.Params)
		return reply(http.StatusOK, req.ID, r, rerr)
	}
	return reply(http.StatusOK, req.ID, nil, &rpcError{Code: codeMethodNotFound, Message: "Method not found: " + req.Method})
}

func capabilities() map[string]any {
	return map[string]any{"tools": map[string]any{"listChanged": false}}
}

func serverInfo() map[string]any {
	return map[string]any{"name": serverName, "title": serverTitle, "version": serverVersion}
}

func reply(status int, id json.RawMessage, result any, rerr *rpcError) (int, []byte) {
	b, err := json.Marshal(response{JSONRPC: "2.0", ID: id, Result: result, Error: rerr})
	if err != nil {
		b, _ = json.Marshal(response{JSONRPC: "2.0", ID: id, Error: &rpcError{Code: codeInternal, Message: "Internal error"}})
		status = http.StatusInternalServerError
	}
	return status, b
}

// decodeHeader undoes the =?base64?…?= sentinel the transport uses for unsafe values.
func decodeHeader(v string) string {
	if strings.HasPrefix(v, "=?base64?") && strings.HasSuffix(v, "?=") {
		if b, err := base64.StdEncoding.DecodeString(strings.TrimSuffix(strings.TrimPrefix(v, "=?base64?"), "?=")); err == nil {
			return string(b)
		}
	}
	return v
}

// call runs a tool. Problems with the call itself are JSON-RPC errors; everything the
// tool says no to (permissions, validation, workflow rules) is a result with isError,
// so the assistant can read it and correct itself.
func (s *Server) call(ctx context.Context, c Caller, raw json.RawMessage) (map[string]any, *rpcError) {
	var p struct {
		Name      string          `json:"name"`
		Arguments json.RawMessage `json:"arguments"`
	}
	if err := json.Unmarshal(raw, &p); err != nil || p.Name == "" {
		return nil, &rpcError{Code: codeInvalidParams, Message: "tools/call needs a tool name"}
	}
	t := s.tool(p.Name)
	if t == nil || !t.allowed(c.User) {
		return nil, &rpcError{Code: codeInvalidParams, Message: "Unknown tool: " + p.Name}
	}
	args := p.Arguments
	if len(args) == 0 || string(args) == "null" {
		args = json.RawMessage(`{}`)
	}
	if t.write {
		ctx = store.WithVia(ctx, store.Via{Channel: "mcp", Client: c.Client})
	}
	out, err := t.run(ctx, s, c.User, args)
	if err != nil {
		return toolError(err), nil
	}
	text, _ := json.MarshalIndent(out, "", "  ")
	return map[string]any{
		"content":           []map[string]any{{"type": "text", "text": string(text)}},
		"structuredContent": out,
		"isError":           false,
	}, nil
}

func toolError(err error) map[string]any {
	msg := "Something went wrong on the server. Try again, or do this in the portal."
	var ae *apperr.Error
	switch {
	case errors.As(err, &ae):
		msg = ae.Message
		if len(ae.Fields) > 0 {
			parts := make([]string, 0, len(ae.Fields))
			for k, v := range ae.Fields {
				parts = append(parts, k+" "+v)
			}
			slices.Sort(parts)
			msg += ": " + strings.Join(parts, "; ")
		}
	case errors.Is(err, errBadArgs):
		msg = err.Error()
	default:
		slog.Error("mcp tool failed", "err", err)
	}
	return map[string]any{"content": []map[string]any{{"type": "text", "text": msg}}, "isError": true}
}
