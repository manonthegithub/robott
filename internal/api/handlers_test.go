package api

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

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

func newHandlers(q command.CommandQueue) *Handlers {
	return &Handlers{
		Queue:         q,
		ServoMinAngle: 0,
		ServoMaxAngle: 180,
		Sequencer:     &sequence.Sequencer{Queue: q},
		Ctx:           context.Background(),
	}
}

func newRouter(q command.CommandQueue) http.Handler {
	return NewRouter(newHandlers(q))
}

func doRequest(t *testing.T, h http.Handler, method, path string, body any) *httptest.ResponseRecorder {
	t.Helper()
	var buf bytes.Buffer
	if body != nil {
		if err := json.NewEncoder(&buf).Encode(body); err != nil {
			t.Fatalf("encode request body: %v", err)
		}
	}
	req := httptest.NewRequest(method, path, &buf)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

func TestHandleLED_HappyPath(t *testing.T) {
	q := &fakeQueue{}
	h := newRouter(q)

	rec := doRequest(t, h, http.MethodPost, "/led", map[string]any{"on": true})

	if rec.Code != http.StatusAccepted {
		t.Fatalf("status = %d, want %d, body=%s", rec.Code, http.StatusAccepted, rec.Body.String())
	}
	got := q.snapshot()
	if len(got) != 1 || got[0] != command.Command(command.LEDCommand{On: true}) {
		t.Fatalf("enqueued = %v, want [LEDCommand{On:true}]", got)
	}
}

func TestHandleStepper_HappyPath(t *testing.T) {
	q := &fakeQueue{}
	h := newRouter(q)

	rec := doRequest(t, h, http.MethodPost, "/stepper", map[string]any{"steps": 200, "dir": "cw"})

	if rec.Code != http.StatusAccepted {
		t.Fatalf("status = %d, want %d, body=%s", rec.Code, http.StatusAccepted, rec.Body.String())
	}
	want := command.StepperCommand{Steps: 200, Dir: command.DirCW}
	got := q.snapshot()
	if len(got) != 1 || got[0] != command.Command(want) {
		t.Fatalf("enqueued = %v, want [%v]", got, want)
	}
}

func TestHandleStepper_ValidationFailures(t *testing.T) {
	tests := []struct {
		name string
		body map[string]any
	}{
		{name: "zero steps", body: map[string]any{"steps": 0, "dir": "cw"}},
		{name: "negative steps", body: map[string]any{"steps": -5, "dir": "cw"}},
		{name: "bad direction", body: map[string]any{"steps": 10, "dir": "sideways"}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			q := &fakeQueue{}
			h := newRouter(q)

			rec := doRequest(t, h, http.MethodPost, "/stepper", tt.body)

			if rec.Code != http.StatusBadRequest {
				t.Fatalf("status = %d, want %d, body=%s", rec.Code, http.StatusBadRequest, rec.Body.String())
			}
			if got := q.snapshot(); len(got) != 0 {
				t.Fatalf("enqueued = %v, want none", got)
			}
		})
	}
}

func TestHandleServo_HappyPath(t *testing.T) {
	q := &fakeQueue{}
	h := newRouter(q)

	rec := doRequest(t, h, http.MethodPost, "/servo", map[string]any{"angle_deg": 90.0})

	if rec.Code != http.StatusAccepted {
		t.Fatalf("status = %d, want %d, body=%s", rec.Code, http.StatusAccepted, rec.Body.String())
	}
	want := command.ServoCommand{AngleDeg: 90.0}
	got := q.snapshot()
	if len(got) != 1 || got[0] != command.Command(want) {
		t.Fatalf("enqueued = %v, want [%v]", got, want)
	}
}

func TestHandleServo_OutOfRangeRejected(t *testing.T) {
	q := &fakeQueue{}
	h := newRouter(q)

	rec := doRequest(t, h, http.MethodPost, "/servo", map[string]any{"angle_deg": 999.0})

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d, body=%s", rec.Code, http.StatusBadRequest, rec.Body.String())
	}
}

func TestHandlers_MalformedJSON(t *testing.T) {
	q := &fakeQueue{}
	h := newRouter(q)

	req := httptest.NewRequest(http.MethodPost, "/led", bytes.NewBufferString("{not json"))
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d, body=%s", rec.Code, http.StatusBadRequest, rec.Body.String())
	}
}

func TestHandlers_QueueFullReturns503(t *testing.T) {
	q := &fakeQueue{err: command.ErrQueueFull}
	h := newRouter(q)

	rec := doRequest(t, h, http.MethodPost, "/led", map[string]any{"on": true})

	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want %d, body=%s", rec.Code, http.StatusServiceUnavailable, rec.Body.String())
	}
}

func TestHandlers_UnexpectedQueueErrorReturns500(t *testing.T) {
	q := &fakeQueue{err: errors.New("boom")}
	h := newRouter(q)

	rec := doRequest(t, h, http.MethodPost, "/led", map[string]any{"on": true})

	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want %d, body=%s", rec.Code, http.StatusInternalServerError, rec.Body.String())
	}
}

// --- delay_ms ---

func TestHandleLED_DelayMsZeroIsUnchangedSyncPath(t *testing.T) {
	q := &fakeQueue{}
	h := newRouter(q)

	rec := doRequest(t, h, http.MethodPost, "/led", map[string]any{"on": true, "delay_ms": 0})

	if rec.Code != http.StatusAccepted {
		t.Fatalf("status = %d, want %d, body=%s", rec.Code, http.StatusAccepted, rec.Body.String())
	}
	// Synchronous path: the command must already be on the queue by the
	// time the response is written, no goroutine/race involved.
	if got := q.snapshot(); len(got) != 1 {
		t.Fatalf("enqueued = %v, want 1 command already present", got)
	}
}

func TestHandleLED_DelayMsOmittedBehavesLikeZero(t *testing.T) {
	q := &fakeQueue{}
	h := newRouter(q)

	rec := doRequest(t, h, http.MethodPost, "/led", map[string]any{"on": true})

	if rec.Code != http.StatusAccepted {
		t.Fatalf("status = %d, want %d, body=%s", rec.Code, http.StatusAccepted, rec.Body.String())
	}
	if got := q.snapshot(); len(got) != 1 {
		t.Fatalf("enqueued = %v, want 1 command already present (delay_ms omitted == 0)", got)
	}
}

func TestHandleLED_DelayMsPositiveRespondsBeforeEnqueuing(t *testing.T) {
	q := &fakeQueue{}
	h := newRouter(q)

	rec := doRequest(t, h, http.MethodPost, "/led", map[string]any{"on": true, "delay_ms": 100})

	if rec.Code != http.StatusAccepted {
		t.Fatalf("status = %d, want %d, body=%s", rec.Code, http.StatusAccepted, rec.Body.String())
	}
	// The response came back already, but the delay hasn't elapsed yet.
	if got := q.snapshot(); len(got) != 0 {
		t.Fatalf("enqueued = %v, want none yet (delay hasn't elapsed)", got)
	}

	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		if len(q.snapshot()) == 1 {
			return
		}
		time.Sleep(2 * time.Millisecond)
	}
	t.Fatal("command was never enqueued after delay_ms elapsed")
}

// --- /sequence, /sequence/stop ---

func TestHandleSequence_HappyPath(t *testing.T) {
	q := &fakeQueue{}
	h := newRouter(q)

	body := map[string]any{"seq": []map[string]any{
		{"type": "led", "on": true},
		{"type": "servo", "angle_deg": 90.0},
	}}
	rec := doRequest(t, h, http.MethodPost, "/sequence", body)

	if rec.Code != http.StatusAccepted {
		t.Fatalf("status = %d, want %d, body=%s", rec.Code, http.StatusAccepted, rec.Body.String())
	}

	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) && len(q.snapshot()) < 2 {
		time.Sleep(2 * time.Millisecond)
	}
	got := q.snapshot()
	want := []command.Command{command.LEDCommand{On: true}, command.ServoCommand{AngleDeg: 90}}
	if len(got) != len(want) || got[0] != want[0] || got[1] != want[1] {
		t.Fatalf("enqueued = %v, want %v", got, want)
	}
}

func TestHandleSequence_ServoOutOfRangeInsideSequenceRejected(t *testing.T) {
	q := &fakeQueue{}
	h := newRouter(q)

	body := map[string]any{"seq": []map[string]any{
		{"type": "loop", "times": 1, "body": []map[string]any{
			{"type": "servo", "angle_deg": 999.0},
		}},
	}}
	rec := doRequest(t, h, http.MethodPost, "/sequence", body)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d, body=%s", rec.Code, http.StatusBadRequest, rec.Body.String())
	}
	if got := q.snapshot(); len(got) != 0 {
		t.Fatalf("enqueued = %v, want none", got)
	}
}

func TestHandleSequence_WhileRunningReturns409(t *testing.T) {
	q := &fakeQueue{}
	h := newRouter(q)

	infinite := map[string]any{"seq": []map[string]any{
		{"type": "loop", "times": 0, "body": []map[string]any{
			{"type": "led", "on": true, "delay_ms": 5},
		}},
	}}
	rec1 := doRequest(t, h, http.MethodPost, "/sequence", infinite)
	if rec1.Code != http.StatusAccepted {
		t.Fatalf("first /sequence status = %d, want %d, body=%s", rec1.Code, http.StatusAccepted, rec1.Body.String())
	}
	defer doRequest(t, h, http.MethodPost, "/sequence/stop", nil)

	rec2 := doRequest(t, h, http.MethodPost, "/sequence", infinite)
	if rec2.Code != http.StatusConflict {
		t.Fatalf("second /sequence status = %d, want %d, body=%s", rec2.Code, http.StatusConflict, rec2.Body.String())
	}
}

func TestHandleSequenceStop_NoneRunningReturns404(t *testing.T) {
	q := &fakeQueue{}
	h := newRouter(q)

	rec := doRequest(t, h, http.MethodPost, "/sequence/stop", nil)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want %d, body=%s", rec.Code, http.StatusNotFound, rec.Body.String())
	}
}

func TestHandleSequenceStop_RunningReturns202AndStopsFurtherEnqueues(t *testing.T) {
	q := &fakeQueue{}
	h := newRouter(q)

	infinite := map[string]any{"seq": []map[string]any{
		{"type": "loop", "times": 0, "body": []map[string]any{
			{"type": "led", "on": true},
		}},
	}}
	if rec := doRequest(t, h, http.MethodPost, "/sequence", infinite); rec.Code != http.StatusAccepted {
		t.Fatalf("/sequence status = %d, want %d", rec.Code, http.StatusAccepted)
	}

	time.Sleep(20 * time.Millisecond)

	rec := doRequest(t, h, http.MethodPost, "/sequence/stop", nil)
	if rec.Code != http.StatusAccepted {
		t.Fatalf("/sequence/stop status = %d, want %d, body=%s", rec.Code, http.StatusAccepted, rec.Body.String())
	}

	time.Sleep(20 * time.Millisecond) // let the goroutine actually unwind
	countAtStop := len(q.snapshot())
	time.Sleep(50 * time.Millisecond)
	if countAfter := len(q.snapshot()); countAfter != countAtStop {
		t.Fatalf("commands kept arriving after stop: %d then %d", countAtStop, countAfter)
	}
}

func TestHandleSequence_UnknownDiscriminatorRejected(t *testing.T) {
	q := &fakeQueue{}
	h := newRouter(q)

	body := map[string]any{"seq": []map[string]any{
		{"type": "spin_around_and_catch_fire"},
	}}
	rec := doRequest(t, h, http.MethodPost, "/sequence", body)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d, body=%s", rec.Code, http.StatusBadRequest, rec.Body.String())
	}
}

func TestHandleSequence_LedWithOmittedOnDefaultsToFalse(t *testing.T) {
	// With the oneOf/allOf shape, "on" isn't wrapped in a nested "led"
	// object the way the earlier flat design had it, so an omitted "on" is
	// just a normal missing-optional-field decode (zero value), not a
	// mismatched-payload error — this documents that behavior rather than
	// asserting a 400 that wouldn't actually happen.
	q := &fakeQueue{}
	h := newRouter(q)

	body := map[string]any{"seq": []map[string]any{
		{"type": "led"},
	}}
	rec := doRequest(t, h, http.MethodPost, "/sequence", body)
	if rec.Code != http.StatusAccepted {
		t.Fatalf("status = %d, want %d, body=%s", rec.Code, http.StatusAccepted, rec.Body.String())
	}

	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) && len(q.snapshot()) < 1 {
		time.Sleep(2 * time.Millisecond)
	}
	got := q.snapshot()
	if len(got) != 1 || got[0] != command.Command(command.LEDCommand{On: false}) {
		t.Fatalf("enqueued = %v, want [LEDCommand{On:false}]", got)
	}
}
