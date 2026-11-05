package limiter

import (
	"context"
	"math"
	"sync"
	"time"
)

type slidingCounterEntry struct {
	windowStart time.Time
	window      time.Duration
	current     int
	previous    int
}

type SlidingWindowCounter struct {
	mu      sync.Mutex
	windows map[string]slidingCounterEntry
}

func NewSlidingWindowCounter() *SlidingWindowCounter {
	return &SlidingWindowCounter{
		windows: make(map[string]slidingCounterEntry),
	}
}

func (s *SlidingWindowCounter) Allow(
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
	entry, exists := s.windows[key]
	if !exists || entry.window != config.Window {
		entry = slidingCounterEntry{
			windowStart: now,
			window:      config.Window,
		}
	} else {
		elapsed := now.Sub(entry.windowStart)
		if elapsed >= config.Window {
			if elapsed/config.Window >= 2 {
				entry.previous = 0
			} else {
				entry.previous = entry.current
			}
			entry.current = 0
			entry.windowStart = entry.windowStart.Add(
				time.Duration(elapsed/config.Window) * config.Window,
			)
		}
	}

	elapsed := now.Sub(entry.windowStart)
	weight := 1 - float64(elapsed)/float64(config.Window)
	estimated := float64(entry.current) + float64(entry.previous)*weight
	resetAt := entry.windowStart.Add(config.Window)

	if estimated >= float64(config.Limit) {
		s.windows[key] = entry
		return Decision{
			Allowed:    false,
			Remaining:  max(0, int(math.Floor(float64(config.Limit)-estimated))),
			RetryAfter: slidingCounterRetryAfter(entry, now, config.Limit),
			ResetAt:    resetAt,
		}, nil
	}

	entry.current++
	s.windows[key] = entry
	remaining := max(0, int(math.Floor(float64(config.Limit)-
		(float64(entry.current)+float64(entry.previous)*weight))))
	return Decision{
		Allowed:   true,
		Remaining: remaining,
		ResetAt:   resetAt,
	}, nil
}

func slidingCounterRetryAfter(entry slidingCounterEntry, now time.Time, limit int) time.Duration {
	elapsed := now.Sub(entry.windowStart)
	window := entry.window
	if entry.current >= limit {
		retryAfter := window - elapsed + time.Duration(
			float64(window)*(1-float64(limit)/float64(entry.current)),
		)
		if retryAfter <= 0 {
			return time.Nanosecond
		}
		return retryAfter + time.Nanosecond
	}
	if entry.previous <= 0 {
		return window - elapsed
	}

	threshold := float64(window) *
		(1 - float64(limit-entry.current)/float64(entry.previous))
	retryAfter := time.Duration(threshold) - elapsed
	if retryAfter <= 0 {
		return time.Nanosecond
	}
	return retryAfter + time.Nanosecond
}
