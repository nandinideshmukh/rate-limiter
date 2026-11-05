package policy

import (
	"context"
	"testing"

	"rate-limiter/internal/limiter"
)

type testLimiter struct{}

func (testLimiter) Allow(context.Context, string, limiter.Config) (limiter.Decision, error) {
	return limiter.Decision{}, nil
}

func TestManagerGetPolicyAndDefault(t *testing.T) {
	defaultPolicy := Policy{
		Limiter: testLimiter{},
		Config:  limiter.Config{Limit: 5},
	}
	proPolicy := Policy{
		Limiter: testLimiter{},
		Config:  limiter.Config{Limit: 30},
	}
	manager := NewManager(defaultPolicy, map[string]Policy{
		"pro-key": proPolicy,
	})

	if got := manager.Get("pro-key"); got.Config != proPolicy.Config {
		t.Errorf("known API key config = %+v, want %+v", got.Config, proPolicy.Config)
	}
	if got := manager.Get("unknown-key"); got.Config != defaultPolicy.Config {
		t.Errorf("unknown API key config = %+v, want default %+v", got.Config, defaultPolicy.Config)
	}
}
