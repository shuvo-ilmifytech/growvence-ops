// Command api runs the HTTP API server.
package main

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"syscall"
	"time"
	_ "time/tzdata" // org_timezone validation must work in minimal containers

	"github.com/redis/go-redis/v9"

	"github.com/solvinx/opsflow/internal/config"
	"github.com/solvinx/opsflow/internal/httpapi"
	"github.com/solvinx/opsflow/internal/jobs"
	"github.com/solvinx/opsflow/internal/notify"
	"github.com/solvinx/opsflow/internal/storage"
	"github.com/solvinx/opsflow/internal/store"
)

func main() {
	slog.SetDefault(slog.New(slog.NewJSONHandler(os.Stdout, nil)))
	if err := run(); err != nil {
		slog.Error("fatal", "err", err)
		os.Exit(1)
	}
}

func run() error {
	cfg, err := config.Load()
	if err != nil {
		return err
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	st, err := store.Open(ctx, cfg.DatabaseURL)
	if err != nil {
		return err
	}
	defer st.Close()
	if cfg.AutoMigrate {
		if err := st.Migrate(ctx); err != nil {
			return err
		}
	}

	ropts, err := redis.ParseURL(cfg.RedisURL)
	if err != nil {
		return err
	}
	rdb := redis.NewClient(ropts)
	defer rdb.Close()
	if err := rdb.Ping(ctx).Err(); err != nil {
		return err
	}

	files, err := storage.New(ctx, cfg.S3)
	if err != nil {
		return fmt.Errorf("object storage: %w", err)
	}

	hub := notify.NewHub()
	go hub.Listen(ctx, st.Pool)

	app, svc, backup, chatSvc := httpapi.NewWithServices(cfg, st, rdb, files, hub)
	go jobs.Run(ctx, rdb, svc, time.Minute)
	// A call nobody picked up stops ringing by itself.
	go jobs.RunLocked(ctx, rdb, "jobs:calls:lock", "missed call sweep", 15*time.Second, chatSvc.ExpireRinging)
	if backup != nil {
		slog.Info("google sheet backup on", "every", cfg.SheetsEvery)
		go jobs.RunLocked(ctx, rdb, "jobs:sheets:lock", "google sheet backup", cfg.SheetsEvery, func(ctx context.Context) error {
			_, err := backup.Once(ctx)
			return err
		})
	}
	go func() {
		<-ctx.Done()
		_ = app.ShutdownWithTimeout(10 * time.Second)
	}()
	slog.Info("listening", "addr", cfg.Addr)
	return app.Listen(cfg.Addr)
}
