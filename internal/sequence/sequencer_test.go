package sequence

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"robottt/internal/command"
)

// drainQueue drains q in the background, appending every dequeued command to
// a thread-safe slice a test can inspect.
type drainedQueue struct {
	*command.ChannelQueue
	mu    sync.Mutex
	items []command.Command
}

func newDrainedQueue(ctx context.Context, capacity int) *drainedQueue {
	q := &drainedQueue{ChannelQueue: command.NewChannelQueue(capacity)}
	go func() {
		for {
			cmd, err := q.Dequeue(ctx)
			if err != nil {
				return
			}
			q.mu.Lock()
			q.items = append(q.items, cmd)
			q.mu.Unlock()
		}
	}()
	return q
}

func (q *drainedQueue) snapshot() []command.Command {
	q.mu.Lock()
	defer q.mu.Unlock()
	out := make([]command.Command, len(q.items))
	copy(out, q.items)
	return out
}

// waitForCompletion polls s.running directly (white-box, same package) until
// it flips false, meaning the sequence's goroutine has finished on its own.
// Deliberately does not call Stop() to observe this — Stop() is destructive
// (it cancels a still-running sequence), which would corrupt tests that want
// to observe a sequence reaching *natural* completion.
func waitForCompletion(t *testing.T, s *Sequencer, timeout time.Duration) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		s.mu.Lock()
		running := s.running
		s.mu.Unlock()
		if !running {
			return
		}
		time.Sleep(2 * time.Millisecond)
	}
	t.Fatal("sequence did not complete within timeout")
}

func TestSequencer_OrderingMatchesNestedExample(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	q := newDrainedQueue(ctx, 32)
	s := &Sequencer{Queue: q}

	seq := OperationSequence{Seq: []Operation{
		Loop{Times: 2, Body: []Operation{
			NewLedCommand(true, 0),
			NewLedCommand(false, 0),
		}},
		NewServoCommand(90, 0),
		Loop{Times: 2, Body: []Operation{
			NewServoCommand(45, 0),
			NewServoCommand(0, 0),
		}},
	}}

	if err := s.Start(seq); err != nil {
		t.Fatalf("Start() error = %v, want nil", err)
	}
	waitForCompletion(t, s, time.Second)

	want := []command.Command{
		command.LEDCommand{On: true}, command.LEDCommand{On: false},
		command.LEDCommand{On: true}, command.LEDCommand{On: false},
		command.ServoCommand{AngleDeg: 90},
		command.ServoCommand{AngleDeg: 45}, command.ServoCommand{AngleDeg: 0},
		command.ServoCommand{AngleDeg: 45}, command.ServoCommand{AngleDeg: 0},
	}
	got := q.snapshot()
	if len(got) != len(want) {
		t.Fatalf("got %d commands, want %d: %v", len(got), len(want), got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("command[%d] = %v, want %v", i, got[i], want[i])
		}
	}
}

func TestSequencer_FiniteLoopEnqueuesExactCount(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	q := newDrainedQueue(ctx, 32)
	s := &Sequencer{Queue: q}

	const times = 3
	seq := OperationSequence{Seq: []Operation{
		Loop{Times: times, Body: []Operation{
			NewLedCommand(true, 0),
			NewLedCommand(false, 0),
		}},
	}}

	if err := s.Start(seq); err != nil {
		t.Fatalf("Start() error = %v, want nil", err)
	}
	waitForCompletion(t, s, time.Second)

	if got, want := len(q.snapshot()), times*2; got != want {
		t.Fatalf("got %d commands, want %d", got, want)
	}
}

func TestSequencer_InfiniteLoopStoppedPromptly(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	q := newDrainedQueue(ctx, 32)
	s := &Sequencer{Queue: q}

	seq := OperationSequence{Seq: []Operation{
		Loop{Times: 0, Body: []Operation{NewLedCommand(true, 0)}},
	}}

	if err := s.Start(seq); err != nil {
		t.Fatalf("Start() error = %v, want nil", err)
	}

	time.Sleep(20 * time.Millisecond) // let it enqueue a handful of commands

	if err := s.Stop(); err != nil {
		t.Fatalf("Stop() error = %v, want nil", err)
	}
	waitForCompletion(t, s, time.Second)

	countAtStop := len(q.snapshot())
	time.Sleep(50 * time.Millisecond)
	countAfter := len(q.snapshot())
	if countAfter != countAtStop {
		t.Fatalf("commands kept arriving after Stop(): %d before extra wait, %d after", countAtStop, countAfter)
	}
}

func TestSequencer_StartWhileRunningReturnsErrAlreadyRunning(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	q := newDrainedQueue(ctx, 32)
	s := &Sequencer{Queue: q}

	seq := OperationSequence{Seq: []Operation{
		Loop{Times: 0, Body: []Operation{NewLedCommand(true, 5)}},
	}}
	if err := s.Start(seq); err != nil {
		t.Fatalf("first Start() error = %v, want nil", err)
	}
	defer func() {
		_ = s.Stop()
		waitForCompletion(t, s, time.Second)
	}()

	if err := s.Start(seq); !errors.Is(err, ErrAlreadyRunning) {
		t.Fatalf("second Start() error = %v, want ErrAlreadyRunning", err)
	}
}

func TestSequencer_ShutdownCtxCancelsRunningSequenceWithoutStop(t *testing.T) {
	// Sequencer.Ctx models the server's shutdown context. A running
	// sequence must end on its own when Ctx is cancelled, without anyone
	// calling Stop() — otherwise it's left parked on the queue forever once
	// whatever drains the queue (the executor) has also stopped.
	shutdownCtx, shutdown := context.WithCancel(context.Background())
	defer shutdown()

	drainCtx, cancelDrain := context.WithCancel(context.Background())
	defer cancelDrain()
	q := newDrainedQueue(drainCtx, 32)
	s := &Sequencer{Queue: q, Ctx: shutdownCtx}

	seq := OperationSequence{Seq: []Operation{
		Loop{Times: 0, Body: []Operation{NewLedCommand(true, 0)}},
	}}
	if err := s.Start(seq); err != nil {
		t.Fatalf("Start() error = %v, want nil", err)
	}

	time.Sleep(20 * time.Millisecond) // let it enqueue a handful of commands
	shutdown()                        // simulate server shutdown, not Stop()

	waitForCompletion(t, s, time.Second) // must end on its own
}

func TestSequencer_NilCtxFallsBackToBackgroundLikeBefore(t *testing.T) {
	// Existing callers (and every other test in this file) construct
	// Sequencer without setting Ctx — must keep working exactly as before.
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	q := newDrainedQueue(ctx, 32)
	s := &Sequencer{Queue: q}

	seq := OperationSequence{Seq: []Operation{NewLedCommand(true, 0)}}
	if err := s.Start(seq); err != nil {
		t.Fatalf("Start() error = %v, want nil", err)
	}
	waitForCompletion(t, s, time.Second)
}

func TestSequencer_StopWithNothingRunningReturnsErrNotRunning(t *testing.T) {
	s := &Sequencer{Queue: newDrainedQueue(context.Background(), 1)}
	if err := s.Stop(); !errors.Is(err, ErrNotRunning) {
		t.Fatalf("Stop() error = %v, want ErrNotRunning", err)
	}
}

func TestSequencer_BlockedQueueUnblockedByStop(t *testing.T) {
	// Undrained queue with capacity 1: the second enqueued command will
	// block on EnqueueBlocking until either space frees (never, here) or
	// the sequence is stopped.
	q := command.NewChannelQueue(1)
	s := &Sequencer{Queue: q}

	seq := OperationSequence{Seq: []Operation{
		NewLedCommand(true, 0),
		NewLedCommand(false, 0), // blocks: queue capacity 1, nothing draining
	}}
	if err := s.Start(seq); err != nil {
		t.Fatalf("Start() error = %v, want nil", err)
	}

	time.Sleep(20 * time.Millisecond) // let it fill the queue and block

	if err := s.Stop(); err != nil {
		t.Fatalf("Stop() error = %v, want nil", err)
	}
	waitForCompletion(t, s, time.Second) // must not hang
}

func TestSequencer_RestartAfterNaturalCompletionSucceeds(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	q := newDrainedQueue(ctx, 32)
	s := &Sequencer{Queue: q}

	seq := OperationSequence{Seq: []Operation{NewLedCommand(true, 0)}}

	if err := s.Start(seq); err != nil {
		t.Fatalf("first Start() error = %v, want nil", err)
	}
	waitForCompletion(t, s, time.Second)

	// A fresh Start() right after natural completion (not Stop()) must
	// succeed, not be wrongly locked into "running".
	if err := s.Start(seq); err != nil {
		t.Fatalf("Start() after natural completion error = %v, want nil", err)
	}
	waitForCompletion(t, s, time.Second)
}

func TestSequencer_ParBranchesRaceIndependently(t *testing.T) {
	// Branch A's command has a longer delay than branch B's — if branches
	// really run concurrently (not one after the other), branch B's command
	// must arrive on the queue first despite being listed second.
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	q := newDrainedQueue(ctx, 32)
	s := &Sequencer{Queue: q}

	seq := OperationSequence{Seq: []Operation{
		Par{Branches: [][]Operation{
			{NewLedCommand(true, 150)},   // branch A: slower
			{NewServoCommand(90, 10)},    // branch B: faster, listed second
		}},
	}}
	if err := s.Start(seq); err != nil {
		t.Fatalf("Start() error = %v, want nil", err)
	}
	waitForCompletion(t, s, time.Second)

	got := q.snapshot()
	if len(got) != 2 {
		t.Fatalf("got %d commands, want 2: %v", len(got), got)
	}
	if got[0] != command.Command(command.ServoCommand{AngleDeg: 90}) {
		t.Fatalf("first command = %v, want the faster branch's ServoCommand (proves branches ran concurrently, not in list order)", got[0])
	}
}

func TestSequencer_ParJoinsBeforeContinuing(t *testing.T) {
	// A step after the Par must not run until *all* branches finish, not
	// just the fastest one.
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	q := newDrainedQueue(ctx, 32)
	s := &Sequencer{Queue: q}

	const slowDelay = 150 * time.Millisecond
	seq := OperationSequence{Seq: []Operation{
		Par{Branches: [][]Operation{
			{NewLedCommand(true, int(slowDelay.Milliseconds()))},
			{NewServoCommand(90, 10)},
		}},
		NewServoCommand(0, 0), // must wait for the slow branch
	}}
	start := time.Now()
	if err := s.Start(seq); err != nil {
		t.Fatalf("Start() error = %v, want nil", err)
	}
	waitForCompletion(t, s, time.Second)

	got := q.snapshot()
	if len(got) != 3 {
		t.Fatalf("got %d commands, want 3: %v", len(got), got)
	}
	if got[2] != command.Command(command.ServoCommand{AngleDeg: 0}) {
		t.Fatalf("last command = %v, want the post-Par ServoCommand{AngleDeg:0}", got[2])
	}
	if elapsed := time.Since(start); elapsed < slowDelay {
		t.Fatalf("post-Par step arrived after %v, want at least %v (must wait for the slow branch)", elapsed, slowDelay)
	}
}

func TestSequencer_ParStoppedMidBranchesEndsCleanly(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	q := newDrainedQueue(ctx, 32)
	s := &Sequencer{Queue: q}

	seq := OperationSequence{Seq: []Operation{
		Par{Branches: [][]Operation{
			{Loop{Times: 0, Body: []Operation{NewLedCommand(true, 0)}}},
			{Loop{Times: 0, Body: []Operation{NewServoCommand(90, 0)}}},
		}},
	}}
	if err := s.Start(seq); err != nil {
		t.Fatalf("Start() error = %v, want nil", err)
	}

	time.Sleep(20 * time.Millisecond)

	if err := s.Stop(); err != nil {
		t.Fatalf("Stop() error = %v, want nil", err)
	}
	waitForCompletion(t, s, time.Second) // must not hang waiting on either branch
}

func TestSequencer_StartRejectsInvalidSequence(t *testing.T) {
	s := &Sequencer{Queue: command.NewChannelQueue(1)}
	seq := OperationSequence{Seq: []Operation{
		Loop{Times: -1, Body: []Operation{NewLedCommand(true, 0)}},
	}}
	if err := s.Start(seq); !errors.Is(err, ErrNegativeTimes) {
		t.Fatalf("Start() error = %v, want ErrNegativeTimes", err)
	}
	// A rejected Start() must not flip running to true.
	if err := s.Stop(); !errors.Is(err, ErrNotRunning) {
		t.Fatalf("Stop() after a rejected Start() error = %v, want ErrNotRunning", err)
	}
}
