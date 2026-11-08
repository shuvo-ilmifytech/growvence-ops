// Package store wires the Postgres pool, migrations and transactions.
package store

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/jackc/pgx/v5/stdlib"
	"github.com/pressly/goose/v3"

	"github.com/solvinx/opsflow/internal/db"
	"github.com/solvinx/opsflow/migrations"
)

type Store struct {
	Pool *pgxpool.Pool
	*db.Queries
}

func Open(ctx context.Context, url string) (*Store, error) {
	pool, err := pgxpool.New(ctx, url)
	if err != nil {
		return nil, err
	}
	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		return nil, fmt.Errorf("ping postgres: %w", err)
	}
	return &Store{Pool: pool, Queries: db.New(pool)}, nil
}

func (s *Store) Close() { s.Pool.Close() }

// Migrate applies all pending goose migrations embedded in the binary.
func (s *Store) Migrate(ctx context.Context) error {
	goose.SetBaseFS(migrations.FS)
	if err := goose.SetDialect("postgres"); err != nil {
		return err
	}
	sqlDB := stdlib.OpenDBFromPool(s.Pool)
	defer sqlDB.Close()
	return goose.UpContext(ctx, sqlDB, ".")
}

// Tx runs fn in a transaction, committing if it returns nil.
func (s *Store) Tx(ctx context.Context, fn func(q *db.Queries) error) error {
	return pgx.BeginFunc(ctx, s.Pool, func(tx pgx.Tx) error {
		return fn(s.Queries.WithTx(tx))
	})
}

// Activity is one entry for the activity log.
type Activity struct {
	Actor      *uuid.UUID
	Action     string
	EntityType string
	EntityID   *uuid.UUID
	// What the entry is about: a project, or a client request (a client with no project).
	ProjectID       *uuid.UUID
	ClientRequestID *uuid.UUID
	Metadata        any
}

// Via says what a change was made through when it wasn't the person in the portal:
// an AI assistant over MCP, acting for them.
type Via struct {
	Channel string // "mcp"
	Client  string // the assistant's name, as connected
}

type viaKey struct{}

// WithVia marks every activity logged under ctx as made through v.
func WithVia(ctx context.Context, v Via) context.Context { return context.WithValue(ctx, viaKey{}, v) }

func LogActivity(ctx context.Context, q *db.Queries, a Activity) error {
	meta := json.RawMessage(`{}`)
	if a.Metadata != nil {
		b, err := json.Marshal(a.Metadata)
		if err != nil {
			return err
		}
		meta = b
	}
	if v, ok := ctx.Value(viaKey{}).(Via); ok {
		m := map[string]any{}
		if err := json.Unmarshal(meta, &m); err != nil {
			// Metadata that isn't an object keeps its value under its own key.
			m = map[string]any{"value": json.RawMessage(meta)}
		}
		m["via"], m["via_client"] = v.Channel, v.Client
		b, err := json.Marshal(m)
		if err != nil {
			return err
		}
		meta = b
	}
	return q.InsertActivity(ctx, db.InsertActivityParams{
		ActorID: a.Actor, Action: a.Action, EntityType: a.EntityType, EntityID: a.EntityID,
		ProjectID: a.ProjectID, ClientRequestID: a.ClientRequestID, Metadata: meta,
	})
}

func IsNotFound(err error) bool { return errors.Is(err, pgx.ErrNoRows) }

// IsUniqueViolation reports a unique-constraint failure (SQLSTATE 23505).
func IsUniqueViolation(err error) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.Code == "23505"
}

// IsForeignKeyViolation reports a reference to a missing row (SQLSTATE 23503).
func IsForeignKeyViolation(err error) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.Code == "23503"
}
