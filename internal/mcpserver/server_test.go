package mcpserver

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"robottt/internal/api"
	"robottt/internal/command"
	"robottt/internal/sequence"
)

type fakeQueue struct {
	mu       sync.Mutex
	enqueued []command.Command
	err      error
}

func (q *fakeQueue) Enqueue(cmd command.Command) error {
	q.mu.Lock()
	defer q.mu.Unlock()
	if q.err != nil {
		return q.err
	}
	q.enqueued = append(q.enqueued, cmd)
	return nil
}

func (q *fakeQueue) EnqueueBlocking(_ context.Context, cmd command.Command) error {
	return q.Enqueue(cmd)
}

func (q *fakeQueue) Dequeue(_ context.Context) (command.Command, error) {
	panic("not used by handlers")
}

func (q *fakeQueue) snapshot() []command.Command {
	q.mu.Lock()
	defer q.mu.Unlock()
	out := make([]command.Command, len(q.enqueued))
	copy(out, q.enqueued)
	return out
}

func newTestServer(q command.CommandQueue) *Server {
	handlers := &api.Handlers{
		Queue:         q,
		ServoMinAngle: 0,
		ServoMaxAngle: 180,
		Sequencer:     &sequence.Sequencer{Queue: q},
		Ctx:           context.Background(),
	}
	return New(handlers)
}

func resultText(t *testing.T, res *mcp.CallToolResult) string {
	t.Helper()
	if len(res.Content) != 1 {
		t.Fatalf("result content = %v, want exactly 1 item", res.Content)
	}
	tc, ok := res.Content[0].(*mcp.TextContent)
	if !ok {
		t.Fatalf("result content[0] = %T, want *mcp.TextContent", res.Content[0])
	}
	return tc.Text
}

func TestHandleSetLED_HappyPath(t *testing.T) {
	q := &fakeQueue{}
	s := newTestServer(q)

	res, _, err := s.handleSetLED(context.Background(), nil, LedInput{On: true})
	if err != nil {
		t.Fatalf("handleSetLED() error = %v, want nil", err)
	}
	if res.IsError {
		t.Fatalf("handleSetLED() result is an error: %s", resultText(t, res))
	}
	got := q.snapshot()
	if len(got) != 1 || got[0] != command.Command(command.LEDCommand{On: true}) {
		t.Fatalf("enqueued = %v, want [LEDCommand{On:true}]", got)
	}
}

func TestHandleSetLED_DelayMsPositiveRespondsBeforeEnqueuing(t *testing.T) {
	q := &fakeQueue{}
	s := newTestServer(q)

	res, _, err := s.handleSetLED(context.Background(), nil, LedInput{On: true, DelayMs: 50})
	if err != nil {
		t.Fatalf("handleSetLED() error = %v, want nil", err)
	}
	if res.IsError {
		t.Fatalf("handleSetLED() result is an error: %s", resultText(t, res))
	}
	if got := q.snapshot(); len(got) != 0 {
		t.Fatalf("enqueued = %v, want none yet (delay hasn't elapsed)", got)
	}

	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) && len(q.snapshot()) < 1 {
		time.Sleep(2 * time.Millisecond)
	}
	if got := q.snapshot(); len(got) != 1 {
		t.Fatalf("enqueued = %v, want 1 command after the delay elapsed", got)
	}
}

func TestHandleMoveStepper_ValidationFailureIsErrorResult(t *testing.T) {
	q := &fakeQueue{}
	s := newTestServer(q)

	res, _, err := s.handleMoveStepper(context.Background(), nil, StepperInput{Steps: 0, Dir: "cw"})
	if err != nil {
		t.Fatalf("handleMoveStepper() error = %v, want nil (validation failures are IsError results, not Go errors)", err)
	}
	if !res.IsError {
		t.Fatalf("handleMoveStepper() with steps=0 result.IsError = false, want true: %s", resultText(t, res))
	}
	if got := q.snapshot(); len(got) != 0 {
		t.Fatalf("enqueued = %v, want none", got)
	}
}

func TestHandleSetServo_HappyPath(t *testing.T) {
	q := &fakeQueue{}
	s := newTestServer(q)

	res, _, err := s.handleSetServo(context.Background(), nil, ServoInput{AngleDeg: 90})
	if err != nil {
		t.Fatalf("handleSetServo() error = %v, want nil", err)
	}
	if res.IsError {
		t.Fatalf("handleSetServo() result is an error: %s", resultText(t, res))
	}
	got := q.snapshot()
	if len(got) != 1 || got[0] != command.Command(command.ServoCommand{AngleDeg: 90}) {
		t.Fatalf("enqueued = %v, want [ServoCommand{AngleDeg:90}]", got)
	}
}

func TestHandleRunSequence_HappyPath(t *testing.T) {
	q := &fakeQueue{}
	s := newTestServer(q)

	in := RunSequenceInput{Seq: []SequenceStepInput{
		{Type: "led", On: true},
		{Type: "servo", AngleDeg: 90},
		{Type: "loop", Times: 2, Body: []SequenceStepInput{
			{Type: "led", On: false},
		}},
	}}

	res, _, err := s.handleRunSequence(context.Background(), nil, in)
	if err != nil {
		t.Fatalf("handleRunSequence() error = %v, want nil", err)
	}
	if res.IsError {
		t.Fatalf("handleRunSequence() result is an error: %s", resultText(t, res))
	}

	want := []command.Command{
		command.LEDCommand{On: true},
		command.ServoCommand{AngleDeg: 90},
		command.LEDCommand{On: false},
		command.LEDCommand{On: false},
	}
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) && len(q.snapshot()) < len(want) {
		time.Sleep(2 * time.Millisecond)
	}
	got := q.snapshot()
	if len(got) != len(want) {
		t.Fatalf("enqueued = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("enqueued[%d] = %v, want %v", i, got[i], want[i])
		}
	}
}

func TestHandleRunSequence_ServoOutOfRangeIsErrorResult(t *testing.T) {
	q := &fakeQueue{}
	s := newTestServer(q)

	in := RunSequenceInput{Seq: []SequenceStepInput{
		{Type: "servo", AngleDeg: 999},
	}}

	res, _, err := s.handleRunSequence(context.Background(), nil, in)
	if err != nil {
		t.Fatalf("handleRunSequence() error = %v, want nil", err)
	}
	if !res.IsError {
		t.Fatalf("handleRunSequence() with out-of-range angle result.IsError = false, want true: %s", resultText(t, res))
	}
	if got := q.snapshot(); len(got) != 0 {
		t.Fatalf("enqueued = %v, want none", got)
	}
}

func TestHandleRunSequence_UnknownStepTypeReturnsGoError(t *testing.T) {
	q := &fakeQueue{}
	s := newTestServer(q)

	in := RunSequenceInput{Seq: []SequenceStepInput{
		{Type: "spin_around_and_catch_fire"},
	}}

	// Unlike a rejected-by-PostSequence case, an unrecognized step type
	// fails before ever reaching PostSequence (toGenOperation itself
	// errors), so this is a Go error, not just an IsError result.
	_, _, err := s.handleRunSequence(context.Background(), nil, in)
	if err == nil {
		t.Fatal("handleRunSequence() error = nil, want a build error for an unknown step type")
	}
}

func TestHandleStopSequence_NoneRunningIsErrorResult(t *testing.T) {
	q := &fakeQueue{}
	s := newTestServer(q)

	res, _, err := s.handleStopSequence(context.Background(), nil, StopSequenceInput{})
	if err != nil {
		t.Fatalf("handleStopSequence() error = %v, want nil", err)
	}
	if !res.IsError {
		t.Fatalf("handleStopSequence() with nothing running result.IsError = false, want true: %s", resultText(t, res))
	}
}

func TestHandleRunSequence_ThenStopSequenceStopsIt(t *testing.T) {
	q := &fakeQueue{}
	s := newTestServer(q)

	infinite := RunSequenceInput{Seq: []SequenceStepInput{
		{Type: "loop", Times: 0, Body: []SequenceStepInput{
			{Type: "led", On: true},
		}},
	}}
	res, _, err := s.handleRunSequence(context.Background(), nil, infinite)
	if err != nil {
		t.Fatalf("handleRunSequence() error = %v, want nil", err)
	}
	if res.IsError {
		t.Fatalf("handleRunSequence() result is an error: %s", resultText(t, res))
	}

	time.Sleep(20 * time.Millisecond)

	stopRes, _, err := s.handleStopSequence(context.Background(), nil, StopSequenceInput{})
	if err != nil {
		t.Fatalf("handleStopSequence() error = %v, want nil", err)
	}
	if stopRes.IsError {
		t.Fatalf("handleStopSequence() result is an error: %s", resultText(t, stopRes))
	}

	time.Sleep(20 * time.Millisecond)
	countAtStop := len(q.snapshot())
	time.Sleep(50 * time.Millisecond)
	if countAfter := len(q.snapshot()); countAfter != countAtStop {
		t.Fatalf("commands kept arriving after stop_sequence: %d then %d", countAtStop, countAfter)
	}
}
