package limiter

import (
	"context"
	"math"
	"sync"
	"time"
)

type tokenBucketEntry struct {
	tokens    float64
	updatedAt time.Time
}

type TokenBucket struct {
	mu      sync.Mutex
	buckets map[string]tokenBucketEntry
}

func NewTokenBucket() *TokenBucket {
	return &TokenBucket{
		buckets: make(map[string]tokenBucketEntry),
	}
}

func (t *TokenBucket) Allow(
	ctx context.Context,
	key string,
	config Config,
) (Decision, error) {
	if err := ctx.Err(); err != nil {
		return Decision{}, err
	}
	if config.Limit <= 0 || config.Window <= 0 || config.BurstCapacity < 0 {
		return Decision{}, ErrInvalidConfig
	}

	capacity := config.BurstCapacity
	if capacity == 0 {
		capacity = config.Limit
	}
	refillRate := float64(config.Limit) / config.Window.Seconds()

	t.mu.Lock()
	defer t.mu.Unlock()

	now := time.Now()
	entry, exists := t.buckets[key]
	if !exists {
		entry = tokenBucketEntry{
			tokens:    float64(capacity),
			updatedAt: now,
		}
	} else {
		elapsed := now.Sub(entry.updatedAt).Seconds()
		entry.tokens = math.Min(float64(capacity), entry.tokens+elapsed*refillRate)
		entry.updatedAt = now
	}

	if entry.tokens < 1 {
		retryAfter := durationForSeconds((1 - entry.tokens) / refillRate)
		t.buckets[key] = entry
		return Decision{
			Allowed:    false,
			Remaining:  int(math.Floor(entry.tokens)),
			RetryAfter: retryAfter,
			ResetAt:    now.Add(durationForSeconds((float64(capacity) - entry.tokens) / refillRate)),
		}, nil
	}

	entry.tokens--
	t.buckets[key] = entry
	return Decision{
		Allowed:   true,
		Remaining: int(math.Floor(entry.tokens)),
		ResetAt:   now.Add(durationForSeconds((float64(capacity) - entry.tokens) / refillRate)),
	}, nil
}

func durationForSeconds(seconds float64) time.Duration {
	if seconds <= 0 {
		return 0
	}
	nanoseconds := seconds * float64(time.Second)
	maxDuration := float64(time.Duration(1<<63 - 1))
	if nanoseconds >= maxDuration {
		return time.Duration(1<<63 - 1)
	}
	return time.Duration(math.Ceil(nanoseconds))
}
