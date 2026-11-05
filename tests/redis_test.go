package tests

import (
	"context"
	"fmt"
	"rate-limiter/internal/limiter"
	"rate-limiter/internal/storage"
	"sync"
	"testing"
	"time"
)

func TestRedisConnection(t *testing.T) {

	r, err := storage.NewRedisClient("localhost:6379")

	if err != nil {
		t.Fatalf("Redis connection failed: %v", err)
	}

	defer r.Close()

	result, err := r.Client.Ping(context.Background()).Result()

	if err != nil {
		t.Fatalf("Redis ping failed: %v", err)
	}

	if result != "PONG" {
		t.Fatalf("Expected PONG, got %s", result)
	}

	t.Log("Redis connection successful!")
}

func TestRedisSetGet(t *testing.T) {

	r, err := storage.NewRedisClient("localhost:6379")

	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()

	ctx := context.Background()

	// Store a key-value pair.
	err = r.Client.Set(ctx, "ratelimitx:test", "hello redis", 0).Err()

	if err != nil {
		t.Fatal(err)
	}

	// Retrieve the value.
	value, err := r.Client.Get(ctx, "ratelimitx:test").Result()

	if err != nil {
		t.Fatal(err)
	}

	if value != "hello redis" {
		t.Fatalf("Expected hello redis, got %s", value)
	}

	t.Log("Redis SET and GET successful!")
}

func TestRedisFixedWindowSharesCounterAcrossInstances(t *testing.T) {
	r, err := storage.NewRedisClient("localhost:6379")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := r.Close(); err != nil {
			t.Errorf("close Redis client: %v", err)
		}
	})

	ctx := context.Background()
	key := fmt.Sprintf("shared-counter:%d", time.Now().UnixNano())
	redisKey := "ratelimit:fixed-window:" + key
	t.Cleanup(func() {
		if err := r.Client.Del(ctx, redisKey).Err(); err != nil {
			t.Errorf("cleanup Redis fixed-window counter: %v", err)
		}
	})

	limit := 7
	config := limiter.Config{Limit: limit, Window: time.Minute}
	instances := []*storage.FixedWindow{
		storage.NewRedisFixedWindow(r.Client),
		storage.NewRedisFixedWindow(r.Client),
	}

	const requestCount = 100
	start := make(chan struct{})
	results := make(chan limiter.Decision, requestCount)
	errors := make(chan error, requestCount)
	var workers sync.WaitGroup
	workers.Add(requestCount)
	for i := 0; i < requestCount; i++ {
		go func(i int) {
			defer workers.Done()
			<-start
			decision, err := instances[i%len(instances)].Allow(ctx, key, config)
			if err != nil {
				errors <- err
				return
			}
			results <- decision
		}(i)
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
		} else if result.RetryAfter <= 0 {
			t.Errorf("rejected request should have a positive retry delay, got %v", result.RetryAfter)
		}
	}
	if allowed != limit {
		t.Fatalf("expected exactly %d allowed requests across instances, got %d", limit, allowed)
	}
}
