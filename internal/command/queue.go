package command

import (
	"context"
	"errors"
)

// ErrQueueFull is returned by Enqueue when the queue has no free capacity.
var ErrQueueFull = errors.New("command queue full")

// CommandQueue decouples command producers (HTTP layer) from the consumer
// (executor). The v1 implementation is an in-process buffered channel; a
// persistent broker-backed implementation can satisfy the same interface
// without changing callers.
type CommandQueue interface {
	// Enqueue adds cmd without blocking. Returns ErrQueueFull if the queue
	// has no free capacity.
	Enqueue(cmd Command) error

	// EnqueueBlocking adds cmd, waiting for free capacity if the queue is
	// full, until ctx is done. The wait itself is just the underlying
	// channel send blocking (Go's native backpressure); ctx exists so a
	// caller (e.g. a cancelled sequence) can interrupt the wait.
	EnqueueBlocking(ctx context.Context, cmd Command) error

	// Dequeue blocks until a command is available or ctx is done.
	Dequeue(ctx context.Context) (Command, error)
}
