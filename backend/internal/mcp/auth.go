package mcp

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"slices"
	"strings"
	"syscall"
	"time"
)

// Token prefixes say what a secret is at a glance (and let secret scanners spot them).
const (
	PersonalPrefix = "ofk_" // personal token, made on the Account page
	AccessPrefix   = "ofa_" // OAuth access token
	RefreshPrefix  = "ofr_" // OAuth refresh token
	CodePrefix     = "ofc_" // OAuth authorization code

	AccessTTL  = time.Hour
	RefreshTTL = 30 * 24 * time.Hour
	CodeTTL    = 10 * time.Minute
)

// NewSecret returns a random token with the given prefix. 32 random bytes: hashing it
// with plain SHA-256 is enough, no password hashing needed.
func NewSecret(prefix string) string {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		panic(err) // crypto/rand never fails on supported platforms
	}
	return prefix + base64.RawURLEncoding.EncodeToString(b)
}

// Hash is how a secret is stored and looked up.
func Hash(secret string) string {
	sum := sha256.Sum256([]byte(secret))
	return hex.EncodeToString(sum[:])
}

// VerifyPKCE checks an S256 code verifier against the challenge sent with the authorization request.
func VerifyPKCE(verifier, challenge string) bool {
	if len(verifier) < 43 || len(verifier) > 128 {
		return false
	}
	sum := sha256.Sum256([]byte(verifier))
	return base64.RawURLEncoding.EncodeToString(sum[:]) == challenge
}

// ValidRedirect: https anywhere, or http on the loopback interface for local tools
// (Claude Code, Codex). No fragments, no credentials in the URL.
func ValidRedirect(raw string) bool {
	u, err := url.Parse(raw)
	if err != nil || u.Fragment != "" || u.User != nil || u.Host == "" {
		return false
	}
	switch u.Scheme {
	case "https":
		return true
	case "http":
		h := u.Hostname()
		return h == "localhost" || h == "127.0.0.1" || h == "::1"
	}
	return false
}

// RedirectMatches: exact string match, except that a loopback redirect may use any port
// (native apps pick a free port at run time; RFC 8252 section 7.3).
func RedirectMatches(registered []string, got string) bool {
	if slices.Contains(registered, got) {
		return true
	}
	g, err := url.Parse(got)
	if err != nil || g.Scheme != "http" {
		return false
	}
	for _, r := range registered {
		u, err := url.Parse(r)
		if err != nil || u.Scheme != "http" || !ValidRedirect(r) {
			continue
		}
		if u.Hostname() == g.Hostname() && u.Path == g.Path && u.RawQuery == g.RawQuery {
			return true
		}
	}
	return false
}

// ── client ID metadata documents ───────────────────────────

// ClientDoc is the part of a client ID metadata document OpsFlow uses.
type ClientDoc struct {
	ClientID     string   `json:"client_id"`
	ClientName   string   `json:"client_name"`
	RedirectURIs []string `json:"redirect_uris"`
}

// IsMetadataClientID: an https URL with a path is a client ID metadata document.
func IsMetadataClientID(id string) bool {
	u, err := url.Parse(id)
	return err == nil && u.Scheme == "https" && u.Host != "" && u.Path != "" && u.Path != "/" && u.Fragment == ""
}

// FetchClientDoc fetches a client's metadata document. The URL comes from whoever starts
// an authorization, so the fetch must not reach this server or any private network:
// every address the name resolves to is checked when the connection is made, there are
// no redirects, and size and time are capped.
func FetchClientDoc(ctx context.Context, id string) (ClientDoc, error) {
	var doc ClientDoc
	if !IsMetadataClientID(id) {
		return doc, errors.New("client_id is not an https URL with a path")
	}
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, id, nil)
	if err != nil {
		return doc, err
	}
	req.Header.Set("Accept", "application/json")
	res, err := safeClient.Do(req)
	if err != nil {
		return doc, fmt.Errorf("could not fetch the client metadata document: %w", err)
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		return doc, fmt.Errorf("the client metadata document answered %d", res.StatusCode)
	}
	body, err := io.ReadAll(io.LimitReader(res.Body, 64<<10+1))
	if err != nil {
		return doc, err
	}
	if len(body) > 64<<10 {
		return doc, errors.New("the client metadata document is too large")
	}
	if err := json.Unmarshal(body, &doc); err != nil {
		return doc, errors.New("the client metadata document is not valid JSON")
	}
	switch {
	case doc.ClientID != id:
		return doc, errors.New("the client metadata document's client_id doesn't match its URL")
	case strings.TrimSpace(doc.ClientName) == "":
		return doc, errors.New("the client metadata document has no client_name")
	case len(doc.RedirectURIs) == 0 || len(doc.RedirectURIs) > 20:
		return doc, errors.New("the client metadata document needs 1 to 20 redirect_uris")
	}
	for _, r := range doc.RedirectURIs {
		if !ValidRedirect(r) {
			return doc, fmt.Errorf("redirect URI %q is not allowed", r)
		}
	}
	return doc, nil
}

var safeClient = &http.Client{
	Timeout: 5 * time.Second,
	CheckRedirect: func(*http.Request, []*http.Request) error {
		return errors.New("redirects are not followed")
	},
	Transport: &http.Transport{
		Proxy: nil,
		DialContext: (&net.Dialer{
			Timeout: 3 * time.Second,
			// Runs after DNS resolution, on the address actually dialled, so a name
			// that resolves (or re-resolves) to a private address is refused.
			Control: func(_, address string, _ syscall.RawConn) error {
				ap, err := netip.ParseAddrPort(address)
				if err != nil {
					return err
				}
				if !PublicAddr(ap.Addr()) {
					return fmt.Errorf("address %s is not public", ap.Addr())
				}
				return nil
			},
		}).DialContext,
		TLSHandshakeTimeout:    3 * time.Second,
		ResponseHeaderTimeout:  3 * time.Second,
		MaxResponseHeaderBytes: 16 << 10,
	},
}

// PublicAddr reports whether a is a globally routable unicast address.
func PublicAddr(a netip.Addr) bool {
	a = a.Unmap()
	if !a.IsValid() || a.IsLoopback() || a.IsPrivate() || a.IsLinkLocalUnicast() || a.IsLinkLocalMulticast() ||
		a.IsMulticast() || a.IsUnspecified() || a.IsInterfaceLocalMulticast() {
		return false
	}
	for _, p := range blockedPrefixes {
		if p.Contains(a) {
			return false
		}
	}
	return true
}

var blockedPrefixes = []netip.Prefix{
	netip.MustParsePrefix("0.0.0.0/8"),
	netip.MustParsePrefix("100.64.0.0/10"), // carrier-grade NAT
	netip.MustParsePrefix("192.0.0.0/24"),
	netip.MustParsePrefix("192.0.2.0/24"),
	netip.MustParsePrefix("198.18.0.0/15"),
	netip.MustParsePrefix("198.51.100.0/24"),
	netip.MustParsePrefix("203.0.113.0/24"),
	netip.MustParsePrefix("240.0.0.0/4"),
	netip.MustParsePrefix("64:ff9b::/96"), // NAT64 can reach private IPv4
	netip.MustParsePrefix("2001:db8::/32"),
}
