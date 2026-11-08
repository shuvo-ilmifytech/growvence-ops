package httpapi

import (
	"context"
	"slices"
	"strings"
	"time"

	"github.com/gofiber/fiber/v3"
	"github.com/google/uuid"

	"github.com/solvinx/opsflow/internal/auth"
	"github.com/solvinx/opsflow/internal/db"
	"github.com/solvinx/opsflow/internal/policy"
	"github.com/solvinx/opsflow/internal/store"
	"github.com/solvinx/opsflow/internal/validate"
)

const (
	loginWindow      = 15 * time.Minute
	maxFailsPerEmail = 8
	maxFailsPerIP    = 50 // failures only: an office shares one IP
)

// userView is the public shape of a user, with derived capabilities for the UI.
type userView struct {
	db.User
	Permissions []policy.Action `json:"permissions"`
}

func viewUser(u db.User) userView {
	perms := policy.ActionsFor(policy.Role(u.Role))
	if policy.CanViewAnalytics(policy.Role(u.Role), u.CanViewAnalytics) {
		perms = append(perms, policy.ViewAnalytics)
		slices.Sort(perms)
	}
	return userView{User: u, Permissions: perms}
}

type loginReq struct {
	Email    string `json:"email"`
	Password string `json:"password"`
}

func (s *Server) login(c fiber.Ctx) error {
	var req loginReq
	if err := decodeJSON(c, &req); err != nil {
		return err
	}
	req.Email = strings.ToLower(strings.TrimSpace(req.Email))
	v := validate.Errors{}
	v.Required("email", req.Email)
	v.Required("password", req.Password)
	v.MaxLen("password", req.Password, auth.MaxPasswordLen)
	if !v.OK() {
		return errValidation(v)
	}

	ctx := c.Context()
	emailKey, ipKey := "login_fail:"+req.Email, "login_ip:"+c.IP()
	if s.overLimit(ctx, ipKey, maxFailsPerIP) || s.overLimit(ctx, emailKey, maxFailsPerEmail) {
		return errTooMany("Too many sign-in attempts. Try again in 15 minutes.")
	}

	u, err := s.store.GetUserByEmail(ctx, req.Email)
	switch {
	case store.IsNotFound(err):
		auth.BurnVerify(req.Password)
		s.bump(ctx, emailKey, ipKey)
		return invalidCredentials()
	case err != nil:
		return err
	}
	if err := auth.VerifyPassword(req.Password, u.PasswordHash); err != nil || !u.IsActive {
		s.bump(ctx, emailKey, ipKey)
		return invalidCredentials()
	}
	s.rdb.Del(ctx, emailKey)

	token, err := s.sessions.Create(ctx, u.ID)
	if err != nil {
		return err
	}
	_ = s.store.TouchUserLogin(ctx, u.ID)
	_ = store.LogActivity(ctx, s.store.Queries, store.Activity{
		Actor: &u.ID, Action: "auth.login", EntityType: "user", EntityID: &u.ID,
	})
	s.setSessionCookie(c, token)
	return c.JSON(fiber.Map{"user": viewUser(u)})
}

func invalidCredentials() *APIError {
	return &APIError{Status: fiber.StatusUnauthorized, Code: "invalid_credentials", Message: "Email or password is incorrect"}
}

func (s *Server) overLimit(ctx context.Context, key string, limit int64) bool {
	n, _ := s.rdb.Get(ctx, key).Int64()
	return n >= limit
}

// bump counts a failed attempt against each key for the login window.
func (s *Server) bump(ctx context.Context, keys ...string) {
	pipe := s.rdb.TxPipeline()
	for _, k := range keys {
		pipe.Incr(ctx, k)
		pipe.ExpireNX(ctx, k, loginWindow)
	}
	_, _ = pipe.Exec(ctx)
}

func (s *Server) logout(c fiber.Ctx) error {
	if token := c.Cookies(sessionCookie); token != "" {
		if err := s.sessions.Destroy(c.Context(), token); err != nil {
			return err
		}
	}
	s.clearSessionCookie(c)
	return c.SendStatus(fiber.StatusNoContent)
}

func (s *Server) me(c fiber.Ctx) error {
	return c.JSON(fiber.Map{"user": viewUser(currentUser(c))})
}

type changePasswordReq struct {
	CurrentPassword string `json:"current_password"`
	NewPassword     string `json:"new_password"`
}

func (s *Server) changePassword(c fiber.Ctx) error {
	var req changePasswordReq
	if err := decodeJSON(c, &req); err != nil {
		return err
	}
	u := currentUser(c)
	v := validate.Errors{}
	v.Required("current_password", req.CurrentPassword)
	validatePassword(v, "new_password", req.NewPassword)
	if !v.OK() {
		return errValidation(v)
	}
	if auth.VerifyPassword(req.CurrentPassword, u.PasswordHash) != nil {
		return errValidation(validate.Errors{"current_password": "is incorrect"})
	}
	if err := s.setPassword(c.Context(), u.ID, req.NewPassword, c.Cookies(sessionCookie)); err != nil {
		return err
	}
	return c.SendStatus(fiber.StatusNoContent)
}

// setPassword hashes and stores a password, then ends every other session.
func (s *Server) setPassword(ctx context.Context, userID uuid.UUID, password, keepToken string) error {
	hash, err := auth.HashPassword(password)
	if err != nil {
		return err
	}
	if err := s.store.SetUserPassword(ctx, db.SetUserPasswordParams{ID: userID, PasswordHash: hash}); err != nil {
		return err
	}
	return s.sessions.DestroyAllForUser(ctx, userID, keepToken)
}

func validatePassword(v validate.Errors, field, pw string) {
	v.Len(field, pw, auth.MinPasswordLen, auth.MaxPasswordLen)
}

func (s *Server) setSessionCookie(c fiber.Ctx, token string) {
	c.Cookie(&fiber.Cookie{
		Name:     sessionCookie,
		Value:    token,
		Path:     "/",
		MaxAge:   int(s.cfg.SessionMaxAge.Seconds()),
		HTTPOnly: true,
		Secure:   s.cfg.CookieSecure,
		SameSite: fiber.CookieSameSiteLaxMode,
	})
}

func (s *Server) clearSessionCookie(c fiber.Ctx) {
	c.Cookie(&fiber.Cookie{
		Name: sessionCookie, Value: "", Path: "/", MaxAge: -1,
		HTTPOnly: true, Secure: s.cfg.CookieSecure, SameSite: fiber.CookieSameSiteLaxMode,
	})
}
