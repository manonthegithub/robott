package command

import "context"

// ChannelQueue is a CommandQueue backed by an in-process buffered channel.
type ChannelQueue struct {
	ch chan Command
}

// NewChannelQueue creates a ChannelQueue with the given buffer capacity.
func NewChannelQueue(capacity int) *ChannelQueue {
	return &ChannelQueue{ch: make(chan Command, capacity)}
}

func (q *ChannelQueue) Enqueue(cmd Command) error {
	select {
	case q.ch <- cmd:
		return nil
	default:
		return ErrQueueFull
	}
}

func (q *ChannelQueue) EnqueueBlocking(ctx context.Context, cmd Command) error {
	// Checked separately (not just as a select case) so an already-cancelled
	// ctx always wins deterministically, even if the channel currently has
	// room — select would otherwise pick randomly between two ready cases.
	if err := ctx.Err(); err != nil {
		return err
	}
	select {
	case q.ch <- cmd:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (q *ChannelQueue) Dequeue(ctx context.Context) (Command, error) {
	select {
	case cmd := <-q.ch:
		return cmd, nil
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}
