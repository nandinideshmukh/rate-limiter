package limiter

import (
	"context"
	"math"
	"sync"
	"time"
)

type leakyBucketEntry struct {
	level     float64
	updatedAt time.Time
}

type LeakyBucket struct {
	mu      sync.Mutex
	buckets map[string]leakyBucketEntry
}

func NewLeakyBucket() *LeakyBucket {
	return &LeakyBucket{
		buckets: make(map[string]leakyBucketEntry),
	}
}

func (l *LeakyBucket) Allow(
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
	leakRate := float64(config.Limit) / config.Window.Seconds()

	l.mu.Lock()
	defer l.mu.Unlock()

	now := time.Now()
	entry, exists := l.buckets[key]
	if exists {
		elapsed := now.Sub(entry.updatedAt).Seconds()
		entry.level = math.Max(0, entry.level-elapsed*leakRate)
	}
	entry.updatedAt = now

	if entry.level+1 > float64(capacity) {
		retryAfter := durationForSeconds((entry.level + 1 - float64(capacity)) / leakRate)
		l.buckets[key] = entry
		return Decision{
			Allowed:    false,
			Remaining:  0,
			RetryAfter: retryAfter,
			ResetAt:    now.Add(durationForSeconds(entry.level / leakRate)),
		}, nil
	}

	entry.level++
	l.buckets[key] = entry
	return Decision{
		Allowed:   true,
		Remaining: max(0, int(math.Floor(float64(capacity)-entry.level))),
		ResetAt:   now.Add(durationForSeconds(entry.level / leakRate)),
	}, nil
}
