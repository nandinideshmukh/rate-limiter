package tests

import (
	"context"
	"rate-limiter/internal/limiter"
	"testing"
	"time"
)

func TestRemainingAlgorithmsEnforceLimit(t *testing.T) {
	algorithms := []struct {
		name string
		new  func() limiter.Limiter
	}{
		{name: "sliding window log", new: func() limiter.Limiter { return limiter.NewSlidingWindowLog() }},
		{name: "sliding window counter", new: func() limiter.Limiter { return limiter.NewSlidingWindowCounter() }},
		{name: "token bucket", new: func() limiter.Limiter { return limiter.NewTokenBucket() }},
		{name: "leaky bucket", new: func() limiter.Limiter { return limiter.NewLeakyBucket() }},
	}

	for _, algorithm := range algorithms {
		t.Run(algorithm.name, func(t *testing.T) {
			l := algorithm.new()
			config := limiter.Config{
				Limit:  3,
				Window: time.Minute,
			}

			for i := 0; i < config.Limit; i++ {
				result, err := l.Allow(context.Background(), "user", config)
				if err != nil {
					t.Fatal(err)
				}
				if !result.Allowed {
					t.Fatalf("request %d should be allowed", i+1)
				}
			}

			result, err := l.Allow(context.Background(), "user", config)
			if err != nil {
				t.Fatal(err)
			}
			if result.Allowed {
				t.Fatal("request after limit should be rejected")
			}
			if result.RetryAfter <= 0 {
				t.Fatalf("retry delay should be positive, got %v", result.RetryAfter)
			}
		})
	}
}
