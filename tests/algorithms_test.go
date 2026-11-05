package tests

import (
	"context"
	"rate-limiter/internal/limiter"
	"testing"
	"time"
)

func TestFixedWindow(t *testing.T) {

	l := limiter.NewFixedWindow()

	config := limiter.Config{
		Limit:  3,
		Window: time.Minute,
	}

	key := "user_123"

	// First request
	for i := 1; i <= 3; i++ {

		result, err := l.Allow(
			context.Background(),
			key,
			config,
		)

		if err != nil {
			t.Fatal(err)
		}

		if !result.Allowed {
			t.Fatalf("Request %d should be allowed", i)
		}
	}

	// Fourth request should be rejected.
	result, err := l.Allow(
		context.Background(),
		key,
		config,
	)

	if err != nil {
		t.Fatal(err)
	}

	if result.Allowed {
		t.Fatal("Fourth request should be rejected")
	}

	t.Log("Fixed Window test passed!")
}
