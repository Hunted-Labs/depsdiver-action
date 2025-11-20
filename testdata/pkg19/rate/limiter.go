package rate

import (
	"context"
	"sync"
	"time"
)

type Limiter struct {
	mu       sync.Mutex
	interval time.Duration
	lastTime time.Time
}

func NewLimiter(interval time.Duration) *Limiter {
	return &Limiter{
		interval: interval,
		lastTime: time.Now(),
	}
}

func (l *Limiter) Allow() bool {
	l.mu.Lock()
	defer l.mu.Unlock()

	now := time.Now()
	if now.Sub(l.lastTime) >= l.interval {
		l.lastTime = now
		return true
	}
	return false
}

func (l *Limiter) Wait(ctx context.Context) error {
	for !l.Allow() {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(l.interval):
		}
	}
	return nil
}

