package tests

import (
	"context"
	"rate-limiter/internal/limiter"
	"sync"
	"testing"
	"time"
)

func TestAlgorithmsHandleConcurrentRequests(t *testing.T) {
	algorithms := []struct {
		name string
		new  func() limiter.Limiter
	}{
		{name: "fixed window", new: func() limiter.Limiter { return limiter.NewFixedWindow() }},
		{name: "sliding window log", new: func() limiter.Limiter { return limiter.NewSlidingWindowLog() }},
		{name: "sliding window counter", new: func() limiter.Limiter { return limiter.NewSlidingWindowCounter() }},
		{name: "token bucket", new: func() limiter.Limiter { return limiter.NewTokenBucket() }},
		{name: "leaky bucket", new: func() limiter.Limiter { return limiter.NewLeakyBucket() }},
	}

	for _, algorithm := range algorithms {
		t.Run(algorithm.name, func(t *testing.T) {
			const requestCount = 100
			const limit = 5

			l := algorithm.new()
			config := limiter.Config{
				Limit:  limit,
				Window: time.Minute,
			}
			start := make(chan struct{})
			results := make(chan limiter.Decision, requestCount)
			errors := make(chan error, requestCount)

			var workers sync.WaitGroup
			workers.Add(requestCount)
			for i := 0; i < requestCount; i++ {
				go func() {
					defer workers.Done()
					<-start
					result, err := l.Allow(context.Background(), "shared-user", config)
					if err != nil {
						errors <- err
						return
					}
					results <- result
				}()
			}

			close(start)
			workers.Wait()
			close(results)
			close(errors)

			for err := range errors {
				t.Errorf("Allow returned error: %v", err)
			}

			allowed := 0
			for result := range results {
				if result.Allowed {
					allowed++
				}
			}
			if allowed != limit {
				t.Errorf("expected exactly %d allowed requests, got %d", limit, allowed)
			}
		})
	}
}
