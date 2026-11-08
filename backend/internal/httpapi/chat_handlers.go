package httpapi

import (
	"encoding/json"
	"time"

	"github.com/gofiber/fiber/v3"
	"github.com/google/uuid"

	"github.com/solvinx/opsflow/internal/apperr"
	"github.com/solvinx/opsflow/internal/projects"
)

// Chat and voice calls. Every rule lives in the chat service.

func personParam(c fiber.Ctx) (uuid.UUID, error) {
	id, err := uuid.Parse(c.Params("uid"))
	if err != nil {
		return uuid.Nil, apperr.NotFound("Person")
	}
	return id, nil
}

func (s *Server) inbox(c fiber.Ctx) error {
	out, err := s.chat.Inbox(c.Context(), currentUser(c))
	if err != nil {
		return err
	}
	return c.JSON(out)
}

func (s *Server) messageThread(c fiber.Ctx) error {
	other, err := personParam(c)
	if err != nil {
		return err
	}
	var before *time.Time
	if raw := c.Query("before"); raw != "" {
		t, err := time.Parse(time.RFC3339, raw)
		if err != nil {
			return apperr.BadRequest("before must be a timestamp")
		}
		before = &t
	}
	out, err := s.chat.Thread(c.Context(), currentUser(c), other, before)
	if err != nil {
		return err
	}
	return c.JSON(out)
}

// sendReq is a chat message: text, files uploaded for it (their upload ids), or both.
type sendReq struct {
	Body        string   `json:"body"`
	Attachments []string `json:"attachments"`
}

// initChatUpload hands out a presigned URL for one file to send in chat.
func (s *Server) initChatUpload(c fiber.Ctx) error {
	var req projects.UploadInput
	if err := decodeJSON(c, &req); err != nil {
		return err
	}
	t, err := s.chat.InitUpload(c.Context(), currentUser(c), req)
	if err != nil {
		return err
	}
	return c.Status(fiber.StatusCreated).JSON(t)
}

// chatAttachment redirects to a short-lived link for a file sent in chat, so it works
// both as a download link and as an image source.
func (s *Server) chatAttachment(c fiber.Ctx) error {
	id, err := uuid.Parse(c.Params("aid"))
	if err != nil {
		return apperr.NotFound("File")
	}
	url, err := s.chat.AttachmentURL(c.Context(), currentUser(c), id)
	if err != nil {
		return err
	}
	return c.Redirect().Status(fiber.StatusFound).To(url)
}

func (s *Server) sendMessage(c fiber.Ctx) error {
	other, err := personParam(c)
	if err != nil {
		return err
	}
	var req sendReq
	if err := decodeJSON(c, &req); err != nil {
		return err
	}
	m, err := s.chat.Send(c.Context(), currentUser(c), other, req.Body, req.Attachments)
	if err != nil {
		return err
	}
	return c.Status(fiber.StatusCreated).JSON(fiber.Map{"message": m})
}

func (s *Server) readThread(c fiber.Ctx) error {
	other, err := personParam(c)
	if err != nil {
		return err
	}
	if err := s.chat.MarkRead(c.Context(), currentUser(c), other); err != nil {
		return err
	}
	return c.SendStatus(fiber.StatusNoContent)
}

// ── Calls ──────────────────────────────────────────────────

// callIce gives the browser the servers it needs to find a path to the other side.
// STUN is enough on most networks; TURN relays the audio when it isn't (configure it
// with TURN_URL / TURN_USERNAME / TURN_PASSWORD).
func (s *Server) callIce(c fiber.Ctx) error {
	servers := []fiber.Map{{"urls": s.cfg.StunURLs}}
	if s.cfg.TurnURL != "" {
		servers = append(servers, fiber.Map{
			"urls": s.cfg.TurnURL, "username": s.cfg.TurnUsername, "credential": s.cfg.TurnPassword,
		})
	}
	return c.JSON(fiber.Map{"ice_servers": servers, "has_turn": s.cfg.TurnURL != ""})
}

func (s *Server) startCall(c fiber.Ctx) error {
	other, err := personParam(c)
	if err != nil {
		return err
	}
	call, err := s.chat.Call(c.Context(), currentUser(c), other)
	if err != nil {
		return err
	}
	return c.Status(fiber.StatusCreated).JSON(fiber.Map{"call": call})
}

func (s *Server) callStatus(c fiber.Ctx) error {
	id, err := uuid.Parse(c.Params("call_id"))
	if err != nil {
		return apperr.NotFound("Call")
	}
	var req struct {
		Status string `json:"status"`
	}
	if err := decodeJSON(c, &req); err != nil {
		return err
	}
	call, err := s.chat.SetStatus(c.Context(), currentUser(c), id, req.Status)
	if err != nil {
		return err
	}
	return c.JSON(fiber.Map{"call": call})
}

// callSignal carries one WebRTC offer, answer or candidate to the other end.
func (s *Server) callSignal(c fiber.Ctx) error {
	id, err := uuid.Parse(c.Params("call_id"))
	if err != nil {
		return apperr.NotFound("Call")
	}
	var req struct {
		Kind    string          `json:"kind"`
		Payload json.RawMessage `json:"payload"`
	}
	if err := decodeJSON(c, &req); err != nil {
		return err
	}
	if err := s.chat.Signal(c.Context(), currentUser(c), id, req.Kind, req.Payload); err != nil {
		return err
	}
	return c.SendStatus(fiber.StatusNoContent)
}

// liveCall lets a browser that reloaded mid-call find its way back.
func (s *Server) liveCall(c fiber.Ctx) error {
	call, err := s.chat.Live(c.Context(), currentUser(c))
	if err != nil {
		return err
	}
	return c.JSON(fiber.Map{"call": call})
}

func (s *Server) recentCalls(c fiber.Ctx) error {
	calls, err := s.chat.Recent(c.Context(), currentUser(c))
	if err != nil {
		return err
	}
	return c.JSON(fiber.Map{"calls": calls})
}

// ── Group chat ─────────────────────────────────────────────

func groupParam(c fiber.Ctx) (uuid.UUID, error) {
	id, err := uuid.Parse(c.Params("gid"))
	if err != nil {
		return uuid.Nil, apperr.NotFound("Group")
	}
	return id, nil
}

func (s *Server) listGroups(c fiber.Ctx) error {
	out, err := s.chat.Groups(c.Context(), currentUser(c))
	if err != nil {
		return err
	}
	return c.JSON(fiber.Map{"groups": out})
}

func (s *Server) createGroup(c fiber.Ctx) error {
	var req struct {
		Name      string      `json:"name"`
		MemberIDs []uuid.UUID `json:"member_ids"`
	}
	if err := decodeJSON(c, &req); err != nil {
		return err
	}
	g, err := s.chat.CreateGroup(c.Context(), currentUser(c), req.Name, req.MemberIDs)
	if err != nil {
		return err
	}
	return c.Status(fiber.StatusCreated).JSON(fiber.Map{"group": g})
}

func (s *Server) groupThread(c fiber.Ctx) error {
	id, err := groupParam(c)
	if err != nil {
		return err
	}
	var before *time.Time
	if raw := c.Query("before"); raw != "" {
		t, err := time.Parse(time.RFC3339, raw)
		if err != nil {
			return apperr.BadRequest("before must be a timestamp")
		}
		before = &t
	}
	out, err := s.chat.GroupThread(c.Context(), currentUser(c), id, before)
	if err != nil {
		return err
	}
	return c.JSON(out)
}

func (s *Server) sendToGroup(c fiber.Ctx) error {
	id, err := groupParam(c)
	if err != nil {
		return err
	}
	var req sendReq
	if err := decodeJSON(c, &req); err != nil {
		return err
	}
	m, err := s.chat.SendToGroup(c.Context(), currentUser(c), id, req.Body, req.Attachments)
	if err != nil {
		return err
	}
	return c.Status(fiber.StatusCreated).JSON(fiber.Map{"message": m})
}

func (s *Server) readGroup(c fiber.Ctx) error {
	id, err := groupParam(c)
	if err != nil {
		return err
	}
	if err := s.chat.MarkGroupRead(c.Context(), currentUser(c), id); err != nil {
		return err
	}
	return c.SendStatus(fiber.StatusNoContent)
}

func (s *Server) renameGroup(c fiber.Ctx) error {
	id, err := groupParam(c)
	if err != nil {
		return err
	}
	var req struct {
		Name string `json:"name"`
	}
	if err := decodeJSON(c, &req); err != nil {
		return err
	}
	g, err := s.chat.RenameGroup(c.Context(), currentUser(c), id, req.Name)
	if err != nil {
		return err
	}
	return c.JSON(fiber.Map{"group": g})
}

func (s *Server) addGroupMembers(c fiber.Ctx) error {
	id, err := groupParam(c)
	if err != nil {
		return err
	}
	var req struct {
		UserIDs []uuid.UUID `json:"user_ids"`
	}
	if err := decodeJSON(c, &req); err != nil {
		return err
	}
	if err := s.chat.AddToGroup(c.Context(), currentUser(c), id, req.UserIDs); err != nil {
		return err
	}
	return c.SendStatus(fiber.StatusNoContent)
}

func (s *Server) removeGroupMember(c fiber.Ctx) error {
	id, err := groupParam(c)
	if err != nil {
		return err
	}
	who, err := personParam(c)
	if err != nil {
		return err
	}
	if err := s.chat.RemoveFromGroup(c.Context(), currentUser(c), id, who); err != nil {
		return err
	}
	return c.SendStatus(fiber.StatusNoContent)
}
