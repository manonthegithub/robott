package api

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"robottt/internal/command"
)

type fakeQueue struct {
	enqueued []command.Command
	err      error
}

func (q *fakeQueue) Enqueue(cmd command.Command) error {
	if q.err != nil {
		return q.err
	}
	q.enqueued = append(q.enqueued, cmd)
	return nil
}

func (q *fakeQueue) Dequeue(_ context.Context) (command.Command, error) {
	panic("not used by handlers")
}

func newRouter(q command.CommandQueue) http.Handler {
	return NewRouter(&Handlers{Queue: q, ServoMinAngle: 0, ServoMaxAngle: 180})
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
	h := NewRouter(&Handlers{Queue: q, ServoMinAngle: 0, ServoMaxAngle: 180})

	rec := doRequest(t, h, http.MethodPost, "/led", map[string]any{"on": true})

	if rec.Code != http.StatusAccepted {
		t.Fatalf("status = %d, want %d, body=%s", rec.Code, http.StatusAccepted, rec.Body.String())
	}
	if len(q.enqueued) != 1 || q.enqueued[0] != command.Command(command.LEDCommand{On: true}) {
		t.Fatalf("enqueued = %v, want [LEDCommand{On:true}]", q.enqueued)
	}
}

func TestHandleStepper_HappyPath(t *testing.T) {
	q := &fakeQueue{}
	h := NewRouter(&Handlers{Queue: q, ServoMinAngle: 0, ServoMaxAngle: 180})

	rec := doRequest(t, h, http.MethodPost, "/stepper", map[string]any{"steps": 200, "dir": "cw"})

	if rec.Code != http.StatusAccepted {
		t.Fatalf("status = %d, want %d, body=%s", rec.Code, http.StatusAccepted, rec.Body.String())
	}
	want := command.StepperCommand{Steps: 200, Dir: command.DirCW}
	if len(q.enqueued) != 1 || q.enqueued[0] != command.Command(want) {
		t.Fatalf("enqueued = %v, want [%v]", q.enqueued, want)
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
			h := NewRouter(&Handlers{Queue: q, ServoMinAngle: 0, ServoMaxAngle: 180})

			rec := doRequest(t, h, http.MethodPost, "/stepper", tt.body)

			if rec.Code != http.StatusBadRequest {
				t.Fatalf("status = %d, want %d, body=%s", rec.Code, http.StatusBadRequest, rec.Body.String())
			}
			if len(q.enqueued) != 0 {
				t.Fatalf("enqueued = %v, want none", q.enqueued)
			}
		})
	}
}

func TestHandleServo_HappyPath(t *testing.T) {
	q := &fakeQueue{}
	h := NewRouter(&Handlers{Queue: q, ServoMinAngle: 0, ServoMaxAngle: 180})

	rec := doRequest(t, h, http.MethodPost, "/servo", map[string]any{"angle_deg": 90.0})

	if rec.Code != http.StatusAccepted {
		t.Fatalf("status = %d, want %d, body=%s", rec.Code, http.StatusAccepted, rec.Body.String())
	}
	want := command.ServoCommand{AngleDeg: 90.0}
	if len(q.enqueued) != 1 || q.enqueued[0] != command.Command(want) {
		t.Fatalf("enqueued = %v, want [%v]", q.enqueued, want)
	}
}

func TestHandleServo_OutOfRangeRejected(t *testing.T) {
	q := &fakeQueue{}
	h := NewRouter(&Handlers{Queue: q, ServoMinAngle: 0, ServoMaxAngle: 180})

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
