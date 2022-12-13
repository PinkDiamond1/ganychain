package ugo

import (
	"context"
	"time"
)

// Returns nil on the first time `f()` returns nil, even if by that time `ctx` has
// been cancelled. Otherwise returns the last error returned by `f()`. If `f()` has
// never got a chance to run, returns `ctx.Err()`.
func Retry(ctx context.Context, taskName string, retryGapMs int64, f func() error) error {
	ticker := time.NewTicker(time.Duration(retryGapMs) * time.Millisecond)
	defer ticker.Stop()

	var lastErr error
	for {
		select {
		case <-ctx.Done():
			if lastErr == nil {
				return ctx.Err()
			}
			return lastErr

		default:
			lastErr = f()
			if lastErr == nil {
				return nil
			}

			select {
			case <-ticker.C:
			case <-ctx.Done():
			}
		}
	}
}
