package executor

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"robottt/internal/command"
)

// fakeQueue is a CommandQueue that yields a fixed sequence of commands, then
// blocks until ctx is done (mirrors ChannelQueue's real Dequeue contract).
type fakeQueue struct {
	mu   sync.Mutex
	cmds []command.Command
}

func (q *fakeQueue) Enqueue(cmd command.Command) error {
	q.mu.Lock()
	defer q.mu.Unlock()
	q.cmds = append(q.cmds, cmd)
	return nil
}

func (q *fakeQueue) Dequeue(ctx context.Context) (command.Command, error) {
	q.mu.Lock()
	if len(q.cmds) > 0 {
		cmd := q.cmds[0]
		q.cmds = q.cmds[1:]
		q.mu.Unlock()
		return cmd, nil
	}
	q.mu.Unlock()

	<-ctx.Done()
	return nil, ctx.Err()
}

type fakeGPIO struct {
	mu     sync.Mutex
	calls  []bool
	err    error
	closed bool
}

func (f *fakeGPIO) SetLED(on bool) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, on)
	return f.err
}
func (f *fakeGPIO) Close() error { f.closed = true; return nil }

type fakeStepper struct {
	mu    sync.Mutex
	calls []struct {
		steps int
		dir   command.Direction
	}
	closed bool
}

func (f *fakeStepper) Move(steps int, dir command.Direction) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, struct {
		steps int
		dir   command.Direction
	}{steps, dir})
	return nil
}
func (f *fakeStepper) Close() error { f.closed = true; return nil }

type fakeServo struct {
	mu     sync.Mutex
	calls  []float64
	closed bool
}

func (f *fakeServo) SetAngle(deg float64) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, deg)
	return nil
}
func (f *fakeServo) Close() error { f.closed = true; return nil }

func waitFor(t *testing.T, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatal("condition not met within 1s")
}

func TestExecutor_DispatchesEachCommandTypeToMatchingController(t *testing.T) {
	q := &fakeQueue{cmds: []command.Command{
		command.LEDCommand{On: true},
		command.StepperCommand{Steps: 5, Dir: command.DirCW},
		command.ServoCommand{AngleDeg: 45},
	}}
	gpio := &fakeGPIO{}
	stepper := &fakeStepper{}
	servo := &fakeServo{}
	e := &Executor{Queue: q, GPIO: gpio, Stepper: stepper, Servo: servo}

	ctx, cancel := context.WithCancel(context.Background())
	go e.Run(ctx)
	defer cancel()

	waitFor(t, func() bool {
		gpio.mu.Lock()
		stepper.mu.Lock()
		servo.mu.Lock()
		defer gpio.mu.Unlock()
		defer stepper.mu.Unlock()
		defer servo.mu.Unlock()
		return len(gpio.calls) == 1 && len(stepper.calls) == 1 && len(servo.calls) == 1
	})

	if !gpio.calls[0] {
		t.Fatalf("gpio.calls = %v, want [true]", gpio.calls)
	}
	if stepper.calls[0].steps != 5 || stepper.calls[0].dir != command.DirCW {
		t.Fatalf("stepper.calls[0] = %+v, want steps=5 dir=cw", stepper.calls[0])
	}
	if servo.calls[0] != 45 {
		t.Fatalf("servo.calls = %v, want [45]", servo.calls)
	}
}

func TestExecutor_ControllerErrorDoesNotStopLoop(t *testing.T) {
	q := &fakeQueue{cmds: []command.Command{
		command.LEDCommand{On: true},
		command.LEDCommand{On: false},
	}}
	gpio := &fakeGPIO{err: errors.New("boom")}
	e := &Executor{Queue: q, GPIO: gpio, Stepper: &fakeStepper{}, Servo: &fakeServo{}}

	ctx, cancel := context.WithCancel(context.Background())
	go e.Run(ctx)
	defer cancel()

	waitFor(t, func() bool {
		gpio.mu.Lock()
		defer gpio.mu.Unlock()
		return len(gpio.calls) == 2
	})
}

func TestExecutor_ContextCancelStopsLoopAndClosesControllers(t *testing.T) {
	q := &fakeQueue{}
	gpio := &fakeGPIO{}
	stepper := &fakeStepper{}
	servo := &fakeServo{}
	e := &Executor{Queue: q, GPIO: gpio, Stepper: stepper, Servo: servo}

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		e.Run(ctx)
		close(done)
	}()

	cancel()

	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("Run() did not return within 1s of ctx cancel")
	}

	if !gpio.closed || !stepper.closed || !servo.closed {
		t.Fatalf("expected all controllers closed: gpio=%v stepper=%v servo=%v", gpio.closed, stepper.closed, servo.closed)
	}
}

func TestExecutor_NilStepperAndServoDropCommandsWithoutPanic(t *testing.T) {
	q := &fakeQueue{cmds: []command.Command{
		command.StepperCommand{Steps: 5, Dir: command.DirCW},
		command.ServoCommand{AngleDeg: 45},
		command.LEDCommand{On: true},
	}}
	gpio := &fakeGPIO{}
	e := &Executor{Queue: q, GPIO: gpio}

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		e.Run(ctx)
		close(done)
	}()

	waitFor(t, func() bool {
		gpio.mu.Lock()
		defer gpio.mu.Unlock()
		return len(gpio.calls) == 1
	})

	cancel()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("Run() did not return within 1s of ctx cancel")
	}

	if !gpio.closed {
		t.Fatal("expected GPIO controller closed")
	}
}
