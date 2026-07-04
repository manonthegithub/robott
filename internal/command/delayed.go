package command

import (
	"context"
	"time"
)

// DelayedEnqueue waits delay (if positive), then enqueues cmd onto q,
// blocking if the queue is full. It is the single place in the codebase
// that sleeps before a command reaches the queue — the executor always
// dispatches immediately on Dequeue. ctx can cancel either the wait or the
// blocked enqueue.
func DelayedEnqueue(ctx context.Context, q CommandQueue, delay time.Duration, cmd Command) error {
	if delay > 0 {
		select {
		case <-time.After(delay):
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	return q.EnqueueBlocking(ctx, cmd)
}
