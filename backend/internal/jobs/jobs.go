// Package jobs runs periodic checks (overdue, unassigned 24h, unsent 24h).
// Every API instance runs the loop; a short Redis lock lets only one execute each tick.
package jobs

import (
	"context"
	"log/slog"
	"time"

	"github.com/redis/go-redis/v9"
)

type Checker interface {
	RunChecks(ctx context.Context, now time.Time) error
}

const lockKey = "jobs:checks:lock"

func Run(ctx context.Context, rdb *redis.Client, c Checker, every time.Duration) {
	RunLocked(ctx, rdb, lockKey, "scheduled checks", every, func(ctx context.Context) error {
		return c.RunChecks(ctx, time.Now())
	})
}

// RunLocked calls fn every interval. The Redis lock means that when several API
// instances run, only one of them works each tick.
func RunLocked(ctx context.Context, rdb *redis.Client, key, what string, every time.Duration, fn func(context.Context) error) {
	tick := func() {
		ok, err := rdb.SetNX(ctx, key, "1", every-time.Second).Result()
		if err != nil || !ok {
			return // another instance has this tick
		}
		start := time.Now()
		if err := fn(ctx); err != nil {
			slog.Error(what+" failed", "err", err)
			return
		}
		slog.Debug(what+" done", "took", time.Since(start))
	}
	tick()
	t := time.NewTicker(every)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			tick()
		}
	}
}
