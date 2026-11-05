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

const leakyBucketKeyPrefix = "ratelimit:leaky-bucket:"

//go:embed scripts/leaky_bucket.lua
var leakyBucketLua string

var leakyBucketScript = redis.NewScript(leakyBucketLua)

// LeakyBucket tracks pending requests by their scheduled processing time in Redis.
type LeakyBucket struct {
	client *redis.Client
}

// NewRedisLeakyBucket creates a Redis-backed leaky-bucket limiter.
func NewRedisLeakyBucket(client *redis.Client) *LeakyBucket {
	return &LeakyBucket{client: client}
}

// Allow atomically queues a request if the shared bucket has available capacity.
func (l *LeakyBucket) Allow(
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
	if l == nil || l.client == nil {
		return limiter.Decision{}, errors.New("redis client is nil")
	}

	capacity := config.BurstCapacity
	if capacity == 0 {
		capacity = config.Limit
	}
	windowMicros := durationMicroseconds(config.Window)
	intervalMicros := int64(math.Ceil(float64(windowMicros) / float64(config.Limit)))
	queueLifetime := time.Duration(intervalMicros*int64(capacity)) * time.Microsecond
	ttlMillis := durationMilliseconds(queueLifetime)

	result, err := leakyBucketScript.Run(
		ctx,
		l.client,
		[]string{
			leakyBucketKeyPrefix + key,
			leakyBucketKeyPrefix + key + ":sequence",
		},
		intervalMicros,
		capacity,
		ttlMillis,
	).Result()
	if err != nil {
		return limiter.Decision{}, fmt.Errorf("update Redis leaky bucket: %w", err)
	}

	allowed, metrics, err := redisDecisionValues(result, 4)
	if err != nil {
		return limiter.Decision{}, fmt.Errorf("parse Redis leaky-bucket response: %w", err)
	}
	resetAfter := time.Duration(metrics[2]) * time.Microsecond
	decision := limiter.Decision{
		Allowed:   allowed,
		Remaining: max(0, capacity-int(metrics[0])),
		ResetAt:   time.Now().Add(resetAfter),
	}
	if !allowed {
		decision.RetryAfter = time.Duration(metrics[1]) * time.Microsecond
	}
	return decision, nil
}

var _ limiter.Limiter = (*LeakyBucket)(nil)
