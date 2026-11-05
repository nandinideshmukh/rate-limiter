package storage

import (
	"context"
	_ "embed"
	"errors"
	"fmt"
	"math"
	"strconv"
	"time"

	"rate-limiter/internal/limiter"

	"github.com/redis/go-redis/v9"
)

const slidingWindowCounterKeyPrefix = "ratelimit:sliding-window-counter:"

//go:embed scripts/sliding_counter.lua
var slidingWindowCounterLua string

var slidingWindowCounterScript = redis.NewScript(slidingWindowCounterLua)

// SlidingWindowCounter estimates the rolling request count from two Redis counters.
type SlidingWindowCounter struct {
	client *redis.Client
}

// NewRedisSlidingWindowCounter creates a Redis-backed sliding-window counter limiter.
func NewRedisSlidingWindowCounter(client *redis.Client) *SlidingWindowCounter {
	return &SlidingWindowCounter{client: client}
}

// Allow atomically checks and increments the current window's shared counter.
func (s *SlidingWindowCounter) Allow(
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
	nowMicros := time.Now().UnixMicro()
	windowNumber := nowMicros / windowMicros
	elapsedMicros := nowMicros % windowMicros
	currentKey := slidingWindowCounterKeyPrefix + key + ":" + strconv.FormatInt(windowNumber, 10)
	previousKey := slidingWindowCounterKeyPrefix + key + ":" + strconv.FormatInt(windowNumber-1, 10)
	ttlMillis := durationMilliseconds(time.Duration(windowMicros) * time.Microsecond * 2)

	result, err := slidingWindowCounterScript.Run(
		ctx,
		s.client,
		[]string{currentKey, previousKey},
		windowMicros,
		elapsedMicros,
		config.Limit,
		ttlMillis,
	).Result()
	if err != nil {
		return limiter.Decision{}, fmt.Errorf("update Redis sliding-window counter: %w", err)
	}

	allowed, metrics, err := redisDecisionValues(result, 3)
	if err != nil {
		return limiter.Decision{}, fmt.Errorf("parse Redis sliding-window counter response: %w", err)
	}
	estimated := float64(metrics[0]) / 1000000
	remaining := max(0, config.Limit-int(math.Floor(estimated)))
	resetAfter := time.Duration(windowMicros-elapsedMicros) * time.Microsecond
	decision := limiter.Decision{
		Allowed:   allowed,
		Remaining: remaining,
		ResetAt:   time.Now().Add(resetAfter),
	}
	if !allowed {
		retryAfter := time.Duration(metrics[1]) * time.Microsecond
		decision.RetryAfter = retryAfter
	}
	return decision, nil
}

var _ limiter.Limiter = (*SlidingWindowCounter)(nil)
