package retry

import (
	"context"
	"time"
)

type Strategy interface {
	NextDelay(attempt int) time.Duration
	ShouldRetry(ctx context.Context, attempt int, err error) bool
}

type ExponentialBackoff struct {
	InitialDelay time.Duration
	MaxDelay     time.Duration
	MaxAttempts  int
}

func (e *ExponentialBackoff) NextDelay(attempt int) time.Duration {
	delay := e.InitialDelay * time.Duration(1<<uint(attempt))
	if delay > e.MaxDelay {
		delay = e.MaxDelay
	}
	return delay
}

func (e *ExponentialBackoff) ShouldRetry(ctx context.Context, attempt int, err error) bool {
	if attempt >= e.MaxAttempts {
		return false
	}
	select {
	case <-ctx.Done():
		return false
	default:
		return true
	}
}

type FixedDelay struct {
	Delay       time.Duration
	MaxAttempts int
}

func (f *FixedDelay) NextDelay(attempt int) time.Duration {
	return f.Delay
}

func (f *FixedDelay) ShouldRetry(ctx context.Context, attempt int, err error) bool {
	if attempt >= f.MaxAttempts {
		return false
	}
	select {
	case <-ctx.Done():
		return false
	default:
		return true
	}
}

