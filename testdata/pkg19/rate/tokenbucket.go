package rate

import (
	"context"
	"sync"
	"time"
)

type TokenBucket struct {
	mu          sync.Mutex
	capacity    int
	tokens      int
	refillRate  time.Duration
	lastRefill  time.Time
}

func NewTokenBucket(capacity int, refillRate time.Duration) *TokenBucket {
	return &TokenBucket{
		capacity:   capacity,
		tokens:     capacity,
		refillRate: refillRate,
		lastRefill: time.Now(),
	}
}

func (tb *TokenBucket) refill() {
	tb.mu.Lock()
	defer tb.mu.Unlock()

	now := time.Now()
	elapsed := now.Sub(tb.lastRefill)
	tokensToAdd := int(elapsed / tb.refillRate)

	if tokensToAdd > 0 {
		tb.tokens = tb.capacity
		if tb.tokens > tb.capacity {
			tb.tokens = tb.capacity
		}
		tb.lastRefill = now
	}
}

func (tb *TokenBucket) Take() bool {
	tb.refill()
	tb.mu.Lock()
	defer tb.mu.Unlock()

	if tb.tokens > 0 {
		tb.tokens--
		return true
	}
	return false
}

func (tb *TokenBucket) Wait(ctx context.Context) error {
	for !tb.Take() {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(tb.refillRate):
		}
	}
	return nil
}

