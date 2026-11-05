package limiter

/*
Fixed window :
Per windows limits are assigned to the number of requests in duration of start to end time of window.
*/
import (
	"context"
	"sync"
	"time"
)

type WindowEntry struct {
	Count     int
	WindowEnd time.Time
}

type FixedWindow struct {
	mu      sync.Mutex
	windows map[string]WindowEntry
}

func NewFixedWindow() *FixedWindow {
	return &FixedWindow{
		windows: make(map[string]WindowEntry),
	}
}

func (f *FixedWindow) Allow(
	ctx context.Context,
	key string,
	config Config,
) (Decision, error) {
	// if error return
	if err := ctx.Err(); err != nil {
		return Decision{}, err
	}
	// if limit or window is less than or equal to 0 return error
	if config.Limit <= 0 || config.Window <= 0 {
		return Decision{}, ErrInvalidConfig
	}
	// lock mutex and unlock at the end of the function
	f.mu.Lock()
	defer f.mu.Unlock()

	now := time.Now()

	entry, exists := f.windows[key]
	// if the entry does not exist or the window has expired, reset the count and window end time
	if !exists || !now.Before(entry.WindowEnd) {

		entry = WindowEntry{
			Count:     0,
			WindowEnd: now.Add(config.Window),
		}
	}
	// if count > limit return decision and its params
	if entry.Count >= config.Limit {

		retryAfter := time.Until(entry.WindowEnd)

		f.windows[key] = entry

		return Decision{
			Allowed:    false,
			Remaining:  0,
			RetryAfter: retryAfter,
			ResetAt:    entry.WindowEnd,
		}, nil
	}
	// else increment count variable and
	entry.Count++
	// append the entry to the windows map
	f.windows[key] = entry

	return Decision{
		Allowed:   true,
		Remaining: config.Limit - entry.Count,
		ResetAt:   entry.WindowEnd,
	}, nil
}
