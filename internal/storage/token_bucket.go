package storage

import (
	"context"
	_ "embed"
	"errors"
	"fmt"
	"math"
	"time"

	"rate-limiter/internal/limiter"

	"github.com/redis/go-redis/v9"
)

const tokenBucketKeyPrefix = "ratelimit:token-bucket:"

//go:embed scripts/token_bucket.lua
var tokenBucketLua string

var tokenBucketScript = redis.NewScript(tokenBucketLua)

// TokenBucket enforces a Redis-backed token bucket with fractional refill.
type TokenBucket struct {
	client *redis.Client
}

// NewRedisTokenBucket creates a Redis-backed token-bucket limiter.
func NewRedisTokenBucket(client *redis.Client) *TokenBucket {
	return &TokenBucket{client: client}
}

// Allow atomically refills, checks, and consumes a token.
func (t *TokenBucket) Allow(
	ctx context.Context,
	key string,
	config limiter.Config,
) (limiter.Decision, error) {
	if err := ctx.Err(); err != nil {
		return limiter.Decision{}, err
	}
	if config.Limit <= 0 || config.Window <= 0 || config.BurstCapacity < 0 {
		return limiter.Decision{}, limiter.ErrInvalidConfig
	}
	if t == nil || t.client == nil {
		return limiter.Decision{}, errors.New("redis client is nil")
	}

	capacity := config.BurstCapacity
	if capacity == 0 {
		capacity = config.Limit
	}
	windowMicros := durationMicroseconds(config.Window)
	refillDuration := time.Duration(math.Ceil(float64(capacity)*float64(windowMicros)/float64(config.Limit))) * time.Microsecond
	ttlMillis := durationMilliseconds(refillDuration)

	result, err := tokenBucketScript.Run(
		ctx,
		t.client,
		[]string{tokenBucketKeyPrefix + key},
		config.Limit,
		capacity,
		windowMicros,
		ttlMillis,
	).Result()
	if err != nil {
		return limiter.Decision{}, fmt.Errorf("update Redis token bucket: %w", err)
	}

	allowed, metrics, err := redisDecisionValues(result, 4)
	if err != nil {
		return limiter.Decision{}, fmt.Errorf("parse Redis token bucket response: %w", err)
	}
	resetAfter := time.Duration(metrics[2]) * time.Microsecond
	decision := limiter.Decision{
		Allowed:   allowed,
		Remaining: int(metrics[0]),
		ResetAt:   time.Now().Add(resetAfter),
	}
	if !allowed {
		decision.RetryAfter = time.Duration(metrics[1]) * time.Microsecond
	}
	return decision, nil
}

var _ limiter.Limiter = (*TokenBucket)(nil)
