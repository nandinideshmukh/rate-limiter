package storage

import (
	"context"
	"fmt"
	"time"

	"github.com/redis/go-redis/v9"
)

type RedisClient struct {
	Client *redis.Client
}

// NewRedisClient creates a new Redis client.
func NewRedisClient(addr string) (*RedisClient, error) {

	client := redis.NewClient(&redis.Options{
		Addr:         addr,
		Password:     "",
		DB:           0,
		PoolSize:     10,
		MinIdleConns: 2,
	})

	// Check whether Redis is reachable.
	ctx, cancel := context.WithTimeout(
		context.Background(),
		5*time.Second,
	)
	defer cancel()

	if err := client.Ping(ctx).Err(); err != nil {
		client.Close()
		return nil, fmt.Errorf("redis connection failed: %w", err)
	}

	return &RedisClient{
		Client: client,
	}, nil
}

// Close gracefully closes the Redis connection.
func (r *RedisClient) Close() error {
	return r.Client.Close()
}
