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

const slidingWindowLogKeyPrefix = "ratelimit:sliding-window-log:"

//go:embed scripts/sliding_log.lua
var slidingWindowLogLua string

var slidingWindowLogScript = redis.NewScript(slidingWindowLogLua)

// SlidingWindowLog enforces a rolling limit using a Redis sorted set of request times.
type SlidingWindowLog struct {
	client *redis.Client
}

// NewRedisSlidingWindowLog creates a Redis-backed sliding-window log limiter.
func NewRedisSlidingWindowLog(client *redis.Client) *SlidingWindowLog {
	return &SlidingWindowLog{client: client}
}

// Allow atomically expires old request timestamps and records the current request.
func (s *SlidingWindowLog) Allow(
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
	if s == nil || s.client == nil {
		return limiter.Decision{}, errors.New("redis client is nil")
	}

	windowMicros := durationMicroseconds(config.Window)
	ttlMillis := durationMilliseconds(config.Window)
	result, err := slidingWindowLogScript.Run(
		ctx,
		s.client,
		[]string{
			slidingWindowLogKeyPrefix + key,
			slidingWindowLogKeyPrefix + key + ":sequence",
		},
		windowMicros,
		config.Limit,
		ttlMillis,
	).Result()
	if err != nil {
		return limiter.Decision{}, fmt.Errorf("update Redis sliding-window log: %w", err)
	}

	allowed, metrics, err := redisDecisionValues(result, 3)
	if err != nil {
		return limiter.Decision{}, fmt.Errorf("parse Redis sliding-window log response: %w", err)
	}
	now := time.Now()
	remaining := max(0, config.Limit-int(metrics[0]))
	decision := limiter.Decision{
		Allowed:   allowed,
		Remaining: remaining,
	}
	if allowed {
		decision.ResetAt = now.Add(config.Window)
	} else {
		retryAfter := time.Duration(metrics[1]) * time.Microsecond
		decision.RetryAfter = retryAfter
		decision.ResetAt = now.Add(retryAfter)
	}
	return decision, nil
}

var _ limiter.Limiter = (*SlidingWindowLog)(nil)
