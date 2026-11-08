// Package live carries short-lived events to whichever API instance holds a person's
// open stream: chat messages as they're typed and the back-and-forth that sets up a
// voice call. Redis pub/sub does the fan-out, so nothing ephemeral touches the database
// and the payload isn't capped the way a Postgres NOTIFY is.
package live

import (
	"context"
	"encoding/json"
	"log/slog"

	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"
)

// Event is one thing that happened, addressed to one person.
type Event struct {
	Kind string          `json:"kind"` // "message", "call", "call_state"
	Data json.RawMessage `json:"data"`
}

type Bus struct{ rdb *redis.Client }

func NewBus(rdb *redis.Client) *Bus { return &Bus{rdb: rdb} }

func channel(user uuid.UUID) string { return "live:" + user.String() }

// Publish sends an event to every stream that person has open, anywhere.
// A failure here is logged, never fatal: the UI also refetches on its own.
func (b *Bus) Publish(ctx context.Context, user uuid.UUID, kind string, data any) {
	raw, err := json.Marshal(data)
	if err != nil {
		slog.Error("live event is not serialisable", "kind", kind, "err", err)
		return
	}
	payload, err := json.Marshal(Event{Kind: kind, Data: raw})
	if err != nil {
		return
	}
	if err := b.rdb.Publish(ctx, channel(user), payload).Err(); err != nil {
		slog.Warn("live event not delivered", "kind", kind, "err", err)
	}
}

// Subscribe returns that person's events until the caller stops it.
func (b *Bus) Subscribe(ctx context.Context, user uuid.UUID) (<-chan Event, func()) {
	sub := b.rdb.Subscribe(ctx, channel(user))
	out := make(chan Event, 32)
	go func() {
		defer close(out)
		for msg := range sub.Channel() {
			var e Event
			if json.Unmarshal([]byte(msg.Payload), &e) != nil {
				continue
			}
			select {
			case out <- e:
			default: // a slow client: it catches up by refetching
			}
		}
	}()
	return out, func() { _ = sub.Close() }
}
