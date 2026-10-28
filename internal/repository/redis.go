// Package repository defines data access interfaces and models for the guestbook application.
package repository

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/redis/go-redis/v9"
)

// RedisSessionStore implements SessionStore backed by a Redis key-value store.
type RedisSessionStore struct {
	client *redis.Client
}

// NewRedisSessionStore creates a new RedisSessionStore instance.
func NewRedisSessionStore(client *redis.Client) *RedisSessionStore {
	return &RedisSessionStore{client: client}
}

// sessionKey formats the storage key for a session in Redis.
func sessionKey(sessionID string) string {
	return "session:" + sessionID
}

// Set stores a user session for the given session ID with an expiration duration.
func (s *RedisSessionStore) Set(ctx context.Context, sessionID string, user SessionUser, ttl time.Duration) error {
	data, err := json.Marshal(user)
	if err != nil {
		return fmt.Errorf("marshaling session user: %w", err)
	}

	if err := s.client.Set(ctx, sessionKey(sessionID), data, ttl).Err(); err != nil {
		return fmt.Errorf("storing session in redis: %w", err)
	}
	return nil
}

// Get retrieves a user session by its session ID. Returns false if not found.
func (s *RedisSessionStore) Get(ctx context.Context, sessionID string) (SessionUser, bool, error) {
	data, err := s.client.Get(ctx, sessionKey(sessionID)).Bytes()
	if err != nil {
		if errors.Is(err, redis.Nil) {
			return SessionUser{}, false, nil
		}
		return SessionUser{}, false, fmt.Errorf("getting session from redis: %w", err)
	}

	var user SessionUser
	if err := json.Unmarshal(data, &user); err != nil {
		return SessionUser{}, false, fmt.Errorf("unmarshaling session user: %w", err)
	}

	return user, true, nil
}

// Delete removes a user session by its session ID.
func (s *RedisSessionStore) Delete(ctx context.Context, sessionID string) error {
	if err := s.client.Del(ctx, sessionKey(sessionID)).Err(); err != nil {
		return fmt.Errorf("deleting session from redis: %w", err)
	}
	return nil
}

// Ping verifies connectivity to the Redis instance.
func (s *RedisSessionStore) Ping(ctx context.Context) error {
	if err := s.client.Ping(ctx).Err(); err != nil {
		return fmt.Errorf("pinging redis: %w", err)
	}
	return nil
}
