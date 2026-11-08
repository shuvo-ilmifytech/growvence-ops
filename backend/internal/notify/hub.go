package notify

import (
	"context"
	"log/slog"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Hub fans committed notifications out to the SSE streams open in this process.
// Every API instance runs its own Hub with its own LISTEN connection.
type Hub struct {
	mu   sync.Mutex
	subs map[uuid.UUID]map[chan int64]struct{}
}

func NewHub() *Hub { return &Hub{subs: map[uuid.UUID]map[chan int64]struct{}{}} }

// Subscribe returns a channel of notification ids for the user and a cancel func.
func (h *Hub) Subscribe(user uuid.UUID) (<-chan int64, func()) {
	ch := make(chan int64, 32)
	h.mu.Lock()
	if h.subs[user] == nil {
		h.subs[user] = map[chan int64]struct{}{}
	}
	h.subs[user][ch] = struct{}{}
	h.mu.Unlock()
	return ch, func() {
		h.mu.Lock()
		delete(h.subs[user], ch)
		if len(h.subs[user]) == 0 {
			delete(h.subs, user)
		}
		h.mu.Unlock()
	}
}

func (h *Hub) publish(user uuid.UUID, id int64) {
	h.mu.Lock()
	defer h.mu.Unlock()
	for ch := range h.subs[user] {
		select {
		case ch <- id:
		default: // slow client: it re-syncs the unread count on its next event
		}
	}
}

// Listen holds a LISTEN connection until ctx ends, reconnecting with backoff.
func (h *Hub) Listen(ctx context.Context, pool *pgxpool.Pool) {
	backoff := time.Second
	for ctx.Err() == nil {
		err := h.listenOnce(ctx, pool)
		if ctx.Err() != nil {
			return
		}
		slog.Warn("notification listener disconnected; retrying", "err", err, "in", backoff)
		select {
		case <-ctx.Done():
			return
		case <-time.After(backoff):
		}
		backoff = min(backoff*2, 30*time.Second)
	}
}

func (h *Hub) listenOnce(ctx context.Context, pool *pgxpool.Pool) error {
	conn, err := pool.Acquire(ctx)
	if err != nil {
		return err
	}
	defer conn.Release()
	if _, err := conn.Exec(ctx, "LISTEN notifications"); err != nil {
		return err
	}
	for {
		n, err := conn.Conn().WaitForNotification(ctx)
		if err != nil {
			return err
		}
		user, idStr, ok := strings.Cut(n.Payload, ":")
		uid, err1 := uuid.Parse(user)
		id, err2 := strconv.ParseInt(idStr, 10, 64)
		if !ok || err1 != nil || err2 != nil {
			continue
		}
		h.publish(uid, id)
	}
}
