package retry

import (
	"context"
	"time"
)

func Execute(ctx context.Context, strategy Strategy, fn func() error) error {
	attempt := 0
	for {
		err := fn()
		if err == nil {
			return nil
		}

		attempt++
		if !strategy.ShouldRetry(ctx, attempt, err) {
			return err
		}

		delay := strategy.NextDelay(attempt)
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(delay):
		}
	}
}

