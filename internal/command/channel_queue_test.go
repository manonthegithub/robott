package command

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestChannelQueue_EnqueueDequeueRoundtrip(t *testing.T) {
	q := NewChannelQueue(1)
	want := LEDCommand{On: true}

	if err := q.Enqueue(want); err != nil {
		t.Fatalf("Enqueue() error = %v, want nil", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()

	got, err := q.Dequeue(ctx)
	if err != nil {
		t.Fatalf("Dequeue() error = %v, want nil", err)
	}
	if got != Command(want) {
		t.Fatalf("Dequeue() = %v, want %v", got, want)
	}
}

func TestChannelQueue_EnqueueFullReturnsErrQueueFull(t *testing.T) {
	q := NewChannelQueue(1)

	if err := q.Enqueue(LEDCommand{On: true}); err != nil {
		t.Fatalf("first Enqueue() error = %v, want nil", err)
	}

	err := q.Enqueue(LEDCommand{On: false})
	if !errors.Is(err, ErrQueueFull) {
		t.Fatalf("second Enqueue() error = %v, want ErrQueueFull", err)
	}
}

func TestChannelQueue_DequeueUnblocksOnContextCancel(t *testing.T) {
	q := NewChannelQueue(1)

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		_, err := q.Dequeue(ctx)
		done <- err
	}()

	cancel()

	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("Dequeue() error = %v, want context.Canceled", err)
		}
	case <-time.After(time.Second):
		t.Fatal("Dequeue() did not unblock within 1s of ctx cancel")
	}
}
