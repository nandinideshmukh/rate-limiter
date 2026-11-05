package limiter

import (
	"context"
	"time"
)

type Config struct {
	Limit         int
	Window        time.Duration
	BurstCapacity int
}

type Decision struct {
	Allowed    bool
	Remaining  int
	RetryAfter time.Duration
	ResetAt    time.Time
}

type Limiter interface {
	Allow(
		ctx context.Context,
		key string,
		config Config,
	) (Decision, error)
}
