// Package testutil provisions throwaway Postgres databases and Redis DBs for integration tests.
//
// Set TEST_DATABASE_URL (a user allowed to CREATE DATABASE) and TEST_REDIS_URL;
// tests that need them are skipped otherwise.
package testutil

import (
	"context"
	"fmt"
	"net/url"
	"os"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/redis/go-redis/v9"

	"github.com/solvinx/opsflow/internal/storage"
	"github.com/solvinx/opsflow/internal/store"
)

// NewStore creates a fresh, migrated database named opsflow_test_<name>.
func NewStore(t *testing.T, name string) *store.Store {
	t.Helper()
	base := os.Getenv("TEST_DATABASE_URL")
	if base == "" {
		t.Skip("TEST_DATABASE_URL not set")
	}
	ctx := context.Background()
	dbName := "opsflow_test_" + name

	admin, err := pgx.Connect(ctx, base)
	if err != nil {
		t.Fatalf("connect admin db: %v", err)
	}
	defer admin.Close(ctx)
	ident := pgx.Identifier{dbName}.Sanitize()
	if _, err := admin.Exec(ctx, "DROP DATABASE IF EXISTS "+ident+" WITH (FORCE)"); err != nil {
		t.Fatalf("drop test db: %v", err)
	}
	if _, err := admin.Exec(ctx, "CREATE DATABASE "+ident); err != nil {
		t.Fatalf("create test db: %v", err)
	}

	u, err := url.Parse(base)
	if err != nil {
		t.Fatal(err)
	}
	u.Path = "/" + dbName
	st, err := store.Open(ctx, u.String())
	if err != nil {
		t.Fatalf("open test db: %v", err)
	}
	if err := st.Migrate(ctx); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	t.Cleanup(st.Close)
	return st
}

// NewRedis returns a client on the given logical DB, flushed before use.
func NewRedis(t *testing.T, dbNum int) *redis.Client {
	t.Helper()
	raw := os.Getenv("TEST_REDIS_URL")
	if raw == "" {
		t.Skip("TEST_REDIS_URL not set")
	}
	opts, err := redis.ParseURL(raw)
	if err != nil {
		t.Fatal(err)
	}
	opts.DB = dbNum
	rdb := redis.NewClient(opts)
	if err := rdb.FlushDB(context.Background()).Err(); err != nil {
		t.Fatalf("flush redis db %d: %v", dbNum, err)
	}
	t.Cleanup(func() { _ = rdb.Close() })
	return rdb
}

func Must[T any](v T, err error) T {
	if err != nil {
		panic(fmt.Sprintf("testutil.Must: %v", err))
	}
	return v
}

// NewStorage connects to the test bucket (TEST_S3_BUCKET, created if missing).
// Uses the same S3_* endpoint and credentials as the app.
func NewStorage(t *testing.T) *storage.Storage {
	t.Helper()
	bucket := os.Getenv("TEST_S3_BUCKET")
	if bucket == "" {
		t.Skip("TEST_S3_BUCKET not set")
	}
	env := func(k, def string) string {
		if v := os.Getenv(k); v != "" {
			return v
		}
		return def
	}
	endpoint := env("S3_ENDPOINT", "localhost:9010")
	s, err := storage.New(context.Background(), storage.Config{
		Endpoint: endpoint, PublicEndpoint: env("S3_PUBLIC_ENDPOINT", endpoint),
		AccessKey: env("S3_ACCESS_KEY", "opsflow"), SecretKey: env("S3_SECRET_KEY", "opsflow-secret"),
		Bucket: bucket, Region: env("S3_REGION", "us-east-1"),
	})
	if err != nil {
		t.Fatalf("storage: %v", err)
	}
	return s
}
