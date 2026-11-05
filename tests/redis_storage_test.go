package tests

import (
	"context"
	"fmt"
	"rate-limiter/internal/limiter"
	"rate-limiter/internal/storage"
	"sync"
	"testing"
	"time"

	"github.com/redis/go-redis/v9"
)

type redisStorageAlgorithm struct {
	name      string
	keyPrefix string
	new       func(*redis.Client) limiter.Limiter
}

func redisStorageAlgorithms() []redisStorageAlgorithm {
	return []redisStorageAlgorithm{
		{
			name:      "fixed window",
			keyPrefix: "ratelimit:fixed-window:",
			new: func(client *redis.Client) limiter.Limiter {
				return storage.NewRedisFixedWindow(client)
			},
		},
		{
			name:      "sliding window log",
			keyPrefix: "ratelimit:sliding-window-log:",
			new: func(client *redis.Client) limiter.Limiter {
				return storage.NewRedisSlidingWindowLog(client)
			},
		},
		{
			name:      "sliding window counter",
			keyPrefix: "ratelimit:sliding-window-counter:",
			new: func(client *redis.Client) limiter.Limiter {
				return storage.NewRedisSlidingWindowCounter(client)
			},
		},
		{
			name:      "token bucket",
			keyPrefix: "ratelimit:token-bucket:",
			new: func(client *redis.Client) limiter.Limiter {
				return storage.NewRedisTokenBucket(client)
			},
		},
		{
			name:      "leaky bucket",
			keyPrefix: "ratelimit:leaky-bucket:",
			new: func(client *redis.Client) limiter.Limiter {
				return storage.NewRedisLeakyBucket(client)
			},
		},
	}
}

func TestRedisStorageAlgorithmsShareLimits(t *testing.T) {
	redisClient, err := storage.NewRedisClient("localhost:6379")
	if err != nil {
		t.Fatalf("connect to Redis: %v", err)
	}
	defer func() {
		if err := redisClient.Close(); err != nil {
			t.Errorf("close Redis client: %v", err)
		}
	}()

	for _, algorithm := range redisStorageAlgorithms() {
		t.Run(algorithm.name, func(t *testing.T) {
			config := limiter.Config{Limit: 4, Window: time.Minute}
			instances := []limiter.Limiter{
				algorithm.new(redisClient.Client),
				algorithm.new(redisClient.Client),
			}
			key := redisTestKey(t.Name())
			registerRedisKeyCleanup(t, redisClient.Client, algorithm.keyPrefix+key+"*")

			for request := 1; request <= config.Limit; request++ {
				decision, err := instances[request%len(instances)].Allow(
					context.Background(),
					key,
					config,
				)
				if err != nil {
					t.Fatalf("request %d returned error: %v", request, err)
				}
				if !decision.Allowed {
					t.Fatalf("request %d should be allowed", request)
				}
				if decision.Remaining != config.Limit-request {
					t.Errorf("request %d: remaining = %d, want %d",
						request, decision.Remaining, config.Limit-request)
				}
			}

			decision, err := instances[0].Allow(context.Background(), key, config)
			if err != nil {
				t.Fatalf("request over limit returned error: %v", err)
			}
			if decision.Allowed {
				t.Fatal("request over the limit should be rejected")
			}
			if decision.RetryAfter <= 0 {
				t.Errorf("rejected request should have a positive retry delay, got %v",
					decision.RetryAfter)
			}

			sharedDecision, err := instances[1].Allow(context.Background(), key, config)
			if err != nil {
				t.Fatalf("request from second instance returned error: %v", err)
			}
			if sharedDecision.Allowed {
				t.Fatal("second limiter instance should observe the shared limit")
			}
		})
	}
}

func TestRedisStorageAlgorithmsEnforceLimitsConcurrently(t *testing.T) {
	redisClient, err := storage.NewRedisClient("localhost:6379")
	if err != nil {
		t.Fatalf("connect to Redis: %v", err)
	}
	defer func() {
		if err := redisClient.Close(); err != nil {
			t.Errorf("close Redis client: %v", err)
		}
	}()

	for _, algorithm := range redisStorageAlgorithms() {
		t.Run(algorithm.name, func(t *testing.T) {
			const requestCount = 64
			config := limiter.Config{Limit: 5, Window: time.Minute}
			instances := []limiter.Limiter{
				algorithm.new(redisClient.Client),
				algorithm.new(redisClient.Client),
			}
			key := redisTestKey(t.Name())
			registerRedisKeyCleanup(t, redisClient.Client, algorithm.keyPrefix+key+"*")

			start := make(chan struct{})
			results := make(chan limiter.Decision, requestCount)
			errors := make(chan error, requestCount)
			var workers sync.WaitGroup
			workers.Add(requestCount)
			for i := 0; i < requestCount; i++ {
				go func(i int) {
					defer workers.Done()
					<-start
					decision, err := instances[i%len(instances)].Allow(
						context.Background(),
						key,
						config,
					)
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
			for decision := range results {
				if decision.Allowed {
					allowed++
				} else if decision.RetryAfter <= 0 {
					t.Errorf("rejected request should have a positive retry delay, got %v",
						decision.RetryAfter)
				}
			}
			if allowed != config.Limit {
				t.Errorf("expected exactly %d allowed requests, got %d", config.Limit, allowed)
			}
		})
	}
}

func TestRedisStorageNamespacesCallersAndClientsAreIndependent(t *testing.T) {
	firstClient, err := storage.NewRedisClient("localhost:6379")
	if err != nil {
		t.Fatalf("connect first Redis client: %v", err)
	}
	t.Cleanup(func() {
		if err := firstClient.Close(); err != nil {
			t.Errorf("close first Redis client: %v", err)
		}
	})

	secondClient, err := storage.NewRedisClient("localhost:6379")
	if err != nil {
		t.Fatalf("connect second Redis client: %v", err)
	}
	t.Cleanup(func() {
		if err := secondClient.Close(); err != nil {
			t.Errorf("close second Redis client: %v", err)
		}
	})

	algorithms := redisStorageAlgorithms()
	namespaceKey := redisTestKey(t.Name() + ":namespaces")
	clientKey := redisTestKey(t.Name() + ":clients")
	config := limiter.Config{Limit: 1, Window: time.Minute, BurstCapacity: 1}
	for _, algorithm := range algorithms {
		registerRedisKeyCleanup(t, firstClient.Client, algorithm.keyPrefix+namespaceKey+"*")
		registerRedisKeyCleanup(t, firstClient.Client, algorithm.keyPrefix+clientKey+"*")
	}

	for _, algorithm := range algorithms {
		t.Run(algorithm.name+"/independent namespace", func(t *testing.T) {
			decision, err := algorithm.new(firstClient.Client).Allow(
				context.Background(),
				namespaceKey,
				config,
			)
			if err != nil {
				t.Fatalf("first request returned error: %v", err)
			}
			if !decision.Allowed {
				t.Fatal("algorithm namespace should have independent Redis state")
			}
		})
	}

	for _, algorithm := range algorithms {
		t.Run(algorithm.name+"/shared clients and callers", func(t *testing.T) {
			firstLimiter := algorithm.new(firstClient.Client)
			secondLimiter := algorithm.new(secondClient.Client)

			decision, err := firstLimiter.Allow(context.Background(), clientKey, config)
			if err != nil {
				t.Fatalf("request from first Redis client returned error: %v", err)
			}
			if !decision.Allowed {
				t.Fatal("first request should be allowed")
			}

			decision, err = secondLimiter.Allow(context.Background(), clientKey, config)
			if err != nil {
				t.Fatalf("request from second Redis client returned error: %v", err)
			}
			if decision.Allowed {
				t.Fatal("second Redis client should observe the shared limit")
			}

			decision, err = secondLimiter.Allow(context.Background(), clientKey+":different-caller", config)
			if err != nil {
				t.Fatalf("request from different caller returned error: %v", err)
			}
			if !decision.Allowed {
				t.Fatal("different callers should have independent Redis state")
			}
		})
	}
}

func redisTestKey(name string) string {
	return fmt.Sprintf("%s:%d", name, time.Now().UnixNano())
}

func registerRedisKeyCleanup(t *testing.T, client *redis.Client, pattern string) {
	t.Helper()
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()

		var cursor uint64
		for {
			keys, nextCursor, err := client.Scan(ctx, cursor, pattern, 100).Result()
			if err != nil {
				t.Errorf("scan Redis test keys for cleanup: %v", err)
				return
			}
			if len(keys) > 0 {
				if err := client.Del(ctx, keys...).Err(); err != nil {
					t.Errorf("delete Redis test keys: %v", err)
					return
				}
			}
			if nextCursor == 0 {
				return
			}
			cursor = nextCursor
		}
	})
}
