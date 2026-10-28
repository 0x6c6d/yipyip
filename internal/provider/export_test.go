package provider

import (
	"context"
	"time"
)

// RetryDelay exposes the production wait schedule to the tests.
func RetryDelay(n int) time.Duration { return retryDelay(n) }

// SetRetryWait replaces the retry wait for one test and returns the restore
// function. Tests never sleep: the default stub below returns at once.
func SetRetryWait(f func(ctx context.Context, d time.Duration) error) (restore func()) {
	old := retryWait
	retryWait = f
	return func() { retryWait = old }
}

// RealRetryWait is the production wait, captured before init stubs it.
var RealRetryWait = retryWait

func init() {
	// No test of this package ever waits for a real retry delay.
	retryWait = func(ctx context.Context, _ time.Duration) error { return ctx.Err() }
}

// SetSSEEventHook installs a hook called with every SSE payload for one test
// and returns the restore function.
func SetSSEEventHook(f func(data string)) (restore func()) {
	old := sseEventHook
	sseEventHook = f
	return func() { sseEventHook = old }
}
