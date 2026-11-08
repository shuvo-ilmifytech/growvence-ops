package httpapi

import (
	"errors"
	"net/url"
	"slices"
	"strings"

	"github.com/gofiber/fiber/v3"

	"github.com/solvinx/opsflow/internal/auth"
	"github.com/solvinx/opsflow/internal/db"
	"github.com/solvinx/opsflow/internal/policy"
	"github.com/solvinx/opsflow/internal/store"
)

type ctxKey int

const userKey ctxKey = iota

// originGuard is the CSRF defence: state-changing requests must come from an
// allowed browser origin. Combined with SameSite=Lax cookies.
func (s *Server) originGuard(c fiber.Ctx) error {
	switch c.Method() {
	case fiber.MethodGet, fiber.MethodHead, fiber.MethodOptions:
		return c.Next()
	}
	origin := c.Get(fiber.HeaderOrigin)
	if origin == "" {
		if ref, err := url.Parse(c.Get(fiber.HeaderReferer)); err == nil && ref.Host != "" {
			origin = ref.Scheme + "://" + ref.Host
		}
	}
	if !slices.Contains(s.cfg.AppOrigins, origin) {
		return &APIError{Status: fiber.StatusForbidden, Code: "bad_origin", Message: "Request origin not allowed"}
	}
	return c.Next()
}

// requireAuth resolves the session cookie to an active user. The user row is
// re-read on every request so role changes and deactivation apply immediately.
func (s *Server) requireAuth(c fiber.Ctx) error {
	sess, err := s.sessions.Get(c.Context(), c.Cookies(sessionCookie))
	if errors.Is(err, auth.ErrNoSession) {
		return errUnauthorized()
	} else if err != nil {
		return err
	}
	u, err := s.store.GetUserByID(c.Context(), sess.UserID)
	if store.IsNotFound(err) || (err == nil && !u.IsActive) {
		_ = s.sessions.Destroy(c.Context(), c.Cookies(sessionCookie))
		return errUnauthorized()
	} else if err != nil {
		return err
	}
	c.Locals(userKey, u)
	return c.Next()
}

// readOnlyGuard enforces "the CEO oversees projects but never operates them" before any
// handler runs. The CEO's writes are limited to their own account, read state, people/HR,
// finance and administration.
func readOnlyGuard(c fiber.Ctx) error {
	switch c.Method() {
	case fiber.MethodGet, fiber.MethodHead, fiber.MethodOptions:
		return c.Next()
	}
	u := currentUser(c)
	if !policy.IsReadOnly(roleOf(u)) {
		return c.Next()
	}
	path := c.Path()
	switch {
	case path == "/api/auth/password",
		path == "/api/notifications/read-all",
		strings.HasPrefix(path, "/api/notifications/") && strings.HasSuffix(path, "/read"),
		strings.HasPrefix(path, "/api/projects/") && strings.HasSuffix(path, "/view"),
		strings.HasPrefix(path, "/api/cards/") && strings.HasSuffix(path, "/opened"),
		// Talking to people is not operating a project: anyone may message or ring anyone.
		path == "/api/groups",
		strings.HasPrefix(path, "/api/messages/"),
		strings.HasPrefix(path, "/api/chat/"),
		// Their own assistant connections are the CEO's to manage, like their password.
		strings.HasPrefix(path, "/api/account/"),
		strings.HasPrefix(path, "/api/groups/"),
		strings.HasPrefix(path, "/api/calls/"),
		strings.HasPrefix(path, "/api/hr/"),
		// Finance is HR and the CEO's to keep, like salaries.
		strings.HasPrefix(path, "/api/finance/"),
		// The Investor Finance Forum is the CEO's to run (shares, reserve,
		// withdrawals), like finance.
		strings.HasPrefix(path, "/api/investors"),
		strings.HasPrefix(path, "/api/investor/"),
		strings.HasPrefix(path, "/api/withdrawals/"),
		strings.HasPrefix(path, "/api/profit-settings"),
		strings.HasPrefix(path, "/api/company-account"),
		strings.HasPrefix(path, "/api/investor-expenses"),
		strings.HasPrefix(path, "/api/currency-rates"),
		// Setting the teams' revenue targets is the CEO's.
		strings.HasPrefix(path, "/api/analytics/targets/"),
		strings.HasPrefix(path, "/api/admin/"),
		// Fiverr reviews and the knowledge base are the CEO's to keep.
		strings.HasPrefix(path, "/api/reviews"),
		strings.HasPrefix(path, "/api/kb"):
		return c.Next()
	}
	return errForbidden()
}

func currentUser(c fiber.Ctx) db.User {
	return fiber.Locals[db.User](c, userKey)
}

func roleOf(u db.User) policy.Role { return policy.Role(u.Role) }

// isSystemAdmin: system setup (marketplace profiles, project types, app settings) is for
// users with the admin flag and the CEO.
func isSystemAdmin(u db.User) bool { return u.IsAdmin || roleOf(u) == policy.CEO }

// canViewInternal reports whether the caller is a member of the internal
// team (a ProjectRole: sales | tl | member | ceo | hr, or a system admin).
// Client, investor and office_admin callers are external to the internal
// tooling: knowledge base, Fiverr reviews, templates and recurring tasks are
// internal-only surfaces. Sprint 1 (P0-8).
func canViewInternal(u db.User) bool {
	if isSystemAdmin(u) {
		return true
	}
	switch roleOf(u) {
	case policy.Sales, policy.TL, policy.Member, policy.CEO, policy.HR:
		return true
	default:
		return false
	}
}

func (s *Server) requireAdmin(c fiber.Ctx) error {
	if !isSystemAdmin(currentUser(c)) {
		return errForbidden()
	}
	return c.Next()
}

// requirePeople gates accounts, roles and HR configuration: HR and the CEO.
func (s *Server) requirePeople(c fiber.Ctx) error {
	if !policy.ManagesPeople(roleOf(currentUser(c))) {
		return errForbidden()
	}
	return c.Next()
}

// requireAnalytics gates the Analytics page: the CEO, and whoever the CEO granted it to.
func (s *Server) requireAnalytics(c fiber.Ctx) error {
	u := currentUser(c)
	if !policy.CanViewAnalytics(roleOf(u), u.CanViewAnalytics) {
		return errForbidden()
	}
	return c.Next()
}

// requireCan gates a route on a role capability. Resource-level checks
// (ownership, assignment, status) still happen in the handler.
func requireCan(a policy.Action) fiber.Handler {
	return func(c fiber.Ctx) error {
		if !policy.Can(roleOf(currentUser(c)), a) {
			return errForbidden()
		}
		return c.Next()
	}
}
