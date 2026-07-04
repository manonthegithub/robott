package command

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestDelayedEnqueue_ZeroDelayEnqueuesImmediately(t *testing.T) {
	q := NewChannelQueue(1)
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()

	start := time.Now()
	if err := DelayedEnqueue(ctx, q, 0, LEDCommand{On: true}); err != nil {
		t.Fatalf("DelayedEnqueue() error = %v, want nil", err)
	}
	if elapsed := time.Since(start); elapsed > 20*time.Millisecond {
		t.Fatalf("DelayedEnqueue() with delay=0 took %v, want ~immediate", elapsed)
	}

	got, err := q.Dequeue(ctx)
	if err != nil {
		t.Fatalf("Dequeue() error = %v, want nil", err)
	}
	if got != Command(LEDCommand{On: true}) {
		t.Fatalf("Dequeue() = %v, want LEDCommand{On:true}", got)
	}
}

func TestDelayedEnqueue_NegativeDelayTreatedAsZero(t *testing.T) {
	q := NewChannelQueue(1)
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()

	start := time.Now()
	if err := DelayedEnqueue(ctx, q, -5*time.Second, LEDCommand{On: true}); err != nil {
		t.Fatalf("DelayedEnqueue() error = %v, want nil", err)
	}
	if elapsed := time.Since(start); elapsed > 20*time.Millisecond {
		t.Fatalf("DelayedEnqueue() with negative delay took %v, want ~immediate (no hang)", elapsed)
	}
}

func TestDelayedEnqueue_PositiveDelayWaitsBeforeEnqueuing(t *testing.T) {
	q := NewChannelQueue(1)
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()

	const delay = 100 * time.Millisecond
	start := time.Now()
	done := make(chan error, 1)
	go func() {
		done <- DelayedEnqueue(ctx, q, delay, LEDCommand{On: true})
	}()

	// Command must not be on the queue before the delay elapses.
	select {
	case cmd := <-q.ch:
		t.Fatalf("command %v appeared on the queue before the delay elapsed", cmd)
	case <-time.After(delay / 2):
	}

	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("DelayedEnqueue() error = %v, want nil", err)
		}
		if elapsed := time.Since(start); elapsed < delay {
			t.Fatalf("DelayedEnqueue() returned after %v, want at least %v", elapsed, delay)
		}
	case <-time.After(time.Second):
		t.Fatal("DelayedEnqueue() did not complete within 1s")
	}
}

func TestDelayedEnqueue_ContextCancelledDuringDelayReturnsEarlyWithoutEnqueuing(t *testing.T) {
	q := NewChannelQueue(1)
	ctx, cancel := context.WithCancel(context.Background())

	done := make(chan error, 1)
	go func() {
		done <- DelayedEnqueue(ctx, q, time.Hour, LEDCommand{On: true})
	}()

	cancel()

	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("DelayedEnqueue() error = %v, want context.Canceled", err)
		}
	case <-time.After(time.Second):
		t.Fatal("DelayedEnqueue() did not unblock within 1s of ctx cancel")
	}

	select {
	case cmd := <-q.ch:
		t.Fatalf("command %v was enqueued despite ctx being cancelled during the delay", cmd)
	default:
	}
}

func TestDelayedEnqueue_ContextCancelledWhileBlockedOnFullQueue(t *testing.T) {
	q := NewChannelQueue(1)
	if err := q.Enqueue(LEDCommand{On: true}); err != nil {
		t.Fatalf("setup Enqueue() error = %v, want nil", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		done <- DelayedEnqueue(ctx, q, 0, LEDCommand{On: false})
	}()

	cancel()

	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("DelayedEnqueue() error = %v, want context.Canceled", err)
		}
	case <-time.After(time.Second):
		t.Fatal("DelayedEnqueue() did not unblock within 1s of ctx cancel while queue was full")
	}
}
