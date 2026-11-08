// Package config loads runtime configuration from environment variables.
package config

import (
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/solvinx/opsflow/internal/sheets"
	"github.com/solvinx/opsflow/internal/storage"
)

type Config struct {
	Addr          string
	DatabaseURL   string
	RedisURL      string
	AppOrigins    []string // allowed browser origins for state-changing requests
	CookieSecure  bool
	SessionTTL    time.Duration // sliding idle timeout
	SessionMaxAge time.Duration // absolute lifetime
	AutoMigrate   bool
	S3            storage.Config
	MaxUploadSize int64
	// Sheets is the Google Sheet backup; SheetsEnabled is false when none is configured.
	Sheets        sheets.Config
	SheetsEnabled bool
	SheetsEvery   time.Duration
	// Voice calls: STUN finds a direct path, TURN relays when the networks won't allow one.
	StunURLs     []string
	TurnURL      string
	TurnUsername string
	TurnPassword string
}

func Load() (Config, error) {
	c := Config{
		Addr:          env("API_ADDR", ":8080"),
		DatabaseURL:   os.Getenv("DATABASE_URL"),
		RedisURL:      env("REDIS_URL", "redis://localhost:6390/0"),
		AppOrigins:    splitList(env("APP_ORIGINS", "http://localhost:3000")),
		CookieSecure:  envBool("COOKIE_SECURE", false),
		SessionTTL:    envDuration("SESSION_TTL", 7*24*time.Hour),
		SessionMaxAge: envDuration("SESSION_MAX_AGE", 30*24*time.Hour),
		AutoMigrate:   envBool("AUTO_MIGRATE", true),
		S3: storage.Config{
			Endpoint:       env("S3_ENDPOINT", "localhost:9010"),
			PublicEndpoint: env("S3_PUBLIC_ENDPOINT", env("S3_ENDPOINT", "localhost:9010")),
			AccessKey:      env("S3_ACCESS_KEY", "opsflow"),
			SecretKey:      env("S3_SECRET_KEY", "opsflow-secret"),
			Bucket:         env("S3_BUCKET", "opsflow"),
			Region:         env("S3_REGION", "us-east-1"),
			UseSSL:         envBool("S3_USE_SSL", false),
			PublicSSL:      envBool("S3_PUBLIC_SSL", envBool("S3_USE_SSL", false)),
		},
		MaxUploadSize: int64(envInt("MAX_UPLOAD_MB", 100)) << 20,
	}
	if c.DatabaseURL == "" {
		return c, fmt.Errorf("DATABASE_URL is required")
	}
	sh, on, err := sheets.LoadConfig(os.Getenv)
	if err != nil {
		return c, err
	}
	c.Sheets, c.SheetsEnabled = sh, on
	c.SheetsEvery = envDuration("SHEETS_SYNC_EVERY", 30*time.Second)
	c.StunURLs = splitList(env("STUN_URLS", "stun:stun.l.google.com:19302,stun:stun1.l.google.com:19302"))
	c.TurnURL = env("TURN_URL", "")
	c.TurnUsername = env("TURN_USERNAME", "")
	c.TurnPassword = env("TURN_PASSWORD", "")
	return c, nil
}

func env(key, def string) string {
	if v, ok := os.LookupEnv(key); ok && v != "" {
		return v
	}
	return def
}

func envBool(key string, def bool) bool {
	b, err := strconv.ParseBool(env(key, strconv.FormatBool(def)))
	if err != nil {
		return def
	}
	return b
}

func envInt(key string, def int) int {
	n, err := strconv.Atoi(env(key, strconv.Itoa(def)))
	if err != nil {
		return def
	}
	return n
}

func envDuration(key string, def time.Duration) time.Duration {
	d, err := time.ParseDuration(env(key, def.String()))
	if err != nil {
		return def
	}
	return d
}

func splitList(s string) []string {
	var out []string
	for _, p := range strings.Split(s, ",") {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, p)
		}
	}
	return out
}
