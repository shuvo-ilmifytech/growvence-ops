package chat

import (
	"context"
	"encoding/json"
	"time"

	"github.com/google/uuid"

	"github.com/solvinx/opsflow/internal/apperr"
	"github.com/solvinx/opsflow/internal/db"
	"github.com/solvinx/opsflow/internal/store"
)

// Voice calls happen directly between the two browsers (WebRTC). The server only
// introduces them: it carries the offer, the answer and the network candidates, and
// keeps a record of who rang whom so missed calls are visible afterwards. No audio
// ever passes through here.

// ringFor is how long an unanswered call keeps ringing before it counts as missed.
const ringFor = 45 * time.Second

type CallOut struct {
	ID         uuid.UUID  `json:"id"`
	CallerID   uuid.UUID  `json:"caller_id"`
	CallerName string     `json:"caller_name"`
	CalleeID   uuid.UUID  `json:"callee_id"`
	CalleeName string     `json:"callee_name"`
	Status     string     `json:"status"`
	StartedAt  time.Time  `json:"started_at"`
	AnsweredAt *time.Time `json:"answered_at"`
	EndedAt    *time.Time `json:"ended_at"`
	Incoming   bool       `json:"incoming"` // true when this person is being called
}

// rowOf pairs a freshly written call with the two names, which the insert doesn't return.
func rowOf(c db.Call, callerName, calleeName string) db.GetCallRow {
	return db.GetCallRow{
		ID: c.ID, CallerID: c.CallerID, CalleeID: c.CalleeID, Status: c.Status,
		StartedAt: c.StartedAt, AnsweredAt: c.AnsweredAt, EndedAt: c.EndedAt,
		CallerName: callerName, CalleeName: calleeName,
	}
}

func out(c db.GetCallRow, me uuid.UUID) CallOut {
	return CallOut{
		ID: c.ID, CallerID: c.CallerID, CallerName: c.CallerName, CalleeID: c.CalleeID,
		CalleeName: c.CalleeName, Status: c.Status, StartedAt: c.StartedAt,
		AnsweredAt: c.AnsweredAt, EndedAt: c.EndedAt, Incoming: c.CalleeID == me,
	}
}

// Call rings someone. The caller's browser then sends its offer through Signal.
func (s *Service) Call(ctx context.Context, from db.User, to uuid.UUID) (CallOut, error) {
	if to == from.ID {
		return CallOut{}, apperr.BadRequest("You cannot call yourself")
	}
	other, err := s.person(ctx, to)
	if err != nil {
		return CallOut{}, err
	}
	if !other.IsActive {
		return CallOut{}, apperr.BadRequest(other.Name + " no longer has access")
	}
	// Whoever is already on a call can't be rung again, and can't ring out.
	if live, err := s.liveCall(ctx, to); err != nil {
		return CallOut{}, err
	} else if live != nil {
		return CallOut{}, apperr.Conflict(other.Name + " is already on a call")
	}
	if live, err := s.liveCall(ctx, from.ID); err != nil {
		return CallOut{}, err
	} else if live != nil {
		return CallOut{}, apperr.Conflict("You are already on a call")
	}
	c, err := s.st.StartCall(ctx, db.StartCallParams{CallerID: from.ID, CalleeID: to})
	if store.IsUniqueViolation(err) {
		return CallOut{}, apperr.Conflict("That call is already ringing")
	} else if err != nil {
		return CallOut{}, err
	}
	row := rowOf(c, from.Name, other.Name)
	s.ring(ctx, row)
	return out(row, from.ID), nil
}

// ring tells both ends about a call's current state.
func (s *Service) ring(ctx context.Context, row db.GetCallRow) {
	s.bus.Publish(ctx, row.CalleeID, "call", out(row, row.CalleeID))
	s.bus.Publish(ctx, row.CallerID, "call", out(row, row.CallerID))
}

// Answer, Decline, Hang up and the missed-call sweep all move a call on.
func (s *Service) SetStatus(ctx context.Context, me db.User, id uuid.UUID, status string) (CallOut, error) {
	row, err := s.st.GetCall(ctx, id)
	if store.IsNotFound(err) {
		return CallOut{}, apperr.NotFound("Call")
	} else if err != nil {
		return CallOut{}, err
	}
	if row.CallerID != me.ID && row.CalleeID != me.ID {
		return CallOut{}, apperr.NotFound("Call")
	}
	switch status {
	case "answered", "declined":
		// Only the person being called may answer or decline it.
		if row.CalleeID != me.ID {
			return CallOut{}, apperr.Forbidden()
		}
	case "ended", "failed":
		// Either end may hang up.
	default:
		return CallOut{}, apperr.BadRequest("Unknown call status")
	}
	c, err := s.st.SetCallStatus(ctx, db.SetCallStatusParams{ID: id, Status: status})
	if store.IsNotFound(err) {
		// Already finished: report where it ended up rather than failing.
		return out(row, me.ID), nil
	} else if err != nil {
		return CallOut{}, err
	}
	updated := rowOf(c, row.CallerName, row.CalleeName)
	s.ring(ctx, updated)
	return out(updated, me.ID), nil
}

// Signal passes one WebRTC message (offer, answer or candidate) to the other end.
// The server never reads inside it; it only checks who is allowed to send it.
func (s *Service) Signal(ctx context.Context, me db.User, id uuid.UUID, kind string, payload json.RawMessage) error {
	switch kind {
	case "offer", "answer", "candidate":
	default:
		return apperr.BadRequest("Unknown signal")
	}
	if len(payload) > 64<<10 {
		return apperr.BadRequest("Signal is too large")
	}
	row, err := s.st.GetCall(ctx, id)
	if store.IsNotFound(err) {
		return apperr.NotFound("Call")
	} else if err != nil {
		return err
	}
	if row.Status != "ringing" && row.Status != "answered" {
		return apperr.Conflict("That call has ended")
	}
	var to uuid.UUID
	switch me.ID {
	case row.CallerID:
		to = row.CalleeID
	case row.CalleeID:
		to = row.CallerID
	default:
		return apperr.NotFound("Call")
	}
	s.bus.Publish(ctx, to, "call_signal", map[string]any{
		"call_id": id, "from": me.ID, "kind": kind, "payload": payload,
	})
	return nil
}

// Live is the call this person is in, if any: a browser that reloads mid-call uses it
// to find its way back, and an incoming call survives a refresh.
func (s *Service) Live(ctx context.Context, me db.User) (*CallOut, error) {
	row, err := s.liveCall(ctx, me.ID)
	if err != nil || row == nil {
		return nil, err
	}
	o := out(*row, me.ID)
	return &o, nil
}

func (s *Service) liveCall(ctx context.Context, user uuid.UUID) (*db.GetCallRow, error) {
	r, err := s.st.LiveCallFor(ctx, user)
	if store.IsNotFound(err) {
		return nil, nil
	} else if err != nil {
		return nil, err
	}
	row := db.GetCallRow(r)
	return &row, nil
}

// Recent is the call log for this person, newest first.
func (s *Service) Recent(ctx context.Context, me db.User) ([]CallOut, error) {
	rows, err := s.st.RecentCalls(ctx, db.RecentCallsParams{Me: me.ID, Lim: recentCalls})
	if err != nil {
		return nil, err
	}
	calls := make([]CallOut, len(rows))
	for i, r := range rows {
		calls[i] = out(db.GetCallRow(r), me.ID)
	}
	return calls, nil
}

// ExpireRinging turns calls nobody picked up into missed calls. The scheduled runner
// calls this, so a caller who closed their laptop doesn't leave someone ringing.
func (s *Service) ExpireRinging(ctx context.Context) error {
	rows, err := s.st.ExpireRingingCalls(ctx, int32(ringFor.Seconds()))
	if err != nil {
		return err
	}
	for _, r := range rows {
		for _, who := range []uuid.UUID{r.CallerID, r.CalleeID} {
			s.bus.Publish(ctx, who, "call_state", map[string]any{"call_id": r.ID, "status": "missed"})
		}
	}
	return nil
}
