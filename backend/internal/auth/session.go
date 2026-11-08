package auth

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"
)

var ErrNoSession = errors.New("no session")

type Session struct {
	UserID    uuid.UUID `json:"user_id"`
	CreatedAt time.Time `json:"created_at"`
}

// SessionStore keeps sessions in Redis. The cookie carries a random token;
// Redis only stores its SHA-256, so a Redis dump cannot be replayed as cookies.
type SessionStore struct {
	rdb    *redis.Client
	ttl    time.Duration // sliding idle timeout
	maxAge time.Duration // absolute lifetime
}

func NewSessionStore(rdb *redis.Client, ttl, maxAge time.Duration) *SessionStore {
	return &SessionStore{rdb: rdb, ttl: ttl, maxAge: maxAge}
}

func sessionKey(tokenHash string) string { return "sess:" + tokenHash }
func userSetKey(userID uuid.UUID) string { return "user_sess:" + userID.String() }

func hashToken(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])
}

// Create starts a session and returns the token for the cookie.
func (s *SessionStore) Create(ctx context.Context, userID uuid.UUID) (string, error) {
	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		return "", err
	}
	token := base64.RawURLEncoding.EncodeToString(raw)
	h := hashToken(token)
	data, _ := json.Marshal(Session{UserID: userID, CreatedAt: time.Now().UTC()})

	pipe := s.rdb.TxPipeline()
	pipe.Set(ctx, sessionKey(h), data, s.ttl)
	pipe.SAdd(ctx, userSetKey(userID), h)
	pipe.Expire(ctx, userSetKey(userID), s.maxAge)
	_, err := pipe.Exec(ctx)
	return token, err
}

// Get validates a token, refreshes its idle timeout and returns the session.
func (s *SessionStore) Get(ctx context.Context, token string) (Session, error) {
	var sess Session
	if token == "" {
		return sess, ErrNoSession
	}
	key := sessionKey(hashToken(token))
	data, err := s.rdb.Get(ctx, key).Bytes()
	if errors.Is(err, redis.Nil) {
		return sess, ErrNoSession
	} else if err != nil {
		return sess, err
	}
	if err := json.Unmarshal(data, &sess); err != nil {
		return sess, ErrNoSession
	}
	remaining := s.maxAge - time.Since(sess.CreatedAt)
	if remaining <= 0 {
		_ = s.rdb.Del(ctx, key).Err()
		return sess, ErrNoSession
	}
	s.rdb.Expire(ctx, key, min(s.ttl, remaining))
	return sess, nil
}

func (s *SessionStore) Destroy(ctx context.Context, token string) error {
	h := hashToken(token)
	data, err := s.rdb.GetDel(ctx, sessionKey(h)).Bytes()
	if err == nil {
		var sess Session
		if json.Unmarshal(data, &sess) == nil {
			s.rdb.SRem(ctx, userSetKey(sess.UserID), h)
		}
	}
	if errors.Is(err, redis.Nil) {
		return nil
	}
	return err
}

// DestroyAllForUser logs a user out everywhere (deactivation, password reset).
// keepToken, if non-empty, is left alive (e.g. the session that changed its own password).
func (s *SessionStore) DestroyAllForUser(ctx context.Context, userID uuid.UUID, keepToken string) error {
	keep := ""
	if keepToken != "" {
		keep = hashToken(keepToken)
	}
	hashes, err := s.rdb.SMembers(ctx, userSetKey(userID)).Result()
	if err != nil {
		return err
	}
	pipe := s.rdb.TxPipeline()
	for _, h := range hashes {
		if h == keep {
			continue
		}
		pipe.Del(ctx, sessionKey(h))
		pipe.SRem(ctx, userSetKey(userID), h)
	}
	_, err = pipe.Exec(ctx)
	return err
}
