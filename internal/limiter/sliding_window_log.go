package limiter

import (
	"context"
	"sync"
	"time"
)

type SlidingWindowLog struct {
	mu       sync.Mutex
	requests map[string][]time.Time
}

func NewSlidingWindowLog() *SlidingWindowLog {
	return &SlidingWindowLog{
		requests: make(map[string][]time.Time),
	}
}

func (s *SlidingWindowLog) Allow(
	ctx context.Context,
	key string,
	config Config,
) (Decision, error) {
	if err := ctx.Err(); err != nil {
		return Decision{}, err
	}
	if config.Limit <= 0 || config.Window <= 0 {
		return Decision{}, ErrInvalidConfig
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	now := time.Now()
	cutoff := now.Add(-config.Window)
	requests := s.requests[key]
	firstActive := 0
	for firstActive < len(requests) && !requests[firstActive].After(cutoff) {
		firstActive++
	}
	requests = requests[firstActive:]

	if len(requests) >= config.Limit {
		resetAt := requests[0].Add(config.Window)
		s.requests[key] = requests
		return Decision{
			Allowed:    false,
			Remaining:  0,
			RetryAfter: resetAt.Sub(now),
			ResetAt:    resetAt,
		}, nil
	}

	requests = append(requests, now)
	s.requests[key] = requests

	return Decision{
		Allowed:   true,
		Remaining: config.Limit - len(requests),
		ResetAt:   requests[0].Add(config.Window),
	}, nil
}
