// Package chat is one-to-one messaging between people in the organisation, and the
// signalling that sets up a voice call between two browsers.
package chat

import (
	"context"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"

	"github.com/solvinx/opsflow/internal/apperr"
	"github.com/solvinx/opsflow/internal/db"
	"github.com/solvinx/opsflow/internal/live"
	"github.com/solvinx/opsflow/internal/storage"
	"github.com/solvinx/opsflow/internal/store"
	"github.com/solvinx/opsflow/internal/validate"
)

const (
	maxBody     = 4000
	threadPage  = 50
	inboxLimit  = 100
	recentCalls = 50
)

type Service struct {
	st        *store.Store
	bus       *live.Bus
	files     *storage.Storage
	rdb       *redis.Client
	maxUpload int64
}

func NewService(st *store.Store, bus *live.Bus, files *storage.Storage, rdb *redis.Client, maxUpload int64) *Service {
	return &Service{st: st, bus: bus, files: files, rdb: rdb, maxUpload: maxUpload}
}

// ── Messages ───────────────────────────────────────────────

type MessageOut struct {
	ID          uuid.UUID       `json:"id"`
	SenderID    uuid.UUID       `json:"sender_id"`
	SenderName  string          `json:"sender_name"`
	Body        string          `json:"body"`
	CreatedAt   time.Time       `json:"created_at"`
	ReadAt      *time.Time      `json:"read_at"`
	Mine        bool            `json:"mine"`
	Attachments []AttachmentOut `json:"attachments"`
}

type Thread struct {
	User     Person       `json:"user"`
	Messages []MessageOut `json:"messages"` // oldest first, ready to render
	// Finished calls between the two in the same stretch of time as Messages, oldest
	// first, so the page can show them in the conversation.
	Calls   []CallOut `json:"calls"`
	HasMore bool      `json:"has_more"`
}

type Person struct {
	ID       uuid.UUID `json:"id"`
	Name     string    `json:"name"`
	Role     string    `json:"role"`
	IsActive bool      `json:"is_active"`
}

// Send writes a message to one person, with any files uploaded for it. Anyone signed
// in may message any active colleague; that's the point of an internal chat.
func (s *Service) Send(ctx context.Context, from db.User, to uuid.UUID, body string, uploads []string) (MessageOut, error) {
	body = strings.TrimSpace(body)
	if err := validBody(body, len(uploads)); err != nil {
		return MessageOut{}, err
	}
	if to == from.ID {
		return MessageOut{}, apperr.Validation(validate.Errors{"to": "cannot be yourself"})
	}
	other, err := s.person(ctx, to)
	if err != nil {
		return MessageOut{}, err
	}
	if !other.IsActive {
		return MessageOut{}, apperr.BadRequest(other.Name + " no longer has access")
	}
	ups, err := s.claimUploads(ctx, from, uploads)
	if err != nil {
		return MessageOut{}, err
	}
	var out MessageOut
	err = s.st.Tx(ctx, func(q *db.Queries) error {
		m, err := q.SendMessage(ctx, db.SendMessageParams{SenderID: from.ID, RecipientID: to, Body: body})
		if err != nil {
			return err
		}
		out = MessageOut{ID: m.ID, SenderID: m.SenderID, SenderName: from.Name, Body: m.Body, CreatedAt: m.CreatedAt, Mine: true}
		out.Attachments, err = saveAttachments(ctx, q, from, &m.ID, nil, ups)
		return err
	})
	if err != nil {
		s.discard(ctx, ups)
		return MessageOut{}, err
	}
	// The recipient's open tabs show it at once; the sender's other tabs too.
	s.bus.Publish(ctx, to, "message", map[string]any{"from": from.ID, "from_name": from.Name, "message": out})
	s.bus.Publish(ctx, from.ID, "message", map[string]any{"from": from.ID, "from_name": from.Name, "with": to, "message": out})
	return out, nil
}

// Thread is the conversation with one person, oldest message last read first; opening
// it marks what they sent as read.
func (s *Service) Thread(ctx context.Context, me db.User, other uuid.UUID, before *time.Time) (Thread, error) {
	who, err := s.person(ctx, other)
	if err != nil {
		return Thread{}, err
	}
	rows, err := s.st.MessageThread(ctx, db.MessageThreadParams{
		Me: me.ID, Other: other, Before: before, Lim: threadPage + 1,
	})
	if err != nil {
		return Thread{}, err
	}
	out := Thread{User: who, Messages: []MessageOut{}, Calls: []CallOut{}, HasMore: len(rows) > threadPage}
	if out.HasMore {
		rows = rows[:threadPage]
	}
	ids := make([]uuid.UUID, len(rows))
	for i, r := range rows {
		ids[i] = r.ID
	}
	files, err := s.st.AttachmentsOfMessages(ctx, ids)
	if err != nil {
		return Thread{}, err
	}
	attached := byMessage(files, false)
	// The query reads newest first; the page wants oldest first.
	for i := len(rows) - 1; i >= 0; i-- {
		r := rows[i]
		out.Messages = append(out.Messages, MessageOut{
			ID: r.ID, SenderID: r.SenderID, SenderName: r.SenderName, Body: r.Body,
			CreatedAt: r.CreatedAt, ReadAt: r.ReadAt, Mine: r.SenderID == me.ID,
			Attachments: orEmpty(attached[r.ID]),
		})
	}
	// Calls from the same stretch as this page: back to its oldest message when older
	// pages remain, otherwise all the way back.
	var after *time.Time
	if out.HasMore && len(rows) > 0 {
		after = &rows[len(rows)-1].CreatedAt
	}
	calls, err := s.st.CallsBetween(ctx, db.CallsBetweenParams{Me: me.ID, Other: other, After: after, Before: before, Lim: recentCalls})
	if err != nil {
		return Thread{}, err
	}
	for i := len(calls) - 1; i >= 0; i-- {
		c := calls[i]
		out.Calls = append(out.Calls, callOut(me.ID, db.GetCallRow{
			ID: c.ID, CallerID: c.CallerID, CalleeID: c.CalleeID, Status: c.Status, StartedAt: c.StartedAt,
			AnsweredAt: c.AnsweredAt, EndedAt: c.EndedAt, CallerName: c.CallerName, CalleeName: c.CalleeName,
		}))
	}
	return out, nil
}

func orEmpty(a []AttachmentOut) []AttachmentOut {
	if a == nil {
		return []AttachmentOut{}
	}
	return a
}

// MarkRead marks everything that person sent as read, and tells the sender.
func (s *Service) MarkRead(ctx context.Context, me db.User, other uuid.UUID) error {
	n, err := s.st.MarkThreadRead(ctx, db.MarkThreadReadParams{Me: me.ID, Other: other})
	if err != nil {
		return err
	}
	if n > 0 {
		s.bus.Publish(ctx, other, "message_read", map[string]any{"by": me.ID})
	}
	return nil
}

type Inbox struct {
	Threads []InboxThread `json:"threads"`
	Unread  int64         `json:"unread"`
}

type InboxThread struct {
	db.InboxRow
}

func (s *Service) Inbox(ctx context.Context, me db.User) (Inbox, error) {
	rows, err := s.st.Inbox(ctx, db.InboxParams{Me: me.ID, Lim: inboxLimit})
	if err != nil {
		return Inbox{}, err
	}
	out := Inbox{Threads: make([]InboxThread, len(rows))}
	for i, r := range rows {
		out.Threads[i] = InboxThread{InboxRow: r}
	}
	out.Unread, err = s.st.CountUnreadMessages(ctx, me.ID)
	if err != nil {
		return out, err
	}
	// One badge covers both: a group nobody has read is still something waiting.
	inGroups, err := s.st.CountUnreadGroupMessages(ctx, me.ID)
	out.Unread += inGroups
	return out, err
}

func (s *Service) person(ctx context.Context, id uuid.UUID) (Person, error) {
	u, err := s.st.GetUserByID(ctx, id)
	if store.IsNotFound(err) {
		return Person{}, apperr.NotFound("Person")
	} else if err != nil {
		return Person{}, err
	}
	return Person{ID: u.ID, Name: u.Name, Role: u.Role, IsActive: u.IsActive}, nil
}

// callOut is out() for code where a local named out shadows it.
func callOut(me uuid.UUID, c db.GetCallRow) CallOut { return out(c, me) }
