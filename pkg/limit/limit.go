package limit

import (
	"context"
	"fmt"
	"math/rand"
	"sync"
	"time"
)

// Limiter sleeps a randomized interval before each request: the delay is
// drawn uniformly from [interval/2, interval*1.5] (i.e. interval ±50%),
// measured from the moment Wait is called — not from the previous request.
// The jitter makes the request rhythm less fingerprintable.
// A zero/negative interval disables limiting.
type Limiter struct {
	mu       sync.Mutex
	interval time.Duration
	first    bool
}

func New(interval time.Duration) *Limiter {
	return &Limiter{interval: interval, first: true}
}

// Wait blocks for a jittered interval before allowing the next request.
// The very first call returns immediately.
func (l *Limiter) Wait(ctx context.Context) error {
	if l.interval <= 0 {
		return nil
	}
	l.mu.Lock()
	defer l.mu.Unlock()

	if l.first {
		l.first = false
		return nil
	}

	// Uniform jitter in [0.5, 1.5) × interval.
	wait := time.Duration(float64(l.interval) * (0.5 + rand.Float64()))
	if wait > 5*time.Second {
		// keep the user informed about long sleeps
		fmt.Printf("  ⏳ rate limit: waiting %s before next request\n", wait.Round(time.Second))
	}
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-time.After(wait):
	}
	return nil
}
