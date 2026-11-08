package chat

import (
	"context"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/solvinx/opsflow/internal/apperr"
	"github.com/solvinx/opsflow/internal/db"
	"github.com/solvinx/opsflow/internal/store"
	"github.com/solvinx/opsflow/internal/validate"
)

const (
	maxGroupName = 80
	maxGroupSize = 200
)

// A group is a named conversation anyone can start — one for Sales, one for Ops.
// Everyone inside it can read it, write to it and add people; nobody outside it
// can see that it exists. Whoever started it also removes people and renames it.

type Group struct {
	ID          uuid.UUID  `json:"id"`
	Name        string     `json:"name"`
	CreatedBy   uuid.UUID  `json:"created_by"`
	CreatedAt   time.Time  `json:"created_at"`
	MemberCount int64      `json:"member_count"`
	LastBody    *string    `json:"last_body"`
	LastAt      *time.Time `json:"last_at"`
	LastSender  *string    `json:"last_sender_name"`
	LastFromMe  bool       `json:"last_from_me"`
	Unread      int64      `json:"unread"`
	Mine        bool       `json:"mine"` // started by the person reading it
}

type GroupMember struct {
	Person
	AddedAt time.Time `json:"added_at"`
}

type GroupThread struct {
	Group    Group         `json:"group"`
	Members  []GroupMember `json:"members"`
	Messages []MessageOut  `json:"messages"` // oldest last read first, ready to render
	HasMore  bool          `json:"has_more"`
}

// Groups lists the groups this person belongs to, busiest conversation first.
func (s *Service) Groups(ctx context.Context, me db.User) ([]Group, error) {
	rows, err := s.st.MyChatGroups(ctx, me.ID)
	if err != nil {
		return nil, err
	}
	out := make([]Group, len(rows))
	for i, r := range rows {
		out[i] = Group{
			ID: r.ID, Name: r.Name, CreatedBy: r.CreatedBy, CreatedAt: r.CreatedAt,
			MemberCount: r.MemberCount, LastBody: r.LastBody, LastAt: r.LastAt,
			LastSender: r.LastSenderName, LastFromMe: r.LastFromMe != nil && *r.LastFromMe,
			Unread: r.Unread, Mine: r.CreatedBy == me.ID,
		}
	}
	return out, nil
}

// CreateGroup starts one. The person starting it is always in it.
func (s *Service) CreateGroup(ctx context.Context, me db.User, name string, members []uuid.UUID) (Group, error) {
	name = strings.TrimSpace(name)
	v := validate.Errors{}
	if name == "" {
		v.Add("name", "is required")
	} else if len([]rune(name)) > maxGroupName {
		v.Add("name", "is too long")
	}
	if len(members) > maxGroupSize {
		v.Add("member_ids", "is too many people")
	}
	if !v.OK() {
		return Group{}, apperr.Validation(v)
	}
	for _, id := range members {
		if _, err := s.person(ctx, id); err != nil {
			return Group{}, err
		}
	}
	g, err := s.st.CreateChatGroup(ctx, db.CreateChatGroupParams{Name: name, CreatedBy: me.ID})
	if err != nil {
		return Group{}, err
	}
	if err := s.addMembers(ctx, g.ID, append([]uuid.UUID{me.ID}, members...)); err != nil {
		return Group{}, err
	}
	s.tellGroup(ctx, g.ID, me.ID, "group_changed", map[string]any{"group": g.ID, "name": g.Name})
	return Group{ID: g.ID, Name: g.Name, CreatedBy: g.CreatedBy, CreatedAt: g.CreatedAt,
		MemberCount: int64(len(members) + 1), Mine: true}, nil
}

func (s *Service) addMembers(ctx context.Context, group uuid.UUID, ids []uuid.UUID) error {
	for _, id := range ids {
		if err := s.st.AddChatGroupMember(ctx, db.AddChatGroupMemberParams{GroupID: group, UserID: id}); err != nil {
			return err
		}
	}
	return nil
}

// Thread is a group's conversation. Only its members may read it, and reading it
// marks everything in it as seen for the reader.
func (s *Service) GroupThread(ctx context.Context, me db.User, id uuid.UUID, before *time.Time) (GroupThread, error) {
	g, err := s.group(ctx, me, id)
	if err != nil {
		return GroupThread{}, err
	}
	rows, err := s.st.GroupThread(ctx, db.GroupThreadParams{GroupID: id, Before: before, Lim: threadPage + 1})
	if err != nil {
		return GroupThread{}, err
	}
	out := GroupThread{Group: g, Messages: []MessageOut{}, HasMore: len(rows) > threadPage}
	if out.HasMore {
		rows = rows[:threadPage]
	}
	ids := make([]uuid.UUID, len(rows))
	for i, r := range rows {
		ids[i] = r.ID
	}
	files, err := s.st.AttachmentsOfGroupMessages(ctx, ids)
	if err != nil {
		return GroupThread{}, err
	}
	attached := byMessage(files, true)
	// The query reads newest first; the page wants oldest first.
	for i := len(rows) - 1; i >= 0; i-- {
		r := rows[i]
		out.Messages = append(out.Messages, MessageOut{
			ID: r.ID, SenderID: r.SenderID, SenderName: r.SenderName, Body: r.Body,
			CreatedAt: r.CreatedAt, Mine: r.SenderID == me.ID, Attachments: orEmpty(attached[r.ID]),
		})
	}
	members, err := s.st.ChatGroupMembers(ctx, id)
	if err != nil {
		return GroupThread{}, err
	}
	out.Members = make([]GroupMember, len(members))
	for i, m := range members {
		out.Members[i] = GroupMember{
			Person:  Person{ID: m.ID, Name: m.Name, Role: m.Role, IsActive: m.IsActive},
			AddedAt: m.AddedAt,
		}
	}
	return out, nil
}

// SendToGroup writes to the group, with any files uploaded for it, and puts it in
// front of everyone else in it.
func (s *Service) SendToGroup(ctx context.Context, me db.User, id uuid.UUID, body string, uploads []string) (MessageOut, error) {
	body = strings.TrimSpace(body)
	if err := validBody(body, len(uploads)); err != nil {
		return MessageOut{}, err
	}
	if _, err := s.group(ctx, me, id); err != nil {
		return MessageOut{}, err
	}
	ups, err := s.claimUploads(ctx, me, uploads)
	if err != nil {
		return MessageOut{}, err
	}
	var out MessageOut
	err = s.st.Tx(ctx, func(q *db.Queries) error {
		m, err := q.SendGroupMessage(ctx, db.SendGroupMessageParams{GroupID: id, SenderID: me.ID, Body: body})
		if err != nil {
			return err
		}
		out = MessageOut{ID: m.ID, SenderID: m.SenderID, SenderName: me.Name, Body: m.Body, CreatedAt: m.CreatedAt, Mine: true}
		out.Attachments, err = saveAttachments(ctx, q, me, nil, &m.ID, ups)
		return err
	})
	if err != nil {
		s.discard(ctx, ups)
		return MessageOut{}, err
	}
	s.tellGroup(ctx, id, uuid.Nil, "group_message", map[string]any{"group": id, "message": out})
	return out, nil
}

// MarkGroupRead moves this member's read mark to now.
func (s *Service) MarkGroupRead(ctx context.Context, me db.User, id uuid.UUID) error {
	if _, err := s.group(ctx, me, id); err != nil {
		return err
	}
	return s.st.MarkChatGroupRead(ctx, db.MarkChatGroupReadParams{GroupID: id, UserID: me.ID})
}

// RenameGroup: whoever started it names it.
func (s *Service) RenameGroup(ctx context.Context, me db.User, id uuid.UUID, name string) (Group, error) {
	g, err := s.group(ctx, me, id)
	if err != nil {
		return Group{}, err
	}
	if g.CreatedBy != me.ID {
		return Group{}, apperr.Forbidden()
	}
	name = strings.TrimSpace(name)
	v := validate.Errors{}
	if name == "" {
		v.Add("name", "is required")
	} else if len([]rune(name)) > maxGroupName {
		v.Add("name", "is too long")
	}
	if !v.OK() {
		return Group{}, apperr.Validation(v)
	}
	row, err := s.st.RenameChatGroup(ctx, db.RenameChatGroupParams{ID: id, Name: name})
	if err != nil {
		return Group{}, err
	}
	g.Name = row.Name
	s.tellGroup(ctx, id, uuid.Nil, "group_changed", map[string]any{"group": id, "name": row.Name})
	return g, nil
}

// AddToGroup: any member may bring someone else in, the way they would in a room.
func (s *Service) AddToGroup(ctx context.Context, me db.User, id uuid.UUID, ids []uuid.UUID) error {
	g, err := s.group(ctx, me, id)
	if err != nil {
		return err
	}
	if g.MemberCount+int64(len(ids)) > maxGroupSize {
		return apperr.BadRequest("That group is full")
	}
	for _, uid := range ids {
		p, err := s.person(ctx, uid)
		if err != nil {
			return err
		}
		if !p.IsActive {
			return apperr.BadRequest(p.Name + " no longer has access")
		}
	}
	if err := s.addMembers(ctx, id, ids); err != nil {
		return err
	}
	s.tellGroup(ctx, id, uuid.Nil, "group_changed", map[string]any{"group": id, "name": g.Name})
	for _, uid := range ids {
		s.bus.Publish(ctx, uid, "group_changed", map[string]any{"group": id, "name": g.Name})
	}
	return nil
}

// RemoveFromGroup: anyone may leave; only whoever started it may remove someone else.
// The group goes when its last member does, so nothing is left behind to find.
func (s *Service) RemoveFromGroup(ctx context.Context, me db.User, id, who uuid.UUID) error {
	g, err := s.group(ctx, me, id)
	if err != nil {
		return err
	}
	if who != me.ID && g.CreatedBy != me.ID {
		return apperr.Forbidden()
	}
	members, err := s.st.ChatGroupMemberIDs(ctx, id)
	if err != nil {
		return err
	}
	if _, err := s.st.RemoveChatGroupMember(ctx, db.RemoveChatGroupMemberParams{GroupID: id, UserID: who}); err != nil {
		return err
	}
	if len(members) <= 1 {
		return s.st.DeleteChatGroup(ctx, id)
	}
	for _, uid := range members {
		s.bus.Publish(ctx, uid, "group_changed", map[string]any{"group": id, "name": g.Name})
	}
	return nil
}

// group is the membership check every other call starts with: a group you are not
// in is simply not there as far as you're concerned.
func (s *Service) group(ctx context.Context, me db.User, id uuid.UUID) (Group, error) {
	row, err := s.st.GetChatGroup(ctx, id)
	if store.IsNotFound(err) {
		return Group{}, apperr.NotFound("Group")
	} else if err != nil {
		return Group{}, err
	}
	in, err := s.st.IsChatGroupMember(ctx, db.IsChatGroupMemberParams{GroupID: id, UserID: me.ID})
	if err != nil {
		return Group{}, err
	}
	if !in {
		return Group{}, apperr.NotFound("Group")
	}
	return Group{
		ID: row.ID, Name: row.Name, CreatedBy: row.CreatedBy, CreatedAt: row.CreatedAt,
		MemberCount: row.MemberCount, Mine: row.CreatedBy == me.ID,
	}, nil
}

// tellGroup pushes an event to everyone in the group, optionally skipping one person.
func (s *Service) tellGroup(ctx context.Context, id, except uuid.UUID, kind string, data map[string]any) {
	ids, err := s.st.ChatGroupMemberIDs(ctx, id)
	if err != nil {
		return
	}
	for _, uid := range ids {
		if uid == except {
			continue
		}
		s.bus.Publish(ctx, uid, kind, data)
	}
}
