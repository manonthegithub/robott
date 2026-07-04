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

func TestChannelQueue_EnqueueBlockingWithFreeCapacityReturnsImmediately(t *testing.T) {
	q := NewChannelQueue(1)

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()

	if err := q.EnqueueBlocking(ctx, LEDCommand{On: true}); err != nil {
		t.Fatalf("EnqueueBlocking() error = %v, want nil", err)
	}
}

func TestChannelQueue_EnqueueBlockingWaitsForSpaceThenSucceeds(t *testing.T) {
	q := NewChannelQueue(1)
	if err := q.Enqueue(LEDCommand{On: true}); err != nil {
		t.Fatalf("setup Enqueue() error = %v, want nil", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()

	done := make(chan error, 1)
	go func() {
		done <- q.EnqueueBlocking(ctx, LEDCommand{On: false})
	}()

	select {
	case err := <-done:
		t.Fatalf("EnqueueBlocking() returned early with err = %v, want it to block on a full queue", err)
	case <-time.After(50 * time.Millisecond):
	}

	// Free a slot; EnqueueBlocking should now unblock.
	if _, err := q.Dequeue(ctx); err != nil {
		t.Fatalf("Dequeue() error = %v, want nil", err)
	}

	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("EnqueueBlocking() error = %v, want nil once space freed", err)
		}
	case <-time.After(time.Second):
		t.Fatal("EnqueueBlocking() did not unblock within 1s of space freeing up")
	}
}

func TestChannelQueue_EnqueueBlockingUnblocksOnContextCancel(t *testing.T) {
	q := NewChannelQueue(1)
	if err := q.Enqueue(LEDCommand{On: true}); err != nil {
		t.Fatalf("setup Enqueue() error = %v, want nil", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		done <- q.EnqueueBlocking(ctx, LEDCommand{On: false})
	}()

	cancel()

	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("EnqueueBlocking() error = %v, want context.Canceled", err)
		}
	case <-time.After(time.Second):
		t.Fatal("EnqueueBlocking() did not unblock within 1s of ctx cancel")
	}
}

func TestChannelQueue_EnqueueBlockingWithAlreadyCancelledContextNeverSends(t *testing.T) {
	q := NewChannelQueue(1) // has free capacity

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // already cancelled before the call

	if err := q.EnqueueBlocking(ctx, LEDCommand{On: true}); !errors.Is(err, context.Canceled) {
		t.Fatalf("EnqueueBlocking() error = %v, want context.Canceled even though queue had room", err)
	}

	// The command must not have been sent despite free capacity.
	select {
	case cmd := <-q.ch:
		t.Fatalf("EnqueueBlocking() sent %v onto the queue despite an already-cancelled ctx", cmd)
	default:
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
