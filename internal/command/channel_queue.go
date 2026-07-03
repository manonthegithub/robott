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

func (q *ChannelQueue) Dequeue(ctx context.Context) (Command, error) {
	select {
	case cmd := <-q.ch:
		return cmd, nil
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}
