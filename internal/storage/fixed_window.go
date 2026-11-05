package storage

import (
	"context"
	_ "embed"
	"errors"
	"fmt"
	"time"

	"rate-limiter/internal/limiter"

	"github.com/redis/go-redis/v9"
)

const fixedWindowKeyPrefix = "ratelimit:fixed-window:"

//go:embed scripts/fixed_window.lua
var fixedWindowLua string

var fixedWindowScript = redis.NewScript(fixedWindowLua)

// FixedWindow enforces a fixed-window limit using a counter shared through Redis.
type FixedWindow struct {
	client *redis.Client
}

// NewRedisFixedWindow creates a Redis-backed fixed-window limiter.
func NewRedisFixedWindow(client *redis.Client) *FixedWindow {
	return &FixedWindow{client: client}
}

// Allow atomically checks and records a request in the Redis-backed window.
func (f *FixedWindow) Allow(
	ctx context.Context,
	key string,
	config limiter.Config,
) (limiter.Decision, error) {
	if err := ctx.Err(); err != nil {
		return limiter.Decision{}, err
	}
	if config.Limit <= 0 || config.Window <= 0 {
		return limiter.Decision{}, limiter.ErrInvalidConfig
	}
	if f == nil || f.client == nil {
		return limiter.Decision{}, errors.New("redis client is nil")
	}

	windowMillis := config.Window / time.Millisecond
	if config.Window%time.Millisecond != 0 {
		windowMillis++
	}
	if windowMillis == 0 {
		windowMillis = 1
	}

	result, err := fixedWindowScript.Run(
		ctx,
		f.client,
		[]string{fixedWindowKeyPrefix + key},
		config.Limit,
		windowMillis,
	).Result()
	if err != nil {
		return limiter.Decision{}, fmt.Errorf("update Redis fixed-window counter: %w", err)
	}

	values, ok := result.([]interface{})
	if !ok || len(values) != 3 {
		return limiter.Decision{}, fmt.Errorf("unexpected Redis fixed-window response: %T", result)
	}
	allowed, ok := values[0].(int64)
	if !ok {
		return limiter.Decision{}, fmt.Errorf("unexpected Redis fixed-window allowed value: %T", values[0])
	}
	count, ok := values[1].(int64)
	if !ok {
		return limiter.Decision{}, fmt.Errorf("unexpected Redis fixed-window count value: %T", values[1])
	}
	ttlMillis, ok := values[2].(int64)
	if !ok {
		return limiter.Decision{}, fmt.Errorf("unexpected Redis fixed-window TTL value: %T", values[2])
	}

	now := time.Now()
	if ttlMillis < 1 {
		ttlMillis = 1
	}
	ttl := time.Duration(ttlMillis) * time.Millisecond
	decision := limiter.Decision{
		Allowed:   allowed == 1,
		Remaining: max(0, config.Limit-int(count)),
		ResetAt:   now.Add(ttl),
	}
	if !decision.Allowed {
		decision.RetryAfter = ttl
	}
	return decision, nil
}

var _ limiter.Limiter = (*FixedWindow)(nil)
