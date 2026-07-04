package sequence

import (
	"context"
	"errors"
	"log"
	"sync"

	"robottt/internal/command"
)

// ErrAlreadyRunning is returned by Start when a sequence is already running
// — only one sequence may run at a time.
var ErrAlreadyRunning = errors.New("sequence: a sequence is already running")

// ErrNotRunning is returned by Stop when no sequence is running.
var ErrNotRunning = errors.New("sequence: no sequence is running")

// Sequencer runs at most one OperationSequence at a time against a shared
// CommandQueue. Start validates and launches a sequence in its own
// goroutine and returns immediately; Stop cancels whichever sequence is
// currently running.
//
// Ctx, if set, is the server's shutdown context: a running sequence's own
// cancellable context is derived from it, so shutdown cancels an in-flight
// sequence the same way it cancels everything else in this codebase, rather
// than leaving its goroutine parked forever on a DelayedEnqueue call against
// a queue the (now-stopped) executor no longer drains. Falls back to
// context.Background() if left nil (e.g. existing tests that don't care
// about shutdown behavior).
type Sequencer struct {
	Queue command.CommandQueue
	Ctx   context.Context

	mu      sync.Mutex
	running bool
	cancel  context.CancelFunc
}

// Start validates seq and, if no sequence is currently running, launches it
// in a new goroutine and returns immediately (it does not wait for the
// sequence to finish). Returns ErrAlreadyRunning if one is already running.
func (s *Sequencer) Start(seq OperationSequence) error {
	if err := validate(seq.Seq, 0); err != nil {
		return err
	}

	parent := s.Ctx
	if parent == nil {
		parent = context.Background()
	}

	s.mu.Lock()
	if s.running {
		s.mu.Unlock()
		return ErrAlreadyRunning
	}
	ctx, cancel := context.WithCancel(parent)
	s.running = true
	s.cancel = cancel
	s.mu.Unlock()

	go s.run(ctx, seq.Seq)
	return nil
}

// Stop cancels the currently running sequence, if any. It does not block
// waiting for the sequence's goroutine to finish unwinding. Returns
// ErrNotRunning if no sequence is running.
func (s *Sequencer) Stop() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.running {
		return ErrNotRunning
	}
	s.cancel()
	// running flips back to false in run() once exec() actually returns —
	// not here — so a Start() racing right after this Stop() correctly
	// waits for the previous goroutine to finish unwinding rather than
	// overlapping with it (see run()'s locking).
	return nil
}

func (s *Sequencer) run(ctx context.Context, ops []Operation) {
	err := s.exec(ctx, ops)

	s.mu.Lock()
	s.running = false
	s.cancel = nil
	s.mu.Unlock()

	switch {
	case err == nil:
		log.Print("sequence: completed")
	case errors.Is(err, context.Canceled):
		log.Print("sequence: stopped")
	default:
		log.Printf("sequence: aborted: %v", err)
	}
}

// exec recursively walks ops, enqueuing each HardwareCommand leaf (after its
// delay) and repeating each Loop's Body Times times (or forever if
// Times==0). It checks ctx at the top of every iteration so a Stop() during
// an infinite loop takes effect within one iteration, not just between
// top-level steps.
func (s *Sequencer) exec(ctx context.Context, ops []Operation) error {
	for _, op := range ops {
		if err := ctx.Err(); err != nil {
			return err
		}
		switch o := op.(type) {
		case Loop:
			for i := 0; o.Times == 0 || i < o.Times; i++ {
				if err := ctx.Err(); err != nil {
					return err
				}
				if err := s.exec(ctx, o.Body); err != nil {
					return err
				}
			}
		case HardwareCommand:
			if err := command.DelayedEnqueue(ctx, s.Queue, o.Delay(), toCommand(o)); err != nil {
				return err
			}
		default:
			return ErrUnknownOperation
		}
	}
	return nil
}
