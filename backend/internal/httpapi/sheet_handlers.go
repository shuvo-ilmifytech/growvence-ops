package httpapi

import (
	"github.com/gofiber/fiber/v3"

	"github.com/solvinx/opsflow/internal/apperr"
	"github.com/solvinx/opsflow/internal/sheets"
)

// sheetStatus reports the Google Sheet backup: whether it's switched on, how much is
// waiting, when it last wrote, and the last error. Works with no sheet configured.
func (s *Server) sheetStatus(c fiber.Ctx) error {
	out, err := sheets.ReadStatus(c.Context(), s.store, s.sheets)
	if err != nil {
		return err
	}
	return c.JSON(out)
}

// sheetCheck proves the credentials work and the sheet is reachable, writing nothing.
func (s *Server) sheetCheck(c fiber.Ctx) error {
	if s.sheets == nil {
		return apperr.BadRequest("No Google Sheet is configured")
	}
	if err := s.sheets.Check(c.Context()); err != nil {
		return apperr.BadRequest(err.Error())
	}
	return c.JSON(fiber.Map{"ok": true, "share_with": s.sheets.ServiceAccountEmail()})
}

// sheetBackfill writes every project to the sheet again.
func (s *Server) sheetBackfill(c fiber.Ctx) error {
	if s.backup == nil {
		return apperr.BadRequest("No Google Sheet is configured")
	}
	n, err := s.backup.Backfill(c.Context())
	if err != nil {
		return apperr.BadRequest(err.Error())
	}
	return c.JSON(fiber.Map{"projects": n})
}
