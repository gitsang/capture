package main

import (
	"context"
	"fmt"
	"sync"
	"time"
)

// Limiter enforces a minimum interval between successive requests.
// A zero/negative interval disables limiting.
type Limiter struct {
	mu       sync.Mutex
	interval time.Duration
	last     time.Time
}

func NewLimiter(interval time.Duration) *Limiter {
	return &Limiter{interval: interval}
}

// Wait blocks until the next request is allowed.
func (l *Limiter) Wait(ctx context.Context) error {
	if l.interval <= 0 {
		return nil
	}
	l.mu.Lock()
	defer l.mu.Unlock()

	now := time.Now()
	next := l.last.Add(l.interval)
	if !l.last.IsZero() && now.Before(next) {
		wait := next.Sub(now)
		if wait > 5*time.Second {
			// keep the user informed about long sleeps
			fmt.Printf("  ⏳ rate limit: waiting %s before next request\n", wait.Round(time.Second))
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(wait):
		}
	}
	l.last = time.Now()
	return nil
}
